package dublincore

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

func TestRepeatedPublishersAndLanguagesRoundTrip(t *testing.T) {
	record := &hubv1.Record{
		Title:      "Repeated values",
		Publisher:  "stale publisher",
		Publishers: []string{"Publisher One", "Publisher Two"},
		Languages:  []string{"eng", "fre"},
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
	if got := hub.GetPublishers(parsed[0]); !reflect.DeepEqual(got, record.Publishers) {
		t.Errorf("publishers = %#v, want %#v", got, record.Publishers)
	}
	if got := hub.GetLanguages(parsed[0]); !reflect.DeepEqual(got, record.Languages) {
		t.Errorf("languages = %#v, want %#v", got, record.Languages)
	}
}
