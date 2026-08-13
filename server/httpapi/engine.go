package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	csvformat "github.com/lehigh-university-libraries/crosswalk/format/csv"
	workbenchformat "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/reconcile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

const maxEngineRecords = 100_000

// CrosswalkEngine implements the HTTP engine with a validated, versioned
// spreadsheet-to-Workbench specification. It performs no network or filesystem
// access and supplies no live taxonomy resolver.
type CrosswalkEngine struct {
	transformation *spec.Transformation
	finder         reconcile.Finder
	policy         reconcile.Policy
	provenance     ReconciliationProvenance
	targetProfile  *profile.Compiled
}

// ReconciliationProvenance identifies the immutable system profile and
// identifier registry used for duplicate detection.
type ReconciliationProvenance = reconcile.ReportProvenance

// ReconciliationConfig binds candidate retrieval to its exact matching policy
// and immutable profile provenance. Finder may be nil when callers only need
// profile-aware duplicate detection within an assume-new batch.
type ReconciliationConfig struct {
	Finder        reconcile.Finder
	Policy        reconcile.Policy
	Provenance    ReconciliationProvenance
	TargetProfile *profile.Compiled
}

var _ Engine = (*CrosswalkEngine)(nil)
var _ MatchEngine = (*CrosswalkEngine)(nil)

// NewCrosswalkEngine creates the default side-effect-free HTTP engine.
func NewCrosswalkEngine() *CrosswalkEngine {
	return &CrosswalkEngine{transformation: spec.FabricatorWorkbench(), policy: reconcile.PolicyV1()}
}

// WithFinder returns an engine that can perform read-only duplicate matching.
// The caller owns the Finder and must make it safe for concurrent requests.
func (e *CrosswalkEngine) WithFinder(finder reconcile.Finder) *CrosswalkEngine {
	if e != nil {
		e.finder = finder
	}
	return e
}

// ConfigureReconciliation validates and defensively copies matching policy
// before the engine begins serving concurrent requests.
func (e *CrosswalkEngine) ConfigureReconciliation(config ReconciliationConfig) error {
	if e == nil {
		return errors.New("crosswalk engine is nil")
	}
	if err := config.Policy.Validate(); err != nil {
		return fmt.Errorf("invalid reconciliation policy: %w", err)
	}
	policy, err := reconcile.NewPolicy(config.Policy.IdentifierRegistry)
	if err != nil {
		return fmt.Errorf("copying reconciliation policy: %w", err)
	}
	if config.Policy.Version != "" && config.Policy.Version != policy.Version {
		return fmt.Errorf("unsupported reconciliation policy version %q", config.Policy.Version)
	}
	registry, err := hub.NewIdentifierRegistry(policy.IdentifierRegistry)
	if err != nil {
		return fmt.Errorf("building reconciliation registry: %w", err)
	}
	provenance := config.Provenance
	if provenance.IdentifierRegistryVersion != "" && provenance.IdentifierRegistryVersion != registry.Version() {
		return fmt.Errorf("configured identifier registry version %q does not match policy version %q", provenance.IdentifierRegistryVersion, registry.Version())
	}
	if provenance.IdentifierRegistryDigest != "" && provenance.IdentifierRegistryDigest != registry.Digest() {
		return errors.New("configured identifier registry digest does not match policy")
	}
	provenance.IdentifierRegistryVersion = registry.Version()
	provenance.IdentifierRegistryDigest = registry.Digest()
	if err := provenance.Validate(); err != nil {
		return fmt.Errorf("invalid reconciliation provenance: %w", err)
	}
	if config.TargetProfile != nil {
		if strings.TrimSpace(e.transformation.Fingerprint.Profile) == "" {
			return errors.New("unbound transformation cannot use a target profile")
		}
		if config.TargetProfile.Name() != provenance.ProfileName {
			return errors.New("target profile name does not match reconciliation provenance")
		}
		if config.TargetProfile.System() != provenance.System {
			return errors.New("target profile system does not match reconciliation provenance")
		}
		if config.TargetProfile.Fingerprint() != provenance.ProfileFingerprint {
			return errors.New("target profile fingerprint does not match reconciliation provenance")
		}
		if config.TargetProfile.ModelFingerprint() != provenance.ModelFingerprint {
			return errors.New("target model fingerprint does not match reconciliation provenance")
		}
		if modelFingerprint := strings.TrimSpace(e.transformation.Fingerprint.Model); modelFingerprint != config.TargetProfile.ModelFingerprint() {
			return errors.New("transformation model fingerprint does not match target profile model")
		}
		if profileFingerprint := strings.TrimSpace(e.transformation.Fingerprint.Profile); profileFingerprint != config.TargetProfile.Fingerprint() {
			return errors.New("transformation profile fingerprint does not match target profile")
		}
	} else if strings.TrimSpace(e.transformation.Fingerprint.Profile) != "" {
		return errors.New("profile-bound transformation requires the exact target profile")
	}
	e.finder = config.Finder
	e.policy = policy
	e.provenance = provenance
	e.targetProfile = config.TargetProfile
	return nil
}

// NewCrosswalkEngineWithSpec creates a side-effect-free HTTP engine using a
// validated transformation specification. The policy is copied so callers
// cannot race requests by mutating it after construction.
func NewCrosswalkEngineWithSpec(transformation *spec.Transformation) (*CrosswalkEngine, error) {
	if transformation == nil {
		return nil, errors.New("transformation specification is required")
	}
	if err := transformation.Validate(); err != nil {
		return nil, fmt.Errorf("invalid transformation specification: %w", err)
	}
	if err := transformation.ValidateSealed(); err != nil {
		return nil, fmt.Errorf("unsealed transformation specification: %w", err)
	}
	if transformation.Source.Format != "csv" {
		return nil, fmt.Errorf("transformation source format %q is not CSV", transformation.Source.Format)
	}
	if transformation.Target.Format != "islandora-workbench" {
		return nil, fmt.Errorf("transformation target format %q is not Islandora Workbench", transformation.Target.Format)
	}
	data, err := json.Marshal(transformation)
	if err != nil {
		return nil, fmt.Errorf("copying transformation specification: %w", err)
	}
	owned, err := spec.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("copying transformation specification: %w", err)
	}
	return &CrosswalkEngine{transformation: owned, policy: reconcile.PolicyV1()}, nil
}

// Check parses and validates spreadsheet rows. Expected data-quality failures
// are returned in the Fabricator-compatible cell-error map, not as HTTP errors.
func (e *CrosswalkEngine) Check(ctx context.Context, rows [][]string) (CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return CheckResult{"A1": "No rows in CSV to process"}, nil
	}
	if len(rows) > maxEngineRecords+2 {
		return nil, errors.New("metadata input exceeds maximum record count")
	}
	result := validateWorkbenchRows(rows, e.transformation)

	var input bytes.Buffer
	writer := csv.NewWriter(&input)
	if err := writer.WriteAll(rows); err != nil {
		return nil, fmt.Errorf("encoding spreadsheet rows: %w", err)
	}

	records, err := e.parse(&input)
	if err != nil {
		var diagnostics *format.DiagnosticsError
		if errors.As(err, &diagnostics) {
			mergeCheckResults(result, diagnosticsResult(diagnostics.Diagnostics, rows, e.transformation))
			return result, nil
		}
		return nil, err
	}
	if len(records) == 0 {
		headerRows, _ := sourceCoordinates(rows, e.transformation)
		appendCheckMessage(result, fmt.Sprintf("A%d", headerRows+1), "No rows in CSV to process")
		return result, nil
	}

	headerRows, columns := sourceCoordinates(rows, e.transformation)
	dataRows := sourceDataRows(rows, headerRows)
	for recordIndex, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		validationOptions := hub.DefaultValidationOptions()
		// Workbench updates and add-media rows do not require a title. The
		// transformation specification enforces title on creates.
		validationOptions.RequireTitle = hub.GetExtraString(record, "node_id") == ""
		validation := hub.Validate(record, validationOptions)
		for _, issue := range validation.Errors {
			row := headerRows + recordIndex + 1
			if recordIndex < len(dataRows) {
				row = dataRows[recordIndex]
			}
			key := validationCoordinate(recordIndex, row, columns, issue.Field)
			appendCheckMessage(result, key, issue.Message)
		}
	}
	return result, nil
}

func mergeCheckResults(target, source CheckResult) {
	for key, message := range source {
		appendCheckMessage(target, key, message)
	}
}

// Transform parses spreadsheet CSV and produces the complete deterministic
// Workbench artifact bundle entirely in memory.
func (e *CrosswalkEngine) Transform(ctx context.Context, input io.Reader) ([]Artifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := e.parse(input)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.New("no metadata records to transform")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	plan, err := workbenchformat.PlanArtifacts(records, &format.SerializeOptions{
		Spec:                e.transformation,
		SystemProfile:       e.targetProfile,
		MultiValueSeparator: e.transformation.Target.MultiValueSeparator,
		IncludeHeader:       true,
	})
	if err != nil {
		return nil, fmt.Errorf("planning Workbench artifacts: %w", err)
	}
	artifacts := make([]Artifact, 0, len(plan.Artifacts))
	for _, planned := range plan.Artifacts {
		artifacts = append(artifacts, Artifact{
			Name:      planned.Name,
			MediaType: planned.MediaType,
			Data:      append([]byte(nil), planned.Data...),
		})
	}
	return artifacts, nil
}

// Matches parses spreadsheet CSV, queries the configured repository, and
// returns deterministic JSON/CSV review artifacts without mutating the site.
func (e *CrosswalkEngine) Matches(ctx context.Context, input io.Reader, modeValue string) (MatchResponse, error) {
	mode := reconcile.Mode(strings.TrimSpace(modeValue))
	if mode == "" {
		mode = reconcile.ModeHold
	}
	if err := reconcile.ValidateMode(mode); err != nil {
		return MatchResponse{}, err
	}
	if e.finder == nil && mode != reconcile.ModeAssumeNew {
		return MatchResponse{}, errors.New("repository lookup is not configured; use assume-new only for inputs known to be new")
	}
	records, err := e.parse(input)
	if err != nil {
		return MatchResponse{}, err
	}
	if len(records) == 0 {
		return MatchResponse{}, errors.New("no metadata records to reconcile")
	}
	inputs := make([]reconcile.Input, 0, len(records))
	for index, record := range records {
		key := hub.GetExtraString(record, "id")
		if key == "" {
			key = fmt.Sprintf("record-%d", index+1)
		}
		inputs = append(inputs, reconcile.Input{Key: key, Record: record})
	}
	report, err := (reconcile.Detector{Finder: e.finder, Policy: e.policy, Provenance: e.provenance}).Detect(ctx, inputs, mode)
	if err != nil {
		return MatchResponse{}, err
	}
	review, err := reconcile.ReviewCSV(report)
	if err != nil {
		return MatchResponse{}, err
	}
	return MatchResponse{Report: report, ReviewCSV: review}, nil
}

func (e *CrosswalkEngine) parse(input io.Reader) ([]*hubv1.Record, error) {
	records, err := (&csvformat.Format{}).Parse(input, &format.ParseOptions{
		Spec:         e.transformation,
		ValueProfile: e.targetProfile,
		Strict:       true,
		StripHTML:    true,
		SourceName:   "request.csv",
	})
	if err != nil {
		return nil, err
	}
	if len(records) > maxEngineRecords {
		return nil, errors.New("metadata input exceeds maximum record count")
	}
	return records, nil
}

func diagnosticsResult(diagnostics []format.Diagnostic, rows [][]string, transformation *spec.Transformation) CheckResult {
	result := make(CheckResult, len(diagnostics))
	_, columns := sourceCoordinates(rows, transformation)
	for _, diagnostic := range diagnostics {
		key := "request"
		if diagnostic.Row > 0 {
			key = fmt.Sprintf("row%d", diagnostic.Row)
		}
		column := diagnostic.Column
		if column == 0 && diagnostic.Header != "" && transformation != nil {
			if field, ok := transformation.SourceField(diagnostic.Header); ok {
				column = columns[hubFieldBase(field.Hub)]
			}
		}
		if column > 0 {
			key = excelColumn(column) + fmt.Sprintf("%d", diagnostic.Row)
		}
		appendCheckMessage(result, key, diagnostic.Message)
	}
	return result
}

func sourceCoordinates(rows [][]string, transformation *spec.Transformation) (int, map[string]int) {
	if len(rows) == 0 || transformation == nil {
		return 0, nil
	}

	headerRows := 1
	candidates := 1
	if len(rows) > 1 && transformation.Source.HeaderRows > 1 {
		firstMatches := matchingHeaders(rows[0], transformation)
		if firstMatches > 0 && isLikelyAdditionalHeader(rows[1], transformation) {
			headerRows = 2
			candidates = 2
		}
	}

	columns := make(map[string]int)
	for row := 0; row < candidates; row++ {
		for column, header := range rows[row] {
			field, ok := transformation.SourceField(header)
			if !ok || field.Hub == "" {
				continue
			}
			base := hubFieldBase(field.Hub)
			if _, exists := columns[base]; !exists {
				columns[base] = column + 1
			}
		}
	}
	return headerRows, columns
}

func matchingHeaders(row []string, transformation *spec.Transformation) int {
	matches := 0
	for _, header := range row {
		if _, ok := transformation.SourceField(header); ok {
			matches++
		}
	}
	return matches
}

func isLikelyAdditionalHeader(row []string, transformation *spec.Transformation) bool {
	nonempty := 0
	matches := 0
	for _, value := range row {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		nonempty++
		if strings.EqualFold(value, "machine name") || strings.EqualFold(value, "human name") {
			matches++
			continue
		}
		if _, ok := transformation.SourceField(value); ok {
			matches++
		}
	}
	return nonempty > 0 && matches*2 >= nonempty
}

func validationCoordinate(recordIndex, row int, columns map[string]int, field string) string {
	if column := columns[hubFieldBase(field)]; column > 0 {
		return excelColumn(column) + fmt.Sprintf("%d", row)
	}
	return fmt.Sprintf("record[%d].%s", recordIndex+1, field)
}

func sourceDataRows(rows [][]string, headerRows int) []int {
	result := make([]int, 0, len(rows)-headerRows)
	for index := headerRows; index < len(rows); index++ {
		nonempty := false
		for _, value := range rows[index] {
			if strings.TrimSpace(value) != "" {
				nonempty = true
				break
			}
		}
		if nonempty {
			result = append(result, index+1)
		}
	}
	return result
}

func hubFieldBase(field string) string {
	field = strings.TrimSpace(field)
	if index := strings.IndexAny(field, ".["); index >= 0 {
		field = field[:index]
	}
	return strings.ToLower(field)
}

func appendCheckMessage(result CheckResult, key, message string) {
	if existing := result[key]; existing != "" && !strings.Contains(existing, message) {
		result[key] = existing + "; " + message
		return
	}
	result[key] = message
}

func excelColumn(column int) string {
	if column < 1 {
		return ""
	}
	var label [16]byte
	position := len(label)
	for column > 0 {
		column--
		position--
		label[position] = byte('A' + column%26)
		column /= 26
	}
	return string(label[position:])
}
