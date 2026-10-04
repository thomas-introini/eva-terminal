package storeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultStorePrefix = "/wp-json/wc/store/v1"
)

// Client is a WooCommerce Store API client.
type Client struct {
	baseURL     string
	storePrefix string
	httpClient  *http.Client

	mu        sync.RWMutex
	cartToken string
	nonce     string
	bridgeKey string
}

// Option configures the Store API client.
type Option func(*Client)

func WithBridgeKey(key string) Option {
	return func(c *Client) { c.bridgeKey = key }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithStorePrefix overrides the Store API prefix.
func WithStorePrefix(prefix string) Option {
	return func(c *Client) {
		if strings.TrimSpace(prefix) != "" {
			c.storePrefix = normalizePrefix(prefix)
		}
	}
}

// WithSessionTokens seeds cart token and nonce.
func WithSessionTokens(cartToken, nonce string) Option {
	return func(c *Client) {
		c.cartToken = cartToken
		c.nonce = nonce
	}
}

// NewClient creates a Store API client.
func NewClient(baseURL string, opts ...Option) *Client {
	jar, _ := cookiejar.New(nil)
	c := &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		storePrefix: defaultStorePrefix,
		httpClient: &http.Client{
			Timeout:       30 * time.Second,
			Jar:           jar,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.storePrefix = normalizePrefix(c.storePrefix)
	return c
}

// SessionState returns latest session headers.
func (c *Client) SessionState() (cartToken, nonce string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cartToken, c.nonce
}

// StoreError is a structured API error.
type StoreError struct {
	StatusCode int
	Code       string `json:"code"`
	Message    string `json:"message"`
	Data       struct {
		Status int `json:"status"`
	} `json:"data"`
	RawBody string `json:"-"`
}

func (e *StoreError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("store api error (status %d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("store api error (status %d)", e.StatusCode)
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body any, result any) error {
	return c.requestJSON(ctx, method, c.storePrefix+path, query, body, result)
}

func (c *Client) requestJSON(ctx context.Context, method, path string, query url.Values, body any, result any) error {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	var requestBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		requestBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, requestBody)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.addSessionHeaders(req)
	if c.bridgeKey != "" {
		req.Header.Set("X-EVA-Bridge-Key", c.bridgeKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	c.captureSessionHeaders(resp.Header)

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return parseStoreError(resp.StatusCode, payload)
	}

	if result == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// ResetSession obtains a new guest cart after an expired Woo token.
func (c *Client) ResetSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cartToken, c.nonce = "", ""
	jar, _ := cookiejar.New(nil)
	c.httpClient.Jar = jar
}

func (c *Client) addSessionHeaders(req *http.Request) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cartToken != "" {
		req.Header.Set("Cart-Token", c.cartToken)
	}
	if c.nonce != "" {
		req.Header.Set("Nonce", c.nonce)
	}
}

func (c *Client) captureSessionHeaders(headers http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if token := headers.Get("Cart-Token"); token != "" {
		c.cartToken = token
	}
	if nonce := headers.Get("Nonce"); nonce != "" {
		c.nonce = nonce
	}
}

func parseStoreError(statusCode int, payload []byte) error {
	var e StoreError
	e.StatusCode = statusCode
	e.RawBody = string(payload)
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &e)
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(payload))
	}
	return &e
}

func normalizePrefix(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return defaultStorePrefix
	}
	trimmed = strings.Trim(trimmed, "/")
	return "/" + trimmed
}
