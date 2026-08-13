// Package arxiv acquires Atom search feeds and OAI enrichment records from arXiv.
package arxiv

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

const (
	// DefaultAPIBaseURL is arXiv's Atom query endpoint.
	DefaultAPIBaseURL = "https://export.arxiv.org/api/query"
	// DefaultOAIBaseURL is arXiv's OAI-PMH endpoint.
	DefaultOAIBaseURL = "https://export.arxiv.org/oai2"
	atomMediaType     = "application/atom+xml"
)

// SearchOptions selects an arXiv query page.
type SearchOptions struct {
	Query      string
	IDs        []string
	Start      int
	MaxResults int
}

// Client retrieves arXiv metadata without parsing it into Hub records.
type Client struct {
	APIBaseURL string
	OAIBaseURL string
	HTTP       *source.Client
}

// NewClient returns an arXiv client using the public export endpoints.
func NewClient() *Client {
	return &Client{
		APIBaseURL: DefaultAPIBaseURL,
		OAIBaseURL: DefaultOAIBaseURL,
		HTTP:       source.NewClient(),
	}
}

// Search retrieves one Atom result page. Pagination policy remains with the caller.
func (c *Client) Search(ctx context.Context, opts SearchOptions) (*source.Document, error) {
	if opts.Start < 0 {
		return nil, fmt.Errorf("searching arXiv: start must not be negative")
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = 100
	}
	if strings.TrimSpace(opts.Query) == "" && len(opts.IDs) == 0 {
		return nil, fmt.Errorf("searching arXiv: a query or at least one ID is required")
	}

	endpoint, err := url.Parse(c.apiBaseURL())
	if err != nil {
		return nil, fmt.Errorf("searching arXiv: parsing endpoint: %w", err)
	}
	query := endpoint.Query()
	if value := strings.TrimSpace(opts.Query); value != "" {
		query.Set("search_query", value)
	}
	if len(opts.IDs) > 0 {
		ids := make([]string, 0, len(opts.IDs))
		for _, id := range opts.IDs {
			if normalized := normalizeID(id); normalized != "" {
				ids = append(ids, normalized)
			}
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("searching arXiv: no valid IDs were supplied")
		}
		query.Set("id_list", strings.Join(ids, ","))
	}
	query.Set("start", strconv.Itoa(opts.Start))
	query.Set("max_results", strconv.Itoa(opts.MaxResults))
	endpoint.RawQuery = query.Encode()

	doc, err := c.httpClient().Fetch(ctx, endpoint.String(), atomMediaType)
	if err != nil {
		return nil, fmt.Errorf("searching arXiv: %w", err)
	}
	return doc, nil
}

// OAI retrieves one arXiv record using the arXiv OAI metadata prefix.
func (c *Client) OAI(ctx context.Context, identifier string) (*source.Document, error) {
	id := normalizeID(identifier)
	if id == "" {
		return nil, fmt.Errorf("retrieving arXiv OAI record: identifier is required")
	}
	endpoint, err := url.Parse(c.oaiBaseURL())
	if err != nil {
		return nil, fmt.Errorf("retrieving arXiv OAI record: parsing endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("verb", "GetRecord")
	query.Set("identifier", "oai:arXiv.org:"+id)
	query.Set("metadataPrefix", "arXiv")
	endpoint.RawQuery = query.Encode()

	doc, err := c.httpClient().Fetch(ctx, endpoint.String(), "application/xml")
	if err != nil {
		return nil, fmt.Errorf("retrieving arXiv OAI record %q: %w", id, err)
	}
	return doc, nil
}

func (c *Client) apiBaseURL() string {
	if c != nil && strings.TrimSpace(c.APIBaseURL) != "" {
		return c.APIBaseURL
	}
	return DefaultAPIBaseURL
}

func (c *Client) oaiBaseURL() string {
	if c != nil && strings.TrimSpace(c.OAIBaseURL) != "" {
		return c.OAIBaseURL
	}
	return DefaultOAIBaseURL
}

func (c *Client) httpClient() *source.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return source.NewClient()
}

func normalizeID(identifier string) string {
	id := strings.TrimSpace(identifier)
	id = strings.TrimPrefix(id, "arXiv:")
	id = strings.TrimPrefix(id, "https://arxiv.org/abs/")
	id = strings.TrimPrefix(id, "http://arxiv.org/abs/")
	if version := strings.LastIndex(id, "v"); version > 0 {
		if _, err := strconv.Atoi(id[version+1:]); err == nil {
			id = id[:version]
		}
	}
	return strings.TrimSpace(id)
}
