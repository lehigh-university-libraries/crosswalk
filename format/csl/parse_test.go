package csl

import (
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestParseDOIResolvedCSL(t *testing.T) {
	t.Parallel()
	input := `{
		"id":"https://doi.org/10.1234/example",
		"type":"article-journal",
		"title":"A useful article",
		"abstract":"An abstract",
		"author":[{"given":"Ada","family":"Lovelace"}],
		"editor":[{"literal":"Example Editors"}],
		"issued":{"date-parts":[[2024,2,29]]},
		"DOI":"10.1234/example",
		"URL":"https://example.test/article",
		"ISSN":"1234-567X",
		"publisher":"Example Press",
		"publisher-place":"Bethlehem",
		"container-title":"Journal of Examples",
		"volume":"12",
		"issue":"3",
		"page":"10-20",
		"keyword":"metadata; repositories"
	}`

	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d", len(records))
	}
	record := records[0]
	if record.Title != "A useful article" || record.Publisher != "Example Press" {
		t.Fatalf("record = %#v", record)
	}
	if record.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE {
		t.Errorf("resource type = %v", record.ResourceType.Type)
	}
	if len(record.Contributors) != 2 || record.Contributors[0].RoleCode != "relators:aut" {
		t.Errorf("contributors = %#v", record.Contributors)
	}
	if len(record.Dates) != 1 || record.Dates[0].Day != 29 {
		t.Errorf("dates = %#v", record.Dates)
	}
	if record.Publication == nil || record.Publication.Issue != "3" || record.Publication.Pages != "10-20" {
		t.Errorf("publication = %#v", record.Publication)
	}
	if len(record.Subjects) != 2 {
		t.Errorf("subjects = %#v", record.Subjects)
	}
	if record.SourceInfo.SourceId != "10.1234/example" {
		t.Errorf("source id = %q", record.SourceInfo.SourceId)
	}
}

func TestParseCSLArrayAndMissingDate(t *testing.T) {
	t.Parallel()
	records, err := (&Format{}).Parse(strings.NewReader(`[
		{"id":"one","type":"book","title":"One"},
		{"id":"two","type":"report","title":"Two","issued":{"date-parts":[]}}
	]`), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d", len(records))
	}
	if len(records[1].Dates) != 0 {
		t.Fatalf("dates = %#v", records[1].Dates)
	}
}

func TestParseDOIResolvedCSLAcceptsIdentifierArrays(t *testing.T) {
	t.Parallel()
	input := `{
		"type":"journal-article",
		"title":"A live-shaped DOI response",
		"DOI":"10.1038/example",
		"ISSN":["0028-0836","1476-4687"],
		"ISBN":["978-1-4028-9462-6","1-4028-9462-7"],
		"issued":{"date-parts":[[2024,8,1]]}
	}`

	records, err := (&Format{}).Parse(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d", len(records))
	}

	identifiers := make(map[hubv1.IdentifierType][]string)
	for _, identifier := range records[0].Identifiers {
		identifiers[identifier.Type] = append(identifiers[identifier.Type], identifier.Value)
	}
	if got := identifiers[hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN]; !equalStrings(got, []string{"0028-0836", "1476-4687"}) {
		t.Errorf("ISSNs = %#v", got)
	}
	if got := identifiers[hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN]; !equalStrings(got, []string{"978-1-4028-9462-6", "1-4028-9462-7"}) {
		t.Errorf("ISBNs = %#v", got)
	}
	if records[0].Publication == nil || records[0].Publication.Issn != "0028-0836" {
		t.Errorf("publication = %#v", records[0].Publication)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestParseCSLRejectsTrailingDocument(t *testing.T) {
	t.Parallel()
	_, err := (&Format{}).Parse(strings.NewReader(`{"id":"one","type":"book"} {"id":"two","type":"book"}`), nil)
	if err == nil || !strings.Contains(err.Error(), "multiple top-level values") {
		t.Fatalf("Parse() error = %v", err)
	}
}

func TestCanParseUsesTopLevelCSLKeys(t *testing.T) {
	t.Parallel()

	parser := &Format{}
	for _, input := range []string{
		`{"type":"article-journal","title":"One"}`,
		`[{"id":"one","type":"book"}]`,
	} {
		if !parser.CanParse([]byte(input)) {
			t.Fatalf("CanParse(%s) = false", input)
		}
	}
	for _, input := range []string{
		`{"id":44,"metadata":{"type":"dataset","title":"Zenodo"}}`,
		`{"type":"article"}`,
		`[{"id":"one","title":"missing type"}]`,
		`{"type":"article","title":"One"} trailing`,
	} {
		if parser.CanParse([]byte(input)) {
			t.Fatalf("CanParse(%s) = true", input)
		}
	}
}
