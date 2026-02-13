package datacite

import (
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestParseDataCiteRecord(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<resource xmlns="http://datacite.org/schema/kernel-4"
          xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
          xsi:schemaLocation="http://datacite.org/schema/kernel-4 http://schema.datacite.org/meta/kernel-4.6/metadata.xsd">
  <identifier identifierType="DOI">10.5281/zenodo.1234567</identifier>
  <creators>
    <creator>
      <creatorName nameType="Personal">Doe, Jane</creatorName>
      <givenName>Jane</givenName>
      <familyName>Doe</familyName>
      <nameIdentifier nameIdentifierScheme="ORCID" schemeURI="https://orcid.org">https://orcid.org/0000-0002-1825-0097</nameIdentifier>
      <affiliation>University of Example</affiliation>
    </creator>
    <creator>
      <creatorName nameType="Personal">Smith, John</creatorName>
      <givenName>John</givenName>
      <familyName>Smith</familyName>
      <nameIdentifier nameIdentifierScheme="ORCID" schemeURI="https://orcid.org">https://orcid.org/0000-0001-5109-3700</nameIdentifier>
      <affiliation>Another University</affiliation>
    </creator>
  </creators>
  <titles>
    <title>A Comprehensive Study of Metadata Crosswalking</title>
    <title titleType="Subtitle">Methods and Applications</title>
  </titles>
  <publisher>Zenodo</publisher>
  <publicationYear>2024</publicationYear>
  <resourceType resourceTypeGeneral="Dataset">Research Data</resourceType>
  <subjects>
    <subject subjectScheme="LCSH" valueURI="http://id.loc.gov/authorities/subjects/sh85082139">Metadata</subject>
    <subject>Digital Libraries</subject>
    <subject subjectScheme="DDC">025.3</subject>
  </subjects>
  <dates>
    <date dateType="Created">2024-01-15</date>
    <date dateType="Issued">2024-03-01</date>
  </dates>
  <language>en</language>
  <alternateIdentifiers>
    <alternateIdentifier alternateIdentifierType="URL">https://zenodo.org/record/1234567</alternateIdentifier>
  </alternateIdentifiers>
  <relatedIdentifiers>
    <relatedIdentifier relatedIdentifierType="DOI" relationType="IsSupplementTo">10.1000/xyz123</relatedIdentifier>
  </relatedIdentifiers>
  <rightsList>
    <rights rightsURI="https://creativecommons.org/licenses/by/4.0/">Creative Commons Attribution 4.0 International</rights>
  </rightsList>
  <descriptions>
    <description descriptionType="Abstract">This dataset contains metadata crosswalk mappings between various library and repository standards.</description>
    <description descriptionType="Methods">Data was collected from 50 institutional repositories.</description>
  </descriptions>
  <fundingReferences>
    <fundingReference>
      <funderName>National Science Foundation</funderName>
      <funderIdentifier>https://doi.org/10.13039/100000001</funderIdentifier>
      <funderIdentifierType>Crossref Funder ID</funderIdentifierType>
      <awardNumber>1234567</awardNumber>
      <awardTitle>Metadata Standards Research</awardTitle>
    </fundingReference>
  </fundingReferences>
</resource>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	r := records[0]

	// DOI identifier
	var foundDOI bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI && id.Value == "10.5281/zenodo.1234567" {
			foundDOI = true
		}
	}
	if !foundDOI {
		t.Error("DOI 10.5281/zenodo.1234567 not found")
	}

	// Title
	if r.Title != "A Comprehensive Study of Metadata Crosswalking" {
		t.Errorf("Title: got %q", r.Title)
	}

	// Alt title (subtitle)
	if len(r.AltTitle) != 1 || r.AltTitle[0] != "Methods and Applications" {
		t.Errorf("AltTitle: got %v", r.AltTitle)
	}

	// Publisher
	if r.Publisher != "Zenodo" {
		t.Errorf("Publisher: got %q", r.Publisher)
	}

	// Language
	if r.Language != "en" {
		t.Errorf("Language: got %q", r.Language)
	}

	// Creators -> contributors with role "creator"
	if len(r.Contributors) != 2 {
		t.Fatalf("Expected 2 contributors, got %d", len(r.Contributors))
	}

	c0 := r.Contributors[0]
	if c0.Name != "Doe, Jane" {
		t.Errorf("Creator 0 name: got %q", c0.Name)
	}
	if c0.Role != "creator" {
		t.Errorf("Creator 0 role: got %q", c0.Role)
	}
	if c0.ParsedName == nil || c0.ParsedName.Given != "Jane" || c0.ParsedName.Family != "Doe" {
		t.Errorf("Creator 0 parsed name: got %v", c0.ParsedName)
	}

	// ORCID
	var foundOrcid bool
	for _, id := range c0.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID && id.Value == "https://orcid.org/0000-0002-1825-0097" {
			foundOrcid = true
		}
	}
	if !foundOrcid {
		t.Error("ORCID for creator 0 not found")
	}

	// Affiliation
	if c0.Affiliation != "University of Example" {
		t.Errorf("Creator 0 affiliation: got %q", c0.Affiliation)
	}
	if len(c0.Affiliations) != 1 || c0.Affiliations[0].Name != "University of Example" {
		t.Errorf("Creator 0 affiliations: got %v", c0.Affiliations)
	}

	// Second creator
	c1 := r.Contributors[1]
	if c1.Name != "Smith, John" {
		t.Errorf("Creator 1 name: got %q", c1.Name)
	}

	// Resource type
	if r.ResourceType == nil {
		t.Fatal("ResourceType is nil")
	}
	if r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET {
		t.Errorf("ResourceType: got %v", r.ResourceType.Type)
	}
	if r.ResourceType.Original != "Research Data" {
		t.Errorf("ResourceType.Original: got %q", r.ResourceType.Original)
	}

	// Subjects
	if len(r.Subjects) != 3 {
		t.Fatalf("Expected 3 subjects, got %d", len(r.Subjects))
	}
	// First subject with LCSH vocabulary and URI
	if r.Subjects[0].Value != "Metadata" {
		t.Errorf("Subject 0 value: got %q", r.Subjects[0].Value)
	}
	if r.Subjects[0].Vocabulary != hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH {
		t.Errorf("Subject 0 vocabulary: got %v", r.Subjects[0].Vocabulary)
	}
	if r.Subjects[0].Uri != "http://id.loc.gov/authorities/subjects/sh85082139" {
		t.Errorf("Subject 0 URI: got %q", r.Subjects[0].Uri)
	}
	// Second subject: plain keyword
	if r.Subjects[1].Value != "Digital Libraries" {
		t.Errorf("Subject 1 value: got %q", r.Subjects[1].Value)
	}
	// Third subject: DDC
	if r.Subjects[2].Vocabulary != hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_DDC {
		t.Errorf("Subject 2 vocabulary: got %v", r.Subjects[2].Vocabulary)
	}

	// Dates: publicationYear creates an issued date, plus Created and Issued from <dates>
	if len(r.Dates) != 3 {
		t.Fatalf("Expected 3 dates, got %d", len(r.Dates))
	}
	// First date is from publicationYear (issued, year precision)
	if r.Dates[0].Type != hubv1.DateType_DATE_TYPE_ISSUED {
		t.Errorf("Date 0 type: got %v", r.Dates[0].Type)
	}
	if r.Dates[0].Year != 2024 {
		t.Errorf("Date 0 year: got %d", r.Dates[0].Year)
	}
	// Second date: Created 2024-01-15
	if r.Dates[1].Type != hubv1.DateType_DATE_TYPE_CREATED {
		t.Errorf("Date 1 type: got %v", r.Dates[1].Type)
	}
	if r.Dates[1].Year != 2024 || r.Dates[1].Month != 1 || r.Dates[1].Day != 15 {
		t.Errorf("Date 1: got %d-%d-%d", r.Dates[1].Year, r.Dates[1].Month, r.Dates[1].Day)
	}
	// Third date: Issued 2024-03-01
	if r.Dates[2].Type != hubv1.DateType_DATE_TYPE_ISSUED {
		t.Errorf("Date 2 type: got %v", r.Dates[2].Type)
	}
	if r.Dates[2].Year != 2024 || r.Dates[2].Month != 3 || r.Dates[2].Day != 1 {
		t.Errorf("Date 2: got %d-%d-%d", r.Dates[2].Year, r.Dates[2].Month, r.Dates[2].Day)
	}

	// Alternate identifiers
	var foundURL bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_URL && id.Value == "https://zenodo.org/record/1234567" {
			foundURL = true
		}
	}
	if !foundURL {
		t.Error("Alternate identifier URL not found")
	}

	// Related identifiers -> relations
	if len(r.Relations) != 1 {
		t.Fatalf("Expected 1 relation, got %d", len(r.Relations))
	}
	if r.Relations[0].Type != hubv1.RelationType_RELATION_TYPE_SUPPLEMENTS {
		t.Errorf("Relation type: got %v", r.Relations[0].Type)
	}
	if r.Relations[0].TargetUri != "10.1000/xyz123" {
		t.Errorf("Relation target URI: got %q", r.Relations[0].TargetUri)
	}

	// Rights
	if len(r.Rights) != 1 {
		t.Fatalf("Expected 1 rights, got %d", len(r.Rights))
	}
	if r.Rights[0].Statement != "Creative Commons Attribution 4.0 International" {
		t.Errorf("Rights statement: got %q", r.Rights[0].Statement)
	}
	if r.Rights[0].Uri != "https://creativecommons.org/licenses/by/4.0/" {
		t.Errorf("Rights URI: got %q", r.Rights[0].Uri)
	}

	// Descriptions -> abstract
	if r.Abstract != "This dataset contains metadata crosswalk mappings between various library and repository standards." {
		t.Errorf("Abstract: got %q", r.Abstract)
	}
	// Second description (Methods) goes to notes
	if len(r.Notes) != 1 || r.Notes[0] != "Data was collected from 50 institutional repositories." {
		t.Errorf("Notes: got %v", r.Notes)
	}

	// Funding references
	if len(r.Funders) != 1 {
		t.Fatalf("Expected 1 funder, got %d", len(r.Funders))
	}
	if r.Funders[0].Name != "National Science Foundation" {
		t.Errorf("Funder name: got %q", r.Funders[0].Name)
	}
	if r.Funders[0].Identifier != "https://doi.org/10.13039/100000001" {
		t.Errorf("Funder identifier: got %q", r.Funders[0].Identifier)
	}
	if r.Funders[0].IdentifierType != "Crossref Funder ID" {
		t.Errorf("Funder identifier type: got %q", r.Funders[0].IdentifierType)
	}
	if len(r.Funders[0].AwardNumbers) != 1 || r.Funders[0].AwardNumbers[0] != "1234567" {
		t.Errorf("Funder award numbers: got %v", r.Funders[0].AwardNumbers)
	}
	if r.Funders[0].AwardTitle != "Metadata Standards Research" {
		t.Errorf("Funder award title: got %q", r.Funders[0].AwardTitle)
	}

	// Source info
	if r.SourceInfo == nil {
		t.Fatal("SourceInfo is nil")
	}
	if r.SourceInfo.Format != "datacite" {
		t.Errorf("SourceInfo.Format: got %q", r.SourceInfo.Format)
	}
	if r.SourceInfo.FormatVersion != Version {
		t.Errorf("SourceInfo.FormatVersion: got %q", r.SourceInfo.FormatVersion)
	}
	if r.SourceInfo.SourceId != "10.5281/zenodo.1234567" {
		t.Errorf("SourceInfo.SourceId: got %q", r.SourceInfo.SourceId)
	}
}

func TestParseOAIPMHWrapped(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/">
  <responseDate>2024-01-01T00:00:00Z</responseDate>
  <GetRecord>
    <record>
      <metadata>
        <resource xmlns="http://datacite.org/schema/kernel-4">
          <identifier identifierType="DOI">10.5072/example</identifier>
          <creators>
            <creator>
              <creatorName>Test Author</creatorName>
            </creator>
          </creators>
          <titles>
            <title>OAI-PMH Wrapped Record</title>
          </titles>
          <publisher>Test Publisher</publisher>
          <publicationYear>2024</publicationYear>
          <resourceType resourceTypeGeneral="Text">Article</resourceType>
        </resource>
      </metadata>
    </record>
  </GetRecord>
</OAI-PMH>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Title != "OAI-PMH Wrapped Record" {
		t.Errorf("Title: got %q", r.Title)
	}
	if r.Publisher != "Test Publisher" {
		t.Errorf("Publisher: got %q", r.Publisher)
	}
	if r.ResourceType == nil || r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT {
		t.Errorf("ResourceType: got %v", r.ResourceType)
	}
}

func TestParseMultipleRecords(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<records>
  <resource xmlns="http://datacite.org/schema/kernel-4">
    <identifier identifierType="DOI">10.5072/first</identifier>
    <creators><creator><creatorName>Author One</creatorName></creator></creators>
    <titles><title>First Record</title></titles>
    <publisher>Publisher A</publisher>
    <publicationYear>2023</publicationYear>
    <resourceType resourceTypeGeneral="Dataset">Data</resourceType>
  </resource>
  <resource xmlns="http://datacite.org/schema/kernel-4">
    <identifier identifierType="DOI">10.5072/second</identifier>
    <creators><creator><creatorName>Author Two</creatorName></creator></creators>
    <titles><title>Second Record</title></titles>
    <publisher>Publisher B</publisher>
    <publicationYear>2024</publicationYear>
    <resourceType resourceTypeGeneral="Software">Code</resourceType>
  </resource>
</records>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("Expected 2 records, got %d", len(records))
	}

	if records[0].Title != "First Record" {
		t.Errorf("Record 0 title: got %q", records[0].Title)
	}
	if records[1].Title != "Second Record" {
		t.Errorf("Record 1 title: got %q", records[1].Title)
	}
	if records[1].ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE {
		t.Errorf("Record 1 resource type: got %v", records[1].ResourceType.Type)
	}
}

func TestParseContributors(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<resource xmlns="http://datacite.org/schema/kernel-4">
  <identifier identifierType="DOI">10.5072/contrib-test</identifier>
  <creators>
    <creator>
      <creatorName nameType="Personal">Creator, Test</creatorName>
      <givenName>Test</givenName>
      <familyName>Creator</familyName>
    </creator>
  </creators>
  <titles><title>Contributor Test</title></titles>
  <publisher>Test</publisher>
  <publicationYear>2024</publicationYear>
  <resourceType resourceTypeGeneral="Text">Article</resourceType>
  <contributors>
    <contributor contributorType="Editor">
      <contributorName nameType="Personal">Editor, Test</contributorName>
      <givenName>Test</givenName>
      <familyName>Editor</familyName>
    </contributor>
    <contributor contributorType="Supervisor">
      <contributorName nameType="Personal">Super, Visor</contributorName>
      <givenName>Visor</givenName>
      <familyName>Super</familyName>
    </contributor>
    <contributor contributorType="HostingInstitution">
      <contributorName nameType="Organizational">Example University</contributorName>
    </contributor>
  </contributors>
</resource>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	r := records[0]
	if len(r.Contributors) != 4 {
		t.Fatalf("Expected 4 contributors, got %d", len(r.Contributors))
	}

	// Creator
	if r.Contributors[0].Role != "creator" {
		t.Errorf("Contributor 0 role: got %q", r.Contributors[0].Role)
	}

	// Editor
	if r.Contributors[1].Role != "editor" {
		t.Errorf("Contributor 1 role: got %q", r.Contributors[1].Role)
	}
	if r.Contributors[1].Name != "Editor, Test" {
		t.Errorf("Contributor 1 name: got %q", r.Contributors[1].Name)
	}

	// Supervisor
	if r.Contributors[2].Role != "supervisor" {
		t.Errorf("Contributor 2 role: got %q", r.Contributors[2].Role)
	}

	// Hosting institution (organizational)
	if r.Contributors[3].Role != "host" {
		t.Errorf("Contributor 3 role: got %q", r.Contributors[3].Role)
	}
	if r.Contributors[3].Type != hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION {
		t.Errorf("Contributor 3 type: got %v", r.Contributors[3].Type)
	}
}

func TestParseEmptyInput(t *testing.T) {
	f := &Format{}
	_, err := f.Parse(strings.NewReader(""), nil)
	if err == nil {
		t.Error("Expected error for empty input")
	}
}

func TestParseNoResource(t *testing.T) {
	f := &Format{}
	_, err := f.Parse(strings.NewReader("<root><other>data</other></root>"), nil)
	if err == nil {
		t.Error("Expected error when no resource element found")
	}
}

func TestParseMinimalRecord(t *testing.T) {
	// DataCite requires: identifier, creators, titles, publisher, publicationYear, resourceType
	input := `<?xml version="1.0" encoding="UTF-8"?>
<resource xmlns="http://datacite.org/schema/kernel-4">
  <identifier identifierType="DOI">10.5072/minimal</identifier>
  <creators>
    <creator>
      <creatorName>Minimal Author</creatorName>
    </creator>
  </creators>
  <titles>
    <title>Minimal Record</title>
  </titles>
  <publisher>Minimal Publisher</publisher>
  <publicationYear>2024</publicationYear>
  <resourceType resourceTypeGeneral="Other">Test</resourceType>
</resource>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Title != "Minimal Record" {
		t.Errorf("Title: got %q", r.Title)
	}
	if r.Publisher != "Minimal Publisher" {
		t.Errorf("Publisher: got %q", r.Publisher)
	}
	if len(r.Contributors) != 1 || r.Contributors[0].Name != "Minimal Author" {
		t.Errorf("Contributors: got %v", r.Contributors)
	}
	if r.ResourceType == nil || r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER {
		t.Errorf("ResourceType: got %v", r.ResourceType)
	}
}
