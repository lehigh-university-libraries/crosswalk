package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/lehigh-university-libraries/crosswalk/spoke"
	"github.com/spf13/cobra"
)

var spokeCmd = &cobra.Command{
	Use:   "spoke",
	Short: "Manage protobuf spoke schemas",
	Long: `Manage protobuf spoke schemas for well-defined formats.

Spokes are protobuf schemas that provide type-safe representations of
source or target formats. Use spokes when you want first-class Go types
instead of dynamic profiles.

Examples:
  # Create a spoke from a Drupal config directory
  crosswalk spoke create drupal islandora --bundle islandora_object \
    --from-config ./islandora-starter-site/config/sync

  # List existing spokes
  crosswalk spoke list

  # Show spoke info
  crosswalk spoke show islandora`,
}

var spokeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available spokes",
	RunE:  runSpokeList,
}

var spokeShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show spoke information",
	Args:  cobra.ExactArgs(1),
	RunE:  runSpokeShow,
}

var spokeCreateCmd = &cobra.Command{
	Use:   "create <format> <name>",
	Short: "Create a new spoke from configuration",
	Long: `Create a new spoke from Drupal configuration.

For Drupal (generates a .proto file):
  crosswalk spoke create drupal islandora --bundle islandora_object \
    --from-config ./config/sync

  This reads the Drupal configuration and generates a .proto file with
  typed fields matching the content type definition.

For Islandora Workbench:
  crosswalk spoke create islandora-workbench islandora-workbench \
    --bundle islandora_object --from-config ./config/sync

  Generates a .proto file for the Islandora Workbench CSV format. The fields
  match Drupal field names used as Workbench column headers. Use this as a
  starting point for sites running standard Islandora without customized field
  configurations.

Interactive Mode:
  crosswalk spoke create drupal islandora --bundle islandora_object \
    --from-config ./config/sync --interactive

  With --interactive (-i), you'll be prompted to map each field to Hub targets.

The generated Drupal proto is placed in spoke/<name>/v1/<name>.proto and
can be compiled with 'make generate'.`,
	Args: cobra.ExactArgs(2),
	RunE: runSpokeCreate,
}

var (
	spokeFromConfig   string
	spokeBundle       string
	spokeOutput       string
	spokeInteractive  bool
	spokeForceReplace bool
	spokeNoHub        bool
)

func init() {
	rootCmd.AddCommand(spokeCmd)

	spokeCmd.AddCommand(spokeListCmd)
	spokeCmd.AddCommand(spokeShowCmd)
	spokeCmd.AddCommand(spokeCreateCmd)

	spokeCreateCmd.Flags().StringVar(&spokeFromConfig, "from-config", "", "Path to Drupal config/sync directory")
	spokeCreateCmd.Flags().StringVar(&spokeBundle, "bundle", "", "Drupal bundle/content type to generate (e.g., islandora_object)")
	spokeCreateCmd.Flags().StringVarP(&spokeOutput, "output", "o", "", "Output path (default: spoke/<name>/v1/<name>.proto)")
	spokeCreateCmd.Flags().BoolVarP(&spokeInteractive, "interactive", "i", false, "Interactively prompt for Hub field mappings")
	spokeCreateCmd.Flags().BoolVarP(&spokeForceReplace, "force", "f", false, "Overwrite existing spoke (reads existing mappings for autofill)")
	spokeCreateCmd.Flags().BoolVar(&spokeNoHub, "no-hub", false, "Skip hub.v1 annotations (generate plain proto only)")
}

func runSpokeList(cmd *cobra.Command, args []string) error {
	// Find all spoke directories
	spokeDir := "spoke"
	entries, err := os.ReadDir(spokeDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No spokes found.")
			fmt.Println("\nCreate one with:")
			fmt.Println("  crosswalk spoke create drupal <name> --bundle <bundle> --from-config <path>")
			return nil
		}
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tPROTO FILE")
	fmt.Fprintln(w, "----\t-------\t----------")

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Find version directories
		versionDir := filepath.Join(spokeDir, name)
		versions, err := os.ReadDir(versionDir)
		if err != nil {
			continue
		}

		for _, v := range versions {
			if !v.IsDir() || !strings.HasPrefix(v.Name(), "v") {
				continue
			}
			version := v.Name()
			protoFile := filepath.Join(versionDir, version, name+".proto")
			if _, err := os.Stat(protoFile); err == nil {
				fmt.Fprintf(w, "%s\t%s\t%s\n", name, version, protoFile)
			}
		}
	}
	w.Flush()

	return nil
}

func runSpokeShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Find the spoke
	protoPath := filepath.Join("spoke", name, "v1", name+".proto")
	if _, err := os.Stat(protoPath); err != nil {
		return fmt.Errorf("spoke %q not found at %s", name, protoPath)
	}

	// Read and display the proto file
	data, err := os.ReadFile(protoPath)
	if err != nil {
		return err
	}

	fmt.Printf("Spoke: %s\n", name)
	fmt.Printf("Proto: %s\n", protoPath)
	fmt.Printf("\n--- Contents ---\n")
	fmt.Println(string(data))

	return nil
}

func runSpokeCreate(cmd *cobra.Command, args []string) error {
	format := args[0]
	name := args[1]

	outputPath := spokeOutput
	if outputPath == "" {
		outputPath = filepath.Join("spoke", name, "v1", name+".proto")
	}

	existingProto := ""
	if _, err := os.Stat(outputPath); err == nil {
		if !spokeForceReplace {
			return fmt.Errorf("spoke already exists at %s; use --force to regenerate (existing mappings will be preserved)", outputPath)
		}
		existingProto = outputPath
		fmt.Printf("Regenerating spoke from %s (existing mappings will be used as defaults)\n", outputPath)
	}

	var proto *spoke.ProtoFile
	var err error

	switch format {
	case "drupal", "islandora-workbench":
		if spokeFromConfig == "" {
			return fmt.Errorf("--from-config is required for %s spokes", format)
		}
		if spokeBundle == "" {
			return fmt.Errorf("--bundle is required for %s spokes (e.g., --bundle islandora_object)", format)
		}
		proto, err = spoke.GenerateDrupalSpoke(name, spokeBundle, spokeFromConfig)
		if err != nil {
			return fmt.Errorf("generating %s spoke: %w", format, err)
		}
		proto.FormatName = format

	default:
		return fmt.Errorf("unknown format: %s (use 'drupal' or 'islandora-workbench')", format)
	}

	// Apply Hub mappings unless --no-hub is set
	if !spokeNoHub {
		if spokeInteractive {
			// Interactive mode: prompt for each field
			autofillPath := existingProto
			if autofillPath == "" {
				autofillPath = outputPath // Will just return empty mappings if doesn't exist
			}
			if err := spoke.ApplyInteractiveMappings(proto, autofillPath); err != nil {
				return fmt.Errorf("interactive mapping: %w", err)
			}
		} else {
			// Auto mode: use RDF predicates and field name heuristics
			spoke.ApplyAutoMappings(proto)
		}
	}

	// Write the proto file
	if err := spoke.WriteProto(proto, outputPath); err != nil {
		return fmt.Errorf("writing proto: %w", err)
	}

	fmt.Printf("\nCreated spoke: %s\n", name)
	fmt.Printf("Proto file: %s\n", outputPath)
	if proto.UseHubOptions {
		fmt.Printf("Hub mappings: enabled\n")
	}
	fmt.Printf("\nGenerate Go code with:\n")
	fmt.Printf("  make generate\n")
	fmt.Printf("\nOr manually:\n")
	fmt.Printf("  buf generate\n")

	return nil
}
