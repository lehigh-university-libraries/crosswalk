package hub

import (
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// RightsString returns a human-readable rights string.
func RightsString(r *hubv1.Rights) string {
	if r.Statement != "" {
		return r.Statement
	}
	if r.License != "" {
		return r.License
	}
	return r.Uri
}

// IsOpenAccess returns true if the rights allow open access.
func IsOpenAccess(r *hubv1.Rights) bool {
	uri := strings.ToLower(r.Uri)
	license := strings.ToLower(r.License)

	// Creative Commons licenses
	if strings.Contains(uri, "creativecommons.org") {
		return true
	}
	if strings.HasPrefix(license, "cc") && !strings.Contains(license, "nc") {
		return true
	}

	// Public domain
	if strings.Contains(uri, "publicdomain") {
		return true
	}
	if strings.Contains(uri, "nkc/1.0") { // No Known Copyright
		return true
	}

	return false
}

// RightsStatementFromURI extracts the rights statement code from a rightsstatements.org URI.
func RightsStatementFromURI(uri string) string {
	if !strings.Contains(uri, "rightsstatements.org") {
		return ""
	}
	parts := strings.Split(strings.TrimSuffix(uri, "/"), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return ""
}

// Standard rightsstatements.org URIs
var RightsStatements = map[string]string{
	"InC":       "http://rightsstatements.org/vocab/InC/1.0/",
	"InC-OW-EU": "http://rightsstatements.org/vocab/InC-OW-EU/1.0/",
	"InC-EDU":   "http://rightsstatements.org/vocab/InC-EDU/1.0/",
	"InC-NC":    "http://rightsstatements.org/vocab/InC-NC/1.0/",
	"InC-RUU":   "http://rightsstatements.org/vocab/InC-RUU/1.0/",
	"NoC-CR":    "http://rightsstatements.org/vocab/NoC-CR/1.0/",
	"NoC-NC":    "http://rightsstatements.org/vocab/NoC-NC/1.0/",
	"NoC-OKLR":  "http://rightsstatements.org/vocab/NoC-OKLR/1.0/",
	"NoC-US":    "http://rightsstatements.org/vocab/NoC-US/1.0/",
	"CNE":       "http://rightsstatements.org/vocab/CNE/1.0/",
	"UND":       "http://rightsstatements.org/vocab/UND/1.0/",
	"NKC":       "http://rightsstatements.org/vocab/NKC/1.0/",
}

// RightsStatementLabels maps codes to human-readable labels.
var RightsStatementLabels = map[string]string{
	"InC":       "In Copyright",
	"InC-OW-EU": "In Copyright - EU Orphan Work",
	"InC-EDU":   "In Copyright - Educational Use Permitted",
	"InC-NC":    "In Copyright - Non-Commercial Use Permitted",
	"InC-RUU":   "In Copyright - Rights-holder(s) Unlocatable or Unidentifiable",
	"NoC-CR":    "No Copyright - Contractual Restrictions",
	"NoC-NC":    "No Copyright - Non-Commercial Use Only",
	"NoC-OKLR":  "No Copyright - Other Known Legal Restrictions",
	"NoC-US":    "No Copyright - United States",
	"CNE":       "Copyright Not Evaluated",
	"UND":       "Copyright Undetermined",
	"NKC":       "No Known Copyright",
}

// LabelForRightsURI returns a human-readable label for a rights URI.
func LabelForRightsURI(uri string) string {
	code := RightsStatementFromURI(uri)
	if label, ok := RightsStatementLabels[code]; ok {
		return label
	}

	// Check for Creative Commons
	if strings.Contains(uri, "creativecommons.org") {
		if strings.Contains(uri, "/zero/") || strings.Contains(uri, "/publicdomain/") {
			return "CC0 / Public Domain"
		}
		if strings.Contains(uri, "/by/") {
			if strings.Contains(uri, "-sa") {
				return "CC BY-SA"
			}
			if strings.Contains(uri, "-nc") {
				if strings.Contains(uri, "-nd") {
					return "CC BY-NC-ND"
				}
				return "CC BY-NC"
			}
			if strings.Contains(uri, "-nd") {
				return "CC BY-ND"
			}
			return "CC BY"
		}
	}

	return uri
}

// NewRightsFromURI creates a Rights from a URI.
func NewRightsFromURI(uri string) *hubv1.Rights {
	return &hubv1.Rights{
		Uri:       uri,
		Statement: LabelForRightsURI(uri),
	}
}
