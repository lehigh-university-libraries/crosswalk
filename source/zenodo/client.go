// Package zenodo acquires bounded Zenodo published-record representations.
package zenodo

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

const (
	// DefaultBaseURL is the Zenodo published-records API collection.
	DefaultBaseURL = "https://zenodo.org/api/records"

	defaultMaxBytes          = int64(32 << 20)
	maxAnonymousPageSize     = 25
	maxAuthenticatedPageSize = 100
)

// SearchOptions describes one explicitly bounded Zenodo records page.
type SearchOptions struct {
	Query       string
	Page        int
	Size        int
	Sort        string
	AllVersions bool
}

// Client retrieves published Zenodo metadata without interpreting it.
// AccessToken is optional for public records and is sent only in a header.
type Client struct {
	BaseURL          string
	AccessToken      string
	HTTP             source.HTTPDoer
	MaxResponseBytes int64
}

// NewClient returns a client configured for Zenodo's published records API.
func NewClient() *Client { return &Client{BaseURL: DefaultBaseURL} }

// Search retrieves one result page. Pagination and stopping policy remain
// with the caller.
func (c *Client) Search(ctx context.Context, options SearchOptions) (*source.Document, error) {
	if ctx == nil {
		return nil, fmt.Errorf("searching Zenodo: context is required")
	}
	if options.Page <= 0 {
		options.Page = 1
	}
	if options.Size <= 0 {
		options.Size = 25
	}
	maxSize := maxAnonymousPageSize
	credentialMode := "unauthenticated requests"
	if strings.TrimSpace(c.accessToken()) != "" {
		maxSize = maxAuthenticatedPageSize
		credentialMode = "authenticated requests"
	}
	if options.Size > maxSize {
		return nil, fmt.Errorf("searching Zenodo: size must not exceed %d for %s", maxSize, credentialMode)
	}
	endpoint, err := c.endpoint()
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	setIfPresent(query, "q", options.Query)
	setIfPresent(query, "sort", options.Sort)
	query.Set("page", strconv.Itoa(options.Page))
	query.Set("size", strconv.Itoa(options.Size))
	if options.AllVersions {
		query.Set("all_versions", "true")
	}
	endpoint.RawQuery = query.Encode()
	return c.fetch(ctx, endpoint.String(), "searching Zenodo")
}

// Record retrieves one published Zenodo record by numeric record ID.
func (c *Client) Record(ctx context.Context, recordID string) (*source.Document, error) {
	if ctx == nil {
		return nil, fmt.Errorf("retrieving Zenodo record: context is required")
	}
	recordID = strings.TrimSpace(recordID)
	if recordID == "" {
		return nil, fmt.Errorf("retrieving Zenodo record: record ID is required")
	}
	for _, character := range recordID {
		if character < '0' || character > '9' {
			return nil, fmt.Errorf("retrieving Zenodo record: record ID must be numeric")
		}
	}
	endpoint, err := c.endpoint()
	if err != nil {
		return nil, err
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/" + recordID
	return c.fetch(ctx, endpoint.String(), "retrieving Zenodo record")
}

func (c *Client) fetch(ctx context.Context, rawURL, operation string) (*source.Document, error) {
	client := source.NewClient()
	client.HTTP = c.httpClient()
	client.UserAgent = "crosswalk/zenodo-acquisition"
	client.MaxResponseBytes = c.byteLimit()
	request := source.Request{URL: rawURL, Accept: "application/json"}
	if token := strings.TrimSpace(c.accessToken()); token != "" {
		request.Auth = source.BearerAuth(token)
	}
	document, err := client.FetchRequest(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return document, nil
}

func (c *Client) endpoint() (*url.URL, error) {
	base := DefaultBaseURL
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		base = c.BaseURL
	}
	endpoint, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("using Zenodo: invalid endpoint: %w", err)
	}
	if !secureEndpoint(endpoint) {
		return nil, fmt.Errorf("using Zenodo: HTTPS endpoint is required except on loopback")
	}
	return endpoint, nil
}

func (c *Client) accessToken() string {
	if c == nil {
		return ""
	}
	return c.AccessToken
}

func (c *Client) httpClient() source.HTTPDoer {
	if c == nil {
		return nil
	}
	return c.HTTP
}

func (c *Client) byteLimit() int64 {
	if c != nil && c.MaxResponseBytes > 0 {
		return c.MaxResponseBytes
	}
	return defaultMaxBytes
}

func secureEndpoint(endpoint *url.URL) bool {
	if endpoint == nil || endpoint.Hostname() == "" {
		return false
	}
	if endpoint.Scheme == "https" {
		return true
	}
	if endpoint.Scheme != "http" {
		return false
	}
	if strings.EqualFold(endpoint.Hostname(), "localhost") {
		return true
	}
	return net.ParseIP(endpoint.Hostname()).IsLoopback()
}

func setIfPresent(values url.Values, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		values.Set(key, value)
	}
}
