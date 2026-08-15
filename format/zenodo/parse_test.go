package zenodo

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

const recordFixture = `{
  "id": 8435696,
  "conceptrecid": "8435695",
  "doi": "10.5281/zenodo.8435696",
  "conceptdoi": "10.5281/zenodo.8435695",
  "created": "2023-10-12T14:26:07Z",
  "modified": "2023-10-13T10:00:00Z",
  "status": "published",
  "metadata": {
    "title": "A Zenodo dataset",
    "additional_titles": [{"title":"Alternate title","type":{"id":"subtitle"}}],
    "publication_date": "2023-10-10",
    "description": "<p>Dataset description.</p>",
    "publisher": "Zenodo",
    "language": "eng",
    "version": "1.2.0",
    "resource_type": {"title":"Dataset","type":"dataset"},
    "creators": [{"name":"Smith, Jane","orcid":"0000-0002-1825-0097","affiliation":"Example University"}],
    "contributors": [{
      "person_or_org":{"name":"Curation Office","type":"organizational"},
      "role":{"id":"DataCurator"},
      "affiliations":[{"name":"Example Library","id":"https://ror.org/123","scheme":"ror"}]
    }],
    "keywords": ["Repositories", "Metadata"],
    "subjects": [{"subject":"Digital libraries","identifier":"https://example.test/subjects/1","scheme":"local"}],
    "license": {"id":"cc-by-4.0","title":"Creative Commons Attribution 4.0","url":"https://creativecommons.org/licenses/by/4.0/"},
    "related_identifiers": [{"identifier":"10.1234/related","relation":"isSupplementTo","resource_type":{"type":"dataset"}}],
    "dates": [{"date":"2023-09","type":{"id":"Collected"}}],
    "grants": [{"code":"ABC-123","title":"Metadata project","funder":{"name":"Example Funder","doi":"10.13039/100000001"}}]
  },
  "links": {
    "self":"https://zenodo.org/api/records/8435696",
    "self_html":"https://zenodo.org/records/8435696"
  },
  "files": [{
    "id":"file-1","key":"data.csv","size":1234,"checksum":"md5:abcdef",
    "type":"text/csv","links":{"self":"https://zenodo.org/api/records/8435696/files/data.csv/content"}
  }]
}`

func TestParseMapsZenodoRecordMetadataAndIdentity(t *testing.T) {
	records, err := (&Format{}).Parse(strings.NewReader(recordFixture), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
	record := records[0]
	if record.Title != "A Zenodo dataset" || record.Abstract != "Dataset description." || record.Publisher != "Zenodo" || record.Language != "eng" {
		t.Fatalf("core metadata = %+v", record)
	}
	if record.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET || len(record.AltTitle) != 1 {
		t.Fatalf("type/alternate titles = %+v / %+v", record.ResourceType, record.AltTitle)
	}
	if len(record.Contributors) != 2 || record.Contributors[0].ParsedName.Family != "Smith" || record.Contributors[1].Type != hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION {
		t.Fatalf("contributors = %+v", record.Contributors)
	}
	if record.Contributors[0].Affiliation != "Example University" || record.Contributors[1].Affiliation != "Example Library" {
		t.Fatalf("contributor affiliations = %+v", record.Contributors)
	}
	if len(record.Dates) != 4 || len(record.Subjects) != 3 || len(record.Rights) != 1 || len(record.Funders) != 1 {
		t.Fatalf("dates/subjects/rights/funders = %d/%d/%d/%d", len(record.Dates), len(record.Subjects), len(record.Rights), len(record.Funders))
	}
	if len(record.Files) != 1 || record.Files[0].Name != "data.csv" || record.Files[0].ChecksumAlgorithm != "md5" || record.Files[0].AccessUrl != "https://zenodo.org/api/records/8435696/files/data.csv/content" {
		t.Fatalf("files = %+v", record.Files)
	}
	if record.SourceInfo.SourceId != "8435696" || record.SourceInfo.SourceUri != "https://zenodo.org/api/records/8435696" {
		t.Fatalf("source info = %+v", record.SourceInfo)
	}
	assertIdentifier(t, record, "doi", "10.5281/zenodo.8435696", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION, true)
	assertIdentifier(t, record, "doi", "10.5281/zenodo.8435695", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT, false)
	assertIdentifier(t, record, "zenodo-record", "8435696", hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION, false)
	if len(record.Relations) != 2 || record.Relations[0].Type != hubv1.RelationType_RELATION_TYPE_VERSION_OF || record.Relations[1].Type != hubv1.RelationType_RELATION_TYPE_IS_SUPPLEMENT_TO {
		t.Fatalf("relations = %+v", record.Relations)
	}
}

func TestParseSearchPageAndNewCreatorShape(t *testing.T) {
	input := `{"hits":{"total":{"value":1},"hits":[{
	      "recid":"44","conceptrecid":"43","metadata":{"title":"One","resource_type":{"type":"software"},
	      "creators":{"person_or_org":{"name":"Doe, John","given_name":"John","family_name":"Doe","type":"personal","identifiers":[{"scheme":"orcid","identifier":"0000-0002-1825-0097"}]}}}
	    }]}}`
	records, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{SourceName: "records-page.json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE || records[0].SourceInfo.SourceUri != "" {
		t.Fatalf("records = %+v", records)
	}
	if len(records[0].Contributors) != 1 || records[0].Contributors[0].ParsedName.Given != "John" || len(records[0].Contributors[0].Identifiers) != 1 {
		t.Fatalf("contributors = %+v", records[0].Contributors)
	}
}

func TestParseSkipsEmptyCreatorAndContributorObjects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		creditsJSON string
		wantNames   []string
	}{
		{name: "single empty creator object", creditsJSON: `"creators": {}`},
		{name: "single empty contributor object", creditsJSON: `"contributors": {}`},
		{name: "empty entries in arrays", creditsJSON: `"creators": [{}, {"name":"Doe, Jane"}], "contributors": [{}, {"name":"Example Organization", "type":"organizational"}]`, wantNames: []string{"Doe, Jane", "Example Organization"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := `{"id":44,"metadata":{"title":"One",` + test.creditsJSON + `}}`
			records, err := (&Format{}).Parse(strings.NewReader(input), nil)
			if err != nil {
				t.Fatal(err)
			}
			contributors := records[0].GetContributors()
			if len(contributors) != len(test.wantNames) {
				t.Fatalf("contributors = %+v, want names %v", contributors, test.wantNames)
			}
			for index, want := range test.wantNames {
				if got := contributors[index].GetName(); got != want {
					t.Errorf("contributor %d name = %q, want %q", index, got, want)
				}
			}
		})
	}
}

func TestZenodoRelationUsesCanonicalIdentifierResolvers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		identifier    string
		wantID        string
		wantType      hubv1.IdentifierType
		wantTargetURI string
	}{
		{name: "Handle", identifier: "hdl:20.500.12345/example", wantID: "20.500.12345/example", wantType: hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE, wantTargetURI: "https://hdl.handle.net/20.500.12345/example"},
		{name: "ISBN", identifier: "ISBN-13: 978-0-306-40615-7", wantID: "9780306406157", wantType: hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN, wantTargetURI: "urn:isbn:9780306406157"},
		{name: "ISSN", identifier: "ISSN: 2049-3630", wantID: "2049-3630", wantType: hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN, wantTargetURI: "urn:issn:2049-3630"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			relation := zenodoRelation(related{Identifier: test.identifier, Relation: "references"})
			if relation == nil {
				t.Fatal("zenodoRelation() = nil")
			}
			if relation.GetTargetId() != test.wantID || relation.GetTargetIdType() != test.wantType || relation.GetTargetUri() != test.wantTargetURI {
				t.Fatalf("relation = %v, want id %q, type %s, URI %q", relation, test.wantID, test.wantType, test.wantTargetURI)
			}
		})
	}
}

func TestParseRetainsFunderIdentifierSchemes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		identifier string
		wantScheme string
	}{
		{name: "DOI", identifier: "10.13039/100000001", wantScheme: "doi"},
		{name: "ROR", identifier: "https://ror.org/02mhbdp94", wantScheme: "ror"},
		{name: "ISNI", identifier: "https://isni.org/isni/000000012124423X", wantScheme: "isni"},
		{name: "GND", identifier: "https://d-nb.info/gnd/118540238", wantScheme: "gnd"},
		{name: "unrecognized", identifier: "local-funder-id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := fmt.Sprintf(`{"id":44,"metadata":{"title":"One","grants":{"funder":{"name":"Example Funder","id":%q}}}}`, test.identifier)
			records, err := (&Format{}).Parse(strings.NewReader(input), nil)
			if err != nil {
				t.Fatal(err)
			}
			funders := records[0].GetFunders()
			if len(funders) != 1 {
				t.Fatalf("funders = %+v", funders)
			}
			if got := funders[0].GetIdentifierType(); got != test.wantScheme {
				t.Fatalf("identifier type = %q, want %q", got, test.wantScheme)
			}
		})
	}
}

func TestParseWarnsWhenDroppingMalformedDates(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	input := `{
	  "id": 44,
	  "created": "bad-created",
	  "modified": "bad-modified",
	  "metadata": {
	    "title": "One",
	    "publication_date": "not-a-date",
	    "dates": [
	      {"date":"2023-99-99","type":"collected"},
	      {"date":"still-not-a-date","type":"available"}
	    ]
	  }
	}`
	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records[0].GetDates()) != 0 {
		t.Fatalf("dates = %+v, want none", records[0].GetDates())
	}
	logged := output.String()
	for _, want := range []string{
		`msg="ignoring malformed Zenodo date"`,
		`field=metadata.publication_date count=1 source_id=44`,
		`field=metadata.dates.date count=2 source_id=44`,
		`field=created count=1 source_id=44`,
		`field=modified count=1 source_id=44`,
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("log output does not contain %q:\n%s", want, logged)
		}
	}
	if got := strings.Count(logged, "field=metadata.dates.date"); got != 1 {
		t.Errorf("metadata date warnings = %d, want one aggregated warning:\n%s", got, logged)
	}

	output.Reset()
	parseZenodoDate("bad", hubv1.DateType_DATE_TYPE_PUBLISHED, "metadata.publication_date", "access_token=secret")
	if logged := output.String(); strings.Contains(logged, "access_token") || strings.Contains(logged, "secret") {
		t.Fatalf("malformed-date warning leaked a non-numeric source ID: %s", logged)
	}
}

func TestSourceNameDoesNotReplaceCanonicalRecordURI(t *testing.T) {
	input := `{"id":44,"links":{"self":"https://zenodo.org/api/records/44"},"metadata":{"title":"One"}}`
	records, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{SourceName: "records-page.json"})
	if err != nil {
		t.Fatal(err)
	}
	if got := records[0].SourceInfo.SourceUri; got != "https://zenodo.org/api/records/44" {
		t.Fatalf("source URI = %q", got)
	}
}

func TestSourceURIIsSafeToPersist(t *testing.T) {
	t.Parallel()

	input := `{"id":44,"links":{"self":"https://user:password@ZENODO.ORG/api/records/44?page=2&access_token=secret#metadata"},"metadata":{"title":"One"}}`
	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := records[0].SourceInfo.SourceUri, "https://zenodo.org/api/records/44?page=2"; got != want {
		t.Fatalf("source URI = %q, want %q", got, want)
	}

	input = `{"id":44,"links":{"self":"file:///tmp/record.json"},"metadata":{"title":"One"}}`
	if _, err := (&Format{}).Parse(strings.NewReader(input), nil); err == nil || !strings.Contains(err.Error(), "HTTP or HTTPS") {
		t.Fatalf("Parse() error = %v, want unsafe source URI error", err)
	}
}

func TestFileURLsAreSanitizedAndSelfIsDownloadFallback(t *testing.T) {
	t.Parallel()

	input := `{"id":44,"metadata":{"title":"One"},"files":[{"key":"one.pdf","links":{"self":"https://user:password@ZENODO.ORG/api/records/44/files/one.pdf/content?download=1&access_token=secret#part"}}]}`
	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://zenodo.org/api/records/44/files/one.pdf/content?download=1"
	if len(records[0].Files) != 1 || records[0].Files[0].Uri != want || records[0].Files[0].AccessUrl != want {
		t.Fatalf("files = %+v, want sanitized self URL %q", records[0].Files, want)
	}

	input = `{"id":44,"metadata":{"title":"One"},"files":[{"key":"one.pdf","links":{"self":"file:///tmp/one.pdf"}}]}`
	if _, err := (&Format{}).Parse(strings.NewReader(input), nil); err == nil || !strings.Contains(err.Error(), "HTTP or HTTPS") {
		t.Fatalf("Parse() error = %v, want unsafe file URL error", err)
	}
}

func TestZenodoCurrentResourceVocabulary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		identifier string
		want       hubv1.ResourceTypeValue
	}{
		{"publication-annotationcollection", hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION},
		{"publication-book", hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK},
		{"publication-section", hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER},
		{"software-computationalnotebook", hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE},
		{"publication-conferencepaper", hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER},
		{"publication-conferenceproceeding", hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PROCEEDING},
		{"publication-datapaper", hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE},
		{"dataset", hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET},
		{"image-diagram", hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE},
		{"image-drawing", hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE},
		{"event", hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER},
		{"image-figure", hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE},
		{"image", hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE},
		{"publication-journal", hubv1.ResourceTypeValue_RESOURCE_TYPE_JOURNAL},
		{"publication-article", hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE},
		{"lesson", hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT},
		{"model", hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER},
		{"image-other", hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE},
		{"other", hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER},
		{"publication-other", hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER},
		{"publication-datamanagementplan", hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT},
		{"publication-patent", hubv1.ResourceTypeValue_RESOURCE_TYPE_PATENT},
		{"publication-peerreview", hubv1.ResourceTypeValue_RESOURCE_TYPE_PEER_REVIEW},
		{"image-photo", hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE},
		{"physicalobject", hubv1.ResourceTypeValue_RESOURCE_TYPE_OBJECT},
		{"image-plot", hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE},
		{"poster", hubv1.ResourceTypeValue_RESOURCE_TYPE_POSTER},
		{"publication-preprint", hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT},
		{"presentation", hubv1.ResourceTypeValue_RESOURCE_TYPE_PRESENTATION},
		{"publication-deliverable", hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT},
		{"publication-milestone", hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT},
		{"publication-proposal", hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT},
		{"publication", hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE},
		{"publication-report", hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT},
		{"software", hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE},
		{"publication-softwaredocumentation", hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT},
		{"publication-standard", hubv1.ResourceTypeValue_RESOURCE_TYPE_STANDARD},
		{"publication-taxonomictreatment", hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE},
		{"publication-technicalnote", hubv1.ResourceTypeValue_RESOURCE_TYPE_TECHNICAL_REPORT},
		{"publication-dissertation", hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS},
		{"video", hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO},
		{"workflow", hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE},
		{"publication-workingpaper", hubv1.ResourceTypeValue_RESOURCE_TYPE_WORKING_PAPER},
	}
	for _, test := range tests {
		t.Run(test.identifier, func(t *testing.T) {
			typeName, subtype, found := strings.Cut(test.identifier, "-")
			if !found {
				typeName, subtype = test.identifier, ""
			}
			value := resourceType{Type: typeName, Subtype: subtype}
			if got := zenodoStructuredResourceType(value); got != test.want {
				t.Fatalf("zenodoStructuredResourceType(%+v) = %s, want %s", value, got, test.want)
			}
			if got := zenodoResourceTypeIdentifier(value); got != test.identifier {
				t.Fatalf("resource type identifier = %q, want %q", got, test.identifier)
			}
		})
	}
}

func TestParseRejectsMeaninglessAndTrailingRecords(t *testing.T) {
	for _, input := range []string{`{}`, `{} {}`, `{"hits":{"hits":[{}]}}`} {
		if _, err := (&Format{}).Parse(strings.NewReader(input), nil); err == nil {
			t.Fatalf("Parse(%s) expected error", input)
		}
	}
}

func TestCanParseRecognizesRecordsAndEmptySearchPagesStructurally(t *testing.T) {
	t.Parallel()

	parser := &Format{}
	for _, input := range []string{
		`{"id":44,"metadata":{"title":"One"}}`,
		`{"hits":{"total":{"value":0},"hits":[]}}`,
		"{\n  \"hits\" : {\n    \"total\" : 0,\n    \"hits\" : []\n  }\n}",
	} {
		if !parser.CanParse([]byte(input)) {
			t.Fatalf("CanParse(%s) = false", input)
		}
	}
	for _, input := range []string{
		`{"hits":[]}`,
		`{"metadata":{"title":"unidentified"}}`,
		`{"message":{"items":[]}}`,
		`{"hits":{"total":0,"hits":[]}} trailing`,
	} {
		if parser.CanParse([]byte(input)) {
			t.Fatalf("CanParse(%s) = true", input)
		}
	}
}

func assertIdentifier(t *testing.T, record interface{ GetIdentifiers() []*hubv1.Identifier }, scheme, value string, level hubv1.IdentifierIdentityLevel, preferred bool) {
	t.Helper()
	for _, identifier := range record.GetIdentifiers() {
		if identifier.Scheme == scheme && identifier.Value == value && identifier.IdentityLevel == level {
			if identifier.IsPreferred != preferred {
				t.Fatalf("identifier = %+v", identifier)
			}
			return
		}
	}
	t.Fatalf("identifier %s:%s/%s not found in %+v", scheme, value, level, record.GetIdentifiers())
}
