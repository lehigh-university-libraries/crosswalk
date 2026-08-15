package omeka_s

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/model"
)

func TestCompileModelBuildsCanonicalTemplateEntities(t *testing.T) {
	t.Parallel()

	snapshot := readSnapshotFixture(t, "custom_snapshot.json")
	compiled, err := CompileModel(snapshot)
	if err != nil {
		t.Fatalf("CompileModel(): %v", err)
	}
	if got, want := compiled.System, ModelSystem; got != want {
		t.Errorf("system = %q, want %q", got, want)
	}
	if got, want := compiled.Provenance.SourceURI, "https://example.org/omeka/api/"; got != want {
		t.Errorf("source URI = %q, want %q", got, want)
	}
	if got, want := compiled.Provenance.ConfigHash, modelFingerprint(t, snapshot); got != want {
		t.Errorf("schema digest = %q, want %q", got, want)
	}
	if len(compiled.Fingerprint.Value) != 64 {
		t.Errorf("model fingerprint = %q, want SHA-256", compiled.Fingerprint.Value)
	}
	if err := compiled.Validate(); err != nil {
		t.Fatalf("compiled model validation: %v", err)
	}

	installation, exists := compiled.Entity(ModelResourceEntity, "")
	if !exists {
		t.Fatal("installation-wide resource entity is missing")
	}
	if _, exists := findModelField(installation.Fields, "local:rating"); !exists {
		t.Error("installation model omitted a captured vocabulary property")
	}
	template, exists := compiled.Entity(ModelResourceEntity, "200")
	if !exists {
		t.Fatal("resource template 200 entity is missing")
	}
	if len(template.SemanticTypes) != 1 || template.SemanticTypes[0] != "local:Photograph" {
		t.Errorf("template semantic types = %#v", template.SemanticTypes)
	}
	title, exists := findModelField(template.Fields, "dcterms:title")
	if !exists {
		t.Fatal("template title property is missing")
	}
	if title.Label != "Photograph title" || !title.Required || title.SourceType != "omeka-s:literal" || title.Kind != model.ValueText {
		t.Errorf("template title field = %#v", title)
	}
	department, exists := findModelField(template.Fields, "local:department")
	if !exists {
		t.Fatal("template local department property is missing")
	}
	if department.Label != "Unit" || department.Kind != model.ValueComposite || department.SourceType != "omeka-s:literal|uri" {
		t.Errorf("template department field = %#v", department)
	}
	if _, exists := findModelField(template.Fields, "local:rating"); exists {
		t.Error("template model included a property not assigned to the template")
	}
	if vocabulary, exists := compiled.Entity("vocabulary", "10"); !exists || len(vocabulary.SemanticTypes) != 1 || vocabulary.SemanticTypes[0] != "https://example.org/vocabulary/" {
		t.Errorf("local vocabulary entity = %#v, exists %t", vocabulary, exists)
	}
	if class, exists := compiled.Entity("resource-class", "100"); !exists || len(class.SemanticTypes) != 2 {
		t.Errorf("resource class entity = %#v, exists %t", class, exists)
	}
	media, exists := compiled.Entity("media", "")
	if !exists {
		t.Fatal("media resource-kind entity is missing")
	}
	if file, exists := findModelField(media.Fields, "o:original_url"); !exists || file.Kind != model.ValueLink || file.SourceType != "uri" {
		t.Errorf("media original URL field = %#v, exists %t", file, exists)
	}
	if item, exists := findModelField(media.Fields, "o:item"); !exists || item.Reference == nil || item.Reference.EntityType != "item" {
		t.Errorf("media item relation = %#v, exists %t", item, exists)
	}
}

func TestCompileModelJSONAcceptsCanonicalModelOnlySnapshot(t *testing.T) {
	t.Parallel()

	snapshot := readSnapshotFixture(t, "custom_snapshot.json")
	snapshot.ItemSets = nil
	snapshot.Items = nil
	snapshot.Media = nil
	raw, err := snapshot.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON(): %v", err)
	}
	compiled, err := CompileModelJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("CompileModelJSON(): %v", err)
	}
	if _, exists := compiled.Entity(ModelResourceEntity, "200"); !exists {
		t.Error("model-only snapshot omitted template entity")
	}
	if _, err := (&Format{}).ParseDataset(bytes.NewReader(raw), nil); err == nil {
		t.Error("model-only snapshot unexpectedly parsed as a record dataset")
	}
	decoded, err := DecodeSnapshot(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("DecodeSnapshot(): %v", err)
	}
	if len(decoded.ResourceTemplates) != 1 || len(decoded.Items) != 0 {
		t.Errorf("decoded model-only snapshot = %#v", decoded)
	}
}

func TestCompileModelJSONRejectsUnknownAndDuplicateFields(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		`{"crosswalk_format":"omeka-s-jsonld","version":1,"unknown":true}`,
		`{"crosswalk_format":"omeka-s-jsonld","version":1,"version":1}`,
	} {
		if _, err := CompileModelJSON(bytes.NewBufferString(input)); err == nil {
			t.Errorf("CompileModelJSON(%s) succeeded", input)
		}
	}
}

func TestCompileModelFingerprintTracksExecutableTemplateShape(t *testing.T) {
	t.Parallel()

	first := readSnapshotFixture(t, "custom_snapshot.json")
	second := readSnapshotFixture(t, "custom_snapshot.json")
	var template map[string]any
	if err := json.Unmarshal(second.ResourceTemplates[0], &template); err != nil {
		t.Fatal(err)
	}
	properties := template["o:resource_template_property"].([]any)
	properties[0].(map[string]any)["o:is_required"] = false
	changed, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	second.ResourceTemplates[0] = changed

	compiledFirst, err := CompileModel(first)
	if err != nil {
		t.Fatal(err)
	}
	compiledSecond, err := CompileModel(second)
	if err != nil {
		t.Fatal(err)
	}
	if compiledFirst.Fingerprint.Value == compiledSecond.Fingerprint.Value {
		t.Error("model fingerprint did not change when a template requirement changed")
	}
}

func TestCompileModelFingerprintIgnoresAcquisitionProvenance(t *testing.T) {
	t.Parallel()

	first := readSnapshotFixture(t, "custom_snapshot.json")
	second := readSnapshotFixture(t, "custom_snapshot.json")
	second.SourceID = "other-installation"
	second.SourceURI = "https://other.example.org/api/"
	firstModel, err := CompileModel(first)
	if err != nil {
		t.Fatal(err)
	}
	secondModel, err := CompileModel(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstModel.Fingerprint.Value != secondModel.Fingerprint.Value {
		t.Errorf("model fingerprint changed with acquisition provenance: %s != %s", firstModel.Fingerprint.Value, secondModel.Fingerprint.Value)
	}
}

func TestCompileModelRejectsMissingOrWrongSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		snapshot Snapshot
	}{
		{name: "missing schema", snapshot: NewSnapshot()},
		{name: "wrong format", snapshot: Snapshot{CrosswalkFormat: "other", Version: SnapshotVersion, Properties: []json.RawMessage{json.RawMessage(`{}`)}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := CompileModel(test.snapshot); err == nil {
				t.Fatal("CompileModel() succeeded, want an error")
			}
		})
	}
}

func TestCompileModelSanitizesAcquisitionProvenance(t *testing.T) {
	t.Parallel()

	snapshot := readSnapshotFixture(t, "custom_snapshot.json")
	snapshot.SourceURI = "https://user:password@EXAMPLE.ORG/api/?page=2&key_credential=secret#model"
	compiled, err := CompileModel(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := compiled.Provenance.SourceURI, "https://example.org/api/"; got != want {
		t.Errorf("source URI = %q, want %q", got, want)
	}
}

func readSnapshotFixture(t *testing.T, name string) Snapshot {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return snapshot
}

func findModelField(fields []model.Field, path string) (model.Field, bool) {
	for _, field := range fields {
		if field.Path == path {
			return field, true
		}
	}
	return model.Field{}, false
}

func modelFingerprint(t *testing.T, snapshot Snapshot) string {
	t.Helper()
	decoded, err := decodeSchemaModel(snapshot)
	if err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	return decoded.fingerprint
}
