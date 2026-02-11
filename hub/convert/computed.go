package convert

import (
	"sync"

	"google.golang.org/protobuf/proto"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// ComputedFieldFunc computes a derived value from the full source message.
// It receives the source proto message and the partially-populated Hub record,
// allowing it to access multiple source fields and add computed values to the record.
//
// Use cases:
//   - Fields derived from multiple source fields (e.g., embargo date from code + accept date)
//   - Fields requiring complex business logic beyond simple parsing
//   - Fields that need access to sibling field values
//
// The function should modify the record in place and return any error encountered.
type ComputedFieldFunc func(source proto.Message, record *hubv1.Record) error

// ComputedFieldRegistry manages computed field functions keyed by message type.
// Computed fields are evaluated after all individual field mappings are processed.
type ComputedFieldRegistry struct {
	mu    sync.RWMutex
	funcs map[string][]ComputedFieldFunc // keyed by message full name
}

// NewComputedFieldRegistry creates a new computed field registry.
func NewComputedFieldRegistry() *ComputedFieldRegistry {
	return &ComputedFieldRegistry{
		funcs: make(map[string][]ComputedFieldFunc),
	}
}

// Register adds a computed field function for a specific message type.
// The messageFullName should be the full proto message name (e.g., "spoke.proquest.v1.Submission").
func (r *ComputedFieldRegistry) Register(messageFullName string, fn ComputedFieldFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.funcs[messageFullName] = append(r.funcs[messageFullName], fn)
}

// Get retrieves all computed field functions for a message type.
func (r *ComputedFieldRegistry) Get(messageFullName string) []ComputedFieldFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.funcs[messageFullName]
}

// Apply executes all computed field functions for the given source message.
// Returns the first error encountered, or nil if all functions succeed.
func (r *ComputedFieldRegistry) Apply(source proto.Message, record *hubv1.Record) error {
	fullName := string(source.ProtoReflect().Descriptor().FullName())

	funcs := r.Get(fullName)
	for _, fn := range funcs {
		if err := fn(source, record); err != nil {
			return err
		}
	}

	return nil
}

// HasComputedFields returns true if there are computed fields registered for the message type.
func (r *ComputedFieldRegistry) HasComputedFields(messageFullName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	funcs, ok := r.funcs[messageFullName]
	return ok && len(funcs) > 0
}

// RegisteredTypes returns all message types that have computed fields registered.
func (r *ComputedFieldRegistry) RegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.funcs))
	for t := range r.funcs {
		types = append(types, t)
	}
	return types
}

// Default computed field registry instance.
var defaultComputedFields = NewComputedFieldRegistry()

// DefaultComputedFields returns the default computed field registry.
func DefaultComputedFields() *ComputedFieldRegistry {
	return defaultComputedFields
}

// RegisterComputedField registers a computed field function with the default registry.
func RegisterComputedField(messageFullName string, fn ComputedFieldFunc) {
	defaultComputedFields.Register(messageFullName, fn)
}
