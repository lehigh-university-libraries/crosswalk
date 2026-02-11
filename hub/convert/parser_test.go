package convert

import (
	"reflect"
	"testing"
)

func TestParserRegistry_Register(t *testing.T) {
	r := NewParserRegistry()

	// Register a custom parser
	r.Register("test_parser", func(input string, opts *ParserOptions) (any, error) {
		return "parsed:" + input, nil
	})

	fn, ok := r.Get("test_parser")
	if !ok {
		t.Fatal("custom parser not found")
	}

	result, err := fn("value", nil)
	if err != nil {
		t.Fatalf("parser error: %v", err)
	}

	if result != "parsed:value" {
		t.Errorf("result = %v, want 'parsed:value'", result)
	}
}

func TestParserRegistry_DefaultParsers(t *testing.T) {
	r := DefaultParsers()

	expectedParsers := []string{
		"passthrough", "strip_html", "normalize_whitespace",
		"year", "iso8601", "edtf", "split", "doi", "isbn",
		"orcid", "url", "bibtex_name", "csl_name", "relator",
		"lowercase", "uppercase", "trim",
	}

	for _, name := range expectedParsers {
		if _, ok := r.Get(name); !ok {
			t.Errorf("default parser %q not found", name)
		}
	}
}

func TestParseDOI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"raw doi", "10.1234/foo.bar", "10.1234/foo.bar"},
		{"with doi prefix", "doi:10.1234/foo.bar", "10.1234/foo.bar"},
		{"with DOI prefix", "DOI:10.1234/foo.bar", "10.1234/foo.bar"},
		{"with https url", "https://doi.org/10.1234/foo.bar", "10.1234/foo.bar"},
		{"with dx url", "http://dx.doi.org/10.1234/foo.bar", "10.1234/foo.bar"},
		{"complex suffix", "10.1000/xyz123/abc.456", "10.1000/xyz123/abc.456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDOI(tt.input, nil)
			if err != nil {
				t.Fatalf("parseDOI error: %v", err)
			}
			if result != tt.want {
				t.Errorf("parseDOI(%q) = %v, want %v", tt.input, result, tt.want)
			}
		})
	}
}

func TestParseISBN(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"isbn-13 raw", "9781234567890", "9781234567890"},
		{"isbn-13 with dashes", "978-1-234-56789-0", "9781234567890"},
		{"isbn-10 raw", "0123456789", "0123456789"},
		{"isbn-10 with dashes", "0-12-345678-9", "0123456789"},
		{"with ISBN prefix", "ISBN:978-1-234-56789-0", "9781234567890"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseISBN(tt.input, nil)
			if err != nil {
				t.Fatalf("parseISBN error: %v", err)
			}
			if result != tt.want {
				t.Errorf("parseISBN(%q) = %v, want %v", tt.input, result, tt.want)
			}
		})
	}
}

func TestParseORCID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"formatted", "0000-0002-1825-0097", "0000-0002-1825-0097"},
		{"with url", "https://orcid.org/0000-0002-1825-0097", "0000-0002-1825-0097"},
		{"raw digits", "0000000218250097", "0000-0002-1825-0097"},
		{"with prefix", "orcid:0000-0002-1825-0097", "0000-0002-1825-0097"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseORCID(tt.input, nil)
			if err != nil {
				t.Fatalf("parseORCID error: %v", err)
			}
			if result != tt.want {
				t.Errorf("parseORCID(%q) = %v, want %v", tt.input, result, tt.want)
			}
		})
	}
}

func TestParseYear(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"four digit year", "2023", "2023"},
		{"year in text", "Published in 2023", "2023"},
		{"old year", "1888", "1888"},
		{"iso date", "2023-05-15", "2023"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseYear(tt.input, nil)
			if err != nil {
				t.Fatalf("parseYear error: %v", err)
			}
			if result != tt.want {
				t.Errorf("parseYear(%q) = %v, want %v", tt.input, result, tt.want)
			}
		})
	}
}

func TestParseISO8601(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"full date", "2023-05-15", "2023-05-15"},
		{"year-month", "2023-05", "2023-05-01"},
		{"year only", "2023", "2023-01-01"},
		{"with time", "2023-05-15T10:30:00", "2023-05-15"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseISO8601(tt.input, nil)
			if err != nil {
				t.Fatalf("parseISO8601 error: %v", err)
			}
			if result != tt.want {
				t.Errorf("parseISO8601(%q) = %v, want %v", tt.input, result, tt.want)
			}
		})
	}
}

func TestParseStripHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no html", "plain text", "plain text"},
		{"simple tags", "<b>bold</b> text", "bold text"},
		{"nested tags", "<p><em>emphasized</em></p>", "emphasized"},
		{"html entities", "Tom &amp; Jerry", "Tom & Jerry"},
		{"multiple spaces", "too   many    spaces", "too many spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseStripHTML(tt.input, nil)
			if err != nil {
				t.Fatalf("parseStripHTML error: %v", err)
			}
			if result != tt.want {
				t.Errorf("parseStripHTML(%q) = %v, want %v", tt.input, result, tt.want)
			}
		})
	}
}

func TestParseSplit(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		delimiter string
		want      []string
	}{
		{"comma separated", "a, b, c", ",", []string{"a", "b", "c"}},
		{"semicolon separated", "a; b; c", ";", []string{"a", "b", "c"}},
		{"default comma", "x,y,z", "", []string{"x", "y", "z"}},
		{"with empty parts", "a,,b", ",", []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &ParserOptions{Delimiter: tt.delimiter}
			result, err := parseSplit(tt.input, opts)
			if err != nil {
				t.Fatalf("parseSplit error: %v", err)
			}

			resultSlice, ok := result.([]string)
			if !ok {
				t.Fatalf("parseSplit returned %T, want []string", result)
			}

			if !reflect.DeepEqual(resultSlice, tt.want) {
				t.Errorf("parseSplit(%q) = %v, want %v", tt.input, resultSlice, tt.want)
			}
		})
	}
}

func TestParseBibTeXName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantGiven  string
		wantFamily string
		wantSuffix string
	}{
		{"last, first", "Smith, John", "John", "Smith", ""},
		{"first last", "John Smith", "John", "Smith", ""},
		{"with suffix", "Smith, John, Jr.", "John", "Smith", "Jr."},
		{"single name", "Madonna", "", "Madonna", ""},
		{"multiple given names", "Smith, John Paul", "John Paul", "Smith", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseBibTeXName(tt.input, nil)
			if err != nil {
				t.Fatalf("parseBibTeXName error: %v", err)
			}

			name, ok := result.(BibTeXName)
			if !ok {
				t.Fatalf("parseBibTeXName returned %T, want BibTeXName", result)
			}

			if name.Given != tt.wantGiven {
				t.Errorf("Given = %q, want %q", name.Given, tt.wantGiven)
			}
			if name.Family != tt.wantFamily {
				t.Errorf("Family = %q, want %q", name.Family, tt.wantFamily)
			}
			if name.Suffix != tt.wantSuffix {
				t.Errorf("Suffix = %q, want %q", name.Suffix, tt.wantSuffix)
			}
		})
	}
}

func TestParseRelator(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"author term", "author", "aut"},
		{"editor term", "editor", "edt"},
		{"already code", "aut", "aut"},
		{"uppercase code", "AUT", "aut"},
		{"unknown", "unknown_role", "unknown_role"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseRelator(tt.input, nil)
			if err != nil {
				t.Fatalf("parseRelator error: %v", err)
			}
			if result != tt.want {
				t.Errorf("parseRelator(%q) = %v, want %v", tt.input, result, tt.want)
			}
		})
	}
}

func TestParseURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"http url", "http://example.com", "http://example.com"},
		{"https url", "https://example.com", "https://example.com"},
		{"no protocol", "example.com/path", "https://example.com/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseURL(tt.input, nil)
			if err != nil {
				t.Fatalf("parseURL error: %v", err)
			}
			if result != tt.want {
				t.Errorf("parseURL(%q) = %v, want %v", tt.input, result, tt.want)
			}
		})
	}
}
