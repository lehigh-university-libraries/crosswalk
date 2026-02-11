package mapping

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed profiles/*.yaml
var embeddedProfiles embed.FS

// ProfileRegistry holds loaded profiles.
type ProfileRegistry struct {
	profiles map[string]*Profile
}

// NewProfileRegistry creates a new profile registry with embedded profiles loaded.
func NewProfileRegistry() (*ProfileRegistry, error) {
	r := &ProfileRegistry{
		profiles: make(map[string]*Profile),
	}

	// Load embedded profiles
	entries, err := embeddedProfiles.ReadDir("profiles")
	if err != nil {
		return r, nil // No embedded profiles, that's okay
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		data, err := embeddedProfiles.ReadFile("profiles/" + entry.Name())
		if err != nil {
			continue
		}

		profile, err := parseProfile(data)
		if err != nil {
			continue
		}

		// Use filename without extension as profile name if not set
		if profile.Name == "" {
			profile.Name = strings.TrimSuffix(entry.Name(), ".yaml")
		}
		r.profiles[profile.Name] = profile
	}

	return r, nil
}

// LoadProfile loads a profile from a file path.
func LoadProfile(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading profile file: %w", err)
	}

	return parseProfile(data)
}

// LoadProfileFromString loads a profile from YAML content.
func LoadProfileFromString(content string) (*Profile, error) {
	return parseProfile([]byte(content))
}

func parseProfile(data []byte) (*Profile, error) {
	var profile Profile
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("parsing profile YAML: %w", err)
	}
	return &profile, nil
}

// Get retrieves a profile by name.
func (r *ProfileRegistry) Get(name string) (*Profile, bool) {
	p, ok := r.profiles[name]
	return p, ok
}

// Register adds a profile to the registry.
func (r *ProfileRegistry) Register(profile *Profile) {
	r.profiles[profile.Name] = profile
}

// List returns all registered profile names.
func (r *ProfileRegistry) List() []string {
	names := make([]string, 0, len(r.profiles))
	for name := range r.profiles {
		names = append(names, name)
	}
	return names
}

// LoadFromDirectory loads all profiles from a directory.
func (r *ProfileRegistry) LoadFromDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading profile directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		profile, err := LoadProfile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue // Skip invalid profiles
		}

		if profile.Name == "" {
			profile.Name = strings.TrimSuffix(entry.Name(), ".yaml")
		}
		r.profiles[profile.Name] = profile
	}

	return nil
}

// MergeProfiles merges a custom profile over a base profile.
// Custom fields override base fields.
func MergeProfiles(base, custom *Profile) *Profile {
	merged := &Profile{
		Name:        custom.Name,
		Format:      custom.Format,
		Description: custom.Description,
		Fields:      make(map[string]FieldMapping),
		Options:     base.Options,
	}

	if merged.Format == "" {
		merged.Format = base.Format
	}
	if merged.Description == "" {
		merged.Description = base.Description
	}

	// Copy base fields
	for k, v := range base.Fields {
		merged.Fields[k] = v
	}

	// Override with custom fields
	for k, v := range custom.Fields {
		merged.Fields[k] = v
	}

	// Merge options
	if custom.Options.TaxonomyFile != "" {
		merged.Options.TaxonomyFile = custom.Options.TaxonomyFile
	}
	if custom.Options.TaxonomyMode != "" {
		merged.Options.TaxonomyMode = custom.Options.TaxonomyMode
	}
	if custom.Options.CSVDelimiter != "" {
		merged.Options.CSVDelimiter = custom.Options.CSVDelimiter
	}
	if custom.Options.MultiValueSeparator != "" {
		merged.Options.MultiValueSeparator = custom.Options.MultiValueSeparator
	}
	if custom.Options.IncludeEmpty {
		merged.Options.IncludeEmpty = true
	}
	if custom.Options.StripHTML {
		merged.Options.StripHTML = true
	}

	return merged
}

// FieldsForIR returns all field mappings that target a specific IR field.
func (p *Profile) FieldsForIR(irField string) []struct {
	SourceField string
	Mapping     FieldMapping
} {
	var matches []struct {
		SourceField string
		Mapping     FieldMapping
	}

	for source, mapping := range p.Fields {
		base, _ := IRFieldName(mapping.IR)
		if base == irField || mapping.IR == irField {
			matches = append(matches, struct {
				SourceField string
				Mapping     FieldMapping
			}{source, mapping})
		}
	}

	return matches
}

// GetFieldMapping retrieves the mapping for a source field.
func (p *Profile) GetFieldMapping(sourceField string) (FieldMapping, bool) {
	m, ok := p.Fields[sourceField]
	return m, ok
}
