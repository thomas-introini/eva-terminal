package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
)

// These bindings drive both dispatch and the footer/expanded help.
type uiBinding struct {
	action    string
	keys      []string
	label     string
	essential bool
}

func (m Model) inputActive() bool {
	return m.showSearch || m.couponMode != "" || m.viewState == ViewAddress || m.viewState == ViewShipping
}
func (m Model) tooSmall() bool { return m.width > 0 && (m.width < 60 || m.height < 18) }
func (m Model) paymentActive() bool {
	return m.sessionState.Attempt != nil && m.sessionState.Attempt.Active()
}
func (m Model) operationBusy() bool {
	return m.preparing || m.creatingOrder || m.shippingBusy || m.couponBusy || m.cancelling
}
func (m Model) cartEditable() bool { return !m.paymentActive() && !m.operationBusy() }

func (m Model) checkoutReason() string {
	if m.paymentActive() {
		return "Cart locked · press o for pending payment"
	}
	if !m.checkoutEnabled && m.shopper != nil {
		return "Checkout unavailable · continue shopping"
	}
	if m.operationBusy() {
		return "Checkout busy · please wait"
	}
	if m.localCart.IsEmpty() {
		return "Add coffee to start checkout"
	}
	if m.sessionState.Pending {
		return "Totals updating · wait for synchronization"
	}
	if m.sessionState.Error != "" {
		return "Cart needs recovery · press r to retry"
	}
	if m.shopper != nil && m.sessionState.Cart.NeedsPayment && !slices.Contains(m.sessionState.Cart.PaymentMethods, storeapi.GatewayID) {
		return "Payment unavailable · press r to retry"
	}
	if len(m.sessionState.Cart.Errors) != 0 {
		return "Resolve the cart issues before checkout"
	}
	if m.viewState == ViewReview && m.shopper != nil {
		if m.quote.Fingerprint == "" {
			return "Review unavailable · press r to retry"
		}
	}
	return ""
}
func (m Model) purchaseReason() string {
	if m.selectedProduct == nil {
		return "No product selected"
	}
	p := m.selectedProduct
	if !p.IsInStock() {
		return "Unavailable: out of stock"
	}
	if p.Purchasable != nil && !*p.Purchasable {
		return "Unavailable: this product cannot be purchased"
	}
	if m.paymentActive() {
		return "Cart locked · press o for pending payment"
	}
	if m.operationBusy() {
		return "Cart busy · please wait"
	}
	if p.IsVariable() {
		if m.loadingVariations {
			return "Loading available sizes…"
		}
		if len(m.productVariations) == 0 {
			return "No available sizes · press r to retry"
		}
		if m.optionsErr != nil {
			return "Options unavailable · press r to retry"
		}
		if m.selectedVariation == nil {
			if !slices.ContainsFunc(m.sizeOptions(), variationAvailable) {
				return "Unavailable: no purchasable sizes"
			}
			return "Choose an available size"
		}
		if !m.selectedVariation.IsInStock() {
			return "Unavailable: size is out of stock"
		}
		if !variationAvailable(*m.selectedVariation) {
			return "Unavailable: size cannot be purchased"
		}
	}
	grinds := m.grindOptions()
	if (len(grinds) > 0 || m.selectedGrindSize != "") && !slices.Contains(grinds, m.selectedGrindSize) {
		return "Choose an available grind"
	}
	if m.selectedQuantity < 1 {
		return "Choose a quantity of at least one"
	}

	return "Available to purchase"
}

func (m Model) bindings() []uiBinding {
	var b []uiBinding
	add := func(action, label string, essential bool, keys ...string) {
		b = append(b, uiBinding{action, keys, label, essential})
	}
	add("quit", "ctrl+c quit", m.inputActive(), "ctrl+c")
	if m.tooSmall() {
		add("quit", "q quit", true, "q")
		return b
	}
	if m.showHelp {
		add("help", "esc close help", true, "esc", "?")
		add("scrollUp", "↑/↓ scroll", false, "up", "k")
		add("scrollDown", "↓ scroll", false, "down", "j")
		add("pageUp", "pgup page up", false, "pgup")
		add("pageDown", "pgdown page down", false, "pgdown")
		add("home", "home first", false, "home")
		add("end", "end last", false, "end")
		if !m.inputActive() {
			add("quit", "q quit", true, "q")
		}
		return b
	}
	if m.confirmCancel {
		add("cancelConfirm", "enter cancel payment", true, "enter")
		add("cancelBack", "esc keep payment", true, "esc")
		add("help", "? help", true, "?")
		add("quit", "q quit", true, "q")
		return b
	}
	add("help", "? help", true, "?")
	if !m.inputActive() {
		add("quit", "q quit", true, "q")
		if m.viewState != ViewCart {
			add("cart", "c cart", m.viewState == ViewProductList, "c")
		}
		if m.viewState != ViewProductList {
			add("browse", "s shop", true, "s")
		}
		if m.paymentActive() && m.viewState != ViewOrderConfirmation {
			add("payment", "o payment", true, "o")
		}
	}
	if m.showSearch {
		add("searchDone", "enter keep search", true, "enter")
		add("searchClear", "esc clear search", true, "esc")
		return b
	}
	if m.couponMode != "" {
		if !m.couponBusy {
			add("couponSubmit", "enter submit coupon", true, "enter")
			add("couponBack", "esc back", true, "esc")
		}
		return b
	}
	switch m.viewState {
	case ViewProductList:
		for _, binding := range []key.Binding{m.productList.KeyMap.CursorUp, m.productList.KeyMap.CursorDown, m.productList.KeyMap.GoToStart, m.productList.KeyMap.GoToEnd} {
			if binding.Enabled() {
				label, essential := binding.Help().Key+" "+binding.Help().Desc, false
				if slices.Contains(binding.Keys(), "up") {
					label, essential = "↑/↓ coffees", true
				}
				add("component", label, essential, binding.Keys()...)
			}
		}
		if m.selectedProduct != nil {
			add("optionNext", "tab options", true, "tab")
			add("optionPrevious", "shift+tab previous option", false, "shift+tab")
			add("optionLeft", "←/→ change option", true, "left")
			add("optionRight", "→ next option", false, "right")
			add("draftIncrease", "+ quantity", false, "+", "=")
			add("draftDecrease", "- quantity", false, "-")
			add("pageUp", "pgup/pgdown description", false, "pgup")
			add("pageDown", "pgdown description", false, "pgdown")
			if m.purchaseReason() == "Available to purchase" {
				add("add", "enter add to cart", true, "enter", "a")
			}
		}
		add("search", "/ search", true, "/")
		add("filter", "f stock filter", false, "f")
		if !m.loadingProducts {
			add("refresh", "r refresh catalog/options", false, "r")
		}
	case ViewCart:
		add("back", "esc shop", false, "esc")
		if m.checkoutReason() == "" && !m.paymentActive() {
			add("checkout", "enter checkout", true, "enter", "o")
		}
		add("up", "↑/↓ select item", false, "up", "k")
		add("down", "↓ next item", false, "down", "j")
		if m.cartEditable() {
			add("increase", "+ quantity", false, "+", "=")
			add("decrease", "- quantity", false, "-")
			add("delete", "d delete item", false, "d", "delete")
			if m.shopper != nil {
				add("couponApply", "p apply coupon", false, "p")
				add("couponRemove", "u remove coupon", false, "u")
				if m.hasDeliveryChoice() {
					add("shipping", "h change delivery", false, "h")
				}
				add("sync", "r retry synchronization", false, "r")
			}
		}
	case ViewAddress:
		if !m.preparing {
			add("back", "esc back", true, "esc")
			add("component", "enter next", true, "enter")
			add("component", "tab next · shift+tab previous", false, "tab", "shift+tab")
		}
	case ViewShipping:
		if !m.shippingBusy {
			add("back", "esc back", true, "esc")
			add("component", "enter choose delivery", true, "enter")
			add("component", "tab next · shift+tab previous", false, "tab", "shift+tab")
			add("component", "↑/↓ choose option", false, "up", "down", "k", "j", "home", "end", "ctrl+u", "ctrl+d")
		}
	case ViewReview:
		if !m.operationBusy() {
			add("back", "esc edit address", true, "esc")
			if m.shopper != nil && m.checkoutReason() == "" {
				add("confirm", "enter confirm & pay", true, "enter", "o")
			}
			if m.shopper != nil {
				add("retryReview", "r refresh review", false, "r")
			}
			if m.hasDeliveryChoice() && m.cartEditable() {
				add("shipping", "h change delivery", false, "h")
			}
		}
	case ViewOrderConfirmation:
		add("back", "esc shop", false, "esc")
		if !m.paymentActive() {
			add("browse", "enter shop", true, "enter")
		}
		if a := m.sessionState.Attempt; a != nil {
			if a.PaymentURL != "" {
				add("copy", "y copy URL", true, "y")
			}
			if !m.checking && !m.cancelling {
				add("recheck", "r check status", true, "r")
			}
			if a.Active() && !m.cancelling && !m.checking {
				add("cancelAsk", "x cancel payment…", false, "x")
			}
		}
	}
	if m.viewState == ViewCart && m.paymentActive() && !m.cancelling && !m.checking {
		add("cancelAsk", "x cancel payment…", false, "x")
	}
	if !m.inputActive() && m.viewState != ViewProductList {
		add("pageUp", "pgup/pgdown scroll", false, "pgup")
		add("pageDown", "pgdown scroll", false, "pgdown")
		add("home", "home first", false, "home")
		add("end", "end last", false, "end")
		if m.viewState != ViewCart {
			add("scrollUp", "↑/↓ scroll", false, "up", "k")
			add("scrollDown", "↓ scroll", false, "down", "j")
		}
	}
	return b
}

// Panel geometry is shared by every screen and leaves balanced outer margins.
func (m Model) panelSize() (int, int) {
	margin := 2
	if m.height >= 24 {
		margin = 6 // Three rows above the centered panel for the compact logo.
	}
	return min(100, max(60, m.width-2)), min(28, max(18, m.height-margin))
}

func (m Model) contentSize() (int, int) {
	pw, ph := m.panelSize()
	fixed := 0
	if m.viewState == ViewCart || m.viewState == ViewReview {
		fixed = lipgloss.Height(m.fixedSummary())
	}
	if m.viewState == ViewOrderConfirmation {
		fixed = 3
	}
	return max(1, pw-m.styles.App.GetHorizontalFrameSize()), max(1, ph-9-fixed)
}
func (m *Model) sizeComponents() {
	w, h := m.contentSize()
	if m.width >= 80 {
		m.productList.SetSize(min(30, w/3), h)
	} else {
		m.productList.SetSize(w, 3)
	}
	m.searchInput.SetWidth(max(1, w-ansi.StringWidth(m.searchPrefix())-ansi.StringWidth(m.searchInput.Prompt)-1))
	m.cartInput.SetWidth(max(1, w-ansi.StringWidth(m.couponMode+" coupon: ")-ansi.StringWidth(m.cartInput.Prompt)-1))
	for _, form := range []*huh.Form{m.addressForm, m.shippingForm} {
		if form != nil {
			form.WithWidth(w).WithHeight(h)
		}
	}
}
func (m *Model) styleForm(form *huh.Form) {
	form.WithShowHelp(false).WithShowErrors(true).WithTheme(m.formTheme())
	keys := huh.NewDefaultKeyMap()
	keys.Select.Filter.SetKeys()
	form.WithKeyMap(keys)
	w, h := m.contentSize()
	if m.width == 0 {
		w, h = 78, 16
	}
	form.WithWidth(w).WithHeight(h)
}
func (m *Model) applyTheme() {
	m.styles = coffeeStyles(m.dark, m.noColor, m.profile)
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.Styles.NormalTitle = lipgloss.NewStyle().PaddingLeft(2)
	delegate.Styles.NormalDesc = m.styles.Subtle.PaddingLeft(2)
	delegate.Styles.SelectedTitle = m.styles.Selection.PaddingLeft(1).Border(lipgloss.NormalBorder(), false, false, false, true)
	delegate.Styles.SelectedDesc = m.styles.Subtle.Border(lipgloss.NormalBorder(), false, false, false, true).PaddingLeft(1)
	m.productList.SetDelegate(delegate)
	m.listSpinner.Style = m.styles.Highlight
	inputStyles := textinput.DefaultStyles(m.dark)
	inputStyles.Focused.Text, inputStyles.Blurred.Text = lipgloss.NewStyle(), lipgloss.NewStyle()
	inputStyles.Focused.Prompt, inputStyles.Blurred.Prompt = m.styles.Highlight, m.styles.Subtle
	inputStyles.Focused.Placeholder, inputStyles.Blurred.Placeholder = m.styles.Subtle, m.styles.Subtle
	inputStyles.Cursor.Color = nil
	m.searchInput.SetStyles(inputStyles)
	m.cartInput.SetStyles(inputStyles)
	for _, form := range []*huh.Form{m.addressForm, m.shippingForm} {
		if form != nil {
			form.WithTheme(m.formTheme())
		}
	}
	m.sizeComponents()
}

func wrappedLines(text string, width int) []string {
	return strings.Split(ansi.Hardwrap(ansi.Wrap(text, max(1, width), ""), max(1, width), true), "\n")
}
func fitLines(text string, width, height int) string {
	lines := wrappedLines(text, width)
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
func oneLine(text string, width int) string {
	return ansi.Truncate(strings.ReplaceAll(text, "\n", " · "), width, "…")
}

func (m Model) title() string {
	titles := []string{"Shop", "Shopping cart", "Address", "Review order", "Payment", "Delivery"}
	return titles[m.viewState]
}
func (m Model) searchPrefix() string {
	filter := "all stock"
	if m.inStockOnly {
		filter = "in stock"
	}
	return fmt.Sprintf("%d results · %s · Search: ", len(m.productList.Items()), filter)
}
func (m Model) contextLine() string {
	switch m.viewState {
	case ViewProductList:
		filter := "all stock"
		if m.inStockOnly {
			filter = "in stock"
		}
		query := "all coffee"
		if m.searchInput.Value() != "" {
			query = fmt.Sprintf("search: %q", m.searchInput.Value())
		}
		if m.showSearch {
			return m.searchPrefix() + m.searchInput.View()
		}
		return fmt.Sprintf("%d results · %s · %s", len(m.productList.Items()), filter, query)
	case ViewAddress, ViewShipping, ViewReview, ViewOrderConfirmation:
		progression := "Address → Delivery → Review → Payment"
		if !m.hasDeliveryChoice() {
			progression = "Address → Review → Payment"
		}
		return progression
	case ViewCart:
		if m.couponMode != "" {
			return m.couponMode + " coupon: " + m.cartInput.View()
		}
		return fmt.Sprintf("%d items · %s", m.localCart.ItemCount(), m.checkoutLabel())
	}
	return ""
}
func (m Model) checkoutLabel() string {
	if reason := m.checkoutReason(); reason != "" {
		return reason
	}
	if m.viewState == ViewReview {
		return "Ready · confirm the current total"
	}
	return "Ready for checkout"
}
func (m Model) feedback() string {
	action := m.notice
	switch {
	case m.cancelling:
		action = "Cancelling payment · checking authoritative status…"
	case m.checking:
		action = "Rechecking payment status…"
	case m.shippingBusy:
		action = "Saving delivery selection…"
	case m.couponBusy:
		action = "Updating coupon…"
	case m.creatingOrder:
		action = "Submitting checkout · please wait…"
	case m.preparing:
		action = "Validating address and updating review…"
	case m.sessionState.Pending:
		action = "Synchronizing cart · totals updating…"
	case m.loadingProducts:
		action = "Refreshing coffee catalog…"
	}
	if m.optionsErr != nil && m.viewState == ViewProductList {
		action = "Options: " + StripHTML(m.optionsErr.Error())
	}
	if m.err != nil {
		action = "Error: " + StripHTML(m.err.Error())
	}
	session := ""
	if m.sessionState.Error != "" {
		session = "Cart: " + StripHTML(m.sessionState.Error) + " · c cart to recover"
	}
	if m.viewState == ViewProductList && m.sessionState.Error == "" {
		if m.catalogErr != nil {
			session = "Refresh failed · saved products · r retry"
		}
		if m.catalog != nil {
			updated, err := m.catalog.Status()
			if err != nil {
				session = "Refresh failed · saved catalog · r retry"
			} else if !updated.IsZero() {
				session = "Catalog updated " + time.Since(updated).Round(time.Second).String() + " ago"
			}
		}
	}
	w, _ := m.contentSize()
	style := m.styles.Subtle
	if m.notice != "" && !m.operationBusy() && !m.sessionState.Pending {
		style = m.styles.Success
	}
	if m.err != nil || (m.optionsErr != nil && m.viewState == ViewProductList) {
		style = m.styles.Error
	}
	return style.Render(oneLine(action, w)) + "\n" + m.styles.Subtle.Render(oneLine(session, w))
}
func (m Model) footer() string {
	// Essential labels precede secondary actions; help holds the complete binding set.
	var labels []string
	for _, b := range m.bindings() {
		if b.essential {
			labels = append(labels, b.label)
		}
	}
	// Give the primary action and navigation first, retaining help/quit at the end.
	var primary, front, tail []string
	for _, label := range labels {
		if label == "? help" || label == "q quit" || label == "ctrl+c quit" {
			tail = append(tail, label)
		} else if strings.HasPrefix(label, "enter ") {
			primary = append(primary, label)
		} else {
			front = append(front, label)
		}
	}
	labels = append(primary, front...)
	if len(tail) != 0 {
		labels = append(labels, strings.Join(tail, " · "))
	}
	w, _ := m.contentSize()
	var lines []string
	line := ""
	for _, label := range labels {
		next := label
		if line != "" {
			next = line + " · " + label
		}
		if ansi.StringWidth(next) > w && line != "" {
			lines = append(lines, line)
			line = label
		} else {
			line = next
		}
	}
	lines = append(lines, line)
	return m.styles.HelpBar.Width(w).Align(lipgloss.Center).Render(fitLines(strings.Join(lines, "\n"), w, 2))
}
func (m Model) bodyText() string {
	switch m.viewState {
	case ViewProductList:
		return m.shopBody()
	case ViewCart:
		return m.cartContent(false)
	case ViewReview:
		return m.cartContent(true)
	case ViewAddress:
		if m.addressForm != nil {
			return m.addressForm.View()
		}
	case ViewShipping:
		if m.shippingForm != nil {
			return m.shippingForm.View()
		}
	case ViewOrderConfirmation:
		return m.paymentContent()
	}
	return ""
}
func (m Model) fixedSummary() string {
	pw, _ := m.panelSize()
	w := max(1, pw-m.styles.App.GetHorizontalFrameSize())
	switch m.viewState {
	case ViewCart, ViewReview:
		total := "Total: awaiting store calculation"
		if raw := m.sessionState.Cart.Totals.TotalPrice; raw != "" {
			total = "Total: " + m.money(raw)
		}
		if m.sessionState.Pending || m.operationBusy() {
			total += " · updating"
		}
		t := m.sessionState.Cart.Totals
		var details []string
		for _, row := range []struct{ name, amount string }{{"Items", t.TotalItems}, {"Discount", t.TotalDiscount}, {"Delivery", t.TotalShipping}, {"Fees", t.TotalFees}, {"Tax", t.TotalTax}} {
			if row.amount != "" {
				details = append(details, row.name+": "+m.money(row.amount))
			}
		}
		summary := m.styles.Highlight.Render(oneLine(total, w)) + "\n" + m.styles.Subtle.Render(oneLine(m.checkoutLabel(), w))
		if len(details) != 0 {
			summary = m.styles.Subtle.Render(strings.Join(wrappedLines(strings.Join(details, " · "), w), "\n")) + "\n" + summary
		}
		return summary
	case ViewOrderConfirmation:
		return m.styles.Highlight.Render(fitLines(m.paymentSummary(), w, 3))
	}
	return ""
}
func (m *Model) moveScroll(action string) {
	w, h := m.contentSize()
	offset := &m.scroll[m.viewState]
	text := m.bodyText()
	if m.viewState == ViewProductList {
		w, h = m.shopDescriptionSize()
		text = m.shopDescription()
	}
	total := len(wrappedLines(text, w))
	if m.showHelp {
		offset = &m.helpScroll
		total = len(wrappedLines(m.helpText(), w))
		_, ph := m.panelSize()
		h = max(1, ph-6)
	}
	switch action {
	case "scrollUp":
		*offset--
	case "scrollDown":
		*offset++
	case "pageUp":
		*offset -= max(1, h-1)
	case "pageDown":
		*offset += max(1, h-1)
	case "home":
		*offset = 0
	case "end":
		*offset = total
	}
	*offset = max(0, min(*offset, max(0, total-h+1)))
}
func (m *Model) revealCartSelection() {
	w, h := m.contentSize()
	lines := wrappedLines(m.cartContent(false), w)
	for i, line := range lines {
		if strings.HasPrefix(ansi.Strip(line), "> ") {
			if i < m.scroll[ViewCart] {
				m.scroll[ViewCart] = i
			}
			if i >= m.scroll[ViewCart]+h-1 {
				m.scroll[ViewCart] = max(0, i-h+2)
			}
			return
		}
	}
}
func scrollWindow(text string, w, h, offset int) string {
	lines := wrappedLines(text, w)
	offset = max(0, min(offset, max(0, len(lines)-h+1)))
	if h <= 1 {
		return fitLines(lines[min(offset, len(lines)-1)], w, 1)
	}
	if len(lines) > h {
		// One reserved line indicates scrolling without hiding the primary controls.
		end := min(len(lines), offset+max(1, h-1))
		shown := strings.Join(lines[offset:end], "\n")
		return fitLines(shown, w, h-1) + "\n" + oneLine(fmt.Sprintf("Lines %d–%d of %d · PgUp/PgDown scroll", offset+1, end, len(lines)), w)
	}
	return fitLines(text, w, h)
}
func (m Model) helpText() string {
	copy := m
	copy.showHelp = false
	var b strings.Builder
	b.WriteString("Controls for " + copy.title() + "\n")
	for _, binding := range copy.bindings() {
		aliases := ""
		if len(binding.keys) > 1 {
			aliases = " (also " + strings.Join(binding.keys[1:], ", ") + ")"
		}
		fmt.Fprintf(&b, "%s%s\n", binding.label, aliases)
	}
	if copy.viewState == ViewProductList {
		if m.selectedProduct != nil {
			fmt.Fprintf(&b, "\n%s\n", m.selectedProduct.Name)
			if m.selectedProduct.IsVariable() {
				fmt.Fprintf(&b, "Size: %s\n", m.sizeLabel())
			} else if attr := m.selectedProduct.GetAttribute("Size"); attr != nil && len(attr.Options) > 0 {
				fmt.Fprintf(&b, "Size: %s\n", strings.Join(attr.Options, ", "))
			}
			if len(m.grindOptions()) > 0 || m.selectedGrindSize != "" {
				fmt.Fprintf(&b, "Grind: %s\n", m.selectedGrindSize)
			}
			fmt.Fprintf(&b, "Quantity: %d\n%s\n", m.selectedQuantity, m.purchaseReason())
		}
		fmt.Fprintf(&b, "\nSearch query: %q\nStock filter: %t\nResults: %d\n", m.searchInput.Value(), m.inStockOnly, len(m.productList.Items()))
	}

	if m.err != nil {
		fmt.Fprintf(&b, "\nError: %s\n", m.err)
	}
	if m.sessionState.Error != "" {
		fmt.Fprintf(&b, "\nCart: %s\n", m.sessionState.Error)
	}
	if m.catalogErr != nil {
		fmt.Fprintf(&b, "\nCatalog: %s\n", m.catalogErr)
	}
	if m.optionsErr != nil {
		fmt.Fprintf(&b, "\nOptions: %s\n", m.optionsErr)
	}
	b.WriteString("\n* required address field. Country defaults to Italy (IT).\nPayment stays open until explicit navigation.\nClosing help preserves your screen and input focus.")
	return b.String()
}

func (m Model) header() string {
	pw, _ := m.panelSize()
	amount := "—"
	if m.sessionState.Cart.Totals.TotalPrice != "" {
		amount = m.money(m.sessionState.Cart.Totals.TotalPrice)
	}
	if m.sessionState.Pending || m.operationBusy() {
		amount += " updating"
	}
	labels := []string{"EVA", "s shop", fmt.Sprintf("c cart %s [%d]", amount, m.localCart.ItemCount())}
	widths := []int{12, 12, pw - 28}
	var top, middle, bottom []string
	for i, label := range labels {
		top = append(top, strings.Repeat("─", widths[i]))
		bottom = append(bottom, strings.Repeat("─", widths[i]))
		style := m.styles.Subtle
		if i == 0 || (i == 1 && m.viewState == ViewProductList) || (i == 2 && m.viewState != ViewProductList) {
			style = m.styles.HeaderTitle
		}
		middle = append(middle, style.Width(widths[i]).Align(lipgloss.Center).Render(oneLine(label, widths[i])))
	}
	border := m.styles.Border
	return border.Render("┌"+strings.Join(top, "┬")+"┐") + "\n" +
		border.Render("│") + strings.Join(middle, border.Render("│")) + border.Render("│") + "\n" +
		border.Render("└"+strings.Join(bottom, "┴")+"┘")
}

// ASCII silhouette of logotipo.pdf; o marks the orange dot.
const splashLogo = `                          .#.
       ..###.   .        ####.     ..###..
    .#####..   ###.     #####.   .##########
  .####.o      .####   ######   #############.
 #####ooooo     ##### .#####.  #####.   ######.
######oooo       ###########  .####.     ######
 ########....     #########   .#### ...   ######
  .###########     .######     ###         ####.
     ...###..        .#.        ..          .#.`

const compactLogo = `                 .#
   .###.. .#    ###.  .#####.
 .###oo   .##. ####. ####.####.
.###ooo    #######. .###   ####.
 #######.   .####.  .##     ####
   ..##..     ..     .       ..`

const smallLogo = ` .##. #.## .##.
##oo  ###. ## ##
.###.  ##  ##.##`

func (m Model) logoText(logo string, width int, dotColor string) string {
	dot := lipgloss.NewStyle()
	if !m.noColor && m.profile != colorprofile.Ascii {
		dot = dot.Foreground(m.profile.Convert(lipgloss.Color(dotColor)))
	}
	lines := strings.Split(logo, "\n")
	for i, line := range lines {
		parts := strings.Split(line, "o")
		for j := range parts {
			parts[j] = m.styles.Subtle.Render(parts[j])
		}
		lines[i] = strings.Join(parts, dot.Render("o"))
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func (m Model) splashText() string {
	colors := [...]string{"#914326", "#D36237", "#FF9864", "#D36237", "#914326"}
	return m.logoText(splashLogo, 48, colors[m.splashFrame%len(colors)])
}

func (m Model) View() tea.View {
	v := tea.NewView("Loading...")
	v.AltScreen, v.ReportFocus = true, true
	if m.width == 0 {
		return v
	}
	if m.tooSmall() {
		text := fitLines("EVA coffee shop\nResize to at least 60 × 18.\nYour shopping state is preserved.\nq / Ctrl+C quit", max(1, min(40, m.width)), max(1, min(4, m.height)))
		v.SetContent(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, text))
		return v
	}
	if m.splash {
		v.SetContent(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.splashText()))
		return v
	}
	pw, ph := m.panelSize()
	w, h := m.contentSize()
	var text string
	if m.showHelp {
		text = m.header() + "\n" + m.styles.App.Render(m.styles.HeaderTitle.Render("EVA · Help")+"\n"+
			scrollWindow(m.helpText(), w, ph-6, m.helpScroll)+"\n"+
			m.styles.Border.Render(strings.Repeat("─", w))+"\n"+
			m.styles.HelpBar.Render(oneLine("Esc / ? close help · ↑/↓ scroll · Ctrl+C quit", w)))
	} else {
		body := m.bodyText()
		if m.inputActive() || m.viewState == ViewProductList {
			body = fitLines(body, w, h)
		} else {
			body = scrollWindow(body, w, h, m.scroll[m.viewState])
		}
		context := m.contextLine()
		if m.viewState != ViewProductList {
			context = m.title() + " · " + context
		}
		text = m.styles.Subtle.Render(oneLine(context, w)) + "\n" + body
		if summary := m.fixedSummary(); summary != "" {
			text += "\n" + summary
		}
		text += "\n" + m.feedback() + "\n" + m.styles.Border.Render(strings.Repeat("─", w)) + "\n" + m.footer()
		text = m.header() + "\n" + m.styles.App.Render(text)
	}
	panel := lipgloss.NewStyle().Width(pw).Height(ph).Render(text)
	screen := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
	if headspace := (m.height - ph) / 2; headspace >= 3 {
		art, width := compactLogo, 32
		if headspace < 6 {
			art, width = smallLogo, 16
		}
		lines := strings.Split(screen, "\n")
		logo := lipgloss.Place(m.width, headspace, lipgloss.Center, lipgloss.Center, m.logoText(art, width, "#D36237"))
		screen = logo + "\n" + strings.Join(lines[headspace:], "\n")
	}
	v.SetContent(screen)
	return v
}
