package spec

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

// CompileDrupalProfile composes an immutable Drupal profile with the exact
// model-backed Islandora Workbench layout. The transformation specification
// remains responsible for source spreadsheet columns and Workbench operational
// policy; the compiled profile remains the sole authority for Hub-to-Drupal
// field values at serialization time.
func CompileDrupalProfile(snapshot *model.Snapshot, compiled *profile.Compiled, options DrupalCompileOptions) (*Transformation, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("compiling Drupal profile transformation: model snapshot is nil")
	}
	if compiled == nil {
		return nil, fmt.Errorf("compiling Drupal profile transformation: compiled profile is nil")
	}
	if compiled.System() != "drupal" {
		return nil, fmt.Errorf("compiling Drupal profile transformation: profile system %q is not drupal", compiled.System())
	}
	if compiled.ModelFingerprint() != snapshot.Fingerprint.Value {
		return nil, fmt.Errorf(
			"compiling Drupal profile transformation: profile model fingerprint %s does not match snapshot %s",
			compiled.ModelFingerprint(), snapshot.Fingerprint.Value,
		)
	}

	mappings := compiled.Mappings()
	bundle := strings.TrimSpace(options.Bundle)
	mappedPaths := make(map[string]struct{})
	for _, mapping := range mappings {
		if !profileMappingIsWorkbenchInput(mapping) {
			continue
		}
		selector := mapping.Field.Selector
		if selector.EntityType != "node" || strings.TrimSpace(selector.Bundle) == "" {
			return nil, fmt.Errorf(
				"compiling Drupal profile transformation: mapping %d field %q does not select a Drupal node bundle",
				mapping.Position+1, selector.Path,
			)
		}
		if bundle == "" {
			bundle = selector.Bundle
		}
		if selector.Bundle != bundle {
			return nil, fmt.Errorf(
				"compiling Drupal profile transformation: profile spans Drupal node bundles %q and %q",
				bundle, selector.Bundle,
			)
		}
		if mapping.Encode != "none" {
			if err := validateProfileWorkbenchEncoding(mapping); err != nil {
				return nil, fmt.Errorf("compiling Drupal profile transformation: mapping %d: %w", mapping.Position+1, err)
			}
			mappedPaths[selector.Path] = struct{}{}
		}
	}
	if bundle == "" {
		return nil, fmt.Errorf("compiling Drupal profile transformation: profile has no Drupal node mappings")
	}
	if len(mappedPaths) == 0 {
		return nil, fmt.Errorf("compiling Drupal profile transformation: profile has no encodable Drupal mappings")
	}
	options.Bundle = bundle

	transformation, err := CompileDrupalModel(snapshot, options)
	if err != nil {
		return nil, fmt.Errorf("compiling Drupal profile transformation: %w", err)
	}
	profileMappings := make(map[string][]profile.CompiledMapping)
	identifierRules, err := profileIdentifierBindings(compiled.LookupPlan().Identifiers)
	if err != nil {
		return nil, fmt.Errorf("compiling Drupal profile transformation: %w", err)
	}
	for _, mapping := range mappings {
		if mapping.Encode == "none" && mapping.Decode == "none" {
			continue
		}
		if !profileMappingIsWorkbenchInput(mapping) {
			continue
		}
		profileMappings[mapping.Field.Selector.Path] = append(profileMappings[mapping.Field.Selector.Path], mapping)
	}
	transformation.Source.Fields, err = profileBoundSourceFields(transformation.Source.Fields, profileMappings, identifierRules)
	if err != nil {
		return nil, fmt.Errorf("compiling Drupal profile transformation: %w", err)
	}
	transformation.Source.RequiredGroups = retainedRequiredGroups(transformation.Source.RequiredGroups, transformation.Source.Fields)
	transformation.Source.RequiredGroups = expandRequiredGroups(transformation.Source.RequiredGroups, transformation.Source.Fields)
	transformation.Source.Fields = retainFieldValidations(transformation.Source.Fields)
	transformation.Source.Validations = retainTableValidations(transformation.Source.Validations, transformation.Source.Fields)
	filtered := make([]Field, 0, len(transformation.Target.Fields))
	resolved := make(map[string]struct{}, len(mappedPaths))
	for _, field := range transformation.Target.Fields {
		path := drupalBaseField(field.Name)
		_, mapped := mappedPaths[path]
		if !mapped && !isWorkbenchTransportField(field.Name) {
			continue
		}
		filtered = append(filtered, field)
		if mapped {
			resolved[path] = struct{}{}
		}
	}
	missing := make([]string, 0)
	for path := range mappedPaths {
		if _, exists := resolved[path]; !exists {
			missing = append(missing, path)
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf(
			"compiling Drupal profile transformation: encodable profile fields are absent from the Workbench model layout: %s",
			strings.Join(missing, ", "),
		)
	}
	transformation.Target.Fields = filtered
	transformation.Target.Fields = retainFieldValidations(transformation.Target.Fields)
	transformation.Target.Validations = retainTableValidations(transformation.Target.Validations, transformation.Target.Fields)
	// profileBoundSourceFields may add selector-specific columns after the
	// model-only pass, so apply the explicit Workbench term-creation policy to
	// the final source contract as well.
	configureDrupalTaxonomyNamePolicy(transformation, options.AllowNewTaxonomyTerms)
	transformation.Name = "drupal-" + bundle + "-" + compiled.Name() + "-workbench"
	transformation.Description = fmt.Sprintf(
		"Profile %s mapping Drupal %s node metadata to Islandora Workbench CSV",
		compiled.Name(), bundle,
	)
	transformation.Fingerprint.Profile = compiled.Fingerprint()
	if err := transformation.SealFingerprint(); err != nil {
		return nil, fmt.Errorf("sealing Drupal profile transformation: %w", err)
	}
	if err := transformation.Validate(); err != nil {
		return nil, fmt.Errorf("validating Drupal profile transformation: %w", err)
	}
	return transformation, nil
}

func expandRequiredGroups(groups []RequiredGroup, fields []Field) []RequiredGroup {
	result := append([]RequiredGroup(nil), groups...)
	for index := range result {
		prefix := result[index].Name + "."
		seen := make(map[string]struct{}, len(result[index].Fields))
		for _, name := range result[index].Fields {
			seen[name] = struct{}{}
		}
		for _, field := range fields {
			if strings.HasPrefix(field.Name, prefix) {
				if _, exists := seen[field.Name]; !exists {
					result[index].Fields = append(result[index].Fields, field.Name)
					seen[field.Name] = struct{}{}
				}
			}
		}
	}
	return result
}

func retainedRequiredGroups(groups []RequiredGroup, fields []Field) []RequiredGroup {
	available := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		available[field.Name] = struct{}{}
	}
	result := make([]RequiredGroup, 0, len(groups))
	for _, group := range groups {
		members := make([]string, 0, len(group.Fields))
		for _, field := range group.Fields {
			if _, exists := available[field]; exists {
				members = append(members, field)
			}
		}
		if len(members) == 0 {
			continue
		}
		group.Fields = members
		result = append(result, group)
	}
	return result
}

type profileIdentifierBinding struct {
	Name          string
	Scheme        string
	Pattern       string
	Canonicalizer string
}

func profileIdentifierBindings(rules []profile.CompiledIdentifierRule) (map[string]profileIdentifierBinding, error) {
	bindings := make(map[string]profileIdentifierBinding)
	for _, rule := range rules {
		key := profileSelectorKey(rule.Value.Selector)
		candidate := profileIdentifierBinding{
			Name: rule.Name, Scheme: rule.Scheme,
			Pattern: rule.Pattern, Canonicalizer: rule.Canonicalizer,
		}
		previous, exists := bindings[key]
		if !exists {
			bindings[key] = candidate
			continue
		}
		if previous.Scheme != candidate.Scheme || previous.Pattern != candidate.Pattern || previous.Canonicalizer != candidate.Canonicalizer {
			first, second := previous.Name, candidate.Name
			if second < first {
				first, second = second, first
			}
			return nil, fmt.Errorf(
				"profile identity rules %q and %q share exact selector %q but differ in scheme, pattern, or canonicalizer",
				first, second, profileSelectorFieldName(rule.Value.Selector),
			)
		}
		// Equivalent rules may describe identity levels that a single Workbench
		// cell cannot distinguish (notably DOI work and version). Select the
		// lexicographically first rule name so the representative is independent
		// of policy order; this deliberately selects "doi" over "doi-version".
		if candidate.Name < previous.Name {
			bindings[key] = candidate
		}
	}
	return bindings, nil
}

func profileBoundSourceFields(fields []Field, mappings map[string][]profile.CompiledMapping, identifierRules map[string]profileIdentifierBinding) ([]Field, error) {
	result := make([]Field, 0, len(fields))
	seenSelectors := make(map[string]struct{})
	for _, field := range fields {
		path := drupalBaseField(field.Name)
		candidates := mappings[path]
		if isWorkbenchTransportField(field.Name) && (len(candidates) == 0 || field.Name != path || path == "title") {
			result = append(result, field)
			continue
		}
		if len(candidates) == 0 {
			continue
		}
		selected, exists, err := profileMappingForSpecField(field, candidates)
		if err != nil {
			return nil, err
		}
		if !exists || selected.Decode == "none" {
			continue
		}
		profileHub := selected.Hub
		profileBase, _, _ := strings.Cut(profileHub, ".")
		fieldBase, _, _ := strings.Cut(field.Hub, ".")
		if profileHub != profileBase || profileBase != fieldBase {
			field.Hub = profileHub
			field.Codec = profileSourceCodec(selected)
		}
		if selected.Decode == "typed-identifier" {
			binding := identifierRules[profileSelectorKey(selected.Field.Selector)]
			field.Hub = "Identifiers"
			field.Codec = "profile_identifier"
			field.ProfileRule = binding.Name
			if field.ProfileRule == "" {
				return nil, fmt.Errorf("source field %q typed-identifier mapping has no exact profile identity rule", field.Name)
			}
			field.Validations = mergeDrupalFieldValidations(field.Validations, []Validation{{Rule: ValidationPattern, Pattern: binding.Pattern}})
		}
		if strings.EqualFold(strings.TrimSpace(selected.Field.SourceType), "created") {
			// The generic profile Hub path is intentionally broad, but Workbench's
			// authored creation timestamp must remain distinguishable from issued
			// and descriptive dates so it can be emitted with its full RFC 3339
			// value rather than as an EDTF calendar date.
			field.Hub = "Dates.Created"
			field.Codec = "edtf"
		}
		result = append(result, field)
		seenSelectors[profileSelectorKey(selected.Field.Selector)] = struct{}{}
	}
	extraSelectors := make([]string, 0)
	bySelector := make(map[string]profile.CompiledMapping)
	for _, candidates := range mappings {
		for _, mapping := range candidates {
			key := profileSelectorKey(mapping.Field.Selector)
			if _, exists := seenSelectors[key]; exists || !profileMappingNeedsSourceField(mapping) {
				continue
			}
			if _, exists := bySelector[key]; !exists {
				bySelector[key] = mapping
				extraSelectors = append(extraSelectors, key)
			}
		}
	}
	sort.Strings(extraSelectors)
	for _, key := range extraSelectors {
		mapping := bySelector[key]
		resolved := mapping.Field
		field := Field{
			Name: profileSelectorFieldName(mapping.Field.Selector), Hub: mapping.Hub,
			SchemaLabel: resolved.Label, Description: resolved.Description,
			Codec: profileSourceCodec(mapping), SourceType: resolved.SourceType,
			Settings: cloneMap(resolved.StorageSettings), InstanceSettings: cloneMap(resolved.InstanceSettings),
			Cardinality: drupalCardinality(resolved.Cardinality),
			Operations:  []Operation{OperationCreate, OperationUpdate},
		}
		config := drupalAttachedField{
			storage:   drupalStorageConfig{Type: resolved.SourceType, Cardinality: resolved.Cardinality, Settings: cloneMap(resolved.StorageSettings)},
			field:     drupalFieldConfig{Label: resolved.Label, Description: resolved.Description, Required: resolved.Required, Settings: cloneMap(resolved.InstanceSettings)},
			reference: resolved.Reference,
		}
		field.Validations = retainScalarDrupalValidations(compileDrupalFieldValidations(config))
		field.Validations = ensureWorkbenchLineBreakValidation(field)
		if mapping.Decode == "typed-identifier" {
			binding := identifierRules[key]
			if binding.Name == "" {
				return nil, fmt.Errorf("typed-identifier selector for Drupal field %q has no exact profile identity rule", mapping.Field.Selector.Path)
			}
			field.Hub = "Identifiers"
			field.Codec = "profile_identifier"
			field.ProfileRule = binding.Name
			field.Validations = mergeDrupalFieldValidations(field.Validations, []Validation{{Rule: ValidationPattern, Pattern: binding.Pattern}})
		}
		result = append(result, field)
	}
	return result, nil
}

func profileMappingNeedsSourceField(mapping profile.CompiledMapping) bool {
	if mapping.Decode == "typed-identifier" {
		return true
	}
	if mapping.Decode != "composite" {
		return false
	}
	switch mapping.Field.SourceType {
	case "related_item", "part_detail":
		return true
	default:
		return false
	}
}

func profileMappingIsWorkbenchInput(mapping profile.CompiledMapping) bool {
	// Drupal changes this base field itself and Islandora Workbench does not
	// accept it as node input. It may remain useful in the immutable system
	// profile for Drupal JSON conversion, but it must not enter the Workbench
	// transformation contract.
	return mapping.Field.Selector.Path != "changed"
}

func profileSelectorFieldName(selector profile.FieldSelector) string {
	name := selector.Path
	if selector.Where != nil {
		return name + "." + selector.Where.Attribute + "=" + selector.Where.Equals
	}
	if selector.Attribute != "" {
		return name + "." + selector.Attribute
	}
	return name
}

func profileSelectorKey(selector profile.FieldSelector) string {
	predicateAttribute, predicateValue := "", ""
	if selector.Where != nil {
		predicateAttribute = selector.Where.Attribute
		predicateValue = selector.Where.Equals
	}
	return strings.Join([]string{selector.EntityType, selector.Bundle, selector.Path, selector.Attribute, predicateAttribute, predicateValue}, "\x00")
}

func profileMappingForSpecField(field Field, mappings []profile.CompiledMapping) (profile.CompiledMapping, bool, error) {
	name := field.Name
	for _, mapping := range mappings {
		selector := mapping.Field.Selector
		if name == profileSelectorFieldName(selector) {
			return mapping, true, nil
		}
	}
	if len(mappings) == 1 {
		mapping := mappings[0]
		selector := mapping.Field.Selector
		mappingHub, _, _ := strings.Cut(mapping.Hub, ".")
		fieldHub, _, _ := strings.Cut(field.Hub, ".")
		if selector.Attribute == "" && selector.Where == nil &&
			selector.Path == drupalBaseField(field.Name) && mappingHub != "" && mappingHub == fieldHub {
			return mapping, true, nil
		}
	}
	return profile.CompiledMapping{}, false, nil
}

func profileSourceCodec(mapping profile.CompiledMapping) string {
	if mapping.Decode == "none" {
		return "ignore"
	}
	if mapping.Field.Cardinality != 1 {
		return "multi"
	}
	switch mapping.Decode {
	case "boolean":
		return "boolean"
	case "integer":
		return "integer"
	case "date":
		return "edtf"
	case "file":
		return "file"
	default:
		return "string"
	}
}

func validateProfileWorkbenchEncoding(mapping profile.CompiledMapping) error {
	sourceType := strings.ToLower(strings.TrimSpace(mapping.Field.SourceType))
	supported := false
	switch sourceType {
	case "string", "string_long", "string_textfield", "text", "text_long", "text_with_summary",
		"email", "telephone", "boolean", "integer", "list_integer", "list_string", "decimal", "float",
		"datetime", "daterange", "edtf", "created", "link", "entity_reference", "typed_relation",
		"geolocation", "authority_link", "media_track", "linked_data_field",
		"textfield_attr", "textarea_attr", "part_detail", "related_item":
		supported = true
	}
	if !supported {
		return fmt.Errorf(
			"drupal field %q source type %q has no explicit Workbench cell encoding",
			mapping.Field.Selector.Path, mapping.Field.SourceType,
		)
	}
	if sourceType == "typed_relation" && mapping.Encode != "typed-identifier" {
		if mapping.Field.Reference == nil || len(mapping.Field.Reference.Bundles) == 0 {
			return fmt.Errorf("drupal field %q typed relation requires at least one model-declared reference bundle for Workbench encoding", mapping.Field.Selector.Path)
		}
		for _, bundle := range mapping.Field.Reference.Bundles {
			if strings.TrimSpace(bundle) == "" {
				return fmt.Errorf("drupal field %q typed relation has an empty reference bundle", mapping.Field.Selector.Path)
			}
		}
	}
	selector := mapping.Field.Selector
	switch sourceType {
	case "textfield_attr", "textarea_attr":
		if selector.Attribute != "value" || selector.Where == nil || selector.Where.Attribute != "attr0" {
			return fmt.Errorf("drupal field %q attribute mapping must explicitly select value where attr0 equals a discriminator", selector.Path)
		}
	case "part_detail":
		if selector.Attribute == "" || selector.Where == nil || selector.Where.Attribute != "type" {
			return fmt.Errorf("drupal field %q part-detail mapping must explicitly select an attribute where type equals a discriminator", selector.Path)
		}
		switch selector.Attribute {
		case "number", "title", "caption":
		default:
			return fmt.Errorf("drupal field %q part-detail attribute %q is unsupported by Workbench", selector.Path, selector.Attribute)
		}
		if !profilePartDetailHubSupported(mapping.Hub, selector.Attribute) {
			return fmt.Errorf("drupal field %q part-detail attribute %q cannot encode Hub path %q for Workbench", selector.Path, selector.Attribute, mapping.Hub)
		}
	case "related_item":
		if selector.Attribute == "" {
			return fmt.Errorf("drupal field %q related-item mapping must explicitly select title, identifier, or number", selector.Path)
		}
		switch selector.Attribute {
		case "title", "identifier", "number":
		default:
			return fmt.Errorf("drupal field %q related-item attribute %q is unsupported by Workbench", selector.Path, selector.Attribute)
		}
		if selector.Where != nil && selector.Where.Attribute != "identifier_type" {
			return fmt.Errorf("drupal field %q related-item predicate must select identifier_type", selector.Path)
		}
		if !profileRelatedItemHubSupported(mapping.Hub, selector.Attribute) {
			return fmt.Errorf("drupal field %q related-item attribute %q cannot encode Hub path %q for Workbench", selector.Path, selector.Attribute, mapping.Hub)
		}
	}
	return nil
}

func profilePartDetailHubSupported(hubPath, attribute string) bool {
	base, qualifier, _ := strings.Cut(hubPath, ".")
	if base == "Extra" {
		return qualifier != ""
	}
	if base != "Publication" || attribute != "number" {
		return false
	}
	switch qualifier {
	case "Volume", "Issue", "Pages":
		return true
	default:
		return false
	}
}

func profileRelatedItemHubSupported(hubPath, attribute string) bool {
	base, qualifier, _ := strings.Cut(hubPath, ".")
	if base == "Extra" {
		return qualifier != ""
	}
	if attribute == "title" && base == "Relations" {
		return true
	}
	if base != "Publication" {
		return false
	}
	switch attribute {
	case "title":
		return qualifier == "Title"
	case "identifier":
		return qualifier == "Issn" || qualifier == "LIssn"
	default:
		return false
	}
}

func isWorkbenchTransportField(name string) bool {
	switch drupalBaseField(name) {
	case "id", "parent_id", "field_weight", "node_id", "file", "supplemental_file", "title",
		"unpublished_supplemental_file", "published", "url_alias", "langcode":
		return true
	default:
		return false
	}
}
