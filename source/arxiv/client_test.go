package arxiv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

func TestSearchBuildsEncodedQuery(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if got := query.Get("search_query"); got != "au:Smith AND cat:cs.DL" {
			t.Errorf("search_query = %q", got)
		}
		if got := query.Get("id_list"); got != "2401.00001,2401.00002" {
			t.Errorf("id_list = %q", got)
		}
		if got := query.Get("start"); got != "10" {
			t.Errorf("start = %q", got)
		}
		if got := query.Get("max_results"); got != "25" {
			t.Errorf("max_results = %q", got)
		}
		_, _ = w.Write([]byte("<feed/>"))
	}))
	defer server.Close()

	httpClient := source.NewClient()
	httpClient.AllowPrivate = true
	client := &Client{APIBaseURL: server.URL, HTTP: httpClient}
	_, err := client.Search(context.Background(), SearchOptions{
		Query:      "au:Smith AND cat:cs.DL",
		IDs:        []string{"arXiv:2401.00001v2", "https://arxiv.org/abs/2401.00002"},
		Start:      10,
		MaxResults: 25,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestOAIBuildsGetRecordRequest(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if got := query.Get("verb"); got != "GetRecord" {
			t.Errorf("verb = %q", got)
		}
		if got := query.Get("identifier"); got != "oai:arXiv.org:2401.00001" {
			t.Errorf("identifier = %q", got)
		}
		if got := query.Get("metadataPrefix"); got != "arXiv" {
			t.Errorf("metadataPrefix = %q", got)
		}
		_, _ = w.Write([]byte("<OAI-PMH/>"))
	}))
	defer server.Close()

	httpClient := source.NewClient()
	httpClient.AllowPrivate = true
	client := &Client{OAIBaseURL: server.URL, HTTP: httpClient}
	_, err := client.OAI(context.Background(), "arXiv:2401.00001v3")
	if err != nil {
		t.Fatalf("OAI() error = %v", err)
	}
}

func TestSearchRequiresSelector(t *testing.T) {
	t.Parallel()
	_, err := NewClient().Search(context.Background(), SearchOptions{})
	if err == nil {
		t.Fatal("Search() error = nil")
	}
}
