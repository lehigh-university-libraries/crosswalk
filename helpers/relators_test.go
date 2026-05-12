package helpers

import "testing"

func TestRelatorLabelUsesFullMARCList(t *testing.T) {
	if got, want := len(MARCRelators), 305; got != want {
		t.Fatalf("len(MARCRelators) = %d, want %d", got, want)
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"interviewer namespace", "relators:ivr", "Interviewer"},
		{"interviewee uri", "http://id.loc.gov/vocabulary/relators/ive", "Interviewee"},
		{"thesis advisor", "relators:ths", "Thesis advisor"},
		{"degree committee member", "dgc", "Degree committee member"},
		{"publication place", "relators:pup", "Publication place"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RelatorLabel(tt.input); got != tt.want {
				t.Errorf("RelatorLabel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeRoleUsesFullMARCList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"interviewer label", "interviewer", "ivr"},
		{"interviewee label", "Interviewee", "ive"},
		{"thesis advisor label", "thesis advisor", "ths"},
		{"relators namespace", "relators:ivr", "ivr"},
		{"full uri", "http://id.loc.gov/vocabulary/relators/ths", "ths"},
		{"legacy introduction label", "Author of introduction", "aui"},
		{"unknown label", "made up role", "made up role"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeRole(tt.input); got != tt.want {
				t.Errorf("NormalizeRole(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
