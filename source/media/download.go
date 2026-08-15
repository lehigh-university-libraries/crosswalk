// Package media downloads explicitly requested scholarly files without making
// deterministic metadata parsers or HTTP transformation handlers stateful.
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/internal/provenanceuri"
	"github.com/lehigh-university-libraries/crosswalk/source"
)

const defaultMaxBytes = int64(1 << 30)

// Downloader streams a remote file into a caller-selected directory.
type Downloader struct {
	// HTTP optionally supplies client-level settings. Supplied *http.Client
	// values retain positive timeouts, cookie jars, and stricter redirect
	// decisions, but their transports are replaced so DNS pinning, destination
	// filtering, HTTPS-only redirects, and the no-proxy policy cannot be
	// bypassed. Other source.HTTPDoer implementations are used verbatim and are
	// responsible for equivalent protections; that escape hatch is primarily
	// intended for tests and in-process adapters.
	HTTP         source.HTTPDoer
	MaxBytes     int64
	UserAgent    string
	AllowPrivate bool
}

// Request describes one media download.
type Request struct {
	URL               string
	Directory         string
	Filename          string
	ExpectedMediaType string
	// ExpectedSHA256 enables safe cache reuse and verifies newly downloaded
	// bytes. Without a trusted digest, an existing same-named file is replaced
	// atomically instead of being assumed to belong to this request.
	ExpectedSHA256 string
}

// Result describes a completed or safely reused download.
type Result struct {
	Path        string
	URL         string
	ContentType string
	SizeBytes   int64
	SHA256      string
	Reused      bool
}

// PDFName returns a stable, non-reversible filename for a source identifier.
func PDFName(identifier string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(identifier))))
	return hex.EncodeToString(digest[:]) + ".pdf"
}

// Download writes a bounded response atomically and validates the requested
// media type before publishing the destination path.
func (d *Downloader) Download(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("downloading media: context is required")
	}
	parsed, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Result{}, fmt.Errorf("downloading media: an absolute HTTP(S) URL is required")
	}
	if parsed.User != nil {
		return Result{}, fmt.Errorf("downloading media: URL user information is not allowed")
	}
	provenanceURL, err := provenanceuri.Normalize(parsed.String(), provenanceuri.Options{})
	if err != nil {
		return Result{}, fmt.Errorf("downloading media: invalid URL: %w", err)
	}
	if err := validateFilename(request.Filename); err != nil {
		return Result{}, fmt.Errorf("downloading media: %w", err)
	}
	expectedDigest, err := normalizedSHA256(request.ExpectedSHA256)
	if err != nil {
		return Result{}, fmt.Errorf("downloading media: %w", err)
	}
	directory := strings.TrimSpace(request.Directory)
	if directory == "" {
		return Result{}, fmt.Errorf("downloading media: output directory is required")
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return Result{}, fmt.Errorf("creating media directory: %w", err)
	}
	target := filepath.Join(directory, request.Filename)
	if result, ok, err := reusableFile(target, provenanceURL, request.ExpectedMediaType, expectedDigest, d.byteLimit()); err != nil {
		return Result{}, err
	} else if ok {
		return result, nil
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("creating media request: %w", err)
	}
	if mediaType := strings.TrimSpace(request.ExpectedMediaType); mediaType != "" {
		httpRequest.Header.Set("Accept", mediaType)
	}
	httpRequest.Header.Set("User-Agent", d.userAgent())

	response, err := d.httpClient().Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("requesting media: %w", source.RedactRequestError(err, httpRequest))
	}
	if response == nil || response.Body == nil {
		return Result{}, fmt.Errorf("requesting media: HTTP client returned an empty response")
	}
	defer response.Body.Close()
	finalRequestURL := parsed
	if response.Request != nil && response.Request.URL != nil {
		finalRequestURL = response.Request.URL
	}
	if !safeMediaResponseURL(parsed, finalRequestURL) {
		return Result{}, fmt.Errorf("requesting media: response came from an unsafe redirect target")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, fmt.Errorf("requesting media: unexpected HTTP status %d", response.StatusCode)
	}
	limit := d.byteLimit()
	if response.ContentLength > limit {
		return Result{}, fmt.Errorf("requesting media: response exceeds %d bytes", limit)
	}

	temporary, err := os.CreateTemp(directory, ".crosswalk-media-*")
	if err != nil {
		return Result{}, fmt.Errorf("creating temporary media file: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := false
	defer func() {
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o640); err != nil {
		_ = temporary.Close()
		return Result{}, fmt.Errorf("setting temporary media permissions: %w", err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(response.Body, limit+1))
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if copyErr != nil {
		return Result{}, fmt.Errorf("writing media: %w", copyErr)
	}
	if syncErr != nil {
		return Result{}, fmt.Errorf("syncing media: %w", syncErr)
	}
	if closeErr != nil {
		return Result{}, fmt.Errorf("closing media: %w", closeErr)
	}
	if written > limit {
		return Result{}, fmt.Errorf("requesting media: response exceeds %d bytes", limit)
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	if expectedDigest != "" && digest != expectedDigest {
		return Result{}, fmt.Errorf("validating media: SHA-256 digest mismatch")
	}
	if err := validateMediaFile(temporaryPath, request.ExpectedMediaType); err != nil {
		return Result{}, fmt.Errorf("validating media: %w", err)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return Result{}, fmt.Errorf("publishing media: %w", err)
	}
	keepTemporary = true
	finalURL, err := provenanceuri.Normalize(finalRequestURL.String(), provenanceuri.Options{})
	if err != nil {
		return Result{}, fmt.Errorf("recording media provenance: %w", err)
	}
	return Result{
		Path:        target,
		URL:         finalURL,
		ContentType: response.Header.Get("Content-Type"),
		SizeBytes:   written,
		SHA256:      digest,
	}, nil
}

func validateFilename(filename string) error {
	if filename == "" || filename == "." || filename == ".." {
		return fmt.Errorf("output filename is required")
	}
	if strings.ContainsAny(filename, "/\\\x00\r\n") || filepath.Base(filename) != filename {
		return fmt.Errorf("output filename must not contain a path: %q", filename)
	}
	return nil
}

func safeMediaResponseURL(origin, target *url.URL) bool {
	if origin == nil || target == nil || target.Host == "" || target.User != nil {
		return false
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return false
	}
	return origin.Scheme != "https" || target.Scheme == "https"
}

func reusableFile(path, rawURL, expectedMediaType, expectedDigest string, maxBytes int64) (Result, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, fmt.Errorf("checking existing media: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Result{}, false, fmt.Errorf("existing media path is not a regular file: %s", path)
	}
	if expectedDigest == "" {
		return Result{}, false, nil
	}
	if info.Size() > maxBytes {
		return Result{}, false, nil
	}
	if err := validateMediaFile(path, expectedMediaType); err != nil {
		return Result{}, false, nil
	}
	digest, err := fileSHA256(path, maxBytes)
	if err != nil {
		return Result{}, false, fmt.Errorf("hashing existing media: %w", err)
	}
	if digest != expectedDigest {
		return Result{}, false, nil
	}
	return Result{Path: path, URL: rawURL, SizeBytes: info.Size(), SHA256: digest, Reused: true}, true, nil
}

func normalizedSHA256(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return "", fmt.Errorf("expected SHA-256 must be 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("expected SHA-256 must be 64 lowercase hexadecimal characters")
	}
	return value, nil
}

func fileSHA256(path string, maxBytes int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	written, err := io.Copy(hasher, io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", err
	}
	if written > maxBytes {
		return "", fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func validateMediaFile(path, expectedMediaType string) error {
	if !strings.EqualFold(strings.TrimSpace(expectedMediaType), "application/pdf") {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	header := make([]byte, 5)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("reading PDF signature: %w", err)
	}
	if string(header) != "%PDF-" {
		return fmt.Errorf("response is not a PDF")
	}
	return nil
}

func (d *Downloader) byteLimit() int64 {
	if d != nil && d.MaxBytes > 0 {
		return d.MaxBytes
	}
	return defaultMaxBytes
}

func (d *Downloader) userAgent() string {
	if d != nil && strings.TrimSpace(d.UserAgent) != "" {
		return d.UserAgent
	}
	return "crosswalk/media-acquisition"
}

func (d *Downloader) httpClient() source.HTTPDoer {
	allowPrivate := d != nil && d.AllowPrivate
	if d != nil && d.HTTP != nil {
		if provided, ok := d.HTTP.(*http.Client); ok {
			client := source.CloneProtectedHTTPClient(provided, allowPrivate, false, source.RedirectHTTPS)
			if provided == nil || provided.Timeout <= 0 {
				client.Timeout = 5 * time.Minute
			}
			return client
		}
		return d.HTTP
	}
	client := source.NewProtectedHTTPClient(allowPrivate, false, source.RedirectHTTPS)
	client.Timeout = 5 * time.Minute
	return client
}
