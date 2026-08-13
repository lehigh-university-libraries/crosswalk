package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

const (
	// IdentifierRegistryVersion is the configuration contract understood by this release.
	IdentifierRegistryVersion = "1"

	// identifierPatternMatchContract participates in the registry digest so a
	// report cannot claim the same identifier policy after matching semantics
	// change. User patterns are always evaluated as whole values even when an
	// alternation would otherwise escape its visible ^ and $ anchors.
	identifierPatternMatchContract = "whole-value-v1"
)

var identifierSchemePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)

// IdentifierCase controls case normalization after wrappers and prefixes are removed.
type IdentifierCase string

const (
	IdentifierCasePreserve IdentifierCase = "preserve"
	IdentifierCaseLower    IdentifierCase = "lower"
	IdentifierCaseUpper    IdentifierCase = "upper"
)

// IdentifierRule defines one canonical identifier scheme. Institution-defined
// rules are deliberately declarative so the same behavior can be applied while
// parsing, validating, serializing, and reconciling records.
type IdentifierRule struct {
	Scheme                  string                          `json:"scheme" yaml:"scheme"`
	Aliases                 []string                        `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	NamespaceURI            string                          `json:"namespace_uri,omitempty" yaml:"namespace_uri,omitempty"`
	Type                    hubv1.IdentifierType            `json:"type,omitempty" yaml:"type,omitempty"`
	DefaultIdentityLevel    hubv1.IdentifierIdentityLevel   `json:"default_identity_level,omitempty" yaml:"default_identity_level,omitempty"`
	Pattern                 string                          `json:"pattern" yaml:"pattern"`
	Prefixes                []string                        `json:"prefixes,omitempty" yaml:"prefixes,omitempty"`
	Case                    IdentifierCase                  `json:"case,omitempty" yaml:"case,omitempty"`
	TrimTrailingPunctuation bool                            `json:"trim_trailing_punctuation,omitempty" yaml:"trim_trailing_punctuation,omitempty"`
	ExactIdentityLevels     []hubv1.IdentifierIdentityLevel `json:"exact_identity_levels,omitempty" yaml:"exact_identity_levels,omitempty"`
}

// IdentifierRegistryConfig selects a versioned registry and adds strict,
// institution-specific schemes. Built-in schemes cannot be replaced.
type IdentifierRegistryConfig struct {
	Version       string                          `json:"version" yaml:"version"`
	Rules         []IdentifierRule                `json:"rules,omitempty" yaml:"rules,omitempty"`
	ExactPolicies []IdentifierExactIdentityPolicy `json:"exact_policies,omitempty" yaml:"exact_policies,omitempty"`
}

// IdentifierExactIdentityPolicy explicitly replaces the identity levels that
// count as exact evidence for one already registered, authority-scoped scheme.
// For example, an institution may deliberately treat a Zenodo concept DOI as
// one repository work even though the conservative built-in policy does not.
type IdentifierExactIdentityPolicy struct {
	Scheme         string                          `json:"scheme" yaml:"scheme"`
	NamespaceURI   string                          `json:"namespace_uri" yaml:"namespace_uri"`
	IdentityLevels []hubv1.IdentifierIdentityLevel `json:"identity_levels" yaml:"identity_levels"`
}

// IdentifierRegistry canonicalizes and validates identifiers under one
// immutable, versioned collection of rules.
type IdentifierRegistry struct {
	version string
	rules   map[string]compiledIdentifierRule
	aliases map[string]string
	digest  string
	config  IdentifierRegistryConfig
}

type compiledIdentifierRule struct {
	IdentifierRule
	pattern   *regexp.Regexp
	normalize func(string) string
	builtIn   bool
}

var defaultIdentifiers = mustIdentifierRegistry(IdentifierRegistryConfig{Version: IdentifierRegistryVersion})

// NewIdentifierRegistry validates configuration and returns an immutable
// registry containing all built-in rules plus the configured institutional rules.
func NewIdentifierRegistry(config IdentifierRegistryConfig) (*IdentifierRegistry, error) {
	if config.Version == "" {
		config.Version = IdentifierRegistryVersion
	}
	if config.Version != IdentifierRegistryVersion {
		return nil, fmt.Errorf("unsupported identifier registry version %q", config.Version)
	}
	registry := &IdentifierRegistry{
		version: config.Version,
		rules:   make(map[string]compiledIdentifierRule),
		aliases: make(map[string]string),
		config:  cloneIdentifierRegistryConfig(config),
	}
	for _, rule := range builtInIdentifierRules() {
		if err := registry.addRule(rule, true); err != nil {
			return nil, fmt.Errorf("registering built-in identifier scheme %q: %w", rule.Scheme, err)
		}
	}
	for _, rule := range config.Rules {
		if err := registry.addRule(compiledIdentifierRule{IdentifierRule: rule}, false); err != nil {
			return nil, fmt.Errorf("registering identifier scheme %q: %w", rule.Scheme, err)
		}
	}
	for _, policy := range config.ExactPolicies {
		if err := registry.applyExactPolicy(policy); err != nil {
			return nil, fmt.Errorf("configuring exact identifier policy for %q: %w", policy.Scheme, err)
		}
	}
	digest, err := registry.computeDigest()
	if err != nil {
		return nil, err
	}
	registry.digest = digest
	return registry, nil
}

// DefaultIdentifierRegistry returns the immutable built-in registry.
func DefaultIdentifierRegistry() *IdentifierRegistry { return defaultIdentifiers }

// Version returns the registry configuration version.
func (registry *IdentifierRegistry) Version() string {
	if registry == nil {
		return IdentifierRegistryVersion
	}
	return registry.version
}

// Digest returns a deterministic SHA-256 digest of the effective rules.
func (registry *IdentifierRegistry) Digest() string {
	if registry == nil {
		return defaultIdentifiers.digest
	}
	return registry.digest
}

// Configuration returns a defensive copy of the caller-supplied registry
// policy. Built-in rules are implicit and are therefore not expanded into the
// returned configuration.
func (registry *IdentifierRegistry) Configuration() IdentifierRegistryConfig {
	registry = effectiveIdentifierRegistry(registry)
	return cloneIdentifierRegistryConfig(registry.config)
}

// Rule returns a copy of the canonical rule for scheme or one of its aliases.
func (registry *IdentifierRegistry) Rule(scheme string) (IdentifierRule, bool) {
	registry = effectiveIdentifierRegistry(registry)
	canonical, ok := registry.aliases[normalizeIdentifierScheme(scheme)]
	if !ok {
		return IdentifierRule{}, false
	}
	rule := registry.rules[canonical].IdentifierRule
	rule.Aliases = append([]string(nil), rule.Aliases...)
	rule.Prefixes = append([]string(nil), rule.Prefixes...)
	rule.ExactIdentityLevels = append([]hubv1.IdentifierIdentityLevel(nil), rule.ExactIdentityLevels...)
	return rule, true
}

// Rules returns defensive copies of every effective canonical rule in stable
// scheme order. It is primarily useful to derive a narrower policy without
// duplicating knowledge of the built-in registry.
func (registry *IdentifierRegistry) Rules() []IdentifierRule {
	registry = effectiveIdentifierRegistry(registry)
	result := make([]IdentifierRule, 0, len(registry.rules))
	for _, compiled := range registry.rules {
		rule := compiled.IdentifierRule
		rule.Aliases = append([]string(nil), rule.Aliases...)
		rule.Prefixes = append([]string(nil), rule.Prefixes...)
		rule.ExactIdentityLevels = append([]hubv1.IdentifierIdentityLevel(nil), rule.ExactIdentityLevels...)
		result = append(result, rule)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Scheme < result[j].Scheme })
	return result
}

// CanonicalizeIdentifier returns a validated canonical copy of identifier.
func (registry *IdentifierRegistry) CanonicalizeIdentifier(identifier *hubv1.Identifier) (*hubv1.Identifier, error) {
	if identifier == nil {
		return nil, fmt.Errorf("identifier is nil")
	}
	registry = effectiveIdentifierRegistry(registry)
	scheme := normalizeIdentifierScheme(identifier.GetScheme())
	if scheme == "" {
		scheme = schemeForType(identifier.GetType())
	}
	if scheme == "" || scheme == "unspecified" {
		scheme = registry.DetectScheme(identifier.GetValue())
	}
	canonicalScheme, ok := registry.aliases[scheme]
	if !ok {
		return nil, fmt.Errorf("identifier scheme %q is not registered", scheme)
	}
	rule := registry.rules[canonicalScheme]
	value := normalizeIdentifierValue(identifier.GetValue(), rule)
	if value == "" {
		return nil, fmt.Errorf("identifier value is required")
	}
	if len(value) > 2048 {
		return nil, fmt.Errorf("identifier value exceeds 2048 bytes")
	}
	if strings.IndexFunc(value, func(character rune) bool { return unicode.IsControl(character) }) >= 0 {
		return nil, fmt.Errorf("identifier value contains a control character")
	}
	if !rule.pattern.MatchString(value) {
		return nil, fmt.Errorf("identifier %q does not match scheme %q pattern", identifier.GetValue(), canonicalScheme)
	}
	namespace := strings.TrimSpace(identifier.GetNamespaceUri())
	if namespace == "" {
		namespace = rule.NamespaceURI
	} else {
		var err error
		namespace, err = canonicalNamespaceURI(namespace)
		if err != nil {
			return nil, err
		}
		if rule.NamespaceURI != "" && namespace != rule.NamespaceURI {
			return nil, fmt.Errorf("identifier namespace %q does not match scheme %q namespace %q", namespace, canonicalScheme, rule.NamespaceURI)
		}
	}
	identityLevel := identifier.GetIdentityLevel()
	if identityLevel == hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED {
		identityLevel = rule.DefaultIdentityLevel
	}
	if !validIdentifierIdentityLevel(identityLevel) {
		return nil, fmt.Errorf("identifier has unsupported identity level %q", identityLevel.String())
	}
	identifierType := rule.Type
	if identifierType == hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED {
		identifierType = identifier.GetType()
	}
	return &hubv1.Identifier{
		Type: identifierType, Value: value, Display: identifier.GetDisplay(),
		IsPreferred: identifier.GetIsPreferred(), Scheme: canonicalScheme,
		NamespaceUri: namespace, IdentityLevel: identityLevel,
	}, nil
}

// NewIdentifierForScheme constructs and canonicalizes an identifier using an
// explicit scheme. Custom schemes must first be registered.
func (registry *IdentifierRegistry) NewIdentifierForScheme(value, scheme string, level hubv1.IdentifierIdentityLevel) (*hubv1.Identifier, error) {
	return registry.CanonicalizeIdentifier(&hubv1.Identifier{Value: value, Scheme: scheme, IdentityLevel: level})
}

// DetectScheme returns the built-in scheme recognized from a value, or local
// when the value has no unambiguous globally recognizable form.
func (registry *IdentifierRegistry) DetectScheme(value string) string {
	registry = effectiveIdentifierRegistry(registry)
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	checks := []string{"doi", "arxiv", "handle", "orcid", "pmcid", "pmid", "wos", "scopus-eid", "scopus-id", "zenodo-record", "uuid", "isbn", "issn"}
	for _, scheme := range checks {
		rule := registry.rules[scheme]
		candidate := normalizeIdentifierValue(trimmed, rule)
		if candidate != "" && rule.pattern.MatchString(candidate) && identifierHasSignal(trimmed, lower, scheme) {
			return scheme
		}
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "url"
	}
	return "local"
}

// ExactIdentity reports whether identifier is valid, authority-scoped exact
// evidence under this registry, and identifies a configured identity level.
func (registry *IdentifierRegistry) ExactIdentity(identifier *hubv1.Identifier) bool {
	canonical, err := registry.CanonicalizeIdentifier(identifier)
	if err != nil || canonical.GetNamespaceUri() == "" {
		return false
	}
	rule, ok := effectiveIdentifierRegistry(registry).rules[canonical.GetScheme()]
	if !ok {
		return false
	}
	for _, level := range rule.ExactIdentityLevels {
		if level == canonical.GetIdentityLevel() {
			return true
		}
	}
	return false
}

func (registry *IdentifierRegistry) addRule(rule compiledIdentifierRule, builtIn bool) error {
	rule.Aliases = append([]string(nil), rule.Aliases...)
	rule.Prefixes = append([]string(nil), rule.Prefixes...)
	rule.ExactIdentityLevels = append([]hubv1.IdentifierIdentityLevel(nil), rule.ExactIdentityLevels...)
	rule.Scheme = normalizeIdentifierScheme(rule.Scheme)
	if !identifierSchemePattern.MatchString(rule.Scheme) {
		return fmt.Errorf("scheme must match %s", identifierSchemePattern)
	}
	if _, exists := registry.rules[rule.Scheme]; exists {
		return fmt.Errorf("scheme is already registered")
	}
	if rule.Pattern == "" || !strings.HasPrefix(rule.Pattern, "^") || !strings.HasSuffix(rule.Pattern, "$") {
		return fmt.Errorf("pattern must be nonempty and anchored with ^ and $")
	}
	pattern, err := regexp.Compile(`\A(?:` + rule.Pattern + `)\z`)
	if err != nil {
		return fmt.Errorf("compiling pattern: %w", err)
	}
	rule.pattern = pattern
	if rule.Case == "" {
		rule.Case = IdentifierCasePreserve
	}
	switch rule.Case {
	case IdentifierCasePreserve, IdentifierCaseLower, IdentifierCaseUpper:
	default:
		return fmt.Errorf("unsupported case normalization %q", rule.Case)
	}
	if rule.NamespaceURI != "" {
		rule.NamespaceURI, err = canonicalNamespaceURI(rule.NamespaceURI)
		if err != nil {
			return err
		}
	}
	if len(rule.ExactIdentityLevels) != 0 && rule.NamespaceURI == "" {
		return fmt.Errorf("exact identity levels require a namespace URI")
	}
	seenLevels := make(map[hubv1.IdentifierIdentityLevel]struct{}, len(rule.ExactIdentityLevels))
	for _, level := range rule.ExactIdentityLevels {
		if level == hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED || !validIdentifierIdentityLevel(level) {
			return fmt.Errorf("unsupported exact identity level %q", level.String())
		}
		if _, exists := seenLevels[level]; exists {
			return fmt.Errorf("duplicate exact identity level %q", level.String())
		}
		seenLevels[level] = struct{}{}
	}
	if rule.DefaultIdentityLevel != hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED && !validIdentifierIdentityLevel(rule.DefaultIdentityLevel) {
		return fmt.Errorf("unsupported default identity level %q", rule.DefaultIdentityLevel.String())
	}
	for index, prefix := range rule.Prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			return fmt.Errorf("prefix %d is empty", index)
		}
		rule.Prefixes[index] = prefix
	}
	sort.Slice(rule.Prefixes, func(i, j int) bool {
		if len(rule.Prefixes[i]) != len(rule.Prefixes[j]) {
			return len(rule.Prefixes[i]) > len(rule.Prefixes[j])
		}
		return rule.Prefixes[i] < rule.Prefixes[j]
	})
	for index := 1; index < len(rule.Prefixes); index++ {
		if strings.EqualFold(rule.Prefixes[index-1], rule.Prefixes[index]) {
			return fmt.Errorf("duplicate prefix %q", rule.Prefixes[index])
		}
	}
	sort.Slice(rule.ExactIdentityLevels, func(i, j int) bool {
		return rule.ExactIdentityLevels[i] < rule.ExactIdentityLevels[j]
	})
	aliases := make([]string, len(rule.Aliases))
	for index, alias := range rule.Aliases {
		aliases[index] = normalizeIdentifierScheme(alias)
	}
	sort.Strings(aliases)
	for _, alias := range append([]string{rule.Scheme}, aliases...) {
		if !identifierSchemePattern.MatchString(alias) {
			return fmt.Errorf("alias %q is invalid", alias)
		}
		if owner, exists := registry.aliases[alias]; exists {
			return fmt.Errorf("alias %q is already registered by %q", alias, owner)
		}
		registry.aliases[alias] = rule.Scheme
	}
	rule.Aliases = aliases
	rule.builtIn = builtIn
	registry.rules[rule.Scheme] = rule
	return nil
}

func (registry *IdentifierRegistry) applyExactPolicy(policy IdentifierExactIdentityPolicy) error {
	scheme := normalizeIdentifierScheme(policy.Scheme)
	canonical, exists := registry.aliases[scheme]
	if !exists {
		return fmt.Errorf("scheme is not registered")
	}
	rule := registry.rules[canonical]
	namespace, err := canonicalNamespaceURI(policy.NamespaceURI)
	if err != nil {
		return err
	}
	if rule.NamespaceURI == "" || namespace != rule.NamespaceURI {
		return fmt.Errorf("namespace %q does not match registered namespace %q", namespace, rule.NamespaceURI)
	}
	levels := append([]hubv1.IdentifierIdentityLevel(nil), policy.IdentityLevels...)
	seen := make(map[hubv1.IdentifierIdentityLevel]struct{}, len(levels))
	for _, level := range levels {
		if level == hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED || !validIdentifierIdentityLevel(level) {
			return fmt.Errorf("unsupported exact identity level %q", level.String())
		}
		if _, duplicate := seen[level]; duplicate {
			return fmt.Errorf("duplicate exact identity level %q", level.String())
		}
		seen[level] = struct{}{}
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })
	rule.ExactIdentityLevels = levels
	registry.rules[canonical] = rule
	return nil
}

func (registry *IdentifierRegistry) computeDigest() (string, error) {
	rules := make([]IdentifierRule, 0, len(registry.rules))
	for _, compiled := range registry.rules {
		rule := compiled.IdentifierRule
		rule.Aliases = append([]string(nil), rule.Aliases...)
		rule.Prefixes = append([]string(nil), rule.Prefixes...)
		rule.ExactIdentityLevels = append([]hubv1.IdentifierIdentityLevel(nil), rule.ExactIdentityLevels...)
		sort.Strings(rule.Aliases)
		sort.Strings(rule.Prefixes)
		sort.Slice(rule.ExactIdentityLevels, func(i, j int) bool { return rule.ExactIdentityLevels[i] < rule.ExactIdentityLevels[j] })
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Scheme < rules[j].Scheme })
	digestInput := struct {
		Version              string           `json:"version"`
		PatternMatchContract string           `json:"pattern_match_contract"`
		Rules                []IdentifierRule `json:"rules"`
	}{
		Version: registry.version, PatternMatchContract: identifierPatternMatchContract, Rules: rules,
	}
	encoded, err := json.Marshal(digestInput)
	if err != nil {
		return "", fmt.Errorf("encoding identifier registry digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func builtInIdentifierRules() []compiledIdentifierRule {
	work := hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK
	version := hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION
	manifestation := hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_MANIFESTATION
	sourceRecord := hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD
	return []compiledIdentifierRule{
		{IdentifierRule: IdentifierRule{Scheme: "doi", NamespaceURI: "https://doi.org/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, DefaultIdentityLevel: work, Pattern: `^10\.\d{4,9}/\S+$`, Prefixes: []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"}, Case: IdentifierCaseLower, TrimTrailingPunctuation: true, ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{work, version, manifestation}}},
		{IdentifierRule: IdentifierRule{Scheme: "handle", NamespaceURI: "https://hdl.handle.net/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE, DefaultIdentityLevel: work, Pattern: `^\d+(?:\.\d+)+/\S+$`, Prefixes: []string{"https://hdl.handle.net/", "http://hdl.handle.net/", "hdl:"}, ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{work, version, manifestation}}},
		{IdentifierRule: IdentifierRule{Scheme: "orcid", NamespaceURI: "https://orcid.org/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID, Pattern: `^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$`, Prefixes: []string{"https://orcid.org/", "http://orcid.org/", "orcid:"}, Case: IdentifierCaseUpper}},
		{IdentifierRule: IdentifierRule{Scheme: "pmid", NamespaceURI: "https://pubmed.ncbi.nlm.nih.gov/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PMID, DefaultIdentityLevel: work, Pattern: `^[1-9][0-9]{0,11}$`, Prefixes: []string{"https://pubmed.ncbi.nlm.nih.gov/", "pmid:"}, ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{work}}},
		{IdentifierRule: IdentifierRule{Scheme: "pmcid", NamespaceURI: "https://pmc.ncbi.nlm.nih.gov/articles/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID, DefaultIdentityLevel: work, Pattern: `^PMC[1-9][0-9]{0,11}$`, Prefixes: []string{"https://www.ncbi.nlm.nih.gov/pmc/articles/", "http://www.ncbi.nlm.nih.gov/pmc/articles/", "https://pmc.ncbi.nlm.nih.gov/articles/", "http://pmc.ncbi.nlm.nih.gov/articles/", "pmcid:"}, Case: IdentifierCaseUpper, ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{work}}},
		{IdentifierRule: IdentifierRule{Scheme: "arxiv", NamespaceURI: "https://arxiv.org/abs/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV, DefaultIdentityLevel: work, Pattern: `^(?:\d{4}\.\d{4,5}|[a-z][a-z0-9.-]*/\d{7})$`, Prefixes: []string{"https://arxiv.org/abs/", "http://arxiv.org/abs/", "https://arxiv.org/pdf/", "http://arxiv.org/pdf/", "arxiv:"}, Case: IdentifierCaseLower, ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{work}}, normalize: normalizeArXivIdentifier},
		{IdentifierRule: IdentifierRule{Scheme: "wos", Aliases: []string{"web-of-science", "ut"}, NamespaceURI: "https://www.webofscience.com/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_WOS, DefaultIdentityLevel: work, Pattern: `^WOS:[A-Z0-9]{12,64}$`, Prefixes: []string{"ut=", "wos:"}, Case: IdentifierCaseUpper, ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{work}}, normalize: normalizeWOSIdentifier},
		{IdentifierRule: IdentifierRule{Scheme: "scopus-eid", Aliases: []string{"eid"}, NamespaceURI: "https://api.elsevier.com/content/abstract/eid/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PID, DefaultIdentityLevel: sourceRecord, Pattern: `^2-s2\.0-[0-9]+$`, Prefixes: []string{"https://api.elsevier.com/content/abstract/eid/", "eid:", "eid="}, Case: IdentifierCaseLower, ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{sourceRecord}}, normalize: normalizeScopusEID},
		{IdentifierRule: IdentifierRule{Scheme: "scopus-id", NamespaceURI: "https://api.elsevier.com/content/abstract/scopus_id/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PID, DefaultIdentityLevel: sourceRecord, Pattern: `^[1-9][0-9]+$`, Prefixes: []string{"https://api.elsevier.com/content/abstract/scopus_id/", "scopus_id:", "scopus-id:", "scopus:"}, ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{sourceRecord}}},
		{IdentifierRule: IdentifierRule{Scheme: "zenodo-record", NamespaceURI: "https://zenodo.org/records/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PID, DefaultIdentityLevel: version, Pattern: `^[1-9][0-9]*$`, Prefixes: []string{"https://zenodo.org/records/", "https://zenodo.org/record/", "zenodo:"}, ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{version, sourceRecord}}},
		{IdentifierRule: IdentifierRule{Scheme: "zenodo-concept", NamespaceURI: "https://zenodo.org/records/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PID, DefaultIdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT, Pattern: `^[1-9][0-9]*$`}},
		{IdentifierRule: IdentifierRule{Scheme: "omeka-s-resource", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, DefaultIdentityLevel: sourceRecord, Pattern: `^[1-9][0-9]*$`}},
		{IdentifierRule: IdentifierRule{Scheme: "archivesspace-uri", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, DefaultIdentityLevel: sourceRecord, Pattern: `^/repositories/[1-9][0-9]*/(?:resources|archival_objects)/[1-9][0-9]*$`}},
		{IdentifierRule: IdentifierRule{Scheme: "archivesspace-resource-id", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, DefaultIdentityLevel: sourceRecord, Pattern: `^\S(?:.*\S)?$`}},
		{IdentifierRule: IdentifierRule{Scheme: "archivesspace-ead-id", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, DefaultIdentityLevel: sourceRecord, Pattern: `^\S(?:.*\S)?$`}},
		{IdentifierRule: IdentifierRule{Scheme: "archivesspace-ref-id", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, DefaultIdentityLevel: sourceRecord, Pattern: `^\S(?:.*\S)?$`}},
		{IdentifierRule: IdentifierRule{Scheme: "archivesspace-component-id", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, DefaultIdentityLevel: sourceRecord, Pattern: `^\S(?:.*\S)?$`}},
		{IdentifierRule: IdentifierRule{Scheme: "archivesspace-external-id", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, DefaultIdentityLevel: sourceRecord, Pattern: `^\S(?:.*\S)?$`}},
		{IdentifierRule: IdentifierRule{Scheme: "archivesspace-agent-uri", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, DefaultIdentityLevel: sourceRecord, Pattern: `^/agents/(?:people|families|corporate_entities|software)/[1-9][0-9]*$`}},
		{IdentifierRule: IdentifierRule{Scheme: "archivesspace-agent-external-id", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, DefaultIdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT, Pattern: `^\S(?:.*\S)?$`}},
		{IdentifierRule: IdentifierRule{Scheme: "ark", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PID, DefaultIdentityLevel: sourceRecord, Pattern: `^https?://\S+/ark:/\S+$`}, normalize: canonicalIdentifierURL},
		{IdentifierRule: IdentifierRule{Scheme: "gnd", NamespaceURI: "https://d-nb.info/gnd/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PID, Pattern: `^[0-9Xx-]+$`, Case: IdentifierCaseUpper}},
		{IdentifierRule: IdentifierRule{Scheme: "lcnaf", Aliases: []string{"naf"}, NamespaceURI: "https://id.loc.gov/authorities/names/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PID, DefaultIdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT, Pattern: `^[A-Za-z0-9]+$`}},
		{IdentifierRule: IdentifierRule{Scheme: "viaf", NamespaceURI: "https://viaf.org/viaf/", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PID, DefaultIdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT, Pattern: `^[1-9][0-9]*$`}},
		{IdentifierRule: IdentifierRule{Scheme: "uuid", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_UUID, DefaultIdentityLevel: sourceRecord, Pattern: `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, Case: IdentifierCaseLower}},
		{IdentifierRule: IdentifierRule{Scheme: "isbn", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN, Pattern: `^(?:[0-9]{9}[0-9X]|[0-9]{13}|[0-9-]+)$`, Case: IdentifierCaseUpper}},
		{IdentifierRule: IdentifierRule{Scheme: "issn", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN, Pattern: `^(?:[0-9]{4}-?[0-9]{3}[0-9X])$`, Case: IdentifierCaseUpper}},
		{IdentifierRule: IdentifierRule{Scheme: "isni", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI, Pattern: `^\S+$`, Case: IdentifierCaseUpper}},
		{IdentifierRule: IdentifierRule{Scheme: "url", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_URL, Pattern: `^https?://\S+$`}, normalize: canonicalIdentifierURL},
		{IdentifierRule: IdentifierRule{Scheme: "local", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, DefaultIdentityLevel: sourceRecord, Pattern: `^\S(?:.*\S)?$`}},
		{IdentifierRule: IdentifierRule{Scheme: "pid", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PID, DefaultIdentityLevel: sourceRecord, Pattern: `^\S(?:.*\S)?$`}},
		{IdentifierRule: IdentifierRule{Scheme: "nid", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_NID, DefaultIdentityLevel: sourceRecord, Pattern: `^\S+$`}},
		{IdentifierRule: IdentifierRule{Scheme: "report-number", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER, Pattern: `^\S(?:.*\S)?$`}},
		{IdentifierRule: IdentifierRule{Scheme: "call-number", Type: hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER, Pattern: `^\S(?:.*\S)?$`}},
	}
}

func normalizeIdentifierValue(value string, rule compiledIdentifierRule) string {
	value = trimIdentifierWrapper(strings.TrimSpace(value))
	if rule.normalize != nil {
		return rule.normalize(value)
	}
	for _, prefix := range rule.Prefixes {
		if len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix) {
			value = value[len(prefix):]
			break
		}
	}
	value = strings.TrimSpace(value)
	if rule.TrimTrailingPunctuation {
		value = strings.TrimRight(value, ".,;")
	}
	switch rule.Case {
	case IdentifierCaseLower:
		value = strings.ToLower(value)
	case IdentifierCaseUpper:
		value = strings.ToUpper(value)
	}
	return value
}

func normalizeArXivIdentifier(value string) string {
	lower := strings.ToLower(value)
	for _, prefix := range []string{"https://arxiv.org/abs/", "http://arxiv.org/abs/", "https://arxiv.org/pdf/", "http://arxiv.org/pdf/", "arxiv:"} {
		if strings.HasPrefix(lower, prefix) {
			value = value[len(prefix):]
			break
		}
	}
	value = strings.SplitN(value, "?", 2)[0]
	value = strings.SplitN(value, "#", 2)[0]
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".pdf")
	return regexp.MustCompile(`(?i)v\d+$`).ReplaceAllString(value, "")
}

func normalizeWOSIdentifier(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "UT=")
	value = strings.TrimPrefix(value, "WOS:")
	return "WOS:" + value
}

func normalizeScopusEID(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && isScopusHost(parsed.Hostname()) {
		if eid := parsed.Query().Get("eid"); eid != "" {
			value = eid
		}
	}
	for _, prefix := range []string{"https://api.elsevier.com/content/abstract/eid/", "eid:", "eid="} {
		if len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix) {
			value = value[len(prefix):]
			break
		}
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func isScopusHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return host == "scopus.com" || strings.HasSuffix(host, ".scopus.com")
}

func canonicalIdentifierURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return strings.TrimSpace(value)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	if (parsed.Scheme == "https" && strings.HasSuffix(parsed.Host, ":443")) || (parsed.Scheme == "http" && strings.HasSuffix(parsed.Host, ":80")) {
		parsed.Host = parsed.Host[:strings.LastIndex(parsed.Host, ":")]
	}
	if parsed.Path != "/" {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}
	return parsed.String()
}

func canonicalNamespaceURI(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || (parsed.Host == "" && parsed.Opaque == "") {
		return "", fmt.Errorf("identifier namespace URI %q must be absolute", value)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("identifier namespace URI %q must not contain credentials, query, or fragment", value)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String(), nil
}

func normalizeIdentifierScheme(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "identifier_type_")
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func schemeForType(identifierType hubv1.IdentifierType) string {
	switch identifierType {
	case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
		return "doi"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
		return "url"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE:
		return "handle"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
		return "isbn"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
		return "issn"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID:
		return "orcid"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_PMID:
		return "pmid"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID:
		return "pmcid"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV:
		return "arxiv"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL:
		return "local"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_PID:
		return "pid"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_NID:
		return "nid"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_UUID:
		return "uuid"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI:
		return "isni"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER:
		return "report-number"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER:
		return "call-number"
	case hubv1.IdentifierType_IDENTIFIER_TYPE_WOS:
		return "wos"
	default:
		return ""
	}
}

func typeForScheme(scheme string) hubv1.IdentifierType {
	if rule, ok := defaultIdentifiers.rules[normalizeIdentifierScheme(scheme)]; ok {
		return rule.Type
	}
	return hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED
}

func identifierHasSignal(original, lower, scheme string) bool {
	switch scheme {
	case "doi":
		return strings.HasPrefix(lower, "10.") || strings.HasPrefix(lower, "doi:") || strings.Contains(lower, "doi.org/")
	case "arxiv":
		return strings.HasPrefix(lower, "arxiv:") || strings.Contains(lower, "arxiv.org/")
	case "handle":
		return strings.HasPrefix(lower, "hdl:") || strings.Contains(lower, "hdl.handle.net/") || regexp.MustCompile(`^\d+(?:\.\d+)+/`).MatchString(original)
	case "orcid":
		return strings.HasPrefix(lower, "orcid:") || strings.Contains(lower, "orcid.org/") || regexp.MustCompile(`^\d{4}-\d{4}-\d{4}-\d{3}[\dXx]$`).MatchString(original)
	case "pmid":
		return strings.HasPrefix(lower, "pmid:") || strings.Contains(lower, "pubmed.ncbi.nlm.nih.gov/")
	case "pmcid":
		return strings.HasPrefix(lower, "pmcid:") || strings.HasPrefix(lower, "pmc") || strings.Contains(lower, "pmc.ncbi.nlm.nih.gov/") || strings.Contains(lower, "ncbi.nlm.nih.gov/pmc/")
	case "wos":
		return strings.HasPrefix(lower, "wos:") || strings.HasPrefix(lower, "ut=")
	case "scopus-eid":
		return strings.HasPrefix(lower, "2-s2.0-") || strings.HasPrefix(lower, "eid:") || strings.HasPrefix(lower, "eid=") || strings.Contains(lower, "scopus.com/")
	case "scopus-id":
		return strings.HasPrefix(lower, "scopus_id:") || strings.HasPrefix(lower, "scopus-id:") || strings.HasPrefix(lower, "scopus:")
	case "zenodo-record":
		return strings.HasPrefix(lower, "zenodo:") || strings.Contains(lower, "zenodo.org/record")
	case "uuid":
		return regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(original)
	case "isbn":
		clean := strings.ReplaceAll(original, "-", "")
		return regexp.MustCompile(`^(?:[0-9]{9}[0-9Xx]|[0-9]{13})$`).MatchString(clean)
	case "issn":
		return regexp.MustCompile(`^[0-9]{4}-?[0-9]{3}[0-9Xx]$`).MatchString(original)
	default:
		return false
	}
}

func validIdentifierIdentityLevel(level hubv1.IdentifierIdentityLevel) bool {
	return level >= hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED && level <= hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD
}

func effectiveIdentifierRegistry(registry *IdentifierRegistry) *IdentifierRegistry {
	if registry == nil {
		return defaultIdentifiers
	}
	return registry
}

func mustIdentifierRegistry(config IdentifierRegistryConfig) *IdentifierRegistry {
	registry, err := NewIdentifierRegistry(config)
	if err != nil {
		panic(err)
	}
	return registry
}

func cloneIdentifierRegistryConfig(config IdentifierRegistryConfig) IdentifierRegistryConfig {
	result := config
	result.Rules = append([]IdentifierRule(nil), config.Rules...)
	for index := range result.Rules {
		result.Rules[index].Aliases = append([]string(nil), config.Rules[index].Aliases...)
		result.Rules[index].Prefixes = append([]string(nil), config.Rules[index].Prefixes...)
		result.Rules[index].ExactIdentityLevels = append([]hubv1.IdentifierIdentityLevel(nil), config.Rules[index].ExactIdentityLevels...)
	}
	result.ExactPolicies = append([]IdentifierExactIdentityPolicy(nil), config.ExactPolicies...)
	for index := range result.ExactPolicies {
		result.ExactPolicies[index].IdentityLevels = append([]hubv1.IdentifierIdentityLevel(nil), config.ExactPolicies[index].IdentityLevels...)
	}
	return result
}
