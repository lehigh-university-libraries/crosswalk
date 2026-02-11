package convert

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SerializerFunc is a function that serializes a value to a string.
type SerializerFunc func(value any, opts *SerializerOptions) (string, error)

// SerializerOptions contains configuration for serializers.
type SerializerOptions struct {
	// DateFormat specifies the output date format
	DateFormat string

	// JoinDelimiter specifies the delimiter for joining arrays
	JoinDelimiter string

	// CustomData allows passing additional serializer-specific configuration
	CustomData map[string]any
}

// SerializerRegistry manages registered serializers.
type SerializerRegistry struct {
	mu          sync.RWMutex
	serializers map[string]SerializerFunc
}

// NewSerializerRegistry creates a new serializer registry with default serializers.
func NewSerializerRegistry() *SerializerRegistry {
	r := &SerializerRegistry{
		serializers: make(map[string]SerializerFunc),
	}
	r.registerDefaults()
	return r
}

// Register adds a serializer to the registry.
func (r *SerializerRegistry) Register(name string, fn SerializerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serializers[name] = fn
}

// Get retrieves a serializer by name.
func (r *SerializerRegistry) Get(name string) (SerializerFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.serializers[name]
	return fn, ok
}

// Serialize applies a named serializer to a value.
func (r *SerializerRegistry) Serialize(serializerName string, value any, opts *SerializerOptions) (string, error) {
	fn, ok := r.Get(serializerName)
	if !ok {
		return "", fmt.Errorf("serializer not found: %s", serializerName)
	}
	if opts == nil {
		opts = &SerializerOptions{}
	}
	return fn(value, opts)
}

// Names returns all registered serializer names.
func (r *SerializerRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.serializers))
	for name := range r.serializers {
		names = append(names, name)
	}
	return names
}

// registerDefaults registers all built-in serializers.
func (r *SerializerRegistry) registerDefaults() {
	r.Register("passthrough", serializePassthrough)
	r.Register("year", serializeYear)
	r.Register("iso8601", serializeISO8601)
	r.Register("edtf", serializeEDTF)
	r.Register("join", serializeJoin)
	r.Register("bibtex_name", serializeBibTeXName)
	r.Register("csl_name", serializeCSLName)
	r.Register("doi_url", serializeDOIURL)
	r.Register("orcid_url", serializeORCIDURL)
}

// Default serializer registry instance.
var defaultSerializerRegistry = NewSerializerRegistry()

// DefaultSerializers returns the default serializer registry.
func DefaultSerializers() *SerializerRegistry {
	return defaultSerializerRegistry
}

// serializePassthrough returns the input as-is converted to string.
func serializePassthrough(value any, opts *SerializerOptions) (string, error) {
	if value == nil {
		return "", nil
	}
	switch v := value.(type) {
	case string:
		return v, nil
	case fmt.Stringer:
		return v.String(), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

// serializeYear extracts and formats a year.
func serializeYear(value any, opts *SerializerOptions) (string, error) {
	if value == nil {
		return "", nil
	}

	switch v := value.(type) {
	case string:
		// Extract 4-digit year
		yearRegex := regexp.MustCompile(`\b(1[0-9]{3}|20[0-9]{2})\b`)
		match := yearRegex.FindString(v)
		if match != "" {
			return match, nil
		}
		return v, nil
	case time.Time:
		return v.Format("2006"), nil
	case int:
		return fmt.Sprintf("%d", v), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

// serializeISO8601 formats a date as ISO 8601.
func serializeISO8601(value any, opts *SerializerOptions) (string, error) {
	if value == nil {
		return "", nil
	}

	switch v := value.(type) {
	case string:
		// Try to parse and reformat
		formats := []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02",
			"2006-01",
			"2006",
		}
		for _, format := range formats {
			if t, err := time.Parse(format, v); err == nil {
				return t.Format("2006-01-02"), nil
			}
		}
		return v, nil
	case time.Time:
		return v.Format("2006-01-02"), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

// serializeEDTF formats a date as EDTF.
func serializeEDTF(value any, opts *SerializerOptions) (string, error) {
	// EDTF is a superset of ISO 8601, so we use ISO format for basic dates
	return serializeISO8601(value, opts)
}

// serializeJoin joins array values with a delimiter.
func serializeJoin(value any, opts *SerializerOptions) (string, error) {
	delimiter := ", "
	if opts != nil && opts.JoinDelimiter != "" {
		delimiter = opts.JoinDelimiter
	}

	switch v := value.(type) {
	case []string:
		return strings.Join(v, delimiter), nil
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = fmt.Sprintf("%v", item)
		}
		return strings.Join(parts, delimiter), nil
	case string:
		return v, nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

// serializeBibTeXName formats a name in BibTeX format (Last, First).
func serializeBibTeXName(value any, opts *SerializerOptions) (string, error) {
	if value == nil {
		return "", nil
	}

	switch v := value.(type) {
	case BibTeXName:
		if v.Given == "" && v.Family == "" {
			return "", nil
		}
		if v.Given == "" {
			return v.Family, nil
		}
		if v.Suffix != "" {
			return fmt.Sprintf("%s, %s, %s", v.Family, v.Given, v.Suffix), nil
		}
		return fmt.Sprintf("%s, %s", v.Family, v.Given), nil

	case CSLName:
		if v.Literal != "" {
			return v.Literal, nil
		}
		if v.Given == "" && v.Family == "" {
			return "", nil
		}
		if v.Given == "" {
			return v.Family, nil
		}
		if v.Suffix != "" {
			return fmt.Sprintf("%s, %s, %s", v.Family, v.Given, v.Suffix), nil
		}
		return fmt.Sprintf("%s, %s", v.Family, v.Given), nil

	case map[string]any:
		family, _ := v["family"].(string)
		given, _ := v["given"].(string)
		suffix, _ := v["suffix"].(string)
		if given == "" && family == "" {
			return "", nil
		}
		if given == "" {
			return family, nil
		}
		if suffix != "" {
			return fmt.Sprintf("%s, %s, %s", family, given, suffix), nil
		}
		return fmt.Sprintf("%s, %s", family, given), nil

	case string:
		return v, nil

	default:
		return fmt.Sprintf("%v", value), nil
	}
}

// serializeCSLName formats a name in CSL-JSON format.
func serializeCSLName(value any, opts *SerializerOptions) (string, error) {
	// For string output, use the same format as BibTeX
	return serializeBibTeXName(value, opts)
}

// serializeDOIURL formats a DOI as a full URL.
func serializeDOIURL(value any, opts *SerializerOptions) (string, error) {
	if value == nil {
		return "", nil
	}

	str, ok := value.(string)
	if !ok {
		return fmt.Sprintf("%v", value), nil
	}

	// Already a URL
	if strings.HasPrefix(str, "http") {
		return str, nil
	}

	// Remove any prefix
	str = strings.TrimPrefix(str, "doi:")
	str = strings.TrimPrefix(str, "DOI:")
	str = strings.TrimSpace(str)

	// Add doi.org prefix
	if strings.HasPrefix(str, "10.") {
		return "https://doi.org/" + str, nil
	}

	return str, nil
}

// serializeORCIDURL formats an ORCID as a full URL.
func serializeORCIDURL(value any, opts *SerializerOptions) (string, error) {
	if value == nil {
		return "", nil
	}

	str, ok := value.(string)
	if !ok {
		return fmt.Sprintf("%v", value), nil
	}

	// Already a URL
	if strings.HasPrefix(str, "http") {
		return str, nil
	}

	// Remove any prefix
	str = strings.TrimPrefix(str, "orcid:")
	str = strings.TrimPrefix(str, "ORCID:")
	str = strings.TrimSpace(str)

	// Add orcid.org prefix if it looks like an ORCID
	orcidRegex := regexp.MustCompile(`^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$`)
	if orcidRegex.MatchString(strings.ToUpper(str)) {
		return "https://orcid.org/" + str, nil
	}

	return str, nil
}
