package hub

import (
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestStrongIdentifierNormalization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		type_ hubv1.IdentifierType
		want  string
	}{
		{name: "DOI URL case and wrapper", value: "<HTTPS://DOI.ORG/10.1234/Example>", type_: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, want: "10.1234/example"},
		{name: "legacy DOI resolver", value: "https://dx.doi.org/10.1234/EXAMPLE", type_: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, want: "10.1234/example"},
		{name: "arXiv abstract version", value: "https://arxiv.org/abs/2401.01234v3", type_: hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV, want: "2401.01234"},
		{name: "arXiv PDF", value: "arXiv:hep-th/9901001v2", type_: hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV, want: "hep-th/9901001"},
		{name: "Web of Science", value: "ut=wos:000123456700001", type_: hubv1.IdentifierType_IDENTIFIER_TYPE_WOS, want: "WOS:000123456700001"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeIdentifier(test.value, test.type_); got != test.want {
				t.Fatalf("NormalizeIdentifier(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestDetectIdentifierTypePrefersStrongResolvableIdentifiers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  hubv1.IdentifierType
	}{
		{value: "https://arxiv.org/abs/2401.01234v2", want: hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV},
		{value: "WOS:000123456700001", want: hubv1.IdentifierType_IDENTIFIER_TYPE_WOS},
		{value: "UT=000123456700001", want: hubv1.IdentifierType_IDENTIFIER_TYPE_WOS},
		{value: "https://example.edu/item", want: hubv1.IdentifierType_IDENTIFIER_TYPE_URL},
	}
	for _, test := range tests {
		if got := DetectIdentifierType(test.value); got != test.want {
			t.Errorf("DetectIdentifierType(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}
