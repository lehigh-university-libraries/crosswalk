package csl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

// Parse reads either one CSL-JSON item or an array of items into Hub records.
func (f *Format) Parse(r io.Reader, _ *format.ParseOptions) ([]*hubv1.Record, error) {
	data, err := format.ReadInput(r)
	if err != nil {
		return nil, fmt.Errorf("reading CSL-JSON: %w", err)
	}
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	if len(data) == 0 {
		return nil, nil
	}

	var items []JSONItem
	switch data[0] {
	case '{':
		var item JSONItem
		if err := decodeJSON(data, &item); err != nil {
			return nil, err
		}
		items = []JSONItem{item}
	case '[':
		if err := decodeJSON(data, &items); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("parsing CSL-JSON: expected an object or array")
	}

	records := make([]*hubv1.Record, 0, len(items))
	for i, item := range items {
		record, err := jsonToHub(item)
		if err != nil {
			return nil, fmt.Errorf("converting CSL item %d: %w", i+1, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("parsing CSL-JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("parsing CSL-JSON: multiple top-level values")
		}
		return fmt.Errorf("parsing CSL-JSON: trailing data: %w", err)
	}
	return nil
}

func jsonToHub(item JSONItem) (*hubv1.Record, error) {
	record := hub.NewRecord()
	record.Title = strings.TrimSpace(item.Title)
	record.Abstract = strings.TrimSpace(item.Abstract)
	hub.SetLanguages(record, []string{item.Language})
	hub.SetPublishers(record, []string{item.Publisher})
	hub.SetPlacesPublished(record, []string{item.PublisherPlace})
	record.Dimensions = strings.TrimSpace(item.Dimensions)
	hub.SetEditions(record, []string{item.Edition})
	record.ResourceType = hub.NewResourceType(cslTypeToResourceType(item.Type), "CSL")

	appendNames := func(names []JSONName, role, roleCode string) {
		for _, name := range names {
			contributor := jsonNameToContributor(name, role, roleCode)
			if contributor.Name != "" {
				record.Contributors = append(record.Contributors, contributor)
			}
		}
	}
	appendNames(item.Author, "author", "relators:aut")
	appendNames(item.Editor, "editor", "relators:edt")
	appendNames(item.Translator, "translator", "relators:trl")

	if date := jsonDateToHub(item.Issued, hubv1.DateType_DATE_TYPE_ISSUED); date != nil {
		record.Dates = append(record.Dates, date)
	}

	identifiers := []struct {
		value string
		type_ hubv1.IdentifierType
	}{
		{item.DOI, hubv1.IdentifierType_IDENTIFIER_TYPE_DOI},
		{item.URL, hubv1.IdentifierType_IDENTIFIER_TYPE_URL},
		{item.PMID, hubv1.IdentifierType_IDENTIFIER_TYPE_PMID},
		{item.PMCID, hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID},
	}
	for _, value := range item.ISBN {
		identifiers = append(identifiers, struct {
			value string
			type_ hubv1.IdentifierType
		}{value, hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN})
	}
	for _, value := range item.ISSN {
		identifiers = append(identifiers, struct {
			value string
			type_ hubv1.IdentifierType
		}{value, hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN})
	}
	if strings.TrimSpace(item.ID) != "" && item.ID != item.DOI {
		identifiers = append(identifiers, struct {
			value string
			type_ hubv1.IdentifierType
		}{item.ID, hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL})
	}
	for _, identifier := range identifiers {
		if value := strings.TrimSpace(identifier.value); value != "" {
			record.Identifiers = append(record.Identifiers, hub.NewIdentifier(value, identifier.type_))
		}
	}

	issn := firstJSONString(item.ISSN)
	if item.ContainerTitle != "" || item.Volume != "" || item.Issue != "" || item.Page != "" || issn != "" {
		record.Publication = &hubv1.PublicationDetails{
			Title:  strings.TrimSpace(item.ContainerTitle),
			Volume: strings.TrimSpace(item.Volume),
			Issue:  strings.TrimSpace(item.Issue),
			Pages:  strings.TrimSpace(item.Page),
			Issn:   issn,
		}
	}
	if container := strings.TrimSpace(item.ContainerTitle); container != "" {
		record.Relations = append(record.Relations, &hubv1.Relation{
			Type:        hubv1.RelationType_RELATION_TYPE_PART_OF,
			TargetTitle: container,
		})
	}
	if note := strings.TrimSpace(item.Note); note != "" {
		record.Notes = append(record.Notes, note)
	}
	for _, keyword := range strings.FieldsFunc(item.Keyword, func(r rune) bool { return r == ',' || r == ';' }) {
		if keyword = strings.TrimSpace(keyword); keyword != "" {
			record.Subjects = append(record.Subjects, &hubv1.Subject{
				Value:      keyword,
				Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS,
			})
		}
	}

	sourceID := strings.TrimSpace(item.DOI)
	if sourceID == "" {
		sourceID = strings.TrimSpace(item.ID)
	}
	record.SourceInfo = &hubv1.SourceInfo{
		Format:        "csl",
		FormatVersion: Version,
		SourceId:      sourceID,
	}
	return record, nil
}

func firstJSONString(values JSONStringList) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func jsonNameToContributor(name JSONName, role, roleCode string) *hubv1.Contributor {
	family := strings.TrimSpace(name.Family)
	given := strings.TrimSpace(name.Given)
	literal := strings.TrimSpace(name.Literal)
	contributor := &hubv1.Contributor{
		Role:     role,
		RoleCode: roleCode,
		Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
	}
	if literal != "" {
		contributor.Name = literal
		return contributor
	}
	contributor.ParsedName = &hubv1.ParsedName{
		Family: family,
		Given:  given,
		Suffix: strings.TrimSpace(name.Suffix),
	}
	contributor.Name = hub.ParsedNameInverted(contributor.ParsedName)
	contributor.ParsedName.FullName = contributor.Name
	contributor.ParsedName.Normalized = contributor.Name
	return contributor
}

func jsonDateToHub(date *JSONDate, dateType hubv1.DateType) *hubv1.DateValue {
	if date == nil || len(date.DateParts) == 0 || len(date.DateParts[0]) == 0 {
		return nil
	}
	parts := date.DateParts[0]
	result := hub.NewDateFromYear(int32(parts[0]), dateType)
	if len(parts) > 1 {
		result.Month = int32(parts[1])
		result.Precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
	}
	if len(parts) > 2 {
		result.Day = int32(parts[2])
		result.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
	}
	return result
}

func cslTypeToResourceType(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "article-journal", "article-magazine", "article-newspaper":
		return "journal article"
	case "chapter":
		return "book chapter"
	case "paper-conference":
		return "conference paper"
	case "motion_picture":
		return "video"
	case "song", "broadcast":
		return "audio"
	case "graphic", "figure":
		return "image"
	default:
		return value
	}
}
