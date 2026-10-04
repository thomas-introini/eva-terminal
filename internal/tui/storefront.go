package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
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
	quote storeapi.Quote
	err   error
}
type shippingCompleteMsg struct{}

func NewStorefrontModel(ctx context.Context, catalog *storefront.Catalog, shopper *storefront.Session, enabled bool) Model {
	m := NewModel(nil, cache.New[ProductListCacheKey, []woo.Product](time.Minute), cache.New[int, []woo.Variation](time.Minute))
	m.ctx, m.catalog, m.shopper, m.checkoutEnabled = ctx, catalog, shopper, enabled
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
	if m.viewState != ViewAddress {
		a := st.Cart.BillingAddress
		if st.Billing != nil {
			a = *st.Billing
		}
		*m.customerInfo = CustomerInfo{FirstName: a.FirstName, LastName: a.LastName, Email: a.Email, Address: a.Address1, City: a.City, State: a.State, Phone: a.Phone, Postcode: a.Postcode, Country: a.Country}
	}
	selected := m.localCart.SelectedIdx
	m.localCart.Items = nil
	for _, item := range st.Desired {
		m.localCart.Items = append(m.localCart.Items, LocalCartItem{ProductID: item.ID, Name: item.Name, Quantity: item.Quantity, GrindSize: item.Grind})
	}
	m.localCart.SelectedIdx = selected
	if selected >= len(st.Desired) {
		m.localCart.SelectedIdx = max(0, len(st.Desired)-1)
	}
	if st.Error != "" {
		m.err = fmt.Errorf("%s", st.Error)
	}
}

func (m Model) refreshCatalog() tea.Cmd {
	return func() tea.Msg {
		err := m.catalog.Refresh(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return productsLoadedMsg{mapStoreProductsToWoo(m.catalog.Products(m.searchInput.Value(), m.inStockOnly))}
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
	country := strings.ToUpper(info.Country)
	if country == "" {
		country = "IT"
	}
	return storeapi.CustomerAddress{FirstName: info.FirstName, LastName: info.LastName, Email: info.Email, Address1: info.Address, City: info.City, State: info.State, Phone: info.Phone, Postcode: info.Postcode, Country: country}
}

func (m Model) prepareReview() tea.Cmd {
	address := m.address()
	return func() tea.Msg {
		if err := m.shopper.Address(m.ctx, address, address); err != nil {
			return preparedMsg{err: err}
		}
		if err := m.shopper.Prepare(m.ctx); err != nil {
			return preparedMsg{err: err}
		}
		quote, err := storefront.QuoteFromCart(m.shopper.Snapshot().Cart)
		return preparedMsg{quote, err}
	}
}

func (m Model) submitCheckout() tea.Cmd {
	accepted, address := m.quote, m.address()
	return func() tea.Msg {
		if !m.checkoutEnabled {
			return errMsg{fmt.Errorf("new checkouts are disabled; existing payments remain available")}
		}
		err := m.shopper.Checkout(m.ctx, accepted, storeapi.CheckoutRequest{BillingAddress: &address, ShippingAddress: &address})
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
	return func() tea.Msg {
		if err := m.shopper.Cancel(m.ctx); err != nil {
			return errMsg{err}
		}
		return preparedMsg{}
	}
}

func (m Model) handleSyncedCartKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.couponMode != "" {
		switch key {
		case "esc":
			m.couponMode = ""
			return m, nil
		case "enter":
			code, remove := m.cartInput.Value(), m.couponMode == "remove"
			m.couponMode = ""
			return m, func() tea.Msg {
				if err := m.shopper.Coupon(m.ctx, code, remove); err != nil {
					return errMsg{err}
				}
				return preparedMsg{}
			}
		}
		var cmd tea.Cmd
		m.cartInput, cmd = m.cartInput.Update(msg)
		return m, cmd
	}
	switch key {
	case "esc", "backspace", "s":
		m.viewState = ViewProductList
	case "up", "k":
		m.localCart.MoveUp()
	case "down", "j":
		m.localCart.MoveDown()
	case "+", "=", "-", "d", "delete":
		if item := m.localCart.GetSelectedItem(); item != nil {
			delta := 0
			switch key {
			case "+", "=":
				delta = 1
			case "-":
				delta = -1
			}
			if delta != 0 {
				m.err = m.shopper.AdjustQuantity(item.ProductID, item.GrindSize, delta)
			} else {
				m.err = m.shopper.SetQuantity(item.ProductID, item.GrindSize, 0)
			}
			m.applyShopper(m.shopper.Snapshot())
		}
	case "o":
		if a := m.sessionState.Attempt; a != nil && a.Active() {
			m.viewState = ViewOrderConfirmation
			return m, nil
		}
		if len(m.sessionState.Desired) > 0 {
			m.viewState = ViewAddress
			return m, m.initAddressForm()
		}
	case "c", "u":
		m.couponMode = "apply"
		if key == "u" {
			m.couponMode = "remove"
		}
		m.cartInput = textinput.New()
		m.cartInput.Placeholder = "Coupon code"
		return m, m.cartInput.Focus()
	case "h":
		return m, m.initShipping()
	case "x":
		return m, m.cancelPayment()
	}
	return m, nil
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
	m.shippingForm = huh.NewForm(huh.NewGroup(fields...))
	m.viewState = ViewShipping
	return m.shippingForm.Init()
}

func (m Model) updateShipping(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "esc" {
		m.viewState = ViewCart
		return m, nil
	}
	form, cmd := m.shippingForm.Update(msg)
	m.shippingForm = form.(*huh.Form)
	if m.shippingForm.State == huh.StateCompleted {
		rates := []storeapi.SelectShippingRateRequest{}
		for _, pack := range m.sessionState.Cart.ShippingRates {
			if value := m.shippingForm.GetString(fmt.Sprint(pack.PackageID)); value != "" {
				rates = append(rates, storeapi.SelectShippingRateRequest{PackageID: pack.PackageID, RateID: value})
			}
		}
		return m, func() tea.Msg {
			for _, rate := range rates {
				if err := m.shopper.Shipping(m.ctx, rate.PackageID, rate.RateID); err != nil {
					return errMsg{err}
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

func (m Model) viewWooCart(review bool) string {
	var b strings.Builder
	if review {
		b.WriteString("Review WooCommerce quote\n\n")
	} else {
		b.WriteString("Shopping cart\n\n")
	}
	if len(m.sessionState.Desired) == 0 {
		b.WriteString("Your cart is empty\n")
	}
	if review {
		a := m.sessionState.Cart.ShippingAddress
		fmt.Fprintf(&b, "Deliver to: %s %s\n%s\n%s %s %s %s\n", a.FirstName, a.LastName, a.Address1, a.Postcode, a.City, a.State, a.Country)
		fmt.Fprintf(&b, "Billing contact: %s · %s\n\n", m.sessionState.Cart.BillingAddress.Email, m.sessionState.Cart.BillingAddress.Phone)
	}
	for i, item := range m.sessionState.Desired {
		prefix := "  "
		if i == m.localCart.SelectedIdx {
			prefix = "▸ "
		}
		fmt.Fprintf(&b, "%s%s · %s × %d\n", prefix, item.Name, item.Grind, item.Quantity)
		for _, confirmed := range m.sessionState.Cart.Items {
			if confirmed.ID == item.ID && confirmed.Quantity == item.Quantity {
				if confirmed.Totals.LineTotal != "" {
					fmt.Fprintf(&b, "    Woo line total: %s\n", m.money(confirmed.Totals.LineTotal))
				}
				break
			}
		}
	}
	if m.sessionState.Pending {
		b.WriteString("\nSynchronizing changes with WooCommerce…\n")
	}
	for _, issue := range m.sessionState.Cart.Errors {
		fmt.Fprintf(&b, "\n%s\n", StripHTML(issue.Message))
	}
	t := m.sessionState.Cart.Totals
	for _, row := range []struct{ name, amount string }{{"Items", t.TotalItems}, {"Discount", t.TotalDiscount}, {"Shipping", t.TotalShipping}, {"Fees", t.TotalFees}, {"Tax", t.TotalTax}, {"Total", t.TotalPrice}} {
		if row.amount != "" {
			fmt.Fprintf(&b, "%s: %s\n", row.name, m.money(row.amount))
		}
	}
	for _, coupon := range m.sessionState.Cart.Coupons {
		fmt.Fprintf(&b, "Coupon: %s\n", coupon.Code)
	}
	for _, pack := range m.sessionState.Cart.ShippingRates {
		for _, rate := range pack.ShippingRates {
			if rate.Selected {
				fmt.Fprintf(&b, "%s: %s\n", pack.Name, rate.Name)
			}
		}
	}
	if m.preparing {
		b.WriteString("\nValidating address and preparing quote…")
	}
	if m.creatingOrder {
		b.WriteString("\nSubmitting checkout…")
	}
	if m.err != nil {
		fmt.Fprintf(&b, "\n%s\n", m.err)
	}
	if m.couponMode != "" {
		fmt.Fprintf(&b, "\n%s coupon: %s", m.couponMode, m.cartInput.View())
	}
	if review {
		b.WriteString("\nCard / eligible Apple Pay or Google Pay on Stripe\nenter confirm • esc edit address • h shipping • c cart")
	} else {
		b.WriteString("\n↑/↓ select • +/- quantity • d delete • c coupon • u remove coupon • h shipping • o checkout • x cancel payment • s browse")
	}
	return m.styles.Box.Render(b.String())
}

func (m Model) viewPayment() string {
	a := m.sessionState.Attempt
	if a == nil {
		return "No active payment"
	}
	text := fmt.Sprintf("Order #%d\nPayment: %s\n%s %s\n", a.OrderID, a.PaymentState, a.Currency, storeapi.FormatMinor(a.Total, 2))
	if a.PaymentURL != "" && a.Active() {
		text += "\nOpen in your browser:\n" + a.PaymentURL + "\nExpires: " + time.Unix(a.ExpiresAt, 0).Format(time.RFC3339) + "\n"
	}
	if a.PaymentState == "paid" {
		text += "\nPayment confirmed by WooCommerce.\n"
	}
	if m.err != nil {
		text += "\n" + m.err.Error() + "\n"
	}
	return m.styles.Box.Render(text + "\nenter browse • x cancel unpaid payment")
}
