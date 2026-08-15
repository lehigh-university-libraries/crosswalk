package omeka_s

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"google.golang.org/protobuf/types/known/structpb"
)

var omekaSCodecs = map[string]struct{}{
	"none": {}, "text": {}, "integer": {}, "decimal": {}, "boolean": {},
	"date": {}, "link": {}, "reference": {}, "typed-relation": {},
	"file": {}, "composite": {}, "opaque": {}, "typed-identifier": {},
}

func applyCompiledProfile(record *hubv1.Record, source *resource, schema *schemaModel, options *format.ParseOptions) (map[string]bool, error) {
	compiled := options.SystemProfile
	mappings, selector, err := compiledOmekaProfile(compiled)
	if err != nil {
		return nil, err
	}
	if err := requireResourceTemplate(source, selector.Bundle); err != nil {
		return nil, err
	}
	// Omeka's display title and visibility are required resource-envelope
	// fields. Initialize them independently of the editable metadata mappings
	// so a profile may omit o:title or o:is_public without erasing the resource
	// identity and access envelope.
	record.Title = source.title
	record.IsPublic = source.isPublic

	set := make(map[string]bool)
	mapped := make(map[string]bool)
	for _, mapping := range mappings {
		if _, exists := omekaSCodecs[mapping.Decode]; !exists {
			return nil, fmt.Errorf("profile mapping %d uses unsupported Omeka S codec %q", mapping.Position+1, mapping.Decode)
		}
		if mapping.Decode == "none" {
			continue
		}
		if _, exists := schemaProperty(source, mapping.Field.Selector.Path); exists {
			mapped[mapping.Field.Selector.Path] = mapping.Field.Selector.Where == nil
		}
		values, present, err := selectOmekaValues(source, schema, mapping.Field.Selector)
		if err != nil {
			return nil, fmt.Errorf("profile mapping %d field %q: %w", mapping.Position+1, mapping.Field.Selector.Path, err)
		}
		if !present {
			continue
		}
		if mapping.Merge == profile.MergeFirstNonempty && set[mapping.Hub] {
			continue
		}
		if mapping.Merge == profile.MergeReplace && set[mapping.Hub] {
			if err := clearOmekaHubValue(record, mapping.Hub); err != nil {
				return nil, fmt.Errorf("profile mapping %d: %w", mapping.Position+1, err)
			}
		}
		valueSet, err := applyOmekaMapping(record, source, values, mapping, compiled, options)
		if err != nil {
			return nil, fmt.Errorf("profile mapping %d field %q to Hub %q: %w", mapping.Position+1, mapping.Field.Selector.Path, mapping.Hub, err)
		}
		if valueSet {
			set[mapping.Hub] = true
		}
	}
	return mapped, nil
}

func compiledOmekaProfile(compiled *profile.Compiled) ([]profile.CompiledMapping, profile.FieldSelector, error) {
	if compiled == nil {
		return nil, profile.FieldSelector{}, fmt.Errorf("compiled Omeka S profile is required")
	}
	if compiled.System() != ModelSystem {
		return nil, profile.FieldSelector{}, fmt.Errorf("profile system %q cannot be used with Omeka S", compiled.System())
	}
	mappings := compiled.Mappings()
	if len(mappings) == 0 {
		return nil, profile.FieldSelector{}, fmt.Errorf("omeka S profile has no mappings")
	}
	selector := mappings[0].Field.Selector
	if selector.EntityType != ModelResourceEntity {
		return nil, profile.FieldSelector{}, fmt.Errorf("omeka S profile entity type %q must be %q", selector.EntityType, ModelResourceEntity)
	}
	for _, mapping := range mappings[1:] {
		candidate := mapping.Field.Selector
		if candidate.EntityType != selector.EntityType || candidate.Bundle != selector.Bundle {
			return nil, profile.FieldSelector{}, fmt.Errorf("omeka S profile spans more than one model entity")
		}
	}
	return mappings, selector, nil
}

// omekaProfileApplies reports whether a resource belongs to the compiled
// content contract. Item sets and media can accompany a template-specific item
// in a complete acquisition snapshot; a different or absent template on those
// support resources does not make the item snapshot invalid. Items remain
// strict because silently applying the wrong item-template contract would
// mislabel editable metadata.
func omekaProfileApplies(source *resource, compiled *profile.Compiled) (bool, error) {
	_, selector, err := compiledOmekaProfile(compiled)
	if err != nil {
		return false, err
	}
	if selector.Bundle == "" {
		return true, nil
	}
	if err := requireResourceTemplate(source, selector.Bundle); err != nil {
		if source.kind != kindItem {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func schemaProperty(source *resource, path string) (termValues, bool) {
	for _, propertyValues := range source.properties {
		if propertyValues.term == path {
			return propertyValues, true
		}
	}
	return termValues{}, false
}

func requireResourceTemplate(source *resource, bundle string) error {
	if bundle == "" {
		return nil
	}
	wanted, err := strconv.ParseInt(bundle, 10, 64)
	if err != nil || wanted <= 0 {
		return fmt.Errorf("omeka S profile has invalid resource-template bundle %q", bundle)
	}
	if source.resourceTemplate == nil {
		return fmt.Errorf("resource has no template; profile requires resource template %d", wanted)
	}
	if source.resourceTemplate.id != wanted {
		return fmt.Errorf("resource uses template %d; profile requires resource template %d", source.resourceTemplate.id, wanted)
	}
	return nil
}

func selectOmekaValues(source *resource, schema *schemaModel, selector profile.FieldSelector) ([]valueObject, bool, error) {
	values := omekaCoreValues(source, schema, selector.Path)
	if values == nil {
		if propertyValues, exists := schemaProperty(source, selector.Path); exists {
			values = append([]valueObject(nil), propertyValues.values...)
		}
	}
	if len(values) == 0 {
		return nil, false, nil
	}
	selected := make([]valueObject, 0, len(values))
	for _, value := range values {
		if selector.Where != nil {
			candidate, err := omekaValueAttribute(value, selector.Where.Attribute)
			if err != nil {
				return nil, false, err
			}
			if candidate != selector.Where.Equals {
				continue
			}
		}
		if selector.Attribute != "" {
			projected, err := omekaValueAttribute(value, selector.Attribute)
			if err != nil {
				return nil, false, err
			}
			if projected == "" {
				continue
			}
			value = valueObject{typeName: "literal", literal: projected, propertyID: value.propertyID}
		}
		selected = append(selected, value)
	}
	return selected, len(selected) != 0, nil
}

func omekaCoreValues(source *resource, schema *schemaModel, path string) []valueObject {
	switch path {
	case "o:id":
		return []valueObject{{typeName: "literal", literal: strconv.FormatInt(source.id, 10)}}
	case "o:title":
		if source.title != "" {
			return []valueObject{{typeName: "literal", literal: source.title}}
		}
	case "o:created":
		if source.created != "" {
			return []valueObject{{typeName: "literal", literal: source.created}}
		}
	case "o:modified":
		if source.modified != "" {
			return []valueObject{{typeName: "literal", literal: source.modified}}
		}
	case "o:is_public":
		return []valueObject{{typeName: "literal", literal: strconv.FormatBool(source.isPublic)}}
	case "o:resource_class":
		if source.resourceClass != nil {
			value := valueObject{typeName: "resource", resourceID: source.resourceClass.id, uri: source.resourceClass.uri, displayTitle: source.resourceClass.title}
			if schema != nil {
				if class, exists := schema.classesByID[source.resourceClass.id]; exists {
					value.displayTitle = firstNonempty(class.term, class.label, value.displayTitle)
				}
			}
			return []valueObject{value}
		}
	case "o:resource_template":
		if source.resourceTemplate != nil {
			value := valueObject{typeName: "resource", resourceID: source.resourceTemplate.id, uri: source.resourceTemplate.uri, displayTitle: source.resourceTemplate.title}
			if schema != nil {
				if template, exists := schema.templatesByID[source.resourceTemplate.id]; exists {
					value.displayTitle = firstNonempty(template.label, value.displayTitle)
				}
			}
			return []valueObject{value}
		}
	}
	return nil
}

func omekaValueAttribute(value valueObject, attribute string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(attribute)) {
	case "value", "@value", "literal":
		return displayValue(value), nil
	case "type":
		return value.typeName, nil
	case "property_id":
		return value.propertyID, nil
	case "property_label":
		return value.propertyLabel, nil
	case "language", "@language":
		return value.language, nil
	case "uri", "@id":
		return value.uri, nil
	case "label":
		return value.label, nil
	case "resource_id":
		if value.resourceID != 0 {
			return strconv.FormatInt(value.resourceID, 10), nil
		}
		return "", nil
	case "resource_name":
		return value.resourceName, nil
	case "display_title":
		return value.displayTitle, nil
	case "is_public":
		if value.isPublic == nil {
			return "", nil
		}
		return strconv.FormatBool(*value.isPublic), nil
	default:
		return "", fmt.Errorf("unsupported Omeka S value attribute %q", attribute)
	}
}

func applyOmekaMapping(record *hubv1.Record, source *resource, values []valueObject, mapping profile.CompiledMapping, compiled *profile.Compiled, options *format.ParseOptions) (bool, error) {
	base, qualifier, _ := strings.Cut(mapping.Hub, ".")
	values, extraValues, err := decodeOmekaProfileValues(values, mapping.Decode, base)
	if err != nil {
		return false, err
	}
	first := firstDisplay(values)
	switch base {
	case "Title":
		return setOmekaString(&record.Title, first, options), nil
	case "FullTitle":
		return setOmekaString(&record.FullTitle, first, options), nil
	case "AltTitle":
		return appendOmekaStrings(&record.AltTitle, values, options), nil
	case "Abstract":
		return setOmekaString(&record.Abstract, first, options), nil
	case "Description":
		return setOmekaString(&record.Description, first, options), nil
	case "Contributors":
		before := len(record.Contributors)
		role := strings.ToLower(firstNonempty(qualifier, "contributor"))
		appendContributors(record, values, role)
		return len(record.Contributors) != before, nil
	case "Dates":
		before := len(record.Dates)
		appendDates(record, values, omekaDateType(qualifier))
		return len(record.Dates) != before, nil
	case "ResourceType":
		if first == "" {
			return false, nil
		}
		record.ResourceType = &hubv1.ResourceType{Type: omekaResourceType(first), Original: first, Vocabulary: "Omeka S profile"}
		return true, nil
	case "Genre":
		before := len(record.Genres)
		for _, value := range values {
			if subject := omekaSubject(value); subject != nil {
				record.Genres = append(record.Genres, subject)
			}
		}
		return len(record.Genres) != before, nil
	case "Subjects":
		before := len(record.Subjects)
		appendSubjects(record, values)
		return len(record.Subjects) != before, nil
	case "Language":
		return appendOmekaCompatibilityValues(record, base, values, options), nil
	case "Publisher":
		return appendOmekaCompatibilityValues(record, base, values, options), nil
	case "PlacePublished":
		return appendOmekaCompatibilityValues(record, base, values, options), nil
	case "Rights":
		before := len(record.Rights)
		appendProfileRights(record, qualifier, values)
		return len(record.Rights) != before, nil
	case "Identifiers":
		return appendProfileIdentifiers(record, source, values, mapping.Field.Selector, compiled)
	case "Relations":
		before := len(record.Relations)
		appendProfileRelations(record, qualifier, values)
		return len(record.Relations) != before, nil
	case "PhysicalDesc":
		return appendOmekaCompatibilityValues(record, base, values, options), nil
	case "Notes":
		return appendOmekaStrings(&record.Notes, values, options), nil
	case "TableOfContents":
		return setOmekaString(&record.TableOfContents, first, options), nil
	case "Source":
		return setOmekaString(&record.Source, first, options), nil
	case "DigitalOrigin":
		return setOmekaString(&record.DigitalOrigin, first, options), nil
	case "Edition":
		return appendOmekaCompatibilityValues(record, base, values, options), nil
	case "Version":
		return setOmekaString(&record.Version, first, options), nil
	case "PreferredCitation":
		return setOmekaString(&record.PreferredCitation, first, options), nil
	case "Departments":
		return appendOmekaStrings(&record.Departments, values, options), nil
	case "AccessCondition":
		return setOmekaString(&record.AccessCondition, first, options), nil
	case "LocalRestriction":
		return setOmekaString(&record.LocalRestriction, first, options), nil
	case "IsPublic":
		parsed, err := strconv.ParseBool(first)
		if err != nil {
			return false, fmt.Errorf("expected a boolean, got %q", first)
		}
		record.IsPublic = parsed
		return true, nil
	case "AddCoverpage":
		parsed, err := strconv.ParseBool(first)
		if err != nil {
			return false, fmt.Errorf("expected a boolean, got %q", first)
		}
		record.AddCoverpage = parsed
		return true, nil
	case "PPI", "PageCount":
		parsed, err := strconv.ParseInt(first, 10, 32)
		if err != nil {
			return false, fmt.Errorf("expected a 32-bit integer, got %q", first)
		}
		if base == "PPI" {
			record.Ppi = int32(parsed)
		} else {
			record.PageCount = int32(parsed)
		}
		return true, nil
	case "Files":
		before := len(record.Files)
		for _, value := range values {
			uri := value.uri
			if uri == "" && value.typeName == "literal" {
				uri = value.literal
			}
			if uri != "" {
				record.Files = append(record.Files, &hubv1.File{AccessUrl: uri, Role: strings.ToLower(qualifier)})
			}
		}
		return len(record.Files) != before, nil
	case "Extra":
		return setOmekaExtra(record, qualifier, extraValues, mapping.Merge)
	default:
		return false, fmt.Errorf("unsupported Hub path %q", mapping.Hub)
	}
}

func appendProfileRights(record *hubv1.Record, qualifier string, values []valueObject) {
	for _, value := range values {
		display := displayValue(value)
		if display == "" {
			continue
		}
		rights := &hubv1.Rights{}
		switch strings.ToLower(qualifier) {
		case "license":
			rights.License = display
		case "holder":
			rights.Holder = display
		case "uri":
			rights.Uri = value.uri
			if rights.Uri == "" {
				rights.Uri = display
			}
		default:
			rights.Statement = display
		}
		if value.typeName == "uri" || strings.HasPrefix(value.typeName, "resource") {
			rights.Uri = value.uri
		}
		record.Rights = append(record.Rights, rights)
	}
}

func setOmekaString(target *string, value string, options *format.ParseOptions) bool {
	value = cleanOmekaText(value, options)
	if value == "" {
		return false
	}
	*target = value
	return true
}

func appendOmekaStrings(target *[]string, values []valueObject, options *format.ParseOptions) bool {
	before := len(*target)
	for _, value := range values {
		if candidate := cleanOmekaText(displayValue(value), options); candidate != "" {
			*target = appendUnique(*target, candidate)
		}
	}
	return len(*target) != before
}

func appendOmekaCompatibilityValues(record *hubv1.Record, field string, values []valueObject, options *format.ParseOptions) bool {
	candidates := make([]string, 0, len(values))
	for _, value := range values {
		if candidate := cleanOmekaText(displayValue(value), options); candidate != "" {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return false
	}

	switch field {
	case "Language":
		hub.SetLanguages(record, append(hub.GetLanguages(record), candidates...))
	case "Publisher":
		hub.SetPublishers(record, append(hub.GetPublishers(record), candidates...))
	case "PlacePublished":
		hub.SetPlacesPublished(record, append(hub.GetPlacesPublished(record), candidates...))
	case "PhysicalDesc":
		hub.SetPhysicalDescriptions(record, append(hub.GetPhysicalDescriptions(record), candidates...))
	case "Edition":
		hub.SetEditions(record, append(hub.GetEditions(record), candidates...))
	default:
		return false
	}
	return true
}

func cleanOmekaText(value string, options *format.ParseOptions) string {
	if options != nil && options.StripHTML {
		return helpers.CleanText(value)
	}
	return strings.TrimSpace(value)
}

func omekaDateType(qualifier string) hubv1.DateType {
	switch strings.ToLower(qualifier) {
	case "issued":
		return hubv1.DateType_DATE_TYPE_ISSUED
	case "created":
		return hubv1.DateType_DATE_TYPE_CREATED
	case "modified":
		return hubv1.DateType_DATE_TYPE_MODIFIED
	case "available":
		return hubv1.DateType_DATE_TYPE_AVAILABLE
	case "submitted":
		return hubv1.DateType_DATE_TYPE_SUBMITTED
	case "accepted":
		return hubv1.DateType_DATE_TYPE_ACCEPTED
	case "published":
		return hubv1.DateType_DATE_TYPE_PUBLISHED
	default:
		return hubv1.DateType_DATE_TYPE_OTHER
	}
}

func omekaSubject(value valueObject) *hubv1.Subject {
	display := displayValue(value)
	if display == "" {
		return nil
	}
	subject := &hubv1.Subject{Value: display, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL}
	if value.typeName == "uri" || strings.HasPrefix(value.typeName, "resource") {
		subject.Uri = value.uri
	}
	if value.resourceID != 0 {
		subject.SourceId = strconv.FormatInt(value.resourceID, 10)
	}
	return subject
}

func appendProfileIdentifiers(record *hubv1.Record, source *resource, values []valueObject, selector profile.FieldSelector, compiled *profile.Compiled) (bool, error) {
	registry := compiled.IdentifierRegistry()
	rules := matchingOmekaIdentifierRules(compiled.LookupPlan().Identifiers, selector)
	added := false
	for _, value := range values {
		candidate, namespace := omekaIdentifierCandidate(value, source)
		if candidate == "" {
			continue
		}
		detected := registry.DetectScheme(candidate)
		var identifier *hubv1.Identifier
		var claimedRule *profile.CompiledIdentifierRule
		var claimedErr error
		for _, rule := range rules {
			_, builtIn := hub.DefaultIdentifierRegistry().Rule(rule.Scheme)
			claimsRule := omekaIdentifierClaimsRule(candidate, detected, rule, rules)
			if builtIn && rule.Scheme != detected && !claimsRule {
				continue
			}
			converted, err := compiled.NewIdentifier(rule.Name, candidate)
			if err != nil {
				if claimsRule && claimedRule == nil {
					copy := rule
					claimedRule = &copy
					claimedErr = err
				}
				continue
			}
			identifier = converted
			break
		}
		if identifier == nil {
			if claimedRule != nil {
				return false, fmt.Errorf("identifier %q claims profile rule %q but is invalid: %w", candidate, claimedRule.Name, claimedErr)
			}
			fallback := &hubv1.Identifier{Value: candidate, Scheme: detected}
			if detected == "local" {
				fallback.NamespaceUri = namespace
				fallback.IdentityLevel = hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD
			}
			converted, err := registry.CanonicalizeIdentifier(fallback)
			if err != nil {
				return false, fmt.Errorf("canonicalizing identifier %q: %w", candidate, err)
			}
			identifier = converted
		}
		if !identifierExists(record.Identifiers, identifier) {
			record.Identifiers = append(record.Identifiers, identifier)
			added = true
		}
	}
	return added, nil
}

// omekaIdentifierClaimsRule distinguishes a malformed typed identifier from
// an ordinary local value in a multi-purpose property such as
// dcterms:identifier. A dedicated one-rule field is an explicit claim; for a
// shared field, a scheme/name prefix or registered authority prefix is needed.
func omekaIdentifierClaimsRule(candidate, detected string, rule profile.CompiledIdentifierRule, rules []profile.CompiledIdentifierRule) bool {
	if strings.EqualFold(detected, rule.Scheme) || len(rules) == 1 {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(candidate))
	for _, prefix := range []string{rule.Scheme + ":", rule.Name + ":", rule.Namespace} {
		prefix = strings.ToLower(strings.TrimSpace(prefix))
		if prefix != "" && strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	registered, exists := hub.DefaultIdentifierRegistry().Rule(rule.Scheme)
	if !exists {
		return false
	}
	for _, prefix := range registered.Prefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

func matchingOmekaIdentifierRules(rules []profile.CompiledIdentifierRule, selector profile.FieldSelector) []profile.CompiledIdentifierRule {
	result := make([]profile.CompiledIdentifierRule, 0)
	for _, rule := range rules {
		if omekaSelectorsEqual(rule.Value.Selector, selector) {
			result = append(result, rule)
		}
	}
	return result
}

func omekaSelectorsEqual(left, right profile.FieldSelector) bool {
	if left.EntityType != right.EntityType || left.Bundle != right.Bundle || left.Path != right.Path || left.Attribute != right.Attribute {
		return false
	}
	if left.Where == nil || right.Where == nil {
		return left.Where == nil && right.Where == nil
	}
	return *left.Where == *right.Where
}

func omekaIdentifierCandidate(value valueObject, source *resource) (string, string) {
	namespace := installationNamespace(source.uri)
	switch value.typeName {
	case "uri":
		return strings.TrimSpace(value.uri), namespace
	case "resource", "resource:item", "resource:itemset", "resource:media":
		if value.resourceID == 0 {
			return "", namespace
		}
		if value.uri != "" {
			namespace = resourceNamespace(value.uri)
		}
		return strconv.FormatInt(value.resourceID, 10), namespace
	default:
		return strings.TrimSpace(displayValue(value)), namespace
	}
}

func appendProfileRelations(record *hubv1.Record, qualifier string, values []valueObject) {
	relationType := relationTypeForProfile(qualifier)
	for _, value := range values {
		display := displayValue(value)
		if display == "" {
			continue
		}
		relation := &hubv1.Relation{Type: relationType, TargetTitle: display}
		switch value.typeName {
		case "uri":
			relation.TargetUri = value.uri
			relation.TargetId = value.uri
			relation.TargetIdType = hubv1.IdentifierType_IDENTIFIER_TYPE_URL
		case "resource", "resource:item", "resource:itemset", "resource:media":
			relation.TargetId = strconv.FormatInt(value.resourceID, 10)
			relation.SourceId = relation.TargetId
			relation.TargetIdType = hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
			relation.TargetUri = value.uri
		default:
			relation.TargetId = display
			relation.TargetIdType = hub.DetectIdentifierType(display)
		}
		record.Relations = append(record.Relations, relation)
	}
}

func relationTypeForProfile(qualifier string) hubv1.RelationType {
	switch strings.ToLower(qualifier) {
	case "haspart":
		return hubv1.RelationType_RELATION_TYPE_HAS_PART
	case "partof":
		return hubv1.RelationType_RELATION_TYPE_PART_OF
	case "hasformat":
		return hubv1.RelationType_RELATION_TYPE_HAS_FORMAT
	case "formatof":
		return hubv1.RelationType_RELATION_TYPE_FORMAT_OF
	case "references":
		return hubv1.RelationType_RELATION_TYPE_REFERENCES
	case "iscitedby":
		return hubv1.RelationType_RELATION_TYPE_IS_CITED_BY
	case "replaces":
		return hubv1.RelationType_RELATION_TYPE_REPLACES
	case "isreplacedby":
		return hubv1.RelationType_RELATION_TYPE_IS_REPLACED_BY
	default:
		return hubv1.RelationType_RELATION_TYPE_RELATED_TO
	}
}

func setOmekaExtra(record *hubv1.Record, key string, converted []any, merge profile.MergePolicy) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("extra mapping requires a key")
	}
	if len(converted) == 0 {
		return false, nil
	}
	if merge == profile.MergeAppend && record.Extra != nil {
		if existing, exists := record.Extra.Fields[key]; exists {
			prior, ok := existing.AsInterface().([]any)
			if !ok {
				prior = []any{existing.AsInterface()}
			}
			converted = append(prior, converted...)
		}
	}
	var stored any = converted
	if len(converted) == 1 {
		stored = converted[0]
	}
	encoded, err := structpb.NewValue(stored)
	if err != nil {
		return false, fmt.Errorf("encoding Extra.%s: %w", key, err)
	}
	if record.Extra == nil {
		record.Extra = &structpb.Struct{Fields: make(map[string]*structpb.Value)}
	}
	record.Extra.Fields[key] = encoded
	return true, nil
}

func clearOmekaHubValue(record *hubv1.Record, path string) error {
	base, qualifier, _ := strings.Cut(path, ".")
	switch base {
	case "Title":
		record.Title = ""
	case "FullTitle":
		record.FullTitle = ""
	case "AltTitle":
		record.AltTitle = nil
	case "Abstract":
		record.Abstract = ""
	case "Description":
		record.Description = ""
	case "Contributors":
		record.Contributors = nil
	case "Dates":
		record.Dates = nil
	case "ResourceType":
		record.ResourceType = nil
	case "Genre":
		record.Genres = nil
	case "Subjects":
		record.Subjects = nil
	case "Language":
		hub.SetLanguages(record, nil)
	case "Publisher":
		hub.SetPublishers(record, nil)
	case "PlacePublished":
		hub.SetPlacesPublished(record, nil)
	case "Rights":
		record.Rights = nil
	case "Identifiers":
		record.Identifiers = nil
	case "Relations":
		record.Relations = nil
	case "PhysicalDesc":
		hub.SetPhysicalDescriptions(record, nil)
	case "Notes":
		record.Notes = nil
	case "TableOfContents":
		record.TableOfContents = ""
	case "Source":
		record.Source = ""
	case "DigitalOrigin":
		record.DigitalOrigin = ""
	case "Edition":
		hub.SetEditions(record, nil)
	case "Version":
		record.Version = ""
	case "PreferredCitation":
		record.PreferredCitation = ""
	case "Departments":
		record.Departments = nil
	case "AccessCondition":
		record.AccessCondition = ""
	case "LocalRestriction":
		record.LocalRestriction = ""
	case "IsPublic":
		record.IsPublic = false
	case "AddCoverpage":
		record.AddCoverpage = false
	case "PPI":
		record.Ppi = 0
	case "PageCount":
		record.PageCount = 0
	case "Files":
		record.Files = nil
	case "Extra":
		if record.Extra != nil {
			delete(record.Extra.Fields, qualifier)
		}
	default:
		return fmt.Errorf("replace is unsupported for Hub path %q", path)
	}
	return nil
}
