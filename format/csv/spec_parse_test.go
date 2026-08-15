package csv

import (
	"bytes"
	stdcsv "encoding/csv"
	"errors"
	"fmt"
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
	if first.Title != "Example Gazette (volume 1, no.1)" {
		t.Errorf("first title = %q", first.Title)
	}
	if first.FullTitle != first.Title {
		t.Errorf("first full title = %q, want %q", first.FullTitle, first.Title)
	}
	if first.ObjectModel != "Paged Content" {
		t.Errorf("first object model = %q", first.ObjectModel)
	}
	if len(first.Contributors) != 1 || first.Contributors[0].Name != "Example Historical Society" {
		t.Errorf("first contributors = %+v", first.Contributors)
	}
	if len(first.Rights) != 1 || first.Rights[0].Uri != "http://rightsstatements.org/vocab/NoC-US/1.0/" {
		t.Errorf("first rights = %+v", first.Rights)
	}
	wantFile := "/mnt/islandora_staging/Example-Gazette/Example-Gazette-2001-09/Example-Gazette-2001-09_001.tif"
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
	input := strings.NewReader("Node ID,File Path\n1,nested\\file.pdf\n2,/home/import/file.pdf\n3,/etc/passwd.txt\n")
	records, err := (&Format{}).Parse(input, &format.ParseOptions{Spec: spec.FabricatorWorkbench(), Strict: true})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []string{
		"/mnt/islandora_staging/nested/file.pdf",
		"/home/import/file.pdf",
		"/mnt/islandora_staging/etc/passwd.txt",
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

func TestMediaExtensionValidationUsesSealedBundlePolicy(t *testing.T) {
	policies := []spec.MediaExtensionPolicy{
		{MediaType: "file", SelectExtensions: []string{"tif"}, AllowedExtensions: []string{"odt", "tif"}, Fallback: true},
		{MediaType: "document", SelectExtensions: []string{"pdf"}, AllowedExtensions: []string{"pdf"}},
		{MediaType: "video", SelectExtensions: []string{"mp4"}, AllowedExtensions: []string{"mp4"}},
		{MediaType: "model_3d", SelectExtensions: []string{"stl"}, AllowedExtensions: []string{"stl"}},
	}
	tests := []struct {
		name      string
		file      string
		mediaType string
		extension string
		allowed   bool
	}{
		{name: "fallback uses actual file field", file: "report.odt", mediaType: "file", extension: "odt", allowed: true},
		{name: "document selection", file: "report.pdf", mediaType: "document", extension: "pdf", allowed: true},
		{name: "URL query is not part of extension", file: "https://files.example/report.pdf?download=1", mediaType: "document", extension: "pdf", allowed: true},
		{name: "video selection", file: "movie.mp4", mediaType: "video", extension: "mp4", allowed: true},
		{name: "manual custom selection", file: "mesh.stl", mediaType: "model_3d", extension: "stl", allowed: true},
		{name: "unselected legacy extension falls back", file: "movie.m4v", mediaType: "file", extension: "m4v", allowed: false},
		{name: "extension required", file: "README", mediaType: "file", extension: "", allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mediaType, extension, allowed := allowedSpecMediaExtension(test.file, policies)
			if mediaType != test.mediaType || extension != test.extension || allowed != test.allowed {
				t.Fatalf("allowedSpecMediaExtension(%q) = %q, %q, %v; want %q, %q, %v", test.file, mediaType, extension, allowed, test.mediaType, test.extension, test.allowed)
			}
		})
	}
}

func TestNotFutureTimestampValidation(t *testing.T) {
	validation := spec.Validation{Rule: spec.ValidationNotFutureTimestamp}
	field := spec.Field{Name: "created", Hub: "Extra.created"}
	if message := validateSpecFieldRule(validation, "2000-01-02T03:04:05+00:00", []string{"2000-01-02T03:04:05+00:00"}, field, specRowView{}, nil, nil); message != "" {
		t.Fatalf("past timestamp finding = %q", message)
	}
	if message := validateSpecFieldRule(validation, "9999-01-02T03:04:05+00:00", []string{"9999-01-02T03:04:05+00:00"}, field, specRowView{}, nil, nil); !strings.Contains(message, "future") {
		t.Fatalf("future timestamp finding = %q", message)
	}
	if message := validateSpecFieldRule(validation, "tomorrow", []string{"tomorrow"}, field, specRowView{}, nil, nil); !strings.Contains(message, "RFC 3339") {
		t.Fatalf("malformed timestamp finding = %q", message)
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
	input := strings.NewReader("Upload ID,Object Model,Title,Full Title,Resource Type,Make Public (Y/N)\n001,Digital Document,Example,Example full,Book,No\n")
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

func TestParseFabricatorCombinedContributorPreservesETDMetadata(t *testing.T) {
	input := strings.NewReader(`Upload ID,Object Model,Title,Full Title,Resource Type,Contributor
1,Digital Document,Synthetic ETD,Synthetic ETD,Text,"{""name"":""relators:cre:person:Example, Alex"",""institution"":""Example University"",""orcid"":""0000-0000-0000-0000"",""email"":""alex@example.invalid"",""status"":""Graduate Student""}"
`)
	records, err := (&Format{}).Parse(input, &format.ParseOptions{
		Spec: spec.FabricatorWorkbench(), Strict: true, SourceName: "etd.csv",
	})
	if err != nil {
		t.Fatalf("parse combined ETD contributor: %v", err)
	}
	if len(records) != 1 || len(records[0].Contributors) != 1 {
		t.Fatalf("contributors = %#v", records)
	}
	contributor := records[0].Contributors[0]
	if contributor.Name != "Example, Alex" || contributor.RoleCode != "relators:cre" || contributor.Email != "alex@example.invalid" || contributor.Status != "Graduate Student" {
		t.Fatalf("contributor = %#v", contributor)
	}
	if len(contributor.Affiliations) != 1 || contributor.Affiliations[0].Name != "Example University" {
		t.Fatalf("affiliations = %#v", contributor.Affiliations)
	}
	if len(contributor.Identifiers) != 1 || contributor.Identifiers[0].Type != hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID || contributor.Identifiers[0].Value != "0000-0000-0000-0000" {
		t.Fatalf("identifiers = %#v", contributor.Identifiers)
	}
}

func TestParseFabricatorRequiresUploadIDForCreate(t *testing.T) {
	input := strings.NewReader("Upload ID,Object Model,Title,Full Title,Resource Type\n,Digital Document,Example,Example full,Book\n")
	_, err := (&Format{}).Parse(input, &format.ParseOptions{Spec: spec.FabricatorWorkbench(), Strict: true})
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) {
		t.Fatalf("Parse() error = %T %v, want DiagnosticsError", err, err)
	}
	for _, diagnostic := range diagnostics.Diagnostics {
		if diagnostic.Code == "required" && diagnostic.Header == "Upload ID" && diagnostic.Row == 2 {
			return
		}
	}
	t.Fatalf("missing create Upload ID diagnostic: %+v", diagnostics.Diagnostics)
}

func TestParseSpecReportsCellDiagnostics(t *testing.T) {
	input := strings.NewReader("Upload ID,Object Model,Title,Full Title,Creation Date,Resource Type\nnot-a-number,Digital Document,,Example full,not-a-date,\n")
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

func TestParseSpecPreservesModelDeclaredContributorBundles(t *testing.T) {
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "contributor-bundles",
		Source: spec.Table{
			Format:              "csv",
			MultiValueSeparator: " ; ",
			Fields: []spec.Field{
				{Name: "name", Hub: "Contributors.Name", Codec: "contributors"},
				{Name: "type", Hub: "Contributors.Type", Codec: "contributors", InstanceSettings: map[string]any{
					"handler_settings": map[string]any{"target_bundles": map[string]any{
						"corporate_body": "Corporate Body", "family": "Family", "person": "Person",
					}},
				}},
			},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{{Name: "title", Hub: "Title"}},
		},
	}
	sealCSVSpec(t, transformation)

	records, err := (&Format{}).Parse(strings.NewReader(
		"name,type\nExample Person ; Example University ; Example Family,person ; corporate_body ; family\n",
	), &format.ParseOptions{Spec: transformation, Strict: true})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 || len(records[0].GetContributors()) != 3 {
		t.Fatalf("contributors = %#v", records)
	}
	wantIDs := []string{
		"person:Example Person",
		"corporate_body:Example University",
		"family:Example Family",
	}
	for index, want := range wantIDs {
		if got := records[0].GetContributors()[index].GetSourceId(); got != want {
			t.Errorf("contributor %d SourceId = %q, want %q", index+1, got, want)
		}
	}
	if got := records[0].GetContributors()[2].GetType(); got != hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION {
		t.Fatalf("family coarse Hub type = %v, want organization", got)
	}

	_, err = (&Format{}).Parse(strings.NewReader(
		"name,type\nExample Agent,unconfigured_agent\n",
	), &format.ParseOptions{Spec: transformation, Strict: true})
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) || len(diagnostics.Diagnostics) != 1 ||
		diagnostics.Diagnostics[0].Code != "invalid_contributor" ||
		!strings.Contains(diagnostics.Diagnostics[0].Message, "not a model-declared Drupal bundle") {
		t.Fatalf("unconfigured contributor bundle error = %T %+v", err, err)
	}
}

func TestParseSpecCardinalityUsesCanonicalSourceEncoding(t *testing.T) {
	t.Parallel()

	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "source-encoded-cardinality",
		Source: spec.Table{
			Format:              "csv",
			MultiValueSeparator: " ; ",
			Fields: []spec.Field{
				{Name: "title", Label: "Renamed headline", Hub: "Title", Codec: "string", Cardinality: 1},
				{Name: "field_scalar", Label: "Renamed scalar", Hub: "Extra.drupal.field_scalar", Codec: "string", Cardinality: 1},
				{Name: "field_bounded", Label: "Renamed bounded", Hub: "Extra.drupal.field_bounded", Codec: "multi", Cardinality: 2},
			},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{
				{Name: "title", Hub: "Title", Cardinality: 1},
				{Name: "field_scalar", Hub: "Extra.drupal.field_scalar", Cardinality: 1},
				{Name: "field_bounded", Hub: "Extra.drupal.field_bounded", Codec: "multi", Cardinality: 2},
			},
		},
	}
	sealCSVSpec(t, transformation)

	t.Run("canonical title remains scalar", func(t *testing.T) {
		records, err := (&Format{}).Parse(strings.NewReader(
			"Renamed headline,Renamed scalar,Renamed bounded\nA ; literal title,one,alpha ; beta\n",
		), &format.ParseOptions{Spec: transformation, Strict: true})
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if got := records[0].Title; got != "A ; literal title" {
			t.Fatalf("title = %q, want separator preserved as scalar text", got)
		}
		if got := records[0].GetExtra().AsMap()["drupal.field_bounded"]; !reflect.DeepEqual(got, []any{"alpha", "beta"}) {
			t.Fatalf("bounded values = %#v", got)
		}
	})

	tests := []struct {
		name       string
		header     string
		value      string
		wantValues int
		wantMax    int
	}{
		{
			name:       "scalar Drupal field",
			header:     "Renamed scalar",
			value:      "one ; two",
			wantValues: 2,
			wantMax:    1,
		},
		{
			name:       "bounded multi Drupal field",
			header:     "Renamed bounded",
			value:      "one ; two ; three",
			wantValues: 3,
			wantMax:    2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := test.header + "\n" + test.value + "\n"
			_, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{
				Spec:       transformation,
				Strict:     true,
				SourceName: "cardinality.csv",
			})
			var diagnostics *format.DiagnosticsError
			if !errors.As(err, &diagnostics) {
				t.Fatalf("Parse() error = %T %v, want DiagnosticsError", err, err)
			}
			if got := len(diagnostics.Diagnostics); got != 1 {
				t.Fatalf("diagnostics = %+v, want exactly one", diagnostics.Diagnostics)
			}
			diagnostic := diagnostics.Diagnostics[0]
			if diagnostic.Code != "cardinality" || diagnostic.Row != 2 || diagnostic.Column != 1 || diagnostic.Header != test.header {
				t.Fatalf("diagnostic = %+v", diagnostic)
			}
			wantMessage := fmt.Sprintf("got %d values; maximum is %d", test.wantValues, test.wantMax)
			if diagnostic.Message != wantMessage {
				t.Fatalf("message = %q, want %q", diagnostic.Message, wantMessage)
			}
		})
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
