// Package model defines portable, instance-specific metadata system models.
package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/internal/strictjson"
	"gopkg.in/yaml.v3"
)

// CurrentVersion is the model snapshot contract understood by this release.
const (
	CurrentVersion   = "1"
	maxSnapshotBytes = int64(32 << 20)
)

var systemNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ValueKind is the source-independent shape of a system field.
type ValueKind string

const (
	// ValueText contains textual data.
	ValueText ValueKind = "text"
	// ValueInteger contains signed integer data.
	ValueInteger ValueKind = "integer"
	// ValueDecimal contains decimal data.
	ValueDecimal ValueKind = "decimal"
	// ValueBoolean contains boolean data.
	ValueBoolean ValueKind = "boolean"
	// ValueDate contains date or date-range data.
	ValueDate ValueKind = "date"
	// ValueLink contains a URI and optional label.
	ValueLink ValueKind = "link"
	// ValueReference points at another system entity.
	ValueReference ValueKind = "reference"
	// ValueTypedReference points at another entity and carries a relation type.
	ValueTypedReference ValueKind = "typed_reference"
	// ValueComposite contains structured sub-values.
	ValueComposite ValueKind = "composite"
	// ValueFile contains a file reference.
	ValueFile ValueKind = "file"
	// ValueOpaque preserves an otherwise unknown source field shape.
	ValueOpaque ValueKind = "opaque"
)

// Fingerprint is a deterministic digest of schema-relevant model content.
type Fingerprint struct {
	Algorithm string `json:"algorithm" yaml:"algorithm"`
	Value     string `json:"value" yaml:"value"`
}

// Provenance records where a snapshot originated without affecting its model
// fingerprint. Operational paths, endpoints, and credentials do not belong here.
type Provenance struct {
	SiteUUID   string `json:"site_uuid,omitempty" yaml:"site_uuid,omitempty"`
	SiteName   string `json:"site_name,omitempty" yaml:"site_name,omitempty"`
	SourceID   string `json:"source_id,omitempty" yaml:"source_id,omitempty"`
	SourceURI  string `json:"source_uri,omitempty" yaml:"source_uri,omitempty"`
	ConfigHash string `json:"config_hash,omitempty" yaml:"config_hash,omitempty"`
}

// Snapshot is the portable data model for one configured metadata system.
type Snapshot struct {
	Version     string      `json:"version" yaml:"version"`
	System      string      `json:"system" yaml:"system"`
	Provenance  Provenance  `json:"provenance,omitempty" yaml:"provenance,omitempty"`
	Entities    []Entity    `json:"entities" yaml:"entities"`
	Fingerprint Fingerprint `json:"fingerprint" yaml:"fingerprint"`
}

// Entity describes one configured entity type and optional bundle or resource
// template. Systems without bundle semantics leave Bundle empty.
type Entity struct {
	EntityType    string   `json:"entity_type" yaml:"entity_type"`
	Bundle        string   `json:"bundle" yaml:"bundle"`
	Label         string   `json:"label,omitempty" yaml:"label,omitempty"`
	Description   string   `json:"description,omitempty" yaml:"description,omitempty"`
	SemanticTypes []string `json:"semantic_types,omitempty" yaml:"semantic_types,omitempty"`
	Fields        []Field  `json:"fields" yaml:"fields"`
}

// Field describes one configured field while retaining its native source type.
type Field struct {
	Path               string         `json:"path" yaml:"path"`
	Label              string         `json:"label,omitempty" yaml:"label,omitempty"`
	Description        string         `json:"description,omitempty" yaml:"description,omitempty"`
	SourceType         string         `json:"source_type" yaml:"source_type"`
	Kind               ValueKind      `json:"kind" yaml:"kind"`
	Cardinality        int            `json:"cardinality" yaml:"cardinality"`
	Required           bool           `json:"required,omitempty" yaml:"required,omitempty"`
	SemanticProperties []string       `json:"semantic_properties,omitempty" yaml:"semantic_properties,omitempty"`
	SemanticSettings   map[string]any `json:"semantic_settings,omitempty" yaml:"semantic_settings,omitempty"`
	Reference          *Reference     `json:"reference,omitempty" yaml:"reference,omitempty"`
	StorageSettings    map[string]any `json:"storage_settings,omitempty" yaml:"storage_settings,omitempty"`
	InstanceSettings   map[string]any `json:"instance_settings,omitempty" yaml:"instance_settings,omitempty"`
}

// Reference identifies the entity type and optional bundles accepted by a field.
type Reference struct {
	EntityType string   `json:"entity_type" yaml:"entity_type"`
	Bundles    []string `json:"bundles,omitempty" yaml:"bundles,omitempty"`
}

// Load decodes and validates one strict JSON or YAML snapshot.
func Load(r io.Reader) (*Snapshot, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxSnapshotBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading model snapshot: %w", err)
	}
	if int64(len(data)) > maxSnapshotBytes {
		return nil, fmt.Errorf("model snapshot exceeds %d bytes", maxSnapshotBytes)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("model snapshot is empty")
	}

	var snapshot Snapshot
	if trimmed[0] == '{' {
		if err := strictjson.RejectDuplicateNames(trimmed); err != nil {
			return nil, fmt.Errorf("decoding model JSON: %w", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&snapshot); err != nil {
			return nil, fmt.Errorf("decoding model JSON: %w", err)
		}
		if err := requireEOF(decoder.Decode(new(any))); err != nil {
			return nil, fmt.Errorf("decoding model JSON: %w", err)
		}
	} else {
		decoder := yaml.NewDecoder(bytes.NewReader(trimmed))
		decoder.KnownFields(true)
		if err := decoder.Decode(&snapshot); err != nil {
			return nil, fmt.Errorf("decoding model YAML: %w", err)
		}
		if err := requireEOF(decoder.Decode(new(any))); err != nil {
			return nil, fmt.Errorf("decoding model YAML: %w", err)
		}
	}
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// Validate checks the snapshot contract and its recorded fingerprint.
func (s *Snapshot) Validate() error {
	if s == nil {
		return fmt.Errorf("model snapshot is nil")
	}
	if s.Version != CurrentVersion {
		return fmt.Errorf("unsupported model version %q", s.Version)
	}
	if !systemNamePattern.MatchString(s.System) {
		return fmt.Errorf("model system %q is invalid", s.System)
	}
	if err := validateProvenance(s.Provenance); err != nil {
		return fmt.Errorf("model provenance: %w", err)
	}
	if len(s.Entities) == 0 {
		return fmt.Errorf("model has no entities")
	}
	seenEntities := make(map[string]struct{}, len(s.Entities))
	for entityIndex, entity := range s.Entities {
		if !validRequiredIdentity(entity.EntityType) {
			return fmt.Errorf("model entity %d requires entity_type", entityIndex+1)
		}
		if !validOptionalIdentity(entity.Bundle) {
			return fmt.Errorf("model entity %d has invalid bundle %q", entityIndex+1, entity.Bundle)
		}
		if err := validateUniqueStrings("semantic type", entity.SemanticTypes); err != nil {
			return fmt.Errorf("model entity %s/%s: %w", entity.EntityType, entity.Bundle, err)
		}
		entityKey := entity.EntityType + "\x00" + entity.Bundle
		if _, exists := seenEntities[entityKey]; exists {
			return fmt.Errorf("model repeats entity %s/%s", entity.EntityType, entity.Bundle)
		}
		seenEntities[entityKey] = struct{}{}
		seenFields := make(map[string]struct{}, len(entity.Fields))
		for fieldIndex, field := range entity.Fields {
			if !validRequiredIdentity(field.Path) {
				return fmt.Errorf("model entity %s/%s field %d has no path", entity.EntityType, entity.Bundle, fieldIndex+1)
			}
			if _, exists := seenFields[field.Path]; exists {
				return fmt.Errorf("model entity %s/%s repeats field %q", entity.EntityType, entity.Bundle, field.Path)
			}
			seenFields[field.Path] = struct{}{}
			if !validRequiredIdentity(field.SourceType) {
				return fmt.Errorf("model entity %s/%s field %q has no source_type", entity.EntityType, entity.Bundle, field.Path)
			}
			if err := validateUniqueStrings("semantic property", field.SemanticProperties); err != nil {
				return fmt.Errorf("model entity %s/%s field %q: %w", entity.EntityType, entity.Bundle, field.Path, err)
			}
			if !validValueKind(field.Kind) {
				return fmt.Errorf("model entity %s/%s field %q has invalid kind %q", entity.EntityType, entity.Bundle, field.Path, field.Kind)
			}
			if field.Cardinality == 0 || field.Cardinality < -1 {
				return fmt.Errorf("model entity %s/%s field %q has invalid cardinality %d", entity.EntityType, entity.Bundle, field.Path, field.Cardinality)
			}
			referenceKind := field.Kind == ValueReference || field.Kind == ValueTypedReference
			if referenceKind && field.Reference == nil {
				return fmt.Errorf("model entity %s/%s field %q requires a reference target", entity.EntityType, entity.Bundle, field.Path)
			}
			if !referenceKind && field.Reference != nil {
				return fmt.Errorf("model entity %s/%s field %q has a reference target for non-reference kind %q", entity.EntityType, entity.Bundle, field.Path, field.Kind)
			}
			if field.Reference != nil {
				if !validRequiredIdentity(field.Reference.EntityType) {
					return fmt.Errorf("model entity %s/%s field %q has a reference without entity_type", entity.EntityType, entity.Bundle, field.Path)
				}
				if err := validateUniqueStrings("reference bundle", field.Reference.Bundles); err != nil {
					return fmt.Errorf("model entity %s/%s field %q: %w", entity.EntityType, entity.Bundle, field.Path, err)
				}
			}
		}
	}
	if s.Fingerprint.Algorithm != "sha256" {
		return fmt.Errorf("model fingerprint algorithm must be sha256")
	}
	if len(s.Fingerprint.Value) != sha256.Size*2 {
		return fmt.Errorf("model fingerprint must contain a SHA-256 digest")
	}
	if _, err := hex.DecodeString(s.Fingerprint.Value); err != nil {
		return fmt.Errorf("model fingerprint must contain a SHA-256 digest")
	}
	if s.Fingerprint.Value != strings.ToLower(s.Fingerprint.Value) {
		return fmt.Errorf("model fingerprint must use lowercase hexadecimal")
	}
	computed, err := s.ComputeFingerprint()
	if err != nil {
		return err
	}
	if computed != s.Fingerprint.Value {
		return fmt.Errorf("model fingerprint mismatch: got %s, computed %s", s.Fingerprint.Value, computed)
	}
	return nil
}

func validateProvenance(provenance Provenance) error {
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{name: "site_uuid", value: provenance.SiteUUID, limit: 2048},
		{name: "site_name", value: provenance.SiteName, limit: 4096},
		{name: "source_id", value: provenance.SourceID, limit: 2048},
		{name: "config_hash", value: provenance.ConfigHash, limit: 4096},
	} {
		if len(field.value) > field.limit {
			return fmt.Errorf("%s exceeds %d bytes", field.name, field.limit)
		}
		if field.value != strings.TrimSpace(field.value) || strings.ContainsAny(field.value, "\x00\r\n") {
			return fmt.Errorf("%s is invalid", field.name)
		}
	}
	if provenance.SourceURI == "" {
		return nil
	}
	if len(provenance.SourceURI) > 4096 || provenance.SourceURI != strings.TrimSpace(provenance.SourceURI) || strings.ContainsAny(provenance.SourceURI, "\x00\r\n") {
		return fmt.Errorf("source_uri is invalid")
	}
	parsed, err := url.Parse(provenance.SourceURI)
	if err != nil || parsed.Scheme == "" || (parsed.Host == "" && parsed.Opaque == "") {
		return fmt.Errorf("source_uri must be an absolute URI")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("source_uri must not contain credentials, query, or fragment")
	}
	return nil
}

// ComputeFingerprint returns a deterministic digest of schema-relevant content.
// Entity, field, semantic-property, and reference-bundle order do not affect it.
func (s *Snapshot) ComputeFingerprint() (string, error) {
	if s == nil {
		return "", fmt.Errorf("model snapshot is nil")
	}
	canonical := canonicalSnapshot{
		Version:  s.Version,
		System:   s.System,
		Entities: cloneEntities(s.Entities),
	}
	sortEntities(canonical.Entities)
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encoding model fingerprint: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// SealFingerprint computes and records the current model digest.
func (s *Snapshot) SealFingerprint() error {
	value, err := s.ComputeFingerprint()
	if err != nil {
		return err
	}
	s.Fingerprint = Fingerprint{Algorithm: "sha256", Value: value}
	return nil
}

// Canonical returns a validated deep copy in deterministic entity, field,
// semantic-property, and reference-bundle order. The returned snapshot has the
// same fingerprint and can be serialized reproducibly without mutating the
// caller's model.
func (s *Snapshot) Canonical() (*Snapshot, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	result := *s
	result.Entities = cloneEntities(s.Entities)
	sortEntities(result.Entities)
	return &result, nil
}

// Entity returns a configured entity by type and bundle.
func (s *Snapshot) Entity(entityType, bundle string) (Entity, bool) {
	if s == nil {
		return Entity{}, false
	}
	for _, entity := range s.Entities {
		if entity.EntityType == entityType && entity.Bundle == bundle {
			return cloneEntity(entity), true
		}
	}
	return Entity{}, false
}

// Field returns a configured field by entity identity and path.
func (s *Snapshot) Field(entityType, bundle, fieldPath string) (Field, bool) {
	entity, ok := s.Entity(entityType, bundle)
	if !ok {
		return Field{}, false
	}
	for _, field := range entity.Fields {
		if field.Path == fieldPath {
			return field, true
		}
	}
	return Field{}, false
}

type canonicalSnapshot struct {
	Version  string   `json:"version"`
	System   string   `json:"system"`
	Entities []Entity `json:"entities"`
}

func validValueKind(kind ValueKind) bool {
	switch kind {
	case ValueText, ValueInteger, ValueDecimal, ValueBoolean, ValueDate, ValueLink,
		ValueReference, ValueTypedReference, ValueComposite, ValueFile, ValueOpaque:
		return true
	default:
		return false
	}
}

func validRequiredIdentity(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

func validOptionalIdentity(value string) bool {
	return value == "" || validRequiredIdentity(value)
}

func validateUniqueStrings(label string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if !validRequiredIdentity(value) {
			return fmt.Errorf("%s %d is invalid", label, index+1)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s %q is repeated", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func cloneEntities(input []Entity) []Entity {
	result := make([]Entity, len(input))
	for index, entity := range input {
		result[index] = cloneEntity(entity)
	}
	return result
}

func cloneEntity(entity Entity) Entity {
	result := entity
	result.SemanticTypes = append([]string(nil), entity.SemanticTypes...)
	result.Fields = make([]Field, len(entity.Fields))
	for index, field := range entity.Fields {
		result.Fields[index] = cloneField(field)
	}
	return result
}

func cloneField(field Field) Field {
	result := field
	result.SemanticProperties = append([]string(nil), field.SemanticProperties...)
	result.SemanticSettings = cloneMap(field.SemanticSettings)
	result.StorageSettings = cloneMap(field.StorageSettings)
	result.InstanceSettings = cloneMap(field.InstanceSettings)
	if field.Reference != nil {
		reference := *field.Reference
		reference.Bundles = append([]string(nil), field.Reference.Bundles...)
		result.Reference = &reference
	}
	return result
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = cloneValue(value)
	}
	return result
}

func cloneValue(value any) any {
	if value == nil {
		return nil
	}
	return cloneReflectValue(reflect.ValueOf(value)).Interface()
}

func cloneReflectValue(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.New(value.Type()).Elem()
		result.Set(cloneReflectValue(value.Elem()))
		return result
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			result.SetMapIndex(iterator.Key(), cloneReflectValue(iterator.Value()))
		}
		return result
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(cloneReflectValue(value.Elem()))
		return result
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := range value.Len() {
			result.Index(index).Set(cloneReflectValue(value.Index(index)))
		}
		return result
	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		for index := range value.Len() {
			result.Index(index).Set(cloneReflectValue(value.Index(index)))
		}
		return result
	default:
		return value
	}
}

func sortEntities(entities []Entity) {
	for entityIndex := range entities {
		entity := &entities[entityIndex]
		sort.Strings(entity.SemanticTypes)
		for fieldIndex := range entity.Fields {
			field := &entity.Fields[fieldIndex]
			sort.Strings(field.SemanticProperties)
			if field.Reference != nil {
				sort.Strings(field.Reference.Bundles)
			}
		}
		sort.Slice(entity.Fields, func(left, right int) bool {
			return entity.Fields[left].Path < entity.Fields[right].Path
		})
	}
	sort.Slice(entities, func(left, right int) bool {
		if entities[left].EntityType == entities[right].EntityType {
			return entities[left].Bundle < entities[right].Bundle
		}
		return entities[left].EntityType < entities[right].EntityType
	})
}

func requireEOF(err error) error {
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("multiple documents are not allowed")
	}
	return err
}
