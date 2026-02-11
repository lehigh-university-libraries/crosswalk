package dublincore

import (
	"encoding/xml"
	"fmt"
	"io"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	dcv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/dublincore/v20200120"
)

// Serialize writes hub records as Dublin Core XML.
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

// hubToSpoke converts a hub record to the Dublin Core spoke proto struct.
func hubToSpoke(record *hubv1.Record) (*dcv1.Record, error) {
	dc := &dcv1.Record{}

	// Title
	if record.Title != "" {
		dc.Title = []*dcv1.LocalizedString{{Value: record.Title}}
	}

	// Creators and Contributors
	for _, c := range record.Contributors {
		agent := &dcv1.Agent{Name: c.Name}
		role := c.Role
		if role == "author" || role == "creator" || role == "aut" || role == "cre" || role == "" {
			dc.Creator = append(dc.Creator, agent)
		} else {
			dc.Contributor = append(dc.Contributor, agent)
		}
	}

	// Publisher
	if record.Publisher != "" {
		dc.Publisher = []*dcv1.Agent{{Name: record.Publisher}}
	}

	// Subjects
	for _, s := range record.Subjects {
		dc.Subject = append(dc.Subject, &dcv1.Subject{Value: s.Value})
	}

	// Description/Abstract
	if record.Abstract != "" {
		dc.Description = []*dcv1.LocalizedString{{Value: record.Abstract}}
	}

	// Dates
	for _, d := range record.Dates {
		dateStr := formatDate(d)
		if dateStr != "" {
			dc.Date = append(dc.Date, &dcv1.Date{Value: dateStr})
		}
	}

	// Language
	if record.Language != "" {
		dc.Language = []string{record.Language}
	}

	// Identifiers
	for _, id := range record.Identifiers {
		dc.Identifier = append(dc.Identifier, &dcv1.Identifier{
			Value: id.Value,
			Type:  identifierTypeToScheme(id.Type),
		})
	}

	// Resource Type
	if record.ResourceType != nil {
		typeValue := record.ResourceType.Original
		if typeValue == "" {
			typeValue = resourceTypeToString(record.ResourceType.Type)
		}
		dc.Type = []*dcv1.TypeValue{{Value: typeValue}}
	}

	// Rights
	for _, r := range record.Rights {
		rights := &dcv1.Rights{}
		if r.Uri != "" {
			rights.Uri = r.Uri
		}
		if r.Statement != "" {
			rights.Statement = r.Statement
		}
		dc.Rights = append(dc.Rights, rights)
	}

	// Relations
	for _, rel := range record.Relations {
		relValue := rel.TargetUri
		if relValue == "" {
			relValue = rel.TargetTitle
		}
		if relValue != "" {
			dc.Relation = append(dc.Relation, &dcv1.Relation{Value: relValue})
		}
	}

	return dc, nil
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

// identifierTypeToScheme maps hub identifier type to DC scheme.
func identifierTypeToScheme(t hubv1.IdentifierType) string {
	switch t {
	case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
		return "DOI"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
		return "ISBN"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
		return "ISSN"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
		return "URI"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE:
		return "HDL"
	default:
		return ""
	}
}

// resourceTypeToString converts hub resource type enum to string.
func resourceTypeToString(rt hubv1.ResourceTypeValue) string {
	switch rt {
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE:
		return "Text"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK:
		return "Text"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION:
		return "Text"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET:
		return "Dataset"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE:
		return "Image"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO:
		return "MovingImage"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO:
		return "Sound"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE:
		return "Software"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION:
		return "Collection"
	default:
		return "Text"
	}
}

// spokeToXML converts a spoke proto struct to an XML-marshalable struct.
func spokeToXML(spoke *dcv1.Record) *XMLRecord {
	xmlRec := &XMLRecord{
		XmlnsDC:      "http://purl.org/dc/elements/1.1/",
		XmlnsDCTerms: "http://purl.org/dc/terms/",
	}

	for _, t := range spoke.Title {
		xmlRec.Title = append(xmlRec.Title, t.Value)
	}

	for _, c := range spoke.Creator {
		xmlRec.Creator = append(xmlRec.Creator, c.Name)
	}

	for _, s := range spoke.Subject {
		xmlRec.Subject = append(xmlRec.Subject, s.Value)
	}

	for _, d := range spoke.Description {
		xmlRec.Description = append(xmlRec.Description, d.Value)
	}

	for _, p := range spoke.Publisher {
		xmlRec.Publisher = append(xmlRec.Publisher, p.Name)
	}

	for _, c := range spoke.Contributor {
		xmlRec.Contributor = append(xmlRec.Contributor, c.Name)
	}

	for _, d := range spoke.Date {
		xmlRec.Date = append(xmlRec.Date, d.Value)
	}

	for _, t := range spoke.Type {
		xmlRec.Type = append(xmlRec.Type, t.Value)
	}

	for _, id := range spoke.Identifier {
		xmlRec.Identifier = append(xmlRec.Identifier, id.Value)
	}

	xmlRec.Language = spoke.Language

	for _, rel := range spoke.Relation {
		xmlRec.Relation = append(xmlRec.Relation, rel.Value)
	}

	xmlRec.Source = spoke.Source

	for _, r := range spoke.Rights {
		if r.Uri != "" {
			xmlRec.Rights = append(xmlRec.Rights, r.Uri)
		} else if r.Statement != "" {
			xmlRec.Rights = append(xmlRec.Rights, r.Statement)
		}
	}

	return xmlRec
}

// XML types for Dublin Core marshaling.

// XMLRecord represents the root Dublin Core record element.
type XMLRecord struct {
	XMLName      xml.Name `xml:"metadata"`
	XmlnsDC      string   `xml:"xmlns:dc,attr"`
	XmlnsDCTerms string   `xml:"xmlns:dcterms,attr"`

	Title       []string `xml:"dc:title,omitempty"`
	Creator     []string `xml:"dc:creator,omitempty"`
	Subject     []string `xml:"dc:subject,omitempty"`
	Description []string `xml:"dc:description,omitempty"`
	Publisher   []string `xml:"dc:publisher,omitempty"`
	Contributor []string `xml:"dc:contributor,omitempty"`
	Date        []string `xml:"dc:date,omitempty"`
	Type        []string `xml:"dc:type,omitempty"`
	Format      []string `xml:"dc:format,omitempty"`
	Identifier  []string `xml:"dc:identifier,omitempty"`
	Source      []string `xml:"dc:source,omitempty"`
	Language    []string `xml:"dc:language,omitempty"`
	Relation    []string `xml:"dc:relation,omitempty"`
	Coverage    []string `xml:"dc:coverage,omitempty"`
	Rights      []string `xml:"dc:rights,omitempty"`
}
