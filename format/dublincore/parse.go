package dublincore

import (
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/format/protoxml"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	dcv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/dublincore/v20200120"
	"github.com/lehigh-university-libraries/crosswalk/hub/convert"
	"google.golang.org/protobuf/proto"
)

// Parse reads Dublin Core XML and returns hub records.
// It handles bare <metadata> elements, multiple records in a single document,
// and OAI-PMH wrapped responses where <metadata> appears inside wrapper elements.
func (f *Format) Parse(r io.Reader, _ *format.ParseOptions) ([]*hubv1.Record, error) {
	spokes, err := protoxml.UnmarshalAll(r, func() proto.Message { return &dcv1.Record{} })
	if err != nil {
		return nil, fmt.Errorf("parsing dublin core XML: %w", err)
	}

	if len(spokes) == 0 {
		return nil, fmt.Errorf("no Dublin Core metadata elements found in input")
	}

	conv := convert.NewConverter()
	records := make([]*hubv1.Record, 0, len(spokes))

	for i, spoke := range spokes {
		dcRecord := spoke.(*dcv1.Record)

		result, err := conv.ToHub(spoke)
		if err != nil {
			return nil, fmt.Errorf("converting record %d to hub: %w", i, err)
		}

		// The generic converter does not extract scalar values from repeated
		// message types (e.g., repeated LocalizedString → title). Patch these
		// fields directly from the spoke proto.
		applyLocalizedFields(result.Record, dcRecord)

		result.Record.SourceInfo = &hubv1.SourceInfo{
			Format:        "dublincore",
			FormatVersion: Version,
		}

		records = append(records, result.Record)
	}

	return records, nil
}

// applyLocalizedFields patches hub record fields that the generic converter
// cannot derive from the DC spoke's repeated message types. The DC proto uses
// repeated LocalizedString for title, description, and abstract, plus repeated
// string for language. These map to scalar hub fields that need the inner value
// extracted.
func applyLocalizedFields(record *hubv1.Record, dc *dcv1.Record) {
	if len(dc.Title) > 0 {
		record.Title = localizedValue(dc.Title)
	}

	// dc:description and dcterms:abstract both target "abstract". Prefer
	// dcterms:abstract when present, fall back to dc:description.
	if len(dc.Abstract) > 0 {
		record.Abstract = localizedValue(dc.Abstract)
	} else if len(dc.Description) > 0 {
		record.Abstract = localizedValue(dc.Description)
	}

	if len(dc.Language) > 0 {
		record.Language = dc.Language[0]
	}

	if len(dc.Publisher) > 0 && dc.Publisher[0].Name != "" {
		record.Publisher = dc.Publisher[0].Name
	}

	// Identifiers: the generic converter may produce a single stringified entry
	// from a repeated Identifier message. Rebuild from spoke data.
	if len(dc.Identifier) > 0 {
		record.Identifiers = make([]*hubv1.Identifier, 0, len(dc.Identifier))
		for _, id := range dc.Identifier {
			if id.Value != "" {
				record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
					Value: id.Value,
				})
			}
		}
	}

	// Subjects: rebuild from spoke so each entry is a separate Subject.
	if len(dc.Subject) > 0 {
		record.Subjects = make([]*hubv1.Subject, 0, len(dc.Subject))
		for _, s := range dc.Subject {
			if s.Value != "" {
				record.Subjects = append(record.Subjects, &hubv1.Subject{
					Value: s.Value,
				})
			}
		}
	}

	// Dates from dc:date (repeated Date message)
	if len(dc.Date) > 0 {
		record.Dates = make([]*hubv1.DateValue, 0, len(dc.Date))
		for _, d := range dc.Date {
			if d.Value != "" {
				record.Dates = append(record.Dates, &hubv1.DateValue{
					Raw: d.Value,
				})
			}
		}
	}

	// Qualified dates from dcterms (issued, created, modified, available)
	appendQualifiedDate(record, dc.Issued, hubv1.DateType_DATE_TYPE_ISSUED)
	appendQualifiedDate(record, dc.Created, hubv1.DateType_DATE_TYPE_CREATED)
	appendQualifiedDate(record, dc.Modified, hubv1.DateType_DATE_TYPE_UPDATED)
	appendQualifiedDate(record, dc.Available, hubv1.DateType_DATE_TYPE_AVAILABLE)

	// Alt title from dcterms:alternative
	if len(dc.Alternative) > 0 {
		for _, alt := range dc.Alternative {
			v := strings.TrimSpace(alt.Value)
			if v != "" {
				record.AltTitle = append(record.AltTitle, v)
			}
		}
	}
}

// localizedValue extracts the text value from the first non-empty LocalizedString.
func localizedValue(ls []*dcv1.LocalizedString) string {
	var parts []string
	for _, l := range ls {
		v := strings.TrimSpace(l.Value)
		if v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, "; ")
}

// appendQualifiedDate adds a typed date to the record if the value is non-empty.
func appendQualifiedDate(record *hubv1.Record, value string, dateType hubv1.DateType) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	record.Dates = append(record.Dates, &hubv1.DateValue{
		Type: dateType,
		Raw:  value,
	})
}
