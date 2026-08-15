package islandora_workbench

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/spec"
	"google.golang.org/protobuf/proto"
)

// Artifact is one deterministic, in-memory Workbench output. Callers own
// persistence or transport; planning never creates temporary files.
type Artifact struct {
	Name      string         `json:"name"`
	Operation spec.Operation `json:"operation"`
	MediaType string         `json:"media_type"`
	Data      []byte         `json:"-"`
	Records   int            `json:"records"`
}

// ArtifactPlan is the complete set of named outputs for one conversion.
type ArtifactPlan struct {
	SpecName    string     `json:"spec_name,omitempty"`
	SpecVersion string     `json:"spec_version,omitempty"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	Artifacts   []Artifact `json:"artifacts"`
}

// PlanArtifacts groups records by Workbench operation and renders named CSV
// artifacts in create, update, add-media, agents, pending-supplemental,
// unpublished-supplemental order. It performs no filesystem or network access.
func PlanArtifacts(records []*hubv1.Record, opts *format.SerializeOptions) (*ArtifactPlan, error) {
	if opts == nil {
		opts = format.NewSerializeOptions()
	}
	resolved := *opts
	if resolved.Spec == nil {
		resolved.Spec = spec.FabricatorWorkbench()
	}
	if err := resolved.Spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid transformation specification: %w", err)
	}
	if err := resolved.Spec.ValidateSealed(); err != nil {
		return nil, fmt.Errorf("unsealed transformation specification: %w", err)
	}
	if err := validateArtifactProfileBinding(resolved.Spec, resolved.SystemProfile); err != nil {
		return nil, err
	}
	resolved.MultiValueSeparator = targetMultiValueSeparator(&resolved)
	resolved.ExtraWriters = nil
	resolved.IncludeHeader = true

	plan := &ArtifactPlan{
		SpecName:    resolved.Spec.Name,
		SpecVersion: resolved.Spec.Version,
		Artifacts:   make([]Artifact, 0, 7),
	}
	var err error
	plan.Fingerprint, err = transformationFingerprint(resolved.Spec)
	if err != nil {
		return nil, err
	}

	groups := map[spec.Operation][]*hubv1.Record{
		spec.OperationCreate:   {},
		spec.OperationUpdate:   {},
		spec.OperationAddMedia: {},
	}
	pendingSupplemental := make([]supplementalMediaRow, 0)
	directSupplemental := make([]supplementalMediaRow, 0)
	for index, record := range records {
		if record == nil {
			return nil, fmt.Errorf("record %d is nil", index+1)
		}
		operation, err := operationForRecord(record, &resolved)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", index+1, err)
		}
		planned, pending, direct, err := planSupplementalMedia(record, operation, resolved.Spec)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", index+1, err)
		}
		pendingSupplemental = append(pendingSupplemental, pending...)
		directSupplemental = append(directSupplemental, direct...)
		if planned != nil {
			groups[operation] = append(groups[operation], planned)
		}
	}

	for _, operation := range []spec.Operation{spec.OperationCreate, spec.OperationUpdate, spec.OperationAddMedia} {
		group := groups[operation]
		if len(group) == 0 && (operation != spec.OperationAddMedia || len(directSupplemental) == 0) {
			continue
		}
		var artifact *Artifact
		if len(group) > 0 {
			operationOpts := resolved
			operationOpts.Operation = operation
			var output bytes.Buffer
			if err := (&Format{}).Serialize(&output, group, &operationOpts); err != nil {
				return nil, fmt.Errorf("serializing %s artifact: %w", operation, err)
			}
			artifact = &Artifact{
				Name:      artifactName(operation),
				Operation: operation,
				MediaType: "text/csv; charset=utf-8",
				Data:      append([]byte(nil), output.Bytes()...),
				Records:   len(group),
			}
		}
		if operation == spec.OperationAddMedia && len(directSupplemental) > 0 {
			merged, err := mergeSupplementalAddMediaArtifact(artifact, directSupplemental)
			if err != nil {
				return nil, err
			}
			artifact = &merged
		}
		plan.Artifacts = append(plan.Artifacts, *artifact)
	}

	if artifact, ok, err := planAgentsArtifact(records, resolved.MultiValueSeparator); err != nil {
		return nil, err
	} else if ok {
		plan.Artifacts = append(plan.Artifacts, artifact)
	}
	if artifact, ok, err := planPendingSupplementalArtifact(pendingSupplemental); err != nil {
		return nil, err
	} else if ok {
		plan.Artifacts = append(plan.Artifacts, artifact)
	}
	if artifact, ok, err := planUnpublishedSupplementalArtifact(records, resolved.Spec); err != nil {
		return nil, err
	} else if ok {
		plan.Artifacts = append(plan.Artifacts, artifact)
	}

	if err := appendArtifactManifest(plan, &resolved); err != nil {
		return nil, err
	}
	return plan, nil
}

type supplementalMediaRow struct {
	id          string
	nodeID      string
	file        string
	mediaUseTID string
	published   string
}

func planSupplementalMedia(record *hubv1.Record, operation spec.Operation, transformation *spec.Transformation) (*hubv1.Record, []supplementalMediaRow, []supplementalMediaRow, error) {
	retain := 1
	if operation == spec.OperationAddMedia {
		retain = 0
	}
	supplementalCount := 0
	for _, file := range record.Files {
		if file != nil && file.Path != "" && file.Role == "supplemental" {
			supplementalCount++
		}
	}
	if supplementalCount <= retain {
		return record, nil, nil, nil
	}

	mediaUseTID := transformation.Default(spec.SupplementalMediaUseTIDDefault)
	if mediaUseTID == "" {
		return nil, nil, nil, fmt.Errorf("supplemental media overflow requires the %s default", spec.SupplementalMediaUseTIDDefault)
	}
	published := transformation.Default(spec.SupplementalPublishedDefault)
	if published == "" {
		return nil, nil, nil, fmt.Errorf("supplemental media overflow requires the %s default", spec.SupplementalPublishedDefault)
	}

	clone, ok := proto.Clone(record).(*hubv1.Record)
	if !ok {
		return nil, nil, nil, fmt.Errorf("copying record for supplemental media planning")
	}
	clone.Files = clone.Files[:0]
	id := hub.GetExtraString(record, "id")
	nodeID := hub.GetExtraString(record, "node_id")
	pending := make([]supplementalMediaRow, 0)
	direct := make([]supplementalMediaRow, 0)
	seenSupplemental := 0
	for fileIndex, file := range record.Files {
		if file == nil || file.Path == "" || file.Role != "supplemental" {
			clone.Files = append(clone.Files, file)
			continue
		}
		if seenSupplemental < retain {
			clone.Files = append(clone.Files, file)
			seenSupplemental++
			continue
		}
		seenSupplemental++
		normalized, err := transformation.NormalizeFilePath(file.Path)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("normalizing supplemental file %d: %w", fileIndex+1, err)
		}
		row := supplementalMediaRow{
			id:          id,
			nodeID:      nodeID,
			file:        normalized,
			mediaUseTID: mediaUseTID,
			published:   published,
		}
		if nodeID != "" {
			direct = append(direct, row)
			continue
		}
		if id == "" {
			return nil, nil, nil, fmt.Errorf("supplemental media overflow for a new node requires an upload ID for node-ID reconciliation")
		}
		pending = append(pending, row)
	}
	if operation == spec.OperationAddMedia && !hasPublishableFile(clone) {
		clone = nil
	}
	return clone, pending, direct, nil
}

func mergeSupplementalAddMediaArtifact(existing *Artifact, supplemental []supplementalMediaRow) (Artifact, error) {
	header := []string{"node_id", "file", "media_use_tid", "published"}
	rows := make([][]string, 0, len(supplemental)+1)
	records := 0
	if existing != nil {
		parsed, err := csv.NewReader(bytes.NewReader(existing.Data)).ReadAll()
		if err != nil {
			return Artifact{}, fmt.Errorf("reading add-media artifact for supplemental merge: %w", err)
		}
		if len(parsed) == 0 {
			return Artifact{}, fmt.Errorf("add-media artifact is empty")
		}
		header = append([]string(nil), parsed[0]...)
		for _, required := range []string{"node_id", "file"} {
			if !containsColumn(header, required) {
				return Artifact{}, fmt.Errorf("add-media artifact has no %q column", required)
			}
		}
		for _, optional := range []string{"media_use_tid", "published"} {
			if !containsColumn(header, optional) {
				header = append(header, optional)
			}
		}
		publishedIndex := columnIndex(header, "published")
		for _, existingRow := range parsed[1:] {
			row := make([]string, len(header))
			copy(row, existingRow)
			if strings.TrimSpace(row[publishedIndex]) == "" {
				// Fabricator made regular media explicitly published whenever it
				// merged rows with supplemental-media publication policy.
				row[publishedIndex] = "1"
			}
			rows = append(rows, row)
		}
		records = existing.Records
	}
	for _, supplementalRow := range supplemental {
		values := map[string]string{
			"node_id":       supplementalRow.nodeID,
			"file":          supplementalRow.file,
			"media_use_tid": supplementalRow.mediaUseTID,
			"published":     supplementalRow.published,
		}
		row := make([]string, len(header))
		for index, column := range header {
			row[index] = values[column]
		}
		rows = append(rows, row)
	}
	data, err := writeArtifactCSV(header, rows)
	if err != nil {
		return Artifact{}, fmt.Errorf("serializing supplemental add-media artifact: %w", err)
	}
	return Artifact{
		Name:      "target.add_media.csv",
		Operation: spec.OperationAddMedia,
		MediaType: "text/csv; charset=utf-8",
		Data:      data,
		Records:   records + len(supplemental),
	}, nil
}

func planPendingSupplementalArtifact(rows []supplementalMediaRow) (Artifact, bool, error) {
	if len(rows) == 0 {
		return Artifact{}, false, nil
	}
	data, err := writeSupplementalRows(rows)
	if err != nil {
		return Artifact{}, false, fmt.Errorf("serializing pending supplemental artifact: %w", err)
	}
	return Artifact{
		Name:      "target.pending_supplemental.csv",
		Operation: spec.OperationPendingSupplemental,
		MediaType: "text/csv; charset=utf-8",
		Data:      data,
		Records:   len(rows),
	}, true, nil
}

func writeSupplementalRows(rows []supplementalMediaRow) ([]byte, error) {
	csvRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		csvRows = append(csvRows, []string{row.id, row.nodeID, row.file, row.mediaUseTID, row.published})
	}
	return writeArtifactCSV([]string{"id", "node_id", "file", "media_use_tid", "published"}, csvRows)
}

func writeArtifactCSV(header []string, rows [][]string) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write(header); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func containsColumn(header []string, name string) bool {
	return columnIndex(header, name) >= 0
}

func columnIndex(header []string, name string) int {
	for index, column := range header {
		if column == name {
			return index
		}
	}
	return -1
}

func operationForRecord(record *hubv1.Record, opts *format.SerializeOptions) (spec.Operation, error) {
	if opts.Operation != "" {
		switch opts.Operation {
		case spec.OperationCreate, spec.OperationUpdate, spec.OperationAddMedia:
			return opts.Operation, nil
		default:
			return "", fmt.Errorf("unsupported primary operation %q", opts.Operation)
		}
	}
	if hub.GetExtraString(record, "node_id") == "" {
		return spec.OperationCreate, nil
	}
	hasMetadata, err := hasUpdateMetadata(record, opts)
	if err != nil {
		return "", fmt.Errorf("detecting update metadata: %w", err)
	}
	if hasMetadata {
		return spec.OperationUpdate, nil
	}
	if hasPublishableFile(record) {
		return spec.OperationAddMedia, nil
	}
	return spec.OperationUpdate, nil
}

func hasUpdateMetadata(record *hubv1.Record, opts *format.SerializeOptions) (bool, error) {
	transformation := opts.Spec
	if sourceColumns := hub.GetExtraString(record, "_source_columns"); sourceColumns != "" {
		for _, name := range strings.Split(sourceColumns, "|") {
			field, ok := transformation.SourceField(name)
			if !ok || !field.AppliesTo(spec.OperationUpdate) {
				continue
			}
			switch field.Hub {
			case "Extra.node_id", "Files.primary", "Extra.id", "Extra.parent_id":
				continue
			default:
				return true, nil
			}
		}
		return false, nil
	}

	delimiter := targetMultiValueSeparator(opts)
	canonical, _, err := projectRecordToColumns(record, delimiter)
	if err != nil {
		return false, err
	}
	var profileColumns map[string]string
	if opts.SystemProfile != nil {
		profileColumns, err = recordToProfileColumns(record, opts.SystemProfile, transformation, delimiter)
		if err != nil {
			return false, err
		}
	}
	for _, field := range transformation.Target.Fields {
		if field.Codec == "ignore" || !field.AppliesTo(spec.OperationUpdate) {
			continue
		}
		switch field.Hub {
		case "Extra.node_id", "Files.primary", "Extra.id", "Extra.parent_id":
			continue
		}
		value, valueErr := serializedTargetFieldValue(record, field, canonical, profileColumns, delimiter)
		if valueErr != nil {
			return false, fmt.Errorf("target field %q: %w", field.Name, valueErr)
		}
		if value != "" {
			return true, nil
		}
	}
	return false, nil
}

func hasPublishableFile(record *hubv1.Record) bool {
	for _, file := range record.Files {
		if file != nil && file.Path != "" && file.Role != "unpublished_supplemental" {
			return true
		}
	}
	return false
}

func artifactName(operation spec.Operation) string {
	switch operation {
	case spec.OperationUpdate:
		return "target.update.csv"
	case spec.OperationAddMedia:
		return "target.add_media.csv"
	default:
		return "target.csv"
	}
}

func planAgentsArtifact(records []*hubv1.Record, delimiter string) (Artifact, bool, error) {
	if delimiter == "" {
		delimiter = sep
	}
	rows := make([]workbenchRow, 0, len(records))
	hasAgents := false
	seen := make(map[string]struct{})
	for _, record := range records {
		_, agentRows, err := projectRecordToColumns(record, delimiter)
		if err != nil {
			return Artifact{}, false, fmt.Errorf("preparing agents artifact: %w", err)
		}
		unique := make([][]string, 0, len(agentRows))
		for _, row := range agentRows {
			key := strings.Join(row, "\x00")
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			unique = append(unique, row)
			hasAgents = true
		}
		rows = append(rows, workbenchRow{agents: unique})
	}
	if !hasAgents {
		return Artifact{}, false, nil
	}
	var output bytes.Buffer
	if err := writeAgentsCSV(&output, rows); err != nil {
		return Artifact{}, false, fmt.Errorf("serializing agents artifact: %w", err)
	}
	return Artifact{
		Name:      "agents.csv",
		Operation: spec.OperationAgents,
		MediaType: "text/csv; charset=utf-8",
		Data:      output.Bytes(),
		Records:   len(seen),
	}, true, nil
}

func planUnpublishedSupplementalArtifact(records []*hubv1.Record, transformation *spec.Transformation) (Artifact, bool, error) {
	rows := make([][]string, 0)
	mediaUseTID := transformation.Default(spec.UnpublishedSupplementalMediaUseTIDDefault)
	published := transformation.Default(spec.UnpublishedSupplementalPublishedDefault)
	for recordIndex, record := range records {
		id := hub.GetExtraString(record, "id")
		nodeID := hub.GetExtraString(record, "node_id")
		for fileIndex, file := range record.Files {
			if file == nil || file.Role != "unpublished_supplemental" || file.Path == "" {
				continue
			}
			normalized, err := transformation.NormalizeFilePath(file.Path)
			if err != nil {
				return Artifact{}, false, fmt.Errorf("record %d unpublished supplemental file %d: %w", recordIndex+1, fileIndex+1, err)
			}
			rows = append(rows, []string{id, nodeID, normalized, mediaUseTID, published})
		}
	}
	if len(rows) == 0 {
		return Artifact{}, false, nil
	}
	if mediaUseTID == "" {
		return Artifact{}, false, fmt.Errorf("unpublished supplemental media requires the unpublished_supplemental.media_use_tid default")
	}
	if published == "" {
		return Artifact{}, false, fmt.Errorf("unpublished supplemental media requires the unpublished_supplemental.published default")
	}

	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"id", "node_id", "file", "media_use_tid", "published"}); err != nil {
		return Artifact{}, false, fmt.Errorf("writing unpublished supplemental header: %w", err)
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return Artifact{}, false, fmt.Errorf("writing unpublished supplemental row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return Artifact{}, false, fmt.Errorf("writing unpublished supplemental rows: %w", err)
	}
	return Artifact{
		Name:      "target.unpublished_supplemental.csv",
		Operation: spec.OperationUnpublishedSupplemental,
		MediaType: "text/csv; charset=utf-8",
		Data:      output.Bytes(),
		Records:   len(rows),
	}, true, nil
}
