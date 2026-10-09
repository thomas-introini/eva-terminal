package storeapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/thomas/eva-terminal-go/internal/analytics"
)

const GatewayID = "eva_terminal_stripe_checkout"

// Quote is opaque to Go except for the amount shown to the shopper.
type Quote struct {
	Fingerprint string `json:"fingerprint"`
	Total       string `json:"total"`
	Currency    string `json:"currency"`
}

type BridgeCheckoutRequest struct {
	AttemptID     string             `json:"attempt_id"`
	CustomerRef   string             `json:"customer_ref"`
	AcceptedQuote Quote              `json:"accepted_quote"`
	Checkout      CheckoutRequest    `json:"checkout"`
	Analytics     *analytics.Context `json:"analytics,omitempty"`
}

type Attempt struct {
	AttemptID    string `json:"attempt_id"`
	OrderID      int    `json:"order_id"`
	OrderKey     string `json:"order_key"`
	PaymentState string `json:"payment_state"`
	PaymentURL   string `json:"payment_url"`
	ExpiresAt    int64  `json:"expires_at"`
	Total        string `json:"total"`
	Currency     string `json:"currency"`
}

func (a Attempt) Active() bool {
	switch a.PaymentState {
	case "paid", "cancelled", "expired", "failed":
		return false
	}
	return a.AttemptID != ""
}

func (c *Client) PrepareCart(ctx context.Context) (*Cart, error) {
	var cart Cart
	err := c.doJSON(ctx, http.MethodPost, "/cart/extensions", nil,
		map[string]any{"namespace": "eva_terminal", "data": map[string]any{"prepare": true}}, &cart)
	return &cart, err
}

func (c *Client) BridgeCheckout(ctx context.Context, req BridgeCheckoutRequest) (*Attempt, error) {
	var a Attempt
	err := c.requestJSON(ctx, http.MethodPost, "/wp-json/eva-terminal/v1/checkout", nil, req, &a)
	return &a, err
}

func (c *Client) GetAttempt(ctx context.Context, id, owner string) (*Attempt, error) {
	var a Attempt
	err := c.requestJSON(ctx, http.MethodGet, "/wp-json/eva-terminal/v1/attempts/"+url.PathEscape(id), url.Values{"customer_ref": {owner}}, nil, &a)
	return &a, err
}

func (c *Client) CancelAttempt(ctx context.Context, id, owner string) (*Attempt, error) {
	var a Attempt
	err := c.requestJSON(ctx, http.MethodPost, "/wp-json/eva-terminal/v1/attempts/"+url.PathEscape(id)+"/cancel", nil, map[string]string{"customer_ref": owner}, &a)
	return &a, err
}

// FormatMinor formats integer money without float rounding, including JPY/KWD.
func FormatMinor(raw string, unit int) string {
	if raw == "" {
		return ""
	}
	if unit < 0 || unit > 6 {
		return raw
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return raw
	}
	text := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}
	if unit == 0 {
		return sign + text
	}
	if len(text) <= unit {
		text = strings.Repeat("0", unit+1-len(text)) + text
	}
	return fmt.Sprintf("%s%s.%s", sign, text[:len(text)-unit], text[len(text)-unit:])
}
