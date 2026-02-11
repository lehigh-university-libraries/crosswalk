package csv

import (
	"encoding/csv"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
)

// Serialize writes hub records as CSV.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	if opts == nil {
		opts = format.NewSerializeOptions()
	}

	sep := opts.MultiValueSeparator
	if sep == "" {
		sep = "|"
	}

	columns := opts.Columns
	if len(columns) == 0 {
		columns = mapping.DefaultCSVColumns()
	}

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header
	if opts.IncludeHeader {
		if err := writer.Write(columns); err != nil {
			return err
		}
	}

	// Write records
	for _, record := range records {
		row := recordToRow(record, columns, sep)
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return writer.Error()
}

func recordToRow(record *hubv1.Record, columns []string, sep string) []string {
	row := make([]string, len(columns))

	for i, col := range columns {
		row[i] = getColumnValue(record, col, sep)
	}

	return row
}

func getColumnValue(record *hubv1.Record, column string, sep string) string {
	switch column {
	case "title":
		return record.Title

	case "alt_title":
		return strings.Join(record.AltTitle, sep)

	case "contributors":
		names := make([]string, 0, len(record.Contributors))
		for _, c := range record.Contributors {
			names = append(names, hub.DisplayName(c))
		}
		return strings.Join(names, sep)

	case "contributor_roles":
		roles := make([]string, 0, len(record.Contributors))
		for _, c := range record.Contributors {
			if c.Role != "" {
				roles = append(roles, c.Role)
			} else if c.RoleCode != "" {
				roles = append(roles, c.RoleCode)
			} else {
				roles = append(roles, "")
			}
		}
		return strings.Join(roles, sep)

	case "date_issued":
		if d := hub.GetDateIssued(record); d != nil {
			return hub.DateString(d)
		}
		return ""

	case "date_created":
		if d := hub.GetDateCreated(record); d != nil {
			return hub.DateString(d)
		}
		return ""

	case "date":
		// Primary date
		if d := hub.PrimaryDate(record); d != nil {
			return hub.DateString(d)
		}
		return ""

	case "resource_type":
		if record.ResourceType != nil {
			return hub.ResourceTypeString(record.ResourceType)
		}
		return ""

	case "genre":
		genres := make([]string, 0, len(record.Genres))
		for _, g := range record.Genres {
			genres = append(genres, g.Value)
		}
		return strings.Join(genres, sep)

	case "language":
		return record.Language

	case "rights":
		rights := make([]string, 0, len(record.Rights))
		for _, r := range record.Rights {
			if r.Uri != "" {
				rights = append(rights, r.Uri)
			} else {
				rights = append(rights, r.Statement)
			}
		}
		return strings.Join(rights, sep)

	case "rights_label":
		labels := make([]string, 0, len(record.Rights))
		for _, r := range record.Rights {
			labels = append(labels, hub.RightsString(r))
		}
		return strings.Join(labels, sep)

	case "abstract":
		return record.Abstract

	case "description":
		return record.Description

	case "identifiers":
		ids := make([]string, 0, len(record.Identifiers))
		for _, id := range record.Identifiers {
			ids = append(ids, id.Value)
		}
		return strings.Join(ids, sep)

	case "doi":
		if id := hub.GetDOI(record); id != nil {
			return id.Value
		}
		return ""

	case "subjects":
		subjects := make([]string, 0, len(record.Subjects))
		for _, s := range record.Subjects {
			subjects = append(subjects, s.Value)
		}
		return strings.Join(subjects, sep)

	case "keywords":
		keywords := hub.GetKeywords(record)
		vals := make([]string, 0, len(keywords))
		for _, k := range keywords {
			vals = append(vals, k.Value)
		}
		return strings.Join(vals, sep)

	case "lcsh_subjects":
		lcsh := hub.GetLCSHSubjects(record)
		vals := make([]string, 0, len(lcsh))
		for _, s := range lcsh {
			vals = append(vals, s.Value)
		}
		return strings.Join(vals, sep)

	case "publisher":
		return record.Publisher

	case "place_published":
		return record.PlacePublished

	case "member_of":
		rels := hub.GetMemberOf(record)
		titles := make([]string, 0, len(rels))
		for _, r := range rels {
			titles = append(titles, r.TargetTitle)
		}
		return strings.Join(titles, sep)

	case "member_of_id":
		rels := hub.GetMemberOf(record)
		ids := make([]string, 0, len(rels))
		for _, r := range rels {
			ids = append(ids, r.SourceId)
		}
		return strings.Join(ids, sep)

	case "degree_name":
		if record.DegreeInfo != nil {
			return record.DegreeInfo.DegreeName
		}
		return ""

	case "degree_level":
		if record.DegreeInfo != nil {
			return record.DegreeInfo.DegreeLevel
		}
		return ""

	case "department":
		if record.DegreeInfo != nil {
			return record.DegreeInfo.Department
		}
		return ""

	case "institution":
		if record.DegreeInfo != nil {
			return record.DegreeInfo.Institution
		}
		return ""

	case "notes":
		return strings.Join(record.Notes, sep)

	case "physical_description":
		return record.PhysicalDesc

	case "table_of_contents":
		return record.TableOfContents

	case "source":
		return record.Source

	case "digital_origin":
		return record.DigitalOrigin

	case "nid":
		return hub.GetExtraString(record, "nid")

	case "uuid":
		return hub.GetExtraString(record, "uuid")

	case "created":
		return hub.GetExtraString(record, "created")

	case "changed":
		return hub.GetExtraString(record, "changed")

	case "status":
		return hub.GetExtraString(record, "status")

	case "type":
		return hub.GetExtraString(record, "type")

	default:
		// Try Extra fields
		if v, ok := hub.GetExtra(record, column); ok {
			switch val := v.(type) {
			case string:
				return val
			case []string:
				return strings.Join(val, sep)
			default:
				return ""
			}
		}
		return ""
	}
}

// DefaultColumns returns the standard column set for CSV output.
func DefaultColumns() []string {
	return mapping.DefaultCSVColumns()
}
