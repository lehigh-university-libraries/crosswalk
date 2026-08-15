package csv

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

func TestCompatibilityValuesRoundTripWithConfiguredSeparator(t *testing.T) {
	record := &hubv1.Record{
		Title:                "Repeated values",
		Publisher:            "stale publisher",
		Publishers:           []string{"Publisher One", "Publisher Two"},
		PlacesPublished:      []string{"Halifax", "Moncton"},
		PhysicalDescriptions: []string{"12 pages", "1 map"},
		Editions:             []string{"First edition", "Revised edition"},
		Languages:            []string{"eng", "fre"},
		Dates: []*hubv1.DateValue{
			{Type: hubv1.DateType_DATE_TYPE_ISSUED, Raw: "2020"},
			{Type: hubv1.DateType_DATE_TYPE_ISSUED, Raw: "2021"},
			{Type: hubv1.DateType_DATE_TYPE_CREATED, Raw: "2018"},
			{Type: hubv1.DateType_DATE_TYPE_CREATED, Raw: "2019"},
		},
	}
	columns := []string{"title", "publisher", "place_published", "physical_description", "edition", "language", "date_issued", "date_created"}

	var output bytes.Buffer
	err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{
		IncludeHeader:       true,
		Columns:             columns,
		MultiValueSeparator: "^",
	})
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}

	parsed, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), &format.ParseOptions{
		Profile: &mapping.Profile{Options: mapping.ProfileOptions{MultiValueSeparator: "^"}},
	})
	if err != nil {
		t.Fatalf("Parse() error = %v\n%s", err, output.String())
	}
	if len(parsed) != 1 {
		t.Fatalf("record count = %d, want 1", len(parsed))
	}

	assertCSVCompatibilityValues(t, "publishers", hub.GetPublishers(parsed[0]), record.Publishers)
	assertCSVCompatibilityValues(t, "places", hub.GetPlacesPublished(parsed[0]), record.PlacesPublished)
	assertCSVCompatibilityValues(t, "physical descriptions", hub.GetPhysicalDescriptions(parsed[0]), record.PhysicalDescriptions)
	assertCSVCompatibilityValues(t, "editions", hub.GetEditions(parsed[0]), record.Editions)
	assertCSVCompatibilityValues(t, "languages", hub.GetLanguages(parsed[0]), record.Languages)
	if got := csvDateStrings(parsed[0], hubv1.DateType_DATE_TYPE_ISSUED); !reflect.DeepEqual(got, []string{"2020", "2021"}) {
		t.Errorf("issued dates = %#v", got)
	}
	if got := csvDateStrings(parsed[0], hubv1.DateType_DATE_TYPE_CREATED); !reflect.DeepEqual(got, []string{"2018", "2019"}) {
		t.Errorf("created dates = %#v", got)
	}
	if parsed[0].Publisher != "Publisher One" {
		t.Errorf("legacy publisher = %q, want primary repeated value", parsed[0].Publisher)
	}
}

func TestSpecParsePreservesRepeatedCompatibilityValues(t *testing.T) {
	fields := []spec.Field{
		{Name: "publisher", Hub: "Publisher", Codec: "multi"},
		{Name: "place", Hub: "PlacePublished", Codec: "multi"},
		{Name: "extent", Hub: "PhysicalDesc", Codec: "multi"},
		{Name: "edition", Hub: "Edition", Codec: "multi"},
		{Name: "language", Hub: "Language", Codec: "multi"},
	}
	transformation := &spec.Transformation{
		Version: spec.CurrentVersion,
		Name:    "repeated-compatibility-values",
		Source: spec.Table{
			Format:              "csv",
			MultiValueSeparator: "^",
			Fields:              fields,
		},
		Target: spec.Table{Format: "islandora-workbench", Fields: fields},
	}
	sealCSVSpec(t, transformation)

	records, err := (&Format{}).Parse(strings.NewReader(
		"publisher,place,extent,edition,language\n"+
			"Publisher One^Publisher Two,Halifax^Moncton,12 pages^1 map,First^Revised,eng^fre\n",
	), &format.ParseOptions{Spec: transformation})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("record count = %d, want 1", len(records))
	}
	assertCSVCompatibilityValues(t, "publishers", hub.GetPublishers(records[0]), []string{"Publisher One", "Publisher Two"})
	assertCSVCompatibilityValues(t, "places", hub.GetPlacesPublished(records[0]), []string{"Halifax", "Moncton"})
	assertCSVCompatibilityValues(t, "physical descriptions", hub.GetPhysicalDescriptions(records[0]), []string{"12 pages", "1 map"})
	assertCSVCompatibilityValues(t, "editions", hub.GetEditions(records[0]), []string{"First", "Revised"})
	assertCSVCompatibilityValues(t, "languages", hub.GetLanguages(records[0]), []string{"eng", "fre"})
}

func TestProfileColumnsAppendToSameRepeatedCompatibilityField(t *testing.T) {
	input := "publisher_primary,publisher_secondary\nPublisher One^Publisher Two,Publisher Three\n"
	profile := &mapping.Profile{
		Fields: map[string]mapping.FieldMapping{
			"publisher_primary":   {IR: "Publisher"},
			"publisher_secondary": {IR: "Publisher"},
		},
		Options: mapping.ProfileOptions{MultiValueSeparator: "^"},
	}
	records, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{Profile: profile})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertCSVCompatibilityValues(t, "publishers", hub.GetPublishers(records[0]), []string{"Publisher One", "Publisher Two", "Publisher Three"})
}

func csvDateStrings(record *hubv1.Record, dateType hubv1.DateType) []string {
	values := make([]string, 0)
	for _, date := range hub.GetDates(record, dateType) {
		values = append(values, hub.DateString(date))
	}
	return values
}

func assertCSVCompatibilityValues(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", name, got, want)
	}
}
