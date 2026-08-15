package profile

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/model"
)

const archivesSpaceSystem = "archivesspace"

// ResolvedField is a selector joined to immutable model metadata. Values
// returned by Compiled methods are defensive copies.
type ResolvedField struct {
	Selector           FieldSelector    `json:"selector" yaml:"selector"`
	Label              string           `json:"label,omitempty" yaml:"label,omitempty"`
	Description        string           `json:"description,omitempty" yaml:"description,omitempty"`
	SourceType         string           `json:"source_type" yaml:"source_type"`
	Kind               model.ValueKind  `json:"kind" yaml:"kind"`
	Cardinality        int              `json:"cardinality" yaml:"cardinality"`
	Required           bool             `json:"required,omitempty" yaml:"required,omitempty"`
	SemanticProperties []string         `json:"semantic_properties,omitempty" yaml:"semantic_properties,omitempty"`
	SemanticSettings   map[string]any   `json:"semantic_settings,omitempty" yaml:"semantic_settings,omitempty"`
	Reference          *model.Reference `json:"reference,omitempty" yaml:"reference,omitempty"`
	StorageSettings    map[string]any   `json:"storage_settings,omitempty" yaml:"storage_settings,omitempty"`
	InstanceSettings   map[string]any   `json:"instance_settings,omitempty" yaml:"instance_settings,omitempty"`
}

// CompiledMapping is one mapping in deterministic definition order.
type CompiledMapping struct {
	Position int           `json:"position" yaml:"position"`
	Field    ResolvedField `json:"field" yaml:"field"`
	Hub      string        `json:"hub" yaml:"hub"`
	Decode   string        `json:"decode" yaml:"decode"`
	Encode   string        `json:"encode" yaml:"encode"`
	Merge    MergePolicy   `json:"merge" yaml:"merge"`
}

// CompiledLookupRule is a validated, ordered set of repository fields and
// named value variants suitable for a system-specific Finder.
type CompiledLookupRule struct {
	Fields   []ResolvedField `json:"fields,omitempty" yaml:"fields,omitempty"`
	Operator LookupOperator  `json:"operator,omitempty" yaml:"operator,omitempty"`
	Variants []string        `json:"variants,omitempty" yaml:"variants,omitempty"`
}

// CompiledIdentifierRule is one validated identifier classification and
// lookup rule in deterministic policy order.
type CompiledIdentifierRule struct {
	Position      int                     `json:"position" yaml:"position"`
	Name          string                  `json:"name" yaml:"name"`
	Scheme        string                  `json:"scheme" yaml:"scheme"`
	IdentityLevel IdentifierIdentityLevel `json:"identity_level" yaml:"identity_level"`
	Value         ResolvedField           `json:"value" yaml:"value"`
	Pattern       string                  `json:"pattern" yaml:"pattern"`
	Canonicalizer string                  `json:"canonicalizer" yaml:"canonicalizer"`
	Strength      IdentifierStrength      `json:"strength" yaml:"strength"`
	Scope         IdentifierScope         `json:"scope" yaml:"scope"`
	Namespace     string                  `json:"namespace_uri,omitempty" yaml:"namespace_uri,omitempty"`
	Lookup        CompiledLookupRule      `json:"lookup,omitempty" yaml:"lookup,omitempty"`
}

// CompiledMetadataIdentity identifies fields used for conservative fallback
// comparison and the bounded ordered searches used to retrieve candidates.
type CompiledMetadataIdentity struct {
	Title        *ResolvedField           `json:"title,omitempty" yaml:"title,omitempty"`
	Contributors []ResolvedField          `json:"contributors,omitempty" yaml:"contributors,omitempty"`
	Date         *ResolvedField           `json:"date,omitempty" yaml:"date,omitempty"`
	Lookups      []CompiledMetadataLookup `json:"lookups,omitempty" yaml:"lookups,omitempty"`
	MetadataOnly MetadataOnlyPolicy       `json:"metadata_only" yaml:"metadata_only"`
}

// CompiledMetadataLookup is one ordered, resolved candidate search. Non-nil
// conditions are combined with AND by a Finder.
type CompiledMetadataLookup struct {
	Position     int                 `json:"position" yaml:"position"`
	Name         string              `json:"name" yaml:"name"`
	Title        CompiledLookupRule  `json:"title" yaml:"title"`
	Contributors *CompiledLookupRule `json:"contributors,omitempty" yaml:"contributors,omitempty"`
	Date         *CompiledLookupRule `json:"date,omitempty" yaml:"date,omitempty"`
}

// LookupPlan is the transport-independent input to a Finder. Endpoint,
// authentication, pagination, and other operations remain sitectl concerns.
type LookupPlan struct {
	Enabled            bool                      `json:"enabled" yaml:"enabled"`
	System             string                    `json:"system" yaml:"system"`
	ProfileFingerprint string                    `json:"profile_fingerprint" yaml:"profile_fingerprint"`
	ModelFingerprint   string                    `json:"model_fingerprint" yaml:"model_fingerprint"`
	Repository         EntitySelector            `json:"repository" yaml:"repository"`
	Identifiers        []CompiledIdentifierRule  `json:"identifiers,omitempty" yaml:"identifiers,omitempty"`
	Metadata           *CompiledMetadataIdentity `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// Compiled is an immutable, model-resolved profile plan. Its methods never
// return references to internal slices, maps, or pointers.
type Compiled struct {
	definition         Definition
	mappings           []CompiledMapping
	lookup             LookupPlan
	patterns           map[string]*regexp.Regexp
	identifierRegistry *hub.IdentifierRegistry
}

// Compile binds a strict definition to the exact model fingerprint it names.
// It performs no network or filesystem access.
func Compile(snapshot *model.Snapshot, definition *Definition) (*Compiled, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("compiling profile: model snapshot is nil")
	}
	if definition == nil {
		return nil, fmt.Errorf("compiling profile: definition is nil")
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("compiling profile: %w", err)
	}
	if err := definition.Validate(); err != nil {
		return nil, fmt.Errorf("compiling profile: %w", err)
	}
	if definition.System != snapshot.System {
		return nil, fmt.Errorf("compiling profile: system %q does not match model system %q", definition.System, snapshot.System)
	}
	if definition.ModelFingerprint != snapshot.Fingerprint.Value {
		return nil, fmt.Errorf("compiling profile: model fingerprint %s does not match snapshot %s", definition.ModelFingerprint, snapshot.Fingerprint.Value)
	}

	compiled := &Compiled{
		definition: cloneDefinition(*definition),
		mappings:   make([]CompiledMapping, 0, len(definition.Mappings)),
		patterns:   make(map[string]*regexp.Regexp),
	}
	for index, mapping := range definition.Mappings {
		resolved, err := resolveField(snapshot, mapping.Field)
		if err != nil {
			return nil, fmt.Errorf("compiling profile mapping %d: %w", index+1, err)
		}
		if err := validateExecutableMapping(definition.System, resolved, mapping); err != nil {
			return nil, fmt.Errorf("compiling profile mapping %d: %w", index+1, err)
		}
		compiled.mappings = append(compiled.mappings, CompiledMapping{
			Position: index, Field: resolved, Hub: mapping.Hub,
			Decode: mapping.Decode, Encode: mapping.Encode, Merge: mapping.Merge,
		})
	}

	lookup, patterns, err := compileIdentity(snapshot, definition)
	if err != nil {
		return nil, err
	}
	if err := validateIdentifierSelectorBindings(compiled.mappings, lookup.Identifiers); err != nil {
		return nil, fmt.Errorf("compiling profile identity: %w", err)
	}
	identifierRegistry, err := compileIdentifierRegistry(lookup.Identifiers)
	if err != nil {
		return nil, fmt.Errorf("compiling profile identity registry: %w", err)
	}
	for index := range lookup.Identifiers {
		registryRule, exists := identifierRegistry.Rule(lookup.Identifiers[index].Scheme)
		if !exists {
			return nil, fmt.Errorf("compiling profile identity registry: identifier scheme %q is unavailable", lookup.Identifiers[index].Scheme)
		}
		lookup.Identifiers[index].Namespace = registryRule.NamespaceURI
	}
	compiled.lookup = lookup
	compiled.patterns = patterns
	compiled.identifierRegistry = identifierRegistry
	return compiled, nil
}

// validateIdentifierSelectorBindings keeps the executable identifier codec and
// the identity policy on the same exact model selectors. Several identity rules
// may intentionally classify one stored value (for example work and version
// DOI rules), so this compares selector sets rather than rule counts.
func validateIdentifierSelectorBindings(mappings []CompiledMapping, identifiers []CompiledIdentifierRule) error {
	typedSelectors := make(map[string]struct{})
	for _, mapping := range mappings {
		if mapping.Decode != "typed-identifier" && mapping.Encode != "typed-identifier" {
			continue
		}
		typedSelectors[selectorKey(mapping.Field.Selector)] = struct{}{}
	}

	identitySelectors := make(map[string]struct{})
	for _, rule := range identifiers {
		identitySelectors[selectorKey(rule.Value.Selector)] = struct{}{}
	}

	for _, mapping := range mappings {
		if mapping.Decode != "typed-identifier" && mapping.Encode != "typed-identifier" {
			continue
		}
		if _, exists := identitySelectors[selectorKey(mapping.Field.Selector)]; !exists {
			return fmt.Errorf("typed-identifier mapping %d has no identity rule for its exact selector", mapping.Position+1)
		}
	}
	for _, rule := range identifiers {
		if _, exists := typedSelectors[selectorKey(rule.Value.Selector)]; !exists {
			return fmt.Errorf("identifier rule %q has no typed-identifier mapping for its exact selector", rule.Name)
		}
	}
	return nil
}

type hubPathShape uint8

const (
	hubPathScalar hubPathShape = iota + 1
	hubPathRepeated
	// hubPathCompatibilityRepeated is ordered repeated storage paired with a
	// legacy scalar primary. Unlike an ordinary repeated path, it accepts
	// replace so profiles that targeted the scalar field before the repeated
	// companion existed remain valid; it also accepts append for the complete
	// list.
	hubPathCompatibilityRepeated
	hubPathDynamic
)

var canonicalHubPathShapes = map[string]hubPathShape{
	"Title": hubPathScalar, "FullTitle": hubPathScalar,
	"Abstract": hubPathScalar, "Description": hubPathScalar,
	"TableOfContents": hubPathScalar, "Source": hubPathScalar,
	"DigitalOrigin": hubPathScalar,
	"Version":       hubPathScalar, "PreferredCitation": hubPathScalar,
	"ObjectModel": hubPathScalar, "AddCoverpage": hubPathScalar,
	"CaptureDevice": hubPathScalar, "PPI": hubPathScalar,
	"PageCount": hubPathScalar, "Dimensions": hubPathScalar,
	"Duration": hubPathScalar, "LocalRestriction": hubPathScalar,
	"AccessCondition": hubPathScalar, "IsPublic": hubPathScalar,
	"ResourceType": hubPathScalar, "Publication": hubPathScalar,
	"DegreeInfo": hubPathScalar, "ArchivalLocation": hubPathScalar,
	"Geographic": hubPathScalar,

	"AltTitle": hubPathRepeated, "Contributors": hubPathRepeated,
	"Departments": hubPathRepeated, "Dates": hubPathRepeated,
	"Genre": hubPathRepeated, "Genres": hubPathRepeated,
	"Subjects": hubPathRepeated, "Rights": hubPathRepeated,
	"Identifiers": hubPathRepeated, "Relations": hubPathRepeated,
	"Notes": hubPathRepeated, "Files": hubPathRepeated,
	"PhysicalForm": hubPathRepeated, "Funders": hubPathRepeated,
	"Publisher": hubPathCompatibilityRepeated, "PlacePublished": hubPathCompatibilityRepeated,
	"PhysicalDesc": hubPathCompatibilityRepeated, "Edition": hubPathCompatibilityRepeated,
	"Language": hubPathCompatibilityRepeated,

	"Extra": hubPathDynamic,
}

func validateExecutableMapping(system string, field ResolvedField, mapping Mapping) error {
	base, qualifier, _ := strings.Cut(mapping.Hub, ".")
	shape, exists := canonicalHubPathShapes[base]
	if !exists {
		return fmt.Errorf("hub path %q has no canonical Record target", mapping.Hub)
	}
	if base == "Extra" && strings.TrimSpace(qualifier) == "" {
		return fmt.Errorf("hub path Extra requires a machine-name key")
	}
	if mapping.Decode == "none" && mapping.Encode == "none" {
		return fmt.Errorf("mapping disables both decode and encode")
	}
	if mapping.Merge == MergeAppend && shape == hubPathScalar {
		return fmt.Errorf("append merge is incompatible with scalar Hub path %q", mapping.Hub)
	}
	if mapping.Merge == MergeReplace && shape == hubPathRepeated {
		return fmt.Errorf("replace merge is incompatible with repeated Hub path %q", mapping.Hub)
	}
	for _, direction := range []struct {
		name  string
		codec string
	}{{name: "decode", codec: mapping.Decode}, {name: "encode", codec: mapping.Encode}} {
		if direction.codec == "none" {
			continue
		}
		if err := validateCodecTarget(direction.codec, base); err != nil {
			return fmt.Errorf("%s codec %q: %w", direction.name, direction.codec, err)
		}
		if err := validateCodecSource(direction.codec, field.Kind); err != nil {
			return fmt.Errorf("%s codec %q for model kind %q: %w", direction.name, direction.codec, field.Kind, err)
		}
	}
	if system == archivesSpaceSystem {
		return fmt.Errorf("ArchivesSpace profiles are not executable; use its API adapter without a system profile")
	}
	return nil
}

func validateCodecTarget(codec, base string) error {
	var allowed bool
	switch codec {
	case "typed-identifier":
		allowed = base == "Identifiers" || base == "Extra"
	case "date":
		allowed = base == "Dates" || base == "Extra"
	case "file":
		allowed = base == "Files" || base == "Extra"
	case "typed-relation":
		allowed = base == "Contributors" || base == "Relations" || base == "Extra"
	case "boolean":
		allowed = base == "IsPublic" || base == "AddCoverpage" || base == "LocalRestriction" || base == "Extra"
	case "integer":
		allowed = base == "PPI" || base == "PageCount" || base == "Extra"
	default:
		allowed = true
	}
	if !allowed {
		return fmt.Errorf("is incompatible with Hub path base %q", base)
	}
	return nil
}

func validateCodecSource(codec string, kind model.ValueKind) error {
	allowed := func(values ...model.ValueKind) bool {
		for _, value := range values {
			if kind == value {
				return true
			}
		}
		return false
	}
	valid := true
	switch codec {
	case "typed-identifier":
		valid = allowed(model.ValueText, model.ValueLink, model.ValueComposite, model.ValueTypedReference, model.ValueOpaque)
	case "date":
		valid = allowed(model.ValueDate, model.ValueText, model.ValueComposite, model.ValueOpaque)
	case "file":
		valid = allowed(model.ValueFile, model.ValueComposite, model.ValueOpaque)
	case "typed-relation":
		valid = allowed(model.ValueTypedReference, model.ValueReference, model.ValueComposite, model.ValueOpaque)
	case "reference":
		valid = allowed(model.ValueReference, model.ValueTypedReference, model.ValueLink, model.ValueComposite, model.ValueText, model.ValueOpaque)
	case "link":
		valid = allowed(model.ValueLink, model.ValueText, model.ValueComposite, model.ValueOpaque)
	case "integer":
		valid = allowed(model.ValueInteger, model.ValueText, model.ValueOpaque)
	case "decimal":
		valid = allowed(model.ValueDecimal, model.ValueInteger, model.ValueText, model.ValueOpaque)
	case "boolean":
		valid = allowed(model.ValueBoolean, model.ValueInteger, model.ValueText, model.ValueOpaque)
	}
	if !valid {
		return fmt.Errorf("cannot consume that model value kind")
	}
	return nil
}

// Name returns the immutable profile name.
func (c *Compiled) Name() string {
	if c == nil {
		return ""
	}
	return c.definition.Name
}

// System returns the modeled system name.
func (c *Compiled) System() string {
	if c == nil {
		return ""
	}
	return c.definition.System
}

// Fingerprint returns the compiled profile fingerprint.
func (c *Compiled) Fingerprint() string {
	if c == nil {
		return ""
	}
	return c.definition.Fingerprint.Value
}

// ModelFingerprint returns the exact model digest bound to this plan.
func (c *Compiled) ModelFingerprint() string {
	if c == nil {
		return ""
	}
	return c.definition.ModelFingerprint
}

// Definition returns a defensive copy of the source definition.
func (c *Compiled) Definition() Definition {
	if c == nil {
		return Definition{}
	}
	return cloneDefinition(c.definition)
}

// Mappings returns deterministic ordered mappings as defensive copies.
func (c *Compiled) Mappings() []CompiledMapping {
	if c == nil {
		return nil
	}
	return cloneCompiledMappings(c.mappings)
}

// LookupPlan returns the complete Finder input as a defensive copy.
func (c *Compiled) LookupPlan() LookupPlan {
	if c == nil {
		return LookupPlan{}
	}
	return cloneLookupPlan(c.lookup)
}

// IdentifierRules returns rules for a Hub identifier scheme in deterministic
// policy order. Callers apply each named canonicalizer before MatchesIdentifier.
func (c *Compiled) IdentifierRules(scheme string) []CompiledIdentifierRule {
	if c == nil {
		return nil
	}
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	result := make([]CompiledIdentifierRule, 0)
	for _, rule := range c.lookup.Identifiers {
		if rule.Scheme == scheme {
			result = append(result, cloneCompiledIdentifier(rule))
		}
	}
	return result
}

// MatchesIdentifier applies a rule's precompiled validation pattern to an
// already canonicalized value. Unknown rule names are reported as errors.
func (c *Compiled) MatchesIdentifier(ruleName, canonicalValue string) (bool, error) {
	if c == nil {
		return false, fmt.Errorf("compiled profile is nil")
	}
	pattern, exists := c.patterns[ruleName]
	if !exists {
		return false, fmt.Errorf("identifier rule %q is not defined", ruleName)
	}
	return pattern.MatchString(canonicalValue), nil
}

// IdentifierRegistry returns the immutable effective registry used to parse
// and reconcile identifiers under this profile.
func (c *Compiled) IdentifierRegistry() *hub.IdentifierRegistry {
	if c == nil || c.identifierRegistry == nil {
		return hub.DefaultIdentifierRegistry()
	}
	return c.identifierRegistry
}

// IdentifierRegistryConfig returns a defensive, serializable copy suitable
// for constructing the reconciliation policy used with this profile.
func (c *Compiled) IdentifierRegistryConfig() hub.IdentifierRegistryConfig {
	return c.IdentifierRegistry().Configuration()
}

// NewIdentifier canonicalizes a source value according to one named profile
// rule and returns a fully authority-scoped Hub identifier.
func (c *Compiled) NewIdentifier(ruleName, value string) (*hubv1.Identifier, error) {
	if c == nil {
		return nil, fmt.Errorf("compiled profile is nil")
	}
	var selected *CompiledIdentifierRule
	for _, rule := range c.lookup.Identifiers {
		if rule.Name == ruleName {
			copy := rule
			selected = &copy
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("identifier rule %q is not defined", ruleName)
	}
	identifier, err := c.IdentifierRegistry().NewIdentifierForScheme(value, selected.Scheme, hubIdentityLevel(selected.IdentityLevel))
	if err != nil {
		return nil, fmt.Errorf("identifier rule %q: %w", ruleName, err)
	}
	if selected.Namespace != "" && identifier.GetNamespaceUri() != selected.Namespace {
		return nil, fmt.Errorf("identifier rule %q resolved namespace %q, want %q", ruleName, identifier.GetNamespaceUri(), selected.Namespace)
	}
	matched, err := c.MatchesIdentifier(ruleName, identifier.GetValue())
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, fmt.Errorf("identifier value does not match rule %q", ruleName)
	}
	return identifier, nil
}

func compileIdentity(snapshot *model.Snapshot, definition *Definition) (LookupPlan, map[string]*regexp.Regexp, error) {
	plan := LookupPlan{
		System: definition.System, ProfileFingerprint: definition.Fingerprint.Value,
		ModelFingerprint: definition.ModelFingerprint,
	}
	patterns := make(map[string]*regexp.Regexp)
	if definition.Identity == nil {
		return plan, patterns, nil
	}
	identity := definition.Identity
	if _, exists := snapshot.Entity(identity.Repository.EntityType, identity.Repository.Bundle); !exists {
		return LookupPlan{}, nil, fmt.Errorf("compiling profile identity: repository entity %s/%s is not in the model", identity.Repository.EntityType, identity.Repository.Bundle)
	}
	plan.Repository = identity.Repository
	plan.Identifiers = make([]CompiledIdentifierRule, 0, len(identity.Identifiers))
	for index, rule := range identity.Identifiers {
		if err := selectorBelongsTo(rule.Value, identity.Repository); err != nil {
			return LookupPlan{}, nil, fmt.Errorf("compiling profile identifier %q value: %w", rule.Name, err)
		}
		value, err := resolveField(snapshot, rule.Value)
		if err != nil {
			return LookupPlan{}, nil, fmt.Errorf("compiling profile identifier %q value: %w", rule.Name, err)
		}
		lookup, err := compileLookup(snapshot, identity.Repository, rule.Lookup)
		if err != nil {
			return LookupPlan{}, nil, fmt.Errorf("compiling profile identifier %q lookup: %w", rule.Name, err)
		}
		pattern, err := regexp.Compile("^(?:" + rule.Pattern + ")$")
		if err != nil {
			return LookupPlan{}, nil, fmt.Errorf("compiling profile identifier %q pattern: %w", rule.Name, err)
		}
		patterns[rule.Name] = pattern
		plan.Identifiers = append(plan.Identifiers, CompiledIdentifierRule{
			Position: index, Name: rule.Name, Scheme: rule.Scheme, Value: value,
			IdentityLevel: rule.IdentityLevel, Pattern: rule.Pattern, Canonicalizer: rule.Canonicalizer,
			Strength: rule.Strength, Scope: rule.Scope, Namespace: rule.Namespace,
			Lookup: lookup,
		})
		plan.Enabled = plan.Enabled || len(lookup.Fields) != 0
	}
	if identity.Metadata != nil {
		metadata := &CompiledMetadataIdentity{MetadataOnly: identity.Metadata.MetadataOnly}
		var err error
		metadata.Title, err = resolveOptionalField(snapshot, identity.Repository, identity.Metadata.Title)
		if err != nil {
			return LookupPlan{}, nil, fmt.Errorf("compiling profile metadata title: %w", err)
		}
		metadata.Contributors = make([]ResolvedField, 0, len(identity.Metadata.Contributors))
		for index, selector := range identity.Metadata.Contributors {
			if err := selectorBelongsTo(selector, identity.Repository); err != nil {
				return LookupPlan{}, nil, fmt.Errorf("compiling profile metadata contributor %d: %w", index+1, err)
			}
			field, err := resolveField(snapshot, selector)
			if err != nil {
				return LookupPlan{}, nil, fmt.Errorf("compiling profile metadata contributor %d: %w", index+1, err)
			}
			metadata.Contributors = append(metadata.Contributors, field)
		}
		metadata.Date, err = resolveOptionalField(snapshot, identity.Repository, identity.Metadata.Date)
		if err != nil {
			return LookupPlan{}, nil, fmt.Errorf("compiling profile metadata date: %w", err)
		}
		metadata.Lookups = make([]CompiledMetadataLookup, 0, len(identity.Metadata.Lookups))
		for index, lookup := range identity.Metadata.Lookups {
			compiledLookup := CompiledMetadataLookup{Position: index, Name: lookup.Name}
			compiledLookup.Title, err = compileLookup(snapshot, identity.Repository, lookup.Title)
			if err != nil {
				return LookupPlan{}, nil, fmt.Errorf("compiling profile metadata lookup %q title: %w", lookup.Name, err)
			}
			if lookup.Contributors != nil {
				contributors, compileErr := compileLookup(snapshot, identity.Repository, *lookup.Contributors)
				if compileErr != nil {
					return LookupPlan{}, nil, fmt.Errorf("compiling profile metadata lookup %q contributors: %w", lookup.Name, compileErr)
				}
				compiledLookup.Contributors = &contributors
			}
			if lookup.Date != nil {
				date, compileErr := compileLookup(snapshot, identity.Repository, *lookup.Date)
				if compileErr != nil {
					return LookupPlan{}, nil, fmt.Errorf("compiling profile metadata lookup %q date: %w", lookup.Name, compileErr)
				}
				compiledLookup.Date = &date
			}
			metadata.Lookups = append(metadata.Lookups, compiledLookup)
		}
		plan.Enabled = plan.Enabled || len(metadata.Lookups) != 0
		plan.Metadata = metadata
	}
	return plan, patterns, nil
}

func compileIdentifierRegistry(rules []CompiledIdentifierRule) (*hub.IdentifierRegistry, error) {
	config := hub.IdentifierRegistryConfig{Version: hub.IdentifierRegistryVersion}
	type builtInPolicy struct {
		scheme    string
		namespace string
		levels    map[hubv1.IdentifierIdentityLevel]struct{}
	}
	// A compiled profile is the authority for duplicate identity. Start every
	// built-in exact scheme disabled, then enable only the identity levels that
	// this profile explicitly marks strong. Without these empty overrides, a
	// DOI (or another globally known scheme) omitted from an institutional
	// profile would silently retain the process-wide default exact policy.
	builtInOrder := make([]string, 0)
	builtInPolicies := make(map[string]*builtInPolicy)
	for _, rule := range hub.DefaultIdentifierRegistry().Rules() {
		if rule.NamespaceURI == "" || len(rule.ExactIdentityLevels) == 0 {
			continue
		}
		builtInPolicies[rule.Scheme] = &builtInPolicy{
			scheme: rule.Scheme, namespace: rule.NamespaceURI,
			levels: make(map[hubv1.IdentifierIdentityLevel]struct{}),
		}
		builtInOrder = append(builtInOrder, rule.Scheme)
	}
	customOrder := make([]string, 0)
	customRules := make(map[string]*hub.IdentifierRule)
	for _, rule := range rules {
		level := hubIdentityLevel(rule.IdentityLevel)
		builtIn, exists := hub.DefaultIdentifierRegistry().Rule(rule.Scheme)
		if exists {
			if rule.Namespace != "" && rule.Namespace != builtIn.NamespaceURI {
				return nil, fmt.Errorf("identifier rule %q namespace_uri %q does not match registered scheme authority %q", rule.Name, rule.Namespace, builtIn.NamespaceURI)
			}
			if rule.Strength == IdentifierStrong && builtIn.NamespaceURI == "" {
				return nil, fmt.Errorf("identifier rule %q uses unscoped built-in scheme %q; define a distinct institution scheme with namespace_uri", rule.Name, rule.Scheme)
			}
			if builtIn.NamespaceURI != "" {
				policy := builtInPolicies[builtIn.Scheme]
				if policy == nil {
					policy = &builtInPolicy{scheme: builtIn.Scheme, namespace: builtIn.NamespaceURI, levels: make(map[hubv1.IdentifierIdentityLevel]struct{})}
					builtInPolicies[builtIn.Scheme] = policy
					builtInOrder = append(builtInOrder, builtIn.Scheme)
				}
				if rule.Strength == IdentifierStrong {
					policy.levels[level] = struct{}{}
				}
			}
			continue
		}

		if rule.Namespace == "" {
			return nil, fmt.Errorf("custom identifier rule %q requires namespace_uri", rule.Name)
		}
		identifierCase, err := identifierCaseForCanonicalizer(rule.Canonicalizer)
		if err != nil {
			return nil, fmt.Errorf("identifier rule %q: %w", rule.Name, err)
		}
		registryRule := customRules[rule.Scheme]
		if registryRule == nil {
			registryRule = &hub.IdentifierRule{
				Scheme: rule.Scheme, NamespaceURI: rule.Namespace,
				Type:                 hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
				DefaultIdentityLevel: level, Pattern: rule.Pattern, Case: identifierCase,
			}
			customRules[rule.Scheme] = registryRule
			customOrder = append(customOrder, rule.Scheme)
		} else if registryRule.NamespaceURI != rule.Namespace || registryRule.Pattern != rule.Pattern || registryRule.Case != identifierCase {
			return nil, fmt.Errorf("custom identifier scheme %q has inconsistent namespace_uri, pattern, or canonicalizer across rules", rule.Scheme)
		}
		if rule.Strength == IdentifierStrong {
			registryRule.ExactIdentityLevels = appendUniqueIdentityLevel(registryRule.ExactIdentityLevels, level)
		}
	}
	sort.Strings(builtInOrder)
	for _, scheme := range builtInOrder {
		policy := builtInPolicies[scheme]
		levels := make([]hubv1.IdentifierIdentityLevel, 0, len(policy.levels))
		for level := range policy.levels {
			levels = append(levels, level)
		}
		sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })
		config.ExactPolicies = append(config.ExactPolicies, hub.IdentifierExactIdentityPolicy{
			Scheme: policy.scheme, NamespaceURI: policy.namespace, IdentityLevels: levels,
		})
	}
	for _, scheme := range customOrder {
		config.Rules = append(config.Rules, *customRules[scheme])
	}
	return hub.NewIdentifierRegistry(config)
}

func appendUniqueIdentityLevel(values []hubv1.IdentifierIdentityLevel, value hubv1.IdentifierIdentityLevel) []hubv1.IdentifierIdentityLevel {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func identifierCaseForCanonicalizer(name string) (hub.IdentifierCase, error) {
	switch name {
	case "canonical", "trim":
		return hub.IdentifierCasePreserve, nil
	case "lower":
		return hub.IdentifierCaseLower, nil
	case "upper":
		return hub.IdentifierCaseUpper, nil
	default:
		return "", fmt.Errorf("custom scheme canonicalizer %q is unsupported; use canonical, trim, lower, or upper", name)
	}
}

func hubIdentityLevel(level IdentifierIdentityLevel) hubv1.IdentifierIdentityLevel {
	switch level {
	case IdentityWork:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK
	case IdentityVersion:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION
	case IdentityManifestation:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_MANIFESTATION
	case IdentityConcept:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT
	case IdentitySourceRecord:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD
	default:
		return hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED
	}
}

func compileLookup(snapshot *model.Snapshot, repository EntitySelector, lookup LookupRule) (CompiledLookupRule, error) {
	result := CompiledLookupRule{
		Fields:   make([]ResolvedField, 0, len(lookup.Fields)),
		Operator: lookup.Operator, Variants: append([]string(nil), lookup.Variants...),
	}
	for index, selector := range lookup.Fields {
		if err := selectorBelongsTo(selector, repository); err != nil {
			return CompiledLookupRule{}, fmt.Errorf("field %d: %w", index+1, err)
		}
		field, err := resolveField(snapshot, selector)
		if err != nil {
			return CompiledLookupRule{}, fmt.Errorf("field %d: %w", index+1, err)
		}
		result.Fields = append(result.Fields, field)
	}
	return result, nil
}

func resolveOptionalField(snapshot *model.Snapshot, repository EntitySelector, selector *FieldSelector) (*ResolvedField, error) {
	if selector == nil {
		return nil, nil
	}
	if err := selectorBelongsTo(*selector, repository); err != nil {
		return nil, err
	}
	field, err := resolveField(snapshot, *selector)
	if err != nil {
		return nil, err
	}
	return &field, nil
}

func selectorBelongsTo(selector FieldSelector, repository EntitySelector) error {
	if selector.EntityType != repository.EntityType || selector.Bundle != repository.Bundle {
		return fmt.Errorf("selector entity %s/%s does not match repository %s/%s", selector.EntityType, selector.Bundle, repository.EntityType, repository.Bundle)
	}
	return nil
}

func resolveField(snapshot *model.Snapshot, selector FieldSelector) (ResolvedField, error) {
	field, exists := snapshot.Field(selector.EntityType, selector.Bundle, selector.Path)
	if !exists {
		return ResolvedField{}, fmt.Errorf("field %s/%s/%s is not in the model", selector.EntityType, selector.Bundle, selector.Path)
	}
	return ResolvedField{
		Selector: cloneSelector(selector), Label: field.Label, Description: field.Description,
		SourceType: field.SourceType, Kind: field.Kind,
		Cardinality: field.Cardinality, Required: field.Required,
		SemanticProperties: append([]string(nil), field.SemanticProperties...),
		SemanticSettings:   cloneCompiledMap(field.SemanticSettings),
		Reference:          cloneCompiledReference(field.Reference),
		StorageSettings:    cloneCompiledMap(field.StorageSettings),
		InstanceSettings:   cloneCompiledMap(field.InstanceSettings),
	}, nil
}

func cloneCompiledMappings(input []CompiledMapping) []CompiledMapping {
	result := make([]CompiledMapping, len(input))
	for index, mapping := range input {
		result[index] = mapping
		result[index].Field = cloneResolvedField(mapping.Field)
	}
	return result
}

func cloneLookupPlan(input LookupPlan) LookupPlan {
	result := input
	result.Identifiers = make([]CompiledIdentifierRule, len(input.Identifiers))
	for index, rule := range input.Identifiers {
		result.Identifiers[index] = cloneCompiledIdentifier(rule)
	}
	if input.Metadata != nil {
		metadata := *input.Metadata
		if input.Metadata.Title != nil {
			title := cloneResolvedField(*input.Metadata.Title)
			metadata.Title = &title
		}
		metadata.Contributors = make([]ResolvedField, len(input.Metadata.Contributors))
		for index, field := range input.Metadata.Contributors {
			metadata.Contributors[index] = cloneResolvedField(field)
		}
		if input.Metadata.Date != nil {
			date := cloneResolvedField(*input.Metadata.Date)
			metadata.Date = &date
		}
		metadata.Lookups = make([]CompiledMetadataLookup, len(input.Metadata.Lookups))
		for index, lookup := range input.Metadata.Lookups {
			metadata.Lookups[index] = cloneCompiledMetadataLookup(lookup)
		}
		result.Metadata = &metadata
	}
	return result
}

func cloneCompiledMetadataLookup(input CompiledMetadataLookup) CompiledMetadataLookup {
	result := input
	result.Title = cloneCompiledLookup(input.Title)
	if input.Contributors != nil {
		contributors := cloneCompiledLookup(*input.Contributors)
		result.Contributors = &contributors
	}
	if input.Date != nil {
		date := cloneCompiledLookup(*input.Date)
		result.Date = &date
	}
	return result
}

func cloneCompiledIdentifier(input CompiledIdentifierRule) CompiledIdentifierRule {
	result := input
	result.Value = cloneResolvedField(input.Value)
	result.Lookup = cloneCompiledLookup(input.Lookup)
	return result
}

func cloneCompiledLookup(input CompiledLookupRule) CompiledLookupRule {
	result := input
	result.Fields = make([]ResolvedField, len(input.Fields))
	for index, field := range input.Fields {
		result.Fields[index] = cloneResolvedField(field)
	}
	result.Variants = append([]string(nil), input.Variants...)
	return result
}

func cloneResolvedField(input ResolvedField) ResolvedField {
	result := input
	result.Selector = cloneSelector(input.Selector)
	result.SemanticProperties = append([]string(nil), input.SemanticProperties...)
	result.SemanticSettings = cloneCompiledMap(input.SemanticSettings)
	result.Reference = cloneCompiledReference(input.Reference)
	result.StorageSettings = cloneCompiledMap(input.StorageSettings)
	result.InstanceSettings = cloneCompiledMap(input.InstanceSettings)
	return result
}

func cloneCompiledReference(input *model.Reference) *model.Reference {
	if input == nil {
		return nil
	}
	result := *input
	result.Bundles = append([]string(nil), input.Bundles...)
	return &result
}

func cloneCompiledMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = cloneCompiledValue(value)
	}
	return result
}

func cloneCompiledValue(value any) any {
	if value == nil {
		return nil
	}
	return cloneCompiledReflectValue(reflect.ValueOf(value)).Interface()
}

func cloneCompiledReflectValue(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.New(value.Type()).Elem()
		result.Set(cloneCompiledReflectValue(value.Elem()))
		return result
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			result.SetMapIndex(iterator.Key(), cloneCompiledReflectValue(iterator.Value()))
		}
		return result
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(cloneCompiledReflectValue(value.Elem()))
		return result
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := range value.Len() {
			result.Index(index).Set(cloneCompiledReflectValue(value.Index(index)))
		}
		return result
	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		for index := range value.Len() {
			result.Index(index).Set(cloneCompiledReflectValue(value.Index(index)))
		}
		return result
	default:
		return value
	}
}
