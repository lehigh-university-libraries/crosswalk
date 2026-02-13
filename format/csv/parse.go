package csv

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

// Parse reads CSV and returns hub records.
func (f *Format) Parse(r io.Reader, opts *format.ParseOptions) ([]*hubv1.Record, error) {
	if opts == nil {
		opts = format.NewParseOptions()
	}

	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // Allow variable number of fields
	reader.LazyQuotes = true

	// Read all rows
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing CSV: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	// First row is header
	header := rows[0]
	columnMap := buildColumnMap(header, opts.Profile)

	// Get multi-value separator
	sep := "|"
	if opts.Profile != nil {
		sep = opts.Profile.GetMultiValueSeparator()
	}

	// Parse data rows
	records := make([]*hubv1.Record, 0, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		record, err := rowToRecord(rows[i], header, columnMap, sep, opts)
		if err != nil {
			continue // Skip invalid rows
		}
		records = append(records, record)
	}

	return records, nil
}

func buildColumnMap(header []string, profile *mapping.Profile) map[int]string {
	colMap := make(map[int]string)

	for i, col := range header {
		col = strings.ToLower(strings.TrimSpace(col))

		// Try profile mapping first
		if profile != nil {
			if m, ok := profile.Fields[col]; ok {
				colMap[i] = m.IR
				continue
			}
		}

		// Default mappings
		defaultMap := map[string]string{
			"title":             "Title",
			"alt_title":         "AltTitle",
			"alternative_title": "AltTitle",
			"contributors":      "Contributors",
			"authors":           "Contributors",
			"author":            "Contributors",
			"creator":           "Contributors",
			"date_issued":       "Dates.issued",
			"date_created":      "Dates.created",
			"date":              "Dates.issued",
			"year":              "Dates.issued",
			"resource_type":     "ResourceType",
			"type":              "ResourceType",
			"genre":             "Genre",
			"language":          "Language",
			"lang":              "Language",
			"rights":            "Rights",
			"license":           "Rights",
			"abstract":          "Abstract",
			"description":       "Description",
			"identifiers":       "Identifiers",
			"identifier":        "Identifiers",
			"doi":               "Identifiers.doi",
			"subjects":          "Subjects",
			"subject":           "Subjects",
			"keywords":          "Subjects.keywords",
			"keyword":           "Subjects.keywords",
			"publisher":         "Publisher",
			"place_published":   "PlacePublished",
			"publication_place": "PlacePublished",
			"member_of":         "Relations.member_of",
			"collection":        "Relations.member_of",
			"degree_name":       "DegreeInfo.DegreeName",
			"degree_level":      "DegreeInfo.DegreeLevel",
			"department":        "DegreeInfo.Department",
			"institution":       "DegreeInfo.Institution",
			"notes":             "Notes",
			"note":              "Notes",
			"nid":               "Extra.nid",
			"uuid":              "Extra.uuid",
		}

		if ir, ok := defaultMap[col]; ok {
			colMap[i] = ir
		}
	}

	return colMap
}

func rowToRecord(row []string, header []string, colMap map[int]string, sep string, opts *format.ParseOptions) (*hubv1.Record, error) {
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
			record.Title = value

		case "AltTitle":
			record.AltTitle = splitMultiValue(value, sep)

		case "Abstract":
			record.Abstract = cleanValue(value, opts)

		case "Description":
			record.Description = cleanValue(value, opts)

		case "Contributors":
			// Contributors always use " ; " as multi-value separator to match serialization
			entries := splitMultiValue(value, " ; ")
			for _, entry := range entries {
				if c := parseContributor(entry); c != nil {
					record.Contributors = append(record.Contributors, c)
				}
			}

		case "Dates":
			dateType := dateTypeFromString(subtype)
			for _, v := range splitMultiValue(value, sep) {
				date, _ := helpers.ParseEDTF(v, dateType)
				if date.Year > 0 {
					record.Dates = append(record.Dates, date)
				}
			}

		case "ResourceType":
			record.ResourceType = hub.NewResourceType(value, "")

		case "Genre":
			for _, g := range splitMultiValue(value, sep) {
				record.Genres = append(record.Genres, &hubv1.Subject{Value: g, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE})
			}

		case "Language":
			record.Language = value

		case "Rights":
			for _, v := range splitMultiValue(value, sep) {
				record.Rights = append(record.Rights, hub.NewRightsFromURI(v))
			}

		case "Subjects":
			vocab := subjectVocabularyFromString(subtype)
			for _, v := range splitMultiValue(value, sep) {
				record.Subjects = append(record.Subjects, &hubv1.Subject{
					Value:      v,
					Vocabulary: vocab,
				})
			}

		case "Identifiers":
			idType := identifierTypeFromString(subtype)
			for _, v := range splitMultiValue(value, sep) {
				record.Identifiers = append(record.Identifiers, hub.NewIdentifier(v, idType))
			}

		case "Publisher":
			record.Publisher = value

		case "PlacePublished":
			record.PlacePublished = value

		case "Relations":
			relType := hub.NormalizeRelationType(subtype)
			for _, v := range splitMultiValue(value, sep) {
				record.Relations = append(record.Relations, &hubv1.Relation{
					Type:        relType,
					TargetTitle: v,
				})
			}

		case "Notes":
			record.Notes = append(record.Notes, splitMultiValue(value, sep)...)

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

	return record, nil
}

func dateTypeFromString(s string) hubv1.DateType {
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
	case "available":
		return hubv1.DateType_DATE_TYPE_AVAILABLE
	case "submitted":
		return hubv1.DateType_DATE_TYPE_SUBMITTED
	case "accepted":
		return hubv1.DateType_DATE_TYPE_ACCEPTED
	case "published":
		return hubv1.DateType_DATE_TYPE_PUBLISHED
	default:
		return hubv1.DateType_DATE_TYPE_ISSUED
	}
}

func subjectVocabularyFromString(s string) hubv1.SubjectVocabulary {
	switch strings.ToLower(s) {
	case "lcsh":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH
	case "mesh":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH
	case "aat":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT
	case "fast":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_FAST
	case "ddc":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_DDC
	case "lcc":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCC
	case "keywords":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS
	case "genre":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE
	case "local":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL
	default:
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
	}
}

func identifierTypeFromString(s string) hubv1.IdentifierType {
	switch strings.ToLower(s) {
	case "doi":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
	case "url":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_URL
	case "handle":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE
	case "isbn":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN
	case "issn":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN
	case "orcid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID
	case "pmid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_PMID
	case "pmcid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID
	case "arxiv":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV
	case "local":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
	case "pid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_PID
	case "nid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_NID
	case "uuid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_UUID
	case "isni":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI
	default:
		return hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED
	}
}

func splitMultiValue(value string, sep string) []string {
	if sep == "" {
		return []string{value}
	}
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

func cleanValue(value string, opts *format.ParseOptions) string {
	if opts.StripHTML {
		return helpers.CleanText(value)
	}
	return value
}

// parseContributor parses a contributor from either JSON format or a plain prefixed string.
//
// JSON format: {"name":"relators:cre:person:Qin, Tian","institution":"...","orcid":"..."}
// Plain format: "relators:cre:person:Qin, Tian" (Islandora workbench style)
// Simple format: "Qin, Tian"
func parseContributor(s string) *hubv1.Contributor {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	if strings.HasPrefix(s, "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err == nil {
			return parseContributorFromJSON(obj)
		}
	}

	return parseContributorFromString(s)
}

// parseContributorFromString parses a plain or prefixed contributor string.
// Prefixed format: "relators:cre:person:Qin, Tian" (role_code:type:name)
func parseContributorFromString(s string) *hubv1.Contributor {
	roleCode, contribType, name := parseNamePrefix(s)
	c := &hubv1.Contributor{
		Name:       name,
		Type:       contribType,
		ParsedName: helpers.ParseName(name),
	}
	if roleCode != "" {
		c.RoleCode = roleCode
		c.Role = helpers.RelatorLabel(roleCode)
	}
	return c
}

// parseContributorFromJSON builds a Contributor from a decoded JSON object.
func parseContributorFromJSON(obj map[string]any) *hubv1.Contributor {
	c := &hubv1.Contributor{}

	if nameVal, ok := obj["name"].(string); ok {
		roleCode, contribType, name := parseNamePrefix(nameVal)
		c.Name = name
		c.Type = contribType
		c.ParsedName = helpers.ParseName(name)
		if roleCode != "" {
			c.RoleCode = roleCode
			c.Role = helpers.RelatorLabel(roleCode)
		}
	}

	if institution, ok := obj["institution"].(string); ok && institution != "" {
		c.Affiliations = append(c.Affiliations, &hubv1.Affiliation{Name: institution})
	}

	if orcid, ok := obj["orcid"].(string); ok && orcid != "" {
		c.Identifiers = append(c.Identifiers, hub.NewIdentifier(orcid, hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID))
	}

	if email, ok := obj["email"].(string); ok && email != "" {
		c.Email = email
	}

	if status, ok := obj["status"].(string); ok && status != "" {
		c.Status = status
	}

	if url, ok := obj["url"].(string); ok && url != "" {
		c.Url = url
	}

	if additionalName, ok := obj["additional_name"].(string); ok && additionalName != "" {
		c.AdditionalName = additionalName
	}

	if authorityURI, ok := obj["authority_uri"].(string); ok && authorityURI != "" {
		c.AuthorityUri = authorityURI
	}

	if authoritySource, ok := obj["authority_source"].(string); ok && authoritySource != "" {
		c.AuthoritySource = authoritySource
	}

	if alumniOf, ok := obj["alumni_of"].([]any); ok {
		for _, a := range alumniOf {
			if s, ok := a.(string); ok && s != "" {
				c.AlumniOf = append(c.AlumniOf, s)
			}
		}
	}

	return c
}

// parseNamePrefix extracts role code, contributor type, and name from a prefixed string.
// Format: "[roleCode:]type:name" where type is "person" or "organization".
// Example: "relators:cre:person:Qin, Tian" → ("relators:cre", PERSON, "Qin, Tian")
func parseNamePrefix(s string) (roleCode string, contribType hubv1.ContributorType, name string) {
	contribType = hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON

	for _, keyword := range []string{"person", "organization"} {
		// Look for ":keyword:" in the middle of the string
		marker := ":" + keyword + ":"
		if idx := strings.Index(s, marker); idx >= 0 {
			roleCode = s[:idx]
			name = s[idx+len(marker):]
			if keyword == "organization" {
				contribType = hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
			}
			return
		}
		// Look for "keyword:" at the start (no role prefix)
		if strings.HasPrefix(s, keyword+":") {
			name = s[len(keyword)+1:]
			if keyword == "organization" {
				contribType = hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
			}
			return
		}
	}

	// No type prefix found — treat the whole string as the name
	name = s
	return
}
