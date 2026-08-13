package httpapi

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

// validateWorkbenchRows contains only deterministic spreadsheet checks. Site
// lookups, TGN resolution, and staging-file inspection require deployment
// context and intentionally belong to sitectl rather than this HTTP service.
func validateWorkbenchRows(rows [][]string, transformation *spec.Transformation) CheckResult {
	result := make(CheckResult)
	layout := newWorkbenchLayout(rows, transformation)
	if layout.dataStart >= len(rows) {
		return result
	}

	uploadIDs := make(map[string]int)
	for rowIndex := layout.dataStart; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		if emptySpreadsheetRow(row) {
			continue
		}
		id := layout.value(row, "id")
		if id == "" {
			continue
		}
		if _, exists := uploadIDs[id]; exists {
			layout.add(result, rowIndex, "id", "Duplicate upload ID")
			continue
		}
		uploadIDs[id] = rowIndex
	}

	for rowIndex := layout.dataStart; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		if emptySpreadsheetRow(row) {
			continue
		}
		model := layout.value(row, "field_model")
		parentID := layout.value(row, "parent_id")
		parentCollection := layout.value(row, "field_member_of")
		isCreate := layout.value(row, "node_id") == ""

		resourceType, _ := transformation.SourceField("field_resource_type")
		if isCreate && layout.has("field_resource_type") && layout.value(row, "field_resource_type") == "" && !resourceType.IsOptionalForObjectModel(model) {
			layout.add(result, rowIndex, "field_resource_type", "Must have a resource type")
		}
		if isCreate && strings.EqualFold(model, "Paged Content") && parentID == "" && parentCollection == "" {
			layout.addFirst(result, rowIndex, []string{"field_member_of", "parent_id"}, "Paged content must have a parent collection or parent ID")
		}
		if isCreate && strings.EqualFold(model, "Page") && parentID == "" && parentCollection == "" {
			layout.addFirst(result, rowIndex, []string{"parent_id", "field_member_of"}, "Pages must have a parent ID or parent collection")
		}
		if isCreate && len(layout.values(row, "supplemental_file")) > 1 && layout.value(row, "id") == "" {
			layout.add(result, rowIndex, "supplemental_file", "Multiple supplemental files require an upload ID for node-ID reconciliation")
		}

		if title := layout.value(row, "title"); utf8.RuneCountInString(title) > 255 {
			layout.add(result, rowIndex, "title", "Title is longer than 255 characters")
		}
		for _, value := range layout.values(row, "field_member_of") {
			if _, err := strconv.ParseUint(value, 10, 64); err != nil {
				layout.add(result, rowIndex, "field_member_of", "Must be an unsigned integer")
				break
			}
		}
		for _, value := range layout.values(row, "field_identifier.attr0=uri") {
			if !validAbsoluteHTTPURL(value) {
				layout.add(result, rowIndex, "field_identifier.attr0=uri", "Invalid URL")
				break
			}
		}
		for _, value := range layout.values(row, "field_rights") {
			if !validRightsStatement(value) {
				layout.add(result, rowIndex, "field_rights", "Invalid Rights Statement")
				break
			}
		}
		for _, field := range []string{"field_add_coverpage", "published"} {
			value := layout.value(row, field)
			if value != "" && !strings.EqualFold(value, "Yes") && !strings.EqualFold(value, "No") {
				layout.add(result, rowIndex, field, "Invalid value. Must be Yes or No")
			}
		}
		if transformation != nil {
			for _, field := range transformation.Source.Fields {
				if !rejectsLineBreaks(field) || !strings.ContainsAny(layout.value(row, field.Name), "\r\n") {
					continue
				}
				layout.add(result, rowIndex, field.Name, "Line breaks are not allowed in taxonomy or multi-value cells")
			}
		}
		for _, file := range layout.values(row, "file") {
			if model != "" && !allowedWorkbenchExtension(file, model) {
				layout.add(result, rowIndex, "file", "File extension is not allowed for object model "+workbenchMediaType(model))
				break
			}
		}

		if parentID != "" {
			if parentID == layout.value(row, "id") {
				layout.add(result, rowIndex, "parent_id", "Upload ID and parent ID cannot be equal")
			} else if _, exists := uploadIDs[parentID]; !exists {
				layout.add(result, rowIndex, "parent_id", "Unknown parent ID")
			}
		}
	}
	return result
}

func rejectsLineBreaks(field spec.Field) bool {
	if field.Codec == "multi" || field.Codec == "contributors" || (field.Codec == "file" && field.Cardinality != 1) {
		return true
	}
	switch field.Hub {
	case "ObjectModel", "ResourceType", "Departments", "Genre", "PhysicalForm", "Rights":
		return true
	}
	if strings.HasPrefix(field.Hub, "Subjects.") || strings.HasPrefix(field.Hub, "Contributors.") {
		return true
	}
	sourceType := strings.ToLower(field.SourceType)
	return strings.Contains(sourceType, "entity_reference") || strings.Contains(sourceType, "taxonomy")
}

type workbenchLayout struct {
	columns   map[string]int
	dataStart int
	separator string
}

func newWorkbenchLayout(rows [][]string, transformation *spec.Transformation) workbenchLayout {
	layout := workbenchLayout{columns: make(map[string]int), dataStart: 1, separator: " ; "}
	if len(rows) == 0 || transformation == nil {
		layout.dataStart = len(rows)
		return layout
	}
	if transformation.Source.MultiValueSeparator != "" {
		layout.separator = transformation.Source.MultiValueSeparator
	}
	if len(rows) > 1 && transformation.Source.HeaderRows > 1 &&
		matchingHeaders(rows[0], transformation) > 0 && isLikelyAdditionalHeader(rows[1], transformation) {
		layout.dataStart = 2
	}
	for headerRow := 0; headerRow < layout.dataStart && headerRow < len(rows); headerRow++ {
		for column, header := range rows[headerRow] {
			field, ok := transformation.SourceField(header)
			if !ok {
				continue
			}
			key := strings.ToLower(field.Name)
			if _, exists := layout.columns[key]; !exists {
				layout.columns[key] = column
			}
		}
	}
	return layout
}

func (l workbenchLayout) has(field string) bool {
	_, ok := l.columns[strings.ToLower(field)]
	return ok
}

func (l workbenchLayout) value(row []string, field string) string {
	column, ok := l.columns[strings.ToLower(field)]
	if !ok || column >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[column])
}

func (l workbenchLayout) values(row []string, field string) []string {
	raw := l.value(row, field)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, l.separator)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values
}

func (l workbenchLayout) add(result CheckResult, rowIndex int, field, message string) {
	column, ok := l.columns[strings.ToLower(field)]
	key := "row" + strconv.Itoa(rowIndex+1)
	if ok {
		key = excelColumn(column+1) + strconv.Itoa(rowIndex+1)
	}
	appendCheckMessage(result, key, message)
}

func (l workbenchLayout) addFirst(result CheckResult, rowIndex int, fields []string, message string) {
	for _, field := range fields {
		if l.has(field) {
			l.add(result, rowIndex, field, message)
			return
		}
	}
	l.add(result, rowIndex, fields[0], message)
}

func emptySpreadsheetRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func validAbsoluteHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func validRightsStatement(value string) bool {
	value = strings.TrimSpace(value)
	for code, label := range hub.RightsStatementLabels {
		if strings.EqualFold(value, label) || value == hub.RightsStatements[code] ||
			strings.Replace(value, "https://", "http://", 1) == hub.RightsStatements[code] {
			return true
		}
	}
	return false
}

var workbenchMediaExtensions = map[string]map[string]struct{}{
	"image":    setOf("png", "gif", "jpg", "jpeg"),
	"document": setOf("doc", "docx", "pdf", "ppt", "pptx", "xls", "xlsx"),
	"file":     setOf("aux", "csv", "dat", "dbf", "ipynb", "hocr", "html", "jp2", "log", "lyr", "mxd", "numbers", "pages", "prj", "psd", "py", "rrd", "rtf", "sbn", "sbx", "sdw", "shp", "shx", "sid", "text", "tfw", "tif", "tiff", "txt", "vtt", "warc", "xml", "zip"),
	"audio":    setOf("mp3", "wav", "aac", "flac", "m4a"),
	"video":    setOf("mp4", "mov", "wmv", "avi", "mts", "flv", "f4v", "swf", "mkv", "webm", "ogv", "mpeg", "m4v", "dv"),
}

func setOf(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func workbenchMediaType(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "audio":
		return "audio"
	case "digital document":
		return "document"
	case "video":
		return "video"
	case "image":
		return "image"
	default:
		return "file"
	}
}

func allowedWorkbenchExtension(name, model string) bool {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(name))), ".")
	if extension == "" {
		return false
	}
	_, ok := workbenchMediaExtensions[workbenchMediaType(model)][extension]
	return ok
}
