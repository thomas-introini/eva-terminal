// Package tui implements the terminal user interface using Bubble Tea.
package tui

import "github.com/charmbracelet/lipgloss"

// Color palette - warm coffee tones
var (
	colorCream     = lipgloss.Color("#FFF8E7")
	colorEspresso  = lipgloss.Color("#3C2415")
	colorCaramel   = lipgloss.Color("#D4A574")
	colorMocha     = lipgloss.Color("#8B7355")
	colorRoast     = lipgloss.Color("#5D4037")
	colorHighlight = lipgloss.Color("#FF9800")
	colorSuccess   = lipgloss.Color("#4CAF50")
	colorWarning   = lipgloss.Color("#FFC107")
	colorError     = lipgloss.Color("#F44336")
	colorMuted     = lipgloss.Color("#9E9E9E")
)

// Styles holds all the lipgloss styles for the TUI.
type Styles struct {
	// App container
	App lipgloss.Style

	// Header
	Header      lipgloss.Style
	HeaderTitle lipgloss.Style
	HeaderHelp  lipgloss.Style

	// List styles
	ListTitle        lipgloss.Style
	ListItem         lipgloss.Style
	ListItemSelected lipgloss.Style
	ListItemDesc     lipgloss.Style

	// Product details
	ProductName        lipgloss.Style
	ProductPrice       lipgloss.Style
	ProductSalePrice   lipgloss.Style
	ProductDescription lipgloss.Style
	ProductAttribute   lipgloss.Style
	ProductInStock     lipgloss.Style
	ProductOutOfStock  lipgloss.Style

	// Configurator
	ConfigTitle   lipgloss.Style
	ConfigOption  lipgloss.Style
	ConfigSummary lipgloss.Style

	// General
	Subtle    lipgloss.Style
	Highlight lipgloss.Style
	Error     lipgloss.Style
	Success   lipgloss.Style
	Box       lipgloss.Style
	HelpBar   lipgloss.Style
}

// DefaultStyles returns the default TUI styles.
func DefaultStyles() Styles {
	return Styles{
		App: lipgloss.NewStyle().
			Padding(1, 2),

		Header: lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(colorMocha).
			MarginBottom(1).
			Padding(0, 1),

		HeaderTitle: lipgloss.NewStyle().
			Foreground(colorCaramel).
			Bold(true),

		HeaderHelp: lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true),

		ListTitle: lipgloss.NewStyle().
			Foreground(colorCaramel).
			Bold(true).
			MarginBottom(1),

		ListItem: lipgloss.NewStyle().
			Foreground(colorCream).
			PaddingLeft(2),

		ListItemSelected: lipgloss.NewStyle().
			Foreground(colorHighlight).
			Bold(true).
			PaddingLeft(1).
			SetString("▸ "),

		ListItemDesc: lipgloss.NewStyle().
			Foreground(colorMuted),

		ProductName: lipgloss.NewStyle().
			Foreground(colorCaramel).
			Bold(true).
			MarginBottom(1),

		ProductPrice: lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true),

		ProductSalePrice: lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true),

		ProductDescription: lipgloss.NewStyle().
			Foreground(colorCream).
			MarginTop(1).
			MarginBottom(1),

		ProductAttribute: lipgloss.NewStyle().
			Foreground(colorMocha),

		ProductInStock: lipgloss.NewStyle().
			Foreground(colorSuccess),

		ProductOutOfStock: lipgloss.NewStyle().
			Foreground(colorError),

		ConfigTitle: lipgloss.NewStyle().
			Foreground(colorCaramel).
			Bold(true).
			MarginBottom(1),

		ConfigOption: lipgloss.NewStyle().
			Foreground(colorCream),

		ConfigSummary: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorMocha).
			Padding(1, 2).
			MarginTop(1),

		Subtle: lipgloss.NewStyle().
			Foreground(colorMuted),

		Highlight: lipgloss.NewStyle().
			Foreground(colorHighlight).
			Bold(true),

		Error: lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true),

		Success: lipgloss.NewStyle().
			Foreground(colorSuccess),

		Box: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorMocha).
			Padding(1, 2),

		HelpBar: lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginTop(1),
	}
}



