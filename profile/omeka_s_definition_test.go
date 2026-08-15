package profile

import (
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/model"
)

func TestNewOmekaSDefinitionBuildsTemplateProfile(t *testing.T) {
	t.Parallel()

	snapshot := omekaSTestModel(t)
	definition, err := NewOmekaSDefinition(snapshot, OmekaSDefinitionOptions{Name: "omeka-photographs", ResourceTemplateID: 200})
	if err != nil {
		t.Fatalf("NewOmekaSDefinition(): %v", err)
	}
	if definition.System != "omeka-s" || definition.ModelFingerprint != snapshot.Fingerprint.Value {
		t.Fatalf("definition binding = %#v", definition)
	}
	if definition.Identity == nil || definition.Identity.Repository != (EntitySelector{EntityType: "resource", Bundle: "200"}) {
		t.Fatalf("identity repository = %#v", definition.Identity)
	}
	if definition.Identity.Metadata == nil || definition.Identity.Metadata.Title == nil || definition.Identity.Metadata.Title.Path != "dcterms:title" {
		t.Fatalf("metadata identity = %#v", definition.Identity.Metadata)
	}
	if len(definition.Identity.Metadata.Lookups) != 1 || definition.Identity.Metadata.Lookups[0].Name != "title-only" {
		t.Errorf("metadata lookups = %#v", definition.Identity.Metadata.Lookups)
	}
	if mapping, exists := omekaSTestMapping(definition.Mappings, "dcterms:title"); !exists || mapping.Hub != "Title" || mapping.Merge != MergeFirstNonempty {
		t.Errorf("title mapping = %#v, exists %t", mapping, exists)
	}
	if mapping, exists := omekaSTestMapping(definition.Mappings, "local:department"); !exists || mapping.Hub != "Extra."+omekaSExtraKey("local:department") || mapping.Merge != MergeAppend {
		t.Errorf("department mapping = %#v, exists %t", mapping, exists)
	}
	if _, err := Compile(snapshot, definition); err != nil {
		t.Fatalf("Compile(): %v", err)
	}
}

func TestNewOmekaSDefinitionAddsExplicitInstitutionalIdentity(t *testing.T) {
	t.Parallel()

	snapshot := omekaSTestModel(t)
	definition, err := NewOmekaSDefinition(snapshot, OmekaSDefinitionOptions{
		Name: "omeka-photographs", ResourceTemplateID: 200,
		InstitutionalIdentifier: &OmekaSInstitutionalIdentifierOptions{
			FieldPath: "local:department", Scheme: "example-accession",
			NamespaceURI: "https://example.org/id/", Pattern: `^[A-Z]{2}-[0-9]{4}$`,
		},
	})
	if err != nil {
		t.Fatalf("NewOmekaSDefinition(): %v", err)
	}
	if len(definition.Identity.Identifiers) != 1 {
		t.Fatalf("identifier rules = %#v", definition.Identity.Identifiers)
	}
	rule := definition.Identity.Identifiers[0]
	if rule.Scheme != "example-accession" || rule.Strength != IdentifierStrong || rule.Scope != ScopeInstitution || rule.Namespace != "https://example.org/id/" {
		t.Errorf("identifier rule = %#v", rule)
	}
	if mapping, exists := omekaSTestMapping(definition.Mappings, "local:department"); !exists || mapping.Hub != "Identifiers" || mapping.Decode != "typed-identifier" {
		t.Errorf("identifier mapping = %#v, exists %t", mapping, exists)
	}
	compiled, err := Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	if identifier, err := compiled.NewIdentifier("example-accession", "AB-1234"); err != nil || identifier.GetNamespaceUri() != "https://example.org/id/" {
		t.Errorf("NewIdentifier() = %#v, %v", identifier, err)
	}
}

func TestNewOmekaSDefinitionRejectsUnknownTemplateAndIdentifierField(t *testing.T) {
	t.Parallel()

	snapshot := omekaSTestModel(t)
	if _, err := NewOmekaSDefinition(snapshot, OmekaSDefinitionOptions{Name: "missing", ResourceTemplateID: 999}); err == nil {
		t.Fatal("unknown template succeeded")
	}
	if _, err := NewOmekaSDefinition(snapshot, OmekaSDefinitionOptions{
		Name: "bad-id", ResourceTemplateID: 200,
		InstitutionalIdentifier: &OmekaSInstitutionalIdentifierOptions{FieldPath: "missing", Scheme: "example-id", NamespaceURI: "https://example.org/id/", Pattern: `^.+$`},
	}); err == nil {
		t.Fatal("unknown identifier field succeeded")
	}
}

func TestOmekaSExtraKeysDoNotMergeDifferentTerms(t *testing.T) {
	t.Parallel()

	first := omekaSExtraKey("local:foo-bar")
	second := omekaSExtraKey("local:foo.bar")
	if first == second {
		t.Fatalf("different compact IRIs produced the same Extra key %q", first)
	}
	for _, value := range []string{first, second} {
		if !hookNamePattern.MatchString(value) {
			t.Errorf("Extra key %q is not a machine name", value)
		}
	}
}

func omekaSTestModel(t *testing.T) *model.Snapshot {
	t.Helper()
	value := &model.Snapshot{
		Version: model.CurrentVersion, System: "omeka-s",
		Entities: []model.Entity{
			{EntityType: "resource", Label: "All resources", Fields: []model.Field{
				{Path: "o:id", SourceType: "integer", Kind: model.ValueInteger, Cardinality: 1, Required: true},
				{Path: "o:title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "dcterms:title", SourceType: "omeka-s:value", Kind: model.ValueComposite, Cardinality: -1},
			}},
			{EntityType: "resource", Bundle: "200", Label: "Photograph Template", SemanticTypes: []string{"local:Photograph"}, Fields: []model.Field{
				{Path: "o:id", SourceType: "integer", Kind: model.ValueInteger, Cardinality: 1, Required: true},
				{Path: "o:title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "dcterms:title", SourceType: "omeka-s:literal", Kind: model.ValueText, Cardinality: -1, Required: true},
				{Path: "local:department", SourceType: "omeka-s:literal|uri", Kind: model.ValueComposite, Cardinality: -1},
			}},
		},
	}
	if err := value.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	return value
}

func omekaSTestMapping(mappings []Mapping, path string) (Mapping, bool) {
	for _, mapping := range mappings {
		if mapping.Field.Path == path {
			return mapping, true
		}
	}
	return Mapping{}, false
}
