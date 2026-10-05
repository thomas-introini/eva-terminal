// Package tui implements the terminal storefront.
package tui

import (
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

type Styles struct {
	App, HeaderTitle, Subtle, Highlight, Selection, Border, Error, Success, HelpBar lipgloss.Style
}

func DefaultStyles() Styles { return coffeeStyles(true, false, colorprofile.TrueColor) }

func coffeeStyles(dark, noColor bool, profile colorprofile.Profile) Styles {
	accent, muted, success, danger := "#F5B916", "#A89F93", "#9BD598", "#FF9B8D"
	if !dark {
		accent, muted, success, danger = "#743C12", "#655445", "#24652D", "#AD2818"
	}
	colored := func(hex string) lipgloss.Style {
		style := lipgloss.NewStyle()
		if !noColor && profile != colorprofile.Ascii {
			style = style.Foreground(profile.Convert(lipgloss.Color(hex)))
		}
		return style
	}
	title, subtle := colored(accent).Bold(true), colored(muted)
	selection := lipgloss.NewStyle().Bold(true).Reverse(true)
	if !noColor && profile != colorprofile.Ascii {
		selection = lipgloss.NewStyle().Bold(true).Background(profile.Convert(lipgloss.Color("#F5B916"))).Foreground(profile.Convert(lipgloss.Color("#211B14")))
	}
	ok, bad := colored(success), colored(danger).Bold(true)
	return Styles{
		App: lipgloss.NewStyle().Padding(0, 1), HeaderTitle: title,
		Subtle: subtle, Highlight: title, Selection: selection, Border: subtle, Error: bad, Success: ok, HelpBar: subtle,
	}
}

func (m Model) formTheme() huh.Theme {
	return huh.ThemeFunc(func(bool) *huh.Styles {
		t := huh.ThemeBase(m.dark)
		t.Focused.Base = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).PaddingLeft(1)
		t.Focused.Title = m.styles.HeaderTitle
		t.Focused.Description = m.styles.Subtle
		t.Focused.ErrorMessage, t.Focused.ErrorIndicator = m.styles.Error, m.styles.Error
		t.Focused.SelectSelector = m.styles.Highlight.SetString("> ")
		t.Focused.NextIndicator, t.Focused.PrevIndicator = m.styles.Subtle, m.styles.Subtle
		t.Focused.Option = lipgloss.NewStyle()
		t.Focused.TextInput.Text = lipgloss.NewStyle()
		t.Focused.TextInput.Cursor = m.styles.Highlight
		t.Focused.TextInput.Placeholder = m.styles.Subtle
		t.Focused.TextInput.Prompt = m.styles.Highlight
		t.Blurred = t.Focused
		t.Blurred.Base = lipgloss.NewStyle().PaddingLeft(2)
		t.Blurred.Title = m.styles.Subtle
		t.Group.Title, t.Group.Description = m.styles.HeaderTitle, m.styles.Subtle
		return t
	})
}
