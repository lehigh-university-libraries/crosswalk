// Package crossrefrest parses Crossref REST API JSON into Crosswalk Hub records.
package crossrefrest

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version is the Crossref REST message representation implemented here.
const Version = "1.0"

// Format implements Crossref REST API JSON parsing.
type Format struct{}

var _ format.Parser = (*Format)(nil)

// Name returns the format identifier.
func (*Format) Name() string { return "crossref-rest" }

// Description returns a human-readable description.
func (*Format) Description() string { return "Crossref REST API JSON" }

// Extensions returns associated extensions.
func (*Format) Extensions() []string { return []string{"json"} }

// CanParse reports whether the input resembles a Crossref REST result page.
func (*Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	return len(peek) > 0 && peek[0] == '{' && bytes.Contains(peek, []byte(`"message"`)) &&
		bytes.Contains(peek, []byte(`"items"`)) && bytes.Contains(peek, []byte(`"DOI"`))
}

func init() { format.MustRegister(&Format{}) }
