package format_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/format/archivesspace"
	"github.com/lehigh-university-libraries/crosswalk/format/crossrefrest"
	"github.com/lehigh-university-libraries/crosswalk/format/csl"
	"github.com/lehigh-university-libraries/crosswalk/format/drupal"
	omekas "github.com/lehigh-university-libraries/crosswalk/format/omeka_s"
	"github.com/lehigh-university-libraries/crosswalk/format/scopus"
	"github.com/lehigh-university-libraries/crosswalk/format/wos"
	"github.com/lehigh-university-libraries/crosswalk/format/zenodo"
)

type testFormat struct {
	name       string
	extensions []string
	marker     []byte
}

func (f *testFormat) Name() string { return f.name }

func (f *testFormat) Description() string { return f.name }

func (f *testFormat) Extensions() []string { return f.extensions }

func (f *testFormat) CanParse(peek []byte) bool { return bytes.Contains(peek, f.marker) }

func TestRegistryRegistrationNormalizesAndRejectsDuplicates(t *testing.T) {
	t.Parallel()

	registry := new(format.Registry)
	original := &testFormat{name: "  Example-JSON  ", extensions: []string{" .JSON "}, marker: []byte("original")}
	if err := registry.Register(original); err != nil {
		t.Fatalf("Register(original) error = %v", err)
	}

	got, ok := registry.Get(" EXAMPLE-json ")
	if !ok {
		t.Fatal("Get() did not find normalized format name")
	}
	if got != original {
		t.Fatalf("Get() = %T %p, want original %p", got, got, original)
	}

	replacement := &testFormat{name: "example-JSON", extensions: []string{"json"}, marker: []byte("replacement")}
	err := registry.Register(replacement)
	if !errors.Is(err, format.ErrDuplicateFormat) {
		t.Fatalf("Register(duplicate) error = %v, want ErrDuplicateFormat", err)
	}
	got, ok = registry.Get("example-json")
	if !ok || got != original {
		t.Fatalf("duplicate registration replaced original: got %T %p, want %p", got, got, original)
	}

	detected, err := registry.DetectFormat("record.JsOn", []byte("original"))
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if detected != original {
		t.Fatalf("DetectFormat() = %T %p, want original %p", detected, detected, original)
	}
}

func TestRegistryRegisterRejectsInvalidFormats(t *testing.T) {
	t.Parallel()

	var nilFormat *testFormat
	tests := []struct {
		name   string
		format format.Format
	}{
		{name: "nil interface", format: nil},
		{name: "typed nil", format: nilFormat},
		{name: "empty name", format: &testFormat{name: " \t"}},
		{name: "empty extension", format: &testFormat{name: "valid", extensions: []string{"."}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := format.NewRegistry().Register(test.format); err == nil {
				t.Fatal("Register() error = nil, want validation error")
			}
		})
	}
}

func TestRegistryListIsNormalizedAndSorted(t *testing.T) {
	t.Parallel()

	registry := format.NewRegistry()
	for _, registered := range []*testFormat{
		{name: "Zulu"},
		{name: " alpha "},
		{name: "Middle"},
	} {
		if err := registry.Register(registered); err != nil {
			t.Fatalf("Register(%q) error = %v", registered.name, err)
		}
	}

	if got, want := registry.List(), []string{"alpha", "middle", "zulu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
}

func TestRegistryDetectsMultipleJSONFormatsByContent(t *testing.T) {
	t.Parallel()

	registry := format.NewRegistry()
	for _, registered := range []format.Format{
		&wos.Format{},
		&drupal.Format{},
		&csl.Format{},
		&crossrefrest.Format{},
		&zenodo.Format{},
		&scopus.Format{},
		&omekas.Format{},
		&archivesspace.Format{},
	} {
		if err := registry.Register(registered); err != nil {
			t.Fatalf("Register(%q) error = %v", registered.Name(), err)
		}
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "Crossref REST",
			content: `{"message":{"items":[{"DOI":"10.1234/example"}]}}`,
			want:    "crossref-rest",
		},
		{
			name:    "Web of Science",
			content: `{"hits":[{"uid":"WOS:1","source":{"title":"Journal"}}]}`,
			want:    "wos",
		},
		{
			name:    "Zenodo record",
			content: `{"id":44,"metadata":{"title":"One"}}`,
			want:    "zenodo",
		},
		{
			name:    "Zenodo empty page",
			content: `{"hits":{"total":{"value":0},"hits":[]}}`,
			want:    "zenodo",
		},
		{
			name:    "Scopus",
			content: `{"eid":"2-s2.0-85123456789","dc:title":"One","prism:publicationName":"Journal"}`,
			want:    "scopus",
		},
		{
			name:    "Omeka S",
			content: `{"@type":"o:Item","o:id":1,"o:title":"One"}`,
			want:    "omeka-s",
		},
		{
			name:    "ArchivesSpace",
			content: "{\n  \"jsonmodel_type\" :\n  \"resource\",\n  \"uri\": \"/repositories/2/resources/1\"\n}",
			want:    "archivesspace",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := registry.DetectFormat("records.JSON", []byte(test.content))
			if err != nil {
				t.Fatalf("DetectFormat() error = %v", err)
			}
			if got.Name() != test.want {
				t.Fatalf("DetectFormat().Name() = %q, want %q", got.Name(), test.want)
			}
		})
	}
}

func TestRegistryDetectFormatReportsSortedJSONAmbiguity(t *testing.T) {
	t.Parallel()

	registry := format.NewRegistry()
	for _, registered := range []format.Format{
		&drupal.Format{},
		&csl.Format{},
		&wos.Format{},
	} {
		if err := registry.Register(registered); err != nil {
			t.Fatalf("Register(%q) error = %v", registered.Name(), err)
		}
	}

	_, err := registry.DetectFormat(
		"record.json",
		[]byte(`{"type":"article","title":"Example","uuid":"record-1","field_topic":[]}`),
	)
	var ambiguity *format.AmbiguousFormatError
	if !errors.As(err, &ambiguity) {
		t.Fatalf("DetectFormat() error = %v, want AmbiguousFormatError", err)
	}
	if got, want := ambiguity.Candidates, []string{"csl", "drupal"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ambiguity candidates = %v, want %v", got, want)
	}
}

func TestRegistryDetectFormatNarrowsContentProbesByExtension(t *testing.T) {
	t.Parallel()

	registry := format.NewRegistry()
	jsonFormat := &testFormat{name: "json-format", extensions: []string{"json"}, marker: []byte("shared")}
	xmlFormat := &testFormat{name: "xml-format", extensions: []string{"xml"}, marker: []byte("shared")}
	for _, registered := range []format.Format{xmlFormat, jsonFormat} {
		if err := registry.Register(registered); err != nil {
			t.Fatalf("Register(%q) error = %v", registered.Name(), err)
		}
	}

	got, err := registry.DetectFormat("record.JSON", []byte("shared"))
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if got != jsonFormat {
		t.Fatalf("DetectFormat() = %T %p, want JSON format %p", got, got, jsonFormat)
	}

	_, err = registry.DetectFromContent([]byte("shared"))
	var ambiguity *format.AmbiguousFormatError
	if !errors.As(err, &ambiguity) {
		t.Fatalf("DetectFromContent() error = %v, want AmbiguousFormatError", err)
	}
	if got, want := ambiguity.Candidates, []string{"json-format", "xml-format"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ambiguity candidates = %v, want %v", got, want)
	}
}

func TestRegistryDetectFormatDoesNotChooseSharedExtensionWithoutEvidence(t *testing.T) {
	t.Parallel()

	registry := format.NewRegistry()
	for _, registered := range []*testFormat{
		{name: "zeta", extensions: []string{"json"}, marker: []byte("zeta")},
		{name: "alpha", extensions: []string{"json"}, marker: []byte("alpha")},
	} {
		if err := registry.Register(registered); err != nil {
			t.Fatalf("Register(%q) error = %v", registered.Name(), err)
		}
	}

	_, err := registry.DetectFormat("record.json", []byte(`{"unknown":true}`))
	var ambiguity *format.AmbiguousFormatError
	if !errors.As(err, &ambiguity) {
		t.Fatalf("DetectFormat() error = %v, want AmbiguousFormatError", err)
	}
	if got, want := ambiguity.Candidates, []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ambiguity candidates = %v, want %v", got, want)
	}
}
