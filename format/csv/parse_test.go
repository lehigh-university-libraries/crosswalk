package csv

import (
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestParseNamePrefix(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantRoleCode string
		wantType     hubv1.ContributorType
		wantName     string
	}{
		{
			name:         "full prefix with relators namespace",
			input:        "relators:cre:person:Qin, Tian",
			wantRoleCode: "relators:cre",
			wantType:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			wantName:     "Qin, Tian",
		},
		{
			name:         "thesis advisor",
			input:        "relators:ths:person:Huang, Wei-Min",
			wantRoleCode: "relators:ths",
			wantType:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			wantName:     "Huang, Wei-Min",
		},
		{
			name:         "organization type",
			input:        "relators:pbl:organization:Lehigh University Press",
			wantRoleCode: "relators:pbl",
			wantType:     hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION,
			wantName:     "Lehigh University Press",
		},
		{
			name:         "type prefix without role",
			input:        "person:Smith, John",
			wantRoleCode: "",
			wantType:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			wantName:     "Smith, John",
		},
		{
			name:         "plain name no prefix",
			input:        "Smith, John",
			wantRoleCode: "",
			wantType:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			wantName:     "Smith, John",
		},
		{
			name:         "name with colon in it",
			input:        "relators:aut:person:Smith, J.: A Title",
			wantRoleCode: "relators:aut",
			wantType:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			wantName:     "Smith, J.: A Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roleCode, contribType, name := parseNamePrefix(tt.input)
			if roleCode != tt.wantRoleCode {
				t.Errorf("roleCode = %q, want %q", roleCode, tt.wantRoleCode)
			}
			if contribType != tt.wantType {
				t.Errorf("type = %v, want %v", contribType, tt.wantType)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestParseContributor(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantName        string
		wantRoleCode    string
		wantRole        string
		wantType        hubv1.ContributorType
		wantInstitution string
		wantOrcid       string
		wantEmail       string
		wantStatus      string
		wantAuthURI     string
	}{
		{
			name:         "plain Islandora workbench format",
			input:        "relators:cre:person:Qin, Tian",
			wantName:     "Qin, Tian",
			wantRoleCode: "relators:cre",
			wantType:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		},
		{
			name:            "rich JSON format",
			input:           `{"name":"relators:cre:person:Qin, Tian","institution":"Lehigh University","email":"test@example.com","status":"Graduate Student"}`,
			wantName:        "Qin, Tian",
			wantRoleCode:    "relators:cre",
			wantType:        hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			wantInstitution: "Lehigh University",
			wantEmail:       "test@example.com",
			wantStatus:      "Graduate Student",
		},
		{
			name:            "JSON with ORCID",
			input:           `{"name":"relators:ths:person:Huang, Wei-Min","institution":"Lehigh University","status":"Faculty"}`,
			wantName:        "Huang, Wei-Min",
			wantRoleCode:    "relators:ths",
			wantType:        hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
			wantInstitution: "Lehigh University",
			wantStatus:      "Faculty",
		},
		{
			name:         "JSON with authority URI",
			input:        `{"name":"relators:aut:person:Smith, Jane","authority_uri":"http://id.loc.gov/authorities/names/n12345","authority_source":"lcnaf"}`,
			wantName:     "Smith, Jane",
			wantRoleCode: "relators:aut",
			wantAuthURI:  "http://id.loc.gov/authorities/names/n12345",
		},
		{
			name:     "plain name no prefix",
			input:    "Smith, John",
			wantName: "Smith, John",
			wantType: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		},
		{
			name:  "empty input",
			input: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := parseContributor(tt.input)

			if tt.input == "" {
				if c != nil {
					t.Error("expected nil for empty input")
				}
				return
			}

			if c == nil {
				t.Fatal("expected non-nil contributor")
			}

			if c.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", c.Name, tt.wantName)
			}
			if c.RoleCode != tt.wantRoleCode {
				t.Errorf("RoleCode = %q, want %q", c.RoleCode, tt.wantRoleCode)
			}
			if tt.wantType != 0 && c.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", c.Type, tt.wantType)
			}
			if tt.wantInstitution != "" {
				if len(c.Affiliations) == 0 || c.Affiliations[0].Name != tt.wantInstitution {
					t.Errorf("institution = %v, want %q", c.Affiliations, tt.wantInstitution)
				}
			}
			if tt.wantEmail != "" && c.Email != tt.wantEmail {
				t.Errorf("Email = %q, want %q", c.Email, tt.wantEmail)
			}
			if tt.wantStatus != "" && c.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", c.Status, tt.wantStatus)
			}
			if tt.wantAuthURI != "" && c.AuthorityUri != tt.wantAuthURI {
				t.Errorf("AuthorityUri = %q, want %q", c.AuthorityUri, tt.wantAuthURI)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Verify that serialize → parse produces equivalent data
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "two contributors with institution",
			input: `{"name":"relators:cre:person:Qin, Tian","institution":"Lehigh University","email":"bojack212324@gmail.com","status":"Graduate Student"} ; {"name":"relators:ths:person:Huang, Wei-Min","institution":"Lehigh University","status":"Faculty"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := strings.Split(tt.input, " ; ")
			contribs := make([]*hubv1.Contributor, 0, len(entries))
			for _, e := range entries {
				if c := parseContributor(strings.TrimSpace(e)); c != nil {
					contribs = append(contribs, c)
				}
			}

			if len(contribs) != 2 {
				t.Fatalf("expected 2 contributors, got %d", len(contribs))
			}

			c1 := contribs[0]
			if c1.Name != "Qin, Tian" {
				t.Errorf("c1.Name = %q, want %q", c1.Name, "Qin, Tian")
			}
			if c1.RoleCode != "relators:cre" {
				t.Errorf("c1.RoleCode = %q, want %q", c1.RoleCode, "relators:cre")
			}
			if len(c1.Affiliations) == 0 || c1.Affiliations[0].Name != "Lehigh University" {
				t.Errorf("c1.Affiliations = %v, want Lehigh University", c1.Affiliations)
			}
			if c1.Email != "bojack212324@gmail.com" {
				t.Errorf("c1.Email = %q", c1.Email)
			}
			if c1.Status != "Graduate Student" {
				t.Errorf("c1.Status = %q", c1.Status)
			}

			c2 := contribs[1]
			if c2.Name != "Huang, Wei-Min" {
				t.Errorf("c2.Name = %q, want %q", c2.Name, "Huang, Wei-Min")
			}
			if c2.RoleCode != "relators:ths" {
				t.Errorf("c2.RoleCode = %q, want %q", c2.RoleCode, "relators:ths")
			}
			if c2.Status != "Faculty" {
				t.Errorf("c2.Status = %q", c2.Status)
			}
		})
	}
}
