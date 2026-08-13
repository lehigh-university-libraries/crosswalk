package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultMaxBodyBytes int64 = 16 << 20
const defaultMaxOutputBytes int64 = 64 << 20
const defaultMaxRows = 100_000
const defaultMaxCells int64 = 1_000_000
const defaultMaxConcurrentRequests = 8
const defaultMaxArtifacts = 10_000
const defaultRequestTimeout = 90 * time.Second

var (
	// ErrAuthenticationNotConfigured prevents accidentally exposing Workbench
	// operations when neither supported authentication mechanism is configured.
	ErrAuthenticationNotConfigured = errors.New("HTTP API authentication is not configured")
	// ErrInvalidArtifact indicates that an engine returned an unsafe ZIP entry.
	ErrInvalidArtifact = errors.New("invalid transformation artifact")
	// ErrOutputTooLarge indicates that a transformation exceeded the configured
	// response bound. The error is intentionally stable so callers can report a
	// safe client-facing failure without exposing artifact contents.
	ErrOutputTooLarge = errors.New("transformation output exceeds configured limit")
)

// Config controls the HTTP API handler.
type Config struct {
	SharedSecret          string
	BearerVerifier        BearerVerifier
	MaxBodyBytes          int64
	MaxOutputBytes        int64
	MaxRows               int
	MaxCells              int64
	MaxConcurrentRequests int
	RequestTimeout        time.Duration
	Logger                *slog.Logger
}

// New constructs a Fabricator-compatible HTTP handler. Authentication is
// mandatory for both Workbench endpoints and intentionally cannot be disabled.
func New(engine Engine, cfg Config) (http.Handler, error) {
	if engine == nil {
		return nil, errors.New("HTTP API engine is required")
	}
	if strings.TrimSpace(cfg.SharedSecret) == "" && cfg.BearerVerifier == nil {
		return nil, ErrAuthenticationNotConfigured
	}
	if strings.TrimSpace(cfg.SharedSecret) == "" {
		cfg.SharedSecret = ""
	}
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	if cfg.MaxBodyBytes < 0 {
		return nil, errors.New("maximum body size must be positive")
	}
	if cfg.MaxOutputBytes == 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}
	if cfg.MaxOutputBytes < 0 {
		return nil, errors.New("maximum output size must be positive")
	}
	if cfg.MaxRows == 0 {
		cfg.MaxRows = defaultMaxRows
	}
	if cfg.MaxRows < 0 {
		return nil, errors.New("maximum row count must be positive")
	}
	if cfg.MaxCells == 0 {
		cfg.MaxCells = defaultMaxCells
	}
	if cfg.MaxCells < 0 {
		return nil, errors.New("maximum cell count must be positive")
	}
	if cfg.MaxConcurrentRequests == 0 {
		cfg.MaxConcurrentRequests = defaultMaxConcurrentRequests
	}
	if cfg.MaxConcurrentRequests < 0 {
		return nil, errors.New("maximum concurrent request count must be positive")
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.RequestTimeout < 0 {
		return nil, errors.New("request timeout must be positive")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	api := &handler{
		engine:    engine,
		config:    cfg,
		admission: make(chan struct{}, cfg.MaxConcurrentRequests),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(HealthcheckPath, api.healthcheck)
	mux.HandleFunc(CheckPath, api.authenticated(api.check))
	mux.HandleFunc(TransformPath, api.authenticated(api.transform))
	if _, ok := engine.(MatchEngine); ok {
		mux.HandleFunc(MatchesPath, api.authenticated(api.matches))
	}
	return mux, nil
}

type handler struct {
	engine    Engine
	config    Config
	admission chan struct{}
}

func (h *handler) healthcheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "OK\n")
}

func (h *handler) authenticated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), h.config.RequestTimeout)
		defer cancel()
		r = r.WithContext(ctx)

		sharedSecretAuthorized := h.sharedSecretAuthorized(r)
		bearerToken, bearerPresented := h.bearerToken(r)
		if !sharedSecretAuthorized && !bearerPresented {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("WWW-Authenticate", `Bearer realm="crosswalk"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Hold one admission slot across bearer verification and request
		// processing. Verifiers may perform expensive cryptographic or network
		// work, so running them before admission would bypass the configured
		// concurrency bound.
		select {
		case h.admission <- struct{}{}:
			defer func() { <-h.admission }()
		default:
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Server is busy", http.StatusServiceUnavailable)
			return
		}
		if !sharedSecretAuthorized && h.config.BearerVerifier.Verify(r.Context(), bearerToken) != nil {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("WWW-Authenticate", `Bearer realm="crosswalk"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (h *handler) sharedSecretAuthorized(r *http.Request) bool {
	if h.config.SharedSecret == "" {
		return false
	}
	configured := sha256.Sum256([]byte(h.config.SharedSecret))
	presented := sha256.Sum256([]byte(r.Header.Get("X-Secret")))
	return subtle.ConstantTimeCompare(configured[:], presented[:]) == 1
}

func (h *handler) bearerToken(r *http.Request) (string, bool) {
	if h.config.BearerVerifier == nil {
		return "", false
	}
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" || strings.Contains(token, " ") {
		return "", false
	}
	return token, true
}

func (h *handler) check(w http.ResponseWriter, r *http.Request) {
	if !hasMediaType(r, "application/json", "application/octet-stream") {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	body := http.MaxBytesReader(w, r.Body, h.config.MaxBodyBytes)
	defer body.Close()
	decoder := json.NewDecoder(body)
	rows, err := decodeStringTable(decoder, h.config.MaxRows, h.config.MaxCells)
	if errors.Is(err, errJSONArrayRequired) {
		http.Error(w, "request must contain a JSON array", http.StatusBadRequest)
		return
	}
	if err != nil {
		h.writeInputError(w, err, "invalid JSON request")
		return
	}

	result, err := h.engine.Check(r.Context(), rows)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	if result == nil {
		result = CheckResult{}
	}

	payload, err := json.Marshal(result)
	if err != nil {
		h.config.Logger.Error("encoding check response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if int64(len(payload)) > h.config.MaxOutputBytes {
		h.writeOutputLimitError(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(payload); err != nil {
		h.config.Logger.Error("writing check response", "error", err)
	}
}

func (h *handler) transform(w http.ResponseWriter, r *http.Request) {
	if !hasMediaType(r, "text/csv", "application/csv", "application/vnd.ms-excel", "application/octet-stream") {
		http.Error(w, "Content-Type must be text/csv", http.StatusUnsupportedMediaType)
		return
	}

	body := http.MaxBytesReader(w, r.Body, h.config.MaxBodyBytes)
	defer body.Close()
	input, err := io.ReadAll(body)
	if err != nil {
		h.writeInputError(w, err, "invalid CSV request")
		return
	}
	if len(input) == 0 {
		http.Error(w, "request body is empty", http.StatusBadRequest)
		return
	}
	if err := h.validateCSVSize(input); err != nil {
		h.writeInputError(w, err, "request table is too large")
		return
	}
	artifacts, err := h.engine.Transform(r.Context(), bytes.NewReader(input))
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	archive, err := buildArchiveWithLimit(artifacts, h.config.MaxOutputBytes)
	if err != nil {
		if errors.Is(err, ErrOutputTooLarge) {
			h.writeOutputLimitError(w)
			return
		}
		h.config.Logger.Error("building transformation archive", "error_type", fmt.Sprintf("%T", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="files.zip"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(archive); err != nil {
		h.config.Logger.Error("writing transformation response", "error", err)
	}
}

func (h *handler) matches(w http.ResponseWriter, r *http.Request) {
	engine, ok := h.engine.(MatchEngine)
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if !hasMediaType(r, "text/csv", "application/csv", "application/vnd.ms-excel", "application/octet-stream") {
		http.Error(w, "Content-Type must be text/csv", http.StatusUnsupportedMediaType)
		return
	}
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	body := http.MaxBytesReader(w, r.Body, h.config.MaxBodyBytes)
	defer body.Close()
	input, err := io.ReadAll(body)
	if err != nil {
		h.writeInputError(w, err, "invalid CSV request")
		return
	}
	if len(input) == 0 {
		http.Error(w, "request body is empty", http.StatusBadRequest)
		return
	}
	if err := h.validateCSVSize(input); err != nil {
		h.writeInputError(w, err, "request table is too large")
		return
	}
	result, err := engine.Matches(r.Context(), bytes.NewReader(input), mode)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	report, err := json.Marshal(result.Report)
	if err != nil {
		h.config.Logger.Error("encoding match report", "error_type", fmt.Sprintf("%T", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	archive, err := buildArchiveWithLimit([]Artifact{
		{Name: "matches.json", MediaType: "application/json", Data: append(report, '\n')},
		{Name: "matches.review.csv", MediaType: "text/csv", Data: result.ReviewCSV},
	}, h.config.MaxOutputBytes)
	if err != nil {
		if errors.Is(err, ErrOutputTooLarge) {
			h.writeOutputLimitError(w)
			return
		}
		h.config.Logger.Error("building match report archive", "error_type", fmt.Sprintf("%T", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="matches.zip"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(archive); err != nil {
		h.config.Logger.Error("writing match report response", "error_type", fmt.Sprintf("%T", err))
	}
}

func (h *handler) writeInputError(w http.ResponseWriter, err error, message string) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if errors.Is(err, errTableTooLarge) {
		http.Error(w, "Request table too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, message, http.StatusBadRequest)
}

func (h *handler) writeOutputLimitError(w http.ResponseWriter) {
	http.Error(w, "Transformation output too large", http.StatusUnprocessableEntity)
}

var (
	errJSONArrayRequired = errors.New("request must contain a JSON array")
	errTableTooLarge     = errors.New("request table exceeds configured limits")
)

// decodeStringTable reads a JSON array of string arrays without first
// allocating the complete untrusted document. Row and cell limits are applied
// before the next nested value is decoded.
func decodeStringTable(decoder *json.Decoder, maxRows int, maxCells int64) ([][]string, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token == nil {
		if err := requireEOF(decoder); err != nil {
			return nil, err
		}
		return nil, errJSONArrayRequired
	}
	opening, ok := token.(json.Delim)
	if !ok || opening != '[' {
		return nil, errors.New("request must be an array of string arrays")
	}

	rows := make([][]string, 0)
	var cells int64
	for decoder.More() {
		if len(rows) >= maxRows {
			return nil, errTableTooLarge
		}
		token, err = decoder.Token()
		if err != nil {
			return nil, err
		}
		opening, ok = token.(json.Delim)
		if !ok || opening != '[' {
			return nil, errors.New("request rows must be arrays")
		}

		row := make([]string, 0)
		for decoder.More() {
			if cells >= maxCells {
				return nil, errTableTooLarge
			}
			token, err = decoder.Token()
			if err != nil {
				return nil, err
			}
			value, ok := token.(string)
			if !ok {
				return nil, errors.New("request cells must be strings")
			}
			row = append(row, value)
			cells++
		}
		closing, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if delimiter, ok := closing.(json.Delim); !ok || delimiter != ']' {
			return nil, errors.New("invalid JSON row")
		}
		rows = append(rows, row)
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != ']' {
		return nil, errors.New("invalid JSON table")
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	return rows, nil
}

// validateCSVSize applies the configured per-request table bounds to CSV
// endpoints before the engine parses the document. Malformed CSV remains the
// engine's responsibility; this pass exists only to make operator-selected
// limits effective for check, transform, and matches alike.
func (h *handler) validateCSVSize(input []byte) error {
	reader := csv.NewReader(bytes.NewReader(input))
	reader.FieldsPerRecord = -1
	var rows int
	var cells int64
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return nil
		}
		if rows >= h.config.MaxRows || int64(len(row)) > h.config.MaxCells-cells {
			return errTableTooLarge
		}
		rows++
		cells += int64(len(row))
	}
}

func (h *handler) writeEngineError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		http.Error(w, "Request timed out", http.StatusGatewayTimeout)
		return
	}
	h.config.Logger.Error("HTTP API engine failed", "error_type", fmt.Sprintf("%T", err))
	http.Error(w, "Unable to process metadata", http.StatusUnprocessableEntity)
}

func hasMediaType(r *http.Request, allowed ...string) bool {
	raw := r.Header.Get("Content-Type")
	if raw == "" {
		// Fabricator's documented curl --upload-file clients do not set a
		// Content-Type. The decoder remains authoritative for these requests.
		return true
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	for _, candidate := range allowed {
		if strings.EqualFold(mediaType, candidate) {
			return true
		}
	}
	return false
}

func requireEOF(decoder *json.Decoder) error {
	_, err := decoder.Token()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func buildArchive(artifacts []Artifact) ([]byte, error) {
	return buildArchiveWithLimit(artifacts, defaultMaxOutputBytes)
}

func buildArchiveWithLimit(artifacts []Artifact, maxBytes int64) ([]byte, error) {
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("%w: no artifacts", ErrInvalidArtifact)
	}
	if len(artifacts) > defaultMaxArtifacts {
		return nil, fmt.Errorf("%w: too many artifacts", ErrOutputTooLarge)
	}
	if maxBytes <= 0 {
		return nil, errors.New("maximum archive size must be positive")
	}

	items := append([]Artifact(nil), artifacts...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.Name == "" || item.Name == "." || item.Name == ".." || path.Base(item.Name) != item.Name || strings.Contains(item.Name, `\`) {
			return nil, fmt.Errorf("%w: unsafe entry name", ErrInvalidArtifact)
		}
		if _, exists := seen[item.Name]; exists {
			return nil, fmt.Errorf("%w: duplicate entry name", ErrInvalidArtifact)
		}
		seen[item.Name] = struct{}{}
	}

	var total int64
	for _, item := range items {
		if int64(len(item.Data)) > maxBytes-total {
			return nil, ErrOutputTooLarge
		}
		total += int64(len(item.Data))
	}

	output := &boundedBuffer{maxBytes: maxBytes}
	zw := zip.NewWriter(output)
	for _, item := range items {
		header := &zip.FileHeader{Name: item.Name, Method: zip.Deflate}
		header.Modified = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
		header.SetMode(0o600)
		entry, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			return nil, fmt.Errorf("creating ZIP entry: %w", err)
		}
		if _, err := entry.Write(item.Data); err != nil {
			_ = zw.Close()
			return nil, fmt.Errorf("writing ZIP entry: %w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("closing ZIP archive: %w", err)
	}
	return append([]byte(nil), output.Bytes()...), nil
}

type boundedBuffer struct {
	bytes.Buffer
	maxBytes int64
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	remaining := b.maxBytes - int64(b.Len())
	if remaining <= 0 {
		return 0, ErrOutputTooLarge
	}
	if int64(len(data)) > remaining {
		written, _ := b.Buffer.Write(data[:remaining])
		return written, ErrOutputTooLarge
	}
	return b.Buffer.Write(data)
}
