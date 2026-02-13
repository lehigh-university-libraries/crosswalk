package islandora_workbench

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

const sep = "|"

// workbenchRow holds a serialized record's column values and any associated agent rows.
type workbenchRow struct {
	cols   map[string]string
	agents [][]string
}

// columnOrder defines the canonical column order for Workbench CSV output.
// Reserved Workbench columns come first, then Drupal metadata fields.
var columnOrder = []string{
	"id",
	"parent_id",
	"node_id",
	"file",
	"title",
	"field_model",
	"field_language",
	"field_linked_agent",
	"field_edtf_date_issued",
	"field_edtf_date_created",
	"field_edtf_date",
	"field_abstract",
	"field_rights",
	"field_subject",
	"field_genre",
	"field_identifier",
	"field_extent",
	"field_note",
	"field_member_of",
	"field_part_detail",
	"field_related_item",
	"url_alias",
}

// Serialize writes hub records as Islandora Workbench CSV to w.
//
// If opts.ExtraWriters["agents"] is set, a second CSV with taxonomy term
// metadata for contributors is written there. Islandora Workbench uses this
// to create or update person/corporate_body taxonomy terms with ORCIDs,
// emails, statuses, and institutional relationships.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	if opts == nil {
		opts = format.NewSerializeOptions()
	}

	allRows := make([]workbenchRow, 0, len(records))
	colSeen := make(map[string]bool)

	for _, record := range records {
		cols, agents := recordToColumns(record)
		for col, val := range cols {
			if val != "" {
				colSeen[col] = true
			}
		}
		allRows = append(allRows, workbenchRow{cols: cols, agents: agents})
	}

	columns := orderedColumns(colSeen)

	mainWriter := csv.NewWriter(w)

	if opts.IncludeHeader {
		if err := mainWriter.Write(columns); err != nil {
			return fmt.Errorf("writing header: %w", err)
		}
	}

	for _, row := range allRows {
		csvRow := make([]string, len(columns))
		for i, col := range columns {
			csvRow[i] = row.cols[col]
		}
		if err := mainWriter.Write(csvRow); err != nil {
			return fmt.Errorf("writing row: %w", err)
		}
	}

	mainWriter.Flush()
	if err := mainWriter.Error(); err != nil {
		return fmt.Errorf("flushing CSV: %w", err)
	}

	if agentsW, ok := opts.ExtraWriters["agents"]; ok {
		if err := writeAgentsCSV(agentsW, allRows); err != nil {
			return fmt.Errorf("writing agents CSV: %w", err)
		}
	}

	return nil
}

// recordToColumns converts a hub record to a map of workbench column values
// and a slice of agent rows (one per contributor with extended metadata).
func recordToColumns(record *hubv1.Record) (map[string]string, [][]string) {
	cols := make(map[string]string)
	var agents [][]string

	// Reserved workbench columns from Extra
	if id := hub.GetExtraString(record, "id"); id != "" {
		cols["id"] = id
	}
	if nid := hub.GetExtraString(record, "node_id"); nid != "" {
		cols["node_id"] = nid
	}
	if parentID := hub.GetExtraString(record, "parent_id"); parentID != "" {
		cols["parent_id"] = parentID
	}

	cols["title"] = record.Title

	if model := islandoraModel(record.ResourceType); model != "" {
		cols["field_model"] = model
	}

	cols["field_language"] = record.Language

	// Contributors → field_linked_agent + optional agents rows
	if len(record.Contributors) > 0 {
		linkedAgents := make([]string, 0, len(record.Contributors))
		for _, c := range record.Contributors {
			linkedAgents = append(linkedAgents, serializeLinkedAgent(c))
			if needsAgentRow(c) {
				agents = append(agents, toAgentRow(c))
			}
		}
		cols["field_linked_agent"] = strings.Join(linkedAgents, sep)
	}

	// Dates (EDTF format)
	var issuedDates, createdDates []string
	for _, d := range record.Dates {
		edtf := hub.FormatEDTF(d)
		if edtf == "" {
			continue
		}
		switch d.Type {
		case hubv1.DateType_DATE_TYPE_ISSUED, hubv1.DateType_DATE_TYPE_PUBLISHED:
			issuedDates = append(issuedDates, edtf)
		case hubv1.DateType_DATE_TYPE_CREATED:
			createdDates = append(createdDates, edtf)
		default:
			issuedDates = append(issuedDates, edtf)
		}
	}
	if len(issuedDates) > 0 {
		cols["field_edtf_date_issued"] = strings.Join(issuedDates, sep)
	}
	if len(createdDates) > 0 {
		cols["field_edtf_date_created"] = strings.Join(createdDates, sep)
	}

	// Abstract and description both go to field_abstract with an attr0 attribute
	var abstracts []string
	if record.Abstract != "" {
		abstracts = append(abstracts, attrValue(record.Abstract, "abstract"))
	}
	if record.Description != "" {
		abstracts = append(abstracts, attrValue(record.Description, "description"))
	}
	if len(abstracts) > 0 {
		cols["field_abstract"] = strings.Join(abstracts, sep)
	}

	// Rights → field_rights (URI form preferred)
	if len(record.Rights) > 0 {
		rights := make([]string, 0, len(record.Rights))
		for _, r := range record.Rights {
			val := rightsValue(r)
			if val != "" {
				rights = append(rights, val)
			}
		}
		if len(rights) > 0 {
			cols["field_rights"] = strings.Join(rights, sep)
		}
	}

	// Subjects → field_subject
	if len(record.Subjects) > 0 {
		subjects := make([]string, 0, len(record.Subjects))
		for _, s := range record.Subjects {
			if val := subjectValue(s); val != "" {
				subjects = append(subjects, val)
			}
		}
		if len(subjects) > 0 {
			cols["field_subject"] = strings.Join(subjects, sep)
		}
	}

	// Genres → field_genre
	if len(record.Genres) > 0 {
		genres := make([]string, 0, len(record.Genres))
		for _, g := range record.Genres {
			if g.Value != "" {
				genres = append(genres, g.Value)
			}
		}
		if len(genres) > 0 {
			cols["field_genre"] = strings.Join(genres, sep)
		}
	}

	// Identifiers → field_identifier (attr0 notation)
	if len(record.Identifiers) > 0 {
		ids := make([]string, 0, len(record.Identifiers))
		for _, id := range record.Identifiers {
			if val := identifierValue(id); val != "" {
				ids = append(ids, val)
			}
		}
		if len(ids) > 0 {
			cols["field_identifier"] = strings.Join(ids, sep)
		}
	}

	// Physical description → field_extent
	var extents []string
	if record.PhysicalDesc != "" {
		extents = append(extents, attrValue(record.PhysicalDesc, "page"))
	}
	if record.Dimensions != "" {
		extents = append(extents, attrValue(record.Dimensions, "dimensions"))
	}
	if record.Duration != "" {
		extents = append(extents, attrValue(record.Duration, "minutes"))
	}
	if record.PageCount > 0 {
		extents = append(extents, attrValue(fmt.Sprintf("%d", record.PageCount), "page"))
	}
	if len(extents) > 0 {
		cols["field_extent"] = strings.Join(extents, sep)
	}

	// Notes → field_note
	if len(record.Notes) > 0 {
		notes := make([]string, 0, len(record.Notes))
		for _, n := range record.Notes {
			if n != "" {
				notes = append(notes, attrValue(n, "note"))
			}
		}
		if len(notes) > 0 {
			cols["field_note"] = strings.Join(notes, sep)
		}
	}

	// Relations → field_member_of
	memberOf := hub.GetMemberOf(record)
	if len(memberOf) > 0 {
		vals := make([]string, 0, len(memberOf))
		for _, rel := range memberOf {
			if rel.TargetId != "" {
				vals = append(vals, rel.TargetId)
			} else if rel.TargetTitle != "" {
				vals = append(vals, rel.TargetTitle)
			}
		}
		if len(vals) > 0 {
			cols["field_member_of"] = strings.Join(vals, sep)
		}
	}

	// Publication details → field_part_detail
	if record.Publication != nil {
		var parts []string
		if record.Publication.Volume != "" {
			parts = append(parts, partDetail(record.Publication.Volume, "volume"))
		}
		if record.Publication.Issue != "" {
			parts = append(parts, partDetail(record.Publication.Issue, "issue"))
		}
		if record.Publication.Pages != "" {
			parts = append(parts, partDetail(record.Publication.Pages, "page"))
		}
		if len(parts) > 0 {
			cols["field_part_detail"] = strings.Join(parts, sep)
		}

		// Journal title → field_related_item
		if record.Publication.Title != "" {
			cols["field_related_item"] = fmt.Sprintf(`{"title":"%s"}`, escapeJSON(record.Publication.Title))
		} else if record.Publication.LIssn != "" {
			cols["field_related_item"] = fmt.Sprintf(`{"type":"issn","identifier":"%s"}`, escapeJSON(record.Publication.LIssn))
		}
	}

	return cols, agents
}

// orderedColumns returns the columns that have data, in canonical order,
// with any unrecognised columns appended alphabetically at the end.
func orderedColumns(seen map[string]bool) []string {
	result := make([]string, 0, len(seen))
	appended := make(map[string]bool)

	for _, col := range columnOrder {
		if seen[col] {
			result = append(result, col)
			appended[col] = true
		}
	}

	// Any column not in columnOrder (e.g., from a profile) appended at end
	extras := make([]string, 0)
	for col := range seen {
		if !appended[col] {
			extras = append(extras, col)
		}
	}
	// Sort extras for deterministic output
	for i := 0; i < len(extras)-1; i++ {
		for j := i + 1; j < len(extras); j++ {
			if extras[i] > extras[j] {
				extras[i], extras[j] = extras[j], extras[i]
			}
		}
	}

	return append(result, extras...)
}

// writeAgentsCSV writes the agents CSV for contributors with extended metadata.
// Columns: term_name, field_contributor_status, field_relationships, field_email, field_identifier
func writeAgentsCSV(w io.Writer, rows []workbenchRow) error {
	writer := csv.NewWriter(w)
	header := []string{"term_name", "field_contributor_status", "field_relationships", "field_email", "field_identifier"}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("writing agents header: %w", err)
	}
	for _, row := range rows {
		for _, agent := range row.agents {
			if err := writer.Write(agent); err != nil {
				return fmt.Errorf("writing agent row: %w", err)
			}
		}
	}
	writer.Flush()
	return writer.Error()
}

// serializeLinkedAgent formats a contributor for the field_linked_agent column.
// Format: "relators:cre:person:Name" or "relators:cre:person:Name - Institution"
func serializeLinkedAgent(c *hubv1.Contributor) string {
	typePart := "person"
	if c.Type == hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION {
		typePart = "corporate_body"
	}

	institution := contributorInstitution(c)

	name := c.Name
	if institution != "" {
		name = fmt.Sprintf("%s - %s", name, institution)
	}

	roleCode := c.RoleCode
	if roleCode == "" {
		roleCode = "relators:aut"
	}

	return fmt.Sprintf("%s:%s:%s", roleCode, typePart, name)
}

// needsAgentRow returns true when this contributor has metadata beyond just a name
// and role — meaning we need to create/update a taxonomy term for them.
func needsAgentRow(c *hubv1.Contributor) bool {
	if c.Status != "" || c.Email != "" {
		return true
	}
	if contributorInstitution(c) != "" {
		return true
	}
	for _, id := range c.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID {
			return true
		}
	}
	return false
}

// toAgentRow converts a contributor to an agents CSV row.
// Returns [term_name, field_contributor_status, field_relationships, field_email, field_identifier]
func toAgentRow(c *hubv1.Contributor) []string {
	institution := contributorInstitution(c)

	termName := c.Name
	if institution != "" {
		termName = fmt.Sprintf("%s - %s", termName, institution)
	}

	relationships := ""
	if institution != "" {
		relationships = fmt.Sprintf("schema:worksFor:corporate_body:%s", institution)
	}

	identifier := ""
	for _, id := range c.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID && id.Value != "" {
			identifier = fmt.Sprintf(`{"attr0":"orcid","value":"%s"}`, id.Value)
			break
		}
	}

	return []string{termName, c.Status, relationships, c.Email, identifier}
}

// contributorInstitution returns the contributor's primary institution name.
func contributorInstitution(c *hubv1.Contributor) string {
	if len(c.Affiliations) > 0 {
		return c.Affiliations[0].Name
	}
	return c.Affiliation
}

// islandoraModel maps a hub ResourceType to an Islandora Models vocabulary term.
func islandoraModel(rt *hubv1.ResourceType) string {
	if rt == nil {
		return ""
	}
	switch rt.Type {
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE:
		return "Image"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO:
		return "Video"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO:
		return "Audio"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION:
		return "Collection"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE:
		return "Binary"
	default:
		return "Digital Document"
	}
}

// rightsValue extracts the preferred value for the field_rights column.
// Prefers the URI (rights statements / CC), falls back to statement text.
func rightsValue(r *hubv1.Rights) string {
	if r.Uri != "" {
		return r.Uri
	}
	if r.License != "" {
		return r.License
	}
	return r.Statement
}

// subjectValue formats a subject for the field_subject column.
// NAF geographic subjects use a "geographic_naf:" prefix.
func subjectValue(s *hubv1.Subject) string {
	if s.Value == "" {
		return ""
	}
	switch s.Vocabulary {
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH:
		if s.Uri != "" {
			return s.Value
		}
		return s.Value
	default:
		return s.Value
	}
}

// identifierValue formats an identifier as a Workbench attr0 JSON value.
func identifierValue(id *hubv1.Identifier) string {
	if id.Value == "" {
		return ""
	}
	switch id.Type {
	case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
		return attrValue(id.Value, "doi")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
		return attrValue(id.Value, "uri")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE:
		return attrValue(id.Value, "hdl")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
		return attrValue(id.Value, "isbn")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
		return attrValue(id.Value, "issn")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL:
		return attrValue(id.Value, "local")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID:
		// ORCIDs belong on the contributor taxonomy term, not on the node
		return ""
	default:
		return ""
	}
}

// attrValue builds a Workbench attr0 JSON object: {"value":"...","attr0":"..."}
func attrValue(value, attr string) string {
	return fmt.Sprintf(`{"value":"%s","attr0":"%s"}`, escapeJSON(value), attr)
}

// partDetail builds a field_part_detail JSON object: {"number":"...","type":"..."}
func partDetail(number, partType string) string {
	return fmt.Sprintf(`{"number":"%s","type":"%s"}`, escapeJSON(number), partType)
}

// escapeJSON escapes backslashes and double quotes for embedding in JSON strings.
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
