package storefront

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

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
	c, _ := NewCatalog(storeapi.NewClient(server.URL), dir, server.URL)
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
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("refresh failure hidden")
	}
	if len(c.Products("", false)) != 105 {
		t.Fatal("failed refresh discarded last good snapshot")
	}
	restored, err := NewCatalog(storeapi.NewClient(server.URL), dir, server.URL)
	if err != nil || len(restored.Products("", false)) != 105 {
		t.Fatal("restart lost catalog")
	}
}
