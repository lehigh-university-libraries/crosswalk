package spec

import (
	"strings"
	"testing"

	modeldrupal "github.com/lehigh-university-libraries/crosswalk/model/drupal"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

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
