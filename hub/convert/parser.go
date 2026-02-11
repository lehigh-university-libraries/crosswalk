package convert

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ParserFunc is a function that parses a string value.
// It takes the input string and any relevant options, returning the parsed value.
type ParserFunc func(input string, opts *ParserOptions) (any, error)

// ParserOptions contains configuration for parsers.
type ParserOptions struct {
	// DateFormat specifies the expected date format for parsing
	DateFormat string

	// Delimiter specifies the delimiter for splitting strings
	Delimiter string

	// CustomData allows passing additional parser-specific configuration
	CustomData map[string]any
}

// ParserRegistry manages registered parsers.
type ParserRegistry struct {
	mu      sync.RWMutex
	parsers map[string]ParserFunc
}

// NewParserRegistry creates a new parser registry with default parsers.
func NewParserRegistry() *ParserRegistry {
	r := &ParserRegistry{
		parsers: make(map[string]ParserFunc),
	}
	r.registerDefaults()
	return r
}

// Register adds a parser to the registry.
func (r *ParserRegistry) Register(name string, fn ParserFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parsers[name] = fn
}

// Get retrieves a parser by name.
func (r *ParserRegistry) Get(name string) (ParserFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.parsers[name]
	return fn, ok
}

// Parse applies a named parser to an input string.
func (r *ParserRegistry) Parse(parserName string, input string, opts *ParserOptions) (any, error) {
	fn, ok := r.Get(parserName)
	if !ok {
		return nil, fmt.Errorf("parser not found: %s", parserName)
	}
	if opts == nil {
		opts = &ParserOptions{}
	}
	return fn(input, opts)
}

// Names returns all registered parser names.
func (r *ParserRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.parsers))
	for name := range r.parsers {
		names = append(names, name)
	}
	return names
}

// registerDefaults registers all built-in parsers.
func (r *ParserRegistry) registerDefaults() {
	r.Register("passthrough", parsePassthrough)
	r.Register("strip_html", parseStripHTML)
	r.Register("normalize_whitespace", parseNormalizeWhitespace)
	r.Register("year", parseYear)
	r.Register("iso8601", parseISO8601)
	r.Register("edtf", parseEDTF)
	r.Register("split", parseSplit)
	r.Register("doi", parseDOI)
	r.Register("isbn", parseISBN)
	r.Register("orcid", parseORCID)
	r.Register("url", parseURL)
	r.Register("bibtex_name", parseBibTeXName)
	r.Register("csl_name", parseCSLName)
	r.Register("relator", parseRelator)
	r.Register("lowercase", parseLowercase)
	r.Register("uppercase", parseUppercase)
	r.Register("trim", parseTrim)
}

// Default parser registry instance.
var defaultParserRegistry = NewParserRegistry()

// DefaultParsers returns the default parser registry.
func DefaultParsers() *ParserRegistry {
	return defaultParserRegistry
}

// parsePassthrough returns the input unchanged.
func parsePassthrough(input string, opts *ParserOptions) (any, error) {
	return input, nil
}

// parseStripHTML removes HTML tags from the input.
func parseStripHTML(input string, opts *ParserOptions) (any, error) {
	// Unescape HTML entities first
	input = html.UnescapeString(input)

	// Remove HTML tags
	tagRegex := regexp.MustCompile(`<[^>]*>`)
	result := tagRegex.ReplaceAllString(input, "")

	// Normalize whitespace
	result = strings.TrimSpace(result)
	wsRegex := regexp.MustCompile(`\s+`)
	result = wsRegex.ReplaceAllString(result, " ")

	return result, nil
}

// parseNormalizeWhitespace collapses multiple whitespace into single spaces.
func parseNormalizeWhitespace(input string, opts *ParserOptions) (any, error) {
	input = strings.TrimSpace(input)
	wsRegex := regexp.MustCompile(`\s+`)
	return wsRegex.ReplaceAllString(input, " "), nil
}

// parseYear extracts a 4-digit year from a string.
func parseYear(input string, opts *ParserOptions) (any, error) {
	yearRegex := regexp.MustCompile(`\b(1[0-9]{3}|20[0-9]{2})\b`)
	match := yearRegex.FindString(input)
	if match == "" {
		return input, nil // Return original if no year found
	}
	return match, nil
}

// parseISO8601 parses ISO 8601 date strings.
func parseISO8601(input string, opts *ParserOptions) (any, error) {
	input = strings.TrimSpace(input)

	// Common ISO 8601 formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
		"2006-01",
		"2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, input); err == nil {
			return t.Format("2006-01-02"), nil
		}
	}

	return input, nil
}

// parseEDTF parses Extended Date/Time Format strings.
// EDTF supports uncertain, approximate, and interval dates.
func parseEDTF(input string, opts *ParserOptions) (any, error) {
	input = strings.TrimSpace(input)

	// Basic validation - EDTF allows ~, ?, /, and X for uncertainty
	// For now, preserve the original EDTF string
	// A full EDTF parser would convert to normalized form

	return input, nil
}

// parseSplit splits a string by a delimiter.
func parseSplit(input string, opts *ParserOptions) (any, error) {
	delimiter := ","
	if opts != nil && opts.Delimiter != "" {
		delimiter = opts.Delimiter
	}

	parts := strings.Split(input, delimiter)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	return result, nil
}

// parseDOI extracts and normalizes a DOI.
func parseDOI(input string, opts *ParserOptions) (any, error) {
	input = strings.TrimSpace(input)

	// Handle various DOI formats:
	// - 10.1234/foo
	// - doi:10.1234/foo
	// - https://doi.org/10.1234/foo
	// - http://dx.doi.org/10.1234/foo

	// Remove common prefixes
	input = strings.TrimPrefix(input, "https://doi.org/")
	input = strings.TrimPrefix(input, "http://doi.org/")
	input = strings.TrimPrefix(input, "https://dx.doi.org/")
	input = strings.TrimPrefix(input, "http://dx.doi.org/")
	input = strings.TrimPrefix(input, "doi:")
	input = strings.TrimPrefix(input, "DOI:")

	// DOI regex pattern
	doiRegex := regexp.MustCompile(`10\.\d{4,}(?:\.\d+)*/[^\s]+`)
	match := doiRegex.FindString(input)
	if match != "" {
		return match, nil
	}

	return input, nil
}

// parseISBN normalizes an ISBN.
func parseISBN(input string, opts *ParserOptions) (any, error) {
	input = strings.TrimSpace(input)

	// Remove common prefixes
	input = strings.TrimPrefix(input, "ISBN:")
	input = strings.TrimPrefix(input, "ISBN-13:")
	input = strings.TrimPrefix(input, "ISBN-10:")
	input = strings.TrimPrefix(input, "isbn:")
	input = strings.TrimSpace(input)

	// Remove dashes and spaces
	cleaned := strings.ReplaceAll(input, "-", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")

	// Validate length (ISBN-10 or ISBN-13)
	if len(cleaned) == 10 || len(cleaned) == 13 {
		return cleaned, nil
	}

	return input, nil
}

// parseORCID normalizes an ORCID identifier.
func parseORCID(input string, opts *ParserOptions) (any, error) {
	input = strings.TrimSpace(input)

	// Remove common prefixes
	input = strings.TrimPrefix(input, "https://orcid.org/")
	input = strings.TrimPrefix(input, "http://orcid.org/")
	input = strings.TrimPrefix(input, "orcid.org/")
	input = strings.TrimPrefix(input, "ORCID:")
	input = strings.TrimPrefix(input, "orcid:")
	input = strings.TrimSpace(input)

	// ORCID format: 0000-0000-0000-000X
	orcidRegex := regexp.MustCompile(`^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$`)
	if orcidRegex.MatchString(input) {
		return input, nil
	}

	// Try to format raw digits
	digitsOnly := regexp.MustCompile(`[^\dX]`).ReplaceAllString(strings.ToUpper(input), "")
	if len(digitsOnly) == 16 {
		return fmt.Sprintf("%s-%s-%s-%s",
			digitsOnly[0:4], digitsOnly[4:8], digitsOnly[8:12], digitsOnly[12:16]), nil
	}

	return input, nil
}

// parseURL normalizes a URL.
func parseURL(input string, opts *ParserOptions) (any, error) {
	input = strings.TrimSpace(input)

	// Ensure protocol
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		if strings.Contains(input, ".") {
			input = "https://" + input
		}
	}

	return input, nil
}

// BibTeXName represents a parsed BibTeX name.
type BibTeXName struct {
	Given  string
	Family string
	Suffix string
}

// parseBibTeXName parses a BibTeX-format name.
// Supports "Last, First" and "First Last" formats.
func parseBibTeXName(input string, opts *ParserOptions) (any, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return BibTeXName{}, nil
	}

	// Handle "Last, First" format
	if strings.Contains(input, ",") {
		parts := strings.SplitN(input, ",", 2)
		family := strings.TrimSpace(parts[0])
		given := ""
		if len(parts) > 1 {
			given = strings.TrimSpace(parts[1])
		}

		// Check for suffix (Jr., III, etc.)
		suffix := ""
		if strings.Contains(given, ",") {
			givenParts := strings.SplitN(given, ",", 2)
			given = strings.TrimSpace(givenParts[0])
			suffix = strings.TrimSpace(givenParts[1])
		}

		return BibTeXName{
			Given:  given,
			Family: family,
			Suffix: suffix,
		}, nil
	}

	// Handle "First Last" format
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return BibTeXName{}, nil
	}
	if len(parts) == 1 {
		return BibTeXName{Family: parts[0]}, nil
	}

	// Last word is family name, rest is given
	family := parts[len(parts)-1]
	given := strings.Join(parts[:len(parts)-1], " ")

	return BibTeXName{
		Given:  given,
		Family: family,
	}, nil
}

// CSLName represents a parsed CSL-JSON name.
type CSLName struct {
	Given   string
	Family  string
	Suffix  string
	Literal string
}

// parseCSLName parses a CSL-JSON style name.
func parseCSLName(input string, opts *ParserOptions) (any, error) {
	// CSL names are typically structured data, but if passed as string,
	// we parse similarly to BibTeX
	result, err := parseBibTeXName(input, opts)
	if err != nil {
		return nil, err
	}

	btx := result.(BibTeXName)
	return CSLName{
		Given:  btx.Given,
		Family: btx.Family,
		Suffix: btx.Suffix,
	}, nil
}

// parseRelator normalizes MARC relator codes and terms.
func parseRelator(input string, opts *ParserOptions) (any, error) {
	input = strings.TrimSpace(input)
	lower := strings.ToLower(input)

	// Common mappings from terms to codes
	termToCode := map[string]string{
		"author":       "aut",
		"editor":       "edt",
		"translator":   "trl",
		"contributor":  "ctb",
		"creator":      "cre",
		"illustrator":  "ill",
		"photographer": "pht",
		"compiler":     "com",
		"narrator":     "nrt",
		"performer":    "prf",
		"sponsor":      "spn",
		"funder":       "fnd",
	}

	if code, ok := termToCode[lower]; ok {
		return code, nil
	}

	// If already a 3-letter code, return as-is
	if len(input) == 3 {
		return strings.ToLower(input), nil
	}

	return input, nil
}

// parseLowercase converts to lowercase.
func parseLowercase(input string, opts *ParserOptions) (any, error) {
	return strings.ToLower(input), nil
}

// parseUppercase converts to uppercase.
func parseUppercase(input string, opts *ParserOptions) (any, error) {
	return strings.ToUpper(input), nil
}

// parseTrim removes leading and trailing whitespace.
func parseTrim(input string, opts *ParserOptions) (any, error) {
	return strings.TrimFunc(input, unicode.IsSpace), nil
}
