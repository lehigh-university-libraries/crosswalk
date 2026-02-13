package proquest

import (
	"fmt"
	"io"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/format/protoxml"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	pqv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/proquest/v1"
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
		record := spokeToHub(sub)
		records = append(records, record)
	}

	return records, nil
}

// spokeToHub converts a ProQuest spoke Submission to a hub Record.
func spokeToHub(sub *pqv1.Submission) *hubv1.Record {
	record := &hubv1.Record{
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION,
		},
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
	}

	// Content
	if sub.Content != nil {
		mapContent(record, sub.Content)
	}

	// Repository embargo
	if sub.Repository != nil && sub.Repository.Embargo != "" {
		record.AccessCondition = sub.Repository.Embargo
	}

	return record
}

// authorToContributor converts a ProQuest Author to a hub Contributor.
func authorToContributor(author *pqv1.Author) *hubv1.Contributor {
	c := &hubv1.Contributor{
		Role: "author",
		Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
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

	if c.Name == "" && c.ParsedName == nil {
		return nil
	}

	return c
}

// advisorToContributor converts a ProQuest Advisor to a hub Contributor.
func advisorToContributor(advisor *pqv1.Advisor) *hubv1.Contributor {
	c := &hubv1.Contributor{
		Role: "advisor",
		Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
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
	record.Title = desc.Title
	record.PageCount = desc.PageCount

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
		record.Dates = append(record.Dates, &hubv1.DateValue{
			Type: hubv1.DateType_DATE_TYPE_ACCEPTED,
			Raw:  dates.AcceptDate,
		})
	}

	if dates.CompletionDate != "" {
		record.Dates = append(record.Dates, &hubv1.DateValue{
			Type: hubv1.DateType_DATE_TYPE_ISSUED,
			Raw:  dates.CompletionDate,
		})
	}
}

// mapContent maps ProQuest Content to hub record fields.
func mapContent(record *hubv1.Record, content *pqv1.Content) {
	if content.Abstract != nil && len(content.Abstract.Paragraphs) > 0 {
		record.Abstract = strings.Join(content.Abstract.Paragraphs, "\n\n")
	}
}
