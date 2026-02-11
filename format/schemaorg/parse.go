package schemaorg

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// Parse reads schema.org JSON-LD and returns hub records.
func (f *Format) Parse(r io.Reader, _ *format.ParseOptions) ([]*hubv1.Record, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	data = trimBOM(data)
	data = skipWhitespace(data)

	if len(data) == 0 {
		return nil, nil
	}

	var records []*hubv1.Record

	// Parse based on first character
	switch data[0] {
	case '[':
		var docs []map[string]any
		if err := json.Unmarshal(data, &docs); err != nil {
			return nil, fmt.Errorf("parsing JSON array: %w", err)
		}
		for i, doc := range docs {
			record, err := schemaOrgToRecord(doc)
			if err != nil {
				return nil, fmt.Errorf("converting document %d: %w", i, err)
			}
			records = append(records, record)
		}
	case '{':
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parsing JSON object: %w", err)
		}
		record, err := schemaOrgToRecord(doc)
		if err != nil {
			return nil, fmt.Errorf("converting document: %w", err)
		}
		records = append(records, record)
	default:
		return nil, fmt.Errorf("invalid JSON: expected { or [")
	}

	return records, nil
}

func trimBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func skipWhitespace(data []byte) []byte {
	for len(data) > 0 && (data[0] == ' ' || data[0] == '\t' || data[0] == '\n' || data[0] == '\r') {
		data = data[1:]
	}
	return data
}

// schemaOrgToRecord converts a schema.org JSON-LD document to a hub record.
func schemaOrgToRecord(doc map[string]any) (*hubv1.Record, error) {
	record := &hubv1.Record{}

	// Get @type
	schemaType := getString(doc, "@type")
	record.ResourceType = mapSchemaTypeToResourceType(schemaType)

	// Core properties
	if name := getString(doc, "name"); name != "" {
		record.Title = name
	} else if headline := getString(doc, "headline"); headline != "" {
		record.Title = headline
	}

	if altTitle := getString(doc, "alternativeHeadline"); altTitle != "" {
		record.AltTitle = []string{altTitle}
	}

	if abstract := getString(doc, "abstract"); abstract != "" {
		record.Abstract = abstract
	}

	if desc := getString(doc, "description"); desc != "" {
		record.Description = desc
	}

	// Contributors
	record.Contributors = append(record.Contributors, parseContributors(doc, "author", "author")...)
	record.Contributors = append(record.Contributors, parseContributors(doc, "creator", "creator")...)
	record.Contributors = append(record.Contributors, parseContributors(doc, "editor", "editor")...)
	record.Contributors = append(record.Contributors, parseContributors(doc, "contributor", "contributor")...)

	// Publisher
	if pub := doc["publisher"]; pub != nil {
		switch p := pub.(type) {
		case string:
			record.Publisher = p
		case map[string]any:
			record.Publisher = getString(p, "name")
		}
	}

	// Dates
	if datePublished := getString(doc, "datePublished"); datePublished != "" {
		record.Dates = append(record.Dates, parseDate(datePublished, hubv1.DateType_DATE_TYPE_PUBLISHED))
	}
	if dateCreated := getString(doc, "dateCreated"); dateCreated != "" {
		record.Dates = append(record.Dates, parseDate(dateCreated, hubv1.DateType_DATE_TYPE_CREATED))
	}
	if dateModified := getString(doc, "dateModified"); dateModified != "" {
		record.Dates = append(record.Dates, parseDate(dateModified, hubv1.DateType_DATE_TYPE_MODIFIED))
	}

	// Language
	if lang := doc["inLanguage"]; lang != nil {
		switch l := lang.(type) {
		case string:
			record.Language = l
		case map[string]any:
			record.Language = getString(l, "name")
			if record.Language == "" {
				record.Language = getString(l, "alternateName")
			}
		}
	}

	// Genre
	if genre := doc["genre"]; genre != nil {
		switch g := genre.(type) {
		case string:
			record.Genres = []*hubv1.Subject{{Value: g, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE}}
		case []any:
			for _, item := range g {
				if s, ok := item.(string); ok {
					record.Genres = append(record.Genres, &hubv1.Subject{Value: s, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE})
				}
			}
		}
	}

	// Keywords/Subjects
	if keywords := doc["keywords"]; keywords != nil {
		switch k := keywords.(type) {
		case string:
			// Split comma-separated keywords
			for _, kw := range strings.Split(k, ",") {
				kw = strings.TrimSpace(kw)
				if kw != "" {
					record.Subjects = append(record.Subjects, &hubv1.Subject{
						Value:      kw,
						Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS,
					})
				}
			}
		case []any:
			for _, item := range k {
				if s, ok := item.(string); ok {
					record.Subjects = append(record.Subjects, &hubv1.Subject{
						Value:      s,
						Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS,
					})
				}
			}
		}
	}

	// Rights/License
	if license := doc["license"]; license != nil {
		rights := &hubv1.Rights{}
		switch l := license.(type) {
		case string:
			if strings.HasPrefix(l, "http") {
				rights.Uri = l
			} else {
				rights.Statement = l
			}
		case map[string]any:
			rights.Uri = getString(l, "url")
			rights.Statement = getString(l, "name")
		}
		if rights.Uri != "" || rights.Statement != "" {
			record.Rights = append(record.Rights, rights)
		}
	}

	// Identifiers
	if url := getString(doc, "url"); url != "" {
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_URL,
			Value: url,
		})
	}

	if sameAs := doc["sameAs"]; sameAs != nil {
		switch s := sameAs.(type) {
		case string:
			record.Identifiers = append(record.Identifiers, parseIdentifierFromURL(s))
		case []any:
			for _, item := range s {
				if str, ok := item.(string); ok {
					record.Identifiers = append(record.Identifiers, parseIdentifierFromURL(str))
				}
			}
		}
	}

	if id := doc["identifier"]; id != nil {
		record.Identifiers = append(record.Identifiers, parseIdentifiers(id)...)
	}

	// ISBN (for books)
	if isbn := getString(doc, "isbn"); isbn != "" {
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN,
			Value: isbn,
		})
	}

	// Relations
	if isPartOf := doc["isPartOf"]; isPartOf != nil {
		rel := parseRelation(isPartOf, hubv1.RelationType_RELATION_TYPE_PART_OF)
		if rel != nil {
			record.Relations = append(record.Relations, rel)
		}
	}

	// Physical description
	if pagination := getString(doc, "pagination"); pagination != "" {
		record.PhysicalDesc = pagination
	}

	// Notes
	if notes := doc["notes"]; notes != nil {
		switch n := notes.(type) {
		case string:
			record.Notes = []string{n}
		case []any:
			for _, item := range n {
				if s, ok := item.(string); ok {
					record.Notes = append(record.Notes, s)
				}
			}
		}
	}

	return record, nil
}

// mapSchemaTypeToResourceType converts schema.org @type to hub ResourceType.
func mapSchemaTypeToResourceType(schemaType string) *hubv1.ResourceType {
	rt := &hubv1.ResourceType{
		Original: schemaType,
	}

	switch schemaType {
	case "ScholarlyArticle", "Article":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE
	case "Book":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK
	case "Chapter":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER
	case "Dataset":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET
	case "Collection":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION
	case "DigitalDocument", "Thesis":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS
	case "Manuscript":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_MANUSCRIPT
	case "AudioObject":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO
	case "ImageObject", "Photograph":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE
	case "VideoObject":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO
	case "PublicationIssue":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_PERIODICAL
	case "PublicationVolume", "Periodical":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_JOURNAL
	case "Report":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT
	case "SoftwareSourceCode", "SoftwareApplication":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE
	case "WebPage":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_WEBPAGE
	case "Map":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_MAP
	case "Presentation", "PresentationDigitalDocument":
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_PRESENTATION
	default:
		rt.Type = hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER
	}

	return rt
}

// parseContributors extracts contributors from a schema.org property.
func parseContributors(doc map[string]any, key, role string) []*hubv1.Contributor {
	val, ok := doc[key]
	if !ok || val == nil {
		return nil
	}

	var contributors []*hubv1.Contributor

	switch v := val.(type) {
	case string:
		contributors = append(contributors, &hubv1.Contributor{
			Name: v,
			Role: role,
			Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		})
	case map[string]any:
		contributors = append(contributors, parsePersonOrOrg(v, role))
	case []any:
		for _, item := range v {
			switch i := item.(type) {
			case string:
				contributors = append(contributors, &hubv1.Contributor{
					Name: i,
					Role: role,
					Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
				})
			case map[string]any:
				contributors = append(contributors, parsePersonOrOrg(i, role))
			}
		}
	}

	return contributors
}

// parsePersonOrOrg parses a Person or Organization object.
func parsePersonOrOrg(obj map[string]any, role string) *hubv1.Contributor {
	contrib := &hubv1.Contributor{
		Role: role,
	}

	objType := getString(obj, "@type")
	if objType == "Organization" {
		contrib.Type = hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
		contrib.Name = getString(obj, "name")
		if contrib.Name == "" {
			contrib.Name = getString(obj, "legalName")
		}
	} else {
		contrib.Type = hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON
		contrib.Name = getString(obj, "name")

		// Parse name components
		given := getString(obj, "givenName")
		family := getString(obj, "familyName")
		if given != "" || family != "" {
			contrib.ParsedName = &hubv1.ParsedName{
				Given:    given,
				Family:   family,
				Middle:   getString(obj, "additionalName"),
				Prefix:   getString(obj, "honorificPrefix"),
				Suffix:   getString(obj, "honorificSuffix"),
				FullName: contrib.Name,
			}
			// Build normalized name if we have components
			if family != "" && given != "" {
				contrib.ParsedName.Normalized = family + ", " + given
			}
		}
	}

	// Affiliation
	if aff := obj["affiliation"]; aff != nil {
		switch a := aff.(type) {
		case string:
			contrib.Affiliation = a
		case map[string]any:
			contrib.Affiliation = getString(a, "name")
		}
	}

	// ORCID from sameAs
	if sameAs := obj["sameAs"]; sameAs != nil {
		switch s := sameAs.(type) {
		case string:
			if strings.Contains(s, "orcid.org") {
				contrib.Identifiers = append(contrib.Identifiers, &hubv1.Identifier{
					Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID,
					Value: extractOrcid(s),
				})
			}
		case []any:
			for _, item := range s {
				if str, ok := item.(string); ok && strings.Contains(str, "orcid.org") {
					contrib.Identifiers = append(contrib.Identifiers, &hubv1.Identifier{
						Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID,
						Value: extractOrcid(str),
					})
				}
			}
		}
	}

	return contrib
}

// parseDate parses a date string to hub DateValue.
func parseDate(dateStr string, dateType hubv1.DateType) *hubv1.DateValue {
	dv := &hubv1.DateValue{
		Type: dateType,
		Raw:  dateStr,
	}

	// Parse ISO 8601 date
	parts := strings.Split(dateStr, "-")
	if len(parts) >= 1 {
		if year, err := strconv.Atoi(parts[0]); err == nil {
			dv.Year = int32(year)
		}
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_YEAR
	}
	if len(parts) >= 2 {
		if month, err := strconv.Atoi(parts[1]); err == nil {
			dv.Month = int32(month)
		}
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
	}
	if len(parts) >= 3 {
		// Handle time component
		dayPart := strings.Split(parts[2], "T")[0]
		if day, err := strconv.Atoi(dayPart); err == nil {
			dv.Day = int32(day)
		}
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
	}

	return dv
}

// parseIdentifierFromURL extracts identifier type from URL.
func parseIdentifierFromURL(url string) *hubv1.Identifier {
	id := &hubv1.Identifier{Value: url}

	switch {
	case strings.Contains(url, "doi.org"):
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
		// Extract DOI from URL
		if idx := strings.Index(url, "doi.org/"); idx >= 0 {
			id.Value = url[idx+8:]
		}
	case strings.Contains(url, "orcid.org"):
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID
		id.Value = extractOrcid(url)
	case strings.Contains(url, "isni.org"):
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI
		if idx := strings.Index(url, "isni/"); idx >= 0 {
			id.Value = url[idx+5:]
		}
	case strings.Contains(url, "handle.net"):
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE
	default:
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_URL
	}

	return id
}

// parseIdentifiers parses schema.org identifier property.
func parseIdentifiers(val any) []*hubv1.Identifier {
	var ids []*hubv1.Identifier

	switch v := val.(type) {
	case string:
		ids = append(ids, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
			Value: v,
		})
	case map[string]any:
		ids = append(ids, parsePropertyValue(v))
	case []any:
		for _, item := range v {
			switch i := item.(type) {
			case string:
				ids = append(ids, &hubv1.Identifier{
					Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
					Value: i,
				})
			case map[string]any:
				ids = append(ids, parsePropertyValue(i))
			}
		}
	}

	return ids
}

// parsePropertyValue parses a PropertyValue object to Identifier.
func parsePropertyValue(pv map[string]any) *hubv1.Identifier {
	id := &hubv1.Identifier{
		Value: getString(pv, "value"),
	}

	propID := strings.ToLower(getString(pv, "propertyID"))
	switch propID {
	case "doi":
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
	case "isbn":
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN
	case "issn":
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN
	case "handle":
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE
	case "pmid":
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_PMID
	case "pmcid":
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID
	case "arxiv":
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV
	default:
		id.Type = hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
	}

	return id
}

// parseRelation parses isPartOf/hasPart to hub Relation.
func parseRelation(val any, relType hubv1.RelationType) *hubv1.Relation {
	rel := &hubv1.Relation{Type: relType}

	switch v := val.(type) {
	case string:
		if strings.HasPrefix(v, "http") {
			rel.TargetUri = v
		} else {
			rel.TargetTitle = v
		}
	case map[string]any:
		rel.TargetTitle = getString(v, "name")
		rel.TargetUri = getString(v, "url")
		if id := getString(v, "@id"); id != "" && rel.TargetUri == "" {
			rel.TargetUri = id
		}
	}

	if rel.TargetTitle == "" && rel.TargetUri == "" {
		return nil
	}

	return rel
}

// extractOrcid extracts ORCID from URL.
func extractOrcid(url string) string {
	// Handle various ORCID URL formats
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "orcid.org/")
	return url
}

// getString safely extracts a string from a map.
func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
