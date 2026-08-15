package hub

import (
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestValidateProfileProvenanceFingerprints(t *testing.T) {
	digest := strings.Repeat("a", 64)
	tests := []struct {
		name   string
		source *hubv1.SourceInfo
		valid  bool
	}{
		{name: "none", source: &hubv1.SourceInfo{}, valid: true},
		{name: "complete", source: &hubv1.SourceInfo{Profile: "site", ProfileFingerprint: digest, ModelFingerprint: digest}, valid: true},
		{name: "missing model", source: &hubv1.SourceInfo{Profile: "site", ProfileFingerprint: digest}},
		{name: "missing profile", source: &hubv1.SourceInfo{ProfileFingerprint: digest, ModelFingerprint: digest}},
		{name: "uppercase", source: &hubv1.SourceInfo{Profile: "site", ProfileFingerprint: strings.ToUpper(digest), ModelFingerprint: digest}},
		{name: "not hex", source: &hubv1.SourceInfo{Profile: "site", ProfileFingerprint: strings.Repeat("z", 64), ModelFingerprint: digest}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := &hubv1.Record{Title: "Example", SourceInfo: test.source}
			result := Validate(record, DefaultValidationOptions())
			if result.IsValid() != test.valid {
				t.Fatalf("Validate() valid = %t errors=%v, want %t", result.IsValid(), result.Errors, test.valid)
			}
		})
	}
}

func TestValidateSourceInfoURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		uri   string
		valid bool
	}{
		{name: "empty", valid: true},
		{name: "absolute", uri: "https://example.org/records/1?page=2", valid: true},
		{name: "relative", uri: "/repositories/2/resources/1", valid: true},
		{name: "credentials", uri: "https://user:password@example.org/records/1"},
		{name: "secret query", uri: "https://example.org/records/1?access_token=secret"},
		{name: "fragment", uri: "https://example.org/records/1#details"},
		{name: "unsafe scheme", uri: "file:///tmp/record.json"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := Validate(&hubv1.Record{Title: "Example", SourceInfo: &hubv1.SourceInfo{SourceUri: test.uri}}, DefaultValidationOptions())
			if result.IsValid() != test.valid {
				t.Fatalf("Validate() valid = %t errors=%v, want %t", result.IsValid(), result.Errors, test.valid)
			}
		})
	}
}
