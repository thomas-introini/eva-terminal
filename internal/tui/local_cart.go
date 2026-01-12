package tui

import (
	"fmt"

	"github.com/thomas/eva-terminal-go/internal/woo"
)

// ShippingConfig holds the shipping calculation settings.
// Since we only ship to Italy with simple flat rate / free shipping threshold.
type ShippingConfig struct {
	FlatRateCost    float64 // e.g., 5.00
	FreeShippingMin float64 // e.g., 50.00 (free if subtotal >= this)
}

// DefaultShippingConfig returns the default shipping configuration.
func DefaultShippingConfig() ShippingConfig {
	return ShippingConfig{
		FlatRateCost:    5.00,
		FreeShippingMin: 50.00,
	}
}

// LocalCart manages cart state locally per SSH session.
type LocalCart struct {
	// Items in the cart
	Items []LocalCartItem

	// Shipping configuration
	ShippingConfig ShippingConfig

	// UI state
	SelectedIdx int
}

// LocalCartItem represents an item in the local cart.
type LocalCartItem struct {
	ProductID   int
	VariationID int
	Name        string            // Display name
	Price       float64           // Unit price
	Quantity    int
	GrindSize   string            // Selected grind size (e.g., "Fine", "Whole Beans")
	Meta        map[string]string // Additional metadata
}

// NewLocalCart creates a new empty local cart.
func NewLocalCart() *LocalCart {
	return &LocalCart{
		Items:          make([]LocalCartItem, 0),
		ShippingConfig: DefaultShippingConfig(),
		SelectedIdx:    0,
	}
}

// ============================================
// Cart Operations
// ============================================

// AddItem adds an item to the cart.
// If the same product/variation/grind exists, it increments quantity.
func (c *LocalCart) AddItem(item LocalCartItem) {
	// Check for existing item
	for i := range c.Items {
		if c.Items[i].ProductID == item.ProductID &&
			c.Items[i].VariationID == item.VariationID &&
			c.Items[i].GrindSize == item.GrindSize {
			c.Items[i].Quantity += item.Quantity
			return
		}
	}

	// Add new item
	c.Items = append(c.Items, item)
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
	return true
}

// RemoveItem removes an item by index.
func (c *LocalCart) RemoveItem(index int) bool {
	if index < 0 || index >= len(c.Items) {
		return false
	}

	c.Items = append(c.Items[:index], c.Items[index+1:]...)

	// Adjust selected index
	if c.SelectedIdx >= len(c.Items) && len(c.Items) > 0 {
		c.SelectedIdx = len(c.Items) - 1
	}
	return true
}

// Clear removes all items from the cart.
func (c *LocalCart) Clear() {
	c.Items = make([]LocalCartItem, 0)
	c.SelectedIdx = 0
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
// Price Calculations
// ============================================

// Subtotal returns the cart subtotal (sum of line totals).
func (c *LocalCart) Subtotal() float64 {
	var total float64
	for _, item := range c.Items {
		total += item.Price * float64(item.Quantity)
	}
	return total
}

// CalculateShipping returns the shipping cost based on config.
// Returns 0 if subtotal >= free shipping threshold.
func (c *LocalCart) CalculateShipping() float64 {
	if c.IsEmpty() {
		return 0
	}
	subtotal := c.Subtotal()
	if subtotal >= c.ShippingConfig.FreeShippingMin {
		return 0
	}
	return c.ShippingConfig.FlatRateCost
}

// CalculateTotal returns the cart total (subtotal + shipping).
func (c *LocalCart) CalculateTotal() float64 {
	return c.Subtotal() + c.CalculateShipping()
}

// QualifiesForFreeShipping returns true if free shipping applies.
func (c *LocalCart) QualifiesForFreeShipping() bool {
	return c.Subtotal() >= c.ShippingConfig.FreeShippingMin
}

// AmountUntilFreeShipping returns how much more is needed for free shipping.
func (c *LocalCart) AmountUntilFreeShipping() float64 {
	remaining := c.ShippingConfig.FreeShippingMin - c.Subtotal()
	if remaining < 0 {
		return 0
	}
	return remaining
}

// GetSubtotal returns the formatted subtotal string.
func (c *LocalCart) GetSubtotal() string {
	return fmt.Sprintf("$%.2f", c.Subtotal())
}

// GetTotal returns the formatted total string.
func (c *LocalCart) GetTotal() string {
	return fmt.Sprintf("$%.2f", c.CalculateTotal())
}

// GetShippingFormatted returns the formatted shipping cost string.
func (c *LocalCart) GetShippingFormatted() string {
	shipping := c.CalculateShipping()
	if shipping == 0 {
		return "FREE"
	}
	return fmt.Sprintf("$%.2f", shipping)
}

// ============================================
// Display Methods
// ============================================

// GetItemDisplayName returns a display name for a cart item.
func (item *LocalCartItem) GetDisplayName() string {
	name := item.Name
	if item.GrindSize != "" {
		name = fmt.Sprintf("%s - %s", name, item.GrindSize)
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
// Factory Methods
// ============================================

// NewLocalCartItemFromProduct creates a LocalCartItem from product data.
func NewLocalCartItemFromProduct(product *woo.Product, variation *woo.Variation, quantity int, grindSize string) LocalCartItem {
	item := LocalCartItem{
		ProductID: product.ID,
		Quantity:  quantity,
		GrindSize: grindSize,
		Meta:      make(map[string]string),
	}

	if variation != nil {
		item.VariationID = variation.ID
		// Build display name with variant info
		variantName := variation.GetAttributeValue("Size")
		if variantName != "" {
			item.Name = fmt.Sprintf("%s (%s)", product.Name, variantName)
		} else {
			item.Name = product.Name
		}
		item.Price = parsePrice(variation.GetDisplayPrice())
	} else {
		item.Name = product.Name
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
