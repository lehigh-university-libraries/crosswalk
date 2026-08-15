package model

import (
	"strings"
	"testing"
)

func TestFingerprintIsStableAcrossOrderingAndProvenance(t *testing.T) {
	first := &Snapshot{
		Version: CurrentVersion,
		System:  "drupal",
		Provenance: Provenance{
			SiteName: "First site",
		},
		Entities: []Entity{
			{
				EntityType: "node", Bundle: "page", SemanticTypes: []string{"schema:Thing", "pcdm:Object"},
				Fields: []Field{
					{Path: "field_z", SourceType: "string", Kind: ValueText, Cardinality: 1},
					{Path: "field_a", SourceType: "entity_reference", Kind: ValueReference, Cardinality: -1, SemanticProperties: []string{"b", "a"}, Reference: &Reference{EntityType: "node", Bundles: []string{"z", "a"}}},
				},
			},
			{EntityType: "media", Bundle: "document", Fields: []Field{{Path: "field_media_file", SourceType: "file", Kind: ValueFile, Cardinality: 1}}},
		},
	}
	if err := first.SealFingerprint(); err != nil {
		t.Fatalf("SealFingerprint() error = %v", err)
	}

	second := &Snapshot{
		Version: CurrentVersion,
		System:  "drupal",
		Provenance: Provenance{
			SiteName: "Renamed site",
		},
		Entities: []Entity{
			first.Entities[1],
			{
				EntityType: "node", Bundle: "page", SemanticTypes: []string{"pcdm:Object", "schema:Thing"},
				Fields: []Field{
					{Path: "field_a", SourceType: "entity_reference", Kind: ValueReference, Cardinality: -1, SemanticProperties: []string{"a", "b"}, Reference: &Reference{EntityType: "node", Bundles: []string{"a", "z"}}},
					{Path: "field_z", SourceType: "string", Kind: ValueText, Cardinality: 1},
				},
			},
		},
	}
	if err := second.SealFingerprint(); err != nil {
		t.Fatalf("second SealFingerprint() error = %v", err)
	}
	if first.Fingerprint.Value != second.Fingerprint.Value {
		t.Fatalf("fingerprints differ by ordering/provenance: %s != %s", first.Fingerprint.Value, second.Fingerprint.Value)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	canonical, err := first.Canonical()
	if err != nil {
		t.Fatalf("Canonical() error = %v", err)
	}
	if canonical.Entities[0].EntityType != "media" || canonical.Entities[1].Fields[0].Path != "field_a" {
		t.Fatalf("canonical order = %#v", canonical.Entities)
	}
	canonical.Entities[1].Fields[0].SemanticProperties[0] = "changed"
	if first.Entities[0].Fields[1].SemanticProperties[0] == "changed" {
		t.Fatal("Canonical() returned nested slices shared with the input")
	}
}

func TestLoadRejectsUnknownYAMLAndFingerprintDrift(t *testing.T) {
	_, err := Load(strings.NewReader(`
version: "1"
system: drupal
entities: []
fingerprint:
  algorithm: sha256
  value: deadbeef
misspelled: true
`))
	if err == nil || !strings.Contains(err.Error(), "field misspelled not found") {
		t.Fatalf("unknown-field error = %v", err)
	}

	snapshot := &Snapshot{
		Version: CurrentVersion,
		System:  "drupal",
		Entities: []Entity{{
			EntityType: "node", Bundle: "article",
			Fields: []Field{{Path: "title", SourceType: "core", Kind: ValueText, Cardinality: 1}},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	snapshot.Entities[0].Fields[0].Required = true
	if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("fingerprint drift error = %v", err)
	}
}

func TestLoadRejectsDuplicateJSONMembers(t *testing.T) {
	input := `{
		"version":"1","system":"drupal","system":"omeka-s",
		"entities":[],
		"fingerprint":{"algorithm":"sha256","value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}`
	if _, err := Load(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), `duplicate JSON member "system"`) {
		t.Fatalf("Load(duplicate) error = %v", err)
	}
}

func TestEntityReturnsDefensiveCopy(t *testing.T) {
	snapshot := &Snapshot{Entities: []Entity{{
		EntityType: "node", Bundle: "article",
		Fields: []Field{{Path: "field_identifier", SourceType: "string", Kind: ValueText, Cardinality: -1, StorageSettings: map[string]any{"max_length": 255, "allowed": []string{"doi", "local"}}}},
	}}}
	entity, ok := snapshot.Entity("node", "article")
	if !ok {
		t.Fatal("Entity() did not find node/article")
	}
	entity.Fields[0].StorageSettings["max_length"] = 10
	entity.Fields[0].StorageSettings["allowed"].([]string)[0] = "changed"
	if got := snapshot.Entities[0].Fields[0].StorageSettings["max_length"]; got != 255 {
		t.Fatalf("snapshot mutated through Entity() copy: %v", got)
	}
	if got := snapshot.Entities[0].Fields[0].StorageSettings["allowed"].([]string)[0]; got != "doi" {
		t.Fatalf("snapshot typed slice mutated through Entity() copy: %v", got)
	}
}

func TestModelDoesNotRequireDrupalBundleSemantics(t *testing.T) {
	snapshot := &Snapshot{
		Version: CurrentVersion,
		System:  "zenodo",
		Entities: []Entity{{
			EntityType: "record",
			Fields:     []Field{{Path: "metadata.title", SourceType: "string", Kind: ValueText, Cardinality: 1}},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("bundle-less model validation error = %v", err)
	}
	if _, exists := snapshot.Entity("record", ""); !exists {
		t.Fatal("bundle-less entity was not addressable")
	}
}

func TestValidateRejectsUnsafeProvenance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		provenance Provenance
	}{
		{name: "credentials", provenance: Provenance{SourceURI: "https://user:password@example.org/api/"}},
		{name: "query", provenance: Provenance{SourceURI: "https://example.org/api/?token=secret"}},
		{name: "fragment", provenance: Provenance{SourceURI: "https://example.org/api/#fragment"}},
		{name: "relative URI", provenance: Provenance{SourceURI: "/api/"}},
		{name: "control", provenance: Provenance{SourceID: "site\nother"}},
		{name: "surrounding whitespace", provenance: Provenance{SiteName: " Example "}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := &Snapshot{
				Version: CurrentVersion, System: "omeka-s", Provenance: test.provenance,
				Entities: []Entity{{EntityType: "resource", Fields: []Field{}}},
			}
			if err := snapshot.SealFingerprint(); err != nil {
				t.Fatal(err)
			}
			if err := snapshot.Validate(); err == nil {
				t.Fatal("Validate() succeeded, want unsafe provenance error")
			}
		})
	}
}

func TestValidateRejectsMalformedReferenceContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field Field
		want  string
	}{
		{
			name:  "reference kind without target",
			field: Field{Path: "field_parent", SourceType: "entity_reference", Kind: ValueReference, Cardinality: 1},
			want:  "requires a reference target",
		},
		{
			name:  "typed reference kind without target",
			field: Field{Path: "field_agent", SourceType: "typed_relation", Kind: ValueTypedReference, Cardinality: -1},
			want:  "requires a reference target",
		},
		{
			name: "reference without entity type",
			field: Field{
				Path: "field_parent", SourceType: "entity_reference", Kind: ValueReference, Cardinality: 1,
				Reference: &Reference{Bundles: []string{"collection"}},
			},
			want: "reference without entity_type",
		},
		{
			name: "target on non-reference kind",
			field: Field{
				Path: "title", SourceType: "string", Kind: ValueText, Cardinality: 1,
				Reference: &Reference{EntityType: "node"},
			},
			want: "reference target for non-reference kind",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := &Snapshot{
				Version: CurrentVersion,
				System:  "drupal",
				Entities: []Entity{{
					EntityType: "node", Bundle: "article", Fields: []Field{test.field},
				}},
			}
			if err := snapshot.SealFingerprint(); err != nil {
				t.Fatal(err)
			}
			if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}
