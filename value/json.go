package value

import (
	"encoding/json"
)

// FromJSON unmarshals JSON into a value and extracts using the given function.
func FromJSON[T any](raw json.RawMessage, extract func(T) any) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return extract(v)
}

// FromArray extracts the first value from a Drupal-style array field.
// Drupal fields are typically: [{"value": "..."}, ...] or [{"target_id": ...}, ...]
func FromArray(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	// Try array of objects first
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		if len(arr) == 0 {
			return nil
		}
		// Return the first item's primary value
		first := arr[0]
		if v, ok := first["value"]; ok {
			return v
		}
		if v, ok := first["target_id"]; ok {
			return v
		}
		if v, ok := first["uri"]; ok {
			return v
		}
		// Return the whole object if no known key
		return first
	}

	// Try single object
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		if v, ok := obj["value"]; ok {
			return v
		}
		if v, ok := obj["target_id"]; ok {
			return v
		}
		return obj
	}

	// Try plain value
	var plain any
	if err := json.Unmarshal(raw, &plain); err == nil {
		return plain
	}

	return nil
}

// FromArrayAll extracts all values from a Drupal-style array field.
func FromArrayAll(raw json.RawMessage) []any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	// Try array of objects
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		if len(arr) == 0 {
			return nil
		}
		result := make([]any, 0, len(arr))
		for _, item := range arr {
			if v, ok := item["value"]; ok {
				result = append(result, v)
			} else if v, ok := item["target_id"]; ok {
				result = append(result, v)
			} else if v, ok := item["uri"]; ok {
				result = append(result, v)
			} else {
				result = append(result, item)
			}
		}
		return result
	}

	// Try single value
	if v := FromArray(raw); v != nil {
		return []any{v}
	}

	return nil
}

// FromArrayMaps extracts all items from a Drupal-style array as maps.
// This preserves the full structure for complex fields.
func FromArrayMaps(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}

	// Try single object
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		return []map[string]any{obj}
	}

	return nil
}

// FromArrayText extracts a single text value from a Drupal field.
func FromArrayText(raw json.RawMessage, opts ...TextOption) string {
	return Text(FromArray(raw))
}

// FromArrayTexts extracts all text values from a Drupal field.
func FromArrayTexts(raw json.RawMessage, opts ...TextOption) []string {
	values := FromArrayAll(raw)
	if len(values) == 0 {
		return nil
	}
	return TextSlice(values, opts...)
}

// FromArrayInt extracts a single int value from a Drupal field.
func FromArrayInt(raw json.RawMessage) int {
	return Int(FromArray(raw))
}

// FromArrayBool extracts a single bool value from a Drupal field.
func FromArrayBool(raw json.RawMessage) bool {
	return Bool(FromArray(raw))
}

// FromArrayRefs extracts entity references from a Drupal field.
func FromArrayRefs(raw json.RawMessage, opts ...RefOption) []Ref {
	maps := FromArrayMaps(raw)
	if len(maps) == 0 {
		return nil
	}

	result := make([]Ref, 0, len(maps))
	for _, m := range maps {
		if ref := RefFromMap(m, opts...); !ref.IsZero() {
			result = append(result, ref)
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// FromArrayTypedRefs extracts typed relations from a Drupal field.
func FromArrayTypedRefs(raw json.RawMessage, opts ...TypedRefOption) []TypedRef {
	maps := FromArrayMaps(raw)
	if len(maps) == 0 {
		return nil
	}

	result := make([]TypedRef, 0, len(maps))
	for _, m := range maps {
		if ref := TypedRefFromMap(m, opts...); !ref.IsZero() {
			result = append(result, ref)
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// FromArrayDates extracts dates from a Drupal field.
func FromArrayDates(raw json.RawMessage) []Date {
	texts := FromArrayTexts(raw)
	if len(texts) == 0 {
		return nil
	}

	result := make([]Date, 0, len(texts))
	for _, s := range texts {
		if d, err := ParseDate(s); err == nil && !d.IsZero() {
			result = append(result, d)
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// FormattedText extracts formatted text from a Drupal field.
// If useProcessed is true, returns the "processed" field (rendered HTML).
// Otherwise returns the raw "value" field.
func FormattedText(raw json.RawMessage, useProcessed bool) string {
	maps := FromArrayMaps(raw)
	if len(maps) == 0 {
		return ""
	}

	first := maps[0]
	if useProcessed {
		if v, ok := first["processed"]; ok {
			return Text(v)
		}
	}
	if v, ok := first["value"]; ok {
		return Text(v)
	}
	return ""
}

// Link represents a Drupal link field value.
type Link struct {
	URI     string
	Title   string
	Options map[string]any
}

// FromArrayLinks extracts link field values.
func FromArrayLinks(raw json.RawMessage) []Link {
	maps := FromArrayMaps(raw)
	if len(maps) == 0 {
		return nil
	}

	result := make([]Link, 0, len(maps))
	for _, m := range maps {
		link := Link{}
		if v, ok := m["uri"]; ok {
			link.URI = Text(v)
		}
		if v, ok := m["title"]; ok {
			link.Title = Text(v)
		}
		if v, ok := m["options"]; ok {
			if opts, ok := v.(map[string]any); ok {
				link.Options = opts
			}
		}
		if link.URI != "" {
			result = append(result, link)
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
