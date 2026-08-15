package validationcontext

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type nodeResolverFunc func(context.Context, uint64) (bool, error)

func (f nodeResolverFunc) NodeExists(ctx context.Context, nodeID uint64) (bool, error) {
	return f(ctx, nodeID)
}

type entityResolverFunc func(context.Context, EntityReferenceQuery) (bool, error)

func (f entityResolverFunc) EntityReferenceExists(ctx context.Context, query EntityReferenceQuery) (bool, error) {
	return f(ctx, query)
}

type allowedValueResolverFunc func(context.Context, AllowedValueQuery) (bool, error)

func (f allowedValueResolverFunc) AllowedValue(ctx context.Context, query AllowedValueQuery) (bool, error) {
	return f(ctx, query)
}

type fileResolverFunc func(context.Context, string) (bool, error)

func (f fileResolverFunc) FileReadable(ctx context.Context, path string) (bool, error) {
	return f(ctx, path)
}

type tgnResolverFunc func(context.Context, string) (bool, error)

func (f tgnResolverFunc) TGNResolves(ctx context.Context, termID string) (bool, error) {
	return f(ctx, termID)
}

type urlAliasResolverFunc func(context.Context, string) (bool, error)

func (f urlAliasResolverFunc) URLAliasAvailable(ctx context.Context, alias string) (bool, error) {
	return f(ctx, alias)
}

func TestCompositeDelegatesCapabilities(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	composite := Composite{
		NodeExistenceResolver: nodeResolverFunc(func(_ context.Context, nodeID uint64) (bool, error) {
			calls.Add(1)
			return nodeID == 42, nil
		}),
		EntityReferenceResolver: entityResolverFunc(func(_ context.Context, query EntityReferenceQuery) (bool, error) {
			calls.Add(1)
			return query.Kind == EntityReferenceID && query.Value == "42", nil
		}),
		AllowedValueResolver: allowedValueResolverFunc(func(_ context.Context, query AllowedValueQuery) (bool, error) {
			calls.Add(1)
			return query.Operation == OperationCreate && query.Value == "eng", nil
		}),
		FileReadabilityResolver: fileResolverFunc(func(_ context.Context, path string) (bool, error) {
			calls.Add(1)
			return path == "objects/file.pdf", nil
		}),
		TGNResolver: tgnResolverFunc(func(_ context.Context, termID string) (bool, error) {
			calls.Add(1)
			return termID == "123", nil
		}),
		URLAliasAvailabilityResolver: urlAliasResolverFunc(func(_ context.Context, alias string) (bool, error) {
			calls.Add(1)
			return alias == "/available", nil
		}),
	}

	checks := []func() (bool, error){
		func() (bool, error) { return composite.NodeExists(context.Background(), 42) },
		func() (bool, error) {
			return composite.EntityReferenceExists(context.Background(), EntityReferenceQuery{Kind: EntityReferenceID, Value: "42"})
		},
		func() (bool, error) {
			return composite.AllowedValue(context.Background(), AllowedValueQuery{Operation: OperationCreate, Value: "eng"})
		},
		func() (bool, error) { return composite.FileReadable(context.Background(), "objects/file.pdf") },
		func() (bool, error) { return composite.TGNResolves(context.Background(), "123") },
		func() (bool, error) { return composite.URLAliasAvailable(context.Background(), "/available") },
	}
	for i, check := range checks {
		if ok, err := check(); err != nil || !ok {
			t.Fatalf("capability %d = (%t, %v), want (true, nil)", i, ok, err)
		}
	}
	if got := calls.Load(); got != int64(len(checks)) {
		t.Fatalf("resolver calls = %d, want %d", got, len(checks))
	}
}

func TestCompositeMissingCapabilitiesFailClosed(t *testing.T) {
	t.Parallel()

	var typedNil nodeResolverFunc
	tests := []struct {
		name       string
		capability Capability
		call       func() (bool, error)
	}{
		{name: "node", capability: CapabilityNodeExistence, call: func() (bool, error) {
			return (Composite{}).NodeExists(context.Background(), 1)
		}},
		{name: "typed nil node", capability: CapabilityNodeExistence, call: func() (bool, error) {
			return (Composite{NodeExistenceResolver: typedNil}).NodeExists(context.Background(), 1)
		}},
		{name: "entity", capability: CapabilityEntityReference, call: func() (bool, error) {
			return (Composite{}).EntityReferenceExists(context.Background(), EntityReferenceQuery{})
		}},
		{name: "allowed value", capability: CapabilityAllowedValue, call: func() (bool, error) {
			return (Composite{}).AllowedValue(context.Background(), AllowedValueQuery{})
		}},
		{name: "file", capability: CapabilityFileReadability, call: func() (bool, error) {
			return (Composite{}).FileReadable(context.Background(), "file")
		}},
		{name: "TGN", capability: CapabilityTGN, call: func() (bool, error) {
			return (Composite{}).TGNResolves(context.Background(), "1")
		}},
		{name: "URL alias", capability: CapabilityURLAliasAvailability, call: func() (bool, error) {
			return (Composite{}).URLAliasAvailable(context.Background(), "/available")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ok, err := test.call()
			if ok || err == nil {
				t.Fatalf("call = (%t, %v), want (false, error)", ok, err)
			}
			var unavailable *ErrCapabilityUnavailable
			if !errors.As(err, &unavailable) {
				t.Fatalf("error = %T %v, want *ErrCapabilityUnavailable", err, err)
			}
			if unavailable.Capability != test.capability {
				t.Fatalf("capability = %q, want %q", unavailable.Capability, test.capability)
			}
		})
	}
}

func TestCompositeResolverFailuresFailClosed(t *testing.T) {
	t.Parallel()

	rootErr := errors.New("repository unavailable")
	composite := Composite{NodeExistenceResolver: nodeResolverFunc(func(context.Context, uint64) (bool, error) {
		return true, rootErr
	})}
	ok, err := composite.NodeExists(context.Background(), 42)
	if ok || !errors.Is(err, rootErr) {
		t.Fatalf("NodeExists() = (%t, %v), want (false, wrapped root error)", ok, err)
	}
}

func TestCompositeRecoversCapabilityPanicWithoutLeakingValue(t *testing.T) {
	t.Parallel()

	const secret = "sensitive backend response"
	composite := Composite{FileReadabilityResolver: fileResolverFunc(func(context.Context, string) (bool, error) {
		panic(secret)
	})}
	ok, err := composite.FileReadable(context.Background(), "objects/file.pdf")
	if ok || err == nil {
		t.Fatalf("FileReadable() = (%t, %v), want (false, error)", ok, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("panic error leaked panic value: %v", err)
	}
}

func TestCompositeRejectsNilAndCanceledContextsBeforeDelegation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	composite := Composite{NodeExistenceResolver: nodeResolverFunc(func(context.Context, uint64) (bool, error) {
		calls.Add(1)
		return true, nil
	})}
	if ok, err := composite.NodeExists(nil, 42); ok || err == nil { //nolint:staticcheck // Deliberately exercise the nil-context guard.
		t.Fatalf("NodeExists(nil) = (%t, %v), want (false, error)", ok, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, err := composite.NodeExists(ctx, 42); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("NodeExists(canceled) = (%t, %v), want (false, context.Canceled)", ok, err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
}

func TestCompositeCopiesSliceBearingQueries(t *testing.T) {
	t.Parallel()

	entityQuery := EntityReferenceQuery{Bundles: []string{"islandora_object"}}
	allowedQuery := AllowedValueQuery{Fields: []CanonicalFieldValue{{
		Field: "field_language", Values: []string{"eng"},
	}}}
	composite := Composite{
		EntityReferenceResolver: entityResolverFunc(func(_ context.Context, query EntityReferenceQuery) (bool, error) {
			query.Bundles[0] = "mutated"
			return true, nil
		}),
		AllowedValueResolver: allowedValueResolverFunc(func(_ context.Context, query AllowedValueQuery) (bool, error) {
			query.Fields[0].Field = "mutated"
			query.Fields[0].Values[0] = "mutated"
			return true, nil
		}),
	}
	if ok, err := composite.EntityReferenceExists(context.Background(), entityQuery); err != nil || !ok {
		t.Fatalf("EntityReferenceExists() = (%t, %v)", ok, err)
	}
	if ok, err := composite.AllowedValue(context.Background(), allowedQuery); err != nil || !ok {
		t.Fatalf("AllowedValue() = (%t, %v)", ok, err)
	}
	if entityQuery.Bundles[0] != "islandora_object" {
		t.Fatalf("caller bundles mutated: %#v", entityQuery.Bundles)
	}
	if allowedQuery.Fields[0].Field != "field_language" || allowedQuery.Fields[0].Values[0] != "eng" {
		t.Fatalf("caller fields mutated: %#v", allowedQuery.Fields)
	}
}

func TestCompositeIsSafeForConcurrentDelegation(t *testing.T) {
	t.Parallel()

	composite := Composite{TGNResolver: tgnResolverFunc(func(context.Context, string) (bool, error) {
		return true, nil
	})}
	var wait sync.WaitGroup
	for range 64 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if ok, err := composite.TGNResolves(context.Background(), "123"); err != nil || !ok {
				t.Errorf("TGNResolves() = (%t, %v)", ok, err)
			}
		}()
	}
	wait.Wait()
}
