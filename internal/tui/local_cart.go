package tui

import (
	"fmt"

	"github.com/thomas/eva-terminal-go/internal/woo"
)

// LocalCart manages cart state locally per SSH session.
// It stores items, applies coupons, and holds quote responses.
type LocalCart struct {
	// Items in the cart
	Items []LocalCartItem

	// Applied coupon codes
	Coupons []string

	// Current quote from server (nil if not yet quoted)
	Quote *woo.QuoteResponse

	// Selected shipping rate ID
	SelectedShippingRateID string

	// UI state
	SelectedIdx int
}

// LocalCartItem represents an item in the local cart.
type LocalCartItem struct {
	ProductID   int
	VariationID int
	ProductName string
	VariantName string // e.g., "250g"
	Price       float64
	Quantity    int
	Meta        map[string]string // e.g., {"grind": "Fine"}
}

// NewLocalCart creates a new empty local cart.
func NewLocalCart() *LocalCart {
	return &LocalCart{
		Items:       make([]LocalCartItem, 0),
		Coupons:     make([]string, 0),
		SelectedIdx: 0,
	}
}

// ============================================
// Cart Operations
// ============================================

// AddItem adds an item to the cart.
// If the same product/variation exists, it increments quantity.
func (c *LocalCart) AddItem(item LocalCartItem) {
	// Check for existing item
	for i := range c.Items {
		if c.Items[i].ProductID == item.ProductID &&
			c.Items[i].VariationID == item.VariationID &&
			metaEqual(c.Items[i].Meta, item.Meta) {
			c.Items[i].Quantity += item.Quantity
			c.invalidateQuote()
			return
		}
	}

	// Add new item
	c.Items = append(c.Items, item)
	c.invalidateQuote()
}

// UpdateQuantity updates the quantity of an item by index.
func (c *LocalCart) UpdateQuantity(index int, quantity int) bool {
	if index < 0 || index >= len(c.Items) {
		return false
	}

	if quantity <= 0 {
		return c.RemoveItem(index)
	}

	c.Items[index].Quantity = quantity
	c.invalidateQuote()
	return true
}

// RemoveItem removes an item by index.
func (c *LocalCart) RemoveItem(index int) bool {
	if index < 0 || index >= len(c.Items) {
		return false
	}

	c.Items = append(c.Items[:index], c.Items[index+1:]...)
	c.invalidateQuote()

	// Adjust selected index
	if c.SelectedIdx >= len(c.Items) && len(c.Items) > 0 {
		c.SelectedIdx = len(c.Items) - 1
	}
	return true
}

// Clear removes all items from the cart.
func (c *LocalCart) Clear() {
	c.Items = make([]LocalCartItem, 0)
	c.Coupons = make([]string, 0)
	c.Quote = nil
	c.SelectedShippingRateID = ""
	c.SelectedIdx = 0
}

// ============================================
// Coupon Operations
// ============================================

// AddCoupon adds a coupon code to the cart.
func (c *LocalCart) AddCoupon(code string) {
	// Check for duplicate
	for _, existing := range c.Coupons {
		if existing == code {
			return
		}
	}
	c.Coupons = append(c.Coupons, code)
	c.invalidateQuote()
}

// RemoveCoupon removes a coupon code from the cart.
func (c *LocalCart) RemoveCoupon(code string) {
	for i, existing := range c.Coupons {
		if existing == code {
			c.Coupons = append(c.Coupons[:i], c.Coupons[i+1:]...)
			c.invalidateQuote()
			return
		}
	}
}

// ============================================
// Quote Operations
// ============================================

// SetQuote stores the quote response from the server.
func (c *LocalCart) SetQuote(quote *woo.QuoteResponse) {
	c.Quote = quote
	c.SelectedShippingRateID = ""
}

// HasQuote returns true if a valid quote exists.
func (c *LocalCart) HasQuote() bool {
	return c.Quote != nil && !c.Quote.IsExpired()
}

// SelectShippingRate sets the selected shipping rate.
func (c *LocalCart) SelectShippingRate(rateID string) {
	c.SelectedShippingRateID = rateID
}

// GetSelectedShippingRate returns the selected shipping rate.
func (c *LocalCart) GetSelectedShippingRate() *woo.QuoteShippingRate {
	if c.Quote == nil || c.SelectedShippingRateID == "" {
		return nil
	}
	return c.Quote.GetShippingRate(c.SelectedShippingRateID)
}

// invalidateQuote clears the quote when cart changes.
func (c *LocalCart) invalidateQuote() {
	c.Quote = nil
	c.SelectedShippingRateID = ""
}

// ============================================
// Query Methods
// ============================================

// IsEmpty returns true if the cart has no items.
func (c *LocalCart) IsEmpty() bool {
	return len(c.Items) == 0
}

// Len returns the number of distinct line items.
func (c *LocalCart) Len() int {
	return len(c.Items)
}

// ItemCount returns the total quantity of all items.
func (c *LocalCart) ItemCount() int {
	count := 0
	for _, item := range c.Items {
		count += item.Quantity
	}
	return count
}

// GetSelectedItem returns the currently selected item.
func (c *LocalCart) GetSelectedItem() *LocalCartItem {
	if c.SelectedIdx < 0 || c.SelectedIdx >= len(c.Items) {
		return nil
	}
	return &c.Items[c.SelectedIdx]
}

// MoveUp moves selection up.
func (c *LocalCart) MoveUp() {
	if c.SelectedIdx > 0 {
		c.SelectedIdx--
	}
}

// MoveDown moves selection down.
func (c *LocalCart) MoveDown() {
	if c.SelectedIdx < len(c.Items)-1 {
		c.SelectedIdx++
	}
}

// ============================================
// Price Calculations (Local Estimates)
// ============================================

// Subtotal returns the local subtotal estimate.
func (c *LocalCart) Subtotal() float64 {
	var total float64
	for _, item := range c.Items {
		total += item.Price * float64(item.Quantity)
	}
	return total
}

// GetSubtotal returns formatted subtotal.
// Uses quote data if available, otherwise local estimate.
func (c *LocalCart) GetSubtotal() string {
	if c.Quote != nil {
		return c.Quote.FormatPrice(c.Quote.Totals.Subtotal)
	}
	return fmt.Sprintf("$%.2f", c.Subtotal())
}

// GetDiscount returns formatted discount.
func (c *LocalCart) GetDiscount() string {
	if c.Quote != nil {
		return c.Quote.FormatPrice(c.Quote.Totals.Discount)
	}
	return "$0.00"
}

// GetShipping returns formatted shipping.
func (c *LocalCart) GetShipping() string {
	if c.Quote != nil {
		rate := c.GetSelectedShippingRate()
		if rate != nil {
			return c.Quote.FormatPrice(rate.Cost)
		}
		return c.Quote.FormatPrice(c.Quote.Totals.Shipping)
	}
	return "$0.00"
}

// GetTax returns formatted tax.
func (c *LocalCart) GetTax() string {
	if c.Quote != nil {
		return c.Quote.FormatPrice(c.Quote.Totals.Tax)
	}
	return "$0.00"
}

// GetTotal returns formatted total.
func (c *LocalCart) GetTotal() string {
	if c.Quote != nil {
		return c.Quote.FormatPrice(c.Quote.Totals.Total)
	}
	return fmt.Sprintf("$%.2f", c.Subtotal())
}

// ============================================
// Conversion Methods
// ============================================

// ToQuoteItems converts cart items to QuoteItem slice for API requests.
func (c *LocalCart) ToQuoteItems() []woo.QuoteItem {
	items := make([]woo.QuoteItem, len(c.Items))
	for i, item := range c.Items {
		items[i] = woo.QuoteItem{
			ProductID:   item.ProductID,
			VariationID: item.VariationID,
			Quantity:    item.Quantity,
			Meta:        item.Meta,
		}
	}
	return items
}

// ToQuoteRequest builds a QuoteRequest from cart state.
func (c *LocalCart) ToQuoteRequest(shippingAddress woo.QuoteAddress) woo.QuoteRequest {
	return woo.QuoteRequest{
		Items:           c.ToQuoteItems(),
		Coupons:         c.Coupons,
		ShippingAddress: shippingAddress,
	}
}

// ============================================
// Display Methods
// ============================================

// GetItemDisplayName returns a display name for a cart item.
func (item *LocalCartItem) GetDisplayName() string {
	name := item.ProductName
	if item.VariantName != "" {
		name = fmt.Sprintf("%s (%s)", name, item.VariantName)
	}
	if grind, ok := item.Meta["grind"]; ok && grind != "" {
		name = fmt.Sprintf("%s - %s", name, grind)
	}
	return name
}

// GetFormattedPrice returns formatted unit price.
func (item *LocalCartItem) GetFormattedPrice() string {
	return fmt.Sprintf("$%.2f", item.Price)
}

// GetFormattedTotal returns formatted line total.
func (item *LocalCartItem) GetFormattedTotal() string {
	return fmt.Sprintf("$%.2f", item.Price*float64(item.Quantity))
}

// ============================================
// Helper Functions
// ============================================

// metaEqual compares two meta maps for equality.
func metaEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// NewLocalCartItemFromProduct creates a LocalCartItem from product data.
func NewLocalCartItemFromProduct(product *woo.Product, variation *woo.Variation, quantity int, meta map[string]string) LocalCartItem {
	item := LocalCartItem{
		ProductID: product.ID,
		Quantity:  quantity,
		Meta:      meta,
	}

	if variation != nil {
		item.VariationID = variation.ID
		item.ProductName = product.Name
		item.VariantName = variation.GetAttributeValue("Size")
		item.Price = parsePrice(variation.GetDisplayPrice())
	} else {
		item.ProductName = product.Name
		item.Price = parsePrice(product.GetDisplayPrice())
	}

	return item
}

// parsePrice converts a price string to float64.
func parsePrice(priceStr string) float64 {
	var price float64
	fmt.Sscanf(priceStr, "%f", &price)
	return price
}
