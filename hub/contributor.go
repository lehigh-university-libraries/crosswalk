package hub

import (
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// DisplayName returns the best available display name for the contributor.
func DisplayName(c *hubv1.Contributor) string {
	if c.ParsedName != nil && c.ParsedName.Normalized != "" {
		return c.ParsedName.Normalized
	}
	return c.Name
}

// InvertedName returns the name in "Family, Given" format if possible.
func InvertedName(c *hubv1.Contributor) string {
	if c.ParsedName != nil {
		return ParsedNameInverted(c.ParsedName)
	}
	return c.Name
}

// DirectName returns the name in "Given Family" format if possible.
func DirectName(c *hubv1.Contributor) string {
	if c.ParsedName != nil {
		return ParsedNameDirect(c.ParsedName)
	}
	return c.Name
}

// ParsedNameInverted returns the name in "Family, Given Middle Suffix" format.
func ParsedNameInverted(p *hubv1.ParsedName) string {
	if p == nil {
		return ""
	}
	result := p.Family
	if p.Given != "" {
		result += ", " + p.Given
	}
	if p.Middle != "" {
		result += " " + p.Middle
	}
	if p.Suffix != "" {
		result += " " + p.Suffix
	}
	return result
}

// ParsedNameDirect returns the name in "Given Middle Family Suffix" format.
func ParsedNameDirect(p *hubv1.ParsedName) string {
	if p == nil {
		return ""
	}
	var parts []string
	if p.Prefix != "" {
		parts = append(parts, p.Prefix)
	}
	if p.Given != "" {
		parts = append(parts, p.Given)
	}
	if p.Middle != "" {
		parts = append(parts, p.Middle)
	}
	if p.Family != "" {
		parts = append(parts, p.Family)
	}
	if p.Suffix != "" {
		parts = append(parts, p.Suffix)
	}
	return strings.Join(parts, " ")
}
