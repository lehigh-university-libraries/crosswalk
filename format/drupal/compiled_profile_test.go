package drupal

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

func TestCompiledProfileControlsDrupalParseAndSerialize(t *testing.T) {
	compiled := compiledDrupalTestProfile(t)
	input := `{
		"title": [{"value":"Fallback title"}],
		"field_full_title": [{"value":"Profile title"}],
		"field_identifier": [
			{"attr0":"doi","value":"HTTPS://DOI.ORG/10.1234/EXAMPLE"},
			{"attr0":"local","value":"unclassified-local-value"}
		],
		"uuid": [{"value":"a65e212c-c4b9-4f8c-b460-50706f17f699"}],
		"field_not_mapped": [{"value":"preserved"}]
	}`
	records, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{
		SystemProfile: compiled, StripHTML: true,
		SourceName: "https://user:password@Repository.Example.Test/node/42?page=2&access_token=secret#view",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 || records[0].GetTitle() != "Profile title" {
		t.Fatalf("records = %#v", records)
	}
	record := records[0]
	if len(record.GetIdentifiers()) < 1 {
		t.Fatal("profile identifier was not parsed")
	}
	doi := record.GetIdentifiers()[0]
	if doi.GetScheme() != "doi" || doi.GetNamespaceUri() != "https://doi.org/" || doi.GetValue() != "10.1234/example" || doi.GetIdentityLevel() != hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK {
		t.Fatalf("DOI = %#v", doi)
	}
	if record.GetSourceInfo().GetProfile() != compiled.Name() || record.GetSourceInfo().GetSourceUri() != "https://repository.example.test/node/42?page=2" ||
		record.GetSourceInfo().GetProfileFingerprint() != compiled.Fingerprint() || record.GetSourceInfo().GetModelFingerprint() != compiled.ModelFingerprint() {
		t.Fatalf("source info = %#v", record.GetSourceInfo())
	}
	if _, exists := hub.GetExtra(record, "drupal_unmapped"); !exists {
		t.Fatal("unmapped and partially mapped Drupal values were not preserved")
	}

	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{SystemProfile: compiled}); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &entity); err != nil {
		t.Fatal(err)
	}
	var identifiers []map[string]any
	if err := json.Unmarshal(entity["field_identifier"], &identifiers); err != nil {
		t.Fatal(err)
	}
	if len(identifiers) != 1 || identifiers[0]["attr0"] != "doi" || identifiers[0]["value"] != "10.1234/example" {
		t.Fatalf("serialized identifiers = %#v", identifiers)
	}
	if got, _ := ExtractString(entity["field_full_title"]); got != "Profile title" {
		t.Fatalf("serialized title = %q", got)
	}
}

func TestCompiledProfileDoesNotTreatInputFilenameAsSourceURI(t *testing.T) {
	t.Parallel()

	compiled := compiledDrupalTestProfile(t)
	records, err := (&Format{}).Parse(strings.NewReader(`{"field_full_title":[{"value":"Profile title"}]}`), &format.ParseOptions{
		SystemProfile: compiled,
		SourceName:    "metadata.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := records[0].GetSourceInfo().GetSourceUri(); got != "" {
		t.Errorf("source URI = %q, want empty for a local input label", got)
	}
}

func TestCompiledProfilePreservesRepeatedTextRights(t *testing.T) {
	field := model.Field{Path: "field_rights", SourceType: "text_long", Kind: model.ValueText, Cardinality: -1}
	selector := profile.FieldSelector{EntityType: "node", Bundle: "article", Path: field.Path}
	compiled := compileDrupalEncodingProfile(t, []model.Field{field}, []profile.Mapping{{
		Field: selector, Hub: "Rights", Decode: "text", Encode: "text", Merge: profile.MergeAppend,
	}}, nil)

	records, err := (&Format{}).Parse(strings.NewReader(`{
		"field_rights": [
			{"value": " First statement "},
			{"value": "Second statement"}
		]
	}`), &format.ParseOptions{SystemProfile: compiled, StripHTML: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"First statement", "Second statement"}
	got := make([]string, 0, len(records[0].GetRights()))
	for _, rights := range records[0].GetRights() {
		got = append(got, rights.GetStatement())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rights = %#v, want %#v", got, want)
	}

	entity := serializeCompiledEntity(t, records[0], compiled)
	if got := drupalFieldTexts(t, entity, "field_rights"); !slices.Equal(got, want) {
		t.Fatalf("serialized field_rights = %#v, want %#v", got, want)
	}
}

func TestCompiledProfileParsesAndSerializesScalarIdentifier(t *testing.T) {
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{EntityType: "node", Bundle: "article", Fields: []model.Field{
			{Path: "field_doi", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
		}}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	selector := profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "field_doi"}
	definition := &profile.Definition{
		Version: profile.CurrentDefinitionVersion, Name: "scalar-doi", System: "drupal", ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: []profile.Mapping{
			{Field: selector, Hub: "Identifiers", Decode: "typed-identifier", Encode: "typed-identifier", Merge: profile.MergeAppend},
			{Field: profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "title"}, Hub: "Title", Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty},
		},
		Identity: &profile.IdentityPolicy{Version: profile.CurrentIdentityVersion, Repository: profile.EntitySelector{EntityType: "node", Bundle: "article"}, Identifiers: []profile.IdentifierRule{{
			Name: "doi", Scheme: "doi", IdentityLevel: profile.IdentityWork, Value: selector, Pattern: `^10\.[0-9]{4,9}/\S+$`, Canonicalizer: "doi", Strength: profile.IdentifierStrong, Scope: profile.ScopeGlobal,
			Lookup: profile.LookupRule{Fields: []profile.FieldSelector{selector}, Operator: profile.LookupExact, Variants: []string{"canonical"}},
		}}},
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	records, err := (&Format{}).Parse(strings.NewReader(`{"title":[{"value":"Scalar identifier"}],"field_doi":[{"value":"https://doi.org/10.1234/SCALAR"}]}`), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatal(err)
	}
	if got := records[0].GetIdentifiers()[0].GetValue(); got != "10.1234/scalar" {
		t.Fatalf("parsed DOI = %q", got)
	}
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, &format.SerializeOptions{SystemProfile: compiled}); err != nil {
		t.Fatal(err)
	}
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &entity); err != nil {
		t.Fatal(err)
	}
	if got, _ := ExtractString(entity["field_doi"]); got != "10.1234/scalar" {
		t.Fatalf("serialized DOI = %q, output %s", got, output.Bytes())
	}
}

func TestStarterProfileParsesPublicationCompositeSelectorsExactly(t *testing.T) {
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "article",
			Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "field_related_item", SourceType: "related_item", Kind: model.ValueComposite, Cardinality: -1},
				{Path: "field_part_detail", SourceType: "part_detail", Kind: model.ValueComposite, Cardinality: -1},
			},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "publication", EntityType: "node", Bundle: "article",
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	input := `{
		"title":[{"value":"Article"}],
		"field_related_item":[
			{"title":"Journal of Examples"},
			{"identifier_type":"l-issn","identifier":"2049-3622"},
			{"identifier_type":"issn","identifier":"2049-3630"}
		],
		"field_part_detail":[
			{"type":"volume","number":"12"},
			{"type":"issue","number":"3"},
			{"type":"page","number":"44-50"}
		]
	}`
	records, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatal(err)
	}
	publication := records[0].GetPublication()
	if publication.GetTitle() != "Journal of Examples" || publication.GetLIssn() != "2049-3622" || publication.GetIssn() != "2049-3630" ||
		publication.GetVolume() != "12" || publication.GetIssue() != "3" || publication.GetPages() != "44-50" {
		t.Fatalf("publication = %#v", publication)
	}
}

func compiledDrupalTestProfile(t *testing.T) *profile.Compiled {
	t.Helper()
	return compiledDrupalTestProfileWithCodec(t, "text")
}

func compiledDrupalTestProfileWithCodec(t *testing.T, titleCodec string) *profile.Compiled {
	t.Helper()
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "article",
			Fields: []model.Field{
				{Path: "field_identifier", SourceType: "textfield_attr", Kind: model.ValueComposite, Cardinality: -1},
				{Path: "field_full_title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	identifier := profile.FieldSelector{
		EntityType: "node", Bundle: "article", Path: "field_identifier", Attribute: "value",
		Where: &profile.FieldPredicate{Attribute: "attr0", Equals: "doi"},
	}
	definition := &profile.Definition{
		Version: profile.CurrentDefinitionVersion, Name: "repository-article", System: "drupal",
		ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: []profile.Mapping{
			{Field: identifier, Hub: "Identifiers", Decode: "typed-identifier", Encode: "typed-identifier", Merge: profile.MergeAppend},
			{Field: profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "field_full_title"}, Hub: "Title", Decode: titleCodec, Encode: titleCodec, Merge: profile.MergeFirstNonempty},
			{Field: profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "title"}, Hub: "Title", Decode: "text", Encode: "none", Merge: profile.MergeFirstNonempty},
		},
		Identity: &profile.IdentityPolicy{
			Version:    profile.CurrentIdentityVersion,
			Repository: profile.EntitySelector{EntityType: "node", Bundle: "article"},
			Identifiers: []profile.IdentifierRule{{
				Name: "doi", Scheme: "doi", IdentityLevel: profile.IdentityWork, Value: identifier,
				Pattern: `^10\.[0-9]{4,9}/\S+$`, Canonicalizer: "doi",
				Strength: profile.IdentifierStrong, Scope: profile.ScopeGlobal,
				Lookup: profile.LookupRule{Fields: []profile.FieldSelector{identifier}, Operator: profile.LookupContains, Variants: []string{"canonical", "doi-url"}},
			}},
		},
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
