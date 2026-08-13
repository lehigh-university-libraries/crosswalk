package spec

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileDrupalDirectoryReconcilesBundle(t *testing.T) {
	transformation, err := CompileDrupalDirectory(filepath.Join("testdata", "drupal"), DrupalCompileOptions{Bundle: "islandora_object"})
	if err != nil {
		t.Fatalf("CompileDrupalDirectory() error = %v", err)
	}
	if transformation.Fingerprint.SiteName != "Example Repository" || transformation.Fingerprint.SiteUUID == "" {
		t.Fatalf("Fingerprint provenance = %#v", transformation.Fingerprint)
	}
	if transformation.Fingerprint.Bundle != "islandora_object" || len(transformation.Fingerprint.Value) != 64 || len(transformation.Fingerprint.Model) != 64 || len(transformation.Fingerprint.ConfigHash) != 64 {
		t.Fatalf("Fingerprint = %#v", transformation.Fingerprint)
	}
	fullTitle, ok := transformation.SourceField("Complete Title")
	if !ok {
		t.Fatal("compiled source does not include reconciled Full Title")
	}
	if fullTitle.Label != "Complete Title" || fullTitle.Description == "" || fullTitle.SourceType != "string_long" || !fullTitle.IsRequired(OperationCreate) {
		t.Fatalf("Full Title field = %#v", fullTitle)
	}
	if !fullTitle.Matches("Full Title") {
		t.Fatal("reconciled field did not retain its historical human label alias")
	}
	doi, ok := transformation.SourceField("field_identifier.attr0=doi")
	if !ok || doi.IsRequired(OperationCreate) {
		t.Fatalf("required aggregate field incorrectly made every identifier pseudo-column required: %#v", doi)
	}
	var identifierGroup *RequiredGroup
	for index := range transformation.Source.RequiredGroups {
		if transformation.Source.RequiredGroups[index].Name == "field_identifier" {
			identifierGroup = &transformation.Source.RequiredGroups[index]
			break
		}
	}
	wantIdentifierFields := []string{
		"field_identifier.attr0=doi",
		"field_identifier.attr0=uri",
		"field_identifier.attr0=call-number",
		"field_identifier.attr0=report-number",
	}
	if identifierGroup == nil || !identifierGroup.IsRequired(OperationCreate) || identifierGroup.IsRequired(OperationUpdate) || !equalStrings(identifierGroup.Fields, wantIdentifierFields) {
		t.Fatalf("required identifier group = %#v, want create-only group over %v", identifierGroup, wantIdentifierFields)
	}
	identifierTarget, ok := transformation.TargetField("field_identifier")
	if !ok || !identifierTarget.Required || identifierTarget.SchemaLabel != "Identifier" || identifierTarget.Description == "" || identifierTarget.SourceType != "typed_relation" || identifierTarget.Cardinality != 0 {
		t.Fatalf("target aggregate did not retain Drupal required policy: %#v", identifierTarget)
	}
	tags, ok := transformation.SourceField("field_custom_tags")
	if !ok || tags.Hub != "Extra.drupal.field_custom_tags" || tags.Cardinality != 0 || tags.Codec != "multi" {
		t.Fatalf("generic unlimited field = %#v, found = %v", tags, ok)
	}
	rating, ok := transformation.TargetField("field_rating")
	if !ok || rating.Cardinality != 3 || rating.Codec != "integer" {
		t.Fatalf("generic bounded field = %#v, found = %v", rating, ok)
	}
	localCode, ok := transformation.SourceField("field_local_code")
	if !ok || localCode.Cardinality != 1 || localCode.Codec != "string" || localCode.Hub != "Extra.drupal.field_local_code" {
		t.Fatalf("generic scalar field = %#v, found = %v", localCode, ok)
	}
	if len(transformation.Defaults) != 0 {
		t.Fatalf("compiled transformation guessed institution-specific Workbench defaults: %#v", transformation.Defaults)
	}
	for _, operational := range []string{"id", "parent_id", "node_id", "file", "title", "published", "url_alias"} {
		if _, ok := transformation.SourceField(operational); !ok {
			t.Errorf("compiled source omitted Workbench operational field %q", operational)
		}
	}
	if _, ok := transformation.SourceField("field_genre"); ok {
		t.Error("compiled source retained field_genre although it is not attached to the bundle")
	}
	if err := transformation.Validate(); err != nil {
		t.Fatalf("compiled transformation Validate() error = %v", err)
	}
	again, err := CompileDrupalDirectory(filepath.Join("testdata", "drupal"), DrupalCompileOptions{Bundle: "islandora_object"})
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(transformation)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("identical Drupal directory produced nondeterministic specification output")
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestCompileDrupalArchiveIsDeterministicAcrossWrapperDirectories(t *testing.T) {
	first := fixtureArchive(t, "export-a/config/sync")
	second := fixtureArchive(t, "another-wrapper")
	options := DrupalCompileOptions{Bundle: "islandora_object"}
	one, err := CompileDrupalArchive(bytes.NewReader(first), options)
	if err != nil {
		t.Fatalf("first CompileDrupalArchive() error = %v", err)
	}
	two, err := CompileDrupalArchive(bytes.NewReader(second), options)
	if err != nil {
		t.Fatalf("second CompileDrupalArchive() error = %v", err)
	}
	if one.Fingerprint != two.Fingerprint {
		t.Fatalf("archive wrapper changed fingerprint: %#v != %#v", one.Fingerprint, two.Fingerprint)
	}
	if one.Fingerprint.Value == "" {
		t.Fatal("compiled archive is not fingerprinted")
	}
}

func TestCompileDrupalArchiveRejectsUnsafeAndOversizedMembers(t *testing.T) {
	tests := []struct {
		name    string
		archive []tarEntry
		options DrupalCompileOptions
		want    string
	}{
		{name: "traversal", archive: []tarEntry{{name: "../system.site.yml", data: "name: Bad\n"}}, want: "unsafe"},
		{name: "oversized", archive: []tarEntry{{name: "config/system.site.yml", data: strings.Repeat("x", 65)}}, options: DrupalCompileOptions{MaxFileBytes: 64}, want: "exceeds 64 bytes"},
		{name: "oversized irrelevant member", archive: []tarEntry{{name: "config/database.dump", data: strings.Repeat("x", 65)}}, options: DrupalCompileOptions{MaxFileBytes: 64}, want: "exceeds 64 bytes"},
		{name: "excessive irrelevant total", archive: []tarEntry{{name: "one.txt", data: strings.Repeat("x", 40)}, {name: "two.txt", data: strings.Repeat("x", 40)}}, options: DrupalCompileOptions{MaxFileBytes: 64, MaxBytes: 64}, want: "exceeds 64 uncompressed bytes"},
		{name: "too many irrelevant members", archive: []tarEntry{{name: "one.txt"}, {name: "two.txt"}}, options: DrupalCompileOptions{MaxFiles: 1}, want: "exceeds 1 members"},
		{name: "duplicate config", archive: []tarEntry{{name: "one/system.site.yml", data: "name: One\n"}, {name: "two/system.site.yml", data: "name: Two\n"}}, want: "duplicate config"},
		{name: "link", archive: []tarEntry{{name: "config/system.site.yml", kind: tar.TypeSymlink}}, want: "not a regular file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.options.Bundle = "islandora_object"
			_, err := CompileDrupalArchive(bytes.NewReader(makeArchive(t, test.archive)), test.options)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CompileDrupalArchive() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCompileDrupalReportsMissingBundleAndStorage(t *testing.T) {
	_, err := CompileDrupalDirectory(filepath.Join("testdata", "drupal"), DrupalCompileOptions{Bundle: "missing"})
	if err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("missing bundle error = %v", err)
	}

	dir := t.TempDir()
	data, readErr := os.ReadFile(filepath.Join("testdata", "drupal", "field.field.node.islandora_object.field_custom_tags.yml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, "field.field.node.islandora_object.field_custom_tags.yml"), data, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	_, err = CompileDrupalDirectory(dir, DrupalCompileOptions{Bundle: "islandora_object"})
	if err == nil || !strings.Contains(err.Error(), "has no field.storage") {
		t.Fatalf("missing storage error = %v", err)
	}
}

type tarEntry struct {
	name string
	data string
	kind byte
}

func fixtureArchive(t *testing.T, prefix string) []byte {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", "drupal"))
	if err != nil {
		t.Fatal(err)
	}
	archive := make([]tarEntry, 0, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join("testdata", "drupal", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		archive = append(archive, tarEntry{name: prefix + "/" + entry.Name(), data: string(data)})
	}
	return makeArchive(t, archive)
}

func makeArchive(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Mode: 0o600, Size: int64(len(entry.data)), Typeflag: kind}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := tarWriter.Write([]byte(entry.data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
