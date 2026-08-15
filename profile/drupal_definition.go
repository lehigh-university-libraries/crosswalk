package profile

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/model"
)

// InstitutionalIdentifierOptions defines an explicitly scoped identifier
// owned by one institution. All fields are required when this option is used;
// Crosswalk never guesses a local identifier authority or validation pattern.
type InstitutionalIdentifierOptions struct {
	Attribute     string
	Scheme        string
	NamespaceURI  string
	Pattern       string
	IdentityLevel IdentifierIdentityLevel
}

// DrupalDefinitionOptions selects one Drupal entity and optional explicit
// institution identifier policy for a deterministic starter definition.
type DrupalDefinitionOptions struct {
	Name                    string
	EntityType              string
	Bundle                  string
	InstitutionalIdentifier *InstitutionalIdentifierOptions
}

// NewDrupalDefinition builds a conservative, ordered starter definition from
// one entity in a compiled Drupal model. It performs no filesystem or network
// access and seals and compiles the result before returning it.
func NewDrupalDefinition(snapshot *model.Snapshot, options DrupalDefinitionOptions) (*Definition, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("building Drupal profile: model snapshot is nil")
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("building Drupal profile: %w", err)
	}
	if snapshot.System != "drupal" {
		return nil, fmt.Errorf("building Drupal profile: model system %q is not drupal", snapshot.System)
	}
	entity, exists := snapshot.Entity(options.EntityType, options.Bundle)
	if !exists {
		return nil, fmt.Errorf("building Drupal profile: entity %s/%s is not in the model", options.EntityType, options.Bundle)
	}
	if len(entity.Fields) == 0 {
		return nil, fmt.Errorf("building Drupal profile: entity %s/%s has no fields", options.EntityType, options.Bundle)
	}

	fields := append([]model.Field(nil), entity.Fields...)
	sort.Slice(fields, func(left, right int) bool { return fields[left].Path < fields[right].Path })
	identity, err := newDrupalIdentity(entity, options.InstitutionalIdentifier)
	if err != nil {
		return nil, fmt.Errorf("building Drupal profile: %w", err)
	}
	mappings := make([]Mapping, 0, len(fields))
	for _, field := range fields {
		if composite := drupalPublicationMappings(entity, field); len(composite) != 0 {
			mappings = append(mappings, composite...)
			continue
		}
		hubPath := drupalHubPath(field)
		if hubPath == "Identifiers" {
			identifierMappings := drupalIdentifierMappings(field, identity)
			if len(identifierMappings) != 0 {
				mappings = append(mappings, identifierMappings...)
				continue
			}
			// A field that merely resembles an identifier is not enough to
			// assign identity semantics. Preserve it until a profile author
			// supplies an explicit rule.
			hubPath = "Extra." + field.Path
		}
		codec := drupalCodec(field, hubPath)
		merge := MergeFirstNonempty
		if repeatedHubPath(hubPath) || (strings.HasPrefix(hubPath, "Extra.") && field.Cardinality != 1) {
			merge = MergeAppend
		}
		mappings = append(mappings, Mapping{
			Field: FieldSelector{EntityType: entity.EntityType, Bundle: entity.Bundle, Path: field.Path},
			Hub:   hubPath, Decode: codec, Encode: codec, Merge: merge,
		})
	}

	description := fmt.Sprintf("Starter profile for Drupal %s/%s", entity.EntityType, entity.Bundle)
	if snapshot.Provenance.SiteName != "" {
		description += " at " + snapshot.Provenance.SiteName
	}
	definition := &Definition{
		Version: CurrentDefinitionVersion, Name: options.Name, Description: description,
		System: snapshot.System, ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: mappings, Identity: identity,
	}
	if err := definition.SealFingerprint(); err != nil {
		return nil, fmt.Errorf("sealing Drupal profile: %w", err)
	}
	if _, err := Compile(snapshot, definition); err != nil {
		return nil, fmt.Errorf("compiling Drupal profile: %w", err)
	}
	return definition, nil
}

func drupalPublicationMappings(entity model.Entity, field model.Field) []Mapping {
	selector := func(attribute, predicate, equals string) FieldSelector {
		result := FieldSelector{
			EntityType: entity.EntityType, Bundle: entity.Bundle, Path: field.Path,
			Attribute: attribute,
		}
		if predicate != "" {
			result.Where = &FieldPredicate{Attribute: predicate, Equals: equals}
		}
		return result
	}
	mapping := func(fieldSelector FieldSelector, hubPath string, merge MergePolicy) Mapping {
		return Mapping{Field: fieldSelector, Hub: hubPath, Decode: "composite", Encode: "composite", Merge: merge}
	}
	switch field.SourceType {
	case "related_item":
		return []Mapping{
			mapping(selector("title", "", ""), "Publication.Title", MergeFirstNonempty),
			mapping(selector("identifier", "identifier_type", "l-issn"), "Publication.LIssn", MergeFirstNonempty),
			mapping(selector("identifier", "identifier_type", "issn"), "Publication.Issn", MergeFirstNonempty),
		}
	case "part_detail":
		return []Mapping{
			mapping(selector("number", "type", "volume"), "Publication.Volume", MergeFirstNonempty),
			mapping(selector("number", "type", "issue"), "Publication.Issue", MergeFirstNonempty),
			mapping(selector("number", "type", "page"), "Publication.Pages", MergeFirstNonempty),
		}
	default:
		return nil
	}
}

func drupalIdentifierMappings(field model.Field, identity *IdentityPolicy) []Mapping {
	if identity == nil {
		return nil
	}
	result := make([]Mapping, 0)
	seen := make(map[string]struct{})
	for _, rule := range identity.Identifiers {
		if rule.Value.Path != field.Path {
			continue
		}
		// One stored Drupal value can participate in more than one explicit
		// identity policy. DOI is the important case: Drupal normally stores
		// both work and version DOIs under attr0=doi, while the source adapter
		// retains the more precise identity level. A single typed-identifier
		// mapping evaluates every rule for its selector, so emitting the same
		// mapping twice would only duplicate parse/serialize work and output.
		key := selectorKey(rule.Value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, Mapping{
			Field: cloneSelector(rule.Value), Hub: "Identifiers",
			Decode: "typed-identifier", Encode: "typed-identifier", Merge: MergeAppend,
		})
	}
	return result
}

func newDrupalIdentity(entity model.Entity, institutional *InstitutionalIdentifierOptions) (*IdentityPolicy, error) {
	repository := EntitySelector{EntityType: entity.EntityType, Bundle: entity.Bundle}
	identity := &IdentityPolicy{Version: CurrentIdentityVersion, Repository: repository}
	identifierField, hasIdentifierField := drupalIdentifierField(entity.Fields)
	for _, descriptor := range drupalIdentifierDescriptors() {
		selector, exists := drupalIdentifierSelector(entity, identifierField, hasIdentifierField, descriptor)
		if !exists {
			continue
		}
		registryRule, exists := hub.DefaultIdentifierRegistry().Rule(descriptor.scheme)
		if !exists {
			return nil, fmt.Errorf("built-in identifier scheme %q is unavailable", descriptor.scheme)
		}
		strength := IdentifierCorroborating
		if identifierLevelIsExact(registryRule.ExactIdentityLevels, descriptor.level) {
			strength = IdentifierStrong
		}
		lookupOperator := LookupExact
		if selector.Attribute != "" || selector.Where != nil {
			lookupOperator = LookupContains
		}
		variants := []string{"canonical"}
		switch descriptor.scheme {
		case "doi":
			variants = append(variants, "doi-url")
		case "wos":
			variants = append(variants, "wos-bare")
		}
		identity.Identifiers = append(identity.Identifiers, IdentifierRule{
			Name: descriptor.ruleName(), Scheme: descriptor.scheme, IdentityLevel: descriptor.level,
			Value: selector, Pattern: registryRule.Pattern, Canonicalizer: "canonical",
			Strength: strength, Scope: ScopeGlobal, Namespace: registryRule.NamespaceURI,
			Lookup: LookupRule{Fields: []FieldSelector{selector}, Operator: lookupOperator, Variants: variants},
		})
	}
	if institutional != nil {
		if !hasIdentifierField {
			return nil, fmt.Errorf("institutional identifier requires a structured identifier field")
		}
		if strings.TrimSpace(institutional.Attribute) == "" || strings.TrimSpace(institutional.Scheme) == "" || strings.TrimSpace(institutional.NamespaceURI) == "" || strings.TrimSpace(institutional.Pattern) == "" {
			return nil, fmt.Errorf("institutional identifier attribute, scheme, namespace URI, and pattern are required")
		}
		if _, builtIn := hub.DefaultIdentifierRegistry().Rule(institutional.Scheme); builtIn {
			return nil, fmt.Errorf("institutional identifier scheme %q is built in; use a distinct institution scheme", institutional.Scheme)
		}
		level := institutional.IdentityLevel
		if level == "" {
			level = IdentitySourceRecord
		}
		selector := structuredIdentifierSelector(entity, identifierField, institutional.Attribute)
		namespace, err := canonicalInstitutionNamespace(institutional.NamespaceURI)
		if err != nil {
			return nil, err
		}
		identity.Identifiers = append(identity.Identifiers, IdentifierRule{
			Name: institutional.Scheme, Scheme: institutional.Scheme, IdentityLevel: level,
			Value: selector, Pattern: institutional.Pattern, Canonicalizer: "trim",
			Strength: IdentifierStrong, Scope: ScopeInstitution, Namespace: namespace,
			Lookup: LookupRule{Fields: []FieldSelector{selector}, Operator: LookupContains, Variants: []string{"canonical"}},
		})
	}

	title, hasTitle := drupalMetadataField(entity, titleFieldScore)
	if hasTitle {
		titleSelector := FieldSelector{EntityType: entity.EntityType, Bundle: entity.Bundle, Path: title.Path}
		metadata := &MetadataIdentity{Title: &titleSelector, MetadataOnly: MetadataOnlyReview}
		contributor, hasContributor := drupalMetadataField(entity, contributorFieldScore)
		if hasContributor {
			selector := metadataSelector(entity, contributor, "name")
			metadata.Contributors = []FieldSelector{selector}
		}
		date, hasDate := drupalMetadataField(entity, dateFieldScore)
		if hasDate {
			selector := metadataSelector(entity, date, "value")
			metadata.Date = &selector
		}
		titleLookup := LookupRule{Fields: []FieldSelector{titleSelector}, Operator: LookupContains, Variants: []string{"canonical", "title-phrase"}}
		if hasContributor {
			contributorLookup := LookupRule{Fields: append([]FieldSelector(nil), metadata.Contributors...), Operator: LookupContains, Variants: []string{"author-family"}}
			metadata.Lookups = append(metadata.Lookups, MetadataLookup{Name: "title-author", Title: titleLookup, Contributors: &contributorLookup})
		}
		if hasDate {
			dateLookup := LookupRule{Fields: []FieldSelector{*metadata.Date}, Operator: LookupExact, Variants: []string{"year"}}
			metadata.Lookups = append(metadata.Lookups, MetadataLookup{Name: "title-date", Title: titleLookup, Date: &dateLookup})
		}
		metadata.Lookups = append(metadata.Lookups, MetadataLookup{Name: "title-only", Title: titleLookup})
		identity.Metadata = metadata
	}
	if len(identity.Identifiers) == 0 && identity.Metadata == nil {
		return nil, nil
	}
	return identity, nil
}

func canonicalInstitutionNamespace(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || (parsed.Host == "" && parsed.Opaque == "") {
		return "", fmt.Errorf("institutional identifier namespace URI %q must be absolute", value)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("institutional identifier namespace URI %q must not contain credentials, query, or fragment", value)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String(), nil
}

type drupalIdentifierDescriptor struct {
	name          string
	scheme        string
	discriminator string
	fieldTokens   []string
	level         IdentifierIdentityLevel
}

func drupalIdentifierDescriptors() []drupalIdentifierDescriptor {
	return []drupalIdentifierDescriptor{
		{name: "doi", scheme: "doi", discriminator: "doi", fieldTokens: []string{"doi"}, level: IdentityWork},
		// A version DOI is globally unique exact evidence, but remains distinct
		// from a concept DOI. Drupal's common identifier field does not encode
		// that granularity, so both exact levels deliberately share one selector.
		{name: "doi-version", scheme: "doi", discriminator: "doi", fieldTokens: []string{"doi"}, level: IdentityVersion},
		{scheme: "arxiv", discriminator: "arxiv", fieldTokens: []string{"arxiv"}, level: IdentityWork},
		{scheme: "wos", discriminator: "wos", fieldTokens: []string{"wos", "web_of_science"}, level: IdentityWork},
		{scheme: "scopus-eid", discriminator: "scopus-eid", fieldTokens: []string{"scopus_eid", "eid"}, level: IdentitySourceRecord},
		{scheme: "scopus-id", discriminator: "scopus-id", fieldTokens: []string{"scopus_id"}, level: IdentitySourceRecord},
		{scheme: "zenodo-record", discriminator: "zenodo-record", fieldTokens: []string{"zenodo_record"}, level: IdentityVersion},
		{scheme: "zenodo-concept", discriminator: "zenodo-concept", fieldTokens: []string{"zenodo_concept"}, level: IdentityConcept},
		{scheme: "handle", discriminator: "handle", fieldTokens: []string{"handle"}, level: IdentityWork},
		{scheme: "pmid", discriminator: "pmid", fieldTokens: []string{"pmid"}, level: IdentityWork},
		{scheme: "pmcid", discriminator: "pmcid", fieldTokens: []string{"pmcid"}, level: IdentityWork},
		{scheme: "isbn", discriminator: "isbn", fieldTokens: []string{"isbn"}, level: IdentityManifestation},
		{scheme: "issn", discriminator: "issn", fieldTokens: []string{"issn"}, level: IdentityManifestation},
	}
}

func (descriptor drupalIdentifierDescriptor) ruleName() string {
	if descriptor.name != "" {
		return descriptor.name
	}
	return descriptor.scheme
}

func drupalIdentifierField(fields []model.Field) (model.Field, bool) {
	for _, field := range fields {
		if field.Path == "field_identifier" && (field.Kind == model.ValueComposite || field.Kind == model.ValueTypedReference || field.Kind == model.ValueText) {
			return field, field.Kind != model.ValueText
		}
	}
	return model.Field{}, false
}

func drupalIdentifierSelector(entity model.Entity, identifierField model.Field, hasIdentifierField bool, descriptor drupalIdentifierDescriptor) (FieldSelector, bool) {
	for _, field := range entity.Fields {
		path := strings.ToLower(field.Path)
		if path == "field_identifier" {
			continue
		}
		for _, token := range descriptor.fieldTokens {
			if path == token || path == "field_"+token || strings.HasSuffix(path, "_"+token) {
				selector := FieldSelector{EntityType: entity.EntityType, Bundle: entity.Bundle, Path: field.Path}
				if field.Kind == model.ValueComposite || field.Kind == model.ValueTypedReference {
					selector.Attribute = "value"
				}
				return selector, true
			}
		}
	}
	if hasIdentifierField {
		return structuredIdentifierSelector(entity, identifierField, descriptor.discriminator), true
	}
	return FieldSelector{}, false
}

func structuredIdentifierSelector(entity model.Entity, field model.Field, discriminator string) FieldSelector {
	return FieldSelector{
		EntityType: entity.EntityType, Bundle: entity.Bundle, Path: field.Path, Attribute: "value",
		Where: &FieldPredicate{Attribute: "attr0", Equals: discriminator},
	}
}

func identifierLevelIsExact(levels []hubv1.IdentifierIdentityLevel, level IdentifierIdentityLevel) bool {
	wanted := hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED
	switch level {
	case IdentityWork:
		wanted = hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK
	case IdentityVersion:
		wanted = hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION
	case IdentityManifestation:
		wanted = hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_MANIFESTATION
	case IdentityConcept:
		wanted = hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT
	case IdentitySourceRecord:
		wanted = hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD
	}
	for _, candidate := range levels {
		if candidate == wanted {
			return true
		}
	}
	return false
}

func drupalHubPath(field model.Field) string {
	path := strings.ToLower(field.Path)
	properties := strings.ToLower(strings.Join(field.SemanticProperties, " "))
	switch {
	case strings.Contains(path, "full_title"):
		return "FullTitle"
	case strings.Contains(path, "alt_title") || strings.Contains(path, "alternative_title"):
		return "AltTitle"
	case path == "title" || strings.Contains(properties, "dcterms:title"):
		return "Title"
	case path == "field_identifier" || strings.Contains(path, "identifier") || strings.Contains(properties, "dcterms:identifier") || dedicatedIdentifierPath(path):
		return "Identifiers"
	case strings.Contains(path, "linked_agent") || strings.Contains(path, "contributor") || strings.Contains(path, "creator") || strings.Contains(path, "author") || strings.Contains(properties, "dcterms:creator"):
		return "Contributors"
	case strings.Contains(path, "date_issued") || strings.Contains(path, "publication_date") || strings.Contains(properties, "dcterms:issued"):
		return "Dates"
	case strings.Contains(path, "date_created") || path == "created" || strings.Contains(properties, "dcterms:created"):
		return "Dates"
	case strings.Contains(path, "resource_type"):
		return "ResourceType"
	case strings.Contains(path, "genre"):
		return "Genre"
	case strings.Contains(path, "language"):
		return "Language"
	case strings.Contains(path, "place_published") || strings.Contains(path, "publication_place"):
		return "PlacePublished"
	case strings.Contains(path, "physical_description") || path == "field_extent" || path == "extent":
		return "PhysicalDesc"
	case strings.Contains(path, "edition"):
		return "Edition"
	case strings.Contains(path, "rights"):
		return "Rights"
	case strings.Contains(path, "abstract"):
		return "Abstract"
	case strings.Contains(path, "description"):
		return "Description"
	case strings.Contains(path, "subject") || strings.Contains(path, "keyword"):
		return "Subjects"
	case strings.Contains(path, "publisher"):
		return "Publisher"
	case strings.Contains(path, "member_of") || strings.Contains(path, "related_item"):
		return "Relations"
	default:
		return "Extra." + field.Path
	}
}

func drupalCodec(field model.Field, hubPath string) string {
	if hubPath == "Identifiers" && (field.Kind == model.ValueComposite || field.Kind == model.ValueTypedReference) {
		return "typed-identifier"
	}
	switch field.Kind {
	case model.ValueText:
		return "text"
	case model.ValueInteger:
		return "integer"
	case model.ValueDecimal:
		return "decimal"
	case model.ValueBoolean:
		return "boolean"
	case model.ValueDate:
		return "date"
	case model.ValueLink:
		return "link"
	case model.ValueReference:
		return "reference"
	case model.ValueTypedReference:
		return "typed-relation"
	case model.ValueFile:
		return "file"
	case model.ValueComposite:
		return "composite"
	default:
		return "opaque"
	}
}

func repeatedHubPath(path string) bool {
	switch path {
	case "AltTitle", "Contributors", "Dates", "Genre", "Subjects", "Rights", "Identifiers", "Relations",
		"Publisher", "PlacePublished", "PhysicalDesc", "Edition", "Language":
		return true
	default:
		return false
	}
}

func drupalMetadataField(entity model.Entity, score func(model.Field) int) (model.Field, bool) {
	bestScore := 0
	best := model.Field{}
	for _, field := range entity.Fields {
		candidateScore := score(field)
		if candidateScore > bestScore || (candidateScore == bestScore && candidateScore > 0 && field.Path < best.Path) {
			bestScore = candidateScore
			best = field
		}
	}
	return best, bestScore > 0
}

func titleFieldScore(field model.Field) int {
	path := strings.ToLower(field.Path)
	switch {
	case path == "title":
		return 100
	case path == "field_full_title":
		return 90
	case path == "field_title":
		return 80
	case path == "name" || path == "label":
		return 70
	case containsSemanticProperty(field, "dcterms:title"):
		return 60
	default:
		return 0
	}
}

func contributorFieldScore(field model.Field) int {
	path := strings.ToLower(field.Path)
	switch {
	case strings.Contains(path, "linked_agent"):
		return 100
	case strings.Contains(path, "contributor"):
		return 90
	case strings.Contains(path, "creator"):
		return 80
	case strings.Contains(path, "author"):
		return 70
	case containsSemanticProperty(field, "dcterms:creator"):
		return 60
	default:
		return 0
	}
}

func dateFieldScore(field model.Field) int {
	path := strings.ToLower(field.Path)
	switch {
	case strings.Contains(path, "date_issued"):
		return 100
	case strings.Contains(path, "publication_date"):
		return 90
	case containsSemanticProperty(field, "dcterms:issued"):
		return 80
	case strings.Contains(path, "date_created"):
		return 70
	case path == "created":
		return 60
	default:
		return 0
	}
}

func metadataSelector(entity model.Entity, field model.Field, structuredAttribute string) FieldSelector {
	selector := FieldSelector{EntityType: entity.EntityType, Bundle: entity.Bundle, Path: field.Path}
	if field.Kind == model.ValueComposite || field.Kind == model.ValueTypedReference || field.Kind == model.ValueReference || (structuredAttribute == "value" && field.Kind == model.ValueDate) {
		selector.Attribute = structuredAttribute
	}
	return selector
}

func dedicatedIdentifierPath(fieldPath string) bool {
	for _, descriptor := range drupalIdentifierDescriptors() {
		for _, token := range descriptor.fieldTokens {
			if fieldPath == token || fieldPath == "field_"+token || strings.HasSuffix(fieldPath, "_"+token) {
				return true
			}
		}
	}
	return false
}

func containsSemanticProperty(field model.Field, property string) bool {
	for _, candidate := range field.SemanticProperties {
		if strings.EqualFold(candidate, property) {
			return true
		}
	}
	return false
}
