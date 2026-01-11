package woo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// QuoteClient handles EVA Checkout API requests.
type QuoteClient struct {
	client *Client
}

// NewQuoteClient creates a new Quote API client.
func NewQuoteClient(client *Client) *QuoteClient {
	return &QuoteClient{
		client: client,
	}
}

// ============================================
// Quote Operations
// ============================================

// CreateQuote creates a new quote with shipping/tax calculations.
func (q *QuoteClient) CreateQuote(ctx context.Context, req QuoteRequest) (*QuoteResponse, error) {
	var quote QuoteResponse
	if err := q.doRequest(ctx, http.MethodPost, "/wp-json/eva/v1/quote", req, &quote); err != nil {
		return nil, fmt.Errorf("creating quote: %w", err)
	}
	return &quote, nil
}

// GetQuote retrieves an existing quote by ID.
func (q *QuoteClient) GetQuote(ctx context.Context, quoteID string) (*QuoteResponse, error) {
	var quote QuoteResponse
	endpoint := fmt.Sprintf("/wp-json/eva/v1/quote/%s", url.PathEscape(quoteID))
	if err := q.doRequest(ctx, http.MethodGet, endpoint, nil, &quote); err != nil {
		return nil, fmt.Errorf("getting quote: %w", err)
	}
	return &quote, nil
}

// ============================================
// Coupon Operations
// ============================================

// ValidateCoupon validates a coupon code against items.
func (q *QuoteClient) ValidateCoupon(ctx context.Context, code string, items []QuoteItem) (*CouponValidateResponse, error) {
	req := CouponValidateRequest{
		Code:  code,
		Items: items,
	}

	var resp CouponValidateResponse
	if err := q.doRequest(ctx, http.MethodPost, "/wp-json/eva/v1/coupon/validate", req, &resp); err != nil {
		return nil, fmt.Errorf("validating coupon: %w", err)
	}
	return &resp, nil
}

// ============================================
// Order Operations
// ============================================

// CreateOrder creates an order from a confirmed quote.
func (q *QuoteClient) CreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResponse, error) {
	var resp CreateOrderResponse
	if err := q.doRequest(ctx, http.MethodPost, "/wp-json/eva/v1/order", req, &resp); err != nil {
		return nil, fmt.Errorf("creating order: %w", err)
	}
	return &resp, nil
}

// ============================================
// Health Check
// ============================================

// HealthCheck checks if the EVA API is available.
func (q *QuoteClient) HealthCheck(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := q.doRequest(ctx, http.MethodGet, "/wp-json/eva/v1/health", nil, &resp); err != nil {
		return nil, fmt.Errorf("health check: %w", err)
	}
	return &resp, nil
}

// ============================================
// Internal HTTP Methods
// ============================================

// doRequest performs an HTTP request to the EVA API.
func (q *QuoteClient) doRequest(ctx context.Context, method, endpoint string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	// Build URL with authentication
	reqURL := q.client.baseURL + endpoint
	query := url.Values{}
	if q.client.consumerKey != "" && q.client.consumerSecret != "" {
		query.Set("consumer_key", q.client.consumerKey)
		query.Set("consumer_secret", q.client.consumerSecret)
	}
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := q.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	// Check for errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to parse as API error
		var apiErr APIError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Code != "" {
			apiErr.Status = resp.StatusCode
			return apiErr
		}
		return fmt.Errorf("EVA API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

// ============================================
// Convenience Methods
// ============================================

// QuoteAndOrder is a convenience method that creates a quote and immediately creates an order.
// This is useful for simple checkout flows where shipping is pre-selected.
func (q *QuoteClient) QuoteAndOrder(ctx context.Context, quoteReq QuoteRequest, orderReq CreateOrderRequest) (*CreateOrderResponse, error) {
	// Create quote
	quote, err := q.CreateQuote(ctx, quoteReq)
	if err != nil {
		return nil, fmt.Errorf("quote step: %w", err)
	}

	// Check for stock issues
	if quote.HasStockIssues() {
		return nil, fmt.Errorf("stock issues detected")
	}

	// Check quote expiry
	if quote.IsExpired() {
		return nil, fmt.Errorf("quote has expired")
	}

	// Set quote ID in order request
	orderReq.QuoteID = quote.QuoteID

	// Create order
	return q.CreateOrder(ctx, orderReq)
}

// BuildQuoteRequest is a helper to build a QuoteRequest from common parameters.
func BuildQuoteRequest(items []QuoteItem, coupons []string, shippingAddress QuoteAddress) QuoteRequest {
	return QuoteRequest{
		Items:           items,
		Coupons:         coupons,
		ShippingAddress: shippingAddress,
	}
}

// BuildOrderRequest is a helper to build a CreateOrderRequest.
func BuildOrderRequest(
	quoteID string,
	shippingRateID string,
	billing QuoteAddress,
	shipping QuoteAddress,
	paymentMethod string,
) CreateOrderRequest {
	return CreateOrderRequest{
		QuoteID:         quoteID,
		ShippingRateID:  shippingRateID,
		BillingAddress:  billing,
		ShippingAddress: shipping,
		PaymentMethod:   paymentMethod,
	}
}
