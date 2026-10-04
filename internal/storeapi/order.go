package storeapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// GetOrder fetches an order using order key and optional guest email.
func (c *Client) GetOrder(ctx context.Context, orderID int, params GetOrderParams) (*StoreOrder, error) {
	if params.Key == "" {
		return nil, errors.New("order key is required")
	}
	query := url.Values{}
	query.Set("key", params.Key)
	if params.BillingEmail != "" {
		query.Set("billing_email", params.BillingEmail)
	}

	var result StoreOrder
	path := "/order/" + strconv.Itoa(orderID)
	if err := c.doJSON(ctx, http.MethodGet, path, query, nil, &result); err != nil {
		return nil, err
	}
	if result.ID == 0 {
		result.ID = result.OrderID
	}
	return &result, nil
}
