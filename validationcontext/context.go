// Package validationcontext defines the deployment-aware capabilities used by
// metadata validation. Query values are inert data: resolver implementations
// choose their own trusted repository and authority endpoints and must never
// treat a candidate value as a caller-selected network destination.
package validationcontext

import "context"

// Operation identifies the Workbench action represented by a row.
type Operation string

const (
	// OperationCreate creates a repository object and its primary media.
	OperationCreate Operation = "create"
	// OperationUpdate updates an existing repository object.
	OperationUpdate Operation = "update"
	// OperationAddMedia adds media to an existing repository object.
	OperationAddMedia Operation = "add_media"
	// OperationAgents creates or updates linked-agent terms.
	OperationAgents Operation = "agents"
	// OperationUnpublishedSupplemental stages unpublished supplemental media.
	OperationUnpublishedSupplemental Operation = "unpublished_supplemental"
	// OperationPendingSupplemental stages published supplemental media.
	OperationPendingSupplemental Operation = "pending_supplemental"
)

// Resolver supplies every deployment-aware validation capability. Resolver
// implementations must be safe for concurrent use.
type Resolver interface {
	NodeExistenceResolver
	EntityReferenceResolver
	AllowedValueResolver
	FileReadabilityResolver
	TGNResolver
	URLAliasAvailabilityResolver
}

// NodeExistenceResolver reports whether a repository node exists.
type NodeExistenceResolver interface {
	NodeExists(ctx context.Context, nodeID uint64) (bool, error)
}

// EntityReferenceResolver reports whether a typed repository reference exists.
type EntityReferenceResolver interface {
	EntityReferenceExists(ctx context.Context, query EntityReferenceQuery) (bool, error)
}

// AllowedValueResolver reports whether a value is allowed for a canonical
// field in the supplied row context.
type AllowedValueResolver interface {
	AllowedValue(ctx context.Context, query AllowedValueQuery) (bool, error)
}

// FileReadabilityResolver reports whether a normalized staging path is
// readable. Implementations must keep access within their configured staging
// root.
type FileReadabilityResolver interface {
	FileReadable(ctx context.Context, normalizedPath string) (bool, error)
}

// TGNResolver reports whether a numeric Getty Thesaurus of Geographic Names
// identifier resolves through the implementation's fixed authority endpoint.
type TGNResolver interface {
	TGNResolves(ctx context.Context, termID string) (bool, error)
}

// URLAliasAvailabilityResolver reports whether an exact Drupal URL alias is
// unused at the resolver's fixed repository origin.
type URLAliasAvailabilityResolver interface {
	URLAliasAvailable(ctx context.Context, alias string) (bool, error)
}

// AllowedValueQuery binds a candidate to a canonical mapping field and frozen
// provider description. Provider is sealed model data; a resolver queries its
// fixed selected site and never executes Provider locally.
type AllowedValueQuery struct {
	Field              string
	SourceType         string
	Provider           string
	Value              string
	Operation          Operation
	NodeID             uint64
	NodeIDPresent      bool
	Bundle             string
	ProfileFingerprint string
	ModelFingerprint   string
	Fields             []CanonicalFieldValue
}

// CanonicalFieldValue is one mapping-identified field from the current row.
// Values retain the sheet's declared cell encoding.
type CanonicalFieldValue struct {
	Field      string
	SourceType string
	Values     []string
}

// EntityReferenceKind identifies the parsed representation supplied to a
// trusted, fixed-origin entity resolver.
type EntityReferenceKind string

const (
	// EntityReferenceID is a numeric repository entity identifier.
	EntityReferenceID EntityReferenceKind = "id"
	// EntityReferenceName is a human-readable entity label.
	EntityReferenceName EntityReferenceKind = "name"
	// EntityReferenceURI is an inert authority URI candidate.
	EntityReferenceURI EntityReferenceKind = "uri"
)

// EntityReferenceQuery contains sealed model constraints and one inert
// spreadsheet value. Resolver implementations must not fetch Value directly.
type EntityReferenceQuery struct {
	EntityType string
	Bundles    []string
	Field      string
	SourceType string
	Handler    string
	Kind       EntityReferenceKind
	Value      string
}
