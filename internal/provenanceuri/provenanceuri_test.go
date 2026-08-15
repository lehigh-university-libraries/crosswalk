package provenanceuri

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		options Options
		want    string
		wantErr bool
	}{
		{
			name:  "redacts credentials",
			value: " HTTPS://user:password@Example.ORG/records/1?page=2&access_token=secret#details ",
			want:  "https://example.org/records/1?page=2",
		},
		{
			name:  "credential key variants",
			value: "https://example.org/records/1?X-Amz-Signature=secret&key_credential=secret&AWSAccessKeyId=secret&client_secret=secret&download=1",
			want:  "https://example.org/records/1?download=1",
		},
		{
			name:    "relative with base",
			value:   "/repositories/2/resources/1?key=secret&resolve=1#tree",
			options: Options{BaseURL: "https://USER:PASS@ASPACE.EXAMPLE.EDU/staff?session=secret#top"},
			want:    "https://aspace.example.edu/repositories/2/resources/1?resolve=1",
		},
		{
			name:    "allowed relative",
			value:   "/repositories/2/resources/1?resolve=1#tree",
			options: Options{AllowRelative: true},
			want:    "/repositories/2/resources/1?resolve=1",
		},
		{name: "empty", value: "  ", want: ""},
		{name: "relative rejected", value: "/records/1", wantErr: true},
		{name: "network path rejected", value: "//example.org/records/1", options: Options{AllowRelative: true}, wantErr: true},
		{name: "network path rejected with base", value: "//evil.example/records/1", options: Options{BaseURL: "https://example.org/api/"}, wantErr: true},
		{name: "unsafe scheme rejected", value: "file:///tmp/record.json", wantErr: true},
		{name: "opaque HTTP rejected", value: "https:record", wantErr: true},
		{name: "control rejected", value: "https://example.org/records/1\nAuthorization: secret", wantErr: true},
		{name: "too long", value: "https://example.org/" + strings.Repeat("a", maxURIBytes), wantErr: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Normalize(test.value, test.options)
			if test.wantErr {
				if err == nil {
					t.Fatalf("Normalize() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			if got != test.want {
				t.Errorf("Normalize() = %q, want %q", got, test.want)
			}
		})
	}
}
