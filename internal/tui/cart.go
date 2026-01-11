package tui

import (
	"fmt"

	"github.com/thomas/eva-terminal-go/internal/woo"
)

// CartItem represents a legacy local cart item.
// Deprecated: Use LocalCartItem from local_cart.go instead.
type CartItem struct {
	Product   *woo.Product
	Variation *woo.Variation
	GrindSize string
	Quantity  int
}

// ToAddToCartRequest converts the cart item to a Store API request.
// Deprecated: No longer used with quote-based flow.
func (ci *CartItem) ToAddToCartRequest() woo.AddToCartRequest {
	req := woo.AddToCartRequest{
		Quantity: ci.Quantity,
	}

	if ci.Variation != nil {
		req.ID = ci.Variation.ID
	} else if ci.Product != nil {
		req.ID = ci.Product.ID
	}

	if ci.Variation != nil {
		for _, attr := range ci.Variation.Attributes {
			req.Variation = append(req.Variation, woo.CartItemVariation{
				Attribute: attr.Name,
				Value:     attr.Option,
			})
		}
	}

	return req
}

// GetItemDisplayName returns the display name for a Store API cart item.
// Deprecated: Use LocalCartItem.GetDisplayName() instead.
func GetItemDisplayName(item *woo.StoreCartItem) string {
	name := item.Name
	for _, v := range item.Variation {
		name = fmt.Sprintf("%s (%s: %s)", name, v.Attribute, v.Value)
	}
	return name
}

// GetItemPrice returns the formatted unit price for a Store API cart item.
// Deprecated: Use quote line item formatting instead.
func GetItemPrice(item *woo.StoreCartItem) string {
	return woo.FormatPrice(
		item.Prices.Price,
		item.Prices.CurrencySymbol,
		item.Prices.CurrencyMinorUnit,
	)
}

// GetItemTotal returns the formatted line total for a Store API cart item.
// Deprecated: Use quote line item formatting instead.
func GetItemTotal(item *woo.StoreCartItem) string {
	return woo.FormatPrice(
		item.Totals.LineTotal,
		item.Totals.CurrencySymbol,
		item.Totals.CurrencyMinorUnit,
	)
}
