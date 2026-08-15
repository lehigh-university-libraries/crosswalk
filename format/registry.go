package format

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// ErrDuplicateFormat indicates that a normalized format name is already
// registered.
var ErrDuplicateFormat = errors.New("format already registered")

// AmbiguousFormatError reports that more than one registered format matches
// the available filename and content evidence.
type AmbiguousFormatError struct {
	Filename   string
	Candidates []string
}

// Error implements error.
func (e *AmbiguousFormatError) Error() string {
	location := "content"
	if e.Filename != "" {
		location = fmt.Sprintf("%q", e.Filename)
	}
	return fmt.Sprintf("ambiguous format for %s: %s", location, strings.Join(e.Candidates, ", "))
}

type registeredFormat struct {
	name       string
	format     Format
	extensions map[string]struct{}
}

// Registry holds registered formats.
type Registry struct {
	mu      sync.RWMutex
	formats map[string]registeredFormat
}

// DefaultRegistry is the global format registry.
var DefaultRegistry = NewRegistry()

// NewRegistry creates a new format registry.
func NewRegistry() *Registry {
	return &Registry{
		formats: make(map[string]registeredFormat),
	}
}

// Register adds a format to the registry. Names and extensions are matched
// case-insensitively with surrounding whitespace and a leading extension dot
// ignored.
func (r *Registry) Register(f Format) error {
	if isNilFormat(f) {
		return fmt.Errorf("register format: format is nil")
	}

	name := normalizeFormatName(f.Name())
	if name == "" {
		return fmt.Errorf("register format: name is empty")
	}

	extensions := make(map[string]struct{}, len(f.Extensions()))
	for _, extension := range f.Extensions() {
		normalized := normalizeExtension(extension)
		if normalized == "" {
			return fmt.Errorf("register format %q: extension is empty", name)
		}
		extensions[normalized] = struct{}{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.formats == nil {
		r.formats = make(map[string]registeredFormat)
	}
	if _, exists := r.formats[name]; exists {
		return fmt.Errorf("register format %q: %w", name, ErrDuplicateFormat)
	}
	r.formats[name] = registeredFormat{
		name:       name,
		format:     f,
		extensions: extensions,
	}
	return nil
}

// Get retrieves a format by name.
func (r *Registry) Get(name string) (Format, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.formats[normalizeFormatName(name)]
	return entry.format, ok
}

// GetParser retrieves a parser by name.
func (r *Registry) GetParser(name string) (Parser, error) {
	f, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown format: %s", name)
	}
	p, ok := f.(Parser)
	if !ok {
		return nil, fmt.Errorf("format %s does not support parsing", name)
	}
	return p, nil
}

// GetSerializer retrieves a serializer by name.
func (r *Registry) GetSerializer(name string) (Serializer, error) {
	f, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown format: %s", name)
	}
	s, ok := f.(Serializer)
	if !ok {
		return nil, fmt.Errorf("format %s does not support serialization", name)
	}
	return s, nil
}

// List returns all registered format names in lexical order.
func (r *Registry) List() []string {
	entries := r.entries()
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.name
	}
	return names
}

// DetectFormat attempts to detect the format from file extension and content.
// A recognized extension narrows the candidates before content probes run.
func (r *Registry) DetectFormat(filename string, peek []byte) (Format, error) {
	entries := r.entries()
	extension := normalizeExtension(filepath.Ext(filename))
	candidates := candidatesForExtension(entries, extension)
	if len(candidates) == 0 {
		return detectFromCandidates(filename, bytes.TrimSpace(peek), entries)
	}

	content := bytes.TrimSpace(peek)
	if len(content) > 0 {
		matches := matchingFormats(candidates, content)
		switch len(matches) {
		case 1:
			return matches[0].format, nil
		case 0:
			// The extension remains useful when it uniquely identifies a format,
			// even when the caller could provide only a short content sample.
		default:
			return nil, newAmbiguousFormatError(filename, matches)
		}
	}

	if len(candidates) == 1 {
		return candidates[0].format, nil
	}
	return nil, newAmbiguousFormatError(filename, candidates)
}

// DetectFromContent attempts to detect format from content alone.
func (r *Registry) DetectFromContent(peek []byte) (Format, error) {
	return detectFromCandidates("", bytes.TrimSpace(peek), r.entries())
}

func (r *Registry) entries() []registeredFormat {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]registeredFormat, 0, len(r.formats))
	for _, entry := range r.formats {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}

func candidatesForExtension(entries []registeredFormat, extension string) []registeredFormat {
	if extension == "" {
		return nil
	}
	candidates := make([]registeredFormat, 0, len(entries))
	for _, entry := range entries {
		if _, matches := entry.extensions[extension]; matches {
			candidates = append(candidates, entry)
		}
	}
	return candidates
}

func detectFromCandidates(filename string, content []byte, candidates []registeredFormat) (Format, error) {
	matches := matchingFormats(candidates, content)
	switch len(matches) {
	case 0:
		if filename == "" {
			return nil, fmt.Errorf("could not detect format from content")
		}
		return nil, fmt.Errorf("could not detect format for %s", filename)
	case 1:
		return matches[0].format, nil
	default:
		return nil, newAmbiguousFormatError(filename, matches)
	}
}

func matchingFormats(candidates []registeredFormat, content []byte) []registeredFormat {
	if len(content) == 0 {
		return nil
	}
	matches := make([]registeredFormat, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.format.CanParse(content) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func newAmbiguousFormatError(filename string, candidates []registeredFormat) *AmbiguousFormatError {
	names := make([]string, len(candidates))
	for i, candidate := range candidates {
		names[i] = candidate.name
	}
	return &AmbiguousFormatError{Filename: filename, Candidates: names}
}

func normalizeFormatName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeExtension(extension string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(extension), "."))
}

func isNilFormat(f Format) bool {
	if f == nil {
		return true
	}
	value := reflect.ValueOf(f)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Register adds a format to the default registry.
func Register(f Format) error {
	return DefaultRegistry.Register(f)
}

// MustRegister adds a format to the default registry and panics if the format
// violates the registry contract.
func MustRegister(f Format) {
	if err := Register(f); err != nil {
		panic(err)
	}
}

// Get retrieves a format from the default registry.
func Get(name string) (Format, bool) {
	return DefaultRegistry.Get(name)
}

// GetParser retrieves a parser from the default registry.
func GetParser(name string) (Parser, error) {
	return DefaultRegistry.GetParser(name)
}

// GetSerializer retrieves a serializer from the default registry.
func GetSerializer(name string) (Serializer, error) {
	return DefaultRegistry.GetSerializer(name)
}

// DetectFormat detects format using the default registry.
func DetectFormat(filename string, peek []byte) (Format, error) {
	return DefaultRegistry.DetectFormat(filename, peek)
}
