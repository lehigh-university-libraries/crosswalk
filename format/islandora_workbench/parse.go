package islandora_workbench

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
)

// Parse reads an Islandora Workbench CSV and returns hub records.
//
// If opts.Profile is set, its field mappings are consulted first when resolving
// column names to hub fields. This allows sites with custom field configurations
// to override or extend the default Islandora Workbench column mappings.
func (f *Format) Parse(r io.Reader, opts *format.ParseOptions) ([]*hubv1.Record, error) {
	if opts == nil {
		opts = format.NewParseOptions()
	}

	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing workbench CSV: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	header := rows[0]
	colMap := buildWorkbenchColumnMap(header, opts.Profile)

	records := make([]*hubv1.Record, 0, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		if record := workbenchRowToRecord(rows[i], header, colMap, opts); record != nil {
			records = append(records, record)
		}
	}

	return records, nil
}

// buildWorkbenchColumnMap maps each column index to an IR field name.
// The profile is consulted first; unmatched columns fall back to the default
// Islandora Workbench field name mappings.
func buildWorkbenchColumnMap(header []string, p *mapping.Profile) map[int]string {
	defaults := defaultWorkbenchColumnMap()
	colMap := make(map[int]string, len(header))

	for i, col := range header {
		col = strings.TrimSpace(col)

		if p != nil {
			if m, ok := p.Fields[col]; ok {
				colMap[i] = m.IR
				continue
			}
		}

		if ir, ok := defaults[col]; ok {
			colMap[i] = ir
		}
	}

	return colMap
}

// defaultWorkbenchColumnMap returns the standard Islandora Workbench column
// name to IR field mappings used when no profile is provided.
func defaultWorkbenchColumnMap() map[string]string {
	return map[string]string{
		// Reserved Workbench columns
		"id":             "Extra.id",
		"parent_id":      "Extra.parent_id",
		"node_id":        "Extra.node_id",
		"file":           "Extra.file",
		"url_alias":      "Extra.url_alias",
		"image_alt_text": "Extra.image_alt_text",
		"checksum":       "Extra.checksum",
		"media_use_tid":  "Extra.media_use_tid",

		// Core
		"title":            "Title",
		"field_full_title": "Title",
		"field_alt_title":  "AltTitle",

		// Contributors
		"field_linked_agent": "Contributors",

		// Dates (EDTF)
		"field_edtf_date_issued":   "Dates.issued",
		"field_edtf_date_created":  "Dates.created",
		"field_edtf_date_captured": "Dates.captured",
		"field_copyright_date":     "Dates.copyright",
		"field_date_modified":      "Dates.modified",

		// Resource type and model
		"field_model":         "ResourceType",
		"field_resource_type": "ResourceType",

		// Language
		"field_language": "Language",

		// Rights
		"field_rights": "Rights",

		// Descriptions
		"field_abstract":             "Abstract",
		"field_description":          "Description",
		"field_physical_description": "PhysicalDesc",
		"field_extent":               "PhysicalDesc",

		// Subjects
		"field_subject":         "Subjects",
		"field_lcsh_topic":      "Subjects.lcsh",
		"field_subject_general": "Subjects.local",
		"field_keywords":        "Subjects.keywords",

		// Genre
		"field_genre": "Genre",

		// Identifiers
		"field_identifier": "Identifiers",
		"field_pid":        "Identifiers.pid",

		// Relations
		"field_member_of":    "Relations.member_of",
		"field_related_item": "Publication.title",
		"field_part_detail":  "Publication.part",

		// Thesis
		"field_degree_name":     "DegreeInfo.DegreeName",
		"field_degree_level":    "DegreeInfo.DegreeLevel",
		"field_department_name": "DegreeInfo.Department",

		// Publishing
		"field_publisher":       "Publisher",
		"field_place_published": "PlacePublished",

		// Miscellaneous
		"field_note":              "Notes",
		"field_table_of_contents": "TableOfContents",
		"field_source":            "Source",
		"field_digital_origin":    "DigitalOrigin",
	}
}

// workbenchRowToRecord converts a single CSV row into a hub Record.
func workbenchRowToRecord(row []string, header []string, colMap map[int]string, opts *format.ParseOptions) *hubv1.Record {
	record := &hubv1.Record{}

	for i, value := range row {
		if i >= len(header) {
			break
		}

		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		irField, ok := colMap[i]
		if !ok {
			continue
		}

		parts := strings.SplitN(irField, ".", 2)
		base := parts[0]
		subtype := ""
		if len(parts) > 1 {
			subtype = parts[1]
		}

		switch base {
		case "Title":
			// field_full_title takes priority over title if both present
			if record.Title == "" || header[i] == "field_full_title" {
				record.Title = value
			}

		case "AltTitle":
			record.AltTitle = append(record.AltTitle, splitPipe(value)...)

		case "Abstract":
			// Workbench serializes abstract as attr0 JSON; accept both forms
			if text := extractAttrValue(value); text != "" {
				record.Abstract = text
			} else {
				record.Abstract = value
			}

		case "Description":
			if text := extractAttrValue(value); text != "" {
				record.Description = text
			} else {
				record.Description = value
			}

		case "Contributors":
			for _, entry := range splitPipe(value) {
				if c := parseWorkbenchLinkedAgent(entry); c != nil {
					record.Contributors = append(record.Contributors, c)
				}
			}

		case "Dates":
			dateType := workbenchDateType(subtype)
			for _, v := range splitPipe(value) {
				date, _ := helpers.ParseEDTF(v, dateType)
				if date.Year > 0 {
					record.Dates = append(record.Dates, date)
				}
			}

		case "ResourceType":
			record.ResourceType = islandoraModelToResourceType(value)

		case "Language":
			record.Language = value

		case "Rights":
			for _, v := range splitPipe(value) {
				record.Rights = append(record.Rights, hub.NewRightsFromURI(v))
			}

		case "Subjects":
			vocab := workbenchSubjectVocab(subtype)
			for _, v := range splitPipe(value) {
				record.Subjects = append(record.Subjects, &hubv1.Subject{
					Value:      v,
					Vocabulary: vocab,
				})
			}

		case "Genre":
			for _, v := range splitPipe(value) {
				record.Genres = append(record.Genres, &hubv1.Subject{
					Value:      v,
					Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE,
				})
			}

		case "Identifiers":
			idType := workbenchIdentifierType(subtype)
			for _, v := range splitPipe(value) {
				if id := parseWorkbenchIdentifier(v, idType); id != nil {
					record.Identifiers = append(record.Identifiers, id)
				}
			}

		case "PhysicalDesc":
			// Workbench serializes extent as attr0 JSON; accept both forms
			if record.PhysicalDesc == "" {
				if text := extractAttrValue(value); text != "" {
					record.PhysicalDesc = text
				} else {
					record.PhysicalDesc = value
				}
			}

		case "Publisher":
			record.Publisher = value

		case "PlacePublished":
			record.PlacePublished = value

		case "Relations":
			relType := hub.NormalizeRelationType(subtype)
			for _, v := range splitPipe(value) {
				record.Relations = append(record.Relations, &hubv1.Relation{
					Type:        relType,
					TargetTitle: v,
				})
			}

		case "Publication":
			if record.Publication == nil {
				record.Publication = &hubv1.PublicationDetails{}
			}
			parseWorkbenchPublicationField(record.Publication, subtype, value)

		case "Notes":
			for _, v := range splitPipe(value) {
				if text := extractAttrValue(v); text != "" {
					record.Notes = append(record.Notes, text)
				} else {
					record.Notes = append(record.Notes, v)
				}
			}

		case "TableOfContents":
			record.TableOfContents = value

		case "Source":
			record.Source = value

		case "DigitalOrigin":
			record.DigitalOrigin = value

		case "DegreeInfo":
			if record.DegreeInfo == nil {
				record.DegreeInfo = &hubv1.DegreeInfo{}
			}
			switch subtype {
			case "DegreeName":
				record.DegreeInfo.DegreeName = value
			case "DegreeLevel":
				record.DegreeInfo.DegreeLevel = value
			case "Department":
				record.DegreeInfo.Department = value
			case "Institution":
				record.DegreeInfo.Institution = value
			}

		case "Extra":
			hub.SetExtra(record, subtype, value)
		}
	}

	return record
}

// parseWorkbenchLinkedAgent parses an Islandora Workbench typed_relation string.
//
// Workbench format: "relators:cre:person:Name - Institution"
//
//	"relators:pbl:corporate_body:Org Name"
func parseWorkbenchLinkedAgent(s string) *hubv1.Contributor {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	if idx := strings.Index(s, ":corporate_body:"); idx >= 0 {
		roleCode := s[:idx]
		name := s[idx+len(":corporate_body:"):]
		c := &hubv1.Contributor{
			Name: name,
			Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION,
		}
		if roleCode != "" {
			c.RoleCode = roleCode
			c.Role = helpers.RelatorLabel(roleCode)
		}
		return c
	}

	if idx := strings.Index(s, ":person:"); idx >= 0 {
		roleCode := s[:idx]
		rest := s[idx+len(":person:"):]
		name, institution := splitNameInstitution(rest)
		c := &hubv1.Contributor{
			Name:       name,
			Type:       hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			ParsedName: helpers.ParseName(name),
		}
		if roleCode != "" {
			c.RoleCode = roleCode
			c.Role = helpers.RelatorLabel(roleCode)
		}
		if institution != "" {
			c.Affiliations = append(c.Affiliations, &hubv1.Affiliation{Name: institution})
		}
		return c
	}

	// No type marker — treat as a plain name
	return &hubv1.Contributor{
		Name:       s,
		Type:       hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		ParsedName: helpers.ParseName(s),
	}
}

// splitNameInstitution splits "Name - Institution" on the last " - " occurrence.
func splitNameInstitution(s string) (name, institution string) {
	if idx := strings.LastIndex(s, " - "); idx >= 0 {
		return s[:idx], s[idx+3:]
	}
	return s, ""
}

// parseWorkbenchIdentifier parses an identifier from Workbench attr0 JSON or a plain value.
func parseWorkbenchIdentifier(s string, defaultType hubv1.IdentifierType) *hubv1.Identifier {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	if strings.HasPrefix(s, "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err == nil {
			value, _ := obj["value"].(string)
			attr0, _ := obj["attr0"].(string)
			if value == "" {
				return nil
			}
			idType := workbenchIdentifierType(attr0)
			if idType == hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED {
				idType = defaultType
			}
			return hub.NewIdentifier(value, idType)
		}
	}

	return hub.NewIdentifier(s, defaultType)
}

// extractAttrValue returns the "value" field from a Workbench attr0 JSON object,
// or empty string if the input is not attr0 JSON.
func extractAttrValue(s string) string {
	if !strings.HasPrefix(strings.TrimSpace(s), "{") {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return ""
	}
	value, _ := obj["value"].(string)
	return value
}

// islandoraModelToResourceType maps an Islandora Models vocabulary term to a hub ResourceType.
func islandoraModelToResourceType(model string) *hubv1.ResourceType {
	switch model {
	case "Image":
		return &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE}
	case "Video":
		return &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO}
	case "Audio":
		return &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO}
	case "Collection":
		return &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION}
	case "Binary":
		return &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET}
	default:
		// "Digital Document" and anything unrecognised
		return &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE}
	}
}

// parseWorkbenchPublicationField sets publication fields from workbench column values.
func parseWorkbenchPublicationField(pub *hubv1.PublicationDetails, subtype, value string) {
	switch subtype {
	case "title":
		// field_related_item: {"title":"..."} or plain title
		if strings.HasPrefix(value, "{") {
			var obj map[string]any
			if err := json.Unmarshal([]byte(value), &obj); err == nil {
				if title, ok := obj["title"].(string); ok {
					pub.Title = title
					return
				}
			}
		}
		pub.Title = value

	case "part":
		// field_part_detail: {"number":"...","type":"volume|issue|page"}
		for _, v := range splitPipe(value) {
			if !strings.HasPrefix(v, "{") {
				continue
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(v), &obj); err != nil {
				continue
			}
			number, _ := obj["number"].(string)
			partType, _ := obj["type"].(string)
			switch partType {
			case "volume":
				pub.Volume = number
			case "issue":
				pub.Issue = number
			case "page":
				pub.Pages = number
			}
		}
	}
}

// splitPipe splits a workbench multi-value field on "|".
func splitPipe(value string) []string {
	parts := strings.Split(value, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func workbenchDateType(s string) hubv1.DateType {
	switch strings.ToLower(s) {
	case "issued":
		return hubv1.DateType_DATE_TYPE_ISSUED
	case "created":
		return hubv1.DateType_DATE_TYPE_CREATED
	case "captured":
		return hubv1.DateType_DATE_TYPE_CAPTURED
	case "copyright":
		return hubv1.DateType_DATE_TYPE_COPYRIGHT
	case "modified":
		return hubv1.DateType_DATE_TYPE_MODIFIED
	default:
		return hubv1.DateType_DATE_TYPE_ISSUED
	}
}

func workbenchSubjectVocab(s string) hubv1.SubjectVocabulary {
	switch strings.ToLower(s) {
	case "lcsh":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH
	case "local":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL
	case "keywords":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS
	case "aat":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT
	case "fast":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_FAST
	default:
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
	}
}

func workbenchIdentifierType(s string) hubv1.IdentifierType {
	switch strings.ToLower(s) {
	case "doi":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
	case "hdl", "handle":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE
	case "isbn":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN
	case "issn":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN
	case "uri", "url":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_URL
	case "local":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
	case "pid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_PID
	default:
		return hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED
	}
}
