package archivesspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
)

var machineNameSeparators = regexp.MustCompile(`[^a-z0-9]+`)

func jsonModelToHub(value *decodedRecord, options *format.ParseOptions) (*hubv1.Record, error) {
	if value == nil {
		return nil, fmt.Errorf("JSONModel record is nil")
	}
	model := &value.model
	title := cleanText(firstNonempty(model.Title, model.DisplayString), options)
	if title == "" {
		return nil, fmt.Errorf("title or display_string is required")
	}
	sourceURI, err := safeSourceURI(model.URI, optionBaseURL(options))
	if err != nil {
		return nil, fmt.Errorf("source record URI: %w", err)
	}
	sourceID := sourceIdentifier(model)
	origin := "archivesspace"
	if options != nil && strings.TrimSpace(options.SourceName) != "" {
		origin = safeSourceOrigin(options.SourceName)
	}
	record := &hubv1.Record{
		Title: title,
		ResourceType: &hubv1.ResourceType{
			Type:       resourceType(model),
			Original:   firstNonempty(model.ResourceType, model.OtherLevel, model.Level, model.JSONModelType),
			Vocabulary: "ArchivesSpace JSONModel",
		},
		SourceInfo: &hubv1.SourceInfo{
			Format:        "archivesspace",
			FormatVersion: Version,
			SourceId:      sourceID,
			Origin:        origin,
			SourceUri:     sourceURI,
		},
	}
	if options != nil {
		if options.Profile != nil {
			record.SourceInfo.Profile = options.Profile.Name
		}
	}

	sourceIdentifiers := appendRecordIdentifiers(record, model, options)
	appendDates(record, model)
	appendAgents(record, model, options)
	appendSubjects(record, model, options)
	appendNotes(record, model, options)
	appendExtents(record, model, options)
	appendRights(record, model, options)
	appendRelations(record, model, options)
	appendFiles(record, model, options)
	appendArchivalContainers(record, model)
	appendLanguages(record, model, options)
	if err := appendExtras(record, value, options, sourceIdentifiers); err != nil {
		return nil, err
	}
	return record, nil
}

func safeSourceOrigin(value string) string {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "://") {
		return value
	}
	if safe, err := safeSourceURI(value, ""); err == nil && safe != "" {
		return safe
	}
	return "archivesspace"
}

func resourceType(model *jsonModel) hubv1.ResourceTypeValue {
	if model.JSONModelType == "resource" {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION
	}
	level := strings.ToLower(firstNonempty(model.OtherLevel, model.Level))
	if strings.Contains(level, "manuscript") {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_MANUSCRIPT
	}
	return hubv1.ResourceTypeValue_RESOURCE_TYPE_ARCHIVAL_MATERIAL
}

func sourceIdentifier(model *jsonModel) string {
	if model == nil {
		return ""
	}
	if model.URI != "" {
		return model.URI
	}
	if model.JSONModelType == "resource" {
		return resourceIdentifier(model)
	}
	return firstNonempty(model.RefID, model.ComponentID)
}

func resourceIdentifier(model *jsonModel) string {
	if model == nil {
		return ""
	}
	parts := []string{
		strings.TrimSpace(model.ID0), strings.TrimSpace(model.ID1),
		strings.TrimSpace(model.ID2), strings.TrimSpace(model.ID3),
	}
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "-")
}

func appendRecordIdentifiers(record *hubv1.Record, model *jsonModel, options *format.ParseOptions) []sourceIdentifierExtra {
	baseURL := optionBaseURL(options)
	installationNamespace := normalizedNamespace(baseURL)
	pending := make([]*hubv1.Identifier, 0)
	pending = append(pending, &hubv1.Identifier{
		Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, Value: model.URI,
		Scheme: "archivesspace-uri", NamespaceUri: installationNamespace,
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
		IsPreferred:   model.URI != "",
	})
	if model.JSONModelType == "resource" {
		pending = append(pending, &hubv1.Identifier{
			Type:          hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
			Value:         resourceIdentifier(model),
			Scheme:        "archivesspace-resource-id",
			NamespaceUri:  installationNamespace,
			IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
			IsPreferred:   model.URI == "",
		})
		pending = append(pending, localIdentifier("archivesspace-ead-id", model.EADID, installationNamespace))
	} else {
		resourceNamespace := installationNamespace
		if model.Resource.Ref != "" {
			resourceNamespace = namespaceForRecord(baseURL, model.Resource.Ref)
		}
		pending = append(pending, &hubv1.Identifier{
			Type:          hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
			Value:         model.RefID,
			Scheme:        "archivesspace-ref-id",
			NamespaceUri:  resourceNamespace,
			IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
		})
		pending = append(pending, &hubv1.Identifier{
			Type:          hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
			Value:         model.ComponentID,
			Scheme:        "archivesspace-component-id",
			NamespaceUri:  resourceNamespace,
			IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
		})
	}
	for _, external := range model.ExternalIDs {
		pending = append(pending, &hubv1.Identifier{
			Type:          hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
			Value:         strings.TrimSpace(external.ExternalID),
			Scheme:        "archivesspace-external-id",
			NamespaceUri:  externalIDNamespace(installationNamespace, external.Source),
			IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
		})
	}
	arks := []string{model.ExternalARKURL, model.ImportCurrentARK, model.ARKName.Current}
	arks = append(arks, model.ImportPreviousARKs...)
	arks = append(arks, model.ARKName.Previous...)
	for _, ark := range arks {
		if identifier := urlIdentifier(ark, "ark", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD); identifier != nil {
			pending = append(pending, identifier)
		}
	}
	overflow := make([]sourceIdentifierExtra, 0)
	for _, identifier := range pending {
		if identifier == nil || strings.TrimSpace(identifier.Value) == "" {
			continue
		}
		if identifier.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL && strings.TrimSpace(identifier.NamespaceUri) == "" {
			overflow = append(overflow, sourceIdentifierExtra{
				Scheme: identifier.Scheme, Value: strings.TrimSpace(identifier.Value),
				IdentityLevel: "source_record",
			})
			continue
		}
		appendIdentifier(record, identifier)
	}
	return overflow
}

func localIdentifier(scheme, value, namespace string) *hubv1.Identifier {
	return &hubv1.Identifier{
		Type:          hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
		Value:         strings.TrimSpace(value),
		Scheme:        scheme,
		NamespaceUri:  namespace,
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
	}
}

func appendIdentifier(record *hubv1.Record, identifier *hubv1.Identifier) {
	if record == nil || identifier == nil {
		return
	}
	identifier.Value = strings.TrimSpace(identifier.Value)
	if identifier.Value == "" {
		return
	}
	identifier.Scheme = machineName(identifier.Scheme)
	for _, existing := range record.Identifiers {
		if existing.GetScheme() == identifier.GetScheme() &&
			existing.GetNamespaceUri() == identifier.GetNamespaceUri() &&
			existing.GetValue() == identifier.GetValue() {
			return
		}
	}
	record.Identifiers = append(record.Identifiers, identifier)
}

func urlIdentifier(value, scheme string, level hubv1.IdentifierIdentityLevel) *hubv1.Identifier {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	sanitized, err := safeSourceURI(value, "")
	if err != nil {
		return nil
	}
	value = sanitized
	identifierType := hubv1.IdentifierType_IDENTIFIER_TYPE_URL
	if strings.Contains(strings.ToLower(value), "ark:/") {
		identifierType = hubv1.IdentifierType_IDENTIFIER_TYPE_PID
	}
	return &hubv1.Identifier{
		Type:          identifierType,
		Value:         value,
		Scheme:        scheme,
		IdentityLevel: level,
	}
}

func appendDates(record *hubv1.Record, model *jsonModel) {
	for _, source := range model.Dates {
		date := archivalDate(source)
		if date != nil {
			record.Dates = append(record.Dates, date)
		}
	}
}

func archivalDate(source dateValue) *hubv1.DateValue {
	raw := firstNonempty(source.Expression, joinedDateRange(source.Begin, source.End))
	if raw == "" {
		return nil
	}
	date := &hubv1.DateValue{
		Type: dateType(source.Label),
		Raw:  raw,
	}
	switch strings.ToLower(strings.TrimSpace(source.Certainty)) {
	case "approximate", "circa", "inferred":
		date.Qualifier = hubv1.DateQualifier_DATE_QUALIFIER_APPROXIMATE
	case "questionable", "uncertain":
		date.Qualifier = hubv1.DateQualifier_DATE_QUALIFIER_UNCERTAIN
	}
	beginYear, beginMonth, beginDay, beginPrecision := parsePartialDate(source.Begin)
	date.Year = beginYear
	date.Month = beginMonth
	date.Day = beginDay
	date.Precision = beginPrecision
	endYear, endMonth, endDay, _ := parsePartialDate(source.End)
	if endYear != 0 {
		date.IsRange = true
		date.EndYear = endYear
		date.EndMonth = endMonth
		date.EndDay = endDay
	}
	return date
}

func dateType(label string) hubv1.DateType {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "creation":
		return hubv1.DateType_DATE_TYPE_CREATED
	case "publication", "broadcast":
		return hubv1.DateType_DATE_TYPE_PUBLISHED
	case "copyright":
		return hubv1.DateType_DATE_TYPE_COPYRIGHT
	case "digitized":
		return hubv1.DateType_DATE_TYPE_CAPTURED
	case "usage":
		return hubv1.DateType_DATE_TYPE_VALID
	default:
		return hubv1.DateType_DATE_TYPE_OTHER
	}
}

func parsePartialDate(value string) (int32, int32, int32, hubv1.DatePrecision) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0, 0, hubv1.DatePrecision_DATE_PRECISION_UNSPECIFIED
	}
	parts := strings.Split(value, "-")
	if len(parts) == 0 || len(parts[0]) != 4 {
		return 0, 0, 0, hubv1.DatePrecision_DATE_PRECISION_UNSPECIFIED
	}
	year, ok := parseInt32(parts[0])
	if !ok {
		return 0, 0, 0, hubv1.DatePrecision_DATE_PRECISION_UNSPECIFIED
	}
	precision := hubv1.DatePrecision_DATE_PRECISION_YEAR
	var month, day int32
	if len(parts) >= 2 {
		month, ok = parseInt32(parts[1])
		if !ok || month < 1 || month > 12 {
			return year, 0, 0, precision
		}
		precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
	}
	if len(parts) == 3 {
		day, ok = parseInt32(parts[2])
		if !ok || day < 1 || day > 31 {
			return year, month, 0, precision
		}
		precision = hubv1.DatePrecision_DATE_PRECISION_DAY
	}
	return year, month, day, precision
}

func parseInt32(value string) (int32, bool) {
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(parsed), true // #nosec G115 -- ParseInt with bitSize 32 guarantees the conversion is bounded.
}

func joinedDateRange(begin, end string) string {
	begin = strings.TrimSpace(begin)
	end = strings.TrimSpace(end)
	if begin == "" {
		return end
	}
	if end == "" || end == begin {
		return begin
	}
	return begin + "/" + end
}

func appendAgents(record *hubv1.Record, model *jsonModel, options *format.ParseOptions) {
	for _, linked := range model.LinkedAgents {
		contributor := agentContributor(linked, options)
		if contributor != nil {
			record.Contributors = append(record.Contributors, contributor)
		}
	}
}

func agentContributor(linked linkedAgent, options *format.ParseOptions) *hubv1.Contributor {
	var resolved resolvedAgent
	hasResolved := decodeResolved(linked.Resolved, &resolved)
	name := cleanText(linked.Title, options)
	var parsed *hubv1.ParsedName
	contributorType := hubv1.ContributorType_CONTRIBUTOR_TYPE_UNSPECIFIED
	if hasResolved {
		agentName := firstAgentName(resolved)
		name, parsed, contributorType = displayAgentName(resolved.JSONModelType, agentName, resolved.Title, options)
	}
	if name == "" {
		return nil
	}
	role := firstNonempty(linked.Role, linked.Relator, "contributor")
	roleCode := strings.TrimSpace(linked.Relator)
	if roleCode != "" && !strings.Contains(roleCode, ":") {
		roleCode = "relators:" + roleCode
	}
	contributor := &hubv1.Contributor{
		Name:       name,
		ParsedName: parsed,
		Role:       role,
		RoleCode:   roleCode,
		Type:       contributorType,
		SourceId:   firstNonempty(resolved.URI, linked.Ref),
	}
	localAgentURI := firstNonempty(resolved.URI, linked.Ref)
	if localAgentURI != "" {
		contributor.Identifiers = append(contributor.Identifiers, &hubv1.Identifier{
			Type:          hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
			Value:         localAgentURI,
			Scheme:        "archivesspace-agent-uri",
			NamespaceUri:  normalizedNamespace(optionBaseURL(options)),
			IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
		})
	}
	if hasResolved {
		for _, external := range resolved.ExternalIDs {
			appendAgentIdentifier(contributor, external.Source, external.ExternalID)
		}
		for _, identifier := range resolved.AgentIdentifiers {
			appendAgentIdentifier(contributor, identifier.IdentifierType, identifier.EntityIdentifier)
		}
	}
	return contributor
}

func firstAgentName(agent resolvedAgent) agentName {
	if len(bytes.TrimSpace(agent.DisplayName)) > 0 && !bytes.Equal(bytes.TrimSpace(agent.DisplayName), []byte("null")) {
		var name agentName
		if json.Unmarshal(agent.DisplayName, &name) == nil {
			return name
		}
	}
	if len(agent.Names) > 0 {
		return agent.Names[0]
	}
	return agentName{}
}

func displayAgentName(modelType string, value agentName, fallback string, options *format.ParseOptions) (string, *hubv1.ParsedName, hubv1.ContributorType) {
	switch modelType {
	case "agent_person":
		parsed := &hubv1.ParsedName{
			Family: strings.TrimSpace(value.PrimaryName),
			Given:  strings.TrimSpace(value.RestOfName),
			Prefix: strings.TrimSpace(value.Prefix),
			Suffix: strings.TrimSpace(value.Suffix),
			Middle: strings.TrimSpace(value.FullerForm),
		}
		name := strings.TrimSpace(value.PrimaryName)
		if strings.EqualFold(value.NameOrder, "direct") {
			name = strings.TrimSpace(strings.Join(compactStrings([]string{value.Prefix, value.RestOfName, value.PrimaryName, value.Suffix}), " "))
		} else if value.RestOfName != "" {
			name += ", " + strings.TrimSpace(value.RestOfName)
			if value.Suffix != "" {
				name += ", " + strings.TrimSpace(value.Suffix)
			}
		}
		name = firstNonempty(name, fallback)
		parsed.FullName = name
		parsed.Normalized = name
		return cleanText(name, options), parsed, hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON
	case "agent_corporate_entity":
		name := strings.Join(compactStrings([]string{value.PrimaryName, value.SubordinateName1, value.SubordinateName2}), ". ")
		return cleanText(firstNonempty(name, fallback), options), nil, hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
	case "agent_family":
		return cleanText(firstNonempty(value.FamilyName, value.PrimaryName, fallback), options), nil, hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON
	case "agent_software":
		return cleanText(firstNonempty(value.SoftwareName, value.PrimaryName, fallback), options), nil, hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
	default:
		return cleanText(firstNonempty(value.PrimaryName, fallback), options), nil, hubv1.ContributorType_CONTRIBUTOR_TYPE_UNSPECIFIED
	}
}

func appendAgentIdentifier(contributor *hubv1.Contributor, source, value string) {
	value = strings.TrimSpace(value)
	if contributor == nil || value == "" {
		return
	}
	scheme := machineName(source)
	if scheme == "naf" {
		scheme = "lcnaf"
	}
	namespace := agentAuthorityNamespace(scheme)
	identifierType := hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
	switch scheme {
	case "gnd", "lcnaf", "viaf":
		identifierType = hubv1.IdentifierType_IDENTIFIER_TYPE_PID
	case "isni":
		identifierType = hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI
	case "orcid":
		identifierType = hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID
	}
	if namespace != "" && strings.HasPrefix(value, namespace) {
		value = strings.TrimPrefix(value, namespace)
	} else if isAbsoluteHTTPURL(value) {
		namespace = ""
		identifierType = hubv1.IdentifierType_IDENTIFIER_TYPE_URL
		scheme = "url"
	} else if namespace == "" {
		scheme = "archivesspace-agent-external-id"
		namespace = agentExternalIDNamespace(source)
	}
	identifier := &hubv1.Identifier{
		Type:          identifierType,
		Value:         value,
		Scheme:        scheme,
		NamespaceUri:  namespace,
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT,
	}
	contributor.Identifiers = append(contributor.Identifiers, identifier)
	if contributor.AuthorityUri == "" {
		if isAbsoluteHTTPURL(value) {
			contributor.AuthorityUri = value
		} else if namespace != "" {
			contributor.AuthorityUri = namespace + value
		}
		if contributor.AuthorityUri != "" {
			contributor.AuthoritySource = scheme
		}
	}
}

func agentAuthorityNamespace(source string) string {
	switch machineName(source) {
	case "lcnaf", "naf":
		return "https://id.loc.gov/authorities/names/"
	case "orcid":
		return "https://orcid.org/"
	case "isni":
		return "https://isni.org/isni/"
	case "viaf":
		return "https://viaf.org/viaf/"
	case "gnd":
		return "https://d-nb.info/gnd/"
	default:
		return ""
	}
}

func agentExternalIDNamespace(source string) string {
	source = machineName(source)
	if source == "" {
		source = "unspecified"
	}
	return "https://www.archivesspace.org/agent-identifier-authorities/" + source + "/"
}

func appendSubjects(record *hubv1.Record, model *jsonModel, options *format.ParseOptions) {
	for _, reference := range model.Subjects {
		var resolved resolvedSubject
		decodeResolved(reference.Resolved, &resolved)
		label := firstNonempty(resolved.Title, reference.Title, reference.DisplayString)
		if label == "" && len(resolved.Terms) > 0 {
			parts := make([]string, 0, len(resolved.Terms))
			for _, term := range resolved.Terms {
				if term.Term = strings.TrimSpace(term.Term); term.Term != "" {
					parts = append(parts, term.Term)
				}
			}
			label = strings.Join(parts, " -- ")
		}
		label = cleanText(label, options)
		if label == "" {
			continue
		}
		termType := ""
		if len(resolved.Terms) > 0 {
			termType = resolved.Terms[0].TermType
		}
		record.Subjects = append(record.Subjects, &hubv1.Subject{
			Value:      label,
			Vocabulary: subjectVocabulary(resolved.Source),
			Uri:        subjectAuthorityURI(resolved.Source, resolved.AuthorityID),
			SourceId:   firstNonempty(resolved.URI, reference.Ref),
			Type:       subjectType(termType),
		})
	}
}

func subjectVocabulary(value string) hubv1.SubjectVocabulary {
	switch machineName(value) {
	case "lcsh":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH
	case "mesh":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH
	case "aat":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT
	case "fast":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_FAST
	case "lcnaf":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCNAF
	case "local", "ingest":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL
	default:
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
	}
}

func subjectType(value string) hubv1.SubjectType {
	switch machineName(value) {
	case "geographic":
		return hubv1.SubjectType_SUBJECT_TYPE_GEOGRAPHIC
	case "temporal":
		return hubv1.SubjectType_SUBJECT_TYPE_TEMPORAL
	case "genre-form":
		return hubv1.SubjectType_SUBJECT_TYPE_GENRE
	case "cultural-context", "function", "occupation", "style-period", "technique", "topical":
		return hubv1.SubjectType_SUBJECT_TYPE_TOPIC
	case "uniform-title":
		return hubv1.SubjectType_SUBJECT_TYPE_TITLE
	default:
		return hubv1.SubjectType_SUBJECT_TYPE_UNSPECIFIED
	}
}

func appendNotes(record *hubv1.Record, model *jsonModel, options *format.ParseOptions) {
	for _, source := range model.Notes {
		values := noteText(source, options)
		if len(values) == 0 {
			continue
		}
		combined := strings.Join(values, "\n\n")
		switch machineName(source.Type) {
		case "abstract":
			if record.Abstract == "" {
				record.Abstract = combined
			} else {
				record.Notes = append(record.Notes, combined)
			}
		case "scopecontent":
			if record.Description == "" {
				record.Description = combined
			} else {
				record.Notes = append(record.Notes, combined)
			}
		case "physdesc":
			if record.PhysicalDesc == "" {
				record.PhysicalDesc = combined
			} else {
				record.Notes = append(record.Notes, combined)
			}
		case "prefercite":
			record.PreferredCitation = combined
		case "accessrestrict":
			record.AccessCondition = combined
		case "userestrict":
			record.LocalRestriction = combined
		default:
			label := cleanText(firstNonempty(source.Label, source.Type), options)
			if label != "" {
				combined = label + ": " + combined
			}
			record.Notes = append(record.Notes, combined)
		}
	}
}

func noteText(source note, options *format.ParseOptions) []string {
	values := rawTextValues(source.Content, options)
	if title := cleanText(source.Title, options); title != "" {
		values = append([]string{title}, values...)
	}
	for _, subnote := range source.Subnotes {
		values = append(values, noteText(subnote, options)...)
	}
	values = append(values, noteItemText(source.Items, options)...)
	return compactStrings(values)
}

func noteItemText(raw json.RawMessage, options *format.ParseOptions) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var stringsOnly []string
	if json.Unmarshal(raw, &stringsOnly) == nil {
		for index := range stringsOnly {
			stringsOnly[index] = cleanText(stringsOnly[index], options)
		}
		return compactStrings(stringsOnly)
	}
	var objects []struct {
		Label         string   `json:"label"`
		Value         string   `json:"value"`
		EventDate     string   `json:"event_date"`
		Place         string   `json:"place"`
		Events        []string `json:"events"`
		Reference     string   `json:"reference"`
		ReferenceText string   `json:"reference_text"`
	}
	if json.Unmarshal(raw, &objects) != nil {
		return nil
	}
	values := make([]string, 0, len(objects))
	for _, object := range objects {
		parts := []string{object.Label, object.Value, object.EventDate, object.Place}
		parts = append(parts, object.Events...)
		parts = append(parts, object.ReferenceText, object.Reference)
		for index := range parts {
			parts[index] = cleanText(parts[index], options)
		}
		if value := strings.Join(compactStrings(parts), ": "); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func rawTextValues(raw json.RawMessage, options *format.ParseOptions) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return compactStrings([]string{cleanText(single, options)})
	}
	var multiple []string
	if json.Unmarshal(raw, &multiple) == nil {
		for index := range multiple {
			multiple[index] = cleanText(multiple[index], options)
		}
		return compactStrings(multiple)
	}
	return nil
}

func appendExtents(record *hubv1.Record, model *jsonModel, options *format.ParseOptions) {
	descriptions := make([]string, 0, len(model.Extents))
	for _, extent := range model.Extents {
		primary := strings.TrimSpace(strings.Join(compactStrings([]string{extent.Number, extent.ExtentType}), " "))
		description := strings.Join(compactStrings([]string{primary, extent.ContainerSummary, extent.PhysicalDetails}), "; ")
		if description = cleanText(description, options); description != "" {
			descriptions = append(descriptions, description)
		}
		if record.Dimensions == "" {
			record.Dimensions = cleanText(extent.Dimensions, options)
		}
	}
	if len(descriptions) > 0 {
		if record.PhysicalDesc == "" {
			record.PhysicalDesc = strings.Join(descriptions, "; ")
		} else {
			record.PhysicalDesc += "; " + strings.Join(descriptions, "; ")
		}
	}
}

func appendRights(record *hubv1.Record, model *jsonModel, options *format.ParseOptions) {
	for _, source := range model.RightsStatements {
		statement := strings.Join(compactStrings([]string{
			source.RightsType,
			source.Status,
			source.LicenseTerms,
			source.StatuteCitation,
			source.Jurisdiction,
			source.OtherRightsBasis,
		}), "; ")
		for _, note := range source.Notes {
			statement = strings.Join(compactStrings([]string{statement, strings.Join(noteText(note, options), " ")}), "; ")
		}
		uri := ""
		for _, document := range source.ExternalDocuments {
			if isAbsoluteHTTPURL(document.Location) {
				uri = sourceRecordURI(document.Location, options)
				break
			}
		}
		holders := make([]string, 0, len(source.LinkedAgents))
		for _, linked := range source.LinkedAgents {
			if holder := agentContributor(linked, options); holder != nil {
				holders = append(holders, holder.Name)
			}
		}
		record.Rights = append(record.Rights, &hubv1.Rights{
			Statement: cleanText(statement, options),
			Uri:       uri,
			Holder:    strings.Join(compactStrings(holders), "; "),
		})
	}
	if booleanValue(model.RestrictionsApply) || booleanValue(model.Restrictions) {
		record.IsPublic = false
		if record.AccessCondition == "" {
			record.AccessCondition = "Restrictions apply"
		}
	} else if model.Publish != nil {
		record.IsPublic = *model.Publish && !booleanValue(model.Suppressed)
	}
}

func appendRelations(record *hubv1.Record, model *jsonModel, options *format.ParseOptions) {
	if parent := parentURI(model); parent != "" {
		record.Relations = append(record.Relations, &hubv1.Relation{
			Type:               hubv1.RelationType_RELATION_TYPE_PART_OF,
			TargetTitle:        resolvedReferenceTitle(model.Parent, options),
			TargetId:           parent,
			TargetIdType:       hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
			TargetUri:          sourceRecordURI(parent, options),
			TargetResourceType: parentResourceType(model),
		})
	}
	for _, document := range model.ExternalDocuments {
		location := sourceRecordURI(document.Location, options)
		if location == "" {
			continue
		}
		record.Relations = append(record.Relations, &hubv1.Relation{
			Type:         hubv1.RelationType_RELATION_TYPE_RELATED_TO,
			TargetTitle:  cleanText(document.Title, options),
			TargetId:     location,
			TargetIdType: hubv1.IdentifierType_IDENTIFIER_TYPE_URL,
			TargetUri:    location,
		})
	}
	if model.EADLocation != "" {
		location := sourceRecordURI(model.EADLocation, options)
		record.Relations = append(record.Relations, &hubv1.Relation{
			Type:         hubv1.RelationType_RELATION_TYPE_IS_DESCRIBED_BY,
			TargetTitle:  cleanText(model.FindingAidTitle, options),
			TargetId:     location,
			TargetIdType: hubv1.IdentifierType_IDENTIFIER_TYPE_URL,
			TargetUri:    location,
		})
	}
}

func parentResourceType(model *jsonModel) hubv1.ResourceTypeValue {
	if strings.TrimSpace(model.Parent.Ref) == "" {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION
	}
	return hubv1.ResourceTypeValue_RESOURCE_TYPE_ARCHIVAL_MATERIAL
}

func resolvedReferenceTitle(reference reference, options *format.ParseOptions) string {
	var resolved struct {
		Title         string `json:"title"`
		DisplayString string `json:"display_string"`
	}
	decodeResolved(reference.Resolved, &resolved)
	return cleanText(firstNonempty(resolved.Title, resolved.DisplayString, reference.Title, reference.DisplayString), options)
}

func appendFiles(record *hubv1.Record, model *jsonModel, options *format.ParseOptions) {
	if model.RepresentativeFileVersion != nil {
		appendFileVersion(record, *model.RepresentativeFileVersion, "representative", options)
	}
	for _, source := range model.Instances {
		if source.DigitalObject == nil {
			continue
		}
		var digital resolvedDigitalObject
		decodeResolved(source.DigitalObject.Resolved, &digital)
		targetURI := sourceRecordURI(firstNonempty(digital.URI, source.DigitalObject.Ref), options)
		if targetURI != "" {
			record.Relations = append(record.Relations, &hubv1.Relation{
				Type:               hubv1.RelationType_RELATION_TYPE_HAS_FORMAT,
				TargetTitle:        cleanText(digital.Title, options),
				TargetId:           firstNonempty(digital.URI, source.DigitalObject.Ref),
				TargetIdType:       hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
				TargetUri:          targetURI,
				TargetResourceType: hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER,
			})
		}
		for _, file := range digital.FileVersions {
			appendFileVersion(record, file, source.InstanceType, options)
		}
	}
}

func appendFileVersion(record *hubv1.Record, source fileVersion, fallbackRole string, options *format.ParseOptions) {
	uri := sourceRecordURI(source.FileURI, options)
	if uri == "" {
		return
	}
	name := firstNonempty(source.FileName, source.Identifier)
	if name == "" {
		if parsed, err := url.Parse(uri); err == nil {
			name = path.Base(parsed.Path)
		}
	}
	mimeType := strings.TrimSpace(source.FileFormatName)
	if !strings.Contains(mimeType, "/") {
		if inferred := mime.TypeByExtension(path.Ext(name)); inferred != "" {
			mimeType = inferred
		}
	}
	role := firstNonempty(source.UseStatement, fallbackRole)
	appendFile(record, &hubv1.File{
		Name:              name,
		MimeType:          mimeType,
		SizeBytes:         source.FileSizeBytes,
		Description:       cleanText(source.Caption, options),
		Role:              role,
		Uri:               uri,
		AccessUrl:         firstNonempty(sourceRecordURI(source.LinkURI, options), uri),
		Checksum:          strings.TrimSpace(source.Checksum),
		ChecksumAlgorithm: strings.TrimSpace(source.ChecksumMethod),
	})
}

func appendFile(record *hubv1.Record, file *hubv1.File) {
	for _, existing := range record.Files {
		if existing.GetUri() == file.GetUri() && existing.GetChecksum() == file.GetChecksum() {
			return
		}
	}
	record.Files = append(record.Files, file)
}

func appendArchivalContainers(record *hubv1.Record, model *jsonModel) {
	location := &hubv1.ArchivalLocation{}
	applyArchivalContainers(location, model.Instances)
	if location.Box != "" || location.Folder != "" {
		record.ArchivalLocation = location
	}
}

func appendLanguages(record *hubv1.Record, model *jsonModel, options *format.ParseOptions) {
	languages := make([]string, 0)
	for _, material := range model.LanguageMaterials {
		if language := strings.TrimSpace(material.LanguageAndScript.Language); language != "" {
			languages = append(languages, language)
		}
		for _, note := range material.Notes {
			languages = append(languages, noteText(note, options)...)
		}
	}
	if record.Language == "" {
		record.Language = strings.Join(compactStrings(languages), "; ")
	}
}

func appendExtras(record *hubv1.Record, decoded *decodedRecord, options *format.ParseOptions, sourceIdentifiers []sourceIdentifierExtra) error {
	if decoded == nil {
		return fmt.Errorf("ArchivesSpace decoded record is nil")
	}
	model := &decoded.model
	extra := make(map[string]any)
	put := func(key string, value any, present bool) {
		if present {
			extra[key] = value
		}
	}
	put("archivesspace_jsonmodel_type", model.JSONModelType, model.JSONModelType != "")
	put("archivesspace_record_uri", model.URI, model.URI != "")
	put("archivesspace_level", model.Level, model.Level != "")
	put("archivesspace_other_level", model.OtherLevel, model.OtherLevel != "")
	put("archivesspace_resource_type", model.ResourceType, model.ResourceType != "")
	put("archivesspace_ref_id", model.RefID, model.RefID != "")
	put("archivesspace_component_id", model.ComponentID, model.ComponentID != "")
	put("archivesspace_ead_id", model.EADID, model.EADID != "")
	put("archivesspace_finding_aid_status", model.FindingAidStatus, model.FindingAidStatus != "")
	put("archivesspace_finding_aid_description_rules", model.FindingAidDescriptionRules, model.FindingAidDescriptionRules != "")
	put("archivesspace_publish", booleanValue(model.Publish), model.Publish != nil)
	put("archivesspace_suppressed", booleanValue(model.Suppressed), model.Suppressed != nil)
	put("archivesspace_restrictions_apply", booleanValue(model.RestrictionsApply), model.RestrictionsApply != nil)
	put("archivesspace_position", intValue(model.Position), model.Position != nil)
	if ids := compactMap(map[string]any{
		"id_0": strings.TrimSpace(model.ID0),
		"id_1": strings.TrimSpace(model.ID1),
		"id_2": strings.TrimSpace(model.ID2),
		"id_3": strings.TrimSpace(model.ID3),
	}); len(ids) > 0 {
		extra["archivesspace_id_components"] = ids
	}
	if extents := extentExtras(model.Extents, options); len(extents) > 0 {
		extra["archivesspace_extents"] = extents
	}
	if containers := containerExtras(model.Instances, options); len(containers) > 0 {
		extra["archivesspace_containers"] = containers
	}
	if dateMetadata := dateExtras(model.Dates); len(dateMetadata) > 0 {
		extra["archivesspace_date_metadata"] = dateMetadata
	}
	if findingAid := findingAidExtras(model, options); len(findingAid) > 0 {
		extra["archivesspace_finding_aid"] = findingAid
	}
	rights, err := rightsExtras(model.RightsStatements, options)
	if err != nil {
		return err
	}
	if len(rights) > 0 {
		extra["archivesspace_rights"] = rights
	}
	if files := fileVersionExtras(model, options); len(files) > 0 {
		extra["archivesspace_file_versions"] = files
	}
	if refs := linkedReferenceExtras(model); len(refs) > 0 {
		extra["archivesspace_linked_references"] = refs
	}
	if len(sourceIdentifiers) > 0 {
		values := make([]any, 0, len(sourceIdentifiers))
		for _, identifier := range sourceIdentifiers {
			values = append(values, compactMap(map[string]any{
				"scheme": identifier.Scheme, "value": identifier.Value,
				"namespace_uri": identifier.NamespaceURI, "identity_level": identifier.IdentityLevel,
			}))
		}
		extra["archivesspace_source_identifiers"] = values
	}
	rawFields, err := preservedJSONModelFields(decoded.raw)
	if err != nil {
		return err
	}
	if len(rawFields) > 0 {
		extra["archivesspace_raw_fields"] = rawFields
	}
	if len(extra) == 0 {
		return nil
	}
	structured, err := structpb.NewStruct(extra)
	if err != nil {
		return fmt.Errorf("encode ArchivesSpace extras: %w", err)
	}
	record.Extra = structured
	record.SourceInfo.UnmappedFields = make([]string, 0, len(extra))
	for key := range extra {
		record.SourceInfo.UnmappedFields = append(record.SourceInfo.UnmappedFields, key)
	}
	sort.Strings(record.SourceInfo.UnmappedFields)
	return nil
}

func preservedJSONModelFields(raw json.RawMessage) (map[string]any, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("decode ArchivesSpace raw fields: %w", err)
	}
	result := make(map[string]any)
	for name, value := range fields {
		if _, projected := projectedJSONModelFields[name]; projected {
			continue
		}
		canonical, err := canonicalJSONText(value)
		if err != nil {
			return nil, fmt.Errorf("canonicalize ArchivesSpace raw field %q: %w", name, err)
		}
		result[name] = canonical
	}
	return result, nil
}

// canonicalJSONText retains the source JSON type and numeric spelling while
// making object member order and insignificant whitespace deterministic. Raw
// values are stored as JSON text because protobuf Struct represents every
// number as a float64 and would otherwise corrupt large integer identifiers.
func canonicalJSONText(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

// projectedJSONModelFields lists top-level values that the adapter maps to a
// Hub field or a lossless ArchivesSpace extra. All other fields are retained
// in archivesspace_raw_fields so a newer JSONModel or instance plugin cannot
// silently discard data.
var projectedJSONModelFields = map[string]struct{}{
	"jsonmodel_type": {}, "uri": {}, "title": {}, "display_string": {}, "publish": {}, "suppressed": {},
	"external_ids": {}, "subjects": {}, "extents": {}, "lang_materials": {}, "dates": {},
	"external_documents": {}, "rights_statements": {}, "linked_agents": {}, "notes": {}, "instances": {},
	"representative_file_version": {}, "id_0": {}, "id_1": {}, "id_2": {}, "id_3": {}, "ead_id": {},
	"ead_location": {}, "external_ark_url": {}, "import_current_ark": {}, "import_previous_arks": {}, "ark_name": {},
	"resource_type": {}, "finding_aid_title": {}, "finding_aid_subtitle": {}, "finding_aid_author": {},
	"finding_aid_date": {}, "finding_aid_language": {}, "finding_aid_script": {}, "finding_aid_language_note": {},
	"finding_aid_description_rules": {}, "finding_aid_edition_statement": {}, "finding_aid_series_statement": {},
	"finding_aid_status": {}, "finding_aid_note": {}, "repository_processing_note": {}, "ref_id": {},
	"component_id": {}, "level": {}, "other_level": {}, "position": {}, "restrictions_apply": {}, "restrictions": {},
	"parent": {}, "resource": {}, "ancestors": {},
}

func extentExtras(values []extent, options *format.ParseOptions) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		entry := compactMap(map[string]any{
			"portion":           strings.TrimSpace(value.Portion),
			"number":            strings.TrimSpace(value.Number),
			"extent_type":       strings.TrimSpace(value.ExtentType),
			"container_summary": cleanText(value.ContainerSummary, options),
			"physical_details":  cleanText(value.PhysicalDetails, options),
			"dimensions":        cleanText(value.Dimensions, options),
		})
		if len(entry) > 0 {
			result = append(result, entry)
		}
	}
	return result
}

func containerExtras(values []instance, options *format.ParseOptions) []any {
	result := make([]any, 0)
	for _, value := range values {
		if value.SubContainer == nil {
			continue
		}
		var top topContainer
		decodeResolved(value.SubContainer.TopContainer.Resolved, &top)
		entry := compactMap(map[string]any{
			"instance_type": strings.TrimSpace(value.InstanceType),
			"display":       cleanText(value.SubContainer.DisplayString, options),
			"top_uri":       firstNonempty(top.URI, value.SubContainer.TopContainer.Ref),
			"top_type":      strings.TrimSpace(top.Type),
			"top_indicator": strings.TrimSpace(top.Indicator),
			"top_barcode":   strings.TrimSpace(top.Barcode),
			"type_2":        strings.TrimSpace(value.SubContainer.Type2),
			"indicator_2":   strings.TrimSpace(value.SubContainer.Indicator2),
			"barcode_2":     strings.TrimSpace(value.SubContainer.Barcode2),
			"type_3":        strings.TrimSpace(value.SubContainer.Type3),
			"indicator_3":   strings.TrimSpace(value.SubContainer.Indicator3),
		})
		locations := make([]any, 0, len(top.ContainerLocations))
		for _, location := range top.ContainerLocations {
			locationEntry := compactMap(map[string]any{
				"ref":        strings.TrimSpace(location.Ref),
				"status":     strings.TrimSpace(location.Status),
				"start_date": strings.TrimSpace(location.StartDate),
				"end_date":   strings.TrimSpace(location.EndDate),
				"note":       cleanText(location.Note, options),
			})
			if len(locationEntry) > 0 {
				locations = append(locations, locationEntry)
			}
		}
		if len(locations) > 0 {
			entry["locations"] = locations
		}
		if len(entry) > 0 {
			result = append(result, entry)
		}
	}
	return result
}

func linkedReferenceExtras(model *jsonModel) []any {
	result := make([]any, 0)
	for _, agent := range model.LinkedAgents {
		if len(bytes.TrimSpace(agent.Resolved)) > 0 && !bytes.Equal(bytes.TrimSpace(agent.Resolved), []byte("null")) {
			continue
		}
		entry := compactMap(map[string]any{
			"kind":    "agent",
			"ref":     strings.TrimSpace(agent.Ref),
			"role":    strings.TrimSpace(agent.Role),
			"relator": strings.TrimSpace(agent.Relator),
		})
		if len(entry) > 1 {
			result = append(result, entry)
		}
	}
	for _, subject := range model.Subjects {
		if len(bytes.TrimSpace(subject.Resolved)) > 0 && !bytes.Equal(bytes.TrimSpace(subject.Resolved), []byte("null")) {
			continue
		}
		if ref := strings.TrimSpace(subject.Ref); ref != "" {
			result = append(result, map[string]any{"kind": "subject", "ref": ref})
		}
	}
	return result
}

func dateExtras(values []dateValue) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		entry := compactMap(map[string]any{
			"date_type": strings.TrimSpace(value.DateType),
			"label":     strings.TrimSpace(value.Label),
			"certainty": strings.TrimSpace(value.Certainty),
			"era":       strings.TrimSpace(value.Era),
			"calendar":  strings.TrimSpace(value.Calendar),
		})
		if len(entry) > 0 {
			result = append(result, entry)
		}
	}
	return result
}

func findingAidExtras(model *jsonModel, options *format.ParseOptions) map[string]any {
	if model == nil || model.JSONModelType != "resource" {
		return nil
	}
	return compactMap(map[string]any{
		"title":                      cleanText(model.FindingAidTitle, options),
		"subtitle":                   cleanText(model.FindingAidSubtitle, options),
		"author":                     cleanText(model.FindingAidAuthor, options),
		"date":                       strings.TrimSpace(model.FindingAidDate),
		"language":                   strings.TrimSpace(model.FindingAidLanguage),
		"script":                     strings.TrimSpace(model.FindingAidScript),
		"language_note":              cleanText(model.FindingAidLanguageNote, options),
		"description_rules":          strings.TrimSpace(model.FindingAidDescriptionRules),
		"edition_statement":          cleanText(model.FindingAidEditionStatement, options),
		"series_statement":           cleanText(model.FindingAidSeriesStatement, options),
		"status":                     strings.TrimSpace(model.FindingAidStatus),
		"note":                       cleanText(model.FindingAidNote, options),
		"repository_processing_note": cleanText(model.RepositoryProcessingNote, options),
	})
}

func rightsExtras(values []rightsStatement, options *format.ParseOptions) ([]any, error) {
	result := make([]any, 0, len(values))
	for index, value := range values {
		documents := make([]any, 0, len(value.ExternalDocuments))
		for _, document := range value.ExternalDocuments {
			entry := compactMap(map[string]any{
				"title":    cleanText(document.Title, options),
				"location": sourceRecordURI(document.Location, options),
			})
			if len(entry) > 0 {
				documents = append(documents, entry)
			}
		}
		entry := compactMap(map[string]any{
			"rights_type":        strings.TrimSpace(value.RightsType),
			"identifier":         strings.TrimSpace(value.Identifier),
			"status":             strings.TrimSpace(value.Status),
			"determination_date": strings.TrimSpace(value.DeterminationDate),
			"start_date":         strings.TrimSpace(value.StartDate),
			"end_date":           strings.TrimSpace(value.EndDate),
			"license_terms":      cleanText(value.LicenseTerms, options),
			"statute_citation":   cleanText(value.StatuteCitation, options),
			"jurisdiction":       strings.TrimSpace(value.Jurisdiction),
			"other_rights_basis": strings.TrimSpace(value.OtherRightsBasis),
		})
		if len(documents) > 0 {
			entry["external_documents"] = documents
		}
		if raw := bytes.TrimSpace(value.Acts); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
			canonical, err := canonicalJSONText(raw)
			if err != nil {
				return nil, fmt.Errorf("canonicalize ArchivesSpace rights statement %d acts: %w", index+1, err)
			}
			entry["acts_json"] = canonical
			if acts := rightsActExtras(raw, options); len(acts) > 0 {
				entry["acts"] = acts
			}
		}
		if len(entry) > 0 {
			result = append(result, entry)
		}
	}
	return result, nil
}

func rightsActExtras(raw json.RawMessage, options *format.ParseOptions) []any {
	var values []rightsAct
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	result := make([]any, 0, len(values))
	for _, value := range values {
		notes := make([]string, 0)
		for _, source := range value.Notes {
			notes = append(notes, noteText(source, options)...)
		}
		entry := compactMap(map[string]any{
			"jsonmodel_type": strings.TrimSpace(value.JSONModelType),
			"act_type":       strings.TrimSpace(value.ActType),
			"restriction":    strings.TrimSpace(value.Restriction),
			"start_date":     strings.TrimSpace(value.StartDate),
			"end_date":       strings.TrimSpace(value.EndDate),
		})
		if notes = compactStrings(notes); len(notes) > 0 {
			items := make([]any, len(notes))
			for index, note := range notes {
				items[index] = note
			}
			entry["notes"] = items
		}
		if len(entry) > 0 {
			result = append(result, entry)
		}
	}
	return result
}

func fileVersionExtras(model *jsonModel, options *format.ParseOptions) []any {
	if model == nil {
		return nil
	}
	versions := make([]fileVersion, 0)
	if model.RepresentativeFileVersion != nil {
		versions = append(versions, *model.RepresentativeFileVersion)
	}
	for _, value := range model.Instances {
		if value.DigitalObject == nil {
			continue
		}
		var digital resolvedDigitalObject
		if decodeResolved(value.DigitalObject.Resolved, &digital) {
			versions = append(versions, digital.FileVersions...)
		}
	}
	result := make([]any, 0, len(versions))
	for _, value := range versions {
		entry := compactMap(map[string]any{
			"identifier":          strings.TrimSpace(value.Identifier),
			"file_uri":            sourceRecordURI(value.FileURI, options),
			"link_uri":            sourceRecordURI(value.LinkURI, options),
			"file_format_name":    strings.TrimSpace(value.FileFormatName),
			"file_format_version": strings.TrimSpace(value.FileFormatVersion),
			"derived_from":        sourceRecordURI(value.DerivedFrom, options),
		})
		if value.IsRepresentative {
			entry["is_representative"] = true
		}
		if len(entry) > 0 {
			result = append(result, entry)
		}
	}
	return result
}

func enrichHierarchyMetadata(dataset *format.Dataset, modelsByKey map[string]*jsonModel, options *format.ParseOptions) {
	if dataset == nil || len(dataset.Records) == 0 {
		return
	}
	recordsByKey := make(map[string]*hubv1.Record, len(dataset.Records))
	parentByKey := make(map[string]string, len(dataset.Hierarchy.Nodes))
	rootKey := ""
	for _, entry := range dataset.Records {
		recordsByKey[entry.Key] = entry.Record
	}
	for _, node := range dataset.Hierarchy.Nodes {
		parentByKey[node.RecordKey] = node.ParentKey
		if node.ParentKey == "" {
			rootKey = node.RecordKey
		}
	}
	rootTitle := ""
	if root := recordsByKey[rootKey]; root != nil {
		rootTitle = root.Title
	}
	for _, entry := range dataset.Records {
		record := entry.Record
		model := modelsByKey[entry.Key]
		if record == nil || model == nil {
			continue
		}
		if record.ArchivalLocation == nil {
			record.ArchivalLocation = &hubv1.ArchivalLocation{}
		}
		record.ArchivalLocation.Collection = rootTitle
		for ancestor := parentByKey[entry.Key]; ancestor != ""; ancestor = parentByKey[ancestor] {
			ancestorModel := modelsByKey[ancestor]
			if ancestorModel != nil && strings.Contains(strings.ToLower(ancestorModel.Level), "series") {
				record.ArchivalLocation.Series = recordsByKey[ancestor].Title
				break
			}
		}
		applyArchivalContainers(record.ArchivalLocation, model.Instances)
		for _, relation := range record.Relations {
			if relation.Type != hubv1.RelationType_RELATION_TYPE_PART_OF || relation.TargetTitle != "" {
				continue
			}
			if parent := recordsByKey[parentByKey[entry.Key]]; parent != nil {
				relation.TargetTitle = parent.Title
			}
		}
		if record.ArchivalLocation.Collection == "" && record.ArchivalLocation.Series == "" && record.ArchivalLocation.Box == "" && record.ArchivalLocation.Folder == "" {
			record.ArchivalLocation = nil
		}
	}
}

func applyArchivalContainers(location *hubv1.ArchivalLocation, instances []instance) {
	if location == nil {
		return
	}
	for _, value := range instances {
		if value.SubContainer == nil {
			continue
		}
		var top topContainer
		decodeResolved(value.SubContainer.TopContainer.Resolved, &top)
		assignContainer(location, top.Type, top.Indicator)
		assignContainer(location, value.SubContainer.Type2, value.SubContainer.Indicator2)
		assignContainer(location, value.SubContainer.Type3, value.SubContainer.Indicator3)
	}
}

func assignContainer(location *hubv1.ArchivalLocation, containerType, indicator string) {
	indicator = strings.TrimSpace(indicator)
	if indicator == "" {
		return
	}
	switch machineName(containerType) {
	case "box", "carton", "case", "container", "drawer", "reel", "volume":
		if location.Box == "" {
			location.Box = indicator
		}
	case "file", "folder":
		if location.Folder == "" {
			location.Folder = indicator
		}
	}
}

func decodeResolved(raw json.RawMessage, target any) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || raw[0] != '{' {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}

func compactMap(values map[string]any) map[string]any {
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			if typed == "" {
				delete(values, key)
			}
		case nil:
			delete(values, key)
		}
	}
	return values
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func cleanText(value string, options *format.ParseOptions) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if options == nil || options.StripHTML {
		return helpers.CleanTextPreserveNewlines(value)
	}
	return value
}

func sourceRecordURI(value string, options *format.ParseOptions) string {
	uri, err := safeSourceURI(value, optionBaseURL(options))
	if err != nil {
		return ""
	}
	return uri
}

func optionBaseURL(options *format.ParseOptions) string {
	if options == nil {
		return ""
	}
	return strings.TrimSpace(options.BaseURL)
}

func normalizedNamespace(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/"
	return parsed.String()
}

func namespaceForRecord(base, recordURI string) string {
	if normalizedNamespace(base) == "" {
		return ""
	}
	absolute := sourceRecordURI(recordURI, &format.ParseOptions{BaseURL: base})
	if parsed, err := url.Parse(absolute); err == nil && parsed.IsAbs() && parsed.Host != "" {
		return strings.TrimRight(absolute, "/") + "/"
	}
	return normalizedNamespace(base)
}

func externalIDNamespace(installationNamespace, source string) string {
	installationNamespace = strings.TrimSpace(installationNamespace)
	if installationNamespace == "" {
		return ""
	}
	source = machineName(source)
	if source == "" {
		source = "unspecified"
	}
	return strings.TrimRight(installationNamespace, "/") + "/identifier-authorities/" + source + "/"
}

func machineName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = machineNameSeparators.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func subjectAuthorityURI(source, value string) string {
	value = strings.TrimSpace(value)
	if isAbsoluteHTTPURL(value) {
		return value
	}
	var namespace string
	switch machineName(source) {
	case "lcsh":
		namespace = "https://id.loc.gov/authorities/subjects/"
	case "lcnaf":
		namespace = "https://id.loc.gov/authorities/names/"
	case "mesh":
		namespace = "https://id.nlm.nih.gov/mesh/"
	case "fast":
		namespace = "http://id.worldcat.org/fast/"
	}
	if namespace != "" && value != "" {
		return namespace + value
	}
	return ""
}

func isAbsoluteHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func booleanValue(value *bool) bool {
	return value != nil && *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
