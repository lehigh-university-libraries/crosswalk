// Package drupal implements bounded, read-only Drupal JSON:API candidate lookup.
package drupal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/lehigh-university-libraries/crosswalk/format"
	drupalformat "github.com/lehigh-university-libraries/crosswalk/format/drupal"
	"github.com/lehigh-university-libraries/crosswalk/internal/provenanceuri"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/reconcile"
	acquisition "github.com/lehigh-university-libraries/crosswalk/source"
)

const (
	defaultMaxResponseBytes = int64(16 << 20)
	defaultMaxCandidates    = 100
	defaultPageSize         = 25
)

var resourceSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// BearerTokenAuth sends a caller-supplied OAuth bearer token.
type BearerTokenAuth string

// Apply adds bearer authentication without placing credentials in the URL.
func (token BearerTokenAuth) Apply(request *http.Request) error {
	if strings.TrimSpace(string(token)) == "" {
		return fmt.Errorf("Drupal JSON:API bearer token is empty")
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	return nil
}

// BasicAuth sends caller-supplied HTTP Basic credentials.
type BasicAuth struct {
	Username string
	Password string
}

// Apply adds HTTP Basic authentication.
func (auth BasicAuth) Apply(request *http.Request) error {
	if strings.TrimSpace(auth.Username) == "" {
		return fmt.Errorf("Drupal JSON:API username is empty")
	}
	request.SetBasicAuth(auth.Username, auth.Password)
	return nil
}

// Authenticator mutates only request headers for repository authentication.
type Authenticator interface {
	Apply(*http.Request) error
}

// Client implements reconcile.Finder using a configured Drupal JSON:API root.
// It performs exact server-side retrieval and leaves matching policy to reconcile.
type Client struct {
	BaseURL         string
	ResourceType    string
	IdentifierField string
	TitleField      string
	// SystemProfile supplies the repository bundle, exact lookup fields,
	// ordered metadata strategies, and candidate parsing contract.
	SystemProfile    *profile.Compiled
	HTTP             acquisition.HTTPDoer
	Auth             Authenticator
	MaxResponseBytes int64
	MaxCandidates    int
	PageSize         int
}

// NewClient returns a JSON:API candidate client with safe defaults.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL, ResourceType: "node--islandora_object",
		IdentifierField: "field_identifier", TitleField: "title",
		MaxResponseBytes: defaultMaxResponseBytes, MaxCandidates: defaultMaxCandidates, PageSize: defaultPageSize,
	}
}

// Candidates implements reconcile.Finder. Identifier lookups try both typed
// JSON:API subproperties and a plain field equality because Drupal field shape
// differs across Islandora installations.
func (client *Client) Candidates(ctx context.Context, query reconcile.Query) ([]reconcile.Candidate, error) {
	if ctx == nil {
		return nil, fmt.Errorf("querying Drupal JSON:API: context is required")
	}
	base, err := client.endpoint()
	if err != nil {
		return nil, err
	}
	if client.SystemProfile != nil {
		return client.profileCandidates(ctx, base, query)
	}
	var lookups []lookup
	switch query.Strategy {
	case reconcile.QueryByIdentifier:
		for _, identifier := range query.Identifiers {
			// Detector supplies only identifiers that its effective, possibly
			// institution-specific policy has accepted as exact lookup evidence.
			if identifier.Scheme == "" || identifier.Value == "" || identifier.NamespaceURI == "" {
				continue
			}
			for _, value := range identifierLookupValues(identifier) {
				lookups = append(lookups, lookup{field: client.identifierField(), value: value, operator: "CONTAINS"})
			}
		}
	case reconcile.QueryByMetadata:
		if strings.TrimSpace(query.Title) != "" {
			for _, value := range titleLookupValues(query.Title) {
				lookups = append(lookups, lookup{field: client.titleField(), value: value, operator: "CONTAINS"})
			}
		}
	default:
		return nil, fmt.Errorf("querying Drupal JSON:API: unsupported strategy %q", query.Strategy)
	}
	if len(lookups) == 0 {
		return nil, nil
	}

	results := make([]reconcile.Candidate, 0)
	seen := make(map[string]struct{})
	for lookupIndex, candidateLookup := range uniqueLookups(lookups) {
		// Metadata lookups are ordered from the complete title through
		// progressively broader fallbacks. Stop once a specific lookup has
		// produced candidates: executing a final single-token CONTAINS query
		// unconditionally can turn a useful result into an over-broad failure.
		if query.Strategy == reconcile.QueryByMetadata && lookupIndex > 0 && len(results) > 0 {
			break
		}
		var candidates []reconcile.Candidate
		if query.Strategy == reconcile.QueryByIdentifier {
			// Drupal JSON:API exposes multi-value text fields as either the
			// typed `.value` path or the field itself, depending on the field
			// normalizer and Drupal version. Prefer the typed path and use the
			// plain path only when it is unsupported or returns no records.
			typedLookup := candidateLookup
			typedLookup.field += ".value"
			candidates, err = client.lookup(ctx, base, typedLookup)
			if err != nil && !isUnsupportedFilter(err) {
				return nil, err
			}
			if len(candidates) == 0 {
				candidates, err = client.lookup(ctx, base, candidateLookup)
				if err != nil {
					if isUnsupportedFilter(err) {
						continue
					}
					return nil, err
				}
			}
		} else {
			candidates, err = client.lookup(ctx, base, candidateLookup)
			if err != nil {
				return nil, err
			}
		}
		for _, candidate := range candidates {
			key := candidate.RepositoryID + "\x00" + candidate.UUID + "\x00" + candidate.URL
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			results = append(results, candidate)
			if len(results) > client.candidateLimit() {
				return nil, fmt.Errorf("querying Drupal JSON:API: candidate results exceed %d", client.candidateLimit())
			}
		}
	}
	return results, nil
}

func (client *Client) profileCandidates(ctx context.Context, base *url.URL, query reconcile.Query) ([]reconcile.Candidate, error) {
	plan := client.SystemProfile.LookupPlan()
	if !plan.Enabled {
		return nil, nil
	}
	includes := profileIncludes(client.SystemProfile)
	var searches [][]lookup
	var err error
	switch query.Strategy {
	case reconcile.QueryByIdentifier:
		searches, err = client.profileIdentifierSearches(query, plan)
	case reconcile.QueryByMetadata:
		searches, err = profileMetadataSearches(query, plan)
	default:
		return nil, fmt.Errorf("querying Drupal JSON:API: unsupported strategy %q", query.Strategy)
	}
	if err != nil {
		return nil, fmt.Errorf("querying Drupal JSON:API with profile %q: %w", client.SystemProfile.Name(), err)
	}

	results := make([]reconcile.Candidate, 0)
	seen := make(map[string]struct{})
	for _, search := range uniqueCompoundLookups(searches) {
		candidates, lookupErr := client.lookupCompound(ctx, base, search, includes)
		if lookupErr != nil {
			return nil, lookupErr
		}
		for _, candidate := range candidates {
			key := candidate.RepositoryID + "\x00" + candidate.UUID + "\x00" + candidate.URL
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			results = append(results, candidate)
			if len(results) > client.candidateLimit() {
				return nil, fmt.Errorf("querying Drupal JSON:API: candidate results exceed %d", client.candidateLimit())
			}
		}
		// Metadata strategies and their variants are ordered from most to least
		// specific. Once one retrieves candidates, broader searches add risk but
		// no useful evidence. Identifier searches remain exhaustive.
		if query.Strategy == reconcile.QueryByMetadata && len(results) != 0 {
			break
		}
	}
	return results, nil
}

func (client *Client) profileIdentifierSearches(query reconcile.Query, plan profile.LookupPlan) ([][]lookup, error) {
	searches := make([][]lookup, 0)
	for _, identifier := range query.Identifiers {
		for _, rule := range plan.Identifiers {
			if rule.Strength != profile.IdentifierStrong || identifier.Scheme != rule.Scheme || !profileIdentifierScopeMatches(identifier, rule, client.SystemProfile) {
				continue
			}
			matched, err := client.SystemProfile.MatchesIdentifier(rule.Name, identifier.Value)
			if err != nil {
				return nil, err
			}
			if !matched {
				continue
			}
			values, err := profileIdentifierVariants(identifier, rule.Lookup.Variants)
			if err != nil {
				return nil, fmt.Errorf("identifier rule %q: %w", rule.Name, err)
			}
			operator := drupalLookupOperator(rule.Lookup.Operator)
			for _, field := range rule.Lookup.Fields {
				for _, value := range values {
					conditions := []lookup{{field: resolvedFieldPath(field), value: value, operator: operator}}
					conditions = appendSelectorPredicate(conditions, field)
					searches = append(searches, conditions)
				}
			}
		}
	}
	return searches, nil
}

func profileMetadataSearches(query reconcile.Query, plan profile.LookupPlan) ([][]lookup, error) {
	if plan.Metadata == nil || plan.Metadata.MetadataOnly != profile.MetadataOnlyReview || strings.TrimSpace(query.Title) == "" {
		return nil, nil
	}
	const maxSearches = 64
	searches := make([][]lookup, 0)
	for _, strategy := range plan.Metadata.Lookups {
		title, err := profileLookupAlternatives(strategy.Title, metadataVariantInput{title: query.Title})
		if err != nil {
			return nil, fmt.Errorf("metadata lookup %q title: %w", strategy.Name, err)
		}
		groups := [][][]lookup{title}
		if strategy.Contributors != nil {
			if len(query.Authors) == 0 {
				continue
			}
			contributors, variantErr := profileLookupAlternatives(*strategy.Contributors, metadataVariantInput{authors: query.Authors})
			if variantErr != nil {
				return nil, fmt.Errorf("metadata lookup %q contributors: %w", strategy.Name, variantErr)
			}
			groups = append(groups, contributors)
		}
		if strategy.Date != nil {
			if query.Year == 0 {
				continue
			}
			date, variantErr := profileLookupAlternatives(*strategy.Date, metadataVariantInput{year: query.Year})
			if variantErr != nil {
				return nil, fmt.Errorf("metadata lookup %q date: %w", strategy.Name, variantErr)
			}
			groups = append(groups, date)
		}
		combinations, combineErr := lookupProduct(groups, maxSearches-len(searches))
		if combineErr != nil {
			return nil, fmt.Errorf("metadata lookup %q: %w", strategy.Name, combineErr)
		}
		searches = append(searches, combinations...)
	}
	return searches, nil
}

type metadataVariantInput struct {
	title   string
	authors []string
	year    int32
}

func profileLookupAlternatives(rule profile.CompiledLookupRule, input metadataVariantInput) ([][]lookup, error) {
	values := make([]string, 0)
	for _, variant := range rule.Variants {
		switch variant {
		case "canonical":
			if input.title != "" {
				values = append(values, strings.Join(strings.Fields(input.title), " "))
			} else if len(input.authors) != 0 {
				values = append(values, input.authors...)
			} else if input.year != 0 {
				values = append(values, strconv.FormatInt(int64(input.year), 10))
			}
		case "title-phrase":
			phrases := titleLookupValues(input.title)
			if len(phrases) > 1 {
				values = append(values, phrases[1:]...)
			}
		case "author-family":
			for _, author := range input.authors {
				if family := authorFamilyName(author); family != "" {
					values = append(values, family)
				}
			}
		case "year":
			if input.year != 0 {
				values = append(values, strconv.FormatInt(int64(input.year), 10))
			}
		default:
			return nil, fmt.Errorf("unsupported value variant %q", variant)
		}
	}
	values = uniqueStrings(values)
	if len(values) == 0 {
		return nil, fmt.Errorf("configured variants produced no query values")
	}
	operator := drupalLookupOperator(rule.Operator)
	result := make([][]lookup, 0, len(values)*len(rule.Fields))
	for _, field := range rule.Fields {
		for _, value := range values {
			condition := lookup{field: resolvedFieldPath(field), value: value, operator: operator}
			result = append(result, appendSelectorPredicate([]lookup{condition}, field))
		}
	}
	return result, nil
}

func lookupProduct(groups [][][]lookup, limit int) ([][]lookup, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("candidate search expansion exceeds 64 requests")
	}
	result := [][]lookup{{}}
	for _, group := range groups {
		if len(group) == 0 {
			return nil, fmt.Errorf("lookup condition has no alternatives")
		}
		next := make([][]lookup, 0)
		for _, prefix := range result {
			for _, alternative := range group {
				candidate := append(append([]lookup(nil), prefix...), alternative...)
				next = append(next, candidate)
				if len(next) > limit {
					return nil, fmt.Errorf("candidate search expansion exceeds 64 requests")
				}
			}
		}
		result = next
	}
	return result, nil
}

func profileIdentifierVariants(identifier reconcile.IdentifierKey, variants []string) ([]string, error) {
	values := make([]string, 0, len(variants))
	for _, variant := range variants {
		switch variant {
		case "canonical":
			values = append(values, identifier.Value)
		case "doi-url":
			if identifier.Scheme != "doi" {
				return nil, fmt.Errorf("doi-url variant requires scheme doi")
			}
			values = append(values, "https://doi.org/"+identifier.Value)
		case "wos-bare":
			if identifier.Scheme != "wos" {
				return nil, fmt.Errorf("wos-bare variant requires scheme wos")
			}
			values = append(values, strings.TrimPrefix(identifier.Value, "WOS:"))
		case "lower":
			values = append(values, strings.ToLower(identifier.Value))
		case "upper":
			values = append(values, strings.ToUpper(identifier.Value))
		default:
			return nil, fmt.Errorf("unsupported value variant %q", variant)
		}
	}
	return uniqueStrings(values), nil
}

func profileIdentifierScopeMatches(identifier reconcile.IdentifierKey, rule profile.CompiledIdentifierRule, compiled *profile.Compiled) bool {
	registryRule, exists := compiled.IdentifierRegistry().Rule(rule.Scheme)
	if !exists {
		return false
	}
	namespace := rule.Namespace
	if namespace == "" {
		namespace = registryRule.NamespaceURI
	}
	level := strings.ToLower(strings.TrimPrefix(identifier.IdentityLevel.String(), "IDENTIFIER_IDENTITY_LEVEL_"))
	return identifier.NamespaceURI == namespace && level == string(rule.IdentityLevel)
}

func resolvedFieldPath(field profile.ResolvedField) string {
	result := field.Selector.Path
	if field.Selector.Attribute != "" {
		result += "." + field.Selector.Attribute
	}
	return result
}

func appendSelectorPredicate(conditions []lookup, field profile.ResolvedField) []lookup {
	if field.Selector.Where != nil {
		conditions = append(conditions, lookup{
			field: field.Selector.Path + "." + field.Selector.Where.Attribute,
			value: field.Selector.Where.Equals, operator: "=",
		})
	}
	return conditions
}

func drupalLookupOperator(operator profile.LookupOperator) string {
	if operator == profile.LookupContains {
		return "CONTAINS"
	}
	return "="
}

func profileIncludes(compiled *profile.Compiled) []string {
	result := make([]string, 0)
	for _, mapping := range compiled.Mappings() {
		if mapping.Decode != "none" && mapping.Field.Reference != nil {
			result = append(result, mapping.Field.Selector.Path)
		}
	}
	return uniqueStrings(result)
}

func authorFamilyName(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if family, _, found := strings.Cut(value, ","); found {
		return strings.TrimSpace(family)
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func uniqueCompoundLookups(searches [][]lookup) [][]lookup {
	seen := make(map[string]struct{}, len(searches))
	result := make([][]lookup, 0, len(searches))
	for _, search := range searches {
		parts := make([]string, 0, len(search))
		valid := true
		for _, condition := range search {
			if condition.field == "" || condition.value == "" {
				valid = false
				break
			}
			parts = append(parts, condition.field+"\x01"+condition.operator+"\x01"+condition.value)
		}
		if !valid {
			continue
		}
		key := strings.Join(parts, "\x02")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, search)
	}
	return result
}

func lookupFields(conditions []lookup) []string {
	result := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		result = append(result, condition.field)
	}
	return result
}

type statusError struct {
	Status int
}

func (err *statusError) Error() string { return fmt.Sprintf("unexpected HTTP status %d", err.Status) }

func isUnsupportedFilter(err error) bool {
	var status *statusError
	return errors.As(err, &status) && (status.Status == http.StatusBadRequest || status.Status == http.StatusUnprocessableEntity)
}

type lookup struct {
	field    string
	value    string
	operator string
}

type jsonAPIResponse struct {
	Data     []jsonAPIResource `json:"data"`
	Included []jsonAPIResource `json:"included"`
	Links    struct {
		Next json.RawMessage `json:"next"`
	} `json:"links"`
}

type jsonAPIResource struct {
	Type          string                     `json:"type"`
	ID            string                     `json:"id"`
	Attributes    map[string]json.RawMessage `json:"attributes"`
	Relationships map[string]struct {
		Data json.RawMessage `json:"data"`
	} `json:"relationships"`
	Links struct {
		Self json.RawMessage `json:"self"`
	} `json:"links"`
}

func (client *Client) lookup(ctx context.Context, base *url.URL, candidateLookup lookup) ([]reconcile.Candidate, error) {
	return client.lookupCompound(ctx, base, []lookup{candidateLookup}, []string{"field_linked_agent"})
}

func (client *Client) lookupCompound(ctx context.Context, base *url.URL, conditions []lookup, includes []string) ([]reconcile.Candidate, error) {
	if len(conditions) == 0 {
		return nil, nil
	}
	pageURL := *base
	query := pageURL.Query()
	if len(conditions) > 1 {
		query.Set("filter[crosswalk][group][conjunction]", "AND")
	}
	for index, condition := range conditions {
		name := "crosswalk"
		if len(conditions) > 1 {
			name = "crosswalk-" + strconv.Itoa(index+1)
		}
		prefix := "filter[" + name + "][condition]"
		query.Set(prefix+"[path]", condition.field)
		operator := condition.operator
		if operator == "" {
			operator = "="
		}
		query.Set(prefix+"[operator]", operator)
		query.Set(prefix+"[value]", condition.value)
		if len(conditions) > 1 {
			query.Set(prefix+"[memberOf]", "crosswalk")
		}
	}
	if includes = uniqueStrings(includes); len(includes) != 0 {
		query.Set("include", strings.Join(includes, ","))
	}
	query.Set("page[limit]", strconv.Itoa(client.pageLimit()))
	pageURL.RawQuery = query.Encode()

	results := make([]reconcile.Candidate, 0)
	visited := make(map[string]struct{})
	for pageIndex := 0; pageURL.String() != ""; pageIndex++ {
		if pageIndex >= 20 {
			return nil, fmt.Errorf("querying Drupal JSON:API: pagination exceeds 20 pages")
		}
		if _, exists := visited[pageURL.String()]; exists {
			return nil, fmt.Errorf("querying Drupal JSON:API: pagination loop detected")
		}
		visited[pageURL.String()] = struct{}{}
		response, err := client.fetch(ctx, &pageURL)
		if err != nil {
			return nil, fmt.Errorf("querying Drupal JSON:API fields %q: %w", lookupFields(conditions), err)
		}
		for _, resource := range response.Data {
			candidate, err := client.resourceCandidate(base, resource, response.Included)
			if err != nil {
				return nil, err
			}
			results = append(results, candidate)
			if len(results) > client.candidateLimit() {
				return nil, fmt.Errorf("querying Drupal JSON:API: candidate results exceed %d", client.candidateLimit())
			}
		}
		next := nextURL(response.Links.Next)
		if next == "" {
			break
		}
		parsedNext, err := pageURL.Parse(next)
		if err != nil || !sameOrigin(base, parsedNext) {
			return nil, fmt.Errorf("querying Drupal JSON:API: unsafe pagination link")
		}
		pageURL = *parsedNext
	}
	return results, nil
}

func (client *Client) fetch(ctx context.Context, endpoint *url.URL) (jsonAPIResponse, error) {
	fetcher := acquisition.NewClient()
	fetcher.HTTP = client.HTTP
	fetcher.UserAgent = "crosswalk/drupal-reconcile"
	fetcher.MaxResponseBytes = client.responseLimit()
	// The repository endpoint is operator-configured and commonly lives on an
	// internal network. Allow those addresses while retaining the acquisition
	// client's no-proxy transport, same-origin redirects, timeout, and bounds.
	fetcher.AllowPrivate = true
	document, err := fetcher.FetchRequest(ctx, acquisition.Request{
		URL: endpoint.String(), Accept: "application/vnd.api+json", Auth: client.Auth,
	})
	if err != nil {
		var status *acquisition.HTTPStatusError
		if errors.As(err, &status) {
			return jsonAPIResponse{}, &statusError{Status: status.Status}
		}
		return jsonAPIResponse{}, fmt.Errorf("request failed: %w", err)
	}
	var decoded jsonAPIResponse
	if err := json.Unmarshal(document.Data, &decoded); err != nil {
		return jsonAPIResponse{}, fmt.Errorf("parsing response: %w", err)
	}
	return decoded, nil
}

func (client *Client) resourceCandidate(base *url.URL, resource jsonAPIResource, included []jsonAPIResource) (reconcile.Candidate, error) {
	if resource.ID == "" {
		return reconcile.Candidate{}, fmt.Errorf("querying Drupal JSON:API: candidate has no UUID")
	}
	entity := make(map[string]json.RawMessage, len(resource.Attributes)+1)
	for name, raw := range resource.Attributes {
		entity[name] = raw
	}
	for name, relationship := range resource.Relationships {
		references, relationErr := enrichedRelationshipValues(relationship.Data, included)
		if relationErr != nil {
			return reconcile.Candidate{}, fmt.Errorf("querying Drupal JSON:API: parsing relationship %q: %w", name, relationErr)
		}
		if len(references) != 0 {
			encodedReferences, encodeErr := json.Marshal(references)
			if encodeErr != nil {
				return reconcile.Candidate{}, fmt.Errorf("querying Drupal JSON:API: encoding relationship %q: %w", name, encodeErr)
			}
			entity[name] = encodedReferences
		}
	}
	if _, exists := entity["uuid"]; !exists {
		encoded, _ := json.Marshal([]map[string]string{{"value": resource.ID}})
		entity["uuid"] = encoded
	}
	encoded, err := json.Marshal(entity)
	if err != nil {
		return reconcile.Candidate{}, fmt.Errorf("querying Drupal JSON:API: encoding candidate: %w", err)
	}
	records, err := (&drupalformat.Format{}).Parse(strings.NewReader(string(encoded)), &format.ParseOptions{
		BaseURL: siteBaseURL(base), StripHTML: true, SystemProfile: client.SystemProfile,
		SourceName: selfLinkOrEndpoint(resource, base),
	})
	if err != nil || len(records) != 1 {
		if err != nil {
			return reconcile.Candidate{}, fmt.Errorf("querying Drupal JSON:API: parsing candidate: %w", err)
		}
		return reconcile.Candidate{}, fmt.Errorf("querying Drupal JSON:API: parsed candidate count %d, want 1", len(records))
	}
	repositoryID := rawScalar(resource.Attributes["drupal_internal__nid"])
	if repositoryID == "" {
		repositoryID = rawScalar(resource.Attributes["nid"])
	}
	candidateURL := linkURL(resource.Links.Self)
	if candidateURL != "" {
		normalizedURL, normalizeErr := provenanceuri.Normalize(candidateURL, provenanceuri.Options{})
		if normalizeErr != nil {
			return reconcile.Candidate{}, fmt.Errorf("querying Drupal JSON:API: unsafe candidate self link: %w", normalizeErr)
		}
		candidateURL = normalizedURL
	}
	if records[0].GetSourceInfo() != nil {
		// The Drupal parser has already canonicalized this provenance URI and
		// removed credentials, fragments, and sensitive query parameters. Never
		// copy the raw JSON:API self link into a persisted review report.
		if sourceURI := records[0].GetSourceInfo().GetSourceUri(); sourceURI != "" {
			candidateURL = sourceURI
		}
	}
	return reconcile.Candidate{
		Kind: reconcile.CandidateRepository, RepositoryID: repositoryID,
		UUID: resource.ID, URL: candidateURL, Record: records[0],
	}, nil
}

type relationshipIdentifier struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Meta struct {
		DrupalInternalTargetID any    `json:"drupal_internal__target_id"`
		RelType                string `json:"rel_type"`
	} `json:"meta"`
}

func enrichedRelationshipValues(raw json.RawMessage, included []jsonAPIResource) ([]map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var identifiers []relationshipIdentifier
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &identifiers); err != nil {
			return nil, err
		}
	} else {
		var identifier relationshipIdentifier
		if err := json.Unmarshal(raw, &identifier); err != nil {
			return nil, err
		}
		identifiers = append(identifiers, identifier)
	}
	indexed := make(map[string]jsonAPIResource, len(included))
	for _, resource := range included {
		indexed[resource.Type+"\x00"+resource.ID] = resource
	}
	result := make([]map[string]any, 0, len(identifiers))
	for _, identifier := range identifiers {
		if identifier.ID == "" {
			continue
		}
		targetID := identifier.Meta.DrupalInternalTargetID
		if targetID == nil || fmt.Sprint(targetID) == "<nil>" || fmt.Sprint(targetID) == "" {
			targetID = identifier.ID
		}
		value := map[string]any{
			"target_id": targetID, "target_uuid": identifier.ID, "target_type": identifier.Type,
		}
		if identifier.Meta.RelType != "" {
			value["rel_type"] = identifier.Meta.RelType
		}
		if related, exists := indexed[identifier.Type+"\x00"+identifier.ID]; exists {
			value["_entity"] = related.Attributes
		}
		result = append(result, value)
	}
	return result, nil
}

func (client *Client) endpoint() (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(client.BaseURL))
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("querying Drupal JSON:API: absolute endpoint is required")
	}
	if !secureEndpoint(base) {
		return nil, fmt.Errorf("querying Drupal JSON:API: HTTPS endpoint is required except on loopback")
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("querying Drupal JSON:API: endpoint must not contain credentials, query, or fragment")
	}
	resourceTypeValue := client.ResourceType
	if client.SystemProfile != nil {
		if client.SystemProfile.System() != "drupal" {
			return nil, fmt.Errorf("querying Drupal JSON:API: profile system %q is not drupal", client.SystemProfile.System())
		}
		repository := client.SystemProfile.LookupPlan().Repository
		if strings.TrimSpace(repository.EntityType) == "" || strings.TrimSpace(repository.Bundle) == "" {
			return nil, fmt.Errorf("querying Drupal JSON:API: profile has no repository entity and bundle")
		}
		resourceTypeValue = repository.EntityType + "--" + repository.Bundle
	}
	resourceType, err := jsonAPIResourcePath(resourceTypeValue)
	if err != nil {
		return nil, fmt.Errorf("querying Drupal JSON:API: %w", err)
	}
	if !strings.HasSuffix(strings.TrimSuffix(base.Path, "/"), "/"+resourceType) {
		base.Path = path.Join(base.Path, resourceType)
	}
	return base, nil
}

func jsonAPIResourcePath(value string) (string, error) {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return "", fmt.Errorf("resource type is required")
	}
	if strings.Count(value, "--") == 1 && !strings.Contains(value, "/") {
		value = strings.Replace(value, "--", "/", 1)
	}
	segments := strings.Split(value, "/")
	if len(segments) != 2 {
		return "", fmt.Errorf("resource type %q must be entity--bundle or entity/bundle", value)
	}
	for _, segment := range segments {
		if !resourceSegmentPattern.MatchString(segment) {
			return "", fmt.Errorf("resource type %q contains an invalid path segment", value)
		}
	}
	return strings.Join(segments, "/"), nil
}

func secureEndpoint(endpoint *url.URL) bool {
	if endpoint.Scheme == "https" {
		return true
	}
	if endpoint.Scheme != "http" {
		return false
	}
	host := endpoint.Hostname()
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func sameOrigin(left, right *url.URL) bool {
	return left != nil && right != nil && strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func nextURL(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	var object struct {
		Href string `json:"href"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return strings.TrimSpace(object.Href)
	}
	return ""
}

func linkURL(raw json.RawMessage) string { return nextURL(raw) }

func selfLinkOrEndpoint(resource jsonAPIResource, endpoint *url.URL) string {
	if self := linkURL(resource.Links.Self); self != "" {
		return self
	}
	if endpoint == nil {
		return ""
	}
	result := *endpoint
	if resource.ID != "" {
		result.Path = path.Join(result.Path, resource.ID)
	}
	result.RawQuery = ""
	result.Fragment = ""
	return result.String()
}

func rawScalar(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case []any:
		if len(typed) > 0 {
			if object, ok := typed[0].(map[string]any); ok {
				return fmt.Sprint(object["value"])
			}
		}
	}
	return ""
}

func siteBaseURL(endpoint *url.URL) string {
	if endpoint == nil {
		return ""
	}
	return endpoint.Scheme + "://" + endpoint.Host
}

func uniqueLookups(values []lookup) []lookup {
	seen := make(map[lookup]struct{}, len(values))
	result := make([]lookup, 0, len(values))
	for _, value := range values {
		if value.field == "" || value.value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func identifierLookupValues(identifier reconcile.IdentifierKey) []string {
	values := []string{strings.TrimSpace(identifier.Value)}
	if identifier.Scheme == "wos" {
		values = append(values, strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(identifier.Value)), "WOS:"))
	}
	return uniqueStrings(values)
}

func titleLookupValues(title string) []string {
	title = strings.Join(strings.Fields(title), " ")
	values := []string{title}
	for _, separator := range []string{":", " - ", " — ", " – "} {
		if prefix, _, found := strings.Cut(title, separator); found && len([]rune(strings.TrimSpace(prefix))) >= 12 {
			values = append(values, strings.TrimSpace(prefix))
		}
	}
	longest := ""
	for _, token := range strings.FieldsFunc(title, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	}) {
		if len([]rune(token)) > len([]rune(longest)) {
			longest = token
		}
	}
	if len([]rune(longest)) >= 8 {
		values = append(values, longest)
	}
	return uniqueStrings(values)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (client *Client) identifierField() string {
	if client != nil && strings.TrimSpace(client.IdentifierField) != "" {
		return strings.TrimSpace(client.IdentifierField)
	}
	return "field_identifier"
}

func (client *Client) titleField() string {
	if client != nil && strings.TrimSpace(client.TitleField) != "" {
		return strings.TrimSpace(client.TitleField)
	}
	return "title"
}

func (client *Client) responseLimit() int64 {
	if client != nil && client.MaxResponseBytes > 0 {
		return client.MaxResponseBytes
	}
	return defaultMaxResponseBytes
}

func (client *Client) candidateLimit() int {
	if client != nil && client.MaxCandidates > 0 {
		return client.MaxCandidates
	}
	return defaultMaxCandidates
}

func (client *Client) pageLimit() int {
	if client != nil && client.PageSize > 0 && client.PageSize <= 50 {
		return client.PageSize
	}
	return defaultPageSize
}
