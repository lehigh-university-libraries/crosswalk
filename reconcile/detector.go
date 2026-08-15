package reconcile

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

const defaultMaxBatchCandidates = 4096

// Detector coordinates identifier-first candidate retrieval, pure comparison,
// and duplicate detection within the current batch.
type Detector struct {
	Finder Finder
	Policy Policy
	// Provenance supplies the optional immutable system profile identity. Detect
	// derives registry version and digest from Policy and rejects mismatches.
	Provenance ReportProvenance
	// MaxBatchCandidates bounds the number of earlier input records compared
	// with any one record. Zero uses a conservative default. Candidate
	// selection is lossless for PolicyV1: records are indexed by identifiers,
	// exact titles, title tokens, and title trigrams before comparison.
	MaxBatchCandidates int
}

// Detect produces a report without mutating inputs or repository state.
func (detector Detector) Detect(ctx context.Context, inputs []Input, mode Mode) (Report, error) {
	if ctx == nil {
		return Report{}, fmt.Errorf("reconcile: context is required")
	}
	mode = normalizedMode(mode)
	if err := ValidateMode(mode); err != nil {
		return Report{}, err
	}
	policy := normalizedPolicy(detector.Policy)
	if err := policy.Validate(); err != nil {
		return Report{}, err
	}
	identifierRegistry, err := policy.identifierRegistry()
	if err != nil {
		return Report{}, err
	}
	provenance := detector.Provenance
	if provenance.IdentifierRegistryVersion != "" && provenance.IdentifierRegistryVersion != identifierRegistry.Version() {
		return Report{}, fmt.Errorf("reconcile: supplied identifier registry version %q does not match policy version %q", provenance.IdentifierRegistryVersion, identifierRegistry.Version())
	}
	if provenance.IdentifierRegistryDigest != "" && provenance.IdentifierRegistryDigest != identifierRegistry.Digest() {
		return Report{}, fmt.Errorf("reconcile: supplied identifier registry digest does not match policy")
	}
	provenance.IdentifierRegistryVersion = identifierRegistry.Version()
	provenance.IdentifierRegistryDigest = identifierRegistry.Digest()
	if err := provenance.Validate(); err != nil {
		return Report{}, err
	}
	if detector.Finder == nil && mode != ModeAssumeNew {
		return Report{}, fmt.Errorf("reconcile: a Finder is required in %q mode", mode)
	}

	normalizedInputs, err := normalizeInputs(inputs)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Version: ReportVersion, PolicyVersion: policy.Version, Provenance: provenance, Mode: mode,
		Results: make([]Result, 0, len(normalizedInputs)),
	}
	batch, err := newBatchCandidateIndex(identifierRegistry, detector.maxBatchCandidates())
	if err != nil {
		return Report{}, err
	}

	for index, input := range normalizedInputs {
		if err := ctx.Err(); err != nil {
			return Report{}, fmt.Errorf("reconcile input %q: %w", input.Key, err)
		}
		query := queryFromRecordWithRegistry(input.Key, input.Record, identifierRegistry)
		candidates, candidateErr := batch.candidates(input.Record)
		if candidateErr != nil {
			return Report{}, fmt.Errorf("reconcile input %q: %w", input.Key, candidateErr)
		}

		strongIDs := strongIdentifiers(query.Identifiers, identifierRegistry)
		if detector.Finder != nil && mode != ModeAssumeNew && len(strongIDs) != 0 {
			identifierQuery := query
			identifierQuery.Strategy = QueryByIdentifier
			identifierQuery.Identifiers = strongIDs
			found, findErr := detector.Finder.Candidates(ctx, identifierQuery)
			if findErr != nil {
				return Report{}, fmt.Errorf("reconcile input %q identifier lookup: %w", input.Key, findErr)
			}
			found, findErr = normalizeFinderCandidates(found)
			if findErr != nil {
				return Report{}, fmt.Errorf("reconcile input %q identifier lookup: %w", input.Key, findErr)
			}
			candidates = append(candidates, found...)
		}

		matches, compareErr := compareCandidates(input.Record, candidates, policy)
		if compareErr != nil {
			return Report{}, fmt.Errorf("reconcile input %q: %w", input.Key, compareErr)
		}
		exactMatches := matchesByKind(matches, MatchExactIdentifier)

		// A metadata query can only add weaker evidence, so avoid it once a
		// strong identifier has resolved a candidate. This also makes the lookup
		// ordering explicit for future Drupal Finder implementations.
		if len(exactMatches) == 0 && detector.Finder != nil && mode != ModeAssumeNew && query.Title != "" {
			metadataQuery := query
			metadataQuery.Strategy = QueryByMetadata
			found, findErr := detector.Finder.Candidates(ctx, metadataQuery)
			if findErr != nil {
				return Report{}, fmt.Errorf("reconcile input %q metadata lookup: %w", input.Key, findErr)
			}
			found, findErr = normalizeFinderCandidates(found)
			if findErr != nil {
				return Report{}, fmt.Errorf("reconcile input %q metadata lookup: %w", input.Key, findErr)
			}
			candidates = append(candidates, found...)
			matches, compareErr = compareCandidates(input.Record, candidates, policy)
			if compareErr != nil {
				return Report{}, fmt.Errorf("reconcile input %q: %w", input.Key, compareErr)
			}
			exactMatches = matchesByKind(matches, MatchExactIdentifier)
		}

		verdict := VerdictNew
		switch len(exactMatches) {
		case 0:
			if len(matches) == 1 {
				verdict = VerdictReview
			} else if len(matches) > 1 {
				verdict = VerdictAmbiguous
			}
		default:
			matches = exactMatches
			if len(exactMatches) > 1 {
				verdict = VerdictAmbiguous
			} else if exactMatches[0].RequiresReview {
				verdict = VerdictReview
			} else {
				verdict = VerdictDuplicate
			}
		}

		result := Result{
			InputKey: input.Key, InputIndex: index, Input: snapshotWithRegistry(input.Record, identifierRegistry),
			Verdict: verdict, Matches: matches,
		}
		report.Results = append(report.Results, result)
		addSummary(&report.Summary, verdict)

		batch.add(Candidate{
			Kind: CandidateBatch, Key: input.Key, Record: input.Record,
		})
	}
	return report, nil
}

func (detector Detector) maxBatchCandidates() int {
	if detector.MaxBatchCandidates == 0 {
		return defaultMaxBatchCandidates
	}
	return detector.MaxBatchCandidates
}

// batchCandidateIndex avoids comparing every record with every prior batch
// record. Every PolicyV1 metadata match has a positive title token or trigram
// overlap, while exact matches share an identifier, so the index does not
// discard reportable candidates. Pathologically broad batches fail closed
// instead of consuming unbounded CPU and report memory.
type batchCandidateIndex struct {
	registry      *hub.IdentifierRegistry
	maxCandidates int
	values        []Candidate
	features      map[string][]int
}

func newBatchCandidateIndex(registry *hub.IdentifierRegistry, maxCandidates int) (*batchCandidateIndex, error) {
	if maxCandidates <= 0 {
		return nil, fmt.Errorf("reconcile: maximum batch candidates must be positive")
	}
	return &batchCandidateIndex{
		registry: registry, maxCandidates: maxCandidates,
		features: make(map[string][]int),
	}, nil
}

func (index *batchCandidateIndex) add(candidate Candidate) {
	position := len(index.values)
	index.values = append(index.values, candidate)
	for _, feature := range batchCandidateFeatures(candidate.Record, index.registry) {
		index.features[feature] = append(index.features[feature], position)
	}
}

func (index *batchCandidateIndex) candidates(record *hubv1.Record) ([]Candidate, error) {
	positions := make(map[int]struct{})
	for _, feature := range batchCandidateFeatures(record, index.registry) {
		for _, position := range index.features[feature] {
			positions[position] = struct{}{}
			if len(positions) > index.maxCandidates {
				return nil, fmt.Errorf("batch candidate set exceeds %d; split the batch or raise MaxBatchCandidates explicitly", index.maxCandidates)
			}
		}
	}
	ordered := make([]int, 0, len(positions))
	for position := range positions {
		ordered = append(ordered, position)
	}
	sort.Ints(ordered)
	result := make([]Candidate, 0, len(ordered))
	for _, position := range ordered {
		result = append(result, index.values[position])
	}
	return result, nil
}

func batchCandidateFeatures(record *hubv1.Record, registry *hub.IdentifierRegistry) []string {
	features := make(map[string]struct{})
	for _, identifier := range identifiersWithRegistry(record, registry) {
		identity := identifierIdentityOf(identifier)
		features[strings.Join([]string{"identifier", identity.Scheme, identity.NamespaceURI, identity.Value}, "\x00")] = struct{}{}
	}
	for _, title := range titleVariants(record) {
		features["title\x00"+title] = struct{}{}
		for token := range uniqueTokens(title) {
			features["token\x00"+token] = struct{}{}
		}
		for trigram := range trigrams(title) {
			features["trigram\x00"+trigram] = struct{}{}
		}
	}
	result := make([]string, 0, len(features))
	for feature := range features {
		result = append(result, feature)
	}
	sort.Strings(result)
	return result
}

// ValidateMode reports whether a mode is supported. An empty mode resolves to
// ModeHold when passed to Detect or PartitionInputs.
func ValidateMode(mode Mode) error {
	switch mode {
	case ModeHold, ModeSkip, ModeForceNew, ModeAssumeNew:
		return nil
	default:
		return fmt.Errorf("reconcile: unsupported mode %q", mode)
	}
}

func normalizedMode(mode Mode) Mode {
	if mode == "" {
		return ModeHold
	}
	return mode
}

func normalizeInputs(inputs []Input) ([]Input, error) {
	result := make([]Input, len(inputs))
	explicit := make(map[string]bool, len(inputs))
	for index, input := range inputs {
		if input.Record == nil {
			return nil, fmt.Errorf("reconcile: input %d has a nil record", index)
		}
		key := strings.TrimSpace(input.Key)
		if key == "" {
			continue
		}
		if explicit[key] {
			return nil, fmt.Errorf("reconcile: duplicate input key %q", key)
		}
		explicit[key] = true
	}

	used := make(map[string]bool, len(inputs))
	for index, input := range inputs {
		key := strings.TrimSpace(input.Key)
		if key == "" {
			base := "record-" + strconv.Itoa(index+1)
			key = base
			for suffix := 2; explicit[key] || used[key]; suffix++ {
				key = base + "-" + strconv.Itoa(suffix)
			}
		}
		if used[key] {
			return nil, fmt.Errorf("reconcile: duplicate input key %q", key)
		}
		used[key] = true
		result[index] = Input{Key: key, Record: input.Record}
	}
	return result, nil
}

func strongIdentifiers(values []IdentifierKey, registry *hub.IdentifierRegistry) []IdentifierKey {
	var result []IdentifierKey
	for _, value := range values {
		if IsStrongIdentifierWithRegistry(value, registry) {
			result = append(result, value)
		}
	}
	return result
}

func normalizeFinderCandidates(candidates []Candidate) ([]Candidate, error) {
	result := make([]Candidate, len(candidates))
	for index, candidate := range candidates {
		if candidate.Record == nil {
			return nil, fmt.Errorf("candidate %d has a nil record", index)
		}
		if candidate.Kind != "" && candidate.Kind != CandidateRepository {
			return nil, fmt.Errorf("candidate %d has invalid Finder kind %q", index, candidate.Kind)
		}
		candidate.Kind = CandidateRepository
		candidate.Key = strings.TrimSpace(candidate.Key)
		candidate.RepositoryID = strings.TrimSpace(candidate.RepositoryID)
		candidate.UUID = strings.TrimSpace(candidate.UUID)
		candidate.URL = strings.TrimSpace(candidate.URL)
		if candidate.Key == "" && candidate.RepositoryID == "" && candidate.UUID == "" && candidate.URL == "" {
			return nil, fmt.Errorf("candidate %d has no stable repository locator", index)
		}
		result[index] = candidate
	}
	return result, nil
}

func compareCandidates(record *hubv1.Record, candidates []Candidate, policy Policy) ([]Match, error) {
	registry, err := normalizedPolicy(policy).identifierRegistry()
	if err != nil {
		return nil, err
	}
	candidates = deduplicateCandidatesWithRegistry(candidates, registry)
	var matches []Match
	for _, candidate := range candidates {
		match, reportable, err := Compare(record, candidate, policy)
		if err != nil {
			return nil, err
		}
		if reportable {
			matches = append(matches, match)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		if matches[i].Kind != matches[j].Kind {
			return matches[i].Kind < matches[j].Kind
		}
		return candidateReferenceSortKey(matches[i].Candidate) < candidateReferenceSortKey(matches[j].Candidate)
	})
	return matches, nil
}

// PartitionInputs applies report mode to the original inputs. It is deliberately
// separate from Detect so callers can persist or inspect a report before ingest.
func PartitionInputs(inputs []Input, report Report) (Partition, error) {
	if err := validateReportContract(report); err != nil {
		return Partition{}, err
	}
	identifierRegistry, err := registryForReport(report)
	if err != nil {
		return Partition{}, err
	}
	return partitionInputsWithRegistry(inputs, report, identifierRegistry)
}

func partitionInputsWithRegistry(inputs []Input, report Report, identifierRegistry *hub.IdentifierRegistry) (Partition, error) {
	mode := normalizedMode(report.Mode)
	if err := ValidateMode(mode); err != nil {
		return Partition{}, err
	}
	if len(inputs) != len(report.Results) {
		return Partition{}, fmt.Errorf("reconcile: report has %d results for %d inputs", len(report.Results), len(inputs))
	}
	normalizedInputs, err := normalizeInputs(inputs)
	if err != nil {
		return Partition{}, err
	}

	partition := Partition{}
	seen := make(map[int]bool, len(report.Results))
	results := make([]Result, len(report.Results))
	for _, result := range report.Results {
		if result.InputIndex < 0 || result.InputIndex >= len(inputs) || seen[result.InputIndex] {
			return Partition{}, fmt.Errorf("reconcile: invalid or duplicate input index %d", result.InputIndex)
		}
		if !validVerdict(result.Verdict) {
			return Partition{}, fmt.Errorf("reconcile: unsupported verdict %q", result.Verdict)
		}
		expected := normalizedInputs[result.InputIndex]
		if result.InputKey != expected.Key {
			return Partition{}, fmt.Errorf("reconcile: result %d has input key %q, want %q", result.InputIndex, result.InputKey, expected.Key)
		}
		if !snapshotsEqual(result.Input, snapshotWithRegistry(expected.Record, identifierRegistry)) {
			return Partition{}, fmt.Errorf("reconcile: input %q metadata changed after detection", expected.Key)
		}
		seen[result.InputIndex] = true
		results[result.InputIndex] = result
	}
	for index, input := range inputs {
		result := results[index]
		switch mode {
		case ModeForceNew:
			partition.Accepted = append(partition.Accepted, input)
		case ModeSkip:
			switch result.Verdict {
			case VerdictNew:
				partition.Accepted = append(partition.Accepted, input)
			case VerdictDuplicate:
				partition.Skipped = append(partition.Skipped, input)
			case VerdictReview, VerdictAmbiguous:
				partition.Held = append(partition.Held, input)
				partition.ReviewRequired = true
			default:
				return Partition{}, fmt.Errorf("reconcile: unsupported verdict %q", result.Verdict)
			}
		case ModeHold, ModeAssumeNew:
			switch result.Verdict {
			case VerdictNew:
				partition.Accepted = append(partition.Accepted, input)
			case VerdictDuplicate, VerdictReview, VerdictAmbiguous:
				partition.Held = append(partition.Held, input)
				partition.ReviewRequired = true
			default:
				return Partition{}, fmt.Errorf("reconcile: unsupported verdict %q", result.Verdict)
			}
		}
	}
	return partition, nil
}

func registryForReport(report Report) (*hub.IdentifierRegistry, error) {
	// A report intentionally stores only the effective registry digest, not the
	// full executable pattern configuration. Partitioning therefore requires
	// callers using a custom policy to retain identifiers exactly as snapshotted.
	// The default registry can be reconstructed and is sufficient for reports
	// without custom policy. A custom registry is verified through snapshots,
	// and non-identifier metadata still gets stale-input protection.
	registry := hub.DefaultIdentifierRegistry()
	if report.Provenance.IdentifierRegistryDigest == registry.Digest() {
		return registry, nil
	}
	return nil, fmt.Errorf("reconcile: partitioning a report with a custom identifier registry requires PartitionInputsWithPolicy")
}

// PartitionInputsWithPolicy applies a report using the exact policy that
// produced it. Registry version and digest must match the report provenance.
func PartitionInputsWithPolicy(inputs []Input, report Report, policy Policy) (Partition, error) {
	if err := validateReportContract(report); err != nil {
		return Partition{}, err
	}
	policy = normalizedPolicy(policy)
	registry, err := policy.identifierRegistry()
	if err != nil {
		return Partition{}, err
	}
	if report.Provenance.IdentifierRegistryVersion != registry.Version() || report.Provenance.IdentifierRegistryDigest != registry.Digest() {
		return Partition{}, fmt.Errorf("reconcile: partition policy identifier registry does not match report provenance")
	}
	return partitionInputsWithRegistry(inputs, report, registry)
}

func validVerdict(verdict Verdict) bool {
	switch verdict {
	case VerdictNew, VerdictDuplicate, VerdictReview, VerdictAmbiguous:
		return true
	default:
		return false
	}
}

func snapshotsEqual(left, right Snapshot) bool {
	return left.Title == right.Title &&
		left.FullTitle == right.FullTitle &&
		slices.Equal(left.Authors, right.Authors) &&
		left.Year == right.Year &&
		slices.Equal(left.Identifiers, right.Identifiers) &&
		left.Publisher == right.Publisher &&
		left.Language == right.Language &&
		left.ResourceType == right.ResourceType &&
		left.AbstractLength == right.AbstractLength &&
		left.AbstractSHA256 == right.AbstractSHA256
}

func addSummary(summary *Summary, verdict Verdict) {
	summary.Total++
	switch verdict {
	case VerdictNew:
		summary.New++
	case VerdictDuplicate:
		summary.Duplicate++
	case VerdictReview:
		summary.Review++
	case VerdictAmbiguous:
		summary.Ambiguous++
	}
}

func matchesByKind(matches []Match, kind MatchKind) []Match {
	var result []Match
	for _, match := range matches {
		if match.Kind == kind {
			result = append(result, match)
		}
	}
	return result
}

func sortCandidatesWithRegistry(candidates []Candidate, registry *hub.IdentifierRegistry) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidateSortKeyWithRegistry(candidates[i], registry), candidateSortKeyWithRegistry(candidates[j], registry)
		return left < right
	})
}

func deduplicateCandidatesWithRegistry(candidates []Candidate, registry *hub.IdentifierRegistry) []Candidate {
	if len(candidates) == 0 {
		return nil
	}
	values := append([]Candidate(nil), candidates...)
	for index := range values {
		if values[index].Kind == "" {
			values[index].Kind = CandidateRepository
		}
	}
	sortCandidatesWithRegistry(values, registry)
	result := make([]Candidate, 0, len(values))
	seen := make(map[string]bool)
	for _, candidate := range values {
		aliases := candidateAliases(candidate)
		duplicate := false
		for _, alias := range aliases {
			duplicate = duplicate || seen[alias]
		}
		for _, alias := range aliases {
			seen[alias] = true
		}
		if !duplicate {
			result = append(result, candidate)
		}
	}
	return result
}

func candidateAliases(candidate Candidate) []string {
	prefix := string(candidate.Kind) + "\x00"
	var aliases []string
	for label, value := range map[string]string{
		"repository": candidate.RepositoryID,
		"uuid":       candidate.UUID,
		"key":        candidate.Key,
		"url":        canonicalURL(candidate.URL),
	} {
		if value != "" {
			aliases = append(aliases, prefix+label+"\x00"+value)
		}
	}
	sort.Strings(aliases)
	return aliases
}

func candidateSortKeyWithRegistry(candidate Candidate, registry *hub.IdentifierRegistry) string {
	return strings.Join([]string{
		string(candidate.Kind), candidate.RepositoryID, candidate.UUID,
		candidate.Key, canonicalURL(candidate.URL), snapshotSortKey(snapshotWithRegistry(candidate.Record, registry)),
	}, "\x00")
}

func candidateReferenceSortKey(candidate CandidateRef) string {
	return strings.Join([]string{
		string(candidate.Kind), candidate.RepositoryID, candidate.UUID,
		candidate.Key, canonicalURL(candidate.URL), snapshotSortKey(candidate.Metadata),
	}, "\x00")
}

func snapshotSortKey(value Snapshot) string {
	return strings.Join([]string{
		normalizeTitle(value.Title), normalizeTitle(value.FullTitle),
		strings.Join(value.Authors, "\x1f"), formatYear(value.Year),
		formatIdentifiers(value.Identifiers), normalizeText(value.Publisher),
		normalizeText(value.Language), normalizeText(value.ResourceType),
		strconv.Itoa(value.AbstractLength), value.AbstractSHA256,
	}, "\x00")
}
