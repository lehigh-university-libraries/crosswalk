package profile

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/model"
	"gopkg.in/yaml.v3"
)

func TestNewDrupalDefinitionBuildsOrderedMappingsAndConservativeIdentity(t *testing.T) {
	snapshot := drupalStarterModel(t)
	definition, err := NewDrupalDefinition(snapshot, DrupalDefinitionOptions{
		Name: "example", EntityType: "node", Bundle: "article",
	})
	if err != nil {
		t.Fatalf("NewDrupalDefinition() error = %v", err)
	}
	paths := make([]string, len(definition.Mappings))
	codecs := make(map[string]string)
	identifierMappings := 0
	uniqueIdentifierSelectors := make(map[string]struct{})
	for index, mapping := range definition.Mappings {
		paths[index] = mapping.Field.Path
		codecs[mapping.Field.Path] = mapping.Decode
		if mapping.Field.Path == "field_identifier" {
			identifierMappings++
			if mapping.Decode != "typed-identifier" || mapping.Field.Attribute != "value" || mapping.Field.Where == nil {
				t.Fatalf("structured identifier mapping = %#v", mapping)
			}
		}
		if mapping.Decode != mapping.Encode {
			t.Fatalf("mapping %q has asymmetric starter codecs: %#v", mapping.Field.Path, mapping)
		}
	}
	if paths[0] != "field_edtf_date_issued" || paths[len(paths)-3] != "field_linked_agent" || paths[len(paths)-2] != "field_rating" || paths[len(paths)-1] != "title" {
		t.Fatalf("mapping path order = %#v", paths)
	}
	for _, rule := range definition.Identity.Identifiers {
		uniqueIdentifierSelectors[selectorKey(rule.Value)] = struct{}{}
	}
	if identifierMappings != len(uniqueIdentifierSelectors) {
		t.Fatalf("identifier mapping count = %d, want one per selector (%d)", identifierMappings, len(uniqueIdentifierSelectors))
	}
	if codecs["field_identifier"] != "typed-identifier" || codecs["field_linked_agent"] != "typed-relation" || codecs["field_edtf_date_issued"] != "date" || codecs["field_rating"] != "integer" {
		t.Fatalf("starter codecs = %#v", codecs)
	}
	if definition.Identity == nil || len(definition.Identity.Identifiers) < 7 {
		t.Fatalf("identity policy = %#v", definition.Identity)
	}
	wantIdentifiers := []struct {
		name   string
		scheme string
		level  IdentifierIdentityLevel
	}{
		{name: "doi", scheme: "doi", level: IdentityWork},
		{name: "doi-version", scheme: "doi", level: IdentityVersion},
		{name: "arxiv", scheme: "arxiv", level: IdentityWork},
		{name: "wos", scheme: "wos", level: IdentityWork},
		{name: "scopus-eid", scheme: "scopus-eid", level: IdentitySourceRecord},
		{name: "scopus-id", scheme: "scopus-id", level: IdentitySourceRecord},
		{name: "zenodo-record", scheme: "zenodo-record", level: IdentityVersion},
		{name: "zenodo-concept", scheme: "zenodo-concept", level: IdentityConcept},
	}
	for index, want := range wantIdentifiers {
		got := definition.Identity.Identifiers[index]
		if got.Name != want.name || got.Scheme != want.scheme || got.IdentityLevel != want.level {
			t.Fatalf("identifier %d = %q/%q/%q, want %q/%q/%q", index, got.Name, got.Scheme, got.IdentityLevel, want.name, want.scheme, want.level)
		}
	}
	if definition.Identity.Identifiers[0].Strength != IdentifierStrong || definition.Identity.Identifiers[1].Strength != IdentifierStrong || definition.Identity.Identifiers[7].Strength != IdentifierCorroborating {
		t.Fatalf("DOI work/version and Zenodo concept strengths = %q/%q/%q", definition.Identity.Identifiers[0].Strength, definition.Identity.Identifiers[1].Strength, definition.Identity.Identifiers[7].Strength)
	}
	registryRule, exists := mustCompileDefinition(t, snapshot, definition).IdentifierRegistry().Rule("doi")
	if !exists || !identifierLevelIsExact(registryRule.ExactIdentityLevels, IdentityWork) || !identifierLevelIsExact(registryRule.ExactIdentityLevels, IdentityVersion) || identifierLevelIsExact(registryRule.ExactIdentityLevels, IdentityConcept) {
		t.Fatalf("compiled DOI exact identity levels = %#v", registryRule.ExactIdentityLevels)
	}
	metadata := definition.Identity.Metadata
	if metadata == nil || metadata.MetadataOnly != MetadataOnlyReview || len(metadata.Lookups) != 3 {
		t.Fatalf("metadata policy = %#v", metadata)
	}
	if got := []string{metadata.Lookups[0].Name, metadata.Lookups[1].Name, metadata.Lookups[2].Name}; !reflect.DeepEqual(got, []string{"title-author", "title-date", "title-only"}) {
		t.Fatalf("metadata lookup order = %#v", got)
	}
	if _, err := Compile(snapshot, definition); err != nil {
		t.Fatalf("generated definition does not compile: %v", err)
	}

	reversed := drupalStarterModel(t)
	for left, right := 0, len(reversed.Entities[0].Fields)-1; left < right; left, right = left+1, right-1 {
		reversed.Entities[0].Fields[left], reversed.Entities[0].Fields[right] = reversed.Entities[0].Fields[right], reversed.Entities[0].Fields[left]
	}
	if err := reversed.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	again, err := NewDrupalDefinition(reversed, DrupalDefinitionOptions{Name: "example", EntityType: "node", Bundle: "article"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Fingerprint != definition.Fingerprint {
		t.Fatalf("starter fingerprint depends on model field order: %#v != %#v", again.Fingerprint, definition.Fingerprint)
	}
}

func mustCompileDefinition(t *testing.T, snapshot *model.Snapshot, definition *Definition) *Compiled {
	t.Helper()
	compiled, err := Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestNewDrupalDefinitionRequiresExplicitInstitutionAuthority(t *testing.T) {
	snapshot := drupalStarterModel(t)
	definition, err := NewDrupalDefinition(snapshot, DrupalDefinitionOptions{
		Name: "institution", EntityType: "node", Bundle: "article",
		InstitutionalIdentifier: &InstitutionalIdentifierOptions{
			Attribute: "accession", Scheme: "example-accession",
			NamespaceURI: "HTTPS://REPOSITORY.EXAMPLE.EDU/id/accession/",
			Pattern:      `^[A-Z]{3}-[0-9]{6}$`, IdentityLevel: IdentitySourceRecord,
		},
	})
	if err != nil {
		t.Fatalf("NewDrupalDefinition() error = %v", err)
	}
	rule := definition.Identity.Identifiers[len(definition.Identity.Identifiers)-1]
	if rule.Scheme != "example-accession" || rule.Namespace != "https://repository.example.edu/id/accession/" || rule.Strength != IdentifierStrong || rule.Value.Where == nil || rule.Value.Where.Equals != "accession" {
		t.Fatalf("institution rule = %#v", rule)
	}
	if _, err := NewDrupalDefinition(snapshot, DrupalDefinitionOptions{
		Name: "unsafe", EntityType: "node", Bundle: "article",
		InstitutionalIdentifier: &InstitutionalIdentifierOptions{Attribute: "local", Scheme: "local", NamespaceURI: "https://example.edu/id/", Pattern: `^.+$`},
	}); err == nil {
		t.Fatal("built-in local scheme accepted as an institution scheme")
	}
}

func TestNewDrupalDefinitionUsesScalarSelectorForDedicatedIdentifierField(t *testing.T) {
	snapshot := drupalStarterModel(t)
	snapshot.Entities[0].Fields = append(snapshot.Entities[0].Fields, model.Field{
		Path: "field_doi", SourceType: "string", Kind: model.ValueText, Cardinality: 1,
	})
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := NewDrupalDefinition(snapshot, DrupalDefinitionOptions{
		Name: "scalar-identifier", EntityType: "node", Bundle: "article",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range definition.Identity.Identifiers {
		if rule.Scheme == "doi" {
			if rule.Value.Path != "field_doi" || rule.Value.Attribute != "" || rule.Value.Where != nil {
				t.Fatalf("dedicated DOI selector = %#v", rule.Value)
			}
			return
		}
	}
	t.Fatal("generated profile has no DOI identity rule")
}

func TestNewDrupalDefinitionExpandsPublicationCompositeFields(t *testing.T) {
	snapshot := drupalStarterModel(t)
	snapshot.Entities[0].Fields = append(snapshot.Entities[0].Fields,
		model.Field{Path: "field_related_item", SourceType: "related_item", Kind: model.ValueComposite, Cardinality: -1},
		model.Field{Path: "field_part_detail", SourceType: "part_detail", Kind: model.ValueComposite, Cardinality: -1},
	)
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := NewDrupalDefinition(snapshot, DrupalDefinitionOptions{Name: "publication", EntityType: "node", Bundle: "article"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"field_related_item/title//:Publication.Title":                           false,
		"field_related_item/identifier/identifier_type/l-issn:Publication.LIssn": false,
		"field_related_item/identifier/identifier_type/issn:Publication.Issn":    false,
		"field_part_detail/number/type/volume:Publication.Volume":                false,
		"field_part_detail/number/type/issue:Publication.Issue":                  false,
		"field_part_detail/number/type/page:Publication.Pages":                   false,
	}
	for _, mapping := range definition.Mappings {
		predicateAttribute, predicateValue := "", ""
		if mapping.Field.Where != nil {
			predicateAttribute = mapping.Field.Where.Attribute
			predicateValue = mapping.Field.Where.Equals
		}
		key := mapping.Field.Path + "/" + mapping.Field.Attribute + "/" + predicateAttribute + "/" + predicateValue + ":" + mapping.Hub
		if _, exists := want[key]; exists {
			want[key] = true
			if mapping.Decode != "composite" || mapping.Encode != "composite" {
				t.Fatalf("publication mapping = %#v", mapping)
			}
		}
		if (mapping.Field.Path == "field_related_item" || mapping.Field.Path == "field_part_detail") && strings.HasPrefix(mapping.Hub, "Extra.") {
			t.Fatalf("publication composite fell back to Extra: %#v", mapping)
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("missing publication mapping %s", key)
		}
	}
}

func TestNewDrupalDefinitionMapsRepeatedBibliographicFields(t *testing.T) {
	snapshot := drupalStarterModel(t)
	snapshot.Entities[0].Fields = append(snapshot.Entities[0].Fields,
		model.Field{Path: "field_publisher", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
		model.Field{Path: "field_place_published", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
		model.Field{Path: "field_physical_description", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
		model.Field{Path: "field_extent", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
		model.Field{Path: "field_edition", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
		model.Field{Path: "field_language", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
	)
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := NewDrupalDefinition(snapshot, DrupalDefinitionOptions{Name: "bibliographic", EntityType: "node", Bundle: "article"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"field_publisher":            "Publisher",
		"field_place_published":      "PlacePublished",
		"field_physical_description": "PhysicalDesc",
		"field_extent":               "PhysicalDesc",
		"field_edition":              "Edition",
		"field_language":             "Language",
	}
	for _, mapping := range definition.Mappings {
		hubPath, exists := want[mapping.Field.Path]
		if !exists {
			continue
		}
		if mapping.Hub != hubPath || mapping.Merge != MergeAppend {
			t.Errorf("mapping %s = Hub %q merge %q, want Hub %q merge %q", mapping.Field.Path, mapping.Hub, mapping.Merge, hubPath, MergeAppend)
		}
		delete(want, mapping.Field.Path)
	}
	if len(want) != 0 {
		t.Fatalf("missing mappings: %#v", want)
	}
	if _, err := Compile(snapshot, definition); err != nil {
		t.Fatalf("generated definition does not compile: %v", err)
	}
}

func drupalStarterModel(t *testing.T) *model.Snapshot {
	t.Helper()
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Provenance: model.Provenance{SiteName: "Example Repository"},
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "article",
			Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "field_rating", SourceType: "integer", Kind: model.ValueInteger, Cardinality: 1},
				{Path: "field_identifier", SourceType: "textfield_attr", Kind: model.ValueComposite, Cardinality: -1},
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

func mustMarshalYAML(t *testing.T, value any) []byte {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
