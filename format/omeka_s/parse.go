package omeka_s

import (
	"fmt"
	"io"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

// Parse converts an Omeka S API resource, API response array, or deterministic
// snapshot into flat Hub records. Call ParseDataset to retain dataset-level
// acquisition provenance.
func (parser *Format) Parse(reader io.Reader, options *format.ParseOptions) ([]*hubv1.Record, error) {
	dataset, err := parser.ParseDataset(reader, options)
	if err != nil {
		return nil, err
	}
	records := make([]*hubv1.Record, len(dataset.Records))
	for index, entry := range dataset.Records {
		records[index] = entry.Record
	}
	return records, nil
}

// ParseDataset converts an Omeka S API resource, API response array, or
// deterministic installation snapshot into a validated, flat Hub dataset.
// Omeka item-set membership and item/media links are represented as relations,
// not as a single-parent hierarchy, because Omeka items can belong to multiple
// item sets.
func (*Format) ParseDataset(reader io.Reader, options *format.ParseOptions) (*format.Dataset, error) {
	raw, err := readOneJSON(reader)
	if err != nil {
		return nil, fmt.Errorf("parsing Omeka S JSON-LD: %w", err)
	}
	input, err := decodeOmekaInput(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing Omeka S JSON-LD: %w", err)
	}
	if len(input.resources) == 0 {
		return nil, fmt.Errorf("parsing Omeka S JSON-LD: snapshot has no item, item set, or media resources")
	}
	if err := applyInputProvenance(input, options); err != nil {
		return nil, fmt.Errorf("applying Omeka S provenance: %w", err)
	}

	resourcesByKey := make(map[string]*resource, len(input.resources))
	for _, source := range input.resources {
		resourcesByKey[resourceLookupKey(source.kind, source.id)] = source
	}
	dataset := &format.Dataset{
		Records:    make([]format.DatasetRecord, 0, len(input.resources)),
		Hierarchy:  format.Hierarchy{Nodes: make([]format.HierarchyNode, 0, len(input.resources))},
		Provenance: input.provenance,
	}
	for index, source := range input.resources {
		record, err := mapResource(source, input.model, input.provenance, options, resourcesByKey)
		if err != nil {
			return nil, fmt.Errorf("mapping Omeka S %s %d: %w", source.kind, source.id, err)
		}
		// Dataset keys are a stable, user-facing serialization contract. Internal
		// lookups use the kind:id form shared with SourceInfo.SourceId.
		key := string(source.kind) + "-" + strconv.FormatInt(source.id, 10)
		dataset.Records = append(dataset.Records, format.DatasetRecord{Key: key, Record: record})
		dataset.Hierarchy.Nodes = append(dataset.Hierarchy.Nodes, format.HierarchyNode{RecordKey: key, Position: index})
	}
	if err := dataset.Validate(); err != nil {
		return nil, fmt.Errorf("validating Omeka S dataset: %w", err)
	}
	return dataset, nil
}

func applyInputProvenance(input *decodedInput, options *format.ParseOptions) error {
	if input.provenance.Format == "" {
		input.provenance = formatProvenance("")
	}
	if options == nil {
		return nil
	}
	input.provenance.Source = strings.TrimSpace(options.SourceName)
	if input.provenance.SourceURI == "" && strings.TrimSpace(options.BaseURL) != "" {
		sourceURI, err := safeAPIURI(options.BaseURL, true)
		if err != nil {
			return fmt.Errorf("base URL: %w", err)
		}
		input.provenance.SourceURI = sourceURI
	}
	if options.Profile != nil {
		input.provenance.Profile = options.Profile.Name
	}
	if options.SystemProfile != nil {
		if options.SystemProfile.System() != "omeka-s" {
			return fmt.Errorf("system profile %q targets %q, not omeka-s", options.SystemProfile.Name(), options.SystemProfile.System())
		}
		// A published profile is already bound to an immutable model snapshot, so
		// ordinary /api/items responses do not need to repeat that schema. When an
		// acquisition snapshot does embed a model, keep the stronger provenance
		// check and refuse to apply the profile to a different contract.
		if input.model != nil {
			if input.model.contractFingerprint == "" {
				return fmt.Errorf("snapshot contains an Omeka S schema model without a contract fingerprint")
			}
			if input.model.contractFingerprint != options.SystemProfile.ModelFingerprint() {
				return fmt.Errorf("snapshot model fingerprint %s does not match profile model %s", input.model.contractFingerprint, options.SystemProfile.ModelFingerprint())
			}
		}
		input.provenance.Profile = options.SystemProfile.Name()
		input.provenance.ProfileFingerprint = options.SystemProfile.Fingerprint()
		input.provenance.ModelFingerprint = options.SystemProfile.ModelFingerprint()
	}
	return nil
}

func mapResource(source *resource, model *schemaModel, provenance format.DatasetProvenance, options *format.ParseOptions, resources map[string]*resource) (*hubv1.Record, error) {
	record := &hubv1.Record{
		SourceInfo: &hubv1.SourceInfo{
			Format:        "omeka-s",
			FormatVersion: Version,
			SourceId:      string(source.kind) + ":" + strconv.FormatInt(source.id, 10),
			Origin:        sourceOrigin(source.uri, provenance.SourceURI),
			SourceUri:     source.uri,
		},
	}
	profiled := false
	if options != nil && options.SystemProfile != nil {
		var err error
		profiled, err = omekaProfileApplies(source, options.SystemProfile)
		if err != nil {
			return nil, err
		}
	}
	if !profiled {
		record.Title = source.title
		record.IsPublic = source.isPublic
	}
	if options != nil {
		if profiled {
			record.SourceInfo.Profile = options.SystemProfile.Name()
			record.SourceInfo.ProfileFingerprint = options.SystemProfile.Fingerprint()
			record.SourceInfo.ModelFingerprint = options.SystemProfile.ModelFingerprint()
		} else if options.Profile != nil {
			record.SourceInfo.Profile = options.Profile.Name
		}
	}
	if profiled {
		mapped, err := applyCompiledProfile(record, source, model, options)
		if err != nil {
			return nil, err
		}
		record.SourceInfo.UnmappedFields = unmappedOmekaFields(source, mapped)
	} else {
		mapResourceType(record, source, model)
		mapProperties(record, source)
		mapSourceDates(record, source)
	}
	if err := addSourceIdentifier(record, source, options, profiled); err != nil {
		return nil, err
	}
	mapStructuralRelations(record, source)
	if source.kind == kindMedia {
		if language := strings.TrimSpace(source.lang); language != "" {
			hub.SetLanguages(record, append(hub.GetLanguages(record), language))
		}
		record.Source = firstNonempty(record.Source, source.mediaSource)
		if file := mediaFile(source); file != nil {
			record.Files = append(record.Files, file)
		}
	}
	if source.kind == kindItem {
		for _, mediaRef := range source.media {
			mediaSource := resources[resourceLookupKey(kindMedia, mediaRef.id)]
			if mediaSource == nil {
				continue
			}
			if file := mediaFile(mediaSource); file != nil {
				record.Files = append(record.Files, file)
			}
		}
	}
	if err := attachOmekaExtras(record, source, model, profiled, !profiled); err != nil {
		return nil, err
	}
	return record, nil
}

func resourceLookupKey(kind resourceKind, id int64) string {
	return string(kind) + ":" + strconv.FormatInt(id, 10)
}

func addSourceIdentifier(record *hubv1.Record, source *resource, options *format.ParseOptions, profiled bool) error {
	namespace := resourceNamespace(source.uri)
	registry := hub.DefaultIdentifierRegistry()
	scheme := "omeka-s-resource"
	if profiled && options != nil && options.SystemProfile != nil {
		compiled := options.SystemProfile
		registry = compiled.IdentifierRegistry()
		configured := make([]profile.CompiledIdentifierRule, 0)
		for _, rule := range compiled.LookupPlan().Identifiers {
			if rule.Value.Selector.Path == "o:id" {
				configured = append(configured, rule)
			}
		}
		if len(configured) != 0 {
			var lastErr error
			for _, rule := range configured {
				identifier, err := compiled.NewIdentifier(rule.Name, strconv.FormatInt(source.id, 10))
				if err != nil {
					lastErr = err
					continue
				}
				identifier.Display = string(source.kind) + ":" + strconv.FormatInt(source.id, 10)
				identifier.IsPreferred = true
				if !identifierExists(record.Identifiers, identifier) {
					record.Identifiers = append(record.Identifiers, identifier)
				}
				return nil
			}
			return fmt.Errorf("source identifier does not satisfy its profile rule: %w", lastErr)
		}
	}
	identifier := &hubv1.Identifier{Value: strconv.FormatInt(source.id, 10), Scheme: scheme, NamespaceUri: namespace, IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD}
	canonical, err := registry.CanonicalizeIdentifier(identifier)
	if err != nil {
		return fmt.Errorf("canonicalizing source identifier: %w", err)
	}
	canonical.Display = string(source.kind) + ":" + strconv.FormatInt(source.id, 10)
	canonical.IsPreferred = true
	record.Identifiers = append(record.Identifiers, canonical)
	return nil
}

func unmappedOmekaFields(source *resource, mapped map[string]bool) []string {
	unmapped := make([]string, 0)
	for _, field := range source.properties {
		if !mapped[field.term] {
			unmapped = append(unmapped, field.term)
		}
	}
	sort.Strings(unmapped)
	return unmapped
}

func resourceNamespace(rawURI string) string {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return rawURI
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimSuffix(path.Dir(strings.TrimSuffix(parsed.Path, "/")), "/") + "/"
	return parsed.String()
}

func installationNamespace(rawURI string) string {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return rawURI
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	apiIndex := -1
	for index, segment := range segments {
		if segment == "api" {
			apiIndex = index
			break
		}
	}
	if apiIndex >= 0 {
		parsed.Path = "/" + strings.Join(segments[:apiIndex+1], "/") + "/"
	} else {
		parsed.Path = "/"
	}
	return parsed.String()
}

func sourceOrigin(resourceURI, provenanceURI string) string {
	if provenanceURI != "" {
		return provenanceURI
	}
	return installationNamespace(resourceURI)
}

func mapResourceType(record *hubv1.Record, source *resource, model *schemaModel) {
	original := ""
	if source.resourceClass != nil && model != nil {
		if class, exists := model.classesByID[source.resourceClass.id]; exists {
			original = class.term
		}
	}
	if original == "" {
		for _, typeName := range source.types {
			if !strings.HasPrefix(typeName, "o:") {
				original = typeName
				break
			}
		}
	}
	if original == "" {
		if value, ok := firstDisplayValue(source, "dcterms:type"); ok {
			original = value
		}
	}
	normalized := omekaResourceType(original)
	switch source.kind {
	case kindItemSet:
		normalized = hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION
		if original == "" {
			original = "o:ItemSet"
		}
	case kindMedia:
		normalized = mediaResourceType(source.mediaType)
		if original == "" {
			original = source.mediaType
		}
	}
	record.ResourceType = &hubv1.ResourceType{Type: normalized, Original: original, Vocabulary: "Omeka S resource class"}
}

func mapProperties(record *hubv1.Record, source *resource) {
	if title, ok := firstDisplayValue(source, "dcterms:title"); ok {
		record.Title = title
	}
	for _, term := range source.properties {
		switch term.term {
		case "dcterms:title":
			titleIndex := firstDisplayIndex(term.values)
			for index, value := range term.values {
				if index == titleIndex {
					continue
				}
				if display := displayValue(value); display != "" {
					record.AltTitle = appendUnique(record.AltTitle, display)
				}
			}
		case "dcterms:alternative":
			for _, value := range term.values {
				if display := displayValue(value); display != "" {
					record.AltTitle = appendUnique(record.AltTitle, display)
				}
			}
		case "dcterms:abstract":
			record.Abstract = firstNonempty(record.Abstract, firstDisplay(term.values))
		case "dcterms:description":
			record.Description = firstNonempty(record.Description, firstDisplay(term.values))
		case "dcterms:creator":
			appendContributors(record, term.values, "creator")
		case "dcterms:contributor":
			appendContributors(record, term.values, "contributor")
		case "dcterms:date":
			appendDates(record, term.values, hubv1.DateType_DATE_TYPE_OTHER)
		case "dcterms:created":
			appendDates(record, term.values, hubv1.DateType_DATE_TYPE_CREATED)
		case "dcterms:modified":
			appendDates(record, term.values, hubv1.DateType_DATE_TYPE_MODIFIED)
		case "dcterms:issued":
			appendDates(record, term.values, hubv1.DateType_DATE_TYPE_ISSUED)
		case "dcterms:available":
			appendDates(record, term.values, hubv1.DateType_DATE_TYPE_AVAILABLE)
		case "dcterms:subject":
			appendSubjects(record, term.values)
		case "dcterms:language":
			appendOmekaCompatibilityValues(record, "Language", term.values, nil)
		case "dcterms:publisher":
			appendOmekaCompatibilityValues(record, "Publisher", term.values, nil)
		case "dcterms:rights", "dcterms:license", "dcterms:accessRights":
			appendRights(record, term.values)
		case "dcterms:identifier":
			appendIdentifiers(record, term.values, installationNamespace(source.uri))
		case "dcterms:relation", "dcterms:hasPart", "dcterms:isPartOf", "dcterms:hasFormat", "dcterms:isFormatOf", "dcterms:references", "dcterms:isReferencedBy", "dcterms:replaces", "dcterms:isReplacedBy", "dcterms:source":
			appendPropertyRelations(record, term.term, term.values)
		case "dcterms:bibliographicCitation":
			record.PreferredCitation = firstNonempty(record.PreferredCitation, firstDisplay(term.values))
		}
	}
}

func firstDisplayIndex(values []valueObject) int {
	for index, value := range values {
		if displayValue(value) != "" {
			return index
		}
	}
	return -1
}

func firstDisplayValue(source *resource, termName string) (string, bool) {
	for _, term := range source.properties {
		if term.term == termName {
			value := firstDisplay(term.values)
			return value, value != ""
		}
	}
	return "", false
}

func firstDisplay(values []valueObject) string {
	for _, value := range values {
		if display := displayValue(value); display != "" {
			return display
		}
	}
	return ""
}

func displayValue(value valueObject) string {
	switch value.typeName {
	case "literal":
		return strings.TrimSpace(value.literal)
	case "uri":
		return firstNonempty(value.label, value.uri)
	case "resource", "resource:item", "resource:itemset", "resource:media":
		return firstNonempty(value.displayTitle, value.uri, strconv.FormatInt(value.resourceID, 10))
	default:
		return ""
	}
}

func appendContributors(record *hubv1.Record, values []valueObject, role string) {
	for _, value := range values {
		name := displayValue(value)
		if name == "" {
			continue
		}
		contributor := &hubv1.Contributor{Name: name, Role: role, RoleCode: role}
		switch value.typeName {
		case "literal":
			contributor.ParsedName = helpers.ParseName(name)
		case "uri":
			contributor.AuthorityUri = value.uri
			registry := hub.DefaultIdentifierRegistry()
			identifier := &hubv1.Identifier{Value: value.uri, Scheme: registry.DetectScheme(value.uri)}
			if canonical, err := registry.CanonicalizeIdentifier(identifier); err == nil {
				contributor.Identifiers = append(contributor.Identifiers, canonical)
				if canonical.GetScheme() == "url" {
					contributor.Url = canonical.GetValue()
				}
			}
		case "resource", "resource:item", "resource:itemset", "resource:media":
			contributor.SourceId = strconv.FormatInt(value.resourceID, 10)
			contributor.Url = value.uri
		}
		record.Contributors = append(record.Contributors, contributor)
	}
}

func appendDates(record *hubv1.Record, values []valueObject, dateType hubv1.DateType) {
	for _, value := range values {
		raw := displayValue(value)
		if raw == "" {
			continue
		}
		date, err := helpers.ParseEDTF(raw, dateType)
		if err == nil && date.GetRaw() != "" {
			appendDateUnique(record, date)
		}
	}
}

func mapSourceDates(record *hubv1.Record, source *resource) {
	for _, entry := range []struct {
		value    string
		dateType hubv1.DateType
	}{{source.created, hubv1.DateType_DATE_TYPE_CREATED}, {source.modified, hubv1.DateType_DATE_TYPE_MODIFIED}} {
		if entry.value == "" {
			continue
		}
		date, err := helpers.ParseEDTF(entry.value, entry.dateType)
		if err == nil {
			appendDateUnique(record, date)
		}
	}
}

func appendDateUnique(record *hubv1.Record, value *hubv1.DateValue) {
	for _, existing := range record.Dates {
		if existing.GetType() == value.GetType() && existing.GetRaw() == value.GetRaw() {
			return
		}
	}
	record.Dates = append(record.Dates, value)
}

func appendSubjects(record *hubv1.Record, values []valueObject) {
	for _, value := range values {
		display := displayValue(value)
		if display == "" {
			continue
		}
		subject := &hubv1.Subject{Value: display, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL}
		if value.typeName == "uri" || strings.HasPrefix(value.typeName, "resource") {
			subject.Uri = value.uri
		}
		if value.resourceID != 0 {
			subject.SourceId = strconv.FormatInt(value.resourceID, 10)
		}
		record.Subjects = append(record.Subjects, subject)
	}
}

func appendRights(record *hubv1.Record, values []valueObject) {
	for _, value := range values {
		display := displayValue(value)
		if display == "" {
			continue
		}
		rights := &hubv1.Rights{Statement: display}
		if value.typeName == "uri" || strings.HasPrefix(value.typeName, "resource") {
			rights.Uri = value.uri
		}
		record.Rights = append(record.Rights, rights)
	}
}

func appendIdentifiers(record *hubv1.Record, values []valueObject, localNamespace string) {
	for _, value := range values {
		candidate := displayValue(value)
		namespace := localNamespace
		switch value.typeName {
		case "uri":
			candidate = value.uri
		case "resource", "resource:item", "resource:itemset", "resource:media":
			candidate = strconv.FormatInt(value.resourceID, 10)
			if value.uri != "" {
				namespace = resourceNamespace(value.uri)
			}
		}
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		registry := hub.DefaultIdentifierRegistry()
		scheme := registry.DetectScheme(candidate)
		identifier := &hubv1.Identifier{Value: candidate, Scheme: scheme}
		if scheme == "local" {
			identifier.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
			identifier.NamespaceUri = namespace
			identifier.IdentityLevel = hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD
		}
		canonical, err := registry.CanonicalizeIdentifier(identifier)
		if err != nil || identifierExists(record.Identifiers, canonical) {
			continue
		}
		record.Identifiers = append(record.Identifiers, canonical)
	}
}

func identifierExists(values []*hubv1.Identifier, candidate *hubv1.Identifier) bool {
	if candidate == nil {
		return false
	}
	for _, value := range values {
		if value.GetScheme() == candidate.GetScheme() && value.GetNamespaceUri() == candidate.GetNamespaceUri() && value.GetValue() == candidate.GetValue() {
			return true
		}
	}
	return false
}

func mapStructuralRelations(record *hubv1.Record, source *resource) {
	for _, value := range source.itemSets {
		record.Relations = append(record.Relations, relationFromReference(value, hubv1.RelationType_RELATION_TYPE_MEMBER_OF, hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION))
	}
	for _, value := range source.media {
		record.Relations = append(record.Relations, relationFromReference(value, hubv1.RelationType_RELATION_TYPE_HAS_PART, mediaResourceType("")))
	}
	if source.item != nil {
		record.Relations = append(record.Relations, relationFromReference(*source.item, hubv1.RelationType_RELATION_TYPE_PART_OF, hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED))
	}
}

func relationFromReference(value reference, relationType hubv1.RelationType, resourceType hubv1.ResourceTypeValue) *hubv1.Relation {
	return &hubv1.Relation{
		Type:               relationType,
		TargetTitle:        value.title,
		TargetId:           strconv.FormatInt(value.id, 10),
		TargetIdType:       hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
		TargetUri:          value.uri,
		SourceId:           strconv.FormatInt(value.id, 10),
		TargetResourceType: resourceType,
	}
}

func appendPropertyRelations(record *hubv1.Record, term string, values []valueObject) {
	relationType := omekaRelationType(term)
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

func mediaFile(source *resource) *hubv1.File {
	if source.originalURL == "" && source.filename == "" && source.mediaSource == "" {
		return nil
	}
	name := firstNonempty(source.filename, sourceBasename(source.mediaSource), sourceBasename(source.originalURL))
	return &hubv1.File{
		Name:              name,
		MimeType:          source.mediaType,
		SizeBytes:         source.size,
		Description:       source.altText,
		Role:              "original",
		Uri:               source.uri,
		Checksum:          source.sha256,
		ChecksumAlgorithm: checksumAlgorithm(source.sha256),
		AccessUrl:         source.originalURL,
	}
}

func sourceBasename(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if parsed, err := url.Parse(value); err == nil && parsed.Path != "" {
		value = parsed.Path
	}
	value = path.Base(value)
	if value == "." || value == "/" || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ""
	}
	return value
}

func checksumAlgorithm(checksum string) string {
	if checksum == "" {
		return ""
	}
	return "sha-256"
}

func omekaRelationType(term string) hubv1.RelationType {
	switch term {
	case "dcterms:hasPart":
		return hubv1.RelationType_RELATION_TYPE_HAS_PART
	case "dcterms:isPartOf":
		return hubv1.RelationType_RELATION_TYPE_PART_OF
	case "dcterms:hasFormat":
		return hubv1.RelationType_RELATION_TYPE_HAS_FORMAT
	case "dcterms:isFormatOf":
		return hubv1.RelationType_RELATION_TYPE_FORMAT_OF
	case "dcterms:references":
		return hubv1.RelationType_RELATION_TYPE_REFERENCES
	case "dcterms:isReferencedBy":
		return hubv1.RelationType_RELATION_TYPE_IS_CITED_BY
	case "dcterms:replaces":
		return hubv1.RelationType_RELATION_TYPE_REPLACES
	case "dcterms:isReplacedBy":
		return hubv1.RelationType_RELATION_TYPE_IS_REPLACED_BY
	case "dcterms:source":
		return hubv1.RelationType_RELATION_TYPE_DERIVED_FROM
	default:
		return hubv1.RelationType_RELATION_TYPE_RELATED_TO
	}
}

func omekaResourceType(value string) hubv1.ResourceTypeValue {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "dctype:collection", "schema:collection", "collection":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION
	case "dctype:dataset", "schema:dataset", "dataset":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET
	case "dctype:stillimage", "schema:imageobject", "image", "still image":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE
	case "dctype:movingimage", "schema:videoobject", "video", "moving image":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO
	case "dctype:sound", "schema:audioobject", "audio", "sound":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO
	case "dctype:text", "schema:textdigitaldocument", "text":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT
	case "dctype:software", "schema:softwareapplication", "software":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE
	case "dctype:physicalobject", "schema:individualproduct", "physical object":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_OBJECT
	case "bibo:article", "schema:scholarlyarticle", "article":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE
	case "bibo:book", "schema:book", "book":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK
	case "schema:map", "map":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_MAP
	case "schema:thesis", "thesis":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS
	default:
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER
	}
}

func mediaResourceType(mediaType string) hubv1.ResourceTypeValue {
	switch {
	case strings.HasPrefix(strings.ToLower(mediaType), "image/"):
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE
	case strings.HasPrefix(strings.ToLower(mediaType), "video/"):
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO
	case strings.HasPrefix(strings.ToLower(mediaType), "audio/"):
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO
	case strings.HasPrefix(strings.ToLower(mediaType), "text/"), strings.EqualFold(mediaType, "application/pdf"):
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT
	default:
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER
	}
}

func appendUnique(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
