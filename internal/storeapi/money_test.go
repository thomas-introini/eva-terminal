package storeapi

import "testing"

func TestIntegerMoneyAndCurrentPrice(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		unit int
		want string
	}{{"1299", 2, "12.99"}, {"1", 3, "0.001"}, {"123", 0, "123"}, {"-9223372036854775808", 2, "-92233720368547758.08"}} {
		if got := FormatMinor(tc.raw, tc.unit); got != tc.want {
			t.Fatalf("%s: %s", tc.raw, got)
		}
	}
	p := ProductPrices{CurrencyCode: "EUR", CurrencyMinorUnit: 2, Price: "1200", SalePrice: "999", RegularPrice: "1500"}
	if p.DisplayPrice() != "12.00" {
		t.Fatal("stale sale field overrode current Woo price")
	}
}
