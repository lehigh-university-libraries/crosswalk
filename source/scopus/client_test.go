package scopus

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

func TestSearchUsesCredentialHeadersAndCursor(t *testing.T) {
	client := NewClient("api-secret")
	client.BaseURL = "https://example.test/content/search/scopus"
	client.InstitutionToken = "institution-secret"
	client.AccessToken = "access-secret"
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("X-ELS-APIKey"); got != "api-secret" {
			t.Fatalf("X-ELS-APIKey = %q", got)
		}
		if got := request.Header.Get("X-ELS-Insttoken"); got != "institution-secret" {
			t.Fatalf("X-ELS-Insttoken = %q", got)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer access-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		query := request.URL.Query()
		if query.Get("query") != "AF-ID(123)" || query.Get("count") != "25" || query.Get("cursor") != "next-value" || query.Get("view") != ViewComplete {
			t.Fatalf("unexpected query: %s", request.URL.RawQuery)
		}
		for _, secret := range []string{"api-secret", "institution-secret", "access-secret"} {
			if strings.Contains(request.URL.String(), secret) {
				t.Fatalf("credential %q leaked into URL", secret)
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"search-results":{"entry":[]}}`)), Header: make(http.Header), Request: request}, nil
	})
	if _, err := client.Search(context.Background(), SearchOptions{Query: "AF-ID(123)", Cursor: "next-value", Count: 25, View: ViewComplete}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchFieldSelectionOverridesView(t *testing.T) {
	client := NewClient("api-secret")
	client.BaseURL = "https://example.test/scopus"
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if got := query.Get("field"); got != "dc:title,eid" {
			t.Fatalf("field = %q", got)
		}
		if _, exists := query["view"]; exists {
			t.Fatalf("view must be omitted when field is present: %s", request.URL.RawQuery)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header), Request: request}, nil
	})
	if _, err := client.Search(context.Background(), SearchOptions{Query: "TITLE(test)", Fields: []string{"dc:title", "eid", "dc:title"}}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchValidatesBoundsAndPagination(t *testing.T) {
	client := NewClient("api-secret")
	tests := []struct {
		name    string
		options SearchOptions
	}{
		{name: "missing query", options: SearchOptions{}},
		{name: "negative start", options: SearchOptions{Query: "x", Start: -1}},
		{name: "cursor and start", options: SearchOptions{Query: "x", Cursor: "*", Start: 1}},
		{name: "invalid view", options: SearchOptions{Query: "x", View: "FULL"}},
		{name: "standard too large", options: SearchOptions{Query: "x", Count: 201}},
		{name: "complete too large", options: SearchOptions{Query: "x", Count: 26, View: ViewComplete}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := client.Search(context.Background(), test.options); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSearchRejectsCredentialNewlines(t *testing.T) {
	client := NewClient("secret\nleak")
	client.BaseURL = "https://example.test/scopus"
	client.HTTP = doerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("request must not be sent")
		return nil, nil
	})
	if _, err := client.Search(context.Background(), SearchOptions{Query: "x"}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error = %v; want a redacted credential validation error", err)
	}
}
