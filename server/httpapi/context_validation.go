package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lehigh-university-libraries/crosswalk/spec"
	"github.com/lehigh-university-libraries/crosswalk/validationcontext"
)

const (
	maxValidationContextValueBytes = 4096
	maxValidationContextRowBytes   = 1 << 20
)

// ValidationContextResolver is the deployment-aware resolver contract used by
// Check My Work. It aliases the transport-neutral validation context package so
// HTTP callers and resolver implementations share one contract.
type ValidationContextResolver = validationcontext.Resolver

// AllowedValueQuery is the transport-neutral dynamic allowed-value query.
type AllowedValueQuery = validationcontext.AllowedValueQuery

// CanonicalFieldValue is one canonical field in an allowed-value row context.
type CanonicalFieldValue = validationcontext.CanonicalFieldValue

// EntityReferenceKind identifies an already-parsed entity-reference value.
type EntityReferenceKind = validationcontext.EntityReferenceKind

const (
	EntityReferenceID   = validationcontext.EntityReferenceID
	EntityReferenceName = validationcontext.EntityReferenceName
	EntityReferenceURI  = validationcontext.EntityReferenceURI
)

// EntityReferenceQuery is the transport-neutral typed reference query.
type EntityReferenceQuery = validationcontext.EntityReferenceQuery

// ErrValidationContextRequired reports that a caller requested contextual
// parity without installing a trusted deployment-aware resolver.
var ErrValidationContextRequired = errors.New("validation context is not configured")

// ConfigureValidationContext installs the trusted, read-only resolver used by
// mapping-declared context validation rules. Configure the engine before it is
// shared by concurrent callers. A nil (including typed-nil) resolver is
// rejected so a requested full validation cannot silently become
// deterministic-only validation.
func (e *CrosswalkEngine) ConfigureValidationContext(resolver ValidationContextResolver) error {
	if e == nil {
		return errors.New("crosswalk engine is nil")
	}
	if validationContextResolverIsNil(resolver) {
		return ErrValidationContextRequired
	}
	e.contextResolver = resolver
	return nil
}

// HasValidationContext reports whether Check My Work will execute
// mapping-declared deployment-aware validation rules. Constructors deliberately
// return deterministic-only engines until a trusted resolver is installed.
func (e *CrosswalkEngine) HasValidationContext() bool {
	return e != nil && !validationContextResolverIsNil(e.contextResolver)
}

func validationContextResolverIsNil(resolver ValidationContextResolver) bool {
	if resolver == nil {
		return true
	}
	value := reflect.ValueOf(resolver)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type validationContextColumn struct {
	index int
	field spec.Field
}

type validationContextRow struct {
	row       []string
	columns   map[string]validationContextColumn
	separator string
}

type validationContextIdentity struct {
	operation     spec.Operation
	nodeID        uint64
	nodeIDPresent bool
	nodeIDValid   bool
}

func newValidationContextRow(row []string, columns map[string]validationContextColumn, separator string) validationContextRow {
	return validationContextRow{row: row, columns: columns, separator: separator}
}

func (v validationContextRow) raw(name string) string {
	column, ok := v.columns[strings.ToLower(name)]
	if !ok || column.index < 0 || column.index >= len(v.row) {
		return ""
	}
	value := strings.TrimSpace(v.row[column.index])
	if value == "" {
		value = column.field.Default
	}
	return strings.TrimSpace(value)
}

func (v validationContextRow) values(name string) []string {
	column, ok := v.columns[strings.ToLower(name)]
	if !ok {
		return nil
	}
	return validationContextValues(v.raw(name), column.field, v.separator)
}

func (v validationContextRow) identity() validationContextIdentity {
	hasNodeID := false
	hasUpdateMetadata := false
	hasPrimaryFile := false
	identity := validationContextIdentity{nodeIDValid: true}
	for _, column := range orderedValidationContextColumns(v.columns) {
		raw := v.raw(column.field.Name)
		if column.field.Codec == "ignore" || raw == "" {
			continue
		}
		switch column.field.Hub {
		case "Extra.node_id":
			hasNodeID = true
			identity.nodeIDPresent = true
			var err error
			identity.nodeID, err = strconv.ParseUint(raw, 10, 64)
			if err != nil {
				identity.nodeIDValid = false
			}
		case "Files.primary":
			hasPrimaryFile = true
		case "Extra.id", "Extra.parent_id":
			// Upload IDs and within-batch hierarchy are transport metadata,
			// not Drupal node metadata updates.
		default:
			if column.field.AppliesTo(spec.OperationUpdate) {
				hasUpdateMetadata = true
			}
		}
	}
	if !hasNodeID {
		identity.operation = spec.OperationCreate
		return identity
	}
	if hasUpdateMetadata {
		identity.operation = spec.OperationUpdate
		return identity
	}
	if hasPrimaryFile {
		identity.operation = spec.OperationAddMedia
		return identity
	}
	identity.operation = spec.OperationUpdate
	return identity
}

func (v validationContextRow) canonicalFields() ([]CanonicalFieldValue, error) {
	fields := make([]CanonicalFieldValue, 0, len(v.columns))
	totalBytes := 0
	for _, column := range orderedValidationContextColumns(v.columns) {
		if column.field.Codec == "ignore" {
			continue
		}
		entry := CanonicalFieldValue{Field: column.field.Name, SourceType: column.field.SourceType}
		for _, value := range validationContextValues(v.raw(column.field.Name), column.field, v.separator) {
			normalized, err := boundedValidationContextValue(value)
			if err != nil {
				return nil, fmt.Errorf("canonical field %s: %w", column.field.Name, err)
			}
			entry.Values = append(entry.Values, normalized)
			totalBytes += len(normalized)
		}
		totalBytes += len(entry.Field) + len(entry.SourceType)
		if totalBytes > maxValidationContextRowBytes {
			return nil, fmt.Errorf("canonical row projection exceeds %d bytes", maxValidationContextRowBytes)
		}
		fields = append(fields, entry)
	}
	return fields, nil
}

func (v validationContextRow) predicateMatches(when *spec.ValidationWhen) bool {
	if when == nil {
		return true
	}
	switch when.Operator {
	case spec.ValidationOperatorIn:
		value := v.raw(when.Field)
		for _, candidate := range when.Values {
			if value == candidate {
				return true
			}
		}
		return false
	case spec.ValidationOperatorValueCountGreaterThan:
		return len(v.values(when.Field)) > when.Count
	default:
		// Sealed specifications reject unknown operators. Fail the predicate
		// closed if a future rule reaches an older evaluator.
		return false
	}
}

func validationContextValues(raw string, field spec.Field, separator string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	shouldSplit := false
	if field.Cardinality != 1 {
		switch field.Codec {
		case "", "string", "multi", "file", "contributors", "boolean", "integer", "unsigned", "edtf", "profile_identifier":
			shouldSplit = true
		}
	}
	if !shouldSplit {
		switch field.Codec {
		case "multi", "file", "contributors":
			shouldSplit = true
		}
	}
	if !shouldSplit {
		return []string{raw}
	}
	if separator == "" {
		separator = "|"
	}
	parts := strings.Split(raw, separator)
	values := make([]string, 0, len(parts))
	for _, value := range parts {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func validationContextLayout(rows [][]string, transformation *spec.Transformation) (int, map[string]validationContextColumn) {
	headerRows, _ := sourceCoordinates(rows, transformation)
	columns := make(map[string]validationContextColumn)
	for rowIndex := 0; rowIndex < headerRows && rowIndex < len(rows); rowIndex++ {
		for columnIndex, header := range rows[rowIndex] {
			field, ok := transformation.SourceField(header)
			if !ok || field.Codec == "ignore" {
				continue
			}
			key := strings.ToLower(field.Name)
			if _, exists := columns[key]; exists {
				continue
			}
			columns[key] = validationContextColumn{index: columnIndex, field: field}
		}
	}
	return headerRows, columns
}

type validationContextBoolResult struct {
	value bool
	err   error
}

// validationContextCache is deliberately request-scoped. It prevents a sheet
// containing thousands of identical references from turning into thousands of
// identical repository/authority calls while findings are still replayed at
// every affected cell.
type validationContextCache struct {
	nodes    map[uint64]validationContextBoolResult
	entities map[string]validationContextBoolResult
	allowed  map[string]validationContextBoolResult
	files    map[string]validationContextBoolResult
	tgn      map[string]validationContextBoolResult
	aliases  map[string]validationContextBoolResult
}

func newValidationContextCache() *validationContextCache {
	return &validationContextCache{
		nodes:    make(map[uint64]validationContextBoolResult),
		entities: make(map[string]validationContextBoolResult),
		allowed:  make(map[string]validationContextBoolResult),
		files:    make(map[string]validationContextBoolResult),
		tgn:      make(map[string]validationContextBoolResult),
		aliases:  make(map[string]validationContextBoolResult),
	}
}

func (c *validationContextCache) nodeExists(ctx context.Context, resolver ValidationContextResolver, nodeID uint64) (bool, error) {
	if cached, ok := c.nodes[nodeID]; ok {
		return cached.value, cached.err
	}
	if err := validationcontext.ConsumeLookup(ctx); err != nil {
		return false, err
	}
	value, err := resolver.NodeExists(ctx, nodeID)
	c.nodes[nodeID] = validationContextBoolResult{value: value, err: err}
	return value, err
}

func (c *validationContextCache) entityExists(ctx context.Context, resolver ValidationContextResolver, query EntityReferenceQuery) (bool, error) {
	key := validationContextQueryKey(query)
	if cached, ok := c.entities[key]; ok {
		return cached.value, cached.err
	}
	if err := validationcontext.ConsumeLookup(ctx); err != nil {
		return false, err
	}
	query.Bundles = append([]string(nil), query.Bundles...)
	value, err := resolver.EntityReferenceExists(ctx, query)
	c.entities[key] = validationContextBoolResult{value: value, err: err}
	return value, err
}

func (c *validationContextCache) valueAllowed(ctx context.Context, resolver ValidationContextResolver, query AllowedValueQuery) (bool, error) {
	key := validationContextQueryKey(query)
	if cached, ok := c.allowed[key]; ok {
		return cached.value, cached.err
	}
	if err := validationcontext.ConsumeLookup(ctx); err != nil {
		return false, err
	}
	query.Fields = cloneCanonicalFieldValues(query.Fields)
	value, err := resolver.AllowedValue(ctx, query)
	c.allowed[key] = validationContextBoolResult{value: value, err: err}
	return value, err
}

func (c *validationContextCache) fileReadable(ctx context.Context, resolver ValidationContextResolver, normalizedPath string) (bool, error) {
	if cached, ok := c.files[normalizedPath]; ok {
		return cached.value, cached.err
	}
	if err := validationcontext.ConsumeLookup(ctx); err != nil {
		return false, err
	}
	value, err := resolver.FileReadable(ctx, normalizedPath)
	c.files[normalizedPath] = validationContextBoolResult{value: value, err: err}
	return value, err
}

func (c *validationContextCache) tgnResolves(ctx context.Context, resolver ValidationContextResolver, termID string) (bool, error) {
	if cached, ok := c.tgn[termID]; ok {
		return cached.value, cached.err
	}
	if err := validationcontext.ConsumeLookup(ctx); err != nil {
		return false, err
	}
	value, err := resolver.TGNResolves(ctx, termID)
	c.tgn[termID] = validationContextBoolResult{value: value, err: err}
	return value, err
}

func (c *validationContextCache) urlAliasAvailable(ctx context.Context, resolver ValidationContextResolver, alias string) (bool, error) {
	if result, exists := c.aliases[alias]; exists {
		return result.value, result.err
	}
	if err := validationcontext.ConsumeLookup(ctx); err != nil {
		return false, err
	}
	value, err := resolver.URLAliasAvailable(ctx, alias)
	c.aliases[alias] = validationContextBoolResult{value: value, err: err}
	return value, err
}

func validationContextQueryKey(query any) string {
	encoded, err := json.Marshal(query)
	if err != nil {
		// The query contracts contain only strings, booleans, numbers, and
		// slices, so encoding cannot fail. Returning a unique-looking fallback
		// keeps a future incompatible type fail-safe rather than panicking.
		return fmt.Sprintf("%T:%#v", query, query)
	}
	return string(encoded)
}

func cloneCanonicalFieldValues(fields []CanonicalFieldValue) []CanonicalFieldValue {
	result := make([]CanonicalFieldValue, len(fields))
	for index, field := range fields {
		result[index] = field
		result[index].Values = append([]string(nil), field.Values...)
	}
	return result
}

func (e *CrosswalkEngine) validateContext(ctx context.Context, rows [][]string) (CheckResult, error) {
	resolver := e.contextResolver
	if validationContextResolverIsNil(resolver) {
		return CheckResult{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = validationcontext.EnsureRequestBudget(ctx)
	headerRows, columns := validationContextLayout(rows, e.transformation)
	result := make(CheckResult)
	cache := newValidationContextCache()
	recordIndex := 0
	for rowIndex := headerRows; rowIndex < len(rows); rowIndex++ {
		if validationContextEmptyRow(rows[rowIndex]) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		view := newValidationContextRow(rows[rowIndex], columns, e.transformation.Source.MultiValueSeparator)
		identity := view.identity()
		canonicalFields, projectionErr := view.canonicalFields()
		for _, column := range orderedValidationContextColumns(columns) {
			raw := view.raw(column.field.Name)
			if raw == "" || !column.field.AppliesTo(identity.operation) {
				continue
			}
			for _, validation := range column.field.Validations {
				if !validation.IsContext() || !validation.AppliesTo(identity.operation) || !view.predicateMatches(validation.When) {
					continue
				}
				for _, value := range validationContextValues(raw, column.field, view.separator) {
					message, err := e.executeContextValidation(ctx, resolver, cache, validation, column.field, value, identity, canonicalFields, projectionErr)
					if err != nil {
						if contextErr := ctx.Err(); contextErr != nil {
							return nil, contextErr
						}
						return nil, fmt.Errorf("context validation %q for source field %q on row %d: %w", validation.Rule, column.field.Name, rowIndex+1, err)
					}
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					if message != "" {
						key := excelColumn(column.index+1) + strconv.Itoa(rowIndex+1)
						if key == "" {
							key = fmt.Sprintf("record[%d].%s", recordIndex+1, column.field.Name)
						}
						appendCheckMessage(result, key, message)
					}
				}
			}
		}
		recordIndex++
	}
	return result, nil
}

func orderedValidationContextColumns(columns map[string]validationContextColumn) []validationContextColumn {
	result := make([]validationContextColumn, 0, len(columns))
	for _, column := range columns {
		result = append(result, column)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].index != result[right].index {
			return result[left].index < result[right].index
		}
		return result[left].field.Name < result[right].field.Name
	})
	return result
}

func (e *CrosswalkEngine) executeContextValidation(
	ctx context.Context,
	resolver ValidationContextResolver,
	cache *validationContextCache,
	validation spec.Validation,
	field spec.Field,
	value string,
	identity validationContextIdentity,
	canonicalFields []CanonicalFieldValue,
	projectionErr error,
) (string, error) {
	value, err := boundedValidationContextValue(value)
	if err != nil {
		return "Cannot perform context validation: " + err.Error(), nil
	}
	switch validation.Rule {
	case spec.ValidationContextNodeExists:
		nodeID, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Sprintf("Invalid Drupal node ID %q", value), nil
		}
		exists, err := cache.nodeExists(ctx, resolver, nodeID)
		if err != nil {
			return "", err
		}
		if !exists {
			return fmt.Sprintf("Drupal node %d does not exist or is not accessible", nodeID), nil
		}
	case spec.ValidationContextEntityExists:
		query, ok := validationContextEntityReference(validation, field, value)
		if !ok {
			return fmt.Sprintf("Invalid %s entity reference %q", validation.EntityType, value), nil
		}
		exists, err := cache.entityExists(ctx, resolver, query)
		if err != nil {
			return "", err
		}
		if !exists {
			if validation.AllowNewNames && query.Kind == EntityReferenceName {
				return "", nil
			}
			return fmt.Sprintf("Referenced %s entity %q does not exist in the configured bundles", validation.EntityType, value), nil
		}
	case spec.ValidationContextAllowedValue:
		if identity.nodeIDPresent && !identity.nodeIDValid {
			return "Cannot evaluate configured allowed values until this row has a valid Node ID", nil
		}
		if projectionErr != nil {
			return "Cannot evaluate configured allowed values: " + projectionErr.Error(), nil
		}
		allowed, err := cache.valueAllowed(ctx, resolver, AllowedValueQuery{
			Field: field.Name, SourceType: field.SourceType, Provider: validation.Provider, Value: value,
			Operation: validationcontext.Operation(identity.operation), NodeID: identity.nodeID, NodeIDPresent: identity.nodeIDPresent,
			Bundle: e.transformation.Fingerprint.Bundle, Fields: cloneCanonicalFieldValues(canonicalFields),
			ProfileFingerprint: e.transformation.Fingerprint.Profile, ModelFingerprint: e.transformation.Fingerprint.Model,
		})
		if err != nil {
			return "", err
		}
		if !allowed {
			return fmt.Sprintf("Value %q is not allowed by configured provider %s", value, validation.Provider), nil
		}
	case spec.ValidationContextFileReadable:
		normalized, err := e.transformation.NormalizeFilePath(value)
		if err != nil {
			return "Invalid Workbench file path: " + err.Error(), nil
		}
		readable, err := cache.fileReadable(ctx, resolver, normalized)
		if err != nil {
			return "", err
		}
		if !readable {
			return fmt.Sprintf(
				"File is missing or unreadable in the Workbench staging context: %s. Verify that the file exists and that the Crosswalk service account has read permission on the file and read/traverse permission on every parent directory.",
				normalized,
			), nil
		}
	case spec.ValidationContextTGNResolves:
		termID, ok := validationContextGettyTGNID(value)
		if !ok {
			return fmt.Sprintf("Invalid Getty TGN URI %q", value), nil
		}
		resolves, err := cache.tgnResolves(ctx, resolver, termID)
		if err != nil {
			return "", err
		}
		if !resolves {
			return fmt.Sprintf("Getty TGN term %s does not resolve", termID), nil
		}
	case spec.ValidationContextURLAliasAvailable:
		available, err := cache.urlAliasAvailable(ctx, resolver, value)
		if err != nil {
			return "", err
		}
		if !available {
			return fmt.Sprintf("Drupal URL alias %q already exists", value), nil
		}
	default:
		// Never silently omit a newly introduced contextual policy rule.
		return "", fmt.Errorf("unsupported context validation rule %q", validation.Rule)
	}
	return "", nil
}

func boundedValidationContextValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		return "", errors.New("context value is not valid UTF-8")
	}
	if len(value) > maxValidationContextValueBytes {
		return "", fmt.Errorf("context value exceeds %d bytes", maxValidationContextValueBytes)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", errors.New("context value contains a control character")
		}
	}
	return value, nil
}

func validationContextEntityReference(validation spec.Validation, field spec.Field, value string) (EntityReferenceQuery, bool) {
	value = strings.TrimSpace(value)
	if field.SourceType == "typed_relation" {
		parts := strings.SplitN(value, ":", 3)
		if len(parts) != 3 {
			return EntityReferenceQuery{}, false
		}
		value = strings.TrimSpace(parts[2])
	}
	query := EntityReferenceQuery{
		EntityType: validation.EntityType,
		Bundles:    append([]string(nil), validation.Bundles...),
		Field:      field.Name,
		SourceType: field.SourceType,
		Handler:    validation.Handler,
		Value:      value,
	}
	if _, err := strconv.ParseUint(value, 10, 64); err == nil {
		query.Kind = EntityReferenceID
		return query, true
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		if validation.EntityType != "taxonomy_term" {
			return EntityReferenceQuery{}, false
		}
		parsed, err := url.ParseRequestURI(value)
		if err != nil || parsed.User != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return EntityReferenceQuery{}, false
		}
		query.Kind = EntityReferenceURI
		query.Value = parsed.String()
		return query, true
	}
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return EntityReferenceQuery{}, false
	}
	if validation.EntityType != "taxonomy_term" {
		return EntityReferenceQuery{}, false
	}
	if namespace, term, namespaced := strings.Cut(value, ":"); namespaced {
		for _, bundle := range query.Bundles {
			if namespace == bundle {
				if term == "" {
					return EntityReferenceQuery{}, false
				}
				query.Bundles = []string{bundle}
				query.Value = term
				query.Kind = EntityReferenceName
				return query, true
			}
		}
		if len(query.Bundles) > 1 {
			return EntityReferenceQuery{}, false
		}
	}
	query.Kind = EntityReferenceName
	return query, true
}

func validationContextGettyTGNID(value string) (string, bool) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	if !strings.EqualFold(parsed.Hostname(), "vocab.getty.edu") || parsed.Port() != "" || parsed.RawPath != "" {
		return "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 2 && parts[0] == "tgn" {
		parts = parts[1:]
	} else if len(parts) == 3 && parts[0] == "page" && parts[1] == "tgn" {
		parts = parts[2:]
	} else {
		return "", false
	}
	termID := parts[0]
	if termID == "" {
		return "", false
	}
	for _, character := range termID {
		if character < '0' || character > '9' {
			return "", false
		}
	}
	return termID, true
}

func validationContextEmptyRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
