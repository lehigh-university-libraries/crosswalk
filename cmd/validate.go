package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/format/drupal"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

var (
	validateInput             string
	validateSourceProfileName string
	validateTaxonomy          string
	validateVerbose           bool
)

var validateCmd = &cobra.Command{
	Use:   "validate <format>",
	Short: "Validate metadata without converting",
	Long: `Validate metadata by parsing it to the intermediate representation.

This command parses the input and reports any issues found without
producing output. Useful for checking data quality before conversion.

Arguments:
  format  Input format (drupal, csv)

Input defaults to stdin.

Examples:
  crosswalk validate drupal -i input.json
  crosswalk validate drupal -i input.json --verbose
  cat data.json | crosswalk validate drupal`,
	Args: cobra.ExactArgs(1),
	RunE: runValidate,
}

func init() {
	validateCmd.Flags().StringVarP(&validateInput, "input", "i", "", "Input file (default: stdin)")
	validateCmd.Flags().StringVar(&validateSourceProfileName, "source-profile", "", "Canonical model-bound profile for the source system")
	validateCmd.Flags().StringVar(&validateTaxonomy, "taxonomy-file", "", "Taxonomy term resolution file")
	validateCmd.Flags().BoolVarP(&validateVerbose, "verbose", "v", false, "Show detailed information")
}

func runValidate(cmd *cobra.Command, args []string) (err error) {
	fromFormat := args[0]

	// Determine input source
	var input io.Reader
	var inputName string

	if validateInput != "" {
		f, openErr := os.Open(validateInput)
		if openErr != nil {
			return fmt.Errorf("opening input file: %w", openErr)
		}
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("closing input file: %w", cerr)
			}
		}()
		input = f
		inputName = validateInput
	} else {
		input = os.Stdin
		inputName = "stdin"
	}

	// Get parser
	parser, err := format.GetParser(fromFormat)
	if err != nil {
		return fmt.Errorf("unknown format %q: %w", fromFormat, err)
	}

	sourceMapping := defaultStaticProfile(fromFormat)
	systemProfile, err := loadSystemProfile(validateSourceProfileName, fromFormat, "source")
	if err != nil {
		return err
	}
	if systemProfile != nil {
		sourceMapping = nil
	}

	// Load taxonomy resolver
	var resolver format.TaxonomyResolver
	if validateTaxonomy != "" {
		store, err := drupal.LoadTaxonomyFile(validateTaxonomy)
		if err != nil {
			return fmt.Errorf("loading taxonomy file: %w", err)
		}
		resolver = store
	}

	// Parse input
	parseOpts := &format.ParseOptions{
		Profile:          sourceMapping,
		SystemProfile:    systemProfile,
		TaxonomyResolver: resolver,
		StripHTML:        true,
		SourceName:       inputName,
	}

	records, err := parser.Parse(input, parseOpts)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	validationOptions := hub.DefaultValidationOptions()
	if systemProfile != nil {
		validationOptions.IdentifierRegistry = systemProfile.IdentifierRegistry()
	}
	validationErrors := make([]string, 0)
	for index, record := range records {
		result := hub.Validate(record, validationOptions)
		for _, validationError := range result.Errors {
			validationErrors = append(validationErrors, fmt.Sprintf("record %d %s", index+1, validationError.Error()))
		}
	}
	if len(validationErrors) != 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(validationErrors, "; "))
	}

	writer := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(writer, "Valid: parsed %d records from %s\n", len(records), inputName); err != nil {
		return fmt.Errorf("writing validation result: %w", err)
	}

	if validateVerbose {
		if _, err := fmt.Fprintln(writer, "\nRecord summary:"); err != nil {
			return fmt.Errorf("writing validation summary: %w", err)
		}
		for i, r := range records {
			if _, err := fmt.Fprintf(writer, "\n  Record %d:\n    Title: %s\n    Contributors: %d\n    Dates: %d\n    Subjects: %d\n    Identifiers: %d\n", i+1, truncate(r.Title, 60), len(r.Contributors), len(r.Dates), len(r.Subjects), len(r.Identifiers)); err != nil {
				return fmt.Errorf("writing validation summary: %w", err)
			}
			if r.ResourceType != nil && r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED {
				if _, err := fmt.Fprintf(writer, "    Resource Type: %s\n", hub.ResourceTypeString(r.ResourceType)); err != nil {
					return fmt.Errorf("writing validation summary: %w", err)
				}
			}
			if nid := hub.GetExtraString(r, "nid"); nid != "" {
				if _, err := fmt.Fprintf(writer, "    NID: %s\n", nid); err != nil {
					return fmt.Errorf("writing validation summary: %w", err)
				}
			}
		}
	}

	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
