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
	"github.com/lehigh-university-libraries/crosswalk/helpers"
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

func TestMARCXMLRepeatedValuesRoundTrip(t *testing.T) {
	record := hub.NewRecord()
	record.Title = "Repeated values"
	record.AltTitle = []string{"Alternate title one", "Alternate title two"}
	record.Contributors = []*hubv1.Contributor{
		{
			Name:     "Doe, Jane",
			Role:     "author",
			RoleCode: "aut",
			Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		},
		{
			Name:     "Smith, John",
			Role:     "editor",
			RoleCode: "edt",
			Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		},
	}
	record.Identifiers = []*hubv1.Identifier{
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, Value: "local-one"},
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, Value: "local-two"},
		hub.NewIdentifier("9781234567897", hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN),
		hub.NewIdentifier("9780987654321", hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN),
		hub.NewIdentifier("1234-5678", hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN),
		hub.NewIdentifier("8765-4321", hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN),
		hub.NewIdentifier("10.1234/example.one", hubv1.IdentifierType_IDENTIFIER_TYPE_DOI),
		hub.NewIdentifier("10.5678/example.two", hubv1.IdentifierType_IDENTIFIER_TYPE_DOI),
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER, Value: "report-one"},
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER, Value: "report-two"},
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER, Value: "QA 1"},
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER, Value: "QA 2"},
		hub.NewIdentifier("000000012124423X", hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI),
		hub.NewIdentifier("000000012146438X", hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI),
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED, Value: "vendor-one", Scheme: "vendor"},
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED, Value: "vendor-two", Scheme: "vendor"},
		hub.NewIdentifier("https://example.edu/one", hubv1.IdentifierType_IDENTIFIER_TYPE_URL),
		hub.NewIdentifier("https://example.edu/two", hubv1.IdentifierType_IDENTIFIER_TYPE_URL),
	}
	record.Dates = []*hubv1.DateValue{
		mustParseDate(t, "2001", hubv1.DateType_DATE_TYPE_ISSUED),
		mustParseDate(t, "2002", hubv1.DateType_DATE_TYPE_ISSUED),
		mustParseDate(t, "2003", hubv1.DateType_DATE_TYPE_COPYRIGHT),
		mustParseDate(t, "2004", hubv1.DateType_DATE_TYPE_COPYRIGHT),
		mustParseDate(t, "1990/1991", hubv1.DateType_DATE_TYPE_CREATED),
		mustParseDate(t, "1992", hubv1.DateType_DATE_TYPE_CREATED),
		mustParseDate(t, "2020-01-02", hubv1.DateType_DATE_TYPE_MODIFIED),
		mustParseDate(t, "2021-03-04", hubv1.DateType_DATE_TYPE_MODIFIED),
		mustParseDate(t, "2022/2023", hubv1.DateType_DATE_TYPE_VALID),
		mustParseDate(t, "2024", hubv1.DateType_DATE_TYPE_VALID),
		mustParseDate(t, "2010", hubv1.DateType_DATE_TYPE_CAPTURED),
		{Type: hubv1.DateType_DATE_TYPE_CAPTURED, Raw: "circa 2011"},
	}
	record.Notes = []string{"Note one", "Note two"}
	record.Rights = []*hubv1.Rights{
		{Statement: "Rights statement one", Uri: "https://rights.example/one"},
		{Statement: "Rights statement two", Uri: "https://rights.example/two"},
	}
	record.Subjects = []*hubv1.Subject{
		{Value: "Subject one", Type: hubv1.SubjectType_SUBJECT_TYPE_TOPIC, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH},
		{Value: "Subject two", Type: hubv1.SubjectType_SUBJECT_TYPE_TOPIC, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH},
	}
	record.Genres = []*hubv1.Subject{
		{Value: "Genre one", Type: hubv1.SubjectType_SUBJECT_TYPE_GENRE, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE},
		{Value: "Genre two", Type: hubv1.SubjectType_SUBJECT_TYPE_GENRE, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE},
	}
	record.Relations = []*hubv1.Relation{
		{Type: hubv1.RelationType_RELATION_TYPE_PART_OF, TargetTitle: "Parent one", TargetId: "parent-one"},
		{Type: hubv1.RelationType_RELATION_TYPE_PART_OF, TargetTitle: "Parent two", TargetId: "parent-two"},
		{Type: hubv1.RelationType_RELATION_TYPE_MEMBER_OF, TargetTitle: "Collection one", TargetId: "collection-one"},
		{Type: hubv1.RelationType_RELATION_TYPE_MEMBER_OF, TargetTitle: "Collection two", TargetId: "collection-two"},
		{Type: hubv1.RelationType_RELATION_TYPE_IN_SERIES, TargetTitle: "Series one"},
		{Type: hubv1.RelationType_RELATION_TYPE_IN_SERIES, TargetTitle: "Series two"},
		{Type: hubv1.RelationType_RELATION_TYPE_HAS_FORMAT, TargetTitle: "Format one", TargetId: "format-one"},
		{Type: hubv1.RelationType_RELATION_TYPE_HAS_FORMAT, TargetTitle: "Format two", TargetId: "format-two"},
		{Type: hubv1.RelationType_RELATION_TYPE_RELATED_TO, TargetTitle: "Related one", TargetId: "related-one"},
		{Type: hubv1.RelationType_RELATION_TYPE_RELATED_TO, TargetTitle: "Related two", TargetId: "related-two"},
	}
	hub.SetPublishers(record, []string{"Publisher one", "Publisher two"})
	hub.SetPlacesPublished(record, []string{"Place one", "Place two"})
	hub.SetPhysicalDescriptions(record, []string{"10 pages", "20 pages"})
	hub.SetEditions(record, []string{"First edition", "Second edition"})
	hub.SetLanguages(record, []string{"eng", "fre"})

	fields := hubToFields(record)
	var capturedFallbacks int
	var foundCreatedRange bool
	for _, field := range fields {
		if field.Tag != "046" {
			continue
		}
		var hasEDTFSource bool
		var isCapturedFallback bool
		for _, sub := range field.SubFields {
			if sub.Code == "2" && sub.Value == "edtf" {
				hasEDTFSource = true
			}
			if sub.Code == "x" && strings.HasPrefix(sub.Value, crosswalkDateNotePrefix+"DATE_TYPE_CAPTURED:") {
				isCapturedFallback = true
			}
		}
		if isCapturedFallback {
			capturedFallbacks++
			if hasEDTFSource {
				t.Fatalf("fallback date field falsely declares $2 edtf: %#v", field)
			}
		}
		if firstSubfield(field, "k") == "1990" && firstSubfield(field, "l") == "1991" && hasEDTFSource {
			foundCreatedRange = true
		}
	}
	if capturedFallbacks != 2 {
		t.Fatalf("captured fallback date fields = %d, want 2", capturedFallbacks)
	}
	if !foundCreatedRange {
		t.Fatal("created date range was not serialized as 046 $k/$l")
	}

	opts := format.NewSerializeOptions()
	opts.Pretty = true
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, opts); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	roundTripped, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), format.NewParseOptions())
	if err != nil {
		t.Fatalf("Parse(serialized output) error = %v", err)
	}
	if len(roundTripped) != 1 {
		t.Fatalf("round-trip records = %d, want 1", len(roundTripped))
	}
	gotRecord := roundTripped[0]

	tests := []struct {
		name string
		got  func(*hubv1.Record) []string
		want []string
	}{
		{name: "alternate titles", got: func(r *hubv1.Record) []string { return r.GetAltTitle() }, want: []string{"Alternate title one", "Alternate title two"}},
		{name: "contributors", got: contributorRoleValues, want: []string{"Doe, Jane|aut", "Smith, John|edt"}},
		{name: "local identifiers", got: func(r *hubv1.Record) []string {
			return identifierValues(r, hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL, "")
		}, want: []string{"local-one", "local-two"}},
		{name: "ISBN identifiers", got: func(r *hubv1.Record) []string {
			return identifierValues(r, hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN, "")
		}, want: []string{"9781234567897", "9780987654321"}},
		{name: "ISSN identifiers", got: func(r *hubv1.Record) []string {
			return identifierValues(r, hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN, "")
		}, want: []string{"1234-5678", "8765-4321"}},
		{name: "DOI identifiers", got: func(r *hubv1.Record) []string {
			return identifierValues(r, hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, "")
		}, want: []string{"10.1234/example.one", "10.5678/example.two"}},
		{name: "report identifiers", got: func(r *hubv1.Record) []string {
			return identifierValues(r, hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER, "")
		}, want: []string{"report-one", "report-two"}},
		{name: "call-number identifiers", got: func(r *hubv1.Record) []string {
			return identifierValues(r, hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER, "")
		}, want: []string{"QA 1", "QA 2"}},
		{name: "ISNI fallback identifiers", got: func(r *hubv1.Record) []string {
			return identifierValues(r, hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI, "")
		}, want: []string{"000000012124423X", "000000012146438X"}},
		{name: "open-scheme fallback identifiers", got: func(r *hubv1.Record) []string {
			return identifierValues(r, hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED, "vendor")
		}, want: []string{"vendor-one", "vendor-two"}},
		{name: "URL identifiers", got: func(r *hubv1.Record) []string {
			return identifierValues(r, hubv1.IdentifierType_IDENTIFIER_TYPE_URL, "")
		}, want: []string{"https://example.edu/one", "https://example.edu/two"}},
		{name: "issued dates", got: func(r *hubv1.Record) []string { return dateValues(r, hubv1.DateType_DATE_TYPE_ISSUED) }, want: []string{"2001", "2002"}},
		{name: "copyright dates", got: func(r *hubv1.Record) []string { return dateValues(r, hubv1.DateType_DATE_TYPE_COPYRIGHT) }, want: []string{"2003", "2004"}},
		{name: "created dates", got: func(r *hubv1.Record) []string { return dateValues(r, hubv1.DateType_DATE_TYPE_CREATED) }, want: []string{"1990/1991", "1992"}},
		{name: "modified dates", got: func(r *hubv1.Record) []string { return dateValues(r, hubv1.DateType_DATE_TYPE_MODIFIED) }, want: []string{"2020-01-02", "2021-03-04"}},
		{name: "valid dates", got: func(r *hubv1.Record) []string { return dateValues(r, hubv1.DateType_DATE_TYPE_VALID) }, want: []string{"2022/2023", "2024"}},
		{name: "unsupported semantic dates", got: func(r *hubv1.Record) []string { return dateValues(r, hubv1.DateType_DATE_TYPE_CAPTURED) }, want: []string{"2010", "circa 2011"}},
		{name: "notes", got: func(r *hubv1.Record) []string { return r.GetNotes() }, want: []string{"Note one", "Note two"}},
		{name: "rights statements", got: rightsStatements, want: []string{"Rights statement one", "Rights statement two"}},
		{name: "rights URIs", got: rightsURIs, want: []string{"https://rights.example/one", "https://rights.example/two"}},
		{name: "subjects", got: func(r *hubv1.Record) []string { return subjectValues(r.GetSubjects()) }, want: []string{"Subject one", "Subject two"}},
		{name: "genres", got: func(r *hubv1.Record) []string { return subjectValues(r.GetGenres()) }, want: []string{"Genre one", "Genre two"}},
		{name: "part-of relations", got: func(r *hubv1.Record) []string { return relationValues(r, hubv1.RelationType_RELATION_TYPE_PART_OF) }, want: []string{"Parent one|parent-one", "Parent two|parent-two"}},
		{name: "member-of relations", got: func(r *hubv1.Record) []string { return relationValues(r, hubv1.RelationType_RELATION_TYPE_MEMBER_OF) }, want: []string{"Collection one|collection-one", "Collection two|collection-two"}},
		{name: "series relations", got: func(r *hubv1.Record) []string { return relationValues(r, hubv1.RelationType_RELATION_TYPE_IN_SERIES) }, want: []string{"Series one|", "Series two|"}},
		{name: "format relations", got: func(r *hubv1.Record) []string { return relationValues(r, hubv1.RelationType_RELATION_TYPE_HAS_FORMAT) }, want: []string{"Format one|format-one", "Format two|format-two"}},
		{name: "related-to relations", got: func(r *hubv1.Record) []string { return relationValues(r, hubv1.RelationType_RELATION_TYPE_RELATED_TO) }, want: []string{"Related one|related-one", "Related two|related-two"}},
		{name: "publishers", got: hub.GetPublishers, want: []string{"Publisher one", "Publisher two"}},
		{name: "publication places", got: hub.GetPlacesPublished, want: []string{"Place one", "Place two"}},
		{name: "physical descriptions", got: hub.GetPhysicalDescriptions, want: []string{"10 pages", "20 pages"}},
		{name: "editions", got: hub.GetEditions, want: []string{"First edition", "Second edition"}},
		{name: "languages", got: hub.GetLanguages, want: []string{"eng", "fre"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.got(gotRecord); !slices.Equal(got, test.want) {
				t.Errorf("round-trip values = %#v, want %#v\n%s", got, test.want, output.String())
			}
		})
	}
}

func TestParseMARCXMLPreservesRepeatableSubfields(t *testing.T) {
	input := strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<collection xmlns="http://www.loc.gov/MARC21/slim">
  <record>
    <leader>00000nam a2200000 i 4500</leader>
    <datafield tag="245" ind1="0" ind2="0"><subfield code="a">Repeatable subfields</subfield></datafield>
    <datafield tag="700" ind1="1" ind2=" ">
      <subfield code="a">Doe, Jane</subfield>
      <subfield code="e">author</subfield>
      <subfield code="4">aut</subfield>
      <subfield code="e">editor</subfield>
      <subfield code="4">edt</subfield>
    </datafield>
    <datafield tag="050" ind1=" " ind2="4">
      <subfield code="a">QA 1</subfield>
      <subfield code="b">.A1</subfield>
      <subfield code="a">QA 2</subfield>
    </datafield>
    <datafield tag="300" ind1=" " ind2=" ">
      <subfield code="a">10 pages</subfield>
      <subfield code="b">illustrations</subfield>
      <subfield code="c">30 cm</subfield>
      <subfield code="a">2 maps</subfield>
      <subfield code="e">1 guide</subfield>
    </datafield>
    <datafield tag="540" ind1=" " ind2=" ">
      <subfield code="a">Shared rights statement</subfield>
      <subfield code="u">https://rights.example/one</subfield>
      <subfield code="u">https://rights.example/two</subfield>
    </datafield>
    <datafield tag="773" ind1="0" ind2=" ">
      <subfield code="t">Shared parent</subfield>
      <subfield code="w">parent-one</subfield>
      <subfield code="w">parent-two</subfield>
    </datafield>
    <datafield tag="787" ind1="0" ind2=" ">
      <subfield code="t">Shared relation</subfield>
      <subfield code="w">related-one</subfield>
      <subfield code="w">related-two</subfield>
    </datafield>
  </record>
</collection>`)

	records, err := (&Format{}).Parse(input, format.NewParseOptions())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Parse() records = %d, want 1", len(records))
	}
	record := records[0]

	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{name: "contributor roles", got: contributorRoleValues(record), want: []string{"Doe, Jane|aut", "Doe, Jane|edt"}},
		{name: "call numbers", got: identifierValues(record, hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER, ""), want: []string{"QA 1 .A1", "QA 2"}},
		{name: "physical extents", got: hub.GetPhysicalDescriptions(record), want: []string{"10 pages illustrations 30 cm", "2 maps 1 guide"}},
		{name: "rights URIs", got: rightsURIs(record), want: []string{"https://rights.example/one", "https://rights.example/two"}},
		{name: "part-of identifiers", got: relationValues(record, hubv1.RelationType_RELATION_TYPE_PART_OF), want: []string{"Shared parent|parent-one", "Shared parent|parent-two"}},
		{name: "related-to identifiers", got: relationValues(record, hubv1.RelationType_RELATION_TYPE_RELATED_TO), want: []string{"Shared relation|related-one", "Shared relation|related-two"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !slices.Equal(test.got, test.want) {
				t.Errorf("Parse() values = %#v, want %#v", test.got, test.want)
			}
		})
	}

	opts := format.NewSerializeOptions()
	opts.Pretty = true
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, opts); err != nil {
		t.Fatalf("Serialize(parsed record) error = %v", err)
	}
	if got := strings.Count(output.String(), `<datafield tag="300"`); got != 2 {
		t.Fatalf("serialized 300 fields = %d, want 2\n%s", got, output.String())
	}
	roundTripped, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), format.NewParseOptions())
	if err != nil {
		t.Fatalf("Parse(serialized record) error = %v", err)
	}
	wantDescriptions := []string{"10 pages illustrations 30 cm", "2 maps 1 guide"}
	if len(roundTripped) != 1 || !slices.Equal(hub.GetPhysicalDescriptions(roundTripped[0]), wantDescriptions) {
		t.Fatalf("round-trip physical descriptions = %#v, want %#v", hub.GetPhysicalDescriptions(roundTripped[0]), wantDescriptions)
	}
}

func mustParseDate(t *testing.T, value string, dateType hubv1.DateType) *hubv1.DateValue {
	t.Helper()
	date, err := helpers.ParseEDTF(value, dateType)
	if err != nil {
		t.Fatalf("ParseEDTF(%q) error = %v", value, err)
	}
	return date
}

func contributorRoleValues(record *hubv1.Record) []string {
	var values []string
	for _, contributor := range record.GetContributors() {
		if contributor != nil {
			values = append(values, contributor.GetName()+"|"+contributor.GetRoleCode())
		}
	}
	return values
}

func identifierValues(record *hubv1.Record, idType hubv1.IdentifierType, scheme string) []string {
	var values []string
	for _, identifier := range record.GetIdentifiers() {
		if identifier == nil || identifier.GetType() != idType {
			continue
		}
		if scheme != "" && identifier.GetScheme() != scheme {
			continue
		}
		values = append(values, identifier.GetValue())
	}
	return values
}

func dateValues(record *hubv1.Record, dateType hubv1.DateType) []string {
	var values []string
	for _, date := range record.GetDates() {
		if date == nil || date.GetType() != dateType {
			continue
		}
		value := strings.TrimSpace(date.GetRaw())
		if value == "" {
			value = hub.FormatEDTF(date)
		}
		values = append(values, value)
	}
	return values
}

func rightsStatements(record *hubv1.Record) []string {
	var values []string
	for _, rights := range record.GetRights() {
		if rights != nil {
			values = append(values, rights.GetStatement())
		}
	}
	return values
}

func rightsURIs(record *hubv1.Record) []string {
	var values []string
	for _, rights := range record.GetRights() {
		if rights != nil {
			values = append(values, rights.GetUri())
		}
	}
	return values
}

func subjectValues(subjects []*hubv1.Subject) []string {
	var values []string
	for _, subject := range subjects {
		if subject != nil {
			values = append(values, subject.GetValue())
		}
	}
	return values
}

func relationValues(record *hubv1.Record, relationType hubv1.RelationType) []string {
	var values []string
	for _, relation := range record.GetRelations() {
		if relation != nil && relation.GetType() == relationType {
			values = append(values, relation.GetTargetTitle()+"|"+relation.GetTargetId())
		}
	}
	return values
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
