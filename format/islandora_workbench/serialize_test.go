package islandora_workbench

import (
	"bytes"
	"encoding/csv"
	"io"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

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
