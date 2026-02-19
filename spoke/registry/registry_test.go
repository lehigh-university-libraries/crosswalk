package registry

import "testing"

func TestBuildProfile_MapsFieldGenreToGenre(t *testing.T) {
	fields := map[string]FieldMeta{
		"genre": {
			DrupalField:  "field_genre",
			HubField:     "ResourceType",
			TargetType:   "taxonomy_term",
			TargetBundle: "genre",
			Cardinality:  -1,
		},
		"resource_type": {
			DrupalField:  "field_resource_type",
			HubField:     "ResourceType",
			TargetType:   "taxonomy_term",
			TargetBundle: "resource_types",
			Cardinality:  1,
		},
	}

	p := buildProfile("drupal", fields)

	genre, ok := p.Fields["field_genre"]
	if !ok {
		t.Fatalf("field_genre not present in profile")
	}
	if genre.IR != "Genre" {
		t.Fatalf("field_genre IR = %q, want %q", genre.IR, "Genre")
	}

	rt, ok := p.Fields["field_resource_type"]
	if !ok {
		t.Fatalf("field_resource_type not present in profile")
	}
	if rt.IR != "ResourceType" {
		t.Fatalf("field_resource_type IR = %q, want %q", rt.IR, "ResourceType")
	}
}
