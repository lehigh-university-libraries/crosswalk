package proquest

import (
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestParse(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<DISS_submission embargo_code="0">
  <DISS_authorship>
    <DISS_author type="primary">
      <DISS_name>
        <DISS_surname>Qin</DISS_surname>
        <DISS_fname>Tian</DISS_fname>
        <DISS_middle>M</DISS_middle>
      </DISS_name>
      <DISS_orcid>0000-0002-1825-0097</DISS_orcid>
    </DISS_author>
  </DISS_authorship>
  <DISS_description page_count="256">
    <DISS_title>An Investigation of Polymer Networks</DISS_title>
    <DISS_degree>Ph.D.</DISS_degree>
    <DISS_institution>
      <DISS_inst_name>Lehigh University</DISS_inst_name>
      <DISS_inst_contact>Department of Chemistry</DISS_inst_contact>
    </DISS_institution>
    <DISS_advisor>
      <DISS_name>
        <DISS_surname>Huang</DISS_surname>
        <DISS_fname>Wei-Min</DISS_fname>
      </DISS_name>
    </DISS_advisor>
    <DISS_categorization>
      <DISS_keyword>polymers</DISS_keyword>
      <DISS_keyword>networks</DISS_keyword>
      <DISS_language>en</DISS_language>
    </DISS_categorization>
    <DISS_dates>
      <DISS_accept_date>01/15/2024</DISS_accept_date>
      <DISS_comp_date>2024</DISS_comp_date>
    </DISS_dates>
  </DISS_description>
  <DISS_content>
    <DISS_abstract>
      <DISS_para>This dissertation investigates polymer networks.</DISS_para>
      <DISS_para>Results show improved properties.</DISS_para>
    </DISS_abstract>
  </DISS_content>
</DISS_submission>`

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
	if r.Title != "An Investigation of Polymer Networks" {
		t.Errorf("Title: got %q, want %q", r.Title, "An Investigation of Polymer Networks")
	}

	// Resource type should be DISSERTATION
	if r.ResourceType == nil || r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION {
		t.Errorf("ResourceType: expected DISSERTATION, got %v", r.ResourceType)
	}

	// Source info
	if r.SourceInfo == nil {
		t.Fatal("SourceInfo is nil")
	}
	if r.SourceInfo.Format != "proquest" {
		t.Errorf("SourceInfo.Format: got %q, want %q", r.SourceInfo.Format, "proquest")
	}
	if r.SourceInfo.FormatVersion != Version {
		t.Errorf("SourceInfo.FormatVersion: got %q, want %q", r.SourceInfo.FormatVersion, Version)
	}

	// Contributors: expect at least the author
	if len(r.Contributors) == 0 {
		t.Fatal("expected at least 1 contributor")
	}

	// Find the author
	var foundAuthor bool
	for _, c := range r.Contributors {
		if c.Role == "author" && c.ParsedName != nil {
			if c.ParsedName.Family == "Qin" && c.ParsedName.Given == "Tian" {
				foundAuthor = true
				if c.ParsedName.Middle != "M" {
					t.Errorf("Author middle name: got %q, want %q", c.ParsedName.Middle, "M")
				}
				// Check ORCID
				var foundOrcid bool
				for _, id := range c.Identifiers {
					if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID && id.Value == "0000-0002-1825-0097" {
						foundOrcid = true
					}
				}
				if !foundOrcid {
					t.Error("Author ORCID 0000-0002-1825-0097 not found")
				}
			}
		}
	}
	if !foundAuthor {
		t.Error("Author Qin, Tian not found in contributors")
	}

	// Find the advisor
	var foundAdvisor bool
	for _, c := range r.Contributors {
		if c.Role == "advisor" && c.ParsedName != nil {
			if c.ParsedName.Family == "Huang" && c.ParsedName.Given == "Wei-Min" {
				foundAdvisor = true
			}
		}
	}
	if !foundAdvisor {
		t.Error("Advisor Huang, Wei-Min not found in contributors")
	}

	// Abstract should contain both paragraphs
	if r.Abstract == "" {
		t.Fatal("Abstract is empty")
	}
	if !strings.Contains(r.Abstract, "This dissertation investigates polymer networks.") {
		t.Errorf("Abstract missing first paragraph: %q", r.Abstract)
	}
	if !strings.Contains(r.Abstract, "Results show improved properties.") {
		t.Errorf("Abstract missing second paragraph: %q", r.Abstract)
	}

	// Degree info
	if r.DegreeInfo == nil {
		t.Fatal("DegreeInfo is nil")
	}
	if r.DegreeInfo.DegreeName != "Ph.D." {
		t.Errorf("DegreeInfo.DegreeName: got %q, want %q", r.DegreeInfo.DegreeName, "Ph.D.")
	}
	if r.DegreeInfo.Institution != "Lehigh University" {
		t.Errorf("DegreeInfo.Institution: got %q, want %q", r.DegreeInfo.Institution, "Lehigh University")
	}

	// Page count
	if r.PageCount != 256 {
		t.Errorf("PageCount: got %d, want 256", r.PageCount)
	}

	// Keywords
	var foundKeywords int
	for _, s := range r.Subjects {
		if s.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS {
			foundKeywords++
		}
	}
	if foundKeywords != 2 {
		t.Errorf("expected 2 keywords, got %d", foundKeywords)
	}

	// Language
	if r.Language != "en" {
		t.Errorf("Language: got %q, want %q", r.Language, "en")
	}

	// Dates
	var foundAccepted, foundIssued bool
	for _, d := range r.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ACCEPTED {
			foundAccepted = true
		}
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED {
			foundIssued = true
		}
	}
	if !foundAccepted {
		t.Error("accepted date not found")
	}
	if !foundIssued {
		t.Error("issued date not found")
	}
}

func TestParseEmptyInput(t *testing.T) {
	f := &Format{}
	_, err := f.Parse(strings.NewReader(""), nil)
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseNoSubmission(t *testing.T) {
	f := &Format{}
	_, err := f.Parse(strings.NewReader("<root><other/></root>"), nil)
	if err == nil {
		t.Error("expected error when no DISS_submission elements found")
	}
}

func TestParseMultipleSubmissions(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<root>
<DISS_submission>
  <DISS_description>
    <DISS_title>First Dissertation</DISS_title>
  </DISS_description>
</DISS_submission>
<DISS_submission>
  <DISS_description>
    <DISS_title>Second Dissertation</DISS_title>
  </DISS_description>
</DISS_submission>
</root>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	if records[0].Title != "First Dissertation" {
		t.Errorf("Record 0 title: got %q, want %q", records[0].Title, "First Dissertation")
	}
	if records[1].Title != "Second Dissertation" {
		t.Errorf("Record 1 title: got %q, want %q", records[1].Title, "Second Dissertation")
	}
}
