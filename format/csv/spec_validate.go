package csv

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

// specRowView resolves cells through canonical mapping fields. Validation rule
// implementations never inspect human-facing spreadsheet labels.
type specRowView struct {
	row       []string
	columns   map[string]specColumn
	separator string
}

func newSpecRowView(row []string, columns []specColumn, separator string) specRowView {
	resolved := make(map[string]specColumn, len(columns))
	for _, column := range columns {
		resolved[strings.ToLower(column.field.Name)] = column
	}
	return specRowView{row: row, columns: resolved, separator: separator}
}

func (v specRowView) column(name string) (specColumn, bool) {
	column, ok := v.columns[strings.ToLower(name)]
	return column, ok
}

func (v specRowView) raw(name string) string {
	column, ok := v.column(name)
	if !ok || column.index < 0 || column.index >= len(v.row) {
		return ""
	}
	value := strings.TrimSpace(v.row[column.index])
	if value == "" {
		value = column.field.Default
	}
	return strings.TrimSpace(value)
}

func (v specRowView) values(name string) []string {
	column, ok := v.column(name)
	if !ok {
		return nil
	}
	value := v.raw(name)
	if value == "" {
		return nil
	}
	return specValues(value, column.field, v.separator)
}

func (v specRowView) operation() spec.Operation {
	hasNodeID := false
	hasUpdateMetadata := false
	hasPrimaryFile := false
	for _, column := range v.columns {
		if v.raw(column.field.Name) == "" || column.field.Codec == "ignore" {
			continue
		}
		switch column.field.Hub {
		case "Extra.node_id":
			hasNodeID = true
		case "Files.primary":
			hasPrimaryFile = true
		case "Extra.id", "Extra.parent_id":
			// Batch identity and hierarchy columns are transport metadata, not
			// node metadata updates.
		default:
			if column.field.AppliesTo(spec.OperationUpdate) {
				hasUpdateMetadata = true
			}
		}
	}
	if !hasNodeID {
		return spec.OperationCreate
	}
	if hasUpdateMetadata {
		return spec.OperationUpdate
	}
	if hasPrimaryFile {
		return spec.OperationAddMedia
	}
	return spec.OperationUpdate
}

func (v specRowView) predicateMatches(when *spec.ValidationWhen) bool {
	if when == nil {
		return true
	}
	switch when.Operator {
	case spec.ValidationOperatorIn:
		value := v.raw(when.Field)
		for _, candidate := range when.Values {
			if value == candidate {
				return true
			}
		}
		return false
	case spec.ValidationOperatorValueCountGreaterThan:
		return len(v.values(when.Field)) > when.Count
	default:
		return false
	}
}

func validateSpecTableRows(rows [][]string, dataStart int, columns []specColumn, opts *format.ParseOptions) []format.Diagnostic {
	type rowState struct {
		number    int
		view      specRowView
		operation spec.Operation
	}
	states := make([]rowState, 0, len(rows)-dataStart)
	for index := dataStart; index < len(rows); index++ {
		if emptyRow(rows[index]) || len(rows[index]) != len(rows[0]) {
			continue
		}
		view := newSpecRowView(rows[index], columns, opts.Spec.Source.MultiValueSeparator)
		states = append(states, rowState{number: index + 1, view: view, operation: view.operation()})
	}

	diagnostics := make([]format.Diagnostic, 0)
	for _, validation := range opts.Spec.Source.Validations {
		if validation.IsContext() {
			continue
		}
		switch validation.Rule {
		case spec.ValidationUnique:
			seen := make(map[string]int)
			for _, state := range states {
				if !validation.AppliesTo(state.operation) || !state.view.predicateMatches(validation.When) {
					continue
				}
				value := state.view.raw(validation.Fields[0])
				if value == "" {
					continue
				}
				if first, exists := seen[value]; exists {
					diagnostics = append(diagnostics, specRuleDiagnostic(opts, state.number, state.view, validation.Fields[0], "unique",
						fmt.Sprintf("Duplicate %s; first appears on row %d", specFieldDescription(opts.Spec, validation.Fields[0]), first)))
					continue
				}
				seen[value] = state.number
			}
		case spec.ValidationReference:
			targets := make(map[string]struct{})
			for _, state := range states {
				if value := state.view.raw(validation.Fields[1]); value != "" {
					targets[value] = struct{}{}
				}
			}
			for _, state := range states {
				if !validation.AppliesTo(state.operation) || !state.view.predicateMatches(validation.When) {
					continue
				}
				value := state.view.raw(validation.Fields[0])
				if value == "" {
					continue
				}
				if _, exists := targets[value]; !exists {
					diagnostics = append(diagnostics, specRuleDiagnostic(opts, state.number, state.view, validation.Fields[0], "reference",
						fmt.Sprintf("does not reference a value present in %s", validation.Fields[1])))
				}
			}
		case spec.ValidationPrecedingReference:
			preceding := make(map[string]struct{})
			for _, state := range states {
				if validation.AppliesTo(state.operation) && state.view.predicateMatches(validation.When) {
					value := state.view.raw(validation.Fields[0])
					if value != "" {
						if _, exists := preceding[value]; !exists {
							diagnostics = append(diagnostics, specRuleDiagnostic(opts, state.number, state.view, validation.Fields[0], "preceding_reference",
								fmt.Sprintf("must reference an earlier value in %s", validation.Fields[1])))
						}
					}
				}
				if target := state.view.raw(validation.Fields[1]); target != "" {
					preceding[target] = struct{}{}
				}
			}
		case spec.ValidationDifferent:
			for _, state := range states {
				if !validation.AppliesTo(state.operation) || !state.view.predicateMatches(validation.When) {
					continue
				}
				left, right := state.view.raw(validation.Fields[0]), state.view.raw(validation.Fields[1])
				if left != "" && left == right {
					diagnostics = append(diagnostics, specRuleDiagnostic(opts, state.number, state.view, validation.Fields[0], "different",
						fmt.Sprintf("must differ from %s", validation.Fields[1])))
				}
			}
		case spec.ValidationRequiredAny:
			for _, state := range states {
				if !validation.AppliesTo(state.operation) || !state.view.predicateMatches(validation.When) {
					continue
				}
				present := false
				for _, name := range validation.Fields {
					if state.view.raw(name) != "" {
						present = true
						break
					}
				}
				if !present {
					diagnostics = append(diagnostics, specRuleDiagnostic(opts, state.number, state.view, firstDeclaredField(state.view, validation.Fields), "required_any",
						"at least one of "+strings.Join(validation.Fields, ", ")+" is required"))
				}
			}
		case spec.ValidationRequiredWhen:
			for _, state := range states {
				if !validation.AppliesTo(state.operation) || !state.view.predicateMatches(validation.When) || state.view.raw(validation.Fields[0]) != "" {
					continue
				}
				// Point at the trigger when possible: that is the cell users can
				// edit when the required field is absent from their sheet.
				location := validation.When.Field
				if _, ok := state.view.column(location); !ok {
					location = validation.Fields[0]
				}
				diagnostics = append(diagnostics, specRuleDiagnostic(opts, state.number, state.view, location, "required_when",
					fmt.Sprintf("%s is required by this value", specFieldDescription(opts.Spec, validation.Fields[0]))))
			}
		}
	}
	return diagnostics
}

func firstDeclaredField(view specRowView, fields []string) string {
	for _, name := range fields {
		if _, ok := view.column(name); ok {
			return name
		}
	}
	return fields[0]
}

func specRuleDiagnostic(opts *format.ParseOptions, row int, view specRowView, field, code, message string) format.Diagnostic {
	if column, ok := view.column(field); ok {
		return cellDiagnostic(opts, row, column, code, message)
	}
	return format.Diagnostic{Source: opts.SourceName, Row: row, Header: field, Code: code, Message: message}
}

func specFieldDescription(transformation *spec.Transformation, name string) string {
	field, ok := transformation.SourceField(name)
	if !ok {
		return name
	}
	description := preferredFieldLabel(field)
	if description == "" {
		return name
	}
	return description
}

func validateSpecRow(row []string, rowNumber int, columns []specColumn, record *hubv1.Record, operation spec.Operation, opts *format.ParseOptions) []format.Diagnostic {
	view := newSpecRowView(row, columns, opts.Spec.Source.MultiValueSeparator)
	diagnostics := make([]format.Diagnostic, 0)
	for _, column := range columns {
		raw := view.raw(column.field.Name)
		if raw == "" {
			continue
		}
		values := view.values(column.field.Name)
		for _, validation := range column.field.Validations {
			if validation.IsContext() || !validation.AppliesTo(operation) || !view.predicateMatches(validation.When) {
				continue
			}
			message := validateSpecFieldRule(validation, raw, values, column.field, view, record, opts.ValueProfile)
			if message != "" {
				diagnostics = append(diagnostics, cellDiagnostic(opts, rowNumber, column, string(validation.Rule), message))
			}
		}
	}
	return diagnostics
}

func validateSpecFieldRule(validation spec.Validation, raw string, values []string, field spec.Field, view specRowView, record *hubv1.Record, valueProfile *profile.Compiled) string {
	switch validation.Rule {
	case spec.ValidationMaximumRunes:
		for _, value := range values {
			if utf8.RuneCountInString(value) > validation.Limit {
				return fmt.Sprintf("value is longer than %d characters", validation.Limit)
			}
		}
	case spec.ValidationAbsoluteHTTPURL:
		for _, value := range values {
			if !validSpecHTTPURL(value) {
				return "Invalid URL; must be an absolute HTTP or HTTPS URL"
			}
		}
	case spec.ValidationWorkbenchLink:
		for _, value := range values {
			if !validSpecWorkbenchLink(value) {
				return "Invalid Workbench link; expected an absolute HTTP or HTTPS URL with an optional %% label"
			}
		}
	case spec.ValidationGeolocation:
		for _, value := range values {
			if !validSpecGeolocation(value) {
				return "Invalid geolocation; expected latitude,longitude within geographic bounds"
			}
		}
	case spec.ValidationAuthorityLink:
		for _, value := range values {
			if !validSpecAuthorityLink(value, validation.Values) {
				return "Invalid authority link; expected an allowed source, absolute HTTP(S) URI, and optional title separated by %%"
			}
		}
	case spec.ValidationMediaTrack:
		for _, value := range values {
			if !validSpecMediaTrack(value) {
				return "Invalid media track; expected label:kind:language:file.vtt"
			}
		}
	case spec.ValidationEntityReference:
		for _, value := range values {
			if !validSpecEntityReference(value, validation.EntityType, validation.Bundles) {
				return "Invalid entity reference for the mapping-declared target bundles"
			}
		}
	case spec.ValidationTypedRelation:
		for _, value := range values {
			if !validSpecTypedRelation(value, validation.Values, validation.EntityType, validation.Bundles) {
				return "Invalid typed relation for the mapping-declared relators and target bundles"
			}
		}
	case spec.ValidationDOI:
		for _, value := range values {
			if !validSpecDOI(value) {
				return "Invalid DOI"
			}
		}
	case spec.ValidationRightsStatement:
		for _, value := range values {
			if !validSpecRightsStatement(value) {
				return "Invalid Rights Statement"
			}
		}
	case spec.ValidationNoLineBreaks:
		if strings.ContainsAny(raw, "\r\n") {
			return "Line breaks are not allowed in this field"
		}
	case spec.ValidationEnum:
		for _, value := range values {
			if !matchesSpecEnum(value, validation.Values, validation.CaseInsensitive) {
				if len(validation.Values) == 2 {
					return "Invalid value. Must be " + validation.Values[0] + " or " + validation.Values[1]
				}
				return "Invalid value. Must be one of: " + strings.Join(validation.Values, ", ")
			}
		}
	case spec.ValidationFiniteNumber:
		for _, value := range values {
			number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
				return "Invalid numeric value"
			}
		}
	case spec.ValidationNumericRange:
		for _, value := range values {
			number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
				return "Invalid numeric value"
			}
			if validation.Minimum != nil && number < *validation.Minimum {
				return fmt.Sprintf("Value must be at least %v", *validation.Minimum)
			}
			if validation.Maximum != nil && number > *validation.Maximum {
				return fmt.Sprintf("Value must be at most %v", *validation.Maximum)
			}
		}
	case spec.ValidationPattern:
		pattern, err := regexp.Compile("^(?:" + validation.Pattern + ")$")
		if err != nil {
			return "Invalid mapping-declared pattern"
		}
		for _, value := range values {
			candidate := value
			if field.ProfileRule != "" && valueProfile != nil {
				identifier, identifierErr := valueProfile.NewIdentifier(field.ProfileRule, value)
				if identifierErr != nil {
					return "Value does not satisfy profile identifier rule " + field.ProfileRule
				}
				candidate = identifier.GetValue()
			}
			if !pattern.MatchString(candidate) {
				return "Value does not match the mapping-declared pattern"
			}
		}
	case spec.ValidationNotFutureTimestamp:
		for _, value := range values {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
			if err != nil {
				return "Invalid RFC 3339 timestamp"
			}
			if parsed.After(time.Now()) {
				return "Timestamp must not be in the future"
			}
		}
	case spec.ValidationMediaExtension:
		for _, value := range values {
			mediaType, extension, allowed := allowedSpecMediaExtension(value, validation.MediaTypes)
			if !allowed {
				if extension == "" {
					return "File name has no extension for mapping-declared media type validation"
				}
				return fmt.Sprintf("File extension .%s is not allowed for selected media type %s", extension, mediaType)
			}
		}
	case spec.ValidationContributor:
		for _, encoded := range values {
			if strings.HasPrefix(strings.TrimSpace(encoded), "{") {
				var value map[string]any
				if err := json.Unmarshal([]byte(encoded), &value); err != nil {
					return "Invalid contributor JSON"
				}
			}
		}
		if message := validateSpecContributors(record.GetContributors(), validation.Values); message != "" {
			return message
		}
	case spec.ValidationGettyTGN:
		for _, value := range values {
			if !validSpecGettyTGN(value) {
				return "Invalid Getty TGN URI"
			}
		}
	}
	return ""
}

func validSpecHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.User == nil
}

func validSpecWorkbenchLink(value string) bool {
	urlValue, _, _ := strings.Cut(strings.TrimSpace(value), "%%")
	return validSpecHTTPURL(urlValue)
}

func validSpecGeolocation(value string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), `\`)
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return false
	}
	latitude, latErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	longitude, longErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	return latErr == nil && longErr == nil && !math.IsNaN(latitude) && !math.IsInf(latitude, 0) &&
		!math.IsNaN(longitude) && !math.IsInf(longitude, 0) &&
		latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}

func validSpecAuthorityLink(value string, sources []string) bool {
	parts := strings.SplitN(strings.TrimSpace(value), "%%", 3)
	return len(parts) >= 2 && matchesSpecEnum(parts[0], sources, false) && validSpecHTTPURL(parts[1])
}

var specLanguageCodePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$`)
var specTypedRelationPrefixPattern = regexp.MustCompile(`^[0-9A-Za-z]+:[0-9A-Za-z]+$`)

func validSpecMediaTrack(value string) bool {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 4)
	if len(parts) != 4 || strings.TrimSpace(parts[0]) == "" || !specLanguageCodePattern.MatchString(parts[2]) ||
		!strings.HasSuffix(strings.ToLower(strings.TrimSpace(parts[3])), ".vtt") {
		return false
	}
	switch parts[1] {
	case "subtitles", "descriptions", "metadata", "captions", "chapters":
		return true
	default:
		return false
	}
}

func validSpecEntityReference(value, entityType string, bundles []string) bool {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > 255 {
		return false
	}
	if _, err := strconv.ParseUint(value, 10, 64); err == nil {
		return true
	}
	if entityType != "" && entityType != "taxonomy_term" {
		return false
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return validSpecHTTPURL(value)
	}
	namespace, term, namespaced := strings.Cut(value, ":")
	if len(bundles) > 1 {
		return namespaced && term != "" && matchesSpecEnum(namespace, bundles, false)
	}
	if namespaced && len(bundles) == 1 {
		if namespace == bundles[0] {
			return term != ""
		}
		// Workbench treats a colon as ordinary term-name text when the field
		// targets only one vocabulary. Only the exact configured prefix is a
		// namespace in that case.
		return true
	}
	return true
}

func validSpecTypedRelation(value string, relators []string, entityType string, bundles []string) bool {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 3)
	if len(parts) != 3 || !specTypedRelationPrefixPattern.MatchString(parts[0]+":"+parts[1]) {
		return false
	}
	relator := parts[0] + ":" + parts[1]
	return (len(relators) == 0 || matchesSpecEnum(relator, relators, false)) && validSpecEntityReference(parts[2], entityType, bundles)
}

var specDOIPattern = regexp.MustCompile(`(?i)^10\.\d{4,9}/\S+$`)

func validSpecDOI(value string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"} {
		if strings.HasPrefix(lower, prefix) {
			value = strings.TrimSpace(value[len(prefix):])
			break
		}
	}
	return specDOIPattern.MatchString(value)
}

func validSpecRightsStatement(value string) bool {
	value = strings.TrimSpace(value)
	for code, label := range hub.RightsStatementLabels {
		if strings.EqualFold(value, label) || value == hub.RightsStatements[code] || strings.Replace(value, "https://", "http://", 1) == hub.RightsStatements[code] {
			return true
		}
	}
	return false
}

func matchesSpecEnum(value string, allowed []string, caseInsensitive bool) bool {
	for _, candidate := range allowed {
		if value == candidate || (caseInsensitive && strings.EqualFold(value, candidate)) {
			return true
		}
	}
	return false
}

func allowedSpecMediaExtension(name string, policies []spec.MediaExtensionPolicy) (string, string, bool) {
	extension := specFileExtension(name)
	selected := ""
	for _, policy := range policies {
		if policy.Fallback {
			selected = policy.MediaType
			break
		}
	}
	for _, policy := range policies {
		if matchesSpecEnum(extension, policy.SelectExtensions, false) {
			selected = policy.MediaType
			break
		}
	}
	for _, policy := range policies {
		if policy.MediaType == selected {
			return selected, extension, extension != "" && matchesSpecEnum(extension, policy.AllowedExtensions, false)
		}
	}
	return selected, extension, false
}

func specFileExtension(name string) string {
	name = strings.TrimSpace(name)
	if parsed, err := url.ParseRequestURI(name); err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		name = parsed.Path
	}
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
}

func validateSpecContributors(contributors []*hubv1.Contributor, allowedRelators []string) string {
	for index, contributor := range contributors {
		if contributor == nil || strings.TrimSpace(contributor.GetName()) == "" {
			return fmt.Sprintf("Contributor %d must have a name", index+1)
		}
		if role := strings.TrimSpace(contributor.GetRoleCode()); role != "" {
			if !strings.HasPrefix(role, "relators:") {
				return fmt.Sprintf("Contributor %d has an invalid relator code", index+1)
			}
			if !specRelatorPattern.MatchString(role) {
				return fmt.Sprintf("Contributor %d has an unsupported relator code", index+1)
			}
			if len(allowedRelators) != 0 && !matchesSpecEnum(role, allowedRelators, false) {
				return fmt.Sprintf("Contributor %d relator code is not configured for this field", index+1)
			}
		}
		if contributor.GetType() != hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON &&
			(contributor.GetStatus() != "" || contributor.GetEmail() != "" || len(contributor.GetAffiliations()) > 0 || hasSpecORCID(contributor)) {
			return fmt.Sprintf("Contributor %d person-only metadata requires contributor type person", index+1)
		}
	}
	return ""
}

func hasSpecORCID(contributor *hubv1.Contributor) bool {
	for _, identifier := range contributor.GetIdentifiers() {
		if identifier.GetType() == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID {
			return true
		}
	}
	return false
}

var specGettyTGNPattern = regexp.MustCompile(`^https?://vocab\.getty\.edu/(?:page/)?tgn/[0-9]+/?$`)
var specRelatorPattern = regexp.MustCompile(`^relators:[a-z0-9]{3}$`)

func validSpecGettyTGN(value string) bool {
	return specGettyTGNPattern.MatchString(strings.TrimSpace(value))
}
