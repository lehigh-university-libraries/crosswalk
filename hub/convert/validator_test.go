package convert

import (
	"testing"
)

func TestValidatorRegistry_Register(t *testing.T) {
	r := NewValidatorRegistry()

	r.Register("test_validator", func(value any, opts *ValidatorOptions) error {
		if value == "invalid" {
			return &ValidationError{
				Field:   opts.FieldName,
				Value:   value,
				Rule:    "test",
				Message: "test failed",
			}
		}
		return nil
	})

	fn, ok := r.Get("test_validator")
	if !ok {
		t.Fatal("custom validator not found")
	}

	// Test valid value
	err := fn("valid", &ValidatorOptions{FieldName: "test"})
	if err != nil {
		t.Errorf("expected nil error for valid value, got %v", err)
	}

	// Test invalid value
	err = fn("invalid", &ValidatorOptions{FieldName: "test"})
	if err == nil {
		t.Error("expected error for invalid value")
	}
}

func TestValidatorRegistry_DefaultValidators(t *testing.T) {
	r := DefaultValidators()

	expectedValidators := []string{
		"required", "doi", "isbn", "issn", "orcid", "url", "email",
		"iso8601", "edtf", "year_range", "pattern", "length", "range", "count",
	}

	for _, name := range expectedValidators {
		if _, ok := r.Get(name); !ok {
			t.Errorf("default validator %q not found", name)
		}
	}
}

func TestValidateRequired(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		wantErr bool
	}{
		{"non-empty string", "hello", false},
		{"empty string", "", true},
		{"whitespace only", "   ", true},
		{"nil", nil, true},
		{"non-empty slice", []string{"a"}, false},
		{"empty slice", []string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequired(tt.value, &ValidatorOptions{FieldName: "test"})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRequired(%v) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateDOI(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid doi", "10.1234/foo.bar", false},
		{"valid complex", "10.1000/xyz123", false},
		{"with url prefix", "https://doi.org/10.1234/foo", false},
		{"invalid format", "not-a-doi", true},
		{"missing prefix", "1234/foo", true},
		{"empty", "", false}, // Empty is not invalid, use required for that
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDOI(tt.value, &ValidatorOptions{FieldName: "doi"})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDOI(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateISBN(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid isbn-10", "0306406152", false},
		{"valid isbn-13", "9780306406157", false},
		{"invalid checksum isbn-10", "0306406151", true},
		{"invalid checksum isbn-13", "9780306406158", true},
		{"wrong length", "12345", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateISBN(tt.value, &ValidatorOptions{FieldName: "isbn"})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateISBN(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateISSN(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid issn", "0378-5955", false},
		{"valid with X", "0317-8471", false},
		{"invalid checksum", "0378-5956", true},
		{"wrong format", "03785955", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateISSN(tt.value, &ValidatorOptions{FieldName: "issn"})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateISSN(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateORCID(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid orcid", "0000-0002-1825-0097", false},
		{"valid with X", "0000-0001-5109-3700", false},
		{"with url", "https://orcid.org/0000-0002-1825-0097", false},
		{"invalid checksum", "0000-0002-1825-0098", true},
		{"wrong format", "000000021825009", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateORCID(tt.value, &ValidatorOptions{FieldName: "orcid"})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateORCID(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"https url", "https://example.com", false},
		{"http url", "http://example.com/path", false},
		{"with params", "https://example.com?foo=bar", false},
		{"no protocol", "example.com", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.value, &ValidatorOptions{FieldName: "url"})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateURL(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid email", "user@example.com", false},
		{"with subdomain", "user@mail.example.com", false},
		{"with plus", "user+tag@example.com", false},
		{"missing @", "userexample.com", true},
		{"missing domain", "user@", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEmail(tt.value, &ValidatorOptions{FieldName: "email"})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEmail(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateISO8601(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"full date", "2023-05-15", false},
		{"year-month", "2023-05", false},
		{"year only", "2023", false},
		{"with time", "2023-05-15T10:30:00Z", false},
		{"invalid format", "15-05-2023", true},
		{"invalid date", "2023-13-01", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateISO8601(tt.value, &ValidatorOptions{FieldName: "date"})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateISO8601(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateYearRange(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		minValue int64
		maxValue int64
		wantErr  bool
	}{
		{"valid year", "2023", 0, 0, false},
		{"historical", "1850", 0, 0, false},
		{"too old", "0800", 0, 0, true},
		{"future", "2050", 0, 0, true},
		{"with custom range", "1950", 1900, 2000, false},
		{"outside custom range", "1850", 1900, 2000, true},
		{"empty", "", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &ValidatorOptions{
				FieldName: "year",
				MinValue:  tt.minValue,
				MaxValue:  tt.maxValue,
			}
			err := validateYearRange(tt.value, opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateYearRange(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePattern(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		pattern string
		wantErr bool
	}{
		{"matches", "ABC123", "^[A-Z]+[0-9]+$", false},
		{"no match", "abc123", "^[A-Z]+[0-9]+$", true},
		{"empty pattern", "anything", "", false},
		{"empty value", "", "^[A-Z]+$", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &ValidatorOptions{
				FieldName: "test",
				Pattern:   tt.pattern,
			}
			err := validatePattern(tt.value, opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePattern(%q, %q) error = %v, wantErr %v", tt.value, tt.pattern, err, tt.wantErr)
			}
		})
	}
}

func TestValidateLength(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		minLength int32
		maxLength int32
		wantErr   bool
	}{
		{"within range", "hello", 1, 10, false},
		{"exact min", "ab", 2, 10, false},
		{"exact max", "abcdefghij", 1, 10, false},
		{"too short", "a", 2, 10, true},
		{"too long", "abcdefghijk", 1, 10, true},
		{"no limits", "anything", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &ValidatorOptions{
				FieldName: "test",
				MinLength: tt.minLength,
				MaxLength: tt.maxLength,
			}
			err := validateLength(tt.value, opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateLength(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateAll(t *testing.T) {
	r := DefaultValidators()

	// Test with multiple validators
	errors := r.ValidateAll("required,doi", "10.1234/foo", &ValidatorOptions{FieldName: "doi"})
	if len(errors) != 0 {
		t.Errorf("expected no errors for valid DOI, got %v", errors)
	}

	// Test with failing validators
	errors = r.ValidateAll("required,doi", "not-a-doi", &ValidatorOptions{FieldName: "doi"})
	if len(errors) != 1 {
		t.Errorf("expected 1 error for invalid DOI, got %d", len(errors))
	}

	// Test with empty string (required fails, doi doesn't)
	errors = r.ValidateAll("required,doi", "", &ValidatorOptions{FieldName: "doi"})
	if len(errors) != 1 {
		t.Errorf("expected 1 error for empty value, got %d", len(errors))
	}
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{
		Field:   "test_field",
		Value:   "bad_value",
		Rule:    "test_rule",
		Message: "test message",
	}

	expected := `validation failed for field "test_field" (rule: test_rule): test message`
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}
