package httpapi

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/spec"
	"github.com/lehigh-university-libraries/crosswalk/validationcontext"
)

type validationContextResolverStub struct {
	node    func(context.Context, uint64) (bool, error)
	entity  func(context.Context, EntityReferenceQuery) (bool, error)
	allowed func(context.Context, AllowedValueQuery) (bool, error)
	file    func(context.Context, string) (bool, error)
	tgn     func(context.Context, string) (bool, error)
	alias   func(context.Context, string) (bool, error)
}

func (r *validationContextResolverStub) AllowedValue(ctx context.Context, query AllowedValueQuery) (bool, error) {
	if r.allowed == nil {
		return true, nil
	}
	return r.allowed(ctx, query)
}

func (r *validationContextResolverStub) EntityReferenceExists(ctx context.Context, query EntityReferenceQuery) (bool, error) {
	if r.entity == nil {
		return true, nil
	}
	return r.entity(ctx, query)
}

func (r *validationContextResolverStub) NodeExists(ctx context.Context, nodeID uint64) (bool, error) {
	if r.node == nil {
		return true, nil
	}
	return r.node(ctx, nodeID)
}

func (r *validationContextResolverStub) FileReadable(ctx context.Context, normalizedPath string) (bool, error) {
	if r.file == nil {
		return true, nil
	}
	return r.file(ctx, normalizedPath)
}

func (r *validationContextResolverStub) TGNResolves(ctx context.Context, termID string) (bool, error) {
	if r.tgn == nil {
		return true, nil
	}
	return r.tgn(ctx, termID)
}

func (r *validationContextResolverStub) URLAliasAvailable(ctx context.Context, alias string) (bool, error) {
	if r.alias == nil {
		return true, nil
	}
	return r.alias(ctx, alias)
}

func TestCrosswalkEngineValidationContextIsExplicit(t *testing.T) {
	engine := NewCrosswalkEngine()
	if engine.HasValidationContext() {
		t.Fatal("new engine unexpectedly has a validation context")
	}
	rows := [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Parent Collection"},
		{"1", "Example", "Digital Document", "Example", "Text", "999"},
	}
	result, err := engine.Check(context.Background(), rows)
	if err != nil {
		t.Fatalf("deterministic Check() error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("deterministic Check() = %#v, want no deployment-aware finding", result)
	}

	if err := engine.ConfigureValidationContext(nil); !errors.Is(err, ErrValidationContextRequired) {
		t.Fatalf("ConfigureValidationContext(nil) error = %v", err)
	}
	var typedNil *validationContextResolverStub
	if err := engine.ConfigureValidationContext(typedNil); !errors.Is(err, ErrValidationContextRequired) {
		t.Fatalf("ConfigureValidationContext(typed nil) error = %v", err)
	}
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{}); err != nil {
		t.Fatalf("ConfigureValidationContext() error = %v", err)
	}
	if !engine.HasValidationContext() {
		t.Fatal("configured engine does not report its validation context")
	}
	var nilEngine *CrosswalkEngine
	if err := nilEngine.ConfigureValidationContext(&validationContextResolverStub{}); err == nil {
		t.Fatal("nil engine accepted a validation context")
	}
}

func TestCrosswalkEngineContextValidationUsesCanonicalMapping(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	labels := map[string]string{
		"id":                             "Batch key",
		"title":                          "Display heading",
		"field_model":                    "Content shape",
		"field_full_title":               "Expanded heading",
		"field_resource_type":            "Schema kind",
		"field_member_of":                "Repository ancestor",
		"supplemental_file":              "Attached paths",
		"field_subject_hierarchical_geo": "Authority terms",
	}
	for index := range transformation.Source.Fields {
		if label := labels[transformation.Source.Fields[index].Name]; label != "" {
			transformation.Source.Fields[index].Label = label
		}
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatalf("SealFingerprint() error = %v", err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatalf("NewCrosswalkEngineWithSpec() error = %v", err)
	}

	var nodeIDs []uint64
	var paths, tgnIDs []string
	resolver := &validationContextResolverStub{
		node: func(_ context.Context, nodeID uint64) (bool, error) {
			nodeIDs = append(nodeIDs, nodeID)
			return nodeID != 43, nil
		},
		file: func(_ context.Context, path string) (bool, error) {
			paths = append(paths, path)
			return !strings.HasSuffix(path, "/two.pdf"), nil
		},
		tgn: func(_ context.Context, termID string) (bool, error) {
			tgnIDs = append(tgnIDs, termID)
			return termID != "7002", nil
		},
	}
	if err := engine.ConfigureValidationContext(resolver); err != nil {
		t.Fatalf("ConfigureValidationContext() error = %v", err)
	}

	result, err := engine.Check(context.Background(), [][]string{
		{"Batch key", "Display heading", "Content shape", "Expanded heading", "Schema kind", "Repository ancestor", "Attached paths", "Authority terms"},
		{"1", "Example", "Digital Document", "Example", "Text", "42 ; 43", "one.pdf ; two.pdf", "http://vocab.getty.edu/page/tgn/7001 ; https://vocab.getty.edu/tgn/7002"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !slices.Equal(nodeIDs, []uint64{42, 43}) {
		t.Errorf("NodeExists IDs = %v", nodeIDs)
	}
	if !slices.Equal(paths, []string{"/mnt/islandora_staging/one.pdf", "/mnt/islandora_staging/two.pdf"}) {
		t.Errorf("FileReadable paths = %v", paths)
	}
	if !slices.Equal(tgnIDs, []string{"7001", "7002"}) {
		t.Errorf("TGNResolves IDs = %v", tgnIDs)
	}
	checks := map[string]string{
		"F2": "Drupal node 43",
		"G2": "/mnt/islandora_staging/two.pdf",
		"H2": "Getty TGN term 7002",
	}
	if len(result) != len(checks) {
		t.Errorf("Check() = %#v, want %d contextual findings", result, len(checks))
	}
	for cell, want := range checks {
		if !strings.Contains(result[cell], want) {
			t.Errorf("Check()[%s] = %q, want substring %q; all = %#v", cell, result[cell], want, result)
		}
	}
	for _, want := range []string{"missing or unreadable", "read permission on the file", "read/traverse permission on every parent directory"} {
		if !strings.Contains(result["G2"], want) {
			t.Errorf("Check()[G2] = %q, want remediation substring %q", result["G2"], want)
		}
	}
}

func TestCrosswalkEngineCombinesDeterministicAndContextFindings(t *testing.T) {
	engine := NewCrosswalkEngine()
	var nodeCalls int
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{node: func(_ context.Context, nodeID uint64) (bool, error) {
		nodeCalls++
		return nodeID != 404, nil
	}}); err != nil {
		t.Fatal(err)
	}

	result, err := engine.Check(context.Background(), [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Parent Collection"},
		{"1", "", "Digital Document", "Example", "Text", "404"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if nodeCalls != 1 {
		t.Fatalf("NodeExists calls = %d, want 1 despite deterministic diagnostics", nodeCalls)
	}
	if !strings.Contains(result["B2"], "required") || !strings.Contains(result["F2"], "Drupal node 404") {
		t.Fatalf("Check() = %#v, want deterministic title and contextual parent findings", result)
	}
}

func TestCrosswalkEngineMemoizesContextQueriesPerCheck(t *testing.T) {
	engine := NewCrosswalkEngine()
	var nodeCalls int
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{node: func(_ context.Context, nodeID uint64) (bool, error) {
		nodeCalls++
		return false, nil
	}}); err != nil {
		t.Fatal(err)
	}

	result, err := engine.Check(context.Background(), [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Parent Collection"},
		{"1", "One", "Digital Document", "One", "Text", "404"},
		{"2", "Two", "Digital Document", "Two", "Text", "404"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if nodeCalls != 1 {
		t.Fatalf("NodeExists calls = %d, want one memoized call", nodeCalls)
	}
	for _, cell := range []string{"F2", "F3"} {
		if !strings.Contains(result[cell], "Drupal node 404") {
			t.Errorf("Check()[%s] = %q, all = %#v", cell, result[cell], result)
		}
	}
}

func TestCrosswalkEngineLookupBudgetStopsBeforeNPlusOneResolverCall(t *testing.T) {
	engine := NewCrosswalkEngine()
	var nodeCalls int
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{node: func(_ context.Context, _ uint64) (bool, error) {
		nodeCalls++
		return true, nil
	}}); err != nil {
		t.Fatal(err)
	}
	rows := [][]string{{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Parent Collection"}}
	for index := 1; index <= 3; index++ {
		rows = append(rows, []string{
			strconv.Itoa(index), "Example", "Digital Document", "Example", "Text", strconv.Itoa(100 + index),
		})
	}
	budget, err := validationcontext.NewRequestBudget(2, 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx := validationcontext.WithRequestBudget(context.Background(), budget)
	result, err := engine.Check(ctx, rows)
	var exceeded *validationcontext.ErrBudgetExceeded
	if !errors.As(err, &exceeded) || exceeded.Kind != validationcontext.BudgetLookup || exceeded.Limit != 2 {
		t.Fatalf("Check() error = %v, want lookup budget exhaustion", err)
	}
	if result != nil {
		t.Fatalf("Check() result = %#v after budget exhaustion", result)
	}
	if nodeCalls != 2 {
		t.Fatalf("NodeExists calls = %d, want two before fail-closed budget error", nodeCalls)
	}
}

func TestCrosswalkEngineContextValidationHonorsOperationAndPredicate(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	for index := range transformation.Source.Fields {
		field := &transformation.Source.Fields[index]
		switch field.Name {
		case "node_id":
			field.Validations = nil
		case "field_member_of":
			field.Validations = []spec.Validation{{
				Rule:       spec.ValidationContextNodeExists,
				Phase:      spec.ValidationPhaseContext,
				Operations: []spec.Operation{spec.OperationUpdate},
				When: &spec.ValidationWhen{
					Field:    "field_model",
					Operator: spec.ValidationOperatorIn,
					Values:   []string{"Image"},
				},
			}}
		}
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatalf("SealFingerprint() error = %v", err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatalf("NewCrosswalkEngineWithSpec() error = %v", err)
	}
	var nodeIDs []uint64
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{node: func(_ context.Context, nodeID uint64) (bool, error) {
		nodeIDs = append(nodeIDs, nodeID)
		return true, nil
	}}); err != nil {
		t.Fatalf("ConfigureValidationContext() error = %v", err)
	}

	result, err := engine.Check(context.Background(), [][]string{
		{"Upload ID", "Node ID", "Parent Collection", "Object Model", "Title", "Full Title", "Resource Type"},
		{"", "9", "41", "Image", "", "", ""},
		{"", "10", "42", "Digital Document", "", "", ""},
		{"1", "", "43", "Image", "Example", "Example", "Still Image"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("Check() = %#v", result)
	}
	if !slices.Equal(nodeIDs, []uint64{41}) {
		t.Fatalf("NodeExists IDs = %v, want only update+Image value 41", nodeIDs)
	}
}

func TestCrosswalkEngineContextValidationFailsClosed(t *testing.T) {
	rows := [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Parent Collection"},
		{"1", "Example", "Digital Document", "Example", "Text", "42"},
	}
	t.Run("resolver error", func(t *testing.T) {
		failure := errors.New("Drupal unavailable")
		engine := NewCrosswalkEngine()
		if err := engine.ConfigureValidationContext(&validationContextResolverStub{node: func(context.Context, uint64) (bool, error) {
			return false, failure
		}}); err != nil {
			t.Fatalf("ConfigureValidationContext() error = %v", err)
		}
		result, err := engine.Check(context.Background(), rows)
		if !errors.Is(err, failure) || !strings.Contains(err.Error(), `context validation "context_node_exists"`) {
			t.Fatalf("Check() error = %v, want wrapped resolver error", err)
		}
		if result != nil {
			t.Fatalf("Check() result = %#v after resolver failure", result)
		}
	})

	t.Run("cancellation during resolver", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		engine := NewCrosswalkEngine()
		if err := engine.ConfigureValidationContext(&validationContextResolverStub{node: func(context.Context, uint64) (bool, error) {
			cancel()
			return false, nil
		}}); err != nil {
			t.Fatalf("ConfigureValidationContext() error = %v", err)
		}
		if _, err := engine.Check(ctx, rows); !errors.Is(err, context.Canceled) {
			t.Fatalf("Check() error = %v, want context.Canceled", err)
		}
	})
}

func TestCrosswalkEngineNeverPassesArbitraryTGNTargets(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	for index := range transformation.Source.Fields {
		field := &transformation.Source.Fields[index]
		if field.Name == "field_subject_hierarchical_geo" {
			field.Validations = []spec.Validation{{Rule: spec.ValidationContextTGNResolves, Phase: spec.ValidationPhaseContext}}
			break
		}
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatalf("SealFingerprint() error = %v", err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatalf("NewCrosswalkEngineWithSpec() error = %v", err)
	}
	called := false
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{tgn: func(context.Context, string) (bool, error) {
		called = true
		return true, nil
	}}); err != nil {
		t.Fatalf("ConfigureValidationContext() error = %v", err)
	}
	result, err := engine.Check(context.Background(), [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Hierarchical Geographic (Getty TGN)"},
		{"1", "Example", "Digital Document", "Example", "Text", "http://127.0.0.1/admin"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if called {
		t.Fatal("TGN resolver received a spreadsheet-selected network target")
	}
	if message := result["F2"]; !strings.Contains(message, "Invalid Getty TGN URI") {
		t.Fatalf("Check()[F2] = %q, all = %#v", message, result)
	}
}

func TestCrosswalkEnginePassesEntityReferencesAsTypedInertQueries(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	for index := range transformation.Source.Fields {
		field := &transformation.Source.Fields[index]
		if field.Name == "field_subject_lcsh" {
			field.SourceType = "typed_relation"
			field.Validations = []spec.Validation{
				{Rule: spec.ValidationTypedRelation, Values: []string{"relators:cre"}, EntityType: "taxonomy_term", Bundles: []string{"subject"}},
				{Rule: spec.ValidationContextEntityExists, Phase: spec.ValidationPhaseContext, EntityType: "taxonomy_term", Bundles: []string{"subject"}},
			}
			break
		}
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatalf("SealFingerprint() error = %v", err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatalf("NewCrosswalkEngineWithSpec() error = %v", err)
	}
	var queries []EntityReferenceQuery
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{entity: func(_ context.Context, query EntityReferenceQuery) (bool, error) {
		queries = append(queries, query)
		return true, nil
	}}); err != nil {
		t.Fatalf("ConfigureValidationContext() error = %v", err)
	}

	result, err := engine.Check(context.Background(), [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Subject Topic (LCSH)"},
		{"1", "Example", "Digital Document", "Example", "Text", "relators:cre:http://127.0.0.1/admin"},
	})
	if err != nil || len(result) != 0 {
		t.Fatalf("Check() = %#v, %v", result, err)
	}
	want := EntityReferenceQuery{
		EntityType: "taxonomy_term", Bundles: []string{"subject"},
		Field: "field_subject_lcsh", SourceType: "typed_relation",
		Kind: EntityReferenceURI, Value: "http://127.0.0.1/admin",
	}
	if len(queries) != 1 || queries[0].EntityType != want.EntityType || queries[0].Field != want.Field || queries[0].SourceType != want.SourceType || queries[0].Handler != want.Handler || queries[0].Kind != want.Kind || queries[0].Value != want.Value || !slices.Equal(queries[0].Bundles, want.Bundles) {
		t.Fatalf("EntityReferenceExists queries = %#v, want %#v", queries, want)
	}
}

func TestCrosswalkEngineAllowsOnlyMissingTaxonomyNamesWhenDeclared(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	for index := range transformation.Source.Fields {
		field := &transformation.Source.Fields[index]
		if field.Name == "field_subject_lcsh" {
			field.SourceType = "entity_reference"
			field.Validations = []spec.Validation{
				{Rule: spec.ValidationEntityReference, EntityType: "taxonomy_term", Bundles: []string{"subject"}},
				{Rule: spec.ValidationContextEntityExists, Phase: spec.ValidationPhaseContext, EntityType: "taxonomy_term", Bundles: []string{"subject"}, AllowNewNames: true},
			}
			break
		}
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []EntityReferenceKind
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{entity: func(_ context.Context, query EntityReferenceQuery) (bool, error) {
		kinds = append(kinds, query.Kind)
		return false, nil
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Check(context.Background(), [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Subject Topic (LCSH)"},
		{"1", "Example", "Digital Document", "Example", "Text", "New Topic ; 42 ; https://id.example/term"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result["F2"]; strings.Count(got, "does not exist") != 2 {
		t.Fatalf("Check()[F2] = %q, want missing ID and URI findings only; all = %#v", got, result)
	}
	if want := []EntityReferenceKind{EntityReferenceName, EntityReferenceID, EntityReferenceURI}; !slices.Equal(kinds, want) {
		t.Fatalf("query kinds = %#v, want %#v", kinds, want)
	}
}

func TestCrosswalkEngineChecksURLAliasAvailabilityAndMemoizes(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	transformation.Source.Fields = append(transformation.Source.Fields, spec.Field{
		Name: "url_alias", Label: "URL Alias", Hub: "Extra.url_alias", Cardinality: 1,
		Operations:  []spec.Operation{spec.OperationCreate, spec.OperationUpdate},
		Validations: []spec.Validation{{Rule: spec.ValidationContextURLAliasAvailable, Phase: spec.ValidationPhaseContext}},
	})
	transformation.Target.Fields = append(transformation.Target.Fields, spec.Field{
		Name: "url_alias", Hub: "Extra.url_alias", Operations: []spec.Operation{spec.OperationCreate, spec.OperationUpdate},
	})
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{alias: func(_ context.Context, alias string) (bool, error) {
		calls++
		if alias != "/existing" {
			t.Fatalf("alias = %q", alias)
		}
		return false, nil
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Check(context.Background(), [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "URL Alias"},
		{"1", "One", "Digital Document", "One", "Text", "/existing"},
		{"2", "Two", "Digital Document", "Two", "Text", "/existing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, cell := range []string{"F2", "F3"} {
		if !strings.Contains(result[cell], "already exists") {
			t.Errorf("Check()[%s] = %q; all=%#v", cell, result[cell], result)
		}
	}
	if calls != 1 {
		t.Fatalf("URL alias resolver calls = %d, want 1", calls)
	}
}

func TestCrosswalkEnginePassesNonTaxonomyReferencesAsTypedIDQueries(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	for index := range transformation.Source.Fields {
		field := &transformation.Source.Fields[index]
		if field.Name == "field_subject_lcsh" {
			field.SourceType = "entity_reference"
			field.Validations = []spec.Validation{
				{Rule: spec.ValidationEntityReference, EntityType: "node", Bundles: []string{"collection"}},
				{Rule: spec.ValidationContextEntityExists, Phase: spec.ValidationPhaseContext, EntityType: "node", Bundles: []string{"collection"}, Handler: "default:node"},
			}
			break
		}
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatal(err)
	}
	var queries []EntityReferenceQuery
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{entity: func(_ context.Context, query EntityReferenceQuery) (bool, error) {
		queries = append(queries, query)
		return true, nil
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Check(context.Background(), [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Subject Topic (LCSH)"},
		{"1", "Example", "Digital Document", "Example", "Text", "42"},
	})
	if err != nil || len(result) != 0 {
		t.Fatalf("Check() = %#v, %v", result, err)
	}
	want := EntityReferenceQuery{
		EntityType: "node", Bundles: []string{"collection"}, Field: "field_subject_lcsh",
		SourceType: "entity_reference", Handler: "default:node", Kind: EntityReferenceID, Value: "42",
	}
	if len(queries) != 1 || !reflect.DeepEqual(queries[0], want) {
		t.Fatalf("EntityReferenceExists queries = %#v, want %#v", queries, want)
	}
}

func TestCrosswalkEngineResolvesDynamicAllowedValuesByCanonicalField(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	for index := range transformation.Source.Fields {
		field := &transformation.Source.Fields[index]
		if field.Name == "field_language" {
			field.Label = "Display language"
			field.SourceType = "list_string"
			field.Validations = append(field.Validations, spec.Validation{
				Rule: spec.ValidationContextAllowedValue, Phase: spec.ValidationPhaseContext, Provider: "repository_languages",
			})
			break
		}
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatal(err)
	}
	var queries []AllowedValueQuery
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{allowed: func(_ context.Context, query AllowedValueQuery) (bool, error) {
		queries = append(queries, query)
		return query.Value == "en", nil
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Check(context.Background(), [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Display language"},
		{"1", "Example", "Digital Document", "Example", "Text", "xx-invalid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := AllowedValueQuery{
		Field: "field_language", SourceType: "list_string", Provider: "repository_languages", Value: "xx-invalid",
		Operation: validationcontext.OperationCreate, Bundle: transformation.Fingerprint.Bundle,
		ProfileFingerprint: transformation.Fingerprint.Profile, ModelFingerprint: transformation.Fingerprint.Model,
		Fields: []CanonicalFieldValue{
			{Field: "id", Values: []string{"1"}},
			{Field: "title", Values: []string{"Example"}},
			{Field: "field_model", Values: []string{"Digital Document"}},
			{Field: "field_full_title", Values: []string{"Example"}},
			{Field: "field_resource_type", Values: []string{"Text"}},
			{Field: "field_language", SourceType: "list_string", Values: []string{"xx-invalid"}},
		},
	}
	if len(queries) != 1 || !reflect.DeepEqual(queries[0], want) {
		t.Fatalf("AllowedValue queries = %#v, want %#v", queries, want)
	}
	if !strings.Contains(result["F2"], "repository_languages") {
		t.Fatalf("Check() = %#v", result)
	}
}

func TestCrosswalkEngineSkipsAllowedValueResolverForAmbiguousOrUnsafeRows(t *testing.T) {
	newEngine := func(t *testing.T) (*CrosswalkEngine, *int) {
		t.Helper()
		transformation := spec.FabricatorWorkbench()
		for index := range transformation.Source.Fields {
			field := &transformation.Source.Fields[index]
			if field.Name == "field_language" {
				field.SourceType = "list_string"
				field.Validations = append(field.Validations, spec.Validation{
					Rule: spec.ValidationContextAllowedValue, Phase: spec.ValidationPhaseContext, Provider: "repository_languages",
				})
				break
			}
		}
		if err := transformation.SealFingerprint(); err != nil {
			t.Fatal(err)
		}
		engine, err := NewCrosswalkEngineWithSpec(transformation)
		if err != nil {
			t.Fatal(err)
		}
		calls := new(int)
		if err := engine.ConfigureValidationContext(&validationContextResolverStub{allowed: func(context.Context, AllowedValueQuery) (bool, error) {
			*calls++
			return true, nil
		}}); err != nil {
			t.Fatal(err)
		}
		return engine, calls
	}

	t.Run("malformed update node ID", func(t *testing.T) {
		engine, calls := newEngine(t)
		result, err := engine.Check(context.Background(), [][]string{
			{"Node ID", "Language"},
			{"not-a-node", "en"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if *calls != 0 || !strings.Contains(result["B2"], "valid Node ID") {
			t.Fatalf("AllowedValue calls = %d, Check() = %#v", *calls, result)
		}
	})

	t.Run("control character", func(t *testing.T) {
		engine, calls := newEngine(t)
		result, err := engine.Check(context.Background(), [][]string{
			{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Language"},
			{"1", "Example", "Digital Document", "Example", "Text", "en\nmalformed"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if *calls != 0 || !strings.Contains(result["F2"], "control character") {
			t.Fatalf("AllowedValue calls = %d, Check() = %#v", *calls, result)
		}
	})
}

func TestCrosswalkEngineReportsEveryInvalidContextCandidateInCell(t *testing.T) {
	transformation := spec.FabricatorWorkbench()
	for index := range transformation.Source.Fields {
		field := &transformation.Source.Fields[index]
		if field.Name == "field_language" {
			field.Codec = "multi"
			field.SourceType = "list_string"
			field.Validations = append(field.Validations, spec.Validation{
				Rule: spec.ValidationContextAllowedValue, Phase: spec.ValidationPhaseContext, Provider: "repository_languages",
			})
			break
		}
	}
	if err := transformation.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	engine, err := NewCrosswalkEngineWithSpec(transformation)
	if err != nil {
		t.Fatal(err)
	}
	var candidates []string
	if err := engine.ConfigureValidationContext(&validationContextResolverStub{allowed: func(_ context.Context, query AllowedValueQuery) (bool, error) {
		candidates = append(candidates, query.Value)
		return false, nil
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Check(context.Background(), [][]string{
		{"Upload ID", "Title", "Object Model", "Full Title", "Resource Type", "Language"},
		{"1", "Example", "Digital Document", "Example", "Text", "bad-one ; bad-two"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(candidates, []string{"bad-one", "bad-two"}) {
		t.Fatalf("AllowedValue candidates = %#v", candidates)
	}
	if message := result["F2"]; !strings.Contains(message, `"bad-one"`) || !strings.Contains(message, `"bad-two"`) {
		t.Fatalf("Check()[F2] = %q, all = %#v", message, result)
	}
}
