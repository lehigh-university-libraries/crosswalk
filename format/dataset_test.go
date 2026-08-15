package format_test

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
)

type flatTestParser struct {
	records []*hubv1.Record
	err     error
}

func (*flatTestParser) Name() string { return "flat-test" }

func (*flatTestParser) Description() string { return "flat test parser" }

func (*flatTestParser) Extensions() []string { return nil }

func (*flatTestParser) CanParse([]byte) bool { return true }

func (p *flatTestParser) Parse(io.Reader, *format.ParseOptions) ([]*hubv1.Record, error) {
	return p.records, p.err
}

type hierarchyTestParser struct {
	dataset    *format.Dataset
	parseCalls int
}

type flatTestSerializer struct {
	records []*hubv1.Record
	calls   int
	err     error
}

func (*flatTestSerializer) Name() string { return "flat-target" }

func (*flatTestSerializer) Description() string { return "flat test serializer" }

func (*flatTestSerializer) Extensions() []string { return nil }

func (*flatTestSerializer) CanParse([]byte) bool { return false }

func (s *flatTestSerializer) Serialize(w io.Writer, records []*hubv1.Record, _ *format.SerializeOptions) error {
	s.calls++
	s.records = records
	if s.err != nil {
		return s.err
	}
	_, err := io.WriteString(w, "flat")
	return err
}

type hierarchyTestSerializer struct {
	flatCalls    int
	datasetCalls int
	dataset      *format.Dataset
}

func (*hierarchyTestSerializer) Name() string { return "hierarchy-target" }

func (*hierarchyTestSerializer) Description() string { return "hierarchy test serializer" }

func (*hierarchyTestSerializer) Extensions() []string { return nil }

func (*hierarchyTestSerializer) CanParse([]byte) bool { return false }

func (s *hierarchyTestSerializer) Serialize(io.Writer, []*hubv1.Record, *format.SerializeOptions) error {
	s.flatCalls++
	return errors.New("flat Serialize must not be called")
}

func (s *hierarchyTestSerializer) SerializeDataset(w io.Writer, dataset *format.Dataset, _ *format.SerializeOptions) error {
	s.datasetCalls++
	s.dataset = dataset
	_, err := io.WriteString(w, dataset.Provenance.SourceID)
	return err
}

func (*hierarchyTestParser) Name() string { return "hierarchy-test" }

func (*hierarchyTestParser) Description() string { return "hierarchy test parser" }

func (*hierarchyTestParser) Extensions() []string { return nil }

func (*hierarchyTestParser) CanParse([]byte) bool { return true }

func (p *hierarchyTestParser) Parse(io.Reader, *format.ParseOptions) ([]*hubv1.Record, error) {
	p.parseCalls++
	return nil, errors.New("flat Parse must not be called")
}

func (p *hierarchyTestParser) ParseDataset(io.Reader, *format.ParseOptions) (*format.Dataset, error) {
	return p.dataset, nil
}

func TestParseDatasetWrapsFlatParser(t *testing.T) {
	t.Parallel()

	parser := &flatTestParser{records: []*hubv1.Record{{Title: "First"}, {Title: "Second"}}}
	dataset, err := format.ParseDataset(parser, strings.NewReader("input"), &format.ParseOptions{
		SourceName: "records.json",
		Profile:    &mapping.Profile{Name: "institution-a"},
	})
	if err != nil {
		t.Fatalf("ParseDataset() error = %v", err)
	}

	if dataset.Provenance.Format != "flat-test" || dataset.Provenance.Source != "records.json" || dataset.Provenance.Profile != "institution-a" {
		t.Fatalf("provenance = %+v", dataset.Provenance)
	}
	wantRecords := []format.DatasetRecord{
		{Key: "record-1", Record: parser.records[0]},
		{Key: "record-2", Record: parser.records[1]},
	}
	if !reflect.DeepEqual(dataset.Records, wantRecords) {
		t.Fatalf("records = %#v, want %#v", dataset.Records, wantRecords)
	}
	wantNodes := []format.HierarchyNode{
		{RecordKey: "record-1", Position: 0},
		{RecordKey: "record-2", Position: 1},
	}
	if !reflect.DeepEqual(dataset.Hierarchy.Nodes, wantNodes) {
		t.Fatalf("hierarchy = %#v, want %#v", dataset.Hierarchy.Nodes, wantNodes)
	}
	if err := dataset.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestParseDatasetUsesDatasetParser(t *testing.T) {
	t.Parallel()

	dataset := validDataset()
	dataset.Provenance.Format = ""
	parser := &hierarchyTestParser{dataset: dataset}
	got, err := format.ParseDataset(parser, strings.NewReader("input"), nil)
	if err != nil {
		t.Fatalf("ParseDataset() error = %v", err)
	}
	if got != dataset {
		t.Fatal("ParseDataset() did not return the DatasetParser result")
	}
	if parser.parseCalls != 0 {
		t.Fatalf("Parse() calls = %d, want 0", parser.parseCalls)
	}
	if got.Provenance.Format != "hierarchy-test" {
		t.Fatalf("provenance format = %q, want hierarchy-test", got.Provenance.Format)
	}
}

func TestParseDatasetBindsSelectedSystemProfileToDatasetAndRecords(t *testing.T) {
	t.Parallel()

	compiled := compiledDatasetTestProfile(t, "hierarchy-test", "site-profile")
	dataset := validDataset()
	dataset.Provenance = format.DatasetProvenance{Format: "hierarchy-test", Profile: compiled.Name()}
	dataset.Records[0].Record.SourceInfo = nil
	dataset.Records[1].Record.SourceInfo = &hubv1.SourceInfo{Profile: compiled.Name()}
	dataset.Records[2].Record = &hubv1.Record{}
	dataset.Records[3].Record.SourceInfo = &hubv1.SourceInfo{
		Profile: compiled.Name(), ProfileFingerprint: compiled.Fingerprint(), ModelFingerprint: compiled.ModelFingerprint(),
	}

	parsed, err := format.ParseDataset(
		&hierarchyTestParser{dataset: dataset},
		strings.NewReader("input"),
		&format.ParseOptions{SystemProfile: compiled},
	)
	if err != nil {
		t.Fatalf("ParseDataset() error = %v", err)
	}
	if parsed.Provenance.Profile != compiled.Name() || parsed.Provenance.ProfileFingerprint != compiled.Fingerprint() || parsed.Provenance.ModelFingerprint != compiled.ModelFingerprint() {
		t.Fatalf("dataset profile provenance = %#v", parsed.Provenance)
	}
	for index, entry := range parsed.Records {
		source := entry.Record.GetSourceInfo()
		if source.GetProfile() != compiled.Name() || source.GetProfileFingerprint() != compiled.Fingerprint() || source.GetModelFingerprint() != compiled.ModelFingerprint() {
			t.Errorf("record %d profile provenance = %#v", index, source)
		}
	}
}

func TestParseDatasetRejectsConflictingSelectedSystemProfileProvenance(t *testing.T) {
	t.Parallel()

	compiled := compiledDatasetTestProfile(t, "hierarchy-test", "site-profile")
	otherDigest := strings.Repeat("c", 64)
	tests := []struct {
		name   string
		mutate func(*format.Dataset)
		want   string
	}{
		{
			name: "dataset profile name",
			mutate: func(dataset *format.Dataset) {
				dataset.Provenance.Profile = "other-profile"
			},
			want: "dataset provenance profile",
		},
		{
			name: "dataset fingerprint",
			mutate: func(dataset *format.Dataset) {
				dataset.Provenance.Profile = compiled.Name()
				dataset.Provenance.ProfileFingerprint = otherDigest
				dataset.Provenance.ModelFingerprint = compiled.ModelFingerprint()
			},
			want: "dataset provenance profile fingerprint conflicts",
		},
		{
			name: "dataset incomplete pair",
			mutate: func(dataset *format.Dataset) {
				dataset.Provenance.Profile = compiled.Name()
				dataset.Provenance.ProfileFingerprint = compiled.Fingerprint()
			},
			want: "dataset provenance profile and model fingerprints must be supplied together",
		},
		{
			name: "record profile name",
			mutate: func(dataset *format.Dataset) {
				dataset.Records[1].Record.SourceInfo = &hubv1.SourceInfo{Profile: "other-profile"}
			},
			want: "records[1].record.source_info profile",
		},
		{
			name: "record fingerprint",
			mutate: func(dataset *format.Dataset) {
				dataset.Records[1].Record.SourceInfo = &hubv1.SourceInfo{
					Profile: compiled.Name(), ProfileFingerprint: otherDigest, ModelFingerprint: compiled.ModelFingerprint(),
				}
			},
			want: "records[1].record.source_info profile fingerprint conflicts",
		},
		{
			name: "record incomplete pair",
			mutate: func(dataset *format.Dataset) {
				dataset.Records[1].Record.SourceInfo = &hubv1.SourceInfo{
					Profile: compiled.Name(), ProfileFingerprint: compiled.Fingerprint(),
				}
			},
			want: "records[1].record.source_info profile and model fingerprints must be supplied together",
		},
		{
			name: "record uppercase digest",
			mutate: func(dataset *format.Dataset) {
				dataset.Records[1].Record.SourceInfo = &hubv1.SourceInfo{
					Profile: compiled.Name(), ProfileFingerprint: strings.ToUpper(compiled.Fingerprint()), ModelFingerprint: compiled.ModelFingerprint(),
				}
			},
			want: "profile fingerprint must be a lowercase SHA-256 digest",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dataset := validDataset()
			dataset.Provenance.Format = "hierarchy-test"
			test.mutate(dataset)
			_, err := format.ParseDataset(
				&hierarchyTestParser{dataset: dataset},
				strings.NewReader("input"),
				&format.ParseOptions{SystemProfile: compiled},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ParseDataset() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestParseDatasetBindsAndChecksParserSuppliedProfileProvenance(t *testing.T) {
	t.Parallel()

	profileDigest := strings.Repeat("a", 64)
	modelDigest := strings.Repeat("b", 64)
	dataset := validDataset()
	dataset.Provenance = format.DatasetProvenance{
		Format: "hierarchy-test", Profile: "parser-profile",
		ProfileFingerprint: profileDigest, ModelFingerprint: modelDigest,
	}
	dataset.Records[0].Record.SourceInfo = nil
	parsed, err := format.ParseDataset(&hierarchyTestParser{dataset: dataset}, strings.NewReader("input"), nil)
	if err != nil {
		t.Fatalf("ParseDataset() error = %v", err)
	}
	for index, entry := range parsed.Records {
		source := entry.Record.GetSourceInfo()
		if source.GetProfile() != "parser-profile" || source.GetProfileFingerprint() != profileDigest || source.GetModelFingerprint() != modelDigest {
			t.Errorf("record %d profile provenance = %#v", index, source)
		}
	}

	conflicting := validDataset()
	conflicting.Provenance = dataset.Provenance
	conflicting.Records[0].Record.SourceInfo = &hubv1.SourceInfo{
		Profile: "parser-profile", ProfileFingerprint: strings.Repeat("c", 64), ModelFingerprint: modelDigest,
	}
	if _, err := format.ParseDataset(&hierarchyTestParser{dataset: conflicting}, strings.NewReader("input"), nil); err == nil || !strings.Contains(err.Error(), "records[0].record.source_info profile fingerprint conflicts") {
		t.Fatalf("conflicting parser provenance error = %v", err)
	}
}

func TestParseDatasetRejectsRecordFingerprintsWithoutDatasetBinding(t *testing.T) {
	t.Parallel()

	dataset := validDataset()
	dataset.Provenance.Format = "hierarchy-test"
	dataset.Records[0].Record.SourceInfo = &hubv1.SourceInfo{
		Profile: "record-only", ProfileFingerprint: strings.Repeat("a", 64), ModelFingerprint: strings.Repeat("b", 64),
	}
	_, err := format.ParseDataset(&hierarchyTestParser{dataset: dataset}, strings.NewReader("input"), nil)
	if err == nil || !strings.Contains(err.Error(), "has profile fingerprints but dataset provenance does not") {
		t.Fatalf("ParseDataset() error = %v", err)
	}
}

func TestParseDatasetRejectsInvalidResultsAndParserErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parser format.Parser
		code   string
		text   string
	}{
		{name: "nil parser", parser: nil, text: "parser is required"},
		{name: "parser error", parser: &flatTestParser{err: errors.New("broken input")}, text: "broken input"},
		{name: "nil record", parser: &flatTestParser{records: []*hubv1.Record{nil}}, code: "nil_record"},
		{name: "nil dataset", parser: &hierarchyTestParser{}, text: "nil dataset"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := format.ParseDataset(test.parser, strings.NewReader("input"), nil)
			if err == nil {
				t.Fatal("ParseDataset() error = nil")
			}
			if test.text != "" && !strings.Contains(err.Error(), test.text) {
				t.Fatalf("ParseDataset() error = %v, want text %q", err, test.text)
			}
			if test.code != "" && !hasDiagnosticCode(err, test.code) {
				t.Fatalf("ParseDataset() error = %v, want diagnostic code %q", err, test.code)
			}
		})
	}
}

func TestSerializeDatasetUsesDatasetSerializerWithProvenance(t *testing.T) {
	t.Parallel()

	dataset := validDataset()
	dataset.Provenance.SourceID = "snapshot-123"
	serializer := &hierarchyTestSerializer{}
	var output bytes.Buffer
	if err := format.SerializeDataset(serializer, &output, dataset, nil); err != nil {
		t.Fatalf("SerializeDataset() error = %v", err)
	}
	if serializer.dataset != dataset || serializer.datasetCalls != 1 || serializer.flatCalls != 0 {
		t.Fatalf("serializer calls = dataset %d, flat %d, dataset pointer %p", serializer.datasetCalls, serializer.flatCalls, serializer.dataset)
	}
	if output.String() != "snapshot-123" {
		t.Fatalf("output = %q, want provenance source ID", output.String())
	}
}

func TestSerializeDatasetFallsBackToCanonicalFlatRecords(t *testing.T) {
	t.Parallel()

	dataset := validDataset()
	serializer := &flatTestSerializer{}
	var output bytes.Buffer
	if err := format.SerializeDataset(serializer, &output, dataset, nil); err != nil {
		t.Fatalf("SerializeDataset() error = %v", err)
	}
	if serializer.calls != 1 || len(serializer.records) != len(dataset.Records) {
		t.Fatalf("serializer calls = %d, records = %d", serializer.calls, len(serializer.records))
	}
	for index, entry := range dataset.Records {
		if serializer.records[index] != entry.Record {
			t.Fatalf("record %d pointer changed", index)
		}
	}
	if output.String() != "flat" {
		t.Fatalf("output = %q", output.String())
	}
}

func TestSerializeDatasetValidatesBeforeWriting(t *testing.T) {
	t.Parallel()

	dataset := validDataset()
	dataset.Provenance.Format = ""
	serializer := &hierarchyTestSerializer{}
	var output bytes.Buffer
	err := format.SerializeDataset(serializer, &output, dataset, nil)
	if err == nil || !hasDiagnosticCode(err, "required_format") {
		t.Fatalf("SerializeDataset() error = %v, want required_format", err)
	}
	if serializer.datasetCalls != 0 || serializer.flatCalls != 0 || output.Len() != 0 {
		t.Fatalf("invalid dataset reached serializer: dataset %d, flat %d, bytes %d", serializer.datasetCalls, serializer.flatCalls, output.Len())
	}
}

func TestDatasetValidateAcceptsOrderedArbitraryDepthHierarchy(t *testing.T) {
	t.Parallel()

	if err := validDataset().Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestDatasetValidateRejectsBrokenContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*format.Dataset)
		code   string
	}{
		{
			name: "missing provenance format",
			mutate: func(dataset *format.Dataset) {
				dataset.Provenance.Format = ""
			},
			code: "required_format",
		},
		{
			name: "credential-bearing provenance URI",
			mutate: func(dataset *format.Dataset) {
				dataset.Provenance.SourceURI = "https://user:password@example.org/records/1"
			},
			code: "unsafe_provenance_uri",
		},
		{
			name: "secret-bearing provenance URI query",
			mutate: func(dataset *format.Dataset) {
				dataset.Provenance.SourceURI = "https://example.org/records/1?access_token=secret"
			},
			code: "unsafe_provenance_uri",
		},
		{
			name: "invalid record key",
			mutate: func(dataset *format.Dataset) {
				dataset.Records[2].Key = "grand child"
			},
			code: "invalid_record_key",
		},
		{
			name: "duplicate record key",
			mutate: func(dataset *format.Dataset) {
				dataset.Records[1].Key = "root"
			},
			code: "duplicate_record_key",
		},
		{
			name: "nil Hub record",
			mutate: func(dataset *format.Dataset) {
				dataset.Records[0].Record = nil
			},
			code: "nil_record",
		},
		{
			name: "unknown hierarchy record",
			mutate: func(dataset *format.Dataset) {
				dataset.Hierarchy.Nodes[2].RecordKey = "unknown"
			},
			code: "unknown_record_key",
		},
		{
			name: "record missing hierarchy node",
			mutate: func(dataset *format.Dataset) {
				dataset.Hierarchy.Nodes = dataset.Hierarchy.Nodes[:len(dataset.Hierarchy.Nodes)-1]
			},
			code: "missing_hierarchy_node",
		},
		{
			name: "duplicate hierarchy key",
			mutate: func(dataset *format.Dataset) {
				dataset.Hierarchy.Nodes[1].RecordKey = "root"
			},
			code: "duplicate_hierarchy_key",
		},
		{
			name: "unknown parent",
			mutate: func(dataset *format.Dataset) {
				dataset.Hierarchy.Nodes[1].ParentKey = "missing"
			},
			code: "unknown_parent_key",
		},
		{
			name: "negative position",
			mutate: func(dataset *format.Dataset) {
				dataset.Hierarchy.Nodes[1].Position = -1
			},
			code: "invalid_position",
		},
		{
			name: "duplicate sibling position",
			mutate: func(dataset *format.Dataset) {
				dataset.Hierarchy.Nodes[3].Position = 0
			},
			code: "duplicate_sibling_position",
		},
		{
			name: "non-contiguous sibling positions",
			mutate: func(dataset *format.Dataset) {
				dataset.Hierarchy.Nodes[3].Position = 2
			},
			code: "non_contiguous_sibling_positions",
		},
		{
			name: "cycle",
			mutate: func(dataset *format.Dataset) {
				dataset.Hierarchy.Nodes[0].ParentKey = "grandchild"
			},
			code: "hierarchy_cycle",
		},
		{
			name: "non-canonical hierarchy order",
			mutate: func(dataset *format.Dataset) {
				dataset.Hierarchy.Nodes[1], dataset.Hierarchy.Nodes[3] = dataset.Hierarchy.Nodes[3], dataset.Hierarchy.Nodes[1]
			},
			code: "non_canonical_hierarchy_order",
		},
		{
			name: "non-canonical record order",
			mutate: func(dataset *format.Dataset) {
				dataset.Records[1], dataset.Records[3] = dataset.Records[3], dataset.Records[1]
			},
			code: "non_canonical_record_order",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dataset := validDataset()
			test.mutate(dataset)
			err := dataset.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil")
			}
			if !hasDiagnosticCode(err, test.code) {
				t.Fatalf("Validate() error = %v, want diagnostic code %q", err, test.code)
			}
		})
	}
}

func TestDatasetValidateProfileProvenance(t *testing.T) {
	t.Parallel()

	digestA := strings.Repeat("a", 64)
	digestB := strings.Repeat("b", 64)
	tests := []struct {
		name       string
		provenance format.DatasetProvenance
		code       string
	}{
		{
			name:       "legacy profile name only",
			provenance: format.DatasetProvenance{Format: "test", Profile: "legacy"},
		},
		{
			name: "coherent profile",
			provenance: format.DatasetProvenance{
				Format: "test", Profile: "site", ProfileFingerprint: digestA, ModelFingerprint: digestB,
			},
		},
		{
			name: "fingerprints require profile name",
			provenance: format.DatasetProvenance{
				Format: "test", ProfileFingerprint: digestA, ModelFingerprint: digestB,
			},
			code: "required_profile",
		},
		{
			name: "fingerprints are all or none",
			provenance: format.DatasetProvenance{
				Format: "test", Profile: "site", ProfileFingerprint: digestA,
			},
			code: "incomplete_profile_fingerprints",
		},
		{
			name: "profile digest is lowercase SHA-256",
			provenance: format.DatasetProvenance{
				Format: "test", Profile: "site", ProfileFingerprint: strings.ToUpper(digestA), ModelFingerprint: digestB,
			},
			code: "invalid_profile_fingerprint",
		},
		{
			name: "model digest is lowercase SHA-256",
			provenance: format.DatasetProvenance{
				Format: "test", Profile: "site", ProfileFingerprint: digestA, ModelFingerprint: "not-a-digest",
			},
			code: "invalid_model_fingerprint",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dataset := validDataset()
			dataset.Provenance = test.provenance
			err := dataset.Validate()
			if test.code == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !hasDiagnosticCode(err, test.code) {
				t.Fatalf("Validate() error = %v, want diagnostic %q", err, test.code)
			}
		})
	}
}

func TestDatasetValidateCycleDiagnosticIsDeterministic(t *testing.T) {
	t.Parallel()

	dataset := validDataset()
	dataset.Hierarchy.Nodes[0].ParentKey = "grandchild"
	err := dataset.Validate()
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) {
		t.Fatalf("Validate() error = %T %v, want DiagnosticsError", err, err)
	}
	for _, diagnostic := range diagnostics.Diagnostics {
		if diagnostic.Code == "hierarchy_cycle" {
			if diagnostic.Message != "hierarchy contains a cycle: child-a -> root -> grandchild -> child-a" {
				t.Fatalf("cycle message = %q", diagnostic.Message)
			}
			return
		}
	}
	t.Fatalf("missing hierarchy_cycle diagnostic: %+v", diagnostics.Diagnostics)
}

func validDataset() *format.Dataset {
	keys := []string{"root", "child-a", "grandchild", "child-b", "other-root"}
	records := make([]format.DatasetRecord, len(keys))
	for index, key := range keys {
		records[index] = format.DatasetRecord{Key: key, Record: &hubv1.Record{Title: key}}
	}
	return &format.Dataset{
		Records: records,
		Hierarchy: format.Hierarchy{Nodes: []format.HierarchyNode{
			{RecordKey: "root", Position: 0},
			{RecordKey: "child-a", ParentKey: "root", Position: 0},
			{RecordKey: "grandchild", ParentKey: "child-a", Position: 0},
			{RecordKey: "child-b", ParentKey: "root", Position: 1},
			{RecordKey: "other-root", Position: 1},
		}},
		Provenance: format.DatasetProvenance{Format: "test"},
	}
}

func compiledDatasetTestProfile(t *testing.T, system, name string) *profile.Compiled {
	t.Helper()
	snapshot := &model.Snapshot{
		Version: model.CurrentVersion,
		System:  system,
		Entities: []model.Entity{{
			EntityType: "record",
			Fields: []model.Field{{
				Path: "title", SourceType: "string", Kind: model.ValueText, Cardinality: 1,
			}},
		}},
	}
	if err := snapshot.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	definition := &profile.Definition{
		Version:          profile.CurrentDefinitionVersion,
		Name:             name,
		System:           system,
		ModelFingerprint: snapshot.Fingerprint.Value,
		Mappings: []profile.Mapping{{
			Field:  profile.FieldSelector{EntityType: "record", Path: "title"},
			Hub:    "Title",
			Decode: "text",
			Encode: "none",
			Merge:  profile.MergeFirstNonempty,
		}},
	}
	if err := definition.SealFingerprint(); err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.Compile(snapshot, definition)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func hasDiagnosticCode(err error, code string) bool {
	var diagnostics *format.DiagnosticsError
	if !errors.As(err, &diagnostics) {
		return false
	}
	for _, diagnostic := range diagnostics.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestDatasetRejectsExcessiveRecordCountBeforeTopologyAllocation(t *testing.T) {
	dataset := &format.Dataset{
		Records:    make([]format.DatasetRecord, 100_001),
		Provenance: format.DatasetProvenance{Format: "test"},
	}
	err := dataset.Validate()
	if !hasDiagnosticCode(err, "record_limit") {
		t.Fatalf("Validate() error = %v, want record_limit", err)
	}
}
