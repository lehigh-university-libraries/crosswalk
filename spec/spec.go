// Package spec defines versioned, direction-aware metadata transformations.
package spec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/internal/strictjson"
	"gopkg.in/yaml.v3"
)

// CurrentVersion is the transformation specification version understood by
// this release.
const (
	CurrentVersion         = "1"
	maxTransformationBytes = int64(8 << 20)
)

// Operation identifies an Islandora Workbench task.
type Operation string

const (
	// OperationCreate creates nodes and their primary media.
	OperationCreate Operation = "create"
	// OperationUpdate updates existing nodes.
	OperationUpdate Operation = "update"
	// OperationAddMedia adds media to existing nodes.
	OperationAddMedia Operation = "add_media"
	// OperationAgents creates or updates linked-agent terms.
	OperationAgents Operation = "agents"
	// OperationUnpublishedSupplemental stages supplemental media that must be
	// reconciled with a node ID after its parent node is created.
	OperationUnpublishedSupplemental Operation = "unpublished_supplemental"
	// OperationPendingSupplemental stages published supplemental media that
	// must be reconciled with a node ID after its parent node is created.
	OperationPendingSupplemental Operation = "pending_supplemental"
)

// Transformation maps an ordered source table through Hub fields to an
// ordered target table. Source and target are deliberately distinct: a source
// spreadsheet label is not a target Drupal machine name.
type Transformation struct {
	Version     string            `json:"version" yaml:"version"`
	Name        string            `json:"name" yaml:"name"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Source      Table             `json:"source" yaml:"source"`
	Target      Table             `json:"target" yaml:"target"`
	Defaults    map[string]string `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	Fingerprint Fingerprint       `json:"fingerprint,omitempty" yaml:"fingerprint,omitempty"`
}

// Default returns a named transformation-level workflow default.
func (t *Transformation) Default(name string) string {
	if t == nil || t.Defaults == nil {
		return ""
	}
	return t.Defaults[name]
}

// Table describes one side of a tabular transformation.
type Table struct {
	Format              string          `json:"format" yaml:"format"`
	HeaderRows          int             `json:"header_rows,omitempty" yaml:"header_rows,omitempty"`
	MachineHeaderRow    int             `json:"machine_header_row,omitempty" yaml:"machine_header_row,omitempty"`
	HumanHeaderRow      int             `json:"human_header_row,omitempty" yaml:"human_header_row,omitempty"`
	MultiValueSeparator string          `json:"multi_value_separator,omitempty" yaml:"multi_value_separator,omitempty"`
	Fields              []Field         `json:"fields" yaml:"fields"`
	RequiredGroups      []RequiredGroup `json:"required_groups,omitempty" yaml:"required_groups,omitempty"`
	Validations         []Validation    `json:"validations,omitempty" yaml:"validations,omitempty"`
}

// RequiredGroup requires at least one of its canonical field names to have a
// value for each listed operation. It represents aggregate Drupal fields that
// are required at the schema level but expand into several source columns.
type RequiredGroup struct {
	Name        string      `json:"name" yaml:"name"`
	Fields      []string    `json:"fields" yaml:"fields"`
	RequiredFor []Operation `json:"required_for" yaml:"required_for"`
}

// IsRequired reports whether this group requires a value for operation.
func (g RequiredGroup) IsRequired(operation Operation) bool {
	for _, candidate := range g.RequiredFor {
		if candidate == operation {
			return true
		}
	}
	return false
}

// ValidationRule names a reusable validation implementation. Rules are bound
// to canonical mapping fields, never to spreadsheet display labels.
type ValidationRule string

// ValidationPhase separates deterministic metadata checks from checks that
// require a selected deployment, filesystem, or external authority.
type ValidationPhase string

const (
	ValidationPhaseDeterministic ValidationPhase = "deterministic"
	ValidationPhaseContext       ValidationPhase = "context"
)

const (
	ValidationUnique                   ValidationRule = "unique"
	ValidationReference                ValidationRule = "reference"
	ValidationPrecedingReference       ValidationRule = "preceding_reference"
	ValidationDifferent                ValidationRule = "different"
	ValidationRequiredAny              ValidationRule = "required_any"
	ValidationRequiredWhen             ValidationRule = "required_when"
	ValidationMaximumRunes             ValidationRule = "max_runes"
	ValidationAbsoluteHTTPURL          ValidationRule = "absolute_http_url"
	ValidationWorkbenchLink            ValidationRule = "workbench_link"
	ValidationGeolocation              ValidationRule = "geolocation"
	ValidationAuthorityLink            ValidationRule = "authority_link"
	ValidationMediaTrack               ValidationRule = "media_track"
	ValidationEntityReference          ValidationRule = "entity_reference"
	ValidationTypedRelation            ValidationRule = "typed_relation"
	ValidationDOI                      ValidationRule = "doi"
	ValidationRightsStatement          ValidationRule = "rights_statement"
	ValidationNoLineBreaks             ValidationRule = "no_line_breaks"
	ValidationEnum                     ValidationRule = "enum"
	ValidationFiniteNumber             ValidationRule = "finite_number"
	ValidationNumericRange             ValidationRule = "numeric_range"
	ValidationPattern                  ValidationRule = "pattern"
	ValidationNotFutureTimestamp       ValidationRule = "not_future_timestamp"
	ValidationMediaExtension           ValidationRule = "media_extension"
	ValidationContributor              ValidationRule = "contributor"
	ValidationGettyTGN                 ValidationRule = "getty_tgn"
	ValidationContextNodeExists        ValidationRule = "context_node_exists"
	ValidationContextEntityExists      ValidationRule = "context_entity_exists"
	ValidationContextAllowedValue      ValidationRule = "context_allowed_value"
	ValidationContextFileReadable      ValidationRule = "context_file_readable"
	ValidationContextTGNResolves       ValidationRule = "context_tgn_resolves"
	ValidationContextURLAliasAvailable ValidationRule = "context_url_alias_available"
)

// ValidationOperator is a typed predicate operator used by mapping-declared
// conditional rules.
type ValidationOperator string

const (
	ValidationOperatorIn                    ValidationOperator = "in"
	ValidationOperatorValueCountGreaterThan ValidationOperator = "value_count_greater_than"
)

// ValidationWhen limits a rule using another canonical field in the same row.
type ValidationWhen struct {
	Field    string             `json:"field" yaml:"field"`
	Operator ValidationOperator `json:"operator" yaml:"operator"`
	Values   []string           `json:"values,omitempty" yaml:"values,omitempty"`
	Count    int                `json:"count,omitempty" yaml:"count,omitempty"`
}

// MediaExtensionPolicy describes how Workbench selects a Drupal media bundle
// from a filename extension and which extensions that bundle's configured file
// field accepts. SelectExtensions is the sealed equivalent of Workbench's
// media_types configuration; AllowedExtensions comes from the selected
// bundle's file field in the immutable Drupal model. Exactly one policy is the
// fallback used when no selector matches.
type MediaExtensionPolicy struct {
	MediaType         string   `json:"media_type" yaml:"media_type"`
	SelectExtensions  []string `json:"select_extensions,omitempty" yaml:"select_extensions,omitempty"`
	AllowedExtensions []string `json:"allowed_extensions,omitempty" yaml:"allowed_extensions,omitempty"`
	Fallback          bool     `json:"fallback,omitempty" yaml:"fallback,omitempty"`
}

// Validation declares one field-level criterion. Field references are exact
// canonical names in the same table. Context rules document checks that a
// deployment-aware consumer such as sitectl must perform after Crosswalk's
// deterministic validation and transformation.
type Validation struct {
	Rule            ValidationRule         `json:"rule" yaml:"rule"`
	Phase           ValidationPhase        `json:"phase,omitempty" yaml:"phase,omitempty"`
	Field           string                 `json:"field,omitempty" yaml:"field,omitempty"`
	Fields          []string               `json:"fields,omitempty" yaml:"fields,omitempty"`
	Limit           int                    `json:"limit,omitempty" yaml:"limit,omitempty"`
	Minimum         *float64               `json:"minimum,omitempty" yaml:"minimum,omitempty"`
	Maximum         *float64               `json:"maximum,omitempty" yaml:"maximum,omitempty"`
	Pattern         string                 `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Values          []string               `json:"values,omitempty" yaml:"values,omitempty"`
	Bundles         []string               `json:"bundles,omitempty" yaml:"bundles,omitempty"`
	EntityType      string                 `json:"entity_type,omitempty" yaml:"entity_type,omitempty"`
	Provider        string                 `json:"provider,omitempty" yaml:"provider,omitempty"`
	Handler         string                 `json:"handler,omitempty" yaml:"handler,omitempty"`
	MediaTypes      []MediaExtensionPolicy `json:"media_types,omitempty" yaml:"media_types,omitempty"`
	AllowNewNames   bool                   `json:"allow_new_names,omitempty" yaml:"allow_new_names,omitempty"`
	CaseInsensitive bool                   `json:"case_insensitive,omitempty" yaml:"case_insensitive,omitempty"`
	Operations      []Operation            `json:"operations,omitempty" yaml:"operations,omitempty"`
	When            *ValidationWhen        `json:"when,omitempty" yaml:"when,omitempty"`
}

// IsContext reports whether a deployment-aware consumer must execute this
// criterion. An omitted phase is deterministic.
func (v Validation) IsContext() bool {
	return v.Phase == ValidationPhaseContext
}

// AppliesTo reports whether a validation participates in operation. An empty
// operation list applies to every operation.
func (v Validation) AppliesTo(operation Operation) bool {
	if len(v.Operations) == 0 || operation == "" {
		return true
	}
	for _, candidate := range v.Operations {
		if candidate == operation {
			return true
		}
	}
	return false
}

// Field describes an ordered table column and its Hub representation.
type Field struct {
	Name        string   `json:"name" yaml:"name"`
	Label       string   `json:"label,omitempty" yaml:"label,omitempty"`
	SchemaLabel string   `json:"schema_label,omitempty" yaml:"schema_label,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Aliases     []string `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	Hub         string   `json:"hub,omitempty" yaml:"hub,omitempty"`
	Codec       string   `json:"codec,omitempty" yaml:"codec,omitempty"`
	// ProfileRule names the exact identifier rule in a bound system profile.
	// It is required for profile-defined identifier columns so Crosswalk never
	// guesses an institution's scheme or authority from a header.
	ProfileRule      string         `json:"profile_rule,omitempty" yaml:"profile_rule,omitempty"`
	SourceType       string         `json:"source_type,omitempty" yaml:"source_type,omitempty"`
	Settings         map[string]any `json:"settings,omitempty" yaml:"settings,omitempty"`
	InstanceSettings map[string]any `json:"instance_settings,omitempty" yaml:"instance_settings,omitempty"`
	// Cardinality is the maximum number of values; zero means unbounded.
	Cardinality int         `json:"cardinality,omitempty" yaml:"cardinality,omitempty"`
	Required    bool        `json:"required,omitempty" yaml:"required,omitempty"`
	RequiredFor []Operation `json:"required_for,omitempty" yaml:"required_for,omitempty"`
	// OptionalForObjectModels suppresses required-field policy when the record
	// uses one of the listed Islandora object model labels.
	OptionalForObjectModels []string     `json:"optional_for_object_models,omitempty" yaml:"optional_for_object_models,omitempty"`
	Default                 string       `json:"default,omitempty" yaml:"default,omitempty"`
	Operations              []Operation  `json:"operations,omitempty" yaml:"operations,omitempty"`
	Validations             []Validation `json:"validations,omitempty" yaml:"validations,omitempty"`
}

// IsRequired reports whether this field is required for operation.
func (f Field) IsRequired(operation Operation) bool {
	if f.Required {
		return true
	}
	for _, candidate := range f.RequiredFor {
		if candidate == operation {
			return true
		}
	}
	return false
}

// IsRequiredFor reports whether this field requires a value for the operation
// and object model combination.
func (f Field) IsRequiredFor(operation Operation, objectModel string) bool {
	return f.IsRequired(operation) && !f.IsOptionalForObjectModel(objectModel)
}

// IsOptionalForObjectModel reports whether the field's conditional exception
// applies to an Islandora object model label.
func (f Field) IsOptionalForObjectModel(objectModel string) bool {
	objectModel = normalizedObjectModel(objectModel)
	if objectModel == "" {
		return false
	}
	for _, candidate := range f.OptionalForObjectModels {
		if objectModel == normalizedObjectModel(candidate) {
			return true
		}
	}
	return false
}

// AppliesTo reports whether a field participates in operation. A field with
// no operation list participates in every operation.
func (f Field) AppliesTo(operation Operation) bool {
	if operation == "" || len(f.Operations) == 0 {
		return true
	}
	for _, candidate := range f.Operations {
		if candidate == operation {
			return true
		}
	}
	return false
}

// Matches reports whether header names this field. Matching is
// case-insensitive and accepts the machine name, human label, and aliases.
func (f Field) Matches(header string) bool {
	header = normalizedHeader(header)
	if header == normalizedHeader(f.Name) || header == normalizedHeader(f.Label) {
		return header != ""
	}
	for _, alias := range f.Aliases {
		if header == normalizedHeader(alias) {
			return true
		}
	}
	return false
}

// Fingerprint records where a specification came from and the digest of its
// schema-relevant content. Site fields are metadata and do not imply network
// access by Crosswalk.
type Fingerprint struct {
	Algorithm string `json:"algorithm,omitempty" yaml:"algorithm,omitempty"`
	Value     string `json:"value,omitempty" yaml:"value,omitempty"`
	// Model identifies the immutable system model from which this
	// transformation was compiled. It is executable provenance and therefore
	// participates in Value; display-only site fields below do not. Profile
	// identifies the exact executable Field-to-Hub mapping layered on that
	// model when a transformation is profile-bound.
	Model      string `json:"model,omitempty" yaml:"model,omitempty"`
	Profile    string `json:"profile,omitempty" yaml:"profile,omitempty"`
	SiteUUID   string `json:"site_uuid,omitempty" yaml:"site_uuid,omitempty"`
	SiteName   string `json:"site_name,omitempty" yaml:"site_name,omitempty"`
	Bundle     string `json:"bundle,omitempty" yaml:"bundle,omitempty"`
	ConfigHash string `json:"config_hash,omitempty" yaml:"config_hash,omitempty"`
}

// Load decodes and validates a sealed JSON or YAML transformation
// specification. Unknown fields are rejected so misspelled policy cannot
// silently disappear.
func Load(r io.Reader) (*Transformation, error) {
	transformation, err := decode(r)
	if err != nil {
		return nil, err
	}
	if err := transformation.Validate(); err != nil {
		return nil, err
	}
	if err := transformation.ValidateSealed(); err != nil {
		return nil, err
	}
	return transformation, nil
}

// DecodeDraft decodes an editable JSON or YAML transformation draft. A stale
// specification digest is deliberately discarded so an author can review and
// change executable mappings or policy before validating and resealing it.
// Immutable model provenance remains in place and is still validated.
func DecodeDraft(r io.Reader) (*Transformation, error) {
	transformation, err := decode(r)
	if err != nil {
		return nil, err
	}
	transformation.Fingerprint.Algorithm = ""
	transformation.Fingerprint.Value = ""
	if err := transformation.Validate(); err != nil {
		return nil, err
	}
	return transformation, nil
}

func decode(r io.Reader) (*Transformation, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxTransformationBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading transformation specification: %w", err)
	}
	if int64(len(data)) > maxTransformationBytes {
		return nil, fmt.Errorf("transformation specification exceeds %d bytes", maxTransformationBytes)
	}

	transformation := new(Transformation)
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("transformation specification is empty")
	}
	if trimmed[0] == '{' {
		if err := strictjson.RejectDuplicateNames(trimmed); err != nil {
			return nil, fmt.Errorf("decoding transformation JSON: %w", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(transformation); err != nil {
			return nil, fmt.Errorf("decoding transformation JSON: %w", err)
		}
		if err := requireEOF(decoder.Decode(new(any))); err != nil {
			return nil, fmt.Errorf("decoding transformation JSON: %w", err)
		}
	} else {
		decoder := yaml.NewDecoder(bytes.NewReader(trimmed))
		decoder.KnownFields(true)
		if err := decoder.Decode(transformation); err != nil {
			return nil, fmt.Errorf("decoding transformation YAML: %w", err)
		}
		if err := requireEOF(decoder.Decode(new(any))); err != nil {
			return nil, fmt.Errorf("decoding transformation YAML: %w", err)
		}
	}

	return transformation, nil
}

// ValidateSealed verifies that a runtime specification carries an exact,
// lowercase SHA-256 digest. Validate intentionally continues to accept an
// editable in-memory draft; Load is the sealed trust boundary.
func (t *Transformation) ValidateSealed() error {
	if err := t.Validate(); err != nil {
		return err
	}
	if t.Fingerprint.Algorithm != "sha256" {
		return fmt.Errorf("transformation fingerprint algorithm must be sha256")
	}
	if len(t.Fingerprint.Value) != sha256.Size*2 || t.Fingerprint.Value != strings.ToLower(t.Fingerprint.Value) {
		return fmt.Errorf("transformation fingerprint must contain lowercase SHA-256 hexadecimal")
	}
	if _, err := hex.DecodeString(t.Fingerprint.Value); err != nil {
		return fmt.Errorf("transformation fingerprint must contain lowercase SHA-256 hexadecimal")
	}
	return nil
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

// Validate checks the specification contract and any supplied fingerprint.
func (t *Transformation) Validate() error {
	if t == nil {
		return fmt.Errorf("transformation specification is nil")
	}
	if t.Version != CurrentVersion {
		return fmt.Errorf("unsupported transformation specification version %q", t.Version)
	}
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("transformation specification name is required")
	}
	if err := validateTable("source", t.Source); err != nil {
		return err
	}
	if err := validateTable("target", t.Target); err != nil {
		return err
	}
	if err := validateWorkflowDefaults(t.Defaults); err != nil {
		return err
	}
	for _, fingerprint := range []struct {
		name  string
		value string
	}{
		{name: "model", value: t.Fingerprint.Model},
		{name: "profile", value: t.Fingerprint.Profile},
	} {
		if fingerprint.value == "" {
			continue
		}
		if len(fingerprint.value) != sha256.Size*2 || fingerprint.value != strings.ToLower(fingerprint.value) {
			return fmt.Errorf("transformation %s fingerprint must contain lowercase SHA-256 hexadecimal", fingerprint.name)
		}
		if _, err := hex.DecodeString(fingerprint.value); err != nil {
			return fmt.Errorf("transformation %s fingerprint must contain lowercase SHA-256 hexadecimal", fingerprint.name)
		}
	}
	if t.Fingerprint.Profile != "" && t.Fingerprint.Model == "" {
		return fmt.Errorf("transformation profile fingerprint requires a model fingerprint")
	}
	if t.Fingerprint.Value != "" {
		if t.Fingerprint.Algorithm != "" && t.Fingerprint.Algorithm != "sha256" {
			return fmt.Errorf("unsupported fingerprint algorithm %q", t.Fingerprint.Algorithm)
		}
		computed, err := t.ComputeFingerprint()
		if err != nil {
			return err
		}
		if !strings.EqualFold(computed, t.Fingerprint.Value) {
			return fmt.Errorf("transformation fingerprint mismatch: got %s, computed %s", t.Fingerprint.Value, computed)
		}
	}
	return nil
}

func validateTable(direction string, table Table) error {
	if table.Format == "" {
		return fmt.Errorf("%s format is required", direction)
	}
	if len(table.Fields) == 0 {
		return fmt.Errorf("%s fields are required", direction)
	}
	if table.HeaderRows < 0 {
		return fmt.Errorf("%s header_rows cannot be negative", direction)
	}
	if table.HeaderRows > 2 {
		return fmt.Errorf("%s header_rows %d exceeds the version 1 maximum of 2", direction, table.HeaderRows)
	}
	if table.HeaderRows > 0 {
		if table.MachineHeaderRow < 0 || table.MachineHeaderRow >= table.HeaderRows {
			return fmt.Errorf("%s machine_header_row %d is outside %d header rows", direction, table.MachineHeaderRow, table.HeaderRows)
		}
		if table.HumanHeaderRow < 0 || table.HumanHeaderRow >= table.HeaderRows {
			return fmt.Errorf("%s human_header_row %d is outside %d header rows", direction, table.HumanHeaderRow, table.HeaderRows)
		}
		if table.HeaderRows > 1 && table.MachineHeaderRow == table.HumanHeaderRow {
			return fmt.Errorf("%s machine and human header rows must differ", direction)
		}
	}
	seen := make(map[string]string, len(table.Fields)*2)
	fields := make(map[string]Field, len(table.Fields))
	for i, field := range table.Fields {
		name := normalizedHeader(field.Name)
		if name == "" {
			return fmt.Errorf("%s field %d has no name", direction, i+1)
		}
		if previous, exists := seen[name]; exists {
			return fmt.Errorf("%s header %q is shared by fields %q and %q", direction, field.Name, previous, field.Name)
		}
		seen[name] = field.Name
		fields[name] = field
		if field.Hub == "" && field.Codec != "ignore" {
			return fmt.Errorf("%s field %q has no Hub mapping", direction, field.Name)
		}
		if direction == "target" && table.Format == "islandora-workbench" && field.Codec != "ignore" && !validWorkbenchTargetHub(field.Hub) {
			return fmt.Errorf("%s field %q has unsupported Islandora Workbench Hub mapping %q", direction, field.Name, field.Hub)
		}
		if !validCodec(field.Codec) {
			return fmt.Errorf("%s field %q has unsupported codec %q", direction, field.Name, field.Codec)
		}
		if field.ProfileRule != "" && field.Codec != "profile_identifier" {
			return fmt.Errorf("%s field %q profile_rule requires profile_identifier codec", direction, field.Name)
		}
		if field.Codec == "profile_identifier" && strings.TrimSpace(field.ProfileRule) == "" {
			return fmt.Errorf("%s field %q profile_identifier codec requires profile_rule", direction, field.Name)
		}
		if field.ProfileRule != strings.TrimSpace(field.ProfileRule) || strings.ContainsAny(field.ProfileRule, "\x00\r\n") {
			return fmt.Errorf("%s field %q has invalid profile_rule", direction, field.Name)
		}
		if field.Cardinality < 0 {
			return fmt.Errorf("%s field %q has negative cardinality", direction, field.Name)
		}
		operations := make(map[Operation]struct{}, len(field.Operations))
		for _, operation := range field.Operations {
			if !validOperation(operation) {
				return fmt.Errorf("%s field %q has unsupported operation %q", direction, field.Name, operation)
			}
			if _, exists := operations[operation]; exists {
				return fmt.Errorf("%s field %q repeats operation %q", direction, field.Name, operation)
			}
			operations[operation] = struct{}{}
		}
		for _, operation := range field.RequiredFor {
			if !validOperation(operation) {
				return fmt.Errorf("%s field %q is required for unsupported operation %q", direction, field.Name, operation)
			}
			if len(operations) > 0 {
				if _, applies := operations[operation]; !applies {
					return fmt.Errorf("%s field %q is required for operation %q but does not apply to it", direction, field.Name, operation)
				}
			}
		}
		validations := make(map[ValidationRule]struct{}, len(field.Validations))
		for _, validation := range field.Validations {
			if err := validateFieldValidation(direction, field, validation, table.Fields); err != nil {
				return err
			}
			if _, exists := validations[validation.Rule]; exists {
				return fmt.Errorf("%s field %q repeats validation rule %q", direction, field.Name, validation.Rule)
			}
			validations[validation.Rule] = struct{}{}
		}
		optionalModels := make(map[string]struct{}, len(field.OptionalForObjectModels))
		for _, objectModel := range field.OptionalForObjectModels {
			normalized := normalizedObjectModel(objectModel)
			if normalized == "" {
				return fmt.Errorf("%s field %q has an empty optional object model", direction, field.Name)
			}
			if _, exists := optionalModels[normalized]; exists {
				return fmt.Errorf("%s field %q repeats optional object model %q", direction, field.Name, objectModel)
			}
			optionalModels[normalized] = struct{}{}
		}
		for _, alternate := range append([]string{field.Label}, field.Aliases...) {
			header := normalizedHeader(alternate)
			if header == "" {
				continue
			}
			if previous, exists := seen[header]; exists && previous != field.Name {
				return fmt.Errorf("%s header %q is shared by fields %q and %q", direction, alternate, previous, field.Name)
			}
			seen[header] = field.Name
		}
	}
	groups := make(map[string]struct{}, len(table.RequiredGroups))
	for index, group := range table.RequiredGroups {
		name := normalizedHeader(group.Name)
		if name == "" {
			return fmt.Errorf("%s required group %d has no name", direction, index+1)
		}
		if _, exists := groups[name]; exists {
			return fmt.Errorf("%s required group %q is duplicated", direction, group.Name)
		}
		groups[name] = struct{}{}
		if len(group.Fields) == 0 {
			return fmt.Errorf("%s required group %q has no fields", direction, group.Name)
		}
		members := make(map[string]struct{}, len(group.Fields))
		for _, member := range group.Fields {
			key := normalizedHeader(member)
			field, exists := fields[key]
			if !exists || key == "" {
				return fmt.Errorf("%s required group %q references unknown field %q", direction, group.Name, member)
			}
			if _, exists := members[key]; exists {
				return fmt.Errorf("%s required group %q repeats field %q", direction, group.Name, member)
			}
			members[key] = struct{}{}
			if member != field.Name {
				return fmt.Errorf("%s required group %q must reference canonical field name %q instead of %q", direction, group.Name, field.Name, member)
			}
		}
		if len(group.RequiredFor) == 0 {
			return fmt.Errorf("%s required group %q has no required_for operations", direction, group.Name)
		}
		operations := make(map[Operation]struct{}, len(group.RequiredFor))
		for _, operation := range group.RequiredFor {
			if !validOperation(operation) {
				return fmt.Errorf("%s required group %q is required for unsupported operation %q", direction, group.Name, operation)
			}
			if _, exists := operations[operation]; exists {
				return fmt.Errorf("%s required group %q repeats operation %q", direction, group.Name, operation)
			}
			operations[operation] = struct{}{}
			for _, member := range group.Fields {
				field := fields[normalizedHeader(member)]
				if !field.AppliesTo(operation) {
					return fmt.Errorf("%s required group %q field %q does not apply to operation %q", direction, group.Name, member, operation)
				}
			}
		}
	}
	for index, validation := range table.Validations {
		if err := validateTableValidation(direction, index, validation, fields); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldValidation(direction string, field Field, validation Validation, fields []Field) error {
	if !validFieldValidationRule(validation.Rule) {
		return fmt.Errorf("%s field %q has unsupported validation rule %q", direction, field.Name, validation.Rule)
	}
	if err := validateValidationCommon(direction, "field "+fmt.Sprintf("%q", field.Name), validation, fields); err != nil {
		return err
	}
	if validation.Field != "" {
		return fmt.Errorf("%s field %q validation %q does not accept a field reference", direction, field.Name, validation.Rule)
	}
	if len(validation.Fields) != 0 {
		return fmt.Errorf("%s field %q validation %q does not accept field lists", direction, field.Name, validation.Rule)
	}
	if validation.Rule == ValidationMaximumRunes {
		if validation.Limit <= 0 {
			return fmt.Errorf("%s field %q validation %q requires a positive limit", direction, field.Name, validation.Rule)
		}
	} else if validation.Limit != 0 {
		return fmt.Errorf("%s field %q validation %q does not accept a limit", direction, field.Name, validation.Rule)
	}
	if validation.Rule == ValidationNumericRange {
		if validation.Minimum == nil && validation.Maximum == nil {
			return fmt.Errorf("%s field %q validation %q requires a minimum or maximum", direction, field.Name, validation.Rule)
		}
		if validation.Minimum != nil && (math.IsNaN(*validation.Minimum) || math.IsInf(*validation.Minimum, 0)) {
			return fmt.Errorf("%s field %q validation %q has a non-finite minimum", direction, field.Name, validation.Rule)
		}
		if validation.Maximum != nil && (math.IsNaN(*validation.Maximum) || math.IsInf(*validation.Maximum, 0)) {
			return fmt.Errorf("%s field %q validation %q has a non-finite maximum", direction, field.Name, validation.Rule)
		}
		if validation.Minimum != nil && validation.Maximum != nil && *validation.Minimum > *validation.Maximum {
			return fmt.Errorf("%s field %q validation %q minimum exceeds maximum", direction, field.Name, validation.Rule)
		}
	} else if validation.Minimum != nil || validation.Maximum != nil {
		return fmt.Errorf("%s field %q validation %q does not accept numeric bounds", direction, field.Name, validation.Rule)
	}
	if validation.Rule == ValidationPattern {
		if validation.Pattern == "" || len(validation.Pattern) > 4096 ||
			!strings.HasPrefix(validation.Pattern, "^") || !strings.HasSuffix(validation.Pattern, "$") {
			return fmt.Errorf("%s field %q validation %q requires a fully anchored pattern of at most 4096 bytes", direction, field.Name, validation.Rule)
		}
		if _, err := regexp.Compile("^(?:" + validation.Pattern + ")$"); err != nil {
			return fmt.Errorf("%s field %q validation %q has invalid pattern: %w", direction, field.Name, validation.Rule, err)
		}
	} else if validation.Pattern != "" {
		return fmt.Errorf("%s field %q validation %q does not accept a pattern", direction, field.Name, validation.Rule)
	}
	valuesRule := validation.Rule == ValidationEnum || validation.Rule == ValidationAuthorityLink ||
		validation.Rule == ValidationTypedRelation || validation.Rule == ValidationContributor
	if valuesRule {
		if validation.Rule != ValidationTypedRelation && validation.Rule != ValidationContributor && len(validation.Values) == 0 {
			return fmt.Errorf("%s field %q validation %q requires values", direction, field.Name, validation.Rule)
		}
		seen := make(map[string]struct{}, len(validation.Values))
		for _, value := range validation.Values {
			key := value
			if validation.CaseInsensitive {
				key = strings.ToLower(value)
			}
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s field %q validation %q has an empty value", direction, field.Name, validation.Rule)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("%s field %q validation %q repeats value %q", direction, field.Name, validation.Rule, value)
			}
			seen[key] = struct{}{}
		}
		if validation.Rule != ValidationEnum && validation.CaseInsensitive {
			return fmt.Errorf("%s field %q validation %q requires exact values", direction, field.Name, validation.Rule)
		}
	} else if len(validation.Values) != 0 || validation.CaseInsensitive {
		return fmt.Errorf("%s field %q validation %q does not accept enum values", direction, field.Name, validation.Rule)
	}
	if validation.Rule == ValidationEntityReference || validation.Rule == ValidationTypedRelation || validation.Rule == ValidationContextEntityExists {
		seen := make(map[string]struct{}, len(validation.Bundles))
		for _, bundle := range validation.Bundles {
			if strings.TrimSpace(bundle) == "" || bundle != strings.TrimSpace(bundle) || strings.ContainsAny(bundle, "\x00\r\n:") {
				return fmt.Errorf("%s field %q validation %q has invalid bundle %q", direction, field.Name, validation.Rule, bundle)
			}
			if _, exists := seen[bundle]; exists {
				return fmt.Errorf("%s field %q validation %q repeats bundle %q", direction, field.Name, validation.Rule, bundle)
			}
			seen[bundle] = struct{}{}
		}
	} else if len(validation.Bundles) != 0 {
		return fmt.Errorf("%s field %q validation %q does not accept bundles", direction, field.Name, validation.Rule)
	}
	entityRule := validation.Rule == ValidationEntityReference || validation.Rule == ValidationTypedRelation || validation.Rule == ValidationContextEntityExists
	if entityRule {
		if !validValidationMachineName(validation.EntityType) {
			return fmt.Errorf("%s field %q validation %q requires a valid entity_type", direction, field.Name, validation.Rule)
		}
	} else if validation.EntityType != "" {
		return fmt.Errorf("%s field %q validation %q does not accept an entity_type", direction, field.Name, validation.Rule)
	}
	if validation.Rule == ValidationContextAllowedValue {
		if validation.Provider == "" || validation.Provider != strings.TrimSpace(validation.Provider) ||
			len(validation.Provider) > 256 || strings.ContainsAny(validation.Provider, "\x00\r\n") {
			return fmt.Errorf("%s field %q validation %q requires a valid provider", direction, field.Name, validation.Rule)
		}
	} else if validation.Provider != "" {
		return fmt.Errorf("%s field %q validation %q does not accept a provider", direction, field.Name, validation.Rule)
	}
	if validation.Rule == ValidationContextEntityExists {
		if len(validation.Handler) > 256 || validation.Handler != strings.TrimSpace(validation.Handler) || strings.ContainsAny(validation.Handler, "\x00\r\n") {
			return fmt.Errorf("%s field %q validation %q has an invalid handler", direction, field.Name, validation.Rule)
		}
	} else if validation.Handler != "" {
		return fmt.Errorf("%s field %q validation %q does not accept a handler", direction, field.Name, validation.Rule)
	}
	if validation.Rule == ValidationMediaExtension {
		if err := validateMediaExtensionPolicies(validation.MediaTypes); err != nil {
			return fmt.Errorf("%s field %q validation %q: %w", direction, field.Name, validation.Rule, err)
		}
	} else if len(validation.MediaTypes) != 0 {
		return fmt.Errorf("%s field %q validation %q does not accept media types", direction, field.Name, validation.Rule)
	}
	if validation.AllowNewNames {
		if validation.Rule != ValidationContextEntityExists || validation.EntityType != "taxonomy_term" {
			return fmt.Errorf("%s field %q validation %q allow_new_names requires a taxonomy_term context entity rule", direction, field.Name, validation.Rule)
		}
	}
	return nil
}

func validateMediaExtensionPolicies(policies []MediaExtensionPolicy) error {
	if len(policies) == 0 {
		return fmt.Errorf("requires at least one media type policy")
	}
	if len(policies) > 128 {
		return fmt.Errorf("exceeds 128 media type policies")
	}
	mediaTypes := make(map[string]struct{}, len(policies))
	selectors := make(map[string]string)
	fallbacks := 0
	for _, policy := range policies {
		if !validValidationMachineName(policy.MediaType) {
			return fmt.Errorf("has invalid media type %q", policy.MediaType)
		}
		if _, exists := mediaTypes[policy.MediaType]; exists {
			return fmt.Errorf("repeats media type %q", policy.MediaType)
		}
		mediaTypes[policy.MediaType] = struct{}{}
		if policy.Fallback {
			fallbacks++
		}
		if len(policy.SelectExtensions) > 1024 || len(policy.AllowedExtensions) > 1024 {
			return fmt.Errorf("media type %q exceeds 1024 extensions", policy.MediaType)
		}
		seenAllowed := make(map[string]struct{}, len(policy.AllowedExtensions))
		for _, extension := range policy.AllowedExtensions {
			if !validMediaExtension(extension) {
				return fmt.Errorf("media type %q has invalid allowed extension %q", policy.MediaType, extension)
			}
			if _, exists := seenAllowed[extension]; exists {
				return fmt.Errorf("media type %q repeats allowed extension %q", policy.MediaType, extension)
			}
			seenAllowed[extension] = struct{}{}
		}
		seenSelected := make(map[string]struct{}, len(policy.SelectExtensions))
		for _, extension := range policy.SelectExtensions {
			if !validMediaExtension(extension) {
				return fmt.Errorf("media type %q has invalid selector extension %q", policy.MediaType, extension)
			}
			if _, exists := seenSelected[extension]; exists {
				return fmt.Errorf("media type %q repeats selector extension %q", policy.MediaType, extension)
			}
			seenSelected[extension] = struct{}{}
			if owner, exists := selectors[extension]; exists {
				return fmt.Errorf("selector extension %q is shared by media types %q and %q", extension, owner, policy.MediaType)
			}
			selectors[extension] = policy.MediaType
		}
	}
	if fallbacks != 1 {
		return fmt.Errorf("requires exactly one fallback media type; got %d", fallbacks)
	}
	return nil
}

func validMediaExtension(value string) bool {
	if value == "" || len(value) > 64 || value != strings.TrimSpace(value) || value != strings.ToLower(value) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' || character == '-' || character == '+' {
			continue
		}
		return false
	}
	return true
}

func validateTableValidation(direction string, index int, validation Validation, fields map[string]Field) error {
	location := fmt.Sprintf("validation %d", index+1)
	if !validTableValidationRule(validation.Rule) {
		return fmt.Errorf("%s %s has unsupported rule %q", direction, location, validation.Rule)
	}
	allFields := make([]Field, 0, len(fields))
	for _, field := range fields {
		allFields = append(allFields, field)
	}
	if err := validateValidationCommon(direction, location, validation, allFields); err != nil {
		return err
	}
	if validation.Field != "" {
		return fmt.Errorf("%s %s rule %q does not accept a singular field", direction, location, validation.Rule)
	}
	if validation.Limit != 0 {
		return fmt.Errorf("%s %s rule %q does not accept a limit", direction, location, validation.Rule)
	}
	if validation.Minimum != nil || validation.Maximum != nil {
		return fmt.Errorf("%s %s rule %q does not accept numeric bounds", direction, location, validation.Rule)
	}
	if validation.Pattern != "" {
		return fmt.Errorf("%s %s rule %q does not accept a pattern", direction, location, validation.Rule)
	}
	if len(validation.Values) != 0 || validation.CaseInsensitive {
		return fmt.Errorf("%s %s rule %q does not accept enum values", direction, location, validation.Rule)
	}
	if len(validation.Bundles) != 0 {
		return fmt.Errorf("%s %s rule %q does not accept bundles", direction, location, validation.Rule)
	}
	if validation.EntityType != "" {
		return fmt.Errorf("%s %s rule %q does not accept an entity_type", direction, location, validation.Rule)
	}
	if validation.Provider != "" {
		return fmt.Errorf("%s %s rule %q does not accept a provider", direction, location, validation.Rule)
	}
	if validation.Handler != "" {
		return fmt.Errorf("%s %s rule %q does not accept a handler", direction, location, validation.Rule)
	}
	if len(validation.MediaTypes) != 0 {
		return fmt.Errorf("%s %s rule %q does not accept media types", direction, location, validation.Rule)
	}
	if validation.AllowNewNames {
		return fmt.Errorf("%s %s rule %q does not accept allow_new_names", direction, location, validation.Rule)
	}
	wantFields := 1
	switch validation.Rule {
	case ValidationReference, ValidationPrecedingReference, ValidationDifferent:
		wantFields = 2
	case ValidationRequiredAny:
		wantFields = -1
	}
	if (wantFields >= 0 && len(validation.Fields) != wantFields) || (wantFields < 0 && len(validation.Fields) < 2) {
		return fmt.Errorf("%s %s rule %q has %d fields; expected %s", direction, location, validation.Rule, len(validation.Fields), validationFieldCount(wantFields))
	}
	seen := make(map[string]struct{}, len(validation.Fields))
	for _, name := range validation.Fields {
		if err := validateCanonicalFieldReference(direction, location+" rule "+fmt.Sprintf("%q", validation.Rule), name, allFields); err != nil {
			return err
		}
		key := normalizedHeader(name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%s %s rule %q repeats field %q", direction, location, validation.Rule, name)
		}
		seen[key] = struct{}{}
	}
	if validation.Rule == ValidationRequiredWhen && validation.When == nil {
		return fmt.Errorf("%s %s rule %q requires a when predicate", direction, location, validation.Rule)
	}
	return nil
}

func validateValidationCommon(direction, location string, validation Validation, fields []Field) error {
	contextRule := validation.Rule == ValidationContextNodeExists || validation.Rule == ValidationContextEntityExists || validation.Rule == ValidationContextAllowedValue ||
		validation.Rule == ValidationContextFileReadable || validation.Rule == ValidationContextTGNResolves || validation.Rule == ValidationContextURLAliasAvailable
	if contextRule && validation.Phase != ValidationPhaseContext {
		return fmt.Errorf("%s %s validation %q must use context phase", direction, location, validation.Rule)
	}
	if !contextRule && validation.Phase != "" && validation.Phase != ValidationPhaseDeterministic {
		return fmt.Errorf("%s %s validation %q must use deterministic phase", direction, location, validation.Rule)
	}
	seenOperations := make(map[Operation]struct{}, len(validation.Operations))
	for _, operation := range validation.Operations {
		if !validOperation(operation) {
			return fmt.Errorf("%s %s validation %q has unsupported operation %q", direction, location, validation.Rule, operation)
		}
		if _, exists := seenOperations[operation]; exists {
			return fmt.Errorf("%s %s validation %q repeats operation %q", direction, location, validation.Rule, operation)
		}
		seenOperations[operation] = struct{}{}
	}
	if validation.When != nil {
		if err := validateCanonicalFieldReference(direction, location+" predicate", validation.When.Field, fields); err != nil {
			return err
		}
		switch validation.When.Operator {
		case ValidationOperatorIn:
			if len(validation.When.Values) == 0 || validation.When.Count != 0 {
				return fmt.Errorf("%s %s predicate %q requires values and no count", direction, location, validation.When.Operator)
			}
		case ValidationOperatorValueCountGreaterThan:
			if validation.When.Count < 0 || len(validation.When.Values) != 0 {
				return fmt.Errorf("%s %s predicate %q requires a non-negative count and no values", direction, location, validation.When.Operator)
			}
		default:
			return fmt.Errorf("%s %s has unsupported predicate operator %q", direction, location, validation.When.Operator)
		}
	}
	return nil
}

func validateCanonicalFieldReference(direction, location, name string, fields []Field) error {
	key := normalizedHeader(name)
	for _, field := range fields {
		if normalizedHeader(field.Name) != key {
			continue
		}
		if name != field.Name {
			return fmt.Errorf("%s %s must reference canonical field name %q instead of %q", direction, location, field.Name, name)
		}
		return nil
	}
	for _, field := range fields {
		if field.Matches(name) {
			return fmt.Errorf("%s %s must reference canonical field name %q instead of %q", direction, location, field.Name, name)
		}
	}
	return fmt.Errorf("%s %s references unknown field %q", direction, location, name)
}

func validationFieldCount(count int) string {
	if count < 0 {
		return "at least 2"
	}
	return fmt.Sprintf("exactly %d", count)
}

func validFieldValidationRule(rule ValidationRule) bool {
	switch rule {
	case ValidationMaximumRunes, ValidationAbsoluteHTTPURL, ValidationWorkbenchLink, ValidationGeolocation,
		ValidationAuthorityLink, ValidationMediaTrack, ValidationEntityReference, ValidationTypedRelation, ValidationDOI, ValidationRightsStatement,
		ValidationNoLineBreaks, ValidationEnum, ValidationFiniteNumber, ValidationNumericRange, ValidationPattern, ValidationNotFutureTimestamp, ValidationMediaExtension, ValidationContributor,
		ValidationGettyTGN, ValidationContextNodeExists, ValidationContextEntityExists, ValidationContextAllowedValue, ValidationContextFileReadable,
		ValidationContextTGNResolves, ValidationContextURLAliasAvailable:
		return true
	default:
		return false
	}
}

func validValidationMachineName(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (index > 0 && character >= '0' && character <= '9') || (index > 0 && character == '_') {
			continue
		}
		return false
	}
	return true
}

func validTableValidationRule(rule ValidationRule) bool {
	switch rule {
	case ValidationUnique, ValidationReference, ValidationPrecedingReference, ValidationDifferent, ValidationRequiredAny, ValidationRequiredWhen:
		return true
	default:
		return false
	}
}

func validWorkbenchTargetHub(hubPath string) bool {
	switch hubPath {
	case "Title", "FullTitle", "ObjectModel", "ResourceType", "AddCoverpage", "IsPublic",
		"Contributors", "Departments", "Genre", "Dates.issued", "Dates.created", "Dates.captured",
		"Dates.available", "Publisher", "PlacePublished", "Edition", "Language", "PhysicalForm", "Files.mime_type",
		"Extent", "PhysicalDesc", "DigitalOrigin", "Descriptions", "Notes", "LocalRestriction", "Subjects.lcsh",
		"Subjects.keywords", "Subjects.lcnaf", "Subjects.geographic", "Subjects.getty_tgn",
		"Publication.RelatedItem", "Publication.Part", "Identifiers", "Rights", "AccessCondition",
		"Relations.member_of", "Files.primary", "Files.supplemental":
		return true
	default:
		return strings.HasPrefix(hubPath, "Extra.") && strings.TrimPrefix(hubPath, "Extra.") != "" && strings.TrimSpace(hubPath) == hubPath
	}
}

func validCodec(codec string) bool {
	switch codec {
	case "", "string", "ignore", "multi", "file", "contributors", "boolean", "integer", "unsigned", "edtf", "restriction", "typed_relation", "attributes", "typed", "json", "profile_identifier":
		return true
	default:
		return false
	}
}

func validOperation(operation Operation) bool {
	switch operation {
	case OperationCreate, OperationUpdate, OperationAddMedia, OperationAgents, OperationUnpublishedSupplemental, OperationPendingSupplemental:
		return true
	default:
		return false
	}
}

// ComputeFingerprint returns a SHA-256 digest of the transformation's
// schema-relevant content. Existing digest and site display metadata do not
// affect the result.
func (t *Transformation) ComputeFingerprint() (string, error) {
	if t == nil {
		return "", fmt.Errorf("transformation specification is nil")
	}
	copy := *t
	copy.Fingerprint.Value = ""
	copy.Fingerprint.Algorithm = ""
	copy.Fingerprint.SiteUUID = ""
	copy.Fingerprint.SiteName = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", fmt.Errorf("encoding transformation fingerprint: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// SealFingerprint computes and records the current specification digest.
func (t *Transformation) SealFingerprint() error {
	value, err := t.ComputeFingerprint()
	if err != nil {
		return err
	}
	t.Fingerprint.Algorithm = "sha256"
	t.Fingerprint.Value = value
	return nil
}

// SourceField resolves a source header to its field definition.
func (t *Transformation) SourceField(header string) (Field, bool) {
	if t == nil {
		return Field{}, false
	}
	for _, field := range t.Source.Fields {
		if field.Matches(header) {
			return field, true
		}
	}
	return Field{}, false
}

// TargetField resolves a target machine name to its field definition.
func (t *Transformation) TargetField(name string) (Field, bool) {
	if t == nil {
		return Field{}, false
	}
	for _, field := range t.Target.Fields {
		if field.Matches(name) {
			return field, true
		}
	}
	return Field{}, false
}

func normalizedHeader(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizedObjectModel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", " ", "_", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}
