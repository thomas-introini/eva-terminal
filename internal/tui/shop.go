package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

type productDraft struct {
	variationID int
	grind       string
	quantity    int
}

// Preview follows the list selection, without a separate details/configuration flow.
func (m *Model) selectShopProduct(refresh bool) tea.Cmd {
	item, ok := m.productList.SelectedItem().(productItem)
	if !ok {
		m.selectedProduct, m.selectedVariation = nil, nil
		m.productVariations, m.optionsErr = nil, nil
		m.loadingVariations = false
		return nil
	}
	changed := m.selectedProduct == nil || m.selectedProduct.ID != item.product.ID
	m.selectedProduct, m.selectedID = &item.product, item.product.ID
	if changed {
		m.productVariations, m.selectedVariation, m.optionsErr = nil, nil, nil
		m.loadingVariations = false
		m.scroll[ViewProductList] = 0
		m.shopFocus = ""
	}
	if item.product.IsVariable() && (changed || refresh) {
		m.loadingVariations, m.optionsErr = true, nil
		m.restoreProductDraft()
		return m.loadVariations(item.product.ID)
	}
	m.restoreProductDraft()
	return nil
}

func (m Model) grindOptions() []string {
	if m.selectedProduct != nil {
		if attr := m.selectedProduct.GetAttribute("Grind Size"); attr != nil {
			return attr.Options
		}
	}
	return nil
}

func (m Model) sizeOptions() []woo.Variation {
	var options []woo.Variation
	for _, v := range m.productVariations {
		if v.ID > 0 && len(v.Attributes) > 0 {
			options = append(options, v)
		}
	}
	return options
}

func variationAvailable(v woo.Variation) bool {
	return v.IsInStock() && (v.Purchasable == nil || *v.Purchasable)
}

func (m *Model) restoreProductDraft() {
	if m.selectedProduct == nil {
		return
	}
	p := m.selectedProduct
	draft, exists := m.productDrafts[p.ID]
	if !exists {
		draft.quantity = 1
		grinds := m.grindOptions()
		if len(grinds) > 0 {
			draft.grind = grinds[0]
			for _, grind := range grinds {
				if strings.EqualFold(grind, "Whole Beans") {
					draft.grind = grind
					break
				}
			}
		}
	}
	// A previously selected ID is retained if removed/unavailable: never silently
	// replace the size immediately before an add after a catalog refresh.
	m.selectedVariation = nil
	if p.IsVariable() {
		for i := range m.productVariations {
			v := &m.productVariations[i]
			if v.ID <= 0 || len(v.Attributes) == 0 {
				continue
			}
			if draft.variationID == 0 && variationAvailable(*v) {
				draft.variationID = v.ID
			}
			if v.ID == draft.variationID {
				m.selectedVariation = v
			}
		}
	}
	m.selectedQuantity, m.selectedGrindSize = max(1, draft.quantity), draft.grind
	draft.quantity = m.selectedQuantity
	m.productDrafts[p.ID] = draft
	if !slices.Contains(m.shopControls(), m.shopFocus) {
		m.shopFocus = m.shopControls()[0]
	}
}

func (m Model) shopControls() []string {
	var controls []string
	if m.sizeEditable() {
		controls = append(controls, "size")
	}
	if m.grindEditable() {
		controls = append(controls, "grind")
	}
	return append(controls, "quantity", "add")
}

func (m Model) sizeEditable() bool {
	if m.selectedProduct == nil || !m.selectedProduct.IsVariable() {
		return false
	}
	options := m.sizeOptions()
	return len(options) > 1 || (len(options) == 1 && m.selectedVariation == nil) ||
		(m.loadingVariations && len(m.selectedProduct.Variations) > 1)
}

func (m Model) grindEditable() bool {
	options := m.grindOptions()
	return len(options) > 1 || (m.selectedGrindSize != "" && !slices.Contains(options, m.selectedGrindSize))
}

func (m *Model) cycleShopFocus(previous bool) {
	controls := m.shopControls()
	delta := 1
	if previous {
		delta = -1
	}
	idx := slices.Index(controls, m.shopFocus)
	m.shopFocus = controls[(max(0, idx)+delta+len(controls))%len(controls)]
}

func choiceIndex(current, length, delta int) int {
	if current < 0 {
		if delta < 0 {
			return length - 1
		}
		return 0
	}
	return (current + delta + length) % length
}

func (m *Model) changeShopOption(delta int) {
	if m.selectedProduct == nil {
		return
	}
	draft := m.productDrafts[m.selectedID]
	switch m.shopFocus {
	case "size":
		if m.loadingVariations {
			return
		}
		options := m.sizeOptions()
		if len(options) == 0 {
			return
		}
		idx := slices.IndexFunc(options, func(v woo.Variation) bool { return v.ID == draft.variationID })
		draft.variationID = options[choiceIndex(idx, len(options), delta)].ID
	case "grind":
		options := m.grindOptions()
		if len(options) == 0 {
			draft.grind = ""
		} else {
			draft.grind = options[choiceIndex(slices.Index(options, draft.grind), len(options), delta)]
		}
	case "quantity":
		m.changeDraftQuantity(delta)
		return
	}
	m.productDrafts[m.selectedID] = draft
	m.restoreProductDraft()
}

func (m *Model) changeDraftQuantity(delta int) {
	if m.selectedProduct == nil {
		return
	}
	draft := m.productDrafts[m.selectedID]
	draft.quantity = max(1, draft.quantity+delta)
	m.productDrafts[m.selectedID] = draft
	m.restoreProductDraft()
}

func variationLabel(v *woo.Variation) string {
	var labels []string
	for _, attr := range v.Attributes {
		if attr.Name == "Size" {
			labels = append(labels, attr.Option)
		} else {
			labels = append(labels, attr.Name+": "+attr.Option)
		}
	}
	return strings.Join(labels, " · ")
}

func (m Model) sizeLabel() string {
	if m.loadingVariations {
		return "Loading…"
	}
	if m.selectedVariation != nil {
		return variationLabel(m.selectedVariation)
	}
	if len(m.sizeOptions()) == 0 {
		return "Unavailable"
	}
	return "Choose an available size"
}

func (m Model) shopOptionRow(key, label, value string, editable bool, width int) string {
	marker := "  "
	if m.shopFocus == key && !m.showSearch {
		marker = "> "
	}
	if editable {
		value = "< " + value + " >"
	}
	text := oneLine(marker+label+": "+value, width)
	if marker == "> " {
		return m.styles.Highlight.Render(text)
	}
	return text
}

func (m Model) shopOptions(width int) string {
	var rows []string
	if m.selectedProduct.IsVariable() {
		rows = append(rows, m.shopOptionRow("size", "Size", m.sizeLabel(), m.sizeEditable(), width))
	} else if attr := m.selectedProduct.GetAttribute("Size"); attr != nil && len(attr.Options) > 0 {
		rows = append(rows, m.shopOptionRow("size", "Size", strings.Join(attr.Options, ", "), false, width))
	}
	if len(m.grindOptions()) > 0 || m.selectedGrindSize != "" {
		rows = append(rows, m.shopOptionRow("grind", "Grind", m.selectedGrindSize, m.grindEditable(), width))
	}
	quantity := m.shopOptionRow("quantity", "Qty", fmt.Sprint(m.selectedQuantity), true, max(1, width-20))
	add := "[ Add to cart ]"
	if m.purchaseReason() != "Available to purchase" {
		add = "[ Unavailable ]"
	}
	if m.shopFocus == "add" && !m.showSearch {
		add = m.styles.Selection.Render("> " + add)
	} else {
		add = m.styles.Highlight.Render(add)
	}
	rows = append(rows, oneLine(quantity+"   "+add, width))
	return strings.Join(rows, "\n")
}

func (m Model) shopDescription() string {
	if m.selectedProduct == nil {
		return ""
	}
	p := m.selectedProduct
	text := p.Description
	if text == "" {
		text = p.ShortDescription
	}
	// Keep the full product name accessible even when the fixed heading truncates.
	w, _ := m.shopDetailSize()
	if ansi.StringWidth(p.Name) > w {
		text = p.Name + "\n\n" + text
	}
	return StripHTML(text)
}

func (m Model) shopDetailSize() (int, int) {
	w, h := m.contentSize()
	if m.width >= 80 {
		w -= m.productList.Width() + 2 // Product list and two-column gap.
	} else {
		h -= 3
	}
	return max(1, w), max(1, h)
}

func (m Model) shopDescriptionSize() (int, int) {
	w, h := m.shopDetailSize()
	if m.selectedProduct == nil {
		return w, h
	}
	return w, max(1, min(6, h-2-lipgloss.Height(m.shopOptions(w))))
}

func (m Model) shopBody() string {
	w, h := m.contentSize()
	if m.selectedProduct == nil {
		return fitLines(m.catalogEmptyMessage(), w, h)
	}
	dw, dh := m.shopDetailSize()
	descw, desch := m.shopDescriptionSize()
	price := m.selectedProduct.GetDisplayPrice()
	if m.selectedVariation != nil {
		price = m.selectedVariation.GetDisplayPrice()
	}
	price = m.selectedProduct.CurrencyCode + " " + price
	head := lipgloss.NewStyle().Bold(true).Render(oneLine(m.selectedProduct.Name, dw)) + "\n" +
		oneLine(m.styles.Highlight.Render(price)+" · "+m.styles.Subtle.Render(m.purchaseReason()), dw)
	description := m.styles.Subtle.Render(scrollWindow(m.shopDescription(), descw, desch, m.scroll[ViewProductList]))
	details := fitLines(head+"\n"+description+"\n"+m.shopOptions(dw), dw, dh)
	if m.width >= 80 {
		lw := m.productList.Width()
		list := lipgloss.NewStyle().Width(lw).Height(h).Render(fitLines(m.productList.View(), lw, h))
		return lipgloss.JoinHorizontal(lipgloss.Top, list, strings.Repeat(" ", 2), details)
	}
	return fitLines(m.productList.View(), w, 3) + "\n" + details
}

func (m Model) catalogEmptyMessage() string {
	if m.loadingProducts {
		return "Loading coffee catalog…"
	}
	if m.searchInput.Value() != "" || m.inStockOnly {
		return "No matches. Press / then Esc to clear search, or f to show all stock."
	}
	if m.catalog != nil {
		updated, err := m.catalog.Status()
		if updated.IsZero() && err == nil {
			return "Loading coffee catalog…"
		}
		if err != nil {
			return "Catalog unavailable. Press r to retry."
		}
	}
	if m.catalogErr != nil {
		return "Catalog unavailable. Press r to retry."
	}
	return "The coffee catalog is empty. Press r to refresh."
}
