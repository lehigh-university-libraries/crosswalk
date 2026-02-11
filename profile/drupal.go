package profile

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DrupalFieldConfig represents a Drupal field configuration from config/sync.
type DrupalFieldConfig struct {
	UUID        string         `yaml:"uuid"`
	Langcode    string         `yaml:"langcode"`
	Status      bool           `yaml:"status"`
	ID          string         `yaml:"id"`
	FieldName   string         `yaml:"field_name"`
	EntityType  string         `yaml:"entity_type"`
	Bundle      string         `yaml:"bundle"`
	Label       string         `yaml:"label"`
	Description string         `yaml:"description"`
	Required    bool           `yaml:"required"`
	FieldType   string         `yaml:"field_type"`
	Settings    map[string]any `yaml:"settings"`
}

// DrupalSiteInfo holds site-level information.
type DrupalSiteInfo struct {
	UUID string `yaml:"uuid"`
	Name string `yaml:"name"`
}

// CreateDrupalProfile creates a profile by parsing a Drupal config/sync directory.
func CreateDrupalProfile(name, configPath string) (*Profile, error) {
	// Verify the path exists
	info, err := os.Stat(configPath)
	if err != nil {
		return nil, fmt.Errorf("config path not accessible: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("config path is not a directory: %s", configPath)
	}

	// Try to get site info
	siteInfo := getSiteInfo(configPath)

	// Find all field configurations
	fields, err := parseFieldConfigs(configPath)
	if err != nil {
		return nil, fmt.Errorf("parsing field configs: %w", err)
	}

	// Build the profile
	profile := &Profile{
		Name:        name,
		Format:      "drupal",
		Description: fmt.Sprintf("Generated from Drupal config at %s", configPath),
		Source: SourceInfo{
			DrupalSiteUUID:   siteInfo.UUID,
			DrupalSiteName:   siteInfo.Name,
			ConfigPath:       configPath,
			FieldFingerprint: computeFieldFingerprint(fields),
		},
		Fields: make(map[string]FieldMapping),
		Options: Options{
			MultiValueSeparator: "|",
			StripHTML:           true,
			TaxonomyMode:        "passthrough",
		},
	}

	// Map fields to hub fields
	for _, field := range fields {
		mapping := mapDrupalField(field)
		if mapping != nil {
			profile.Fields[field.FieldName] = *mapping
		}
	}

	// Add core Drupal entity fields
	addCoreDrupalFields(profile)

	return profile, nil
}

func getSiteInfo(configPath string) DrupalSiteInfo {
	var info DrupalSiteInfo

	// Try system.site.yml
	sitePath := filepath.Join(configPath, "system.site.yml")
	data, err := os.ReadFile(sitePath)
	if err == nil {
		var siteConfig struct {
			UUID string `yaml:"uuid"`
			Name string `yaml:"name"`
		}
		if yaml.Unmarshal(data, &siteConfig) == nil {
			info.UUID = siteConfig.UUID
			info.Name = siteConfig.Name
		}
	}

	return info
}

func parseFieldConfigs(configPath string) ([]DrupalFieldConfig, error) {
	var fields []DrupalFieldConfig

	// Look for field.field.*.yml files
	pattern := filepath.Join(configPath, "field.field.*.yml")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var field DrupalFieldConfig
		if err := yaml.Unmarshal(data, &field); err != nil {
			continue
		}

		// Only include node fields for now
		if field.EntityType == "node" {
			fields = append(fields, field)
		}
	}

	return fields, nil
}

func computeFieldFingerprint(fields []DrupalFieldConfig) string {
	var names []string
	for _, f := range fields {
		names = append(names, f.FieldName)
	}
	sort.Strings(names)
	hash := sha256.Sum256([]byte(strings.Join(names, ",")))
	return fmt.Sprintf("%x", hash[:8])
}

// mapDrupalField maps a Drupal field to a hub field based on field name and type.
func mapDrupalField(field DrupalFieldConfig) *FieldMapping {
	name := field.FieldName
	fieldType := field.FieldType

	// Skip internal/system fields
	if !strings.HasPrefix(name, "field_") {
		return nil
	}

	mapping := &FieldMapping{}

	// Map by field name patterns
	switch {
	// Title fields
	case name == "field_full_title":
		mapping.Hub = "Title"
		mapping.Priority = 1
	case strings.Contains(name, "alt_title") || strings.Contains(name, "alternative_title"):
		mapping.Hub = "AltTitle"

	// Contributors
	case strings.Contains(name, "linked_agent") || strings.Contains(name, "contributor"):
		mapping.Hub = "Contributors"
		mapping.Type = "typed_relation"
		mapping.RoleField = "rel_type"
		mapping.Resolve = "taxonomy_term"

	// Dates
	case strings.Contains(name, "date_issued") || name == "field_edtf_date_issued":
		mapping.Hub = "Dates"
		mapping.DateType = "issued"
		mapping.Parser = "edtf"
	case strings.Contains(name, "date_created") || name == "field_edtf_date_created":
		mapping.Hub = "Dates"
		mapping.DateType = "created"
		mapping.Parser = "edtf"
	case strings.Contains(name, "date_captured") || name == "field_edtf_date_captured":
		mapping.Hub = "Dates"
		mapping.DateType = "captured"
		mapping.Parser = "edtf"
	case strings.Contains(name, "copyright_date"):
		mapping.Hub = "Dates"
		mapping.DateType = "copyright"
		mapping.Parser = "edtf"
	case strings.Contains(name, "date_modified"):
		mapping.Hub = "Dates"
		mapping.DateType = "modified"
		mapping.Parser = "edtf"

	// Resource type and genre
	case strings.Contains(name, "resource_type"):
		mapping.Hub = "ResourceType"
		mapping.Resolve = "taxonomy_term"
	case strings.Contains(name, "genre"):
		mapping.Hub = "Genre"
		mapping.Resolve = "taxonomy_term"
		mapping.MultiValue = true

	// Language
	case strings.Contains(name, "language"):
		mapping.Hub = "Language"
		mapping.Resolve = "taxonomy_term"

	// Rights
	case strings.Contains(name, "rights"):
		mapping.Hub = "Rights"
		if fieldType == "link" {
			mapping.Type = "uri"
		}

	// Descriptions
	case strings.Contains(name, "abstract"):
		mapping.Hub = "Abstract"
	case strings.Contains(name, "description"):
		mapping.Hub = "Description"
	case strings.Contains(name, "physical_description") || strings.Contains(name, "extent"):
		mapping.Hub = "PhysicalDesc"

	// Subjects
	case name == "field_subject" || strings.Contains(name, "subject_general"):
		mapping.Hub = "Subjects"
		mapping.Resolve = "taxonomy_term"
		mapping.MultiValue = true
	case strings.Contains(name, "lcsh"):
		mapping.Hub = "Subjects"
		mapping.Vocabulary = "lcsh"
		mapping.Resolve = "taxonomy_term"
		mapping.MultiValue = true
	case strings.Contains(name, "keyword"):
		mapping.Hub = "Subjects"
		mapping.Vocabulary = "keywords"
		mapping.MultiValue = true

	// Publisher
	case strings.Contains(name, "publisher"):
		mapping.Hub = "Publisher"
	case strings.Contains(name, "place_published"):
		mapping.Hub = "PlacePublished"

	// Relations
	case strings.Contains(name, "member_of"):
		mapping.Hub = "Relations"
		mapping.RelationType = "member_of"
		mapping.Resolve = "node"
	case strings.Contains(name, "related_item"):
		mapping.Hub = "Relations"
		mapping.RelationType = "related_to"

	// Identifiers
	case strings.Contains(name, "identifier"):
		mapping.Hub = "Identifiers"
	case strings.Contains(name, "pid"):
		mapping.Hub = "Identifiers"
		mapping.Type = "pid"
	case strings.Contains(name, "doi"):
		mapping.Hub = "Identifiers"
		mapping.Type = "doi"

	// Thesis fields
	case strings.Contains(name, "degree_name"):
		mapping.Hub = "DegreeInfo.DegreeName"
	case strings.Contains(name, "degree_level"):
		mapping.Hub = "DegreeInfo.DegreeLevel"
	case strings.Contains(name, "department"):
		mapping.Hub = "DegreeInfo.Department"
		mapping.Resolve = "taxonomy_term"
	case strings.Contains(name, "institution"):
		mapping.Hub = "DegreeInfo.Institution"

	// Notes and other
	case strings.Contains(name, "note"):
		mapping.Hub = "Notes"
		mapping.MultiValue = true
	case strings.Contains(name, "table_of_contents"):
		mapping.Hub = "TableOfContents"
	case strings.Contains(name, "source"):
		mapping.Hub = "Source"
	case strings.Contains(name, "digital_origin"):
		mapping.Hub = "DigitalOrigin"
		mapping.Resolve = "taxonomy_term"

	default:
		// Unknown field - store in Extra
		// Convert field_foo_bar to FooBar
		cleanName := strings.TrimPrefix(name, "field_")
		cleanName = toCamelCase(cleanName)
		mapping.Hub = "Extra." + cleanName
	}

	return mapping
}

func addCoreDrupalFields(profile *Profile) {
	core := map[string]FieldMapping{
		"nid":     {Hub: "Extra.nid"},
		"uuid":    {Hub: "Extra.uuid"},
		"vid":     {Hub: "Extra.vid"},
		"created": {Hub: "Extra.created"},
		"changed": {Hub: "Extra.changed"},
		"status":  {Hub: "Extra.status"},
		"type":    {Hub: "Extra.type"},
		"title":   {Hub: "Title", Priority: 0},
	}
	for k, v := range core {
		profile.Fields[k] = v
	}
}

func toCamelCase(s string) string {
	parts := regexp.MustCompile(`[_-]+`).Split(s, -1)
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, "")
}

// MatchDrupalProfile tries to find a matching profile for a Drupal export.
func MatchDrupalProfile(jsonData []byte) (*Profile, error) {
	profiles, err := List()
	if err != nil {
		return nil, err
	}

	for _, name := range profiles {
		p, err := Load(name)
		if err != nil {
			continue
		}
		if p.Format != "drupal" {
			continue
		}

		// Try to match by site UUID if present in the data
		// For now, we do simple field fingerprint matching
		if matchesDrupalProfile(p, jsonData) {
			return p, nil
		}
	}

	return nil, nil
}

func matchesDrupalProfile(p *Profile, jsonData []byte) bool {
	// Simple heuristic: check if the JSON contains field names from the profile
	jsonStr := string(jsonData)
	matchCount := 0
	totalFields := 0

	for fieldName := range p.Fields {
		if strings.HasPrefix(fieldName, "field_") {
			totalFields++
			if strings.Contains(jsonStr, `"`+fieldName+`"`) {
				matchCount++
			}
		}
	}

	if totalFields == 0 {
		return false
	}

	// Match if >50% of custom fields are present
	return float64(matchCount)/float64(totalFields) > 0.5
}
