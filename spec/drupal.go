package spec

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/model"
	modeldrupal "github.com/lehigh-university-libraries/crosswalk/model/drupal"
)

const (
	defaultMaxConfigFileBytes = int64(2 << 20)
	defaultMaxConfigBytes     = int64(32 << 20)
	defaultMaxConfigFiles     = 10_000
)

// DrupalCompileOptions controls compilation of a Drupal node bundle into a
// transformation specification.
type DrupalCompileOptions struct {
	Bundle                string
	AllowNewTaxonomyTerms bool
	MaxFileBytes          int64
	MaxBytes              int64
	MaxFiles              int
}

// CompileDrupalDirectory compiles a transformation from an uncompressed
// Drupal config/sync directory. The canonical Drupal model compiler is the
// only component that interprets config/sync; this package derives a tabular
// contract from that immutable model.
func CompileDrupalDirectory(dir string, options DrupalCompileOptions) (*Transformation, error) {
	options = normalizeDrupalCompileOptions(options)
	snapshot, err := modeldrupal.CompileDirectory(dir, modeldrupal.CompileOptions{
		MaxFileBytes: options.MaxFileBytes,
		MaxBytes:     options.MaxBytes,
		MaxFiles:     options.MaxFiles,
	})
	if err != nil {
		return nil, err
	}
	return CompileDrupalModel(snapshot, options)
}

// CompileDrupalArchive compiles a transformation from a gzip-compressed tar
// config export without extracting it to disk. Unsafe paths, links, and
// oversized archives are rejected even though no archive member is written.
func CompileDrupalArchive(r io.Reader, options DrupalCompileOptions) (*Transformation, error) {
	options = normalizeDrupalCompileOptions(options)
	snapshot, err := modeldrupal.CompileArchive(r, modeldrupal.CompileOptions{
		MaxFileBytes: options.MaxFileBytes,
		MaxBytes:     options.MaxBytes,
		MaxFiles:     options.MaxFiles,
	})
	if err != nil {
		return nil, err
	}
	return CompileDrupalModel(snapshot, options)
}

type drupalStorageConfig struct {
	Type        string
	Cardinality int
	Settings    map[string]any
}

type drupalFieldConfig struct {
	Label       string
	Description string
	Required    bool
	Settings    map[string]any
}

type drupalAttachedField struct {
	storage   drupalStorageConfig
	field     drupalFieldConfig
	reference *model.Reference
}

// CompileDrupalModel derives an Islandora Workbench transformation from the
// exact Drupal model used by canonical profiles. It performs no acquisition
// or YAML parsing.
func CompileDrupalModel(snapshot *model.Snapshot, options DrupalCompileOptions) (*Transformation, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("drupal model snapshot is required")
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Drupal model snapshot: %w", err)
	}
	if snapshot.System != "drupal" {
		return nil, fmt.Errorf("model system %q is not Drupal", snapshot.System)
	}
	bundle := strings.TrimSpace(options.Bundle)
	if bundle == "" {
		return nil, fmt.Errorf("drupal node bundle is required")
	}
	if !validDrupalBundle(bundle) {
		return nil, fmt.Errorf("drupal node bundle %q is not a valid machine name", bundle)
	}
	entity, ok := snapshot.Entity("node", bundle)
	if !ok {
		return nil, fmt.Errorf("drupal node bundle %q was not found in model snapshot", bundle)
	}
	attached := make(map[string]drupalAttachedField)
	for _, field := range entity.Fields {
		// Workbench accepts the authored creation timestamp but not Drupal's
		// runtime-managed modification timestamp. Keeping changed in a generated
		// sheet would advertise an input that Workbench silently ignores.
		if field.Path == "changed" {
			continue
		}
		attached[field.Path] = drupalAttachedField{
			storage: drupalStorageConfig{
				Type: field.SourceType, Cardinality: field.Cardinality,
				Settings: cloneMap(field.StorageSettings),
			},
			field: drupalFieldConfig{
				Label: field.Label, Description: field.Description,
				Required: field.Required, Settings: cloneMap(field.InstanceSettings),
			},
			reference: field.Reference,
		}
	}

	transformation := reconcileDrupalFields(FabricatorWorkbench(), attached)
	configureDrupalWorkbenchMediaValidations(transformation, snapshot)
	configureDrupalTaxonomyNamePolicy(transformation, options.AllowNewTaxonomyTerms)
	transformation.Name = "drupal-" + bundle + "-workbench"
	transformation.Description = fmt.Sprintf("Drupal %s node bundle to Islandora Workbench CSV", bundle)
	transformation.Fingerprint = Fingerprint{
		Model:      snapshot.Fingerprint.Value,
		SiteUUID:   snapshot.Provenance.SiteUUID,
		SiteName:   snapshot.Provenance.SiteName,
		Bundle:     bundle,
		ConfigHash: snapshot.Provenance.ConfigHash,
	}
	if err := transformation.SealFingerprint(); err != nil {
		return nil, err
	}
	if err := transformation.Validate(); err != nil {
		return nil, fmt.Errorf("validating compiled Drupal transformation: %w", err)
	}
	return transformation, nil
}

func reconcileDrupalFields(base *Transformation, attached map[string]drupalAttachedField) *Transformation {
	result := &Transformation{
		Version:  CurrentVersion,
		Source:   base.Source,
		Target:   base.Target,
		Defaults: cloneStringMap(base.Defaults),
	}
	result.Source.Fields = nil
	result.Target.Fields = nil
	result.Source.RequiredGroups = nil
	result.Target.RequiredGroups = nil
	// The built-in Fabricator policy is a compatibility preset for one legacy
	// deployment. A spec compiled from an institution's Drupal model must not
	// silently inherit Lehigh-specific filesystem roots or taxonomy term IDs.
	result.Defaults = nil

	seenSource := make(map[string]struct{})
	seenTarget := make(map[string]struct{})
	sourceFieldCounts := make(map[string]int)
	labelCounts := make(map[string]int)
	for _, config := range attached {
		if label := normalizedHeader(config.field.Label); label != "" {
			labelCounts[label]++
		}
	}
	for _, field := range base.Source.Fields {
		if _, ok := attached[drupalBaseField(field.Name)]; ok {
			sourceFieldCounts[drupalBaseField(field.Name)]++
		}
	}
	for _, field := range base.Source.Fields {
		baseName := drupalBaseField(field.Name)
		config, attachedToBundle := attached[baseName]
		if !attachedToBundle && !isWorkbenchOperationalField(field.Name) {
			continue
		}
		if attachedToBundle {
			useLabel := sourceFieldCounts[baseName] == 1 && labelCounts[normalizedHeader(config.field.Label)] == 1
			field = applyDrupalFieldConfig(field, config, useLabel, sourceFieldCounts[baseName] == 1)
		}
		result.Source.Fields = append(result.Source.Fields, field)
		seenSource[drupalBaseField(field.Name)] = struct{}{}
	}
	for _, field := range base.Target.Fields {
		config, attachedToBundle := attached[drupalBaseField(field.Name)]
		if !attachedToBundle && !isWorkbenchOperationalField(field.Name) {
			continue
		}
		if attachedToBundle {
			field = applyDrupalFieldConfig(field, config, false, false)
			field.Required = config.field.Required
		}
		result.Target.Fields = append(result.Target.Fields, field)
		seenTarget[drupalBaseField(field.Name)] = struct{}{}
	}
	reservedFields := []Field{
		{
			Name: "url_alias", Label: "URL Alias", Hub: "Extra.url_alias", Codec: "string", Cardinality: 1,
			Operations: []Operation{OperationCreate, OperationUpdate},
			Validations: []Validation{
				{Rule: ValidationPattern, Pattern: `^/.*$`},
				{Rule: ValidationNoLineBreaks},
				{Rule: ValidationContextURLAliasAvailable, Phase: ValidationPhaseContext},
			},
		},
		{
			Name: "langcode", Label: "Language Code", Hub: "Extra.langcode", Codec: "string", Cardinality: 1,
			Operations:  []Operation{OperationCreate, OperationUpdate},
			Validations: []Validation{{Rule: ValidationEnum, Values: append([]string(nil), drupalSupportedLanguageCodes...)}},
		},
	}
	for _, field := range reservedFields {
		if _, exists := seenSource[field.Name]; !exists {
			result.Source.Fields = append(result.Source.Fields, field)
			seenSource[field.Name] = struct{}{}
		}
		if _, exists := seenTarget[field.Name]; !exists {
			field.Validations = nil
			result.Target.Fields = append(result.Target.Fields, field)
			seenTarget[field.Name] = struct{}{}
		}
	}
	result.Source.Validations = append(result.Source.Validations, Validation{Rule: ValidationUnique, Fields: []string{"url_alias"}})

	names := make([]string, 0, len(attached))
	for name := range attached {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		config := attached[name]
		if _, exists := seenSource[name]; !exists {
			useLabel := labelCounts[normalizedHeader(config.field.Label)] == 1
			result.Source.Fields = append(result.Source.Fields, genericDrupalField(name, config, useLabel))
		}
		if _, exists := seenTarget[name]; !exists {
			field := genericDrupalField(name, config, false)
			field.Required = config.field.Required
			field.RequiredFor = nil
			result.Target.Fields = append(result.Target.Fields, field)
		}
	}
	result.Source.Fields = uniqueDrupalHeaders(result.Source.Fields)
	result.Target.Fields = uniqueDrupalHeaders(result.Target.Fields)
	result.Source.Fields = retainFieldValidations(result.Source.Fields)
	result.Target.Fields = retainFieldValidations(result.Target.Fields)
	result.Source.RequiredGroups = requiredDrupalSourceGroups(result.Source.Fields, attached)
	result.Source.Validations = retainTableValidations(result.Source.Validations, result.Source.Fields)
	result.Target.Validations = retainTableValidations(result.Target.Validations, result.Target.Fields)
	return result
}

func retainFieldValidations(fields []Field) []Field {
	available := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		available[normalizedHeader(field.Name)] = struct{}{}
	}
	for index := range fields {
		retained := fields[index].Validations[:0]
		for _, validation := range fields[index].Validations {
			if validation.Field != "" {
				if _, exists := available[normalizedHeader(validation.Field)]; !exists {
					continue
				}
			}
			if validation.When != nil {
				if _, exists := available[normalizedHeader(validation.When.Field)]; !exists {
					continue
				}
			}
			retained = append(retained, validation)
		}
		fields[index].Validations = retained
	}
	return fields
}

func retainTableValidations(validations []Validation, fields []Field) []Validation {
	available := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		available[normalizedHeader(field.Name)] = struct{}{}
	}
	retained := validations[:0]
	for _, validation := range validations {
		valid := true
		for _, name := range validation.Fields {
			if _, exists := available[normalizedHeader(name)]; !exists {
				valid = false
				break
			}
		}
		if valid && validation.When != nil {
			_, valid = available[normalizedHeader(validation.When.Field)]
		}
		if valid {
			retained = append(retained, validation)
		}
	}
	return retained
}

func requiredDrupalSourceGroups(fields []Field, attached map[string]drupalAttachedField) []RequiredGroup {
	names := make([]string, 0, len(attached))
	for name, config := range attached {
		if config.field.Required {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	groups := make([]RequiredGroup, 0)
	for _, name := range names {
		members := make([]string, 0)
		for _, field := range fields {
			if drupalBaseField(field.Name) == name {
				members = append(members, field.Name)
			}
		}
		if len(members) < 2 {
			continue
		}
		groups = append(groups, RequiredGroup{
			Name:        name,
			Fields:      members,
			RequiredFor: []Operation{OperationCreate},
		})
	}
	return groups
}

func uniqueDrupalHeaders(fields []Field) []Field {
	owners := make(map[string]string, len(fields)*2)
	for _, field := range fields {
		owners[normalizedHeader(field.Name)] = field.Name
	}
	for index := range fields {
		field := &fields[index]
		if label := normalizedHeader(field.Label); label != "" {
			if owner, exists := owners[label]; exists && owner != field.Name {
				field.Label = ""
			} else {
				owners[label] = field.Name
			}
		}
		aliases := field.Aliases[:0]
		for _, alias := range field.Aliases {
			header := normalizedHeader(alias)
			if header == "" {
				continue
			}
			if owner, exists := owners[header]; exists && owner != field.Name {
				continue
			}
			owners[header] = field.Name
			aliases = append(aliases, alias)
		}
		field.Aliases = aliases
	}
	return fields
}

func applyDrupalFieldConfig(field Field, config drupalAttachedField, useLabel, inferRequired bool) Field {
	field.SchemaLabel = config.field.Label
	if useLabel && config.field.Label != "" && config.field.Label != field.Label {
		if field.Label != "" && !containsString(field.Aliases, field.Label) {
			field.Aliases = append(field.Aliases, field.Label)
		}
		field.Label = config.field.Label
	}
	field.Description = config.field.Description
	field.SourceType = config.storage.Type
	field.Settings = cloneMap(config.storage.Settings)
	field.InstanceSettings = cloneMap(config.field.Settings)
	field.Cardinality = drupalCardinality(config.storage.Cardinality)
	if inferRequired && config.field.Required && !containsOperation(field.RequiredFor, OperationCreate) {
		field.RequiredFor = append(field.RequiredFor, OperationCreate)
	}
	compiledValidations := compileDrupalFieldValidations(config)
	if field.Name != drupalBaseField(field.Name) {
		compiledValidations = retainScalarDrupalValidations(compiledValidations)
	}
	if hasValidationRule(field.Validations, ValidationContextNodeExists) {
		compiledValidations = withoutValidationRule(compiledValidations, ValidationContextEntityExists)
	}
	field.Validations = mergeDrupalFieldValidations(field.Validations, compiledValidations)
	field.Validations = configureContributorRelators(field.Validations, drupalConfiguredStrings(config.field.Settings["rel_types"]))
	field.Validations = ensureWorkbenchLineBreakValidation(field)
	return field
}

func hasValidationRule(validations []Validation, rule ValidationRule) bool {
	for _, validation := range validations {
		if validation.Rule == rule {
			return true
		}
	}
	return false
}

func withoutValidationRule(validations []Validation, rule ValidationRule) []Validation {
	result := validations[:0]
	for _, validation := range validations {
		if validation.Rule != rule {
			result = append(result, validation)
		}
	}
	return result
}

func retainScalarDrupalValidations(validations []Validation) []Validation {
	result := validations[:0]
	for _, validation := range validations {
		switch validation.Rule {
		case ValidationGeolocation, ValidationAuthorityLink, ValidationMediaTrack, ValidationEntityReference, ValidationTypedRelation, ValidationContextEntityExists:
			continue
		default:
			result = append(result, validation)
		}
	}
	return result
}

func configureContributorRelators(validations []Validation, relators []string) []Validation {
	if len(relators) == 0 {
		return validations
	}
	for index := range validations {
		if validations[index].Rule == ValidationContributor {
			validations[index].Values = append([]string(nil), relators...)
		}
	}
	return validations
}

func genericDrupalField(name string, config drupalAttachedField, useLabel bool) Field {
	operations := []Operation{OperationCreate, OperationUpdate}
	if name == "uid" {
		// Workbench only applies an explicit owner while creating a node.
		operations = []Operation{OperationCreate}
	}
	field := Field{
		Name:             name,
		SchemaLabel:      config.field.Label,
		Description:      config.field.Description,
		Hub:              "Extra.drupal." + name,
		Codec:            drupalCodec(config.storage.Type, config.storage.Cardinality),
		SourceType:       config.storage.Type,
		Settings:         cloneMap(config.storage.Settings),
		InstanceSettings: cloneMap(config.field.Settings),
		Cardinality:      drupalCardinality(config.storage.Cardinality),
		Operations:       operations,
	}
	if useLabel {
		field.Label = config.field.Label
	}
	if config.field.Required {
		field.RequiredFor = []Operation{OperationCreate}
	}
	field.Validations = compileDrupalFieldValidations(config)
	if name == "created" {
		field.Validations = mergeDrupalFieldValidations(field.Validations, []Validation{
			{Rule: ValidationPattern, Pattern: `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}$`},
			{Rule: ValidationNotFutureTimestamp},
		})
	}
	field.Validations = ensureWorkbenchLineBreakValidation(field)
	return field
}

func ensureWorkbenchLineBreakValidation(field Field) []Validation {
	if !workbenchSourceRejectsLineBreaks(field) {
		return field.Validations
	}
	for _, validation := range field.Validations {
		if validation.Rule == ValidationNoLineBreaks {
			return field.Validations
		}
	}
	return append(field.Validations, Validation{Rule: ValidationNoLineBreaks})
}

func drupalBaseField(name string) string {
	if index := strings.IndexByte(name, '.'); index >= 0 {
		return name[:index]
	}
	return name
}

func isWorkbenchOperationalField(name string) bool {
	switch drupalBaseField(name) {
	case "id", "parent_id", "field_weight", "node_id", "file", "supplemental_file", "unpublished_supplemental_file", "title", "published":
		return true
	default:
		return false
	}
}

func drupalCardinality(cardinality int) int {
	if cardinality < 0 {
		return 0
	}
	return cardinality
}

func drupalCodec(fieldType string, cardinality int) string {
	switch fieldType {
	case "boolean":
		return "boolean"
	case "integer", "list_integer":
		return "integer"
	case "datetime", "daterange", "edtf":
		return "edtf"
	default:
		if cardinality != 1 {
			return "multi"
		}
		return "string"
	}
}

func normalizeDrupalCompileOptions(options DrupalCompileOptions) DrupalCompileOptions {
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

func validDrupalBundle(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsOperation(values []Operation, target Operation) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
