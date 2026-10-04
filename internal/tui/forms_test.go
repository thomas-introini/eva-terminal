package tui

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

// Run finite form commands as Bubble Tea would, including Batch and Sequence.
// Blink/tick messages are ignored so timers don't keep the test loop alive.
func drainCommands(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	commands := reflect.ValueOf(msg)
	commandSlice := reflect.TypeOf([]tea.Cmd{})
	if commands.Type().ConvertibleTo(commandSlice) {
		for _, next := range commands.Convert(commandSlice).Interface().([]tea.Cmd) {
			drainCommands(t, m, next)
		}
		return
	}
	switch msg.(type) {
	case cursor.BlinkMsg, spinner.TickMsg:
		return
	}
	updated, next := m.Update(msg)
	*m = updated.(Model)
	drainCommands(t, m, next)
}

func sendMessage(t *testing.T, m *Model, msg tea.Msg) {
	t.Helper()
	updated, cmd := m.Update(msg)
	*m = updated.(Model)
	drainCommands(t, m, cmd)
}

func press(t *testing.T, m *Model, code rune) {
	t.Helper()
	key := tea.KeyPressMsg{Code: code}
	if unicode.IsPrint(code) {
		key.Text = string(code)
	}
	sendMessage(t, m, key)
}

func variableModel(t *testing.T) Model {
	t.Helper()
	products := []woo.Product{{ID: 101, Name: "Coffee", Type: "variable", Attributes: []woo.Attribute{
		{Name: "Size", Options: []string{"250g", "1kg"}, Variation: true},
		{Name: "Grind Size", Options: []string{"Beans", "Espresso", "Filter"}},
	}}}
	variations := map[int][]woo.Variation{101: {
		{ID: 1011, Price: "14.99", Attributes: []woo.VariationAttribute{{Name: "Size", Option: "250g"}}},
		{ID: 1012, Price: "49.99", Attributes: []woo.VariationAttribute{{Name: "Size", Option: "1kg"}}},
	}}
	m, server := setupTestModel(t, products, variations)
	t.Cleanup(server.Close)
	sendMessage(t, &m, m.loadProducts()())
	press(t, &m, tea.KeyEnter)
	if len(m.productVariations) != 2 || m.productVariations[1].GetAttributeValue("Size") != "1kg" {
		t.Fatal("lost parent variation metadata")
	}
	press(t, &m, 'c')
	if m.configForm == nil || m.configForm.GetFocusedField().GetKey() != "variation_id" {
		t.Fatal("size form was not activated")
	}
	return m
}

func completeConfiguration(t *testing.T, m *Model) {
	t.Helper()
	press(t, m, tea.KeyDown)
	press(t, m, tea.KeyEnter)
	press(t, m, tea.KeyDown)
	press(t, m, tea.KeyEnter)
	if !m.configCompleted || m.selectedVariation == nil || m.selectedVariation.ID != 1012 || m.selectedGrindSize != "Espresso" {
		t.Fatalf("lost selection: complete=%v variation=%+v grind=%q err=%v", m.configCompleted, m.selectedVariation, m.selectedGrindSize, m.err)
	}
}

func TestConfigurationSelectionsAndReentry(t *testing.T) {
	m := variableModel(t)
	completeConfiguration(t, &m)
	press(t, &m, 'a')
	if m.viewState != ViewCart || len(m.localCart.Items) != 1 {
		t.Fatal("item was not added")
	}
	item := m.localCart.Items[0]
	if item.ProductID != 101 || item.VariationID != 1012 || item.GrindSize != "Espresso" || item.PriceMinor != 4999 {
		t.Fatalf("wrong cart item: %+v", item)
	}
	m.viewState = ViewConfigurator
	old := m.configForm
	press(t, &m, tea.KeyEscape)
	press(t, &m, 'c')
	if m.configForm == nil || m.configForm == old || m.configCompleted || m.selectedVariation != nil || m.selectedGrindSize != "" {
		t.Fatal("configuration wasn't reset on re-entry")
	}
	press(t, &m, tea.KeyEscape)
	press(t, &m, tea.KeyEscape)
	m.products = []woo.Product{{ID: 2, Type: "simple"}}
	m.updateProductList()
	press(t, &m, tea.KeyEnter)
	if m.configForm != nil || len(m.productVariations) != 0 {
		t.Fatal("obsolete configuration retained for another product")
	}
}

func TestInvalidVariationSelection(t *testing.T) {
	for _, id := range []int{0, 9999} {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			m := variableModel(t)
			m.configForm = huh.NewForm(huh.NewGroup(huh.NewSelect[int]().Key("variation_id").Options(huh.NewOption("Invalid", id))))
			if err := m.addToCart(); err == nil || !m.localCart.IsEmpty() || m.selectedVariation != nil {
				t.Fatal("invalid variation accepted")
			}
		})
	}
	m := variableModel(t)
	m.productVariations = nil
	cmd := m.initConfigurator()
	if cmd != nil || m.err == nil || m.configForm != nil {
		t.Fatal("missing variation metadata accepted")
	}
}

func TestSimpleGrindSelection(t *testing.T) {
	m, server := setupTestModel(t, nil, nil)
	defer server.Close()
	m.selectedProduct = &woo.Product{ID: 1, Type: "simple", Attributes: []woo.Attribute{{Name: "Grind Size", Options: []string{"Beans", "Espresso"}}}}
	m.viewState = ViewProductDetails
	press(t, &m, 'c')
	press(t, &m, tea.KeyDown)
	press(t, &m, tea.KeyEnter)
	if !m.configCompleted || m.selectedGrindSize != "Espresso" {
		t.Fatal("simple grind result lost")
	}
	press(t, &m, 'a')
	if m.localCart.Items[0].GrindSize != "Espresso" {
		t.Fatal("simple grind missing from cart")
	}
}

func TestAddressValidationAsyncCompletionAndReentry(t *testing.T) {
	m, server := setupTestModel(t, nil, nil)
	defer server.Close()
	m.localCart.AddItem(LocalCartItem{ProductID: 1, Quantity: 1})
	m.viewState = ViewCart
	press(t, &m, 'o')
	if m.addressForm == nil || m.addressForm.GetFocusedField().GetKey() != "" {
		t.Fatal("address form not activated")
	}
	press(t, &m, tea.KeyEnter)
	if !strings.Contains(m.addressForm.View(), "first name is required") || m.viewState != ViewAddress {
		t.Fatal("required field validation lost")
	}
	for _, value := range []string{"Ada", "Lovelace", "ada@example.com", "Via Roma 1", "Rome", "00100", "IT", "RM", "+39061234567"} {
		if value == "ada@example.com" {
			sendMessage(t, &m, tea.PasteMsg{Content: "invalid"})
			press(t, &m, tea.KeyEnter)
			if m.addressForm.GetFocusedField().Error() == nil || m.viewState != ViewAddress {
				t.Fatal("invalid email accepted")
			}
			sendMessage(t, &m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
		}
		sendMessage(t, &m, tea.PasteMsg{Content: value})
		press(t, &m, tea.KeyEnter)
	}
	// Confirm defaults to No, which must fail validation.
	press(t, &m, tea.KeyEnter)
	if m.viewState != ViewAddress {
		t.Fatal("unconfirmed address accepted")
	}
	press(t, &m, 'y')
	press(t, &m, tea.KeyEnter)
	if m.viewState != ViewReview || !m.customerInfo.AddressConfirmed || m.customerInfo.Email != "ada@example.com" {
		t.Fatalf("async completion failed: view=%v info=%+v", m.viewState, m.customerInfo)
	}
	old := m.addressForm
	sendMessage(t, &m, tea.FocusMsg{})
	if m.viewState != ViewReview {
		t.Fatal("completion transitioned twice")
	}
	press(t, &m, tea.KeyEscape)
	if m.viewState != ViewAddress || m.addressForm == old || m.addressForm.State != huh.StateNormal || m.customerInfo.AddressConfirmed {
		t.Fatal("address form not reset on re-entry")
	}
	press(t, &m, tea.KeyEscape)
	press(t, &m, 'o')
	if m.addressForm == nil || m.addressForm.State != huh.StateNormal {
		t.Fatal("cancelled address form not reinitialized")
	}
}

func TestSearchPasteAndKeyRelease(t *testing.T) {
	m, server := setupTestModel(t, nil, nil)
	defer server.Close()
	if view := m.View(); view.Content != "Loading..." || !view.AltScreen {
		t.Fatal("loading view missing alternate screen")
	}
	for _, code := range []rune{'q', 'f', '/', 'c'} {
		updated, cmd := m.Update(tea.KeyReleaseMsg{Code: code, Text: string(code)})
		m = updated.(Model)
		if cmd != nil || m.showSearch || m.inStockOnly || m.viewState != ViewProductList {
			t.Fatal("key release triggered a command")
		}
	}
	press(t, &m, '/')
	press(t, &m, 'q')
	sendMessage(t, &m, tea.PasteMsg{Content: " espresso"})
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 100, Height: 30})
	sendMessage(t, &m, tea.BlurMsg{})
	sendMessage(t, &m, tea.FocusMsg{})
	if m.searchInput.Value() != "q espresso" || !m.searchInput.Focused() || !m.searchInput.VirtualCursor() {
		t.Fatal("search typing/paste/focus failed")
	}
	press(t, &m, tea.KeyEscape)
	if m.showSearch || m.searchInput.Value() != "" {
		t.Fatal("search cancellation failed")
	}
	for _, key := range []tea.KeyPressMsg{{Code: 'q', Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}} {
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatal("quit shortcut missing")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("quit shortcut did not quit")
		}
	}
}

func TestVariationPricesJoinedByID(t *testing.T) {
	var products []storeapi.Product
	if err := json.Unmarshal([]byte(`[{"id":101,"type":"variable","attributes":[{"name":"Size","terms":[{"name":"Large bag","slug":"large"}]}],"variations":[{"id":1012,"attributes":[{"name":"Size","value":"large"}]},{"id":1011,"attributes":[{"name":"Size","value":"small"}]}]}]`), &products); err != nil {
		t.Fatal(err)
	}
	parent := mapStoreProductsToWoo(products)[0]
	variations := mapStoreVariationsToWoo([]storeapi.Product{{ID: 1011, Prices: storeapi.ProductPrices{Price: "1499"}}, {ID: 9999}, {ID: 1012, Prices: storeapi.ProductPrices{Price: "4999"}}}, parent.VariationDetails)
	if len(parent.Variations) != 2 || len(variations) != 2 || variations[1].ID != 1012 || variations[1].GetAttributeValue("Size") != "Large bag" || variations[1].Price != "49.99" {
		t.Fatalf("incorrect ID join: %+v", variations)
	}
}

func TestConfiguredVariationIntent(t *testing.T) {
	m := variableModel(t)
	completeConfiguration(t, &m)
	press(t, &m, 'a')
	item := m.localCart.Items[0]
	if item.VariationID != 1012 || item.Quantity != 1 || item.GrindSize != "Espresso" {
		t.Fatalf("wrong selected variation: %+v", item)
	}
}
