package omeka_s

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

func TestParseDatasetCustomSnapshot(t *testing.T) {
	t.Parallel()

	dataset := parseFixture(t, "custom_snapshot.json")
	if got, want := len(dataset.Records), 3; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}
	wantKeys := []string{"item_set-3", "item-20", "media-30"}
	for index, want := range wantKeys {
		if got := dataset.Records[index].Key; got != want {
			t.Errorf("record %d key = %q, want %q", index, got, want)
		}
	}
	if got, want := dataset.Provenance.SourceURI, "https://example.org/omeka/api/"; got != want {
		t.Errorf("source URI = %q, want %q", got, want)
	}
	if dataset.Provenance.RetrievedAt == nil || dataset.Provenance.RetrievedAt.Format("2006-01-02") != "2026-08-13" {
		t.Errorf("retrieved_at = %v, want 2026-08-13", dataset.Provenance.RetrievedAt)
	}

	item := dataset.Records[1].Record
	if got, want := item.GetTitle(), "Campus in Winter"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got, want := item.GetResourceType().GetOriginal(), "local:Photograph"; got != want {
		t.Errorf("resource type original = %q, want %q", got, want)
	}
	if got := item.GetSourceInfo().GetSourceUri(); got != "https://example.org/omeka/api/items/20" {
		t.Errorf("source URI = %q", got)
	}
	if got := item.GetSourceInfo().GetOrigin(); got != "https://example.org/omeka/api/" {
		t.Errorf("origin = %q", got)
	}
	if got, want := len(item.GetContributors()), 1; got != want {
		t.Fatalf("contributors = %d, want %d", got, want)
	}
	if got := item.GetContributors()[0].GetName(); got != "Doe, Jane" {
		t.Errorf("contributor name = %q", got)
	}
	if got := item.GetContributors()[0].GetSourceId(); got != "90" {
		t.Errorf("contributor source ID = %q", got)
	}
	if !hasIdentifier(item, "doi", "10.1234/example") {
		t.Errorf("identifiers do not include canonical DOI: %+v", item.GetIdentifiers())
	}
	if !hasIdentifier(item, "omeka-s-resource", "20") {
		t.Errorf("identifiers do not include source identifier: %+v", item.GetIdentifiers())
	}
	if got := identifierNamespace(item, "omeka-s-resource", "20"); got != "https://example.org/omeka/api/items/" {
		t.Errorf("source identifier namespace = %q", got)
	}
	if sourceIdentifier := findIdentifier(item, "omeka-s-resource", "20"); sourceIdentifier == nil || hub.DefaultIdentifierRegistry().ExactIdentity(sourceIdentifier) {
		t.Errorf("default Omeka source identifier must remain non-exact: %#v", sourceIdentifier)
	}
	if got, want := len(item.GetSubjects()), 1; got != want {
		t.Fatalf("subjects = %d, want %d", got, want)
	}
	if got := item.GetSubjects()[0].GetUri(); got != "https://id.loc.gov/authorities/subjects/sh85016677" {
		t.Errorf("subject URI = %q", got)
	}
	if !hasRelation(item, hubv1.RelationType_RELATION_TYPE_MEMBER_OF, "3") {
		t.Error("item-set membership relation was not mapped")
	}
	if !hasRelation(item, hubv1.RelationType_RELATION_TYPE_HAS_PART, "30") {
		t.Error("item/media relation was not mapped")
	}
	if got, want := len(item.GetFiles()), 1; got != want {
		t.Fatalf("item files = %d, want %d", got, want)
	}
	assertMediaFile(t, item.GetFiles()[0])

	media := dataset.Records[2].Record
	if got, want := len(media.GetFiles()), 1; got != want {
		t.Fatalf("media files = %d, want %d", got, want)
	}
	assertMediaFile(t, media.GetFiles()[0])
	if !hasRelation(media, hubv1.RelationType_RELATION_TYPE_PART_OF, "20") {
		t.Error("media/item relation was not mapped")
	}

	omeka := omekaExtra(t, item)
	if fingerprint, ok := omeka["model_fingerprint"].(string); !ok || len(fingerprint) != 64 {
		t.Errorf("model fingerprint = %#v, want SHA-256 hex", omeka["model_fingerprint"])
	}
	if _, exists := omeka["resource_template_definition"]; !exists {
		t.Error("resource template definition was not retained")
	}
	assertValueTypesAndCustomData(t, omeka)
	assertMetadataPreserved(t, omeka)
}

func TestParseDatasetSortsByKindAndID(t *testing.T) {
	t.Parallel()

	dataset := parseFixture(t, "unordered_snapshot.json")
	want := []string{"item_set-2", "item-10", "item-20"}
	if len(dataset.Records) != len(want) {
		t.Fatalf("record count = %d, want %d", len(dataset.Records), len(want))
	}
	for index := range want {
		if got := dataset.Records[index].Key; got != want[index] {
			t.Errorf("record %d key = %q, want %q", index, got, want[index])
		}
	}
}

func TestParseDatasetAppliesCompiledOmekaProfile(t *testing.T) {
	t.Parallel()

	snapshot := readSnapshotFixture(t, "custom_snapshot.json")
	modelSnapshot, err := CompileModel(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewOmekaSDefinition(modelSnapshot, profile.OmekaSDefinitionOptions{Name: "example-omeka"})
	if err != nil {
		t.Fatal(err)
	}
	for index := range definition.Mappings {
		mapping := &definition.Mappings[index]
		switch mapping.Field.Path {
		case "dcterms:title":
			mapping.Hub = "Extra.profile_title"
			mapping.Decode = "text"
			mapping.Merge = profile.MergeAppend
		case "local:department":
			mapping.Hub = "Departments"
			mapping.Decode = "text"
			mapping.Merge = profile.MergeAppend
		}
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(modelSnapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := snapshot.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := (&Format{}).ParseDataset(bytes.NewReader(raw), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatalf("ParseDataset(): %v", err)
	}
	item := dataset.Records[1].Record
	if item.GetTitle() != "Fallback display title" {
		t.Fatalf("title = %q, want Omeka display title fallback", item.GetTitle())
	}
	if got := item.GetDepartments(); len(got) != 1 || got[0] != "Special Collections" {
		t.Errorf("departments = %#v", got)
	}
	if value, exists := hub.GetExtra(item, "profile_title"); !exists || value != "Campus in Winter" {
		t.Errorf("Extra.profile_title = %#v, exists %t", value, exists)
	}
	profiledOmeka := omekaExtra(t, item)
	if _, exists := profiledOmeka["resource_template_definition"]; exists {
		t.Error("profiled record duplicates its immutable resource-template definition in Extra")
	}
	values, _ := profiledOmeka["values"].([]any)
	for _, rawValue := range values {
		entry, _ := rawValue.(map[string]any)
		if _, exists := entry["property"]; exists {
			t.Error("profiled record duplicates an immutable property definition in Extra")
		}
	}
	if got, want := item.GetSourceInfo().GetProfile(), "example-omeka"; got != want {
		t.Errorf("source profile = %q, want %q", got, want)
	}
	if dataset.Provenance.ProfileFingerprint != compiled.Fingerprint() {
		t.Errorf("dataset profile fingerprint = %q", dataset.Provenance.ProfileFingerprint)
	}
	if dataset.Provenance.ModelFingerprint != compiled.ModelFingerprint() {
		t.Errorf("dataset model fingerprint = %q", dataset.Provenance.ModelFingerprint)
	}
	if item.GetSourceInfo().GetProfileFingerprint() != compiled.Fingerprint() || item.GetSourceInfo().GetModelFingerprint() != compiled.ModelFingerprint() {
		t.Errorf("record provenance fingerprints = %#v", item.GetSourceInfo())
	}
}

func TestParseDatasetProfileAcceptsBareResourcesAndRejectsEmbeddedMismatch(t *testing.T) {
	t.Parallel()

	snapshot := readSnapshotFixture(t, "custom_snapshot.json")
	modelSnapshot, err := CompileModel(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewOmekaSDefinition(modelSnapshot, profile.OmekaSDefinitionOptions{Name: "photographs", ResourceTemplateID: 200})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(modelSnapshot, definition)
	if err != nil {
		t.Fatal(err)
	}

	itemOnly := snapshot
	itemOnly.ItemSets = nil
	itemOnly.Media = nil
	itemOnly.Items = append([]json.RawMessage(nil), snapshot.Items...)
	var item map[string]any
	if err := json.Unmarshal(itemOnly.Items[0], &item); err != nil {
		t.Fatal(err)
	}
	item["o:resource_template"] = nil
	rawItem, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	itemOnly.Items[0] = rawItem
	raw, err := itemOnly.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Format{}).ParseDataset(bytes.NewReader(raw), &format.ParseOptions{SystemProfile: compiled}); err == nil || !strings.Contains(err.Error(), "profile requires resource template 200") {
		t.Fatalf("wrong-template parse error = %v", err)
	}

	bare, err := json.Marshal(snapshot.Items)
	if err != nil {
		t.Fatal(err)
	}
	bareDataset, err := (&Format{}).ParseDataset(bytes.NewReader(bare), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatalf("bare API profile parse: %v", err)
	}
	if len(bareDataset.Records) != 1 || bareDataset.Records[0].Record.GetTitle() == "" {
		t.Fatalf("bare API profile records = %#v", bareDataset.Records)
	}
	if bareDataset.Provenance.ProfileFingerprint != compiled.Fingerprint() || bareDataset.Provenance.ModelFingerprint != compiled.ModelFingerprint() {
		t.Errorf("bare API profile provenance = %#v", bareDataset.Provenance)
	}

	mismatched := snapshot
	mismatched.ResourceTemplates = append([]json.RawMessage(nil), snapshot.ResourceTemplates...)
	var template map[string]any
	if err := json.Unmarshal(mismatched.ResourceTemplates[0], &template); err != nil {
		t.Fatal(err)
	}
	template["o:label"] = "Changed profile contract"
	mismatched.ResourceTemplates[0], err = json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	mismatchedRaw, err := mismatched.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Format{}).ParseDataset(bytes.NewReader(mismatchedRaw), &format.ParseOptions{SystemProfile: compiled}); err == nil || !strings.Contains(err.Error(), "does not match profile model") {
		t.Fatalf("embedded model mismatch error = %v", err)
	}
}

func TestParseDatasetTemplateProfileMapsCompleteSnapshot(t *testing.T) {
	t.Parallel()

	snapshot := readSnapshotFixture(t, "custom_snapshot.json")
	modelSnapshot, err := CompileModel(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewOmekaSDefinition(modelSnapshot, profile.OmekaSDefinitionOptions{Name: "photographs", ResourceTemplateID: 200})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(modelSnapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := snapshot.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := (&Format{}).ParseDataset(bytes.NewReader(raw), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatalf("ParseDataset(): %v", err)
	}
	if got, want := len(dataset.Records), 3; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}

	itemSet := dataset.Records[0].Record
	item := dataset.Records[1].Record
	media := dataset.Records[2].Record
	if got := item.GetSourceInfo().GetProfileFingerprint(); got != compiled.Fingerprint() {
		t.Errorf("item profile fingerprint = %q, want %q", got, compiled.Fingerprint())
	}
	for name, support := range map[string]*hubv1.Record{"item set": itemSet, "media": media} {
		if got := support.GetSourceInfo().GetProfileFingerprint(); got != "" {
			t.Errorf("%s profile fingerprint = %q; support record did not execute the item-template profile", name, got)
		}
		if support.GetTitle() == "" {
			t.Errorf("%s lost its structural title", name)
		}
	}
	if got := len(media.GetFiles()); got != 1 {
		t.Errorf("supporting media files = %d, want 1", got)
	}
	if !hasRelation(media, hubv1.RelationType_RELATION_TYPE_PART_OF, "20") {
		t.Error("supporting media lost its structural item relation")
	}
	if dataset.Provenance.ProfileFingerprint != compiled.Fingerprint() {
		t.Errorf("dataset profile fingerprint = %q, want %q", dataset.Provenance.ProfileFingerprint, compiled.Fingerprint())
	}
}

func TestCompiledProfileKeepsOmekaEnvelopeFieldsWithoutMappings(t *testing.T) {
	t.Parallel()

	snapshot := readSnapshotFixture(t, "custom_snapshot.json")
	modelSnapshot, err := CompileModel(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewOmekaSDefinition(modelSnapshot, profile.OmekaSDefinitionOptions{Name: "envelope-fields", ResourceTemplateID: 200})
	if err != nil {
		t.Fatal(err)
	}
	filtered := make([]profile.Mapping, 0, len(definition.Mappings))
	for _, mapping := range definition.Mappings {
		switch mapping.Field.Path {
		case "o:title", "o:is_public":
			continue
		case "dcterms:title":
			mapping.Hub = "Extra.metadata_title"
			mapping.Merge = profile.MergeAppend
		}
		filtered = append(filtered, mapping)
	}
	definition.Mappings = filtered
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(modelSnapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := snapshot.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := (&Format{}).ParseDataset(bytes.NewReader(raw), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatalf("ParseDataset(): %v", err)
	}
	item := dataset.Records[1].Record
	if got, want := item.GetTitle(), "Fallback display title"; got != want {
		t.Errorf("structural o:title = %q, want %q", got, want)
	}
	if !item.GetIsPublic() {
		t.Error("structural o:is_public was erased when its editable mapping was omitted")
	}
	if value, exists := hub.GetExtra(item, "metadata_title"); !exists || value != "Campus in Winter" {
		t.Errorf("editable metadata title = %#v, exists %t", value, exists)
	}
}

func TestCompiledProfileExecutesModuleValueCodecs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dataTypes []string
		valueType string
		value     any
		codec     string
		assert    func(*testing.T, any)
	}{
		{
			name: "integer", dataTypes: []string{"numeric:integer"}, valueType: "numeric:integer", value: float64(7), codec: "integer",
			assert: func(t *testing.T, value any) {
				if value != float64(7) {
					t.Errorf("integer Extra value = %#v, want numeric 7", value)
				}
			},
		},
		{
			name: "boolean", dataTypes: []string{"boolean"}, valueType: "boolean", value: true, codec: "boolean",
			assert: func(t *testing.T, value any) {
				if value != true {
					t.Errorf("boolean Extra value = %#v, want true", value)
				}
			},
		},
		{
			name: "composite", dataTypes: []string{"numeric:integer", "uri"}, valueType: "numeric:integer", value: float64(7), codec: "composite",
			assert: assertTaggedModuleRating,
		},
		{
			name: "opaque", dataTypes: []string{"custom:rating"}, valueType: "custom:rating", value: float64(7), codec: "opaque",
			assert: assertTaggedModuleRating,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := readSnapshotFixture(t, "custom_snapshot.json")
			addRatingToTemplate(t, &snapshot, test.dataTypes)
			setRatingModuleValue(t, &snapshot, test.valueType, test.value)
			modelSnapshot, err := CompileModel(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			definition, err := profile.NewOmekaSDefinition(modelSnapshot, profile.OmekaSDefinitionOptions{Name: "rating-" + test.name, ResourceTemplateID: 200})
			if err != nil {
				t.Fatal(err)
			}
			var extraKey string
			for _, mapping := range definition.Mappings {
				if mapping.Field.Path != "local:rating" {
					continue
				}
				if mapping.Decode != test.codec {
					t.Fatalf("generated codec = %q, want %q", mapping.Decode, test.codec)
				}
				extraKey = strings.TrimPrefix(mapping.Hub, "Extra.")
			}
			if extraKey == "" {
				t.Fatal("generated profile has no local:rating Extra mapping")
			}
			compiled, err := profile.Compile(modelSnapshot, definition)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := snapshot.CanonicalJSON()
			if err != nil {
				t.Fatal(err)
			}
			dataset, err := (&Format{}).ParseDataset(bytes.NewReader(raw), &format.ParseOptions{SystemProfile: compiled})
			if err != nil {
				t.Fatalf("ParseDataset(): %v", err)
			}
			value, exists := hub.GetExtra(dataset.Records[1].Record, extraKey)
			if !exists {
				t.Fatalf("Extra.%s is missing", extraKey)
			}
			test.assert(t, value)
		})
	}
}

func TestModuleValueCodecsRejectAmbiguousOrLossyConversions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value valueObject
		codec string
		base  string
		want  string
	}{
		{
			name:  "integer outside exact JSON range",
			value: valueObject{typeName: "numeric:integer", raw: json.RawMessage(`{"type":"numeric:integer","property_id":11,"@value":9007199254740992}`)},
			codec: "integer", base: "Extra", want: "exceeds the exact JSON range",
		},
		{
			name:  "ambiguous boolean",
			value: valueObject{typeName: "boolean", raw: json.RawMessage(`{"type":"boolean","property_id":11,"@value":"yes"}`)},
			codec: "boolean", base: "Extra", want: "expected true, false, 1, or 0",
		},
		{
			name:  "opaque semantic target",
			value: valueObject{typeName: "custom:rating", raw: json.RawMessage(`{"type":"custom:rating","property_id":11,"@value":7}`)},
			codec: "opaque", base: "Title", want: "can only be mapped to Hub Extra",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := decodeOmekaProfileValues([]valueObject{test.value}, test.codec, test.base)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decode error = %v, want %q", err, test.want)
			}
		})
	}
}

func addRatingToTemplate(t *testing.T, snapshot *Snapshot, dataTypes []string) {
	t.Helper()
	var template map[string]any
	if err := json.Unmarshal(snapshot.ResourceTemplates[0], &template); err != nil {
		t.Fatal(err)
	}
	properties, ok := template["o:resource_template_property"].([]any)
	if !ok {
		t.Fatalf("template properties = %#v", template["o:resource_template_property"])
	}
	properties = append(properties, map[string]any{
		"o:property":        map[string]any{"@id": "https://example.org/omeka/api/properties/11", "o:id": float64(11)},
		"o:alternate_label": "Rating", "o:alternate_comment": nil,
		"o:data_type": dataTypes, "o:is_required": false, "o:is_private": false, "o:default_lang": nil,
	})
	template["o:resource_template_property"] = properties
	encoded, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ResourceTemplates = append([]json.RawMessage(nil), snapshot.ResourceTemplates...)
	snapshot.ResourceTemplates[0] = encoded
}

func setRatingModuleValue(t *testing.T, snapshot *Snapshot, valueType string, value any) {
	t.Helper()
	var item map[string]any
	if err := json.Unmarshal(snapshot.Items[0], &item); err != nil {
		t.Fatal(err)
	}
	values, ok := item["local:rating"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("local:rating values = %#v", item["local:rating"])
	}
	entry := values[0].(map[string]any)
	entry["type"] = valueType
	entry["@value"] = value
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Items = append([]json.RawMessage(nil), snapshot.Items...)
	snapshot.Items[0] = encoded
}

func assertTaggedModuleRating(t *testing.T, value any) {
	t.Helper()
	preserved, ok := value.(map[string]any)
	if !ok || preserved["kind"] != "object" {
		t.Fatalf("preserved module value = %#v, want tagged object", value)
	}
	rawValue := taggedObjectMember(t, preserved, "@value")
	if got, want := rawValue["number_value"], "7"; got != want {
		t.Errorf("preserved numeric lexical value = %#v, want %q", got, want)
	}
	unit := taggedObjectMember(t, preserved, "unit")
	if got, want := unit["string_value"], "stars"; got != want {
		t.Errorf("preserved extension field = %#v, want %q", got, want)
	}
}

func TestParseDatasetProfileUsesExplicitInstitutionalIdentifier(t *testing.T) {
	t.Parallel()

	snapshot := readSnapshotFixture(t, "custom_snapshot.json")
	modelSnapshot, err := CompileModel(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewOmekaSDefinition(modelSnapshot, profile.OmekaSDefinitionOptions{
		Name: "example-ids",
		InstitutionalIdentifier: &profile.OmekaSInstitutionalIdentifierOptions{
			FieldPath: "local:department", Scheme: "example-accession",
			NamespaceURI: "https://example.org/id/", Pattern: `^Special Collections$`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(modelSnapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := snapshot.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := (&Format{}).ParseDataset(bytes.NewReader(raw), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatalf("ParseDataset(): %v", err)
	}
	identifier := findIdentifier(dataset.Records[1].Record, "example-accession", "Special Collections")
	if identifier == nil || identifier.GetNamespaceUri() != "https://example.org/id/" || !compiled.IdentifierRegistry().ExactIdentity(identifier) {
		t.Errorf("institutional identifier = %#v", identifier)
	}
}

func TestProfilePreservesUnmatchedLocalIdentifierAsNonExact(t *testing.T) {
	t.Parallel()

	snapshot := readSnapshotFixture(t, "custom_snapshot.json")
	modelSnapshot, err := CompileModel(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewOmekaSDefinition(modelSnapshot, profile.OmekaSDefinitionOptions{Name: "example-ids"})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(modelSnapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	source := &resource{uri: "https://example.org/omeka/api/items/20"}
	record := &hubv1.Record{}
	selector := profile.FieldSelector{EntityType: ModelResourceEntity, Path: "dcterms:identifier"}
	added, err := appendProfileIdentifiers(record, source, []valueObject{{typeName: "literal", literal: "LOCAL-123"}}, selector, compiled)
	if err != nil {
		t.Fatalf("appendProfileIdentifiers(): %v", err)
	}
	identifier := findIdentifier(record, "local", "LOCAL-123")
	if !added || identifier == nil {
		t.Fatalf("local identifier = %#v, added %t", identifier, added)
	}
	if identifier.GetNamespaceUri() != "https://example.org/omeka/api/" || identifier.GetIdentityLevel() != hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD {
		t.Errorf("local identifier scope = %#v", identifier)
	}
	if compiled.IdentifierRegistry().ExactIdentity(identifier) {
		t.Errorf("unmatched local identifier became exact identity evidence: %#v", identifier)
	}
	if _, err := appendProfileIdentifiers(&hubv1.Record{}, source, []valueObject{{typeName: "literal", literal: "doi:not-a-doi"}}, selector, compiled); err == nil || !strings.Contains(err.Error(), `claims profile rule "doi"`) {
		t.Fatalf("malformed claimed DOI error = %v", err)
	}
}

func TestProfileRejectsInvalidIdentifierInDedicatedInstitutionalField(t *testing.T) {
	t.Parallel()

	snapshot := readSnapshotFixture(t, "custom_snapshot.json")
	modelSnapshot, err := CompileModel(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewOmekaSDefinition(modelSnapshot, profile.OmekaSDefinitionOptions{
		Name: "example-ids",
		InstitutionalIdentifier: &profile.OmekaSInstitutionalIdentifierOptions{
			FieldPath: "local:department", Scheme: "example-accession",
			NamespaceURI: "https://example.org/id/", Pattern: `^Special Collections$`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(modelSnapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	source := &resource{uri: "https://example.org/omeka/api/items/20"}
	selector := profile.FieldSelector{EntityType: ModelResourceEntity, Path: "local:department"}
	_, err = appendProfileIdentifiers(&hubv1.Record{}, source, []valueObject{{typeName: "literal", literal: "Another Department"}}, selector, compiled)
	if err == nil || !strings.Contains(err.Error(), `claims profile rule "example-accession"`) {
		t.Fatalf("invalid institutional identifier error = %v", err)
	}
}

func TestProfileRightsQualifiersPreserveMeaning(t *testing.T) {
	t.Parallel()

	record := &hubv1.Record{}
	appendProfileRights(record, "License", []valueObject{{typeName: "uri", uri: "https://creativecommons.org/licenses/by/4.0/", label: "CC BY 4.0"}})
	appendProfileRights(record, "Holder", []valueObject{{typeName: "literal", literal: "Example University"}})
	if len(record.Rights) != 2 {
		t.Fatalf("rights = %#v", record.Rights)
	}
	if got := record.Rights[0]; got.GetLicense() != "CC BY 4.0" || got.GetUri() != "https://creativecommons.org/licenses/by/4.0/" {
		t.Errorf("license = %#v", got)
	}
	if got := record.Rights[1]; got.GetHolder() != "Example University" || got.GetStatement() != "" {
		t.Errorf("holder = %#v", got)
	}
}

func TestProfileMarksMappedEmptyPropertiesAsMapped(t *testing.T) {
	t.Parallel()

	source := &resource{properties: []termValues{{term: "dcterms:title", values: []valueObject{}}}}
	if _, exists := schemaProperty(source, "dcterms:title"); !exists {
		t.Error("empty but present property was not recognized")
	}
}

func TestSnapshotCanonicalJSONIsOrderIndependent(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "custom_snapshot.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	first, err := snapshot.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON(): %v", err)
	}
	reverseRawMessages(snapshot.Vocabularies)
	reverseRawMessages(snapshot.Properties)
	reverseRawMessages(snapshot.ResourceClasses)
	reverseRawMessages(snapshot.ResourceTemplates)
	reverseRawMessages(snapshot.ItemSets)
	reverseRawMessages(snapshot.Items)
	reverseRawMessages(snapshot.Media)
	second, err := snapshot.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() after reordering: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("canonical snapshot changed when collection order changed")
	}
	if fresh := NewSnapshot(); fresh.CrosswalkFormat != SnapshotFormat || fresh.Version != SnapshotVersion {
		t.Errorf("NewSnapshot() = %+v", fresh)
	}
}

func TestBareAPIResponseDerivesInstallationProvenance(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "custom_snapshot.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	page, err := json.Marshal(snapshot.Items)
	if err != nil {
		t.Fatalf("encode API page: %v", err)
	}
	dataset, err := (&Format{}).ParseDataset(bytes.NewReader(page), nil)
	if err != nil {
		t.Fatalf("ParseDataset(): %v", err)
	}
	if got, want := dataset.Provenance.SourceURI, "https://example.org/omeka/api/"; got != want {
		t.Errorf("source URI = %q, want %q", got, want)
	}
}

func TestParseFailsClosedOnMalformedKnownValue(t *testing.T) {
	t.Parallel()

	file := openFixture(t, "malformed_value.json")
	defer closeFixture(t, file)
	_, err := (&Format{}).Parse(file, nil)
	if err == nil || !strings.Contains(err.Error(), "@value is required") {
		t.Fatalf("Parse() error = %v, want missing @value error", err)
	}
}

func TestParseRejectsDuplicateJSONKeys(t *testing.T) {
	t.Parallel()

	input := `{"@id":"https://example.org/api/items/1","@type":"o:Item","o:id":1,"o:id":2,"o:is_public":true}`
	_, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err == nil || !strings.Contains(err.Error(), `duplicate object key "o:id"`) {
		t.Fatalf("Parse() error = %v, want duplicate-key error", err)
	}
}

func TestParseSanitizesProvenanceURIs(t *testing.T) {
	t.Parallel()

	input := `{"crosswalk_format":"omeka-s-jsonld","version":1,"source_uri":"https://user:password@EXAMPLE.ORG/api/?page=2&key_credential=secret#snapshot","items":[{"@id":"https://user:password@EXAMPLE.ORG/api/items/1?page=2&access_token=secret#item","@type":"o:Item","o:id":1,"o:is_public":true,"o:title":"One","o:created":null,"o:modified":null,"o:resource_class":null,"o:resource_template":null,"o:item_set":[],"o:media":[]}]}`
	dataset, err := (&Format{}).ParseDataset(strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := dataset.Provenance.SourceURI, "https://example.org/api/"; got != want {
		t.Errorf("dataset source URI = %q, want %q", got, want)
	}
	if got, want := dataset.Records[0].Record.GetSourceInfo().GetSourceUri(), "https://example.org/api/items/1?page=2"; got != want {
		t.Errorf("record source URI = %q, want %q", got, want)
	}
	if _, err := (&Format{}).Parse(strings.NewReader(`{"@id":"javascript:alert(1)","@type":"o:Item","o:id":1,"o:is_public":true}`), nil); err == nil {
		t.Fatal("Parse() accepted an unsafe source URI scheme")
	}
}

func TestParseRejectsUnknownSnapshotFields(t *testing.T) {
	t.Parallel()

	input := `{"crosswalk_format":"omeka-s-jsonld","version":1,"unexpected":true,"items":[{"@id":"https://example.org/api/items/1","@type":"o:Item","o:id":1,"o:is_public":true,"o:title":"One","o:created":null,"o:modified":null,"o:resource_class":null,"o:resource_template":null,"o:item_set":[],"o:media":[]}]}`
	_, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err == nil || !strings.Contains(err.Error(), `unknown field "unexpected"`) {
		t.Fatalf("Parse() error = %v, want unknown snapshot field error", err)
	}
}

func TestParseRejectsMediaFilenameTraversal(t *testing.T) {
	t.Parallel()

	input := `{"@id":"https://example.org/api/media/1","@type":"o:Media","o:id":1,"o:is_public":true,"o:title":"Unsafe","o:created":null,"o:modified":null,"o:resource_class":null,"o:resource_template":null,"o:item":{"@id":"https://example.org/api/items/2","o:id":2},"o:filename":"../../etc/passwd"}`
	_, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err == nil || !strings.Contains(err.Error(), "filename must not contain path components") {
		t.Fatalf("Parse() error = %v, want filename traversal error", err)
	}
}

func TestCanParseOmekaOnly(t *testing.T) {
	t.Parallel()

	parser := &Format{}
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "snapshot", input: `{"crosswalk_format":"omeka-s-jsonld"}`, want: true},
		{name: "item", input: `{"@type":"o:Item","o:id":1}`, want: true},
		{name: "item type array", input: `{"@type":["o:Item","local:Photograph"],"o:id":1}`, want: true},
		{name: "template", input: `{"@type":"o:ResourceTemplate","o:id":1}`, want: true},
		{name: "schema org", input: `{"@type":"ScholarlyArticle","name":"Example"}`, want: false},
		{name: "empty", input: ``, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := parser.CanParse([]byte(test.input)); got != test.want {
				t.Errorf("CanParse() = %t, want %t", got, test.want)
			}
		})
	}
}

func parseFixture(t *testing.T, name string) *format.Dataset {
	t.Helper()
	file := openFixture(t, name)
	defer closeFixture(t, file)
	dataset, err := (&Format{}).ParseDataset(file, nil)
	if err != nil {
		t.Fatalf("ParseDataset(%s): %v", name, err)
	}
	return dataset
}

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	return file
}

func closeFixture(t *testing.T, file *os.File) {
	t.Helper()
	if err := file.Close(); err != nil {
		t.Errorf("close fixture: %v", err)
	}
}

func hasIdentifier(record *hubv1.Record, scheme, value string) bool {
	for _, identifier := range record.GetIdentifiers() {
		if identifier.GetScheme() == scheme && identifier.GetValue() == value {
			return true
		}
	}
	return false
}

func identifierNamespace(record *hubv1.Record, scheme, value string) string {
	identifier := findIdentifier(record, scheme, value)
	if identifier == nil {
		return ""
	}
	return identifier.GetNamespaceUri()
}

func findIdentifier(record *hubv1.Record, scheme, value string) *hubv1.Identifier {
	for _, identifier := range record.GetIdentifiers() {
		if identifier.GetScheme() == scheme && identifier.GetValue() == value {
			return identifier
		}
	}
	return nil
}

func hasRelation(record *hubv1.Record, relationType hubv1.RelationType, targetID string) bool {
	for _, relation := range record.GetRelations() {
		if relation.GetType() == relationType && relation.GetTargetId() == targetID {
			return true
		}
	}
	return false
}

func assertMediaFile(t *testing.T, file *hubv1.File) {
	t.Helper()
	if got, want := file.GetMimeType(), "image/tiff"; got != want {
		t.Errorf("MIME type = %q, want %q", got, want)
	}
	if got, want := file.GetAccessUrl(), "https://example.org/omeka/files/original/abc123.tif"; got != want {
		t.Errorf("access URL = %q, want %q", got, want)
	}
	if got, want := file.GetChecksumAlgorithm(), "sha-256"; got != want {
		t.Errorf("checksum algorithm = %q, want %q", got, want)
	}
	if got, want := file.GetSizeBytes(), int64(4096); got != want {
		t.Errorf("size = %d, want %d", got, want)
	}
}

func omekaExtra(t *testing.T, record *hubv1.Record) map[string]any {
	t.Helper()
	extra := record.GetExtra().AsMap()
	omeka, ok := extra["omeka_s"].(map[string]any)
	if !ok {
		t.Fatalf("omeka_s extra = %#v", extra["omeka_s"])
	}
	return omeka
}

func assertValueTypesAndCustomData(t *testing.T, omeka map[string]any) {
	t.Helper()
	values, ok := omeka["values"].([]any)
	if !ok {
		t.Fatalf("values extra = %#v", omeka["values"])
	}
	types := make(map[string]string)
	var rating map[string]any
	var department map[string]any
	for _, rawTerm := range values {
		term := rawTerm.(map[string]any)
		entries := term["values"].([]any)
		if len(entries) == 0 {
			continue
		}
		entry := entries[0].(map[string]any)
		types[term["term"].(string)] = entry["type"].(string)
		if term["term"] == "local:rating" {
			rating = entry
		}
		if term["term"] == "local:department" {
			department = entry
		}
	}
	for term, wantType := range map[string]string{
		"local:department": "literal",
		"dcterms:subject":  "uri",
		"dcterms:relation": "resource:itemset",
		"local:rating":     "numeric:integer",
	} {
		if got := types[term]; got != wantType {
			t.Errorf("%s type = %q, want %q", term, got, wantType)
		}
	}
	if rating == nil {
		t.Fatal("local:rating was not retained")
	}
	sourceValue := rating["source_value"].(map[string]any)
	rawValue := taggedObjectMember(t, sourceValue, "@value")
	if got, want := rawValue["number_value"], "7"; got != want {
		t.Errorf("retained numeric lexical value = %#v, want %q", got, want)
	}
	unit := taggedObjectMember(t, sourceValue, "unit")
	if got, want := unit["string_value"], "stars"; got != want {
		t.Errorf("retained extension field = %#v, want %q", got, want)
	}
	if department == nil {
		t.Fatal("local:department was not retained")
	}
	annotation := taggedObjectMember(t, department["source_value"].(map[string]any), "@annotation")
	if annotation["kind"] != "object" {
		t.Errorf("retained annotation kind = %#v, want object", annotation["kind"])
	}
}

func assertMetadataPreserved(t *testing.T, omeka map[string]any) {
	t.Helper()
	metadata := omeka["metadata"].([]any)
	for _, rawEntry := range metadata {
		entry := rawEntry.(map[string]any)
		if entry["name"] != "curation:state" {
			continue
		}
		value := entry["value"].(map[string]any)
		reviewed := taggedObjectMember(t, value, "reviewed")
		if got, want := reviewed["boolean_value"], true; got != want {
			t.Errorf("retained reviewed flag = %#v, want %t", got, want)
		}
		score := taggedObjectMember(t, value, "score")
		if got, want := score["number_value"], "1.50"; got != want {
			t.Errorf("retained score lexical value = %#v, want %q", got, want)
		}
		return
	}
	t.Error("curation:state metadata was not retained")
}

func taggedObjectMember(t *testing.T, object map[string]any, name string) map[string]any {
	t.Helper()
	if object["kind"] != "object" {
		t.Fatalf("tagged value kind = %#v, want object", object["kind"])
	}
	members, ok := object["members"].([]any)
	if !ok {
		t.Fatalf("tagged object members = %#v", object["members"])
	}
	for _, rawMember := range members {
		member := rawMember.(map[string]any)
		if member["name"] == name {
			return member["value"].(map[string]any)
		}
	}
	t.Fatalf("tagged object has no member %q", name)
	return nil
}

func reverseRawMessages(values []json.RawMessage) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
