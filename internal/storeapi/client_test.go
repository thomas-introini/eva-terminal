package storeapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetProductsBuildsQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wp-json/wc/store/v1/products" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("search") != "ethiopian" {
			t.Fatalf("expected search query")
		}
		if q.Get("stock_status") != "instock" {
			t.Fatalf("expected stock_status=instock")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]Product{
			{ID: 1, Name: "Ethiopian", Prices: ProductPrices{Price: "1299"}, StockStatus: "instock"},
		})
	}))
	defer server.Close()

	c := NewClient(server.URL)
	products, err := c.GetProducts(context.Background(), ProductQuery{
		Page:    1,
		PerPage: 10,
		Search:  "ethiopian",
		InStock: true,
	})
	if err != nil {
		t.Fatalf("GetProducts failed: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
}

func TestCartAndCheckoutCaptureSessionHeaders(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/wp-json/wc/store/v1/cart/add-item", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cart-Token") != "" || r.Header.Get("Nonce") != "" {
			t.Fatalf("unexpected session headers on first request")
		}
		w.Header().Set("Cart-Token", "carttok-123")
		w.Header().Set("Nonce", "nonce-abc")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Cart{Items: []CartItem{{Key: "line-1", ID: 11, Quantity: 1}}})
	})

	mux.HandleFunc("/wp-json/wc/store/v1/checkout", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cart-Token") != "carttok-123" {
			t.Fatalf("missing cart token header on checkout")
		}
		if r.Header.Get("Nonce") != "nonce-abc" {
			t.Fatalf("missing nonce header on checkout")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CheckoutResponse{
			OrderID:  321,
			OrderKey: "wc_order_321",
			Status:   "pending",
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := NewClient(server.URL)
	if _, err := c.AddItem(context.Background(), AddItemRequest{ID: 11, Quantity: 1}); err != nil {
		t.Fatalf("AddItem failed: %v", err)
	}
	if _, err := c.Checkout(context.Background(), CheckoutRequest{PaymentMethod: "bacs"}); err != nil {
		t.Fatalf("Checkout failed: %v", err)
	}

	cartToken, nonce := c.SessionState()
	if cartToken != "carttok-123" || nonce != "nonce-abc" {
		t.Fatalf("unexpected session state: token=%q nonce=%q", cartToken, nonce)
	}
}

func TestGetOrderBuildsRequiredQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wp-json/wc/store/v1/order/77" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "wc_order_abc" {
			t.Fatalf("expected key in query")
		}
		if r.URL.Query().Get("billing_email") != "guest@example.com" {
			t.Fatalf("expected billing_email in query")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(StoreOrder{
			ID:       77,
			OrderKey: "wc_order_abc",
			Status:   "processing",
		})
	}))
	defer server.Close()

	c := NewClient(server.URL)
	order, err := c.GetOrder(context.Background(), 77, GetOrderParams{
		Key:          "wc_order_abc",
		BillingEmail: "guest@example.com",
	})
	if err != nil {
		t.Fatalf("GetOrder failed: %v", err)
	}
	if order.ID != 77 {
		t.Fatalf("expected order 77, got %d", order.ID)
	}
}

func TestAPIErrorParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"woocommerce_rest_invalid_order","message":"invalid order key","data":{"status":400}}`))
	}))
	defer server.Close()

	c := NewClient(server.URL)
	_, err := c.GetOrder(context.Background(), 1, GetOrderParams{Key: "bad"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid order key") {
		t.Fatalf("unexpected error: %v", err)
	}
}
