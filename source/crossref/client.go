// Package crossref acquires bounded Crossref REST API result pages.
package crossref

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

const (
	// DefaultBaseURL is the Crossref REST API works endpoint.
	DefaultBaseURL = "https://api.crossref.org/works"
	maxRows        = 1000
)

// SearchOptions describes one explicitly bounded Crossref result page.
type SearchOptions struct {
	Query       string
	QueryTitle  string
	QueryAuthor string
	Filter      []string
	Rows        int
	Offset      int
	Sort        string
	Order       string
	Mailto      string
}

// Client retrieves Crossref REST metadata without interpreting it.
type Client struct {
	BaseURL string
	HTTP    *source.Client
}

// NewClient returns a Crossref REST client using shared acquisition limits.
func NewClient() *Client { return &Client{BaseURL: DefaultBaseURL, HTTP: source.NewClient()} }

// Search retrieves one result page. Pagination and stopping policy remain with the caller.
func (c *Client) Search(ctx context.Context, options SearchOptions) (*source.Document, error) {
	if strings.TrimSpace(options.Query) == "" && strings.TrimSpace(options.QueryTitle) == "" && strings.TrimSpace(options.QueryAuthor) == "" {
		return nil, fmt.Errorf("searching Crossref: at least one query is required")
	}
	if options.Offset < 0 {
		return nil, fmt.Errorf("searching Crossref: offset must not be negative")
	}
	if options.Rows <= 0 {
		options.Rows = 100
	}
	if options.Rows > maxRows {
		return nil, fmt.Errorf("searching Crossref: rows must not exceed %d", maxRows)
	}
	endpoint, err := url.Parse(c.baseURL())
	if err != nil {
		return nil, fmt.Errorf("searching Crossref: invalid endpoint: %w", err)
	}
	query := endpoint.Query()
	setIfPresent(query, "query.bibliographic", options.Query)
	setIfPresent(query, "query.title", options.QueryTitle)
	setIfPresent(query, "query.author", options.QueryAuthor)
	if filters := compact(options.Filter); len(filters) > 0 {
		query.Set("filter", strings.Join(filters, ","))
	}
	query.Set("rows", strconv.Itoa(options.Rows))
	query.Set("offset", strconv.Itoa(options.Offset))
	setIfPresent(query, "sort", options.Sort)
	setIfPresent(query, "order", options.Order)
	setIfPresent(query, "mailto", options.Mailto)
	endpoint.RawQuery = query.Encode()
	document, err := c.httpClient().Fetch(ctx, endpoint.String(), "application/json")
	if err != nil {
		return nil, fmt.Errorf("searching Crossref: %w", err)
	}
	return document, nil
}

func (c *Client) baseURL() string {
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}

func (c *Client) httpClient() *source.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return source.NewClient()
}

func setIfPresent(query url.Values, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		query.Set(key, value)
	}
}

func compact(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
