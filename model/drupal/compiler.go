// Package drupal compiles Drupal configuration exports into portable models.
package drupal

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/model"
	"gopkg.in/yaml.v3"
)

const (
	defaultMaxConfigFileBytes = int64(2 << 20)
	defaultMaxConfigBytes     = int64(32 << 20)
	defaultMaxConfigFiles     = 10_000
)

// CompileOptions bounds input accepted by the Drupal model compiler.
type CompileOptions struct {
	MaxFileBytes int64
	MaxBytes     int64
	MaxFiles     int
}

// ConfigFile is one named Drupal config document. Name must be a base filename,
// not an operational filesystem path.
type ConfigFile struct {
	Name string
	Data []byte
}

// CompileDirectory compiles all supported configs in one config/sync directory.
// Symlinks and non-regular matching files are rejected.
func CompileDirectory(dir string, options CompileOptions) (*model.Snapshot, error) {
	options = normalizeOptions(options)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("opening Drupal config directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("reading Drupal config directory: %w", err)
	}

	files := make([]ConfigFile, 0)
	var total int64
	for _, entry := range entries {
		if !isSupportedConfigName(entry.Name()) {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("Drupal config %q is not a regular file", entry.Name())
		}
		if len(files) >= options.MaxFiles {
			return nil, fmt.Errorf("Drupal config exceeds %d files", options.MaxFiles)
		}
		file, err := root.Open(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("opening Drupal config %q: %w", entry.Name(), err)
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			return nil, fmt.Errorf("Drupal config %q is not a regular file", entry.Name())
		}
		data, readErr := readBounded(file, options.MaxFileBytes)
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("reading Drupal config %q: %w", entry.Name(), readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("closing Drupal config %q: %w", entry.Name(), closeErr)
		}
		total += int64(len(data))
		if total > options.MaxBytes {
			return nil, fmt.Errorf("Drupal config exceeds %d total bytes", options.MaxBytes)
		}
		files = append(files, ConfigFile{Name: entry.Name(), Data: data})
	}
	return Compile(files, options)
}

// CompileArchive compiles a gzip-compressed tar config export without
// extracting it. Wrapper directories are accepted; links and unsafe paths are
// rejected.
func CompileArchive(r io.Reader, options CompileOptions) (*model.Snapshot, error) {
	options = normalizeOptions(options)
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("opening Drupal config archive: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	streamLimit := options.MaxBytes + int64(options.MaxFiles)*2048 + (1 << 20)
	limitedStream := &io.LimitedReader{R: gzipReader, N: streamLimit + 1}
	reader := tar.NewReader(limitedStream)
	files := make([]ConfigFile, 0)
	seen := make(map[string]struct{})
	var total int64
	memberCount := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading Drupal config archive: %w", err)
		}
		memberCount++
		if memberCount > options.MaxFiles {
			return nil, fmt.Errorf("Drupal config archive exceeds %d members", options.MaxFiles)
		}
		cleanName, err := safeArchiveName(header.Name)
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("Drupal config archive member %q is not a regular file", header.Name)
		}
		if header.Size < 0 || header.Size > options.MaxFileBytes {
			return nil, fmt.Errorf("Drupal config archive member %q exceeds %d bytes", header.Name, options.MaxFileBytes)
		}
		if header.Size > options.MaxBytes-total {
			return nil, fmt.Errorf("Drupal config archive exceeds %d uncompressed bytes", options.MaxBytes)
		}
		total += header.Size
		name := path.Base(cleanName)
		if !isSupportedConfigName(name) {
			continue
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("Drupal config archive contains duplicate config %q", name)
		}
		data, err := readBounded(reader, options.MaxFileBytes)
		if err != nil {
			return nil, fmt.Errorf("reading Drupal config archive member %q: %w", header.Name, err)
		}
		seen[name] = struct{}{}
		files = append(files, ConfigFile{Name: name, Data: data})
	}
	if limitedStream.N == 0 {
		return nil, fmt.Errorf("Drupal config archive exceeds %d expanded bytes", streamLimit)
	}
	return Compile(files, options)
}

// Compile builds a model from an already acquired config snapshot. It performs
// no filesystem or network access, which lets an operator supply the same input
// from a Drupal export, API, or another controlled transport.
func Compile(configs []ConfigFile, options CompileOptions) (*model.Snapshot, error) {
	options = normalizeOptions(options)
	if len(configs) == 0 {
		return nil, fmt.Errorf("Drupal config snapshot is empty")
	}
	if len(configs) > options.MaxFiles {
		return nil, fmt.Errorf("Drupal config exceeds %d files", options.MaxFiles)
	}

	files := make(map[string][]byte, len(configs))
	var total int64
	for _, config := range configs {
		if config.Name == "" || config.Name == "." || config.Name == ".." || path.IsAbs(config.Name) || config.Name != path.Base(config.Name) || strings.Contains(config.Name, `\`) || strings.ContainsAny(config.Name, "\x00\r\n") {
			return nil, fmt.Errorf("unsafe Drupal config name %q", config.Name)
		}
		if int64(len(config.Data)) > options.MaxFileBytes {
			return nil, fmt.Errorf("Drupal config %q exceeds %d bytes", config.Name, options.MaxFileBytes)
		}
		total += int64(len(config.Data))
		if total > options.MaxBytes {
			return nil, fmt.Errorf("Drupal config exceeds %d total bytes", options.MaxBytes)
		}
		if !isSupportedConfigName(config.Name) {
			continue
		}
		if _, exists := files[config.Name]; exists {
			return nil, fmt.Errorf("Drupal config snapshot repeats %q", config.Name)
		}
		files[config.Name] = append([]byte(nil), config.Data...)
	}
	return compileFiles(files)
}

type storageConfig struct {
	ID          string         `yaml:"id"`
	FieldName   string         `yaml:"field_name"`
	EntityType  string         `yaml:"entity_type"`
	Type        string         `yaml:"type"`
	Cardinality int            `yaml:"cardinality"`
	Settings    map[string]any `yaml:"settings"`
}

type fieldConfig struct {
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

type siteConfig struct {
	UUID string `yaml:"uuid"`
	Name string `yaml:"name"`
}

type rdfConfig struct {
	ID               string                     `yaml:"id"`
	TargetEntityType string                     `yaml:"targetEntityType"`
	Bundle           string                     `yaml:"bundle"`
	Types            []string                   `yaml:"types"`
	FieldMappings    map[string]rdfFieldMapping `yaml:"fieldMappings"`
}

type rdfFieldMapping struct {
	Properties       []string       `yaml:"properties"`
	MappingType      string         `yaml:"mapping_type"`
	DatatypeCallback map[string]any `yaml:"datatype_callback"`
}

type entityKey struct {
	entityType string
	bundle     string
}

type fieldKey struct {
	entityType string
	field      string
}

func compileFiles(files map[string][]byte) (*model.Snapshot, error) {
	storages := make(map[fieldKey]storageConfig)
	fields := make(map[entityKey]map[string]fieldConfig)
	rdfMappings := make(map[entityKey]rdfConfig)
	site := siteConfig{}

	for name, data := range files {
		switch {
		case name == "system.site.yml":
			if err := decodeConfig(data, &site); err != nil {
				return nil, fmt.Errorf("decoding Drupal config %q: %w", name, err)
			}
		case strings.HasPrefix(name, "field.storage."):
			var storage storageConfig
			if err := decodeConfig(data, &storage); err != nil {
				return nil, fmt.Errorf("decoding Drupal config %q: %w", name, err)
			}
			if err := validateStorageName(name, storage); err != nil {
				return nil, err
			}
			if storage.Type == "" {
				return nil, fmt.Errorf("Drupal field storage %q has no type", name)
			}
			if storage.Cardinality == 0 || storage.Cardinality < -1 {
				return nil, fmt.Errorf("Drupal field storage %q has invalid cardinality %d", name, storage.Cardinality)
			}
			key := fieldKey{entityType: storage.EntityType, field: storage.FieldName}
			if _, exists := storages[key]; exists {
				return nil, fmt.Errorf("Drupal config repeats storage for %s.%s", storage.EntityType, storage.FieldName)
			}
			storages[key] = storage
		case strings.HasPrefix(name, "field.field."):
			var field fieldConfig
			if err := decodeConfig(data, &field); err != nil {
				return nil, fmt.Errorf("decoding Drupal config %q: %w", name, err)
			}
			if err := validateFieldName(name, field); err != nil {
				return nil, err
			}
			key := entityKey{entityType: field.EntityType, bundle: field.Bundle}
			if fields[key] == nil {
				fields[key] = make(map[string]fieldConfig)
			}
			if _, exists := fields[key][field.FieldName]; exists {
				return nil, fmt.Errorf("Drupal config repeats field %s.%s.%s", field.EntityType, field.Bundle, field.FieldName)
			}
			fields[key][field.FieldName] = field
		case strings.HasPrefix(name, "rdf.mapping."):
			var mapping rdfConfig
			if err := decodeConfig(data, &mapping); err != nil {
				return nil, fmt.Errorf("decoding Drupal config %q: %w", name, err)
			}
			if err := validateRDFName(name, mapping); err != nil {
				return nil, err
			}
			key := entityKey{entityType: mapping.TargetEntityType, bundle: mapping.Bundle}
			if _, exists := rdfMappings[key]; exists {
				return nil, fmt.Errorf("Drupal config repeats RDF mapping for %s.%s", mapping.TargetEntityType, mapping.Bundle)
			}
			rdfMappings[key] = mapping
		}
	}

	keys := make(map[entityKey]struct{}, len(fields)+len(rdfMappings))
	for key := range fields {
		keys[key] = struct{}{}
	}
	for key := range rdfMappings {
		keys[key] = struct{}{}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("Drupal config contains no bundle fields or RDF mappings")
	}

	entities := make([]model.Entity, 0, len(keys))
	for key := range keys {
		entity, err := compileEntity(key, fields[key], rdfMappings[key], storages)
		if err != nil {
			return nil, err
		}
		entities = append(entities, entity)
	}
	sort.Slice(entities, func(left, right int) bool {
		if entities[left].EntityType == entities[right].EntityType {
			return entities[left].Bundle < entities[right].Bundle
		}
		return entities[left].EntityType < entities[right].EntityType
	})

	snapshot := &model.Snapshot{
		Version: model.CurrentVersion,
		System:  "drupal",
		Provenance: model.Provenance{
			SiteUUID: site.UUID, SiteName: site.Name, ConfigHash: hashFiles(files),
		},
		Entities: entities,
	}
	if err := snapshot.SealFingerprint(); err != nil {
		return nil, err
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("validating compiled Drupal model: %w", err)
	}
	return snapshot, nil
}

func compileEntity(key entityKey, configured map[string]fieldConfig, rdf rdfConfig, storages map[fieldKey]storageConfig) (model.Entity, error) {
	entity := model.Entity{
		EntityType: key.entityType,
		Bundle:     key.bundle,
		Label:      key.bundle,
		Fields:     make([]model.Field, 0, len(configured)+len(rdf.FieldMappings)),
	}
	entity.SemanticTypes = uniqueSorted(rdf.Types)
	names := make(map[string]struct{}, len(configured)+len(rdf.FieldMappings))
	for name := range configured {
		names[name] = struct{}{}
	}
	for name := range rdf.FieldMappings {
		names[name] = struct{}{}
	}
	orderedNames := make([]string, 0, len(names))
	for name := range names {
		orderedNames = append(orderedNames, name)
	}
	sort.Strings(orderedNames)

	for _, name := range orderedNames {
		instance, attached := configured[name]
		semantic := rdf.FieldMappings[name]
		if !attached {
			entity.Fields = append(entity.Fields, coreField(name, semantic))
			continue
		}
		storage, exists := storages[fieldKey{entityType: key.entityType, field: name}]
		if !exists {
			return model.Entity{}, fmt.Errorf("Drupal field %s.%s.%s has no field.storage config", key.entityType, key.bundle, name)
		}
		if instance.FieldType != "" && instance.FieldType != storage.Type {
			return model.Entity{}, fmt.Errorf("Drupal field %s.%s.%s type %q does not match storage type %q", key.entityType, key.bundle, name, instance.FieldType, storage.Type)
		}
		field := model.Field{
			Path:               name,
			Label:              instance.Label,
			Description:        instance.Description,
			SourceType:         storage.Type,
			Kind:               valueKind(storage.Type),
			Cardinality:        storage.Cardinality,
			Required:           instance.Required,
			SemanticProperties: uniqueSorted(semantic.Properties),
			SemanticSettings:   semanticSettings(semantic),
			StorageSettings:    cloneMap(storage.Settings),
			InstanceSettings:   cloneMap(instance.Settings),
		}
		field.Reference = referenceFor(field.Kind, storage.Settings, instance.Settings)
		entity.Fields = append(entity.Fields, field)
	}
	return entity, nil
}

func coreField(name string, semantic rdfFieldMapping) model.Field {
	kind := model.ValueOpaque
	sourceType := "base_field"
	var reference *model.Reference
	switch name {
	case "title", "name", "label":
		kind = model.ValueText
		sourceType = "string"
	case "created", "changed":
		kind = model.ValueDate
		sourceType = name
	case "status", "sticky", "promote":
		kind = model.ValueBoolean
		sourceType = "boolean"
	case "uid":
		kind = model.ValueReference
		sourceType = "entity_reference"
		reference = &model.Reference{EntityType: "user"}
	case "uuid":
		kind = model.ValueText
		sourceType = "uuid"
	}
	return model.Field{
		Path: name, Label: name, SourceType: sourceType, Kind: kind,
		Cardinality: 1, SemanticProperties: uniqueSorted(semantic.Properties),
		SemanticSettings: semanticSettings(semantic), Reference: reference,
	}
}

func semanticSettings(mapping rdfFieldMapping) map[string]any {
	settings := make(map[string]any)
	if mapping.MappingType != "" {
		settings["mapping_type"] = mapping.MappingType
	}
	if len(mapping.DatatypeCallback) > 0 {
		settings["datatype_callback"] = cloneMap(mapping.DatatypeCallback)
	}
	if len(settings) == 0 {
		return nil
	}
	return settings
}

func referenceFor(kind model.ValueKind, storageSettings, instanceSettings map[string]any) *model.Reference {
	if kind != model.ValueReference && kind != model.ValueTypedReference {
		return nil
	}
	entityType, _ := storageSettings["target_type"].(string)
	if entityType == "" {
		entityType, _ = instanceSettings["target_type"].(string)
	}
	bundles := make([]string, 0)
	if handler, ok := instanceSettings["handler_settings"].(map[string]any); ok {
		bundles = append(bundles, mapKeys(handler["target_bundles"])...)
	}
	if entityType == "" && len(bundles) == 0 {
		return nil
	}
	return &model.Reference{EntityType: entityType, Bundles: uniqueSorted(bundles)}
}

func mapKeys(value any) []string {
	result := make([]string, 0)
	switch typed := value.(type) {
	case map[string]any:
		for key, enabled := range typed {
			if enabled == nil || fmt.Sprint(enabled) == "0" || fmt.Sprint(enabled) == "false" {
				continue
			}
			result = append(result, key)
		}
	case []any:
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" {
				result = append(result, value)
			}
		}
	}
	return result
}

func valueKind(sourceType string) model.ValueKind {
	switch sourceType {
	case "string", "string_long", "text", "text_long", "text_with_summary", "list_string", "email", "telephone":
		return model.ValueText
	case "integer", "list_integer":
		return model.ValueInteger
	case "decimal", "float":
		return model.ValueDecimal
	case "boolean":
		return model.ValueBoolean
	case "datetime", "daterange", "edtf", "timestamp":
		return model.ValueDate
	case "link":
		return model.ValueLink
	case "entity_reference":
		return model.ValueReference
	case "typed_relation":
		return model.ValueTypedReference
	case "file", "image":
		return model.ValueFile
	case "paragraph", "entity_reference_revisions", "related_item", "part_detail", "textfield_attr", "textarea_attr":
		return model.ValueComposite
	default:
		return model.ValueOpaque
	}
}

func validateStorageName(name string, config storageConfig) error {
	want := "field.storage." + config.EntityType + "." + config.FieldName + ".yml"
	if !validMachineName(config.EntityType) || !validMachineName(config.FieldName) || name != want {
		return fmt.Errorf("Drupal field storage %q has invalid identity", name)
	}
	if config.ID != "" && config.ID != config.EntityType+"."+config.FieldName {
		return fmt.Errorf("Drupal field storage %q has mismatched id %q", name, config.ID)
	}
	return nil
}

func validateFieldName(name string, config fieldConfig) error {
	want := "field.field." + config.EntityType + "." + config.Bundle + "." + config.FieldName + ".yml"
	if !validMachineName(config.EntityType) || !validMachineName(config.Bundle) || !validMachineName(config.FieldName) || name != want {
		return fmt.Errorf("Drupal field config %q has invalid identity", name)
	}
	if config.ID != "" && config.ID != config.EntityType+"."+config.Bundle+"."+config.FieldName {
		return fmt.Errorf("Drupal field config %q has mismatched id %q", name, config.ID)
	}
	return nil
}

func validateRDFName(name string, config rdfConfig) error {
	want := "rdf.mapping." + config.TargetEntityType + "." + config.Bundle + ".yml"
	if !validMachineName(config.TargetEntityType) || !validMachineName(config.Bundle) || name != want {
		return fmt.Errorf("Drupal RDF mapping %q has invalid identity", name)
	}
	if config.ID != "" && config.ID != config.TargetEntityType+"."+config.Bundle {
		return fmt.Errorf("Drupal RDF mapping %q has mismatched id %q", name, config.ID)
	}
	for fieldName := range config.FieldMappings {
		if !validMachineName(fieldName) {
			return fmt.Errorf("Drupal RDF mapping %q contains invalid field %q", name, fieldName)
		}
	}
	return nil
}

func decodeConfig(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple YAML documents are not allowed")
		}
		return err
	}
	return nil
}

func isSupportedConfigName(name string) bool {
	return name == "system.site.yml" ||
		(strings.HasPrefix(name, "field.storage.") && strings.HasSuffix(name, ".yml")) ||
		(strings.HasPrefix(name, "field.field.") && strings.HasSuffix(name, ".yml")) ||
		(strings.HasPrefix(name, "rdf.mapping.") && strings.HasSuffix(name, ".yml"))
}

func validMachineName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func normalizeOptions(options CompileOptions) CompileOptions {
	if options.MaxFileBytes <= 0 {
		options.MaxFileBytes = defaultMaxConfigFileBytes
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = defaultMaxConfigBytes
	}
	if options.MaxFiles <= 0 {
		options.MaxFiles = defaultMaxConfigFiles
	}
	return options
}

func readBounded(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file exceeds %d bytes", max)
	}
	return data, nil
}

func safeArchiveName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, `\`) || path.IsAbs(name) {
		return "", fmt.Errorf("unsafe Drupal config archive path %q", name)
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe Drupal config archive path %q", name)
	}
	return clean, nil
}

func hashFiles(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		_, _ = io.WriteString(hash, name)
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(files[name])
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		switch typed := value.(type) {
		case map[string]any:
			result[key] = cloneMap(typed)
		case []any:
			copy := make([]any, len(typed))
			for index, item := range typed {
				if nested, ok := item.(map[string]any); ok {
					copy[index] = cloneMap(nested)
				} else {
					copy[index] = item
				}
			}
			result[key] = copy
		default:
			result[key] = value
		}
	}
	return result
}
