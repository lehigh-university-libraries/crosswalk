package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/reconcile"
)

type engineStub struct {
	check     func(context.Context, [][]string) (CheckResult, error)
	transform func(context.Context, io.Reader) ([]Artifact, error)
}

type matchEngineStub struct {
	engineStub
	matches func(context.Context, io.Reader, string) (MatchResponse, error)
}

func (e matchEngineStub) Matches(ctx context.Context, input io.Reader, mode string) (MatchResponse, error) {
	if e.matches == nil {
		return MatchResponse{Report: reconcile.Report{Mode: reconcile.Mode(mode)}, ReviewCSV: []byte("verdict\nnew\n")}, nil
	}
	return e.matches(ctx, input, mode)
}

func (e engineStub) Check(ctx context.Context, rows [][]string) (CheckResult, error) {
	if e.check == nil {
		return CheckResult{}, nil
	}
	return e.check(ctx, rows)
}

func (e engineStub) Transform(ctx context.Context, input io.Reader) ([]Artifact, error) {
	if e.transform == nil {
		return []Artifact{{Name: "target.csv", MediaType: "text/csv", Data: []byte("title\n")}}, nil
	}
	return e.transform(ctx, input)
}

func newTestHandler(t *testing.T, engine Engine, cfg Config) http.Handler {
	t.Helper()
	if cfg.SharedSecret == "" && cfg.BearerVerifier == nil {
		cfg.SharedSecret = "test-secret"
	}
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := New(engine, cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func authorizedRequest(method, target, mediaType, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", mediaType)
	request.Header.Set("X-Secret", "test-secret")
	return request
}

func TestNewFailsClosedWithoutAuthentication(t *testing.T) {
	_, err := New(engineStub{}, Config{})
	if !errors.Is(err, ErrAuthenticationNotConfigured) {
		t.Fatalf("New() error = %v, want %v", err, ErrAuthenticationNotConfigured)
	}

	_, err = New(engineStub{}, Config{SharedSecret: " \t "})
	if !errors.Is(err, ErrAuthenticationNotConfigured) {
		t.Fatalf("New() whitespace-secret error = %v, want %v", err, ErrAuthenticationNotConfigured)
	}
}

func TestNewRejectsDisabledSafetyLimits(t *testing.T) {
	tests := map[string]Config{
		"body":        {MaxBodyBytes: -1},
		"output":      {MaxOutputBytes: -1},
		"rows":        {MaxRows: -1},
		"cells":       {MaxCells: -1},
		"concurrency": {MaxConcurrentRequests: -1},
		"timeout":     {RequestTimeout: -1},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			cfg.SharedSecret = "test-secret"
			if _, err := New(engineStub{}, cfg); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}

func TestHealthcheckIsGETAndDoesNotRequireAuthentication(t *testing.T) {
	handler := newTestHandler(t, engineStub{}, Config{})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, HealthcheckPath, nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "OK\n" {
		t.Fatalf("GET healthcheck = (%d, %q), want (200, %q)", recorder.Code, recorder.Body.String(), "OK\n")
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, HealthcheckPath, nil))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST healthcheck = %d Allow=%q, want 405 Allow=GET", recorder.Code, recorder.Header().Get("Allow"))
	}
}

func TestWorkbenchEndpointsAreAuthenticatedPOSTOnly(t *testing.T) {
	handler := newTestHandler(t, engineStub{}, Config{})
	for _, target := range []string{CheckPath, TransformPath} {
		t.Run(target+" method", func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
			if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodPost {
				t.Fatalf("GET %s = %d Allow=%q, want 405 Allow=POST", target, recorder.Code, recorder.Header().Get("Allow"))
			}
		})
		t.Run(target+" authentication", func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, target, nil)
			request.Header.Set("X-Secret", "wrong")
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated POST %s = %d, want 401", target, recorder.Code)
			}
		})
	}
}

func TestMatchesEndpointReturnsDeterministicReviewArchive(t *testing.T) {
	handler := newTestHandler(t, matchEngineStub{matches: func(_ context.Context, input io.Reader, mode string) (MatchResponse, error) {
		data, err := io.ReadAll(input)
		if err != nil {
			return MatchResponse{}, err
		}
		if string(data) != "title\nExample\n" || mode != "skip" {
			return MatchResponse{}, fmt.Errorf("unexpected input %q mode %q", data, mode)
		}
		return MatchResponse{Report: reconcile.Report{Mode: reconcile.Mode(mode), Summary: reconcile.Summary{Total: 1}}, ReviewCSV: []byte("verdict\nnew\n")}, nil
	}}, Config{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, MatchesPath+"?mode=skip", "text/csv", "title\nExample\n"))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("matches = %d %q", recorder.Code, recorder.Body.String())
	}
	contents := readArchive(t, recorder.Body.Bytes())
	if string(contents["matches.review.csv"]) != "verdict\nnew\n" || !strings.Contains(string(contents["matches.json"]), `"mode":"skip"`) {
		t.Fatalf("matches archive = %#v", contents)
	}
}

func TestMatchesEndpointIsAbsentWithoutFinderCapability(t *testing.T) {
	handler := newTestHandler(t, engineStub{}, Config{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, MatchesPath, "text/csv", "title\nExample\n"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("matches status = %d, want 404", recorder.Code)
	}
}

type verifierFunc func(context.Context, string) error

func (f verifierFunc) Verify(ctx context.Context, token string) error { return f(ctx, token) }

type failAfterReader struct {
	prefix   *strings.Reader
	readPast bool
}

func (r *failAfterReader) Read(data []byte) (int, error) {
	if r.prefix.Len() == 0 {
		r.readPast = true
		return 0, errors.New("read beyond bounded JSON prefix")
	}
	return r.prefix.Read(data)
}

func TestBearerVerifierIsInjected(t *testing.T) {
	var received string
	handler := newTestHandler(t, engineStub{}, Config{
		BearerVerifier: verifierFunc(func(_ context.Context, token string) error {
			received = token
			if token != "valid-token" {
				return errors.New("invalid")
			}
			return nil
		}),
	})

	request := httptest.NewRequest(http.MethodPost, CheckPath, strings.NewReader("[]"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer valid-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || received != "valid-token" {
		t.Fatalf("bearer request = %d token=%q, want 200 valid-token", recorder.Code, received)
	}
}

func TestBearerVerificationUsesRequestAdmission(t *testing.T) {
	verificationStarted := make(chan struct{}, 2)
	releaseVerification := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseVerification) }) }
	defer release()

	handler := newTestHandler(t, engineStub{}, Config{
		BearerVerifier: verifierFunc(func(context.Context, string) error {
			verificationStarted <- struct{}{}
			<-releaseVerification
			return nil
		}),
		MaxConcurrentRequests: 1,
	})
	bearerRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, CheckPath, strings.NewReader("[]"))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer valid-token")
		return request
	}

	first := httptest.NewRecorder()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		handler.ServeHTTP(first, bearerRequest())
	}()
	<-verificationStarted

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, bearerRequest())
	if second.Code != http.StatusServiceUnavailable || second.Header().Get("Retry-After") != "1" {
		t.Fatalf("concurrent bearer request = %d Retry-After=%q, want 503 and 1", second.Code, second.Header().Get("Retry-After"))
	}
	select {
	case <-verificationStarted:
		t.Fatal("second bearer verifier ran outside the request admission bound")
	default:
	}

	release()
	<-firstDone
	if first.Code != http.StatusOK {
		t.Fatalf("admitted bearer request = %d body=%q, want 200", first.Code, first.Body.String())
	}
}

func TestCheckPreservesResponseContract(t *testing.T) {
	handler := newTestHandler(t, engineStub{check: func(_ context.Context, rows [][]string) (CheckResult, error) {
		if len(rows) != 2 || rows[1][0] != "" {
			return nil, fmt.Errorf("unexpected rows: %#v", rows)
		}
		return CheckResult{"A2": "Missing value"}, nil
	}}, Config{})

	request := authorizedRequest(http.MethodPost, CheckPath, "application/json; charset=utf-8", `[["Title"],[""]]`)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("check status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	var result CheckResult
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if result["A2"] != "Missing value" {
		t.Fatalf("response = %#v", result)
	}
}

func TestCheckSuccessPreservesFabricatorEmptyObjectBytes(t *testing.T) {
	handler := newTestHandler(t, engineStub{check: func(context.Context, [][]string) (CheckResult, error) {
		return CheckResult{}, nil
	}}, Config{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, CheckPath, "application/json", "[]"))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "{}" {
		t.Fatalf("empty check response = (%d, %q), want (200, %q)", recorder.Code, recorder.Body.String(), "{}")
	}
}

func TestBodyLimitAppliesToBothEndpoints(t *testing.T) {
	readingEngine := engineStub{transform: func(_ context.Context, input io.Reader) ([]Artifact, error) {
		_, err := io.ReadAll(input)
		return nil, err
	}}
	handler := newTestHandler(t, readingEngine, Config{MaxBodyBytes: 8})

	tests := []struct {
		path      string
		mediaType string
		body      string
	}{
		{path: CheckPath, mediaType: "application/json", body: `[["too large"]]`},
		{path: TransformPath, mediaType: "text/csv", body: "title\na metadata value\n"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, test.path, test.mediaType, test.body))
			if recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("oversized %s = %d body=%q, want 413", test.path, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestDecodedTableLimitsApplyBeforeEngine(t *testing.T) {
	called := false
	handler := newTestHandler(t, engineStub{check: func(context.Context, [][]string) (CheckResult, error) {
		called = true
		return CheckResult{}, nil
	}}, Config{MaxRows: 2, MaxCells: 3})

	for _, body := range []string{
		`[["a"],["b"],["c"]]`,
		`[["a","b"],["c","d"]]`,
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, CheckPath, "application/json", body))
		if recorder.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized table status = %d body=%q, want 413", recorder.Code, recorder.Body.String())
		}
	}
	if called {
		t.Fatal("engine was called for an oversized decoded table")
	}
}

func TestStringTableDecoderStopsAtConfiguredLimits(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		maxRows  int
		maxCells int64
	}{
		{name: "rows", prefix: `[["kept"],[`, maxRows: 1, maxCells: 10},
		{name: "cells", prefix: `[["kept","`, maxRows: 10, maxCells: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := &failAfterReader{prefix: strings.NewReader(test.prefix)}
			_, err := decodeStringTable(json.NewDecoder(input), test.maxRows, test.maxCells)
			if !errors.Is(err, errTableTooLarge) {
				t.Fatalf("decodeStringTable() error = %v, want errTableTooLarge", err)
			}
			if input.readPast {
				t.Fatal("decoder read beyond the prefix needed to identify an oversized table")
			}
		})
	}
}

func TestCheckStrictlyRequiresStringCells(t *testing.T) {
	called := false
	handler := newTestHandler(t, engineStub{check: func(context.Context, [][]string) (CheckResult, error) {
		called = true
		return CheckResult{}, nil
	}}, Config{})

	for _, body := range []string{
		`{"rows":[]}`,
		`[null]`,
		`[["a",null]]`,
		`[["a",1]]`,
		`[["a",true]]`,
		`[["a",["nested"]]]`,
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, CheckPath, "application/json", body))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("non-string table %s = %d body=%q, want 400", body, recorder.Code, recorder.Body.String())
		}
	}
	if called {
		t.Fatal("engine was called for an invalid JSON string table")
	}
}

func TestCheckNullPreservesArrayRequiredResponse(t *testing.T) {
	handler := newTestHandler(t, engineStub{}, Config{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, CheckPath, "application/json", "null"))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "request must contain a JSON array") {
		t.Fatalf("null table = %d body=%q, want array-required 400", recorder.Code, recorder.Body.String())
	}
}

func TestCSVTableLimitsApplyBeforeTransformAndMatches(t *testing.T) {
	transformCalled := false
	matchCalled := false
	engine := matchEngineStub{
		engineStub: engineStub{transform: func(context.Context, io.Reader) ([]Artifact, error) {
			transformCalled = true
			return nil, errors.New("unexpected transform")
		}},
		matches: func(context.Context, io.Reader, string) (MatchResponse, error) {
			matchCalled = true
			return MatchResponse{}, errors.New("unexpected matches")
		},
	}
	handler := newTestHandler(t, engine, Config{MaxRows: 2, MaxCells: 3})
	tests := []struct {
		path string
		body string
	}{
		{path: TransformPath, body: "title\none\ntwo\n"},
		{path: MatchesPath, body: "title,author\none,two\n"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, test.path, "text/csv", test.body))
		if recorder.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("oversized CSV %s = %d body=%q, want 413", test.path, recorder.Code, recorder.Body.String())
		}
	}
	if transformCalled || matchCalled {
		t.Fatalf("engine called for oversized CSV: transform=%t matches=%t", transformCalled, matchCalled)
	}
}

func TestOutputLimitRejectsAmplifyingTransformation(t *testing.T) {
	handler := newTestHandler(t, engineStub{transform: func(context.Context, io.Reader) ([]Artifact, error) {
		return []Artifact{{Name: "target.csv", Data: bytes.Repeat([]byte("x"), 129)}}, nil
	}}, Config{MaxOutputBytes: 128})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, TransformPath, "text/csv", "title\nExample\n"))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "output too large") {
		t.Fatalf("oversized output = %d body=%q, want 422", recorder.Code, recorder.Body.String())
	}
}

func TestConcurrentRequestAdmissionIsBounded(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	handler := newTestHandler(t, engineStub{check: func(context.Context, [][]string) (CheckResult, error) {
		close(started)
		<-release
		return CheckResult{}, nil
	}}, Config{MaxConcurrentRequests: 1})

	first := httptest.NewRecorder()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		handler.ServeHTTP(first, authorizedRequest(http.MethodPost, CheckPath, "application/json", "[]"))
	}()
	<-started

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, authorizedRequest(http.MethodPost, CheckPath, "application/json", "[]"))
	if second.Code != http.StatusServiceUnavailable || second.Header().Get("Retry-After") != "1" {
		t.Fatalf("concurrent request = %d Retry-After=%q, want 503 and 1", second.Code, second.Header().Get("Retry-After"))
	}
	close(release)
	<-firstDone
	if first.Code != http.StatusOK {
		t.Fatalf("admitted request = %d, want 200", first.Code)
	}
}

func TestRequestTimeoutCancelsEngine(t *testing.T) {
	engine := engineStub{check: func(ctx context.Context, _ [][]string) (CheckResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	handler := newTestHandler(t, engine, Config{RequestTimeout: 10 * time.Millisecond})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, CheckPath, "application/json", "[]"))
	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("timed-out request = %d body=%q, want 504", recorder.Code, recorder.Body.String())
	}
}

func TestTransformCreatesDeterministicArchive(t *testing.T) {
	engine := engineStub{transform: func(_ context.Context, input io.Reader) ([]Artifact, error) {
		data, err := io.ReadAll(input)
		if err != nil {
			return nil, err
		}
		return []Artifact{
			{Name: "target.csv", MediaType: "text/csv", Data: data},
			{Name: "agents.csv", MediaType: "text/csv", Data: []byte("name\n")},
		}, nil
	}}
	handler := newTestHandler(t, engine, Config{})

	var archives [][]byte
	for range 2 {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, TransformPath, "text/csv", "title\nExample\n"))
		if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/zip" {
			t.Fatalf("transform = %d Content-Type=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
		}
		archives = append(archives, append([]byte(nil), recorder.Body.Bytes()...))
	}
	if !bytes.Equal(archives[0], archives[1]) {
		t.Fatal("identical transformations produced different ZIP bytes")
	}

	contents := readArchive(t, archives[0])
	if string(contents["target.csv"]) != "title\nExample\n" || string(contents["agents.csv"]) != "name\n" {
		t.Fatalf("archive contents = %#v", contents)
	}
}

func TestTransformRequestsDoNotShareState(t *testing.T) {
	const requests = 64
	engine := engineStub{transform: func(_ context.Context, input io.Reader) ([]Artifact, error) {
		data, err := io.ReadAll(input)
		if err != nil {
			return nil, err
		}
		return []Artifact{{Name: "target.csv", MediaType: "text/csv", Data: data}}, nil
	}}
	handler := newTestHandler(t, engine, Config{MaxConcurrentRequests: requests})
	var wait sync.WaitGroup
	errorsCh := make(chan error, requests)
	for i := range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			body := fmt.Sprintf("title\nrecord-%d\n", i)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, TransformPath, "text/csv", body))
			if recorder.Code != http.StatusOK {
				errorsCh <- fmt.Errorf("request %d status = %d", i, recorder.Code)
				return
			}
			contents, err := archiveContents(recorder.Body.Bytes())
			if err != nil {
				errorsCh <- fmt.Errorf("request %d: %w", i, err)
				return
			}
			if string(contents["target.csv"]) != body {
				errorsCh <- fmt.Errorf("request %d got %q, want %q", i, contents["target.csv"], body)
			}
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
}

func TestEngineErrorsAreNotExposed(t *testing.T) {
	handler := newTestHandler(t, engineStub{check: func(context.Context, [][]string) (CheckResult, error) {
		return nil, errors.New("database password is hunter2")
	}}, Config{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, CheckPath, "application/json", "[]"))
	if recorder.Code != http.StatusUnprocessableEntity || strings.Contains(recorder.Body.String(), "hunter2") {
		t.Fatalf("engine error response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestArchiveRejectsUnsafeOrDuplicateNames(t *testing.T) {
	tests := [][]Artifact{
		{{Name: "../target.csv", Data: []byte("x")}},
		{{Name: "..", Data: []byte("x")}},
		{{Name: `folder\\target.csv`, Data: []byte("x")}},
		{{Name: "target.csv"}, {Name: "target.csv"}},
	}
	for _, artifacts := range tests {
		if _, err := buildArchive(artifacts); !errors.Is(err, ErrInvalidArtifact) {
			t.Fatalf("buildArchive(%#v) error = %v, want ErrInvalidArtifact", artifacts, err)
		}
	}
}

func TestRejectsUnsupportedMediaTypesAndTrailingJSON(t *testing.T) {
	handler := newTestHandler(t, engineStub{}, Config{})
	tests := []struct {
		path      string
		mediaType string
		body      string
		status    int
	}{
		{CheckPath, "text/plain", "[]", http.StatusUnsupportedMediaType},
		{TransformPath, "application/json", "[]", http.StatusUnsupportedMediaType},
		{CheckPath, "application/json", "[] []", http.StatusBadRequest},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, test.path, test.mediaType, test.body))
		if recorder.Code != test.status {
			t.Errorf("%s %s = %d, want %d", test.path, test.mediaType, recorder.Code, test.status)
		}
	}
}

func TestDocumentedUploadFileRequestsMayOmitContentType(t *testing.T) {
	handler := newTestHandler(t, engineStub{}, Config{})
	tests := []struct {
		path string
		body string
	}{
		{CheckPath, "[]"},
		{TransformPath, "Title\nExample\n"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		request := authorizedRequest(http.MethodPost, test.path, "", test.body)
		request.Header.Del("Content-Type")
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("POST %s without Content-Type = %d body=%q, want 200", test.path, recorder.Code, recorder.Body.String())
		}
	}
}

func readArchive(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	contents, err := archiveContents(data)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func archiveContents(data []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	contents := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(entry)
		closeErr := entry.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		contents[file.Name] = data
	}
	return contents, nil
}
