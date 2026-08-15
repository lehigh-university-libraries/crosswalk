package drupal

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	zenodoformat "github.com/lehigh-university-libraries/crosswalk/format/zenodo"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/reconcile"
)

func TestZenodoVersionDOIFindsAndExactlyMatchesExistingDrupalRecord(t *testing.T) {
	compiled := compiledStarterFinderProfile(t)
	policy, err := reconcile.NewPolicy(compiled.IdentifierRegistryConfig())
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := (&zenodoformat.Format{}).Parse(strings.NewReader(`{
		"id":8435696,
		"conceptrecid":"8435695",
		"doi":"10.5281/zenodo.8435696",
		"conceptdoi":"10.5281/zenodo.8435695",
		"metadata":{"title":"A Zenodo dataset"}
	}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := incoming[0].GetIdentifiers()[0].GetIdentityLevel(); got != hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_VERSION {
		t.Fatalf("incoming DOI identity level = %s", got)
	}

	client := NewClient("https://repo.example.org/jsonapi")
	client.SystemProfile = compiled
	doiLookups := make([]string, 0)
	conceptLookedUp := false
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		value := query.Get("filter[crosswalk-1][condition][value]")
		discriminator := query.Get("filter[crosswalk-2][condition][value]")
		body := `{"data":[]}`
		if discriminator == "doi" {
			doiLookups = append(doiLookups, value)
			if value == "10.5281/zenodo.8435695" || value == "https://doi.org/10.5281/zenodo.8435695" {
				conceptLookedUp = true
			}
			if value == "10.5281/zenodo.8435696" {
				body = `{"data":[{"type":"node--article","id":"11111111-1111-1111-1111-111111111111","attributes":{"drupal_internal__nid":42,"title":"A Zenodo dataset","field_identifier":[{"attr0":"doi","value":"10.5281/zenodo.8435696"}]}}]}`
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})

	report, err := (reconcile.Detector{Finder: client, Policy: policy}).Detect(
		context.Background(), []reconcile.Input{{Key: "zenodo-8435696", Record: incoming[0]}}, reconcile.ModeHold,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(doiLookups, ",") != "10.5281/zenodo.8435696,https://doi.org/10.5281/zenodo.8435696" {
		t.Fatalf("DOI lookup values = %#v", doiLookups)
	}
	if conceptLookedUp {
		t.Fatal("Zenodo concept DOI was used as exact lookup evidence")
	}
	result := report.Results[0]
	if result.Verdict != reconcile.VerdictDuplicate || len(result.Matches) != 1 || result.Matches[0].Kind != reconcile.MatchExactIdentifier || result.Matches[0].Candidate.RepositoryID != "42" {
		t.Fatalf("reconciliation result = %#v", result)
	}
	if levels := result.Matches[0].Candidate.Metadata.Identifiers; len(levels) == 0 || levels[0].IdentityLevel != hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK {
		t.Fatalf("existing Drupal DOI identity = %#v", levels)
	}
}

func TestCompiledProfileDrivesCompoundMetadataLookup(t *testing.T) {
	compiled := compiledFinderProfile(t)
	client := NewClient("https://repo.example.org/jsonapi")
	client.SystemProfile = compiled
	requests := 0
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Path != "/jsonapi/node/article" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		query := request.URL.Query()
		if query.Get("filter[crosswalk][group][conjunction]") != "AND" {
			t.Fatalf("missing AND group: %s", request.URL.RawQuery)
		}
		paths := []string{
			query.Get("filter[crosswalk-1][condition][path]"),
			query.Get("filter[crosswalk-2][condition][path]"),
		}
		if strings.Join(paths, ",") != "title,field_linked_agent.name" {
			t.Fatalf("condition paths = %#v", paths)
		}
		if query.Get("filter[crosswalk-2][condition][value]") != "Doe" {
			t.Fatalf("author value = %q", query.Get("filter[crosswalk-2][condition][value]"))
		}
		if query.Get("include") != "field_linked_agent" {
			t.Fatalf("include = %q", query.Get("include"))
		}
		body := `{"data":[{"type":"node--article","id":"11111111-1111-1111-1111-111111111111","attributes":{"drupal_internal__nid":42,"title":"A distinctive title","field_identifier":[{"attr0":"doi","value":"10.1234/example"}],"field_edtf_date_issued":[{"value":"2024"}]},"relationships":{"field_linked_agent":{"data":[{"type":"taxonomy_term--person","id":"22222222-2222-2222-2222-222222222222","meta":{"drupal_internal__target_id":906,"rel_type":"relators:aut"}}]}}}],"included":[{"type":"taxonomy_term--person","id":"22222222-2222-2222-2222-222222222222","attributes":{"name":"Doe, Jane"}}]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	candidates, err := client.Candidates(context.Background(), reconcile.Query{
		Strategy: reconcile.QueryByMetadata, Title: "A distinctive title", Authors: []string{"Jane Doe"}, Year: 2024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(candidates) != 1 || candidates[0].Record.GetTitle() != "A distinctive title" || len(candidates[0].Record.GetContributors()) != 1 {
		t.Fatalf("requests=%d candidates=%#v", requests, candidates)
	}
}

func TestCompiledProfileIdentifierLookupUsesDeclaredPredicateAndVariants(t *testing.T) {
	compiled := compiledFinderProfile(t)
	client := NewClient("https://repo.example.org/jsonapi")
	client.SystemProfile = compiled
	var requested []string
	client.HTTP = doerFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("filter[crosswalk-1][condition][path]") != "field_identifier.value" || query.Get("filter[crosswalk-2][condition][path]") != "field_identifier.attr0" || query.Get("filter[crosswalk-2][condition][value]") != "doi" {
			t.Fatalf("identifier query = %s", request.URL.RawQuery)
		}
		requested = append(requested, query.Get("filter[crosswalk-1][condition][value]"))
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Request: request}, nil
	})
	_, err := client.Candidates(context.Background(), reconcile.Query{
		Strategy:    reconcile.QueryByIdentifier,
		Identifiers: []reconcile.IdentifierKey{{Scheme: "doi", NamespaceURI: "https://doi.org/", Value: "10.1234/example", IdentityLevel: hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_WORK}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(requested, ",") != "10.1234/example,https://doi.org/10.1234/example" {
		t.Fatalf("variants = %#v", requested)
	}
}

func compiledFinderProfile(t *testing.T) *profile.Compiled {
	t.Helper()
	fields := []model.Field{
		{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
		{Path: "field_identifier", SourceType: "textfield_attr", Kind: model.ValueComposite, Cardinality: -1},
		{Path: "field_linked_agent", SourceType: "typed_relation", Kind: model.ValueTypedReference, Cardinality: -1, Reference: &model.Reference{EntityType: "taxonomy_term", Bundles: []string{"person"}}},
		{Path: "field_edtf_date_issued", SourceType: "edtf", Kind: model.ValueDate, Cardinality: 1},
	}
	snapshot := &model.Snapshot{Version: model.CurrentVersion, System: "drupal", Entities: []model.Entity{{EntityType: "node", Bundle: "article", Fields: fields}}}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	selector := func(path string) profile.FieldSelector {
		return profile.FieldSelector{EntityType: "node", Bundle: "article", Path: path}
	}
	identifier := selector("field_identifier")
	identifier.Attribute = "value"
	identifier.Where = &profile.FieldPredicate{Attribute: "attr0", Equals: "doi"}
	author := selector("field_linked_agent")
	author.Attribute = "name"
	date := selector("field_edtf_date_issued")
	date.Attribute = "value"
	definition := &profile.Definition{
		Version: profile.CurrentDefinitionVersion, Name: "finder-article", System: "drupal", ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: []profile.Mapping{
			{Field: selector("title"), Hub: "Title", Decode: "text", Encode: "text", Merge: profile.MergeFirstNonempty},
			{Field: identifier, Hub: "Identifiers", Decode: "typed-identifier", Encode: "typed-identifier", Merge: profile.MergeAppend},
			{Field: selector("field_linked_agent"), Hub: "Contributors", Decode: "typed-relation", Encode: "typed-relation", Merge: profile.MergeAppend},
			{Field: selector("field_edtf_date_issued"), Hub: "Dates.issued", Decode: "date", Encode: "date", Merge: profile.MergeAppend},
		},
		Identity: &profile.IdentityPolicy{
			Version: profile.CurrentIdentityVersion, Repository: profile.EntitySelector{EntityType: "node", Bundle: "article"},
			Identifiers: []profile.IdentifierRule{{
				Name: "doi", Scheme: "doi", IdentityLevel: profile.IdentityWork, Value: identifier,
				Pattern: `^10\.[0-9]{4,9}/\S+$`, Canonicalizer: "doi", Strength: profile.IdentifierStrong, Scope: profile.ScopeGlobal,
				Lookup: profile.LookupRule{Fields: []profile.FieldSelector{identifier}, Operator: profile.LookupContains, Variants: []string{"canonical", "doi-url"}},
			}},
			Metadata: &profile.MetadataIdentity{
				Title: &profile.FieldSelector{EntityType: "node", Bundle: "article", Path: "title"}, Contributors: []profile.FieldSelector{author}, Date: &date,
				Lookups: []profile.MetadataLookup{
					{Name: "title-author", Title: profile.LookupRule{Fields: []profile.FieldSelector{selector("title")}, Operator: profile.LookupContains, Variants: []string{"canonical", "title-phrase"}}, Contributors: &profile.LookupRule{Fields: []profile.FieldSelector{author}, Operator: profile.LookupContains, Variants: []string{"author-family"}}},
					{Name: "title-date", Title: profile.LookupRule{Fields: []profile.FieldSelector{selector("title")}, Operator: profile.LookupContains, Variants: []string{"canonical"}}, Date: &profile.LookupRule{Fields: []profile.FieldSelector{date}, Operator: profile.LookupExact, Variants: []string{"year"}}},
				}, MetadataOnly: profile.MetadataOnlyReview,
			},
		},
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func compiledStarterFinderProfile(t *testing.T) *profile.Compiled {
	t.Helper()
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion, System: "drupal",
		Entities: []model.Entity{{
			EntityType: "node", Bundle: "article",
			Fields: []model.Field{
				{Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1},
				{Path: "field_identifier", SourceType: "textfield_attr", Kind: model.ValueComposite, Cardinality: -1},
			},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: "starter-article", EntityType: "node", Bundle: "article",
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}
