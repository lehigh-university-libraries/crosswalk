package mods

import (
	"encoding/xml"
	"fmt"
	"io"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	modsv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/mods/v3_8"
)

// Serialize writes hub records as MODS XML.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	// opts reserved for future use (e.g., pretty print, encoding options)
	_ = opts

	for i, record := range records {
		spokeRecord, err := hubToSpoke(record)
		if err != nil {
			return fmt.Errorf("converting record %d to spoke: %w", i, err)
		}

		xmlRecord := spokeToXML(spokeRecord)

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

// hubToSpoke converts a hub record to the MODS spoke proto struct.
func hubToSpoke(record *hubv1.Record) (*modsv1.Record, error) {
	mods := &modsv1.Record{}

	// Title
	if record.Title != "" {
		mods.TitleInfo = []*modsv1.TitleInfo{{Title: record.Title}}
	}
	for _, alt := range record.AltTitle {
		mods.TitleInfo = append(mods.TitleInfo, &modsv1.TitleInfo{
			Title: alt,
			Type:  modsv1.TitleType_TITLE_TYPE_ALTERNATIVE,
		})
	}

	// Names (contributors)
	for _, c := range record.Contributors {
		name := &modsv1.Name{}
		if c.Type == hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION {
			name.Type = modsv1.NameType_NAME_TYPE_CORPORATE
			name.NamePart = []*modsv1.NamePart{{Value: c.Name}}
		} else {
			name.Type = modsv1.NameType_NAME_TYPE_PERSONAL
			if c.ParsedName != nil {
				if c.ParsedName.Family != "" {
					name.NamePart = append(name.NamePart, &modsv1.NamePart{
						Type:  modsv1.NamePartType_NAME_PART_TYPE_FAMILY,
						Value: c.ParsedName.Family,
					})
				}
				if c.ParsedName.Given != "" {
					name.NamePart = append(name.NamePart, &modsv1.NamePart{
						Type:  modsv1.NamePartType_NAME_PART_TYPE_GIVEN,
						Value: c.ParsedName.Given,
					})
				}
			} else if c.Name != "" {
				name.NamePart = []*modsv1.NamePart{{Value: c.Name}}
			}
		}

		// Role
		if c.Role != "" {
			name.Role = []*modsv1.Role{{
				RoleTerm: []*modsv1.RoleTerm{{
					Type:  modsv1.RoleTermType_ROLE_TERM_TYPE_TEXT,
					Value: c.Role,
				}},
			}}
		}

		// Affiliation
		if c.Affiliation != "" {
			name.Affiliation = []string{c.Affiliation}
		}

		mods.Name = append(mods.Name, name)
	}

	// Type of resource
	if record.ResourceType != nil {
		mods.TypeOfResource = []*modsv1.TypeOfResource{{
			Value: mapResourceTypeToMODS(record.ResourceType.Type),
		}}
	}

	// Genre
	for _, g := range record.Genres {
		mods.Genre = append(mods.Genre, &modsv1.Genre{Value: g.Value})
	}

	// Origin info
	originInfo := &modsv1.OriginInfo{}
	hasOriginInfo := false

	if record.Publisher != "" {
		originInfo.Publisher = []string{record.Publisher}
		hasOriginInfo = true
	}
	if record.PlacePublished != "" {
		originInfo.Place = []*modsv1.Place{{
			PlaceTerm: []*modsv1.PlaceTerm{{Value: record.PlacePublished}},
		}}
		hasOriginInfo = true
	}
	if record.Edition != "" {
		originInfo.Edition = record.Edition
		hasOriginInfo = true
	}

	for _, d := range record.Dates {
		dateStr := formatDate(d)
		if dateStr != "" {
			switch d.Type {
			case hubv1.DateType_DATE_TYPE_ISSUED, hubv1.DateType_DATE_TYPE_PUBLISHED:
				originInfo.DateIssued = []*modsv1.DateElement{{Value: dateStr}}
			case hubv1.DateType_DATE_TYPE_CREATED:
				originInfo.DateCreated = []*modsv1.DateElement{{Value: dateStr}}
			case hubv1.DateType_DATE_TYPE_COPYRIGHT:
				originInfo.CopyrightDate = []*modsv1.DateElement{{Value: dateStr}}
			}
			hasOriginInfo = true
		}
	}

	if hasOriginInfo {
		mods.OriginInfo = []*modsv1.OriginInfo{originInfo}
	}

	// Language
	if record.Language != "" {
		mods.Language = []*modsv1.Language{{
			LanguageTerm: []*modsv1.LanguageTerm{{Value: record.Language}},
		}}
	}

	// Abstract
	if record.Abstract != "" {
		mods.Abstract = []*modsv1.Abstract{{Value: record.Abstract}}
	}

	// Notes
	for _, n := range record.Notes {
		mods.Note = append(mods.Note, &modsv1.Note{Value: n})
	}

	// Subjects
	for _, s := range record.Subjects {
		mods.Subject = append(mods.Subject, &modsv1.Subject{
			Topic: []string{s.Value},
		})
	}

	// Identifiers
	for _, id := range record.Identifiers {
		mods.Identifier = append(mods.Identifier, &modsv1.Identifier{
			Type:  identifierTypeToMODS(id.Type),
			Value: id.Value,
		})
	}

	// Relations
	for _, rel := range record.Relations {
		relatedItem := &modsv1.RelatedItem{
			Type: relationTypeToMODS(rel.Type),
		}
		// RelatedItem can embed a Record for title and identifier info
		if rel.TargetTitle != "" || rel.TargetUri != "" {
			embeddedRecord := &modsv1.Record{}
			if rel.TargetTitle != "" {
				embeddedRecord.TitleInfo = []*modsv1.TitleInfo{{Title: rel.TargetTitle}}
			}
			if rel.TargetUri != "" {
				embeddedRecord.Identifier = []*modsv1.Identifier{{
					Type:  "uri",
					Value: rel.TargetUri,
				}}
			}
			relatedItem.Record = embeddedRecord
		}
		mods.RelatedItem = append(mods.RelatedItem, relatedItem)
	}

	// Rights
	for _, r := range record.Rights {
		ac := &modsv1.AccessCondition{}
		if r.Uri != "" {
			ac.Href = r.Uri
		}
		if r.Statement != "" {
			ac.Value = r.Statement
		}
		mods.AccessCondition = append(mods.AccessCondition, ac)
	}

	return mods, nil
}

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

func mapResourceTypeToMODS(rt hubv1.ResourceTypeValue) string {
	switch rt {
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_WORKING_PAPER,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_TECHNICAL_REPORT,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_MANUSCRIPT:
		return "text"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE:
		return "still image"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO:
		return "moving image"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO:
		return "sound recording"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE:
		return "software, multimedia"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET:
		return "software, multimedia"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION:
		return "mixed material"
	default:
		return "text"
	}
}

func identifierTypeToMODS(t hubv1.IdentifierType) string {
	switch t {
	case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
		return "doi"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
		return "isbn"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
		return "issn"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
		return "uri"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE:
		return "hdl"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID:
		return "orcid"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL:
		return "local"
	default:
		return "local"
	}
}

func relationTypeToMODS(rt hubv1.RelationType) modsv1.RelatedItemType {
	switch rt {
	case hubv1.RelationType_RELATION_TYPE_PART_OF:
		return modsv1.RelatedItemType_RELATED_ITEM_TYPE_HOST
	case hubv1.RelationType_RELATION_TYPE_HAS_PART:
		return modsv1.RelatedItemType_RELATED_ITEM_TYPE_CONSTITUENT
	case hubv1.RelationType_RELATION_TYPE_IN_SERIES:
		return modsv1.RelatedItemType_RELATED_ITEM_TYPE_SERIES
	case hubv1.RelationType_RELATION_TYPE_VERSION_OF:
		return modsv1.RelatedItemType_RELATED_ITEM_TYPE_OTHER_VERSION
	case hubv1.RelationType_RELATION_TYPE_REFERENCES:
		return modsv1.RelatedItemType_RELATED_ITEM_TYPE_REFERENCES
	default:
		return modsv1.RelatedItemType_RELATED_ITEM_TYPE_UNSPECIFIED
	}
}

// spokeToXML converts a spoke proto struct to an XML-marshalable struct.
func spokeToXML(spoke *modsv1.Record) *XMLMods {
	xmlMods := &XMLMods{
		Xmlns:    "http://www.loc.gov/mods/v3",
		XmlnsXsi: "http://www.w3.org/2001/XMLSchema-instance",
		XsiSchemaLocation: "http://www.loc.gov/mods/v3 " +
			"http://www.loc.gov/standards/mods/v3/mods-3-8.xsd",
		Version: "3.8",
	}

	// Titles
	for _, t := range spoke.TitleInfo {
		xmlMods.TitleInfo = append(xmlMods.TitleInfo, XMLTitleInfo{
			Type:  titleTypeToString(t.Type),
			Title: t.Title,
		})
	}

	// Names
	for _, n := range spoke.Name {
		xmlName := XMLName{Type: nameTypeToString(n.Type)}
		for _, np := range n.NamePart {
			xmlName.NameParts = append(xmlName.NameParts, XMLNamePart{
				Type:  namePartTypeToString(np.Type),
				Value: np.Value,
			})
		}
		for _, r := range n.Role {
			for _, rt := range r.RoleTerm {
				xmlName.Roles = append(xmlName.Roles, XMLRole{
					RoleTerm: XMLRoleTerm{
						Type:  roleTermTypeToString(rt.Type),
						Value: rt.Value,
					},
				})
			}
		}
		xmlName.Affiliations = n.Affiliation
		xmlMods.Names = append(xmlMods.Names, xmlName)
	}

	// Type of resource
	for _, t := range spoke.TypeOfResource {
		xmlMods.TypeOfResource = append(xmlMods.TypeOfResource, t.Value)
	}

	// Genre
	for _, g := range spoke.Genre {
		xmlMods.Genre = append(xmlMods.Genre, g.Value)
	}

	// Origin info
	for _, o := range spoke.OriginInfo {
		xmlOrigin := XMLOriginInfo{}
		for _, p := range o.Place {
			for _, pt := range p.PlaceTerm {
				xmlOrigin.Places = append(xmlOrigin.Places, XMLPlace{
					PlaceTerm: XMLPlaceTerm{Value: pt.Value},
				})
			}
		}
		xmlOrigin.Publishers = o.Publisher
		for _, d := range o.DateIssued {
			xmlOrigin.DateIssued = append(xmlOrigin.DateIssued, d.Value)
		}
		for _, d := range o.DateCreated {
			xmlOrigin.DateCreated = append(xmlOrigin.DateCreated, d.Value)
		}
		for _, d := range o.CopyrightDate {
			xmlOrigin.CopyrightDates = append(xmlOrigin.CopyrightDates, d.Value)
		}
		if o.Edition != "" {
			xmlOrigin.Editions = []string{o.Edition}
		}
		xmlMods.OriginInfo = append(xmlMods.OriginInfo, xmlOrigin)
	}

	// Language
	for _, l := range spoke.Language {
		for _, lt := range l.LanguageTerm {
			xmlMods.Languages = append(xmlMods.Languages, XMLLanguage{
				LanguageTerm: XMLLanguageTerm{Value: lt.Value},
			})
		}
	}

	// Abstract
	for _, a := range spoke.Abstract {
		xmlMods.Abstracts = append(xmlMods.Abstracts, a.Value)
	}

	// Notes
	for _, n := range spoke.Note {
		xmlMods.Notes = append(xmlMods.Notes, n.Value)
	}

	// Subjects
	for _, s := range spoke.Subject {
		xmlSubject := XMLSubject{Topics: s.Topic}
		xmlMods.Subjects = append(xmlMods.Subjects, xmlSubject)
	}

	// Identifiers
	for _, id := range spoke.Identifier {
		xmlMods.Identifiers = append(xmlMods.Identifiers, XMLIdentifier{
			Type:  id.Type,
			Value: id.Value,
		})
	}

	// Related items
	for _, r := range spoke.RelatedItem {
		xmlRelated := XMLRelatedItem{Type: relatedItemTypeToString(r.Type)}
		// Access embedded record for title and identifier info
		if r.Record != nil {
			for _, t := range r.Record.TitleInfo {
				xmlRelated.TitleInfo = append(xmlRelated.TitleInfo, XMLTitleInfo{Title: t.Title})
			}
			for _, id := range r.Record.Identifier {
				xmlRelated.Identifiers = append(xmlRelated.Identifiers, XMLIdentifier{
					Type:  id.Type,
					Value: id.Value,
				})
			}
		}
		xmlMods.RelatedItems = append(xmlMods.RelatedItems, xmlRelated)
	}

	// Access conditions
	for _, ac := range spoke.AccessCondition {
		xmlMods.AccessConditions = append(xmlMods.AccessConditions, XMLAccessCondition{
			Href:  ac.Href,
			Value: ac.Value,
		})
	}

	return xmlMods
}

func titleTypeToString(t modsv1.TitleType) string {
	switch t {
	case modsv1.TitleType_TITLE_TYPE_ALTERNATIVE:
		return "alternative"
	case modsv1.TitleType_TITLE_TYPE_TRANSLATED:
		return "translated"
	case modsv1.TitleType_TITLE_TYPE_UNIFORM:
		return "uniform"
	default:
		return ""
	}
}

func nameTypeToString(t modsv1.NameType) string {
	switch t {
	case modsv1.NameType_NAME_TYPE_PERSONAL:
		return "personal"
	case modsv1.NameType_NAME_TYPE_CORPORATE:
		return "corporate"
	case modsv1.NameType_NAME_TYPE_CONFERENCE:
		return "conference"
	default:
		return ""
	}
}

func namePartTypeToString(t modsv1.NamePartType) string {
	switch t {
	case modsv1.NamePartType_NAME_PART_TYPE_GIVEN:
		return "given"
	case modsv1.NamePartType_NAME_PART_TYPE_FAMILY:
		return "family"
	case modsv1.NamePartType_NAME_PART_TYPE_DATE:
		return "date"
	case modsv1.NamePartType_NAME_PART_TYPE_TERMS_OF_ADDRESS:
		return "termsOfAddress"
	default:
		return ""
	}
}

func roleTermTypeToString(t modsv1.RoleTermType) string {
	switch t {
	case modsv1.RoleTermType_ROLE_TERM_TYPE_TEXT:
		return "text"
	case modsv1.RoleTermType_ROLE_TERM_TYPE_CODE:
		return "code"
	default:
		return ""
	}
}

func relatedItemTypeToString(t modsv1.RelatedItemType) string {
	switch t {
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_HOST:
		return "host"
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_CONSTITUENT:
		return "constituent"
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_SERIES:
		return "series"
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_OTHER_VERSION:
		return "otherVersion"
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_REFERENCES:
		return "references"
	default:
		return ""
	}
}

// XML types for MODS marshaling.

type XMLMods struct {
	XMLName           xml.Name             `xml:"mods"`
	Xmlns             string               `xml:"xmlns,attr"`
	XmlnsXsi          string               `xml:"xmlns:xsi,attr"`
	XsiSchemaLocation string               `xml:"xsi:schemaLocation,attr"`
	Version           string               `xml:"version,attr"`
	TitleInfo         []XMLTitleInfo       `xml:"titleInfo,omitempty"`
	Names             []XMLName            `xml:"name,omitempty"`
	TypeOfResource    []string             `xml:"typeOfResource,omitempty"`
	Genre             []string             `xml:"genre,omitempty"`
	OriginInfo        []XMLOriginInfo      `xml:"originInfo,omitempty"`
	Languages         []XMLLanguage        `xml:"language,omitempty"`
	Abstracts         []string             `xml:"abstract,omitempty"`
	Notes             []string             `xml:"note,omitempty"`
	Subjects          []XMLSubject         `xml:"subject,omitempty"`
	Identifiers       []XMLIdentifier      `xml:"identifier,omitempty"`
	RelatedItems      []XMLRelatedItem     `xml:"relatedItem,omitempty"`
	AccessConditions  []XMLAccessCondition `xml:"accessCondition,omitempty"`
}

type XMLTitleInfo struct {
	Type  string `xml:"type,attr,omitempty"`
	Title string `xml:"title"`
}

type XMLName struct {
	Type         string        `xml:"type,attr,omitempty"`
	NameParts    []XMLNamePart `xml:"namePart,omitempty"`
	Roles        []XMLRole     `xml:"role,omitempty"`
	Affiliations []string      `xml:"affiliation,omitempty"`
}

type XMLNamePart struct {
	Type  string `xml:"type,attr,omitempty"`
	Value string `xml:",chardata"`
}

type XMLRole struct {
	RoleTerm XMLRoleTerm `xml:"roleTerm"`
}

type XMLRoleTerm struct {
	Type  string `xml:"type,attr,omitempty"`
	Value string `xml:",chardata"`
}

type XMLOriginInfo struct {
	Places         []XMLPlace `xml:"place,omitempty"`
	Publishers     []string   `xml:"publisher,omitempty"`
	DateIssued     []string   `xml:"dateIssued,omitempty"`
	DateCreated    []string   `xml:"dateCreated,omitempty"`
	CopyrightDates []string   `xml:"copyrightDate,omitempty"`
	Editions       []string   `xml:"edition,omitempty"`
}

type XMLPlace struct {
	PlaceTerm XMLPlaceTerm `xml:"placeTerm"`
}

type XMLPlaceTerm struct {
	Value string `xml:",chardata"`
}

type XMLLanguage struct {
	LanguageTerm XMLLanguageTerm `xml:"languageTerm"`
}

type XMLLanguageTerm struct {
	Value string `xml:",chardata"`
}

type XMLSubject struct {
	Topics []string `xml:"topic,omitempty"`
}

type XMLIdentifier struct {
	Type  string `xml:"type,attr,omitempty"`
	Value string `xml:",chardata"`
}

type XMLRelatedItem struct {
	Type        string          `xml:"type,attr,omitempty"`
	TitleInfo   []XMLTitleInfo  `xml:"titleInfo,omitempty"`
	Identifiers []XMLIdentifier `xml:"identifier,omitempty"`
}

type XMLAccessCondition struct {
	Href  string `xml:"xlink:href,attr,omitempty"`
	Value string `xml:",chardata"`
}
