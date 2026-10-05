package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/thomas/eva-terminal-go/internal/cache"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

// setupTestModel creates a model with a mock server for testing.
func setupTestModel(t *testing.T, products []woo.Product, variations map[int][]woo.Variation) (Model, *httptest.Server) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cart-Token", "test-cart-token")
		w.Header().Set("Nonce", "test-nonce")

		if r.URL.Path == "/wp-json/wc/store/v1/products" {
			query := r.URL.Query()
			if query.Get("type") == "variation" {
				parentID, _ := strconv.Atoi(query.Get("parent"))
				storeProducts := make([]storeapi.Product, 0)
				for _, v := range variations[parentID] {
					storeProducts = append(storeProducts, storeapi.Product{
						ID:            v.ID,
						Type:          "variation",
						StockStatus:   v.StockStatus,
						IsInStock:     v.IsInStock(),
						IsPurchasable: v.Purchasable == nil || *v.Purchasable,
						Prices: storeapi.ProductPrices{
							Price:        minor(v.Price),
							RegularPrice: minor(v.RegularPrice),
							SalePrice:    minor(v.SalePrice),
						},
					})
				}
				json.NewEncoder(w).Encode(storeProducts)
				return
			}

			storeProducts := make([]storeapi.Product, 0, len(products))
			for _, p := range products {
				attrs := make([]storeapi.ProductAttribute, 0, len(p.Attributes))
				for _, a := range p.Attributes {
					terms := make([]storeapi.Term, 0, len(a.Options))
					for _, opt := range a.Options {
						terms = append(terms, storeapi.Term{Name: opt})
					}
					attrs = append(attrs, storeapi.ProductAttribute{
						ID:            a.ID,
						Name:          a.Name,
						HasVariations: a.Variation,
						Terms:         terms,
					})
				}
				metadata := make([]storeapi.ProductVariation, 0)
				for _, v := range variations[p.ID] {
					va := make([]storeapi.ProductVariationAttribute, 0)
					for _, attr := range v.Attributes {
						va = append(va, storeapi.ProductVariationAttribute{Name: attr.Name, Value: attr.Option})
					}
					metadata = append(metadata, storeapi.ProductVariation{ID: v.ID, Attributes: va})
				}

				storeProducts = append(storeProducts, storeapi.Product{
					ID:            p.ID,
					Name:          p.Name,
					Type:          p.Type,
					Description:   p.Description,
					ShortDesc:     p.ShortDescription,
					StockStatus:   p.StockStatus,
					IsInStock:     p.IsInStock(),
					IsPurchasable: p.Purchasable == nil || *p.Purchasable,
					Prices: storeapi.ProductPrices{
						CurrencyCode:      p.CurrencyCode,
						CurrencyMinorUnit: p.CurrencyMinorUnit,
						Price:             minor(p.Price),
						RegularPrice:      minor(p.RegularPrice),
						SalePrice:         minor(p.SalePrice),
					},
					Attributes: attrs,
					Variations: metadata,
				})
			}
			json.NewEncoder(w).Encode(storeProducts)
			return
		}

		// Default: return empty array
		json.NewEncoder(w).Encode([]storeapi.Product{})
	}))

	client := storeapi.NewClient(server.URL)
	productsCache := cache.New[ProductListCacheKey, []woo.Product](time.Minute)
	variationsCache := cache.New[int, []woo.Variation](time.Minute)

	model := NewModel(client, productsCache, variationsCache)
	return model, server
}

func minor(price string) string {
	if price == "" {
		return ""
	}
	f, err := strconv.ParseFloat(price, 64)
	if err != nil {
		return "0"
	}
	return strconv.Itoa(int(f * 100))
}

func TestNewModel(t *testing.T) {
	products := []woo.Product{
		{ID: 1, Name: "Test Coffee", Type: "simple", Price: "10.00", StockStatus: "instock"},
	}

	model, server := setupTestModel(t, products, nil)
	defer server.Close()

	// Check initial state
	if model.GetViewState() != ViewProductList {
		t.Errorf("expected initial view state to be ProductList, got %v", model.GetViewState())
	}

	if model.GetSelectedProduct() != nil {
		t.Error("expected no product to be selected initially")
	}
}

func TestViewStateTransitions(t *testing.T) {
	products := []woo.Product{{ID: 1, Name: "Simple Coffee", Type: "simple", Price: "10.00", StockStatus: "instock"}}
	m, server := setupTestModel(t, products, nil)
	defer server.Close()
	sendMessage(t, &m, m.loadProducts()())
	if m.viewState != ViewProductList || m.selectedProduct.ID != 1 {
		t.Fatal("shop preview missing")
	}
	press(t, &m, tea.KeyEnter)
	if m.viewState != ViewCart || m.localCart.ItemCount() != 1 {
		t.Fatal("Enter did not add displayed coffee")
	}
	press(t, &m, tea.KeyEscape)
	if m.viewState != ViewProductList || m.selectedProduct.ID != 1 {
		t.Fatal("Esc did not return to selected coffee")
	}
}

func TestVariableProductTriggersVariationsFetch(t *testing.T) {
	m := variableModel(t)
	if m.viewState != ViewProductList || len(m.productVariations) != 2 || m.selectedVariation.ID != 1011 {
		t.Fatal("preview failed to load/default available size")
	}
}

func TestFilterToggle(t *testing.T) {
	products := []woo.Product{
		{ID: 1, Name: "In Stock Coffee", Type: "simple", Price: "10.00", StockStatus: "instock"},
		{ID: 2, Name: "Out of Stock Coffee", Type: "simple", Price: "12.00", StockStatus: "outofstock"},
	}

	model, server := setupTestModel(t, products, nil)
	defer server.Close()

	m := model

	// Initially not filtering
	if m.inStockOnly {
		t.Error("expected inStockOnly to be false initially")
	}

	// Toggle filter with 'f' key
	newModel, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: string('f')})
	m = newModel.(Model)

	if !m.inStockOnly {
		t.Error("expected inStockOnly to be true after pressing 'f'")
	}

	// Toggle again
	newModel, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: string('f')})
	m = newModel.(Model)

	if m.inStockOnly {
		t.Error("expected inStockOnly to be false after pressing 'f' again")
	}
}

func TestSearchMode(t *testing.T) {
	products := []woo.Product{
		{ID: 1, Name: "Ethiopian Coffee", Type: "simple", Price: "18.00", StockStatus: "instock"},
	}

	model, server := setupTestModel(t, products, nil)
	defer server.Close()

	m := model

	// Initially not in search mode
	if m.showSearch {
		t.Error("expected showSearch to be false initially")
	}

	// Enter search mode with '/' key
	newModel, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	m = newModel.(Model)

	if !m.showSearch {
		t.Error("expected showSearch to be true after pressing '/'")
	}

	// Exit search mode with Esc
	newModel, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = newModel.(Model)

	if m.showSearch {
		t.Error("expected showSearch to be false after pressing Esc")
	}
}

func TestProductItemInterface(t *testing.T) {
	p := woo.Product{
		ID:          1,
		Name:        "Test Coffee",
		Type:        "variable",
		Price:       "15.99",
		StockStatus: "instock",
	}

	item := productItem{product: p}

	if item.Title() != "Test Coffee" {
		t.Errorf("expected title 'Test Coffee', got '%s'", item.Title())
	}

	desc := item.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}

	if item.FilterValue() != "Test Coffee" {
		t.Errorf("expected filter value 'Test Coffee', got '%s'", item.FilterValue())
	}
}

func TestViewRendering(t *testing.T) {
	products := []woo.Product{
		{ID: 1, Name: "Test Coffee", Type: "simple", Price: "10.00", StockStatus: "instock"},
	}

	model, server := setupTestModel(t, products, nil)
	defer server.Close()

	m := model
	m.width = 80
	m.height = 24
	m.products = products
	m.updateProductList()

	// Test product list view
	view := m.View()
	if view.Content == "" || !view.AltScreen {
		t.Error("expected non-empty view output")
	}

}

func TestInlineConfigurationSummary(t *testing.T) {
	m := variableModel(t)
	completeConfiguration(t, &m)
	view := m.View().Content
	for _, text := range []string{"1kg", "Espresso", "EUR 49.99", "Qty", "Add to cart"} {
		if !strings.Contains(view, text) {
			t.Fatalf("missing %q in shop: %s", text, view)
		}
	}
}
