package hub

import (
	"reflect"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestSetPublishersPreservesOrderAndPrimaryPublisher(t *testing.T) {
	record := &hubv1.Record{Publisher: "old primary"}
	SetPublishers(record, []string{" First Press ", "Second Press", "First Press", ""})

	want := []string{"First Press", "Second Press", "First Press"}
	if !reflect.DeepEqual(record.GetPublishers(), want) {
		t.Fatalf("Record.Publishers = %#v, want %#v", record.GetPublishers(), want)
	}
	if record.GetPublisher() != want[0] {
		t.Fatalf("Record.Publisher = %q, want primary %q", record.GetPublisher(), want[0])
	}
}

func TestGetPublishersUsesLegacyScalarFallback(t *testing.T) {
	record := &hubv1.Record{Publisher: " Legacy Press "}
	got := GetPublishers(record)
	if want := []string{"Legacy Press"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("GetPublishers() = %#v, want %#v", got, want)
	}

	got[0] = "changed"
	if record.GetPublisher() != " Legacy Press " {
		t.Fatalf("GetPublishers() returned storage alias; scalar changed to %q", record.GetPublisher())
	}
}

func TestGetPublishersPrefersRepeatedValues(t *testing.T) {
	record := &hubv1.Record{
		Publisher:  "stale primary",
		Publishers: []string{"Current Press", "Partner Press"},
	}
	if want := []string{"Current Press", "Partner Press"}; !reflect.DeepEqual(GetPublishers(record), want) {
		t.Fatalf("GetPublishers() = %#v, want %#v", GetPublishers(record), want)
	}
}
