// Package islandora_workbench implements the Islandora Workbench CSV format.
//
// Islandora Workbench is a tool for creating, updating, and deleting Islandora
// content via CSV files. This format serializes hub records to Workbench CSV.
//
// Key characteristics:
//   - Multi-value separator is "|" (pipe)
//   - Column names are Drupal field names (e.g., field_linked_agent)
//   - Contributors with extra metadata (orcid, email, status, institution)
//     generate rows in a separate agents CSV, written to ExtraWriters["agents"]
//   - Reserved columns: id, parent_id, node_id, file, title, url_alias, etc.
package islandora_workbench

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Format implements the Islandora Workbench CSV format.
type Format struct{}

// Name returns the format identifier.
func (f *Format) Name() string { return "islandora-workbench" }

// Description returns a human-readable format description.
func (f *Format) Description() string { return "Islandora Workbench CSV import format" }

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string { return []string{"csv"} }

// CanParse returns true if the content looks like a Workbench CSV file.
// Workbench CSVs have "id" or "node_id" as the first column.
func (f *Format) CanParse(peek []byte) bool {
	firstLine := bytes.SplitN(peek, []byte("\n"), 2)[0]
	firstLine = bytes.ToLower(bytes.TrimSpace(firstLine))
	return bytes.HasPrefix(firstLine, []byte("id,")) ||
		bytes.HasPrefix(firstLine, []byte("node_id,"))
}

func init() {
	format.Register(&Format{})
}
