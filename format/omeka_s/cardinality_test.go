package omeka_s

import (
	"reflect"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

func TestDefaultPropertiesPreserveRepeatedPublishersAndLanguages(t *testing.T) {
	record := &hubv1.Record{}
	source := &resource{properties: []termValues{
		{term: "dcterms:publisher", values: omekaLiteralValues("Publisher One", "Publisher Two")},
		{term: "dcterms:language", values: omekaLiteralValues("eng", "fre")},
	}}
	mapProperties(record, source)

	if got, want := hub.GetPublishers(record), []string{"Publisher One", "Publisher Two"}; !reflect.DeepEqual(got, want) {
		t.Errorf("publishers = %#v, want %#v", got, want)
	}
	if got, want := hub.GetLanguages(record), []string{"eng", "fre"}; !reflect.DeepEqual(got, want) {
		t.Errorf("languages = %#v, want %#v", got, want)
	}
}

func TestCompiledCompatibilityMappingsAppendAllValuesInOrder(t *testing.T) {
	record := &hubv1.Record{}
	for _, test := range []struct {
		field string
		get   func(*hubv1.Record) []string
	}{
		{field: "Publisher", get: hub.GetPublishers},
		{field: "PlacePublished", get: hub.GetPlacesPublished},
		{field: "PhysicalDesc", get: hub.GetPhysicalDescriptions},
		{field: "Edition", get: hub.GetEditions},
		{field: "Language", get: hub.GetLanguages},
	} {
		if !appendOmekaCompatibilityValues(record, test.field, omekaLiteralValues(test.field+" One", test.field+" Two"), nil) {
			t.Fatalf("first %s mapping did not set a value", test.field)
		}
		if !appendOmekaCompatibilityValues(record, test.field, omekaLiteralValues(test.field+" Three"), nil) {
			t.Fatalf("append %s mapping did not set a value", test.field)
		}
		want := []string{test.field + " One", test.field + " Two", test.field + " Three"}
		if got := test.get(record); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, want %#v", test.field, got, want)
		}
	}
}

func omekaLiteralValues(values ...string) []valueObject {
	result := make([]valueObject, 0, len(values))
	for _, value := range values {
		result = append(result, valueObject{typeName: "literal", literal: value})
	}
	return result
}
