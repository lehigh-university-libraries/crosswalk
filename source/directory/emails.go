// Package directory acquires bounded public directory pages and extracts
// addresses that can be used as scholarly-search queries.
package directory

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

const defaultMaxEmails = 1000

var emailPattern = regexp.MustCompile(`(?i)\b[A-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[A-Z0-9](?:[A-Z0-9-]{0,61}[A-Z0-9])?(?:\.[A-Z0-9](?:[A-Z0-9-]{0,61}[A-Z0-9])?)+\b`)

// Client fetches a directory page with the shared bounded HTTP client.
type Client struct {
	HTTP      *source.Client
	MaxEmails int
}

// NewClient returns a directory client with bounded response and result limits.
func NewClient() *Client {
	return &Client{HTTP: source.NewClient(), MaxEmails: defaultMaxEmails}
}

// Emails fetches rawURL and returns normalized, unique email addresses.
func (c *Client) Emails(ctx context.Context, rawURL string) ([]string, error) {
	client := source.NewClient()
	if c != nil && c.HTTP != nil {
		client = c.HTTP
	}
	document, err := client.Fetch(ctx, rawURL, "text/html, text/plain;q=0.9")
	if err != nil {
		return nil, fmt.Errorf("fetching directory: %w", err)
	}
	return ExtractEmails(string(document.Data), c.emailLimit())
}

// ExtractEmails returns sorted, case-insensitively unique addresses from text.
// It fails instead of silently truncating a directory whose scope is larger
// than the caller intended.
func ExtractEmails(text string, maxEmails int) ([]string, error) {
	if maxEmails <= 0 {
		maxEmails = defaultMaxEmails
	}
	matches := emailPattern.FindAllString(text, -1)
	seen := make(map[string]string, len(matches))
	for _, match := range matches {
		normalized := strings.ToLower(strings.TrimSpace(match))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		if len(seen) == maxEmails {
			return nil, fmt.Errorf("directory contains more than %d unique email addresses", maxEmails)
		}
		seen[normalized] = normalized
	}
	emails := make([]string, 0, len(seen))
	for _, email := range seen {
		emails = append(emails, email)
	}
	sort.Strings(emails)
	return emails, nil
}

func (c *Client) emailLimit() int {
	if c != nil && c.MaxEmails > 0 {
		return c.MaxEmails
	}
	return defaultMaxEmails
}
