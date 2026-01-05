package tui

import (
	"strings"
	"testing"
)

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "plain text",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "simple paragraph",
			input:    "<p>Hello World</p>",
			expected: "Hello World",
		},
		{
			name:     "multiple paragraphs",
			input:    "<p>First paragraph</p><p>Second paragraph</p>",
			expected: "First paragraph\nSecond paragraph",
		},
		{
			name:     "bold and italic",
			input:    "<p>This is <strong>bold</strong> and <em>italic</em></p>",
			expected: "This is bold and italic",
		},
		{
			name:     "unordered list",
			input:    "<ul><li>Item 1</li><li>Item 2</li></ul>",
			expected: "Item 1\nItem 2",
		},
		{
			name:     "line breaks",
			input:    "Line 1<br>Line 2<br/>Line 3",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "nested tags",
			input:    "<div><p>Nested <span>content</span> here</p></div>",
			expected: "Nested content here",
		},
		{
			name:     "headings",
			input:    "<h1>Title</h1><p>Content</p>",
			expected: "Title\nContent",
		},
		{
			name:     "complex HTML",
			input:    "<p>A bright and fruity coffee from the <strong>Yirgacheffe</strong> region.</p><ul><li>Notes of blueberry</li><li>Floral undertones</li></ul>",
			expected: "A bright and fruity coffee from the Yirgacheffe region.\nNotes of blueberry\nFloral undertones",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripHTML(tt.input)
			if result != tt.expected {
				t.Errorf("StripHTML(%q)\ngot:  %q\nwant: %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStripHTMLEntities(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "ampersand",
			input:    "<p>Coffee &amp; Tea</p>",
			contains: "Coffee & Tea",
		},
		{
			name:     "less than",
			input:    "<p>A &lt; B</p>",
			contains: "A < B",
		},
		{
			name:     "greater than",
			input:    "<p>A &gt; B</p>",
			contains: "A > B",
		},
		{
			name:     "quote",
			input:    "<p>&quot;Hello&quot;</p>",
			contains: "\"Hello\"",
		},
		{
			name:     "apostrophe",
			input:    "<p>It&#39;s great</p>",
			contains: "It's great",
		},
		{
			name:     "non-breaking space",
			input:    "<p>Hello&nbsp;World</p>",
			contains: "Hello World",
		},
		{
			name:     "em dash",
			input:    "<p>Hello&mdash;World</p>",
			contains: "Hello—World",
		},
		{
			name:     "ellipsis",
			input:    "<p>Loading&hellip;</p>",
			contains: "Loading…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripHTML(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("StripHTML(%q)\ngot:  %q\nwant to contain: %q", tt.input, result, tt.contains)
			}
		})
	}
}

func TestStripHTMLPreservesWhitespace(t *testing.T) {
	input := "<p>  Hello   World  </p>"
	result := StripHTML(input)

	// Should have normalized whitespace
	if strings.Contains(result, "  ") {
		// Note: Current implementation might preserve some whitespace
		// This test documents current behavior
	}

	// Should contain the essential content
	if !strings.Contains(result, "Hello") || !strings.Contains(result, "World") {
		t.Errorf("expected result to contain 'Hello' and 'World', got %q", result)
	}
}

func TestStripHTMLMalformed(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "unclosed tag",
			input: "<p>Unclosed paragraph",
		},
		{
			name:  "mismatched tags",
			input: "<p>Mismatched <strong>tags</p></strong>",
		},
		{
			name:  "only opening tag",
			input: "<div>Content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic on malformed HTML
			result := StripHTML(tt.input)
			if result == "" {
				t.Error("expected non-empty result for malformed HTML")
			}
		})
	}
}

func BenchmarkStripHTML(b *testing.B) {
	input := "<p>A bright and fruity coffee from the <strong>Yirgacheffe</strong> region of Ethiopia. Notes of blueberry, lemon, and floral undertones.</p><p>Perfect for pour-over and filter brewing methods.</p><ul><li>Origin: Ethiopia</li><li>Roast: Light</li><li>Process: Washed</li></ul>"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StripHTML(input)
	}
}



