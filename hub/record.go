// Package hub provides helper functions for working with hub.v1 protobuf types.
package hub

import (
	"google.golang.org/protobuf/types/known/structpb"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// NewRecord creates a new empty Record.
func NewRecord() *hubv1.Record {
	return &hubv1.Record{
		Contributors: make([]*hubv1.Contributor, 0),
		Dates:        make([]*hubv1.DateValue, 0),
		Subjects:     make([]*hubv1.Subject, 0),
		Rights:       make([]*hubv1.Rights, 0),
		Identifiers:  make([]*hubv1.Identifier, 0),
		Notes:        make([]string, 0),
		Relations:    make([]*hubv1.Relation, 0),
		Genres:       make([]*hubv1.Subject, 0),
	}
}

// GetDate returns the first date of a given type, or nil if not found.
func GetDate(r *hubv1.Record, dateType hubv1.DateType) *hubv1.DateValue {
	for _, d := range r.Dates {
		if d.Type == dateType {
			return d
		}
	}
	return nil
}

// GetDates returns all dates of a given type.
func GetDates(r *hubv1.Record, dateType hubv1.DateType) []*hubv1.DateValue {
	var result []*hubv1.DateValue
	for _, d := range r.Dates {
		if d.Type == dateType {
			result = append(result, d)
		}
	}
	return result
}

// GetDateIssued returns the issued date if present.
func GetDateIssued(r *hubv1.Record) *hubv1.DateValue {
	return GetDate(r, hubv1.DateType_DATE_TYPE_ISSUED)
}

// GetDateCreated returns the created date if present.
func GetDateCreated(r *hubv1.Record) *hubv1.DateValue {
	return GetDate(r, hubv1.DateType_DATE_TYPE_CREATED)
}

// PrimaryDate returns the most appropriate date for display.
func PrimaryDate(r *hubv1.Record) *hubv1.DateValue {
	priorities := []hubv1.DateType{
		hubv1.DateType_DATE_TYPE_ISSUED,
		hubv1.DateType_DATE_TYPE_PUBLISHED,
		hubv1.DateType_DATE_TYPE_CREATED,
		hubv1.DateType_DATE_TYPE_CAPTURED,
		hubv1.DateType_DATE_TYPE_COPYRIGHT,
	}
	for _, dt := range priorities {
		if d := GetDate(r, dt); d != nil {
			return d
		}
	}
	if len(r.Dates) > 0 {
		return r.Dates[0]
	}
	return nil
}

// GetIdentifier returns the first identifier of a given type.
func GetIdentifier(r *hubv1.Record, idType hubv1.IdentifierType) *hubv1.Identifier {
	for _, id := range r.Identifiers {
		if id.Type == idType {
			return id
		}
	}
	return nil
}

// GetDOI returns the DOI if present.
func GetDOI(r *hubv1.Record) *hubv1.Identifier {
	return GetIdentifier(r, hubv1.IdentifierType_IDENTIFIER_TYPE_DOI)
}

// GetContributorsByRole returns contributors with a specific role.
func GetContributorsByRole(r *hubv1.Record, role string) []*hubv1.Contributor {
	var result []*hubv1.Contributor
	for _, c := range r.Contributors {
		if c.Role == role {
			result = append(result, c)
		}
	}
	return result
}

// GetAuthors returns contributors with author/creator roles.
func GetAuthors(r *hubv1.Record) []*hubv1.Contributor {
	var result []*hubv1.Contributor
	for _, c := range r.Contributors {
		if c.Role == "creator" || c.Role == "author" || c.Role == "aut" || c.Role == "cre" {
			result = append(result, c)
		}
	}
	return result
}

// GetSubjectsByVocab returns subjects from a specific vocabulary.
func GetSubjectsByVocab(r *hubv1.Record, vocab hubv1.SubjectVocabulary) []*hubv1.Subject {
	var result []*hubv1.Subject
	for _, s := range r.Subjects {
		if s.Vocabulary == vocab {
			result = append(result, s)
		}
	}
	return result
}

// GetKeywords returns keyword subjects.
func GetKeywords(r *hubv1.Record) []*hubv1.Subject {
	return GetSubjectsByVocab(r, hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS)
}

// GetLCSHSubjects returns LCSH subjects.
func GetLCSHSubjects(r *hubv1.Record) []*hubv1.Subject {
	return GetSubjectsByVocab(r, hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH)
}

// GetRelationsByType returns relations of a specific type.
func GetRelationsByType(r *hubv1.Record, relType hubv1.RelationType) []*hubv1.Relation {
	var result []*hubv1.Relation
	for _, rel := range r.Relations {
		if rel.Type == relType {
			result = append(result, rel)
		}
	}
	return result
}

// GetMemberOf returns member_of relations.
func GetMemberOf(r *hubv1.Record) []*hubv1.Relation {
	return GetRelationsByType(r, hubv1.RelationType_RELATION_TYPE_MEMBER_OF)
}

// SetExtra sets an extra field value on the record.
func SetExtra(r *hubv1.Record, key string, value any) {
	if r.Extra == nil {
		r.Extra = &structpb.Struct{
			Fields: make(map[string]*structpb.Value),
		}
	}
	v, err := structpb.NewValue(value)
	if err == nil {
		r.Extra.Fields[key] = v
	}
}

// GetExtra retrieves an extra field value.
func GetExtra(r *hubv1.Record, key string) (any, bool) {
	if r.Extra == nil || r.Extra.Fields == nil {
		return nil, false
	}
	v, ok := r.Extra.Fields[key]
	if !ok {
		return nil, false
	}
	return v.AsInterface(), true
}

// GetExtraString retrieves an extra field as a string.
func GetExtraString(r *hubv1.Record, key string) string {
	v, ok := GetExtra(r, key)
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// GetExtraFields returns all extra fields as a map.
func GetExtraFields(r *hubv1.Record) map[string]any {
	if r.Extra == nil || r.Extra.Fields == nil {
		return nil
	}
	result := make(map[string]any, len(r.Extra.Fields))
	for k, v := range r.Extra.Fields {
		result[k] = v.AsInterface()
	}
	return result
}

// GetSubjectsByType returns subjects of a specific type (topic, name, geographic, etc.).
func GetSubjectsByType(r *hubv1.Record, t hubv1.SubjectType) []*hubv1.Subject {
	var result []*hubv1.Subject
	for _, s := range r.Subjects {
		if s.Type == t {
			result = append(result, s)
		}
	}
	return result
}

// CollapseSubjects combines all subjects into a single deduplicated list.
// Useful for formats that don't support multiple subject vocabularies.
// Prefers URI over Value when both are available.
func CollapseSubjects(subjects []*hubv1.Subject) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range subjects {
		// Prefer URI when available (more semantic), fall back to Value
		val := s.Uri
		if val == "" {
			val = s.Value
		}
		if val != "" && !seen[val] {
			result = append(result, val)
			seen[val] = true
		}
	}
	return result
}

// CollapseSubjectsLabels combines all subjects into a single deduplicated list of labels.
// Always uses Value (label) instead of URI.
func CollapseSubjectsLabels(subjects []*hubv1.Subject) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range subjects {
		if s.Value != "" && !seen[s.Value] {
			result = append(result, s.Value)
			seen[s.Value] = true
		}
	}
	return result
}
