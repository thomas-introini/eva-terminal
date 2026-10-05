package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/thomas/eva-terminal-go/internal/cache"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
	"github.com/thomas/eva-terminal-go/internal/storefront"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

func uxModel() Model {
	m := NewModel(nil, cache.New[ProductListCacheKey, []woo.Product](time.Minute), cache.New[int, []woo.Variation](time.Minute))
	m.loadingProducts = false
	m.products = []woo.Product{{ID: 1, Name: strings.Repeat("Coffee 珈琲 ☕ ", 15), Type: "simple", Price: "12.00", CurrencyCode: "EUR", StockStatus: "instock", Description: strings.Repeat("Long description 珈琲. ", 150), Attributes: []woo.Attribute{{Name: "Grind Size", Options: []string{"Whole beans", strings.Repeat("Espresso 珈琲 ", 15)}}}}, {ID: 2, Name: "Decaf", Type: "simple", StockStatus: "instock"}}
	m.updateProductList()
	m.selectedProduct = &m.products[0]
	m.sessionState.Cart.Totals = storeapi.CartTotals{CurrencyCode: "EUR", CurrencyMinorUnit: 2, TotalPrice: "2400", TotalItems: "2400", TotalShipping: "0", TotalTax: "0"}
	for i := 0; i < 20; i++ {
		m.localCart.AddItem(LocalCartItem{ProductID: i + 1, Name: m.products[0].Name, GrindSize: "Whole beans", Quantity: 1})
	}
	m.customerInfo = &CustomerInfo{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Address: strings.Repeat("Via 珈琲 ", 25), City: "Rome", Postcode: "00100", Country: "IT"}
	m.sessionState.Cart.ShippingAddress = m.address()
	m.sessionState.Cart.BillingAddress = m.address()
	m.sessionState.Cart.NeedsShipping = true
	m.sessionState.Cart.ShippingRates = []storeapi.ShippingPackage{{Name: "Delivery", ShippingRates: []storeapi.ShippingRate{{RateID: "standard", Name: strings.Repeat("Standard 珈琲 ", 15), Price: "0", Selected: true}, {RateID: "express", Name: "Express", Price: "500"}}}}
	m.sessionState.Attempt = &storeapi.Attempt{AttemptID: "a", OrderID: 42, PaymentState: "pending", Currency: "EUR", Total: "2400", PaymentURL: "https://pay.example/" + strings.Repeat("very-long-url/", 100), ExpiresAt: time.Now().Add(time.Minute * 30).Unix()}
	return m
}

func TestSplashLifecycle(t *testing.T) {
	m := uxModel()
	m.splash = true
	m.viewState = ViewOrderConfirmation
	m.noColor = false
	m.profile = colorprofile.TrueColor
	m.applyTheme()
	// Deliver messages without running background subscriptions or real timers.
	update := func(msg tea.Msg) tea.Cmd {
		updated, cmd := m.Update(msg)
		m = updated.(Model)
		return cmd
	}
	if m.View().Content != "Loading..." || !m.splashStarted.IsZero() {
		t.Fatal("splash started before terminal dimensions arrived")
	}
	update(tea.WindowSizeMsg{})
	if !m.splashStarted.IsZero() {
		t.Fatal("empty dimensions started the splash")
	}
	if cmd := update(tea.WindowSizeMsg{Width: 80, Height: 24}); cmd == nil || m.splashStarted.IsZero() {
		t.Fatal("valid dimensions did not schedule the splash")
	}
	started := m.splashStarted
	for _, key := range []rune{'c', 'o', 'a', '/', '?', tea.KeyEnter, tea.KeyEscape} {
		if cmd := update(tea.KeyPressMsg{Code: key}); cmd != nil || m.viewState != ViewOrderConfirmation || m.showSearch || m.showHelp {
			t.Fatal("splash accepted a shopping action")
		}
	}
	m.viewState, m.showSearch = ViewProductList, true
	update(tea.PasteMsg{Content: "hidden input"})
	if m.searchInput.Value() != "" {
		t.Fatal("splash accepted pasted input")
	}
	m.viewState, m.showSearch = ViewOrderConfirmation, false
	for _, key := range []tea.KeyPressMsg{{Code: 'q', Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}} {
		cmd := update(key)
		if cmd == nil {
			t.Fatal("splash blocked quitting")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("splash did not issue quit")
		}
	}
	state := m.sessionState
	state.Attempt = &storeapi.Attempt{OrderID: 42, PaymentState: "paid", Currency: "EUR", Total: "2400"}
	state.Desired = []storefront.Intent{{ID: 1, Name: "Restored coffee", Quantity: 2}}
	update(storefrontMsg{state})
	update(productsLoadedMsg{[]woo.Product{{ID: 1, Name: "Loaded coffee", Type: "simple", StockStatus: "instock"}}})
	update(tea.BackgroundColorMsg{Color: color.White})
	if m.sessionState.Attempt.PaymentState != "paid" || m.localCart.Items[0].Quantity != 2 || m.loadingProducts || m.products[0].Name != "Loaded coffee" || m.dark {
		t.Fatal("splash blocked background updates")
	}
	update(tea.WindowSizeMsg{Width: 40, Height: 12})
	if !strings.Contains(m.View().Content, "Resize to at least") {
		t.Fatal("splash hid resize guidance")
	}
	update(tea.WindowSizeMsg{Width: 120, Height: 36})
	if m.splashStarted != started {
		t.Fatal("resizing restarted the splash")
	}
	shape := ansi.Strip(m.splashText())
	frames := map[string]bool{m.splashText(): true}
	for elapsed := splashInterval; elapsed < splashDuration; elapsed += splashInterval {
		cmd := update(splashTickMsg(started.Add(elapsed)))
		if !m.splash || cmd == nil || ansi.Strip(m.splashText()) != shape {
			t.Fatal("pulse ended early or moved the logo")
		}
		frames[m.splashText()] = true
	}
	if len(frames) != 3 {
		t.Fatal("orange dot did not change brightness")
	}
	update(splashTickMsg(started.Add(time.Second - time.Millisecond)))
	if !m.splash {
		t.Fatal("splash ended before one second")
	}
	if cmd := update(splashTickMsg(started.Add(time.Second))); m.splash || cmd != nil || m.viewState != ViewOrderConfirmation || !strings.Contains(m.View().Content, "Paid · payment confirmed") {
		t.Fatal("splash did not reveal the updated payment screen at one second")
	}
	if cmd := update(splashTickMsg(started.Add(2 * time.Second))); cmd != nil {
		t.Fatal("dismissed splash kept ticking")
	}
	update(tea.WindowSizeMsg{Width: 80, Height: 24})
	update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if m.splash || m.viewState != ViewCart {
		t.Fatal("splash replayed or input stayed blocked")
	}
}

func TestSplashRendering(t *testing.T) {
	m := uxModel()
	m.splash = true
	logoLines := strings.Split(splashLogo, "\n")
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}} {
		m.width, m.height = size[0], size[1]
		view := m.View()
		lines := strings.Split(ansi.Strip(view.Content), "\n")
		if !view.AltScreen || len(lines) != size[1] {
			t.Fatal("splash did not fill the alternate screen")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("splash exceeded the terminal width")
			}
		}
		top, left := (size[1]-len(logoLines))/2, (size[0]-48)/2
		for i, line := range logoLines {
			if strings.TrimRight(lines[top+i], " ") != strings.Repeat(" ", left)+line {
				t.Fatalf("logo was not centered at %dx%d:\n%s", size[0], size[1], view.Content)
			}
		}
		for _, character := range ansi.Strip(view.Content) {
			if character > 127 {
				t.Fatal("splash contains non-ASCII characters")
			}
		}
	}
	for _, dark := range []bool{true, false} {
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.Ascii} {
			for _, noColor := range []bool{false, true} {
				m.dark, m.profile, m.noColor = dark, profile, noColor
				m.applyTheme()
				m.splashFrame = 0
				dim := m.splashText()
				m.splashFrame = 2
				bright := m.splashText()
				if noColor || profile == colorprofile.Ascii {
					if dim != bright || strings.Contains(dim, "\x1b") {
						t.Fatal("monochrome splash emitted color or animation")
					}
				} else if profile == colorprofile.TrueColor && dim == bright {
					t.Fatal("true-color dot did not pulse")
				}
			}
		}
	}
}

func TestEveryScreenFitsAndKeepsActions(t *testing.T) {
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}, {120, 50}, {40, 12}} {
		for state := ViewProductList; state <= ViewShipping; state++ {
			t.Run(fmt.Sprintf("%dx%d/%d", size[0], size[1], state), func(t *testing.T) {
				m := uxModel()
				m.viewState = state
				if state == ViewAddress {
					drainCommands(t, &m, m.initAddressForm())
				}
				if state == ViewShipping {
					drainCommands(t, &m, m.initShipping())
				}
				sendMessage(t, &m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				checkBounds(t, m, size)
				if size[0] < 60 {
					if !strings.Contains(m.View().Content, "Resize to at least") {
						t.Fatal("missing resize guard")
					}
					oldState := m.viewState
					press(t, &m, tea.KeyEnter)
					if m.viewState != oldState {
						t.Fatal("small terminal changed shopping state")
					}
					sendMessage(t, &m, tea.WindowSizeMsg{Width: 80, Height: 24})
					if m.viewState != oldState {
						t.Fatal("resize lost screen")
					}
					return
				}
				text := strings.Join(strings.Fields(ansi.Strip(m.View().Content)), " ")
				for _, binding := range m.bindings() {
					if binding.essential && !strings.Contains(text, binding.label) {
						t.Fatalf("essential action %q clipped:\n%s", binding.label, m.View().Content)
					}
				}
				press(t, &m, '?')
				checkBounds(t, m, size)
				press(t, &m, tea.KeyEnd)
				checkBounds(t, m, size)
				press(t, &m, tea.KeyEscape)
				if m.viewState != state {
					t.Fatal("help changed screen")
				}
			})
		}
	}
}

func TestCompactLogoHeadroom(t *testing.T) {
	m := uxModel()
	for _, size := range [][2]int{{79, 23}, {79, 24}, {80, 24}, {120, 36}, {79, 39}, {79, 40}, {120, 50}, {80, 24}} {
		sendMessage(t, &m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		checkBounds(t, m, size)
		text := ansi.Strip(m.View().Content)
		smallVisible := strings.Contains(text, "##oo  ###. ## ##")
		largeVisible := strings.Contains(text, ".###ooo    #######. .###   ####.")
		if smallVisible != (size[1] >= 24 && size[1] < 40) || largeVisible != (size[1] >= 40) || m.viewState != ViewProductList {
			t.Fatal("resizing lost the logo or shopping screen")
		}
	}
}

func checkBounds(t *testing.T, m Model, size [2]int) {
	t.Helper()
	view := m.View().Content
	if height := strings.Count(view, "\n") + 1; height > size[1] {
		t.Fatalf("height %d > %d:\n%s", height, size[1], view)
	}
	for _, line := range strings.Split(view, "\n") {
		if width := ansi.StringWidth(line); width > size[0] {
			t.Fatalf("width %d > %d: %q", width, size[0], line)
		}
	}
	if !m.tooSmall() {
		pw, ph := m.panelSize()
		top, left := (size[1]-ph)/2, (size[0]-pw)/2
		lines := strings.Split(ansi.Strip(view), "\n")
		if !strings.HasPrefix(lines[top], strings.Repeat(" ", left)+"┌") || ansi.StringWidth(strings.TrimSpace(lines[top])) != pw {
			t.Fatalf("panel not centered at (%d,%d):\n%s", left, top, view)
		}
		if top >= 3 {
			art, width := smallLogo, 16
			if top >= 6 {
				art, width = compactLogo, 32
			}
			logoLines := strings.Split(art, "\n")
			logoTop, logoLeft := (top-len(logoLines))/2, (size[0]-width)/2
			for i, line := range logoLines {
				if strings.TrimRight(lines[logoTop+i], " ") != strings.Repeat(" ", logoLeft)+line {
					t.Fatalf("compact logo not centered above the panel:\n%s", view)
				}
			}
		}
	}
}

func TestCenteredShopLayouts(t *testing.T) {
	m := variableModel(t)
	m.products[0].Name = "House Blend Signature"
	m.products[0].Description = "Smooth and balanced coffee with notes of chocolate, caramel, and roasted nuts. Choose your bag size and grind before adding it to your cart."
	m.products = append(m.products, woo.Product{ID: 2, Name: "Ethiopian Yirgacheffe", Type: "simple", StockStatus: "instock"}, woo.Product{ID: 3, Name: "Colombian Supremo", Type: "simple", StockStatus: "instock"})
	drainCommands(t, &m, m.updateProductList())
	m.sessionState.Cart.Totals = storeapi.CartTotals{CurrencyCode: "EUR", CurrencyMinorUnit: 2, TotalPrice: "0"}
	for _, size := range [][2]int{{60, 18}, {79, 24}, {80, 18}, {80, 24}, {120, 36}, {120, 50}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			sendMessage(t, &m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			checkBounds(t, m, size)
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(ansi.Strip(m.productList.View()), "House Blend Signature") {
				t.Fatalf("coffee name clipped in the product list:\n%s", view)
			}
			for _, text := range []string{"s shop", "c cart EUR 0.00 [0]", "Size: < 250g >", "Grind: < Whole Beans >", "Qty: < 1 >", "[ Add to cart ]", "enter add to cart", "↑/↓ coffees", "←/→ change option"} {
				if !strings.Contains(view, text) {
					t.Fatalf("missing %q:\n%s", text, view)
				}
			}
			t.Logf("\n%s", view)
		})
	}
	// Help and resizing preserve option focus and don't consume a configuration.
	press(t, &m, tea.KeyTab)
	focus := m.shopFocus
	press(t, &m, '?')
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 40, Height: 12})
	press(t, &m, tea.KeyEnter)
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 60, Height: 18})
	press(t, &m, tea.KeyEscape)
	if m.shopFocus != focus || m.selectedVariation.ID != 1011 || !m.localCart.IsEmpty() {
		t.Fatal("help/resize lost focus or added an item")
	}
	press(t, &m, tea.KeyPgDown)
	if m.scroll[ViewProductList] == 0 || !strings.Contains(m.View().Content, "Add to cart") {
		t.Fatal("compact description cannot scroll with action visible")
	}
}

func TestInlineOptionsRefreshAndDelayedResponses(t *testing.T) {
	m := variableModel(t)
	completeConfiguration(t, &m)
	old := m.loadVariations(101)().(variationsLoadedMsg)
	old.variations = append([]woo.Variation{}, old.variations...)
	old.variations[1].Price = "0.01"
	current := m.selectShopProduct(true)()
	sendMessage(t, &m, current)
	sendMessage(t, &m, old)
	if m.selectedVariation.ID != 1012 || m.selectedGrindSize != "Espresso" || m.selectedVariation.Price != "49.99" {
		t.Fatal("late result lost the current draft")
	}
	// A selected size becoming unavailable cannot silently switch to a different size.
	variations := append([]woo.Variation{}, m.productVariations...)
	variations[1].StockStatus = "outofstock"
	sendMessage(t, &m, variationsLoadedMsg{productID: 101, requestID: m.variationRequest, variations: variations})
	press(t, &m, tea.KeyEnter)
	if m.viewState != ViewProductList || !m.localCart.IsEmpty() || m.selectedVariation.ID != 1012 || !strings.Contains(m.View().Content, "out of stock") {
		t.Fatal("unavailable size added or silently replaced")
	}
	m.shopFocus = "size"
	press(t, &m, tea.KeyLeft)
	if m.purchaseReason() != "Available to purchase" {
		t.Fatal("available size did not recover purchase")
	}
	// If only one valid option remains, its selector must still allow explicit recovery.
	draft := m.productDrafts[101]
	draft.variationID = 1012
	m.productDrafts[101] = draft
	sendMessage(t, &m, variationsLoadedMsg{productID: 101, requestID: m.variationRequest, variations: variations[:1]})
	m.shopFocus = "size"
	press(t, &m, tea.KeyRight)
	if m.selectedVariation == nil || m.selectedVariation.ID != 1011 {
		t.Fatal("cannot recover removed size")
	}
	// Removed grinds also require explicit choice, including a single remaining grind.
	m.products[0].Attributes[1].Options = []string{"Whole Beans"}
	drainCommands(t, &m, m.updateProductList())
	if m.purchaseReason() != "Choose an available grind" {
		t.Fatal("invalid grind accepted after refresh")
	}
	m.shopFocus = "grind"
	press(t, &m, tea.KeyRight)
	if m.selectedGrindSize != "Whole Beans" || m.purchaseReason() != "Available to purchase" {
		t.Fatal("cannot recover removed grind")
	}
	sendMessage(t, &m, variationsErrorMsg{productID: 101, requestID: m.variationRequest, err: errors.New("offline")})
	if !strings.Contains(m.View().Content, "offline") || m.purchaseReason() == "Available to purchase" {
		t.Fatal("failed options allowed purchase or hid recovery")
	}
}

func TestShopReadonlyOptionsFocusAndLongValues(t *testing.T) {
	m := uxModel()
	m.sessionState.Attempt = nil
	m.products[0].Attributes = []woo.Attribute{{Name: "Size", Options: []string{"250g"}}, {Name: "Grind Size", Options: []string{"Whole Beans"}}}
	m.productDrafts = make(map[int]productDraft)
	drainCommands(t, &m, m.updateProductList())
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 60, Height: 18})
	if m.shopFocus != "quantity" || !strings.Contains(m.View().Content, "Size: 250g") || !strings.Contains(m.View().Content, "Grind: Whole Beans") {
		t.Fatal("single options are not read-only")
	}
	press(t, &m, tea.KeyLeft)
	if m.selectedQuantity != 1 {
		t.Fatal("quantity fell below one")
	}
	press(t, &m, tea.KeyRight)
	press(t, &m, tea.KeyTab)
	if m.shopFocus != "add" || m.selectedQuantity != 2 {
		t.Fatal("Tab did not skip read-only choices")
	}
	sendMessage(t, &m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.shopFocus != "quantity" {
		t.Fatal("Shift+Tab did not restore quantity focus")
	}
	long := strings.Repeat("Espresso 珈琲 ", 20)
	m.products[0].Attributes[1].Options = []string{"Whole Beans", long}
	drainCommands(t, &m, m.updateProductList())
	m.shopFocus = "grind"
	press(t, &m, tea.KeyRight)
	checkBounds(t, m, [2]int{60, 18})
	press(t, &m, '?')
	if !strings.Contains(m.helpText(), long) || !strings.Contains(m.helpText(), m.products[0].Name) {
		t.Fatal("long option/name inaccessible")
	}
	press(t, &m, tea.KeyEscape)
	if m.shopFocus != "grind" || m.selectedGrindSize != long {
		t.Fatal("help lost the long choice")
	}
	press(t, &m, tea.KeyDown)
	if strings.Contains(m.helpText(), "\nSize:") || strings.Contains(m.helpText(), "\nGrind:") {
		t.Fatal("help invents unavailable product options")
	}
}

func TestSearchLivePasteClearAndSelectionRetention(t *testing.T) {
	m := uxModel()
	m.sessionState.Attempt = nil
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.productList.Select(1)
	sendMessage(t, &m, productsLoadedMsg{[]woo.Product{m.products[1], m.products[0]}})
	if m.productList.SelectedItem().(productItem).product.ID != 2 {
		t.Fatal("refresh lost selection ID")
	}
	press(t, &m, '/')
	sendMessage(t, &m, tea.PasteMsg{Content: "Decaf"})
	if len(m.productList.Items()) != 1 {
		t.Fatal("paste did not filter immediately")
	}
	press(t, &m, tea.KeyEnter)
	if !strings.Contains(m.View().Content, `search: "Decaf"`) {
		t.Fatal("applied query disappeared")
	}
	press(t, &m, 'c')
	press(t, &m, 's')
	if m.searchInput.Value() != "Decaf" || m.productList.SelectedItem().(productItem).product.ID != 2 {
		t.Fatal("return navigation lost search/selection")
	}
	press(t, &m, '/')
	press(t, &m, tea.KeyEscape)
	if len(m.productList.Items()) != 2 || m.searchInput.Value() != "" {
		t.Fatal("clear did not restore products")
	}
}

func TestDraftsErrorsHelpAndInputRouting(t *testing.T) {
	m := uxModel()
	m.sessionState.Attempt = nil
	m.viewState = ViewCart
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 60, Height: 18})
	// Open the coupon field without a real session; input routing is shared.
	m.couponMode = "apply"
	drainCommands(t, &m, m.cartInput.Focus())
	press(t, &m, 'q')
	sendMessage(t, &m, tea.PasteMsg{Content: "COFFEE10"})
	if m.cartInput.Value() != "qCOFFEE10" {
		t.Fatal("coupon input dropped or duplicated")
	}
	press(t, &m, '?')
	press(t, &m, 'z')
	press(t, &m, tea.KeyEscape)
	if !m.cartInput.Focused() || m.cartInput.Value() != "qCOFFEE10" {
		t.Fatal("help lost focus/draft")
	}
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.cartInput.Value() != "qCOFFEE10" {
		t.Fatal("resize lost coupon draft")
	}
	m.couponMode = ""
	m.viewState = ViewAddress
	drainCommands(t, &m, m.initAddressForm())
	sendMessage(t, &m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	sendMessage(t, &m, tea.PasteMsg{Content: "Draft name"})
	st := storefront.SessionState{Error: "offline", Billing: &storeapi.CustomerAddress{FirstName: "remote"}}
	m.applyShopper(st)
	if m.customerInfo.FirstName != "Draft name" || m.err != nil {
		t.Fatal("background update overwrote draft or copied session error")
	}
	if !strings.Contains(m.View().Content, "offline") {
		t.Fatal("session error invisible")
	}
	m.err = errors.New("action failed")
	m.applyShopper(storefront.SessionState{})
	if !strings.Contains(m.View().Content, "action failed") || strings.Contains(m.View().Content, "offline") {
		t.Fatal("error recovery cleared the wrong message")
	}
	for state := ViewProductList; state <= ViewShipping; state++ {
		m.viewState = state
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatalf("Ctrl+C absent on %d", state)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("Ctrl+C did not quit")
		}
	}
}

func TestPaymentStaysOpenAndScrollReachesFullURL(t *testing.T) {
	m := uxModel()
	m.viewState = ViewOrderConfirmation
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 60, Height: 18})
	for _, key := range []rune{'z', 'p', 'u', tea.KeyEnter} {
		press(t, &m, key)
		if m.viewState != ViewOrderConfirmation {
			t.Fatal("unrelated key dismissed payment")
		}
	}
	press(t, &m, 'y')
	if m.notice != "Copy requested" {
		t.Fatal("clipboard feedback missing")
	}
	press(t, &m, tea.KeyEnd)
	if !strings.Contains(m.View().Content, "Use r to recheck") {
		t.Fatal("scroll cannot reach payment instructions")
	}
	press(t, &m, 'x')
	if !m.confirmCancel {
		t.Fatal("cancel lacked confirmation")
	}
	press(t, &m, tea.KeyEscape)
	if m.confirmCancel || m.viewState != ViewOrderConfirmation {
		t.Fatal("declining cancellation lost payment")
	}
	m.confirmCancel = true
	a := *m.sessionState.Attempt
	a.PaymentState = "paid"
	m.applyShopper(storefront.SessionState{Attempt: &a})
	if m.confirmCancel {
		t.Fatal("paid payment kept cancellation prompt")
	}
}

func TestCartLineTotalsMatchGrindAndTotalsStayVisible(t *testing.T) {
	m := uxModel()
	m.viewState = ViewCart
	m.localCart.Items = []LocalCartItem{{ProductID: 1, Name: "Coffee", Quantity: 1, GrindSize: "Beans"}, {ProductID: 1, Name: "Coffee", Quantity: 1, GrindSize: "Espresso"}}
	items := []storeapi.CartItem{
		{ID: 1, Quantity: 1, Extensions: map[string]json.RawMessage{"eva_terminal": json.RawMessage(`{"grind":"Espresso"}`)}},
		{ID: 1, Quantity: 1, Extensions: map[string]json.RawMessage{"eva_terminal": json.RawMessage(`{"grind":"Beans"}`)}},
	}
	items[0].Totals.LineTotal = "2200"
	items[1].Totals.LineTotal = "1100"
	m.sessionState.Cart.Items = items
	text := m.cartContent(false)
	if !strings.Contains(text, "Beans × 1 · EUR 11.00") || !strings.Contains(text, "Espresso × 1 · EUR 22.00") {
		t.Fatalf("grinds mixed: %s", text)
	}
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 60, Height: 18})
	m.sessionState.Pending = true
	press(t, &m, tea.KeyEnd)
	if !strings.Contains(m.View().Content, "Items: EUR 24.00") || !strings.Contains(m.View().Content, "Tax: EUR 0.00") {
		t.Fatal("totals breakdown scrolled away")
	}
	if !strings.Contains(m.View().Content, "Total: EUR 24.00 · updating") {
		t.Fatal("total scrolled away or missing pending label")
	}
}

func TestThemeAndFocusedFields(t *testing.T) {
	m := uxModel()
	m.sessionState.Attempt = nil
	sendMessage(t, &m, tea.BackgroundColorMsg{Color: color.White})
	if m.dark {
		t.Fatal("light background ignored")
	}
	sendMessage(t, &m, tea.ColorProfileMsg{Profile: colorprofile.Ascii})
	m.viewState = ViewAddress
	drainCommands(t, &m, m.initAddressForm())
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 60, Height: 18})
	for range 8 {
		press(t, &m, tea.KeyTab)
		checkBounds(t, m, [2]int{60, 18})
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Phone") {
		t.Fatal("last address field unreachable")
	}
	if strings.Contains(m.View().Content, "38;2") || strings.Contains(m.View().Content, "38;5") {
		t.Fatal("ASCII terminal received color")
	}
	m.noColor = true
	m.profile = colorprofile.TrueColor
	m.applyTheme()
	if strings.Contains(m.View().Content, "38;") {
		t.Fatal("NO_COLOR ignored")
	}
	m.viewState = ViewProductList
	m.noColor = false
	m.applyTheme()
	if !strings.Contains(m.View().Content, "38;2;") || !strings.Contains(m.View().Content, "48;2;") {
		t.Fatal("true-color shop lost its text or amber selection colors")
	}
}

func TestCheckoutAvailabilityAndRepeatedOperations(t *testing.T) {
	m := uxModel()
	m.sessionState.Attempt = nil
	m.viewState = ViewCart
	// Only availability is exercised here, so a zero controller is sufficient.
	m.shopper = &storefront.Session{}
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 80, Height: 24})
	press(t, &m, tea.KeyEnter)
	if m.viewState != ViewCart || !strings.Contains(m.View().Content, "Checkout unavailable") {
		t.Fatal("disabled checkout allowed navigation")
	}
	m.checkoutEnabled = true
	m.sessionState.Pending = true
	press(t, &m, 'o')
	if m.viewState != ViewCart {
		t.Fatal("pending cart allowed checkout")
	}
	m.sessionState.Pending = false
	m.couponMode = "apply"
	m.couponBusy = true
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("coupon submitted twice")
	}
	m.couponMode = ""
	m.couponBusy = false
	m.viewState = ViewShipping
	m.shippingBusy = true
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("delivery submitted twice")
	}
	m.viewState = ViewReview
	m.shippingBusy = false
	m.creatingOrder = true
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("checkout submitted twice")
	}
	m.viewState = ViewOrderConfirmation
	m.creatingOrder = false
	m.cancelling = true
	m.sessionState.Attempt = &storeapi.Attempt{AttemptID: "a", PaymentState: "awaiting_payment"}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if cmd != nil || m.confirmCancel {
		t.Fatal("cancellation submitted twice")
	}
}

func TestLateVariationResultsDoNotOverwriteAnotherProductOrError(t *testing.T) {
	m := uxModel()
	m.viewState = ViewProductList
	m.selectedProduct = &woo.Product{ID: 2, Type: "variable"}
	m.loadingVariations = true
	sendMessage(t, &m, variationsLoadedMsg{productID: 1, variations: []woo.Variation{{ID: 11}}})
	if !m.loadingVariations || len(m.productVariations) != 0 {
		t.Fatal("stale options replaced current product")
	}
	m.viewState = ViewCart
	m.err = errors.New("coupon failed")
	sendMessage(t, &m, variationsLoadedMsg{productID: 2, variations: []woo.Variation{{ID: 21}}})
	if m.err == nil || m.err.Error() != "coupon failed" {
		t.Fatal("background options cleared unrelated action error")
	}
}

func TestLongActiveInputsKeepTheCursorVisible(t *testing.T) {
	m := uxModel()
	m.sessionState.Attempt = nil
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 60, Height: 18})
	press(t, &m, '/')
	sendMessage(t, &m, tea.PasteMsg{Content: strings.Repeat("珈琲", 40) + "XYZ"})
	if width := ansi.StringWidth(m.contextLine()); width > 58 {
		t.Fatalf("search input extends past screen: %d", width)
	}
	if !strings.Contains(m.contextLine(), "XYZ") {
		t.Fatal("search cursor end clipped")
	}
	m.showSearch = false
	m.viewState = ViewCart
	m.couponMode = "apply"
	m.sizeComponents()
	drainCommands(t, &m, m.cartInput.Focus())
	sendMessage(t, &m, tea.PasteMsg{Content: strings.Repeat("LONG", 40) + "XYZ"})
	if width := ansi.StringWidth(m.contextLine()); width > 58 {
		t.Fatalf("coupon input extends past screen: %d", width)
	}
	if !strings.Contains(m.contextLine(), "XYZ") {
		t.Fatal("coupon cursor end clipped")
	}
}

func TestCatalogEmptyLoadingAndRefreshFailureCopy(t *testing.T) {
	m := uxModel()
	m.products = nil
	m.updateProductList()
	if !strings.Contains(m.bodyText(), "catalog is empty") {
		t.Fatal("empty catalog copy missing")
	}
	m.loadingProducts = true
	if !strings.Contains(m.bodyText(), "Loading coffee") {
		t.Fatal("initial loading copy missing")
	}
	m.loadingProducts = false
	m.searchInput.SetValue("no matches")
	if !strings.Contains(m.bodyText(), "No matches") {
		t.Fatal("no matches confused with empty catalog")
	}
	m.searchInput.SetValue("")
	m.catalogErr = errors.New("offline")
	if !strings.Contains(m.bodyText(), "unavailable") {
		t.Fatal("initial failure lacks recovery")
	}
	m.products = []woo.Product{{ID: 1, Name: "Saved coffee"}}
	m.updateProductList()
	sendMessage(t, &m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(m.View().Content, "Saved coffee") || !strings.Contains(m.View().Content, "Refresh failed") {
		t.Fatal("failed refresh lost saved catalog")
	}
}
