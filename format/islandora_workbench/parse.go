package islandora_workbench

import (
	"encoding/csv"
	"encoding/json"
	"errors"
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

	limited := &io.LimitedReader{R: r, N: maxWorkbenchInputBytes + 1}
	reader := csv.NewReader(limited)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = false

	rows, err := readBoundedWorkbenchRows(reader)
	if err != nil {
		var parseError *csv.ParseError
		if errors.As(err, &parseError) {
			return nil, &format.DiagnosticsError{Diagnostics: []format.Diagnostic{{
				Source:  opts.SourceName,
				Row:     parseError.Line,
				Column:  parseError.Column,
				Code:    "invalid_csv",
				Message: parseError.Err.Error(),
			}}}
		}
		return nil, fmt.Errorf("parsing workbench CSV: %w", err)
	}
	if limited.N == 0 {
		return nil, fmt.Errorf("Workbench input exceeds %d bytes", maxWorkbenchInputBytes)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	header := rows[0]
	colMap := buildWorkbenchColumnMap(header, opts.Profile)
	diagnostics := make([]format.Diagnostic, 0)
	if opts.Strict {
		for index, name := range header {
			if _, ok := colMap[index]; !ok && strings.TrimSpace(name) != "" {
				diagnostics = append(diagnostics, format.Diagnostic{
					Source:  opts.SourceName,
					Row:     1,
					Column:  index + 1,
					Header:  strings.TrimSpace(name),
					Code:    "unknown_column",
					Message: "column has no Workbench-to-Hub mapping",
				})
			}
		}
	}

	records := make([]*hubv1.Record, 0, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		if blankWorkbenchRow(rows[i]) {
			continue
		}
		if len(rows[i]) != len(header) {
			diagnostics = append(diagnostics, format.Diagnostic{
				Source:  opts.SourceName,
				Row:     i + 1,
				Code:    "column_count",
				Message: fmt.Sprintf("got %d columns; header defines %d", len(rows[i]), len(header)),
			})
			continue
		}
		record, rowDiagnostics := workbenchRowToRecord(rows[i], i+1, header, colMap, opts)
		diagnostics = append(diagnostics, rowDiagnostics...)
		records = append(records, record)
	}
	if len(diagnostics) > 0 {
		return nil, &format.DiagnosticsError{Diagnostics: diagnostics}
	}

	return records, nil
}

const (
	maxWorkbenchInputBytes = int64(64 << 20)
	maxWorkbenchRows       = 100_001
	maxWorkbenchCells      = int64(1_000_000)
)

func readBoundedWorkbenchRows(reader *csv.Reader) ([][]string, error) {
	rows := make([][]string, 0)
	var cells int64
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		if len(rows) >= maxWorkbenchRows {
			return nil, fmt.Errorf("Workbench row count exceeds %d", maxWorkbenchRows)
		}
		if int64(len(row)) > maxWorkbenchCells-cells {
			return nil, fmt.Errorf("Workbench cell count exceeds %d", maxWorkbenchCells)
		}
		cells += int64(len(row))
		rows = append(rows, row)
	}
}

func blankWorkbenchRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
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
		"id":                "Extra.id",
		"parent_id":         "Extra.parent_id",
		"node_id":           "Extra.node_id",
		"field_weight":      "Extra.field_weight",
		"file":              "Files.primary",
		"supplemental_file": "Files.supplemental",
		"url_alias":         "Extra.url_alias",
		"image_alt_text":    "Extra.image_alt_text",
		"checksum":          "Extra.checksum",
		"media_use_tid":     "Extra.media_use_tid",

		// Core
		"title":               "Title",
		"field_full_title":    "FullTitle",
		"field_alt_title":     "AltTitle",
		"field_add_coverpage": "AddCoverpage",
		"published":           "IsPublic",

		// Contributors
		"field_linked_agent": "Contributors",

		// Dates (EDTF)
		"field_edtf_date_issued":   "Dates.issued",
		"field_edtf_date_created":  "Dates.created",
		"field_edtf_date_captured": "Dates.captured",
		"field_edtf_date_embargo":  "Dates.available",
		"field_date_season":        "Extra.date_season",
		"field_copyright_date":     "Dates.copyright",
		"field_date_modified":      "Dates.modified",

		// Resource type and model
		"field_model":         "ObjectModel",
		"field_resource_type": "ResourceType",

		// Language
		"field_language":        "Language",
		"field_department_name": "Departments",

		// Rights
		"field_rights": "Rights",

		// Descriptions
		"field_abstract":             "Abstract",
		"field_description":          "Description",
		"field_physical_description": "PhysicalDesc",
		"field_extent":               "PhysicalDesc",

		// Subjects
		"field_subject":                  "Subjects",
		"field_lcsh_topic":               "Subjects.lcsh",
		"field_subject_lcsh":             "Subjects.lcsh",
		"field_subject_general":          "Subjects.local",
		"field_keywords":                 "Subjects.keywords",
		"field_subjects_name":            "Subjects.lcnaf",
		"field_geographic_subject":       "Subjects.geographic",
		"field_subject_hierarchical_geo": "Subjects.getty_tgn",

		// Genre
		"field_genre":         "Genre",
		"field_physical_form": "PhysicalForm",
		"field_media_type":    "FileMetadata.mime_type",

		// Identifiers
		"field_identifier": "Identifiers",
		"field_pid":        "Identifiers.pid",

		// Relations
		"field_member_of":    "Relations.member_of",
		"field_related_item": "Publication.title",
		"field_part_detail":  "Publication.part",

		// Thesis
		"field_degree_name":  "DegreeInfo.DegreeName",
		"field_degree_level": "DegreeInfo.DegreeLevel",

		// Publishing
		"field_publisher":       "Publisher",
		"field_place_published": "PlacePublished",
		"field_edition":         "Edition",

		// Miscellaneous
		"field_note":              "Notes",
		"field_table_of_contents": "TableOfContents",
		"field_source":            "Source",
		"field_digital_origin":    "DigitalOrigin",
		"field_local_restriction": "LocalRestriction",
		"field_access":            "AccessCondition",
	}
}

// workbenchRowToRecord converts a single CSV row into a hub Record.
func workbenchRowToRecord(row []string, rowNumber int, header []string, colMap map[int]string, opts *format.ParseOptions) (*hubv1.Record, []format.Diagnostic) {
	record := &hubv1.Record{}
	diagnostics := make([]format.Diagnostic, 0)

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
			record.Title = value

		case "FullTitle":
			record.FullTitle = value

		case "AddCoverpage", "IsPublic":
			parsed, ok := parseWorkbenchBoolean(value)
			if !ok {
				diagnostics = append(diagnostics, format.Diagnostic{
					Source:  opts.SourceName,
					Row:     rowNumber,
					Column:  i + 1,
					Header:  header[i],
					Code:    "invalid_boolean",
					Message: fmt.Sprintf("must be yes/no or 1/0: %q", value),
				})
				continue
			}
			if base == "AddCoverpage" {
				record.AddCoverpage = parsed
				hub.SetExtra(record, "_present_add_coverpage", true)
			} else {
				record.IsPublic = parsed
				hub.SetExtra(record, "_present_is_public", true)
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
				date, err := helpers.ParseEDTF(v, dateType)
				if date.Year > 0 {
					record.Dates = append(record.Dates, date)
				} else {
					message := fmt.Sprintf("must be a valid EDTF date: %q", v)
					if err != nil {
						message = err.Error()
					}
					diagnostics = append(diagnostics, format.Diagnostic{
						Source:  opts.SourceName,
						Row:     rowNumber,
						Column:  i + 1,
						Header:  header[i],
						Code:    "invalid_date",
						Message: message,
					})
				}
			}

		case "ResourceType":
			record.ResourceType = hub.NewResourceType(value, "")

		case "ObjectModel":
			record.ObjectModel = value

		case "Language":
			record.Language = value

		case "Departments":
			record.Departments = append(record.Departments, splitPipe(value)...)

		case "Rights":
			for _, v := range splitPipe(value) {
				record.Rights = append(record.Rights, hub.NewRightsFromURI(v))
			}

		case "Subjects":
			vocab := workbenchSubjectVocab(subtype)
			for _, v := range splitPipe(value) {
				subjectType := hubv1.SubjectType_SUBJECT_TYPE_TOPIC
				if subtype == "lcnaf" {
					subjectType = hubv1.SubjectType_SUBJECT_TYPE_NAME
				}
				if subtype == "geographic" {
					subjectType = hubv1.SubjectType_SUBJECT_TYPE_GEOGRAPHIC
					switch {
					case strings.HasPrefix(v, "geographic_naf:"):
						v = strings.TrimPrefix(v, "geographic_naf:")
						vocab = hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCNAF
					case strings.HasPrefix(v, "geographic_local:"):
						v = strings.TrimPrefix(v, "geographic_local:")
						vocab = hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL
					}
				}
				subject := &hubv1.Subject{Value: v, Vocabulary: vocab, Type: subjectType}
				if subtype == "getty_tgn" && (strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://")) {
					subject.Uri = v
					subject.Type = hubv1.SubjectType_SUBJECT_TYPE_GEOGRAPHIC
				}
				record.Subjects = append(record.Subjects, subject)
			}

		case "Genre":
			for _, v := range splitPipe(value) {
				record.Genres = append(record.Genres, &hubv1.Subject{
					Value:      v,
					Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE,
				})
			}

		case "PhysicalForm":
			for _, v := range splitPipe(value) {
				record.PhysicalForm = append(record.PhysicalForm, &hubv1.Subject{
					Value:      v,
					Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT,
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

		case "Edition":
			record.Edition = value

		case "Files":
			for _, path := range splitPipe(value) {
				if subtype == "primary" {
					file := parsedPrimaryFile(record)
					if file.Path == "" {
						file.Path = path
						continue
					}
				}
				record.Files = append(record.Files, &hubv1.File{Path: path, Role: subtype})
			}

		case "FileMetadata":
			file := parsedPrimaryFile(record)
			if subtype == "mime_type" {
				file.MimeType = value
			}

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

		case "LocalRestriction":
			record.LocalRestriction = value

		case "AccessCondition":
			record.AccessCondition = value

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

	if record.ResourceType == nil && record.ObjectModel != "" {
		record.ResourceType = islandoraModelToResourceType(record.ObjectModel)
	}
	return record, diagnostics
}

func parsedPrimaryFile(record *hubv1.Record) *hubv1.File {
	for _, file := range record.Files {
		if file != nil && (file.Role == "" || file.Role == "primary") {
			return file
		}
	}
	file := &hubv1.File{Role: "primary"}
	record.Files = append(record.Files, file)
	return file
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
		// field_related_item can contain title and ISSN JSON values.
		for _, item := range splitPipe(value) {
			if !strings.HasPrefix(item, "{") {
				if pub.Title == "" {
					pub.Title = item
				}
				continue
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(item), &obj); err == nil {
				if title, ok := obj["title"].(string); ok {
					pub.Title = title
				}
				if identifierType, _ := obj["type"].(string); identifierType == "issn" {
					pub.LIssn, _ = obj["identifier"].(string)
				}
			}
		}

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

func parseWorkbenchBoolean(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "yes", "y", "true":
		return true, true
	case "0", "no", "n", "false":
		return false, true
	default:
		return false, false
	}
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
	case "available", "embargo":
		return hubv1.DateType_DATE_TYPE_AVAILABLE
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
	case "lcnaf", "geographic":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCNAF
	case "getty_tgn":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GETTY_TGN
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
	case "call-number", "call_number":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER
	case "report-number", "report_number":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER
	case "arxiv":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV
	case "wos", "web-of-science", "web_of_science", "ut":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_WOS
	case "pmid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_PMID
	case "pmcid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID
	case "uuid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_UUID
	default:
		return hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED
	}
}
