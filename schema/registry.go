package schema

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Registry holds schema definitions for dynamic formats.
// It organizes entities by entity_type and bundle.
type Registry struct {
	mu sync.RWMutex
	// entities[entityType][bundle] = *Entity
	entities map[string]map[string]*Entity
}

// NewRegistry creates an empty schema registry.
func NewRegistry() *Registry {
	return &Registry{
		entities: make(map[string]map[string]*Entity),
	}
}

// Register adds or updates an entity definition.
// If an entity with the same type and bundle exists, it will be replaced.
func (r *Registry) Register(e *Entity) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entities[e.EntityType] == nil {
		r.entities[e.EntityType] = make(map[string]*Entity)
	}
	r.entities[e.EntityType][e.Bundle] = e
}

// Get retrieves an entity definition by type and bundle.
func (r *Registry) Get(entityType, bundle string) (*Entity, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.entities[entityType] == nil {
		return nil, false
	}
	e, ok := r.entities[entityType][bundle]
	return e, ok
}

// GetField retrieves a field definition.
func (r *Registry) GetField(entityType, bundle, fieldName string) (*Field, bool) {
	e, ok := r.Get(entityType, bundle)
	if !ok {
		return nil, false
	}
	return e.GetField(fieldName)
}

// GetFieldType returns the normalized type of a field.
func (r *Registry) GetFieldType(entityType, bundle, fieldName string) (FieldType, bool) {
	f, ok := r.GetField(entityType, bundle, fieldName)
	if !ok {
		return "", false
	}
	return f.Type, true
}

// GetSourceType returns the source type of a field.
func (r *Registry) GetSourceType(entityType, bundle, fieldName string) (string, bool) {
	f, ok := r.GetField(entityType, bundle, fieldName)
	if !ok {
		return "", false
	}
	return f.SourceType, true
}

// ListEntityTypes returns all registered entity types.
func (r *Registry) ListEntityTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.entities))
	for t := range r.entities {
		types = append(types, t)
	}
	return types
}

// ListBundles returns all bundles for an entity type.
func (r *Registry) ListBundles(entityType string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.entities[entityType] == nil {
		return nil
	}
	bundles := make([]string, 0, len(r.entities[entityType]))
	for b := range r.entities[entityType] {
		bundles = append(bundles, b)
	}
	return bundles
}

// Validate checks if an entity has all required fields.
func (r *Registry) Validate(entityType, bundle string, hasField func(string) bool) []string {
	e, ok := r.Get(entityType, bundle)
	if !ok {
		return nil // Unknown entity - can't validate
	}
	return e.Validate(hasField)
}

// Merge combines another registry into this one.
// Entities from the other registry will override existing entities.
func (r *Registry) Merge(other *Registry) {
	other.mu.RLock()
	defer other.mu.RUnlock()

	for _, bundles := range other.entities {
		for _, entity := range bundles {
			r.Register(entity)
		}
	}
}

// =============================================================================
// YAML LOADING
// =============================================================================

// EntityConfig is the top-level YAML config format.
type EntityConfig struct {
	Version  string   `yaml:"version"`
	Entities []Entity `yaml:"entities"`
}

// LoadFromYAML loads entity definitions from YAML bytes.
func (r *Registry) LoadFromYAML(data []byte) error {
	var config EntityConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	for i := range config.Entities {
		r.Register(&config.Entities[i])
	}
	return nil
}

// LoadFromPath loads entity definitions from a file or directory.
func (r *Registry) LoadFromPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if info.IsDir() {
		return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !isYAMLFile(p) {
				return nil
			}
			return r.loadFile(p)
		})
	}

	return r.loadFile(path)
}

// LoadEmbedded loads entity definitions from an embedded filesystem.
func (r *Registry) LoadEmbedded(fsys embed.FS, dir string) error {
	return fs.WalkDir(fsys, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !isYAMLFile(path) {
			return nil
		}

		data, err := fsys.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		return r.LoadFromYAML(data)
	})
}

func (r *Registry) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := r.LoadFromYAML(data); err != nil {
		return fmt.Errorf("loading %s: %w", path, err)
	}
	return nil
}

func isYAMLFile(path string) bool {
	return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")
}
