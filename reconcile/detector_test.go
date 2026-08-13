package reconcile

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

type recordingFinder struct {
	identifierCandidates []Candidate
	metadataCandidates   []Candidate
	err                  map[QueryStrategy]error
	queries              []Query
}

func TestDetectorRejectsNilContext(t *testing.T) {
	t.Parallel()
	_, err := (Detector{}).Detect(nil, nil, ModeAssumeNew)
	if err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("Detect(nil) error = %v", err)
	}
}

func TestDetectorDerivesRegistryAndPreservesProfileProvenance(t *testing.T) {
	t.Parallel()
	provenance := ReportProvenance{
		System: "drupal", ProfileName: "repository-article",
		ProfileFingerprint: strings.Repeat("a", 64), ModelFingerprint: strings.Repeat("b", 64),
	}
	report, err := (Detector{Provenance: provenance}).Detect(context.Background(), nil, ModeAssumeNew)
	if err != nil {
		t.Fatal(err)
	}
	if report.Provenance.System != provenance.System || report.Provenance.ProfileName != provenance.ProfileName || report.Provenance.ProfileFingerprint != provenance.ProfileFingerprint || report.Provenance.ModelFingerprint != provenance.ModelFingerprint {
		t.Fatalf("profile provenance = %#v, want %#v", report.Provenance, provenance)
	}
	if report.Provenance.IdentifierRegistryVersion == "" || report.Provenance.IdentifierRegistryDigest == "" {
		t.Fatalf("registry provenance was not derived: %#v", report.Provenance)
	}
}

func TestDetectorRejectsMismatchedOrIncompleteProvenance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		provenance ReportProvenance
		contains   string
	}{
		{name: "registry digest", provenance: ReportProvenance{IdentifierRegistryDigest: strings.Repeat("0", 64)}, contains: "digest does not match"},
		{name: "incomplete profile", provenance: ReportProvenance{System: "drupal"}, contains: "must be supplied together"},
		{name: "invalid profile digest", provenance: ReportProvenance{System: "drupal", ProfileName: "article", ProfileFingerprint: "not-a-digest", ModelFingerprint: strings.Repeat("b", 64)}, contains: "profile fingerprint"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := (Detector{Provenance: test.provenance}).Detect(context.Background(), nil, ModeAssumeNew)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("Detect() error = %v, want substring %q", err, test.contains)
			}
		})
	}
}

func (finder *recordingFinder) Candidates(_ context.Context, query Query) ([]Candidate, error) {
	finder.queries = append(finder.queries, query)
	if err := finder.err[query.Strategy]; err != nil {
		return nil, err
	}
	switch query.Strategy {
	case QueryByIdentifier:
		return append([]Candidate(nil), finder.identifierCandidates...), nil
	case QueryByMetadata:
		return append([]Candidate(nil), finder.metadataCandidates...), nil
	default:
		return nil, errors.New("missing query strategy")
	}
}

func TestDetectorUsesIdentifierFirstAndStopsOnExactMatch(t *testing.T) {
	t.Parallel()
	incoming := testRecord(
		"A stable title for exact identifier detection", 2024, []string{"Doe, Jane"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1000/ABC"),
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN, "1234-567X"),
	)
	existing := testRecord(
		"A stable title for exact identifier detection", 2023, []string{"Jane Doe"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "https://doi.org/10.1000/abc"),
	)
	finder := &recordingFinder{identifierCandidates: []Candidate{{RepositoryID: "node-7", Record: existing}}}

	report, err := (Detector{Finder: finder}).Detect(context.Background(), []Input{{Key: "row-1", Record: incoming}}, ModeHold)
	if err != nil {
		t.Fatal(err)
	}
	if len(finder.queries) != 1 || finder.queries[0].Strategy != QueryByIdentifier {
		t.Fatalf("queries = %#v, want one identifier query", finder.queries)
	}
	if got, want := finder.queries[0].Identifiers, []IdentifierKey{builtinIdentifierKey(t, "doi", "10.1000/abc")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("identifier query = %#v, want only strong identifiers %#v", got, want)
	}
	if report.Results[0].Verdict != VerdictDuplicate || report.Summary.Duplicate != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestDetectorFallsBackToMetadataInOrder(t *testing.T) {
	t.Parallel()
	incoming := testRecord(
		"A sufficiently distinctive title for metadata reconciliation", 2024, []string{"Doe, Jane"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1000/new"),
	)
	existing := testRecord(
		"A sufficiently distinctive title for metadata reconciliation", 2024, []string{"Jane Doe"},
	)
	finder := &recordingFinder{metadataCandidates: []Candidate{{RepositoryID: "node-8", Record: existing}}}

	report, err := (Detector{Finder: finder}).Detect(context.Background(), []Input{{Record: incoming}}, ModeSkip)
	if err != nil {
		t.Fatal(err)
	}
	if len(finder.queries) != 2 || finder.queries[0].Strategy != QueryByIdentifier || finder.queries[1].Strategy != QueryByMetadata {
		t.Fatalf("query order = %#v, want identifier then metadata", finder.queries)
	}
	if report.Results[0].InputKey != "record-1" || report.Results[0].Verdict != VerdictReview {
		t.Fatalf("unexpected result: %#v", report.Results[0])
	}
}

func TestDetectorMultipleExactCandidatesAreAmbiguousAndDeterministic(t *testing.T) {
	t.Parallel()
	id := identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1000/same")
	incoming := testRecord("A shared title for duplicate candidate ordering", 2024, nil, id)
	first := Candidate{RepositoryID: "10", UUID: "bbbb", Record: testRecord("A shared title for duplicate candidate ordering", 2024, nil, id)}
	second := Candidate{RepositoryID: "2", UUID: "aaaa", Record: testRecord("A shared title for duplicate candidate ordering", 2024, nil, id)}

	detect := func(candidates []Candidate) Report {
		t.Helper()
		report, err := (Detector{Finder: &recordingFinder{identifierCandidates: candidates}}).Detect(
			context.Background(), []Input{{Key: "input", Record: incoming}}, ModeHold,
		)
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	left := detect([]Candidate{first, second})
	right := detect([]Candidate{second, first})
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("reports differ with Finder order:\nleft=%#v\nright=%#v", left, right)
	}
	if left.Results[0].Verdict != VerdictAmbiguous || len(left.Results[0].Matches) != 2 {
		t.Fatalf("unexpected ambiguous result: %#v", left.Results[0])
	}
}

func TestDetectorDeduplicatesFinderCandidatesByAnyStableLocator(t *testing.T) {
	t.Parallel()
	id := identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1000/same")
	record := testRecord("A shared title for duplicate candidate collapse", 2024, nil, id)
	candidates := []Candidate{
		{RepositoryID: "22", UUID: "same-uuid", Record: record},
		{UUID: "same-uuid", URL: "https://example.test/node/22", Record: record},
		{RepositoryID: "22", URL: "https://example.test/node/22", Record: record},
	}
	report, err := (Detector{Finder: &recordingFinder{identifierCandidates: candidates}}).Detect(
		context.Background(), []Input{{Record: record}}, ModeHold,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(report.Results[0].Matches); got != 1 {
		t.Fatalf("match count = %d, want 1: %#v", got, report.Results[0].Matches)
	}
}

func TestDetectorSelectsDuplicateLocatorCandidateDeterministically(t *testing.T) {
	t.Parallel()

	id := identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1000/same-locator")
	incoming := testRecord("A shared title for deterministic candidate collapse", 2024, nil, id)
	english := testRecord("A shared title for deterministic candidate collapse", 2024, nil, id)
	english.Language = "en"
	french := testRecord("A shared title for deterministic candidate collapse", 2024, nil, id)
	french.Language = "fr"
	first := Candidate{RepositoryID: "22", Record: english}
	second := Candidate{RepositoryID: "22", Record: french}

	detect := func(candidates []Candidate) Report {
		t.Helper()
		report, err := (Detector{Finder: &recordingFinder{identifierCandidates: candidates}}).Detect(
			context.Background(), []Input{{Key: "input", Record: incoming}}, ModeHold,
		)
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	left := detect([]Candidate{first, second})
	right := detect([]Candidate{second, first})
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("duplicate-locator reports depend on Finder order:\nleft=%#v\nright=%#v", left, right)
	}
	if got := left.Results[0].Matches[0].Candidate.Metadata.Language; got != "en" {
		t.Fatalf("deterministic candidate language = %q, want en", got)
	}
}

func TestDetectorAssumeNewSkipsFinderButDetectsBatchDuplicates(t *testing.T) {
	t.Parallel()
	idOne := identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV, "arXiv:2401.01234v1")
	idTwo := identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV, "https://arxiv.org/abs/2401.01234v3")
	inputs := []Input{
		{Key: "first", Record: testRecord("A new preprint title", 2024, []string{"Doe, Jane"}, idOne)},
		{Key: "second", Record: testRecord("A new preprint title", 2024, []string{"Doe, Jane"}, idTwo)},
	}
	finder := &recordingFinder{}
	report, err := (Detector{Finder: finder}).Detect(context.Background(), inputs, ModeAssumeNew)
	if err != nil {
		t.Fatal(err)
	}
	if len(finder.queries) != 0 {
		t.Fatalf("assume-new made repository queries: %#v", finder.queries)
	}
	if got, want := []Verdict{report.Results[0].Verdict, report.Results[1].Verdict}, []Verdict{VerdictNew, VerdictDuplicate}; !reflect.DeepEqual(got, want) {
		t.Fatalf("verdicts = %#v, want %#v", got, want)
	}
	if match := report.Results[1].Matches[0]; match.Candidate.Kind != CandidateBatch || match.Candidate.Key != "first" {
		t.Fatalf("unexpected batch match: %#v", match)
	}
}

func TestDetectorIndexesFuzzyBatchCandidatesWithoutDroppingReviewMatches(t *testing.T) {
	t.Parallel()
	inputs := []Input{
		{Key: "unrelated", Record: testRecord("A completely unrelated history of alpine botany", 1998, []string{"Smith, Alex"})},
		{Key: "first", Record: testRecord("Machine learning methods for identifying duplicate scholarly records", 2023, []string{"Doe, Jane"})},
		{Key: "second", Record: testRecord("Machine learning method for identifying duplicate scholarly records", 2023, []string{"Jane Doe"})},
	}
	report, err := (Detector{}).Detect(context.Background(), inputs, ModeAssumeNew)
	if err != nil {
		t.Fatal(err)
	}
	result := report.Results[2]
	if result.Verdict != VerdictReview || len(result.Matches) != 1 || result.Matches[0].Candidate.Key != "first" {
		t.Fatalf("indexed fuzzy result = %#v", result)
	}
}

func TestDetectorBatchCandidateLimitFailsClosed(t *testing.T) {
	t.Parallel()
	inputs := []Input{
		{Key: "one", Record: testRecord("The same sufficiently distinctive batch title", 2024, nil)},
		{Key: "two", Record: testRecord("The same sufficiently distinctive batch title", 2024, nil)},
		{Key: "three", Record: testRecord("The same sufficiently distinctive batch title", 2024, nil)},
	}
	_, err := (Detector{MaxBatchCandidates: 1}).Detect(context.Background(), inputs, ModeAssumeNew)
	if err == nil || !strings.Contains(err.Error(), "batch candidate set exceeds 1") {
		t.Fatalf("Detect() candidate-limit error = %v", err)
	}
}

func TestDetectorRejectsInvalidBatchCandidateLimit(t *testing.T) {
	t.Parallel()
	_, err := (Detector{MaxBatchCandidates: -1}).Detect(context.Background(), nil, ModeAssumeNew)
	if err == nil || !strings.Contains(err.Error(), "maximum batch candidates must be positive") {
		t.Fatalf("Detect() limit error = %v", err)
	}
}

func TestCustomRegistryIsUsedThroughoutReportAndPartition(t *testing.T) {
	t.Parallel()
	const namespace = "https://repository.example.edu/identifiers/accession/"
	policy, err := NewPolicy(hub.IdentifierRegistryConfig{
		Version: hub.IdentifierRegistryVersion,
		Rules: []hub.IdentifierRule{{
			Scheme: "example-accession", NamespaceURI: namespace,
			Type:                 hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
			DefaultIdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
			Pattern:              `^[A-Z]+-[0-9]+$`, Case: hub.IdentifierCaseUpper,
			ExactIdentityLevels: []hubv1.IdentifierIdentityLevel{hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	record := testRecord("A custom institutional identifier title", 2024, nil)
	record.Identifiers = []*hubv1.Identifier{{
		Scheme: "example-accession", NamespaceUri: namespace, Value: "abc-42",
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
	}}
	inputs := []Input{{Key: "row", Record: record}}
	report, err := (Detector{Policy: policy}).Detect(context.Background(), inputs, ModeAssumeNew)
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Results[0].Input.Identifiers[0].Value; got != "ABC-42" {
		t.Fatalf("report identifier = %q, want custom-registry canonical value", got)
	}
	if _, err := PartitionInputs(inputs, report); err == nil || !strings.Contains(err.Error(), "PartitionInputsWithPolicy") {
		t.Fatalf("PartitionInputs() custom-policy error = %v", err)
	}
	if _, err := PartitionInputsWithPolicy(inputs, report, policy); err != nil {
		t.Fatalf("PartitionInputsWithPolicy() error = %v", err)
	}
}

func TestDetectorPropagatesFinderFailure(t *testing.T) {
	t.Parallel()
	finder := &recordingFinder{err: map[QueryStrategy]error{QueryByMetadata: errors.New("repository unavailable")}}
	_, err := (Detector{Finder: finder}).Detect(
		context.Background(),
		[]Input{{Key: "row", Record: testRecord("A title long enough for a lookup", 2024, nil)}},
		ModeHold,
	)
	if err == nil || !strings.Contains(err.Error(), "metadata lookup") || !strings.Contains(err.Error(), "repository unavailable") {
		t.Fatalf("Detect() error = %v", err)
	}
}

func TestDetectorValidatesInputsAndFinder(t *testing.T) {
	t.Parallel()
	record := testRecord("A title long enough for input validation", 2024, nil)
	tests := []struct {
		name     string
		detector Detector
		inputs   []Input
		mode     Mode
		contains string
	}{
		{"finder required", Detector{}, []Input{{Record: record}}, ModeHold, "Finder is required"},
		{"nil record", Detector{}, []Input{{Record: nil}}, ModeAssumeNew, "nil record"},
		{"duplicate key", Detector{}, []Input{{Key: "same", Record: record}, {Key: "same", Record: record}}, ModeAssumeNew, "duplicate input key"},
		{"unknown mode", Detector{}, []Input{{Record: record}}, Mode("mystery"), "unsupported mode"},
		{"unknown policy", Detector{Policy: Policy{Version: "future"}}, []Input{{Record: record}}, ModeAssumeNew, "unsupported policy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := test.detector.Detect(context.Background(), test.inputs, test.mode)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("Detect() error = %v, want substring %q", err, test.contains)
			}
		})
	}
}

func TestPartitionInputsModes(t *testing.T) {
	t.Parallel()
	inputs := []Input{
		{Key: "new", Record: testRecord("A new title", 2024, nil)},
		{Key: "duplicate", Record: testRecord("A duplicate title", 2024, nil)},
		{Key: "review", Record: testRecord("A title needing review", 2024, nil)},
		{Key: "ambiguous", Record: testRecord("An ambiguous title", 2024, nil)},
	}
	results := []Result{
		{InputKey: "review", InputIndex: 2, Input: snapshot(inputs[2].Record), Verdict: VerdictReview, Matches: []Match{{Kind: MatchHeuristic, RequiresReview: true}}},
		{InputKey: "new", InputIndex: 0, Input: snapshot(inputs[0].Record), Verdict: VerdictNew},
		{InputKey: "ambiguous", InputIndex: 3, Input: snapshot(inputs[3].Record), Verdict: VerdictAmbiguous, Matches: []Match{{Kind: MatchHeuristic}, {Kind: MatchHeuristic}}},
		{InputKey: "duplicate", InputIndex: 1, Input: snapshot(inputs[1].Record), Verdict: VerdictDuplicate, Matches: []Match{{Kind: MatchExactIdentifier}}},
	}
	tests := []struct {
		mode           Mode
		accepted       []string
		held           []string
		skipped        []string
		reviewRequired bool
	}{
		{ModeHold, []string{"new"}, []string{"duplicate", "review", "ambiguous"}, nil, true},
		{ModeAssumeNew, []string{"new"}, []string{"duplicate", "review", "ambiguous"}, nil, true},
		{ModeSkip, []string{"new"}, []string{"review", "ambiguous"}, []string{"duplicate"}, true},
		{ModeForceNew, []string{"new", "duplicate", "review", "ambiguous"}, nil, nil, false},
	}
	for _, test := range tests {
		t.Run(string(test.mode), func(t *testing.T) {
			t.Parallel()
			summary := Summary{Total: 4, New: 1, Duplicate: 1, Review: 1, Ambiguous: 1}
			partition, err := PartitionInputs(inputs, Report{
				Version: ReportVersion, PolicyVersion: PolicyVersion1,
				Provenance: testReportProvenance(), Mode: test.mode, Results: results, Summary: summary,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := inputKeys(partition.Accepted); !reflect.DeepEqual(got, test.accepted) {
				t.Errorf("accepted = %#v, want %#v", got, test.accepted)
			}
			if got := inputKeys(partition.Held); !reflect.DeepEqual(got, test.held) {
				t.Errorf("held = %#v, want %#v", got, test.held)
			}
			if got := inputKeys(partition.Skipped); !reflect.DeepEqual(got, test.skipped) {
				t.Errorf("skipped = %#v, want %#v", got, test.skipped)
			}
			if partition.ReviewRequired != test.reviewRequired {
				t.Errorf("ReviewRequired = %v, want %v", partition.ReviewRequired, test.reviewRequired)
			}
		})
	}
}

func TestPartitionInputsRejectsMalformedReport(t *testing.T) {
	t.Parallel()
	inputs := []Input{
		{Key: "one", Record: testRecord("First input title", 2024, nil)},
		{Key: "two", Record: testRecord("Second input title", 2024, nil)},
	}
	_, err := PartitionInputs(inputs, Report{
		Version: ReportVersion, PolicyVersion: PolicyVersion1, Provenance: testReportProvenance(), Mode: ModeHold,
		Results: []Result{
			{InputKey: "one", InputIndex: 0, Input: snapshot(inputs[0].Record), Verdict: VerdictNew},
			{InputKey: "one", InputIndex: 0, Input: snapshot(inputs[0].Record), Verdict: VerdictNew},
		},
		Summary: Summary{Total: 2, New: 2},
	})
	if err == nil || !(strings.Contains(err.Error(), "duplicate input index") || strings.Contains(err.Error(), "repeats input index")) {
		t.Fatalf("PartitionInputs() error = %v", err)
	}
}

func TestPartitionInputsRejectsStaleInput(t *testing.T) {
	t.Parallel()
	record := testRecord("Original title for reconciliation", 2024, nil)
	report, err := (Detector{}).Detect(context.Background(), []Input{{Key: "row", Record: record}}, ModeAssumeNew)
	if err != nil {
		t.Fatal(err)
	}
	record.Title = "Changed after reconciliation"
	_, err = PartitionInputs([]Input{{Key: "row", Record: record}}, report)
	if err == nil || !strings.Contains(err.Error(), "metadata changed") {
		t.Fatalf("PartitionInputs() error = %v, want stale-input error", err)
	}
}

func inputKeys(inputs []Input) []string {
	if len(inputs) == 0 {
		return nil
	}
	result := make([]string, len(inputs))
	for index, input := range inputs {
		result[index] = input.Key
	}
	return result
}
