package crossref

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	crossrefv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/crossref/v5_3_1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

const (
	gettyArticleURI       = "http://vocab.getty.edu/page/aat/300048715"
	gettyJournalURI       = "http://vocab.getty.edu/page/aat/300215390"
	gettyIssueURI         = "http://vocab.getty.edu/page/aat/300312349"
	gettyVolumeURI        = "http://vocab.getty.edu/page/aat/300265632"
	gettyUnboundVolumeURI = "http://vocab.getty.edu/page/aat/300417904"
)

var doiWhitespaceRE = regexp.MustCompile(`\s+`)

// Serialize writes hub records as CrossRef deposit XML.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	if opts == nil {
		opts = format.NewSerializeOptions()
	}

	// Step 1: Convert hub records to spoke proto struct
	spokeDeposit, err := hubToSpoke(records, opts)
	if err != nil {
		return fmt.Errorf("converting records to spoke: %w", err)
	}

	// Step 2: Convert spoke struct to XML-marshalable types
	xmlDeposit := spokeToXML(spokeDeposit)

	// Step 3: Marshal to XML
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		return err
	}

	encoder := xml.NewEncoder(w)
	if opts.Pretty {
		encoder.Indent("", "  ")
	}

	return encoder.Encode(xmlDeposit)
}

// hubToSpoke converts hub records to the CrossRef spoke proto struct.
func hubToSpoke(records []*hubv1.Record, opts *format.SerializeOptions) (*crossrefv1.Deposit, error) {
	deposit := &crossrefv1.Deposit{
		Head: &crossrefv1.Head{
			DoiBatchId: fmt.Sprintf("batch_%d", time.Now().Unix()),
			Timestamp:  time.Now().Format("20060102150405"),
			Depositor: &crossrefv1.Depositor{
				DepositorName: "Crosswalk",
				EmailAddress:  "crosswalk@example.com",
			},
			Registrant: "Crosswalk",
		},
		Body: &crossrefv1.Body{},
	}

	for _, record := range records {
		// Determine content type from resource type
		if record.ResourceType == nil {
			if rel := findJournalParentRelation(record); rel != nil {
				addJournalArticle(deposit, record, rel, opts)
			}
			continue
		}

		if rel := findJournalParentRelation(record); rel != nil {
			addJournalArticle(deposit, record, rel, opts)
			continue
		}

		switch record.ResourceType.Type {
		case hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION,
			hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS:
			deposit.Body.Dissertation = append(deposit.Body.Dissertation, buildDissertation(record))

		case hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
			hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT:
			// Create a posted content record for preprints/articles without journal context
			deposit.Body.PostedContent = append(deposit.Body.PostedContent, buildPostedContent(record))

		case hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET:
			deposit.Body.Dataset = append(deposit.Body.Dataset, buildDataset(record))

		case hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK:
			deposit.Body.Book = append(deposit.Body.Book, buildBook(record))

		default:
			// Default to posted content for other types
			deposit.Body.PostedContent = append(deposit.Body.PostedContent, buildPostedContent(record))
		}
	}

	return deposit, nil
}

func addJournalArticle(deposit *crossrefv1.Deposit, record *hubv1.Record, rel *hubv1.Relation, opts *format.SerializeOptions) {
	context := journalContextFromRelation(rel)
	if context.Issue != nil && context.Issue.PublicationDate == nil {
		context.Issue.PublicationDate = publicationDateFromRecord(record)
	}
	key := context.JournalResource
	if key == "" {
		key = context.JournalTitle
	}

	for _, journal := range deposit.Body.Journal {
		if journal.JournalMetadata == nil {
			continue
		}
		journalKey := journal.JournalMetadata.FullTitle
		if journal.JournalMetadata.DoiData != nil && journal.JournalMetadata.DoiData.Resource != "" {
			journalKey = journal.JournalMetadata.DoiData.Resource
		}
		if journalKey == key {
			if context.Issue != nil {
				addJournalIssue(journal, context.Issue)
			}
			journal.JournalArticle = append(journal.JournalArticle, buildJournalArticle(record, opts))
			return
		}
	}

	journal := &crossrefv1.Journal{
		JournalMetadata: buildJournalMetadataFromContext(context),
		JournalArticle:  []*crossrefv1.JournalArticle{buildJournalArticle(record, opts)},
	}
	if context.Issue != nil {
		journal.JournalIssue = append(journal.JournalIssue, context.Issue)
	}
	deposit.Body.Journal = append(deposit.Body.Journal, journal)
}

func addJournalIssue(journal *crossrefv1.Journal, issue *crossrefv1.JournalIssue) {
	if issue == nil {
		return
	}
	key := ""
	if issue.DoiData != nil {
		key = issue.DoiData.Resource
		if key == "" {
			key = issue.DoiData.Doi
		}
	}
	if key == "" {
		key = issue.Volume + ":" + issue.Issue
	}
	for _, existing := range journal.JournalIssue {
		existingKey := ""
		if existing.DoiData != nil {
			existingKey = existing.DoiData.Resource
			if existingKey == "" {
				existingKey = existing.DoiData.Doi
			}
		}
		if existingKey == "" {
			existingKey = existing.Volume + ":" + existing.Issue
		}
		if existingKey == key {
			return
		}
	}
	journal.JournalIssue = append(journal.JournalIssue, issue)
}

func buildJournalMetadataFromContext(context journalContext) *crossrefv1.JournalMetadata {
	return &crossrefv1.JournalMetadata{
		FullTitle: context.JournalTitle,
		DoiData: &crossrefv1.DoiData{
			Doi:      context.JournalDOI,
			Resource: context.JournalResource,
		},
	}
}

func journalContextFromRelation(rel *hubv1.Relation) journalContext {
	meta := relationMetadata(rel)
	current := relationNodeFromRelation(rel, meta)
	context := journalContext{
		JournalTitle:    current.Title,
		JournalDOI:      current.DOI,
		JournalResource: current.Resource,
	}

	switch {
	case current.hasGenre(gettyJournalURI):
		return context
	case current.hasGenre(gettyIssueURI):
		context.Issue = current.toJournalIssue()
		if current.Parent != nil {
			parent := current.Parent
			context.JournalTitle = parent.Title
			context.JournalDOI = parent.DOI
			context.JournalResource = parent.Resource
		}
	case current.hasGenre(gettyVolumeURI) || current.hasGenre(gettyUnboundVolumeURI):
		return context
	default:
		if current.Parent != nil {
			parent := current.Parent
			if parent.hasGenre(gettyJournalURI) || parent.hasGenre(gettyVolumeURI) || parent.hasGenre(gettyUnboundVolumeURI) {
				context.JournalTitle = parent.Title
				context.JournalDOI = parent.DOI
				context.JournalResource = parent.Resource
			}
		}
	}

	return context
}

type journalContext struct {
	JournalTitle    string
	JournalDOI      string
	JournalResource string
	Issue           *crossrefv1.JournalIssue
}

func relationNodeFromRelation(rel *hubv1.Relation, meta relationMeta) relationNode {
	node := relationNode{
		Title:    meta.Title,
		DOI:      meta.DOI,
		Resource: meta.Resource,
		Genres:   meta.Genres,
	}
	if node.Title == "" && rel != nil {
		node.Title = rel.TargetTitle
	}
	if node.Resource == "" && rel != nil {
		node.Resource = rel.TargetUri
	}
	if meta.Parent != nil {
		node.Parent = relationNodeFromMeta(*meta.Parent)
	}
	return node
}

func relationNodeFromMeta(meta relationMeta) *relationNode {
	node := &relationNode{
		Title:    meta.Title,
		DOI:      meta.DOI,
		Resource: meta.Resource,
		Genres:   meta.Genres,
	}
	if meta.Parent != nil {
		node.Parent = relationNodeFromMeta(*meta.Parent)
	}
	return node
}

type relationNode struct {
	Title    string
	DOI      string
	Resource string
	Genres   []string
	Parent   *relationNode
}

func (n relationNode) hasGenre(uri string) bool {
	for _, genre := range n.Genres {
		if normalizeAuthorityURI(genre) == uri {
			return true
		}
	}
	return false
}

func (n relationNode) toJournalIssue() *crossrefv1.JournalIssue {
	return &crossrefv1.JournalIssue{
		DoiData: &crossrefv1.DoiData{
			Doi:      n.DOI,
			Resource: n.Resource,
		},
	}
}

func buildJournalArticle(record *hubv1.Record, opts *format.SerializeOptions) *crossrefv1.JournalArticle {
	article := &crossrefv1.JournalArticle{
		Titles:       buildTitles(record),
		Contributors: buildContributors(record.Contributors),
		Abstract:     record.Abstract,
		DoiData:      buildDoiData(record),
	}
	if opts != nil {
		article.CitationList = buildCitationList(opts.ReferenceDOIs)
	}

	article.PublicationDate = publicationDateFromRecord(record)

	return article
}

func publicationDateFromRecord(record *hubv1.Record) *crossrefv1.PublicationDate {
	for _, d := range record.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED || d.Type == hubv1.DateType_DATE_TYPE_PUBLISHED {
			return buildPublicationDate(d)
		}
	}
	return nil
}

func findJournalParentRelation(record *hubv1.Record) *hubv1.Relation {
	if !isJournalArticleLike(record) {
		return nil
	}

	for _, rel := range record.Relations {
		if rel.Type == hubv1.RelationType_RELATION_TYPE_MEMBER_OF ||
			rel.Type == hubv1.RelationType_RELATION_TYPE_PART_OF {
			return rel
		}
	}

	return nil
}

func isJournalArticleLike(record *hubv1.Record) bool {
	for _, genre := range record.Genres {
		switch normalizeAuthorityURI(genre.Uri) {
		case gettyArticleURI, gettyVolumeURI, gettyUnboundVolumeURI:
			return true
		}
	}
	return false
}

type relationMeta struct {
	Source   string        `json:"source,omitempty"`
	Title    string        `json:"title,omitempty"`
	DOI      string        `json:"doi,omitempty"`
	Resource string        `json:"resource,omitempty"`
	Genres   []string      `json:"genres,omitempty"`
	Parent   *relationMeta `json:"parent,omitempty"`
}

func relationMetadata(rel *hubv1.Relation) relationMeta {
	var meta relationMeta
	if rel == nil || rel.Description == "" {
		return meta
	}
	_ = json.Unmarshal([]byte(rel.Description), &meta)
	return meta
}

func normalizeAuthorityURI(uri string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(uri)), "/")
}

func buildCitationList(referenceDOIs []string) *crossrefv1.CitationList {
	if len(referenceDOIs) == 0 {
		return nil
	}
	citations := &crossrefv1.CitationList{}
	seen := make(map[string]bool)
	for _, raw := range referenceDOIs {
		doi := NormalizeReferenceDOI(raw)
		if doi == "" || seen[doi] {
			continue
		}
		seen[doi] = true
		citations.Citation = append(citations.Citation, &crossrefv1.Citation{
			Key: fmt.Sprintf("ref%d", len(citations.Citation)+1),
			Doi: doi,
		})
	}
	if len(citations.Citation) == 0 {
		return nil
	}
	return citations
}

func NormalizeReferenceDOI(raw string) string {
	doi := strings.TrimSpace(raw)
	doi = strings.TrimPrefix(doi, "https://doi.org/")
	doi = strings.TrimPrefix(doi, "http://doi.org/")
	doi = strings.TrimPrefix(doi, "doi.org/")
	doi = strings.TrimPrefix(doi, "https://dx.doi.org/")
	doi = strings.TrimPrefix(doi, "http://dx.doi.org/")
	doi = strings.TrimPrefix(doi, "dx.doi.org/")
	doi = strings.TrimPrefix(doi, "doi:")
	doi = doiWhitespaceRE.ReplaceAllString(doi, "")
	return strings.TrimSpace(doi)
}

func buildDissertation(record *hubv1.Record) *crossrefv1.Dissertation {
	diss := &crossrefv1.Dissertation{
		Titles:   buildTitles(record),
		Abstract: record.Abstract,
	}

	// Author (first contributor)
	if len(record.Contributors) > 0 {
		diss.PersonName = buildPersonName(record.Contributors[0], "author", "first")
	}

	// Approval date
	for _, d := range record.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED || d.Type == hubv1.DateType_DATE_TYPE_PUBLISHED {
			diss.ApprovalDate = buildPublicationDate(d)
			break
		}
	}

	// Institution from degree info
	if record.DegreeInfo != nil {
		diss.Institution = &crossrefv1.Institution{
			InstitutionName: record.DegreeInfo.Institution,
		}
		diss.Degree = record.DegreeInfo.DegreeName
	}

	// DOI
	diss.DoiData = buildDoiData(record)

	return diss
}

func buildPostedContent(record *hubv1.Record) *crossrefv1.PostedContent {
	pc := &crossrefv1.PostedContent{
		Titles:       buildTitles(record),
		Contributors: buildContributors(record.Contributors),
		Abstract:     record.Abstract,
		DoiData:      buildDoiData(record),
	}

	// Type
	if record.ResourceType != nil && record.ResourceType.Type == hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT {
		pc.Type = "preprint"
	} else {
		pc.Type = "other"
	}

	// Posted date
	for _, d := range record.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED || d.Type == hubv1.DateType_DATE_TYPE_PUBLISHED {
			pc.PostedDate = buildPublicationDate(d)
			break
		}
	}

	return pc
}

func buildDataset(record *hubv1.Record) *crossrefv1.Dataset {
	ds := &crossrefv1.Dataset{
		Titles:       buildTitles(record),
		Contributors: buildContributors(record.Contributors),
		DoiData:      buildDoiData(record),
	}

	// Publication date
	for _, d := range record.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED || d.Type == hubv1.DateType_DATE_TYPE_PUBLISHED {
			ds.PublicationDate = buildPublicationDate(d)
			break
		}
	}

	return ds
}

func buildBook(record *hubv1.Record) *crossrefv1.Book {
	edition := primaryCompatibilityValue(hub.GetEditions(record))
	book := &crossrefv1.Book{
		BookType: "monograph",
		BookMetadata: &crossrefv1.BookMetadata{
			Titles:        buildTitles(record),
			Contributors:  buildContributors(record.Contributors),
			DoiData:       buildDoiData(record),
			EditionNumber: edition,
		},
	}

	// Publication date
	for _, d := range record.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED || d.Type == hubv1.DateType_DATE_TYPE_PUBLISHED {
			book.BookMetadata.PublicationDate = buildPublicationDate(d)
			break
		}
	}

	// Publisher
	publisher := primaryCompatibilityValue(hub.GetPublishers(record))
	if publisher != "" {
		book.BookMetadata.Publisher = &crossrefv1.Publisher{
			PublisherName:  publisher,
			PublisherPlace: primaryCompatibilityValue(hub.GetPlacesPublished(record)),
		}
	}

	// ISBN
	for _, id := range record.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN {
			book.BookMetadata.IsbnPrint = id.Value
		}
	}

	return book
}

func primaryCompatibilityValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func buildTitles(record *hubv1.Record) *crossrefv1.Titles {
	titles := &crossrefv1.Titles{
		Title: record.Title,
	}
	if len(record.AltTitle) > 0 {
		titles.Subtitle = record.AltTitle[0]
	}
	return titles
}

func buildContributors(contributors []*hubv1.Contributor) *crossrefv1.Contributors {
	result := &crossrefv1.Contributors{}

	for i, c := range contributors {
		sequence := "additional"
		if i == 0 {
			sequence = "first"
		}

		role := "author"
		if strings.ToLower(c.Role) == "editor" || strings.ToLower(c.Role) == "edt" {
			role = "editor"
		}

		result.PersonName = append(result.PersonName, buildPersonName(c, role, sequence))
	}

	return result
}

func buildPersonName(c *hubv1.Contributor, role, sequence string) *crossrefv1.PersonName {
	pn := &crossrefv1.PersonName{
		ContributorRole: role,
		Sequence:        sequence,
	}

	if c.ParsedName != nil {
		pn.GivenName = c.ParsedName.Given
		pn.Surname = c.ParsedName.Family
		pn.Suffix = c.ParsedName.Suffix
	} else if c.Name != "" {
		// Try to parse name
		parts := strings.Fields(c.Name)
		if len(parts) >= 2 {
			pn.Surname = parts[len(parts)-1]
			pn.GivenName = strings.Join(parts[:len(parts)-1], " ")
		} else if len(parts) == 1 {
			pn.Surname = parts[0]
		}
	}

	// ORCID
	for _, id := range c.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID {
			pn.Orcid = id.Value
		}
	}

	// Affiliations
	for _, aff := range c.Affiliations {
		pn.Affiliation = append(pn.Affiliation, &crossrefv1.Affiliation{
			Name: aff.Name,
		})
	}

	return pn
}

func buildPublicationDate(d *hubv1.DateValue) *crossrefv1.PublicationDate {
	return &crossrefv1.PublicationDate{
		MediaType: "online",
		Year:      d.Year,
		Month:     d.Month,
		Day:       d.Day,
	}
}

func buildDoiData(record *hubv1.Record) *crossrefv1.DoiData {
	doiData := &crossrefv1.DoiData{}

	for _, id := range record.Identifiers {
		switch id.Type {
		case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
			doiData.Doi = id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
			if doiData.Resource == "" {
				doiData.Resource = id.Value
			}
		}
	}

	return doiData
}

// spokeToXML converts spoke proto structs to XML-marshalable types.
func spokeToXML(spoke *crossrefv1.Deposit) *XMLDeposit {
	deposit := &XMLDeposit{
		XMLNS:     "http://www.crossref.org/schema/5.3.1",
		XSI:       "http://www.w3.org/2001/XMLSchema-instance",
		SchemaLoc: "http://www.crossref.org/schema/5.3.1 http://www.crossref.org/schemas/crossref5.3.1.xsd",
		Version:   "5.3.1",
	}

	if spoke.Head != nil {
		deposit.Head = &XMLHead{
			DoiBatchID: spoke.Head.DoiBatchId,
			Timestamp:  spoke.Head.Timestamp,
		}
		if spoke.Head.Depositor != nil {
			deposit.Head.Depositor = &XMLDepositor{
				DepositorName: spoke.Head.Depositor.DepositorName,
				EmailAddress:  spoke.Head.Depositor.EmailAddress,
			}
		}
		deposit.Head.Registrant = spoke.Head.Registrant
	}

	deposit.Body = &XMLBody{}

	for _, journal := range spoke.Body.Journal {
		xmlJournal := journalToXML(journal)
		deposit.Body.Journal = append(deposit.Body.Journal, xmlJournal)
	}

	// Dissertations
	for _, diss := range spoke.Body.Dissertation {
		xmlDiss := dissertationToXML(diss)
		deposit.Body.Dissertation = append(deposit.Body.Dissertation, xmlDiss)
	}

	// Posted content
	for _, pc := range spoke.Body.PostedContent {
		xmlPC := postedContentToXML(pc)
		deposit.Body.PostedContent = append(deposit.Body.PostedContent, xmlPC)
	}

	// Datasets
	if len(spoke.Body.Dataset) > 0 {
		deposit.Body.Database = &XMLDatabase{}
		for _, ds := range spoke.Body.Dataset {
			xmlDS := datasetToXML(ds)
			deposit.Body.Database.Dataset = append(deposit.Body.Database.Dataset, xmlDS)
		}
	}

	// Books
	for _, book := range spoke.Body.Book {
		xmlBook := bookToXML(book)
		deposit.Body.Book = append(deposit.Body.Book, xmlBook)
	}

	return deposit
}

func journalToXML(journal *crossrefv1.Journal) *XMLJournal {
	xmlJournal := &XMLJournal{}

	if journal.JournalMetadata != nil {
		xmlJournal.JournalMetadata = &XMLJournalMetadata{
			Language:    "en",
			FullTitle:   journal.JournalMetadata.FullTitle,
			AbbrevTitle: journal.JournalMetadata.AbbrevTitle,
		}
		if journal.JournalMetadata.IssnPrint != "" {
			xmlJournal.JournalMetadata.ISSN = append(xmlJournal.JournalMetadata.ISSN, journal.JournalMetadata.IssnPrint)
		}
		if journal.JournalMetadata.IssnElectronic != "" {
			xmlJournal.JournalMetadata.ISSN = append(xmlJournal.JournalMetadata.ISSN, journal.JournalMetadata.IssnElectronic)
		}
		if journal.JournalMetadata.DoiData != nil && journal.JournalMetadata.DoiData.Doi != "" {
			xmlJournal.JournalMetadata.DoiData = doiDataToXML(journal.JournalMetadata.DoiData)
		}
	}

	for _, issue := range journal.JournalIssue {
		xmlIssue := &XMLJournalIssue{
			Volume: issue.Volume,
			Issue:  issue.Issue,
		}
		if issue.PublicationDate != nil {
			xmlIssue.PublicationDate = publicationDateToXML(issue.PublicationDate)
		}
		if issue.DoiData != nil && issue.DoiData.Doi != "" {
			xmlIssue.DoiData = doiDataToXML(issue.DoiData)
		}
		xmlJournal.JournalIssue = append(xmlJournal.JournalIssue, xmlIssue)
	}

	for _, article := range journal.JournalArticle {
		xmlJournal.JournalArticle = append(xmlJournal.JournalArticle, journalArticleToXML(article))
	}

	return xmlJournal
}

func journalArticleToXML(article *crossrefv1.JournalArticle) *XMLJournalArticle {
	xmlArticle := &XMLJournalArticle{
		PublicationType: "full_text",
	}

	if article.Titles != nil {
		xmlArticle.Titles = titlesToXML(article.Titles)
	}
	if article.Contributors != nil {
		xmlArticle.Contributors = contributorsToXML(article.Contributors)
	}
	if article.PublicationDate != nil {
		xmlArticle.PublicationDate = publicationDateToXML(article.PublicationDate)
	}
	if article.Abstract != "" {
		xmlArticle.Abstract = &XMLAbstract{Content: article.Abstract}
	}
	if article.DoiData != nil && article.DoiData.Doi != "" {
		xmlArticle.DoiData = doiDataToXML(article.DoiData)
	}
	if article.CitationList != nil && len(article.CitationList.Citation) > 0 {
		xmlArticle.CitationList = citationListToXML(article.CitationList)
	}

	return xmlArticle
}

func citationListToXML(citations *crossrefv1.CitationList) *XMLCitationList {
	xmlList := &XMLCitationList{}
	for _, citation := range citations.Citation {
		xmlList.Citation = append(xmlList.Citation, &XMLCitation{
			Key: citation.Key,
			DOI: citation.Doi,
		})
	}
	return xmlList
}

func dissertationToXML(diss *crossrefv1.Dissertation) *XMLDissertation {
	xmlDiss := &XMLDissertation{
		Degree: diss.Degree,
	}

	if diss.Titles != nil {
		xmlDiss.Titles = titlesToXML(diss.Titles)
	}

	if diss.PersonName != nil {
		xmlDiss.PersonName = personNameToXML(diss.PersonName)
	}

	if diss.ApprovalDate != nil {
		xmlDiss.ApprovalDate = publicationDateToXML(diss.ApprovalDate)
	}

	if diss.Institution != nil {
		xmlDiss.Institution = &XMLInstitution{
			InstitutionName: diss.Institution.InstitutionName,
		}
	}

	if diss.Abstract != "" {
		xmlDiss.Abstract = &XMLAbstract{Content: diss.Abstract}
	}

	if diss.DoiData != nil && diss.DoiData.Doi != "" {
		xmlDiss.DoiData = doiDataToXML(diss.DoiData)
	}

	return xmlDiss
}

func postedContentToXML(pc *crossrefv1.PostedContent) *XMLPostedContent {
	xmlPC := &XMLPostedContent{
		Type: pc.Type,
	}

	if pc.Titles != nil {
		xmlPC.Titles = titlesToXML(pc.Titles)
	}

	if pc.Contributors != nil {
		xmlPC.Contributors = contributorsToXML(pc.Contributors)
	}

	if pc.PostedDate != nil {
		xmlPC.PostedDate = publicationDateToXML(pc.PostedDate)
	}

	if pc.Abstract != "" {
		xmlPC.Abstract = &XMLAbstract{Content: pc.Abstract}
	}

	if pc.DoiData != nil && pc.DoiData.Doi != "" {
		xmlPC.DoiData = doiDataToXML(pc.DoiData)
	}

	return xmlPC
}

func datasetToXML(ds *crossrefv1.Dataset) *XMLDataset {
	xmlDS := &XMLDataset{
		DatasetType: "record",
	}

	if ds.Titles != nil {
		xmlDS.Titles = titlesToXML(ds.Titles)
	}

	if ds.Contributors != nil {
		xmlDS.Contributors = contributorsToXML(ds.Contributors)
	}

	if ds.PublicationDate != nil {
		xmlDS.DatabaseDate = publicationDateToXML(ds.PublicationDate)
	}

	if ds.DoiData != nil && ds.DoiData.Doi != "" {
		xmlDS.DoiData = doiDataToXML(ds.DoiData)
	}

	return xmlDS
}

func bookToXML(book *crossrefv1.Book) *XMLBook {
	xmlBook := &XMLBook{
		BookType: book.BookType,
	}

	if book.BookMetadata != nil {
		xmlBook.BookMetadata = &XMLBookMetadata{
			EditionNumber:  book.BookMetadata.EditionNumber,
			IsbnPrint:      book.BookMetadata.IsbnPrint,
			IsbnElectronic: book.BookMetadata.IsbnElectronic,
		}

		if book.BookMetadata.Titles != nil {
			xmlBook.BookMetadata.Titles = titlesToXML(book.BookMetadata.Titles)
		}

		if book.BookMetadata.Contributors != nil {
			xmlBook.BookMetadata.Contributors = contributorsToXML(book.BookMetadata.Contributors)
		}

		if book.BookMetadata.PublicationDate != nil {
			xmlBook.BookMetadata.PublicationDate = publicationDateToXML(book.BookMetadata.PublicationDate)
		}

		if book.BookMetadata.Publisher != nil {
			xmlBook.BookMetadata.Publisher = &XMLPublisher{
				PublisherName:  book.BookMetadata.Publisher.PublisherName,
				PublisherPlace: book.BookMetadata.Publisher.PublisherPlace,
			}
		}

		if book.BookMetadata.DoiData != nil && book.BookMetadata.DoiData.Doi != "" {
			xmlBook.BookMetadata.DoiData = doiDataToXML(book.BookMetadata.DoiData)
		}
	}

	return xmlBook
}

func titlesToXML(titles *crossrefv1.Titles) *XMLTitles {
	return &XMLTitles{
		Title:    titles.Title,
		Subtitle: titles.Subtitle,
	}
}

func contributorsToXML(contributors *crossrefv1.Contributors) *XMLContributors {
	result := &XMLContributors{}

	for _, pn := range contributors.PersonName {
		result.PersonName = append(result.PersonName, personNameToXML(pn))
	}

	return result
}

func personNameToXML(pn *crossrefv1.PersonName) *XMLPersonName {
	return &XMLPersonName{
		ContributorRole: pn.ContributorRole,
		Sequence:        pn.Sequence,
		GivenName:       pn.GivenName,
		Surname:         pn.Surname,
		Suffix:          pn.Suffix,
		ORCID:           pn.Orcid,
	}
}

func publicationDateToXML(pd *crossrefv1.PublicationDate) *XMLPublicationDate {
	return &XMLPublicationDate{
		MediaType: pd.MediaType,
		Year:      pd.Year,
		Month:     pd.Month,
		Day:       pd.Day,
	}
}

func doiDataToXML(dd *crossrefv1.DoiData) *XMLDoiData {
	return &XMLDoiData{
		DOI:      dd.Doi,
		Resource: dd.Resource,
	}
}

// XML types for CrossRef deposit serialization.

type XMLDeposit struct {
	XMLName   xml.Name `xml:"doi_batch"`
	XMLNS     string   `xml:"xmlns,attr"`
	XSI       string   `xml:"xmlns:xsi,attr"`
	SchemaLoc string   `xml:"xsi:schemaLocation,attr"`
	Version   string   `xml:"version,attr"`
	Head      *XMLHead `xml:"head"`
	Body      *XMLBody `xml:"body"`
}

type XMLHead struct {
	DoiBatchID string        `xml:"doi_batch_id"`
	Timestamp  string        `xml:"timestamp"`
	Depositor  *XMLDepositor `xml:"depositor"`
	Registrant string        `xml:"registrant"`
}

type XMLDepositor struct {
	DepositorName string `xml:"depositor_name"`
	EmailAddress  string `xml:"email_address"`
}

type XMLBody struct {
	Journal       []*XMLJournal       `xml:"journal,omitempty"`
	Dissertation  []*XMLDissertation  `xml:"dissertation,omitempty"`
	PostedContent []*XMLPostedContent `xml:"posted_content,omitempty"`
	Database      *XMLDatabase        `xml:"database,omitempty"`
	Book          []*XMLBook          `xml:"book,omitempty"`
}

type XMLDatabase struct {
	Dataset []*XMLDataset `xml:"dataset,omitempty"`
}

type XMLJournal struct {
	JournalMetadata *XMLJournalMetadata  `xml:"journal_metadata,omitempty"`
	JournalIssue    []*XMLJournalIssue   `xml:"journal_issue,omitempty"`
	JournalArticle  []*XMLJournalArticle `xml:"journal_article,omitempty"`
}

type XMLJournalMetadata struct {
	Language    string      `xml:"language,attr,omitempty"`
	FullTitle   string      `xml:"full_title,omitempty"`
	AbbrevTitle string      `xml:"abbrev_title,omitempty"`
	ISSN        []string    `xml:"issn,omitempty"`
	DoiData     *XMLDoiData `xml:"doi_data,omitempty"`
}

type XMLJournalIssue struct {
	PublicationDate *XMLPublicationDate `xml:"publication_date,omitempty"`
	Volume          string              `xml:"journal_volume>volume,omitempty"`
	Issue           string              `xml:"issue,omitempty"`
	DoiData         *XMLDoiData         `xml:"doi_data,omitempty"`
}

type XMLJournalArticle struct {
	PublicationType string              `xml:"publication_type,attr,omitempty"`
	Titles          *XMLTitles          `xml:"titles,omitempty"`
	Contributors    *XMLContributors    `xml:"contributors,omitempty"`
	PublicationDate *XMLPublicationDate `xml:"publication_date,omitempty"`
	Abstract        *XMLAbstract        `xml:"abstract,omitempty"`
	DoiData         *XMLDoiData         `xml:"doi_data,omitempty"`
	CitationList    *XMLCitationList    `xml:"citation_list,omitempty"`
}

type XMLCitationList struct {
	Citation []*XMLCitation `xml:"citation,omitempty"`
}

type XMLCitation struct {
	Key string `xml:"key,attr,omitempty"`
	DOI string `xml:"doi,omitempty"`
}

type XMLDissertation struct {
	Titles       *XMLTitles          `xml:"titles,omitempty"`
	PersonName   *XMLPersonName      `xml:"person_name,omitempty"`
	ApprovalDate *XMLPublicationDate `xml:"approval_date,omitempty"`
	Institution  *XMLInstitution     `xml:"institution,omitempty"`
	Degree       string              `xml:"degree,omitempty"`
	Abstract     *XMLAbstract        `xml:"abstract,omitempty"`
	DoiData      *XMLDoiData         `xml:"doi_data,omitempty"`
}

type XMLPostedContent struct {
	Type         string              `xml:"type,attr"`
	Titles       *XMLTitles          `xml:"titles,omitempty"`
	Contributors *XMLContributors    `xml:"contributors,omitempty"`
	PostedDate   *XMLPublicationDate `xml:"posted_date,omitempty"`
	Abstract     *XMLAbstract        `xml:"abstract,omitempty"`
	DoiData      *XMLDoiData         `xml:"doi_data,omitempty"`
}

type XMLDataset struct {
	DatasetType  string              `xml:"dataset_type,attr"`
	Titles       *XMLTitles          `xml:"titles,omitempty"`
	Contributors *XMLContributors    `xml:"contributors,omitempty"`
	DatabaseDate *XMLPublicationDate `xml:"database_date>publication_date,omitempty"`
	DoiData      *XMLDoiData         `xml:"doi_data,omitempty"`
}

type XMLBook struct {
	BookType     string           `xml:"book_type,attr"`
	BookMetadata *XMLBookMetadata `xml:"book_metadata,omitempty"`
}

type XMLBookMetadata struct {
	Titles          *XMLTitles          `xml:"titles,omitempty"`
	Contributors    *XMLContributors    `xml:"contributors,omitempty"`
	PublicationDate *XMLPublicationDate `xml:"publication_date,omitempty"`
	IsbnPrint       string              `xml:"isbn,omitempty"`
	IsbnElectronic  string              `xml:"noisbn,omitempty"`
	Publisher       *XMLPublisher       `xml:"publisher,omitempty"`
	EditionNumber   string              `xml:"edition_number,omitempty"`
	DoiData         *XMLDoiData         `xml:"doi_data,omitempty"`
}

type XMLTitles struct {
	Title    string `xml:"title,omitempty"`
	Subtitle string `xml:"subtitle,omitempty"`
}

type XMLContributors struct {
	PersonName []*XMLPersonName `xml:"person_name,omitempty"`
}

type XMLPersonName struct {
	ContributorRole string `xml:"contributor_role,attr,omitempty"`
	Sequence        string `xml:"sequence,attr,omitempty"`
	GivenName       string `xml:"given_name,omitempty"`
	Surname         string `xml:"surname,omitempty"`
	Suffix          string `xml:"suffix,omitempty"`
	ORCID           string `xml:"ORCID,omitempty"`
}

type XMLPublicationDate struct {
	MediaType string `xml:"media_type,attr,omitempty"`
	Month     int32  `xml:"month,omitempty"`
	Day       int32  `xml:"day,omitempty"`
	Year      int32  `xml:"year,omitempty"`
}

type XMLInstitution struct {
	InstitutionName string `xml:"institution_name,omitempty"`
}

type XMLPublisher struct {
	PublisherName  string `xml:"publisher_name,omitempty"`
	PublisherPlace string `xml:"publisher_place,omitempty"`
}

type XMLDoiData struct {
	DOI      string `xml:"doi,omitempty"`
	Resource string `xml:"resource,omitempty"`
}

type XMLAbstract struct {
	Content string `xml:",chardata"`
}
