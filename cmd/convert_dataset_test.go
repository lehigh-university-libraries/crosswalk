package cmd

import (
	"bytes"
	"encoding/csv"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertArchivesSpaceSnapshotPreservesWorkbenchHierarchy(t *testing.T) {
	options := defaultConvertOptions()
	options.inputPath = filepath.Join("..", "format", "archivesspace", "testdata", "snapshot.json")

	var output bytes.Buffer
	var diagnostics bytes.Buffer
	command := newConvertCmd()
	command.SetOut(&output)
	command.SetErr(&diagnostics)
	if err := runConvert(command, []string{"archivesspace", "islandora-workbench"}, options); err != nil {
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

func TestNewConvertCmdKeepsFlagStatePerCommand(t *testing.T) {
	first := newConvertCmd()
	if err := first.Flags().Set("separator", ";"); err != nil {
		t.Fatal(err)
	}
	if err := first.Flags().Set("input", "first.json"); err != nil {
		t.Fatal(err)
	}

	second := newConvertCmd()
	separator, err := second.Flags().GetString("separator")
	if err != nil {
		t.Fatal(err)
	}
	input, err := second.Flags().GetString("input")
	if err != nil {
		t.Fatal(err)
	}
	if separator != "|" || input != "" {
		t.Fatalf("fresh convert command inherited separator %q or input %q", separator, input)
	}
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
