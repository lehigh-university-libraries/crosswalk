package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/format/drupal"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
)

var (
	validateInput       string
	validateProfileName string
	validateTaxonomy    string
	validateVerbose     bool
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
	validateCmd.Flags().StringVarP(&validateProfileName, "profile", "p", "", "Mapping profile name")
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

	// Load profile
	var profile *mapping.Profile
	if validateProfileName != "" {
		registry, err := mapping.NewProfileRegistry()
		if err != nil {
			return err
		}
		p, ok := registry.Get(validateProfileName)
		if !ok {
			return fmt.Errorf("unknown profile: %s", validateProfileName)
		}
		profile = p
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
		Profile:          profile,
		TaxonomyResolver: resolver,
		StripHTML:        true,
		SourceName:       inputName,
	}

	records, err := parser.Parse(input, parseOpts)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	fmt.Printf("✓ Valid: parsed %d records from %s\n", len(records), inputName)

	if validateVerbose {
		fmt.Println("\nRecord summary:")
		for i, r := range records {
			fmt.Printf("\n  Record %d:\n", i+1)
			fmt.Printf("    Title: %s\n", truncate(r.Title, 60))
			fmt.Printf("    Contributors: %d\n", len(r.Contributors))
			fmt.Printf("    Dates: %d\n", len(r.Dates))
			fmt.Printf("    Subjects: %d\n", len(r.Subjects))
			fmt.Printf("    Identifiers: %d\n", len(r.Identifiers))
			if r.ResourceType != nil && r.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED {
				fmt.Printf("    Resource Type: %s\n", hub.ResourceTypeString(r.ResourceType))
			}
			if nid := hub.GetExtraString(r, "nid"); nid != "" {
				fmt.Printf("    NID: %s\n", nid)
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
