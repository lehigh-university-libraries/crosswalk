// Package protoxml provides XML marshaling for protobuf messages using hub.v1 options.
//
// Instead of creating wrapper types with XML tags, annotate your proto fields:
//
//	message Record {
//	  string title = 1 [(hub.v1.field) = {xml_name: "dc:title"}];
//	  repeated Creator creators = 2 [(hub.v1.field) = {xml_name: "dc:creator"}];
//	}
//
// Then marshal directly:
//
//	data, err := protoxml.Marshal(record)
package protoxml

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// MarshalOptions configures XML marshaling behavior.
type MarshalOptions struct {
	Indent string // Indentation string (default: "  ")
}

// Marshal serializes a protobuf message to XML using hub.v1.field options.
func Marshal(m proto.Message) ([]byte, error) {
	return MarshalWithOptions(m, MarshalOptions{Indent: "  "})
}

// MarshalWithOptions serializes with custom options.
func MarshalWithOptions(m proto.Message, opts MarshalOptions) ([]byte, error) {
	var buf strings.Builder
	if err := marshalMessage(&buf, m.ProtoReflect(), opts, 0); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// WriteTo writes XML to a writer.
func WriteTo(w io.Writer, m proto.Message) error {
	data, err := Marshal(m)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(xml.Header))
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func marshalMessage(buf *strings.Builder, msg protoreflect.Message, opts MarshalOptions, depth int) error {
	md := msg.Descriptor()
	indent := strings.Repeat(opts.Indent, depth)

	// Get message options for XML element name and namespaces
	msgOpts := getMessageOptions(md)
	elementName := string(md.Name())
	if msgOpts != nil && msgOpts.XmlName != "" {
		elementName = msgOpts.XmlName
	}

	// Start element
	buf.WriteString(indent)
	buf.WriteString("<")
	buf.WriteString(elementName)

	// Add namespace declarations
	if msgOpts != nil {
		if msgOpts.XmlDefaultNs != "" {
			fmt.Fprintf(buf, ` xmlns="%s"`, msgOpts.XmlDefaultNs)
		}
		for _, ns := range msgOpts.XmlNamespaces {
			parts := strings.SplitN(ns, "=", 2)
			if len(parts) == 2 {
				fmt.Fprintf(buf, ` xmlns:%s="%s"`, parts[0], parts[1])
			}
		}
	}

	// Collect attributes and elements separately
	var attrs []string
	var elements []func() error

	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if !msg.Has(fd) {
			continue
		}

		fieldOpts := getFieldOptions(fd)
		if fieldOpts != nil && fieldOpts.XmlOmit {
			continue
		}

		value := msg.Get(fd)

		// Handle attributes
		if fieldOpts != nil && fieldOpts.XmlAttr {
			xmlName := getXMLName(fd, fieldOpts)
			attrs = append(attrs, fmt.Sprintf(` %s="%s"`, xmlName, escapeXML(formatValue(value, fd))))
			continue
		}

		// Collect elements for later
		elements = append(elements, func() error {
			return marshalField(buf, fd, value, fieldOpts, opts, depth+1)
		})
	}

	// Write attributes
	for _, attr := range attrs {
		buf.WriteString(attr)
	}

	if len(elements) == 0 {
		buf.WriteString("/>\n")
		return nil
	}

	buf.WriteString(">\n")

	// Write child elements
	for _, writeElement := range elements {
		if err := writeElement(); err != nil {
			return err
		}
	}

	// End element
	buf.WriteString(indent)
	buf.WriteString("</")
	buf.WriteString(elementName)
	buf.WriteString(">\n")

	return nil
}

func marshalField(buf *strings.Builder, fd protoreflect.FieldDescriptor, value protoreflect.Value, fieldOpts *hubv1.FieldOptions, opts MarshalOptions, depth int) error {
	xmlName := getXMLName(fd, fieldOpts)

	// Handle repeated fields
	if fd.IsList() {
		list := value.List()
		for i := 0; i < list.Len(); i++ {
			if err := marshalSingleValue(buf, xmlName, list.Get(i), fd, fieldOpts, opts, depth); err != nil {
				return err
			}
		}
		return nil
	}

	// Handle maps (less common in XML)
	if fd.IsMap() {
		// Skip maps for now - could implement as nested elements
		return nil
	}

	return marshalSingleValue(buf, xmlName, value, fd, fieldOpts, opts, depth)
}

func marshalSingleValue(buf *strings.Builder, xmlName string, value protoreflect.Value, fd protoreflect.FieldDescriptor, fieldOpts *hubv1.FieldOptions, opts MarshalOptions, depth int) error {
	indent := strings.Repeat(opts.Indent, depth)

	// Handle nested messages
	if fd.Kind() == protoreflect.MessageKind {
		nestedMsg := value.Message()
		if !nestedMsg.IsValid() {
			return nil
		}

		// Check if this should be chardata (value only, no wrapper)
		if fieldOpts != nil && fieldOpts.XmlChardata {
			// Find a "value" field in the nested message
			valueField := nestedMsg.Descriptor().Fields().ByName("value")
			if valueField != nil && nestedMsg.Has(valueField) {
				buf.WriteString(indent)
				buf.WriteString("<")
				buf.WriteString(xmlName)
				buf.WriteString(">")
				buf.WriteString(escapeXML(formatValue(nestedMsg.Get(valueField), valueField)))
				buf.WriteString("</")
				buf.WriteString(xmlName)
				buf.WriteString(">\n")
				return nil
			}
		}

		// Regular nested message
		buf.WriteString(indent)
		buf.WriteString("<")
		buf.WriteString(xmlName)
		buf.WriteString(">\n")

		if err := marshalMessageFields(buf, nestedMsg, opts, depth+1); err != nil {
			return err
		}

		buf.WriteString(indent)
		buf.WriteString("</")
		buf.WriteString(xmlName)
		buf.WriteString(">\n")
		return nil
	}

	// Scalar value
	buf.WriteString(indent)
	buf.WriteString("<")
	buf.WriteString(xmlName)
	buf.WriteString(">")
	buf.WriteString(escapeXML(formatValue(value, fd)))
	buf.WriteString("</")
	buf.WriteString(xmlName)
	buf.WriteString(">\n")
	return nil
}

func marshalMessageFields(buf *strings.Builder, msg protoreflect.Message, opts MarshalOptions, depth int) error {
	md := msg.Descriptor()
	fields := md.Fields()

	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if !msg.Has(fd) {
			continue
		}

		fieldOpts := getFieldOptions(fd)
		if fieldOpts != nil && fieldOpts.XmlOmit {
			continue
		}
		if fieldOpts != nil && fieldOpts.XmlAttr {
			continue // Already handled as attribute
		}

		value := msg.Get(fd)
		if err := marshalField(buf, fd, value, fieldOpts, opts, depth); err != nil {
			return err
		}
	}
	return nil
}

func getXMLName(fd protoreflect.FieldDescriptor, fieldOpts *hubv1.FieldOptions) string {
	if fieldOpts != nil {
		if fieldOpts.XmlName != "" {
			return fieldOpts.XmlName
		}
		if fieldOpts.XmlNs != "" {
			return fieldOpts.XmlNs + ":" + string(fd.Name())
		}
	}
	return string(fd.Name())
}

func getFieldOptions(fd protoreflect.FieldDescriptor) *hubv1.FieldOptions {
	opts := fd.Options()
	if opts == nil {
		return nil
	}

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

func getMessageOptions(md protoreflect.MessageDescriptor) *hubv1.MessageOptions {
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

func formatValue(value protoreflect.Value, fd protoreflect.FieldDescriptor) string {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		if value.Bool() {
			return "true"
		}
		return "false"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return fmt.Sprintf("%d", value.Int())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return fmt.Sprintf("%d", value.Int())
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return fmt.Sprintf("%d", value.Uint())
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return fmt.Sprintf("%d", value.Uint())
	case protoreflect.FloatKind:
		return fmt.Sprintf("%g", value.Float())
	case protoreflect.DoubleKind:
		return fmt.Sprintf("%g", value.Float())
	case protoreflect.StringKind:
		return value.String()
	case protoreflect.BytesKind:
		return string(value.Bytes())
	case protoreflect.EnumKind:
		return string(fd.Enum().Values().ByNumber(value.Enum()).Name())
	default:
		return fmt.Sprintf("%v", value.Interface())
	}
}

func escapeXML(s string) string {
	var buf strings.Builder
	for _, r := range s {
		switch r {
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '&':
			buf.WriteString("&amp;")
		case '"':
			buf.WriteString("&quot;")
		case '\'':
			buf.WriteString("&apos;")
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}
