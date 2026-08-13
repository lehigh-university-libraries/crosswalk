package islandora_workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

func TestPlanArtifactsAppendsDeterministicIntegrityManifest(t *testing.T) {
	options := format.NewSerializeOptions()
	options.Spec = spec.FabricatorWorkbench()
	record := &hubv1.Record{Title: "Example", FullTitle: "Example", ObjectModel: "Digital Document"}

	first, err := PlanArtifacts([]*hubv1.Record{record}, options)
	if err != nil {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
	second, err := PlanArtifacts([]*hubv1.Record{record}, options)
	if err != nil {
		t.Fatalf("second PlanArtifacts() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical plans produced different manifest bytes")
	}
	if got, want := artifactNames(first.Artifacts), []string{"target.csv", ArtifactManifestName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("artifact names = %v, want %v", got, want)
	}
	manifestArtifact := first.Artifacts[len(first.Artifacts)-1]
	if manifestArtifact.MediaType != ArtifactManifestMediaType || manifestArtifact.Records != 0 {
		t.Fatalf("manifest artifact metadata = %#v", manifestArtifact)
	}
	if !strings.HasSuffix(string(manifestArtifact.Data), "\n") {
		t.Fatal("manifest has no deterministic trailing newline")
	}

	manifest := decodeArtifactManifest(t, manifestArtifact.Data)
	if manifest.Version != ArtifactManifestVersion {
		t.Fatalf("manifest version = %d, want %d", manifest.Version, ArtifactManifestVersion)
	}
	if manifest.Spec.Name != first.SpecName || manifest.Spec.Version != first.SpecVersion || manifest.Spec.Fingerprint != first.Fingerprint {
		t.Fatalf("manifest spec = %#v, plan = %q %q %q", manifest.Spec, first.SpecName, first.SpecVersion, first.Fingerprint)
	}
	if manifest.ProfileFingerprint != "" || manifest.ModelFingerprint != "" {
		t.Fatalf("manifest invented target profile provenance: %#v", manifest)
	}
	if manifest.Policy == nil || manifest.Policy.PathMode != ArtifactManifestPathModeStagedPOSIX || manifest.Policy.StagingRoot != "/mnt/islandora_staging" {
		t.Fatalf("manifest path policy = %#v", manifest.Policy)
	}
	if manifest.Policy.AllowedAbsoluteRoots == nil || !reflect.DeepEqual(*manifest.Policy.AllowedAbsoluteRoots, []string{"/home", "/mnt"}) {
		t.Fatalf("manifest allowed roots = %#v", manifest.Policy.AllowedAbsoluteRoots)
	}
	if manifest.Policy.SupplementalMediaUseTID != "151326" || manifest.Policy.PendingSupplementalPublished != "1" ||
		manifest.Policy.UnpublishedSupplementalMediaUseTID != "151326" || manifest.Policy.UnpublishedSupplementalPublished != "0" {
		t.Fatalf("manifest supplemental policy = %#v", manifest.Policy)
	}
	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != "target.csv" {
		t.Fatalf("manifest artifacts = %#v", manifest.Artifacts)
	}
	entry := manifest.Artifacts[0]
	target := first.Artifacts[0]
	digest := sha256.Sum256(target.Data)
	if entry.MediaType != target.MediaType || entry.SHA256 != hex.EncodeToString(digest[:]) || entry.Bytes != int64(len(target.Data)) || entry.CSVRows != 1 {
		t.Fatalf("manifest entry = %#v", entry)
	}
	for _, entry := range manifest.Artifacts {
		if entry.Path == ArtifactManifestName {
			t.Fatal("manifest digests itself")
		}
	}
}

func TestPlanArtifactsManifestIncludesTargetProfileFingerprints(t *testing.T) {
	compiled := compileManifestTargetProfile(t)
	transformation := minimalWorkbenchTransformation(t)
	transformation.Defaults = map[string]string{spec.FileStagingRootDefault: "/srv/workbench"}
	transformation.Fingerprint.Model = compiled.ModelFingerprint()
	transformation.Fingerprint.Profile = compiled.Fingerprint()
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanArtifacts([]*hubv1.Record{{Title: "Example"}}, &format.SerializeOptions{
		Spec:          transformation,
		SystemProfile: compiled,
		IncludeHeader: true,
	})
	if err != nil {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
	manifest := decodeArtifactManifest(t, plan.Artifacts[len(plan.Artifacts)-1].Data)
	if manifest.ProfileFingerprint != compiled.Fingerprint() || manifest.ModelFingerprint != compiled.ModelFingerprint() {
		t.Fatalf("target fingerprints = %q %q, want %q %q", manifest.ProfileFingerprint, manifest.ModelFingerprint, compiled.Fingerprint(), compiled.ModelFingerprint())
	}
	if manifest.Policy == nil || manifest.Policy.AllowedAbsoluteRoots == nil || len(*manifest.Policy.AllowedAbsoluteRoots) != 0 {
		t.Fatalf("known empty absolute-root allowlist = %#v", manifest.Policy)
	}
}

func TestPlanArtifactsRejectsUnboundSpecWithTargetProfile(t *testing.T) {
	compiled := compileManifestTargetProfile(t)
	transformation := minimalWorkbenchTransformation(t)
	_, err := PlanArtifacts([]*hubv1.Record{{Title: "Example"}}, &format.SerializeOptions{
		Spec: transformation, SystemProfile: compiled, IncludeHeader: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unbound transformation") {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
}

func TestPlanArtifactsRejectsProfileBoundSpecWithoutTargetProfile(t *testing.T) {
	compiled := compileManifestTargetProfile(t)
	transformation := minimalWorkbenchTransformation(t)
	transformation.Fingerprint.Model = compiled.ModelFingerprint()
	transformation.Fingerprint.Profile = compiled.Fingerprint()
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	_, err := PlanArtifacts([]*hubv1.Record{{Title: "Example"}}, &format.SerializeOptions{
		Spec: transformation, IncludeHeader: true,
	})
	if err == nil || !strings.Contains(err.Error(), "requires the exact target system profile") {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
}

func TestPlanArtifactsRejectsNonDrupalTargetProfile(t *testing.T) {
	compiled := compileManifestProfileForSystem(t, "omeka-s")
	transformation := minimalWorkbenchTransformation(t)
	transformation.Fingerprint.Model = compiled.ModelFingerprint()
	transformation.Fingerprint.Profile = compiled.Fingerprint()
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	_, err := PlanArtifacts([]*hubv1.Record{{Title: "Example"}}, &format.SerializeOptions{
		Spec: transformation, SystemProfile: compiled,
	})
	if err == nil || !strings.Contains(err.Error(), `profile system "omeka-s" is not drupal`) {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
}

func TestPlanArtifactsRejectsSpecFromAnotherProfileOnSameModel(t *testing.T) {
	compiled := compileManifestTargetProfile(t)
	transformation := minimalWorkbenchTransformation(t)
	transformation.Fingerprint.Model = compiled.ModelFingerprint()
	transformation.Fingerprint.Profile = strings.Repeat("a", 64)
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	_, err := PlanArtifacts([]*hubv1.Record{{Title: "Example"}}, &format.SerializeOptions{
		Spec: transformation, SystemProfile: compiled, IncludeHeader: true,
	})
	if err == nil || !strings.Contains(err.Error(), "profile fingerprint") {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
}

func TestBuildArtifactContractBindsReviewedSpecAndProfile(t *testing.T) {
	compiled := compileManifestTargetProfile(t)
	transformation := minimalWorkbenchTransformation(t)
	transformation.Fingerprint.Model = compiled.ModelFingerprint()
	transformation.Fingerprint.Profile = compiled.Fingerprint()
	transformation.Defaults = map[string]string{
		spec.FileStagingRootDefault:          "/srv/workbench",
		spec.FileAllowedAbsoluteRootsDefault: "/srv/workbench",
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	contract, err := BuildArtifactContract(transformation, compiled)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version != ArtifactContractVersion || contract.Spec.Fingerprint != transformation.Fingerprint.Value {
		t.Fatalf("contract spec = %#v", contract)
	}
	if contract.ProfileFingerprint != compiled.Fingerprint() || contract.ModelFingerprint != compiled.ModelFingerprint() {
		t.Fatalf("contract provenance = %#v", contract)
	}
	if contract.Policy == nil || contract.Policy.StagingRoot != "/srv/workbench" {
		t.Fatalf("contract policy = %#v", contract.Policy)
	}

	unbound := minimalWorkbenchTransformation(t)
	if _, err := BuildArtifactContract(unbound, compiled); err == nil || !strings.Contains(err.Error(), "unbound transformation") {
		t.Fatalf("BuildArtifactContract(unbound) error = %v", err)
	}
	if _, err := BuildArtifactContract(transformation, nil); err == nil || !strings.Contains(err.Error(), "requires the exact target system profile") {
		t.Fatalf("BuildArtifactContract(profile-bound without profile) error = %v", err)
	}
	sameModelWrongProfile := minimalWorkbenchTransformation(t)
	sameModelWrongProfile.Fingerprint.Model = compiled.ModelFingerprint()
	sameModelWrongProfile.Fingerprint.Profile = strings.Repeat("a", 64)
	if err := sameModelWrongProfile.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildArtifactContract(sameModelWrongProfile, compiled); err == nil || !strings.Contains(err.Error(), "profile fingerprint") {
		t.Fatalf("BuildArtifactContract(same-model wrong-profile) error = %v", err)
	}
}

func TestPlanArtifactsManifestOmitsUnavailablePolicyAndComputesSpecFingerprint(t *testing.T) {
	transformation := minimalWorkbenchTransformation(t)
	plan, err := PlanArtifacts([]*hubv1.Record{{Title: "Example"}}, &format.SerializeOptions{Spec: transformation, IncludeHeader: true})
	if err != nil {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
	manifest := decodeArtifactManifest(t, plan.Artifacts[len(plan.Artifacts)-1].Data)
	if manifest.Policy != nil {
		t.Fatalf("manifest invented Workbench policy: %#v", manifest.Policy)
	}
	computed, err := transformation.ComputeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Spec.Fingerprint != computed || plan.Fingerprint != computed {
		t.Fatalf("computed spec fingerprint = %q / %q, want %q", manifest.Spec.Fingerprint, plan.Fingerprint, computed)
	}
}

func TestArtifactTrustBoundariesRejectUnsignedSpecifications(t *testing.T) {
	transformation := minimalWorkbenchTransformation(t)
	transformation.Fingerprint = spec.Fingerprint{}
	_, planErr := PlanArtifacts([]*hubv1.Record{{Title: "Example"}}, &format.SerializeOptions{Spec: transformation})
	if planErr == nil || !strings.Contains(planErr.Error(), "unsealed transformation specification") {
		t.Fatalf("PlanArtifacts(unsigned) error = %v", planErr)
	}
	if _, err := BuildArtifactContract(transformation, nil); err == nil || !strings.Contains(err.Error(), "unsealed transformation specification") {
		t.Fatalf("BuildArtifactContract(unsigned) error = %v", err)
	}
}

func TestAppendArtifactManifestRejectsIncorrectCSVRowCount(t *testing.T) {
	transformation := minimalWorkbenchTransformation(t)
	plan := &ArtifactPlan{Artifacts: []Artifact{{
		Name: "target.csv", MediaType: "text/csv", Data: []byte("title\nExample\n"), Records: 2,
	}}}
	err := appendArtifactManifest(plan, &format.SerializeOptions{Spec: transformation})
	if err == nil || !strings.Contains(err.Error(), "declares 2 CSV rows but contains 1") {
		t.Fatalf("appendArtifactManifest() error = %v", err)
	}
}

func decodeArtifactManifest(t *testing.T, data []byte) ArtifactManifest {
	t.Helper()
	var manifest ArtifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v\n%s", err, data)
	}
	return manifest
}

func minimalWorkbenchTransformation(t *testing.T) *spec.Transformation {
	t.Helper()
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "manifest-test",
		Source: spec.Table{Format: "csv", Fields: []spec.Field{{
			Name: "title", Hub: "Title", Codec: "string",
		}}},
		Target: spec.Table{Format: "islandora-workbench", Fields: []spec.Field{{
			Name: "title", Hub: "Title", Codec: "string",
		}}},
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatalf("seal test transformation: %v", err)
	}
	return transformation
}

func compileManifestTargetProfile(t *testing.T) *profile.Compiled {
	return compileManifestProfileForSystem(t, "drupal")
}

func compileManifestProfileForSystem(t *testing.T, system string) *profile.Compiled {
	t.Helper()
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion,
		System:  system,
		Entities: []model.Entity{{
			EntityType: "node",
			Bundle:     "article",
			Fields:     []model.Field{{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1}},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition := &profile.Definition{
		Version:          profile.CurrentDefinitionVersion,
		Name:             "manifest-target",
		System:           system,
		ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: []profile.Mapping{{
			Field:  profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "title"},
			Hub:    "Title",
			Decode: "text",
			Encode: "text",
			Merge:  profile.MergeFirstNonempty,
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
