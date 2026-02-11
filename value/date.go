package value

import (
	"regexp"
	"strconv"
	"strings"
)

// DatePrecision indicates the granularity of a date.
type DatePrecision int

const (
	PrecisionUnknown DatePrecision = iota
	PrecisionYear
	PrecisionMonth
	PrecisionDay
)

// DateQualifier indicates uncertainty or approximation.
type DateQualifier int

const (
	QualifierNone        DateQualifier = iota
	QualifierApproximate               // ~
	QualifierUncertain                 // ?
	QualifierBoth                      // %
)

// Date represents a parsed date with precision and qualifiers.
type Date struct {
	Year      int
	Month     int
	Day       int
	EndYear   int // For ranges
	EndMonth  int
	EndDay    int
	Precision DatePrecision
	Qualifier DateQualifier
	IsRange   bool
	Raw       string // Original string
}

// String returns a formatted date string.
func (d Date) String() string {
	if d.Raw != "" {
		return d.Raw
	}
	if d.Year == 0 {
		return ""
	}

	var sb strings.Builder

	// Start date
	sb.WriteString(strconv.Itoa(d.Year))
	if d.Month > 0 {
		sb.WriteString("-")
		if d.Month < 10 {
			sb.WriteString("0")
		}
		sb.WriteString(strconv.Itoa(d.Month))
		if d.Day > 0 {
			sb.WriteString("-")
			if d.Day < 10 {
				sb.WriteString("0")
			}
			sb.WriteString(strconv.Itoa(d.Day))
		}
	}

	// Qualifier
	switch d.Qualifier {
	case QualifierApproximate:
		sb.WriteString("~")
	case QualifierUncertain:
		sb.WriteString("?")
	case QualifierBoth:
		sb.WriteString("%")
	}

	// Range
	if d.IsRange && d.EndYear > 0 {
		sb.WriteString("/")
		sb.WriteString(strconv.Itoa(d.EndYear))
		if d.EndMonth > 0 {
			sb.WriteString("-")
			if d.EndMonth < 10 {
				sb.WriteString("0")
			}
			sb.WriteString(strconv.Itoa(d.EndMonth))
			if d.EndDay > 0 {
				sb.WriteString("-")
				if d.EndDay < 10 {
					sb.WriteString("0")
				}
				sb.WriteString(strconv.Itoa(d.EndDay))
			}
		}
	}

	return sb.String()
}

// IsZero returns true if the date has no meaningful value.
func (d Date) IsZero() bool {
	return d.Year == 0 && d.Month == 0 && d.Day == 0
}

// Common date patterns
var (
	// ISO 8601 / EDTF patterns
	isoDateRegex   = regexp.MustCompile(`^(\d{4})(?:-(\d{2})(?:-(\d{2}))?)?([~?%])?$`)
	edtfRangeRegex = regexp.MustCompile(`^(.+)/(.+)$`)

	// Year only with qualifier
	yearOnlyRegex = regexp.MustCompile(`^(\d{4})([~?%])?$`)
)

// ParseDate parses a date string with format auto-detection.
// Supports ISO 8601, EDTF, and common human-readable formats.
func ParseDate(s string) (Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Date{}, nil
	}

	d := Date{Raw: s}

	// Check for EDTF range first
	if matches := edtfRangeRegex.FindStringSubmatch(s); matches != nil {
		start, _ := ParseDate(matches[1])
		end, _ := ParseDate(matches[2])
		d.Year = start.Year
		d.Month = start.Month
		d.Day = start.Day
		d.EndYear = end.Year
		d.EndMonth = end.Month
		d.EndDay = end.Day
		d.Precision = start.Precision
		d.Qualifier = start.Qualifier
		d.IsRange = true
		return d, nil
	}

	// Try ISO/EDTF format
	if matches := isoDateRegex.FindStringSubmatch(s); matches != nil {
		d.Year, _ = strconv.Atoi(matches[1])
		d.Precision = PrecisionYear

		if matches[2] != "" {
			d.Month, _ = strconv.Atoi(matches[2])
			d.Precision = PrecisionMonth
		}
		if matches[3] != "" {
			d.Day, _ = strconv.Atoi(matches[3])
			d.Precision = PrecisionDay
		}
		if matches[4] != "" {
			switch matches[4] {
			case "~":
				d.Qualifier = QualifierApproximate
			case "?":
				d.Qualifier = QualifierUncertain
			case "%":
				d.Qualifier = QualifierBoth
			}
		}
		return d, nil
	}

	// Try year only
	if matches := yearOnlyRegex.FindStringSubmatch(s); matches != nil {
		d.Year, _ = strconv.Atoi(matches[1])
		d.Precision = PrecisionYear
		if matches[2] != "" {
			switch matches[2] {
			case "~":
				d.Qualifier = QualifierApproximate
			case "?":
				d.Qualifier = QualifierUncertain
			case "%":
				d.Qualifier = QualifierBoth
			}
		}
		return d, nil
	}

	// Try to extract just a year from anywhere in the string
	yearExtract := regexp.MustCompile(`\b(1[0-9]{3}|20[0-9]{2})\b`)
	if matches := yearExtract.FindStringSubmatch(s); matches != nil {
		d.Year, _ = strconv.Atoi(matches[1])
		d.Precision = PrecisionYear
		return d, nil
	}

	return d, nil
}

// ParseEDTF parses Extended Date/Time Format strings.
// This is the primary format used by Drupal's EDTF field.
func ParseEDTF(s string) (Date, error) {
	return ParseDate(s)
}

// DateSlice parses multiple date strings.
func DateSlice(v any, opts ...TextOption) []Date {
	strings := TextSlice(v, opts...)
	if len(strings) == 0 {
		return nil
	}

	result := make([]Date, 0, len(strings))
	for _, s := range strings {
		if d, err := ParseDate(s); err == nil && !d.IsZero() {
			result = append(result, d)
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
