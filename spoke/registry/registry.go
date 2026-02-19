// Package registry provides a global registry for spoke field metadata.
// Generated spoke _meta.go files register themselves here via init().
package registry

import (
	"github.com/lehigh-university-libraries/crosswalk/mapping"
)

// FieldMeta contains metadata about a field for parsing and Hub mapping.
// This is the shared type used by all generated spoke _meta.go files.
type FieldMeta struct {
	// Proto field info
	ProtoField string // Proto field name (e.g., "linked_agent")

	// Source info
	DrupalField  string // Original Drupal field name (e.g., "field_linked_agent")
	DrupalType   string // Drupal field type (e.g., "typed_relation", "edtf", "entity_reference")
	Cardinality  int    // -1 = unlimited, 1 = single, N = max N values
	TargetType   string // For entity_reference: target entity type (e.g., "taxonomy_term", "node")
	TargetBundle string // For entity_reference: target bundle if restricted
	RDFPredicate string // RDF predicate from Drupal RDF mapping (e.g., "dcterms:issued")

	// Hub mapping info
	HubField string // Hub schema field (e.g., "Contributors", "Dates", "Extra.model")
	HubType  string // Hub field subtype (e.g., "issued" for date type, "doi" for identifier type)
	Parser   string // Parser to use (e.g., "edtf")
}

var registered = map[string]map[string]FieldMeta{}

// Register associates a field registry with a format name.
// Called from init() in generated spoke _meta.go files.
func Register(format string, fields map[string]FieldMeta) {
	registered[format] = fields
}

// ProfileFrom returns a mapping.Profile built from the registered spoke for the given format.
// Returns (nil, false) if no spoke is registered for the format.
func ProfileFrom(format string) (*mapping.Profile, bool) {
	fields, ok := registered[format]
	if !ok {
		return nil, false
	}
	return buildProfile(format, fields), true
}

// buildProfile converts a spoke FieldRegistry to a mapping.Profile.
func buildProfile(format string, fields map[string]FieldMeta) *mapping.Profile {
	p := &mapping.Profile{
		Name:   format,
		Format: format,
		Fields: make(map[string]mapping.FieldMapping),
		Options: mapping.ProfileOptions{
			MultiValueSeparator: "|",
			StripHTML:           true,
			TaxonomyMode:        "passthrough",
		},
	}

	for _, meta := range fields {
		if meta.DrupalField == "" || meta.HubField == "" {
			continue
		}

		fm := mapping.FieldMapping{
			IR:         meta.HubField,
			MultiValue: meta.Cardinality == -1 || meta.Cardinality > 1,
			Parser:     meta.Parser,
		}

		// Generated Drupal RDF mappings frequently map dcterms:type-backed
		// field_genre terms to ResourceType. In the Islandora data model this
		// field should populate Genre, while field_resource_type should drive
		// Hub ResourceType. If both map to ResourceType they race/override.
		if meta.DrupalField == "field_genre" && meta.TargetBundle == "genre" {
			fm.IR = "Genre"
		}

		// Set Drupal type — for typed_relation this is the primary type signal
		fm.Type = meta.DrupalType

		// Map HubType to the appropriate subtype field based on target IR
		if meta.HubType != "" {
			switch meta.HubField {
			case "Dates":
				fm.DateType = meta.HubType
			case "Relations":
				fm.RelationType = meta.HubType
			case "Identifiers":
				// HubType for identifiers (e.g., "pid", "isbn") stored in Type
				// to match how islandora.yaml uses the type field for identifier subtypes
				fm.Type = meta.HubType
			case "Subjects":
				fm.Vocabulary = meta.HubType
			}
		}

		// Set resolve for entity references
		if meta.TargetType != "" {
			fm.Resolve = meta.TargetType
		}

		// typed_relation fields also need role_field set
		if meta.DrupalType == "typed_relation" {
			fm.RoleField = "rel_type"
		}

		p.Fields[meta.DrupalField] = fm
	}

	// Always include the title field
	if _, ok := p.Fields["title"]; !ok {
		p.Fields["title"] = mapping.FieldMapping{
			IR: "Title",
		}
	}

	return p
}
