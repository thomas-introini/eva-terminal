package storefront

import (
	"testing"

	"github.com/thomas/eva-terminal-go/internal/storeapi"
)

func TestQuoteRejectsNativeCartErrors(t *testing.T) {
	cart := storeapi.Cart{
		Errors: []storeapi.CartError{{Code: "stock", Message: "Coffee is out of stock"}},
		Extensions: map[string]any{"eva_terminal": map[string]any{"quote": storeapi.Quote{
			Fingerprint: "valid-fingerprint", Total: "1000", Currency: "EUR",
		}}},
	}
	if _, err := QuoteFromCart(cart); err == nil || err.Error() != "Coffee is out of stock" {
		t.Fatalf("native cart validation was ignored: %v", err)
	}
}
