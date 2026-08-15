package archivesspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/internal/provenanceuri"
)

const (
	maxInputBytes     = int64(64 << 20)
	maxRecordCount    = 100_000
	maxHierarchyDepth = 2_048
)

type decodedInput struct {
	records      []decodedRecord
	ordered      []OrderedRecord
	hierarchical bool
	provenance   format.DatasetProvenance
}

type decodedRecord struct {
	model jsonModel
	raw   json.RawMessage
}

// Parse converts ArchivesSpace JSONModel input into flat Hub records. Call
// ParseDataset when the source hierarchy must be retained.
func (parser *Format) Parse(reader io.Reader, options *format.ParseOptions) ([]*hubv1.Record, error) {
	dataset, err := parser.ParseDataset(reader, options)
	if err != nil {
		return nil, err
	}
	records := make([]*hubv1.Record, len(dataset.Records))
	for index, entry := range dataset.Records {
		records[index] = entry.Record
	}
	return records, nil
}

// ParseDataset converts ArchivesSpace JSONModel input into a validated Hub
// dataset. Plain API list/search responses remain flat because they may be
// partial or paginated; the snapshot and resource_ordered_records forms are
// treated as complete trees and validated fail closed.
func (*Format) ParseDataset(reader io.Reader, options *format.ParseOptions) (*format.Dataset, error) {
	if options != nil && options.SystemProfile != nil {
		return nil, fmt.Errorf("ArchivesSpace profiles are not executable; use the API adapter without a system profile")
	}
	raw, err := readOneJSON(reader)
	if err != nil {
		return nil, fmt.Errorf("parsing ArchivesSpace JSON: %w", err)
	}
	strict := options != nil && options.Strict
	input, err := decodeArchivesSpace(raw, strict)
	if err != nil {
		return nil, fmt.Errorf("parsing ArchivesSpace JSON: %w", err)
	}
	if len(input.records) > maxRecordCount {
		return nil, fmt.Errorf("parsing ArchivesSpace JSON: record count exceeds %d", maxRecordCount)
	}

	dataset, modelsByKey, err := mapDataset(input, options)
	if err != nil {
		return nil, fmt.Errorf("mapping ArchivesSpace dataset: %w", err)
	}
	applyProvenance(dataset, input.provenance, options)
	if input.hierarchical {
		enrichHierarchyMetadata(dataset, modelsByKey, options)
	}
	if err := dataset.Validate(); err != nil {
		return nil, fmt.Errorf("validating ArchivesSpace dataset: %w", err)
	}
	return dataset, nil
}

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
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("decode JSON: multiple top-level values")
		}
		return nil, fmt.Errorf("decode trailing JSON: %w", err)
	}
	return raw, nil
}

func decodeArchivesSpace(raw []byte, strict bool) (*decodedInput, error) {
	switch raw[0] {
	case '[':
		return decodeRecordArray(raw, strict)
	case '{':
		var marker map[string]json.RawMessage
		if err := json.Unmarshal(raw, &marker); err != nil {
			return nil, fmt.Errorf("decode object marker: %w", err)
		}
		if value, ok := marker["crosswalk_format"]; ok {
			var name string
			if err := json.Unmarshal(value, &name); err != nil {
				return nil, fmt.Errorf("crosswalk_format must be a string")
			}
			if name != SnapshotFormat {
				return nil, fmt.Errorf("unsupported snapshot format %q", name)
			}
			return decodeSnapshot(raw, strict)
		}
		var modelType string
		if value, ok := marker["jsonmodel_type"]; ok {
			if err := json.Unmarshal(value, &modelType); err != nil {
				return nil, fmt.Errorf("jsonmodel_type must be a string")
			}
		}
		switch modelType {
		case "resource", "archival_object":
			record, err := decodeRecord(raw, strict)
			if err != nil {
				return nil, err
			}
			return &decodedInput{records: []decodedRecord{record}}, nil
		case "resource_ordered_records":
			return decodeResolvedOrderedRecords(raw, strict)
		case "":
			if _, ok := marker["results"]; ok {
				return decodeSearchResults(raw, strict)
			}
			return nil, fmt.Errorf("jsonmodel_type is required")
		default:
			return nil, fmt.Errorf("unsupported JSONModel type %q", modelType)
		}
	default:
		return nil, fmt.Errorf("top-level JSON must be an object or array")
	}
}

func decodeSnapshot(raw []byte, strict bool) (*decodedInput, error) {
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
	if envelope.OrderedRecords.JSONModelType != "resource_ordered_records" {
		return nil, fmt.Errorf("ordered_records.jsonmodel_type must be %q", "resource_ordered_records")
	}
	if len(envelope.Records) == 0 {
		return nil, fmt.Errorf("snapshot records are required")
	}
	if len(envelope.OrderedRecords.URIs) == 0 {
		return nil, fmt.Errorf("snapshot ordered_records.uris are required")
	}
	records, err := decodeRawRecords(envelope.Records, strict)
	if err != nil {
		return nil, fmt.Errorf("decode snapshot records: %w", err)
	}
	provenance := format.DatasetProvenance{
		Format:        "archivesspace",
		FormatVersion: Version,
		SourceID:      strings.TrimSpace(envelope.SourceID),
	}
	if envelope.SourceURI != "" {
		provenance.SourceURI, err = safeSourceURI(envelope.SourceURI, "")
		if err != nil {
			return nil, fmt.Errorf("snapshot source_uri: %w", err)
		}
	}
	if envelope.RetrievedAt != "" {
		retrievedAt, parseErr := time.Parse(time.RFC3339Nano, envelope.RetrievedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("snapshot retrieved_at: %w", parseErr)
		}
		retrievedAt = retrievedAt.UTC()
		provenance.RetrievedAt = &retrievedAt
	}
	return &decodedInput{
		records:      records,
		ordered:      envelope.OrderedRecords.URIs,
		hierarchical: true,
		provenance:   provenance,
	}, nil
}

func decodeResolvedOrderedRecords(raw []byte, strict bool) (*decodedInput, error) {
	var ordered OrderedRecords
	if err := json.Unmarshal(raw, &ordered); err != nil {
		return nil, fmt.Errorf("decode resource_ordered_records: %w", err)
	}
	if len(ordered.URIs) == 0 {
		return nil, fmt.Errorf("resource_ordered_records.uris are required")
	}
	records := make([]decodedRecord, 0, len(ordered.URIs))
	for index, entry := range ordered.URIs {
		if len(bytes.TrimSpace(entry.Resolved)) == 0 || bytes.Equal(bytes.TrimSpace(entry.Resolved), []byte("null")) {
			return nil, fmt.Errorf("resource_ordered_records.uris[%d]._resolved is required for static conversion", index)
		}
		record, err := decodeRecord(entry.Resolved, strict)
		if err != nil {
			return nil, fmt.Errorf("decode resource_ordered_records.uris[%d]._resolved: %w", index, err)
		}
		if record.model.URI == "" {
			record.model.URI = strings.TrimSpace(entry.Ref)
		} else if record.model.URI != strings.TrimSpace(entry.Ref) {
			return nil, fmt.Errorf("resource_ordered_records.uris[%d] ref %q does not match resolved record URI %q", index, entry.Ref, record.model.URI)
		}
		if record.model.Title == "" {
			record.model.DisplayString = strings.TrimSpace(entry.DisplayString)
		}
		records = append(records, record)
	}
	return &decodedInput{
		records:      records,
		ordered:      ordered.URIs,
		hierarchical: true,
	}, nil
}

func decodeRecordArray(raw []byte, strict bool) (*decodedInput, error) {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode JSONModel array: %w", err)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("JSONModel array is empty")
	}
	records, err := decodeRawRecords(values, strict)
	if err != nil {
		return nil, fmt.Errorf("decode JSONModel array: %w", err)
	}
	return &decodedInput{records: records}, nil
}

func decodeSearchResults(raw []byte, strict bool) (*decodedInput, error) {
	var response searchResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode search results: %w", err)
	}
	if len(response.Results) == 0 {
		return nil, fmt.Errorf("search results are empty")
	}
	values := make([]json.RawMessage, 0, len(response.Results))
	for index, result := range response.Results {
		if result.PrimaryType != "resource" && result.PrimaryType != "archival_object" {
			return nil, fmt.Errorf("search results[%d] has unsupported primary_type %q", index, result.PrimaryType)
		}
		var serialized string
		if err := json.Unmarshal(result.JSON, &serialized); err != nil {
			return nil, fmt.Errorf("search results[%d].json must be a serialized JSONModel object", index)
		}
		serialized = strings.TrimSpace(serialized)
		if serialized == "" {
			return nil, fmt.Errorf("search results[%d].json is empty", index)
		}
		values = append(values, json.RawMessage(serialized))
	}
	records, err := decodeRawRecords(values, strict)
	if err != nil {
		return nil, fmt.Errorf("decode search result JSONModels: %w", err)
	}
	return &decodedInput{records: records}, nil
}

func decodeRawRecords(values []json.RawMessage, strict bool) ([]decodedRecord, error) {
	if len(values) > maxRecordCount {
		return nil, fmt.Errorf("record count exceeds %d", maxRecordCount)
	}
	records := make([]decodedRecord, 0, len(values))
	for index, value := range values {
		record, err := decodeRecord(value, strict)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", index+1, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func decodeRecord(raw json.RawMessage, strict bool) (decodedRecord, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return decodedRecord{}, fmt.Errorf("JSONModel record must be an object")
	}
	var model jsonModel
	if err := json.Unmarshal(raw, &model); err != nil {
		return decodedRecord{}, fmt.Errorf("decode JSONModel record: %w", err)
	}
	if model.JSONModelType != "resource" && model.JSONModelType != "archival_object" {
		return decodedRecord{}, fmt.Errorf("unsupported JSONModel type %q", model.JSONModelType)
	}
	if model.URI != strings.TrimSpace(model.URI) {
		return decodedRecord{}, fmt.Errorf("record URI must not contain surrounding whitespace")
	}
	if model.URI != "" {
		if err := validateJSONModelURI(model.URI); err != nil {
			return decodedRecord{}, fmt.Errorf("record URI: %w", err)
		}
	}
	if strict {
		if err := validateStrictJSONModelRecord(raw, model.JSONModelType); err != nil {
			return decodedRecord{}, err
		}
	}
	return decodedRecord{model: model, raw: append(json.RawMessage(nil), raw...)}, nil
}

func validateStrictJSONModelRecord(raw json.RawMessage, modelType string) error {
	// ArchivesSpace adds JSONModel fields between server releases and plugins
	// may add installation-local fields. Strict mode therefore validates the
	// shapes Crosswalk understands instead of rejecting unknown members; the
	// latter are preserved verbatim-by-value in archivesspace_raw_fields.
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return fmt.Errorf("decode JSONModel structure: %w", err)
	}
	validators := []struct {
		name         string
		resourceOnly bool
		validate     func(json.RawMessage) error
	}{
		{name: "finding_aid_script", resourceOnly: true, validate: validateJSONStringOrNull},
		{name: "revision_statements", resourceOnly: true, validate: validateJSONArrayOfObjects},
		{name: "metadata_rights_declarations", resourceOnly: true, validate: validateJSONArrayOfObjects},
		{name: "user_defined", resourceOnly: true, validate: validateJSONObjectOrNull},
		{name: "collection_management", resourceOnly: true, validate: validateJSONObjectOrNull},
		{name: "rights_statements", validate: validateStrictRightsStatements},
	}
	for _, validator := range validators {
		value, exists := object[validator.name]
		if !exists {
			continue
		}
		if validator.resourceOnly && modelType != "resource" {
			return fmt.Errorf("field %q is not supported for JSONModel type %q", validator.name, modelType)
		}
		if err := validator.validate(value); err != nil {
			return fmt.Errorf("field %q: %w", validator.name, err)
		}
	}
	return nil
}

func validateStrictRightsStatements(raw json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("must be an array of objects")
	}
	var statements []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &statements); err != nil {
		return fmt.Errorf("must be an array of objects")
	}
	for index, statement := range statements {
		if statement == nil {
			return fmt.Errorf("entry %d must be an object", index+1)
		}
		acts, exists := statement["acts"]
		if !exists {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(acts), []byte("null")) {
			return fmt.Errorf("entry %d acts must be an array of objects", index+1)
		}
		var values []json.RawMessage
		if err := json.Unmarshal(acts, &values); err != nil {
			return fmt.Errorf("entry %d acts must be an array of objects", index+1)
		}
		for actIndex, value := range values {
			value = bytes.TrimSpace(value)
			if len(value) == 0 || value[0] != '{' {
				return fmt.Errorf("entry %d act %d must be an object", index+1, actIndex+1)
			}
			var act rightsAct
			if err := json.Unmarshal(value, &act); err != nil {
				return fmt.Errorf("entry %d act %d is malformed: %w", index+1, actIndex+1, err)
			}
			if act.JSONModelType != "" && act.JSONModelType != "rights_statement_act" {
				return fmt.Errorf("entry %d act %d has unsupported JSONModel type %q", index+1, actIndex+1, act.JSONModelType)
			}
		}
	}
	return nil
}

func validateJSONStringOrNull(raw json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("must be a string or null")
	}
	return nil
}

func validateJSONArrayOfObjects(raw json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("must be an array of objects")
	}
	var values []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("must be an array of objects")
	}
	for index, value := range values {
		if value == nil {
			return fmt.Errorf("entry %d must be an object", index+1)
		}
	}
	return nil
}

func validateJSONObjectOrNull(raw json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("must be an object or null")
	}
	return nil
}

func validateJSONModelURI(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse URI: %w", err)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("credentials, query parameters, and fragments are not allowed")
	}
	if parsed.IsAbs() {
		if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("absolute URI must use HTTP or HTTPS and include a host")
		}
		return nil
	}
	if !strings.HasPrefix(parsed.Path, "/") {
		return fmt.Errorf("relative ArchivesSpace URI must start with a slash")
	}
	return nil
}

func mapDataset(input *decodedInput, options *format.ParseOptions) (*format.Dataset, map[string]*jsonModel, error) {
	if input == nil || len(input.records) == 0 {
		return nil, nil, fmt.Errorf("at least one JSONModel record is required")
	}
	keys, recordsByURI, err := assignRecordKeys(input.records, input.hierarchical)
	if err != nil {
		return nil, nil, err
	}

	var nodes []format.HierarchyNode
	orderedRecords := input.records
	orderedKeys := keys
	if input.hierarchical {
		orderedRecords, orderedKeys, nodes, err = orderHierarchy(input.records, recordsByURI, input.ordered)
		if err != nil {
			return nil, nil, err
		}
	} else {
		nodes = make([]format.HierarchyNode, len(input.records))
		for index, key := range keys {
			nodes[index] = format.HierarchyNode{RecordKey: key, Position: index}
		}
	}

	dataset := &format.Dataset{
		Records:    make([]format.DatasetRecord, 0, len(orderedRecords)),
		Hierarchy:  format.Hierarchy{Nodes: nodes},
		Provenance: input.provenance,
	}
	modelsByKey := make(map[string]*jsonModel, len(orderedRecords))
	for index := range orderedRecords {
		record, err := jsonModelToHub(&orderedRecords[index], options)
		if err != nil {
			return nil, nil, fmt.Errorf("record %q: %w", orderedKeys[index], err)
		}
		dataset.Records = append(dataset.Records, format.DatasetRecord{Key: orderedKeys[index], Record: record})
		modelsByKey[orderedKeys[index]] = &orderedRecords[index].model
	}
	return dataset, modelsByKey, nil
}

func assignRecordKeys(records []decodedRecord, hierarchy bool) ([]string, map[string]int, error) {
	keys := make([]string, len(records))
	seenKeys := make(map[string]int, len(records))
	recordsByURI := make(map[string]int, len(records))
	for index := range records {
		model := &records[index].model
		if model.URI != "" {
			if previous, exists := recordsByURI[model.URI]; exists {
				return nil, nil, fmt.Errorf("duplicate record URI %q at records %d and %d", model.URI, previous+1, index+1)
			}
			recordsByURI[model.URI] = index
		} else if hierarchy {
			return nil, nil, fmt.Errorf("hierarchical record %d is missing its URI", index+1)
		}
		key := stableRecordKey(records[index])
		if previous, exists := seenKeys[key]; exists {
			return nil, nil, fmt.Errorf("duplicate record key %q at records %d and %d", key, previous+1, index+1)
		}
		seenKeys[key] = index
		keys[index] = key
	}
	return keys, recordsByURI, nil
}

func stableRecordKey(record decodedRecord) string {
	if record.model.URI != "" {
		return record.model.URI
	}
	var nativeID string
	switch record.model.JSONModelType {
	case "resource":
		nativeID = resourceIdentifier(&record.model)
	case "archival_object":
		nativeID = firstNonempty(record.model.RefID, record.model.ComponentID)
	}
	if nativeID != "" {
		return record.model.JSONModelType + ":" + strings.ReplaceAll(nativeID, " ", "-")
	}
	canonical := record.raw
	var value any
	if json.Unmarshal(record.raw, &value) == nil {
		if encoded, err := json.Marshal(value); err == nil {
			canonical = encoded
		}
	}
	digest := sha256.Sum256(canonical)
	return record.model.JSONModelType + ":" + hex.EncodeToString(digest[:8])
}

func orderHierarchy(records []decodedRecord, recordsByURI map[string]int, ordered []OrderedRecord) ([]decodedRecord, []string, []format.HierarchyNode, error) {
	if len(ordered) != len(records) {
		return nil, nil, nil, fmt.Errorf("ordered record count %d does not match snapshot record count %d", len(ordered), len(records))
	}
	parentByURI := make(map[string]string, len(records))
	rootCount := 0
	rootURI := ""
	for index := range records {
		model := &records[index].model
		for _, candidate := range []struct {
			label     string
			reference string
		}{{"parent", model.Parent.Ref}, {"resource", model.Resource.Ref}} {
			label, reference := candidate.label, candidate.reference
			if reference == "" {
				continue
			}
			if reference != strings.TrimSpace(reference) {
				return nil, nil, nil, fmt.Errorf("record %q %s reference must not contain surrounding whitespace", model.URI, label)
			}
			if err := validateJSONModelURI(reference); err != nil {
				return nil, nil, nil, fmt.Errorf("record %q %s reference: %w", model.URI, label, err)
			}
		}
		parent := parentURI(model)
		if model.JSONModelType == "resource" {
			if parent != "" {
				return nil, nil, nil, fmt.Errorf("resource %q cannot have a hierarchy parent", model.URI)
			}
			rootCount++
			rootURI = model.URI
		} else if parent == "" {
			return nil, nil, nil, fmt.Errorf("archival object %q is missing its parent or resource reference", model.URI)
		}
		if parent != "" {
			if _, exists := recordsByURI[parent]; !exists {
				return nil, nil, nil, fmt.Errorf("record %q refers to missing parent %q", model.URI, parent)
			}
		}
		parentByURI[model.URI] = parent
	}
	if rootCount != 1 {
		return nil, nil, nil, fmt.Errorf("resource hierarchy must contain exactly one resource root, found %d", rootCount)
	}
	for index := range records {
		model := &records[index].model
		if model.JSONModelType == "archival_object" && strings.TrimSpace(model.Resource.Ref) != rootURI {
			return nil, nil, nil, fmt.Errorf("archival object %q resource reference %q does not match hierarchy root %q", model.URI, model.Resource.Ref, rootURI)
		}
	}
	if cycle := hierarchyCycle(parentByURI); len(cycle) > 0 {
		return nil, nil, nil, fmt.Errorf("hierarchy cycle: %s", strings.Join(cycle, " -> "))
	}

	result := make([]decodedRecord, 0, len(records))
	keys := make([]string, 0, len(records))
	nodes := make([]format.HierarchyNode, 0, len(records))
	seen := make(map[string]struct{}, len(ordered))
	depthByURI := make(map[string]int, len(ordered))
	childrenSeen := make(map[string]int)
	for index, entry := range ordered {
		ref := strings.TrimSpace(entry.Ref)
		if ref == "" {
			return nil, nil, nil, fmt.Errorf("ordered_records.uris[%d].ref is required", index)
		}
		if ref != entry.Ref {
			return nil, nil, nil, fmt.Errorf("ordered_records.uris[%d].ref must not contain surrounding whitespace", index)
		}
		if _, duplicate := seen[ref]; duplicate {
			return nil, nil, nil, fmt.Errorf("duplicate ordered record reference %q", ref)
		}
		seen[ref] = struct{}{}
		recordIndex, exists := recordsByURI[ref]
		if !exists {
			return nil, nil, nil, fmt.Errorf("ordered record reference %q has no JSONModel record", ref)
		}
		model := &records[recordIndex].model
		parent := parentByURI[ref]
		expectedDepth := 0
		if parent != "" {
			parentDepth, parentSeen := depthByURI[parent]
			if !parentSeen {
				return nil, nil, nil, fmt.Errorf("record %q appears before hierarchy parent %q", ref, parent)
			}
			expectedDepth = parentDepth + 1
		}
		if expectedDepth > maxHierarchyDepth {
			return nil, nil, nil, fmt.Errorf("record %q exceeds maximum hierarchy depth %d", ref, maxHierarchyDepth)
		}
		if entry.Depth != nil && *entry.Depth != expectedDepth {
			return nil, nil, nil, fmt.Errorf("ordered record %q has depth %d; expected %d from its parent reference", ref, *entry.Depth, expectedDepth)
		}
		if entry.Level != "" && model.Level != "" && strings.TrimSpace(entry.Level) != strings.TrimSpace(model.Level) {
			return nil, nil, nil, fmt.Errorf("ordered record %q has level %q; JSONModel record has level %q", ref, entry.Level, model.Level)
		}
		depthByURI[ref] = expectedDepth
		position := childrenSeen[parent]
		if model.Position != nil && *model.Position != position {
			return nil, nil, nil, fmt.Errorf("record %q has position %d; expected %d from ordered_records", ref, *model.Position, position)
		}
		if model.Title == "" && model.DisplayString == "" {
			model.DisplayString = strings.TrimSpace(entry.DisplayString)
		}
		childrenSeen[parent] = position + 1
		result = append(result, records[recordIndex])
		keys = append(keys, ref)
		nodes = append(nodes, format.HierarchyNode{RecordKey: ref, ParentKey: parent, Position: position})
	}
	if len(seen) != len(records) {
		missing := make([]string, 0)
		for uri := range recordsByURI {
			if _, exists := seen[uri]; !exists {
				missing = append(missing, uri)
			}
		}
		sort.Strings(missing)
		return nil, nil, nil, fmt.Errorf("JSONModel records missing from ordered_records: %s", strings.Join(missing, ", "))
	}
	return result, keys, nodes, nil
}

func hierarchyCycle(parentByURI map[string]string) []string {
	keys := make([]string, 0, len(parentByURI))
	for key := range parentByURI {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(parentByURI))
	for _, start := range keys {
		if state[start] != unvisited {
			continue
		}
		path := make([]string, 0)
		pathIndex := make(map[string]int)
		current := start
		for current != "" && state[current] == unvisited {
			state[current] = visiting
			pathIndex[current] = len(path)
			path = append(path, current)
			current = parentByURI[current]
		}
		if current != "" && state[current] == visiting {
			startIndex := pathIndex[current]
			cycle := append([]string(nil), path[startIndex:]...)
			return append(cycle, current)
		}
		for _, key := range path {
			state[key] = visited
		}
	}
	return nil
}

func parentURI(model *jsonModel) string {
	if model == nil || model.JSONModelType == "resource" {
		return ""
	}
	if parent := strings.TrimSpace(model.Parent.Ref); parent != "" {
		return parent
	}
	return strings.TrimSpace(model.Resource.Ref)
}

func applyProvenance(dataset *format.Dataset, provenance format.DatasetProvenance, options *format.ParseOptions) {
	if dataset.Provenance.Format == "" {
		dataset.Provenance.Format = "archivesspace"
	}
	if dataset.Provenance.FormatVersion == "" {
		dataset.Provenance.FormatVersion = Version
	}
	if provenance.Source != "" {
		dataset.Provenance.Source = provenance.Source
	}
	if provenance.SourceURI != "" {
		dataset.Provenance.SourceURI = provenance.SourceURI
	}
	if provenance.SourceID != "" {
		dataset.Provenance.SourceID = provenance.SourceID
	}
	if provenance.RetrievedAt != nil {
		dataset.Provenance.RetrievedAt = provenance.RetrievedAt
	}
	if options == nil {
		return
	}
	if dataset.Provenance.Source == "" {
		dataset.Provenance.Source = strings.TrimSpace(options.SourceName)
	}
	if dataset.Provenance.SourceURI == "" && options.BaseURL != "" {
		if safe, err := safeSourceURI(options.BaseURL, ""); err == nil {
			dataset.Provenance.SourceURI = safe
		}
	}
	if options.Profile != nil {
		dataset.Provenance.Profile = options.Profile.Name
	}
}

func safeSourceURI(value, base string) (string, error) {
	return provenanceuri.Normalize(value, provenanceuri.Options{
		BaseURL:       base,
		AllowRelative: strings.TrimSpace(base) == "",
	})
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
