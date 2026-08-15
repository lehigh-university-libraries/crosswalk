package archivesspace

import "encoding/json"

// Snapshot is the versioned, transport-free representation produced by an
// ArchivesSpace acquisition client after it has fetched a complete resource
// tree. Records contains resource and archival_object JSONModel objects;
// OrderedRecords carries the official tree order and depth evidence.
type Snapshot struct {
	CrosswalkFormat string            `json:"crosswalk_format"`
	Version         int               `json:"version"`
	SourceURI       string            `json:"source_uri,omitempty"`
	SourceID        string            `json:"source_id,omitempty"`
	RetrievedAt     string            `json:"retrieved_at,omitempty"`
	Records         []json.RawMessage `json:"records"`
	OrderedRecords  OrderedRecords    `json:"ordered_records"`
}

// OrderedRecords is the ArchivesSpace resource_ordered_records response shape.
type OrderedRecords struct {
	JSONModelType string          `json:"jsonmodel_type"`
	URIs          []OrderedRecord `json:"uris"`
}

// OrderedRecord is one resource-tree entry in canonical depth-first order.
// Resolved may hold the JSONModel object when parsing a directly resolved
// resource_ordered_records API response.
type OrderedRecord struct {
	Ref           string          `json:"ref"`
	DisplayString string          `json:"display_string"`
	Depth         *int            `json:"depth"`
	Level         string          `json:"level"`
	Resolved      json.RawMessage `json:"_resolved"`
}

type searchResponse struct {
	Results []searchResult `json:"results"`
}

type searchResult struct {
	PrimaryType string          `json:"primary_type"`
	JSON        json.RawMessage `json:"json"`
}

type jsonModel struct {
	JSONModelType string `json:"jsonmodel_type"`
	URI           string `json:"uri"`
	Title         string `json:"title"`
	DisplayString string `json:"display_string"`
	Publish       *bool  `json:"publish"`
	Suppressed    *bool  `json:"suppressed"`

	ExternalIDs               []externalID       `json:"external_ids"`
	Subjects                  []reference        `json:"subjects"`
	Extents                   []extent           `json:"extents"`
	LanguageMaterials         []languageMaterial `json:"lang_materials"`
	Dates                     []dateValue        `json:"dates"`
	ExternalDocuments         []externalDocument `json:"external_documents"`
	RightsStatements          []rightsStatement  `json:"rights_statements"`
	LinkedAgents              []linkedAgent      `json:"linked_agents"`
	Notes                     []note             `json:"notes"`
	Instances                 []instance         `json:"instances"`
	RepresentativeFileVersion *fileVersion       `json:"representative_file_version"`

	ID0 string `json:"id_0"`
	ID1 string `json:"id_1"`
	ID2 string `json:"id_2"`
	ID3 string `json:"id_3"`

	EADID                      string   `json:"ead_id"`
	EADLocation                string   `json:"ead_location"`
	ExternalARKURL             string   `json:"external_ark_url"`
	ImportCurrentARK           string   `json:"import_current_ark"`
	ImportPreviousARKs         []string `json:"import_previous_arks"`
	ARKName                    arkName  `json:"ark_name"`
	ResourceType               string   `json:"resource_type"`
	FindingAidTitle            string   `json:"finding_aid_title"`
	FindingAidSubtitle         string   `json:"finding_aid_subtitle"`
	FindingAidAuthor           string   `json:"finding_aid_author"`
	FindingAidDate             string   `json:"finding_aid_date"`
	FindingAidLanguage         string   `json:"finding_aid_language"`
	FindingAidScript           string   `json:"finding_aid_script"`
	FindingAidLanguageNote     string   `json:"finding_aid_language_note"`
	FindingAidDescriptionRules string   `json:"finding_aid_description_rules"`
	FindingAidEditionStatement string   `json:"finding_aid_edition_statement"`
	FindingAidSeriesStatement  string   `json:"finding_aid_series_statement"`
	FindingAidStatus           string   `json:"finding_aid_status"`
	FindingAidNote             string   `json:"finding_aid_note"`
	RepositoryProcessingNote   string   `json:"repository_processing_note"`

	RefID             string      `json:"ref_id"`
	ComponentID       string      `json:"component_id"`
	Level             string      `json:"level"`
	OtherLevel        string      `json:"other_level"`
	Position          *int        `json:"position"`
	RestrictionsApply *bool       `json:"restrictions_apply"`
	Restrictions      *bool       `json:"restrictions"`
	Parent            reference   `json:"parent"`
	Resource          reference   `json:"resource"`
	Ancestors         []reference `json:"ancestors"`
}

type arkName struct {
	Current           string   `json:"current"`
	CurrentIsExternal bool     `json:"current_is_external"`
	Previous          []string `json:"previous"`
}

type externalID struct {
	ExternalID string `json:"external_id"`
	Source     string `json:"source"`
}

type sourceIdentifierExtra struct {
	Scheme        string `json:"scheme"`
	Value         string `json:"value"`
	NamespaceURI  string `json:"namespace_uri,omitempty"`
	IdentityLevel string `json:"identity_level"`
}

type reference struct {
	Ref           string          `json:"ref"`
	Title         string          `json:"title"`
	DisplayString string          `json:"display_string"`
	Level         string          `json:"level"`
	Resolved      json.RawMessage `json:"_resolved"`
}

type extent struct {
	Portion          string `json:"portion"`
	Number           string `json:"number"`
	ExtentType       string `json:"extent_type"`
	ContainerSummary string `json:"container_summary"`
	PhysicalDetails  string `json:"physical_details"`
	Dimensions       string `json:"dimensions"`
}

type languageMaterial struct {
	LanguageAndScript struct {
		Language string `json:"language"`
		Script   string `json:"script"`
	} `json:"language_and_script"`
	Notes []note `json:"notes"`
}

type dateValue struct {
	DateType   string `json:"date_type"`
	Label      string `json:"label"`
	Certainty  string `json:"certainty"`
	Expression string `json:"expression"`
	Begin      string `json:"begin"`
	End        string `json:"end"`
	Era        string `json:"era"`
	Calendar   string `json:"calendar"`
}

type externalDocument struct {
	Title    string `json:"title"`
	Location string `json:"location"`
	Publish  *bool  `json:"publish"`
}

type rightsStatement struct {
	RightsType        string             `json:"rights_type"`
	Identifier        string             `json:"identifier"`
	Status            string             `json:"status"`
	DeterminationDate string             `json:"determination_date"`
	StartDate         string             `json:"start_date"`
	EndDate           string             `json:"end_date"`
	LicenseTerms      string             `json:"license_terms"`
	StatuteCitation   string             `json:"statute_citation"`
	Jurisdiction      string             `json:"jurisdiction"`
	OtherRightsBasis  string             `json:"other_rights_basis"`
	Notes             []note             `json:"notes"`
	ExternalDocuments []externalDocument `json:"external_documents"`
	LinkedAgents      []linkedAgent      `json:"linked_agents"`
	Acts              json.RawMessage    `json:"acts"`
}

type rightsAct struct {
	JSONModelType string `json:"jsonmodel_type"`
	ActType       string `json:"act_type"`
	Restriction   string `json:"restriction"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	Notes         []note `json:"notes"`
}

type linkedAgent struct {
	Role      string          `json:"role"`
	Relator   string          `json:"relator"`
	Title     string          `json:"title"`
	Ref       string          `json:"ref"`
	IsPrimary bool            `json:"is_primary"`
	Resolved  json.RawMessage `json:"_resolved"`
}

type note struct {
	JSONModelType string          `json:"jsonmodel_type"`
	Type          string          `json:"type"`
	Label         string          `json:"label"`
	Title         string          `json:"title"`
	Content       json.RawMessage `json:"content"`
	Subnotes      []note          `json:"subnotes"`
	Items         json.RawMessage `json:"items"`
}

type instance struct {
	InstanceType     string        `json:"instance_type"`
	IsRepresentative bool          `json:"is_representative"`
	SubContainer     *subContainer `json:"sub_container"`
	DigitalObject    *reference    `json:"digital_object"`
}

type subContainer struct {
	TopContainer  reference `json:"top_container"`
	Type2         string    `json:"type_2"`
	Indicator2    string    `json:"indicator_2"`
	Barcode2      string    `json:"barcode_2"`
	Type3         string    `json:"type_3"`
	Indicator3    string    `json:"indicator_3"`
	DisplayString string    `json:"display_string"`
}

type topContainer struct {
	URI                string              `json:"uri"`
	Type               string              `json:"type"`
	Indicator          string              `json:"indicator"`
	Barcode            string              `json:"barcode"`
	DisplayString      string              `json:"display_string"`
	LongDisplayString  string              `json:"long_display_string"`
	ContainerLocations []containerLocation `json:"container_locations"`
}

type containerLocation struct {
	Status    string          `json:"status"`
	StartDate string          `json:"start_date"`
	EndDate   string          `json:"end_date"`
	Note      string          `json:"note"`
	Ref       string          `json:"ref"`
	Resolved  json.RawMessage `json:"_resolved"`
}

type fileVersion struct {
	Identifier        string `json:"identifier"`
	FileURI           string `json:"file_uri"`
	LinkURI           string `json:"link_uri"`
	FileName          string `json:"file_name"`
	FileFormatName    string `json:"file_format_name"`
	FileFormatVersion string `json:"file_format_version"`
	FileSizeBytes     int64  `json:"file_size_bytes"`
	Checksum          string `json:"checksum"`
	ChecksumMethod    string `json:"checksum_method"`
	UseStatement      string `json:"use_statement"`
	Caption           string `json:"caption"`
	DerivedFrom       string `json:"derived_from"`
	IsRepresentative  bool   `json:"is_representative"`
}

type resolvedAgent struct {
	JSONModelType    string            `json:"jsonmodel_type"`
	URI              string            `json:"uri"`
	Title            string            `json:"title"`
	DisplayName      json.RawMessage   `json:"display_name"`
	Names            []agentName       `json:"names"`
	ExternalIDs      []externalID      `json:"external_ids"`
	AgentIdentifiers []agentIdentifier `json:"agent_identifiers"`
}

type agentIdentifier struct {
	EntityIdentifier string `json:"entity_identifier"`
	IdentifierType   string `json:"identifier_type"`
}

type agentName struct {
	PrimaryName      string `json:"primary_name"`
	RestOfName       string `json:"rest_of_name"`
	Prefix           string `json:"prefix"`
	Suffix           string `json:"suffix"`
	FullerForm       string `json:"fuller_form"`
	Title            string `json:"title"`
	NameOrder        string `json:"name_order"`
	SubordinateName1 string `json:"subordinate_name_1"`
	SubordinateName2 string `json:"subordinate_name_2"`
	FamilyName       string `json:"family_name"`
	SoftwareName     string `json:"software_name"`
}

type resolvedSubject struct {
	URI         string        `json:"uri"`
	Title       string        `json:"title"`
	Source      string        `json:"source"`
	AuthorityID string        `json:"authority_id"`
	Terms       []subjectTerm `json:"terms"`
}

type subjectTerm struct {
	Term       string `json:"term"`
	TermType   string `json:"term_type"`
	Vocabulary string `json:"vocabulary"`
}

type resolvedDigitalObject struct {
	URI          string        `json:"uri"`
	Title        string        `json:"title"`
	FileVersions []fileVersion `json:"file_versions"`
}
