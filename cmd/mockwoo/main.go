// Package main implements a mock WooCommerce REST API server for local development.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/thomas/eva-terminal-go/internal/storeapi"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

//go:embed testdata/*
var testdataFS embed.FS

var products []woo.Product
var variationsMap map[int][]woo.Variation

func init() {
	// Load products
	data, err := testdataFS.ReadFile("testdata/products.json")
	if err != nil {
		log.Fatalf("Failed to load products.json: %v", err)
	}
	if err := json.Unmarshal(data, &products); err != nil {
		log.Fatalf("Failed to parse products.json: %v", err)
	}

	// Load variations
	variationsMap = make(map[int][]woo.Variation)

	// Load variations for product 101
	data101, err := testdataFS.ReadFile("testdata/variations/101.json")
	if err == nil {
		var vars []woo.Variation
		if json.Unmarshal(data101, &vars) == nil {
			variationsMap[101] = vars
		}
	}

	// Load variations for product 102
	data102, err := testdataFS.ReadFile("testdata/variations/102.json")
	if err == nil {
		var vars []woo.Variation
		if json.Unmarshal(data102, &vars) == nil {
			variationsMap[102] = vars
		}
	}
}

func main() {
	addr := getEnv("MOCKWOO_ADDR", ":18080")

	http.Handle("/", newMockStore())

	log.Printf("Mock WooCommerce server listening on %s", addr)
	log.Printf("Loaded %d products", len(products))
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func handleProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()

	// Parse pagination
	page, _ := strconv.Atoi(query.Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(query.Get("per_page"))
	if perPage < 1 {
		perPage = 10
	}

	// Filter products
	filtered := filterProducts(products, query.Get("search"), query.Get("stock_status"))

	// Paginate
	start := (page - 1) * perPage
	end := start + perPage
	if start >= len(filtered) {
		filtered = []woo.Product{}
	} else {
		if end > len(filtered) {
			end = len(filtered)
		}
		filtered = filtered[start:end]
	}

	// Set headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-WP-Total", strconv.Itoa(len(products)))
	w.Header().Set("X-WP-TotalPages", strconv.Itoa((len(products)+perPage-1)/perPage))

	json.NewEncoder(w).Encode(filtered)
}

func handleStoreProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	search := strings.ToLower(query.Get("search"))
	stockStatus := query.Get("stock_status")

	type storeProduct struct {
		ID               int                         `json:"id"`
		Name             string                      `json:"name"`
		Slug             string                      `json:"slug"`
		Type             string                      `json:"type"`
		Description      string                      `json:"description"`
		ShortDescription string                      `json:"short_description"`
		StockStatus      string                      `json:"stock_status"`
		IsInStock        bool                        `json:"is_in_stock"`
		IsPurchasable    bool                        `json:"is_purchasable"`
		OnSale           bool                        `json:"on_sale"`
		HasOptions       bool                        `json:"has_options"`
		Prices           map[string]any              `json:"prices"`
		Attributes       []map[string]any            `json:"attributes"`
		Categories       []map[string]any            `json:"categories"`
		Images           []map[string]any            `json:"images"`
		Variations       []storeapi.ProductVariation `json:"variations"`
	}

	if query.Get("type") == "variation" {
		parentID, _ := strconv.Atoi(query.Get("parent"))
		out := make([]storeapi.Product, 0)
		for _, v := range variationsMap[parentID] {
			salePrice := ""
			if v.SalePrice != "" {
				salePrice = toMinorString(v.SalePrice)
			}
			out = append(out, storeapi.Product{ID: v.ID, Type: "variation", StockStatus: v.StockStatus, IsInStock: v.IsInStock(), IsPurchasable: true,
				Prices: storeapi.ProductPrices{CurrencyCode: "EUR", CurrencyMinorUnit: 2, Price: toMinorString(v.Price), RegularPrice: toMinorString(v.RegularPrice), SalePrice: salePrice}})
		}
		_ = json.NewEncoder(w).Encode(paginate(out, r))
		return
	}

	out := make([]storeProduct, 0)
	for _, p := range products {
		if search != "" && !strings.Contains(strings.ToLower(p.Name), search) {
			continue
		}
		if stockStatus != "" && p.StockStatus != stockStatus {
			continue
		}

		priceMinor := toMinorString(p.GetDisplayPrice())
		salePrice := ""
		if p.SalePrice != "" {
			salePrice = toMinorString(p.SalePrice)
		}
		attrs := make([]map[string]any, 0, len(p.Attributes))
		for _, a := range p.Attributes {
			terms := make([]map[string]any, 0, len(a.Options))
			for _, opt := range a.Options {
				terms = append(terms, map[string]any{"id": 0, "name": opt, "slug": strings.ToLower(strings.ReplaceAll(opt, " ", "-"))})
			}
			attrs = append(attrs, map[string]any{
				"id":             a.ID,
				"name":           a.Name,
				"taxonomy":       strings.ToLower(strings.ReplaceAll(a.Name, " ", "_")),
				"has_variations": a.Variation,
				"terms":          terms,
			})
		}

		metadata := make([]storeapi.ProductVariation, 0)
		for _, v := range variationsMap[p.ID] {
			attrs := make([]storeapi.ProductVariationAttribute, 0, len(v.Attributes))
			for _, a := range v.Attributes {
				attrs = append(attrs, storeapi.ProductVariationAttribute{Name: a.Name, Value: a.Option})
			}
			metadata = append(metadata, storeapi.ProductVariation{ID: v.ID, Attributes: attrs})
		}

		out = append(out, storeProduct{
			ID:               p.ID,
			Name:             p.Name,
			Slug:             fmt.Sprintf("product-%d", p.ID),
			Type:             p.Type,
			Description:      p.Description,
			ShortDescription: p.ShortDescription,
			StockStatus:      p.StockStatus,
			IsInStock:        p.IsInStock(),
			IsPurchasable:    true, OnSale: p.SalePrice != "" && p.Price != p.RegularPrice,
			HasOptions: p.IsVariable(),
			Prices: map[string]any{
				"currency_minor_unit": 2,
				"currency_code":       "EUR",
				"price":               priceMinor,
				"regular_price":       toMinorString(p.RegularPrice),
				"sale_price":          salePrice,
			},
			Attributes: attrs,
			Variations: metadata,
			Categories: []map[string]any{},
			Images:     []map[string]any{},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(paginate(out, r))
}

func handleProductsWithID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse path: /wp-json/wc/v3/products/{id} or /wp-json/wc/v3/products/{id}/variations
	path := strings.TrimPrefix(r.URL.Path, "/wp-json/wc/v3/products/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Product ID required", http.StatusBadRequest)
		return
	}

	productID, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "Invalid product ID", http.StatusBadRequest)
		return
	}

	// Check if requesting variations
	if len(parts) >= 2 && parts[1] == "variations" {
		handleVariations(w, productID)
		return
	}

	// Return single product
	for _, p := range products {
		if p.ID == productID {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(p)
			return
		}
	}

	http.Error(w, "Product not found", http.StatusNotFound)
}

func handleVariations(w http.ResponseWriter, productID int) {
	variations, ok := variationsMap[productID]
	if !ok {
		// Return empty array for products without variations
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]woo.Variation{})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(variations)
}

func filterProducts(products []woo.Product, search, stockStatus string) []woo.Product {
	if search == "" && stockStatus == "" {
		return products
	}

	search = strings.ToLower(search)
	var filtered []woo.Product

	for _, p := range products {
		// Filter by search term
		if search != "" {
			if !strings.Contains(strings.ToLower(p.Name), search) &&
				!strings.Contains(strings.ToLower(p.Description), search) {
				continue
			}
		}

		// Filter by stock status
		if stockStatus != "" && p.StockStatus != stockStatus {
			continue
		}

		filtered = append(filtered, p)
	}

	return filtered
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func toMinorString(price string) string {
	if price == "" {
		return "0"
	}
	parts := strings.SplitN(price, ".", 2)
	major, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return "0"
	}
	fraction := ""
	if len(parts) > 1 {
		fraction = parts[1]
	}
	fraction += "00"
	minor, err := strconv.ParseInt(fraction[:2], 10, 64)
	if err != nil {
		return "0"
	}
	if strings.HasPrefix(price, "-") {
		minor = -minor
	}
	return strconv.FormatInt(major*100+minor, 10)
}
