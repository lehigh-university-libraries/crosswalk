package arxiv

import (
	"bytes"
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

func TestParseBareRecord(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<arXivRecord xmlns="http://arXiv.org/arXivRecord" version="1.0">
  <identifier>2511.11447</identifier>
  <primary>cs.CL</primary>
  <cross>cs.AI</cross>
  <submitter>
    <email>test@example.com</email>
    <identifier>submitter123</identifier>
  </submitter>
  <version>2</version>
  <date>2025-05-19T18:00:00Z</date>
  <source>
    <type>tex</type>
    <size>123456</size>
    <md5>abcdef01234567890abcdef012345678</md5>
  </source>
  <title>A Sample arXiv Paper Title</title>
  <authorship>
    <affiliation affid="1">
      <institution>MIT</institution>
      <address>Cambridge, MA</address>
    </affiliation>
    <affiliation affid="2">
      <institution>Stanford University</institution>
    </affiliation>
    <author affref="1">
      <beforekey>John</beforekey>
      <keyname>Doe</keyname>
    </author>
    <author affref="2">
      <beforekey>Jane</beforekey>
      <keyname>Smith</keyname>
      <afterkey>Jr</afterkey>
    </author>
    <author affref="">
      <keyname>Sumedha</keyname>
    </author>
  </authorship>
  <classification scheme="MSC2000">
    <value>68T50</value>
    <value>68T05</value>
  </classification>
  <alternate>
    <DOI>10.1234/test.2025</DOI>
    <report-no>MIT-TR-2025-01</report-no>
    <journal-ref>Nature 580, 123-128 (2025)</journal-ref>
  </alternate>
  <comments>10 pages, 3 figures</comments>
  <abstract>This is the abstract of the paper.</abstract>
</arXivRecord>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	r := records[0]

	// Title
	if r.Title != "A Sample arXiv Paper Title" {
		t.Errorf("Title: got %q", r.Title)
	}

	// Resource type
	if r.ResourceType == nil || r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT {
		t.Errorf("ResourceType: expected PREPRINT, got %v", r.ResourceType)
	}

	// arXiv identifier
	var foundArxiv bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV && id.Value == "2511.11447" {
			foundArxiv = true
		}
	}
	if !foundArxiv {
		t.Error("arXiv identifier 2511.11447 not found")
	}

	// Subjects: primary + cross (SourceId holds the code, Value holds the label)
	wantSubjects := map[string]string{
		"cs.CL": "Computation and Language",
		"cs.AI": "Artificial Intelligence",
	}
	for _, s := range r.Subjects {
		if s.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV {
			if wantLabel, ok := wantSubjects[s.SourceId]; ok {
				if s.Value != wantLabel {
					t.Errorf("Subject %q label: got %q, want %q", s.SourceId, s.Value, wantLabel)
				}
				delete(wantSubjects, s.SourceId)
			}
		}
	}
	for code := range wantSubjects {
		t.Errorf("arXiv subject %q not found", code)
	}

	// MSC classification
	var foundMSC bool
	for _, s := range r.Subjects {
		if s.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MSC && s.Value == "68T50" {
			foundMSC = true
		}
	}
	if !foundMSC {
		t.Error("MSC classification 68T50 not found")
	}

	// Date
	if len(r.Dates) != 1 {
		t.Fatalf("Expected 1 date, got %d", len(r.Dates))
	}
	if r.Dates[0].Type != hubv1.DateType_DATE_TYPE_SUBMITTED {
		t.Errorf("Date type: got %v", r.Dates[0].Type)
	}
	if r.Dates[0].Year != 2025 || r.Dates[0].Month != 5 || r.Dates[0].Day != 19 {
		t.Errorf("Date: got %d-%d-%d", r.Dates[0].Year, r.Dates[0].Month, r.Dates[0].Day)
	}

	// Contributors
	if len(r.Contributors) != 3 {
		t.Fatalf("Expected 3 contributors, got %d", len(r.Contributors))
	}

	// First author: John Doe, affiliated with MIT
	c0 := r.Contributors[0]
	if c0.ParsedName.Given != "John" || c0.ParsedName.Family != "Doe" {
		t.Errorf("Author 0 name: got %q %q", c0.ParsedName.Given, c0.ParsedName.Family)
	}
	if c0.Affiliation != "MIT" {
		t.Errorf("Author 0 affiliation: got %q", c0.Affiliation)
	}
	if c0.Name != "John Doe" {
		t.Errorf("Author 0 display name: got %q", c0.Name)
	}
	if c0.ParsedName.Normalized != "Doe, John" {
		t.Errorf("Author 0 normalized: got %q", c0.ParsedName.Normalized)
	}

	// Second author: Jane Smith Jr, affiliated with Stanford
	c1 := r.Contributors[1]
	if c1.ParsedName.Suffix != "Jr" {
		t.Errorf("Author 1 suffix: got %q", c1.ParsedName.Suffix)
	}
	if c1.Affiliation != "Stanford University" {
		t.Errorf("Author 1 affiliation: got %q", c1.Affiliation)
	}

	// Third author: Sumedha (single name, no affiliation)
	c2 := r.Contributors[2]
	if c2.ParsedName.Family != "Sumedha" {
		t.Errorf("Author 2 keyname: got %q", c2.ParsedName.Family)
	}
	if c2.Affiliation != "" {
		t.Errorf("Author 2 should have no affiliation, got %q", c2.Affiliation)
	}

	// Abstract
	if r.Abstract != "This is the abstract of the paper." {
		t.Errorf("Abstract: got %q", r.Abstract)
	}

	// DOI
	var foundDOI bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI && id.Value == "10.1234/test.2025" {
			foundDOI = true
		}
	}
	if !foundDOI {
		t.Error("DOI 10.1234/test.2025 not found")
	}

	// Report number
	var foundReport bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER && id.Value == "MIT-TR-2025-01" {
			foundReport = true
		}
	}
	if !foundReport {
		t.Error("Report number MIT-TR-2025-01 not found")
	}

	// Journal reference as relation
	if len(r.Relations) != 1 {
		t.Fatalf("Expected 1 relation, got %d", len(r.Relations))
	}
	if r.Relations[0].Type != hubv1.RelationType_RELATION_TYPE_PART_OF {
		t.Errorf("Relation type: got %v", r.Relations[0].Type)
	}
	if r.Relations[0].TargetTitle != "Nature 580, 123-128 (2025)" {
		t.Errorf("Journal ref: got %q", r.Relations[0].TargetTitle)
	}

	// Notes
	if len(r.Notes) != 1 || r.Notes[0] != "10 pages, 3 figures" {
		t.Errorf("Notes: got %v", r.Notes)
	}

	// Extra fields
	if v := hub.GetExtraString(r, "submitter_email"); v != "test@example.com" {
		t.Errorf("submitter_email: got %q", v)
	}
	if v := hub.GetExtraString(r, "submitter_id"); v != "submitter123" {
		t.Errorf("submitter_id: got %q", v)
	}
	if v := hub.GetExtraString(r, "source_type"); v != "tex" {
		t.Errorf("source_type: got %q", v)
	}
	if v := hub.GetExtraString(r, "source_md5"); v != "abcdef01234567890abcdef012345678" {
		t.Errorf("source_md5: got %q", v)
	}

	// Source info
	if r.SourceInfo == nil || r.SourceInfo.Format != "arxiv" {
		t.Errorf("SourceInfo.Format: got %v", r.SourceInfo)
	}
	if r.SourceInfo.SourceId != "2511.11447" {
		t.Errorf("SourceInfo.SourceId: got %q", r.SourceInfo.SourceId)
	}
}

func TestParseOAIPMHWrapped(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://www.openarchives.org/OAI/2.0/ http://www.openarchives.org/OAI/2.0/OAI-PMH.xsd">
  <responseDate>2025-05-20T00:00:00Z</responseDate>
  <request verb="GetRecord" identifier="oai:arXiv.org:2511.11447" metadataPrefix="arXiv">http://export.arxiv.org/oai2</request>
  <GetRecord>
    <record>
      <header>
        <identifier>oai:arXiv.org:2511.11447</identifier>
        <datestamp>2025-05-20</datestamp>
        <setSpec>cs</setSpec>
      </header>
      <metadata>
        <arXivRecord xmlns="http://arXiv.org/arXivRecord" version="1.0">
          <identifier>2511.11447</identifier>
          <primary>cs.CL</primary>
          <cross>cs.AI</cross>
          <cross>cs.LG</cross>
          <submitter>
            <email>author@university.edu</email>
          </submitter>
          <version>1</version>
          <date>2025-05-19T18:00:00Z</date>
          <source>
            <type>tex</type>
            <size>50000</size>
            <md5>0123456789abcdef0123456789abcdef</md5>
          </source>
          <title>OAI-PMH Wrapped Paper on Language Models</title>
          <authorship>
            <author affref="">
              <beforekey>Alice</beforekey>
              <keyname>Wang</keyname>
            </author>
            <author affref="">
              <beforekey>Bob</beforekey>
              <keyname>Chen</keyname>
            </author>
          </authorship>
          <alternate>
            <DOI>10.5678/example.2025</DOI>
          </alternate>
          <abstract>An abstract about language models.</abstract>
        </arXivRecord>
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

	if r.Title != "OAI-PMH Wrapped Paper on Language Models" {
		t.Errorf("Title: got %q", r.Title)
	}

	// Should have 3 arXiv subjects: cs.CL (primary), cs.AI, cs.LG (cross)
	arxivSubjects := 0
	for _, s := range r.Subjects {
		if s.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV {
			arxivSubjects++
		}
	}
	if arxivSubjects != 3 {
		t.Errorf("Expected 3 arXiv subjects, got %d", arxivSubjects)
	}

	if len(r.Contributors) != 2 {
		t.Fatalf("Expected 2 contributors, got %d", len(r.Contributors))
	}
	if r.Contributors[0].ParsedName.Family != "Wang" {
		t.Errorf("Author 0 family: got %q", r.Contributors[0].ParsedName.Family)
	}

	if r.Abstract != "An abstract about language models." {
		t.Errorf("Abstract: got %q", r.Abstract)
	}

	// DOI from alternate
	var foundDOI bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI && id.Value == "10.5678/example.2025" {
			foundDOI = true
		}
	}
	if !foundDOI {
		t.Error("DOI not found in OAI-PMH wrapped record")
	}
}

func TestParseOAIFormat(t *testing.T) {
	// Real OAI-PMH arXiv format (http://arxiv.org/OAI/arXiv/)
	input := `<?xml version="1.0" encoding="UTF-8"?>
<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://www.openarchives.org/OAI/2.0/ http://www.openarchives.org/OAI/2.0/OAI-PMH.xsd">
    <responseDate>2026-02-13T14:02:47Z</responseDate>
    <request verb="GetRecord" identifier="oai:arXiv.org:2511.11447" metadataPrefix="arXiv">http://oaipmh.arxiv.org/oai</request>
    <GetRecord>
        <record>
            <header>
                <identifier>oai:arXiv.org:2511.11447</identifier>
                <datestamp>2025-11-18</datestamp>
                <setSpec>cs:cs:DL</setSpec>
                <setSpec>cs:cs:IR</setSpec>
            </header>
            <metadata>
                <arXiv xmlns="http://arxiv.org/OAI/arXiv/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://arxiv.org/OAI/arXiv/ https://oaipmh.arxiv.org/OAI/arXiv.xsd">
                    <id>2511.11447</id>
                    <created>2025-11-17</created>
                    <updated>2025-11-18</updated>
                    <authors>
                        <author>
                            <keyname>Daly</keyname>
                            <forenames>Liza</forenames>
                        </author>
                        <author>
                            <keyname>Cargnelutti</keyname>
                            <forenames>Matteo</forenames>
                        </author>
                        <author>
                            <keyname>Brobston</keyname>
                            <forenames>Catherine</forenames>
                        </author>
                    </authors>
                    <title>GRIN Transfer: A production-ready tool</title>
                    <categories>cs.DL cs.IR</categories>
                    <license>http://creativecommons.org/licenses/by/4.0/</license>
                    <abstract>This is about GRIN Transfer.</abstract>
                </arXiv>
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

	if r.Title != "GRIN Transfer: A production-ready tool" {
		t.Errorf("Title: got %q", r.Title)
	}

	// arXiv ID
	var foundID bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV && id.Value == "2511.11447" {
			foundID = true
		}
	}
	if !foundID {
		t.Error("arXiv identifier not found")
	}

	// Categories as subjects (SourceId holds code, Value holds label)
	wantCats := map[string]string{
		"cs.DL": "Digital Libraries",
		"cs.IR": "Information Retrieval",
	}
	for _, s := range r.Subjects {
		if s.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV {
			if wantLabel, ok := wantCats[s.SourceId]; ok {
				if s.Value != wantLabel {
					t.Errorf("Subject %q label: got %q, want %q", s.SourceId, s.Value, wantLabel)
				}
				delete(wantCats, s.SourceId)
			}
		}
	}
	for cat := range wantCats {
		t.Errorf("Category %q not found", cat)
	}

	// Authors with parsed names
	if len(r.Contributors) != 3 {
		t.Fatalf("Expected 3 contributors, got %d", len(r.Contributors))
	}
	if r.Contributors[0].ParsedName.Family != "Daly" || r.Contributors[0].ParsedName.Given != "Liza" {
		t.Errorf("Author 0: got %q %q", r.Contributors[0].ParsedName.Given, r.Contributors[0].ParsedName.Family)
	}
	if r.Contributors[0].Name != "Liza Daly" {
		t.Errorf("Author 0 display name: got %q", r.Contributors[0].Name)
	}

	// Dates: created + updated
	if len(r.Dates) != 2 {
		t.Fatalf("Expected 2 dates, got %d", len(r.Dates))
	}
	if r.Dates[0].Type != hubv1.DateType_DATE_TYPE_SUBMITTED {
		t.Errorf("Date 0 type: got %v", r.Dates[0].Type)
	}
	if r.Dates[0].Year != 2025 || r.Dates[0].Month != 11 || r.Dates[0].Day != 17 {
		t.Errorf("Date 0: got %d-%d-%d", r.Dates[0].Year, r.Dates[0].Month, r.Dates[0].Day)
	}
	if r.Dates[1].Type != hubv1.DateType_DATE_TYPE_UPDATED {
		t.Errorf("Date 1 type: got %v", r.Dates[1].Type)
	}

	// License
	if len(r.Rights) != 1 || r.Rights[0].Uri != "http://creativecommons.org/licenses/by/4.0/" {
		t.Errorf("Rights: got %v", r.Rights)
	}

	// Abstract
	if r.Abstract != "This is about GRIN Transfer." {
		t.Errorf("Abstract: got %q", r.Abstract)
	}

	// Source info
	if r.SourceInfo == nil || r.SourceInfo.SourceId != "2511.11447" {
		t.Errorf("SourceInfo: got %v", r.SourceInfo)
	}
}

func TestParseOAIWithExtras(t *testing.T) {
	// OAI format with DOI, journal-ref, report-no, msc-class, comments
	input := `<arXiv xmlns="http://arxiv.org/OAI/arXiv/">
    <id>math/0301001</id>
    <created>2003-01-01</created>
    <authors>
        <author>
            <keyname>Euler</keyname>
            <forenames>Leonhard</forenames>
            <suffix>III</suffix>
            <affiliation>University of Basel</affiliation>
        </author>
    </authors>
    <title>On Numbers</title>
    <categories>math.NT</categories>
    <comments>25 pages, 4 figures</comments>
    <report-no>REPORT-2003-001</report-no>
    <journal-ref>J. Math 10, 1-25 (2003)</journal-ref>
    <doi>10.1234/math.2003</doi>
    <msc-class>11B39, 11B37</msc-class>
    <abstract>A paper on numbers.</abstract>
</arXiv>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	r := records[0]

	// Author with suffix and affiliation
	if len(r.Contributors) != 1 {
		t.Fatalf("Expected 1 contributor, got %d", len(r.Contributors))
	}
	c := r.Contributors[0]
	if c.ParsedName.Suffix != "III" {
		t.Errorf("Suffix: got %q", c.ParsedName.Suffix)
	}
	if c.Affiliation != "University of Basel" {
		t.Errorf("Affiliation: got %q", c.Affiliation)
	}

	// DOI
	var foundDOI bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI && id.Value == "10.1234/math.2003" {
			foundDOI = true
		}
	}
	if !foundDOI {
		t.Error("DOI not found")
	}

	// Report number
	var foundReport bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER && id.Value == "REPORT-2003-001" {
			foundReport = true
		}
	}
	if !foundReport {
		t.Error("Report number not found")
	}

	// Journal ref
	if len(r.Relations) != 1 || r.Relations[0].TargetTitle != "J. Math 10, 1-25 (2003)" {
		t.Errorf("Journal ref: got %v", r.Relations)
	}

	// MSC classifications
	mscCount := 0
	for _, s := range r.Subjects {
		if s.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MSC {
			mscCount++
		}
	}
	if mscCount != 2 {
		t.Errorf("Expected 2 MSC subjects, got %d", mscCount)
	}

	// Comments as notes
	if len(r.Notes) != 1 || r.Notes[0] != "25 pages, 4 figures" {
		t.Errorf("Notes: got %v", r.Notes)
	}
}

func TestParseAtomAPI(t *testing.T) {
	input := `<?xml version='1.0' encoding='UTF-8'?>
<feed xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/" xmlns:arxiv="http://arxiv.org/schemas/atom" xmlns="http://www.w3.org/2005/Atom">
  <id>https://arxiv.org/api/query</id>
  <title>arXiv Query</title>
  <updated>2026-02-13T15:09:34Z</updated>
  <opensearch:totalResults>1</opensearch:totalResults>
  <entry>
    <id>http://arxiv.org/abs/2511.11447v2</id>
    <title>GRIN Transfer: A production-ready tool for libraries</title>
    <updated>2025-11-17T15:20:14Z</updated>
    <link href="https://arxiv.org/abs/2511.11447v2" rel="alternate" type="text/html"/>
    <link href="https://arxiv.org/pdf/2511.11447v2" rel="related" type="application/pdf" title="pdf"/>
    <summary>This is the summary of the paper.</summary>
    <category term="cs.DL" scheme="http://arxiv.org/schemas/atom"/>
    <category term="cs.IR" scheme="http://arxiv.org/schemas/atom"/>
    <published>2025-11-14T16:16:04Z</published>
    <arxiv:primary_category term="cs.DL"/>
    <author>
      <name>Liza Daly</name>
    </author>
    <author>
      <name>Matteo Cargnelutti</name>
    </author>
    <author>
      <name>Jonathan Zittrain</name>
    </author>
  </entry>
</feed>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	r := records[0]

	if r.Title != "GRIN Transfer: A production-ready tool for libraries" {
		t.Errorf("Title: got %q", r.Title)
	}

	// arXiv ID extracted from URL, version stripped
	var foundID bool
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV && id.Value == "2511.11447" {
			foundID = true
		}
	}
	if !foundID {
		t.Errorf("arXiv ID not found; identifiers: %v", r.Identifiers)
	}

	// Abstract from summary
	if r.Abstract != "This is the summary of the paper." {
		t.Errorf("Abstract: got %q", r.Abstract)
	}

	// Categories (SourceId holds code, Value holds label)
	wantCats := map[string]string{
		"cs.DL": "Digital Libraries",
		"cs.IR": "Information Retrieval",
	}
	for _, s := range r.Subjects {
		if s.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV {
			if wantLabel, ok := wantCats[s.SourceId]; ok {
				if s.Value != wantLabel {
					t.Errorf("Subject %q label: got %q, want %q", s.SourceId, s.Value, wantLabel)
				}
				delete(wantCats, s.SourceId)
			}
		}
	}
	for cat := range wantCats {
		t.Errorf("Category %q not found", cat)
	}

	// Published date
	if len(r.Dates) != 1 {
		t.Fatalf("Expected 1 date, got %d", len(r.Dates))
	}
	if r.Dates[0].Year != 2025 || r.Dates[0].Month != 11 || r.Dates[0].Day != 14 {
		t.Errorf("Date: got %d-%d-%d", r.Dates[0].Year, r.Dates[0].Month, r.Dates[0].Day)
	}

	// Authors — only full names available from Atom
	if len(r.Contributors) != 3 {
		t.Fatalf("Expected 3 contributors, got %d", len(r.Contributors))
	}
	if r.Contributors[0].Name != "Liza Daly" {
		t.Errorf("Author 0 name: got %q", r.Contributors[0].Name)
	}
	if r.Contributors[0].ParsedName.Family != "Daly" || r.Contributors[0].ParsedName.Given != "Liza" {
		t.Errorf("Author 0 parsed name: got %q %q", r.Contributors[0].ParsedName.Given, r.Contributors[0].ParsedName.Family)
	}
	if r.Contributors[2].Name != "Jonathan Zittrain" {
		t.Errorf("Author 2 name: got %q", r.Contributors[2].Name)
	}

	// PDF URL in extra
	if v := hub.GetExtraString(r, "pdf_url"); v != "https://arxiv.org/pdf/2511.11447v2" {
		t.Errorf("pdf_url: got %q", v)
	}

	// Source info
	if r.SourceInfo == nil || r.SourceInfo.SourceId != "2511.11447" {
		t.Errorf("SourceInfo: got %v", r.SourceInfo)
	}
}

func TestParseMultipleRecords(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/">
  <responseDate>2025-05-20T00:00:00Z</responseDate>
  <ListRecords>
    <record>
      <metadata>
        <arXivRecord xmlns="http://arXiv.org/arXivRecord" version="1.0">
          <identifier>2511.00001</identifier>
          <primary>math.AG</primary>
          <version>1</version>
          <date>2025-05-01T00:00:00Z</date>
          <title>First Paper</title>
          <authorship>
            <author affref=""><keyname>Author1</keyname></author>
          </authorship>
          <abstract>Abstract one.</abstract>
        </arXivRecord>
      </metadata>
    </record>
    <record>
      <metadata>
        <arXivRecord xmlns="http://arXiv.org/arXivRecord" version="1.0">
          <identifier>2511.00002</identifier>
          <primary>physics.hep-th</primary>
          <version>1</version>
          <date>2025-05-02T00:00:00Z</date>
          <title>Second Paper</title>
          <authorship>
            <author affref=""><keyname>Author2</keyname></author>
          </authorship>
          <abstract>Abstract two.</abstract>
        </arXivRecord>
      </metadata>
    </record>
  </ListRecords>
</OAI-PMH>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("Expected 2 records, got %d", len(records))
	}

	if records[0].Title != "First Paper" {
		t.Errorf("Record 0 title: got %q", records[0].Title)
	}
	if records[1].Title != "Second Paper" {
		t.Errorf("Record 1 title: got %q", records[1].Title)
	}
}

func TestParseMultipleAffiliations(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<arXivRecord xmlns="http://arXiv.org/arXivRecord" version="1.0">
  <identifier>math.AG/0101001</identifier>
  <primary>math.AG</primary>
  <version>1</version>
  <date>2001-01-01T00:00:00Z</date>
  <title>Multi-Affiliation Paper</title>
  <authorship>
    <affiliation affid="1">
      <institution>MIT</institution>
    </affiliation>
    <affiliation affid="2">
      <institution>Harvard</institution>
    </affiliation>
    <author affref="1 2">
      <beforekey>John</beforekey>
      <keyname>von Neumann</keyname>
    </author>
  </authorship>
  <abstract>Test.</abstract>
</arXivRecord>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	c := records[0].Contributors[0]

	// Primary affiliation string should be MIT (first in affref)
	if c.Affiliation != "MIT" {
		t.Errorf("Primary affiliation: got %q", c.Affiliation)
	}

	// Should have both affiliations in the structured field
	if len(c.Affiliations) != 2 {
		t.Fatalf("Expected 2 affiliations, got %d", len(c.Affiliations))
	}
	if c.Affiliations[0].Name != "MIT" {
		t.Errorf("Affiliation 0: got %q", c.Affiliations[0].Name)
	}
	if c.Affiliations[1].Name != "Harvard" {
		t.Errorf("Affiliation 1: got %q", c.Affiliations[1].Name)
	}

	// Multi-word keyname
	if c.ParsedName.Family != "von Neumann" {
		t.Errorf("Family name: got %q", c.ParsedName.Family)
	}
}

func TestParseAuthorRole(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<arXivRecord xmlns="http://arXiv.org/arXivRecord" version="1.0">
  <identifier>math/0301001</identifier>
  <primary>math.CO</primary>
  <version>1</version>
  <date>2003-01-01T00:00:00Z</date>
  <title>Paper with Appendix Author</title>
  <authorship>
    <author affref="">
      <beforekey>Alice</beforekey>
      <keyname>Main</keyname>
    </author>
    <author affref="">
      <beforekey>Bob</beforekey>
      <keyname>Appendix</keyname>
      <role>appendix</role>
    </author>
  </authorship>
  <abstract>Test.</abstract>
</arXivRecord>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	c0 := records[0].Contributors[0]
	if c0.Role != "author" {
		t.Errorf("Author 0 role: got %q, want %q", c0.Role, "author")
	}

	c1 := records[0].Contributors[1]
	if c1.Role != "appendix" {
		t.Errorf("Author 1 role: got %q, want %q", c1.Role, "appendix")
	}
}

func TestParseEmptyInput(t *testing.T) {
	f := &Format{}
	_, err := f.Parse(strings.NewReader(""), nil)
	if err == nil {
		t.Error("Expected error for empty input")
	}
}

func TestParseInvalidXML(t *testing.T) {
	f := &Format{}
	_, err := f.Parse(strings.NewReader("<not>valid xml"), nil)
	// Should return error (no arXivRecord found) or XML error
	if err == nil {
		t.Error("Expected error for invalid XML")
	}
}

func TestRoundTrip(t *testing.T) {
	// Create a hub record, serialize to arXiv XML, parse back
	original := &hubv1.Record{
		Title:    "Round Trip Test Paper",
		Abstract: "Testing round trip conversion.",
		ResourceType: &hubv1.ResourceType{
			Type:     hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
			Original: "arXiv",
		},
		Identifiers: []*hubv1.Identifier{
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV, Value: "2511.99999"},
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.9999/roundtrip"},
		},
		Subjects: []*hubv1.Subject{
			{Value: "cs.SE", Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV},
			{Value: "cs.PL", Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_ARXIV},
		},
		Dates: []*hubv1.DateValue{
			{
				Type:      hubv1.DateType_DATE_TYPE_SUBMITTED,
				Year:      2025,
				Month:     6,
				Day:       15,
				Precision: hubv1.DatePrecision_DATE_PRECISION_DAY,
				Raw:       "2025-06-15T00:00:00Z",
			},
		},
		Contributors: []*hubv1.Contributor{
			{
				Name:        "Alice Test",
				Role:        "author",
				Type:        hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
				Affiliation: "Test University",
				ParsedName: &hubv1.ParsedName{
					Given:  "Alice",
					Family: "Test",
				},
			},
		},
		Notes: []string{"5 pages"},
	}

	// Serialize
	f := &Format{}
	var buf bytes.Buffer
	if err := f.Serialize(&buf, []*hubv1.Record{original}, nil); err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	// Parse back
	records, err := f.Parse(&buf, nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	parsed := records[0]

	if parsed.Title != original.Title {
		t.Errorf("Title mismatch: got %q, want %q", parsed.Title, original.Title)
	}
	if parsed.Abstract != original.Abstract {
		t.Errorf("Abstract mismatch: got %q, want %q", parsed.Abstract, original.Abstract)
	}

	// Check arXiv identifier survived
	var foundArxiv bool
	for _, id := range parsed.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV && id.Value == "2511.99999" {
			foundArxiv = true
		}
	}
	if !foundArxiv {
		t.Error("arXiv identifier lost in round trip")
	}

	// Check DOI survived (via alternate)
	var foundDOI bool
	for _, id := range parsed.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI && id.Value == "10.9999/roundtrip" {
			foundDOI = true
		}
	}
	if !foundDOI {
		t.Error("DOI lost in round trip")
	}

	// Check subjects survived
	if len(parsed.Subjects) < 2 {
		t.Errorf("Expected at least 2 subjects, got %d", len(parsed.Subjects))
	}

	// Check contributor survived
	if len(parsed.Contributors) < 1 {
		t.Fatalf("Expected at least 1 contributor, got %d", len(parsed.Contributors))
	}
	if parsed.Contributors[0].ParsedName.Family != "Test" {
		t.Errorf("Contributor family name: got %q", parsed.Contributors[0].ParsedName.Family)
	}
}

func TestExtractArxivID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"http://arxiv.org/abs/2511.11447v2", "2511.11447"},
		{"http://arxiv.org/abs/2511.11447v1", "2511.11447"},
		{"http://arxiv.org/abs/2511.11447", "2511.11447"},
		{"http://arxiv.org/abs/hep-th/9901001v3", "hep-th/9901001"},
		{"2511.11447", "2511.11447"},
		{"2511.11447v2", "2511.11447"},
	}
	for _, tt := range tests {
		got := extractArxivID(tt.input)
		if got != tt.want {
			t.Errorf("extractArxivID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
