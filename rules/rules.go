// Package rules provides conditional transformation rules for format conversion.
//
// Rules allow format-specific output decisions based on hub field values.
// For example, a hub record with ResourceType="Dataset" might need to become
// schema:Dataset in schema.org output, while ResourceType="Article" becomes
// schema:ScholarlyArticle.
package rules

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// RuleSet contains all transformation rules for a conversion.
type RuleSet struct {
	// Name identifies this rule set
	Name string `yaml:"name" json:"name"`

	// Description documents what these rules are for
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// TargetFormat is the output format these rules apply to (e.g., "schemaorg", "crossref")
	TargetFormat string `yaml:"target_format" json:"target_format"`

	// Rules is the ordered list of transformation rules
	Rules []Rule `yaml:"rules" json:"rules"`
}

// Rule defines a single conditional transformation.
type Rule struct {
	// Name identifies this rule for debugging/logging
	Name string `yaml:"name" json:"name"`

	// Description documents what this rule does
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Priority determines rule evaluation order (higher = first). Default is 0.
	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`

	// When defines the conditions that must be met for this rule to apply
	When Condition `yaml:"when" json:"when"`

	// Then defines the transformations to apply when conditions are met
	Then Action `yaml:"then" json:"then"`
}

// Condition defines when a rule should be applied.
type Condition struct {
	// Field is the hub field to check (e.g., "ResourceType", "Language")
	Field string `yaml:"field,omitempty" json:"field,omitempty"`

	// Equals matches exact value
	Equals string `yaml:"equals,omitempty" json:"equals,omitempty"`

	// Contains matches if the field contains this substring
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`

	// Matches is a regex pattern to match against
	Matches string `yaml:"matches,omitempty" json:"matches,omitempty"`

	// In matches if the field value is in this list
	In []string `yaml:"in,omitempty" json:"in,omitempty"`

	// Exists checks if the field has any value
	Exists *bool `yaml:"exists,omitempty" json:"exists,omitempty"`

	// All requires all sub-conditions to match (AND)
	All []Condition `yaml:"all,omitempty" json:"all,omitempty"`

	// Any requires at least one sub-condition to match (OR)
	Any []Condition `yaml:"any,omitempty" json:"any,omitempty"`

	// Not inverts the sub-condition
	Not *Condition `yaml:"not,omitempty" json:"not,omitempty"`
}

// Action defines what transformation to apply when a rule matches.
type Action struct {
	// SetType sets the output type (e.g., schema.org @type)
	SetType string `yaml:"set_type,omitempty" json:"set_type,omitempty"`

	// SetField sets a specific output field
	SetField string `yaml:"set_field,omitempty" json:"set_field,omitempty"`

	// SetValue sets the value for SetField
	SetValue string `yaml:"set_value,omitempty" json:"set_value,omitempty"`

	// MapValue transforms the input value using a mapping table
	MapValue map[string]string `yaml:"map_value,omitempty" json:"map_value,omitempty"`

	// Template uses Go template syntax to compute a value
	Template string `yaml:"template,omitempty" json:"template,omitempty"`

	// Skip causes this field to be omitted from output
	Skip bool `yaml:"skip,omitempty" json:"skip,omitempty"`

	// Multiple actions can be combined
	Actions []Action `yaml:"actions,omitempty" json:"actions,omitempty"`
}

// Result holds the outcome of rule evaluation.
type Result struct {
	// Matched indicates if any rule matched
	Matched bool

	// RuleName is the name of the matched rule
	RuleName string

	// Type is the output type if SetType was used
	Type string

	// Fields contains any field/value pairs to set
	Fields map[string]string

	// Skip indicates the field should be omitted
	Skip bool
}

// Evaluate checks all rules against the given field values and returns the result.
// fieldValues maps hub field names to their string values.
func (rs *RuleSet) Evaluate(fieldValues map[string]string) *Result {
	result := &Result{
		Fields: make(map[string]string),
	}

	for _, rule := range rs.Rules {
		if rule.When.Evaluate(fieldValues) {
			result.Matched = true
			result.RuleName = rule.Name
			rule.Then.Apply(result, fieldValues)

			// First matching rule wins (unless we want to support multiple)
			break
		}
	}

	return result
}

// Evaluate checks if the condition matches the given field values.
func (c *Condition) Evaluate(fieldValues map[string]string) bool {
	// Handle composite conditions first
	if len(c.All) > 0 {
		for _, sub := range c.All {
			if !sub.Evaluate(fieldValues) {
				return false
			}
		}
		return true
	}

	if len(c.Any) > 0 {
		for _, sub := range c.Any {
			if sub.Evaluate(fieldValues) {
				return true
			}
		}
		return false
	}

	if c.Not != nil {
		return !c.Not.Evaluate(fieldValues)
	}

	// Simple field condition
	if c.Field == "" {
		return true // No condition means always match
	}

	value, exists := fieldValues[c.Field]

	// Check exists condition
	if c.Exists != nil {
		return exists == *c.Exists
	}

	if !exists {
		return false
	}

	// Check value conditions
	if c.Equals != "" {
		return strings.EqualFold(value, c.Equals)
	}

	if c.Contains != "" {
		return strings.Contains(strings.ToLower(value), strings.ToLower(c.Contains))
	}

	if c.Matches != "" {
		matched, _ := regexp.MatchString(c.Matches, value)
		return matched
	}

	if len(c.In) > 0 {
		valueLower := strings.ToLower(value)
		for _, v := range c.In {
			if strings.EqualFold(valueLower, v) {
				return true
			}
		}
		return false
	}

	// No specific condition, just check field exists
	return true
}

// Apply executes the action and updates the result.
func (a *Action) Apply(result *Result, fieldValues map[string]string) {
	if a.SetType != "" {
		result.Type = a.SetType
	}

	if a.SetField != "" && a.SetValue != "" {
		result.Fields[a.SetField] = a.SetValue
	}

	if len(a.MapValue) > 0 && a.SetField != "" {
		// Look up the field value in the map
		for field, value := range fieldValues {
			if mappedValue, ok := a.MapValue[value]; ok {
				result.Fields[a.SetField] = mappedValue
				break
			}
			_ = field // Use field if needed for more complex mapping
		}
	}

	if a.Skip {
		result.Skip = true
	}

	// Apply nested actions
	for _, sub := range a.Actions {
		sub.Apply(result, fieldValues)
	}
}

// LoadRuleSet loads a rule set from a YAML file.
func LoadRuleSet(path string) (*RuleSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading rules file: %w", err)
	}

	var rs RuleSet
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("parsing rules YAML: %w", err)
	}

	return &rs, nil
}

// LoadRuleSetFromBytes loads a rule set from YAML bytes.
func LoadRuleSetFromBytes(data []byte) (*RuleSet, error) {
	var rs RuleSet
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("parsing rules YAML: %w", err)
	}
	return &rs, nil
}
