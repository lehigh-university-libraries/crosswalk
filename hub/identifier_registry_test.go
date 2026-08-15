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

func TestIdentifierRegistryCanonicalAuthorityResolvers(t *testing.T) {
	t.Parallel()
	registry := DefaultIdentifierRegistry()
	tests := []struct {
		name      string
		scheme    string
		value     string
		canonical string
		uri       string
	}{
		{name: "Handle", scheme: "handle", value: "https://hdl.handle.net/20.500.12345/example", canonical: "20.500.12345/example", uri: "https://hdl.handle.net/20.500.12345/example"},
		{name: "ISBN", scheme: "isbn", value: "ISBN-13: 978-0-306-40615-7", canonical: "9780306406157", uri: "urn:isbn:9780306406157"},
		{name: "ISSN", scheme: "issn", value: "urn:issn:2049-3630", canonical: "2049-3630", uri: "urn:issn:2049-3630"},
		{name: "ROR", scheme: "ror", value: "https://ror.org/02MHBDP94", canonical: "02mhbdp94", uri: "https://ror.org/02mhbdp94"},
		{name: "GND", scheme: "gnd", value: "https://d-nb.info/gnd/118540238", canonical: "118540238", uri: "https://d-nb.info/gnd/118540238"},
		{name: "ISNI", scheme: "isni", value: "https://isni.org/isni/000000012124423X", canonical: "000000012124423X", uri: "https://isni.org/isni/000000012124423X"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			identifier, err := registry.NewIdentifierForScheme(test.value, test.scheme, hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED)
			if err != nil {
				t.Fatal(err)
			}
			if identifier.GetValue() != test.canonical || identifier.GetScheme() != test.scheme {
				t.Fatalf("canonical identifier = %v, want %s:%s", identifier, test.scheme, test.canonical)
			}
			if got := IdentifierURI(identifier); got != test.uri {
				t.Fatalf("IdentifierURI() = %q, want %q", got, test.uri)
			}
			if got := registry.DetectScheme(test.value); got != test.scheme {
				t.Fatalf("DetectScheme(%q) = %q, want %q", test.value, got, test.scheme)
			}
		})
	}
}

func TestIdentifierRegistryRejectsMalformedRORIdentifiers(t *testing.T) {
	t.Parallel()
	registry := DefaultIdentifierRegistry()
	for _, value := range []string{
		"https://ror.org/02mhbdp9",
		"https://ror.org/12mhbdp94",
		"https://ror.org/02mhbdp9x",
		"https://ror.org/02mhbdp94/extra",
		"https://ror.org.attacker.example/02mhbdp94",
	} {
		if _, err := registry.NewIdentifierForScheme(value, "ror", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED); err == nil {
			t.Errorf("NewIdentifierForScheme(%q, ror) succeeded, want error", value)
		}
	}
}

func TestIdentifierRegistryValidatesISBNFormats(t *testing.T) {
	t.Parallel()
	registry := DefaultIdentifierRegistry()
	tests := []struct {
		name  string
		value string
		want  string
		valid bool
	}{
		{name: "ISBN-10 digits", value: "0306406152", want: "0306406152", valid: true},
		{name: "ISBN-10 lowercase check digit and prefix", value: "ISBN-10: 0-9752298-0-x", want: "097522980X", valid: true},
		{name: "ISBN-13 digits", value: "9780306406157", want: "9780306406157", valid: true},
		{name: "ISBN-13 hyphens and prefix", value: "ISBN-13: 978-0-306-40615-7", want: "9780306406157", valid: true},
		{name: "generic prefix and spaces", value: "isbn: 978 0 306 40615 7", want: "9780306406157", valid: true},
		{name: "single digit", value: "1"},
		{name: "short hyphenated", value: "123-45"},
		{name: "truncated ISBN-10", value: "0-306-40615"},
		{name: "truncated ISBN-13", value: "978-0-306-40615"},
		{name: "too many digits", value: "97803064061570"},
		{name: "double hyphen", value: "978--0-306-40615-7"},
		{name: "leading hyphen", value: "-9780306406157"},
		{name: "trailing hyphen", value: "9780306406157-"},
		{name: "ISBN-10 X before check digit", value: "03064061X2"},
		{name: "ISBN-13 X check digit", value: "978030640615X"},
		{name: "non-digit", value: "978-0-30A-40615-7"},
		{name: "ISBN-10 prefix with ISBN-13 value", value: "ISBN-10: 9780306406157"},
		{name: "ISBN-13 prefix with ISBN-10 value", value: "ISBN-13: 0306406152"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			identifier := &hubv1.Identifier{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN, Value: test.value}
			canonical, err := registry.CanonicalizeIdentifier(identifier)
			result := Validate(&hubv1.Record{Title: "ISBN validation", Identifiers: []*hubv1.Identifier{identifier}}, DefaultValidationOptions())
			if !test.valid {
				if err == nil {
					t.Errorf("CanonicalizeIdentifier(%q) = %v, want error", test.value, canonical)
				}
				if result.IsValid() {
					t.Errorf("Validate() accepted invalid ISBN %q", test.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalizeIdentifier(%q): %v", test.value, err)
			}
			if canonical.GetScheme() != "isbn" || canonical.GetValue() != test.want {
				t.Errorf("CanonicalizeIdentifier(%q) = %v, want ISBN value %q", test.value, canonical, test.want)
			}
			if scheme := registry.DetectScheme(test.value); scheme != "isbn" {
				t.Errorf("DetectScheme(%q) = %q, want isbn", test.value, scheme)
			}
			if err := result.Error(); err != nil {
				t.Errorf("Validate() rejected valid ISBN %q: %v", test.value, err)
			}
		})
	}
}

func TestIdentifierRegistryValidatesISNIFormats(t *testing.T) {
	t.Parallel()
	registry := DefaultIdentifierRegistry()
	tests := []struct {
		name  string
		value string
		want  string
		valid bool
	}{
		{name: "compact", value: "000000012124423X", want: "000000012124423X", valid: true},
		{name: "human-readable", value: "ISNI 0000 0001 2124 423x", want: "000000012124423X", valid: true},
		{name: "canonical URL", value: "https://isni.org/isni/000000012124423X", want: "000000012124423X", valid: true},
		{name: "arbitrary text", value: "not-an-isni"},
		{name: "too short", value: "000000012124423"},
		{name: "misplaced check digit", value: "00000001212442X3"},
		{name: "malformed grouping", value: "0000  0001 2124 423X"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			canonical, err := registry.CanonicalizeIdentifier(&hubv1.Identifier{
				Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI,
				Value: test.value,
			})
			if !test.valid {
				if err == nil {
					t.Errorf("CanonicalizeIdentifier(%q) = %v, want error", test.value, canonical)
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalizeIdentifier(%q): %v", test.value, err)
			}
			if canonical.GetScheme() != "isni" || canonical.GetValue() != test.want {
				t.Errorf("CanonicalizeIdentifier(%q) = %v, want ISNI value %q", test.value, canonical, test.want)
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
