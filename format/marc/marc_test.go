package marc

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	drupalfmt "github.com/lehigh-university-libraries/crosswalk/format/drupal"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

func TestParseAndSerializeMARCXMLPreservesRepeatedPublishers(t *testing.T) {
	input := strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<collection xmlns="http://www.loc.gov/MARC21/slim">
  <record>
    <leader>00000nam a2200000 i 4500</leader>
    <datafield tag="245" ind1="0" ind2="0">
      <subfield code="a">Multiple publishers</subfield>
    </datafield>
    <datafield tag="264" ind1=" " ind2="1">
      <subfield code="a">Bethlehem, Pa.</subfield>
      <subfield code="b">First Press</subfield>
      <subfield code="b">Second Press</subfield>
      <subfield code="c">2026</subfield>
    </datafield>
    <datafield tag="264" ind1="2" ind2="1">
      <subfield code="b">Third Press</subfield>
    </datafield>
    <datafield tag="264" ind1=" " ind2="2">
      <subfield code="b">A distributor, not a publisher</subfield>
    </datafield>
  </record>
</collection>`)

	records, err := (&Format{}).Parse(input, format.NewParseOptions())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []string{"First Press", "Second Press", "Third Press"}
	if len(records) != 1 || !slices.Equal(hub.GetPublishers(records[0]), want) {
		t.Fatalf("Parse() publishers = %#v, want %#v", hub.GetPublishers(records[0]), want)
	}
	if records[0].GetPublisher() != want[0] {
		t.Fatalf("Parse() primary publisher = %q, want %q", records[0].GetPublisher(), want[0])
	}

	opts := format.NewSerializeOptions()
	opts.Pretty = true
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, opts); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	if got := strings.Count(output.String(), `<subfield code="b">`); got != len(want) {
		t.Fatalf("serialized publisher subfields = %d, want %d\n%s", got, len(want), output.String())
	}

	roundTripped, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), format.NewParseOptions())
	if err != nil {
		t.Fatalf("Parse(serialized output) error = %v", err)
	}
	if len(roundTripped) != 1 || !slices.Equal(hub.GetPublishers(roundTripped[0]), want) {
		t.Fatalf("round-trip publishers = %#v, want %#v", hub.GetPublishers(roundTripped[0]), want)
	}
}

func TestDrupalJSONToMARCXMLPreservesReportedPublishers(t *testing.T) {
	want := []string{
		"University of New Brunswick at Saint John",
		"Prince Edward Island Museum and Heritage Foundation",
		"Canadian Heritage Information Network",
		"Canadian Museum of Civilization",
		"Gorsebrook Research Institute, St. Mary's University",
		"Memorial University of Newfoundland",
		"New Brunswick Museum, Saint John",
		"Newfoundland Museum",
		"Nova Scotia Museum, Halifax",
	}
	publisherValues := make([]map[string]string, 0, len(want))
	for _, publisher := range want {
		publisherValues = append(publisherValues, map[string]string{"value": publisher})
	}
	input, err := json.Marshal(map[string]any{
		"title":           []map[string]string{{"value": "Atlantic Canada Newspaper Survey - Dataset"}},
		"field_publisher": publisherValues,
	})
	if err != nil {
		t.Fatalf("encoding Drupal fixture: %v", err)
	}

	records, err := (&drupalfmt.Format{}).Parse(bytes.NewReader(input), format.NewParseOptions())
	if err != nil {
		t.Fatalf("Drupal Parse() error = %v", err)
	}
	if len(records) != 1 || !slices.Equal(hub.GetPublishers(records[0]), want) {
		t.Fatalf("Drupal publishers = %#v, want %#v", hub.GetPublishers(records[0]), want)
	}

	opts := format.NewSerializeOptions()
	opts.Pretty = true
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, opts); err != nil {
		t.Fatalf("MARC Serialize() error = %v", err)
	}
	if got := strings.Count(output.String(), `<subfield code="b">`); got != len(want) {
		t.Fatalf("MARC publisher subfields = %d, want %d\n%s", got, len(want), output.String())
	}
	roundTripped, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), format.NewParseOptions())
	if err != nil {
		t.Fatalf("Parse(MARCXML) error = %v", err)
	}
	if len(roundTripped) != 1 || !slices.Equal(hub.GetPublishers(roundTripped[0]), want) {
		t.Fatalf("MARCXML publishers = %#v, want %#v", hub.GetPublishers(roundTripped[0]), want)
	}
}

func TestReaderFileRejectsInputOverLimit(t *testing.T) {
	file, cleanup, err := readerFileWithLimit(strings.NewReader("12345"), 4)
	if cleanup != nil {
		cleanup()
	}
	if file != nil {
		t.Fatal("readerFileWithLimit() returned a file for oversized input")
	}
	if err == nil || !strings.Contains(err.Error(), "MARC input exceeds 4 bytes") {
		t.Fatalf("readerFileWithLimit() error = %v, want size-limit error", err)
	}
}

func TestParseMARCXML(t *testing.T) {
	input := strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<collection xmlns="http://www.loc.gov/MARC21/slim">
  <record>
    <leader>00750ngm a2200205 i 4500</leader>
    <controlfield tag="001">cw-test-parse</controlfield>
    <controlfield tag="008">260512s2026    xxu           000 0 eng d</controlfield>
    <datafield tag="100" ind1="1" ind2=" ">
      <subfield code="a">Benedict, Archer</subfield>
      <subfield code="4">ivr</subfield>
    </datafield>
    <datafield tag="245" ind1="1" ind2="0">
      <subfield code="a">Kate Ames Schartel Novak Oral History</subfield>
    </datafield>
    <datafield tag="264" ind1=" " ind2="1">
      <subfield code="c">2026-04-29</subfield>
    </datafield>
    <datafield tag="650" ind1=" " ind2="0">
      <subfield code="a">Lehigh University</subfield>
    </datafield>
  </record>
</collection>`)

	records, err := (&Format{}).Parse(input, format.NewParseOptions())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record := records[0]
	wantTitle := "Kate Ames Schartel Novak Oral History"
	if record.Title != wantTitle {
		t.Fatalf("unexpected title: %q", record.Title)
	}
	if len(record.Contributors) != 1 || record.Contributors[0].Role != "ivr" || record.Contributors[0].RoleCode != "ivr" {
		t.Fatalf("unexpected contributor relator: %#v", record.Contributors)
	}
	if len(record.Dates) != 1 || record.Dates[0].Raw != "2026-04-29" {
		t.Fatalf("unexpected dates: %#v", record.Dates)
	}
	if record.SourceInfo == nil || record.SourceInfo.SourceId != "cw-test-parse" {
		t.Fatalf("unexpected source info: %#v", record.SourceInfo)
	}
}

func TestSerializeMARC21BinaryRoundTrip(t *testing.T) {
	original := &hubv1.Record{
		Title:          "MARC Round Trip",
		Abstract:       "A test record serialized to MARC.",
		Publisher:      "Crosswalk Press",
		PlacePublished: "Bethlehem, Pa.",
		Language:       "eng",
		Contributors: []*hubv1.Contributor{{
			Name:     "Doe, Jane",
			Role:     "author",
			RoleCode: "aut",
			Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		}},
		Dates: []*hubv1.DateValue{{
			Type:      hubv1.DateType_DATE_TYPE_ISSUED,
			Raw:       "2024",
			Year:      2024,
			Precision: hubv1.DatePrecision_DATE_PRECISION_YEAR,
		}},
		Identifiers: []*hubv1.Identifier{
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, Value: "cw-test-1"},
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN, Value: "9781234567890"},
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_URL, Value: "https://example.edu/record"},
		},
		Subjects: []*hubv1.Subject{{
			Value:      "Metadata",
			Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH,
			Type:       hubv1.SubjectType_SUBJECT_TYPE_TOPIC,
		}},
	}

	var out bytes.Buffer
	if err := (&Format{}).Serialize(&out, []*hubv1.Record{original}, format.NewSerializeOptions()); err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("expected MARC output")
	}

	records, err := (&Format{}).Parse(bytes.NewReader(out.Bytes()), format.NewParseOptions())
	if err != nil {
		t.Fatalf("Parse serialized MARC failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	parsed := records[0]
	if parsed.Title != original.Title {
		t.Fatalf("title mismatch: got %q want %q", parsed.Title, original.Title)
	}
	if parsed.Publisher != original.Publisher {
		t.Fatalf("publisher mismatch: got %q want %q", parsed.Publisher, original.Publisher)
	}
	if len(parsed.Subjects) != 1 || parsed.Subjects[0].Value != "Metadata" {
		t.Fatalf("subject mismatch: %#v", parsed.Subjects)
	}
	if len(parsed.Contributors) != 1 || parsed.Contributors[0].Role != "aut" || parsed.Contributors[0].RoleCode != "aut" {
		t.Fatalf("relator mismatch: %#v", parsed.Contributors)
	}
}

func TestSerializeMARCXMLWithPretty(t *testing.T) {
	record := &hubv1.Record{
		Title:    "MARCXML Output",
		Language: "eng",
		Contributors: []*hubv1.Contributor{{
			Name:     "Benedict, Archer",
			Role:     "ivr",
			RoleCode: "ivr",
			Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		}},
	}

	opts := format.NewSerializeOptions()
	opts.Pretty = true

	var out bytes.Buffer
	if err := (&Format{}).Serialize(&out, []*hubv1.Record{record}, opts); err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	xml := out.String()
	if !bytes.Contains(out.Bytes(), []byte("\n  <record>")) {
		t.Fatalf("expected indented MARCXML, got %q", xml)
	}
	if !bytes.Contains(out.Bytes(), []byte(`<subfield code="4">ivr</subfield>`)) {
		t.Fatalf("expected relator code in $4, got %q", xml)
	}
	if bytes.Contains(out.Bytes(), []byte(`interviewer`)) {
		t.Fatalf("did not expect human-readable relator label, got %q", xml)
	}
}
