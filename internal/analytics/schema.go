// Package analytics collects anonymous SSH connection activity, never shopper data.
package analytics

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const SchemaVersion = 1

type Config struct {
	Enabled                                   bool
	BaseURL, WebsiteID, Hostname, Environment string
}

var uuidPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var hostPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,99}$`)

func UUIDValid(id string) bool { return uuidPattern.MatchString(id) }
func EnvironmentValid(env string) bool {
	return env == "development" || env == "staging" || env == "production"
}

func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("UMAMI_BASE_URL must be HTTP(S) without credentials, query or fragment")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme == "http" && (!local || c.Environment == "production") {
		return fmt.Errorf("Umami requires HTTPS except for local development/staging mocks")
	}
	if !UUIDValid(c.WebsiteID) {
		return fmt.Errorf("UMAMI_WEBSITE_ID must be a lowercase UUID")
	}
	if !EnvironmentValid(c.Environment) {
		return fmt.Errorf("invalid UMAMI_ENVIRONMENT")
	}
	if !hostPattern.MatchString(c.Hostname) {
		return fmt.Errorf("invalid UMAMI_HOSTNAME")
	}
	return nil
}

// Context is frozen with the checkout request. CheckoutID is an analytics-only UUID.
type Context struct {
	ConnectionID  string `json:"connection_id"`
	SchemaVersion int    `json:"schema_version"`
	Environment   string `json:"environment"`
	Collect       bool   `json:"collect"`
	CheckoutID    string `json:"checkout_id,omitempty"`
}

func NewContext(environment string) (Context, error) {
	id, err := NewID()
	return Context{ConnectionID: id, SchemaVersion: SchemaVersion, Environment: environment, Collect: err == nil}, err
}

func (c Context) Valid() bool {
	return c.Collect && UUIDValid(c.ConnectionID) && c.SchemaVersion == SchemaVersion && EnvironmentValid(c.Environment) && (c.CheckoutID == "" || UUIDValid(c.CheckoutID))
}

type PageView struct {
	Context Context
	Path    string
}
type Event struct {
	Context    Context
	Path, Name string
	Data       map[string]any
}
type Tracker interface {
	PageView(PageView) bool
	Event(Event) bool
}
type Noop struct{}

func (Noop) PageView(PageView) bool { return false }
func (Noop) Event(Event) bool       { return false }

var pages = map[string]string{
	"/shop": "Shop", "/cart": "Cart", "/checkout/address": "Checkout address",
	"/checkout/shipping": "Checkout shipping", "/checkout/review": "Checkout review", "/checkout/payment": "Checkout payment",
}

// Each key is explicitly allowed; values are scalar and validated before enqueue.
var fields = map[string]string{
	"session_started": "resumed_payment", "view_item": "product_id", "search": "query_length results_count",
	"filter_changed": "in_stock_only", "add_to_cart": "product_id variation_id catalog_item_id quantity grind confirmation",
	"remove_from_cart":      "product_id variation_id catalog_item_id quantity confirmation",
	"cart_quantity_changed": "product_id variation_id catalog_item_id quantity_before quantity_after confirmation",
	"begin_checkout":        "item_count", "coupon_applied": "", "coupon_removed": "", "shipping_selected": "shipping_method",
	"checkout_submitted": "checkout_id", "payment_link_available": "checkout_id", "payment_link_copy_requested": "",
	"checkout_error": "stage error_code",
}

var requiredFields = map[string]string{
	"session_started": "resumed_payment", "view_item": "product_id", "search": "query_length results_count",
	"filter_changed": "in_stock_only", "add_to_cart": "quantity confirmation", "remove_from_cart": "quantity confirmation",
	"cart_quantity_changed": "quantity_before quantity_after confirmation", "begin_checkout": "item_count",
	"shipping_selected": "shipping_method", "checkout_submitted": "checkout_id", "payment_link_available": "checkout_id",
	"checkout_error": "stage error_code",
}

func validField(key string, value any) bool {
	switch key {
	case "resumed_payment", "in_stock_only":
		_, ok := value.(bool)
		return ok
	case "product_id", "variation_id", "catalog_item_id", "quantity", "quantity_before", "quantity_after", "item_count", "query_length", "results_count":
		n, ok := value.(int)
		return ok && n >= 0 && n <= 2147483647 && (n > 0 || key == "results_count")
	case "confirmation":
		return value == "local_intent"
	case "checkout_id":
		s, ok := value.(string)
		return ok && UUIDValid(s)
	case "grind":
		s, ok := value.(string)
		return ok && Grind(s) == s && s != ""
	case "shipping_method":
		return value == "flat_rate" || value == "free_shipping" || value == "local_pickup" || value == "other"
	case "stage":
		return value == "cart_sync" || value == "cart" || value == "address" || value == "coupon" || value == "shipping" || value == "checkout" || value == "payment"
	case "error_code":
		return value == "validation" || value == "conflict" || value == "unavailable" || value == "network" || value == "unknown"
	}
	return false
}

func Grind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "whole beans", "whole bean":
		return "whole_beans"
	case "whole_beans", "espresso", "fine", "medium", "coarse", "filter", "moka", "french_press":
		return strings.ToLower(strings.TrimSpace(value))
	case "french press":
		return "french_press"
	}
	return ""
}

func ShippingMethod(value string) string {
	s := strings.SplitN(value, ":", 2)[0]
	switch s {
	case "flat_rate", "free_shipping", "local_pickup":
		return s
	}
	return "other"
}
