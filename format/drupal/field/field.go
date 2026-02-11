// Package field provides type-aware field extraction for Drupal entities.
// It uses the SourceType from schema definitions to determine how to parse fields.
package field

import (
	"encoding/json"

	"github.com/lehigh-university-libraries/crosswalk/value"
)

// Handler extracts values from a Drupal field based on its SourceType.
type Handler interface {
	// Text extracts a single text value.
	Text(raw json.RawMessage) string
	// Texts extracts multiple text values.
	Texts(raw json.RawMessage) []string
	// Int extracts an integer value.
	Int(raw json.RawMessage) int
	// Bool extracts a boolean value.
	Bool(raw json.RawMessage) bool
	// Refs extracts entity references.
	Refs(raw json.RawMessage, opts ...value.RefOption) []value.Ref
	// TypedRefs extracts typed relations.
	TypedRefs(raw json.RawMessage, opts ...value.TypedRefOption) []value.TypedRef
	// Dates extracts date values.
	Dates(raw json.RawMessage) []value.Date
	// Links extracts link values.
	Links(raw json.RawMessage) []value.Link
	// FormattedText extracts formatted text with optional processed HTML.
	FormattedText(raw json.RawMessage, useProcessed bool) string
}

// GetHandler returns a Handler for the given Drupal source type.
// Source types include: string, string_long, text, text_long, text_with_summary,
// entity_reference, typed_relation, link, edtf, boolean, integer, list_string, etc.
func GetHandler(sourceType string) Handler {
	switch sourceType {
	case "typed_relation":
		return &TypedRelationHandler{}
	case "entity_reference":
		return &EntityRefHandler{}
	case "link":
		return &LinkHandler{}
	case "edtf":
		return &EDTFHandler{}
	case "text", "text_long", "text_with_summary":
		return &FormattedTextHandler{}
	default:
		return &GenericHandler{}
	}
}

// GenericHandler handles most Drupal fields with standard extraction.
type GenericHandler struct{}

func (h *GenericHandler) Text(raw json.RawMessage) string {
	return value.FromArrayText(raw)
}

func (h *GenericHandler) Texts(raw json.RawMessage) []string {
	return value.FromArrayTexts(raw)
}

func (h *GenericHandler) Int(raw json.RawMessage) int {
	return value.FromArrayInt(raw)
}

func (h *GenericHandler) Bool(raw json.RawMessage) bool {
	return value.FromArrayBool(raw)
}

func (h *GenericHandler) Refs(raw json.RawMessage, opts ...value.RefOption) []value.Ref {
	return value.FromArrayRefs(raw, opts...)
}

func (h *GenericHandler) TypedRefs(raw json.RawMessage, opts ...value.TypedRefOption) []value.TypedRef {
	return value.FromArrayTypedRefs(raw, opts...)
}

func (h *GenericHandler) Dates(raw json.RawMessage) []value.Date {
	return value.FromArrayDates(raw)
}

func (h *GenericHandler) Links(raw json.RawMessage) []value.Link {
	return value.FromArrayLinks(raw)
}

func (h *GenericHandler) FormattedText(raw json.RawMessage, useProcessed bool) string {
	return value.FormattedText(raw, useProcessed)
}

// FormattedTextHandler handles text, text_long, text_with_summary fields.
// These have "value", "format", and optionally "processed" keys.
type FormattedTextHandler struct {
	GenericHandler
}

func (h *FormattedTextHandler) Text(raw json.RawMessage) string {
	return value.FormattedText(raw, false)
}

// EntityRefHandler handles entity_reference fields.
type EntityRefHandler struct {
	GenericHandler
}

// TypedRelationHandler handles typed_relation fields.
// These have target_id, target_type, and rel_type keys.
type TypedRelationHandler struct {
	GenericHandler
}

// LinkHandler handles link fields.
// These have uri, title, and options keys.
type LinkHandler struct {
	GenericHandler
}

func (h *LinkHandler) Text(raw json.RawMessage) string {
	links := value.FromArrayLinks(raw)
	if len(links) == 0 {
		return ""
	}
	// Return title if available, otherwise URI
	if links[0].Title != "" {
		return links[0].Title
	}
	return links[0].URI
}

func (h *LinkHandler) Texts(raw json.RawMessage) []string {
	links := value.FromArrayLinks(raw)
	if len(links) == 0 {
		return nil
	}
	result := make([]string, 0, len(links))
	for _, link := range links {
		if link.Title != "" {
			result = append(result, link.Title)
		} else if link.URI != "" {
			result = append(result, link.URI)
		}
	}
	return result
}

// EDTFHandler handles EDTF date fields.
type EDTFHandler struct {
	GenericHandler
}

func (h *EDTFHandler) Dates(raw json.RawMessage) []value.Date {
	texts := value.FromArrayTexts(raw)
	if len(texts) == 0 {
		return nil
	}
	result := make([]value.Date, 0, len(texts))
	for _, s := range texts {
		if d, err := value.ParseEDTF(s); err == nil && !d.IsZero() {
			result = append(result, d)
		}
	}
	return result
}
