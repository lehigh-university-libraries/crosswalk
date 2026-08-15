package reconcile

import (
	"reflect"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

func TestIdentifiersCanonicalizeStrongSources(t *testing.T) {
	t.Parallel()
	record := &hubv1.Record{
		Identifiers: []*hubv1.Identifier{
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_URL, Value: "HTTPS://DOI.ORG/10.1234/Example.;"},
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV, Value: "https://arxiv.org/pdf/2301.01234v3.pdf?download=1"},
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, Display: "Web of Science", Value: "UT=000123456789"},
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_UUID, Value: "A1B2C3D4-E5F6-47A8-9012-ABCDEF123456"},
		},
	}

	want := []IdentifierKey{
		builtinIdentifierKey(t, "arxiv", "2301.01234"),
		builtinIdentifierKey(t, "doi", "10.1234/example"),
		builtinIdentifierKey(t, "uuid", "a1b2c3d4-e5f6-47a8-9012-abcdef123456"),
		builtinIdentifierKey(t, "wos", "WOS:000123456789"),
	}
	if got := Identifiers(record); !reflect.DeepEqual(got, want) {
		t.Fatalf("Identifiers() = %#v, want %#v", got, want)
	}
}

func TestIdentifiersUseKnownSourceInfoAndDeduplicate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		record *hubv1.Record
		want   []IdentifierKey
	}{
		{
			name: "Crossref DOI",
			record: &hubv1.Record{
				Identifiers: []*hubv1.Identifier{{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.5555/SAME"}},
				SourceInfo:  &hubv1.SourceInfo{Format: "crossref", SourceId: "https://doi.org/10.5555/same"},
			},
			want: []IdentifierKey{builtinIdentifierKey(t, "doi", "10.5555/same")},
		},
		{
			name:   "arXiv version",
			record: &hubv1.Record{SourceInfo: &hubv1.SourceInfo{Format: "arxiv", SourceId: "arXiv:math/0301234v2"}},
			want:   []IdentifierKey{builtinIdentifierKey(t, "arxiv", "math/0301234")},
		},
		{
			name:   "Web of Science",
			record: &hubv1.Record{SourceInfo: &hubv1.SourceInfo{Format: "web_of_science", SourceId: "WOS:000987654321"}},
			want:   []IdentifierKey{builtinIdentifierKey(t, "wos", "WOS:000987654321")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := Identifiers(test.record); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Identifiers() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestStrongIdentifiersRejectMalformedAndPlaceholderValues(t *testing.T) {
	t.Parallel()
	invalid := []IdentifierKey{
		{Scheme: "doi", Value: "N/A"},
		{Scheme: "doi", Value: "10.1234/has whitespace"},
		{Scheme: "arxiv", Value: "unknown"},
		{Scheme: "wos", Value: "WOS:UNKNOWN"},
		{Scheme: "pmid", Value: "0"},
		{Scheme: "pmcid", Value: "PMC0"},
		{Scheme: "handle", Value: "N/A"},
		{Scheme: "pid", Value: "pending"},
		{Scheme: "uuid", Value: "not-a-uuid"},
	}
	for _, identifier := range invalid {
		if IsStrongIdentifier(identifier) {
			t.Errorf("IsStrongIdentifier(%#v) = true", identifier)
		}
	}
	valid := []IdentifierKey{
		builtinIdentifierKey(t, "doi", "10.1234/example"),
		builtinIdentifierKey(t, "arxiv", "2301.01234"),
		builtinIdentifierKey(t, "arxiv", "hep-th/9901001"),
		builtinIdentifierKey(t, "wos", "WOS:000123456789"),
		builtinIdentifierKey(t, "pmid", "12345678"),
		builtinIdentifierKey(t, "pmcid", "PMC1234567"),
		builtinIdentifierKey(t, "handle", "20.500.12345/example"),
	}
	for _, identifier := range valid {
		if !IsStrongIdentifier(identifier) {
			t.Errorf("IsStrongIdentifier(%#v) = false", identifier)
		}
	}
}

func TestTitleNormalizationAndSimilarity(t *testing.T) {
	t.Parallel()
	if got, want := normalizeTitle("<i>An&nbsp;Example:</i> A Test"), "an example a test"; got != want {
		t.Fatalf("normalizeTitle() = %q, want %q", got, want)
	}
	left := normalizeTitle("Machine learning methods for identifying duplicate scholarly records")
	right := normalizeTitle("Machine learning method for identifying duplicate scholarly records")
	if score := titleSimilarity(left, right); score < 92 {
		t.Fatalf("titleSimilarity() = %d, want at least 92", score)
	}
}

func TestQueryFromRecordIsDeterministic(t *testing.T) {
	t.Parallel()
	record := testRecord(
		"A sufficiently long title for deterministic lookup",
		2024,
		[]string{"Zed, Alice", "Able, Bob"},
		identifier(hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "DOI:10.1234/ABC"),
	)
	query := QueryFromRecord("source-row-7", record)
	if query.Key != "source-row-7" || query.Year != 2024 {
		t.Fatalf("unexpected query identity: %#v", query)
	}
	if got, want := query.Authors, []string{"Able, Bob", "Zed, Alice"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("query authors = %#v, want %#v", got, want)
	}
	if got, want := query.Identifiers, []IdentifierKey{builtinIdentifierKey(t, "doi", "10.1234/abc")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("query identifiers = %#v, want %#v", got, want)
	}
}

func TestPrimaryYearFallsBackFromEmptyPreferredDate(t *testing.T) {
	t.Parallel()
	record := &hubv1.Record{Dates: []*hubv1.DateValue{
		{Type: hubv1.DateType_DATE_TYPE_ISSUED},
		{Type: hubv1.DateType_DATE_TYPE_CREATED, Year: 2022},
	}}
	if got := primaryYear(record); got != 2022 {
		t.Fatalf("primaryYear() = %d, want 2022", got)
	}
}

func TestContributorORCIDCanonicalization(t *testing.T) {
	t.Parallel()
	record := testRecord("A shared title with an ORCID author", 2024, []string{"Doe, Jane"})
	record.Contributors[0].Identifiers = []*hubv1.Identifier{{
		Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID, Value: "https://orcid.org/0000-0002-1825-0097",
	}}
	authors := normalizedAuthors(record)
	if got, want := authors[0].ORCIDs, []string{"0000-0002-1825-0097"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ORCIDs = %#v, want %#v", got, want)
	}
}

func identifier(identifierType hubv1.IdentifierType, value string) *hubv1.Identifier {
	return &hubv1.Identifier{Type: identifierType, Value: value}
}

func builtinIdentifierKey(t *testing.T, scheme, value string) IdentifierKey {
	t.Helper()
	identifier, err := hub.DefaultIdentifierRegistry().NewIdentifierForScheme(value, scheme, hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED)
	if err != nil {
		t.Fatal(err)
	}
	return identifierKey(identifier)
}

func testRecord(title string, year int32, authors []string, identifiers ...*hubv1.Identifier) *hubv1.Record {
	record := &hubv1.Record{Title: title, Identifiers: identifiers}
	for _, author := range authors {
		record.Contributors = append(record.Contributors, &hubv1.Contributor{Name: author, Role: "author"})
	}
	if year != 0 {
		record.Dates = []*hubv1.DateValue{{Type: hubv1.DateType_DATE_TYPE_ISSUED, Year: year}}
	}
	return record
}

func testReportProvenance() ReportProvenance {
	registry := hub.DefaultIdentifierRegistry()
	return ReportProvenance{
		IdentifierRegistryVersion: registry.Version(),
		IdentifierRegistryDigest:  registry.Digest(),
	}
}
