package helpers

import (
	"html"
	"regexp"
	"strings"
)

var (
	// HTML tag patterns
	htmlTagRegex     = regexp.MustCompile(`<[^>]*>`)
	htmlCommentRegex = regexp.MustCompile(`<!--[\s\S]*?-->`)
	multiSpaceRegex  = regexp.MustCompile(`\s+`)
	newlineRegex     = regexp.MustCompile(`[\r\n]+`)

	// Specific tag patterns for better text extraction
	brTagRegex    = regexp.MustCompile(`<br\s*/?>`)
	blockEndRegex = regexp.MustCompile(`</(?:p|div|li|h[1-6]|blockquote|tr)>`)
)

// StripHTML removes HTML tags from a string and decodes HTML entities.
func StripHTML(s string) string {
	if s == "" {
		return ""
	}

	// Remove comments first
	s = htmlCommentRegex.ReplaceAllString(s, "")

	// Convert block-level closing tags to newlines for better text flow
	s = blockEndRegex.ReplaceAllString(s, "\n")
	s = brTagRegex.ReplaceAllString(s, "\n")

	// Remove all remaining HTML tags
	s = htmlTagRegex.ReplaceAllString(s, "")

	// Decode HTML entities
	s = html.UnescapeString(s)

	// Normalize whitespace
	s = multiSpaceRegex.ReplaceAllString(s, " ")

	// Clean up multiple newlines
	s = regexp.MustCompile(`\n\s*\n`).ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}

// DecodeHTMLEntities decodes HTML entities without stripping tags.
func DecodeHTMLEntities(s string) string {
	return html.UnescapeString(s)
}

// EncodeHTMLEntities encodes special characters as HTML entities.
func EncodeHTMLEntities(s string) string {
	return html.EscapeString(s)
}

// StripHTMLPreserveLinks removes HTML but preserves link URLs.
func StripHTMLPreserveLinks(s string) string {
	if s == "" {
		return ""
	}

	// Extract links before stripping
	linkRegex := regexp.MustCompile(`<a[^>]*href=["']([^"']+)["'][^>]*>([^<]*)</a>`)
	s = linkRegex.ReplaceAllString(s, "$2 ($1)")

	return StripHTML(s)
}

// CleanText performs general text cleanup:
// - Strips HTML
// - Normalizes whitespace
// - Trims leading/trailing whitespace
func CleanText(s string) string {
	s = StripHTML(s)
	s = multiSpaceRegex.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// CleanTextPreserveNewlines cleans text but preserves intentional line breaks.
func CleanTextPreserveNewlines(s string) string {
	if s == "" {
		return ""
	}

	// Remove comments
	s = htmlCommentRegex.ReplaceAllString(s, "")

	// Convert block tags to double newlines
	s = blockEndRegex.ReplaceAllString(s, "\n\n")
	s = brTagRegex.ReplaceAllString(s, "\n")

	// Remove remaining tags
	s = htmlTagRegex.ReplaceAllString(s, "")

	// Decode entities
	s = html.UnescapeString(s)

	// Normalize spaces (but not newlines)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(multiSpaceRegex.ReplaceAllString(line, " "))
	}
	s = strings.Join(lines, "\n")

	// Clean up excessive newlines
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}

// IsHTML checks if a string appears to contain HTML markup.
func IsHTML(s string) bool {
	return htmlTagRegex.MatchString(s)
}

// ExtractTextFromHTML extracts plain text from HTML, converting
// certain elements to appropriate plain text representations.
func ExtractTextFromHTML(s string) string {
	if s == "" {
		return ""
	}

	// Convert lists to plain text with bullets
	ulRegex := regexp.MustCompile(`<li[^>]*>`)
	s = ulRegex.ReplaceAllString(s, "• ")

	// Convert headings to bold-ish text
	hRegex := regexp.MustCompile(`<h[1-6][^>]*>([^<]*)</h[1-6]>`)
	s = hRegex.ReplaceAllString(s, "\n$1\n")

	return CleanTextPreserveNewlines(s)
}

// TruncateText truncates text to a maximum length, adding ellipsis if needed.
func TruncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}

	// Try to truncate at a word boundary
	truncated := s[:maxLen-3]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxLen/2 {
		truncated = truncated[:lastSpace]
	}

	return truncated + "..."
}

// NormalizeWhitespace normalizes all whitespace to single spaces and trims.
func NormalizeWhitespace(s string) string {
	s = newlineRegex.ReplaceAllString(s, " ")
	s = multiSpaceRegex.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
