// Package marc provides a format plugin for MARC21 bibliographic records.
package marc

import (
	"bytes"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version documents the MARC flavor this implementation targets.
const Version = "MARC21"

// Format implements the MARC21 format.
type Format struct{}

var (
	_ format.Format     = (*Format)(nil)
	_ format.Parser     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "marc"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "MARC21 bibliographic records"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"mrc", "marc", "marcxml"}
}

// CanParse returns true if the input looks like MARC21 binary or MARCXML.
func (f *Format) CanParse(peek []byte) bool {
	trimmed := bytes.TrimSpace(peek)
	if len(trimmed) == 0 {
		return false
	}

	if bytes.HasPrefix(trimmed, []byte("<")) {
		lower := bytes.ToLower(trimmed)
		return bytes.Contains(lower, []byte("marc21/slim")) ||
			(bytes.Contains(lower, []byte("<leader>")) &&
				(bytes.Contains(lower, []byte("<controlfield")) ||
					bytes.Contains(lower, []byte("<datafield"))))
	}

	if len(trimmed) < 24 {
		return false
	}
	for _, b := range trimmed[:5] {
		if b < '0' || b > '9' {
			return false
		}
	}

	leader := string(trimmed[:24])
	if !strings.HasSuffix(leader, "4500") {
		return false
	}
	return leader[10:12] == "22"
}

func init() {
	format.MustRegister(&Format{})
}
