package drupal

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"google.golang.org/protobuf/proto"
)

type compiledDrupalField struct {
	cardinality int
	values      []any
	position    int
}

// EncodeEntityWithProfile applies an immutable Drupal system profile to one
// Hub record and returns Drupal field values keyed by machine name. It is the
// shared profile execution boundary for transports such as Drupal JSON and
// Islandora Workbench; those transports may choose different outer encodings,
// but must not reimplement or guess the profile's Field-to-Hub mappings.
func EncodeEntityWithProfile(record *hubv1.Record, compiled *profile.Compiled) (map[string]any, error) {
	if record == nil {
		return nil, fmt.Errorf("encoding Drupal entity: record is nil")
	}
	if compiled == nil {
		return nil, fmt.Errorf("encoding Drupal entity: compiled profile is required")
	}
	if compiled.System() != "drupal" {
		return nil, fmt.Errorf("encoding Drupal entity: profile system %q is not drupal", compiled.System())
	}
	prepared, ok := proto.Clone(record).(*hubv1.Record)
	if !ok {
		return nil, fmt.Errorf("encoding Drupal entity: copying record")
	}
	// Workbench resolves textual references while Drupal entity JSON normally
	// requires numeric IDs. Supply deterministic textual fallbacks only in this
	// transport adapter; direct Drupal serialization retains its stricter ID
	// behavior through recordToEntityWithCompiledProfile.
	for _, contributor := range prepared.Contributors {
		if contributor != nil && contributor.SourceId == "" {
			contributor.SourceId = contributor.Name
		}
	}
	for _, relation := range prepared.Relations {
		if relation != nil && relation.SourceId == "" && relation.TargetId == "" {
			relation.TargetId = relation.TargetTitle
		}
	}
	return recordToEntityWithCompiledProfile(prepared, compiled)
}

func recordToEntityWithCompiledProfile(record *hubv1.Record, compiled *profile.Compiled) (map[string]any, error) {
	mappings, err := compiledDrupalMappings(compiled, true)
	if err != nil {
		return nil, fmt.Errorf("using profile %q: %w", compiled.Name(), err)
	}

	fields := make(map[string]*compiledDrupalField)
	for _, entry := range mappings {
		if entry.mapping.Encode == "none" {
			continue
		}
		selector := entry.mapping.Field.Selector
		values, err := encodeCompiledDrupalMapping(record, entry, compiled)
		if err != nil {
			return nil, fmt.Errorf(
				"profile %q mapping %d Hub %q to Drupal field %q with codec %q: %w",
				compiled.Name(), entry.mapping.Position+1, entry.mapping.Hub,
				selector.Path, entry.mapping.Encode, err,
			)
		}
		if len(values) == 0 {
			continue
		}

		field := fields[selector.Path]
		if field == nil {
			field = &compiledDrupalField{cardinality: entry.mapping.Field.Cardinality}
			fields[selector.Path] = field
		} else if field.cardinality != entry.mapping.Field.Cardinality {
			return nil, fmt.Errorf(
				"profile %q mapping %d resolves Drupal field %q with cardinality %d after cardinality %d",
				compiled.Name(), entry.mapping.Position+1, selector.Path,
				entry.mapping.Field.Cardinality, field.cardinality,
			)
		}

		switch entry.mapping.Merge {
		case profile.MergeFirstNonempty:
			// A selector-qualified composite mapping contributes one member of
			// a structured Drupal field. Distinct members (for example title,
			// ISSN, volume, and issue) must be aggregated even though each Hub
			// destination is scalar and therefore correctly uses first_nonempty
			// while decoding.
			if entry.mapping.Encode == "composite" && selector.Attribute != "" {
				field.values = append(field.values, values...)
			} else if len(field.values) == 0 {
				field.values = values
			}
		case profile.MergeAppend:
			field.values = append(field.values, values...)
		case profile.MergeReplace:
			field.values = values
		default:
			return nil, fmt.Errorf("profile %q mapping %d has unsupported merge policy %q", compiled.Name(), entry.mapping.Position+1, entry.mapping.Merge)
		}
		field.position = entry.mapping.Position
	}

	entity := make(map[string]any, len(fields))
	for path, field := range fields {
		if field.cardinality > 0 && len(field.values) > field.cardinality {
			return nil, fmt.Errorf(
				"profile %q mapping %d encodes %d values for Drupal field %q with cardinality %d",
				compiled.Name(), field.position+1, len(field.values), path, field.cardinality,
			)
		}
		entity[path] = field.values
	}
	return entity, nil
}

func encodeCompiledDrupalMapping(record *hubv1.Record, entry compiledDrupalMapping, compiled *profile.Compiled) ([]any, error) {
	if entry.mapping.Encode == "typed-identifier" {
		return encodeCompiledIdentifiers(record, entry, compiled)
	}
	values, err := compiledHubValues(record, entry)
	if err != nil {
		return nil, err
	}
	result := make([]any, 0, len(values))
	for valueIndex, value := range values {
		encoded, err := encodeCompiledDrupalValue(value, entry)
		if err != nil {
			return nil, fmt.Errorf("hub value %d: %w", valueIndex+1, err)
		}
		if encoded != nil {
			result = append(result, encoded)
		}
	}
	return result, nil
}

func compiledHubValues(record *hubv1.Record, entry compiledDrupalMapping) ([]any, error) {
	base, qualifier := mapping.IRFieldName(entry.mapping.Hub)
	switch base {
	case "Title":
		return nonemptyStringValue(record.GetTitle()), nil
	case "FullTitle":
		return nonemptyStringValue(record.GetFullTitle()), nil
	case "AltTitle":
		return stringValues(record.GetAltTitle()), nil
	case "Abstract":
		return nonemptyStringValue(record.GetAbstract()), nil
	case "Description":
		return nonemptyStringValue(record.GetDescription()), nil
	case "Publisher":
		return stringValues(hub.GetPublishers(record)), nil
	case "PlacePublished":
		return nonemptyStringValue(record.GetPlacePublished()), nil
	case "PhysicalDesc":
		return nonemptyStringValue(record.GetPhysicalDesc()), nil
	case "Notes":
		return stringValues(record.GetNotes()), nil
	case "TableOfContents":
		return nonemptyStringValue(record.GetTableOfContents()), nil
	case "Source":
		return nonemptyStringValue(record.GetSource()), nil
	case "DigitalOrigin":
		return nonemptyStringValue(record.GetDigitalOrigin()), nil
	case "Edition":
		return nonemptyStringValue(record.GetEdition()), nil
	case "Version":
		return nonemptyStringValue(record.GetVersion()), nil
	case "PreferredCitation":
		return nonemptyStringValue(record.GetPreferredCitation()), nil
	case "ObjectModel":
		return nonemptyStringValue(record.GetObjectModel()), nil
	case "CaptureDevice":
		return nonemptyStringValue(record.GetCaptureDevice()), nil
	case "Dimensions":
		return nonemptyStringValue(record.GetDimensions()), nil
	case "Duration":
		return nonemptyStringValue(record.GetDuration()), nil
	case "AccessCondition":
		return nonemptyStringValue(record.GetAccessCondition()), nil
	case "LocalRestriction":
		return nonemptyStringValue(record.GetLocalRestriction()), nil
	case "Language":
		return nonemptyStringValue(record.GetLanguage()), nil
	case "Departments":
		return stringValues(record.GetDepartments()), nil
	case "IsPublic":
		return []any{record.GetIsPublic()}, nil
	case "AddCoverpage":
		return []any{record.GetAddCoverpage()}, nil
	case "PPI", "Ppi":
		return []any{record.GetPpi()}, nil
	case "PageCount":
		return []any{record.GetPageCount()}, nil
	case "Contributors":
		return contributorValues(record.GetContributors(), qualifier), nil
	case "Dates":
		return dateValues(record.GetDates(), qualifier, entry.mapping.Field.Selector.Path), nil
	case "ResourceType":
		if record.GetResourceType() == nil {
			return nil, nil
		}
		return []any{record.GetResourceType()}, nil
	case "Genre", "Genres":
		return subjectValues(record.GetGenres(), qualifier, entry.mapping.Field.Selector.Path), nil
	case "PhysicalForm":
		return subjectValues(record.GetPhysicalForm(), qualifier, entry.mapping.Field.Selector.Path), nil
	case "Subjects":
		return subjectValues(record.GetSubjects(), qualifier, entry.mapping.Field.Selector.Path), nil
	case "Rights":
		return pointerValues(record.GetRights()), nil
	case "Identifiers":
		return identifierValues(record.GetIdentifiers(), qualifier), nil
	case "Relations":
		return relationValues(record.GetRelations(), qualifier, entry.mapping.Field.Selector.Path), nil
	case "Files":
		return fileValues(record.GetFiles(), qualifier), nil
	case "DegreeInfo":
		return degreeInfoValues(record.GetDegreeInfo(), qualifier)
	case "Publication":
		return publicationValues(record.GetPublication(), qualifier)
	case "ArchivalLocation":
		return archivalLocationValues(record.GetArchivalLocation(), qualifier)
	case "Geographic":
		return geographicValues(record.GetGeographic(), qualifier)
	case "Funders":
		return funderValues(record.GetFunders(), qualifier), nil
	case "Extra":
		return extraValues(record, qualifier), nil
	default:
		return nil, fmt.Errorf("unsupported Hub path %q", entry.mapping.Hub)
	}
}

func nonemptyStringValue(value string) []any {
	if value == "" {
		return nil
	}
	return []any{value}
}

func stringValues(values []string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func pointerValues[T any](values []*T) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, value)
		}
	}
	return result
}

func contributorValues(values []*hubv1.Contributor, qualifier string) []any {
	if qualifier == "" {
		return pointerValues(values)
	}
	wanted := normalizedQualifier(qualifier)
	result := make([]any, 0, len(values))
	for _, contributor := range values {
		if contributor == nil {
			continue
		}
		role := normalizedQualifier(contributor.GetRoleCode())
		if role == "" {
			role = normalizedQualifier(contributor.GetRole())
		}
		if role == wanted || strings.TrimPrefix(role, "relators_") == strings.TrimPrefix(wanted, "relators_") {
			result = append(result, contributor)
		}
	}
	return result
}

func dateValues(values []*hubv1.DateValue, qualifier, fieldPath string) []any {
	wanted := hubv1.DateType_DATE_TYPE_UNSPECIFIED
	filter := false
	if qualifier != "" {
		wanted, filter = dateTypeForEncoding(qualifier)
	} else if inferred := inferDateType("", fieldPath); inferred != "other" {
		wanted, filter = dateTypeForEncoding(inferred)
	}
	result := make([]any, 0, len(values))
	for _, date := range values {
		if date != nil && (!filter || date.GetType() == wanted) {
			result = append(result, date)
		}
	}
	return result
}

func dateTypeForEncoding(value string) (hubv1.DateType, bool) {
	typeValue := dateTypeFromString(strings.ReplaceAll(strings.ToLower(value), "-", "_"))
	return typeValue, typeValue != hubv1.DateType_DATE_TYPE_OTHER || normalizedQualifier(value) == "other"
}

func subjectValues(values []*hubv1.Subject, qualifier, fieldPath string) []any {
	wanted := subjectVocabularyFromString(qualifier)
	filter := qualifier != "" && wanted != hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
	if !filter {
		inferred := inferVocabulary("", fieldPath)
		wanted = subjectVocabularyFromString(inferred)
		filter = inferred != "" && wanted != hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
	}
	result := make([]any, 0, len(values))
	for _, subject := range values {
		if subject != nil && (!filter || subject.GetVocabulary() == wanted) {
			result = append(result, subject)
		}
	}
	return result
}

func identifierValues(values []*hubv1.Identifier, qualifier string) []any {
	result := make([]any, 0, len(values))
	for _, identifier := range values {
		if identifier != nil && (qualifier == "" || normalizedQualifier(identifier.GetScheme()) == normalizedQualifier(qualifier)) {
			result = append(result, identifier)
		}
	}
	return result
}

func relationValues(values []*hubv1.Relation, qualifier, fieldPath string) []any {
	wanted, filter := relationTypeForEncoding(qualifier)
	if !filter {
		wanted, filter = inferredRelationType(fieldPath)
	}
	result := make([]any, 0, len(values))
	for _, relation := range values {
		if relation != nil && (!filter || relation.GetType() == wanted) {
			result = append(result, relation)
		}
	}
	return result
}

func relationTypeForEncoding(value string) (hubv1.RelationType, bool) {
	if strings.TrimSpace(value) == "" {
		return hubv1.RelationType_RELATION_TYPE_UNSPECIFIED, false
	}
	name := "RELATION_TYPE_" + strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"), " ", "_"))
	number, exists := hubv1.RelationType_value[name]
	return hubv1.RelationType(number), exists
}

func inferredRelationType(fieldPath string) (hubv1.RelationType, bool) {
	lower := strings.ToLower(fieldPath)
	candidates := []string{
		"is_supplement_to", "supplemented_by", "is_documented_by", "is_described_by",
		"is_replaced_by", "is_cited_by", "has_version", "has_member", "has_format",
		"member_of", "part_of", "has_part", "version_of", "format_of", "derived_from",
		"source_of", "based_on", "is_basis_for", "supplements", "documents", "describes",
		"references", "cites", "replaces", "same_as", "identical_to", "in_series",
		"series_of", "related_to", "reviews", "requires", "required_by",
	}
	for _, candidate := range candidates {
		if strings.Contains(lower, candidate) {
			return relationTypeForEncoding(candidate)
		}
	}
	return hubv1.RelationType_RELATION_TYPE_UNSPECIFIED, false
}

func fileValues(values []*hubv1.File, qualifier string) []any {
	result := make([]any, 0, len(values))
	for _, file := range values {
		if file != nil && (qualifier == "" || normalizedQualifier(file.GetRole()) == normalizedQualifier(qualifier)) {
			result = append(result, file)
		}
	}
	return result
}

func degreeInfoValues(degree *hubv1.DegreeInfo, qualifier string) ([]any, error) {
	if degree == nil {
		return nil, nil
	}
	switch qualifier {
	case "DegreeName":
		return nonemptyStringValue(degree.GetDegreeName()), nil
	case "DegreeLevel":
		return nonemptyStringValue(degree.GetDegreeLevel()), nil
	case "Department":
		return nonemptyStringValue(degree.GetDepartment()), nil
	case "Institution":
		return nonemptyStringValue(degree.GetInstitution()), nil
	case "Date":
		if degree.GetDate() == nil {
			return nil, nil
		}
		return []any{degree.GetDate()}, nil
	case "":
		return []any{degree}, nil
	default:
		return nil, fmt.Errorf("unsupported Hub path %q", "DegreeInfo."+qualifier)
	}
}

func publicationValues(publication *hubv1.PublicationDetails, qualifier string) ([]any, error) {
	if publication == nil {
		return nil, nil
	}
	var value string
	switch qualifier {
	case "Title":
		value = publication.GetTitle()
	case "Volume":
		value = publication.GetVolume()
	case "Issue":
		value = publication.GetIssue()
	case "Pages":
		value = publication.GetPages()
	case "Issn":
		value = publication.GetIssn()
	case "LIssn":
		value = publication.GetLIssn()
	case "":
		return []any{publication}, nil
	default:
		return nil, fmt.Errorf("unsupported Hub path %q", "Publication."+qualifier)
	}
	return nonemptyStringValue(value), nil
}

func archivalLocationValues(location *hubv1.ArchivalLocation, qualifier string) ([]any, error) {
	if location == nil {
		return nil, nil
	}
	var value string
	switch qualifier {
	case "Collection":
		value = location.GetCollection()
	case "Series":
		value = location.GetSeries()
	case "Box":
		value = location.GetBox()
	case "Folder":
		value = location.GetFolder()
	case "":
		return []any{location}, nil
	default:
		return nil, fmt.Errorf("unsupported Hub path %q", "ArchivalLocation."+qualifier)
	}
	return nonemptyStringValue(value), nil
}

func geographicValues(location *hubv1.HierarchicalGeographic, qualifier string) ([]any, error) {
	if location == nil {
		return nil, nil
	}
	var value string
	switch qualifier {
	case "Country":
		value = location.GetCountry()
	case "State":
		value = location.GetState()
	case "County":
		value = location.GetCounty()
	case "City":
		value = location.GetCity()
	case "Area":
		value = location.GetArea()
	case "":
		return []any{location}, nil
	default:
		return nil, fmt.Errorf("unsupported Hub path %q", "Geographic."+qualifier)
	}
	return nonemptyStringValue(value), nil
}

func funderValues(funders []*hubv1.Funder, qualifier string) []any {
	result := make([]any, 0, len(funders))
	for _, funder := range funders {
		if funder == nil {
			continue
		}
		if qualifier == "" {
			result = append(result, funder)
			continue
		}
		var values []string
		switch qualifier {
		case "Name":
			values = []string{funder.GetName()}
		case "Identifier":
			values = []string{funder.GetIdentifier()}
		case "AwardNumbers":
			values = funder.GetAwardNumbers()
		case "AwardTitle":
			values = []string{funder.GetAwardTitle()}
		case "AwardUri":
			values = []string{funder.GetAwardUri()}
		}
		result = append(result, stringValues(values)...)
	}
	return result
}

func extraValues(record *hubv1.Record, qualifier string) []any {
	extra := hub.GetExtraFields(record)
	if qualifier == "" {
		if len(extra) == 0 {
			return nil
		}
		return []any{extra}
	}
	value, exists := extra[qualifier]
	if !exists || value == nil {
		return nil
	}
	if values, ok := value.([]any); ok {
		return append([]any(nil), values...)
	}
	return []any{value}
}

func encodeCompiledDrupalValue(value any, entry compiledDrupalMapping) (any, error) {
	selector := entry.mapping.Field.Selector
	switch entry.mapping.Encode {
	case "text":
		text, err := drupalTextValue(value)
		if err != nil || text == "" {
			return nil, err
		}
		return scalarDrupalValue(selector, "value", text)
	case "integer":
		integer, err := drupalIntegerValue(value)
		if err != nil {
			return nil, err
		}
		return scalarDrupalValue(selector, "value", integer)
	case "decimal":
		decimal, err := drupalDecimalValue(value)
		if err != nil {
			return nil, err
		}
		return scalarDrupalValue(selector, "value", decimal)
	case "boolean":
		boolean, err := drupalBooleanValue(value)
		if err != nil {
			return nil, err
		}
		return scalarDrupalValue(selector, "value", boolean)
	case "date":
		date, err := drupalDateValue(value)
		if err != nil || date == "" {
			return nil, err
		}
		return scalarDrupalValue(selector, "value", date)
	case "link":
		return linkDrupalValue(value, selector)
	case "reference":
		return referenceDrupalValue(value, entry)
	case "typed-relation":
		return typedRelationDrupalValue(value, entry)
	case "file":
		return fileDrupalValue(value, selector)
	case "composite", "opaque":
		return compositeDrupalValue(value, selector)
	default:
		return nil, fmt.Errorf("unsupported codec %q", entry.mapping.Encode)
	}
}

func scalarDrupalValue(selector profile.FieldSelector, defaultAttribute string, value any) (map[string]any, error) {
	attribute := selector.Attribute
	if attribute == "" {
		attribute = defaultAttribute
	}
	result := map[string]any{attribute: value}
	if err := applyDrupalPredicate(result, selector); err != nil {
		return nil, err
	}
	return result, nil
}

func applyDrupalPredicate(value map[string]any, selector profile.FieldSelector) error {
	if selector.Where == nil {
		return nil
	}
	if existing, exists := value[selector.Where.Attribute]; exists && scalarText(existing) != selector.Where.Equals {
		return fmt.Errorf("selector predicate %s=%q conflicts with encoded value %q", selector.Where.Attribute, selector.Where.Equals, scalarText(existing))
	}
	value[selector.Where.Attribute] = selector.Where.Equals
	return nil
}

func drupalTextValue(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case *hubv1.Contributor:
		if typed.GetName() != "" {
			return typed.GetName(), nil
		}
		return typed.GetSourceId(), nil
	case *hubv1.DateValue:
		return hub.FormatEDTF(typed), nil
	case *hubv1.ResourceType:
		return hub.ResourceTypeString(typed), nil
	case *hubv1.Subject:
		if typed.GetValue() != "" {
			return typed.GetValue(), nil
		}
		return typed.GetSourceId(), nil
	case *hubv1.Rights:
		if typed.GetUri() != "" {
			return typed.GetUri(), nil
		}
		return typed.GetStatement(), nil
	case *hubv1.Identifier:
		return typed.GetValue(), nil
	case *hubv1.Relation:
		for _, candidate := range []string{typed.GetTargetTitle(), typed.GetSourceId(), typed.GetTargetId(), typed.GetTargetUri()} {
			if candidate != "" {
				return candidate, nil
			}
		}
		return "", nil
	case bool:
		return strconv.FormatBool(typed), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32), nil
	case int:
		return strconv.Itoa(typed), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	default:
		return "", fmt.Errorf("expected a scalar or text-compatible Hub value, got %T", value)
	}
}

func drupalIntegerValue(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		// MaxInt64 rounds to 2^63 as a float64, so the upper bound must be
		// inclusive to reject that unrepresentable int64 value before conversion.
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed || typed < math.MinInt64 || typed >= math.MaxInt64 {
			return 0, fmt.Errorf("decimal %v is not an integer", typed)
		}
		return int64(typed), nil
	case string:
		integer, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parsing integer %q: %w", typed, err)
		}
		return integer, nil
	default:
		return 0, fmt.Errorf("expected an integer Hub value, got %T", value)
	}
}

func drupalDecimalValue(value any) (float64, error) {
	var result float64
	switch typed := value.(type) {
	case float64:
		result = typed
	case float32:
		result = float64(typed)
	case int:
		result = float64(typed)
	case int32:
		result = float64(typed)
	case int64:
		result = float64(typed)
	case string:
		var err error
		result, err = strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, fmt.Errorf("parsing decimal %q: %w", typed, err)
		}
	default:
		return 0, fmt.Errorf("expected a numeric Hub value, got %T", value)
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, fmt.Errorf("decimal %v is not finite", result)
	}
	return result, nil
}

func drupalBooleanValue(value any) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case int:
		if typed == 0 || typed == 1 {
			return typed == 1, nil
		}
	case int32:
		if typed == 0 || typed == 1 {
			return typed == 1, nil
		}
	case int64:
		if typed == 0 || typed == 1 {
			return typed == 1, nil
		}
	case float64:
		if typed == 0 || typed == 1 {
			return typed == 1, nil
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1":
			return true, nil
		case "false", "0":
			return false, nil
		}
	}
	return false, fmt.Errorf("expected a boolean Hub value, got %T(%v)", value, value)
}

func drupalDateValue(value any) (string, error) {
	switch typed := value.(type) {
	case *hubv1.DateValue:
		formatted := hub.FormatEDTF(typed)
		if formatted == "" && strings.TrimSpace(typed.GetRaw()) != "" {
			parsed, err := helpers.ParseEDTF(typed.GetRaw(), typed.GetType())
			if err != nil {
				return "", fmt.Errorf("parsing raw EDTF date %q: %w", typed.GetRaw(), err)
			}
			formatted = hub.FormatEDTF(parsed)
		}
		if formatted == "" {
			return "", fmt.Errorf("hub date has no serializable EDTF value")
		}
		return formatted, nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("expected a Hub date or EDTF string, got %T", value)
	}
}

func linkDrupalValue(value any, selector profile.FieldSelector) (map[string]any, error) {
	uri, title := "", ""
	switch typed := value.(type) {
	case string:
		uri, title, _ = strings.Cut(typed, "%%")
		uri = strings.TrimSpace(uri)
	case *hubv1.Rights:
		uri, title = typed.GetUri(), typed.GetStatement()
	case *hubv1.Subject:
		uri, title = typed.GetUri(), typed.GetValue()
	case *hubv1.Relation:
		uri, title = typed.GetTargetUri(), typed.GetTargetTitle()
	case *hubv1.Identifier:
		uri, title = typed.GetValue(), typed.GetDisplay()
	case *hubv1.Funder:
		uri, title = typed.GetAwardUri(), typed.GetAwardTitle()
	default:
		return nil, fmt.Errorf("expected a link-compatible Hub value, got %T", value)
	}
	if uri == "" {
		return nil, fmt.Errorf("link-compatible Hub value has no URI")
	}
	attribute := selector.Attribute
	if attribute == "" {
		attribute = "uri"
	}
	result := map[string]any{attribute: uri}
	if title != "" && attribute != "title" {
		result["title"] = title
	}
	if err := applyDrupalPredicate(result, selector); err != nil {
		return nil, err
	}
	return result, nil
}

func referenceDrupalValue(value any, entry compiledDrupalMapping) (map[string]any, error) {
	targetID, targetUUID, targetURL := "", "", ""
	switch typed := value.(type) {
	case string:
		targetID = typed
	case *hubv1.Contributor:
		targetID = typed.GetSourceId()
	case *hubv1.Subject:
		targetID = typed.GetSourceId()
		if targetID == "" {
			targetID = typed.GetValue()
		}
	case *hubv1.Relation:
		targetID = typed.GetSourceId()
		if targetID == "" {
			targetID = typed.GetTargetId()
		}
		targetURL = typed.GetTargetUri()
	case *hubv1.ResourceType:
		targetID = typed.GetOriginal()
		if targetID == "" {
			targetID = hub.ResourceTypeString(typed)
		}
	case *hubv1.Rights:
		targetID = typed.GetStatement()
		if targetID == "" {
			targetID = typed.GetUri()
		}
	case *hubv1.Identifier:
		targetID = typed.GetValue()
	default:
		return nil, fmt.Errorf("expected a reference-compatible Hub value, got %T", value)
	}
	if targetID == "" {
		return nil, fmt.Errorf("reference-compatible Hub value has no source identifier")
	}
	attribute := entry.mapping.Field.Selector.Attribute
	if attribute == "" {
		attribute = "target_id"
	}
	result := map[string]any{attribute: targetID}
	if entry.mapping.Field.Reference != nil && entry.mapping.Field.Reference.EntityType != "" {
		result["target_type"] = entry.mapping.Field.Reference.EntityType
	}
	if targetUUID != "" {
		result["target_uuid"] = targetUUID
	}
	if targetURL != "" {
		result["url"] = targetURL
	}
	if err := applyDrupalPredicate(result, entry.mapping.Field.Selector); err != nil {
		return nil, err
	}
	return result, nil
}

func typedRelationDrupalValue(value any, entry compiledDrupalMapping) (map[string]any, error) {
	if encoded, ok := value.(string); ok {
		if reference := entry.mapping.Field.Reference; reference != nil {
			role, targetID, parsed := splitTypedRelationString(encoded, reference.Bundles)
			if parsed {
				result := map[string]any{"target_id": targetID, "target_type": reference.EntityType}
				if role != "" {
					result["rel_type"] = role
				}
				if err := applyDrupalPredicate(result, entry.mapping.Field.Selector); err != nil {
					return nil, err
				}
				return result, nil
			}
		}
	}
	result, err := referenceDrupalValue(value, entry)
	if err != nil {
		return nil, err
	}
	role := ""
	switch typed := value.(type) {
	case *hubv1.Contributor:
		role = typed.GetRoleCode()
		if role == "" && typed.GetRole() != "" {
			role = "relators:" + helpers.RoleToCode(typed.GetRole())
		}
	case *hubv1.Relation:
		role = strings.ToLower(strings.TrimPrefix(typed.GetType().String(), "RELATION_TYPE_"))
	}
	if role != "" {
		result["rel_type"] = role
	}
	if err := applyDrupalPredicate(result, entry.mapping.Field.Selector); err != nil {
		return nil, err
	}
	return result, nil
}

func splitTypedRelationString(encoded string, bundles []string) (string, string, bool) {
	encoded = strings.TrimSpace(encoded)
	for _, bundle := range bundles {
		bundle = strings.TrimSpace(bundle)
		if bundle == "" {
			continue
		}
		markers := []string{bundle}
		switch bundle {
		case "corporate_body":
			markers = append(markers, "organization")
		case "organization":
			markers = append(markers, "corporate_body")
		}
		for _, markerBundle := range markers {
			if target, found := strings.CutPrefix(encoded, markerBundle+":"); found {
				target = strings.TrimSpace(target)
				if target != "" {
					return "", target, true
				}
			}
			marker := ":" + markerBundle + ":"
			if index := strings.Index(encoded, marker); index > 0 {
				role := strings.TrimSpace(encoded[:index])
				target := strings.TrimSpace(encoded[index+len(marker):])
				if role != "" && target != "" {
					return role, target, true
				}
			}
		}
	}
	return "", "", false
}

func fileDrupalValue(value any, selector profile.FieldSelector) (map[string]any, error) {
	file, ok := value.(*hubv1.File)
	if !ok {
		return nil, fmt.Errorf("expected a Hub file, got %T", value)
	}
	result := make(map[string]any)
	setNonemptyString(result, "path", file.GetPath())
	setNonemptyString(result, "filename", file.GetName())
	setNonemptyString(result, "filemime", file.GetMimeType())
	setNonemptyString(result, "uri", file.GetUri())
	setNonemptyString(result, "url", file.GetAccessUrl())
	setNonemptyString(result, "description", file.GetDescription())
	setNonemptyString(result, "checksum", file.GetChecksum())
	setNonemptyString(result, "checksum_algorithm", file.GetChecksumAlgorithm())
	if file.GetSizeBytes() != 0 {
		result["filesize"] = file.GetSizeBytes()
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("hub file has no path, name, URI, URL, or metadata")
	}
	if err := applyDrupalPredicate(result, selector); err != nil {
		return nil, err
	}
	return result, nil
}

func setNonemptyString(target map[string]any, key, value string) {
	if value != "" {
		target[key] = value
	}
}

func compositeDrupalValue(value any, selector profile.FieldSelector) (map[string]any, error) {
	if object, ok := value.(map[string]any); ok {
		result := cloneStringMap(object)
		if err := applyDrupalPredicate(result, selector); err != nil {
			return nil, err
		}
		return result, nil
	}
	if selector.Attribute != "" {
		return scalarDrupalValue(selector, selector.Attribute, value)
	}
	return scalarDrupalValue(selector, "value", value)
}

func cloneStringMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func encodeCompiledIdentifiers(record *hubv1.Record, entry compiledDrupalMapping, compiled *profile.Compiled) ([]any, error) {
	selector := entry.mapping.Field.Selector
	rules := identifierRulesForSelector(compiled, selector)
	if len(rules) == 0 {
		return nil, fmt.Errorf("typed-identifier mapping has no identity rule for its selector")
	}
	result := make([]any, 0)
	for identifierIndex, identifier := range record.GetIdentifiers() {
		for _, rule := range rules {
			canonical, matches, err := canonicalIdentifierMatchingRule(identifier, rule, compiled)
			if err != nil {
				return nil, fmt.Errorf("hub identifier %d for rule %q: %w", identifierIndex+1, rule.Name, err)
			}
			if !matches {
				continue
			}
			attribute := selector.Attribute
			if attribute == "" {
				attribute = "value"
			}
			value := map[string]any{attribute: canonical.GetValue()}
			if err := applyDrupalPredicate(value, selector); err != nil {
				return nil, err
			}
			result = append(result, value)
			break
		}
	}
	return result, nil
}

func identifierRulesForSelector(compiled *profile.Compiled, selector profile.FieldSelector) []profile.CompiledIdentifierRule {
	result := make([]profile.CompiledIdentifierRule, 0)
	for _, rule := range compiled.LookupPlan().Identifiers {
		if selectorsEqual(rule.Value.Selector, selector) {
			result = append(result, rule)
		}
	}
	return result
}

func canonicalIdentifierMatchingRule(identifier *hubv1.Identifier, rule profile.CompiledIdentifierRule, compiled *profile.Compiled) (*hubv1.Identifier, bool, error) {
	if identifier == nil {
		return nil, false, nil
	}
	registry := compiled.IdentifierRegistry()
	if !identifierMayTargetRule(identifier, rule, registry) {
		return nil, false, nil
	}
	canonical, err := registry.CanonicalizeIdentifier(identifier)
	if err != nil {
		return nil, false, fmt.Errorf("canonicalizing %q as %s: %w", identifier.GetValue(), rule.Scheme, err)
	}
	registered, exists := registry.Rule(rule.Scheme)
	if !exists {
		return nil, false, fmt.Errorf("identifier scheme %q is not registered", rule.Scheme)
	}
	namespace := rule.Namespace
	if namespace == "" {
		namespace = registered.NamespaceURI
	}
	if canonical.GetScheme() != registered.Scheme || canonical.GetNamespaceUri() != namespace || canonical.GetIdentityLevel() != profileIdentityLevel(rule.IdentityLevel) {
		return canonical, false, nil
	}
	matched, err := compiled.MatchesIdentifier(rule.Name, canonical.GetValue())
	if err != nil {
		return nil, false, err
	}
	return canonical, matched, nil
}

func identifierMayTargetRule(identifier *hubv1.Identifier, rule profile.CompiledIdentifierRule, registry *hub.IdentifierRegistry) bool {
	if rawScheme := strings.TrimSpace(identifier.GetScheme()); rawScheme != "" {
		registered, exists := registry.Rule(rawScheme)
		return exists && registered.Scheme == rule.Scheme
	}
	ruleDefinition, exists := registry.Rule(rule.Scheme)
	if exists && identifier.GetType() != hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED {
		return ruleDefinition.Type == identifier.GetType()
	}
	return registry.DetectScheme(identifier.GetValue()) == rule.Scheme
}

func profileIdentityLevel(level profile.IdentifierIdentityLevel) hubv1.IdentifierIdentityLevel {
	switch level {
	case profile.IdentityWork:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK
	case profile.IdentityVersion:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION
	case profile.IdentityManifestation:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_MANIFESTATION
	case profile.IdentityConcept:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT
	case profile.IdentitySourceRecord:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD
	default:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED
	}
}

func normalizedQualifier(value string) string {
	return strings.ToLower(strings.NewReplacer(":", "_", "-", "_", " ", "_").Replace(strings.TrimSpace(value)))
}
