package marc

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	marcfile "github.com/hectorcorrea/marcli/pkg/marc"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

var yearPattern = regexp.MustCompile(`\d{4}`)

const maxMARCInputBytes = int64(64 << 20)

// Parse reads MARC21 binary or MARCXML and returns hub records.
func (f *Format) Parse(r io.Reader, _ *format.ParseOptions) ([]*hubv1.Record, error) {
	file, cleanup, err := readerFile(r)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	marc := marcfile.NewMarcFile(file)
	var records []*hubv1.Record
	var index int
	for marc.Scan() {
		index++
		rec, err := marc.Record()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading MARC record %d: %w", index, err)
		}
		records = append(records, recordToHub(rec))
	}
	if err := marc.Err(); err != nil {
		return nil, fmt.Errorf("scanning MARC input: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("no MARC records found in input")
	}
	return records, nil
}

func readerFile(r io.Reader) (*os.File, func(), error) {
	return readerFileWithLimit(r, maxMARCInputBytes)
}

func readerFileWithLimit(r io.Reader, maxBytes int64) (*os.File, func(), error) {
	if r == nil {
		return nil, nil, fmt.Errorf("MARC input reader is required")
	}

	tmp, err := os.CreateTemp("", "crosswalk-marc-*")
	if err != nil {
		return nil, nil, fmt.Errorf("creating temporary MARC input: %w", err)
	}
	cleanup := func() {
		name := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(name)
	}

	written, err := io.Copy(tmp, io.LimitReader(r, maxBytes+1))
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("buffering MARC input: %w", err)
	}
	if written > maxBytes {
		cleanup()
		return nil, nil, fmt.Errorf("MARC input exceeds %d bytes", maxBytes)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("rewinding MARC input: %w", err)
	}
	return tmp, cleanup, nil
}

func recordToHub(rec marcfile.Record) *hubv1.Record {
	record := hub.NewRecord()

	if control := cleanMARCValue(rec.ControlNum()); control != "" {
		record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
			Type:        hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL,
			Value:       control,
			IsPreferred: true,
		})
	}

	record.Title = titleFromField(firstField(rec, "245"))
	for _, f := range rec.FieldsByTag("246") {
		if title := titleFromField(f); title != "" {
			record.AltTitle = append(record.AltTitle, title)
		}
	}

	addIdentifiers(record, rec)
	addContributors(record, rec)
	addPublication(record, rec)
	addPhysicalDescription(record, rec)
	addNotes(record, rec)
	addRights(record, rec)
	addSubjects(record, rec)
	addRelations(record, rec)

	if lang := languageFromRecord(rec); lang != "" {
		record.Language = lang
	}
	if record.ResourceType == nil {
		record.ResourceType = resourceTypeFromLeader(rec.Leader.Raw())
	}

	record.SourceInfo = &hubv1.SourceInfo{
		Format:        "marc",
		FormatVersion: Version,
		SourceId:      rec.ControlNum(),
	}
	hub.SetExtra(record, "marc_leader", rec.Leader.Raw())

	return record
}

func addIdentifiers(record *hubv1.Record, rec marcfile.Record) {
	for _, f := range rec.FieldsByTag("020") {
		for _, value := range subfieldValues(f, "a", "z") {
			addIdentifier(record, value, hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN)
		}
	}
	for _, f := range rec.FieldsByTag("022") {
		for _, value := range subfieldValues(f, "a", "l", "z") {
			addIdentifier(record, value, hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN)
		}
	}
	for _, f := range rec.FieldsByTag("024") {
		for _, value := range subfieldValues(f, "a") {
			addIdentifier(record, value, hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED)
		}
	}
	for _, tag := range []string{"035", "088"} {
		for _, f := range rec.FieldsByTag(tag) {
			for _, value := range subfieldValues(f, "a") {
				idType := hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
				if tag == "088" {
					idType = hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER
				}
				addIdentifier(record, value, idType)
			}
		}
	}
	for _, tag := range []string{"050", "090"} {
		for _, f := range rec.FieldsByTag(tag) {
			if callNumber := joinSubfields(f, " ", "a", "b"); callNumber != "" {
				addIdentifier(record, callNumber, hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER)
			}
		}
	}
	for _, f := range rec.FieldsByTag("856") {
		for _, value := range subfieldValues(f, "u") {
			addIdentifier(record, value, hubv1.IdentifierType_IDENTIFIER_TYPE_URL)
		}
	}
}

func addIdentifier(record *hubv1.Record, value string, idType hubv1.IdentifierType) {
	value = cleanIdentifier(value)
	if value == "" {
		return
	}
	id := hub.NewIdentifier(value, idType)
	for _, existing := range record.Identifiers {
		if existing.Type == id.Type && existing.Value == id.Value {
			return
		}
	}
	record.Identifiers = append(record.Identifiers, id)
}

func addContributors(record *hubv1.Record, rec marcfile.Record) {
	contributorFields := []struct {
		tag         string
		defaultRole string
		defaultCode string
		person      bool
	}{
		{"100", "aut", "aut", true},
		{"110", "cre", "cre", false},
		{"111", "cre", "cre", false},
		{"700", "ctb", "ctb", true},
		{"710", "ctb", "ctb", false},
		{"711", "ctb", "ctb", false},
	}

	for _, cfg := range contributorFields {
		for _, f := range rec.FieldsByTag(cfg.tag) {
			name := contributorName(f, cfg.person)
			if name == "" {
				continue
			}

			role, code := contributorRole(f, cfg.defaultRole, cfg.defaultCode)
			c := &hubv1.Contributor{
				Name:     name,
				Role:     role,
				RoleCode: code,
			}
			if cfg.person {
				c.Type = hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON
				c.ParsedName = helpers.ParseName(name)
			} else {
				c.Type = hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
			}
			record.Contributors = append(record.Contributors, c)
		}
	}
}

func addPublication(record *hubv1.Record, rec marcfile.Record) {
	fields := publicationFields(rec)
	if len(fields) == 0 {
		return
	}

	var publishers []string
	for _, field := range fields {
		if record.PlacePublished == "" {
			record.PlacePublished = cleanMARCValue(firstSubfield(field, "a"))
		}
		publishers = append(publishers, subfieldValues(field, "b")...)
		if len(record.Dates) == 0 {
			if rawDate := cleanDateValue(firstSubfield(field, "c")); rawDate != "" {
				record.Dates = append(record.Dates, dateFromString(rawDate, hubv1.DateType_DATE_TYPE_ISSUED))
			}
		}
	}
	hub.SetPublishers(record, publishers)
}

func addPhysicalDescription(record *hubv1.Record, rec marcfile.Record) {
	for _, f := range rec.FieldsByTag("300") {
		if value := joinSubfields(f, " ", "a", "b", "c", "e", "f", "g"); value != "" {
			record.PhysicalDesc = value
			return
		}
	}
}

func addNotes(record *hubv1.Record, rec marcfile.Record) {
	for _, f := range rec.FieldsByTag("520") {
		if value := joinSubfields(f, " ", "a", "b"); value != "" && record.Abstract == "" {
			record.Abstract = value
		}
	}
	for _, tag := range []string{"500", "502", "504", "505", "506", "508", "511", "538", "545", "546"} {
		for _, f := range rec.FieldsByTag(tag) {
			if value := joinSubfields(f, " ", "a", "b", "g", "u"); value != "" {
				record.Notes = append(record.Notes, value)
			}
		}
	}
}

func addRights(record *hubv1.Record, rec marcfile.Record) {
	for _, f := range rec.FieldsByTag("540") {
		rights := &hubv1.Rights{
			Statement: joinSubfields(f, " ", "a", "b", "c", "d"),
			Uri:       cleanMARCValue(firstSubfield(f, "u")),
		}
		if rights.Statement != "" || rights.Uri != "" {
			record.Rights = append(record.Rights, rights)
		}
	}
}

func addSubjects(record *hubv1.Record, rec marcfile.Record) {
	subjectTags := []string{"600", "610", "611", "630", "648", "650", "651"}
	for _, tag := range subjectTags {
		for _, f := range rec.FieldsByTag(tag) {
			subject := subjectFromField(f)
			if subject.Value != "" {
				record.Subjects = append(record.Subjects, subject)
			}
		}
	}
	for _, f := range rec.FieldsByTag("655") {
		genre := subjectFromField(f)
		genre.Type = hubv1.SubjectType_SUBJECT_TYPE_GENRE
		if genre.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED {
			genre.Vocabulary = hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE
		}
		if genre.Value != "" {
			record.Genres = append(record.Genres, genre)
		}
	}
}

func addRelations(record *hubv1.Record, rec marcfile.Record) {
	for _, tag := range []string{"440", "490", "830"} {
		for _, f := range rec.FieldsByTag(tag) {
			if title := joinSubfields(f, " ", "a", "v"); title != "" {
				record.Relations = append(record.Relations, &hubv1.Relation{
					Type:        hubv1.RelationType_RELATION_TYPE_IN_SERIES,
					TargetTitle: title,
				})
			}
		}
	}

	for _, f := range rec.FieldsByTag("773") {
		if rel := relationFromField(f, hubv1.RelationType_RELATION_TYPE_PART_OF); rel.TargetTitle != "" || rel.TargetId != "" {
			record.Relations = append(record.Relations, rel)
		}
	}
	for _, f := range rec.FieldsByTag("776") {
		if rel := relationFromField(f, hubv1.RelationType_RELATION_TYPE_HAS_FORMAT); rel.TargetTitle != "" || rel.TargetId != "" {
			record.Relations = append(record.Relations, rel)
		}
	}
	for _, f := range rec.FieldsByTag("787") {
		if rel := relationFromField(f, hubv1.RelationType_RELATION_TYPE_RELATED_TO); rel.TargetTitle != "" || rel.TargetId != "" {
			record.Relations = append(record.Relations, rel)
		}
	}
}

func relationFromField(f marcfile.Field, relType hubv1.RelationType) *hubv1.Relation {
	return &hubv1.Relation{
		Type:        relType,
		TargetTitle: joinSubfields(f, " ", "a", "t"),
		TargetId:    cleanIdentifier(firstSubfield(f, "w")),
	}
}

func firstField(rec marcfile.Record, tag string) marcfile.Field {
	fields := rec.FieldsByTag(tag)
	if len(fields) == 0 {
		return marcfile.Field{}
	}
	return fields[0]
}

func publicationFields(rec marcfile.Record) []marcfile.Field {
	var fields []marcfile.Field
	for _, f := range rec.FieldsByTag("264") {
		if f.Indicator2 == "1" || f.Indicator2 == " " || f.Indicator2 == "" {
			fields = append(fields, f)
		}
	}
	if len(fields) > 0 {
		return fields
	}
	return rec.FieldsByTag("260")
}

func titleFromField(f marcfile.Field) string {
	if f.Tag == "" {
		return ""
	}
	return joinSubfields(f, " ", "a", "b", "h", "n", "p")
}

func contributorName(f marcfile.Field, person bool) string {
	if person {
		return joinSubfields(f, " ", "a", "b", "c", "q", "d")
	}
	return joinSubfields(f, " ", "a", "b", "c", "d", "n")
}

func contributorRole(f marcfile.Field, defaultRole, defaultCode string) (string, string) {
	if code := helpers.NormalizeRole(firstSubfield(f, "4")); code != "" {
		return code, code
	}
	if role := cleanMARCValue(firstSubfield(f, "e")); role != "" {
		code := helpers.NormalizeRole(role)
		if code != "" && code != role {
			return code, code
		}
		return strings.ToLower(role), ""
	}
	return defaultRole, defaultCode
}

func subjectFromField(f marcfile.Field) *hubv1.Subject {
	value := joinSubfields(f, " -- ", "a", "b", "c", "d", "t", "v", "x", "y", "z")
	subject := &hubv1.Subject{
		Value:      value,
		Vocabulary: subjectVocabulary(f),
		Type:       subjectType(f.Tag),
	}
	return subject
}

func subjectVocabulary(f marcfile.Field) hubv1.SubjectVocabulary {
	source := strings.ToLower(cleanMARCValue(firstSubfield(f, "2")))
	switch source {
	case "fast":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_FAST
	case "lcgft":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE
	case "mesh":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH
	case "aat":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT
	}

	switch f.Indicator2 {
	case "0":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH
	case "2":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH
	case "7":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL
	default:
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
	}
}

func subjectType(tag string) hubv1.SubjectType {
	switch tag {
	case "600", "610", "611":
		return hubv1.SubjectType_SUBJECT_TYPE_NAME
	case "630":
		return hubv1.SubjectType_SUBJECT_TYPE_TITLE
	case "648":
		return hubv1.SubjectType_SUBJECT_TYPE_TEMPORAL
	case "651":
		return hubv1.SubjectType_SUBJECT_TYPE_GEOGRAPHIC
	case "655":
		return hubv1.SubjectType_SUBJECT_TYPE_GENRE
	default:
		return hubv1.SubjectType_SUBJECT_TYPE_TOPIC
	}
}

func languageFromRecord(rec marcfile.Record) string {
	if value := rec.GetValue("008", ""); len(value) >= 38 {
		lang := strings.TrimSpace(value[35:38])
		if lang != "" && lang != "|||" {
			return lang
		}
	}
	for _, f := range rec.FieldsByTag("041") {
		if lang := cleanMARCValue(firstSubfield(f, "a")); lang != "" {
			return lang
		}
	}
	return ""
}

func resourceTypeFromLeader(raw string) *hubv1.ResourceType {
	if len(raw) < 8 {
		return &hubv1.ResourceType{Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED}
	}

	var original string
	rt := hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED
	switch raw[6] {
	case 'a', 't':
		original = "Text"
		rt = hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT
	case 'm':
		original = "Computer file"
		rt = hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE
	case 'e', 'f':
		original = "Map"
		rt = hubv1.ResourceTypeValue_RESOURCE_TYPE_MAP
	case 'g', 'k', 'r':
		original = "Visual material"
		rt = hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE
	case 'i', 'j':
		original = "Sound recording"
		rt = hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO
	}
	if raw[7] == 's' {
		original = "Serial"
		rt = hubv1.ResourceTypeValue_RESOURCE_TYPE_PERIODICAL
	}

	return &hubv1.ResourceType{
		Type:       rt,
		Original:   original,
		Vocabulary: "marc21/leader",
	}
}

func dateFromString(value string, dateType hubv1.DateType) *hubv1.DateValue {
	date := &hubv1.DateValue{
		Type: dateType,
		Raw:  value,
	}
	if year := yearPattern.FindString(value); year != "" {
		if parsed, err := strconv.ParseInt(year, 10, 32); err == nil {
			date.Year = int32(parsed)
			date.Precision = hubv1.DatePrecision_DATE_PRECISION_YEAR
		}
	}
	return date
}

func firstSubfield(f marcfile.Field, code string) string {
	for _, sub := range f.SubFields {
		if sub.Code == code {
			return sub.Value
		}
	}
	return ""
}

func subfieldValues(f marcfile.Field, codes ...string) []string {
	var values []string
	for _, sub := range f.SubFields {
		for _, code := range codes {
			if sub.Code == code {
				if value := cleanMARCValue(sub.Value); value != "" {
					values = append(values, value)
				}
				break
			}
		}
	}
	return values
}

func joinSubfields(f marcfile.Field, sep string, codes ...string) string {
	var parts []string
	for _, sub := range f.SubFields {
		for _, code := range codes {
			if sub.Code == code {
				if value := cleanMARCValue(sub.Value); value != "" {
					parts = append(parts, value)
				}
				break
			}
		}
	}
	return strings.Join(parts, sep)
}

func cleanIdentifier(value string) string {
	value = cleanMARCValue(value)
	if i := strings.Index(value, " "); i > 0 && strings.HasPrefix(value, "978") {
		return value[:i]
	}
	return value
}

func cleanDateValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "[]")
	return cleanMARCValue(value)
}

func cleanMARCValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "/")
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, " ,.;:/")
	value = strings.TrimSpace(value)
	return value
}
