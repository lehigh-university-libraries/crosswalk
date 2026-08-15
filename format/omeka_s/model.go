package omeka_s

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

func decodeSchemaModel(envelope Snapshot) (*schemaModel, error) {
	model := &schemaModel{
		vocabulariesByID:     make(map[int64]vocabulary, len(envelope.Vocabularies)),
		vocabulariesByPrefix: make(map[string]vocabulary, len(envelope.Vocabularies)),
		propertiesByID:       make(map[int64]property, len(envelope.Properties)),
		propertiesByTerm:     make(map[string]property, len(envelope.Properties)),
		classesByID:          make(map[int64]resourceClass, len(envelope.ResourceClasses)),
		templatesByID:        make(map[int64]resourceTemplate, len(envelope.ResourceTemplates)),
	}
	for index, raw := range envelope.Vocabularies {
		value, err := decodeVocabulary(raw)
		if err != nil {
			return nil, fmt.Errorf("vocabularies[%d]: %w", index, err)
		}
		if _, duplicate := model.vocabulariesByID[value.id]; duplicate {
			return nil, fmt.Errorf("vocabularies[%d]: duplicate o:id %d", index, value.id)
		}
		if _, duplicate := model.vocabulariesByPrefix[value.prefix]; duplicate {
			return nil, fmt.Errorf("vocabularies[%d]: duplicate o:prefix %q", index, value.prefix)
		}
		model.vocabulariesByID[value.id] = value
		model.vocabulariesByPrefix[value.prefix] = value
	}
	for index, raw := range envelope.Properties {
		value, err := decodeProperty(raw)
		if err != nil {
			return nil, fmt.Errorf("properties[%d]: %w", index, err)
		}
		if _, duplicate := model.propertiesByID[value.id]; duplicate {
			return nil, fmt.Errorf("properties[%d]: duplicate o:id %d", index, value.id)
		}
		if _, duplicate := model.propertiesByTerm[value.term]; duplicate {
			return nil, fmt.Errorf("properties[%d]: duplicate o:term %q", index, value.term)
		}
		model.propertiesByID[value.id] = value
		model.propertiesByTerm[value.term] = value
	}
	for index, raw := range envelope.ResourceClasses {
		value, err := decodeResourceClass(raw)
		if err != nil {
			return nil, fmt.Errorf("resource_classes[%d]: %w", index, err)
		}
		if _, duplicate := model.classesByID[value.id]; duplicate {
			return nil, fmt.Errorf("resource_classes[%d]: duplicate o:id %d", index, value.id)
		}
		model.classesByID[value.id] = value
	}
	for index, raw := range envelope.ResourceTemplates {
		value, err := decodeResourceTemplate(raw)
		if err != nil {
			return nil, fmt.Errorf("resource_templates[%d]: %w", index, err)
		}
		if _, duplicate := model.templatesByID[value.id]; duplicate {
			return nil, fmt.Errorf("resource_templates[%d]: duplicate o:id %d", index, value.id)
		}
		model.templatesByID[value.id] = value
	}
	if err := validateSchemaModel(model); err != nil {
		return nil, err
	}
	fingerprint, err := modelDigest(envelope.Vocabularies, envelope.Properties, envelope.ResourceClasses, envelope.ResourceTemplates)
	if err != nil {
		return nil, fmt.Errorf("fingerprint model: %w", err)
	}
	model.fingerprint = fingerprint
	return model, nil
}

func decodeVocabulary(raw json.RawMessage) (vocabulary, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return vocabulary{}, err
	}
	if err := requireOmekaType(object, "o:Vocabulary"); err != nil {
		return vocabulary{}, err
	}
	base, err := decodeModelBase(object)
	if err != nil {
		return vocabulary{}, err
	}
	prefix, err := requiredString(object, "o:prefix")
	if err != nil {
		return vocabulary{}, err
	}
	if !regexpPrefix(prefix) {
		return vocabulary{}, fmt.Errorf("o:prefix %q is not a valid JSON-LD prefix", prefix)
	}
	namespaceURI, err := requiredString(object, "o:namespace_uri")
	if err != nil {
		return vocabulary{}, err
	}
	namespaceURI, err = absoluteURI(namespaceURI)
	if err != nil {
		return vocabulary{}, fmt.Errorf("o:namespace_uri: %w", err)
	}
	label, err := optionalString(object, "o:label")
	if err != nil {
		return vocabulary{}, err
	}
	comment, err := optionalString(object, "o:comment")
	if err != nil {
		return vocabulary{}, err
	}
	return vocabulary{id: base.id, uri: base.uri, prefix: prefix, namespaceURI: namespaceURI, label: label, comment: comment, raw: raw}, nil
}

func decodeProperty(raw json.RawMessage) (property, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return property{}, err
	}
	if err := requireOmekaType(object, "o:Property"); err != nil {
		return property{}, err
	}
	base, err := decodeVocabularyMember(object)
	if err != nil {
		return property{}, err
	}
	return property{id: base.id, uri: base.uri, localName: base.localName, label: base.label, comment: base.comment, term: base.term, vocabularyID: base.vocabularyID, raw: raw}, nil
}

func decodeResourceClass(raw json.RawMessage) (resourceClass, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return resourceClass{}, err
	}
	if err := requireOmekaType(object, "o:ResourceClass"); err != nil {
		return resourceClass{}, err
	}
	base, err := decodeVocabularyMember(object)
	if err != nil {
		return resourceClass{}, err
	}
	return resourceClass{id: base.id, uri: base.uri, localName: base.localName, label: base.label, comment: base.comment, term: base.term, vocabularyID: base.vocabularyID, raw: raw}, nil
}

type modelBase struct {
	id  int64
	uri string
}

type vocabularyMemberBase struct {
	modelBase
	localName    string
	label        string
	comment      string
	term         string
	vocabularyID int64
}

func decodeModelBase(object map[string]json.RawMessage) (modelBase, error) {
	id, err := requiredPositiveID(object, "o:id")
	if err != nil {
		return modelBase{}, err
	}
	uri, err := requiredString(object, "@id")
	if err != nil {
		return modelBase{}, err
	}
	uri, err = safeAPIURI(uri, false)
	if err != nil {
		return modelBase{}, fmt.Errorf("@id: %w", err)
	}
	return modelBase{id: id, uri: uri}, nil
}

func decodeVocabularyMember(object map[string]json.RawMessage) (vocabularyMemberBase, error) {
	base, err := decodeModelBase(object)
	if err != nil {
		return vocabularyMemberBase{}, err
	}
	localName, err := requiredString(object, "o:local_name")
	if err != nil {
		return vocabularyMemberBase{}, err
	}
	term, err := requiredString(object, "o:term")
	if err != nil {
		return vocabularyMemberBase{}, err
	}
	if !termPattern.MatchString(term) {
		return vocabularyMemberBase{}, fmt.Errorf("o:term %q is not a valid compact IRI", term)
	}
	label, err := optionalString(object, "o:label")
	if err != nil {
		return vocabularyMemberBase{}, err
	}
	comment, err := optionalString(object, "o:comment")
	if err != nil {
		return vocabularyMemberBase{}, err
	}
	vocabularyRef, err := requiredReference(object, "o:vocabulary")
	if err != nil {
		return vocabularyMemberBase{}, err
	}
	return vocabularyMemberBase{modelBase: base, localName: localName, label: label, comment: comment, term: term, vocabularyID: vocabularyRef.id}, nil
}

func decodeResourceTemplate(raw json.RawMessage) (resourceTemplate, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return resourceTemplate{}, err
	}
	if err := requireOmekaType(object, "o:ResourceTemplate"); err != nil {
		return resourceTemplate{}, err
	}
	base, err := decodeModelBase(object)
	if err != nil {
		return resourceTemplate{}, err
	}
	label, err := requiredString(object, "o:label")
	if err != nil {
		return resourceTemplate{}, err
	}
	value := resourceTemplate{id: base.id, uri: base.uri, label: label, raw: raw}
	for _, field := range []struct {
		name   string
		target *int64
	}{
		{name: "o:resource_class", target: &value.resourceClassID},
		{name: "o:title_property", target: &value.titlePropertyID},
		{name: "o:description_property", target: &value.descriptionPropertyID},
	} {
		referenceValue, err := optionalReference(object, field.name)
		if err != nil {
			return resourceTemplate{}, err
		}
		if referenceValue != nil {
			*field.target = referenceValue.id
		}
	}
	rawProperties, exists := object["o:resource_template_property"]
	if !exists || bytes.Equal(bytes.TrimSpace(rawProperties), []byte("null")) {
		return value, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rawProperties, &entries); err != nil {
		return resourceTemplate{}, fmt.Errorf("o:resource_template_property must be an array")
	}
	if len(entries) > maxModelValues {
		return resourceTemplate{}, fmt.Errorf("o:resource_template_property count exceeds %d", maxModelValues)
	}
	seen := make(map[int64]struct{}, len(entries))
	for index, entry := range entries {
		propertyValue, err := decodeTemplateProperty(entry)
		if err != nil {
			return resourceTemplate{}, fmt.Errorf("o:resource_template_property[%d]: %w", index, err)
		}
		if _, duplicate := seen[propertyValue.propertyID]; duplicate {
			return resourceTemplate{}, fmt.Errorf("o:resource_template_property[%d]: duplicate property %d", index, propertyValue.propertyID)
		}
		seen[propertyValue.propertyID] = struct{}{}
		value.properties = append(value.properties, propertyValue)
	}
	return value, nil
}

func decodeTemplateProperty(raw json.RawMessage) (templateProperty, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return templateProperty{}, err
	}
	propertyRef, err := requiredReference(object, "o:property")
	if err != nil {
		return templateProperty{}, err
	}
	value := templateProperty{propertyID: propertyRef.id}
	value.alternateLabel, err = optionalString(object, "o:alternate_label")
	if err != nil {
		return templateProperty{}, err
	}
	value.alternateComment, err = optionalString(object, "o:alternate_comment")
	if err != nil {
		return templateProperty{}, err
	}
	value.defaultLanguage, err = optionalString(object, "o:default_lang")
	if err != nil {
		return templateProperty{}, err
	}
	value.isRequired, err = optionalBool(object, "o:is_required")
	if err != nil {
		return templateProperty{}, err
	}
	value.isPrivate, err = optionalBool(object, "o:is_private")
	if err != nil {
		return templateProperty{}, err
	}
	if rawTypes, exists := object["o:data_type"]; exists && !bytes.Equal(bytes.TrimSpace(rawTypes), []byte("null")) {
		if err := json.Unmarshal(rawTypes, &value.dataTypes); err != nil {
			var single string
			if stringErr := json.Unmarshal(rawTypes, &single); stringErr != nil {
				return templateProperty{}, fmt.Errorf("o:data_type must be a string or string array")
			}
			value.dataTypes = []string{single}
		}
		for index, dataType := range value.dataTypes {
			dataType = strings.TrimSpace(dataType)
			if dataType == "" {
				return templateProperty{}, fmt.Errorf("o:data_type[%d] must not be empty", index)
			}
			value.dataTypes[index] = dataType
		}
	}
	return value, nil
}

func validateSchemaModel(model *schemaModel) error {
	for _, id := range sortedInt64Keys(model.propertiesByID) {
		value := model.propertiesByID[id]
		vocab, exists := model.vocabulariesByID[value.vocabularyID]
		if !exists {
			return fmt.Errorf("property %q references missing vocabulary %d", value.term, value.vocabularyID)
		}
		expected := vocab.prefix + ":" + value.localName
		if value.term != expected {
			return fmt.Errorf("property %d term %q does not match vocabulary/local name %q", value.id, value.term, expected)
		}
	}
	for _, id := range sortedInt64Keys(model.classesByID) {
		value := model.classesByID[id]
		vocab, exists := model.vocabulariesByID[value.vocabularyID]
		if !exists {
			return fmt.Errorf("resource class %q references missing vocabulary %d", value.term, value.vocabularyID)
		}
		expected := vocab.prefix + ":" + value.localName
		if value.term != expected {
			return fmt.Errorf("resource class %d term %q does not match vocabulary/local name %q", value.id, value.term, expected)
		}
	}
	for _, id := range sortedInt64Keys(model.templatesByID) {
		value := model.templatesByID[id]
		if value.resourceClassID != 0 {
			if _, exists := model.classesByID[value.resourceClassID]; !exists {
				return fmt.Errorf("resource template %d references missing resource class %d", value.id, value.resourceClassID)
			}
		}
		for _, propertyID := range []int64{value.titlePropertyID, value.descriptionPropertyID} {
			if propertyID != 0 {
				if _, exists := model.propertiesByID[propertyID]; !exists {
					return fmt.Errorf("resource template %d references missing property %d", value.id, propertyID)
				}
			}
		}
		for _, templateProperty := range value.properties {
			if _, exists := model.propertiesByID[templateProperty.propertyID]; !exists {
				return fmt.Errorf("resource template %d references missing property %d", value.id, templateProperty.propertyID)
			}
		}
	}
	return nil
}

func sortedInt64Keys[T any](values map[int64]T) []int64 {
	keys := make([]int64, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func decodeObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return nil, fmt.Errorf("value must be an object")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("decode object: %w", err)
	}
	return object, nil
}

func requireOmekaType(object map[string]json.RawMessage, expected string) error {
	raw, exists := object["@type"]
	if !exists {
		return fmt.Errorf("@type is required")
	}
	types, err := decodeTypesRaw(raw)
	if err != nil {
		return err
	}
	for _, value := range types {
		if value == expected {
			return nil
		}
	}
	return fmt.Errorf("@type must include %q", expected)
}

func decodeTypesRaw(raw json.RawMessage) ([]string, error) {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if strings.TrimSpace(single) == "" {
			return nil, fmt.Errorf("@type must not be empty")
		}
		return []string{single}, nil
	}
	var multiple []string
	if err := json.Unmarshal(raw, &multiple); err != nil || len(multiple) == 0 {
		return nil, fmt.Errorf("@type must be a string or nonempty string array")
	}
	for index, value := range multiple {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("@type[%d] must not be empty", index)
		}
	}
	return multiple, nil
}

func requiredPositiveID(object map[string]json.RawMessage, field string) (int64, error) {
	raw, exists := object[field]
	if !exists {
		return 0, fmt.Errorf("%s is required", field)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", field)
	}
	return value, nil
}

func requiredString(object map[string]json.RawMessage, field string) (string, error) {
	value, err := optionalString(object, field)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	return value, nil
}

func optionalString(object map[string]json.RawMessage, field string) (string, error) {
	raw, exists := object[field]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string or null", field)
	}
	return strings.TrimSpace(value), nil
}

func optionalBool(object map[string]json.RawMessage, field string) (bool, error) {
	raw, exists := object[field]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("%s must be a boolean or null", field)
	}
	return value, nil
}

func requiredReference(object map[string]json.RawMessage, field string) (*reference, error) {
	value, err := optionalReference(object, field)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("%s is required", field)
	}
	return value, nil
}

func optionalReference(object map[string]json.RawMessage, field string) (*reference, error) {
	raw, exists := object[field]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	value, err := decodeReference(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", field, err)
	}
	return &value, nil
}

func decodeReference(raw json.RawMessage) (reference, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return reference{}, err
	}
	id, err := requiredPositiveID(object, "o:id")
	if err != nil {
		if fallback, fallbackExists := object["value_resource_id"]; fallbackExists {
			id, err = requiredPositiveID(map[string]json.RawMessage{"value_resource_id": fallback}, "value_resource_id")
		}
		if err != nil {
			return reference{}, err
		}
	}
	uri, err := optionalString(object, "@id")
	if err != nil {
		return reference{}, err
	}
	if uri != "" {
		uri, err = safeAPIURI(uri, false)
		if err != nil {
			return reference{}, fmt.Errorf("@id: %w", err)
		}
	}
	title, err := optionalString(object, "display_title")
	if err != nil {
		return reference{}, err
	}
	kind, err := optionalString(object, "value_resource_name")
	if err != nil {
		return reference{}, err
	}
	return reference{id: id, uri: uri, title: title, kind: kind, raw: append(json.RawMessage(nil), raw...)}, nil
}

func regexpPrefix(value string) bool {
	if value == "" || !unicode.IsLetter(rune(value[0])) {
		return false
	}
	for _, character := range value[1:] {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	return true
}

func sortedMapKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
