package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thomas/eva-terminal-go/internal/storeapi"
)

type mockAttempt struct {
	Owner, Hash string
	Attempt     storeapi.Attempt
	Order       storeapi.StoreOrder
	Created     time.Time
}
type mockStore struct {
	mu        sync.Mutex
	carts     map[string]*storeapi.Cart
	attempts  map[string]*mockAttempt
	nextOrder int
}

func newMockStore() *mockStore {
	return &mockStore{carts: make(map[string]*storeapi.Cart), attempts: make(map[string]*mockAttempt), nextOrder: 1000}
}

func paginate[T any](rows []T, r *http.Request) []T {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per < 1 {
		per = 100
	}
	start := min((page-1)*per, len(rows))
	end := min(start+per, len(rows))
	return rows[start:end]
}

func mockJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func mockError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "data": map[string]int{"status": status}})
}

func (s *mockStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if delay, _ := strconv.Atoi(getEnv("MOCK_API_DELAY_MS", "0")); delay > 0 {
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
	path := strings.Replace(r.URL.Path, "/wc/store/v1/", "/wc/store/", 1)
	if path == "/wp-json/wc/store/products" {
		handleStoreProducts(w, r)
		return
	}
	if path == "/wp-json/wc/v3/products" {
		handleProducts(w, r)
		return
	}
	if strings.HasPrefix(path, "/wp-json/wc/v3/products/") {
		handleProductsWithID(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasPrefix(path, "/wp-json/eva-terminal/v1/") {
		s.bridge(w, r, path)
		return
	}
	if strings.HasPrefix(path, "/wp-json/wc/store/order/") {
		id, _ := strconv.Atoi(strings.TrimPrefix(path, "/wp-json/wc/store/order/"))
		for _, a := range s.attempts {
			if a.Order.ID == id && a.Order.OrderKey == r.URL.Query().Get("key") {
				mockJSON(w, a.Order)
				return
			}
		}
		mockError(w, 404, "order_missing", "Order not found")
		return
	}
	token := r.Header.Get("Cart-Token")
	if token == "" && path == "/wp-json/wc/store/cart" && r.Method == http.MethodGet {
		var b [16]byte
		_, _ = rand.Read(b[:])
		token = hex.EncodeToString(b[:])
		s.carts[token] = &storeapi.Cart{}
	}
	cart := s.carts[token]
	if cart == nil {
		mockError(w, 401, "woocommerce_rest_cart_invalid_token", "Cart token expired")
		return
	}
	w.Header().Set("Cart-Token", token)
	if path == "/wp-json/wc/store/cart" && r.Method == http.MethodGet {
		mockRecalc(cart)
		mockJSON(w, cart)
		return
	}
	if r.Method != http.MethodPost {
		mockError(w, 405, "method", "Use POST")
		return
	}
	switch path {
	case "/wp-json/wc/store/cart/add-item":
		var req storeapi.AddItemRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			mockError(w, 400, "invalid", "Invalid JSON")
			return
		}
		name, price, stock := "", "", false
		for _, p := range products {
			if p.ID == req.ID {
				name, price, stock = p.Name, p.Price, p.IsInStock() && !p.IsVariable()
			}
		}
		for parent, vars := range variationsMap {
			for _, v := range vars {
				if v.ID == req.ID {
					name, price, stock = fmt.Sprintf("Coffee %d (%s)", parent, v.GetAttributeValue("Size")), v.Price, v.IsInStock()
				}
			}
		}
		grind := ""
		if ext, ok := req.Extensions["eva_terminal"].(map[string]any); ok {
			grind, _ = ext["grind"].(string)
		}
		if !stock || req.Quantity < 1 || req.Quantity > 50 {
			mockError(w, 400, "stock", "Product unavailable or quantity exceeds stock")
			return
		}
		if grind != "" && !validMockGrind(req.ID, grind) {
			mockError(w, 400, "grind", "Invalid grind choice")
			return
		}
		key := fmt.Sprintf("%d-%s", req.ID, grind)
		found := false
		for i := range cart.Items {
			if cart.Items[i].Key == key {
				cart.Items[i].Quantity += req.Quantity
				found = true
			}
		}
		if !found {
			ext, _ := json.Marshal(map[string]string{"grind": grind})
			cart.Items = append(cart.Items, storeapi.CartItem{Key: key, ID: req.ID, Quantity: req.Quantity, Name: name, Prices: storeapi.ProductPrices{CurrencyCode: "EUR", CurrencyMinorUnit: 2, Price: toMinorString(price)}, Extensions: map[string]json.RawMessage{"eva_terminal": ext}})
		}
	case "/wp-json/wc/store/cart/update-item":
		var req storeapi.UpdateItemRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Quantity < 1 || req.Quantity > 50 {
			mockError(w, 400, "quantity", "Quantity must be 1–50")
			return
		}
		for i := range cart.Items {
			if cart.Items[i].Key == req.Key {
				cart.Items[i].Quantity = req.Quantity
			}
		}
	case "/wp-json/wc/store/cart/remove-item":
		var req storeapi.RemoveItemRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		for i, item := range cart.Items {
			if item.Key == req.Key {
				cart.Items = append(cart.Items[:i], cart.Items[i+1:]...)
				break
			}
		}
	case "/wp-json/wc/store/cart/update-customer":
		var req storeapi.UpdateCustomerRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.BillingAddress != nil {
			cart.BillingAddress = *req.BillingAddress
		}
		if req.ShippingAddress != nil {
			cart.ShippingAddress = *req.ShippingAddress
		}
	case "/wp-json/wc/store/cart/apply-coupon":
		var req storeapi.CouponRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Code != "COFFEE10" {
			mockError(w, 400, "coupon", "Use mock coupon COFFEE10")
			return
		}
		cart.Coupons = []storeapi.CartCoupon{{Code: req.Code}}
	case "/wp-json/wc/store/cart/remove-coupon":
		cart.Coupons = nil
	case "/wp-json/wc/store/cart/select-shipping-rate":
		var req storeapi.SelectShippingRateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.PackageID != 0 || (req.RateID != "flat_rate:1" && req.RateID != "local_pickup:2") {
			mockError(w, 400, "shipping", "Invalid rate")
			return
		}
		for i := range cart.ShippingRates[0].ShippingRates {
			cart.ShippingRates[0].ShippingRates[i].Selected = cart.ShippingRates[0].ShippingRates[i].RateID == req.RateID
		}
	case "/wp-json/wc/store/cart/extensions":
	default:
		mockError(w, 404, "route", "Route not found")
		return
	}
	mockRecalc(cart)
	mockJSON(w, cart)
}

func validMockGrind(id int, grind string) bool {
	for parent, vars := range variationsMap {
		for _, v := range vars {
			if v.ID == id {
				id = parent
				break
			}
		}
	}
	for _, p := range products {
		if p.ID != id {
			continue
		}
		if a := p.GetAttribute("Grind Size"); a != nil {
			for _, option := range a.Options {
				if option == grind {
					return true
				}
			}
		}
	}
	return false
}

func mockRecalc(cart *storeapi.Cart) {
	total, count := 0, 0
	for _, item := range cart.Items {
		price, _ := strconv.Atoi(item.Prices.Price)
		total += price * item.Quantity
		count += item.Quantity
	}
	shipping := 500
	if len(cart.ShippingRates) > 0 {
		for _, rate := range cart.ShippingRates[0].ShippingRates {
			if rate.Selected {
				shipping, _ = strconv.Atoi(rate.Price)
			}
		}
	}
	if total == 0 {
		shipping = 0
	}
	discount := 0
	if len(cart.Coupons) > 0 {
		discount = total / 10
	}
	cart.ItemsCount = count
	cart.NeedsShipping = total > 0
	cart.NeedsPayment = total+shipping-discount > 0
	cart.Totals = storeapi.CartTotals{CurrencyCode: "EUR", CurrencyMinorUnit: 2, TotalItems: strconv.Itoa(total), TotalDiscount: strconv.Itoa(discount), TotalShipping: strconv.Itoa(shipping), TotalTax: "0", TotalFees: "0", TotalPrice: strconv.Itoa(total + shipping - discount)}
	cart.PaymentMethods = []string{storeapi.GatewayID}
	if len(cart.ShippingRates) == 0 {
		cart.ShippingRates = []storeapi.ShippingPackage{{PackageID: 0, Name: "Coffee shipment", ShippingRates: []storeapi.ShippingRate{{RateID: "flat_rate:1", Name: "Flat rate", Price: "500", Selected: true}, {RateID: "local_pickup:2", Name: "Local pickup", Price: "0"}}}}
	}
	data, _ := json.Marshal(struct {
		Items             []storeapi.CartItem
		Totals            storeapi.CartTotals
		Billing, Shipping storeapi.CustomerAddress
		Rates             []storeapi.ShippingPackage
		Coupons           []storeapi.CartCoupon
	}{cart.Items, cart.Totals, cart.BillingAddress, cart.ShippingAddress, cart.ShippingRates, cart.Coupons})
	sum := sha256.Sum256(data)
	cart.Extensions = map[string]any{"eva_terminal": map[string]any{"quote": storeapi.Quote{Fingerprint: hex.EncodeToString(sum[:]), Total: cart.Totals.TotalPrice, Currency: "EUR"}}}
}

func (s *mockStore) bridge(w http.ResponseWriter, r *http.Request, path string) {
	if r.Header.Get("X-EVA-Bridge-Key") != getEnv("EVA_BRIDGE_KEY", "") || r.Header.Get("X-EVA-Bridge-Key") == "" {
		mockError(w, 403, "bridge_auth", "Bridge key required")
		return
	}
	if path == "/wp-json/eva-terminal/v1/checkout" && r.Method == http.MethodPost {
		var req storeapi.BridgeCheckoutRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			mockError(w, 400, "json", "Invalid JSON")
			return
		}
		data, _ := json.Marshal(req)
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		if a := s.attempts[req.AttemptID]; a != nil {
			if a.Hash != hash || a.Owner != req.CustomerRef {
				mockError(w, 409, "attempt_mismatch", "Attempt data changed")
				return
			}
			mockJSON(w, a.Attempt)
			return
		}
		for _, a := range s.attempts {
			if a.Owner == req.CustomerRef && a.Attempt.Active() {
				mockError(w, 409, "active_attempt", "An active payment exists")
				return
			}
		}
		cart := s.carts[r.Header.Get("Cart-Token")]
		if cart == nil {
			mockError(w, 401, "token", "Cart token required")
			return
		}
		mockRecalc(cart)
		ext := cart.Extensions["eva_terminal"].(map[string]any)
		if req.AcceptedQuote != ext["quote"].(storeapi.Quote) {
			mockError(w, 409, "quote_changed", "Woo quote changed")
			return
		}
		if len(cart.Items) == 0 || req.Checkout.BillingAddress == nil || req.Checkout.BillingAddress.Email == "" {
			mockError(w, 400, "validation", "Cart and billing email required")
			return
		}
		s.nextOrder++
		order := storeapi.StoreOrder{ID: s.nextOrder, OrderID: s.nextOrder, OrderKey: fmt.Sprintf("wc_order_%d", s.nextOrder), Status: "pending", CurrencyCode: "EUR", Totals: cart.Totals, Items: append([]storeapi.CartItem(nil), cart.Items...), BillingAddress: cart.BillingAddress, ShippingAddress: cart.ShippingAddress}
		a := &mockAttempt{Owner: req.CustomerRef, Hash: hash, Order: order, Created: time.Now(), Attempt: storeapi.Attempt{AttemptID: req.AttemptID, OrderID: order.ID, OrderKey: order.OrderKey, PaymentState: "awaiting_payment", PaymentURL: "https://checkout.stripe.com/mock/" + req.AttemptID, ExpiresAt: time.Now().Add(30 * time.Minute).Unix(), Total: cart.Totals.TotalPrice, Currency: "EUR"}}
		s.attempts[req.AttemptID] = a
		cart.Items = nil
		mockRecalc(cart)
		mockJSON(w, a.Attempt)
		return
	}
	id := strings.TrimPrefix(path, "/wp-json/eva-terminal/v1/attempts/")
	cancel := strings.HasSuffix(id, "/cancel")
	id = strings.TrimSuffix(id, "/cancel")
	owner := r.URL.Query().Get("customer_ref")
	if cancel {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		owner = body["customer_ref"]
	}
	a := s.attempts[id]
	if a == nil || a.Owner != owner {
		mockError(w, 404, "attempt_missing", "Attempt not found")
		return
	}
	paidSeconds, _ := strconv.Atoi(getEnv("MOCK_PAYMENT_SECONDS", "15"))
	if a.Attempt.Active() && paidSeconds > 0 && time.Since(a.Created) >= time.Duration(paidSeconds)*time.Second {
		a.Attempt.PaymentState = "paid"
		a.Order.Status = "processing"
	}
	if cancel && a.Attempt.Active() {
		a.Attempt.PaymentState = "cancelled"
		a.Order.Status = "cancelled"
	}
	mockJSON(w, a.Attempt)
}
