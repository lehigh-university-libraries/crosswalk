package reconcile

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

var reviewCSVHeader = []string{
	"report_version", "policy_version", "identifier_registry_version", "identifier_registry_digest",
	"system", "profile_name", "profile_fingerprint", "model_fingerprint",
	"mode", "input_key", "input_index",
	"verdict", "match_kind", "score", "confidence", "candidate_kind",
	"candidate_key", "repository_id", "uuid", "url", "input_title",
	"candidate_title", "input_authors", "candidate_authors", "input_year",
	"candidate_year", "input_identifiers", "candidate_identifiers", "evidence",
	"differences", "recommended_action",
}

// WriteReviewCSV writes one row per candidate match, or one candidate-less row
// for a new record. All dynamic cells are protected against spreadsheet formula
// execution and structured evidence is encoded as JSON within its CSV cell.
func WriteReviewCSV(writer io.Writer, report Report) error {
	if writer == nil {
		return fmt.Errorf("reconcile: CSV writer is nil")
	}
	if err := validateReportContract(report); err != nil {
		return err
	}
	mode := normalizedMode(report.Mode)
	if err := ValidateMode(mode); err != nil {
		return err
	}

	csvWriter := csv.NewWriter(writer)
	if err := csvWriter.Write(reviewCSVHeader); err != nil {
		return fmt.Errorf("reconcile: write CSV header: %w", err)
	}
	for _, result := range report.Results {
		if len(result.Matches) == 0 {
			if err := csvWriter.Write(reviewCSVRow(report, mode, result, nil)); err != nil {
				return fmt.Errorf("reconcile: write CSV row for %q: %w", result.InputKey, err)
			}
			continue
		}
		for index := range result.Matches {
			if err := csvWriter.Write(reviewCSVRow(report, mode, result, &result.Matches[index])); err != nil {
				return fmt.Errorf("reconcile: write CSV row for %q: %w", result.InputKey, err)
			}
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("reconcile: flush CSV: %w", err)
	}
	return nil
}

// ReviewCSV returns the deterministic CSV encoding of report.
func ReviewCSV(report Report) ([]byte, error) {
	var buffer bytes.Buffer
	if err := WriteReviewCSV(&buffer, report); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// SafeSpreadsheetCell prevents values beginning with spreadsheet formula
// sigils from being interpreted as formulas. Leading whitespace and a UTF-8 BOM
// are ignored for detection but retained in the escaped cell.
func SafeSpreadsheetCell(value string) string {
	probe := strings.TrimPrefix(value, "\ufeff")
	if probe != "" {
		switch probe[0] {
		case '\t', '\r', '\n':
			return "'" + value
		}
	}
	probe = strings.TrimLeftFunc(probe, unicode.IsSpace)
	if probe == "" {
		return value
	}
	switch probe[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func reviewCSVRow(report Report, mode Mode, result Result, match *Match) []string {
	cells := map[string]string{
		"report_version":              report.Version,
		"policy_version":              report.PolicyVersion,
		"identifier_registry_version": report.Provenance.IdentifierRegistryVersion,
		"identifier_registry_digest":  report.Provenance.IdentifierRegistryDigest,
		"system":                      report.Provenance.System,
		"profile_name":                report.Provenance.ProfileName,
		"profile_fingerprint":         report.Provenance.ProfileFingerprint,
		"model_fingerprint":           report.Provenance.ModelFingerprint,
		"mode":                        string(mode),
		"input_key":                   result.InputKey,
		"input_index":                 strconv.Itoa(result.InputIndex),
		"verdict":                     string(result.Verdict),
		"match_kind":                  "",
		"score":                       "",
		"confidence":                  "",
		"candidate_kind":              "",
		"candidate_key":               "",
		"repository_id":               "",
		"uuid":                        "",
		"url":                         "",
		"input_title":                 preferredTitle(result.Input),
		"candidate_title":             "",
		"input_authors":               strings.Join(result.Input.Authors, "; "),
		"candidate_authors":           "",
		"input_year":                  formatYear(result.Input.Year),
		"candidate_year":              "",
		"input_identifiers":           formatIdentifiers(result.Input.Identifiers),
		"candidate_identifiers":       "",
		"evidence":                    "[]",
		"differences":                 "[]",
		"recommended_action":          recommendedAction(result.Verdict),
	}
	if match != nil {
		cells["match_kind"] = string(match.Kind)
		cells["score"] = strconv.Itoa(match.Score)
		cells["confidence"] = match.Confidence
		cells["candidate_kind"] = string(match.Candidate.Kind)
		cells["candidate_key"] = match.Candidate.Key
		cells["repository_id"] = match.Candidate.RepositoryID
		cells["uuid"] = match.Candidate.UUID
		cells["url"] = match.Candidate.URL
		cells["candidate_title"] = preferredTitle(match.Candidate.Metadata)
		cells["candidate_authors"] = strings.Join(match.Candidate.Metadata.Authors, "; ")
		cells["candidate_year"] = formatYear(match.Candidate.Metadata.Year)
		cells["candidate_identifiers"] = formatIdentifiers(match.Candidate.Metadata.Identifiers)
		cells["evidence"] = mustJSON(match.Evidence)
		cells["differences"] = mustJSON(match.Differences)
	}

	row := make([]string, len(reviewCSVHeader))
	for index := range row {
		row[index] = SafeSpreadsheetCell(cells[reviewCSVHeader[index]])
	}
	return row
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		// All report values are JSON-safe structs. Preserve a valid CSV contract
		// if that invariant changes instead of silently emitting partial JSON.
		return fmt.Sprintf(`[{"code":"encoding_error","message":%q}]`, err.Error())
	}
	return string(encoded)
}

func recommendedAction(verdict Verdict) string {
	switch verdict {
	case VerdictNew:
		return "create"
	case VerdictDuplicate:
		return "skip or review metadata update"
	case VerdictReview:
		return "review existing item; update it or force new"
	case VerdictAmbiguous:
		return "review multiple existing items"
	default:
		return "review"
	}
}
