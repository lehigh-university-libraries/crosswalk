package hub

import (
	"fmt"
	"time"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// DateString returns the date in a normalized string format.
func DateString(d *hubv1.DateValue) string {
	if d.Raw != "" {
		return d.Raw
	}
	return FormatDate(d)
}

// FormatDate returns the date formatted according to its precision.
func FormatDate(d *hubv1.DateValue) string {
	if d.Year == 0 {
		return ""
	}

	var result string
	switch d.Precision {
	case hubv1.DatePrecision_DATE_PRECISION_DECADE:
		result = fmt.Sprintf("%d0s", d.Year/10)
	case hubv1.DatePrecision_DATE_PRECISION_CENTURY:
		result = fmt.Sprintf("%dth century", (d.Year/100)+1)
	case hubv1.DatePrecision_DATE_PRECISION_YEAR:
		result = fmt.Sprintf("%d", d.Year)
	case hubv1.DatePrecision_DATE_PRECISION_MONTH:
		result = fmt.Sprintf("%d-%02d", d.Year, d.Month)
	case hubv1.DatePrecision_DATE_PRECISION_DAY, hubv1.DatePrecision_DATE_PRECISION_TIME:
		result = fmt.Sprintf("%d-%02d-%02d", d.Year, d.Month, d.Day)
	default:
		result = fmt.Sprintf("%d", d.Year)
	}

	if d.IsRange && d.EndYear > 0 {
		var end string
		switch d.Precision {
		case hubv1.DatePrecision_DATE_PRECISION_YEAR:
			end = fmt.Sprintf("%d", d.EndYear)
		case hubv1.DatePrecision_DATE_PRECISION_MONTH:
			end = fmt.Sprintf("%d-%02d", d.EndYear, d.EndMonth)
		case hubv1.DatePrecision_DATE_PRECISION_DAY:
			end = fmt.Sprintf("%d-%02d-%02d", d.EndYear, d.EndMonth, d.EndDay)
		default:
			end = fmt.Sprintf("%d", d.EndYear)
		}
		result = result + "/" + end
	}

	switch d.Qualifier {
	case hubv1.DateQualifier_DATE_QUALIFIER_APPROXIMATE:
		result = result + "~"
	case hubv1.DateQualifier_DATE_QUALIFIER_UNCERTAIN:
		result = result + "?"
	case hubv1.DateQualifier_DATE_QUALIFIER_BOTH:
		result = result + "%"
	}

	return result
}

// FormatEDTF returns the date in Extended Date/Time Format.
func FormatEDTF(d *hubv1.DateValue) string {
	if d.Year == 0 {
		return ""
	}

	var result string
	switch d.Precision {
	case hubv1.DatePrecision_DATE_PRECISION_DECADE:
		result = fmt.Sprintf("%dX", d.Year/10)
	case hubv1.DatePrecision_DATE_PRECISION_CENTURY:
		result = fmt.Sprintf("%dXX", d.Year/100)
	case hubv1.DatePrecision_DATE_PRECISION_YEAR:
		result = fmt.Sprintf("%04d", d.Year)
	case hubv1.DatePrecision_DATE_PRECISION_MONTH:
		result = fmt.Sprintf("%04d-%02d", d.Year, d.Month)
	case hubv1.DatePrecision_DATE_PRECISION_DAY, hubv1.DatePrecision_DATE_PRECISION_TIME:
		result = fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
	default:
		result = fmt.Sprintf("%04d", d.Year)
	}

	if d.IsRange && d.EndYear > 0 {
		var end string
		switch d.Precision {
		case hubv1.DatePrecision_DATE_PRECISION_YEAR:
			end = fmt.Sprintf("%04d", d.EndYear)
		case hubv1.DatePrecision_DATE_PRECISION_MONTH:
			end = fmt.Sprintf("%04d-%02d", d.EndYear, d.EndMonth)
		case hubv1.DatePrecision_DATE_PRECISION_DAY:
			end = fmt.Sprintf("%04d-%02d-%02d", d.EndYear, d.EndMonth, d.EndDay)
		default:
			end = fmt.Sprintf("%04d", d.EndYear)
		}
		result = result + "/" + end
	}

	switch d.Qualifier {
	case hubv1.DateQualifier_DATE_QUALIFIER_APPROXIMATE:
		result = result + "~"
	case hubv1.DateQualifier_DATE_QUALIFIER_UNCERTAIN:
		result = result + "?"
	case hubv1.DateQualifier_DATE_QUALIFIER_BOTH:
		result = result + "%"
	}

	return result
}

// DateToTime converts the DateValue to a time.Time.
func DateToTime(d *hubv1.DateValue) time.Time {
	if d.Year == 0 {
		return time.Time{}
	}
	month := int(d.Month)
	if month == 0 {
		month = 1
	}
	day := int(d.Day)
	if day == 0 {
		day = 1
	}
	return time.Date(int(d.Year), time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// NewDateFromYear creates a DateValue from a year.
func NewDateFromYear(year int32, dateType hubv1.DateType) *hubv1.DateValue {
	return &hubv1.DateValue{
		Type:      dateType,
		Year:      year,
		Precision: hubv1.DatePrecision_DATE_PRECISION_YEAR,
	}
}

// NewDateFromYearMonth creates a DateValue from year and month.
func NewDateFromYearMonth(year, month int32, dateType hubv1.DateType) *hubv1.DateValue {
	return &hubv1.DateValue{
		Type:      dateType,
		Year:      year,
		Month:     month,
		Precision: hubv1.DatePrecision_DATE_PRECISION_MONTH,
	}
}

// NewDateFromYMD creates a DateValue from year, month, and day.
func NewDateFromYMD(year, month, day int32, dateType hubv1.DateType) *hubv1.DateValue {
	return &hubv1.DateValue{
		Type:      dateType,
		Year:      year,
		Month:     month,
		Day:       day,
		Precision: hubv1.DatePrecision_DATE_PRECISION_DAY,
	}
}
