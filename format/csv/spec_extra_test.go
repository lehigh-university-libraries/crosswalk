package csv_test

import (
	"bytes"
	"encoding/csv"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	csvformat "github.com/lehigh-university-libraries/crosswalk/format/csv"
	workbenchformat "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

func TestCompiledDrupalGenericExtraRoundTrip(t *testing.T) {
	transformation, err := spec.CompileDrupalDirectory(filepath.Join("..", "..", "spec", "testdata", "drupal"), spec.DrupalCompileOptions{Bundle: "islandora_object"})
	if err != nil {
		t.Fatal(err)
	}
	// config/sync supplies the site data model, not context-host filesystem or
	// taxonomy policy. A deployer must add and reseal those values explicitly.
	transformation.Defaults = map[string]string{
		spec.FileStagingRootDefault:                    "/srv/islandora/staging",
		spec.FileAllowedAbsoluteRootsDefault:           "/srv/islandora/staging",
		spec.UnpublishedSupplementalMediaUseTIDDefault: "151326",
		spec.UnpublishedSupplementalPublishedDefault:   "0",
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("id,title,field_full_title,field_identifier.attr0=doi,field_custom_tags,field_rating,field_local_date,field_local_code\nUpload ID,Title,Complete Title,DOI,Local Tags,Rating,Local Date,Local Code\n1,Example,Example full,10.1234/example,alpha ; beta,1 ; 2,2025-08,ABC-123\n")
	records, err := (&csvformat.Format{}).Parse(input, &format.ParseOptions{Spec: transformation, Strict: true})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, ok := hub.GetExtra(records[0], "drupal.field_custom_tags"); !ok || len(got.([]any)) != 2 {
		t.Fatalf("generic tags Extra = %#v, found = %v", got, ok)
	}
	if got, ok := hub.GetExtra(records[0], "drupal.field_rating"); !ok || len(got.([]any)) != 2 {
		t.Fatalf("generic rating Extra = %#v, found = %v", got, ok)
	}
	if got := hub.GetExtraString(records[0], "drupal.field_local_date"); got != "2025-08" {
		t.Fatalf("generic EDTF Extra = %q", got)
	}
	if got := hub.GetExtraString(records[0], "drupal.field_local_code"); got != "ABC-123" {
		t.Fatalf("generic scalar Extra = %q", got)
	}

	var output bytes.Buffer
	if err := (&workbenchformat.Format{}).Serialize(&output, records, &format.SerializeOptions{
		Spec:                transformation,
		Operation:           spec.OperationCreate,
		IncludeHeader:       true,
		MultiValueSeparator: "|",
	}); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	columns := make(map[string]int)
	for index, name := range rows[0] {
		columns[name] = index
	}
	if got := rows[1][columns["field_custom_tags"]]; got != "alpha|beta" {
		t.Fatalf("round-trip tags = %q", got)
	}
	if got := rows[1][columns["field_rating"]]; got != "1|2" {
		t.Fatalf("round-trip ratings = %q", got)
	}
	if got := rows[1][columns["field_local_date"]]; got != "2025-08" {
		t.Fatalf("round-trip EDTF date = %q", got)
	}
	if got := rows[1][columns["field_local_code"]]; got != "ABC-123" {
		t.Fatalf("round-trip scalar = %q", got)
	}

	hub.SetExtra(records[0], "id", "1")
	records[0].Files = append(records[0].Files, &hubv1.File{Path: "private/supplement.pdf", Role: "unpublished_supplemental"})
	plan, err := workbenchformat.PlanArtifacts(records, &format.SerializeOptions{Spec: transformation, IncludeHeader: true})
	if err != nil {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
	foundSupplemental := false
	for _, artifact := range plan.Artifacts {
		if artifact.Operation == spec.OperationUnpublishedSupplemental {
			foundSupplemental = true
			if !bytes.Contains(artifact.Data, []byte("151326,0")) {
				t.Fatalf("unpublished artifact did not use compiled defaults: %s", artifact.Data)
			}
		}
	}
	if !foundSupplemental {
		t.Fatal("compiled specification did not plan unpublished supplemental artifact")
	}
}

func TestCompiledDrupalRequiredAggregateGroupPreservesUpdateSemantics(t *testing.T) {
	transformation, err := spec.CompileDrupalDirectory(filepath.Join("..", "..", "spec", "testdata", "drupal"), spec.DrupalCompileOptions{Bundle: "islandora_object"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = (&csvformat.Format{}).Parse(strings.NewReader("id,title,field_full_title,field_identifier.attr0=doi\n1,Example,Example full,\n"), &format.ParseOptions{
		Spec:       transformation,
		Strict:     true,
		SourceName: "create.csv",
	})
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) {
		t.Fatalf("missing aggregate identifier error = %T %v, want DiagnosticsError", err, err)
	}
	foundGroup := false
	for _, diagnostic := range diagnostics.Diagnostics {
		if diagnostic.Code == "required_group" && diagnostic.Row == 2 && diagnostic.Column == 4 && strings.Contains(diagnostic.Message, "field_identifier") {
			foundGroup = true
		}
	}
	if !foundGroup {
		t.Fatalf("required aggregate diagnostics = %+v", diagnostics.Diagnostics)
	}

	createRecords, err := (&csvformat.Format{}).Parse(strings.NewReader("id,title,field_full_title,field_identifier.attr0=doi\n1,Example,Example full,10.1234/example\n"), &format.ParseOptions{
		Spec:   transformation,
		Strict: true,
	})
	if err != nil || len(createRecords) != 1 {
		t.Fatalf("create with one aggregate member records = %d, error = %v", len(createRecords), err)
	}

	updateRecords, err := (&csvformat.Format{}).Parse(strings.NewReader("node_id,title\n123,Updated title\n"), &format.ParseOptions{
		Spec:   transformation,
		Strict: true,
	})
	if err != nil || len(updateRecords) != 1 {
		t.Fatalf("update without required create fields records = %d, error = %v", len(updateRecords), err)
	}
}
