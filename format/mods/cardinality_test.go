package mods

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

func TestCompatibilityValuesRoundTrip(t *testing.T) {
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
			{Type: hubv1.DateType_DATE_TYPE_COPYRIGHT, Raw: "2017"},
			{Type: hubv1.DateType_DATE_TYPE_COPYRIGHT, Raw: "2016"},
			{Type: hubv1.DateType_DATE_TYPE_MODIFIED, Raw: "2022"},
			{Type: hubv1.DateType_DATE_TYPE_MODIFIED, Raw: "2023"},
		},
	}
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{}); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	parsed, err := (&Format{}).Parse(bytes.NewReader(output.Bytes()), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v\n%s", err, output.String())
	}
	if len(parsed) != 1 {
		t.Fatalf("record count = %d, want 1", len(parsed))
	}

	assertMODSCompatibilityValues(t, "publishers", hub.GetPublishers(parsed[0]), record.Publishers)
	assertMODSCompatibilityValues(t, "places", hub.GetPlacesPublished(parsed[0]), record.PlacesPublished)
	assertMODSCompatibilityValues(t, "physical descriptions", hub.GetPhysicalDescriptions(parsed[0]), record.PhysicalDescriptions)
	assertMODSCompatibilityValues(t, "editions", hub.GetEditions(parsed[0]), record.Editions)
	assertMODSCompatibilityValues(t, "languages", hub.GetLanguages(parsed[0]), record.Languages)
	for _, test := range []struct {
		name     string
		dateType hubv1.DateType
		want     []string
	}{
		{name: "issued", dateType: hubv1.DateType_DATE_TYPE_ISSUED, want: []string{"2020", "2021"}},
		{name: "created", dateType: hubv1.DateType_DATE_TYPE_CREATED, want: []string{"2018", "2019"}},
		{name: "copyright", dateType: hubv1.DateType_DATE_TYPE_COPYRIGHT, want: []string{"2017", "2016"}},
		{name: "modified", dateType: hubv1.DateType_DATE_TYPE_MODIFIED, want: []string{"2022", "2023"}},
	} {
		values := make([]string, 0)
		for _, date := range hub.GetDates(parsed[0], test.dateType) {
			values = append(values, hub.DateString(date))
		}
		if !reflect.DeepEqual(values, test.want) {
			t.Errorf("%s dates = %#v, want %#v", test.name, values, test.want)
		}
	}
}

func assertMODSCompatibilityValues(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", name, got, want)
	}
}
