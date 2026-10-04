package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/thomas/eva-terminal-go/internal/cache"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
	"github.com/thomas/eva-terminal-go/internal/storefront"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

// ViewState represents the current view in the application.
type ViewState int

const (
	ViewProductList ViewState = iota
	ViewProductDetails
	ViewConfigurator
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

	// Dependencies
	storeClient     *storeapi.Client
	productsCache   *cache.Cache[ProductListCacheKey, []woo.Product]
	variationsCache *cache.Cache[int, []woo.Variation]

	// View state
	viewState ViewState
	width     int
	height    int
	styles    Styles

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

	// Product details view
	selectedProduct   *woo.Product
	productVariations []woo.Variation
	loadingVariations bool

	// Configurator view
	selectedVariation *woo.Variation
	selectedGrindSize string
	configForm        *huh.Form
	configCompleted   bool

	// Local cart (per SSH session)
	localCart *LocalCart

	// Review/Checkout
	addressForm   *huh.Form
	customerInfo  *CustomerInfo
	creatingOrder bool

	// Order confirmation
	orderResponse *woo.OrderResponse

	// Error handling
	err error
}

// CustomerInfo holds customer information for checkout.
type CustomerInfo struct {
	FirstName        string
	LastName         string
	Email            string
	Address          string
	City             string
	Postcode         string
	State            string
	Phone            string
	Country          string
	AddressConfirmed bool
}

// productItem implements list.Item for products.
type productItem struct {
	product woo.Product
	styles  Styles
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
		typeLabel = " [Variable]"
	}
	return fmt.Sprintf("%s %s • %s%s", i.product.CurrencyCode, price, stock, typeLabel)
}

func (i productItem) FilterValue() string {
	return i.product.Name
}

// Messages
type (
	productsLoadedMsg struct {
		products []woo.Product
	}
	variationsLoadedMsg struct {
		variations []woo.Variation
	}
	orderCreatedMsg struct {
		order *woo.OrderResponse
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
	sp.Style = lipgloss.NewStyle().Foreground(colorCaramel)

	// Initialize search input
	ti := textinput.New()
	ti.Placeholder = "Search products..."
	ti.CharLimit = 50
	ti.SetWidth(30)

	// Initialize product list
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(colorHighlight).
		BorderLeftForeground(colorHighlight)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(colorMocha).
		BorderLeftForeground(colorHighlight)

	productList := list.New([]list.Item{}, delegate, 0, 0)
	productList.Title = "☕ Coffee Products"
	productList.SetShowHelp(false)
	productList.SetFilteringEnabled(true)
	productList.Styles.Title = styles.ListTitle

	return Model{
		storeClient:     storeClient,
		productsCache:   productsCache,
		variationsCache: variationsCache,
		viewState:       ViewProductList,
		styles:          styles,
		productList:     productList,
		searchInput:     ti,
		listSpinner:     sp,
		currentPage:     1,
		perPage:         20,
		localCart:       NewLocalCart(),
		customerInfo:    &CustomerInfo{},
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	if m.shopper != nil {
		return tea.Batch(m.listSpinner.Tick, m.loadProducts(), m.waitShopper(), tea.Tick(time.Second, func(time.Time) tea.Msg { return catalogTickMsg{} }))
	}
	return tea.Batch(
		m.listSpinner.Tick,
		m.loadProducts(),
	)
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.productList.SetSize(msg.Width-4, msg.Height-8)

	case tea.KeyReleaseMsg:
		return m, nil

	case catalogTickMsg:
		if m.catalog != nil {
			var reload tea.Cmd
			updated, _ := m.catalog.Status()
			if m.viewState == ViewProductList && updated != m.catalogUpdated {
				reload = m.loadProducts()
			}
			cmds = append(cmds, reload, tea.Tick(time.Second, func(time.Time) tea.Msg { return catalogTickMsg{} }))
		}
	case checkoutSubmittedMsg:
		m.creatingOrder = false
		m.viewState = ViewOrderConfirmation
		m.applyShopper(m.shopper.Snapshot())
	case storefrontMsg:
		m.applyShopper(msg.state)
		cmds = append(cmds, m.waitShopper())
		if m.sessionState.Attempt != nil && m.sessionState.Attempt.Active() && !m.polling {
			m.polling = true
			cmds = append(cmds, m.pollPayment())
		}
	case paymentPollMsg:
		m.pollStep++
		m.polling = false
		if m.sessionState.Attempt != nil && m.sessionState.Attempt.Active() {
			m.polling = true
			cmds = append(cmds, m.pollPayment())
		}
	case preparedMsg:
		m.preparing = false
		m.err = msg.err
		if msg.err == nil {
			m.quote = msg.quote
		}
	case shippingCompleteMsg:
		m.shippingForm = nil
		m.viewState = ViewCart
	case tea.KeyPressMsg:
		return m.handleKeyMsg(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.listSpinner, cmd = m.listSpinner.Update(msg)
		cmds = append(cmds, cmd)

	case productsLoadedMsg:
		if m.catalog != nil {
			m.catalogUpdated, _ = m.catalog.Status()
		}
		if m.viewState == ViewProductList {
			m.err = nil
		}
		m.loadingProducts = false
		m.products = msg.products
		m.updateProductList()

	case variationsLoadedMsg:
		m.loadingVariations = false
		m.productVariations = msg.variations

	case orderCreatedMsg:
		m.creatingOrder = false
		m.orderResponse = msg.order
		m.viewState = ViewOrderConfirmation
		m.localCart.Clear()

	case errMsg:
		m.err = msg.err
		m.loadingProducts = false
		m.loadingVariations = false
		m.creatingOrder = false
	}

	updated, cmd := m.updateActiveComponent(msg)
	cmds = append(cmds, cmd)
	return updated, tea.Batch(cmds...)
}

// All form messages, including asynchronous navigation, share completion handling.
func (m Model) updateActiveComponent(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.viewState {
	case ViewShipping:
		if m.shippingForm != nil {
			updated, next := m.updateShipping(msg)
			return updated.(Model), next
		}
	case ViewProductList:
		if m.showSearch {
			m.searchInput, cmd = m.searchInput.Update(msg)
		} else {
			m.productList, cmd = m.productList.Update(msg)
		}
	case ViewConfigurator:
		if m.configForm != nil && !m.configCompleted {
			form, next := m.configForm.Update(msg)
			m.configForm = form.(*huh.Form)
			cmd = next
			if m.configForm.State == huh.StateCompleted {
				m.err = m.extractConfigFormValues()
				m.configCompleted = m.err == nil
			}
		}
	case ViewAddress:
		if m.addressForm != nil {
			form, next := m.addressForm.Update(msg)
			m.addressForm = form.(*huh.Form)
			cmd = next
			if m.addressForm.State == huh.StateCompleted {
				m.viewState = ViewReview
				if m.shopper != nil {
					m.preparing = true
					cmd = m.prepareReview()
				}
			}
		}
	}
	return m, cmd
}

func (m Model) handleKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global keys
	if key == "o" && m.shopper != nil && m.couponMode == "" && !m.showSearch && m.viewState != ViewAddress && m.viewState != ViewConfigurator && m.viewState != ViewShipping && m.sessionState.Attempt != nil && m.sessionState.Attempt.Active() {
		m.viewState = ViewOrderConfirmation
		return m, nil
	}
	switch key {
	case "ctrl+c", "q":
		if m.viewState == ViewProductList && !m.showSearch {
			return m, tea.Quit
		}
	}

	switch m.viewState {
	case ViewProductList:
		return m.handleProductListKeys(msg)
	case ViewProductDetails:
		return m.handleProductDetailsKeys(msg)
	case ViewConfigurator:
		return m.handleConfiguratorKeys(msg)
	case ViewCart:
		return m.handleCartKeys(msg)
	case ViewAddress:
		return m.handleAddressKeys(msg)
	case ViewReview:
		return m.handleReviewKeys(msg)
	case ViewShipping:
		return m.updateShipping(msg)
	case ViewOrderConfirmation:
		return m.handleOrderConfirmationKeys(msg)
	}

	return m, nil
}

func (m Model) handleProductListKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.showSearch {
		switch key {
		case "enter":
			m.showSearch = false
			m.searchInput.Blur()
			return m, m.loadProducts()
		case "esc":
			m.showSearch = false
			m.searchInput.Blur()
			m.searchInput.SetValue("")
			return m, nil
		}
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}

	switch key {
	case "/":
		m.showSearch = true
		cmd := m.searchInput.Focus()
		return m, cmd

	case "f":
		m.inStockOnly = !m.inStockOnly
		return m, m.loadProducts()

	case "r":
		if m.catalog != nil {
			return m, m.refreshCatalog()
		}
		return m, m.loadProducts()

	case "c":
		m.viewState = ViewCart
		m.localCart.SelectedIdx = 0
		return m, nil

	case "enter":
		if item, ok := m.productList.SelectedItem().(productItem); ok {
			m.selectedProduct = &item.product
			m.viewState = ViewProductDetails
			m.configCompleted = false
			m.selectedVariation = nil
			m.selectedGrindSize = ""
			m.configForm = nil
			m.productVariations = nil
			m.err = nil

			if m.selectedProduct.IsVariable() {
				m.loadingVariations = true
				return m, m.loadVariations(m.selectedProduct.ID)
			}
		}
	}

	var cmd tea.Cmd
	m.productList, cmd = m.productList.Update(msg)
	return m, cmd
}

func (m Model) handleProductDetailsKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc", "backspace":
		m.viewState = ViewProductList
		m.selectedProduct = nil
		m.productVariations = nil
		return m, nil

	case "c", "enter":
		if m.selectedProduct != nil {
			if m.shopper != nil && (!m.selectedProduct.IsInStock() || (m.selectedProduct.Purchasable != nil && !*m.selectedProduct.Purchasable)) {
				m.err = fmt.Errorf("this product cannot be purchased")
				return m, nil
			}
			if m.selectedProduct.IsVariable() && len(m.productVariations) > 0 {
				m.viewState = ViewConfigurator
				cmd := m.initConfigurator()
				return m, cmd
			} else if !m.selectedProduct.IsVariable() {
				if attr := m.selectedProduct.GetAttribute("Grind Size"); attr != nil && len(attr.Options) > 0 {
					m.viewState = ViewConfigurator
					cmd := m.initSimpleConfigurator()
					return m, cmd
				}
				m.err = m.addToCart()
				if m.err == nil {
					m.viewState = ViewCart
				}
				return m, nil
			}
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleConfiguratorKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.viewState = ViewProductDetails
		m.configForm = nil
		m.configCompleted = false
		return m, nil

	case "a":
		// Add to cart if configuration is complete
		if m.configCompleted && m.selectedProduct != nil {
			m.err = m.addToCart()
			if m.err == nil {
				m.viewState = ViewCart
			}
			return m, nil
		}
	}

	return m.updateActiveComponent(msg)
}

func (m Model) handleCartKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.shopper != nil {
		return m.handleSyncedCartKeys(msg)
	}
	key := msg.String()

	switch key {
	case "esc", "backspace":
		m.viewState = ViewProductList
		return m, nil

	case "up", "k":
		m.localCart.MoveUp()
		return m, nil

	case "down", "j":
		m.localCart.MoveDown()
		return m, nil

	case "+", "=":
		if item := m.localCart.GetSelectedItem(); item != nil {
			m.localCart.UpdateQuantity(m.localCart.SelectedIdx, item.Quantity+1)
		}
		return m, nil

	case "-":
		if item := m.localCart.GetSelectedItem(); item != nil {
			if item.Quantity > 1 {
				m.localCart.UpdateQuantity(m.localCart.SelectedIdx, item.Quantity-1)
			}
		}
		return m, nil

	case "d", "delete":
		m.localCart.RemoveItem(m.localCart.SelectedIdx)
		return m, nil

	case "o":
		// Proceed to checkout - enter address
		if !m.localCart.IsEmpty() {
			m.viewState = ViewAddress
			cmd := m.initAddressForm()
			return m, cmd
		}
		return m, nil

	case "s":
		// Continue shopping
		m.viewState = ViewProductList
		return m, nil
	}

	return m, nil
}

func (m Model) handleAddressKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.viewState = ViewCart
		m.addressForm = nil
		return m, nil
	}

	return m.updateActiveComponent(msg)
}

func (m Model) handleReviewKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.shopper != nil {
		if msg.String() == "h" {
			return m, m.initShipping()
		}
		if msg.String() == "c" {
			m.viewState = ViewCart
			return m, nil
		}
		if msg.String() == "enter" || msg.String() == "p" {
			if m.preparing || m.creatingOrder {
				return m, nil
			}
			current, err := storefront.QuoteFromCart(m.shopper.Snapshot().Cart)
			if err != nil {
				m.err = err
				return m, nil
			}
			if current != m.quote {
				m.quote = current
				m.err = fmt.Errorf("quote changed; review the updated total and confirm again")
				return m, nil
			}
		}
	}
	key := msg.String()

	switch key {
	case "esc":
		m.viewState = ViewAddress
		cmd := m.initAddressForm()
		return m, cmd

	case "enter", "p":
		// Create order using WooCommerce v3 API
		if !m.creatingOrder && !m.preparing && !m.localCart.IsEmpty() {
			m.creatingOrder = true
			return m, m.createOrder()
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleOrderConfirmationKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.shopper != nil {
		if msg.String() == "x" {
			return m, m.cancelPayment()
		}
		m.viewState = ViewProductList
		return m, nil
	}
	key := msg.String()

	switch key {
	case "enter", "esc", "q":
		m.viewState = ViewProductList
		m.orderResponse = nil
		m.localCart.Clear()
		m.err = nil
		return m, nil
	}

	return m, nil
}

func (m *Model) extractConfigFormValues() error {
	m.selectedVariation = nil
	m.selectedGrindSize = ""
	if m.configForm == nil || m.selectedProduct == nil {
		return fmt.Errorf("configuration is missing")
	}
	if m.selectedProduct.IsVariable() {
		id := m.configForm.GetInt("variation_id")
		for i := range m.productVariations {
			if id > 0 && m.productVariations[i].ID == id {
				m.selectedVariation = &m.productVariations[i]
				break
			}
		}
		if m.selectedVariation == nil {
			return fmt.Errorf("select a valid size")
		}
	}
	m.selectedGrindSize = m.configForm.GetString("grind")
	return nil
}

func (m *Model) addToCart() error {
	if m.selectedProduct == nil {
		return fmt.Errorf("select a product")
	}
	if m.selectedProduct.IsVariable() {
		if err := m.extractConfigFormValues(); err != nil {
			return err
		}
	}
	item := NewLocalCartItemFromProduct(m.selectedProduct, m.selectedVariation, 1, m.selectedGrindSize)
	if m.shopper != nil {
		id := item.ProductID
		if item.VariationID > 0 {
			id = item.VariationID
		}
		if err := m.shopper.Add(storefront.Intent{ID: id, Name: item.Name, Grind: item.GrindSize, Quantity: 1}); err != nil {
			return err
		}
		m.applyShopper(m.shopper.Snapshot())
		return nil
	}
	m.localCart.AddItem(item)
	return nil
}

func (m *Model) initAddressForm() tea.Cmd {
	m.err = nil
	m.customerInfo.AddressConfirmed = false
	m.addressForm = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("First Name").
				Value(&m.customerInfo.FirstName).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("first name is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("Last Name").
				Value(&m.customerInfo.LastName).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("last name is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("Email").
				Value(&m.customerInfo.Email).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("email is required")
					}
					if !strings.Contains(s, "@") {
						return fmt.Errorf("invalid email format")
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Street Address").
				Value(&m.customerInfo.Address),
			huh.NewInput().
				Title("City").
				Value(&m.customerInfo.City),
			huh.NewInput().
				Title("Postcode").
				Value(&m.customerInfo.Postcode),
			huh.NewInput().
				Title("Country (2-letter code)").
				Value(&m.customerInfo.Country).
				Placeholder("IT"),
			huh.NewInput().Title("State / province code").Value(&m.customerInfo.State),
			huh.NewInput().Title("Phone").Value(&m.customerInfo.Phone),
			huh.NewConfirm().
				Key("enter").
				Value(&m.customerInfo.AddressConfirmed).
				Title("Is this correct?").
				Validate(func(v bool) error {
					if !v {
						return fmt.Errorf("address is not correct")
					}
					return nil
				}).
				Affirmative("Yes").
				Negative("No"),
		),
	).WithShowHelp(true).WithShowErrors(true)
	return m.addressForm.Init()
}

// Order commands

func (m Model) createOrder() tea.Cmd {
	return m.createOrderWithStore()
}

func (m Model) createOrderWithStore() tea.Cmd {
	if m.shopper == nil {
		return func() tea.Msg { return errMsg{err: fmt.Errorf("durable shopper session is required for checkout")} }
	}
	return m.submitCheckout()
}

func (m *Model) updateProductList() {
	items := make([]list.Item, len(m.products))
	for i, p := range m.products {
		items[i] = productItem{product: p, styles: m.styles}
	}
	m.productList.SetItems(items)
}

func (m Model) loadProducts() tea.Cmd {
	if m.catalog != nil {
		return func() tea.Msg {
			return productsLoadedMsg{products: mapStoreProductsToWoo(m.catalog.Products(m.searchInput.Value(), m.inStockOnly))}
		}
	}
	m.loadingProducts = true

	return func() tea.Msg {
		cacheKey := ProductListCacheKey{
			Page:        m.currentPage,
			PerPage:     m.perPage,
			Search:      m.searchInput.Value(),
			InStockOnly: m.inStockOnly,
		}

		// Check cache first
		if products, ok := m.productsCache.Get(cacheKey); ok {
			return productsLoadedMsg{products: products}
		}

		storeProducts, err := m.storeClient.GetProducts(context.Background(), storeapi.ProductQuery{
			Page:    m.currentPage,
			PerPage: m.perPage,
			Search:  m.searchInput.Value(),
			InStock: m.inStockOnly,
		})
		if err != nil {
			return errMsg{err: err}
		}
		products := mapStoreProductsToWoo(storeProducts)

		// Cache the result
		m.productsCache.Set(cacheKey, products)

		return productsLoadedMsg{products: products}
	}
}

func (m Model) loadVariations(productID int) tea.Cmd {
	if m.catalog != nil {
		metadata := m.selectedProduct.VariationDetails
		return func() tea.Msg {
			return variationsLoadedMsg{variations: mapStoreVariationsToWoo(m.catalog.Variations(productID), metadata)}
		}
	}
	metadata := m.selectedProduct.VariationDetails
	return func() tea.Msg {
		// Check cache first
		if variations, ok := m.variationsCache.Get(productID); ok {
			return variationsLoadedMsg{variations: variations}
		}

		storeVariations, err := m.storeClient.GetProductVariations(context.Background(), productID)
		if err != nil {
			return errMsg{err: err}
		}
		variations := mapStoreVariationsToWoo(storeVariations, metadata)

		// Cache the result
		m.variationsCache.Set(productID, variations)

		return variationsLoadedMsg{variations: variations}
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

func (m *Model) initConfigurator() tea.Cmd {
	m.configCompleted = false
	m.selectedVariation = nil
	m.selectedGrindSize = ""
	m.configForm = nil
	m.err = nil
	if m.selectedProduct == nil || !m.selectedProduct.IsVariable() {
		return nil
	}
	var sizeOptions []huh.Option[int]
	for _, v := range m.productVariations {
		if m.shopper != nil && (!v.IsInStock() || (v.Purchasable != nil && !*v.Purchasable)) {
			continue
		}
		labels := []string{}
		for _, a := range v.Attributes {
			labels = append(labels, a.Name+": "+a.Option)
		}
		size := strings.Join(labels, ", ")
		if size != "" && v.ID > 0 {
			sizeOptions = append(sizeOptions, huh.NewOption(fmt.Sprintf("%s (%s %s)", size, m.selectedProduct.CurrencyCode, v.GetDisplayPrice()), v.ID))
		}
	}
	if len(sizeOptions) == 0 {
		m.err = fmt.Errorf("no valid sizes available")
		return nil
	}
	m.configForm = huh.NewForm(
		huh.NewGroup(huh.NewSelect[int]().Key("variation_id").Title("Select Size").Options(sizeOptions...)),
		huh.NewGroup(m.grindSelect()),
	).WithShowHelp(true).WithShowErrors(true)
	return m.configForm.Init()
}

func (m Model) grindSelect() *huh.Select[string] {
	options := []huh.Option[string]{}
	if attr := m.selectedProduct.GetAttribute("Grind Size"); attr != nil {
		for _, opt := range attr.Options {
			options = append(options, huh.NewOption(opt, opt))
		}
	}
	if len(options) == 0 {
		options = append(options, huh.NewOption("No grind option", ""))
	}
	return huh.NewSelect[string]().Key("grind").Title("Select Grind Size").Options(options...)
}

func (m *Model) initSimpleConfigurator() tea.Cmd {
	m.configCompleted = false
	m.selectedVariation = nil
	m.selectedGrindSize = ""
	m.err = nil
	m.configForm = huh.NewForm(huh.NewGroup(m.grindSelect())).WithShowHelp(true).WithShowErrors(true)
	return m.configForm.Init()
}

// View renders the current view.
func (m Model) View() tea.View {
	v := tea.NewView("Loading...")
	v.AltScreen = true
	v.ReportFocus = true
	if m.width == 0 {
		return v
	}

	var content string

	switch m.viewState {
	case ViewProductList:
		content = m.viewProductList()
	case ViewProductDetails:
		content = m.viewProductDetails()
	case ViewConfigurator:
		content = m.viewConfigurator()
	case ViewCart:
		content = m.viewCart()
	case ViewAddress:
		content = m.viewAddress()
	case ViewReview:
		content = m.viewReview()
	case ViewShipping:
		if m.shippingForm != nil {
			content = m.shippingForm.View()
		}
	case ViewOrderConfirmation:
		content = m.viewOrderConfirmation()
	}

	v.SetContent(m.styles.App.Render(content))
	return v
}

func (m Model) viewProductList() string {
	var sb strings.Builder

	// Header
	header := m.styles.HeaderTitle.Render("☕ WooCommerce Coffee Browser")
	if m.inStockOnly {
		header += m.styles.Highlight.Render(" [In Stock Only]")
	}
	sb.WriteString(m.styles.Header.Render(header))
	sb.WriteString("\n")

	// Search bar
	if m.showSearch {
		sb.WriteString("Search: ")
		sb.WriteString(m.searchInput.View())
		sb.WriteString("\n\n")
	}

	// Loading indicator or product list
	if m.loadingProducts {
		sb.WriteString(m.listSpinner.View())
		sb.WriteString(" Loading products...")
	} else {
		sb.WriteString(m.productList.View())
	}

	if m.catalog != nil {
		updated, err := m.catalog.Status()
		if updated.IsZero() {
			sb.WriteString("\nWaiting for first catalog snapshot")
		} else {
			sb.WriteString(fmt.Sprintf("\nCatalog updated %s ago", time.Since(updated).Round(time.Second)))
		}
		if err != nil {
			sb.WriteString(" (refresh failed; saved catalog)")
		}
	}
	if m.err != nil {
		sb.WriteString("\n" + m.err.Error())
	}
	// Help bar with cart info
	cartInfo := ""
	if m.localCart.ItemCount() > 0 {
		cartInfo = fmt.Sprintf(" • 🛒 %d items (%s)", m.localCart.ItemCount(), "Woo cart")
	}
	if m.sessionState.Attempt != nil && m.sessionState.Attempt.Active() {
		cartInfo += " • o pending payment"
	}
	help := "/ search • f filter in-stock • r refresh • enter select • c cart • q quit" + cartInfo
	sb.WriteString("\n")
	sb.WriteString(m.styles.HelpBar.Render(help))

	return sb.String()
}

func (m Model) viewProductDetails() string {
	if m.selectedProduct == nil {
		return "No product selected"
	}

	var sb strings.Builder
	p := m.selectedProduct

	// Product name
	sb.WriteString(m.styles.ProductName.Render(p.Name))
	sb.WriteString("\n\n")

	// Price
	price := p.GetDisplayPrice()
	if p.OnSale {
		sb.WriteString(m.styles.ProductSalePrice.Render(fmt.Sprintf("%s %s", p.CurrencyCode, price)))
		sb.WriteString(" ")
		sb.WriteString(m.styles.Subtle.Render(fmt.Sprintf("(was %s %s)", p.CurrencyCode, p.RegularPrice)))
	} else {
		sb.WriteString(m.styles.ProductPrice.Render(fmt.Sprintf("%s %s", p.CurrencyCode, price)))
	}
	sb.WriteString("\n")

	// Stock status
	if p.IsInStock() {
		sb.WriteString(m.styles.ProductInStock.Render("✓ In Stock"))
	} else {
		sb.WriteString(m.styles.ProductOutOfStock.Render("✗ Out of Stock"))
	}

	// Product type
	if p.IsVariable() {
		sb.WriteString("  ")
		sb.WriteString(m.styles.Highlight.Render("[Variable Product]"))
	}
	sb.WriteString("\n")

	// Description
	desc := StripHTML(p.Description)
	if desc != "" {
		sb.WriteString("\n")
		sb.WriteString(m.styles.ProductDescription.Render(desc))
		sb.WriteString("\n")
	}

	// Attributes
	if len(p.Attributes) > 0 {
		sb.WriteString("\n")
		sb.WriteString(m.styles.Subtle.Render("Available Options:"))
		sb.WriteString("\n")
		for _, attr := range p.Attributes {
			sb.WriteString(fmt.Sprintf("  • %s: %s\n", attr.Name, strings.Join(attr.Options, ", ")))
		}
	}

	// Variations info (if loading or loaded)
	if p.IsVariable() {
		sb.WriteString("\n")
		if m.loadingVariations {
			sb.WriteString(m.listSpinner.View())
			sb.WriteString(" Loading variations...")
		} else if len(m.productVariations) > 0 {
			sb.WriteString(m.styles.Subtle.Render(fmt.Sprintf("%d variations available", len(m.productVariations))))
		}
	}

	// Help bar
	sb.WriteString("\n\n")
	helpText := "esc/backspace back"
	if p.IsVariable() && len(m.productVariations) > 0 {
		helpText += " • c/enter configure"
	} else if grindAttr := p.GetAttribute("Grind Size"); grindAttr != nil {
		helpText += " • c/enter select grind"
	}
	sb.WriteString(m.styles.HelpBar.Render(helpText))

	return m.styles.Box.Render(sb.String())
}

func (m Model) viewConfigurator() string {
	if m.selectedProduct == nil {
		return "No product selected"
	}

	var sb strings.Builder

	// Title
	sb.WriteString(m.styles.ConfigTitle.Render(fmt.Sprintf("Configure: %s", m.selectedProduct.Name)))
	sb.WriteString("\n\n")

	if m.err != nil {
		sb.WriteString(m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err)))
		sb.WriteString("\n\n")
	}

	// Form
	if m.configForm != nil {
		sb.WriteString(m.configForm.View())
	}

	// Summary (if completed)
	if m.configCompleted {
		sb.WriteString("\n")
		sb.WriteString(m.styles.ConfigSummary.Render(m.renderConfigSummary()))
	}

	// Help bar
	sb.WriteString("\n\n")
	helpText := "esc back • enter/tab navigate • space select"
	if m.configCompleted {
		helpText += " • a add to cart"
	}
	sb.WriteString(m.styles.HelpBar.Render(helpText))

	return m.styles.Box.Render(sb.String())
}

func (m Model) renderConfigSummary() string {
	var sb strings.Builder
	sb.WriteString(m.styles.Success.Render("✓ Configuration Complete"))
	sb.WriteString("\n\n")

	if m.selectedProduct != nil {
		sb.WriteString(fmt.Sprintf("Product: %s\n", m.selectedProduct.Name))
	}

	if m.selectedVariation != nil {
		sb.WriteString(fmt.Sprintf("Variation ID: %d\n", m.selectedVariation.ID))
		sb.WriteString(fmt.Sprintf("Price: %s %s\n", m.selectedProduct.CurrencyCode, m.selectedVariation.GetDisplayPrice()))
	} else if m.selectedProduct != nil {
		sb.WriteString(fmt.Sprintf("Price: %s %s\n", m.selectedProduct.CurrencyCode, m.selectedProduct.GetDisplayPrice()))
	}

	if m.selectedGrindSize != "" {
		sb.WriteString(fmt.Sprintf("Grind: %s\n", m.selectedGrindSize))
	}

	return sb.String()
}

func (m Model) viewCart() string { return m.viewWooCart(false) }

func (m Model) viewAddress() string {
	var sb strings.Builder

	// Header with progress
	sb.WriteString(m.styles.HeaderTitle.Render("📦 Shipping Address"))
	sb.WriteString("  ")
	sb.WriteString(m.styles.Subtle.Render("Step 1 of 2"))
	sb.WriteString("\n\n")

	if m.err != nil {
		sb.WriteString(m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err)))
		sb.WriteString("\n\n")
	}

	// Address form
	if m.addressForm != nil {
		sb.WriteString(m.addressForm.View())
		sb.WriteString("\n")
		sb.WriteString(m.styles.HelpBar.Render("esc back • tab navigate • enter submit"))
	}

	return m.styles.Box.Render(sb.String())
}

func (m Model) viewReview() string            { return m.viewWooCart(true) }
func (m Model) viewOrderConfirmation() string { return m.viewPayment() }

// GetSelectedProduct returns the currently selected product (for testing).
func (m Model) GetSelectedProduct() *woo.Product {
	return m.selectedProduct
}

// GetViewState returns the current view state (for testing).
func (m Model) GetViewState() ViewState {
	return m.viewState
}

// GetConfigCompleted returns whether configuration is complete (for testing).
func (m Model) GetConfigCompleted() bool {
	return m.configCompleted
}
