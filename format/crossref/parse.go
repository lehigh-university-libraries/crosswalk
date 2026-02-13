package crossref

import (
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/format/protoxml"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	crossrefv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/crossref/v5_3_1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

// Parse reads CrossRef deposit XML and returns hub records.
// It parses the entire doi_batch, then walks the body to extract
// individual record-level messages (articles, books, dissertations, etc.).
func (f *Format) Parse(r io.Reader, _ *format.ParseOptions) ([]*hubv1.Record, error) {
	deposit := &crossrefv1.Deposit{}
	if err := protoxml.UnmarshalReader(r, deposit); err != nil {
		return nil, fmt.Errorf("unmarshaling crossref deposit: %w", err)
	}

	body := deposit.GetBody()
	if body == nil {
		return nil, fmt.Errorf("crossref deposit has no body")
	}

	var records []*hubv1.Record

	// Journals: extract articles with journal-level context
	for _, j := range body.GetJournal() {
		recs, err := extractJournalRecords(j)
		if err != nil {
			return nil, fmt.Errorf("extracting journal records: %w", err)
		}
		records = append(records, recs...)
	}

	// Books: extract book-level and chapter-level records
	for _, b := range body.GetBook() {
		recs, err := extractBookRecords(b)
		if err != nil {
			return nil, fmt.Errorf("extracting book records: %w", err)
		}
		records = append(records, recs...)
	}

	// Conferences: extract conference paper records
	for _, c := range body.GetConference() {
		recs, err := extractConferenceRecords(c)
		if err != nil {
			return nil, fmt.Errorf("extracting conference records: %w", err)
		}
		records = append(records, recs...)
	}

	// Datasets
	for _, ds := range body.GetDataset() {
		rec := datasetToHub(ds)
		records = append(records, rec)
	}

	// Dissertations
	for _, diss := range body.GetDissertation() {
		rec := dissertationToHub(diss)
		records = append(records, rec)
	}

	// Posted content
	for _, pc := range body.GetPostedContent() {
		rec := postedContentToHub(pc)
		records = append(records, rec)
	}

	// Peer reviews
	for _, pr := range body.GetPeerReview() {
		rec := peerReviewToHub(pr)
		records = append(records, rec)
	}

	// Set source info on all records
	for _, rec := range records {
		rec.SourceInfo = &hubv1.SourceInfo{
			Format:        "crossref",
			FormatVersion: Version,
		}
		// Use DOI as source ID when available
		for _, id := range rec.Identifiers {
			if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI {
				rec.SourceInfo.SourceId = id.Value
				break
			}
		}
	}

	return records, nil
}

// extractJournalRecords pulls articles from a journal, enriching each with
// journal-level metadata (title, ISSN).
func extractJournalRecords(j *crossrefv1.Journal) ([]*hubv1.Record, error) {
	var records []*hubv1.Record

	jm := j.GetJournalMetadata()

	for _, article := range j.GetJournalArticle() {
		rec := journalArticleToHub(article)

		// Enrich with journal-level metadata
		if jm != nil {
			addJournalMetadata(rec, jm)
		}

		// Enrich with issue-level metadata (volume, issue) from the first issue
		if len(j.GetJournalIssue()) > 0 {
			addIssueMetadata(rec, j.GetJournalIssue()[0])
		}

		records = append(records, rec)
	}

	// If no articles but there are issues, the journal itself is a container
	// without extractable records -- skip it.
	return records, nil
}

// extractBookRecords pulls both book-level and chapter-level records from a book.
func extractBookRecords(b *crossrefv1.Book) ([]*hubv1.Record, error) {
	var records []*hubv1.Record

	// If the book has content items (chapters), each chapter becomes a record
	for _, item := range b.GetContentItem() {
		rec := contentItemToHub(item)

		// Enrich chapter with book-level metadata as a relation
		if bm := b.GetBookMetadata(); bm != nil {
			rel := &hubv1.Relation{
				Type:        hubv1.RelationType_RELATION_TYPE_PART_OF,
				TargetTitle: extractTitle(bm.GetTitles()),
			}
			if bm.GetDoiData() != nil && bm.GetDoiData().GetDoi() != "" {
				rel.TargetId = bm.GetDoiData().GetDoi()
				rel.TargetIdType = hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
			}
			rec.Relations = append(rec.Relations, rel)
		}

		records = append(records, rec)
	}

	// If there are no content items, the book itself is the record
	if len(b.GetContentItem()) == 0 && b.GetBookMetadata() != nil {
		rec := bookMetadataToHub(b.GetBookMetadata())
		rec.ResourceType = &hubv1.ResourceType{
			Type:     hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK,
			Original: b.GetBookType(),
		}
		records = append(records, rec)
	}

	return records, nil
}

// extractConferenceRecords pulls papers from a conference proceeding.
func extractConferenceRecords(c *crossrefv1.Conference) ([]*hubv1.Record, error) {
	var records []*hubv1.Record

	for _, paper := range c.GetConferencePaper() {
		rec := conferencePaperToHub(paper)

		// Enrich with proceedings-level metadata
		if pm := c.GetProceedingsMetadata(); pm != nil {
			rel := &hubv1.Relation{
				Type:        hubv1.RelationType_RELATION_TYPE_PART_OF,
				TargetTitle: pm.GetProceedingsTitle(),
			}
			rec.Relations = append(rec.Relations, rel)
		}

		// Enrich with event metadata
		if em := c.GetEventMetadata(); em != nil {
			if em.GetConferenceName() != "" {
				hub.SetExtra(rec, "conference_name", em.GetConferenceName())
			}
			if em.GetConferenceDate() != "" {
				hub.SetExtra(rec, "conference_date", em.GetConferenceDate())
			}
			if em.GetConferenceLocation() != "" {
				hub.SetExtra(rec, "conference_location", em.GetConferenceLocation())
			}
		}

		records = append(records, rec)
	}

	return records, nil
}

// journalArticleToHub converts a JournalArticle to a hub record.
func journalArticleToHub(article *crossrefv1.JournalArticle) *hubv1.Record {
	rec := &hubv1.Record{
		Title: extractTitle(article.GetTitles()),
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		},
		Abstract: article.GetAbstract(),
	}

	rec.Contributors = extractContributors(article.GetContributors())
	rec.Dates = appendPublicationDate(rec.Dates, article.GetPublicationDate(), hubv1.DateType_DATE_TYPE_ISSUED)
	rec.Identifiers = appendDoiIdentifier(rec.Identifiers, article.GetDoiData())

	addPages(rec, article.GetPages())

	return rec
}

// bookMetadataToHub converts BookMetadata to a hub record.
func bookMetadataToHub(bm *crossrefv1.BookMetadata) *hubv1.Record {
	rec := &hubv1.Record{
		Title: extractTitle(bm.GetTitles()),
	}

	rec.Contributors = extractContributors(bm.GetContributors())
	rec.Dates = appendPublicationDate(rec.Dates, bm.GetPublicationDate(), hubv1.DateType_DATE_TYPE_ISSUED)
	rec.Identifiers = appendDoiIdentifier(rec.Identifiers, bm.GetDoiData())

	if bm.GetIsbnPrint() != "" {
		rec.Identifiers = append(rec.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN,
			Value: bm.GetIsbnPrint(),
		})
	}
	if bm.GetIsbnElectronic() != "" {
		rec.Identifiers = append(rec.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN,
			Value: bm.GetIsbnElectronic(),
		})
	}

	if pub := bm.GetPublisher(); pub != nil {
		rec.Publisher = pub.GetPublisherName()
		rec.PlacePublished = pub.GetPublisherPlace()
	}

	if bm.GetEditionNumber() != "" {
		rec.Edition = bm.GetEditionNumber()
	}

	return rec
}

// contentItemToHub converts a ContentItem (book chapter) to a hub record.
func contentItemToHub(item *crossrefv1.ContentItem) *hubv1.Record {
	rec := &hubv1.Record{
		Title: extractTitle(item.GetTitles()),
		ResourceType: &hubv1.ResourceType{
			Type:     hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER,
			Original: item.GetComponentType(),
		},
	}

	rec.Contributors = extractContributors(item.GetContributors())
	rec.Dates = appendPublicationDate(rec.Dates, item.GetPublicationDate(), hubv1.DateType_DATE_TYPE_ISSUED)
	rec.Identifiers = appendDoiIdentifier(rec.Identifiers, item.GetDoiData())

	addPages(rec, item.GetPages())

	return rec
}

// conferencePaperToHub converts a ConferencePaper to a hub record.
func conferencePaperToHub(paper *crossrefv1.ConferencePaper) *hubv1.Record {
	rec := &hubv1.Record{
		Title: extractTitle(paper.GetTitles()),
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER,
		},
		Abstract: paper.GetAbstract(),
	}

	rec.Contributors = extractContributors(paper.GetContributors())
	rec.Dates = appendPublicationDate(rec.Dates, paper.GetPublicationDate(), hubv1.DateType_DATE_TYPE_ISSUED)
	rec.Identifiers = appendDoiIdentifier(rec.Identifiers, paper.GetDoiData())

	addPages(rec, paper.GetPages())

	return rec
}

// datasetToHub converts a Dataset to a hub record.
func datasetToHub(ds *crossrefv1.Dataset) *hubv1.Record {
	rec := &hubv1.Record{
		Title: extractTitle(ds.GetTitles()),
		ResourceType: &hubv1.ResourceType{
			Type:     hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET,
			Original: ds.GetDatasetType(),
		},
		Abstract: ds.GetDescription(),
	}

	rec.Contributors = extractContributors(ds.GetContributors())
	rec.Dates = appendPublicationDate(rec.Dates, ds.GetPublicationDate(), hubv1.DateType_DATE_TYPE_ISSUED)
	rec.Identifiers = appendDoiIdentifier(rec.Identifiers, ds.GetDoiData())

	return rec
}

// dissertationToHub converts a Dissertation to a hub record.
func dissertationToHub(diss *crossrefv1.Dissertation) *hubv1.Record {
	rec := &hubv1.Record{
		Title: extractTitle(diss.GetTitles()),
		ResourceType: &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION,
		},
		Abstract: diss.GetAbstract(),
	}

	// Dissertation has a single PersonName, not Contributors
	if pn := diss.GetPersonName(); pn != nil {
		rec.Contributors = append(rec.Contributors, personNameToContributor(pn))
	}

	rec.Dates = appendPublicationDate(rec.Dates, diss.GetApprovalDate(), hubv1.DateType_DATE_TYPE_ACCEPTED)
	rec.Identifiers = appendDoiIdentifier(rec.Identifiers, diss.GetDoiData())

	// Institution and degree
	if inst := diss.GetInstitution(); inst != nil {
		if rec.DegreeInfo == nil {
			rec.DegreeInfo = &hubv1.DegreeInfo{}
		}
		rec.DegreeInfo.Institution = inst.GetInstitutionName()
		if inst.GetInstitutionDepartment() != "" {
			rec.DegreeInfo.Department = inst.GetInstitutionDepartment()
		}
	}
	if diss.GetDegree() != "" {
		if rec.DegreeInfo == nil {
			rec.DegreeInfo = &hubv1.DegreeInfo{}
		}
		rec.DegreeInfo.DegreeName = diss.GetDegree()
	}

	return rec
}

// postedContentToHub converts PostedContent to a hub record.
func postedContentToHub(pc *crossrefv1.PostedContent) *hubv1.Record {
	rec := &hubv1.Record{
		Title: extractTitle(pc.GetTitles()),
		ResourceType: &hubv1.ResourceType{
			Type:     hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
			Original: pc.GetType(),
		},
		Abstract: pc.GetAbstract(),
	}

	rec.Contributors = extractContributors(pc.GetContributors())
	rec.Dates = appendPublicationDate(rec.Dates, pc.GetPostedDate(), hubv1.DateType_DATE_TYPE_ISSUED)
	rec.Identifiers = appendDoiIdentifier(rec.Identifiers, pc.GetDoiData())

	if pc.GetGroupTitle() != "" {
		rec.Subjects = append(rec.Subjects, &hubv1.Subject{
			Value:      pc.GetGroupTitle(),
			Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL,
		})
	}

	return rec
}

// peerReviewToHub converts a PeerReview to a hub record.
func peerReviewToHub(pr *crossrefv1.PeerReview) *hubv1.Record {
	rec := &hubv1.Record{
		Title: extractTitle(pr.GetTitles()),
		ResourceType: &hubv1.ResourceType{
			Type:     hubv1.ResourceTypeValue_RESOURCE_TYPE_PEER_REVIEW,
			Original: pr.GetType(),
		},
	}

	rec.Contributors = extractContributors(pr.GetContributors())
	rec.Dates = appendPublicationDate(rec.Dates, pr.GetReviewDate(), hubv1.DateType_DATE_TYPE_ISSUED)
	rec.Identifiers = appendDoiIdentifier(rec.Identifiers, pr.GetDoiData())

	if pr.GetStage() != "" {
		hub.SetExtra(rec, "review_stage", pr.GetStage())
	}

	return rec
}

// addJournalMetadata enriches a record with journal-level metadata.
func addJournalMetadata(rec *hubv1.Record, jm *crossrefv1.JournalMetadata) {
	if jm.GetFullTitle() != "" {
		rec.Relations = append(rec.Relations, &hubv1.Relation{
			Type:        hubv1.RelationType_RELATION_TYPE_PART_OF,
			TargetTitle: jm.GetFullTitle(),
		})
	}

	if jm.GetIssnPrint() != "" {
		rec.Identifiers = append(rec.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN,
			Value: jm.GetIssnPrint(),
		})
	}
	if jm.GetIssnElectronic() != "" {
		rec.Identifiers = append(rec.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN,
			Value: jm.GetIssnElectronic(),
		})
	}
}

// addIssueMetadata enriches a record with issue-level metadata.
func addIssueMetadata(rec *hubv1.Record, issue *crossrefv1.JournalIssue) {
	if issue.GetVolume() != "" {
		hub.SetExtra(rec, "volume", issue.GetVolume())
	}
	if issue.GetIssue() != "" {
		hub.SetExtra(rec, "issue", issue.GetIssue())
	}
}

// extractTitle returns the main title from a Titles message.
func extractTitle(titles *crossrefv1.Titles) string {
	if titles == nil {
		return ""
	}
	t := titles.GetTitle()
	if sub := titles.GetSubtitle(); sub != "" {
		t += ": " + sub
	}
	return t
}

// extractContributors converts a CrossRef Contributors message to hub contributors.
func extractContributors(contribs *crossrefv1.Contributors) []*hubv1.Contributor {
	if contribs == nil {
		return nil
	}

	var result []*hubv1.Contributor
	for _, pn := range contribs.GetPersonName() {
		result = append(result, personNameToContributor(pn))
	}
	for _, org := range contribs.GetOrganization() {
		c := &hubv1.Contributor{
			Name: org.GetName(),
			Role: org.GetContributorRole(),
			Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION,
		}
		result = append(result, c)
	}
	return result
}

// personNameToContributor converts a PersonName to a hub Contributor.
func personNameToContributor(pn *crossrefv1.PersonName) *hubv1.Contributor {
	c := &hubv1.Contributor{
		Role: pn.GetContributorRole(),
		Type: hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON,
	}

	given := pn.GetGivenName()
	family := pn.GetSurname()
	suffix := pn.GetSuffix()

	if given != "" || family != "" {
		c.ParsedName = &hubv1.ParsedName{
			Given:  given,
			Family: family,
			Suffix: suffix,
		}
	}

	// Build display name from components
	c.Name = buildName(family, given, suffix)

	// ORCID
	if pn.GetOrcid() != "" {
		orcid := pn.GetOrcid()
		// Strip common URI prefixes
		orcid = strings.TrimPrefix(orcid, "https://orcid.org/")
		orcid = strings.TrimPrefix(orcid, "http://orcid.org/")
		c.Identifiers = append(c.Identifiers, &hubv1.Identifier{
			Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID,
			Value: orcid,
		})
	}

	// Affiliations
	for _, aff := range pn.GetAffiliation() {
		if aff.GetName() != "" {
			c.Affiliations = append(c.Affiliations, &hubv1.Affiliation{
				Name: aff.GetName(),
			})
		}
	}

	return c
}

// buildName creates a display name in "Family, Given" format.
func buildName(family, given, suffix string) string {
	if family == "" && given == "" {
		return ""
	}
	var parts []string
	if family != "" {
		parts = append(parts, family)
	}
	if given != "" {
		if len(parts) > 0 {
			parts[0] += ","
		}
		parts = append(parts, given)
	}
	if suffix != "" {
		parts = append(parts, suffix)
	}
	return strings.Join(parts, " ")
}

// appendPublicationDate appends a publication date to the dates slice.
func appendPublicationDate(dates []*hubv1.DateValue, pd *crossrefv1.PublicationDate, dateType hubv1.DateType) []*hubv1.DateValue {
	if pd == nil || pd.GetYear() == 0 {
		return dates
	}

	dv := &hubv1.DateValue{
		Type:  dateType,
		Year:  pd.GetYear(),
		Month: pd.GetMonth(),
		Day:   pd.GetDay(),
	}

	// Set precision based on available components
	switch {
	case pd.GetDay() > 0:
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_DAY
	case pd.GetMonth() > 0:
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_MONTH
	default:
		dv.Precision = hubv1.DatePrecision_DATE_PRECISION_YEAR
	}

	// Build raw date string
	raw := fmt.Sprintf("%d", pd.GetYear())
	if pd.GetMonth() > 0 {
		raw = fmt.Sprintf("%d-%02d", pd.GetYear(), pd.GetMonth())
	}
	if pd.GetDay() > 0 {
		raw = fmt.Sprintf("%d-%02d-%02d", pd.GetYear(), pd.GetMonth(), pd.GetDay())
	}
	dv.Raw = raw

	return append(dates, dv)
}

// appendDoiIdentifier appends a DOI identifier from DoiData.
func appendDoiIdentifier(ids []*hubv1.Identifier, dd *crossrefv1.DoiData) []*hubv1.Identifier {
	if dd == nil || dd.GetDoi() == "" {
		return ids
	}
	return append(ids, &hubv1.Identifier{
		Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_DOI,
		Value: dd.GetDoi(),
	})
}

// addPages adds page information to a record's extra fields.
func addPages(rec *hubv1.Record, pages *crossrefv1.Pages) {
	if pages == nil {
		return
	}
	if pages.GetFirstPage() != "" {
		hub.SetExtra(rec, "first_page", pages.GetFirstPage())
	}
	if pages.GetLastPage() != "" {
		hub.SetExtra(rec, "last_page", pages.GetLastPage())
	}
}
