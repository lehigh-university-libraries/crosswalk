package convert

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// Converter handles conversion between spoke messages and Hub records.
type Converter struct {
	parsers        *ParserRegistry
	validators     *ValidatorRegistry
	serializers    *SerializerRegistry
	computedFields *ComputedFieldRegistry
}

// NewConverter creates a new converter with default registries.
func NewConverter() *Converter {
	return &Converter{
		parsers:        DefaultParsers(),
		validators:     DefaultValidators(),
		serializers:    DefaultSerializers(),
		computedFields: DefaultComputedFields(),
	}
}

// NewConverterWithRegistries creates a converter with custom registries.
func NewConverterWithRegistries(parsers *ParserRegistry, validators *ValidatorRegistry, serializers *SerializerRegistry) *Converter {
	return &Converter{
		parsers:        parsers,
		validators:     validators,
		serializers:    serializers,
		computedFields: DefaultComputedFields(),
	}
}

// NewConverterWithAllRegistries creates a converter with all custom registries including computed fields.
func NewConverterWithAllRegistries(parsers *ParserRegistry, validators *ValidatorRegistry, serializers *SerializerRegistry, computed *ComputedFieldRegistry) *Converter {
	return &Converter{
		parsers:        parsers,
		validators:     validators,
		serializers:    serializers,
		computedFields: computed,
	}
}

// Parsers returns the parser registry.
func (c *Converter) Parsers() *ParserRegistry {
	return c.parsers
}

// Validators returns the validator registry.
func (c *Converter) Validators() *ValidatorRegistry {
	return c.validators
}

// Serializers returns the serializer registry.
func (c *Converter) Serializers() *SerializerRegistry {
	return c.serializers
}

// ComputedFields returns the computed field registry.
func (c *Converter) ComputedFields() *ComputedFieldRegistry {
	return c.computedFields
}

// ConversionError represents an error during conversion.
type ConversionError struct {
	Field   string
	Message string
	Cause   error
}

func (e *ConversionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("conversion error for field %q: %s: %v", e.Field, e.Message, e.Cause)
	}
	return fmt.Sprintf("conversion error for field %q: %s", e.Field, e.Message)
}

func (e *ConversionError) Unwrap() error {
	return e.Cause
}

// ConversionResult contains the result of a conversion operation.
type ConversionResult struct {
	// Record is the converted Hub record
	Record *hubv1.Record

	// Errors contains any non-fatal errors encountered
	Errors []error

	// Warnings contains validation warnings
	Warnings []string
}

// ToHub converts a spoke proto message to a Hub Record using annotations.
func (c *Converter) ToHub(msg proto.Message) (*ConversionResult, error) {
	result := &ConversionResult{
		Record: newEmptyRecord(),
		Errors: make([]error, 0),
	}

	// Get message options
	msgOpts := GetMessageOptions(msg)
	if msgOpts != nil && msgOpts.Target != "Record" && msgOpts.Target != "" {
		return nil, fmt.Errorf("message target %q is not supported; expected 'Record'", msgOpts.Target)
	}

	// Process all mapped fields
	mappings := GetAllFieldMappings(msg)
	msgRef := msg.ProtoReflect()

	for _, mapping := range mappings {
		if mapping.Options == nil || mapping.Options.Target == "" {
			// Handle unmapped fields if preserve_unmapped is true
			if msgOpts != nil && msgOpts.PreserveUnmapped {
				c.handleUnmappedField(result, msgRef, mapping)
			}
			continue
		}

		if err := c.processField(result, msgRef, mapping); err != nil {
			result.Errors = append(result.Errors, err)
		}
	}

	// Apply computed fields after all individual field mappings are processed.
	// Computed fields can access the full source message to derive values from
	// multiple fields (e.g., embargo date computed from embargo_code + accept_date).
	if c.computedFields != nil {
		if err := c.computedFields.Apply(msg, result.Record); err != nil {
			result.Errors = append(result.Errors, &ConversionError{
				Field:   "_computed",
				Message: "computed field error",
				Cause:   err,
			})
		}
	}

	return result, nil
}

// processField processes a single field mapping.
func (c *Converter) processField(result *ConversionResult, msgRef protoreflect.Message, mapping FieldMapping) error {
	fd := mapping.FieldDescriptor
	opts := mapping.Options

	// Get field value
	if !msgRef.Has(fd) {
		// Field not set, skip (unless required)
		if opts.Required {
			return &ConversionError{
				Field:   mapping.Name,
				Message: "required field is empty",
			}
		}
		return nil
	}

	value := msgRef.Get(fd)

	// Convert protoreflect.Value to Go value
	goValue := c.protoValueToGo(value, fd)

	// Apply parser if specified
	if opts.Parser != "" {
		parserOpts := &ParserOptions{
			DateFormat: opts.DateFormat,
			Delimiter:  opts.Delimiter,
		}
		parsed, err := c.parsers.Parse(opts.Parser, fmt.Sprintf("%v", goValue), parserOpts)
		if err != nil {
			return &ConversionError{
				Field:   mapping.Name,
				Message: "parser failed",
				Cause:   err,
			}
		}
		goValue = parsed
	}

	// Apply validators if specified
	if opts.Validators != "" {
		validatorOpts := &ValidatorOptions{
			FieldName: mapping.Name,
			Pattern:   opts.Pattern,
			MinLength: opts.MinLength,
			MaxLength: opts.MaxLength,
			MinValue:  opts.MinValue,
			MaxValue:  opts.MaxValue,
			MinCount:  opts.MinCount,
			MaxCount:  opts.MaxCount,
		}
		errors := c.validators.ValidateAll(opts.Validators, goValue, validatorOpts)
		result.Errors = append(result.Errors, errors...)
	}

	// Map to Hub field
	return c.mapToHubField(result.Record, goValue, mapping)
}

// protoValueToGo converts a protoreflect.Value to a Go value.
func (c *Converter) protoValueToGo(value protoreflect.Value, fd protoreflect.FieldDescriptor) any {
	if fd.IsList() {
		list := value.List()
		result := make([]any, list.Len())
		for i := 0; i < list.Len(); i++ {
			result[i] = c.singleProtoValueToGo(list.Get(i), fd)
		}
		return result
	}

	if fd.IsMap() {
		m := value.Map()
		result := make(map[string]any)
		m.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
			result[k.String()] = c.singleProtoValueToGo(v, fd.MapValue())
			return true
		})
		return result
	}

	return c.singleProtoValueToGo(value, fd)
}

// singleProtoValueToGo converts a single proto value to a Go value.
func (c *Converter) singleProtoValueToGo(value protoreflect.Value, fd protoreflect.FieldDescriptor) any {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return value.Bool()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return int32(value.Int())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return value.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return uint32(value.Uint())
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return value.Uint()
	case protoreflect.FloatKind:
		return float32(value.Float())
	case protoreflect.DoubleKind:
		return value.Float()
	case protoreflect.StringKind:
		return value.String()
	case protoreflect.BytesKind:
		return value.Bytes()
	case protoreflect.EnumKind:
		return int32(value.Enum())
	case protoreflect.MessageKind:
		return value.Message().Interface()
	default:
		return value.Interface()
	}
}

// mapToHubField maps a value to the appropriate Hub Record field.
func (c *Converter) mapToHubField(record *hubv1.Record, value any, mapping FieldMapping) error {
	opts := mapping.Options
	target := opts.Target

	// Handle nested paths (e.g., "degree_info.institution")
	if strings.Contains(target, ".") {
		return c.mapToNestedField(record, value, mapping)
	}

	switch target {
	case "title":
		record.Title = toString(value)

	case "abstract":
		record.Abstract = toString(value)

	case "publisher":
		record.Publisher = toString(value)

	case "place_published":
		record.PlacePublished = toString(value)

	case "language":
		record.Language = toString(value)

	case "resource_type":
		// Handle enum mapping
		c.mapResourceType(record, value, mapping)

	case "contributors":
		return c.mapContributors(record, value, mapping)

	case "dates":
		return c.mapDates(record, value, mapping)

	case "identifiers":
		return c.mapIdentifiers(record, value, mapping)

	case "subjects":
		return c.mapSubjects(record, value, mapping)

	case "relations":
		return c.mapRelations(record, value, mapping)

	case "notes":
		c.mapNotes(record, value)

	case "extra":
		c.mapExtra(record, value, mapping.Name, opts.Description)

	default:
		// Unknown target, store in extra
		c.mapExtra(record, value, mapping.Name, opts.Description)
	}

	return nil
}

// mapToNestedField handles nested field paths.
func (c *Converter) mapToNestedField(record *hubv1.Record, value any, mapping FieldMapping) error {
	opts := mapping.Options
	parts := strings.SplitN(opts.Target, ".", 2)

	switch parts[0] {
	case "degree_info":
		if record.DegreeInfo == nil {
			record.DegreeInfo = &hubv1.DegreeInfo{}
		}
		if len(parts) > 1 {
			switch parts[1] {
			case "institution":
				record.DegreeInfo.Institution = toString(value)
			case "degree_name":
				record.DegreeInfo.DegreeName = toString(value)
			case "department":
				record.DegreeInfo.Department = toString(value)
			}
		}
	default:
		// Store in extra with dot notation
		c.mapExtra(record, value, opts.Target, opts.Description)
	}

	return nil
}

// mapResourceType maps a value to the resource type field.
func (c *Converter) mapResourceType(record *hubv1.Record, value any, mapping FieldMapping) {
	fd := mapping.FieldDescriptor

	// If it's an enum, try to get the enum value options
	if fd.Kind() == protoreflect.EnumKind {
		enumNum, ok := value.(int32)
		if !ok {
			return
		}

		ed := fd.Enum()
		enumMapping := GetEnumMappingByNumber(ed, enumNum)
		if enumMapping != nil && enumMapping.Options != nil && enumMapping.Options.Target != "" {
			// Parse the target enum value
			targetStr := enumMapping.Options.Target
			if val, ok := hubv1.ResourceTypeValue_value[targetStr]; ok {
				record.ResourceType = &hubv1.ResourceType{
					Type: hubv1.ResourceTypeValue(val),
				}
				return
			}
		}
	}

	// Fallback: try to match by string
	str := toString(value)
	if val, ok := hubv1.ResourceTypeValue_value[str]; ok {
		record.ResourceType = &hubv1.ResourceType{
			Type: hubv1.ResourceTypeValue(val),
		}
	}
}

// mapContributors maps values to the contributors field.
func (c *Converter) mapContributors(record *hubv1.Record, value any, mapping FieldMapping) error {
	opts := mapping.Options

	// Handle repeated message fields
	if list, ok := value.([]any); ok {
		for _, item := range list {
			contributor := c.createContributor(item, opts)
			if contributor != nil {
				record.Contributors = append(record.Contributors, contributor)
			}
		}
		return nil
	}

	// Handle single value
	contributor := c.createContributor(value, opts)
	if contributor != nil {
		record.Contributors = append(record.Contributors, contributor)
	}

	return nil
}

// createContributor creates a Hub Contributor from a value.
func (c *Converter) createContributor(value any, opts *hubv1.FieldOptions) *hubv1.Contributor {
	contributor := &hubv1.Contributor{
		Role: opts.Role,
	}

	// Set contributor type
	switch opts.ContributorType {
	case "person":
		contributor.Type = hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON
	case "organization":
		contributor.Type = hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
	}

	// Handle different value types
	switch v := value.(type) {
	case proto.Message:
		// Process the nested message
		c.populateContributorFromMessage(contributor, v)
	case BibTeXName:
		contributor.Name = fmt.Sprintf("%s, %s", v.Family, v.Given)
		contributor.ParsedName = &hubv1.ParsedName{
			Given:  v.Given,
			Family: v.Family,
			Suffix: v.Suffix,
		}
	case CSLName:
		if v.Literal != "" {
			contributor.Name = v.Literal
		} else {
			contributor.Name = fmt.Sprintf("%s, %s", v.Family, v.Given)
		}
		contributor.ParsedName = &hubv1.ParsedName{
			Given:  v.Given,
			Family: v.Family,
			Suffix: v.Suffix,
		}
	case string:
		contributor.Name = v
	case map[string]any:
		if name, ok := v["name"].(string); ok {
			contributor.Name = name
		}
		if given, ok := v["given"].(string); ok {
			if contributor.ParsedName == nil {
				contributor.ParsedName = &hubv1.ParsedName{}
			}
			contributor.ParsedName.Given = given
		}
		if family, ok := v["family"].(string); ok {
			if contributor.ParsedName == nil {
				contributor.ParsedName = &hubv1.ParsedName{}
			}
			contributor.ParsedName.Family = family
		}
	}

	if contributor.Name == "" && contributor.ParsedName == nil {
		return nil
	}

	return contributor
}

// populateContributorFromMessage extracts contributor info from a proto message.
func (c *Converter) populateContributorFromMessage(contributor *hubv1.Contributor, msg proto.Message) {
	msgRef := msg.ProtoReflect()
	md := msgRef.Descriptor()

	// Look for standard fields
	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		if !msgRef.Has(fd) {
			continue
		}

		opts := GetFieldOptionsFromDescriptor(fd)
		value := msgRef.Get(fd)

		switch string(fd.Name()) {
		case "name":
			contributor.Name = value.String()
		case "given":
			if contributor.ParsedName == nil {
				contributor.ParsedName = &hubv1.ParsedName{}
			}
			contributor.ParsedName.Given = value.String()
		case "family":
			if contributor.ParsedName == nil {
				contributor.ParsedName = &hubv1.ParsedName{}
			}
			contributor.ParsedName.Family = value.String()
		case "suffix":
			if contributor.ParsedName == nil {
				contributor.ParsedName = &hubv1.ParsedName{}
			}
			contributor.ParsedName.Suffix = value.String()
		case "orcid":
			if opts != nil && opts.IdentifierType == "orcid" {
				contributor.Identifiers = append(contributor.Identifiers, &hubv1.Identifier{
					Type:  hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID,
					Value: value.String(),
				})
			}
		}
	}

	// Build name from parsed parts if not set
	if contributor.Name == "" && contributor.ParsedName != nil {
		pn := contributor.ParsedName
		if pn.Family != "" && pn.Given != "" {
			contributor.Name = fmt.Sprintf("%s, %s", pn.Family, pn.Given)
		} else if pn.Family != "" {
			contributor.Name = pn.Family
		} else if pn.Given != "" {
			contributor.Name = pn.Given
		}
	}
}

// mapDates maps values to the dates field.
func (c *Converter) mapDates(record *hubv1.Record, value any, mapping FieldMapping) error {
	opts := mapping.Options

	dateType := hubv1.DateType_DATE_TYPE_UNSPECIFIED
	if opts.DateType != "" {
		dateTypeName := "DATE_TYPE_" + strings.ToUpper(opts.DateType)
		if val, ok := hubv1.DateType_value[dateTypeName]; ok {
			dateType = hubv1.DateType(val)
		}
	}

	// Handle string values
	str := toString(value)
	if str == "" {
		return nil
	}

	dateValue := &hubv1.DateValue{
		Type: dateType,
		Raw:  str,
	}

	record.Dates = append(record.Dates, dateValue)
	return nil
}

// mapIdentifiers maps values to the identifiers field.
func (c *Converter) mapIdentifiers(record *hubv1.Record, value any, mapping FieldMapping) error {
	opts := mapping.Options

	idType := hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED
	if opts.IdentifierType != "" {
		idTypeName := "IDENTIFIER_TYPE_" + strings.ToUpper(opts.IdentifierType)
		if val, ok := hubv1.IdentifierType_value[idTypeName]; ok {
			idType = hubv1.IdentifierType(val)
		}
	}

	str := toString(value)
	if str == "" {
		return nil
	}

	identifier := &hubv1.Identifier{
		Type:  idType,
		Value: str,
	}

	record.Identifiers = append(record.Identifiers, identifier)
	return nil
}

// mapSubjects maps values to the subjects field.
func (c *Converter) mapSubjects(record *hubv1.Record, value any, mapping FieldMapping) error {
	opts := mapping.Options

	vocab := hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
	if opts.SubjectVocabulary != "" {
		vocabName := "SUBJECT_VOCABULARY_" + strings.ToUpper(opts.SubjectVocabulary)
		if val, ok := hubv1.SubjectVocabulary_value[vocabName]; ok {
			vocab = hubv1.SubjectVocabulary(val)
		}
	}

	// Handle array of strings
	if list, ok := value.([]any); ok {
		for _, item := range list {
			str := toString(item)
			if str != "" {
				record.Subjects = append(record.Subjects, &hubv1.Subject{
					Value:      str,
					Vocabulary: vocab,
				})
			}
		}
		return nil
	}

	// Handle single string
	str := toString(value)
	if str != "" {
		record.Subjects = append(record.Subjects, &hubv1.Subject{
			Value:      str,
			Vocabulary: vocab,
		})
	}

	return nil
}

// mapRelations maps values to the relations field.
func (c *Converter) mapRelations(record *hubv1.Record, value any, mapping FieldMapping) error {
	opts := mapping.Options

	relType := hubv1.RelationType_RELATION_TYPE_UNSPECIFIED
	if opts.RelationType != "" {
		relTypeName := "RELATION_TYPE_" + strings.ToUpper(opts.RelationType)
		if val, ok := hubv1.RelationType_value[relTypeName]; ok {
			relType = hubv1.RelationType(val)
		}
	}

	str := toString(value)
	if str == "" {
		return nil
	}

	relation := &hubv1.Relation{
		Type:        relType,
		TargetTitle: str,
	}

	record.Relations = append(record.Relations, relation)
	return nil
}

// mapNotes adds values to the notes field.
func (c *Converter) mapNotes(record *hubv1.Record, value any) {
	if list, ok := value.([]any); ok {
		for _, item := range list {
			str := toString(item)
			if str != "" {
				record.Notes = append(record.Notes, str)
			}
		}
		return
	}

	str := toString(value)
	if str != "" {
		record.Notes = append(record.Notes, str)
	}
}

// mapExtra adds a value to the extra field.
func (c *Converter) mapExtra(record *hubv1.Record, value any, key, description string) {
	if record.Extra == nil {
		record.Extra = &structpb.Struct{
			Fields: make(map[string]*structpb.Value),
		}
	}

	v, err := structpb.NewValue(value)
	if err != nil {
		// Try converting to string
		v, _ = structpb.NewValue(toString(value))
	}
	if v != nil {
		record.Extra.Fields[key] = v
	}
}

// handleUnmappedField stores unmapped fields in extra.
func (c *Converter) handleUnmappedField(result *ConversionResult, msgRef protoreflect.Message, mapping FieldMapping) {
	fd := mapping.FieldDescriptor
	if !msgRef.Has(fd) {
		return
	}

	value := msgRef.Get(fd)
	goValue := c.protoValueToGo(value, fd)
	c.mapExtra(result.Record, goValue, mapping.Name, "")
}

// toString converts any value to a string.
func toString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// newEmptyRecord creates a new empty Hub Record with initialized slices.
func newEmptyRecord() *hubv1.Record {
	return &hubv1.Record{
		Contributors: make([]*hubv1.Contributor, 0),
		Dates:        make([]*hubv1.DateValue, 0),
		Subjects:     make([]*hubv1.Subject, 0),
		Rights:       make([]*hubv1.Rights, 0),
		Identifiers:  make([]*hubv1.Identifier, 0),
		Notes:        make([]string, 0),
		Relations:    make([]*hubv1.Relation, 0),
		Genres:       make([]*hubv1.Subject, 0),
	}
}
