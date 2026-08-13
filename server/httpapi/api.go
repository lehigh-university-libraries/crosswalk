// Package httpapi exposes Crosswalk metadata operations over HTTP.
package httpapi

import (
	"context"
	"io"

	"github.com/lehigh-university-libraries/crosswalk/reconcile"
)

const (
	// CheckPath is the Fabricator-compatible metadata validation endpoint.
	CheckPath = "/workbench/check"
	// TransformPath is the Fabricator-compatible Workbench conversion endpoint.
	TransformPath = "/workbench/transform"
	// MatchesPath produces an existing-item review report without transforming or uploading records.
	MatchesPath = "/workbench/matches"
	// HealthcheckPath reports whether the HTTP process is serving requests.
	HealthcheckPath = "/healthcheck"
)

// CheckResult maps spreadsheet cell references (or record field paths) to
// validation messages. It deliberately preserves Fabricator's JSON response
// shape while allowing the validation implementation to evolve independently
// of the HTTP layer.
type CheckResult map[string]string

// Artifact is one deterministic output file in a Workbench transformation.
type Artifact struct {
	Name      string
	MediaType string
	Data      []byte
}

// Engine owns metadata validation and transformation. Implementations must be
// safe for concurrent use by HTTP handlers. Network access and mutation are not
// part of this interface; callers that need site data must supply it before a
// request reaches the engine.
type Engine interface {
	Check(ctx context.Context, rows [][]string) (CheckResult, error)
	Transform(ctx context.Context, input io.Reader) ([]Artifact, error)
}

// MatchEngine extends Engine with read-only duplicate reconciliation.
type MatchEngine interface {
	Engine
	Matches(ctx context.Context, input io.Reader, mode string) (MatchResponse, error)
}

// MatchResponse contains the machine-readable report and review CSV.
type MatchResponse struct {
	Report    reconcile.Report
	ReviewCSV []byte
}

// BearerVerifier optionally verifies bearer tokens without coupling the HTTP
// package to a particular identity provider or network client. Implementations
// must honor context cancellation and deadlines.
type BearerVerifier interface {
	Verify(ctx context.Context, token string) error
}
