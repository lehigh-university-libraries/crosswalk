package cmd

import (
	"bytes"
	"encoding/csv"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConvertArchivesSpaceSnapshotPreservesWorkbenchHierarchy(t *testing.T) {
	state := captureConvertState()
	t.Cleanup(state.restore)

	inputFile = filepath.Join("..", "format", "archivesspace", "testdata", "snapshot.json")
	outputFile = ""
	sourceProfileName = ""
	targetProfileName = ""
	taxonomyFile = ""
	columns = nil
	multiValueSep = "|"
	stripHTML = true
	pretty = false
	baseURL = ""
	referenceDOIs = nil
	skipReferenceDOIValidation = false
	transformationSpecFile = ""

	var output bytes.Buffer
	var diagnostics bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&output)
	command.SetErr(&diagnostics)
	if err := runConvert(command, []string{"archivesspace", "islandora-workbench"}); err != nil {
		t.Fatalf("runConvert() error = %v", err)
	}
	if !strings.Contains(diagnostics.String(), "Parsed 5 records") {
		t.Fatalf("command diagnostics = %q", diagnostics.String())
	}

	rows, err := csv.NewReader(bytes.NewReader(output.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("reading Workbench CSV: %v\n%s", err, output.String())
	}
	if len(rows) != 6 {
		t.Fatalf("Workbench rows = %d, want 6", len(rows))
	}
	want := map[string][3]string{
		"The Example Family Papers":   {"1", "", "0"},
		"Professional correspondence": {"2", "1", "0"},
		"Project files":               {"3", "2", "0"},
		"Blueprint and notes":         {"4", "3", "0"},
		"Photographs":                 {"5", "1", "1"},
	}
	for _, row := range rows[1:] {
		title := commandCSVValue(t, rows[0], row, "title")
		got := [3]string{
			commandCSVValue(t, rows[0], row, "id"),
			commandCSVValue(t, rows[0], row, "parent_id"),
			commandCSVValue(t, rows[0], row, "field_weight"),
		}
		if got != want[title] {
			t.Errorf("record %q operational hierarchy = %v, want %v", title, got, want[title])
		}
	}
}

type convertState struct {
	inputFile                  string
	outputFile                 string
	sourceProfileName          string
	targetProfileName          string
	taxonomyFile               string
	columns                    []string
	multiValueSep              string
	stripHTML                  bool
	pretty                     bool
	baseURL                    string
	referenceDOIs              []string
	skipReferenceDOIValidation bool
	transformationSpecFile     string
}

func captureConvertState() convertState {
	return convertState{
		inputFile: inputFile, outputFile: outputFile,
		sourceProfileName: sourceProfileName, targetProfileName: targetProfileName,
		taxonomyFile: taxonomyFile, columns: append([]string(nil), columns...),
		multiValueSep: multiValueSep, stripHTML: stripHTML, pretty: pretty,
		baseURL:                    baseURL,
		referenceDOIs:              append([]string(nil), referenceDOIs...),
		skipReferenceDOIValidation: skipReferenceDOIValidation,
		transformationSpecFile:     transformationSpecFile,
	}
}

func (state convertState) restore() {
	inputFile = state.inputFile
	outputFile = state.outputFile
	sourceProfileName = state.sourceProfileName
	targetProfileName = state.targetProfileName
	taxonomyFile = state.taxonomyFile
	columns = state.columns
	multiValueSep = state.multiValueSep
	stripHTML = state.stripHTML
	pretty = state.pretty
	baseURL = state.baseURL
	referenceDOIs = state.referenceDOIs
	skipReferenceDOIValidation = state.skipReferenceDOIValidation
	transformationSpecFile = state.transformationSpecFile
}

func commandCSVValue(t *testing.T, header, row []string, column string) string {
	t.Helper()
	for index, candidate := range header {
		if candidate == column {
			return row[index]
		}
	}
	t.Fatalf("column %q not found in %v", column, header)
	return ""
}
