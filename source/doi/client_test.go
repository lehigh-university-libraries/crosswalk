package doi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

func TestResolveCSL(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != "/10.1234/example" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Accept"); got != CSLJSONMediaType {
			t.Errorf("Accept = %q", got)
		}
		w.Header().Set("Content-Type", CSLJSONMediaType)
		_, _ = w.Write([]byte(`{"DOI":"10.1234/example"}`))
	}))
	defer server.Close()

	httpClient := source.NewClient()
	httpClient.AllowPrivate = true
	client := &Client{BaseURL: server.URL, HTTP: httpClient}
	doc, err := client.ResolveCSL(context.Background(), "https://doi.org/10.1234/example")
	if err != nil {
		t.Fatalf("ResolveCSL() error = %v", err)
	}
	if !strings.Contains(string(doc.Data), "10.1234/example") {
		t.Fatalf("Data = %q", doc.Data)
	}
}

func TestResolveCSLRejectsInvalidDOI(t *testing.T) {
	t.Parallel()
	_, err := NewClient().ResolveCSL(context.Background(), "not-a-doi")
	if err == nil || !strings.Contains(err.Error(), "invalid DOI") {
		t.Fatalf("ResolveCSL() error = %v", err)
	}
}
