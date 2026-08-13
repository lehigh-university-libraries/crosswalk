package islandora_workbench

import (
	"bytes"
	"encoding/csv"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/spec"
	"google.golang.org/protobuf/proto"
)

func TestSerializeRequiresSealedTransformationSpecification(t *testing.T) {
	unsigned := minimalWorkbenchTransformation(t)
	unsigned.Fingerprint = spec.Fingerprint{}
	compiled := compileManifestTargetProfile(t)

	tests := []struct {
		name    string
		opts    *format.SerializeOptions
		wantErr string
	}{
		{name: "nil options", wantErr: "requires a transformation specification"},
		{name: "nil specification", opts: &format.SerializeOptions{}, wantErr: "requires a transformation specification"},
		{
			name:    "profile with nil specification",
			opts:    &format.SerializeOptions{SystemProfile: compiled},
			wantErr: "requires a transformation specification",
		},
		{
			name:    "unsealed specification",
			opts:    &format.SerializeOptions{Spec: unsigned},
			wantErr: "unsealed transformation specification",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := (&Format{}).Serialize(io.Discard, nil, test.opts)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Serialize() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestSerializeEnforcesExactTargetProfileBindingAtEntry(t *testing.T) {
	drupalProfile := compileManifestTargetProfile(t)
	bound := minimalWorkbenchTransformation(t)
	bound.Fingerprint.Model = drupalProfile.ModelFingerprint()
	bound.Fingerprint.Profile = drupalProfile.Fingerprint()
	if err := bound.SealFingerprint(); err != nil {
		t.Fatal(err)
	}

	unbound := minimalWorkbenchTransformation(t)
	wrongProfile := minimalWorkbenchTransformation(t)
	wrongProfile.Fingerprint.Model = drupalProfile.ModelFingerprint()
	wrongProfile.Fingerprint.Profile = strings.Repeat("a", 64)
	if err := wrongProfile.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	omekaProfile := compileManifestProfileForSystem(t, "omeka-s")
	omekaBound := minimalWorkbenchTransformation(t)
	omekaBound.Fingerprint.Model = omekaProfile.ModelFingerprint()
	omekaBound.Fingerprint.Profile = omekaProfile.Fingerprint()
	if err := omekaBound.SealFingerprint(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		opts    *format.SerializeOptions
		wantErr string
	}{
		{
			name:    "profile-bound specification without profile",
			opts:    &format.SerializeOptions{Spec: bound},
			wantErr: "requires the exact target system profile",
		},
		{
			name:    "unbound specification with profile",
			opts:    &format.SerializeOptions{Spec: unbound, SystemProfile: drupalProfile},
			wantErr: "unbound transformation cannot use a target system profile",
		},
		{
			name:    "non-Drupal profile",
			opts:    &format.SerializeOptions{Spec: omekaBound, SystemProfile: omekaProfile},
			wantErr: `profile system "omeka-s" is not drupal`,
		},
		{
			name:    "different profile on same model",
			opts:    &format.SerializeOptions{Spec: wrongProfile, SystemProfile: drupalProfile},
			wantErr: "profile fingerprint does not match target profile",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := (&Format{}).Serialize(io.Discard, nil, test.opts)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Serialize() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}

	if err := (&Format{}).Serialize(io.Discard, nil, &format.SerializeOptions{
		Spec: bound, SystemProfile: drupalProfile,
	}); err != nil {
		t.Fatalf("Serialize() exact binding error = %v", err)
	}
}

func TestExtraStringPreservesFloatsAndCanonicalizesStructuredValues(t *testing.T) {
	record := &hubv1.Record{}
	hub.SetExtra(record, "fraction", 12.75)
	hub.SetExtra(record, "structured", map[string]any{
		"z": []any{2.5, map[string]any{"b": 2.0, "a": 1.0}},
		"a": "first",
	})
	hub.SetExtra(record, "list", []any{
		"plain", 1.25, map[string]any{"z": 2.0, "a": "first"}, []any{"nested", 3.5},
	})

	tests := []struct {
		key  string
		want string
	}{
		{key: "fraction", want: "12.75"},
		{key: "structured", want: `{"a":"first","z":[2.5,{"a":1,"b":2}]}`},
		{key: "list", want: `plain|1.25|{"a":"first","z":2}|["nested",3.5]`},
	}
	for _, test := range tests {
		got, err := extraString(record, test.key, "|")
		if err != nil {
			t.Fatalf("extraString(%q) error = %v", test.key, err)
		}
		if got != test.want {
			t.Errorf("extraString(%q) = %q, want %q", test.key, got, test.want)
		}
	}

	if _, err := extraWorkbenchValue(math.Inf(1), "|"); err == nil || !strings.Contains(err.Error(), "not finite") {
		t.Fatalf("non-finite scalar error = %v", err)
	}
	if _, err := extraWorkbenchValue(map[string]any{"bad": math.NaN()}, "|"); err == nil || !strings.Contains(err.Error(), "unsupported value") {
		t.Fatalf("non-finite structured value error = %v", err)
	}
}

func TestSerializeLinkedAgent(t *testing.T) {
	tests := []struct {
		name string
		c    *hubv1.Contributor
		want string
	}{
		{
			name: "person with role",
			c: &hubv1.Contributor{
				Name:     "Qin, Tian",
				RoleCode: "relators:cre",
				Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			},
			want: "relators:cre:person:Qin, Tian",
		},
		{
			name: "person with institution",
			c: &hubv1.Contributor{
				Name:         "Qin, Tian",
				RoleCode:     "relators:cre",
				Type:         hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
				Affiliations: []*hubv1.Affiliation{{Name: "Lehigh University"}},
			},
			want: "relators:cre:person:Qin, Tian - Lehigh University",
		},
		{
			name: "organization",
			c: &hubv1.Contributor{
				Name:     "Lehigh University Press",
				RoleCode: "relators:pbl",
				Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION,
			},
			want: "relators:pbl:corporate_body:Lehigh University Press",
		},
		{
			name: "no role defaults to relators:aut",
			c: &hubv1.Contributor{
				Name: "Smith, Jane",
				Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			},
			want: "relators:aut:person:Smith, Jane",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serializeLinkedAgent(tt.c)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToAgentRow(t *testing.T) {
	c := &hubv1.Contributor{
		Name:         "Qin, Tian",
		RoleCode:     "relators:cre",
		Type:         hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		Affiliations: []*hubv1.Affiliation{{Name: "Lehigh University"}},
		Email:        "bojack212324@gmail.com",
		Status:       "Graduate Student",
		Identifiers: []*hubv1.Identifier{
			hub.NewIdentifier("0000-0001-2345-6789", hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID),
		},
	}

	row := toAgentRow(c)

	if row[0] != "Qin, Tian - Lehigh University" {
		t.Errorf("term_name = %q", row[0])
	}
	if row[1] != "Graduate Student" {
		t.Errorf("field_contributor_status = %q", row[1])
	}
	if row[2] != "schema:worksFor:corporate_body:Lehigh University" {
		t.Errorf("field_relationships = %q", row[2])
	}
	if row[3] != "bojack212324@gmail.com" {
		t.Errorf("field_email = %q", row[3])
	}
	if row[4] != `{"attr0":"orcid","value":"0000-0001-2345-6789"}` {
		t.Errorf("field_identifier = %q", row[4])
	}
}

func TestNeedsAgentRow(t *testing.T) {
	tests := []struct {
		name string
		c    *hubv1.Contributor
		want bool
	}{
		{
			name: "bare name - no agent row",
			c:    &hubv1.Contributor{Name: "Smith, John", RoleCode: "relators:aut"},
			want: false,
		},
		{
			name: "has status",
			c:    &hubv1.Contributor{Name: "Smith, John", Status: "Faculty"},
			want: true,
		},
		{
			name: "has email",
			c:    &hubv1.Contributor{Name: "Smith, John", Email: "j@example.com"},
			want: true,
		},
		{
			name: "has institution",
			c: &hubv1.Contributor{
				Name:         "Smith, John",
				Affiliations: []*hubv1.Affiliation{{Name: "Lehigh University"}},
			},
			want: true,
		},
		{
			name: "has ORCID",
			c: &hubv1.Contributor{
				Name: "Smith, John",
				Identifiers: []*hubv1.Identifier{
					hub.NewIdentifier("0000-0001-2345-6789", hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID),
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsAgentRow(tt.c)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSerialize_MainCSV(t *testing.T) {
	record := &hubv1.Record{
		Title:    "A Study of Something",
		Language: "en",
		Abstract: "This paper examines something important.",
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		},
		Contributors: []*hubv1.Contributor{
			{
				Name:     "Qin, Tian",
				RoleCode: "relators:cre",
				Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			},
			{
				Name:     "Huang, Wei-Min",
				RoleCode: "relators:ths",
				Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			},
		},
		Rights: []*hubv1.Rights{
			{Uri: "http://rightsstatements.org/vocab/InC/1.0/"},
		},
		Identifiers: []*hubv1.Identifier{
			hub.NewIdentifier("10.1234/example", hubv1.IdentifierType_IDENTIFIER_TYPE_DOI),
		},
	}

	var buf bytes.Buffer
	f := &Format{}
	opts := format.NewSerializeOptions()
	opts.IncludeHeader = true
	opts.Spec = spec.FabricatorWorkbench()

	if err := f.Serialize(&buf, []*hubv1.Record{record}, opts); err != nil {
		t.Fatalf("Serialize error: %v", err)
	}

	rows := parseCSV(t, buf.String())
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 rows (header + data), got %d", len(rows))
	}

	header := rows[0]
	data := rows[1]
	colIndex := func(name string) int {
		for i, h := range header {
			if h == name {
				return i
			}
		}
		t.Fatalf("column %q not found in header %v", name, header)
		return -1
	}

	if data[colIndex("title")] != "A Study of Something" {
		t.Errorf("title = %q", data[colIndex("title")])
	}
	if data[colIndex("field_model")] != "Digital Document" {
		t.Errorf("field_model = %q", data[colIndex("field_model")])
	}
	if data[colIndex("field_language")] != "en" {
		t.Errorf("field_language = %q", data[colIndex("field_language")])
	}
	if data[colIndex("field_rights")] != "http://rightsstatements.org/vocab/InC/1.0/" {
		t.Errorf("field_rights = %q", data[colIndex("field_rights")])
	}

	// Two contributors joined with pipe
	linkedAgent := data[colIndex("field_linked_agent")]
	parts := strings.Split(linkedAgent, "|")
	if len(parts) != 2 {
		t.Errorf("expected 2 linked agents, got %d: %q", len(parts), linkedAgent)
	}
	if parts[0] != "relators:cre:person:Qin, Tian" {
		t.Errorf("linked agent 0 = %q", parts[0])
	}
	if parts[1] != "relators:ths:person:Huang, Wei-Min" {
		t.Errorf("linked agent 1 = %q", parts[1])
	}

	// DOI identifier
	idVal := data[colIndex("field_identifier")]
	if !strings.Contains(idVal, `"doi"`) || !strings.Contains(idVal, "10.1234/example") {
		t.Errorf("field_identifier = %q", idVal)
	}
}

func TestSerializeTruncatesLongTitleOnRuneBoundary(t *testing.T) {
	t.Parallel()
	fullTitle := strings.Repeat("界", 260)
	record := &hubv1.Record{Title: fullTitle}

	var output bytes.Buffer
	if err := (&Format{}).Serialize(&output, []*hubv1.Record{record}, builtInSerializeOptions()); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	rows := parseCSV(t, output.String())
	if len(rows) != 2 {
		t.Fatalf("CSV rows = %d, want 2", len(rows))
	}
	columns := make(map[string]string, len(rows[0]))
	for index, header := range rows[0] {
		columns[header] = rows[1][index]
	}
	if got := columns["title"]; got != strings.Repeat("界", 255) {
		t.Errorf("title rune count = %d, want 255", len([]rune(got)))
	}
	if got := columns["field_full_title"]; got != fullTitle {
		t.Errorf("field_full_title changed from source")
	}
}

func TestSerialize_AgentsCSV(t *testing.T) {
	record := &hubv1.Record{
		Title: "A Thesis",
		Contributors: []*hubv1.Contributor{
			{
				Name:         "Qin, Tian",
				RoleCode:     "relators:cre",
				Type:         hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
				Affiliations: []*hubv1.Affiliation{{Name: "Lehigh University"}},
				Email:        "bojack212324@gmail.com",
				Status:       "Graduate Student",
				Identifiers: []*hubv1.Identifier{
					hub.NewIdentifier("0000-0001-2345-6789", hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID),
				},
			},
			// Bare contributor - should NOT appear in agents CSV
			{
				Name:     "Huang, Wei-Min",
				RoleCode: "relators:ths",
				Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			},
		},
	}

	var mainBuf, agentsBuf bytes.Buffer
	f := &Format{}
	opts := format.NewSerializeOptions()
	opts.ExtraWriters = map[string]io.Writer{"agents": &agentsBuf}
	opts.Spec = spec.FabricatorWorkbench()

	if err := f.Serialize(&mainBuf, []*hubv1.Record{record}, opts); err != nil {
		t.Fatalf("Serialize error: %v", err)
	}

	agentRows := parseCSV(t, agentsBuf.String())
	// header + 1 agent row (Huang has no extra metadata)
	if len(agentRows) != 2 {
		t.Fatalf("expected 2 agent rows (header + 1), got %d:\n%s", len(agentRows), agentsBuf.String())
	}

	header := agentRows[0]
	if header[0] != "term_name" {
		t.Errorf("agents header[0] = %q, want term_name", header[0])
	}

	agent := agentRows[1]
	if agent[0] != "Qin, Tian - Lehigh University" {
		t.Errorf("term_name = %q", agent[0])
	}
	if agent[1] != "Graduate Student" {
		t.Errorf("field_contributor_status = %q", agent[1])
	}
	if agent[2] != "schema:worksFor:corporate_body:Lehigh University" {
		t.Errorf("field_relationships = %q", agent[2])
	}
	if agent[3] != "bojack212324@gmail.com" {
		t.Errorf("field_email = %q", agent[3])
	}
	if agent[4] != `{"attr0":"orcid","value":"0000-0001-2345-6789"}` {
		t.Errorf("field_identifier = %q", agent[4])
	}
}

func TestSerializeDatasetPreservesArbitraryDepthHierarchy(t *testing.T) {
	t.Parallel()

	dataset := hierarchyDataset()
	hub.SetExtra(dataset.Records[0].Record, "id", "root-42")
	hub.SetExtra(dataset.Records[2].Record, "id", "1")
	before := make([]*hubv1.Record, len(dataset.Records))
	for index, entry := range dataset.Records {
		before[index] = proto.Clone(entry.Record).(*hubv1.Record)
	}

	var first bytes.Buffer
	if err := (&Format{}).SerializeDataset(&first, dataset, builtInSerializeOptions()); err != nil {
		t.Fatalf("SerializeDataset() error = %v", err)
	}
	var second bytes.Buffer
	if err := (&Format{}).SerializeDataset(&second, dataset, builtInSerializeOptions()); err != nil {
		t.Fatalf("SerializeDataset() second error = %v", err)
	}
	if first.String() != second.String() {
		t.Fatal("SerializeDataset() output is not deterministic")
	}

	rows := parseCSV(t, first.String())
	if len(rows) != 6 {
		t.Fatalf("CSV rows = %d, want 6", len(rows))
	}
	want := map[string][3]string{
		"root":       {"root-42", "", "0"},
		"child-a":    {"2", "root-42", "0"},
		"grandchild": {"1", "2", "0"},
		"child-b":    {"3", "root-42", "1"},
		"other-root": {"4", "", "1"},
	}
	for _, row := range rows[1:] {
		title := csvRowValue(rows[0], row, "title")
		got := [3]string{
			csvRowValue(rows[0], row, "id"),
			csvRowValue(rows[0], row, "parent_id"),
			csvRowValue(rows[0], row, "field_weight"),
		}
		if got != want[title] {
			t.Errorf("record %q operational fields = %v, want %v", title, got, want[title])
		}
	}

	for index, entry := range dataset.Records {
		if !proto.Equal(entry.Record, before[index]) {
			t.Errorf("caller record %q was mutated", entry.Key)
		}
	}
}

func TestSerializeDatasetRejectsOperationalConflicts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*format.Dataset)
		wantErr string
	}{
		{
			name: "duplicate explicit upload ID",
			mutate: func(dataset *format.Dataset) {
				hub.SetExtra(dataset.Records[0].Record, "id", "same")
				hub.SetExtra(dataset.Records[1].Record, "id", "same")
			},
			wantErr: "same upload ID",
		},
		{
			name: "invalid explicit upload ID",
			mutate: func(dataset *format.Dataset) {
				hub.SetExtra(dataset.Records[0].Record, "id", "invalid id")
			},
			wantErr: "must not contain whitespace",
		},
		{
			name: "parent conflict",
			mutate: func(dataset *format.Dataset) {
				hub.SetExtra(dataset.Records[1].Record, "parent_id", "wrong")
			},
			wantErr: "conflicts with dataset hierarchy value",
		},
		{
			name: "root parent conflict",
			mutate: func(dataset *format.Dataset) {
				hub.SetExtra(dataset.Records[0].Record, "parent_id", "external")
			},
			wantErr: "conflicts with dataset hierarchy value",
		},
		{
			name: "weight conflict",
			mutate: func(dataset *format.Dataset) {
				hub.SetExtra(dataset.Records[3].Record, "field_weight", "0")
			},
			wantErr: "conflicts with dataset hierarchy position 1",
		},
		{
			name: "invalid weight type",
			mutate: func(dataset *format.Dataset) {
				hub.SetExtra(dataset.Records[1].Record, "field_weight", true)
			},
			wantErr: "must be a string or integer",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dataset := hierarchyDataset()
			test.mutate(dataset)
			var output bytes.Buffer
			err := (&Format{}).SerializeDataset(&output, dataset, nil)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("SerializeDataset() error = %v, want containing %q", err, test.wantErr)
			}
			if output.Len() != 0 {
				t.Fatalf("SerializeDataset() wrote %d bytes before rejecting conflict", output.Len())
			}
		})
	}
}

func TestSerializeDatasetLeavesFlatOutputUnchanged(t *testing.T) {
	t.Parallel()

	records := []*hubv1.Record{{Title: "First"}, {Title: "Second"}}
	hub.SetExtra(records[0], "id", "existing")
	dataset := &format.Dataset{
		Records: []format.DatasetRecord{
			{Key: "first", Record: records[0]},
			{Key: "second", Record: records[1]},
		},
		Hierarchy: format.Hierarchy{Nodes: []format.HierarchyNode{
			{RecordKey: "first", Position: 0},
			{RecordKey: "second", Position: 1},
		}},
		Provenance: format.DatasetProvenance{Format: "test"},
	}
	var flat, hierarchical bytes.Buffer
	serializer := &Format{}
	if err := serializer.Serialize(&flat, records, builtInSerializeOptions()); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	if err := serializer.SerializeDataset(&hierarchical, dataset, builtInSerializeOptions()); err != nil {
		t.Fatalf("SerializeDataset() error = %v", err)
	}
	if flat.String() != hierarchical.String() {
		t.Fatalf("flat output changed:\nSerialize:\n%s\nSerializeDataset:\n%s", flat.String(), hierarchical.String())
	}
}

func builtInSerializeOptions() *format.SerializeOptions {
	opts := format.NewSerializeOptions()
	opts.Spec = spec.FabricatorWorkbench()
	return opts
}

func TestSerializeDatasetRejectsColumnsThatDropHierarchy(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := (&Format{}).SerializeDataset(&output, hierarchyDataset(), &format.SerializeOptions{
		Columns:       []string{"id", "title"},
		IncludeHeader: true,
	})
	if err == nil || !strings.Contains(err.Error(), `requires column "parent_id"`) {
		t.Fatalf("SerializeDataset() error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("SerializeDataset() wrote %d bytes before rejecting columns", output.Len())
	}
}

func TestIslandoraModel(t *testing.T) {
	tests := []struct {
		rt   hubv1.ResourceTypeValue
		want string
	}{
		{hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE, "Image"},
		{hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO, "Video"},
		{hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO, "Audio"},
		{hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION, "Collection"},
		{hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET, "Binary"},
		{hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE, "Digital Document"},
		{hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS, "Digital Document"},
	}

	for _, tt := range tests {
		got := islandoraModel(&hubv1.ResourceType{Type: tt.rt})
		if got != tt.want {
			t.Errorf("%v: got %q, want %q", tt.rt, got, tt.want)
		}
	}
}

func parseCSV(t *testing.T, s string) [][]string {
	t.Helper()
	r := csv.NewReader(strings.NewReader(s))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parsing CSV: %v\n%s", err, s)
	}
	return rows
}

func csvRowValue(header, row []string, name string) string {
	for index, candidate := range header {
		if candidate == name {
			return row[index]
		}
	}
	return ""
}

func hierarchyDataset() *format.Dataset {
	keys := []string{"root", "child-a", "grandchild", "child-b", "other-root"}
	records := make([]format.DatasetRecord, len(keys))
	for index, key := range keys {
		records[index] = format.DatasetRecord{Key: key, Record: &hubv1.Record{Title: key}}
	}
	return &format.Dataset{
		Records: records,
		Hierarchy: format.Hierarchy{Nodes: []format.HierarchyNode{
			{RecordKey: "root", Position: 0},
			{RecordKey: "child-a", ParentKey: "root", Position: 0},
			{RecordKey: "grandchild", ParentKey: "child-a", Position: 0},
			{RecordKey: "child-b", ParentKey: "root", Position: 1},
			{RecordKey: "other-root", Position: 1},
		}},
		Provenance: format.DatasetProvenance{Format: "test"},
	}
}
