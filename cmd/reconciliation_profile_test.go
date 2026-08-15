package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workbenchformat "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/reconcile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func TestLoadDrupalReconciliationProfileBindsPolicyAndProvenance(t *testing.T) {
	useProfileCommandConfig(t)
	snapshot, definition := publishCommandProfile(t, "repository-objects")

	loaded, err := loadDrupalReconciliationProfile(definition.Name, true)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.compiled.System() != "drupal" || loaded.provenance.System != "drupal" {
		t.Fatalf("loaded system = %q provenance=%#v", loaded.compiled.System(), loaded.provenance)
	}
	if loaded.provenance.ProfileName != definition.Name || loaded.provenance.ProfileFingerprint != definition.Fingerprint.Value || loaded.provenance.ModelFingerprint != snapshot.Fingerprint.Value {
		t.Fatalf("loaded provenance = %#v", loaded.provenance)
	}
	if loaded.transformation == nil || loaded.transformation.Fingerprint.Model != snapshot.Fingerprint.Value || loaded.transformation.Fingerprint.Bundle != "islandora_object" {
		t.Fatalf("profile-bound transformation = %#v", loaded.transformation)
	}
	if loaded.snapshot == nil || loaded.snapshot == snapshot || loaded.snapshot.Fingerprint.Value != snapshot.Fingerprint.Value {
		t.Fatalf("profile-bound canonical model = %#v", loaded.snapshot)
	}
	registry, err := hub.NewIdentifierRegistry(loaded.policy.IdentifierRegistry)
	if err != nil {
		t.Fatal(err)
	}
	if registry.Digest() != loaded.compiled.IdentifierRegistry().Digest() {
		t.Fatalf("policy registry digest = %q, want %q", registry.Digest(), loaded.compiled.IdentifierRegistry().Digest())
	}
}

func TestReconcileFetchedRecordsPersistsProfileProvenance(t *testing.T) {
	useProfileCommandConfig(t)
	_, definition := publishCommandProfile(t, "repository-review")
	reportPath := filepath.Join(t.TempDir(), "matches.json")
	command := &cobra.Command{}
	command.SetContext(t.Context())
	command.SetErr(&bytes.Buffer{})
	records, err := reconcileFetchedRecords(command, []*hubv1.Record{{Title: "A distinct new work"}}, reconcileCommandOptions{
		mode: "assume-new", drupalProfile: definition.Name, reportPath: reportPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("accepted records = %d, want 1", len(records))
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report reconcile.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Provenance.ProfileName != definition.Name || report.Provenance.ProfileFingerprint != definition.Fingerprint.Value || report.Provenance.IdentifierRegistryDigest == "" {
		t.Fatalf("persisted report provenance = %#v", report.Provenance)
	}
}

func TestWriteFetchedArtifactManifestBindsReconciliationProfile(t *testing.T) {
	useProfileCommandConfig(t)
	_, definition := publishCommandProfile(t, "repository-artifacts")
	loaded, err := loadDrupalReconciliationProfile(definition.Name, false)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "artifacts")
	command := &cobra.Command{}
	command.SetErr(&bytes.Buffer{})
	err = writeFetchedRecords(command, []*hubv1.Record{{
		Title: "A distinct new work", FullTitle: "A distinct new work", ObjectModel: "Digital Document",
		Identifiers: []*hubv1.Identifier{{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.1234/profile-bound"}},
	}}, fetchOutputOptions{
		format: "islandora-workbench", artifactDirectory: directory, separator: "|",
	}, reconcileCommandOptions{runtime: &reconcileRuntime{
		mode: reconcile.ModeAssumeNew, policy: loaded.policy, systemProfile: loaded,
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, workbenchformat.ArtifactManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var manifest workbenchformat.ArtifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ProfileFingerprint != loaded.compiled.Fingerprint() || manifest.ModelFingerprint != loaded.compiled.ModelFingerprint() {
		t.Fatalf("manifest target provenance = %q %q", manifest.ProfileFingerprint, manifest.ModelFingerprint)
	}
	if manifest.Spec.Fingerprint != loaded.transformation.Fingerprint.Value {
		t.Fatalf("manifest spec fingerprint = %q, want %q", manifest.Spec.Fingerprint, loaded.transformation.Fingerprint.Value)
	}
}

func TestFetchTransformationRejectsProfileAndSpecFromDifferentModels(t *testing.T) {
	useProfileCommandConfig(t)
	_, definition := publishCommandProfile(t, "repository-mismatch")
	loaded, err := loadDrupalReconciliationProfile(definition.Name, false)
	if err != nil {
		t.Fatal(err)
	}
	transformation := spec.FabricatorWorkbench()
	transformation.Fingerprint.Model = strings.Repeat("b", 64)
	transformation.Fingerprint.Profile = loaded.compiled.Fingerprint()
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(transformation)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "wrong-model.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	output := fetchOutputOptions{format: "islandora-workbench", specPath: path}
	err = resolveFetchTransformation(&output, reconcileCommandOptions{runtime: &reconcileRuntime{systemProfile: loaded}})
	if err == nil || !strings.Contains(err.Error(), "does not match Drupal profile model") {
		t.Fatalf("resolveFetchTransformation() error = %v", err)
	}
}

func TestFetchTransformationRejectsSpecFromAnotherProfileOnSameModel(t *testing.T) {
	useProfileCommandConfig(t)
	_, definition := publishCommandProfile(t, "repository-profile-mismatch")
	loaded, err := loadDrupalReconciliationProfile(definition.Name, false)
	if err != nil {
		t.Fatal(err)
	}
	transformation := *loaded.transformation
	transformation.Fingerprint.Profile = strings.Repeat("a", 64)
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(&transformation)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "wrong-profile.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	output := fetchOutputOptions{format: "islandora-workbench", specPath: path}
	err = resolveFetchTransformation(&output, reconcileCommandOptions{runtime: &reconcileRuntime{systemProfile: loaded}})
	if err == nil || !strings.Contains(err.Error(), "profile fingerprint") {
		t.Fatalf("resolveFetchTransformation() error = %v", err)
	}
}

func TestFetchTransformationRejectsProfileBoundSpecWithoutProfile(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	transformation.Fingerprint.Model = strings.Repeat("b", 64)
	transformation.Fingerprint.Profile = strings.Repeat("a", 64)
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	output := fetchOutputOptions{format: "islandora-workbench", transformation: transformation}
	err := resolveFetchTransformation(&output, reconcileCommandOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires the exact --drupal-profile") {
		t.Fatalf("resolveFetchTransformation() error = %v", err)
	}
}

func TestLoadDrupalReconciliationProfileRejectsWrongSystem(t *testing.T) {
	useProfileCommandConfig(t)
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "omeka-s",
		Entities: []model.Entity{{
			EntityType: "items", Bundle: "", Fields: []model.Field{{Path: "dcterms:title", SourceType: "literal", Kind: model.ValueText, Cardinality: -1}},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition := &profile.Definition{
		Version: profile.CurrentDefinitionVersion, Name: "omeka-items", System: "omeka-s",
		ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: []profile.Mapping{{
			Field: profile.FieldSelector{EntityType: "items", Path: "dcterms:title"}, Hub: "Title",
			Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty,
		}},
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Publish(snapshot, definition, profile.PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := loadDrupalReconciliationProfile(definition.Name, false)
	if err == nil || !strings.Contains(err.Error(), "not drupal") {
		t.Fatalf("wrong-system profile error = %v", err)
	}
}

func TestLoadDrupalReconciliationProfileRequiresLookupWhenLive(t *testing.T) {
	useProfileCommandConfig(t)
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "page", Fields: []model.Field{{Path: "field_note", SourceType: "string", Kind: model.ValueText, Cardinality: 1}},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{Name: "pages", EntityType: "node", Bundle: "page"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Publish(snapshot, definition, profile.PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDrupalReconciliationProfile(definition.Name, true); err == nil || !strings.Contains(err.Error(), "no enabled existing-item lookup") {
		t.Fatalf("lookup-disabled profile error = %v", err)
	}
	if _, err := loadDrupalReconciliationProfile(definition.Name, false); err != nil {
		t.Fatalf("offline identity policy load failed: %v", err)
	}
}
