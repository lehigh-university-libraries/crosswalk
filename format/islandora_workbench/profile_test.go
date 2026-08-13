package islandora_workbench

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	csvformat "github.com/lehigh-university-libraries/crosswalk/format/csv"
	"github.com/lehigh-university-libraries/crosswalk/model"
	modeldrupal "github.com/lehigh-university-libraries/crosswalk/model/drupal"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

func TestProfileBoundSpecRoundTripsPublicationCompositeFields(t *testing.T) {
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "article",
			Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "field_related_item", SourceType: "related_item", Kind: model.ValueComposite, Cardinality: -1},
				{Path: "field_part_detail", SourceType: "part_detail", Kind: model.ValueComposite, Cardinality: -1},
			},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "publication-workbench", EntityType: "node", Bundle: "article",
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	transformation, err := spec.CompileDrupalProfile(snapshot, compiled, spec.DrupalCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"field_related_item.title", "field_related_item.identifier_type=l-issn", "field_related_item.identifier_type=issn",
		"field_part_detail.type=volume", "field_part_detail.type=issue", "field_part_detail.type=page",
	} {
		if _, exists := transformation.SourceField(name); !exists {
			t.Errorf("profile-bound source field %q is missing", name)
		}
	}

	input := strings.Join([]string{
		"title,field_related_item.title,field_related_item.identifier_type=l-issn,field_related_item.identifier_type=issn,field_part_detail.type=volume,field_part_detail.type=issue,field_part_detail.type=page",
		"Article,Journal of Examples,2049-3622,2049-3630,12,3,44-50",
	}, "\n") + "\n"
	records, err := (&csvformat.Format{}).Parse(strings.NewReader(input), &format.ParseOptions{
		Spec: transformation, ValueProfile: compiled, Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	publication := records[0].GetPublication()
	if publication.GetTitle() != "Journal of Examples" || publication.GetLIssn() != "2049-3622" || publication.GetIssn() != "2049-3630" ||
		publication.GetVolume() != "12" || publication.GetIssue() != "3" || publication.GetPages() != "44-50" {
		t.Fatalf("parsed publication = %#v", publication)
	}

	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, &format.SerializeOptions{
		Spec: transformation, SystemProfile: compiled, IncludeHeader: true, Operation: spec.OperationUpdate,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(output.Bytes())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	related := profileJSONCells(t, profileTestColumn(t, rows[0], rows[1], "field_related_item"))
	if !containsProfileJSON(related, map[string]string{"title": "Journal of Examples"}) ||
		!containsProfileJSON(related, map[string]string{"identifier": "2049-3622", "identifier_type": "l-issn"}) ||
		!containsProfileJSON(related, map[string]string{"identifier": "2049-3630", "identifier_type": "issn"}) {
		t.Fatalf("serialized related items = %#v", related)
	}
	parts := profileJSONCells(t, profileTestColumn(t, rows[0], rows[1], "field_part_detail"))
	if !containsProfileJSON(parts, map[string]string{"number": "12", "type": "volume"}) ||
		!containsProfileJSON(parts, map[string]string{"number": "3", "type": "issue"}) ||
		!containsProfileJSON(parts, map[string]string{"number": "44-50", "type": "page"}) {
		t.Fatalf("serialized part details = %#v", parts)
	}
}

func profileJSONCells(t *testing.T, cell string) []map[string]string {
	t.Helper()
	parts := strings.Split(cell, "|")
	result := make([]map[string]string, 0, len(parts))
	for _, part := range parts {
		value := make(map[string]string)
		if err := json.Unmarshal([]byte(part), &value); err != nil {
			t.Fatalf("decoding Workbench JSON cell %q: %v", part, err)
		}
		result = append(result, value)
	}
	return result
}

func containsProfileJSON(values []map[string]string, want map[string]string) bool {
	for _, value := range values {
		if len(value) != len(want) {
			continue
		}
		matches := true
		for key, expected := range want {
			if value[key] != expected {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func TestProfileBoundSpecRoundTripsEditedCSVMapping(t *testing.T) {
	snapshot, err := modeldrupal.CompileDirectory(filepath.Join("..", "..", "spec", "testdata", "drupal"), modeldrupal.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "edited-workbench", EntityType: "node", Bundle: "islandora_object",
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range definition.Mappings {
		if definition.Mappings[index].Field.Path == "field_custom_tags" {
			definition.Mappings[index].Hub = "Subjects.keywords"
			definition.Mappings[index].Merge = profile.MergeAppend
		}
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	transformation, err := spec.CompileDrupalProfile(snapshot, compiled, spec.DrupalCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	records, err := (&csvformat.Format{}).Parse(bytes.NewBufferString(
		"title,field_full_title,field_identifier.attr0=doi,field_custom_tags\n"+
			"Example,Example,10.1234/example,alpha ; beta\n",
	), &format.ParseOptions{Spec: transformation, ValueProfile: compiled, Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records[0].Subjects) != 2 {
		t.Fatalf("profile-bound CSV subjects = %#v", records)
	}
	if len(records[0].Identifiers) != 1 || records[0].Identifiers[0].GetScheme() != "doi" || records[0].Identifiers[0].GetValue() != "10.1234/example" {
		t.Fatalf("profile-bound CSV identifier = %#v", records[0].Identifiers)
	}
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, &format.SerializeOptions{
		Spec: transformation, SystemProfile: compiled, IncludeHeader: true, Operation: spec.OperationUpdate,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(output.Bytes())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := profileTestColumn(t, rows[0], rows[1], "field_custom_tags"); got != "alpha|beta" {
		t.Fatalf("profile-bound field_custom_tags = %q, want alpha|beta\n%s", got, output.String())
	}
}

func TestProfileBoundSpecUsesExplicitInstitutionIdentifierRule(t *testing.T) {
	snapshot, err := modeldrupal.CompileDirectory(filepath.Join("..", "..", "spec", "testdata", "drupal"), modeldrupal.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "institution-workbench", EntityType: "node", Bundle: "islandora_object",
		InstitutionalIdentifier: &profile.InstitutionalIdentifierOptions{
			Attribute: "accession", Scheme: "example-accession",
			NamespaceURI: "https://repository.example.edu/id/accession/",
			Pattern:      `^[A-Z]{3}-[0-9]{6}$`, IdentityLevel: profile.IdentitySourceRecord,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	transformation, err := spec.CompileDrupalProfile(snapshot, compiled, spec.DrupalCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	field, ok := transformation.SourceField("field_identifier.attr0=accession")
	if !ok || field.Codec != "profile_identifier" || field.ProfileRule != "example-accession" {
		t.Fatalf("institution identifier source field = %#v, found = %v", field, ok)
	}
	records, err := (&csvformat.Format{}).Parse(bytes.NewBufferString(
		"title,field_full_title,field_identifier.attr0=accession\nExample,Example,ABC-123456\n",
	), &format.ParseOptions{Spec: transformation, ValueProfile: compiled, Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	identifier := records[0].Identifiers[0]
	if identifier.GetScheme() != "example-accession" || identifier.GetNamespaceUri() != "https://repository.example.edu/id/accession/" || identifier.GetValue() != "ABC-123456" {
		t.Fatalf("institution identifier = %#v", identifier)
	}
}

func profileTestColumn(t *testing.T, header, row []string, name string) string {
	t.Helper()
	for index, candidate := range header {
		if candidate == name {
			return row[index]
		}
	}
	t.Fatalf("column %q not found in %v", name, header)
	return ""
}
