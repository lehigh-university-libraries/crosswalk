// Package reconcile detects likely duplicate repository records without making
// any repository changes. It keeps matching policy separate from candidate
// retrieval so callers can use Drupal, a fixture, or another record store.
package reconcile

import (
	"context"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

const (
	// ReportVersion is the serialization contract for Report and its CSV form.
	ReportVersion = "2"
	// PolicyVersion1 identifies the initial conservative matching policy.
	PolicyVersion1 = "1"
)

var (
	reportSystemPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	reportProfilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
)

// Mode controls how a reconciliation report is partitioned for a later ingest.
// Detection itself is unchanged except that ModeAssumeNew avoids repository
// lookups while continuing to detect duplicates within the input batch.
type Mode string

const (
	ModeHold      Mode = "hold"
	ModeSkip      Mode = "skip"
	ModeForceNew  Mode = "force-new"
	ModeAssumeNew Mode = "assume-new"
)

// Verdict is the result of comparing one input record with its candidates.
type Verdict string

const (
	VerdictNew       Verdict = "new"
	VerdictDuplicate Verdict = "duplicate"
	VerdictReview    Verdict = "review"
	VerdictAmbiguous Verdict = "ambiguous"
)

// CandidateKind identifies whether a possible match came from the repository
// or from an earlier record in the same input batch.
type CandidateKind string

const (
	CandidateRepository CandidateKind = "repository"
	CandidateBatch      CandidateKind = "batch"
)

// QueryStrategy tells a Finder which retrieval pass is being performed.
// Identifier lookups always precede metadata lookups.
type QueryStrategy string

const (
	QueryByIdentifier QueryStrategy = "identifier"
	QueryByMetadata   QueryStrategy = "metadata"
)

// MatchKind identifies the evidence class that made a candidate reportable.
type MatchKind string

const (
	MatchExactIdentifier MatchKind = "exact_identifier"
	MatchHeuristic       MatchKind = "heuristic"
)

// DifferenceKind describes which side of a metadata comparison has a value.
type DifferenceKind string

const (
	DifferenceChanged      DifferenceKind = "changed"
	DifferenceIncomingOnly DifferenceKind = "incoming_only"
	DifferenceExistingOnly DifferenceKind = "existing_only"
)

// Policy selects a versioned, deterministic matching policy. Callers should
// persist the version with reports so decisions can be reproduced later.
type Policy struct {
	Version            string                       `json:"version"`
	IdentifierRegistry hub.IdentifierRegistryConfig `json:"identifier_registry"`
}

// NewPolicy returns PolicyV1 with a caller-supplied identifier registry. The
// registry configuration is copied so later caller mutations cannot change a
// reconciliation run.
func NewPolicy(config hub.IdentifierRegistryConfig) (Policy, error) {
	policy := Policy{Version: PolicyVersion1, IdentifierRegistry: cloneIdentifierRegistryConfig(config)}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

// PolicyV1 returns the initial conservative policy. Metadata-only matches are
// candidates for human review and never automatic duplicates under this policy.
func PolicyV1() Policy {
	return Policy{
		Version:            PolicyVersion1,
		IdentifierRegistry: hub.IdentifierRegistryConfig{Version: hub.IdentifierRegistryVersion},
	}
}

// Validate reports whether the policy version is supported. A zero Policy is
// accepted and resolves to PolicyV1 when used by Detector or Compare.
func (p Policy) Validate() error {
	if p.Version == "" || p.Version == PolicyVersion1 {
		_, err := hub.NewIdentifierRegistry(p.IdentifierRegistry)
		if err != nil {
			return fmt.Errorf("reconcile: identifier registry: %w", err)
		}
		return nil
	}
	return fmt.Errorf("reconcile: unsupported policy version %q", p.Version)
}

func (p Policy) identifierRegistry() (*hub.IdentifierRegistry, error) {
	registry, err := hub.NewIdentifierRegistry(p.IdentifierRegistry)
	if err != nil {
		return nil, fmt.Errorf("reconcile: identifier registry: %w", err)
	}
	return registry, nil
}

func cloneIdentifierRegistryConfig(config hub.IdentifierRegistryConfig) hub.IdentifierRegistryConfig {
	cloned := config
	cloned.Rules = append([]hub.IdentifierRule(nil), config.Rules...)
	for index := range cloned.Rules {
		cloned.Rules[index].Aliases = append([]string(nil), config.Rules[index].Aliases...)
		cloned.Rules[index].Prefixes = append([]string(nil), config.Rules[index].Prefixes...)
		cloned.Rules[index].ExactIdentityLevels = append([]hubv1.IdentifierIdentityLevel(nil), config.Rules[index].ExactIdentityLevels...)
	}
	cloned.ExactPolicies = append([]hub.IdentifierExactIdentityPolicy(nil), config.ExactPolicies...)
	for index := range cloned.ExactPolicies {
		cloned.ExactPolicies[index].IdentityLevels = append([]hubv1.IdentifierIdentityLevel(nil), config.ExactPolicies[index].IdentityLevels...)
	}
	return cloned
}

// Input is a Hub record and its stable caller-supplied key. Blank keys are
// replaced deterministically with record-1, record-2, and so on.
type Input struct {
	Key    string
	Record *hubv1.Record
}

// IdentifierKey is a canonical, authority-scoped identifier suitable for lookup
// and comparison. Scheme names are lower-case (for example doi, arxiv, and
// scopus-eid). NamespaceURI prevents repository-local values from colliding
// across institutions.
type IdentifierKey struct {
	Scheme        string                        `json:"scheme"`
	NamespaceURI  string                        `json:"namespace_uri,omitempty"`
	Value         string                        `json:"value"`
	IdentityLevel hubv1.IdentifierIdentityLevel `json:"identity_level,omitempty"`
}

// Query contains normalized lookup material. A Finder must honor Strategy and
// should not broaden an identifier query into a metadata query on its own.
type Query struct {
	Strategy    QueryStrategy   `json:"strategy"`
	Key         string          `json:"key"`
	Identifiers []IdentifierKey `json:"identifiers,omitempty"`
	Title       string          `json:"title,omitempty"`
	Authors     []string        `json:"authors,omitempty"`
	Year        int32           `json:"year,omitempty"`
}

// Candidate is a possible existing work returned by a Finder or constructed
// from an earlier input. Repository candidates need at least one stable locator.
type Candidate struct {
	Kind         CandidateKind
	Key          string
	RepositoryID string
	UUID         string
	URL          string
	Record       *hubv1.Record
}

// Finder supplies read-only candidates. Detector calls identifier queries
// first and only performs a metadata query when no exact strong identifier was
// found. Implementations should return complete enough Hub records to compare.
type Finder interface {
	Candidates(context.Context, Query) ([]Candidate, error)
}

// Snapshot is the compact, deterministic metadata retained in a report.
type Snapshot struct {
	Title          string          `json:"title,omitempty"`
	FullTitle      string          `json:"full_title,omitempty"`
	Authors        []string        `json:"authors,omitempty"`
	Year           int32           `json:"year,omitempty"`
	Identifiers    []IdentifierKey `json:"identifiers,omitempty"`
	Publisher      string          `json:"publisher,omitempty"`
	Language       string          `json:"language,omitempty"`
	ResourceType   string          `json:"resource_type,omitempty"`
	AbstractLength int             `json:"abstract_length,omitempty"`
	AbstractSHA256 string          `json:"abstract_sha256,omitempty"`
}

// CandidateRef is the stable, report-safe representation of a Candidate.
type CandidateRef struct {
	Kind         CandidateKind `json:"kind"`
	Key          string        `json:"key,omitempty"`
	RepositoryID string        `json:"repository_id,omitempty"`
	UUID         string        `json:"uuid,omitempty"`
	URL          string        `json:"url,omitempty"`
	Metadata     Snapshot      `json:"metadata"`
}

// Evidence records one normalized comparison and its contribution to Score.
type Evidence struct {
	Code     string `json:"code"`
	Field    string `json:"field"`
	Incoming string `json:"incoming,omitempty"`
	Existing string `json:"existing,omitempty"`
	Weight   int    `json:"weight"`
}

// Difference describes a curated metadata difference for manual reconciliation.
type Difference struct {
	Field    string         `json:"field"`
	Incoming string         `json:"incoming,omitempty"`
	Existing string         `json:"existing,omitempty"`
	Kind     DifferenceKind `json:"kind"`
}

// Match is one reportable candidate. RequiresReview is set when even an exact
// identifier has serious contradictory metadata, such as a grossly different
// title or publisher, disjoint authors, a materially different year, or a
// different resource type.
type Match struct {
	Kind           MatchKind    `json:"kind"`
	Candidate      CandidateRef `json:"candidate"`
	Score          int          `json:"score"`
	Confidence     string       `json:"confidence"`
	RequiresReview bool         `json:"requires_review,omitempty"`
	Evidence       []Evidence   `json:"evidence,omitempty"`
	Differences    []Difference `json:"differences,omitempty"`
}

// Result is the decision for one input record.
type Result struct {
	InputKey   string   `json:"input_key"`
	InputIndex int      `json:"input_index"`
	Input      Snapshot `json:"input"`
	Verdict    Verdict  `json:"verdict"`
	Matches    []Match  `json:"matches,omitempty"`
}

// Summary contains report-wide verdict counts.
type Summary struct {
	Total     int `json:"total"`
	New       int `json:"new"`
	Duplicate int `json:"duplicate"`
	Review    int `json:"review"`
	Ambiguous int `json:"ambiguous"`
}

// ReportProvenance identifies the exact registry and optional immutable system
// profile used to produce a reconciliation report. Registry fields are always
// required; profile fields are either all populated or all empty.
type ReportProvenance struct {
	IdentifierRegistryVersion string `json:"identifier_registry_version"`
	IdentifierRegistryDigest  string `json:"identifier_registry_digest"`
	System                    string `json:"system,omitempty"`
	ProfileName               string `json:"profile_name,omitempty"`
	ProfileFingerprint        string `json:"profile_fingerprint,omitempty"`
	ModelFingerprint          string `json:"model_fingerprint,omitempty"`
}

// Validate checks that provenance is complete and uses canonical digest and
// name forms suitable for deterministic reports.
func (provenance ReportProvenance) Validate() error {
	if provenance.IdentifierRegistryVersion == "" || strings.TrimSpace(provenance.IdentifierRegistryVersion) != provenance.IdentifierRegistryVersion {
		return fmt.Errorf("reconcile: identifier registry version is required and must be canonical")
	}
	if provenance.IdentifierRegistryVersion != hub.IdentifierRegistryVersion {
		return fmt.Errorf("reconcile: unsupported identifier registry version %q", provenance.IdentifierRegistryVersion)
	}
	if err := validateReportSHA256("identifier registry digest", provenance.IdentifierRegistryDigest); err != nil {
		return err
	}
	profileValues := []string{provenance.System, provenance.ProfileName, provenance.ProfileFingerprint, provenance.ModelFingerprint}
	populated := 0
	for _, value := range profileValues {
		if value != "" {
			populated++
		}
	}
	if populated == 0 {
		return nil
	}
	if populated != len(profileValues) {
		return fmt.Errorf("reconcile: system, profile name, profile fingerprint, and model fingerprint must be supplied together")
	}
	if !reportSystemPattern.MatchString(provenance.System) {
		return fmt.Errorf("reconcile: invalid provenance system %q", provenance.System)
	}
	if !reportProfilePattern.MatchString(provenance.ProfileName) {
		return fmt.Errorf("reconcile: invalid provenance profile name %q", provenance.ProfileName)
	}
	if err := validateReportSHA256("profile fingerprint", provenance.ProfileFingerprint); err != nil {
		return err
	}
	return validateReportSHA256("model fingerprint", provenance.ModelFingerprint)
}

func validateReportSHA256(name, value string) error {
	if len(value) != 64 || strings.ToLower(value) != value {
		return fmt.Errorf("reconcile: %s must be a lowercase SHA-256 digest", name)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("reconcile: %s must be a lowercase SHA-256 digest", name)
	}
	return nil
}

// Report is deterministic for a fixed set of inputs and Finder responses.
// Consumers validate its decision invariants before producing a review artifact
// or partitioning inputs, so a persisted report cannot be edited into a
// contradictory verdict/summary combination silently.
type Report struct {
	Version       string           `json:"version"`
	PolicyVersion string           `json:"policy_version"`
	Provenance    ReportProvenance `json:"provenance"`
	Mode          Mode             `json:"mode"`
	Results       []Result         `json:"results"`
	Summary       Summary          `json:"summary"`
}

func validateReportContract(report Report) error {
	if report.Version != ReportVersion {
		return fmt.Errorf("reconcile: unsupported report version %q", report.Version)
	}
	if err := (Policy{Version: report.PolicyVersion}).Validate(); err != nil {
		return err
	}
	if err := report.Provenance.Validate(); err != nil {
		return err
	}
	return validateReportContents(report)
}

func validateReportContents(report Report) error {
	if report.Mode == "" {
		return fmt.Errorf("reconcile: report mode is required")
	}
	if err := ValidateMode(report.Mode); err != nil {
		return err
	}
	wantSummary := Summary{}
	seenIndexes := make(map[int]struct{}, len(report.Results))
	seenKeys := make(map[string]struct{}, len(report.Results))
	for index, result := range report.Results {
		if result.InputIndex < 0 || result.InputIndex >= len(report.Results) {
			return fmt.Errorf("reconcile: report result %d has invalid input index %d", index, result.InputIndex)
		}
		if _, exists := seenIndexes[result.InputIndex]; exists {
			return fmt.Errorf("reconcile: report repeats input index %d", result.InputIndex)
		}
		seenIndexes[result.InputIndex] = struct{}{}
		if result.InputKey == "" || strings.TrimSpace(result.InputKey) != result.InputKey {
			return fmt.Errorf("reconcile: report result %d has invalid input key %q", index, result.InputKey)
		}
		if _, exists := seenKeys[result.InputKey]; exists {
			return fmt.Errorf("reconcile: report repeats input key %q", result.InputKey)
		}
		seenKeys[result.InputKey] = struct{}{}
		if !validVerdict(result.Verdict) {
			return fmt.Errorf("reconcile: report result %q has unsupported verdict %q", result.InputKey, result.Verdict)
		}
		if result.Verdict == VerdictNew && len(result.Matches) != 0 {
			return fmt.Errorf("reconcile: new report result %q must not contain matches", result.InputKey)
		}
		if result.Verdict != VerdictNew && len(result.Matches) == 0 {
			return fmt.Errorf("reconcile: %s report result %q must contain at least one match", result.Verdict, result.InputKey)
		}
		exactMatches := 0
		for matchIndex, match := range result.Matches {
			if match.Kind != MatchExactIdentifier && match.Kind != MatchHeuristic {
				return fmt.Errorf("reconcile: report result %q match %d has unsupported kind %q", result.InputKey, matchIndex, match.Kind)
			}
			if match.Score < 0 || match.Score > 100 {
				return fmt.Errorf("reconcile: report result %q match %d has invalid score %d", result.InputKey, matchIndex, match.Score)
			}
			if match.Kind == MatchExactIdentifier {
				exactMatches++
			}
		}
		switch result.Verdict {
		case VerdictDuplicate:
			if len(result.Matches) != 1 || exactMatches != 1 || result.Matches[0].RequiresReview {
				return fmt.Errorf("reconcile: duplicate report result %q must contain one review-free exact match", result.InputKey)
			}
		case VerdictReview:
			if len(result.Matches) != 1 {
				return fmt.Errorf("reconcile: review report result %q must contain one match", result.InputKey)
			}
			match := result.Matches[0]
			if match.Kind == MatchExactIdentifier && !match.RequiresReview {
				return fmt.Errorf("reconcile: review report result %q has an exact match that does not require review", result.InputKey)
			}
		case VerdictAmbiguous:
			if len(result.Matches) < 2 {
				return fmt.Errorf("reconcile: ambiguous report result %q must contain multiple matches", result.InputKey)
			}
		}
		addSummary(&wantSummary, result.Verdict)
	}
	if report.Summary != wantSummary {
		return fmt.Errorf("reconcile: report summary does not match results")
	}
	return nil
}

// Partition separates inputs for a later ingest operation. It never mutates or
// uploads records.
type Partition struct {
	Accepted       []Input
	Held           []Input
	Skipped        []Input
	ReviewRequired bool
}
