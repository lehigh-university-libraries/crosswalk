package scopus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// Page is one decoded Scopus result page and its pagination metadata.
type Page struct {
	Records      []*hubv1.Record
	TotalResults int
	StartIndex   int
	ItemsPerPage int
	NextCursor   string
}

type response struct {
	SearchResults searchResults `json:"search-results"`
}

type searchResults struct {
	TotalResults string          `json:"opensearch:totalResults"`
	StartIndex   string          `json:"opensearch:startIndex"`
	ItemsPerPage string          `json:"opensearch:itemsPerPage"`
	Cursor       cursor          `json:"cursor"`
	Entries      []entry         `json:"entry"`
	Error        string          `json:"error"`
	Errors       json.RawMessage `json:"service-error"`
}

type cursor struct {
	Current string `json:"@current"`
	Next    string `json:"@next"`
}

type entry struct {
	Status             string            `json:"@status"`
	Error              string            `json:"error"`
	EID                string            `json:"eid"`
	Identifier         string            `json:"dc:identifier"`
	Title              string            `json:"dc:title"`
	Creator            string            `json:"dc:creator"`
	Description        string            `json:"dc:description"`
	PublicationName    string            `json:"prism:publicationName"`
	ISSN               string            `json:"prism:issn"`
	EISSN              string            `json:"prism:eIssn"`
	ISBN               json.RawMessage   `json:"prism:isbn"`
	Volume             string            `json:"prism:volume"`
	Issue              string            `json:"prism:issueIdentifier"`
	PageRange          string            `json:"prism:pageRange"`
	CoverDate          string            `json:"prism:coverDate"`
	DOI                string            `json:"prism:doi"`
	AggregationType    string            `json:"prism:aggregationType"`
	Subtype            string            `json:"subtype"`
	SubtypeDescription string            `json:"subtypeDescription"`
	CitedByCount       string            `json:"citedby-count"`
	OpenAccess         string            `json:"openaccess"`
	Links              many[link]        `json:"link"`
	Affiliations       many[affiliation] `json:"affiliation"`
	Authors            many[author]      `json:"author"`
	AuthorKeywords     string            `json:"authkeywords"`
	SubjectAreas       many[subject]     `json:"subject-area"`
}

type link struct {
	Ref  string `json:"@ref"`
	Href string `json:"@href"`
}

type affiliation struct {
	ID      string `json:"afid"`
	Name    string `json:"affilname"`
	City    string `json:"affiliation-city"`
	Country string `json:"affiliation-country"`
}

type author struct {
	ID           string          `json:"authid"`
	Name         string          `json:"authname"`
	Given        string          `json:"given-name"`
	Surname      string          `json:"surname"`
	Initials     string          `json:"initials"`
	ORCID        string          `json:"orcid"`
	URL          string          `json:"author-url"`
	Affiliations json.RawMessage `json:"afid"`
}

type subject struct {
	Code   string `json:"@code"`
	Abbrev string `json:"@abbrev"`
	Value  string `json:"$"`
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

// Parse converts a Scopus entry or result page into Hub records.
func (f *Format) Parse(reader io.Reader, options *format.ParseOptions) ([]*hubv1.Record, error) {
	page, err := f.ParsePage(reader, options)
	if err != nil {
		return nil, err
	}
	return page.Records, nil
}

// ParsePage converts a Scopus result while retaining pagination metadata.
func (*Format) ParsePage(reader io.Reader, options *format.ParseOptions) (*Page, error) {
	raw, err := readOneJSON(reader)
	if err != nil {
		return nil, fmt.Errorf("parsing Scopus JSON: %w", err)
	}
	var marker map[string]json.RawMessage
	if err := json.Unmarshal(raw, &marker); err != nil {
		return nil, fmt.Errorf("parsing Scopus JSON: %w", err)
	}

	result := &Page{}
	entries := make([]entry, 0)
	if _, isPage := marker["search-results"]; isPage {
		var decoded response
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, fmt.Errorf("parsing Scopus result page: %w", err)
		}
		if decoded.SearchResults.Error != "" || len(decoded.SearchResults.Errors) > 0 {
			return nil, fmt.Errorf("parsing Scopus result page: API returned an error payload")
		}
		result.TotalResults = parseNonnegativeInt(decoded.SearchResults.TotalResults)
		result.StartIndex = parseNonnegativeInt(decoded.SearchResults.StartIndex)
		result.ItemsPerPage = parseNonnegativeInt(decoded.SearchResults.ItemsPerPage)
		result.NextCursor = strings.TrimSpace(decoded.SearchResults.Cursor.Next)
		entries = decoded.SearchResults.Entries
	} else {
		var single entry
		if err := json.Unmarshal(raw, &single); err != nil {
			return nil, fmt.Errorf("parsing Scopus entry: %w", err)
		}
		entries = append(entries, single)
	}

	result.Records = make([]*hubv1.Record, 0, len(entries))
	for index := range entries {
		record, err := toHub(&entries[index], options)
		if err != nil {
			return nil, fmt.Errorf("converting Scopus entry %d: %w", index+1, err)
		}
		result.Records = append(result.Records, record)
	}
	return result, nil
}

func toHub(value *entry, options *format.ParseOptions) (*hubv1.Record, error) {
	if value.Error != "" || strings.EqualFold(strings.TrimSpace(value.Status), "RESOURCE_NOT_FOUND") {
		return nil, fmt.Errorf("API returned an unavailable result")
	}
	title := strings.TrimSpace(value.Title)
	doi := hub.NormalizeIdentifier(value.DOI, hubv1.IdentifierType_IDENTIFIER_TYPE_DOI)
	eid := strings.TrimSpace(value.EID)
	scopusID := strings.TrimSpace(value.Identifier)
	scopusID = strings.TrimPrefix(strings.TrimPrefix(scopusID, "SCOPUS_ID:"), "scopus_id:")
	if title == "" && doi == "" && eid == "" && scopusID == "" {
		return nil, fmt.Errorf("title or identifier is required")
	}
	originalType := strings.TrimSpace(value.SubtypeDescription)
	if originalType == "" {
		originalType = strings.TrimSpace(value.AggregationType)
	}
	record := &hubv1.Record{
		Title:    title,
		Abstract: strings.TrimSpace(helpers.StripHTML(value.Description)),
		ResourceType: &hubv1.ResourceType{
			Type:       scopusResourceType(value.Subtype, originalType),
			Original:   originalType,
			Vocabulary: "Scopus document type",
		},
		Publication: &hubv1.PublicationDetails{
			Title:  strings.TrimSpace(value.PublicationName),
			Volume: strings.TrimSpace(value.Volume),
			Issue:  strings.TrimSpace(value.Issue),
			Pages:  strings.TrimSpace(value.PageRange),
		},
		SourceInfo: &hubv1.SourceInfo{
			Format:        "scopus",
			FormatVersion: Version,
			SourceId:      firstNonempty(eid, scopusID, doi),
			Origin:        "scopus",
		},
	}
	printISSN, err := canonicalContainerIdentifier(value.ISSN, "issn")
	if err != nil {
		return nil, err
	}
	record.Publication.Issn = printISSN
	electronicISSN, err := canonicalContainerIdentifier(value.EISSN, "issn")
	if err != nil {
		return nil, err
	}
	if electronicISSN != "" {
		// PublicationDetails currently has no distinct eISSN slot. Preserve it
		// explicitly without misclassifying the journal identifier as work identity.
		hub.SetExtra(record, "publication_eissn", electronicISSN)
	}
	containerISBNs := make([]any, 0)
	for _, isbn := range rawStrings(value.ISBN) {
		canonical, canonicalErr := canonicalContainerIdentifier(isbn, "isbn")
		if canonicalErr != nil {
			return nil, canonicalErr
		}
		if canonical != "" {
			containerISBNs = append(containerISBNs, canonical)
		}
	}
	if len(containerISBNs) != 0 {
		// Scopus prism:isbn describes the containing publication. Keep it out
		// of Record.Identifiers so reconciliation cannot mistake container
		// identity for work identity.
		hub.SetExtra(record, "publication_isbn", containerISBNs)
	}
	if err := appendIdentifier(record, doi, "doi", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK, doi != ""); err != nil {
		return nil, err
	}
	if err := appendIdentifier(record, eid, "scopus-eid", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD, doi == "" && eid != ""); err != nil {
		return nil, err
	}
	if err := appendIdentifier(record, scopusID, "scopus-id", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD, doi == "" && eid == ""); err != nil {
		return nil, err
	}
	affiliations := make(map[string]affiliation, len(value.Affiliations))
	for _, item := range value.Affiliations {
		if id := strings.TrimSpace(item.ID); id != "" {
			affiliations[id] = item
		}
	}
	for _, item := range value.Authors {
		record.Contributors = append(record.Contributors, scopusContributor(item, affiliations))
	}
	if len(record.Contributors) == 0 && strings.TrimSpace(value.Creator) != "" {
		record.Contributors = append(record.Contributors, contributorFromDisplay(value.Creator))
	}
	if date := parseDate(value.CoverDate, hubv1.DateType_DATE_TYPE_PUBLISHED); date != nil {
		record.Dates = append(record.Dates, date)
	}
	for _, keyword := range splitKeywords(value.AuthorKeywords) {
		record.Subjects = append(record.Subjects, &hubv1.Subject{Value: keyword, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS})
	}
	for _, area := range value.SubjectAreas {
		if label := strings.TrimSpace(area.Value); label != "" {
			record.Subjects = append(record.Subjects, &hubv1.Subject{Value: label, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL, SourceId: strings.TrimSpace(area.Code)})
		}
	}
	for _, item := range value.Links {
		if !strings.EqualFold(strings.TrimSpace(item.Ref), "scopus") || strings.TrimSpace(item.Href) == "" {
			continue
		}
		sourceURI, err := provenanceuri.Normalize(item.Href, provenanceuri.Options{})
		if err != nil {
			return nil, fmt.Errorf("source record URI: %w", err)
		}
		if err := appendIdentifier(record, sourceURI, "url", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED, false); err != nil {
			return nil, err
		}
		if record.SourceInfo.SourceUri == "" {
			record.SourceInfo.SourceUri = sourceURI
		}
		break
	}
	if count, err := strconv.ParseInt(strings.TrimSpace(value.CitedByCount), 10, 64); err == nil && count >= 0 {
		hub.SetExtra(record, "scopus_cited_by_count", count)
	}
	if value.OpenAccess != "" {
		hub.SetExtra(record, "scopus_open_access", strings.TrimSpace(value.OpenAccess))
	}
	if value.Subtype != "" {
		hub.SetExtra(record, "scopus_subtype", strings.TrimSpace(value.Subtype))
	}
	return record, nil
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

func canonicalContainerIdentifier(value, scheme string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	identifier, err := hub.DefaultIdentifierRegistry().NewIdentifierForScheme(
		value, scheme, hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED,
	)
	if err != nil {
		return "", fmt.Errorf("canonicalizing publication %s: %w", scheme, err)
	}
	return identifier.Value, nil
}

func scopusContributor(value author, affiliations map[string]affiliation) *hubv1.Contributor {
	display := strings.TrimSpace(value.Name)
	given := strings.TrimSpace(value.Given)
	family := strings.TrimSpace(value.Surname)
	if display == "" {
		display = strings.Trim(strings.Join([]string{family, given}, ", "), ", ")
	}
	contributor := &hubv1.Contributor{
		Name: display,
		ParsedName: &hubv1.ParsedName{
			Given: given, Family: family, FullName: display, Normalized: strings.Trim(strings.Join([]string{family, given}, ", "), ", "),
		},
		Role: "author", RoleCode: "relators:aut", Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		SourceId: strings.TrimSpace(value.ID), Url: strings.TrimSpace(value.URL),
	}
	if orcid := hub.NormalizeIdentifier(value.ORCID, hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID); orcid != "" {
		identifier, err := hub.DefaultIdentifierRegistry().NewIdentifierForScheme(orcid, "orcid", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED)
		if err == nil {
			contributor.Identifiers = append(contributor.Identifiers, identifier)
		}
	}
	for _, id := range rawStrings(value.Affiliations) {
		item, exists := affiliations[id]
		if !exists || strings.TrimSpace(item.Name) == "" {
			continue
		}
		affiliation := &hubv1.Affiliation{Name: strings.TrimSpace(item.Name), Identifier: id, IdentifierType: "scopus-afid"}
		contributor.Affiliations = append(contributor.Affiliations, affiliation)
		if contributor.Affiliation == "" {
			contributor.Affiliation = affiliation.Name
		}
	}
	return contributor
}

func contributorFromDisplay(value string) *hubv1.Contributor {
	display := strings.TrimSpace(value)
	parsed := &hubv1.ParsedName{FullName: display, Normalized: display}
	if family, given, found := strings.Cut(display, ","); found {
		parsed.Family = strings.TrimSpace(family)
		parsed.Given = strings.TrimSpace(given)
	}
	return &hubv1.Contributor{Name: display, ParsedName: parsed, Role: "author", RoleCode: "relators:aut", Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON}
}

func scopusResourceType(code, label string) hubv1.ResourceTypeValue {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "ar", "re", "sh", "le", "ed", "no":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE
	case "cp":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER
	case "ch":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER
	case "bk":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK
	case "dp":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET
	}
	return hub.NormalizeResourceType(label)
}

func parseDate(value string, dateType hubv1.DateType) *hubv1.DateValue {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		result := hub.NewDateFromYear(int32(parsed.Year()), dateType)
		if layout == "2006-01" {
			result.Month = int32(parsed.Month())
			result.Precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
		} else if layout == "2006-01-02" {
			result.Month = int32(parsed.Month())
			result.Day = int32(parsed.Day())
			result.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
		}
		result.Raw = value
		return result
	}
	return nil
}

func splitKeywords(value string) []string {
	parts := strings.FieldsFunc(value, func(character rune) bool { return character == '|' || character == ';' })
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		key := strings.ToLower(part)
		if part == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, part)
	}
	return result
}

func rawStrings(raw json.RawMessage) []string {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	result := make([]string, 0)
	var collect func(any)
	collect = func(current any) {
		switch item := current.(type) {
		case string:
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		case json.Number:
			result = append(result, item.String())
		case []any:
			for _, child := range item {
				collect(child)
			}
		case map[string]any:
			if child, exists := item["$"]; exists {
				collect(child)
			}
		}
	}
	collect(value)
	return splitAndDedupe(result)
}

func splitAndDedupe(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(character rune) bool { return character == '|' || character == ';' }) {
			part = strings.TrimSpace(part)
			key := strings.ToLower(part)
			if part == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, part)
		}
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

func parseNonnegativeInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
