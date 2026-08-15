package format

import (
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/internal/provenanceuri"
)

const maxDatasetRecords = 100_000

// DatasetParser extends Parser for formats that preserve relationships between
// records. Parse remains available to callers that only need flat records.
type DatasetParser interface {
	Parser

	// ParseDataset reads input into a validated dataset with record identity,
	// hierarchy, diagnostics, and dataset-level provenance.
	ParseDataset(r io.Reader, opts *ParseOptions) (*Dataset, error)
}

// DatasetSerializer extends Serializer for formats that can preserve
// relationships between records. Serialize remains available to callers that
// only have flat records.
type DatasetSerializer interface {
	Serializer

	// SerializeDataset writes a validated dataset, including its hierarchy and
	// dataset-level provenance when the target format can represent them.
	SerializeDataset(w io.Writer, dataset *Dataset, opts *SerializeOptions) error
}

// Dataset is one parsed metadata result set. Records and hierarchy nodes use
// the same canonical depth-first order so serialization is deterministic.
type Dataset struct {
	Records     []DatasetRecord   `json:"records"`
	Hierarchy   Hierarchy         `json:"hierarchy"`
	Diagnostics []Diagnostic      `json:"diagnostics,omitempty"`
	Provenance  DatasetProvenance `json:"provenance"`
}

// DatasetRecord binds a dataset-local stable key to one Hub record.
type DatasetRecord struct {
	Key    string        `json:"key"`
	Record *hubv1.Record `json:"record"`
}

// Hierarchy represents an ordered forest. Nodes are stored in canonical
// depth-first pre-order; Position is zero-based within each sibling group.
type Hierarchy struct {
	Nodes []HierarchyNode `json:"nodes"`
}

// HierarchyNode locates one record in an arbitrary-depth hierarchy. An empty
// ParentKey identifies a root node.
type HierarchyNode struct {
	RecordKey string `json:"record_key"`
	ParentKey string `json:"parent_key,omitempty"`
	Position  int    `json:"position"`
}

// DatasetProvenance records the origin of a parsed result set. SourceURI must
// be safe to persist: acquisition clients must remove credentials before
// assigning it.
type DatasetProvenance struct {
	Format             string     `json:"format"`
	FormatVersion      string     `json:"format_version,omitempty"`
	Source             string     `json:"source,omitempty"`
	SourceURI          string     `json:"source_uri,omitempty"`
	SourceID           string     `json:"source_id,omitempty"`
	RetrievedAt        *time.Time `json:"retrieved_at,omitempty"`
	Profile            string     `json:"profile,omitempty"`
	ProfileFingerprint string     `json:"profile_fingerprint,omitempty"`
	ModelFingerprint   string     `json:"model_fingerprint,omitempty"`
}

// ParseDataset parses with a DatasetParser when available and otherwise wraps
// an existing Parser result in a flat, deterministically keyed dataset.
func ParseDataset(parser Parser, r io.Reader, opts *ParseOptions) (*Dataset, error) {
	if isNilFormat(parser) {
		return nil, fmt.Errorf("parsing dataset: parser is required")
	}

	var (
		dataset *Dataset
		err     error
	)
	if datasetParser, ok := parser.(DatasetParser); ok {
		dataset, err = datasetParser.ParseDataset(r, opts)
	} else {
		var records []*hubv1.Record
		records, err = parser.Parse(r, opts)
		if err == nil {
			if len(records) > maxDatasetRecords {
				return nil, fmt.Errorf("parsing %s dataset: record count exceeds %d", parser.Name(), maxDatasetRecords)
			}
			dataset = flatDataset(records)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("parsing %s dataset: %w", parser.Name(), err)
	}
	if dataset == nil {
		return nil, fmt.Errorf("parsing %s dataset: parser returned a nil dataset", parser.Name())
	}

	if err := applyDatasetProvenanceDefaults(dataset, parser, opts); err != nil {
		return nil, fmt.Errorf("binding %s dataset provenance: %w", parser.Name(), err)
	}
	if err := dataset.Validate(); err != nil {
		return nil, fmt.Errorf("validating %s dataset: %w", parser.Name(), err)
	}
	return dataset, nil
}

// SerializeDataset serializes with a DatasetSerializer when available and
// otherwise passes the dataset's canonical record order to an existing flat
// Serializer. The dataset is validated before any output is written.
func SerializeDataset(serializer Serializer, w io.Writer, dataset *Dataset, opts *SerializeOptions) error {
	if isNilFormat(serializer) {
		return fmt.Errorf("serializing dataset: serializer is required")
	}
	if dataset == nil {
		return fmt.Errorf("serializing %s dataset: dataset is required", serializer.Name())
	}
	if err := dataset.Validate(); err != nil {
		return fmt.Errorf("validating %s dataset: %w", serializer.Name(), err)
	}

	if datasetSerializer, ok := serializer.(DatasetSerializer); ok {
		if err := datasetSerializer.SerializeDataset(w, dataset, opts); err != nil {
			return fmt.Errorf("serializing %s dataset: %w", serializer.Name(), err)
		}
		return nil
	}

	records := make([]*hubv1.Record, len(dataset.Records))
	for index, entry := range dataset.Records {
		records[index] = entry.Record
	}
	if err := serializer.Serialize(w, records, opts); err != nil {
		return fmt.Errorf("serializing %s dataset: %w", serializer.Name(), err)
	}
	return nil
}

// Validate checks record identity, hierarchy references, cycles, sibling
// positions, and canonical ordering without modifying the dataset.
func (d *Dataset) Validate() error {
	if d == nil {
		return datasetValidationError(Diagnostic{
			Source:  "dataset",
			Code:    "nil_dataset",
			Message: "dataset is nil",
		})
	}

	diagnostics := make([]Diagnostic, 0)
	if len(d.Records) > maxDatasetRecords {
		diagnostics = append(diagnostics, datasetDiagnostic(
			"records", "record_limit", fmt.Sprintf("dataset record count exceeds %d", maxDatasetRecords),
		))
	}
	if len(d.Hierarchy.Nodes) > maxDatasetRecords {
		diagnostics = append(diagnostics, datasetDiagnostic(
			"hierarchy.nodes", "hierarchy_limit", fmt.Sprintf("dataset hierarchy node count exceeds %d", maxDatasetRecords),
		))
	}
	if len(d.Records) > maxDatasetRecords || len(d.Hierarchy.Nodes) > maxDatasetRecords {
		return &DiagnosticsError{Diagnostics: diagnostics}
	}
	if strings.TrimSpace(d.Provenance.Format) == "" {
		diagnostics = append(diagnostics, datasetDiagnostic(
			"provenance.format",
			"required_format",
			"dataset provenance format is required",
		))
	} else if d.Provenance.Format != strings.TrimSpace(d.Provenance.Format) {
		diagnostics = append(diagnostics, datasetDiagnostic(
			"provenance.format",
			"invalid_format",
			"dataset provenance format must not contain surrounding whitespace",
		))
	}
	if value := d.Provenance.SourceURI; value != "" {
		normalized, err := provenanceuri.Normalize(value, provenanceuri.Options{AllowRelative: true})
		if err != nil || normalized != value {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"provenance.source_uri",
				"unsafe_provenance_uri",
				"dataset provenance source URI must be a canonical HTTP(S) URI or safe relative reference without credentials or fragments",
			))
		}
	}
	if d.Provenance.ProfileFingerprint != "" || d.Provenance.ModelFingerprint != "" {
		if strings.TrimSpace(d.Provenance.Profile) == "" {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"provenance.profile",
				"required_profile",
				"dataset provenance profile is required when fingerprints are present",
			))
		}
		if d.Provenance.ProfileFingerprint == "" || d.Provenance.ModelFingerprint == "" {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"provenance",
				"incomplete_profile_fingerprints",
				"dataset provenance profile and model fingerprints must be supplied together",
			))
		}
		if d.Provenance.ProfileFingerprint != "" && !validDatasetSHA256(d.Provenance.ProfileFingerprint) {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"provenance.profile_fingerprint",
				"invalid_profile_fingerprint",
				"dataset provenance profile fingerprint must be a lowercase SHA-256 digest",
			))
		}
		if d.Provenance.ModelFingerprint != "" && !validDatasetSHA256(d.Provenance.ModelFingerprint) {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"provenance.model_fingerprint",
				"invalid_model_fingerprint",
				"dataset provenance model fingerprint must be a lowercase SHA-256 digest",
			))
		}
	}

	recordsByKey := make(map[string]int, len(d.Records))
	for index, entry := range d.Records {
		path := "records[" + strconv.Itoa(index) + "]"
		if err := validateDatasetKey(entry.Key); err != nil {
			diagnostics = append(diagnostics, datasetDiagnostic(path+".key", "invalid_record_key", err.Error()))
			continue
		}
		if previous, exists := recordsByKey[entry.Key]; exists {
			diagnostics = append(diagnostics, datasetDiagnostic(
				path+".key",
				"duplicate_record_key",
				fmt.Sprintf("record key %q duplicates records[%d]", entry.Key, previous),
			))
		} else {
			recordsByKey[entry.Key] = index
		}
		if entry.Record == nil {
			diagnostics = append(diagnostics, datasetDiagnostic(path+".record", "nil_record", "Hub record is nil"))
		}
	}

	nodesByKey := make(map[string]HierarchyNode, len(d.Hierarchy.Nodes))
	parentByKey := make(map[string]string, len(d.Hierarchy.Nodes))
	childrenByParent := make(map[string][]HierarchyNode)
	topologyValid := true
	for index, node := range d.Hierarchy.Nodes {
		path := "hierarchy.nodes[" + strconv.Itoa(index) + "]"
		if err := validateDatasetKey(node.RecordKey); err != nil {
			diagnostics = append(diagnostics, datasetDiagnostic(path+".record_key", "invalid_record_key", err.Error()))
			topologyValid = false
			continue
		}
		if _, exists := nodesByKey[node.RecordKey]; exists {
			diagnostics = append(diagnostics, datasetDiagnostic(
				path+".record_key",
				"duplicate_hierarchy_key",
				fmt.Sprintf("record key %q occurs more than once in the hierarchy", node.RecordKey),
			))
			topologyValid = false
			continue
		}
		nodesByKey[node.RecordKey] = node
		parentByKey[node.RecordKey] = node.ParentKey
		childrenByParent[node.ParentKey] = append(childrenByParent[node.ParentKey], node)

		if _, exists := recordsByKey[node.RecordKey]; !exists {
			diagnostics = append(diagnostics, datasetDiagnostic(
				path+".record_key",
				"unknown_record_key",
				fmt.Sprintf("hierarchy record key %q has no dataset record", node.RecordKey),
			))
			topologyValid = false
		}
		if node.ParentKey != "" {
			if err := validateDatasetKey(node.ParentKey); err != nil {
				diagnostics = append(diagnostics, datasetDiagnostic(path+".parent_key", "invalid_parent_key", err.Error()))
				topologyValid = false
			}
		}
		if node.Position < 0 {
			diagnostics = append(diagnostics, datasetDiagnostic(
				path+".position",
				"invalid_position",
				"hierarchy position must be zero or greater",
			))
			topologyValid = false
		}
	}

	for index, entry := range d.Records {
		if _, valid := recordsByKey[entry.Key]; !valid {
			continue
		}
		if _, exists := nodesByKey[entry.Key]; !exists {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"records["+strconv.Itoa(index)+"].key",
				"missing_hierarchy_node",
				fmt.Sprintf("record key %q has no hierarchy node", entry.Key),
			))
			topologyValid = false
		}
	}

	for index, node := range d.Hierarchy.Nodes {
		if node.ParentKey == "" {
			continue
		}
		if _, exists := nodesByKey[node.ParentKey]; !exists {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"hierarchy.nodes["+strconv.Itoa(index)+"].parent_key",
				"unknown_parent_key",
				fmt.Sprintf("parent key %q has no hierarchy node", node.ParentKey),
			))
			topologyValid = false
		}
	}

	if !hierarchyPositionsValid(childrenByParent, &diagnostics) {
		topologyValid = false
	}
	if cycles := hierarchyCycles(parentByKey); len(cycles) > 0 {
		topologyValid = false
		for _, cycle := range cycles {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"hierarchy",
				"hierarchy_cycle",
				"hierarchy contains a cycle: "+strings.Join(cycle, " -> "),
			))
		}
	}

	if topologyValid {
		expected := canonicalHierarchyKeys(childrenByParent)
		if len(expected) != len(d.Hierarchy.Nodes) {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"hierarchy.nodes",
				"unreachable_hierarchy_node",
				"one or more hierarchy nodes are not reachable from a root",
			))
		} else if mismatch := hierarchyOrderMismatch(d.Hierarchy.Nodes, expected); mismatch >= 0 {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"hierarchy.nodes["+strconv.Itoa(mismatch)+"]",
				"non_canonical_hierarchy_order",
				fmt.Sprintf("hierarchy nodes must use depth-first order; expected record key %q", expected[mismatch]),
			))
		} else if mismatch := recordOrderMismatch(d.Records, expected); mismatch >= 0 {
			diagnostics = append(diagnostics, datasetDiagnostic(
				"records["+strconv.Itoa(mismatch)+"]",
				"non_canonical_record_order",
				fmt.Sprintf("records must follow hierarchy order; expected record key %q", expected[mismatch]),
			))
		}
	}

	if len(diagnostics) > 0 {
		return &DiagnosticsError{Diagnostics: diagnostics}
	}
	return nil
}

func flatDataset(records []*hubv1.Record) *Dataset {
	dataset := &Dataset{
		Records: make([]DatasetRecord, len(records)),
		Hierarchy: Hierarchy{
			Nodes: make([]HierarchyNode, len(records)),
		},
	}
	for index, record := range records {
		key := "record-" + strconv.Itoa(index+1)
		dataset.Records[index] = DatasetRecord{Key: key, Record: record}
		dataset.Hierarchy.Nodes[index] = HierarchyNode{RecordKey: key, Position: index}
	}
	return dataset
}

type profileProvenanceBinding struct {
	name               string
	profileFingerprint string
	modelFingerprint   string
}

func applyDatasetProvenanceDefaults(dataset *Dataset, parser Parser, opts *ParseOptions) error {
	if strings.TrimSpace(dataset.Provenance.Format) == "" {
		dataset.Provenance.Format = strings.TrimSpace(parser.Name())
	}
	if opts != nil {
		if dataset.Provenance.Source == "" {
			dataset.Provenance.Source = opts.SourceName
		}
		if opts.SystemProfile != nil {
			expected := profileProvenanceBinding{
				name:               opts.SystemProfile.Name(),
				profileFingerprint: opts.SystemProfile.Fingerprint(),
				modelFingerprint:   opts.SystemProfile.ModelFingerprint(),
			}
			if expected.name == "" || !validDatasetSHA256(expected.profileFingerprint) || !validDatasetSHA256(expected.modelFingerprint) {
				return fmt.Errorf("selected system profile is not a complete immutable profile")
			}
			if system := opts.SystemProfile.System(); system == "" || system != strings.TrimSpace(parser.Name()) {
				return fmt.Errorf("selected profile %q targets system %q, not parser %q", expected.name, system, parser.Name())
			}
			return bindProfileProvenance(dataset, expected)
		}
		if dataset.Provenance.Profile == "" && opts.Profile != nil {
			dataset.Provenance.Profile = opts.Profile.Name
		}
	}
	return bindRecordProvenanceFromDataset(dataset)
}

func bindProfileProvenance(dataset *Dataset, expected profileProvenanceBinding) error {
	datasetActual := datasetProfileProvenance(dataset)
	if err := checkProfileProvenance("dataset provenance", datasetActual, expected); err != nil {
		return err
	}
	for index, entry := range dataset.Records {
		if entry.Record == nil || entry.Record.SourceInfo == nil {
			continue
		}
		path := "records[" + strconv.Itoa(index) + "].record.source_info"
		if err := checkProfileProvenance(path, sourceInfoProfileProvenance(entry.Record.SourceInfo), expected); err != nil {
			return err
		}
	}

	setDatasetProfileProvenance(dataset, expected)
	for _, entry := range dataset.Records {
		if entry.Record == nil {
			continue
		}
		if entry.Record.SourceInfo == nil {
			entry.Record.SourceInfo = &hubv1.SourceInfo{}
		}
		setSourceInfoProfileProvenance(entry.Record.SourceInfo, expected)
	}
	return nil
}

func bindRecordProvenanceFromDataset(dataset *Dataset) error {
	actual := datasetProfileProvenance(dataset)
	if actual.profileFingerprint == "" && actual.modelFingerprint == "" {
		for index, entry := range dataset.Records {
			if entry.Record == nil || entry.Record.SourceInfo == nil {
				continue
			}
			record := sourceInfoProfileProvenance(entry.Record.SourceInfo)
			if record.profileFingerprint != "" || record.modelFingerprint != "" {
				return fmt.Errorf("records[%d].record.source_info has profile fingerprints but dataset provenance does not", index)
			}
		}
		return nil
	}
	if actual.name == "" {
		return fmt.Errorf("dataset provenance profile is required when fingerprints are present")
	}
	if actual.profileFingerprint == "" || actual.modelFingerprint == "" {
		return fmt.Errorf("dataset provenance profile and model fingerprints must be supplied together")
	}
	if !validDatasetSHA256(actual.profileFingerprint) {
		return fmt.Errorf("dataset provenance profile fingerprint must be a lowercase SHA-256 digest")
	}
	if !validDatasetSHA256(actual.modelFingerprint) {
		return fmt.Errorf("dataset provenance model fingerprint must be a lowercase SHA-256 digest")
	}
	return bindProfileProvenance(dataset, actual)
}

func checkProfileProvenance(path string, actual, expected profileProvenanceBinding) error {
	if actual.profileFingerprint != "" || actual.modelFingerprint != "" {
		if actual.profileFingerprint == "" || actual.modelFingerprint == "" {
			return fmt.Errorf("%s profile and model fingerprints must be supplied together", path)
		}
		if !validDatasetSHA256(actual.profileFingerprint) {
			return fmt.Errorf("%s profile fingerprint must be a lowercase SHA-256 digest", path)
		}
		if !validDatasetSHA256(actual.modelFingerprint) {
			return fmt.Errorf("%s model fingerprint must be a lowercase SHA-256 digest", path)
		}
	}
	if actual.name != "" && actual.name != expected.name {
		return fmt.Errorf("%s profile %q conflicts with selected profile %q", path, actual.name, expected.name)
	}
	if actual.profileFingerprint != "" && actual.profileFingerprint != expected.profileFingerprint {
		return fmt.Errorf("%s profile fingerprint conflicts with selected profile %q", path, expected.name)
	}
	if actual.modelFingerprint != "" && actual.modelFingerprint != expected.modelFingerprint {
		return fmt.Errorf("%s model fingerprint conflicts with selected profile %q", path, expected.name)
	}
	return nil
}

func datasetProfileProvenance(dataset *Dataset) profileProvenanceBinding {
	return profileProvenanceBinding{
		name:               dataset.Provenance.Profile,
		profileFingerprint: dataset.Provenance.ProfileFingerprint,
		modelFingerprint:   dataset.Provenance.ModelFingerprint,
	}
}

func sourceInfoProfileProvenance(source *hubv1.SourceInfo) profileProvenanceBinding {
	return profileProvenanceBinding{
		name:               source.GetProfile(),
		profileFingerprint: source.GetProfileFingerprint(),
		modelFingerprint:   source.GetModelFingerprint(),
	}
}

func setDatasetProfileProvenance(dataset *Dataset, provenance profileProvenanceBinding) {
	dataset.Provenance.Profile = provenance.name
	dataset.Provenance.ProfileFingerprint = provenance.profileFingerprint
	dataset.Provenance.ModelFingerprint = provenance.modelFingerprint
}

func setSourceInfoProfileProvenance(source *hubv1.SourceInfo, provenance profileProvenanceBinding) {
	source.Profile = provenance.name
	source.ProfileFingerprint = provenance.profileFingerprint
	source.ModelFingerprint = provenance.modelFingerprint
}

func validateDatasetKey(key string) error {
	if key == "" {
		return fmt.Errorf("record key is required")
	}
	if key != strings.TrimSpace(key) {
		return fmt.Errorf("record key must not contain surrounding whitespace")
	}
	if strings.IndexFunc(key, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character)
	}) >= 0 {
		return fmt.Errorf("record key must not contain whitespace or control characters")
	}
	return nil
}

func validDatasetSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func hierarchyPositionsValid(childrenByParent map[string][]HierarchyNode, diagnostics *[]Diagnostic) bool {
	valid := true
	parentKeys := make([]string, 0, len(childrenByParent))
	for parentKey := range childrenByParent {
		parentKeys = append(parentKeys, parentKey)
	}
	sort.Strings(parentKeys)

	for _, parentKey := range parentKeys {
		children := append([]HierarchyNode(nil), childrenByParent[parentKey]...)
		sortHierarchySiblings(children)
		positions := make([]int, 0, len(children))
		seen := make(map[int]string, len(children))
		for _, child := range children {
			if child.Position < 0 {
				continue
			}
			if previous, exists := seen[child.Position]; exists {
				*diagnostics = append(*diagnostics, datasetDiagnostic(
					"hierarchy",
					"duplicate_sibling_position",
					fmt.Sprintf("records %q and %q both use position %d under parent %q", previous, child.RecordKey, child.Position, parentKey),
				))
				valid = false
				continue
			}
			seen[child.Position] = child.RecordKey
			positions = append(positions, child.Position)
		}
		sort.Ints(positions)
		for expected, position := range positions {
			if position != expected {
				*diagnostics = append(*diagnostics, datasetDiagnostic(
					"hierarchy",
					"non_contiguous_sibling_positions",
					fmt.Sprintf("positions under parent %q must be contiguous from zero; expected %d, found %d", parentKey, expected, position),
				))
				valid = false
				break
			}
		}
	}
	return valid
}

func hierarchyCycles(parentByKey map[string]string) [][]string {
	keys := make([]string, 0, len(parentByKey))
	for key := range parentByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(parentByKey))
	stack := make([]string, 0, len(parentByKey))
	stackIndex := make(map[string]int, len(parentByKey))
	cycles := make([][]string, 0)

	var visit func(string)
	visit = func(key string) {
		state[key] = visiting
		stackIndex[key] = len(stack)
		stack = append(stack, key)

		parentKey := parentByKey[key]
		if _, exists := parentByKey[parentKey]; parentKey != "" && exists {
			switch state[parentKey] {
			case unvisited:
				visit(parentKey)
			case visiting:
				start := stackIndex[parentKey]
				cycle := append([]string(nil), stack[start:]...)
				cycle = append(cycle, parentKey)
				cycles = append(cycles, cycle)
			}
		}

		stack = stack[:len(stack)-1]
		delete(stackIndex, key)
		state[key] = visited
	}

	for _, key := range keys {
		if state[key] == unvisited {
			visit(key)
		}
	}
	return cycles
}

func canonicalHierarchyKeys(childrenByParent map[string][]HierarchyNode) []string {
	children := make(map[string][]HierarchyNode, len(childrenByParent))
	for parentKey, siblings := range childrenByParent {
		children[parentKey] = append([]HierarchyNode(nil), siblings...)
		sortHierarchySiblings(children[parentKey])
	}

	ordered := make([]string, 0)
	var appendChildren func(string)
	appendChildren = func(parentKey string) {
		for _, child := range children[parentKey] {
			ordered = append(ordered, child.RecordKey)
			appendChildren(child.RecordKey)
		}
	}
	appendChildren("")
	return ordered
}

func sortHierarchySiblings(nodes []HierarchyNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Position == nodes[j].Position {
			return nodes[i].RecordKey < nodes[j].RecordKey
		}
		return nodes[i].Position < nodes[j].Position
	})
}

func hierarchyOrderMismatch(nodes []HierarchyNode, expected []string) int {
	for index, node := range nodes {
		if node.RecordKey != expected[index] {
			return index
		}
	}
	return -1
}

func recordOrderMismatch(records []DatasetRecord, expected []string) int {
	if len(records) != len(expected) {
		return 0
	}
	for index, record := range records {
		if record.Key != expected[index] {
			return index
		}
	}
	return -1
}

func datasetDiagnostic(source, code, message string) Diagnostic {
	return Diagnostic{Source: source, Code: code, Message: message}
}

func datasetValidationError(diagnostic Diagnostic) error {
	return &DiagnosticsError{Diagnostics: []Diagnostic{diagnostic}}
}
