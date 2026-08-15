package spec

import (
	"reflect"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/model"
)

func TestCompileDrupalModelBindsOperationalFilesToMediaBundleConfiguration(t *testing.T) {
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion,
		System:  "drupal",
		Entities: []model.Entity{
			{EntityType: "node", Bundle: "islandora_object", Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			}},
			{EntityType: "media", Bundle: "file", Fields: []model.Field{
				{Path: "field_media_file", SourceType: "file", Kind: model.ValueFile, Cardinality: 1, InstanceSettings: map[string]any{"file_extensions": "odt htm"}},
			}},
			{EntityType: "media", Bundle: "document", Fields: []model.Field{
				{Path: "field_media_document", SourceType: "file", Kind: model.ValueFile, Cardinality: 1, InstanceSettings: map[string]any{"file_extensions": "pdf docx"}},
				{Path: "field_media_thumbnail", SourceType: "image", Kind: model.ValueFile, Cardinality: 1, InstanceSettings: map[string]any{"file_extensions": "jpg"}},
			}},
			{EntityType: "media", Bundle: "video", Fields: []model.Field{
				{Path: "field_media_video_file", SourceType: "file", Kind: model.ValueFile, Cardinality: 1, InstanceSettings: map[string]any{"file_extensions": "mp4"}},
			}},
			{EntityType: "media", Bundle: "model_3d", Fields: []model.Field{
				{Path: "field_media_model", SourceType: "file", Kind: model.ValueFile, Cardinality: 1, InstanceSettings: map[string]any{"file_extensions": "stl"}},
			}},
		},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	transformation, err := CompileDrupalModel(snapshot, DrupalCompileOptions{Bundle: "islandora_object"})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"file", "supplemental_file", "unpublished_supplemental_file"} {
		field, ok := transformation.SourceField(name)
		if !ok {
			t.Fatalf("missing operational file field %q", name)
		}
		validation, ok := fieldValidation(field, ValidationMediaExtension)
		if !ok {
			t.Fatalf("%s has no media-extension policy: %#v", name, field.Validations)
		}
		assertMediaExtensions(t, validation.MediaTypes, "file", []string{"htm", "odt"}, true)
		assertMediaExtensions(t, validation.MediaTypes, "document", []string{"docx", "pdf"}, false)
		assertMediaExtensions(t, validation.MediaTypes, "video", []string{"mp4"}, false)
		assertMediaExtensions(t, validation.MediaTypes, "image", nil, false)
		assertNoMediaType(t, validation.MediaTypes, "model_3d")
	}
}

func TestCompileDrupalModelDoesNotGuessMissingMediaFileField(t *testing.T) {
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion,
		System:  "drupal",
		Entities: []model.Entity{
			{EntityType: "node", Bundle: "islandora_object", Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			}},
			{EntityType: "media", Bundle: "document", Fields: []model.Field{
				{Path: "field_media_thumbnail", SourceType: "image", Kind: model.ValueFile, Cardinality: 1, InstanceSettings: map[string]any{"file_extensions": "jpg"}},
			}},
		},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	transformation, err := CompileDrupalModel(snapshot, DrupalCompileOptions{Bundle: "islandora_object"})
	if err != nil {
		t.Fatal(err)
	}
	field, ok := transformation.SourceField("file")
	if !ok {
		t.Fatal("missing operational file field")
	}
	validation, ok := fieldValidation(field, ValidationMediaExtension)
	if !ok {
		t.Fatalf("file has no media-extension policy: %#v", field.Validations)
	}
	assertMediaExtensions(t, validation.MediaTypes, "document", nil, false)
}

func TestManualSpecCanSealCustomMediaSelector(t *testing.T) {
	transformation := FabricatorWorkbench()
	custom := []MediaExtensionPolicy{
		{MediaType: "model_3d", SelectExtensions: []string{"stl"}, AllowedExtensions: []string{"stl"}},
		{MediaType: "file", AllowedExtensions: []string{"zip"}, Fallback: true},
	}
	found := false
	for fieldIndex := range transformation.Source.Fields {
		field := &transformation.Source.Fields[fieldIndex]
		if field.Name != "file" {
			continue
		}
		for validationIndex := range field.Validations {
			if field.Validations[validationIndex].Rule == ValidationMediaExtension {
				field.Validations[validationIndex].MediaTypes = cloneMediaExtensionPolicies(custom)
				found = true
			}
		}
	}
	if !found {
		t.Fatal("built-in specification has no file media-extension validation")
	}
	transformation.Fingerprint = Fingerprint{}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	if err := transformation.ValidateSealed(); err != nil {
		t.Fatalf("custom media selector did not remain a valid sealed policy: %v", err)
	}
	field, _ := transformation.SourceField("file")
	validation, _ := fieldValidation(field, ValidationMediaExtension)
	assertMediaExtensions(t, validation.MediaTypes, "model_3d", []string{"stl"}, false)
}

func TestCompileDrupalFieldValidationsBindsDirectFileExtensions(t *testing.T) {
	for _, fieldType := range []string{"file", "image", "media_track"} {
		t.Run(fieldType, func(t *testing.T) {
			validations := compileDrupalFieldValidations(drupalAttachedField{
				storage: drupalStorageConfig{Type: fieldType},
				field:   drupalFieldConfig{Settings: map[string]any{"file_extensions": "VTT srt"}},
			})
			var policy Validation
			found := false
			for _, validation := range validations {
				if validation.Rule == ValidationMediaExtension {
					policy, found = validation, true
					break
				}
			}
			if !found || len(policy.MediaTypes) != 1 || !policy.MediaTypes[0].Fallback ||
				!reflect.DeepEqual(policy.MediaTypes[0].AllowedExtensions, []string{"srt", "vtt"}) {
				t.Fatalf("file extension validation = %#v", policy)
			}
		})
	}
}

func TestDrupalTaxonomyNameCreationPolicyIsExplicitAndSelectorSafe(t *testing.T) {
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion,
		System:  "drupal",
		Entities: []model.Entity{
			{EntityType: "node", Bundle: "islandora_object", Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "field_subject", SourceType: "entity_reference", Kind: model.ValueReference, Cardinality: -1,
					Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: []string{"subject"}}},
			}},
			{EntityType: "taxonomy_term", Bundle: "subject", Fields: []model.Field{
				{Path: "name", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			}},
		},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		allow bool
	}{
		{name: "strict", allow: false},
		{name: "allow names", allow: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			transformation, err := CompileDrupalModel(snapshot, DrupalCompileOptions{
				Bundle: "islandora_object", AllowNewTaxonomyTerms: test.allow,
			})
			if err != nil {
				t.Fatal(err)
			}
			field, ok := transformation.SourceField("field_subject")
			if !ok {
				t.Fatal("missing field_subject")
			}
			validation, ok := fieldValidation(field, ValidationContextEntityExists)
			if !ok || validation.AllowNewNames != test.allow {
				t.Fatalf("context validation = %#v, found=%v", validation, ok)
			}
		})
	}
}

func fieldValidation(field Field, rule ValidationRule) (Validation, bool) {
	for _, validation := range field.Validations {
		if validation.Rule == rule {
			return validation, true
		}
	}
	return Validation{}, false
}

func assertMediaExtensions(t *testing.T, policies []MediaExtensionPolicy, mediaType string, want []string, fallback bool) {
	t.Helper()
	for _, policy := range policies {
		if policy.MediaType != mediaType {
			continue
		}
		if !reflect.DeepEqual(policy.AllowedExtensions, want) || policy.Fallback != fallback {
			t.Fatalf("media type %s = %#v, want allowed=%#v fallback=%v", mediaType, policy, want, fallback)
		}
		return
	}
	t.Fatalf("missing media type policy %q in %#v", mediaType, policies)
}

func assertNoMediaType(t *testing.T, policies []MediaExtensionPolicy, mediaType string) {
	t.Helper()
	for _, policy := range policies {
		if policy.MediaType == mediaType {
			t.Fatalf("unexpected media type policy %q in %#v", mediaType, policies)
		}
	}
}
