package schema

import (
	"encoding/json"

	"github.com/lehigh-university-libraries/crosswalk/value"
)

// DynamicEntity represents an entity where some fields are known (core)
// and others are dynamic (bundle-specific).
type DynamicEntity struct {
	// EntityType is the type (e.g., "node", "taxonomy_term")
	EntityType string

	// Bundle is the bundle name (e.g., "article", "tags")
	Bundle string

	// Fields holds dynamic fields as raw JSON for lazy decoding
	Fields map[string]json.RawMessage

	// Registry is optional - provides field type info for extraction
	registry *Registry
}

// NewDynamicEntity creates a new dynamic entity.
func NewDynamicEntity(entityType, bundle string) *DynamicEntity {
	return &DynamicEntity{
		EntityType: entityType,
		Bundle:     bundle,
		Fields:     make(map[string]json.RawMessage),
	}
}

// SetRegistry attaches a schema registry for field type lookups.
func (e *DynamicEntity) SetRegistry(r *Registry) {
	e.registry = r
}

// Registry returns the attached schema registry.
func (e *DynamicEntity) Registry() *Registry {
	return e.registry
}

// HasField checks if a field exists.
func (e *DynamicEntity) HasField(name string) bool {
	_, ok := e.Fields[name]
	return ok
}

// FieldNames returns all field names.
func (e *DynamicEntity) FieldNames() []string {
	names := make([]string, 0, len(e.Fields))
	for name := range e.Fields {
		names = append(names, name)
	}
	return names
}

// Raw returns the raw JSON for a field.
func (e *DynamicEntity) Raw(name string) json.RawMessage {
	return e.Fields[name]
}

// GetFieldSchema returns the schema field definition if a registry is attached.
func (e *DynamicEntity) GetFieldSchema(name string) (*Field, bool) {
	if e.registry == nil {
		return nil, false
	}
	return e.registry.GetField(e.EntityType, e.Bundle, name)
}

// GetSourceType returns the source type for a field from the registry.
func (e *DynamicEntity) GetSourceType(name string) string {
	if e.registry == nil {
		return ""
	}
	if st, ok := e.registry.GetSourceType(e.EntityType, e.Bundle, name); ok {
		return st
	}
	return ""
}

// Validate checks required fields against the schema.
func (e *DynamicEntity) Validate() []string {
	if e.registry == nil {
		return nil
	}
	return e.registry.Validate(e.EntityType, e.Bundle, e.HasField)
}

// =============================================================================
// FIELD EXTRACTION (uses value package primitives)
// =============================================================================

// GetText extracts a field as text.
func (e *DynamicEntity) GetText(name string) string {
	raw := e.Fields[name]
	if len(raw) == 0 {
		return ""
	}
	return value.FromArrayText(raw)
}

// GetTexts extracts a field as text slice.
func (e *DynamicEntity) GetTexts(name string, opts ...value.TextOption) []string {
	raw := e.Fields[name]
	if len(raw) == 0 {
		return nil
	}
	return value.FromArrayTexts(raw, opts...)
}

// GetInt extracts a field as int.
func (e *DynamicEntity) GetInt(name string) int {
	raw := e.Fields[name]
	if len(raw) == 0 {
		return 0
	}
	return value.FromArrayInt(raw)
}

// GetBool extracts a field as bool.
func (e *DynamicEntity) GetBool(name string) bool {
	raw := e.Fields[name]
	if len(raw) == 0 {
		return false
	}
	return value.FromArrayBool(raw)
}

// GetRefs extracts a field as entity references.
func (e *DynamicEntity) GetRefs(name string, opts ...value.RefOption) []value.Ref {
	raw := e.Fields[name]
	if len(raw) == 0 {
		return nil
	}
	return value.FromArrayRefs(raw, opts...)
}

// GetTypedRefs extracts a field as typed references.
func (e *DynamicEntity) GetTypedRefs(name string, opts ...value.TypedRefOption) []value.TypedRef {
	raw := e.Fields[name]
	if len(raw) == 0 {
		return nil
	}
	return value.FromArrayTypedRefs(raw, opts...)
}

// GetDates extracts a field as dates.
func (e *DynamicEntity) GetDates(name string) []value.Date {
	raw := e.Fields[name]
	if len(raw) == 0 {
		return nil
	}
	return value.FromArrayDates(raw)
}

// GetLinks extracts a field as links.
func (e *DynamicEntity) GetLinks(name string) []value.Link {
	raw := e.Fields[name]
	if len(raw) == 0 {
		return nil
	}
	return value.FromArrayLinks(raw)
}

// GetFormattedText extracts formatted text (with optional processed HTML).
func (e *DynamicEntity) GetFormattedText(name string, useProcessed bool) string {
	raw := e.Fields[name]
	if len(raw) == 0 {
		return ""
	}
	return value.FormattedText(raw, useProcessed)
}

// Get extracts a field using the registry to determine type.
// Falls back to generic extraction if no registry or unknown field.
func (e *DynamicEntity) Get(name string) any {
	raw := e.Fields[name]
	if len(raw) == 0 {
		return nil
	}

	// Use registry for type-aware extraction
	if e.registry != nil {
		if field, ok := e.registry.GetField(e.EntityType, e.Bundle, name); ok {
			return e.extractByType(raw, field)
		}
	}

	// Fallback: generic extraction
	return value.FromArray(raw)
}

func (e *DynamicEntity) extractByType(raw json.RawMessage, field *Field) any {
	switch field.Type {
	case FieldText:
		if field.IsMultiValue() {
			return value.FromArrayTexts(raw)
		}
		return value.FromArrayText(raw)
	case FieldInt:
		return value.FromArrayInt(raw)
	case FieldBool:
		return value.FromArrayBool(raw)
	case FieldDate:
		return value.FromArrayDates(raw)
	case FieldRef:
		return value.FromArrayRefs(raw)
	case FieldTypedRef:
		return value.FromArrayTypedRefs(raw)
	case FieldLink:
		return value.FromArrayLinks(raw)
	default:
		return value.FromArray(raw)
	}
}
