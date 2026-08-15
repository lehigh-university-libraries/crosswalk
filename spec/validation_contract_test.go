package spec

import (
	"math"
	"strings"
	"testing"
)

func validationContractDraft() *Transformation {
	return &Transformation{
		Version: CurrentVersion,
		Name:    "validation-contract",
		Source: Table{
			Format: "csv",
			Fields: []Field{
				{Name: "title", Label: "Human Title", Hub: "Title"},
				{Name: "model", Hub: "ObjectModel"},
				{Name: "file", Hub: "Files.primary"},
			},
		},
		Target: Table{
			Format: "islandora-workbench",
			Fields: []Field{{Name: "title", Hub: "Title"}},
		},
	}
}

func TestValidationContractRejectsMalformedFieldRules(t *testing.T) {
	minimum := 1.0
	maximum := 2.0
	nan := math.NaN()
	infinity := math.Inf(1)

	tests := []struct {
		name       string
		validation Validation
		second     *Validation
		want       string
	}{
		{name: "unsupported rule", validation: Validation{Rule: "invented"}, want: `unsupported validation rule "invented"`},
		{name: "context rule needs context phase", validation: Validation{Rule: ValidationContextNodeExists}, want: "must use context phase"},
		{name: "entity reference needs entity type", validation: Validation{Rule: ValidationEntityReference}, want: "requires a valid entity_type"},
		{name: "typed relation needs entity type", validation: Validation{Rule: ValidationTypedRelation}, want: "requires a valid entity_type"},
		{name: "entity context needs entity type", validation: Validation{Rule: ValidationContextEntityExists, Phase: ValidationPhaseContext}, want: "requires a valid entity_type"},
		{name: "entity context rejects unsafe entity type", validation: Validation{Rule: ValidationContextEntityExists, Phase: ValidationPhaseContext, EntityType: "Taxonomy Term"}, want: "requires a valid entity_type"},
		{name: "entity context rejects padded handler", validation: Validation{Rule: ValidationContextEntityExists, Phase: ValidationPhaseContext, EntityType: "taxonomy_term", Handler: " default:taxonomy_term"}, want: "has an invalid handler"},
		{name: "entity context rejects newline handler", validation: Validation{Rule: ValidationContextEntityExists, Phase: ValidationPhaseContext, EntityType: "taxonomy_term", Handler: "default:\ntaxonomy_term"}, want: "has an invalid handler"},
		{name: "entity context rejects overlong handler", validation: Validation{Rule: ValidationContextEntityExists, Phase: ValidationPhaseContext, EntityType: "taxonomy_term", Handler: strings.Repeat("a", 257)}, want: "has an invalid handler"},
		{name: "allowed value context needs provider", validation: Validation{Rule: ValidationContextAllowedValue, Phase: ValidationPhaseContext}, want: "requires a valid provider"},
		{name: "allowed value context rejects unsafe provider", validation: Validation{Rule: ValidationContextAllowedValue, Phase: ValidationPhaseContext, Provider: "bad\nprovider"}, want: "requires a valid provider"},
		{name: "unexpected entity type", validation: Validation{Rule: ValidationDOI, EntityType: "node"}, want: "does not accept an entity_type"},
		{name: "unexpected provider", validation: Validation{Rule: ValidationDOI, Provider: "callback"}, want: "does not accept a provider"},
		{name: "unexpected handler", validation: Validation{Rule: ValidationDOI, Handler: "default:taxonomy_term"}, want: "does not accept a handler"},
		{name: "deterministic rule rejects context phase", validation: Validation{Rule: ValidationDOI, Phase: ValidationPhaseContext}, want: "must use deterministic phase"},
		{name: "unknown operation", validation: Validation{Rule: ValidationDOI, Operations: []Operation{"delete"}}, want: `unsupported operation "delete"`},
		{name: "duplicate operation", validation: Validation{Rule: ValidationDOI, Operations: []Operation{OperationCreate, OperationCreate}}, want: `repeats operation "create"`},
		{name: "unexpected singular field", validation: Validation{Rule: ValidationDOI, Field: "model"}, want: "does not accept a field reference"},
		{name: "unexpected field list", validation: Validation{Rule: ValidationDOI, Fields: []string{"model"}}, want: "does not accept field lists"},
		{name: "media extension rejects field selector", validation: Validation{Rule: ValidationMediaExtension, Field: "model"}, want: "does not accept a field reference"},
		{name: "media extension needs policies", validation: Validation{Rule: ValidationMediaExtension}, want: "requires at least one media type policy"},
		{name: "media extension needs one fallback", validation: Validation{Rule: ValidationMediaExtension, MediaTypes: []MediaExtensionPolicy{{MediaType: "file"}}}, want: "requires exactly one fallback"},
		{name: "media extension rejects duplicate selector", validation: Validation{Rule: ValidationMediaExtension, MediaTypes: []MediaExtensionPolicy{{MediaType: "file", SelectExtensions: []string{"pdf"}, Fallback: true}, {MediaType: "document", SelectExtensions: []string{"pdf"}}}}, want: `selector extension "pdf" is shared`},
		{name: "media extension rejects noncanonical extension", validation: Validation{Rule: ValidationMediaExtension, MediaTypes: []MediaExtensionPolicy{{MediaType: "file", AllowedExtensions: []string{"PDF"}, Fallback: true}}}, want: `invalid allowed extension "PDF"`},
		{name: "unexpected media policy", validation: Validation{Rule: ValidationDOI, MediaTypes: []MediaExtensionPolicy{{MediaType: "file", Fallback: true}}}, want: "does not accept media types"},
		{name: "new names requires taxonomy context", validation: Validation{Rule: ValidationDOI, AllowNewNames: true}, want: "allow_new_names requires"},
		{name: "maximum runes needs positive limit", validation: Validation{Rule: ValidationMaximumRunes}, want: "requires a positive limit"},
		{name: "unexpected limit", validation: Validation{Rule: ValidationDOI, Limit: 1}, want: "does not accept a limit"},
		{name: "numeric range needs a bound", validation: Validation{Rule: ValidationNumericRange}, want: "requires a minimum or maximum"},
		{name: "numeric range rejects NaN", validation: Validation{Rule: ValidationNumericRange, Minimum: &nan}, want: "non-finite minimum"},
		{name: "numeric range rejects infinity", validation: Validation{Rule: ValidationNumericRange, Maximum: &infinity}, want: "non-finite maximum"},
		{name: "numeric range orders bounds", validation: Validation{Rule: ValidationNumericRange, Minimum: &maximum, Maximum: &minimum}, want: "minimum exceeds maximum"},
		{name: "unexpected numeric bound", validation: Validation{Rule: ValidationDOI, Minimum: &minimum}, want: "does not accept numeric bounds"},
		{name: "pattern required", validation: Validation{Rule: ValidationPattern}, want: "requires a fully anchored pattern"},
		{name: "pattern must be anchored", validation: Validation{Rule: ValidationPattern, Pattern: `[A-Z]+`}, want: "requires a fully anchored pattern"},
		{name: "pattern must compile", validation: Validation{Rule: ValidationPattern, Pattern: `^[$`}, want: "has invalid pattern"},
		{name: "unexpected pattern", validation: Validation{Rule: ValidationDOI, Pattern: `^x$`}, want: "does not accept a pattern"},
		{name: "enum needs values", validation: Validation{Rule: ValidationEnum}, want: "requires values"},
		{name: "enum rejects blank value", validation: Validation{Rule: ValidationEnum, Values: []string{"Yes", " "}}, want: "has an empty value"},
		{name: "case insensitive enum rejects duplicates", validation: Validation{Rule: ValidationEnum, Values: []string{"Yes", "yes"}, CaseInsensitive: true}, want: `repeats value "yes"`},
		{name: "unexpected enum values", validation: Validation{Rule: ValidationDOI, Values: []string{"value"}}, want: "does not accept enum values"},
		{name: "authority link needs sources", validation: Validation{Rule: ValidationAuthorityLink}, want: "requires values"},
		{name: "authority link values are exact", validation: Validation{Rule: ValidationAuthorityLink, Values: []string{"lcsh"}, CaseInsensitive: true}, want: "requires exact values"},
		{name: "unexpected bundles", validation: Validation{Rule: ValidationDOI, Bundles: []string{"subjects"}}, want: "does not accept bundles"},
		{name: "blank bundle", validation: Validation{Rule: ValidationEntityReference, EntityType: "taxonomy_term", Bundles: []string{" "}}, want: "has invalid bundle"},
		{name: "duplicate bundle", validation: Validation{Rule: ValidationTypedRelation, EntityType: "taxonomy_term", Bundles: []string{"person", "person"}}, want: `repeats bundle "person"`},
		{name: "duplicate rule", validation: Validation{Rule: ValidationDOI}, second: &Validation{Rule: ValidationDOI}, want: `repeats validation rule "doi"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			draft := validationContractDraft()
			draft.Source.Fields[0].Validations = []Validation{test.validation}
			if test.second != nil {
				draft.Source.Fields[0].Validations = append(draft.Source.Fields[0].Validations, *test.second)
			}
			if err := draft.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidationContractRejectsMalformedTableRules(t *testing.T) {
	tests := []struct {
		name       string
		validation Validation
		want       string
	}{
		{name: "unsupported rule", validation: Validation{Rule: "invented", Fields: []string{"title"}}, want: `unsupported rule "invented"`},
		{name: "singular field", validation: Validation{Rule: ValidationUnique, Field: "title", Fields: []string{"title"}}, want: "does not accept a singular field"},
		{name: "unexpected limit", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, Limit: 1}, want: "does not accept a limit"},
		{name: "unexpected numeric bounds", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, Minimum: float64ContractPointer(1)}, want: "does not accept numeric bounds"},
		{name: "unexpected pattern", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, Pattern: `^x$`}, want: "does not accept a pattern"},
		{name: "unexpected enum values", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, Values: []string{"x"}}, want: "does not accept enum values"},
		{name: "unexpected case insensitive flag", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, CaseInsensitive: true}, want: "does not accept enum values"},
		{name: "unexpected bundles", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, Bundles: []string{"subject"}}, want: "does not accept bundles"},
		{name: "unique field count", validation: Validation{Rule: ValidationUnique}, want: "expected exactly 1"},
		{name: "reference field count", validation: Validation{Rule: ValidationReference, Fields: []string{"title"}}, want: "expected exactly 2"},
		{name: "preceding reference field count", validation: Validation{Rule: ValidationPrecedingReference, Fields: []string{"title"}}, want: "expected exactly 2"},
		{name: "required any field count", validation: Validation{Rule: ValidationRequiredAny, Fields: []string{"title"}}, want: "expected at least 2"},
		{name: "required when field count", validation: Validation{Rule: ValidationRequiredWhen, Fields: []string{"title", "model"}, When: &ValidationWhen{Field: "model", Operator: ValidationOperatorIn, Values: []string{"Page"}}}, want: "expected exactly 1"},
		{name: "unknown field", validation: Validation{Rule: ValidationUnique, Fields: []string{"missing"}}, want: `references unknown field "missing"`},
		{name: "label is not canonical", validation: Validation{Rule: ValidationUnique, Fields: []string{"Human Title"}}, want: `must reference canonical field name "title"`},
		{name: "duplicate fields", validation: Validation{Rule: ValidationDifferent, Fields: []string{"title", "title"}}, want: `repeats field "title"`},
		{name: "required when needs predicate", validation: Validation{Rule: ValidationRequiredWhen, Fields: []string{"title"}}, want: "requires a when predicate"},
		{name: "context phase is rejected", validation: Validation{Rule: ValidationUnique, Phase: ValidationPhaseContext, Fields: []string{"title"}}, want: "must use deterministic phase"},
		{name: "unknown predicate field", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, When: &ValidationWhen{Field: "missing", Operator: ValidationOperatorIn, Values: []string{"x"}}}, want: `references unknown field "missing"`},
		{name: "predicate label is not canonical", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, When: &ValidationWhen{Field: "Human Title", Operator: ValidationOperatorIn, Values: []string{"x"}}}, want: `must reference canonical field name "title"`},
		{name: "unknown predicate operator", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, When: &ValidationWhen{Field: "model", Operator: "equals"}}, want: `unsupported predicate operator "equals"`},
		{name: "in predicate needs values", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, When: &ValidationWhen{Field: "model", Operator: ValidationOperatorIn}}, want: "requires values and no count"},
		{name: "in predicate rejects count", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, When: &ValidationWhen{Field: "model", Operator: ValidationOperatorIn, Values: []string{"x"}, Count: 1}}, want: "requires values and no count"},
		{name: "value count rejects values", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, When: &ValidationWhen{Field: "model", Operator: ValidationOperatorValueCountGreaterThan, Values: []string{"x"}}}, want: "requires a non-negative count and no values"},
		{name: "value count rejects negative", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, When: &ValidationWhen{Field: "model", Operator: ValidationOperatorValueCountGreaterThan, Count: -1}}, want: "requires a non-negative count and no values"},
		{name: "duplicate operation", validation: Validation{Rule: ValidationUnique, Fields: []string{"title"}, Operations: []Operation{OperationCreate, OperationCreate}}, want: `repeats operation "create"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			draft := validationContractDraft()
			draft.Source.Validations = []Validation{test.validation}
			if err := draft.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func float64ContractPointer(value float64) *float64 {
	return &value
}

func TestValidationContractAcceptsWellFormedRules(t *testing.T) {
	minimum, maximum := 0.0, 10.0
	draft := validationContractDraft()
	draft.Source.Fields[0].Validations = []Validation{
		{Rule: ValidationMaximumRunes, Limit: 255},
		{Rule: ValidationEnum, Values: []string{"Draft", "Published"}},
		{Rule: ValidationNumericRange, Minimum: &minimum, Maximum: &maximum},
		{Rule: ValidationPattern, Pattern: `^.+$`},
		{Rule: ValidationWorkbenchLink},
		{Rule: ValidationGeolocation},
		{Rule: ValidationAuthorityLink, Values: []string{"lcsh", "viaf"}},
		{Rule: ValidationMediaTrack},
		{Rule: ValidationEntityReference, EntityType: "node", Bundles: []string{"collection"}},
		{Rule: ValidationTypedRelation, EntityType: "taxonomy_term", Values: []string{"relators:cre"}, Bundles: []string{"person"}},
	}
	draft.Source.Fields[2].Validations = []Validation{
		{Rule: ValidationMediaExtension, MediaTypes: []MediaExtensionPolicy{{MediaType: "file", SelectExtensions: []string{"txt"}, AllowedExtensions: []string{"txt", "pdf"}, Fallback: true}}},
		{Rule: ValidationContextFileReadable, Phase: ValidationPhaseContext},
		{Rule: ValidationContextEntityExists, Phase: ValidationPhaseContext, EntityType: "taxonomy_term", Bundles: []string{"subject"}, Handler: "default:taxonomy_term", AllowNewNames: true},
		{Rule: ValidationContextAllowedValue, Phase: ValidationPhaseContext, Provider: "repository_allowed_values"},
	}
	draft.Source.Validations = []Validation{
		{Rule: ValidationUnique, Fields: []string{"title"}, Operations: []Operation{OperationCreate}},
		{Rule: ValidationRequiredAny, Fields: []string{"title", "model"}, When: &ValidationWhen{Field: "model", Operator: ValidationOperatorIn, Values: []string{"Page"}}},
		{Rule: ValidationRequiredWhen, Fields: []string{"title"}, When: &ValidationWhen{Field: "file", Operator: ValidationOperatorValueCountGreaterThan, Count: 0}},
	}

	if err := draft.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
