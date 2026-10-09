package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/thomas/eva-terminal-go/internal/cache"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
	"github.com/thomas/eva-terminal-go/internal/storefront"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

// ViewState represents the current view in the application.
type ViewState int

const (
	ViewProductList ViewState = iota
	ViewCart
	ViewAddress // Address entry
	ViewReview  // Review order with calculated totals
	ViewOrderConfirmation
	ViewShipping
)

// ProductListCacheKey is the cache key for product lists.
type ProductListCacheKey struct {
	Page        int
	PerPage     int
	Search      string
	InStockOnly bool
}

// Model is the main Bubble Tea model for the TUI.
type Model struct {
	analytics       analyticsState
	ctx             context.Context
	catalog         *storefront.Catalog
	catalogUpdated  time.Time
	shopper         *storefront.Session
	changes         <-chan storefront.SessionState
	sessionState    storefront.SessionState
	checkoutEnabled bool
	quote           storeapi.Quote
	preparing       bool
	polling         bool
	pollStep        int
	cartInput       textinput.Model
	couponMode      string
	shippingForm    *huh.Form
	shippingReturn  ViewState
	shippingBusy    bool
	couponBusy      bool
	cancelling      bool
	checking        bool
	confirmCancel   bool
	showHelp        bool
	helpScroll      int
	scroll          [ViewShipping + 1]int
	selectedID      int
	notice          string
	catalogErr      error
	dark            bool
	noColor         bool
	profile         colorprofile.Profile
	addressDraft    bool

	// Dependencies
	storeClient     *storeapi.Client
	productsCache   *cache.Cache[ProductListCacheKey, []woo.Product]
	variationsCache *cache.Cache[int, []woo.Variation]

	// View state
	viewState     ViewState
	width         int
	height        int
	styles        Styles
	splash        bool
	splashStarted time.Time
	splashFrame   int

	// Product list view
	productList     list.Model
	products        []woo.Product
	searchInput     textinput.Model
	showSearch      bool
	inStockOnly     bool
	currentPage     int
	perPage         int
	loadingProducts bool
	listSpinner     spinner.Model

	// Selected coffee and inline options
	selectedProduct   *woo.Product
	productVariations []woo.Variation
	loadingVariations bool
	variationRequest  int
	optionsErr        error

	// Per-product shopping drafts (session-local)
	selectedVariation *woo.Variation
	selectedGrindSize string
	selectedQuantity  int
	shopFocus         string
	productDrafts     map[int]productDraft

	// Local cart (per SSH session)
	localCart *LocalCart

	// Review/Checkout
	addressForm   *huh.Form
	customerInfo  *CustomerInfo
	creatingOrder bool

	// Error handling
	err error
}

// CustomerInfo holds customer information for checkout.
type CustomerInfo struct {
	FirstName string
	LastName  string
	Email     string
	Address   string
	City      string
	Postcode  string
	State     string
	Phone     string
	Country   string
}

// productItem implements list.Item for products.
type productItem struct {
	product woo.Product
}

func (i productItem) Title() string {
	return i.product.Name
}

func (i productItem) Description() string {
	price := i.product.GetDisplayPrice()
	stock := "In Stock"
	if !i.product.IsInStock() {
		stock = "Out of Stock"
	}
	typeLabel := ""
	if i.product.IsVariable() {
		typeLabel = " · size options"
	}
	return fmt.Sprintf("%s %s • %s%s", i.product.CurrencyCode, price, stock, typeLabel)
}

func (i productItem) FilterValue() string {
	return i.product.Name
}

// Messages
const (
	splashDuration = time.Second
	splashInterval = 100 * time.Millisecond
)

type splashTickMsg time.Time

type (
	productsLoadedMsg struct {
		products []woo.Product
	}
	variationsLoadedMsg struct {
		productID  int
		requestID  int
		variations []woo.Variation
	}
	variationsErrorMsg struct {
		productID int
		requestID int
		err       error
	}
	errMsg struct {
		err error
	}
)

// NewModel creates a new TUI model.
func NewModel(storeClient *storeapi.Client, productsCache *cache.Cache[ProductListCacheKey, []woo.Product], variationsCache *cache.Cache[int, []woo.Variation]) Model {
	styles := DefaultStyles()

	// Initialize spinner
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styles.Highlight

	// Initialize search input
	ti := textinput.New()
	ti.Placeholder = "Search coffee..."
	ti.Prompt = ""
	ti.CharLimit = 100
	ti.SetWidth(30)

	// Initialize product list
	delegate := list.NewDefaultDelegate()

	productList := list.New([]list.Item{}, delegate, 0, 0)
	productList.SetShowTitle(false)
	productList.SetShowStatusBar(false)
	productList.SetShowPagination(false)
	productList.DisableQuitKeybindings()
	productList.SetShowHelp(false)
	productList.SetFilteringEnabled(false)
	productList.KeyMap.PrevPage.SetKeys()
	productList.KeyMap.PrevPage.SetHelp("pgup", "previous page")
	productList.KeyMap.NextPage.SetKeys()
	productList.KeyMap.NextPage.SetHelp("pgdown", "next page")
	productList.KeyMap.GoToStart.SetKeys("home")
	productList.KeyMap.GoToStart.SetHelp("home", "first coffee")
	productList.KeyMap.GoToEnd.SetKeys("end")
	productList.KeyMap.GoToEnd.SetHelp("end", "last coffee")

	m := Model{
		storeClient:      storeClient,
		productsCache:    productsCache,
		variationsCache:  variationsCache,
		viewState:        ViewProductList,
		styles:           styles,
		productList:      productList,
		searchInput:      ti,
		listSpinner:      sp,
		currentPage:      1,
		perPage:          20,
		localCart:        NewLocalCart(),
		customerInfo:     &CustomerInfo{Country: "IT"},
		loadingProducts:  true,
		selectedQuantity: 1,
		productDrafts:    make(map[int]productDraft),
		dark:             true,
		noColor:          os.Getenv("NO_COLOR") != "",
		profile:          colorprofile.TrueColor,
	}
	m.cartInput = textinput.New()
	m.cartInput.Placeholder = "Coupon code"
	m.cartInput.Prompt = ""
	m.applyTheme()
	return m
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	if m.shopper != nil {
		return tea.Batch(m.listSpinner.Tick, m.loadProducts(), tea.RequestBackgroundColor, m.waitShopper(), tea.Tick(time.Second, func(time.Time) tea.Msg { return catalogTickMsg{} }))
	}
	return tea.Batch(
		m.listSpinner.Tick,
		tea.RequestBackgroundColor,
		m.loadProducts(),
	)
}

func tickSplash(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(now time.Time) tea.Msg { return splashTickMsg(now) })
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	updated := next.(Model)
	observed := updated.observeAnalytics(msg)
	return updated, tea.Batch(cmd, observed)
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.sizeComponents()
		if m.splash && m.splashStarted.IsZero() && m.width > 0 && m.height > 0 {
			m.splashStarted = time.Now()
			cmds = append(cmds, tickSplash(splashInterval))
		}
	case splashTickMsg:
		if !m.splash || m.splashStarted.IsZero() {
			return m, nil
		}
		elapsed := time.Time(msg).Sub(m.splashStarted)
		if elapsed >= splashDuration {
			m.splash = false
			return m, nil
		}
		m.splashFrame = int(elapsed / splashInterval)
		return m, tickSplash(min(splashInterval, splashDuration-elapsed))
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.applyTheme()
	case tea.ColorProfileMsg:
		m.profile = msg.Profile
		m.applyTheme()
	case tea.KeyReleaseMsg:
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKeyMsg(msg)
	case catalogTickMsg:
		if m.catalog != nil {
			updated, _ := m.catalog.Status()
			if updated != m.catalogUpdated {
				cmds = append(cmds, m.loadProducts())
			}
			cmds = append(cmds, tea.Tick(time.Second, func(time.Time) tea.Msg { return catalogTickMsg{} }))
		}
	case checkoutSubmittedMsg:
		m.creatingOrder, m.err = false, nil
		m.viewState = ViewOrderConfirmation
		m.applyShopper(m.shopper.Snapshot())
	case storefrontMsg:
		m.applyShopper(msg.state)
		cmds = append(cmds, m.waitShopper())
		if m.paymentActive() && !m.polling {
			m.polling = true
			cmds = append(cmds, m.pollPayment())
		}
	case paymentPollMsg:
		m.pollStep++
		m.polling = false
		if m.paymentActive() {
			m.polling = true
			cmds = append(cmds, m.pollPayment())
		}
	case paymentActionMsg:
		m.cancelling, m.checking, m.confirmCancel = false, false, false
		m.err = msg.err
		m.analyticsError("payment", msg.err)
		m.applyShopper(m.shopper.Snapshot())
		if msg.err == nil {
			m.notice = "Payment status checked"
			if a := m.sessionState.Attempt; a != nil {
				if a.PaymentState == "paid" {
					m.notice = "Payment confirmed"
				}
				if a.PaymentState == "cancelled" {
					m.notice = "Payment cancelled · cart restored"
				}
			}
		}
	case cartSyncMsg:
		m.err = msg.err
		m.analyticsError("cart", msg.err)
		m.applyShopper(m.shopper.Snapshot())
	case couponCompleteMsg:
		m.couponBusy = false
		m.err = msg.err
		m.analyticsError("coupon", msg.err)
		m.applyShopper(m.shopper.Snapshot())
		if msg.err == nil {
			m.couponMode = ""
			m.cartInput.Blur()
			m.notice = "Coupon updated"
		}
	case preparedMsg:
		m.preparing = false
		m.err = msg.err
		stage := "checkout"
		if msg.fromAddress {
			stage = "address"
		}
		m.analyticsError(stage, msg.err)
		m.applyShopper(m.shopper.Snapshot())
		if msg.err == nil {
			m.quote = msg.quote
			if msg.fromAddress && m.hasDeliveryChoice() {
				m.viewState = ViewReview
				cmds = append(cmds, m.initShipping())
			} else if m.viewState == ViewAddress || m.viewState == ViewReview {
				m.viewState = ViewReview
			}
		} else if msg.fromAddress {
			// Reopen with the entered values, retaining the backend validation message.
			savedErr := m.err
			cmds = append(cmds, m.initAddressForm())
			m.err = savedErr
		}
	case shippingCompleteMsg:
		m.shippingBusy = false
		m.err = msg.err
		m.analyticsError("shipping", msg.err)
		m.applyShopper(m.shopper.Snapshot())
		if msg.err == nil {
			m.shippingForm = nil
			m.viewState = m.shippingReturn
			if m.viewState == ViewReview {
				m.preparing = true
				cmds = append(cmds, m.prepareQuote())
			}
		} else {
			savedErr := m.err
			cmds = append(cmds, m.initShipping())
			m.err = savedErr
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.listSpinner, cmd = m.listSpinner.Update(msg)
		cmds = append(cmds, cmd)
	case productsLoadedMsg:
		if m.catalog != nil {
			m.catalogUpdated, _ = m.catalog.Status()
		}
		m.catalogErr, m.loadingProducts = nil, false
		m.products = msg.products
		cmd := m.updateProductList()
		if cmd == nil {
			cmd = m.selectShopProduct(true)
		}
		cmds = append(cmds, cmd)
	case variationsLoadedMsg:
		if m.selectedProduct != nil && m.selectedProduct.ID == msg.productID && m.variationRequest == msg.requestID {
			m.loadingVariations = false
			m.productVariations = msg.variations
			m.optionsErr = nil
			m.restoreProductDraft()
		}
	case variationsErrorMsg:
		if m.selectedProduct != nil && m.selectedProduct.ID == msg.productID && m.variationRequest == msg.requestID {
			m.loadingVariations = false
			m.optionsErr = msg.err
		}
	case catalogErrorMsg:
		m.catalogErr, m.loadingProducts = msg.err, false
	case errMsg:
		m.err = msg.err
		m.analyticsError("checkout", msg.err)
		m.creatingOrder = false
		if m.shopper != nil {
			m.applyShopper(m.shopper.Snapshot())
			// Recovery owns checkout errors also stored on the durable session.
			if msg.err != nil && m.sessionState.Error == msg.err.Error() {
				m.err = nil
			}
			if quote, err := storefront.QuoteFromCart(m.sessionState.Cart); err == nil {
				m.quote = quote
			}
		}
	}
	// Background state never edits an input. Only component messages are forwarded.
	switch msg.(type) {
	case tea.WindowSizeMsg, tea.FocusMsg, tea.BlurMsg, tea.PasteMsg:
		updated, cmd := m.updateActiveComponent(msg)
		return updated, tea.Batch(append(cmds, cmd)...)
	default:
		// huh navigation and cursor blink messages are private component messages.
		switch msg.(type) {
		case analyticsTickMsg, cartSyncMsg, catalogTickMsg, storefrontMsg, paymentPollMsg, paymentActionMsg, couponCompleteMsg, preparedMsg, shippingCompleteMsg, productsLoadedMsg, variationsLoadedMsg, variationsErrorMsg, errMsg, catalogErrorMsg, spinner.TickMsg, checkoutSubmittedMsg, tea.BackgroundColorMsg, tea.ColorProfileMsg:
		default:
			updated, cmd := m.updateActiveComponent(msg)
			return updated, tea.Batch(append(cmds, cmd)...)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m Model) updateActiveComponent(msg tea.Msg) (Model, tea.Cmd) {
	if m.splash || m.showHelp || m.tooSmall() {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.PasteMsg, tea.FocusMsg, tea.BlurMsg:
			return m, nil
		}
	}
	var cmd tea.Cmd
	switch {
	case m.viewState == ViewCart && m.couponMode != "" && !m.couponBusy:
		m.cartInput, cmd = m.cartInput.Update(msg)
	case m.viewState == ViewProductList && m.showSearch:
		before := m.searchInput.Value()
		m.searchInput, cmd = m.searchInput.Update(msg)
		if before != m.searchInput.Value() {
			cmd = tea.Batch(cmd, m.updateProductList())
		}
	case m.viewState == ViewProductList:
		m.productList, cmd = m.productList.Update(msg)
		cmd = tea.Batch(cmd, m.selectShopProduct(false))
	case m.viewState == ViewAddress && m.addressForm != nil && !m.preparing:
		form, next := m.addressForm.Update(msg)
		m.addressForm, cmd = form.(*huh.Form), next
		if m.addressForm.State == huh.StateCompleted {
			if m.shopper == nil {
				m.viewState = ViewReview
			} else {
				m.preparing, m.err = true, nil
				cmd = m.prepareReview()
			}
		}
	case m.viewState == ViewShipping && m.shippingForm != nil && !m.shippingBusy:
		return m.updateShipping(msg)
	}
	return m, cmd
}

func (m Model) handleKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.splash {
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}
	for _, binding := range m.bindings() {
		for _, key := range binding.keys {
			if key == msg.String() {
				return m.performAction(binding.action, msg)
			}
		}
	}
	if m.showHelp || m.tooSmall() || m.confirmCancel {
		return m, nil
	}
	if m.inputActive() || m.viewState == ViewProductList {
		return m.updateActiveComponent(msg)
	}
	return m, nil
}

func (m Model) performAction(action string, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch action {
	case "quit":
		return m, tea.Quit
	case "help":
		m.showHelp = !m.showHelp
		m.helpScroll = 0
	case "cart":
		m.viewState = ViewCart
		m.err = nil
	case "browse":
		m.viewState = ViewProductList
		m.err = nil
	case "back":
		m.err, m.notice = nil, ""
		switch m.viewState {
		case ViewCart, ViewOrderConfirmation:
			m.viewState = ViewProductList
		case ViewAddress:
			m.viewState = ViewCart
			m.addressForm = nil
		case ViewReview:
			m.viewState = ViewAddress
			return m, m.initAddressForm()
		case ViewShipping:
			m.viewState = m.shippingReturn
			m.shippingForm = nil
		}
	case "search":
		m.showSearch = true
		return m, m.searchInput.Focus()
	case "searchDone":
		m.trackSearch()
		m.showSearch = false
		m.searchInput.Blur()
	case "searchClear":
		m.showSearch = false
		m.searchInput.Blur()
		m.searchInput.SetValue("")
		return m, m.updateProductList()
	case "filter":
		m.inStockOnly = !m.inStockOnly
		m.event("filter_changed", map[string]any{"in_stock_only": m.inStockOnly})
		return m, m.updateProductList()
	case "sync":
		m.err = nil
		return m, func() tea.Msg { return cartSyncMsg{m.shopper.Sync(m.ctx)} }
	case "refresh":
		m.loadingProducts = true
		if m.catalog != nil {
			return m, m.refreshCatalog()
		}
		return m, m.loadProducts()
	case "optionNext", "optionPrevious":
		m.cycleShopFocus(action == "optionPrevious")
	case "optionLeft", "optionRight":
		delta := 1
		if action == "optionLeft" {
			delta = -1
		}
		m.changeShopOption(delta)
	case "draftIncrease", "draftDecrease":
		delta := 1
		if action == "draftDecrease" {
			delta = -1
		}
		m.changeDraftQuantity(delta)
	case "add":
		m.err = m.addToCart()
		m.analyticsError("cart", m.err)
		if m.err == nil {
			m.viewState = ViewCart
			m.notice = "Added to cart: " + m.selectedProduct.Name
		}
	case "component":
		return m.updateActiveComponent(msg)
	case "up":
		m.localCart.MoveUp()
		m.revealCartSelection()
	case "down":
		m.localCart.MoveDown()
		m.revealCartSelection()
	case "increase", "decrease", "delete":
		if item := m.localCart.GetSelectedItem(); item != nil {
			if m.shopper != nil {
				id := item.ProductID
				if item.VariationID > 0 {
					id = item.VariationID
				}
				if action == "delete" {
					m.err = m.shopper.SetQuantity(id, item.GrindSize, 0, m.analytics.connection)
				} else {
					delta := 1
					if action == "decrease" {
						delta = -1
					}
					m.err = m.shopper.AdjustQuantity(id, item.GrindSize, delta, m.analytics.connection)
				}
				m.applyShopper(m.shopper.Snapshot())
				m.analyticsError("cart", m.err)
			} else {
				if action == "delete" {
					m.localCart.RemoveItem(m.localCart.SelectedIdx)
				} else {
					delta := 1
					if action == "decrease" {
						delta = -1
					}
					m.localCart.UpdateQuantity(m.localCart.SelectedIdx, max(1, item.Quantity+delta))
				}
			}
		}
	case "checkout":
		if m.shopper != nil {
			m.applyShopper(m.shopper.Snapshot())
		}
		if m.checkoutReason() != "" {
			return m, nil
		}
		m.event("begin_checkout", map[string]any{"item_count": m.localCart.ItemCount()})
		m.viewState = ViewAddress
		return m, m.initAddressForm()
	case "confirm":
		m.applyShopper(m.shopper.Snapshot())
		if reason := m.checkoutReason(); reason != "" {
			m.err = fmt.Errorf("%s", reason)
			return m, nil
		}
		current, err := storefront.QuoteFromCart(m.sessionState.Cart)
		if err != nil {
			m.err = err
			return m, nil
		}
		if current != m.quote {
			m.quote = current
			m.err = fmt.Errorf("quote changed; review the updated total and confirm again")
			return m, nil
		}
		m.err, m.creatingOrder = nil, true
		return m, m.submitCheckout()
	case "retryReview":
		m.preparing, m.err = true, nil
		return m, m.prepareQuote()
	case "shipping":
		return m, m.initShipping()
	case "couponApply", "couponRemove":
		m.couponMode = "apply"
		if action == "couponRemove" {
			m.couponMode = "remove"
		}
		m.cartInput.SetValue("")
		m.sizeComponents()
		return m, m.cartInput.Focus()
	case "couponBack":
		m.couponMode = ""
		m.cartInput.Blur()
	case "couponSubmit":
		code := strings.TrimSpace(m.cartInput.Value())
		if code == "" {
			m.err = fmt.Errorf("enter a coupon code")
			return m, nil
		}
		m.couponBusy, m.err = true, nil
		return m, m.submitCoupon(code, m.couponMode == "remove")
	case "payment":
		m.viewState = ViewOrderConfirmation
		m.err = nil
	case "copy":
		m.event("payment_link_copy_requested", nil)
		m.notice = "Copy requested"
		return m, tea.SetClipboard(m.sessionState.Attempt.PaymentURL)
	case "recheck":
		m.checking, m.err = true, nil
		return m, m.recheckPayment()
	case "cancelAsk":
		m.confirmCancel = true
		m.viewState = ViewOrderConfirmation
	case "cancelBack":
		m.confirmCancel = false
	case "cancelConfirm":
		m.confirmCancel, m.cancelling, m.err = false, true, nil
		return m, m.cancelPayment()
	case "scrollUp", "scrollDown", "pageUp", "pageDown", "home", "end":
		m.moveScroll(action)
	}
	return m, nil
}

func (m *Model) addToCart() error {
	if m.selectedProduct == nil {
		return fmt.Errorf("select a product")
	}
	if reason := m.purchaseReason(); reason != "Available to purchase" {
		return fmt.Errorf("%s", reason)
	}
	item := NewLocalCartItemFromProduct(m.selectedProduct, m.selectedVariation, m.selectedQuantity, m.selectedGrindSize)
	if m.shopper != nil {
		id := item.ProductID
		if item.VariationID > 0 {
			id = item.VariationID
		}
		if err := m.shopper.Add(storefront.Intent{ID: id, ProductID: item.ProductID, VariationID: item.VariationID, Name: item.Name, Grind: item.GrindSize, Quantity: item.Quantity}, m.analytics.connection); err != nil {
			return err
		}
		m.applyShopper(m.shopper.Snapshot())
		return nil
	}
	m.localCart.AddItem(item)
	return nil
}

func requiredField(name string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
}

func (m *Model) initAddressForm() tea.Cmd {
	m.err = nil
	m.addressDraft = true
	if m.customerInfo.Country == "" {
		m.customerInfo.Country = "IT"
	}
	info := m.customerInfo
	fields := []huh.Field{
		huh.NewInput().Title("First name *").Value(&info.FirstName).Validate(requiredField("first name")),
		huh.NewInput().Title("Last name *").Value(&info.LastName).Validate(requiredField("last name")),
		huh.NewInput().Title("Email *").Value(&info.Email).Validate(func(s string) error {
			parsed, err := mail.ParseAddress(strings.TrimSpace(s))
			if err != nil || parsed.Address != strings.TrimSpace(s) || !strings.Contains(parsed.Address, ".") {
				return fmt.Errorf("enter a valid email address")
			}
			return nil
		}),
		huh.NewInput().Title("Street address *").Value(&info.Address).Validate(requiredField("street address")),
		huh.NewInput().Title("City *").Value(&info.City).Validate(requiredField("city")),
		huh.NewInput().Title("Country * (2-letter code; Italy = IT)").Value(&info.Country).Validate(func(s string) error {
			s = strings.ToUpper(strings.TrimSpace(s))
			if len(s) != 2 || s[0] < 'A' || s[0] > 'Z' || s[1] < 'A' || s[1] > 'Z' {
				return fmt.Errorf("use a 2-letter country code")
			}
			return nil
		}),
		huh.NewInput().Title("Postcode *").Value(&info.Postcode).Validate(func(s string) error {
			if err := requiredField("postcode")(s); err != nil {
				return err
			}
			if strings.EqualFold(strings.TrimSpace(info.Country), "IT") {
				if len(s) != 5 {
					return fmt.Errorf("Italian postcodes need 5 digits")
				}
				for _, r := range s {
					if r < '0' || r > '9' {
						return fmt.Errorf("Italian postcodes need 5 digits")
					}
				}
			}
			return nil
		}),
		huh.NewInput().Title("State / province code").Value(&info.State),
		huh.NewInput().Title("Phone").Value(&info.Phone),
	}
	groups := make([]*huh.Group, 0, len(fields))
	for _, field := range fields {
		groups = append(groups, huh.NewGroup(field))
	}
	m.addressForm = huh.NewForm(groups...)
	m.styleForm(m.addressForm)
	return m.addressForm.Init()
}

func (m *Model) updateProductList() tea.Cmd {
	if item, ok := m.productList.SelectedItem().(productItem); ok {
		m.selectedID = item.product.ID
	}
	var items []list.Item
	selected := 0
	for _, p := range m.products {
		if m.inStockOnly && !p.IsInStock() {
			continue
		}
		if !strings.Contains(strings.ToLower(p.Name), strings.ToLower(m.searchInput.Value())) {
			continue
		}
		if p.ID == m.selectedID {
			selected = len(items)
		}
		items = append(items, productItem{product: p})
	}
	m.productList.SetItems(items)
	m.productList.Select(selected)
	m.sizeComponents()
	return m.selectShopProduct(false)
}

func (m Model) loadProducts() tea.Cmd {
	if m.catalog != nil {
		return func() tea.Msg {
			return productsLoadedMsg{products: mapStoreProductsToWoo(m.catalog.Products("", false))}
		}
	}
	return func() tea.Msg {
		cacheKey := ProductListCacheKey{
			Page:        m.currentPage,
			PerPage:     m.perPage,
			Search:      "",
			InStockOnly: false,
		}

		// Check cache first
		if products, ok := m.productsCache.Get(cacheKey); ok {
			return productsLoadedMsg{products: products}
		}

		storeProducts, err := m.storeClient.GetProducts(context.Background(), storeapi.ProductQuery{
			Page:    m.currentPage,
			PerPage: m.perPage,
			Search:  "",
			InStock: false,
		})
		if err != nil {
			return catalogErrorMsg{err: err}
		}
		products := mapStoreProductsToWoo(storeProducts)

		// Cache the result
		m.productsCache.Set(cacheKey, products)

		return productsLoadedMsg{products: products}
	}
}

func (m *Model) loadVariations(productID int) tea.Cmd {
	m.variationRequest++
	requestID := m.variationRequest
	if m.catalog != nil {
		metadata := m.selectedProduct.VariationDetails
		return func() tea.Msg {
			return variationsLoadedMsg{productID: productID, requestID: requestID, variations: mapStoreVariationsToWoo(m.catalog.Variations(productID), metadata)}
		}
	}
	metadata := m.selectedProduct.VariationDetails
	return func() tea.Msg {
		// Check cache first
		if variations, ok := m.variationsCache.Get(productID); ok {
			return variationsLoadedMsg{productID: productID, requestID: requestID, variations: variations}
		}

		storeVariations, err := m.storeClient.GetProductVariations(context.Background(), productID)
		if err != nil {
			return variationsErrorMsg{productID: productID, requestID: requestID, err: err}
		}
		variations := mapStoreVariationsToWoo(storeVariations, metadata)

		// Cache the result
		m.variationsCache.Set(productID, variations)

		return variationsLoadedMsg{productID: productID, requestID: requestID, variations: variations}
	}
}

func mapStoreProductsToWoo(products []storeapi.Product) []woo.Product {
	result := make([]woo.Product, 0, len(products))
	for _, p := range products {
		attrs := make([]woo.Attribute, 0, len(p.Attributes))
		for _, attr := range p.Attributes {
			options := make([]string, 0, len(attr.Terms))
			for _, term := range attr.Terms {
				options = append(options, term.Name)
			}
			attrs = append(attrs, woo.Attribute{
				ID:        attr.ID,
				Name:      attr.Name,
				Variation: attr.HasVariations,
				Options:   options,
			})
		}

		var ext struct {
			Grinds []string `json:"grinds"`
		}
		if json.Unmarshal(p.Extensions["eva_terminal"], &ext) == nil && len(ext.Grinds) > 0 {
			kept := attrs[:0]
			for _, a := range attrs {
				if a.Name != "Grind Size" {
					kept = append(kept, a)
				}
			}
			attrs = append(kept, woo.Attribute{Name: "Grind Size", Options: ext.Grinds})
		}
		ids := make([]int, 0, len(p.Variations))
		metadata := make([]woo.Variation, 0, len(p.Variations))
		for _, v := range p.Variations {
			ids = append(ids, v.ID)
			va := make([]woo.VariationAttribute, 0, len(v.Attributes))
			for _, attr := range v.Attributes {
				value := attr.Value
				for _, parentAttr := range p.Attributes {
					if parentAttr.Name == attr.Name {
						for _, term := range parentAttr.Terms {
							if term.Slug == value {
								value = term.Name
								break
							}
						}
					}
				}
				va = append(va, woo.VariationAttribute{Name: attr.Name, Option: value})
			}
			metadata = append(metadata, woo.Variation{ID: v.ID, Attributes: va})
		}

		result = append(result, woo.Product{
			ID:               p.ID,
			Name:             p.Name,
			Type:             p.Type,
			Status:           "publish",
			Description:      p.Description,
			ShortDescription: p.ShortDesc,
			Price:            p.Prices.DisplayPrice(),
			RegularPrice:     storeapi.FormatMinor(p.Prices.RegularPrice, p.Prices.MinorUnit()),
			SalePrice:        storeapi.FormatMinor(p.Prices.SalePrice, p.Prices.MinorUnit()),
			OnSale:           p.OnSale,
			StockStatus:      storeStock(p),
			CurrencyCode:     p.Prices.CurrencyCode, CurrencyMinorUnit: p.Prices.MinorUnit(), Purchasable: &p.IsPurchasable, PriceRange: storeRange(p.Prices),
			Attributes:       attrs,
			Variations:       ids,
			VariationDetails: metadata,
		})
	}
	return result
}

func mapStoreVariationsToWoo(products []storeapi.Product, metadata []woo.Variation) []woo.Variation {
	result := make([]woo.Variation, 0, len(products))
	for _, p := range products {
		var attrs []woo.VariationAttribute
		for _, v := range metadata {
			if v.ID == p.ID {
				attrs = v.Attributes
				break
			}
		}
		if len(attrs) == 0 {
			continue
		}
		result = append(result, woo.Variation{
			ID:           p.ID,
			Attributes:   attrs,
			Price:        p.Prices.DisplayPrice(),
			RegularPrice: storeapi.FormatMinor(p.Prices.RegularPrice, p.Prices.MinorUnit()),
			SalePrice:    storeapi.FormatMinor(p.Prices.SalePrice, p.Prices.MinorUnit()),
			StockStatus:  storeStock(p), Purchasable: &p.IsPurchasable,
		})
	}
	return result
}

func minorRawToDisplay(v string) string {
	if v == "" {
		return ""
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return v
	}
	return storeapi.FormatMinor(strconv.Itoa(n), 2)
}

func formatMinorAmount(v string) string {
	if v == "" {
		return ""
	}
	return minorRawToDisplay(v)
}

// GetSelectedProduct returns the currently selected product (for testing).
func (m Model) GetSelectedProduct() *woo.Product {
	return m.selectedProduct
}

// GetViewState returns the current view state (for testing).
func (m Model) GetViewState() ViewState {
	return m.viewState
}
