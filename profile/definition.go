package profile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/internal/strictjson"
	"github.com/lehigh-university-libraries/crosswalk/model"
	"gopkg.in/yaml.v3"
)

const (
	// CurrentDefinitionVersion is the canonical profile contract understood by
	// this release.
	CurrentDefinitionVersion = "1"
	// CurrentIdentityVersion is the identifier and duplicate-lookup policy
	// contract understood by this release.
	CurrentIdentityVersion    = "1"
	maxDefinitionBytes        = int64(8 << 20)
	maxProfileMappings        = 4096
	maxIdentifierRules        = 128
	maxIdentifierPatternBytes = 4096
	maxMetadataLookups        = 32
	maxLookupFields           = 32
	maxLookupVariants         = 16
)

var (
	profileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	systemNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	hubPathPattern     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)*$`)
	hookNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	knownCodecs        = map[string]struct{}{
		"none": {}, "text": {}, "integer": {}, "decimal": {}, "boolean": {},
		"date": {}, "link": {}, "reference": {}, "typed-relation": {},
		"file": {}, "composite": {}, "opaque": {}, "typed-identifier": {},
	}
)

// MergePolicy controls how an ordered mapping combines with values already
// decoded into the same Hub path.
type MergePolicy string

const (
	MergeFirstNonempty MergePolicy = "first_nonempty"
	MergeAppend        MergePolicy = "append"
	MergeReplace       MergePolicy = "replace"
)

// IdentifierStrength controls how identity evidence may be used by a
// reconciliation policy. Profiles describe evidence; they do not decide that
// a candidate is a duplicate.
type IdentifierStrength string

const (
	IdentifierStrong        IdentifierStrength = "strong"
	IdentifierCorroborating IdentifierStrength = "corroborating"
	IdentifierIgnored       IdentifierStrength = "ignored"
)

// IdentifierScope distinguishes globally unique identifiers from values whose
// meaning depends on an institution or one repository.
type IdentifierScope string

const (
	ScopeGlobal      IdentifierScope = "global"
	ScopeInstitution IdentifierScope = "institution"
	ScopeRepository  IdentifierScope = "repository"
)

// IdentifierIdentityLevel declares what one identifier identifies. Keeping
// this value explicit prevents a source-record accession, a version DOI, and a
// concept identifier from being treated as interchangeable duplicate evidence.
type IdentifierIdentityLevel string

const (
	IdentityWork          IdentifierIdentityLevel = "work"
	IdentityVersion       IdentifierIdentityLevel = "version"
	IdentityManifestation IdentifierIdentityLevel = "manifestation"
	IdentityConcept       IdentifierIdentityLevel = "concept"
	IdentitySourceRecord  IdentifierIdentityLevel = "source_record"
)

// LookupOperator is a portable query intent. A system adapter translates it
// to the native API syntax.
type LookupOperator string

const (
	LookupExact    LookupOperator = "exact"
	LookupContains LookupOperator = "contains"
)

// MetadataOnlyPolicy controls whether a metadata-only candidate lookup is
// disabled or may produce candidates requiring manual review.
type MetadataOnlyPolicy string

const (
	MetadataOnlyDisabled MetadataOnlyPolicy = "disabled"
	MetadataOnlyReview   MetadataOnlyPolicy = "review"
)

// Definition is one versioned, ordered mapping from a configured system model
// to and from the Crosswalk Hub. Slice order is semantic and is retained in the
// fingerprint and compiled plan.
type Definition struct {
	Version          string            `json:"version" yaml:"version"`
	Name             string            `json:"name" yaml:"name"`
	Description      string            `json:"description,omitempty" yaml:"description,omitempty"`
	System           string            `json:"system" yaml:"system"`
	ModelFingerprint string            `json:"model_fingerprint" yaml:"model_fingerprint"`
	Mappings         []Mapping         `json:"mappings" yaml:"mappings"`
	Identity         *IdentityPolicy   `json:"identity,omitempty" yaml:"identity,omitempty"`
	Fingerprint      model.Fingerprint `json:"fingerprint" yaml:"fingerprint"`
}

// Mapping describes one ordered system-field/Hub-field conversion. Decode and
// Encode are registered codec names; use "none" when a direction is disabled.
type Mapping struct {
	Field  FieldSelector `json:"field" yaml:"field"`
	Hub    string        `json:"hub" yaml:"hub"`
	Decode string        `json:"decode" yaml:"decode"`
	Encode string        `json:"encode" yaml:"encode"`
	Merge  MergePolicy   `json:"merge" yaml:"merge"`
}

// EntitySelector identifies one entity kind in a model snapshot. Bundle is
// empty for systems without bundle or resource-template semantics.
type EntitySelector struct {
	EntityType string `json:"entity_type" yaml:"entity_type"`
	Bundle     string `json:"bundle" yaml:"bundle"`
}

// FieldSelector identifies a model field and, for structured values, an
// optional attribute and equality predicate. The predicate makes shapes such
// as Drupal's attr0=doi/value identifier field explicit and reproducible.
type FieldSelector struct {
	EntityType string          `json:"entity_type" yaml:"entity_type"`
	Bundle     string          `json:"bundle" yaml:"bundle"`
	Path       string          `json:"path" yaml:"path"`
	Attribute  string          `json:"attribute,omitempty" yaml:"attribute,omitempty"`
	Where      *FieldPredicate `json:"where,omitempty" yaml:"where,omitempty"`
}

// FieldPredicate restricts a structured field value by one attribute.
type FieldPredicate struct {
	Attribute string `json:"attribute" yaml:"attribute"`
	Equals    string `json:"equals" yaml:"equals"`
}

// IdentityPolicy declares how records in one modeled system expose identity
// and which bounded lookups a Finder may perform.
type IdentityPolicy struct {
	Version     string            `json:"version" yaml:"version"`
	Repository  EntitySelector    `json:"repository" yaml:"repository"`
	Identifiers []IdentifierRule  `json:"identifiers,omitempty" yaml:"identifiers,omitempty"`
	Metadata    *MetadataIdentity `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// IdentifierRule describes one institution-aware work identifier. Pattern is
// a fully anchored Go regular expression. Canonicalizer and lookup variants
// are named hooks resolved by parser and Finder adapters.
type IdentifierRule struct {
	Name          string                  `json:"name" yaml:"name"`
	Scheme        string                  `json:"scheme" yaml:"scheme"`
	IdentityLevel IdentifierIdentityLevel `json:"identity_level" yaml:"identity_level"`
	Value         FieldSelector           `json:"value" yaml:"value"`
	Pattern       string                  `json:"pattern" yaml:"pattern"`
	Canonicalizer string                  `json:"canonicalizer" yaml:"canonicalizer"`
	Strength      IdentifierStrength      `json:"strength" yaml:"strength"`
	Scope         IdentifierScope         `json:"scope" yaml:"scope"`
	Namespace     string                  `json:"namespace_uri,omitempty" yaml:"namespace_uri,omitempty"`
	Lookup        LookupRule              `json:"lookup,omitempty" yaml:"lookup,omitempty"`
}

// LookupRule describes fields and named value variants for read-only
// candidate retrieval. Empty Fields means the identifier is parseable but not
// queryable in this system.
type LookupRule struct {
	Fields   []FieldSelector `json:"fields,omitempty" yaml:"fields,omitempty"`
	Operator LookupOperator  `json:"operator,omitempty" yaml:"operator,omitempty"`
	Variants []string        `json:"variants,omitempty" yaml:"variants,omitempty"`
}

// MetadataIdentity supplies conservative fallback evidence when no strong
// identifier resolves. Lookups controls retrieval; all selected fields are
// available to the comparison policy.
type MetadataIdentity struct {
	Title        *FieldSelector     `json:"title,omitempty" yaml:"title,omitempty"`
	Contributors []FieldSelector    `json:"contributors,omitempty" yaml:"contributors,omitempty"`
	Date         *FieldSelector     `json:"date,omitempty" yaml:"date,omitempty"`
	Lookups      []MetadataLookup   `json:"lookups,omitempty" yaml:"lookups,omitempty"`
	MetadataOnly MetadataOnlyPolicy `json:"metadata_only" yaml:"metadata_only"`
}

// MetadataLookup is one ordered candidate search. Conditions in a search are
// combined with AND; fields and variants inside one condition are ordered
// alternatives. Requiring a title prevents dangerously broad author/date-only
// repository queries.
type MetadataLookup struct {
	Name         string      `json:"name" yaml:"name"`
	Title        LookupRule  `json:"title" yaml:"title"`
	Contributors *LookupRule `json:"contributors,omitempty" yaml:"contributors,omitempty"`
	Date         *LookupRule `json:"date,omitempty" yaml:"date,omitempty"`
}

// DecodeDefinition strictly decodes and validates one JSON or YAML profile
// definition. Unknown fields and additional documents are rejected.
func DecodeDefinition(r io.Reader) (*Definition, error) {
	definition, err := decodeDefinitionDocument(r)
	if err != nil {
		return nil, err
	}
	if err := definition.Validate(); err != nil {
		return nil, err
	}
	return definition, nil
}

// DecodeDraftDefinition strictly decodes an editable profile authoring
// document. Unlike DecodeDefinition, it validates executable content without
// trusting the recorded fingerprint, which is expected to become stale while
// an operator edits mappings. PrepareDefinition seals and compiles the result.
func DecodeDraftDefinition(r io.Reader) (*Definition, error) {
	definition, err := decodeDefinitionDocument(r)
	if err != nil {
		return nil, err
	}
	definition.Fingerprint = model.Fingerprint{}
	if err := definition.validateContent(); err != nil {
		return nil, err
	}
	return definition, nil
}

func decodeDefinitionDocument(r io.Reader) (*Definition, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDefinitionBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading profile definition: %w", err)
	}
	if int64(len(data)) > maxDefinitionBytes {
		return nil, fmt.Errorf("profile definition exceeds %d bytes", maxDefinitionBytes)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("profile definition is empty")
	}

	var definition Definition
	if trimmed[0] == '{' {
		if err := strictjson.RejectDuplicateNames(trimmed); err != nil {
			return nil, fmt.Errorf("decoding profile definition JSON: %w", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&definition); err != nil {
			return nil, fmt.Errorf("decoding profile definition JSON: %w", err)
		}
		if err := definitionEOF(decoder.Decode(new(any))); err != nil {
			return nil, fmt.Errorf("decoding profile definition JSON: %w", err)
		}
	} else {
		decoder := yaml.NewDecoder(bytes.NewReader(trimmed))
		decoder.KnownFields(true)
		if err := decoder.Decode(&definition); err != nil {
			return nil, fmt.Errorf("decoding profile definition YAML: %w", err)
		}
		if err := definitionEOF(decoder.Decode(new(any))); err != nil {
			return nil, fmt.Errorf("decoding profile definition YAML: %w", err)
		}
	}
	return &definition, nil
}

// PrepareDefinition returns a sealed defensive copy compiled against the exact
// model it names. It is the authoring boundary between an editable draft and a
// publishable immutable profile.
func PrepareDefinition(snapshot *model.Snapshot, draft *Definition) (*Definition, *Compiled, error) {
	if draft == nil {
		return nil, nil, fmt.Errorf("profile draft is nil")
	}
	prepared := cloneDefinition(*draft)
	prepared.Fingerprint = model.Fingerprint{}
	if err := prepared.SealFingerprint(); err != nil {
		return nil, nil, fmt.Errorf("sealing profile definition: %w", err)
	}
	compiled, err := Compile(snapshot, &prepared)
	if err != nil {
		return nil, nil, fmt.Errorf("compiling profile definition: %w", err)
	}
	return &prepared, compiled, nil
}

// Validate checks the profile contract and its recorded fingerprint. Model
// field resolution is performed by Compile.
func (d *Definition) Validate() error {
	if err := d.validateContent(); err != nil {
		return err
	}
	if d.Fingerprint.Algorithm != "sha256" {
		return fmt.Errorf("profile fingerprint algorithm must be sha256")
	}
	if err := validateSHA256("profile fingerprint", d.Fingerprint.Value); err != nil {
		return err
	}
	computed, err := d.ComputeFingerprint()
	if err != nil {
		return err
	}
	if computed != d.Fingerprint.Value {
		return fmt.Errorf("profile fingerprint mismatch: got %s, computed %s", d.Fingerprint.Value, computed)
	}
	return nil
}

// ComputeFingerprint returns a deterministic digest of executable profile
// policy. Description and Fingerprint are deliberately excluded. Ordered
// mappings, identity rules, lookup fields, and variants retain their order.
func (d *Definition) ComputeFingerprint() (string, error) {
	if err := d.validateContent(); err != nil {
		return "", err
	}
	canonical := canonicalDefinition{
		Version: d.Version, System: d.System,
		ModelFingerprint: d.ModelFingerprint,
		Mappings:         cloneMappings(d.Mappings),
		Identity:         cloneIdentity(d.Identity),
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encoding profile fingerprint: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// SealFingerprint computes and records the current profile digest.
func (d *Definition) SealFingerprint() error {
	value, err := d.ComputeFingerprint()
	if err != nil {
		return err
	}
	d.Fingerprint = model.Fingerprint{Algorithm: "sha256", Value: value}
	return nil
}

type canonicalDefinition struct {
	Version          string          `json:"version"`
	System           string          `json:"system"`
	ModelFingerprint string          `json:"model_fingerprint"`
	Mappings         []Mapping       `json:"mappings"`
	Identity         *IdentityPolicy `json:"identity,omitempty"`
}

func (d *Definition) validateContent() error {
	if d == nil {
		return fmt.Errorf("profile definition is nil")
	}
	if d.Version != CurrentDefinitionVersion {
		return fmt.Errorf("unsupported profile definition version %q", d.Version)
	}
	if err := validateProfileName(d.Name); err != nil {
		return err
	}
	if !systemNamePattern.MatchString(d.System) {
		return fmt.Errorf("profile system %q is invalid", d.System)
	}
	if err := validateSHA256("model_fingerprint", d.ModelFingerprint); err != nil {
		return err
	}
	if len(d.Mappings) == 0 {
		return fmt.Errorf("profile mappings are required")
	}
	if len(d.Mappings) > maxProfileMappings {
		return fmt.Errorf("profile mappings exceed %d entries", maxProfileMappings)
	}
	seenMappings := make(map[string]struct{}, len(d.Mappings))
	for index, mapping := range d.Mappings {
		if err := validateFieldSelector(mapping.Field); err != nil {
			return fmt.Errorf("profile mapping %d field: %w", index+1, err)
		}
		if !hubPathPattern.MatchString(mapping.Hub) {
			return fmt.Errorf("profile mapping %d has invalid Hub path %q", index+1, mapping.Hub)
		}
		if !validCodec(mapping.Decode) {
			return fmt.Errorf("profile mapping %d has invalid decode codec %q", index+1, mapping.Decode)
		}
		if !validCodec(mapping.Encode) {
			return fmt.Errorf("profile mapping %d has invalid encode codec %q", index+1, mapping.Encode)
		}
		if !validMergePolicy(mapping.Merge) {
			return fmt.Errorf("profile mapping %d has invalid merge policy %q", index+1, mapping.Merge)
		}
		key := selectorKey(mapping.Field) + "\x00" + mapping.Hub
		if _, exists := seenMappings[key]; exists {
			return fmt.Errorf("profile mapping %d repeats field/Hub mapping", index+1)
		}
		seenMappings[key] = struct{}{}
	}
	if d.Identity != nil {
		if err := validateIdentity(*d.Identity); err != nil {
			return err
		}
	}
	return nil
}

func validateIdentity(identity IdentityPolicy) error {
	if identity.Version != CurrentIdentityVersion {
		return fmt.Errorf("unsupported identity policy version %q", identity.Version)
	}
	if err := validateEntitySelector(identity.Repository); err != nil {
		return fmt.Errorf("identity repository: %w", err)
	}
	if len(identity.Identifiers) == 0 && identity.Metadata == nil {
		return fmt.Errorf("identity policy has no identifier or metadata rules")
	}
	if len(identity.Identifiers) > maxIdentifierRules {
		return fmt.Errorf("identity policy exceeds %d identifier rules", maxIdentifierRules)
	}
	seenNames := make(map[string]struct{}, len(identity.Identifiers))
	for index, rule := range identity.Identifiers {
		if !validHookName(rule.Name) {
			return fmt.Errorf("identity identifier %d has invalid name %q", index+1, rule.Name)
		}
		if _, exists := seenNames[rule.Name]; exists {
			return fmt.Errorf("identity identifier %d repeats name %q", index+1, rule.Name)
		}
		seenNames[rule.Name] = struct{}{}
		if !validHookName(rule.Scheme) {
			return fmt.Errorf("identity identifier %q has invalid scheme %q", rule.Name, rule.Scheme)
		}
		if !validIdentifierIdentityLevel(rule.IdentityLevel) {
			return fmt.Errorf("identity identifier %q has invalid identity_level %q", rule.Name, rule.IdentityLevel)
		}
		if err := validateFieldSelector(rule.Value); err != nil {
			return fmt.Errorf("identity identifier %q value: %w", rule.Name, err)
		}
		if rule.Pattern == "" || !strings.HasPrefix(rule.Pattern, "^") || !strings.HasSuffix(rule.Pattern, "$") {
			return fmt.Errorf("identity identifier %q pattern must be fully anchored", rule.Name)
		}
		if len(rule.Pattern) > maxIdentifierPatternBytes {
			return fmt.Errorf("identity identifier %q pattern exceeds %d bytes", rule.Name, maxIdentifierPatternBytes)
		}
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			return fmt.Errorf("identity identifier %q has invalid pattern: %w", rule.Name, err)
		}
		if !validHookName(rule.Canonicalizer) {
			return fmt.Errorf("identity identifier %q has invalid canonicalizer %q", rule.Name, rule.Canonicalizer)
		}
		if !validIdentifierStrength(rule.Strength) {
			return fmt.Errorf("identity identifier %q has invalid strength %q", rule.Name, rule.Strength)
		}
		if !validIdentifierScope(rule.Scope) {
			return fmt.Errorf("identity identifier %q has invalid scope %q", rule.Name, rule.Scope)
		}
		namespace := strings.TrimSpace(rule.Namespace)
		if rule.Scope != ScopeGlobal && namespace == "" {
			return fmt.Errorf("identity identifier %q requires namespace_uri for %s scope", rule.Name, rule.Scope)
		}
		if namespace != rule.Namespace || strings.ContainsAny(namespace, "\x00\r\n") {
			return fmt.Errorf("identity identifier %q has invalid namespace_uri", rule.Name)
		}
		if namespace != "" {
			parsed, err := url.Parse(namespace)
			if err != nil || parsed.Scheme == "" || (parsed.Host == "" && parsed.Opaque == "") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
				return fmt.Errorf("identity identifier %q namespace_uri must be an absolute URI without credentials, query, or fragment", rule.Name)
			}
		}
		if rule.Strength == IdentifierIgnored && len(rule.Lookup.Fields) != 0 {
			return fmt.Errorf("identity identifier %q is ignored but configures lookup fields", rule.Name)
		}
		if err := validateLookupRule(rule.Lookup); err != nil {
			return fmt.Errorf("identity identifier %q lookup: %w", rule.Name, err)
		}
	}
	if identity.Metadata != nil {
		metadata := *identity.Metadata
		if metadata.Title == nil {
			return fmt.Errorf("identity metadata title is required")
		}
		if err := validateFieldSelector(*metadata.Title); err != nil {
			return fmt.Errorf("identity metadata title: %w", err)
		}
		for index, selector := range metadata.Contributors {
			if err := validateFieldSelector(selector); err != nil {
				return fmt.Errorf("identity metadata contributor %d: %w", index+1, err)
			}
		}
		if metadata.Date != nil {
			if err := validateFieldSelector(*metadata.Date); err != nil {
				return fmt.Errorf("identity metadata date: %w", err)
			}
		}
		if metadata.MetadataOnly != MetadataOnlyDisabled && metadata.MetadataOnly != MetadataOnlyReview {
			return fmt.Errorf("identity metadata has invalid metadata_only policy %q", metadata.MetadataOnly)
		}
		seenLookups := make(map[string]struct{}, len(metadata.Lookups))
		if len(metadata.Lookups) > maxMetadataLookups {
			return fmt.Errorf("identity metadata exceeds %d lookups", maxMetadataLookups)
		}
		for index, lookup := range metadata.Lookups {
			if !validHookName(lookup.Name) {
				return fmt.Errorf("identity metadata lookup %d has invalid name %q", index+1, lookup.Name)
			}
			if _, exists := seenLookups[lookup.Name]; exists {
				return fmt.Errorf("identity metadata lookup %d repeats name %q", index+1, lookup.Name)
			}
			seenLookups[lookup.Name] = struct{}{}
			if len(lookup.Title.Fields) == 0 {
				return fmt.Errorf("identity metadata lookup %q requires title fields", lookup.Name)
			}
			if err := validateLookupRule(lookup.Title); err != nil {
				return fmt.Errorf("identity metadata lookup %q title: %w", lookup.Name, err)
			}
			if lookup.Contributors != nil {
				if len(lookup.Contributors.Fields) == 0 {
					return fmt.Errorf("identity metadata lookup %q contributors requires fields", lookup.Name)
				}
				if err := validateLookupRule(*lookup.Contributors); err != nil {
					return fmt.Errorf("identity metadata lookup %q contributors: %w", lookup.Name, err)
				}
			}
			if lookup.Date != nil {
				if len(lookup.Date.Fields) == 0 {
					return fmt.Errorf("identity metadata lookup %q date requires fields", lookup.Name)
				}
				if err := validateLookupRule(*lookup.Date); err != nil {
					return fmt.Errorf("identity metadata lookup %q date: %w", lookup.Name, err)
				}
			}
		}
		if metadata.MetadataOnly == MetadataOnlyReview && len(metadata.Lookups) == 0 {
			return fmt.Errorf("identity metadata review policy requires lookups")
		}
		if metadata.MetadataOnly == MetadataOnlyDisabled && len(metadata.Lookups) != 0 {
			return fmt.Errorf("identity metadata disabled policy cannot configure lookups")
		}
	}
	return nil
}

func validateLookupRule(rule LookupRule) error {
	if len(rule.Fields) == 0 {
		if rule.Operator != "" || len(rule.Variants) != 0 {
			return fmt.Errorf("operator and variants require lookup fields")
		}
		return nil
	}
	if len(rule.Fields) > maxLookupFields {
		return fmt.Errorf("lookup fields exceed %d entries", maxLookupFields)
	}
	if rule.Operator != LookupExact && rule.Operator != LookupContains {
		return fmt.Errorf("invalid operator %q", rule.Operator)
	}
	if len(rule.Variants) == 0 {
		return fmt.Errorf("lookup variants are required")
	}
	if len(rule.Variants) > maxLookupVariants {
		return fmt.Errorf("lookup variants exceed %d entries", maxLookupVariants)
	}
	seenFields := make(map[string]struct{}, len(rule.Fields))
	for index, field := range rule.Fields {
		if err := validateFieldSelector(field); err != nil {
			return fmt.Errorf("field %d: %w", index+1, err)
		}
		key := selectorKey(field)
		if _, exists := seenFields[key]; exists {
			return fmt.Errorf("field %d is repeated", index+1)
		}
		seenFields[key] = struct{}{}
	}
	seenVariants := make(map[string]struct{}, len(rule.Variants))
	for index, variant := range rule.Variants {
		if !validHookName(variant) {
			return fmt.Errorf("variant %d has invalid name %q", index+1, variant)
		}
		if _, exists := seenVariants[variant]; exists {
			return fmt.Errorf("variant %d repeats %q", index+1, variant)
		}
		seenVariants[variant] = struct{}{}
	}
	return nil
}

func validateEntitySelector(selector EntitySelector) error {
	if strings.TrimSpace(selector.EntityType) == "" {
		return fmt.Errorf("entity_type is required")
	}
	if strings.TrimSpace(selector.EntityType) != selector.EntityType || strings.TrimSpace(selector.Bundle) != selector.Bundle || strings.ContainsAny(selector.EntityType+selector.Bundle, "\x00\r\n") {
		return fmt.Errorf("entity_type or bundle is invalid")
	}
	return nil
}

func validateFieldSelector(selector FieldSelector) error {
	if err := validateEntitySelector(EntitySelector{EntityType: selector.EntityType, Bundle: selector.Bundle}); err != nil {
		return err
	}
	if strings.TrimSpace(selector.Path) == "" {
		return fmt.Errorf("path is required")
	}
	if strings.TrimSpace(selector.Path) != selector.Path || strings.ContainsAny(selector.Path, "\x00\r\n") {
		return fmt.Errorf("path is invalid")
	}
	if selector.Attribute != "" && (strings.TrimSpace(selector.Attribute) != selector.Attribute || strings.ContainsAny(selector.Attribute, "\x00\r\n")) {
		return fmt.Errorf("invalid attribute %q", selector.Attribute)
	}
	if selector.Where != nil {
		if strings.TrimSpace(selector.Where.Attribute) == "" || strings.TrimSpace(selector.Where.Attribute) != selector.Where.Attribute || strings.ContainsAny(selector.Where.Attribute, "\x00\r\n") {
			return fmt.Errorf("invalid predicate attribute %q", selector.Where.Attribute)
		}
		if selector.Where.Equals == "" || strings.ContainsAny(selector.Where.Equals, "\x00\r\n") {
			return fmt.Errorf("predicate equals value is invalid")
		}
	}
	return nil
}

func selectorKey(selector FieldSelector) string {
	predicateAttribute, predicateValue := "", ""
	if selector.Where != nil {
		predicateAttribute = selector.Where.Attribute
		predicateValue = selector.Where.Equals
	}
	return strings.Join([]string{
		selector.EntityType, selector.Bundle, selector.Path, selector.Attribute,
		predicateAttribute, predicateValue,
	}, "\x00")
}

func validHookName(value string) bool {
	return hookNamePattern.MatchString(value)
}

func validCodec(value string) bool {
	_, exists := knownCodecs[value]
	return exists
}

func validMergePolicy(value MergePolicy) bool {
	return value == MergeFirstNonempty || value == MergeAppend || value == MergeReplace
}

func validIdentifierStrength(value IdentifierStrength) bool {
	return value == IdentifierStrong || value == IdentifierCorroborating || value == IdentifierIgnored
}

func validIdentifierScope(value IdentifierScope) bool {
	return value == ScopeGlobal || value == ScopeInstitution || value == ScopeRepository
}

func validIdentifierIdentityLevel(value IdentifierIdentityLevel) bool {
	switch value {
	case IdentityWork, IdentityVersion, IdentityManifestation, IdentityConcept, IdentitySourceRecord:
		return true
	default:
		return false
	}
}

func validateSHA256(label, value string) error {
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("%s must contain a SHA-256 digest", label)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%s must contain a SHA-256 digest", label)
	}
	if value != strings.ToLower(value) {
		return fmt.Errorf("%s must use lowercase hexadecimal", label)
	}
	return nil
}

func validateProfileName(name string) error {
	if !profileNamePattern.MatchString(name) || name == "." || name == ".." {
		return fmt.Errorf("profile name %q is invalid; use 1-128 lowercase letters, digits, dots, underscores, or hyphens", name)
	}
	return nil
}

func cloneMappings(input []Mapping) []Mapping {
	result := make([]Mapping, len(input))
	for index, mapping := range input {
		result[index] = mapping
		result[index].Field = cloneSelector(mapping.Field)
	}
	return result
}

func cloneIdentity(input *IdentityPolicy) *IdentityPolicy {
	if input == nil {
		return nil
	}
	result := *input
	result.Identifiers = make([]IdentifierRule, len(input.Identifiers))
	for index, rule := range input.Identifiers {
		result.Identifiers[index] = rule
		result.Identifiers[index].Value = cloneSelector(rule.Value)
		result.Identifiers[index].Lookup = cloneLookup(rule.Lookup)
	}
	if input.Metadata != nil {
		metadata := *input.Metadata
		if input.Metadata.Title != nil {
			title := cloneSelector(*input.Metadata.Title)
			metadata.Title = &title
		}
		metadata.Contributors = make([]FieldSelector, len(input.Metadata.Contributors))
		for index, selector := range input.Metadata.Contributors {
			metadata.Contributors[index] = cloneSelector(selector)
		}
		if input.Metadata.Date != nil {
			date := cloneSelector(*input.Metadata.Date)
			metadata.Date = &date
		}
		metadata.Lookups = make([]MetadataLookup, len(input.Metadata.Lookups))
		for index, lookup := range input.Metadata.Lookups {
			metadata.Lookups[index] = lookup
			metadata.Lookups[index].Title = cloneLookup(lookup.Title)
			if lookup.Contributors != nil {
				contributors := cloneLookup(*lookup.Contributors)
				metadata.Lookups[index].Contributors = &contributors
			}
			if lookup.Date != nil {
				date := cloneLookup(*lookup.Date)
				metadata.Lookups[index].Date = &date
			}
		}
		result.Metadata = &metadata
	}
	return &result
}

func cloneLookup(input LookupRule) LookupRule {
	result := input
	result.Fields = make([]FieldSelector, len(input.Fields))
	for index, selector := range input.Fields {
		result.Fields[index] = cloneSelector(selector)
	}
	result.Variants = append([]string(nil), input.Variants...)
	return result
}

func cloneSelector(input FieldSelector) FieldSelector {
	result := input
	if input.Where != nil {
		predicate := *input.Where
		result.Where = &predicate
	}
	return result
}

func cloneDefinition(input Definition) Definition {
	result := input
	result.Mappings = cloneMappings(input.Mappings)
	result.Identity = cloneIdentity(input.Identity)
	return result
}

func definitionEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("multiple documents are not allowed")
	}
	return err
}
