package reconcile

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

const (
	grossPublisherConflictThreshold = 55
	grossTitleConflictThreshold     = 55
	seriousYearDifferenceThreshold  = 1
)

// Compare evaluates one candidate using a versioned policy. The boolean is
// false when the candidate has insufficient evidence to include in a report.
func Compare(incoming *hubv1.Record, candidate Candidate, policy Policy) (Match, bool, error) {
	if incoming == nil {
		return Match{}, false, fmt.Errorf("reconcile: incoming record is nil")
	}
	if candidate.Record == nil {
		return Match{}, false, fmt.Errorf("reconcile: candidate record is nil")
	}
	policy = normalizedPolicy(policy)
	if err := policy.Validate(); err != nil {
		return Match{}, false, err
	}
	identifierRegistry, err := policy.identifierRegistry()
	if err != nil {
		return Match{}, false, err
	}

	incomingIDs := identifiersWithRegistry(incoming, identifierRegistry)
	existingIDs := identifiersWithRegistry(candidate.Record, identifierRegistry)
	strongExact, weakExact, identifierConflicts := compareIdentifiers(incomingIDs, existingIDs, identifierRegistry)
	title := compareTitles(incoming, candidate.Record)
	author := compareAuthors(incoming, candidate.Record)
	incomingYear, existingYear := primaryYear(incoming), primaryYear(candidate.Record)

	match := Match{
		Candidate:   candidateReferenceWithRegistry(candidate, identifierRegistry),
		Differences: metadataDifferencesWithRegistry(incoming, candidate.Record, identifierRegistry),
	}

	if len(strongExact) != 0 {
		match.Kind = MatchExactIdentifier
		match.Score = 100
		match.Confidence = "exact"
		for index := range strongExact {
			if index > 0 {
				strongExact[index].Weight = 0
			}
		}
		match.Evidence = append(match.Evidence, strongExact...)
		if len(identifierConflicts) != 0 {
			match.RequiresReview = true
			match.Score -= 5
			match.Confidence = "conflict"
			for index := range identifierConflicts {
				identifierConflicts[index].Weight = 0
				if index == 0 {
					identifierConflicts[index].Weight = -5
				}
			}
			match.Evidence = append(match.Evidence, identifierConflicts...)
		}
		if title.available && title.score < grossTitleConflictThreshold {
			match.RequiresReview = true
			match.Score -= 5
			match.Confidence = "conflict"
			match.Evidence = append(match.Evidence, Evidence{
				Code:     "title_conflict",
				Field:    "title",
				Incoming: title.incoming,
				Existing: title.existing,
				Weight:   -5,
			})
		} else if title.available {
			match.Evidence = append(match.Evidence, titleEvidence(title, 0))
		}
		metadataConflicts := seriousMetadataConflicts(incoming, candidate.Record, match.Differences, author)
		yearConflictReported := false
		if len(metadataConflicts) != 0 {
			match.RequiresReview = true
			match.Score -= 5
			match.Confidence = "conflict"
			for index := range metadataConflicts {
				metadataConflicts[index].Weight = 0
				if index == 0 {
					metadataConflicts[index].Weight = -5
				}
				if metadataConflicts[index].Field == "year" {
					yearConflictReported = true
				}
			}
			match.Evidence = append(match.Evidence, metadataConflicts...)
		}
		match.Score = max(0, match.Score)
		if evidence, ok := author.evidence(0); ok {
			match.Evidence = append(match.Evidence, evidence)
		}
		if incomingYear != 0 && existingYear != 0 {
			code := "year_exact"
			if incomingYear != existingYear {
				code = "year_conflict"
			}
			if !yearConflictReported {
				match.Evidence = append(match.Evidence, Evidence{
					Code:     code,
					Field:    "year",
					Incoming: formatYear(incomingYear),
					Existing: formatYear(existingYear),
				})
			}
		}
		sortEvidence(match.Evidence)
		return match, true, nil
	}

	match.Kind = MatchHeuristic
	score := 0
	if title.available {
		if title.exact {
			score += 50
			match.Evidence = append(match.Evidence, titleEvidence(title, 50))
		} else {
			weight := title.score / 2
			score += weight
			match.Evidence = append(match.Evidence, titleEvidence(title, weight))
		}
	}
	if evidence, ok := author.evidence(author.weight()); ok {
		score += evidence.Weight
		match.Evidence = append(match.Evidence, evidence)
	}
	if incomingYear != 0 && existingYear != 0 {
		if incomingYear == existingYear {
			score += 15
			match.Evidence = append(match.Evidence, Evidence{
				Code: "year_exact", Field: "year",
				Incoming: formatYear(incomingYear), Existing: formatYear(existingYear), Weight: 15,
			})
		} else {
			score -= 10
			match.Evidence = append(match.Evidence, Evidence{
				Code: "year_conflict", Field: "year",
				Incoming: formatYear(incomingYear), Existing: formatYear(existingYear), Weight: -10,
			})
		}
	}
	if len(weakExact) != 0 {
		weight := 10
		score += weight
		for i := range weakExact {
			weakExact[i].Weight = weight
			if i > 0 {
				weakExact[i].Weight = 0
			}
		}
		match.Evidence = append(match.Evidence, weakExact...)
	}
	if len(identifierConflicts) != 0 {
		score -= 20
		for i := range identifierConflicts {
			identifierConflicts[i].Weight = -20
			if i > 0 {
				identifierConflicts[i].Weight = 0
			}
		}
		match.Evidence = append(match.Evidence, identifierConflicts...)
	}
	match.Score = max(0, min(99, score))

	yearExact := incomingYear != 0 && incomingYear == existingYear
	authorAny := author.kind != ""
	authorExact := author.kind == "orcid" || author.kind == "full"
	weakIDExact := len(weakExact) != 0
	shortTitle := isShortTitle(title.incoming)
	qualifies := false
	switch {
	case title.exact && !shortTitle:
		qualifies = true
	case title.exact && shortTitle && authorAny && yearExact:
		qualifies = true
	case title.score >= 92 && authorAny && yearExact:
		qualifies = true
	case title.score >= 94 && authorExact:
		qualifies = true
	case title.score >= 96 && yearExact && !shortTitle:
		qualifies = true
	case weakIDExact && title.score >= 80 && (authorAny || yearExact):
		qualifies = true
	}
	if !qualifies {
		return Match{}, false, nil
	}

	switch {
	case match.Score >= 80:
		match.Confidence = "high"
	case match.Score >= 65:
		match.Confidence = "medium"
	default:
		match.Confidence = "low"
	}
	match.RequiresReview = true
	sortEvidence(match.Evidence)
	return match, true, nil
}

type titleComparison struct {
	available bool
	exact     bool
	score     int
	incoming  string
	existing  string
}

func compareTitles(incoming, existing *hubv1.Record) titleComparison {
	incomingTitles := titleVariants(incoming)
	existingTitles := titleVariants(existing)
	best := titleComparison{}
	for _, incomingTitle := range incomingTitles {
		for _, existingTitle := range existingTitles {
			score := titleSimilarity(incomingTitle, existingTitle)
			comparison := titleComparison{
				available: true,
				exact:     incomingTitle == existingTitle,
				score:     score,
				incoming:  incomingTitle,
				existing:  existingTitle,
			}
			if betterTitleComparison(comparison, best) {
				best = comparison
			}
		}
	}
	return best
}

func betterTitleComparison(candidate, current titleComparison) bool {
	if !current.available || candidate.score != current.score {
		return !current.available || candidate.score > current.score
	}
	if candidate.incoming != current.incoming {
		return candidate.incoming < current.incoming
	}
	return candidate.existing < current.existing
}

func titleEvidence(comparison titleComparison, weight int) Evidence {
	code := "title_similar"
	if comparison.exact {
		code = "title_exact"
	}
	return Evidence{
		Code: code, Field: "title", Incoming: comparison.incoming,
		Existing: comparison.existing, Weight: weight,
	}
}

func titleSimilarity(left, right string) int {
	if left == "" || right == "" {
		return 0
	}
	if left == right {
		return 100
	}
	tokenScore := diceCoefficient(uniqueTokens(left), uniqueTokens(right))
	trigramScore := diceCoefficient(trigrams(left), trigrams(right))
	return int(math.Round(max(tokenScore, trigramScore) * 100))
}

func uniqueTokens(value string) map[string]int {
	result := make(map[string]int)
	for _, token := range strings.Fields(value) {
		result[token] = 1
	}
	return result
}

func trigrams(value string) map[string]int {
	runes := []rune(value)
	result := make(map[string]int)
	if len(runes) < 3 {
		if len(runes) != 0 {
			result[string(runes)] = 1
		}
		return result
	}
	for i := 0; i <= len(runes)-3; i++ {
		result[string(runes[i:i+3])]++
	}
	return result
}

func diceCoefficient(left, right map[string]int) float64 {
	leftCount, rightCount, overlap := 0, 0, 0
	for key, count := range left {
		leftCount += count
		if other := right[key]; other != 0 {
			overlap += min(count, other)
		}
	}
	for _, count := range right {
		rightCount += count
	}
	if leftCount+rightCount == 0 {
		return 0
	}
	return float64(2*overlap) / float64(leftCount+rightCount)
}

func isShortTitle(value string) bool {
	return len(strings.Fields(value)) < 3 || utf8.RuneCountInString(value) < 20
}

type authorComparison struct {
	kind     string
	incoming string
	existing string
}

func compareAuthors(incoming, existing *hubv1.Record) authorComparison {
	incomingAuthors := normalizedAuthors(incoming)
	existingAuthors := normalizedAuthors(existing)
	for _, left := range incomingAuthors {
		for _, right := range existingAuthors {
			if value, ok := stringIntersection(left.ORCIDs, right.ORCIDs); ok {
				return authorComparison{kind: "orcid", incoming: value, existing: value}
			}
		}
	}
	for _, left := range incomingAuthors {
		for _, right := range existingAuthors {
			if left.Full != "" && left.Full == right.Full {
				return authorComparison{kind: "full", incoming: left.Full, existing: right.Full}
			}
		}
	}
	for _, left := range incomingAuthors {
		for _, right := range existingAuthors {
			if left.Family != "" && left.Family == right.Family {
				return authorComparison{kind: "family", incoming: left.Family, existing: right.Family}
			}
		}
	}
	return authorComparison{}
}

func (comparison authorComparison) weight() int {
	switch comparison.kind {
	case "orcid":
		return 30
	case "full":
		return 25
	case "family":
		return 15
	default:
		return 0
	}
}

func (comparison authorComparison) evidence(weight int) (Evidence, bool) {
	if comparison.kind == "" {
		return Evidence{}, false
	}
	return Evidence{
		Code: "author_" + comparison.kind + "_exact", Field: "author",
		Incoming: comparison.incoming, Existing: comparison.existing, Weight: weight,
	}, true
}

func seriousMetadataConflicts(incoming, existing *hubv1.Record, differences []Difference, author authorComparison) []Evidence {
	var conflicts []Evidence
	for _, difference := range differences {
		if difference.Kind != DifferenceChanged {
			continue
		}
		conflict := Evidence{
			Field: difference.Field, Incoming: difference.Incoming, Existing: difference.Existing,
		}
		switch difference.Field {
		case "authors":
			if author.kind != "" {
				continue
			}
			conflict.Code = "author_conflict"
			conflict.Field = "author"
		case "year":
			yearDifference := int64(primaryYear(incoming)) - int64(primaryYear(existing))
			if yearDifference < 0 {
				yearDifference = -yearDifference
			}
			if yearDifference <= seriousYearDifferenceThreshold {
				continue
			}
			conflict.Code = "year_conflict"
		case "publisher":
			if titleSimilarity(normalizeText(difference.Incoming), normalizeText(difference.Existing)) >= grossPublisherConflictThreshold {
				continue
			}
			conflict.Code = "publisher_conflict"
		case "resource_type":
			if !resourceTypesConflict(incoming.GetResourceType(), existing.GetResourceType()) {
				continue
			}
			conflict.Code = "resource_type_conflict"
		default:
			continue
		}
		conflicts = append(conflicts, conflict)
	}
	return conflicts
}

func resourceTypesConflict(incoming, existing *hubv1.ResourceType) bool {
	return resourceTypeComparisonKey(incoming) != resourceTypeComparisonKey(existing)
}

func resourceTypeComparisonKey(value *hubv1.ResourceType) string {
	if value == nil {
		return ""
	}
	if value.GetType() != hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED {
		name := strings.TrimPrefix(value.GetType().String(), "RESOURCE_TYPE_")
		return normalizeText(strings.ReplaceAll(name, "_", " "))
	}
	return normalizeText(value.GetOriginal())
}

func stringIntersection(left, right []string) (string, bool) {
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		switch {
		case left[i] == right[j]:
			return left[i], true
		case left[i] < right[j]:
			i++
		default:
			j++
		}
	}
	return "", false
}

func compareIdentifiers(incoming, existing []IdentifierKey, registry *hub.IdentifierRegistry) (strong, weak, conflicts []Evidence) {
	existingSet := make(map[identifierIdentity][]IdentifierKey, len(existing))
	incomingByType := strongIdentifiersByScope(incoming, registry)
	existingByType := strongIdentifiersByScope(existing, registry)
	for _, id := range existing {
		identity := identifierIdentityOf(id)
		existingSet[identity] = append(existingSet[identity], id)
	}
	for _, id := range incoming {
		matches := existingSet[identifierIdentityOf(id)]
		if len(matches) == 0 {
			continue
		}
		evidence := Evidence{
			Code: "identifier_exact", Field: "identifier." + id.Scheme,
			Incoming: id.Value, Existing: id.Value,
		}
		strongPair := false
		if IsStrongIdentifierWithRegistry(id, registry) {
			for _, existingID := range matches {
				if IsStrongIdentifierWithRegistry(existingID, registry) {
					strongPair = true
					break
				}
			}
		}
		if strongPair {
			evidence.Weight = 100
			strong = append(strong, evidence)
		} else {
			weak = append(weak, evidence)
		}
	}

	var types []string
	for typeName := range incomingByType {
		if len(existingByType[typeName]) != 0 &&
			!identifierValuesIntersect(incomingByType[typeName], existingByType[typeName]) {
			types = append(types, typeName)
		}
	}
	sort.Strings(types)
	for _, typeName := range types {
		conflicts = append(conflicts, Evidence{
			Code: "identifier_conflict", Field: "identifier." + typeName,
			Incoming: strings.Join(incomingByType[typeName], "; "),
			Existing: strings.Join(existingByType[typeName], "; "),
		})
	}
	return strong, weak, conflicts
}

type identifierIdentity struct {
	Scheme       string
	NamespaceURI string
	Value        string
}

func identifierIdentityOf(identifier IdentifierKey) identifierIdentity {
	return identifierIdentity{
		Scheme: identifier.Scheme, NamespaceURI: identifier.NamespaceURI, Value: identifier.Value,
	}
}

func strongIdentifiersByScope(values []IdentifierKey, registry *hub.IdentifierRegistry) map[string][]string {
	result := make(map[string][]string)
	for _, value := range values {
		if IsStrongIdentifierWithRegistry(value, registry) {
			scope := strings.Join([]string{value.Scheme, value.NamespaceURI, value.IdentityLevel.String()}, "@")
			result[scope] = append(result[scope], value.Value)
		}
	}
	return result
}

func identifierValuesIntersect(left, right []string) bool {
	_, ok := stringIntersection(left, right)
	return ok
}

func sortEvidence(values []Evidence) {
	sort.SliceStable(values, func(i, j int) bool {
		if evidenceRank(values[i].Code) != evidenceRank(values[j].Code) {
			return evidenceRank(values[i].Code) < evidenceRank(values[j].Code)
		}
		if values[i].Field != values[j].Field {
			return values[i].Field < values[j].Field
		}
		if values[i].Incoming != values[j].Incoming {
			return values[i].Incoming < values[j].Incoming
		}
		return values[i].Existing < values[j].Existing
	})
}

func evidenceRank(code string) int {
	switch {
	case strings.HasPrefix(code, "identifier_"):
		return 0
	case strings.HasPrefix(code, "title_"):
		return 1
	case strings.HasPrefix(code, "author_"):
		return 2
	case strings.HasPrefix(code, "year_"):
		return 3
	default:
		return 4
	}
}

func candidateReferenceWithRegistry(candidate Candidate, registry *hub.IdentifierRegistry) CandidateRef {
	kind := candidate.Kind
	if kind == "" {
		kind = CandidateRepository
	}
	return CandidateRef{
		Kind: kind, Key: candidate.Key, RepositoryID: candidate.RepositoryID,
		UUID: candidate.UUID, URL: candidate.URL, Metadata: snapshotWithRegistry(candidate.Record, registry),
	}
}

func metadataDifferencesWithRegistry(incoming, existing *hubv1.Record, registry *hub.IdentifierRegistry) []Difference {
	incomingSnapshot, existingSnapshot := snapshotWithRegistry(incoming, registry), snapshotWithRegistry(existing, registry)
	type fieldComparison struct {
		field                    string
		incoming, existing       string
		incomingKey, existingKey string
	}
	comparisons := []fieldComparison{
		{"title", incomingSnapshot.Title, existingSnapshot.Title, normalizeTitle(incomingSnapshot.Title), normalizeTitle(existingSnapshot.Title)},
		{"full_title", incomingSnapshot.FullTitle, existingSnapshot.FullTitle, normalizeTitle(incomingSnapshot.FullTitle), normalizeTitle(existingSnapshot.FullTitle)},
		{"authors", strings.Join(incomingSnapshot.Authors, "; "), strings.Join(existingSnapshot.Authors, "; "), normalizedAuthorList(incoming), normalizedAuthorList(existing)},
		{"year", formatYear(incomingSnapshot.Year), formatYear(existingSnapshot.Year), formatYear(incomingSnapshot.Year), formatYear(existingSnapshot.Year)},
		{"identifiers", formatIdentifiers(incomingSnapshot.Identifiers), formatIdentifiers(existingSnapshot.Identifiers), formatIdentifiers(incomingSnapshot.Identifiers), formatIdentifiers(existingSnapshot.Identifiers)},
		{"publisher", incomingSnapshot.Publisher, existingSnapshot.Publisher, normalizeText(incomingSnapshot.Publisher), normalizeText(existingSnapshot.Publisher)},
		{"language", incomingSnapshot.Language, existingSnapshot.Language, normalizeText(incomingSnapshot.Language), normalizeText(existingSnapshot.Language)},
		{"resource_type", incomingSnapshot.ResourceType, existingSnapshot.ResourceType, normalizeText(incomingSnapshot.ResourceType), normalizeText(existingSnapshot.ResourceType)},
		{"abstract", abstractDescription(incomingSnapshot), abstractDescription(existingSnapshot), incomingSnapshot.AbstractSHA256, existingSnapshot.AbstractSHA256},
	}
	var result []Difference
	for _, comparison := range comparisons {
		if comparison.incomingKey == comparison.existingKey {
			continue
		}
		kind := DifferenceChanged
		switch {
		case comparison.incomingKey == "":
			kind = DifferenceExistingOnly
		case comparison.existingKey == "":
			kind = DifferenceIncomingOnly
		}
		result = append(result, Difference{
			Field: comparison.field, Incoming: comparison.incoming,
			Existing: comparison.existing, Kind: kind,
		})
	}
	return result
}

func normalizedAuthorList(record *hubv1.Record) string {
	authors := normalizedAuthors(record)
	values := make([]string, 0, len(authors))
	for _, author := range authors {
		values = append(values, author.Full)
	}
	return strings.Join(values, ";")
}

func formatIdentifiers(values []IdentifierKey) string {
	formatted := make([]string, 0, len(values))
	for _, value := range values {
		label := value.Scheme
		if value.NamespaceURI != "" {
			label += "@" + value.NamespaceURI
		}
		if value.IdentityLevel != hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED {
			label += "[" + strings.ToLower(strings.TrimPrefix(value.IdentityLevel.String(), "IDENTIFIER_IDENTITY_LEVEL_")) + "]"
		}
		formatted = append(formatted, label+":"+value.Value)
	}
	return strings.Join(formatted, "; ")
}

func abstractDescription(value Snapshot) string {
	if value.AbstractSHA256 == "" {
		return ""
	}
	return fmt.Sprintf("%d characters; sha256:%s", value.AbstractLength, value.AbstractSHA256)
}

func normalizedPolicy(policy Policy) Policy {
	if policy.Version == "" {
		policy.Version = PolicyVersion1
	}
	if policy.IdentifierRegistry.Version == "" {
		policy.IdentifierRegistry.Version = hub.IdentifierRegistryVersion
	}
	return policy
}
