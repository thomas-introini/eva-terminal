// Package woo provides a client for the WooCommerce REST API.
package woo

// Product represents a WooCommerce product (simple or variable).
type Product struct {
	ID             int         `json:"id"`
	Name           string      `json:"name"`
	Type           string      `json:"type"` // "simple" or "variable"
	Status         string      `json:"status"`
	Description    string      `json:"description"`
	ShortDescription string    `json:"short_description"`
	Price          string      `json:"price"`
	RegularPrice   string      `json:"regular_price"`
	SalePrice      string      `json:"sale_price"`
	StockStatus    string      `json:"stock_status"` // "instock", "outofstock", "onbackorder"
	StockQuantity  *int        `json:"stock_quantity"`
	Attributes     []Attribute `json:"attributes"`
	Variations     []int       `json:"variations"` // IDs of variations for variable products
}

// Variation represents a product variation (e.g., 250g or 1kg version).
type Variation struct {
	ID            int                 `json:"id"`
	Price         string              `json:"price"`
	RegularPrice  string              `json:"regular_price"`
	SalePrice     string              `json:"sale_price"`
	StockStatus   string              `json:"stock_status"`
	StockQuantity *int                `json:"stock_quantity"`
	Attributes    []VariationAttribute `json:"attributes"`
}

// Attribute represents a product attribute (e.g., "Grind Size" or "Weight").
type Attribute struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Position  int      `json:"position"`
	Visible   bool     `json:"visible"`
	Variation bool     `json:"variation"` // True if used for variations
	Options   []string `json:"options"`
}

// VariationAttribute represents an attribute value for a specific variation.
type VariationAttribute struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Option string `json:"option"` // The selected value
}

// IsInStock returns true if the product is in stock.
func (p *Product) IsInStock() bool {
	return p.StockStatus == "instock"
}

// IsVariable returns true if the product is a variable product.
func (p *Product) IsVariable() bool {
	return p.Type == "variable"
}

// GetDisplayPrice returns the price to display (sale price if available).
func (p *Product) GetDisplayPrice() string {
	if p.SalePrice != "" {
		return p.SalePrice
	}
	if p.Price != "" {
		return p.Price
	}
	return p.RegularPrice
}

// GetAttribute returns the attribute with the given name, or nil if not found.
func (p *Product) GetAttribute(name string) *Attribute {
	for i := range p.Attributes {
		if p.Attributes[i].Name == name {
			return &p.Attributes[i]
		}
	}
	return nil
}

// IsInStock returns true if the variation is in stock.
func (v *Variation) IsInStock() bool {
	return v.StockStatus == "instock"
}

// GetDisplayPrice returns the price to display for a variation.
func (v *Variation) GetDisplayPrice() string {
	if v.SalePrice != "" {
		return v.SalePrice
	}
	if v.Price != "" {
		return v.Price
	}
	return v.RegularPrice
}

// GetAttributeValue returns the value for an attribute by name.
func (v *Variation) GetAttributeValue(name string) string {
	for _, attr := range v.Attributes {
		if attr.Name == name {
			return attr.Option
		}
	}
	return ""
}

// ============================================
// Order Types
// ============================================

// OrderRequest represents the data needed to create a WooCommerce order.
type OrderRequest struct {
	PaymentMethod      string           `json:"payment_method"`
	PaymentMethodTitle string           `json:"payment_method_title"`
	SetPaid            bool             `json:"set_paid"`
	Billing            BillingAddress   `json:"billing"`
	Shipping           *BillingAddress  `json:"shipping,omitempty"`
	LineItems          []OrderLineItem  `json:"line_items"`
	ShippingLines      []ShippingLine   `json:"shipping_lines,omitempty"`
}

// ShippingLine represents a shipping line in an order.
type ShippingLine struct {
	MethodID    string `json:"method_id"`
	MethodTitle string `json:"method_title"`
	Total       string `json:"total"`
}

// BillingAddress represents the billing address for an order.
type BillingAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone,omitempty"`
	Address1  string `json:"address_1"`
	Address2  string `json:"address_2,omitempty"`
	City      string `json:"city"`
	State     string `json:"state,omitempty"`
	Postcode  string `json:"postcode"`
	Country   string `json:"country"`
}

// OrderLineItem represents a line item in an order.
type OrderLineItem struct {
	ProductID   int                     `json:"product_id"`
	VariationID int                     `json:"variation_id,omitempty"`
	Quantity    int                     `json:"quantity"`
	MetaData    []OrderLineItemMetaData `json:"meta_data,omitempty"`
}

// OrderLineItemMetaData represents metadata for a line item (e.g., grind size).
type OrderLineItemMetaData struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// OrderResponse represents the response from creating an order.
type OrderResponse struct {
	ID            int             `json:"id"`
	Status        string          `json:"status"`
	Currency      string          `json:"currency"`
	Total         string          `json:"total"`
	TotalTax      string          `json:"total_tax"`
	Billing       BillingAddress  `json:"billing"`
	LineItems     []OrderLineItem `json:"line_items"`
	DateCreated   string          `json:"date_created"`
	OrderKey      string          `json:"order_key"`
	PaymentMethod string          `json:"payment_method"`
}

// PaymentGateway represents an available payment method.
type PaymentGateway struct {
	ID                string                 `json:"id"`
	Title             string                 `json:"title"`
	Description       string                 `json:"description"`
	Order             int                    `json:"order"`
	Enabled           bool                   `json:"enabled"`
	MethodTitle       string                 `json:"method_title"`
	MethodDescription string                 `json:"method_description"`
	MethodSupports    []string               `json:"method_supports"`
	Settings          map[string]interface{} `json:"settings"`
	NeedsSetup        bool                   `json:"needs_setup"`
}
