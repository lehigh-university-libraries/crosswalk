package omeka_s

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func attachOmekaExtras(record *hubv1.Record, source *resource, model *schemaModel, preserveExisting, includeDefinitions bool) error {
	values := make([]any, 0, len(source.properties))
	for _, term := range source.properties {
		entry := map[string]any{"term": term.term}
		if model != nil && includeDefinitions {
			if propertyValue, exists := model.propertiesByTerm[term.term]; exists {
				propertyExtra, err := propertyDefinitionExtra(propertyValue, model)
				if err != nil {
					return fmt.Errorf("preserving property model %q: %w", term.term, err)
				}
				entry["property"] = propertyExtra
			}
		}
		items := make([]any, 0, len(term.values))
		for index, value := range term.values {
			item, err := valueExtra(value)
			if err != nil {
				return fmt.Errorf("preserving property %q value %d: %w", term.term, index, err)
			}
			items = append(items, item)
		}
		entry["values"] = items
		values = append(values, entry)
	}

	metadata := make([]any, 0, len(source.metadata))
	for _, value := range source.metadata {
		structured, err := structuredJSON(value.raw)
		if err != nil {
			return fmt.Errorf("preserving metadata %q: %w", value.name, err)
		}
		metadata = append(metadata, map[string]any{"name": value.name, "value": structured})
	}

	types := make([]any, len(source.types))
	for index, value := range source.types {
		types[index] = value
	}
	omeka := map[string]any{
		"resource_kind": string(source.kind),
		"resource_id":   strconv.FormatInt(source.id, 10),
		"resource_uri":  source.uri,
		"types":         types,
		"is_public":     source.isPublic,
		"values":        values,
		"metadata":      metadata,
	}
	if source.resourceClass != nil {
		value, err := referenceExtra(*source.resourceClass)
		if err != nil {
			return fmt.Errorf("preserving resource class reference: %w", err)
		}
		omeka["resource_class"] = value
		if model != nil && includeDefinitions {
			if definition, exists := model.classesByID[source.resourceClass.id]; exists {
				structured, err := structuredJSON(definition.raw)
				if err != nil {
					return fmt.Errorf("preserving resource class definition: %w", err)
				}
				omeka["resource_class_definition"] = structured
			}
		}
	}
	if source.resourceTemplate != nil {
		value, err := referenceExtra(*source.resourceTemplate)
		if err != nil {
			return fmt.Errorf("preserving resource template reference: %w", err)
		}
		omeka["resource_template"] = value
		if model != nil && includeDefinitions {
			if definition, exists := model.templatesByID[source.resourceTemplate.id]; exists {
				structured, err := structuredJSON(definition.raw)
				if err != nil {
					return fmt.Errorf("preserving resource template definition: %w", err)
				}
				omeka["resource_template_definition"] = structured
			}
		}
	}
	if len(source.itemSets) > 0 {
		references, err := referencesExtra(source.itemSets)
		if err != nil {
			return fmt.Errorf("preserving item set references: %w", err)
		}
		omeka["item_sets"] = references
	}
	if len(source.media) > 0 {
		references, err := referencesExtra(source.media)
		if err != nil {
			return fmt.Errorf("preserving media references: %w", err)
		}
		omeka["media"] = references
	}
	if source.item != nil {
		value, err := referenceExtra(*source.item)
		if err != nil {
			return fmt.Errorf("preserving item reference: %w", err)
		}
		omeka["item"] = value
	}
	switch source.kind {
	case kindItemSet:
		omeka["is_open"] = source.itemSetOpen
	case kindMedia:
		omeka["media_type"] = source.mediaType
		omeka["media_source"] = source.mediaSource
		omeka["filename"] = source.filename
		omeka["original_url"] = source.originalURL
		omeka["sha256"] = source.sha256
		omeka["size"] = strconv.FormatInt(source.size, 10)
		omeka["language"] = source.lang
		omeka["alt_text"] = source.altText
	}
	if model != nil {
		omeka["model_fingerprint"] = model.fingerprint
	}
	root := map[string]any{"omeka_s": omeka}
	if preserveExisting && record.Extra != nil {
		for key, value := range record.Extra.AsMap() {
			root[key] = value
		}
	}
	extra, err := structpb.NewStruct(root)
	if err != nil {
		return fmt.Errorf("encode Omeka S extras: %w", err)
	}
	record.Extra = extra
	return nil
}

func propertyDefinitionExtra(value property, model *schemaModel) (map[string]any, error) {
	result := map[string]any{
		"id":         strconv.FormatInt(value.id, 10),
		"uri":        value.uri,
		"term":       value.term,
		"local_name": value.localName,
		"label":      value.label,
		"comment":    value.comment,
	}
	if vocabularyValue, exists := model.vocabulariesByID[value.vocabularyID]; exists {
		result["vocabulary"] = map[string]any{
			"id":            strconv.FormatInt(vocabularyValue.id, 10),
			"uri":           vocabularyValue.uri,
			"prefix":        vocabularyValue.prefix,
			"namespace_uri": vocabularyValue.namespaceURI,
			"label":         vocabularyValue.label,
			"comment":       vocabularyValue.comment,
		}
	}
	structured, err := structuredJSON(value.raw)
	if err != nil {
		return nil, err
	}
	result["source_value"] = structured
	return result, nil
}

func valueExtra(value valueObject) (map[string]any, error) {
	result := map[string]any{
		"type":           value.typeName,
		"property_id":    value.propertyID,
		"property_label": value.propertyLabel,
	}
	if value.isPublic != nil {
		result["is_public"] = *value.isPublic
	}
	switch value.typeName {
	case "literal":
		result["value"] = value.literal
		if value.language != "" {
			result["language"] = value.language
		}
	case "uri":
		result["uri"] = value.uri
		if value.label != "" {
			result["label"] = value.label
		}
	case "resource", "resource:item", "resource:itemset", "resource:media":
		result["resource_id"] = strconv.FormatInt(value.resourceID, 10)
		if value.uri != "" {
			result["resource_uri"] = value.uri
		}
		if value.resourceName != "" {
			result["resource_name"] = value.resourceName
		}
		if value.displayTitle != "" {
			result["display_title"] = value.displayTitle
		}
		if value.thumbnailURL != "" {
			result["thumbnail_url"] = value.thumbnailURL
		}
	}
	structured, err := structuredJSON(value.raw)
	if err != nil {
		return nil, err
	}
	result["source_value"] = structured
	return result, nil
}

func referencesExtra(values []reference) ([]any, error) {
	result := make([]any, 0, len(values))
	for index, value := range values {
		converted, err := referenceExtra(value)
		if err != nil {
			return nil, fmt.Errorf("reference %d: %w", index, err)
		}
		result = append(result, converted)
	}
	return result, nil
}

func referenceExtra(value reference) (map[string]any, error) {
	result := map[string]any{"id": strconv.FormatInt(value.id, 10)}
	if value.uri != "" {
		result["uri"] = value.uri
	}
	if value.title != "" {
		result["title"] = value.title
	}
	if value.kind != "" {
		result["kind"] = value.kind
	}
	if len(value.raw) > 0 {
		structured, err := structuredJSON(value.raw)
		if err != nil {
			return nil, err
		}
		result["source_value"] = structured
	}
	return result, nil
}

func structuredJSON(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return taggedJSON(value, 0)
}

func taggedJSON(value any, depth int) (any, error) {
	const maxDepth = 256
	if depth > maxDepth {
		return nil, fmt.Errorf("JSON nesting exceeds %d levels", maxDepth)
	}
	switch typed := value.(type) {
	case nil:
		return map[string]any{"kind": "null"}, nil
	case bool:
		return map[string]any{"kind": "boolean", "boolean_value": typed}, nil
	case json.Number:
		return map[string]any{"kind": "number", "number_value": typed.String()}, nil
	case string:
		return map[string]any{"kind": "string", "string_value": typed}, nil
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			converted, err := taggedJSON(item, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, converted)
		}
		return map[string]any{"kind": "array", "items": items}, nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		members := make([]any, 0, len(keys))
		for _, key := range keys {
			converted, err := taggedJSON(typed[key], depth+1)
			if err != nil {
				return nil, err
			}
			members = append(members, map[string]any{"name": key, "value": converted})
		}
		return map[string]any{"kind": "object", "members": members}, nil
	default:
		return nil, fmt.Errorf("unsupported JSON value type %T", value)
	}
}
