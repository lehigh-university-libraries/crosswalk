// Package scopus acquires bounded Scopus Search API result pages.
package scopus

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

const (
	// DefaultBaseURL is the Scopus Search API endpoint.
	DefaultBaseURL = "https://api.elsevier.com/content/search/scopus"
	// ViewStandard is the standard Scopus search representation.
	ViewStandard = "STANDARD"
	// ViewComplete is the expanded Scopus search representation.
	ViewComplete = "COMPLETE"

	defaultMaxBytes = int64(32 << 20)
)

// SearchOptions describes one explicitly bounded Scopus result page.
// Cursor and Start are mutually exclusive. A cursor value of "*" starts
// cursor pagination; subsequent requests should use the response's next cursor.
type SearchOptions struct {
	Query  string
	Cursor string
	Start  int
	Count  int
	View   string
	Sort   string
	Date   string
	Fields []string
}

// Client retrieves Scopus search metadata without interpreting it.
type Client struct {
	BaseURL          string
	APIKey           string
	InstitutionToken string
	AccessToken      string
	HTTP             source.HTTPDoer
	MaxResponseBytes int64
}

// NewClient returns a client configured for the Scopus Search API.
func NewClient(apiKey string) *Client {
	return &Client{BaseURL: DefaultBaseURL, APIKey: strings.TrimSpace(apiKey)}
}

// Search retrieves one result page. Pagination and stopping policy remain
// with the caller.
func (c *Client) Search(ctx context.Context, options SearchOptions) (*source.Document, error) {
	if ctx == nil {
		return nil, fmt.Errorf("searching Scopus: context is required")
	}
	if c == nil || strings.TrimSpace(c.APIKey) == "" {
		return nil, fmt.Errorf("searching Scopus: API key is required")
	}
	if strings.TrimSpace(options.Query) == "" {
		return nil, fmt.Errorf("searching Scopus: query is required")
	}
	if options.Start < 0 {
		return nil, fmt.Errorf("searching Scopus: start must not be negative")
	}
	if strings.TrimSpace(options.Cursor) != "" && options.Start != 0 {
		return nil, fmt.Errorf("searching Scopus: cursor and start are mutually exclusive")
	}
	view := strings.ToUpper(strings.TrimSpace(options.View))
	if view == "" {
		view = ViewStandard
	}
	if view != ViewStandard && view != ViewComplete {
		return nil, fmt.Errorf("searching Scopus: view must be STANDARD or COMPLETE")
	}
	if options.Count <= 0 {
		options.Count = 25
	}
	maxCount := 200
	if view == ViewComplete {
		maxCount = 25
	}
	if options.Count > maxCount {
		return nil, fmt.Errorf("searching Scopus: count must not exceed %d for the %s view", maxCount, view)
	}

	endpoint, err := url.Parse(c.baseURL())
	if err != nil {
		return nil, fmt.Errorf("searching Scopus: invalid endpoint: %w", err)
	}
	if !secureEndpoint(endpoint) {
		return nil, fmt.Errorf("searching Scopus: HTTPS endpoint is required except on loopback")
	}
	query := endpoint.Query()
	query.Set("query", strings.TrimSpace(options.Query))
	query.Set("count", strconv.Itoa(options.Count))
	if cursor := strings.TrimSpace(options.Cursor); cursor != "" {
		query.Set("cursor", cursor)
	} else if options.Start > 0 {
		query.Set("start", strconv.Itoa(options.Start))
	}
	if fields := compact(options.Fields); len(fields) > 0 {
		query.Set("field", strings.Join(fields, ","))
	} else {
		query.Set("view", view)
	}
	setIfPresent(query, "sort", options.Sort)
	setIfPresent(query, "date", options.Date)
	endpoint.RawQuery = query.Encode()

	client := source.NewClient()
	client.HTTP = c.HTTP
	client.UserAgent = "crosswalk/scopus-acquisition"
	client.MaxResponseBytes = c.byteLimit()
	document, err := client.FetchRequest(ctx, source.Request{
		URL:    endpoint.String(),
		Accept: "application/json",
		Auth: scopusAuth{
			apiKey:           c.APIKey,
			institutionToken: c.InstitutionToken,
			accessToken:      c.AccessToken,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("searching Scopus: %w", err)
	}
	return document, nil
}

type scopusAuth struct {
	apiKey           string
	institutionToken string
	accessToken      string
}

func (auth scopusAuth) Apply(request *http.Request) error {
	apiKey := strings.TrimSpace(auth.apiKey)
	if apiKey == "" || strings.ContainsAny(apiKey, "\r\n") {
		return fmt.Errorf("Scopus API key is empty or invalid")
	}
	request.Header.Set("X-ELS-APIKey", apiKey)
	if token := strings.TrimSpace(auth.institutionToken); token != "" {
		if strings.ContainsAny(token, "\r\n") {
			return fmt.Errorf("Scopus institution credential is invalid")
		}
		request.Header.Set("X-ELS-Insttoken", token)
	}
	if token := strings.TrimSpace(auth.accessToken); token != "" {
		if strings.ContainsAny(token, "\r\n") {
			return fmt.Errorf("Scopus access credential is invalid")
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return nil
}

func (c *Client) baseURL() string {
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
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

func compact(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
