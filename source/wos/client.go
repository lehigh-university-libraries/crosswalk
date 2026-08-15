// Package wos acquires bounded Web of Science Starter API result pages.
package wos

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
	// DefaultBaseURL is the Web of Science Starter v1 API root.
	DefaultBaseURL  = "https://api.clarivate.com/apis/wos-starter/v1"
	defaultMaxBytes = int64(16 << 20)
)

// SearchOptions describes one explicitly bounded result page.
type SearchOptions struct {
	Query     string
	Database  string
	Page      int
	Limit     int
	SortField string
}

// Client retrieves Web of Science metadata without interpreting it.
type Client struct {
	BaseURL          string
	APIKey           string
	HTTP             source.HTTPDoer
	MaxResponseBytes int64
}

// NewClient returns a client configured for the public Starter API endpoint.
func NewClient(apiKey string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		APIKey:  strings.TrimSpace(apiKey),
	}
}

// Search retrieves one result page. The API key is never placed in the URL or errors.
func (c *Client) Search(ctx context.Context, options SearchOptions) (*source.Document, error) {
	if ctx == nil {
		return nil, fmt.Errorf("searching Web of Science: context is required")
	}
	if strings.TrimSpace(c.apiKey()) == "" {
		return nil, fmt.Errorf("searching Web of Science: API key is required")
	}
	if strings.TrimSpace(options.Query) == "" {
		return nil, fmt.Errorf("searching Web of Science: query is required")
	}
	if options.Page <= 0 {
		options.Page = 1
	}
	if options.Limit <= 0 {
		options.Limit = 50
	}
	if options.Limit > 50 {
		return nil, fmt.Errorf("searching Web of Science: limit must not exceed 50")
	}
	base, err := url.Parse(strings.TrimRight(c.baseURL(), "/") + "/documents")
	if err != nil {
		return nil, fmt.Errorf("searching Web of Science: invalid endpoint: %w", err)
	}
	if !secureEndpoint(base) {
		return nil, fmt.Errorf("searching Web of Science: HTTPS endpoint is required except on loopback")
	}
	query := base.Query()
	query.Set("q", strings.TrimSpace(options.Query))
	query.Set("db", defaultString(strings.TrimSpace(options.Database), "WOS"))
	query.Set("page", strconv.Itoa(options.Page))
	query.Set("limit", strconv.Itoa(options.Limit))
	if options.SortField != "" {
		query.Set("sortField", options.SortField)
	}
	base.RawQuery = query.Encode()

	client := source.NewClient()
	client.HTTP = c.httpClient()
	client.UserAgent = "crosswalk/wos-acquisition"
	client.MaxResponseBytes = c.byteLimit()
	document, err := client.FetchRequest(ctx, source.Request{
		URL: base.String(), Accept: "application/json",
		Auth: source.HeaderAuth{Name: "X-ApiKey", Value: c.apiKey()},
	})
	if err != nil {
		return nil, fmt.Errorf("searching Web of Science: %w", err)
	}
	return document, nil
}

func (c *Client) baseURL() string {
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}

func (c *Client) apiKey() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.APIKey)
}

func (c *Client) httpClient() source.HTTPDoer {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return nil
}

func (c *Client) byteLimit() int64 {
	if c != nil && c.MaxResponseBytes > 0 {
		return c.MaxResponseBytes
	}
	return defaultMaxBytes
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
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
