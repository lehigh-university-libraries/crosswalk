package format_test

import (
	"bytes"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/format/csv"
	"github.com/lehigh-university-libraries/crosswalk/format/drupal"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

// TestCSVRoundTrip tests that data survives a CSV serialize/parse cycle.
func TestCSVRoundTrip(t *testing.T) {
	// Create a test record
	original := &hubv1.Record{
		Title:     "Test Title for Round Trip",
		Abstract:  "This is an abstract.",
		Language:  "English",
		Publisher: "Test Publisher",
		Contributors: []*hubv1.Contributor{
			{Name: "Doe, John", Role: "Author", RoleCode: "aut"},
			{Name: "Smith, Jane", Role: "Editor", RoleCode: "edt"},
		},
		Dates: []*hubv1.DateValue{
			{Type: hubv1.DateType_DATE_TYPE_ISSUED, Year: 2024, Precision: hubv1.DatePrecision_DATE_PRECISION_YEAR, Raw: "2024"},
		},
		Subjects: []*hubv1.Subject{
			{Value: "Computer Science", Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS},
		},
		Identifiers: []*hubv1.Identifier{
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.1234/test"},
		},
		Rights: []*hubv1.Rights{
			{Uri: "http://rightsstatements.org/vocab/InC/1.0/", Statement: "In Copyright"},
		},
		Genres: []*hubv1.Subject{{Value: "Thesis", Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE}},
	}
	hub.SetExtra(original, "nid", "12345")

	// Serialize to CSV
	csvFormat := &csv.Format{}
	var buf bytes.Buffer

	serializeOpts := format.NewSerializeOptions()
	serializeOpts.Columns = []string{
		"title", "abstract", "language", "publisher",
		"contributors", "contributor_roles",
		"date_issued", "subjects", "identifiers",
		"rights", "genre", "nid",
	}

	err := csvFormat.Serialize(&buf, []*hubv1.Record{original}, serializeOpts)
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	csvData := buf.String()
	t.Logf("Serialized CSV:\n%s", csvData)

	// Parse the CSV back
	parseOpts := format.NewParseOptions()
	records, err := csvFormat.Parse(bytes.NewReader(buf.Bytes()), parseOpts)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	parsed := records[0]

	// Compare key fields
	if parsed.Title != original.Title {
		t.Errorf("Title mismatch: got %q, want %q", parsed.Title, original.Title)
	}
	if parsed.Abstract != original.Abstract {
		t.Errorf("Abstract mismatch: got %q, want %q", parsed.Abstract, original.Abstract)
	}
	if parsed.Language != original.Language {
		t.Errorf("Language mismatch: got %q, want %q", parsed.Language, original.Language)
	}
	if parsed.Publisher != original.Publisher {
		t.Errorf("Publisher mismatch: got %q, want %q", parsed.Publisher, original.Publisher)
	}
	if len(parsed.Contributors) != len(original.Contributors) {
		t.Errorf("Contributors count mismatch: got %d, want %d",
			len(parsed.Contributors), len(original.Contributors))
	}
	if len(parsed.Dates) != len(original.Dates) {
		t.Errorf("Dates count mismatch: got %d, want %d",
			len(parsed.Dates), len(original.Dates))
	}
	if len(parsed.Subjects) != len(original.Subjects) {
		t.Errorf("Subjects count mismatch: got %d, want %d",
			len(parsed.Subjects), len(original.Subjects))
	}
}

// TestDrupalParsePreservesData tests that parsing Drupal JSON extracts data correctly.
func TestDrupalParsePreservesData(t *testing.T) {
	drupalJSON := `{
		"nid": [{"value": 12345}],
		"uuid": [{"value": "abc-123-def"}],
		"title": [{"value": "Test Drupal Record"}],
		"field_abstract": [{"value": "An abstract from Drupal."}],
		"field_edtf_date_issued": [{"value": "2024"}],
		"field_rights": [{"value": "http://rightsstatements.org/vocab/InC/1.0/"}],
		"field_language": [{"target_id": 100}],
		"field_linked_agent": [
			{"target_id": 200, "rel_type": "relators:aut"}
		]
	}`

	drupalFormat := &drupal.Format{}
	parseOpts := format.NewParseOptions()

	records, err := drupalFormat.Parse(bytes.NewReader([]byte(drupalJSON)), parseOpts)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	record := records[0]

	// Verify extracted data
	if record.Title != "Test Drupal Record" {
		t.Errorf("Title mismatch: got %q", record.Title)
	}
	if record.Abstract != "An abstract from Drupal." {
		t.Errorf("Abstract mismatch: got %q", record.Abstract)
	}
	if len(record.Dates) != 1 {
		t.Errorf("Expected 1 date, got %d", len(record.Dates))
	} else if record.Dates[0].Year != 2024 {
		t.Errorf("Date year mismatch: got %d", record.Dates[0].Year)
	}
	if len(record.Rights) != 1 {
		t.Errorf("Expected 1 rights, got %d", len(record.Rights))
	}
	if len(record.Contributors) != 1 {
		t.Errorf("Expected 1 contributor, got %d", len(record.Contributors))
	} else if record.Contributors[0].RoleCode != "relators:aut" {
		t.Errorf("Contributor role code mismatch: got %q", record.Contributors[0].RoleCode)
	}

	// Check Extra fields
	if nid := hub.GetExtraString(record, "nid"); nid != "12345" {
		t.Errorf("NID mismatch: got %q", nid)
	}
	if uuid := hub.GetExtraString(record, "uuid"); uuid != "abc-123-def" {
		t.Errorf("UUID mismatch: got %q", uuid)
	}
}

// TestDrupalToCSVConversion tests the full Drupal -> CSV conversion pipeline.
func TestDrupalToCSVConversion(t *testing.T) {
	drupalJSON := `{
		"nid": [{"value": 426120}],
		"uuid": [{"value": "66a3c074-57e6-40a5-931a-6add26bcfeff"}],
		"title": [{"value": "Inflation And The Progressivity Of The Federal Individual Income Tax"}],
		"field_edtf_date_issued": [{"value": "1978"}],
		"field_rights": [{"value": "http://rightsstatements.org/vocab/InC/1.0/"}],
		"field_linked_agent": [
			{"target_id": 153126, "rel_type": "relators:cre"}
		]
	}`

	// Parse Drupal JSON
	drupalFormat := &drupal.Format{}
	parseOpts := format.NewParseOptions()

	records, err := drupalFormat.Parse(bytes.NewReader([]byte(drupalJSON)), parseOpts)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Serialize to CSV
	csvFormat := &csv.Format{}
	var buf bytes.Buffer

	serializeOpts := format.NewSerializeOptions()
	serializeOpts.Columns = []string{"title", "date_issued", "rights", "contributors", "nid", "uuid"}

	err = csvFormat.Serialize(&buf, records, serializeOpts)
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	csvOutput := buf.String()
	t.Logf("CSV Output:\n%s", csvOutput)

	// Verify CSV contains expected data
	if !bytes.Contains(buf.Bytes(), []byte("Inflation And The Progressivity")) {
		t.Error("CSV should contain title")
	}
	if !bytes.Contains(buf.Bytes(), []byte("1978")) {
		t.Error("CSV should contain date")
	}
	if !bytes.Contains(buf.Bytes(), []byte("426120")) {
		t.Error("CSV should contain nid")
	}
}
