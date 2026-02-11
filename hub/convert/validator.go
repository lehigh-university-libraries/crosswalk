package convert

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ValidationError represents a validation failure.
type ValidationError struct {
	Field   string
	Value   any
	Rule    string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for field %q (rule: %s): %s", e.Field, e.Rule, e.Message)
}

// ValidatorFunc is a function that validates a value.
// It returns nil if valid, or a ValidationError if invalid.
type ValidatorFunc func(value any, opts *ValidatorOptions) error

// ValidatorOptions contains configuration for validators.
type ValidatorOptions struct {
	// FieldName is the name of the field being validated (for error messages)
	FieldName string

	// Pattern is a regex pattern for pattern validation
	Pattern string

	// MinLength is the minimum string length
	MinLength int32

	// MaxLength is the maximum string length
	MaxLength int32

	// MinValue is the minimum numeric value
	MinValue int64

	// MaxValue is the maximum numeric value
	MaxValue int64

	// MinCount is the minimum number of items (for arrays)
	MinCount int32

	// MaxCount is the maximum number of items (for arrays)
	MaxCount int32

	// CustomData allows passing additional validator-specific configuration
	CustomData map[string]any
}

// ValidatorRegistry manages registered validators.
type ValidatorRegistry struct {
	mu         sync.RWMutex
	validators map[string]ValidatorFunc
}

// NewValidatorRegistry creates a new validator registry with default validators.
func NewValidatorRegistry() *ValidatorRegistry {
	r := &ValidatorRegistry{
		validators: make(map[string]ValidatorFunc),
	}
	r.registerDefaults()
	return r
}

// Register adds a validator to the registry.
func (r *ValidatorRegistry) Register(name string, fn ValidatorFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validators[name] = fn
}

// Get retrieves a validator by name.
func (r *ValidatorRegistry) Get(name string) (ValidatorFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.validators[name]
	return fn, ok
}

// Validate applies a named validator to a value.
func (r *ValidatorRegistry) Validate(validatorName string, value any, opts *ValidatorOptions) error {
	fn, ok := r.Get(validatorName)
	if !ok {
		return fmt.Errorf("validator not found: %s", validatorName)
	}
	if opts == nil {
		opts = &ValidatorOptions{}
	}
	return fn(value, opts)
}

// ValidateAll applies multiple validators (comma-separated names) to a value.
func (r *ValidatorRegistry) ValidateAll(validatorNames string, value any, opts *ValidatorOptions) []error {
	if validatorNames == "" {
		return nil
	}

	names := strings.Split(validatorNames, ",")
	var errors []error

	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if err := r.Validate(name, value, opts); err != nil {
			errors = append(errors, err)
		}
	}

	return errors
}

// Names returns all registered validator names.
func (r *ValidatorRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.validators))
	for name := range r.validators {
		names = append(names, name)
	}
	return names
}

// registerDefaults registers all built-in validators.
func (r *ValidatorRegistry) registerDefaults() {
	r.Register("required", validateRequired)
	r.Register("doi", validateDOI)
	r.Register("isbn", validateISBN)
	r.Register("issn", validateISSN)
	r.Register("orcid", validateORCID)
	r.Register("url", validateURL)
	r.Register("email", validateEmail)
	r.Register("iso8601", validateISO8601)
	r.Register("edtf", validateEDTF)
	r.Register("year_range", validateYearRange)
	r.Register("pattern", validatePattern)
	r.Register("length", validateLength)
	r.Register("range", validateRange)
	r.Register("count", validateCount)
}

// Default validator registry instance.
var defaultValidatorRegistry = NewValidatorRegistry()

// DefaultValidators returns the default validator registry.
func DefaultValidators() *ValidatorRegistry {
	return defaultValidatorRegistry
}

// validateRequired checks that a value is not empty.
func validateRequired(value any, opts *ValidatorOptions) error {
	if value == nil {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "required",
			Message: "value is required",
		}
	}

	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return &ValidationError{
				Field:   opts.FieldName,
				Value:   value,
				Rule:    "required",
				Message: "value is required",
			}
		}
	case []string:
		if len(v) == 0 {
			return &ValidationError{
				Field:   opts.FieldName,
				Value:   value,
				Rule:    "required",
				Message: "at least one value is required",
			}
		}
	case []any:
		if len(v) == 0 {
			return &ValidationError{
				Field:   opts.FieldName,
				Value:   value,
				Rule:    "required",
				Message: "at least one value is required",
			}
		}
	}

	return nil
}

// validateDOI validates a DOI format.
func validateDOI(value any, opts *ValidatorOptions) error {
	str, ok := value.(string)
	if !ok || str == "" {
		return nil // Empty values are not invalid, use required for that
	}

	// Normalize first
	str = strings.TrimPrefix(str, "https://doi.org/")
	str = strings.TrimPrefix(str, "http://doi.org/")
	str = strings.TrimPrefix(str, "https://dx.doi.org/")
	str = strings.TrimPrefix(str, "http://dx.doi.org/")
	str = strings.TrimPrefix(str, "doi:")
	str = strings.TrimPrefix(str, "DOI:")

	// DOI must start with 10. followed by at least 4 digits
	doiRegex := regexp.MustCompile(`^10\.\d{4,}(?:\.\d+)*/[^\s]+$`)
	if !doiRegex.MatchString(str) {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "doi",
			Message: "invalid DOI format",
		}
	}

	return nil
}

// validateISBN validates ISBN-10 or ISBN-13 format.
func validateISBN(value any, opts *ValidatorOptions) error {
	str, ok := value.(string)
	if !ok || str == "" {
		return nil
	}

	// Remove common prefixes, dashes, and spaces
	str = strings.TrimPrefix(str, "ISBN:")
	str = strings.TrimPrefix(str, "isbn:")
	str = strings.ReplaceAll(str, "-", "")
	str = strings.ReplaceAll(str, " ", "")
	str = strings.TrimSpace(str)

	if len(str) == 10 {
		return validateISBN10(str, opts)
	}
	if len(str) == 13 {
		return validateISBN13(str, opts)
	}

	return &ValidationError{
		Field:   opts.FieldName,
		Value:   value,
		Rule:    "isbn",
		Message: "ISBN must be 10 or 13 digits",
	}
}

func validateISBN10(isbn string, opts *ValidatorOptions) error {
	var sum int
	for i := 0; i < 9; i++ {
		digit, err := strconv.Atoi(string(isbn[i]))
		if err != nil {
			return &ValidationError{
				Field:   opts.FieldName,
				Value:   isbn,
				Rule:    "isbn",
				Message: "ISBN-10 contains invalid characters",
			}
		}
		sum += digit * (10 - i)
	}

	// Check digit can be X (representing 10)
	lastChar := strings.ToUpper(string(isbn[9]))
	var checkDigit int
	if lastChar == "X" {
		checkDigit = 10
	} else {
		var err error
		checkDigit, err = strconv.Atoi(lastChar)
		if err != nil {
			return &ValidationError{
				Field:   opts.FieldName,
				Value:   isbn,
				Rule:    "isbn",
				Message: "ISBN-10 check digit invalid",
			}
		}
	}
	sum += checkDigit

	if sum%11 != 0 {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   isbn,
			Rule:    "isbn",
			Message: "ISBN-10 checksum invalid",
		}
	}

	return nil
}

func validateISBN13(isbn string, opts *ValidatorOptions) error {
	var sum int
	for i := 0; i < 13; i++ {
		digit, err := strconv.Atoi(string(isbn[i]))
		if err != nil {
			return &ValidationError{
				Field:   opts.FieldName,
				Value:   isbn,
				Rule:    "isbn",
				Message: "ISBN-13 contains invalid characters",
			}
		}
		if i%2 == 0 {
			sum += digit
		} else {
			sum += digit * 3
		}
	}

	if sum%10 != 0 {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   isbn,
			Rule:    "isbn",
			Message: "ISBN-13 checksum invalid",
		}
	}

	return nil
}

// validateISSN validates ISSN format (XXXX-XXXX).
func validateISSN(value any, opts *ValidatorOptions) error {
	str, ok := value.(string)
	if !ok || str == "" {
		return nil
	}

	// Remove prefix and normalize
	str = strings.TrimPrefix(str, "ISSN:")
	str = strings.TrimPrefix(str, "issn:")
	str = strings.TrimSpace(str)

	// Must match XXXX-XXXX pattern
	issnRegex := regexp.MustCompile(`^\d{4}-\d{3}[\dX]$`)
	if !issnRegex.MatchString(strings.ToUpper(str)) {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "issn",
			Message: "ISSN must be in format XXXX-XXXX",
		}
	}

	// Validate checksum
	digits := strings.ReplaceAll(str, "-", "")
	var sum int
	for i := 0; i < 7; i++ {
		digit, _ := strconv.Atoi(string(digits[i]))
		sum += digit * (8 - i)
	}

	lastChar := strings.ToUpper(string(digits[7]))
	var checkDigit int
	if lastChar == "X" {
		checkDigit = 10
	} else {
		checkDigit, _ = strconv.Atoi(lastChar)
	}

	if (sum+checkDigit)%11 != 0 {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "issn",
			Message: "ISSN checksum invalid",
		}
	}

	return nil
}

// validateORCID validates ORCID format.
func validateORCID(value any, opts *ValidatorOptions) error {
	str, ok := value.(string)
	if !ok || str == "" {
		return nil
	}

	// Remove common prefixes
	str = strings.TrimPrefix(str, "https://orcid.org/")
	str = strings.TrimPrefix(str, "http://orcid.org/")
	str = strings.TrimPrefix(str, "orcid:")
	str = strings.TrimSpace(str)

	// Must match 0000-0000-0000-000X pattern
	orcidRegex := regexp.MustCompile(`^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$`)
	if !orcidRegex.MatchString(strings.ToUpper(str)) {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "orcid",
			Message: "ORCID must be in format 0000-0000-0000-000X",
		}
	}

	// Validate checksum (ISO 7064 Mod 11-2)
	digits := strings.ReplaceAll(str, "-", "")
	var total int
	for i := 0; i < 15; i++ {
		digit, _ := strconv.Atoi(string(digits[i]))
		total = (total + digit) * 2
	}
	remainder := total % 11
	checkDigit := (12 - remainder) % 11

	lastChar := strings.ToUpper(string(digits[15]))
	var expectedCheck int
	if lastChar == "X" {
		expectedCheck = 10
	} else {
		expectedCheck, _ = strconv.Atoi(lastChar)
	}

	if checkDigit != expectedCheck {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "orcid",
			Message: "ORCID checksum invalid",
		}
	}

	return nil
}

// validateURL validates URL format.
func validateURL(value any, opts *ValidatorOptions) error {
	str, ok := value.(string)
	if !ok || str == "" {
		return nil
	}

	// Basic URL validation
	urlRegex := regexp.MustCompile(`^https?://[^\s]+$`)
	if !urlRegex.MatchString(str) {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "url",
			Message: "invalid URL format",
		}
	}

	return nil
}

// validateEmail validates email format.
func validateEmail(value any, opts *ValidatorOptions) error {
	str, ok := value.(string)
	if !ok || str == "" {
		return nil
	}

	// Basic email validation
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(str) {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "email",
			Message: "invalid email format",
		}
	}

	return nil
}

// validateISO8601 validates ISO 8601 date format.
func validateISO8601(value any, opts *ValidatorOptions) error {
	str, ok := value.(string)
	if !ok || str == "" {
		return nil
	}

	// Try common ISO 8601 formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
		"2006-01",
		"2006",
	}

	for _, format := range formats {
		if _, err := time.Parse(format, str); err == nil {
			return nil
		}
	}

	return &ValidationError{
		Field:   opts.FieldName,
		Value:   value,
		Rule:    "iso8601",
		Message: "invalid ISO 8601 date format",
	}
}

// validateEDTF validates Extended Date/Time Format.
func validateEDTF(value any, opts *ValidatorOptions) error {
	str, ok := value.(string)
	if !ok || str == "" {
		return nil
	}

	// EDTF level 0 (same as ISO 8601)
	// Level 1 adds: uncertain (?), approximate (~), unspecified (X)
	// Level 2 adds: sets, seasons, qualifications

	// Basic EDTF pattern
	edtfRegex := regexp.MustCompile(`^[\d\-/~?X\[\]{}]+$`)
	if !edtfRegex.MatchString(str) {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "edtf",
			Message: "invalid EDTF format",
		}
	}

	return nil
}

// validateYearRange validates a year is within reasonable bounds.
func validateYearRange(value any, opts *ValidatorOptions) error {
	str, ok := value.(string)
	if !ok || str == "" {
		return nil
	}

	// Extract year
	yearRegex := regexp.MustCompile(`^\d{4}$`)
	if !yearRegex.MatchString(str) {
		// Try to extract year from longer string
		yearRegex = regexp.MustCompile(`\b(\d{4})\b`)
		match := yearRegex.FindString(str)
		if match == "" {
			return nil // Not a year format, skip
		}
		str = match
	}

	year, err := strconv.Atoi(str)
	if err != nil {
		return nil
	}

	minYear := 1000
	maxYear := time.Now().Year() + 10

	if opts.MinValue != 0 {
		minYear = int(opts.MinValue)
	}
	if opts.MaxValue != 0 {
		maxYear = int(opts.MaxValue)
	}

	if year < minYear || year > maxYear {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "year_range",
			Message: fmt.Sprintf("year must be between %d and %d", minYear, maxYear),
		}
	}

	return nil
}

// validatePattern validates against a regex pattern.
func validatePattern(value any, opts *ValidatorOptions) error {
	if opts.Pattern == "" {
		return nil
	}

	str, ok := value.(string)
	if !ok || str == "" {
		return nil
	}

	pattern, err := regexp.Compile(opts.Pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}

	if !pattern.MatchString(str) {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "pattern",
			Message: fmt.Sprintf("value does not match pattern %q", opts.Pattern),
		}
	}

	return nil
}

// validateLength validates string length.
func validateLength(value any, opts *ValidatorOptions) error {
	str, ok := value.(string)
	if !ok {
		return nil
	}

	length := len(str)

	if opts.MinLength > 0 && int32(length) < opts.MinLength {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "length",
			Message: fmt.Sprintf("length must be at least %d", opts.MinLength),
		}
	}

	if opts.MaxLength > 0 && int32(length) > opts.MaxLength {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "length",
			Message: fmt.Sprintf("length must be at most %d", opts.MaxLength),
		}
	}

	return nil
}

// validateRange validates numeric range.
func validateRange(value any, opts *ValidatorOptions) error {
	var num int64

	switch v := value.(type) {
	case int:
		num = int64(v)
	case int32:
		num = int64(v)
	case int64:
		num = v
	case string:
		var err error
		num, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil // Not a number, skip
		}
	default:
		return nil
	}

	if opts.MinValue != 0 && num < opts.MinValue {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "range",
			Message: fmt.Sprintf("value must be at least %d", opts.MinValue),
		}
	}

	if opts.MaxValue != 0 && num > opts.MaxValue {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "range",
			Message: fmt.Sprintf("value must be at most %d", opts.MaxValue),
		}
	}

	return nil
}

// validateCount validates array/slice length.
func validateCount(value any, opts *ValidatorOptions) error {
	var count int

	switch v := value.(type) {
	case []string:
		count = len(v)
	case []any:
		count = len(v)
	default:
		return nil
	}

	if opts.MinCount > 0 && int32(count) < opts.MinCount {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "count",
			Message: fmt.Sprintf("must have at least %d items", opts.MinCount),
		}
	}

	if opts.MaxCount > 0 && int32(count) > opts.MaxCount {
		return &ValidationError{
			Field:   opts.FieldName,
			Value:   value,
			Rule:    "count",
			Message: fmt.Sprintf("must have at most %d items", opts.MaxCount),
		}
	}

	return nil
}
