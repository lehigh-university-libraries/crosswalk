package archivesspace

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

func TestParseRejectsSystemProfileUntilArchivesSpaceProfilesAreExecutable(t *testing.T) {
	t.Parallel()
	_, err := (&Format{}).ParseDataset(bytes.NewReader(readFixture(t, "resource.json")), &format.ParseOptions{SystemProfile: &profile.Compiled{}})
	if err == nil || !strings.Contains(err.Error(), "profiles are not executable") {
		t.Fatalf("ParseDataset() profile error = %v", err)
	}
}

func TestParseSingleResource(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "resource.json")
	parser := &Format{}
	records, err := parser.Parse(bytes.NewReader(raw), &format.ParseOptions{
		BaseURL:    "https://aspace.example.edu/",
		SourceName: "example-archives",
		StripHTML:  true,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("record count = %d, want 1", len(records))
	}
	record := records[0]
	assertHubValid(t, record)
	if record.Title != "The Example Family Papers" {
		t.Errorf("title = %q", record.Title)
	}
	if record.GetResourceType().GetType() != hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION {
		t.Errorf("resource type = %v", record.GetResourceType().GetType())
	}
	if record.GetSourceInfo().GetSourceUri() != "https://aspace.example.edu/repositories/2/resources/1" {
		t.Errorf("source URI = %q", record.GetSourceInfo().GetSourceUri())
	}
	if record.GetSourceInfo().GetOrigin() != "example-archives" {
		t.Errorf("origin = %q", record.GetSourceInfo().GetOrigin())
	}
	if record.Description != "Correspondence and research files." {
		t.Errorf("description = %q", record.Description)
	}
	if record.PreferredCitation == "" {
		t.Error("preferred citation was not mapped")
	}
	if record.PhysicalDesc != "12 linear_feet; 24 boxes" || record.Dimensions != "30 cm" {
		t.Errorf("physical description = %q, dimensions = %q", record.PhysicalDesc, record.Dimensions)
	}
	if len(record.Contributors) != 1 || record.Contributors[0].Name != "Example, Ada, 1870-1955" {
		t.Fatalf("contributors = %#v", record.Contributors)
	}
	if record.Contributors[0].GetAuthorityUri() != "https://id.loc.gov/authorities/names/n12345678" {
		t.Errorf("agent authority URI = %q", record.Contributors[0].GetAuthorityUri())
	}
	if len(record.Subjects) != 1 || record.Subjects[0].GetVocabulary() != hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH {
		t.Fatalf("subjects = %#v", record.Subjects)
	}
	if len(record.Dates) != 1 || !record.Dates[0].GetIsRange() || record.Dates[0].GetEndYear() != 1950 {
		t.Fatalf("dates = %#v", record.Dates)
	}
	if got := identifierValue(record, "archivesspace-resource-id"); got != "SC-001" {
		t.Errorf("resource identifier = %q", got)
	}
	if got := identifierNamespace(record, "archivesspace-external-id"); got != "" {
		t.Errorf("unexpected record external identifier namespace = %q", got)
	}
	if record.GetArchivalLocation() != nil {
		t.Errorf("flat resource acquired an artificial hierarchy location: %#v", record.GetArchivalLocation())
	}
}

func TestParseDatasetPreservesDeepOrderedHierarchy(t *testing.T) {
	t.Parallel()

	dataset, err := (&Format{}).ParseDataset(bytes.NewReader(readFixture(t, "snapshot.json")), &format.ParseOptions{
		BaseURL:    "https://aspace.example.edu/",
		SourceName: "example-archives",
		StripHTML:  true,
	})
	if err != nil {
		t.Fatalf("ParseDataset() error = %v", err)
	}
	if len(dataset.Records) != 5 || len(dataset.Hierarchy.Nodes) != 5 {
		t.Fatalf("dataset sizes = %d records, %d nodes", len(dataset.Records), len(dataset.Hierarchy.Nodes))
	}
	wantKeys := []string{
		"/repositories/2/resources/1",
		"/repositories/2/archival_objects/10",
		"/repositories/2/archival_objects/11",
		"/repositories/2/archival_objects/12",
		"/repositories/2/archival_objects/20",
	}
	for index, want := range wantKeys {
		if dataset.Records[index].Key != want || dataset.Hierarchy.Nodes[index].RecordKey != want {
			t.Errorf("entry %d key = %q / %q, want %q", index, dataset.Records[index].Key, dataset.Hierarchy.Nodes[index].RecordKey, want)
		}
	}
	itemNode := dataset.Hierarchy.Nodes[3]
	if itemNode.ParentKey != "/repositories/2/archival_objects/11" || itemNode.Position != 0 {
		t.Errorf("item node = %#v", itemNode)
	}
	secondSeries := dataset.Hierarchy.Nodes[4]
	if secondSeries.ParentKey != "/repositories/2/resources/1" || secondSeries.Position != 1 {
		t.Errorf("second series node = %#v", secondSeries)
	}
	if dataset.Provenance.SourceURI != "https://aspace.example.edu/staff" {
		t.Errorf("dataset source URI = %q", dataset.Provenance.SourceURI)
	}
	if dataset.Provenance.RetrievedAt == nil || dataset.Provenance.SourceID != "repository-2-resource-1" {
		t.Errorf("dataset provenance = %#v", dataset.Provenance)
	}

	item := dataset.Records[3].Record
	for _, entry := range dataset.Records {
		assertHubValid(t, entry.Record)
	}
	if location := item.GetArchivalLocation(); location.GetCollection() != "The Example Family Papers" || location.GetSeries() != "Professional correspondence" || location.GetBox() != "3" || location.GetFolder() != "7" {
		t.Errorf("archival location = %#v", location)
	}
	if len(item.Files) != 1 || item.Files[0].GetUri() != "https://media.example.edu/blueprint.pdf" || item.Files[0].GetChecksum() != "abc123" {
		t.Errorf("files = %#v", item.Files)
	}
	if len(item.Rights) != 1 || item.Rights[0].GetStatement() != "copyright; copyrighted; Research use permitted" {
		t.Errorf("rights = %#v", item.Rights)
	}
	if len(item.Relations) < 3 || item.Relations[0].GetTargetTitle() != "Project files" {
		t.Errorf("relations = %#v", item.Relations)
	}
}

func TestParseDatasetRejectsBrokenHierarchy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{
			name: "cycle",
			mutate: func(snapshot map[string]any) {
				record(snapshot, 1)["parent"] = map[string]any{"ref": "/repositories/2/archival_objects/12"}
			},
			wantErr: "hierarchy cycle",
		},
		{
			name: "missing parent",
			mutate: func(snapshot map[string]any) {
				record(snapshot, 2)["parent"] = map[string]any{"ref": "/repositories/2/archival_objects/999"}
			},
			wantErr: "missing parent",
		},
		{
			name: "child before parent",
			mutate: func(snapshot map[string]any) {
				ordered := orderedEntries(snapshot)
				ordered[2], ordered[3] = ordered[3], ordered[2]
			},
			wantErr: "appears before hierarchy parent",
		},
		{
			name: "duplicate URI",
			mutate: func(snapshot map[string]any) {
				record(snapshot, 4)["uri"] = "/repositories/2/archival_objects/10"
			},
			wantErr: "duplicate record URI",
		},
		{
			name: "depth mismatch",
			mutate: func(snapshot map[string]any) {
				orderedEntries(snapshot)[3].(map[string]any)["depth"] = float64(2)
			},
			wantErr: "expected 3",
		},
		{
			name: "position mismatch",
			mutate: func(snapshot map[string]any) {
				record(snapshot, 4)["position"] = float64(0)
			},
			wantErr: "position 0; expected 1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var snapshot map[string]any
			if err := json.Unmarshal(readFixture(t, "snapshot.json"), &snapshot); err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			test.mutate(snapshot)
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatalf("marshal mutation: %v", err)
			}
			_, err = (&Format{}).ParseDataset(bytes.NewReader(raw), nil)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("ParseDataset() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestParseOfficialListSearchAndResolvedOrderForms(t *testing.T) {
	t.Parallel()

	resource := readFixture(t, "resource.json")
	var resourceValue map[string]any
	if err := json.Unmarshal(resource, &resourceValue); err != nil {
		t.Fatalf("decode resource: %v", err)
	}
	archivalObject := map[string]any{
		"jsonmodel_type": "archival_object",
		"uri":            "/repositories/2/archival_objects/1",
		"title":          "One folder",
		"level":          "file",
		"position":       float64(0),
		"resource":       map[string]any{"ref": "/repositories/2/resources/1"},
	}

	tests := []struct {
		name      string
		input     any
		wantCount int
	}{
		{name: "list endpoint", input: []any{resourceValue, archivalObject}, wantCount: 2},
		{name: "search endpoint", input: map[string]any{"results": []any{
			map[string]any{"primary_type": "resource", "json": string(resource)},
		}}, wantCount: 1},
		{name: "resolved ordered records", input: map[string]any{
			"jsonmodel_type": "resource_ordered_records",
			"uris": []any{
				map[string]any{"ref": "/repositories/2/resources/1", "depth": float64(0), "_resolved": resourceValue},
				map[string]any{"ref": "/repositories/2/archival_objects/1", "depth": float64(1), "_resolved": archivalObject},
			},
		}, wantCount: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(test.input)
			if err != nil {
				t.Fatalf("marshal input: %v", err)
			}
			dataset, err := (&Format{}).ParseDataset(bytes.NewReader(raw), nil)
			if err != nil {
				t.Fatalf("ParseDataset() error = %v", err)
			}
			if len(dataset.Records) != test.wantCount {
				t.Errorf("record count = %d, want %d", len(dataset.Records), test.wantCount)
			}
		})
	}
}

func TestParseRejectsMalformedAndOversizedInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
	}{
		{name: "empty", input: nil},
		{name: "trailing value", input: []byte(`{"jsonmodel_type":"resource","title":"x"} {}`)},
		{name: "unsupported model", input: []byte(`{"jsonmodel_type":"accession","title":"x"}`)},
		{name: "snapshot unknown field", input: []byte(`{"crosswalk_format":"archivesspace-jsonmodel","version":1,"records":[],"ordered_records":{},"unexpected":true}`)},
		{name: "oversized", input: bytes.Repeat([]byte(" "), int(maxInputBytes)+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := (&Format{}).ParseDataset(bytes.NewReader(test.input), nil); err == nil {
				t.Fatal("ParseDataset() error = nil")
			}
		})
	}
}

func TestParseStructuredNoteItems(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"jsonmodel_type":"archival_object",
		"uri":"/repositories/2/archival_objects/50",
		"title":"Indexed names",
		"level":"file",
		"resource":{"ref":"/repositories/2/resources/1"},
		"notes":[{
			"jsonmodel_type":"note_index",
			"type":"index",
			"content":["Name index"],
			"items":[{"value":"Example, Ada","type":"person","reference_text":"Box 3"}]
		}]
	}`)
	records, err := (&Format{}).Parse(bytes.NewReader(input), &format.ParseOptions{StripHTML: true})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 || len(records[0].Notes) != 1 || !strings.Contains(records[0].Notes[0], "Example, Ada") {
		t.Fatalf("notes = %#v", records[0].Notes)
	}
}

func TestParseRejectsDuplicateRecordsWithoutURIs(t *testing.T) {
	t.Parallel()

	input := []byte(`[
		{"jsonmodel_type":"resource","title":"Duplicate","id_0":"SC-1"},
		{"jsonmodel_type":"resource","title":"Duplicate","id_0":"SC-1"}
	]`)
	_, err := (&Format{}).ParseDataset(bytes.NewReader(input), nil)
	if err == nil || !strings.Contains(err.Error(), "duplicate record key") {
		t.Fatalf("ParseDataset() error = %v", err)
	}
}

func TestDatasetBounds(t *testing.T) {
	t.Parallel()

	tooMany := make([]json.RawMessage, maxRecordCount+1)
	if _, err := decodeRawRecords(tooMany, false); err == nil || !strings.Contains(err.Error(), "record count") {
		t.Fatalf("decodeRawRecords() error = %v", err)
	}

	records := make([]decodedRecord, maxHierarchyDepth+2)
	ordered := make([]OrderedRecord, len(records))
	recordsByURI := make(map[string]int, len(records))
	rootURI := "/repositories/2/resources/1"
	records[0].model = jsonModel{JSONModelType: "resource", URI: rootURI, Title: "Root", Level: "collection"}
	depth := 0
	ordered[0] = OrderedRecord{Ref: rootURI, Depth: &depth, Level: "collection"}
	recordsByURI[rootURI] = 0
	parent := rootURI
	for index := 1; index < len(records); index++ {
		uri := "/repositories/2/archival_objects/" + strconv.Itoa(index)
		position := 0
		records[index].model = jsonModel{
			JSONModelType: "archival_object", URI: uri, Title: "Level " + strconv.Itoa(index),
			Level: "file", Position: &position, Parent: reference{Ref: parent}, Resource: reference{Ref: rootURI},
		}
		entryDepth := index
		ordered[index] = OrderedRecord{Ref: uri, Depth: &entryDepth, Level: "file"}
		recordsByURI[uri] = index
		parent = uri
	}
	if _, _, _, err := orderHierarchy(records, recordsByURI, ordered); err == nil || !strings.Contains(err.Error(), "maximum hierarchy depth") {
		t.Fatalf("orderHierarchy() error = %v", err)
	}
}

func TestStrictModeAcceptsCurrentJSONModelFieldsAndPreservesRaw(t *testing.T) {
	t.Parallel()

	records, err := (&Format{}).Parse(bytes.NewReader(readFixture(t, "resource-current-fields.json")), &format.ParseOptions{
		Strict:    true,
		StripHTML: true,
	})
	if err != nil {
		t.Fatalf("strict Parse() rejected current JSONModel fields: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("record count = %d, want 1", len(records))
	}
	extra := records[0].GetExtra().AsMap()
	findingAid := requireExtraObject(t, extra, "archivesspace_finding_aid")
	if got, want := findingAid["script"], "Latn"; got != want {
		t.Errorf("finding aid script = %#v, want %q", got, want)
	}

	rights, ok := extra["archivesspace_rights"].([]any)
	if !ok || len(rights) != 1 {
		t.Fatalf("archivesspace_rights = %#v", extra["archivesspace_rights"])
	}
	rightsEntry, ok := rights[0].(map[string]any)
	if !ok {
		t.Fatalf("rights entry = %#v", rights[0])
	}
	acts, ok := rightsEntry["acts"].([]any)
	if !ok || len(acts) != 1 {
		t.Fatalf("rights acts = %#v", rightsEntry["acts"])
	}
	act, ok := acts[0].(map[string]any)
	if !ok || act["act_type"] != "use" || act["restriction"] != "allow" {
		t.Errorf("rights act = %#v", acts[0])
	}
	if actsJSON, ok := rightsEntry["acts_json"].(string); !ok || !strings.Contains(actsJSON, `"end_date":"2049-12-31"`) {
		t.Errorf("rights acts canonical JSON = %#v", rightsEntry["acts_json"])
	}

	rawFields := requireExtraObject(t, extra, "archivesspace_raw_fields")
	for _, name := range []string{
		"revision_statements",
		"metadata_rights_declarations",
		"user_defined",
		"collection_management",
		"local_plugin_data",
	} {
		if _, ok := rawFields[name].(string); !ok {
			t.Errorf("raw field %q = %#v, want canonical JSON string", name, rawFields[name])
		}
	}
	if _, duplicated := rawFields["finding_aid_script"]; duplicated {
		t.Error("mapped finding_aid_script was duplicated as a raw field")
	}
	if got, want := rawFields["local_plugin_data"], `{"review_state":"approved","sequence":9007199254740995}`; got != want {
		t.Errorf("canonical local_plugin_data = %#v, want %s", got, want)
	}
	if got, want := rawFields["user_defined"], `{"boolean_1":true,"integer_1":9007199254740993,"jsonmodel_type":"user_defined","real_1":1.50,"string_1":"locally reviewed"}`; got != want {
		t.Errorf("canonical user_defined = %#v, want %s", got, want)
	}
}

func TestStrictModeAcceptsUnknownJSONModelFields(t *testing.T) {
	t.Parallel()

	input := []byte(`{"jsonmodel_type":"resource","title":"Future record","future_plugin_field":true}`)
	records, err := (&Format{}).Parse(bytes.NewReader(input), &format.ParseOptions{Strict: true})
	if err != nil {
		t.Fatalf("strict Parse() rejected forward-compatible field: %v", err)
	}
	rawFields := requireExtraObject(t, records[0].GetExtra().AsMap(), "archivesspace_raw_fields")
	if got, want := rawFields["future_plugin_field"], "true"; got != want {
		t.Errorf("future_plugin_field = %#v, want %q", got, want)
	}
}

func TestStrictModeRejectsMalformedOrUnsupportedJSONModelShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "unsupported record type",
			input:   `{"jsonmodel_type":"accession","title":"Accession"}`,
			wantErr: `unsupported JSONModel type "accession"`,
		},
		{
			name:    "revision statements object",
			input:   `{"jsonmodel_type":"resource","title":"Collection","revision_statements":{}}`,
			wantErr: `field "revision_statements": must be an array of objects`,
		},
		{
			name:    "metadata rights scalar entry",
			input:   `{"jsonmodel_type":"resource","title":"Collection","metadata_rights_declarations":["cc0"]}`,
			wantErr: `field "metadata_rights_declarations": must be an array of objects`,
		},
		{
			name:    "user defined array",
			input:   `{"jsonmodel_type":"resource","title":"Collection","user_defined":[]}`,
			wantErr: `field "user_defined": must be an object or null`,
		},
		{
			name:    "collection management scalar",
			input:   `{"jsonmodel_type":"resource","title":"Collection","collection_management":"complete"}`,
			wantErr: `field "collection_management": must be an object or null`,
		},
		{
			name:    "rights acts object",
			input:   `{"jsonmodel_type":"resource","title":"Collection","rights_statements":[{"acts":{}}]}`,
			wantErr: `acts must be an array of objects`,
		},
		{
			name:    "rights act malformed field",
			input:   `{"jsonmodel_type":"resource","title":"Collection","rights_statements":[{"acts":[{"jsonmodel_type":"rights_statement_act","act_type":7}]}]}`,
			wantErr: `act 1 is malformed`,
		},
		{
			name:    "rights act unsupported type",
			input:   `{"jsonmodel_type":"resource","title":"Collection","rights_statements":[{"acts":[{"jsonmodel_type":"rights_statement"}]}]}`,
			wantErr: `unsupported JSONModel type "rights_statement"`,
		},
		{
			name:    "resource-only field on archival object",
			input:   `{"jsonmodel_type":"archival_object","title":"Folder","finding_aid_script":"Latn"}`,
			wantErr: `field "finding_aid_script" is not supported`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := (&Format{}).Parse(bytes.NewReader([]byte(test.input)), &format.ParseOptions{Strict: true})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("strict Parse() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestSafeSourceURIRedactsCredentialsAndFragment(t *testing.T) {
	t.Parallel()

	got, err := safeSourceURI("https://user:password@EXAMPLE.EDU/record?page=2&access_token=secret&X-Amz-Signature=secret#section", "")
	if err != nil {
		t.Fatalf("safeSourceURI() error = %v", err)
	}
	if got != "https://example.edu/record?page=2" {
		t.Errorf("safeSourceURI() = %q", got)
	}
	if _, err := safeSourceURI("file:///tmp/record.json", ""); err == nil {
		t.Fatal("safeSourceURI() accepted an unsafe scheme")
	}
	if got, want := safeSourceOrigin("https://user:password@EXAMPLE.EDU/staff?page=2&session=secret#view"), "https://example.edu/staff?page=2"; got != want {
		t.Errorf("safeSourceOrigin() = %q, want %q", got, want)
	}
	if got := safeSourceOrigin("file:///tmp/record.json"); got != "archivesspace" {
		t.Errorf("safeSourceOrigin() = %q for unsafe scheme", got)
	}
}

func TestExternalRecordIDUsesStableScopedScheme(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"jsonmodel_type":"resource",
		"uri":"/repositories/2/resources/4",
		"title":"Imported collection",
		"external_ids":[{"source":"legacy_migration","external_id":"ABC 123"}]
	}`)
	records, err := (&Format{}).Parse(bytes.NewReader(input), &format.ParseOptions{BaseURL: "https://aspace.example.edu/"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := identifierValue(records[0], "archivesspace-external-id"); got != "ABC 123" {
		t.Errorf("external identifier = %q", got)
	}
	if got := identifierNamespace(records[0], "archivesspace-external-id"); got != "https://aspace.example.edu/identifier-authorities/legacy-migration/" {
		t.Errorf("external identifier namespace = %q", got)
	}
}

func TestAgentExternalIdentifiersUseCanonicalOrStableSchemes(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"jsonmodel_type":"resource",
		"uri":"/repositories/2/resources/8",
		"title":"Agent identifiers",
		"linked_agents":[{
			"role":"creator",
			"ref":"/agents/people/8",
			"_resolved":{
				"jsonmodel_type":"agent_person",
				"uri":"/agents/people/8",
				"display_name":{"primary_name":"Example","rest_of_name":"Sam","name_order":"inverted"},
				"external_ids":[
					{"source":"naf","external_id":"n123"},
					{"source":"institutional_authority","external_id":"person 88"},
					{"source":"profile","external_id":"https://people.example.edu/sam"}
				]
			}
		}]
	}`)
	records, err := (&Format{}).Parse(bytes.NewReader(input), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	identifiers := records[0].Contributors[0].Identifiers
	want := map[string]string{
		"archivesspace-agent-uri":         "/agents/people/8",
		"lcnaf":                           "n123",
		"archivesspace-agent-external-id": "person 88",
		"url":                             "https://people.example.edu/sam",
	}
	for _, identifier := range identifiers {
		delete(want, identifier.GetScheme())
		if identifier.GetScheme() == "archivesspace-agent-external-id" && identifier.GetNamespaceUri() != "https://www.archivesspace.org/agent-identifier-authorities/institutional-authority/" {
			t.Errorf("custom agent namespace = %q", identifier.GetNamespaceUri())
		}
	}
	if len(want) != 0 {
		t.Errorf("missing identifier schemes: %#v; identifiers = %#v", want, identifiers)
	}
}

func TestCanParse(t *testing.T) {
	t.Parallel()

	parser := &Format{}
	tests := []struct {
		name  string
		input []byte
		want  bool
	}{
		{name: "resource fixture", input: readFixture(t, "resource.json"), want: true},
		{name: "snapshot fixture", input: readFixture(t, "snapshot.json"), want: true},
		{name: "record array", input: []byte(`[{"jsonmodel_type":"archival_object","title":"Folder"}]`), want: true},
		{name: "arbitrary whitespace", input: []byte("{\n\t\"jsonmodel_type\"\n:\t\"resource\",\n\"title\":\"Collection\"\n}"), want: true},
		{name: "ordered records", input: []byte(`{"jsonmodel_type":"resource_ordered_records","uris":[]}`), want: true},
		{name: "search results", input: []byte(`{"results":[{"primary_type":"resource","json":"{\"jsonmodel_type\":\"resource\",\"title\":\"Collection\"}"}]}`), want: true},
		{name: "generic JSON", input: []byte(`{"title":"generic JSON"}`), want: false},
		{name: "marker in string", input: []byte(`{"description":"plugin says \"jsonmodel_type\":\"resource\""}`), want: false},
		{name: "nested marker", input: []byte(`{"payload":{"jsonmodel_type":"resource"}}`), want: false},
		{name: "near-match type", input: []byte(`{"jsonmodel_type":"resourceful"}`), want: false},
		{name: "unsupported type", input: []byte(`{"jsonmodel_type":"accession"}`), want: false},
		{name: "empty array", input: []byte(`[]`), want: false},
		{name: "mixed array", input: []byte(`[{"jsonmodel_type":"resource"},{"title":"generic"}]`), want: false},
		{name: "malformed JSON", input: []byte(`{"jsonmodel_type":"resource"`), want: false},
		{name: "search marker only", input: []byte(`{"results":[],"primary_type":"resource","json":"resource"}`), want: false},
		{name: "search type mismatch", input: []byte(`{"results":[{"primary_type":"resource","json":"{\"jsonmodel_type\":\"archival_object\"}"}]}`), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := parser.CanParse(test.input); got != test.want {
				t.Errorf("CanParse() = %t, want %t for %s", got, test.want, test.input[:min(len(test.input), 120)])
			}
		})
	}
}

func TestEncodeSnapshotIsDeterministicAndValidated(t *testing.T) {
	t.Parallel()

	var snapshot Snapshot
	if err := json.Unmarshal(readFixture(t, "snapshot.json"), &snapshot); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	var first bytes.Buffer
	if err := EncodeSnapshot(&first, &snapshot); err != nil {
		t.Fatalf("EncodeSnapshot() error = %v", err)
	}
	var second bytes.Buffer
	if err := EncodeSnapshot(&second, &snapshot); err != nil {
		t.Fatalf("EncodeSnapshot() second error = %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Error("EncodeSnapshot() output is not deterministic")
	}
	if bytes.Contains(first.Bytes(), []byte("session=")) || bytes.Contains(first.Bytes(), []byte("#fragment")) {
		t.Error("EncodeSnapshot() retained credentials or a fragment in source_uri")
	}
	if first.Bytes()[len(first.Bytes())-1] != '\n' {
		t.Error("EncodeSnapshot() output is not newline terminated")
	}
	if _, err := (&Format{}).ParseDataset(bytes.NewReader(first.Bytes()), nil); err != nil {
		t.Fatalf("parse encoded snapshot: %v", err)
	}

	snapshot.OrderedRecords.URIs = snapshot.OrderedRecords.URIs[:len(snapshot.OrderedRecords.URIs)-1]
	if err := EncodeSnapshot(&bytes.Buffer{}, &snapshot); err == nil || !strings.Contains(err.Error(), "count") {
		t.Fatalf("EncodeSnapshot() invalid hierarchy error = %v", err)
	}
}

func identifierValue(record *hubv1.Record, scheme string) string {
	for _, identifier := range record.GetIdentifiers() {
		if identifier.GetScheme() == scheme {
			return identifier.GetValue()
		}
	}
	return ""
}

func identifierNamespace(record *hubv1.Record, scheme string) string {
	for _, identifier := range record.GetIdentifiers() {
		if identifier.GetScheme() == scheme {
			return identifier.GetNamespaceUri()
		}
	}
	return ""
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return raw
}

func assertHubValid(t *testing.T, record *hubv1.Record) {
	t.Helper()
	options := hub.DefaultValidationOptions()
	if result := hub.Validate(record, options); !result.IsValid() {
		t.Fatalf("Hub record is invalid: %v", result.Error())
	}
}

func requireExtraObject(t *testing.T, extra map[string]any, name string) map[string]any {
	t.Helper()
	value, ok := extra[name].(map[string]any)
	if !ok {
		t.Fatalf("extra %q = %#v, want object", name, extra[name])
	}
	return value
}

func record(snapshot map[string]any, index int) map[string]any {
	return snapshot["records"].([]any)[index].(map[string]any)
}

func orderedEntries(snapshot map[string]any) []any {
	return snapshot["ordered_records"].(map[string]any)["uris"].([]any)
}
