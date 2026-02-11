package datacite

import (
	"encoding/xml"
	"fmt"
	"io"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	dcv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/datacite/v4_6"
)

// Serialize writes hub records as DataCite XML.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	// opts reserved for future use (e.g., pretty print, encoding options)
	_ = opts

	for i, record := range records {
		spokeResource, err := hubToSpoke(record)
		if err != nil {
			return fmt.Errorf("converting record %d to spoke: %w", i, err)
		}

		xmlResource := spokeToXML(spokeResource)

		output, err := xml.MarshalIndent(xmlResource, "", "  ")
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

// hubToSpoke converts a hub record to the DataCite spoke proto struct.
func hubToSpoke(record *hubv1.Record) (*dcv1.Resource, error) {
	resource := &dcv1.Resource{
		Publisher: record.Publisher,
		Language:  record.Language,
	}

	// DOI identifier
	for _, id := range record.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI {
			resource.Identifier = &dcv1.Identifier{
				Value:          id.Value,
				IdentifierType: "DOI",
			}
			break
		}
	}

	// Titles
	if record.Title != "" {
		resource.Titles = []*dcv1.Title{{Value: record.Title}}
	}
	for _, alt := range record.AltTitle {
		resource.Titles = append(resource.Titles, &dcv1.Title{
			Value:     alt,
			TitleType: dcv1.TitleType_TITLE_TYPE_ALTERNATIVE_TITLE,
		})
	}

	// Creators from contributors
	for _, c := range record.Contributors {
		role := c.Role
		if role == "author" || role == "creator" || role == "aut" || role == "cre" || role == "" {
			creator := &dcv1.Creator{
				Name: c.Name,
			}
			if c.Type == hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION {
				creator.NameType = "Organizational"
			} else {
				creator.NameType = "Personal"
				if c.ParsedName != nil {
					creator.GivenName = c.ParsedName.Given
					creator.FamilyName = c.ParsedName.Family
				}
			}

			// Affiliations (prefer new field, fall back to deprecated)
			if len(c.Affiliations) > 0 {
				for _, aff := range c.Affiliations {
					creator.Affiliations = append(creator.Affiliations, &dcv1.Affiliation{Name: aff.Name})
				}
			} else if c.Affiliation != "" {
				creator.Affiliations = []*dcv1.Affiliation{{Name: c.Affiliation}}
			}

			// ORCID
			for _, cid := range c.Identifiers {
				if cid.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID {
					creator.NameIdentifiers = []*dcv1.NameIdentifier{{
						Value:                cid.Value,
						NameIdentifierScheme: "ORCID",
						SchemeUri:            "https://orcid.org",
					}}
				}
			}
			resource.Creators = append(resource.Creators, creator)
		}
	}

	// Publication year from dates
	for _, d := range record.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED || d.Type == hubv1.DateType_DATE_TYPE_PUBLISHED {
			if d.Year > 0 {
				resource.PublicationYear = d.Year
				break
			}
		}
	}

	// Resource type
	if record.ResourceType != nil {
		resource.ResourceType = &dcv1.ResourceType{
			ResourceTypeGeneral: mapResourceType(record.ResourceType.Type),
			Value:               record.ResourceType.Original,
		}
	}

	// Subjects
	for _, s := range record.Subjects {
		resource.Subjects = append(resource.Subjects, &dcv1.Subject{Value: s.Value})
	}

	// Descriptions
	if record.Abstract != "" {
		resource.Descriptions = []*dcv1.Description{{
			Value:           record.Abstract,
			DescriptionType: dcv1.DescriptionType_DESCRIPTION_TYPE_ABSTRACT,
		}}
	}

	// Rights
	for _, r := range record.Rights {
		right := &dcv1.Rights{RightsUri: r.Uri}
		if r.Statement != "" {
			right.Value = r.Statement
		}
		resource.RightsList = append(resource.RightsList, right)
	}

	// Funders
	for _, f := range record.Funders {
		ref := &dcv1.FundingReference{
			FunderName:           f.Name,
			FunderIdentifier:     f.Identifier,
			FunderIdentifierType: f.IdentifierType,
		}
		if len(f.AwardNumbers) > 0 {
			ref.AwardNumber = f.AwardNumbers[0]
		}
		resource.FundingReferences = append(resource.FundingReferences, ref)
	}

	// Alternate identifiers
	for _, id := range record.Identifiers {
		if id.Type != hubv1.IdentifierType_IDENTIFIER_TYPE_DOI {
			resource.AlternateIdentifiers = append(resource.AlternateIdentifiers, &dcv1.AlternateIdentifier{
				Value:                   id.Value,
				AlternateIdentifierType: identifierTypeToString(id.Type),
			})
		}
	}

	// Related identifiers from relations
	for _, rel := range record.Relations {
		if rel.TargetUri != "" {
			resource.RelatedIdentifiers = append(resource.RelatedIdentifiers, &dcv1.RelatedIdentifier{
				Value:        rel.TargetUri,
				RelationType: mapRelationType(rel.Type),
			})
		}
	}

	return resource, nil
}

// mapResourceType maps hub resource type to DataCite general type.
func mapResourceType(rt hubv1.ResourceTypeValue) dcv1.ResourceTypeGeneral {
	switch rt {
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_WORKING_PAPER,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_JOURNAL_ARTICLE
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_BOOK
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_DATASET
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_SOFTWARE
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_DISSERTATION
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_TECHNICAL_REPORT:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_REPORT
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_IMAGE
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_AUDIOVISUAL
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_SOUND
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_COLLECTION
	default:
		return dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_OTHER
	}
}

// mapRelationType maps hub relation type to DataCite relation type.
func mapRelationType(rt hubv1.RelationType) dcv1.RelationType {
	switch rt {
	case hubv1.RelationType_RELATION_TYPE_PART_OF:
		return dcv1.RelationType_RELATION_TYPE_IS_PART_OF
	case hubv1.RelationType_RELATION_TYPE_HAS_PART:
		return dcv1.RelationType_RELATION_TYPE_HAS_PART
	case hubv1.RelationType_RELATION_TYPE_REFERENCES:
		return dcv1.RelationType_RELATION_TYPE_REFERENCES
	case hubv1.RelationType_RELATION_TYPE_IS_CITED_BY:
		return dcv1.RelationType_RELATION_TYPE_IS_CITED_BY
	case hubv1.RelationType_RELATION_TYPE_VERSION_OF:
		return dcv1.RelationType_RELATION_TYPE_IS_VERSION_OF
	default:
		return dcv1.RelationType_RELATION_TYPE_UNSPECIFIED
	}
}

// identifierTypeToString converts identifier type to string.
func identifierTypeToString(t hubv1.IdentifierType) string {
	switch t {
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
		return "ISBN"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
		return "ISSN"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
		return "URL"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE:
		return "Handle"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV:
		return "arXiv"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_PMID:
		return "PMID"
	default:
		return "Other"
	}
}

// spokeToXML converts a spoke proto struct to an XML-marshalable struct.
func spokeToXML(spoke *dcv1.Resource) *XMLResource {
	xmlRes := &XMLResource{
		Xmlns:    "http://datacite.org/schema/kernel-4",
		XmlnsXsi: "http://www.w3.org/2001/XMLSchema-instance",
		XsiSchemaLocation: "http://datacite.org/schema/kernel-4 " +
			"http://schema.datacite.org/meta/kernel-4.6/metadata.xsd",
		Publisher:       spoke.Publisher,
		PublicationYear: spoke.PublicationYear,
		Language:        spoke.Language,
		Version:         spoke.Version,
	}

	// Identifier
	if spoke.Identifier != nil {
		xmlRes.Identifier = &XMLIdentifier{
			IdentifierType: spoke.Identifier.IdentifierType,
			Value:          spoke.Identifier.Value,
		}
	}

	// Creators
	for _, c := range spoke.Creators {
		creator := XMLCreator{
			CreatorName: XMLCreatorName{
				NameType: c.NameType,
				Value:    c.Name,
			},
			GivenName:  c.GivenName,
			FamilyName: c.FamilyName,
		}
		for _, ni := range c.NameIdentifiers {
			creator.NameIdentifiers = append(creator.NameIdentifiers, XMLNameIdentifier{
				NameIdentifierScheme: ni.NameIdentifierScheme,
				SchemeURI:            ni.SchemeUri,
				Value:                ni.Value,
			})
		}
		for _, a := range c.Affiliations {
			creator.Affiliations = append(creator.Affiliations, XMLAffiliation{Value: a.Name})
		}
		xmlRes.Creators = append(xmlRes.Creators, creator)
	}

	// Titles
	for _, t := range spoke.Titles {
		xmlRes.Titles = append(xmlRes.Titles, XMLTitle{
			TitleType: titleTypeToString(t.TitleType),
			Value:     t.Value,
		})
	}

	// Subjects
	for _, s := range spoke.Subjects {
		xmlRes.Subjects = append(xmlRes.Subjects, XMLSubject{Value: s.Value})
	}

	// Resource type
	if spoke.ResourceType != nil {
		xmlRes.ResourceType = &XMLResourceType{
			ResourceTypeGeneral: resourceTypeGeneralToString(spoke.ResourceType.ResourceTypeGeneral),
			Value:               spoke.ResourceType.Value,
		}
	}

	// Descriptions
	for _, d := range spoke.Descriptions {
		xmlRes.Descriptions = append(xmlRes.Descriptions, XMLDescription{
			DescriptionType: descriptionTypeToString(d.DescriptionType),
			Value:           d.Value,
		})
	}

	// Rights
	for _, r := range spoke.RightsList {
		xmlRes.RightsList = append(xmlRes.RightsList, XMLRights{
			RightsURI: r.RightsUri,
			Value:     r.Value,
		})
	}

	// Funding references
	for _, f := range spoke.FundingReferences {
		xmlRes.FundingReferences = append(xmlRes.FundingReferences, XMLFundingReference{
			FunderName:  f.FunderName,
			AwardNumber: f.AwardNumber,
		})
	}

	// Alternate identifiers
	for _, a := range spoke.AlternateIdentifiers {
		xmlRes.AlternateIdentifiers = append(xmlRes.AlternateIdentifiers, XMLAlternateIdentifier{
			AlternateIdentifierType: a.AlternateIdentifierType,
			Value:                   a.Value,
		})
	}

	// Related identifiers
	for _, r := range spoke.RelatedIdentifiers {
		xmlRes.RelatedIdentifiers = append(xmlRes.RelatedIdentifiers, XMLRelatedIdentifier{
			RelatedIdentifierType: relatedIdentifierTypeToString(r.RelatedIdentifierType),
			RelationType:          relationTypeToString(r.RelationType),
			Value:                 r.Value,
		})
	}

	return xmlRes
}

func titleTypeToString(tt dcv1.TitleType) string {
	switch tt {
	case dcv1.TitleType_TITLE_TYPE_ALTERNATIVE_TITLE:
		return "AlternativeTitle"
	case dcv1.TitleType_TITLE_TYPE_SUBTITLE:
		return "Subtitle"
	case dcv1.TitleType_TITLE_TYPE_TRANSLATED_TITLE:
		return "TranslatedTitle"
	default:
		return ""
	}
}

func resourceTypeGeneralToString(rt dcv1.ResourceTypeGeneral) string {
	switch rt {
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_AUDIOVISUAL:
		return "Audiovisual"
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_BOOK:
		return "Book"
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_COLLECTION:
		return "Collection"
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_DATASET:
		return "Dataset"
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_DISSERTATION:
		return "Dissertation"
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_IMAGE:
		return "Image"
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_JOURNAL_ARTICLE:
		return "JournalArticle"
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_REPORT:
		return "Report"
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_SOFTWARE:
		return "Software"
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_SOUND:
		return "Sound"
	case dcv1.ResourceTypeGeneral_RESOURCE_TYPE_GENERAL_TEXT:
		return "Text"
	default:
		return "Other"
	}
}

func descriptionTypeToString(dt dcv1.DescriptionType) string {
	switch dt {
	case dcv1.DescriptionType_DESCRIPTION_TYPE_ABSTRACT:
		return "Abstract"
	case dcv1.DescriptionType_DESCRIPTION_TYPE_METHODS:
		return "Methods"
	case dcv1.DescriptionType_DESCRIPTION_TYPE_TABLE_OF_CONTENTS:
		return "TableOfContents"
	case dcv1.DescriptionType_DESCRIPTION_TYPE_TECHNICAL_INFO:
		return "TechnicalInfo"
	default:
		return "Other"
	}
}

func relationTypeToString(rt dcv1.RelationType) string {
	switch rt {
	case dcv1.RelationType_RELATION_TYPE_IS_PART_OF:
		return "IsPartOf"
	case dcv1.RelationType_RELATION_TYPE_HAS_PART:
		return "HasPart"
	case dcv1.RelationType_RELATION_TYPE_IS_CITED_BY:
		return "IsCitedBy"
	case dcv1.RelationType_RELATION_TYPE_REFERENCES:
		return "References"
	case dcv1.RelationType_RELATION_TYPE_IS_VERSION_OF:
		return "IsVersionOf"
	default:
		return "IsRelatedTo"
	}
}

func relatedIdentifierTypeToString(t dcv1.RelatedIdentifierType) string {
	switch t {
	case dcv1.RelatedIdentifierType_RELATED_IDENTIFIER_TYPE_DOI:
		return "DOI"
	case dcv1.RelatedIdentifierType_RELATED_IDENTIFIER_TYPE_URL:
		return "URL"
	case dcv1.RelatedIdentifierType_RELATED_IDENTIFIER_TYPE_ARK:
		return "ARK"
	case dcv1.RelatedIdentifierType_RELATED_IDENTIFIER_TYPE_ARXIV:
		return "arXiv"
	case dcv1.RelatedIdentifierType_RELATED_IDENTIFIER_TYPE_HANDLE:
		return "Handle"
	case dcv1.RelatedIdentifierType_RELATED_IDENTIFIER_TYPE_ISBN:
		return "ISBN"
	case dcv1.RelatedIdentifierType_RELATED_IDENTIFIER_TYPE_ISSN:
		return "ISSN"
	case dcv1.RelatedIdentifierType_RELATED_IDENTIFIER_TYPE_PMID:
		return "PMID"
	case dcv1.RelatedIdentifierType_RELATED_IDENTIFIER_TYPE_URN:
		return "URN"
	default:
		return "URL"
	}
}

// XML types for DataCite marshaling.

type XMLResource struct {
	XMLName              xml.Name                 `xml:"resource"`
	Xmlns                string                   `xml:"xmlns,attr"`
	XmlnsXsi             string                   `xml:"xmlns:xsi,attr"`
	XsiSchemaLocation    string                   `xml:"xsi:schemaLocation,attr"`
	Identifier           *XMLIdentifier           `xml:"identifier"`
	Creators             []XMLCreator             `xml:"creators>creator"`
	Titles               []XMLTitle               `xml:"titles>title"`
	Publisher            string                   `xml:"publisher"`
	PublicationYear      int32                    `xml:"publicationYear"`
	ResourceType         *XMLResourceType         `xml:"resourceType,omitempty"`
	Subjects             []XMLSubject             `xml:"subjects>subject,omitempty"`
	Language             string                   `xml:"language,omitempty"`
	AlternateIdentifiers []XMLAlternateIdentifier `xml:"alternateIdentifiers>alternateIdentifier,omitempty"`
	RelatedIdentifiers   []XMLRelatedIdentifier   `xml:"relatedIdentifiers>relatedIdentifier,omitempty"`
	RightsList           []XMLRights              `xml:"rightsList>rights,omitempty"`
	Descriptions         []XMLDescription         `xml:"descriptions>description,omitempty"`
	FundingReferences    []XMLFundingReference    `xml:"fundingReferences>fundingReference,omitempty"`
	Version              string                   `xml:"version,omitempty"`
}

type XMLIdentifier struct {
	IdentifierType string `xml:"identifierType,attr"`
	Value          string `xml:",chardata"`
}

type XMLCreator struct {
	CreatorName     XMLCreatorName      `xml:"creatorName"`
	GivenName       string              `xml:"givenName,omitempty"`
	FamilyName      string              `xml:"familyName,omitempty"`
	NameIdentifiers []XMLNameIdentifier `xml:"nameIdentifier,omitempty"`
	Affiliations    []XMLAffiliation    `xml:"affiliation,omitempty"`
}

type XMLCreatorName struct {
	NameType string `xml:"nameType,attr,omitempty"`
	Value    string `xml:",chardata"`
}

type XMLNameIdentifier struct {
	NameIdentifierScheme string `xml:"nameIdentifierScheme,attr"`
	SchemeURI            string `xml:"schemeURI,attr,omitempty"`
	Value                string `xml:",chardata"`
}

type XMLAffiliation struct {
	Value string `xml:",chardata"`
}

type XMLTitle struct {
	TitleType string `xml:"titleType,attr,omitempty"`
	Value     string `xml:",chardata"`
}

type XMLSubject struct {
	Value string `xml:",chardata"`
}

type XMLResourceType struct {
	ResourceTypeGeneral string `xml:"resourceTypeGeneral,attr"`
	Value               string `xml:",chardata"`
}

type XMLDescription struct {
	DescriptionType string `xml:"descriptionType,attr"`
	Value           string `xml:",chardata"`
}

type XMLRights struct {
	RightsURI string `xml:"rightsURI,attr,omitempty"`
	Value     string `xml:",chardata"`
}

type XMLFundingReference struct {
	FunderName  string `xml:"funderName"`
	AwardNumber string `xml:"awardNumber,omitempty"`
}

type XMLAlternateIdentifier struct {
	AlternateIdentifierType string `xml:"alternateIdentifierType,attr"`
	Value                   string `xml:",chardata"`
}

type XMLRelatedIdentifier struct {
	RelatedIdentifierType string `xml:"relatedIdentifierType,attr,omitempty"`
	RelationType          string `xml:"relationType,attr"`
	Value                 string `xml:",chardata"`
}
