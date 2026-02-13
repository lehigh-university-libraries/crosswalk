// Package schema provides optional schema registration for dynamic formats.
// Static formats can ignore this entirely - it's designed for formats like
// Drupal where the schema is configured per-instance.
package schema

import (
	"fmt"
)

// FieldType identifies the normalized kind of field.
type FieldType string

const (
	FieldText        FieldType = "text"
	FieldInt         FieldType = "int"
	FieldBool        FieldType = "bool"
	FieldDate        FieldType = "date"
	FieldRef         FieldType = "reference"
	FieldTypedRef    FieldType = "typed_reference"
	FieldLink        FieldType = "link"
	FieldComposite   FieldType = "composite"
	FieldFile        FieldType = "file"
	FieldImage       FieldType = "image"
	FieldRelatedItem FieldType = "related_item"
	FieldPartDetail  FieldType = "part_detail"
	FieldAttr        FieldType = "attr" // textfield_attr, textarea_attr
)

// Cardinality defines how many values a field can have.
type Cardinality int

const (
	Single    Cardinality = 1
	Unlimited Cardinality = -1
	// Specific numbers (2, 3, etc.) are also valid
)

// Field describes a field in a schema.
type Field struct {
	// Name is the field machine name (e.g., "field_author")
	Name string `yaml:"name" json:"name"`

	// Type is our normalized type for extraction
	Type FieldType `yaml:"type" json:"type"`

	// SourceType is the original type from the source system (e.g., "typed_relation", "edtf")
	// This is critical for knowing HOW to parse the field.
	SourceType string `yaml:"source_type,omitempty" json:"source_type,omitempty"`

	// Cardinality: 1 = single, -1 = unlimited, N = max N
	Cardinality Cardinality `yaml:"cardinality,omitempty" json:"cardinality,omitempty"`

	// Required indicates if the field must have a value
	Required bool `yaml:"required,omitempty" json:"required,omitempty"`

	// Label is the human-readable name
	Label string `yaml:"label,omitempty" json:"label,omitempty"`

	// Description provides documentation
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// SubFields for composite types
	SubFields []Field `yaml:"sub_fields,omitempty" json:"sub_fields,omitempty"`

	// RefType is the target entity type for reference fields
	RefType string `yaml:"ref_type,omitempty" json:"ref_type,omitempty"`

	// Settings holds additional field-specific settings
	Settings map[string]any `yaml:"settings,omitempty" json:"settings,omitempty"`
}

// IsMultiValue returns true if the field can have multiple values.
func (f Field) IsMultiValue() bool {
	return f.Cardinality != Single
}

// Entity describes an entity type (bundle, content type, vocabulary, etc.).
type Entity struct {
	// EntityType is the entity type (e.g., "node", "taxonomy_term", "media", "user")
	EntityType string `yaml:"entity_type" json:"entity_type"`

	// Bundle is the bundle machine name (e.g., "article", "tags", "image")
	// For users, this is typically "user"
	Bundle string `yaml:"bundle" json:"bundle"`

	// Name is the human-readable name
	Name string `yaml:"name" json:"name"`

	// Description provides documentation
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Fields defines the schema for this entity
	Fields []Field `yaml:"fields" json:"fields"`

	// fieldIndex is built lazily for fast lookups
	fieldIndex map[string]*Field
}

// GetField returns a field by name.
func (e *Entity) GetField(name string) (*Field, bool) {
	e.buildIndex()
	f, ok := e.fieldIndex[name]
	return f, ok
}

// HasField checks if a field exists.
func (e *Entity) HasField(name string) bool {
	_, ok := e.GetField(name)
	return ok
}

// FieldNames returns all field names.
func (e *Entity) FieldNames() []string {
	names := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		names[i] = f.Name
	}
	return names
}

// RequiredFields returns all required field names.
func (e *Entity) RequiredFields() []string {
	var result []string
	for _, f := range e.Fields {
		if f.Required {
			result = append(result, f.Name)
		}
	}
	return result
}

func (e *Entity) buildIndex() {
	if e.fieldIndex != nil {
		return
	}
	e.fieldIndex = make(map[string]*Field, len(e.Fields))
	for i := range e.Fields {
		e.fieldIndex[e.Fields[i].Name] = &e.Fields[i]
	}
}

// Validate checks if the given fields satisfy the schema.
// Returns a list of validation errors, or nil if valid.
func (e *Entity) Validate(hasField func(string) bool) []string {
	var errors []string
	for _, f := range e.Fields {
		if f.Required && !hasField(f.Name) {
			errors = append(errors, fmt.Sprintf("missing required field %q", f.Name))
		}
	}
	return errors
}
