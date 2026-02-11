// Package drupal provides a format plugin for Drupal entity JSON.
package drupal

import (
	"encoding/json"

	"github.com/lehigh-university-libraries/crosswalk/value"
)

// DrupalEntity represents a raw Drupal JSON entity.
type DrupalEntity map[string]json.RawMessage

// FieldValue represents a single Drupal field value.
// Kept for backward compatibility; new code should use value package directly.
type FieldValue struct {
	// Plain value fields
	Value     any    `json:"value,omitempty"`
	Format    string `json:"format,omitempty"`
	Processed string `json:"processed,omitempty"`

	// Entity reference fields
	TargetID   any    `json:"target_id,omitempty"` // can be int or string
	TargetType string `json:"target_type,omitempty"`
	TargetUUID string `json:"target_uuid,omitempty"`
	TargetURL  string `json:"url,omitempty"`

	// Typed relation fields
	RelType string `json:"rel_type,omitempty"`

	// Link fields
	URI     string `json:"uri,omitempty"`
	Title   string `json:"title,omitempty"`
	Options any    `json:"options,omitempty"`

	// Enriched entity data (added by enricher)
	Entity json.RawMessage `json:"_entity,omitempty"`
}

// ExtractString extracts a string value from a Drupal field.
func ExtractString(raw json.RawMessage) (string, error) {
	return value.FromArrayText(raw), nil
}

// ExtractStrings extracts multiple string values from a Drupal field.
func ExtractStrings(raw json.RawMessage) ([]string, error) {
	return value.FromArrayTexts(raw), nil
}

// ExtractInt extracts an integer value from a Drupal field.
func ExtractInt(raw json.RawMessage) (int, error) {
	return value.FromArrayInt(raw), nil
}

// ExtractBool extracts a boolean value from a Drupal field.
func ExtractBool(raw json.RawMessage) (bool, error) {
	return value.FromArrayBool(raw), nil
}

// ExtractEntityRefs extracts entity reference values.
func ExtractEntityRefs(raw json.RawMessage) ([]FieldValue, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var arr []FieldValue
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// ExtractTypedRelations extracts typed relation values (entity refs with rel_type).
func ExtractTypedRelations(raw json.RawMessage) ([]FieldValue, error) {
	return ExtractEntityRefs(raw)
}

// ExtractLinks extracts link field values.
func ExtractLinks(raw json.RawMessage) ([]FieldValue, error) {
	return ExtractEntityRefs(raw)
}

// ExtractFormattedText extracts formatted text, optionally returning processed HTML.
func ExtractFormattedText(raw json.RawMessage, useProcessed bool) (string, error) {
	return value.FormattedText(raw, useProcessed), nil
}

// GetTargetID returns the target_id as a string.
func (fv *FieldValue) GetTargetID() string {
	return value.Text(fv.TargetID)
}

// GetResolvedName returns the resolved entity name from enriched data.
// Returns the name and true if found, or empty string and false if not enriched.
func (fv *FieldValue) GetResolvedName() (string, bool) {
	if len(fv.Entity) == 0 {
		return "", false
	}

	// Parse the enriched entity
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(fv.Entity, &entity); err != nil {
		return "", false
	}

	// Try "name" field (taxonomy terms, users)
	if nameRaw, ok := entity["name"]; ok {
		if name := value.FromArrayText(nameRaw); name != "" {
			return name, true
		}
	}

	// Try "title" field (nodes)
	if titleRaw, ok := entity["title"]; ok {
		if title := value.FromArrayText(titleRaw); title != "" {
			return title, true
		}
	}

	// Try "label" field (some entities)
	if labelRaw, ok := entity["label"]; ok {
		if label := value.FromArrayText(labelRaw); label != "" {
			return label, true
		}
	}

	return "", false
}

// AuthorityLink contains authority link data from Islandora taxonomy terms.
type AuthorityLink struct {
	URI    string // Authority URI (e.g., "http://vocab.getty.edu/page/aat/300028029")
	Title  string // Optional title
	Source string // Vocabulary source identifier (e.g., "aat", "lcsh", "lcnaf")
}

// GetAuthorityURI returns the authority link URI from enriched taxonomy data.
// This is specific to Islandora, where taxonomy terms have a field_authority_link
// field containing the canonical URI (e.g., Getty AAT, LCSH, etc.).
// Returns the URI and true if found, or empty string and false if not present.
func (fv *FieldValue) GetAuthorityURI() (string, bool) {
	link, ok := fv.GetAuthorityLink()
	if !ok {
		return "", false
	}
	return link.URI, true
}

// GetAuthorityLink returns the full authority link data from enriched taxonomy data.
// This is specific to Islandora, where taxonomy terms have a field_authority_link
// field containing the canonical URI, title, and vocabulary source.
// Returns the AuthorityLink and true if found, or empty AuthorityLink and false if not present.
func (fv *FieldValue) GetAuthorityLink() (AuthorityLink, bool) {
	if len(fv.Entity) == 0 {
		return AuthorityLink{}, false
	}

	// Parse the enriched entity
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(fv.Entity, &entity); err != nil {
		return AuthorityLink{}, false
	}

	// Look for field_authority_link (Islandora standard field for authority URIs)
	if linkRaw, ok := entity["field_authority_link"]; ok {
		// field_authority_link is an array: [{"uri": "...", "title": "...", "source": "..."}]
		var links []struct {
			URI    string `json:"uri"`
			Title  string `json:"title"`
			Source string `json:"source"`
		}
		if err := json.Unmarshal(linkRaw, &links); err == nil && len(links) > 0 && links[0].URI != "" {
			return AuthorityLink{
				URI:    links[0].URI,
				Title:  links[0].Title,
				Source: links[0].Source,
			}, true
		}
	}

	return AuthorityLink{}, false
}

// GetNodeModel returns the Islandora model name from enriched node data.
// For nodes, this extracts the model from field_model (e.g., "Collection", "Binary", "Image").
// Returns the model name and true if found, or empty string and false if not present.
func (fv *FieldValue) GetNodeModel() (string, bool) {
	if len(fv.Entity) == 0 {
		return "", false
	}

	// Parse the enriched entity
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(fv.Entity, &entity); err != nil {
		return "", false
	}

	// Look for field_model (Islandora model reference)
	if modelRaw, ok := entity["field_model"]; ok {
		// field_model is an entity reference array with _entity containing resolved term
		var models []struct {
			Entity json.RawMessage `json:"_entity"`
		}
		if err := json.Unmarshal(modelRaw, &models); err == nil && len(models) > 0 {
			// Extract name from the resolved taxonomy term
			var term map[string]json.RawMessage
			if err := json.Unmarshal(models[0].Entity, &term); err == nil {
				if nameRaw, ok := term["name"]; ok {
					if name := value.FromArrayText(nameRaw); name != "" {
						return name, true
					}
				}
			}
		}
	}

	return "", false
}

// GetModelExternalURI returns the external URI (e.g., schema.org type) from enriched node data.
// Islandora model taxonomy terms store their schema.org type in field_external_uri.
// For example, "Digital Document" has field_external_uri = "https://schema.org/DigitalDocument".
// Returns the URI and true if found, or empty string and false if not present.
func (fv *FieldValue) GetModelExternalURI() (string, bool) {
	if len(fv.Entity) == 0 {
		return "", false
	}

	// Parse the enriched entity
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(fv.Entity, &entity); err != nil {
		return "", false
	}

	// Look for field_model (Islandora model reference)
	if modelRaw, ok := entity["field_model"]; ok {
		// field_model is an entity reference array with _entity containing resolved term
		var models []struct {
			Entity json.RawMessage `json:"_entity"`
		}
		if err := json.Unmarshal(modelRaw, &models); err == nil && len(models) > 0 {
			// Extract field_external_uri from the resolved taxonomy term
			var term map[string]json.RawMessage
			if err := json.Unmarshal(models[0].Entity, &term); err == nil {
				if uriRaw, ok := term["field_external_uri"]; ok {
					// field_external_uri is a link field array: [{"uri": "..."}]
					var links []struct {
						URI string `json:"uri"`
					}
					if err := json.Unmarshal(uriRaw, &links); err == nil && len(links) > 0 && links[0].URI != "" {
						return links[0].URI, true
					}
				}
			}
		}
	}

	return "", false
}

// GetNodeResourceType returns the content type/bundle from enriched node data.
// Returns the bundle name (e.g., "islandora_object", "article") and true if found.
func (fv *FieldValue) GetNodeResourceType() (string, bool) {
	if len(fv.Entity) == 0 {
		return "", false
	}

	// Parse the enriched entity
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(fv.Entity, &entity); err != nil {
		return "", false
	}

	// Look for type field (Drupal bundle/content type)
	if typeRaw, ok := entity["type"]; ok {
		var types []struct {
			TargetID string `json:"target_id"`
		}
		if err := json.Unmarshal(typeRaw, &types); err == nil && len(types) > 0 {
			return types[0].TargetID, true
		}
	}

	return "", false
}

// ToRef converts a FieldValue to a value.Ref.
func (fv *FieldValue) ToRef() value.Ref {
	return value.Ref{
		ID:   value.Text(fv.TargetID),
		Type: fv.TargetType,
		UUID: fv.TargetUUID,
	}
}

// ToTypedRef converts a FieldValue to a value.TypedRef.
func (fv *FieldValue) ToTypedRef() value.TypedRef {
	return value.TypedRef{
		Ref:     fv.ToRef(),
		RelType: fv.RelType,
	}
}

// ToLink converts a FieldValue to a value.Link.
func (fv *FieldValue) ToLink() value.Link {
	var opts map[string]any
	if o, ok := fv.Options.(map[string]any); ok {
		opts = o
	}
	return value.Link{
		URI:     fv.URI,
		Title:   fv.Title,
		Options: opts,
	}
}

// AsRefs extracts entity references using the value package.
func AsRefs(raw json.RawMessage, opts ...value.RefOption) []value.Ref {
	return value.FromArrayRefs(raw, opts...)
}

// AsTypedRefs extracts typed relations using the value package.
func AsTypedRefs(raw json.RawMessage, opts ...value.TypedRefOption) []value.TypedRef {
	return value.FromArrayTypedRefs(raw, opts...)
}

// AsLinks extracts links using the value package.
func AsLinks(raw json.RawMessage) []value.Link {
	return value.FromArrayLinks(raw)
}

// AsDates extracts dates using the value package.
func AsDates(raw json.RawMessage) []value.Date {
	return value.FromArrayDates(raw)
}
