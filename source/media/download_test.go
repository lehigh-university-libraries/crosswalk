package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type mediaDoerFunc func(*http.Request) (*http.Response, error)

func (function mediaDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestDownloadWritesValidatedPDFAndReusesIt(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/pdf" {
			t.Errorf("Accept = %q", got)
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.7\nbody"))
	}))
	defer server.Close()

	directory := t.TempDir()
	downloader := &Downloader{HTTP: server.Client(), MaxBytes: 1024}
	body := []byte("%PDF-1.7\nbody")
	digest := sha256.Sum256(body)
	request := Request{
		URL: server.URL, Directory: directory, Filename: "paper.pdf", ExpectedMediaType: "application/pdf",
		ExpectedSHA256: hex.EncodeToString(digest[:]),
	}
	result, err := downloader.Download(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != filepath.Join(directory, "paper.pdf") || result.SizeBytes == 0 || result.Reused || result.SHA256 != request.ExpectedSHA256 {
		t.Fatalf("Download() = %+v", result)
	}

	reused, err := downloader.Download(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reused.Reused || reused.Path != result.Path {
		t.Fatalf("reused Download() = %+v", reused)
	}
}

func TestDownloadDoesNotReuseSameFilenameWithoutTrustedDigest(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte("%PDF-1.7\nfirst"))
			return
		}
		_, _ = w.Write([]byte("%PDF-1.7\nsecond"))
	}))
	defer server.Close()

	directory := t.TempDir()
	request := Request{URL: server.URL, Directory: directory, Filename: "paper.pdf", ExpectedMediaType: "application/pdf"}
	downloader := &Downloader{HTTP: server.Client(), MaxBytes: 1024}
	if _, err := downloader.Download(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	result, err := downloader.Download(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reused || calls.Load() != 2 || string(data) != "%PDF-1.7\nsecond" {
		t.Fatalf("second download = %+v calls=%d data=%q", result, calls.Load(), data)
	}
}

func TestDownloadRejectsDigestMismatchWithoutPublishing(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("%PDF-1.7\nbody"))
	}))
	defer server.Close()

	directory := t.TempDir()
	_, err := (&Downloader{HTTP: server.Client(), MaxBytes: 1024}).Download(context.Background(), Request{
		URL: server.URL, Directory: directory, Filename: "paper.pdf", ExpectedMediaType: "application/pdf",
		ExpectedSHA256: strings.Repeat("0", 64),
	})
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("Download() error = %v, want digest mismatch", err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, "paper.pdf")); !os.IsNotExist(statErr) {
		t.Fatalf("digest-mismatched destination remains: %v", statErr)
	}
}

func TestDownloadRejectsOversizedOrInvalidPDF(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		body  string
		limit int64
		want  string
	}{
		{name: "oversized", body: "%PDF-123456", limit: 5, want: "exceeds 5 bytes"},
		{name: "not pdf", body: "<html>login</html>", limit: 1024, want: "not a PDF"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			directory := t.TempDir()
			_, err := (&Downloader{HTTP: server.Client(), MaxBytes: test.limit}).Download(context.Background(), Request{
				URL: server.URL, Directory: directory, Filename: "paper.pdf", ExpectedMediaType: "application/pdf",
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Download() error = %v, want %q", err, test.want)
			}
			if _, statErr := os.Stat(filepath.Join(directory, "paper.pdf")); !os.IsNotExist(statErr) {
				t.Fatalf("partial destination remains: %v", statErr)
			}
		})
	}
}

func TestDownloadRejectsPathFilename(t *testing.T) {
	t.Parallel()
	_, err := (&Downloader{}).Download(context.Background(), Request{
		URL: "https://example.org/paper.pdf", Directory: t.TempDir(), Filename: "../paper.pdf",
	})
	if err == nil || !strings.Contains(err.Error(), "must not contain a path") {
		t.Fatalf("Download() error = %v", err)
	}
}

func TestDownloadRejectsURLUserInformation(t *testing.T) {
	t.Parallel()
	_, err := (&Downloader{}).Download(t.Context(), Request{
		URL: "https://user:password@example.org/paper.pdf", Directory: t.TempDir(), Filename: "paper.pdf",
	})
	if err == nil || !strings.Contains(err.Error(), "user information is not allowed") {
		t.Fatalf("Download() error = %v", err)
	}
}

func TestDownloadRedactsSignedURLFromErrorsAndProvenance(t *testing.T) {
	t.Parallel()
	const secret = "do-not-leak"
	downloader := &Downloader{MaxBytes: 1024}
	downloader.HTTP = mediaDoerFunc(func(request *http.Request) (*http.Response, error) {
		return nil, &url.Error{
			Op:  "Get",
			URL: request.URL.String(),
			Err: fmt.Errorf("publisher rejected signature %s", request.URL.Query().Get("X-Amz-Signature")),
		}
	})
	request := Request{
		URL:       "https://publisher.example/paper.pdf?X-Amz-Signature=" + secret + "&download=1",
		Directory: t.TempDir(), Filename: "paper.pdf", ExpectedMediaType: "application/pdf",
	}
	if _, err := downloader.Download(t.Context(), request); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("Download() transport error leaked signed URL: %v", err)
	}

	downloader.HTTP = mediaDoerFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/pdf"}},
			Body:       io.NopCloser(strings.NewReader("%PDF-1.7\nbody")),
			Request:    request,
		}, nil
	})
	result, err := downloader.Download(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.URL, secret) || result.URL != "https://publisher.example/paper.pdf?download=1" {
		t.Fatalf("Result.URL = %q", result.URL)
	}
}

func TestDownloadRejectsInjectedHTTPSDowngrade(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	downloader := &Downloader{MaxBytes: 1024, HTTP: mediaDoerFunc(func(*http.Request) (*http.Response, error) {
		redirected, err := http.NewRequest(http.MethodGet, "http://publisher.example/paper.pdf", nil)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("%PDF-1.7\nbody")),
			Request:    redirected,
		}, nil
	})}
	_, err := downloader.Download(t.Context(), Request{
		URL: "https://publisher.example/paper.pdf", Directory: directory,
		Filename: "paper.pdf", ExpectedMediaType: "application/pdf",
	})
	if err == nil || !strings.Contains(err.Error(), "unsafe redirect target") {
		t.Fatalf("Download() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, "paper.pdf")); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe redirect published destination: %v", statErr)
	}
}

func TestDefaultHTTPClientDoesNotInheritEnvironmentProxy(t *testing.T) {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || defaultTransport.Proxy == nil {
		t.Fatal("test requires the standard environment-aware default transport")
	}
	t.Setenv("HTTPS_PROXY", "http://hostile-proxy.invalid:8080")

	client, ok := (&Downloader{}).httpClient().(*http.Client)
	if !ok {
		t.Fatal("default downloader did not return an HTTP client")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("protected media transport inherited environment proxy support")
	}
}

func TestPDFNameIsStableAndOpaque(t *testing.T) {
	t.Parallel()
	first := PDFName("10.1234/Example")
	second := PDFName(" 10.1234/example ")
	if first != second || !strings.HasSuffix(first, ".pdf") || strings.Contains(first, "1234") {
		t.Fatalf("PDFName() = %q and %q", first, second)
	}
}
