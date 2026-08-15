package getty

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestClientResolvesOnlyTheFixedGettyTerm(t *testing.T) {
	t.Parallel()

	client := NewClient()
	client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.URL.String(); got != "https://vocab.getty.edu/tgn/7013416.json" {
			t.Fatalf("request URL = %q", got)
		}
		if got := request.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
		return response(request, http.StatusOK, `{"id":"http://vocab.getty.edu/tgn/7013416","_label":"Bethlehem"}`), nil
	})
	if ok, err := client.TGNResolves(context.Background(), "7013416"); err != nil || !ok {
		t.Fatalf("TGNResolves() = (%t, %v), want (true, nil)", ok, err)
	}
}

func TestClientRejectsInvalidIDsBeforeHTTP(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	client := NewClient()
	client.HTTP = httpDoerFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected request")
	})
	for _, termID := range []string{"", " 123", "123 ", "12/3", "12?target=http://127.0.0.1", "+123", "１２３", strings.Repeat("1", maxTermIDBytes+1)} {
		if ok, err := client.TGNResolves(context.Background(), termID); ok || err == nil {
			t.Errorf("TGNResolves(%q) = (%t, %v), want invalid-ID rejection", termID, ok, err)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("HTTP calls = %d, want 0", got)
	}
}

func TestClientReturnsFalseForMissingTerms(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			client := NewClient()
			client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
				return response(request, status, `{}`), nil
			})
			if ok, err := client.TGNResolves(context.Background(), "9999999"); err != nil || ok {
				t.Fatalf("TGNResolves() = (%t, %v), want (false, nil)", ok, err)
			}
		})
	}
}

func TestClientRejectsUntrustedOrMalformedResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		requestURL string
	}{
		{name: "wrong term", body: `{"id":"http://vocab.getty.edu/tgn/7654321","_label":"Wrong"}`},
		{name: "wrong authority", body: `{"id":"https://attacker.example/tgn/1234567","_label":"Wrong"}`},
		{name: "identity query", body: `{"id":"https://vocab.getty.edu/tgn/1234567?next=1","_label":"Wrong"}`},
		{name: "missing label", body: `{"id":"http://vocab.getty.edu/tgn/1234567"}`},
		{name: "duplicate identity", body: `{"id":"http://vocab.getty.edu/tgn/1234567","id":"http://vocab.getty.edu/tgn/1234567","_label":"Duplicate"}`},
		{name: "malformed JSON", body: `{"id":`},
		{name: "cross-origin response", body: `{"id":"http://vocab.getty.edu/tgn/1234567","_label":"Term"}`, requestURL: "https://attacker.example/tgn/1234567.json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := NewClient()
			client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
				result := response(request, http.StatusOK, test.body)
				if test.requestURL != "" {
					redirected := request.Clone(request.Context())
					redirected.URL, _ = url.Parse(test.requestURL)
					result.Request = redirected
				}
				return result, nil
			})
			if ok, err := client.TGNResolves(context.Background(), "1234567"); ok || err == nil {
				t.Fatalf("TGNResolves() = (%t, %v), want response rejection", ok, err)
			}
		})
	}
}

func TestClientBoundsResponsesAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	client := NewClient()
	client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(request, http.StatusOK, strings.Repeat("x", int(maxTGNResponseBytes)+1)), nil
	})
	if ok, err := client.TGNResolves(context.Background(), "123"); ok || err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized TGNResolves() = (%t, %v), want bounded-response error", ok, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, err := client.TGNResolves(ctx, "123"); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled TGNResolves() = (%t, %v), want context.Canceled", ok, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("HTTP calls = %d, want 1", got)
	}
}

func TestClientRejectsCancellationRaisedDuringHTTP(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	client := NewClient()
	client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		cancel()
		return response(request, http.StatusOK, `{"id":"https://vocab.getty.edu/tgn/123","_label":"Term"}`), nil
	})
	if ok, err := client.TGNResolves(ctx, "123"); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("TGNResolves() = (%t, %v), want context.Canceled", ok, err)
	}
}

func TestClientSupportsConcurrentUse(t *testing.T) {
	t.Parallel()

	client := NewClient()
	client.HTTP = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		return response(request, http.StatusOK, `{"id":"https://vocab.getty.edu/page/tgn/123","_label":"Term"}`), nil
	})
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if ok, err := client.TGNResolves(context.Background(), "123"); err != nil || !ok {
				t.Errorf("TGNResolves() = (%t, %v), want (true, nil)", ok, err)
			}
		}()
	}
	wait.Wait()
}

func response(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
