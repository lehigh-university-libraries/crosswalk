package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClientFetch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "crosswalk-test" {
			t.Errorf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient()
	client.AllowPrivate = true
	client.UserAgent = "crosswalk-test"
	doc, err := client.Fetch(context.Background(), server.URL, "application/json")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if doc.ContentType != "application/json" {
		t.Fatalf("ContentType = %q", doc.ContentType)
	}
	if string(doc.Data) != `{"ok":true}` {
		t.Fatalf("Data = %q", doc.Data)
	}
}

func TestClientFetchRejectsBadResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		limit   int64
		want    string
	}{
		{
			name: "status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			want: "status 502",
		},
		{
			name: "size",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("too large"))
			},
			limit: 3,
			want:  "exceeds 3 bytes",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(test.handler)
			defer server.Close()
			client := NewClient()
			client.AllowPrivate = true
			client.MaxResponseBytes = test.limit
			_, err := client.Fetch(context.Background(), server.URL, "")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Fetch() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestClientFetchRequiresHTTPURL(t *testing.T) {
	t.Parallel()
	client := NewClient()
	_, err := client.Fetch(context.Background(), "file:///etc/passwd", "")
	if err == nil || !strings.Contains(err.Error(), "unsupported scheme") {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestDefaultHTTPClientDoesNotInheritEnvironmentProxy(t *testing.T) {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || defaultTransport.Proxy == nil {
		t.Fatal("test requires the standard environment-aware default transport")
	}
	t.Setenv("HTTPS_PROXY", "http://hostile-proxy.invalid:8080")

	client := defaultHTTPClient(false, false, RedirectSameOrigin)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("protected metadata transport inherited environment proxy support")
	}
}

func TestClientAllowLoopbackDoesNotPermitOtherPrivateAddresses(t *testing.T) {
	t.Parallel()
	if !permittedMetadataIP(net.ParseIP("127.0.0.1"), false, true) {
		t.Fatal("loopback address was rejected with loopback access enabled")
	}
	if permittedMetadataIP(net.ParseIP("10.0.0.1"), false, true) {
		t.Fatal("non-loopback private address was allowed by loopback-only access")
	}
	if !permittedMetadataIP(net.ParseIP("10.0.0.1"), true, false) {
		t.Fatal("explicit private-network access did not allow a private address")
	}
}

func TestPublicMetadataIPAcceptsOnlyPublicRoutableAddresses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		address string
		public  bool
	}{
		{address: "8.8.8.8", public: true},
		{address: "1.1.1.1", public: true},
		{address: "2606:4700:4700::1111", public: true},
		{address: "2001:4860:4860::8888", public: true},
		{address: "0.0.0.0"},
		{address: "10.0.0.1"},
		{address: "100.64.0.1"},
		{address: "127.0.0.1"},
		{address: "169.254.1.1"},
		{address: "172.16.0.1"},
		{address: "192.0.0.9"},
		{address: "192.0.2.1"},
		{address: "192.168.1.1"},
		{address: "198.18.0.1"},
		{address: "198.51.100.1"},
		{address: "203.0.113.1"},
		{address: "224.0.0.1"},
		{address: "240.0.0.1"},
		{address: "255.255.255.255"},
		{address: "::"},
		{address: "::1"},
		{address: "::ffff:127.0.0.1"},
		{address: "64:ff9b::1"},
		{address: "100::1"},
		{address: "2001::1"},
		{address: "2001:db8::1"},
		{address: "2002::1"},
		{address: "3fff::1"},
		{address: "5f00::1"},
		{address: "fc00::1"},
		{address: "fe80::1"},
		{address: "ff02::1"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.address, func(t *testing.T) {
			t.Parallel()
			if got := publicMetadataIP(net.ParseIP(test.address)); got != test.public {
				t.Fatalf("publicMetadataIP(%q) = %t, want %t", test.address, got, test.public)
			}
		})
	}
}

func TestPermittedMetadataIPRejectsInvalidUnspecifiedAndMulticastEvenWhenPrivateAllowed(t *testing.T) {
	t.Parallel()
	for _, address := range []string{"0.0.0.0", "224.0.0.1", "::", "ff02::1"} {
		if permittedMetadataIP(net.ParseIP(address), true, true) {
			t.Errorf("permittedMetadataIP(%q) unexpectedly allowed unusable destination", address)
		}
	}
	if permittedMetadataIP(nil, true, true) {
		t.Error("permittedMetadataIP(nil) unexpectedly allowed invalid destination")
	}
}

func TestClientFetchRejectsPrivateAddressesByDefault(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("private endpoint should not be reached")
	}))
	defer server.Close()

	_, err := NewClient().Fetch(context.Background(), server.URL, "")
	if err == nil || !strings.Contains(err.Error(), "no permitted address") {
		t.Fatalf("Fetch() error = %v, want private-address rejection", err)
	}
}

func TestClientFetchHandlesInjectedResponseWithoutRequest(t *testing.T) {
	t.Parallel()

	client := NewClient()
	client.HTTP = httpDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("metadata")),
		}, nil
	})
	doc, err := client.Fetch(context.Background(), "https://example.org/metadata", "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if doc.URL != "https://example.org/metadata" {
		t.Fatalf("Document.URL = %q", doc.URL)
	}
}

func TestClientFetchRejectsInjectedEmptyResponse(t *testing.T) {
	t.Parallel()

	client := NewClient()
	client.HTTP = httpDoerFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	})
	_, err := client.Fetch(context.Background(), "https://example.org/metadata", "")
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("Fetch() error = %v, want empty response", err)
	}
}

func TestClientFetchRejectsInjectedHTTPSDowngrade(t *testing.T) {
	t.Parallel()
	client := NewClient()
	client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		redirected, err := http.NewRequest(http.MethodGet, "http://publisher.example/metadata", nil)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("metadata")),
			Request:    redirected,
		}, nil
	})
	_, err := client.Fetch(context.Background(), "https://doi.org/10.1234/example", "")
	if err == nil || !strings.Contains(err.Error(), "unsafe redirect") {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestClientFetchRequestAppliesAuthAndSanitizesProvenance(t *testing.T) {
	t.Parallel()
	const secret = "private-api-token"
	client := NewClient()
	client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("X-ApiKey"); got != secret {
			t.Fatalf("X-ApiKey = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("metadata")),
			Request:    request,
		}, nil
	})
	document, err := client.FetchRequest(t.Context(), Request{
		URL:  "https://example.org/metadata?api_key=" + secret + "&page=2#request",
		Auth: HeaderAuth{Name: "X-ApiKey", Value: secret},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(document.URL, secret) || document.URL != "https://example.org/metadata?page=2" {
		t.Fatalf("Document.URL = %q", document.URL)
	}
}

func TestClientFetchRequestDoesNotLeakCredentialInRequestError(t *testing.T) {
	t.Parallel()
	const secret = "private-api-token"
	client := NewClient()
	client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: request.URL.String(), Err: fmt.Errorf("rejected credential %s", request.Header.Get("X-ApiKey"))}
	})
	_, err := client.FetchRequest(t.Context(), Request{
		URL:  "https://example.org/metadata?api_key=" + secret,
		Auth: HeaderAuth{Name: "X-ApiKey", Value: secret},
	})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("FetchRequest() error = %v", err)
	}
}

func TestClientFetchRequestReportsRetryAfter(t *testing.T) {
	t.Parallel()
	client := NewClient()
	client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"120"}},
			Body:       io.NopCloser(strings.NewReader("slow down")),
			Request:    request,
		}, nil
	})
	_, err := client.Fetch(t.Context(), "https://example.org/metadata", "")
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.Status != http.StatusTooManyRequests || status.RetryAfter != 2*time.Minute {
		t.Fatalf("Fetch() error = %#v", err)
	}
}

func TestClientFetchRequestRedirectPolicies(t *testing.T) {
	t.Parallel()
	client := NewClient()
	client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.URL, _ = url.Parse("https://publisher.example/record")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("metadata")),
			Request:    redirected,
		}, nil
	})
	if _, err := client.Fetch(t.Context(), "https://resolver.example/id", ""); err == nil || !strings.Contains(err.Error(), "unsafe redirect") {
		t.Fatalf("same-origin Fetch() error = %v", err)
	}
	client.RedirectPolicy = RedirectHTTPS
	if _, err := client.Fetch(t.Context(), "https://resolver.example/id", ""); err != nil {
		t.Fatalf("HTTPS redirect Fetch() error = %v", err)
	}
	_, err := client.FetchRequest(t.Context(), Request{
		URL: "https://resolver.example/id", RedirectPolicy: RedirectHTTPS,
		Auth: BearerAuth("secret"),
	})
	if err == nil || !strings.Contains(err.Error(), "authenticated requests require same-origin") {
		t.Fatalf("authenticated cross-origin policy error = %v", err)
	}
}
