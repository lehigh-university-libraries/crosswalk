package dublincore

import (
	"strings"
	"testing"
)

func TestParseSingleRecord(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/">
  <dc:title>Understanding Dublin Core Metadata</dc:title>
  <dc:creator>Smith, John</dc:creator>
  <dc:creator>Doe, Jane</dc:creator>
  <dc:subject>Metadata</dc:subject>
  <dc:subject>Library Science</dc:subject>
  <dc:description>A comprehensive guide to Dublin Core metadata standards.</dc:description>
  <dc:publisher>Test Publisher</dc:publisher>
  <dc:date>2024-01-15</dc:date>
  <dc:type>Text</dc:type>
  <dc:identifier>doi:10.1234/test.2024</dc:identifier>
  <dc:identifier>isbn:978-3-16-148410-0</dc:identifier>
  <dc:language>en</dc:language>
  <dc:rights>CC BY 4.0</dc:rights>
  <dcterms:issued>2024-06-01</dcterms:issued>
</metadata>`

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
	if r.Title != "Understanding Dublin Core Metadata" {
		t.Errorf("Title: got %q", r.Title)
	}

	// Contributors (from dc:creator)
	if len(r.Contributors) < 2 {
		t.Fatalf("Expected at least 2 contributors, got %d", len(r.Contributors))
	}
	// The bibtex_name parser handles "Last, First" format
	if r.Contributors[0].Name != "Smith, John" {
		t.Errorf("Contributor 0 name: got %q", r.Contributors[0].Name)
	}
	if r.Contributors[0].Role != "creator" {
		t.Errorf("Contributor 0 role: got %q, want %q", r.Contributors[0].Role, "creator")
	}
	if r.Contributors[1].Name != "Doe, Jane" {
		t.Errorf("Contributor 1 name: got %q", r.Contributors[1].Name)
	}

	// Subjects
	if len(r.Subjects) < 2 {
		t.Fatalf("Expected at least 2 subjects, got %d", len(r.Subjects))
	}
	subjectValues := make(map[string]bool)
	for _, s := range r.Subjects {
		subjectValues[s.Value] = true
	}
	for _, want := range []string{"Metadata", "Library Science"} {
		if !subjectValues[want] {
			t.Errorf("Subject %q not found", want)
		}
	}

	// Description/Abstract
	if r.Abstract != "A comprehensive guide to Dublin Core metadata standards." {
		t.Errorf("Abstract: got %q", r.Abstract)
	}

	// Language
	if r.Language != "en" {
		t.Errorf("Language: got %q", r.Language)
	}

	// Identifiers
	if len(r.Identifiers) < 2 {
		t.Fatalf("Expected at least 2 identifiers, got %d", len(r.Identifiers))
	}
	idValues := make(map[string]bool)
	for _, id := range r.Identifiers {
		idValues[id.Value] = true
	}
	for _, want := range []string{"doi:10.1234/test.2024", "isbn:978-3-16-148410-0"} {
		if !idValues[want] {
			t.Errorf("Identifier %q not found", want)
		}
	}

	// Dates (dc:date + dcterms:issued)
	if len(r.Dates) < 2 {
		t.Fatalf("Expected at least 2 dates, got %d", len(r.Dates))
	}

	// Source info
	if r.SourceInfo == nil {
		t.Fatal("SourceInfo is nil")
	}
	if r.SourceInfo.Format != "dublincore" {
		t.Errorf("SourceInfo.Format: got %q", r.SourceInfo.Format)
	}
	if r.SourceInfo.FormatVersion != Version {
		t.Errorf("SourceInfo.FormatVersion: got %q", r.SourceInfo.FormatVersion)
	}
}

func TestParseMultipleRecords(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<records>
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>First Record</dc:title>
    <dc:creator>Author One</dc:creator>
  </metadata>
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Second Record</dc:title>
    <dc:creator>Author Two</dc:creator>
  </metadata>
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

	// Both should have source info set
	for i, r := range records {
		if r.SourceInfo == nil || r.SourceInfo.Format != "dublincore" {
			t.Errorf("Record %d SourceInfo.Format: got %v", i, r.SourceInfo)
		}
	}
}

func TestParseOAIPMHWrapped(t *testing.T) {
	// OAI-PMH wraps Dublin Core inside a <metadata> element. Since the DC
	// proto's xml_name is also "metadata", UnmarshalAll finds the OAI-PMH
	// <metadata> element and parses the namespace-prefixed DC child elements
	// directly from it.
	input := `<?xml version="1.0" encoding="UTF-8"?>
<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <responseDate>2024-01-20T00:00:00Z</responseDate>
  <GetRecord>
    <record>
      <header>
        <identifier>oai:example.org:12345</identifier>
        <datestamp>2024-01-20</datestamp>
      </header>
      <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/">
        <dc:title>OAI-PMH Wrapped Dublin Core Record</dc:title>
        <dc:creator>Wright, Alice</dc:creator>
        <dc:subject>Digital Libraries</dc:subject>
        <dc:subject>Open Access</dc:subject>
        <dc:description>A record harvested via OAI-PMH.</dc:description>
        <dc:date>2024-03-01</dc:date>
        <dc:identifier>https://example.org/items/12345</dc:identifier>
        <dc:language>en</dc:language>
        <dcterms:issued>2024-03-15</dcterms:issued>
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

	if r.Title != "OAI-PMH Wrapped Dublin Core Record" {
		t.Errorf("Title: got %q", r.Title)
	}

	// Contributor from dc:creator
	if len(r.Contributors) < 1 {
		t.Fatalf("Expected at least 1 contributor, got %d", len(r.Contributors))
	}
	if r.Contributors[0].Name != "Wright, Alice" {
		t.Errorf("Contributor 0 name: got %q", r.Contributors[0].Name)
	}

	// Subjects
	subjectValues := make(map[string]bool)
	for _, s := range r.Subjects {
		subjectValues[s.Value] = true
	}
	for _, want := range []string{"Digital Libraries", "Open Access"} {
		if !subjectValues[want] {
			t.Errorf("Subject %q not found in OAI-PMH record", want)
		}
	}

	// Description
	if r.Abstract != "A record harvested via OAI-PMH." {
		t.Errorf("Abstract: got %q", r.Abstract)
	}

	// Language
	if r.Language != "en" {
		t.Errorf("Language: got %q", r.Language)
	}

	// At least one identifier
	if len(r.Identifiers) < 1 {
		t.Error("Expected at least 1 identifier")
	}

	// Source info
	if r.SourceInfo == nil || r.SourceInfo.Format != "dublincore" {
		t.Errorf("SourceInfo: got %v", r.SourceInfo)
	}
}

func TestParseNoRecords(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<root>
  <something>No DC records here</something>
</root>`

	f := &Format{}
	_, err := f.Parse(strings.NewReader(input), nil)
	if err == nil {
		t.Error("Expected error when no DC records found")
	}
}
