// Package config handles environment variable parsing and validation.
package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"time"
)

// AuthMode represents the SSH authentication mode.
type AuthMode string

const (
	AuthModeAllowlist AuthMode = "allowlist"
	AuthModePublic    AuthMode = "public"
)

// Config holds all application configuration.
type Config struct {
	// SSH server settings
	SSHAddr        string
	SSHHostKeyPath string
	SSHAuthMode    AuthMode
	AllowlistPath  string

	// WooCommerce API settings
	WooBaseURL        string
	WooConsumerKey    string
	WooConsumerSecret string
	WooStorePrefix    string

	// Cache settings
	CacheTTL        time.Duration
	StateDir        string
	BridgeKey       string
	CheckoutEnabled bool
}

// Load reads configuration from environment variables with defaults.
func Load() (*Config, error) {
	cfg := &Config{
		SSHAddr:           getEnv("SSH_ADDR", ":23234"),
		SSHHostKeyPath:    getEnv("SSH_HOSTKEY_PATH", "./.ssh_host_ed25519_key"),
		SSHAuthMode:       AuthMode(getEnv("SSH_AUTH_MODE", "allowlist")),
		AllowlistPath:     getEnv("SSH_ALLOWLIST_PATH", "./allowlist_authorized_keys"),
		WooBaseURL:        getEnv("WOO_BASE_URL", "http://127.0.0.1:18080"),
		WooConsumerKey:    os.Getenv("WOO_CONSUMER_KEY"),
		WooConsumerSecret: os.Getenv("WOO_CONSUMER_SECRET"),
		WooStorePrefix:    getEnv("WOO_STORE_PREFIX", "/wp-json/wc/store/v1"),
		StateDir:          getEnv("STATE_DIR", "./var/eva-terminal"),
		BridgeKey:         os.Getenv("EVA_BRIDGE_KEY"),
		CheckoutEnabled:   getEnv("CHECKOUT_ENABLED", "false") == "true",
	}

	// Parse cache TTL
	ttlSeconds, err := strconv.Atoi(getEnv("CACHE_TTL_SECONDS", "60"))
	if err != nil {
		return nil, errors.New("CACHE_TTL_SECONDS must be a valid integer")
	}
	cfg.CacheTTL = time.Duration(ttlSeconds) * time.Second
	if ttlSeconds <= 0 || ttlSeconds > 86400 {
		return nil, errors.New("CACHE_TTL_SECONDS must be between 1 and 86400")
	}
	if cfg.CheckoutEnabled && len(cfg.BridgeKey) < 32 {
		return nil, errors.New("checkout requires EVA_BRIDGE_KEY with at least 32 characters")
	}
	endpoint, parseErr := url.Parse(cfg.WooBaseURL)
	if parseErr != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("WOO_BASE_URL must be an HTTP(S) URL without credentials")
	}
	if cfg.BridgeKey != "" && endpoint.Scheme != "https" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "::1" {
		return nil, errors.New("the checkout bridge requires HTTPS outside localhost")
	}

	// Validate auth mode
	if cfg.SSHAuthMode != AuthModeAllowlist && cfg.SSHAuthMode != AuthModePublic {
		return nil, errors.New("SSH_AUTH_MODE must be 'allowlist' or 'public'")
	}

	return cfg, nil
}

// getEnv returns the value of an environment variable or a default value.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
