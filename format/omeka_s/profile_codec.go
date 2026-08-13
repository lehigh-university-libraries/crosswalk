package omeka_s

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	maxOmekaProfileValueBytes = 1 << 20
	maxExactJSONInteger       = int64(1<<53 - 1)
)

// decodeOmekaProfileValues executes a profile codec before Hub dispatch. The
// normalized values retain the established Omeka semantic mapping behavior;
// extras are separately represented with protobuf-safe scalar types or a
// tagged, lossless JSON shape for composite/opaque values.
func decodeOmekaProfileValues(values []valueObject, codec, hubBase string) ([]valueObject, []any, error) {
	normalized := make([]valueObject, 0, len(values))
	extras := make([]any, 0, len(values))
	for index, value := range values {
		converted, extra, err := decodeOmekaProfileValue(value, codec, hubBase)
		if err != nil {
			return nil, nil, fmt.Errorf("%s codec value %d: %w", codec, index+1, err)
		}
		normalized = append(normalized, converted)
		if extra != nil {
			extras = append(extras, extra)
		}
	}
	return normalized, extras, nil
}

func decodeOmekaProfileValue(value valueObject, codec, hubBase string) (valueObject, any, error) {
	switch codec {
	case "text", "date":
		text, err := omekaProfileText(value)
		if err != nil {
			return valueObject{}, nil, err
		}
		return omekaProfileLiteral(value, text), text, nil
	case "integer":
		scalar, err := omekaProfileScalar(value)
		if err != nil {
			return valueObject{}, nil, err
		}
		integer, err := strconv.ParseInt(strings.TrimSpace(scalarText(scalar)), 10, 64)
		if err != nil {
			return valueObject{}, nil, fmt.Errorf("expected a base-10 integer")
		}
		if integer < -maxExactJSONInteger || integer > maxExactJSONInteger {
			return valueObject{}, nil, fmt.Errorf("integer %d exceeds the exact JSON range [%d,%d]", integer, -maxExactJSONInteger, maxExactJSONInteger)
		}
		canonical := strconv.FormatInt(integer, 10)
		return omekaProfileLiteral(value, canonical), float64(integer), nil
	case "decimal":
		scalar, err := omekaProfileScalar(value)
		if err != nil {
			return valueObject{}, nil, err
		}
		decimal, err := strconv.ParseFloat(strings.TrimSpace(scalarText(scalar)), 64)
		if err != nil || math.IsNaN(decimal) || math.IsInf(decimal, 0) {
			return valueObject{}, nil, fmt.Errorf("expected a finite decimal")
		}
		canonical := strconv.FormatFloat(decimal, 'g', -1, 64)
		return omekaProfileLiteral(value, canonical), decimal, nil
	case "boolean":
		scalar, err := omekaProfileScalar(value)
		if err != nil {
			return valueObject{}, nil, err
		}
		boolean, err := omekaProfileBoolean(scalar)
		if err != nil {
			return valueObject{}, nil, err
		}
		canonical := strconv.FormatBool(boolean)
		return omekaProfileLiteral(value, canonical), boolean, nil
	case "link":
		candidate := value.uri
		if candidate == "" {
			scalar, err := omekaProfileScalar(value)
			if err != nil {
				return valueObject{}, nil, err
			}
			candidate = scalarText(scalar)
		}
		uri, err := absoluteURI(candidate)
		if err != nil {
			return valueObject{}, nil, err
		}
		value.typeName, value.uri = "uri", uri
		return value, uri, nil
	case "reference", "typed-relation":
		if isOmekaResourceValue(value) {
			return value, omekaProfileReference(value), nil
		}
		if value.typeName == "uri" {
			return value, value.uri, nil
		}
		text, err := omekaProfileText(value)
		if err != nil {
			return valueObject{}, nil, err
		}
		return omekaProfileLiteral(value, text), text, nil
	case "file":
		candidate := value.uri
		if candidate == "" {
			text, err := omekaProfileText(value)
			if err != nil {
				return valueObject{}, nil, err
			}
			candidate = text
		}
		uri, err := absoluteURI(candidate)
		if err != nil {
			return valueObject{}, nil, err
		}
		value.typeName, value.uri = "uri", uri
		return value, uri, nil
	case "typed-identifier":
		if isOmekaResourceValue(value) {
			return value, omekaProfileReference(value), nil
		}
		if value.typeName == "uri" {
			return value, value.uri, nil
		}
		text, err := omekaProfileText(value)
		if err != nil {
			return valueObject{}, nil, err
		}
		return omekaProfileLiteral(value, text), text, nil
	case "composite":
		if hubBase != "Extra" {
			if !isOmekaBuiltInValue(value) {
				return valueObject{}, nil, fmt.Errorf("module value type %q cannot be projected into Hub %s; map it to Extra or choose an explicit scalar codec", value.typeName, hubBase)
			}
			return value, omekaProfileDefaultExtra(value), nil
		}
		preserved, err := preserveOmekaProfileValue(value)
		return value, preserved, err
	case "opaque":
		if hubBase != "Extra" {
			return valueObject{}, nil, fmt.Errorf("opaque values can only be mapped to Hub Extra, not %s", hubBase)
		}
		preserved, err := preserveOmekaProfileValue(value)
		return value, preserved, err
	default:
		return valueObject{}, nil, fmt.Errorf("unsupported codec %q", codec)
	}
}

func omekaProfileScalar(value valueObject) (any, error) {
	switch value.typeName {
	case "literal":
		return value.literal, nil
	case "uri":
		return value.uri, nil
	case "resource", "resource:item", "resource:itemset", "resource:media":
		return displayValue(value), nil
	}
	if len(value.raw) == 0 {
		return nil, fmt.Errorf("module value type %q has no preserved JSON value", value.typeName)
	}
	if len(value.raw) > maxOmekaProfileValueBytes {
		return nil, fmt.Errorf("value exceeds the %d-byte codec limit", maxOmekaProfileValueBytes)
	}
	object, err := decodeObject(value.raw)
	if err != nil {
		return nil, fmt.Errorf("decoding module value: %w", err)
	}
	for _, key := range []string{"@value", "value", "o:value"} {
		raw, exists := object[key]
		if !exists {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var scalar any
		if err := decoder.Decode(&scalar); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		switch scalar.(type) {
		case string, json.Number, bool:
			return scalar, nil
		default:
			return nil, fmt.Errorf("%s must be a string, number, or boolean", key)
		}
	}
	return nil, fmt.Errorf("module value type %q has no @value, value, or o:value scalar", value.typeName)
}

func omekaProfileText(value valueObject) (string, error) {
	if display := displayValue(value); display != "" {
		return display, nil
	}
	scalar, err := omekaProfileScalar(value)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(scalarText(scalar))
	if text == "" {
		return "", fmt.Errorf("value is empty")
	}
	return text, nil
}

func omekaProfileBoolean(value any) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case json.Number:
		switch typed.String() {
		case "0":
			return false, nil
		case "1":
			return true, nil
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "false", "0":
			return false, nil
		case "true", "1":
			return true, nil
		}
	}
	return false, fmt.Errorf("expected true, false, 1, or 0")
}

func omekaProfileLiteral(source valueObject, text string) valueObject {
	return valueObject{
		typeName: "literal", literal: text, propertyID: source.propertyID,
		propertyLabel: source.propertyLabel, isPublic: source.isPublic,
	}
}

func preserveOmekaProfileValue(value valueObject) (any, error) {
	if len(value.raw) == 0 {
		return nil, fmt.Errorf("value type %q has no preserved JSON value", value.typeName)
	}
	if len(value.raw) > maxOmekaProfileValueBytes {
		return nil, fmt.Errorf("value exceeds the %d-byte preservation limit", maxOmekaProfileValueBytes)
	}
	preserved, err := structuredJSON(value.raw)
	if err != nil {
		return nil, fmt.Errorf("preserving value: %w", err)
	}
	return preserved, nil
}

func isOmekaBuiltInValue(value valueObject) bool {
	return value.typeName == "literal" || value.typeName == "uri" || isOmekaResourceValue(value)
}

func isOmekaResourceValue(value valueObject) bool {
	switch value.typeName {
	case "resource", "resource:item", "resource:itemset", "resource:media":
		return true
	default:
		return false
	}
}

func omekaProfileReference(value valueObject) map[string]any {
	return map[string]any{
		"id": strconv.FormatInt(value.resourceID, 10), "uri": value.uri,
		"title": value.displayTitle, "resource_name": value.resourceName,
	}
}

func omekaProfileDefaultExtra(value valueObject) any {
	if value.typeName == "uri" {
		return value.uri
	}
	if isOmekaResourceValue(value) {
		return omekaProfileReference(value)
	}
	return displayValue(value)
}

func scalarText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}
