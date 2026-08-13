package islandora_workbench

import (
	"bytes"
	"encoding/csv"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

func TestPlanArtifactsDeterministicNamedOutputs(t *testing.T) {
	create := &hubv1.Record{
		Title:       "Create me",
		FullTitle:   "Create me: full title",
		ObjectModel: "Digital Document",
		Files: []*hubv1.File{
			{Path: "primary.pdf", Role: "primary", MimeType: "application/pdf"},
			{Path: "supplement-1.csv", Role: "supplemental"},
			{Path: "supplement-2.csv", Role: "supplemental"},
			{Path: "supplement-3.csv", Role: "supplemental"},
			{Path: "private.pdf", Role: "unpublished_supplemental"},
		},
		Contributors: []*hubv1.Contributor{{
			Name:     "Doe, Jane",
			RoleCode: "relators:cre",
			Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			Email:    "jane@example.edu",
		}},
	}
	hub.SetExtra(create, "id", "001")

	update := &hubv1.Record{Title: "Update me", Files: []*hubv1.File{
		{Path: "ignored.pdf", Role: "primary"},
		{Path: "supplement.pdf", Role: "supplemental"},
	}}
	hub.SetExtra(update, "node_id", "100")
	hub.SetExtra(update, "_source_columns", "node_id|file|supplemental_file|title")

	addMedia := &hubv1.Record{Files: []*hubv1.File{{Path: "/home/import/new.pdf", Role: "primary"}}}
	hub.SetExtra(addMedia, "node_id", "200")
	hub.SetExtra(addMedia, "_source_columns", "node_id|file")

	options := format.NewSerializeOptions()
	options.Spec = spec.FabricatorWorkbench()
	first, err := PlanArtifacts([]*hubv1.Record{create, update, addMedia}, options)
	if err != nil {
		t.Fatalf("plan artifacts: %v", err)
	}
	second, err := PlanArtifacts([]*hubv1.Record{create, update, addMedia}, options)
	if err != nil {
		t.Fatalf("plan artifacts again: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("artifact planning is not deterministic")
	}

	wantNames := []string{
		"target.csv",
		"target.update.csv",
		"target.add_media.csv",
		"agents.csv",
		"target.pending_supplemental.csv",
		"target.unpublished_supplemental.csv",
		ArtifactManifestName,
	}
	if got := artifactNames(first.Artifacts); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("artifact names = %v, want %v", got, wantNames)
	}

	createRows := readArtifactCSV(t, first.Artifacts[0].Data)
	assertCSVValue(t, createRows, "id", "001")
	assertCSVValue(t, createRows, "file", "/mnt/islandora_staging/primary.pdf")
	assertCSVValue(t, createRows, "supplemental_file", "/mnt/islandora_staging/supplement-1.csv")
	assertCSVValue(t, createRows, "field_full_title", "Create me: full title")

	updateRows := readArtifactCSV(t, first.Artifacts[1].Data)
	assertCSVValue(t, updateRows, "node_id", "100")
	if slicesContain(updateRows[0], "file") {
		t.Fatalf("update header unexpectedly contains file: %v", updateRows[0])
	}
	assertCSVValue(t, updateRows, "supplemental_file", "/mnt/islandora_staging/supplement.pdf")

	mediaRows := readArtifactCSV(t, first.Artifacts[2].Data)
	if got, want := mediaRows[0], []string{"node_id", "file"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("add-media header = %v, want %v", got, want)
	}
	assertCSVValue(t, mediaRows, "file", "/home/import/new.pdf")

	pending := readArtifactCSV(t, first.Artifacts[4].Data)
	wantPending := [][]string{
		{"id", "node_id", "file", "media_use_tid", "published"},
		{"001", "", "/mnt/islandora_staging/supplement-2.csv", "151326", "1"},
		{"001", "", "/mnt/islandora_staging/supplement-3.csv", "151326", "1"},
	}
	if !reflect.DeepEqual(pending, wantPending) {
		t.Fatalf("pending supplemental rows = %v, want %v", pending, wantPending)
	}

	unpublished := readArtifactCSV(t, first.Artifacts[5].Data)
	if got, want := unpublished[1], []string{"001", "", "/mnt/islandora_staging/private.pdf", "151326", "0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unpublished row = %v, want %v", got, want)
	}
}

func TestPlanArtifactsMergesExistingNodeSupplementalOverflowIntoAddMedia(t *testing.T) {
	record := &hubv1.Record{Files: []*hubv1.File{
		{Path: "primary.pdf", Role: "primary"},
		{Path: "supplement-1.csv", Role: "supplemental"},
		{Path: "supplement-2.csv", Role: "supplemental"},
	}}
	hub.SetExtra(record, "node_id", "200")
	hub.SetExtra(record, "_source_columns", "node_id|file")

	plan, err := PlanArtifacts([]*hubv1.Record{record}, &format.SerializeOptions{Spec: spec.FabricatorWorkbench(), IncludeHeader: true})
	if err != nil {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
	if got, want := artifactNames(plan.Artifacts), []string{"target.add_media.csv", ArtifactManifestName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("artifact names = %v, want %v", got, want)
	}
	wantRows := [][]string{
		{"node_id", "file", "media_use_tid", "published"},
		{"200", "/mnt/islandora_staging/primary.pdf", "", "1"},
		{"200", "/mnt/islandora_staging/supplement-1.csv", "151326", "1"},
		{"200", "/mnt/islandora_staging/supplement-2.csv", "151326", "1"},
	}
	if got := readArtifactCSV(t, plan.Artifacts[0].Data); !reflect.DeepEqual(got, wantRows) {
		t.Fatalf("add-media rows = %v, want %v", got, wantRows)
	}
	if got, want := plan.Artifacts[0].Records, 3; got != want {
		t.Fatalf("add-media records = %d, want %d", got, want)
	}
}

func TestPlanArtifactsRequiresUploadIDForPendingSupplementalOverflow(t *testing.T) {
	record := &hubv1.Record{
		Title:       "Example",
		FullTitle:   "Example",
		ObjectModel: "Digital Document",
		Files: []*hubv1.File{
			{Path: "supplement-1.csv", Role: "supplemental"},
			{Path: "supplement-2.csv", Role: "supplemental"},
		},
	}
	_, err := PlanArtifacts([]*hubv1.Record{record}, &format.SerializeOptions{Spec: spec.FabricatorWorkbench(), IncludeHeader: true})
	if err == nil || !strings.Contains(err.Error(), "upload ID") {
		t.Fatalf("PlanArtifacts() error = %v, want missing upload ID", err)
	}
}

func TestPlanArtifactsUsesSpecificationDefaultsForSupplementalOverflow(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	transformation.Defaults[spec.SupplementalMediaUseTIDDefault] = "98765"
	transformation.Defaults[spec.SupplementalPublishedDefault] = "0"
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	record := &hubv1.Record{
		Title:       "Example",
		FullTitle:   "Example",
		ObjectModel: "Digital Document",
		Files: []*hubv1.File{
			{Path: "supplement-1.csv", Role: "supplemental"},
			{Path: "supplement-2.csv", Role: "supplemental"},
		},
	}
	hub.SetExtra(record, "id", "1")

	plan, err := PlanArtifacts([]*hubv1.Record{record}, &format.SerializeOptions{Spec: transformation, IncludeHeader: true})
	if err != nil {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
	pending := readArtifactCSV(t, plan.Artifacts[1].Data)
	if got, want := pending[1][3:], []string{"98765", "0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pending workflow defaults = %v, want %v", got, want)
	}
}

func TestSerializeRejectsMultipleSupplementalFilesWithoutArtifactPlanner(t *testing.T) {
	record := &hubv1.Record{Files: []*hubv1.File{
		{Path: "supplement-1.csv", Role: "supplemental"},
		{Path: "supplement-2.csv", Role: "supplemental"},
	}}
	err := (&Format{}).Serialize(&bytes.Buffer{}, []*hubv1.Record{record}, &format.SerializeOptions{
		Spec:      spec.FabricatorWorkbench(),
		Operation: spec.OperationCreate,
	})
	if err == nil || !strings.Contains(err.Error(), "artifact planning") {
		t.Fatalf("Serialize() error = %v, want artifact-planning guidance", err)
	}
}

func TestSerializeHonorsTargetColumnsAndOperationalHubFields(t *testing.T) {
	record := &hubv1.Record{
		Title:            "Short title",
		FullTitle:        "A deliberately much longer full title",
		ObjectModel:      "Paged Content",
		IsPublic:         false,
		AddCoverpage:     true,
		AccessCondition:  "open",
		LocalRestriction: "restricted",
		Departments:      []string{"Special Collections", "Library"},
		Files:            []*hubv1.File{{Path: "item.tif", Role: "primary", MimeType: "image/tiff", SizeBytes: 42}},
	}
	hub.SetExtra(record, "_present_is_public", true)
	hub.SetExtra(record, "_present_add_coverpage", true)

	options := format.NewSerializeOptions()
	options.Spec = spec.FabricatorWorkbench()
	options.Operation = spec.OperationCreate
	options.Columns = []string{
		"file",
		"field_full_title",
		"published",
		"field_add_coverpage",
		"field_department_name",
		"field_media_type",
		"field_extent",
		"field_local_restriction",
		"field_access",
	}

	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, options); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	rows := readArtifactCSV(t, output.Bytes())
	if got := rows[0]; !reflect.DeepEqual(got, options.Columns) {
		t.Fatalf("header = %v, want %v", got, options.Columns)
	}
	assertCSVValue(t, rows, "file", "/mnt/islandora_staging/item.tif")
	assertCSVValue(t, rows, "field_full_title", record.FullTitle)
	assertCSVValue(t, rows, "published", "0")
	assertCSVValue(t, rows, "field_add_coverpage", "1")
	assertCSVValue(t, rows, "field_department_name", "Special Collections|Library")
	assertCSVValue(t, rows, "field_media_type", "image/tiff")
	assertCSVValue(t, rows, "field_local_restriction", "1")
	assertCSVValue(t, rows, "field_access", "open")
	if extent := csvValue(t, rows, "field_extent"); !strings.Contains(extent, `"bytes"`) || !strings.Contains(extent, `"42"`) {
		t.Errorf("field_extent = %q", extent)
	}
}

func TestSerializeUsesHubPathForCustomTargetName(t *testing.T) {
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "custom-target",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{{Name: "source_title", Hub: "Title"}},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{{Name: "custom_title", Hub: "Title"}},
		},
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := (&Format{}).Serialize(&output, []*hubv1.Record{{Title: "Mapped by Hub path"}}, &format.SerializeOptions{
		Spec:          transformation,
		IncludeHeader: true,
	})
	if err != nil {
		t.Fatalf("serialize custom target: %v", err)
	}
	rows := readArtifactCSV(t, output.Bytes())
	if got, want := rows, [][]string{{"custom_title"}, {"Mapped by Hub path"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestSerializeTreatsTargetHubMappingAsAuthoritative(t *testing.T) {
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "authoritative-target-hub",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{{Name: "source_title", Hub: "Title"}},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{{Name: "title", Hub: "Publisher"}},
		},
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := (&Format{}).Serialize(&output, []*hubv1.Record{{
		Title:     "This must not win",
		Publisher: "Mapped publisher",
	}}, &format.SerializeOptions{Spec: transformation, IncludeHeader: true})
	if err != nil {
		t.Fatalf("serialize authoritative target Hub: %v", err)
	}
	if got, want := readArtifactCSV(t, output.Bytes()), [][]string{{"title"}, {"Mapped publisher"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}

	transformation.Target.Fields[0].Hub = "Publsiher"
	output.Reset()
	err = (&Format{}).Serialize(&output, []*hubv1.Record{{Publisher: "value"}}, &format.SerializeOptions{Spec: transformation})
	if err == nil || !strings.Contains(err.Error(), `unsupported Islandora Workbench Hub mapping "Publsiher"`) {
		t.Fatalf("Serialize() target Hub typo error = %v", err)
	}
}

func TestTargetSpecOwnsMultiValueSeparatorInSerializeAndPlan(t *testing.T) {
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "target-separator",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{{Name: "title", Hub: "Title"}},
		},
		Target: spec.Table{
			Format:              "islandora-workbench",
			MultiValueSeparator: "^",
			Fields:              []spec.Field{{Name: "field_department_name", Hub: "Departments", Codec: "multi"}},
		},
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	record := &hubv1.Record{Title: "Example", Departments: []string{"Archives", "Library"}}
	options := &format.SerializeOptions{
		Spec:                transformation,
		IncludeHeader:       true,
		MultiValueSeparator: "!",
	}

	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, options); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	assertCSVValue(t, readArtifactCSV(t, output.Bytes()), "field_department_name", "Archives^Library")

	plan, err := PlanArtifacts([]*hubv1.Record{record}, options)
	if err != nil {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
	if len(plan.Artifacts) != 2 || plan.Artifacts[0].Name != "target.csv" || plan.Artifacts[1].Name != ArtifactManifestName {
		t.Fatalf("artifacts = %+v", plan.Artifacts)
	}
	assertCSVValue(t, readArtifactCSV(t, plan.Artifacts[0].Data), "field_department_name", "Archives^Library")
}

func TestPlanArtifactsEnforcesCompiledRequiredTargetsOnlyForCreates(t *testing.T) {
	transformation, err := spec.CompileDrupalDirectory(filepath.Join("..", "..", "spec", "testdata", "drupal"), spec.DrupalCompileOptions{Bundle: "islandora_object"})
	if err != nil {
		t.Fatal(err)
	}

	missingIdentifier := &hubv1.Record{Title: "Example", FullTitle: "Example full"}
	_, err = PlanArtifacts([]*hubv1.Record{missingIdentifier}, &format.SerializeOptions{Spec: transformation, IncludeHeader: true})
	if err == nil || !strings.Contains(err.Error(), `target field "field_identifier" is required for create`) {
		t.Fatalf("PlanArtifacts() missing required aggregate error = %v", err)
	}

	withIdentifier := &hubv1.Record{
		Title:       "Example",
		FullTitle:   "Example full",
		Identifiers: []*hubv1.Identifier{hub.NewIdentifier("10.1234/example", hubv1.IdentifierType_IDENTIFIER_TYPE_DOI)},
	}
	if _, err := PlanArtifacts([]*hubv1.Record{withIdentifier}, &format.SerializeOptions{Spec: transformation, IncludeHeader: true}); err != nil {
		t.Fatalf("PlanArtifacts() with required aggregate error = %v", err)
	}

	partialUpdate := &hubv1.Record{Title: "Updated title"}
	hub.SetExtra(partialUpdate, "node_id", "123")
	if _, err := PlanArtifacts([]*hubv1.Record{partialUpdate}, &format.SerializeOptions{Spec: transformation, IncludeHeader: true}); err != nil {
		t.Fatalf("PlanArtifacts() partial update error = %v", err)
	}
}

func TestSerializeAppliesRequiredForOnlyToListedOperation(t *testing.T) {
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "target-required-policy",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{{Name: "title", Hub: "Title"}},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{
				{Name: "schema_required", Hub: "Publisher", Required: true, Operations: []spec.Operation{spec.OperationCreate, spec.OperationUpdate}},
				{Name: "update_required", Hub: "Extra.update_required", RequiredFor: []spec.Operation{spec.OperationUpdate}, Operations: []spec.Operation{spec.OperationCreate, spec.OperationUpdate}},
			},
		},
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if err := (&Format{}).Serialize(&bytes.Buffer{}, []*hubv1.Record{{}}, &format.SerializeOptions{Spec: transformation}); err == nil || !strings.Contains(err.Error(), `target field "schema_required" is required for create`) {
		t.Fatalf("default serialization Required error = %v", err)
	}

	if err := (&Format{}).Serialize(&bytes.Buffer{}, []*hubv1.Record{{Publisher: "Present on create"}}, &format.SerializeOptions{
		Spec:      transformation,
		Operation: spec.OperationCreate,
	}); err != nil {
		t.Fatalf("create incorrectly enforced update RequiredFor: %v", err)
	}

	update := &hubv1.Record{}
	hub.SetExtra(update, "update_required", "present")
	if err := (&Format{}).Serialize(&bytes.Buffer{}, []*hubv1.Record{update}, &format.SerializeOptions{
		Spec:      transformation,
		Operation: spec.OperationUpdate,
	}); err != nil {
		t.Fatalf("update incorrectly enforced schema Required: %v", err)
	}

	if err := (&Format{}).Serialize(&bytes.Buffer{}, []*hubv1.Record{{}}, &format.SerializeOptions{
		Spec:      transformation,
		Operation: spec.OperationUpdate,
	}); err == nil || !strings.Contains(err.Error(), `target field "update_required" is required for update`) {
		t.Fatalf("update RequiredFor error = %v", err)
	}
}

func TestSerializeOmitsIgnoredTargetFields(t *testing.T) {
	t.Parallel()

	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "ignored-target",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{{Name: "title", Hub: "Title"}},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{
				{Name: "ignored_required", Codec: "ignore", Required: true},
				{Name: "ignored_default", Codec: "ignore", Default: "must-not-be-serialized"},
				{
					Name:        "ignored_update",
					Codec:       "ignore",
					RequiredFor: []spec.Operation{spec.OperationUpdate},
					Operations:  []spec.Operation{spec.OperationUpdate},
				},
				{Name: "title", Hub: "Title"},
			},
		},
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	record := &hubv1.Record{Title: "Only this column"}

	for _, operation := range []spec.Operation{spec.OperationCreate, spec.OperationUpdate} {
		operation := operation
		t.Run(string(operation), func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{
				Spec:          transformation,
				Operation:     operation,
				IncludeHeader: true,
				Columns:       []string{"ignored_required", "ignored_default", "ignored_update", "title"},
			})
			if err != nil {
				t.Fatalf("Serialize() error = %v", err)
			}
			if got, want := readArtifactCSV(t, output.Bytes()), [][]string{{"title"}, {record.Title}}; !reflect.DeepEqual(got, want) {
				t.Fatalf("rows = %v, want %v", got, want)
			}
		})
	}
}

func TestDynamicExtraTargetRoundTrip(t *testing.T) {
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "dynamic-extra",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{{Name: "custom", Hub: "Extra.custom"}},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{{Name: "field_site_specific", Hub: "Extra.custom"}},
		},
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	record := &hubv1.Record{}
	hub.SetExtra(record, "custom", "site value")
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{Spec: transformation, IncludeHeader: true}); err != nil {
		t.Fatalf("serialize dynamic extra: %v", err)
	}
	profile := &mapping.Profile{Fields: map[string]mapping.FieldMapping{
		"field_site_specific": {IR: "Extra.custom"},
	}}
	parsed, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), &format.ParseOptions{Profile: profile, Strict: true})
	if err != nil {
		t.Fatalf("parse dynamic extra: %v", err)
	}
	if got := hub.GetExtraString(parsed[0], "custom"); got != "site value" {
		t.Fatalf("dynamic extra = %q, want site value", got)
	}
}

func TestBuiltInTargetRoundTripsOperationalFieldsStrictly(t *testing.T) {
	record := &hubv1.Record{
		Title:        "Round trip",
		FullTitle:    "Round trip full title",
		ObjectModel:  "Paged Content",
		IsPublic:     true,
		AddCoverpage: true,
		Files:        []*hubv1.File{{Path: "round-trip.tif", Role: "primary", MimeType: "image/tiff"}},
	}
	hub.SetExtra(record, "id", "10")
	hub.SetExtra(record, "_present_is_public", true)
	hub.SetExtra(record, "_present_add_coverpage", true)

	options := format.NewSerializeOptions()
	options.Spec = spec.FabricatorWorkbench()
	options.Operation = spec.OperationCreate
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, options); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	parsed, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), &format.ParseOptions{Strict: true, SourceName: "target.csv"})
	if err != nil {
		t.Fatalf("strict parse: %v\n%s", err, output.String())
	}
	if len(parsed) != 1 {
		t.Fatalf("parsed records = %d", len(parsed))
	}
	got := parsed[0]
	if got.FullTitle != record.FullTitle || got.ObjectModel != record.ObjectModel || !got.IsPublic || !got.AddCoverpage {
		t.Errorf("round-tripped record = %+v", got)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "/mnt/islandora_staging/round-trip.tif" || got.Files[0].MimeType != "image/tiff" {
		t.Errorf("round-tripped files = %+v", got.Files)
	}
}

func artifactNames(artifacts []Artifact) []string {
	names := make([]string, len(artifacts))
	for i, artifact := range artifacts {
		names[i] = artifact.Name
	}
	return names
}

func readArtifactCSV(t *testing.T, data []byte) [][]string {
	t.Helper()
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("read artifact CSV: %v\n%s", err, data)
	}
	return rows
}

func assertCSVValue(t *testing.T, rows [][]string, column, want string) {
	t.Helper()
	if got := csvValue(t, rows, column); got != want {
		t.Errorf("%s = %q, want %q", column, got, want)
	}
}

func csvValue(t *testing.T, rows [][]string, column string) string {
	t.Helper()
	if len(rows) < 2 {
		t.Fatalf("CSV has %d rows, want header and data", len(rows))
	}
	for index, name := range rows[0] {
		if name == column {
			return rows[1][index]
		}
	}
	t.Fatalf("column %q not found in %v", column, rows[0])
	return ""
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
