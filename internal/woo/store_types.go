package woo

// ============================================
// WooCommerce Store API Types
// Based on /wc/store/v1/ endpoints
// ============================================

// StoreCart represents the cart from the Store API.
type StoreCart struct {
	Coupons               []StoreCoupon       `json:"coupons"`
	ShippingRates         []ShippingPackage   `json:"shipping_rates"`
	ShippingAddress       StoreAddress        `json:"shipping_address"`
	BillingAddress        StoreAddress        `json:"billing_address"`
	Items                 []StoreCartItem     `json:"items"`
	ItemsCount            int                 `json:"items_count"`
	ItemsWeight           float64             `json:"items_weight"`
	CrossSells            []interface{}       `json:"cross_sells"`
	NeedsPayment          bool                `json:"needs_payment"`
	NeedsShipping         bool                `json:"needs_shipping"`
	HasCalculatedShipping bool                `json:"has_calculated_shipping"`
	Totals                CartTotals          `json:"totals"`
	Errors                []StoreError        `json:"errors"`
	PaymentMethods        []string            `json:"payment_methods"`
	PaymentRequirements   []string            `json:"payment_requirements"`
	Extensions            map[string]interface{} `json:"extensions"`
}

// StoreCartItem represents an item in the Store API cart.
type StoreCartItem struct {
	Key               string                 `json:"key"`
	ID                int                    `json:"id"`
	Quantity          int                    `json:"quantity"`
	QuantityLimits    QuantityLimits         `json:"quantity_limits"`
	Name              string                 `json:"name"`
	ShortDescription  string                 `json:"short_description"`
	Description       string                 `json:"description"`
	SKU               string                 `json:"sku"`
	LowStockRemaining *int                   `json:"low_stock_remaining"`
	BackordersAllowed bool                   `json:"backorders_allowed"`
	ShowBackorderBadge bool                  `json:"show_backorder_badge"`
	SoldIndividually  bool                   `json:"sold_individually"`
	Permalink         string                 `json:"permalink"`
	Images            []CartItemImage        `json:"images"`
	Variation         []CartItemVariation    `json:"variation"`
	ItemData          []CartItemData         `json:"item_data"`
	Prices            CartItemPrices         `json:"prices"`
	Totals            CartItemTotals         `json:"totals"`
	CatalogVisibility string                 `json:"catalog_visibility"`
	Extensions        map[string]interface{} `json:"extensions"`
}

// QuantityLimits defines min/max quantity for a cart item.
type QuantityLimits struct {
	Minimum     int  `json:"minimum"`
	Maximum     int  `json:"maximum"`
	MultipleOf  int  `json:"multiple_of"`
	Editable    bool `json:"editable"`
}

// CartItemImage represents a product image.
type CartItemImage struct {
	ID        int    `json:"id"`
	Src       string `json:"src"`
	Thumbnail string `json:"thumbnail"`
	Srcset    string `json:"srcset"`
	Sizes     string `json:"sizes"`
	Name      string `json:"name"`
	Alt       string `json:"alt"`
}

// CartItemVariation represents a variation attribute.
type CartItemVariation struct {
	Attribute string `json:"attribute"`
	Value     string `json:"value"`
}

// CartItemData represents additional item metadata.
type CartItemData struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Display string `json:"display"`
}

// CartItemPrices represents pricing info for a cart item.
type CartItemPrices struct {
	Price         string       `json:"price"`
	RegularPrice  string       `json:"regular_price"`
	SalePrice     string       `json:"sale_price"`
	PriceRange    *PriceRange  `json:"price_range"`
	CurrencyCode  string       `json:"currency_code"`
	CurrencySymbol string      `json:"currency_symbol"`
	CurrencyMinorUnit int      `json:"currency_minor_unit"`
	CurrencyDecimalSeparator string `json:"currency_decimal_separator"`
	CurrencyThousandSeparator string `json:"currency_thousand_separator"`
	CurrencyPrefix string      `json:"currency_prefix"`
	CurrencySuffix string      `json:"currency_suffix"`
	RawPrices     RawPrices    `json:"raw_prices"`
}

// PriceRange for variable products.
type PriceRange struct {
	MinAmount string `json:"min_amount"`
	MaxAmount string `json:"max_amount"`
}

// RawPrices contains unformatted prices.
type RawPrices struct {
	Precision    int    `json:"precision"`
	Price        string `json:"price"`
	RegularPrice string `json:"regular_price"`
	SalePrice    string `json:"sale_price"`
}

// CartItemTotals represents totals for a cart item.
type CartItemTotals struct {
	LineSubtotal    string `json:"line_subtotal"`
	LineSubtotalTax string `json:"line_subtotal_tax"`
	LineTotal       string `json:"line_total"`
	LineTotalTax    string `json:"line_total_tax"`
	CurrencyCode    string `json:"currency_code"`
	CurrencySymbol  string `json:"currency_symbol"`
	CurrencyMinorUnit int  `json:"currency_minor_unit"`
	CurrencyDecimalSeparator string `json:"currency_decimal_separator"`
	CurrencyThousandSeparator string `json:"currency_thousand_separator"`
	CurrencyPrefix  string `json:"currency_prefix"`
	CurrencySuffix  string `json:"currency_suffix"`
}

// CartTotals represents the cart totals.
type CartTotals struct {
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
	TaxLines          []TaxLine `json:"tax_lines"`
	CurrencyCode      string `json:"currency_code"`
	CurrencySymbol    string `json:"currency_symbol"`
	CurrencyMinorUnit int    `json:"currency_minor_unit"`
	CurrencyDecimalSeparator string `json:"currency_decimal_separator"`
	CurrencyThousandSeparator string `json:"currency_thousand_separator"`
	CurrencyPrefix    string `json:"currency_prefix"`
	CurrencySuffix    string `json:"currency_suffix"`
}

// TaxLine represents a tax line in totals.
type TaxLine struct {
	Name  string `json:"name"`
	Price string `json:"price"`
	Rate  string `json:"rate"`
}

// StoreCoupon represents an applied coupon.
type StoreCoupon struct {
	Code        string `json:"code"`
	DiscountType string `json:"discount_type"`
	Totals      CouponTotals `json:"totals"`
}

// CouponTotals represents coupon discount totals.
type CouponTotals struct {
	TotalDiscount    string `json:"total_discount"`
	TotalDiscountTax string `json:"total_discount_tax"`
	CurrencyCode     string `json:"currency_code"`
}

// ShippingPackage represents a shipping package with rates.
type ShippingPackage struct {
	PackageID     int             `json:"package_id"`
	Name          string          `json:"name"`
	Destination   ShippingDestination `json:"destination"`
	Items         []ShippingItem  `json:"items"`
	ShippingRates []ShippingRate  `json:"shipping_rates"`
}

// ShippingDestination is the shipping address for rate calculation.
type ShippingDestination struct {
	Address1 string `json:"address_1"`
	Address2 string `json:"address_2"`
	City     string `json:"city"`
	State    string `json:"state"`
	Postcode string `json:"postcode"`
	Country  string `json:"country"`
}

// ShippingItem represents an item in a shipping package.
type ShippingItem struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

// ShippingRate represents a shipping rate option.
type ShippingRate struct {
	RateID       string `json:"rate_id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	DeliveryTime string `json:"delivery_time"`
	Price        string `json:"price"`
	Taxes        string `json:"taxes"`
	InstanceID   int    `json:"instance_id"`
	MethodID     string `json:"method_id"`
	MetaData     []ShippingRateMeta `json:"meta_data"`
	Selected     bool   `json:"selected"`
	CurrencyCode string `json:"currency_code"`
	CurrencySymbol string `json:"currency_symbol"`
	CurrencyMinorUnit int `json:"currency_minor_unit"`
	CurrencyDecimalSeparator string `json:"currency_decimal_separator"`
	CurrencyThousandSeparator string `json:"currency_thousand_separator"`
	CurrencyPrefix string `json:"currency_prefix"`
	CurrencySuffix string `json:"currency_suffix"`
}

// ShippingRateMeta is metadata for a shipping rate.
type ShippingRateMeta struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// StoreAddress represents a billing or shipping address.
type StoreAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Company   string `json:"company"`
	Address1  string `json:"address_1"`
	Address2  string `json:"address_2"`
	City      string `json:"city"`
	State     string `json:"state"`
	Postcode  string `json:"postcode"`
	Country   string `json:"country"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone"`
}

// StoreError represents an error from the Store API.
type StoreError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ============================================
// Checkout Types
// ============================================

// CheckoutResponse represents the response from GET /checkout.
type CheckoutResponse struct {
	OrderID        int              `json:"order_id"`
	Status         string           `json:"status"`
	OrderKey       string           `json:"order_key"`
	CustomerNote   string           `json:"customer_note"`
	CustomerID     int              `json:"customer_id"`
	BillingAddress StoreAddress     `json:"billing_address"`
	ShippingAddress StoreAddress    `json:"shipping_address"`
	PaymentMethod  string           `json:"payment_method"`
	PaymentResult  *PaymentResult   `json:"payment_result,omitempty"`
}

// PaymentResult contains payment processing result.
type PaymentResult struct {
	PaymentStatus  string              `json:"payment_status"`
	PaymentDetails []PaymentDetail     `json:"payment_details"`
	RedirectURL    string              `json:"redirect_url"`
}

// PaymentDetail contains payment method details.
type PaymentDetail struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// PaymentGateway represents an available payment method.
type PaymentGateway struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Order       int    `json:"order"`
	Enabled     bool   `json:"enabled"`
	MethodTitle string `json:"method_title"`
	MethodDescription string `json:"method_description"`
	MethodSupports []string `json:"method_supports"`
	Settings    map[string]interface{} `json:"settings"`
	NeedsSetup  bool   `json:"needs_setup"`
}

// ============================================
// Request Types
// ============================================

// AddToCartRequest is the request body for adding items to cart.
type AddToCartRequest struct {
	ID        int                    `json:"id"`
	Quantity  int                    `json:"quantity"`
	Variation []CartItemVariation    `json:"variation,omitempty"`
}

// UpdateCartItemRequest is the request body for updating cart items.
type UpdateCartItemRequest struct {
	Key      string `json:"key"`
	Quantity int    `json:"quantity"`
}

// RemoveCartItemRequest is the request body for removing cart items.
type RemoveCartItemRequest struct {
	Key string `json:"key"`
}

// SelectShippingRateRequest is the request for selecting a shipping rate.
type SelectShippingRateRequest struct {
	PackageID int    `json:"package_id"`
	RateID    string `json:"rate_id"`
}

// UpdateCustomerRequest is the request for updating customer addresses.
type UpdateCustomerRequest struct {
	BillingAddress  StoreAddress `json:"billing_address"`
	ShippingAddress StoreAddress `json:"shipping_address"`
}

// CheckoutRequest is the request body for placing an order.
type CheckoutRequest struct {
	BillingAddress   StoreAddress `json:"billing_address"`
	ShippingAddress  StoreAddress `json:"shipping_address"`
	CustomerNote     string       `json:"customer_note,omitempty"`
	PaymentMethod    string       `json:"payment_method"`
	PaymentData      map[string]string `json:"payment_data,omitempty"`
	CreateAccount    bool         `json:"create_account,omitempty"`
}

// ApplyCouponRequest is the request for applying a coupon.
type ApplyCouponRequest struct {
	Code string `json:"code"`
}
