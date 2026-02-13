package crossref

import (
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

func TestParseJournalArticle(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<doi_batch xmlns="http://www.crossref.org/schema/5.3.1" version="5.3.1">
  <head>
    <doi_batch_id>test_batch_001</doi_batch_id>
    <timestamp>20250101120000</timestamp>
    <depositor>
      <depositor_name>Test Depositor</depositor_name>
      <email_address>test@example.com</email_address>
    </depositor>
    <registrant>Test Registrant</registrant>
  </head>
  <body>
    <journal>
      <journal_metadata>
        <full_title>Journal of Testing</full_title>
        <issn>1234-5678</issn>
      </journal_metadata>
      <journal_issue>
        <publication_date media_type="online">
          <year>2025</year>
          <month>3</month>
        </publication_date>
        <volume>42</volume>
        <issue>7</issue>
      </journal_issue>
      <journal_article>
        <titles>
          <title>A Novel Approach to Unit Testing</title>
        </titles>
        <contributors>
          <person_name contributor_role="author" sequence="first">
            <given_name>Alice</given_name>
            <surname>Smith</surname>
            <ORCID>https://orcid.org/0000-0001-2345-6789</ORCID>
          </person_name>
          <person_name contributor_role="author" sequence="additional">
            <given_name>Bob</given_name>
            <surname>Jones</surname>
          </person_name>
        </contributors>
        <publication_date media_type="online">
          <year>2025</year>
          <month>3</month>
          <day>15</day>
        </publication_date>
        <doi_data>
          <doi>10.1234/test.2025.001</doi>
          <resource>https://example.com/article/001</resource>
        </doi_data>
      </journal_article>
    </journal>
  </body>
</doi_batch>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]

	// Title
	if r.Title != "A Novel Approach to Unit Testing" {
		t.Errorf("Title: got %q, want %q", r.Title, "A Novel Approach to Unit Testing")
	}

	// Resource type
	if r.ResourceType == nil || r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE {
		t.Errorf("ResourceType: expected ARTICLE, got %v", r.ResourceType)
	}

	// DOI
	var foundDOI bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI && id.Value == "10.1234/test.2025.001" {
			foundDOI = true
		}
	}
	if !foundDOI {
		t.Error("DOI 10.1234/test.2025.001 not found")
	}

	// ISSN from journal metadata
	var foundISSN bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN && id.Value == "1234-5678" {
			foundISSN = true
		}
	}
	if !foundISSN {
		t.Error("ISSN 1234-5678 not found")
	}

	// Contributors
	if len(r.Contributors) != 2 {
		t.Fatalf("expected 2 contributors, got %d", len(r.Contributors))
	}

	c0 := r.Contributors[0]
	if c0.ParsedName == nil || c0.ParsedName.Given != "Alice" || c0.ParsedName.Family != "Smith" {
		t.Errorf("contributor 0 name: got given=%q family=%q", c0.ParsedName.GetGiven(), c0.ParsedName.GetFamily())
	}
	if c0.Role != "author" {
		t.Errorf("contributor 0 role: got %q, want %q", c0.Role, "author")
	}
	if c0.Name != "Smith, Alice" {
		t.Errorf("contributor 0 display name: got %q, want %q", c0.Name, "Smith, Alice")
	}

	// ORCID
	var foundORCID bool
	for _, id := range c0.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID && id.Value == "0000-0001-2345-6789" {
			foundORCID = true
		}
	}
	if !foundORCID {
		t.Error("ORCID not found on first contributor")
	}

	c1 := r.Contributors[1]
	if c1.ParsedName == nil || c1.ParsedName.Given != "Bob" || c1.ParsedName.Family != "Jones" {
		t.Errorf("contributor 1 name: got given=%q family=%q", c1.ParsedName.GetGiven(), c1.ParsedName.GetFamily())
	}

	// Publication date
	if len(r.Dates) != 1 {
		t.Fatalf("expected 1 date, got %d", len(r.Dates))
	}
	d := r.Dates[0]
	if d.Type != hubv1.DateType_DATE_TYPE_ISSUED {
		t.Errorf("date type: got %v, want ISSUED", d.Type)
	}
	if d.Year != 2025 || d.Month != 3 || d.Day != 15 {
		t.Errorf("date: got %d-%d-%d, want 2025-3-15", d.Year, d.Month, d.Day)
	}

	// Journal relation
	var foundJournal bool
	for _, rel := range r.Relations {
		if rel.Type == hubv1.RelationType_RELATION_TYPE_PART_OF && rel.TargetTitle == "Journal of Testing" {
			foundJournal = true
		}
	}
	if !foundJournal {
		t.Error("journal relation not found")
	}

	// Volume and issue from journal_issue
	if v := hub.GetExtraString(r, "volume"); v != "42" {
		t.Errorf("volume: got %q, want %q", v, "42")
	}
	if v := hub.GetExtraString(r, "issue"); v != "7" {
		t.Errorf("issue: got %q, want %q", v, "7")
	}

	// Source info
	if r.SourceInfo == nil {
		t.Fatal("SourceInfo is nil")
	}
	if r.SourceInfo.Format != "crossref" {
		t.Errorf("SourceInfo.Format: got %q, want %q", r.SourceInfo.Format, "crossref")
	}
	if r.SourceInfo.FormatVersion != Version {
		t.Errorf("SourceInfo.FormatVersion: got %q, want %q", r.SourceInfo.FormatVersion, Version)
	}
	if r.SourceInfo.SourceId != "10.1234/test.2025.001" {
		t.Errorf("SourceInfo.SourceId: got %q, want %q", r.SourceInfo.SourceId, "10.1234/test.2025.001")
	}
}

func TestParseDissertation(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<doi_batch xmlns="http://www.crossref.org/schema/5.3.1" version="5.3.1">
  <head>
    <doi_batch_id>diss_batch_001</doi_batch_id>
    <timestamp>20250601120000</timestamp>
    <depositor>
      <depositor_name>University Library</depositor_name>
      <email_address>library@university.edu</email_address>
    </depositor>
    <registrant>University</registrant>
  </head>
  <body>
    <dissertation>
      <titles>
        <title>Exploring Novel Algorithms for Distributed Systems</title>
      </titles>
      <person_name contributor_role="author" sequence="first">
        <given_name>Carol</given_name>
        <surname>Williams</surname>
      </person_name>
      <approval_date>
        <year>2025</year>
        <month>5</month>
      </approval_date>
      <institution>
        <institution_name>Lehigh University</institution_name>
        <institution_department>Computer Science</institution_department>
      </institution>
      <degree>PhD</degree>
      <doi_data>
        <doi>10.5678/diss.2025.001</doi>
        <resource>https://example.com/dissertation/001</resource>
      </doi_data>
    </dissertation>
  </body>
</doi_batch>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]

	// Title
	if r.Title != "Exploring Novel Algorithms for Distributed Systems" {
		t.Errorf("Title: got %q", r.Title)
	}

	// Resource type
	if r.ResourceType == nil || r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION {
		t.Errorf("ResourceType: expected DISSERTATION, got %v", r.ResourceType)
	}

	// Contributor (single author)
	if len(r.Contributors) != 1 {
		t.Fatalf("expected 1 contributor, got %d", len(r.Contributors))
	}
	c := r.Contributors[0]
	if c.ParsedName == nil || c.ParsedName.Given != "Carol" || c.ParsedName.Family != "Williams" {
		t.Errorf("author name: got given=%q family=%q", c.ParsedName.GetGiven(), c.ParsedName.GetFamily())
	}
	if c.Role != "author" {
		t.Errorf("author role: got %q, want %q", c.Role, "author")
	}

	// Approval date
	if len(r.Dates) != 1 {
		t.Fatalf("expected 1 date, got %d", len(r.Dates))
	}
	d := r.Dates[0]
	if d.Type != hubv1.DateType_DATE_TYPE_ACCEPTED {
		t.Errorf("date type: got %v, want ACCEPTED", d.Type)
	}
	if d.Year != 2025 || d.Month != 5 {
		t.Errorf("date: got year=%d month=%d, want year=2025 month=5", d.Year, d.Month)
	}

	// Degree info
	if r.DegreeInfo == nil {
		t.Fatal("DegreeInfo is nil")
	}
	if r.DegreeInfo.Institution != "Lehigh University" {
		t.Errorf("institution: got %q, want %q", r.DegreeInfo.Institution, "Lehigh University")
	}
	if r.DegreeInfo.Department != "Computer Science" {
		t.Errorf("department: got %q, want %q", r.DegreeInfo.Department, "Computer Science")
	}
	if r.DegreeInfo.DegreeName != "PhD" {
		t.Errorf("degree: got %q, want %q", r.DegreeInfo.DegreeName, "PhD")
	}

	// DOI
	var foundDOI bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI && id.Value == "10.5678/diss.2025.001" {
			foundDOI = true
		}
	}
	if !foundDOI {
		t.Error("DOI 10.5678/diss.2025.001 not found")
	}

	// Source info
	if r.SourceInfo == nil {
		t.Fatal("SourceInfo is nil")
	}
	if r.SourceInfo.Format != "crossref" {
		t.Errorf("SourceInfo.Format: got %q", r.SourceInfo.Format)
	}
	if r.SourceInfo.SourceId != "10.5678/diss.2025.001" {
		t.Errorf("SourceInfo.SourceId: got %q", r.SourceInfo.SourceId)
	}
}
