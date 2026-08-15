package csv

import (
	"errors"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

func TestParseSpecNumericRangeRejectsNonFiniteAndOutOfBoundsValues(t *testing.T) {
	minimum, maximum := 0.0, 10.0
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "numeric-range",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{{
				Name: "weight",
				Hub:  "Extra.weight",
				Validations: []spec.Validation{{
					Rule: spec.ValidationNumericRange, Minimum: &minimum, Maximum: &maximum,
				}},
			}},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{{Name: "weight", Hub: "Extra.weight"}},
		},
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatalf("SealFingerprint() error = %v", err)
	}

	for _, value := range []string{"NaN", "+Inf", "-Inf", "-1", "11", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			_, err := (&Format{}).Parse(strings.NewReader("weight\n"+value+"\n"), &format.ParseOptions{
				Spec: transformation, Strict: true, SourceName: "numbers.csv",
			})
			var diagnostics *format.DiagnosticsError
			if !errors.As(err, &diagnostics) || len(diagnostics.Diagnostics) != 1 {
				t.Fatalf("Parse() error = %T %v, want one cell diagnostic", err, err)
			}
			if got := diagnostics.Diagnostics[0]; got.Code != string(spec.ValidationNumericRange) || got.Row != 2 || got.Column != 1 {
				t.Fatalf("numeric-range diagnostic = %+v", got)
			}
		})
	}

	for _, value := range []string{"0", "5.5", "10"} {
		t.Run("accepts_"+value, func(t *testing.T) {
			records, err := (&Format{}).Parse(strings.NewReader("weight\n"+value+"\n"), &format.ParseOptions{
				Spec: transformation, Strict: true,
			})
			if err != nil || len(records) != 1 {
				t.Fatalf("Parse() = %d records, %v", len(records), err)
			}
		})
	}
}

func TestParseSpecFiniteNumberRejectsNonNumericAndNonFiniteValues(t *testing.T) {
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "finite-number",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{{
				Name: "decimal", Hub: "Extra.decimal",
				Validations: []spec.Validation{{Rule: spec.ValidationFiniteNumber}},
			}},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{{Name: "decimal", Hub: "Extra.decimal"}},
		},
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatalf("SealFingerprint() error = %v", err)
	}

	for _, value := range []string{"not-a-number", "NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			_, err := (&Format{}).Parse(strings.NewReader("decimal\n"+value+"\n"), &format.ParseOptions{Spec: transformation, Strict: true})
			var diagnostics *format.DiagnosticsError
			if !errors.As(err, &diagnostics) || len(diagnostics.Diagnostics) != 1 || diagnostics.Diagnostics[0].Code != string(spec.ValidationFiniteNumber) {
				t.Fatalf("Parse() error = %T %v, want one finite-number diagnostic", err, err)
			}
		})
	}

	for _, value := range []string{"-10.25", "0", "1e6"} {
		t.Run("accepts_"+value, func(t *testing.T) {
			records, err := (&Format{}).Parse(strings.NewReader("decimal\n"+value+"\n"), &format.ParseOptions{Spec: transformation, Strict: true})
			if err != nil || len(records) != 1 {
				t.Fatalf("Parse() = %d records, %v", len(records), err)
			}
		})
	}
}

func TestParseSpecPatternUsesMappingDeclaredRegex(t *testing.T) {
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "pattern",
		Source: spec.Table{
			Format: "csv",
			Fields: []spec.Field{{
				Name: "accession", Hub: "Extra.accession",
				Validations: []spec.Validation{{Rule: spec.ValidationPattern, Pattern: `^[A-Z]{3}-[0-9]{6}$`}},
			}},
		},
		Target: spec.Table{
			Format: "islandora-workbench",
			Fields: []spec.Field{{Name: "accession", Hub: "Extra.accession"}},
		},
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatalf("SealFingerprint() error = %v", err)
	}

	for _, test := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid", value: "ABC-123456"},
		{name: "invalid", value: "abc-123456", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := (&Format{}).Parse(strings.NewReader("accession\n"+test.value+"\n"), &format.ParseOptions{Spec: transformation, Strict: true})
			if test.wantErr {
				var diagnostics *format.DiagnosticsError
				if !errors.As(err, &diagnostics) || len(diagnostics.Diagnostics) != 1 || diagnostics.Diagnostics[0].Code != string(spec.ValidationPattern) {
					t.Fatalf("Parse() error = %T %v, want one pattern diagnostic", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
		})
	}
}

func TestValidSpecWorkbenchLink(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "https://example.edu/item", want: true},
		{value: "https://example.edu/item%%Readable label", want: true},
		{value: "http://example.edu", want: true},
		{value: "/internal/path", want: false},
		{value: "https://user@example.edu/item", want: false},
		{value: "javascript:alert(1)", want: false},
	} {
		if got := validSpecWorkbenchLink(test.value); got != test.want {
			t.Errorf("validSpecWorkbenchLink(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestStructuredDrupalValueValidators(t *testing.T) {
	tests := []struct {
		name  string
		valid func(string) bool
		good  []string
		bad   []string
	}{
		{
			name: "geolocation", valid: validSpecGeolocation,
			good: []string{"40.6079,-75.3783", `\40.6079, -75.3783`, "-90,180"},
			bad:  []string{"91,0", "0,-181", "north,west", "40"},
		},
		{
			name: "authority link", valid: func(value string) bool { return validSpecAuthorityLink(value, []string{"lcsh", "viaf"}) },
			good: []string{"lcsh%%https://id.loc.gov/authorities/subjects/sh85000001", "viaf%%https://viaf.org/viaf/123%%Title"},
			bad:  []string{"aat%%https://vocab.getty.edu/aat/1", "lcsh%%/relative", "lcsh"},
		},
		{
			name: "media track", valid: validSpecMediaTrack,
			good: []string{"English captions:captions:en:captions.vtt", "Chapters:chapters:en-US:chapters.VTT"},
			bad:  []string{"captions:unknown:en:file.vtt", "captions:captions:en:file.srt", ":captions:en:file.vtt"},
		},
		{
			name: "entity reference", valid: func(value string) bool {
				return validSpecEntityReference(value, "taxonomy_term", []string{"collection", "subject"})
			},
			good: []string{"123", "collection:University Archives", "https://example.edu/term/1"},
			bad:  []string{"University Archives", "unknown:Term", "https://user@example.edu/term/1"},
		},
		{
			name: "typed relation", valid: func(value string) bool {
				return validSpecTypedRelation(value, []string{"relators:cre", "relators:edt"}, "taxonomy_term", []string{"person", "corporate_body"})
			},
			good: []string{"relators:cre:person:Example, Avery", "relators:edt:123"},
			bad:  []string{"relators:ths:person:Advisor", "bad shape", "relators:cre:unknown:Name"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range test.good {
				if !test.valid(value) {
					t.Errorf("valid value %q was rejected", value)
				}
			}
			for _, value := range test.bad {
				if test.valid(value) {
					t.Errorf("invalid value %q was accepted", value)
				}
			}
		})
	}
}

func TestSingleVocabularyEntityReferencesPreserveColonsInTermNames(t *testing.T) {
	for _, value := range []string{"History: 20th century", "subject:History: 20th century"} {
		if !validSpecEntityReference(value, "taxonomy_term", []string{"subject"}) {
			t.Errorf("single-vocabulary term %q was rejected", value)
		}
		if !validSpecTypedRelation("relators:cre:"+value, []string{"relators:cre"}, "taxonomy_term", []string{"subject"}) {
			t.Errorf("single-vocabulary typed relation target %q was rejected", value)
		}
	}
	if validSpecEntityReference("person name", "node", []string{"article"}) {
		t.Fatal("non-taxonomy entity reference accepted a nonnumeric name")
	}
}

func TestContributorValidatorUsesMappingDeclaredRelators(t *testing.T) {
	contributors := []*hubv1.Contributor{{Name: "Example, Person", RoleCode: "relators:ths"}}
	if message := validateSpecContributors(contributors, []string{"relators:cre"}); !strings.Contains(message, "not configured") {
		t.Fatalf("validateSpecContributors() = %q", message)
	}
	if message := validateSpecContributors(contributors, []string{"relators:cre", "relators:ths"}); message != "" {
		t.Fatalf("validateSpecContributors() = %q, want clean", message)
	}
}
