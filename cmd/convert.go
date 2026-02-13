package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lehigh-university-libraries/crosswalk/format"
	csvfmt "github.com/lehigh-university-libraries/crosswalk/format/csv"
	"github.com/lehigh-university-libraries/crosswalk/format/drupal"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
	"github.com/lehigh-university-libraries/crosswalk/profile"

	// Register all format plugins
	_ "github.com/lehigh-university-libraries/crosswalk/format/arxiv"
	_ "github.com/lehigh-university-libraries/crosswalk/format/bibtex"
	_ "github.com/lehigh-university-libraries/crosswalk/format/crossref"
	_ "github.com/lehigh-university-libraries/crosswalk/format/csl"
	_ "github.com/lehigh-university-libraries/crosswalk/format/datacite"
	_ "github.com/lehigh-university-libraries/crosswalk/format/dublincore"
	_ "github.com/lehigh-university-libraries/crosswalk/format/mods"
	_ "github.com/lehigh-university-libraries/crosswalk/format/proquest"
	_ "github.com/lehigh-university-libraries/crosswalk/format/schemaorg"
)

var (
	inputFile     string
	outputFile    string
	profileName   string
	profileFile   string
	taxonomyFile  string
	columns       []string
	multiValueSep string
	stripHTML     bool
	pretty        bool
	baseURL       string
	enrichDepth   int
)

var convertCmd = &cobra.Command{
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

  # Enrich entity references from live Drupal site
  crosswalk convert drupal csv -i data.json --base-url https://example.com`,
	Args: cobra.ExactArgs(2),
	RunE: runConvert,
}

func init() {
	convertCmd.Flags().StringVarP(&inputFile, "input", "i", "", "Input file (default: stdin)")
	convertCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file (default: stdout)")
	convertCmd.Flags().StringVarP(&profileName, "profile", "p", "", "Mapping profile name (e.g., islandora)")
	convertCmd.Flags().StringVar(&profileFile, "profile-file", "", "Custom profile YAML file")
	convertCmd.Flags().StringVar(&taxonomyFile, "taxonomy-file", "", "Taxonomy term resolution file (JSON)")
	convertCmd.Flags().StringSliceVarP(&columns, "columns", "c", nil, "CSV columns to output")
	convertCmd.Flags().StringVar(&multiValueSep, "separator", "|", "Multi-value field separator")
	convertCmd.Flags().BoolVar(&stripHTML, "strip-html", true, "Strip HTML from text fields")
	convertCmd.Flags().BoolVar(&pretty, "pretty", false, "Pretty-print JSON output")
	convertCmd.Flags().StringVar(&baseURL, "base-url", "", "Drupal site base URL for enriching entity references")
	convertCmd.Flags().IntVar(&enrichDepth, "enrich-depth", 2, "Maximum depth for recursive entity enrichment")
}

func runConvert(cmd *cobra.Command, args []string) (err error) {
	fromFormat := args[0]
	toFormat := args[1]

	// Determine input source
	var input io.Reader
	var inputName string

	if inputFile != "" {
		f, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("opening input file: %w", err)
		}
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("closing input file: %w", cerr)
			}
		}()
		input = f
		inputName = inputFile
	} else {
		input = os.Stdin
		inputName = "stdin"
	}

	// Enrich Drupal input if base URL is provided
	if baseURL != "" && fromFormat == "drupal" {
		enrichedInput, err := enrichDrupalInput(input)
		if err != nil {
			return fmt.Errorf("enriching input: %w", err)
		}
		input = enrichedInput
	}

	// Determine output destination
	var output io.Writer
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("closing output file: %w", cerr)
			}
		}()
		output = f
	} else {
		output = os.Stdout
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

	// Load profile
	profile, err := loadProfile(fromFormat)
	if err != nil {
		return fmt.Errorf("loading profile: %w", err)
	}

	// Load taxonomy resolver
	var resolver format.TaxonomyResolver
	if taxonomyFile != "" {
		store, err := drupal.LoadTaxonomyFile(taxonomyFile)
		if err != nil {
			return fmt.Errorf("loading taxonomy file: %w", err)
		}
		resolver = store
		fmt.Fprintf(os.Stderr, "Loaded %d taxonomy terms, %d nodes\n", store.TermCount(), store.NodeCount())
	}

	// Parse input
	parseOpts := &format.ParseOptions{
		Profile:          profile,
		TaxonomyResolver: resolver,
		StripHTML:        stripHTML,
		SourceName:       inputName,
		BaseURL:          baseURL,
	}

	records, err := parser.Parse(input, parseOpts)
	if err != nil {
		return fmt.Errorf("parsing input: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Parsed %d records\n", len(records))

	// Serialize output
	serializeOpts := &format.SerializeOptions{
		Profile:             profile,
		Columns:             columns,
		MultiValueSeparator: multiValueSep,
		IncludeHeader:       true,
		Pretty:              pretty,
	}

	if len(serializeOpts.Columns) == 0 && toFormat == "csv" {
		serializeOpts.Columns = csvfmt.DefaultColumns()
	}

	if err := serializer.Serialize(output, records, serializeOpts); err != nil {
		return fmt.Errorf("serializing output: %w", err)
	}

	return nil
}

func loadProfile(fromFormat string) (*mapping.Profile, error) {
	// Load from file if specified
	if profileFile != "" {
		return mapping.LoadProfile(profileFile)
	}

	// Load from user profiles by name first
	if profileName != "" {
		// Try user profile in ~/.crosswalk/profiles/
		if profile.Exists(profileName) {
			p, err := profile.Load(profileName)
			if err != nil {
				return nil, fmt.Errorf("loading user profile: %w", err)
			}
			return convertUserProfile(p), nil
		}

		// Fall back to embedded profiles
		registry, err := mapping.NewProfileRegistry()
		if err != nil {
			return nil, err
		}

		mp, ok := registry.Get(profileName)
		if !ok {
			return nil, fmt.Errorf("unknown profile: %s (not found in ~/.crosswalk/profiles/ or embedded profiles)", profileName)
		}
		return mp, nil
	}

	// Try auto-discovery from user profiles based on input file
	if inputFile != "" {
		p, err := autoDiscoverProfile(fromFormat, inputFile)
		if err == nil && p != nil {
			fmt.Fprintf(os.Stderr, "Auto-discovered profile: %s\n", p.Name)
			return convertUserProfile(p), nil
		}
	}

	// Use default embedded profile based on input format
	if fromFormat == "drupal" {
		registry, err := mapping.NewProfileRegistry()
		if err != nil {
			return nil, err
		}
		if mp, ok := registry.Get("islandora"); ok {
			return mp, nil
		}
	}

	return nil, nil
}

// autoDiscoverProfile attempts to find a matching user profile for the input.
func autoDiscoverProfile(format, inputPath string) (*profile.Profile, error) {
	switch format {
	case "csv":
		return profile.MatchCSVProfile(inputPath)
	case "drupal":
		// Read JSON and try to match based on field fingerprint
		data, err := os.ReadFile(inputPath)
		if err != nil {
			return nil, nil
		}
		return profile.MatchDrupalProfile(data)
	default:
		return nil, nil
	}
}

// enrichDrupalInput enriches entity references in Drupal JSON input.
func enrichDrupalInput(input io.Reader) (io.Reader, error) {
	// Read all input
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	// Create enricher
	enricher, err := drupal.NewEnricher(baseURL)
	if err != nil {
		return nil, fmt.Errorf("creating enricher: %w", err)
	}
	enricher.MaxDepth = enrichDepth

	fmt.Fprintf(os.Stderr, "Enriching entity references from %s...\n", baseURL)

	// Enrich the data
	enrichedData, err := enricher.Enrich(data)
	if err != nil {
		return nil, fmt.Errorf("enriching data: %w", err)
	}

	return strings.NewReader(string(enrichedData)), nil
}

// convertUserProfile converts a user profile.Profile to mapping.Profile.
func convertUserProfile(p *profile.Profile) *mapping.Profile {
	mp := &mapping.Profile{
		Name:        p.Name,
		Format:      p.Format,
		Description: p.Description,
		Fields:      make(map[string]mapping.FieldMapping),
		Options: mapping.ProfileOptions{
			MultiValueSeparator: p.Options.MultiValueSeparator,
			CSVDelimiter:        p.Options.CSVDelimiter,
			StripHTML:           p.Options.StripHTML,
			TaxonomyMode:        p.Options.TaxonomyMode,
		},
	}

	for source, fm := range p.Fields {
		mp.Fields[source] = mapping.FieldMapping{
			IR:           fm.Hub, // Hub field maps to IR in the mapping package
			Type:         fm.Type,
			Priority:     fm.Priority,
			DateType:     fm.DateType,
			Parser:       fm.Parser,
			Resolve:      fm.Resolve,
			RoleField:    fm.RoleField,
			RelationType: fm.RelationType,
			Vocabulary:   fm.Vocabulary,
			MultiValue:   fm.MultiValue,
			Delimiter:    fm.Delimiter,
		}
	}

	return mp
}
