package tui

import (
	"strings"

	"golang.org/x/net/html"
)

// StripHTML removes HTML tags from a string and converts it to plain text.
// It uses the golang.org/x/net/html tokenizer for safe parsing.
func StripHTML(s string) string {
	if s == "" {
		return ""
	}

	var result strings.Builder
	tokenizer := html.NewTokenizer(strings.NewReader(s))

	for {
		tokenType := tokenizer.Next()

		switch tokenType {
		case html.ErrorToken:
			// End of document or error
			return cleanupWhitespace(result.String())

		case html.TextToken:
			text := string(tokenizer.Text())
			result.WriteString(text)

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, _ := tokenizer.TagName()
			tagName := string(tn)

			// Add spacing for block elements
			switch tagName {
			case "p", "div", "br", "li", "h1", "h2", "h3", "h4", "h5", "h6":
				result.WriteString("\n")
			case "ul", "ol":
				result.WriteString("\n")
			}

		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			tagName := string(tn)

			// Add spacing after block elements
			switch tagName {
			case "p", "div", "li", "h1", "h2", "h3", "h4", "h5", "h6":
				result.WriteString("\n")
			case "ul", "ol":
				result.WriteString("\n")
			}
		}
	}
}

// cleanupWhitespace normalizes whitespace in the string.
func cleanupWhitespace(s string) string {
	// Split into lines
	lines := strings.Split(s, "\n")
	var cleanLines []string

	for _, line := range lines {
		// Trim whitespace from each line
		line = strings.TrimSpace(line)
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}

	// Join with single newlines
	result := strings.Join(cleanLines, "\n")

	// Decode common HTML entities
	result = decodeHTMLEntities(result)

	return result
}

// decodeHTMLEntities decodes common HTML entities.
func decodeHTMLEntities(s string) string {
	replacements := map[string]string{
		"&amp;":   "&",
		"&lt;":    "<",
		"&gt;":    ">",
		"&quot;":  "\"",
		"&#39;":   "'",
		"&apos;":  "'",
		"&nbsp;":  " ",
		"&mdash;": "—",
		"&ndash;": "–",
		"&hellip;": "…",
		"&copy;":  "©",
		"&reg;":   "®",
		"&trade;": "™",
	}

	for entity, replacement := range replacements {
		s = strings.ReplaceAll(s, entity, replacement)
	}

	return s
}



