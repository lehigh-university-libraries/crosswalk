// Package spoke provides tools for generating protobuf spoke schemas from source configurations.
package spoke

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// DrupalFieldConfig represents a Drupal field configuration from config/sync.
type DrupalFieldConfig struct {
	UUID        string         `yaml:"uuid"`
	ID          string         `yaml:"id"`
	FieldName   string         `yaml:"field_name"`
	EntityType  string         `yaml:"entity_type"`
	Bundle      string         `yaml:"bundle"`
	Label       string         `yaml:"label"`
	Description string         `yaml:"description"`
	Required    bool           `yaml:"required"`
	FieldType   string         `yaml:"field_type"`
	Settings    map[string]any `yaml:"settings"`
}

// DrupalFieldStorage represents field storage config (has cardinality).
type DrupalFieldStorage struct {
	UUID        string         `yaml:"uuid"`
	ID          string         `yaml:"id"`
	FieldName   string         `yaml:"field_name"`
	EntityType  string         `yaml:"entity_type"`
	FieldType   string         `yaml:"type"`
	Cardinality int            `yaml:"cardinality"` // -1 = unlimited
	Settings    map[string]any `yaml:"settings"`
}

// DrupalRDFMapping represents an RDF mapping configuration.
type DrupalRDFMapping struct {
	ID               string                     `yaml:"id"`
	TargetEntityType string                     `yaml:"targetEntityType"`
	Bundle           string                     `yaml:"bundle"`
	Types            []string                   `yaml:"types"`
	FieldMappings    map[string]RDFFieldMapping `yaml:"fieldMappings"`
}

// RDFFieldMapping represents the RDF mapping for a single field.
type RDFFieldMapping struct {
	Properties  []string `yaml:"properties"`
	MappingType string   `yaml:"mapping_type,omitempty"`
}

// ProtoField represents a field in the generated proto.
type ProtoField struct {
	Name         string // Proto field name (snake_case)
	Type         string // Proto type (string, int64, repeated string, etc.)
	Number       int    // Field number
	Comment      string // Field comment
	DrupalField  string // Original Drupal field name
	DrupalType   string // Original Drupal field type
	Cardinality  int    // -1 = unlimited, 1 = single, N = max N
	TargetType   string // For entity_reference: target entity type
	TargetBundle string // For entity_reference: target bundle (if restricted)
	RDFPredicate string // RDF predicate from Drupal RDF mapping (e.g., "dcterms:issued")
	HubField     string // Hub schema field mapping (e.g., "Contributors", "Dates")
	HubType      string // Hub field type hint (e.g., "date_issued", "doi")
	Parser       string // Parser to use (e.g., "edtf")

	// Hub mapping options (populated by interactive mode)
	HubTarget     string // Hub target field (e.g., "title", "contributors", "dates")
	HubDateType   string // For dates: issued, created, captured, etc.
	HubIDType     string // For identifiers: doi, isbn, etc.
	HubRole       string // For contributors: author, editor, etc.
	HubSubjectVoc string // For subjects: lcsh, keywords, etc.
	HubRelType    string // For relations: member_of, part_of, etc.
	HubExtraKey   string // For extra fields: the key name
	HubSkip       bool   // Whether to skip this field in Hub mapping
}

// ProtoMessage represents a message in the generated proto.
type ProtoMessage struct {
	Name    string       // Message name (PascalCase)
	Comment string       // Message comment
	Fields  []ProtoField // Fields in order
}

// ProtoFile represents a complete proto file.
type ProtoFile struct {
	Package       string         // Proto package (e.g., "islandora.v1")
	GoPackage     string         // Go package path
	PackageName   string         // Short package name for generated Go code
	FormatName    string         // Format name used by the convert command (e.g., "drupal", "islandora-workbench")
	Messages      []ProtoMessage // All messages
	Enums         []ProtoEnum    // All enums
	Description   string         // File description
	UseHubOptions bool           // Whether to include hub.v1 annotations
}

// ProtoEnum represents an enum in the proto.
type ProtoEnum struct {
	Name    string
	Comment string
	Values  []ProtoEnumValue
}

// ProtoEnumValue represents an enum value.
type ProtoEnumValue struct {
	Name   string
	Number int
}

// GenerateDrupalSpoke generates a proto file from a Drupal config directory.
func GenerateDrupalSpoke(name, bundle, configPath string) (*ProtoFile, error) {
	// Verify path exists
	info, err := os.Stat(configPath)
	if err != nil {
		return nil, fmt.Errorf("config path not accessible: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("config path is not a directory: %s", configPath)
	}

	// Parse field configs for the specific bundle
	fields, err := parseFieldConfigs(configPath, bundle)
	if err != nil {
		return nil, fmt.Errorf("parsing field configs: %w", err)
	}

	// Parse field storage for cardinality
	storage, err := parseFieldStorage(configPath)
	if err != nil {
		return nil, fmt.Errorf("parsing field storage: %w", err)
	}

	// Parse RDF mapping if available
	rdfMapping, err := parseRDFMapping(configPath, "node", bundle)
	if err != nil {
		// RDF mapping is optional, just log and continue
		fmt.Fprintf(os.Stderr, "Note: No RDF mapping found for %s.%s, using field name heuristics\n", "node", bundle)
	}

	// Build storage map for quick lookup
	storageMap := make(map[string]DrupalFieldStorage)
	for _, s := range storage {
		storageMap[s.FieldName] = s
	}

	// Proto identifiers cannot contain hyphens; use underscores in package names.
	protoName := strings.ReplaceAll(name, "-", "_")

	// Generate proto
	proto := &ProtoFile{
		Package:     fmt.Sprintf("spoke.%s.v1", protoName),
		GoPackage:   fmt.Sprintf("github.com/lehigh-university-libraries/crosswalk/spoke/%s/v1;%sv1", name, protoName),
		PackageName: fmt.Sprintf("%sv1", protoName),
		Description: fmt.Sprintf("Generated from Drupal bundle '%s'", bundle),
	}

	// Create main message
	mainMsg := ProtoMessage{
		Name:    toPascalCase(bundle),
		Comment: fmt.Sprintf("%s represents a %s from Drupal.", toPascalCase(bundle), bundle),
	}

	// Add core Drupal fields
	fieldNum := 1
	coreFields := []ProtoField{
		{Name: "nid", Type: "int64", Number: fieldNum, Comment: "Drupal node ID"},
		{Name: "uuid", Type: "string", Number: fieldNum + 1, Comment: "Drupal UUID"},
		{Name: "title", Type: "string", Number: fieldNum + 2, Comment: "Node title"},
		{Name: "status", Type: "string", Number: fieldNum + 3, Comment: "Published status"},
		{Name: "created", Type: "string", Number: fieldNum + 4, Comment: "Created timestamp"},
		{Name: "changed", Type: "string", Number: fieldNum + 5, Comment: "Changed timestamp"},
	}
	mainMsg.Fields = append(mainMsg.Fields, coreFields...)
	fieldNum = 10 // Start custom fields at 10

	// Sort fields by name for consistent output
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].FieldName < fields[j].FieldName
	})

	// Group fields by category for better field number organization
	categories := categorizeFields(fields)

	for _, category := range categories {
		// Add a comment for the category
		for _, field := range category.Fields {
			stor := storageMap[field.FieldName]
			protoField := drupalFieldToProto(field, stor, rdfMapping, fieldNum)
			mainMsg.Fields = append(mainMsg.Fields, protoField)
			fieldNum++
		}
		// Add gap between categories
		fieldNum = ((fieldNum / 10) + 1) * 10
	}

	proto.Messages = append(proto.Messages, mainMsg)

	// Add helper messages for complex types
	proto.Messages = append(proto.Messages, generateHelperMessages()...)

	return proto, nil
}

type fieldCategory struct {
	Name   string
	Fields []DrupalFieldConfig
}

func categorizeFields(fields []DrupalFieldConfig) []fieldCategory {
	categories := map[string][]DrupalFieldConfig{
		"identifiers":  {},
		"titles":       {},
		"descriptions": {},
		"dates":        {},
		"subjects":     {},
		"agents":       {},
		"publication":  {},
		"other":        {},
	}

	for _, f := range fields {
		name := f.FieldName
		switch {
		case strings.Contains(name, "identifier") || strings.Contains(name, "pid") ||
			strings.Contains(name, "isbn") || strings.Contains(name, "oclc"):
			categories["identifiers"] = append(categories["identifiers"], f)
		case strings.Contains(name, "title"):
			categories["titles"] = append(categories["titles"], f)
		case strings.Contains(name, "description") || strings.Contains(name, "abstract") ||
			strings.Contains(name, "note") || strings.Contains(name, "extent"):
			categories["descriptions"] = append(categories["descriptions"], f)
		case strings.Contains(name, "date") || strings.Contains(name, "edtf"):
			categories["dates"] = append(categories["dates"], f)
		case strings.Contains(name, "subject") || strings.Contains(name, "genre") ||
			strings.Contains(name, "topic") || strings.Contains(name, "geographic"):
			categories["subjects"] = append(categories["subjects"], f)
		case strings.Contains(name, "agent") || strings.Contains(name, "contributor") ||
			strings.Contains(name, "creator") || strings.Contains(name, "author"):
			categories["agents"] = append(categories["agents"], f)
		case strings.Contains(name, "publisher") || strings.Contains(name, "place_published"):
			categories["publication"] = append(categories["publication"], f)
		default:
			categories["other"] = append(categories["other"], f)
		}
	}

	// Return in order
	order := []string{"identifiers", "titles", "descriptions", "dates", "agents", "subjects", "publication", "other"}
	var result []fieldCategory
	for _, name := range order {
		if len(categories[name]) > 0 {
			result = append(result, fieldCategory{Name: name, Fields: categories[name]})
		}
	}
	return result
}

func drupalFieldToProto(field DrupalFieldConfig, storage DrupalFieldStorage, rdfMapping *DrupalRDFMapping, num int) ProtoField {
	pf := ProtoField{
		Name:        strings.TrimPrefix(field.FieldName, "field_"),
		Number:      num,
		Comment:     field.Label,
		DrupalField: field.FieldName,
		DrupalType:  field.FieldType,
		Cardinality: storage.Cardinality,
	}

	// Extract target type for entity references
	if field.FieldType == "entity_reference" || field.FieldType == "typed_relation" {
		if handler, ok := field.Settings["handler"].(string); ok {
			// handler format: "default:taxonomy_term" or "default:node"
			parts := strings.Split(handler, ":")
			if len(parts) > 1 {
				pf.TargetType = parts[1]
			}
		}
		// Check for bundle restrictions
		if handlerSettings, ok := field.Settings["handler_settings"].(map[string]any); ok {
			if targetBundles, ok := handlerSettings["target_bundles"].(map[string]any); ok {
				// Get first bundle if restricted
				for bundle := range targetBundles {
					pf.TargetBundle = bundle
					break
				}
			}
		}
	}

	// Extract RDF predicate if available
	if rdfMapping != nil {
		if rdfField, ok := rdfMapping.FieldMappings[field.FieldName]; ok {
			if len(rdfField.Properties) > 0 {
				pf.RDFPredicate = rdfField.Properties[0]
			}
		}
	}

	// Map to Hub schema field (uses RDF predicate if available, falls back to name heuristics)
	mapToHubField(&pf, field)

	// Determine proto type based on Drupal field type
	baseType := drupalTypeToProto(field.FieldType)

	// Check cardinality
	if storage.Cardinality == -1 || storage.Cardinality > 1 {
		pf.Type = "repeated " + baseType
	} else {
		pf.Type = baseType
	}

	return pf
}

// mapToHubField maps a Drupal field to its corresponding Hub schema field.
// It first tries to use RDF predicate mapping (if available), then falls back to name heuristics.
func mapToHubField(pf *ProtoField, field DrupalFieldConfig) {
	// Try RDF predicate mapping first
	if pf.RDFPredicate != "" && mapFromRDFPredicate(pf) {
		return
	}

	// Fall back to name-based heuristics
	mapFromFieldName(pf, field)
}

// mapFromRDFPredicate maps a field based on its RDF predicate. Returns true if mapped.
func mapFromRDFPredicate(pf *ProtoField) bool {
	pred := pf.RDFPredicate

	switch pred {
	// Dublin Core Terms
	case "dcterms:title":
		pf.HubField = "Title"
	case "dcterms:alternative":
		pf.HubField = "AltTitle"
	case "dcterms:creator":
		pf.HubField = "Contributors"
		pf.HubType = "creator"
	case "dcterms:contributor":
		pf.HubField = "Contributors"
	case "dcterms:issued":
		pf.HubField = "Dates"
		pf.HubType = "issued"
		pf.Parser = "edtf"
	case "dcterms:created":
		pf.HubField = "Dates"
		pf.HubType = "created"
		pf.Parser = "edtf"
	case "dcterms:modified":
		pf.HubField = "Dates"
		pf.HubType = "modified"
		pf.Parser = "edtf"
	case "dcterms:date":
		pf.HubField = "Dates"
		pf.HubType = "other"
		pf.Parser = "edtf"
	case "dcterms:dateCopyrighted":
		pf.HubField = "Dates"
		pf.HubType = "copyright"
		pf.Parser = "edtf"
	case "dcterms:valid":
		pf.HubField = "Dates"
		pf.HubType = "valid"
		pf.Parser = "edtf"
	case "premis:creation":
		pf.HubField = "Dates"
		pf.HubType = "captured"
		pf.Parser = "edtf"
	case "dcterms:identifier":
		pf.HubField = "Identifiers"
	case "dcterms:abstract":
		pf.HubField = "Abstract"
	case "dcterms:description":
		pf.HubField = "Description"
	case "dcterms:subject":
		pf.HubField = "Subjects"
	case "dcterms:temporal":
		pf.HubField = "Subjects"
		pf.HubType = "temporal"
	case "dcterms:spatial":
		pf.HubField = "Subjects"
		pf.HubType = "geographic"
	case "dcterms:type":
		pf.HubField = "ResourceType"
	case "dcterms:format":
		pf.HubField = "PhysicalDesc"
	case "dcterms:extent":
		pf.HubField = "PhysicalDesc"
	case "dcterms:language":
		pf.HubField = "Language"
	case "dcterms:tableOfContents":
		pf.HubField = "TableOfContents"
	case "dcterms:source":
		pf.HubField = "Source"

	// Dublin Core 1.1
	case "dc11:rights":
		pf.HubField = "Rights"
	case "dc11:publisher":
		pf.HubField = "Publisher"
	case "dc11:subject":
		pf.HubField = "Subjects"

	// PCDM
	case "pcdm:memberOf":
		pf.HubField = "Relations"
		pf.HubType = "member_of"

	// DBpedia
	case "dbpedia:isbn":
		pf.HubField = "Identifiers"
		pf.HubType = "isbn"
	case "dbpedia:oclc":
		pf.HubField = "Identifiers"
		pf.HubType = "oclc"

	// MARC Relators
	case "relators:pup":
		pf.HubField = "PlacePublished"

	// SKOS
	case "skos:note":
		pf.HubField = "Notes"

	// CO (Collection Ontology)
	case "co:index":
		pf.HubField = "Extra.weight"

	// RDA Unconstrained
	case "rdau:P60515": // title proper
		pf.HubField = "Title"
	case "rdau:P60329": // edition statement
		pf.HubField = "Extra.edition"
	case "rdau:P60538": // frequency
		pf.HubField = "Extra.frequency"
	case "rdau:P60051": // mode of issuance
		pf.HubField = "Extra.mode_of_issuance"

	default:
		return false
	}
	return true
}

// mapFromFieldName maps a field based on its Drupal field name (heuristic fallback).
func mapFromFieldName(pf *ProtoField, field DrupalFieldConfig) {
	name := field.FieldName
	fieldType := field.FieldType

	switch {
	// Title fields
	case name == "field_full_title":
		pf.HubField = "Title"
	case strings.Contains(name, "alt_title") || strings.Contains(name, "alternative_title"):
		pf.HubField = "AltTitle"

	// Contributors
	case strings.Contains(name, "linked_agent") || strings.Contains(name, "contributor"):
		pf.HubField = "Contributors"

	// Dates
	case strings.Contains(name, "date_issued") || name == "field_edtf_date_issued":
		pf.HubField = "Dates"
		pf.HubType = "issued"
		pf.Parser = "edtf"
	case strings.Contains(name, "date_created") || name == "field_edtf_date_created":
		pf.HubField = "Dates"
		pf.HubType = "created"
		pf.Parser = "edtf"
	case strings.Contains(name, "date_captured"):
		pf.HubField = "Dates"
		pf.HubType = "captured"
		pf.Parser = "edtf"
	case strings.Contains(name, "copyright_date"):
		pf.HubField = "Dates"
		pf.HubType = "copyright"
		pf.Parser = "edtf"
	case strings.Contains(name, "date_modified"):
		pf.HubField = "Dates"
		pf.HubType = "modified"
		pf.Parser = "edtf"
	case strings.Contains(name, "date_valid"):
		pf.HubField = "Dates"
		pf.HubType = "valid"
		pf.Parser = "edtf"
	case name == "field_edtf_date":
		pf.HubField = "Dates"
		pf.HubType = "other"
		pf.Parser = "edtf"

	// Resource type and genre
	case strings.Contains(name, "resource_type"):
		pf.HubField = "ResourceType"
	case strings.Contains(name, "genre"):
		pf.HubField = "Genre"

	// Language
	case strings.Contains(name, "language"):
		pf.HubField = "Language"

	// Rights
	case strings.Contains(name, "rights"):
		pf.HubField = "Rights"

	// Descriptions
	case strings.Contains(name, "abstract"):
		pf.HubField = "Abstract"
	case strings.Contains(name, "description"):
		pf.HubField = "Description"
	case strings.Contains(name, "physical_description") || strings.Contains(name, "extent"):
		pf.HubField = "PhysicalDesc"

	// Subjects
	case name == "field_subject" || strings.Contains(name, "subject_general"):
		pf.HubField = "Subjects"
	case strings.Contains(name, "geographic_subject"):
		pf.HubField = "Subjects"
		pf.HubType = "geographic"
	case strings.Contains(name, "temporal_subject"):
		pf.HubField = "Subjects"
		pf.HubType = "temporal"
	case strings.Contains(name, "subjects_name"):
		pf.HubField = "Subjects"
		pf.HubType = "name"

	// Publisher
	case strings.Contains(name, "publisher"):
		pf.HubField = "Publisher"
	case strings.Contains(name, "place_published"):
		pf.HubField = "PlacePublished"

	// Relations
	case strings.Contains(name, "member_of"):
		pf.HubField = "Relations"
		pf.HubType = "member_of"
	case strings.Contains(name, "related_item"):
		pf.HubField = "Relations"
		pf.HubType = "related_to"

	// Identifiers
	case strings.Contains(name, "identifier") && !strings.Contains(name, "local"):
		pf.HubField = "Identifiers"
	case strings.Contains(name, "local_identifier"):
		pf.HubField = "Identifiers"
		pf.HubType = "local"
	case strings.Contains(name, "pid"):
		pf.HubField = "Identifiers"
		pf.HubType = "pid"
	case strings.Contains(name, "doi"):
		pf.HubField = "Identifiers"
		pf.HubType = "doi"
	case strings.Contains(name, "isbn"):
		pf.HubField = "Identifiers"
		pf.HubType = "isbn"
	case strings.Contains(name, "oclc"):
		pf.HubField = "Identifiers"
		pf.HubType = "oclc"

	// Thesis fields
	case strings.Contains(name, "degree_name"):
		pf.HubField = "DegreeInfo.DegreeName"
	case strings.Contains(name, "degree_level"):
		pf.HubField = "DegreeInfo.DegreeLevel"
	case strings.Contains(name, "department"):
		pf.HubField = "DegreeInfo.Department"

	// Notes and other
	case strings.Contains(name, "note"):
		pf.HubField = "Notes"
	case strings.Contains(name, "table_of_contents"):
		pf.HubField = "TableOfContents"
	case strings.Contains(name, "source"):
		pf.HubField = "Source"
	case strings.Contains(name, "digital_origin"):
		pf.HubField = "DigitalOrigin"
	case strings.Contains(name, "physical_form"):
		pf.HubField = "PhysicalDesc"

	// Model (Islandora-specific, maps to extra)
	case name == "field_model":
		pf.HubField = "Extra.model"
	case name == "field_weight":
		pf.HubField = "Extra.weight"

	default:
		// Unknown field - store in Extra with the field name
		if fieldType != "" {
			cleanName := strings.TrimPrefix(name, "field_")
			pf.HubField = "Extra." + cleanName
		}
	}
}

func drupalTypeToProto(drupalType string) string {
	switch drupalType {
	case "string", "string_long", "text", "text_long", "text_with_summary":
		return "string"
	case "integer":
		return "int32"
	case "float", "decimal":
		return "double"
	case "boolean":
		return "bool"
	case "datetime", "timestamp", "created", "changed":
		return "string"
	case "edtf":
		return "string" // EDTF dates as strings
	case "entity_reference":
		return "TaxonomyRef"
	case "typed_relation":
		return "LinkedAgent"
	case "link":
		return "Link"
	case "image":
		return "ImageRef"
	case "file":
		return "FileRef"
	case "geolocation":
		return "Geolocation"
	default:
		return "string"
	}
}

func generateHelperMessages() []ProtoMessage {
	return []ProtoMessage{
		{
			Name:    "LinkedAgent",
			Comment: "LinkedAgent represents a typed relation to a person, family, or corporate body.",
			Fields: []ProtoField{
				{Name: "target_id", Type: "int64", Number: 1},
				{Name: "target_uuid", Type: "string", Number: 2},
				{Name: "target_type", Type: "string", Number: 3, Comment: "taxonomy_term"},
				{Name: "rel_type", Type: "string", Number: 4, Comment: "MARC relator code"},
				{Name: "name", Type: "string", Number: 10, Comment: "Resolved agent name"},
				{Name: "rel_label", Type: "string", Number: 11, Comment: "Resolved relation label"},
			},
		},
		{
			Name:    "TaxonomyRef",
			Comment: "TaxonomyRef represents a reference to a taxonomy term.",
			Fields: []ProtoField{
				{Name: "target_id", Type: "int64", Number: 1},
				{Name: "target_uuid", Type: "string", Number: 2},
				{Name: "target_type", Type: "string", Number: 3},
				{Name: "name", Type: "string", Number: 10, Comment: "Resolved term name"},
				{Name: "vid", Type: "string", Number: 11, Comment: "Vocabulary ID"},
				{Name: "uri", Type: "string", Number: 12, Comment: "External URI"},
			},
		},
		{
			Name:    "EntityRef",
			Comment: "EntityRef represents a generic entity reference.",
			Fields: []ProtoField{
				{Name: "target_id", Type: "int64", Number: 1},
				{Name: "target_uuid", Type: "string", Number: 2},
				{Name: "target_type", Type: "string", Number: 3},
				{Name: "title", Type: "string", Number: 10},
			},
		},
		{
			Name:    "Geolocation",
			Comment: "Geolocation represents geographic coordinates.",
			Fields: []ProtoField{
				{Name: "lat", Type: "double", Number: 1},
				{Name: "lng", Type: "double", Number: 2},
				{Name: "data", Type: "string", Number: 3},
			},
		},
		{
			Name:    "ImageRef",
			Comment: "ImageRef represents an image file reference.",
			Fields: []ProtoField{
				{Name: "target_id", Type: "int64", Number: 1},
				{Name: "alt", Type: "string", Number: 2},
				{Name: "title", Type: "string", Number: 3},
				{Name: "width", Type: "int32", Number: 4},
				{Name: "height", Type: "int32", Number: 5},
				{Name: "uri", Type: "string", Number: 10},
				{Name: "url", Type: "string", Number: 11},
			},
		},
		{
			Name:    "FileRef",
			Comment: "FileRef represents a file reference.",
			Fields: []ProtoField{
				{Name: "target_id", Type: "int64", Number: 1},
				{Name: "description", Type: "string", Number: 2},
				{Name: "uri", Type: "string", Number: 10},
				{Name: "url", Type: "string", Number: 11},
				{Name: "filemime", Type: "string", Number: 12},
				{Name: "filesize", Type: "int64", Number: 13},
			},
		},
		{
			Name:    "Link",
			Comment: "Link represents a URL with optional title.",
			Fields: []ProtoField{
				{Name: "uri", Type: "string", Number: 1},
				{Name: "title", Type: "string", Number: 2},
			},
		},
	}
}

func parseFieldConfigs(configPath, bundle string) ([]DrupalFieldConfig, error) {
	var fields []DrupalFieldConfig

	// Look for field.field.node.<bundle>.*.yml files
	pattern := filepath.Join(configPath, fmt.Sprintf("field.field.node.%s.*.yml", bundle))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var field DrupalFieldConfig
		if err := yaml.Unmarshal(data, &field); err != nil {
			continue
		}

		fields = append(fields, field)
	}

	return fields, nil
}

func parseFieldStorage(configPath string) ([]DrupalFieldStorage, error) {
	var storage []DrupalFieldStorage

	pattern := filepath.Join(configPath, "field.storage.node.*.yml")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var s DrupalFieldStorage
		if err := yaml.Unmarshal(data, &s); err != nil {
			continue
		}

		storage = append(storage, s)
	}

	return storage, nil
}

// parseRDFMapping loads the RDF mapping configuration for an entity type and bundle.
func parseRDFMapping(configPath, entityType, bundle string) (*DrupalRDFMapping, error) {
	// Look for rdf.mapping.<entity_type>.<bundle>.yml
	rdfPath := filepath.Join(configPath, fmt.Sprintf("rdf.mapping.%s.%s.yml", entityType, bundle))

	data, err := os.ReadFile(rdfPath)
	if err != nil {
		return nil, fmt.Errorf("reading RDF mapping: %w", err)
	}

	var mapping DrupalRDFMapping
	if err := yaml.Unmarshal(data, &mapping); err != nil {
		return nil, fmt.Errorf("parsing RDF mapping: %w", err)
	}

	return &mapping, nil
}

func toPascalCase(s string) string {
	parts := regexp.MustCompile(`[_-]+`).Split(s, -1)
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, "")
}

// ApplyAutoMappings converts the auto-detected HubField/HubType values to Hub* annotation fields.
// This is used in non-interactive mode to generate hub.v1 annotations from RDF/heuristic mappings.
func ApplyAutoMappings(proto *ProtoFile) {
	if len(proto.Messages) == 0 {
		return
	}

	mainMsg := &proto.Messages[0]
	for i := range mainMsg.Fields {
		field := &mainMsg.Fields[i]

		// Skip core fields
		if isCoreDrupalField(field.Name) {
			continue
		}

		// Convert HubField to HubTarget
		if field.HubField == "" {
			continue
		}

		// Handle compound fields like "Extra.model" or "DegreeInfo.DegreeName"
		if strings.HasPrefix(field.HubField, "Extra.") {
			field.HubTarget = "extra"
			field.HubExtraKey = strings.TrimPrefix(field.HubField, "Extra.")
		} else if strings.HasPrefix(field.HubField, "DegreeInfo.") {
			field.HubTarget = "degree_info"
		} else {
			// Map to lowercase target names
			field.HubTarget = fieldToTarget(field.HubField)
		}

		// Convert HubType to appropriate type-specific field
		if field.HubType != "" {
			switch field.HubTarget {
			case "dates":
				field.HubDateType = field.HubType
			case "identifiers":
				field.HubIDType = field.HubType
			case "subjects":
				field.HubSubjectVoc = field.HubType
			case "relations":
				field.HubRelType = field.HubType
			case "contributors":
				field.HubRole = field.HubType
			}
		}
	}

	proto.UseHubOptions = true
}

// fieldToTarget converts a HubField name to a hub target name.
func fieldToTarget(hubField string) string {
	mapping := map[string]string{
		"Title":           "title",
		"AltTitle":        "title",
		"Abstract":        "abstract",
		"Description":     "notes",
		"Publisher":       "publisher",
		"PlacePublished":  "place_published",
		"Language":        "language",
		"ResourceType":    "resource_type",
		"Genre":           "subjects",
		"Notes":           "notes",
		"Rights":          "rights",
		"Contributors":    "contributors",
		"Dates":           "dates",
		"Identifiers":     "identifiers",
		"Subjects":        "subjects",
		"Relations":       "relations",
		"PhysicalDesc":    "notes",
		"Source":          "notes",
		"TableOfContents": "notes",
		"DigitalOrigin":   "extra",
	}

	if target, ok := mapping[hubField]; ok {
		return target
	}
	return strings.ToLower(hubField)
}

// ApplyInteractiveMappings prompts the user to map each field to Hub targets.
// If protoPath exists, it loads previous mappings to autofill selections.
func ApplyInteractiveMappings(proto *ProtoFile, protoPath string) error {
	mapper, err := NewInteractiveMapper(protoPath)
	if err != nil {
		return fmt.Errorf("creating interactive mapper: %w", err)
	}

	fmt.Println("\n╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║             Interactive Hub Field Mapping                     ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════╣")
	fmt.Println("║ For each field, select the Hub target it should map to.       ║")
	fmt.Println("║ Press Enter to accept the default (marked with *).            ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")

	// Process main message fields
	if len(proto.Messages) == 0 {
		return nil
	}

	mainMsg := &proto.Messages[0]
	for i := range mainMsg.Fields {
		field := &mainMsg.Fields[i]

		// Skip core Drupal fields that don't need Hub mapping
		if isCoreDrupalField(field.Name) {
			continue
		}

		mapping, err := mapper.MapField(*field)
		if err != nil {
			return fmt.Errorf("mapping field %s: %w", field.Name, err)
		}

		// Apply the mapping to the field
		field.HubSkip = mapping.Skip
		if !mapping.Skip {
			field.HubTarget = mapping.Target
			field.HubDateType = mapping.DateType
			field.HubIDType = mapping.IDType
			field.HubRole = mapping.Role
			field.HubSubjectVoc = mapping.SubjectVoc
			field.HubRelType = mapping.RelType
			field.HubExtraKey = mapping.ExtraKey
			if mapping.Parser != "" {
				field.Parser = mapping.Parser
			}
		}
	}

	// Enable hub options in output
	proto.UseHubOptions = true

	fmt.Println("\n✓ Field mapping complete!")
	return nil
}

// isCoreDrupalField returns true if the field is a core Drupal field that doesn't need Hub mapping.
func isCoreDrupalField(name string) bool {
	switch name {
	case "nid", "uuid", "status", "created", "changed":
		return true
	default:
		return false
	}
}

// WriteProto writes a ProtoFile and companion metadata file to disk.
func WriteProto(proto *ProtoFile, outputPath string) error {
	// Ensure directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Write proto file
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating proto file: %w", err)
	}
	defer f.Close()

	tmpl := template.Must(template.New("proto").Parse(protoTemplate))
	if err := tmpl.Execute(f, proto); err != nil {
		return fmt.Errorf("writing proto: %w", err)
	}

	// Write companion metadata file
	metaPath := strings.TrimSuffix(outputPath, ".proto") + "_meta.go"
	mf, err := os.Create(metaPath)
	if err != nil {
		return fmt.Errorf("creating meta file: %w", err)
	}
	defer mf.Close()

	metaTmpl := template.Must(template.New("meta").Parse(metaTemplate))
	if err := metaTmpl.Execute(mf, proto); err != nil {
		return fmt.Errorf("writing meta: %w", err)
	}

	return nil
}

// HubAnnotation generates the hub.v1.field annotation for this field.
func (f ProtoField) HubAnnotation() string {
	if f.HubSkip || f.HubTarget == "" {
		return ""
	}

	var parts []string
	parts = append(parts, fmt.Sprintf(`target: "%s"`, f.HubTarget))

	// Add type-specific options
	if f.HubDateType != "" {
		parts = append(parts, fmt.Sprintf(`date_type: "%s"`, f.HubDateType))
	}
	if f.HubIDType != "" {
		parts = append(parts, fmt.Sprintf(`identifier_type: "%s"`, f.HubIDType))
	}
	if f.HubRole != "" {
		parts = append(parts, fmt.Sprintf(`role: "%s"`, f.HubRole))
	}
	if f.HubSubjectVoc != "" {
		parts = append(parts, fmt.Sprintf(`subject_vocabulary: "%s"`, f.HubSubjectVoc))
	}
	if f.HubRelType != "" {
		parts = append(parts, fmt.Sprintf(`relation_type: "%s"`, f.HubRelType))
	}

	// Add parser if specified
	if f.Parser != "" {
		parts = append(parts, fmt.Sprintf(`parser: "%s"`, f.Parser))
	}

	return " [(hub.v1.field) = {" + strings.Join(parts, " ") + "}]"
}

// HasHubAnnotation returns true if this field has hub mapping.
func (f ProtoField) HasHubAnnotation() bool {
	return !f.HubSkip && f.HubTarget != ""
}

const protoTemplate = `syntax = "proto3";

package {{.Package}};

option go_package = "{{.GoPackage}}";
{{if .UseHubOptions}}
import "hub/v1/options.proto";
{{end}}
// {{.Description}}
{{range .Messages}}
// {{.Comment}}
message {{.Name}} {
{{- range .Fields}}
  {{if .Comment}}// {{.Comment}}{{if .DrupalType}} [drupal:{{.DrupalType}}]{{end}}
  {{end}}{{.Type}} {{.Name}} = {{.Number}}{{if $.UseHubOptions}}{{.HubAnnotation}}{{end}};
{{end -}}
}
{{end}}
`

const metaTemplate = `// Code generated by crosswalk spoke generator. DO NOT EDIT.

package {{.PackageName}}

import (
	spokeregistry "github.com/lehigh-university-libraries/crosswalk/spoke/registry"
)

// FieldRegistry maps proto field names to their Drupal metadata.
var FieldRegistry = map[string]spokeregistry.FieldMeta{
{{- range .Messages}}{{if eq .Name (index $.Messages 0).Name}}
{{- range .Fields}}{{if .DrupalField}}
	"{{.Name}}": {
		ProtoField:   "{{.Name}}",
		DrupalField:  "{{.DrupalField}}",
		DrupalType:   "{{.DrupalType}}",
		Cardinality:  {{.Cardinality}},
		TargetType:   "{{.TargetType}}",
		TargetBundle: "{{.TargetBundle}}",
		RDFPredicate: "{{.RDFPredicate}}",
		HubField:     "{{.HubField}}",
		HubType:      "{{.HubType}}",
		Parser:       "{{.Parser}}",
	},
{{- end}}{{end}}
{{- end}}{{end}}
}
{{if .FormatName}}
func init() {
	spokeregistry.Register("{{.FormatName}}", FieldRegistry)
}
{{end}}
// GetFieldMeta returns metadata for a proto field, or nil if not found.
func GetFieldMeta(protoField string) *spokeregistry.FieldMeta {
	if meta, ok := FieldRegistry[protoField]; ok {
		return &meta
	}
	return nil
}

// GetFieldMetaByDrupalName returns metadata for a Drupal field name, or nil if not found.
func GetFieldMetaByDrupalName(drupalField string) *spokeregistry.FieldMeta {
	for _, meta := range FieldRegistry {
		if meta.DrupalField == drupalField {
			return &meta
		}
	}
	return nil
}

// DrupalFieldTypes returns all unique Drupal field types in use.
func DrupalFieldTypes() []string {
	types := make(map[string]bool)
	for _, meta := range FieldRegistry {
		if meta.DrupalType != "" {
			types[meta.DrupalType] = true
		}
	}
	var result []string
	for t := range types {
		result = append(result, t)
	}
	return result
}
`
