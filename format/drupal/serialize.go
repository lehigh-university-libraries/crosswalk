package drupal

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
)

// Serialize writes hub records as Drupal entity JSON.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	if opts == nil {
		opts = format.NewSerializeOptions()
	}

	profile := opts.Profile
	if profile == nil {
		profile = defaultProfile()
	}

	entities := make([]map[string]any, 0, len(records))
	for _, record := range records {
		entity, err := recordToEntity(record, profile, opts)
		if err != nil {
			return fmt.Errorf("converting record: %w", err)
		}
		entities = append(entities, entity)
	}

	encoder := json.NewEncoder(w)
	if opts.Pretty {
		encoder.SetIndent("", "  ")
	}

	// If single record, output object; otherwise output array
	if len(entities) == 1 {
		return encoder.Encode(entities[0])
	}
	return encoder.Encode(entities)
}

func recordToEntity(record *hubv1.Record, profile *mapping.Profile, opts *format.SerializeOptions) (map[string]any, error) {
	entity := make(map[string]any)

	// Build reverse mapping: hub field -> source fields
	irToSource := make(map[string][]struct {
		SourceField string
		Mapping     mapping.FieldMapping
	})

	for source, fieldMapping := range profile.Fields {
		base, _ := mapping.IRFieldName(fieldMapping.IR)
		irToSource[base] = append(irToSource[base], struct {
			SourceField string
			Mapping     mapping.FieldMapping
		}{source, fieldMapping})
	}

	// Title
	if record.Title != "" {
		if sources, ok := irToSource["Title"]; ok && len(sources) > 0 {
			entity[sources[0].SourceField] = []map[string]any{{"value": record.Title}}
		}
	}

	// AltTitle
	if len(record.AltTitle) > 0 {
		if sources, ok := irToSource["AltTitle"]; ok && len(sources) > 0 {
			altTitles := make([]map[string]any, 0, len(record.AltTitle))
			for _, alt := range record.AltTitle {
				altTitles = append(altTitles, map[string]any{"value": alt})
			}
			entity[sources[0].SourceField] = altTitles
		}
	}

	// Abstract
	if record.Abstract != "" {
		if sources, ok := irToSource["Abstract"]; ok && len(sources) > 0 {
			entity[sources[0].SourceField] = []map[string]any{{"value": record.Abstract}}
		}
	}

	// Description
	if record.Description != "" {
		if sources, ok := irToSource["Description"]; ok && len(sources) > 0 {
			entity[sources[0].SourceField] = []map[string]any{{"value": record.Description}}
		}
	}

	// Contributors
	if len(record.Contributors) > 0 {
		if sources, ok := irToSource["Contributors"]; ok && len(sources) > 0 {
			contribs := make([]map[string]any, 0, len(record.Contributors))
			for _, c := range record.Contributors {
				contrib := map[string]any{}
				if c.SourceId != "" {
					contrib["target_id"] = c.SourceId
				}
				if c.RoleCode != "" {
					contrib["rel_type"] = c.RoleCode
				} else if c.Role != "" {
					contrib["rel_type"] = "relators:" + helpers.RoleToCode(c.Role)
				}
				contrib["target_type"] = "taxonomy_term"
				contribs = append(contribs, contrib)
			}
			entity[sources[0].SourceField] = contribs
		}
	}

	// Dates
	for _, d := range record.Dates {
		var targetField string
		switch d.Type {
		case hubv1.DateType_DATE_TYPE_ISSUED:
			if sources, ok := irToSource["Dates"]; ok {
				for _, s := range sources {
					if s.Mapping.DateType == "issued" {
						targetField = s.SourceField
						break
					}
				}
			}
		case hubv1.DateType_DATE_TYPE_CREATED:
			if sources, ok := irToSource["Dates"]; ok {
				for _, s := range sources {
					if s.Mapping.DateType == "created" {
						targetField = s.SourceField
						break
					}
				}
			}
		}
		if targetField != "" {
			entity[targetField] = []map[string]any{{"value": hub.FormatEDTF(d)}}
		}
	}

	// ResourceType
	if record.ResourceType != nil && (record.ResourceType.Original != "" || record.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED) {
		if sources, ok := irToSource["ResourceType"]; ok && len(sources) > 0 {
			// Would need taxonomy ID - for now store the original value
			entity[sources[0].SourceField] = []map[string]any{
				{"target_id": hub.ResourceTypeString(record.ResourceType)},
			}
		}
	}

	// Genre
	if len(record.Genres) > 0 {
		if sources, ok := irToSource["Genre"]; ok && len(sources) > 0 {
			genres := make([]map[string]any, 0, len(record.Genres))
			for _, g := range record.Genres {
				if g.SourceId != "" {
					genres = append(genres, map[string]any{"target_id": g.SourceId})
				} else {
					genres = append(genres, map[string]any{"target_id": g.Value})
				}
			}
			entity[sources[0].SourceField] = genres
		}
	}

	// Language
	if record.Language != "" {
		if sources, ok := irToSource["Language"]; ok && len(sources) > 0 {
			entity[sources[0].SourceField] = []map[string]any{{"target_id": record.Language}}
		}
	}

	// Rights
	if len(record.Rights) > 0 {
		if sources, ok := irToSource["Rights"]; ok && len(sources) > 0 {
			rights := make([]map[string]any, 0, len(record.Rights))
			for _, r := range record.Rights {
				if r.Uri != "" {
					rights = append(rights, map[string]any{"value": r.Uri})
				} else {
					rights = append(rights, map[string]any{"value": r.Statement})
				}
			}
			entity[sources[0].SourceField] = rights
		}
	}

	// Subjects
	if len(record.Subjects) > 0 {
		if sources, ok := irToSource["Subjects"]; ok && len(sources) > 0 {
			subjects := make([]map[string]any, 0, len(record.Subjects))
			for _, s := range record.Subjects {
				if s.SourceId != "" {
					subjects = append(subjects, map[string]any{"target_id": s.SourceId})
				} else {
					subjects = append(subjects, map[string]any{"target_id": s.Value})
				}
			}
			entity[sources[0].SourceField] = subjects
		}
	}

	// Publisher
	if record.Publisher != "" {
		if sources, ok := irToSource["Publisher"]; ok && len(sources) > 0 {
			entity[sources[0].SourceField] = []map[string]any{{"value": record.Publisher}}
		}
	}

	// PlacePublished
	if record.PlacePublished != "" {
		if sources, ok := irToSource["PlacePublished"]; ok && len(sources) > 0 {
			entity[sources[0].SourceField] = []map[string]any{{"value": record.PlacePublished}}
		}
	}

	// Relations
	if len(record.Relations) > 0 {
		if sources, ok := irToSource["Relations"]; ok && len(sources) > 0 {
			rels := make([]map[string]any, 0, len(record.Relations))
			for _, r := range record.Relations {
				if r.SourceId != "" {
					rels = append(rels, map[string]any{"target_id": r.SourceId})
				}
			}
			if len(rels) > 0 {
				entity[sources[0].SourceField] = rels
			}
		}
	}

	// Identifiers
	if len(record.Identifiers) > 0 {
		if sources, ok := irToSource["Identifiers"]; ok && len(sources) > 0 {
			ids := make([]map[string]any, 0, len(record.Identifiers))
			for _, id := range record.Identifiers {
				ids = append(ids, map[string]any{"value": id.Value})
			}
			entity[sources[0].SourceField] = ids
		}
	}

	// Notes
	if len(record.Notes) > 0 {
		if sources, ok := irToSource["Notes"]; ok && len(sources) > 0 {
			notes := make([]map[string]any, 0, len(record.Notes))
			for _, n := range record.Notes {
				notes = append(notes, map[string]any{"value": n})
			}
			entity[sources[0].SourceField] = notes
		}
	}

	// DegreeInfo
	if record.DegreeInfo != nil {
		if record.DegreeInfo.DegreeName != "" {
			for _, s := range profile.Fields {
				if s.IR == "DegreeInfo.DegreeName" {
					for name, m := range profile.Fields {
						if m.IR == s.IR {
							entity[name] = []map[string]any{{"value": record.DegreeInfo.DegreeName}}
							break
						}
					}
					break
				}
			}
		}
		if record.DegreeInfo.DegreeLevel != "" {
			for name, m := range profile.Fields {
				if m.IR == "DegreeInfo.DegreeLevel" {
					entity[name] = []map[string]any{{"value": record.DegreeInfo.DegreeLevel}}
					break
				}
			}
		}
		if record.DegreeInfo.Department != "" {
			for name, m := range profile.Fields {
				if m.IR == "DegreeInfo.Department" {
					entity[name] = []map[string]any{{"target_id": record.DegreeInfo.Department}}
					break
				}
			}
		}
	}

	// Extra fields
	extraFields := hub.GetExtraFields(record)
	for key, value := range extraFields {
		for name, m := range profile.Fields {
			_, subfield := mapping.IRFieldName(m.IR)
			if subfield == key {
				switch v := value.(type) {
				case string:
					entity[name] = []map[string]any{{"value": v}}
				case int, float64:
					entity[name] = []map[string]any{{"value": v}}
				case bool:
					entity[name] = []map[string]any{{"value": v}}
				default:
					entity[name] = v
				}
				break
			}
		}
	}

	return entity, nil
}
