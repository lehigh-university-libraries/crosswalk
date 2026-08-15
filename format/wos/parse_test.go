package wos

import (
	"io"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestParseStarterPage(t *testing.T) {
	input := `{"metadata":{"total":1,"page":1,"limit":10},"hits":[{"uid":"WOS:000222526200005","title":"A paper","types":["Article"],"source":{"sourceTitle":"Journal","publishYear":2024,"publishMonth":"APR","volume":"3","issue":"2","pages":{"range":"10-19"}},"names":{"authors":[{"displayName":"Smith, Ada","researcherId":"ABC-123"}]},"identifiers":{"doi":"https://doi.org/10.1234/EXAMPLE","issn":"1234-567X"},"keywords":{"authorKeywords":["Repositories"]}}]}`
	records, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{SourceName: "https://example.test/documents?q=x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Title != "A paper" || record.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE {
		t.Fatalf("unexpected record: %#v", record)
	}
	if got := record.Identifiers[0].Value; got != "WOS:000222526200005" {
		t.Errorf("WOS identifier = %q", got)
	}
	if got := record.Identifiers[1].Value; got != "10.1234/example" {
		t.Errorf("DOI = %q", got)
	}
	if record.Dates[0].Month != 4 || record.Publication.Pages != "10-19" {
		t.Errorf("publication mapping incomplete: %#v %#v", record.Dates[0], record.Publication)
	}
	if record.Contributors[0].ParsedName.Family != "Smith" || record.Contributors[0].SourceId != "ABC-123" {
		t.Errorf("author mapping incomplete: %#v", record.Contributors[0])
	}
}

func TestParseEmptyStarterPage(t *testing.T) {
	records, err := (&Format{}).Parse(strings.NewReader(`{"metadata":{"total":0},"hits":[]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %d, want 0", len(records))
	}
}

func TestParseRejectsTrailingJSONOrData(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{`{"uid":"WOS:2"}`, "not-json"} {
		input := `{"uid":"WOS:1"}` + suffix
		if _, err := (&Format{}).Parse(strings.NewReader(input), nil); err == nil {
			t.Fatalf("Parse(%q) succeeded, want trailing-data error", suffix)
		}
	}
}

func TestReadJSONDocumentEnforcesWholeDocumentLimit(t *testing.T) {
	t.Parallel()
	_, err := readJSONDocument(io.LimitReader(strings.NewReader("123456"), 6), 5)
	if err == nil || !strings.Contains(err.Error(), "exceeds 5 bytes") {
		t.Fatalf("readJSONDocument() error = %v", err)
	}
}
