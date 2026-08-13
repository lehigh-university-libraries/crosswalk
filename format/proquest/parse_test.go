package proquest

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	workbench "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

func TestParse(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<DISS_submission embargo_code="3">
  <DISS_authorship>
    <DISS_author type="primary">
      <DISS_name>
        <DISS_surname>Qin</DISS_surname>
        <DISS_fname>Tian</DISS_fname>
        <DISS_middle>M</DISS_middle>
      </DISS_name>
      <DISS_contact type="future">
        <DISS_email>tian@example.edu</DISS_email>
      </DISS_contact>
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
    <DISS_binary type="PDF">qin-dissertation.pdf</DISS_binary>
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
	if r.FullTitle != "An Investigation of Polymer Networks" {
		t.Errorf("FullTitle: got %q, want original title", r.FullTitle)
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
				if c.RoleCode != "relators:cre" {
					t.Errorf("Author role code: got %q", c.RoleCode)
				}
				if c.Email != "tian@example.edu" || c.Status != "Graduate Student" {
					t.Errorf("Author contact/status: got email=%q status=%q", c.Email, c.Status)
				}
				if c.Affiliation != "Lehigh University" {
					t.Errorf("Author affiliation: got %q", c.Affiliation)
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
				if c.RoleCode != "relators:ths" || c.Status != "Faculty" {
					t.Errorf("Advisor role/status: got %q %q", c.RoleCode, c.Status)
				}
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
	if len(r.Departments) != 1 || r.Departments[0] != "Department of Chemistry" {
		t.Errorf("Departments: got %q, want institution department", r.Departments)
	}
	if len(r.Genres) != 1 || r.Genres[0].Value != "dissertations" {
		t.Errorf("Genres: got %#v, want dissertations", r.Genres)
	}
	if r.DigitalOrigin != "born digital" {
		t.Errorf("DigitalOrigin: got %q, want born digital", r.DigitalOrigin)
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
	var foundAccepted, foundIssued, foundAvailable bool
	for _, d := range r.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ACCEPTED {
			foundAccepted = true
			if d.Year != 2024 || d.Month != 1 || d.Day != 15 {
				t.Errorf("accepted date fields: %#v", d)
			}
		}
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED {
			foundIssued = true
			if d.Year != 2024 || d.Precision != hubv1.DatePrecision_DATE_PRECISION_YEAR {
				t.Errorf("issued date fields: %#v", d)
			}
		}
		if d.Type == hubv1.DateType_DATE_TYPE_AVAILABLE {
			foundAvailable = true
			if d.Raw != "2026-01-15" {
				t.Errorf("available date: got %q", d.Raw)
			}
		}
	}
	if !foundAccepted {
		t.Error("accepted date not found")
	}
	if !foundIssued {
		t.Error("issued date not found")
	}
	if !foundAvailable {
		t.Error("24-month embargo date not found")
	}

	if len(r.Files) != 1 || r.Files[0].Path != "qin-dissertation.pdf" || r.Files[0].MimeType != "application/pdf" {
		t.Errorf("files: %#v", r.Files)
	}
}

func TestProQuestToWorkbenchFallsBackToEmbargoCodeAndOmitsAcceptedDate(t *testing.T) {
	t.Parallel()
	input := `<DISS_submission embargo_code="3">
  <DISS_description>
    <DISS_title>Embargo compatibility</DISS_title>
    <DISS_dates>
      <DISS_accept_date>01/15/2024</DISS_accept_date>
      <DISS_comp_date>2024</DISS_comp_date>
    </DISS_dates>
  </DISS_description>
  <DISS_repository>
    <DISS_delayed_release>not-a-date some additional text</DISS_delayed_release>
  </DISS_repository>
</DISS_submission>`

	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := records[0].AccessCondition; got != "" {
		t.Fatalf("AccessCondition = %q, want empty; delayed release is embargo timing", got)
	}
	var output bytes.Buffer
	if err := (&workbench.Format{}).Serialize(&output, records, workbenchSerializeOptions()); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("reading Workbench CSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("CSV rows = %d, want 2", len(rows))
	}

	columns := make(map[string]string, len(rows[0]))
	for index, header := range rows[0] {
		columns[header] = rows[1][index]
	}
	if got := columns["field_edtf_date_embargo"]; got != "2026-01-15" {
		t.Errorf("field_edtf_date_embargo = %q, want %q", got, "2026-01-15")
	}
	if got := columns["field_edtf_date_issued"]; got != "2024" {
		t.Errorf("field_edtf_date_issued = %q, want completion date only", got)
	}
	if got := columns["field_access"]; got != "" {
		t.Errorf("field_access = %q, want empty for delayed release", got)
	}
}

func TestProQuestDelayedReleaseMapsToAvailabilityOnly(t *testing.T) {
	t.Parallel()
	input := `<DISS_submission embargo_code="1">
  <DISS_description>
    <DISS_title>Explicit embargo date</DISS_title>
    <DISS_dates><DISS_accept_date>01/15/2024</DISS_accept_date></DISS_dates>
  </DISS_description>
  <DISS_repository>
    <DISS_delayed_release>2027-06-01 release after embargo</DISS_delayed_release>
  </DISS_repository>
</DISS_submission>`

	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	record := records[0]
	if got := record.AccessCondition; got != "" {
		t.Fatalf("AccessCondition = %q, want empty", got)
	}
	var available string
	for _, date := range record.Dates {
		if date.Type == hubv1.DateType_DATE_TYPE_AVAILABLE {
			available = date.Raw
		}
	}
	if available != "2027-06-01" {
		t.Fatalf("available date = %q, want explicit delayed-release date", available)
	}

	var output bytes.Buffer
	if err := (&workbench.Format{}).Serialize(&output, records, workbenchSerializeOptions()); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("reading Workbench CSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("CSV rows = %d, want 2", len(rows))
	}
	columns := make(map[string]string, len(rows[0]))
	for index, header := range rows[0] {
		columns[header] = rows[1][index]
	}
	if got := columns["field_edtf_date_embargo"]; got != "2027-06-01" {
		t.Errorf("field_edtf_date_embargo = %q", got)
	}
	if got := columns["field_access"]; got != "" {
		t.Errorf("field_access = %q, want empty", got)
	}
}

func TestProQuestMastersETDToWorkbenchPreservesSourceSemantics(t *testing.T) {
	t.Parallel()
	fullTitle := strings.Repeat("界", 260)
	input := `<DISS_submission>
  <DISS_description>
    <DISS_title>` + fullTitle + `</DISS_title>
    <DISS_degree>Master of Arts</DISS_degree>
    <DISS_degree_level>Masters</DISS_degree_level>
    <DISS_institution>
      <DISS_inst_name>Example University</DISS_inst_name>
      <DISS_inst_contact>Department of English</DISS_inst_contact>
    </DISS_institution>
    <DISS_categorization>
      <DISS_language>en</DISS_language>
    </DISS_categorization>
  </DISS_description>
</DISS_submission>`

	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	if record.FullTitle != fullTitle {
		t.Errorf("FullTitle changed from source")
	}
	if record.Title != fullTitle {
		t.Error("canonical Title changed from source")
	}
	if record.ResourceType == nil || record.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS {
		t.Errorf("ResourceType = %#v, want thesis", record.ResourceType)
	}
	if len(record.Genres) != 1 || record.Genres[0].Value != "theses" {
		t.Errorf("Genres = %#v, want theses", record.Genres)
	}

	var output bytes.Buffer
	if err := (&workbench.Format{}).Serialize(&output, records, workbenchSerializeOptions()); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("reading Workbench CSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("CSV rows = %d, want 2", len(rows))
	}
	columns := make(map[string]string, len(rows[0]))
	for index, header := range rows[0] {
		columns[header] = rows[1][index]
	}

	wants := map[string]string{
		"title":                 strings.Repeat("界", 255),
		"field_full_title":      fullTitle,
		"field_department_name": "Department of English",
		"field_genre":           "theses",
		"field_model":           "Digital Document",
		"field_language":        "en",
		"field_digital_origin":  "born digital",
	}
	for column, want := range wants {
		if got := columns[column]; got != want {
			t.Errorf("%s = %q, want %q", column, got, want)
		}
	}
}

func workbenchSerializeOptions() *format.SerializeOptions {
	options := format.NewSerializeOptions()
	options.Spec = spec.FabricatorWorkbench()
	return options
}

func TestIsDoctoralDegree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		degree string
		level  string
		want   bool
	}{
		{name: "PhD abbreviation", degree: "Ph.D.", want: true},
		{name: "doctor name", degree: "Doctor of Philosophy", want: true},
		{name: "doctoral level", degree: "Engineering", level: "Doctoral", want: true},
		{name: "other D degree", degree: "Ed.D. in Educational Leadership", want: true},
		{name: "masters", degree: "Master of Arts", level: "Masters", want: false},
		{name: "advanced degree is not EdD", degree: "Advanced Degree", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isDoctoralDegree(tt.degree, tt.level); got != tt.want {
				t.Errorf("isDoctoralDegree(%q, %q) = %t, want %t", tt.degree, tt.level, got, tt.want)
			}
		})
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
