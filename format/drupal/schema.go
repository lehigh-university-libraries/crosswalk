package drupal

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/schema"
	"gopkg.in/yaml.v3"
)

// SchemaLoader loads Drupal schema definitions from config sync files.
type SchemaLoader struct {
	registry *schema.Registry
}

// NewSchemaLoader creates a new schema loader with the given registry.
// If registry is nil, a new one is created.
func NewSchemaLoader(r *schema.Registry) *SchemaLoader {
	if r == nil {
		r = schema.NewRegistry()
	}
	return &SchemaLoader{registry: r}
}

// Registry returns the underlying schema registry.
func (l *SchemaLoader) Registry() *schema.Registry {
	return l.registry
}

// LoadFromConfigSync loads schema definitions from a Drupal config sync directory.
// It parses field.storage.*.yml and field.field.*.yml files.
func (l *SchemaLoader) LoadFromConfigSync(dir string) error {
	// Load field storage configs first (defines field types)
	storageTypes := make(map[string]string) // field name -> field type

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "field.storage.") && strings.HasSuffix(name, ".yml") {
			if err := l.loadFieldStorage(path, storageTypes); err != nil {
				return fmt.Errorf("loading %s: %w", path, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Load field field configs (assigns fields to bundles)
	fieldsByBundle := make(map[string]map[string][]fieldConfig) // entityType -> bundle -> fields

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "field.field.") && strings.HasSuffix(name, ".yml") {
			if err := l.loadFieldField(path, storageTypes, fieldsByBundle); err != nil {
				return fmt.Errorf("loading %s: %w", path, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Build schema entities
	for entityType, bundles := range fieldsByBundle {
		for bundle, fields := range bundles {
			entity := &schema.Entity{
				EntityType: entityType,
				Bundle:     bundle,
				Name:       bundle,
				Fields:     make([]schema.Field, 0, len(fields)),
			}
			for _, fc := range fields {
				entity.Fields = append(entity.Fields, fc.toSchemaField())
			}
			l.registry.Register(entity)
		}
	}

	return nil
}

// fieldStorageConfig represents a Drupal field.storage.*.yml file.
type fieldStorageConfig struct {
	ID          string `yaml:"id"`
	FieldName   string `yaml:"field_name"`
	EntityType  string `yaml:"entity_type"`
	Type        string `yaml:"type"`
	Cardinality int    `yaml:"cardinality"`
}

// fieldFieldConfig represents a Drupal field.field.*.yml file.
type fieldFieldConfig struct {
	ID          string `yaml:"id"`
	FieldName   string `yaml:"field_name"`
	EntityType  string `yaml:"entity_type"`
	Bundle      string `yaml:"bundle"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	FieldType   string `yaml:"field_type"`
}

// fieldConfig is our internal representation during loading.
type fieldConfig struct {
	Name        string
	SourceType  string
	Label       string
	Description string
	Required    bool
	Cardinality int
}

func (fc fieldConfig) toSchemaField() schema.Field {
	return schema.Field{
		Name:        fc.Name,
		Type:        drupalTypeToSchemaType(fc.SourceType),
		SourceType:  fc.SourceType,
		Cardinality: schema.Cardinality(fc.Cardinality),
		Required:    fc.Required,
		Label:       fc.Label,
		Description: fc.Description,
	}
}

func (l *SchemaLoader) loadFieldStorage(path string, storageTypes map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cfg fieldStorageConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}

	// Store the field type keyed by entity_type.field_name
	key := cfg.EntityType + "." + cfg.FieldName
	storageTypes[key] = cfg.Type

	return nil
}

func (l *SchemaLoader) loadFieldField(path string, storageTypes map[string]string, fieldsByBundle map[string]map[string][]fieldConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cfg fieldFieldConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}

	// Look up the field type from storage
	storageKey := cfg.EntityType + "." + cfg.FieldName
	sourceType := storageTypes[storageKey]
	if sourceType == "" {
		sourceType = cfg.FieldType // Fallback to field_type in field config
	}

	fc := fieldConfig{
		Name:        cfg.FieldName,
		SourceType:  sourceType,
		Label:       cfg.Label,
		Description: cfg.Description,
		Required:    cfg.Required,
		Cardinality: 1, // Default, could be enhanced to read from storage
	}

	// Initialize maps if needed
	if fieldsByBundle[cfg.EntityType] == nil {
		fieldsByBundle[cfg.EntityType] = make(map[string][]fieldConfig)
	}
	fieldsByBundle[cfg.EntityType][cfg.Bundle] = append(fieldsByBundle[cfg.EntityType][cfg.Bundle], fc)

	return nil
}

// drupalTypeToSchemaType maps Drupal field types to our schema types.
func drupalTypeToSchemaType(drupalType string) schema.FieldType {
	switch drupalType {
	case "string", "string_long", "text", "text_long", "text_with_summary", "list_string":
		return schema.FieldText
	case "integer", "list_integer":
		return schema.FieldInt
	case "boolean":
		return schema.FieldBool
	case "datetime", "daterange", "edtf":
		return schema.FieldDate
	case "entity_reference":
		return schema.FieldRef
	case "typed_relation":
		return schema.FieldTypedRef
	case "link":
		return schema.FieldLink
	case "file":
		return schema.FieldFile
	case "image":
		return schema.FieldImage
	case "paragraph", "entity_reference_revisions":
		return schema.FieldComposite
	default:
		return schema.FieldText // Default to text for unknown types
	}
}

// LoadFromSchemaYAML loads schema definitions from a schema YAML file.
// This uses our custom schema format, not Drupal config sync format.
func (l *SchemaLoader) LoadFromSchemaYAML(path string) error {
	return l.registry.LoadFromPath(path)
}
