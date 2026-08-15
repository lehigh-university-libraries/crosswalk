package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lehigh-university-libraries/crosswalk/format"
	crossreffmt "github.com/lehigh-university-libraries/crosswalk/format/crossref"
	csvfmt "github.com/lehigh-university-libraries/crosswalk/format/csv"
	"github.com/lehigh-university-libraries/crosswalk/format/drupal"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/source"
	transformationspec "github.com/lehigh-university-libraries/crosswalk/spec"
	spokeregistry "github.com/lehigh-university-libraries/crosswalk/spoke/registry"

	// Register all format plugins
	_ "github.com/lehigh-university-libraries/crosswalk/format/archivesspace"
	_ "github.com/lehigh-university-libraries/crosswalk/format/arxiv"
	_ "github.com/lehigh-university-libraries/crosswalk/format/bibtex"
	_ "github.com/lehigh-university-libraries/crosswalk/format/crossrefrest"
	_ "github.com/lehigh-university-libraries/crosswalk/format/csl"
	_ "github.com/lehigh-university-libraries/crosswalk/format/datacite"
	_ "github.com/lehigh-university-libraries/crosswalk/format/dublincore"
	_ "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	_ "github.com/lehigh-university-libraries/crosswalk/format/mods"
	_ "github.com/lehigh-university-libraries/crosswalk/format/omeka_s"
	_ "github.com/lehigh-university-libraries/crosswalk/format/proquest"
	_ "github.com/lehigh-university-libraries/crosswalk/format/schemaorg"
	_ "github.com/lehigh-university-libraries/crosswalk/format/scopus"
	_ "github.com/lehigh-university-libraries/crosswalk/format/wos"
	_ "github.com/lehigh-university-libraries/crosswalk/format/zenodo"

	// Register spoke field registries for use as default profiles
	_ "github.com/lehigh-university-libraries/crosswalk/spoke/islandora/v1"
	_ "github.com/lehigh-university-libraries/crosswalk/spoke/islandora_workbench/v1"
)

type convertOptions struct {
	inputPath                  string
	outputPath                 string
	sourceProfileName          string
	targetProfileName          string
	taxonomyPath               string
	columns                    []string
	multiValueSeparator        string
	stripHTML                  bool
	pretty                     bool
	baseURL                    string
	referenceDOIs              []string
	skipReferenceDOIValidation bool
	transformationSpecPath     string
}

func defaultConvertOptions() convertOptions {
	return convertOptions{multiValueSeparator: "|", stripHTML: true}
}

func newConvertCmd() *cobra.Command {
	options := defaultConvertOptions()
	command := &cobra.Command{
		Use:   "convert <from> <to>",
		Short: "Convert metadata between formats",
		Long: `Convert scholarly metadata from one format to another.

Arguments:
  from    Source format (drupal, csv)
  to      Target format (drupal, csv)

Input defaults to stdin, output defaults to stdout.

Examples:
  # Convert Drupal JSON to CSV (stdin to stdout)
  cat export.json | crosswalk convert drupal csv

  # Explicit input file
  crosswalk convert drupal csv --input data.json

  # Input and output files
  crosswalk convert drupal csv -i data.json -o output.csv

  # With taxonomy resolution
  crosswalk convert drupal csv -i data.json --taxonomy-file terms.json

  # Resolve relative source identifiers without network access
  crosswalk convert archivesspace csv -i data.json --base-url https://example.com`,
		Args: cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runConvert(command, args, options)
		},
	}
	command.Flags().StringVarP(&options.inputPath, "input", "i", "", "Input file (default: stdin)")
	command.Flags().StringVarP(&options.outputPath, "output", "o", "", "Output file (default: stdout)")
	command.Flags().StringVar(&options.sourceProfileName, "source-profile", "", "Canonical model-bound profile for the source system")
	command.Flags().StringVar(&options.targetProfileName, "target-profile", "", "Canonical model-bound profile for the target system")
	command.Flags().StringVar(&options.taxonomyPath, "taxonomy-file", "", "Taxonomy term resolution file (JSON)")
	command.Flags().StringSliceVarP(&options.columns, "columns", "c", nil, "CSV columns to output")
	command.Flags().StringVar(&options.multiValueSeparator, "separator", options.multiValueSeparator, "Multi-value field separator")
	command.Flags().BoolVar(&options.stripHTML, "strip-html", options.stripHTML, "Strip HTML from text fields")
	command.Flags().BoolVar(&options.pretty, "pretty", false, "Pretty-print JSON output")
	command.Flags().StringVar(&options.baseURL, "base-url", "", "source system base URL used only to resolve relative identifiers")
	command.Flags().StringSliceVar(&options.referenceDOIs, "reference-doi", nil, "DOI referenced by this work; repeat or comma-separate")
	command.Flags().BoolVar(&options.skipReferenceDOIValidation, "skip-reference-doi-validation", false, "Do not resolve --reference-doi values with DOI.org before writing output")
	command.Flags().StringVar(&options.transformationSpecPath, "spec", "", "Transformation specification JSON/YAML for spec-driven conversion")
	return command
}

func runConvert(cmd *cobra.Command, args []string, options convertOptions) (err error) {
	fromFormat := args[0]
	toFormat := args[1]

	// Determine input source
	var input io.Reader
	var inputName string

	if options.inputPath != "" {
		f, err := os.Open(options.inputPath)
		if err != nil {
			return fmt.Errorf("opening input file: %w", err)
		}
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("closing input file: %w", cerr)
			}
		}()
		input = f
		inputName = options.inputPath
	} else {
		input = cmd.InOrStdin()
		inputName = "stdin"
	}

	if len(options.referenceDOIs) > 0 && !options.skipReferenceDOIValidation {
		if err := validateReferenceDOIValues(cmd.Context(), options.referenceDOIs); err != nil {
			return err
		}
	}

	// Get parser
	parser, err := format.GetParser(fromFormat)
	if err != nil {
		return fmt.Errorf("unknown source format %q: %w", fromFormat, err)
	}

	// Get serializer
	serializer, err := format.GetSerializer(toFormat)
	if err != nil {
		return fmt.Errorf("unknown target format %q: %w", toFormat, err)
	}

	// Static format mappings and instance-specific system profiles are separate
	// contracts. Source configuration is never reused as a target mapping.
	sourceMapping := defaultStaticProfile(fromFormat)
	targetMapping := defaultStaticProfile(toFormat)
	sourceProfile, err := loadSystemProfile(options.sourceProfileName, fromFormat, "source")
	if err != nil {
		return err
	}
	targetProfile, err := loadSystemProfile(options.targetProfileName, toFormat, "target")
	if err != nil {
		return err
	}
	if sourceProfile != nil {
		sourceMapping = nil
	}
	if targetProfile != nil {
		targetMapping = nil
	}

	transformation, err := loadTransformationSpec(options.transformationSpecPath, fromFormat, toFormat)
	if err != nil {
		return err
	}
	if transformation != nil {
		if sourceProfile != nil {
			return fmt.Errorf("--spec cannot be combined with --source-profile")
		}
		if transformation.Fingerprint.Profile != "" && targetProfile == nil {
			return fmt.Errorf("profile-bound --spec requires the exact --target-profile")
		}
		if targetProfile != nil {
			if targetProfile.System() != "drupal" || toFormat != "islandora-workbench" {
				return fmt.Errorf("--target-profile with --spec is supported only for a Drupal-bound Islandora Workbench target")
			}
			if transformation.Fingerprint.Model != targetProfile.ModelFingerprint() {
				return fmt.Errorf("transformation model fingerprint does not match target profile model")
			}
			if transformation.Fingerprint.Profile != targetProfile.Fingerprint() {
				return fmt.Errorf("transformation profile fingerprint does not match target profile")
			}
		}
		if cmd.Flags().Changed("columns") {
			return fmt.Errorf("--spec cannot be combined with --columns; target columns come from the specification")
		}
		if cmd.Flags().Changed("separator") {
			return fmt.Errorf("--spec cannot be combined with --separator; the target separator comes from the specification")
		}
	} else if targetProfile != nil && toFormat == "islandora-workbench" {
		return fmt.Errorf("--target-profile for islandora-workbench requires a profile-bound --spec")
	}

	// Load taxonomy resolver
	var resolver format.TaxonomyResolver
	if options.taxonomyPath != "" {
		store, err := drupal.LoadTaxonomyFile(options.taxonomyPath)
		if err != nil {
			return fmt.Errorf("loading taxonomy file: %w", err)
		}
		resolver = store
		fmt.Fprintf(os.Stderr, "Loaded %d taxonomy terms, %d nodes\n", store.TermCount(), store.NodeCount())
	}

	// Parse input
	parseOpts := &format.ParseOptions{
		Profile:          sourceMapping,
		SystemProfile:    sourceProfile,
		TaxonomyResolver: resolver,
		StripHTML:        options.stripHTML,
		SourceName:       inputName,
		BaseURL:          options.baseURL,
		Spec:             transformation,
		Strict:           transformation != nil,
	}
	if transformation != nil && targetProfile != nil && fromFormat == "csv" && toFormat == "islandora-workbench" {
		// Profile-bound identifier columns are source cells, but their authority
		// and validation rules belong to the exact target Drupal profile.
		parseOpts.ValueProfile = targetProfile
	}

	dataset, err := format.ParseDataset(parser, input, parseOpts)
	if err != nil {
		return fmt.Errorf("parsing input: %w", err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Parsed %d records\n", len(dataset.Records))

	// Serialize output
	serializeOpts := &format.SerializeOptions{
		Profile:             targetMapping,
		SystemProfile:       targetProfile,
		Columns:             options.columns,
		MultiValueSeparator: options.multiValueSeparator,
		IncludeHeader:       true,
		Pretty:              options.pretty,
		ReferenceDOIs:       options.referenceDOIs,
		Spec:                transformation,
	}
	if toFormat == "islandora-workbench" && serializeOpts.Spec == nil {
		serializeOpts.Spec, err = compatibilityWorkbenchSpec(options.multiValueSeparator)
		if err != nil {
			return err
		}
	}

	if transformation == nil && len(serializeOpts.Columns) == 0 && toFormat == "csv" {
		serializeOpts.Columns = csvfmt.DefaultColumns()
	}

	serialize := func(output io.Writer) error {
		if err := format.SerializeDataset(serializer, output, dataset, serializeOpts); err != nil {
			return fmt.Errorf("serializing output: %w", err)
		}
		return nil
	}
	if options.outputPath != "" {
		return writeOutputFile(options.outputPath, serialize)
	}
	return serialize(cmd.OutOrStdout())
}

// compatibilityWorkbenchSpec makes use of the legacy Workbench layout an
// explicit caller decision. Direct Workbench serializers require a sealed
// specification so profile binding and artifact provenance cannot be bypassed.
func compatibilityWorkbenchSpec(separator string) (*transformationspec.Transformation, error) {
	transformation := transformationspec.FabricatorWorkbench()
	if separator != "" && separator != transformation.Target.MultiValueSeparator {
		transformation.Target.MultiValueSeparator = separator
		if err := transformation.SealFingerprint(); err != nil {
			return nil, fmt.Errorf("sealing built-in Workbench transformation: %w", err)
		}
	}
	if err := transformation.ValidateSealed(); err != nil {
		return nil, fmt.Errorf("validating built-in Workbench transformation: %w", err)
	}
	return transformation, nil
}

func loadTransformationSpec(specPath, fromFormat, toFormat string) (*transformationspec.Transformation, error) {
	if strings.TrimSpace(specPath) == "" {
		return nil, nil
	}
	file, err := os.Open(specPath)
	if err != nil {
		return nil, fmt.Errorf("opening transformation specification: %w", err)
	}
	transformation, loadErr := transformationspec.Load(file)
	closeErr := file.Close()
	if loadErr != nil {
		return nil, fmt.Errorf("loading transformation specification: %w", loadErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("closing transformation specification: %w", closeErr)
	}
	if transformation.Source.Format != fromFormat {
		return nil, fmt.Errorf("transformation source format %q does not match requested source %q", transformation.Source.Format, fromFormat)
	}
	if transformation.Target.Format != toFormat {
		return nil, fmt.Errorf("transformation target format %q does not match requested target %q", transformation.Target.Format, toFormat)
	}
	return transformation, nil
}

type doiHandleResponse struct {
	ResponseCode int `json:"responseCode"`
}

func validateReferenceDOIValues(ctx context.Context, values []string) error {
	if ctx == nil {
		return fmt.Errorf("validating reference DOIs: context is required")
	}
	client := source.NewClient()
	client.MaxResponseBytes = 1 << 20
	seen := make(map[string]bool)

	for _, raw := range values {
		doi := crossreffmt.NormalizeReferenceDOI(raw)
		if doi == "" || seen[doi] {
			continue
		}
		seen[doi] = true

		handleURL := "https://doi.org/api/handles/" + url.PathEscape(doi)
		document, err := client.FetchRequest(ctx, source.Request{
			URL: handleURL, Accept: "application/json", RedirectPolicy: source.RedirectHTTPS,
		})
		if err != nil {
			return fmt.Errorf("validating reference DOI %q: %w", doi, err)
		}

		var handle doiHandleResponse
		if err := json.Unmarshal(document.Data, &handle); err != nil {
			return fmt.Errorf("validating reference DOI %q: decoding DOI.org response: %w", doi, err)
		}
		if handle.ResponseCode != 1 {
			return fmt.Errorf("reference DOI %q did not resolve through DOI.org: responseCode %d", doi, handle.ResponseCode)
		}
	}

	return nil
}

func defaultStaticProfile(formatName string) *mapping.Profile {
	// Use generated spoke code as default profile if available
	if staticProfile, ok := spokeregistry.ProfileFrom(formatName); ok {
		return staticProfile
	}
	return nil
}

func loadSystemProfile(name, formatName, direction string) (*profile.Compiled, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	stored, err := profile.LoadStored(name)
	if err != nil {
		return nil, fmt.Errorf("loading %s profile %q: %w", direction, name, err)
	}
	wantSystem := formatName
	if direction == "target" && formatName == "islandora-workbench" {
		wantSystem = "drupal"
	}
	if stored.Compiled.System() != wantSystem {
		return nil, fmt.Errorf("%s profile %q targets system %q, not format %q", direction, name, stored.Compiled.System(), formatName)
	}
	return stored.Compiled, nil
}
