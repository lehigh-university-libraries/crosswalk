package drupal

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
)

func TestDefaultProfilePreservesMultiplePublishers(t *testing.T) {
	input := `{
		"title": [{"value": "Multiple publishers"}],
		"field_publisher": [
			{"value": "First Press"},
			{"value": "Second Press"},
			{"value": "Third Press"}
		]
	}`

	formatPlugin := &Format{}
	records, err := formatPlugin.Parse(strings.NewReader(input), format.NewParseOptions())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Parse() records = %d, want 1", len(records))
	}
	want := []string{"First Press", "Second Press", "Third Press"}
	if !slices.Equal(records[0].GetPublishers(), want) {
		t.Fatalf("Record.Publishers = %#v, want %#v", records[0].GetPublishers(), want)
	}
	if records[0].GetPublisher() != want[0] {
		t.Fatalf("Record.Publisher = %q, want primary %q", records[0].GetPublisher(), want[0])
	}

	var output bytes.Buffer
	if err := formatPlugin.Serialize(&output, records, format.NewSerializeOptions()); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	var entity DrupalEntity
	if err := json.Unmarshal(output.Bytes(), &entity); err != nil {
		t.Fatalf("decoding serialized Drupal JSON: %v", err)
	}
	got, err := ExtractStrings(entity["field_publisher"])
	if err != nil {
		t.Fatalf("ExtractStrings(field_publisher) error = %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("serialized field_publisher = %#v, want %#v", got, want)
	}
}

func TestDefaultProfilePreservesRepeatedBibliographicValues(t *testing.T) {
	input := `{
		"field_language": [
			{"target_id":"eng","_entity":{"name":[{"value":"English"}]}},
			{"target_id":"fre","_entity":{"name":[{"value":"French"}]}}
		],
		"field_place_published": [{"value":"Halifax"},{"value":"Moncton"}],
		"field_extent": [{"value":"fallback extent"}],
		"field_physical_description": [{"value":"12 pages"},{"value":"1 map"}],
		"field_edition": [{"value":"First edition"},{"value":"Revised edition"}],
		"field_edtf_date_issued": [{"value":"2020"},{"value":"2021"}],
		"field_subject": [{"target_id":"1","_entity":{"name":[{"value":"Newspapers"}]}}],
		"field_keywords": [{"target_id":"2","_entity":{"name":[{"value":"Atlantic Canada"}]}}]
	}`

	f := &Format{}
	parseOptions := format.NewParseOptions()
	// The generated site profile supplies the Drupal storage type while the
	// built-in physical-description mapping remains the preferred source.
	parseOptions.Profile = &mapping.Profile{Fields: map[string]mapping.FieldMapping{
		"field_extent": {IR: "PhysicalDesc", Type: "string", Priority: -1},
	}}
	records, err := f.Parse(strings.NewReader(input), parseOptions)
	if err != nil {
		t.Fatal(err)
	}
	record := records[0]
	assertValues := func(name string, got, want []string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Fatalf("%s = %#v, want %#v", name, got, want)
		}
	}
	assertValues("languages", hub.GetLanguages(record), []string{"English", "French"})
	assertValues("places", hub.GetPlacesPublished(record), []string{"Halifax", "Moncton"})
	assertValues("physical descriptions", hub.GetPhysicalDescriptions(record), []string{"12 pages", "1 map"})
	assertValues("editions", hub.GetEditions(record), []string{"First edition", "Revised edition"})
	if got := []string{record.GetSubjects()[0].GetValue(), record.GetSubjects()[1].GetValue()}; !slices.Equal(got, []string{"Atlantic Canada", "Newspapers"}) {
		t.Fatalf("subjects from distinct Drupal fields = %#v", got)
	}
	if record.GetLanguage() != "English" || record.GetPlacePublished() != "Halifax" ||
		record.GetPhysicalDesc() != "12 pages" || record.GetEdition() != "First edition" {
		t.Fatalf("legacy primaries were not mirrored: %#v", record)
	}

	var output bytes.Buffer
	if err := f.Serialize(&output, records, format.NewSerializeOptions()); err != nil {
		t.Fatal(err)
	}
	var entity DrupalEntity
	if err := json.Unmarshal(output.Bytes(), &entity); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		field string
		want  []string
	}{
		{field: "field_language", want: []string{"English", "French"}},
		{field: "field_place_published", want: []string{"Halifax", "Moncton"}},
		{field: "field_physical_description", want: []string{"12 pages", "1 map"}},
		{field: "field_edition", want: []string{"First edition", "Revised edition"}},
		{field: "field_edtf_date_issued", want: []string{"2020", "2021"}},
	} {
		got, err := ExtractStrings(entity[test.field])
		if err != nil {
			t.Fatalf("ExtractStrings(%s): %v", test.field, err)
		}
		assertValues("serialized "+test.field, got, test.want)
	}
}

func TestDefaultProfileUsesExtentAsPhysicalDescriptionFallback(t *testing.T) {
	records, err := (&Format{}).Parse(strings.NewReader(`{
		"field_extent":[{"value":"123 pages"},{"value":"2 volumes"}]
	}`), format.NewParseOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hub.GetPhysicalDescriptions(records[0]), []string{"123 pages", "2 volumes"}; !slices.Equal(got, want) {
		t.Fatalf("physical descriptions = %#v, want %#v", got, want)
	}
}

func TestStaticProfileSerializesEveryDateOfEveryMappedType(t *testing.T) {
	p := &mapping.Profile{Fields: map[string]mapping.FieldMapping{
		"field_issued":           {IR: "Dates", DateType: "issued", Parser: "edtf"},
		"field_issued_alternate": {IR: "Dates", DateType: "issued", Parser: "edtf"},
		"field_modified":         {IR: "Dates", DateType: "modified", Parser: "edtf"},
	}}
	input := `{
		"field_issued":[{"value":"2020"},{"value":"2021"}],
		"field_issued_alternate":[{"value":"2024"}],
		"field_modified":[{"value":"2022"},{"value":"2023"}]
	}`
	records, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{Profile: p})
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, &format.SerializeOptions{Profile: p}); err != nil {
		t.Fatal(err)
	}
	var entity DrupalEntity
	if err := json.Unmarshal(output.Bytes(), &entity); err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string][]string{
		"field_issued":   {"2020", "2021", "2024"},
		"field_modified": {"2022", "2023"},
	} {
		got, err := ExtractStrings(entity[field])
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s = %#v, want %#v", field, got, want)
		}
	}
}

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
		{name: "map atlas", uri: "http://vocab.getty.edu/page/aat/300028053", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_MAP, match: true},
		{name: "periodical journal", uri: "http://vocab.getty.edu/page/aat/300215390", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_PERIODICAL, match: true},
		{name: "report", uri: "http://vocab.getty.edu/page/aat/300027267", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT, match: true},
		{name: "presentation", uri: "http://vocab.getty.edu/page/aat/300258677", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_PRESENTATION, match: true},
		{name: "poster", uri: "http://vocab.getty.edu/page/aat/300426530", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_POSTER, match: true},
		{name: "manuscript letters", uri: "http://vocab.getty.edu/page/aat/300026879", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_MANUSCRIPT, match: true},
		{name: "book-like novel", uri: "http://vocab.getty.edu/page/aat/300202580", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK, match: true},
		{name: "generic text", uri: "http://vocab.getty.edu/page/aat/300417822", want: hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT, match: true},
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

func TestParseMemberOfPreservesParentDoiInRelationDescription(t *testing.T) {
	input := `{
		"title": [{"value": "Operationalizing Ghana's mobile money reforms"}],
		"nid": [{"value": 504922}],
		"path": [{"alias": "/lehigh-scholarship/example"}],
		"field_member_of": [{
			"target_id": 453223,
			"target_type": "node",
			"url": "/node/453223",
			"_entity": {
				"title": [{"value": "Martindale Policy Briefs"}],
				"uuid": [{"value": "caf1bda2-5b5f-48b0-a607-243439faa3ca"}],
				"field_genre": [{
					"target_id": 1,
					"target_type": "taxonomy_term",
					"_entity": {
						"name": [{"value": "journals"}],
						"field_authority_link": [{
							"uri": "http://vocab.getty.edu/page/aat/300215390",
							"source": "aat"
						}]
					}
				}],
				"field_identifier": [{
					"attr0": "doi",
					"value": "10.18275/martindale-pb"
				}]
			}
		}]
	}`

	p := &mapping.Profile{
		Name:   "test",
		Format: "drupal",
		Fields: map[string]mapping.FieldMapping{
			"title":           {IR: "Title"},
			"nid":             {IR: "Extra.nid"},
			"path":            {IR: "Extra.path"},
			"field_member_of": {IR: "Relations", RelationType: "member_of", Resolve: "node"},
		},
	}

	f := &Format{}
	records, err := f.Parse(strings.NewReader(input), &format.ParseOptions{
		Profile: p,
		BaseURL: "https://preserve.lehigh.edu",
	})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	r := records[0]
	if len(r.Relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(r.Relations))
	}
	if r.Relations[0].TargetTitle != "Martindale Policy Briefs" {
		t.Fatalf("target title = %q", r.Relations[0].TargetTitle)
	}

	var meta struct {
		DOI      string   `json:"doi"`
		Resource string   `json:"resource"`
		Genres   []string `json:"genres"`
	}
	if err := json.Unmarshal([]byte(r.Relations[0].Description), &meta); err != nil {
		t.Fatalf("relation description is not JSON: %v", err)
	}
	if meta.DOI != "10.18275/martindale-pb" {
		t.Fatalf("parent DOI = %q", meta.DOI)
	}
	if meta.Resource != "https://preserve.lehigh.edu/node/453223" {
		t.Fatalf("parent resource = %q", meta.Resource)
	}
	if len(meta.Genres) != 1 || meta.Genres[0] != "http://vocab.getty.edu/page/aat/300215390" {
		t.Fatalf("parent genres = %#v", meta.Genres)
	}

	foundURL := false
	for _, id := range r.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_URL &&
			id.Value == "https://preserve.lehigh.edu/node/504922" {
			foundURL = true
		}
	}
	if !foundURL {
		t.Fatalf("expected record URL identifier, got %#v", r.Identifiers)
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

func TestIdentifierTypeFromStringRecognizesWebOfScience(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"wos", "Web of Science", "web-of-science"} {
		if got := identifierTypeFromString(label); got != hubv1.IdentifierType_IDENTIFIER_TYPE_WOS {
			t.Fatalf("identifierTypeFromString(%q) = %v, want WOS", label, got)
		}
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
