// Package spoke provides proto parsing for existing annotations.
package spoke

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// ExistingMapping represents an existing hub.v1 annotation from a proto file.
type ExistingMapping struct {
	FieldName   string
	Target      string
	DateType    string
	IDType      string
	SubjectVoc  string
	Role        string
	RelType     string
	Parser      string
	Validators  string
	Description string
	Required    bool
}

// ParseExistingProto reads an existing proto file and extracts hub.v1.field annotations.
// Returns a map from field name to its existing mapping.
func ParseExistingProto(protoPath string) (map[string]ExistingMapping, error) {
	file, err := os.Open(protoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]ExistingMapping), nil
		}
		return nil, err
	}
	defer file.Close()

	mappings := make(map[string]ExistingMapping)
	scanner := bufio.NewScanner(file)

	// Patterns for extracting field info
	fieldPattern := regexp.MustCompile(`^\s*(repeated\s+)?(\w+)\s+(\w+)\s*=\s*\d+`)
	optionPattern := regexp.MustCompile(`\[\(hub\.v1\.field\)\s*=\s*\{([^}]+)\}\]`)

	for scanner.Scan() {
		line := scanner.Text()

		// Check if this is a field definition with hub.v1.field annotation
		fieldMatch := fieldPattern.FindStringSubmatch(line)
		if fieldMatch == nil {
			continue
		}

		fieldName := fieldMatch[3]
		optionMatch := optionPattern.FindStringSubmatch(line)
		if optionMatch == nil {
			continue
		}

		// Parse the options
		mapping := ExistingMapping{FieldName: fieldName}
		optionStr := optionMatch[1]

		// Extract individual options
		mapping.Target = extractOption(optionStr, "target")
		mapping.DateType = extractOption(optionStr, "date_type")
		mapping.IDType = extractOption(optionStr, "identifier_type")
		mapping.SubjectVoc = extractOption(optionStr, "subject_vocabulary")
		mapping.Role = extractOption(optionStr, "role")
		mapping.RelType = extractOption(optionStr, "relation_type")
		mapping.Parser = extractOption(optionStr, "parser")
		mapping.Validators = extractOption(optionStr, "validators")
		mapping.Description = extractOption(optionStr, "description")
		mapping.Required = strings.Contains(optionStr, "required: true")

		mappings[fieldName] = mapping
	}

	return mappings, scanner.Err()
}

// extractOption extracts a single option value from the options string.
func extractOption(optionStr, name string) string {
	// Match: name: "value" or name: 'value'
	pattern := regexp.MustCompile(name + `:\s*["']([^"']+)["']`)
	match := pattern.FindStringSubmatch(optionStr)
	if match != nil {
		return match[1]
	}
	return ""
}

// HubTargetType represents a type of Hub target field.
type HubTargetType int

const (
	TargetSimple HubTargetType = iota
	TargetDate
	TargetIdentifier
	TargetContributor
	TargetSubject
	TargetRelation
	TargetExtra
	TargetSkip
)

// HubTarget describes a potential Hub mapping target.
type HubTarget struct {
	Name        string
	Type        HubTargetType
	Description string
}

// GetHubTargets returns the available Hub targets for mapping.
func GetHubTargets() []HubTarget {
	return []HubTarget{
		// Simple text fields
		{Name: "title", Type: TargetSimple, Description: "Primary title of the work"},
		{Name: "abstract", Type: TargetSimple, Description: "Summary or abstract"},
		{Name: "publisher", Type: TargetSimple, Description: "Publisher name"},
		{Name: "place_published", Type: TargetSimple, Description: "Place of publication"},
		{Name: "language", Type: TargetSimple, Description: "Language of the work"},
		{Name: "resource_type", Type: TargetSimple, Description: "Type of resource (article, book, etc.)"},
		{Name: "notes", Type: TargetSimple, Description: "General notes"},
		{Name: "rights", Type: TargetSimple, Description: "Rights/license information"},

		// Complex typed fields
		{Name: "contributors", Type: TargetContributor, Description: "Authors, editors, and other contributors"},
		{Name: "dates", Type: TargetDate, Description: "Publication, creation, and other dates"},
		{Name: "identifiers", Type: TargetIdentifier, Description: "DOIs, ISBNs, and other identifiers"},
		{Name: "subjects", Type: TargetSubject, Description: "Topics, keywords, and classifications"},
		{Name: "relations", Type: TargetRelation, Description: "Related items and collections"},

		// Degree info
		{Name: "degree_info", Type: TargetSimple, Description: "Thesis/dissertation degree information"},

		// Extension field
		{Name: "extra", Type: TargetExtra, Description: "Additional metadata not in Hub schema"},

		// Skip
		{Name: "(skip)", Type: TargetSkip, Description: "Do not map this field to Hub"},
	}
}

// GetDateTypes returns the available date types.
func GetDateTypes() []string {
	return []string{
		"issued",
		"created",
		"captured",
		"copyright",
		"modified",
		"valid",
		"available",
		"submitted",
		"accepted",
		"other",
	}
}

// GetIdentifierTypes returns the available identifier types.
func GetIdentifierTypes() []string {
	return []string{
		"doi",
		"isbn",
		"issn",
		"orcid",
		"handle",
		"url",
		"urn",
		"ark",
		"pmid",
		"pmcid",
		"oclc",
		"local",
		"other",
	}
}

// GetContributorRoles returns common contributor roles.
func GetContributorRoles() []string {
	return []string{
		"author",
		"editor",
		"translator",
		"compiler",
		"illustrator",
		"contributor",
		"advisor",
		"committee_member",
		"creator",
		"other",
	}
}

// GetSubjectVocabularies returns common subject vocabularies.
func GetSubjectVocabularies() []string {
	return []string{
		"lcsh",
		"mesh",
		"fast",
		"aat",
		"tgm",
		"keywords",
		"genre",
		"geographic",
		"temporal",
		"name",
		"local",
		"other",
	}
}

// GetRelationTypes returns common relation types.
func GetRelationTypes() []string {
	return []string{
		"member_of",
		"part_of",
		"version_of",
		"replaces",
		"replaced_by",
		"references",
		"referenced_by",
		"requires",
		"is_format_of",
		"has_format",
		"related_to",
		"other",
	}
}

// GetParsers returns available field parsers.
func GetParsers() []string {
	return []string{
		"passthrough",
		"strip_html",
		"normalize_whitespace",
		"edtf",
		"iso8601",
		"year",
		"doi",
		"isbn",
		"orcid",
		"url",
		"relator",
		"split",
		"custom",
	}
}
