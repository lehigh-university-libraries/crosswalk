package protoxml

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Unmarshal populates a proto message from XML using hub.v1 field annotations.
// The root element name must match the message's xml_name annotation.
func Unmarshal(data []byte, msg proto.Message) error {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	return unmarshalFromDecoder(decoder, msg, "")
}

// UnmarshalReader populates a proto message from an XML reader.
func UnmarshalReader(r io.Reader, msg proto.Message) error {
	decoder := xml.NewDecoder(r)
	return unmarshalFromDecoder(decoder, msg, "")
}

// UnmarshalElement populates a proto message from XML, scanning for a specific
// root element name. This is useful for extracting records from OAI-PMH wrappers.
// If rootElement is empty, the message's xml_name annotation is used.
func UnmarshalElement(r io.Reader, msg proto.Message, rootElement string) error {
	decoder := xml.NewDecoder(r)
	return unmarshalFromDecoder(decoder, msg, rootElement)
}

// UnmarshalAll finds all matching elements in the XML and unmarshals each into a new
// proto message. The factory function creates a new empty message for each element.
// This is useful for parsing documents with multiple records (e.g., OAI-PMH ListRecords).
func UnmarshalAll(r io.Reader, factory func() proto.Message) ([]proto.Message, error) {
	decoder := xml.NewDecoder(r)
	sample := factory()
	md := sample.ProtoReflect().Descriptor()

	msgOpts := getMessageOptions(md)
	rootElement := string(md.Name())
	if msgOpts != nil && msgOpts.XmlName != "" {
		rootElement = msgOpts.XmlName
	}

	var results []proto.Message
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing XML: %w", err)
		}

		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		if start.Name.Local == rootElement {
			msg := factory()
			if err := unmarshalMessageElement(decoder, &start, msg.ProtoReflect()); err != nil {
				return nil, fmt.Errorf("unmarshaling element %d: %w", len(results), err)
			}
			results = append(results, msg)
		}
	}

	return results, nil
}

// unmarshalFromDecoder scans for the root element and unmarshals it into the message.
func unmarshalFromDecoder(decoder *xml.Decoder, msg proto.Message, rootElement string) error {
	msgRef := msg.ProtoReflect()
	md := msgRef.Descriptor()

	// Determine expected root element name
	if rootElement == "" {
		msgOpts := getMessageOptions(md)
		if msgOpts != nil && msgOpts.XmlName != "" {
			rootElement = msgOpts.XmlName
		} else {
			rootElement = string(md.Name())
		}
	}

	// Scan for the root element
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			return fmt.Errorf("element %q not found in XML", rootElement)
		}
		if err != nil {
			return fmt.Errorf("parsing XML: %w", err)
		}

		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		if start.Name.Local == rootElement {
			return unmarshalMessageElement(decoder, &start, msgRef)
		}
	}
}

// unmarshalMessageElement populates a proto message from the current XML element.
// The decoder is positioned inside the start element.
func unmarshalMessageElement(decoder *xml.Decoder, start *xml.StartElement, msgRef protoreflect.Message) error {
	md := msgRef.Descriptor()

	// Build lookup: xml name → field descriptor
	fieldsByXMLName := buildFieldLookup(md)

	// Build namespace URI → prefix mapping from message options
	nsURIToPrefix := buildNamespaceLookup(md)

	// Process attributes
	for _, attr := range start.Attr {
		// Skip xmlns declarations
		if attr.Name.Space == "xmlns" || attr.Name.Local == "xmlns" {
			continue
		}

		attrName := resolveXMLName(attr.Name, nsURIToPrefix)
		fd, ok := fieldsByXMLName[attrName]
		if !ok && attr.Name.Space != "" {
			// Try local name only
			fd, ok = fieldsByXMLName[attr.Name.Local]
		}
		if !ok {
			continue
		}

		fieldOpts := getFieldOptions(fd)
		if fieldOpts == nil || !fieldOpts.XmlAttr {
			continue
		}

		if err := setScalarField(msgRef, fd, attr.Value); err != nil {
			return fmt.Errorf("setting attribute %q: %w", attrName, err)
		}
	}

	// Process child elements
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("parsing XML: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			xmlName := resolveXMLName(t.Name, nsURIToPrefix)

			fd, ok := fieldsByXMLName[xmlName]
			if !ok && t.Name.Space != "" {
				// Try local name only as fallback
				fd, ok = fieldsByXMLName[t.Name.Local]
			}
			if !ok {
				// Skip unknown elements
				if err := decoder.Skip(); err != nil {
					return fmt.Errorf("skipping unknown element %q: %w", xmlName, err)
				}
				continue
			}

			if err := unmarshalField(decoder, &t, msgRef, fd); err != nil {
				return fmt.Errorf("unmarshaling field %q (element %q): %w", fd.Name(), xmlName, err)
			}

		case xml.EndElement:
			return nil

		case xml.CharData:
			// Handle chardata fields at message level
			text := strings.TrimSpace(string(t))
			if text == "" {
				continue
			}

			chardataFd := findChardataField(md)
			if chardataFd != nil {
				if err := setScalarField(msgRef, chardataFd, text); err != nil {
					return fmt.Errorf("setting chardata: %w", err)
				}
			}
		}
	}
}

// resolveXMLName converts an xml.Name (which has resolved namespace URIs)
// back to the prefixed form used in proto annotations.
func resolveXMLName(name xml.Name, nsURIToPrefix map[string]string) string {
	if name.Space == "" {
		return name.Local
	}

	if prefix, ok := nsURIToPrefix[name.Space]; ok {
		return prefix + ":" + name.Local
	}

	// Return local name if namespace not recognized
	return name.Local
}

// buildNamespaceLookup builds a map from namespace URI → prefix using message options.
func buildNamespaceLookup(md protoreflect.MessageDescriptor) map[string]string {
	nsURIToPrefix := make(map[string]string)

	msgOpts := getMessageOptions(md)
	if msgOpts == nil {
		return nsURIToPrefix
	}

	for _, ns := range msgOpts.XmlNamespaces {
		parts := strings.SplitN(ns, "=", 2)
		if len(parts) == 2 {
			nsURIToPrefix[parts[1]] = parts[0]
		}
	}

	// Default namespace maps to empty prefix
	if msgOpts.XmlDefaultNs != "" {
		nsURIToPrefix[msgOpts.XmlDefaultNs] = ""
	}

	return nsURIToPrefix
}

// unmarshalField processes a single XML element and sets the corresponding proto field.
func unmarshalField(decoder *xml.Decoder, start *xml.StartElement, msgRef protoreflect.Message, fd protoreflect.FieldDescriptor) error {
	switch fd.Kind() {
	case protoreflect.MessageKind:
		return unmarshalMessageField(decoder, start, msgRef, fd)
	default:
		// Scalar: read text content
		text, err := readElementText(decoder)
		if err != nil {
			return err
		}
		if fd.IsList() {
			return appendScalarField(msgRef, fd, text)
		}
		return setScalarField(msgRef, fd, text)
	}
}

// unmarshalMessageField processes a message-typed field.
func unmarshalMessageField(decoder *xml.Decoder, start *xml.StartElement, parentMsg protoreflect.Message, fd protoreflect.FieldDescriptor) error {
	if fd.IsList() {
		// Repeated message: create new element and append
		newMsg := parentMsg.NewField(fd).List().NewElement().Message()
		if err := unmarshalMessageElement(decoder, start, newMsg); err != nil {
			return err
		}
		list := parentMsg.Mutable(fd).List()
		list.Append(protoreflect.ValueOfMessage(newMsg))
		return nil
	}

	// Singular message: get or create
	childMsg := parentMsg.Mutable(fd).Message()
	return unmarshalMessageElement(decoder, start, childMsg)
}

// buildFieldLookup creates a map from XML element/attribute names to field descriptors.
func buildFieldLookup(md protoreflect.MessageDescriptor) map[string]protoreflect.FieldDescriptor {
	lookup := make(map[string]protoreflect.FieldDescriptor)
	fields := md.Fields()

	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		fieldOpts := getFieldOptions(fd)

		xmlName := string(fd.Name())
		if fieldOpts != nil {
			if fieldOpts.XmlName != "" {
				xmlName = fieldOpts.XmlName
			} else if fieldOpts.XmlNs != "" {
				xmlName = fieldOpts.XmlNs + ":" + string(fd.Name())
			}
		}

		lookup[xmlName] = fd

		// Also map by proto field name as fallback
		protoName := string(fd.Name())
		if protoName != xmlName {
			lookup[protoName] = fd
		}
	}

	return lookup
}

// findChardataField finds the field marked with xml_chardata in a message.
func findChardataField(md protoreflect.MessageDescriptor) protoreflect.FieldDescriptor {
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		fieldOpts := getFieldOptions(fd)
		if fieldOpts != nil && fieldOpts.XmlChardata {
			return fd
		}
	}
	return nil
}

// readElementText reads the text content of an XML element until its end tag.
func readElementText(decoder *xml.Decoder) (string, error) {
	var text strings.Builder
	depth := 0

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			return text.String(), nil
		}
		if err != nil {
			return "", err
		}

		switch t := tok.(type) {
		case xml.CharData:
			if depth == 0 {
				text.Write(t)
			}
		case xml.StartElement:
			depth++
			// Skip nested elements in scalar fields
		case xml.EndElement:
			if depth == 0 {
				return strings.TrimSpace(text.String()), nil
			}
			depth--
		}
	}
}

// setScalarField sets a scalar proto field from a string value.
func setScalarField(msgRef protoreflect.Message, fd protoreflect.FieldDescriptor, text string) error {
	val, err := parseScalar(fd, text)
	if err != nil {
		return err
	}
	msgRef.Set(fd, val)
	return nil
}

// appendScalarField appends a value to a repeated scalar field.
func appendScalarField(msgRef protoreflect.Message, fd protoreflect.FieldDescriptor, text string) error {
	val, err := parseScalar(fd, text)
	if err != nil {
		return err
	}
	list := msgRef.Mutable(fd).List()
	list.Append(val)
	return nil
}

// parseScalar converts a string to a protoreflect.Value for the given field type.
func parseScalar(fd protoreflect.FieldDescriptor, text string) (protoreflect.Value, error) {
	switch fd.Kind() {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(text), nil
	case protoreflect.BoolKind:
		b := text == "true" || text == "1" || text == "yes"
		return protoreflect.ValueOfBool(b), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		n, err := strconv.ParseInt(text, 10, 32)
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("parsing int32 %q: %w", text, err)
		}
		return protoreflect.ValueOfInt32(int32(n)), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("parsing int64 %q: %w", text, err)
		}
		return protoreflect.ValueOfInt64(n), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		n, err := strconv.ParseUint(text, 10, 32)
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("parsing uint32 %q: %w", text, err)
		}
		return protoreflect.ValueOfUint32(uint32(n)), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		n, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("parsing uint64 %q: %w", text, err)
		}
		return protoreflect.ValueOfUint64(n), nil
	case protoreflect.FloatKind:
		f, err := strconv.ParseFloat(text, 32)
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("parsing float %q: %w", text, err)
		}
		return protoreflect.ValueOfFloat32(float32(f)), nil
	case protoreflect.DoubleKind:
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("parsing double %q: %w", text, err)
		}
		return protoreflect.ValueOfFloat64(f), nil
	case protoreflect.EnumKind:
		return parseEnumValue(fd, text)
	case protoreflect.BytesKind:
		return protoreflect.ValueOfBytes([]byte(text)), nil
	default:
		return protoreflect.Value{}, fmt.Errorf("unsupported field kind: %v", fd.Kind())
	}
}

// parseEnumValue parses an enum value from a string.
// Tries matching by name first, then by hub.v1.enum_value target annotation.
func parseEnumValue(fd protoreflect.FieldDescriptor, text string) (protoreflect.Value, error) {
	ed := fd.Enum()
	values := ed.Values()

	// Try exact match by enum value name
	upper := strings.ToUpper(text)
	for i := 0; i < values.Len(); i++ {
		ev := values.Get(i)
		if string(ev.Name()) == upper || string(ev.Name()) == text {
			return protoreflect.ValueOfEnum(ev.Number()), nil
		}
	}

	// Try matching via enum_value target annotation
	for i := 0; i < values.Len(); i++ {
		ev := values.Get(i)
		enumOpts := getEnumValueOptions(ev)
		if enumOpts != nil && enumOpts.Target == text {
			return protoreflect.ValueOfEnum(ev.Number()), nil
		}
	}

	// Try case-insensitive match on the last segment after _
	// e.g., "MSC2000" matching "CLASSIFICATION_SCHEME_MSC2000"
	for i := 0; i < values.Len(); i++ {
		ev := values.Get(i)
		name := string(ev.Name())
		parts := strings.Split(name, "_")
		suffix := parts[len(parts)-1]
		if strings.EqualFold(suffix, text) {
			return protoreflect.ValueOfEnum(ev.Number()), nil
		}
		// Also try joining last N parts for multi-word suffixes
		if len(parts) > 2 {
			// Try 2-part suffix like "ENCRYPTED_TEX"
			twoPartSuffix := parts[len(parts)-2] + "_" + parts[len(parts)-1]
			if strings.EqualFold(twoPartSuffix, strings.ReplaceAll(text, "-", "_")) {
				return protoreflect.ValueOfEnum(ev.Number()), nil
			}
		}
	}

	// Try numeric value
	if n, err := strconv.ParseInt(text, 10, 32); err == nil {
		return protoreflect.ValueOfEnum(protoreflect.EnumNumber(n)), nil
	}

	// Default to 0 (unspecified) rather than error
	return protoreflect.ValueOfEnum(0), nil
}

// getEnumValueOptions extracts hub.v1.EnumValueOptions from an enum value descriptor.
func getEnumValueOptions(ev protoreflect.EnumValueDescriptor) *hubv1.EnumValueOptions {
	opts := ev.Options()
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
