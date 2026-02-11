package arxiv

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	arxivv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/arxiv/v1_0"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

// Serialize writes hub records as arXiv metadata XML.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	// opts reserved for future use (e.g., pretty print, encoding options)
	_ = opts

	for i, record := range records {
		// Step 1: Convert hub record to spoke proto struct
		spokeRecord, err := hubToSpoke(record)
		if err != nil {
			return fmt.Errorf("converting record %d to spoke: %w", i, err)
		}

		// Step 2: Convert spoke proto to XML-marshalable struct
		xmlRecord := spokeToXML(spokeRecord)

		// Step 3: Marshal to XML
		output, err := xml.MarshalIndent(xmlRecord, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling record %d: %w", i, err)
		}

		// Write XML declaration for first record
		if i == 0 {
			if _, err := w.Write([]byte(xml.Header)); err != nil {
				return err
			}
		}

		if _, err := w.Write(output); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
	}

	return nil
}

// hubToSpoke converts a hub record to the arXiv spoke proto struct.
func hubToSpoke(record *hubv1.Record) (*arxivv1.Record, error) {
	arxiv := &arxivv1.Record{
		Title: record.Title,
	}

	// Extract arXiv identifier
	for _, id := range record.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV {
			arxiv.Identifier = id.Value
			break
		}
	}
	if arxiv.Identifier == "" {
		arxiv.Identifier = "unknown/0000000"
	}

	// Primary and cross-listed classifications from subjects
	for _, subj := range record.Subjects {
		if subj.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV ||
			strings.Contains(strings.ToLower(subj.Value), ".") {
			if arxiv.Primary == "" {
				arxiv.Primary = subj.Value
			} else {
				arxiv.Cross = append(arxiv.Cross, subj.Value)
			}
		}
	}
	if arxiv.Primary == "" {
		arxiv.Primary = "cs.DL" // Default to Digital Libraries
	}

	// Date
	submittedDate := hub.GetDate(record, hubv1.DateType_DATE_TYPE_SUBMITTED)
	if submittedDate != nil {
		arxiv.Date = formatArxivDate(submittedDate)
	} else {
		arxiv.Date = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}

	// Version
	arxiv.Version = 1

	// Authorship
	arxiv.Authorship = buildSpokeAuthorship(record.Contributors)

	// External classifications (MSC, ACM, PACS)
	arxiv.Classification = buildSpokeClassifications(record.Subjects)

	// Alternate identifiers
	arxiv.Alternate = buildSpokeAlternate(record)

	// Abstract
	if record.Abstract != "" {
		arxiv.Abstract = []string{record.Abstract}
	}

	// Notes as comments
	if len(record.Notes) > 0 {
		arxiv.Comments = record.Notes
	}

	return arxiv, nil
}

// formatArxivDate formats a date for arXiv (ISO 8601 with Z suffix).
func formatArxivDate(d *hubv1.DateValue) string {
	if d.Year == 0 {
		return time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}

	month := d.Month
	if month == 0 {
		month = 1
	}
	day := d.Day
	if day == 0 {
		day = 1
	}

	t := time.Date(int(d.Year), time.Month(month), int(day), 0, 0, 0, 0, time.UTC)
	return t.Format("2006-01-02T15:04:05Z")
}

// buildSpokeAuthorship creates the authorship element from contributors.
func buildSpokeAuthorship(contributors []*hubv1.Contributor) *arxivv1.Authorship {
	if len(contributors) == 0 {
		return &arxivv1.Authorship{
			Authors: []*arxivv1.Author{{Keyname: "Unknown"}},
		}
	}

	authorship := &arxivv1.Authorship{}
	affMap := make(map[string]int32)
	var affID int32 = 1

	for _, c := range contributors {
		author := &arxivv1.Author{}

		// Name parts
		if c.ParsedName != nil {
			author.Beforekey = c.ParsedName.Given
			author.Keyname = c.ParsedName.Family
			author.Afterkey = c.ParsedName.Suffix
		} else if c.Name != "" {
			parts := strings.Split(c.Name, ",")
			if len(parts) >= 2 {
				author.Keyname = strings.TrimSpace(parts[0])
				author.Beforekey = strings.TrimSpace(parts[1])
			} else {
				parts = strings.Fields(c.Name)
				if len(parts) >= 2 {
					author.Keyname = parts[len(parts)-1]
					author.Beforekey = strings.Join(parts[:len(parts)-1], " ")
				} else {
					author.Keyname = c.Name
				}
			}
		}

		if author.Keyname == "" {
			author.Keyname = "Unknown"
		}

		// Role
		if c.Role != "" && c.Role != "author" && c.Role != "aut" {
			author.Role = c.Role
		}

		// Affiliation
		if c.Affiliation != "" {
			if id, ok := affMap[c.Affiliation]; ok {
				author.Affref = fmt.Sprintf("%d", id)
			} else {
				affMap[c.Affiliation] = affID
				authorship.Affiliations = append(authorship.Affiliations, &arxivv1.Affiliation{
					Affid:       affID,
					Institution: c.Affiliation,
				})
				author.Affref = fmt.Sprintf("%d", affID)
				affID++
			}
		}

		authorship.Authors = append(authorship.Authors, author)
	}

	return authorship
}

// buildSpokeClassifications creates classification elements from subjects.
func buildSpokeClassifications(subjects []*hubv1.Subject) []*arxivv1.Classification {
	msc := []string{}
	acm := []string{}
	pacs := []string{}

	for _, s := range subjects {
		switch s.Vocabulary {
		case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MSC:
			msc = append(msc, s.Value)
		case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ACM:
			acm = append(acm, s.Value)
		case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_PACS:
			pacs = append(pacs, s.Value)
		}
	}

	var result []*arxivv1.Classification

	if len(msc) > 0 {
		result = append(result, &arxivv1.Classification{
			Scheme: arxivv1.ClassificationScheme_CLASSIFICATION_SCHEME_MSC2000,
			Value:  msc,
		})
	}
	if len(acm) > 0 {
		result = append(result, &arxivv1.Classification{
			Scheme: arxivv1.ClassificationScheme_CLASSIFICATION_SCHEME_ACM1998,
			Value:  acm,
		})
	}
	if len(pacs) > 0 {
		result = append(result, &arxivv1.Classification{
			Scheme: arxivv1.ClassificationScheme_CLASSIFICATION_SCHEME_PACS2003,
			Value:  pacs,
		})
	}

	return result
}

// buildSpokeAlternate creates alternate identifiers from the record.
func buildSpokeAlternate(record *hubv1.Record) *arxivv1.Alternate {
	alt := &arxivv1.Alternate{}
	hasContent := false

	for _, id := range record.Identifiers {
		switch id.Type {
		case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
			alt.Doi = append(alt.Doi, id.Value)
			hasContent = true
		case hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER:
			alt.ReportNo = append(alt.ReportNo, id.Value)
			hasContent = true
		}
	}

	// Journal references from relations
	for _, rel := range record.Relations {
		if rel.Type == hubv1.RelationType_RELATION_TYPE_PART_OF {
			if rel.TargetTitle != "" {
				alt.JournalRef = append(alt.JournalRef, rel.TargetTitle)
				hasContent = true
			}
		}
	}

	if hasContent {
		return alt
	}
	return nil
}

// spokeToXML converts a spoke proto struct to an XML-marshalable struct.
func spokeToXML(spoke *arxivv1.Record) *XMLRecord {
	xmlRec := &XMLRecord{
		Xmlns:      "http://arXiv.org/arXivRecord",
		Version:    "1.0",
		Identifier: spoke.Identifier,
		Primary:    spoke.Primary,
		Cross:      spoke.Cross,
		VersionNum: int(spoke.Version),
		Date:       spoke.Date,
		Title:      spoke.Title,
		Comments:   spoke.Comments,
		Abstract:   spoke.Abstract,
	}

	// Authorship
	if spoke.Authorship != nil {
		xmlRec.Authorship = &XMLAuthorship{}
		for _, aff := range spoke.Authorship.Affiliations {
			xmlRec.Authorship.Affiliations = append(xmlRec.Authorship.Affiliations, XMLAffiliation{
				AffID:       int(aff.Affid),
				Institution: aff.Institution,
				Address:     aff.Address,
			})
		}
		for _, auth := range spoke.Authorship.Authors {
			xmlRec.Authorship.Authors = append(xmlRec.Authorship.Authors, XMLAuthor{
				Affref:    auth.Affref,
				Beforekey: auth.Beforekey,
				Keyname:   auth.Keyname,
				Afterkey:  auth.Afterkey,
				Role:      auth.Role,
			})
		}
	}

	// Classifications
	for _, cls := range spoke.Classification {
		xmlCls := XMLClassification{
			Values: cls.Value,
		}
		switch cls.Scheme {
		case arxivv1.ClassificationScheme_CLASSIFICATION_SCHEME_MSC1991:
			xmlCls.Scheme = "MSC1991"
		case arxivv1.ClassificationScheme_CLASSIFICATION_SCHEME_MSC2000:
			xmlCls.Scheme = "MSC2000"
		case arxivv1.ClassificationScheme_CLASSIFICATION_SCHEME_ACM1998:
			xmlCls.Scheme = "ACM1998"
		case arxivv1.ClassificationScheme_CLASSIFICATION_SCHEME_PACS2003:
			xmlCls.Scheme = "PACS2003"
		}
		xmlRec.Classification = append(xmlRec.Classification, xmlCls)
	}

	// Alternate identifiers
	if spoke.Alternate != nil {
		xmlRec.Alternate = &XMLAlternate{
			ReportNo:   spoke.Alternate.ReportNo,
			JournalRef: spoke.Alternate.JournalRef,
			DOI:        spoke.Alternate.Doi,
		}
	}

	return xmlRec
}

// XML types for marshaling to arXiv XML format.
// These wrap the spoke proto structs with proper XML tags.

// XMLRecord represents the root arXivRecord element.
type XMLRecord struct {
	XMLName        xml.Name            `xml:"arXivRecord"`
	Xmlns          string              `xml:"xmlns,attr"`
	Version        string              `xml:"version,attr"`
	Identifier     string              `xml:"identifier"`
	Primary        string              `xml:"primary"`
	Cross          []string            `xml:"cross,omitempty"`
	Submitter      *XMLSubmitter       `xml:"submitter,omitempty"`
	VersionNum     int                 `xml:"version"`
	Date           string              `xml:"date"`
	Source         *XMLSource          `xml:"source,omitempty"`
	Title          string              `xml:"title"`
	Authorship     *XMLAuthorship      `xml:"authorship"`
	Classification []XMLClassification `xml:"classification,omitempty"`
	Alternate      *XMLAlternate       `xml:"alternate,omitempty"`
	Comments       []string            `xml:"comments,omitempty"`
	Abstract       []string            `xml:"abstract,omitempty"`
}

// XMLSubmitter represents submitter information.
type XMLSubmitter struct {
	Email      string `xml:"email"`
	Identifier string `xml:"identifier,omitempty"`
}

// XMLSource describes the source file.
type XMLSource struct {
	Type string `xml:"type"`
	Size int64  `xml:"size"`
	MD5  string `xml:"md5"`
}

// XMLAuthorship contains authors and affiliations.
type XMLAuthorship struct {
	Affiliations []XMLAffiliation `xml:"affiliation,omitempty"`
	Authors      []XMLAuthor      `xml:"author"`
}

// XMLAffiliation represents an institutional affiliation.
type XMLAffiliation struct {
	AffID       int    `xml:"affid,attr"`
	Institution string `xml:"institution"`
	Address     string `xml:"address,omitempty"`
}

// XMLAuthor represents a single author.
type XMLAuthor struct {
	Affref    string `xml:"affref,attr,omitempty"`
	Beforekey string `xml:"beforekey,omitempty"`
	Keyname   string `xml:"keyname"`
	Afterkey  string `xml:"afterkey,omitempty"`
	Role      string `xml:"role,omitempty"`
}

// XMLClassification represents an external classification.
type XMLClassification struct {
	Scheme string   `xml:"scheme,attr"`
	Values []string `xml:"value"`
}

// XMLAlternate contains alternate identifiers.
type XMLAlternate struct {
	ReportNo   []string `xml:"report-no,omitempty"`
	JournalRef []string `xml:"journal-ref,omitempty"`
	DOI        []string `xml:"DOI,omitempty"`
}
