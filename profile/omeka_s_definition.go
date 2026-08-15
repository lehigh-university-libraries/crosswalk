package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/model"
)

const (
	omekaSSystem         = "omeka-s"
	omekaSResourceEntity = "resource"
)

// OmekaSInstitutionalIdentifierOptions defines an institution-owned
// identifier exposed by an Omeka core field or vocabulary property. Crosswalk
// requires the authority and pattern explicitly rather than guessing that an
// arbitrary dcterms:identifier value is unique.
type OmekaSInstitutionalIdentifierOptions struct {
	FieldPath     string
	Scheme        string
	NamespaceURI  string
	Pattern       string
	IdentityLevel IdentifierIdentityLevel
}

// OmekaSDefinitionOptions selects the installation-wide Omeka property model
// or one resource template and an optional explicit institutional identifier.
type OmekaSDefinitionOptions struct {
	Name                    string
	ResourceTemplateID      int64
	InstitutionalIdentifier *OmekaSInstitutionalIdentifierOptions
}

// NewOmekaSDefinition builds a conservative, editable starter definition from
// a compiled Omeka S model. It performs no acquisition and binds the result to
// the exact immutable model fingerprint.
func NewOmekaSDefinition(snapshot *model.Snapshot, options OmekaSDefinitionOptions) (*Definition, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("building Omeka S profile: model snapshot is nil")
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("building Omeka S profile: %w", err)
	}
	if snapshot.System != omekaSSystem {
		return nil, fmt.Errorf("building Omeka S profile: model system %q is not omeka-s", snapshot.System)
	}
	if options.ResourceTemplateID < 0 {
		return nil, fmt.Errorf("building Omeka S profile: resource template ID must not be negative")
	}
	bundle := ""
	if options.ResourceTemplateID > 0 {
		bundle = strconv.FormatInt(options.ResourceTemplateID, 10)
	}
	entity, exists := snapshot.Entity(omekaSResourceEntity, bundle)
	if !exists {
		return nil, fmt.Errorf("building Omeka S profile: resource template %q is not in the model", bundle)
	}

	fields := append([]model.Field(nil), entity.Fields...)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Path < fields[j].Path })
	identity, err := newOmekaSIdentity(entity, options.InstitutionalIdentifier)
	if err != nil {
		return nil, fmt.Errorf("building Omeka S profile: %w", err)
	}
	mappings := make([]Mapping, 0, len(fields))
	for _, field := range fields {
		if field.Path == "o:id" || field.Path == "o:resource_template" {
			continue
		}
		hubPath := omekaSHubPath(field)
		if omekaSIdentityUsesPath(identity, field.Path) {
			hubPath = "Identifiers"
		}
		if hubPath == "" {
			continue
		}
		codec := omekaSCodec(field, hubPath)
		merge := MergeFirstNonempty
		if omekaSRepeatedHubPath(hubPath) || (strings.HasPrefix(hubPath, "Extra.") && field.Cardinality != 1) {
			merge = MergeAppend
		}
		mappings = append(mappings, Mapping{
			Field: FieldSelector{EntityType: entity.EntityType, Bundle: entity.Bundle, Path: field.Path},
			Hub:   hubPath, Decode: codec, Encode: "none", Merge: merge,
		})
	}
	if len(mappings) == 0 {
		return nil, fmt.Errorf("building Omeka S profile: selected model entity has no mappable fields")
	}

	description := "Starter profile for the installation-wide Omeka S resource model"
	if bundle != "" {
		description = fmt.Sprintf("Starter profile for Omeka S resource template %s", bundle)
	}
	if entity.Label != "" {
		description += " (" + entity.Label + ")"
	}
	definition := &Definition{
		Version: CurrentDefinitionVersion, Name: options.Name, Description: description,
		System: snapshot.System, ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: mappings, Identity: identity,
	}
	if err := definition.SealFingerprint(); err != nil {
		return nil, fmt.Errorf("sealing Omeka S profile: %w", err)
	}
	if _, err := Compile(snapshot, definition); err != nil {
		return nil, fmt.Errorf("compiling Omeka S profile: %w", err)
	}
	return definition, nil
}

func newOmekaSIdentity(entity model.Entity, institutional *OmekaSInstitutionalIdentifierOptions) (*IdentityPolicy, error) {
	repository := EntitySelector{EntityType: entity.EntityType, Bundle: entity.Bundle}
	identity := &IdentityPolicy{Version: CurrentIdentityVersion, Repository: repository}
	identifierField, hasIdentifierField := omekaSField(entity, "dcterms:identifier")
	if hasIdentifierField {
		selector := omekaSSelector(entity, identifierField.Path)
		for _, descriptor := range omekaSIdentifierDescriptors() {
			registryRule, exists := hub.DefaultIdentifierRegistry().Rule(descriptor.scheme)
			if !exists {
				return nil, fmt.Errorf("built-in identifier scheme %q is unavailable", descriptor.scheme)
			}
			strength := IdentifierCorroborating
			if identifierLevelIsExact(registryRule.ExactIdentityLevels, descriptor.level) {
				strength = IdentifierStrong
			}
			identity.Identifiers = append(identity.Identifiers, IdentifierRule{
				Name: descriptor.scheme, Scheme: descriptor.scheme, IdentityLevel: descriptor.level,
				Value: selector, Pattern: registryRule.Pattern, Canonicalizer: "canonical",
				Strength: strength, Scope: ScopeGlobal, Namespace: registryRule.NamespaceURI,
				Lookup: LookupRule{Fields: []FieldSelector{selector}, Operator: LookupContains, Variants: descriptor.variants},
			})
		}
	}
	if institutional != nil {
		fieldPath := strings.TrimSpace(institutional.FieldPath)
		if fieldPath == "" {
			return nil, fmt.Errorf("institutional identifier field path is required")
		}
		if _, exists := omekaSField(entity, fieldPath); !exists {
			return nil, fmt.Errorf("institutional identifier field %q is not in the selected model entity", fieldPath)
		}
		if strings.TrimSpace(institutional.Scheme) == "" || strings.TrimSpace(institutional.NamespaceURI) == "" || strings.TrimSpace(institutional.Pattern) == "" {
			return nil, fmt.Errorf("institutional identifier scheme, namespace URI, and pattern are required")
		}
		if _, builtIn := hub.DefaultIdentifierRegistry().Rule(institutional.Scheme); builtIn {
			return nil, fmt.Errorf("institutional identifier scheme %q is built in; use a distinct institution scheme", institutional.Scheme)
		}
		namespace, err := canonicalInstitutionNamespace(institutional.NamespaceURI)
		if err != nil {
			return nil, err
		}
		level := institutional.IdentityLevel
		if level == "" {
			level = IdentitySourceRecord
		}
		selector := omekaSSelector(entity, fieldPath)
		operator := LookupContains
		if strings.HasPrefix(fieldPath, "o:") {
			operator = LookupExact
		}
		identity.Identifiers = append(identity.Identifiers, IdentifierRule{
			Name: institutional.Scheme, Scheme: institutional.Scheme, IdentityLevel: level,
			Value: selector, Pattern: institutional.Pattern, Canonicalizer: "trim",
			Strength: IdentifierStrong, Scope: ScopeInstitution, Namespace: namespace,
			Lookup: LookupRule{Fields: []FieldSelector{selector}, Operator: operator, Variants: []string{"canonical"}},
		})
	}

	title, hasTitle := omekaSFirstField(entity, "dcterms:title", "o:title")
	if hasTitle {
		titleSelector := omekaSSelector(entity, title.Path)
		metadata := &MetadataIdentity{Title: &titleSelector, MetadataOnly: MetadataOnlyReview}
		for _, path := range []string{"dcterms:creator", "dcterms:contributor"} {
			if field, exists := omekaSField(entity, path); exists {
				metadata.Contributors = append(metadata.Contributors, omekaSSelector(entity, field.Path))
			}
		}
		if field, exists := omekaSFirstField(entity, "dcterms:issued", "dcterms:date", "dcterms:created", "o:created"); exists {
			selector := omekaSSelector(entity, field.Path)
			metadata.Date = &selector
		}
		titleLookup := LookupRule{Fields: []FieldSelector{titleSelector}, Operator: LookupContains, Variants: []string{"canonical", "title-phrase"}}
		if len(metadata.Contributors) != 0 {
			contributors := LookupRule{Fields: append([]FieldSelector(nil), metadata.Contributors...), Operator: LookupContains, Variants: []string{"author-family"}}
			metadata.Lookups = append(metadata.Lookups, MetadataLookup{Name: "title-author", Title: titleLookup, Contributors: &contributors})
		}
		if metadata.Date != nil {
			date := LookupRule{Fields: []FieldSelector{*metadata.Date}, Operator: LookupExact, Variants: []string{"year"}}
			metadata.Lookups = append(metadata.Lookups, MetadataLookup{Name: "title-date", Title: titleLookup, Date: &date})
		}
		metadata.Lookups = append(metadata.Lookups, MetadataLookup{Name: "title-only", Title: titleLookup})
		identity.Metadata = metadata
	}
	if len(identity.Identifiers) == 0 && identity.Metadata == nil {
		return nil, nil
	}
	return identity, nil
}

type omekaSIdentifierDescriptor struct {
	scheme   string
	level    IdentifierIdentityLevel
	variants []string
}

func omekaSIdentifierDescriptors() []omekaSIdentifierDescriptor {
	return []omekaSIdentifierDescriptor{
		{scheme: "doi", level: IdentityWork, variants: []string{"canonical", "doi-url"}},
		{scheme: "arxiv", level: IdentityWork, variants: []string{"canonical"}},
		{scheme: "handle", level: IdentityWork, variants: []string{"canonical"}},
		{scheme: "pmid", level: IdentityWork, variants: []string{"canonical"}},
		{scheme: "pmcid", level: IdentityWork, variants: []string{"canonical"}},
		{scheme: "wos", level: IdentityWork, variants: []string{"canonical", "wos-bare"}},
		{scheme: "scopus-eid", level: IdentitySourceRecord, variants: []string{"canonical"}},
		{scheme: "scopus-id", level: IdentitySourceRecord, variants: []string{"canonical"}},
		{scheme: "zenodo-record", level: IdentityVersion, variants: []string{"canonical"}},
		{scheme: "zenodo-concept", level: IdentityConcept, variants: []string{"canonical"}},
		{scheme: "isbn", level: IdentityManifestation, variants: []string{"canonical"}},
		{scheme: "issn", level: IdentityManifestation, variants: []string{"canonical"}},
	}
}

func omekaSHubPath(field model.Field) string {
	switch strings.ToLower(field.Path) {
	case "o:title", "dcterms:title":
		return "Title"
	case "dcterms:alternative":
		return "AltTitle"
	case "dcterms:abstract":
		return "Abstract"
	case "dcterms:description":
		return "Description"
	case "dcterms:creator":
		return "Contributors.Creator"
	case "dcterms:contributor":
		return "Contributors.Contributor"
	case "o:created", "dcterms:created":
		return "Dates.Created"
	case "o:modified", "dcterms:modified":
		return "Dates.Modified"
	case "dcterms:issued":
		return "Dates.Issued"
	case "dcterms:available":
		return "Dates.Available"
	case "dcterms:date":
		return "Dates.Other"
	case "o:resource_class", "dcterms:type":
		return "ResourceType"
	case "dcterms:format":
		return "Genre"
	case "dcterms:subject", "dcterms:coverage", "dcterms:spatial":
		return "Subjects"
	case "dcterms:language":
		return "Language"
	case "dcterms:publisher":
		return "Publisher"
	case "dcterms:rights":
		return "Rights.Statement"
	case "dcterms:license":
		return "Rights.License"
	case "dcterms:accessrights":
		return "AccessCondition"
	case "dcterms:rightsholder":
		return "Rights.Holder"
	case "dcterms:identifier":
		return "Identifiers"
	case "dcterms:relation":
		return "Relations.RelatedTo"
	case "dcterms:haspart":
		return "Relations.HasPart"
	case "dcterms:ispartof":
		return "Relations.PartOf"
	case "dcterms:hasformat":
		return "Relations.HasFormat"
	case "dcterms:isformatof":
		return "Relations.FormatOf"
	case "dcterms:references":
		return "Relations.References"
	case "dcterms:isreferencedby":
		return "Relations.IsCitedBy"
	case "dcterms:replaces":
		return "Relations.Replaces"
	case "dcterms:isreplacedby":
		return "Relations.IsReplacedBy"
	case "dcterms:source":
		return "Source"
	case "dcterms:bibliographiccitation":
		return "PreferredCitation"
	case "dcterms:extent":
		return "PhysicalDesc"
	case "dcterms:tableofcontents":
		return "TableOfContents"
	case "o:is_public":
		return "IsPublic"
	case "o:id", "o:resource_template":
		return ""
	default:
		return "Extra." + omekaSExtraKey(field.Path)
	}
}

func omekaSCodec(field model.Field, hubPath string) string {
	base, _, _ := strings.Cut(hubPath, ".")
	if base == "Identifiers" {
		return "typed-identifier"
	}
	switch base {
	case "Dates":
		return "date"
	case "IsPublic":
		return "boolean"
	case "ResourceType":
		return "reference"
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

func omekaSRepeatedHubPath(path string) bool {
	base, _, _ := strings.Cut(path, ".")
	switch base {
	case "AltTitle", "Contributors", "Dates", "Genre", "Subjects", "Rights", "Identifiers", "Relations", "Notes",
		"Publisher", "PlacePublished", "PhysicalDesc", "Edition", "Language":
		return true
	default:
		return false
	}
}

func omekaSIdentityUsesPath(identity *IdentityPolicy, path string) bool {
	if identity == nil {
		return false
	}
	for _, rule := range identity.Identifiers {
		if rule.Value.Path == path && path != "o:id" {
			return true
		}
	}
	return false
}

func omekaSExtraKey(path string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, character := range strings.ToLower(path) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' {
			builder.WriteRune(character)
			lastUnderscore = character == '_'
		} else if builder.Len() > 0 && !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	value := strings.Trim(builder.String(), "_")
	if value == "" {
		value = "field"
	}
	if value[0] < 'a' || value[0] > 'z' {
		value = "field_" + value
	}
	digest := sha256.Sum256([]byte(path))
	// Different compact IRIs can have the same snake-case rendering. Retaining
	// a short digest prevents silently merging unrelated custom properties.
	return value + "_" + hex.EncodeToString(digest[:6])
}

func omekaSSelector(entity model.Entity, path string) FieldSelector {
	return FieldSelector{EntityType: entity.EntityType, Bundle: entity.Bundle, Path: path}
}

func omekaSField(entity model.Entity, path string) (model.Field, bool) {
	for _, field := range entity.Fields {
		if strings.EqualFold(field.Path, path) {
			return field, true
		}
	}
	return model.Field{}, false
}

func omekaSFirstField(entity model.Entity, paths ...string) (model.Field, bool) {
	for _, path := range paths {
		if field, exists := omekaSField(entity, path); exists {
			return field, true
		}
	}
	return model.Field{}, false
}
