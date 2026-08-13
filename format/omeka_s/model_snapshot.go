package omeka_s

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/lehigh-university-libraries/crosswalk/model"
)

const (
	// ModelSystem is the canonical system name used by Omeka S model snapshots
	// and profiles.
	ModelSystem = "omeka-s"
	// ModelResourceEntity is the model entity containing Omeka resource
	// properties. Its bundle is empty for the installation-wide property set or
	// the decimal resource-template ID for a template-specific shape.
	ModelResourceEntity = "resource"
)

// CompileModel converts the schema portion of a captured Omeka S snapshot into
// Crosswalk's immutable, instance-specific model contract. It performs no
// network or filesystem access, and accepts a model-only acquisition snapshot.
func CompileModel(snapshot Snapshot) (*model.Snapshot, error) {
	if snapshot.CrosswalkFormat != SnapshotFormat {
		return nil, fmt.Errorf("compiling Omeka S model: crosswalk_format must be %q", SnapshotFormat)
	}
	if snapshot.Version != SnapshotVersion {
		return nil, fmt.Errorf("compiling Omeka S model: unsupported snapshot version %d", snapshot.Version)
	}
	totalModel := len(snapshot.Vocabularies) + len(snapshot.Properties) + len(snapshot.ResourceClasses) + len(snapshot.ResourceTemplates)
	if totalModel == 0 {
		return nil, fmt.Errorf("compiling Omeka S model: snapshot has no schema resources")
	}
	if totalModel > maxModelValues {
		return nil, fmt.Errorf("compiling Omeka S model: schema resource count exceeds %d", maxModelValues)
	}

	decoded, err := decodeSchemaModel(snapshot)
	if err != nil {
		return nil, fmt.Errorf("compiling Omeka S model: %w", err)
	}
	sourceURI := ""
	if strings.TrimSpace(snapshot.SourceURI) != "" {
		sourceURI, err = safeAPIURI(snapshot.SourceURI, true)
		if err != nil {
			return nil, fmt.Errorf("compiling Omeka S model: source_uri: %w", err)
		}
	}
	sourceID := strings.TrimSpace(snapshot.SourceID)
	if sourceID != snapshot.SourceID || strings.IndexFunc(sourceID, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("compiling Omeka S model: source_id is invalid")
	}
	if len(sourceID) > 2048 {
		return nil, fmt.Errorf("compiling Omeka S model: source_id exceeds 2048 bytes")
	}
	return compileModelSnapshot(decoded, sourceID, sourceURI)
}

// CompileModelJSON strictly reads a bounded Omeka S acquisition snapshot and
// compiles its schema portion. Unknown fields, duplicate JSON keys, additional
// documents, and inputs larger than the adapter limit are rejected.
func CompileModelJSON(reader io.Reader) (*model.Snapshot, error) {
	snapshot, err := DecodeSnapshot(reader)
	if err != nil {
		return nil, fmt.Errorf("compiling Omeka S model: %w", err)
	}
	return CompileModel(snapshot)
}

// DecodeSnapshot strictly reads and validates a bounded Omeka S acquisition
// snapshot, returning a defensive value suitable for canonical persistence or
// model compilation. It does not parse content records into the Hub.
func DecodeSnapshot(reader io.Reader) (Snapshot, error) {
	raw, err := readOneJSON(reader)
	if err != nil {
		return Snapshot{}, fmt.Errorf("decoding Omeka S snapshot: %w", err)
	}
	if raw[0] != '{' {
		return Snapshot{}, fmt.Errorf("decoding Omeka S snapshot: snapshot must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decoding Omeka S snapshot: %w", err)
	}
	canonical, err := snapshot.CanonicalJSON()
	if err != nil {
		return Snapshot{}, err
	}
	if err := json.Unmarshal(canonical, &snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decoding canonical Omeka S snapshot: %w", err)
	}
	return snapshot, nil
}

func compileModelSnapshot(schema *schemaModel, sourceID, sourceURI string) (*model.Snapshot, error) {
	if schema == nil {
		return nil, fmt.Errorf("compiling Omeka S model: schema is nil")
	}
	entities := make([]model.Entity, 0, 1+len(schema.templatesByID)+len(schema.vocabulariesByID)+len(schema.classesByID))
	entities = append(entities, model.Entity{
		EntityType: ModelResourceEntity,
		Label:      "All Omeka S resources",
		Fields:     omekaModelFields(schema, nil),
	})
	entities = append(entities, omekaStructuralEntity(kindItem), omekaStructuralEntity(kindItemSet), omekaStructuralEntity(kindMedia))
	for _, id := range sortedInt64Keys(schema.templatesByID) {
		template := schema.templatesByID[id]
		entity := model.Entity{
			EntityType:  ModelResourceEntity,
			Bundle:      strconv.FormatInt(template.id, 10),
			Label:       template.label,
			Description: template.uri,
			Fields:      omekaModelFields(schema, &template),
		}
		if class, exists := schema.classesByID[template.resourceClassID]; exists {
			entity.SemanticTypes = []string{class.term}
		}
		entities = append(entities, entity)
	}
	for _, id := range sortedInt64Keys(schema.vocabulariesByID) {
		vocabulary := schema.vocabulariesByID[id]
		entities = append(entities, model.Entity{
			EntityType: "vocabulary", Bundle: strconv.FormatInt(vocabulary.id, 10),
			Label: vocabulary.label, Description: vocabulary.comment,
			SemanticTypes: []string{vocabulary.namespaceURI}, Fields: []model.Field{},
		})
	}
	for _, id := range sortedInt64Keys(schema.classesByID) {
		class := schema.classesByID[id]
		entities = append(entities, model.Entity{
			EntityType: "resource-class", Bundle: strconv.FormatInt(class.id, 10),
			Label: class.label, Description: class.comment,
			SemanticTypes: []string{class.term, class.uri}, Fields: []model.Field{},
		})
	}

	snapshot := &model.Snapshot{
		Version: model.CurrentVersion,
		System:  ModelSystem,
		Provenance: model.Provenance{
			SourceID: sourceID, SourceURI: sourceURI, ConfigHash: schema.fingerprint,
		},
		Entities: entities,
	}
	if err := snapshot.SealFingerprint(); err != nil {
		return nil, fmt.Errorf("compiling Omeka S model fingerprint: %w", err)
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("validating compiled Omeka S model: %w", err)
	}
	return snapshot.Canonical()
}

func omekaStructuralEntity(kind resourceKind) model.Entity {
	entity := model.Entity{
		EntityType: string(kind), Label: "Omeka S " + strings.ReplaceAll(string(kind), "_", " "),
		Fields: omekaCoreFields(),
	}
	switch kind {
	case kindItem:
		entity.Fields = append(entity.Fields,
			model.Field{Path: "o:item_set", Label: "Item sets", SourceType: "resource:itemset", Kind: model.ValueReference, Cardinality: -1, Reference: &model.Reference{EntityType: string(kindItemSet)}},
			model.Field{Path: "o:media", Label: "Media", SourceType: "resource:media", Kind: model.ValueReference, Cardinality: -1, Reference: &model.Reference{EntityType: string(kindMedia)}},
		)
	case kindItemSet:
		entity.Fields = append(entity.Fields, model.Field{Path: "o:is_open", Label: "Open", SourceType: "boolean", Kind: model.ValueBoolean, Cardinality: 1, Required: true})
	case kindMedia:
		entity.Fields = append(entity.Fields,
			model.Field{Path: "o:item", Label: "Item", SourceType: "resource:item", Kind: model.ValueReference, Cardinality: 1, Required: true, Reference: &model.Reference{EntityType: string(kindItem)}},
			model.Field{Path: "o:media_type", Label: "Media type", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			model.Field{Path: "o:source", Label: "Media source", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			model.Field{Path: "o:filename", Label: "Filename", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			model.Field{Path: "o:original_url", Label: "Original URL", SourceType: "uri", Kind: model.ValueLink, Cardinality: 1},
			model.Field{Path: "o:sha256", Label: "SHA-256", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			model.Field{Path: "o:size", Label: "Size", SourceType: "integer", Kind: model.ValueInteger, Cardinality: 1},
			model.Field{Path: "o:lang", Label: "Language", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			model.Field{Path: "o:alt_text", Label: "Alternative text", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
		)
	}
	sort.Slice(entity.Fields, func(i, j int) bool { return entity.Fields[i].Path < entity.Fields[j].Path })
	return entity
}

func omekaModelFields(schema *schemaModel, template *resourceTemplate) []model.Field {
	fields := omekaCoreFields()
	if template == nil {
		for _, id := range sortedInt64Keys(schema.propertiesByID) {
			fields = append(fields, omekaPropertyField(schema, schema.propertiesByID[id], nil))
		}
	} else {
		for index := range template.properties {
			configured := template.properties[index]
			fields = append(fields, omekaPropertyField(schema, schema.propertiesByID[configured.propertyID], &configured))
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Path < fields[j].Path })
	return fields
}

func omekaCoreFields() []model.Field {
	return []model.Field{
		{Path: "o:created", Label: "Created", SourceType: "timestamp", Kind: model.ValueDate, Cardinality: 1},
		{Path: "o:id", Label: "Resource ID", SourceType: "integer", Kind: model.ValueInteger, Cardinality: 1, Required: true},
		{Path: "o:is_public", Label: "Public", SourceType: "boolean", Kind: model.ValueBoolean, Cardinality: 1, Required: true},
		{Path: "o:modified", Label: "Modified", SourceType: "timestamp", Kind: model.ValueDate, Cardinality: 1},
		{Path: "o:resource_class", Label: "Resource class", SourceType: "resource", Kind: model.ValueReference, Cardinality: 1, Reference: &model.Reference{EntityType: "resource-class"}},
		{Path: "o:resource_template", Label: "Resource template", SourceType: "resource", Kind: model.ValueReference, Cardinality: 1, Reference: &model.Reference{EntityType: ModelResourceEntity}},
		{Path: "o:title", Label: "Display title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
	}
}

func omekaPropertyField(schema *schemaModel, propertyValue property, configured *templateProperty) model.Field {
	vocabularyValue := schema.vocabulariesByID[propertyValue.vocabularyID]
	field := model.Field{
		Path: propertyValue.term, Label: firstNonempty(propertyValue.label, propertyValue.term),
		Description: propertyValue.comment, SourceType: "omeka-s:value", Kind: model.ValueComposite,
		Cardinality: -1, SemanticProperties: []string{propertyValue.uri},
		SemanticSettings: map[string]any{
			"property_id": strconv.FormatInt(propertyValue.id, 10), "term": propertyValue.term,
			"vocabulary_id": strconv.FormatInt(vocabularyValue.id, 10), "vocabulary_prefix": vocabularyValue.prefix,
			"namespace_uri": vocabularyValue.namespaceURI,
		},
	}
	if configured == nil {
		return field
	}
	dataTypes := uniqueSortedStrings(configured.dataTypes)
	field.SourceType = omekaSourceType(dataTypes)
	field.Kind = omekaValueKind(dataTypes)
	field.Required = configured.isRequired
	field.Label = firstNonempty(configured.alternateLabel, field.Label)
	field.Description = firstNonempty(configured.alternateComment, field.Description)
	field.InstanceSettings = map[string]any{
		"data_types": dataTypes, "is_private": configured.isPrivate,
	}
	if configured.defaultLanguage != "" {
		field.InstanceSettings["default_language"] = configured.defaultLanguage
	}
	if field.Kind == model.ValueReference {
		field.Reference = &model.Reference{EntityType: ModelResourceEntity}
	}
	return field
}

func omekaSourceType(dataTypes []string) string {
	if len(dataTypes) == 0 {
		return "omeka-s:value"
	}
	return "omeka-s:" + strings.Join(dataTypes, "|")
}

func omekaValueKind(dataTypes []string) model.ValueKind {
	if len(dataTypes) == 0 {
		return model.ValueComposite
	}
	kind := model.ValueOpaque
	for _, dataType := range dataTypes {
		candidate := model.ValueOpaque
		switch {
		case dataType == "numeric:integer":
			candidate = model.ValueInteger
		case dataType == "numeric:float" || dataType == "numeric:decimal":
			candidate = model.ValueDecimal
		case dataType == "boolean":
			candidate = model.ValueBoolean
		case dataType == "literal" || strings.HasPrefix(dataType, "numeric:"):
			candidate = model.ValueText
		case dataType == "uri":
			candidate = model.ValueLink
		case dataType == "resource" || strings.HasPrefix(dataType, "resource:"):
			candidate = model.ValueReference
		}
		if kind == model.ValueOpaque {
			kind = candidate
		} else if candidate != kind {
			return model.ValueComposite
		}
	}
	return kind
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
