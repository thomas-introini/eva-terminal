package storefront

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thomas/eva-terminal-go/internal/storeapi"
)

func TestCatalogPaginationPersistenceAndStaleSnapshot(t *testing.T) {
	var calls atomic.Int64
	var unavailable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if unavailable.Load() {
			w.WriteHeader(503)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		rows := []storeapi.Product{}
		pages := 2
		if r.URL.Query().Get("type") == "variation" {
			pages = 3
		}
		if page <= pages {
			count := 100
			if page == pages {
				count = 5
			}
			for i := 0; i < count; i++ {
				rows = append(rows, storeapi.Product{ID: (page-1)*100 + i + 1, Name: "Coffee", IsInStock: true})
			}
		}
		if page == 1 && r.URL.Query().Get("type") == "" {
			rows[0].Type = "variable"
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer server.Close()
	dir := t.TempDir()
	c, _ := NewCatalog(storeapi.NewClient(server.URL), dir, server.URL, 30*time.Second)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.Products("coffee", true)) != 105 {
		t.Fatal("catalog not fully paginated")
	}
	if len(c.Variations(1)) != 205 {
		t.Fatal("first snapshot did not paginate variations")
	}
	before := calls.Load()
	for i := 0; i < 100; i++ {
		_ = c.Products("cof", false)
	}
	if calls.Load() != before {
		t.Fatal("cached navigation called API")
	}
	unavailable.Store(true)
	c.lastRefresh = time.Now().Add(-c.refreshCooldown)
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("refresh failure hidden")
	}
	if len(c.Products("", false)) != 105 {
		t.Fatal("failed refresh discarded last good snapshot")
	}
	restored, err := NewCatalog(storeapi.NewClient(server.URL), dir, server.URL, 30*time.Second)
	if err != nil || len(restored.Products("", false)) != 105 {
		t.Fatal("restart lost catalog")
	}
}

func TestCatalogRefreshCooldown(t *testing.T) {
	var calls atomic.Int64
	var unavailable atomic.Bool
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode([]storeapi.Product{{ID: 1, Name: "Coffee"}})
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	c, err := NewCatalog(storeapi.NewClient(server.URL), t.TempDir(), server.URL, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// A refresh already in progress and requests from other connections share
	// one upstream fetch, rather than queueing a new fetch for each caller.
	results := make(chan error, 21)
	go func() { results <- c.Refresh(context.Background()) }()
	<-started
	var ready sync.WaitGroup
	ready.Add(20)
	for i := 0; i < 20; i++ {
		go func() {
			ready.Done()
			results <- c.Refresh(context.Background())
		}()
	}
	ready.Wait()
	close(release)
	for i := 0; i < 21; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	// The configured two-minute cooldown still applies after the old default
	// of 30 seconds has elapsed.
	c.lastRefresh = time.Now().Add(-time.Minute)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(c.Products("", false)) != 1 {
		t.Fatalf("refreshes did not reuse catalog: upstream calls=%d", calls.Load())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled refresh returned %v", err)
	}

	// Advance beyond the cooldown without sleeping; a refresh is allowed again.
	c.lastRefresh = time.Now().Add(-c.refreshCooldown)
	if err := c.Refresh(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatalf("refresh after cooldown: err=%v, upstream calls=%d", err, calls.Load())
	}

	// An unavailable Woo store is also protected; retain its error and the last
	// good snapshot while suppressing repeated retry requests.
	unavailable.Store(true)
	c.lastRefresh = time.Now().Add(-c.refreshCooldown)
	failure := c.Refresh(context.Background())
	if failure == nil {
		t.Fatal("refresh failure hidden")
	}
	for i := 0; i < 20; i++ {
		if err := c.Refresh(context.Background()); err != failure {
			t.Fatalf("cached refresh error=%v, want %v", err, failure)
		}
	}
	if calls.Load() != 3 || len(c.Products("", false)) != 1 {
		t.Fatalf("failed retries did not reuse catalog: upstream calls=%d", calls.Load())
	}
	unavailable.Store(false)
	c.lastRefresh = time.Now().Add(-c.refreshCooldown)
	if err := c.Refresh(context.Background()); err != nil || calls.Load() != 4 {
		t.Fatalf("recovery after cooldown: err=%v, upstream calls=%d", err, calls.Load())
	}
}
