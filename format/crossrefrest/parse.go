package crossrefrest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

const maxInputBytes = int64(64 << 20)

type response struct {
	Message struct {
		Items []item `json:"items"`
	} `json:"message"`
}

type item struct {
	DOI             string   `json:"DOI"`
	URL             string   `json:"URL"`
	Title           []string `json:"title"`
	Subtitle        []string `json:"subtitle"`
	Abstract        string   `json:"abstract"`
	Type            string   `json:"type"`
	Publisher       string   `json:"publisher"`
	Author          []person `json:"author"`
	Editor          []person `json:"editor"`
	ContainerTitle  []string `json:"container-title"`
	Volume          string   `json:"volume"`
	Issue           string   `json:"issue"`
	Page            string   `json:"page"`
	ISSN            []string `json:"ISSN"`
	ISBN            []string `json:"ISBN"`
	Subject         []string `json:"subject"`
	Issued          date     `json:"issued"`
	Published       date     `json:"published"`
	PublishedPrint  date     `json:"published-print"`
	PublishedOnline date     `json:"published-online"`
	License         []struct {
		URL string `json:"URL"`
	} `json:"license"`
	Link []struct {
		URL         string `json:"URL"`
		ContentType string `json:"content-type"`
	} `json:"link"`
}

type person struct {
	Given       string `json:"given"`
	Family      string `json:"family"`
	Name        string `json:"name"`
	ORCID       string `json:"ORCID"`
	Affiliation []struct {
		Name string `json:"name"`
	} `json:"affiliation"`
}

type date struct {
	DateParts [][]int32 `json:"date-parts"`
}

// Parse converts one Crossref REST search response into Hub records.
func (*Format) Parse(reader io.Reader, options *format.ParseOptions) ([]*hubv1.Record, error) {
	raw, err := readJSONDocument(reader, maxInputBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing Crossref REST JSON: %w", err)
	}
	var decoded response
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("parsing Crossref REST JSON: %w", err)
	}
	records := make([]*hubv1.Record, 0, len(decoded.Message.Items))
	for index := range decoded.Message.Items {
		record, err := toHub(&decoded.Message.Items[index])
		if err != nil {
			return nil, fmt.Errorf("converting Crossref result %d: %w", index+1, err)
		}
		if options != nil && strings.TrimSpace(options.SourceName) != "" {
			hub.SetExtra(record, "source_url", options.SourceName)
		}
		records = append(records, record)
	}
	return records, nil
}

func readJSONDocument(reader io.Reader, maxBytes int64) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("input reader is required")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("input exceeds %d bytes", maxBytes)
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty input")
	}
	return raw, nil
}

func toHub(value *item) (*hubv1.Record, error) {
	doi := hub.NormalizeIdentifier(value.DOI, hubv1.IdentifierType_IDENTIFIER_TYPE_DOI)
	title := first(value.Title)
	if doi == "" && title == "" {
		return nil, fmt.Errorf("DOI or title is required")
	}
	record := &hubv1.Record{
		Title:     title,
		AltTitle:  compact(value.Subtitle),
		Abstract:  strings.TrimSpace(helpers.StripHTML(value.Abstract)),
		Publisher: strings.TrimSpace(value.Publisher),
		ResourceType: &hubv1.ResourceType{
			Type:       crossrefResourceType(value.Type),
			Original:   strings.TrimSpace(value.Type),
			Vocabulary: "Crossref work type",
		},
		Publication: &hubv1.PublicationDetails{
			Title:  first(value.ContainerTitle),
			Volume: strings.TrimSpace(value.Volume),
			Issue:  strings.TrimSpace(value.Issue),
			Pages:  strings.TrimSpace(value.Page),
		},
		SourceInfo: &hubv1.SourceInfo{Format: "crossref-rest", FormatVersion: Version, SourceId: doi},
	}
	appendIdentifier := func(raw string, identifierType hubv1.IdentifierType, preferred bool) {
		normalized := hub.NormalizeIdentifier(raw, identifierType)
		if normalized != "" {
			identifier := &hubv1.Identifier{Type: identifierType, Value: normalized, IsPreferred: preferred}
			if identifierType == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI {
				identifier.IdentityLevel = hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK
			}
			record.Identifiers = append(record.Identifiers, identifier)
		}
	}
	appendIdentifier(doi, hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, true)
	appendIdentifier(value.URL, hubv1.IdentifierType_IDENTIFIER_TYPE_URL, doi == "")
	for _, issn := range value.ISSN {
		appendIdentifier(issn, hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN, false)
	}
	for _, isbn := range value.ISBN {
		appendIdentifier(isbn, hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN, false)
	}
	for _, author := range value.Author {
		record.Contributors = append(record.Contributors, toContributor(author, "author", "relators:aut"))
	}
	for _, editor := range value.Editor {
		record.Contributors = append(record.Contributors, toContributor(editor, "editor", "relators:edt"))
	}
	if published := bestDate(value.PublishedPrint, value.PublishedOnline, value.Published, value.Issued); published != nil {
		record.Dates = append(record.Dates, published)
	}
	for _, subject := range compact(value.Subject) {
		record.Subjects = append(record.Subjects, &hubv1.Subject{Value: subject, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS})
	}
	for _, license := range value.License {
		if uri := strings.TrimSpace(license.URL); uri != "" {
			record.Rights = append(record.Rights, &hubv1.Rights{Uri: uri})
		}
	}
	for _, link := range value.Link {
		if strings.EqualFold(strings.TrimSpace(link.ContentType), "application/pdf") && strings.TrimSpace(link.URL) != "" {
			hub.SetExtra(record, "pdf_url", strings.TrimSpace(link.URL))
			break
		}
	}
	if record.SourceInfo.SourceId == "" {
		record.SourceInfo.SourceId = strings.TrimSpace(value.URL)
	}
	return record, nil
}

func toContributor(value person, role, roleCode string) *hubv1.Contributor {
	given := strings.TrimSpace(value.Given)
	family := strings.TrimSpace(value.Family)
	display := strings.TrimSpace(value.Name)
	if display == "" {
		display = strings.TrimSpace(strings.Join([]string{given, family}, " "))
	}
	contributor := &hubv1.Contributor{
		Name: display,
		ParsedName: &hubv1.ParsedName{
			Given:      given,
			Family:     family,
			FullName:   display,
			Normalized: strings.Trim(strings.Join([]string{family, given}, ", "), ", "),
		},
		Role:     role,
		RoleCode: roleCode,
		Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
	}
	if orcid := hub.NormalizeIdentifier(value.ORCID, hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID); orcid != "" {
		contributor.Identifiers = append(contributor.Identifiers, &hubv1.Identifier{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID, Value: orcid})
	}
	for _, affiliation := range value.Affiliation {
		if name := strings.TrimSpace(affiliation.Name); name != "" {
			if contributor.Affiliation == "" {
				contributor.Affiliation = name
			}
			contributor.Affiliations = append(contributor.Affiliations, &hubv1.Affiliation{Name: name})
		}
	}
	return contributor
}

func bestDate(values ...date) *hubv1.DateValue {
	for _, value := range values {
		if len(value.DateParts) == 0 || len(value.DateParts[0]) == 0 || value.DateParts[0][0] <= 0 {
			continue
		}
		parts := value.DateParts[0]
		result := hub.NewDateFromYear(parts[0], hubv1.DateType_DATE_TYPE_PUBLISHED)
		if len(parts) > 1 && parts[1] > 0 {
			result.Month = parts[1]
			result.Precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
		}
		if len(parts) > 2 && parts[2] > 0 {
			result.Day = parts[2]
			result.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
		}
		result.Raw = hub.FormatDate(result)
		return result
	}
	return nil
}

func crossrefResourceType(value string) hubv1.ResourceTypeValue {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "journal-article", "component":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE
	case "book-chapter", "book-section", "reference-entry":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER
	case "proceedings-article":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER
	case "proceedings":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PROCEEDING
	case "posted-content":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT
	case "dissertation":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION
	case "report", "report-series":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT
	case "journal", "journal-volume", "journal-issue":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_JOURNAL
	case "book", "edited-book", "reference-book", "book-series", "book-set":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK
	}
	return hub.NormalizeResourceType(value)
}

func first(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func compact(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
