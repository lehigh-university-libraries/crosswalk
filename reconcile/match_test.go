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
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}
