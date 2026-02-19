package drupal

import (
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
)

func TestResourceTypeFromGenreAuthorityURI(t *testing.T) {
	tests := []struct {
		name  string
		uri   string
		want  hubv1.ResourceTypeValue
		match bool
	}{
		{name: "thesis uri 300028029", uri: "http://vocab.getty.edu/page/aat/300028029", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS, match: true},
		{name: "thesis uri 300028028", uri: "http://vocab.getty.edu/page/aat/300028028", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS, match: true},
		{name: "article uri 300048715", uri: "http://vocab.getty.edu/page/aat/300048715", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE, match: true},
		{name: "article uri 300048715 with trailing slash", uri: "http://vocab.getty.edu/page/aat/300048715/", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE, match: true},
		{name: "dataset loc genreform", uri: "http://id.loc.gov/authorities/genreForms/gf2018026119", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET, match: true},
		{name: "unmapped", uri: "http://example.org/not-mapped", match: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resourceTypeFromGenreAuthorityURI(tt.uri)
			if ok != tt.match {
				t.Fatalf("resourceTypeFromGenreAuthorityURI(%q) match=%v, want %v", tt.uri, ok, tt.match)
			}
			if tt.match && got != tt.want {
				t.Fatalf("resourceTypeFromGenreAuthorityURI(%q) type=%v, want %v", tt.uri, got, tt.want)
			}
		})
	}
}

func TestParseGenreSetsArticleResourceTypeFromAuthorityURI(t *testing.T) {
	input := `{
		"title": [{"value": "Test"}],
		"field_genre": [{
			"target_id": 123,
			"target_type": "taxonomy_term",
			"_entity": {
				"name": [{"value": "Some Genre"}],
				"field_authority_link": [{
					"uri": "http://vocab.getty.edu/page/aat/300028029",
					"title": "",
					"source": "aat"
				}]
			}
		}]
	}`

	p := &mapping.Profile{
		Name:   "test",
		Format: "drupal",
		Fields: map[string]mapping.FieldMapping{
			"title":       {IR: "Title"},
			"field_genre": {IR: "Genre", Resolve: "taxonomy_term"},
		},
	}

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), &format.ParseOptions{Profile: p})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.ResourceType == nil || r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS {
		t.Fatalf("resource type = %v, want THESIS", r.ResourceType)
	}
	if len(r.Genres) != 1 {
		t.Fatalf("expected 1 genre, got %d", len(r.Genres))
	}
}

func TestParseResourceTypeFromAuthorityURIWhenGenreMappedAsResourceType(t *testing.T) {
	input := `{
		"title": [{"value": "Test"}],
		"field_genre": [{
			"target_id": 456,
			"target_type": "taxonomy_term",
			"_entity": {
				"name": [{"value": "Not article label"}],
				"field_authority_link": [{
					"uri": "http://vocab.getty.edu/page/aat/300028028",
					"title": "",
					"source": "aat"
				}]
			}
		}]
	}`

	p := &mapping.Profile{
		Name:   "test",
		Format: "drupal",
		Fields: map[string]mapping.FieldMapping{
			"title":       {IR: "Title"},
			"field_genre": {IR: "ResourceType", Resolve: "taxonomy_term"},
		},
	}

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), &format.ParseOptions{Profile: p})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.ResourceType == nil || r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS {
		t.Fatalf("resource type = %v, want THESIS", r.ResourceType)
	}
}

func TestParseGenreAuthorityNotClobberedByUnresolvedResourceType(t *testing.T) {
	input := `{
		"title": [{"value": "Test"}],
		"field_resource_type": [{
			"target_id": 11,
			"target_type": "taxonomy_term"
		}],
		"field_genre": [{
			"target_id": 2026,
			"target_type": "taxonomy_term",
			"_entity": {
				"name": [{"value": "dissertations"}],
				"field_authority_link": [{
					"uri": "http://vocab.getty.edu/page/aat/300028029",
					"title": "",
					"source": "aat"
				}]
			}
		}]
	}`

	p := &mapping.Profile{
		Name:   "test",
		Format: "drupal",
		Fields: map[string]mapping.FieldMapping{
			"title":               {IR: "Title"},
			"field_resource_type": {IR: "ResourceType", Resolve: "taxonomy_term"},
			"field_genre":         {IR: "Genre", Resolve: "taxonomy_term"},
		},
	}

	f := &Format{}
	// Parse repeatedly to exercise randomized map iteration order.
	for i := 0; i < 100; i++ {
		records, err := f.Parse(strings.NewReader(input), &format.ParseOptions{Profile: p})
		if err != nil {
			t.Fatalf("Parse failed on iteration %d: %v", i, err)
		}
		if len(records) != 1 {
			t.Fatalf("iteration %d: expected 1 record, got %d", i, len(records))
		}
		r := records[0]
		if r.ResourceType == nil || r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS {
			t.Fatalf("iteration %d: resource type = %v, want THESIS", i, r.ResourceType)
		}
	}
}

func TestDefaultProfile_FieldKeywordsResolvesTermLabels(t *testing.T) {
	input := `{
		"title": [{"value": "Keyword label test"}],
		"field_keywords": [
			{
				"target_id": 159882,
				"target_type": "taxonomy_term",
				"_entity": {"name": [{"value": "organic semiconductors"}]}
			},
			{
				"target_id": 159883,
				"target_type": "taxonomy_term",
				"_entity": {"name": [{"value": "charge transport"}]}
			}
		]
	}`

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), format.NewParseOptions())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if len(r.Subjects) != 2 {
		t.Fatalf("subjects count = %d, want 2", len(r.Subjects))
	}
	if r.Subjects[0].Value != "organic semiconductors" {
		t.Fatalf("subjects[0].Value = %q, want %q", r.Subjects[0].Value, "organic semiconductors")
	}
	if r.Subjects[0].Vocabulary != hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS {
		t.Fatalf("subjects[0].Vocabulary = %v, want KEYWORDS", r.Subjects[0].Vocabulary)
	}
	if r.Subjects[1].Value != "charge transport" {
		t.Fatalf("subjects[1].Value = %q, want %q", r.Subjects[1].Value, "charge transport")
	}
}
