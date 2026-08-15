package validationcontext

import (
	"context"
	"fmt"
	"sync"
)

const (
	// DefaultLookupLimit bounds distinct resolver queries in one validation
	// request. Cache hits do not consume this budget.
	DefaultLookupLimit = 4096
	// DefaultNetworkRequestLimit independently bounds outbound validation
	// fetches. Keeping this separate from logical lookups avoids charging an
	// ordinary one-request lookup twice against a single counter while still
	// bounding resolver-internal fan-out.
	DefaultNetworkRequestLimit = 4096
)

// BudgetKind identifies one independently bounded class of validation work.
type BudgetKind string

const (
	BudgetLookup         BudgetKind = "lookup"
	BudgetNetworkRequest BudgetKind = "network_request"
)

// ErrBudgetExceeded reports that a validation request exhausted a finite work
// budget. The rejected operation is not executed.
type ErrBudgetExceeded struct {
	Kind  BudgetKind
	Limit int
}

// Error implements error.
func (e *ErrBudgetExceeded) Error() string {
	if e == nil {
		return "validation work budget exceeded"
	}
	return fmt.Sprintf("validation %s budget exceeded (limit %d)", e.Kind, e.Limit)
}

// RequestBudget is shared by every deployment-aware resolver used for one
// validation request. Logical cache misses and actual network fetches have
// separate limits so resolver fan-out cannot escape the request boundary.
type RequestBudget struct {
	mu sync.Mutex

	lookupLimit  int
	networkLimit int
	lookups      int
	network      int
}

// NewRequestBudget returns a finite request budget.
func NewRequestBudget(lookupLimit, networkLimit int) (*RequestBudget, error) {
	if lookupLimit <= 0 || networkLimit <= 0 {
		return nil, fmt.Errorf("validation budget limits must be positive")
	}
	return &RequestBudget{lookupLimit: lookupLimit, networkLimit: networkLimit}, nil
}

// NewDefaultRequestBudget returns the production validation work limits.
func NewDefaultRequestBudget() *RequestBudget {
	budget, err := NewRequestBudget(DefaultLookupLimit, DefaultNetworkRequestLimit)
	if err != nil {
		panic(err)
	}
	return budget
}

type requestBudgetContextKey struct{}

// WithRequestBudget attaches budget to ctx. A nil budget leaves ctx unchanged.
func WithRequestBudget(ctx context.Context, budget *RequestBudget) context.Context {
	if budget == nil {
		return ctx
	}
	return context.WithValue(ctx, requestBudgetContextKey{}, budget)
}

// EnsureRequestBudget preserves an existing caller-supplied budget or installs
// the default finite limits. Callers use this at the outer request/resolver
// boundary so every nested lookup sees the same counters.
func EnsureRequestBudget(ctx context.Context) context.Context {
	if RequestBudgetFromContext(ctx) != nil {
		return ctx
	}
	return WithRequestBudget(ctx, NewDefaultRequestBudget())
}

// RequestBudgetFromContext returns the shared validation budget, when present.
func RequestBudgetFromContext(ctx context.Context) *RequestBudget {
	if ctx == nil {
		return nil
	}
	budget, _ := ctx.Value(requestBudgetContextKey{}).(*RequestBudget)
	return budget
}

// ConsumeLookup charges one distinct resolver query. A context without a
// budget is rejected so a missed outer-boundary installation cannot silently
// make validation unbounded.
func ConsumeLookup(ctx context.Context) error {
	budget := RequestBudgetFromContext(ctx)
	if budget == nil {
		return fmt.Errorf("validation request budget is not configured")
	}
	return budget.consume(BudgetLookup)
}

// ConsumeNetworkRequest charges one outbound validation fetch.
func ConsumeNetworkRequest(ctx context.Context) error {
	budget := RequestBudgetFromContext(ctx)
	if budget == nil {
		return fmt.Errorf("validation request budget is not configured")
	}
	return budget.consume(BudgetNetworkRequest)
}

func (b *RequestBudget) consume(kind BudgetKind) error {
	if b == nil {
		return fmt.Errorf("validation request budget is nil")
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	switch kind {
	case BudgetLookup:
		if b.lookups >= b.lookupLimit {
			return &ErrBudgetExceeded{Kind: kind, Limit: b.lookupLimit}
		}
		b.lookups++
	case BudgetNetworkRequest:
		if b.network >= b.networkLimit {
			return &ErrBudgetExceeded{Kind: kind, Limit: b.networkLimit}
		}
		b.network++
	default:
		return fmt.Errorf("unsupported validation budget kind %q", kind)
	}
	return nil
}
