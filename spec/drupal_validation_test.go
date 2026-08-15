package spec

import (
	"math"
	"strings"
	"testing"
)

func TestCompileDrupalFieldValidationsUsesMachineSettingsNotLabels(t *testing.T) {
	config := drupalAttachedField{
		storage: drupalStorageConfig{
			Type: "list_string",
			Settings: map[string]any{
				"allowed_values": []any{
					map[string]any{"value": "staff", "label": "Staff only"},
					map[string]any{"value": "public", "label": "Public"},
				},
			},
		},
		field: drupalFieldConfig{Label: "A human label that may change"},
	}
	validations := compileDrupalFieldValidations(config)
	if len(validations) != 1 || validations[0].Rule != ValidationEnum || !equalStrings(validations[0].Values, []string{"public", "staff"}) {
		t.Fatalf("validations = %#v", validations)
	}

	config.field.Label = "A translated label"
	translated := compileDrupalFieldValidations(config)
	if len(translated) != 1 || !equalStrings(translated[0].Values, validations[0].Values) {
		t.Fatalf("label changed validations: %#v != %#v", translated, validations)
	}
}

func TestCompileDrupalFieldValidationsDeclaresDynamicAllowedValueContext(t *testing.T) {
	config := drupalAttachedField{
		storage: drupalStorageConfig{
			Type: "list_string",
			Settings: map[string]any{
				"allowed_values":          map[string]any{"public": "Public"},
				"allowed_values_function": "repository_allowed_values",
			},
		},
	}
	got := compileDrupalFieldValidations(config)
	if len(got) != 1 || got[0].Rule != ValidationContextAllowedValue || got[0].Phase != ValidationPhaseContext || got[0].Provider != "repository_allowed_values" {
		t.Fatalf("dynamic allowed value validation = %#v", got)
	}
}

func TestCompileDrupalFieldValidationsHonorsScalarTextMaximumLength(t *testing.T) {
	tests := []struct {
		name      string
		fieldType string
		maxLength any
		wantLimit int
	}{
		{name: "string", fieldType: "string", maxLength: 255, wantLimit: 255},
		{name: "long string", fieldType: "string_long", maxLength: "500", wantLimit: 500},
		{name: "textfield string", fieldType: "string_textfield", maxLength: 255, wantLimit: 255},
		{name: "starter site FITS MD5 text", fieldType: "text", maxLength: 255, wantLimit: 255},
		{name: "long text", fieldType: "text_long", maxLength: 1024, wantLimit: 1024},
		{name: "text with summary", fieldType: "text_with_summary", maxLength: 1024, wantLimit: 1024},
		{name: "string list", fieldType: "list_string", maxLength: 64, wantLimit: 64},
		{name: "email", fieldType: "email", maxLength: 254, wantLimit: 254},
		{name: "telephone", fieldType: "telephone", maxLength: 32, wantLimit: 32},
		{name: "date and time", fieldType: "datetime", maxLength: 32, wantLimit: 32},
		{name: "date range", fieldType: "daterange", maxLength: 64, wantLimit: 64},
		{name: "starter site EDTF date issued", fieldType: "edtf", maxLength: 128, wantLimit: 128},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := drupalAttachedField{
				storage: drupalStorageConfig{
					Type:     test.fieldType,
					Settings: map[string]any{"max_length": test.maxLength},
				},
			}
			validations := compileDrupalFieldValidations(config)
			for _, validation := range validations {
				if validation.Rule == ValidationMaximumRunes {
					if validation.Limit != test.wantLimit {
						t.Fatalf("maximum-runes limit = %d, want %d", validation.Limit, test.wantLimit)
					}
					return
				}
			}
			t.Fatalf("missing %q in %#v", ValidationMaximumRunes, validations)
		})
	}
}

func TestCompileDrupalFieldValidationsDoesNotApplyMaximumLengthToStructuredOrNonTextFields(t *testing.T) {
	fieldTypes := []string{
		"boolean", "integer", "list_integer", "decimal", "float",
		"link", "geolocation", "authority_link", "media_track",
		"entity_reference", "typed_relation", "textfield_attr", "textarea_attr", "part_detail", "related_item",
	}

	for _, fieldType := range fieldTypes {
		t.Run(fieldType, func(t *testing.T) {
			config := drupalAttachedField{
				storage: drupalStorageConfig{
					Type:     fieldType,
					Settings: map[string]any{"max_length": 255},
				},
			}
			for _, validation := range compileDrupalFieldValidations(config) {
				if validation.Rule == ValidationMaximumRunes {
					t.Fatalf("structured or non-text field compiled maximum-runes validation: %#v", validation)
				}
			}
		})
	}
}

func TestCompileDrupalFieldValidationsIgnoresInvalidMaximumLength(t *testing.T) {
	for _, maxLength := range []any{nil, 0, -1, 1.5, "", "unlimited"} {
		config := drupalAttachedField{
			storage: drupalStorageConfig{
				Type:     "text",
				Settings: map[string]any{"max_length": maxLength},
			},
		}
		for _, validation := range compileDrupalFieldValidations(config) {
			if validation.Rule == ValidationMaximumRunes {
				t.Fatalf("max_length %#v compiled maximum-runes validation: %#v", maxLength, validation)
			}
		}
	}
}

func TestCompileDrupalFieldValidationsHonorsLinkAndNumericSettings(t *testing.T) {
	external := drupalAttachedField{
		storage: drupalStorageConfig{Type: "link"},
		field:   drupalFieldConfig{Settings: map[string]any{"link_type": 16}},
	}
	if got := compileDrupalFieldValidations(external); len(got) != 1 || got[0].Rule != ValidationWorkbenchLink {
		t.Fatalf("external link validations = %#v", got)
	}
	generic := external
	generic.field.Settings = map[string]any{"link_type": 17}
	if got := compileDrupalFieldValidations(generic); len(got) != 1 || got[0].Rule != ValidationWorkbenchLink {
		t.Fatalf("generic Workbench link validations = %#v", got)
	}

	unsigned := drupalAttachedField{
		storage: drupalStorageConfig{Type: "integer", Settings: map[string]any{"unsigned": true}},
	}
	got := compileDrupalFieldValidations(unsigned)
	if len(got) != 1 || got[0].Rule != ValidationNumericRange || got[0].Minimum == nil || *got[0].Minimum != 0 || got[0].Maximum != nil {
		t.Fatalf("unsigned integer validations = %#v", got)
	}

	decimal := drupalAttachedField{storage: drupalStorageConfig{Type: "decimal"}}
	got = compileDrupalFieldValidations(decimal)
	if len(got) != 1 || got[0].Rule != ValidationFiniteNumber {
		t.Fatalf("unbounded decimal validations = %#v", got)
	}
}

func TestCompileDrupalFieldValidationsDerivesStructuredFieldCriteria(t *testing.T) {
	tests := []struct {
		name   string
		config drupalAttachedField
		rule   ValidationRule
		check  func(Validation) bool
	}{
		{name: "geolocation", config: drupalAttachedField{storage: drupalStorageConfig{Type: "geolocation"}}, rule: ValidationGeolocation},
		{
			name: "authority link",
			config: drupalAttachedField{
				storage: drupalStorageConfig{Type: "authority_link"},
				field:   drupalFieldConfig{Settings: map[string]any{"authority_sources": map[string]any{"viaf": "VIAF", "lcsh": "LCSH"}}},
			},
			rule:  ValidationAuthorityLink,
			check: func(validation Validation) bool { return equalStrings(validation.Values, []string{"lcsh", "viaf"}) },
		},
		{name: "media track", config: drupalAttachedField{storage: drupalStorageConfig{Type: "media_track"}}, rule: ValidationMediaTrack},
		{
			name: "entity reference",
			config: drupalAttachedField{
				storage: drupalStorageConfig{Type: "entity_reference", Settings: map[string]any{"target_type": "taxonomy_term"}},
				field:   drupalFieldConfig{Settings: map[string]any{"handler_settings": map[string]any{"target_bundles": map[string]any{"subject": "subject", "collection": "collection"}}}},
			},
			rule: ValidationEntityReference,
			check: func(validation Validation) bool {
				return equalStrings(validation.Bundles, []string{"collection", "subject"})
			},
		},
		{
			name: "typed relation",
			config: drupalAttachedField{
				storage: drupalStorageConfig{Type: "typed_relation", Settings: map[string]any{"target_type": "taxonomy_term"}},
				field: drupalFieldConfig{Settings: map[string]any{
					"rel_types":        []any{"relators:edt", "relators:cre"},
					"handler_settings": map[string]any{"target_bundles": map[string]any{"person": "person"}},
				}},
			},
			rule: ValidationTypedRelation,
			check: func(validation Validation) bool {
				return equalStrings(validation.Values, []string{"relators:cre", "relators:edt"}) && equalStrings(validation.Bundles, []string{"person"})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validations := compileDrupalFieldValidations(test.config)
			for _, validation := range validations {
				if validation.Rule == test.rule {
					if test.check != nil && !test.check(validation) {
						t.Fatalf("validation = %#v", validation)
					}
					return
				}
			}
			t.Fatalf("missing %q in %#v", test.rule, validations)
		})
	}
}

func TestCompileDrupalFieldValidationsDeclaresExactCoreDefaultHandler(t *testing.T) {
	validations := compileDrupalFieldValidations(drupalAttachedField{
		storage: drupalStorageConfig{Type: "entity_reference", Settings: map[string]any{"target_type": "taxonomy_term"}},
		field: drupalFieldConfig{Settings: map[string]any{
			"handler_settings": map[string]any{"target_bundles": map[string]any{"subject": "subject"}},
		}},
	})
	for _, validation := range validations {
		if validation.Rule == ValidationContextEntityExists {
			if validation.Handler != "default:taxonomy_term" {
				t.Fatalf("context handler = %q, want default:taxonomy_term", validation.Handler)
			}
			return
		}
	}
	t.Fatalf("missing context entity validation in %#v", validations)
}

func TestApplyDrupalFieldConfigUsesTypedRelationRelatorsForContributorColumns(t *testing.T) {
	field := Field{
		Name: "field_linked_agent.name", Hub: "Contributors.Name", Codec: "contributors",
		Validations: []Validation{{Rule: ValidationContributor}},
	}
	config := drupalAttachedField{
		storage: drupalStorageConfig{Type: "typed_relation", Settings: map[string]any{"target_type": "taxonomy_term"}},
		field: drupalFieldConfig{Settings: map[string]any{
			"rel_types":        []any{"relators:ths", "relators:cre"},
			"handler_settings": map[string]any{"target_bundles": map[string]any{"person": "person"}},
		}},
	}
	got := applyDrupalFieldConfig(field, config, false, false)
	for _, validation := range got.Validations {
		if validation.Rule == ValidationContributor {
			if !equalStrings(validation.Values, []string{"relators:cre", "relators:ths"}) {
				t.Fatalf("contributor validation = %#v", validation)
			}
			return
		}
	}
	t.Fatalf("contributor validations = %#v", got.Validations)
}

func TestNumericRangeValidationContract(t *testing.T) {
	tests := []struct {
		name       string
		validation Validation
		want       string
	}{
		{name: "missing bounds", validation: Validation{Rule: ValidationNumericRange}, want: "requires a minimum or maximum"},
		{name: "reversed", validation: Validation{Rule: ValidationNumericRange, Minimum: float64Pointer(2), Maximum: float64Pointer(1)}, want: "minimum exceeds maximum"},
		{name: "non-finite", validation: Validation{Rule: ValidationNumericRange, Minimum: float64Pointer(math.Inf(1))}, want: "non-finite minimum"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transformation := FabricatorWorkbench()
			transformation.Fingerprint = Fingerprint{}
			for index := range transformation.Source.Fields {
				if transformation.Source.Fields[index].Name == "field_weight" {
					transformation.Source.Fields[index].Validations = append(transformation.Source.Fields[index].Validations, test.validation)
					break
				}
			}
			err := transformation.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}
