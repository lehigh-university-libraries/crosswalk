package cmd

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/spec"
	"github.com/spf13/cobra"
)

func TestDOIInputsCombinesAndDeduplicates(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "dois.txt")
	if err := os.WriteFile(path, []byte("10.1234/two\n10.1234/THREE\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := doiInputs([]string{"10.1234/one", "10.1234/two", "10.1234/three"}, path, 1000)
	if err != nil {
		t.Fatalf("doiInputs() error = %v", err)
	}
	want := []string{"10.1234/one", "10.1234/two", "10.1234/three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("doiInputs() = %#v, want %#v", got, want)
	}
}

func TestPrepareFetchedWorkbenchRecordsAddsOperationalMetadata(t *testing.T) {
	t.Parallel()

	fullTitle := strings.Repeat("Long title ", 30)
	record := &hubv1.Record{
		Title:      fullTitle,
		SourceInfo: &hubv1.SourceInfo{SourceId: "10.1234/example"},
	}
	prepareFetchedWorkbenchRecords([]*hubv1.Record{record})
	if record.FullTitle != fullTitle {
		t.Fatalf("FullTitle = %q, want original title", record.FullTitle)
	}
	if len([]rune(record.Title)) != 255 {
		t.Fatalf("Title rune length = %d, want 255", len([]rune(record.Title)))
	}
	if record.ObjectModel != "Digital Document" {
		t.Fatalf("ObjectModel = %q", record.ObjectModel)
	}
	if got := hub.GetExtraString(record, "id"); got != "" {
		t.Fatalf("id = %q, want no operational ID derived from SourceInfo", got)
	}
	if record.SourceInfo.SourceId != "10.1234/example" {
		t.Fatalf("SourceInfo.SourceId = %q", record.SourceInfo.SourceId)
	}
}

func TestWriteFetchedRecordsKeepsSourceIdentifiersOutOfOperationalID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceID   string
		identifier hubv1.IdentifierType
		attribute  string
		existingID string
	}{
		{name: "DOI", sourceID: "10.1234/example", identifier: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, attribute: "doi"},
		{name: "arXiv", sourceID: "2401.01234", identifier: hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV, attribute: "arxiv"},
		{name: "existing numeric Workbench ID", sourceID: "10.1234/existing", identifier: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, attribute: "doi", existingID: "42"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			record := &hubv1.Record{
				Title:       "Fetched work",
				SourceInfo:  &hubv1.SourceInfo{SourceId: test.sourceID},
				Identifiers: []*hubv1.Identifier{{Type: test.identifier, Value: test.sourceID}},
			}
			if test.existingID != "" {
				hub.SetExtra(record, "id", test.existingID)
			}

			var output bytes.Buffer
			command := &cobra.Command{}
			command.SetOut(&output)
			if err := writeFetchedRecords(command, []*hubv1.Record{record}, fetchOutputOptions{
				format:    "islandora-workbench",
				separator: "|",
			}, reconcileCommandOptions{}); err != nil {
				t.Fatalf("writeFetchedRecords() error = %v", err)
			}
			rows, err := csv.NewReader(&output).ReadAll()
			if err != nil {
				t.Fatalf("read Workbench CSV: %v\n%s", err, output.String())
			}
			if len(rows) != 2 {
				t.Fatalf("Workbench rows = %v", rows)
			}

			idIndex := csvHeaderIndex(rows[0], "id")
			if test.existingID == "" {
				if idIndex >= 0 && rows[1][idIndex] != "" {
					t.Fatalf("Workbench id = %q, want empty operational ID", rows[1][idIndex])
				}
			} else {
				if idIndex < 0 {
					t.Fatalf("Workbench header = %v, want id", rows[0])
				}
				if _, err := strconv.ParseUint(rows[1][idIndex], 10, 64); err != nil {
					t.Fatalf("Workbench id = %q, want unsigned integer: %v", rows[1][idIndex], err)
				}
				if rows[1][idIndex] != test.existingID {
					t.Fatalf("Workbench id = %q, want %q", rows[1][idIndex], test.existingID)
				}
			}

			identifierIndex := csvHeaderIndex(rows[0], "field_identifier")
			if identifierIndex < 0 {
				t.Fatalf("Workbench header = %v, want field_identifier", rows[0])
			}
			wantIdentifier := `{"value":"` + test.sourceID + `","attr0":"` + test.attribute + `"}`
			if got := rows[1][identifierIndex]; got != wantIdentifier {
				t.Fatalf("field_identifier = %q, want %q", got, wantIdentifier)
			}
			if record.SourceInfo.SourceId != test.sourceID || len(record.Identifiers) != 1 || record.Identifiers[0].Value != test.sourceID {
				t.Fatalf("source metadata changed: SourceInfo=%+v Identifiers=%+v", record.SourceInfo, record.Identifiers)
			}
		})
	}
}

func csvHeaderIndex(header []string, name string) int {
	for index, value := range header {
		if value == name {
			return index
		}
	}
	return -1
}

func TestDOIInputsRequiresValue(t *testing.T) {
	t.Parallel()
	if _, err := doiInputs(nil, "", 1000); err == nil {
		t.Fatal("doiInputs() error = nil")
	}
}

func TestDOIInputsEnforcesUniqueRecordLimit(t *testing.T) {
	t.Parallel()
	if _, err := doiInputs([]string{"10.1234/one", "10.1234/one", "10.1234/two"}, "", 1); err == nil || !strings.Contains(err.Error(), "--max-records 1") {
		t.Fatalf("doiInputs() limit error = %v", err)
	}
	if _, err := doiInputs([]string{"10.1234/one"}, "", 0); err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Fatalf("doiInputs() invalid-limit error = %v", err)
	}
}

func TestReconcileFetchedRecordsAssumeNewDetectsBatchDuplicates(t *testing.T) {
	t.Parallel()
	records := []*hubv1.Record{
		{Title: "Example", Identifiers: []*hubv1.Identifier{{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.1234/example"}}},
		{Title: "Example", Identifiers: []*hubv1.Identifier{{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.1234/example"}}},
	}
	command := &cobra.Command{}
	command.SetContext(t.Context())
	command.SetErr(&bytes.Buffer{})
	_, err := reconcileFetchedRecords(command, records, reconcileCommandOptions{mode: "assume-new"})
	if err == nil || !strings.Contains(err.Error(), "review required") {
		t.Fatalf("reconcileFetchedRecords() error = %v", err)
	}
}

func TestReconcileFetchedRecordsWritesReviewBeforeHolding(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	reviewPath := filepath.Join(directory, "review.csv")
	reportPath := filepath.Join(directory, "report.json")
	records := []*hubv1.Record{
		{Title: "Example", Identifiers: []*hubv1.Identifier{{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.1234/example"}}},
		{Title: "Example", Identifiers: []*hubv1.Identifier{{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.1234/example"}}},
	}
	command := &cobra.Command{}
	command.SetContext(t.Context())
	command.SetErr(&bytes.Buffer{})
	_, err := reconcileFetchedRecords(command, records, reconcileCommandOptions{mode: "assume-new", reviewCSVPath: reviewPath, reportPath: reportPath})
	if err == nil {
		t.Fatal("expected hold error")
	}
	review, readErr := os.ReadFile(reviewPath)
	if readErr != nil || !bytes.Contains(review, []byte("duplicate")) {
		t.Fatalf("review = %q err=%v", review, readErr)
	}
	report, readErr := os.ReadFile(reportPath)
	if readErr != nil || !bytes.Contains(report, []byte(`"duplicate": 1`)) {
		t.Fatalf("report = %q err=%v", report, readErr)
	}
}

func TestFetchCommandsExposeExistingItemControls(t *testing.T) {
	t.Parallel()
	command := newFetchCmd()
	for _, name := range []string{"doi", "arxiv", "crossref", "wos", "scopus", "zenodo", "proquest"} {
		subcommand, _, err := command.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		for _, flag := range []string{"existing", "drupal-jsonapi", "drupal-profile", "match-report", "match-review", "spec"} {
			if subcommand.Flags().Lookup(flag) == nil {
				t.Errorf("fetch %s missing --%s", name, flag)
			}
		}
		if got := subcommand.Flags().Lookup("existing").DefValue; got != "hold" {
			t.Errorf("fetch %s --existing default = %q, want hold", name, got)
		}
	}
}

func TestReconcileDefaultsToFailClosedRepositoryLookup(t *testing.T) {
	t.Parallel()
	_, err := validateReconcileCommandOptions(reconcileCommandOptions{})
	if err == nil || !strings.Contains(err.Error(), "--drupal-jsonapi is required") {
		t.Fatalf("validateReconcileCommandOptions() error = %v", err)
	}
	mode, err := validateReconcileCommandOptions(reconcileCommandOptions{mode: "assume-new"})
	if err != nil || mode != "assume-new" {
		t.Fatalf("assume-new validation = %q, %v", mode, err)
	}
	_, err = validateReconcileCommandOptions(reconcileCommandOptions{
		mode: "hold", drupalJSONAPI: "https://repository.example.edu/jsonapi",
	})
	if err == nil || !strings.Contains(err.Error(), "--drupal-profile is required") {
		t.Fatalf("profile-less Drupal validation error = %v", err)
	}
}

func TestChunkArxivIdentifierSelectorsDoesNotTruncate(t *testing.T) {
	t.Parallel()
	identifiers := []string{"1", "2", "3", "4", "5"}
	selectors := chunkArxivIdentifierSelectors(identifiers, 2)
	if len(selectors) != 3 {
		t.Fatalf("selectors = %#v", selectors)
	}
	var got []string
	for _, selector := range selectors {
		if len(selector.identifiers) > 2 {
			t.Fatalf("chunk exceeds page size: %#v", selector)
		}
		got = append(got, selector.identifiers...)
	}
	if !reflect.DeepEqual(got, identifiers) {
		t.Fatalf("chunked IDs = %#v, want %#v", got, identifiers)
	}
}

func TestRecordIdentityDoesNotCollapseMetadataOnlyWorks(t *testing.T) {
	t.Parallel()
	if got := recordIdentity(&hubv1.Record{Title: "A shared title"}); got != "" {
		t.Fatalf("recordIdentity(metadata-only) = %q, want empty", got)
	}
	got := recordIdentity(&hubv1.Record{Identifiers: []*hubv1.Identifier{{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.1234/Example"}}})
	if got == "" {
		t.Fatal("recordIdentity(DOI) is empty")
	}
}

func TestRecordIdentityUsesAuthorityScopedOpenSchemes(t *testing.T) {
	t.Parallel()
	registry := hub.DefaultIdentifierRegistry()
	tests := []struct {
		name   string
		value  string
		scheme string
		level  hubv1.IdentifierIdentityLevel
		want   bool
	}{
		{name: "Scopus EID", value: "2-s2.0-85123456789", scheme: "scopus-eid", level: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD, want: true},
		{name: "Zenodo record", value: "8435696", scheme: "zenodo-record", level: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_SOURCE_RECORD, want: true},
		{name: "Zenodo concept", value: "8435695", scheme: "zenodo-concept", level: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT, want: false},
		{name: "concept DOI", value: "10.5281/zenodo.8435695", scheme: "doi", level: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_CONCEPT, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identifier, err := registry.NewIdentifierForScheme(test.value, test.scheme, test.level)
			if err != nil {
				t.Fatal(err)
			}
			got := recordIdentity(&hubv1.Record{Identifiers: []*hubv1.Identifier{identifier}}) != ""
			if got != test.want {
				t.Fatalf("recordIdentity() present = %t, want %t", got, test.want)
			}
		})
	}
}

func TestResolveFetchMediaDirectoryUsesWorkbenchStagingContract(t *testing.T) {
	t.Parallel()

	output := fetchOutputOptions{format: "islandora-workbench"}
	got, err := resolveFetchMediaDirectory("scholarship/pdfs", output)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.FromSlash("/mnt/islandora_staging/scholarship/pdfs"); got != want {
		t.Fatalf("resolved media directory = %q, want %q", got, want)
	}

	got, err = resolveFetchMediaDirectory("/home/islandora/import", output)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.FromSlash("/home/islandora/import"); got != want {
		t.Fatalf("allowlisted media directory = %q, want %q", got, want)
	}

	if _, err := resolveFetchMediaDirectory("../escape", output); err == nil {
		t.Fatal("relative traversal was accepted")
	}
}

func TestResolveFetchMediaDirectoryUsesExplicitSpecification(t *testing.T) {
	t.Parallel()
	transformation := spec.FabricatorWorkbench()
	transformation.Defaults[spec.FileStagingRootDefault] = "/srv/context/staging"
	transformation.Defaults[spec.FileAllowedAbsoluteRootsDefault] = "/srv/context/staging"
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	output := fetchOutputOptions{format: "islandora-workbench", transformation: transformation}
	got, err := resolveFetchMediaDirectory("scholarship/pdfs", output)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.FromSlash("/srv/context/staging/scholarship/pdfs"); got != want {
		t.Fatalf("resolved media directory = %q, want %q", got, want)
	}
}

func TestResolveFetchTransformationRejectsSpecForAnotherTarget(t *testing.T) {
	t.Parallel()
	output := fetchOutputOptions{format: "csl-json", specPath: "mapping.yaml"}
	if err := resolveFetchTransformation(&output, reconcileCommandOptions{}); err == nil || !strings.Contains(err.Error(), "requires --to islandora-workbench") {
		t.Fatalf("resolveFetchTransformation() error = %v", err)
	}
}

func TestEmitExistingItemReviewNeverSilentlySkips(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	command := &cobra.Command{}
	command.SetErr(&stderr)
	location, err := emitExistingItemReview(command, []byte("verdict,repository_id\nduplicate,42\n"), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "duplicate,42") || !strings.Contains(location, "--match-review") {
		t.Fatalf("stderr = %q location = %q", stderr.String(), location)
	}
}
