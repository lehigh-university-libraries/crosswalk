// Package format defines the interface for metadata format plugins.
package format

import (
	"io"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
)

// Format defines the interface that all format plugins must implement.
type Format interface {
	// Name returns the format identifier (e.g., "drupal", "csv", "bibtex")
	Name() string

	// Description returns a human-readable format description
	Description() string

	// Extensions returns file extensions associated with this format
	Extensions() []string

	// CanParse returns true if this format can parse the given input
	CanParse(peek []byte) bool
}

// Parser is a format that can parse input into IR records.
type Parser interface {
	Format

	// Parse reads input and returns IR records.
	// Options is format-specific configuration.
	Parse(r io.Reader, opts *ParseOptions) ([]*hubv1.Record, error)
}

// Serializer is a format that can write IR records to output.
type Serializer interface {
	Format

	// Serialize writes IR records to the output.
	// Options is format-specific configuration.
	Serialize(w io.Writer, records []*hubv1.Record, opts *SerializeOptions) error
}

// ParseOptions contains options for parsing.
type ParseOptions struct {
	// Profile is the mapping profile to use
	Profile *mapping.Profile

	// TaxonomyResolver is an optional taxonomy term resolver
	TaxonomyResolver TaxonomyResolver

	// StripHTML removes HTML from text fields
	StripHTML bool

	// Strict fails on unknown fields
	Strict bool

	// SourceName is an identifier for the source (for error messages)
	SourceName string

	// BaseURL is the base URL for the source system (e.g., "https://preserve.lehigh.edu")
	// Used to construct full URLs for relations and other references.
	BaseURL string
}

// SerializeOptions contains options for serialization.
type SerializeOptions struct {
	// Profile is the mapping profile to use
	Profile *mapping.Profile

	// Columns specifies which columns to include (for tabular formats)
	Columns []string

	// MultiValueSeparator is the delimiter for multi-value fields
	MultiValueSeparator string

	// IncludeHeader includes a header row (for tabular formats)
	IncludeHeader bool

	// Pretty enables pretty-printing (for JSON/XML formats)
	Pretty bool

	// ExtraWriters holds additional output writers for formats that produce
	// more than one output file. Keys are format-specific names.
	// Example: the islandora-workbench format writes an agents CSV to ExtraWriters["agents"].
	ExtraWriters map[string]io.Writer
}

// TaxonomyResolver resolves taxonomy term IDs to their values.
type TaxonomyResolver interface {
	// Resolve returns the term name for a taxonomy term ID.
	Resolve(termID string, vocabulary string) (string, bool)

	// ResolveNode returns the node title for a node ID.
	ResolveNode(nodeID string) (string, bool)
}

// NewParseOptions creates ParseOptions with defaults.
func NewParseOptions() *ParseOptions {
	return &ParseOptions{
		StripHTML: true,
	}
}

// NewSerializeOptions creates SerializeOptions with defaults.
func NewSerializeOptions() *SerializeOptions {
	return &SerializeOptions{
		MultiValueSeparator: "|",
		IncludeHeader:       true,
	}
}
