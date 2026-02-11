package value

// Ref represents a reference to another entity.
type Ref struct {
	ID       string // The identifier (may be numeric string)
	Type     string // Entity type (e.g., "taxonomy_term", "node", "user")
	UUID     string // Optional UUID
	Resolved string // Resolved human-readable value (if available)
}

// IsZero returns true if the reference has no ID.
func (r Ref) IsZero() bool {
	return r.ID == ""
}

// String returns the resolved value if available, otherwise the ID.
func (r Ref) String() string {
	if r.Resolved != "" {
		return r.Resolved
	}
	return r.ID
}

// TypedRef is a reference with a relationship type.
type TypedRef struct {
	Ref
	RelType string // Relationship type (e.g., "relators:aut", "author")
}

// Resolver resolves reference IDs to human-readable values.
type Resolver interface {
	// Resolve returns the resolved value for an entity ID.
	// The entityType parameter hints at what kind of entity (taxonomy_term, node, etc.)
	Resolve(id, entityType string) (string, bool)
}

// RefOption configures reference extraction.
type RefOption func(*refConfig)

type refConfig struct {
	defaultType string
	resolver    Resolver
}

// WithRefType sets the default entity type for references.
func WithRefType(t string) RefOption {
	return func(c *refConfig) {
		c.defaultType = t
	}
}

// WithResolver sets a resolver for looking up reference values.
func WithResolver(r Resolver) RefOption {
	return func(c *refConfig) {
		c.resolver = r
	}
}

// RefFromMap extracts a Ref from a map (like JSON object).
// Looks for common keys: target_id, id, tid, nid, target_type, target_uuid
func RefFromMap(m map[string]any, opts ...RefOption) Ref {
	cfg := &refConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	ref := Ref{}

	// Extract ID - try multiple common keys
	if v, ok := m["target_id"]; ok {
		ref.ID = Text(v)
	} else if v, ok := m["id"]; ok {
		ref.ID = Text(v)
	} else if v, ok := m["tid"]; ok {
		ref.ID = Text(v)
	} else if v, ok := m["nid"]; ok {
		ref.ID = Text(v)
	}

	// Extract type
	if v, ok := m["target_type"]; ok {
		ref.Type = Text(v)
	} else if cfg.defaultType != "" {
		ref.Type = cfg.defaultType
	}

	// Extract UUID
	if v, ok := m["target_uuid"]; ok {
		ref.UUID = Text(v)
	} else if v, ok := m["uuid"]; ok {
		ref.UUID = Text(v)
	}

	// Try to resolve
	if cfg.resolver != nil && ref.ID != "" {
		if resolved, ok := cfg.resolver.Resolve(ref.ID, ref.Type); ok {
			ref.Resolved = resolved
		}
	}

	return ref
}

// RefSlice extracts references from various formats.
// Handles: []map[string]any, []any with maps, single map
func RefSlice(v any, opts ...RefOption) []Ref {
	if v == nil {
		return nil
	}

	var result []Ref

	switch val := v.(type) {
	case []map[string]any:
		result = make([]Ref, 0, len(val))
		for _, m := range val {
			if ref := RefFromMap(m, opts...); !ref.IsZero() {
				result = append(result, ref)
			}
		}
	case []any:
		result = make([]Ref, 0, len(val))
		for _, item := range val {
			if m, ok := item.(map[string]any); ok {
				if ref := RefFromMap(m, opts...); !ref.IsZero() {
					result = append(result, ref)
				}
			}
		}
	case map[string]any:
		if ref := RefFromMap(val, opts...); !ref.IsZero() {
			result = []Ref{ref}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// TypedRefOption configures typed reference extraction.
type TypedRefOption func(*typedRefConfig)

type typedRefConfig struct {
	refConfig
	relTypeKey string
}

// WithRelTypeKey sets the key to look for relationship type.
func WithRelTypeKey(key string) TypedRefOption {
	return func(c *typedRefConfig) {
		c.relTypeKey = key
	}
}

// WithTypedRefType sets the default entity type.
func WithTypedRefType(t string) TypedRefOption {
	return func(c *typedRefConfig) {
		c.defaultType = t
	}
}

// WithTypedResolver sets a resolver for typed references.
func WithTypedResolver(r Resolver) TypedRefOption {
	return func(c *typedRefConfig) {
		c.resolver = r
	}
}

// TypedRefFromMap extracts a TypedRef from a map.
func TypedRefFromMap(m map[string]any, opts ...TypedRefOption) TypedRef {
	cfg := &typedRefConfig{
		relTypeKey: "rel_type", // Default Drupal key
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Build ref options from typed config
	var refOpts []RefOption
	if cfg.defaultType != "" {
		refOpts = append(refOpts, WithRefType(cfg.defaultType))
	}
	if cfg.resolver != nil {
		refOpts = append(refOpts, WithResolver(cfg.resolver))
	}

	ref := TypedRef{
		Ref: RefFromMap(m, refOpts...),
	}

	// Extract relationship type
	if v, ok := m[cfg.relTypeKey]; ok {
		ref.RelType = Text(v)
	}

	return ref
}

// TypedRefSlice extracts typed references from various formats.
func TypedRefSlice(v any, opts ...TypedRefOption) []TypedRef {
	if v == nil {
		return nil
	}

	var result []TypedRef

	switch val := v.(type) {
	case []map[string]any:
		result = make([]TypedRef, 0, len(val))
		for _, m := range val {
			if ref := TypedRefFromMap(m, opts...); !ref.IsZero() {
				result = append(result, ref)
			}
		}
	case []any:
		result = make([]TypedRef, 0, len(val))
		for _, item := range val {
			if m, ok := item.(map[string]any); ok {
				if ref := TypedRefFromMap(m, opts...); !ref.IsZero() {
					result = append(result, ref)
				}
			}
		}
	case map[string]any:
		if ref := TypedRefFromMap(val, opts...); !ref.IsZero() {
			result = []TypedRef{ref}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
