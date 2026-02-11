package hub

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// ValidationError represents a validation failure with context.
type ValidationError struct {
	Field   string // Field path (e.g., "contributors[0].name")
	Code    string // Error code (e.g., "required", "invalid_format")
	Message string // Human-readable message
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationResult contains all validation errors for a record.
type ValidationResult struct {
	Errors   []ValidationError
	Warnings []ValidationError // Non-fatal issues (e.g., data in extras that could be promoted)
}

// IsValid returns true if there are no errors.
func (r *ValidationResult) IsValid() bool {
	return len(r.Errors) == 0
}

// HasWarnings returns true if there are warnings.
func (r *ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// Error returns a combined error message, or nil if valid.
func (r *ValidationResult) Error() error {
	if r.IsValid() {
		return nil
	}
	var msgs []string
	for _, e := range r.Errors {
		msgs = append(msgs, e.Error())
	}
	return fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
}

// ValidationOptions configures validation behavior.
type ValidationOptions struct {
	// RequireTitle requires a non-empty title
	RequireTitle bool
	// RequireIdentifier requires at least one identifier
	RequireIdentifier bool
	// RequireContributor requires at least one contributor
	RequireContributor bool
	// RequireDate requires at least one date
	RequireDate bool
	// StrictExtras warns about commonly-used extras fields that should be promoted
	StrictExtras bool
	// ValidateIdentifierFormats checks identifier format validity (DOI, ORCID, etc.)
	ValidateIdentifierFormats bool
	// ValidateDates checks date value validity
	ValidateDates bool
}

// DefaultValidationOptions returns standard validation options.
func DefaultValidationOptions() ValidationOptions {
	return ValidationOptions{
		RequireTitle:              true,
		RequireIdentifier:         false,
		RequireContributor:        false,
		RequireDate:               false,
		StrictExtras:              true,
		ValidateIdentifierFormats: true,
		ValidateDates:             true,
	}
}

// StrictValidationOptions returns strict validation for production use.
func StrictValidationOptions() ValidationOptions {
	return ValidationOptions{
		RequireTitle:              true,
		RequireIdentifier:         true,
		RequireContributor:        true,
		RequireDate:               true,
		StrictExtras:              true,
		ValidateIdentifierFormats: true,
		ValidateDates:             true,
	}
}

// Validate validates a Hub record according to the given options.
func Validate(record *hubv1.Record, opts ValidationOptions) *ValidationResult {
	result := &ValidationResult{}

	// Required field checks
	if opts.RequireTitle && strings.TrimSpace(record.GetTitle()) == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "title",
			Code:    "required",
			Message: "title is required",
		})
	}

	if opts.RequireIdentifier && len(record.GetIdentifiers()) == 0 {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "identifiers",
			Code:    "required",
			Message: "at least one identifier is required",
		})
	}

	if opts.RequireContributor && len(record.GetContributors()) == 0 {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "contributors",
			Code:    "required",
			Message: "at least one contributor is required",
		})
	}

	if opts.RequireDate && len(record.GetDates()) == 0 {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "dates",
			Code:    "required",
			Message: "at least one date is required",
		})
	}

	// Validate identifiers
	if opts.ValidateIdentifierFormats {
		for i, id := range record.GetIdentifiers() {
			if errs := validateIdentifier(id, i); len(errs) > 0 {
				result.Errors = append(result.Errors, errs...)
			}
		}
	}

	// Validate contributors
	for i, contrib := range record.GetContributors() {
		if errs := validateContributor(contrib, i); len(errs) > 0 {
			result.Errors = append(result.Errors, errs...)
		}
	}

	// Validate dates
	if opts.ValidateDates {
		for i, date := range record.GetDates() {
			if errs := validateDate(date, i); len(errs) > 0 {
				result.Errors = append(result.Errors, errs...)
			}
		}
	}

	// Check extras for promotion candidates
	if opts.StrictExtras && record.GetExtra() != nil {
		warnings := checkExtrasForPromotion(record.GetExtra())
		result.Warnings = append(result.Warnings, warnings...)
	}

	return result
}

// Identifier format patterns
var (
	doiPattern   = regexp.MustCompile(`^10\.\d{4,}/[^\s]+$`)
	orcidPattern = regexp.MustCompile(`^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$`)
	issnPattern  = regexp.MustCompile(`^\d{4}-\d{3}[\dX]$`)
)

func validateIdentifier(id *hubv1.Identifier, index int) []ValidationError {
	var errs []ValidationError
	field := fmt.Sprintf("identifiers[%d]", index)

	if strings.TrimSpace(id.GetValue()) == "" {
		errs = append(errs, ValidationError{
			Field:   field + ".value",
			Code:    "required",
			Message: "identifier value is required",
		})
		return errs
	}

	// Validate format based on type
	value := strings.TrimSpace(id.GetValue())
	switch id.GetType() {
	case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
		// Strip common prefixes
		value = strings.TrimPrefix(value, "https://doi.org/")
		value = strings.TrimPrefix(value, "http://doi.org/")
		value = strings.TrimPrefix(value, "doi:")
		if !doiPattern.MatchString(value) {
			errs = append(errs, ValidationError{
				Field:   field + ".value",
				Code:    "invalid_format",
				Message: fmt.Sprintf("invalid DOI format: %s (expected 10.XXXX/...)", id.GetValue()),
			})
		}

	case hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID:
		// Strip common prefixes
		value = strings.TrimPrefix(value, "https://orcid.org/")
		value = strings.TrimPrefix(value, "http://orcid.org/")
		if !orcidPattern.MatchString(value) {
			errs = append(errs, ValidationError{
				Field:   field + ".value",
				Code:    "invalid_format",
				Message: fmt.Sprintf("invalid ORCID format: %s (expected XXXX-XXXX-XXXX-XXXX)", id.GetValue()),
			})
		}

	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
		value = strings.ReplaceAll(value, "-", "")
		if len(value) == 8 {
			value = value[:4] + "-" + value[4:]
		}
		if !issnPattern.MatchString(value) {
			errs = append(errs, ValidationError{
				Field:   field + ".value",
				Code:    "invalid_format",
				Message: fmt.Sprintf("invalid ISSN format: %s (expected XXXX-XXXX)", id.GetValue()),
			})
		}

	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
		// Remove hyphens and spaces for validation
		cleaned := strings.ReplaceAll(strings.ReplaceAll(value, "-", ""), " ", "")
		if len(cleaned) != 10 && len(cleaned) != 13 {
			errs = append(errs, ValidationError{
				Field:   field + ".value",
				Code:    "invalid_format",
				Message: fmt.Sprintf("invalid ISBN format: %s (expected 10 or 13 digits)", id.GetValue()),
			})
		}
	}

	return errs
}

func validateContributor(contrib *hubv1.Contributor, index int) []ValidationError {
	var errs []ValidationError
	field := fmt.Sprintf("contributors[%d]", index)

	// Must have either name or parsed_name
	hasName := strings.TrimSpace(contrib.GetName()) != ""
	hasParsedName := contrib.GetParsedName() != nil &&
		(strings.TrimSpace(contrib.GetParsedName().GetFamily()) != "" ||
			strings.TrimSpace(contrib.GetParsedName().GetGiven()) != "")

	if !hasName && !hasParsedName {
		errs = append(errs, ValidationError{
			Field:   field,
			Code:    "required",
			Message: "contributor must have name or parsed_name",
		})
	}

	// Validate contributor identifiers (e.g., ORCID)
	for i, id := range contrib.GetIdentifiers() {
		subErrs := validateIdentifier(id, i)
		for _, e := range subErrs {
			e.Field = field + "." + e.Field
			errs = append(errs, e)
		}
	}

	return errs
}

func validateDate(date *hubv1.DateValue, index int) []ValidationError {
	var errs []ValidationError
	field := fmt.Sprintf("dates[%d]", index)

	// Must have at least a year
	if date.GetYear() == 0 {
		// Check if there's a raw value we can parse
		if strings.TrimSpace(date.GetRaw()) == "" {
			errs = append(errs, ValidationError{
				Field:   field,
				Code:    "required",
				Message: "date must have year or raw value",
			})
		}
		return errs
	}

	// Validate year range (reasonable bounds)
	year := date.GetYear()
	currentYear := int32(time.Now().Year())
	if year < 1000 || year > currentYear+10 {
		errs = append(errs, ValidationError{
			Field:   field + ".year",
			Code:    "out_of_range",
			Message: fmt.Sprintf("year %d is outside reasonable range (1000-%d)", year, currentYear+10),
		})
	}

	// Validate month
	if date.GetMonth() != 0 && (date.GetMonth() < 1 || date.GetMonth() > 12) {
		errs = append(errs, ValidationError{
			Field:   field + ".month",
			Code:    "out_of_range",
			Message: fmt.Sprintf("month %d is invalid (must be 1-12)", date.GetMonth()),
		})
	}

	// Validate day
	if date.GetDay() != 0 && (date.GetDay() < 1 || date.GetDay() > 31) {
		errs = append(errs, ValidationError{
			Field:   field + ".day",
			Code:    "out_of_range",
			Message: fmt.Sprintf("day %d is invalid (must be 1-31)", date.GetDay()),
		})
	}

	return errs
}

// Fields that commonly appear in extras and should be considered for promotion.
// If these appear frequently, they should become first-class Hub fields.
var promotionCandidates = map[string]string{
	"citation":          "Consider adding a 'citation' field to Hub schema",
	"funding":           "Consider adding a 'funding' repeated field to Hub schema",
	"funder":            "Consider adding a 'funding' repeated field to Hub schema",
	"grant":             "Consider adding a 'funding' repeated field to Hub schema",
	"volume":            "Consider adding publication volume to Hub schema",
	"issue":             "Consider adding publication issue to Hub schema",
	"pages":             "Consider adding page range to Hub schema",
	"first_page":        "Consider adding page range to Hub schema",
	"last_page":         "Consider adding page range to Hub schema",
	"edition":           "Consider adding edition field to Hub schema",
	"series":            "Consider adding series info to Hub schema",
	"conference":        "Consider adding conference info to Hub schema",
	"event":             "Consider adding event info to Hub schema",
	"geo_location":      "Consider adding geographic coverage to Hub schema",
	"spatial_coverage":  "Consider adding geographic coverage to Hub schema",
	"temporal_coverage": "Consider adding temporal coverage to Hub schema",
}

func checkExtrasForPromotion(extra *structpb.Struct) []ValidationError {
	var warnings []ValidationError

	if extra == nil || extra.Fields == nil {
		return warnings
	}

	for key := range extra.Fields {
		// Normalize key for comparison
		normalizedKey := strings.ToLower(strings.ReplaceAll(key, "-", "_"))

		if suggestion, ok := promotionCandidates[normalizedKey]; ok {
			warnings = append(warnings, ValidationError{
				Field:   "extra." + key,
				Code:    "promotion_candidate",
				Message: suggestion,
			})
		}

		// Warn about human-readable labels used as keys
		if strings.Contains(key, " ") {
			warnings = append(warnings, ValidationError{
				Field:   "extra." + key,
				Code:    "invalid_key",
				Message: "extras keys should be machine names (no spaces); use snake_case",
			})
		}
	}

	return warnings
}

// ValidateExtrasTypes checks that values in extras have consistent types.
// Call this when aggregating records from multiple sources.
func ValidateExtrasTypes(records []*hubv1.Record) map[string][]string {
	// Track types seen for each key
	keyTypes := make(map[string]map[string]bool)

	for _, record := range records {
		if record.GetExtra() == nil {
			continue
		}
		for key, value := range record.GetExtra().Fields {
			if keyTypes[key] == nil {
				keyTypes[key] = make(map[string]bool)
			}
			keyTypes[key][getValueType(value)] = true
		}
	}

	// Find keys with inconsistent types
	inconsistent := make(map[string][]string)
	for key, types := range keyTypes {
		if len(types) > 1 {
			var typeList []string
			for t := range types {
				typeList = append(typeList, t)
			}
			inconsistent[key] = typeList
		}
	}

	return inconsistent
}

func getValueType(v *structpb.Value) string {
	switch v.Kind.(type) {
	case *structpb.Value_NullValue:
		return "null"
	case *structpb.Value_NumberValue:
		return "number"
	case *structpb.Value_StringValue:
		return "string"
	case *structpb.Value_BoolValue:
		return "bool"
	case *structpb.Value_StructValue:
		return "object"
	case *structpb.Value_ListValue:
		return "array"
	default:
		return "unknown"
	}
}
