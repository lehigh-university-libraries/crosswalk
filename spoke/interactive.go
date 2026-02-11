// Package spoke provides interactive field mapping for proto generation.
package spoke

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// InteractiveMapper handles interactive field mapping prompts.
type InteractiveMapper struct {
	reader   *bufio.Reader
	existing map[string]ExistingMapping
	targets  []HubTarget
}

// NewInteractiveMapper creates a new interactive mapper.
// If protoPath points to an existing file, it loads previous mappings for autofill.
func NewInteractiveMapper(protoPath string) (*InteractiveMapper, error) {
	existing, err := ParseExistingProto(protoPath)
	if err != nil {
		return nil, fmt.Errorf("parsing existing proto: %w", err)
	}

	return &InteractiveMapper{
		reader:   bufio.NewReader(os.Stdin),
		existing: existing,
		targets:  GetHubTargets(),
	}, nil
}

// FieldMapping represents the result of interactive field mapping.
type FieldMapping struct {
	Target     string // Hub target field
	DateType   string // For date fields
	IDType     string // For identifier fields
	Role       string // For contributor fields
	SubjectVoc string // For subject fields
	RelType    string // For relation fields
	ExtraKey   string // For extra fields
	Parser     string // Parser to use
	Skip       bool   // Whether to skip this field
}

// MapField prompts the user to map a single field to the Hub schema.
func (im *InteractiveMapper) MapField(field ProtoField) (*FieldMapping, error) {
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Field: %s\n", field.Name)
	fmt.Printf("  Type: %s\n", field.Type)
	if field.Comment != "" {
		fmt.Printf("  Description: %s\n", field.Comment)
	}
	if field.DrupalType != "" {
		fmt.Printf("  Drupal type: %s\n", field.DrupalType)
	}
	if field.RDFPredicate != "" {
		fmt.Printf("  RDF predicate: %s\n", field.RDFPredicate)
	}

	// Check for existing mapping (from previously generated proto)
	existing := im.existing[field.Name]

	// Also check the auto-detected mapping from the generator
	var suggested string
	if field.HubField != "" {
		suggested = field.HubField
	}

	mapping := &FieldMapping{}

	// Select target
	target, err := im.selectTarget(existing, suggested)
	if err != nil {
		return nil, err
	}

	if target == "(skip)" {
		mapping.Skip = true
		return mapping, nil
	}

	mapping.Target = target

	// Get type-specific options based on target
	targetInfo := im.getTargetInfo(target)
	switch targetInfo.Type {
	case TargetDate:
		mapping.DateType, err = im.selectDateType(existing)
		if err != nil {
			return nil, err
		}
		mapping.Parser, err = im.selectParser(existing, "edtf")
		if err != nil {
			return nil, err
		}

	case TargetIdentifier:
		mapping.IDType, err = im.selectIdentifierType(existing, field)
		if err != nil {
			return nil, err
		}

	case TargetContributor:
		mapping.Role, err = im.selectContributorRole(existing)
		if err != nil {
			return nil, err
		}
		// Check if we need a parser
		if strings.Contains(field.Type, "LinkedAgent") || field.DrupalType == "typed_relation" {
			mapping.Parser = "relator"
		}

	case TargetSubject:
		mapping.SubjectVoc, err = im.selectSubjectVocabulary(existing, field)
		if err != nil {
			return nil, err
		}

	case TargetRelation:
		mapping.RelType, err = im.selectRelationType(existing)
		if err != nil {
			return nil, err
		}

	case TargetExtra:
		mapping.ExtraKey, err = im.promptExtraKey(field.Name)
		if err != nil {
			return nil, err
		}
	}

	return mapping, nil
}

// selectTarget prompts for target selection.
func (im *InteractiveMapper) selectTarget(existing ExistingMapping, suggested string) (string, error) {
	fmt.Println("\nSelect Hub target:")

	// Determine default
	defaultChoice := 0
	defaultTarget := ""
	if existing.Target != "" {
		defaultTarget = existing.Target
	} else if suggested != "" {
		defaultTarget = suggested
	}

	for i, t := range im.targets {
		marker := "  "
		if t.Name == defaultTarget {
			marker = "* "
			defaultChoice = i + 1
		}
		fmt.Printf("%s%2d. %-20s %s\n", marker, i+1, t.Name, t.Description)
	}

	if defaultChoice > 0 {
		fmt.Printf("\nEnter choice [%d]: ", defaultChoice)
	} else {
		fmt.Print("\nEnter choice: ")
	}

	choice, err := im.readChoice(defaultChoice, len(im.targets))
	if err != nil {
		return "", err
	}

	return im.targets[choice-1].Name, nil
}

// selectDateType prompts for date type selection.
func (im *InteractiveMapper) selectDateType(existing ExistingMapping) (string, error) {
	dateTypes := GetDateTypes()
	fmt.Println("\nSelect date type:")

	defaultChoice := 1
	for i, dt := range dateTypes {
		marker := "  "
		if dt == existing.DateType {
			marker = "* "
			defaultChoice = i + 1
		}
		fmt.Printf("%s%2d. %s\n", marker, i+1, dt)
	}

	fmt.Printf("\nEnter choice [%d]: ", defaultChoice)
	choice, err := im.readChoice(defaultChoice, len(dateTypes))
	if err != nil {
		return "", err
	}

	return dateTypes[choice-1], nil
}

// selectIdentifierType prompts for identifier type selection.
func (im *InteractiveMapper) selectIdentifierType(existing ExistingMapping, field ProtoField) (string, error) {
	idTypes := GetIdentifierTypes()
	fmt.Println("\nSelect identifier type:")

	// Try to guess from field name
	defaultChoice := 1
	suggested := ""
	switch {
	case strings.Contains(field.Name, "doi"):
		suggested = "doi"
	case strings.Contains(field.Name, "isbn"):
		suggested = "isbn"
	case strings.Contains(field.Name, "issn"):
		suggested = "issn"
	case strings.Contains(field.Name, "orcid"):
		suggested = "orcid"
	case strings.Contains(field.Name, "handle"):
		suggested = "handle"
	case strings.Contains(field.Name, "oclc"):
		suggested = "oclc"
	case strings.Contains(field.Name, "local"):
		suggested = "local"
	case strings.Contains(field.Name, "pid"):
		suggested = "local"
	case strings.Contains(field.Name, "url"):
		suggested = "url"
	}

	if existing.IDType != "" {
		suggested = existing.IDType
	}

	for i, idt := range idTypes {
		marker := "  "
		if idt == suggested {
			marker = "* "
			defaultChoice = i + 1
		}
		fmt.Printf("%s%2d. %s\n", marker, i+1, idt)
	}

	fmt.Printf("\nEnter choice [%d]: ", defaultChoice)
	choice, err := im.readChoice(defaultChoice, len(idTypes))
	if err != nil {
		return "", err
	}

	return idTypes[choice-1], nil
}

// selectContributorRole prompts for contributor role selection.
func (im *InteractiveMapper) selectContributorRole(existing ExistingMapping) (string, error) {
	roles := GetContributorRoles()
	fmt.Println("\nSelect default contributor role:")

	defaultChoice := 1
	if existing.Role != "" {
		for i, r := range roles {
			if r == existing.Role {
				defaultChoice = i + 1
				break
			}
		}
	}

	for i, r := range roles {
		marker := "  "
		if i+1 == defaultChoice {
			marker = "* "
		}
		fmt.Printf("%s%2d. %s\n", marker, i+1, r)
	}

	fmt.Printf("\nEnter choice [%d]: ", defaultChoice)
	choice, err := im.readChoice(defaultChoice, len(roles))
	if err != nil {
		return "", err
	}

	return roles[choice-1], nil
}

// selectSubjectVocabulary prompts for subject vocabulary selection.
func (im *InteractiveMapper) selectSubjectVocabulary(existing ExistingMapping, field ProtoField) (string, error) {
	vocabs := GetSubjectVocabularies()
	fmt.Println("\nSelect subject vocabulary:")

	// Try to guess from field name
	defaultChoice := 1
	suggested := ""
	switch {
	case strings.Contains(field.Name, "geographic"):
		suggested = "geographic"
	case strings.Contains(field.Name, "temporal"):
		suggested = "temporal"
	case strings.Contains(field.Name, "name"):
		suggested = "name"
	case strings.Contains(field.Name, "genre"):
		suggested = "genre"
	case strings.Contains(field.Name, "lcsh"):
		suggested = "lcsh"
	case strings.Contains(field.Name, "mesh"):
		suggested = "mesh"
	}

	if existing.SubjectVoc != "" {
		suggested = existing.SubjectVoc
	}

	for i, v := range vocabs {
		marker := "  "
		if v == suggested {
			marker = "* "
			defaultChoice = i + 1
		}
		fmt.Printf("%s%2d. %s\n", marker, i+1, v)
	}

	fmt.Printf("\nEnter choice [%d]: ", defaultChoice)
	choice, err := im.readChoice(defaultChoice, len(vocabs))
	if err != nil {
		return "", err
	}

	return vocabs[choice-1], nil
}

// selectRelationType prompts for relation type selection.
func (im *InteractiveMapper) selectRelationType(existing ExistingMapping) (string, error) {
	relTypes := GetRelationTypes()
	fmt.Println("\nSelect relation type:")

	defaultChoice := 1
	if existing.RelType != "" {
		for i, r := range relTypes {
			if r == existing.RelType {
				defaultChoice = i + 1
				break
			}
		}
	}

	for i, r := range relTypes {
		marker := "  "
		if i+1 == defaultChoice {
			marker = "* "
		}
		fmt.Printf("%s%2d. %s\n", marker, i+1, r)
	}

	fmt.Printf("\nEnter choice [%d]: ", defaultChoice)
	choice, err := im.readChoice(defaultChoice, len(relTypes))
	if err != nil {
		return "", err
	}

	return relTypes[choice-1], nil
}

// selectParser prompts for parser selection.
func (im *InteractiveMapper) selectParser(existing ExistingMapping, suggested string) (string, error) {
	parsers := GetParsers()
	fmt.Println("\nSelect parser:")

	defaultChoice := 1
	if existing.Parser != "" {
		suggested = existing.Parser
	}

	for i, p := range parsers {
		marker := "  "
		if p == suggested {
			marker = "* "
			defaultChoice = i + 1
		}
		fmt.Printf("%s%2d. %s\n", marker, i+1, p)
	}

	fmt.Printf("\nEnter choice [%d]: ", defaultChoice)
	choice, err := im.readChoice(defaultChoice, len(parsers))
	if err != nil {
		return "", err
	}

	return parsers[choice-1], nil
}

// promptExtraKey prompts for a custom key for extra fields.
func (im *InteractiveMapper) promptExtraKey(fieldName string) (string, error) {
	fmt.Printf("\nEnter key name for extra field [%s]: ", fieldName)
	input, err := im.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return fieldName, nil
	}
	return input, nil
}

// readChoice reads a numeric choice from stdin.
func (im *InteractiveMapper) readChoice(defaultVal, maxVal int) (int, error) {
	input, err := im.reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	input = strings.TrimSpace(input)

	if input == "" {
		return defaultVal, nil
	}

	choice, err := strconv.Atoi(input)
	if err != nil {
		return 0, fmt.Errorf("invalid choice: %s", input)
	}

	if choice < 1 || choice > maxVal {
		return 0, fmt.Errorf("choice out of range: %d (1-%d)", choice, maxVal)
	}

	return choice, nil
}

// getTargetInfo returns the HubTarget for a given target name.
func (im *InteractiveMapper) getTargetInfo(name string) HubTarget {
	for _, t := range im.targets {
		if t.Name == name {
			return t
		}
	}
	return HubTarget{Name: name, Type: TargetSimple}
}

// Confirm prompts for yes/no confirmation.
func (im *InteractiveMapper) Confirm(prompt string, defaultYes bool) (bool, error) {
	if defaultYes {
		fmt.Printf("%s [Y/n]: ", prompt)
	} else {
		fmt.Printf("%s [y/N]: ", prompt)
	}

	input, err := im.reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" {
		return defaultYes, nil
	}

	return input == "y" || input == "yes", nil
}
