// Package zenodo parses Zenodo Records API JSON into Crosswalk Hub records.
package zenodo

import (
	"bytes"
	"encoding/json"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version is the Zenodo published-record representation implemented here.
const Version = "records-api"

// Format implements the Zenodo Records API JSON format.
type Format struct{}

var _ format.Parser = (*Format)(nil)

// Name returns the format identifier.
func (*Format) Name() string { return "zenodo" }

// Description returns a human-readable format description.
func (*Format) Description() string { return "Zenodo Records API JSON" }

// Extensions returns the associated file extensions.
func (*Format) Extensions() []string { return []string{"json"} }

// CanParse reports whether input resembles a Zenodo record or records page.
func (*Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 || peek[0] != '{' {
		return false
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(peek, &envelope); err != nil {
		return false
	}
	if _, hasMetadata := envelope["metadata"]; hasMetadata {
		for _, key := range []string{"id", "record_id", "recid", "doi", "conceptrecid", "conceptdoi"} {
			if _, identified := envelope[key]; identified {
				return true
			}
		}
	}
	rawHits, exists := envelope["hits"]
	if !exists {
		return false
	}
	var page map[string]json.RawMessage
	if err := json.Unmarshal(rawHits, &page); err != nil {
		return false
	}
	_, hasHits := page["hits"]
	_, hasTotal := page["total"]
	return hasHits && hasTotal
}

func init() { format.MustRegister(&Format{}) }
