package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const QueueSize = 512
const HTTPTimeout = 2 * time.Second
const DrainTimeout = 3 * time.Second

type Stats struct{ Accepted, Sent, Dropped, Failed, Uncertain uint64 }
type payload struct {
	Website  string         `json:"website"`
	Hostname string         `json:"hostname"`
	URL      string         `json:"url"`
	Title    string         `json:"title"`
	Referrer string         `json:"referrer"`
	ID       string         `json:"id"`
	Name     string         `json:"name,omitempty"`
	Data     map[string]any `json:"data"`
}

type Umami struct {
	cfg                                        Config
	client                                     *http.Client
	queue                                      chan []byte
	mu                                         sync.Mutex
	closed                                     bool
	cancel                                     context.CancelFunc
	done                                       chan struct{}
	accepted, sent, dropped, failed, uncertain atomic.Uint64
	now                                        func() time.Time
	lastLog                                    time.Time // worker-owned
}

// New starts exactly one process worker. Invalid/disabled configuration starts none.
// The optional client supplies a test transport; timeout and redirect policy remain fixed.
func New(ctx context.Context, cfg Config, client *http.Client) (*Umami, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	if client == nil {
		client = &http.Client{}
	}
	c := *client
	c.Timeout = HTTPTimeout
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	workerCtx, cancel := context.WithCancel(ctx)
	u := &Umami{cfg: cfg, client: &c, queue: make(chan []byte, QueueSize), cancel: cancel, done: make(chan struct{}), now: time.Now}
	go u.run(workerCtx)
	return u, nil
}

func (u *Umami) PageView(p PageView) bool { return u.enqueue(p.Context, p.Path, "", nil) }
func (u *Umami) Event(e Event) bool       { return u.enqueue(e.Context, e.Path, e.Name, e.Data) }

func (u *Umami) enqueue(c Context, path, name string, data map[string]any) bool {
	if u == nil {
		return false
	}
	title, ok := pages[path]
	if !ok || !c.Valid() || c.Environment != u.cfg.Environment {
		u.dropped.Add(1)
		return false
	}
	props := map[string]any{"channel": "ssh", "schema_version": SchemaVersion, "environment": c.Environment, "connection_id": c.ConnectionID}
	if name != "" {
		allowed, ok := fields[name]
		if !ok {
			u.dropped.Add(1)
			return false
		}
		for _, key := range strings.Fields(requiredFields[name]) {
			if _, exists := data[key]; !exists {
				u.dropped.Add(1)
				return false
			}
		}
		if name == "add_to_cart" || name == "remove_from_cart" || name == "cart_quantity_changed" {
			if data["product_id"] == nil && data["variation_id"] == nil && data["catalog_item_id"] == nil {
				u.dropped.Add(1)
				return false
			}
		}
		for k, v := range data {
			if !strings.Contains(" "+allowed+" ", " "+k+" ") || !validField(k, v) {
				u.dropped.Add(1)
				return false
			}
			props[k] = v
		}
	} else if len(data) != 0 {
		u.dropped.Add(1)
		return false
	}
	b, err := json.Marshal(struct {
		Type    string  `json:"type"`
		Payload payload `json:"payload"`
	}{"event", payload{u.cfg.WebsiteID, u.cfg.Hostname, path, title, "", c.ConnectionID, name, props}})
	if err != nil {
		u.dropped.Add(1)
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		u.dropped.Add(1)
		return false
	}
	select {
	case u.queue <- b:
		u.accepted.Add(1)
		return true
	default:
		u.dropped.Add(1)
		return false
	}
}

func (u *Umami) run(ctx context.Context) {
	defer close(u.done)
	for {
		select {
		case <-ctx.Done():
			u.abandon()
			return
		case body, ok := <-u.queue:
			if !ok {
				return
			}
			if ctx.Err() != nil {
				u.dropped.Add(1)
				u.abandon()
				return
			}
			u.send(ctx, body)
		}
	}
}

func (u *Umami) abandon() {
	u.mu.Lock()
	u.closed = true
	u.dropped.Add(uint64(len(u.queue)))
	u.mu.Unlock()
}

func (u *Umami) send(ctx context.Context, body []byte) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(u.cfg.BaseURL, "/")+"/api/send", bytes.NewReader(body))
	if err != nil {
		u.failed.Add(1)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "EvaTerminal/1.0")
	// Stateless distinct-ID contract: never forward or reuse X-Umami-Cache.
	res, err := u.client.Do(req)
	if err != nil {
		u.uncertain.Add(1)
		u.diagnostic()
		return
	}
	defer res.Body.Close()
	var receipt struct {
		SessionID string `json:"sessionId"`
		VisitID   string `json:"visitId"`
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&receipt)
	if res.StatusCode >= 200 && res.StatusCode < 300 && err == nil && UUIDValid(receipt.SessionID) && UUIDValid(receipt.VisitID) {
		u.sent.Add(1)
	} else if res.StatusCode >= 400 && res.StatusCode < 500 {
		u.failed.Add(1)
	} else {
		u.uncertain.Add(1)
	}
	u.diagnostic()
}

func (u *Umami) diagnostic() {
	if u.now().Sub(u.lastLog) < time.Minute {
		return
	}
	u.lastLog = u.now()
	log.Printf("analytics counters: %+v", u.Stats())
}

func (u *Umami) Stats() Stats {
	if u == nil {
		return Stats{}
	}
	return Stats{u.accepted.Load(), u.sent.Load(), u.dropped.Load(), u.failed.Load(), u.uncertain.Load()}
}

func (u *Umami) Close() {
	if u == nil {
		return
	}
	u.mu.Lock()
	if !u.closed {
		u.closed = true
		close(u.queue)
	}
	u.mu.Unlock()
	timer := time.NewTimer(DrainTimeout)
	defer timer.Stop()
	select {
	case <-u.done:
	case <-timer.C:
		u.cancel()
		<-u.done
	}
	u.cancel()
	log.Printf("analytics shutdown counters: %+v", u.Stats())
}
