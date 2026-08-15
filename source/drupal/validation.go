package drupal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/internal/strictjson"
	"github.com/lehigh-university-libraries/crosswalk/model"
	acquisition "github.com/lehigh-university-libraries/crosswalk/source"
	"github.com/lehigh-university-libraries/crosswalk/validationcontext"
)

const (
	maxValidationResponseBytes = int64(1 << 20)
	maxValidationBundles       = 64
	maxValidationViewResults   = 100
	maxValidationAliasResults  = 2
)

var (
	_ validationcontext.NodeExistenceResolver        = (*Client)(nil)
	_ validationcontext.EntityReferenceResolver      = (*Client)(nil)
	_ validationcontext.AllowedValueResolver         = (*Client)(nil)
	_ validationcontext.URLAliasAvailabilityResolver = (*Client)(nil)
)

// ConfigureValidationModel binds deployment-aware validation to the exact
// immutable model and compiled profile already selected for reconciliation.
// Configure the client before sharing it with concurrent callers.
func (client *Client) ConfigureValidationModel(snapshot *model.Snapshot) error {
	if client == nil {
		return errors.New("configuring Drupal validation: client is nil")
	}
	if client.SystemProfile == nil {
		return errors.New("configuring Drupal validation: compiled profile is required")
	}
	canonical, err := snapshot.Canonical()
	if err != nil {
		return fmt.Errorf("configuring Drupal validation model: %w", err)
	}
	if canonical.System != "drupal" {
		return fmt.Errorf("configuring Drupal validation: model system %q is not drupal", canonical.System)
	}
	if client.SystemProfile.System() != "drupal" {
		return fmt.Errorf("configuring Drupal validation: profile system %q is not drupal", client.SystemProfile.System())
	}
	if client.SystemProfile.ModelFingerprint() != canonical.Fingerprint.Value {
		return errors.New("configuring Drupal validation: profile and model fingerprints do not match")
	}
	repository := client.SystemProfile.LookupPlan().Repository
	if _, ok := canonical.Entity(repository.EntityType, repository.Bundle); !ok {
		return errors.New("configuring Drupal validation: profile repository entity is absent from the model")
	}
	if _, _, err := client.validationEndpoints(); err != nil {
		return err
	}
	client.validationModel = canonical
	return nil
}

// NodeExists reports whether the numeric node identifier exists in the
// profile's repository bundle. The identifier is always an encoded filter
// value on the operator-configured JSON:API origin.
func (client *Client) NodeExists(ctx context.Context, nodeID uint64) (bool, error) {
	if ctx == nil {
		return false, errors.New("validating Drupal node: context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("validating Drupal node: %w", err)
	}
	ctx = validationcontext.EnsureRequestBudget(ctx)
	snapshot, err := client.configuredValidationModel()
	if err != nil {
		return false, err
	}
	if nodeID == 0 {
		return false, nil
	}
	repository := client.SystemProfile.LookupPlan().Repository
	if repository.EntityType != "node" {
		return false, errors.New("validating Drupal node: profile repository entity is not a node")
	}
	if _, ok := snapshot.Entity(repository.EntityType, repository.Bundle); !ok {
		return false, errors.New("validating Drupal node: repository bundle is absent from the model")
	}
	return client.jsonAPIEntityExists(ctx, repository.EntityType, repository.Bundle, "drupal_internal__nid", strconv.FormatUint(nodeID, 10))
}

// EntityReferenceExists validates an inert, parsed reference against the
// exact target type, bundles, field, source type, and handler in the sealed
// model. It never requests query.Value as a URL.
func (client *Client) EntityReferenceExists(ctx context.Context, query validationcontext.EntityReferenceQuery) (bool, error) {
	if ctx == nil {
		return false, errors.New("validating Drupal entity reference: context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("validating Drupal entity reference: %w", err)
	}
	ctx = validationcontext.EnsureRequestBudget(ctx)
	if len(query.Value) > 4096 || strings.ContainsAny(query.Value, "\x00\r\n") {
		return false, errors.New("validating Drupal entity reference: value is invalid")
	}
	snapshot, err := client.configuredValidationModel()
	if err != nil {
		return false, err
	}
	expectedHandler := "default:" + query.EntityType
	if query.Handler != expectedHandler {
		return false, fmt.Errorf("entity-reference handler %q requires a site-specific fixed adapter; only %q is supported: %w",
			query.Handler, expectedHandler, &validationcontext.ErrCapabilityUnavailable{Capability: validationcontext.CapabilityEntityReference})
	}
	_, bundles, err := client.validatedReferenceQuery(snapshot, query)
	if err != nil {
		return false, err
	}

	switch query.Kind {
	case validationcontext.EntityReferenceID:
		if !decimalIdentifier(query.Value) {
			return false, nil
		}
		attribute, ok := drupalInternalIDAttribute(query.EntityType)
		if !ok {
			return false, fmt.Errorf("validating Drupal entity reference: entity type %q has no supported numeric identifier", query.EntityType)
		}
		for _, bundle := range bundles {
			exists, lookupErr := client.jsonAPIEntityExists(ctx, query.EntityType, bundle, attribute, query.Value)
			if lookupErr != nil {
				return false, lookupErr
			}
			if exists {
				return true, nil
			}
		}
		return false, nil
	case validationcontext.EntityReferenceName:
		if query.EntityType != "taxonomy_term" || strings.TrimSpace(query.Value) == "" {
			return false, nil
		}
		for _, bundle := range bundles {
			exists, lookupErr := client.jsonAPIEntityExists(ctx, query.EntityType, bundle, "name", query.Value)
			if lookupErr != nil {
				return false, lookupErr
			}
			if exists {
				return true, nil
			}
		}
		return false, nil
	case validationcontext.EntityReferenceURI:
		if query.EntityType != "taxonomy_term" {
			return false, nil
		}
		return client.taxonomyURIExists(ctx, query.Value, bundles)
	default:
		return false, fmt.Errorf("validating Drupal entity reference: unsupported reference kind %q", query.Kind)
	}
}

// AllowedValue verifies that a dynamic provider name and row context are
// bound to the selected model/profile. Crosswalk deliberately does not execute
// a Drupal PHP callback name locally. A future site adapter can implement a
// fixed authenticated endpoint for this capability; until then it fails
// explicitly instead of accepting unchecked values.
func (client *Client) AllowedValue(ctx context.Context, query validationcontext.AllowedValueQuery) (bool, error) {
	if ctx == nil {
		return false, errors.New("validating Drupal allowed value: context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("validating Drupal allowed value: %w", err)
	}
	snapshot, err := client.configuredValidationModel()
	if err != nil {
		return false, err
	}
	repository := client.SystemProfile.LookupPlan().Repository
	if query.ProfileFingerprint != client.SystemProfile.Fingerprint() || query.ModelFingerprint != snapshot.Fingerprint.Value {
		return false, errors.New("validating Drupal allowed value: query fingerprints do not match the selected profile")
	}
	if query.Bundle != repository.Bundle {
		return false, errors.New("validating Drupal allowed value: query bundle does not match the selected repository")
	}
	fieldPath := baseDrupalFieldPath(query.Field)
	field, ok := snapshot.Field(repository.EntityType, repository.Bundle, fieldPath)
	if !ok || field.SourceType != query.SourceType {
		return false, errors.New("validating Drupal allowed value: query field does not match the selected model")
	}
	provider := configuredAllowedValuesProvider(field)
	if provider == "" || provider != query.Provider {
		return false, errors.New("validating Drupal allowed value: provider does not match the selected model")
	}
	return false, fmt.Errorf("dynamic allowed-values provider is not exposed by a fixed Drupal validation endpoint: %w", &validationcontext.ErrCapabilityUnavailable{Capability: validationcontext.CapabilityAllowedValue})
}

// URLAliasAvailable checks an inert, canonical alias through Drupal's fixed
// path_alias JSON:API collection. Candidate input is encoded only as an exact
// filter value and never becomes a request path or destination.
func (client *Client) URLAliasAvailable(ctx context.Context, alias string) (bool, error) {
	if ctx == nil {
		return false, errors.New("validating Drupal URL alias: context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("validating Drupal URL alias: %w", err)
	}
	ctx = validationcontext.EnsureRequestBudget(ctx)
	if _, err := client.configuredValidationModel(); err != nil {
		return false, err
	}
	alias = strings.TrimSpace(alias)
	if len(alias) > 2048 || !strings.HasPrefix(alias, "/") || strings.HasPrefix(alias, "//") || strings.ContainsAny(alias, "\x00\r\n\\") {
		return false, errors.New("validating Drupal URL alias: alias is invalid")
	}
	parsed, err := url.ParseRequestURI(alias)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.Path != alias {
		return false, errors.New("validating Drupal URL alias: alias must be an unencoded absolute path")
	}
	if path.Clean(alias) != alias {
		return false, errors.New("validating Drupal URL alias: alias path must be canonical")
	}
	endpoint, err := client.validationJSONAPIResource("path_alias", "path_alias")
	if err != nil {
		return false, err
	}
	query := endpoint.Query()
	prefix := "filter[crosswalk-context][condition]"
	query.Set(prefix+"[path]", "alias")
	query.Set(prefix+"[operator]", "=")
	query.Set(prefix+"[value]", alias)
	query.Set("page[limit]", strconv.Itoa(maxValidationAliasResults))
	endpoint.RawQuery = query.Encode()
	data, err := client.fetchValidationJSON(ctx, endpoint, "application/vnd.api+json")
	if err != nil {
		return false, fmt.Errorf("validating Drupal URL alias availability: %w", err)
	}
	records, err := validationAliasRecords(data, alias)
	if err != nil {
		return false, fmt.Errorf("validating Drupal URL alias availability: %w", err)
	}
	return len(records) == 0, nil
}

func validationAliasRecords(data []byte, alias string) ([]string, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parsing path-alias response: %w", err)
	}
	if len(envelope.Data) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Data), []byte("null")) {
		return nil, errors.New("path-alias response has no data collection")
	}
	var records []struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			Alias    *string `json:"alias"`
			Path     *string `json:"path"`
			Langcode *string `json:"langcode"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(envelope.Data, &records); err != nil {
		return nil, fmt.Errorf("parsing path-alias data: %w", err)
	}
	if len(records) > maxValidationAliasResults {
		return nil, fmt.Errorf("path-alias response exceeds %d results", maxValidationAliasResults)
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		if record.Type != "path_alias--path_alias" || strings.TrimSpace(record.ID) == "" || record.Attributes.Alias == nil ||
			record.Attributes.Path == nil || record.Attributes.Langcode == nil {
			return nil, errors.New("path-alias response contains an incomplete resource")
		}
		if *record.Attributes.Alias != alias {
			return nil, errors.New("path-alias response does not match the requested alias")
		}
		if strings.TrimSpace(*record.Attributes.Path) == "" || strings.TrimSpace(*record.Attributes.Langcode) == "" ||
			strings.ContainsAny(*record.Attributes.Path+*record.Attributes.Langcode, "\x00\r\n") {
			return nil, errors.New("path-alias response contains invalid attributes")
		}
		ids = append(ids, record.ID)
	}
	return ids, nil
}

func (client *Client) configuredValidationModel() (*model.Snapshot, error) {
	if client == nil || client.validationModel == nil || client.SystemProfile == nil {
		return nil, errors.New("drupal validation model is not configured")
	}
	if client.SystemProfile.ModelFingerprint() != client.validationModel.Fingerprint.Value {
		return nil, errors.New("drupal validation profile no longer matches its configured model")
	}
	return client.validationModel, nil
}

func (client *Client) validatedReferenceQuery(snapshot *model.Snapshot, query validationcontext.EntityReferenceQuery) (model.Field, []string, error) {
	repository := client.SystemProfile.LookupPlan().Repository
	fieldPath := baseDrupalFieldPath(query.Field)
	field, ok := snapshot.Field(repository.EntityType, repository.Bundle, fieldPath)
	if !ok || field.SourceType != query.SourceType || field.Reference == nil {
		return model.Field{}, nil, errors.New("validating Drupal entity reference: query field does not match the selected model")
	}
	if field.Reference.EntityType != query.EntityType {
		return model.Field{}, nil, errors.New("validating Drupal entity reference: query target type does not match the selected model")
	}
	configuredHandler := settingString(field.InstanceSettings["handler"])
	if configuredHandler == "" {
		configuredHandler = "default:" + field.Reference.EntityType
	}
	if configuredHandler != strings.TrimSpace(query.Handler) {
		return model.Field{}, nil, errors.New("validating Drupal entity reference: query handler does not match the selected model")
	}
	if len(query.Bundles) > maxValidationBundles {
		return model.Field{}, nil, fmt.Errorf("validating Drupal entity reference: target bundles exceed %d", maxValidationBundles)
	}
	seenQueryBundles := make(map[string]struct{}, len(query.Bundles))
	for _, bundle := range query.Bundles {
		if bundle != strings.TrimSpace(bundle) || !resourceSegmentPattern.MatchString(bundle) {
			return model.Field{}, nil, errors.New("validating Drupal entity reference: query contains an invalid bundle")
		}
		if _, exists := seenQueryBundles[bundle]; exists {
			return model.Field{}, nil, errors.New("validating Drupal entity reference: query repeats a bundle")
		}
		seenQueryBundles[bundle] = struct{}{}
	}
	modelBundles := canonicalStrings(field.Reference.Bundles)
	queryBundles := canonicalStrings(query.Bundles)
	if len(modelBundles) != 0 {
		if len(queryBundles) == 0 {
			return model.Field{}, nil, errors.New("validating Drupal entity reference: query omits the selected model bundles")
		}
		for _, bundle := range queryBundles {
			if !containsString(modelBundles, bundle) {
				return model.Field{}, nil, errors.New("validating Drupal entity reference: query bundles do not match the selected model")
			}
		}
	} else if len(queryBundles) != 0 {
		return model.Field{}, nil, errors.New("validating Drupal entity reference: query narrows a model reference without declared bundles")
	}
	bundles := queryBundles
	if len(bundles) == 0 {
		// Drupal's core user entity has the JSON:API resource identity
		// user--user even though config/sync normally has no bundle config from
		// which the portable model could synthesize a separate user entity.
		if query.EntityType == "user" {
			bundles = []string{"user"}
		}
	}
	if len(bundles) == 0 {
		for _, entity := range snapshot.Entities {
			if entity.EntityType == query.EntityType {
				bundles = append(bundles, entity.Bundle)
			}
		}
		bundles = canonicalStrings(bundles)
	}
	if len(bundles) == 0 {
		return model.Field{}, nil, errors.New("validating Drupal entity reference: selected model has no target bundles")
	}
	if len(bundles) > maxValidationBundles {
		return model.Field{}, nil, fmt.Errorf("validating Drupal entity reference: target bundles exceed %d", maxValidationBundles)
	}
	for _, bundle := range bundles {
		if query.EntityType == "user" && bundle == "user" {
			continue
		}
		if _, ok := snapshot.Entity(query.EntityType, bundle); !ok {
			return model.Field{}, nil, errors.New("validating Drupal entity reference: target bundle is absent from the selected model")
		}
	}
	return field, bundles, nil
}

func (client *Client) jsonAPIEntityExists(ctx context.Context, entityType, bundle, attribute, value string) (bool, error) {
	endpoint, err := client.validationJSONAPIResource(entityType, bundle)
	if err != nil {
		return false, err
	}
	query := endpoint.Query()
	prefix := "filter[crosswalk-context][condition]"
	query.Set(prefix+"[path]", attribute)
	query.Set(prefix+"[operator]", "=")
	query.Set(prefix+"[value]", value)
	query.Set("page[limit]", "1")
	endpoint.RawQuery = query.Encode()
	data, err := client.fetchValidationJSON(ctx, endpoint, "application/vnd.api+json")
	if err != nil {
		return false, fmt.Errorf("validating Drupal entity existence: %w", err)
	}
	exists, err := validationEntityExistsResponse(data, entityType, bundle, attribute, value)
	if err != nil {
		return false, fmt.Errorf("validating Drupal entity existence: %w", err)
	}
	return exists, nil
}

func validationEntityExistsResponse(data []byte, entityType, bundle, attribute, value string) (bool, error) {
	if err := strictjson.RejectDuplicateNames(data); err != nil {
		return false, fmt.Errorf("invalid JSON response: %w", err)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return false, fmt.Errorf("parsing response: %w", err)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return false, errors.New("response has no data collection")
	}
	var records []struct {
		Type       string                     `json:"type"`
		ID         string                     `json:"id"`
		Attributes map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(envelope.Data, &records); err != nil {
		return false, fmt.Errorf("parsing data collection: %w", err)
	}
	switch len(records) {
	case 0:
		return false, nil
	case 1:
		// Continue below.
	default:
		return false, errors.New("response exceeds the requested page limit of 1")
	}
	record := records[0]
	expectedType := entityType + "--" + bundle
	if record.Type != expectedType {
		return false, fmt.Errorf("response resource type %q does not match %q", record.Type, expectedType)
	}
	if strings.TrimSpace(record.ID) == "" {
		return false, errors.New("response resource has no ID")
	}
	raw, exists := record.Attributes[attribute]
	if !exists {
		return false, fmt.Errorf("response resource has no %q attribute", attribute)
	}
	actual, err := validationEntityScalar(raw)
	if err != nil {
		return false, fmt.Errorf("response resource attribute %q: %w", attribute, err)
	}
	if actual != value {
		return false, fmt.Errorf("response resource attribute %q does not match the requested value", attribute)
	}
	return true, nil
}

func validationEntityScalar(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("parsing scalar: %w", err)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case json.Number:
		text := typed.String()
		if !decimalIdentifier(text) {
			return "", errors.New("must be a JSON string or unsigned integer")
		}
		return text, nil
	default:
		return "", errors.New("must be a JSON string or unsigned integer")
	}
}

func (client *Client) taxonomyURIExists(ctx context.Context, value string, bundles []string) (bool, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false, nil
	}
	for _, route := range []struct {
		path  string
		param string
	}{{path: "term_from_uri", param: "uri"}, {path: "term_from_authority_link", param: "authority_link"}} {
		endpoint, endpointErr := client.validationSiteEndpoint(route.path)
		if endpointErr != nil {
			return false, endpointErr
		}
		query := endpoint.Query()
		query.Set("_format", "json")
		query.Set(route.param, parsed.String())
		endpoint.RawQuery = query.Encode()
		data, fetchErr := client.fetchValidationJSON(ctx, endpoint, "application/json")
		if fetchErr != nil {
			if validationRouteMissing(fetchErr) {
				continue
			}
			return false, fmt.Errorf("validating Drupal taxonomy URI: %w", fetchErr)
		}
		termIDs, parseErr := validationViewTermIDs(data)
		if parseErr != nil {
			return false, fmt.Errorf("validating Drupal taxonomy URI: %w", parseErr)
		}
		for _, termID := range termIDs {
			for _, bundle := range bundles {
				exists, lookupErr := client.jsonAPIEntityExists(ctx, "taxonomy_term", bundle, "drupal_internal__tid", termID)
				if lookupErr != nil {
					return false, lookupErr
				}
				if exists {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func validationViewTermIDs(data []byte) ([]string, error) {
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parsing term lookup response: %w", err)
	}
	if len(rows) > maxValidationViewResults {
		return nil, fmt.Errorf("term lookup response exceeds %d results", maxValidationViewResults)
	}
	result := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		termID := nestedDrupalScalar(row["tid"], "value")
		if !decimalIdentifier(termID) {
			continue
		}
		if _, exists := seen[termID]; exists {
			continue
		}
		seen[termID] = struct{}{}
		result = append(result, termID)
	}
	return result, nil
}

func nestedDrupalScalar(raw json.RawMessage, key string) string {
	var values []map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(values[0][key], &value) != nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed >= 0 && typed <= math.MaxUint64 && typed == float64(uint64(typed)) {
			return strconv.FormatUint(uint64(typed), 10)
		}
	}
	return ""
}

func (client *Client) fetchValidationJSON(ctx context.Context, endpoint *url.URL, accept string) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("validation context is required")
	}
	jsonAPI, site, err := client.validationEndpoints()
	if err != nil {
		return nil, err
	}
	if endpoint == nil || (!sameOrigin(jsonAPI, endpoint) && !sameOrigin(site, endpoint)) {
		return nil, errors.New("validation endpoint is outside the configured Drupal origin")
	}
	if err := validationcontext.ConsumeNetworkRequest(ctx); err != nil {
		return nil, err
	}
	fetcher := acquisition.NewClient()
	fetcher.HTTP = client.HTTP
	fetcher.UserAgent = "crosswalk/drupal-validation"
	fetcher.MaxResponseBytes = maxValidationResponseBytes
	fetcher.AllowPrivate = true
	document, err := fetcher.FetchRequest(ctx, acquisition.Request{
		URL: endpoint.String(), Accept: accept, Auth: client.Auth,
	})
	if err != nil {
		return nil, err
	}
	return document.Data, nil
}

func (client *Client) validationJSONAPIResource(entityType, bundle string) (*url.URL, error) {
	if !resourceSegmentPattern.MatchString(entityType) || !resourceSegmentPattern.MatchString(bundle) {
		return nil, errors.New("validating Drupal entity: model contains an invalid resource segment")
	}
	jsonAPI, _, err := client.validationEndpoints()
	if err != nil {
		return nil, err
	}
	endpoint := *jsonAPI
	endpoint.Path = path.Join(jsonAPI.Path, entityType, bundle)
	return &endpoint, nil
}

func (client *Client) validationSiteEndpoint(route string) (*url.URL, error) {
	if !resourceSegmentPattern.MatchString(route) {
		return nil, errors.New("validating Drupal site route is invalid")
	}
	_, site, err := client.validationEndpoints()
	if err != nil {
		return nil, err
	}
	endpoint := *site
	endpoint.Path = path.Join(site.Path, route)
	return &endpoint, nil
}

func (client *Client) validationEndpoints() (*url.URL, *url.URL, error) {
	if client == nil {
		return nil, nil, errors.New("configuring Drupal validation: client is nil")
	}
	base, err := url.Parse(strings.TrimSpace(client.BaseURL))
	if err != nil || base.Host == "" {
		return nil, nil, errors.New("configuring Drupal validation: absolute JSON:API endpoint is required")
	}
	if !secureEndpoint(base) {
		return nil, nil, errors.New("configuring Drupal validation: HTTPS endpoint is required except on loopback")
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.RawPath != "" || base.Opaque != "" {
		return nil, nil, errors.New("configuring Drupal validation: endpoint must not contain credentials, query, fragment, or encoded path")
	}
	canonicalPath := strings.TrimSuffix(base.Path, "/")
	if canonicalPath == "" {
		canonicalPath = "/"
	}
	if path.Clean(base.Path) != canonicalPath {
		return nil, nil, errors.New("configuring Drupal validation: endpoint path must be clean")
	}
	segments := strings.Split(strings.Trim(base.Path, "/"), "/")
	jsonAPIIndex := -1
	for index, segment := range segments {
		if segment == "jsonapi" {
			jsonAPIIndex = index
		}
	}
	if jsonAPIIndex < 0 {
		return nil, nil, errors.New("configuring Drupal validation: endpoint path must contain /jsonapi")
	}
	remainder := segments[jsonAPIIndex+1:]
	if len(remainder) != 0 && len(remainder) != 2 {
		return nil, nil, errors.New("configuring Drupal validation: JSON:API endpoint must be a root or one entity resource")
	}
	for _, segment := range remainder {
		if !resourceSegmentPattern.MatchString(segment) {
			return nil, nil, errors.New("configuring Drupal validation: JSON:API resource path is invalid")
		}
	}
	jsonAPI := *base
	jsonAPI.Path = "/" + strings.Join(segments[:jsonAPIIndex+1], "/")
	jsonAPI.RawQuery = ""
	jsonAPI.Fragment = ""
	site := *base
	siteSegments := segments[:jsonAPIIndex]
	if len(siteSegments) == 0 {
		site.Path = "/"
	} else {
		site.Path = "/" + strings.Join(siteSegments, "/")
	}
	site.RawQuery = ""
	site.Fragment = ""
	return &jsonAPI, &site, nil
}

func validationRouteMissing(err error) bool {
	var status *acquisition.HTTPStatusError
	return errors.As(err, &status) && status.Status == http.StatusNotFound
}

func drupalInternalIDAttribute(entityType string) (string, bool) {
	switch entityType {
	case "node":
		return "drupal_internal__nid", true
	case "taxonomy_term":
		return "drupal_internal__tid", true
	case "media":
		return "drupal_internal__mid", true
	case "file":
		return "drupal_internal__fid", true
	case "user":
		return "drupal_internal__uid", true
	default:
		return "", false
	}
}

func baseDrupalFieldPath(name string) string {
	name = strings.TrimSpace(name)
	if index := strings.IndexByte(name, '.'); index >= 0 {
		return name[:index]
	}
	return name
}

func configuredAllowedValuesProvider(field model.Field) string {
	for _, settings := range []map[string]any{field.InstanceSettings, field.StorageSettings} {
		if provider := settingString(settings["allowed_values_function"]); provider != "" {
			return provider
		}
	}
	return ""
}

func settingString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func decimalIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 20 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func canonicalStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if len(result) < 2 {
		return result
	}
	write := 1
	for read := 1; read < len(result); read++ {
		if result[read] != result[write-1] {
			result[write] = result[read]
			write++
		}
	}
	return result[:write]
}

func containsString(values []string, value string) bool {
	index := sort.SearchStrings(values, value)
	return index < len(values) && values[index] == value
}
