package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	workbenchformat "github.com/lehigh-university-libraries/crosswalk/format/islandora_workbench"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	transformationspec "github.com/lehigh-university-libraries/crosswalk/spec"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newSpecCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "spec",
		Short: "Compile and inspect metadata transformation specifications",
	}
	compile := &cobra.Command{
		Use:   "compile",
		Short: "Compile a transformation specification from source schemas",
	}
	compile.AddCommand(newSpecCompileDrupalCmd())
	command.AddCommand(compile)
	command.AddCommand(newSpecValidateCmd())
	command.AddCommand(newSpecContractCmd())
	return command
}

func newSpecContractCmd() *cobra.Command {
	var specPath string
	var profileName string
	var outputPath string
	command := &cobra.Command{
		Use:          "contract",
		Short:        "Create a trusted sitectl Workbench contract from reviewed configuration",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if strings.TrimSpace(specPath) == "" {
				return fmt.Errorf("--spec is required")
			}
			transformation, err := loadTransformationSpec(specPath, "csv", "islandora-workbench")
			if err != nil {
				return err
			}
			var compiled *profile.Compiled
			if strings.TrimSpace(profileName) != "" {
				stored, err := profile.LoadStored(profileName)
				if err != nil {
					return fmt.Errorf("loading target profile %q: %w", profileName, err)
				}
				if stored.Compiled.System() != "drupal" {
					return fmt.Errorf("target profile %q uses system %q, not drupal", profileName, stored.Compiled.System())
				}
				compiled = stored.Compiled
			}
			contract, err := workbenchformat.BuildArtifactContract(transformation, compiled)
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(contract, "", "  ")
			if err != nil {
				return fmt.Errorf("encoding Workbench artifact contract: %w", err)
			}
			data = append(data, '\n')
			write := func(writer io.Writer) error {
				if _, err := writer.Write(data); err != nil {
					return fmt.Errorf("writing Workbench artifact contract: %w", err)
				}
				return nil
			}
			if path := strings.TrimSpace(outputPath); path != "" && path != "-" {
				return writeOutputFile(path, write)
			}
			return write(command.OutOrStdout())
		},
	}
	command.Flags().StringVar(&specPath, "spec", "", "reviewed, sealed Workbench transformation specification")
	command.Flags().StringVar(&profileName, "profile", "", "published Drupal target profile whose fingerprints must match the specification")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "trusted contract JSON path, or - for stdout")
	return command
}

func newSpecValidateCmd() *cobra.Command {
	var inputPath string
	var outputPath string
	var outputFormat string
	command := &cobra.Command{
		Use:          "validate",
		Short:        "Validate and seal an editable transformation specification",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			data, err := readAuthoringInput(command, inputPath, "specification")
			if err != nil {
				return err
			}
			transformation, err := transformationspec.DecodeDraft(strings.NewReader(string(data)))
			if err != nil {
				return fmt.Errorf("decoding transformation draft: %w", err)
			}
			if err := transformation.SealFingerprint(); err != nil {
				return fmt.Errorf("sealing transformation specification: %w", err)
			}
			if err := transformation.Validate(); err != nil {
				return fmt.Errorf("validating sealed transformation specification: %w", err)
			}
			return writeTransformation(command, transformation, outputPath, outputFormat)
		},
	}
	command.Flags().StringVarP(&inputPath, "input", "i", "-", "editable specification draft path, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "sealed specification path, or - for stdout")
	command.Flags().StringVar(&outputFormat, "format", "", "output format: json or yaml (inferred from --output, default yaml)")
	return command
}

func newSpecCompileDrupalCmd() *cobra.Command {
	var configPath string
	var bundle string
	var profileName string
	var outputPath string
	var outputFormat string
	command := &cobra.Command{
		Use:   "drupal",
		Short: "Compile a Workbench transformation from a Drupal config export",
		Long: `Compile a deterministic, fingerprinted transformation from a Drupal
config/sync directory or a sitectl-drupal gzip-compressed tar config-export.
The command performs no network access and never extracts archive members.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			usingProfile := strings.TrimSpace(profileName) != ""
			usingConfig := strings.TrimSpace(configPath) != "" || strings.TrimSpace(bundle) != ""
			if usingProfile && usingConfig {
				return fmt.Errorf("--profile cannot be combined with --config or --bundle")
			}
			var (
				transformation *transformationspec.Transformation
				err            error
			)
			if usingProfile {
				stored, loadErr := profile.LoadStored(profileName)
				if loadErr != nil {
					return fmt.Errorf("loading Drupal profile %q: %w", profileName, loadErr)
				}
				transformation, err = transformationspec.CompileDrupalProfile(stored.Model, stored.Compiled, transformationspec.DrupalCompileOptions{})
			} else {
				if strings.TrimSpace(configPath) == "" {
					return fmt.Errorf("--config is required when --profile is not supplied")
				}
				if strings.TrimSpace(bundle) == "" {
					return fmt.Errorf("--bundle is required when --profile is not supplied")
				}
				transformation, err = compileDrupalPath(configPath, transformationspec.DrupalCompileOptions{Bundle: bundle})
			}
			if err != nil {
				return err
			}
			return writeTransformation(command, transformation, outputPath, outputFormat)
		},
		SilenceUsage: true,
	}
	command.Flags().StringVar(&configPath, "config", "", "Drupal config/sync directory or config-export .tar.gz")
	command.Flags().StringVar(&bundle, "bundle", "", "Drupal node bundle, such as islandora_object")
	command.Flags().StringVar(&profileName, "profile", "", "published Drupal profile to bind to the Workbench transformation")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "output path, or - for stdout")
	command.Flags().StringVar(&outputFormat, "format", "", "output format: json or yaml (inferred from --output, default yaml)")
	return command
}

func compileDrupalPath(configPath string, options transformationspec.DrupalCompileOptions) (transformation *transformationspec.Transformation, returnErr error) {
	info, err := os.Stat(configPath)
	if err != nil {
		return nil, fmt.Errorf("accessing Drupal config %q: %w", configPath, err)
	}
	if info.IsDir() {
		return transformationspec.CompileDrupalDirectory(configPath, options)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("drupal config %q is not a directory or regular archive", configPath)
	}
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("opening Drupal config archive: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && returnErr == nil {
			returnErr = fmt.Errorf("closing Drupal config archive: %w", closeErr)
		}
	}()
	return transformationspec.CompileDrupalArchive(file, options)
}

func writeTransformation(command *cobra.Command, transformation *transformationspec.Transformation, outputPath, outputFormat string) error {
	formatName := strings.ToLower(strings.TrimSpace(outputFormat))
	if formatName == "" {
		switch strings.ToLower(filepath.Ext(outputPath)) {
		case ".json":
			formatName = "json"
		default:
			formatName = "yaml"
		}
	}
	if formatName != "json" && formatName != "yaml" && formatName != "yml" {
		return fmt.Errorf("unsupported specification output format %q", outputFormat)
	}

	var data []byte
	var err error
	if formatName == "json" {
		data, err = json.MarshalIndent(transformation, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding transformation JSON: %w", err)
		}
		data = append(data, '\n')
	} else {
		data, err = yaml.Marshal(transformation)
		if err != nil {
			return fmt.Errorf("encoding transformation YAML: %w", err)
		}
	}
	write := func(writer io.Writer) error {
		if _, err := writer.Write(data); err != nil {
			return fmt.Errorf("writing transformation specification: %w", err)
		}
		return nil
	}
	if path := strings.TrimSpace(outputPath); path != "" && path != "-" {
		return writeOutputFile(path, write)
	}
	return write(command.OutOrStdout())
}
