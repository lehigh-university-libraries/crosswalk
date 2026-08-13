package crossrefrest

import (
	"io"
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestParseSearchResponse(t *testing.T) {
	input := `{"message":{"items":[{"DOI":"https://doi.org/10.1234/EXAMPLE","URL":"https://doi.org/10.1234/example","title":["A title"],"subtitle":["A subtitle"],"abstract":"<jats:p>An abstract</jats:p>","type":"journal-article","publisher":"A publisher","author":[{"given":"Ada","family":"Smith","ORCID":"https://orcid.org/0000-0001-2345-6789","affiliation":[{"name":"Lehigh"}]}],"container-title":["Journal"],"volume":"2","issue":"3","page":"4-9","ISSN":["1234-567X"],"published-print":{"date-parts":[[2024,5,6]]},"subject":["Repositories"],"license":[{"URL":"https://creativecommons.org/licenses/by/4.0/"}],"link":[{"URL":"https://example.test/paper.pdf","content-type":"application/pdf"}]}]}}`
	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
	record := records[0]
	if record.Title != "A title" || record.Abstract != "An abstract" || record.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE {
		t.Fatalf("unexpected record: %#v", record)
	}
	if record.Identifiers[0].Value != "10.1234/example" || record.Identifiers[0].IdentityLevel != hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK || record.Contributors[0].ParsedName.Family != "Smith" {
		t.Errorf("identifier/contributor mapping incomplete: %#v %#v", record.Identifiers, record.Contributors)
	}
	if record.Dates[0].Year != 2024 || record.Dates[0].Month != 5 || record.Dates[0].Day != 6 {
		t.Errorf("date mapping incomplete: %#v", record.Dates[0])
	}
	if record.Rights[0].Uri == "" || record.Extra.Fields["pdf_url"].GetStringValue() == "" {
		t.Errorf("rights/PDF mapping incomplete: %#v", record)
	}
}

func TestParseRejectsTrailingJSONOrData(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{`{"message":{"items":[]}}`, "not-json"} {
		input := `{"message":{"items":[]}}` + suffix
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
