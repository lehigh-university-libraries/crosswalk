// Package mapping provides configuration types for field mappings between formats.
package mapping

// Profile represents a complete mapping configuration for a specific format/system.
type Profile struct {
	// Name is the profile identifier
	Name string `yaml:"name" json:"name"`

	// Format is the source format (e.g., "drupal", "csv", "schemaorg")
	Format string `yaml:"format" json:"format"`

	// Version is the schema/format version this profile targets (e.g., "29.4" for schema.org)
	// This is explicit and required for schemas with breaking changes between versions.
	Version string `yaml:"version,omitempty" json:"version,omitempty"`

	// SchemaURL is an optional URL to the schema specification
	SchemaURL string `yaml:"schema_url,omitempty" json:"schema_url,omitempty"`

	// Description provides human-readable documentation
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Fields maps source field names to IR field configurations
	Fields map[string]FieldMapping `yaml:"fields" json:"fields"`

	// Options contains format-specific options
	Options ProfileOptions `yaml:"options,omitempty" json:"options,omitempty"`
}

// VersionedName returns the profile name with version (e.g., "schemaorg@29.4")
func (p *Profile) VersionedName() string {
	if p.Version != "" {
		return p.Name + "@" + p.Version
	}
	return p.Name
}

// FieldMapping describes how a source field maps to an IR field.
type FieldMapping struct {
	// IR is the target IR field name (e.g., "Title", "Contributors", "Extra.nid")
	IR string `yaml:"ir" json:"ir"`

	// Type is the Drupal field type hint (e.g., "typed_relation", "entity_reference", "uri")
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// Priority determines which field wins when multiple map to same IR field (higher wins)
	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`

	// DateType specifies the semantic date type for date fields
	DateType string `yaml:"date_type,omitempty" json:"date_type,omitempty"`

	// Parser specifies a special parser to use (e.g., "edtf")
	Parser string `yaml:"parser,omitempty" json:"parser,omitempty"`

	// Resolve indicates the entity type to resolve (e.g., "taxonomy_term", "node")
	Resolve string `yaml:"resolve,omitempty" json:"resolve,omitempty"`

	// RoleField is the field containing role information for typed relations
	RoleField string `yaml:"role_field,omitempty" json:"role_field,omitempty"`

	// RelationType specifies the relation type for relation fields
	RelationType string `yaml:"relation_type,omitempty" json:"relation_type,omitempty"`

	// Vocabulary specifies the vocabulary for subject fields
	Vocabulary string `yaml:"vocabulary,omitempty" json:"vocabulary,omitempty"`

	// Transform specifies a transformation to apply (e.g., "strip_html", "lowercase")
	Transform string `yaml:"transform,omitempty" json:"transform,omitempty"`

	// Default is a default value if the source field is empty
	Default string `yaml:"default,omitempty" json:"default,omitempty"`

	// Required indicates the field is required (for validation)
	Required bool `yaml:"required,omitempty" json:"required,omitempty"`

	// MultiValue indicates the field can have multiple values
	MultiValue bool `yaml:"multi_value,omitempty" json:"multi_value,omitempty"`

	// Delimiter for multi-value fields in CSV format
	Delimiter string `yaml:"delimiter,omitempty" json:"delimiter,omitempty"`
}

// ProfileOptions contains format-specific configuration options.
type ProfileOptions struct {
	// TaxonomyFile is the path to a taxonomy term resolution file
	TaxonomyFile string `yaml:"taxonomy_file,omitempty" json:"taxonomy_file,omitempty"`

	// TaxonomyMode specifies how to handle taxonomy references
	// "resolve" = lookup names, "passthrough" = keep IDs, "api" = use Drupal API
	TaxonomyMode string `yaml:"taxonomy_mode,omitempty" json:"taxonomy_mode,omitempty"`

	// CSVDelimiter is the CSV field delimiter
	CSVDelimiter string `yaml:"csv_delimiter,omitempty" json:"csv_delimiter,omitempty"`

	// MultiValueSeparator is the delimiter for multi-value fields in CSV
	MultiValueSeparator string `yaml:"multi_value_separator,omitempty" json:"multi_value_separator,omitempty"`

	// IncludeEmpty includes empty fields in output
	IncludeEmpty bool `yaml:"include_empty,omitempty" json:"include_empty,omitempty"`

	// StripHTML strips HTML from text fields
	StripHTML bool `yaml:"strip_html,omitempty" json:"strip_html,omitempty"`
}

// GetMultiValueSeparator returns the multi-value separator with a default.
func (p *Profile) GetMultiValueSeparator() string {
	if p.Options.MultiValueSeparator != "" {
		return p.Options.MultiValueSeparator
	}
	return "|"
}

// GetCSVDelimiter returns the CSV delimiter with a default.
func (p *Profile) GetCSVDelimiter() string {
	if p.Options.CSVDelimiter != "" {
		return p.Options.CSVDelimiter
	}
	return ","
}

// CSVProfile represents a CSV-specific mapping configuration.
type CSVProfile struct {
	Profile `yaml:",inline"`

	// Columns maps CSV column names to IR fields
	Columns map[string]string `yaml:"columns,omitempty" json:"columns,omitempty"`

	// ColumnOrder specifies the output column order
	ColumnOrder []string `yaml:"column_order,omitempty" json:"column_order,omitempty"`
}

// DefaultCSVColumns returns the default column set for CSV output.
func DefaultCSVColumns() []string {
	return []string{
		"title",
		"contributors",
		"date_issued",
		"date_created",
		"resource_type",
		"genre",
		"language",
		"rights",
		"abstract",
		"description",
		"identifiers",
		"subjects",
		"keywords",
		"publisher",
		"place_published",
		"member_of",
		"degree_name",
		"degree_level",
		"department",
		"notes",
		"nid",
		"uuid",
	}
}

// IRFieldName extracts the base IR field name from a mapping.
// Handles dotted notation like "Extra.nid" or "DegreeInfo.Department".
func IRFieldName(ir string) (base string, subfield string) {
	parts := splitFirst(ir, ".")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return ir, ""
}

func splitFirst(s, sep string) []string {
	idx := 0
	for i, c := range s {
		if string(c) == sep {
			return []string{s[:i], s[i+1:]}
		}
		idx = i
	}
	_ = idx
	return []string{s}
}
