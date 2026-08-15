package validationcontext

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

// Capability identifies one deployment-aware validation service.
type Capability string

const (
	// CapabilityNodeExistence checks repository node identifiers.
	CapabilityNodeExistence Capability = "node_existence"
	// CapabilityEntityReference checks typed repository entity references.
	CapabilityEntityReference Capability = "entity_reference"
	// CapabilityAllowedValue checks dynamic field values.
	CapabilityAllowedValue Capability = "allowed_value"
	// CapabilityFileReadability checks staging paths.
	CapabilityFileReadability Capability = "file_readability"
	// CapabilityTGN checks Getty TGN identifiers.
	CapabilityTGN Capability = "tgn"
	// CapabilityURLAliasAvailability checks whether a Drupal URL alias is unused.
	CapabilityURLAliasAvailability Capability = "url_alias_availability"
)

// ErrCapabilityUnavailable reports that a Composite has no usable resolver for
// a requested capability. Use errors.As to inspect Capability.
type ErrCapabilityUnavailable struct {
	Capability Capability
}

// Error implements error.
func (e *ErrCapabilityUnavailable) Error() string {
	if e == nil || e.Capability == "" {
		return "validation capability is unavailable"
	}
	return fmt.Sprintf("validation capability %q is unavailable", e.Capability)
}

// Composite combines independently configured validation capabilities. A
// missing or typed-nil capability returns ErrCapabilityUnavailable rather than
// silently accepting the candidate. Composite has no mutable state; each
// configured capability must itself be safe for concurrent use.
type Composite struct {
	NodeExistenceResolver
	EntityReferenceResolver
	AllowedValueResolver
	FileReadabilityResolver
	TGNResolver
	URLAliasAvailabilityResolver
}

var _ Resolver = Composite{}

// NodeExists delegates to the configured node-existence capability.
func (c Composite) NodeExists(ctx context.Context, nodeID uint64) (bool, error) {
	if interfaceIsNil(c.NodeExistenceResolver) {
		return false, capabilityUnavailable(CapabilityNodeExistence)
	}
	return callCapability(ctx, CapabilityNodeExistence, func() (bool, error) {
		return c.NodeExistenceResolver.NodeExists(ctx, nodeID)
	})
}

// EntityReferenceExists delegates to the configured entity-reference
// capability. Slice-bearing query fields are copied before delegation so a
// resolver cannot mutate its caller's model constraints.
func (c Composite) EntityReferenceExists(ctx context.Context, query EntityReferenceQuery) (bool, error) {
	if interfaceIsNil(c.EntityReferenceResolver) {
		return false, capabilityUnavailable(CapabilityEntityReference)
	}
	query.Bundles = cloneStrings(query.Bundles)
	return callCapability(ctx, CapabilityEntityReference, func() (bool, error) {
		return c.EntityReferenceResolver.EntityReferenceExists(ctx, query)
	})
}

// AllowedValue delegates to the configured allowed-value capability. The row
// projection is deep-copied before delegation.
func (c Composite) AllowedValue(ctx context.Context, query AllowedValueQuery) (bool, error) {
	if interfaceIsNil(c.AllowedValueResolver) {
		return false, capabilityUnavailable(CapabilityAllowedValue)
	}
	query.Fields = cloneCanonicalFields(query.Fields)
	return callCapability(ctx, CapabilityAllowedValue, func() (bool, error) {
		return c.AllowedValueResolver.AllowedValue(ctx, query)
	})
}

// FileReadable delegates to the configured file-readability capability.
func (c Composite) FileReadable(ctx context.Context, normalizedPath string) (bool, error) {
	if interfaceIsNil(c.FileReadabilityResolver) {
		return false, capabilityUnavailable(CapabilityFileReadability)
	}
	return callCapability(ctx, CapabilityFileReadability, func() (bool, error) {
		return c.FileReadabilityResolver.FileReadable(ctx, normalizedPath)
	})
}

// TGNResolves delegates to the configured TGN capability.
func (c Composite) TGNResolves(ctx context.Context, termID string) (bool, error) {
	if interfaceIsNil(c.TGNResolver) {
		return false, capabilityUnavailable(CapabilityTGN)
	}
	return callCapability(ctx, CapabilityTGN, func() (bool, error) {
		return c.TGNResolver.TGNResolves(ctx, termID)
	})
}

// URLAliasAvailable delegates to the configured fixed-origin alias capability.
func (c Composite) URLAliasAvailable(ctx context.Context, alias string) (bool, error) {
	if interfaceIsNil(c.URLAliasAvailabilityResolver) {
		return false, capabilityUnavailable(CapabilityURLAliasAvailability)
	}
	return callCapability(ctx, CapabilityURLAliasAvailability, func() (bool, error) {
		return c.URLAliasAvailabilityResolver.URLAliasAvailable(ctx, alias)
	})
}

func callCapability(ctx context.Context, capability Capability, call func() (bool, error)) (ok bool, err error) {
	if interfaceIsNil(ctx) {
		return false, errors.New("validation context is nil")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("validation capability %q canceled: %w", capability, err)
	}
	defer func() {
		if recover() != nil {
			ok = false
			err = fmt.Errorf("validation capability %q panicked", capability)
		}
	}()
	ok, err = call()
	if err != nil {
		return false, fmt.Errorf("validation capability %q failed: %w", capability, err)
	}
	return ok, nil
}

func capabilityUnavailable(capability Capability) error {
	return &ErrCapabilityUnavailable{Capability: capability}
}

func interfaceIsNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneCanonicalFields(fields []CanonicalFieldValue) []CanonicalFieldValue {
	if fields == nil {
		return nil
	}
	cloned := make([]CanonicalFieldValue, len(fields))
	for i, field := range fields {
		cloned[i] = field
		cloned[i].Values = cloneStrings(field.Values)
	}
	return cloned
}
