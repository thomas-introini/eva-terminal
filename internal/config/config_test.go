package config

import (
	"context"
	"testing"
	"time"

	"github.com/thomas/eva-terminal-go/internal/analytics"
)

func TestInvalidAnalyticsDoesNotDisableStorefront(t *testing.T) {
	t.Setenv("CHECKOUT_ENABLED", "true")
	t.Setenv("EVA_BRIDGE_KEY", "test-bridge-key-with-at-least-32-characters")
	t.Setenv("WOO_BASE_URL", "http://127.0.0.1:18080")
	t.Setenv("UMAMI_ENABLED", "true")
	t.Setenv("UMAMI_BASE_URL", "https://name:secret@example.com")
	t.Setenv("CACHE_TTL_SECONDS", "60")
	t.Setenv("CATALOG_REFRESH_COOLDOWN_SECONDS", "30")
	t.Setenv("SSH_AUTH_MODE", "allowlist")
	cfg, err := Load()
	if err != nil || !cfg.CheckoutEnabled {
		t.Fatalf("analytics disabled shopping: %v", err)
	}
	if collector, err := analytics.New(context.Background(), cfg.Analytics, nil); err == nil || collector != nil {
		t.Fatal("invalid analytics worker started")
	}
	t.Setenv("UMAMI_ENABLED", "invalid")
	cfg, err = Load()
	if err != nil || cfg.Analytics.Enabled || !cfg.CheckoutEnabled {
		t.Fatal("malformed flag changed checkout availability")
	}
}

func TestCatalogRefreshCooldownConfig(t *testing.T) {
	t.Setenv("CHECKOUT_ENABLED", "false")
	t.Setenv("EVA_BRIDGE_KEY", "")
	t.Setenv("WOO_BASE_URL", "http://127.0.0.1:18080")
	t.Setenv("CACHE_TTL_SECONDS", "60")
	t.Setenv("SSH_AUTH_MODE", "allowlist")
	t.Setenv("UMAMI_ENABLED", "false")
	for _, tt := range []struct {
		name, value string
		want        time.Duration
	}{
		{"default", "", 30 * time.Second},
		{"custom", "120", 2 * time.Minute},
		{"minimum", "1", time.Second},
		{"maximum", "86400", 24 * time.Hour},
		{"zero", "0", 0},
		{"negative", "-1", 0},
		{"too_large", "86401", 0},
		{"fraction", "1.5", 0},
		{"not_integer", "invalid", 0},
		{"overflow", "99999999999999999999", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CATALOG_REFRESH_COOLDOWN_SECONDS", tt.value)
			cfg, err := Load()
			if tt.want == 0 {
				if err == nil {
					t.Fatal("invalid cooldown accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CatalogRefreshCooldown != tt.want {
				t.Fatalf("cooldown=%v, want %v", cfg.CatalogRefreshCooldown, tt.want)
			}
		})
	}
}
