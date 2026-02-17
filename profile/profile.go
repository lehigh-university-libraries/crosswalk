// Package profile manages crosswalk mapping profiles stored in ~/.crosswalk/profiles.
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Profile represents a mapping configuration for a specific source.
type Profile struct {
	// Name is the profile identifier (e.g., "lehigh-preserve")
	Name string `yaml:"name" json:"name"`

	// Format is the source format (e.g., "drupal", "csv")
	Format string `yaml:"format" json:"format"`

	// Description provides human-readable documentation
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Source contains information about the original source for auto-discovery
	Source SourceInfo `yaml:"source,omitempty" json:"source,omitempty"`

	// Fields maps source field names to hub field configurations
	Fields map[string]FieldMapping `yaml:"fields" json:"fields"`

	// Options contains format-specific options
	Options Options `yaml:"options,omitempty" json:"options,omitempty"`
}

// SourceInfo contains metadata for auto-discovery.
type SourceInfo struct {
	// DrupalSiteUUID is the Drupal site UUID for matching
	DrupalSiteUUID string `yaml:"drupal_site_uuid,omitempty" json:"drupal_site_uuid,omitempty"`

	// DrupalSiteName is a human-readable site name
	DrupalSiteName string `yaml:"drupal_site_name,omitempty" json:"drupal_site_name,omitempty"`

	// ConfigPath is the path to the Drupal config/sync directory used to generate this profile
	ConfigPath string `yaml:"config_path,omitempty" json:"config_path,omitempty"`

	// CSVColumns is the expected column headers for CSV matching
	CSVColumns []string `yaml:"csv_columns,omitempty" json:"csv_columns,omitempty"`

	// FieldFingerprint is a hash of field names for quick matching
	FieldFingerprint string `yaml:"field_fingerprint,omitempty" json:"field_fingerprint,omitempty"`
}

// FieldMapping describes how a source field maps to a hub field.
type FieldMapping struct {
	// Hub is the target hub field name (e.g., "Title", "Contributors", "Extra.nid")
	Hub string `yaml:"hub" json:"hub"`

	// Type is the field type hint (e.g., "typed_relation", "entity_reference", "uri")
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// Priority determines which field wins when multiple map to same hub field (higher wins)
	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`

	// DateType specifies the semantic date type for date fields
	DateType string `yaml:"date_type,omitempty" json:"date_type,omitempty"`

	// Parser specifies a special parser to use (e.g., "edtf")
	Parser string `yaml:"parser,omitempty" json:"parser,omitempty"`

	// Resolve indicates the entity type to resolve (e.g., "taxonomy_term", "node")
	Resolve string `yaml:"resolve,omitempty" json:"resolve,omitempty"`

	// RoleField is the field containing role information for typed relations
	RoleField string `yaml:"role_field,omitempty" json:"role_field,omitempty"`

	// RelationType specifies the relation type for relation fields
	RelationType string `yaml:"relation_type,omitempty" json:"relation_type,omitempty"`

	// Vocabulary specifies the vocabulary for subject fields
	Vocabulary string `yaml:"vocabulary,omitempty" json:"vocabulary,omitempty"`

	// MultiValue indicates the field can have multiple values
	MultiValue bool `yaml:"multi_value,omitempty" json:"multi_value,omitempty"`

	// Delimiter for multi-value fields
	Delimiter string `yaml:"delimiter,omitempty" json:"delimiter,omitempty"`

	// Skip indicates this field should be ignored
	Skip bool `yaml:"skip,omitempty" json:"skip,omitempty"`
}

// Options contains format-specific configuration options.
type Options struct {
	// MultiValueSeparator is the delimiter for multi-value fields
	MultiValueSeparator string `yaml:"multi_value_separator,omitempty" json:"multi_value_separator,omitempty"`

	// CSVDelimiter is the CSV field delimiter
	CSVDelimiter string `yaml:"csv_delimiter,omitempty" json:"csv_delimiter,omitempty"`

	// StripHTML strips HTML from text fields
	StripHTML bool `yaml:"strip_html,omitempty" json:"strip_html,omitempty"`

	// TaxonomyMode specifies how to handle taxonomy references
	// "resolve" = lookup names, "passthrough" = keep IDs
	TaxonomyMode string `yaml:"taxonomy_mode,omitempty" json:"taxonomy_mode,omitempty"`
}

// GetMultiValueSeparator returns the multi-value separator with a default.
func (p *Profile) GetMultiValueSeparator() string {
	if p.Options.MultiValueSeparator != "" {
		return p.Options.MultiValueSeparator
	}
	return "|"
}

// GetCSVDelimiter returns the CSV delimiter with a default.
func (p *Profile) GetCSVDelimiter() string {
	if p.Options.CSVDelimiter != "" {
		return p.Options.CSVDelimiter
	}
	return ","
}

// configDirOverride holds a user-specified configuration directory.
// When empty, the default $HOME/.crosswalk is used.
var configDirOverride string

// SetConfigDir overrides the default configuration directory.
func SetConfigDir(dir string) {
	configDirOverride = dir
}

// ConfigDir returns the crosswalk configuration directory.
func ConfigDir() (string, error) {
	if configDirOverride != "" {
		return configDirOverride, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".crosswalk"), nil
}

// ProfilesDir returns the profiles directory.
func ProfilesDir() (string, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "profiles"), nil
}

// EnsureProfilesDir creates the profiles directory if it doesn't exist.
func EnsureProfilesDir() error {
	dir, err := ProfilesDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0755)
}

// ProfilePath returns the path for a profile file.
func ProfilePath(name string) (string, error) {
	dir, err := ProfilesDir()
	if err != nil {
		return "", err
	}
	// Sanitize name
	name = strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	return filepath.Join(dir, name+".yaml"), nil
}

// Save writes the profile to disk.
func (p *Profile) Save() error {
	if err := EnsureProfilesDir(); err != nil {
		return fmt.Errorf("creating profiles directory: %w", err)
	}

	path, err := ProfilePath(p.Name)
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshaling profile: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing profile: %w", err)
	}

	return nil
}

// Load reads a profile from disk.
func Load(name string) (*Profile, error) {
	path, err := ProfilePath(name)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("profile %q not found", name)
		}
		return nil, fmt.Errorf("reading profile: %w", err)
	}

	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing profile: %w", err)
	}

	return &p, nil
}

// List returns all available profile names.
func List() ([]string, error) {
	dir, err := ProfilesDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading profiles directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			names = append(names, strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"))
		}
	}

	return names, nil
}

// Delete removes a profile.
func Delete(name string) error {
	path, err := ProfilePath(name)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("profile %q not found", name)
		}
		return fmt.Errorf("deleting profile: %w", err)
	}

	return nil
}

// Exists checks if a profile exists.
func Exists(name string) bool {
	path, err := ProfilePath(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// ToMappingProfile converts a profile.Profile to a mapping.Profile for compatibility
// with existing format parsers.
func (p *Profile) ToMappingProfile() *MappingProfile {
	mp := &MappingProfile{
		Name:        p.Name,
		Format:      p.Format,
		Description: p.Description,
		Fields:      make(map[string]MappingFieldMapping),
		Options: MappingProfileOptions{
			MultiValueSeparator: p.Options.MultiValueSeparator,
			CSVDelimiter:        p.Options.CSVDelimiter,
			StripHTML:           p.Options.StripHTML,
			TaxonomyMode:        p.Options.TaxonomyMode,
		},
	}

	for source, fm := range p.Fields {
		mp.Fields[source] = MappingFieldMapping{
			IR:           fm.Hub, // Hub field maps to IR in the old format
			Type:         fm.Type,
			Priority:     fm.Priority,
			DateType:     fm.DateType,
			Parser:       fm.Parser,
			Resolve:      fm.Resolve,
			RoleField:    fm.RoleField,
			RelationType: fm.RelationType,
			Vocabulary:   fm.Vocabulary,
			MultiValue:   fm.MultiValue,
			Delimiter:    fm.Delimiter,
		}
	}

	return mp
}

// MappingProfile mirrors the mapping.Profile type for conversion.
// This allows user profiles to work with existing format parsers.
type MappingProfile struct {
	Name        string                         `yaml:"name" json:"name"`
	Format      string                         `yaml:"format" json:"format"`
	Description string                         `yaml:"description,omitempty" json:"description,omitempty"`
	Fields      map[string]MappingFieldMapping `yaml:"fields" json:"fields"`
	Options     MappingProfileOptions          `yaml:"options,omitempty" json:"options,omitempty"`
}

// MappingFieldMapping mirrors the mapping.FieldMapping type.
type MappingFieldMapping struct {
	IR           string `yaml:"ir" json:"ir"`
	Type         string `yaml:"type,omitempty" json:"type,omitempty"`
	Priority     int    `yaml:"priority,omitempty" json:"priority,omitempty"`
	DateType     string `yaml:"date_type,omitempty" json:"date_type,omitempty"`
	Parser       string `yaml:"parser,omitempty" json:"parser,omitempty"`
	Resolve      string `yaml:"resolve,omitempty" json:"resolve,omitempty"`
	RoleField    string `yaml:"role_field,omitempty" json:"role_field,omitempty"`
	RelationType string `yaml:"relation_type,omitempty" json:"relation_type,omitempty"`
	Vocabulary   string `yaml:"vocabulary,omitempty" json:"vocabulary,omitempty"`
	MultiValue   bool   `yaml:"multi_value,omitempty" json:"multi_value,omitempty"`
	Delimiter    string `yaml:"delimiter,omitempty" json:"delimiter,omitempty"`
}

// MappingProfileOptions mirrors the mapping.ProfileOptions type.
type MappingProfileOptions struct {
	TaxonomyFile        string `yaml:"taxonomy_file,omitempty" json:"taxonomy_file,omitempty"`
	TaxonomyMode        string `yaml:"taxonomy_mode,omitempty" json:"taxonomy_mode,omitempty"`
	CSVDelimiter        string `yaml:"csv_delimiter,omitempty" json:"csv_delimiter,omitempty"`
	MultiValueSeparator string `yaml:"multi_value_separator,omitempty" json:"multi_value_separator,omitempty"`
	IncludeEmpty        bool   `yaml:"include_empty,omitempty" json:"include_empty,omitempty"`
	StripHTML           bool   `yaml:"strip_html,omitempty" json:"strip_html,omitempty"`
}

// GetMultiValueSeparator returns the multi-value separator with a default.
func (p *MappingProfile) GetMultiValueSeparator() string {
	if p.Options.MultiValueSeparator != "" {
		return p.Options.MultiValueSeparator
	}
	return "|"
}

// GetCSVDelimiter returns the CSV delimiter with a default.
func (p *MappingProfile) GetCSVDelimiter() string {
	if p.Options.CSVDelimiter != "" {
		return p.Options.CSVDelimiter
	}
	return ","
}
