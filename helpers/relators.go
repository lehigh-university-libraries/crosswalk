package helpers

import "strings"

// MARCRelator represents a MARC relator code and its label.
type MARCRelator struct {
	Code  string
	Label string
}

// MARCRelators maps MARC relator codes to human-readable labels.
// This is a subset of the full MARC relator list, focusing on common scholarly roles.
var MARCRelators = map[string]string{
	// Primary creators
	"aut": "Author",
	"cre": "Creator",
	"edt": "Editor",
	"com": "Compiler",
	"trl": "Translator",
	"ill": "Illustrator",
	"pht": "Photographer",
	"art": "Artist",
	"cmp": "Composer",

	// Contributors
	"ctb": "Contributor",
	"aui": "Author of introduction",
	"aft": "Author of afterword",
	"ann": "Annotator",
	"cmm": "Commentator",
	"wpr": "Writer of preface",
	"wam": "Writer of accompanying material",

	// Thesis-related
	"ths": "Thesis advisor",
	"dgs": "Degree supervisor",
	"dgc": "Degree committee member",
	"opn": "Opponent",

	// Publishing
	"pbl": "Publisher",
	"dst": "Distributor",
	"bkd": "Book designer",
	"bkp": "Book producer",
	"prt": "Printer",
	"tyg": "Typographer",

	// Research
	"res": "Researcher",
	"fnd": "Funder",
	"spn": "Sponsor",
	"his": "Host institution",

	// Data and software
	"dtc": "Data contributor",
	"dtm": "Data manager",
	"prg": "Programmer",

	// Performance
	"prf": "Performer",
	"act": "Actor",
	"nrt": "Narrator",
	"sng": "Singer",
	"cnd": "Conductor",
	"drt": "Director",
	"pro": "Producer",

	// Organization
	"org": "Originator",
	"isb": "Issuing body",
	"cph": "Copyright holder",
	"oth": "Other",

	// Legacy/common
	"col": "Collector",
	"cur": "Curator",
	"own": "Owner",
	"dnr": "Donor",
}

// RelatorCodeFromURI extracts the relator code from a URI like "relators:cre"
func RelatorCodeFromURI(uri string) string {
	// Handle "relators:xxx" format
	if strings.HasPrefix(uri, "relators:") {
		return strings.TrimPrefix(uri, "relators:")
	}

	// Handle full URI like "http://id.loc.gov/vocabulary/relators/aut"
	if strings.Contains(uri, "relators/") {
		parts := strings.Split(uri, "relators/")
		if len(parts) > 1 {
			return strings.TrimSuffix(parts[1], "/")
		}
	}

	// Already just a code
	return uri
}

// RelatorLabel returns the human-readable label for a relator code.
func RelatorLabel(codeOrURI string) string {
	code := strings.ToLower(RelatorCodeFromURI(codeOrURI))

	if label, ok := MARCRelators[code]; ok {
		return label
	}

	// Return the code itself if not found
	return codeOrURI
}

// NormalizeRole normalizes a role string to a canonical form.
// Accepts MARC codes, URIs, or plain text labels.
func NormalizeRole(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return ""
	}

	// Extract code if it's a URI
	code := RelatorCodeFromURI(role)

	// Check if it's a known MARC code
	lowerCode := strings.ToLower(code)
	if _, ok := MARCRelators[lowerCode]; ok {
		return lowerCode
	}

	// Try to match by label
	lowerRole := strings.ToLower(role)
	for c, label := range MARCRelators {
		if strings.ToLower(label) == lowerRole {
			return c
		}
	}

	// Handle some common aliases
	aliases := map[string]string{
		"author":           "aut",
		"authors":          "aut",
		"creator":          "cre",
		"creators":         "cre",
		"editor":           "edt",
		"editors":          "edt",
		"translator":       "trl",
		"contributor":      "ctb",
		"photographer":     "pht",
		"illustrator":      "ill",
		"advisor":          "ths",
		"thesis advisor":   "ths",
		"committee":        "dgc",
		"committee member": "dgc",
		"publisher":        "pbl",
		"funder":           "fnd",
		"sponsor":          "spn",
	}

	if normalized, ok := aliases[lowerRole]; ok {
		return normalized
	}

	// Return original if we can't normalize
	return role
}

// IsCreatorRole returns true if the role is a primary creator role.
func IsCreatorRole(role string) bool {
	code := NormalizeRole(role)
	creatorCodes := map[string]bool{
		"aut": true,
		"cre": true,
		"edt": true,
		"com": true,
		"trl": true,
		"ill": true,
		"pht": true,
		"art": true,
		"cmp": true,
	}
	return creatorCodes[code]
}

// RoleToCode converts a role label to its MARC relator code.
func RoleToCode(role string) string {
	return NormalizeRole(role)
}

// CodeToRole converts a MARC relator code to its label.
func CodeToRole(code string) string {
	return RelatorLabel(code)
}
