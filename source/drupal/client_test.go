package drupal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/reconcile"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

func TestCandidatesQueriesStrongIdentifierAndParsesResource(t *testing.T) {
	client := NewClient("https://repo.example.org/jsonapi")
	client.Auth = BearerTokenAuth("secret")
	requests := 0
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if got := request.URL.Path; got != "/jsonapi/node/islandora_object" {
			t.Fatalf("request path = %q, want Drupal JSON:API collection path", got)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing auth header")
		}
		if strings.Contains(request.URL.String(), "secret") {
			t.Fatal("credential leaked into URL")
		}
		if got := request.URL.Query().Get("filter[crosswalk][condition][value]"); got != "10.1234/example" {
			t.Fatalf("filter value = %q", got)
		}
		if got := request.URL.Query().Get("filter[crosswalk][condition][operator]"); got != "CONTAINS" {
			t.Fatalf("filter operator = %q, want CONTAINS", got)
		}
		body := `{"data":[{"type":"node--islandora_object","id":"11111111-1111-1111-1111-111111111111","attributes":{"drupal_internal__nid":42,"title":"A title","field_identifier":[{"attr0":"doi","value":"10.1234/example"}],"field_edtf_date_issued":[{"value":"2024"}]},"links":{"self":{"href":"https://user:password@REPO.EXAMPLE.ORG/jsonapi/node/islandora_object/11111111-1111-1111-1111-111111111111?page=2&access_token=secret#metadata"}}}],"links":{}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	candidates, err := client.Candidates(context.Background(), reconcile.Query{
		Strategy:    reconcile.QueryByIdentifier,
		Identifiers: []reconcile.IdentifierKey{{Scheme: "doi", NamespaceURI: "https://doi.org/", Value: "10.1234/example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want typed field lookup without unnecessary fallback", requests)
	}
	if len(candidates) != 1 || candidates[0].RepositoryID != "42" || candidates[0].UUID == "" || candidates[0].Record.Title != "A title" {
		t.Fatalf("candidates = %+v", candidates)
	}
	if got, want := candidates[0].URL, "https://repo.example.org/jsonapi/node/islandora_object/11111111-1111-1111-1111-111111111111?page=2"; got != want {
		t.Fatalf("candidate URL = %q, want %q", got, want)
	}
}

func TestEndpointAcceptsJSONAPITypeOrCollectionPath(t *testing.T) {
	t.Parallel()
	for _, resourceType := range []string{"node--islandora_object", "node/islandora_object"} {
		client := NewClient("https://repo.example.org/jsonapi")
		client.ResourceType = resourceType
		endpoint, err := client.endpoint()
		if err != nil {
			t.Fatal(err)
		}
		if got := endpoint.Path; got != "/jsonapi/node/islandora_object" {
			t.Errorf("resource type %q path = %q", resourceType, got)
		}
	}
	client := NewClient("https://repo.example.org/jsonapi")
	client.ResourceType = "../../admin"
	if _, err := client.endpoint(); err == nil {
		t.Fatal("expected unsafe resource type error")
	}
}

func TestCandidatesRejectsUnsafeEndpointAndPagination(t *testing.T) {
	client := NewClient("http://repo.example.org/jsonapi")
	if _, err := client.Candidates(context.Background(), reconcile.Query{Strategy: reconcile.QueryByMetadata, Title: "x"}); err == nil {
		t.Fatal("expected insecure endpoint error")
	}

	client = NewClient("https://repo.example.org/jsonapi")
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"data":[],"links":{"next":{"href":"https://evil.example.org/jsonapi/node/islandora_object?page=2"}}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	if _, err := client.Candidates(context.Background(), reconcile.Query{Strategy: reconcile.QueryByMetadata, Title: "x"}); err == nil || !strings.Contains(err.Error(), "unsafe pagination") {
		t.Fatalf("pagination error = %v", err)
	}
}

func TestDefaultClientAllowsConfiguredLoopbackRepository(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := request.URL.Path; got != "/jsonapi/node/islandora_object" {
			t.Errorf("request path = %q", got)
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()

	client := NewClient(server.URL + "/jsonapi")
	if _, err := client.Candidates(t.Context(), reconcile.Query{Strategy: reconcile.QueryByMetadata, Title: "A title"}); err != nil {
		t.Fatalf("Candidates() error = %v", err)
	}
}

func TestDefaultClientRejectsCrossOriginRedirect(t *testing.T) {
	t.Parallel()
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, target.URL+"/jsonapi", http.StatusFound)
	}))
	defer origin.Close()

	client := NewClient(origin.URL + "/jsonapi")
	_, err := client.Candidates(t.Context(), reconcile.Query{Strategy: reconcile.QueryByMetadata, Title: "A title"})
	if err == nil || !strings.Contains(err.Error(), "unsafe metadata redirect") {
		t.Fatalf("Candidates() error = %v, want unsafe redirect rejection", err)
	}
	if targetRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests", targetRequests.Load())
	}
}

func TestClientRedactsAuthenticationFromTransportError(t *testing.T) {
	t.Parallel()
	const secret = "do-not-leak"
	client := NewClient("https://repo.example.org/jsonapi")
	client.Auth = BearerTokenAuth(secret)
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		return nil, &url.Error{
			Op:  "Get",
			URL: request.URL.String(),
			Err: fmt.Errorf("proxy rejected %s", request.Header.Get("Authorization")),
		}
	})
	_, err := client.Candidates(t.Context(), reconcile.Query{Strategy: reconcile.QueryByMetadata, Title: "A title"})
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "Bearer ") {
		t.Fatalf("Candidates() error leaked authentication: %v", err)
	}
}

func TestClientEnforcesResponseLimit(t *testing.T) {
	t.Parallel()
	client := NewClient("https://repo.example.org/jsonapi")
	client.MaxResponseBytes = 8
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
			Request:    request,
		}, nil
	})
	_, err := client.Candidates(t.Context(), reconcile.Query{Strategy: reconcile.QueryByMetadata, Title: "A title"})
	if err == nil || !strings.Contains(err.Error(), "response exceeds 8 bytes") {
		t.Fatalf("Candidates() error = %v, want response limit", err)
	}
}

func TestCandidatesRejectUnsafeSelfLink(t *testing.T) {
	t.Parallel()
	client := NewClient("https://repo.example.org/jsonapi")
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"data":[{"type":"node--islandora_object","id":"11111111-1111-1111-1111-111111111111","attributes":{"title":"One"},"links":{"self":{"href":"file:///tmp/candidate.json"}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	_, err := client.Candidates(context.Background(), reconcile.Query{Strategy: reconcile.QueryByMetadata, Title: "One"})
	if err == nil || !strings.Contains(err.Error(), "unsafe candidate self link") {
		t.Fatalf("Candidates() error = %v", err)
	}
}

func TestMetadataCandidatesUseContainsAndIncludedLinkedAgent(t *testing.T) {
	t.Parallel()
	client := NewClient("https://repo.example.org/jsonapi")
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if got := query.Get("filter[crosswalk][condition][operator]"); got != "CONTAINS" {
			t.Errorf("operator = %q, want CONTAINS", got)
		}
		if got := query.Get("include"); got != "field_linked_agent" {
			t.Errorf("include = %q", got)
		}
		body := `{"data":[{"type":"node--islandora_object","id":"11111111-1111-1111-1111-111111111111","attributes":{"drupal_internal__nid":42,"title":"A distinctive scholarly title","field_edtf_date_issued":[{"value":"2024"}]},"relationships":{"field_linked_agent":{"data":[{"type":"taxonomy_term--person","id":"22222222-2222-2222-2222-222222222222","meta":{"drupal_internal__target_id":906,"rel_type":"relators:aut"}}]}}}],"included":[{"type":"taxonomy_term--person","id":"22222222-2222-2222-2222-222222222222","attributes":{"name":"Doe, Jane"}}]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	candidates, err := client.Candidates(context.Background(), reconcile.Query{Strategy: reconcile.QueryByMetadata, Title: "distinctive scholarly"})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || len(candidates[0].Record.Contributors) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	contributor := candidates[0].Record.Contributors[0]
	if contributor.Name != "Doe, Jane" || contributor.RoleCode != "relators:aut" {
		t.Fatalf("contributor = %#v", contributor)
	}
}

func TestIdentifierLookupFallsBackOnlyWhenTypedFilterIsUnsupported(t *testing.T) {
	client := NewClient("https://repo.example.org/jsonapi")
	requests := 0
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errors":[{"detail":"invalid field"}]}`)), Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Request: request}, nil
	})
	_, err := client.Candidates(context.Background(), reconcile.Query{Strategy: reconcile.QueryByIdentifier, Identifiers: []reconcile.IdentifierKey{{Scheme: "doi", NamespaceURI: "https://doi.org/", Value: "10.1234/example"}}})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}

	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`unauthorized`)), Request: request}, nil
	})
	_, err = client.Candidates(context.Background(), reconcile.Query{Strategy: reconcile.QueryByIdentifier, Identifiers: []reconcile.IdentifierKey{{Scheme: "doi", NamespaceURI: "https://doi.org/", Value: "10.1234/example"}}})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("authentication error = %v", err)
	}
}

func TestIdentifierLookupUsesWOSAccessionVariant(t *testing.T) {
	t.Parallel()
	client := NewClient("https://repo.example.org/jsonapi")
	var values []string
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		values = append(values, request.URL.Query().Get("filter[crosswalk][condition][value]"))
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Request: request}, nil
	})
	_, err := client.Candidates(context.Background(), reconcile.Query{
		Strategy:    reconcile.QueryByIdentifier,
		Identifiers: []reconcile.IdentifierKey{{Scheme: "wos", NamespaceURI: "https://www.webofscience.com/", Value: "WOS:000123456789"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(values, ",") != "WOS:000123456789,WOS:000123456789,000123456789,000123456789" {
		t.Fatalf("lookup values = %#v", values)
	}
}

func TestTitleLookupValuesIncludeConservativeBroadening(t *testing.T) {
	t.Parallel()
	got := titleLookupValues("Distinctive scholarly methods: a longitudinal analysis")
	want := []string{"Distinctive scholarly methods: a longitudinal analysis", "Distinctive scholarly methods", "longitudinal"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("titleLookupValues() = %#v, want %#v", got, want)
	}
}

func TestMetadataCandidatesStopAfterSpecificTitleMatch(t *testing.T) {
	t.Parallel()
	client := NewClient("https://repo.example.org/jsonapi")
	var values []string
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		values = append(values, request.URL.Query().Get("filter[crosswalk][condition][value]"))
		body := `{"data":[{"type":"node--islandora_object","id":"11111111-1111-1111-1111-111111111111","attributes":{"drupal_internal__nid":42,"title":"Distinctive scholarly methods: a longitudinal analysis"}}]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	_, err := client.Candidates(context.Background(), reconcile.Query{
		Strategy: reconcile.QueryByMetadata,
		Title:    "Distinctive scholarly methods: a longitudinal analysis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0] != "Distinctive scholarly methods: a longitudinal analysis" {
		t.Fatalf("lookup values = %#v, want only the complete title", values)
	}
}

func TestMetadataCandidatesBroadenOnlyUntilAMatch(t *testing.T) {
	t.Parallel()
	client := NewClient("https://repo.example.org/jsonapi")
	var values []string
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		value := request.URL.Query().Get("filter[crosswalk][condition][value]")
		values = append(values, value)
		body := `{"data":[]}`
		if value == "Distinctive scholarly methods" {
			body = `{"data":[{"type":"node--islandora_object","id":"11111111-1111-1111-1111-111111111111","attributes":{"drupal_internal__nid":42,"title":"Distinctive scholarly methods"}}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	_, err := client.Candidates(context.Background(), reconcile.Query{
		Strategy: reconcile.QueryByMetadata,
		Title:    "Distinctive scholarly methods: a longitudinal analysis",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Distinctive scholarly methods: a longitudinal analysis", "Distinctive scholarly methods"}
	if strings.Join(values, "|") != strings.Join(want, "|") {
		t.Fatalf("lookup values = %#v, want %#v", values, want)
	}
}
