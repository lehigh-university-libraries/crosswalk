package proquest

import (
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestHubToSpokeUsesPrimaryRepeatedLanguage(t *testing.T) {
	record := &hubv1.Record{
		Language:  "stale language",
		Languages: []string{"eng", "fre"},
		Dates: []*hubv1.DateValue{
			{Type: hubv1.DateType_DATE_TYPE_ACCEPTED, Year: 2020},
			{Type: hubv1.DateType_DATE_TYPE_ACCEPTED, Year: 2021},
			{Type: hubv1.DateType_DATE_TYPE_ISSUED, Year: 2022},
			{Type: hubv1.DateType_DATE_TYPE_ISSUED, Year: 2023},
		},
	}
	submission, err := hubToSpoke(record)
	if err != nil {
		t.Fatalf("hubToSpoke() error = %v", err)
	}
	if submission.Description.Categorization == nil || submission.Description.Categorization.Language != "eng" {
		t.Fatalf("primary language = %#v, want eng", submission.Description.Categorization)
	}
	if submission.Description.Dates == nil || submission.Description.Dates.AcceptDate != "2020" || submission.Description.Dates.CompletionDate != "2022" {
		t.Errorf("primary dates = %#v, want accepted 2020 and completion 2022", submission.Description.Dates)
	}
}
