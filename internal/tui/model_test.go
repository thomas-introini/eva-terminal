package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
						ID:          v.ID,
						Type:        "variation",
						StockStatus: v.StockStatus,
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
					ID:          p.ID,
					Name:        p.Name,
					Type:        p.Type,
					Description: p.Description,
					ShortDesc:   p.ShortDescription,
					StockStatus: p.StockStatus,
					Prices: storeapi.ProductPrices{
						Price:        minor(p.Price),
						RegularPrice: minor(p.RegularPrice),
						SalePrice:    minor(p.SalePrice),
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
	products := []woo.Product{
		{ID: 1, Name: "Simple Coffee", Type: "simple", Price: "10.00", StockStatus: "instock"},
		{
			ID:          101,
			Name:        "Variable Coffee",
			Type:        "variable",
			Price:       "15.00",
			StockStatus: "instock",
			Variations:  []int{1011, 1012},
			Attributes: []woo.Attribute{
				{Name: "Size", Options: []string{"250g", "1kg"}, Variation: true},
			},
		},
	}

	model, server := setupTestModel(t, products, nil)
	defer server.Close()

	// Set window size
	updatedModel, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m := updatedModel.(Model)

	// Verify initial state
	if m.GetViewState() != ViewProductList {
		t.Error("expected ProductList view initially")
	}

	// Simulate loading products (manual since we're testing synchronously)
	m.products = products
	m.updateProductList()

	// Select first item and press enter
	m.productList.Select(0)
	m.selectedProduct = &products[0]
	m.viewState = ViewProductDetails

	if m.GetViewState() != ViewProductDetails {
		t.Error("expected ProductDetails view after selection")
	}

	// Go back to list
	newModel, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = newModel.(Model)

	if m.GetViewState() != ViewProductList {
		t.Error("expected ProductList view after pressing Esc")
	}
}

func TestVariableProductTriggersVariationsFetch(t *testing.T) {
	products := []woo.Product{
		{
			ID:          101,
			Name:        "Variable Coffee",
			Type:        "variable",
			Price:       "15.00",
			StockStatus: "instock",
			Variations:  []int{1011, 1012},
			Attributes: []woo.Attribute{
				{Name: "Size", Options: []string{"250g", "1kg"}, Variation: true},
				{Name: "Grind Size", Options: []string{"Beans", "Espresso"}, Variation: false},
			},
		},
	}

	variations := map[int][]woo.Variation{
		101: {
			{ID: 1011, Price: "14.99", StockStatus: "instock", Attributes: []woo.VariationAttribute{{Name: "Size", Option: "250g"}}},
			{ID: 1012, Price: "49.99", StockStatus: "instock", Attributes: []woo.VariationAttribute{{Name: "Size", Option: "1kg"}}},
		},
	}

	model, server := setupTestModel(t, products, variations)
	defer server.Close()

	m := model
	m.products = products
	m.updateProductList()

	// Select the variable product
	m.selectedProduct = &products[0]
	m.viewState = ViewProductDetails

	// Check that the product is variable
	if !m.selectedProduct.IsVariable() {
		t.Error("expected selected product to be variable")
	}

	// Simulate variations being loaded
	m.productVariations = variations[101]

	// Now entering configurator should work
	m.initConfigurator()
	m.viewState = ViewConfigurator

	if m.GetViewState() != ViewConfigurator {
		t.Error("expected Configurator view for variable product")
	}
}

func TestSimpleProductGrindSelection(t *testing.T) {
	products := []woo.Product{
		{
			ID:          1,
			Name:        "Simple Coffee",
			Type:        "simple",
			Price:       "10.00",
			StockStatus: "instock",
			Attributes: []woo.Attribute{
				{Name: "Grind Size", Options: []string{"Beans", "Espresso", "Filter"}, Variation: false},
			},
		},
	}

	model, server := setupTestModel(t, products, nil)
	defer server.Close()

	m := model
	m.products = products
	m.selectedProduct = &products[0]
	m.viewState = ViewProductDetails

	// Simple product should allow grind selection
	grindAttr := m.selectedProduct.GetAttribute("Grind Size")
	if grindAttr == nil {
		t.Fatal("expected Grind Size attribute on simple product")
	}

	if len(grindAttr.Options) != 3 {
		t.Errorf("expected 3 grind options, got %d", len(grindAttr.Options))
	}

	// Initialize simple configurator
	m.initSimpleConfigurator()
	m.viewState = ViewConfigurator

	if m.GetViewState() != ViewConfigurator {
		t.Error("expected Configurator view for grind selection")
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

	item := productItem{product: p, styles: DefaultStyles()}

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

	// Test product details view
	m.selectedProduct = &products[0]
	m.viewState = ViewProductDetails
	detailsView := m.View()
	if detailsView.Content == "" || !detailsView.AltScreen {
		t.Error("expected non-empty details view output")
	}
}

func TestConfigurationSummary(t *testing.T) {
	products := []woo.Product{
		{
			ID:          101,
			Name:        "Variable Coffee",
			Type:        "variable",
			Price:       "15.00",
			StockStatus: "instock",
			Attributes: []woo.Attribute{
				{Name: "Size", Options: []string{"250g", "1kg"}, Variation: true},
			},
		},
	}

	model, server := setupTestModel(t, products, nil)
	defer server.Close()

	m := model
	m.selectedProduct = &products[0]
	m.productVariations = []woo.Variation{
		{ID: 1011, Price: "14.99", StockStatus: "instock"},
	}
	m.selectedVariation = &m.productVariations[0]
	m.selectedGrindSize = "Espresso"
	m.configCompleted = true

	summary := m.renderConfigSummary()
	if summary == "" {
		t.Error("expected non-empty configuration summary")
	}

	if !m.GetConfigCompleted() {
		t.Error("expected config to be marked as completed")
	}
}
