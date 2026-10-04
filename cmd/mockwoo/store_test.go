package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thomas/eva-terminal-go/internal/storeapi"
	"github.com/thomas/eva-terminal-go/internal/storefront"
)

type lossyTransport struct {
	base http.RoundTripper
	lose atomic.Bool
}

func (t *lossyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(r)
	if err == nil && r.URL.Path == "/wp-json/wc/store/v1/cart/add-item" && t.lose.Swap(false) {
		resp.Body.Close()
		return nil, errors.New("response lost after server applied add")
	}
	return resp, err
}

func TestDurableStorefront(t *testing.T) {
	t.Setenv("EVA_BRIDGE_KEY", "test-bridge-key-with-at-least-32-characters")
	t.Setenv("MOCK_PAYMENT_SECONDS", "0")
	backend := newMockStore()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); backend.ServeHTTP(w, r) }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	lossy := &lossyTransport{base: http.DefaultTransport}
	factory := func(token string) *storeapi.Client {
		return storeapi.NewClient(server.URL, storeapi.WithHTTPClient(&http.Client{Transport: lossy}), storeapi.WithSessionTokens(token, ""), storeapi.WithBridgeKey(os.Getenv("EVA_BRIDGE_KEY")))
	}
	manager := storefront.NewSessions(ctx, dir, server.URL, factory)
	a, err := manager.Open("SHA256:shopperA")
	if err != nil {
		t.Fatal(err)
	}
	b, err := manager.Open("SHA256:shopperB")
	if err != nil {
		t.Fatal(err)
	}
	aAgain, _ := manager.Open("SHA256:shopperA")
	if aAgain != a {
		t.Fatal("same key did not share controller")
	}
	if err = a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err = b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().CartToken == b.Snapshot().CartToken {
		t.Fatal("shared Woo cart token")
	}
	if err = a.Add(storefront.Intent{ID: 1, Name: "Coffee", Quantity: 1}); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	burstStarted := time.Now()
	for qty := 2; qty <= 30; qty++ {
		if err = a.SetQuantity(1, "", qty); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for a.Snapshot().Pending && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if st := a.Snapshot(); st.Pending || len(st.Cart.Items) != 1 || st.Cart.Items[0].Quantity != 30 {
		t.Fatalf("coalesced sync failed: %+v", st)
	}
	if calls.Load()-before >= 29 {
		t.Fatal("quantity burst made one request per key")
	}
	t.Logf("29 quantity keys: %d API calls, synchronized in %s", calls.Load()-before, time.Since(burstStarted).Round(time.Millisecond))
	if len(b.Snapshot().Cart.Items) != 0 {
		t.Fatal("shopper carts mixed")
	}
	// A failed response to an applied add must not duplicate quantity on retry.
	lossy.lose.Store(true)
	if err = b.Add(storefront.Intent{ID: 2, Name: "Second coffee", Quantity: 1}); err != nil {
		t.Fatal(err)
	}
	if err = b.Sync(ctx); err == nil {
		t.Fatal("lost response was not surfaced")
	}
	if err = b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if b.Snapshot().Cart.Items[0].Quantity != 1 {
		t.Fatal("ambiguous add was replayed")
	}
	// Validation restores the confirmed Woo quantity.
	_ = b.SetQuantity(2, "", 100)
	if err = b.Sync(ctx); err == nil {
		t.Fatal("stock rejection not surfaced")
	}
	if b.Snapshot().Desired[0].Quantity != 1 || b.Snapshot().Pending {
		t.Fatal("invalid intent not rolled back")
	}
	// Expired tokens rebuild from saved intentions.
	backend.mu.Lock()
	delete(backend.carts, b.Snapshot().CartToken)
	backend.mu.Unlock()
	if err = b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if b.Snapshot().Cart.Items[0].ID != 2 {
		t.Fatal("expired cart was not restored")
	}
	address := storeapi.CustomerAddress{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Address1: "Via Roma 1", City: "Rome", State: "RM", Postcode: "00100", Country: "IT", Phone: "061234567"}
	if err = b.Address(ctx, address, address); err != nil {
		t.Fatal(err)
	}
	if err = b.Coupon(ctx, "COFFEE10", false); err != nil {
		t.Fatal(err)
	}
	// Token renewal also restores the address and coupon before preparing a quote.
	backend.mu.Lock()
	delete(backend.carts, b.Snapshot().CartToken)
	backend.mu.Unlock()
	if err = b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if st := b.Snapshot(); st.Cart.BillingAddress.Email != address.Email || len(st.Cart.Coupons) != 1 || st.Cart.Coupons[0].Code != "COFFEE10" {
		t.Fatalf("expired token lost checkout metadata: %+v", st.Cart)
	}
	if err = b.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := storefront.QuoteFromCart(b.Snapshot().Cart)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Checkout(ctx, quote, storeapi.CheckoutRequest{BillingAddress: &address, ShippingAddress: &address}); err != nil {
		t.Fatal(err)
	}
	order := *b.Snapshot().Attempt
	if !order.Active() || order.OrderID == 0 || order.PaymentURL == "" {
		t.Fatalf("payment not linked: %+v", order)
	}
	if err = b.Add(storefront.Intent{ID: 1, Quantity: 1}); err == nil {
		t.Fatal("pending payment allowed cart edits")
	}
	if err = b.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if b.Snapshot().Attempt.OrderID != order.OrderID {
		t.Fatal("recovery duplicated order")
	}
	// A fresh process restores the same attempt before synchronizing the emptied cart.
	cancel()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	restored, _ := storefront.NewSessions(ctx2, dir, server.URL, factory).Open("SHA256:shopperB")
	if err = restored.Recover(ctx2); err != nil {
		t.Fatal(err)
	}
	if restored.Snapshot().Attempt.OrderID != order.OrderID {
		t.Fatal("restart lost payment attempt")
	}
	if err = restored.Cancel(ctx2); err != nil {
		t.Fatal(err)
	}
	if len(restored.Snapshot().Cart.Items) != 1 {
		t.Fatal("cancel did not restore cart")
	}
	backend.mu.Lock()
	saved := backend.attempts[order.AttemptID].Order
	backend.mu.Unlock()
	if len(saved.Items) != 1 || saved.Totals.TotalPrice != order.Total {
		t.Fatal("saved order changed with cart")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))
	for _, file := range files {
		stat, _ := os.Stat(file)
		if stat.Mode().Perm() != 0600 {
			t.Fatal("private state permissions")
		}
	}
}

func TestSameKeyConcurrentIntentAndBroadcast(t *testing.T) {
	t.Setenv("EVA_BRIDGE_KEY", "test-bridge-key-with-at-least-32-characters")
	server := httptest.NewServer(newMockStore())
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := storefront.NewSessions(ctx, t.TempDir(), server.URL, func(token string) *storeapi.Client {
		return storeapi.NewClient(server.URL, storeapi.WithSessionTokens(token, ""))
	})
	a, _ := manager.Open("key")
	updates := a.Subscribe(ctx)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.Add(storefront.Intent{ID: 1, Quantity: 1}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	st := <-updates
	if st.Desired[0].Quantity != 20 || st.Pending {
		t.Fatal("same-key broadcast stale")
	}
	st.Desired[0].Quantity = 999
	if a.Snapshot().Desired[0].Quantity != 20 {
		t.Fatal("snapshot mutated controller")
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.AdjustQuantity(1, "", 1); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if a.Snapshot().Desired[0].Quantity != 40 {
		t.Fatal("simultaneous plus keys lost an increment")
	}
}

func TestMockMoneyIsExact(t *testing.T) {
	for _, tc := range []struct{ price, minor string }{{"18.99", "1899"}, {"0.01", "1"}, {"19.99", "1999"}, {"-0.01", "-1"}} {
		if got := toMinorString(tc.price); got != tc.minor {
			t.Fatalf("%s became %s", tc.price, got)
		}
	}
}
