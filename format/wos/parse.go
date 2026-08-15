package wos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

const maxInputBytes = int64(64 << 20)

type response struct {
	Metadata struct {
		Total int `json:"total"`
		Page  int `json:"page"`
		Limit int `json:"limit"`
	} `json:"metadata"`
	Hits []document `json:"hits"`
}

type document struct {
	UID         string   `json:"uid"`
	Title       string   `json:"title"`
	Types       []string `json:"types"`
	SourceTypes []string `json:"sourceTypes"`
	Source      struct {
		Title        string `json:"sourceTitle"`
		PublishYear  int32  `json:"publishYear"`
		PublishMonth string `json:"publishMonth"`
		Volume       string `json:"volume"`
		Issue        string `json:"issue"`
		Pages        struct {
			Range string `json:"range"`
			Begin string `json:"begin"`
			End   string `json:"end"`
		} `json:"pages"`
	} `json:"source"`
	Names struct {
		Authors      []name `json:"authors"`
		BookEditors  []name `json:"bookEditors"`
		Contributors []name `json:"contributors"`
	} `json:"names"`
	Links struct {
		Record string `json:"record"`
	} `json:"links"`
	Identifiers struct {
		DOI   string `json:"doi"`
		ISSN  string `json:"issn"`
		EISSN string `json:"eissn"`
		ISBN  string `json:"isbn"`
		EISBN string `json:"eisbn"`
		PMID  string `json:"pmid"`
	} `json:"identifiers"`
	Keywords struct {
		Author []string `json:"authorKeywords"`
	} `json:"keywords"`
}

type name struct {
	DisplayName  string `json:"displayName"`
	WOSStandard  string `json:"wosStandard"`
	ResearcherID string `json:"researcherId"`
}

// Parse converts a Starter API document or result page into Hub records.
func (*Format) Parse(reader io.Reader, options *format.ParseOptions) ([]*hubv1.Record, error) {
	raw, err := readJSONDocument(reader, maxInputBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing Web of Science JSON: %w", err)
	}

	var marker map[string]json.RawMessage
	if err := json.Unmarshal(raw, &marker); err != nil {
		return nil, fmt.Errorf("parsing Web of Science JSON: %w", err)
	}
	documents := make([]document, 0)
	if _, isPage := marker["hits"]; isPage {
		var page response
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("parsing Web of Science result page: %w", err)
		}
		documents = page.Hits
	} else {
		var item document
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("parsing Web of Science document: %w", err)
		}
		if strings.TrimSpace(item.UID) == "" {
			return nil, fmt.Errorf("parsing Web of Science document: uid is required")
		}
		documents = append(documents, item)
	}

	records := make([]*hubv1.Record, 0, len(documents))
	for index := range documents {
		record, err := toHub(&documents[index])
		if err != nil {
			return nil, fmt.Errorf("converting Web of Science hit %d: %w", index+1, err)
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

func toHub(item *document) (*hubv1.Record, error) {
	uid := hub.NormalizeIdentifier(item.UID, hubv1.IdentifierType_IDENTIFIER_TYPE_WOS)
	if uid == "" {
		return nil, fmt.Errorf("uid is required")
	}
	originalType := firstNonempty(item.Types, item.SourceTypes)
	record := &hubv1.Record{
		Title: strings.TrimSpace(item.Title),
		ResourceType: &hubv1.ResourceType{
			Type:       hub.NormalizeResourceType(originalType),
			Original:   originalType,
			Vocabulary: "Web of Science document type",
		},
		Identifiers: []*hubv1.Identifier{{
			Type:        hubv1.IdentifierType_IDENTIFIER_TYPE_WOS,
			Value:       uid,
			IsPreferred: true,
		}},
		Publication: &hubv1.PublicationDetails{
			Title:  strings.TrimSpace(item.Source.Title),
			Volume: strings.TrimSpace(item.Source.Volume),
			Issue:  strings.TrimSpace(item.Source.Issue),
			Pages:  strings.TrimSpace(item.Source.Pages.Range),
		},
		SourceInfo: &hubv1.SourceInfo{
			Format:        "wos",
			FormatVersion: Version,
			SourceId:      uid,
		},
	}
	if record.Publication.Pages == "" && (item.Source.Pages.Begin != "" || item.Source.Pages.End != "") {
		record.Publication.Pages = strings.Trim(strings.TrimSpace(item.Source.Pages.Begin)+"-"+strings.TrimSpace(item.Source.Pages.End), "-")
	}
	if item.Source.PublishYear > 0 {
		date := hub.NewDateFromYear(item.Source.PublishYear, hubv1.DateType_DATE_TYPE_PUBLISHED)
		if month := parseMonth(item.Source.PublishMonth); month > 0 {
			date.Month = month
			date.Precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
		}
		date.Raw = strings.TrimSpace(strconv.Itoa(int(item.Source.PublishYear)) + " " + item.Source.PublishMonth)
		record.Dates = append(record.Dates, date)
	}
	for _, author := range item.Names.Authors {
		record.Contributors = append(record.Contributors, contributor(author, "author", "relators:aut"))
	}
	for _, editor := range item.Names.BookEditors {
		record.Contributors = append(record.Contributors, contributor(editor, "editor", "relators:edt"))
	}
	for _, other := range item.Names.Contributors {
		record.Contributors = append(record.Contributors, contributor(other, "contributor", "relators:ctb"))
	}
	appendIdentifier := func(value string, identifierType hubv1.IdentifierType) {
		value = hub.NormalizeIdentifier(value, identifierType)
		if value != "" {
			record.Identifiers = append(record.Identifiers, &hubv1.Identifier{Type: identifierType, Value: value})
		}
	}
	appendIdentifier(item.Identifiers.DOI, hubv1.IdentifierType_IDENTIFIER_TYPE_DOI)
	appendIdentifier(item.Identifiers.ISSN, hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN)
	appendIdentifier(item.Identifiers.EISSN, hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN)
	appendIdentifier(item.Identifiers.ISBN, hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN)
	appendIdentifier(item.Identifiers.EISBN, hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN)
	appendIdentifier(item.Identifiers.PMID, hubv1.IdentifierType_IDENTIFIER_TYPE_PMID)
	for _, keyword := range item.Keywords.Author {
		if keyword = strings.TrimSpace(keyword); keyword != "" {
			record.Subjects = append(record.Subjects, &hubv1.Subject{Value: keyword, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS})
		}
	}
	if item.Links.Record != "" {
		hub.SetExtra(record, "wos_record_url", strings.TrimSpace(item.Links.Record))
	}
	return record, nil
}

func firstNonempty(groups ...[]string) string {
	for _, group := range groups {
		for _, value := range group {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return ""
}

func contributor(value name, role, roleCode string) *hubv1.Contributor {
	display := strings.TrimSpace(value.DisplayName)
	if display == "" {
		display = strings.TrimSpace(value.WOSStandard)
	}
	parsed := &hubv1.ParsedName{FullName: display, Normalized: display}
	if family, given, found := strings.Cut(display, ","); found {
		parsed.Family = strings.TrimSpace(family)
		parsed.Given = strings.TrimSpace(given)
	} else {
		parts := strings.Fields(display)
		if len(parts) > 1 {
			parsed.Family = parts[len(parts)-1]
			parsed.Given = strings.Join(parts[:len(parts)-1], " ")
		} else if len(parts) == 1 {
			parsed.Family = parts[0]
		}
	}
	return &hubv1.Contributor{
		Name:       display,
		ParsedName: parsed,
		Role:       role,
		RoleCode:   roleCode,
		Type:       hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		SourceId:   strings.TrimSpace(value.ResearcherID),
	}
}

func parseMonth(value string) int32 {
	value = strings.TrimSpace(value)
	if numeric, err := strconv.Atoi(value); err == nil && numeric >= 1 && numeric <= 12 {
		return int32(numeric)
	}
	months := map[string]int32{
		"JAN": 1, "JANUARY": 1, "FEB": 2, "FEBRUARY": 2, "MAR": 3, "MARCH": 3,
		"APR": 4, "APRIL": 4, "MAY": 5, "JUN": 6, "JUNE": 6, "JUL": 7, "JULY": 7,
		"AUG": 8, "AUGUST": 8, "SEP": 9, "SEPT": 9, "SEPTEMBER": 9,
		"OCT": 10, "OCTOBER": 10, "NOV": 11, "NOVEMBER": 11, "DEC": 12, "DECEMBER": 12,
	}
	return months[strings.ToUpper(value)]
}
