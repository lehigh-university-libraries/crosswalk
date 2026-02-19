package csl_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/format/csl"
	"github.com/lehigh-university-libraries/crosswalk/format/drupal"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

// loadFixture reads a JSON fixture from the testdata directory.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("loading fixture %s: %v", name, err)
	}
	return data
}

// TestDrupalToCSL_PartDetail verifies that field_part_detail entries for
// volume, issue, page, and section all reach the correct places in CSL-JSON
// after a full Drupal → hub → CSL pipeline.
//
// Fixture: Watson & Crick (1953), Nature 171(4356):737-738, doi:10.1038/171737a0
func TestDrupalToCSL_PartDetail(t *testing.T) {
	fixture := loadFixture(t, "watson_crick_1953.json")

	drupalFmt := &drupal.Format{}
	records, err := drupalFmt.Parse(bytes.NewReader(fixture), format.NewParseOptions())
	if err != nil {
		t.Fatalf("parsing drupal JSON: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	record := records[0]

	// ── Hub-level assertions ────────────────────────────────────────────────
	// Verify the hub record carries the right publication fields before any
	// serializer runs, so a failing test pinpoints parse vs. serialize.

	if record.Publication == nil {
		t.Fatal("record.Publication is nil; field_part_detail and field_related_item were not mapped")
	}

	cases := []struct {
		field string
		got   string
		want  string
	}{
		{"Publication.Volume", record.Publication.Volume, "171"},
		{"Publication.Issue", record.Publication.Issue, "4356"},
		{"Publication.Pages", record.Publication.Pages, "737-738"},
		{"Publication.Title", record.Publication.Title, "Nature"},
		{"Publication.LIssn", record.Publication.LIssn, "0028-0836"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.field, tc.got, tc.want)
		}
	}

	// section lands in extra, not Publication
	if sn := hub.GetExtraString(record, "section_number"); sn != "1" {
		t.Errorf("extra.section_number = %q, want %q", sn, "1")
	}
	if st := hub.GetExtraString(record, "section_title"); st != "Double Helix" {
		t.Errorf("extra.section_title = %q, want %q", st, "Double Helix")
	}

	// ── CSL serialization ───────────────────────────────────────────────────
	cslFmt := &csl.Format{}
	var buf bytes.Buffer
	serializeOpts := format.NewSerializeOptions()
	serializeOpts.Pretty = true
	if err := cslFmt.Serialize(&buf, records, serializeOpts); err != nil {
		t.Fatalf("serializing to CSL: %v", err)
	}
	t.Logf("CSL output:\n%s", buf.String())

	var item map[string]any
	if err := json.Unmarshal(buf.Bytes(), &item); err != nil {
		t.Fatalf("parsing CSL JSON output: %v", err)
	}

	cslCases := []struct {
		field string
		want  string
	}{
		{"volume", "171"},
		{"issue", "4356"},
		{"page", "737-738"},
		{"container-title", "Nature"},
		{"DOI", "10.1038/171737a0"},
		{"type", "article-journal"},
		{"language", "English"},
	}
	for _, tc := range cslCases {
		got, _ := item[tc.field].(string)
		if got != tc.want {
			t.Errorf("CSL[%q] = %q, want %q", tc.field, got, tc.want)
		}
	}

	// Authors: Watson first, Crick second
	authors, _ := item["author"].([]any)
	if len(authors) != 2 {
		t.Fatalf("CSL author count = %d, want 2", len(authors))
	}
	wantAuthors := []struct{ family, given string }{
		{"Watson", "James D."},
		{"Crick", "Francis H. C."},
	}
	for i, want := range wantAuthors {
		a, _ := authors[i].(map[string]any)
		family, _ := a["family"].(string)
		given, _ := a["given"].(string)
		if family != want.family || given != want.given {
			t.Errorf("author[%d] = {family:%q given:%q}, want {family:%q given:%q}",
				i, family, given, want.family, want.given)
		}
	}

	// Date: 1953-04-25
	issued, _ := item["issued"].(map[string]any)
	dateParts, _ := issued["date-parts"].([]any)
	if len(dateParts) == 0 {
		t.Fatal("CSL issued.date-parts is empty")
	}
	parts, _ := dateParts[0].([]any)
	if len(parts) < 3 {
		t.Fatalf("CSL issued.date-parts[0] has %d elements, want ≥3", len(parts))
	}
	wantDate := []float64{1953, 4, 25}
	for i, w := range wantDate {
		if v, _ := parts[i].(float64); v != w {
			t.Errorf("issued.date-parts[0][%d] = %v, want %v", i, v, w)
		}
	}
}

// TestDrupalToCSL_PartDetail_NoCaptionFallback ensures that a volume entry
// with only a Caption (display label) does not pollute Publication.Volume.
// Caption is a display label like "Vol." — not the actual volume number.
func TestDrupalToCSL_PartDetail_NoCaptionFallback(t *testing.T) {
	const drupalJSON = `{
		"title": [{"value": "Test Article"}],
		"field_part_detail": [
			{"type": "volume", "caption": "Vol.", "number": "", "title": null}
		]
	}`

	drupalFmt := &drupal.Format{}
	records, err := drupalFmt.Parse(bytes.NewReader([]byte(drupalJSON)), format.NewParseOptions())
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	record := records[0]

	if record.Publication != nil && record.Publication.Volume != "" {
		t.Errorf("Volume = %q; caption-only entry should not set Volume", record.Publication.Volume)
	}
}

// TestDrupalToCSL_PartDetail_PageNoConcat ensures that multiple page entries
// do not get concatenated with "-".  A single Number already encodes the range.
func TestDrupalToCSL_PartDetail_PageNoConcat(t *testing.T) {
	const drupalJSON = `{
		"title": [{"value": "Test Article"}],
		"field_part_detail": [
			{"type": "page", "caption": null, "number": "100-110", "title": null},
			{"type": "page", "caption": null, "number": "200",     "title": null}
		]
	}`

	drupalFmt := &drupal.Format{}
	records, err := drupalFmt.Parse(bytes.NewReader([]byte(drupalJSON)), format.NewParseOptions())
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	record := records[0]

	if record.Publication == nil {
		t.Fatal("Publication is nil")
	}
	// First entry wins; second is ignored (not concatenated).
	if record.Publication.Pages != "100-110" {
		t.Errorf("Pages = %q, want %q", record.Publication.Pages, "100-110")
	}
}

// TestDrupalToCSL_PartDetail_ArticleNumber ensures that an article-number
// part_detail goes to extra.article_number, not to Publication.Pages.
func TestDrupalToCSL_PartDetail_ArticleNumber(t *testing.T) {
	const drupalJSON = `{
		"title": [{"value": "Test Article"}],
		"field_part_detail": [
			{"type": "article", "caption": null, "number": "e12345", "title": null}
		]
	}`

	drupalFmt := &drupal.Format{}
	records, err := drupalFmt.Parse(bytes.NewReader([]byte(drupalJSON)), format.NewParseOptions())
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	record := records[0]

	if an := hub.GetExtraString(record, "article_number"); an != "e12345" {
		t.Errorf("extra.article_number = %q, want %q", an, "e12345")
	}
	if record.Publication != nil && record.Publication.Pages != "" {
		t.Errorf("Pages = %q; article number must not land in Pages", record.Publication.Pages)
	}
}

func TestDrupalToCSL_RelatorsExcludeThesisAdvisorFromAuthor(t *testing.T) {
	const drupalJSON = `{
		"title": [{"value": "Relator Role Test"}],
		"field_linked_agent": [
			{
				"target_id": 1,
				"rel_type": "relators:cre",
				"target_type": "taxonomy_term",
				"_entity": {"name": [{"value": "Alex Rivera"}]}
			},
			{
				"target_id": 2,
				"rel_type": "relators:ths",
				"target_type": "taxonomy_term",
				"_entity": {"name": [{"value": "Jordan Lee"}]}
			}
		]
	}`

	drupalFmt := &drupal.Format{}
	records, err := drupalFmt.Parse(bytes.NewReader([]byte(drupalJSON)), format.NewParseOptions())
	if err != nil {
		t.Fatalf("parsing drupal JSON: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	cslFmt := &csl.Format{}
	var buf bytes.Buffer
	if err := cslFmt.Serialize(&buf, records, format.NewSerializeOptions()); err != nil {
		t.Fatalf("serializing to CSL: %v", err)
	}

	var item map[string]any
	if err := json.Unmarshal(buf.Bytes(), &item); err != nil {
		t.Fatalf("parsing CSL JSON output: %v", err)
	}

	authors, _ := item["author"].([]any)
	if len(authors) != 1 {
		t.Fatalf("CSL author count = %d, want 1", len(authors))
	}

	author, _ := authors[0].(map[string]any)
	family, _ := author["family"].(string)
	if family != "Rivera" {
		t.Errorf("author family = %q, want %q", family, "Rivera")
	}
}
