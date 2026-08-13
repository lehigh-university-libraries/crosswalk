package omeka_s

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var consumedResourceKeys = map[string]struct{}{
	"@context":            {},
	"@id":                 {},
	"@type":               {},
	"o:id":                {},
	"o:is_public":         {},
	"o:title":             {},
	"o:created":           {},
	"o:modified":          {},
	"o:resource_class":    {},
	"o:resource_template": {},
	"o:item_set":          {},
	"o:media":             {},
	"o:item":              {},
	"o:is_open":           {},
	"o:media_type":        {},
	"o:source":            {},
	"o:filename":          {},
	"o:original_url":      {},
	"o:sha256":            {},
	"o:size":              {},
	"o:lang":              {},
	"o:alt_text":          {},
}

func decodeResource(raw json.RawMessage, expectedKind resourceKind, model *schemaModel) (*resource, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	expectedType := map[resourceKind]string{kindItem: "o:Item", kindItemSet: "o:ItemSet", kindMedia: "o:Media"}[expectedKind]
	if err := requireOmekaType(object, expectedType); err != nil {
		return nil, err
	}
	types, err := decodeTypesRaw(object["@type"])
	if err != nil {
		return nil, err
	}
	id, err := requiredPositiveID(object, "o:id")
	if err != nil {
		return nil, err
	}
	uri, err := requiredString(object, "@id")
	if err != nil {
		return nil, err
	}
	uri, err = safeAPIURI(uri, false)
	if err != nil {
		return nil, fmt.Errorf("@id: %w", err)
	}
	isPublic, err := requiredBool(object, "o:is_public")
	if err != nil {
		return nil, err
	}
	title, err := optionalString(object, "o:title")
	if err != nil {
		return nil, err
	}
	created, err := optionalDateTime(object, "o:created")
	if err != nil {
		return nil, err
	}
	modified, err := optionalDateTime(object, "o:modified")
	if err != nil {
		return nil, err
	}
	value := &resource{kind: expectedKind, id: id, uri: uri, types: types, title: title, isPublic: isPublic, created: created, modified: modified}
	value.resourceClass, err = optionalReference(object, "o:resource_class")
	if err != nil {
		return nil, err
	}
	value.resourceTemplate, err = optionalReference(object, "o:resource_template")
	if err != nil {
		return nil, err
	}
	if model != nil {
		if value.resourceClass != nil && len(model.classesByID) > 0 {
			if _, exists := model.classesByID[value.resourceClass.id]; !exists {
				return nil, fmt.Errorf("o:resource_class references missing resource class %d", value.resourceClass.id)
			}
		}
		if value.resourceTemplate != nil && len(model.templatesByID) > 0 {
			if _, exists := model.templatesByID[value.resourceTemplate.id]; !exists {
				return nil, fmt.Errorf("o:resource_template references missing resource template %d", value.resourceTemplate.id)
			}
		}
	}

	switch expectedKind {
	case kindItem:
		value.itemSets, err = decodeReferenceArray(object, "o:item_set")
		if err != nil {
			return nil, err
		}
		value.media, err = decodeReferenceArray(object, "o:media")
		if err != nil {
			return nil, err
		}
	case kindItemSet:
		value.itemSetOpen, err = requiredBool(object, "o:is_open")
		if err != nil {
			return nil, err
		}
	case kindMedia:
		value.item, err = requiredReference(object, "o:item")
		if err != nil {
			return nil, err
		}
		value.mediaType, err = optionalString(object, "o:media_type")
		if err != nil {
			return nil, err
		}
		value.mediaSource, err = optionalString(object, "o:source")
		if err != nil {
			return nil, err
		}
		value.filename, err = optionalString(object, "o:filename")
		if err != nil {
			return nil, err
		}
		if err := validateFilename(value.filename); err != nil {
			return nil, fmt.Errorf("o:filename: %w", err)
		}
		value.originalURL, err = optionalString(object, "o:original_url")
		if err != nil {
			return nil, err
		}
		if value.originalURL != "" {
			value.originalURL, err = safeAPIURI(value.originalURL, false)
			if err != nil {
				return nil, fmt.Errorf("o:original_url: %w", err)
			}
		}
		value.sha256, err = optionalString(object, "o:sha256")
		if err != nil {
			return nil, err
		}
		if value.sha256 != "" && !sha256Pattern.MatchString(value.sha256) {
			return nil, fmt.Errorf("o:sha256 must be 64 hexadecimal characters")
		}
		value.sha256 = strings.ToLower(value.sha256)
		value.size, err = optionalNonnegativeInt64(object, "o:size")
		if err != nil {
			return nil, err
		}
		value.lang, err = optionalString(object, "o:lang")
		if err != nil {
			return nil, err
		}
		value.altText, err = optionalString(object, "o:alt_text")
		if err != nil {
			return nil, err
		}
	}

	for _, key := range sortedMapKeys(object) {
		rawValue := object[key]
		isTerm := model != nil && model.propertiesByTerm[key].term != ""
		if !isTerm && termPattern.MatchString(key) && !strings.HasPrefix(key, "o:") {
			isTerm = looksLikePropertyValues(rawValue)
		}
		if isTerm {
			values, err := decodeTermValues(key, rawValue, model)
			if err != nil {
				return nil, err
			}
			value.properties = append(value.properties, termValues{term: key, values: values})
			continue
		}
		if _, consumed := consumedResourceKeys[key]; !consumed {
			value.metadata = append(value.metadata, namedRawValue{name: key, raw: rawValue})
		}
	}
	sort.Slice(value.properties, func(i, j int) bool { return value.properties[i].term < value.properties[j].term })
	return value, nil
}

func validateFilename(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 4096 {
		return fmt.Errorf("filename exceeds 4096 bytes")
	}
	if value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("filename must not contain path components")
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("filename contains a control character")
	}
	return nil
}

func looksLikePropertyValues(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' {
		return false
	}
	var values []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return false
	}
	if len(values) == 0 {
		return true
	}
	_, hasType := values[0]["type"]
	_, hasPropertyID := values[0]["property_id"]
	return hasType && hasPropertyID
}

func decodeTermValues(term string, raw json.RawMessage, model *schemaModel) ([]valueObject, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("property %q must be an array", term)
	}
	if len(entries) > maxTermValues {
		return nil, fmt.Errorf("property %q value count exceeds %d", term, maxTermValues)
	}
	var expectedProperty *property
	if model != nil && len(model.propertiesByID) > 0 {
		value, exists := model.propertiesByTerm[term]
		if !exists {
			return nil, fmt.Errorf("property term %q is missing from the snapshot model", term)
		}
		expectedProperty = &value
	}
	values := make([]valueObject, 0, len(entries))
	for index, entry := range entries {
		value, err := decodeValueObject(entry)
		if err != nil {
			return nil, fmt.Errorf("property %q value %d: %w", term, index, err)
		}
		if expectedProperty != nil && value.propertyID != "auto" && value.propertyID != strconv.FormatInt(expectedProperty.id, 10) {
			return nil, fmt.Errorf("property %q value %d has property_id %q; expected %d", term, index, value.propertyID, expectedProperty.id)
		}
		values = append(values, value)
	}
	return values, nil
}

func decodeValueObject(raw json.RawMessage) (valueObject, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return valueObject{}, err
	}
	typeName, err := requiredString(object, "type")
	if err != nil {
		return valueObject{}, err
	}
	propertyID, err := requiredPropertyID(object)
	if err != nil {
		return valueObject{}, err
	}
	value := valueObject{typeName: typeName, propertyID: propertyID, raw: raw}
	value.propertyLabel, err = optionalString(object, "property_label")
	if err != nil {
		return valueObject{}, err
	}
	if rawPublic, exists := object["is_public"]; exists && !bytes.Equal(bytes.TrimSpace(rawPublic), []byte("null")) {
		var isPublic bool
		if err := json.Unmarshal(rawPublic, &isPublic); err != nil {
			return valueObject{}, fmt.Errorf("is_public must be a boolean or null")
		}
		value.isPublic = &isPublic
	}
	switch typeName {
	case "literal":
		value.literal, err = requiredLiteral(object, "@value")
		if err != nil {
			return valueObject{}, err
		}
		value.language, err = optionalString(object, "@language")
		if err != nil {
			return valueObject{}, err
		}
	case "uri":
		value.uri, err = requiredString(object, "@id")
		if err != nil {
			return valueObject{}, err
		}
		value.uri, err = absoluteURI(value.uri)
		if err != nil {
			return valueObject{}, fmt.Errorf("@id: %w", err)
		}
		value.label, err = optionalString(object, "o:label")
		if err != nil {
			return valueObject{}, err
		}
	case "resource", "resource:item", "resource:itemset", "resource:media":
		value.resourceID, err = requiredPositiveID(object, "value_resource_id")
		if err != nil {
			return valueObject{}, err
		}
		value.uri, err = optionalString(object, "@id")
		if err != nil {
			return valueObject{}, err
		}
		if value.uri != "" {
			value.uri, err = safeAPIURI(value.uri, false)
			if err != nil {
				return valueObject{}, fmt.Errorf("@id: %w", err)
			}
		}
		value.resourceName, err = optionalString(object, "value_resource_name")
		if err != nil {
			return valueObject{}, err
		}
		value.displayTitle, err = optionalString(object, "display_title")
		if err != nil {
			return valueObject{}, err
		}
		value.thumbnailURL, err = optionalString(object, "thumbnail_url")
		if err != nil {
			return valueObject{}, err
		}
		if value.thumbnailURL != "" {
			value.thumbnailURL, err = safeAPIURI(value.thumbnailURL, false)
			if err != nil {
				return valueObject{}, fmt.Errorf("thumbnail_url: %w", err)
			}
		}
	default:
		// Modules can register additional data types. Their complete JSON value is
		// retained in extras; Crosswalk does not invent extraction semantics.
	}
	return value, nil
}

func requiredPropertyID(object map[string]json.RawMessage) (string, error) {
	raw, exists := object["property_id"]
	if !exists {
		return "", fmt.Errorf("property_id is required")
	}
	var stringValue string
	if err := json.Unmarshal(raw, &stringValue); err == nil {
		stringValue = strings.TrimSpace(stringValue)
		if stringValue == "auto" {
			return stringValue, nil
		}
		id, parseErr := strconv.ParseInt(stringValue, 10, 64)
		if parseErr != nil || id <= 0 {
			return "", fmt.Errorf("property_id string must be \"auto\" or a positive integer")
		}
		return strconv.FormatInt(id, 10), nil
	}
	id, err := requiredPositiveID(map[string]json.RawMessage{"property_id": raw}, "property_id")
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}

func requiredLiteral(object map[string]json.RawMessage, field string) (string, error) {
	raw, exists := object[field]
	if !exists {
		return "", fmt.Errorf("%s is required", field)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", field)
	}
	return value, nil
}

func requiredBool(object map[string]json.RawMessage, field string) (bool, error) {
	raw, exists := object[field]
	if !exists {
		return false, fmt.Errorf("%s is required", field)
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("%s must be a boolean", field)
	}
	return value, nil
}

func optionalDateTime(object map[string]json.RawMessage, field string) (string, error) {
	raw, exists := object[field]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	valueObject, err := decodeObject(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	value, err := requiredString(valueObject, "@value")
	if err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return "", fmt.Errorf("%s @value must be an RFC 3339 timestamp: %w", field, err)
	}
	return value, nil
}

func decodeReferenceArray(object map[string]json.RawMessage, field string) ([]reference, error) {
	raw, exists := object[field]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("%s must be an array", field)
	}
	if len(entries) > maxResources {
		return nil, fmt.Errorf("%s count exceeds %d", field, maxResources)
	}
	values := make([]reference, 0, len(entries))
	seen := make(map[int64]struct{}, len(entries))
	for index, entry := range entries {
		value, err := decodeReference(entry)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", field, index, err)
		}
		if _, duplicate := seen[value.id]; duplicate {
			return nil, fmt.Errorf("%s[%d]: duplicate reference %d", field, index, value.id)
		}
		seen[value.id] = struct{}{}
		values = append(values, value)
	}
	return values, nil
}

func optionalNonnegativeInt64(object map[string]json.RawMessage, field string) (int64, error) {
	raw, exists := object[field]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return 0, fmt.Errorf("%s must be an integer or null", field)
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a nonnegative integer", field)
	}
	return value, nil
}
