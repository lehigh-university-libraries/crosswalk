// Package omeka_s parses static Omeka S REST API JSON-LD responses and
// deterministic Crosswalk snapshots. Network access and API authentication are
// deliberately outside this package.
package omeka_s

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

const (
	// Version identifies the Omeka S JSON-LD representation supported by this
	// adapter. Omeka S does not version its REST API independently from the
	// application, so this names the representation rather than a server release.
	Version = "jsonld-v1"

	// SnapshotFormat identifies Crosswalk's deterministic envelope for Omeka S
	// API resources and the installation-specific vocabulary/template model.
	SnapshotFormat = "omeka-s-jsonld"

	// SnapshotVersion is the current deterministic snapshot envelope version.
	SnapshotVersion = 1
)

// Format implements static Omeka S JSON-LD parsing.
type Format struct{}

var (
	_ format.Parser        = (*Format)(nil)
	_ format.DatasetParser = (*Format)(nil)
)

// Name returns the format identifier.
func (*Format) Name() string { return "omeka-s" }

// Description returns a human-readable format description.
func (*Format) Description() string {
	return "Omeka S REST API JSON-LD resource or installation snapshot"
}

// Extensions returns the associated file extensions.
func (*Format) Extensions() []string { return []string{"json", "jsonld"} }

// CanParse reports whether input resembles a supported Omeka S JSON-LD
// resource or deterministic snapshot. Generic JSON-LD is intentionally not
// claimed without an Omeka-specific type or marker.
func (*Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 || (peek[0] != '{' && peek[0] != '[') {
		return false
	}
	if bytes.Contains(peek, []byte(`"crosswalk_format"`)) && bytes.Contains(peek, []byte(`"`+SnapshotFormat+`"`)) {
		return true
	}
	if !bytes.Contains(peek, []byte(`"@type"`)) || !bytes.Contains(peek, []byte(`"o:id"`)) {
		return false
	}
	for _, typeName := range []string{"o:Item", "o:ItemSet", "o:Media", "o:Property", "o:ResourceClass", "o:ResourceTemplate", "o:Vocabulary"} {
		if bytes.Contains(peek, []byte(`"`+typeName+`"`)) {
			return true
		}
	}
	return false
}

func init() { format.MustRegister(&Format{}) }
