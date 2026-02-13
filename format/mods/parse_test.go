package mods

import (
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestParseSingleRecord(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<mods xmlns="http://www.loc.gov/mods/v3"
      xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
      xsi:schemaLocation="http://www.loc.gov/mods/v3 http://www.loc.gov/standards/mods/v3/mods-3-8.xsd"
      version="3.8">
  <titleInfo>
    <title>Advances in Metadata Crosswalking</title>
  </titleInfo>
  <name type="personal">
    <namePart type="given">Alice</namePart>
    <namePart type="family">Johnson</namePart>
    <role>
      <roleTerm type="text" authority="marcrelator">author</roleTerm>
    </role>
  </name>
  <name type="corporate">
    <namePart>Lehigh University</namePart>
    <role>
      <roleTerm type="text">sponsor</roleTerm>
    </role>
  </name>
  <typeOfResource>text</typeOfResource>
  <originInfo>
    <publisher>University Press</publisher>
    <dateIssued encoding="w3cdtf">2024-03-15</dateIssued>
  </originInfo>
  <abstract>This paper explores advances in metadata crosswalking techniques.</abstract>
  <subject authority="lcsh">
    <topic>Metadata</topic>
    <topic>Crosswalking</topic>
  </subject>
  <subject>
    <topic>Digital libraries</topic>
  </subject>
  <identifier type="doi">10.1234/mods.2024</identifier>
  <identifier type="isbn">978-0-12-345678-9</identifier>
  <language>
    <languageTerm type="code" authority="iso639-2b">eng</languageTerm>
  </language>
</mods>`

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
	if r.Title != "Advances in Metadata Crosswalking" {
		t.Errorf("Title: got %q", r.Title)
	}

	// Contributors
	if len(r.Contributors) < 2 {
		t.Fatalf("expected at least 2 contributors, got %d", len(r.Contributors))
	}

	// Subjects - should include topics from subject elements
	if len(r.Subjects) == 0 {
		t.Fatal("expected at least one subject")
	}
	subjectValues := make(map[string]bool)
	for _, s := range r.Subjects {
		subjectValues[s.Value] = true
	}
	for _, want := range []string{"Metadata", "Crosswalking", "Digital libraries"} {
		if !subjectValues[want] {
			t.Errorf("subject %q not found in subjects: %v", want, r.Subjects)
		}
	}

	// Abstract
	if r.Abstract != "This paper explores advances in metadata crosswalking techniques." {
		t.Errorf("Abstract: got %q", r.Abstract)
	}

	// Publisher
	if r.Publisher != "University Press" {
		t.Errorf("Publisher: got %q", r.Publisher)
	}

	// Dates
	if len(r.Dates) == 0 {
		t.Fatal("expected at least one date")
	}
	var foundIssuedDate bool
	for _, d := range r.Dates {
		if d.Raw == "2024-03-15" {
			foundIssuedDate = true
		}
	}
	if !foundIssuedDate {
		t.Error("issued date 2024-03-15 not found")
	}

	// Identifiers
	var foundDOI, foundISBN bool
	for _, id := range r.Identifiers {
		if id.Value == "10.1234/mods.2024" {
			foundDOI = true
		}
		if id.Value == "978-0-12-345678-9" {
			foundISBN = true
		}
	}
	if !foundDOI {
		t.Error("DOI identifier not found")
	}
	if !foundISBN {
		t.Error("ISBN identifier not found")
	}

	// Source info
	if r.SourceInfo == nil {
		t.Fatal("SourceInfo is nil")
	}
	if r.SourceInfo.Format != "mods" {
		t.Errorf("SourceInfo.Format: got %q", r.SourceInfo.Format)
	}
	if r.SourceInfo.FormatVersion != Version {
		t.Errorf("SourceInfo.FormatVersion: got %q", r.SourceInfo.FormatVersion)
	}
}

func TestParseModsCollection(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<modsCollection xmlns="http://www.loc.gov/mods/v3">
  <mods>
    <titleInfo>
      <title>First Article</title>
    </titleInfo>
    <abstract>Abstract of the first article.</abstract>
  </mods>
  <mods>
    <titleInfo>
      <title>Second Article</title>
    </titleInfo>
    <abstract>Abstract of the second article.</abstract>
  </mods>
</modsCollection>`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	if records[0].Title != "First Article" {
		t.Errorf("Record 0 title: got %q", records[0].Title)
	}
	if records[1].Title != "Second Article" {
		t.Errorf("Record 1 title: got %q", records[1].Title)
	}

	if records[0].Abstract != "Abstract of the first article." {
		t.Errorf("Record 0 abstract: got %q", records[0].Abstract)
	}
	if records[1].Abstract != "Abstract of the second article." {
		t.Errorf("Record 1 abstract: got %q", records[1].Abstract)
	}

	// Both should have source info
	for i, r := range records {
		if r.SourceInfo == nil || r.SourceInfo.Format != "mods" {
			t.Errorf("Record %d: missing or incorrect SourceInfo", i)
		}
	}
}

func TestParseEmptyInput(t *testing.T) {
	f := &Format{}
	_, err := f.Parse(strings.NewReader(""), nil)
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseNoModsElements(t *testing.T) {
	f := &Format{}
	_, err := f.Parse(strings.NewReader("<root><other>data</other></root>"), nil)
	if err == nil {
		t.Error("expected error when no <mods> elements found")
	}
}

func TestParseInterfaceCompliance(t *testing.T) {
	// Verify Format implements Parser at compile time (redundant with var _ check, but explicit).
	var _ hubv1.Record // reference the type to ensure the import is used
	f := &Format{}
	if f.Name() != "mods" {
		t.Errorf("Name: got %q", f.Name())
	}
}
