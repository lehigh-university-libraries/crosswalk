// Package doi acquires a deterministic CSL-JSON representation for a DOI.
package doi

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

const (
	// DefaultBaseURL is the canonical DOI content-negotiation endpoint.
	DefaultBaseURL = "https://doi.org"
	// CSLJSONMediaType requests Citation Style Language JSON from a DOI resolver.
	CSLJSONMediaType = "application/vnd.citationstyles.csl+json"
)

// Client resolves DOI metadata through content negotiation.
type Client struct {
	BaseURL string
	HTTP    *source.Client
}

// NewClient returns a DOI client using doi.org and safe acquisition defaults.
func NewClient() *Client {
	httpClient := source.NewClient()
	httpClient.RedirectPolicy = source.RedirectHTTPS
	return &Client{BaseURL: DefaultBaseURL, HTTP: httpClient}
}

// ResolveCSL retrieves one DOI as CSL-JSON.
func (c *Client) ResolveCSL(ctx context.Context, identifier string) (*source.Document, error) {
	doi := normalize(identifier)
	if !strings.HasPrefix(doi, "10.") || !strings.Contains(doi, "/") {
		return nil, fmt.Errorf("resolving DOI: invalid DOI %q", identifier)
	}
	base := DefaultBaseURL
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		base = strings.TrimRight(c.BaseURL, "/")
	}
	endpoint, err := url.JoinPath(base, doi)
	if err != nil {
		return nil, fmt.Errorf("resolving DOI: constructing endpoint: %w", err)
	}
	client := source.NewClient()
	client.RedirectPolicy = source.RedirectHTTPS
	if c != nil && c.HTTP != nil {
		client = c.HTTP
	}
	doc, err := client.Fetch(ctx, endpoint, CSLJSONMediaType)
	if err != nil {
		return nil, fmt.Errorf("resolving DOI %q: %w", doi, err)
	}
	return doc, nil
}

func normalize(identifier string) string {
	doi := strings.TrimSpace(identifier)
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:", "DOI:"} {
		doi = strings.TrimPrefix(doi, prefix)
	}
	return strings.TrimSpace(doi)
}
