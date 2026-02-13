package arxiv

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

// Parse reads arXiv metadata XML and returns hub records.
// Handles three arXiv XML variants:
//   - arXivRecord (XSD 1.0 schema)
//   - OAI-PMH arXiv format (http://arxiv.org/OAI/arXiv/)
//   - Atom API format (http://arxiv.org/schemas/atom)
func (f *Format) Parse(r io.Reader, _ *format.ParseOptions) ([]*hubv1.Record, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	// Detect which variant and dispatch
	if bytes.Contains(data, []byte("http://arxiv.org/schemas/atom")) ||
		(bytes.Contains(data, []byte("<feed")) && bytes.Contains(data, []byte("<entry"))) {
		return parseAtom(data)
	}

	if bytes.Contains(data, []byte("http://arxiv.org/OAI/arXiv/")) {
		return parseOAI(data)
	}

	return parseXSDRecord(data)
}

// ---------------------------------------------------------------------------
// Atom API format: <feed>/<entry> with <author><name>, <category>, <summary>
// ---------------------------------------------------------------------------

// XMLAtomFeed represents the Atom API response.
type XMLAtomFeed struct {
	XMLName xml.Name       `xml:"feed"`
	Entries []XMLAtomEntry `xml:"entry"`
}

// XMLAtomEntry represents a single Atom entry.
type XMLAtomEntry struct {
	ID              string            `xml:"id"`
	Title           string            `xml:"title"`
	Summary         string            `xml:"summary"`
	Published       string            `xml:"published"`
	Updated         string            `xml:"updated"`
	Authors         []XMLAtomAuthor   `xml:"author"`
	Categories      []XMLAtomCategory `xml:"category"`
	PrimaryCategory XMLAtomCategory   `xml:"primary_category"`
	Links           []XMLAtomLink     `xml:"link"`
	DOI             string            `xml:"doi"`
	JournalRef      string            `xml:"journal_ref"`
	Comment         string            `xml:"comment"`
}

// XMLAtomAuthor represents an Atom author.
type XMLAtomAuthor struct {
	Name        string   `xml:"name"`
	Affiliation []string `xml:"affiliation"`
}

// XMLAtomCategory represents an Atom category.
type XMLAtomCategory struct {
	Term string `xml:"term,attr"`
}

// XMLAtomLink represents an Atom link.
type XMLAtomLink struct {
	Href  string `xml:"href,attr"`
	Rel   string `xml:"rel,attr"`
	Type  string `xml:"type,attr"`
	Title string `xml:"title,attr"`
}

func parseAtom(data []byte) ([]*hubv1.Record, error) {
	var feed XMLAtomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("parsing Atom XML: %w", err)
	}

	if len(feed.Entries) == 0 {
		return nil, fmt.Errorf("no entry elements found in Atom feed")
	}

	var records []*hubv1.Record
	for i, entry := range feed.Entries {
		record, err := atomEntryToHub(&entry)
		if err != nil {
			return nil, fmt.Errorf("converting entry %d: %w", i, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func atomEntryToHub(entry *XMLAtomEntry) (*hubv1.Record, error) {
	record := &hubv1.Record{
		Title: strings.TrimSpace(entry.Title),
		ResourceType: &hubv1.ResourceType{
			Type:     hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
			Original: "arXiv",
		},
	}

	// Extract arXiv ID from the entry URL (e.g., "http://arxiv.org/abs/2511.11447v2")
	arxivID := extractArxivID(entry.ID)
	if arxivID != "" {
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV,
			Value: arxivID,
		})
	}

	// Abstract
	if s := strings.TrimSpace(entry.Summary); s != "" {
		record.Abstract = s
	}

	// Categories as subjects; first is primary
	for _, cat := range entry.Categories {
		if cat.Term != "" {
			record.Subjects = append(record.Subjects, &hubv1.Subject{
				Value:      cat.Term,
				Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV,
			})
		}
	}

	// Published date
	if entry.Published != "" {
		dv := parseArxivDate(entry.Published)
		dv.Type = hubv1.DateType_DATE_TYPE_SUBMITTED
		record.Dates = append(record.Dates, dv)
	}

	// Authors — Atom only gives full name strings
	for _, author := range entry.Authors {
		name := strings.TrimSpace(author.Name)
		if name == "" {
			continue
		}
		c := &hubv1.Contributor{
			Role:       "author",
			Type:       hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			Name:       name,
			ParsedName: parseFullName(name),
		}
		for _, aff := range author.Affiliation {
			if aff != "" {
				if c.Affiliation == "" {
					c.Affiliation = aff
				}
				c.Affiliations = append(c.Affiliations, &hubv1.Affiliation{Name: aff})
			}
		}
		record.Contributors = append(record.Contributors, c)
	}

	// DOI
	if entry.DOI != "" {
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_DOI,
			Value: strings.TrimSpace(entry.DOI),
		})
	}

	// Journal reference
	if entry.JournalRef != "" {
		record.Relations = append(record.Relations, &hubv1.Relation{
			Type:        hubv1.RelationType_RELATION_TYPE_PART_OF,
			TargetTitle: strings.TrimSpace(entry.JournalRef),
		})
	}

	// Comment
	if s := strings.TrimSpace(entry.Comment); s != "" {
		record.Notes = append(record.Notes, s)
	}

	// PDF link
	for _, link := range entry.Links {
		if link.Title == "pdf" && link.Href != "" {
			hub.SetExtra(record, "pdf_url", link.Href)
			break
		}
	}

	record.SourceInfo = &hubv1.SourceInfo{
		Format:        "arxiv",
		FormatVersion: Version,
		SourceId:      arxivID,
	}

	return record, nil
}

// extractArxivID extracts the arXiv ID from a URL like "http://arxiv.org/abs/2511.11447v2".
// Strips the version suffix to return "2511.11447".
func extractArxivID(rawURL string) string {
	// Handle both URL and bare ID
	id := rawURL
	if idx := strings.LastIndex(rawURL, "/abs/"); idx >= 0 {
		id = rawURL[idx+5:]
	}
	// Strip version suffix (v1, v2, etc.)
	if idx := strings.LastIndex(id, "v"); idx > 0 {
		if _, err := strconv.Atoi(id[idx+1:]); err == nil {
			id = id[:idx]
		}
	}
	return strings.TrimSpace(id)
}

// parseFullName splits "Given Family" into parsed name components.
func parseFullName(name string) *hubv1.ParsedName {
	parts := strings.Fields(name)
	pn := &hubv1.ParsedName{FullName: name}
	switch len(parts) {
	case 0:
		// nothing
	case 1:
		pn.Family = parts[0]
		pn.Normalized = parts[0]
	default:
		pn.Family = parts[len(parts)-1]
		pn.Given = strings.Join(parts[:len(parts)-1], " ")
		pn.Normalized = pn.Family + ", " + pn.Given
	}
	return pn
}

// ---------------------------------------------------------------------------
// OAI-PMH format: <arXiv xmlns="http://arxiv.org/OAI/arXiv/">
// ---------------------------------------------------------------------------

// XMLOAIArXiv represents a record in the OAI-PMH arXiv format.
type XMLOAIArXiv struct {
	ID         string        `xml:"id"`
	Created    string        `xml:"created"`
	Updated    string        `xml:"updated"`
	Authors    XMLOAIAuthors `xml:"authors"`
	Title      string        `xml:"title"`
	Categories string        `xml:"categories"`
	Comments   string        `xml:"comments"`
	ReportNo   string        `xml:"report-no"`
	JournalRef string        `xml:"journal-ref"`
	DOI        string        `xml:"doi"`
	MSCClass   string        `xml:"msc-class"`
	ACMClass   string        `xml:"acm-class"`
	License    string        `xml:"license"`
	Abstract   string        `xml:"abstract"`
	Proxy      string        `xml:"proxy"`
}

// XMLOAIAuthors is a wrapper for OAI author elements.
type XMLOAIAuthors struct {
	Authors []XMLOAIAuthor `xml:"author"`
}

// XMLOAIAuthor represents an author in the OAI format.
type XMLOAIAuthor struct {
	Keyname      string   `xml:"keyname"`
	Forenames    string   `xml:"forenames"`
	Suffix       string   `xml:"suffix"`
	Affiliations []string `xml:"affiliation"`
}

func parseOAI(data []byte) ([]*hubv1.Record, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var oaiRecords []*XMLOAIArXiv

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing XML: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// Match <arXiv> element (local name only, namespace varies)
		if start.Name.Local == "arXiv" {
			var rec XMLOAIArXiv
			if err := decoder.DecodeElement(&rec, &start); err != nil {
				return nil, fmt.Errorf("decoding arXiv OAI element: %w", err)
			}
			oaiRecords = append(oaiRecords, &rec)
		}
	}

	if len(oaiRecords) == 0 {
		return nil, fmt.Errorf("no arXiv elements found in OAI-PMH input")
	}

	var records []*hubv1.Record
	for i, oai := range oaiRecords {
		record, err := oaiToHub(oai)
		if err != nil {
			return nil, fmt.Errorf("converting OAI record %d: %w", i, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func oaiToHub(oai *XMLOAIArXiv) (*hubv1.Record, error) {
	record := &hubv1.Record{
		Title: strings.TrimSpace(oai.Title),
		ResourceType: &hubv1.ResourceType{
			Type:     hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
			Original: "arXiv",
		},
	}

	// arXiv ID
	if oai.ID != "" {
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV,
			Value: oai.ID,
		})
	}

	// Categories (space-separated, first is primary)
	if oai.Categories != "" {
		for _, cat := range strings.Fields(oai.Categories) {
			record.Subjects = append(record.Subjects, &hubv1.Subject{
				Value:      cat,
				Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV,
			})
		}
	}

	// Dates
	if oai.Created != "" {
		dv := parseArxivDate(oai.Created)
		dv.Type = hubv1.DateType_DATE_TYPE_SUBMITTED
		record.Dates = append(record.Dates, dv)
	}
	if oai.Updated != "" {
		dv := parseArxivDate(oai.Updated)
		dv.Type = hubv1.DateType_DATE_TYPE_UPDATED
		record.Dates = append(record.Dates, dv)
	}

	// Abstract
	if s := strings.TrimSpace(oai.Abstract); s != "" {
		record.Abstract = s
	}

	// Authors
	for _, author := range oai.Authors.Authors {
		c := &hubv1.Contributor{
			Role: "author",
			Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			ParsedName: &hubv1.ParsedName{
				Given:  strings.TrimSpace(author.Forenames),
				Family: strings.TrimSpace(author.Keyname),
				Suffix: strings.TrimSpace(author.Suffix),
			},
		}
		c.Name = buildDisplayName(c.ParsedName)
		c.ParsedName.FullName = c.Name
		if c.ParsedName.Family != "" {
			if c.ParsedName.Given != "" {
				c.ParsedName.Normalized = c.ParsedName.Family + ", " + c.ParsedName.Given
			} else {
				c.ParsedName.Normalized = c.ParsedName.Family
			}
		}
		for _, aff := range author.Affiliations {
			if aff != "" {
				if c.Affiliation == "" {
					c.Affiliation = aff
				}
				c.Affiliations = append(c.Affiliations, &hubv1.Affiliation{Name: aff})
			}
		}
		record.Contributors = append(record.Contributors, c)
	}

	// Comments
	if s := strings.TrimSpace(oai.Comments); s != "" {
		record.Notes = append(record.Notes, s)
	}

	// DOI
	if oai.DOI != "" {
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_DOI,
			Value: strings.TrimSpace(oai.DOI),
		})
	}

	// Report number
	if oai.ReportNo != "" {
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER,
			Value: strings.TrimSpace(oai.ReportNo),
		})
	}

	// Journal reference
	if oai.JournalRef != "" {
		record.Relations = append(record.Relations, &hubv1.Relation{
			Type:        hubv1.RelationType_RELATION_TYPE_PART_OF,
			TargetTitle: strings.TrimSpace(oai.JournalRef),
		})
	}

	// MSC/ACM classifications
	if oai.MSCClass != "" {
		for _, val := range splitClassification(oai.MSCClass) {
			record.Subjects = append(record.Subjects, &hubv1.Subject{
				Value:      val,
				Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MSC,
			})
		}
	}
	if oai.ACMClass != "" {
		for _, val := range splitClassification(oai.ACMClass) {
			record.Subjects = append(record.Subjects, &hubv1.Subject{
				Value:      val,
				Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ACM,
			})
		}
	}

	// License
	if oai.License != "" {
		record.Rights = append(record.Rights, &hubv1.Rights{
			Uri: strings.TrimSpace(oai.License),
		})
	}

	record.SourceInfo = &hubv1.SourceInfo{
		Format:        "arxiv",
		FormatVersion: Version,
		SourceId:      oai.ID,
	}

	return record, nil
}

// splitClassification splits comma-or-semicolon-separated classification strings.
func splitClassification(s string) []string {
	var result []string
	// MSC/ACM classes can be separated by commas or semicolons
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';'
	}) {
		if v := strings.TrimSpace(part); v != "" {
			result = append(result, v)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// arXivRecord XSD 1.0 format (original schema)
// ---------------------------------------------------------------------------

func parseXSDRecord(data []byte) ([]*hubv1.Record, error) {
	xmlRecords, err := extractArXivRecords(data)
	if err != nil {
		return nil, err
	}

	if len(xmlRecords) == 0 {
		return nil, fmt.Errorf("no arXiv elements found in input")
	}

	var records []*hubv1.Record
	for i, xmlRec := range xmlRecords {
		record, err := xmlToHub(xmlRec)
		if err != nil {
			return nil, fmt.Errorf("converting record %d: %w", i, err)
		}
		records = append(records, record)
	}

	return records, nil
}

// extractArXivRecords finds all arXivRecord elements in the XML.
func extractArXivRecords(data []byte) ([]*XMLRecord, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var records []*XMLRecord

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing XML: %w", err)
		}

		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		if start.Name.Local == "arXivRecord" {
			var rec XMLRecord
			if err := decoder.DecodeElement(&rec, &start); err != nil {
				return nil, fmt.Errorf("decoding arXivRecord: %w", err)
			}
			records = append(records, &rec)
		}
	}

	return records, nil
}

// xmlToHub converts a parsed arXiv XML record to a hub record.
func xmlToHub(xmlRec *XMLRecord) (*hubv1.Record, error) {
	record := &hubv1.Record{
		Title: strings.TrimSpace(xmlRec.Title),
		ResourceType: &hubv1.ResourceType{
			Type:     hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
			Original: "arXiv",
		},
	}

	// arXiv identifier
	if xmlRec.Identifier != "" {
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV,
			Value: xmlRec.Identifier,
		})
	}

	// Primary classification
	if xmlRec.Primary != "" {
		record.Subjects = append(record.Subjects, &hubv1.Subject{
			Value:      xmlRec.Primary,
			Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV,
		})
	}

	// Cross-listed classifications
	for _, cross := range xmlRec.Cross {
		record.Subjects = append(record.Subjects, &hubv1.Subject{
			Value:      cross,
			Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV,
		})
	}

	// Submission date
	if xmlRec.Date != "" {
		record.Dates = append(record.Dates, parseArxivDate(xmlRec.Date))
	}

	// Abstract (XSD allows multiple abstract elements)
	if len(xmlRec.Abstract) > 0 {
		var trimmed []string
		for _, a := range xmlRec.Abstract {
			if s := strings.TrimSpace(a); s != "" {
				trimmed = append(trimmed, s)
			}
		}
		record.Abstract = strings.Join(trimmed, "\n\n")
	}

	// Comments → notes
	for _, c := range xmlRec.Comments {
		if s := strings.TrimSpace(c); s != "" {
			record.Notes = append(record.Notes, s)
		}
	}

	// Authorship → contributors
	if xmlRec.Authorship != nil {
		record.Contributors = parseAuthorship(xmlRec.Authorship)
	}

	// External classifications (MSC, ACM, PACS)
	for _, cls := range xmlRec.Classification {
		vocab := classSchemeToVocab(cls.Scheme)
		for _, val := range cls.Values {
			record.Subjects = append(record.Subjects, &hubv1.Subject{
				Value:      val,
				Vocabulary: vocab,
			})
		}
	}

	// Alternate identifiers and journal references
	if xmlRec.Alternate != nil {
		for _, doi := range xmlRec.Alternate.DOI {
			record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
				Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_DOI,
				Value: doi,
			})
		}
		for _, rn := range xmlRec.Alternate.ReportNo {
			record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
				Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER,
				Value: rn,
			})
		}
		for _, jr := range xmlRec.Alternate.JournalRef {
			record.Relations = append(record.Relations, &hubv1.Relation{
				Type:        hubv1.RelationType_RELATION_TYPE_PART_OF,
				TargetTitle: jr,
			})
		}
	}

	// Extra metadata
	if xmlRec.VersionNum > 0 {
		hub.SetExtra(record, "version", float64(xmlRec.VersionNum))
	}
	if xmlRec.Submitter != nil {
		if xmlRec.Submitter.Email != "" {
			hub.SetExtra(record, "submitter_email", xmlRec.Submitter.Email)
		}
		if xmlRec.Submitter.Identifier != "" {
			hub.SetExtra(record, "submitter_id", xmlRec.Submitter.Identifier)
		}
	}
	if xmlRec.Proxy != nil {
		if xmlRec.Proxy.Identifier != "" {
			hub.SetExtra(record, "proxy_id", xmlRec.Proxy.Identifier)
		}
		if xmlRec.Proxy.RemoteIdentifier != "" {
			hub.SetExtra(record, "proxy_remote_id", xmlRec.Proxy.RemoteIdentifier)
		}
	}
	if xmlRec.Source != nil {
		if xmlRec.Source.Type != "" {
			hub.SetExtra(record, "source_type", xmlRec.Source.Type)
		}
		if xmlRec.Source.Size > 0 {
			hub.SetExtra(record, "source_size", float64(xmlRec.Source.Size))
		}
		if xmlRec.Source.MD5 != "" {
			hub.SetExtra(record, "source_md5", xmlRec.Source.MD5)
		}
	}

	// Source tracking
	record.SourceInfo = &hubv1.SourceInfo{
		Format:        "arxiv",
		FormatVersion: Version,
		SourceId:      xmlRec.Identifier,
	}

	return record, nil
}

// parseArxivDate parses an arXiv date string (ISO 8601 with Z suffix).
func parseArxivDate(dateStr string) *hubv1.DateValue {
	dv := &hubv1.DateValue{
		Type: hubv1.DateType_DATE_TYPE_SUBMITTED,
		Raw:  dateStr,
	}

	// Try RFC3339 (2006-01-02T15:04:05Z)
	if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
		dv.Year = int32(t.Year())
		dv.Month = int32(t.Month())
		dv.Day = int32(t.Day())
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
		return dv
	}

	// Try date-only (2006-01-02)
	if t, err := time.Parse("2006-01-02", dateStr); err == nil {
		dv.Year = int32(t.Year())
		dv.Month = int32(t.Month())
		dv.Day = int32(t.Day())
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
		return dv
	}

	// Fallback: parse components manually
	parts := strings.Split(dateStr, "-")
	if len(parts) >= 1 {
		if year, err := strconv.Atoi(parts[0]); err == nil {
			dv.Year = int32(year)
			dv.Precision = hubv1.DatePrecision_DATE_PRECISION_YEAR
		}
	}
	if len(parts) >= 2 {
		if month, err := strconv.Atoi(parts[1]); err == nil {
			dv.Month = int32(month)
			dv.Precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
		}
	}

	return dv
}

// parseAuthorship converts XML authorship to hub contributors.
func parseAuthorship(auth *XMLAuthorship) []*hubv1.Contributor {
	// Build affiliation lookup: affid string → institution name
	affLookup := make(map[string]string)
	for _, aff := range auth.Affiliations {
		affLookup[strconv.Itoa(aff.AffID)] = aff.Institution
	}

	var contributors []*hubv1.Contributor
	for _, author := range auth.Authors {
		c := &hubv1.Contributor{
			Role: "author",
			Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			ParsedName: &hubv1.ParsedName{
				Given:  strings.TrimSpace(author.Beforekey),
				Family: strings.TrimSpace(author.Keyname),
				Suffix: strings.TrimSpace(author.Afterkey),
			},
		}

		// Build display and normalized names
		c.Name = buildDisplayName(c.ParsedName)
		c.ParsedName.FullName = c.Name
		if c.ParsedName.Family != "" {
			if c.ParsedName.Given != "" {
				c.ParsedName.Normalized = c.ParsedName.Family + ", " + c.ParsedName.Given
			} else {
				c.ParsedName.Normalized = c.ParsedName.Family
			}
		}

		// Override default role if author has a specific role
		if author.Role != "" {
			c.Role = author.Role
		}

		// Resolve affiliations from affref attribute (space-separated IDs)
		if author.Affref != "" {
			refs := strings.Fields(author.Affref)
			for i, ref := range refs {
				if inst, ok := affLookup[ref]; ok {
					if i == 0 {
						c.Affiliation = inst
					}
					c.Affiliations = append(c.Affiliations, &hubv1.Affiliation{
						Name: inst,
					})
				}
			}
		}

		contributors = append(contributors, c)
	}

	return contributors
}

// buildDisplayName creates a display name from parsed name components.
// Format: "Given Family Suffix" (e.g., "John Doe Jr")
func buildDisplayName(pn *hubv1.ParsedName) string {
	var parts []string
	if pn.Given != "" {
		parts = append(parts, pn.Given)
	}
	if pn.Family != "" {
		parts = append(parts, pn.Family)
	}
	if pn.Suffix != "" {
		parts = append(parts, pn.Suffix)
	}
	return strings.Join(parts, " ")
}

// classSchemeToVocab maps a classification scheme string to a hub vocabulary.
func classSchemeToVocab(scheme string) hubv1.SubjectVocabulary {
	switch strings.ToUpper(scheme) {
	case "MSC1991", "MSC2000":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MSC
	case "ACM1998":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ACM
	case "PACS2003":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_PACS
	default:
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
	}
}
