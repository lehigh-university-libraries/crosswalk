package format

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

// Registry holds registered formats.
type Registry struct {
	formats map[string]Format
}

// DefaultRegistry is the global format registry.
var DefaultRegistry = NewRegistry()

// NewRegistry creates a new format registry.
func NewRegistry() *Registry {
	return &Registry{
		formats: make(map[string]Format),
	}
}

// Register adds a format to the registry.
func (r *Registry) Register(f Format) {
	r.formats[f.Name()] = f
}

// Get retrieves a format by name.
func (r *Registry) Get(name string) (Format, bool) {
	f, ok := r.formats[strings.ToLower(name)]
	return f, ok
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

// List returns all registered format names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.formats))
	for name := range r.formats {
		names = append(names, name)
	}
	return names
}

// DetectFormat attempts to detect the format from file extension and/or content.
func (r *Registry) DetectFormat(filename string, peek []byte) (Format, error) {
	// Try by extension first
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	for _, f := range r.formats {
		for _, fext := range f.Extensions() {
			if ext == fext {
				return f, nil
			}
		}
	}

	// Try by content detection
	if len(peek) > 0 {
		for _, f := range r.formats {
			if f.CanParse(peek) {
				return f, nil
			}
		}
	}

	return nil, fmt.Errorf("could not detect format for %s", filename)
}

// DetectFromContent attempts to detect format from content alone.
func (r *Registry) DetectFromContent(peek []byte) (Format, error) {
	// Trim whitespace for detection
	peek = bytes.TrimSpace(peek)

	for _, f := range r.formats {
		if f.CanParse(peek) {
			return f, nil
		}
	}

	return nil, fmt.Errorf("could not detect format from content")
}

// Register adds a format to the default registry.
func Register(f Format) {
	DefaultRegistry.Register(f)
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
