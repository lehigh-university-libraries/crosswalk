package islandora_workbench

import (
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
		Publishers:           []string{"Publisher One", "Publisher Two"},
		PlacesPublished:      []string{"Halifax", "Moncton"},
		PhysicalDescriptions: []string{"12 pages", "1 map"},
		Editions:             []string{"First edition", "Revised edition"},
		Languages:            []string{"eng", "fre"},
		Dimensions:           "30 cm",
	}
	columns, _, err := projectRecordToColumns(record, "^")
	if err != nil {
		t.Fatalf("projectRecordToColumns() error = %v", err)
	}
	if got := columns["field_publisher"]; got != "Publisher One^Publisher Two" {
		t.Errorf("field_publisher = %q", got)
	}
	if got := columns["field_extent"]; !strings.Contains(got, `"attr0":"page"`) || !strings.Contains(got, `"attr0":"dimensions"`) {
		t.Errorf("field_extent = %q, want page and dimensions entries", got)
	}

	header := []string{"field_publisher", "field_place_published", "field_extent", "field_edition", "field_language"}
	row := make([]string, len(header))
	for index, name := range header {
		row[index] = columns[name]
	}
	parsed, diagnostics := workbenchRowToRecord(row, 2, header, buildWorkbenchColumnMap(header, nil), &format.ParseOptions{
		Spec: &spec.Transformation{Source: spec.Table{Format: "islandora-workbench", MultiValueSeparator: "^"}},
	})
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}

	assertWorkbenchCompatibilityValues(t, "publishers", hub.GetPublishers(parsed), record.Publishers)
	assertWorkbenchCompatibilityValues(t, "places", hub.GetPlacesPublished(parsed), record.PlacesPublished)
	assertWorkbenchCompatibilityValues(t, "physical descriptions", hub.GetPhysicalDescriptions(parsed), record.PhysicalDescriptions)
	assertWorkbenchCompatibilityValues(t, "editions", hub.GetEditions(parsed), record.Editions)
	assertWorkbenchCompatibilityValues(t, "languages", hub.GetLanguages(parsed), record.Languages)
	if parsed.Dimensions != "30 cm" {
		t.Errorf("dimensions = %q, want 30 cm", parsed.Dimensions)
	}
}

func TestCustomColumnsAppendAndUnknownExtentAttributesRemainPhysicalDescriptions(t *testing.T) {
	header := []string{"publisher_primary", "publisher_secondary", "field_extent"}
	profile := &mapping.Profile{Fields: map[string]mapping.FieldMapping{
		"publisher_primary":   {IR: "Publisher"},
		"publisher_secondary": {IR: "Publisher"},
	}}
	row := []string{"Publisher One^Publisher Two", "Publisher Three", `{"value":"one optical disc","attr0":"carrier"}`}
	record, diagnostics := workbenchRowToRecord(row, 2, header, buildWorkbenchColumnMap(header, profile), &format.ParseOptions{
		Profile: profile,
		Spec:    &spec.Transformation{Source: spec.Table{Format: "islandora-workbench", MultiValueSeparator: "^"}},
	})
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	assertWorkbenchCompatibilityValues(t, "publishers", hub.GetPublishers(record), []string{"Publisher One", "Publisher Two", "Publisher Three"})
	assertWorkbenchCompatibilityValues(t, "physical descriptions", hub.GetPhysicalDescriptions(record), []string{"one optical disc"})
}

func assertWorkbenchCompatibilityValues(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", name, got, want)
	}
}
