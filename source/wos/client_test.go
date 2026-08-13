package wos

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

func TestSearchUsesHeaderAndBoundsPage(t *testing.T) {
	client := NewClient("secret-key")
	client.BaseURL = "https://example.test/v1"
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("X-ApiKey"); got != "secret-key" {
			t.Fatalf("X-ApiKey = %q", got)
		}
		if request.URL.Query().Get("q") != "AU=Smith" || request.URL.Query().Get("limit") != "25" || request.URL.Query().Get("page") != "2" {
			t.Fatalf("unexpected query: %s", request.URL.RawQuery)
		}
		if strings.Contains(request.URL.String(), "secret-key") {
			t.Fatal("API key leaked into URL")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"hits":[]}`)), Header: make(http.Header), Request: request}, nil
	})
	if _, err := client.Search(context.Background(), SearchOptions{Query: "AU=Smith", Page: 2, Limit: 25}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchRejectsUnboundedLimit(t *testing.T) {
	client := NewClient("secret-key")
	if _, err := client.Search(context.Background(), SearchOptions{Query: "AU=Smith", Limit: 51}); err == nil {
		t.Fatal("expected limit error")
	}
}
