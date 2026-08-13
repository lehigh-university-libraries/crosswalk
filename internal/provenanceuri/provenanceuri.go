// Package provenanceuri normalizes HTTP(S) locators before they are retained
// as metadata provenance.
package provenanceuri

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

const maxURIBytes = 4096

// Options controls how Normalize handles URI references.
type Options struct {
	// BaseURL resolves a relative value. It must be an absolute HTTP(S) URL.
	BaseURL string
	// AllowRelative preserves a relative reference when BaseURL is empty.
	AllowRelative bool
}

// Normalize returns a safe, deterministic provenance URI. It removes user
// information, credential-like query parameters, and fragments while
// preserving ordinary query parameters that identify a public record.
func Normalize(value string, options Options) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > maxURIBytes || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("URI is invalid or exceeds %d bytes", maxURIBytes)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse URI: %w", err)
	}
	if !parsed.IsAbs() && parsed.Host != "" {
		return "", fmt.Errorf("network-path URI is not allowed")
	}

	if !parsed.IsAbs() {
		if strings.TrimSpace(options.BaseURL) != "" {
			base, err := absoluteBase(options.BaseURL)
			if err != nil {
				return "", err
			}
			parsed = base.ResolveReference(parsed)
		} else if !options.AllowRelative {
			return "", fmt.Errorf("URI must be an absolute HTTP(S) URI")
		}
	}
	if parsed.IsAbs() {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", fmt.Errorf("URI must use HTTP or HTTPS")
		}
		if parsed.Host == "" || parsed.Opaque != "" {
			return "", fmt.Errorf("URI must be an absolute HTTP(S) URI")
		}
		parsed.Host = strings.ToLower(parsed.Host)
	}

	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if IsSensitiveQueryKey(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""

	normalized := parsed.String()
	if len(normalized) > maxURIBytes {
		return "", fmt.Errorf("normalized URI exceeds %d bytes", maxURIBytes)
	}
	return normalized, nil
}

func absoluteBase(value string) (*url.URL, error) {
	base, err := Normalize(value, Options{})
	if err != nil {
		return nil, fmt.Errorf("base URL: %w", err)
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	// A base URL describes the installation rather than a request, so its query
	// must not be inherited by a relative record reference.
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed, nil
}

// IsSensitiveQueryKey reports whether a query key commonly carries an
// authentication credential or another secret that must not be persisted.
func IsSensitiveQueryKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("-", "", "_", "", ".", "", "[", "", "]", "").Replace(key)
	switch key {
	case "apikey", "key", "token", "accesstoken", "insttoken", "wskey",
		"password", "authorization", "auth", "session", "sessionid",
		"credential", "credentials", "secret", "clientsecret", "signature",
		"sig", "jwt", "cookie", "code":
		return true
	default:
		return strings.Contains(key, "credential") ||
			strings.Contains(key, "password") ||
			strings.Contains(key, "secret") ||
			strings.Contains(key, "signature") ||
			strings.Contains(key, "authorization") ||
			strings.Contains(key, "token") ||
			strings.Contains(key, "accesskey") ||
			strings.Contains(key, "privatekey") ||
			strings.Contains(key, "keypairid") ||
			strings.HasPrefix(key, "xamz") ||
			strings.Contains(key, "session")
	}
}
