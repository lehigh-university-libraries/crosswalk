// Package archivesspace parses ArchivesSpace JSONModel records and static
// resource-tree snapshots into Crosswalk Hub datasets.
package archivesspace

import (
	"bytes"
	"encoding/json"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

const (
	// Version identifies the ArchivesSpace JSONModel representation supported by
	// this adapter.
	Version = "jsonmodel-v1"

	// SnapshotFormat identifies Crosswalk's deterministic envelope for a set of
	// ArchivesSpace JSONModel records and their resource ordering metadata.
	SnapshotFormat = "archivesspace-jsonmodel"

	// SnapshotVersion is the current ArchivesSpace static snapshot contract.
	SnapshotVersion = 1
)

// Format implements static ArchivesSpace JSON parsing. API authentication,
// pagination, record resolution, and tree acquisition belong to the caller.
type Format struct{}

var (
	_ format.Parser        = (*Format)(nil)
	_ format.DatasetParser = (*Format)(nil)
)

// Name returns the format identifier.
func (*Format) Name() string { return "archivesspace" }

// Description returns a human-readable format description.
func (*Format) Description() string {
	return "ArchivesSpace JSONModel record or resource-tree snapshot"
}

// Extensions returns the associated file extensions.
func (*Format) Extensions() []string { return []string{"json"} }

// CanParse reports whether input resembles a supported ArchivesSpace JSON
// response or Crosswalk ArchivesSpace snapshot.
func (*Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 || (peek[0] != '{' && peek[0] != '[') {
		return false
	}
	if peek[0] == '[' {
		return canParseRecordArray(peek)
	}
	return canParseArchivesSpaceObject(peek)
}

func canParseRecordArray(raw []byte) bool {
	var records []json.RawMessage
	if err := json.Unmarshal(raw, &records); err != nil || len(records) == 0 {
		return false
	}
	for _, record := range records {
		if _, ok := supportedRecordType(record); !ok {
			return false
		}
	}
	return true
}

func canParseArchivesSpaceObject(raw []byte) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return false
	}
	if marker, exists := object["crosswalk_format"]; exists {
		var value string
		return json.Unmarshal(marker, &value) == nil && value == SnapshotFormat
	}
	if modelType, ok := jsonModelType(object); ok {
		switch modelType {
		case "resource", "archival_object", "resource_ordered_records":
			return true
		default:
			return false
		}
	}
	return canParseSearchResults(object["results"])
}

func canParseSearchResults(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var results []searchResult
	if err := json.Unmarshal(raw, &results); err != nil || len(results) == 0 {
		return false
	}
	for _, result := range results {
		if result.PrimaryType != "resource" && result.PrimaryType != "archival_object" {
			return false
		}
		var serialized string
		if err := json.Unmarshal(result.JSON, &serialized); err != nil {
			return false
		}
		modelType, ok := supportedRecordType([]byte(serialized))
		if !ok || modelType != result.PrimaryType {
			return false
		}
	}
	return true
}

func supportedRecordType(raw []byte) (string, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(raw), &object); err != nil {
		return "", false
	}
	modelType, ok := jsonModelType(object)
	return modelType, ok && (modelType == "resource" || modelType == "archival_object")
}

func jsonModelType(object map[string]json.RawMessage) (string, bool) {
	raw, exists := object["jsonmodel_type"]
	if !exists {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return "", false
	}
	return value, true
}

func init() { format.MustRegister(&Format{}) }
