// Package convert provides annotation-driven conversion between spoke protos and Hub schema.
//
// This package uses proto reflection to read hub.v1 custom options from spoke
// proto definitions and applies the specified parsers, validators, and serializers
// during conversion.
//
// Usage:
//
//	// Get field options from a spoke message
//	entry := &bibtexv1.Entry{}
//	fieldOpts := convert.GetFieldOptions(entry, "doi")
//	if fieldOpts != nil {
//	    fmt.Println("Target:", fieldOpts.Target)
//	    fmt.Println("Parser:", fieldOpts.Parser)
//	}
//
//	// Get message options
//	msgOpts := convert.GetMessageOptions(entry)
//	if msgOpts != nil {
//	    fmt.Println("Target:", msgOpts.Target)
//	}
package convert

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// GetFieldOptions extracts hub.v1.FieldOptions from a proto message field.
// Returns nil if no options are defined for the field.
func GetFieldOptions(msg proto.Message, fieldName string) *hubv1.FieldOptions {
	md := msg.ProtoReflect().Descriptor()
	fd := md.Fields().ByName(protoreflect.Name(fieldName))
	if fd == nil {
		return nil
	}
	return GetFieldOptionsFromDescriptor(fd)
}

// GetFieldOptionsFromDescriptor extracts hub.v1.FieldOptions from a field descriptor.
// This is the core function for reading field options using proto reflection.
func GetFieldOptionsFromDescriptor(fd protoreflect.FieldDescriptor) *hubv1.FieldOptions {
	opts := fd.Options()
	if opts == nil {
		return nil
	}

	// Use proto.GetExtension to read the custom option
	ext := proto.GetExtension(opts, hubv1.E_Field)
	if ext == nil {
		return nil
	}

	fieldOpts, ok := ext.(*hubv1.FieldOptions)
	if !ok {
		return nil
	}

	return fieldOpts
}

// GetMessageOptions extracts hub.v1.MessageOptions from a proto message.
// Returns nil if no options are defined.
func GetMessageOptions(msg proto.Message) *hubv1.MessageOptions {
	md := msg.ProtoReflect().Descriptor()
	return GetMessageOptionsFromDescriptor(md)
}

// GetMessageOptionsFromDescriptor extracts hub.v1.MessageOptions from a message descriptor.
func GetMessageOptionsFromDescriptor(md protoreflect.MessageDescriptor) *hubv1.MessageOptions {
	opts := md.Options()
	if opts == nil {
		return nil
	}

	ext := proto.GetExtension(opts, hubv1.E_Message)
	if ext == nil {
		return nil
	}

	msgOpts, ok := ext.(*hubv1.MessageOptions)
	if !ok {
		return nil
	}

	return msgOpts
}

// GetEnumValueOptions extracts hub.v1.EnumValueOptions from an enum value.
// Returns nil if no options are defined.
func GetEnumValueOptions(enumVal protoreflect.EnumValueDescriptor) *hubv1.EnumValueOptions {
	opts := enumVal.Options()
	if opts == nil {
		return nil
	}

	ext := proto.GetExtension(opts, hubv1.E_EnumValue)
	if ext == nil {
		return nil
	}

	enumOpts, ok := ext.(*hubv1.EnumValueOptions)
	if !ok {
		return nil
	}

	return enumOpts
}

// FieldMapping contains extracted field information for conversion.
type FieldMapping struct {
	// FieldDescriptor is the proto field descriptor
	FieldDescriptor protoreflect.FieldDescriptor

	// Name is the proto field name
	Name string

	// Options are the hub.v1.FieldOptions if present
	Options *hubv1.FieldOptions
}

// GetAllFieldMappings extracts all field mappings from a proto message.
// This iterates through all fields and extracts their hub.v1 options.
func GetAllFieldMappings(msg proto.Message) []FieldMapping {
	md := msg.ProtoReflect().Descriptor()
	fields := md.Fields()

	mappings := make([]FieldMapping, 0, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		mapping := FieldMapping{
			FieldDescriptor: fd,
			Name:            string(fd.Name()),
			Options:         GetFieldOptionsFromDescriptor(fd),
		}
		mappings = append(mappings, mapping)
	}

	return mappings
}

// GetMappedFields returns only fields that have hub.v1 options defined.
func GetMappedFields(msg proto.Message) []FieldMapping {
	all := GetAllFieldMappings(msg)
	mapped := make([]FieldMapping, 0, len(all))
	for _, m := range all {
		if m.Options != nil && m.Options.Target != "" {
			mapped = append(mapped, m)
		}
	}
	return mapped
}

// GetUnmappedFields returns fields without hub.v1 options.
func GetUnmappedFields(msg proto.Message) []FieldMapping {
	all := GetAllFieldMappings(msg)
	unmapped := make([]FieldMapping, 0, len(all))
	for _, m := range all {
		if m.Options == nil || m.Options.Target == "" {
			unmapped = append(unmapped, m)
		}
	}
	return unmapped
}

// EnumMapping contains extracted enum value information for conversion.
type EnumMapping struct {
	// EnumValue is the proto enum value descriptor
	EnumValue protoreflect.EnumValueDescriptor

	// Name is the enum value name
	Name string

	// Number is the enum value number
	Number int32

	// Options are the hub.v1.EnumValueOptions if present
	Options *hubv1.EnumValueOptions
}

// GetEnumMappings extracts all enum value mappings from an enum descriptor.
func GetEnumMappings(ed protoreflect.EnumDescriptor) []EnumMapping {
	values := ed.Values()
	mappings := make([]EnumMapping, 0, values.Len())

	for i := 0; i < values.Len(); i++ {
		ev := values.Get(i)
		mapping := EnumMapping{
			EnumValue: ev,
			Name:      string(ev.Name()),
			Number:    int32(ev.Number()),
			Options:   GetEnumValueOptions(ev),
		}
		mappings = append(mappings, mapping)
	}

	return mappings
}

// GetEnumMappingByNumber finds an enum mapping by its number value.
func GetEnumMappingByNumber(ed protoreflect.EnumDescriptor, num int32) *EnumMapping {
	values := ed.Values()
	for i := 0; i < values.Len(); i++ {
		ev := values.Get(i)
		if int32(ev.Number()) == num {
			return &EnumMapping{
				EnumValue: ev,
				Name:      string(ev.Name()),
				Number:    num,
				Options:   GetEnumValueOptions(ev),
			}
		}
	}
	return nil
}
