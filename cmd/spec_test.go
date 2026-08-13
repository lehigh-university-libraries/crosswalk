package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workbenchformat "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	transformationspec "github.com/lehigh-university-libraries/crosswalk/spec"
	"gopkg.in/yaml.v3"
)

func TestSpecCompileDrupalCommandWritesLoadableJSON(t *testing.T) {
	command := newSpecCompileDrupalCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{
		"--config", filepath.Join("..", "spec", "testdata", "drupal"),
		"--bundle", "islandora_object",
		"--format", "json",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	compiled, err := transformationspec.Load(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("spec.Load(command output) error = %v", err)
	}
	if compiled.Fingerprint.Bundle != "islandora_object" || len(compiled.Fingerprint.Model) != 64 {
		t.Fatalf("compiled fingerprint = %#v", compiled.Fingerprint)
	}
}

func TestSpecCompileDrupalCommandWritesLoadableYAML(t *testing.T) {
	command := newSpecCompileDrupalCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{
		"--config", filepath.Join("..", "spec", "testdata", "drupal"),
		"--bundle", "islandora_object",
		"--format", "yaml",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	compiled, err := transformationspec.Load(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("spec.Load(command YAML output) error = %v", err)
	}
	if compiled.Fingerprint.Bundle != "islandora_object" || len(compiled.Fingerprint.Model) != 64 {
		t.Fatalf("compiled fingerprint = %#v", compiled.Fingerprint)
	}
}

func TestSpecCompileDrupalCommandBindsPublishedProfile(t *testing.T) {
	useProfileCommandConfig(t)
	_, definition := publishCommandProfile(t, "workbench-profile")
	command := newSpecCompileDrupalCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--profile", definition.Name, "--format", "json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	compiled, err := transformationspec.Load(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("spec.Load(command output) error = %v", err)
	}
	if compiled.Fingerprint.Profile != definition.Fingerprint.Value || compiled.Fingerprint.Model != definition.ModelFingerprint {
		t.Fatalf("profile-bound fingerprint = %#v", compiled.Fingerprint)
	}
	if _, ok := compiled.TargetField("field_custom_tags"); !ok {
		t.Fatal("profile-bound specification omitted an encodable profile field")
	}
}

func TestSpecCompileDrupalCommandRejectsProfileAndConfig(t *testing.T) {
	command := newSpecCompileDrupalCmd()
	command.SetArgs([]string{"--profile", "example", "--config", "config", "--bundle", "article"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestSpecValidateResealsEditedDraft(t *testing.T) {
	transformation := transformationspec.FabricatorWorkbench()
	oldFingerprint := transformation.Fingerprint.Value
	transformation.Defaults[transformationspec.FileStagingRootDefault] = "/srv/islandora/staging"
	draft, err := yaml.Marshal(transformation)
	if err != nil {
		t.Fatal(err)
	}

	command := newSpecValidateCmd()
	command.SetIn(bytes.NewReader(draft))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--format", "json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("spec validate Execute() error = %v", err)
	}
	sealed, err := transformationspec.Load(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("validated output is not sealed: %v", err)
	}
	if sealed.Fingerprint.Value == oldFingerprint || sealed.Default(transformationspec.FileStagingRootDefault) != "/srv/islandora/staging" {
		t.Fatalf("sealed edited specification = %#v", sealed)
	}
}

func TestSpecValidateRejectsUnknownDraftFields(t *testing.T) {
	command := newSpecValidateCmd()
	command.SetIn(strings.NewReader(`
version: "1"
name: bad
source:
  format: csv
  fields: [{name: title, hub: Title}]
target:
  format: islandora-workbench
  fields: [{name: title, hub: Title}]
defualts: {}
`))
	command.SetArgs(nil)
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "field defualts not found") {
		t.Fatalf("spec validate unknown-field error = %v", err)
	}
}

func TestSpecContractWritesProfileBoundTrustAnchor(t *testing.T) {
	useProfileCommandConfig(t)
	_, definition := publishCommandProfile(t, "contract-profile")
	loaded, err := loadDrupalReconciliationProfile(definition.Name, false)
	if err != nil {
		t.Fatal(err)
	}
	transformation := loaded.transformation
	transformation.Defaults = map[string]string{
		transformationspec.FileStagingRootDefault:          "/srv/workbench",
		transformationspec.FileAllowedAbsoluteRootsDefault: "/srv/workbench",
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(transformation)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "reviewed.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	command := newSpecContractCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--spec", path, "--profile", definition.Name})
	if err := command.Execute(); err != nil {
		t.Fatalf("spec contract Execute() error = %v", err)
	}
	var contract workbenchformat.ArtifactContract
	if err := json.Unmarshal(output.Bytes(), &contract); err != nil {
		t.Fatal(err)
	}
	if contract.ProfileFingerprint != loaded.compiled.Fingerprint() || contract.ModelFingerprint != loaded.compiled.ModelFingerprint() {
		t.Fatalf("contract provenance = %#v", contract)
	}
	if contract.Spec.Fingerprint != transformation.Fingerprint.Value || contract.Policy == nil || contract.Policy.StagingRoot != "/srv/workbench" {
		t.Fatalf("contract = %#v", contract)
	}
}

func TestConvertWithCompiledSpecPreservesCustomField(t *testing.T) {
	transformation, err := transformationspec.CompileDrupalDirectory(filepath.Join("..", "spec", "testdata", "drupal"), transformationspec.DrupalCompileOptions{Bundle: "islandora_object"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(transformation)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(t.TempDir(), "compiled.json")
	if err := os.WriteFile(specPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	previousSpec := transformationSpecFile
	previousInput := inputFile
	previousOutput := outputFile
	previousSourceProfile := sourceProfileName
	previousTargetProfile := targetProfileName
	previousColumns := columns
	t.Cleanup(func() {
		transformationSpecFile = previousSpec
		inputFile = previousInput
		outputFile = previousOutput
		sourceProfileName = previousSourceProfile
		targetProfileName = previousTargetProfile
		columns = previousColumns
	})
	transformationSpecFile = specPath
	inputFile = ""
	outputFile = ""
	sourceProfileName = ""
	targetProfileName = ""
	columns = nil

	var output bytes.Buffer
	convertCmd.SetIn(strings.NewReader("Machine Name,title,field_full_title,field_identifier.attr0=doi,field_local_code\nHuman Name,Title,Complete Title,DOI,Local Code\n,Example,Example full,10.1234/example,ABC-123\n"))
	convertCmd.SetOut(&output)
	if err := runConvert(convertCmd, []string{"csv", "islandora-workbench"}); err != nil {
		t.Fatalf("runConvert() error = %v", err)
	}
	if !strings.Contains(output.String(), "field_local_code") || !strings.Contains(output.String(), "ABC-123") {
		t.Fatalf("spec-driven output omitted custom field: %s", output.String())
	}
}

func TestConvertWithProfileBoundSpecHonorsEditedTargetMapping(t *testing.T) {
	state := captureConvertState()
	t.Cleanup(state.restore)
	useProfileCommandConfig(t)
	snapshot, definition := publishCommandProfile(t, "converted-profile")
	for index := range definition.Mappings {
		mapping := &definition.Mappings[index]
		if mapping.Field.Path == "field_custom_tags" {
			mapping.Hub = "Subjects.keywords"
			mapping.Merge = profile.MergeAppend
		}
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Publish(snapshot, definition, profile.PublishOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	stored, err := profile.LoadStored(definition.Name)
	if err != nil {
		t.Fatal(err)
	}
	transformation, err := transformationspec.CompileDrupalProfile(stored.Model, stored.Compiled, transformationspec.DrupalCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(transformation)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(t.TempDir(), "profile-bound.json")
	if err := os.WriteFile(specPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	transformationSpecFile = specPath
	targetProfileName = definition.Name
	sourceProfileName = ""
	inputFile, outputFile = "", ""
	columns = nil
	convertCmd.SetIn(strings.NewReader("title,field_full_title,field_identifier.attr0=doi,field_custom_tags\nExample,Example,10.1234/example,alpha ; beta\n"))
	var output bytes.Buffer
	convertCmd.SetOut(&output)
	if err := runConvert(convertCmd, []string{"csv", "islandora-workbench"}); err != nil {
		t.Fatalf("runConvert() error = %v", err)
	}
	if !strings.Contains(output.String(), "field_custom_tags") || !strings.Contains(output.String(), "alpha|beta") {
		t.Fatalf("profile-bound conversion output = %s", output.String())
	}
}

func TestConvertProfileBoundSpecRequiresTargetProfile(t *testing.T) {
	state := captureConvertState()
	t.Cleanup(state.restore)
	useProfileCommandConfig(t)
	snapshot, definition := publishCommandProfile(t, "converted-profile-required")
	stored, err := profile.LoadStored(definition.Name)
	if err != nil {
		t.Fatal(err)
	}
	transformation, err := transformationspec.CompileDrupalProfile(snapshot, stored.Compiled, transformationspec.DrupalCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(transformation)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(t.TempDir(), "profile-bound.json")
	if err := os.WriteFile(specPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	transformationSpecFile = specPath
	targetProfileName = ""
	sourceProfileName = ""
	inputFile, outputFile = "", ""
	columns = nil
	convertCmd.SetIn(strings.NewReader("title\nExample\n"))
	err = runConvert(convertCmd, []string{"csv", "islandora-workbench"})
	if err == nil || !strings.Contains(err.Error(), "requires the exact --target-profile") {
		t.Fatalf("runConvert() error = %v", err)
	}
}

func TestLoadTransformationSpecRejectsFingerprintDrift(t *testing.T) {
	transformation := transformationspec.FabricatorWorkbench()
	transformation.Fingerprint.Value = strings.Repeat("0", 64)
	data, err := json.Marshal(transformation)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "drift.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = loadTransformationSpec(path, "csv", "islandora-workbench")
	if err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("loadTransformationSpec() error = %v", err)
	}
}
