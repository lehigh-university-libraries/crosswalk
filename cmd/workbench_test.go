package cmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/spec"
)

func TestWorkbenchValidateAcceptsSanitizedDirectCSVFixtures(t *testing.T) {
	for _, name := range []string{"etd-create", "create", "update", "add-media"} {
		t.Run(name, func(t *testing.T) {
			command := newWorkbenchValidateCmd()
			var stdout, stderr bytes.Buffer
			command.SetOut(&stdout)
			command.SetErr(&stderr)
			command.SetArgs([]string{"--input", workbenchFixture(name + ".csv")})

			if err := command.Execute(); err != nil {
				t.Fatalf("workbench validate: %v\nstderr: %s", err, stderr.String())
			}
			if got := stdout.String(); got != "Valid: no Workbench metadata findings\n" {
				t.Fatalf("stdout = %q", got)
			}
		})
	}
}

func TestWorkbenchTransformMatchesCompleteGoldenBundles(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		unexpected  string
		artifactNum int
	}{
		{name: "etd-create", target: "target.csv", unexpected: "target.update.csv", artifactNum: 3},
		{name: "create", target: "target.csv", unexpected: "target.update.csv", artifactNum: 2},
		{name: "update", target: "target.update.csv", unexpected: "target.csv", artifactNum: 2},
		{name: "add-media", target: "target.add_media.csv", unexpected: "target.csv", artifactNum: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "artifacts")
			stdout, stderr, err := executeWorkbenchTransform(workbenchFixture(test.name+".csv"), output, "")
			if err != nil {
				t.Fatalf("workbench transform: %v\nstderr: %s", err, stderr)
			}
			if !strings.Contains(stdout, "Wrote "+string(rune('0'+test.artifactNum))+" Workbench artifacts") {
				t.Fatalf("stdout = %q", stdout)
			}
			if _, err := os.Stat(filepath.Join(output, test.target)); err != nil {
				t.Fatalf("expected routed target %q: %v", test.target, err)
			}
			if _, err := os.Stat(filepath.Join(output, test.unexpected)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected routed target %q exists or cannot be checked: %v", test.unexpected, err)
			}
			assertWorkbenchDirectoriesEqual(t, workbenchFixture("golden", test.name), output)

			second := filepath.Join(t.TempDir(), "artifacts")
			if _, secondStderr, err := executeWorkbenchTransform(workbenchFixture(test.name+".csv"), second, ""); err != nil {
				t.Fatalf("second deterministic transform: %v\nstderr: %s", err, secondStderr)
			}
			assertWorkbenchDirectoriesEqual(t, output, second)
		})
	}
}

func TestWorkbenchTransformMatchesFabricatorReferenceFields(t *testing.T) {
	output := filepath.Join(t.TempDir(), "artifacts")
	if _, stderr, err := executeWorkbenchTransform(workbenchFixture("fabricator-create.csv"), output, ""); err != nil {
		t.Fatalf("workbench transform: %v\nstderr: %s", err, stderr)
	}
	fabricator := readWorkbenchFixtureCSV(t, workbenchFixture("fabricator-create.golden.csv"))
	crosswalk := readWorkbenchFixtureCSV(t, filepath.Join(output, "target.csv"))
	fabricatorRow := workbenchCSVRow(t, fabricator)
	crosswalkRow := workbenchCSVRow(t, crosswalk)
	for field, want := range fabricatorRow {
		if got := crosswalkRow[field]; got != want {
			t.Errorf("canonical field %q = %q, want Fabricator reference %q", field, got, want)
		}
	}
}

func TestWorkbenchETDTransformMatchesFabricatorGoldenFields(t *testing.T) {
	output := filepath.Join(t.TempDir(), "artifacts")
	if _, stderr, err := executeWorkbenchTransform(workbenchFixture("etd-create.csv"), output, ""); err != nil {
		t.Fatalf("workbench ETD transform: %v\nstderr: %s", err, stderr)
	}
	fabricator := workbenchCSVRow(t, readWorkbenchFixtureCSV(t, workbenchFixture("etd-create.fabricator.golden.csv")))
	crosswalk := workbenchCSVRow(t, readWorkbenchFixtureCSV(t, filepath.Join(output, "target.csv")))
	// Fabricator resolves contributors by mutating/querying Drupal and preserves
	// HTML in its abstract JSON. Crosswalk intentionally emits agents.csv for a
	// later guarded operation and normalizes prose, so those two fields have
	// separate assertions below. Every other Fabricator ETD field is the golden
	// compatibility contract for this XML-derived CSV.
	for field, want := range fabricator {
		if field == "field_linked_agent" || field == "field_abstract" {
			continue
		}
		if got := crosswalk[field]; got != want {
			t.Errorf("ETD canonical field %q = %q, want Fabricator golden %q", field, got, want)
		}
	}
	if !strings.Contains(crosswalk["field_linked_agent"], "relators:cre:person:Example, Alex - Example University") {
		t.Fatalf("Crosswalk contributor plan = %q", crosswalk["field_linked_agent"])
	}
	agents := readWorkbenchFixtureCSV(t, filepath.Join(output, "agents.csv"))
	if len(agents) != 3 {
		t.Fatalf("agents.csv rows = %d, want header plus two agents", len(agents))
	}
}

func TestWorkbenchTransformRejectsInvalidCSVWithoutPublishing(t *testing.T) {
	tests := []struct {
		name    string
		finding string
	}{
		{name: "invalid-ambiguous-update", finding: "field does not apply to update operations"},
		{name: "invalid-date", finding: "must be a valid EDTF date"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "artifacts")
			_, stderr, err := executeWorkbenchTransform(workbenchFixture(test.name+".csv"), output, "")
			var validationErr *workbenchValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("workbench transform error = %v, want validation error", err)
			}
			if validationErr.count == 0 || !strings.Contains(stderr, test.finding) {
				t.Fatalf("validation result = count %d, stderr %q", validationErr.count, stderr)
			}
			if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("invalid transform published output directory: %v", statErr)
			}
		})
	}
}

func TestWorkbenchValidationUsesCanonicalMappingsAfterHumanLabelsChange(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	aliases := map[string]struct {
		label string
		alias string
	}{
		"field_model":            {label: "Content Kind", alias: "Repository Model"},
		"title":                  {label: "Display Heading", alias: "Public Heading"},
		"field_full_title":       {label: "Complete Heading", alias: "Unabridged Heading"},
		"field_resource_type":    {label: "Material Kind", alias: "Descriptive Type"},
		"field_edtf_date_issued": {label: "Date Made", alias: "Human Date"},
	}
	for index := range transformation.Source.Fields {
		field := &transformation.Source.Fields[index]
		change, ok := aliases[field.Name]
		if !ok {
			continue
		}
		field.Label = change.label
		field.Aliases = []string{change.alias}
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(transformation, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(t.TempDir(), "renamed-labels.json")
	if err := os.WriteFile(specPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	command := newWorkbenchValidateCmd()
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader("Upload ID,Repository Model,Public Heading,Unabridged Heading,Descriptive Type,Human Date\n1,Digital Document,Label-independent validation,Label-independent validation,Report,2026\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--spec", specPath})
	if err := command.Execute(); err != nil {
		t.Fatalf("workbench validate with renamed labels: %v\nstderr: %s", err, stderr.String())
	}
	if got := stdout.String(); got != "Valid: no Workbench metadata findings\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func executeWorkbenchTransform(input, output, specPath string) (string, string, error) {
	command := newWorkbenchTransformCmd()
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	args := []string{"--input", input, "--artifact-dir", output}
	if specPath != "" {
		args = append(args, "--spec", specPath)
	}
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}

func workbenchFixture(parts ...string) string {
	return filepath.Join(append([]string{"testdata", "workbench"}, parts...)...)
}

func assertWorkbenchDirectoriesEqual(t *testing.T, wantDirectory, gotDirectory string) {
	t.Helper()
	wantNames := workbenchDirectoryFiles(t, wantDirectory)
	gotNames := workbenchDirectoryFiles(t, gotDirectory)
	if !slices.Equal(wantNames, gotNames) {
		t.Fatalf("artifact names = %v, want %v", gotNames, wantNames)
	}
	for _, name := range wantNames {
		want, err := os.ReadFile(filepath.Join(wantDirectory, name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(gotDirectory, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("artifact %s differs\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
		}
	}
}

func workbenchDirectoryFiles(t *testing.T, directory string) []string {
	t.Helper()
	files := make([]string, 0)
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		files = append(files, name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}

func readWorkbenchFixtureCSV(t *testing.T, path string) [][]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func workbenchCSVRow(t *testing.T, rows [][]string) map[string]string {
	t.Helper()
	if len(rows) != 2 || len(rows[0]) != len(rows[1]) {
		t.Fatalf("expected one rectangular CSV data row, got %#v", rows)
	}
	values := make(map[string]string, len(rows[0]))
	for index, field := range rows[0] {
		values[field] = rows[1][index]
	}
	return values
}
