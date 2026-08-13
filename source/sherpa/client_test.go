package sherpa

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLookupSelectsImmediateRepositoryLicense(t *testing.T) {
	t.Parallel()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			http.Redirect(w, r, server.URL+"/publication/12345", http.StatusFound)
		case "/retrieve":
			if got := r.URL.Query().Get("api-key"); got != "secret" {
				t.Errorf("api-key = %q", got)
			}
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Errorf("Accept = %q", got)
			}
			_, _ = w.Write([]byte(`{"items":[{"publisher_policy":[{"uri":"https://example.edu/policy","permitted_oa":[{"article_version":["published"],"conditions":["credit publisher"],"embargo":{"amount":0},"license":[{"license":"cc_by_nc","version":"4.0"}],"location":{"location":["institutional_repository"]}}]}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{SearchURL: server.URL + "/search", RetrieveURL: server.URL + "/retrieve", HTTP: noRedirectClient()}
	got, err := client.Lookup(context.Background(), "1234-5678", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if got.PublicationID != "12345" || got.LicenseURI != "https://creativecommons.org/licenses/by-nc/4.0/" || got.ReviewRequired {
		t.Fatalf("Lookup() = %+v", got)
	}
}

func TestLookupKeepsEmbargoedPolicyForReview(t *testing.T) {
	t.Parallel()
	decoded := response{Items: []publication{{PublisherPolicy: []policy{{
		URI: "https://example.edu/policy",
		PermittedOA: []permittedOpen{{
			ArticleVersion: []string{"published"},
			Embargo: struct {
				Amount int    `json:"amount"`
				Units  string `json:"units"`
			}{Amount: 12, Units: "months"},
			Location: struct {
				Locations []string `json:"location"`
			}{Locations: []string{"institutional_repository"}},
		}},
	}}}}}
	got := selectRecommendation("1234-5678", "42", decoded)
	if !got.ReviewRequired || got.EmbargoAmount != 12 || got.PolicyURI == "" {
		t.Fatalf("selectRecommendation() = %+v", got)
	}
}

func TestLookupDoesNotExposeAPIKeyInStatusError(t *testing.T) {
	t.Parallel()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			http.Redirect(w, r, server.URL+"/publication/123", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := &Client{SearchURL: server.URL + "/search", RetrieveURL: server.URL + "/retrieve", HTTP: noRedirectClient()}
	_, err := client.Lookup(context.Background(), "1234-5678", "do-not-leak")
	if err == nil || strings.Contains(err.Error(), "do-not-leak") || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("Lookup() error = %v", err)
	}
}

func TestLookupDoesNotExposeAPIKeyInTransportError(t *testing.T) {
	t.Parallel()
	const secret = "do-not-leak"
	client := &Client{
		SearchURL:   "https://sherpa.example/search",
		RetrieveURL: "https://sherpa.example/retrieve",
		HTTP: httpDoerFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/search" {
				return &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": []string{"/publication/123"}},
					Body:       http.NoBody,
					Request:    request,
				}, nil
			}
			return nil, &url.Error{
				Op:  "Get",
				URL: request.URL.String(),
				Err: fmt.Errorf("proxy rejected api-key %s", request.URL.Query().Get("api-key")),
			}
		}),
	}

	_, err := client.Lookup(context.Background(), "1234-5678", secret)
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("Lookup() error = %v", err)
	}
}

func TestLookupRejectsCrossOriginPublicationRedirect(t *testing.T) {
	t.Parallel()
	client := &Client{
		SearchURL:   "https://sherpa.example/search",
		RetrieveURL: "https://sherpa.example/retrieve",
		HTTP: httpDoerFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://evil.example/publication/123"}},
				Body:       http.NoBody,
				Request:    request,
			}, nil
		}),
	}
	_, err := client.Lookup(t.Context(), "1234-5678", "secret")
	if err == nil || !strings.Contains(err.Error(), "unsafe redirect") {
		t.Fatalf("Lookup() error = %v", err)
	}
}

func TestLookupRejectsInsecureRemoteOrCredentialedEndpoints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		searchURL string
	}{
		{name: "remote HTTP", searchURL: "http://sherpa.example/search"},
		{name: "URL credentials", searchURL: "https://user:password@sherpa.example/search"},
		{name: "URL query", searchURL: "https://sherpa.example/search?api-key=secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &Client{SearchURL: test.searchURL, RetrieveURL: "https://sherpa.example/retrieve"}
			if _, err := client.Lookup(t.Context(), "1234-5678", "secret"); err == nil {
				t.Fatal("Lookup() error = nil")
			}
		})
	}
}

func TestDefaultHTTPClientDoesNotInheritEnvironmentProxy(t *testing.T) {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || defaultTransport.Proxy == nil {
		t.Fatal("test requires the standard environment-aware default transport")
	}
	t.Setenv("HTTPS_PROXY", "http://hostile-proxy.invalid:8080")

	client, ok := (&Client{}).httpClient().(*http.Client)
	if !ok {
		t.Fatal("default SHERPA client did not return an HTTP client")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("protected SHERPA transport inherited environment proxy support")
	}
	request, err := http.NewRequest(http.MethodGet, "https://v2.sherpa.ac.uk/publication/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := http.NewRequest(http.MethodGet, "https://v2.sherpa.ac.uk/search", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(request, []*http.Request{origin}); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect() error = %v", err)
	}
}

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (do httpDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return do(request)
}

func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
