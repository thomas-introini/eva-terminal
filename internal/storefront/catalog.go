package storefront

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/thomas/eva-terminal-go/internal/storeapi"
)

type CatalogSnapshot struct {
	Version    int                        `json:"version"`
	Store      string                     `json:"store"`
	UpdatedAt  time.Time                  `json:"updated_at"`
	Products   []storeapi.Product         `json:"products"`
	Variations map[int][]storeapi.Product `json:"variations"`
}

type Catalog struct {
	mu              sync.RWMutex
	refreshMu       sync.Mutex
	refreshCooldown time.Duration
	lastRefresh     time.Time // Protected by refreshMu; includes failed attempts.
	client          *storeapi.Client
	path, store     string
	snapshot        CatalogSnapshot
	lastError       error
}

func NewCatalog(client *storeapi.Client, stateDir, store string, refreshCooldown time.Duration) (*Catalog, error) {
	if refreshCooldown <= 0 {
		return nil, errors.New("catalog refresh cooldown must be positive")
	}
	c := &Catalog{client: client, store: store, path: filepath.Join(stateDir, "catalog-"+digest(store)+".json"), refreshCooldown: refreshCooldown}
	if err := readJSON(c.path, &c.snapshot); err != nil {
		return nil, err
	}
	if c.snapshot.Version != 0 && (c.snapshot.Version != 1 || c.snapshot.Store != store) {
		return nil, errors.New("unsupported catalog snapshot")
	}
	return c, nil
}

func (c *Catalog) Snapshot() (CatalogSnapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return clone(c.snapshot), c.lastError
}

func (c *Catalog) Products(search string, inStock bool) []storeapi.Product {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []storeapi.Product
	for _, p := range c.snapshot.Products {
		if inStock && !p.IsInStock {
			continue
		}
		if !strings.Contains(strings.ToLower(p.Name), strings.ToLower(search)) {
			continue
		}
		out = append(out, p)
	}
	return clone(out)
}

func (c *Catalog) Refresh(ctx context.Context) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if time.Since(c.lastRefresh) < c.refreshCooldown {
		_, err := c.Status()
		return err
	}
	// Start the cooldown after completion so queued callers share this result.
	defer func() { c.lastRefresh = time.Now() }()
	s := CatalogSnapshot{Version: 1, Store: c.store, UpdatedAt: time.Now().UTC(), Variations: make(map[int][]storeapi.Product)}
	var err error
	s.Products, err = c.all(ctx, storeapi.ProductQuery{})
	if err == nil {
		for _, p := range s.Products {
			if p.Type != "variable" {
				continue
			}
			s.Variations[p.ID], err = c.all(ctx, storeapi.ProductQuery{Type: "variation", ParentIDs: []int{p.ID}})
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		err = atomicJSON(c.path, s)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastError = err
	if err == nil {
		c.snapshot = s
	}
	return err
}

func (c *Catalog) all(ctx context.Context, q storeapi.ProductQuery) ([]storeapi.Product, error) {
	q.PerPage = 100
	var out []storeapi.Product
	for q.Page = 1; ; q.Page++ {
		rows, err := c.client.GetProducts(ctx, q)
		if err != nil {
			var api *storeapi.StoreError
			if q.Page > 1 && errors.As(err, &api) && api.Code == "rest_post_invalid_page_number" {
				return out, nil
			}
			return nil, err
		}
		out = append(out, rows...)
		if len(rows) < q.PerPage {
			return out, nil
		}
	}
}

func (c *Catalog) Run(ctx context.Context, interval time.Duration) {
	_ = c.Refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.Refresh(ctx)
		}
	}
}

func (c *Catalog) Status() (time.Time, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot.UpdatedAt, c.lastError
}
func (c *Catalog) Variations(id int) []storeapi.Product {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return clone(c.snapshot.Variations[id])
}
