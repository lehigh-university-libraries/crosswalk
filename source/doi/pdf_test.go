package doi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

func TestPDFURLUsesCSLMediaLinkWithoutLandingRequest(t *testing.T) {
	t.Parallel()
	client := NewClient()
	got, err := client.PDFURL(context.Background(), []byte(`{
  "DOI":"10.1234/example",
  "link":[{"URL":"https://cdn.example.edu/paper.pdf","content-type":"application/pdf"}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://cdn.example.edu/paper.pdf" {
		t.Fatalf("PDFURL() = %q", got)
	}
}

func TestPDFURLDiscoversRelativeCitationMetaLink(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/html" {
			t.Errorf("Accept = %q", got)
		}
		_, _ = w.Write([]byte(`<html><head><meta content='/files/paper.pdf?download=1' name='citation_pdf_url'></head></html>`))
	}))
	defer server.Close()

	httpClient := source.NewClient()
	httpClient.AllowPrivate = true
	client := &Client{HTTP: httpClient}
	got, err := client.PDFURL(context.Background(), []byte(`{"URL":`+quoteJSON(server.URL+"/article")+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != server.URL+"/files/paper.pdf?download=1" {
		t.Fatalf("PDFURL() = %q", got)
	}
}

func TestPDFURLReportsMissingDeclaration(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()
	httpClient := source.NewClient()
	httpClient.AllowPrivate = true
	_, err := (&Client{HTTP: httpClient}).PDFURL(context.Background(), []byte(`{"URL":`+quoteJSON(server.URL)+`}`))
	if err == nil || !strings.Contains(err.Error(), "does not declare") {
		t.Fatalf("PDFURL() error = %v", err)
	}
}

func quoteJSON(value string) string {
	return `"` + value + `"`
}
