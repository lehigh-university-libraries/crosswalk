// Package hub provides helper functions for working with hub.v1 protobuf types.
package hub

import (
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// NewRecord creates a new empty Record.
func NewRecord() *hubv1.Record {
	return &hubv1.Record{
		Contributors:         make([]*hubv1.Contributor, 0),
		Dates:                make([]*hubv1.DateValue, 0),
		Subjects:             make([]*hubv1.Subject, 0),
		Rights:               make([]*hubv1.Rights, 0),
		Identifiers:          make([]*hubv1.Identifier, 0),
		Notes:                make([]string, 0),
		Relations:            make([]*hubv1.Relation, 0),
		Genres:               make([]*hubv1.Subject, 0),
		Publishers:           make([]string, 0),
		PlacesPublished:      make([]string, 0),
		PhysicalDescriptions: make([]string, 0),
		Editions:             make([]string, 0),
		Languages:            make([]string, 0),
	}
}

// SetPublishers stores the complete ordered publisher list and mirrors its
// first value to the legacy scalar Publisher field for compatibility.
func SetPublishers(r *hubv1.Record, publishers []string) {
	if r == nil {
		return
	}

	r.Publishers = normalizeRepeatedStrings(publishers)
	r.Publisher = ""
	if len(r.Publishers) > 0 {
		r.Publisher = r.Publishers[0]
	}
}

// GetPublishers returns the complete ordered publisher list. Records created
// before the repeated field was added fall back to the legacy scalar value.
func GetPublishers(r *hubv1.Record) []string {
	if r == nil {
		return nil
	}
	if publishers := normalizeRepeatedStrings(r.Publishers); len(publishers) > 0 {
		return publishers
	}
	return normalizeRepeatedStrings([]string{r.Publisher})
}

// SetPlacesPublished stores every publication place and mirrors its first value
// to the legacy scalar PlacePublished field.
func SetPlacesPublished(r *hubv1.Record, places []string) {
	if r == nil {
		return
	}
	r.PlacesPublished = normalizeRepeatedStrings(places)
	r.PlacePublished = ""
	if len(r.PlacesPublished) > 0 {
		r.PlacePublished = r.PlacesPublished[0]
	}
}

// GetPlacesPublished returns every publication place, falling back to the
// legacy scalar field for records created before the repeated field existed.
func GetPlacesPublished(r *hubv1.Record) []string {
	if r == nil {
		return nil
	}
	if places := normalizeRepeatedStrings(r.PlacesPublished); len(places) > 0 {
		return places
	}
	return normalizeRepeatedStrings([]string{r.PlacePublished})
}

// SetPhysicalDescriptions stores every physical description and mirrors its
// first value to the legacy scalar PhysicalDesc field.
func SetPhysicalDescriptions(r *hubv1.Record, descriptions []string) {
	if r == nil {
		return
	}
	r.PhysicalDescriptions = normalizeRepeatedStrings(descriptions)
	r.PhysicalDesc = ""
	if len(r.PhysicalDescriptions) > 0 {
		r.PhysicalDesc = r.PhysicalDescriptions[0]
	}
}

// GetPhysicalDescriptions returns every physical description, falling back to
// the legacy scalar field for older records.
func GetPhysicalDescriptions(r *hubv1.Record) []string {
	if r == nil {
		return nil
	}
	if descriptions := normalizeRepeatedStrings(r.PhysicalDescriptions); len(descriptions) > 0 {
		return descriptions
	}
	return normalizeRepeatedStrings([]string{r.PhysicalDesc})
}

// SetEditions stores every edition statement and mirrors its first value to the
// legacy scalar Edition field.
func SetEditions(r *hubv1.Record, editions []string) {
	if r == nil {
		return
	}
	r.Editions = normalizeRepeatedStrings(editions)
	r.Edition = ""
	if len(r.Editions) > 0 {
		r.Edition = r.Editions[0]
	}
}

// GetEditions returns every edition statement, falling back to the legacy
// scalar field for older records.
func GetEditions(r *hubv1.Record) []string {
	if r == nil {
		return nil
	}
	if editions := normalizeRepeatedStrings(r.Editions); len(editions) > 0 {
		return editions
	}
	return normalizeRepeatedStrings([]string{r.Edition})
}

// SetLanguages stores every language value and mirrors its first value to the
// legacy scalar Language field.
func SetLanguages(r *hubv1.Record, languages []string) {
	if r == nil {
		return
	}
	r.Languages = normalizeRepeatedStrings(languages)
	r.Language = ""
	if len(r.Languages) > 0 {
		r.Language = r.Languages[0]
	}
}

// GetLanguages returns every language value, falling back to the legacy scalar
// field for older records.
func GetLanguages(r *hubv1.Record) []string {
	if r == nil {
		return nil
	}
	if languages := normalizeRepeatedStrings(r.Languages); len(languages) > 0 {
		return languages
	}
	return normalizeRepeatedStrings([]string{r.Language})
}

func normalizeRepeatedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
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
