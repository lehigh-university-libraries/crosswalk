package hub

import (
	"regexp"
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

var (
	doiRegex    = regexp.MustCompile(`^10\.\d{4,}/[^\s]+$`)
	handleRegex = regexp.MustCompile(`^\d+\.\d+/[^\s]+$`)
	orcidRegex  = regexp.MustCompile(`^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$`)
	isbnRegex   = regexp.MustCompile(`^(?:\d{10}|\d{13}|\d{1,5}-\d{1,7}-\d{1,7}-[\dX])$`)
	issnRegex   = regexp.MustCompile(`^\d{4}-\d{3}[\dX]$`)
	uuidRegex   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// IdentifierURI returns the identifier as a resolvable URI where possible.
func IdentifierURI(id *hubv1.Identifier) string {
	switch id.Type {
	case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
		return "https://doi.org/" + id.Value
	case hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE:
		return "https://hdl.handle.net/" + id.Value
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID:
		return "https://orcid.org/" + id.Value
	case hubv1.IdentifierType_IDENTIFIER_TYPE_PMID:
		return "https://pubmed.ncbi.nlm.nih.gov/" + id.Value
	case hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID:
		return "https://www.ncbi.nlm.nih.gov/pmc/articles/" + id.Value
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV:
		return "https://arxiv.org/abs/" + id.Value
	case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
		return id.Value
	default:
		return id.Value
	}
}

// DetectIdentifierType attempts to determine the identifier type from its value.
func DetectIdentifierType(value string) hubv1.IdentifierType {
	value = strings.TrimSpace(value)

	// Check for DOI
	if strings.HasPrefix(value, "10.") && doiRegex.MatchString(value) {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
	}
	if strings.HasPrefix(value, "https://doi.org/") || strings.HasPrefix(value, "http://doi.org/") {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
	}
	if strings.HasPrefix(value, "doi:") {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
	}

	// Check for Handle
	if handleRegex.MatchString(value) {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE
	}
	if strings.HasPrefix(value, "https://hdl.handle.net/") || strings.HasPrefix(value, "http://hdl.handle.net/") {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE
	}

	// Check for ORCID
	if orcidRegex.MatchString(value) {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID
	}
	if strings.HasPrefix(value, "https://orcid.org/") {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID
	}

	// Check for UUID
	if uuidRegex.MatchString(value) {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_UUID
	}

	// Check for ISBN
	cleanISBN := strings.ReplaceAll(value, "-", "")
	if len(cleanISBN) == 10 || len(cleanISBN) == 13 {
		if isbnRegex.MatchString(value) || regexp.MustCompile(`^\d{10}$|^\d{13}$`).MatchString(cleanISBN) {
			return hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN
		}
	}

	// Check for ISSN
	if issnRegex.MatchString(value) {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN
	}

	// Check for URL
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_URL
	}

	// Check for arXiv
	if strings.HasPrefix(value, "arXiv:") || strings.Contains(value, "arxiv.org") {
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV
	}

	return hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED
}

// NewIdentifier creates a new Identifier with automatic type detection if type is unspecified.
func NewIdentifier(value string, idType hubv1.IdentifierType) *hubv1.Identifier {
	if idType == hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED {
		idType = DetectIdentifierType(value)
	}
	return &hubv1.Identifier{
		Type:  idType,
		Value: NormalizeIdentifier(value, idType),
	}
}

// NormalizeIdentifier normalizes an identifier value based on its type.
func NormalizeIdentifier(value string, idType hubv1.IdentifierType) string {
	value = strings.TrimSpace(value)

	switch idType {
	case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
		value = strings.TrimPrefix(value, "https://doi.org/")
		value = strings.TrimPrefix(value, "http://doi.org/")
		value = strings.TrimPrefix(value, "doi:")
		value = strings.TrimPrefix(value, "DOI:")
		return value

	case hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE:
		value = strings.TrimPrefix(value, "https://hdl.handle.net/")
		value = strings.TrimPrefix(value, "http://hdl.handle.net/")
		value = strings.TrimPrefix(value, "hdl:")
		return value

	case hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID:
		value = strings.TrimPrefix(value, "https://orcid.org/")
		value = strings.TrimPrefix(value, "http://orcid.org/")
		return value

	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN, hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
		return strings.ToUpper(value)

	default:
		return value
	}
}
