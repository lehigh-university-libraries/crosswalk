package zenodo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/internal/provenanceuri"
)

const maxInputBytes = int64(64 << 20)

type searchResponse struct {
	Hits struct {
		Hits []record `json:"hits"`
	} `json:"hits"`
}

type record struct {
	ID           json.Number `json:"id"`
	RecordID     json.Number `json:"record_id"`
	RecID        string      `json:"recid"`
	ConceptRecID string      `json:"conceptrecid"`
	DOI          string      `json:"doi"`
	ConceptDOI   string      `json:"conceptdoi"`
	Created      string      `json:"created"`
	Modified     string      `json:"modified"`
	Updated      string      `json:"updated"`
	Title        string      `json:"title"`
	Status       string      `json:"status"`
	Metadata     metadata    `json:"metadata"`
	Links        recordLinks `json:"links"`
	Files        []file      `json:"files"`
}

type metadata struct {
	Title              string                `json:"title"`
	AdditionalTitles   many[additionalTitle] `json:"additional_titles"`
	DOI                string                `json:"doi"`
	PublicationDate    string                `json:"publication_date"`
	Description        string                `json:"description"`
	Notes              string                `json:"notes"`
	Publisher          string                `json:"publisher"`
	Language           string                `json:"language"`
	Version            string                `json:"version"`
	UploadType         string                `json:"upload_type"`
	PublicationType    string                `json:"publication_type"`
	ResourceType       resourceType          `json:"resource_type"`
	Creators           many[credit]          `json:"creators"`
	Contributors       many[credit]          `json:"contributors"`
	Keywords           []string              `json:"keywords"`
	Subjects           many[subject]         `json:"subjects"`
	License            license               `json:"license"`
	Rights             many[rights]          `json:"rights"`
	RelatedIdentifiers many[related]         `json:"related_identifiers"`
	Dates              many[date]            `json:"dates"`
	Grants             many[grant]           `json:"grants"`
}

type additionalTitle struct {
	Title string `json:"title"`
	Type  any    `json:"type"`
}

type resourceType struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Title   string `json:"title"`
}

type credit struct {
	Name         string            `json:"name"`
	GivenName    string            `json:"given_name"`
	FamilyName   string            `json:"family_name"`
	Type         string            `json:"type"`
	Affiliation  string            `json:"affiliation"`
	Affiliations many[affiliation] `json:"affiliations"`
	ORCID        string            `json:"orcid"`
	GND          string            `json:"gnd"`
	Role         any               `json:"role"`
	PersonOrOrg  personOrOrg       `json:"person_or_org"`
}

type personOrOrg struct {
	Name        string `json:"name"`
	GivenName   string `json:"given_name"`
	FamilyName  string `json:"family_name"`
	Type        string `json:"type"`
	Identifiers []struct {
		Scheme     string `json:"scheme"`
		Identifier string `json:"identifier"`
	} `json:"identifiers"`
}

type affiliation struct {
	Name       string `json:"name"`
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Scheme     string `json:"scheme"`
}

type subject struct {
	Subject    string            `json:"subject"`
	Identifier string            `json:"identifier"`
	Scheme     string            `json:"scheme"`
	ID         string            `json:"id"`
	Title      map[string]string `json:"title"`
}

type license struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type rights struct {
	Title       map[string]string `json:"title"`
	Description map[string]string `json:"description"`
	ID          string            `json:"id"`
	URI         string            `json:"uri"`
}

type related struct {
	Identifier   string       `json:"identifier"`
	Relation     any          `json:"relation"`
	ResourceType resourceType `json:"resource_type"`
}

type date struct {
	Date        string `json:"date"`
	Description string `json:"description"`
	Type        any    `json:"type"`
}

type grant struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Title   string `json:"title"`
	Acronym string `json:"acronym"`
	Funder  struct {
		Name string `json:"name"`
		DOI  string `json:"doi"`
		ID   string `json:"id"`
	} `json:"funder"`
}

type recordLinks struct {
	Self       string `json:"self"`
	SelfHTML   string `json:"self_html"`
	HTML       string `json:"html"`
	DOI        string `json:"doi"`
	Parent     string `json:"parent"`
	ParentHTML string `json:"parent_html"`
	ParentDOI  string `json:"parent_doi"`
}

type file struct {
	ID       string          `json:"id"`
	Key      string          `json:"key"`
	Filename string          `json:"filename"`
	Size     int64           `json:"size"`
	Checksum string          `json:"checksum"`
	Type     string          `json:"type"`
	Links    json.RawMessage `json:"links"`
}

type many[T any] []T

func (values *many[T]) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		*values = nil
		return nil
	}
	if data[0] == '[' {
		return json.Unmarshal(data, (*[]T)(values))
	}
	var single T
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	*values = []T{single}
	return nil
}

// Parse converts one Zenodo record or published-record search page into Hub records.
func (*Format) Parse(reader io.Reader, options *format.ParseOptions) ([]*hubv1.Record, error) {
	raw, err := readOneJSON(reader)
	if err != nil {
		return nil, fmt.Errorf("parsing Zenodo JSON: %w", err)
	}
	var marker map[string]json.RawMessage
	if err := json.Unmarshal(raw, &marker); err != nil {
		return nil, fmt.Errorf("parsing Zenodo JSON: %w", err)
	}
	var values []record
	if _, isPage := marker["hits"]; isPage {
		var page searchResponse
		if err := decodeJSON(raw, &page); err != nil {
			return nil, fmt.Errorf("parsing Zenodo records page: %w", err)
		}
		values = page.Hits.Hits
	} else {
		var single record
		if err := decodeJSON(raw, &single); err != nil {
			return nil, fmt.Errorf("parsing Zenodo record: %w", err)
		}
		values = append(values, single)
	}
	records := make([]*hubv1.Record, 0, len(values))
	for index := range values {
		record, err := toHub(&values[index], options)
		if err != nil {
			return nil, fmt.Errorf("converting Zenodo record %d: %w", index+1, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func toHub(value *record, options *format.ParseOptions) (*hubv1.Record, error) {
	recordID := firstNonempty(value.RecID, numericString(value.RecordID), numericString(value.ID))
	title := firstNonempty(value.Metadata.Title, value.Title)
	doi := hub.NormalizeIdentifier(firstNonempty(value.Metadata.DOI, value.DOI), hubv1.IdentifierType_IDENTIFIER_TYPE_DOI)
	if title == "" && doi == "" && recordID == "" {
		return nil, fmt.Errorf("title or identifier is required")
	}
	originalType := firstNonempty(
		zenodoResourceTypeIdentifier(value.Metadata.ResourceType),
		value.Metadata.PublicationType, value.Metadata.UploadType, value.Metadata.ResourceType.Title,
	)
	normalizedType := zenodoStructuredResourceType(value.Metadata.ResourceType)
	if normalizedType == hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED {
		normalizedType = zenodoResourceType(firstNonempty(value.Metadata.PublicationType, value.Metadata.UploadType, value.Metadata.ResourceType.Title))
	}
	sourceURI, err := provenanceuri.Normalize(value.Links.Self, provenanceuri.Options{})
	if err != nil {
		return nil, fmt.Errorf("source record URI: %w", err)
	}
	record := &hubv1.Record{
		Title:     title,
		Abstract:  strings.TrimSpace(helpers.StripHTML(value.Metadata.Description)),
		Publisher: strings.TrimSpace(value.Metadata.Publisher),
		Language:  strings.TrimSpace(value.Metadata.Language),
		ResourceType: &hubv1.ResourceType{
			Type:       normalizedType,
			Original:   originalType,
			Vocabulary: "Zenodo resource type",
		},
		SourceInfo: &hubv1.SourceInfo{
			Format:        "zenodo",
			FormatVersion: Version,
			SourceId:      recordID,
			Origin:        "zenodo",
			SourceUri:     sourceURI,
		},
	}
	for _, alternate := range value.Metadata.AdditionalTitles {
		if alternate.Title = strings.TrimSpace(alternate.Title); alternate.Title != "" {
			record.AltTitle = append(record.AltTitle, alternate.Title)
		}
	}
	if err := appendIdentifier(record, doi, "doi", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION, doi != ""); err != nil {
		return nil, err
	}
	conceptDOI := hub.NormalizeIdentifier(value.ConceptDOI, hubv1.IdentifierType_IDENTIFIER_TYPE_DOI)
	if conceptDOI != "" && !strings.EqualFold(conceptDOI, doi) {
		if err := appendIdentifier(record, conceptDOI, "doi", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT, false); err != nil {
			return nil, err
		}
		record.Relations = append(record.Relations, &hubv1.Relation{
			Type: hubv1.RelationType_RELATION_TYPE_VERSION_OF, TargetId: conceptDOI, TargetIdType: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, TargetUri: "https://doi.org/" + conceptDOI,
		})
	}
	// A Zenodo record is one published version of a concept. SourceInfo.SourceId
	// retains the API-record provenance; the identifier's identity level must
	// remain VERSION so profile-driven lookup and serialization agree with the
	// canonical identifier registry.
	if err := appendIdentifier(record, recordID, "zenodo-record", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION, doi == "" && recordID != ""); err != nil {
		return nil, err
	}
	if conceptID := strings.TrimSpace(value.ConceptRecID); conceptID != "" && conceptID != recordID {
		if err := appendIdentifier(record, conceptID, "zenodo-concept", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT, false); err != nil {
			return nil, err
		}
	}
	if publicURL := firstNonempty(value.Links.SelfHTML, value.Links.HTML); publicURL != "" {
		if err := appendIdentifier(record, publicURL, "url", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED, false); err != nil {
			return nil, err
		}
	}

	for _, creator := range value.Metadata.Creators {
		if contributor := zenodoContributor(creator, "creator"); contributor != nil {
			record.Contributors = append(record.Contributors, contributor)
		}
	}
	for _, contributor := range value.Metadata.Contributors {
		role := machineValue(contributor.Role)
		if role == "" {
			role = "contributor"
		}
		if parsed := zenodoContributor(contributor, role); parsed != nil {
			record.Contributors = append(record.Contributors, parsed)
		}
	}
	if published := parseZenodoDate(value.Metadata.PublicationDate, hubv1.DateType_DATE_TYPE_PUBLISHED, "metadata.publication_date", recordID); published != nil {
		record.Dates = append(record.Dates, published)
	}
	malformedDateCount := 0
	for _, item := range value.Metadata.Dates {
		if parsed := parseDate(item.Date, zenodoDateType(machineValue(item.Type))); parsed != nil {
			record.Dates = append(record.Dates, parsed)
		} else if strings.TrimSpace(item.Date) != "" {
			malformedDateCount++
		}
	}
	warnMalformedZenodoDates("metadata.dates.date", recordID, malformedDateCount)
	appendDateIfDistinct(record, value.Created, hubv1.DateType_DATE_TYPE_CREATED, "created", recordID)
	appendDateIfDistinct(record, firstNonempty(value.Modified, value.Updated), hubv1.DateType_DATE_TYPE_MODIFIED, "modified", recordID)
	for _, keyword := range compact(value.Metadata.Keywords) {
		record.Subjects = append(record.Subjects, &hubv1.Subject{Value: keyword, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS})
	}
	for _, item := range value.Metadata.Subjects {
		label := firstNonempty(item.Subject, localized(item.Title))
		if label == "" {
			continue
		}
		record.Subjects = append(record.Subjects, &hubv1.Subject{
			Value: label, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL, Uri: strings.TrimSpace(item.Identifier), SourceId: firstNonempty(item.ID, item.Identifier),
		})
	}
	appendLicense(record, value.Metadata.License)
	for _, item := range value.Metadata.Rights {
		record.Rights = append(record.Rights, &hubv1.Rights{Statement: firstNonempty(localized(item.Title), localized(item.Description)), Uri: strings.TrimSpace(item.URI), License: strings.TrimSpace(item.ID)})
	}
	for _, item := range value.Metadata.RelatedIdentifiers {
		if relation := zenodoRelation(item); relation != nil {
			record.Relations = append(record.Relations, relation)
		}
	}
	for _, item := range value.Metadata.Grants {
		funder := &hubv1.Funder{
			Name: firstNonempty(item.Funder.Name, item.Acronym), Identifier: firstNonempty(item.Funder.DOI, item.Funder.ID), IdentifierType: identifierScheme(firstNonempty(item.Funder.DOI, item.Funder.ID)),
			AwardNumbers: compact([]string{item.Code, item.ID}), AwardTitle: strings.TrimSpace(item.Title),
		}
		if funder.Name != "" || funder.Identifier != "" || len(funder.AwardNumbers) > 0 {
			record.Funders = append(record.Funders, funder)
		}
	}
	for _, item := range value.Files {
		file, err := zenodoFile(item)
		if err != nil {
			return nil, fmt.Errorf("zenodo file %q: %w", firstNonempty(item.Key, item.Filename), err)
		}
		record.Files = append(record.Files, file)
	}
	if value.Metadata.Version != "" {
		hub.SetExtra(record, "zenodo_version", strings.TrimSpace(value.Metadata.Version))
	}
	if value.Metadata.Notes != "" {
		hub.SetExtra(record, "zenodo_notes", strings.TrimSpace(helpers.StripHTML(value.Metadata.Notes)))
	}
	if value.Status != "" {
		hub.SetExtra(record, "zenodo_status", strings.TrimSpace(value.Status))
	}
	return record, nil
}

func zenodoContributor(value credit, role string) *hubv1.Contributor {
	name := firstNonempty(value.Name, value.PersonOrOrg.Name)
	given := firstNonempty(value.GivenName, value.PersonOrOrg.GivenName)
	family := firstNonempty(value.FamilyName, value.PersonOrOrg.FamilyName)
	if name == "" {
		name = strings.Trim(strings.Join([]string{family, given}, ", "), ", ")
	}
	if family == "" && given == "" {
		if left, right, found := strings.Cut(name, ","); found {
			family, given = strings.TrimSpace(left), strings.TrimSpace(right)
		}
	}
	kind := strings.ToLower(firstNonempty(value.Type, value.PersonOrOrg.Type))
	contributorType := hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON
	if strings.Contains(kind, "org") || strings.Contains(kind, "corporate") {
		contributorType = hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
	}
	contributor := &hubv1.Contributor{
		Name: name, Role: role, RoleCode: zenodoRoleCode(role), Type: contributorType,
		ParsedName: &hubv1.ParsedName{Given: given, Family: family, FullName: name, Normalized: strings.Trim(strings.Join([]string{family, given}, ", "), ", ")},
	}
	if contributorType == hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION {
		contributor.ParsedName = nil
	}
	affiliations := append([]affiliation(nil), value.Affiliations...)
	if strings.TrimSpace(value.Affiliation) != "" {
		affiliations = append([]affiliation{{Name: strings.TrimSpace(value.Affiliation)}}, affiliations...)
	}
	for _, item := range affiliations {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		affiliation := &hubv1.Affiliation{Name: name, Identifier: firstNonempty(item.Identifier, item.ID), IdentifierType: strings.TrimSpace(item.Scheme)}
		contributor.Affiliations = append(contributor.Affiliations, affiliation)
		if contributor.Affiliation == "" {
			contributor.Affiliation = name
		}
	}
	orcid := value.ORCID
	for _, identifier := range value.PersonOrOrg.Identifiers {
		if strings.EqualFold(identifier.Scheme, "orcid") && orcid == "" {
			orcid = identifier.Identifier
		}
	}
	if orcid = strings.TrimSpace(orcid); orcid != "" {
		identifier, err := hub.DefaultIdentifierRegistry().NewIdentifierForScheme(orcid, "orcid", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED)
		if err == nil {
			contributor.Identifiers = append(contributor.Identifiers, identifier)
		}
	}
	if gnd := strings.TrimSpace(value.GND); gnd != "" {
		identifier, err := hub.DefaultIdentifierRegistry().NewIdentifierForScheme(gnd, "gnd", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED)
		if err == nil {
			contributor.Identifiers = append(contributor.Identifiers, identifier)
		}
	}
	if contributor.GetName() == "" && len(contributor.GetAffiliations()) == 0 && len(contributor.GetIdentifiers()) == 0 {
		return nil
	}
	return contributor
}

func zenodoFile(value file) (*hubv1.File, error) {
	name := firstNonempty(value.Key, value.Filename)
	links := rawStringMap(value.Links)
	checksumAlgorithm, checksum := "", strings.TrimSpace(value.Checksum)
	if algorithm, digest, found := strings.Cut(checksum, ":"); found {
		checksumAlgorithm, checksum = strings.ToLower(strings.TrimSpace(algorithm)), strings.TrimSpace(digest)
	}
	uri, err := normalizeFileURL(firstNonempty(links["self"], links["content"], links["download"]))
	if err != nil {
		return nil, fmt.Errorf("resource URI: %w", err)
	}
	// The current Records API uses files[].links.self as the content URL,
	// while older/deposit responses may expose content or download explicitly.
	// Retain that current self-only shape as an actionable access URL.
	accessURL, err := normalizeFileURL(firstNonempty(links["content"], links["download"], links["self"]))
	if err != nil {
		return nil, fmt.Errorf("access URL: %w", err)
	}
	return &hubv1.File{
		Name: name, MimeType: strings.TrimSpace(value.Type), SizeBytes: value.Size, Role: "associated",
		Uri: uri, AccessUrl: accessURL, Checksum: checksum, ChecksumAlgorithm: checksumAlgorithm,
	}, nil
}

func normalizeFileURL(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return provenanceuri.Normalize(value, provenanceuri.Options{})
}

func zenodoRelation(value related) *hubv1.Relation {
	identifier := strings.TrimSpace(value.Identifier)
	if identifier == "" {
		return nil
	}
	identifierType := hub.DetectIdentifierType(identifier)
	targetIdentifier := hub.NewIdentifier(identifier, identifierType)
	relation := &hubv1.Relation{
		Type: zenodoRelationType(machineValue(value.Relation)), TargetId: targetIdentifier.GetValue(), TargetIdType: identifierType,
		TargetResourceType: zenodoStructuredResourceType(value.ResourceType),
	}
	switch identifierType {
	case hubv1.IdentifierType_IDENTIFIER_TYPE_URL,
		hubv1.IdentifierType_IDENTIFIER_TYPE_DOI,
		hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE,
		hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN,
		hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
		relation.TargetUri = hub.IdentifierURI(targetIdentifier)
	}
	return relation
}

func appendIdentifier(record *hubv1.Record, value, scheme string, level hubv1.IdentifierIdentityLevel, preferred bool) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	identifier, err := hub.DefaultIdentifierRegistry().NewIdentifierForScheme(value, scheme, level)
	if err != nil {
		return fmt.Errorf("canonicalizing %s identifier: %w", scheme, err)
	}
	identifier.IsPreferred = preferred
	for _, existing := range record.Identifiers {
		if existing != nil && strings.EqualFold(existing.Scheme, identifier.Scheme) && strings.EqualFold(existing.Value, identifier.Value) && existing.IdentityLevel == identifier.IdentityLevel {
			return nil
		}
	}
	record.Identifiers = append(record.Identifiers, identifier)
	return nil
}

func appendLicense(record *hubv1.Record, value license) {
	if value.ID == "" && value.Title == "" && value.URL == "" {
		return
	}
	record.Rights = append(record.Rights, &hubv1.Rights{Statement: strings.TrimSpace(value.Title), Uri: strings.TrimSpace(value.URL), License: strings.TrimSpace(value.ID)})
}

func appendDateIfDistinct(record *hubv1.Record, value string, dateType hubv1.DateType, field, sourceID string) {
	parsed := parseZenodoDate(value, dateType, field, sourceID)
	if parsed == nil {
		return
	}
	for _, existing := range record.Dates {
		if existing != nil && existing.Type == parsed.Type && existing.Raw == parsed.Raw {
			return
		}
	}
	record.Dates = append(record.Dates, parsed)
}

func parseZenodoDate(value string, dateType hubv1.DateType, field, sourceID string) *hubv1.DateValue {
	parsed := parseDate(value, dateType)
	if parsed == nil && strings.TrimSpace(value) != "" {
		warnMalformedZenodoDates(field, sourceID, 1)
	}
	return parsed
}

func warnMalformedZenodoDates(field, sourceID string, count int) {
	if count == 0 {
		return
	}
	attributes := []any{"field", field, "count", count}
	if safeSourceID := numericString(json.Number(sourceID)); safeSourceID != "" {
		attributes = append(attributes, "source_id", safeSourceID)
	}
	slog.Warn("ignoring malformed Zenodo date", attributes...)
}

func parseDate(value string, dateType hubv1.DateType) *hubv1.DateValue {
	value = strings.TrimSpace(value)
	if len(value) >= len("2006-01-02") && value[4] == '-' && value[7] == '-' {
		value = value[:len("2006-01-02")]
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		result := hub.NewDateFromYear(int32(parsed.Year()), dateType)
		switch layout {
		case "2006-01":
			result.Month = int32(parsed.Month())
			result.Precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
		case "2006-01-02":
			result.Month = int32(parsed.Month())
			result.Day = int32(parsed.Day())
			result.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
		}
		result.Raw = value
		return result
	}
	return nil
}

func zenodoResourceType(value string) hubv1.ResourceTypeValue {
	machine := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.TrimSpace(value)))
	switch machine {
	case "publication", "article", "journalarticle", "datapaper", "taxonomictreatment":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE
	case "book":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK
	case "journal":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_JOURNAL
	case "booksection", "section":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER
	case "conferencepaper":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER
	case "conferenceproceeding", "conferenceproceedings":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PROCEEDING
	case "annotationcollection":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION
	case "dataset":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET
	case "thesis", "dissertation":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS
	case "preprint":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT
	case "report", "datamanagementplan", "deliverable", "milestone", "proposal":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT
	case "technicalnote":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_TECHNICAL_REPORT
	case "workingpaper":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_WORKING_PAPER
	case "poster":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_POSTER
	case "presentation":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_PRESENTATION
	case "software", "computationalnotebook", "workflow":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE
	case "softwaredocumentation":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT
	case "image", "photo", "figure", "plot", "diagram", "drawing":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE
	case "video":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO
	case "audio":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO
	case "lesson":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT
	case "peerreview":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_PEER_REVIEW
	case "patent":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_PATENT
	case "standard":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_STANDARD
	case "physicalobject":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_OBJECT
	case "event", "model", "other":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER
	}
	return hub.NormalizeResourceType(value)
}

func zenodoStructuredResourceType(value resourceType) hubv1.ResourceTypeValue {
	typeName := strings.ToLower(strings.TrimSpace(value.Type))
	subtype := strings.ToLower(strings.TrimSpace(value.Subtype))
	// "other" is reused beneath more than one vocabulary branch. Keep the
	// parent for image-other instead of flattening it to the global other type.
	if subtype == "other" && typeName == "image" {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE
	}
	return zenodoResourceType(firstNonempty(subtype, typeName, value.Title))
}

func zenodoResourceTypeIdentifier(value resourceType) string {
	typeName := strings.ToLower(strings.TrimSpace(value.Type))
	subtype := strings.ToLower(strings.TrimSpace(value.Subtype))
	if typeName != "" && subtype != "" && subtype != typeName {
		return typeName + "-" + subtype
	}
	return firstNonempty(subtype, typeName, value.Title)
}

func zenodoRelationType(value string) hubv1.RelationType {
	switch strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(value)) {
	case "ispartof":
		return hubv1.RelationType_RELATION_TYPE_PART_OF
	case "haspart":
		return hubv1.RelationType_RELATION_TYPE_HAS_PART
	case "isversionof", "isnewversionof", "ispreviousversionof":
		return hubv1.RelationType_RELATION_TYPE_VERSION_OF
	case "hasversion":
		return hubv1.RelationType_RELATION_TYPE_HAS_VERSION
	case "isidenticalto":
		return hubv1.RelationType_RELATION_TYPE_IDENTICAL_TO
	case "references", "isreferencedby":
		return hubv1.RelationType_RELATION_TYPE_REFERENCES
	case "cites":
		return hubv1.RelationType_RELATION_TYPE_CITES
	case "iscitedby":
		return hubv1.RelationType_RELATION_TYPE_IS_CITED_BY
	case "isderivedfrom":
		return hubv1.RelationType_RELATION_TYPE_DERIVED_FROM
	case "issourceof":
		return hubv1.RelationType_RELATION_TYPE_SOURCE_OF
	case "issupplementto":
		return hubv1.RelationType_RELATION_TYPE_IS_SUPPLEMENT_TO
	case "issupplementedby":
		return hubv1.RelationType_RELATION_TYPE_SUPPLEMENTED_BY
	case "documents":
		return hubv1.RelationType_RELATION_TYPE_DOCUMENTS
	case "isdocumentedby":
		return hubv1.RelationType_RELATION_TYPE_IS_DOCUMENTED_BY
	case "describes":
		return hubv1.RelationType_RELATION_TYPE_DESCRIBES
	case "isdescribedby":
		return hubv1.RelationType_RELATION_TYPE_IS_DESCRIBED_BY
	case "requires":
		return hubv1.RelationType_RELATION_TYPE_REQUIRES
	case "isrequiredby":
		return hubv1.RelationType_RELATION_TYPE_REQUIRED_BY
	case "reviews":
		return hubv1.RelationType_RELATION_TYPE_REVIEWS
	}
	return hubv1.RelationType_RELATION_TYPE_RELATED_TO
}

func zenodoDateType(value string) hubv1.DateType {
	switch strings.ToLower(value) {
	case "available":
		return hubv1.DateType_DATE_TYPE_AVAILABLE
	case "collected":
		return hubv1.DateType_DATE_TYPE_COLLECTED
	case "created":
		return hubv1.DateType_DATE_TYPE_CREATED
	case "issued", "published":
		return hubv1.DateType_DATE_TYPE_PUBLISHED
	case "submitted":
		return hubv1.DateType_DATE_TYPE_SUBMITTED
	case "updated", "modified":
		return hubv1.DateType_DATE_TYPE_MODIFIED
	case "valid":
		return hubv1.DateType_DATE_TYPE_VALID
	}
	return hubv1.DateType_DATE_TYPE_OTHER
}

func zenodoRoleCode(value string) string {
	switch strings.ToLower(strings.ReplaceAll(value, "_", " ")) {
	case "creator", "author":
		return "relators:aut"
	case "editor":
		return "relators:edt"
	case "data curator", "datacurator":
		return "relators:cur"
	case "project leader", "projectleader":
		return "relators:pdr"
	case "researcher":
		return "relators:res"
	case "supervisor":
		return "relators:ths"
	}
	return "relators:ctb"
}

func machineValue(value any) string {
	switch item := value.(type) {
	case string:
		return strings.TrimSpace(item)
	case map[string]any:
		for _, key := range []string{"id", "type", "title", "name"} {
			if text, ok := item[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func rawStringMap(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func localized(values map[string]string) string {
	if value := strings.TrimSpace(values["en"]); value != "" {
		return value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func identifierScheme(value string) string {
	switch scheme := hub.DetectIdentifierScheme(value); scheme {
	case "doi", "ror", "isni", "gnd":
		return scheme
	}
	return ""
}

func compact(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func readOneJSON(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("reader is required")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxInputBytes {
		return nil, fmt.Errorf("input exceeds %d bytes", maxInputBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("input is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values are not allowed")
		}
		return nil, err
	}
	return value, nil
}

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func numericString(value json.Number) string {
	if value == "" {
		return ""
	}
	if _, err := strconv.ParseInt(value.String(), 10, 64); err != nil {
		return ""
	}
	return value.String()
}
