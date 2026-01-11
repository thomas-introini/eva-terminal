package woo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// StoreAPIClient handles WooCommerce Store API requests.
// It maintains a cart token for session persistence.
type StoreAPIClient struct {
	client    *Client
	cartToken string
	mu        sync.RWMutex
}

// NewStoreAPIClient creates a new Store API client.
func NewStoreAPIClient(client *Client) *StoreAPIClient {
	return &StoreAPIClient{
		client: client,
	}
}

// GetCartToken returns the current cart token.
func (s *StoreAPIClient) GetCartToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cartToken
}

// SetCartToken sets the cart token (used when restoring a session).
func (s *StoreAPIClient) SetCartToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cartToken = token
}

// ClearCart clears the cart token (starts a new session).
func (s *StoreAPIClient) ClearCart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cartToken = ""
}

// ============================================
// Cart Operations
// ============================================

// GetCart retrieves the current cart state.
func (s *StoreAPIClient) GetCart(ctx context.Context) (*StoreCart, error) {
	var cart StoreCart
	if err := s.doStoreRequest(ctx, "GET", "/wp-json/wc/store/v1/cart", nil, &cart); err != nil {
		return nil, err
	}
	return &cart, nil
}

// AddToCart adds an item to the cart.
func (s *StoreAPIClient) AddToCart(ctx context.Context, req AddToCartRequest) (*StoreCart, error) {
	var cart StoreCart
	if err := s.doStoreRequest(ctx, "POST", "/wp-json/wc/store/v1/cart/add-item", req, &cart); err != nil {
		return nil, err
	}
	return &cart, nil
}

// UpdateCartItem updates the quantity of a cart item.
func (s *StoreAPIClient) UpdateCartItem(ctx context.Context, req UpdateCartItemRequest) (*StoreCart, error) {
	var cart StoreCart
	if err := s.doStoreRequest(ctx, "POST", "/wc/store/v1/cart/update-item", req, &cart); err != nil {
		return nil, err
	}
	return &cart, nil
}

// RemoveCartItem removes an item from the cart.
func (s *StoreAPIClient) RemoveCartItem(ctx context.Context, req RemoveCartItemRequest) (*StoreCart, error) {
	var cart StoreCart
	if err := s.doStoreRequest(ctx, "POST", "/wc/store/v1/cart/remove-item", req, &cart); err != nil {
		return nil, err
	}
	return &cart, nil
}

// ApplyCoupon applies a coupon code to the cart.
func (s *StoreAPIClient) ApplyCoupon(ctx context.Context, code string) (*StoreCart, error) {
	var cart StoreCart
	req := ApplyCouponRequest{Code: code}
	if err := s.doStoreRequest(ctx, "POST", "/wc/store/v1/cart/apply-coupon", req, &cart); err != nil {
		return nil, err
	}
	return &cart, nil
}

// RemoveCoupon removes a coupon from the cart.
func (s *StoreAPIClient) RemoveCoupon(ctx context.Context, code string) (*StoreCart, error) {
	var cart StoreCart
	req := ApplyCouponRequest{Code: code}
	if err := s.doStoreRequest(ctx, "POST", "/wc/store/v1/cart/remove-coupon", req, &cart); err != nil {
		return nil, err
	}
	return &cart, nil
}

// ============================================
// Shipping Operations
// ============================================

// SelectShippingRate selects a shipping rate for a package.
func (s *StoreAPIClient) SelectShippingRate(ctx context.Context, packageID int, rateID string) (*StoreCart, error) {
	var cart StoreCart
	req := SelectShippingRateRequest{
		PackageID: packageID,
		RateID:    rateID,
	}
	if err := s.doStoreRequest(ctx, "POST", "/wc/store/v1/cart/select-shipping-rate", req, &cart); err != nil {
		return nil, err
	}
	return &cart, nil
}

// UpdateCustomer updates the customer billing and shipping addresses.
// This triggers shipping rate recalculation.
func (s *StoreAPIClient) UpdateCustomer(ctx context.Context, billing, shipping StoreAddress) (*StoreCart, error) {
	var cart StoreCart
	req := UpdateCustomerRequest{
		BillingAddress:  billing,
		ShippingAddress: shipping,
	}
	if err := s.doStoreRequest(ctx, "POST", "/wc/store/v1/cart/update-customer", req, &cart); err != nil {
		return nil, err
	}
	return &cart, nil
}

// ============================================
// Checkout Operations
// ============================================

// GetCheckout retrieves checkout data including available payment methods.
func (s *StoreAPIClient) GetCheckout(ctx context.Context) (*CheckoutResponse, error) {
	var checkout CheckoutResponse
	if err := s.doStoreRequest(ctx, "GET", "/wc/store/v1/checkout", nil, &checkout); err != nil {
		return nil, err
	}
	return &checkout, nil
}

// PlaceOrder places the order and processes payment.
func (s *StoreAPIClient) PlaceOrder(ctx context.Context, req CheckoutRequest) (*CheckoutResponse, error) {
	var checkout CheckoutResponse
	if err := s.doStoreRequest(ctx, "POST", "/wc/store/v1/checkout", req, &checkout); err != nil {
		return nil, err
	}
	return &checkout, nil
}

// GetPaymentGateways retrieves available payment gateways.
func (s *StoreAPIClient) GetPaymentGateways(ctx context.Context) ([]PaymentGateway, error) {
	var gateways []PaymentGateway
	// Use the REST API v3 for payment gateways as Store API doesn't expose them directly
	if err := s.client.doRequest(ctx, "/wp-json/wc/v3/payment_gateways", nil, &gateways); err != nil {
		return nil, err
	}
	// Filter to only enabled gateways
	var enabled []PaymentGateway
	for _, gw := range gateways {
		if gw.Enabled {
			enabled = append(enabled, gw)
		}
	}
	return enabled, nil
}

// ============================================
// Internal HTTP Methods
// ============================================

// doStoreRequest performs a Store API request with cart token handling.
func (s *StoreAPIClient) doStoreRequest(ctx context.Context, method, endpoint string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	reqURL := s.client.baseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Add cart token if we have one
	s.mu.RLock()
	if s.cartToken != "" {
		req.Header.Set("Cart-Token", s.cartToken)
	}
	s.mu.RUnlock()

	// Add nonce header for Store API (required for some operations)
	req.Header.Set("Nonce", "")

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	// Extract and save cart token from response
	if cartToken := resp.Header.Get("Cart-Token"); cartToken != "" {
		s.mu.Lock()
		s.cartToken = cartToken
		s.mu.Unlock()
	}
	// Also check X-WC-Store-API-Nonce
	if nonce := resp.Header.Get("X-WC-Store-API-Nonce"); nonce != "" {
		// Store nonce for future requests if needed
	}

	// Check for errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Store API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

// ============================================
// Helper Methods
// ============================================

// FormatPrice formats a price string from minor units to display format.
func FormatPrice(priceMinorUnits string, currencySymbol string, minorUnit int) string {
	// Price comes as string in minor units (e.g., "1999" for $19.99)
	// This is a simple implementation - production would need more robust handling
	if priceMinorUnits == "" || priceMinorUnits == "0" {
		return currencySymbol + "0.00"
	}

	// Parse the price
	var cents int64
	fmt.Sscanf(priceMinorUnits, "%d", &cents)

	// Convert to dollars
	divisor := int64(1)
	for i := 0; i < minorUnit; i++ {
		divisor *= 10
	}

	dollars := float64(cents) / float64(divisor)
	return fmt.Sprintf("%s%.2f", currencySymbol, dollars)
}

// GetSelectedShippingRate returns the currently selected shipping rate, if any.
func GetSelectedShippingRate(cart *StoreCart) *ShippingRate {
	for _, pkg := range cart.ShippingRates {
		for i := range pkg.ShippingRates {
			if pkg.ShippingRates[i].Selected {
				return &pkg.ShippingRates[i]
			}
		}
	}
	return nil
}

// GetAllShippingRates returns all available shipping rates across packages.
func GetAllShippingRates(cart *StoreCart) []ShippingRate {
	var rates []ShippingRate
	for _, pkg := range cart.ShippingRates {
		rates = append(rates, pkg.ShippingRates...)
	}
	return rates
}
