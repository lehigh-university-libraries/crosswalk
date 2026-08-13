package drupal

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

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
