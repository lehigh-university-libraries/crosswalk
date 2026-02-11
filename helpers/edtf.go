// Package helpers provides utility functions for parsing and processing metadata values.
package helpers

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

// Alias for convenience
type DateValue = hubv1.DateValue

// EDTFParser parses Extended Date/Time Format strings.
// Supports a practical subset of EDTF Level 0 and Level 1.
type EDTFParser struct{}

var (
	// Year only: 1978
	yearOnlyRegex = regexp.MustCompile(`^(\d{4})([~?%])?$`)

	// Year-month: 1978-03
	yearMonthRegex = regexp.MustCompile(`^(\d{4})-(\d{2})([~?%])?$`)

	// Full date: 1978-03-15
	fullDateRegex = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})([~?%])?$`)

	// Decade: 197X or 1970s
	decadeRegex = regexp.MustCompile(`^(\d{3})[Xx]$|^(\d{4})s$`)

	// Century: 19XX or "19th century"
	centuryRegex = regexp.MustCompile(`^(\d{2})[Xx]{2}$`)

	// Interval/range: 1978/1980 or 1978-03/1980-05
	intervalRegex = regexp.MustCompile(`^(.+)/(.+)$`)

	// ISO timestamp: 2024-12-13T22:43:14+00:00
	timestampRegex = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})`)
)

// Parse parses an EDTF date string into a DateValue.
func (p *EDTFParser) Parse(input string, dateType hubv1.DateType) (*hubv1.DateValue, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return &hubv1.DateValue{}, nil
	}

	result := &hubv1.DateValue{
		Type: dateType,
		Raw:  input,
	}

	// Try timestamp first (ISO 8601)
	if t, err := time.Parse(time.RFC3339, input); err == nil {
		result.Year = int32(t.Year())
		result.Month = int32(t.Month())
		result.Day = int32(t.Day())
		result.Precision = hubv1.DatePrecision_DATE_PRECISION_TIME
		return result, nil
	}

	// Try parsing without timezone
	if timestampRegex.MatchString(input) {
		layouts := []string{
			"2006-01-02T15:04:05Z07:00",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, input); err == nil {
				result.Year = int32(t.Year())
				result.Month = int32(t.Month())
				result.Day = int32(t.Day())
				result.Precision = hubv1.DatePrecision_DATE_PRECISION_TIME
				return result, nil
			}
		}
	}

	// Check for interval/range
	if matches := intervalRegex.FindStringSubmatch(input); matches != nil {
		start := strings.TrimSpace(matches[1])
		end := strings.TrimSpace(matches[2])

		// Parse start date
		startDate, _ := p.Parse(start, dateType)
		result.Year = startDate.Year
		result.Month = startDate.Month
		result.Day = startDate.Day
		result.Precision = startDate.Precision
		result.Qualifier = startDate.Qualifier

		// Parse end date
		endDate, _ := p.Parse(end, dateType)
		result.EndYear = endDate.Year
		result.EndMonth = endDate.Month
		result.EndDay = endDate.Day
		result.IsRange = true

		return result, nil
	}

	// Try full date
	if matches := fullDateRegex.FindStringSubmatch(input); matches != nil {
		year, _ := strconv.Atoi(matches[1])
		month, _ := strconv.Atoi(matches[2])
		day, _ := strconv.Atoi(matches[3])
		result.Year = int32(year)
		result.Month = int32(month)
		result.Day = int32(day)
		result.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
		result.Qualifier = parseQualifier(matches[4])
		return result, nil
	}

	// Try year-month
	if matches := yearMonthRegex.FindStringSubmatch(input); matches != nil {
		year, _ := strconv.Atoi(matches[1])
		month, _ := strconv.Atoi(matches[2])
		result.Year = int32(year)
		result.Month = int32(month)
		result.Precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
		result.Qualifier = parseQualifier(matches[3])
		return result, nil
	}

	// Try year only
	if matches := yearOnlyRegex.FindStringSubmatch(input); matches != nil {
		year, _ := strconv.Atoi(matches[1])
		result.Year = int32(year)
		result.Precision = hubv1.DatePrecision_DATE_PRECISION_YEAR
		result.Qualifier = parseQualifier(matches[2])
		return result, nil
	}

	// Try decade
	if matches := decadeRegex.FindStringSubmatch(input); matches != nil {
		var decadeStr string
		if matches[1] != "" {
			decadeStr = matches[1]
		} else {
			decadeStr = matches[2][:3]
		}
		decade, _ := strconv.Atoi(decadeStr)
		result.Year = int32(decade * 10)
		result.Precision = hubv1.DatePrecision_DATE_PRECISION_DECADE
		return result, nil
	}

	// Try century
	if matches := centuryRegex.FindStringSubmatch(input); matches != nil {
		century, _ := strconv.Atoi(matches[1])
		result.Year = int32(century * 100)
		result.Precision = hubv1.DatePrecision_DATE_PRECISION_CENTURY
		return result, nil
	}

	// Try plain integer year
	if year, err := strconv.Atoi(input); err == nil && year > 0 && year < 3000 {
		result.Year = int32(year)
		result.Precision = hubv1.DatePrecision_DATE_PRECISION_YEAR
		return result, nil
	}

	// Return raw value if we can't parse it
	result.Precision = hubv1.DatePrecision_DATE_PRECISION_UNSPECIFIED
	return result, nil
}

func parseQualifier(s string) hubv1.DateQualifier {
	switch s {
	case "~":
		return hubv1.DateQualifier_DATE_QUALIFIER_APPROXIMATE
	case "?":
		return hubv1.DateQualifier_DATE_QUALIFIER_UNCERTAIN
	case "%":
		return hubv1.DateQualifier_DATE_QUALIFIER_BOTH
	default:
		return hubv1.DateQualifier_DATE_QUALIFIER_UNSPECIFIED
	}
}

// ParseEDTF is a convenience function to parse an EDTF string.
func ParseEDTF(input string, dateType hubv1.DateType) (*hubv1.DateValue, error) {
	parser := &EDTFParser{}
	return parser.Parse(input, dateType)
}

// ParseTimestamp parses a timestamp string into a DateValue.
func ParseTimestamp(input string, dateType hubv1.DateType) (*hubv1.DateValue, error) {
	return ParseEDTF(input, dateType)
}

// FormatDateForCSV formats a DateValue for CSV output.
func FormatDateForCSV(d *hubv1.DateValue) string {
	if d == nil || d.Year == 0 {
		if d != nil {
			return d.Raw
		}
		return ""
	}
	return hub.DateString(d)
}
