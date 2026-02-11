package crossref

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	crossrefv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/crossref/v5_3_1"
)

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
	book := &crossrefv1.Book{
		BookType: "monograph",
		BookMetadata: &crossrefv1.BookMetadata{
			Titles:        buildTitles(record),
			Contributors:  buildContributors(record.Contributors),
			DoiData:       buildDoiData(record),
			EditionNumber: record.Edition,
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
	if record.Publisher != "" {
		book.BookMetadata.Publisher = &crossrefv1.Publisher{
			PublisherName:  record.Publisher,
			PublisherPlace: record.PlacePublished,
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
	for _, ds := range spoke.Body.Dataset {
		xmlDS := datasetToXML(ds)
		deposit.Body.Dataset = append(deposit.Body.Dataset, xmlDS)
	}

	// Books
	for _, book := range spoke.Body.Book {
		xmlBook := bookToXML(book)
		deposit.Body.Book = append(deposit.Body.Book, xmlBook)
	}

	return deposit
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
	Dissertation  []*XMLDissertation  `xml:"dissertation,omitempty"`
	PostedContent []*XMLPostedContent `xml:"posted_content,omitempty"`
	Dataset       []*XMLDataset       `xml:"database>dataset,omitempty"`
	Book          []*XMLBook          `xml:"book,omitempty"`
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
	Year      int32  `xml:"year,omitempty"`
	Month     int32  `xml:"month,omitempty"`
	Day       int32  `xml:"day,omitempty"`
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
