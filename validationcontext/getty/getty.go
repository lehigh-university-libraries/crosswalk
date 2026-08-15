// Package getty provides a fixed-origin Getty TGN validation capability.
package getty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/lehigh-university-libraries/crosswalk/internal/strictjson"
	"github.com/lehigh-university-libraries/crosswalk/source"
	"github.com/lehigh-university-libraries/crosswalk/validationcontext"
)

const (
	tgnEndpoint          = "https://vocab.getty.edu/tgn/"
	maxTermIDBytes       = 32
	maxTGNResponseBytes  = int64(512 << 10)
	gettyJSONContentType = "application/json"
)

// Client resolves numeric TGN identifiers against Getty's fixed HTTPS
// authority endpoint. HTTP may be configured before first use for tests or
// deployment-specific transport settings; Client is safe for concurrent use
// after configuration.
type Client struct {
	HTTP source.HTTPDoer
}

var _ validationcontext.TGNResolver = (*Client)(nil)

// NewClient returns a TGN resolver using Crosswalk's protected, no-proxy HTTP
// transport and bounded response defaults.
func NewClient() *Client {
	return &Client{}
}

// TGNResolves reports whether termID resolves to a JSON object whose identity
// is the same TGN term. Candidate input contributes only one validated numeric
// path segment; it can never choose a scheme, authority, or redirect policy.
func (c *Client) TGNResolves(ctx context.Context, termID string) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("resolving Getty TGN term: context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("resolving Getty TGN term: %w", err)
	}
	if c == nil {
		return false, fmt.Errorf("resolving Getty TGN term: client is nil")
	}
	if err := validateTermID(termID); err != nil {
		return false, err
	}
	ctx = validationcontext.EnsureRequestBudget(ctx)

	acquisition := source.NewClient()
	acquisition.HTTP = c.HTTP
	acquisition.UserAgent = "crosswalk/context-tgn"
	acquisition.MaxResponseBytes = maxTGNResponseBytes
	if err := validationcontext.ConsumeNetworkRequest(ctx); err != nil {
		return false, fmt.Errorf("resolving Getty TGN term: %w", err)
	}
	document, err := acquisition.Fetch(ctx, tgnEndpoint+termID+".json", gettyJSONContentType)
	if err != nil {
		var status *source.HTTPStatusError
		if errors.As(err, &status) && (status.Status == http.StatusNotFound || status.Status == http.StatusGone) {
			return false, nil
		}
		return false, fmt.Errorf("resolving Getty TGN term: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("resolving Getty TGN term: %w", err)
	}
	if !utf8.Valid(document.Data) {
		return false, fmt.Errorf("resolving Getty TGN term: response is not valid UTF-8")
	}
	if err := strictjson.RejectDuplicateNames(document.Data); err != nil {
		return false, fmt.Errorf("resolving Getty TGN term: invalid JSON response: %w", err)
	}

	var record struct {
		ID    string `json:"id"`
		Label string `json:"_label"`
	}
	decoder := json.NewDecoder(bytes.NewReader(document.Data))
	if err := decoder.Decode(&record); err != nil {
		return false, fmt.Errorf("resolving Getty TGN term: decoding JSON response: %w", err)
	}
	if strings.TrimSpace(record.Label) == "" {
		return false, fmt.Errorf("resolving Getty TGN term: JSON response has no label")
	}
	if !sameTGNIdentity(record.ID, termID) {
		return false, fmt.Errorf("resolving Getty TGN term: JSON response identity does not match the requested term")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("resolving Getty TGN term: %w", err)
	}
	return true, nil
}

func validateTermID(termID string) error {
	if termID == "" || len(termID) > maxTermIDBytes {
		return fmt.Errorf("getty TGN term ID must contain between 1 and %d ASCII digits", maxTermIDBytes)
	}
	for _, character := range termID {
		if character < '0' || character > '9' {
			return fmt.Errorf("getty TGN term ID must contain between 1 and %d ASCII digits", maxTermIDBytes)
		}
	}
	return nil
}

func sameTGNIdentity(rawIdentity, termID string) bool {
	identity, err := url.ParseRequestURI(strings.TrimSpace(rawIdentity))
	if err != nil || identity.User != nil || identity.Opaque != "" || identity.RawQuery != "" || identity.Fragment != "" || identity.ForceQuery {
		return false
	}
	if identity.Scheme != "http" && identity.Scheme != "https" {
		return false
	}
	if !strings.EqualFold(identity.Hostname(), "vocab.getty.edu") || identity.Port() != "" || identity.RawPath != "" {
		return false
	}
	path := strings.TrimSuffix(identity.Path, "/")
	return path == "/tgn/"+termID || path == "/page/tgn/"+termID
}
