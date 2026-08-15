// Package sherpa performs explicit SHERPA/RoMEO policy lookups for metadata
// enrichment. Policy data is returned with its evidence so callers do not
// mistake a publisher policy page for a reusable-content license.
package sherpa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

const (
	defaultSearchURL   = "https://v2.sherpa.ac.uk/cgi/romeosearch"
	defaultRetrieveURL = "https://v2.sherpa.ac.uk/cgi/retrieve"
	defaultMaxBytes    = int64(4 << 20)
)

var publicationIDPattern = regexp.MustCompile(`^[0-9]+$`)

// Client resolves journals and retrieves publisher policy records.
type Client struct {
	SearchURL        string
	RetrieveURL      string
	HTTP             source.HTTPDoer
	MaxResponseBytes int64
	// AllowPrivate permits an explicitly configured internal SHERPA mirror.
	// It has no effect on the official public defaults.
	AllowPrivate bool
}

// Recommendation is the strongest repository-relevant policy evidence found
// for an ISSN. ReviewRequired remains true unless an explicit, immediate
// license was present in the source policy.
type Recommendation struct {
	ISSN           string   `json:"issn"`
	PublicationID  string   `json:"publication_id"`
	PolicyURI      string   `json:"policy_uri,omitempty"`
	LicenseURI     string   `json:"license_uri,omitempty"`
	ArticleVersion []string `json:"article_version,omitempty"`
	Locations      []string `json:"locations,omitempty"`
	Conditions     []string `json:"conditions,omitempty"`
	EmbargoAmount  int      `json:"embargo_amount,omitempty"`
	EmbargoUnits   string   `json:"embargo_units,omitempty"`
	ReviewRequired bool     `json:"review_required"`
}

type response struct {
	Items []publication `json:"items"`
}

type publication struct {
	PublisherPolicy []policy `json:"publisher_policy"`
}

type policy struct {
	URI         string          `json:"uri"`
	PermittedOA []permittedOpen `json:"permitted_oa"`
}

type permittedOpen struct {
	ArticleVersion []string `json:"article_version"`
	Conditions     []string `json:"conditions"`
	Embargo        struct {
		Amount int    `json:"amount"`
		Units  string `json:"units"`
	} `json:"embargo"`
	License []struct {
		Value   string `json:"license"`
		Version string `json:"version"`
	} `json:"license"`
	Location struct {
		Locations []string `json:"location"`
	} `json:"location"`
}

// Lookup returns policy evidence for one ISSN. The API key is used only for
// the retrieve request and is never included in returned errors.
func (c *Client) Lookup(ctx context.Context, issn, apiKey string) (Recommendation, error) {
	issn = strings.TrimSpace(issn)
	if issn == "" {
		return Recommendation{}, fmt.Errorf("looking up SHERPA policy: ISSN is required")
	}
	if strings.TrimSpace(apiKey) == "" {
		return Recommendation{}, fmt.Errorf("looking up SHERPA policy: API key is required")
	}
	publicationID, err := c.publicationID(ctx, issn)
	if err != nil {
		return Recommendation{}, err
	}
	document, err := c.retrieve(ctx, publicationID, apiKey)
	if err != nil {
		return Recommendation{}, err
	}
	var decoded response
	if err := json.Unmarshal(document, &decoded); err != nil {
		return Recommendation{}, fmt.Errorf("looking up SHERPA policy: parsing response: %w", err)
	}
	recommendation := selectRecommendation(issn, publicationID, decoded)
	if recommendation.PolicyURI == "" && recommendation.LicenseURI == "" {
		return Recommendation{}, fmt.Errorf("looking up SHERPA policy: no policy found for ISSN %q", issn)
	}
	return recommendation, nil
}

func (c *Client) publicationID(ctx context.Context, issn string) (string, error) {
	endpoint, err := c.endpoint(c.searchURL(), "search")
	if err != nil {
		return "", fmt.Errorf("looking up SHERPA publication: %w", err)
	}
	query := endpoint.Query()
	query.Set("publication_title-auto", issn)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", fmt.Errorf("looking up SHERPA publication: creating request: %w", err)
	}
	request.Header.Set("Accept", "text/html")
	request.Header.Set("User-Agent", "crosswalk/sherpa-enrichment")
	result, err := c.httpClient().Do(request)
	if err != nil {
		return "", fmt.Errorf("looking up SHERPA publication: request failed: %w", source.RedactRequestError(err, request))
	}
	if result == nil || result.Body == nil {
		return "", fmt.Errorf("looking up SHERPA publication: empty response")
	}
	defer result.Body.Close()
	if result.Request != nil && result.Request.URL != nil && !sameOrigin(endpoint, result.Request.URL) {
		return "", fmt.Errorf("looking up SHERPA publication: response came from an unsafe redirect target")
	}
	if result.StatusCode < 300 || result.StatusCode >= 400 {
		return "", fmt.Errorf("looking up SHERPA publication: expected redirect, got HTTP %d", result.StatusCode)
	}
	location := strings.TrimSpace(result.Header.Get("Location"))
	parsed, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("looking up SHERPA publication: invalid redirect")
	}
	redirected := endpoint.ResolveReference(parsed)
	if !sameOrigin(endpoint, redirected) || redirected.User != nil {
		return "", fmt.Errorf("looking up SHERPA publication: unsafe redirect")
	}
	id := path.Base(strings.TrimRight(redirected.Path, "/"))
	if !publicationIDPattern.MatchString(id) {
		return "", fmt.Errorf("looking up SHERPA publication: redirect did not contain a publication ID")
	}
	return id, nil
}

func (c *Client) retrieve(ctx context.Context, publicationID, apiKey string) ([]byte, error) {
	endpoint, err := c.endpoint(c.retrieveURL(), "retrieve")
	if err != nil {
		return nil, fmt.Errorf("looking up SHERPA policy: %w", err)
	}
	filter, err := json.Marshal([][]string{{"id", "equals", publicationID}})
	if err != nil {
		return nil, fmt.Errorf("looking up SHERPA policy: encoding filter: %w", err)
	}
	query := endpoint.Query()
	query.Set("item-type", "publication")
	query.Set("format", "Json")
	query.Set("limit", "10")
	query.Set("offset", "0")
	query.Set("order", "-id")
	query.Set("filter", string(filter))
	query.Set("api-key", apiKey)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("looking up SHERPA policy: creating request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "crosswalk/sherpa-enrichment")
	result, err := c.httpClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("looking up SHERPA policy: request failed: %w", source.RedactRequestError(err, request))
	}
	if result == nil || result.Body == nil {
		return nil, fmt.Errorf("looking up SHERPA policy: empty response")
	}
	defer result.Body.Close()
	if result.Request != nil && result.Request.URL != nil && !sameOrigin(endpoint, result.Request.URL) {
		return nil, fmt.Errorf("looking up SHERPA policy: response came from an unsafe redirect target")
	}
	if result.StatusCode < http.StatusOK || result.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("looking up SHERPA policy: unexpected HTTP status %d", result.StatusCode)
	}
	limit := c.responseLimit()
	data, err := io.ReadAll(io.LimitReader(result.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("looking up SHERPA policy: reading response: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("looking up SHERPA policy: response exceeds %d bytes", limit)
	}
	return data, nil
}

func selectRecommendation(issn, publicationID string, decoded response) Recommendation {
	best := Recommendation{ISSN: issn, PublicationID: publicationID, ReviewRequired: true}
	bestScore := -1
	for _, publication := range decoded.Items {
		for _, policy := range publication.PublisherPolicy {
			if len(policy.PermittedOA) == 0 && best.PolicyURI == "" {
				best.PolicyURI = strings.TrimSpace(policy.URI)
			}
			for _, option := range policy.PermittedOA {
				if !containsFold(option.ArticleVersion, "published") || !repositoryLocation(option.Location.Locations) {
					continue
				}
				licenseURI := ""
				for _, license := range option.License {
					if candidate := creativeCommonsURI(license.Value, license.Version); candidate != "" {
						licenseURI = candidate
						break
					}
				}
				score := 1
				if option.Embargo.Amount == 0 {
					score += 2
				}
				if licenseURI != "" {
					score += 4
				}
				if score <= bestScore {
					continue
				}
				bestScore = score
				best.PolicyURI = strings.TrimSpace(policy.URI)
				best.LicenseURI = licenseURI
				best.ArticleVersion = sortedCopy(option.ArticleVersion)
				best.Locations = sortedCopy(option.Location.Locations)
				best.Conditions = sortedCopy(option.Conditions)
				best.EmbargoAmount = option.Embargo.Amount
				best.EmbargoUnits = strings.TrimSpace(option.Embargo.Units)
				best.ReviewRequired = licenseURI == "" || option.Embargo.Amount != 0
			}
		}
	}
	return best
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), wanted) {
			return true
		}
	}
	return false
}

func repositoryLocation(values []string) bool {
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "any_website", "non_commercial_website", "institutional_repository", "non_commercial_repository":
			return true
		}
	}
	return false
}

func creativeCommonsURI(value, version string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "cc_") {
		return ""
	}
	name := strings.ReplaceAll(strings.TrimPrefix(value, "cc_"), "_", "-")
	if name == "public-domain" {
		return "https://creativecommons.org/publicdomain/"
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = "4.0"
	}
	return "https://creativecommons.org/licenses/" + name + "/" + version + "/"
}

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func (c *Client) searchURL() string {
	if c != nil && strings.TrimSpace(c.SearchURL) != "" {
		return c.SearchURL
	}
	return defaultSearchURL
}

func (c *Client) retrieveURL() string {
	if c != nil && strings.TrimSpace(c.RetrieveURL) != "" {
		return c.RetrieveURL
	}
	return defaultRetrieveURL
}

func (c *Client) responseLimit() int64 {
	if c != nil && c.MaxResponseBytes > 0 {
		return c.MaxResponseBytes
	}
	return defaultMaxBytes
}

func (c *Client) endpoint(rawURL, name string) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || endpoint.Host == "" || endpoint.Opaque != "" {
		return nil, fmt.Errorf("invalid %s endpoint", name)
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("%s endpoint must not contain credentials, query, or fragment", name)
	}
	if endpoint.Scheme == "https" {
		return endpoint, nil
	}
	if endpoint.Scheme == "http" && loopbackHost(endpoint.Hostname()) {
		return endpoint, nil
	}
	return nil, fmt.Errorf("%s endpoint must use HTTPS except on explicitly allowed loopback", name)
}

func loopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func sameOrigin(left, right *url.URL) bool {
	return left != nil && right != nil && strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func (c *Client) httpClient() source.HTTPDoer {
	allowPrivate := c != nil && c.AllowPrivate
	allowLoopback := c != nil && (configuredLoopback(c.searchURL()) || configuredLoopback(c.retrieveURL()))
	if c != nil && c.HTTP != nil {
		if provided, ok := c.HTTP.(*http.Client); ok {
			client := source.CloneProtectedHTTPClient(provided, allowPrivate, allowLoopback, source.RedirectSameOrigin)
			client.CheckRedirect = func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}
			return client
		}
		return c.HTTP
	}
	client := source.NewProtectedHTTPClient(allowPrivate, allowLoopback, source.RedirectSameOrigin)
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

func configuredLoopback(rawURL string) bool {
	endpoint, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && endpoint.Scheme == "http" && loopbackHost(endpoint.Hostname())
}
