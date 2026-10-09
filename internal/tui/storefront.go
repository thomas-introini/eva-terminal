package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/thomas/eva-terminal-go/internal/cache"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
	"github.com/thomas/eva-terminal-go/internal/storefront"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

type catalogTickMsg struct{}
type storefrontMsg struct{ state storefront.SessionState }
type paymentPollMsg struct{}
type preparedMsg struct {
	quote       storeapi.Quote
	err         error
	fromAddress bool
}
type shippingCompleteMsg struct{ err error }
type couponCompleteMsg struct{ err error }
type paymentActionMsg struct{ err error }
type catalogErrorMsg struct{ err error }
type cartSyncMsg struct{ err error }

func NewStorefrontModel(ctx context.Context, catalog *storefront.Catalog, shopper *storefront.Session, enabled bool) Model {
	m := NewModel(nil, cache.New[ProductListCacheKey, []woo.Product](time.Minute), cache.New[int, []woo.Variation](time.Minute))
	m.ctx, m.catalog, m.shopper, m.checkoutEnabled = ctx, catalog, shopper, enabled
	m.splash = true
	m.changes = shopper.Subscribe(ctx)
	m.applyShopper(shopper.Snapshot())
	if m.sessionState.Attempt != nil && m.sessionState.Attempt.Active() {
		m.viewState = ViewOrderConfirmation
	}
	return m
}

type errorModel string

func NewErrorModel(message string) tea.Model             { return errorModel(message) }
func (m errorModel) Init() tea.Cmd                       { return nil }
func (m errorModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, tea.Quit }
func (m errorModel) View() tea.View                      { return tea.NewView(string(m)) }

func (m Model) waitShopper() tea.Cmd {
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
			return nil
		case state, ok := <-m.changes:
			if ok {
				return storefrontMsg{state}
			}
			return nil
		}
	}
}

func (m *Model) applyShopper(st storefront.SessionState) {
	m.sessionState = st
	if !m.addressDraft {
		a := st.Cart.BillingAddress
		if st.Billing != nil {
			a = *st.Billing
		}
		*m.customerInfo = CustomerInfo{FirstName: a.FirstName, LastName: a.LastName, Email: a.Email, Address: a.Address1, City: a.City, State: a.State, Phone: a.Phone, Postcode: a.Postcode, Country: a.Country}
	}
	selected := m.localCart.SelectedIdx
	m.localCart.Items = nil
	for _, item := range st.Desired {
		productID := item.ProductID
		if productID == 0 {
			productID = item.ID
		}
		m.localCart.Items = append(m.localCart.Items, LocalCartItem{ProductID: productID, VariationID: item.VariationID, Name: item.Name, Quantity: item.Quantity, GrindSize: item.Grind})
	}
	m.localCart.SelectedIdx = selected
	if selected >= len(st.Desired) {
		m.localCart.SelectedIdx = max(0, len(st.Desired)-1)
	}
	if m.customerInfo.Country == "" {
		m.customerInfo.Country = "IT"
	}
	if st.Attempt != nil && !st.Attempt.Active() {
		m.confirmCancel = false
	}
}

func (m Model) refreshCatalog() tea.Cmd {
	return func() tea.Msg {
		err := m.catalog.Refresh(m.ctx)
		if err != nil {
			return catalogErrorMsg{err}
		}
		return productsLoadedMsg{mapStoreProductsToWoo(m.catalog.Products("", false))}
	}
}

func storeStock(p storeapi.Product) string {
	if p.IsInStock {
		return "instock"
	}
	if p.StockStatus != "" {
		return p.StockStatus
	}
	return "outofstock"
}

func storeRange(p storeapi.ProductPrices) string {
	if p.PriceRange == nil {
		return ""
	}
	return storeapi.FormatMinor(p.PriceRange.MinAmount, p.MinorUnit()) + "–" + storeapi.FormatMinor(p.PriceRange.MaxAmount, p.MinorUnit())
}

func (m Model) address() storeapi.CustomerAddress {
	info := *m.customerInfo
	country := strings.ToUpper(strings.TrimSpace(info.Country))
	if country == "" {
		country = "IT"
	}
	return storeapi.CustomerAddress{FirstName: info.FirstName, LastName: info.LastName, Email: info.Email, Address1: info.Address, City: info.City, State: info.State, Phone: info.Phone, Postcode: info.Postcode, Country: country}
}

func (m Model) prepareReview() tea.Cmd {
	address := m.address()
	return func() tea.Msg {
		if err := m.shopper.Address(m.ctx, address, address); err != nil {
			return preparedMsg{err: err, fromAddress: true}
		}
		state := m.shopper.Snapshot()
		for _, pack := range state.Cart.ShippingRates {
			if len(pack.ShippingRates) > 1 {
				return preparedMsg{fromAddress: true}
			}
		}
		if err := m.shopper.Prepare(m.ctx); err != nil {
			return preparedMsg{err: err, fromAddress: true}
		}
		quote, err := storefront.QuoteFromCart(m.shopper.Snapshot().Cart)
		return preparedMsg{quote: quote, err: err, fromAddress: true}
	}
}

func (m Model) prepareQuote() tea.Cmd {
	return func() tea.Msg {
		if err := m.shopper.Prepare(m.ctx); err != nil {
			return preparedMsg{err: err}
		}
		quote, err := storefront.QuoteFromCart(m.shopper.Snapshot().Cart)
		return preparedMsg{quote: quote, err: err}
	}
}

func (m Model) submitCheckout() tea.Cmd {
	accepted, address := m.quote, m.address()
	return func() tea.Msg {
		if !m.checkoutEnabled {
			return errMsg{fmt.Errorf("new checkouts are disabled; existing payments remain available")}
		}
		err := m.shopper.Checkout(m.ctx, accepted, storeapi.CheckoutRequest{BillingAddress: &address, ShippingAddress: &address}, m.analytics.connection)
		if err != nil {
			return errMsg{err}
		}
		return checkoutSubmittedMsg{}
	}
}

type checkoutSubmittedMsg struct{}

func (m Model) pollPayment() tea.Cmd {
	step := min(m.pollStep, 3)
	delay := []time.Duration{2, 4, 8, 10}[step] * time.Second
	return func() tea.Msg {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-m.ctx.Done():
			return nil
		case <-timer.C:
		}
		_ = m.shopper.Recover(m.ctx)
		return paymentPollMsg{}
	}
}

func (m Model) cancelPayment() tea.Cmd {
	return func() tea.Msg { return paymentActionMsg{m.shopper.Cancel(m.ctx)} }
}

func (m Model) recheckPayment() tea.Cmd {
	return func() tea.Msg { return paymentActionMsg{m.shopper.Recover(m.ctx)} }
}

func (m Model) submitCoupon(code string, remove bool) tea.Cmd {
	return func() tea.Msg {
		return couponCompleteMsg{m.shopper.Coupon(m.ctx, code, remove, m.analytics.connection)}
	}
}

func (m Model) hasDeliveryChoice() bool {
	for _, pack := range m.sessionState.Cart.ShippingRates {
		if len(pack.ShippingRates) > 1 {
			return true
		}
	}
	return false
}

func (m *Model) initShipping() tea.Cmd {
	fields := []huh.Field{}
	for _, pack := range m.sessionState.Cart.ShippingRates {
		options := []huh.Option[string]{}
		selected := ""
		for _, rate := range pack.ShippingRates {
			if rate.Selected {
				selected = rate.RateID
			}
			options = append(options, huh.NewOption(rate.Name+" "+m.money(rate.Price), rate.RateID))
		}
		if len(options) > 0 {
			fields = append(fields, huh.NewSelect[string]().Key(fmt.Sprint(pack.PackageID)).Title(pack.Name).Options(options...).Value(&selected))
		}
	}
	if len(fields) == 0 {
		m.err = fmt.Errorf("enter a complete address to load shipping rates")
		return nil
	}
	if m.viewState != ViewShipping {
		m.shippingReturn = m.viewState
	}
	groups := make([]*huh.Group, 0, len(fields))
	for _, field := range fields {
		groups = append(groups, huh.NewGroup(field))
	}
	m.shippingForm = huh.NewForm(groups...)
	m.styleForm(m.shippingForm)
	m.viewState = ViewShipping
	return m.shippingForm.Init()
}

func (m Model) updateShipping(msg tea.Msg) (Model, tea.Cmd) {
	if m.shippingForm == nil || m.shippingBusy {
		return m, nil
	}
	form, cmd := m.shippingForm.Update(msg)
	m.shippingForm = form.(*huh.Form)
	if m.shippingForm.State == huh.StateCompleted {
		m.shippingBusy, m.err = true, nil
		rates := []storeapi.SelectShippingRateRequest{}
		for _, pack := range m.sessionState.Cart.ShippingRates {
			if value := m.shippingForm.GetString(fmt.Sprint(pack.PackageID)); value != "" {
				rates = append(rates, storeapi.SelectShippingRateRequest{PackageID: pack.PackageID, RateID: value})
			}
		}
		return m, func() tea.Msg {
			for _, rate := range rates {
				if err := m.shopper.Shipping(m.ctx, rate.PackageID, rate.RateID, m.analytics.connection); err != nil {
					return shippingCompleteMsg{err}
				}
			}
			return shippingCompleteMsg{}
		}
	}
	return m, cmd
}

func (m Model) money(raw string) string {
	t := m.sessionState.Cart.Totals
	return t.CurrencyCode + " " + storeapi.FormatMinor(raw, t.CurrencyMinorUnit)
}

// Cart content scrolls independently of the totals and checkout controls.
func (m Model) cartContent(review bool) string {
	var b strings.Builder
	if review {
		a := m.sessionState.Cart.ShippingAddress
		fmt.Fprintf(&b, "Deliver to: %s %s\n%s\n%s %s %s %s\n", a.FirstName, a.LastName, a.Address1, a.Postcode, a.City, a.State, a.Country)
		fmt.Fprintf(&b, "Contact: %s · %s\n", m.sessionState.Cart.BillingAddress.Email, m.sessionState.Cart.BillingAddress.Phone)
	}
	if m.localCart.IsEmpty() {
		b.WriteString("Your cart is empty. Press s to browse coffee.\n")
	}
	for i, item := range m.localCart.Items {
		prefix := "  "
		if i == m.localCart.SelectedIdx && !review {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%s%s\n  %s × %d", prefix, item.Name, item.GrindSize, item.Quantity)
		id := item.ProductID
		if item.VariationID != 0 {
			id = item.VariationID
		}
		for _, confirmed := range m.sessionState.Cart.Items {
			var ext struct {
				Grind string `json:"grind"`
			}
			_ = json.Unmarshal(confirmed.Extensions["eva_terminal"], &ext)
			if confirmed.ID == id && ext.Grind == item.GrindSize && confirmed.Quantity == item.Quantity {
				if confirmed.Totals.LineTotal != "" {
					fmt.Fprintf(&b, " · %s", m.money(confirmed.Totals.LineTotal))
				}
				break
			}
		}
		b.WriteString("\n")
	}
	for _, coupon := range m.sessionState.Cart.Coupons {
		fmt.Fprintf(&b, "Coupon: %s\n", coupon.Code)
	}
	for _, pack := range m.sessionState.Cart.ShippingRates {
		for _, rate := range pack.ShippingRates {
			if rate.Selected {
				fmt.Fprintf(&b, "Delivery: %s\n", rate.Name)
			}
		}
	}
	for _, issue := range m.sessionState.Cart.Errors {
		fmt.Fprintf(&b, "Attention: %s\n", StripHTML(issue.Message))
	}
	if review {
		b.WriteString("Pay by card or eligible Apple Pay / Google Pay.\nConfirm this total to create your payment link.")
	}
	return b.String()
}

func (m Model) paymentSummary() string {
	a := m.sessionState.Attempt
	if a == nil {
		return "No active payment"
	}
	label := "Checking payment"
	switch a.PaymentState {
	case "paid":
		label = "Paid · payment confirmed"
	case "cancelled":
		label = "Cancelled · cart available"
	case "expired":
		label = "Link expired · checkout again"
	case "failed":
		label = "Payment failed · checkout again"
	case "submitting":
		label = "Creating payment link"
	case "resolving":
		label = "Confirming checkout outcome"
	case "pending", "unpaid", "pending_payment", "awaiting_payment":
		label = "Awaiting payment confirmation"
	}
	order := fmt.Sprintf("Order #%d", a.OrderID)
	if a.OrderID == 0 {
		order = "Order being confirmed"
	}
	remaining := ""
	if a.Active() && a.ExpiresAt > 0 {
		remaining = "Time remaining: " + max(time.Duration(0), time.Until(time.Unix(a.ExpiresAt, 0))).Round(time.Second).String()
	}
	return fmt.Sprintf("%s · %s %s\n%s\n%s", order, a.Currency, storeapi.FormatMinor(a.Total, 2), label, remaining)
}

func (m Model) paymentContent() string {
	a := m.sessionState.Attempt
	if a == nil {
		return "Press Esc to continue shopping."
	}
	if m.confirmCancel {
		return "Cancel this unpaid payment?\nThe payment link will close and the cart will unlock.\nIf payment has already completed, it stays confirmed.\n\nEnter confirms cancellation. Esc keeps the payment open."
	}
	if a.PaymentState == "paid" {
		return "Payment confirmed by the store. Thank you!\nPress Enter to continue shopping."
	}
	if !a.Active() {
		return "Your saved cart is available. Press c to review it,\nor Enter to continue shopping."
	}
	if a.PaymentURL == "" {
		return "The store is confirming your checkout.\nUse r to check again. You can also browse and return with o."
	}
	text := "Open this full link in your browser:\n" + a.PaymentURL
	if a.ExpiresAt > 0 {
		remaining := max(time.Duration(0), time.Until(time.Unix(a.ExpiresAt, 0)))
		text += fmt.Sprintf("\nTime remaining: %s\n", remaining.Round(time.Second))
	}
	return text + "\nWaiting for store confirmation. Browser returns do not confirm payment.\nUse y to request copying the link, or copy it manually.\nUse r to recheck payment status."
}
