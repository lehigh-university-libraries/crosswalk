package drupal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/validationcontext"
)

func TestValidationNodeExistsUsesProfileRepositoryAndInertFilter(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:node")
	requests := 0
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Scheme != "https" || request.URL.Host != "repo.example.org" || request.URL.Path != "/sub/jsonapi/node/article" {
			t.Fatalf("request URL = %s", request.URL)
		}
		query := request.URL.Query()
		if query.Get("filter[crosswalk-context][condition][path]") != "drupal_internal__nid" ||
			query.Get("filter[crosswalk-context][condition][operator]") != "=" ||
			query.Get("filter[crosswalk-context][condition][value]") != "42" || query.Get("page[limit]") != "1" {
			t.Fatalf("query = %s", request.URL.RawQuery)
		}
		return validationResponse(request, http.StatusOK, `{"data":[{"type":"node--article","id":"uuid","attributes":{"drupal_internal__nid":42}}]}`), nil
	})

	exists, err := client.NodeExists(context.Background(), 42)
	if err != nil || !exists || requests != 1 {
		t.Fatalf("NodeExists() = %v, %v; requests=%d", exists, err, requests)
	}
}

func TestValidationNodeExistsRejectsMalformedFilteredResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing data", body: `{}`},
		{name: "null data", body: `{"data": null}`},
		{name: "invalid JSON", body: `{"data":[`},
		{name: "object data", body: `{"data":{"type":"node--article","id":"uuid","attributes":{"drupal_internal__nid":42}}}`},
		{name: "incomplete resource", body: `{"data":[{}]}`},
		{name: "multiple resources", body: `{"data":[{"type":"node--article","id":"one","attributes":{"drupal_internal__nid":42}},{"type":"node--article","id":"two","attributes":{"drupal_internal__nid":42}}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
			client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
				return validationResponse(request, http.StatusOK, test.body), nil
			})
			exists, err := client.NodeExists(context.Background(), 42)
			if err == nil || exists {
				t.Fatalf("NodeExists() = %v, %v; want fail-closed response error", exists, err)
			}
		})
	}
}

func TestValidationEntityReferenceUsesModelTargetBundles(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
	paths := make([]string, 0, 2)
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		if request.URL.Query().Get("filter[crosswalk-context][condition][path]") != "drupal_internal__tid" ||
			request.URL.Query().Get("filter[crosswalk-context][condition][value]") != "17" {
			t.Fatalf("query = %s", request.URL.RawQuery)
		}
		body := `{"data":[]}`
		if request.URL.Path == "/sub/jsonapi/taxonomy_term/person" {
			body = `{"data":[{"type":"taxonomy_term--person","id":"uuid","attributes":{"drupal_internal__tid":"17"}}]}`
		}
		return validationResponse(request, http.StatusOK, body), nil
	})

	exists, err := client.EntityReferenceExists(context.Background(), validationcontext.EntityReferenceQuery{
		EntityType: "taxonomy_term", Bundles: []string{"person", "genre"}, Field: "field_subject",
		SourceType: "entity_reference", Handler: "default:taxonomy_term", Kind: validationcontext.EntityReferenceID, Value: "17",
	})
	if err != nil || !exists {
		t.Fatalf("EntityReferenceExists() = %v, %v", exists, err)
	}
	if strings.Join(paths, ",") != "/sub/jsonapi/taxonomy_term/genre,/sub/jsonapi/taxonomy_term/person" {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestValidationEntityReferenceRejectsMisroutedOrFilterIgnoredResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "misrouted resource type",
			body: `{"data":[{"type":"taxonomy_term--person","id":"uuid","attributes":{"drupal_internal__tid":17}}]}`,
		},
		{
			name: "ignored filter",
			body: `{"data":[{"type":"taxonomy_term--genre","id":"uuid","attributes":{"drupal_internal__tid":18}}]}`,
		},
		{
			name: "missing resource ID",
			body: `{"data":[{"type":"taxonomy_term--genre","attributes":{"drupal_internal__tid":17}}]}`,
		},
		{
			name: "nonscalar filter evidence",
			body: `{"data":[{"type":"taxonomy_term--genre","id":"uuid","attributes":{"drupal_internal__tid":[17]}}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
			client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
				return validationResponse(request, http.StatusOK, test.body), nil
			})
			exists, err := client.EntityReferenceExists(context.Background(), validationcontext.EntityReferenceQuery{
				EntityType: "taxonomy_term", Bundles: []string{"genre"}, Field: "field_subject",
				SourceType: "entity_reference", Handler: "default:taxonomy_term", Kind: validationcontext.EntityReferenceID, Value: "17",
			})
			if err == nil || exists {
				t.Fatalf("EntityReferenceExists() = %v, %v; want fail-closed response error", exists, err)
			}
		})
	}
}

func TestValidationEntityReferenceDoesNotTreatAuthorizationFailureAsMissing(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		return validationResponse(request, http.StatusForbidden, `{"message":"forbidden"}`), nil
	})
	exists, err := client.EntityReferenceExists(context.Background(), validationcontext.EntityReferenceQuery{
		EntityType: "taxonomy_term", Bundles: []string{"genre"}, Field: "field_subject",
		SourceType: "entity_reference", Handler: "default:taxonomy_term", Kind: validationcontext.EntityReferenceName, Value: "New Topic",
	})
	if err == nil || exists || !strings.Contains(err.Error(), "403") {
		t.Fatalf("EntityReferenceExists() = %v, %v; authorization failure must not look like a creatable missing name", exists, err)
	}
}

func TestValidationUserReferenceUsesCoreJSONAPIResourceWithoutSyntheticModelBundle(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/sub/jsonapi/user/user" ||
			request.URL.Query().Get("filter[crosswalk-context][condition][path]") != "drupal_internal__uid" ||
			request.URL.Query().Get("filter[crosswalk-context][condition][value]") != "7" {
			t.Fatalf("request = %s", request.URL)
		}
		return validationResponse(request, http.StatusOK, `{"data":[{"type":"user--user","id":"uuid","attributes":{"drupal_internal__uid":7}}]}`), nil
	})
	exists, err := client.EntityReferenceExists(context.Background(), validationcontext.EntityReferenceQuery{
		EntityType: "user", Field: "uid", SourceType: "entity_reference", Handler: "default:user", Kind: validationcontext.EntityReferenceID, Value: "7",
	})
	if err != nil || !exists {
		t.Fatalf("EntityReferenceExists() = %v, %v", exists, err)
	}
}

func TestValidationURLAliasUsesOnlyConfiguredDrupalOrigin(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
	requests := 0
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Scheme != "https" || request.URL.Host != "repo.example.org" || request.URL.Path != "/sub/jsonapi/path_alias/path_alias" {
			t.Fatalf("request escaped fixed alias endpoint: %s", request.URL)
		}
		query := request.URL.Query()
		if query.Get("filter[crosswalk-context][condition][path]") != "alias" ||
			query.Get("filter[crosswalk-context][condition][operator]") != "=" ||
			query.Get("filter[crosswalk-context][condition][value]") != "/admin/config/system" ||
			query.Get("page[limit]") != "2" {
			t.Fatalf("alias query = %s", request.URL.RawQuery)
		}
		return validationResponse(request, http.StatusOK, `{"data":[]}`), nil
	})
	available, err := client.URLAliasAvailable(context.Background(), "/admin/config/system")
	if err != nil || !available || requests != 1 {
		t.Fatalf("URLAliasAvailable() = %v, %v; requests=%d", available, err, requests)
	}

	for _, alias := range []string{"//169.254.169.254/latest", "/../admin", "/encoded%2fpath", "/alias?destination=https://evil.example"} {
		if available, err := client.URLAliasAvailable(context.Background(), alias); err == nil || available {
			t.Errorf("URLAliasAvailable(%q) = %v, %v; want rejected", alias, available, err)
		}
	}
	if requests != 1 {
		t.Fatalf("invalid aliases caused %d requests", requests-1)
	}
}

func TestValidationURLAliasReportsExistingPath(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		return validationResponse(request, http.StatusOK, `{"data":[{"type":"path_alias--path_alias","id":"alias-uuid","attributes":{"alias":"/existing","path":"/node/42","langcode":"en"}}]}`), nil
	})
	available, err := client.URLAliasAvailable(context.Background(), "/existing")
	if err != nil || available {
		t.Fatalf("URLAliasAvailable() = %v, %v; want false, nil", available, err)
	}
}

func TestValidationURLAliasRejectsMismatchedFilteredResponse(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		return validationResponse(request, http.StatusOK, `{"data":[{"type":"path_alias--path_alias","id":"alias-uuid","attributes":{"alias":"/different","path":"/node/42","langcode":"en"}}]}`), nil
	})
	if available, err := client.URLAliasAvailable(context.Background(), "/requested"); err == nil || available || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("URLAliasAvailable() = %v, %v; want mismatched-response error", available, err)
	}
}

func TestValidationTaxonomyURIIsOnlyAQueryValueOnFixedOrigin(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
	const candidate = "http://169.254.169.254/latest/meta-data"
	paths := make([]string, 0, 2)
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "repo.example.org" {
			t.Fatalf("request escaped configured origin: %s", request.URL)
		}
		paths = append(paths, request.URL.Path)
		switch request.URL.Path {
		case "/sub/term_from_uri":
			if request.URL.Query().Get("uri") != candidate {
				t.Fatalf("URI query value = %q", request.URL.Query().Get("uri"))
			}
			return validationResponse(request, http.StatusOK, `[{"tid":[{"value":"19"}]}]`), nil
		case "/sub/jsonapi/taxonomy_term/genre":
			if request.URL.Query().Get("filter[crosswalk-context][condition][value]") != "19" {
				t.Fatalf("term ID query = %s", request.URL.RawQuery)
			}
			return validationResponse(request, http.StatusOK, `{"data":[{"type":"taxonomy_term--genre","id":"uuid","attributes":{"drupal_internal__tid":19}}]}`), nil
		default:
			return validationResponse(request, http.StatusOK, `{"data":[]}`), nil
		}
	})

	exists, err := client.EntityReferenceExists(context.Background(), validationcontext.EntityReferenceQuery{
		EntityType: "taxonomy_term", Bundles: []string{"genre", "person"}, Field: "field_subject",
		SourceType: "entity_reference", Handler: "default:taxonomy_term", Kind: validationcontext.EntityReferenceURI, Value: candidate,
	})
	if err != nil || !exists {
		t.Fatalf("EntityReferenceExists() = %v, %v", exists, err)
	}
	if strings.Join(paths, ",") != "/sub/term_from_uri,/sub/jsonapi/taxonomy_term/genre" {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestValidationRejectsReferenceConstraintDriftBeforeRequest(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request to %s", request.URL)
		return nil, nil
	})
	_, err := client.EntityReferenceExists(context.Background(), validationcontext.EntityReferenceQuery{
		EntityType: "taxonomy_term", Bundles: []string{"place"}, Field: "field_subject",
		SourceType: "entity_reference", Handler: "default:taxonomy_term", Kind: validationcontext.EntityReferenceID, Value: "17",
	})
	if err == nil || !strings.Contains(err.Error(), "bundles do not match") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidationAllowsModelBoundNamespaceNarrowing(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/sub/jsonapi/taxonomy_term/person" || request.URL.Query().Get("filter[crosswalk-context][condition][value]") != "Doe, Jane" {
			t.Fatalf("request = %s", request.URL)
		}
		return validationResponse(request, http.StatusOK, `{"data":[{"type":"taxonomy_term--person","id":"uuid","attributes":{"name":"Doe, Jane"}}]}`), nil
	})
	exists, err := client.EntityReferenceExists(context.Background(), validationcontext.EntityReferenceQuery{
		EntityType: "taxonomy_term", Bundles: []string{"person"}, Field: "field_subject",
		SourceType: "entity_reference", Handler: "default:taxonomy_term", Kind: validationcontext.EntityReferenceName, Value: "Doe, Jane",
	})
	if err != nil || !exists {
		t.Fatalf("EntityReferenceExists() = %v, %v", exists, err)
	}
}

func TestValidationViewsHandlerFailsExplicitly(t *testing.T) {
	for _, handler := range []string{"views", "views:subjects"} {
		t.Run(handler, func(t *testing.T) {
			client, _, _ := configuredValidationClient(t, handler)
			client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected request to %s", request.URL)
				return nil, nil
			})
			_, err := client.EntityReferenceExists(context.Background(), validationcontext.EntityReferenceQuery{
				EntityType: "taxonomy_term", Bundles: []string{"genre", "person"}, Field: "field_subject",
				SourceType: "entity_reference", Handler: handler, Kind: validationcontext.EntityReferenceName, Value: "Maps",
			})
			var unavailable *validationcontext.ErrCapabilityUnavailable
			if !errors.As(err, &unavailable) || unavailable.Capability != validationcontext.CapabilityEntityReference {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidationCustomHandlerFailsBeforeRequest(t *testing.T) {
	const handler = "custom:restricted_terms"
	client, _, _ := configuredValidationClient(t, handler)
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		t.Fatalf("custom handler caused request to %s", request.URL)
		return nil, nil
	})
	_, err := client.EntityReferenceExists(context.Background(), validationcontext.EntityReferenceQuery{
		EntityType: "taxonomy_term", Bundles: []string{"genre", "person"}, Field: "field_subject",
		SourceType: "entity_reference", Handler: handler, Kind: validationcontext.EntityReferenceName, Value: "Maps",
	})
	var unavailable *validationcontext.ErrCapabilityUnavailable
	if !errors.As(err, &unavailable) || unavailable.Capability != validationcontext.CapabilityEntityReference {
		t.Fatalf("error = %v", err)
	}
}

func TestValidationTaxonomyFanoutHonorsSharedNetworkBudget(t *testing.T) {
	client, _, _ := configuredValidationClient(t, "default:taxonomy_term")
	requests := 0
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.Path {
		case "/sub/term_from_uri":
			return validationResponse(request, http.StatusOK, `[{"tid":[{"value":"19"}]},{"tid":[{"value":"20"}]}]`), nil
		default:
			return validationResponse(request, http.StatusOK, `{"data":[]}`), nil
		}
	})
	budget, err := validationcontext.NewRequestBudget(10, 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx := validationcontext.WithRequestBudget(context.Background(), budget)
	_, err = client.EntityReferenceExists(ctx, validationcontext.EntityReferenceQuery{
		EntityType: "taxonomy_term", Bundles: []string{"genre", "person"}, Field: "field_subject",
		SourceType: "entity_reference", Handler: "default:taxonomy_term", Kind: validationcontext.EntityReferenceURI, Value: "https://id.example/term",
	})
	var exceeded *validationcontext.ErrBudgetExceeded
	if !errors.As(err, &exceeded) || exceeded.Kind != validationcontext.BudgetNetworkRequest || exceeded.Limit != 2 {
		t.Fatalf("error = %v, want network budget exhaustion", err)
	}
	if requests != 2 {
		t.Fatalf("HTTP requests = %d, want exactly two before fail-closed budget error", requests)
	}
}

func TestValidationDynamicAllowedProviderIsBoundButNeverExecuted(t *testing.T) {
	client, snapshot, compiled := configuredValidationClient(t, "default:taxonomy_term")
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		t.Fatalf("provider caused an HTTP request to %s", request.URL)
		return nil, nil
	})
	query := validationcontext.AllowedValueQuery{
		Field: "field_access", SourceType: "list_string", Provider: "repository_allowed_values", Value: "public",
		Operation: validationcontext.OperationCreate, Bundle: "article",
		ProfileFingerprint: compiled.Fingerprint(), ModelFingerprint: snapshot.Fingerprint.Value,
	}
	_, err := client.AllowedValue(context.Background(), query)
	var unavailable *validationcontext.ErrCapabilityUnavailable
	if !errors.As(err, &unavailable) || unavailable.Capability != validationcontext.CapabilityAllowedValue {
		t.Fatalf("error = %v", err)
	}
	query.Provider = "attacker_callback"
	if _, err := client.AllowedValue(context.Background(), query); err == nil || !strings.Contains(err.Error(), "provider does not match") {
		t.Fatalf("provider mismatch error = %v", err)
	}
}

func TestConfigureValidationModelRejectsUnsafeJSONAPIPath(t *testing.T) {
	client, snapshot, compiled := validationClientParts(t, "default:taxonomy_term")
	client.BaseURL = "https://repo.example.org/%2fjsonapi"
	client.SystemProfile = compiled
	if err := client.ConfigureValidationModel(snapshot); err == nil || !strings.Contains(err.Error(), "encoded path") {
		t.Fatalf("error = %v", err)
	}
}

func configuredValidationClient(t *testing.T, handler string) (*Client, *model.Snapshot, *profile.Compiled) {
	t.Helper()
	client, snapshot, compiled := validationClientParts(t, handler)
	client.SystemProfile = compiled
	if err := client.ConfigureValidationModel(snapshot); err != nil {
		t.Fatal(err)
	}
	return client, snapshot, compiled
}

func validationClientParts(t *testing.T, handler string) (*Client, *model.Snapshot, *profile.Compiled) {
	t.Helper()
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{
			{EntityType: "node", Bundle: "article", Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "uid", SourceType: "entity_reference", Kind: model.ValueReference, Cardinality: 1, Reference: &model.Reference{EntityType: "user"}},
				{Path: "field_member_of", SourceType: "entity_reference", Kind: model.ValueReference, Cardinality: -1, InstanceSettings: map[string]any{"handler": "default:node"}, Reference: &model.Reference{EntityType: "node", Bundles: []string{"article"}}},
				{Path: "field_subject", SourceType: "entity_reference", Kind: model.ValueReference, Cardinality: -1, InstanceSettings: map[string]any{"handler": handler}, Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: []string{"person", "genre"}}},
				{Path: "field_access", SourceType: "list_string", Kind: model.ValueText, Cardinality: 1, StorageSettings: map[string]any{"allowed_values_function": "repository_allowed_values"}},
			}},
			{EntityType: "taxonomy_term", Bundle: "genre", Fields: []model.Field{{Path: "name", SourceType: "string", Kind: model.ValueText, Cardinality: 1}}},
			{EntityType: "taxonomy_term", Bundle: "person", Fields: []model.Field{{Path: "name", SourceType: "string", Kind: model.ValueText, Cardinality: 1}}},
		},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "validation-article", EntityType: "node", Bundle: "article",
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	return NewClient("https://repo.example.org/sub/jsonapi"), snapshot, compiled
}

func validationResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: request,
	}
}
