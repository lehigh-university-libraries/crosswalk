package csv

import (
	"bytes"
	stdcsv "encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	workbench "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

func TestParseFabricatorGoldenFixture(t *testing.T) {
	path := filepath.Join("testdata", "fabricator-sample1.csv")
	input, err := os.Open(path)
	if err != nil {
		t.Fatalf("open Fabricator fixture: %v", err)
	}
	t.Cleanup(func() { _ = input.Close() })

	records, err := (&Format{}).Parse(input, &format.ParseOptions{
		Spec:       spec.FabricatorWorkbench(),
		Strict:     true,
		StripHTML:  true,
		SourceName: "sample1.csv",
	})
	if err != nil {
		t.Fatalf("parse Fabricator fixture: %v", err)
	}
	if got, want := len(records), 195; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}
	first := records[0]
	if first.Title == "Title" || first.Title == "Human Name" {
		t.Fatalf("header row was parsed as metadata: title %q", first.Title)
	}
	if first.Title != "Coplay Echoes (volume 1, no.1)" {
		t.Errorf("first title = %q", first.Title)
	}
	if first.FullTitle != first.Title {
		t.Errorf("first full title = %q, want %q", first.FullTitle, first.Title)
	}
	if first.ObjectModel != "Paged Content" {
		t.Errorf("first object model = %q", first.ObjectModel)
	}
	if len(first.Contributors) != 1 || first.Contributors[0].Name != "People of Coplay" {
		t.Errorf("first contributors = %+v", first.Contributors)
	}
	if len(first.Rights) != 1 || first.Rights[0].Uri != "http://rightsstatements.org/vocab/NoC-US/1.0/" {
		t.Errorf("first rights = %+v", first.Rights)
	}
	wantFile := "/mnt/islandora_staging/Coplay-Echoes/Coplay-Echoes-1943-09/Coplay-Echoes-1943-09_001.tif"
	if got := records[1].Files[0].Path; got != wantFile {
		t.Errorf("normalized file path = %q", got)
	}

	plan, err := workbench.PlanArtifacts(records, &format.SerializeOptions{
		Spec:                spec.FabricatorWorkbench(),
		IncludeHeader:       true,
		MultiValueSeparator: "|",
	})
	if err != nil {
		t.Fatalf("plan Fabricator fixture: %v", err)
	}
	if len(plan.Artifacts) == 0 || plan.Artifacts[0].Name != "target.csv" || plan.Artifacts[0].Records != 195 {
		t.Fatalf("create artifact = %+v", plan.Artifacts)
	}
	rows, err := stdcsv.NewReader(bytes.NewReader(plan.Artifacts[0].Data)).ReadAll()
	if err != nil {
		t.Fatalf("parse planned target.csv: %v", err)
	}
	if len(rows) != 196 {
		t.Fatalf("target.csv rows = %d, want header + 195 records", len(rows))
	}
	titleColumn := -1
	for index, header := range rows[0] {
		if header == "title" {
			titleColumn = index
			break
		}
	}
	if titleColumn < 0 || rows[1][titleColumn] != first.Title {
		t.Fatalf("first serialized title does not match Hub record")
	}
	fileColumn := -1
	for index, header := range rows[0] {
		if header == "file" {
			fileColumn = index
			break
		}
	}
	if fileColumn < 0 || rows[2][fileColumn] != wantFile {
		t.Fatalf("first relative fixture file was not staged in target.csv: %q", rows[2][fileColumn])
	}
}

func TestParseSpecNormalizesFileReferences(t *testing.T) {
	input := strings.NewReader("Node ID,File Path\n1,nested\\file.pdf\n2,/home/import/file.pdf\n3,/etc/passwd\n")
	records, err := (&Format{}).Parse(input, &format.ParseOptions{Spec: spec.FabricatorWorkbench(), Strict: true})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []string{
		"/mnt/islandora_staging/nested/file.pdf",
		"/home/import/file.pdf",
		"/mnt/islandora_staging/etc/passwd",
	}
	for index := range want {
		if got := records[index].Files[0].Path; got != want[index] {
			t.Errorf("record %d file = %q, want %q", index+1, got, want[index])
		}
	}
}

func TestParseSpecRejectsFileURLs(t *testing.T) {
	_, err := (&Format{}).Parse(strings.NewReader("Node ID,File Path\n1,https://example.edu/private.pdf\n"), &format.ParseOptions{
		Spec:       spec.FabricatorWorkbench(),
		Strict:     true,
		SourceName: "url.csv",
	})
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) || len(diagnostics.Diagnostics) != 1 || !strings.Contains(diagnostics.Diagnostics[0].Message, "URLs are not supported") {
		t.Fatalf("Parse() URL error = %T %+v", err, err)
	}
}

func TestParseSpecRejectsFilePathTraversal(t *testing.T) {
	_, err := (&Format{}).Parse(strings.NewReader("Node ID,File Path\n1,../private.pdf\n"), &format.ParseOptions{
		Spec:       spec.FabricatorWorkbench(),
		Strict:     true,
		SourceName: "traversal.csv",
	})
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) || len(diagnostics.Diagnostics) != 1 {
		t.Fatalf("Parse() error = %T %+v, want one diagnostic", err, err)
	}
	got := diagnostics.Diagnostics[0]
	if got.Code != "invalid_value" || got.Row != 2 || got.Column != 2 || !strings.Contains(got.Message, "escapes") {
		t.Fatalf("traversal diagnostic = %+v", got)
	}
}

func TestParseSpecRequiresSealedExactValueProfileBinding(t *testing.T) {
	t.Parallel()

	drupalModel := csvValueProfileModel(t, "drupal")
	matching := compileCSVValueProfile(t, drupalModel, "matching", "Title")
	sameModelWrongProfile := compileCSVValueProfile(t, drupalModel, "wrong-profile", "FullTitle")
	otherModel := csvValueProfileModel(t, "drupal")
	otherModel.Entities[0].Fields = append(otherModel.Entities[0].Fields, model.Field{
		Path: "field_note", SourceType: "string", Kind: model.ValueText, Cardinality: 1,
	})
	if err := otherModel.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	otherModelProfile := compileCSVValueProfile(t, otherModel, "other-model", "Title")
	omekaModel := csvValueProfileModel(t, "omeka-s")
	omekaProfile := compileCSVValueProfile(t, omekaModel, "omeka", "Title")

	matchingSpec := boundCSVSpec(t, matching.Fingerprint(), matching.ModelFingerprint())
	modelMismatchSpec := boundCSVSpec(t, otherModelProfile.Fingerprint(), matching.ModelFingerprint())
	omekaBoundSpec := boundCSVSpec(t, omekaProfile.Fingerprint(), omekaProfile.ModelFingerprint())
	unsealed := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "unsealed",
		Source:  spec.Table{Format: "csv", Fields: []spec.Field{{Name: "title", Hub: "Title"}}},
		Target:  spec.Table{Format: "islandora-workbench", Fields: []spec.Field{{Name: "title", Hub: "Title"}}},
	}

	tests := []struct {
		name       string
		transform  *spec.Transformation
		value      *profile.Compiled
		want       string
		wantRecord bool
	}{
		{name: "matching", transform: matchingSpec, value: matching, wantRecord: true},
		{name: "unsealed", transform: unsealed, want: "unsealed transformation specification"},
		{name: "bound without profile", transform: matchingSpec, want: "requires the exact value profile"},
		{name: "unbound with profile", transform: spec.FabricatorWorkbench(), value: matching, want: "unbound transformation cannot use a value profile"},
		{name: "same model wrong profile", transform: matchingSpec, value: sameModelWrongProfile, want: "profile fingerprint does not match"},
		{name: "wrong model", transform: modelMismatchSpec, value: otherModelProfile, want: "model fingerprint does not match"},
		{name: "wrong system", transform: omekaBoundSpec, value: omekaProfile, want: `value profile system "omeka-s" does not match transformation target system "drupal"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			records, err := (&Format{}).Parse(strings.NewReader("title\nExample\n"), &format.ParseOptions{
				Spec: test.transform, ValueProfile: test.value, Strict: true,
			})
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("Parse() error = %v, want substring %q", err, test.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if !test.wantRecord || len(records) != 1 || records[0].GetTitle() != "Example" {
				t.Fatalf("Parse() records = %#v", records)
			}
		})
	}
	if _, err := (&Format{}).Parse(strings.NewReader(""), &format.ParseOptions{Spec: matchingSpec}); err == nil || !strings.Contains(err.Error(), "requires the exact value profile") {
		t.Fatalf("empty profile-bound Parse() error = %v, want exact value-profile requirement", err)
	}
}

func TestParseFabricatorHumanHeader(t *testing.T) {
	input := strings.NewReader("Upload ID,Object Model,Title,Full Title,Make Public (Y/N)\n001,Digital Document,Example,Example full,No\n")
	records, err := (&Format{}).Parse(input, &format.ParseOptions{
		Spec:       spec.FabricatorWorkbench(),
		Strict:     true,
		SourceName: "human.csv",
	})
	if err != nil {
		t.Fatalf("parse human header: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("record count = %d, want 1", len(records))
	}
	if records[0].Title != "Example" || records[0].FullTitle != "Example full" {
		t.Errorf("record = %+v", records[0])
	}
}

func TestParseSpecReportsCellDiagnostics(t *testing.T) {
	input := strings.NewReader("Upload ID,Object Model,Title,Full Title,Creation Date\nnot-a-number,Digital Document,,Example full,not-a-date\n")
	_, err := (&Format{}).Parse(input, &format.ParseOptions{
		Spec:       spec.FabricatorWorkbench(),
		Strict:     true,
		SourceName: "bad.csv",
	})
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) {
		t.Fatalf("error = %T %v, want DiagnosticsError", err, err)
	}
	want := map[string]bool{"invalid_value": false, "invalid_date": false, "required": false}
	for _, diagnostic := range diagnostics.Diagnostics {
		if _, ok := want[diagnostic.Code]; ok {
			want[diagnostic.Code] = diagnostic.Row == 2 && diagnostic.Column > 0
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing cell-level %q diagnostic: %+v", code, diagnostics.Diagnostics)
		}
	}
}

func TestParseSpecRejectsUnknownAndRaggedColumns(t *testing.T) {
	input := strings.NewReader("Upload ID,Object Model,Title,Full Title,Surprise\n001,Digital Document,Example,Example full,value,extra\n")
	_, err := (&Format{}).Parse(input, &format.ParseOptions{
		Spec:       spec.FabricatorWorkbench(),
		Strict:     true,
		SourceName: "ragged.csv",
	})
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) {
		t.Fatalf("error = %T %v, want DiagnosticsError", err, err)
	}
	if len(diagnostics.Diagnostics) == 0 || diagnostics.Diagnostics[0].Code != "unknown_column" {
		t.Fatalf("diagnostics = %+v", diagnostics.Diagnostics)
	}
}

func TestParseSpecAppliesDefaultsAndCardinality(t *testing.T) {
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "defaults-and-cardinality",
		Source: spec.Table{
			Format:              "csv",
			MultiValueSeparator: "|",
			Fields: []spec.Field{
				{Name: "title", Hub: "Title", Default: "Untitled"},
				{Name: "keywords", Hub: "Subjects.keywords", Codec: "multi", Cardinality: 1},
			},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{{Name: "title", Hub: "Title"}},
		},
	}
	sealCSVSpec(t, transformation)
	records, err := (&Format{}).Parse(strings.NewReader("title,keywords\n,one\n"), &format.ParseOptions{Spec: transformation})
	if err != nil {
		t.Fatalf("parse default: %v", err)
	}
	if records[0].Title != "Untitled" {
		t.Errorf("default title = %q", records[0].Title)
	}

	_, err = (&Format{}).Parse(strings.NewReader("title,keywords\nExample,one|two\n"), &format.ParseOptions{Spec: transformation})
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) || len(diagnostics.Diagnostics) != 1 || diagnostics.Diagnostics[0].Code != "cardinality" {
		t.Fatalf("cardinality error = %T %+v", err, err)
	}
}

func TestParseSpecSkipsIgnoredSourceFields(t *testing.T) {
	t.Parallel()

	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "ignored-source",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{
				{
					Name:        "discard",
					Hub:         "Extra.discard",
					Codec:       "ignore",
					Required:    true,
					RequiredFor: []spec.Operation{spec.OperationUpdate},
					Default:     "must-not-be-applied",
					Operations:  []spec.Operation{spec.OperationUpdate},
				},
				{Name: "title", Hub: "Title", Required: true},
			},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{{Name: "title", Hub: "Title"}},
		},
	}
	sealCSVSpec(t, transformation)
	records, err := (&Format{}).Parse(strings.NewReader("discard,title\nnonempty,First\n,Second\n"), &format.ParseOptions{
		Spec:       transformation,
		Strict:     true,
		SourceName: "ignored.csv",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(records), 2; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}
	for index, record := range records {
		if _, ok := hub.GetExtra(record, "discard"); ok {
			t.Errorf("record %d applied ignored value/default", index+1)
		}
		if got := hub.GetExtraString(record, "_source_columns"); got != "title" {
			t.Errorf("record %d source columns = %q, want title", index+1, got)
		}
	}
}

func TestParseSpecReportsRequiredEmptyCellOnce(t *testing.T) {
	t.Parallel()

	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "required-once",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{
				{Name: "title", Hub: "Title", Required: true},
				{Name: "note", Hub: "Description"},
			},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{{Name: "title", Hub: "Title"}},
		},
	}
	sealCSVSpec(t, transformation)
	_, err := (&Format{}).Parse(strings.NewReader("title,note\n,present\n"), &format.ParseOptions{
		Spec:       transformation,
		Strict:     true,
		SourceName: "required.csv",
	})
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) {
		t.Fatalf("Parse() error = %T %v, want DiagnosticsError", err, err)
	}
	if got := len(diagnostics.Diagnostics); got != 1 {
		t.Fatalf("diagnostics = %+v, want exactly one", diagnostics.Diagnostics)
	}
	if diagnostic := diagnostics.Diagnostics[0]; diagnostic.Code != "required" || diagnostic.Row != 2 || diagnostic.Column != 1 {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
}

func TestGenericExtraStringAndEDTFRoundTrip(t *testing.T) {
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "generic-extra-types",
		Source: spec.Table{
			Format:              "csv",
			MultiValueSeparator: " ; ",
			Fields: []spec.Field{
				{Name: "title", Hub: "Title", Codec: "string", Cardinality: 1},
				{Name: "field_custom_text", Hub: "Extra.drupal.field_custom_text", Codec: "string", Cardinality: 1},
				{Name: "field_custom_date", Hub: "Extra.drupal.field_custom_date", Codec: "edtf", Cardinality: 1},
				{Name: "field_custom_dates", Hub: "Extra.drupal.field_custom_dates", Codec: "edtf"},
			},
		},
		Target: spec.Table{
			Format:              "islandora-workbench",
			MultiValueSeparator: "|",
			Fields: []spec.Field{
				{Name: "title", Hub: "Title", Codec: "string", Cardinality: 1},
				{Name: "field_custom_text", Hub: "Extra.drupal.field_custom_text", Codec: "string", Cardinality: 1},
				{Name: "field_custom_date", Hub: "Extra.drupal.field_custom_date", Codec: "edtf", Cardinality: 1},
				{Name: "field_custom_dates", Hub: "Extra.drupal.field_custom_dates", Codec: "edtf"},
			},
		},
	}
	sealCSVSpec(t, transformation)
	input := "title,field_custom_text,field_custom_date,field_custom_dates\nExample,hello,2024-01,2023 ; 2024\n"
	records, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{Spec: transformation, Strict: true})
	if err != nil {
		t.Fatalf("parse generic Extra fields: %v", err)
	}
	if got := records[0].GetExtra().AsMap()["drupal.field_custom_text"]; got != "hello" {
		t.Fatalf("scalar string Extra = %#v", got)
	}
	if got := records[0].GetExtra().AsMap()["drupal.field_custom_date"]; got != "2024-01" {
		t.Fatalf("scalar EDTF Extra = %#v", got)
	}
	if got := records[0].GetExtra().AsMap()["drupal.field_custom_dates"]; !reflect.DeepEqual(got, []any{"2023", "2024"}) {
		t.Fatalf("multi EDTF Extra = %#v", got)
	}

	var output bytes.Buffer
	if err := (&workbench.Format{}).Serialize(&output, records, &format.SerializeOptions{
		Spec:                transformation,
		IncludeHeader:       true,
		MultiValueSeparator: "|",
	}); err != nil {
		t.Fatalf("serialize generic Extra fields: %v", err)
	}
	rows, err := stdcsv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("read generic Extra output: %v", err)
	}
	if got, want := rows[1], []string{"Example", "hello", "2024-01", "2023|2024"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("generic Extra row = %v, want %v", got, want)
	}

	badInput := "title,field_custom_text,field_custom_date,field_custom_dates\nExample,hello,not-a-date,2024\n"
	_, err = (&Format{}).Parse(strings.NewReader(badInput), &format.ParseOptions{Spec: transformation, Strict: true})
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) || len(diagnostics.Diagnostics) != 1 || diagnostics.Diagnostics[0].Code != "invalid_date" {
		t.Fatalf("invalid Extra EDTF error = %T %v", err, err)
	}
}

func TestInferSourceOperationRequiresPrimaryMediaForAddMedia(t *testing.T) {
	t.Parallel()

	transformation := spec.FabricatorWorkbench()
	record := &hubv1.Record{}
	hub.SetExtra(record, "node_id", "123")
	if got := inferSourceOperation(record, []string{"node_id"}, transformation); got != spec.OperationUpdate {
		t.Fatalf("node-only operation = %q, want update", got)
	}
	record.Files = []*hubv1.File{{Path: "media.tif", Role: "primary"}}
	if got := inferSourceOperation(record, []string{"node_id", "file"}, transformation); got != spec.OperationAddMedia {
		t.Fatalf("primary-file operation = %q, want add_media", got)
	}
}

func csvValueProfileModel(t *testing.T, system string) *model.Snapshot {
	t.Helper()
	entityType, bundle := "node", "article"
	if system == "omeka-s" {
		entityType, bundle = "resource", ""
	}
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion,
		System:  system,
		Entities: []model.Entity{{
			EntityType: entityType,
			Bundle:     bundle,
			Fields: []model.Field{{
				Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1,
			}},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func compileCSVValueProfile(t *testing.T, snapshot *model.Snapshot, name, hubPath string) *profile.Compiled {
	t.Helper()
	entity := snapshot.Entities[0]
	definition := &profile.Definition{
		Version:          profile.CurrentDefinitionVersion,
		Name:             name,
		System:           snapshot.System,
		ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: []profile.Mapping{{
			Field: profile.FieldSelector{EntityType: entity.EntityType, Bundle: entity.Bundle, Path: "title"},
			Hub:   hubPath, Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty,
		}},
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func boundCSVSpec(t *testing.T, profileFingerprint, modelFingerprint string) *spec.Transformation {
	t.Helper()
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "bound-csv",
		Source:  spec.Table{Format: "csv", Fields: []spec.Field{{Name: "title", Hub: "Title"}}},
		Target:  spec.Table{Format: "islandora-workbench", Fields: []spec.Field{{Name: "title", Hub: "Title"}}},
		Fingerprint: spec.Fingerprint{
			Profile: profileFingerprint,
			Model:   modelFingerprint,
		},
	}
	sealCSVSpec(t, transformation)
	return transformation
}

func sealCSVSpec(t *testing.T, transformation *spec.Transformation) {
	t.Helper()
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
}
