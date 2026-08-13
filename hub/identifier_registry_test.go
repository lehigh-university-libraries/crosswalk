package hub

import (
	"reflect"
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"google.golang.org/protobuf/proto"
)

func TestIdentifierRegistryCanonicalizationIsIdempotent(t *testing.T) {
	t.Parallel()
	registry := DefaultIdentifierRegistry()
	tests := []struct {
		name      string
		input     *hubv1.Identifier
		scheme    string
		namespace string
		value     string
		level     hubv1.IdentifierIdentityLevel
	}{
		{"DOI", &hubv1.Identifier{Value: "<HTTPS://DOI.ORG/10.1234/Example.>"}, "doi", "https://doi.org/", "10.1234/example", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK},
		{"arXiv version", &hubv1.Identifier{Value: "https://arxiv.org/pdf/2301.01234v3.pdf?download=1"}, "arxiv", "https://arxiv.org/abs/", "2301.01234", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK},
		{"Web of Science", &hubv1.Identifier{Value: "ut=wos:000123456700001"}, "wos", "https://www.webofscience.com/", "WOS:000123456700001", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK},
		{"Scopus EID", &hubv1.Identifier{Value: "https://www.scopus.com/record/display.uri?eid=2-s2.0-85123456789"}, "scopus-eid", "https://api.elsevier.com/content/abstract/eid/", "2-s2.0-85123456789", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD},
		{"Zenodo record", &hubv1.Identifier{Value: "https://zenodo.org/records/1234567"}, "zenodo-record", "https://zenodo.org/records/", "1234567", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			canonical, err := registry.CanonicalizeIdentifier(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if canonical.GetScheme() != test.scheme || canonical.GetNamespaceUri() != test.namespace || canonical.GetValue() != test.value || canonical.GetIdentityLevel() != test.level {
				t.Fatalf("canonical identifier = %#v", canonical)
			}
			again, err := registry.CanonicalizeIdentifier(canonical)
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(canonical, again) {
				t.Fatalf("canonicalization is not idempotent:\nfirst=%v\nsecond=%v", canonical, again)
			}
		})
	}
}

func TestIdentifierRegistryDistinguishesZenodoConceptAndVersionIdentity(t *testing.T) {
	t.Parallel()
	registry := DefaultIdentifierRegistry()
	version, err := registry.CanonicalizeIdentifier(&hubv1.Identifier{
		Scheme: "doi", Value: "10.5281/zenodo.1234567",
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION,
	})
	if err != nil {
		t.Fatal(err)
	}
	concept, err := registry.CanonicalizeIdentifier(&hubv1.Identifier{
		Scheme: "doi", Value: "10.5281/zenodo.1000000",
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !registry.ExactIdentity(version) {
		t.Fatal("Zenodo version DOI should be exact identity evidence")
	}
	if registry.ExactIdentity(concept) {
		t.Fatal("Zenodo concept DOI must not collapse versions under the default policy")
	}
	conceptPolicy, err := NewIdentifierRegistry(IdentifierRegistryConfig{
		Version: IdentifierRegistryVersion,
		ExactPolicies: []IdentifierExactIdentityPolicy{{
			Scheme: "doi", NamespaceURI: "https://doi.org/",
			IdentityLevels: []hubv1.IdentifierIdentityLevel{
				hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK,
				hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION,
				hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_MANIFESTATION,
				hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT,
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !conceptPolicy.ExactIdentity(concept) {
		t.Fatal("an explicit concept-level policy should permit concept DOI identity")
	}
}

func TestIdentifierRegistryRequiresExplicitScopedInstitutionalRule(t *testing.T) {
	t.Parallel()
	registry := DefaultIdentifierRegistry()
	for _, identifier := range []*hubv1.Identifier{
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, Value: "islandora:123"},
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_PID, Value: "islandora:123"},
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_UUID, Value: "a1b2c3d4-e5f6-47a8-9012-abcdef123456"},
	} {
		if registry.ExactIdentity(identifier) {
			t.Fatalf("unscoped identifier is exact: %v", identifier)
		}
	}

	localRule := IdentifierRule{
		Scheme: "lehigh-item", NamespaceURI: "https://preserve.lehigh.edu/identifiers/item/",
		Type:                 hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
		DefaultIdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
		Pattern:              `^LUP-[0-9]{6}$`, Prefixes: []string{"item:"}, Case: IdentifierCaseUpper,
		ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD},
	}
	registry, err := NewIdentifierRegistry(IdentifierRegistryConfig{Version: IdentifierRegistryVersion, Rules: []IdentifierRule{localRule}})
	if err != nil {
		t.Fatal(err)
	}
	identifier, err := registry.NewIdentifierForScheme("item:lup-000123", "lehigh-item", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED)
	if err != nil {
		t.Fatal(err)
	}
	if identifier.GetValue() != "LUP-000123" || !registry.ExactIdentity(identifier) {
		t.Fatalf("institutional identifier = %v, want canonical exact identifier", identifier)
	}
}

func TestIdentifierRegistryRejectsUnsafeInstitutionalRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rule IdentifierRule
		want string
	}{
		{"unanchored pattern", IdentifierRule{Scheme: "local-item", Pattern: `[0-9]+`}, "anchored"},
		{"exact without authority", IdentifierRule{Scheme: "local-item", Pattern: `^[0-9]+$`, ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD}}, "namespace URI"},
		{"invalid authority", IdentifierRule{Scheme: "local-item", NamespaceURI: "repo one", Pattern: `^[0-9]+$`}, "absolute"},
		{"built-in override", IdentifierRule{Scheme: "doi", NamespaceURI: "https://example.org/", Pattern: `^.*$`}, "already registered"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewIdentifierRegistry(IdentifierRegistryConfig{Version: IdentifierRegistryVersion, Rules: []IdentifierRule{test.rule}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewIdentifierRegistry() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestIdentifierRegistryMatchesInstitutionalAlternationsAsWholeValues(t *testing.T) {
	t.Parallel()

	registry, err := NewIdentifierRegistry(IdentifierRegistryConfig{
		Version: IdentifierRegistryVersion,
		Rules: []IdentifierRule{{
			Scheme: "repository-item", NamespaceURI: "https://repository.example.edu/identifiers/item/",
			DefaultIdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
			Pattern:              `^ABC|XYZ$`,
			ExactIdentityLevels:  []hubv1.IdentifierIdentityLevel{hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"ABC", "XYZ"} {
		identifier, err := registry.NewIdentifierForScheme(value, "repository-item", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED)
		if err != nil {
			t.Errorf("whole alternative %q was rejected: %v", value, err)
			continue
		}
		if !registry.ExactIdentity(identifier) {
			t.Errorf("whole alternative %q is not exact identity", value)
		}
	}
	for _, value := range []string{"ABC-suffix", "prefix-XYZ"} {
		if _, err := registry.NewIdentifierForScheme(value, "repository-item", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED); err == nil {
			t.Errorf("partial alternative %q was accepted as an institutional identifier", value)
		}
	}
}

func TestScopusEIDWrapperRequiresOfficialHost(t *testing.T) {
	t.Parallel()

	registry := DefaultIdentifierRegistry()
	for _, value := range []string{
		"https://evilscopus.com/record/display.uri?eid=2-s2.0-85123456789",
		"https://scopus.com.attacker.example/record/display.uri?eid=2-s2.0-85123456789",
	} {
		if _, err := registry.NewIdentifierForScheme(value, "scopus-eid", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED); err == nil {
			t.Errorf("untrusted Scopus wrapper host was accepted: %q", value)
		}
	}
	identifier, err := registry.NewIdentifierForScheme(
		"https://www.scopus.com/record/display.uri?eid=2-s2.0-85123456789",
		"scopus-eid",
		hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED,
	)
	if err != nil {
		t.Fatalf("official Scopus wrapper was rejected: %v", err)
	}
	if identifier.GetValue() != "2-s2.0-85123456789" || !registry.ExactIdentity(identifier) {
		t.Fatalf("official Scopus wrapper = %v", identifier)
	}
}

func TestIdentifierRegistryDigestIsConfigurationOrderIndependent(t *testing.T) {
	t.Parallel()
	rules := []IdentifierRule{
		{Scheme: "local-a", Aliases: []string{"catalog-a", "source-a"}, NamespaceURI: "https://example.edu/a/", Pattern: `^[A-Z]+$`, Prefixes: []string{"id:", "id:long:"}, Case: IdentifierCaseUpper},
		{Scheme: "local-b", NamespaceURI: "https://example.edu/b/", Pattern: `^[0-9]+$`},
	}
	left, err := NewIdentifierRegistry(IdentifierRegistryConfig{Version: IdentifierRegistryVersion, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	reorderedA := rules[0]
	reorderedA.Aliases = []string{"source-a", "catalog-a"}
	reorderedA.Prefixes = []string{"id:long:", "id:"}
	right, err := NewIdentifierRegistry(IdentifierRegistryConfig{Version: IdentifierRegistryVersion, Rules: []IdentifierRule{rules[1], reorderedA}})
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest() == "" || !reflect.DeepEqual(left.Digest(), right.Digest()) {
		t.Fatalf("registry digests differ: %q != %q", left.Digest(), right.Digest())
	}
	identifier, err := left.NewIdentifierForScheme("id:long:abc", "catalog-a", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED)
	if err != nil {
		t.Fatal(err)
	}
	if identifier.GetValue() != "ABC" || identifier.GetScheme() != "local-a" {
		t.Fatalf("canonical identifier = %v, want longest prefix and canonical scheme", identifier)
	}
}

func TestIdentifierRegistryRulesAreOrderedDefensiveCopies(t *testing.T) {
	t.Parallel()
	registry := DefaultIdentifierRegistry()
	rules := registry.Rules()
	if len(rules) < 2 {
		t.Fatalf("Rules() returned %d rules", len(rules))
	}
	for index := 1; index < len(rules); index++ {
		if rules[index-1].Scheme >= rules[index].Scheme {
			t.Fatalf("Rules() is not strictly ordered at %q, %q", rules[index-1].Scheme, rules[index].Scheme)
		}
	}
	doiBefore, ok := registry.Rule("doi")
	if !ok {
		t.Fatal("DOI rule is missing")
	}
	rules[0].Scheme = "changed"
	for index := range rules {
		if rules[index].Scheme == "doi" {
			rules[index].Prefixes[0] = "changed:"
			rules[index].ExactIdentityLevels[0] = hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT
		}
	}
	doiAfter, _ := registry.Rule("doi")
	if !reflect.DeepEqual(doiBefore, doiAfter) {
		t.Fatal("Rules() exposed mutable registry storage")
	}
}
