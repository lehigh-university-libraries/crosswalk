// Package proquestv1 contains ProQuest ETD format specific types and computed fields.
package proquestv1

import (
	"log/slog"
	"time"

	"google.golang.org/protobuf/proto"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	proquestv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/proquest/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub/convert"
)

func init() {
	// Register ProQuest computed fields with the default registry
	convert.RegisterComputedField("spoke.proquest.v1.Submission", ComputeEmbargoDate)
}

// ComputeEmbargoDate computes the embargo end date from a ProQuest submission.
// The embargo date is determined by:
// 1. Explicit embargo date from Repository.Embargo (if present)
// 2. Computed from embargo_code + accept_date
//
// The computed date is added to the Hub record as DATE_TYPE_AVAILABLE.
func ComputeEmbargoDate(source proto.Message, record *hubv1.Record) error {
	submission, ok := source.(*proquestv1.Submission)
	if !ok {
		return nil // Not a ProQuest submission, skip
	}

	embargoDate := computeEmbargoDateFromSubmission(submission)
	if embargoDate == "" {
		return nil // No embargo
	}

	// Add the embargo end date as DATE_TYPE_AVAILABLE
	record.Dates = append(record.Dates, &hubv1.DateValue{
		Type:      hubv1.DateType_DATE_TYPE_AVAILABLE,
		Raw:       embargoDate,
		Year:      parseYear(embargoDate),
		Month:     parseMonth(embargoDate),
		Day:       parseDay(embargoDate),
		Precision: hubv1.DatePrecision_DATE_PRECISION_DAY,
	})

	return nil
}

// computeEmbargoDateFromSubmission extracts or computes the embargo date.
func computeEmbargoDateFromSubmission(submission *proquestv1.Submission) string {
	// First, check for explicit embargo date in Repository
	if submission.Repository != nil && submission.Repository.Embargo != "" {
		// The Repository.Embargo field contains the explicit delayed_release date
		embargoUntil := extractEmbargoDate(submission.Repository.Embargo)
		if embargoUntil != "" {
			return embargoUntil
		}
	}

	// Compute from embargo_code + accept_date
	if submission.Description != nil && submission.Description.Dates != nil {
		return computeEmbargoFromCode(
			int(submission.EmbargoCode),
			submission.Description.Dates.AcceptDate,
		)
	}

	return ""
}

// extractEmbargoDate extracts a date from the repository embargo field.
// The embargo field may contain a date string in various formats.
func extractEmbargoDate(embargoField string) string {
	if embargoField == "" {
		return ""
	}

	// Try common date formats
	formats := []string{
		"2006-01-02",
		"01/02/2006",
		"January 2, 2006",
		"Jan 2, 2006",
		"2006/01/02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, embargoField); err == nil {
			return t.Format("2006-01-02")
		}
	}

	// If no format matches, return the original value
	// (it may be a date string we don't recognize but is still valid)
	return embargoField
}

// computeEmbargoFromCode computes the embargo end date from embargo code and accept date.
//
// Embargo codes:
//   - 0: No embargo
//   - 1: 6 months
//   - 2: 12 months (1 year)
//   - 3: 24 months (2 years)
func computeEmbargoFromCode(embargoCode int, acceptDate string) string {
	if embargoCode == 0 {
		return ""
	}

	if acceptDate == "" {
		slog.Warn("Cannot compute embargo date: missing accept_date", "embargo_code", embargoCode)
		return ""
	}

	// Parse the accept date (ProQuest uses MM/DD/YYYY format)
	acceptTime, err := time.Parse("01/02/2006", acceptDate)
	if err != nil {
		// Try alternative formats
		altFormats := []string{
			"2006-01-02",
			"2006/01/02",
		}
		for _, format := range altFormats {
			if t, err := time.Parse(format, acceptDate); err == nil {
				acceptTime = t
				break
			}
		}
		if acceptTime.IsZero() {
			slog.Error("Invalid accept_date format", "date", acceptDate, "error", err)
			return ""
		}
	}

	// Compute embargo duration based on code
	// Using 30-day months for consistency with original implementation
	var embargoDuration time.Duration
	switch embargoCode {
	case 1:
		embargoDuration = 6 * 30 * 24 * time.Hour // 6 months
	case 2:
		embargoDuration = 12 * 30 * 24 * time.Hour // 12 months
	case 3:
		embargoDuration = 24 * 30 * 24 * time.Hour // 24 months
	default:
		slog.Warn("Unknown embargo code", "embargo_code", embargoCode)
		return ""
	}

	embargoEndDate := acceptTime.Add(embargoDuration)
	return embargoEndDate.Format("2006-01-02")
}

// parseYear extracts the year from a YYYY-MM-DD date string.
func parseYear(date string) int32 {
	if len(date) < 4 {
		return 0
	}
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	return int32(t.Year())
}

// parseMonth extracts the month from a YYYY-MM-DD date string.
func parseMonth(date string) int32 {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	return int32(t.Month())
}

// parseDay extracts the day from a YYYY-MM-DD date string.
func parseDay(date string) int32 {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	return int32(t.Day())
}
