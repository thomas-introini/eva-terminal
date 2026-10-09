package storefront

import (
	"errors"
	"net"

	"github.com/thomas/eva-terminal-go/internal/analytics"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
)

func (s *Session) event(c analytics.Context, path, name string, data map[string]any) {
	if s.tracker != nil && c.Valid() {
		s.tracker.Event(analytics.Event{Context: c, Path: path, Name: name, Data: data})
	}
}

// Legacy Store API carts expose a sellable ID without a reliable parent mapping.
func IntentAnalyticsIDs(item Intent) map[string]any {
	data := map[string]any{}
	if item.ProductID > 0 {
		data["product_id"] = item.ProductID
	}
	if item.VariationID > 0 {
		data["variation_id"] = item.VariationID
	}
	if len(data) == 0 {
		data["catalog_item_id"] = item.ID
	}
	return data
}

func AnalyticsErrorCode(err error) string {
	var api *storeapi.StoreError
	if errors.As(err, &api) {
		switch {
		case api.StatusCode == 409:
			return "conflict"
		case api.StatusCode == 408 || api.StatusCode == 429 || api.StatusCode >= 500:
			return "unavailable"
		case api.StatusCode >= 400:
			return "validation"
		}
	}
	var network net.Error
	if errors.As(err, &network) {
		return "network"
	}
	return "unknown"
}
