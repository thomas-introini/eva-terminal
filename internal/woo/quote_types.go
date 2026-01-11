package woo

import "time"

// ============================================
// Quote API Types
// ============================================

// QuoteRequest represents a request to create a quote.
type QuoteRequest struct {
	Items           []QuoteItem  `json:"items"`
	Coupons         []string     `json:"coupons,omitempty"`
	ShippingAddress QuoteAddress `json:"shipping_address,omitempty"`
	CustomerID      int          `json:"customer_id,omitempty"`
}

// QuoteItem represents an item in a quote request.
type QuoteItem struct {
	ProductID   int               `json:"product_id"`
	VariationID int               `json:"variation_id,omitempty"`
	Quantity    int               `json:"quantity"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// QuoteAddress represents an address for quote/order operations.
type QuoteAddress struct {
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Company   string `json:"company,omitempty"`
	Address1  string `json:"address_1,omitempty"`
	Address2  string `json:"address_2,omitempty"`
	City      string `json:"city,omitempty"`
	State     string `json:"state,omitempty"`
	Postcode  string `json:"postcode,omitempty"`
	Country   string `json:"country,omitempty"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
}

// QuoteResponse represents the response from creating a quote.
type QuoteResponse struct {
	QuoteID       string              `json:"quote_id"`
	ExpiresAt     time.Time           `json:"expires_at"`
	LineItems     []QuoteLineItem     `json:"line_items"`
	Coupons       []QuoteCoupon       `json:"coupons"`
	ShippingRates []QuoteShippingRate `json:"shipping_rates"`
	Totals        QuoteTotals         `json:"totals"`
	Currency      QuoteCurrency       `json:"currency"`
	StockStatus   []StockCheck        `json:"stock_status"`
}

// QuoteLineItem represents a line item in a quote response.
type QuoteLineItem struct {
	ProductID   int               `json:"product_id"`
	VariationID int               `json:"variation_id"`
	Name        string            `json:"name"`
	SKU         string            `json:"sku"`
	Quantity    int               `json:"quantity"`
	UnitPrice   string            `json:"unit_price"`
	LineTotal   string            `json:"line_total"`
	LineTax     string            `json:"line_tax"`
	Meta        map[string]string `json:"meta"`
}

// QuoteCoupon represents a coupon validation result.
type QuoteCoupon struct {
	Code         string `json:"code"`
	Valid        bool   `json:"valid"`
	Discount     string `json:"discount"`
	DiscountType string `json:"discount_type,omitempty"`
	Amount       string `json:"amount,omitempty"`
	FreeShipping bool   `json:"free_shipping,omitempty"`
	Description  string `json:"description,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
}

// QuoteShippingRate represents a shipping rate option.
type QuoteShippingRate struct {
	RateID     string                 `json:"rate_id"`
	MethodID   string                 `json:"method_id"`
	InstanceID int                    `json:"instance_id"`
	Label      string                 `json:"label"`
	Cost       string                 `json:"cost"`
	Tax        string                 `json:"tax"`
	MetaData   map[string]interface{} `json:"meta_data,omitempty"`
}

// QuoteTotals represents the calculated totals.
type QuoteTotals struct {
	Subtotal string `json:"subtotal"`
	Discount string `json:"discount"`
	Shipping string `json:"shipping"`
	Tax      string `json:"tax"`
	Total    string `json:"total"`
}

// QuoteCurrency represents currency information.
type QuoteCurrency struct {
	Code     string `json:"code"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
}

// StockCheck represents stock availability for a product.
type StockCheck struct {
	ProductID int  `json:"product_id"`
	Available int  `json:"available"`
	Requested int  `json:"requested"`
	OK        bool `json:"ok"`
}

// ============================================
// Coupon Validation Types
// ============================================

// CouponValidateRequest represents a coupon validation request.
type CouponValidateRequest struct {
	Code  string      `json:"code"`
	Items []QuoteItem `json:"items,omitempty"`
}

// CouponValidateResponse represents a coupon validation response.
type CouponValidateResponse struct {
	Code         string `json:"code"`
	Valid        bool   `json:"valid"`
	Discount     string `json:"discount"`
	DiscountType string `json:"discount_type,omitempty"`
	Amount       string `json:"amount,omitempty"`
	FreeShipping bool   `json:"free_shipping,omitempty"`
	Description  string `json:"description,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
}

// ============================================
// Order Creation Types
// ============================================

// CreateOrderRequest represents a request to create an order.
type CreateOrderRequest struct {
	QuoteID         string       `json:"quote_id"`
	IdempotencyKey  string       `json:"idempotency_key,omitempty"`
	ShippingRateID  string       `json:"shipping_rate_id,omitempty"`
	BillingAddress  QuoteAddress `json:"billing_address"`
	ShippingAddress QuoteAddress `json:"shipping_address,omitempty"`
	CustomerEmail   string       `json:"customer_email,omitempty"`
	PaymentMethod   string       `json:"payment_method"`
	CustomerNote    string       `json:"customer_note,omitempty"`
	SetPaid         bool         `json:"set_paid,omitempty"`
}

// CreateOrderResponse represents the response from creating an order.
type CreateOrderResponse struct {
	OrderID    int         `json:"order_id"`
	OrderKey   string      `json:"order_key"`
	Status     string      `json:"status"`
	Totals     OrderTotals `json:"totals"`
	PaymentURL string      `json:"payment_url"`
	NextAction string      `json:"next_action"`
	Created    bool        `json:"created"`
}

// OrderTotals represents order total amounts.
type OrderTotals struct {
	Subtotal string `json:"subtotal"`
	Shipping string `json:"shipping"`
	Discount string `json:"discount"`
	Tax      string `json:"tax"`
	Total    string `json:"total"`
}

// ============================================
// API Error Types
// ============================================

// APIError represents an error response from the EVA API.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status,omitempty"`
}

// Error implements the error interface.
func (e APIError) Error() string {
	return e.Message
}

// ============================================
// Health Check Types
// ============================================

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	WC      string `json:"wc"`
}

// ============================================
// Helper Methods
// ============================================

// IsExpired returns true if the quote has expired.
func (q *QuoteResponse) IsExpired() bool {
	return time.Now().After(q.ExpiresAt)
}

// GetShippingRate returns the shipping rate with the given ID, or nil if not found.
func (q *QuoteResponse) GetShippingRate(rateID string) *QuoteShippingRate {
	for i := range q.ShippingRates {
		if q.ShippingRates[i].RateID == rateID {
			return &q.ShippingRates[i]
		}
	}
	return nil
}

// HasValidCoupons returns true if any coupons are valid.
func (q *QuoteResponse) HasValidCoupons() bool {
	for _, c := range q.Coupons {
		if c.Valid {
			return true
		}
	}
	return false
}

// HasStockIssues returns true if any items have stock problems.
func (q *QuoteResponse) HasStockIssues() bool {
	for _, s := range q.StockStatus {
		if !s.OK {
			return true
		}
	}
	return false
}

// NeedsShipping returns true if the quote requires shipping selection.
func (q *QuoteResponse) NeedsShipping() bool {
	return len(q.ShippingRates) > 0
}

// FormatPrice formats a price string from minor units.
func (q *QuoteResponse) FormatPrice(minorUnits string) string {
	return FormatPrice(minorUnits, q.Currency.Symbol, q.Currency.Decimals)
}
