package schemaorg

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// Serialize writes hub records as schema.org JSON-LD.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	if opts == nil {
		opts = format.NewSerializeOptions()
	}

	jsonldDocs := make([]any, 0, len(records))
	for _, record := range records {
		doc, err := recordToSchemaOrg(record)
		if err != nil {
			return fmt.Errorf("converting record: %w", err)
		}
		jsonldDocs = append(jsonldDocs, doc)
	}

	encoder := json.NewEncoder(w)
	if opts.Pretty {
		encoder.SetIndent("", "  ")
	}

	// Single record outputs object; multiple outputs array
	if len(jsonldDocs) == 1 {
		return encoder.Encode(jsonldDocs[0])
	}
	return encoder.Encode(jsonldDocs)
}

// recordToSchemaOrg converts a hub record to the appropriate schema.org type.
func recordToSchemaOrg(record *hubv1.Record) (any, error) {
	schemaType := determineSchemaType(record)

	switch schemaType {
	case TypeScholarlyArticle:
		return recordToScholarlyArticle(record), nil
	case TypeBook:
		return recordToBook(record), nil
	case TypeDataset:
		return recordToDataset(record), nil
	case TypeCollection:
		return recordToCollection(record), nil
	case TypeThesis:
		return recordToThesis(record), nil
	case TypeDigitalDocument:
		return recordToDigitalDocument(record), nil
	case TypeManuscript:
		return recordToManuscript(record), nil
	case TypeAudioObject:
		return recordToAudioObject(record), nil
	case TypeImageObject:
		return recordToImageObject(record), nil
	case TypeVideoObject:
		return recordToVideoObject(record), nil
	case TypePublicationIssue:
		return recordToPublicationIssue(record), nil
	case TypePublicationVolume:
		return recordToPublicationVolume(record), nil
	default:
		return recordToCreativeWork(record, schemaType), nil
	}
}

// determineSchemaType maps hub ResourceType to schema.org @type.
func determineSchemaType(record *hubv1.Record) SchemaType {
	if record.ResourceType == nil {
		return TypeCreativeWork
	}

	switch record.ResourceType.Type {
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_WORKING_PAPER:
		return TypeScholarlyArticle

	case hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER:
		return TypeBook

	case hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET:
		return TypeDataset

	case hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION:
		return TypeCollection

	case hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION:
		return TypeThesis

	case hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_TECHNICAL_REPORT:
		return TypeDigitalDocument

	case hubv1.ResourceTypeValue_RESOURCE_TYPE_MANUSCRIPT:
		return TypeManuscript

	case hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO:
		return TypeAudioObject

	case hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE:
		return TypeImageObject

	case hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO:
		return TypeVideoObject

	case hubv1.ResourceTypeValue_RESOURCE_TYPE_PERIODICAL,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_JOURNAL:
		return TypePublicationVolume

	default:
		return TypeCreativeWork
	}
}

// buildCreativeWorkBase creates the common CreativeWork fields.
func buildCreativeWorkBase(record *hubv1.Record, schemaType SchemaType) CreativeWork {
	cw := CreativeWork{
		Thing: Thing{
			Context: "https://schema.org",
			Type:    schemaType,
		},
	}

	// Title
	if record.Title != "" {
		cw.Name = record.Title
		cw.Headline = record.Title
	}
	if len(record.AltTitle) > 0 {
		cw.AlternativeTitle = record.AltTitle[0]
	}

	// Descriptions
	if record.Abstract != "" {
		cw.Abstract = record.Abstract
	}
	if record.Description != "" {
		cw.Description = record.Description
	}

	// Contributors
	if len(record.Contributors) > 0 {
		authors, editors, contributors := categorizeContributors(record.Contributors)
		if len(authors) > 0 {
			cw.Author = authors
		}
		if len(editors) > 0 {
			cw.Editor = editors
		}
		if len(contributors) > 0 {
			cw.Contributor = contributors
		}
	}

	// Dates
	for _, d := range record.Dates {
		dateStr := formatDate(d)
		if dateStr == "" {
			continue
		}
		switch d.Type {
		case hubv1.DateType_DATE_TYPE_PUBLISHED, hubv1.DateType_DATE_TYPE_ISSUED:
			cw.DatePublished = dateStr
		case hubv1.DateType_DATE_TYPE_CREATED:
			cw.DateCreated = dateStr
		case hubv1.DateType_DATE_TYPE_MODIFIED:
			cw.DateModified = dateStr
		case hubv1.DateType_DATE_TYPE_COPYRIGHT:
			cw.CopyrightYear = d.Year
		}
	}

	// Language
	if record.Language != "" {
		cw.InLanguage = record.Language
	}

	// Publisher
	if record.Publisher != "" {
		cw.Publisher = &Organization{
			Thing: Thing{
				Type: TypeOrganization,
				Name: record.Publisher,
			},
		}
	}

	// Genre - output rich DefinedTerm when we have both label and URI
	if len(record.Genres) > 0 {
		genres := make([]any, 0, len(record.Genres))
		for _, g := range record.Genres {
			genre := subjectToDefinedTerm(g)
			if genre != nil {
				genres = append(genres, genre)
			}
		}
		if len(genres) == 1 {
			cw.Genre = genres[0]
		} else if len(genres) > 1 {
			cw.Genre = genres
		}
	}

	// Subjects/Keywords
	keywords := extractKeywords(record.Subjects)
	if len(keywords) > 0 {
		cw.Keywords = keywords
	}

	// Rights
	if len(record.Rights) > 0 {
		for _, r := range record.Rights {
			if r.Uri != "" {
				cw.License = r.Uri
				break
			} else if r.Statement != "" {
				cw.License = r.Statement
				break
			}
		}
	}

	// Identifiers
	ids := buildIdentifiers(record.Identifiers)
	if len(ids) > 0 {
		cw.Identifier = ids
	}

	// URL (from identifiers)
	for _, id := range record.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_URL ||
			id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI {
			if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_DOI {
				cw.URL = "https://doi.org/" + id.Value
			} else {
				cw.URL = id.Value
			}
			break
		}
	}

	// Relations
	for _, rel := range record.Relations {
		switch rel.Type {
		case hubv1.RelationType_RELATION_TYPE_PART_OF,
			hubv1.RelationType_RELATION_TYPE_MEMBER_OF:
			cw.IsPartOf = relationToCreativeWork(rel)
		}
	}

	return cw
}

// categorizeContributors splits contributors by role.
func categorizeContributors(contribs []*hubv1.Contributor) (authors, editors, others []any) {
	for _, c := range contribs {
		person := contributorToPersonOrOrg(c)
		role := strings.ToLower(c.Role)

		switch {
		case role == "" || role == "author" || role == "creator" ||
			role == "aut" || role == "cre" || strings.Contains(role, "author"):
			authors = append(authors, person)
		case role == "editor" || role == "edt" || strings.Contains(role, "editor"):
			editors = append(editors, person)
		default:
			others = append(others, person)
		}
	}
	return
}

// contributorToPersonOrOrg converts a hub contributor to Person or Organization.
func contributorToPersonOrOrg(c *hubv1.Contributor) any {
	if c.Type == hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION {
		org := &Organization{
			Thing: Thing{
				Type: TypeOrganization,
				Name: c.Name,
			},
		}
		// Add ISNI/other identifiers
		for _, id := range c.Identifiers {
			if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI {
				org.SameAs = "https://isni.org/isni/" + id.Value
			}
		}
		return org
	}

	person := &Person{
		Thing: Thing{
			Type: TypePerson,
			Name: c.Name,
		},
	}

	// Use parsed name if available
	if c.ParsedName != nil {
		if c.ParsedName.Given != "" {
			person.GivenName = c.ParsedName.Given
		}
		if c.ParsedName.Family != "" {
			person.FamilyName = c.ParsedName.Family
		}
		if c.ParsedName.Middle != "" {
			person.AdditionalName = c.ParsedName.Middle
		}
		if c.ParsedName.Suffix != "" {
			person.HonorificSuffix = c.ParsedName.Suffix
		}
		if c.ParsedName.Prefix != "" {
			person.HonorificPrefix = c.ParsedName.Prefix
		}
	}

	// Affiliation
	if c.Affiliation != "" {
		person.Affiliation = &Organization{
			Thing: Thing{
				Type: TypeOrganization,
				Name: c.Affiliation,
			},
		}
	}

	// ORCID and other identifiers
	for _, id := range c.Identifiers {
		switch id.Type {
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID:
			person.SameAs = "https://orcid.org/" + id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI:
			if person.SameAs == nil {
				person.SameAs = "https://isni.org/isni/" + id.Value
			}
		}
	}

	return person
}

// formatDate formats a hub DateValue to ISO 8601 string.
func formatDate(d *hubv1.DateValue) string {
	if d.Year == 0 {
		return d.Raw
	}

	// Build ISO 8601 date based on precision
	if d.Month == 0 {
		return fmt.Sprintf("%04d", d.Year)
	}
	if d.Day == 0 {
		return fmt.Sprintf("%04d-%02d", d.Year, d.Month)
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// extractKeywords extracts keywords from subjects.
func extractKeywords(subjects []*hubv1.Subject) []string {
	var keywords []string
	for _, s := range subjects {
		if s.Value != "" {
			keywords = append(keywords, s.Value)
		}
	}
	return keywords
}

// subjectToDefinedTerm converts a hub Subject to a schema.org DefinedTerm.
// Returns a rich DefinedTerm struct when URI is available, otherwise just the label string.
func subjectToDefinedTerm(s *hubv1.Subject) any {
	if s.Value == "" && s.Uri == "" {
		return nil
	}

	// If we have both a label and URI, create a rich DefinedTerm
	if s.Value != "" && s.Uri != "" {
		term := &DefinedTerm{
			Type: "DefinedTerm",
			Name: s.Value,
			URL:  s.Uri,
		}

		// Add vocabulary info if available
		if termSet := vocabularyToTermSet(s.Vocabulary); termSet != nil {
			term.InDefinedTermSet = termSet
		}

		return term
	}

	// If we only have URI, return it as-is
	if s.Uri != "" {
		return s.Uri
	}

	// If we only have label, return it as-is
	return s.Value
}

// vocabularyToTermSet converts a SubjectVocabulary to a DefinedTermSet.
func vocabularyToTermSet(vocab hubv1.SubjectVocabulary) *DefinedTermSet {
	var source string
	switch vocab {
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT:
		source = "aat"
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH:
		source = "lcsh"
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCNAF:
		source = "lcnaf"
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GETTY_TGN:
		source = "tgn"
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH:
		source = "mesh"
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_FAST:
		source = "fast"
	default:
		return nil
	}

	if info, ok := KnownVocabularies[source]; ok {
		return &DefinedTermSet{
			Type: "DefinedTermSet",
			Name: info.Name,
			URL:  info.URL,
		}
	}
	return nil
}

// relationToCreativeWork converts a hub Relation to a schema.org CreativeWork reference.
// Returns a rich structure with name and URL when available.
func relationToCreativeWork(rel *hubv1.Relation) any {
	if rel == nil {
		return nil
	}

	// If we only have a URI and no title, return just the URI
	if rel.TargetTitle == "" && rel.TargetUri != "" {
		return rel.TargetUri
	}

	// If we have no useful data, return nil
	if rel.TargetTitle == "" && rel.TargetUri == "" {
		return nil
	}

	// Determine @type from target_type_uri (e.g., "https://schema.org/Collection" → "Collection")
	schemaType := "CreativeWork"
	if rel.TargetTypeUri != "" {
		schemaType = extractSchemaOrgType(rel.TargetTypeUri)
	}

	// Build a rich CreativeWork reference
	result := map[string]any{
		"@type": schemaType,
	}

	if rel.TargetTitle != "" {
		result["name"] = rel.TargetTitle
	}

	if rel.TargetUri != "" {
		result["url"] = rel.TargetUri
		// Use URL path as identifier (e.g., "/node/202" from "https://example.com/node/202")
		if path := extractURLPath(rel.TargetUri); path != "" {
			result["identifier"] = path
		}
	}

	return result
}

// extractURLPath extracts the path from a URL.
func extractURLPath(uri string) string {
	// Find the path after the host
	// Handle both http:// and https://
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(uri, prefix) {
			rest := strings.TrimPrefix(uri, prefix)
			// Find first slash after host
			if idx := strings.Index(rest, "/"); idx >= 0 {
				return rest[idx:]
			}
		}
	}
	return ""
}

// extractSchemaOrgType extracts the type name from a type URI and maps it to schema.org.
// Handles schema.org URIs directly, and maps Dublin Core types to schema.org equivalents.
func extractSchemaOrgType(uri string) string {
	// Handle schema.org URIs directly
	for _, prefix := range []string{"https://schema.org/", "http://schema.org/"} {
		if strings.HasPrefix(uri, prefix) {
			return strings.TrimPrefix(uri, prefix)
		}
	}

	// Handle Dublin Core type URIs (http://purl.org/dc/dcmitype/*)
	for _, prefix := range []string{"http://purl.org/dc/dcmitype/", "https://purl.org/dc/dcmitype/"} {
		if strings.HasPrefix(uri, prefix) {
			dcType := strings.TrimPrefix(uri, prefix)
			return mapDCTypeToSchemaOrg(dcType)
		}
	}

	// If not a recognized URI, return CreativeWork as fallback
	return "CreativeWork"
}

// mapDCTypeToSchemaOrg maps Dublin Core types to schema.org equivalents.
func mapDCTypeToSchemaOrg(dcType string) string {
	switch dcType {
	case "Collection":
		return "Collection"
	case "Dataset":
		return "Dataset"
	case "Event":
		return "Event"
	case "Image", "StillImage":
		return "ImageObject"
	case "MovingImage":
		return "VideoObject"
	case "Sound":
		return "AudioObject"
	case "Text":
		return "TextDigitalDocument"
	case "Software":
		return "SoftwareSourceCode"
	case "Service":
		return "Service"
	case "InteractiveResource":
		return "WebApplication"
	case "PhysicalObject":
		return "Product"
	default:
		return "CreativeWork"
	}
}

// buildIdentifiers converts hub identifiers to schema.org PropertyValue array.
func buildIdentifiers(ids []*hubv1.Identifier) []PropertyValue {
	var result []PropertyValue
	for _, id := range ids {
		pv := PropertyValue{
			Type:  "PropertyValue",
			Value: id.Value,
		}
		switch id.Type {
		case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
			pv.PropertyID = "doi"
			pv.Name = "DOI"
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
			pv.PropertyID = "isbn"
			pv.Name = "ISBN"
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
			pv.PropertyID = "issn"
			pv.Name = "ISSN"
		case hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE:
			pv.PropertyID = "handle"
			pv.Name = "Handle"
		case hubv1.IdentifierType_IDENTIFIER_TYPE_PMID:
			pv.PropertyID = "pmid"
			pv.Name = "PubMed ID"
		case hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID:
			pv.PropertyID = "pmcid"
			pv.Name = "PubMed Central ID"
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV:
			pv.PropertyID = "arxiv"
			pv.Name = "arXiv ID"
		case hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL:
			pv.PropertyID = "local"
			pv.Name = "Local ID"
		default:
			pv.PropertyID = "identifier"
		}
		result = append(result, pv)
	}
	return result
}

// Type-specific converters

func recordToScholarlyArticle(record *hubv1.Record) *ScholarlyArticle {
	base := buildCreativeWorkBase(record, TypeScholarlyArticle)
	article := &ScholarlyArticle{
		CreativeWork: base,
	}

	// Physical description for pagination
	if record.PhysicalDesc != "" {
		article.Pagination = record.PhysicalDesc
	}

	return article
}

func recordToBook(record *hubv1.Record) *Book {
	base := buildCreativeWorkBase(record, TypeBook)
	book := &Book{
		CreativeWork: base,
	}

	// ISBN from identifiers
	for _, id := range record.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN {
			book.ISBN = id.Value
			break
		}
	}

	return book
}

func recordToDataset(record *hubv1.Record) *Dataset {
	base := buildCreativeWorkBase(record, TypeDataset)
	return &Dataset{
		CreativeWork: base,
	}
}

func recordToCollection(record *hubv1.Record) *Collection {
	base := buildCreativeWorkBase(record, TypeCollection)
	return &Collection{
		CreativeWork: base,
	}
}

func recordToThesis(record *hubv1.Record) *CreativeWork {
	base := buildCreativeWorkBase(record, TypeThesis)

	// Preserve thesis/dissertation-specific enrichment that was previously
	// done in DigitalDocument output.
	if record.DegreeInfo != nil {
		if record.DegreeInfo.Institution != "" {
			base.Publisher = &Organization{
				Thing: Thing{
					Type: TypeOrganization,
					Name: record.DegreeInfo.Institution,
				},
			}
		}
		if record.DegreeInfo.DegreeName != "" {
			if base.Description != "" {
				base.Description += ". "
			}
			base.Description += record.DegreeInfo.DegreeName
			if record.DegreeInfo.Department != "" {
				base.Description += ", " + record.DegreeInfo.Department
			}
		}
	}

	return &base
}

func recordToDigitalDocument(record *hubv1.Record) *DigitalDocument {
	base := buildCreativeWorkBase(record, TypeDigitalDocument)
	doc := &DigitalDocument{
		CreativeWork: base,
	}

	// Thesis/dissertation handling
	if record.DegreeInfo != nil {
		if record.DegreeInfo.Institution != "" {
			doc.Publisher = &Organization{
				Thing: Thing{
					Type: TypeOrganization,
					Name: record.DegreeInfo.Institution,
				},
			}
		}
		// Add degree info to description
		if record.DegreeInfo.DegreeName != "" {
			if doc.Description != "" {
				doc.Description += ". "
			}
			doc.Description += record.DegreeInfo.DegreeName
			if record.DegreeInfo.Department != "" {
				doc.Description += ", " + record.DegreeInfo.Department
			}
		}
	}

	return doc
}

func recordToManuscript(record *hubv1.Record) *Manuscript {
	base := buildCreativeWorkBase(record, TypeManuscript)
	return &Manuscript{
		CreativeWork: base,
	}
}

func recordToAudioObject(record *hubv1.Record) *AudioObject {
	base := buildCreativeWorkBase(record, TypeAudioObject)
	audio := &AudioObject{
		MediaObject: MediaObject{
			CreativeWork: base,
		},
	}

	// Look for URL in identifiers
	for _, id := range record.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_URL {
			audio.ContentURL = id.Value
			break
		}
	}

	return audio
}

func recordToImageObject(record *hubv1.Record) *ImageObject {
	base := buildCreativeWorkBase(record, TypeImageObject)
	image := &ImageObject{
		MediaObject: MediaObject{
			CreativeWork: base,
		},
	}

	// Look for URL in identifiers
	for _, id := range record.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_URL {
			image.ContentURL = id.Value
			break
		}
	}

	return image
}

func recordToVideoObject(record *hubv1.Record) *VideoObject {
	base := buildCreativeWorkBase(record, TypeVideoObject)
	video := &VideoObject{
		MediaObject: MediaObject{
			CreativeWork: base,
		},
	}

	// Look for URL in identifiers
	for _, id := range record.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_URL {
			video.ContentURL = id.Value
			break
		}
	}

	return video
}

func recordToPublicationIssue(record *hubv1.Record) *PublicationIssue {
	base := buildCreativeWorkBase(record, TypePublicationIssue)
	return &PublicationIssue{
		CreativeWork: base,
	}
}

func recordToPublicationVolume(record *hubv1.Record) *PublicationVolume {
	base := buildCreativeWorkBase(record, TypePublicationVolume)
	return &PublicationVolume{
		CreativeWork: base,
	}
}

func recordToCreativeWork(record *hubv1.Record, schemaType SchemaType) *CreativeWork {
	base := buildCreativeWorkBase(record, schemaType)
	return &base
}
