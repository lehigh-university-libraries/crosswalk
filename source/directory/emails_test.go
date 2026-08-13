package directory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/source"
)

func TestExtractEmailsNormalizesDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()
	got, err := ExtractEmails("Zed@Example.edu alice@example.edu zed@example.edu invalid@localhost", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alice@example.edu", "zed@example.edu"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractEmails() = %#v, want %#v", got, want)
	}
}

func TestExtractEmailsRejectsUnexpectedlyLargeDirectory(t *testing.T) {
	t.Parallel()
	_, err := ExtractEmails("one@example.edu two@example.edu", 1)
	if err == nil || !strings.Contains(err.Error(), "more than 1") {
		t.Fatalf("ExtractEmails() error = %v", err)
	}
}

func TestClientEmailsUsesBoundedSourceClient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); !strings.Contains(got, "text/html") {
			t.Errorf("Accept = %q", got)
		}
		_, _ = w.Write([]byte("Faculty: person@example.edu"))
	}))
	defer server.Close()

	httpClient := source.NewClient()
	httpClient.AllowPrivate = true
	client := &Client{HTTP: httpClient, MaxEmails: 2}
	got, err := client.Emails(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"person@example.edu"}) {
		t.Fatalf("Emails() = %#v", got)
	}
}
