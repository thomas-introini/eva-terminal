package woo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetProducts(t *testing.T) {
	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify path
		if !strings.HasPrefix(r.URL.Path, "/wp-json/wc/v3/products") {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Verify query params
		query := r.URL.Query()
		if query.Get("page") != "1" {
			t.Errorf("expected page=1, got %s", query.Get("page"))
		}
		if query.Get("per_page") != "10" {
			t.Errorf("expected per_page=10, got %s", query.Get("per_page"))
		}

		// Return mock products
		products := []Product{
			{
				ID:          1,
				Name:        "Test Coffee",
				Type:        "simple",
				Price:       "12.99",
				StockStatus: "instock",
			},
			{
				ID:          2,
				Name:        "Test Variable Coffee",
				Type:        "variable",
				Price:       "15.99",
				StockStatus: "instock",
				Variations:  []int{21, 22},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(products)
	}))
	defer server.Close()

	// Create client
	client := NewClient(server.URL)

	// Test GetProducts
	products, err := client.GetProducts(context.Background(), GetProductsParams{
		Page:    1,
		PerPage: 10,
	})
	if err != nil {
		t.Fatalf("GetProducts failed: %v", err)
	}

	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(products))
	}

	if products[0].Name != "Test Coffee" {
		t.Errorf("expected name 'Test Coffee', got '%s'", products[0].Name)
	}

	if products[1].Type != "variable" {
		t.Errorf("expected type 'variable', got '%s'", products[1].Type)
	}
}

func TestGetProductsWithSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		search := query.Get("search")
		if search != "ethiopian" {
			t.Errorf("expected search=ethiopian, got %s", search)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Product{})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.GetProducts(context.Background(), GetProductsParams{
		Page:    1,
		PerPage: 10,
		Search:  "ethiopian",
	})
	if err != nil {
		t.Fatalf("GetProducts with search failed: %v", err)
	}
}

func TestGetProductsInStockOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		stockStatus := query.Get("stock_status")
		if stockStatus != "instock" {
			t.Errorf("expected stock_status=instock, got %s", stockStatus)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Product{})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.GetProducts(context.Background(), GetProductsParams{
		Page:        1,
		PerPage:     10,
		InStockOnly: true,
	})
	if err != nil {
		t.Fatalf("GetProducts in stock only failed: %v", err)
	}
}

func TestGetVariations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify path
		expectedPath := "/wp-json/wc/v3/products/101/variations"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		variations := []Variation{
			{
				ID:          1011,
				Price:       "14.99",
				StockStatus: "instock",
				Attributes: []VariationAttribute{
					{Name: "Size", Option: "250g"},
				},
			},
			{
				ID:          1012,
				Price:       "49.99",
				StockStatus: "instock",
				Attributes: []VariationAttribute{
					{Name: "Size", Option: "1kg"},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(variations)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	variations, err := client.GetVariations(context.Background(), 101)
	if err != nil {
		t.Fatalf("GetVariations failed: %v", err)
	}

	if len(variations) != 2 {
		t.Fatalf("expected 2 variations, got %d", len(variations))
	}

	if variations[0].GetAttributeValue("Size") != "250g" {
		t.Errorf("expected size '250g', got '%s'", variations[0].GetAttributeValue("Size"))
	}

	if variations[1].Price != "49.99" {
		t.Errorf("expected price '49.99', got '%s'", variations[1].Price)
	}
}

func TestGetProductsErrorNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.GetProducts(context.Background(), GetProductsParams{Page: 1, PerPage: 10})
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain status code, got: %v", err)
	}
}

func TestGetProductsErrorInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.GetProducts(context.Background(), GetProductsParams{Page: 1, PerPage: 10})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}

	if !strings.Contains(err.Error(), "decoding") {
		t.Errorf("expected decoding error, got: %v", err)
	}
}

func TestGetProductsWithCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("consumer_key") != "ck_test" {
			t.Errorf("expected consumer_key=ck_test, got %s", query.Get("consumer_key"))
		}
		if query.Get("consumer_secret") != "cs_test" {
			t.Errorf("expected consumer_secret=cs_test, got %s", query.Get("consumer_secret"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Product{})
	}))
	defer server.Close()

	client := NewClient(server.URL, WithCredentials("ck_test", "cs_test"))
	_, err := client.GetProducts(context.Background(), GetProductsParams{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("GetProducts with credentials failed: %v", err)
	}
}

func TestProductMethods(t *testing.T) {
	p := Product{
		ID:           1,
		Name:         "Test",
		Type:         "variable",
		Price:        "10.00",
		RegularPrice: "12.00",
		SalePrice:    "10.00",
		StockStatus:  "instock",
		Attributes: []Attribute{
			{Name: "Grind Size", Options: []string{"Beans", "Espresso"}},
		},
	}

	if !p.IsInStock() {
		t.Error("expected IsInStock to return true")
	}

	if !p.IsVariable() {
		t.Error("expected IsVariable to return true")
	}

	if p.GetDisplayPrice() != "10.00" {
		t.Errorf("expected display price '10.00', got '%s'", p.GetDisplayPrice())
	}

	attr := p.GetAttribute("Grind Size")
	if attr == nil {
		t.Fatal("expected to find Grind Size attribute")
	}
	if len(attr.Options) != 2 {
		t.Errorf("expected 2 options, got %d", len(attr.Options))
	}

	noAttr := p.GetAttribute("Nonexistent")
	if noAttr != nil {
		t.Error("expected nil for nonexistent attribute")
	}
}

func TestVariationMethods(t *testing.T) {
	v := Variation{
		ID:           1,
		Price:        "15.00",
		RegularPrice: "15.00",
		SalePrice:    "",
		StockStatus:  "outofstock",
		Attributes: []VariationAttribute{
			{Name: "Size", Option: "1kg"},
		},
	}

	if v.IsInStock() {
		t.Error("expected IsInStock to return false")
	}

	if v.GetDisplayPrice() != "15.00" {
		t.Errorf("expected display price '15.00', got '%s'", v.GetDisplayPrice())
	}

	if v.GetAttributeValue("Size") != "1kg" {
		t.Errorf("expected size '1kg', got '%s'", v.GetAttributeValue("Size"))
	}

	if v.GetAttributeValue("Nonexistent") != "" {
		t.Error("expected empty string for nonexistent attribute")
	}
}



