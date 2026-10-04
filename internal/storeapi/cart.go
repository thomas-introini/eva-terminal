package storeapi

import (
	"context"
	"net/http"
)

// GetCart fetches the current cart.
func (c *Client) GetCart(ctx context.Context) (*Cart, error) {
	var result Cart
	if err := c.doJSON(ctx, http.MethodGet, "/cart", nil, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// AddItem adds an item to cart.
func (c *Client) AddItem(ctx context.Context, req AddItemRequest) (*Cart, error) {
	if req.Quantity <= 0 {
		req.Quantity = 1
	}
	var result Cart
	if err := c.doJSON(ctx, http.MethodPost, "/cart/add-item", nil, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateItem updates a cart item quantity.
func (c *Client) UpdateItem(ctx context.Context, req UpdateItemRequest) (*Cart, error) {
	var result Cart
	if err := c.doJSON(ctx, http.MethodPost, "/cart/update-item", nil, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RemoveItem removes a cart item.
func (c *Client) RemoveItem(ctx context.Context, req RemoveItemRequest) (*Cart, error) {
	var result Cart
	if err := c.doJSON(ctx, http.MethodPost, "/cart/remove-item", nil, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ApplyCoupon applies a coupon.
func (c *Client) ApplyCoupon(ctx context.Context, req CouponRequest) (*Cart, error) {
	var result Cart
	if err := c.doJSON(ctx, http.MethodPost, "/cart/apply-coupon", nil, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RemoveCoupon removes a coupon.
func (c *Client) RemoveCoupon(ctx context.Context, req CouponRequest) (*Cart, error) {
	var result Cart
	if err := c.doJSON(ctx, http.MethodPost, "/cart/remove-coupon", nil, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateCustomer updates customer addresses for cart.
func (c *Client) UpdateCustomer(ctx context.Context, req UpdateCustomerRequest) (*Cart, error) {
	var result Cart
	if err := c.doJSON(ctx, http.MethodPost, "/cart/update-customer", nil, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SelectShippingRate picks a shipping rate.
func (c *Client) SelectShippingRate(ctx context.Context, req SelectShippingRateRequest) (*Cart, error) {
	var result Cart
	if err := c.doJSON(ctx, http.MethodPost, "/cart/select-shipping-rate", nil, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
