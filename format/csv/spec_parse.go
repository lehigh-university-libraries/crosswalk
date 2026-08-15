package csv

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

type specColumn struct {
	index  int
	header string
	field  spec.Field
}

func parseRowsWithSpec(rows [][]string, opts *format.ParseOptions) ([]*hubv1.Record, error) {
	header := rows[0]
	columns, diagnostics := resolveSpecColumns(header, opts)
	dataStart := 1
	if opts.Spec.Source.HeaderRows > 1 && len(rows) > 1 && isAdditionalHeader(rows[1], opts.Spec) {
		dataStart = 2
		diagnostics = append(diagnostics, validateHeaderAlignment(header, rows[1], opts)...)
	}
	if len(diagnostics) > 0 {
		return nil, &format.DiagnosticsError{Diagnostics: diagnostics}
	}
	diagnostics = append(diagnostics, validateSpecTableRows(rows, dataStart, columns, opts)...)

	records := make([]*hubv1.Record, 0, len(rows)-dataStart)
	for rowIndex := dataStart; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		if emptyRow(row) {
			continue
		}
		if len(row) != len(header) {
			diagnostics = append(diagnostics, format.Diagnostic{
				Source:  opts.SourceName,
				Row:     rowIndex + 1,
				Code:    "column_count",
				Message: fmt.Sprintf("got %d columns; header defines %d", len(row), len(header)),
			})
			continue
		}

		record, rowDiagnostics := specRowToRecord(row, rowIndex+1, columns, opts)
		if len(rowDiagnostics) > 0 {
			diagnostics = append(diagnostics, rowDiagnostics...)
			continue
		}
		records = append(records, record)
	}
	if len(diagnostics) > 0 {
		return nil, &format.DiagnosticsError{Diagnostics: diagnostics}
	}
	return records, nil
}

func validateSpecParseOptions(opts *format.ParseOptions) error {
	if opts == nil || opts.Spec == nil {
		return fmt.Errorf("CSV specification is required")
	}
	if err := opts.Spec.Validate(); err != nil {
		return fmt.Errorf("invalid transformation specification: %w", err)
	}
	if err := opts.Spec.ValidateSealed(); err != nil {
		return fmt.Errorf("unsealed transformation specification: %w", err)
	}
	if opts.Spec.Source.Format != "csv" {
		return fmt.Errorf("transformation source format %q is not CSV", opts.Spec.Source.Format)
	}
	if err := validateSpecValueProfileBinding(opts); err != nil {
		return err
	}
	return nil
}

func validateSpecValueProfileBinding(opts *format.ParseOptions) error {
	profileFingerprint := strings.TrimSpace(opts.Spec.Fingerprint.Profile)
	if profileFingerprint == "" {
		if opts.ValueProfile != nil {
			return fmt.Errorf("unbound transformation cannot use a value profile")
		}
		return nil
	}
	if opts.ValueProfile == nil {
		return fmt.Errorf("profile-bound transformation requires the exact value profile")
	}

	expectedSystem := strings.TrimSpace(opts.Spec.Target.Format)
	if expectedSystem == "islandora-workbench" {
		expectedSystem = "drupal"
	}
	if opts.ValueProfile.System() != expectedSystem {
		return fmt.Errorf(
			"value profile system %q does not match transformation target system %q",
			opts.ValueProfile.System(), expectedSystem,
		)
	}
	if opts.ValueProfile.Fingerprint() != profileFingerprint {
		return fmt.Errorf("transformation profile fingerprint does not match value profile")
	}
	if opts.ValueProfile.ModelFingerprint() != strings.TrimSpace(opts.Spec.Fingerprint.Model) {
		return fmt.Errorf("transformation model fingerprint does not match value profile model")
	}
	return nil
}

func validateHeaderAlignment(first, second []string, opts *format.ParseOptions) []format.Diagnostic {
	if len(first) != len(second) {
		return []format.Diagnostic{{
			Source:  opts.SourceName,
			Row:     2,
			Code:    "header_column_count",
			Message: fmt.Sprintf("got %d columns; first header row defines %d", len(second), len(first)),
		}}
	}
	diagnostics := make([]format.Diagnostic, 0)
	for index := range first {
		if isHeaderSentinel(first[index]) && isHeaderSentinel(second[index]) {
			continue
		}
		firstField, firstOK := opts.Spec.SourceField(first[index])
		secondField, secondOK := opts.Spec.SourceField(second[index])
		if firstOK && secondOK && firstField.Name == secondField.Name {
			continue
		}
		diagnostics = append(diagnostics, format.Diagnostic{
			Source:  opts.SourceName,
			Row:     2,
			Column:  index + 1,
			Header:  strings.TrimSpace(second[index]),
			Code:    "header_mismatch",
			Message: fmt.Sprintf("does not describe the same field as first-row header %q", strings.TrimSpace(first[index])),
		})
	}
	return diagnostics
}

func resolveSpecColumns(header []string, opts *format.ParseOptions) ([]specColumn, []format.Diagnostic) {
	columns := make([]specColumn, 0, len(header))
	diagnostics := make([]format.Diagnostic, 0)
	seen := make(map[string]int)
	for index, name := range header {
		name = strings.TrimSpace(name)
		if name == "" || isHeaderSentinel(name) {
			continue
		}
		field, ok := opts.Spec.SourceField(name)
		if !ok {
			diagnostics = append(diagnostics, format.Diagnostic{
				Source:  opts.SourceName,
				Row:     1,
				Column:  index + 1,
				Header:  name,
				Code:    "unknown_column",
				Message: "column is not declared by the source specification",
			})
			continue
		}
		key := strings.ToLower(field.Name)
		if previous, exists := seen[key]; exists {
			diagnostics = append(diagnostics, format.Diagnostic{
				Source:  opts.SourceName,
				Row:     1,
				Column:  index + 1,
				Header:  name,
				Code:    "duplicate_column",
				Message: fmt.Sprintf("duplicates column %d mapped to %q", previous+1, field.Name),
			})
			continue
		}
		seen[key] = index
		columns = append(columns, specColumn{index: index, header: name, field: field})
	}
	return columns, diagnostics
}

func isAdditionalHeader(row []string, transformation *spec.Transformation) bool {
	matched := 0
	nonempty := 0
	for _, value := range row {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		nonempty++
		if isHeaderSentinel(value) {
			matched++
			continue
		}
		if _, ok := transformation.SourceField(value); ok {
			matched++
		}
	}
	return nonempty > 0 && matched*2 >= nonempty
}

func isHeaderSentinel(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "machine name", "human name":
		return true
	default:
		return false
	}
}

func emptyRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

type contributorParts struct {
	values        map[string][]string
	bundleAliases map[string]string
}

func specRowToRecord(row []string, rowNumber int, columns []specColumn, opts *format.ParseOptions) (*hubv1.Record, []format.Diagnostic) {
	record := &hubv1.Record{}
	diagnostics := make([]format.Diagnostic, 0)
	contributors := contributorParts{values: make(map[string][]string)}
	sourceColumns := make([]string, 0, len(columns))
	present := make(map[string]bool, len(columns))
	declared := make(map[string]specColumn, len(columns))
	for _, column := range columns {
		if column.field.Codec == "ignore" {
			continue
		}
		declared[strings.ToLower(column.field.Name)] = column
	}

	for _, column := range columns {
		if column.field.Codec == "ignore" {
			continue
		}
		raw := strings.TrimSpace(row[column.index])
		if raw == "" {
			raw = column.field.Default
		}
		if raw == "" {
			continue
		}

		values := specValues(raw, column.field, opts.Spec.Source.MultiValueSeparator)
		encodedValues := specCardinalityValues(raw, column.field, opts.Spec.Source.MultiValueSeparator)
		if column.field.Cardinality > 0 && len(encodedValues) > column.field.Cardinality {
			diagnostics = append(diagnostics, cellDiagnostic(opts, rowNumber, column, "cardinality",
				fmt.Sprintf("got %d values; maximum is %d", len(encodedValues), column.field.Cardinality)))
			continue
		}
		sourceColumns = append(sourceColumns, column.field.Name)
		present[strings.ToLower(column.field.Name)] = true

		if strings.HasPrefix(column.field.Hub, "Contributors.") {
			component := strings.TrimPrefix(column.field.Hub, "Contributors.")
			contributors.values[component] = append(contributors.values[component], values...)
			if component == "Type" {
				contributors.bundleAliases = contributorBundleAliases(column.field)
			}
			continue
		}
		for _, value := range values {
			if err := assignSpecValue(record, column.field, value, opts); err != nil {
				code := "invalid_value"
				if column.field.Codec == "edtf" {
					code = "invalid_date"
				}
				diagnostics = append(diagnostics, cellDiagnostic(opts, rowNumber, column, code, err.Error()))
				break
			}
		}
	}

	if err := contributors.apply(record); err != nil {
		diagnostics = append(diagnostics, format.Diagnostic{
			Source:  opts.SourceName,
			Row:     rowNumber,
			Code:    "invalid_contributor",
			Message: err.Error(),
		})
	}
	if len(sourceColumns) > 0 {
		hub.SetExtra(record, "_source_columns", strings.Join(sourceColumns, "|"))
	}
	// Determine the task from declared canonical fields, including cells whose
	// value failed conversion. That prevents one bad cell from cascading into
	// unrelated operation diagnostics.
	operation := newSpecRowView(row, columns, opts.Spec.Source.MultiValueSeparator).operation()
	for _, column := range columns {
		if column.field.Codec == "ignore" || strings.TrimSpace(row[column.index]) == "" || column.field.AppliesTo(operation) {
			continue
		}
		diagnostics = append(diagnostics, cellDiagnostic(opts, rowNumber, column, "operation",
			fmt.Sprintf("field does not apply to %s operations", operation)))
	}
	diagnostics = append(diagnostics, validateSpecRow(row, rowNumber, columns, record, operation, opts)...)
	for _, field := range opts.Spec.Source.Fields {
		if field.Codec == "ignore" {
			continue
		}
		key := strings.ToLower(field.Name)
		if field.IsRequiredFor(operation, record.ObjectModel) && !present[key] {
			message := fmt.Sprintf("%s is required for %s operations", specFieldDescription(opts.Spec, field.Name), operation)
			if column, ok := declared[key]; ok {
				diagnostics = append(diagnostics, cellDiagnostic(opts, rowNumber, column, "required", message))
			} else {
				diagnostics = append(diagnostics, format.Diagnostic{
					Source:  opts.SourceName,
					Row:     rowNumber,
					Header:  preferredFieldLabel(field),
					Code:    "required",
					Message: message,
				})
			}
		}
	}
	for _, group := range opts.Spec.Source.RequiredGroups {
		if !requiredGroupHasActiveField(group, opts.Spec) || !group.IsRequired(operation) || requiredGroupPresent(group, present) {
			continue
		}
		message := fmt.Sprintf("at least one value in required group %q is required for %s operations", group.Name, operation)
		if column, ok := declaredGroupColumn(group, declared); ok {
			diagnostics = append(diagnostics, cellDiagnostic(opts, rowNumber, column, "required_group", message))
			continue
		}
		header := group.Name
		if len(group.Fields) > 0 {
			header = group.Fields[0]
		}
		diagnostics = append(diagnostics, format.Diagnostic{
			Source:  opts.SourceName,
			Row:     rowNumber,
			Header:  header,
			Code:    "required_group",
			Message: message,
		})
	}
	return record, diagnostics
}

func requiredGroupHasActiveField(group spec.RequiredGroup, transformation *spec.Transformation) bool {
	for _, name := range group.Fields {
		field, ok := transformation.SourceField(name)
		if ok && field.Codec != "ignore" {
			return true
		}
	}
	return false
}

func requiredGroupPresent(group spec.RequiredGroup, present map[string]bool) bool {
	for _, field := range group.Fields {
		if present[strings.ToLower(field)] {
			return true
		}
	}
	return false
}

func declaredGroupColumn(group spec.RequiredGroup, declared map[string]specColumn) (specColumn, bool) {
	for _, field := range group.Fields {
		if column, ok := declared[strings.ToLower(field)]; ok {
			return column, true
		}
	}
	return specColumn{}, false
}

func inferSourceOperation(record *hubv1.Record, sourceColumns []string, transformation *spec.Transformation) spec.Operation {
	if hub.GetExtraString(record, "node_id") == "" {
		return spec.OperationCreate
	}
	for _, name := range sourceColumns {
		field, ok := transformation.SourceField(name)
		if !ok || !field.AppliesTo(spec.OperationUpdate) {
			continue
		}
		switch field.Hub {
		case "Extra.node_id", "Files.primary", "Extra.id", "Extra.parent_id":
			continue
		default:
			return spec.OperationUpdate
		}
	}
	for _, file := range record.Files {
		if file != nil && file.Path != "" && (file.Role == "primary" || file.Role == "") {
			return spec.OperationAddMedia
		}
	}
	return spec.OperationUpdate
}

func preferredFieldLabel(field spec.Field) string {
	if field.Label != "" {
		return field.Label
	}
	return field.Name
}

func cellDiagnostic(opts *format.ParseOptions, row int, column specColumn, code, message string) format.Diagnostic {
	return format.Diagnostic{
		Source:  opts.SourceName,
		Row:     row,
		Column:  column.index + 1,
		Header:  column.header,
		Code:    code,
		Message: message,
	}
}

func specValues(raw string, field spec.Field, separator string) []string {
	if field.Cardinality != 1 {
		switch field.Codec {
		case "multi", "file", "contributors", "boolean", "integer", "unsigned", "edtf", "string", "profile_identifier", "":
			if separator == "" {
				separator = "|"
			}
			return splitMultiValue(raw, separator)
		}
	}
	switch field.Codec {
	case "multi", "file", "contributors":
		if separator == "" {
			separator = "|"
		}
		return splitMultiValue(raw, separator)
	default:
		return []string{strings.TrimSpace(raw)}
	}
}

// specCardinalityValues interprets the declared source encoding independently
// of the Hub codec. A scalar Drupal codec can still contain multiple values in
// one source cell when the configured separator is present. Workbench treats
// its canonical title machine field as scalar text because titles may contain
// the separator; the exception is intentionally keyed to Field.Name, never a
// human-facing label or alias.
func specCardinalityValues(raw string, field spec.Field, separator string) []string {
	if strings.EqualFold(strings.TrimSpace(field.Name), "title") {
		return []string{strings.TrimSpace(raw)}
	}
	if separator == "" {
		separator = "|"
	}
	return splitMultiValue(raw, separator)
}

func assignSpecValue(record *hubv1.Record, field spec.Field, value string, opts *format.ParseOptions) error {
	switch field.Codec {
	case "boolean":
		parsed, err := parseBoolean(value)
		if err != nil {
			return err
		}
		if strings.HasPrefix(field.Hub, "Extra.") && field.Cardinality != 1 {
			return appendExtraValue(record, strings.TrimPrefix(field.Hub, "Extra."), parsed)
		}
		return assignBoolean(record, field.Hub, parsed)
	case "integer":
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return fmt.Errorf("must be an integer: %q", value)
		}
		if strings.HasPrefix(field.Hub, "Extra.") && field.Cardinality != 1 {
			return appendExtraValue(record, strings.TrimPrefix(field.Hub, "Extra."), parsed)
		}
		return assignInteger(record, field.Hub, parsed)
	case "unsigned":
		if _, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64); err != nil {
			return fmt.Errorf("must be an unsigned integer: %q", value)
		}
	case "edtf":
		dateType := dateTypeFromString(strings.TrimPrefix(field.Hub, "Dates."))
		date, err := helpers.ParseEDTF(value, dateType)
		if err != nil || date.Year < 1000 || date.Month > 12 || date.Day > 31 {
			return fmt.Errorf("must be a valid EDTF date: %q", value)
		}
		if strings.HasPrefix(field.Hub, "Extra.") {
			key := strings.TrimPrefix(field.Hub, "Extra.")
			if field.Cardinality != 1 {
				return appendExtraValue(record, key, value)
			}
			hub.SetExtra(record, key, value)
			return nil
		}
		record.Dates = append(record.Dates, date)
		return nil
	case "restriction":
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "yes", "true", "local restriction", "restricted":
			record.LocalRestriction = "1"
		default:
			record.LocalRestriction = "0"
		}
		return nil
	case "file":
		role := strings.TrimPrefix(field.Hub, "Files.")
		normalized, err := opts.Spec.NormalizeFilePath(value)
		if err != nil {
			return err
		}
		record.Files = append(record.Files, &hubv1.File{Path: normalized, Role: role})
		return nil
	case "profile_identifier":
		if opts.ValueProfile == nil {
			return fmt.Errorf("profile identifier column requires the bound target system profile")
		}
		if field.ProfileRule == "" {
			return fmt.Errorf("profile identifier column has no profile rule")
		}
		identifier, err := opts.ValueProfile.NewIdentifier(field.ProfileRule, value)
		if err != nil {
			return fmt.Errorf("profile identifier rule %q: %w", field.ProfileRule, err)
		}
		record.Identifiers = append(record.Identifiers, identifier)
		return nil
	}

	switch field.Hub {
	case "Title":
		record.Title = value
	case "FullTitle":
		record.FullTitle = value
	case "Abstract":
		record.Abstract = cleanValue(value, opts)
	case "Description":
		record.Description = cleanValue(value, opts)
	case "ObjectModel":
		record.ObjectModel = value
	case "ResourceType":
		record.ResourceType = hub.NewResourceType(value, "")
	case "Language":
		record.Language = value
	case "Publisher":
		record.Publisher = value
	case "Edition":
		record.Edition = value
	case "DigitalOrigin":
		record.DigitalOrigin = value
	case "Dimensions":
		record.Dimensions = value
	case "Duration":
		record.Duration = value
	case "PreferredCitation":
		record.PreferredCitation = value
	case "CaptureDevice":
		record.CaptureDevice = value
	case "LocalRestriction":
		record.LocalRestriction = value
	case "AccessCondition":
		record.AccessCondition = value
	case "Departments":
		record.Departments = append(record.Departments, value)
	case "Genre":
		record.Genres = append(record.Genres, &hubv1.Subject{Value: value, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT, Type: hubv1.SubjectType_SUBJECT_TYPE_GENRE})
	case "PhysicalForm":
		record.PhysicalForm = append(record.PhysicalForm, &hubv1.Subject{Value: value, Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT})
	case "Rights":
		record.Rights = append(record.Rights, rightsFromValue(value))
	case "Relations.member_of":
		record.Relations = append(record.Relations, &hubv1.Relation{Type: hubv1.RelationType_RELATION_TYPE_MEMBER_OF, TargetTitle: value})
	case "Subjects.lcsh":
		record.Subjects = append(record.Subjects, newSubject(value, hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH, hubv1.SubjectType_SUBJECT_TYPE_TOPIC))
	case "Subjects.keywords":
		record.Subjects = append(record.Subjects, newSubject(value, hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS, hubv1.SubjectType_SUBJECT_TYPE_TOPIC))
	case "Subjects.lcnaf":
		record.Subjects = append(record.Subjects, newSubject(value, hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCNAF, hubv1.SubjectType_SUBJECT_TYPE_NAME))
	case "Subjects.geographic_naf":
		record.Subjects = append(record.Subjects, newSubject(value, hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCNAF, hubv1.SubjectType_SUBJECT_TYPE_GEOGRAPHIC))
	case "Subjects.geographic_local":
		record.Subjects = append(record.Subjects, newSubject(value, hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL, hubv1.SubjectType_SUBJECT_TYPE_GEOGRAPHIC))
	case "Subjects.getty_tgn":
		subject := newSubject(value, hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GETTY_TGN, hubv1.SubjectType_SUBJECT_TYPE_GEOGRAPHIC)
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			subject.Uri = value
		}
		record.Subjects = append(record.Subjects, subject)
	case "Publication.Title", "Publication.Issn", "Publication.LIssn", "Publication.Volume", "Publication.Issue", "Publication.Pages":
		ensurePublication(record)
		switch field.Hub {
		case "Publication.Title":
			record.Publication.Title = value
		case "Publication.Issn":
			record.Publication.Issn = value
		case "Publication.LIssn":
			record.Publication.LIssn = value
		case "Publication.Volume":
			record.Publication.Volume = value
		case "Publication.Issue":
			record.Publication.Issue = value
		case "Publication.Pages":
			record.Publication.Pages = value
		}
	case "Identifiers.doi":
		record.Identifiers = append(record.Identifiers, hub.NewIdentifier(value, hubv1.IdentifierType_IDENTIFIER_TYPE_DOI))
	case "Identifiers.url":
		record.Identifiers = append(record.Identifiers, hub.NewIdentifier(value, hubv1.IdentifierType_IDENTIFIER_TYPE_URL))
	case "Identifiers.call_number":
		record.Identifiers = append(record.Identifiers, hub.NewIdentifier(value, hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER))
	case "Identifiers.report_number":
		record.Identifiers = append(record.Identifiers, hub.NewIdentifier(value, hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER))
	case "ArchivalLocation.Collection", "ArchivalLocation.Series", "ArchivalLocation.Box", "ArchivalLocation.Folder":
		if record.ArchivalLocation == nil {
			record.ArchivalLocation = &hubv1.ArchivalLocation{}
		}
		switch field.Hub {
		case "ArchivalLocation.Collection":
			record.ArchivalLocation.Collection = value
		case "ArchivalLocation.Series":
			record.ArchivalLocation.Series = value
		case "ArchivalLocation.Box":
			record.ArchivalLocation.Box = value
		case "ArchivalLocation.Folder":
			record.ArchivalLocation.Folder = value
		}
	default:
		if strings.HasPrefix(field.Hub, "Extra.") {
			key := strings.TrimPrefix(field.Hub, "Extra.")
			if field.Cardinality != 1 {
				return appendExtraValue(record, key, value)
			}
			hub.SetExtra(record, key, value)
			return nil
		}
		if strings.HasPrefix(field.Hub, "Files.") {
			return assignFileMetadata(record, strings.TrimPrefix(field.Hub, "Files."), value)
		}
		return fmt.Errorf("unsupported Hub mapping %q", field.Hub)
	}
	return nil
}

func appendExtraValue(record *hubv1.Record, key string, value any) error {
	existing, ok := hub.GetExtra(record, key)
	if !ok {
		hub.SetExtra(record, key, []any{value})
		return nil
	}
	values, ok := existing.([]any)
	if !ok {
		return fmt.Errorf("extra mapping %q already contains a scalar value", key)
	}
	hub.SetExtra(record, key, append(values, value))
	return nil
}

func parseBoolean(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "yes", "y", "true":
		return true, nil
	case "0", "no", "n", "false":
		return false, nil
	default:
		return false, fmt.Errorf("must be yes/no or 1/0: %q", value)
	}
}

func assignBoolean(record *hubv1.Record, path string, value bool) error {
	switch path {
	case "AddCoverpage":
		record.AddCoverpage = value
		hub.SetExtra(record, "_present_add_coverpage", true)
	case "IsPublic":
		record.IsPublic = value
		hub.SetExtra(record, "_present_is_public", true)
	default:
		if strings.HasPrefix(path, "Extra.") {
			hub.SetExtra(record, strings.TrimPrefix(path, "Extra."), value)
			return nil
		}
		return fmt.Errorf("unsupported boolean Hub mapping %q", path)
	}
	return nil
}

func assignInteger(record *hubv1.Record, path string, value int64) error {
	switch path {
	case "PageCount":
		if value < 0 || value > int64(^uint32(0)>>1) {
			return fmt.Errorf("page count is out of range: %d", value)
		}
		record.PageCount = int32(value)
	case "PPI":
		if value < 0 || value > int64(^uint32(0)>>1) {
			return fmt.Errorf("PPI is out of range: %d", value)
		}
		record.Ppi = int32(value)
	case "Files.size_bytes":
		file := primaryFile(record)
		file.SizeBytes = value
	default:
		if strings.HasPrefix(path, "Extra.") {
			hub.SetExtra(record, strings.TrimPrefix(path, "Extra."), value)
			return nil
		}
		return fmt.Errorf("unsupported integer Hub mapping %q", path)
	}
	return nil
}

func assignFileMetadata(record *hubv1.Record, name, value string) error {
	file := primaryFile(record)
	switch name {
	case "mime_type":
		file.MimeType = value
	case "size_bytes":
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("must be an integer byte size: %q", value)
		}
		file.SizeBytes = parsed
	default:
		return fmt.Errorf("unsupported file property %q", name)
	}
	return nil
}

func primaryFile(record *hubv1.Record) *hubv1.File {
	for _, file := range record.Files {
		if file.Role == "primary" || file.Role == "" {
			return file
		}
	}
	file := &hubv1.File{Role: "primary"}
	record.Files = append(record.Files, file)
	return file
}

func ensurePublication(record *hubv1.Record) {
	if record.Publication == nil {
		record.Publication = &hubv1.PublicationDetails{}
	}
}

func newSubject(value string, vocabulary hubv1.SubjectVocabulary, subjectType hubv1.SubjectType) *hubv1.Subject {
	return &hubv1.Subject{Value: value, Vocabulary: vocabulary, Type: subjectType}
}

func rightsFromValue(value string) *hubv1.Rights {
	for code, label := range hub.RightsStatementLabels {
		if strings.EqualFold(strings.TrimSpace(value), label) {
			return hub.NewRightsFromURI(hub.RightsStatements[code])
		}
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return hub.NewRightsFromURI(value)
	}
	return &hubv1.Rights{Statement: value}
}

func (parts contributorParts) apply(record *hubv1.Record) error {
	count := 0
	for _, values := range parts.values {
		if len(values) > count {
			count = len(values)
		}
	}
	for index := 0; index < count; index++ {
		name := contributorValue(parts.values["Name"], index)
		if name == "" {
			return fmt.Errorf("contributor %d has metadata but no name", index+1)
		}
		contributor := parseContributor(name)
		if contributor == nil {
			return fmt.Errorf("contributor %d has an empty name", index+1)
		}
		if role := contributorValue(parts.values["RoleCode"], index); role != "" {
			contributor.RoleCode = strings.TrimSpace(strings.SplitN(role, "|", 2)[0])
			contributor.Role = helpers.RelatorLabel(contributor.RoleCode)
		}
		if kind := contributorValue(parts.values["Type"], index); kind != "" {
			bundle, contributorType, err := contributorBundle(kind, parts.bundleAliases)
			if err != nil {
				return fmt.Errorf("contributor %d: %w", index+1, err)
			}
			contributor.Type = contributorType
			// Hub's ContributorType intentionally has only person/organization.
			// Preserve the exact Drupal vocabulary in SourceId so profile-bound
			// Workbench serialization can distinguish family from corporate_body
			// without guessing from a human-facing label.
			contributor.SourceId = bundle + ":" + contributor.Name
		}
		if orcid := contributorValue(parts.values["ORCID"], index); orcid != "" {
			contributor.Identifiers = append(contributor.Identifiers, hub.NewIdentifier(orcid, hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID))
		}
		// A combined Contributor JSON cell already carries these optional
		// properties. Separate template columns override them only when they
		// contain a value; an absent companion column must not erase the JSON
		// metadata produced by the ETD converter.
		if status := contributorValue(parts.values["Status"], index); status != "" {
			contributor.Status = status
		}
		if email := contributorValue(parts.values["Email"], index); email != "" {
			contributor.Email = email
		}
		if affiliation := contributorValue(parts.values["Affiliation"], index); affiliation != "" {
			affiliation = strings.TrimPrefix(affiliation, "schema:worksFor:corporate_body:")
			contributor.Affiliations = append(contributor.Affiliations, &hubv1.Affiliation{Name: affiliation})
		}
		record.Contributors = append(record.Contributors, contributor)
	}
	return nil
}

func contributorBundle(raw string, aliases map[string]string) (string, hubv1.ContributorType, error) {
	normalized := normalizeContributorBundle(raw)
	if normalized == "" {
		return "", hubv1.ContributorType_CONTRIBUTOR_TYPE_UNSPECIFIED, fmt.Errorf("contributor type is empty")
	}
	if len(aliases) == 0 {
		aliases = map[string]string{
			"person":         "person",
			"family":         "family",
			"corporate_body": "corporate_body",
			"organization":   "corporate_body",
		}
	}
	bundle, exists := aliases[normalized]
	if !exists && normalized == "organization" {
		bundle, exists = aliases["corporate_body"]
	}
	if !exists {
		return "", hubv1.ContributorType_CONTRIBUTOR_TYPE_UNSPECIFIED,
			fmt.Errorf("contributor type %q is not a model-declared Drupal bundle", raw)
	}
	contributorType := hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
	if bundle == "person" {
		contributorType = hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON
	}
	return bundle, contributorType, nil
}

func contributorBundleAliases(field spec.Field) map[string]string {
	for _, validation := range field.Validations {
		if validation.Rule != spec.ValidationTypedRelation || len(validation.Bundles) == 0 {
			continue
		}
		return contributorBundleNames(validation.Bundles)
	}
	handler, ok := field.InstanceSettings["handler_settings"].(map[string]any)
	if !ok {
		return nil
	}
	targets, ok := handler["target_bundles"]
	if !ok {
		return nil
	}
	result := make(map[string]string)
	add := func(bundle string) {
		bundle = strings.TrimSpace(bundle)
		if bundle == "" {
			return
		}
		result[normalizeContributorBundle(bundle)] = bundle
	}
	switch typed := targets.(type) {
	case map[string]any:
		for bundle := range typed {
			add(bundle)
		}
	case map[string]string:
		for bundle := range typed {
			add(bundle)
		}
	case []any:
		for _, bundle := range typed {
			add(fmt.Sprint(bundle))
		}
	case []string:
		for _, bundle := range typed {
			add(bundle)
		}
	case string:
		add(typed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func contributorBundleNames(bundles []string) map[string]string {
	result := make(map[string]string, len(bundles))
	for _, bundle := range bundles {
		bundle = strings.TrimSpace(bundle)
		if bundle != "" {
			result[normalizeContributorBundle(bundle)] = bundle
		}
	}
	return result
}

func normalizeContributorBundle(value string) string {
	return strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}

func contributorValue(values []string, index int) string {
	if index >= 0 && index < len(values) {
		return strings.TrimSpace(values[index])
	}
	return ""
}
