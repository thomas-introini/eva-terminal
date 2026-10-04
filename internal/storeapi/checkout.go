package storeapi

import (
	"context"
	"net/http"
	"strconv"
)

// GetCheckout fetches checkout state.
func (c *Client) GetCheckout(ctx context.Context) (*CheckoutState, error) {
	var result CheckoutState
	if err := c.doJSON(ctx, http.MethodGet, "/checkout", nil, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Checkout creates an order from current session cart.
func (c *Client) Checkout(ctx context.Context, req CheckoutRequest) (*CheckoutResponse, error) {
	var result CheckoutResponse
	if err := c.doJSON(ctx, http.MethodPost, "/checkout", nil, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ProcessCheckoutOrder processes payment for an order checkout id.
func (c *Client) ProcessCheckoutOrder(ctx context.Context, checkoutID int, paymentData []KeyValue) (*CheckoutResponse, error) {
	payload := struct {
		PaymentData []KeyValue `json:"payment_data,omitempty"`
	}{
		PaymentData: paymentData,
	}
	var result CheckoutResponse
	path := "/checkout/" + strconv.Itoa(checkoutID)
	if err := c.doJSON(ctx, http.MethodPost, path, nil, payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
