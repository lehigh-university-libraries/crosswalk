package schemaorg

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

func TestRepeatedSchemaPropertiesRoundTrip(t *testing.T) {
	record := &hubv1.Record{
		Title:                "Repeated values",
		AltTitle:             []string{"Alternate One", "Alternate Two"},
		Publisher:            "stale publisher",
		Publishers:           []string{"Publisher One", "Publisher Two"},
		Languages:            []string{"eng", "fre"},
		PhysicalDescriptions: []string{"12 pages", "1 map"},
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		},
		Dates: []*hubv1.DateValue{
			{Type: hubv1.DateType_DATE_TYPE_PUBLISHED, Year: 2020},
			{Type: hubv1.DateType_DATE_TYPE_PUBLISHED, Year: 2021},
			{Type: hubv1.DateType_DATE_TYPE_CREATED, Year: 2018},
			{Type: hubv1.DateType_DATE_TYPE_CREATED, Year: 2019},
			{Type: hubv1.DateType_DATE_TYPE_MODIFIED, Year: 2022},
			{Type: hubv1.DateType_DATE_TYPE_MODIFIED, Year: 2023},
			{Type: hubv1.DateType_DATE_TYPE_COPYRIGHT, Year: 2016},
			{Type: hubv1.DateType_DATE_TYPE_COPYRIGHT, Year: 2017},
		},
		Rights: []*hubv1.Rights{
			{Uri: "https://creativecommons.org/licenses/by/4.0/"},
			{Statement: "Permission required"},
		},
		Relations: []*hubv1.Relation{
			{Type: hubv1.RelationType_RELATION_TYPE_PART_OF, TargetTitle: "Parent One", TargetUri: "https://example.org/one"},
			{Type: hubv1.RelationType_RELATION_TYPE_MEMBER_OF, TargetUri: "https://example.org/two"},
		},
	}
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{}); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, ok := document["publisher"].([]any); !ok || len(got) != 2 {
		t.Fatalf("publisher JSON = %#v, want two organizations", document["publisher"])
	}
	if got, ok := document["inLanguage"].([]any); !ok || len(got) != 2 {
		t.Fatalf("inLanguage JSON = %#v, want two values", document["inLanguage"])
	}
	if got := document["pagination"]; got != "12 pages" {
		t.Errorf("pagination = %#v, want primary physical description", got)
	}
	for _, property := range []string{"alternativeHeadline", "datePublished", "dateCreated", "dateModified", "copyrightYear", "license", "isPartOf"} {
		if got, ok := document[property].([]any); !ok || len(got) != 2 {
			t.Errorf("%s JSON = %#v, want two values", property, document[property])
		}
	}

	parsed, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := hub.GetPublishers(parsed[0]); !reflect.DeepEqual(got, record.Publishers) {
		t.Errorf("publishers = %#v, want %#v", got, record.Publishers)
	}
	if got := hub.GetLanguages(parsed[0]); !reflect.DeepEqual(got, record.Languages) {
		t.Errorf("languages = %#v, want %#v", got, record.Languages)
	}
	if !reflect.DeepEqual(parsed[0].AltTitle, record.AltTitle) {
		t.Errorf("alternate titles = %#v, want %#v", parsed[0].AltTitle, record.AltTitle)
	}
	for _, test := range []struct {
		dateType hubv1.DateType
		want     int
	}{
		{hubv1.DateType_DATE_TYPE_PUBLISHED, 2},
		{hubv1.DateType_DATE_TYPE_CREATED, 2},
		{hubv1.DateType_DATE_TYPE_MODIFIED, 2},
		{hubv1.DateType_DATE_TYPE_COPYRIGHT, 2},
	} {
		if got := len(hub.GetDates(parsed[0], test.dateType)); got != test.want {
			t.Errorf("%s dates = %d, want %d", test.dateType, got, test.want)
		}
	}
	if len(parsed[0].Rights) != 2 {
		t.Errorf("rights = %#v, want two values", parsed[0].Rights)
	}
	if len(parsed[0].Relations) != 2 {
		t.Errorf("relations = %#v, want two values", parsed[0].Relations)
	}
}

func TestRepeatableSchemaPropertiesKeepSingleValueJSONShape(t *testing.T) {
	record := &hubv1.Record{
		Title:    "Single values",
		AltTitle: []string{"One alternate"},
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		},
		Dates: []*hubv1.DateValue{
			{Type: hubv1.DateType_DATE_TYPE_PUBLISHED, Year: 2020},
			{Type: hubv1.DateType_DATE_TYPE_CREATED, Year: 2019},
			{Type: hubv1.DateType_DATE_TYPE_MODIFIED, Year: 2021},
			{Type: hubv1.DateType_DATE_TYPE_COPYRIGHT, Year: 2018},
		},
		Rights: []*hubv1.Rights{{Uri: "https://creativecommons.org/licenses/by/4.0/"}},
		Relations: []*hubv1.Relation{{
			Type: hubv1.RelationType_RELATION_TYPE_PART_OF, TargetTitle: "One parent",
		}},
	}
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{}); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	for _, property := range []string{"alternativeHeadline", "datePublished", "dateCreated", "dateModified", "copyrightYear", "license", "isPartOf"} {
		if _, isArray := document[property].([]any); isArray {
			t.Errorf("%s JSON = %#v, want scalar/object shape for one value", property, document[property])
		}
		if document[property] == nil {
			t.Errorf("%s JSON is missing", property)
		}
	}

	parsed, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(parsed[0].AltTitle) != 1 || len(parsed[0].Rights) != 1 || len(parsed[0].Relations) != 1 {
		t.Errorf("single-value round trip = alt %#v rights %#v relations %#v", parsed[0].AltTitle, parsed[0].Rights, parsed[0].Relations)
	}
}

func TestRightsEntryWithURIAndStatementRoundTripsTogether(t *testing.T) {
	tests := []struct {
		name       string
		rights     *hubv1.Rights
		wantScalar bool
	}{
		{name: "URI only", rights: &hubv1.Rights{Uri: "https://example.org/license"}, wantScalar: true},
		{name: "statement only", rights: &hubv1.Rights{Statement: "Permission required"}, wantScalar: true},
		{name: "URI and statement", rights: &hubv1.Rights{Uri: "https://example.org/license", Statement: "Permission required"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := &hubv1.Record{Title: "Rights", Rights: []*hubv1.Rights{test.rights}}
			var output bytes.Buffer
			if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{}); err != nil {
				t.Fatalf("Serialize() error = %v", err)
			}
			var document map[string]any
			if err := json.Unmarshal(output.Bytes(), &document); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			_, scalar := document["license"].(string)
			if scalar != test.wantScalar {
				t.Errorf("license JSON = %#v, scalar = %t, want %t", document["license"], scalar, test.wantScalar)
			}

			parsed, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), nil)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(parsed) != 1 || len(parsed[0].Rights) != 1 {
				t.Fatalf("parsed rights = %#v", parsed)
			}
			got := parsed[0].Rights[0]
			if got.Uri != test.rights.Uri || got.Statement != test.rights.Statement {
				t.Errorf("rights = {URI:%q Statement:%q}, want {URI:%q Statement:%q}", got.Uri, got.Statement, test.rights.Uri, test.rights.Statement)
			}
		})
	}
}

func TestBookUsesPrimaryRepeatedEdition(t *testing.T) {
	book := recordToBook(&hubv1.Record{
		Edition:  "stale edition",
		Editions: []string{"First edition", "Revised edition"},
	})
	if book.BookEdition != "First edition" {
		t.Errorf("book edition = %q, want primary repeated value", book.BookEdition)
	}
}

func TestDegreeInstitutionAppendsToRepeatedPublishers(t *testing.T) {
	record := &hubv1.Record{
		Publishers: []string{"Publisher One", "Publisher Two"},
		DegreeInfo: &hubv1.DegreeInfo{Institution: "Degree University"},
	}
	for name, publisher := range map[string]any{
		"thesis":           recordToThesis(record).Publisher,
		"digital document": recordToDigitalDocument(record).Publisher,
	} {
		organizations, ok := publisher.([]*Organization)
		if !ok || len(organizations) != 3 {
			t.Errorf("%s publishers = %#v, want three organizations", name, publisher)
			continue
		}
		want := []string{"Publisher One", "Publisher Two", "Degree University"}
		for index := range want {
			if organizations[index].Name != want[index] {
				t.Errorf("%s publisher %d = %q, want %q", name, index, organizations[index].Name, want[index])
			}
		}
	}
}
