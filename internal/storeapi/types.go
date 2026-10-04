package storeapi

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ProductQuery controls product collection filters.
type ProductQuery struct {
	Page      int
	PerPage   int
	Search    string
	InStock   bool
	Type      string
	ParentIDs []int
}

// Product is a Store API product.
type Product struct {
	ID            int                        `json:"id"`
	Name          string                     `json:"name"`
	Slug          string                     `json:"slug"`
	Type          string                     `json:"type"`
	Description   string                     `json:"description"`
	ShortDesc     string                     `json:"short_description"`
	SKU           string                     `json:"sku"`
	StockStatus   string                     `json:"stock_status"`
	IsInStock     bool                       `json:"is_in_stock"`
	IsPurchasable bool                       `json:"is_purchasable"`
	OnSale        bool                       `json:"on_sale"`
	HasOptions    bool                       `json:"has_options"`
	Prices        ProductPrices              `json:"prices"`
	Images        []ProductImage             `json:"images"`
	Categories    []ProductCategory          `json:"categories"`
	Attributes    []ProductAttribute         `json:"attributes"`
	Variations    []ProductVariation         `json:"variations"`
	Extensions    map[string]json.RawMessage `json:"extensions"`
}

// ProductVariation maps a parent product's variation ID to its attribute values.
type ProductVariation struct {
	ID         int                         `json:"id"`
	Attributes []ProductVariationAttribute `json:"attributes"`
}

// ProductVariationAttribute is an attribute value from a parent variation entry.
type ProductVariationAttribute struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ProductPrices is a Store API price payload.
type ProductPrices struct {
	CurrencyCode      string `json:"currency_code"`
	CurrencyMinorUnit int    `json:"currency_minor_unit"`
	CurrencySymbol    string `json:"currency_symbol"`
	CurrencyPrefix    string `json:"currency_prefix"`
	CurrencySuffix    string `json:"currency_suffix"`
	PriceRange        *struct {
		MinAmount string `json:"min_amount"`
		MaxAmount string `json:"max_amount"`
	} `json:"price_range"`
	Price        string `json:"price"`
	RegularPrice string `json:"regular_price"`
	SalePrice    string `json:"sale_price"`
}

// DisplayPrice returns a decimal string from minor units.
func (p ProductPrices) DisplayPrice() string {
	if p.Price != "" {
		return FormatMinor(p.Price, p.MinorUnit())
	}
	return FormatMinor(p.RegularPrice, p.MinorUnit())
}

func (p ProductPrices) MinorUnit() int {
	if p.CurrencyCode == "" {
		return 2
	} // Legacy fixtures have no currency metadata.
	return p.CurrencyMinorUnit
}

// ProductImage is an image attached to a product.
type ProductImage struct {
	ID        int    `json:"id"`
	Src       string `json:"src"`
	Thumbnail string `json:"thumbnail"`
	Name      string `json:"name"`
	Alt       string `json:"alt"`
}

// ProductCategory is a Store API category.
type ProductCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// ProductAttribute is a Store API product attribute.
type ProductAttribute struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Taxonomy      string `json:"taxonomy"`
	HasVariations bool   `json:"has_variations"`
	Terms         []Term `json:"terms"`
}

// Term is an attribute term.
type Term struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// Cart represents current session cart.
type Cart struct {
	Items           []CartItem        `json:"items"`
	Totals          CartTotals        `json:"totals"`
	Coupons         []CartCoupon      `json:"coupons"`
	ShippingRates   []ShippingPackage `json:"shipping_rates"`
	NeedsPayment    bool              `json:"needs_payment"`
	NeedsShipping   bool              `json:"needs_shipping"`
	PaymentMethods  []string          `json:"payment_methods"`
	ShippingAddress CustomerAddress   `json:"shipping_address"`
	BillingAddress  CustomerAddress   `json:"billing_address"`
	ItemsCount      int               `json:"items_count"`
	ItemsWeight     int               `json:"items_weight"`
	Extensions      map[string]any    `json:"extensions"`
	Errors          []CartError       `json:"errors"`
}

// CartError is a native Woo validation error returned with the cart.
type CartError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CartItem is a line in the Store cart.
type CartItem struct {
	Key            string                     `json:"key"`
	ID             int                        `json:"id"`
	Quantity       int                        `json:"quantity"`
	Name           string                     `json:"name"`
	ShortDesc      string                     `json:"short_description"`
	SKU            string                     `json:"sku"`
	Permalink      string                     `json:"permalink"`
	Images         []ProductImage             `json:"images"`
	Prices         ProductPrices              `json:"prices"`
	Variation      []KeyValue                 `json:"variation"`
	Extensions     map[string]json.RawMessage `json:"extensions"`
	QuantityLimits struct {
		Minimum    int  `json:"minimum"`
		Maximum    int  `json:"maximum"`
		MultipleOf int  `json:"multiple_of"`
		Editable   bool `json:"editable"`
	} `json:"quantity_limits"`
	Totals struct {
		LineTotal    string `json:"line_total"`
		LineTotalTax string `json:"line_total_tax"`
	} `json:"totals"`
}

// KeyValue is a generic key/value pair used by Store API payloads.
type KeyValue struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// CartTotals holds Store API totals as strings.
type CartTotals struct {
	CurrencyCode      string `json:"currency_code"`
	CurrencyMinorUnit int    `json:"currency_minor_unit"`
	TotalItems        string `json:"total_items"`
	TotalItemsTax     string `json:"total_items_tax"`
	TotalFees         string `json:"total_fees"`
	TotalFeesTax      string `json:"total_fees_tax"`
	TotalDiscount     string `json:"total_discount"`
	TotalDiscountTax  string `json:"total_discount_tax"`
	TotalShipping     string `json:"total_shipping"`
	TotalShippingTax  string `json:"total_shipping_tax"`
	TotalPrice        string `json:"total_price"`
	TotalTax          string `json:"total_tax"`
}

// CartCoupon is an applied coupon.
type CartCoupon struct {
	Code string `json:"code"`
}

// ShippingPackage lists available and selected rates.
type ShippingPackage struct {
	PackageID     int             `json:"package_id"`
	Name          string          `json:"name"`
	Destination   CustomerAddress `json:"destination"`
	ShippingRates []ShippingRate  `json:"shipping_rates"`
}

// ShippingRate is a selectable shipping option.
type ShippingRate struct {
	RateID      string `json:"rate_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       string `json:"price"`
	Selected    bool   `json:"selected"`
}

// CustomerAddress is used in cart/checkout payloads.
type CustomerAddress struct {
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

// AddItemRequest adds an item to cart.
type AddItemRequest struct {
	ID         int            `json:"id"`
	Quantity   int            `json:"quantity,omitempty"`
	Variation  []VariationKV  `json:"variation,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// VariationKV stores variation attributes.
type VariationKV struct {
	Attribute string `json:"attribute"`
	Value     string `json:"value"`
}

// UpdateItemRequest updates an existing line.
type UpdateItemRequest struct {
	Key      string `json:"key"`
	Quantity int    `json:"quantity"`
}

// RemoveItemRequest removes a cart line.
type RemoveItemRequest struct {
	Key string `json:"key"`
}

// CouponRequest applies or removes coupons.
type CouponRequest struct {
	Code string `json:"code"`
}

// UpdateCustomerRequest updates checkout customer data.
type UpdateCustomerRequest struct {
	BillingAddress  *CustomerAddress `json:"billing_address,omitempty"`
	ShippingAddress *CustomerAddress `json:"shipping_address,omitempty"`
}

// SelectShippingRateRequest chooses a shipping rate.
type SelectShippingRateRequest struct {
	PackageID int    `json:"package_id"`
	RateID    string `json:"rate_id"`
}

// CheckoutRequest submits checkout.
type CheckoutRequest struct {
	BillingAddress   *CustomerAddress `json:"billing_address,omitempty"`
	ShippingAddress  *CustomerAddress `json:"shipping_address,omitempty"`
	CustomerNote     string           `json:"customer_note,omitempty"`
	PaymentMethod    string           `json:"payment_method,omitempty"`
	PaymentData      []KeyValue       `json:"payment_data,omitempty"`
	CreateAccount    bool             `json:"create_account,omitempty"`
	CustomerPassword string           `json:"customer_password,omitempty"`
}

// CheckoutState is returned by GET /checkout.
type CheckoutState struct {
	PaymentMethods []map[string]any `json:"payment_methods"`
	CartTotals     CartTotals       `json:"totals"`
}

// CheckoutResponse captures common checkout responses.
type CheckoutResponse struct {
	OrderID       int    `json:"order_id"`
	Status        string `json:"status"`
	OrderKey      string `json:"order_key"`
	PaymentResult struct {
		PaymentStatus string `json:"payment_status"`
		RedirectURL   string `json:"redirect_url"`
	} `json:"payment_result"`
	Order StoreOrder `json:"order"`
}

// UnmarshalJSON handles variant checkout response structures.
func (r *CheckoutResponse) UnmarshalJSON(data []byte) error {
	type Alias CheckoutResponse
	aux := &struct {
		*Alias
		ID int `json:"id"`
	}{
		Alias: (*Alias)(r),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if r.OrderID == 0 {
		r.OrderID = aux.ID
	}
	if r.OrderID == 0 && r.Order.ID != 0 {
		r.OrderID = r.Order.ID
	}
	if r.Status == "" && r.Order.Status != "" {
		r.Status = r.Order.Status
	}
	if r.OrderKey == "" && r.Order.OrderKey != "" {
		r.OrderKey = r.Order.OrderKey
	}
	return nil
}

// StoreOrder is the Store API order object.
type StoreOrder struct {
	ID              int             `json:"id"`
	OrderID         int             `json:"order_id"`
	Status          string          `json:"status"`
	OrderKey        string          `json:"order_key"`
	CurrencyCode    string          `json:"currency_code"`
	Totals          CartTotals      `json:"totals"`
	BillingAddress  CustomerAddress `json:"billing_address"`
	ShippingAddress CustomerAddress `json:"shipping_address"`
	Items           []CartItem      `json:"items"`
}

// GetOrderParams are query args for order lookup.
type GetOrderParams struct {
	Key          string
	BillingEmail string
}

func minorUnitsToDecimal(v string) string {
	if v == "" {
		return ""
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return v
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	major := n / 100
	minor := n % 100
	return fmt.Sprintf("%s%d.%02d", sign, major, minor)
}
