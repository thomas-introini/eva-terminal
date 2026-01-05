package tui

import (
	"context"
	"fmt"
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

	// Error handling
	err error
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
		productsCache:   productsCache,
		variationsCache: variationsCache,
		viewState:       ViewProductList,
		styles:          styles,
		productList:     productList,
		searchInput:     ti,
		listSpinner:     sp,
		currentPage:     1,
		perPage:         20,
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

	case errMsg:
		m.err = msg.err
		m.loadingProducts = false
		m.loadingVariations = false
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
	}

	// Let the form handle the key
	if m.configForm != nil {
		form, cmd := m.configForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.configForm = f
			if m.configForm.State == huh.StateCompleted {
				m.configCompleted = true
			}
		}
		return m, cmd
	}

	return m, nil
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

	// Help bar
	help := "/ search • f filter in-stock • r refresh • enter select • q quit"
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
	sb.WriteString(m.styles.HelpBar.Render("esc back • enter/tab navigate • space select"))

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



