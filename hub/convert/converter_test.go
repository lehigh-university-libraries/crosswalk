package convert

import (
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	bibtexv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/bibtex/v1"
)

func TestNewConverter(t *testing.T) {
	c := NewConverter()

	if c.Parsers() == nil {
		t.Error("Parsers() should not return nil")
	}
	if c.Validators() == nil {
		t.Error("Validators() should not return nil")
	}
	if c.Serializers() == nil {
		t.Error("Serializers() should not return nil")
	}
}

func TestConverter_ToHub_BasicFields(t *testing.T) {
	entry := &bibtexv1.Entry{
		Title:     "Test Article Title",
		Year:      "2023",
		Publisher: "Test Publisher",
		Address:   "New York",
	}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	if result.Record == nil {
		t.Fatal("result.Record is nil")
	}

	if result.Record.Title != "Test Article Title" {
		t.Errorf("Title = %q, want 'Test Article Title'", result.Record.Title)
	}

	if result.Record.Publisher != "Test Publisher" {
		t.Errorf("Publisher = %q, want 'Test Publisher'", result.Record.Publisher)
	}

	if result.Record.PlacePublished != "New York" {
		t.Errorf("PlacePublished = %q, want 'New York'", result.Record.PlacePublished)
	}
}

func TestConverter_ToHub_Identifiers(t *testing.T) {
	entry := &bibtexv1.Entry{
		Title: "Test Article",
		Doi:   "10.1234/test.doi",
		Isbn:  "978-0-306-40615-7",
		Issn:  "0378-5955",
		Url:   "https://example.com/article",
	}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	// Check identifiers were created
	if len(result.Record.Identifiers) == 0 {
		t.Fatal("no identifiers created")
	}

	// Find DOI identifier
	var foundDOI, foundISBN, foundISSN, foundURL bool
	for _, id := range result.Record.Identifiers {
		switch id.Type {
		case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
			foundDOI = true
			if id.Value != "10.1234/test.doi" {
				t.Errorf("DOI value = %q, want '10.1234/test.doi'", id.Value)
			}
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
			foundISBN = true
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
			foundISSN = true
		case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
			foundURL = true
		}
	}

	if !foundDOI {
		t.Error("DOI identifier not found")
	}
	if !foundISBN {
		t.Error("ISBN identifier not found")
	}
	if !foundISSN {
		t.Error("ISSN identifier not found")
	}
	if !foundURL {
		t.Error("URL identifier not found")
	}
}

func TestConverter_ToHub_Dates(t *testing.T) {
	entry := &bibtexv1.Entry{
		Title: "Test Article",
		Year:  "2023",
		Month: "May",
	}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	// Check dates were created
	if len(result.Record.Dates) == 0 {
		t.Fatal("no dates created")
	}

	// Find issued dates - both year and month map to issued
	var foundYear, foundMonth bool
	for _, d := range result.Record.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED {
			if d.Raw == "2023" {
				foundYear = true
			}
			if d.Raw == "May" {
				foundMonth = true
			}
		}
	}

	if !foundYear {
		t.Error("year date not found")
	}
	if !foundMonth {
		t.Error("month date not found")
	}
}

func TestConverter_ToHub_Contributors(t *testing.T) {
	entry := &bibtexv1.Entry{
		Title: "Test Article",
		Author: []*bibtexv1.Person{
			{Name: "Smith, John"},
			{Given: "Jane", Family: "Doe"},
		},
		Editor: []*bibtexv1.Person{
			{Name: "Brown, Charlie"},
		},
	}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	// Check contributors were created
	if len(result.Record.Contributors) != 3 {
		t.Fatalf("expected 3 contributors, got %d", len(result.Record.Contributors))
	}

	// Check author roles
	var authorCount, editorCount int
	for _, contrib := range result.Record.Contributors {
		switch contrib.Role {
		case "author":
			authorCount++
		case "editor":
			editorCount++
		}
	}

	if authorCount != 2 {
		t.Errorf("expected 2 authors, got %d", authorCount)
	}
	if editorCount != 1 {
		t.Errorf("expected 1 editor, got %d", editorCount)
	}
}

func TestConverter_ToHub_Relations(t *testing.T) {
	entry := &bibtexv1.Entry{
		Title:     "Test Chapter",
		Booktitle: "Test Book",
		Journal:   "Test Journal",
		Series:    "Test Series",
	}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	// Check relations were created
	if len(result.Record.Relations) == 0 {
		t.Fatal("no relations created")
	}

	var foundPartOf, foundInSeries bool
	for _, rel := range result.Record.Relations {
		switch rel.Type {
		case hubv1.RelationType_RELATION_TYPE_PART_OF:
			foundPartOf = true
		case hubv1.RelationType_RELATION_TYPE_IN_SERIES:
			foundInSeries = true
		}
	}

	if !foundPartOf {
		t.Error("part_of relation not found")
	}
	if !foundInSeries {
		t.Error("in_series relation not found")
	}
}

func TestConverter_ToHub_Subjects(t *testing.T) {
	entry := &bibtexv1.Entry{
		Title:    "Test Article",
		Keywords: []string{"machine learning", "natural language processing", "AI"},
	}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	// Check subjects were created
	if len(result.Record.Subjects) != 3 {
		t.Fatalf("expected 3 subjects, got %d", len(result.Record.Subjects))
	}

	for _, subj := range result.Record.Subjects {
		if subj.Vocabulary != hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS {
			t.Errorf("subject vocabulary = %v, want KEYWORDS", subj.Vocabulary)
		}
	}
}

func TestConverter_ToHub_ResourceType(t *testing.T) {
	tests := []struct {
		name         string
		entryType    bibtexv1.EntryType
		wantResource hubv1.ResourceTypeValue
	}{
		{
			name:         "article",
			entryType:    bibtexv1.EntryType_ENTRY_TYPE_ARTICLE,
			wantResource: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		},
		{
			name:         "book",
			entryType:    bibtexv1.EntryType_ENTRY_TYPE_BOOK,
			wantResource: hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK,
		},
		{
			name:         "inproceedings",
			entryType:    bibtexv1.EntryType_ENTRY_TYPE_INPROCEEDINGS,
			wantResource: hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER,
		},
		{
			name:         "phdthesis",
			entryType:    bibtexv1.EntryType_ENTRY_TYPE_PHDTHESIS,
			wantResource: hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION,
		},
		{
			name:         "mastersthesis",
			entryType:    bibtexv1.EntryType_ENTRY_TYPE_MASTERSTHESIS,
			wantResource: hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS,
		},
	}

	c := NewConverter()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &bibtexv1.Entry{
				Title:     "Test",
				EntryType: tt.entryType,
			}

			result, err := c.ToHub(entry)
			if err != nil {
				t.Fatalf("ToHub error: %v", err)
			}

			if result.Record.ResourceType == nil {
				t.Fatal("ResourceType is nil")
			}
			if result.Record.ResourceType.Type != tt.wantResource {
				t.Errorf("ResourceType.Type = %v, want %v", result.Record.ResourceType.Type, tt.wantResource)
			}
		})
	}
}

func TestConverter_ToHub_DegreeInfo(t *testing.T) {
	entry := &bibtexv1.Entry{
		Title:       "My Dissertation",
		EntryType:   bibtexv1.EntryType_ENTRY_TYPE_PHDTHESIS,
		School:      "MIT",
		Institution: "Massachusetts Institute of Technology",
		Type:        "PhD Thesis",
	}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	if result.Record.DegreeInfo == nil {
		t.Fatal("DegreeInfo is nil")
	}

	// school should map to institution
	if result.Record.DegreeInfo.Institution == "" {
		t.Error("DegreeInfo.Institution should not be empty")
	}
}

func TestConverter_ToHub_Notes(t *testing.T) {
	entry := &bibtexv1.Entry{
		Title:  "Test Article",
		Annote: "This is an annotation",
		Note:   "This is a note",
	}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	if len(result.Record.Notes) == 0 {
		t.Error("expected notes to be created")
	}
}

func TestConverter_ToHub_Extra(t *testing.T) {
	entry := &bibtexv1.Entry{
		Title:   "Test Article",
		Chapter: "5",
		Edition: "2nd",
		Volume:  "10",
		Number:  "3",
		Pages:   "100-120",
	}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	// These fields should be stored in extra
	if result.Record.Extra == nil {
		t.Fatal("Extra is nil")
	}

	// Check that some extra fields exist
	fields := result.Record.Extra.Fields
	if fields == nil {
		t.Fatal("Extra.Fields is nil")
	}
}

func TestConverter_ToHub_CompleteEntry(t *testing.T) {
	// Test a complete BibTeX entry with all common fields
	entry := &bibtexv1.Entry{
		EntryType:   bibtexv1.EntryType_ENTRY_TYPE_ARTICLE,
		CitationKey: "smith2023test",
		Title:       "A Complete Test Article",
		Year:        "2023",
		Month:       "January",
		Journal:     "Journal of Testing",
		Volume:      "42",
		Number:      "1",
		Pages:       "1-25",
		Publisher:   "Test Publisher",
		Address:     "New York",
		Doi:         "10.1234/test.2023.001",
		Url:         "https://example.com/article",
		Abstract:    "This is the abstract of the test article.",
		Keywords:    []string{"testing", "software", "quality"},
		Author: []*bibtexv1.Person{
			{Given: "John", Family: "Smith", Orcid: "0000-0002-1825-0097"},
			{Name: "Doe, Jane"},
		},
		Language: "en",
	}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	record := result.Record

	// Verify all fields were mapped
	if record.Title != "A Complete Test Article" {
		t.Errorf("Title mismatch")
	}
	if record.Abstract != "This is the abstract of the test article." {
		t.Errorf("Abstract mismatch")
	}
	if record.Publisher != "Test Publisher" {
		t.Errorf("Publisher mismatch")
	}
	if record.PlacePublished != "New York" {
		t.Errorf("PlacePublished mismatch")
	}
	if record.Language != "en" {
		t.Errorf("Language mismatch")
	}
	if record.ResourceType == nil || record.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE {
		t.Errorf("ResourceType mismatch")
	}

	// Check contributors
	if len(record.Contributors) != 2 {
		t.Errorf("expected 2 contributors, got %d", len(record.Contributors))
	}

	// Check identifiers (should have DOI, URL, and citation_key as local)
	if len(record.Identifiers) < 2 {
		t.Errorf("expected at least 2 identifiers, got %d", len(record.Identifiers))
	}

	// Check subjects
	if len(record.Subjects) != 3 {
		t.Errorf("expected 3 subjects, got %d", len(record.Subjects))
	}

	// Check dates
	if len(record.Dates) < 1 {
		t.Errorf("expected at least 1 date, got %d", len(record.Dates))
	}

	// Check relations (journal)
	if len(record.Relations) < 1 {
		t.Errorf("expected at least 1 relation, got %d", len(record.Relations))
	}

	// Check for errors
	if len(result.Errors) > 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestConverter_ToHub_EmptyEntry(t *testing.T) {
	entry := &bibtexv1.Entry{}

	c := NewConverter()
	result, err := c.ToHub(entry)
	if err != nil {
		t.Fatalf("ToHub error: %v", err)
	}

	// Should return a valid but empty record
	if result.Record == nil {
		t.Fatal("result.Record is nil")
	}

	// Check initialized slices
	if result.Record.Contributors == nil {
		t.Error("Contributors should be initialized")
	}
	if result.Record.Dates == nil {
		t.Error("Dates should be initialized")
	}
	if result.Record.Identifiers == nil {
		t.Error("Identifiers should be initialized")
	}
}

func TestConversionError(t *testing.T) {
	err := &ConversionError{
		Field:   "test_field",
		Message: "test message",
		Cause:   nil,
	}

	expected := `conversion error for field "test_field": test message`
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}

	// With cause
	err.Cause = &ValidationError{Message: "inner error"}
	if err.Unwrap() == nil {
		t.Error("Unwrap() should return cause")
	}
}
