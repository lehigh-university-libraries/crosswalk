package profile

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// HubField represents a possible hub field for mapping.
type HubField struct {
	Name        string
	Description string
	HasSubtype  bool
	Subtypes    []string
}

// AvailableHubFields returns all hub fields available for mapping.
func AvailableHubFields() []HubField {
	return []HubField{
		{Name: "Title", Description: "Main title of the work"},
		{Name: "AltTitle", Description: "Alternative/subtitle"},
		{Name: "Abstract", Description: "Abstract or summary"},
		{Name: "Description", Description: "General description"},
		{Name: "Contributors", Description: "Authors, editors, creators"},
		{Name: "Dates", Description: "Publication, creation dates", HasSubtype: true,
			Subtypes: []string{"issued", "created", "captured", "copyright", "modified", "available", "submitted", "accepted"}},
		{Name: "ResourceType", Description: "Type of resource (article, book, etc.)"},
		{Name: "Genre", Description: "Genre or form"},
		{Name: "Language", Description: "Language of the work"},
		{Name: "Rights", Description: "Rights statement or license"},
		{Name: "Subjects", Description: "Subject headings or topics", HasSubtype: true,
			Subtypes: []string{"lcsh", "mesh", "aat", "fast", "keywords", "local"}},
		{Name: "Identifiers", Description: "DOI, ISBN, URL, etc.", HasSubtype: true,
			Subtypes: []string{"doi", "isbn", "issn", "url", "handle", "orcid", "local"}},
		{Name: "Publisher", Description: "Publisher name"},
		{Name: "PlacePublished", Description: "Place of publication"},
		{Name: "Notes", Description: "General notes"},
		{Name: "PhysicalDesc", Description: "Physical description"},
		{Name: "TableOfContents", Description: "Table of contents"},
		{Name: "Source", Description: "Source of the work"},
		{Name: "Relations", Description: "Related works", HasSubtype: true,
			Subtypes: []string{"member_of", "part_of", "version_of", "related_to"}},
		{Name: "DegreeInfo.DegreeName", Description: "Degree name (thesis)"},
		{Name: "DegreeInfo.DegreeLevel", Description: "Degree level (thesis)"},
		{Name: "DegreeInfo.Department", Description: "Department (thesis)"},
		{Name: "DegreeInfo.Institution", Description: "Institution (thesis)"},
		{Name: "Extra", Description: "Custom field (specify name)", HasSubtype: true},
		{Name: "Skip", Description: "Ignore this column"},
	}
}

// CSVWizardOptions configures the CSV wizard.
type CSVWizardOptions struct {
	// Reader for user input (defaults to os.Stdin)
	Input io.Reader
	// Writer for prompts (defaults to os.Stdout)
	Output io.Writer
}

// CreateCSVProfileInteractive creates a profile by walking through a CSV file interactively.
func CreateCSVProfileInteractive(name, csvPath string, opts *CSVWizardOptions) (*Profile, error) {
	if opts == nil {
		opts = &CSVWizardOptions{
			Input:  os.Stdin,
			Output: os.Stdout,
		}
	}

	// Read the CSV header
	columns, sampleRow, err := readCSVHeader(csvPath)
	if err != nil {
		return nil, fmt.Errorf("reading CSV: %w", err)
	}

	profile := &Profile{
		Name:        name,
		Format:      "csv",
		Description: fmt.Sprintf("Generated from CSV file: %s", csvPath),
		Source: SourceInfo{
			CSVColumns: columns,
		},
		Fields: make(map[string]FieldMapping),
		Options: Options{
			MultiValueSeparator: "|",
			CSVDelimiter:        ",",
		},
	}

	reader := bufio.NewReader(opts.Input)
	out := opts.Output

	fmt.Fprintf(out, "\nCSV Profile Wizard\n")
	fmt.Fprintf(out, "==================\n")
	fmt.Fprintf(out, "Found %d columns in %s\n\n", len(columns), csvPath)

	// Ask about multi-value separator
	fmt.Fprintf(out, "What character separates multiple values in a cell? [|]: ")
	sep, _ := reader.ReadString('\n')
	sep = strings.TrimSpace(sep)
	if sep != "" {
		profile.Options.MultiValueSeparator = sep
	}

	fmt.Fprintf(out, "\nFor each column, select the hub field it maps to.\n\n")

	hubFields := AvailableHubFields()

	for i, col := range columns {
		sample := ""
		if i < len(sampleRow) {
			sample = sampleRow[i]
			if len(sample) > 50 {
				sample = sample[:47] + "..."
			}
		}

		fmt.Fprintf(out, "Column %d: %q\n", i+1, col)
		if sample != "" {
			fmt.Fprintf(out, "  Sample: %s\n", sample)
		}

		// Show auto-suggestion
		suggestion := suggestHubField(col)
		if suggestion != "" {
			fmt.Fprintf(out, "  Suggested: %s\n", suggestion)
		}

		fmt.Fprintf(out, "\nAvailable hub fields:\n")
		for j, hf := range hubFields {
			fmt.Fprintf(out, "  %2d. %-25s %s\n", j+1, hf.Name, hf.Description)
		}
		fmt.Fprintf(out, "\n")

		// Get selection
		defaultChoice := ""
		if suggestion != "" {
			for j, hf := range hubFields {
				if hf.Name == suggestion || strings.HasPrefix(suggestion, hf.Name+".") {
					defaultChoice = fmt.Sprintf("%d", j+1)
					break
				}
			}
		}

		prompt := "Select (1-%d)"
		if defaultChoice != "" {
			prompt += fmt.Sprintf(" [%s]", defaultChoice)
		}
		prompt += ": "
		fmt.Fprintf(out, prompt, len(hubFields))

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			input = defaultChoice
		}

		choice, err := strconv.Atoi(input)
		if err != nil || choice < 1 || choice > len(hubFields) {
			fmt.Fprintf(out, "Invalid choice, skipping column\n\n")
			profile.Fields[col] = FieldMapping{Skip: true}
			continue
		}

		selected := hubFields[choice-1]

		mapping := FieldMapping{}

		if selected.Name == "Skip" {
			mapping.Skip = true
		} else {
			mapping.Hub = selected.Name

			// Handle subtypes
			if selected.HasSubtype && len(selected.Subtypes) > 0 {
				fmt.Fprintf(out, "\nSubtype options for %s:\n", selected.Name)
				for j, st := range selected.Subtypes {
					fmt.Fprintf(out, "  %d. %s\n", j+1, st)
				}
				fmt.Fprintf(out, "Select subtype (or press Enter to skip): ")
				stInput, _ := reader.ReadString('\n')
				stInput = strings.TrimSpace(stInput)
				if stInput != "" {
					stChoice, err := strconv.Atoi(stInput)
					if err == nil && stChoice >= 1 && stChoice <= len(selected.Subtypes) {
						subtype := selected.Subtypes[stChoice-1]
						switch selected.Name {
						case "Dates":
							mapping.DateType = subtype
						case "Subjects":
							mapping.Vocabulary = subtype
						case "Identifiers":
							mapping.Type = subtype
						case "Relations":
							mapping.RelationType = subtype
						case "Extra":
							mapping.Hub = "Extra." + subtype
						}
					}
				}
			} else if selected.Name == "Extra" {
				fmt.Fprintf(out, "Enter the custom field name: ")
				customName, _ := reader.ReadString('\n')
				customName = strings.TrimSpace(customName)
				if customName != "" {
					mapping.Hub = "Extra." + customName
				}
			}

			// Ask about multi-value
			if selected.Name == "Contributors" || selected.Name == "Subjects" ||
				selected.Name == "Identifiers" || selected.Name == "Genre" ||
				selected.Name == "Notes" || selected.Name == "Relations" {
				fmt.Fprintf(out, "Can this column contain multiple values? [y/N]: ")
				mvInput, _ := reader.ReadString('\n')
				mvInput = strings.TrimSpace(strings.ToLower(mvInput))
				if mvInput == "y" || mvInput == "yes" {
					mapping.MultiValue = true
					mapping.Delimiter = profile.Options.MultiValueSeparator
				}
			}
		}

		profile.Fields[col] = mapping
		fmt.Fprintf(out, "\n")
	}

	return profile, nil
}

func readCSVHeader(path string) ([]string, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 // Allow variable fields

	header, err := reader.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("reading header: %w", err)
	}

	// Clean header names
	for i, h := range header {
		header[i] = strings.TrimSpace(h)
	}

	// Try to read first data row for samples
	sample, _ := reader.Read()

	return header, sample, nil
}

func suggestHubField(column string) string {
	col := strings.ToLower(column)
	col = strings.ReplaceAll(col, "_", " ")
	col = strings.ReplaceAll(col, "-", " ")

	suggestions := map[string]string{
		"title":             "Title",
		"name":              "Title",
		"alt title":         "AltTitle",
		"alternative title": "AltTitle",
		"subtitle":          "AltTitle",
		"author":            "Contributors",
		"authors":           "Contributors",
		"creator":           "Contributors",
		"creators":          "Contributors",
		"contributor":       "Contributors",
		"contributors":      "Contributors",
		"date":              "Dates",
		"year":              "Dates",
		"date issued":       "Dates",
		"date created":      "Dates",
		"publication date":  "Dates",
		"pub date":          "Dates",
		"type":              "ResourceType",
		"resource type":     "ResourceType",
		"genre":             "Genre",
		"language":          "Language",
		"lang":              "Language",
		"rights":            "Rights",
		"license":           "Rights",
		"abstract":          "Abstract",
		"summary":           "Abstract",
		"description":       "Description",
		"doi":               "Identifiers",
		"identifier":        "Identifiers",
		"identifiers":       "Identifiers",
		"isbn":              "Identifiers",
		"issn":              "Identifiers",
		"url":               "Identifiers",
		"subject":           "Subjects",
		"subjects":          "Subjects",
		"keyword":           "Subjects",
		"keywords":          "Subjects",
		"topic":             "Subjects",
		"topics":            "Subjects",
		"publisher":         "Publisher",
		"place":             "PlacePublished",
		"place published":   "PlacePublished",
		"publication place": "PlacePublished",
		"notes":             "Notes",
		"note":              "Notes",
		"collection":        "Relations",
		"member of":         "Relations",
		"degree":            "DegreeInfo.DegreeName",
		"degree name":       "DegreeInfo.DegreeName",
		"degree level":      "DegreeInfo.DegreeLevel",
		"department":        "DegreeInfo.Department",
		"institution":       "DegreeInfo.Institution",
		"nid":               "Extra.nid",
		"uuid":              "Extra.uuid",
		"id":                "Extra.id",
	}

	if hub, ok := suggestions[col]; ok {
		return hub
	}

	// Partial matching
	for key, hub := range suggestions {
		if strings.Contains(col, key) {
			return hub
		}
	}

	return ""
}

// MatchCSVProfile tries to find a matching profile for a CSV file.
func MatchCSVProfile(csvPath string) (*Profile, error) {
	columns, _, err := readCSVHeader(csvPath)
	if err != nil {
		return nil, err
	}

	profiles, err := List()
	if err != nil {
		return nil, err
	}

	var bestMatch *Profile
	bestScore := 0.0

	for _, name := range profiles {
		p, err := Load(name)
		if err != nil {
			continue
		}
		if p.Format != "csv" {
			continue
		}

		score := scoreCSVMatch(p, columns)
		if score > bestScore && score > 0.5 {
			bestScore = score
			bestMatch = p
		}
	}

	return bestMatch, nil
}

func scoreCSVMatch(p *Profile, columns []string) float64 {
	if len(p.Source.CSVColumns) == 0 {
		return 0
	}

	// Create a set of expected columns
	expected := make(map[string]bool)
	for _, c := range p.Source.CSVColumns {
		expected[strings.ToLower(c)] = true
	}

	// Count matches
	matches := 0
	for _, c := range columns {
		if expected[strings.ToLower(c)] {
			matches++
		}
	}

	// Score based on overlap
	return float64(matches) / float64(len(p.Source.CSVColumns))
}

// CreateCSVProfileFromColumns creates a profile with auto-suggested mappings.
func CreateCSVProfileFromColumns(name string, columns []string) *Profile {
	profile := &Profile{
		Name:        name,
		Format:      "csv",
		Description: "Auto-generated CSV profile",
		Source: SourceInfo{
			CSVColumns: columns,
		},
		Fields: make(map[string]FieldMapping),
		Options: Options{
			MultiValueSeparator: "|",
			CSVDelimiter:        ",",
		},
	}

	for _, col := range columns {
		suggestion := suggestHubField(col)
		if suggestion != "" {
			mapping := FieldMapping{Hub: suggestion}

			// Set date type for date fields
			colLower := strings.ToLower(col)
			if strings.Contains(colLower, "issued") {
				mapping.DateType = "issued"
			} else if strings.Contains(colLower, "created") {
				mapping.DateType = "created"
			}

			profile.Fields[col] = mapping
		} else {
			// Skip unknown columns by default
			profile.Fields[col] = FieldMapping{Skip: true}
		}
	}

	// Sort fields for consistent output
	sortedCols := make([]string, 0, len(columns))
	sortedCols = append(sortedCols, columns...)
	sort.Strings(sortedCols)

	return profile
}
