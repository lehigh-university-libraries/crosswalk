package profile

import (
	"fmt"
)

// CreateWorkbenchProfile creates a mapping profile for the islandora-workbench format
// by parsing a Drupal config/sync directory. The profile maps Drupal field names
// (used as column headers in Workbench CSV) to hub fields.
func CreateWorkbenchProfile(name, bundle, configPath string) (*Profile, error) {
	fields, err := parseFieldConfigsForBundle(configPath, bundle)
	if err != nil {
		return nil, fmt.Errorf("parsing field configs: %w", err)
	}

	siteInfo := getSiteInfo(configPath)

	p := &Profile{
		Name:        name,
		Format:      "islandora-workbench",
		Description: fmt.Sprintf("Generated from Drupal config at %s (bundle: %s)", configPath, bundle),
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

	for _, field := range fields {
		mapping := mapDrupalField(field)
		if mapping != nil {
			p.Fields[field.FieldName] = *mapping
		}
	}

	// Add workbench reserved columns that map to core hub fields.
	// These are always present in a workbench CSV regardless of the bundle config.
	p.Fields["title"] = FieldMapping{Hub: "Title", Priority: 0}

	return p, nil
}

// parseFieldConfigsForBundle reads field.field.<entityType>.<bundle>.yml files.
// When bundle is empty, all node fields are included (same as parseFieldConfigs).
func parseFieldConfigsForBundle(configPath, bundle string) ([]DrupalFieldConfig, error) {
	if bundle == "" {
		return parseFieldConfigs(configPath)
	}

	all, err := parseFieldConfigs(configPath)
	if err != nil {
		return nil, err
	}

	var filtered []DrupalFieldConfig
	for _, f := range all {
		if f.Bundle == bundle {
			filtered = append(filtered, f)
		}
	}
	return filtered, nil
}
