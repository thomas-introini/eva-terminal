package tui

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/thomas/eva-terminal-go/internal/cache"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

// ViewState represents the current view in the application.
type ViewState int

const (
	ViewProductList ViewState = iota
	ViewProductDetails
	ViewConfigurator
	ViewCart
	ViewQuote // New: shows quote with shipping options
	ViewPayment
	ViewReview
	ViewOrderConfirmation
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
	// Dependencies
	wooClient       *woo.Client
	quoteClient     *woo.QuoteClient
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

	// Quote state
	loadingQuote        bool
	shippingSelectedIdx int

	// Payment view
	paymentGateways    []woo.PaymentGateway
	paymentSelectedIdx int
	loadingPayment     bool

	// Review/Checkout
	addressForm   *huh.Form
	customerInfo  *CustomerInfo
	creatingOrder bool

	// Order confirmation
	orderResponse *woo.CreateOrderResponse

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
	Country   string
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
	return fmt.Sprintf("$%s • %s%s", price, stock, typeLabel)
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
	// Quote API messages
	quoteCreatedMsg struct {
		quote *woo.QuoteResponse
	}
	couponValidatedMsg struct {
		result *woo.CouponValidateResponse
	}
	paymentGatewaysLoadedMsg struct {
		gateways []woo.PaymentGateway
	}
	orderCreatedMsg struct {
		order *woo.CreateOrderResponse
	}
	errMsg struct {
		err error
	}
)

// NewModel creates a new TUI model.
func NewModel(wooClient *woo.Client, productsCache *cache.Cache[ProductListCacheKey, []woo.Product], variationsCache *cache.Cache[int, []woo.Variation]) Model {
	styles := DefaultStyles()

	// Initialize spinner
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorCaramel)

	// Initialize search input
	ti := textinput.New()
	ti.Placeholder = "Search products..."
	ti.CharLimit = 50
	ti.Width = 30

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
		wooClient:       wooClient,
		quoteClient:     woo.NewQuoteClient(wooClient),
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
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.listSpinner, cmd = m.listSpinner.Update(msg)
		cmds = append(cmds, cmd)

	case productsLoadedMsg:
		m.loadingProducts = false
		m.products = msg.products
		m.updateProductList()

	case variationsLoadedMsg:
		m.loadingVariations = false
		m.productVariations = msg.variations
		if m.selectedProduct != nil && m.selectedProduct.IsVariable() {
			m.initConfigurator()
		}

	// Quote API messages
	case quoteCreatedMsg:
		m.loadingQuote = false
		m.localCart.SetQuote(msg.quote)
		// Auto-select first shipping rate if available
		if len(msg.quote.ShippingRates) > 0 {
			m.shippingSelectedIdx = 0
		}

	case couponValidatedMsg:
		// Handle coupon validation result
		if msg.result.Valid {
			m.localCart.AddCoupon(msg.result.Code)
		}

	case paymentGatewaysLoadedMsg:
		m.loadingPayment = false
		m.paymentGateways = msg.gateways

	case orderCreatedMsg:
		m.creatingOrder = false
		m.orderResponse = msg.order
		m.viewState = ViewOrderConfirmation
		m.localCart.Clear()

	case errMsg:
		m.err = msg.err
		m.loadingProducts = false
		m.loadingVariations = false
		m.loadingQuote = false
		m.loadingPayment = false
		m.creatingOrder = false
	}

	// Update sub-models based on view state
	switch m.viewState {
	case ViewProductList:
		if m.showSearch {
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			cmds = append(cmds, cmd)
		} else {
			var cmd tea.Cmd
			m.productList, cmd = m.productList.Update(msg)
			cmds = append(cmds, cmd)
		}

	case ViewConfigurator:
		if m.configForm != nil {
			form, cmd := m.configForm.Update(msg)
			if f, ok := form.(*huh.Form); ok {
				m.configForm = f
				if m.configForm.State == huh.StateCompleted {
					m.configCompleted = true
				}
			}
			cmds = append(cmds, cmd)
		}

	case ViewQuote:
		if m.addressForm != nil {
			form, cmd := m.addressForm.Update(msg)
			if f, ok := form.(*huh.Form); ok {
				m.addressForm = f
			}
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global keys
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
	case ViewQuote:
		return m.handleQuoteKeys(msg)
	case ViewPayment:
		return m.handlePaymentKeys(msg)
	case ViewReview:
		return m.handleReviewKeys(msg)
	case ViewOrderConfirmation:
		return m.handleOrderConfirmationKeys(msg)
	}

	return m, nil
}

func (m Model) handleProductListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		m.searchInput.Focus()
		return m, textinput.Blink

	case "f":
		m.inStockOnly = !m.inStockOnly
		return m, m.loadProducts()

	case "r":
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

			if m.selectedProduct.IsVariable() {
				m.loadingVariations = true
				return m, m.loadVariations(m.selectedProduct.ID)
			}
			// For simple products, go directly to configurator if grind options exist
			if attr := m.selectedProduct.GetAttribute("Grind Size"); attr != nil && len(attr.Options) > 0 {
				m.initSimpleConfigurator()
			}
		}
	}

	var cmd tea.Cmd
	m.productList, cmd = m.productList.Update(msg)
	return m, cmd
}

func (m Model) handleProductDetailsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc", "backspace":
		m.viewState = ViewProductList
		m.selectedProduct = nil
		m.productVariations = nil
		return m, nil

	case "c", "enter":
		if m.selectedProduct != nil {
			if m.selectedProduct.IsVariable() && len(m.productVariations) > 0 {
				m.initConfigurator()
				m.viewState = ViewConfigurator
			} else if attr := m.selectedProduct.GetAttribute("Grind Size"); attr != nil && len(attr.Options) > 0 {
				m.initSimpleConfigurator()
				m.viewState = ViewConfigurator
			}
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleConfiguratorKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			m.addToCart()
			m.viewState = ViewCart
			return m, nil
		}
	}

	// Let the form handle the key
	if m.configForm != nil {
		form, cmd := m.configForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.configForm = f
			if m.configForm.State == huh.StateCompleted {
				m.configCompleted = true
				// Extract selected values from form
				m.extractConfigFormValues()
			}
		}
		return m, cmd
	}

	return m, nil
}

func (m Model) handleCartKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		// Proceed to checkout - get a quote
		if !m.localCart.IsEmpty() {
			m.initAddressForm()
			m.addressForm.Init()
			m.viewState = ViewQuote
		}
		return m, nil

	case "s":
		// Continue shopping
		m.viewState = ViewProductList
		return m, nil
	}

	return m, nil
}

func (m Model) handleQuoteKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	log.Println("handleQuoteKeys", key)

	switch key {
	case "esc":
		m.viewState = ViewCart
		m.addressForm = nil
		return m, nil
	}

	// Handle address form
	if m.addressForm != nil && m.addressForm.State != huh.StateCompleted {
		form, cmd := m.addressForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.addressForm = f
			if m.customerInfo != nil && m.customerInfo.AddressConfirmed {
				m.loadingQuote = true
				m.customerInfo.AddressConfirmed = false
				return m, m.createQuote()
			}
		}
		return m, cmd
	}

	// Handle shipping rate selection (after quote is received)
	if m.localCart.HasQuote() {
		rates := m.localCart.Quote.ShippingRates
		switch key {
		case "up", "k":
			if m.shippingSelectedIdx > 0 {
				m.shippingSelectedIdx--
			}
			return m, nil

		case "down", "j":
			if m.shippingSelectedIdx < len(rates)-1 {
				m.shippingSelectedIdx++
			}
			return m, nil

		case "enter":
			// Select shipping rate
			if len(rates) > 0 && m.shippingSelectedIdx < len(rates) {
				m.localCart.SelectShippingRate(rates[m.shippingSelectedIdx].RateID)
			}
			return m, nil

		case "n":
			// Proceed to payment if shipping is selected (or not needed)
			if m.localCart.GetSelectedShippingRate() != nil || !m.localCart.Quote.NeedsShipping() {
				m.loadingPayment = true
				m.viewState = ViewPayment
				return m, m.loadPaymentGateways()
			}
			return m, nil
		}
	}

	return m, nil
}

func (m Model) handlePaymentKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.viewState = ViewQuote
		return m, nil

	case "up", "k":
		if m.paymentSelectedIdx > 0 {
			m.paymentSelectedIdx--
		}
		return m, nil

	case "down", "j":
		if m.paymentSelectedIdx < len(m.paymentGateways)-1 {
			m.paymentSelectedIdx++
		}
		return m, nil

	case "enter":
		if len(m.paymentGateways) > 0 {
			m.viewState = ViewReview
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleReviewKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.viewState = ViewPayment
		return m, nil

	case "enter", "p":
		// Create order from quote
		if !m.creatingOrder && len(m.paymentGateways) > 0 && m.localCart.HasQuote() {
			m.creatingOrder = true
			return m, m.createOrder()
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleOrderConfirmationKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m *Model) extractConfigFormValues() {
	if m.configForm == nil || m.selectedProduct == nil {
		return
	}

	// Get form values - this is a bit tricky with huh forms
	// The values are bound to the variables we passed in initConfigurator
	// For now, we'll re-extract from the form's groups

	// Find the selected variation based on size
	if m.selectedProduct.IsVariable() && len(m.productVariations) > 0 {
		// Try to find selected size from form
		for _, v := range m.productVariations {
			// Default to first variation if we can't determine selection
			if m.selectedVariation == nil {
				m.selectedVariation = &v
			}
		}
	}
}

func (m *Model) addToCart() {
	if m.selectedProduct == nil {
		return
	}

	// Build meta for grind size if specified
	meta := make(map[string]string)
	if m.selectedGrindSize != "" {
		meta["grind"] = m.selectedGrindSize
	}

	// Create local cart item
	item := NewLocalCartItemFromProduct(m.selectedProduct, m.selectedVariation, 1, meta)
	m.localCart.AddItem(item)
}

func (m *Model) initAddressForm() {
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
				Placeholder("US"),
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
}

// Quote API commands

func (m Model) createQuote() tea.Cmd {
	return func() tea.Msg {
		country := m.customerInfo.Country
		if country == "" {
			country = "US"
		}

		shippingAddress := woo.QuoteAddress{
			FirstName: m.customerInfo.FirstName,
			LastName:  m.customerInfo.LastName,
			Email:     m.customerInfo.Email,
			Address1:  m.customerInfo.Address,
			City:      m.customerInfo.City,
			Postcode:  m.customerInfo.Postcode,
			Country:   country,
		}

		req := m.localCart.ToQuoteRequest(shippingAddress)

		quote, err := m.quoteClient.CreateQuote(context.Background(), req)
		if err != nil {
			return errMsg{err: fmt.Errorf("creating quote: %w", err)}
		}
		return quoteCreatedMsg{quote: quote}
	}
}

func (m Model) loadPaymentGateways() tea.Cmd {
	return func() tea.Msg {
		// Use WooCommerce REST API for payment gateways
		var gateways []woo.PaymentGateway
		err := m.wooClient.GetPaymentGateways(context.Background(), &gateways)
		if err != nil {
			return errMsg{err: fmt.Errorf("loading payment methods: %w", err)}
		}

		// Filter to enabled gateways
		var enabled []woo.PaymentGateway
		for _, gw := range gateways {
			if gw.Enabled {
				enabled = append(enabled, gw)
			}
		}
		return paymentGatewaysLoadedMsg{gateways: enabled}
	}
}

func (m Model) createOrder() tea.Cmd {
	return func() tea.Msg {
		if m.localCart.Quote == nil {
			return errMsg{err: fmt.Errorf("no quote available")}
		}

		country := m.customerInfo.Country
		if country == "" {
			country = "US"
		}

		address := woo.QuoteAddress{
			FirstName: m.customerInfo.FirstName,
			LastName:  m.customerInfo.LastName,
			Email:     m.customerInfo.Email,
			Address1:  m.customerInfo.Address,
			City:      m.customerInfo.City,
			Postcode:  m.customerInfo.Postcode,
			Country:   country,
		}

		paymentMethod := "cod"
		if len(m.paymentGateways) > 0 && m.paymentSelectedIdx < len(m.paymentGateways) {
			paymentMethod = m.paymentGateways[m.paymentSelectedIdx].ID
		}

		req := woo.CreateOrderRequest{
			QuoteID:         m.localCart.Quote.QuoteID,
			IdempotencyKey:  fmt.Sprintf("order_%s_%d", m.localCart.Quote.QuoteID, m.paymentSelectedIdx),
			ShippingRateID:  m.localCart.SelectedShippingRateID,
			BillingAddress:  address,
			ShippingAddress: address,
			CustomerEmail:   m.customerInfo.Email,
			PaymentMethod:   paymentMethod,
		}

		order, err := m.quoteClient.CreateOrder(context.Background(), req)
		if err != nil {
			return errMsg{err: fmt.Errorf("creating order: %w", err)}
		}
		return orderCreatedMsg{order: order}
	}
}

func (m *Model) updateProductList() {
	items := make([]list.Item, len(m.products))
	for i, p := range m.products {
		items[i] = productItem{product: p, styles: m.styles}
	}
	m.productList.SetItems(items)
}

func (m Model) loadProducts() tea.Cmd {
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

		// Fetch from API
		params := woo.GetProductsParams{
			Page:        m.currentPage,
			PerPage:     m.perPage,
			Search:      m.searchInput.Value(),
			InStockOnly: m.inStockOnly,
		}

		products, err := m.wooClient.GetProducts(context.Background(), params)
		if err != nil {
			return errMsg{err: err}
		}

		// Cache the result
		m.productsCache.Set(cacheKey, products)

		return productsLoadedMsg{products: products}
	}
}

func (m Model) loadVariations(productID int) tea.Cmd {
	return func() tea.Msg {
		// Check cache first
		if variations, ok := m.variationsCache.Get(productID); ok {
			return variationsLoadedMsg{variations: variations}
		}

		// Fetch from API
		variations, err := m.wooClient.GetVariations(context.Background(), productID)
		if err != nil {
			return errMsg{err: err}
		}

		// Cache the result
		m.variationsCache.Set(productID, variations)

		return variationsLoadedMsg{variations: variations}
	}
}

func (m *Model) initConfigurator() {
	if m.selectedProduct == nil || !m.selectedProduct.IsVariable() {
		return
	}

	// Build size options from variations
	var sizeOptions []huh.Option[string]
	sizeAttr := m.selectedProduct.GetAttribute("Size")
	if sizeAttr != nil {
		for _, opt := range sizeAttr.Options {
			// Find the variation with this size to get its price
			var price string
			for _, v := range m.productVariations {
				if v.GetAttributeValue("Size") == opt {
					price = v.GetDisplayPrice()
					break
				}
			}
			label := opt
			if price != "" {
				label = fmt.Sprintf("%s ($%s)", opt, price)
			}
			sizeOptions = append(sizeOptions, huh.NewOption(label, opt))
		}
	}

	// Build grind options
	var grindOptions []huh.Option[string]
	grindAttr := m.selectedProduct.GetAttribute("Grind Size")
	if grindAttr != nil {
		for _, opt := range grindAttr.Options {
			grindOptions = append(grindOptions, huh.NewOption(opt, opt))
		}
	} else {
		grindOptions = []huh.Option[string]{huh.NewOption("Whole Beans", "Whole Beans")}
	}

	var selectedSize string
	var selectedGrind string

	// Build form groups
	var groups []*huh.Group

	if len(sizeOptions) > 0 {
		groups = append(groups, huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select Size").
				Options(sizeOptions...).
				Value(&selectedSize),
		))
	}

	groups = append(groups, huh.NewGroup(
		huh.NewSelect[string]().
			Title("Select Grind Size").
			Options(grindOptions...).
			Value(&selectedGrind),
	))

	m.configForm = huh.NewForm(groups...).
		WithShowHelp(true).
		WithShowErrors(true)

	// Store pointers for later access
	m.selectedGrindSize = ""
}

func (m *Model) initSimpleConfigurator() {
	if m.selectedProduct == nil {
		return
	}

	// Build grind options
	var grindOptions []huh.Option[string]
	grindAttr := m.selectedProduct.GetAttribute("Grind Size")
	if grindAttr != nil {
		for _, opt := range grindAttr.Options {
			grindOptions = append(grindOptions, huh.NewOption(opt, opt))
		}
	} else {
		grindOptions = []huh.Option[string]{huh.NewOption("Whole Beans", "Whole Beans")}
	}

	var selectedGrind string

	m.configForm = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select Grind Size").
				Options(grindOptions...).
				Value(&selectedGrind),
		),
	).WithShowHelp(true).WithShowErrors(true)
}

// View renders the current view.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
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
	case ViewQuote:
		content = m.viewQuote()
	case ViewPayment:
		content = m.viewPayment()
	case ViewReview:
		content = m.viewReview()
	case ViewOrderConfirmation:
		content = m.viewOrderConfirmation()
	}

	return m.styles.App.Render(content)
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
	} else if m.err != nil {
		sb.WriteString(m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err)))
	} else {
		sb.WriteString(m.productList.View())
	}

	// Help bar with cart info
	cartInfo := ""
	if m.localCart.ItemCount() > 0 {
		cartInfo = fmt.Sprintf(" • 🛒 %d items (%s)", m.localCart.ItemCount(), m.localCart.GetSubtotal())
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
	if p.SalePrice != "" && p.SalePrice != p.RegularPrice {
		sb.WriteString(m.styles.ProductSalePrice.Render(fmt.Sprintf("$%s", price)))
		sb.WriteString(" ")
		sb.WriteString(m.styles.Subtle.Render(fmt.Sprintf("(was $%s)", p.RegularPrice)))
	} else {
		sb.WriteString(m.styles.ProductPrice.Render(fmt.Sprintf("$%s", price)))
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
		sb.WriteString(fmt.Sprintf("Price: $%s\n", m.selectedVariation.GetDisplayPrice()))
	} else if m.selectedProduct != nil {
		sb.WriteString(fmt.Sprintf("Price: $%s\n", m.selectedProduct.GetDisplayPrice()))
	}

	if m.selectedGrindSize != "" {
		sb.WriteString(fmt.Sprintf("Grind: %s\n", m.selectedGrindSize))
	}

	return sb.String()
}

func (m Model) viewCart() string {
	var sb strings.Builder

	// Header
	sb.WriteString(m.styles.HeaderTitle.Render("🛒 Shopping Cart"))
	sb.WriteString("\n\n")

	if m.localCart.IsEmpty() {
		sb.WriteString(m.styles.Subtle.Render("Your cart is empty"))
		sb.WriteString("\n\n")
		sb.WriteString(m.styles.HelpBar.Render("esc back to products"))
		return m.styles.Box.Render(sb.String())
	}

	// Cart items (local)
	for i, item := range m.localCart.Items {
		prefix := "  "
		if i == m.localCart.SelectedIdx {
			prefix = m.styles.Highlight.Render("▸ ")
		}

		name := item.GetDisplayName()
		price := item.GetFormattedPrice()
		qty := fmt.Sprintf("x%d", item.Quantity)
		total := item.GetFormattedTotal()

		line := fmt.Sprintf("%s%s  %s  %s  = %s", prefix, name, price, qty, total)
		if i == m.localCart.SelectedIdx {
			sb.WriteString(m.styles.Highlight.Render(line))
		} else {
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}

	// Totals (local estimate)
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Subtotal: %s\n", m.localCart.GetSubtotal()))
	sb.WriteString(m.styles.ProductPrice.Render(fmt.Sprintf("Estimated Total: %s", m.localCart.GetTotal())))
	sb.WriteString(fmt.Sprintf(" (%d items)", m.localCart.ItemCount()))
	sb.WriteString("\n")
	sb.WriteString(m.styles.Subtle.Render("(Taxes and shipping calculated at checkout)"))
	sb.WriteString("\n")

	// Help bar
	sb.WriteString("\n")
	sb.WriteString(m.styles.HelpBar.Render("↑/↓ select • +/- quantity • d delete • o checkout • s continue shopping • esc back"))

	return m.styles.Box.Render(sb.String())
}

func (m Model) viewQuote() string {
	var sb strings.Builder

	// Header with progress
	sb.WriteString(m.styles.HeaderTitle.Render("📦 Quote & Shipping"))
	sb.WriteString("  ")
	sb.WriteString(m.styles.Subtle.Render("Step 1 of 3"))
	sb.WriteString("\n\n")

	if m.loadingQuote {
		sb.WriteString(m.listSpinner.View())
		sb.WriteString(" Getting quote...")
		return m.styles.Box.Render(sb.String())
	}

	if m.err != nil {
		sb.WriteString(m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err)))
		sb.WriteString("\n\n")
	}

	// Address form
	if m.addressForm != nil && m.addressForm.State != huh.StateCompleted {
		sb.WriteString(m.styles.Subtle.Render("Shipping Address:"))
		sb.WriteString("\n")
		sb.WriteString(m.addressForm.View())
		sb.WriteString("\n")
		sb.WriteString(m.styles.HelpBar.Render("esc back • tab navigate • enter submit"))
		return m.styles.Box.Render(sb.String())
	}

	// Show address summary
	if m.customerInfo.FirstName != "" {
		sb.WriteString(m.styles.Subtle.Render("Ship to:"))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("  %s %s\n", m.customerInfo.FirstName, m.customerInfo.LastName))
		if m.customerInfo.Address != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", m.customerInfo.Address))
		}
		if m.customerInfo.City != "" || m.customerInfo.Postcode != "" {
			sb.WriteString(fmt.Sprintf("  %s %s %s\n", m.customerInfo.City, m.customerInfo.Postcode, m.customerInfo.Country))
		}
		sb.WriteString("\n")
	}

	// Quote details
	if m.localCart.HasQuote() {
		quote := m.localCart.Quote

		// Show line items with calculated prices
		sb.WriteString(m.styles.Subtle.Render("Quote Items:"))
		sb.WriteString("\n")
		for _, item := range quote.LineItems {
			sb.WriteString(fmt.Sprintf("  • %s x%d = %s\n", item.Name, item.Quantity, quote.FormatPrice(item.LineTotal)))
		}
		sb.WriteString("\n")

		// Shipping rate selection
		if len(quote.ShippingRates) > 0 {
			sb.WriteString(m.styles.Subtle.Render("Select Shipping Method:"))
			sb.WriteString("\n\n")

			for i, rate := range quote.ShippingRates {
				prefix := "  "
				if i == m.shippingSelectedIdx {
					prefix = m.styles.Highlight.Render("▸ ")
				}

				price := quote.FormatPrice(rate.Cost)
				line := fmt.Sprintf("%s%s - %s", prefix, rate.Label, price)
				if rate.RateID == m.localCart.SelectedShippingRateID {
					line += m.styles.Success.Render(" ✓")
				}
				if i == m.shippingSelectedIdx {
					sb.WriteString(m.styles.Highlight.Render(line))
				} else {
					sb.WriteString(line)
				}
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		} else {
			sb.WriteString(m.styles.Success.Render("✓ No shipping required"))
			sb.WriteString("\n\n")
		}

		// Totals
		sb.WriteString(fmt.Sprintf("Subtotal: %s\n", quote.FormatPrice(quote.Totals.Subtotal)))
		if quote.Totals.Discount != "0" {
			sb.WriteString(fmt.Sprintf("Discount: -%s\n", quote.FormatPrice(quote.Totals.Discount)))
		}
		if m.localCart.GetSelectedShippingRate() != nil {
			sb.WriteString(fmt.Sprintf("Shipping: %s\n", m.localCart.GetShipping()))
		}
		if quote.Totals.Tax != "0" {
			sb.WriteString(fmt.Sprintf("Tax: %s\n", quote.FormatPrice(quote.Totals.Tax)))
		}
		sb.WriteString(m.styles.ProductPrice.Render(fmt.Sprintf("Total: %s", quote.FormatPrice(quote.Totals.Total))))
		sb.WriteString("\n")

		sb.WriteString("\n")
		sb.WriteString(m.styles.HelpBar.Render("↑/↓ select • enter confirm • n next step • esc back"))
	} else {
		sb.WriteString(m.styles.Subtle.Render("Enter your address to get a quote"))
	}

	return m.styles.Box.Render(sb.String())
}

func (m Model) viewPayment() string {
	var sb strings.Builder

	// Header with progress
	sb.WriteString(m.styles.HeaderTitle.Render("💳 Payment"))
	sb.WriteString("  ")
	sb.WriteString(m.styles.Subtle.Render("Step 2 of 3"))
	sb.WriteString("\n\n")

	if m.loadingPayment {
		sb.WriteString(m.listSpinner.View())
		sb.WriteString(" Loading payment methods...")
		return m.styles.Box.Render(sb.String())
	}

	if m.err != nil {
		sb.WriteString(m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err)))
		sb.WriteString("\n\n")
	}

	// Order summary
	sb.WriteString(m.styles.Subtle.Render("Order Summary:"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  Subtotal: %s\n", m.localCart.GetSubtotal()))
	if m.localCart.GetShipping() != "$0.00" {
		sb.WriteString(fmt.Sprintf("  Shipping: %s\n", m.localCart.GetShipping()))
	}
	if m.localCart.GetTax() != "$0.00" {
		sb.WriteString(fmt.Sprintf("  Tax: %s\n", m.localCart.GetTax()))
	}
	sb.WriteString(m.styles.ProductPrice.Render(fmt.Sprintf("  Total: %s", m.localCart.GetTotal())))
	sb.WriteString("\n\n")

	// Payment methods
	if len(m.paymentGateways) > 0 {
		sb.WriteString(m.styles.Subtle.Render("Select Payment Method:"))
		sb.WriteString("\n\n")

		for i, gateway := range m.paymentGateways {
			prefix := "  "
			if i == m.paymentSelectedIdx {
				prefix = m.styles.Highlight.Render("▸ ")
			}

			line := fmt.Sprintf("%s%s", prefix, gateway.Title)
			if gateway.Description != "" {
				line += fmt.Sprintf(" - %s", StripHTML(gateway.Description))
			}
			if i == m.paymentSelectedIdx {
				sb.WriteString(m.styles.Highlight.Render(line))
			} else {
				sb.WriteString(line)
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString(m.styles.Subtle.Render("No payment methods available"))
		sb.WriteString("\n")
	}

	// Help bar
	sb.WriteString("\n")
	sb.WriteString(m.styles.HelpBar.Render("↑/↓ select • enter continue • esc back"))

	return m.styles.Box.Render(sb.String())
}

func (m Model) viewReview() string {
	var sb strings.Builder

	// Header with progress
	sb.WriteString(m.styles.HeaderTitle.Render("📋 Review Order"))
	sb.WriteString("  ")
	sb.WriteString(m.styles.Subtle.Render("Step 3 of 3"))
	sb.WriteString("\n\n")

	if m.creatingOrder {
		sb.WriteString(m.listSpinner.View())
		sb.WriteString(" Placing order...")
		return m.styles.Box.Render(sb.String())
	}

	if m.err != nil {
		sb.WriteString(m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err)))
		sb.WriteString("\n\n")
	}

	// Shipping address
	sb.WriteString(m.styles.Subtle.Render("Shipping Address:"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  %s %s\n", m.customerInfo.FirstName, m.customerInfo.LastName))
	if m.customerInfo.Address != "" {
		sb.WriteString(fmt.Sprintf("  %s\n", m.customerInfo.Address))
	}
	sb.WriteString(fmt.Sprintf("  %s %s %s\n", m.customerInfo.City, m.customerInfo.Postcode, m.customerInfo.Country))
	sb.WriteString(fmt.Sprintf("  %s\n", m.customerInfo.Email))
	sb.WriteString("\n")

	// Shipping method
	if rate := m.localCart.GetSelectedShippingRate(); rate != nil {
		sb.WriteString(m.styles.Subtle.Render("Shipping Method:"))
		sb.WriteString("\n")
		price := m.localCart.Quote.FormatPrice(rate.Cost)
		sb.WriteString(fmt.Sprintf("  %s - %s\n\n", rate.Label, price))
	}

	// Payment method
	if len(m.paymentGateways) > 0 && m.paymentSelectedIdx < len(m.paymentGateways) {
		sb.WriteString(m.styles.Subtle.Render("Payment Method:"))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("  %s\n\n", m.paymentGateways[m.paymentSelectedIdx].Title))
	}

	// Items
	sb.WriteString(m.styles.Subtle.Render("Items:"))
	sb.WriteString("\n")
	if m.localCart.HasQuote() {
		for _, item := range m.localCart.Quote.LineItems {
			total := m.localCart.Quote.FormatPrice(item.LineTotal)
			sb.WriteString(fmt.Sprintf("  • %s x%d = %s\n", item.Name, item.Quantity, total))
		}
	}
	sb.WriteString("\n")

	// Totals
	sb.WriteString(fmt.Sprintf("Subtotal: %s\n", m.localCart.GetSubtotal()))
	if m.localCart.GetShipping() != "$0.00" {
		sb.WriteString(fmt.Sprintf("Shipping: %s\n", m.localCart.GetShipping()))
	}
	if m.localCart.GetDiscount() != "$0.00" {
		sb.WriteString(fmt.Sprintf("Discount: -%s\n", m.localCart.GetDiscount()))
	}
	if m.localCart.GetTax() != "$0.00" {
		sb.WriteString(fmt.Sprintf("Tax: %s\n", m.localCart.GetTax()))
	}
	sb.WriteString(m.styles.ProductPrice.Render(fmt.Sprintf("\nTotal: %s", m.localCart.GetTotal())))
	sb.WriteString("\n")

	// Help bar
	sb.WriteString("\n")
	sb.WriteString(m.styles.HelpBar.Render("p/enter place order • esc back"))

	return m.styles.Box.Render(sb.String())
}

func (m Model) viewOrderConfirmation() string {
	var sb strings.Builder

	// Header
	sb.WriteString(m.styles.Success.Render("✓ Order Placed Successfully!"))
	sb.WriteString("\n\n")

	if m.orderResponse != nil {
		sb.WriteString(fmt.Sprintf("Order #%d\n", m.orderResponse.OrderID))
		sb.WriteString(fmt.Sprintf("Status: %s\n", m.orderResponse.Status))
		sb.WriteString(fmt.Sprintf("Order Key: %s\n", m.orderResponse.OrderKey))

		sb.WriteString("\n")
		sb.WriteString(m.styles.Subtle.Render("Shipping Address:"))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("  %s %s\n", m.customerInfo.FirstName, m.customerInfo.LastName))
		if m.customerInfo.Address != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", m.customerInfo.Address))
		}
		sb.WriteString(fmt.Sprintf("  %s, %s %s\n",
			m.customerInfo.City,
			m.customerInfo.Postcode,
			m.customerInfo.Country))

		sb.WriteString("\n")
		sb.WriteString(m.styles.Subtle.Render("Next Step:"))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("  %s\n", m.orderResponse.NextAction))
		if m.orderResponse.PaymentURL != "" {
			sb.WriteString(fmt.Sprintf("  Payment URL: %s\n", m.orderResponse.PaymentURL))
		}
	}

	if m.err != nil {
		sb.WriteString("\n")
		sb.WriteString(m.styles.Error.Render(fmt.Sprintf("Note: %v", m.err)))
	}

	// Help bar
	sb.WriteString("\n\n")
	sb.WriteString(m.styles.HelpBar.Render("Press Enter to continue shopping"))

	return m.styles.Box.Render(sb.String())
}

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



