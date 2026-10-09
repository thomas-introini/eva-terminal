package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testID = "11111111-1111-4111-8111-111111111111"
const receipt = `{"sessionId":"11111111-1111-4111-8111-111111111111","visitId":"22222222-2222-4222-8222-222222222222","cache":"never-reused"}`

func testConfig(base string) Config { return Config{true, base, testID, "eva-terminal", "development"} }
func testContext(t *testing.T) Context {
	t.Helper()
	c, err := NewContext("development")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConfigAndDisabledCollector(t *testing.T) {
	u, err := New(context.Background(), Config{}, nil)
	if err != nil || u != nil || u.Event(Event{}) {
		t.Fatal("disabled collector started")
	}
	u.Close()
	for _, edit := range []func(*Config){
		func(c *Config) { c.BaseURL = "https://name:password@example.com" },
		func(c *Config) { c.BaseURL = "https://example.com?key=secret" },
		func(c *Config) { c.BaseURL = "https://example.com#secret" },
		func(c *Config) { c.BaseURL = "http://example.com" },
		func(c *Config) { c.Environment = "production" },
		func(c *Config) { c.Environment = "unknown" },
		func(c *Config) { c.WebsiteID = "invalid" },
		func(c *Config) { c.Hostname = "host/?secret" },
	} {
		cfg := testConfig("http://localhost")
		edit(&cfg)
		if u, err := New(context.Background(), cfg, nil); err == nil || u != nil {
			t.Fatalf("accepted invalid config: %+v", cfg)
		}
	}
}

func TestPayloadPrivacyIdentityFIFOAndReceipts(t *testing.T) {
	var mu sync.Mutex
	var received []payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/send" || r.Method != "POST" || r.Header.Get("User-Agent") != "EvaTerminal/1.0" || r.Header.Get("X-Umami-Cache") != "" || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Authorization") != "" {
			t.Error("transport contract violated")
		}
		var envelope struct {
			Type    string  `json:"type"`
			Payload payload `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Error(err)
		}
		if envelope.Type != "event" {
			t.Error("incorrect type")
		}
		mu.Lock()
		received = append(received, envelope.Payload)
		mu.Unlock()
		fmt.Fprint(w, receipt)
	}))
	defer server.Close()
	u, err := New(context.Background(), testConfig(server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b := testContext(t), testContext(t)
	if a.ConnectionID == b.ConnectionID {
		t.Fatal("connections share identity")
	}
	u.PageView(PageView{a, "/shop"})
	u.PageView(PageView{b, "/shop"})
	for i := 1; i <= 20; i++ {
		if !u.Event(Event{a, "/shop", "view_item", map[string]any{"product_id": i}}) {
			t.Fatal("queue rejected event")
		}
	}
	for _, event := range []Event{
		{a, "/shop", "search", map[string]any{"query_length": 5, "results_count": 0, "query": "email@example.com"}},
		{a, "/shop", "view_item", map[string]any{"product_id": "123"}},
		{a, "/shop", "view_item", nil},
		{a, "/shop?token=secret", "view_item", map[string]any{"product_id": 1}},
		{a, "/shop", "purchase", nil},
		{a, "/shop", "add_to_cart", map[string]any{"product_id": 1, "quantity": 1, "confirmation": "local_intent", "grind": "arbitrary text"}},
	} {
		if u.Event(event) {
			t.Fatal("unsafe schema accepted")
		}
	}
	u.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 22 {
		t.Fatalf("received %d events", len(received))
	}
	if received[0].ID != a.ConnectionID || received[1].ID != b.ConnectionID || received[0].Name != "" {
		t.Fatal("pageview/identity contract incorrect")
	}
	for i, p := range received[2:] {
		if p.ID != a.ConnectionID || p.Data["product_id"] != float64(i+1) {
			t.Fatal("FIFO violated")
		}
	}
	stats := u.Stats()
	if stats.Accepted != 22 || stats.Sent != 22 || stats.Dropped != 6 {
		t.Fatalf("counters: %+v", stats)
	}
}

func TestBoundedQueueSlowCollectorAndDrain(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
			fmt.Fprint(w, receipt)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	u, _ := New(context.Background(), testConfig(server.URL), nil)
	c := testContext(t)
	u.PageView(PageView{c, "/shop"})
	<-started
	start := time.Now()
	for i := 0; i < QueueSize; i++ {
		if !u.PageView(PageView{c, "/cart"}) {
			t.Fatal("queue filled early")
		}
	}
	if u.PageView(PageView{c, "/cart"}) || len(u.queue) != QueueSize || time.Since(start) > time.Second {
		t.Fatal("queue unbounded or UI blocked")
	}
	// Disconnection does not cancel accepted events; only process/shutdown owns the worker.
	close(release)
	u.Close()
	if u.Stats().Sent != QueueSize+1 || u.PageView(PageView{c, "/shop"}) {
		t.Fatal("drain/close failed")
	}
}

func TestErrorResponsesNeverRetryOrFollowRedirects(t *testing.T) {
	for _, response := range []struct {
		code      int
		body      string
		uncertain bool
	}{
		{200, `{"beep":"boop"}`, true}, {500, "error", true}, {400, "rejected", false}, {302, "", true},
	} {
		t.Run(fmt.Sprint(response.code), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "/redirect")
				w.WriteHeader(response.code)
				fmt.Fprint(w, response.body)
			}))
			defer server.Close()
			u, _ := New(context.Background(), testConfig(server.URL), nil)
			u.PageView(PageView{testContext(t), "/shop"})
			u.Close()
			if calls.Load() != 1 || u.Stats().Sent != 0 || (response.uncertain && u.Stats().Uncertain != 1) || (!response.uncertain && u.Stats().Failed != 1) {
				t.Fatalf("response policy: %+v", u.Stats())
			}
		})
	}
	if Grind("Whole Beans") != "whole_beans" || Grind("email@example.com") != "" || ShippingMethod("flat_rate:4") != "flat_rate" || strings.Contains(ShippingMethod("private-courier"), "private") {
		t.Fatal("normalization failed")
	}
}

type stalledTransport struct {
	started chan struct{}
	calls   atomic.Int32
}

func (s *stalledTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.calls.Add(1)
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestTimeoutAndShutdownDeadline(t *testing.T) {
	transport := &stalledTransport{started: make(chan struct{}, 1)}
	u, _ := New(context.Background(), testConfig("http://localhost"), &http.Client{Transport: transport})
	c := testContext(t)
	u.PageView(PageView{c, "/shop"})
	<-transport.started
	u.PageView(PageView{c, "/cart"})
	u.PageView(PageView{c, "/checkout/address"})
	start := time.Now()
	u.Close()
	if elapsed := time.Since(start); elapsed < HTTPTimeout || elapsed > DrainTimeout+time.Second {
		t.Fatalf("shutdown deadline: %s", elapsed)
	}
	stats := u.Stats()
	if stats.Uncertain != 2 || stats.Dropped != 1 || transport.calls.Load() != 2 {
		t.Fatalf("timeouts retried or failed to cancel: %+v", stats)
	}
	select {
	case <-u.done:
	default:
		t.Fatal("worker leaked")
	}
}
