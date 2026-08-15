// Package scopus parses Scopus Search API JSON into Crosswalk Hub records.
package scopus

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version is the Scopus Search API representation implemented here.
const Version = "search-v1"

// Format implements the Scopus Search API JSON format.
type Format struct{}

var _ format.Parser = (*Format)(nil)

// Name returns the format identifier.
func (*Format) Name() string { return "scopus" }

// Description returns a human-readable format description.
func (*Format) Description() string { return "Scopus Search API JSON" }

// Extensions returns the associated file extensions.
func (*Format) Extensions() []string { return []string{"json"} }

// CanParse reports whether input resembles a Scopus search response or entry.
func (*Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 || peek[0] != '{' {
		return false
	}
	if bytes.Contains(peek, []byte(`"search-results"`)) {
		return true
	}
	return (bytes.Contains(peek, []byte(`"dc:identifier"`)) || bytes.Contains(peek, []byte(`"eid"`))) &&
		(bytes.Contains(peek, []byte(`"prism:publicationName"`)) || bytes.Contains(peek, []byte(`"subtypeDescription"`)))
}

func init() { format.MustRegister(&Format{}) }
