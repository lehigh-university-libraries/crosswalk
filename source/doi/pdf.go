package doi

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

var (
	metaTagPattern  = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	metaAttrPattern = regexp.MustCompile(`(?i)([a-z_:][a-z0-9_:.-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

type cslMedia struct {
	DOI  string `json:"DOI"`
	URL  string `json:"URL"`
	Link []struct {
		URL         string `json:"URL"`
		ContentType string `json:"content-type"`
	} `json:"link"`
}

// PDFURL discovers a publisher-provided PDF URL from CSL metadata or the
// publication landing page. It does not download the PDF.
func (c *Client) PDFURL(ctx context.Context, cslJSON []byte) (string, error) {
	var metadata cslMedia
	if err := json.Unmarshal(cslJSON, &metadata); err != nil {
		return "", fmt.Errorf("discovering DOI PDF: parsing CSL-JSON: %w", err)
	}
	for _, link := range metadata.Link {
		if strings.EqualFold(strings.TrimSpace(link.ContentType), "application/pdf") || strings.Contains(strings.ToLower(link.URL), ".pdf") {
			return absoluteHTTPURL(link.URL, "")
		}
	}

	landingURL := strings.TrimSpace(metadata.URL)
	if landingURL == "" && strings.TrimSpace(metadata.DOI) != "" {
		base := DefaultBaseURL
		if c != nil && strings.TrimSpace(c.BaseURL) != "" {
			base = strings.TrimRight(c.BaseURL, "/")
		}
		var err error
		landingURL, err = url.JoinPath(base, normalize(metadata.DOI))
		if err != nil {
			return "", fmt.Errorf("discovering DOI PDF: constructing landing URL: %w", err)
		}
	}
	if landingURL == "" {
		return "", fmt.Errorf("discovering DOI PDF: CSL metadata has no PDF link or landing URL")
	}
	client := source.NewClient()
	if c != nil && c.HTTP != nil {
		client = c.HTTP
	}
	document, err := client.Fetch(ctx, landingURL, "text/html")
	if err != nil {
		return "", fmt.Errorf("discovering DOI PDF: fetching landing page: %w", err)
	}
	discovered := citationPDFURL(document.Data)
	if discovered == "" {
		return "", fmt.Errorf("discovering DOI PDF: landing page does not declare citation_pdf_url")
	}
	return absoluteHTTPURL(discovered, document.URL)
}

func citationPDFURL(document []byte) string {
	for _, tag := range metaTagPattern.FindAll(document, -1) {
		attributes := make(map[string]string)
		for _, match := range metaAttrPattern.FindAllSubmatch(tag, -1) {
			value := ""
			for _, candidate := range match[2:] {
				if len(candidate) > 0 {
					value = string(candidate)
					break
				}
			}
			attributes[strings.ToLower(string(match[1]))] = html.UnescapeString(value)
		}
		name := strings.ToLower(strings.TrimSpace(attributes["name"]))
		if name == "" {
			name = strings.ToLower(strings.TrimSpace(attributes["property"]))
		}
		if name == "citation_pdf_url" {
			return strings.TrimSpace(attributes["content"])
		}
	}
	return ""
}

func absoluteHTTPURL(rawURL, baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("discovering DOI PDF: invalid URL: %w", err)
	}
	if !parsed.IsAbs() && baseURL != "" {
		base, err := url.Parse(baseURL)
		if err != nil {
			return "", fmt.Errorf("discovering DOI PDF: invalid landing URL: %w", err)
		}
		parsed = base.ResolveReference(parsed)
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("discovering DOI PDF: absolute HTTP(S) URL is required")
	}
	return parsed.String(), nil
}
