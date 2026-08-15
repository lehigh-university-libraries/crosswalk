package drupal

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

func TestCompiledProfilePreservesMultiplePublishers(t *testing.T) {
	compiled := compileDrupalEncodingProfile(t, []model.Field{{
		Path: "field_publisher", SourceType: "string", Kind: model.ValueText, Cardinality: -1,
	}}, []profile.Mapping{{
		Field: profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "field_publisher"},
		Hub:   "Publisher", Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty,
	}}, nil)
	want := []string{"First Press", "Second Press", "Third Press"}
	input := `{"field_publisher":[{"value":"First Press"},{"value":"Second Press"},{"value":"Third Press"}]}`

	records, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 || !slices.Equal(records[0].GetPublishers(), want) {
		t.Fatalf("Parse() publishers = %#v, want %#v", records[0].GetPublishers(), want)
	}

	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, records, &format.SerializeOptions{SystemProfile: compiled}); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	var entity map[string][]map[string]any
	if err := json.Unmarshal(output.Bytes(), &entity); err != nil {
		t.Fatalf("decoding serialized entity: %v", err)
	}
	got := make([]string, 0, len(entity["field_publisher"]))
	for _, value := range entity["field_publisher"] {
		got = append(got, value["value"].(string))
	}
	if !slices.Equal(got, want) {
		t.Fatalf("serialized field_publisher = %#v, want %#v", got, want)
	}
}

func TestCompiledProfileAppendMergesCompatibilityFieldsAcrossSources(t *testing.T) {
	tests := []struct {
		name string
		hub  string
		get  func(*hubv1.Record) []string
	}{
		{name: "publishers", hub: "Publisher", get: hub.GetPublishers},
		{name: "places published", hub: "PlacePublished", get: hub.GetPlacesPublished},
		{name: "physical descriptions", hub: "PhysicalDesc", get: hub.GetPhysicalDescriptions},
		{name: "editions", hub: "Edition", get: hub.GetEditions},
		{name: "languages", hub: "Language", get: hub.GetLanguages},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields := []model.Field{
				{Path: "field_first", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
				{Path: "field_second", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
			}
			selector := func(path string) profile.FieldSelector {
				return profile.FieldSelector{EntityType: "node", Bundle: "article", Path: path}
			}
			compiled := compileDrupalEncodingProfile(t, fields, []profile.Mapping{
				{Field: selector("field_first"), Hub: test.hub, Decode: "text", Encode: "none", Merge: profile.MergeAppend},
				{Field: selector("field_second"), Hub: test.hub, Decode: "text", Encode: "none", Merge: profile.MergeAppend},
			}, nil)

			records, err := (&Format{}).Parse(strings.NewReader(`{
				"field_first":[{"value":"one"},{"value":"two"}],
				"field_second":[{"value":"three"},{"value":"four"}]
			}`), &format.ParseOptions{SystemProfile: compiled})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"one", "two", "three", "four"}
			if got := test.get(records[0]); !slices.Equal(got, want) {
				t.Fatalf("values = %#v, want %#v", got, want)
			}
		})
	}
}

func TestCompiledProfilePreservesRepeatedCompatibilityFields(t *testing.T) {
	fields := []model.Field{
		{Path: "field_language", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
		{Path: "field_place", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
		{Path: "field_physical", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
		{Path: "field_edition", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
	}
	selector := func(path string) profile.FieldSelector {
		return profile.FieldSelector{EntityType: "node", Bundle: "article", Path: path}
	}
	compiled := compileDrupalEncodingProfile(t, fields, []profile.Mapping{
		{Field: selector("field_language"), Hub: "Language", Decode: "text", Encode: "text", Merge: profile.MergeAppend},
		{Field: selector("field_place"), Hub: "PlacePublished", Decode: "text", Encode: "text", Merge: profile.MergeAppend},
		{Field: selector("field_physical"), Hub: "PhysicalDesc", Decode: "text", Encode: "text", Merge: profile.MergeAppend},
		{Field: selector("field_edition"), Hub: "Edition", Decode: "text", Encode: "text", Merge: profile.MergeAppend},
	}, nil)
	wants := map[string][]string{
		"field_language": {"English", "French"},
		"field_place":    {"Halifax", "Moncton"},
		"field_physical": {"12 pages", "1 map"},
		"field_edition":  {"First edition", "Revised edition"},
	}
	input := `{
		"field_language":[{"value":"English"},{"value":"French"}],
		"field_place":[{"value":"Halifax"},{"value":"Moncton"}],
		"field_physical":[{"value":"12 pages"},{"value":"1 map"}],
		"field_edition":[{"value":"First edition"},{"value":"Revised edition"}]
	}`

	records, err := (&Format{}).Parse(strings.NewReader(input), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatal(err)
	}
	record := records[0]
	for name, test := range map[string]struct {
		get  func(*hubv1.Record) []string
		want []string
	}{
		"languages":             {get: hub.GetLanguages, want: wants["field_language"]},
		"places published":      {get: hub.GetPlacesPublished, want: wants["field_place"]},
		"physical descriptions": {get: hub.GetPhysicalDescriptions, want: wants["field_physical"]},
		"editions":              {get: hub.GetEditions, want: wants["field_edition"]},
	} {
		if got := test.get(record); !slices.Equal(got, test.want) {
			t.Fatalf("%s = %#v, want %#v", name, got, test.want)
		}
	}

	entity := serializeCompiledEntity(t, record, compiled)
	for field, want := range wants {
		if got := drupalFieldTexts(t, entity, field); !slices.Equal(got, want) {
			t.Fatalf("%s = %#v, want %#v", field, got, want)
		}
	}
}

func TestClearReplaceableCompatibilityFieldsClearsRepeatedAndScalarStorage(t *testing.T) {
	tests := []struct {
		name string
		path string
		seed func(*hubv1.Record)
		get  func(*hubv1.Record) []string
	}{
		{name: "publisher", path: "Publisher", seed: func(record *hubv1.Record) { hub.SetPublishers(record, []string{"one", "two"}) }, get: hub.GetPublishers},
		{name: "place", path: "PlacePublished", seed: func(record *hubv1.Record) { hub.SetPlacesPublished(record, []string{"one", "two"}) }, get: hub.GetPlacesPublished},
		{name: "physical description", path: "PhysicalDesc", seed: func(record *hubv1.Record) { hub.SetPhysicalDescriptions(record, []string{"one", "two"}) }, get: hub.GetPhysicalDescriptions},
		{name: "edition", path: "Edition", seed: func(record *hubv1.Record) { hub.SetEditions(record, []string{"one", "two"}) }, get: hub.GetEditions},
		{name: "language", path: "Language", seed: func(record *hubv1.Record) { hub.SetLanguages(record, []string{"one", "two"}) }, get: hub.GetLanguages},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := &hubv1.Record{}
			test.seed(record)
			if err := clearReplaceableHubValue(record, test.path); err != nil {
				t.Fatal(err)
			}
			if got := test.get(record); len(got) != 0 {
				t.Fatalf("get() = %#v, want empty", got)
			}
		})
	}
}

func TestEncodeEntityWithProfileRejectsNilInputs(t *testing.T) {
	compiled := compileDrupalEncodingProfile(t, []model.Field{{
		Path: "field_title", SourceType: "string", Kind: model.ValueText, Cardinality: 1,
	}}, []profile.Mapping{{
		Field: profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "field_title"},
		Hub:   "Title", Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty,
	}}, nil)

	tests := []struct {
		name     string
		record   *hubv1.Record
		compiled *profile.Compiled
		want     string
	}{
		{name: "nil record", compiled: compiled, want: "encoding Drupal entity: record is nil"},
		{name: "nil compiled profile", record: &hubv1.Record{}, want: "encoding Drupal entity: compiled profile is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := EncodeEntityWithProfile(test.record, test.compiled)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("EncodeEntityWithProfile() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestCompiledProfileEncoderAppliesOrderedTargetMerges(t *testing.T) {
	fields := []model.Field{
		{Path: "field_values", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
		{Path: "field_choice", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
		{Path: "field_replace", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
	}
	selector := func(path string) profile.FieldSelector {
		return profile.FieldSelector{EntityType: "node", Bundle: "article", Path: path}
	}
	compiled := compileDrupalEncodingProfile(t, fields, []profile.Mapping{
		{Field: selector("field_values"), Hub: "AltTitle", Decode: "text", Encode: "text", Merge: profile.MergeAppend},
		{Field: selector("field_values"), Hub: "Notes", Decode: "text", Encode: "text", Merge: profile.MergeAppend},
		{Field: selector("field_choice"), Hub: "Title", Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_choice"), Hub: "FullTitle", Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_replace"), Hub: "Title", Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_replace"), Hub: "FullTitle", Decode: "text", Encode: "text", Merge: profile.MergeReplace},
	}, nil)
	record := &hubv1.Record{Title: "Display", FullTitle: "Complete", AltTitle: []string{"First alternate", "Second alternate"}, Notes: []string{"Note"}}

	entity := serializeCompiledEntity(t, record, compiled)
	if got := drupalFieldTexts(t, entity, "field_values"); strings.Join(got, "|") != "First alternate|Second alternate|Note" {
		t.Fatalf("field_values = %#v", got)
	}
	if got := drupalFieldTexts(t, entity, "field_choice"); strings.Join(got, "|") != "Display" {
		t.Fatalf("field_choice = %#v", got)
	}
	if got := drupalFieldTexts(t, entity, "field_replace"); strings.Join(got, "|") != "Complete" {
		t.Fatalf("field_replace = %#v", got)
	}
}

func TestCompiledProfileEncoderRejectsTargetCardinalityOverflow(t *testing.T) {
	field := model.Field{Path: "field_single", SourceType: "string", Kind: model.ValueText, Cardinality: 1}
	selector := profile.FieldSelector{EntityType: "node", Bundle: "article", Path: field.Path}
	compiled := compileDrupalEncodingProfile(t, []model.Field{field}, []profile.Mapping{{
		Field: selector, Hub: "AltTitle", Decode: "text", Encode: "text", Merge: profile.MergeAppend,
	}}, nil)

	var output bytes.Buffer
	err := (&Format{}).Serialize(&output, []*hubv1.Record{{AltTitle: []string{"one", "two"}}}, &format.SerializeOptions{SystemProfile: compiled})
	if err == nil || !strings.Contains(err.Error(), `encodes 2 values for Drupal field "field_single" with cardinality 1`) {
		t.Fatalf("Serialize() error = %v", err)
	}
}

func TestCompiledProfileEncoderUsesDeclaredCodecs(t *testing.T) {
	fields := []model.Field{
		{Path: "field_integer", SourceType: "integer", Kind: model.ValueInteger, Cardinality: 1},
		{Path: "field_decimal", SourceType: "decimal", Kind: model.ValueDecimal, Cardinality: 1},
		{Path: "field_boolean", SourceType: "boolean", Kind: model.ValueBoolean, Cardinality: 1},
		{Path: "field_link", SourceType: "link", Kind: model.ValueLink, Cardinality: 1},
		{Path: "field_file", SourceType: "file", Kind: model.ValueFile, Cardinality: -1},
		{Path: "field_composite", SourceType: "paragraph", Kind: model.ValueComposite, Cardinality: 1},
		{Path: "field_opaque", SourceType: "institution_widget", Kind: model.ValueOpaque, Cardinality: 1},
	}
	selector := func(path string) profile.FieldSelector {
		return profile.FieldSelector{EntityType: "node", Bundle: "article", Path: path}
	}
	integerSelector := selector("field_integer")
	integerSelector.Attribute = "number"
	compiled := compileDrupalEncodingProfile(t, fields, []profile.Mapping{
		{Field: integerSelector, Hub: "Extra.integer", Decode: "integer", Encode: "integer", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_decimal"), Hub: "Extra.decimal", Decode: "decimal", Encode: "decimal", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_boolean"), Hub: "Extra.boolean", Decode: "boolean", Encode: "boolean", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_link"), Hub: "Extra.link", Decode: "link", Encode: "link", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_file"), Hub: "Files.supplemental", Decode: "file", Encode: "file", Merge: profile.MergeAppend},
		{Field: selector("field_composite"), Hub: "Extra.composite", Decode: "composite", Encode: "composite", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_opaque"), Hub: "Extra.opaque", Decode: "opaque", Encode: "opaque", Merge: profile.MergeFirstNonempty},
	}, nil)
	record := &hubv1.Record{Files: []*hubv1.File{
		{Path: "/data/table.csv", Name: "table.csv", MimeType: "text/csv", SizeBytes: 128, Role: "supplemental"},
		{Path: "/data/primary.pdf", Name: "primary.pdf", Role: "primary"},
	}}
	hub.SetExtra(record, "integer", 7)
	hub.SetExtra(record, "decimal", 2.5)
	hub.SetExtra(record, "boolean", false)
	hub.SetExtra(record, "link", "https://example.test/item")
	hub.SetExtra(record, "composite", map[string]any{"left": "a", "right": 2})
	hub.SetExtra(record, "opaque", map[string]any{"native": true})

	entity := serializeCompiledEntity(t, record, compiled)
	assertDrupalObjectValue(t, entity, "field_integer", "number", float64(7))
	assertDrupalObjectValue(t, entity, "field_decimal", "value", 2.5)
	assertDrupalObjectValue(t, entity, "field_boolean", "value", false)
	assertDrupalObjectValue(t, entity, "field_link", "uri", "https://example.test/item")
	assertDrupalObjectValue(t, entity, "field_composite", "left", "a")
	assertDrupalObjectValue(t, entity, "field_opaque", "native", true)
	var files []map[string]any
	if err := json.Unmarshal(entity["field_file"], &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0]["filename"] != "table.csv" || files[0]["filesize"] != float64(128) {
		t.Fatalf("field_file = %#v", files)
	}
	serialized, err := json.Marshal(entity)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := (&Format{}).Parse(bytes.NewReader(serialized), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed[0].GetFiles()) != 1 || parsed[0].GetFiles()[0].GetRole() != "supplemental" || parsed[0].GetFiles()[0].GetSizeBytes() != 128 {
		t.Fatalf("round-tripped files = %#v", parsed[0].GetFiles())
	}
}

func TestCompiledProfileEncoderRejectsInvalidNumericAndBooleanValues(t *testing.T) {
	fields := []model.Field{
		{Path: "field_integer", SourceType: "integer", Kind: model.ValueInteger, Cardinality: 1},
		{Path: "field_decimal", SourceType: "decimal", Kind: model.ValueDecimal, Cardinality: 1},
		{Path: "field_boolean", SourceType: "boolean", Kind: model.ValueBoolean, Cardinality: 1},
	}
	selector := func(path string) profile.FieldSelector {
		return profile.FieldSelector{EntityType: "node", Bundle: "article", Path: path}
	}
	compiled := compileDrupalEncodingProfile(t, fields, []profile.Mapping{
		{Field: selector("field_integer"), Hub: "Extra.integer", Decode: "integer", Encode: "integer", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_decimal"), Hub: "Extra.decimal", Decode: "decimal", Encode: "decimal", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_boolean"), Hub: "Extra.boolean", Decode: "boolean", Encode: "boolean", Merge: profile.MergeFirstNonempty},
	}, nil)

	tests := []struct {
		name  string
		key   string
		value any
		want  string
	}{
		{name: "malformed integer", key: "integer", value: "twelve", want: `parsing integer "twelve"`},
		{name: "integer string above maximum", key: "integer", value: "9223372036854775808", want: "value out of range"},
		{name: "integer string below minimum", key: "integer", value: "-9223372036854775809", want: "value out of range"},
		{name: "floating integer above maximum", key: "integer", value: math.Exp2(63), want: "is not an integer"},
		{name: "fractional integer", key: "integer", value: 1.5, want: "decimal 1.5 is not an integer"},
		{name: "malformed decimal", key: "decimal", value: "one point five", want: `parsing decimal "one point five"`},
		{name: "decimal NaN", key: "decimal", value: math.NaN(), want: "decimal NaN is not finite"},
		{name: "decimal positive infinity", key: "decimal", value: math.Inf(1), want: "decimal +Inf is not finite"},
		{name: "decimal negative infinity", key: "decimal", value: math.Inf(-1), want: "decimal -Inf is not finite"},
		{name: "malformed boolean string", key: "boolean", value: "yes", want: "expected a boolean Hub value, got string(yes)"},
		{name: "boolean integer outside domain", key: "boolean", value: 2, want: "expected a boolean Hub value, got float64(2)"},
		{name: "boolean decimal outside domain", key: "boolean", value: 0.5, want: "expected a boolean Hub value, got float64(0.5)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := &hubv1.Record{}
			hub.SetExtra(record, test.key, test.value)
			var output bytes.Buffer
			err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{SystemProfile: compiled})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Serialize() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestCompiledProfileEncoderRejectsSelectorPredicateConflicts(t *testing.T) {
	tests := []struct {
		name       string
		field      model.Field
		selector   profile.FieldSelector
		codec      string
		hub        string
		extraValue any
		record     func() *hubv1.Record
		want       string
	}{
		{
			name:     "scalar attribute conflicts",
			field:    model.Field{Path: "field_text", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			selector: profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "field_text", Attribute: "value", Where: &profile.FieldPredicate{Attribute: "value", Equals: "required"}},
			codec:    "text", extraValue: "actual",
			want: `selector predicate value="required" conflicts with encoded value "actual"`,
		},
		{
			name:     "composite attribute conflicts",
			field:    model.Field{Path: "field_composite", SourceType: "textfield_attr", Kind: model.ValueComposite, Cardinality: 1},
			selector: profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "field_composite", Where: &profile.FieldPredicate{Attribute: "attr0", Equals: "doi"}},
			codec:    "composite", extraValue: map[string]any{"attr0": "isbn", "value": "9781234567897"},
			want: `selector predicate attr0="doi" conflicts with encoded value "isbn"`,
		},
		{
			name: "typed relation string role conflicts",
			field: model.Field{
				Path: "field_agent", SourceType: "typed_relation", Kind: model.ValueTypedReference, Cardinality: 1,
				Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: []string{"person", "corporate_body"}},
			},
			selector:   profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "field_agent", Where: &profile.FieldPredicate{Attribute: "rel_type", Equals: "relators:aut"}},
			codec:      "typed-relation",
			extraValue: "relators:cre:person:Example, Alex",
			want:       `selector predicate rel_type="relators:aut" conflicts with encoded value "relators:cre"`,
		},
		{
			name: "typed relation contributor role conflicts",
			field: model.Field{
				Path: "field_agent", SourceType: "typed_relation", Kind: model.ValueTypedReference, Cardinality: 1,
				Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: []string{"person", "corporate_body"}},
			},
			selector: profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "field_agent", Where: &profile.FieldPredicate{Attribute: "rel_type", Equals: "relators:aut"}},
			codec:    "typed-relation",
			hub:      "Contributors",
			record: func() *hubv1.Record {
				return &hubv1.Record{Contributors: []*hubv1.Contributor{{
					Name: "Example, Alex", SourceId: "person:Example, Alex", RoleCode: "relators:cre",
				}}}
			},
			want: `selector predicate rel_type="relators:aut" conflicts with encoded value "relators:cre"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hubPath := test.hub
			if hubPath == "" {
				hubPath = "Extra.value"
			}
			compiled := compileDrupalEncodingProfile(t, []model.Field{test.field}, []profile.Mapping{{
				Field: test.selector, Hub: hubPath, Decode: test.codec, Encode: test.codec, Merge: profile.MergeFirstNonempty,
			}}, nil)
			record := &hubv1.Record{}
			if test.record != nil {
				record = test.record()
			} else {
				hub.SetExtra(record, "value", test.extraValue)
			}
			var output bytes.Buffer
			err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{SystemProfile: compiled})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Serialize() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestCompiledProfileEncoderParsesTypedRelationPrefixes(t *testing.T) {
	field := model.Field{
		Path: "field_linked_agent", SourceType: "typed_relation", Kind: model.ValueTypedReference, Cardinality: -1,
		Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: []string{"person", "corporate_body"}},
	}
	selector := profile.FieldSelector{EntityType: "node", Bundle: "article", Path: field.Path}
	compiled := compileDrupalEncodingProfile(t, []model.Field{field}, []profile.Mapping{{
		Field: selector, Hub: "Extra.agent", Decode: "typed-relation", Encode: "typed-relation", Merge: profile.MergeAppend,
	}}, nil)

	tests := []struct {
		name       string
		encoded    string
		wantTarget string
		wantRole   string
	}{
		{name: "namespaced person role", encoded: "relators:cre:person:Example, Avery", wantTarget: "Example, Avery", wantRole: "relators:cre"},
		{name: "namespaced corporate body role", encoded: "relators:pbl:corporate_body:Example University Press", wantTarget: "Example University Press", wantRole: "relators:pbl"},
		{name: "namespaced organization alias", encoded: "relators:pbl:organization:Example University Press", wantTarget: "Example University Press", wantRole: "relators:pbl"},
		{name: "unnamespaced role", encoded: "creator:person:Jane Doe", wantTarget: "Jane Doe", wantRole: "creator"},
		{name: "no role", encoded: "person:Jane Doe", wantTarget: "Jane Doe"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := &hubv1.Record{}
			hub.SetExtra(record, "agent", test.encoded)
			entity := serializeCompiledEntity(t, record, compiled)
			var values []map[string]any
			if err := json.Unmarshal(entity[field.Path], &values); err != nil {
				t.Fatal(err)
			}
			if len(values) != 1 || values[0]["target_id"] != test.wantTarget || values[0]["target_type"] != "taxonomy_term" {
				t.Fatalf("typed relation = %#v, want target %q", values, test.wantTarget)
			}
			if role, _ := values[0]["rel_type"].(string); role != test.wantRole {
				t.Errorf("rel_type = %q, want %q", role, test.wantRole)
			}
		})
	}
}

func TestCompiledProfileEncoderCanonicalizesSourceIdentifiers(t *testing.T) {
	field := model.Field{Path: "field_identifier", SourceType: "textfield_attr", Kind: model.ValueComposite, Cardinality: -1}
	selectors := map[string]profile.FieldSelector{}
	for _, scheme := range []string{"doi", "wos", "arxiv", "zenodo-record"} {
		selectors[scheme] = profile.FieldSelector{
			EntityType: "node", Bundle: "article", Path: field.Path, Attribute: "value",
			Where: &profile.FieldPredicate{Attribute: "attr0", Equals: scheme},
		}
	}
	mappings := make([]profile.Mapping, 0, len(selectors))
	rules := make([]profile.IdentifierRule, 0, len(selectors))
	for _, scheme := range []string{"doi", "wos", "arxiv", "zenodo-record"} {
		registryRule, exists := hub.DefaultIdentifierRegistry().Rule(scheme)
		if !exists {
			t.Fatalf("identifier scheme %q is unavailable", scheme)
		}
		level := profile.IdentityWork
		if scheme == "zenodo-record" {
			level = profile.IdentityVersion
		}
		selector := selectors[scheme]
		mappings = append(mappings, profile.Mapping{
			Field: selector, Hub: "Identifiers", Decode: "typed-identifier", Encode: "typed-identifier", Merge: profile.MergeAppend,
		})
		rules = append(rules, profile.IdentifierRule{
			Name: scheme, Scheme: scheme, IdentityLevel: level, Value: selector,
			Pattern: registryRule.Pattern, Canonicalizer: "canonical", Strength: profile.IdentifierStrong,
			Scope: profile.ScopeGlobal, Namespace: registryRule.NamespaceURI,
			Lookup: profile.LookupRule{Fields: []profile.FieldSelector{selector}, Operator: profile.LookupContains, Variants: []string{"canonical"}},
		})
	}
	doiVersionSelector := profile.FieldSelector{
		EntityType: "node", Bundle: "article", Path: field.Path, Attribute: "value",
		Where: &profile.FieldPredicate{Attribute: "attr0", Equals: "zenodo-doi"},
	}
	mappings = append(mappings, profile.Mapping{
		Field: doiVersionSelector, Hub: "Identifiers", Decode: "typed-identifier", Encode: "typed-identifier", Merge: profile.MergeAppend,
	})
	doiRegistryRule, _ := hub.DefaultIdentifierRegistry().Rule("doi")
	rules = append(rules, profile.IdentifierRule{
		Name: "doi-version", Scheme: "doi", IdentityLevel: profile.IdentityVersion, Value: doiVersionSelector,
		Pattern: doiRegistryRule.Pattern, Canonicalizer: "canonical", Strength: profile.IdentifierStrong,
		Scope: profile.ScopeGlobal, Namespace: doiRegistryRule.NamespaceURI,
		Lookup: profile.LookupRule{Fields: []profile.FieldSelector{doiVersionSelector}, Operator: profile.LookupContains, Variants: []string{"canonical"}},
	})
	compiled := compileDrupalEncodingProfile(t, []model.Field{field}, mappings, &profile.IdentityPolicy{
		Version: profile.CurrentIdentityVersion, Repository: profile.EntitySelector{EntityType: "node", Bundle: "article"}, Identifiers: rules,
	})
	record := &hubv1.Record{Identifiers: []*hubv1.Identifier{
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "HTTPS://DOI.ORG/10.1234/EXAMPLE."},
		{Scheme: "doi", Value: "https://doi.org/10.5281/ZENODO.8435696", IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION},
		{Scheme: "doi", Value: "10.5281/zenodo.8435695", IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT},
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_WOS, Value: "UT=WOS:000123456700001"},
		{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV, Value: "https://arxiv.org/pdf/2401.01234v2.pdf"},
		{Scheme: "zenodo-record", Value: "https://zenodo.org/records/1234567"},
		{Scheme: "zenodo-record", Value: "7654321", IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT},
	}}

	entity := serializeCompiledEntity(t, record, compiled)
	var values []map[string]any
	if err := json.Unmarshal(entity["field_identifier"], &values); err != nil {
		t.Fatal(err)
	}
	want := []struct{ scheme, value string }{
		{"doi", "10.1234/example"},
		{"wos", "WOS:000123456700001"},
		{"arxiv", "2401.01234"},
		{"zenodo-record", "1234567"},
		{"zenodo-doi", "10.5281/zenodo.8435696"},
	}
	if len(values) != len(want) {
		t.Fatalf("identifiers = %#v", values)
	}
	for index, expected := range want {
		if values[index]["attr0"] != expected.scheme || values[index]["value"] != expected.value {
			t.Fatalf("identifier %d = %#v, want %s=%s", index+1, values[index], expected.scheme, expected.value)
		}
	}
	serialized, err := json.Marshal(entity)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := (&Format{}).Parse(bytes.NewReader(serialized), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatal(err)
	}
	var zenodoVersion bool
	for _, identifier := range parsed[0].GetIdentifiers() {
		if identifier.GetScheme() == "doi" && identifier.GetValue() == "10.5281/zenodo.8435696" {
			zenodoVersion = identifier.GetIdentityLevel() == hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION
		}
		if identifier.GetIdentityLevel() == hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT {
			t.Fatalf("unexpected concept identifier after serialization: %#v", identifier)
		}
	}
	if !zenodoVersion {
		t.Fatalf("parsed identifiers do not retain the Zenodo DOI version level: %#v", parsed[0].GetIdentifiers())
	}
}

func TestDrupalStarterProfileSerializesVersionDOIThroughSharedSelector(t *testing.T) {
	field := model.Field{Path: "field_identifier", SourceType: "textfield_attr", Kind: model.ValueComposite, Cardinality: -1}
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "article",
			Fields: []model.Field{
				field,
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
			},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "starter-version-doi", EntityType: "node", Bundle: "article",
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}

	record := &hubv1.Record{Title: "A Zenodo dataset", Identifiers: []*hubv1.Identifier{
		{Scheme: "doi", Value: "10.5281/zenodo.8435696", IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION},
		{Scheme: "doi", Value: "10.5281/zenodo.8435695", IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT},
	}}
	entity := serializeCompiledEntity(t, record, compiled)
	var values []map[string]any
	if err := json.Unmarshal(entity[field.Path], &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0]["attr0"] != "doi" || values[0]["value"] != "10.5281/zenodo.8435696" {
		t.Fatalf("serialized DOI values = %#v", values)
	}
}

func TestCompiledProfileEncoderRejectsInvalidProfileIdentifier(t *testing.T) {
	field := model.Field{Path: "field_doi", SourceType: "string", Kind: model.ValueText, Cardinality: 1}
	selector := profile.FieldSelector{EntityType: "node", Bundle: "article", Path: field.Path}
	registryRule, _ := hub.DefaultIdentifierRegistry().Rule("doi")
	compiled := compileDrupalEncodingProfile(t, []model.Field{field}, []profile.Mapping{{
		Field: selector, Hub: "Identifiers", Decode: "typed-identifier", Encode: "typed-identifier", Merge: profile.MergeAppend,
	}}, &profile.IdentityPolicy{
		Version: profile.CurrentIdentityVersion, Repository: profile.EntitySelector{EntityType: "node", Bundle: "article"},
		Identifiers: []profile.IdentifierRule{{
			Name: "doi", Scheme: "doi", IdentityLevel: profile.IdentityWork, Value: selector,
			Pattern: registryRule.Pattern, Canonicalizer: "canonical", Strength: profile.IdentifierStrong,
			Scope: profile.ScopeGlobal, Namespace: registryRule.NamespaceURI,
			Lookup: profile.LookupRule{Fields: []profile.FieldSelector{selector}, Operator: profile.LookupExact, Variants: []string{"canonical"}},
		}},
	})

	var output bytes.Buffer
	err := (&Format{}).Serialize(&output, []*hubv1.Record{{Identifiers: []*hubv1.Identifier{{Scheme: "doi", Value: "not-a-doi"}}}}, &format.SerializeOptions{SystemProfile: compiled})
	if err == nil || !strings.Contains(err.Error(), `canonicalizing "not-a-doi" as doi`) {
		t.Fatalf("Serialize() error = %v", err)
	}
}

func TestCompiledProfileDrupalRoundTripUsesProfileSemantics(t *testing.T) {
	fields := []model.Field{
		{Path: "field_full_title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
		{Path: "field_alt_title", SourceType: "string", Kind: model.ValueText, Cardinality: -1},
		{Path: "field_edtf_date_issued", SourceType: "edtf", Kind: model.ValueDate, Cardinality: 1},
		{Path: "field_linked_agent", SourceType: "typed_relation", Kind: model.ValueTypedReference, Cardinality: -1, Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: []string{"person"}}},
		{Path: "field_keywords", SourceType: "entity_reference", Kind: model.ValueReference, Cardinality: -1, Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: []string{"tags"}}},
		{Path: "field_member_of", SourceType: "entity_reference", Kind: model.ValueReference, Cardinality: -1, Reference: &model.Reference{EntityType: "node", Bundles: []string{"collection"}}},
	}
	selector := func(path string) profile.FieldSelector {
		return profile.FieldSelector{EntityType: "node", Bundle: "article", Path: path}
	}
	compiled := compileDrupalEncodingProfile(t, fields, []profile.Mapping{
		{Field: selector("field_full_title"), Hub: "FullTitle", Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty},
		{Field: selector("field_alt_title"), Hub: "AltTitle", Decode: "text", Encode: "text", Merge: profile.MergeAppend},
		{Field: selector("field_edtf_date_issued"), Hub: "Dates.issued", Decode: "date", Encode: "date", Merge: profile.MergeAppend},
		{Field: selector("field_linked_agent"), Hub: "Contributors", Decode: "typed-relation", Encode: "typed-relation", Merge: profile.MergeAppend},
		{Field: selector("field_keywords"), Hub: "Subjects.keywords", Decode: "reference", Encode: "reference", Merge: profile.MergeAppend},
		{Field: selector("field_member_of"), Hub: "Relations.member_of", Decode: "reference", Encode: "reference", Merge: profile.MergeAppend},
	}, nil)
	record := &hubv1.Record{
		FullTitle: "A complete title", AltTitle: []string{"Alternate one", "Alternate two"},
		Dates: []*hubv1.DateValue{
			{Type: hubv1.DateType_DATE_TYPE_ISSUED, Raw: "2024", Year: 2024, Precision: hubv1.DatePrecision_DATE_PRECISION_YEAR},
			{Type: hubv1.DateType_DATE_TYPE_CREATED, Raw: "2020", Year: 2020, Precision: hubv1.DatePrecision_DATE_PRECISION_YEAR},
		},
		Contributors: []*hubv1.Contributor{{SourceId: "91", RoleCode: "relators:aut", Name: "Doe, Jane"}},
		Subjects: []*hubv1.Subject{
			{SourceId: "7", Value: "Metadata", Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS},
			{SourceId: "8", Value: "Libraries", Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH},
		},
		Relations: []*hubv1.Relation{
			{SourceId: "42", Type: hubv1.RelationType_RELATION_TYPE_MEMBER_OF},
			{SourceId: "43", Type: hubv1.RelationType_RELATION_TYPE_CITES},
		},
	}

	var serialized bytes.Buffer
	if err := (&Format{}).Serialize(&serialized, []*hubv1.Record{record}, &format.SerializeOptions{SystemProfile: compiled}); err != nil {
		t.Fatal(err)
	}
	parsed, err := (&Format{}).Parse(bytes.NewReader(serialized.Bytes()), &format.ParseOptions{SystemProfile: compiled})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 {
		t.Fatalf("parsed records = %d", len(parsed))
	}
	got := parsed[0]
	if got.GetFullTitle() != record.GetFullTitle() || strings.Join(got.GetAltTitle(), "|") != "Alternate one|Alternate two" {
		t.Fatalf("titles = full %q alternate %#v", got.GetFullTitle(), got.GetAltTitle())
	}
	if len(got.GetDates()) != 1 || got.GetDates()[0].GetType() != hubv1.DateType_DATE_TYPE_ISSUED || hub.FormatEDTF(got.GetDates()[0]) != "2024" {
		t.Fatalf("dates = %#v", got.GetDates())
	}
	if len(got.GetContributors()) != 1 || got.GetContributors()[0].GetSourceId() != "91" || got.GetContributors()[0].GetRoleCode() != "relators:aut" {
		t.Fatalf("contributors = %#v", got.GetContributors())
	}
	if len(got.GetSubjects()) != 1 || got.GetSubjects()[0].GetSourceId() != "7" || got.GetSubjects()[0].GetVocabulary() != hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS {
		t.Fatalf("subjects = %#v", got.GetSubjects())
	}
	if len(got.GetRelations()) != 1 || got.GetRelations()[0].GetSourceId() != "42" || got.GetRelations()[0].GetType() != hubv1.RelationType_RELATION_TYPE_MEMBER_OF {
		t.Fatalf("relations = %#v", got.GetRelations())
	}
}

func compileDrupalEncodingProfile(t *testing.T, fields []model.Field, mappings []profile.Mapping, identity *profile.IdentityPolicy) *profile.Compiled {
	t.Helper()
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{EntityType: "node", Bundle: "article", Fields: fields}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition := &profile.Definition{
		Version: profile.CurrentDefinitionVersion, Name: "encoder-test", System: "drupal",
		ModelFingerprint: snapshot.Fingerprint.Value, Mappings: mappings, Identity: identity,
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func serializeCompiledEntity(t *testing.T, record *hubv1.Record, compiled *profile.Compiled) map[string]json.RawMessage {
	t.Helper()
	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, &format.SerializeOptions{SystemProfile: compiled}); err != nil {
		t.Fatal(err)
	}
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &entity); err != nil {
		t.Fatal(err)
	}
	return entity
}

func drupalFieldTexts(t *testing.T, entity map[string]json.RawMessage, field string) []string {
	t.Helper()
	var values []map[string]any
	if err := json.Unmarshal(entity[field], &values); err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value["value"].(string))
	}
	return result
}

func assertDrupalObjectValue(t *testing.T, entity map[string]json.RawMessage, field, attribute string, want any) {
	t.Helper()
	var values []map[string]any
	if err := json.Unmarshal(entity[field], &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0][attribute] != want {
		t.Fatalf("%s = %#v, want %s=%#v", field, values, attribute, want)
	}
}
