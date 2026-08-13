package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/model"
)

func TestPublishStoresAndLoadsExactPair(t *testing.T) {
	useTestConfigDirectory(t)
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)

	compiled, err := Publish(snapshot, &definition, PublishOptions{})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if compiled.Fingerprint() != definition.Fingerprint.Value {
		t.Fatalf("compiled fingerprint = %q, want %q", compiled.Fingerprint(), definition.Fingerprint.Value)
	}
	stored, err := LoadStored(definition.Name)
	if err != nil {
		t.Fatalf("LoadStored() error = %v", err)
	}
	if stored.Model.Fingerprint != snapshot.Fingerprint || stored.Definition.Fingerprint != definition.Fingerprint {
		t.Fatalf("stored pair = model %#v profile %#v", stored.Model.Fingerprint, stored.Definition.Fingerprint)
	}
	if stored.Model.Provenance != (model.Provenance{}) {
		t.Fatalf("content-addressed model retained non-addressed provenance: %#v", stored.Model.Provenance)
	}
	modelPath, err := ModelPath(snapshot.Fingerprint.Value)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(modelPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("stored model info = %#v, %v", info, err)
	}
	if _, err := Publish(snapshot, &definition, PublishOptions{}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate Publish() error = %v", err)
	}
}

func TestStoreModelIsIdenticalAcrossAcquisitionProvenance(t *testing.T) {
	useTestConfigDirectory(t)
	first := testModel(t)
	first.Provenance = model.Provenance{SiteUUID: "one", SiteName: "First", ConfigHash: strings.Repeat("1", 64)}
	second := testModel(t)
	second.Provenance = model.Provenance{SiteUUID: "two", SiteName: "Second", ConfigHash: strings.Repeat("2", 64)}
	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("provenance changed schema fingerprint: %#v != %#v", first.Fingerprint, second.Fingerprint)
	}
	if err := StoreModel(first); err != nil {
		t.Fatal(err)
	}
	path, err := ModelPath(first.Fingerprint.Value)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreModel(second); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("the same model address produced different bytes for different acquisition provenance")
	}
}

func TestFailedForcedPublishPreservesOldPair(t *testing.T) {
	useTestConfigDirectory(t)
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	if _, err := Publish(snapshot, &definition, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	profilePath, err := ProfilePath(definition.Name)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}

	replacementModel := testModel(t)
	replacementModel.Entities[0].Fields = append(replacementModel.Entities[0].Fields, model.Field{
		Path: "field_replacement", SourceType: "string", Kind: model.ValueText, Cardinality: 1,
	})
	if err := replacementModel.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	replacement := testDefinition(t, replacementModel)
	replacement.Description = "replacement that must not be partially published"
	profilesDirectory, err := ProfilesDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(profilesDirectory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(profilesDirectory, 0o755) })
	if _, err := Publish(replacementModel, &replacement, PublishOptions{Force: true}); err == nil {
		t.Fatal("forced Publish() succeeded in a non-writable profiles directory")
	}
	if err := os.Chmod(profilesDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed forced publication changed the stored definition")
	}
	stored, err := LoadStored(definition.Name)
	if err != nil {
		t.Fatalf("old pair is no longer loadable: %v", err)
	}
	if stored.Definition.Description != definition.Description {
		t.Fatalf("stored description = %q, want %q", stored.Definition.Description, definition.Description)
	}
	if _, err := LoadModel(replacementModel.Fingerprint.Value); err != nil {
		t.Fatalf("safe orphan replacement model was not retained: %v", err)
	}
}

func TestModelStorageRejectsSymlinkObjectAndDirectory(t *testing.T) {
	t.Run("config root", func(t *testing.T) {
		target := t.TempDir()
		link := filepath.Join(t.TempDir(), "config")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
		SetConfigDir(link)
		t.Cleanup(func() { SetConfigDir("") })
		if err := StoreModel(testModel(t)); err == nil || !strings.Contains(err.Error(), "not a regular directory") {
			t.Fatalf("StoreModel() error = %v", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		configDirectory := useTestConfigDirectory(t)
		target := t.TempDir()
		if err := os.Symlink(target, filepath.Join(configDirectory, "models")); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
		if err := StoreModel(testModel(t)); err == nil || !strings.Contains(err.Error(), "not a regular directory") {
			t.Fatalf("StoreModel() error = %v", err)
		}
	})

	t.Run("object", func(t *testing.T) {
		useTestConfigDirectory(t)
		snapshot := testModel(t)
		if err := EnsureModelsDir(); err != nil {
			t.Fatal(err)
		}
		modelPath, err := ModelPath(snapshot.Fingerprint.Value)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "target.yaml")
		if err := os.WriteFile(target, []byte("not a model\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, modelPath); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
		if err := StoreModel(snapshot); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("StoreModel() error = %v", err)
		}
	})
}

func TestLoadStoredRejectsModelFingerprintMismatch(t *testing.T) {
	useTestConfigDirectory(t)
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	if _, err := Publish(snapshot, &definition, PublishOptions{}); err != nil {
		t.Fatal(err)
	}

	different := testModel(t)
	different.Entities[0].Fields = append(different.Entities[0].Fields, model.Field{
		Path: "field_new", SourceType: "string", Kind: model.ValueText, Cardinality: 1,
	})
	if err := different.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	data := mustMarshalYAML(t, different)
	modelPath, err := ModelPath(snapshot.Fingerprint.Value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStored(definition.Name); err == nil || !strings.Contains(err.Error(), "does not match object name") {
		t.Fatalf("LoadStored() error = %v", err)
	}
}

func TestDeleteDefinitionRetainsModel(t *testing.T) {
	useTestConfigDirectory(t)
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	if _, err := Publish(snapshot, &definition, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteDefinition(definition.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDefinition(definition.Name); err == nil {
		t.Fatal("deleted definition remains loadable")
	}
	if _, err := LoadModel(snapshot.Fingerprint.Value); err != nil {
		t.Fatalf("DeleteDefinition() removed shared model: %v", err)
	}
}

func useTestConfigDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	SetConfigDir(directory)
	t.Cleanup(func() { SetConfigDir("") })
	return directory
}
