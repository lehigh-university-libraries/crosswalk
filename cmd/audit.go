package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/spf13/cobra"
)

// auditCmd represents the audit command
var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit metadata records for quality issues",
	Long:  `Audit commands help identify data quality issues and promotion candidates.`,
}

// auditExtrasCmd analyzes extras fields across records
var auditExtrasCmd = &cobra.Command{
	Use:   "extras [format] [input-file]",
	Short: "Analyze extras fields to find promotion candidates",
	Long: `Analyzes the 'extra' field across all records to identify:
- Fields that appear frequently and should be promoted to Hub schema
- Inconsistent types across records (e.g., string vs number)
- Keys using human labels instead of machine names

Example:
  crosswalk audit extras drupal export.json
  crosswalk audit extras csv records.csv --profile my-profile`,
	Args: cobra.ExactArgs(2),
	RunE: runAuditExtras,
}

// ExtrasAuditReport contains the results of an extras audit.
type ExtrasAuditReport struct {
	TotalRecords        int                   `json:"total_records"`
	RecordsWithExtras   int                   `json:"records_with_extras"`
	FieldFrequency      map[string]FieldStats `json:"field_frequency"`
	TypeInconsistency   map[string][]string   `json:"type_inconsistency"`
	PromotionCandidates []PromotionCandidate  `json:"promotion_candidates"`
	InvalidKeys         []string              `json:"invalid_keys"`
}

// FieldStats tracks statistics for a single extras field.
type FieldStats struct {
	Count      int            `json:"count"`
	Percentage float64        `json:"percentage"`
	Types      map[string]int `json:"types"`
	Examples   []string       `json:"examples,omitempty"`
}

// PromotionCandidate represents a field that should be promoted.
type PromotionCandidate struct {
	Field      string  `json:"field"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
	Reason     string  `json:"reason"`
}

func init() {
	rootCmd.AddCommand(auditCmd)
	auditCmd.AddCommand(auditExtrasCmd)

	auditExtrasCmd.Flags().StringP("profile", "p", "", "Profile name to use for parsing")
	auditExtrasCmd.Flags().Float64("threshold", 50.0, "Percentage threshold for promotion candidates")
	auditExtrasCmd.Flags().IntP("examples", "e", 3, "Number of example values to include")
	auditExtrasCmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	auditExtrasCmd.Flags().Bool("json", false, "Output as JSON")
}

func runAuditExtras(cmd *cobra.Command, args []string) error {
	formatName := args[0]
	inputFile := args[1]

	profileName, _ := cmd.Flags().GetString("profile")
	threshold, _ := cmd.Flags().GetFloat64("threshold")
	maxExamples, _ := cmd.Flags().GetInt("examples")
	outputFile, _ := cmd.Flags().GetString("output")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// Read input file
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("reading input file: %w", err)
	}

	// Get parser
	parser, err := format.GetParser(formatName)
	if err != nil {
		return fmt.Errorf("unknown format: %s: %w", formatName, err)
	}

	// Parse records
	parseOpts := &format.ParseOptions{}
	// TODO: Load profile by name if provided
	_ = profileName // Profile loading not yet implemented

	records, err := parser.Parse(bytes.NewReader(data), parseOpts)
	if err != nil {
		return fmt.Errorf("parsing input: %w", err)
	}

	// Run audit
	report := auditExtras(records, threshold, maxExamples)

	// Output
	var output []byte
	if jsonOutput {
		output, err = json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling report: %w", err)
		}
	} else {
		output = []byte(formatExtrasReport(report))
	}

	if outputFile != "" {
		return os.WriteFile(outputFile, output, 0644)
	}

	fmt.Println(string(output))
	return nil
}

func auditExtras(records []*hubv1.Record, threshold float64, maxExamples int) *ExtrasAuditReport {
	report := &ExtrasAuditReport{
		TotalRecords:   len(records),
		FieldFrequency: make(map[string]FieldStats),
	}

	// Count extras usage
	for _, record := range records {
		if record.GetExtra() == nil || len(record.GetExtra().Fields) == 0 {
			continue
		}
		report.RecordsWithExtras++

		for key, value := range record.GetExtra().Fields {
			stats, ok := report.FieldFrequency[key]
			if !ok {
				stats = FieldStats{Types: make(map[string]int)}
			}
			stats.Count++
			stats.Types[getValueTypeName(value)]++

			// Collect examples
			if len(stats.Examples) < maxExamples {
				if example := getValueString(value); example != "" && len(example) < 100 {
					// Avoid duplicates
					found := false
					for _, ex := range stats.Examples {
						if ex == example {
							found = true
							break
						}
					}
					if !found {
						stats.Examples = append(stats.Examples, example)
					}
				}
			}

			report.FieldFrequency[key] = stats
		}
	}

	// Calculate percentages and find promotion candidates
	for key, stats := range report.FieldFrequency {
		stats.Percentage = float64(stats.Count) / float64(report.TotalRecords) * 100
		report.FieldFrequency[key] = stats

		// Check for promotion candidates
		if stats.Percentage >= threshold {
			report.PromotionCandidates = append(report.PromotionCandidates, PromotionCandidate{
				Field:      key,
				Count:      stats.Count,
				Percentage: stats.Percentage,
				Reason:     fmt.Sprintf("Appears in %.1f%% of records (threshold: %.1f%%)", stats.Percentage, threshold),
			})
		}

		// Check for invalid keys
		if strings.Contains(key, " ") {
			report.InvalidKeys = append(report.InvalidKeys, key)
		}
	}

	// Sort promotion candidates by percentage
	sort.Slice(report.PromotionCandidates, func(i, j int) bool {
		return report.PromotionCandidates[i].Percentage > report.PromotionCandidates[j].Percentage
	})

	// Check type consistency
	report.TypeInconsistency = hub.ValidateExtrasTypes(records)

	return report
}

func getValueTypeName(v interface{}) string {
	switch v.(type) {
	case nil:
		return "null"
	case float64:
		return "number"
	case string:
		return "string"
	case bool:
		return "bool"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func getValueString(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case float64:
		return fmt.Sprintf("%v", val)
	case bool:
		return fmt.Sprintf("%v", val)
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

func formatExtrasReport(report *ExtrasAuditReport) string {
	var sb strings.Builder

	sb.WriteString("=== Extras Field Audit Report ===\n\n")
	sb.WriteString(fmt.Sprintf("Total records: %d\n", report.TotalRecords))
	sb.WriteString(fmt.Sprintf("Records with extras: %d (%.1f%%)\n\n",
		report.RecordsWithExtras,
		float64(report.RecordsWithExtras)/float64(report.TotalRecords)*100))

	// Promotion candidates
	if len(report.PromotionCandidates) > 0 {
		sb.WriteString("🚨 PROMOTION CANDIDATES (should become Hub fields):\n")
		for _, pc := range report.PromotionCandidates {
			sb.WriteString(fmt.Sprintf("  • %s: %d records (%.1f%%)\n", pc.Field, pc.Count, pc.Percentage))
		}
		sb.WriteString("\n")
	}

	// Type inconsistencies
	if len(report.TypeInconsistency) > 0 {
		sb.WriteString("⚠️  TYPE INCONSISTENCIES (mixed types for same field):\n")
		for field, types := range report.TypeInconsistency {
			sb.WriteString(fmt.Sprintf("  • %s: %s\n", field, strings.Join(types, ", ")))
		}
		sb.WriteString("\n")
	}

	// Invalid keys
	if len(report.InvalidKeys) > 0 {
		sb.WriteString("❌ INVALID KEYS (contain spaces, should use snake_case):\n")
		for _, key := range report.InvalidKeys {
			sb.WriteString(fmt.Sprintf("  • \"%s\"\n", key))
		}
		sb.WriteString("\n")
	}

	// All fields by frequency
	sb.WriteString("📊 ALL EXTRAS FIELDS BY FREQUENCY:\n")

	// Sort by count
	type kv struct {
		key   string
		stats FieldStats
	}
	var sorted []kv
	for k, v := range report.FieldFrequency {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].stats.Count > sorted[j].stats.Count
	})

	for _, item := range sorted {
		sb.WriteString(fmt.Sprintf("  %s: %d (%.1f%%)\n", item.key, item.stats.Count, item.stats.Percentage))
		if len(item.stats.Examples) > 0 {
			sb.WriteString(fmt.Sprintf("    examples: %s\n", strings.Join(item.stats.Examples, ", ")))
		}
	}

	return sb.String()
}
