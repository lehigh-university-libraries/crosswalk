package hub

import (
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// IdentifierURI returns the identifier as a resolvable URI where possible.
func IdentifierURI(id *hubv1.Identifier) string {
	if id == nil {
		return ""
	}
	if canonical, err := DefaultIdentifierRegistry().CanonicalizeIdentifier(id); err == nil {
		if canonical.GetNamespaceUri() != "" {
			return canonical.GetNamespaceUri() + canonical.GetValue()
		}
		return canonical.GetValue()
	}
	return id.GetValue()
}

// DetectIdentifierType attempts to determine the broad interoperability type
// from its value. New code should retain the open Scheme as well as this enum.
func DetectIdentifierType(value string) hubv1.IdentifierType {
	return typeForScheme(DefaultIdentifierRegistry().DetectScheme(value))
}

// DetectIdentifierScheme returns the canonical open identifier scheme inferred
// from a value.
func DetectIdentifierScheme(value string) string {
	return DefaultIdentifierRegistry().DetectScheme(value)
}

// NewIdentifier creates a canonical Identifier, detecting its scheme when the
// broad type is unspecified. Use IdentifierRegistry.NewIdentifierForScheme for
// institution-defined schemes.
func NewIdentifier(value string, idType hubv1.IdentifierType) *hubv1.Identifier {
	identifier := &hubv1.Identifier{Type: idType, Value: value}
	if idType == hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED {
		identifier.Scheme = DetectIdentifierScheme(value)
	}
	canonical, err := DefaultIdentifierRegistry().CanonicalizeIdentifier(identifier)
	if err == nil {
		return canonical
	}
	// Constructors historically cannot return an error. Preserve invalid source
	// data for hub validation to report instead of silently dropping it.
	identifier.Value = strings.TrimSpace(value)
	identifier.Scheme = schemeForType(idType)
	if identifier.Scheme == "" {
		identifier.Scheme = "local"
	}
	return identifier
}

// NormalizeIdentifier normalizes an identifier value based on its broad type.
// It preserves the legacy string-only API; new code should canonicalize the
// complete Identifier so scheme, authority, and identity level are retained.
func NormalizeIdentifier(value string, idType hubv1.IdentifierType) string {
	return NewIdentifier(value, idType).GetValue()
}

func trimIdentifierWrapper(value string) string {
	value = strings.TrimSpace(value)
	for len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '<' && last == '>') || (first == '(' && last == ')') ||
			(first == '[' && last == ']') || (first == '{' && last == '}') ||
			(first == '"' && last == '"') || (first == '\'' && last == '\'') {
			value = strings.TrimSpace(value[1 : len(value)-1])
			continue
		}
		break
	}
	return value
}
