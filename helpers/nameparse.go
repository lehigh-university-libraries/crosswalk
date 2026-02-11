package helpers

import (
	"regexp"
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

// NameParser parses personal names into components.
type NameParser struct{}

var (
	// Suffixes that appear after a name
	suffixes = []string{"Jr.", "Jr", "Sr.", "Sr", "III", "II", "IV", "V", "PhD", "Ph.D.", "MD", "M.D.", "Esq.", "Esq"}

	// Name prefixes (nobiliary particles)
	prefixes = []string{"van", "von", "de", "del", "della", "di", "da", "le", "la", "du", "des", "den", "der", "het", "ter", "ten", "op", "mc", "mac", "o'", "d'", "al-", "el-", "ibn"}

	// Pattern for "Last, First Middle" format
	invertedNameRegex = regexp.MustCompile(`^([^,]+),\s*(.+)$`)
)

// Parse parses a name string into its components.
// Handles both "First Last" and "Last, First" formats.
func (p *NameParser) Parse(name string) *hubv1.ParsedName {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	result := &hubv1.ParsedName{
		FullName: name,
	}

	// Check for inverted format: "Last, First Middle Suffix"
	if matches := invertedNameRegex.FindStringSubmatch(name); matches != nil {
		result.Family = strings.TrimSpace(matches[1])
		rest := strings.TrimSpace(matches[2])

		// Extract suffix from the rest
		rest, result.Suffix = extractSuffix(rest)

		// Split remaining into given and middle
		parts := strings.Fields(rest)
		if len(parts) > 0 {
			result.Given = parts[0]
		}
		if len(parts) > 1 {
			result.Middle = strings.Join(parts[1:], " ")
		}
	} else {
		// Direct format: "First Middle Prefix Last Suffix"
		name, result.Suffix = extractSuffix(name)
		parts := strings.Fields(name)

		if len(parts) == 0 {
			return nil
		}

		if len(parts) == 1 {
			// Single name - treat as family name
			result.Family = parts[0]
		} else {
			// Find where the family name starts (after any prefix)
			familyStart := len(parts) - 1

			// Check for prefix before last name
			if familyStart > 0 && isPrefix(parts[familyStart-1]) {
				result.Prefix = parts[familyStart-1]
				familyStart--
			}

			result.Family = strings.Join(parts[familyStart:], " ")
			if result.Prefix != "" {
				result.Family = result.Prefix + " " + parts[len(parts)-1]
			}

			// First name is always first part
			result.Given = parts[0]

			// Middle name(s) are everything between first and family
			if familyStart > 1 {
				middleParts := parts[1:familyStart]
				result.Middle = strings.Join(middleParts, " ")
			}
		}
	}

	// Build normalized form
	result.Normalized = hub.ParsedNameInverted(result)

	return result
}

// extractSuffix extracts a suffix from a name string.
func extractSuffix(name string) (string, string) {
	// Look for common suffixes at the end
	for _, suffix := range suffixes {
		// Check with trailing comma (common format)
		if strings.HasSuffix(name, ", "+suffix) {
			return strings.TrimSuffix(name, ", "+suffix), suffix
		}
		// Check without comma
		if strings.HasSuffix(name, " "+suffix) {
			return strings.TrimSuffix(name, " "+suffix), suffix
		}
	}
	return name, ""
}

// isPrefix checks if a word is a nobiliary particle.
func isPrefix(word string) bool {
	lower := strings.ToLower(word)
	for _, prefix := range prefixes {
		if lower == prefix || lower == strings.TrimSuffix(prefix, "'") {
			return true
		}
	}
	return false
}

// ParseName is a convenience function to parse a name string.
func ParseName(name string) *hubv1.ParsedName {
	parser := &NameParser{}
	return parser.Parse(name)
}

// FormatNameInverted formats a name in "Last, First Middle Suffix" form.
func FormatNameInverted(name string) string {
	parsed := ParseName(name)
	if parsed == nil {
		return name
	}
	return hub.ParsedNameInverted(parsed)
}

// FormatNameDirect formats a name in "First Middle Last Suffix" form.
func FormatNameDirect(name string) string {
	parsed := ParseName(name)
	if parsed == nil {
		return name
	}
	return hub.ParsedNameDirect(parsed)
}

// IsInvertedName checks if a name appears to be in "Last, First" format.
func IsInvertedName(name string) bool {
	return strings.Contains(name, ",")
}

// NormalizeName normalizes a name to a consistent format.
// If already inverted, keeps it; otherwise converts to inverted.
func NormalizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	// Clean up extra whitespace
	name = regexp.MustCompile(`\s+`).ReplaceAllString(name, " ")

	// If already in inverted format, just clean it up
	if IsInvertedName(name) {
		parsed := ParseName(name)
		if parsed != nil {
			return hub.ParsedNameInverted(parsed)
		}
		return name
	}

	// Convert to inverted format
	return FormatNameInverted(name)
}

// SplitNames splits a string containing multiple names.
// Handles semicolon, "and", and numbered list separators.
func SplitNames(names string) []string {
	if names == "" {
		return nil
	}

	// First try semicolon
	if strings.Contains(names, ";") {
		parts := strings.Split(names, ";")
		return cleanNameList(parts)
	}

	// Try " and " separator (but not if it's part of a name)
	if strings.Contains(names, " and ") && !strings.Contains(names, ",") {
		parts := strings.Split(names, " and ")
		return cleanNameList(parts)
	}

	// Try pipe separator (common in CSV)
	if strings.Contains(names, "|") {
		parts := strings.Split(names, "|")
		return cleanNameList(parts)
	}

	// Single name
	return []string{strings.TrimSpace(names)}
}

func cleanNameList(parts []string) []string {
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
