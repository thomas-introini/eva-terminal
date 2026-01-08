// Package main implements a mock WooCommerce REST API server for local development.
package main

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

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

	http.HandleFunc("/wp-json/wc/v3/products", handleProducts)
	http.HandleFunc("/wp-json/wc/v3/products/", handleProductsWithID)

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



