package csl

import (
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestHubToSpokeUsesPrimaryRepeatedCompatibilityValues(t *testing.T) {
	record := &hubv1.Record{
		Publisher:            "stale publisher",
		PlacePublished:       "stale place",
		Edition:              "stale edition",
		Language:             "stale language",
		Publishers:           []string{"Publisher One", "Publisher Two"},
		PlacesPublished:      []string{"Halifax", "Moncton"},
		Editions:             []string{"First edition", "Revised edition"},
		Languages:            []string{"eng", "fre"},
		PhysicalDescriptions: []string{"12 pages", "1 map"},
		Dimensions:           "30 cm",
	}
	item, err := hubToSpoke(record)
	if err != nil {
		t.Fatalf("hubToSpoke() error = %v", err)
	}
	if item.Publisher != "Publisher One" || item.PublisherPlace != "Halifax" || item.Dimensions != "30 cm" || item.Edition != "First edition" || item.Language != "eng" {
		t.Errorf("primary values = publisher %q, place %q, dimensions %q, edition %q, language %q", item.Publisher, item.PublisherPlace, item.Dimensions, item.Edition, item.Language)
	}
}
