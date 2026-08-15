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
		"id,title,field_related_item.title,field_related_item.identifier_type=l-issn,field_related_item.identifier_type=issn,field_part_detail.type=volume,field_part_detail.type=issue,field_part_detail.type=page",
		"1,Article,Journal of Examples,2049-3622,2049-3630,12,3,44-50",
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
		"id,title,field_full_title,field_identifier.attr0=doi,field_custom_tags\n"+
			"1,Example,Example,https://doi.org/10.1234/example,alpha ; beta\n",
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
	assertProfileFieldPattern(t, field, `^[A-Z]{3}-[0-9]{6}$`)
	records, err := (&csvformat.Format{}).Parse(bytes.NewBufferString(
		"id,title,field_full_title,field_identifier.attr0=accession\n1,Example,Example,ABC-123456 ; XYZ-654321\n",
	), &format.ParseOptions{Spec: transformation, ValueProfile: compiled, Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(records[0].Identifiers) != 2 {
		t.Fatalf("institution identifiers = %#v", records[0].Identifiers)
	}
	for index, want := range []string{"ABC-123456", "XYZ-654321"} {
		identifier := records[0].Identifiers[index]
		if identifier.GetScheme() != "example-accession" || identifier.GetNamespaceUri() != "https://repository.example.edu/id/accession/" || identifier.GetValue() != want {
			t.Fatalf("institution identifier %d = %#v", index+1, identifier)
		}
	}
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, &format.SerializeOptions{
		Spec: transformation, SystemProfile: compiled, IncludeHeader: true, Operation: spec.OperationCreate,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(output.Bytes())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	cell := profileTestColumn(t, rows[0], rows[1], "field_identifier")
	if !strings.Contains(cell, "ABC-123456") || !strings.Contains(cell, "XYZ-654321") || !strings.Contains(cell, "|") {
		t.Fatalf("multi-value identifier Workbench cell = %q", cell)
	}
}

func TestProfileBoundSpecRoundTripsTypedRelationWorkbenchCell(t *testing.T) {
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "article",
			Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{
					Path: "field_association", SourceType: "typed_relation", Kind: model.ValueTypedReference, Cardinality: -1,
					Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: []string{"person"}},
					InstanceSettings: map[string]any{"rel_types": []any{"relators:cre"}, "handler_settings": map[string]any{
						"target_bundles": map[string]any{"person": "person"},
					}},
				},
			},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "typed-relation-workbench", EntityType: "node", Bundle: "article",
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
	input := "id,title,field_association\n1,Example,relators:cre:person:Example Avery\n"
	records, err := (&csvformat.Format{}).Parse(strings.NewReader(input), &format.ParseOptions{
		Spec: transformation, ValueProfile: compiled, Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, &format.SerializeOptions{
		Spec: transformation, SystemProfile: compiled, IncludeHeader: true, Operation: spec.OperationCreate,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(output.Bytes())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := profileTestColumn(t, rows[0], rows[1], "field_association"); got != "relators:cre:person:Example Avery" {
		t.Fatalf("typed relation Workbench cell = %q\n%s", got, output.String())
	}
}

func TestProfileBoundSpecPreservesWorkbenchLinkLabel(t *testing.T) {
	snapshot, err := modeldrupal.CompileDirectory(filepath.Join("..", "..", "spec", "testdata", "drupal-validation"), modeldrupal.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "link-workbench", EntityType: "node", Bundle: "islandora_object",
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
	input := "id,title,field_external_link\n1,Example,https://example.edu/item%%Catalog record\n"
	records, err := (&csvformat.Format{}).Parse(strings.NewReader(input), &format.ParseOptions{
		Spec: transformation, ValueProfile: compiled, Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, &format.SerializeOptions{
		Spec: transformation, SystemProfile: compiled, IncludeHeader: true, Operation: spec.OperationCreate,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(output.Bytes())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := profileTestColumn(t, rows[0], rows[1], "field_external_link"); got != "https://example.edu/item%%Catalog record" {
		t.Fatalf("Workbench link cell = %q\n%s", got, output.String())
	}
}

func TestProfileBoundSpecRoundTripsStructuredWorkbenchCellsAndBaseFields(t *testing.T) {
	snapshot := structuredWorkbenchProfileSnapshot(t, []string{"person"})
	compiled, transformation := compileWorkbenchTestProfile(t, snapshot, "structured-workbench")
	input := strings.Join([]string{
		"id,title,created,uid,field_coordinates,field_vocabulary_link,field_track,field_linked_data",
		`1,Example,2020-11-15T23:49:22+00:00,7,"49.16667,-123.93333 ; 50.1,-120.5",viaf%%http://viaf.org/viaf/10646807%%VIAF Record,Transcript:subtitles:en:/mnt/islandora_staging/example.vtt,https://id.example/record%%Example record`,
	}, "\n") + "\n"
	records, err := (&csvformat.Format{}).Parse(strings.NewReader(input), &format.ParseOptions{
		Spec: transformation, ValueProfile: compiled, Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, &format.SerializeOptions{
		Spec: transformation, SystemProfile: compiled, IncludeHeader: true, Operation: spec.OperationCreate,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(output.Bytes())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"created":               "2020-11-15T23:49:22+00:00",
		"uid":                   "7",
		"field_coordinates":     "49.16667,-123.93333|50.1,-120.5",
		"field_vocabulary_link": "viaf%%http://viaf.org/viaf/10646807%%VIAF Record",
		"field_track":           "Transcript:subtitles:en:/mnt/islandora_staging/example.vtt",
		"field_linked_data":     "https://id.example/record%%Example record",
	}
	for column, expected := range want {
		if got := profileTestColumn(t, rows[0], rows[1], column); got != expected {
			t.Errorf("%s Workbench cell = %q, want %q\n%s", column, got, expected, output.String())
		}
	}
	if _, ok := transformation.SourceField("changed"); ok {
		t.Fatal("profile-bound Workbench source retained changed")
	}
	if uid, ok := transformation.TargetField("uid"); !ok || uid.AppliesTo(spec.OperationUpdate) {
		t.Fatalf("profile-bound uid target = %#v, found = %v", uid, ok)
	}
}

func TestProfileWorkbenchSerializesStructuredDrupalObjects(t *testing.T) {
	tests := []struct {
		name       string
		sourceType string
		value      map[string]any
		want       string
	}{
		{name: "geolocation", sourceType: "geolocation", value: map[string]any{"lat": "49.16667", "lng": "-123.93333"}, want: "49.16667,-123.93333"},
		{name: "authority with title", sourceType: "authority_link", value: map[string]any{"source": "viaf", "uri": "https://viaf.org/viaf/10646807", "title": "VIAF Record"}, want: "viaf%%https://viaf.org/viaf/10646807%%VIAF Record"},
		{name: "authority without title", sourceType: "authority_link", value: map[string]any{"source": "viaf", "uri": "https://viaf.org/viaf/10646807", "title": ""}, want: "viaf%%https://viaf.org/viaf/10646807"},
		{name: "media track path", sourceType: "media_track", value: map[string]any{"label": "Transcript", "kind": "subtitles", "srclang": "en", "file_path": "/mnt/staging/transcript.vtt"}, want: "Transcript:subtitles:en:/mnt/staging/transcript.vtt"},
		{name: "media track URL export", sourceType: "media_track", value: map[string]any{"label": "Transcript", "kind": "captions", "srclang": "en", "url": "/sites/default/files/transcript.vtt"}, want: "Transcript:captions:en:transcript.vtt"},
		{name: "linked data", sourceType: "linked_data_field", value: map[string]any{"url": "https://id.example/1", "value": "Example"}, want: "https://id.example/1%%Example"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := profileWorkbenchValue(spec.Field{}, profile.ResolvedField{SourceType: test.sourceType}, test.value, "")
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("profileWorkbenchValue() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestProfileBoundTypedRelationRoundTripsAllowedBundles(t *testing.T) {
	snapshot := structuredWorkbenchProfileSnapshot(t, []string{"corporate_body", "family", "person"})
	compiled, transformation := compileWorkbenchTestProfile(t, snapshot, "multi-bundle-workbench")
	input := strings.Join([]string{
		"id,title,field_linked_agent.name,field_linked_agent.rel_type,field_linked_agent.vid",
		`1,Contributors,"Example, Avery ; Example University ; Example family",relators:aut ; relators:pbl ; relators:cre,person ; corporate_body ; family`,
	}, "\n") + "\n"
	records, err := (&csvformat.Format{}).Parse(strings.NewReader(input), &format.ParseOptions{
		Spec: transformation, ValueProfile: compiled, Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantSourceIDs := []string{"person:Example, Avery", "corporate_body:Example University", "family:Example family"}
	for index, want := range wantSourceIDs {
		if got := records[0].GetContributors()[index].GetSourceId(); got != want {
			t.Fatalf("contributor %d SourceId = %q, want %q", index+1, got, want)
		}
	}
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, &format.SerializeOptions{
		Spec: transformation, SystemProfile: compiled, IncludeHeader: true, Operation: spec.OperationCreate,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(output.Bytes())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"relators:aut:person:Example, Avery",
		"relators:pbl:corporate_body:Example University",
		"relators:cre:family:Example family",
	}, "|")
	if got := profileTestColumn(t, rows[0], rows[1], "field_linked_agent"); got != want {
		t.Fatalf("multi-bundle typed relation = %q, want %q\n%s", got, want, output.String())
	}
	if _, _, err := profileTypedRelationTarget("family:Example family", "person", []string{"person", "corporate_body"}); err == nil {
		t.Fatal("typed relation accepted a bundle outside the sealed model")
	}
}

func structuredWorkbenchProfileSnapshot(t *testing.T, contributorBundles []string) *model.Snapshot {
	t.Helper()
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "islandora_object",
			Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "changed", SourceType: "changed", Kind: model.ValueDate, Cardinality: 1},
				{Path: "created", SourceType: "created", Kind: model.ValueDate, Cardinality: 1},
				{Path: "uid", SourceType: "entity_reference", Kind: model.ValueReference, Cardinality: 1, Reference: &model.Reference{EntityType: "user"}},
				{Path: "field_coordinates", SourceType: "geolocation", Kind: model.ValueOpaque, Cardinality: -1},
				{Path: "field_vocabulary_link", SourceType: "authority_link", Kind: model.ValueOpaque, Cardinality: -1, InstanceSettings: map[string]any{"authority_sources": []any{"viaf"}}},
				{Path: "field_track", SourceType: "media_track", Kind: model.ValueOpaque, Cardinality: -1},
				{Path: "field_linked_data", SourceType: "linked_data_field", Kind: model.ValueOpaque, Cardinality: -1},
				{
					Path: "field_linked_agent", SourceType: "typed_relation", Kind: model.ValueTypedReference, Cardinality: -1,
					Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: append([]string(nil), contributorBundles...)},
					InstanceSettings: map[string]any{
						"rel_types":        []any{"relators:aut", "relators:cre", "relators:pbl"},
						"handler_settings": map[string]any{"target_bundles": stringSet(contributorBundles)},
					},
				},
			},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func compileWorkbenchTestProfile(t *testing.T, snapshot *model.Snapshot, name string) (*profile.Compiled, *spec.Transformation) {
	t.Helper()
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: name, EntityType: "node", Bundle: "islandora_object",
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
	return compiled, transformation
}

func stringSet(values []string) map[string]any {
	result := make(map[string]any, len(values))
	for _, value := range values {
		result[value] = value
	}
	return result
}

func assertProfileFieldPattern(t *testing.T, field spec.Field, want string) {
	t.Helper()
	for _, validation := range field.Validations {
		if validation.Rule == spec.ValidationPattern {
			if validation.Pattern != want {
				t.Fatalf("field %q pattern = %q, want %q", field.Name, validation.Pattern, want)
			}
			return
		}
	}
	t.Fatalf("field %q has no mapping-declared pattern: %#v", field.Name, field.Validations)
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
