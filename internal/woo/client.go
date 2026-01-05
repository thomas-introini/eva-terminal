package woo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client is a WooCommerce REST API client.
type Client struct {
	baseURL       string
	consumerKey   string
	consumerSecret string
	httpClient    *http.Client
}

// ClientOption is a functional option for configuring the client.
type ClientOption func(*Client)

// WithCredentials sets the WooCommerce API credentials.
func WithCredentials(key, secret string) ClientOption {
	return func(c *Client) {
		c.consumerKey = key
		c.consumerSecret = secret
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// NewClient creates a new WooCommerce API client.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetProductsParams holds parameters for listing products.
type GetProductsParams struct {
	Page        int
	PerPage     int
	Search      string
	InStockOnly bool
}

// GetProducts fetches a list of products from the WooCommerce API.
func (c *Client) GetProducts(ctx context.Context, params GetProductsParams) ([]Product, error) {
	endpoint := "/wp-json/wc/v3/products"

	query := url.Values{}
	if params.Page > 0 {
		query.Set("page", strconv.Itoa(params.Page))
	}
	if params.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(params.PerPage))
	}
	if params.Search != "" {
		query.Set("search", params.Search)
	}
	if params.InStockOnly {
		query.Set("stock_status", "instock")
	}

	var products []Product
	if err := c.doRequest(ctx, endpoint, query, &products); err != nil {
		return nil, err
	}
	return products, nil
}

// GetVariations fetches variations for a variable product.
func (c *Client) GetVariations(ctx context.Context, productID int) ([]Variation, error) {
	endpoint := fmt.Sprintf("/wp-json/wc/v3/products/%d/variations", productID)

	query := url.Values{}
	query.Set("per_page", "100") // Get all variations

	var variations []Variation
	if err := c.doRequest(ctx, endpoint, query, &variations); err != nil {
		return nil, err
	}
	return variations, nil
}

// doRequest performs an HTTP GET request to the WooCommerce API.
func (c *Client) doRequest(ctx context.Context, endpoint string, query url.Values, result interface{}) error {
	// Add authentication if credentials are provided
	if c.consumerKey != "" && c.consumerSecret != "" {
		query.Set("consumer_key", c.consumerKey)
		query.Set("consumer_secret", c.consumerSecret)
	}

	reqURL := c.baseURL + endpoint
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}



