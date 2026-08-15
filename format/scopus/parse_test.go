package scopus

import (
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

const pageFixture = `{
  "search-results": {
    "opensearch:totalResults": "101",
    "opensearch:startIndex": "0",
    "opensearch:itemsPerPage": "1",
    "cursor": {"@current":"current","@next":"next"},
    "entry": [{
      "eid": "2-s2.0-85123456789",
      "dc:identifier": "SCOPUS_ID:85123456789",
      "dc:title": "A Scopus work",
      "dc:description": "<p>An abstract.</p>",
      "prism:publicationName": "Journal of Tests",
      "prism:issn": "12345678",
      "prism:eIssn": "87654321",
      "prism:isbn": [{"$":"978-1-2345-6789-0"}],
      "prism:volume": "12",
      "prism:issueIdentifier": "3",
      "prism:pageRange": "40-52",
      "prism:coverDate": "2024-04-03",
      "prism:doi": "https://doi.org/10.1234/TEST.1",
      "prism:aggregationType": "Journal",
      "subtype": "ar",
      "subtypeDescription": "Article",
      "citedby-count": "7",
      "openaccess": "1",
      "link": [{"@ref":"scopus","@href":"https://www.scopus.com/record/85123456789"}],
      "affiliation": [{"afid":"6001","affilname":"Example University"}],
      "author": [{
        "authid":"7001", "authname":"Smith, Jane", "given-name":"Jane", "surname":"Smith",
        "orcid":"https://orcid.org/0000-0002-1825-0097", "afid":[{"$":"6001"}]
      }],
      "authkeywords": "Metadata | Repositories | metadata",
      "subject-area": [{"@code":"1710","$":"Information Systems"}]
    }]
  }
}`

func TestParsePageMapsScopusMetadataAndIdentity(t *testing.T) {
	page, err := (&Format{}).ParsePage(strings.NewReader(pageFixture), &format.ParseOptions{SourceName: "https://api.elsevier.test/search"})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalResults != 101 || page.StartIndex != 0 || page.ItemsPerPage != 1 || page.NextCursor != "next" {
		t.Fatalf("page metadata = %+v", page)
	}
	if len(page.Records) != 1 {
		t.Fatalf("records = %d", len(page.Records))
	}
	record := page.Records[0]
	if record.Title != "A Scopus work" || record.Abstract != "An abstract." {
		t.Fatalf("record title/abstract = %q / %q", record.Title, record.Abstract)
	}
	if record.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE || record.Publication.Title != "Journal of Tests" || record.Publication.Pages != "40-52" {
		t.Fatalf("resource/publication = %+v / %+v", record.ResourceType, record.Publication)
	}
	if record.Publication.Issn != "12345678" || hub.GetExtraString(record, "publication_eissn") != "87654321" {
		t.Fatalf("publication identifiers = %+v / extras=%+v", record.Publication, hub.GetExtraFields(record))
	}
	isbns, exists := hub.GetExtra(record, "publication_isbn")
	values, stringsOK := isbns.([]any)
	if !exists || !stringsOK || len(values) != 1 || values[0] != "9781234567890" {
		t.Fatalf("publication ISBNs = %#v", isbns)
	}
	for _, identifier := range record.Identifiers {
		if identifier.Scheme == "issn" || identifier.Scheme == "isbn" {
			t.Fatalf("container identifier was attached to work: %+v", identifier)
		}
	}
	if len(record.Dates) != 1 || record.Dates[0].Year != 2024 || record.Dates[0].Month != 4 || record.Dates[0].Day != 3 {
		t.Fatalf("dates = %+v", record.Dates)
	}
	if len(record.Contributors) != 1 || record.Contributors[0].ParsedName.Family != "Smith" || record.Contributors[0].Affiliation != "Example University" {
		t.Fatalf("contributors = %+v", record.Contributors)
	}
	if len(record.Contributors[0].Identifiers) != 1 || record.Contributors[0].Identifiers[0].Type != hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID {
		t.Fatalf("contributor identifiers = %+v", record.Contributors[0].Identifiers)
	}
	if len(record.Subjects) != 3 {
		t.Fatalf("subjects = %+v", record.Subjects)
	}
	if record.SourceInfo.SourceId != "2-s2.0-85123456789" || record.SourceInfo.SourceUri != "https://www.scopus.com/record/85123456789" {
		t.Fatalf("source info = %+v", record.SourceInfo)
	}
	assertIdentifier(t, record, "doi", "10.1234/test.1", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK, true)
	assertIdentifier(t, record, "scopus-eid", "2-s2.0-85123456789", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD, false)
}

func TestParseSingleEntryAndFallbackCreator(t *testing.T) {
	input := `{"dc:identifier":"SCOPUS_ID:12345","dc:title":"A record","dc:creator":"Doe, J.","subtype":"cp"}`
	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records[0].Contributors) != 1 || records[0].ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER {
		t.Fatalf("records = %+v", records)
	}
	assertIdentifier(t, records[0], "scopus-id", "12345", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD, true)
}

func TestSourceNameDoesNotReplaceCanonicalRecordURI(t *testing.T) {
	records, err := (&Format{}).Parse(strings.NewReader(`{"dc:title":"One","link":[{"@ref":"scopus","@href":"https://www.scopus.com/record/1"}]}`), &format.ParseOptions{SourceName: "records-page.json"})
	if err != nil {
		t.Fatal(err)
	}
	if got := records[0].SourceInfo.SourceUri; got != "https://www.scopus.com/record/1" {
		t.Fatalf("source URI = %q", got)
	}
}

func TestSourceURIIsSafeToPersist(t *testing.T) {
	t.Parallel()

	input := `{"dc:title":"One","link":[{"@ref":"scopus","@href":"https://user:password@WWW.SCOPUS.COM/record/1?page=2&api_key=secret#metadata"}]}`
	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := records[0].SourceInfo.SourceUri, "https://www.scopus.com/record/1?page=2"; got != want {
		t.Fatalf("source URI = %q, want %q", got, want)
	}
	assertIdentifier(t, records[0], "url", "https://www.scopus.com/record/1?page=2", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED, false)

	input = `{"dc:title":"One","link":[{"@ref":"scopus","@href":"javascript:alert(1)"}]}`
	if _, err := (&Format{}).Parse(strings.NewReader(input), nil); err == nil || !strings.Contains(err.Error(), "HTTP or HTTPS") {
		t.Fatalf("Parse() error = %v, want unsafe source URI error", err)
	}
}

func TestParseRejectsErrorAndMeaninglessEntries(t *testing.T) {
	for _, input := range []string{
		`{"search-results":{"service-error":{"status":{"statusCode":"AUTHENTICATION_ERROR"}}}}`,
		`{"search-results":{"entry":[{"@status":"RESOURCE_NOT_FOUND"}]}}`,
		`{"search-results":{"entry":[{}]}}`,
		`{} {}`,
	} {
		if _, err := (&Format{}).Parse(strings.NewReader(input), nil); err == nil {
			t.Fatalf("Parse(%s) expected error", input)
		}
	}
}

func assertIdentifier(t *testing.T, record interface{ GetIdentifiers() []*hubv1.Identifier }, scheme, value string, level hubv1.IdentifierIdentityLevel, preferred bool) {
	t.Helper()
	for _, identifier := range record.GetIdentifiers() {
		if identifier.Scheme == scheme && identifier.Value == value {
			if identifier.IdentityLevel != level || identifier.IsPreferred != preferred {
				t.Fatalf("identifier = %+v", identifier)
			}
			return
		}
	}
	t.Fatalf("identifier %s:%s not found in %+v", scheme, value, record.GetIdentifiers())
}
