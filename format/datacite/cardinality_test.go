package datacite

import (
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestHubToSpokeUsesPrimaryRepeatedCompatibilityValues(t *testing.T) {
	record := &hubv1.Record{
		Publisher:  "stale publisher",
		Language:   "stale language",
		Publishers: []string{"Publisher One", "Publisher Two"},
		Languages:  []string{"eng", "fre"},
	}
	resource, err := hubToSpoke(record)
	if err != nil {
		t.Fatalf("hubToSpoke() error = %v", err)
	}
	if resource.Publisher != "Publisher One" || resource.Language != "eng" {
		t.Errorf("primary values = publisher %q, language %q", resource.Publisher, resource.Language)
	}
}
