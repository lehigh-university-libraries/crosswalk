package schemaorg

// SchemaType represents supported schema.org @type values.
type SchemaType string

const (
	TypeScholarlyArticle  SchemaType = "ScholarlyArticle"
	TypeBook              SchemaType = "Book"
	TypeDataset           SchemaType = "Dataset"
	TypeCollection        SchemaType = "Collection"
	TypeThesis            SchemaType = "Thesis"
	TypeReport            SchemaType = "Report"
	TypePeriodical        SchemaType = "Periodical"
	TypeMap               SchemaType = "Map"
	TypePoster            SchemaType = "Poster"
	TypePresentationDoc   SchemaType = "PresentationDigitalDocument"
	TypeDigitalDocument   SchemaType = "DigitalDocument"
	TypeManuscript        SchemaType = "Manuscript"
	TypeAudioObject       SchemaType = "AudioObject"
	TypeImageObject       SchemaType = "ImageObject"
	TypeVideoObject       SchemaType = "VideoObject"
	TypePublicationIssue  SchemaType = "PublicationIssue"
	TypePublicationVolume SchemaType = "PublicationVolume"
	TypePerson            SchemaType = "Person"
	TypeOrganization      SchemaType = "Organization"
	TypeCreativeWork      SchemaType = "CreativeWork" // Fallback type
)

// Thing is the base schema.org type.
type Thing struct {
	Context     any    `json:"@context,omitempty"`
	Type        any    `json:"@type"`
	ID          string `json:"@id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Identifier  any    `json:"identifier,omitempty"` // string, []string, or []PropertyValue
	SameAs      any    `json:"sameAs,omitempty"`     // string or []string
}

// CreativeWork extends Thing with properties common to creative works.
type CreativeWork struct {
	Thing

	// Core properties
	Headline         string `json:"headline,omitempty"`
	AlternativeTitle string `json:"alternativeHeadline,omitempty"`
	Abstract         string `json:"abstract,omitempty"`

	// Authorship
	Author      any `json:"author,omitempty"`      // Person, Organization, or array
	Creator     any `json:"creator,omitempty"`     // Person, Organization, or array
	Contributor any `json:"contributor,omitempty"` // Person, Organization, or array
	Editor      any `json:"editor,omitempty"`      // Person or array
	Publisher   any `json:"publisher,omitempty"`   // Person or Organization

	// Dates
	DateCreated   string `json:"dateCreated,omitempty"`
	DatePublished string `json:"datePublished,omitempty"`
	DateModified  string `json:"dateModified,omitempty"`
	CopyrightYear any    `json:"copyrightYear,omitempty"` // int or string

	// Classification
	Genre                any    `json:"genre,omitempty"`      // string or []string
	About                any    `json:"about,omitempty"`      // Thing or []Thing
	Keywords             any    `json:"keywords,omitempty"`   // string or []string
	InLanguage           any    `json:"inLanguage,omitempty"` // string or Language
	LearningResourceType string `json:"learningResourceType,omitempty"`

	// Rights
	License            any    `json:"license,omitempty"` // URL string or CreativeWork
	CopyrightHolder    any    `json:"copyrightHolder,omitempty"`
	CopyrightNotice    string `json:"copyrightNotice,omitempty"`
	ConditionsOfAccess string `json:"conditionsOfAccess,omitempty"`

	// Relations
	IsPartOf    any `json:"isPartOf,omitempty"`    // CreativeWork or URL
	HasPart     any `json:"hasPart,omitempty"`     // CreativeWork or array
	Citation    any `json:"citation,omitempty"`    // CreativeWork, URL, or Text
	IsBasedOn   any `json:"isBasedOn,omitempty"`   // CreativeWork or URL
	WorkExample any `json:"workExample,omitempty"` // CreativeWork

	// Physical/digital description
	EncodingFormat string `json:"encodingFormat,omitempty"` // MIME type
	ContentSize    string `json:"contentSize,omitempty"`
	ContentURL     string `json:"contentUrl,omitempty"`

	// Location
	LocationCreated  any    `json:"locationCreated,omitempty"` // Place
	SpatialCoverage  any    `json:"spatialCoverage,omitempty"` // Place or Text
	TemporalCoverage string `json:"temporalCoverage,omitempty"`
	ContentLocation  any    `json:"contentLocation,omitempty"` // Place

	// Funding
	Funder  any `json:"funder,omitempty"` // Person or Organization
	Sponsor any `json:"sponsor,omitempty"`
}

// ScholarlyArticle represents an academic article.
type ScholarlyArticle struct {
	CreativeWork

	// Article-specific
	ArticleBody    string `json:"articleBody,omitempty"`
	ArticleSection string `json:"articleSection,omitempty"`
	PageStart      string `json:"pageStart,omitempty"`
	PageEnd        string `json:"pageEnd,omitempty"`
	Pagination     string `json:"pagination,omitempty"`
	WordCount      int    `json:"wordCount,omitempty"`

	// Journal info
	IsPartOfJournal any `json:"isPartOf,omitempty"` // Periodical or PublicationVolume
}

// Book represents a book.
type Book struct {
	CreativeWork

	// Book-specific
	ISBN            string `json:"isbn,omitempty"`
	NumberOfPages   int    `json:"numberOfPages,omitempty"`
	BookEdition     string `json:"bookEdition,omitempty"`
	BookFormat      string `json:"bookFormat,omitempty"` // EBook, Hardcover, Paperback, etc.
	Illustrator     any    `json:"illustrator,omitempty"`
	AbridgedContent bool   `json:"abridged,omitempty"`
}

// Dataset represents a dataset.
type Dataset struct {
	CreativeWork

	// Dataset-specific
	Distribution          any    `json:"distribution,omitempty"` // DataDownload or array
	MeasurementTechnique  string `json:"measurementTechnique,omitempty"`
	VariableMeasured      any    `json:"variableMeasured,omitempty"`      // string or PropertyValue
	IncludedInDataCatalog any    `json:"includedInDataCatalog,omitempty"` // DataCatalog
}

// Collection represents a collection of creative works.
type Collection struct {
	CreativeWork

	// Collection-specific
	CollectionSize int `json:"collectionSize,omitempty"`
}

// DigitalDocument represents a digital document.
type DigitalDocument struct {
	CreativeWork

	// Document-specific
	HasDigitalDocumentPermission any `json:"hasDigitalDocumentPermission,omitempty"`
}

// Manuscript represents a manuscript or unpublished work.
type Manuscript struct {
	CreativeWork
}

// MediaObject is the base for audio/video/image objects.
type MediaObject struct {
	CreativeWork

	// Media-specific
	ContentURL        string `json:"contentUrl,omitempty"`
	EmbedURL          string `json:"embedUrl,omitempty"`
	EncodingFormat    string `json:"encodingFormat,omitempty"`
	Duration          string `json:"duration,omitempty"` // ISO 8601 duration
	Width             any    `json:"width,omitempty"`    // Distance or QuantitativeValue
	Height            any    `json:"height,omitempty"`
	Bitrate           string `json:"bitrate,omitempty"`
	PlayerType        string `json:"playerType,omitempty"`
	ProductionCompany any    `json:"productionCompany,omitempty"`
	UploadDate        string `json:"uploadDate,omitempty"`
}

// AudioObject represents audio content.
type AudioObject struct {
	MediaObject

	// Audio-specific
	Transcript string `json:"transcript,omitempty"`
}

// ImageObject represents an image.
type ImageObject struct {
	MediaObject

	// Image-specific
	Caption              any  `json:"caption,omitempty"` // string or MediaObject
	ExifData             any  `json:"exifData,omitempty"`
	RepresentativeOfPage bool `json:"representativeOfPage,omitempty"`
}

// VideoObject represents video content.
type VideoObject struct {
	MediaObject

	// Video-specific
	Actor          any    `json:"actor,omitempty"`
	Director       any    `json:"director,omitempty"`
	MusicBy        any    `json:"musicBy,omitempty"`
	Thumbnail      any    `json:"thumbnail,omitempty"` // ImageObject
	Transcript     string `json:"transcript,omitempty"`
	VideoFrameSize string `json:"videoFrameSize,omitempty"`
	VideoQuality   string `json:"videoQuality,omitempty"`
}

// PublicationIssue represents an issue of a periodical.
type PublicationIssue struct {
	CreativeWork

	IssueNumber string `json:"issueNumber,omitempty"`
	PageStart   string `json:"pageStart,omitempty"`
	PageEnd     string `json:"pageEnd,omitempty"`
}

// PublicationVolume represents a volume of a periodical.
type PublicationVolume struct {
	CreativeWork

	VolumeNumber string `json:"volumeNumber,omitempty"`
	PageStart    string `json:"pageStart,omitempty"`
	PageEnd      string `json:"pageEnd,omitempty"`
}

// Person represents a person.
type Person struct {
	Thing

	GivenName       string `json:"givenName,omitempty"`
	FamilyName      string `json:"familyName,omitempty"`
	AdditionalName  string `json:"additionalName,omitempty"` // Middle name
	HonorificPrefix string `json:"honorificPrefix,omitempty"`
	HonorificSuffix string `json:"honorificSuffix,omitempty"`
	Email           string `json:"email,omitempty"`
	Affiliation     any    `json:"affiliation,omitempty"` // Organization or array
	JobTitle        string `json:"jobTitle,omitempty"`
	WorksFor        any    `json:"worksFor,omitempty"` // Organization
}

// Organization represents an organization.
type Organization struct {
	Thing

	LegalName          string `json:"legalName,omitempty"`
	Email              string `json:"email,omitempty"`
	Telephone          string `json:"telephone,omitempty"`
	Address            any    `json:"address,omitempty"` // PostalAddress or Text
	ParentOrganization any    `json:"parentOrganization,omitempty"`
	SubOrganization    any    `json:"subOrganization,omitempty"`
	Department         any    `json:"department,omitempty"`
}

// PropertyValue represents a property-value pair for identifiers.
type PropertyValue struct {
	Type        string `json:"@type,omitempty"`
	PropertyID  string `json:"propertyID,omitempty"`
	Value       string `json:"value,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// DefinedTerm represents a term from a controlled vocabulary.
// Used for rich representation of genres, subjects, keywords with authority URIs.
type DefinedTerm struct {
	Type             string          `json:"@type,omitempty"`
	Name             string          `json:"name,omitempty"`
	URL              string          `json:"url,omitempty"`
	SameAs           string          `json:"sameAs,omitempty"`
	InDefinedTermSet *DefinedTermSet `json:"inDefinedTermSet,omitempty"`
}

// DefinedTermSet represents a controlled vocabulary (thesaurus, ontology, etc.).
type DefinedTermSet struct {
	Type string `json:"@type,omitempty"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// VocabularyInfo holds metadata about a controlled vocabulary.
type VocabularyInfo struct {
	Name string // Human-readable name (e.g., "Art & Architecture Thesaurus")
	URL  string // Base URL (e.g., "http://vocab.getty.edu/aat")
}

// KnownVocabularies maps vocabulary sources to their metadata.
var KnownVocabularies = map[string]VocabularyInfo{
	"aat": {
		Name: "Art & Architecture Thesaurus",
		URL:  "http://vocab.getty.edu/aat",
	},
	"lcsh": {
		Name: "Library of Congress Subject Headings",
		URL:  "http://id.loc.gov/authorities/subjects",
	},
	"lcnaf": {
		Name: "Library of Congress Name Authority File",
		URL:  "http://id.loc.gov/authorities/names",
	},
	"tgn": {
		Name: "Getty Thesaurus of Geographic Names",
		URL:  "http://vocab.getty.edu/tgn",
	},
	"mesh": {
		Name: "Medical Subject Headings",
		URL:  "https://meshb.nlm.nih.gov",
	},
	"fast": {
		Name: "Faceted Application of Subject Terminology",
		URL:  "http://id.worldcat.org/fast",
	},
}
