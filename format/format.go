// Package format defines the interface for metadata format plugins.
package format

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

const defaultMaxInputBytes = int64(64 << 20)

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
	// Profile is the legacy static format mapping to use. Instance-specific
	// system mappings belong in SystemProfile.
	Profile *mapping.Profile

	// SystemProfile is an immutable model-bound profile for a configured
	// repository system such as Drupal or Omeka S.
	SystemProfile *profile.Compiled

	// ValueProfile supplies executable value rules for a transformation whose
	// input transport is not the profiled system. A profile-bound Workbench CSV
	// uses this only for explicit profile_identifier columns; it does not claim
	// the CSV dataset originated from Drupal.
	ValueProfile *profile.Compiled

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

	// Spec is a direction-aware transformation specification. When present it
	// controls source header handling, ordered mappings, codecs, cardinality,
	// defaults, and target metadata independently of Profile.
	Spec *spec.Transformation
}

// SerializeOptions contains options for serialization.
type SerializeOptions struct {
	// Profile is the legacy static format mapping to use. Instance-specific
	// system mappings belong in SystemProfile.
	Profile *mapping.Profile

	// SystemProfile is the target system's immutable model-bound profile. A
	// source profile must never be reused here implicitly.
	SystemProfile *profile.Compiled

	// Columns specifies which columns to include (for tabular formats)
	Columns []string

	// MultiValueSeparator is the delimiter for multi-value fields
	MultiValueSeparator string

	// IncludeHeader includes a header row (for tabular formats)
	IncludeHeader bool

	// Pretty enables pretty-printing (for JSON/XML formats)
	Pretty bool

	// ReferenceDOIs are DOI references to attach to each serialized work when
	// the target format supports citation lists.
	ReferenceDOIs []string

	// ExtraWriters holds additional output writers for formats that produce
	// more than one output file. Keys are format-specific names.
	// Example: the islandora-workbench format writes an agents CSV to ExtraWriters["agents"].
	ExtraWriters map[string]io.Writer

	// Spec is the direction-aware target specification to apply.
	Spec *spec.Transformation

	// Operation limits target fields to a specific Workbench task.
	Operation spec.Operation
}

// Diagnostic identifies a precise input problem. Row and Column are one-based
// CSV coordinates, including header rows.
type Diagnostic struct {
	Source  string `json:"source,omitempty"`
	Row     int    `json:"row"`
	Column  int    `json:"column,omitempty"`
	Header  string `json:"header,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error formats a diagnostic for command-line and HTTP clients.
func (d Diagnostic) Error() string {
	location := ""
	if d.Source != "" {
		location = d.Source
	}
	if d.Row > 0 {
		if location != "" {
			location += ":"
		}
		location += "row " + strconv.Itoa(d.Row)
	}
	if d.Column > 0 {
		location += ", column " + strconv.Itoa(d.Column)
	}
	if d.Header != "" {
		location += " (" + d.Header + ")"
	}
	if location == "" {
		return d.Message
	}
	return location + ": " + d.Message
}

// DiagnosticsError aggregates all cell-level errors discovered in one parse.
// Parsers return no records with this error to prevent partial imports.
type DiagnosticsError struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Error implements error.
func (e *DiagnosticsError) Error() string {
	if e == nil || len(e.Diagnostics) == 0 {
		return "metadata validation failed"
	}
	const preview = 3
	count := len(e.Diagnostics)
	limit := count
	if limit > preview {
		limit = preview
	}
	parts := make([]string, 0, limit+1)
	for _, diagnostic := range e.Diagnostics[:limit] {
		parts = append(parts, diagnostic.Error())
	}
	if count > limit {
		parts = append(parts, strconv.Itoa(count-limit)+" more error(s)")
	}
	return strings.Join(parts, "; ")
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

// ReadInput reads a complete format document with Crosswalk's default input
// bound. Formats that need tighter limits or true streaming can impose those
// themselves; no parser should call io.ReadAll on an unbounded caller stream.
func ReadInput(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("input reader is required")
	}
	data, err := io.ReadAll(io.LimitReader(r, defaultMaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > defaultMaxInputBytes {
		return nil, fmt.Errorf("input exceeds %d bytes", defaultMaxInputBytes)
	}
	return data, nil
}
