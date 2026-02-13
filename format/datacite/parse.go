package datacite

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
)

// Parse reads DataCite XML and returns hub records.
// Handles both bare <resource> elements and OAI-PMH wrapped responses.
func (f *Format) Parse(r io.Reader, _ *format.ParseOptions) ([]*hubv1.Record, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	xmlResources, err := extractResources(data)
	if err != nil {
		return nil, err
	}

	if len(xmlResources) == 0 {
		return nil, fmt.Errorf("no DataCite resource elements found in input")
	}

	var records []*hubv1.Record
	for i, xmlRes := range xmlResources {
		record, err := xmlResourceToHub(xmlRes)
		if err != nil {
			return nil, fmt.Errorf("converting record %d: %w", i, err)
		}
		records = append(records, record)
	}

	return records, nil
}

// extractResources finds all <resource> elements in the XML.
// Works for both bare resource documents and OAI-PMH wrapped responses.
func extractResources(data []byte) ([]*XMLParseResource, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var resources []*XMLParseResource

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

		if start.Name.Local == "resource" {
			var res XMLParseResource
			if err := decoder.DecodeElement(&res, &start); err != nil {
				return nil, fmt.Errorf("decoding resource: %w", err)
			}
			resources = append(resources, &res)
		}
	}

	return resources, nil
}

// xmlResourceToHub converts a parsed DataCite XML resource to a hub record.
func xmlResourceToHub(xmlRes *XMLParseResource) (*hubv1.Record, error) {
	record := &hubv1.Record{
		Publisher: xmlRes.Publisher,
		Language:  xmlRes.Language,
	}

	// Identifier (DOI)
	if xmlRes.Identifier != nil {
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_DOI,
			Value: strings.TrimSpace(xmlRes.Identifier.Value),
		})
	}

	// Titles: first title becomes primary title
	for i, t := range xmlRes.Titles {
		val := strings.TrimSpace(t.Value)
		if val == "" {
			continue
		}
		if i == 0 && t.TitleType == "" {
			record.Title = val
		} else if t.TitleType == "" && record.Title == "" {
			record.Title = val
		} else {
			record.AltTitle = append(record.AltTitle, val)
		}
	}

	// Creators -> contributors with role "creator"
	for _, c := range xmlRes.Creators {
		contributor := creatorToContributor(c)
		if contributor != nil {
			record.Contributors = append(record.Contributors, contributor)
		}
	}

	// Contributors (non-creator)
	for _, c := range xmlRes.Contributors {
		contributor := contributorToHub(c)
		if contributor != nil {
			record.Contributors = append(record.Contributors, contributor)
		}
	}

	// Publication year -> issued date
	if xmlRes.PublicationYear > 0 {
		record.Dates = append(record.Dates, &hubv1.DateValue{
			Type:      hubv1.DateType_DATE_TYPE_ISSUED,
			Year:      xmlRes.PublicationYear,
			Precision: hubv1.DatePrecision_DATE_PRECISION_YEAR,
			Raw:       strconv.Itoa(int(xmlRes.PublicationYear)),
		})
	}

	// Resource type
	if xmlRes.ResourceType != nil {
		record.ResourceType = &hubv1.ResourceType{
			Type:     parseResourceTypeGeneral(xmlRes.ResourceType.ResourceTypeGeneral),
			Original: strings.TrimSpace(xmlRes.ResourceType.Value),
		}
	}

	// Subjects
	for _, s := range xmlRes.Subjects {
		val := strings.TrimSpace(s.Value)
		if val == "" {
			continue
		}
		subject := &hubv1.Subject{
			Value: val,
		}
		if s.SubjectScheme != "" {
			subject.Vocabulary = parseSubjectVocabulary(s.SubjectScheme)
		}
		if s.ValueURI != "" {
			subject.Uri = s.ValueURI
		}
		record.Subjects = append(record.Subjects, subject)
	}

	// Dates
	for _, d := range xmlRes.Dates {
		dv := parseDate(d)
		if dv != nil {
			record.Dates = append(record.Dates, dv)
		}
	}

	// Alternate identifiers
	for _, alt := range xmlRes.AlternateIdentifiers {
		val := strings.TrimSpace(alt.Value)
		if val == "" {
			continue
		}
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:  parseIdentifierType(alt.AlternateIdentifierType),
			Value: val,
		})
	}

	// Related identifiers -> relations
	for _, rel := range xmlRes.RelatedIdentifiers {
		val := strings.TrimSpace(rel.Value)
		if val == "" {
			continue
		}
		record.Relations = append(record.Relations, &hubv1.Relation{
			Type:      parseRelationType(rel.RelationType),
			TargetUri: val,
		})
	}

	// Rights
	for _, r := range xmlRes.RightsList {
		right := &hubv1.Rights{}
		if r.Value != "" {
			right.Statement = strings.TrimSpace(r.Value)
		}
		if r.RightsURI != "" {
			right.Uri = r.RightsURI
		}
		if right.Statement != "" || right.Uri != "" {
			record.Rights = append(record.Rights, right)
		}
	}

	// Descriptions -> abstract (first Abstract type) and notes (others)
	for _, d := range xmlRes.Descriptions {
		val := strings.TrimSpace(d.Value)
		if val == "" {
			continue
		}
		if d.DescriptionType == "Abstract" && record.Abstract == "" {
			record.Abstract = val
		} else {
			record.Notes = append(record.Notes, val)
		}
	}

	// Funding references
	for _, fr := range xmlRes.FundingReferences {
		funder := &hubv1.Funder{
			Name:           fr.FunderName,
			Identifier:     fr.FunderIdentifier,
			IdentifierType: fr.FunderIdentifierType,
		}
		if fr.AwardNumber != "" {
			funder.AwardNumbers = append(funder.AwardNumbers, fr.AwardNumber)
		}
		if fr.AwardTitle != "" {
			funder.AwardTitle = fr.AwardTitle
		}
		record.Funders = append(record.Funders, funder)
	}

	// Source tracking
	sourceID := ""
	if xmlRes.Identifier != nil {
		sourceID = strings.TrimSpace(xmlRes.Identifier.Value)
	}
	record.SourceInfo = &hubv1.SourceInfo{
		Format:        "datacite",
		FormatVersion: Version,
		SourceId:      sourceID,
	}

	return record, nil
}

// creatorToContributor converts a DataCite creator XML element to a hub contributor.
func creatorToContributor(c XMLParseCreator) *hubv1.Contributor {
	contributor := &hubv1.Contributor{
		Role: "creator",
		Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
	}

	// Name from creatorName element
	if c.CreatorName.Value != "" {
		contributor.Name = strings.TrimSpace(c.CreatorName.Value)
	}

	// Name type
	if strings.EqualFold(c.CreatorName.NameType, "Organizational") {
		contributor.Type = hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
	}

	// Parsed name components
	if c.GivenName != "" || c.FamilyName != "" {
		contributor.ParsedName = &hubv1.ParsedName{
			Given:  strings.TrimSpace(c.GivenName),
			Family: strings.TrimSpace(c.FamilyName),
		}
	}

	// Name identifiers (ORCID, etc.)
	for _, ni := range c.NameIdentifiers {
		val := strings.TrimSpace(ni.Value)
		if val == "" {
			continue
		}
		id := &hubv1.Identifier{
			Value: val,
			Type:  parseNameIdentifierScheme(ni.NameIdentifierScheme),
		}
		contributor.Identifiers = append(contributor.Identifiers, id)
	}

	// Affiliations
	for _, a := range c.Affiliations {
		name := strings.TrimSpace(a.Value)
		if name == "" {
			continue
		}
		contributor.Affiliations = append(contributor.Affiliations, &hubv1.Affiliation{
			Name: name,
		})
	}

	// Set primary affiliation from first structured affiliation
	if len(contributor.Affiliations) > 0 {
		contributor.Affiliation = contributor.Affiliations[0].Name
	}

	return contributor
}

// contributorToHub converts a DataCite contributor XML element to a hub contributor.
func contributorToHub(c XMLParseContributor) *hubv1.Contributor {
	contributor := &hubv1.Contributor{
		Role: parseContributorType(c.ContributorType),
		Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
	}

	if c.ContributorName.Value != "" {
		contributor.Name = strings.TrimSpace(c.ContributorName.Value)
	}

	if strings.EqualFold(c.ContributorName.NameType, "Organizational") {
		contributor.Type = hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
	}

	if c.GivenName != "" || c.FamilyName != "" {
		contributor.ParsedName = &hubv1.ParsedName{
			Given:  strings.TrimSpace(c.GivenName),
			Family: strings.TrimSpace(c.FamilyName),
		}
	}

	for _, ni := range c.NameIdentifiers {
		val := strings.TrimSpace(ni.Value)
		if val == "" {
			continue
		}
		contributor.Identifiers = append(contributor.Identifiers, &hubv1.Identifier{
			Value: val,
			Type:  parseNameIdentifierScheme(ni.NameIdentifierScheme),
		})
	}

	for _, a := range c.Affiliations {
		name := strings.TrimSpace(a.Value)
		if name == "" {
			continue
		}
		contributor.Affiliations = append(contributor.Affiliations, &hubv1.Affiliation{
			Name: name,
		})
	}

	if len(contributor.Affiliations) > 0 {
		contributor.Affiliation = contributor.Affiliations[0].Name
	}

	if contributor.Name == "" {
		return nil
	}

	return contributor
}

// parseDate converts a DataCite date XML element to a hub DateValue.
func parseDate(d XMLParseDate) *hubv1.DateValue {
	raw := strings.TrimSpace(d.Value)
	if raw == "" {
		return nil
	}

	dv := &hubv1.DateValue{
		Type: parseDateType(d.DateType),
		Raw:  raw,
	}

	// Try RFC3339
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		dv.Year = int32(t.Year())
		dv.Month = int32(t.Month())
		dv.Day = int32(t.Day())
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
		return dv
	}

	// Try date-only
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		dv.Year = int32(t.Year())
		dv.Month = int32(t.Month())
		dv.Day = int32(t.Day())
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
		return dv
	}

	// Try year-month
	if t, err := time.Parse("2006-01", raw); err == nil {
		dv.Year = int32(t.Year())
		dv.Month = int32(t.Month())
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
		return dv
	}

	// Try year only
	if year, err := strconv.Atoi(raw); err == nil && year > 0 && year < 10000 {
		dv.Year = int32(year)
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_YEAR
		return dv
	}

	return dv
}

// parseResourceTypeGeneral maps a DataCite resourceTypeGeneral string to a hub ResourceTypeValue.
func parseResourceTypeGeneral(s string) hubv1.ResourceTypeValue {
	switch s {
	case "Audiovisual":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO
	case "Book":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK
	case "BookChapter":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER
	case "Collection":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION
	case "ComputationalNotebook":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE
	case "ConferencePaper":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER
	case "ConferenceProceeding":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PROCEEDING
	case "DataPaper":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE
	case "Dataset":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET
	case "Dissertation":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION
	case "Image":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE
	case "InteractiveResource":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_INTERACTIVE
	case "Journal":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_JOURNAL
	case "JournalArticle":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE
	case "Preprint":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT
	case "Report":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT
	case "Software":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE
	case "Sound":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO
	case "Standard":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_STANDARD
	case "Text":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT
	default:
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER
	}
}

// parseDateType maps a DataCite dateType string to a hub DateType.
func parseDateType(s string) hubv1.DateType {
	switch s {
	case "Accepted":
		return hubv1.DateType_DATE_TYPE_ACCEPTED
	case "Available":
		return hubv1.DateType_DATE_TYPE_AVAILABLE
	case "Copyrighted":
		return hubv1.DateType_DATE_TYPE_COPYRIGHT
	case "Created":
		return hubv1.DateType_DATE_TYPE_CREATED
	case "Issued":
		return hubv1.DateType_DATE_TYPE_ISSUED
	case "Submitted":
		return hubv1.DateType_DATE_TYPE_SUBMITTED
	case "Updated":
		return hubv1.DateType_DATE_TYPE_UPDATED
	default:
		return hubv1.DateType_DATE_TYPE_OTHER
	}
}

// parseRelationType maps a DataCite relationType string to a hub RelationType.
func parseRelationType(s string) hubv1.RelationType {
	switch s {
	case "IsCitedBy":
		return hubv1.RelationType_RELATION_TYPE_IS_CITED_BY
	case "Cites":
		return hubv1.RelationType_RELATION_TYPE_CITES
	case "IsSupplementTo":
		return hubv1.RelationType_RELATION_TYPE_SUPPLEMENTS
	case "IsSupplementedBy":
		return hubv1.RelationType_RELATION_TYPE_SUPPLEMENTED_BY
	case "IsPartOf", "IsPublishedIn":
		return hubv1.RelationType_RELATION_TYPE_PART_OF
	case "HasPart":
		return hubv1.RelationType_RELATION_TYPE_HAS_PART
	case "IsVersionOf", "IsNewVersionOf", "IsVariantFormOf":
		return hubv1.RelationType_RELATION_TYPE_VERSION_OF
	case "HasVersion", "IsPreviousVersionOf", "IsOriginalFormOf":
		return hubv1.RelationType_RELATION_TYPE_HAS_VERSION
	case "IsIdenticalTo":
		return hubv1.RelationType_RELATION_TYPE_SAME_AS
	case "IsDerivedFrom":
		return hubv1.RelationType_RELATION_TYPE_DERIVED_FROM
	case "IsSourceOf":
		return hubv1.RelationType_RELATION_TYPE_SOURCE_OF
	case "IsRequiredBy":
		return hubv1.RelationType_RELATION_TYPE_REQUIRED_BY
	case "Requires":
		return hubv1.RelationType_RELATION_TYPE_REQUIRES
	case "IsObsoletedBy", "IsContinuedBy":
		return hubv1.RelationType_RELATION_TYPE_IS_REPLACED_BY
	case "Obsoletes", "Continues":
		return hubv1.RelationType_RELATION_TYPE_REPLACES
	case "IsReferencedBy":
		return hubv1.RelationType_RELATION_TYPE_IS_CITED_BY
	case "References":
		return hubv1.RelationType_RELATION_TYPE_REFERENCES
	default:
		return hubv1.RelationType_RELATION_TYPE_OTHER
	}
}

// parseContributorType maps a DataCite contributorType attribute to a role string.
func parseContributorType(s string) string {
	switch s {
	case "ContactPerson":
		return "contributor"
	case "DataCollector":
		return "data_collector"
	case "DataCurator":
		return "data_curator"
	case "DataManager":
		return "data_manager"
	case "Distributor":
		return "distributor"
	case "Editor":
		return "editor"
	case "HostingInstitution":
		return "host"
	case "Producer":
		return "producer"
	case "ProjectLeader":
		return "project_leader"
	case "ProjectManager":
		return "project_manager"
	case "ProjectMember":
		return "project_member"
	case "Researcher":
		return "researcher"
	case "ResearchGroup":
		return "research_group"
	case "RightsHolder":
		return "rights_holder"
	case "Sponsor":
		return "sponsor"
	case "Supervisor":
		return "supervisor"
	default:
		return "contributor"
	}
}

// parseNameIdentifierScheme maps a name identifier scheme to a hub IdentifierType.
func parseNameIdentifierScheme(scheme string) hubv1.IdentifierType {
	switch strings.ToUpper(scheme) {
	case "ORCID":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID
	case "ISNI":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI
	default:
		return hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED
	}
}

// parseIdentifierType maps an identifier type string to a hub IdentifierType.
func parseIdentifierType(s string) hubv1.IdentifierType {
	switch strings.ToUpper(s) {
	case "DOI":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
	case "URL":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_URL
	case "ISBN":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN
	case "ISSN":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN
	case "HANDLE":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE
	case "ARXIV":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV
	case "PMID":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_PMID
	default:
		return hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED
	}
}

// parseSubjectVocabulary maps a subject scheme string to a hub SubjectVocabulary.
func parseSubjectVocabulary(scheme string) hubv1.SubjectVocabulary {
	switch strings.ToUpper(scheme) {
	case "LCSH":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH
	case "DDC":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_DDC
	case "MESH":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH
	default:
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS
	}
}

// XML types for DataCite parsing.
// These extend the serialization types with additional attributes needed
// for round-trip parsing (e.g., subject scheme, date type, contributor type).

// XMLParseResource is the top-level DataCite resource for parsing.
type XMLParseResource struct {
	XMLName              xml.Name                 `xml:"resource"`
	Identifier           *XMLIdentifier           `xml:"identifier"`
	Creators             []XMLParseCreator        `xml:"creators>creator"`
	Titles               []XMLTitle               `xml:"titles>title"`
	Publisher            string                   `xml:"publisher"`
	PublicationYear      int32                    `xml:"publicationYear"`
	ResourceType         *XMLResourceType         `xml:"resourceType"`
	Subjects             []XMLParseSubject        `xml:"subjects>subject"`
	Contributors         []XMLParseContributor    `xml:"contributors>contributor"`
	Dates                []XMLParseDate           `xml:"dates>date"`
	Language             string                   `xml:"language"`
	AlternateIdentifiers []XMLAlternateIdentifier `xml:"alternateIdentifiers>alternateIdentifier"`
	RelatedIdentifiers   []XMLRelatedIdentifier   `xml:"relatedIdentifiers>relatedIdentifier"`
	RightsList           []XMLParseRights         `xml:"rightsList>rights"`
	Descriptions         []XMLDescription         `xml:"descriptions>description"`
	FundingReferences    []XMLParseFundingRef     `xml:"fundingReferences>fundingReference"`
	Version              string                   `xml:"version"`
}

// XMLParseCreator extends XMLCreator with full parsing support.
type XMLParseCreator struct {
	CreatorName     XMLCreatorName      `xml:"creatorName"`
	GivenName       string              `xml:"givenName"`
	FamilyName      string              `xml:"familyName"`
	NameIdentifiers []XMLNameIdentifier `xml:"nameIdentifier"`
	Affiliations    []XMLAffiliation    `xml:"affiliation"`
}

// XMLParseContributor represents a DataCite contributor for parsing.
type XMLParseContributor struct {
	ContributorType string              `xml:"contributorType,attr"`
	ContributorName XMLCreatorName      `xml:"contributorName"`
	GivenName       string              `xml:"givenName"`
	FamilyName      string              `xml:"familyName"`
	NameIdentifiers []XMLNameIdentifier `xml:"nameIdentifier"`
	Affiliations    []XMLAffiliation    `xml:"affiliation"`
}

// XMLParseSubject extends XMLSubject with scheme and URI attributes.
type XMLParseSubject struct {
	Value         string `xml:",chardata"`
	SubjectScheme string `xml:"subjectScheme,attr"`
	SchemeURI     string `xml:"schemeURI,attr"`
	ValueURI      string `xml:"valueURI,attr"`
}

// XMLParseDate represents a DataCite date for parsing.
type XMLParseDate struct {
	Value    string `xml:",chardata"`
	DateType string `xml:"dateType,attr"`
}

// XMLParseRights extends XMLRights with additional attributes for parsing.
type XMLParseRights struct {
	Value     string `xml:",chardata"`
	RightsURI string `xml:"rightsURI,attr"`
}

// XMLParseFundingRef extends XMLFundingReference with additional fields for parsing.
type XMLParseFundingRef struct {
	FunderName           string `xml:"funderName"`
	FunderIdentifier     string `xml:"funderIdentifier"`
	FunderIdentifierType string `xml:"funderIdentifierType"`
	AwardNumber          string `xml:"awardNumber"`
	AwardTitle           string `xml:"awardTitle"`
}
