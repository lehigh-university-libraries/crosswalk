package crossref

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

func TestSearchBuildsBoundedPage(t *testing.T) {
	client := NewClient()
	client.BaseURL = "https://example.test/works"
	client.HTTP = source.NewClient()
	client.HTTP.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("query.title") != "A title" || query.Get("query.author") != "Smith" || query.Get("rows") != "25" || query.Get("offset") != "50" {
			t.Fatalf("unexpected query: %s", request.URL.RawQuery)
		}
		if query.Get("filter") != "from-pub-date:2024-01-01,type:journal-article" {
			t.Fatalf("filter = %q", query.Get("filter"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":{"items":[]}}`)), Request: request}, nil
	})
	_, err := client.Search(context.Background(), SearchOptions{QueryTitle: "A title", QueryAuthor: "Smith", Filter: []string{"from-pub-date:2024-01-01", "type:journal-article"}, Rows: 25, Offset: 50})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchRequiresQueryAndBoundsRows(t *testing.T) {
	client := NewClient()
	if _, err := client.Search(context.Background(), SearchOptions{}); err == nil {
		t.Fatal("expected query error")
	}
	if _, err := client.Search(context.Background(), SearchOptions{Query: "x", Rows: 1001}); err == nil {
		t.Fatal("expected rows error")
	}
}
