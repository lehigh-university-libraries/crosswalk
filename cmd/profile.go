package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lehigh-university-libraries/crosswalk/profile"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage mapping profiles",
	Long: `Manage mapping profiles for converting between formats.

Profiles define how source fields map to hub fields. They are stored
in ~/.crosswalk/profiles/ and can be auto-discovered or explicitly
specified with --profile.

Examples:
  # Create a profile from a Drupal config/sync directory
  crosswalk profile create drupal my-site --from-config ./config/sync

  # Create a profile interactively from a CSV file
  crosswalk profile create csv my-template --from-file sample.csv

  # List all profiles
  crosswalk profile list

  # Show a profile's contents
  crosswalk profile show my-site

  # Delete a profile
  crosswalk profile delete my-site`,
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available profiles",
	RunE:  runProfileList,
}

var profileShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show a profile's contents",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileShow,
}

var profileDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileDelete,
}

var profileCreateCmd = &cobra.Command{
	Use:   "create <format> <name>",
	Short: "Create a new profile",
	Long: `Create a new mapping profile.

For Drupal:
  crosswalk profile create drupal my-site --from-config ./config/sync

  This reads the Drupal configuration export directory and auto-generates
  field mappings based on field names and types.

For CSV:
  crosswalk profile create csv my-template --from-file sample.csv

  This walks through each column in the CSV and asks you to map it to
  a hub field.`,
	Args: cobra.ExactArgs(2),
	RunE: runProfileCreate,
}

var (
	profileFromConfig string
	profileFromFile   string
	profileAutoMap    bool
)

func init() {
	rootCmd.AddCommand(profileCmd)

	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileShowCmd)
	profileCmd.AddCommand(profileDeleteCmd)
	profileCmd.AddCommand(profileCreateCmd)

	profileCreateCmd.Flags().StringVar(&profileFromConfig, "from-config", "", "Path to Drupal config/sync directory")
	profileCreateCmd.Flags().StringVar(&profileFromFile, "from-file", "", "Path to sample CSV file")
	profileCreateCmd.Flags().BoolVar(&profileAutoMap, "auto", false, "Auto-generate mappings without interactive prompts")
}

func runProfileList(cmd *cobra.Command, args []string) error {
	profiles, err := profile.List()
	if err != nil {
		return err
	}

	if len(profiles) == 0 {
		fmt.Println("No profiles found.")
		fmt.Println("\nCreate one with:")
		fmt.Println("  crosswalk profile create drupal <name> --from-config <path>")
		fmt.Println("  crosswalk profile create csv <name> --from-file <path>")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tFORMAT\tDESCRIPTION")
	fmt.Fprintln(w, "----\t------\t-----------")

	for _, name := range profiles {
		p, err := profile.Load(name)
		if err != nil {
			fmt.Fprintf(w, "%s\t?\terror loading\n", name)
			continue
		}
		desc := p.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.Format, desc)
	}
	w.Flush()

	return nil
}

func runProfileShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	p, err := profile.Load(name)
	if err != nil {
		return err
	}

	fmt.Printf("Name:        %s\n", p.Name)
	fmt.Printf("Format:      %s\n", p.Format)
	fmt.Printf("Description: %s\n", p.Description)

	if p.Source.DrupalSiteName != "" {
		fmt.Printf("Drupal Site: %s\n", p.Source.DrupalSiteName)
	}
	if p.Source.DrupalSiteUUID != "" {
		fmt.Printf("Site UUID:   %s\n", p.Source.DrupalSiteUUID)
	}
	if p.Source.ConfigPath != "" {
		fmt.Printf("Config Path: %s\n", p.Source.ConfigPath)
	}
	if len(p.Source.CSVColumns) > 0 {
		fmt.Printf("CSV Columns: %d columns\n", len(p.Source.CSVColumns))
	}

	fmt.Printf("\nOptions:\n")
	fmt.Printf("  Multi-value separator: %s\n", p.GetMultiValueSeparator())
	if p.Options.CSVDelimiter != "" {
		fmt.Printf("  CSV delimiter:         %s\n", p.Options.CSVDelimiter)
	}
	fmt.Printf("  Strip HTML:            %v\n", p.Options.StripHTML)
	if p.Options.TaxonomyMode != "" {
		fmt.Printf("  Taxonomy mode:         %s\n", p.Options.TaxonomyMode)
	}

	fmt.Printf("\nField Mappings (%d fields):\n", len(p.Fields))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  SOURCE FIELD\tHUB FIELD\tOPTIONS")
	fmt.Fprintln(w, "  ------------\t---------\t-------")

	for source, mapping := range p.Fields {
		if mapping.Skip {
			fmt.Fprintf(w, "  %s\t(skip)\t\n", source)
			continue
		}

		opts := ""
		if mapping.Type != "" {
			opts += "type=" + mapping.Type + " "
		}
		if mapping.DateType != "" {
			opts += "date=" + mapping.DateType + " "
		}
		if mapping.Vocabulary != "" {
			opts += "vocab=" + mapping.Vocabulary + " "
		}
		if mapping.Resolve != "" {
			opts += "resolve=" + mapping.Resolve + " "
		}
		if mapping.MultiValue {
			opts += "multi "
		}
		if mapping.Priority != 0 {
			opts += fmt.Sprintf("priority=%d ", mapping.Priority)
		}

		fmt.Fprintf(w, "  %s\t%s\t%s\n", source, mapping.Hub, opts)
	}
	w.Flush()

	return nil
}

func runProfileDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	if !profile.Exists(name) {
		return fmt.Errorf("profile %q not found", name)
	}

	if err := profile.Delete(name); err != nil {
		return err
	}

	fmt.Printf("Deleted profile: %s\n", name)
	return nil
}

func runProfileCreate(cmd *cobra.Command, args []string) error {
	format := args[0]
	name := args[1]

	// Check if profile already exists
	if profile.Exists(name) {
		return fmt.Errorf("profile %q already exists; delete it first or choose a different name", name)
	}

	var p *profile.Profile
	var err error

	switch format {
	case "drupal":
		if profileFromConfig == "" {
			return fmt.Errorf("--from-config is required for Drupal profiles")
		}
		p, err = profile.CreateDrupalProfile(name, profileFromConfig)
		if err != nil {
			return fmt.Errorf("creating Drupal profile: %w", err)
		}

	case "csv":
		if profileFromFile == "" {
			return fmt.Errorf("--from-file is required for CSV profiles")
		}
		if profileAutoMap {
			// Read columns and auto-generate
			columns, _, err := readCSVColumns(profileFromFile)
			if err != nil {
				return err
			}
			p = profile.CreateCSVProfileFromColumns(name, columns)
		} else {
			// Interactive wizard
			p, err = profile.CreateCSVProfileInteractive(name, profileFromFile, nil)
			if err != nil {
				return fmt.Errorf("creating CSV profile: %w", err)
			}
		}

	default:
		return fmt.Errorf("unknown format: %s (use 'drupal' or 'csv')", format)
	}

	if err := p.Save(); err != nil {
		return fmt.Errorf("saving profile: %w", err)
	}

	path, _ := profile.ProfilePath(name)
	fmt.Printf("Created profile: %s\n", name)
	fmt.Printf("Saved to: %s\n", path)
	fmt.Printf("\nMapped %d fields. Review with:\n", len(p.Fields))
	fmt.Printf("  crosswalk profile show %s\n", name)

	return nil
}

func readCSVColumns(path string) ([]string, []string, error) {
	// Reuse the profile package's function
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var columns []string
	var sample []string

	// Simple CSV header reading
	buf := make([]byte, 4096)
	n, err := f.Read(buf)
	if err != nil {
		return nil, nil, err
	}

	lines := string(buf[:n])
	for i, line := range splitLines(lines) {
		if i == 0 {
			columns = splitCSV(line)
		} else if i == 1 {
			sample = splitCSV(line)
			break
		}
	}

	return columns, sample, nil
}

func splitLines(s string) []string {
	var lines []string
	var current string
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, current)
			current = ""
		} else if c != '\r' {
			current += string(c)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func splitCSV(line string) []string {
	var fields []string
	var current string
	inQuotes := false

	for _, c := range line {
		switch c {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if inQuotes {
				current += string(c)
			} else {
				fields = append(fields, current)
				current = ""
			}
		default:
			current += string(c)
		}
	}
	fields = append(fields, current)
	return fields
}
