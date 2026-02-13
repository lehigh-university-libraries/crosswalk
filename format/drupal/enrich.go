package drupal

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cacheDir     = ".crosswalk/cache"
	cacheVersion = "v1"
)

// Enricher fetches and caches entity references from a Drupal site.
type Enricher struct {
	BaseURL    string
	HTTPClient *http.Client
	CacheDir   string
	MaxDepth   int // Maximum recursion depth for nested references (default: 2)

	// Optional auth
	Username string
	Password string
}

// NewEnricher creates a new Enricher for the given Drupal site.
func NewEnricher(baseURL string) (*Enricher, error) {
	cacheDir, err := getCacheDir()
	if err != nil {
		return nil, err
	}

	// Normalize base URL
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &Enricher{
		BaseURL:  baseURL,
		CacheDir: cacheDir,
		MaxDepth: 2,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Enrich recursively enriches entity references in the given JSON data.
// It modifies the data in place, adding resolved entity data to references.
func (e *Enricher) Enrich(data []byte) ([]byte, error) {
	start := time.Now()
	slog.Debug("starting enrichment", "baseURL", e.BaseURL, "maxDepth", e.MaxDepth)

	var obj any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	enriched, err := e.enrichValue(obj, 0)
	if err != nil {
		return nil, err
	}

	slog.Debug("enrichment complete", "duration", time.Since(start))
	return json.MarshalIndent(enriched, "", "  ")
}

// EnrichEntity enriches a single entity map.
func (e *Enricher) EnrichEntity(entity map[string]any) (map[string]any, error) {
	enriched, err := e.enrichValue(entity, 0)
	if err != nil {
		return nil, err
	}
	return enriched.(map[string]any), nil
}

func (e *Enricher) enrichValue(v any, depth int) (any, error) {
	if depth > e.MaxDepth {
		return v, nil
	}

	switch val := v.(type) {
	case map[string]any:
		return e.enrichMap(val, depth)
	case []any:
		return e.enrichSlice(val, depth)
	default:
		return v, nil
	}
}

func (e *Enricher) enrichMap(m map[string]any, depth int) (map[string]any, error) {
	result := make(map[string]any)

	for k, v := range m {
		enriched, err := e.enrichValue(v, depth)
		if err != nil {
			return nil, err
		}
		result[k] = enriched
	}

	return result, nil
}

func (e *Enricher) enrichSlice(s []any, depth int) ([]any, error) {
	result := make([]any, len(s))

	for i, v := range s {
		// Check if this looks like an entity reference
		if ref, ok := v.(map[string]any); ok {
			if isEntityReference(ref) {
				enriched, err := e.enrichReference(ref, depth)
				if err != nil {
					// Log error but don't fail - just keep the original reference
					result[i] = ref
					continue
				}
				result[i] = enriched
				continue
			}
		}

		// Otherwise recurse
		enriched, err := e.enrichValue(v, depth)
		if err != nil {
			return nil, err
		}
		result[i] = enriched
	}

	return result, nil
}

// isEntityReference checks if a map looks like a Drupal entity reference.
func isEntityReference(m map[string]any) bool {
	_, hasTargetID := m["target_id"]
	_, hasTargetType := m["target_type"]
	return hasTargetID && hasTargetType
}

// enrichReference fetches the full entity data for a reference.
func (e *Enricher) enrichReference(ref map[string]any, depth int) (map[string]any, error) {
	targetID, ok := ref["target_id"].(float64)
	if !ok {
		return ref, nil
	}

	targetType, ok := ref["target_type"].(string)
	if !ok {
		return ref, nil
	}

	// Skip user entities - they typically contain sensitive data and are not needed for metadata
	if targetType == "user" {
		slog.Debug("skipping user entity", "targetID", int64(targetID))
		return ref, nil
	}

	// Build the entity URL
	entityURL := e.buildEntityURL(targetType, int64(targetID))
	if entityURL == "" {
		slog.Debug("skipping unknown entity type", "targetType", targetType, "targetID", int64(targetID))
		return ref, nil
	}

	slog.Debug("enriching reference", "targetType", targetType, "targetID", int64(targetID), "depth", depth)

	// Fetch the entity (with caching)
	entityData, err := e.fetchEntity(entityURL)
	if err != nil {
		slog.Debug("failed to fetch entity", "url", entityURL, "error", err)
		return ref, err
	}

	// Merge the reference with the fetched entity data
	result := make(map[string]any)

	// Start with original reference fields
	for k, v := range ref {
		result[k] = v
	}

	// Add fetched entity data under "_entity" key
	var entity map[string]any
	if err := json.Unmarshal(entityData, &entity); err != nil {
		return ref, nil
	}

	// Recursively enrich the fetched entity
	enrichedEntity, err := e.enrichValue(entity, depth+1)
	if err != nil {
		return ref, nil
	}

	result["_entity"] = enrichedEntity

	return result, nil
}

func (e *Enricher) buildEntityURL(targetType string, targetID int64) string {
	switch targetType {
	case "taxonomy_term":
		return fmt.Sprintf("%s/taxonomy/term/%d?_format=json", e.BaseURL, targetID)
	case "node":
		return fmt.Sprintf("%s/node/%d?_format=json", e.BaseURL, targetID)
	case "media":
		return fmt.Sprintf("%s/media/%d?_format=json", e.BaseURL, targetID)
	case "user":
		return fmt.Sprintf("%s/user/%d?_format=json", e.BaseURL, targetID)
	case "file":
		return fmt.Sprintf("%s/entity/file/%d?_format=json", e.BaseURL, targetID)
	default:
		// Unknown entity type
		return ""
	}
}

func (e *Enricher) fetchEntity(url string) ([]byte, error) {
	// Check cache first
	cached, status, found := e.loadFromCacheWithStatus(url)
	if found {
		slog.Debug("cache hit", "url", url, "cachedStatus", status)
		// Return error for cached non-2xx responses
		if status >= 400 {
			return nil, fmt.Errorf("fetching %s: cached status %d", url, status)
		}
		return cached, nil
	}

	// Fetch from network
	slog.Debug("cache miss, fetching from network", "url", url)
	start := time.Now()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	if e.Username != "" {
		req.SetBasicAuth(e.Username, e.Password)
	}

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		slog.Debug("network request failed", "url", url, "error", err, "duration", time.Since(start))
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	slog.Debug("network request complete", "url", url, "status", resp.StatusCode, "duration", time.Since(start))

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Cache 2xx-4xx responses to avoid retrying
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		if err := e.saveToCacheWithStatus(url, data, resp.StatusCode); err != nil {
			slog.Warn("failed to cache entity", "url", url, "error", err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: status %d", url, resp.StatusCode)
	}

	return data, nil
}

// Cache operations

func getCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}

	dir := filepath.Join(home, cacheDir, "drupal", cacheVersion)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	return dir, nil
}

func (e *Enricher) cacheKey(url string) string {
	// Include base URL in hash to separate caches per site
	hash := md5.Sum([]byte(url))
	return hex.EncodeToString(hash[:])
}

// cacheEntry wraps cached data with metadata including HTTP status.
type cacheEntry struct {
	Status int             `json:"status"`
	Data   json.RawMessage `json:"data"`
}

func (e *Enricher) loadFromCacheWithStatus(url string) (data []byte, status int, found bool) {
	key := e.cacheKey(url)
	cachePath := filepath.Join(e.CacheDir, key+".json")

	info, err := os.Stat(cachePath)
	if os.IsNotExist(err) {
		return nil, 0, false
	}
	if err != nil {
		return nil, 0, false
	}

	// Check if cache is expired (24 hours)
	if time.Since(info.ModTime()) > 24*time.Hour {
		os.Remove(cachePath)
		return nil, 0, false
	}

	fileData, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, 0, false
	}

	// Try to parse as new format with status
	var entry cacheEntry
	if err := json.Unmarshal(fileData, &entry); err == nil && entry.Status != 0 {
		return entry.Data, entry.Status, true
	}

	// Fall back to old format (raw data, assume 200)
	return fileData, 200, true
}

func (e *Enricher) saveToCacheWithStatus(url string, data []byte, status int) error {
	key := e.cacheKey(url)
	cachePath := filepath.Join(e.CacheDir, key+".json")

	entry := cacheEntry{
		Status: status,
		Data:   data,
	}

	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	return os.WriteFile(cachePath, encoded, 0644)
}

// ClearCache removes all cached entity data for this enricher's base URL.
func (e *Enricher) ClearCache() error {
	entries, err := os.ReadDir(e.CacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			os.Remove(filepath.Join(e.CacheDir, entry.Name()))
		}
	}

	return nil
}

// ClearAllCache removes all cached Drupal entity data.
func ClearAllCache() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	cacheDir := filepath.Join(home, cacheDir, "drupal")
	return os.RemoveAll(cacheDir)
}

// CachedEntity represents cached entity data with metadata.
type CachedEntity struct {
	URL       string          `json:"url"`
	CachedAt  time.Time       `json:"cached_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	Data      json.RawMessage `json:"data"`
}
