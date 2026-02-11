package convert

import (
	"testing"

	"google.golang.org/protobuf/proto"

	bibtexv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/bibtex/v1"
)

func TestGetFieldOptions(t *testing.T) {
	entry := &bibtexv1.Entry{}

	tests := []struct {
		name          string
		fieldName     string
		wantTarget    string
		wantParser    string
		wantValidator string
	}{
		{
			name:       "title field",
			fieldName:  "title",
			wantTarget: "title",
			wantParser: "strip_html",
		},
		{
			name:          "doi field",
			fieldName:     "doi",
			wantTarget:    "identifiers",
			wantParser:    "doi",
			wantValidator: "doi",
		},
		{
			name:          "year field",
			fieldName:     "year",
			wantTarget:    "dates",
			wantParser:    "year",
			wantValidator: "year_range",
		},
		{
			name:       "publisher field",
			fieldName:  "publisher",
			wantTarget: "publisher",
		},
		{
			name:       "address field",
			fieldName:  "address",
			wantTarget: "place_published",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := GetFieldOptions(entry, tt.fieldName)
			if opts == nil {
				t.Fatalf("GetFieldOptions() returned nil for field %s", tt.fieldName)
			}

			if opts.Target != tt.wantTarget {
				t.Errorf("Target = %q, want %q", opts.Target, tt.wantTarget)
			}

			if tt.wantParser != "" && opts.Parser != tt.wantParser {
				t.Errorf("Parser = %q, want %q", opts.Parser, tt.wantParser)
			}

			if tt.wantValidator != "" && opts.Validators != tt.wantValidator {
				t.Errorf("Validators = %q, want %q", opts.Validators, tt.wantValidator)
			}
		})
	}
}

func TestGetFieldOptions_NonExistent(t *testing.T) {
	entry := &bibtexv1.Entry{}
	opts := GetFieldOptions(entry, "nonexistent_field")
	if opts != nil {
		t.Errorf("GetFieldOptions() should return nil for non-existent field")
	}
}

func TestGetMessageOptions(t *testing.T) {
	entry := &bibtexv1.Entry{}
	opts := GetMessageOptions(entry)

	if opts == nil {
		t.Fatal("GetMessageOptions() returned nil")
	}

	if opts.Target != "Record" {
		t.Errorf("Target = %q, want 'Record'", opts.Target)
	}

	if !opts.PreserveUnmapped {
		t.Error("PreserveUnmapped should be true")
	}
}

func TestGetAllFieldMappings(t *testing.T) {
	entry := &bibtexv1.Entry{}
	mappings := GetAllFieldMappings(entry)

	if len(mappings) == 0 {
		t.Fatal("GetAllFieldMappings() returned empty slice")
	}

	// Check that we have mappings for expected fields
	fieldNames := make(map[string]bool)
	for _, m := range mappings {
		fieldNames[m.Name] = true
	}

	expectedFields := []string{"title", "doi", "year", "publisher", "author", "editor"}
	for _, field := range expectedFields {
		if !fieldNames[field] {
			t.Errorf("Missing mapping for field %q", field)
		}
	}
}

func TestGetMappedFields(t *testing.T) {
	entry := &bibtexv1.Entry{}
	mapped := GetMappedFields(entry)

	// All BibTeX fields should have mappings
	for _, m := range mapped {
		if m.Options == nil || m.Options.Target == "" {
			t.Errorf("Field %q has empty target", m.Name)
		}
	}
}

func TestGetEnumValueOptions(t *testing.T) {
	// Test enum value mapping
	entry := &bibtexv1.Entry{
		EntryType: bibtexv1.EntryType_ENTRY_TYPE_ARTICLE,
	}

	md := proto.Clone(entry).ProtoReflect().Descriptor()
	fd := md.Fields().ByName("entry_type")
	if fd == nil {
		t.Fatal("entry_type field not found")
	}

	ed := fd.Enum()
	if ed == nil {
		t.Fatal("entry_type is not an enum")
	}

	mappings := GetEnumMappings(ed)
	if len(mappings) == 0 {
		t.Fatal("GetEnumMappings() returned empty slice")
	}

	// Check ARTICLE mapping
	articleMapping := GetEnumMappingByNumber(ed, 1)
	if articleMapping == nil {
		t.Fatal("ENTRY_TYPE_ARTICLE mapping not found")
	}

	if articleMapping.Options == nil {
		t.Fatal("ENTRY_TYPE_ARTICLE has no options")
	}

	if articleMapping.Options.Target != "RESOURCE_TYPE_ARTICLE" {
		t.Errorf("ENTRY_TYPE_ARTICLE target = %q, want 'RESOURCE_TYPE_ARTICLE'", articleMapping.Options.Target)
	}
}

func TestFieldMappingContributorOptions(t *testing.T) {
	entry := &bibtexv1.Entry{}

	// Test author field options
	authorOpts := GetFieldOptions(entry, "author")
	if authorOpts == nil {
		t.Fatal("author field options not found")
	}

	if authorOpts.Target != "contributors" {
		t.Errorf("author Target = %q, want 'contributors'", authorOpts.Target)
	}

	if authorOpts.Role != "author" {
		t.Errorf("author Role = %q, want 'author'", authorOpts.Role)
	}

	if authorOpts.ContributorType != "person" {
		t.Errorf("author ContributorType = %q, want 'person'", authorOpts.ContributorType)
	}

	// Test editor field options
	editorOpts := GetFieldOptions(entry, "editor")
	if editorOpts == nil {
		t.Fatal("editor field options not found")
	}

	if editorOpts.Role != "editor" {
		t.Errorf("editor Role = %q, want 'editor'", editorOpts.Role)
	}
}

func TestFieldMappingDateOptions(t *testing.T) {
	entry := &bibtexv1.Entry{}

	yearOpts := GetFieldOptions(entry, "year")
	if yearOpts == nil {
		t.Fatal("year field options not found")
	}

	if yearOpts.DateType != "issued" {
		t.Errorf("year DateType = %q, want 'issued'", yearOpts.DateType)
	}
}

func TestFieldMappingIdentifierOptions(t *testing.T) {
	entry := &bibtexv1.Entry{}

	doiOpts := GetFieldOptions(entry, "doi")
	if doiOpts == nil {
		t.Fatal("doi field options not found")
	}

	if doiOpts.IdentifierType != "doi" {
		t.Errorf("doi IdentifierType = %q, want 'doi'", doiOpts.IdentifierType)
	}

	isbnOpts := GetFieldOptions(entry, "isbn")
	if isbnOpts == nil {
		t.Fatal("isbn field options not found")
	}

	if isbnOpts.IdentifierType != "isbn" {
		t.Errorf("isbn IdentifierType = %q, want 'isbn'", isbnOpts.IdentifierType)
	}
}

func TestFieldMappingRelationOptions(t *testing.T) {
	entry := &bibtexv1.Entry{}

	journalOpts := GetFieldOptions(entry, "journal")
	if journalOpts == nil {
		t.Fatal("journal field options not found")
	}

	if journalOpts.Target != "relations" {
		t.Errorf("journal Target = %q, want 'relations'", journalOpts.Target)
	}

	if journalOpts.RelationType != "part_of" {
		t.Errorf("journal RelationType = %q, want 'part_of'", journalOpts.RelationType)
	}
}
