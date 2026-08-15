package omeka_s

import (
	"encoding/json"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

const (
	maxInputBytes  = int64(64 << 20)
	maxResources   = 100_000
	maxModelValues = 100_000
	maxTermValues  = 100_000
)

// Snapshot is a static Omeka S acquisition envelope. Schema resources describe
// the installation-specific metadata model; content resources are unmodified
// Omeka S REST API JSON-LD objects. Acquisition clients should populate this
// type and call CanonicalJSON before persistence or conversion.
type Snapshot struct {
	CrosswalkFormat   string            `json:"crosswalk_format"`
	Version           int               `json:"version"`
	SourceURI         string            `json:"source_uri,omitempty"`
	SourceID          string            `json:"source_id,omitempty"`
	RetrievedAt       string            `json:"retrieved_at,omitempty"`
	Vocabularies      []json.RawMessage `json:"vocabularies,omitempty"`
	Properties        []json.RawMessage `json:"properties,omitempty"`
	ResourceClasses   []json.RawMessage `json:"resource_classes,omitempty"`
	ResourceTemplates []json.RawMessage `json:"resource_templates,omitempty"`
	ItemSets          []json.RawMessage `json:"item_sets,omitempty"`
	Items             []json.RawMessage `json:"items,omitempty"`
	Media             []json.RawMessage `json:"media,omitempty"`
}

type decodedInput struct {
	model      *schemaModel
	resources  []*resource
	provenance format.DatasetProvenance
}

type resourceKind string

const (
	kindItem    resourceKind = "item"
	kindItemSet resourceKind = "item_set"
	kindMedia   resourceKind = "media"
)

type resource struct {
	kind             resourceKind
	id               int64
	uri              string
	types            []string
	title            string
	isPublic         bool
	created          string
	modified         string
	resourceClass    *reference
	resourceTemplate *reference
	itemSets         []reference
	media            []reference
	item             *reference
	properties       []termValues
	metadata         []namedRawValue

	itemSetOpen bool
	mediaType   string
	mediaSource string
	filename    string
	originalURL string
	sha256      string
	size        int64
	lang        string
	altText     string
}

type reference struct {
	id    int64
	uri   string
	title string
	kind  string
	raw   json.RawMessage
}

type termValues struct {
	term   string
	values []valueObject
}

type valueObject struct {
	typeName      string
	propertyID    string
	propertyLabel string
	isPublic      *bool
	literal       string
	language      string
	uri           string
	label         string
	resourceID    int64
	resourceName  string
	displayTitle  string
	thumbnailURL  string
	raw           json.RawMessage
}

type namedRawValue struct {
	name string
	raw  json.RawMessage
}

type schemaModel struct {
	vocabulariesByID     map[int64]vocabulary
	vocabulariesByPrefix map[string]vocabulary
	propertiesByID       map[int64]property
	propertiesByTerm     map[string]property
	classesByID          map[int64]resourceClass
	templatesByID        map[int64]resourceTemplate
	fingerprint          string
	contractFingerprint  string
}

type vocabulary struct {
	id           int64
	uri          string
	prefix       string
	namespaceURI string
	label        string
	comment      string
	raw          json.RawMessage
}

type property struct {
	id           int64
	uri          string
	localName    string
	label        string
	comment      string
	term         string
	vocabularyID int64
	raw          json.RawMessage
}

type resourceClass struct {
	id           int64
	uri          string
	localName    string
	label        string
	comment      string
	term         string
	vocabularyID int64
	raw          json.RawMessage
}

type resourceTemplate struct {
	id                    int64
	uri                   string
	label                 string
	resourceClassID       int64
	titlePropertyID       int64
	descriptionPropertyID int64
	properties            []templateProperty
	raw                   json.RawMessage
}

type templateProperty struct {
	propertyID       int64
	alternateLabel   string
	alternateComment string
	dataTypes        []string
	isRequired       bool
	isPrivate        bool
	defaultLanguage  string
}
