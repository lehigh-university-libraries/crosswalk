package drupal

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/internal/provenanceuri"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

var drupalCodecs = map[string]struct{}{
	"none": {}, "text": {}, "integer": {}, "decimal": {}, "boolean": {},
	"date": {}, "link": {}, "reference": {}, "typed-relation": {},
	"file": {}, "composite": {}, "opaque": {}, "typed-identifier": {},
}

func convertEntityWithCompiledProfile(entity DrupalEntity, options *format.ParseOptions, compiled *profile.Compiled) (*hubv1.Record, error) {
	mappings, err := compiledDrupalMappings(compiled, false)
	if err != nil {
		return nil, err
	}
	record := &hubv1.Record{}
	set := make(map[string]bool)
	mappedFields := make(map[string]bool)
	partialFields := make(map[string]bool)
	for _, entry := range mappings {
		if entry.mapping.Decode == "none" {
			continue
		}
		raw, exists := entity[entry.mapping.Field.Selector.Path]
		if !exists {
			continue
		}
		path := entry.mapping.Field.Selector.Path
		if entry.mapping.Field.Selector.Where != nil {
			partialFields[path] = true
		}
		selected, present, selectErr := selectDrupalValues(raw, entry.mapping.Field.Selector, entry.mapping.Decode)
		if selectErr != nil {
			return nil, fmt.Errorf("profile mapping %d field %q: %w", entry.mapping.Position+1, entry.mapping.Field.Selector.Path, selectErr)
		}
		if !present {
			continue
		}
		key := entry.mapping.Hub
		if entry.mapping.Merge == profile.MergeFirstNonempty && set[key] {
			continue
		}
		if entry.mapping.Merge == profile.MergeReplace && set[key] {
			if err := clearReplaceableHubValue(record, key); err != nil {
				return nil, fmt.Errorf("profile mapping %d: %w", entry.mapping.Position+1, err)
			}
		}

		var valueSet bool
		if entry.mapping.Decode == "typed-identifier" {
			valueSet, err = processCompiledIdentifiers(record, selected, entry.mapping, compiled)
		} else if entry.mapping.Decode == "file" {
			valueSet, err = processCompiledFiles(record, selected, entry.mapping)
		} else if entry.mapping.Decode == "composite" && compiledPublicationPath(entry.mapping.Hub) {
			valueSet, err = processCompiledPublication(record, selected, entry.mapping)
		} else {
			valueSet, err = processField(record, entry.mapping.Field.Selector.Path, selected, entry.decodeMapping, options)
		}
		if err != nil {
			return nil, fmt.Errorf("profile mapping %d field %q to Hub %q: %w", entry.mapping.Position+1, entry.mapping.Field.Selector.Path, entry.mapping.Hub, err)
		}
		if valueSet {
			set[key] = true
			mappedFields[path] = true
		}
	}

	addDrupalResourceIdentifier(record, entity, options)
	applyDrupalProvenance(record, entity, options, compiled, mappedFields, partialFields)
	return record, nil
}

func compiledPublicationPath(path string) bool {
	switch path {
	case "Publication.Title", "Publication.Issn", "Publication.LIssn",
		"Publication.Volume", "Publication.Issue", "Publication.Pages":
		return true
	default:
		return false
	}
}

// processCompiledPublication assigns a profile-selected composite attribute to
// its exact Hub path. The generic Drupal composite decoder consumes a complete
// related_item or part_detail object and infers several Hub fields at once;
// doing that here would discard the selector boundary expressed by a compiled
// profile and could let one mapping populate another mapping's Hub path.
func processCompiledPublication(record *hubv1.Record, raw json.RawMessage, item profile.CompiledMapping) (bool, error) {
	values, err := ExtractStrings(raw)
	if err != nil {
		return false, err
	}
	value := ""
	for _, candidate := range values {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			value = candidate
			break
		}
	}
	if value == "" {
		return false, nil
	}
	if record.Publication == nil {
		record.Publication = &hubv1.PublicationDetails{}
	}
	switch item.Hub {
	case "Publication.Title":
		record.Publication.Title = value
	case "Publication.Issn":
		record.Publication.Issn = value
	case "Publication.LIssn":
		record.Publication.LIssn = value
	case "Publication.Volume":
		record.Publication.Volume = value
	case "Publication.Issue":
		record.Publication.Issue = value
	case "Publication.Pages":
		record.Publication.Pages = value
	default:
		return false, fmt.Errorf("publication composite cannot assign Hub path %q", item.Hub)
	}
	return true, nil
}

type compiledDrupalMapping struct {
	mapping       profile.CompiledMapping
	decodeMapping mapping.FieldMapping
}

func compiledDrupalMappings(compiled *profile.Compiled, encode bool) ([]compiledDrupalMapping, error) {
	if compiled == nil {
		return nil, fmt.Errorf("compiled profile is required")
	}
	if compiled.System() != "drupal" {
		return nil, fmt.Errorf("profile system %q cannot be used with Drupal", compiled.System())
	}
	resolved := compiled.Mappings()
	result := make([]compiledDrupalMapping, 0, len(resolved))
	var entity *profile.EntitySelector
	for _, item := range resolved {
		codec := item.Decode
		if encode {
			codec = item.Encode
		}
		if _, exists := drupalCodecs[codec]; !exists {
			return nil, fmt.Errorf("profile mapping %d uses unsupported Drupal codec %q", item.Position+1, codec)
		}
		selector := item.Field.Selector
		candidate := profile.EntitySelector{EntityType: selector.EntityType, Bundle: selector.Bundle}
		if entity == nil {
			copy := candidate
			entity = &copy
		} else if *entity != candidate {
			return nil, fmt.Errorf("drupal format profile spans %s/%s and %s/%s; select one entity profile per conversion", entity.EntityType, entity.Bundle, candidate.EntityType, candidate.Bundle)
		}
		decodeMapping := mapping.FieldMapping{}
		if !encode {
			var err error
			decodeMapping, err = compiledToDrupalDecodeMapping(item, codec)
			if err != nil {
				return nil, fmt.Errorf("profile mapping %d: %w", item.Position+1, err)
			}
		}
		result = append(result, compiledDrupalMapping{mapping: item, decodeMapping: decodeMapping})
	}
	return result, nil
}

func compiledToDrupalDecodeMapping(item profile.CompiledMapping, codec string) (mapping.FieldMapping, error) {
	base, qualifier := mapping.IRFieldName(item.Hub)
	field := mapping.FieldMapping{IR: item.Hub, MultiValue: item.Field.Cardinality == -1}
	switch codec {
	case "none":
		return field, nil
	case "typed-identifier":
		if base != "Identifiers" {
			return field, fmt.Errorf("typed-identifier codec requires Hub Identifiers, got %q", item.Hub)
		}
	case "typed-relation":
		field.Type = "typed_relation"
		field.RoleField = "rel_type"
		field.Resolve = referenceEntityType(item)
	case "reference":
		field.Resolve = referenceEntityType(item)
	case "date":
		if base != "Dates" {
			return field, fmt.Errorf("date codec requires Hub Dates, got %q", item.Hub)
		}
		field.Parser = "edtf"
		field.DateType = inferDateType(qualifier, item.Field.Selector.Path)
	case "link":
		field.Type = "uri"
	case "file":
		if base != "Files" {
			return field, fmt.Errorf("file codec requires Hub Files, got %q", item.Hub)
		}
	case "composite":
		field.Type = item.Field.SourceType
	case "text", "integer", "decimal", "boolean", "opaque":
		field.Type = specializedDrupalSourceType(item.Field.SourceType)
	}
	if base == "Relations" {
		field.RelationType = qualifier
		if field.RelationType == "" {
			if relationType, inferred := inferredRelationType(item.Field.Selector.Path); inferred {
				field.RelationType = strings.ToLower(strings.TrimPrefix(relationType.String(), "RELATION_TYPE_"))
			}
		}
	}
	if base == "Subjects" || base == "Genre" {
		field.Vocabulary = inferVocabulary(qualifier, item.Field.Selector.Path)
	}
	return field, nil
}

func specializedDrupalSourceType(sourceType string) string {
	switch sourceType {
	case "typed_relation", "related_item", "part_detail", "textfield_attr", "textarea_attr":
		return sourceType
	default:
		return ""
	}
}

func referenceEntityType(item profile.CompiledMapping) string {
	if item.Field.Reference != nil {
		return item.Field.Reference.EntityType
	}
	return ""
}

func inferDateType(qualifier, field string) string {
	if qualifier != "" {
		return strings.ToLower(strings.ReplaceAll(qualifier, "_", "-"))
	}
	lower := strings.ToLower(field)
	for _, candidate := range []string{"issued", "created", "captured", "copyright", "modified", "available", "submitted", "accepted", "published", "valid", "updated", "collected"} {
		if strings.Contains(lower, candidate) {
			return candidate
		}
	}
	return "other"
}

func inferVocabulary(qualifier, field string) string {
	if qualifier != "" {
		return strings.ToLower(qualifier)
	}
	lower := strings.ToLower(field)
	for _, candidate := range []string{"lcsh", "mesh", "aat", "fast", "ddc", "lcc", "keywords", "genre", "local"} {
		if strings.Contains(lower, candidate) {
			return candidate
		}
	}
	return ""
}

func selectDrupalValues(raw json.RawMessage, selector profile.FieldSelector, codec string) (json.RawMessage, bool, error) {
	if selector.Where == nil && (selector.Attribute == "" || codec == "reference" || codec == "typed-relation" || codec == "file" || codec == "opaque" || codec == "typed-identifier") {
		return raw, hasDrupalValue(raw), nil
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false, fmt.Errorf("expected an array field: %w", err)
	}
	selected := make([]any, 0, len(values))
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			if selector.Where == nil && selector.Attribute == "" {
				selected = append(selected, value)
			}
			continue
		}
		if selector.Where != nil && scalarText(object[selector.Where.Attribute]) != selector.Where.Equals {
			continue
		}
		if selector.Attribute != "" && codec != "typed-identifier" && codec != "reference" && codec != "typed-relation" && codec != "file" && codec != "opaque" {
			projected, exists := object[selector.Attribute]
			if !exists {
				continue
			}
			selected = append(selected, map[string]any{"value": projected})
		} else {
			selected = append(selected, object)
		}
	}
	if len(selected) == 0 {
		return nil, false, nil
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		return nil, false, fmt.Errorf("encoding selected values: %w", err)
	}
	return encoded, true, nil
}

func hasDrupalValue(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "[]" && trimmed != "{}"
}

func scalarText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%g", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func processCompiledIdentifiers(record *hubv1.Record, raw json.RawMessage, item profile.CompiledMapping, compiled *profile.Compiled) (bool, error) {
	var values []string
	var err error
	if item.Field.Selector.Attribute == "" {
		values, err = ExtractStrings(raw)
	} else {
		values, err = selectedAttributeValues(raw, item.Field.Selector.Attribute)
	}
	if err != nil {
		return false, err
	}
	rules := compiled.LookupPlan().Identifiers
	matchingRules := make([]profile.CompiledIdentifierRule, 0)
	for _, rule := range rules {
		if selectorsEqual(rule.Value.Selector, item.Field.Selector) {
			matchingRules = append(matchingRules, rule)
		}
	}
	if len(matchingRules) == 0 {
		return false, fmt.Errorf("typed-identifier mapping has no identity rule for its selector")
	}
	added := false
	for _, value := range values {
		matched := false
		var lastErr error
		for _, rule := range matchingRules {
			identifier, identifierErr := compiled.NewIdentifier(rule.Name, value)
			if identifierErr != nil {
				lastErr = identifierErr
				continue
			}
			if !hasHubIdentifier(record, identifier) {
				record.Identifiers = append(record.Identifiers, identifier)
			}
			matched, added = true, true
			break
		}
		if !matched {
			return false, fmt.Errorf("identifier %q does not satisfy its profile rule: %w", value, lastErr)
		}
	}
	return added, nil
}

func selectedAttributeValues(raw json.RawMessage, attribute string) ([]string, error) {
	var values []map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decoding structured identifier field: %w", err)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if selected := strings.TrimSpace(scalarText(value[attribute])); selected != "" {
			result = append(result, selected)
		}
	}
	return result, nil
}

func selectorsEqual(left, right profile.FieldSelector) bool {
	if left.EntityType != right.EntityType || left.Bundle != right.Bundle || left.Path != right.Path || left.Attribute != right.Attribute {
		return false
	}
	if left.Where == nil || right.Where == nil {
		return left.Where == nil && right.Where == nil
	}
	return *left.Where == *right.Where
}

func hasHubIdentifier(record *hubv1.Record, identifier *hubv1.Identifier) bool {
	for _, existing := range record.GetIdentifiers() {
		if existing.GetScheme() == identifier.GetScheme() && existing.GetNamespaceUri() == identifier.GetNamespaceUri() && existing.GetValue() == identifier.GetValue() && existing.GetIdentityLevel() == identifier.GetIdentityLevel() {
			return true
		}
	}
	return false
}

func processCompiledFiles(record *hubv1.Record, raw json.RawMessage, item profile.CompiledMapping) (bool, error) {
	var values []map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return false, fmt.Errorf("decoding file field: %w", err)
	}
	added := false
	_, role := mapping.IRFieldName(item.Hub)
	for _, value := range values {
		file := &hubv1.File{
			Path: scalarText(value["path"]), Name: scalarText(value["filename"]),
			MimeType: scalarText(value["filemime"]), Uri: scalarText(value["uri"]),
			AccessUrl: scalarText(value["url"]), Role: role,
			Description: scalarText(value["description"]), Checksum: scalarText(value["checksum"]),
			ChecksumAlgorithm: scalarText(value["checksum_algorithm"]),
		}
		if size := strings.TrimSpace(scalarText(value["filesize"])); size != "" {
			parsed, err := strconv.ParseInt(size, 10, 64)
			if err != nil || parsed < 0 {
				return false, fmt.Errorf("file size %q is not a non-negative integer", size)
			}
			file.SizeBytes = parsed
		}
		if file.Name == "" {
			file.Name = scalarText(value["name"])
		}
		if file.Uri == "" && file.AccessUrl == "" && file.Path == "" && file.Name == "" {
			return false, fmt.Errorf("file reference %q has no enriched path, URI, URL, or filename", scalarText(value["target_id"]))
		}
		record.Files = append(record.Files, file)
		added = true
	}
	return added, nil
}

func clearReplaceableHubValue(record *hubv1.Record, path string) error {
	base, subfield := mapping.IRFieldName(path)
	switch base {
	case "Title":
		record.Title = ""
	case "Abstract":
		record.Abstract = ""
	case "Description":
		record.Description = ""
	case "Publisher":
		record.Publisher = ""
	case "PlacePublished":
		record.PlacePublished = ""
	case "PhysicalDesc":
		record.PhysicalDesc = ""
	case "TableOfContents":
		record.TableOfContents = ""
	case "Source":
		record.Source = ""
	case "DigitalOrigin":
		record.DigitalOrigin = ""
	case "ResourceType":
		record.ResourceType = nil
	case "DegreeInfo":
		if record.DegreeInfo == nil {
			return nil
		}
		switch subfield {
		case "DegreeName":
			record.DegreeInfo.DegreeName = ""
		case "DegreeLevel":
			record.DegreeInfo.DegreeLevel = ""
		case "Department":
			record.DegreeInfo.Department = ""
		case "Institution":
			record.DegreeInfo.Institution = ""
		default:
			return fmt.Errorf("replace is unsupported for Hub path %q", path)
		}
	case "Extra":
		if record.Extra != nil {
			delete(record.Extra.Fields, subfield)
		}
	default:
		return fmt.Errorf("replace is unsupported for repeated or structured Hub path %q", path)
	}
	return nil
}

func applyDrupalProvenance(record *hubv1.Record, entity DrupalEntity, options *format.ParseOptions, compiled *profile.Compiled, mapped, partial map[string]bool) {
	unmapped := make([]string, 0)
	preserved := make(map[string]any)
	for field, raw := range entity {
		if mapped[field] && !partial[field] {
			continue
		}
		unmapped = append(unmapped, field)
		var value any
		if json.Unmarshal(raw, &value) == nil {
			preserved[field] = value
		}
	}
	sort.Strings(unmapped)
	if len(preserved) != 0 {
		hub.SetExtra(record, "drupal_unmapped", preserved)
	}
	sourceID := sourceIDFromDrupalEntity(entity)
	origin, _ := provenanceuri.Normalize(options.BaseURL, provenanceuri.Options{})
	record.SourceInfo = &hubv1.SourceInfo{
		Format:             "drupal",
		SourceId:           sourceID,
		Profile:            compiled.Name(),
		ProfileFingerprint: compiled.Fingerprint(),
		ModelFingerprint:   compiled.ModelFingerprint(),
		UnmappedFields:     unmapped,
		Origin:             origin,
	}
	if sourceURI, err := provenanceuri.Normalize(options.SourceName, provenanceuri.Options{}); err == nil {
		record.SourceInfo.SourceUri = sourceURI
	}
}

func sourceIDFromDrupalEntity(entity DrupalEntity) string {
	for _, field := range []string{"uuid", "drupal_internal__nid", "nid"} {
		if raw, exists := entity[field]; exists {
			if value, _ := ExtractString(raw); value != "" {
				return value
			}
		}
	}
	return ""
}
