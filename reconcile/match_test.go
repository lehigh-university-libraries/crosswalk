package reconcile

import (
	"reflect"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

func TestCompareInstitutionalIdentifierRequiresMatchingAuthority(t *testing.T) {
	t.Parallel()
	const authority = "https://repository-a.example.edu/identifiers/item/"
	policy := Policy{
		Version: PolicyVersion1,
		IdentifierRegistry: hub.IdentifierRegistryConfig{
			Version: hub.IdentifierRegistryVersion,
			Rules: []hub.IdentifierRule{{
				Scheme: "repository-item", NamespaceURI: authority,
				Type:                 hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
				DefaultIdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
				Pattern:              `^[0-9]+$`,
				ExactIdentityLevels:  []hubv1.IdentifierIdentityLevel{hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD},
			}},
		},
	}
	incoming := &hubv1.Record{Identifiers: []*hubv1.Identifier{{
		Scheme: "repository-item", NamespaceUri: authority, Value: "42",
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
	}}}
	sameAuthority := &hubv1.Record{Identifiers: []*hubv1.Identifier{{
		Scheme: "repository-item", NamespaceUri: authority, Value: "42",
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
	}}}
	match, reportable, err := Compare(incoming, Candidate{RepositoryID: "same", Record: sameAuthority}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !reportable || match.Kind != MatchExactIdentifier {
		t.Fatalf("same-authority match = %#v, reportable %v", match, reportable)
	}

	otherAuthority := &hubv1.Record{Identifiers: []*hubv1.Identifier{{
		Scheme: "repository-item", NamespaceUri: "https://repository-b.example.edu/identifiers/item/", Value: "42",
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD,
	}}}
	_, reportable, err = Compare(incoming, Candidate{RepositoryID: "other", Record: otherAuthority}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if reportable {
		t.Fatal("same local value from another authority must not match")
	}
}

func TestCompareScopusEIDIsExactButZenodoConceptDOIIsNot(t *testing.T) {
	t.Parallel()
	registry := hub.DefaultIdentifierRegistry()
	scopus, err := registry.NewIdentifierForScheme("EID:2-s2.0-85123456789", "scopus-eid", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED)
	if err != nil {
		t.Fatal(err)
	}
	match, reportable, err := Compare(
		&hubv1.Record{Identifiers: []*hubv1.Identifier{scopus}},
		Candidate{RepositoryID: "scopus", Record: &hubv1.Record{Identifiers: []*hubv1.Identifier{scopus}}},
		PolicyV1(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reportable || match.Kind != MatchExactIdentifier {
		t.Fatalf("Scopus match = %#v, reportable %v", match, reportable)
	}
	zenodoVersion, err := registry.CanonicalizeIdentifier(&hubv1.Identifier{
		Scheme: "doi", Value: "10.5281/zenodo.1234567",
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericDOI := hub.NewIdentifier("10.5281/zenodo.1234567", hubv1.IdentifierType_IDENTIFIER_TYPE_DOI)
	match, reportable, err = Compare(
		&hubv1.Record{Identifiers: []*hubv1.Identifier{zenodoVersion}},
		Candidate{RepositoryID: "existing-doi", Record: &hubv1.Record{Identifiers: []*hubv1.Identifier{genericDOI}}},
		PolicyV1(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reportable || match.Kind != MatchExactIdentifier {
		t.Fatalf("version DOI should match the same generic DOI exactly: %#v, reportable %v", match, reportable)
	}

	concept, err := registry.CanonicalizeIdentifier(&hubv1.Identifier{
		Scheme: "doi", Value: "10.5281/zenodo.1000000",
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, reportable, err = Compare(
		&hubv1.Record{Identifiers: []*hubv1.Identifier{concept}},
		Candidate{RepositoryID: "concept", Record: &hubv1.Record{Identifiers: []*hubv1.Identifier{hub.NewIdentifier("10.5281/zenodo.1000000", hubv1.IdentifierType_IDENTIFIER_TYPE_DOI)}}},
		PolicyV1(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if reportable {
		t.Fatal("concept DOI alone must not collapse all Zenodo versions")
	}
}

func TestCompareUsesAnyStrongExistingAnnotationForSharedIdentifier(t *testing.T) {
	t.Parallel()

	registry := hub.DefaultIdentifierRegistry()
	work, err := registry.CanonicalizeIdentifier(&hubv1.Identifier{
		Scheme: "doi", Value: "10.5281/zenodo.1234567",
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK,
	})
	if err != nil {
		t.Fatal(err)
	}
	concept, err := registry.CanonicalizeIdentifier(&hubv1.Identifier{
		Scheme: "doi", Value: "10.5281/zenodo.1234567",
		IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT,
	})
	if err != nil {
		t.Fatal(err)
	}
	incoming := &hubv1.Record{Identifiers: []*hubv1.Identifier{work}}
	for _, identifiers := range [][]*hubv1.Identifier{{concept, work}, {work, concept}} {
		match, reportable, err := Compare(
			incoming,
			Candidate{RepositoryID: "existing", Record: &hubv1.Record{Identifiers: identifiers}},
			PolicyV1(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if !reportable || match.Kind != MatchExactIdentifier || match.Score != 100 {
			t.Fatalf("shared strong identifier = (%#v, %t), want exact match", match, reportable)
		}
	}
}

func TestCompareExactIdentifier(t *testing.T) {
	t.Parallel()
	incoming := testRecord(
		"Reliable duplicate detection for scholarly repositories",
		2024,
		[]string{"Doe, Jane"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "https://doi.org/10.1234/ABC"),
	)
	existing := testRecord(
		"Reliable duplicate detection for scholarly repositories",
		2024,
		[]string{"Jane Doe"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1234/abc"),
	)

	match, reportable, err := Compare(incoming, Candidate{RepositoryID: "91", Record: existing}, PolicyV1())
	if err != nil {
		t.Fatal(err)
	}
	if !reportable || match.Kind != MatchExactIdentifier || match.Score != 100 || match.RequiresReview {
		t.Fatalf("unexpected exact match: reportable=%v match=%#v", reportable, match)
	}
	if got := match.Evidence[0]; got.Code != "identifier_exact" || got.Field != "identifier.doi" {
		t.Fatalf("first evidence = %#v", got)
	}
}

func TestCompareExactIdentifierWithGrossTitleConflictRequiresReview(t *testing.T) {
	t.Parallel()
	id := identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_WOS, "WOS:000123456789")
	incoming := testRecord("Quantum dynamics in nanoscale systems", 2022, []string{"Doe, Jane"}, id)
	existing := testRecord("A field guide to Appalachian wildflowers", 1980, []string{"Smith, Alex"}, id)

	match, reportable, err := Compare(incoming, Candidate{UUID: "candidate", Record: existing}, PolicyV1())
	if err != nil {
		t.Fatal(err)
	}
	if !reportable || !match.RequiresReview || match.Confidence != "conflict" {
		t.Fatalf("unexpected conflict match: reportable=%v match=%#v", reportable, match)
	}
	if !hasEvidence(match.Evidence, "title_conflict") {
		t.Fatalf("missing title conflict evidence: %#v", match.Evidence)
	}
}

func TestCompareExactIdentifierWithConflictingStrongIdentifierRequiresReview(t *testing.T) {
	t.Parallel()
	incoming := testRecord(
		"A consistent title for identifier conflict detection", 2024, nil,
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1000/shared"),
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_WOS, "WOS:000111111111"),
	)
	existing := testRecord(
		"A consistent title for identifier conflict detection", 2024, nil,
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1000/shared"),
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_WOS, "WOS:000222222222"),
	)

	match, reportable, err := Compare(incoming, Candidate{RepositoryID: "conflict", Record: existing}, PolicyV1())
	if err != nil {
		t.Fatal(err)
	}
	if !reportable || !match.RequiresReview || match.Score != 95 || !hasEvidence(match.Evidence, "identifier_conflict") {
		t.Fatalf("unexpected identifier conflict match: reportable=%v match=%#v", reportable, match)
	}
}

func TestCompareExactIdentifierWithSeriousMetadataConflictRequiresReview(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		configure func(*hubv1.Record, *hubv1.Record)
		evidence  string
	}{
		{
			name: "materially different year",
			configure: func(_, existing *hubv1.Record) {
				existing.Dates[0].Year = 2021
			},
			evidence: "year_conflict",
		},
		{
			name: "disjoint authors",
			configure: func(_, existing *hubv1.Record) {
				existing.Contributors = []*hubv1.Contributor{{Name: "Smith, Alex", Role: "author"}}
			},
			evidence: "author_conflict",
		},
		{
			name: "grossly different publisher",
			configure: func(incoming, existing *hubv1.Record) {
				incoming.Publisher = "Northwestern University Press"
				existing.Publisher = "Elsevier"
			},
			evidence: "publisher_conflict",
		},
		{
			name: "different resource type",
			configure: func(incoming, existing *hubv1.Record) {
				incoming.ResourceType = &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE}
				existing.ResourceType = &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET}
			},
			evidence: "resource_type_conflict",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			incoming, existing := exactMetadataPair()
			test.configure(incoming, existing)

			match, reportable, err := Compare(incoming, Candidate{RepositoryID: "metadata-conflict", Record: existing}, PolicyV1())
			if err != nil {
				t.Fatal(err)
			}
			if !reportable || !match.RequiresReview || match.Score != 95 || match.Confidence != "conflict" {
				t.Fatalf("serious metadata conflict = (%#v, %v), want score-95 review", match, reportable)
			}
			evidence, ok := evidenceByCode(match.Evidence, test.evidence)
			if !ok || evidence.Weight != -5 {
				t.Fatalf("%s evidence = (%#v, %v), want weight -5 in %#v", test.evidence, evidence, ok, match.Evidence)
			}
		})
	}
}

func TestCompareExactIdentifierIgnoresNonSeriousMetadataDifferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configure  func(*hubv1.Record, *hubv1.Record)
		evidence   string
		difference string
	}{
		{
			name: "one-sided values",
			configure: func(incoming, existing *hubv1.Record) {
				incoming.Publisher = "Northwestern University Press"
				incoming.ResourceType = &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE}
				existing.Contributors = nil
				existing.Dates = nil
			},
		},
		{
			name: "adjacent publication years",
			configure: func(_, existing *hubv1.Record) {
				existing.Dates[0].Year = 2023
			},
			evidence: "year_conflict",
		},
		{
			name: "similar normalized publishers",
			configure: func(incoming, existing *hubv1.Record) {
				incoming.Publisher = "The University of Chicago Press"
				existing.Publisher = "University of Chicago Press"
			},
			difference: "publisher",
		},
		{
			name: "overlapping authors",
			configure: func(incoming, existing *hubv1.Record) {
				incoming.Contributors = []*hubv1.Contributor{
					{Name: "Doe, Jane", Role: "author"},
					{Name: "Jones, Pat", Role: "author"},
				}
				existing.Contributors = []*hubv1.Contributor{
					{Name: "Jane Doe", Role: "creator"},
					{Name: "Smith, Alex", Role: "author"},
				}
			},
			difference: "authors",
		},
		{
			name: "same canonical resource type",
			configure: func(incoming, existing *hubv1.Record) {
				incoming.ResourceType = &hubv1.ResourceType{
					Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE, Original: "Journal Article",
				}
				existing.ResourceType = &hubv1.ResourceType{
					Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE, Original: "Article",
				}
			},
			difference: "resource_type",
		},
		{
			name: "language and abstract changes",
			configure: func(incoming, existing *hubv1.Record) {
				incoming.Language, existing.Language = "en", "fr"
				incoming.Abstract, existing.Abstract = "incoming abstract", "existing abstract"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			incoming, existing := exactMetadataPair()
			test.configure(incoming, existing)

			match, reportable, err := Compare(incoming, Candidate{RepositoryID: "benign-difference", Record: existing}, PolicyV1())
			if err != nil {
				t.Fatal(err)
			}
			if !reportable || match.RequiresReview || match.Score != 100 || match.Confidence != "exact" {
				t.Fatalf("non-serious metadata difference = (%#v, %v), want review-free exact match", match, reportable)
			}
			if test.evidence != "" {
				evidence, ok := evidenceByCode(match.Evidence, test.evidence)
				if !ok || evidence.Weight != 0 {
					t.Fatalf("%s evidence = (%#v, %v), want zero-weight evidence", test.evidence, evidence, ok)
				}
			}
			if test.difference != "" && !hasDifference(match.Differences, test.difference) {
				t.Fatalf("missing exercised %s difference in %#v", test.difference, match.Differences)
			}
		})
	}
}

func TestCompareExactIdentifierConflictClassesHaveIndependentPenalties(t *testing.T) {
	t.Parallel()
	incoming := testRecord(
		"Quantum dynamics in nanoscale systems", 2024, []string{"Doe, Jane"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1000/shared"),
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_WOS, "WOS:000111111111"),
	)
	incoming.Publisher = "Northwestern University Press"
	incoming.ResourceType = &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE}
	existing := testRecord(
		"A field guide to Appalachian wildflowers", 1990, []string{"Smith, Alex"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1000/shared"),
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_WOS, "WOS:000222222222"),
	)
	existing.Publisher = "Elsevier"
	existing.ResourceType = &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET}

	match, reportable, err := Compare(incoming, Candidate{RepositoryID: "combined-conflict", Record: existing}, PolicyV1())
	if err != nil {
		t.Fatal(err)
	}
	if !reportable || !match.RequiresReview || match.Score != 85 || match.Confidence != "conflict" {
		t.Fatalf("combined exact conflict = (%#v, %v), want three class penalties", match, reportable)
	}
	wantWeights := map[string]int{
		"identifier_conflict":    -5,
		"title_conflict":         -5,
		"author_conflict":        -5,
		"year_conflict":          0,
		"publisher_conflict":     0,
		"resource_type_conflict": 0,
	}
	for code, wantWeight := range wantWeights {
		evidence, ok := evidenceByCode(match.Evidence, code)
		if !ok || evidence.Weight != wantWeight {
			t.Errorf("%s evidence = (%#v, %v), want weight %d", code, evidence, ok, wantWeight)
		}
	}
}

func TestCompareMetadataSignalsAreReviewOnly(t *testing.T) {
	t.Parallel()
	incoming := testRecord(
		"Machine learning methods for identifying duplicate scholarly records",
		2023,
		[]string{"Doe, Jane"},
	)
	existing := testRecord(
		"Machine learning method for identifying duplicate scholarly records",
		2023,
		[]string{"Jane Doe"},
	)

	match, reportable, err := Compare(incoming, Candidate{RepositoryID: "42", Record: existing}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if !reportable || match.Kind != MatchHeuristic || !match.RequiresReview {
		t.Fatalf("metadata comparison should require review: reportable=%v match=%#v", reportable, match)
	}
	if match.Score >= 100 || !hasEvidence(match.Evidence, "author_full_exact") || !hasEvidence(match.Evidence, "year_exact") {
		t.Fatalf("unexpected metadata evidence: %#v", match)
	}
}

func TestCompareShortTitleNeedsAuthorAndYear(t *testing.T) {
	t.Parallel()
	incoming := testRecord("Editorial", 2020, nil)
	existing := testRecord("Editorial", 2020, nil)
	_, reportable, err := Compare(incoming, Candidate{RepositoryID: "1", Record: existing}, PolicyV1())
	if err != nil {
		t.Fatal(err)
	}
	if reportable {
		t.Fatal("short title and year alone must not be a match")
	}

	incoming.Contributors = []*hubv1.Contributor{{Name: "Doe, Jane", Role: "author"}}
	existing.Contributors = []*hubv1.Contributor{{Name: "Jane Doe", Role: "creator"}}
	match, reportable, err := Compare(incoming, Candidate{RepositoryID: "1", Record: existing}, PolicyV1())
	if err != nil {
		t.Fatal(err)
	}
	if !reportable || match.Kind != MatchHeuristic || !match.RequiresReview {
		t.Fatalf("short title with author and year = (%#v, %v), want review match", match, reportable)
	}
}

func TestCompareMetadataDifferencesAreStableAndCurated(t *testing.T) {
	t.Parallel()
	incoming := testRecord(
		"A sufficiently long shared title for comparison",
		2024,
		[]string{"Doe, Jane"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1234/shared"),
	)
	incoming.Publisher = "New Publisher"
	incoming.Language = "en"
	incoming.Abstract = "new abstract"
	existing := testRecord(
		"A sufficiently long shared title for comparison",
		2023,
		[]string{"Doe, Jane"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1234/shared"),
	)
	existing.Publisher = "Old Publisher"
	existing.Abstract = "old abstract"

	match, reportable, err := Compare(incoming, Candidate{RepositoryID: "2", Record: existing}, PolicyV1())
	if err != nil || !reportable {
		t.Fatalf("Compare() = (%#v, %v, %v)", match, reportable, err)
	}
	var fields []string
	for _, difference := range match.Differences {
		fields = append(fields, difference.Field)
	}
	want := []string{"year", "publisher", "language", "abstract"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("difference fields = %#v, want %#v", fields, want)
	}
	if match.Differences[2].Kind != DifferenceIncomingOnly {
		t.Fatalf("language difference = %#v, want incoming-only", match.Differences[2])
	}
	if match.Differences[3].Incoming == "new abstract" || match.Differences[3].Existing == "old abstract" {
		t.Fatal("abstract difference should contain fingerprints, not full text")
	}
}

func TestCompareRejectsUnsupportedPolicy(t *testing.T) {
	t.Parallel()
	_, _, err := Compare(
		testRecord("A long enough title for matching", 2024, nil),
		Candidate{RepositoryID: "1", Record: testRecord("A long enough title for matching", 2024, nil)},
		Policy{Version: "future"},
	)
	if err == nil {
		t.Fatal("expected unsupported policy error")
	}
}

func hasEvidence(values []Evidence, code string) bool {
	_, ok := evidenceByCode(values, code)
	return ok
}

func evidenceByCode(values []Evidence, code string) (Evidence, bool) {
	for _, value := range values {
		if value.Code == code {
			return value, true
		}
	}
	return Evidence{}, false
}

func hasDifference(values []Difference, field string) bool {
	for _, value := range values {
		if value.Field == field {
			return true
		}
	}
	return false
}

func exactMetadataPair() (*hubv1.Record, *hubv1.Record) {
	incoming := testRecord(
		"Reliable duplicate detection for scholarly repositories", 2024, []string{"Doe, Jane"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1234/shared"),
	)
	existing := testRecord(
		"Reliable duplicate detection for scholarly repositories", 2024, []string{"Jane Doe"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "10.1234/shared"),
	)
	return incoming, existing
}
