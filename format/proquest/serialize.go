package proquest

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	pqv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/proquest/v1"
)

// Serialize writes hub records as ProQuest ETD XML.
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

// hubToSpoke converts a hub record to the ProQuest spoke proto struct.
func hubToSpoke(record *hubv1.Record) (*pqv1.Submission, error) {
	submission := &pqv1.Submission{
		Authorship:  &pqv1.Authorship{},
		Description: &pqv1.Description{},
		Repository:  &pqv1.Repository{},
		Content:     &pqv1.Content{},
	}

	// Title
	submission.Description.Title = record.Title

	// Abstract - split into paragraphs
	if record.Abstract != "" {
		paragraphs := strings.Split(record.Abstract, "\n\n")
		if len(paragraphs) == 1 {
			paragraphs = []string{record.Abstract}
		}
		submission.Content.Abstract = &pqv1.Abstract{
			Paragraphs: paragraphs,
		}
	}

	// Degree info
	if record.DegreeInfo != nil {
		submission.Description.Degree = record.DegreeInfo.DegreeName
		submission.Description.DegreeLevel = record.DegreeInfo.DegreeLevel
		submission.Description.Discipline = record.DegreeInfo.Department
		if record.DegreeInfo.Institution != "" {
			submission.Description.Institution = &pqv1.Institution{
				Name: record.DegreeInfo.Institution,
			}
		}
	}

	// Page count
	if record.PageCount > 0 {
		submission.Description.PageCount = record.PageCount
	}

	// Contributors - separate authors from advisors
	for _, c := range record.Contributors {
		role := strings.ToLower(c.Role)
		name := contributorToName(c)

		switch role {
		case "advisor", "ths", "thesis advisor":
			submission.Description.Advisors = append(submission.Description.Advisors, &pqv1.Advisor{
				Name: name,
			})
		default:
			// Treat as author
			author := &pqv1.Author{
				Type: "primary",
				Name: name,
			}
			// Add ORCID if available
			for _, id := range c.Identifiers {
				if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID {
					author.Orcid = id.Value
					break
				}
			}
			submission.Authorship.Authors = append(submission.Authorship.Authors, author)
		}
	}

	// Subjects -> Categories and Keywords
	categorization := &pqv1.Categorization{}
	for _, s := range record.Subjects {
		if s.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS {
			categorization.Keywords = append(categorization.Keywords, s.Value)
		} else {
			categorization.Categories = append(categorization.Categories, &pqv1.Category{
				Description: s.Value,
			})
		}
	}
	if len(categorization.Categories) > 0 || len(categorization.Keywords) > 0 || record.Language != "" {
		categorization.Language = record.Language
		submission.Description.Categorization = categorization
	}

	// Dates
	dates := &pqv1.Dates{}
	for _, d := range record.Dates {
		dateStr := formatDate(d)
		switch d.Type {
		case hubv1.DateType_DATE_TYPE_ACCEPTED:
			dates.AcceptDate = dateStr
		case hubv1.DateType_DATE_TYPE_ISSUED, hubv1.DateType_DATE_TYPE_PUBLISHED:
			dates.CompletionDate = dateStr
		}
	}
	if dates.AcceptDate != "" || dates.CompletionDate != "" {
		submission.Description.Dates = dates
	}

	// Access condition / embargo
	if record.AccessCondition != "" {
		submission.Repository.Embargo = record.AccessCondition
	}

	// Files
	if len(record.Files) > 0 {
		file := record.Files[0]
		submission.Content.Binary = &pqv1.Binary{
			Type:     file.MimeType,
			FileName: file.Name,
		}
	}

	return submission, nil
}

// contributorToName converts a hub contributor to a ProQuest Name.
func contributorToName(c *hubv1.Contributor) *pqv1.Name {
	name := &pqv1.Name{}

	if c.ParsedName != nil {
		name.Surname = c.ParsedName.Family
		name.First = c.ParsedName.Given
		name.Middle = c.ParsedName.Middle
		name.Suffix = c.ParsedName.Suffix
	} else if c.Name != "" {
		// Try to parse a simple "Last, First" format
		parts := strings.SplitN(c.Name, ",", 2)
		if len(parts) == 2 {
			name.Surname = strings.TrimSpace(parts[0])
			name.First = strings.TrimSpace(parts[1])
		} else {
			// Fall back to putting full name in surname
			name.Surname = c.Name
		}
	}

	return name
}

// formatDate formats a date value to ISO 8601 string.
func formatDate(d *hubv1.DateValue) string {
	if d.Year == 0 {
		return d.Raw
	}
	if d.Month == 0 {
		return fmt.Sprintf("%04d", d.Year)
	}
	if d.Day == 0 {
		return fmt.Sprintf("%04d-%02d", d.Year, d.Month)
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// spokeToXML converts a spoke proto struct to an XML-marshalable struct.
func spokeToXML(spoke *pqv1.Submission) *XMLSubmission {
	xml := &XMLSubmission{}

	// Embargo code attribute
	if spoke.EmbargoCode > 0 {
		xml.EmbargoCode = spoke.EmbargoCode
	}

	// Authorship
	if spoke.Authorship != nil && len(spoke.Authorship.Authors) > 0 {
		xml.Authorship = &XMLAuthorship{}
		for _, a := range spoke.Authorship.Authors {
			author := XMLAuthor{
				Type: a.Type,
			}
			if a.Name != nil {
				author.Name = &XMLName{
					Surname: a.Name.Surname,
					First:   a.Name.First,
					Middle:  a.Name.Middle,
					Suffix:  a.Name.Suffix,
				}
			}
			if a.Orcid != "" {
				author.ORCID = a.Orcid
			}
			xml.Authorship.Authors = append(xml.Authorship.Authors, author)
		}
	}

	// Description
	if spoke.Description != nil {
		xml.Description = &XMLDescription{
			Title:       spoke.Description.Title,
			Degree:      spoke.Description.Degree,
			DegreeLevel: degreeLevelToCode(spoke.Description.DegreeLevel),
			Discipline:  spoke.Description.Discipline,
			PageCount:   spoke.Description.PageCount,
			Department:  spoke.Description.Department,
		}

		if spoke.Description.Institution != nil {
			xml.Description.Institution = &XMLInstitution{
				Name:       spoke.Description.Institution.Name,
				Department: spoke.Description.Institution.Department,
			}
		}

		for _, adv := range spoke.Description.Advisors {
			if adv.Name != nil {
				xml.Description.Advisors = append(xml.Description.Advisors, XMLAdvisor{
					Name: XMLName{
						Surname: adv.Name.Surname,
						First:   adv.Name.First,
						Middle:  adv.Name.Middle,
						Suffix:  adv.Name.Suffix,
					},
				})
			}
		}

		if spoke.Description.Categorization != nil {
			cat := spoke.Description.Categorization
			xml.Description.Categorization = &XMLCategorization{
				Language: cat.Language,
				Keywords: cat.Keywords,
			}
			for _, c := range cat.Categories {
				xml.Description.Categorization.Categories = append(
					xml.Description.Categorization.Categories,
					XMLCategory{Description: c.Description},
				)
			}
		}

		if spoke.Description.Dates != nil {
			xml.Description.Dates = &XMLDates{
				AcceptDate:     spoke.Description.Dates.AcceptDate,
				CompletionDate: spoke.Description.Dates.CompletionDate,
			}
		}
	}

	// Repository
	if spoke.Repository != nil && spoke.Repository.Embargo != "" {
		xml.Repository = &XMLRepository{
			DelayedRelease: spoke.Repository.Embargo,
		}
	}

	// Content
	if spoke.Content != nil {
		xml.Content = &XMLContent{}

		if spoke.Content.Abstract != nil && len(spoke.Content.Abstract.Paragraphs) > 0 {
			xml.Content.Abstract = &XMLAbstract{
				Paragraphs: spoke.Content.Abstract.Paragraphs,
			}
		}

		if spoke.Content.Binary != nil {
			xml.Content.Binary = &XMLBinary{
				Type:     spoke.Content.Binary.Type,
				FileName: spoke.Content.Binary.FileName,
			}
		}
	}

	return xml
}

// degreeLevelToCode converts degree level to ProQuest code.
func degreeLevelToCode(level string) string {
	switch strings.ToLower(level) {
	case "doctoral", "phd", "doctorate":
		return "Doctoral"
	case "masters", "master", "master's":
		return "Masters"
	default:
		return level
	}
}

// XML types for ProQuest ETD marshaling.

// XMLSubmission represents the DISS_submission root element.
type XMLSubmission struct {
	XMLName     xml.Name        `xml:"DISS_submission"`
	EmbargoCode int32           `xml:"embargo_code,attr,omitempty"`
	Authorship  *XMLAuthorship  `xml:"DISS_authorship,omitempty"`
	Description *XMLDescription `xml:"DISS_description,omitempty"`
	Content     *XMLContent     `xml:"DISS_content,omitempty"`
	Repository  *XMLRepository  `xml:"DISS_repository,omitempty"`
}

// XMLAuthorship represents DISS_authorship.
type XMLAuthorship struct {
	Authors []XMLAuthor `xml:"DISS_author"`
}

// XMLAuthor represents DISS_author.
type XMLAuthor struct {
	Type     string   `xml:"type,attr,omitempty"`
	Name     *XMLName `xml:"DISS_name,omitempty"`
	Contacts []XMLContact
	ORCID    string `xml:"DISS_orcid,omitempty"`
}

// XMLName represents DISS_name.
type XMLName struct {
	Surname string `xml:"DISS_surname,omitempty"`
	First   string `xml:"DISS_fname,omitempty"`
	Middle  string `xml:"DISS_middle,omitempty"`
	Suffix  string `xml:"DISS_suffix,omitempty"`
}

// XMLContact represents DISS_contact.
type XMLContact struct {
	Type    string      `xml:"type,attr,omitempty"`
	Email   string      `xml:"DISS_email,omitempty"`
	Address *XMLAddress `xml:"DISS_address,omitempty"`
}

// XMLAddress represents DISS_address.
type XMLAddress struct {
	Line    string `xml:"DISS_addrline,omitempty"`
	City    string `xml:"DISS_city,omitempty"`
	State   string `xml:"DISS_st,omitempty"`
	Zip     string `xml:"DISS_pcode,omitempty"`
	Country string `xml:"DISS_country,omitempty"`
}

// XMLDescription represents DISS_description.
type XMLDescription struct {
	Title          string             `xml:"DISS_title,omitempty"`
	Degree         string             `xml:"DISS_degree,omitempty"`
	DegreeLevel    string             `xml:"DISS_degree_level,omitempty"`
	Discipline     string             `xml:"DISS_discipline,omitempty"`
	Institution    *XMLInstitution    `xml:"DISS_institution,omitempty"`
	PageCount      int32              `xml:"DISS_page_count,omitempty"`
	Department     string             `xml:"DISS_department,omitempty"`
	Advisors       []XMLAdvisor       `xml:"DISS_advisor,omitempty"`
	Categorization *XMLCategorization `xml:"DISS_categorization,omitempty"`
	Dates          *XMLDates          `xml:"DISS_dates,omitempty"`
}

// XMLInstitution represents DISS_institution.
type XMLInstitution struct {
	Name       string `xml:"DISS_inst_name,omitempty"`
	Department string `xml:"DISS_inst_contact,omitempty"`
}

// XMLAdvisor represents DISS_advisor.
type XMLAdvisor struct {
	Name XMLName `xml:"DISS_name"`
}

// XMLCategorization represents DISS_categorization.
type XMLCategorization struct {
	Categories []XMLCategory `xml:"DISS_category,omitempty"`
	Keywords   []string      `xml:"DISS_keyword,omitempty"`
	Language   string        `xml:"DISS_language,omitempty"`
}

// XMLCategory represents DISS_category.
type XMLCategory struct {
	Description string `xml:"DISS_cat_desc,omitempty"`
}

// XMLDates represents DISS_dates.
type XMLDates struct {
	AcceptDate     string `xml:"DISS_accept_date,omitempty"`
	CompletionDate string `xml:"DISS_comp_date,omitempty"`
}

// XMLContent represents DISS_content.
type XMLContent struct {
	Abstract *XMLAbstract `xml:"DISS_abstract,omitempty"`
	Binary   *XMLBinary   `xml:"DISS_binary,omitempty"`
}

// XMLAbstract represents DISS_abstract.
type XMLAbstract struct {
	Paragraphs []string `xml:"DISS_para"`
}

// XMLBinary represents DISS_binary.
type XMLBinary struct {
	Type     string `xml:"type,attr,omitempty"`
	FileName string `xml:",chardata"`
}

// XMLRepository represents DISS_repository.
type XMLRepository struct {
	DelayedRelease string `xml:"DISS_delayed_release,omitempty"`
}
