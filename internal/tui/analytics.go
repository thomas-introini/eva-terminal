package tui

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/thomas/eva-terminal-go/internal/analytics"
	"github.com/thomas/eva-terminal-go/internal/storefront"
)

const analyticsDebounce = 700 * time.Millisecond

type analyticsTickMsg struct {
	kind       string
	generation uint64
}
type analyticsState struct {
	tracker                 analytics.Tracker
	connection              analytics.Context
	started, resumedPayment bool
	page                    string
	product                 int
	productGeneration       uint64
	viewed                  map[int]bool
	query, lastQuery        string // local memory only; never included in a message or payload
	searchGeneration        uint64
}

func (m Model) WithAnalytics(tracker analytics.Tracker, connection analytics.Context) Model {
	m.analytics = analyticsState{tracker: tracker, connection: connection, viewed: make(map[int]bool), resumedPayment: m.viewState == ViewOrderConfirmation && m.paymentActive()}
	return m
}

func analyticsPath(view ViewState) string {
	switch view {
	case ViewProductList:
		return "/shop"
	case ViewCart:
		return "/cart"
	case ViewAddress:
		return "/checkout/address"
	case ViewShipping:
		return "/checkout/shipping"
	case ViewReview:
		return "/checkout/review"
	case ViewOrderConfirmation:
		return "/checkout/payment"
	}
	return ""
}

func (m Model) analyticsVisible() bool {
	return m.width > 0 && !m.tooSmall() && !m.splash && !m.showHelp
}
func (m Model) event(name string, data map[string]any) {
	if m.analytics.tracker != nil && m.analytics.connection.Valid() {
		m.analytics.tracker.Event(analytics.Event{Context: m.analytics.connection, Path: analyticsPath(m.viewState), Name: name, Data: data})
	}
}
func (m Model) analyticsError(stage string, err error) {
	if err != nil {
		m.event("checkout_error", map[string]any{"stage": stage, "error_code": storefront.AnalyticsErrorCode(err)})
	}
}

// Every Update, including early returns and background transitions, passes here.
// Only the final visible state is observed. View and all render helpers are pure.
func (m *Model) observeAnalytics(msg tea.Msg) tea.Cmd {
	a := &m.analytics
	if a.tracker == nil || !a.connection.Valid() {
		return nil
	}
	visible := m.analyticsVisible()
	if visible {
		path := analyticsPath(m.viewState)
		if !a.started {
			a.started = true
			m.event("session_started", map[string]any{"resumed_payment": a.resumedPayment})
		}
		if a.page != path {
			a.page = path
			a.tracker.PageView(analytics.PageView{Context: a.connection, Path: path})
		}
	}
	product := 0
	if visible && m.viewState == ViewProductList && m.selectedProduct != nil {
		product = m.selectedProduct.ID
	}
	query := ""
	if visible && m.viewState == ViewProductList && m.showSearch {
		query = strings.TrimSpace(m.searchInput.Value())
	}
	var cmds []tea.Cmd
	if a.product != product {
		a.product = product
		a.productGeneration++
		if product > 0 && !a.viewed[product] {
			generation := a.productGeneration
			cmds = append(cmds, tea.Tick(analyticsDebounce, func(time.Time) tea.Msg { return analyticsTickMsg{"product", generation} }))
		}
	}
	if a.query != query {
		a.query = query
		a.searchGeneration++
		if query != "" && query != a.lastQuery {
			generation := a.searchGeneration
			cmds = append(cmds, tea.Tick(analyticsDebounce, func(time.Time) tea.Msg { return analyticsTickMsg{"search", generation} }))
		}
	}
	if tick, ok := msg.(analyticsTickMsg); ok {
		switch tick.kind {
		case "product":
			if product > 0 && tick.generation == a.productGeneration && !a.viewed[product] {
				a.viewed[product] = true
				m.event("view_item", map[string]any{"product_id": product})
			}
		case "search":
			if query != "" && tick.generation == a.searchGeneration {
				m.trackSearch()
			}
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) trackSearch() {
	query := strings.TrimSpace(m.searchInput.Value())
	if m.analyticsVisible() && m.viewState == ViewProductList && query != "" && query != m.analytics.lastQuery {
		m.analytics.lastQuery = query
		m.event("search", map[string]any{"query_length": utf8.RuneCountInString(query), "results_count": len(m.productList.Items())})
	}
}
