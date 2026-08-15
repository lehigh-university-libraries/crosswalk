package omeka_s

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/internal/provenanceuri"
)

var (
	termPattern       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*:[^\s:]+$`)
	sha256Pattern     = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	sensitiveQueryKey = regexp.MustCompile(`(?i)(?:credential|password|secret|signature|token|authorization|auth|identity|api[_-]?key|key_credential)`)
)

func readOneJSON(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("input reader is required")
	}
	limited := &io.LimitedReader{R: reader, N: maxInputBytes + 1}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	if int64(len(raw)) > maxInputBytes {
		return nil, fmt.Errorf("input exceeds %d bytes", maxInputBytes)
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("input is empty")
	}
	if err := validateJSONShape(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func validateJSONShape(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decodeJSONValue(decoder, 0); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode JSON: multiple top-level values")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

func decodeJSONValue(decoder *json.Decoder, depth int) error {
	const maxDepth = 256
	if depth > maxDepth {
		return fmt.Errorf("nesting exceeds %d levels", maxDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := decodeJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := decodeJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("array is not closed")
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delimiter)
	}
	return nil
}

func decodeOmekaInput(raw []byte) (*decodedInput, error) {
	switch raw[0] {
	case '{':
		var marker map[string]json.RawMessage
		if err := json.Unmarshal(raw, &marker); err != nil {
			return nil, fmt.Errorf("decode object marker: %w", err)
		}
		if _, exists := marker["crosswalk_format"]; exists {
			return decodeSnapshot(raw)
		}
		return decodeBareResources([]json.RawMessage{raw})
	case '[':
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("decode resource array: %w", err)
		}
		return decodeBareResources(values)
	default:
		return nil, fmt.Errorf("top-level JSON must be an object or array")
	}
}

func decodeSnapshot(raw []byte) (*decodedInput, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope Snapshot
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode snapshot envelope: %w", err)
	}
	if envelope.CrosswalkFormat != SnapshotFormat {
		return nil, fmt.Errorf("snapshot crosswalk_format must be %q", SnapshotFormat)
	}
	if envelope.Version != SnapshotVersion {
		return nil, fmt.Errorf("unsupported snapshot version %d", envelope.Version)
	}
	totalModel := len(envelope.Vocabularies) + len(envelope.Properties) + len(envelope.ResourceClasses) + len(envelope.ResourceTemplates)
	totalResources := len(envelope.ItemSets) + len(envelope.Items) + len(envelope.Media)
	if totalModel == 0 && totalResources == 0 {
		return nil, fmt.Errorf("snapshot must contain a model or resources")
	}
	if totalModel > maxModelValues {
		return nil, fmt.Errorf("snapshot model count exceeds %d", maxModelValues)
	}
	if totalResources > maxResources {
		return nil, fmt.Errorf("snapshot resource count exceeds %d", maxResources)
	}

	var (
		model *schemaModel
		err   error
	)
	if totalModel > 0 {
		model, err = decodeSchemaModel(envelope)
		if err != nil {
			return nil, fmt.Errorf("decode snapshot model: %w", err)
		}
	}
	resources := make([]*resource, 0, totalResources)
	for _, collection := range []struct {
		name string
		kind resourceKind
		raw  []json.RawMessage
	}{
		{name: "item_sets", kind: kindItemSet, raw: envelope.ItemSets},
		{name: "items", kind: kindItem, raw: envelope.Items},
		{name: "media", kind: kindMedia, raw: envelope.Media},
	} {
		for index, value := range collection.raw {
			decoded, err := decodeResource(value, collection.kind, model)
			if err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", collection.name, index, err)
			}
			resources = append(resources, decoded)
		}
	}
	if err := validateAndSortResources(resources); err != nil {
		return nil, err
	}
	provenance := formatProvenance(envelope.SourceID)
	if envelope.SourceURI != "" {
		provenance.SourceURI, err = safeAPIURI(envelope.SourceURI, true)
		if err != nil {
			return nil, fmt.Errorf("snapshot source_uri: %w", err)
		}
	}
	if envelope.RetrievedAt != "" {
		retrieved, parseErr := time.Parse(time.RFC3339Nano, envelope.RetrievedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("snapshot retrieved_at: %w", parseErr)
		}
		retrieved = retrieved.UTC()
		provenance.RetrievedAt = &retrieved
	}
	if model != nil {
		contract, compileErr := compileModelSnapshot(model, strings.TrimSpace(envelope.SourceID), provenance.SourceURI)
		if compileErr != nil {
			return nil, compileErr
		}
		model.contractFingerprint = contract.Fingerprint.Value
	}
	return &decodedInput{model: model, resources: resources, provenance: provenance}, nil
}

func decodeBareResources(values []json.RawMessage) (*decodedInput, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("resource array is empty")
	}
	if len(values) > maxResources {
		return nil, fmt.Errorf("resource count exceeds %d", maxResources)
	}
	resources := make([]*resource, 0, len(values))
	modelEnvelope := Snapshot{CrosswalkFormat: SnapshotFormat, Version: SnapshotVersion}
	for index, raw := range values {
		types, err := resourceTypes(raw)
		if err != nil {
			return nil, fmt.Errorf("resource[%d]: %w", index, err)
		}
		switch primaryOmekaType(types) {
		case "o:Item":
			decoded, err := decodeResource(raw, kindItem, nil)
			if err != nil {
				return nil, fmt.Errorf("resource[%d]: %w", index, err)
			}
			resources = append(resources, decoded)
		case "o:ItemSet":
			decoded, err := decodeResource(raw, kindItemSet, nil)
			if err != nil {
				return nil, fmt.Errorf("resource[%d]: %w", index, err)
			}
			resources = append(resources, decoded)
		case "o:Media":
			decoded, err := decodeResource(raw, kindMedia, nil)
			if err != nil {
				return nil, fmt.Errorf("resource[%d]: %w", index, err)
			}
			resources = append(resources, decoded)
		case "o:Vocabulary":
			modelEnvelope.Vocabularies = append(modelEnvelope.Vocabularies, raw)
		case "o:Property":
			modelEnvelope.Properties = append(modelEnvelope.Properties, raw)
		case "o:ResourceClass":
			modelEnvelope.ResourceClasses = append(modelEnvelope.ResourceClasses, raw)
		case "o:ResourceTemplate":
			modelEnvelope.ResourceTemplates = append(modelEnvelope.ResourceTemplates, raw)
		default:
			return nil, fmt.Errorf("resource[%d]: unsupported Omeka S @type", index)
		}
	}
	if err := validateAndSortResources(resources); err != nil {
		return nil, err
	}
	if len(resources) == 0 {
		return nil, fmt.Errorf("API response has no item, item set, or media resources")
	}
	var model *schemaModel
	if len(modelEnvelope.Vocabularies)+len(modelEnvelope.Properties)+len(modelEnvelope.ResourceClasses)+len(modelEnvelope.ResourceTemplates) > 0 {
		var err error
		model, err = decodeSchemaModel(modelEnvelope)
		if err != nil {
			return nil, fmt.Errorf("decode API model resources: %w", err)
		}
	}
	provenance := formatProvenance("")
	origin := installationNamespace(resources[0].uri)
	for _, resource := range resources[1:] {
		if installationNamespace(resource.uri) != origin {
			origin = ""
			break
		}
	}
	provenance.SourceURI = origin
	if model != nil {
		contract, compileErr := compileModelSnapshot(model, "", origin)
		if compileErr != nil {
			return nil, compileErr
		}
		model.contractFingerprint = contract.Fingerprint.Value
	}
	return &decodedInput{model: model, resources: resources, provenance: provenance}, nil
}

func formatProvenance(sourceID string) format.DatasetProvenance {
	return format.DatasetProvenance{
		Format:        "omeka-s",
		FormatVersion: Version,
		SourceID:      strings.TrimSpace(sourceID),
	}
}

func resourceTypes(raw json.RawMessage) ([]string, error) {
	var marker map[string]json.RawMessage
	if err := json.Unmarshal(raw, &marker); err != nil {
		return nil, fmt.Errorf("decode JSON-LD resource: %w", err)
	}
	value, exists := marker["@type"]
	if !exists {
		return nil, fmt.Errorf("@type is required")
	}
	var single string
	if err := json.Unmarshal(value, &single); err == nil {
		if strings.TrimSpace(single) == "" {
			return nil, fmt.Errorf("@type must not be empty")
		}
		return []string{single}, nil
	}
	var multiple []string
	if err := json.Unmarshal(value, &multiple); err != nil {
		return nil, fmt.Errorf("@type must be a string or string array")
	}
	if len(multiple) == 0 {
		return nil, fmt.Errorf("@type must not be empty")
	}
	for index, entry := range multiple {
		if strings.TrimSpace(entry) == "" {
			return nil, fmt.Errorf("@type[%d] must not be empty", index)
		}
	}
	return multiple, nil
}

func primaryOmekaType(types []string) string {
	for _, candidate := range []string{"o:Item", "o:ItemSet", "o:Media", "o:Vocabulary", "o:Property", "o:ResourceClass", "o:ResourceTemplate"} {
		for _, actual := range types {
			if actual == candidate {
				return candidate
			}
		}
	}
	return ""
}

func validateAndSortResources(resources []*resource) error {
	seen := make(map[string]struct{}, len(resources))
	for _, value := range resources {
		key := string(value.kind) + ":" + strconv.FormatInt(value.id, 10)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate Omeka S resource %q", key)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].kind != resources[j].kind {
			return resourceKindOrder(resources[i].kind) < resourceKindOrder(resources[j].kind)
		}
		if resources[i].id != resources[j].id {
			return resources[i].id < resources[j].id
		}
		return resources[i].uri < resources[j].uri
	})
	return nil
}

func resourceKindOrder(kind resourceKind) int {
	switch kind {
	case kindItemSet:
		return 0
	case kindItem:
		return 1
	case kindMedia:
		return 2
	default:
		return 3
	}
}

func safeAPIURI(value string, baseOnly bool) (string, error) {
	normalized, err := provenanceuri.Normalize(value, provenanceuri.Options{})
	if err != nil {
		return "", err
	}
	if !baseOnly {
		return normalized, nil
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("parse normalized base URI: %w", err)
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	return parsed.String(), nil
}

func absoluteURI(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("URI contains a control character")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return "", fmt.Errorf("URI must be absolute")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("URI must not contain user credentials")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	switch parsed.Scheme {
	case "javascript", "data", "vbscript", "file":
		return "", fmt.Errorf("URI scheme %q is not allowed", parsed.Scheme)
	}
	for key := range parsed.Query() {
		if sensitiveQueryKey.MatchString(key) {
			return "", fmt.Errorf("URI must not contain credential-like query parameters")
		}
	}
	return parsed.String(), nil
}

func canonicalRaw(raw json.RawMessage) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func modelDigest(collections ...[]json.RawMessage) (string, error) {
	hash := sha256.New()
	for _, collection := range collections {
		canonical := make([][]byte, 0, len(collection))
		for _, raw := range collection {
			value, err := canonicalRaw(raw)
			if err != nil {
				return "", err
			}
			canonical = append(canonical, value)
		}
		sort.Slice(canonical, func(i, j int) bool { return bytes.Compare(canonical[i], canonical[j]) < 0 })
		for _, value := range canonical {
			if _, err := hash.Write(value); err != nil {
				return "", err
			}
			if _, err := hash.Write([]byte{0}); err != nil {
				return "", err
			}
		}
		if _, err := hash.Write([]byte{0xff}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
