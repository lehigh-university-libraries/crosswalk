package islandora_workbench

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/lehigh-university-libraries/crosswalk/format"
	drupalformat "github.com/lehigh-university-libraries/crosswalk/format/drupal"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
	"google.golang.org/protobuf/proto"
)

const sep = "|"

var workbenchCreatedPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}$`)

// workbenchRow holds a serialized record's column values and any associated agent rows.
type workbenchRow struct {
	cols   map[string]string
	agents [][]string
}

// SerializeDataset writes a dataset as Workbench CSV while retaining an
// internal hierarchy through Workbench upload IDs, parent IDs, and weights.
// Flat datasets deliberately use Serialize unchanged so ordinary conversions
// retain their existing output contract.
func (f *Format) SerializeDataset(w io.Writer, dataset *format.Dataset, opts *format.SerializeOptions) error {
	if dataset == nil {
		return fmt.Errorf("dataset is required")
	}
	if err := dataset.Validate(); err != nil {
		return fmt.Errorf("invalid dataset: %w", err)
	}

	records := datasetRecords(dataset)
	if !datasetHasRelationships(dataset) {
		return f.Serialize(w, records, opts)
	}
	if err := validateHierarchySerializationOptions(opts); err != nil {
		return err
	}

	prepared, err := prepareHierarchyRecords(dataset, targetMultiValueSeparator(opts))
	if err != nil {
		return err
	}
	return f.Serialize(w, prepared, opts)
}

func validateHierarchySerializationOptions(opts *format.SerializeOptions) error {
	if opts == nil {
		return nil
	}
	required := []struct {
		hubPath string
		column  string
	}{
		{hubPath: "Extra.id", column: "id"},
		{hubPath: "Extra.parent_id", column: "parent_id"},
		{hubPath: "Extra.field_weight", column: "field_weight"},
	}
	selected := make(map[string]struct{}, len(opts.Columns))
	for _, column := range opts.Columns {
		selected[column] = struct{}{}
	}
	if opts.Spec == nil {
		if len(selected) == 0 {
			return nil
		}
		for _, field := range required {
			if _, exists := selected[field.column]; !exists {
				return fmt.Errorf("hierarchical Workbench serialization requires column %q", field.column)
			}
		}
		return nil
	}

	for _, requiredField := range required {
		found := false
		for _, field := range opts.Spec.Target.Fields {
			if field.Hub != requiredField.hubPath || field.Codec == "ignore" || !field.AppliesTo(opts.Operation) {
				continue
			}
			if len(selected) > 0 {
				if _, exists := selected[field.Name]; !exists {
					continue
				}
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("hierarchical Workbench serialization requires an active target field for %q", requiredField.hubPath)
		}
	}
	return nil
}

func datasetRecords(dataset *format.Dataset) []*hubv1.Record {
	records := make([]*hubv1.Record, len(dataset.Records))
	for index, entry := range dataset.Records {
		records[index] = entry.Record
	}
	return records
}

func datasetHasRelationships(dataset *format.Dataset) bool {
	for _, node := range dataset.Hierarchy.Nodes {
		if node.ParentKey != "" {
			return true
		}
	}
	return false
}

func prepareHierarchyRecords(dataset *format.Dataset, delimiter string) ([]*hubv1.Record, error) {
	reserved := make(map[string]string, len(dataset.Records))
	idByKey := make(map[string]string, len(dataset.Records))
	for _, entry := range dataset.Records {
		id, present, err := operationalExtra(entry.Record, "id")
		if err != nil {
			return nil, fmt.Errorf("record %q upload ID: %w", entry.Key, err)
		}
		if !present || id == "" {
			continue
		}
		if err := validateUploadID(id, delimiter); err != nil {
			return nil, fmt.Errorf("record %q upload ID %q: %w", entry.Key, id, err)
		}
		if previousKey, conflict := reserved[id]; conflict {
			return nil, fmt.Errorf("records %q and %q use the same upload ID %q", previousKey, entry.Key, id)
		}
		reserved[id] = entry.Key
		idByKey[entry.Key] = id
	}

	nextID := 1
	for _, entry := range dataset.Records {
		if idByKey[entry.Key] != "" {
			continue
		}
		for {
			candidate := strconv.Itoa(nextID)
			nextID++
			if _, exists := reserved[candidate]; exists {
				continue
			}
			reserved[candidate] = entry.Key
			idByKey[entry.Key] = candidate
			break
		}
	}

	nodeByKey := make(map[string]format.HierarchyNode, len(dataset.Hierarchy.Nodes))
	for _, node := range dataset.Hierarchy.Nodes {
		nodeByKey[node.RecordKey] = node
	}

	prepared := make([]*hubv1.Record, len(dataset.Records))
	for index, entry := range dataset.Records {
		node := nodeByKey[entry.Key]
		parentID := ""
		if node.ParentKey != "" {
			parentID = idByKey[node.ParentKey]
		}
		if err := verifyHierarchyExtra(entry.Record, entry.Key, "parent_id", parentID); err != nil {
			return nil, err
		}
		weight := strconv.Itoa(node.Position)
		if err := verifyHierarchyWeight(entry.Record, entry.Key, node.Position); err != nil {
			return nil, err
		}

		clone, ok := proto.Clone(entry.Record).(*hubv1.Record)
		if !ok {
			return nil, fmt.Errorf("record %q: copying Hub record", entry.Key)
		}
		hub.SetExtra(clone, "id", idByKey[entry.Key])
		if parentID != "" {
			hub.SetExtra(clone, "parent_id", parentID)
		}
		hub.SetExtra(clone, "field_weight", weight)
		prepared[index] = clone
	}
	return prepared, nil
}

func operationalExtra(record *hubv1.Record, key string) (string, bool, error) {
	value, present := hub.GetExtra(record, key)
	if !present {
		return "", false, nil
	}
	switch typed := value.(type) {
	case string:
		return typed, true, nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed != math.Trunc(typed) {
			return "", true, fmt.Errorf("must be a string or integer")
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), true, nil
	default:
		return "", true, fmt.Errorf("must be a string or integer, got %T", value)
	}
}

func validateUploadID(id, delimiter string) error {
	if id == "" {
		return fmt.Errorf("must not be empty")
	}
	if id != strings.TrimSpace(id) {
		return fmt.Errorf("must not contain surrounding whitespace")
	}
	if strings.IndexFunc(id, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character)
	}) >= 0 {
		return fmt.Errorf("must not contain whitespace or control characters")
	}
	for index, character := range id {
		if asciiAlphaNumeric(character) || (index > 0 && strings.ContainsRune("-_.:", character)) {
			continue
		}
		return fmt.Errorf("must start with an ASCII letter or digit and contain only ASCII letters, digits, hyphens, underscores, periods, or colons")
	}
	if delimiter != "" && strings.Contains(id, delimiter) {
		return fmt.Errorf("must not contain the multi-value separator %q", delimiter)
	}
	return nil
}

func asciiAlphaNumeric(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9'
}

func verifyHierarchyExtra(record *hubv1.Record, key, field, expected string) error {
	actual, present, err := operationalExtra(record, field)
	if err != nil {
		return fmt.Errorf("record %q %s: %w", key, field, err)
	}
	if !present || actual == "" {
		return nil
	}
	if actual != expected {
		return fmt.Errorf("record %q %s %q conflicts with dataset hierarchy value %q", key, field, actual, expected)
	}
	return nil
}

func verifyHierarchyWeight(record *hubv1.Record, key string, expected int) error {
	actual, present, err := operationalExtra(record, "field_weight")
	if err != nil {
		return fmt.Errorf("record %q field_weight: %w", key, err)
	}
	if !present || actual == "" {
		return nil
	}
	parsed, err := strconv.Atoi(actual)
	if err != nil || parsed != expected {
		return fmt.Errorf("record %q field_weight %q conflicts with dataset hierarchy position %d", key, actual, expected)
	}
	return nil
}

// columnOrder defines the canonical column order for Workbench CSV output.
// Reserved Workbench columns come first, then Drupal metadata fields.
var columnOrder = []string{
	"id",
	"parent_id",
	"node_id",
	"file",
	"title",
	"field_model",
	"field_language",
	"field_linked_agent",
	"field_edtf_date_issued",
	"field_edtf_date_created",
	"field_edtf_date",
	"field_abstract",
	"field_rights",
	"field_subject",
	"field_genre",
	"field_identifier",
	"field_extent",
	"field_note",
	"field_member_of",
	"field_part_detail",
	"field_related_item",
	"url_alias",
}

// Serialize writes hub records as Islandora Workbench CSV to w.
//
// If opts.ExtraWriters["agents"] is set, a second CSV with taxonomy term
// metadata for contributors is written there. Islandora Workbench uses this
// to create or update person/corporate_body taxonomy terms with ORCIDs,
// emails, statuses, and institutional relationships.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	if opts == nil {
		opts = format.NewSerializeOptions()
	}

	if opts.Spec == nil {
		return fmt.Errorf("serializing Islandora Workbench requires a transformation specification")
	}
	if err := opts.Spec.Validate(); err != nil {
		return fmt.Errorf("invalid transformation specification: %w", err)
	}
	if err := opts.Spec.ValidateSealed(); err != nil {
		return fmt.Errorf("unsealed transformation specification: %w", err)
	}
	if err := validateArtifactProfileBinding(opts.Spec, opts.SystemProfile); err != nil {
		return err
	}
	if opts.Spec.Target.Format != "islandora-workbench" {
		return fmt.Errorf("transformation target format %q is not Islandora Workbench", opts.Spec.Target.Format)
	}

	delimiter := targetMultiValueSeparator(opts)
	allRows := make([]workbenchRow, 0, len(records))
	colSeen := make(map[string]bool)

	for index, record := range records {
		if record == nil {
			return fmt.Errorf("record %d is nil", index+1)
		}
		cols, agents, err := recordToColumns(record, delimiter)
		if err != nil {
			return fmt.Errorf("record %d: %w", index+1, err)
		}
		var profileColumns map[string]string
		if opts.SystemProfile != nil {
			var err error
			profileColumns, err = recordToProfileColumns(record, opts.SystemProfile, opts.Spec, delimiter)
			if err != nil {
				return fmt.Errorf("record %d: %w", index+1, err)
			}
		}
		if opts.Spec != nil {
			if err := normalizeFileColumns(cols, record, opts.Spec, delimiter); err != nil {
				return fmt.Errorf("record %d: %w", index+1, err)
			}
			target := make(map[string]string)
			for _, field := range opts.Spec.Target.Fields {
				if field.Codec == "ignore" {
					continue
				}
				if !field.AppliesTo(opts.Operation) {
					continue
				}
				value, valueErr := serializedTargetFieldValue(record, field, cols, profileColumns, delimiter)
				if valueErr != nil {
					return fmt.Errorf("record %d target field %q: %w", index+1, field.Name, valueErr)
				}
				if value == "" {
					value = field.Default
				}
				if value == "" && isTargetFieldRequired(field, opts.Operation, record.ObjectModel) {
					operation := opts.Operation
					if operation == "" {
						operation = spec.OperationCreate
					}
					return fmt.Errorf("record %d target field %q is required for %s serialization", index+1, field.Name, operation)
				}
				target[field.Name] = value
				colSeen[field.Name] = true
			}
			cols = target
		} else {
			for col, val := range cols {
				if val != "" {
					colSeen[col] = true
				}
			}
		}
		allRows = append(allRows, workbenchRow{cols: cols, agents: agents})
	}

	columns, err := serializationColumns(opts, colSeen)
	if err != nil {
		return err
	}

	mainWriter := csv.NewWriter(w)

	if opts.IncludeHeader {
		if err := mainWriter.Write(columns); err != nil {
			return fmt.Errorf("writing header: %w", err)
		}
	}

	for _, row := range allRows {
		csvRow := make([]string, len(columns))
		for i, col := range columns {
			csvRow[i] = row.cols[col]
		}
		if err := mainWriter.Write(csvRow); err != nil {
			return fmt.Errorf("writing row: %w", err)
		}
	}

	mainWriter.Flush()
	if err := mainWriter.Error(); err != nil {
		return fmt.Errorf("flushing CSV: %w", err)
	}

	if agentsW, ok := opts.ExtraWriters["agents"]; ok {
		if err := writeAgentsCSV(agentsW, allRows); err != nil {
			return fmt.Errorf("writing agents CSV: %w", err)
		}
	}

	return nil
}

func normalizeFileColumns(cols map[string]string, record *hubv1.Record, transformation *spec.Transformation, delimiter string) error {
	if err := validateSupplementalFileCardinality(record); err != nil {
		return err
	}
	delete(cols, "file")
	delete(cols, "supplemental_file")
	primary := make([]string, 0)
	supplemental := make([]string, 0)
	for index, file := range record.Files {
		if file == nil || file.Path == "" || file.Role == "unpublished_supplemental" {
			continue
		}
		normalized, err := transformation.NormalizeFilePath(file.Path)
		if err != nil {
			return fmt.Errorf("normalizing media file %d: %w", index+1, err)
		}
		if file.Role == "supplemental" {
			supplemental = append(supplemental, normalized)
			continue
		}
		primary = append(primary, normalized)
	}
	if len(primary) > 0 {
		cols["file"] = strings.Join(primary, delimiter)
	}
	if len(supplemental) > 0 {
		cols["supplemental_file"] = strings.Join(supplemental, delimiter)
	}
	return nil
}

func validateSupplementalFileCardinality(record *hubv1.Record) error {
	count := 0
	for _, file := range record.GetFiles() {
		if file == nil || file.GetPath() == "" || file.GetRole() != "supplemental" {
			continue
		}
		count++
		if count > 1 {
			return fmt.Errorf("multiple supplemental files require artifact planning so each file receives its own Workbench media row")
		}
	}
	return nil
}

func isTargetFieldRequired(field spec.Field, operation spec.Operation, objectModel string) bool {
	if field.IsOptionalForObjectModel(objectModel) {
		return false
	}
	if field.Required && (operation == "" || operation == spec.OperationCreate) {
		return true
	}
	for _, candidate := range field.RequiredFor {
		if candidate == operation {
			return true
		}
	}
	return false
}

func targetMultiValueSeparator(opts *format.SerializeOptions) string {
	if opts != nil && opts.Spec != nil && opts.Spec.Target.MultiValueSeparator != "" {
		return opts.Spec.Target.MultiValueSeparator
	}
	if opts != nil && opts.MultiValueSeparator != "" {
		return opts.MultiValueSeparator
	}
	return sep
}

func targetFieldValue(record *hubv1.Record, hubPath string, canonical map[string]string, delimiter string) (string, error) {
	switch hubPath {
	case "Title":
		return canonical["title"], nil
	case "FullTitle":
		return canonical["field_full_title"], nil
	case "ObjectModel":
		return canonical["field_model"], nil
	case "ResourceType":
		return canonical["field_resource_type"], nil
	case "AddCoverpage":
		return canonical["field_add_coverpage"], nil
	case "IsPublic":
		return canonical["published"], nil
	case "Contributors":
		return canonical["field_linked_agent"], nil
	case "Departments":
		return canonical["field_department_name"], nil
	case "Genre":
		return canonical["field_genre"], nil
	case "Dates.issued":
		return canonical["field_edtf_date_issued"], nil
	case "Dates.created":
		return canonical["field_edtf_date_created"], nil
	case "Dates.captured":
		return canonical["field_edtf_date_captured"], nil
	case "Dates.available":
		return canonical["field_edtf_date_embargo"], nil
	case "Publisher":
		return canonical["field_publisher"], nil
	case "PlacePublished":
		return canonical["field_place_published"], nil
	case "Edition":
		return canonical["field_edition"], nil
	case "Language":
		return canonical["field_language"], nil
	case "PhysicalForm":
		return canonical["field_physical_form"], nil
	case "Files.mime_type":
		return canonical["field_media_type"], nil
	case "Extent", "PhysicalDesc":
		return canonical["field_extent"], nil
	case "DigitalOrigin":
		return canonical["field_digital_origin"], nil
	case "Descriptions":
		return canonical["field_abstract"], nil
	case "Notes":
		return canonical["field_note"], nil
	case "LocalRestriction":
		return canonical["field_local_restriction"], nil
	case "Subjects.lcsh":
		return canonical["field_subject_lcsh"], nil
	case "Subjects.keywords":
		return canonical["field_keywords"], nil
	case "Subjects.lcnaf":
		return canonical["field_subjects_name"], nil
	case "Subjects.geographic":
		return canonical["field_geographic_subject"], nil
	case "Subjects.getty_tgn":
		return canonical["field_subject_hierarchical_geo"], nil
	case "Publication.RelatedItem":
		return canonical["field_related_item"], nil
	case "Publication.Part":
		return canonical["field_part_detail"], nil
	case "Identifiers":
		return canonical["field_identifier"], nil
	case "Rights":
		return canonical["field_rights"], nil
	case "AccessCondition":
		return canonical["field_access"], nil
	case "Relations.member_of":
		return canonical["field_member_of"], nil
	case "Files.primary":
		return canonical["file"], nil
	case "Files.supplemental":
		return canonical["supplemental_file"], nil
	}
	if strings.HasPrefix(hubPath, "Extra.") {
		return extraString(record, strings.TrimPrefix(hubPath, "Extra."), delimiter)
	}
	slog.Warn("Islandora Workbench target has no mapping for Hub path", "hub_path", hubPath)
	return "", nil
}

// serializedTargetFieldValue projects one target field exactly as Serialize
// does before applying a field default. Keeping operation routing on this same
// projection prevents Hub values outside a hand-maintained field subset from
// being mistaken for an add-media-only record and silently omitted.
func serializedTargetFieldValue(record *hubv1.Record, field spec.Field, canonical, profileColumns map[string]string, delimiter string) (string, error) {
	if profileColumns != nil && !profileTransportColumn(field.Name) {
		return profileColumns[field.Name], nil
	}
	return targetFieldValue(record, field.Hub, canonical, delimiter)
}

func profileTransportColumn(name string) bool {
	switch drupalFieldBase(name) {
	case "id", "parent_id", "field_weight", "node_id", "file", "supplemental_file",
		"unpublished_supplemental_file", "published", "title", "url_alias":
		return true
	default:
		return false
	}
}

func recordToProfileColumns(record *hubv1.Record, compiled *profile.Compiled, transformation *spec.Transformation, delimiter string) (map[string]string, error) {
	if transformation == nil {
		return nil, fmt.Errorf("drupal system profile requires a profile-bound transformation specification")
	}
	if compiled.System() != "drupal" {
		return nil, fmt.Errorf("system profile %q targets %q, not drupal", compiled.Name(), compiled.System())
	}
	if transformation.Fingerprint.Profile == "" {
		return nil, fmt.Errorf("transformation is not bound to a Drupal profile fingerprint")
	}
	if transformation.Fingerprint.Profile != compiled.Fingerprint() {
		return nil, fmt.Errorf("transformation profile fingerprint does not match target profile")
	}
	if transformation.Fingerprint.Model != compiled.ModelFingerprint() {
		return nil, fmt.Errorf("transformation model fingerprint does not match target profile model")
	}
	entity, err := drupalformat.EncodeEntityWithProfile(record, compiled)
	if err != nil {
		return nil, fmt.Errorf("applying Drupal profile %q: %w", compiled.Name(), err)
	}
	fields := make(map[string][]spec.Field)
	for _, field := range transformation.Target.Fields {
		base := drupalFieldBase(field.Name)
		fields[base] = append(fields[base], field)
	}
	profileFields := make(map[string]profile.ResolvedField)
	for _, mapping := range compiled.Mappings() {
		if mapping.Encode == "none" {
			continue
		}
		profileFields[mapping.Field.Selector.Path] = mapping.Field
	}
	result := make(map[string]string, len(entity))
	for fieldName, raw := range entity {
		if fieldName == "changed" {
			// Drupal owns this modification timestamp. It can still be present in
			// a reusable system profile, but Workbench does not accept it as input.
			continue
		}
		declared := fields[fieldName]
		if len(declared) == 0 {
			return nil, fmt.Errorf("drupal profile emitted undeclared Workbench field %q", fieldName)
		}
		resolvedField, exists := profileFields[fieldName]
		if !exists {
			return nil, fmt.Errorf("drupal profile emitted field %q without an encodable mapping", fieldName)
		}
		for _, field := range declared {
			var cell string
			var err error
			if strings.EqualFold(strings.TrimSpace(resolvedField.SourceType), "created") {
				cell, err = profileWorkbenchCreatedCell(record, field, resolvedField, raw, delimiter)
			} else {
				cell, err = profileWorkbenchCellForRecord(record, field, resolvedField, raw, delimiter)
			}
			if err != nil {
				return nil, fmt.Errorf("encoding Drupal field %q for Workbench column %q: %w", fieldName, field.Name, err)
			}
			if cell != "" {
				result[field.Name] = cell
			}
		}
	}
	return result, nil
}

func profileWorkbenchCell(field spec.Field, resolved profile.ResolvedField, raw any, delimiter string) (string, error) {
	return profileWorkbenchCellForRecord(nil, field, resolved, raw, delimiter)
}

func profileWorkbenchCellForRecord(record *hubv1.Record, field spec.Field, resolved profile.ResolvedField, raw any, delimiter string) (string, error) {
	values, ok := raw.([]any)
	if !ok {
		return "", fmt.Errorf("profile encoder returned %T, want []any", raw)
	}
	encoded := make([]string, 0, len(values))
	for index, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("value %d has type %T, want a Drupal field object", index+1, value)
		}
		bundleHint, err := profileTypedRelationBundleHint(record, field, resolved, index)
		if err != nil {
			return "", fmt.Errorf("value %d: %w", index+1, err)
		}
		cell, err := profileWorkbenchValue(field, resolved, object, bundleHint)
		if err != nil {
			return "", fmt.Errorf("value %d: %w", index+1, err)
		}
		if cell != "" {
			encoded = append(encoded, cell)
		}
	}
	return strings.Join(encoded, delimiter), nil
}

func profileWorkbenchValue(field spec.Field, resolved profile.ResolvedField, value map[string]any, bundleHint string) (string, error) {
	if len(value) == 0 {
		return "", nil
	}
	// The Drupal encoder is bound to a sealed model and profile, so every
	// emitted attribute must be accounted for here. Ignoring an unfamiliar key
	// would silently discard metadata and mask a stale or mismatched profile.
	sourceType := strings.ToLower(strings.TrimSpace(resolved.SourceType))
	if sourceType == "typed_relation" {
		if _, hasValue := value["value"]; hasValue {
			return profileAttributeJSON(value, "value", "attr0")
		}
	}
	switch sourceType {
	case "string", "string_long", "string_textfield", "text", "text_long", "text_with_summary",
		"email", "telephone", "boolean", "integer", "list_integer", "list_string", "decimal", "float",
		"datetime", "daterange", "edtf", "created":
		return profileScalarAttribute(value, "value")
	case "link":
		return profileWorkbenchLinkValue(value)
	case "entity_reference":
		return profileSelectedAttribute(value, "target_id", "target_type", "target_uuid", "url")
	case "typed_relation":
		return profileTypedRelation(value, resolved, bundleHint)
	case "geolocation":
		return profileWorkbenchGeolocationValue(value)
	case "authority_link":
		return profileWorkbenchAuthorityLinkValue(value)
	case "media_track":
		return profileWorkbenchMediaTrackValue(value)
	case "linked_data_field":
		return profileWorkbenchLinkedDataValue(value)
	case "textfield_attr", "textarea_attr":
		return profileAttributeJSON(value, "value", "attr0")
	case "part_detail":
		return profileStructuredJSON(value, "part detail", "type", "caption", "number", "title")
	case "related_item":
		return profileStructuredJSON(value, "related item", "identifier", "identifier_type", "number", "title")
	default:
		return "", fmt.Errorf("drupal source type %q has no explicit Workbench cell encoding", resolved.SourceType)
	}
}

func profileWorkbenchCreatedCell(record *hubv1.Record, field spec.Field, resolved profile.ResolvedField, raw any, delimiter string) (string, error) {
	for _, date := range record.GetDates() {
		if date == nil || date.GetType() != hubv1.DateType_DATE_TYPE_CREATED || strings.TrimSpace(date.GetRaw()) == "" {
			continue
		}
		return validateWorkbenchCreatedTimestamp(date.GetRaw())
	}
	cell, err := profileWorkbenchCell(field, resolved, raw, delimiter)
	if err != nil {
		return "", err
	}
	return validateWorkbenchCreatedTimestamp(cell)
}

func validateWorkbenchCreatedTimestamp(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !workbenchCreatedPattern.MatchString(value) {
		return "", fmt.Errorf("created value %q must use Workbench timestamp grammar YYYY-MM-DDTHH:MM:SS+HH:MM", value)
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return "", fmt.Errorf("created value %q is not a valid RFC 3339 timestamp: %w", value, err)
	}
	return value, nil
}

func profileWorkbenchGeolocationValue(value map[string]any) (string, error) {
	if raw, ok, err := profileWorkbenchRawValue(value); ok || err != nil {
		return raw, err
	}
	if err := requireProfileAttributes(value, "geolocation", "lat", "lng"); err != nil {
		return "", err
	}
	latitude, err := requiredProfileString(value, "lat")
	if err != nil {
		return "", err
	}
	longitude, err := requiredProfileString(value, "lng")
	if err != nil {
		return "", err
	}
	return latitude + "," + longitude, nil
}

func profileWorkbenchAuthorityLinkValue(value map[string]any) (string, error) {
	if raw, ok, err := profileWorkbenchRawValue(value); ok || err != nil {
		return raw, err
	}
	if err := requireProfileAttributes(value, "authority link", "source", "uri", "title"); err != nil {
		return "", err
	}
	source, err := requiredProfileString(value, "source")
	if err != nil {
		return "", err
	}
	uri, err := requiredProfileString(value, "uri")
	if err != nil {
		return "", err
	}
	title, err := optionalProfileString(value, "title")
	if err != nil {
		return "", err
	}
	result := source + "%%" + uri
	if title != "" {
		result += "%%" + title
	}
	return result, nil
}

func profileWorkbenchMediaTrackValue(value map[string]any) (string, error) {
	if raw, ok, err := profileWorkbenchRawValue(value); ok || err != nil {
		return raw, err
	}
	if err := requireProfileAttributes(value, "media track", "label", "kind", "srclang", "file_path", "url"); err != nil {
		return "", err
	}
	label, err := requiredProfileString(value, "label")
	if err != nil {
		return "", err
	}
	kind, err := requiredProfileString(value, "kind")
	if err != nil {
		return "", err
	}
	language, err := requiredProfileString(value, "srclang")
	if err != nil {
		return "", err
	}
	filePath, err := optionalProfileString(value, "file_path")
	if err != nil {
		return "", err
	}
	if filePath == "" {
		filePath, err = requiredProfileString(value, "url")
		if err != nil {
			return "", err
		}
		filePath = path.Base(filePath)
	}
	return strings.Join([]string{label, kind, language, filePath}, ":"), nil
}

func profileWorkbenchLinkedDataValue(value map[string]any) (string, error) {
	if raw, ok, err := profileWorkbenchRawValue(value); ok || err != nil {
		return raw, err
	}
	if err := requireProfileAttributes(value, "linked data", "url", "value"); err != nil {
		return "", err
	}
	url, err := requiredProfileString(value, "url")
	if err != nil {
		return "", err
	}
	label, err := optionalProfileString(value, "value")
	if err != nil {
		return "", err
	}
	if label == "" {
		return url, nil
	}
	return url + "%%" + label, nil
}

func profileWorkbenchRawValue(value map[string]any) (string, bool, error) {
	raw, exists := value["value"]
	if !exists || len(value) != 1 {
		return "", false, nil
	}
	text, err := profileScalarString(raw)
	return text, true, err
}

func requireProfileAttributes(value map[string]any, label string, allowed ...string) error {
	permitted := make(map[string]struct{}, len(allowed))
	for _, attribute := range allowed {
		permitted[attribute] = struct{}{}
	}
	for attribute := range value {
		if _, ok := permitted[attribute]; !ok {
			return fmt.Errorf("%s has unsupported attribute %q", label, attribute)
		}
	}
	return nil
}

func profileWorkbenchLinkValue(value map[string]any) (string, error) {
	allowed := map[string]struct{}{"uri": {}, "title": {}}
	for key := range value {
		if _, exists := allowed[key]; !exists {
			return "", fmt.Errorf("link value has unsupported attribute %q", key)
		}
	}
	uri, err := requiredProfileString(value, "uri")
	if err != nil {
		return "", err
	}
	title, err := optionalProfileString(value, "title")
	if err != nil {
		return "", err
	}
	if title == "" {
		return uri, nil
	}
	return uri + "%%" + title, nil
}

func profileSelectedAttribute(value map[string]any, selected string, allowed ...string) (string, error) {
	permitted := map[string]struct{}{selected: {}}
	for _, key := range allowed {
		permitted[key] = struct{}{}
	}
	for key := range value {
		if _, exists := permitted[key]; !exists {
			return "", fmt.Errorf("%q value has unsupported attribute %q", selected, key)
		}
	}
	raw, exists := value[selected]
	if !exists {
		return "", fmt.Errorf("value has no %q attribute", selected)
	}
	return profileScalarString(raw)
}

func profileScalarAttribute(value map[string]any, attribute string) (string, error) {
	if len(value) != 1 {
		return "", fmt.Errorf("scalar %q value has unexpected attributes %s", attribute, sortedMapKeys(value))
	}
	raw, exists := value[attribute]
	if !exists {
		return "", fmt.Errorf("scalar value has no %q attribute", attribute)
	}
	return profileScalarString(raw)
}

func profileTypedRelation(value map[string]any, resolved profile.ResolvedField, bundleHint string) (string, error) {
	allowed := map[string]struct{}{"target_id": {}, "target_type": {}, "rel_type": {}}
	for key := range value {
		if _, exists := allowed[key]; !exists {
			return "", fmt.Errorf("typed relation has unsupported attribute %q", key)
		}
	}
	targetID, err := requiredProfileString(value, "target_id")
	if err != nil {
		return "", err
	}
	targetType, err := requiredProfileString(value, "target_type")
	if err != nil {
		return "", err
	}
	if resolved.Reference == nil || len(resolved.Reference.Bundles) == 0 {
		return "", fmt.Errorf("typed relation requires model-declared profile reference bundles")
	}
	if expected := strings.TrimSpace(resolved.Reference.EntityType); expected != "" && targetType != expected {
		return "", fmt.Errorf("typed relation target type %q does not match model entity type %q", targetType, expected)
	}
	targetBundle, targetValue, err := profileTypedRelationTarget(targetID, bundleHint, resolved.Reference.Bundles)
	if err != nil {
		return "", err
	}
	role, err := optionalProfileString(value, "rel_type")
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, 3)
	if role != "" {
		parts = append(parts, role)
	}
	parts = append(parts, targetBundle, targetValue)
	return strings.Join(parts, ":"), nil
}

func profileTypedRelationTarget(targetID, bundleHint string, bundles []string) (string, string, error) {
	allowed := make(map[string]struct{}, len(bundles))
	ordered := make([]string, 0, len(bundles))
	for _, bundle := range bundles {
		bundle = strings.TrimSpace(bundle)
		if bundle == "" {
			return "", "", fmt.Errorf("typed relation profile reference bundle is empty")
		}
		if _, exists := allowed[bundle]; exists {
			continue
		}
		allowed[bundle] = struct{}{}
		ordered = append(ordered, bundle)
	}
	for _, bundle := range ordered {
		if target, found := strings.CutPrefix(targetID, bundle+":"); found {
			if strings.TrimSpace(target) == "" {
				return "", "", fmt.Errorf("typed relation target for bundle %q is empty", bundle)
			}
			return bundle, target, nil
		}
	}
	if prefix, _, found := strings.Cut(targetID, ":"); found && isWorkbenchContributorBundle(prefix) {
		return "", "", fmt.Errorf("typed relation bundle %q is outside model-declared bundles %s", prefix, strings.Join(ordered, ", "))
	}
	if bundleHint = strings.TrimSpace(bundleHint); bundleHint != "" {
		if _, exists := allowed[bundleHint]; !exists {
			return "", "", fmt.Errorf("typed relation bundle %q is outside model-declared bundles %s", bundleHint, strings.Join(ordered, ", "))
		}
		return bundleHint, targetID, nil
	}
	if len(ordered) == 1 {
		return ordered[0], targetID, nil
	}
	return "", "", fmt.Errorf("typed relation target %q does not identify one of model-declared bundles %s", targetID, strings.Join(ordered, ", "))
}

func isWorkbenchContributorBundle(value string) bool {
	switch strings.TrimSpace(value) {
	case "person", "family", "corporate_body", "organization":
		return true
	default:
		return false
	}
}

func profileTypedRelationBundleHint(record *hubv1.Record, field spec.Field, resolved profile.ResolvedField, index int) (string, error) {
	if record == nil || !strings.EqualFold(strings.TrimSpace(resolved.SourceType), "typed_relation") {
		return "", nil
	}
	base, _, _ := strings.Cut(field.Hub, ".")
	if base != "Contributors" || index < 0 || index >= len(record.GetContributors()) {
		return "", nil
	}
	contributor := record.GetContributors()[index]
	if contributor == nil {
		return "", fmt.Errorf("typed relation contributor is nil")
	}
	if resolved.Reference == nil {
		return "", nil
	}
	allowed := make(map[string]struct{}, len(resolved.Reference.Bundles))
	for _, bundle := range resolved.Reference.Bundles {
		allowed[bundle] = struct{}{}
	}
	choose := func(candidates ...string) string {
		for _, candidate := range candidates {
			if _, ok := allowed[candidate]; ok {
				return candidate
			}
		}
		return ""
	}
	switch contributor.GetType() {
	case hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON:
		if bundle := choose("person"); bundle != "" {
			return bundle, nil
		}
		return "person", nil
	case hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION:
		if bundle := choose("corporate_body", "organization"); bundle != "" {
			return bundle, nil
		}
		organizationBundles := make([]string, 0, len(allowed))
		for bundle := range allowed {
			if bundle != "person" {
				organizationBundles = append(organizationBundles, bundle)
			}
		}
		if len(organizationBundles) == 1 {
			return organizationBundles[0], nil
		}
		return "corporate_body", nil
	default:
		return "", nil
	}
}

func profileAttributeJSON(value map[string]any, valueAttribute, discriminatorAttribute string) (string, error) {
	allowed := map[string]struct{}{valueAttribute: {}, discriminatorAttribute: {}}
	for key := range value {
		if _, exists := allowed[key]; !exists {
			return "", fmt.Errorf("attribute value has unsupported attribute %q", key)
		}
	}
	text, err := requiredProfileString(value, valueAttribute)
	if err != nil {
		return "", err
	}
	discriminator, err := requiredProfileString(value, discriminatorAttribute)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]string{valueAttribute: text, discriminatorAttribute: discriminator})
	if err != nil {
		return "", fmt.Errorf("encoding attribute JSON: %w", err)
	}
	return string(data), nil
}

func profileStructuredJSON(value map[string]any, label string, allowed ...string) (string, error) {
	permitted := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		permitted[key] = struct{}{}
	}
	result := make(map[string]string, len(value))
	for key := range value {
		if _, exists := permitted[key]; !exists {
			return "", fmt.Errorf("%s has unsupported attribute %q", label, key)
		}
	}
	for _, key := range allowed {
		text, err := optionalProfileString(value, key)
		if err != nil {
			return "", err
		}
		if text != "" {
			result[key] = text
		}
	}
	if len(result) == 0 {
		return "", fmt.Errorf("%s has no supported attributes", label)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encoding %s JSON: %w", label, err)
	}
	return string(data), nil
}

func requiredProfileString(value map[string]any, key string) (string, error) {
	text, err := optionalProfileString(value, key)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("value has no %q attribute", key)
	}
	return text, nil
}

func optionalProfileString(value map[string]any, key string) (string, error) {
	raw, exists := value[key]
	if !exists || raw == nil {
		return "", nil
	}
	return profileScalarString(raw)
}

func profileScalarString(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case bool:
		return boolString(typed), nil
	case int:
		return strconv.Itoa(typed), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return "", fmt.Errorf("numeric value is not finite")
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("attribute has unsupported scalar type %T", value)
	}
}

func sortedMapKeys(value map[string]any) string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func drupalFieldBase(name string) string {
	if index := strings.IndexByte(name, '.'); index >= 0 {
		return name[:index]
	}
	return name
}

// recordToColumns converts a hub record to a map of workbench column values
// and a slice of agent rows (one per contributor with extended metadata).
func recordToColumns(record *hubv1.Record, delimiter string) (map[string]string, [][]string, error) {
	if err := validateSupplementalFileCardinality(record); err != nil {
		return nil, nil, err
	}
	return projectRecordToColumns(record, delimiter)
}

// projectRecordToColumns builds the canonical intermediate projection used
// while artifact routing is still deciding how to split supplemental media.
// It must not be emitted directly because Workbench accepts one supplemental
// path per parent row.
func projectRecordToColumns(record *hubv1.Record, delimiter string) (map[string]string, [][]string, error) {
	cols := make(map[string]string)
	var agents [][]string

	// Reserved workbench columns from Extra
	if id := hub.GetExtraString(record, "id"); id != "" {
		cols["id"] = id
	}
	if nid := hub.GetExtraString(record, "node_id"); nid != "" {
		cols["node_id"] = nid
	}
	if parentID := hub.GetExtraString(record, "parent_id"); parentID != "" {
		cols["parent_id"] = parentID
	}
	weight, err := extraString(record, "field_weight", delimiter)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding Extra.field_weight: %w", err)
	}
	if weight != "" {
		cols["field_weight"] = weight
	}
	if alias := hub.GetExtraString(record, "url_alias"); alias != "" {
		cols["url_alias"] = alias
	}

	var primaryFiles, supplementalFiles []string
	for _, file := range record.Files {
		if file == nil || file.Path == "" {
			continue
		}
		switch file.Role {
		case "supplemental":
			supplementalFiles = append(supplementalFiles, file.Path)
		case "unpublished_supplemental":
			// Planned as its own artifact; never leak it into a published task.
		default:
			primaryFiles = append(primaryFiles, file.Path)
		}
	}
	if len(primaryFiles) > 0 {
		cols["file"] = strings.Join(primaryFiles, delimiter)
	}
	if len(supplementalFiles) > 0 {
		cols["supplemental_file"] = strings.Join(supplementalFiles, delimiter)
	}

	title := record.Title
	fullTitle := record.FullTitle
	if titleRunes := []rune(title); len(titleRunes) > 255 {
		if fullTitle == "" {
			fullTitle = title
		}
		title = string(titleRunes[:255])
	}
	cols["title"] = title
	cols["field_full_title"] = fullTitle
	if _, present := hub.GetExtra(record, "_present_is_public"); present || record.IsPublic {
		cols["published"] = boolString(record.IsPublic)
	}
	if _, present := hub.GetExtra(record, "_present_add_coverpage"); present || record.AddCoverpage {
		cols["field_add_coverpage"] = boolString(record.AddCoverpage)
	}

	if record.ObjectModel != "" {
		cols["field_model"] = record.ObjectModel
	} else if model := islandoraModel(record.ResourceType); model != "" {
		cols["field_model"] = model
	}
	if record.ResourceType != nil {
		cols["field_resource_type"] = hub.ResourceTypeString(record.ResourceType)
	}

	cols["field_language"] = strings.Join(hub.GetLanguages(record), delimiter)

	// Contributors → field_linked_agent + optional agents rows
	if len(record.Contributors) > 0 {
		linkedAgents := make([]string, 0, len(record.Contributors))
		for _, c := range record.Contributors {
			linkedAgents = append(linkedAgents, serializeLinkedAgent(c))
			if needsAgentRow(c) {
				agents = append(agents, toAgentRow(c))
			}
		}
		cols["field_linked_agent"] = strings.Join(linkedAgents, delimiter)
	}
	if len(record.Departments) > 0 {
		cols["field_department_name"] = strings.Join(nonempty(record.Departments), delimiter)
	}

	// Dates (EDTF format)
	var issuedDates, createdDates, capturedDates, availableDates []string
	for _, d := range record.Dates {
		edtf := hub.FormatEDTF(d)
		if edtf == "" {
			continue
		}
		switch d.Type {
		case hubv1.DateType_DATE_TYPE_ISSUED, hubv1.DateType_DATE_TYPE_PUBLISHED:
			issuedDates = append(issuedDates, edtf)
		case hubv1.DateType_DATE_TYPE_CREATED:
			createdDates = append(createdDates, edtf)
		case hubv1.DateType_DATE_TYPE_CAPTURED:
			capturedDates = append(capturedDates, edtf)
		case hubv1.DateType_DATE_TYPE_AVAILABLE:
			availableDates = append(availableDates, edtf)
		case hubv1.DateType_DATE_TYPE_ACCEPTED:
			// Workbench has no accepted-date destination. In particular, an ETD
			// acceptance date must not be emitted as its issued/completion date.
		default:
			issuedDates = append(issuedDates, edtf)
		}
	}
	if len(issuedDates) > 0 {
		cols["field_edtf_date_issued"] = strings.Join(issuedDates, delimiter)
	}
	if len(createdDates) > 0 {
		cols["field_edtf_date_created"] = strings.Join(createdDates, delimiter)
	}
	if len(capturedDates) > 0 {
		cols["field_edtf_date_captured"] = strings.Join(capturedDates, delimiter)
	}
	if len(availableDates) > 0 {
		cols["field_edtf_date_embargo"] = strings.Join(availableDates, delimiter)
	}
	if season := hub.GetExtraString(record, "date_season"); season != "" {
		cols["field_date_season"] = season
	}

	// Abstract and description both go to field_abstract with an attr0 attribute
	var abstracts []string
	if record.Abstract != "" {
		abstracts = append(abstracts, attrValue(record.Abstract, "abstract"))
	}
	if record.Description != "" {
		abstracts = append(abstracts, attrValue(record.Description, "description"))
	}
	if len(abstracts) > 0 {
		cols["field_abstract"] = strings.Join(abstracts, delimiter)
	}
	cols["field_publisher"] = strings.Join(hub.GetPublishers(record), delimiter)
	cols["field_place_published"] = strings.Join(hub.GetPlacesPublished(record), delimiter)
	cols["field_edition"] = strings.Join(hub.GetEditions(record), delimiter)
	cols["field_digital_origin"] = record.DigitalOrigin

	// Rights → field_rights (URI form preferred)
	if len(record.Rights) > 0 {
		rights := make([]string, 0, len(record.Rights))
		for _, r := range record.Rights {
			val := rightsValue(r)
			if val != "" {
				rights = append(rights, val)
			}
		}
		if len(rights) > 0 {
			cols["field_rights"] = strings.Join(rights, delimiter)
		}
	}

	// Subjects → field_subject
	if len(record.Subjects) > 0 {
		subjects := make([]string, 0, len(record.Subjects))
		for _, s := range record.Subjects {
			if val := subjectValue(s); val != "" {
				subjects = append(subjects, val)
			}
		}
		if len(subjects) > 0 {
			cols["field_subject"] = strings.Join(subjects, delimiter)
		}
		assignSubjectColumns(cols, record.Subjects, delimiter)
	}

	// Genres → field_genre
	if len(record.Genres) > 0 {
		genres := make([]string, 0, len(record.Genres))
		for _, g := range record.Genres {
			if g.Value != "" {
				genres = append(genres, g.Value)
			}
		}
		if len(genres) > 0 {
			cols["field_genre"] = strings.Join(genres, delimiter)
		}
	}
	if len(record.PhysicalForm) > 0 {
		values := make([]string, 0, len(record.PhysicalForm))
		for _, physicalForm := range record.PhysicalForm {
			if physicalForm != nil && physicalForm.Value != "" {
				values = append(values, physicalForm.Value)
			}
		}
		cols["field_physical_form"] = strings.Join(values, delimiter)
	}

	// Identifiers → field_identifier (attr0 notation)
	if len(record.Identifiers) > 0 {
		ids := make([]string, 0, len(record.Identifiers))
		for _, id := range record.Identifiers {
			if val := identifierValue(id); val != "" {
				ids = append(ids, val)
			}
		}
		if len(ids) > 0 {
			cols["field_identifier"] = strings.Join(ids, delimiter)
		}
	}

	// Physical description → field_extent
	var extents []string
	for _, description := range hub.GetPhysicalDescriptions(record) {
		extents = append(extents, attrValue(description, "page"))
	}
	if record.Dimensions != "" {
		extents = append(extents, attrValue(record.Dimensions, "dimensions"))
	}
	if record.Duration != "" {
		extents = append(extents, attrValue(record.Duration, "minutes"))
	}
	if record.PageCount > 0 {
		extents = append(extents, attrValue(fmt.Sprintf("%d", record.PageCount), "page"))
	}
	if file := firstPrimaryFile(record); file != nil {
		cols["field_media_type"] = file.MimeType
		if file.SizeBytes > 0 {
			extents = append(extents, attrValue(strconv.FormatInt(file.SizeBytes, 10), "bytes"))
		}
	}
	if len(extents) > 0 {
		cols["field_extent"] = strings.Join(extents, delimiter)
	}

	// Notes → field_note
	if len(record.Notes) > 0 {
		notes := make([]string, 0, len(record.Notes))
		for _, n := range record.Notes {
			if n != "" {
				notes = append(notes, attrValue(n, "note"))
			}
		}
		if len(notes) > 0 {
			cols["field_note"] = strings.Join(notes, delimiter)
		}
	}
	noteValues := splitJoined(cols["field_note"], delimiter)
	if record.PreferredCitation != "" {
		noteValues = append(noteValues, attrValue(record.PreferredCitation, "preferred-citation"))
	}
	if record.CaptureDevice != "" {
		noteValues = append(noteValues, attrValue(record.CaptureDevice, "capture-device"))
	}
	if record.Ppi > 0 {
		noteValues = append(noteValues, attrValue(strconv.FormatInt(int64(record.Ppi), 10), "ppi"))
	}
	if record.ArchivalLocation != nil {
		if record.ArchivalLocation.Collection != "" {
			noteValues = append(noteValues, attrValue(record.ArchivalLocation.Collection, "collection"))
		}
		if record.ArchivalLocation.Box != "" {
			noteValues = append(noteValues, attrValue(record.ArchivalLocation.Box, "box"))
		}
		if record.ArchivalLocation.Series != "" {
			noteValues = append(noteValues, attrValue(record.ArchivalLocation.Series, "series"))
		}
		if record.ArchivalLocation.Folder != "" {
			noteValues = append(noteValues, attrValue(record.ArchivalLocation.Folder, "folder"))
		}
	}
	if len(noteValues) > 0 {
		cols["field_note"] = strings.Join(noteValues, delimiter)
	}
	if record.LocalRestriction != "" {
		cols["field_local_restriction"] = restrictionString(record.LocalRestriction)
	}
	cols["field_access"] = record.AccessCondition

	// Relations → field_member_of
	memberOf := hub.GetMemberOf(record)
	if len(memberOf) > 0 {
		vals := make([]string, 0, len(memberOf))
		for _, rel := range memberOf {
			if rel.TargetId != "" {
				vals = append(vals, rel.TargetId)
			} else if rel.TargetTitle != "" {
				vals = append(vals, rel.TargetTitle)
			}
		}
		if len(vals) > 0 {
			cols["field_member_of"] = strings.Join(vals, delimiter)
		}
	}

	// Publication details → field_part_detail
	if record.Publication != nil {
		var parts []string
		if record.Publication.Volume != "" {
			parts = append(parts, partDetail(record.Publication.Volume, "volume"))
		}
		if record.Publication.Issue != "" {
			parts = append(parts, partDetail(record.Publication.Issue, "issue"))
		}
		if record.Publication.Pages != "" {
			parts = append(parts, partDetail(record.Publication.Pages, "page"))
		}
		if len(parts) > 0 {
			cols["field_part_detail"] = strings.Join(parts, delimiter)
		}

		// Journal title → field_related_item
		var relatedItems []string
		if record.Publication.Title != "" {
			relatedItems = append(relatedItems, fmt.Sprintf(`{"title":"%s"}`, escapeJSON(record.Publication.Title)))
		}
		issn := record.Publication.LIssn
		if issn == "" {
			issn = record.Publication.Issn
		}
		if issn != "" {
			relatedItems = append(relatedItems, fmt.Sprintf(`{"type":"issn","identifier":"%s"}`, escapeJSON(issn)))
		}
		if len(relatedItems) > 0 {
			cols["field_related_item"] = strings.Join(relatedItems, delimiter)
		}
	}

	return cols, agents, nil
}

func serializationColumns(opts *format.SerializeOptions, seen map[string]bool) ([]string, error) {
	if len(opts.Columns) > 0 {
		columns := make([]string, 0, len(opts.Columns))
		used := make(map[string]struct{}, len(opts.Columns))
		for _, raw := range opts.Columns {
			column := strings.TrimSpace(raw)
			if column == "" {
				return nil, fmt.Errorf("serialization column cannot be empty")
			}
			if _, exists := used[column]; exists {
				return nil, fmt.Errorf("serialization column %q is duplicated", column)
			}
			if opts.Spec != nil {
				field, ok := opts.Spec.TargetField(column)
				if !ok {
					return nil, fmt.Errorf("serialization column %q is not declared for operation %q", column, opts.Operation)
				}
				if field.Codec == "ignore" {
					continue
				}
				if !field.AppliesTo(opts.Operation) {
					return nil, fmt.Errorf("serialization column %q is not declared for operation %q", column, opts.Operation)
				}
			}
			used[column] = struct{}{}
			columns = append(columns, column)
		}
		return columns, nil
	}
	if opts.Spec != nil {
		columns := make([]string, 0, len(opts.Spec.Target.Fields))
		for _, field := range opts.Spec.Target.Fields {
			if field.Codec != "ignore" && field.AppliesTo(opts.Operation) {
				columns = append(columns, field.Name)
			}
		}
		return columns, nil
	}
	return orderedColumns(seen), nil
}

func extraString(record *hubv1.Record, key, delimiter string) (string, error) {
	value, ok := hub.GetExtra(record, key)
	if !ok {
		return "", nil
	}
	return extraWorkbenchValue(value, delimiter)
}

func extraWorkbenchValue(value any, delimiter string) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return "", fmt.Errorf("numeric value is not finite")
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case bool:
		return boolString(typed), nil
	case nil:
		return "", nil
	case []any:
		values := make([]string, 0, len(typed))
		for index, item := range typed {
			value, err := extraWorkbenchListValue(item)
			if err != nil {
				return "", fmt.Errorf("list value %d: %w", index+1, err)
			}
			values = append(values, value)
		}
		return strings.Join(values, delimiter), nil
	default:
		return canonicalExtraJSON(typed)
	}
}

func extraWorkbenchListValue(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return "", fmt.Errorf("numeric value is not finite")
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case bool:
		return boolString(typed), nil
	default:
		return canonicalExtraJSON(typed)
	}
}

func canonicalExtraJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encoding structured value as canonical JSON: %w", err)
	}
	return string(data), nil
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func restrictionString(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "yes", "true", "local restriction", "restricted":
		return "1"
	default:
		return "0"
	}
}

func firstPrimaryFile(record *hubv1.Record) *hubv1.File {
	for _, file := range record.Files {
		if file != nil && (file.Role == "" || file.Role == "primary") {
			return file
		}
	}
	return nil
}

func nonempty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func splitJoined(value, delimiter string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, delimiter)
}

func assignSubjectColumns(cols map[string]string, subjects []*hubv1.Subject, delimiter string) {
	var lcsh, keywords, names, geographic, hierarchical []string
	for _, subject := range subjects {
		if subject == nil || (subject.Value == "" && subject.Uri == "") {
			continue
		}
		value := subject.Value
		if value == "" {
			value = subject.Uri
		}
		switch {
		case subject.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GETTY_TGN:
			if subject.Uri != "" {
				value = subject.Uri
			}
			hierarchical = append(hierarchical, value)
		case subject.Type == hubv1.SubjectType_SUBJECT_TYPE_GEOGRAPHIC && subject.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCNAF:
			geographic = append(geographic, "geographic_naf:"+value)
		case subject.Type == hubv1.SubjectType_SUBJECT_TYPE_GEOGRAPHIC:
			geographic = append(geographic, "geographic_local:"+value)
		case subject.Type == hubv1.SubjectType_SUBJECT_TYPE_NAME || subject.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCNAF:
			names = append(names, value)
		case subject.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH:
			lcsh = append(lcsh, value)
		case subject.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS:
			keywords = append(keywords, value)
		}
	}
	if len(lcsh) > 0 {
		cols["field_subject_lcsh"] = strings.Join(lcsh, delimiter)
	}
	if len(keywords) > 0 {
		cols["field_keywords"] = strings.Join(keywords, delimiter)
	}
	if len(names) > 0 {
		cols["field_subjects_name"] = strings.Join(names, delimiter)
	}
	if len(geographic) > 0 {
		cols["field_geographic_subject"] = strings.Join(geographic, delimiter)
	}
	if len(hierarchical) > 0 {
		cols["field_subject_hierarchical_geo"] = strings.Join(hierarchical, delimiter)
	}
}

// orderedColumns returns the columns that have data, in canonical order,
// with any unrecognised columns appended alphabetically at the end.
func orderedColumns(seen map[string]bool) []string {
	result := make([]string, 0, len(seen))
	appended := make(map[string]bool)

	for _, col := range columnOrder {
		if seen[col] {
			result = append(result, col)
			appended[col] = true
		}
	}

	// Any column not in columnOrder (e.g., from a profile) appended at end
	extras := make([]string, 0)
	for col := range seen {
		if !appended[col] {
			extras = append(extras, col)
		}
	}
	// Sort extras for deterministic output
	for i := 0; i < len(extras)-1; i++ {
		for j := i + 1; j < len(extras); j++ {
			if extras[i] > extras[j] {
				extras[i], extras[j] = extras[j], extras[i]
			}
		}
	}

	return append(result, extras...)
}

// writeAgentsCSV writes the agents CSV for contributors with extended metadata.
// Columns: term_name, field_contributor_status, field_relationships, field_email, field_identifier
func writeAgentsCSV(w io.Writer, rows []workbenchRow) error {
	writer := csv.NewWriter(w)
	header := []string{"term_name", "field_contributor_status", "field_relationships", "field_email", "field_identifier"}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("writing agents header: %w", err)
	}
	for _, row := range rows {
		for _, agent := range row.agents {
			if err := writer.Write(agent); err != nil {
				return fmt.Errorf("writing agent row: %w", err)
			}
		}
	}
	writer.Flush()
	return writer.Error()
}

// serializeLinkedAgent formats a contributor for the field_linked_agent column.
// Format: "relators:cre:person:Name" or "relators:cre:person:Name - Institution"
func serializeLinkedAgent(c *hubv1.Contributor) string {
	typePart := "person"
	if c.Type == hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION {
		typePart = "corporate_body"
	}

	institution := contributorInstitution(c)

	name := c.Name
	if institution != "" {
		name = fmt.Sprintf("%s - %s", name, institution)
	}

	roleCode := c.RoleCode
	if roleCode == "" {
		roleCode = "relators:aut"
	}

	return fmt.Sprintf("%s:%s:%s", roleCode, typePart, name)
}

// needsAgentRow returns true when this contributor has metadata beyond just a name
// and role — meaning we need to create/update a taxonomy term for them.
func needsAgentRow(c *hubv1.Contributor) bool {
	if c.Status != "" || c.Email != "" {
		return true
	}
	if contributorInstitution(c) != "" {
		return true
	}
	for _, id := range c.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID {
			return true
		}
	}
	return false
}

// toAgentRow converts a contributor to an agents CSV row.
// Returns [term_name, field_contributor_status, field_relationships, field_email, field_identifier]
func toAgentRow(c *hubv1.Contributor) []string {
	institution := contributorInstitution(c)

	termName := c.Name
	if institution != "" {
		termName = fmt.Sprintf("%s - %s", termName, institution)
	}

	relationships := ""
	if institution != "" {
		relationships = fmt.Sprintf("schema:worksFor:corporate_body:%s", institution)
	}

	identifier := ""
	for _, id := range c.Identifiers {
		if id.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID && id.Value != "" {
			identifier = fmt.Sprintf(`{"attr0":"orcid","value":"%s"}`, id.Value)
			break
		}
	}

	return []string{termName, c.Status, relationships, c.Email, identifier}
}

// contributorInstitution returns the contributor's primary institution name.
func contributorInstitution(c *hubv1.Contributor) string {
	if len(c.Affiliations) > 0 {
		return c.Affiliations[0].Name
	}
	return c.Affiliation
}

// islandoraModel maps a hub ResourceType to an Islandora Models vocabulary term.
func islandoraModel(rt *hubv1.ResourceType) string {
	if rt == nil {
		return ""
	}
	switch rt.Type {
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE:
		return "Image"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO:
		return "Video"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO:
		return "Audio"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION:
		return "Collection"
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE:
		return "Binary"
	default:
		return "Digital Document"
	}
}

// rightsValue extracts the preferred value for the field_rights column.
// Prefers the URI (rights statements / CC), falls back to statement text.
func rightsValue(r *hubv1.Rights) string {
	if r.Uri != "" {
		return r.Uri
	}
	if r.License != "" {
		return r.License
	}
	return r.Statement
}

// subjectValue formats a subject for the field_subject column.
// NAF geographic subjects use a "geographic_naf:" prefix.
func subjectValue(s *hubv1.Subject) string {
	if s.Value == "" {
		return ""
	}
	switch s.Vocabulary {
	case hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH:
		if s.Uri != "" {
			return s.Value
		}
		return s.Value
	default:
		return s.Value
	}
}

// identifierValue formats an identifier as a Workbench attr0 JSON value.
func identifierValue(id *hubv1.Identifier) string {
	if id.Value == "" {
		return ""
	}
	switch id.Type {
	case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
		return attrValue(id.Value, "doi")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
		return attrValue(id.Value, "uri")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE:
		return attrValue(id.Value, "hdl")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
		return attrValue(id.Value, "isbn")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
		return attrValue(id.Value, "issn")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL:
		return attrValue(id.Value, "local")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER:
		return attrValue(id.Value, "call-number")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER:
		return attrValue(id.Value, "report-number")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV:
		return attrValue(id.Value, "arxiv")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_WOS:
		return attrValue(id.Value, "wos")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_PMID:
		return attrValue(id.Value, "pmid")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID:
		return attrValue(id.Value, "pmcid")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_UUID:
		return attrValue(id.Value, "uuid")
	case hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID:
		// ORCIDs belong on the contributor taxonomy term, not on the node
		return ""
	default:
		return ""
	}
}

// attrValue builds a Workbench attr0 JSON object: {"value":"...","attr0":"..."}
func attrValue(value, attr string) string {
	return fmt.Sprintf(`{"value":"%s","attr0":"%s"}`, escapeJSON(value), attr)
}

// partDetail builds a field_part_detail JSON object: {"number":"...","type":"..."}
func partDetail(number, partType string) string {
	return fmt.Sprintf(`{"number":"%s","type":"%s"}`, escapeJSON(number), partType)
}

// escapeJSON escapes backslashes and double quotes for embedding in JSON strings.
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
