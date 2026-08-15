package spec

import (
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/model"
	modeldrupal "github.com/lehigh-university-libraries/crosswalk/model/drupal"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

func TestProfileMappingForSpecFieldRequiresExactQualifiedSelector(t *testing.T) {
	mapping := profile.CompiledMapping{Field: profile.ResolvedField{Selector: profile.FieldSelector{
		EntityType: "node", Bundle: "islandora_object", Path: "field_identifier", Attribute: "value",
		Where: &profile.FieldPredicate{Attribute: "attr0", Equals: "accession"},
	}}}
	if _, found, err := profileMappingForSpecField(Field{Name: "field_identifier.attr0=doi"}, []profile.CompiledMapping{mapping}); err != nil || found {
		t.Fatalf("DOI sibling mapping = found %v, error %v", found, err)
	}
	selected, found, err := profileMappingForSpecField(Field{Name: "field_identifier.attr0=accession"}, []profile.CompiledMapping{mapping})
	if err != nil || !found || selected.Field.Selector.Where.Equals != "accession" {
		t.Fatalf("accession mapping = %#v, found %v, error %v", selected, found, err)
	}

	unqualified := profile.CompiledMapping{Field: profile.ResolvedField{Selector: profile.FieldSelector{
		EntityType: "node", Bundle: "islandora_object", Path: "field_plain",
	}}}
	if _, found, err := profileMappingForSpecField(Field{Name: "field_plain"}, []profile.CompiledMapping{unqualified}); err != nil || !found {
		t.Fatalf("unqualified mapping = found %v, error %v", found, err)
	}

	aggregate := profile.CompiledMapping{
		Field: profile.ResolvedField{Selector: profile.FieldSelector{
			EntityType: "node", Bundle: "islandora_object", Path: "field_linked_agent",
		}},
		Hub: "Contributors",
	}
	for _, field := range []Field{
		{Name: "field_linked_agent.name", Hub: "Contributors.Name"},
		{Name: "field_linked_agent.rel_type", Hub: "Contributors.RoleCode"},
		{Name: "field_linked_agent.vid", Hub: "Contributors.Type"},
	} {
		selected, found, err := profileMappingForSpecField(field, []profile.CompiledMapping{aggregate})
		if err != nil || !found || selected.Field.Selector.Path != "field_linked_agent" {
			t.Fatalf("aggregate mapping for %q = %#v, found %v, error %v", field.Name, selected, found, err)
		}
	}

	if _, found, err := profileMappingForSpecField(
		Field{Name: "field_linked_agent.name", Hub: "Unrelated.Name"},
		[]profile.CompiledMapping{aggregate},
	); err != nil || found {
		t.Fatalf("unrelated Hub mapping = found %v, error %v", found, err)
	}
}

func TestCompileDrupalProfileUsesEditedMappingsForSourceAndTarget(t *testing.T) {
	snapshot, err := modeldrupal.CompileDirectory("testdata/drupal", modeldrupal.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "edited", EntityType: "node", Bundle: "islandora_object",
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range definition.Mappings {
		if definition.Mappings[index].Field.Path == "field_custom_tags" {
			definition.Mappings[index].Hub = "Subjects.keywords"
			definition.Mappings[index].Merge = profile.MergeAppend
		}
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	transformation, err := CompileDrupalProfile(snapshot, compiled, DrupalCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	source, ok := transformation.SourceField("field_custom_tags")
	if !ok || source.Hub != "Subjects.keywords" || source.Codec != "multi" {
		t.Fatalf("profile-bound source field = %#v, found = %v", source, ok)
	}
	if _, ok := transformation.TargetField("field_custom_tags"); !ok {
		t.Fatal("profile-bound target omitted field_custom_tags")
	}
	if transformation.Fingerprint.Profile != compiled.Fingerprint() || transformation.Fingerprint.Model != compiled.ModelFingerprint() {
		t.Fatalf("profile-bound fingerprint = %#v", transformation.Fingerprint)
	}
	if _, ok := transformation.TargetField("field_rating"); !ok {
		t.Fatal("profile-bound target omitted another encodable profile field")
	}
}

func TestCompileDrupalProfileRejectsConflictingIdentityRulesForExactSelector(t *testing.T) {
	snapshot, err := modeldrupal.CompileDirectory("testdata/drupal", modeldrupal.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "conflicting-identifiers", EntityType: "node", Bundle: "islandora_object",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for index := range definition.Identity.Identifiers {
		if definition.Identity.Identifiers[index].Name == "doi-version" {
			definition.Identity.Identifiers[index].Pattern = `^10\.9999/.+$`
			found = true
			break
		}
	}
	if !found {
		t.Fatal("generated Drupal profile has no doi-version identity rule")
	}
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatalf("profile.Compile() error = %v", err)
	}

	_, err = CompileDrupalProfile(snapshot, compiled, DrupalCompileOptions{})
	if err == nil || !strings.Contains(err.Error(), `identity rules "doi" and "doi-version" share exact selector "field_identifier.attr0=doi"`) {
		t.Fatalf("CompileDrupalProfile() error = %v", err)
	}
}

func TestCompileDrupalProfileSelectsDeterministicEquivalentIdentityRule(t *testing.T) {
	snapshot, err := modeldrupal.CompileDirectory("testdata/drupal", modeldrupal.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "equivalent-identifiers", EntityType: "node", Bundle: "islandora_object",
	})
	if err != nil {
		t.Fatal(err)
	}
	doiIndex, versionIndex := -1, -1
	for index, rule := range definition.Identity.Identifiers {
		switch rule.Name {
		case "doi":
			doiIndex = index
		case "doi-version":
			versionIndex = index
		}
	}
	if doiIndex < 0 || versionIndex < 0 {
		t.Fatalf("generated Drupal DOI rule indexes = %d/%d", doiIndex, versionIndex)
	}
	definition.Identity.Identifiers[doiIndex], definition.Identity.Identifiers[versionIndex] =
		definition.Identity.Identifiers[versionIndex], definition.Identity.Identifiers[doiIndex]
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatalf("profile.Compile() error = %v", err)
	}

	transformation, err := CompileDrupalProfile(snapshot, compiled, DrupalCompileOptions{})
	if err != nil {
		t.Fatalf("CompileDrupalProfile() error = %v", err)
	}
	field, ok := transformation.SourceField("field_identifier.attr0=doi")
	if !ok || field.ProfileRule != "doi" {
		t.Fatalf("DOI source field = %#v, found = %v", field, ok)
	}
}

func TestCompileDrupalProfileRejectsUnsupportedWorkbenchEncoding(t *testing.T) {
	snapshot, err := modeldrupal.CompileDirectory("testdata/drupal", modeldrupal.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for entityIndex := range snapshot.Entities {
		for fieldIndex := range snapshot.Entities[entityIndex].Fields {
			field := &snapshot.Entities[entityIndex].Fields[fieldIndex]
			if field.Path == "field_custom_tags" {
				field.SourceType = "mystery_composite"
			}
		}
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "unsupported", EntityType: "node", Bundle: "islandora_object",
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileDrupalProfile(snapshot, compiled, DrupalCompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "no explicit Workbench cell encoding") {
		t.Fatalf("CompileDrupalProfile() error = %v", err)
	}
}

func TestCompileDrupalProfileSupportsListStringFields(t *testing.T) {
	snapshot, err := modeldrupal.CompileDirectory("testdata/drupal-validation", modeldrupal.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "list-string", EntityType: "node", Bundle: "islandora_object",
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	transformation, err := CompileDrupalProfile(snapshot, compiled, DrupalCompileOptions{})
	if err != nil {
		t.Fatalf("CompileDrupalProfile() error = %v", err)
	}
	field, ok := transformation.SourceField("field_access_level")
	if !ok || field.SourceType != "list_string" {
		t.Fatalf("list-string source field = %#v, found = %v", field, ok)
	}
	assertFieldValidation(t, field, ValidationEnum, func(validation Validation) bool {
		return equalStrings(validation.Values, []string{"public", "staff"})
	})
}

func TestDrupalWorkbenchProfilesSupportStructuredAndWritableBaseFields(t *testing.T) {
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion,
		System:  "drupal",
		Entities: []model.Entity{{
			EntityType: "node",
			Bundle:     "islandora_object",
			Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "changed", SourceType: "changed", Kind: model.ValueDate, Cardinality: 1},
				{Path: "created", SourceType: "created", Kind: model.ValueDate, Cardinality: 1},
				{Path: "uid", SourceType: "entity_reference", Kind: model.ValueReference, Cardinality: 1, Reference: &model.Reference{EntityType: "user"}},
				{Path: "field_coordinates", SourceType: "geolocation", Kind: model.ValueOpaque, Cardinality: -1},
				{Path: "field_vocabulary_link", SourceType: "authority_link", Kind: model.ValueOpaque, Cardinality: -1, InstanceSettings: map[string]any{
					"authority_sources": []any{"viaf", "lcnaf"},
				}},
				{Path: "field_track", SourceType: "media_track", Kind: model.ValueOpaque, Cardinality: -1},
				{Path: "field_linked_data", SourceType: "linked_data_field", Kind: model.ValueOpaque, Cardinality: -1},
			},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}

	modelOnly, err := CompileDrupalModel(snapshot, DrupalCompileOptions{Bundle: "islandora_object"})
	if err != nil {
		t.Fatalf("CompileDrupalModel() error = %v", err)
	}
	if _, ok := modelOnly.SourceField("changed"); ok {
		t.Error("model-only Workbench source retained runtime-managed changed field")
	}
	if _, ok := modelOnly.TargetField("changed"); ok {
		t.Error("model-only Workbench target retained runtime-managed changed field")
	}
	modelSourceUID, sourceFound := modelOnly.SourceField("uid")
	modelTargetUID, targetFound := modelOnly.TargetField("uid")
	if !sourceFound || !targetFound {
		t.Fatalf("model-only uid fields found: source=%v target=%v", sourceFound, targetFound)
	}
	for _, field := range []Field{modelSourceUID, modelTargetUID} {
		if !field.AppliesTo(OperationCreate) || field.AppliesTo(OperationUpdate) {
			t.Errorf("model-only uid field = %#v", field)
		}
	}
	urlAlias, ok := modelOnly.SourceField("url_alias")
	if !ok {
		t.Fatal("model-only Workbench source omitted url_alias")
	}
	assertFieldValidation(t, urlAlias, ValidationPattern, func(validation Validation) bool {
		return validation.Pattern == `^/.*$`
	})
	langcode, ok := modelOnly.SourceField("langcode")
	if !ok {
		t.Fatal("model-only Workbench source omitted langcode")
	}
	assertFieldValidation(t, langcode, ValidationEnum, func(validation Validation) bool {
		return containsString(validation.Values, "en") && containsString(validation.Values, "zh-hant") && !containsString(validation.Values, "invalid")
	})
	aliasUnique := false
	for _, validation := range modelOnly.Source.Validations {
		if validation.Rule == ValidationUnique && equalStrings(validation.Fields, []string{"url_alias"}) {
			aliasUnique = true
		}
	}
	if !aliasUnique {
		t.Fatal("model-only Workbench source omitted within-sheet URL alias uniqueness")
	}

	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "workbench-parity", EntityType: "node", Bundle: "islandora_object",
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	transformation, err := CompileDrupalProfile(snapshot, compiled, DrupalCompileOptions{})
	if err != nil {
		t.Fatalf("CompileDrupalProfile() error = %v", err)
	}
	if _, ok := transformation.SourceField("changed"); ok {
		t.Fatal("profile-bound source retained runtime-managed changed field")
	}
	if _, ok := transformation.TargetField("changed"); ok {
		t.Fatal("profile-bound target retained runtime-managed changed field")
	}
	created, ok := transformation.SourceField("created")
	if !ok || created.Hub != "Dates.Created" || created.Codec != "edtf" || !created.AppliesTo(OperationCreate) || !created.AppliesTo(OperationUpdate) {
		t.Fatalf("profile-bound created field = %#v, found = %v", created, ok)
	}
	assertFieldValidation(t, created, ValidationPattern, func(validation Validation) bool {
		return strings.Contains(validation.Pattern, "T") && strings.Contains(validation.Pattern, "[+-]")
	})
	assertFieldValidation(t, created, ValidationNotFutureTimestamp, func(Validation) bool { return true })
	uid, ok := transformation.SourceField("uid")
	if !ok || !uid.AppliesTo(OperationCreate) || uid.AppliesTo(OperationUpdate) {
		t.Fatalf("profile-bound uid field = %#v, found = %v", uid, ok)
	}
	for _, test := range []struct {
		name string
		rule ValidationRule
	}{
		{name: "field_coordinates", rule: ValidationGeolocation},
		{name: "field_vocabulary_link", rule: ValidationAuthorityLink},
		{name: "field_track", rule: ValidationMediaTrack},
	} {
		field, ok := transformation.SourceField(test.name)
		if !ok {
			t.Fatalf("profile-bound source omitted %s", test.name)
		}
		assertFieldValidation(t, field, test.rule, nil)
	}
	if field, ok := transformation.SourceField("field_linked_data"); !ok || field.SourceType != "linked_data_field" {
		t.Fatalf("profile-bound linked-data field = %#v, found = %v", field, ok)
	}
}
