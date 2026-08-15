package spec

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// compileDrupalFieldValidations converts only constraints represented by the
// frozen Drupal model into deterministic, canonical-field validation rules.
// Deployment-aware entity and vocabulary existence checks deliberately remain
// outside this compiler; their handler metadata stays in the field settings.
func compileDrupalFieldValidations(config drupalAttachedField) []Validation {
	validations := make([]Validation, 0, 4)
	if provider := drupalAllowedValuesProvider(config); provider != "" {
		validations = append(validations, Validation{
			Rule: ValidationContextAllowedValue, Phase: ValidationPhaseContext, Provider: provider,
		})
	}
	if validation, ok := fieldMediaExtensionValidation(config); ok {
		validations = append(validations, validation)
	}

	if drupalMaximumLengthType(config.storage.Type) {
		if limit, ok := drupalPositiveInteger(config.storage.Settings["max_length"]); ok {
			validations = append(validations, Validation{Rule: ValidationMaximumRunes, Limit: limit})
		}
	}
	if values, ok := drupalAllowedValues(config); ok && len(values) > 0 {
		validations = append(validations, Validation{Rule: ValidationEnum, Values: values})
	}
	if config.storage.Type == "link" {
		// Workbench encodes link fields as an absolute URL followed by an
		// optional %% display label. This cell grammar is narrower than Drupal's
		// storage-level link mask and is the same for external-only and generic
		// link fields.
		validations = append(validations, Validation{Rule: ValidationWorkbenchLink})
	}
	switch config.storage.Type {
	case "geolocation":
		validations = append(validations, Validation{Rule: ValidationGeolocation})
	case "authority_link":
		// A missing source list is an invalid sealed authority-link contract;
		// ValidationAuthorityLink deliberately requires at least one source so
		// model compilation fails closed instead of reporting unchecked values.
		validations = append(validations, Validation{
			Rule: ValidationAuthorityLink, Values: drupalConfiguredStrings(config.field.Settings["authority_sources"]),
		})
	case "media_track":
		validations = append(validations, Validation{Rule: ValidationMediaTrack})
	case "entity_reference":
		entityType := drupalReferenceEntityType(config)
		bundles := drupalReferenceBundles(config)
		handler := drupalReferenceHandler(config, entityType)
		// Emit the complete contract even when target_type is absent. Sealing the
		// transformation then fails closed instead of silently omitting reference
		// validation for malformed model input.
		validations = append(validations,
			Validation{Rule: ValidationEntityReference, EntityType: entityType, Bundles: bundles},
			Validation{Rule: ValidationContextEntityExists, Phase: ValidationPhaseContext, EntityType: entityType, Bundles: bundles, Handler: handler},
		)
	case "typed_relation":
		entityType := drupalReferenceEntityType(config)
		bundles := drupalReferenceBundles(config)
		validations = append(validations, Validation{
			Rule: ValidationTypedRelation, Values: drupalConfiguredStrings(config.field.Settings["rel_types"]),
			EntityType: entityType, Bundles: bundles,
		})
		if entityType != "" {
			validations = append(validations, Validation{
				Rule: ValidationContextEntityExists, Phase: ValidationPhaseContext, EntityType: entityType, Bundles: bundles,
				Handler: drupalReferenceHandler(config, entityType),
			})
		}
	}
	if drupalNumericType(config.storage.Type) {
		minimum, hasMinimum := drupalNumericSetting(config, "min")
		maximum, hasMaximum := drupalNumericSetting(config, "max")
		if drupalTruthy(config.storage.Settings["unsigned"]) && (!hasMinimum || minimum < 0) {
			minimum, hasMinimum = 0, true
		}
		if hasMinimum || hasMaximum {
			validation := Validation{Rule: ValidationNumericRange}
			if hasMinimum {
				validation.Minimum = float64Pointer(minimum)
			}
			if hasMaximum {
				validation.Maximum = float64Pointer(maximum)
			}
			validations = append(validations, validation)
		} else if config.storage.Type == "decimal" || config.storage.Type == "float" {
			// Integer codecs already reject non-integers during parsing. Decimal
			// and float fields remain text in the Hub so their exact lexical value
			// survives a Workbench round trip; declare the finite-number check
			// explicitly even when Drupal has no min/max constraint.
			validations = append(validations, Validation{Rule: ValidationFiniteNumber})
		}
	}

	return validations
}

func drupalReferenceHandler(config drupalAttachedField, entityType string) string {
	handler := strings.TrimSpace(drupalString(config.field.Settings["handler"]))
	if handler == "" && entityType != "" {
		return "default:" + entityType
	}
	return handler
}

func drupalConfiguredStrings(raw any) []string {
	values, ok := decodeDrupalAllowedValues(raw)
	if !ok {
		return nil
	}
	sort.Strings(values)
	return compactSortedStrings(values)
}

func drupalReferenceBundles(config drupalAttachedField) []string {
	if config.reference != nil {
		bundles := append([]string(nil), config.reference.Bundles...)
		sort.Strings(bundles)
		return compactSortedStrings(bundles)
	}
	handler, ok := config.field.Settings["handler_settings"].(map[string]any)
	if !ok {
		return nil
	}
	return drupalConfiguredStrings(handler["target_bundles"])
}

func drupalReferenceEntityType(config drupalAttachedField) string {
	if config.reference != nil {
		return strings.TrimSpace(config.reference.EntityType)
	}
	return drupalString(config.storage.Settings["target_type"])
}

func mergeDrupalFieldValidations(existing, compiled []Validation) []Validation {
	if len(compiled) == 0 {
		return existing
	}
	merged := append([]Validation(nil), existing...)
	for _, validation := range compiled {
		replaced := false
		for index := range merged {
			if merged[index].Rule == validation.Rule {
				merged[index] = validation
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, validation)
		}
	}
	return merged
}

// drupalMaximumLengthType reports whether max_length constrains the scalar
// textual value represented by one Workbench cell. Structured fields are
// deliberately excluded: their encoded cell can contain attributes and other
// metadata that are not part of Drupal's value-length constraint.
func drupalMaximumLengthType(fieldType string) bool {
	switch fieldType {
	case "string", "string_long", "string_textfield",
		"text", "text_long", "text_with_summary",
		"list_string", "email", "telephone", "datetime", "daterange", "edtf":
		return true
	default:
		return false
	}
}

func drupalNumericType(fieldType string) bool {
	switch fieldType {
	case "integer", "list_integer", "decimal", "float":
		return true
	default:
		return false
	}
}

func drupalAllowedValues(config drupalAttachedField) ([]string, bool) {
	for _, settings := range []map[string]any{config.field.Settings, config.storage.Settings} {
		if value, exists := settings["allowed_values_function"]; exists && strings.TrimSpace(drupalString(value)) != "" {
			return nil, false
		}
	}

	var raw any
	found := false
	for _, settings := range []map[string]any{config.field.Settings, config.storage.Settings} {
		if value, exists := settings["allowed_values"]; exists {
			raw, found = value, true
			break
		}
	}
	if !found || raw == nil {
		return nil, false
	}

	values, ok := decodeDrupalAllowedValues(raw)
	if !ok || len(values) == 0 {
		return nil, false
	}
	sort.Strings(values)
	values = compactSortedStrings(values)
	return values, len(values) > 0
}

func drupalAllowedValuesProvider(config drupalAttachedField) string {
	for _, settings := range []map[string]any{config.field.Settings, config.storage.Settings} {
		if value := strings.TrimSpace(drupalString(settings["allowed_values_function"])); value != "" {
			return value
		}
	}
	return ""
}

func decodeDrupalAllowedValues(raw any) ([]string, bool) {
	switch typed := raw.(type) {
	case map[string]any:
		values := make([]string, 0, len(typed))
		for key := range typed {
			if key == "" {
				return nil, false
			}
			values = append(values, key)
		}
		return values, true
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := decodeDrupalAllowedValue(item)
			if !ok {
				return nil, false
			}
			values = append(values, text)
		}
		return values, true
	case []map[string]any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := decodeDrupalAllowedValue(item)
			if !ok {
				return nil, false
			}
			values = append(values, text)
		}
		return values, true
	default:
		return nil, false
	}
}

func decodeDrupalAllowedValue(item any) (string, bool) {
	if mapped, ok := item.(map[string]any); ok {
		var exists bool
		item, exists = mapped["value"]
		if !exists {
			return "", false
		}
	}
	text := drupalString(item)
	if text == "" {
		return "", false
	}
	return text, true
}

func drupalNumericSetting(config drupalAttachedField, key string) (float64, bool) {
	for _, settings := range []map[string]any{config.field.Settings, config.storage.Settings} {
		if value, exists := settings[key]; exists {
			return drupalFiniteNumber(value)
		}
	}
	return 0, false
}

func drupalPositiveInteger(value any) (int, bool) {
	number, ok := drupalFiniteNumber(value)
	if !ok || number <= 0 || math.Trunc(number) != number || number > float64(maxInt()) {
		return 0, false
	}
	return int(number), true
}

func drupalFiniteNumber(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case int:
		number = float64(typed)
	case int8:
		number = float64(typed)
	case int16:
		number = float64(typed)
	case int32:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case uint:
		number = float64(typed)
	case uint8:
		number = float64(typed)
	case uint16:
		number = float64(typed)
	case uint32:
		number = float64(typed)
	case uint64:
		number = float64(typed)
	case float32:
		number = float64(typed)
	case float64:
		number = typed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil || strings.TrimSpace(typed) == "" {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func drupalTruthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && parsed
	default:
		number, ok := drupalFiniteNumber(value)
		return ok && number != 0
	}
}

func drupalString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func compactSortedStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func float64Pointer(value float64) *float64 {
	return &value
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
