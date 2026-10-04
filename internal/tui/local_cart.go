package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/thomas/eva-terminal-go/internal/woo"
)

// LocalCart manages cart state locally per SSH session.
type LocalCart struct {
	// Items in the cart
	Items []LocalCartItem

	// UI state
	SelectedIdx int
}

// LocalCartItem represents an item in the local cart.
type LocalCartItem struct {
	ProductID   int
	VariationID int
	Name        string // Display name
	PriceMinor  int64  // Cached unit price in minor units; Woo computes all totals
	Quantity    int
	GrindSize   string            // Selected grind size (e.g., "Fine", "Whole Beans")
	Meta        map[string]string // Additional metadata
}

// NewLocalCart creates a new empty local cart.
func NewLocalCart() *LocalCart {
	return &LocalCart{
		Items:       make([]LocalCartItem, 0),
		SelectedIdx: 0,
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

	unit := product.CurrencyMinorUnit
	if product.CurrencyCode == "" {
		unit = 2
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
		item.PriceMinor = decimalMinor(variation.GetDisplayPrice(), unit)
	} else {
		item.Name = product.Name
		item.PriceMinor = decimalMinor(product.Price, unit)
	}

	return item
}

// decimalMinor parses cached display prices without floating point arithmetic.
func decimalMinor(price string, unit int) int64 {
	if unit < 0 || unit > 6 {
		return 0
	}
	parts := strings.SplitN(price, ".", 2)
	fraction := ""
	if len(parts) > 1 {
		fraction = parts[1]
	}
	fraction += strings.Repeat("0", unit)
	n, _ := strconv.ParseInt(parts[0]+fraction[:unit], 10, 64)
	return n
}
