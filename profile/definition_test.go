package profile

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/model"
)

func TestDefinitionFingerprintIsStableAndOrderSensitive(t *testing.T) {
	snapshot := testModel(t)
	first := testDefinition(t, snapshot)
	second := cloneDefinition(first)
	second.Description = "documentation does not change executable policy"
	second.Name = "same-policy-another-name"
	second.Fingerprint = model.Fingerprint{}
	if err := second.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint.Value != second.Fingerprint.Value {
		t.Fatalf("fingerprint changed with descriptive metadata: %s != %s", first.Fingerprint.Value, second.Fingerprint.Value)
	}

	second.Mappings[0], second.Mappings[1] = second.Mappings[1], second.Mappings[0]
	second.Fingerprint = model.Fingerprint{}
	if err := second.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint.Value == second.Fingerprint.Value {
		t.Fatal("fingerprint did not retain semantic mapping order")
	}
}

func TestDecodeDefinitionRejectsUnknownYAML(t *testing.T) {
	_, err := DecodeDefinition(strings.NewReader(`
version: "1"
name: example
system: drupal
model_fingerprint: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
mappings: []
fingerprint:
  algorithm: sha256
  value: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
misspelled: true
`))
	if err == nil || !strings.Contains(err.Error(), "field misspelled not found") {
		t.Fatalf("unknown-field error = %v", err)
	}
}

func TestDefinitionDecodersRejectDuplicateJSONMembers(t *testing.T) {
	input := `{
		"version":"1","name":"first","name":"second","system":"drupal",
		"model_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"mappings":[],
		"fingerprint":{"algorithm":"sha256","value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	}`
	for name, decode := range map[string]func(io.Reader) (*Definition, error){
		"sealed": DecodeDefinition,
		"draft":  DecodeDraftDefinition,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decode(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), `duplicate JSON member "name"`) {
				t.Fatalf("decode error = %v", err)
			}
		})
	}
}

func TestCompilePreservesMappingAndLookupOrderAndIsImmutable(t *testing.T) {
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	compiled, err := Compile(snapshot, &definition)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	mappings := compiled.Mappings()
	if got := []string{mappings[0].Field.Selector.Path, mappings[1].Field.Selector.Path}; !reflect.DeepEqual(got, []string{"field_identifier", "title"}) {
		t.Fatalf("mapping order = %#v", got)
	}
	if mappings[0].Position != 0 || mappings[0].Field.Cardinality != -1 || mappings[0].Field.SourceType != "textfield_attr" {
		t.Fatalf("first resolved mapping = %#v", mappings[0])
	}
	lookup := compiled.LookupPlan()
	if !lookup.Enabled || lookup.Repository.Bundle != "article" || len(lookup.Identifiers) != 2 {
		t.Fatalf("lookup plan = %#v", lookup)
	}
	if lookup.Identifiers[0].Name != "doi" || lookup.Identifiers[1].Name != "local-accession" {
		t.Fatalf("identifier order = %#v", lookup.Identifiers)
	}
	if got := lookup.Identifiers[0].Lookup.Fields[0].Selector.Attribute; got != "value" {
		t.Fatalf("identifier lookup attribute = %q", got)
	}
	if lookup.Metadata == nil || lookup.Metadata.MetadataOnly != MetadataOnlyReview || lookup.Metadata.Date == nil || len(lookup.Metadata.Lookups) != 2 {
		t.Fatalf("metadata lookup plan = %#v", lookup.Metadata)
	}
	if lookup.Metadata.Lookups[0].Name != "title-author" || lookup.Metadata.Lookups[0].Contributors == nil || lookup.Metadata.Lookups[1].Name != "title-date" || lookup.Metadata.Lookups[1].Date == nil {
		t.Fatalf("metadata lookup order/conditions = %#v", lookup.Metadata.Lookups)
	}
	matched, err := compiled.MatchesIdentifier("doi", "10.1234/example")
	if err != nil || !matched {
		t.Fatalf("MatchesIdentifier() = %v, %v", matched, err)
	}
	localIdentifier, err := compiled.NewIdentifier("local-accession", "AB-42")
	if err != nil {
		t.Fatalf("NewIdentifier() error = %v", err)
	}
	if localIdentifier.GetScheme() != "lehigh-accession" || localIdentifier.GetNamespaceUri() != "https://preserve.lehigh.edu/identifiers/accession/" || localIdentifier.GetIdentityLevel().String() != "IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD" {
		t.Fatalf("NewIdentifier() = %#v", localIdentifier)
	}
	if !compiled.IdentifierRegistry().ExactIdentity(localIdentifier) {
		t.Fatal("profile strong institution identifier is not exact identity evidence")
	}
	undeclaredPMID, err := compiled.IdentifierRegistry().NewIdentifierForScheme(
		"12345678", "pmid", 0,
	)
	if err != nil {
		t.Fatalf("canonicalizing undeclared built-in identifier: %v", err)
	}
	if compiled.IdentifierRegistry().ExactIdentity(undeclaredPMID) {
		t.Fatal("built-in identifier omitted from the profile retained default exact identity policy")
	}

	// Mutating any input or returned value must not mutate the compiled plan.
	definition.Mappings[0].Field.Path = "changed"
	mappings[0].Field.StorageSettings["shape"].(map[string]any)["attribute"] = "changed"
	mappings[0].Field.StorageSettings["allowed"].([]string)[0] = "changed"
	lookup.Identifiers[0].Lookup.Fields[0].Selector.Path = "changed"
	lookup.Identifiers[0].Lookup.Variants[0] = "changed"
	lookup.Metadata.Lookups[0].Title.Fields[0].Selector.Path = "changed"
	copyDefinition := compiled.Definition()
	copyDefinition.Mappings[0].Field.Path = "changed"

	again := compiled.Mappings()
	if again[0].Field.Selector.Path != "field_identifier" || again[0].Field.StorageSettings["shape"].(map[string]any)["attribute"] != "attr0" || again[0].Field.StorageSettings["allowed"].([]string)[0] != "doi" {
		t.Fatalf("compiled mappings mutated through a copy: %#v", again[0])
	}
	againLookup := compiled.LookupPlan()
	if againLookup.Identifiers[0].Lookup.Fields[0].Selector.Path != "field_identifier" || againLookup.Identifiers[0].Lookup.Variants[0] != "canonical" {
		t.Fatalf("compiled lookup mutated through a copy: %#v", againLookup.Identifiers[0].Lookup)
	}
	if againLookup.Metadata.Lookups[0].Title.Fields[0].Selector.Path != "title" {
		t.Fatal("compiled metadata lookup mutated through a copy")
	}
	if compiled.Definition().Mappings[0].Field.Path != "field_identifier" {
		t.Fatal("compiled definition mutated through a copy")
	}
}

func TestDefinitionRejectsAmbiguousIdentifierAuthority(t *testing.T) {
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	definition.Identity.Identifiers[1].Namespace = "lehigh"
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err == nil || !strings.Contains(err.Error(), "absolute URI") {
		t.Fatalf("relative namespace error = %v", err)
	}

	definition = testDefinition(t, snapshot)
	definition.Identity.Identifiers[1].IdentityLevel = ""
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err == nil || !strings.Contains(err.Error(), "identity_level") {
		t.Fatalf("missing identity level error = %v", err)
	}
}

func TestDefinitionIdentifierPatternUsesWholeValueSemantics(t *testing.T) {
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	definition.Identity.Identifiers[1].Pattern = `^AB|CD$`
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(snapshot, &definition)
	if err != nil {
		t.Fatal(err)
	}
	for value, want := range map[string]bool{"AB": true, "CD": true, "AB-suffix": false, "prefix-CD": false} {
		matched, err := compiled.MatchesIdentifier("local-accession", value)
		if err != nil {
			t.Fatal(err)
		}
		if matched != want {
			t.Errorf("MatchesIdentifier(%q) = %v, want %v", value, matched, want)
		}
	}
}

func TestDefinitionRejectsOversizedIdentifierPattern(t *testing.T) {
	t.Parallel()

	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	definition.Identity.Identifiers[1].Pattern = "^" + strings.Repeat("a", maxIdentifierPatternBytes) + "$"
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err == nil || !strings.Contains(err.Error(), "pattern exceeds 4096 bytes") {
		t.Fatalf("oversized identifier pattern error = %v", err)
	}
}

func TestCompileCanonicalizesIdentifierNamespacesInLookupPlan(t *testing.T) {
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	definition.Identity.Identifiers[1].Namespace = "HTTPS://PRESERVE.LEHIGH.EDU/identifiers/accession/"
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(snapshot, &definition)
	if err != nil {
		t.Fatal(err)
	}
	got := compiled.LookupPlan().Identifiers[1].Namespace
	if got != "https://preserve.lehigh.edu/identifiers/accession/" {
		t.Fatalf("compiled namespace = %q", got)
	}
	identifier, err := compiled.NewIdentifier("local-accession", "AB-42")
	if err != nil {
		t.Fatal(err)
	}
	if identifier.GetNamespaceUri() != got {
		t.Fatalf("identifier namespace = %q, lookup namespace = %q", identifier.GetNamespaceUri(), got)
	}
}

func TestDefinitionRejectsUnknownCodec(t *testing.T) {
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	definition.Mappings[0].Decode = "made-up"
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err == nil || !strings.Contains(err.Error(), "invalid decode codec") {
		t.Fatalf("unknown codec error = %v", err)
	}
}

func TestCompileRejectsModelDriftAndUnknownFields(t *testing.T) {
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	definition.ModelFingerprint = strings.Repeat("a", 64)
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(snapshot, &definition); err == nil || !strings.Contains(err.Error(), "does not match snapshot") {
		t.Fatalf("model drift error = %v", err)
	}

	definition = testDefinition(t, snapshot)
	definition.Mappings[0].Field.Path = "field_missing"
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(snapshot, &definition); err == nil || !strings.Contains(err.Error(), "is not in the model") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestCompileRejectsNonExecutableMappings(t *testing.T) {
	snapshot := testModel(t)
	tests := []struct {
		name   string
		mutate func(*Definition)
		want   string
	}{
		{
			name: "unknown Hub target",
			mutate: func(definition *Definition) {
				definition.Mappings[1].Hub = "Titel"
			},
			want: "no canonical Record target",
		},
		{
			name: "incompatible codec",
			mutate: func(definition *Definition) {
				definition.Mappings[1].Decode = "typed-identifier"
			},
			want: "incompatible with Hub path",
		},
		{
			name: "append into scalar",
			mutate: func(definition *Definition) {
				definition.Mappings[1].Merge = MergeAppend
			},
			want: "append merge is incompatible",
		},
		{
			name: "replace repeated value",
			mutate: func(definition *Definition) {
				definition.Mappings[0].Merge = MergeReplace
			},
			want: "replace merge is incompatible",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := testDefinition(t, snapshot)
			test.mutate(&definition)
			definition.Fingerprint = model.Fingerprint{}
			if err := definition.SealFingerprint(); err != nil {
				t.Fatal(err)
			}
			if _, err := Compile(snapshot, &definition); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestCompileAllowsAppendAndReplaceForRepeatedCompatibilityPaths(t *testing.T) {
	snapshot := testModel(t)
	for _, hubPath := range []string{"Publisher", "PlacePublished", "PhysicalDesc", "Edition", "Language"} {
		for _, merge := range []MergePolicy{MergeAppend, MergeReplace} {
			t.Run(hubPath+"/"+string(merge), func(t *testing.T) {
				definition := testDefinition(t, snapshot)
				definition.Mappings[1].Hub = hubPath
				definition.Mappings[1].Merge = merge
				definition.Fingerprint = model.Fingerprint{}
				if err := definition.SealFingerprint(); err != nil {
					t.Fatal(err)
				}
				if _, err := Compile(snapshot, &definition); err != nil {
					t.Fatalf("Compile() error = %v", err)
				}
			})
		}
	}
}

func TestCompileRejectsArchivesSpaceProfiles(t *testing.T) {
	snapshot := testModel(t)
	snapshot.System = archivesSpaceSystem
	snapshot.Fingerprint = model.Fingerprint{}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}

	definition := testDefinition(t, snapshot)
	definition.System = archivesSpaceSystem
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(snapshot, &definition); err == nil || !strings.Contains(err.Error(), "ArchivesSpace profiles are not executable") {
		t.Fatalf("Compile() error = %v, want ArchivesSpace profile rejection", err)
	}
}

func TestCompileRequiresExactIdentifierSelectorBindings(t *testing.T) {
	snapshot := testModel(t)
	tests := []struct {
		name   string
		mutate func(*Definition)
		want   string
	}{
		{
			name: "typed mapping without identity rule",
			mutate: func(definition *Definition) {
				definition.Identity.Identifiers = definition.Identity.Identifiers[:1]
			},
			want: "typed-identifier mapping 3 has no identity rule for its exact selector",
		},
		{
			name: "identity rule without typed mapping",
			mutate: func(definition *Definition) {
				definition.Mappings = definition.Mappings[:2]
			},
			want: `identifier rule "local-accession" has no typed-identifier mapping for its exact selector`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := testDefinition(t, snapshot)
			test.mutate(&definition)
			definition.Fingerprint = model.Fingerprint{}
			if err := definition.SealFingerprint(); err != nil {
				t.Fatal(err)
			}
			if _, err := Compile(snapshot, &definition); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestCompileAllowsMultipleIdentifierRulesForOneSelector(t *testing.T) {
	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	secondDOI := definition.Identity.Identifiers[0]
	secondDOI.Name = "doi-version"
	secondDOI.IdentityLevel = IdentityVersion
	definition.Identity.Identifiers = append(definition.Identity.Identifiers, secondDOI)
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(snapshot, &definition); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
}

func TestPrepareDefinitionResealsEditedDraft(t *testing.T) {
	snapshot := testModel(t)
	draft := testDefinition(t, snapshot)
	oldFingerprint := draft.Fingerprint.Value
	draft.Mappings[1].Decode = "none"
	prepared, compiled, err := PrepareDefinition(snapshot, &draft)
	if err != nil {
		t.Fatalf("PrepareDefinition() error = %v", err)
	}
	if prepared.Fingerprint.Value == oldFingerprint || compiled.Fingerprint() != prepared.Fingerprint.Value {
		t.Fatalf("prepared fingerprint = %q, compiled = %q, old = %q", prepared.Fingerprint.Value, compiled.Fingerprint(), oldFingerprint)
	}
	if draft.Fingerprint.Value != oldFingerprint {
		t.Fatal("PrepareDefinition mutated the caller's draft")
	}
}

func TestProfilePathRejectsTraversalAndSaveIsAtomic(t *testing.T) {
	SetConfigDir(t.TempDir())
	t.Cleanup(func() { SetConfigDir("") })

	for _, name := range []string{"", ".", "..", "../escape", "a/b", `a\b`, "/absolute", "Upper", "white space"} {
		if path, err := ProfilePath(name); err == nil {
			t.Errorf("ProfilePath(%q) = %q, want error", name, path)
		}
	}
	path, err := ProfilePath("lehigh-preserve.v1")
	if err != nil {
		t.Fatalf("valid ProfilePath() error = %v", err)
	}
	directory, err := ProfilesDir()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(directory, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("profile path %q is outside %q", path, directory)
	}

	snapshot := testModel(t)
	definition := testDefinition(t, snapshot)
	if _, err := Publish(snapshot, &definition, PublishOptions{}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	loaded, err := LoadDefinition(definition.Name)
	if err != nil {
		t.Fatalf("LoadDefinition() error = %v", err)
	}
	if loaded.Fingerprint != definition.Fingerprint {
		t.Fatalf("loaded fingerprint = %#v, want %#v", loaded.Fingerprint, definition.Fingerprint)
	}
	definition.Description = "atomically replaced"
	if _, err := Publish(snapshot, &definition, PublishOptions{Force: true}); err != nil {
		t.Fatalf("replacement Publish() error = %v", err)
	}
	loaded, err = LoadDefinition(definition.Name)
	if err != nil || loaded.Description != "atomically replaced" {
		t.Fatalf("replacement LoadDefinition() = %#v, %v", loaded, err)
	}
	temporaryFiles, err := filepath.Glob(filepath.Join(directory, ".crosswalk-profile-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("atomic save left temporary files: %#v", temporaryFiles)
	}
}

func TestProfileStorageRejectsSymlinkedDirectory(t *testing.T) {
	configDirectory := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(configDirectory, "profiles")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	SetConfigDir(configDirectory)
	t.Cleanup(func() { SetConfigDir("") })
	if err := EnsureProfilesDir(); err == nil || !strings.Contains(err.Error(), "not a regular directory") {
		t.Fatalf("EnsureProfilesDir() error = %v", err)
	}
}

func testModel(t *testing.T) *model.Snapshot {
	t.Helper()
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion,
		System:  "drupal",
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "article",
			Fields: []model.Field{
				{Path: "title", SourceType: "core", Kind: model.ValueText, Cardinality: 1},
				{Path: "field_identifier", SourceType: "textfield_attr", Kind: model.ValueComposite, Cardinality: -1, StorageSettings: map[string]any{"shape": map[string]any{"attribute": "attr0", "value": "value"}, "allowed": []string{"doi", "local"}}},
				{Path: "field_linked_agent", SourceType: "typed_relation", Kind: model.ValueTypedReference, Cardinality: -1, Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: []string{"person"}}},
				{Path: "field_edtf_date_issued", SourceType: "edtf", Kind: model.ValueDate, Cardinality: 1},
			},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func testDefinition(t *testing.T, snapshot *model.Snapshot) Definition {
	t.Helper()
	identifierValue := FieldSelector{
		EntityType: "node", Bundle: "article", Path: "field_identifier", Attribute: "value",
		Where: &FieldPredicate{Attribute: "attr0", Equals: "doi"},
	}
	localValue := cloneSelector(identifierValue)
	localValue.Where.Equals = "local"
	definition := Definition{
		Version: CurrentDefinitionVersion, Name: "lehigh-preserve", Description: "fixture",
		System: "drupal", ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: []Mapping{
			{Field: identifierValue, Hub: "Identifiers", Decode: "typed-identifier", Encode: "typed-identifier", Merge: MergeAppend},
			{Field: FieldSelector{EntityType: "node", Bundle: "article", Path: "title"}, Hub: "Title", Decode: "text", Encode: "text", Merge: MergeFirstNonempty},
			{Field: localValue, Hub: "Identifiers", Decode: "typed-identifier", Encode: "typed-identifier", Merge: MergeAppend},
		},
		Identity: &IdentityPolicy{
			Version:    CurrentIdentityVersion,
			Repository: EntitySelector{EntityType: "node", Bundle: "article"},
			Identifiers: []IdentifierRule{
				{
					Name: "doi", Scheme: "doi", IdentityLevel: IdentityWork, Value: identifierValue,
					Pattern: `^10\.[0-9]{4,9}/\S+$`, Canonicalizer: "doi",
					Strength: IdentifierStrong, Scope: ScopeGlobal,
					Lookup: LookupRule{Fields: []FieldSelector{identifierValue}, Operator: LookupContains, Variants: []string{"canonical", "doi-url"}},
				},
				{
					Name: "local-accession", Scheme: "lehigh-accession", IdentityLevel: IdentitySourceRecord, Value: localValue,
					Pattern: `^[A-Z]{2}-[0-9]+$`, Canonicalizer: "trim",
					Strength: IdentifierStrong, Scope: ScopeInstitution, Namespace: "https://preserve.lehigh.edu/identifiers/accession/",
					Lookup: LookupRule{Fields: []FieldSelector{localValue}, Operator: LookupExact, Variants: []string{"canonical"}},
				},
			},
			Metadata: &MetadataIdentity{
				Title:        &FieldSelector{EntityType: "node", Bundle: "article", Path: "title"},
				Contributors: []FieldSelector{{EntityType: "node", Bundle: "article", Path: "field_linked_agent", Attribute: "target_id"}},
				Date:         &FieldSelector{EntityType: "node", Bundle: "article", Path: "field_edtf_date_issued", Attribute: "value"},
				Lookups: []MetadataLookup{
					{
						Name:         "title-author",
						Title:        LookupRule{Fields: []FieldSelector{{EntityType: "node", Bundle: "article", Path: "title"}}, Operator: LookupContains, Variants: []string{"canonical", "title-phrase"}},
						Contributors: &LookupRule{Fields: []FieldSelector{{EntityType: "node", Bundle: "article", Path: "field_linked_agent", Attribute: "name"}}, Operator: LookupContains, Variants: []string{"author-family"}},
					},
					{
						Name:  "title-date",
						Title: LookupRule{Fields: []FieldSelector{{EntityType: "node", Bundle: "article", Path: "title"}}, Operator: LookupContains, Variants: []string{"canonical"}},
						Date:  &LookupRule{Fields: []FieldSelector{{EntityType: "node", Bundle: "article", Path: "field_edtf_date_issued", Attribute: "value"}}, Operator: LookupExact, Variants: []string{"year"}},
					},
				},
				MetadataOnly: MetadataOnlyReview,
			},
		},
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if err := definition.Validate(); err != nil {
		t.Fatal(err)
	}
	return definition
}
