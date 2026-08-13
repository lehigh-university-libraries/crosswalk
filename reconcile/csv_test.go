package reconcile

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestSafeSpreadsheetCell(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"=1+1":         "'=1+1",
		" +SUM(A1:A2)": "' +SUM(A1:A2)",
		"-4":           "'-4",
		"@cmd":         "'@cmd",
		"\tformula":    "'\tformula",
		"\rformula":    "'\rformula",
		"\nformula":    "'\nformula",
		"\ufeff=1":     "'\ufeff=1",
		"ordinary":     "ordinary",
		"":             "",
	}
	for input, want := range tests {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if got := SafeSpreadsheetCell(input); got != want {
				t.Fatalf("SafeSpreadsheetCell(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestReviewCSVIsDeterministicAndFormulaSafe(t *testing.T) {
	t.Parallel()
	report := Report{
		Version: ReportVersion, PolicyVersion: PolicyVersion1, Provenance: testReportProvenance(), Mode: ModeHold,
		Results: []Result{{
			InputKey: "=input", InputIndex: 0, Verdict: VerdictReview,
			Input: Snapshot{
				Title: " +SUM(A1:A2)", Authors: []string{"@author"}, Year: 2024,
				Identifiers: []IdentifierKey{{Scheme: "local", Value: "-identifier"}},
			},
			Matches: []Match{{
				Kind: MatchHeuristic, Score: 90, Confidence: "high", RequiresReview: true,
				Candidate: CandidateRef{
					Kind: CandidateRepository, RepositoryID: "22", URL: "=HYPERLINK(\"bad\")",
					Metadata: Snapshot{Title: "@candidate", Authors: []string{"-candidate author"}, Year: 2024},
				},
				Evidence:    []Evidence{{Code: "title_exact", Field: "title", Incoming: "=bad", Weight: 50}},
				Differences: []Difference{{Field: "publisher", Incoming: "+bad", Kind: DifferenceIncomingOnly}},
			}},
		}},
		Summary: Summary{Total: 1, Review: 1},
	}
	report.Provenance.System = "drupal"
	report.Provenance.ProfileName = "repository-article"
	report.Provenance.ProfileFingerprint = strings.Repeat("a", 64)
	report.Provenance.ModelFingerprint = strings.Repeat("b", 64)

	first, err := ReviewCSV(report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReviewCSV(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("CSV output is not deterministic:\n%s\n%s", first, second)
	}
	rows, err := csv.NewReader(bytes.NewReader(first)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(rows[1]) != len(reviewCSVHeader) {
		t.Fatalf("unexpected CSV dimensions: %#v", rows)
	}
	header := make(map[string]int, len(rows[0]))
	for index, value := range rows[0] {
		header[value] = index
	}
	for field, wantPrefix := range map[string]string{
		"input_key":         "'=",
		"url":               "'=",
		"input_title":       "' +",
		"candidate_title":   "'@",
		"input_authors":     "'@",
		"candidate_authors": "'-",
	} {
		if got := rows[1][header[field]]; !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("%s = %q, want prefix %q", field, got, wantPrefix)
		}
	}
	for field, want := range map[string]string{
		"identifier_registry_version": report.Provenance.IdentifierRegistryVersion,
		"identifier_registry_digest":  report.Provenance.IdentifierRegistryDigest,
		"system":                      "drupal",
		"profile_name":                "repository-article",
		"profile_fingerprint":         report.Provenance.ProfileFingerprint,
		"model_fingerprint":           report.Provenance.ModelFingerprint,
	} {
		if got := rows[1][header[field]]; got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	if got := rows[1][header["evidence"]]; !strings.HasPrefix(got, "[") || !strings.Contains(got, `"incoming":"=bad"`) {
		t.Errorf("evidence cell = %q, want JSON", got)
	}
	if got := rows[1][header["recommended_action"]]; got != "review existing item; update it or force new" {
		t.Errorf("recommended action = %q", got)
	}
}

func TestReviewCSVWritesNewRecordWithoutCandidate(t *testing.T) {
	t.Parallel()
	report := Report{
		Version: ReportVersion, PolicyVersion: PolicyVersion1, Provenance: testReportProvenance(), Mode: ModeAssumeNew,
		Results: []Result{{InputKey: "new", Verdict: VerdictNew, Input: Snapshot{Title: "New work"}}},
		Summary: Summary{Total: 1, New: 1},
	}
	encoded, err := ReviewCSV(report)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(encoded)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	header := make(map[string]int, len(rows[0]))
	for index, value := range rows[0] {
		header[value] = index
	}
	if got := rows[1][header["candidate_kind"]]; got != "" {
		t.Fatalf("candidate kind = %q, want blank", got)
	}
	if got := rows[1][header["recommended_action"]]; got != "create" {
		t.Fatalf("recommended action = %q, want create", got)
	}
}

func TestWriteReviewCSVReportsWriterFailure(t *testing.T) {
	t.Parallel()
	err := WriteReviewCSV(errorWriter{}, Report{Version: ReportVersion, PolicyVersion: PolicyVersion1, Provenance: testReportProvenance(), Mode: ModeHold})
	if err == nil || !strings.Contains(err.Error(), "CSV") {
		t.Fatalf("WriteReviewCSV() error = %v", err)
	}
}

func TestReviewCSVHeaderIsStable(t *testing.T) {
	t.Parallel()
	encoded, err := ReviewCSV(Report{Version: ReportVersion, PolicyVersion: PolicyVersion1, Provenance: testReportProvenance(), Mode: ModeHold})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(encoded)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows[0], reviewCSVHeader) {
		t.Fatalf("header = %#v, want %#v", rows[0], reviewCSVHeader)
	}
}

func TestReviewCSVRejectsMissingOrMalformedProvenance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		provenance ReportProvenance
		contains   string
	}{
		{name: "missing", contains: "registry version"},
		{name: "unsupported registry", provenance: ReportProvenance{IdentifierRegistryVersion: "future", IdentifierRegistryDigest: strings.Repeat("a", 64)}, contains: "unsupported identifier registry version"},
		{name: "bad registry digest", provenance: ReportProvenance{IdentifierRegistryVersion: "1", IdentifierRegistryDigest: "BAD"}, contains: "registry digest"},
		{name: "partial profile", provenance: func() ReportProvenance {
			value := testReportProvenance()
			value.System = "drupal"
			return value
		}(), contains: "must be supplied together"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ReviewCSV(Report{Version: ReportVersion, PolicyVersion: PolicyVersion1, Provenance: test.provenance, Mode: ModeHold})
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("ReviewCSV() error = %v, want substring %q", err, test.contains)
			}
		})
	}
}

func TestReviewCSVRejectsTamperedDecisionContract(t *testing.T) {
	t.Parallel()

	base := Report{
		Version: ReportVersion, PolicyVersion: PolicyVersion1, Provenance: testReportProvenance(), Mode: ModeHold,
		Results: []Result{{InputKey: "row", InputIndex: 0, Verdict: VerdictNew}},
		Summary: Summary{Total: 1, New: 1},
	}
	tests := []struct {
		name     string
		mutate   func(*Report)
		contains string
	}{
		{name: "missing mode", mutate: func(report *Report) { report.Mode = "" }, contains: "mode is required"},
		{name: "summary mismatch", mutate: func(report *Report) { report.Summary.New = 0 }, contains: "summary does not match"},
		{name: "duplicate index", mutate: func(report *Report) {
			report.Results = append(report.Results, Result{InputKey: "row-2", InputIndex: 0, Verdict: VerdictNew})
			report.Summary = Summary{Total: 2, New: 2}
		}, contains: "repeats input index"},
		{name: "out of range index", mutate: func(report *Report) { report.Results[0].InputIndex = 7 }, contains: "invalid input index"},
		{name: "new with match", mutate: func(report *Report) {
			report.Results[0].Matches = []Match{{Kind: MatchHeuristic}}
		}, contains: "must not contain matches"},
		{name: "review without match", mutate: func(report *Report) {
			report.Results[0].Verdict = VerdictReview
			report.Summary = Summary{Total: 1, Review: 1}
		}, contains: "must contain at least one match"},
		{name: "duplicate with heuristic match", mutate: func(report *Report) {
			report.Results[0].Verdict = VerdictDuplicate
			report.Results[0].Matches = []Match{{Kind: MatchHeuristic, Score: 99}}
			report.Summary = Summary{Total: 1, Duplicate: 1}
		}, contains: "review-free exact match"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			report := base
			report.Results = append([]Result(nil), base.Results...)
			test.mutate(&report)
			_, err := ReviewCSV(report)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("ReviewCSV() error = %v, want containing %q", err, test.contains)
			}
		})
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

var _ io.Writer = errorWriter{}
