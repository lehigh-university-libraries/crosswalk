// Package source provides bounded, context-aware acquisition primitives for
// metadata sources that sit outside Crosswalk's deterministic format layer.
package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/internal/provenanceuri"
)

const (
	defaultTimeout          = 30 * time.Second
	defaultMaxResponseBytes = int64(16 << 20)
)

// nonPublicMetadataPrefixes supplements netip.Addr.IsGlobalUnicast, which
// intentionally reports some special-purpose ranges (including CGNAT and
// documentation networks) as global unicast. Metadata URLs are untrusted, so
// the default policy accepts only ordinary public-routable destinations.
var nonPublicMetadataPrefixes = []netip.Prefix{
	// IPv4 special-purpose and reserved ranges.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	// IPv6 special-purpose, translation, documentation, and local ranges.
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// HTTPDoer is implemented by http.Client and test transports. Client and
// Downloader-style APIs in this module protect supplied *http.Client values,
// but use other implementations verbatim. A non-*http.Client implementation
// must therefore enforce any required destination and redirect policy itself.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Document is one acquired metadata representation and its provenance.
type Document struct {
	URL         string
	ContentType string
	Data        []byte
}

// RedirectPolicy controls which redirect targets an acquisition may follow.
type RedirectPolicy string

const (
	// RedirectSameOrigin is the default and keeps credentials and repository
	// requests on the original scheme and authority.
	RedirectSameOrigin RedirectPolicy = "same-origin"
	// RedirectHTTPS permits cross-origin HTTPS redirects. It is intended for
	// unauthenticated resolvers such as DOI content negotiation.
	RedirectHTTPS RedirectPolicy = "https"
)

// Authenticator applies request authentication without putting credentials in
// a URL. Implementations must not include secret values in returned errors.
type Authenticator interface {
	Apply(*http.Request) error
}

// HeaderAuth applies one caller-supplied credential header.
type HeaderAuth struct {
	Name  string
	Value string
}

// Apply implements Authenticator.
func (auth HeaderAuth) Apply(request *http.Request) error {
	name := strings.TrimSpace(auth.Name)
	if name == "" || strings.ContainsAny(name, "\r\n") {
		return fmt.Errorf("authentication header name is invalid")
	}
	if strings.TrimSpace(auth.Value) == "" || strings.ContainsAny(auth.Value, "\r\n") {
		return fmt.Errorf("authentication credential is empty or invalid")
	}
	request.Header.Set(name, auth.Value)
	return nil
}

// BearerAuth applies an OAuth bearer token.
type BearerAuth string

// Apply implements Authenticator.
func (auth BearerAuth) Apply(request *http.Request) error {
	value := strings.TrimSpace(string(auth))
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("bearer credential is empty or invalid")
	}
	request.Header.Set("Authorization", "Bearer "+value)
	return nil
}

// Request describes a bounded read-only acquisition.
type Request struct {
	URL            string
	Accept         string
	Headers        http.Header
	Auth           Authenticator
	RedirectPolicy RedirectPolicy
}

// HTTPStatusError reports a non-success response without retaining a request
// URL or response body that might contain credentials. RetryAfter is populated
// from a valid Retry-After header.
type HTTPStatusError struct {
	Status     int
	RetryAfter time.Duration
}

// Error implements error.
func (err *HTTPStatusError) Error() string {
	if err == nil {
		return "unexpected HTTP status"
	}
	return fmt.Sprintf("unexpected HTTP status %d", err.Status)
}

// Client retrieves bounded metadata responses with request cancellation.
type Client struct {
	// HTTP optionally supplies client-level settings. Supplied *http.Client
	// values retain positive timeouts, cookie jars, and stricter redirect
	// decisions, but their transports are replaced so DNS pinning, destination
	// filtering, and the no-proxy policy cannot be bypassed. Other HTTPDoer
	// implementations are used verbatim and are responsible for equivalent
	// protections; that escape hatch is primarily intended for tests and
	// in-process adapters.
	HTTP             HTTPDoer
	UserAgent        string
	MaxResponseBytes int64
	AllowPrivate     bool
	// AllowLoopback permits loopback destinations without permitting other
	// private networks. It is intended for explicitly configured local test and
	// development endpoints.
	AllowLoopback  bool
	RedirectPolicy RedirectPolicy
}

// NewClient returns an acquisition client with safe timeout and response-size defaults.
func NewClient() *Client {
	return &Client{
		UserAgent:        "crosswalk/metadata-acquisition",
		MaxResponseBytes: defaultMaxResponseBytes,
	}
}

// Fetch retrieves rawURL while requiring a successful HTTP response.
func (c *Client) Fetch(ctx context.Context, rawURL, accept string) (*Document, error) {
	return c.FetchRequest(ctx, Request{URL: rawURL, Accept: accept})
}

// FetchRequest retrieves a bounded response using explicit headers,
// authentication, and redirect policy.
func (c *Client) FetchRequest(ctx context.Context, options Request) (*Document, error) {
	if ctx == nil {
		return nil, fmt.Errorf("fetching metadata: context is required")
	}
	parsed, err := url.Parse(options.URL)
	if err != nil {
		return nil, fmt.Errorf("parsing source URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("parsing source URL: unsupported scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("parsing source URL: host is required")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("parsing source URL: user information is not allowed")
	}
	policy := c.redirectPolicy(options.RedirectPolicy)
	if policy != RedirectSameOrigin && policy != RedirectHTTPS {
		return nil, fmt.Errorf("fetching metadata: unsupported redirect policy %q", policy)
	}
	if (options.Auth != nil || hasCredentialHeader(options.Headers)) && policy != RedirectSameOrigin {
		return nil, fmt.Errorf("fetching metadata: authenticated requests require same-origin redirects")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating metadata request: %w", err)
	}
	for name, values := range options.Headers {
		if !validCallerHeader(name) {
			return nil, fmt.Errorf("creating metadata request: header %q is not allowed", name)
		}
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n") {
				return nil, fmt.Errorf("creating metadata request: header %q has an invalid value", name)
			}
			req.Header.Add(name, value)
		}
	}
	if strings.TrimSpace(options.Accept) != "" {
		req.Header.Set("Accept", options.Accept)
	}
	if userAgent := strings.TrimSpace(c.userAgent()); userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if options.Auth != nil {
		if err := options.Auth.Apply(req); err != nil {
			return nil, fmt.Errorf("authenticating metadata request: %w", err)
		}
	}

	resp, err := c.httpClient(policy).Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting metadata: %w", RedactRequestError(err, req))
	}
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("requesting metadata: HTTP client returned an empty response")
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil && !safeMetadataRedirect(parsed, resp.Request.URL, policy) {
		return nil, fmt.Errorf("requesting metadata: response came from an unsafe redirect target")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("requesting metadata: %w", &HTTPStatusError{
			Status: resp.StatusCode, RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		})
	}

	limit := c.responseLimit()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading metadata response: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("reading metadata response: response exceeds %d bytes", limit)
	}

	contentType := resp.Header.Get("Content-Type")
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = mediaType
	}
	finalURL := sanitizedURL(parsed)
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = sanitizedURL(resp.Request.URL)
	}
	return &Document{
		URL:         finalURL,
		ContentType: contentType,
		Data:        data,
	}, nil
}

func (c *Client) httpClient(policy RedirectPolicy) HTTPDoer {
	allowPrivate := c != nil && c.AllowPrivate
	allowLoopback := c != nil && c.AllowLoopback
	if c != nil && c.HTTP != nil {
		if provided, ok := c.HTTP.(*http.Client); ok {
			return CloneProtectedHTTPClient(provided, allowPrivate, allowLoopback, policy)
		}
		return c.HTTP
	}
	return defaultHTTPClient(allowPrivate, allowLoopback, policy)
}

// NewProtectedHTTPClient returns an HTTP client that resolves and dials each
// destination directly, without environment proxy inheritance. Its dialer
// pins each connection to a permitted DNS answer, applies the requested
// private-network policy to redirects as well as the original request, and
// supplies finite connection, header, and whole-request bounds.
//
// Callers may make the redirect policy more restrictive, such as replacing
// CheckRedirect with http.ErrUseLastResponse, without weakening the transport.
func NewProtectedHTTPClient(allowPrivate, allowLoopback bool, policy RedirectPolicy) *http.Client {
	return defaultHTTPClient(allowPrivate, allowLoopback, policy)
}

// CloneProtectedHTTPClient returns a protected client derived from provided.
// Positive timeouts, cookie jars, and stricter redirect decisions are retained.
// The transport is deliberately replaced: custom transports, including proxy
// settings, cannot preserve local DNS pinning and destination validation.
// A nil client returns the same safe defaults as NewProtectedHTTPClient.
func CloneProtectedHTTPClient(provided *http.Client, allowPrivate, allowLoopback bool, policy RedirectPolicy) *http.Client {
	client := defaultHTTPClient(allowPrivate, allowLoopback, policy)
	if provided == nil {
		return client
	}
	if provided.Timeout > 0 {
		client.Timeout = provided.Timeout
	}
	client.Jar = provided.Jar
	client.CheckRedirect = protectedRedirectPolicy(policy, provided.CheckRedirect)
	return client
}

func defaultHTTPClient(allowPrivate, allowLoopback bool, policy RedirectPolicy) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           100,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ExpectContinueTimeout:  time.Second,
		ResponseHeaderTimeout:  defaultTimeout,
		MaxResponseHeaderBytes: 1 << 20,
	}
	// Proxies resolve and fetch the target themselves, bypassing the address
	// validation in DialContext, so protected clients never use one.
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("validating metadata address: %w", err)
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolving metadata host: %w", err)
		}
		var lastErr error
		for _, address := range addresses {
			if !permittedMetadataIP(address.IP, allowPrivate, allowLoopback) {
				continue
			}
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("metadata host has no permitted address")
	}
	return &http.Client{
		Timeout: defaultTimeout, Transport: transport,
		CheckRedirect: protectedRedirectPolicy(policy, nil),
	}
}

func protectedRedirectPolicy(policy RedirectPolicy, prior func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		var origin *url.URL
		if len(via) > 0 && via[0] != nil && via[0].URL != nil {
			copy := *via[0].URL
			origin = &copy
		}
		if prior != nil {
			if err := prior(request, via); err != nil {
				return err
			}
		}
		// Run the mandatory check after the caller hook so even a hook that
		// mutates the redirect request cannot weaken the built-in policy. The
		// original origin was copied first for the same reason.
		if len(via) > 0 && (request == nil || !safeMetadataRedirect(origin, request.URL, policy)) {
			return fmt.Errorf("refusing unsafe metadata redirect")
		}
		return nil
	}
}

func publicMetadataIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicMetadataPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func permittedMetadataIP(ip net.IP, allowPrivate, allowLoopback bool) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if allowPrivate {
		// Explicitly configured repository clients may need RFC1918, CGNAT,
		// loopback, or link-local destinations, but never unspecified or
		// multicast addresses.
		return address.IsGlobalUnicast() || address.IsLoopback() || address.IsLinkLocalUnicast()
	}
	return (allowLoopback && address.IsLoopback()) || publicMetadataIP(ip)
}

func safeMetadataRedirect(origin, target *url.URL, policy RedirectPolicy) bool {
	if origin == nil || target == nil || (target.Scheme != "http" && target.Scheme != "https") {
		return false
	}
	switch policy {
	case RedirectHTTPS:
		return target.Scheme == "https"
	case RedirectSameOrigin:
		return strings.EqualFold(origin.Scheme, target.Scheme) && strings.EqualFold(origin.Host, target.Host)
	default:
		return false
	}
}

func (c *Client) redirectPolicy(requestPolicy RedirectPolicy) RedirectPolicy {
	policy := requestPolicy
	if policy == "" && c != nil {
		policy = c.RedirectPolicy
	}
	if policy == "" {
		return RedirectSameOrigin
	}
	return policy
}

func validCallerHeader(name string) bool {
	name = http.CanonicalHeaderKey(strings.TrimSpace(name))
	if name == "" || strings.ContainsAny(name, "\r\n") {
		return false
	}
	switch name {
	case "Host", "Content-Length", "Transfer-Encoding", "Connection", "Proxy-Authorization":
		return false
	default:
		return true
	}
}

func sanitizedURL(value *url.URL) string {
	if value == nil {
		return ""
	}
	normalized, err := provenanceuri.Normalize(value.String(), provenanceuri.Options{})
	if err != nil {
		return ""
	}
	return normalized
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

// RedactRequestError removes credentials from a transport error associated
// with request. Source adapters that put unavoidable credentials in query
// parameters should apply it before returning an HTTP client error.
func RedactRequestError(err error, request *http.Request) error {
	if err == nil {
		return nil
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		copy := *urlError
		if request != nil && request.URL != nil {
			copy.URL = sanitizedURL(request.URL)
		} else if parsed, parseErr := url.Parse(copy.URL); parseErr == nil {
			copy.URL = sanitizedURL(parsed)
		} else {
			copy.URL = "[redacted]"
		}
		if urlError.Err != nil {
			copy.Err = errors.New(redactRequestText(urlError.Err.Error(), request))
		}
		return &copy
	}
	message := redactRequestText(err.Error(), request)
	if message != err.Error() {
		return errors.New(message)
	}
	return err
}

func redactRequestText(message string, request *http.Request) string {
	if request == nil {
		return message
	}
	for _, values := range request.Header {
		for _, value := range values {
			if value != "" {
				message = strings.ReplaceAll(message, value, "[redacted]")
			}
		}
	}
	if request.URL != nil {
		for key, values := range request.URL.Query() {
			if !provenanceuri.IsSensitiveQueryKey(key) {
				continue
			}
			for _, value := range values {
				if value != "" {
					message = strings.ReplaceAll(message, value, "[redacted]")
				}
			}
		}
	}
	return message
}

func hasCredentialHeader(header http.Header) bool {
	for name := range header {
		normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(name))
		if normalized == "authorization" || normalized == "cookie" || strings.Contains(normalized, "apikey") || strings.Contains(normalized, "token") {
			return true
		}
	}
	return false
}

func (c *Client) responseLimit() int64 {
	if c != nil && c.MaxResponseBytes > 0 {
		return c.MaxResponseBytes
	}
	return defaultMaxResponseBytes
}

func (c *Client) userAgent() string {
	if c != nil && c.UserAgent != "" {
		return c.UserAgent
	}
	return "crosswalk/metadata-acquisition"
}
