package crossref

import (
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestBookUsesPrimaryRepeatedCompatibilityValues(t *testing.T) {
	record := &hubv1.Record{
		Publisher:       "stale publisher",
		PlacePublished:  "stale place",
		Edition:         "stale edition",
		Publishers:      []string{"Publisher One", "Publisher Two"},
		PlacesPublished: []string{"Halifax", "Moncton"},
		Editions:        []string{"First edition", "Revised edition"},
	}
	book := buildBook(record)
	if book.BookMetadata.Publisher == nil {
		t.Fatal("publisher is nil")
	}
	if book.BookMetadata.Publisher.PublisherName != "Publisher One" || book.BookMetadata.Publisher.PublisherPlace != "Halifax" || book.BookMetadata.EditionNumber != "First edition" {
		t.Errorf("primary values = publisher %q, place %q, edition %q", book.BookMetadata.Publisher.PublisherName, book.BookMetadata.Publisher.PublisherPlace, book.BookMetadata.EditionNumber)
	}
}
