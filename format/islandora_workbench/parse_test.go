package islandora_workbench

import (
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestParseWorkbenchLinkedAgent(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantName        string
		wantRole        string
		wantType        hubv1.ContributorType
		wantInstitution string
	}{
		{
			name:     "person with role",
			input:    "relators:cre:person:Qin, Tian",
			wantName: "Qin, Tian",
			wantRole: "relators:cre",
			wantType: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		},
		{
			name:            "person with role and institution",
			input:           "relators:cre:person:Qin, Tian - Lehigh University",
			wantName:        "Qin, Tian",
			wantRole:        "relators:cre",
			wantType:        hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			wantInstitution: "Lehigh University",
		},
		{
			name:     "corporate body",
			input:    "relators:pbl:corporate_body:Lehigh University Press",
			wantName: "Lehigh University Press",
			wantRole: "relators:pbl",
			wantType: hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION,
		},
		{
			name:     "plain name no role",
			input:    "Smith, Jane",
			wantName: "Smith, Jane",
			wantType: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWorkbenchLinkedAgent(tt.input)
			if got == nil {
				t.Fatal("got nil contributor")
			}
			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}
			if got.RoleCode != tt.wantRole {
				t.Errorf("RoleCode = %q, want %q", got.RoleCode, tt.wantRole)
			}
			if got.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
			}
			institution := ""
			if len(got.Affiliations) > 0 {
				institution = got.Affiliations[0].Name
			}
			if institution != tt.wantInstitution {
				t.Errorf("Institution = %q, want %q", institution, tt.wantInstitution)
			}
		})
	}
}

func TestExtractAttrValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`{"value":"This paper examines something.","attr0":"abstract"}`, "This paper examines something."},
		{`{"value":"256 pages","attr0":"page"}`, "256 pages"},
		{"plain text", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractAttrValue(tt.input)
		if got != tt.want {
			t.Errorf("extractAttrValue(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIslandoraModelToResourceType(t *testing.T) {
	tests := []struct {
		model string
		want  hubv1.ResourceTypeValue
	}{
		{"Image", hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE},
		{"Video", hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO},
		{"Audio", hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO},
		{"Collection", hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION},
		{"Binary", hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET},
		{"Digital Document", hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE},
		{"Unknown", hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE},
	}

	for _, tt := range tests {
		rt := islandoraModelToResourceType(tt.model)
		if rt == nil || rt.Type != tt.want {
			t.Errorf("islandoraModelToResourceType(%q) = %v, want %v", tt.model, rt.Type, tt.want)
		}
	}
}

func TestParse_StandardColumns(t *testing.T) {
	csvInput := "id,title,field_model,field_language,field_rights,field_linked_agent,field_edtf_date_issued,field_identifier\n" +
		`1,A Study of Something,Digital Document,en,http://rightsstatements.org/vocab/InC/1.0/,"relators:cre:person:Qin, Tian|relators:ths:person:Huang, Wei-Min",2024,"{""value"":""10.1234/example"",""attr0"":""doi""}"` + "\n"

	f := &Format{}
	opts := format.NewParseOptions()
	records, err := f.Parse(strings.NewReader(csvInput), opts)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]

	if r.Title != "A Study of Something" {
		t.Errorf("Title = %q", r.Title)
	}
	if r.Language != "en" {
		t.Errorf("Language = %q", r.Language)
	}
	if r.ResourceType == nil || r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE {
		t.Errorf("ResourceType = %v", r.ResourceType)
	}
	if len(r.Rights) != 1 || r.Rights[0].Uri != "http://rightsstatements.org/vocab/InC/1.0/" {
		t.Errorf("Rights = %v", r.Rights)
	}
	if len(r.Contributors) != 2 {
		t.Fatalf("expected 2 contributors, got %d", len(r.Contributors))
	}
	if r.Contributors[0].Name != "Qin, Tian" || r.Contributors[0].RoleCode != "relators:cre" {
		t.Errorf("contributor[0] = %+v", r.Contributors[0])
	}
	if r.Contributors[1].Name != "Huang, Wei-Min" || r.Contributors[1].RoleCode != "relators:ths" {
		t.Errorf("contributor[1] = %+v", r.Contributors[1])
	}
	if len(r.Identifiers) != 1 || r.Identifiers[0].Value != "10.1234/example" {
		t.Errorf("Identifiers = %v", r.Identifiers)
	}
	if r.Identifiers[0].Type != hubv1.IdentifierType_IDENTIFIER_TYPE_DOI {
		t.Errorf("Identifier type = %v", r.Identifiers[0].Type)
	}
}

func TestParse_AbstractAndExtent(t *testing.T) {
	csvInput := "title,field_abstract,field_extent\n" +
		`A Paper,"{""value"":""This examines X."",""attr0"":""abstract""}","{""value"":""200 pages"",""attr0"":""page""}"` + "\n"

	f := &Format{}
	records, err := f.Parse(strings.NewReader(csvInput), nil)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Abstract != "This examines X." {
		t.Errorf("Abstract = %q", r.Abstract)
	}
	if r.PhysicalDesc != "200 pages" {
		t.Errorf("PhysicalDesc = %q", r.PhysicalDesc)
	}
}

func TestParse_ReservedColumns(t *testing.T) {
	csvInput := "id,node_id,title\n42,100,My Item\n"

	f := &Format{}
	records, err := f.Parse(strings.NewReader(csvInput), nil)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Title != "My Item" {
		t.Errorf("Title = %q", r.Title)
	}
}

func TestParse_RoundTrip(t *testing.T) {
	// Serialize a record, then parse it back and verify key fields survive.
	record := &hubv1.Record{
		Title:    "Round Trip Test",
		Language: "en",
		Abstract: "Test abstract.",
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		},
		Contributors: []*hubv1.Contributor{
			{
				Name:     "Doe, Jane",
				RoleCode: "relators:aut",
				Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			},
		},
		Rights: []*hubv1.Rights{
			{Uri: "http://rightsstatements.org/vocab/InC/1.0/"},
		},
	}

	var buf strings.Builder
	f := &Format{}
	serOpts := format.NewSerializeOptions()
	serOpts.IncludeHeader = true
	if err := f.Serialize(&buf, []*hubv1.Record{record}, serOpts); err != nil {
		t.Fatalf("Serialize error: %v", err)
	}

	parseOpts := format.NewParseOptions()
	parsed, err := f.Parse(strings.NewReader(buf.String()), parseOpts)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 record, got %d", len(parsed))
	}

	p := parsed[0]
	if p.Title != record.Title {
		t.Errorf("Title = %q, want %q", p.Title, record.Title)
	}
	if p.Language != record.Language {
		t.Errorf("Language = %q, want %q", p.Language, record.Language)
	}
	if p.Abstract != record.Abstract {
		t.Errorf("Abstract = %q, want %q", p.Abstract, record.Abstract)
	}
	if len(p.Contributors) != 1 {
		t.Fatalf("expected 1 contributor, got %d", len(p.Contributors))
	}
	if p.Contributors[0].Name != "Doe, Jane" {
		t.Errorf("contributor name = %q", p.Contributors[0].Name)
	}
	if p.Contributors[0].RoleCode != "relators:aut" {
		t.Errorf("contributor role = %q", p.Contributors[0].RoleCode)
	}
	if len(p.Rights) != 1 || p.Rights[0].Uri != "http://rightsstatements.org/vocab/InC/1.0/" {
		t.Errorf("Rights = %v", p.Rights)
	}
}
