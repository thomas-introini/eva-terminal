package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/thomas/eva-terminal-go/internal/analytics"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
	"github.com/thomas/eva-terminal-go/internal/storefront"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

type recordingTracker struct {
	pages  []analytics.PageView
	events []analytics.Event
}

func (r *recordingTracker) PageView(p analytics.PageView) bool {
	r.pages = append(r.pages, p)
	return true
}
func (r *recordingTracker) Event(e analytics.Event) bool { r.events = append(r.events, e); return true }
func (r *recordingTracker) named(name string) []analytics.Event {
	var result []analytics.Event
	for _, e := range r.events {
		if e.Name == name {
			result = append(result, e)
		}
	}
	return result
}
func analyticsModel(t *testing.T) (Model, *recordingTracker) {
	t.Helper()
	m := uxModel()
	m.sessionState.Attempt = nil
	m.width, m.height = 80, 24
	r := &recordingTracker{}
	c, err := analytics.NewContext("development")
	if err != nil {
		t.Fatal(err)
	}
	return m.WithAnalytics(r, c), r
}
func updateAnalytics(m *Model, msg tea.Msg) {
	next, _ := m.Update(msg)
	*m = next.(Model)
}

func TestAnalyticsVisibleTransitionsAndRendering(t *testing.T) {
	m, r := analyticsModel(t)
	m.splash = true
	m.splashStarted = time.Now()
	updateAnalytics(&m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if len(r.pages) != 0 || len(r.events) != 0 {
		t.Fatal("splash counted")
	}
	updateAnalytics(&m, splashTickMsg(m.splashStarted.Add(time.Second))) // early return
	for i := 0; i < 100; i++ {
		_ = m.View()
		updateAnalytics(&m, tea.KeyReleaseMsg{Code: 'x'})
		updateAnalytics(&m, tea.WindowSizeMsg{Width: 80, Height: 24})
		updateAnalytics(&m, paymentPollMsg{})
	}
	if len(r.pages) != 1 || len(r.named("session_started")) != 1 {
		t.Fatal("redraw/poll duplicated navigation")
	}
	updateAnalytics(&m, tea.KeyPressMsg{Code: '?', Text: "?"})
	m.viewState = ViewCart
	updateAnalytics(&m, tea.WindowSizeMsg{Width: 40, Height: 10})
	updateAnalytics(&m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(r.pages) != 1 {
		t.Fatal("hidden screen counted")
	}
	m.showHelp = false
	updateAnalytics(&m, tea.WindowSizeMsg{Width: 80, Height: 24})
	updateAnalytics(&m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.viewState != ViewAddress {
		t.Fatal("valid checkout not entered")
	}
	m.viewState = ViewReview
	updateAnalytics(&m, tea.KeyReleaseMsg{})
	m.viewState = ViewOrderConfirmation
	updateAnalytics(&m, tea.KeyReleaseMsg{})
	var paths []string
	for _, p := range r.pages {
		paths = append(paths, p.Path)
	}
	if !reflect.DeepEqual(paths, []string{"/shop", "/cart", "/checkout/address", "/checkout/review", "/checkout/payment"}) || len(r.named("begin_checkout")) != 1 {
		t.Fatalf("paths: %v", paths)
	}
	m.viewState = ViewCart
	m.localCart.Clear()
	updateAnalytics(&m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.viewState != ViewCart || len(r.named("begin_checkout")) != 1 {
		t.Fatal("invalid checkout counted")
	}
}

func TestAnalyticsProductAndSearchGenerations(t *testing.T) {
	m, r := analyticsModel(t)
	updateAnalytics(&m, tea.KeyReleaseMsg{})
	old := m.analytics.productGeneration
	m.selectedProduct = &woo.Product{ID: 2}
	updateAnalytics(&m, tea.KeyReleaseMsg{})
	m.selectedProduct = &woo.Product{ID: 3}
	updateAnalytics(&m, tea.KeyReleaseMsg{})
	updateAnalytics(&m, analyticsTickMsg{"product", old})
	if len(r.named("view_item")) != 0 {
		t.Fatal("stale timer counted")
	}
	updateAnalytics(&m, analyticsTickMsg{"product", m.analytics.productGeneration})
	if events := r.named("view_item"); len(events) != 1 || events[0].Data["product_id"] != 3 {
		t.Fatal("stable selection incorrect")
	}
	m.viewState = ViewCart
	updateAnalytics(&m, tea.KeyReleaseMsg{})
	m.viewState = ViewProductList
	updateAnalytics(&m, tea.KeyReleaseMsg{})
	updateAnalytics(&m, analyticsTickMsg{"product", m.analytics.productGeneration})
	if len(r.named("view_item")) != 1 {
		t.Fatal("product impression duplicated")
	}
	m.selectedProduct = &woo.Product{ID: 4}
	updateAnalytics(&m, tea.KeyReleaseMsg{})
	gen := m.analytics.productGeneration
	m.showHelp = true
	updateAnalytics(&m, tea.KeyReleaseMsg{})
	updateAnalytics(&m, analyticsTickMsg{"product", gen})
	if len(r.named("view_item")) != 1 {
		t.Fatal("hidden product impression counted")
	}
	m.showHelp = false
	updateAnalytics(&m, tea.KeyPressMsg{Code: '/', Text: "/"})
	updateAnalytics(&m, tea.PasteMsg{Content: "private@example.com"})
	search := m.analytics.searchGeneration
	updateAnalytics(&m, analyticsTickMsg{"search", search - 1})
	updateAnalytics(&m, analyticsTickMsg{"search", search})
	updateAnalytics(&m, tea.KeyPressMsg{Code: tea.KeyEnter})
	events := r.named("search")
	if len(events) != 1 || events[0].Data["results_count"] != 0 || events[0].Data["query_length"] != 19 {
		t.Fatalf("search payload: %+v", events)
	}
	b, _ := json.Marshal(events)
	if strings.Contains(string(b), "private") || strings.Contains(string(b), "example") {
		t.Fatal("search leaked text")
	}
	updateAnalytics(&m, tea.KeyPressMsg{Code: '/', Text: "/"})
	updateAnalytics(&m, tea.KeyPressMsg{Code: tea.KeyEscape})
	updateAnalytics(&m, analyticsTickMsg{"search", search})
	if len(r.named("search")) != 1 {
		t.Fatal("cleared query counted")
	}
}

func TestPreparedShowsOnlyFinalShippingViewAndPendingReconnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"shipping_rates":[{"package_id":0,"name":"Delivery","shipping_rates":[{"rate_id":"flat_rate:1","name":"Standard"},{"rate_id":"local_pickup:2","name":"Pickup"}]}]}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shopper, _ := storefront.NewSessions(ctx, t.TempDir(), server.URL, func(string) *storeapi.Client { return storeapi.NewClient(server.URL) }).Open("verified-key")
	if err := shopper.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	m, r := analyticsModel(t)
	m.ctx, m.shopper = ctx, shopper
	m.viewState = ViewAddress
	updateAnalytics(&m, tea.KeyReleaseMsg{})
	updateAnalytics(&m, preparedMsg{fromAddress: true})
	if m.viewState != ViewShipping || len(r.pages) != 2 || r.pages[1].Path != "/checkout/shipping" {
		t.Fatalf("intermediate Review counted: %+v", r.pages)
	}
	updateAnalytics(&m, shippingCompleteMsg{})
	if r.pages[2].Path != "/checkout/review" {
		t.Fatal("shipping completion missed")
	}
	pending, p := analyticsModel(t)
	pending.viewState = ViewOrderConfirmation
	pending.sessionState.Attempt = &storeapi.Attempt{AttemptID: "pending", PaymentState: "awaiting_payment"}
	pending = pending.WithAnalytics(p, pending.analytics.connection)
	updateAnalytics(&pending, tea.WindowSizeMsg{Width: 80, Height: 24})
	if len(p.pages) != 1 || p.pages[0].Path != "/checkout/payment" || p.named("session_started")[0].Data["resumed_payment"] != true {
		t.Fatal("reconnect invented shop visit")
	}
}
