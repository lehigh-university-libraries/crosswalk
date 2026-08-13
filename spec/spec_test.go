package spec

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestLoadRejectsOversizedSpecification(t *testing.T) {
	_, err := Load(io.LimitReader(strings.NewReader(strings.Repeat("x", int(maxTransformationBytes)+1)), maxTransformationBytes+1))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestFabricatorWorkbenchIsVersionedDirectionalAndOrdered(t *testing.T) {
	transformation := FabricatorWorkbench()
	if err := transformation.Validate(); err != nil {
		t.Fatalf("validate built-in specification: %v", err)
	}
	if transformation.Version != CurrentVersion {
		t.Errorf("version = %q, want %q", transformation.Version, CurrentVersion)
	}
	if transformation.Source.Format != "csv" || transformation.Target.Format != "islandora-workbench" {
		t.Errorf("directions = %q -> %q", transformation.Source.Format, transformation.Target.Format)
	}
	if transformation.Source.HeaderRows != 2 {
		t.Errorf("source header rows = %d, want 2", transformation.Source.HeaderRows)
	}
	if got := transformation.Target.Fields[:4]; got[0].Name != "id" || got[1].Name != "parent_id" || got[2].Name != "field_weight" || got[3].Name != "node_id" {
		t.Errorf("target field order = %+v", got)
	}
	if transformation.Fingerprint.Algorithm != "sha256" || len(transformation.Fingerprint.Value) != 64 {
		t.Errorf("fingerprint = %+v", transformation.Fingerprint)
	}
	if got := transformation.Default(FileStagingRootDefault); got != "/mnt/islandora_staging" {
		t.Errorf("%s = %q", FileStagingRootDefault, got)
	}
	if got := transformation.Default(FileAllowedAbsoluteRootsDefault); got != "/home|/mnt" {
		t.Errorf("%s = %q", FileAllowedAbsoluteRootsDefault, got)
	}
	if got := transformation.Default(SupplementalMediaUseTIDDefault); got != "151326" {
		t.Errorf("%s = %q", SupplementalMediaUseTIDDefault, got)
	}
	resourceType, ok := transformation.SourceField("field_resource_type")
	if !ok || !resourceType.IsOptionalForObjectModel("Sub Collection") || !resourceType.IsOptionalForObjectModel("page") {
		t.Errorf("resource-type object-model exceptions = %#v", resourceType.OptionalForObjectModels)
	}
}

func TestLoadRejectsUnknownPolicyAndFingerprintDrift(t *testing.T) {
	_, err := Load(strings.NewReader(`
version: "1"
name: example
source:
  format: csv
  fields:
    - name: title
      hub: Title
      misspelled_required: true
target:
  format: islandora-workbench
  fields:
    - name: title
      hub: Title
`))
	if err == nil || !strings.Contains(err.Error(), "field misspelled_required not found") {
		t.Fatalf("unknown-field error = %v", err)
	}

	transformation := FabricatorWorkbench()
	data, err := json.Marshal(transformation)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	data = bytes.Replace(data, []byte(`"name":"fabricator-workbench"`), []byte(`"name":"changed"`), 1)
	_, err = Load(bytes.NewReader(data))
	if err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("fingerprint error = %v", err)
	}
}

func TestLoadRequiresSealedFingerprintAndRejectsDuplicateJSONMembers(t *testing.T) {
	unsigned := `{
		"version":"1","name":"unsigned",
		"source":{"format":"csv","fields":[{"name":"title","hub":"Title"}]},
		"target":{"format":"islandora-workbench","fields":[{"name":"title","hub":"Title"}]}
	}`
	if _, err := Load(strings.NewReader(unsigned)); err == nil || !strings.Contains(err.Error(), "fingerprint algorithm must be sha256") {
		t.Fatalf("Load(unsigned) error = %v", err)
	}
	if draft, err := DecodeDraft(strings.NewReader(unsigned)); err != nil || draft.Name != "unsigned" {
		t.Fatalf("DecodeDraft(unsigned) = %#v, %v", draft, err)
	}

	duplicate := `{
		"version":"1","name":"first","name":"second",
		"source":{"format":"csv","fields":[{"name":"title","hub":"Title"}]},
		"target":{"format":"islandora-workbench","fields":[{"name":"title","hub":"Title"}]}
	}`
	if _, err := Load(strings.NewReader(duplicate)); err == nil || !strings.Contains(err.Error(), `duplicate JSON member "name"`) {
		t.Fatalf("Load(duplicate) error = %v", err)
	}
	if _, err := DecodeDraft(strings.NewReader(duplicate)); err == nil || !strings.Contains(err.Error(), `duplicate JSON member "name"`) {
		t.Fatalf("DecodeDraft(duplicate) error = %v", err)
	}
}

func TestDecodeDraftAcceptsIntentionalEditsAndPreservesModelBinding(t *testing.T) {
	transformation := FabricatorWorkbench()
	transformation.Fingerprint.Model = strings.Repeat("a", 64)
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(transformation)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"/mnt/islandora_staging"`), []byte(`"/srv/islandora/staging"`), 1)

	draft, err := DecodeDraft(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeDraft() error = %v", err)
	}
	if draft.Fingerprint.Value != "" || draft.Fingerprint.Algorithm != "" {
		t.Fatalf("draft retained stale digest: %#v", draft.Fingerprint)
	}
	if draft.Fingerprint.Model != strings.Repeat("a", 64) {
		t.Fatalf("draft discarded model binding: %#v", draft.Fingerprint)
	}
	if got := draft.Default(FileStagingRootDefault); got != "/srv/islandora/staging" {
		t.Fatalf("edited staging root = %q", got)
	}
}

func TestFieldOperationPolicy(t *testing.T) {
	field := Field{
		Name:                    "title",
		RequiredFor:             []Operation{OperationCreate},
		OptionalForObjectModels: []string{"Sub-Collection"},
		Operations:              []Operation{OperationCreate, OperationUpdate},
	}
	if !field.IsRequired(OperationCreate) || field.IsRequired(OperationUpdate) {
		t.Errorf("required policy is incorrect")
	}
	if !field.AppliesTo(OperationCreate) || !field.AppliesTo(OperationUpdate) || field.AppliesTo(OperationAddMedia) {
		t.Errorf("operation policy is incorrect")
	}
	if field.IsRequiredFor(OperationCreate, "sub_collection") || !field.IsRequiredFor(OperationCreate, "Digital Document") {
		t.Errorf("conditional required policy is incorrect")
	}
}

func TestLoadValidatesRequiredGroups(t *testing.T) {
	transformation, err := DecodeDraft(strings.NewReader(`
version: "1"
name: aggregate-policy
source:
  format: csv
  fields:
    - name: identifier_doi
      hub: Identifiers.doi
      operations: [create, update]
    - name: identifier_uri
      hub: Identifiers.url
      operations: [create, update]
  required_groups:
    - name: identifiers
      fields: [identifier_doi, identifier_uri]
      required_for: [create]
target:
  format: islandora-workbench
  fields:
    - name: field_identifier
      hub: Identifiers
`))
	if err != nil {
		t.Fatalf("Load() required group error = %v", err)
	}
	group := transformation.Source.RequiredGroups[0]
	if !group.IsRequired(OperationCreate) || group.IsRequired(OperationUpdate) {
		t.Fatalf("required group policy = %#v", group)
	}

	transformation.Source.RequiredGroups[0].Fields[1] = "missing"
	if err := transformation.Validate(); err == nil || !strings.Contains(err.Error(), "references unknown field") {
		t.Fatalf("Validate() unknown group member error = %v", err)
	}
}

func TestLoadRejectsUnsupportedWorkbenchTargetHubMapping(t *testing.T) {
	_, err := DecodeDraft(strings.NewReader(`
version: "1"
name: target-typo
source:
  format: csv
  fields:
    - name: title
      hub: Title
target:
  format: islandora-workbench
  fields:
    - name: title
      hub: Publsiher
`))
	if err == nil || !strings.Contains(err.Error(), `unsupported Islandora Workbench Hub mapping "Publsiher"`) {
		t.Fatalf("Load() target Hub typo error = %v", err)
	}
}
