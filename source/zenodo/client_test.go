package zenodo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

func TestSearchBuildsBoundedAuthenticatedPage(t *testing.T) {
	client := NewClient()
	client.BaseURL = "https://example.test/api/records"
	client.AccessToken = "access-secret"
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("q") != `metadata.creators.name:"Smith"` || query.Get("page") != "2" || query.Get("size") != "50" || query.Get("sort") != "mostrecent" || query.Get("all_versions") != "true" {
			t.Fatalf("unexpected query: %s", request.URL.RawQuery)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer access-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		if strings.Contains(request.URL.String(), "access-secret") {
			t.Fatal("access token leaked into URL")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"hits":{"hits":[]}}`)), Header: make(http.Header), Request: request}, nil
	})
	_, err := client.Search(context.Background(), SearchOptions{
		Query: `metadata.creators.name:"Smith"`, Page: 2, Size: 50, Sort: "mostrecent", AllVersions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRecordUsesNumericPath(t *testing.T) {
	client := NewClient()
	client.BaseURL = "https://example.test/api/records/"
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/records/12345" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":12345}`)), Header: make(http.Header), Request: request}, nil
	})
	if _, err := client.Record(context.Background(), "12345"); err != nil {
		t.Fatal(err)
	}
}

func TestSearchAndRecordValidateBounds(t *testing.T) {
	client := NewClient()
	if _, err := client.Search(context.Background(), SearchOptions{Size: 26}); err == nil {
		t.Fatal("expected anonymous page size error")
	}
	client.AccessToken = "access-secret"
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"hits":{"hits":[]}}`)), Header: make(http.Header), Request: request}, nil
	})
	if _, err := client.Search(context.Background(), SearchOptions{Size: 100}); err != nil {
		t.Fatalf("authenticated size 100: %v", err)
	}
	if _, err := client.Search(context.Background(), SearchOptions{Size: 101}); err == nil {
		t.Fatal("expected authenticated page size error")
	}
	for _, identifier := range []string{"", "abc", "12/34", "-1"} {
		if _, err := client.Record(context.Background(), identifier); err == nil {
			t.Fatalf("Record(%q) expected validation error", identifier)
		}
	}
}

func TestSearchRejectsCredentialNewlines(t *testing.T) {
	client := NewClient()
	client.BaseURL = "https://example.test/api/records"
	client.AccessToken = "secret\nleak"
	client.HTTP = doerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("request must not be sent")
		return nil, nil
	})
	if _, err := client.Search(context.Background(), SearchOptions{}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error = %v; want a redacted credential validation error", err)
	}
}
