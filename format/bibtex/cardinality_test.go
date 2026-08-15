package bibtex

import (
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestHubToSpokeUsesPrimaryRepeatedCompatibilityValues(t *testing.T) {
	record := &hubv1.Record{
		Publisher:       "stale publisher",
		PlacePublished:  "stale place",
		Edition:         "stale edition",
		Language:        "stale language",
		Publishers:      []string{"Publisher One", "Publisher Two"},
		PlacesPublished: []string{"Halifax", "Moncton"},
		Editions:        []string{"First edition", "Revised edition"},
		Languages:       []string{"eng", "fre"},
	}
	entry, err := hubToSpoke(record)
	if err != nil {
		t.Fatalf("hubToSpoke() error = %v", err)
	}
	if entry.Publisher != "Publisher One" || entry.Address != "Halifax" || entry.Edition != "First edition" || entry.Language != "eng" {
		t.Errorf("primary values = publisher %q, address %q, edition %q, language %q", entry.Publisher, entry.Address, entry.Edition, entry.Language)
	}
}
