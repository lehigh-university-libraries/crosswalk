package proquest

import (
	"fmt"
	"io"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/format/protoxml"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	pqv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/proquest/v1"
	pqcomputed "github.com/lehigh-university-libraries/crosswalk/spoke/proquest/v1"
)

// Parse reads ProQuest ETD XML and returns hub records.
// Each DISS_submission element in the input produces one hub record.
func (f *Format) Parse(r io.Reader, _ *format.ParseOptions) ([]*hubv1.Record, error) {
	msgs, err := protoxml.UnmarshalAll(r, func() proto.Message {
		return &pqv1.Submission{}
	})
	if err != nil {
		return nil, fmt.Errorf("unmarshaling ProQuest XML: %w", err)
	}

	if len(msgs) == 0 {
		return nil, fmt.Errorf("no DISS_submission elements found in input")
	}

	records := make([]*hubv1.Record, 0, len(msgs))
	for i, msg := range msgs {
		sub, ok := msg.(*pqv1.Submission)
		if !ok {
			return nil, fmt.Errorf("submission %d: unexpected message type", i)
		}
		record, err := spokeToHub(sub)
		if err != nil {
			return nil, fmt.Errorf("submission %d: %w", i+1, err)
		}
		records = append(records, record)
	}

	return records, nil
}

// spokeToHub converts a ProQuest spoke Submission to a hub Record.
func spokeToHub(sub *pqv1.Submission) (*hubv1.Record, error) {
	record := &hubv1.Record{
		SourceInfo: &hubv1.SourceInfo{
			Format:        "proquest",
			FormatVersion: Version,
		},
	}

	// Authorship
	if sub.Authorship != nil {
		for _, author := range sub.Authorship.Authors {
			c := authorToContributor(author)
			if c != nil {
				record.Contributors = append(record.Contributors, c)
			}
		}
	}

	// Description
	if sub.Description != nil {
		mapDescription(record, sub.Description)
		applyContributorContext(record)
	}

	// Content
	if sub.Content != nil {
		mapContent(record, sub.Content)
	}

	if err := pqcomputed.ComputeEmbargoDate(sub, record); err != nil {
		return nil, fmt.Errorf("computing embargo date: %w", err)
	}

	return record, nil
}

// authorToContributor converts a ProQuest Author to a hub Contributor.
func authorToContributor(author *pqv1.Author) *hubv1.Contributor {
	if author == nil {
		return nil
	}
	c := &hubv1.Contributor{
		Role:     "author",
		RoleCode: "relators:cre",
		Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		Status:   "Graduate Student",
	}

	if author.Name != nil {
		c.ParsedName = nameToHub(author.Name)
		c.Name = buildDisplayName(c.ParsedName)
	}

	if author.Orcid != "" {
		c.Identifiers = append(c.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID,
			Value: author.Orcid,
		})
	}
	for _, contact := range author.Contacts {
		if contact != nil && contact.Email != "" {
			c.Email = contact.Email
			break
		}
	}

	if c.Name == "" && c.ParsedName == nil {
		return nil
	}

	return c
}

// advisorToContributor converts a ProQuest Advisor to a hub Contributor.
func advisorToContributor(advisor *pqv1.Advisor) *hubv1.Contributor {
	if advisor == nil {
		return nil
	}
	c := &hubv1.Contributor{
		Role:     "advisor",
		RoleCode: "relators:ths",
		Type:     hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
		Status:   "Faculty",
	}

	if advisor.Name != nil {
		c.ParsedName = nameToHub(advisor.Name)
		c.Name = buildDisplayName(c.ParsedName)
	}

	if c.Name == "" && c.ParsedName == nil {
		return nil
	}

	return c
}

// nameToHub converts a ProQuest Name to a hub ParsedName.
func nameToHub(name *pqv1.Name) *hubv1.ParsedName {
	return &hubv1.ParsedName{
		Family: name.Surname,
		Given:  name.First,
		Middle: name.Middle,
		Suffix: name.Suffix,
	}
}

// buildDisplayName creates "Family, Given" from parsed name components.
func buildDisplayName(pn *hubv1.ParsedName) string {
	if pn.Family == "" && pn.Given == "" {
		return ""
	}
	if pn.Family != "" && pn.Given != "" {
		return pn.Family + ", " + pn.Given
	}
	if pn.Family != "" {
		return pn.Family
	}
	return pn.Given
}

// mapDescription maps ProQuest Description fields to the hub record.
func mapDescription(record *hubv1.Record, desc *pqv1.Description) {
	fullTitle := desc.Title
	record.FullTitle = fullTitle
	record.Title = fullTitle
	record.PageCount = desc.PageCount
	mapETDType(record, desc)

	// Degree info
	if desc.Degree != "" || desc.DegreeLevel != "" || desc.Discipline != "" ||
		desc.Institution != nil {
		record.DegreeInfo = &hubv1.DegreeInfo{
			DegreeName:  desc.Degree,
			DegreeLevel: desc.DegreeLevel,
			Department:  desc.Discipline,
		}
		if desc.Institution != nil {
			record.DegreeInfo.Institution = desc.Institution.Name
			if department := strings.TrimSpace(desc.Institution.Department); department != "" {
				record.Departments = append(record.Departments, department)
			}
		}
	}

	// Advisors
	for _, adv := range desc.Advisors {
		c := advisorToContributor(adv)
		if c != nil {
			record.Contributors = append(record.Contributors, c)
		}
	}

	// Categorization
	if desc.Categorization != nil {
		mapCategorization(record, desc.Categorization)
	}

	// Dates
	if desc.Dates != nil {
		mapDates(record, desc.Dates)
	}
}

func mapETDType(record *hubv1.Record, desc *pqv1.Description) {
	resourceType := hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS
	genre := "theses"
	if isDoctoralDegree(desc.Degree, desc.DegreeLevel) {
		resourceType = hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION
		genre = "dissertations"
	}
	record.ResourceType = &hubv1.ResourceType{Type: resourceType}
	record.Genres = append(record.Genres, &hubv1.Subject{
		Value:      genre,
		Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE,
		Type:       hubv1.SubjectType_SUBJECT_TYPE_GENRE,
	})
	record.DigitalOrigin = "born digital"
}

func isDoctoralDegree(degree, level string) bool {
	value := strings.ToLower(strings.TrimSpace(degree + " " + level))
	if strings.Contains(value, "doctor") {
		return true
	}

	// ProQuest commonly supplies abbreviated degree names. Remove punctuation
	// before matching so values such as "Ph.D." and "Ed.D." classify reliably.
	for _, candidate := range []string{degree, level} {
		compact := compactDegreeName(candidate)
		for _, abbreviation := range []string{"phd", "dphil", "edd", "dsc", "scd", "dma", "dba", "dnp", "psyd"} {
			if strings.HasPrefix(compact, abbreviation) {
				return true
			}
		}
	}
	return false
}

func compactDegreeName(value string) string {
	var normalized strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}

// mapCategorization maps ProQuest Categorization to hub subjects and language.
func mapCategorization(record *hubv1.Record, cat *pqv1.Categorization) {
	for _, kw := range cat.Keywords {
		record.Subjects = append(record.Subjects, &hubv1.Subject{
			Value:      kw,
			Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS,
		})
	}

	for _, c := range cat.Categories {
		if c.Description != "" {
			record.Subjects = append(record.Subjects, &hubv1.Subject{
				Value:      c.Description,
				Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL,
			})
		}
	}

	if cat.Language != "" {
		record.Language = cat.Language
	}
}

// mapDates maps ProQuest Dates to hub dates.
func mapDates(record *hubv1.Record, dates *pqv1.Dates) {
	if dates.AcceptDate != "" {
		record.Dates = append(record.Dates, parseProQuestDate(dates.AcceptDate, hubv1.DateType_DATE_TYPE_ACCEPTED))
	}

	if dates.CompletionDate != "" {
		record.Dates = append(record.Dates, parseProQuestDate(dates.CompletionDate, hubv1.DateType_DATE_TYPE_ISSUED))
	}
}

func parseProQuestDate(raw string, dateType hubv1.DateType) *hubv1.DateValue {
	raw = strings.TrimSpace(raw)
	date := &hubv1.DateValue{Type: dateType, Raw: raw}
	formats := []struct {
		layout    string
		precision hubv1.DatePrecision
	}{
		{"01/02/2006", hubv1.DatePrecision_DATE_PRECISION_DAY},
		{"2006-01-02", hubv1.DatePrecision_DATE_PRECISION_DAY},
		{"2006-01", hubv1.DatePrecision_DATE_PRECISION_MONTH},
		{"2006", hubv1.DatePrecision_DATE_PRECISION_YEAR},
	}
	for _, candidate := range formats {
		parsed, err := time.Parse(candidate.layout, raw)
		if err != nil {
			continue
		}
		date.Year = int32(parsed.Year())
		date.Precision = candidate.precision
		if candidate.precision == hubv1.DatePrecision_DATE_PRECISION_MONTH || candidate.precision == hubv1.DatePrecision_DATE_PRECISION_DAY {
			date.Month = int32(parsed.Month())
		}
		if candidate.precision == hubv1.DatePrecision_DATE_PRECISION_DAY {
			date.Day = int32(parsed.Day())
		}
		break
	}
	return date
}

func applyContributorContext(record *hubv1.Record) {
	if record.DegreeInfo == nil || record.DegreeInfo.Institution == "" {
		return
	}
	institution := record.DegreeInfo.Institution
	for _, contributor := range record.Contributors {
		if contributor.Affiliation == "" {
			contributor.Affiliation = institution
		}
		if len(contributor.Affiliations) == 0 {
			contributor.Affiliations = append(contributor.Affiliations, &hubv1.Affiliation{Name: institution})
		}
	}
}

// mapContent maps ProQuest Content to hub record fields.
func mapContent(record *hubv1.Record, content *pqv1.Content) {
	if content.Abstract != nil && len(content.Abstract.Paragraphs) > 0 {
		record.Abstract = strings.Join(content.Abstract.Paragraphs, "\n\n")
	}
	if content.Binary != nil && strings.TrimSpace(content.Binary.FileName) != "" {
		name := strings.TrimSpace(content.Binary.FileName)
		mimeType := strings.TrimSpace(content.Binary.Type)
		if strings.EqualFold(mimeType, "PDF") {
			mimeType = "application/pdf"
		}
		record.Files = append(record.Files, &hubv1.File{
			Path:     name,
			Name:     name,
			MimeType: mimeType,
			Role:     "primary",
		})
	}
}
