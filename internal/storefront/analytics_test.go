package storefront

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/thomas/eva-terminal-go/internal/analytics"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
)

type analyticsRecorder struct {
	mu     sync.Mutex
	events []analytics.Event
}

func (*analyticsRecorder) PageView(analytics.PageView) bool { return true }
func (r *analyticsRecorder) Event(e analytics.Event) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return true
}
func (r *analyticsRecorder) snapshot() []analytics.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]analytics.Event(nil), r.events...)
}

func TestLocalIntentsTrackOnlyAcceptedActorAndActualQuantities(t *testing.T) {
	r := &analyticsRecorder{}
	a, _ := analytics.NewContext("development")
	b, _ := analytics.NewContext("development")
	s := &Session{path: filepath.Join(t.TempDir(), "state.json"), client: storeapi.NewClient("http://localhost"), wake: make(chan struct{}, 1), subscribers: make(map[chan SessionState]struct{}), tracker: r}
	updates := make(chan SessionState, 1)
	s.subscribers[updates] = struct{}{}
	item := Intent{ID: 101, ProductID: 1, VariationID: 101, Quantity: 1, Grind: "Espresso"}
	if err := s.Add(item, a); err != nil {
		t.Fatal(err)
	}
	_ = <-updates // shared-cart subscriber has no action side effects
	if err := s.AdjustQuantity(101, "Espresso", -1, b); err != nil {
		t.Fatal(err)
	}
	if err := s.AdjustQuantity(101, "Espresso", 1, b); err != nil {
		t.Fatal(err)
	}
	if err := s.SetQuantity(101, "Espresso", 1, b); err != nil {
		t.Fatal(err)
	}
	if err := s.SetQuantity(101, "Espresso", 0, a); err != nil {
		t.Fatal(err)
	}
	if s.SetQuantity(101, "Espresso", 0, a) == nil || s.Add(Intent{ID: 1, Quantity: 0}, a) == nil {
		t.Fatal("invalid mutation accepted")
	}
	events := r.snapshot()
	if len(events) != 4 || events[0].Name != "add_to_cart" || events[1].Name != "cart_quantity_changed" || events[2].Name != "remove_from_cart" || events[3].Name != "remove_from_cart" {
		t.Fatalf("actions: %+v", events)
	}
	if events[1].Context.ConnectionID != b.ConnectionID || events[1].Data["quantity_before"] != 1 || events[1].Data["quantity_after"] != 2 || events[2].Data["quantity"] != 1 || events[0].Data["product_id"] != 1 || events[0].Data["variation_id"] != 101 {
		t.Fatal("mutation identity/quantity incorrect")
	}
	if !reflect.DeepEqual(IntentAnalyticsIDs(Intent{ID: 999}), map[string]any{"catalog_item_id": 999}) {
		t.Fatal("legacy variant guessed parent")
	}
	_ = s.Add(item, a)
	_ = s.failed(&storeapi.StoreError{StatusCode: 400, Code: "stock", Message: "private raw failure"}, s.Snapshot().Revision, true)
	_ = s.failed(&storeapi.StoreError{StatusCode: 400}, s.Snapshot().Revision, true)
	events = r.snapshot()
	last := events[len(events)-1]
	if len(events) != 6 || last.Name != "checkout_error" || last.Context.ConnectionID != a.ConnectionID || last.Data["error_code"] != "validation" || len(last.Data) != 2 {
		t.Fatalf("sync rejection not classified once: %+v", events)
	}
	// Address/recovery revisions must not inherit a previous cart actor.
	s.state.Revision++
	s.state.Pending = true
	_ = s.failed(fmt.Errorf("address validation"), s.state.Revision, false)
	if len(r.snapshot()) != 6 {
		t.Fatal("unrelated operation inherited mutation attribution")
	}
}

func TestFrozenCheckoutSurvivesRetryRestartAndLegacy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	r := &analyticsRecorder{}
	a, _ := analytics.NewContext("development")
	b, _ := analytics.NewContext("development")
	var count atomic.Int32
	var bodiesMu sync.Mutex
	var bodies []storeapi.BridgeCheckoutRequest
	quote := storeapi.Quote{Fingerprint: "quote", Total: "1200", Currency: "EUR"}
	cart := storeapi.Cart{Extensions: map[string]any{"eva_terminal": map[string]any{"quote": quote}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/wp-json/wc/store/v1/cart":
			json.NewEncoder(w).Encode(cart)
		case "/wp-json/eva-terminal/v1/checkout":
			var body storeapi.BridgeCheckoutRequest
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			files, _ := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))
			if len(files) != 1 {
				t.Error("request sent before durable state")
			} else {
				var state SessionState
				if err := readJSON(files[0], &state); err != nil || !reflect.DeepEqual(state.Checkout, &body) || (body.Analytics != nil && !state.AnalyticsSubmitted) {
					t.Error("request/attribution not frozen before HTTP")
				}
			}
			bodiesMu.Lock()
			bodies = append(bodies, body)
			bodiesMu.Unlock()
			if count.Add(1) == 1 {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"code":"eva_resolving","message":"unknown outcome"}`)
				return
			}
			json.NewEncoder(w).Encode(storeapi.Attempt{AttemptID: body.AttemptID, PaymentState: "awaiting_payment", PaymentURL: "https://checkout.example/private", OrderID: 1})
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":"missing","message":"missing"}`)
		}
	}))
	defer server.Close()
	factory := func(token string) *storeapi.Client {
		return storeapi.NewClient(server.URL, storeapi.WithSessionTokens(token, ""))
	}
	manager := NewSessions(ctx, dir, server.URL, factory, r)
	s, err := manager.Open("same-verified-key")
	if err != nil {
		t.Fatal(err)
	}
	same, _ := manager.Open("same-verified-key")
	if same != s || a.ConnectionID == b.ConnectionID {
		t.Fatal("cart/analytics identity mixed")
	}
	if err := s.Checkout(ctx, quote, storeapi.CheckoutRequest{}, a); err == nil {
		t.Fatal("unknown outcome hidden")
	}
	if err := s.Checkout(ctx, quote, storeapi.CheckoutRequest{}, b); err != nil {
		t.Fatal(err)
	}
	cancel()
	ctx2, stop := context.WithCancel(context.Background())
	defer stop()
	restored, err := NewSessions(ctx2, dir, server.URL, factory, r).Open("same-verified-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Recover(ctx2); err != nil {
		t.Fatal(err)
	}
	bodiesMu.Lock()
	for _, body := range bodies {
		if !reflect.DeepEqual(body, bodies[0]) || body.Analytics.ConnectionID != a.ConnectionID || !analytics.UUIDValid(body.Analytics.CheckoutID) {
			t.Error("retry rewrote frozen request")
		}
	}
	legacy := bodies[0]
	legacy.Analytics = nil
	bodiesMu.Unlock()
	data, _ := json.Marshal(legacy)
	var roundTrip storeapi.BridgeCheckoutRequest
	json.Unmarshal(data, &roundTrip)
	encoded, _ := json.Marshal(roundTrip)
	if string(data) != string(encoded) || string(data) == "" || roundTrip.Analytics != nil {
		t.Fatal("legacy request rewritten")
	}
	// Context and event markers survive a real private-state file round trip.
	files, _ := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))
	info, _ := os.Stat(files[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal("analytics weakened durable-state permissions")
	}
	events := r.snapshot()
	if len(events) != 2 || events[0].Name != "checkout_submitted" || events[1].Name != "payment_link_available" || events[1].Context.ConnectionID != a.ConnectionID {
		t.Fatalf("attempt events duplicated: %+v", events)
	}
}

func TestCouponAndShippingEventsRequireWooConfirmation(t *testing.T) {
	var mu sync.Mutex
	cart := storeapi.Cart{}
	confirmed := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch req.URL.Path {
		case "/wp-json/wc/store/v1/cart/apply-coupon":
			cart.Coupons = []storeapi.CartCoupon{{Code: "PRIVATE10"}}
		case "/wp-json/wc/store/v1/cart/remove-coupon":
			cart.Coupons = nil
		case "/wp-json/wc/store/v1/cart/select-shipping-rate":
			cart.ShippingRates = []storeapi.ShippingPackage{{PackageID: 0, ShippingRates: []storeapi.ShippingRate{{RateID: "flat_rate:42", Selected: confirmed}}}}
		}
		json.NewEncoder(w).Encode(cart)
	}))
	defer server.Close()
	r := &analyticsRecorder{}
	c, _ := analytics.NewContext("development")
	s := &Session{path: filepath.Join(t.TempDir(), "state.json"), client: storeapi.NewClient(server.URL), wake: make(chan struct{}, 1), subscribers: make(map[chan SessionState]struct{}), tracker: r}
	ctx := context.Background()
	for _, remove := range []bool{false, false, true, true} {
		if err := s.Coupon(ctx, "PRIVATE10", remove, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Shipping(ctx, 0, "flat_rate:42", c); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	confirmed = false
	mu.Unlock()
	if err := s.Shipping(ctx, 0, "flat_rate:42", c); err != nil {
		t.Fatal(err)
	}
	events := r.snapshot()
	if len(events) != 3 || events[0].Name != "coupon_applied" || events[1].Name != "coupon_removed" || events[2].Data["shipping_method"] != "flat_rate" {
		t.Fatalf("unconfirmed/unchanged operations counted: %+v", events)
	}
	encoded, _ := json.Marshal(events)
	if bytes.Contains(encoded, []byte("PRIVATE10")) || len(events[2].Data) != 1 {
		t.Fatal("coupon/rate identifiers leaked")
	}
}
