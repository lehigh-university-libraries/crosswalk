package hub

import (
	"reflect"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

type repeatedCompatibilityField struct {
	name        string
	set         func(*hubv1.Record, []string)
	get         func(*hubv1.Record) []string
	setScalar   func(*hubv1.Record, string)
	getScalar   func(*hubv1.Record) string
	setRepeated func(*hubv1.Record, []string)
}

func repeatedCompatibilityFields() []repeatedCompatibilityField {
	return []repeatedCompatibilityField{
		{
			name: "publishers", set: SetPublishers, get: GetPublishers,
			setScalar:   func(record *hubv1.Record, value string) { record.Publisher = value },
			getScalar:   func(record *hubv1.Record) string { return record.GetPublisher() },
			setRepeated: func(record *hubv1.Record, values []string) { record.Publishers = values },
		},
		{
			name: "places published", set: SetPlacesPublished, get: GetPlacesPublished,
			setScalar:   func(record *hubv1.Record, value string) { record.PlacePublished = value },
			getScalar:   func(record *hubv1.Record) string { return record.GetPlacePublished() },
			setRepeated: func(record *hubv1.Record, values []string) { record.PlacesPublished = values },
		},
		{
			name: "physical descriptions", set: SetPhysicalDescriptions, get: GetPhysicalDescriptions,
			setScalar:   func(record *hubv1.Record, value string) { record.PhysicalDesc = value },
			getScalar:   func(record *hubv1.Record) string { return record.GetPhysicalDesc() },
			setRepeated: func(record *hubv1.Record, values []string) { record.PhysicalDescriptions = values },
		},
		{
			name: "editions", set: SetEditions, get: GetEditions,
			setScalar:   func(record *hubv1.Record, value string) { record.Edition = value },
			getScalar:   func(record *hubv1.Record) string { return record.GetEdition() },
			setRepeated: func(record *hubv1.Record, values []string) { record.Editions = values },
		},
		{
			name: "languages", set: SetLanguages, get: GetLanguages,
			setScalar:   func(record *hubv1.Record, value string) { record.Language = value },
			getScalar:   func(record *hubv1.Record) string { return record.GetLanguage() },
			setRepeated: func(record *hubv1.Record, values []string) { record.Languages = values },
		},
	}
}

func TestRepeatedCompatibilitySettersPreserveOrderAndMirrorPrimary(t *testing.T) {
	want := []string{"First", "Second", "First"}
	for _, field := range repeatedCompatibilityFields() {
		t.Run(field.name, func(t *testing.T) {
			record := &hubv1.Record{}
			field.setScalar(record, "stale primary")
			field.set(record, []string{" First ", "Second", "First", ""})

			if got := field.get(record); !reflect.DeepEqual(got, want) {
				t.Fatalf("get() = %#v, want %#v", got, want)
			}
			if got := field.getScalar(record); got != want[0] {
				t.Fatalf("legacy scalar = %q, want primary %q", got, want[0])
			}
		})
	}
}

func TestRepeatedCompatibilityGettersUseLegacyScalarFallback(t *testing.T) {
	for _, field := range repeatedCompatibilityFields() {
		t.Run(field.name, func(t *testing.T) {
			record := &hubv1.Record{}
			field.setScalar(record, " Legacy value ")
			got := field.get(record)
			if want := []string{"Legacy value"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("get() = %#v, want %#v", got, want)
			}

			got[0] = "changed"
			if scalar := field.getScalar(record); scalar != " Legacy value " {
				t.Fatalf("get() returned a storage alias; scalar changed to %q", scalar)
			}
		})
	}
}

func TestRepeatedCompatibilityGettersPreferRepeatedValuesOverStaleScalar(t *testing.T) {
	want := []string{"Current", "Partner"}
	for _, field := range repeatedCompatibilityFields() {
		t.Run(field.name, func(t *testing.T) {
			record := &hubv1.Record{}
			field.setScalar(record, "stale primary")
			field.setRepeated(record, append([]string(nil), want...))

			if got := field.get(record); !reflect.DeepEqual(got, want) {
				t.Fatalf("get() = %#v, want %#v", got, want)
			}
			if got := field.getScalar(record); got != "stale primary" {
				t.Fatalf("get() mutated stale scalar to %q", got)
			}
		})
	}
}
