package bibtex

import (
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestHubToSpoke_RelatorsExcludeThesisAdvisorFromAuthor(t *testing.T) {
	record := &hubv1.Record{
		Title: "Relator Role Test",
		Contributors: []*hubv1.Contributor{
			{
				Name:     "Alex Rivera",
				Role:     "Creator",
				RoleCode: "relators:cre",
				ParsedName: &hubv1.ParsedName{
					Given:  "Alex",
					Family: "Rivera",
				},
			},
			{
				Name:     "Jordan Lee",
				Role:     "Thesis advisor",
				RoleCode: "relators:ths",
				ParsedName: &hubv1.ParsedName{
					Given:  "Jordan",
					Family: "Lee",
				},
			},
		},
	}

	entry, err := hubToSpoke(record)
	if err != nil {
		t.Fatalf("hubToSpoke failed: %v", err)
	}

	if len(entry.Author) != 1 {
		t.Fatalf("author count = %d, want 1", len(entry.Author))
	}
	if entry.Author[0].Family != "Rivera" {
		t.Fatalf("author[0].Family = %q, want %q", entry.Author[0].Family, "Rivera")
	}
	if len(entry.Editor) != 0 {
		t.Fatalf("editor count = %d, want 0", len(entry.Editor))
	}
}
