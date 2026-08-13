package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	workbenchformat "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/reconcile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

type finderFunc func(context.Context, reconcile.Query) ([]reconcile.Candidate, error)

func (finder finderFunc) Candidates(ctx context.Context, query reconcile.Query) ([]reconcile.Candidate, error) {
	return finder(ctx, query)
}

func TestCrosswalkEngineCheckReturnsCellDiagnostics(t *testing.T) {
	engine := NewCrosswalkEngine()
	result, err := engine.Check(context.Background(), [][]string{
		{"Title", "Object Model", "Full Title"},
		{"", "Digital Document", "An example"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !strings.Contains(result["A2"], "required") {
		t.Fatalf("Check() = %#v, want A2 title diagnostic", result)
	}
}

func TestNewCrosswalkEngineWithSpecValidatesPolicy(t *testing.T) {
	if _, err := NewCrosswalkEngineWithSpec(nil); err == nil {
		t.Fatal("NewCrosswalkEngineWithSpec(nil) error = nil")
	}
	transformation := spec.FabricatorWorkbench()
	transformation.Fingerprint.Value = strings.Repeat("0", 64)
	if _, err := NewCrosswalkEngineWithSpec(transformation); err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("NewCrosswalkEngineWithSpec(drift) error = %v", err)
	}
	wrongFormat := spec.FabricatorWorkbench()
	wrongFormat.Source.Format = "drupal"
	if err := wrongFormat.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCrosswalkEngineWithSpec(wrongFormat); err == nil || !strings.Contains(err.Error(), "is not CSV") {
		t.Fatalf("NewCrosswalkEngineWithSpec(wrong source) error = %v", err)
	}
	typo := spec.FabricatorWorkbench()
	for index := range typo.Target.Fields {
		if typo.Target.Fields[index].Name == "field_publisher" {
			typo.Target.Fields[index].Hub = "Publsiher"
			break
		}
	}
	if err := typo.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCrosswalkEngineWithSpec(typo); err == nil || !strings.Contains(err.Error(), `unsupported Islandora Workbench Hub mapping "Publsiher"`) {
		t.Fatalf("NewCrosswalkEngineWithSpec(target Hub typo) error = %v", err)
	}
}

func TestCrosswalkEngineCheckUnderstandsMachineAndHumanHeaderRows(t *testing.T) {
	engine := NewCrosswalkEngine()
	result, err := engine.Check(context.Background(), [][]string{
		{"Machine Name", "title", "field_model", "field_full_title"},
		{"Human Name", "Title", "Object Model", "Full Title"},
		{"", "", "Digital Document", "An example"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !strings.Contains(result["B3"], "required") {
		t.Fatalf("Check() = %#v, want B3 title diagnostic", result)
	}
}

func TestCrosswalkEngineCheckReturnsParserDiagnostics(t *testing.T) {
	engine := NewCrosswalkEngine()
	result, err := engine.Check(context.Background(), [][]string{
		{"Node ID", "Title"},
		{"not-an-id", "Example"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !strings.Contains(result["A2"], "unsigned integer") {
		t.Fatalf("Check() = %#v, want A2 unsigned-integer diagnostic", result)
	}
}

func TestCrosswalkEngineCheckDoesNotRequireTitleForUpdates(t *testing.T) {
	result, err := NewCrosswalkEngine().Check(context.Background(), [][]string{
		{"Node ID", "File Path"},
		{"123", "image.tif"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("Check() = %#v, want no title error for an add-media row", result)
	}
}

func TestCrosswalkEngineCheckDoesNotRequireCreateRelationshipsOnUpdates(t *testing.T) {
	result, err := NewCrosswalkEngine().Check(context.Background(), [][]string{
		{"Node ID", "Object Model", "Resource Type", "Parent Collection", "Page/Item Parent ID", "Title"},
		{"123", "Page", "", "", "", "Updated page"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("Check() = %#v, want existing site relationships to remain a sitectl concern", result)
	}
}

func TestCrosswalkEngineCheckAllowsSubCollectionWithoutResourceType(t *testing.T) {
	result, err := NewCrosswalkEngine().Check(context.Background(), [][]string{
		{"Title", "Object Model", "Full Title", "Resource Type"},
		{"A child collection", "Sub-Collection", "A child collection", ""},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("Check() = %#v, want resource type to be optional for a sub-collection", result)
	}
}

func TestCrosswalkEngineHonorsRequiredResourceTypeExceptionFromSpec(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	transformation.Fingerprint = spec.Fingerprint{}
	for index := range transformation.Source.Fields {
		if transformation.Source.Fields[index].Hub == "ResourceType" {
			transformation.Source.Fields[index].Required = true
		}
	}
	for index := range transformation.Target.Fields {
		if transformation.Target.Fields[index].Hub == "ResourceType" {
			transformation.Target.Fields[index].Required = true
		}
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatalf("NewCrosswalkEngineWithSpec() error = %v", err)
	}
	rows := [][]string{
		{"Title", "Object Model", "Full Title", "Resource Type", "Upload ID"},
		{"A child collection", "Sub Collection", "A child collection", "", "1"},
	}
	result, err := engine.Check(context.Background(), rows)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("Check() = %#v, want the specification's resource-type exception", result)
	}
	artifacts, err := engine.Transform(context.Background(), strings.NewReader("Title,Object Model,Full Title,Resource Type,Upload ID\nA child collection,Sub Collection,A child collection,,1\n"))
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	if len(artifacts) != 2 || artifacts[0].Name != "target.csv" || artifacts[1].Name != workbenchformat.ArtifactManifestName {
		t.Fatalf("Transform() artifacts = %#v", artifacts)
	}
}

func TestCrosswalkEngineCheckReportsLineBreaksInMultiValueCells(t *testing.T) {
	result, err := NewCrosswalkEngine().Check(context.Background(), [][]string{
		{"Title", "Object Model", "Full Title", "Subject Topic (LCSH)", "Description"},
		{"Example", "Digital Document", "Example", "History\n\nLibraries", "A paragraph\n\nAnother paragraph"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if message := result["D2"]; !strings.Contains(message, "Line breaks") {
		t.Fatalf("Check()[D2] = %q, want line-break diagnostic; all = %#v", message, result)
	}
	if message := result["E2"]; message != "" {
		t.Fatalf("Check()[E2] = %q, want multiline prose to remain valid; all = %#v", message, result)
	}
}

func TestCrosswalkEngineCheckRequiresUploadIDForSupplementalOverflow(t *testing.T) {
	result, err := NewCrosswalkEngine().Check(context.Background(), [][]string{
		{"Title", "Object Model", "Full Title", "Upload ID", "Supplemental File"},
		{"Example", "Digital Document", "Example", "", "one.csv ; two.csv"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if message := result["E2"]; !strings.Contains(message, "upload ID") {
		t.Fatalf("Check()[E2] = %q, want upload-ID diagnostic; all = %#v", message, result)
	}
}

func TestCrosswalkEngineCheckPreservesDeterministicFabricatorRules(t *testing.T) {
	longTitle := strings.Repeat("x", 256)
	result, err := NewCrosswalkEngine().Check(context.Background(), [][]string{
		{"Upload ID", "Page/Item Parent ID", "Parent Collection", "Object Model", "Full Title", "Title", "Resource Type", "Catalog or ArchivesSpace URL", "File Path", "Make Public (Y/N)"},
		{"1", "", "not-a-node", "Digital Document", "First", longTitle, "", "relative/path", "paper.pdf", "sometimes"},
		{"1", "99", "", "Video", "Second", "Second", "Moving Image", "https://example.org/item", "movie.pdf", "Yes"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	wants := map[string]string{
		"A3": "Duplicate upload ID",
		"B3": "Unknown parent ID",
		"C2": "unsigned integer",
		"F2": "longer than 255",
		"G2": "resource type",
		"H2": "Invalid URL",
		"I3": "File extension",
		"J2": "Yes or No",
	}
	for cell, message := range wants {
		if !strings.Contains(result[cell], message) {
			t.Errorf("Check()[%s] = %q, want substring %q; all = %#v", cell, result[cell], message, result)
		}
	}
}

func TestCrosswalkEngineCheckParentIDsMayReferForward(t *testing.T) {
	result, err := NewCrosswalkEngine().Check(context.Background(), [][]string{
		{"Upload ID", "Page/Item Parent ID", "Object Model", "Full Title", "Title", "Resource Type"},
		{"2", "1", "Page", "Page", "Page", ""},
		{"1", "", "Paged Content", "Book", "Book", "Book"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if message := result["B2"]; message != "" {
		t.Fatalf("Check()[B2] = %q, want a forward upload-ID reference to be valid; all = %#v", message, result)
	}
}

func TestCrosswalkEngineCheckEmptyInput(t *testing.T) {
	result, err := NewCrosswalkEngine().Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result["A1"] == "" {
		t.Fatalf("Check() = %#v, want A1 error", result)
	}
}

func TestCrosswalkEngineHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine := NewCrosswalkEngine()
	if _, err := engine.Check(ctx, [][]string{{"Title"}}); err == nil {
		t.Fatal("Check() error = nil with canceled context")
	}
	if _, err := engine.Transform(ctx, strings.NewReader("Title\nExample\n")); err == nil {
		t.Fatal("Transform() error = nil with canceled context")
	}
}

func TestCrosswalkEngineMatchesUsesIdentifierFirstAndEmitsReviewCSV(t *testing.T) {
	queries := make([]reconcile.QueryStrategy, 0)
	finder := finderFunc(func(_ context.Context, query reconcile.Query) ([]reconcile.Candidate, error) {
		queries = append(queries, query.Strategy)
		return []reconcile.Candidate{{RepositoryID: "42", Record: &hubv1.Record{
			Title: "Example", Identifiers: []*hubv1.Identifier{{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.1234/example"}},
		}}}, nil
	})
	input := "Title,Object Model,Full Title,Upload ID,DOI\nExample,Digital Document,Example,1,10.1234/example\n"
	result, err := NewCrosswalkEngine().WithFinder(finder).Matches(context.Background(), strings.NewReader(input), "skip")
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report
	if report.Summary.Duplicate != 1 || report.Mode != reconcile.ModeSkip {
		t.Fatalf("report = %#v", result.Report)
	}
	if !reflect.DeepEqual(queries, []reconcile.QueryStrategy{reconcile.QueryByIdentifier}) {
		t.Fatalf("queries = %#v", queries)
	}
	if !bytes.Contains(result.ReviewCSV, []byte("skip or review metadata update")) {
		t.Fatalf("review CSV = %s", result.ReviewCSV)
	}
}

func TestCrosswalkEngineMatchesPreservesConfiguredProfileProvenance(t *testing.T) {
	t.Parallel()
	engine := NewCrosswalkEngine()
	provenance := ReconciliationProvenance{
		System: "drupal", ProfileName: "repository-objects",
		ProfileFingerprint: strings.Repeat("a", 64), ModelFingerprint: strings.Repeat("b", 64),
	}
	if err := engine.ConfigureReconciliation(ReconciliationConfig{Policy: reconcile.PolicyV1(), Provenance: provenance}); err != nil {
		t.Fatal(err)
	}
	input := "Title,Object Model,Full Title,Upload ID,DOI\nExample,Digital Document,Example,1,10.1234/example\n"
	result, err := engine.Matches(context.Background(), strings.NewReader(input), "assume-new")
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Provenance.System != provenance.System || result.Report.Provenance.ProfileName != provenance.ProfileName || result.Report.Provenance.ProfileFingerprint != provenance.ProfileFingerprint || result.Report.Provenance.ModelFingerprint != provenance.ModelFingerprint {
		t.Fatalf("report provenance = %#v, want profile %#v", result.Report.Provenance, provenance)
	}
	if result.Report.Provenance.IdentifierRegistryVersion == "" || result.Report.Provenance.IdentifierRegistryDigest == "" {
		t.Fatalf("registry provenance missing: %#v", result.Report.Provenance)
	}
	if !bytes.Contains(result.ReviewCSV, []byte("repository-objects")) {
		t.Fatalf("review CSV omits profile provenance: %s", result.ReviewCSV)
	}
}

func TestCrosswalkEngineRejectsMismatchedReconciliationProvenance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		provenance ReconciliationProvenance
		contains   string
	}{
		{name: "registry digest", provenance: ReconciliationProvenance{IdentifierRegistryDigest: strings.Repeat("0", 64)}, contains: "digest does not match"},
		{name: "incomplete profile", provenance: ReconciliationProvenance{System: "drupal"}, contains: "must be supplied together"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := NewCrosswalkEngine().ConfigureReconciliation(ReconciliationConfig{Policy: reconcile.PolicyV1(), Provenance: test.provenance})
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("ConfigureReconciliation() error = %v, want substring %q", err, test.contains)
			}
		})
	}
}

func TestCrosswalkEngineMatchesAssumeNewStillDetectsBatchDuplicates(t *testing.T) {
	input := "Title,Object Model,Full Title,Upload ID,DOI\n" +
		"Example,Digital Document,Example,1,10.1234/example\n" +
		"Example,Digital Document,Example,2,10.1234/example\n"
	result, err := NewCrosswalkEngine().Matches(context.Background(), strings.NewReader(input), "assume-new")
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report
	if report.Summary.New != 1 || report.Summary.Duplicate != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if _, err := NewCrosswalkEngine().Matches(context.Background(), strings.NewReader(input), "hold"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unconfigured hold error = %v", err)
	}
}

func TestCrosswalkEngineTransformPlansWorkbenchArtifacts(t *testing.T) {
	tests := []struct {
		name         string
		csv          string
		artifactName string
		contains     string
	}{
		{
			name:         "create",
			csv:          "Title,Object Model,Full Title,Upload ID\nExample,Digital Document,Example full,1\n",
			artifactName: "target.csv",
			contains:     "Example",
		},
		{
			name:         "update",
			csv:          "Node ID,Title\n123,Updated title\n",
			artifactName: "target.update.csv",
			contains:     "Updated title",
		},
		{
			name:         "add media relative",
			csv:          "Node ID,File Path\n123,Coplay-Echoes\\image.tif\n",
			artifactName: "target.add_media.csv",
			contains:     "/mnt/islandora_staging/Coplay-Echoes/image.tif",
		},
		{
			name:         "add media absolute",
			csv:          "Node ID,File Path\n123,/home/import/image.tif\n",
			artifactName: "target.add_media.csv",
			contains:     "/home/import/image.tif",
		},
	}

	engine := NewCrosswalkEngine()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artifacts, err := engine.Transform(context.Background(), strings.NewReader(test.csv))
			if err != nil {
				t.Fatalf("Transform() error = %v", err)
			}
			if len(artifacts) != 2 || artifacts[0].Name != test.artifactName || artifacts[1].Name != workbenchformat.ArtifactManifestName {
				t.Fatalf("Transform() artifacts = %#v, want %s", artifacts, test.artifactName)
			}
			if !bytes.Contains(artifacts[0].Data, []byte(test.contains)) {
				t.Fatalf("artifact %s does not contain %q: %s", artifacts[0].Name, test.contains, artifacts[0].Data)
			}

			again, err := engine.Transform(context.Background(), strings.NewReader(test.csv))
			if err != nil {
				t.Fatalf("second Transform() error = %v", err)
			}
			if !reflect.DeepEqual(artifacts, again) {
				t.Fatal("identical transformation did not produce identical artifact data")
			}
		})
	}
}

func TestCrosswalkEngineTransformManifestBindsReconciliationProfile(t *testing.T) {
	compiled := compileEngineTargetProfile(t)
	transformation := spec.FabricatorWorkbench()
	transformation.Fingerprint.Model = compiled.ModelFingerprint()
	transformation.Fingerprint.Profile = compiled.Fingerprint()
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ConfigureReconciliation(ReconciliationConfig{
		Policy: reconcile.PolicyV1(),
		Provenance: ReconciliationProvenance{
			System:             compiled.System(),
			ProfileName:        compiled.Name(),
			ProfileFingerprint: compiled.Fingerprint(),
			ModelFingerprint:   compiled.ModelFingerprint(),
		},
		TargetProfile: compiled,
	}); err != nil {
		t.Fatalf("ConfigureReconciliation() error = %v", err)
	}
	artifacts, err := engine.Transform(context.Background(), strings.NewReader("Title,Object Model,Full Title,Upload ID\nExample,Digital Document,Example,1\n"))
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	var manifest workbenchformat.ArtifactManifest
	if err := json.Unmarshal(artifacts[len(artifacts)-1].Data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.ProfileFingerprint != compiled.Fingerprint() || manifest.ModelFingerprint != compiled.ModelFingerprint() {
		t.Fatalf("manifest target provenance = %q %q", manifest.ProfileFingerprint, manifest.ModelFingerprint)
	}
}

func TestCrosswalkEngineRejectsUnboundBuiltInSpecWithTargetProfile(t *testing.T) {
	compiled := compileEngineTargetProfile(t)
	err := NewCrosswalkEngine().ConfigureReconciliation(ReconciliationConfig{
		Policy: reconcile.PolicyV1(),
		Provenance: ReconciliationProvenance{
			System: compiled.System(), ProfileName: compiled.Name(),
			ProfileFingerprint: compiled.Fingerprint(), ModelFingerprint: compiled.ModelFingerprint(),
		},
		TargetProfile: compiled,
	})
	if err == nil || !strings.Contains(err.Error(), "unbound transformation") {
		t.Fatalf("ConfigureReconciliation() error = %v", err)
	}
}

func TestCrosswalkEngineRejectsProfileBoundSpecWithoutTargetProfile(t *testing.T) {
	compiled := compileEngineTargetProfile(t)
	transformation := spec.FabricatorWorkbench()
	transformation.Fingerprint.Model = compiled.ModelFingerprint()
	transformation.Fingerprint.Profile = compiled.Fingerprint()
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatal(err)
	}
	err = engine.ConfigureReconciliation(ReconciliationConfig{
		Policy: reconcile.PolicyV1(),
		Provenance: ReconciliationProvenance{
			System: "drupal", ProfileName: "missing", ProfileFingerprint: compiled.Fingerprint(), ModelFingerprint: compiled.ModelFingerprint(),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "requires the exact target profile") {
		t.Fatalf("ConfigureReconciliation() error = %v", err)
	}
}

func TestCrosswalkEngineRejectsSpecAndProfileFromDifferentModels(t *testing.T) {
	compiled := compileEngineTargetProfile(t)
	transformation := spec.FabricatorWorkbench()
	transformation.Fingerprint = spec.Fingerprint{Model: strings.Repeat("c", 64), Profile: compiled.Fingerprint()}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatal(err)
	}
	err = engine.ConfigureReconciliation(ReconciliationConfig{
		Policy: reconcile.PolicyV1(),
		Provenance: ReconciliationProvenance{
			System: compiled.System(), ProfileName: compiled.Name(),
			ProfileFingerprint: compiled.Fingerprint(), ModelFingerprint: compiled.ModelFingerprint(),
		},
		TargetProfile: compiled,
	})
	if err == nil || !strings.Contains(err.Error(), "transformation model fingerprint") {
		t.Fatalf("ConfigureReconciliation() error = %v", err)
	}
}

func TestCrosswalkEngineTransformStagesDisallowedAbsolutePathsAndRejectsURLs(t *testing.T) {
	engine := NewCrosswalkEngine()
	artifacts, err := engine.Transform(context.Background(), strings.NewReader("Node ID,File Path\n123,/etc/passwd\n"))
	if err != nil {
		t.Fatalf("Transform() disallowed absolute error = %v", err)
	}
	if len(artifacts) != 2 || artifacts[1].Name != workbenchformat.ArtifactManifestName || !bytes.Contains(artifacts[0].Data, []byte("/mnt/islandora_staging/etc/passwd")) {
		t.Fatalf("Transform() disallowed absolute artifacts = %#v", artifacts)
	}

	_, err = engine.Transform(context.Background(), strings.NewReader("Node ID,File Path\n123,https://example.edu/media/file.pdf\n"))
	if err == nil || !strings.Contains(err.Error(), "URLs are not supported") {
		t.Fatalf("Transform() URL error = %v", err)
	}
}

func TestCrosswalkEngineTransformPlansSupplementalOverflow(t *testing.T) {
	input := "Title,Object Model,Full Title,Upload ID,Supplemental File\n" +
		"Example,Digital Document,Example,1,one.csv ; two.csv ; three.csv\n"
	artifacts, err := NewCrosswalkEngine().Transform(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	if got, want := artifactAPINameList(artifacts), []string{"target.csv", "target.pending_supplemental.csv", workbenchformat.ArtifactManifestName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("artifact names = %v, want %v", got, want)
	}
	if !bytes.Contains(artifacts[0].Data, []byte("/mnt/islandora_staging/one.csv")) || bytes.Contains(artifacts[0].Data, []byte("two.csv")) {
		t.Fatalf("target.csv did not retain exactly the first supplemental file: %s", artifacts[0].Data)
	}
	wantPending := []byte("1,,/mnt/islandora_staging/two.csv,151326,1")
	if !bytes.Contains(artifacts[1].Data, wantPending) || !bytes.Contains(artifacts[1].Data, []byte("/mnt/islandora_staging/three.csv")) {
		t.Fatalf("pending supplemental artifact is incomplete: %s", artifacts[1].Data)
	}
}

func TestCrosswalkEngineTransformPlansExistingSupplementalOverflow(t *testing.T) {
	input := "Node ID,Title,Supplemental File\n" +
		"200,Updated title,one.csv ; two.csv ; three.csv\n"
	artifacts, err := NewCrosswalkEngine().Transform(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	if got, want := artifactAPINameList(artifacts), []string{"target.update.csv", "target.add_media.csv", workbenchformat.ArtifactManifestName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("artifact names = %v, want %v", got, want)
	}
	if !bytes.Contains(artifacts[0].Data, []byte("/mnt/islandora_staging/one.csv")) || bytes.Contains(artifacts[0].Data, []byte("two.csv")) {
		t.Fatalf("target.update.csv did not retain exactly the first supplemental file: %s", artifacts[0].Data)
	}
	wantAddMedia := []byte("200,/mnt/islandora_staging/two.csv,151326,1")
	if !bytes.Contains(artifacts[1].Data, wantAddMedia) || !bytes.Contains(artifacts[1].Data, []byte("/mnt/islandora_staging/three.csv")) {
		t.Fatalf("add-media supplemental artifact is incomplete: %s", artifacts[1].Data)
	}
}

func TestCrosswalkEngineTransformNormalizesRealFabricatorFixture(t *testing.T) {
	fixture, err := os.Open(filepath.Join("..", "..", "format", "csv", "testdata", "fabricator-sample1.csv"))
	if err != nil {
		t.Fatalf("open Fabricator fixture: %v", err)
	}
	t.Cleanup(func() { _ = fixture.Close() })

	artifacts, err := NewCrosswalkEngine().Transform(context.Background(), fixture)
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	want := []byte("/mnt/islandora_staging/Coplay-Echoes/Coplay-Echoes-1943-09/Coplay-Echoes-1943-09_001.tif")
	for _, artifact := range artifacts {
		if artifact.Name == "target.csv" {
			if !bytes.Contains(artifact.Data, want) {
				t.Fatalf("target.csv does not contain normalized fixture path %q", want)
			}
			return
		}
	}
	t.Fatalf("Transform() artifacts = %#v, want target.csv", artifacts)
}

func TestCrosswalkEngineConcurrentTransformsDoNotShareState(t *testing.T) {
	engine := NewCrosswalkEngine()
	const requests = 32
	var wait sync.WaitGroup
	errorsCh := make(chan error, requests)
	for index := range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			title := fmt.Sprintf("Record %d", index)
			input := fmt.Sprintf("Title,Object Model,Full Title,Upload ID\n%s,Digital Document,%s,%d\n", title, title, index+1)
			artifacts, err := engine.Transform(context.Background(), strings.NewReader(input))
			if err != nil {
				errorsCh <- fmt.Errorf("request %d: %w", index, err)
				return
			}
			if len(artifacts) != 2 || artifacts[1].Name != workbenchformat.ArtifactManifestName || !bytes.Contains(artifacts[0].Data, []byte(title)) {
				errorsCh <- fmt.Errorf("request %d received another request's output", index)
			}
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
}

func TestCoordinateHelpers(t *testing.T) {
	columns := map[string]int{"title": 28}
	if got := validationCoordinate(1, 4, columns, "title"); got != "AB4" {
		t.Fatalf("validationCoordinate() = %q, want AB4", got)
	}
	if got := hubFieldBase("identifiers[0].value"); got != "identifiers" {
		t.Fatalf("hubFieldBase() = %q, want identifiers", got)
	}
}

func artifactAPINameList(artifacts []Artifact) []string {
	names := make([]string, len(artifacts))
	for index, artifact := range artifacts {
		names[index] = artifact.Name
	}
	return names
}

func compileEngineTargetProfile(t *testing.T) *profile.Compiled {
	t.Helper()
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "article",
			Fields: []model.Field{{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1}},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition := &profile.Definition{
		Version: profile.CurrentDefinitionVersion, Name: "http-manifest", System: "drupal",
		ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: []profile.Mapping{{
			Field: profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "title"},
			Hub:   "Title", Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty,
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
