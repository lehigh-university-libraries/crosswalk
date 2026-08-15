package marc

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	marcfile "github.com/hectorcorrea/marcli/pkg/marc"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

const (
	marcRecordTerminator  = byte(0x1d)
	marcFieldTerminator   = byte(0x1e)
	marcSubfieldDelimiter = byte(0x1f)
	defaultLeader         = "00000nam a2200000 i 4500"
)

// Serialize writes hub records as MARC21 binary records.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	if opts != nil && opts.Pretty {
		return serializeXML(w, records)
	}

	for i, record := range records {
		data, err := hubToMARC(record)
		if err != nil {
			return fmt.Errorf("converting record %d to MARC: %w", i, err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

type xmlCollection struct {
	XMLName xml.Name    `xml:"collection"`
	Xmlns   string      `xml:"xmlns,attr"`
	Records []xmlRecord `xml:"record"`
}

type xmlRecord struct {
	Leader        string            `xml:"leader"`
	ControlFields []xmlControlField `xml:"controlfield"`
	DataFields    []xmlDataField    `xml:"datafield"`
}

type xmlControlField struct {
	Tag   string `xml:"tag,attr"`
	Value string `xml:",chardata"`
}

type xmlDataField struct {
	Tag       string        `xml:"tag,attr"`
	Ind1      string        `xml:"ind1,attr"`
	Ind2      string        `xml:"ind2,attr"`
	Subfields []xmlSubfield `xml:"subfield"`
}

type xmlSubfield struct {
	Code  string `xml:"code,attr"`
	Value string `xml:",chardata"`
}

func serializeXML(w io.Writer, records []*hubv1.Record) error {
	collection := xmlCollection{
		Xmlns:   "http://www.loc.gov/MARC21/slim",
		Records: make([]xmlRecord, 0, len(records)),
	}
	for i, record := range records {
		xmlRecord, err := hubToXMLRecord(record)
		if err != nil {
			return fmt.Errorf("converting record %d to MARCXML: %w", i, err)
		}
		collection.Records = append(collection.Records, xmlRecord)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	if err := encoder.Encode(collection); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	return nil
}

func hubToXMLRecord(record *hubv1.Record) (xmlRecord, error) {
	fields := hubToFields(record)
	data, err := encodeISO2709(fields, leaderForRecord(record))
	if err != nil {
		return xmlRecord{}, err
	}

	xmlRec := xmlRecord{Leader: string(data[:24])}
	for _, f := range fields {
		if f.IsControlField() {
			xmlRec.ControlFields = append(xmlRec.ControlFields, xmlControlField{
				Tag:   f.Tag,
				Value: f.Value,
			})
			continue
		}

		dataField := xmlDataField{
			Tag:  f.Tag,
			Ind1: f.Indicator1,
			Ind2: f.Indicator2,
		}
		for _, sub := range f.SubFields {
			if strings.TrimSpace(sub.Value) != "" {
				dataField.Subfields = append(dataField.Subfields, xmlSubfield{
					Code:  sub.Code,
					Value: sub.Value,
				})
			}
		}
		xmlRec.DataFields = append(xmlRec.DataFields, dataField)
	}
	return xmlRec, nil
}

func hubToMARC(record *hubv1.Record) ([]byte, error) {
	fields := hubToFields(record)
	return encodeISO2709(fields, leaderForRecord(record))
}

func hubToFields(record *hubv1.Record) []marcfile.Field {
	var fields []marcfile.Field

	if id := localIdentifier(record); id != "" {
		fields = append(fields, controlField("001", id))
	}
	fields = append(fields, controlField("005", time.Now().UTC().Format("20060102150405.0")))
	fields = append(fields, controlField("008", control008(record)))

	for _, id := range record.Identifiers {
		switch id.Type {
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
			fields = append(fields, dataField("020", " ", " ", subfield("a", id.Value)))
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
			fields = append(fields, dataField("022", " ", " ", subfield("a", id.Value)))
		case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
			fields = append(fields, dataField("024", "7", " ", subfield("a", id.Value), subfield("2", "doi")))
		case hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER:
			fields = append(fields, dataField("088", " ", " ", subfield("a", id.Value)))
		case hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER:
			fields = append(fields, dataField("050", " ", "4", subfield("a", id.Value)))
		}
	}

	fields = append(fields, contributorFields(record)...)

	if record.Title != "" {
		fields = append(fields, dataField("245", titleIndicator1(record), "0", subfield("a", record.Title)))
	}
	for _, title := range record.AltTitle {
		if title = strings.TrimSpace(title); title != "" {
			fields = append(fields, dataField("246", "3", " ", subfield("a", title)))
		}
	}

	publishers := hub.GetPublishers(record)
	if record.PlacePublished != "" || len(publishers) > 0 || primaryDateString(record) != "" {
		subs := []marcfile.SubField{subfield("a", record.PlacePublished)}
		for _, publisher := range publishers {
			subs = append(subs, subfield("b", publisher))
		}
		subs = append(subs, subfield("c", primaryDateString(record)))
		fields = append(fields, dataField("264", " ", "1", subs...))
	}

	if record.PhysicalDesc != "" {
		fields = append(fields, dataField("300", " ", " ", subfield("a", record.PhysicalDesc)))
	}
	if record.Abstract != "" {
		fields = append(fields, dataField("520", " ", " ", subfield("a", record.Abstract)))
	}

	for _, note := range record.Notes {
		if note = strings.TrimSpace(note); note != "" {
			fields = append(fields, dataField("500", " ", " ", subfield("a", note)))
		}
	}

	for _, rights := range record.Rights {
		if rights == nil {
			continue
		}
		fields = append(fields, dataField("540", " ", " ",
			subfield("a", hub.RightsString(rights)),
			subfield("u", rights.Uri),
		))
	}

	for _, subject := range record.Subjects {
		if subject == nil || strings.TrimSpace(subject.Value) == "" {
			continue
		}
		fields = append(fields, subjectField(subject))
	}
	for _, genre := range record.Genres {
		if genre == nil || strings.TrimSpace(genre.Value) == "" {
			continue
		}
		fields = append(fields, dataField("655", " ", subjectIndicator2(genre), subfield("a", genre.Value), subjectSourceSubfield(genre)))
	}

	for _, rel := range record.Relations {
		if rel == nil {
			continue
		}
		switch rel.Type {
		case hubv1.RelationType_RELATION_TYPE_PART_OF:
			fields = append(fields, dataField("773", "0", " ",
				subfield("t", rel.TargetTitle),
				subfield("w", rel.TargetId),
			))
		case hubv1.RelationType_RELATION_TYPE_IN_SERIES:
			fields = append(fields, dataField("830", " ", "0", subfield("a", rel.TargetTitle)))
		case hubv1.RelationType_RELATION_TYPE_HAS_FORMAT:
			fields = append(fields, dataField("776", "0", " ",
				subfield("t", rel.TargetTitle),
				subfield("w", rel.TargetId),
			))
		}
	}

	for _, id := range record.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_URL {
			fields = append(fields, dataField("856", "4", "0", subfield("u", id.Value)))
		}
	}

	return compactFields(fields)
}

func contributorFields(record *hubv1.Record) []marcfile.Field {
	var fields []marcfile.Field
	mainAdded := false
	for _, c := range record.Contributors {
		if c == nil || strings.TrimSpace(c.Name) == "" {
			continue
		}

		isPerson := c.Type != hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
		tag := "700"
		if !isPerson {
			tag = "710"
		}
		if !mainAdded && creatorLike(c) {
			if isPerson {
				tag = "100"
			} else {
				tag = "110"
			}
			mainAdded = true
		}

		ind1 := "1"
		if !isPerson {
			ind1 = "2"
		}
		name := c.Name
		if isPerson && c.ParsedName != nil {
			name = hub.ParsedNameInverted(c.ParsedName)
		}
		code := contributorCode(c)
		subs := []marcfile.SubField{subfield("a", name)}
		if code != "" {
			subs = append(subs, subfield("4", code))
		} else if c.Role != "" {
			subs = append(subs, subfield("e", strings.ToLower(c.Role)))
		}
		fields = append(fields, dataField(tag, ind1, " ", subs...))
	}
	return fields
}

func subjectField(subject *hubv1.Subject) marcfile.Field {
	tag := "650"
	switch subject.Type {
	case hubv1.SubjectType_SUBJECT_TYPE_NAME:
		tag = "600"
	case hubv1.SubjectType_SUBJECT_TYPE_GEOGRAPHIC:
		tag = "651"
	case hubv1.SubjectType_SUBJECT_TYPE_TEMPORAL:
		tag = "648"
	case hubv1.SubjectType_SUBJECT_TYPE_TITLE:
		tag = "630"
	case hubv1.SubjectType_SUBJECT_TYPE_GENRE:
		tag = "655"
	}
	return dataField(tag, " ", subjectIndicator2(subject), subfield("a", subject.Value), subjectSourceSubfield(subject))
}

func encodeISO2709(fields []marcfile.Field, leader string) ([]byte, error) {
	leaderBytes := []byte(leader)
	if len(leaderBytes) != 24 {
		return nil, fmt.Errorf("leader must be 24 bytes, got %d", len(leaderBytes))
	}

	var directory []byte
	var fieldData []byte
	for _, f := range fields {
		content := fieldContent(f)
		length := len(content) + 1
		offset := len(fieldData)
		if length > 9999 || offset > 99999 {
			return nil, fmt.Errorf("record too large for MARC directory")
		}
		directory = append(directory, []byte(fmt.Sprintf("%3s%04d%05d", f.Tag, length, offset))...)
		fieldData = append(fieldData, content...)
		fieldData = append(fieldData, marcFieldTerminator)
	}

	baseAddress := len(leaderBytes) + len(directory) + 1
	recordLength := baseAddress + len(fieldData) + 1
	if recordLength > 99999 {
		return nil, fmt.Errorf("record too large for MARC leader")
	}

	copy(leaderBytes[0:5], []byte(fmt.Sprintf("%05d", recordLength)))
	copy(leaderBytes[12:17], []byte(fmt.Sprintf("%05d", baseAddress)))

	out := make([]byte, 0, recordLength)
	out = append(out, leaderBytes...)
	out = append(out, directory...)
	out = append(out, marcFieldTerminator)
	out = append(out, fieldData...)
	out = append(out, marcRecordTerminator)
	return out, nil
}

func fieldContent(f marcfile.Field) []byte {
	if f.IsControlField() {
		return []byte(f.Value)
	}

	ind1 := indicator(f.Indicator1)
	ind2 := indicator(f.Indicator2)
	data := []byte{ind1, ind2}
	for _, sub := range f.SubFields {
		if strings.TrimSpace(sub.Value) == "" {
			continue
		}
		data = append(data, marcSubfieldDelimiter)
		data = append(data, []byte(sub.Code)...)
		data = append(data, []byte(sub.Value)...)
	}
	return data
}

func leaderForRecord(record *hubv1.Record) string {
	leader := []byte(defaultLeader)
	if record != nil && record.ResourceType != nil {
		switch record.ResourceType.Type {
		case hubv1.ResourceTypeValue_RESOURCE_TYPE_MAP:
			leader[6] = 'e'
		case hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE, hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET, hubv1.ResourceTypeValue_RESOURCE_TYPE_INTERACTIVE:
			leader[6] = 'm'
		case hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE, hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO:
			leader[6] = 'g'
		case hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO:
			leader[6] = 'i'
		default:
			leader[6] = 'a'
		}
		if record.ResourceType.Type == hubv1.ResourceTypeValue_RESOURCE_TYPE_PERIODICAL ||
			record.ResourceType.Type == hubv1.ResourceTypeValue_RESOURCE_TYPE_JOURNAL {
			leader[7] = 's'
		}
	}
	return string(leader)
}

func control008(record *hubv1.Record) string {
	date := primaryDateString(record)
	year := "    "
	if len(date) >= 4 {
		year = date[:4]
	}
	lang := strings.TrimSpace(record.GetLanguage())
	if lang == "" {
		lang = "eng"
	}
	if len(lang) > 3 {
		lang = lang[:3]
	}
	return fmt.Sprintf("%s%s%s", time.Now().UTC().Format("060102"), "s"+year+"    xxu           000 0 ", fmt.Sprintf("%-3s d", lang))
}

func compactFields(fields []marcfile.Field) []marcfile.Field {
	result := make([]marcfile.Field, 0, len(fields))
	for _, f := range fields {
		if f.Tag == "" {
			continue
		}
		if f.IsControlField() {
			if strings.TrimSpace(f.Value) != "" {
				result = append(result, f)
			}
			continue
		}
		if len(f.SubFields) > 0 {
			result = append(result, f)
		}
	}
	return result
}

func controlField(tag, value string) marcfile.Field {
	return marcfile.Field{Tag: tag, Value: value}
}

func dataField(tag, ind1, ind2 string, subs ...marcfile.SubField) marcfile.Field {
	field := marcfile.Field{Tag: tag, Indicator1: indicatorString(ind1), Indicator2: indicatorString(ind2)}
	for _, sub := range subs {
		if strings.TrimSpace(sub.Value) != "" {
			field.SubFields = append(field.SubFields, sub)
		}
	}
	return field
}

func subfield(code, value string) marcfile.SubField {
	return marcfile.SubField{Code: code, Value: strings.TrimSpace(value)}
}

func subjectSourceSubfield(subject *hubv1.Subject) marcfile.SubField {
	switch subject.Vocabulary {
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_FAST:
		return subfield("2", "fast")
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH:
		return subfield("2", "mesh")
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT:
		return subfield("2", "aat")
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL:
		return subfield("2", "local")
	default:
		return marcfile.SubField{}
	}
}

func subjectIndicator2(subject *hubv1.Subject) string {
	switch subject.Vocabulary {
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH:
		return "0"
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH:
		return "2"
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_FAST,
		hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT,
		hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL:
		return "7"
	default:
		return " "
	}
}

func localIdentifier(record *hubv1.Record) string {
	if record.SourceInfo != nil && record.SourceInfo.SourceId != "" {
		return record.SourceInfo.SourceId
	}
	if id := hub.GetIdentifier(record, hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL); id != nil {
		return id.Value
	}
	return ""
}

func primaryDateString(record *hubv1.Record) string {
	if date := hub.PrimaryDate(record); date != nil {
		return hub.DateString(date)
	}
	return ""
}

func titleIndicator1(record *hubv1.Record) string {
	if len(record.Contributors) > 0 {
		return "1"
	}
	return "0"
}

func creatorLike(c *hubv1.Contributor) bool {
	code := contributorCode(c)
	return code == "aut" || code == "cre" || code == "" || c.Role == "author" || c.Role == "creator"
}

func contributorCode(c *hubv1.Contributor) string {
	if c == nil {
		return ""
	}
	if code := helpers.NormalizeRole(c.RoleCode); code != "" {
		return code
	}
	return helpers.NormalizeRole(c.Role)
}

func indicator(value string) byte {
	if value == "" {
		return ' '
	}
	return value[0]
}

func indicatorString(value string) string {
	if value == "" {
		return " "
	}
	return value[:1]
}
