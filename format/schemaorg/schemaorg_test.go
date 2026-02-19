package schemaorg

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestSerializeScholarlyArticle(t *testing.T) {
	record := &hubv1.Record{
		Title:    "Test Article Title",
		Abstract: "This is an abstract for testing.",
		Contributors: []*hubv1.Contributor{
			{
				Name: "Jane Doe",
				Role: "author",
				Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
				ParsedName: &hubv1.ParsedName{
					Given:  "Jane",
					Family: "Doe",
				},
				Identifiers: []*hubv1.Identifier{
					{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID, Value: "0000-0001-2345-6789"},
				},
			},
		},
		Dates: []*hubv1.DateValue{
			{Type: hubv1.DateType_DATE_TYPE_PUBLISHED, Year: 2024, Month: 3, Day: 15},
		},
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		},
		Language:  "en",
		Publisher: "Test Publisher",
		Identifiers: []*hubv1.Identifier{
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.1234/test"},
		},
		Subjects: []*hubv1.Subject{
			{Value: "Computer Science", Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS},
			{Value: "Testing", Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS},
		},
		Rights: []*hubv1.Rights{
			{Uri: "https://creativecommons.org/licenses/by/4.0/"},
		},
	}

	f := &Format{}
	var buf bytes.Buffer
	opts := &format.SerializeOptions{Pretty: true}

	err := f.Serialize(&buf, []*hubv1.Record{record}, opts)
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	output := buf.String()
	t.Logf("Serialized JSON-LD:\n%s", output)

	// Verify JSON structure
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	// Check required fields
	if doc["@context"] != "https://schema.org" {
		t.Errorf("Expected @context 'https://schema.org', got %v", doc["@context"])
	}
	if doc["@type"] != "ScholarlyArticle" {
		t.Errorf("Expected @type 'ScholarlyArticle', got %v", doc["@type"])
	}
	if doc["name"] != "Test Article Title" {
		t.Errorf("Expected name 'Test Article Title', got %v", doc["name"])
	}
	if doc["abstract"] != "This is an abstract for testing." {
		t.Errorf("Expected abstract, got %v", doc["abstract"])
	}
	if doc["datePublished"] != "2024-03-15" {
		t.Errorf("Expected datePublished '2024-03-15', got %v", doc["datePublished"])
	}
	if doc["inLanguage"] != "en" {
		t.Errorf("Expected inLanguage 'en', got %v", doc["inLanguage"])
	}

	// Check author
	authors, ok := doc["author"].([]any)
	if !ok || len(authors) == 0 {
		t.Errorf("Expected author array, got %v", doc["author"])
	} else {
		author := authors[0].(map[string]any)
		if author["@type"] != "Person" {
			t.Errorf("Expected author @type 'Person', got %v", author["@type"])
		}
		if author["name"] != "Jane Doe" {
			t.Errorf("Expected author name 'Jane Doe', got %v", author["name"])
		}
		if author["givenName"] != "Jane" {
			t.Errorf("Expected givenName 'Jane', got %v", author["givenName"])
		}
	}

	// Check keywords
	keywords, ok := doc["keywords"].([]any)
	if !ok || len(keywords) != 2 {
		t.Errorf("Expected 2 keywords, got %v", doc["keywords"])
	}
}

func TestSerializeDataset(t *testing.T) {
	record := &hubv1.Record{
		Title:       "Climate Data 2023",
		Description: "Annual climate measurements.",
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET,
		},
		Publisher: "Climate Research Institute",
		Dates: []*hubv1.DateValue{
			{Type: hubv1.DateType_DATE_TYPE_PUBLISHED, Year: 2023},
		},
	}

	f := &Format{}
	var buf bytes.Buffer
	opts := &format.SerializeOptions{Pretty: true}

	err := f.Serialize(&buf, []*hubv1.Record{record}, opts)
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	if doc["@type"] != "Dataset" {
		t.Errorf("Expected @type 'Dataset', got %v", doc["@type"])
	}
}

func TestSerializeDatasetAbstractMapsToDescription(t *testing.T) {
	record := &hubv1.Record{
		Title:    "Dataset With Abstract",
		Abstract: "Dataset summary text.",
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET,
		},
	}

	f := &Format{}
	var buf bytes.Buffer
	opts := &format.SerializeOptions{Pretty: true}

	err := f.Serialize(&buf, []*hubv1.Record{record}, opts)
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	if doc["@type"] != "Dataset" {
		t.Fatalf("Expected @type Dataset, got %v", doc["@type"])
	}
	if doc["description"] != "Dataset summary text." {
		t.Errorf("Expected description from abstract, got %v", doc["description"])
	}
	if _, ok := doc["abstract"]; ok {
		t.Errorf("Did not expect abstract field for dataset when mapped to description")
	}
}

func TestSerializeThesis(t *testing.T) {
	record := &hubv1.Record{
		Title: "Test Thesis",
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS,
		},
	}

	f := &Format{}
	var buf bytes.Buffer
	opts := &format.SerializeOptions{Pretty: true}

	err := f.Serialize(&buf, []*hubv1.Record{record}, opts)
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	rawType, ok := doc["@type"].([]any)
	if !ok {
		t.Fatalf("Expected @type array, got %T (%v)", doc["@type"], doc["@type"])
	}
	if len(rawType) != 2 {
		t.Fatalf("Expected @type length 2, got %d (%v)", len(rawType), rawType)
	}
	if rawType[0] != "ScholarlyArticle" || rawType[1] != "Thesis" {
		t.Errorf("Expected @type [ScholarlyArticle Thesis], got %v", rawType)
	}
}

func TestParseScholarlyArticle(t *testing.T) {
	input := `{
		"@context": "https://schema.org",
		"@type": "ScholarlyArticle",
		"name": "Parsed Article",
		"headline": "Parsed Article",
		"abstract": "An abstract.",
		"author": [
			{
				"@type": "Person",
				"name": "John Smith",
				"givenName": "John",
				"familyName": "Smith",
				"sameAs": "https://orcid.org/0000-0001-2345-6789"
			}
		],
		"datePublished": "2024-01-15",
		"inLanguage": "en",
		"keywords": ["AI", "Machine Learning"],
		"license": "https://creativecommons.org/licenses/by/4.0/",
		"identifier": [
			{
				"@type": "PropertyValue",
				"propertyID": "doi",
				"value": "10.1234/parsed"
			}
		]
	}`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	record := records[0]

	if record.Title != "Parsed Article" {
		t.Errorf("Expected title 'Parsed Article', got %q", record.Title)
	}
	if record.Abstract != "An abstract." {
		t.Errorf("Expected abstract, got %q", record.Abstract)
	}
	if len(record.Contributors) != 1 {
		t.Errorf("Expected 1 contributor, got %d", len(record.Contributors))
	} else {
		c := record.Contributors[0]
		if c.Name != "John Smith" {
			t.Errorf("Expected contributor name 'John Smith', got %q", c.Name)
		}
		if c.ParsedName == nil || c.ParsedName.Given != "John" {
			t.Errorf("Expected parsed givenName 'John'")
		}
		if len(c.Identifiers) == 0 || c.Identifiers[0].Type != hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID {
			t.Errorf("Expected ORCID identifier")
		}
	}
	if len(record.Dates) == 0 || record.Dates[0].Year != 2024 {
		t.Errorf("Expected date year 2024, got %v", record.Dates)
	}
	if record.Language != "en" {
		t.Errorf("Expected language 'en', got %q", record.Language)
	}
	if len(record.Subjects) != 2 {
		t.Errorf("Expected 2 subjects, got %d", len(record.Subjects))
	}
	if len(record.Rights) == 0 || record.Rights[0].Uri != "https://creativecommons.org/licenses/by/4.0/" {
		t.Errorf("Expected CC license, got %v", record.Rights)
	}
}

func TestRoundTrip(t *testing.T) {
	original := &hubv1.Record{
		Title:       "Round Trip Test",
		Abstract:    "Testing round-trip conversion.",
		Description: "Full description here.",
		Contributors: []*hubv1.Contributor{
			{Name: "Test Author", Role: "author", Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON},
		},
		Dates: []*hubv1.DateValue{
			{Type: hubv1.DateType_DATE_TYPE_PUBLISHED, Year: 2024, Month: 6},
		},
		ResourceType: &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK},
		Language:     "en",
		Publisher:    "Test Press",
		Subjects: []*hubv1.Subject{
			{Value: "Testing"},
		},
	}

	f := &Format{}

	// Serialize
	var buf bytes.Buffer
	err := f.Serialize(&buf, []*hubv1.Record{original}, nil)
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	t.Logf("Serialized:\n%s", buf.String())

	// Parse
	records, err := f.Parse(&buf, nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	parsed := records[0]

	// Verify key fields survived round-trip
	if parsed.Title != original.Title {
		t.Errorf("Title mismatch: %q vs %q", parsed.Title, original.Title)
	}
	if parsed.Abstract != original.Abstract {
		t.Errorf("Abstract mismatch: %q vs %q", parsed.Abstract, original.Abstract)
	}
	if parsed.Language != original.Language {
		t.Errorf("Language mismatch: %q vs %q", parsed.Language, original.Language)
	}
	if parsed.Publisher != original.Publisher {
		t.Errorf("Publisher mismatch: %q vs %q", parsed.Publisher, original.Publisher)
	}
	if len(parsed.Contributors) != len(original.Contributors) {
		t.Errorf("Contributor count mismatch: %d vs %d", len(parsed.Contributors), len(original.Contributors))
	}
}

func TestCanParse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "schema.org with @context",
			input:    `{"@context": "https://schema.org", "@type": "ScholarlyArticle"}`,
			expected: true,
		},
		{
			name:     "schema.org without context but with type",
			input:    `{"@type": "Dataset", "name": "Test"}`,
			expected: true,
		},
		{
			name:     "drupal json",
			input:    `{"nid": [{"value": 123}], "uuid": [{"value": "abc"}], "field_title": []}`,
			expected: false,
		},
		{
			name:     "plain json",
			input:    `{"name": "test", "value": 123}`,
			expected: false,
		},
	}

	f := &Format{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.CanParse([]byte(tt.input))
			if result != tt.expected {
				t.Errorf("CanParse(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
