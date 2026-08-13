package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	omekaformat "github.com/lehigh-university-libraries/crosswalk/format/omeka_s"
	"github.com/lehigh-university-libraries/crosswalk/model"
	modeldrupal "github.com/lehigh-university-libraries/crosswalk/model/drupal"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	rootCmd.AddCommand(newProfileCmd())
}

func newProfileCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "profile",
		Short: "Manage instance-specific metadata profiles",
		Long: `Manage strict, fingerprinted metadata profiles and their immutable model
snapshots. Crosswalk reads supplied configuration snapshots but performs no site
discovery or network access; acquiring snapshots is a sitectl responsibility.`,
	}
	command.AddCommand(newProfileCreateCmd())
	command.AddCommand(newProfileValidateCmd())
	command.AddCommand(newProfilePublishCmd())
	command.AddCommand(newProfileListCmd())
	command.AddCommand(newProfileShowCmd())
	command.AddCommand(newProfileDeleteCmd())
	return command
}

func newProfileCreateCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a profile from a system model snapshot",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("a profile system subcommand is required")
		},
	}
	command.AddCommand(newProfileCreateDrupalCmd())
	command.AddCommand(newProfileCreateOmekaSCmd())
	return command
}

func newProfileCreateOmekaSCmd() *cobra.Command {
	var snapshotPath string
	var templateID int64
	var outputPath string
	var institutionField string
	var institutionScheme string
	var institutionNamespace string
	var institutionPattern string
	var institutionIdentityLevel string
	command := &cobra.Command{
		Use:   "omeka-s <name>",
		Short: "Create an editable Omeka S profile draft from a supplied schema snapshot",
		Long: `Compile a bounded Omeka S acquisition snapshot into an immutable model,
then write an ordered editable profile draft for either the installation-wide
property model or one resource template. Crosswalk performs no API or database
calls; sitectl or another acquisition tool must capture the snapshot first.
Validate the reviewed draft to seal it, then publish the sealed definition.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(snapshotPath) == "" {
				return fmt.Errorf("--snapshot is required")
			}
			institutional, err := omekaInstitutionalIdentifierOptions(
				institutionField, institutionScheme, institutionNamespace,
				institutionPattern, institutionIdentityLevel,
			)
			if err != nil {
				return err
			}
			snapshot, err := compileOmekaModelPath(snapshotPath)
			if err != nil {
				return err
			}
			definition, err := profile.NewOmekaSDefinition(snapshot, profile.OmekaSDefinitionOptions{
				Name: args[0], ResourceTemplateID: templateID, InstitutionalIdentifier: institutional,
			})
			if err != nil {
				return err
			}
			if err := profile.StoreModel(snapshot); err != nil {
				return err
			}
			return writeProfileDefinition(command, definition, outputPath)
		},
	}
	command.Flags().StringVar(&snapshotPath, "snapshot", "", "Omeka S schema acquisition snapshot JSON")
	command.Flags().Int64Var(&templateID, "resource-template-id", 0, "resource template ID (default installation-wide property model)")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "editable profile draft path, or - for stdout")
	command.Flags().StringVar(&institutionField, "institution-field", "", "Omeka property term containing an institutional identifier")
	command.Flags().StringVar(&institutionScheme, "institution-scheme", "", "distinct machine name for an institutional identifier scheme")
	command.Flags().StringVar(&institutionNamespace, "institution-namespace", "", "absolute authority URI for the institutional identifier scheme")
	command.Flags().StringVar(&institutionPattern, "institution-pattern", "", "fully anchored Go regular expression for institutional identifier values")
	command.Flags().StringVar(&institutionIdentityLevel, "institution-identity-level", string(profile.IdentitySourceRecord), "identity level: work, version, manifestation, concept, or source_record")
	return command
}

func newProfileCreateDrupalCmd() *cobra.Command {
	var configPath string
	var entityType string
	var bundle string
	var outputPath string
	var institutionAttribute string
	var institutionScheme string
	var institutionNamespace string
	var institutionPattern string
	var institutionIdentityLevel string
	command := &cobra.Command{
		Use:   "drupal <name>",
		Short: "Create an editable Drupal profile draft from a supplied config snapshot",
		Long: `Compile a Drupal config/sync directory or gzip-compressed tar export into
an immutable model, then write an ordered editable profile draft for one entity
and bundle. Validate the edited draft to seal its executable fingerprint, then
publish that sealed definition. Local identifiers become exact duplicate evidence
only when an explicit institution scheme, authority URI, and anchored validation
pattern are supplied.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}
			if entityType == "" {
				return fmt.Errorf("--entity-type is required")
			}
			if bundle == "" {
				return fmt.Errorf("--bundle is required")
			}
			institutional, err := institutionalIdentifierOptions(
				institutionAttribute, institutionScheme, institutionNamespace,
				institutionPattern, institutionIdentityLevel,
			)
			if err != nil {
				return err
			}
			snapshot, err := compileDrupalModelPath(configPath)
			if err != nil {
				return err
			}
			definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
				Name: args[0], EntityType: entityType, Bundle: bundle,
				InstitutionalIdentifier: institutional,
			})
			if err != nil {
				return err
			}
			if err := profile.StoreModel(snapshot); err != nil {
				return err
			}
			return writeProfileDefinition(command, definition, outputPath)
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "Drupal config/sync directory or config-export .tar.gz")
	command.Flags().StringVar(&entityType, "entity-type", "node", "Drupal entity type")
	command.Flags().StringVar(&bundle, "bundle", "", "Drupal bundle, such as islandora_object")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "editable profile draft path, or - for stdout")
	command.Flags().StringVar(&institutionAttribute, "institution-attribute", "local", "typed identifier attribute used in Drupal")
	command.Flags().StringVar(&institutionScheme, "institution-scheme", "", "distinct machine name for an institutional identifier scheme")
	command.Flags().StringVar(&institutionNamespace, "institution-namespace", "", "absolute authority URI for the institutional identifier scheme")
	command.Flags().StringVar(&institutionPattern, "institution-pattern", "", "fully anchored Go regular expression for institutional identifier values")
	command.Flags().StringVar(&institutionIdentityLevel, "institution-identity-level", string(profile.IdentitySourceRecord), "identity level: work, version, manifestation, concept, or source_record")
	return command
}

func newProfileValidateCmd() *cobra.Command {
	var inputPath string
	var outputPath string
	command := &cobra.Command{
		Use:          "validate",
		Short:        "Validate and seal an editable profile draft",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			draft, err := readProfileDraft(command, inputPath)
			if err != nil {
				return err
			}
			snapshot, err := profile.LoadModel(draft.ModelFingerprint)
			if err != nil {
				return fmt.Errorf("loading draft model: %w", err)
			}
			sealed, _, err := profile.PrepareDefinition(snapshot, draft)
			if err != nil {
				return err
			}
			return writeProfileDefinition(command, sealed, outputPath)
		},
	}
	command.Flags().StringVarP(&inputPath, "input", "i", "-", "editable profile draft path, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "sealed profile path, or - for stdout")
	return command
}

func newProfilePublishCmd() *cobra.Command {
	var inputPath string
	var force bool
	command := &cobra.Command{
		Use:          "publish",
		Short:        "Publish a sealed profile definition",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			definition, err := readSealedProfile(command, inputPath)
			if err != nil {
				return err
			}
			snapshot, err := profile.LoadModel(definition.ModelFingerprint)
			if err != nil {
				return fmt.Errorf("loading profile model: %w", err)
			}
			if _, err := profile.Publish(snapshot, definition, profile.PublishOptions{Force: force}); err != nil {
				return err
			}
			profilePath, err := profile.ProfilePath(definition.Name)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "Published profile %s at %s\n", definition.Name, profilePath); err != nil {
				return fmt.Errorf("writing profile publication result: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVarP(&inputPath, "input", "i", "-", "sealed profile definition path, or - for stdin")
	command.Flags().BoolVarP(&force, "force", "f", false, "atomically replace an existing profile with the same name")
	return command
}

const maxAuthoringInputBytes = int64(8 << 20)

func readProfileDraft(command *cobra.Command, inputPath string) (*profile.Definition, error) {
	data, err := readAuthoringInput(command, inputPath, "profile")
	if err != nil {
		return nil, err
	}
	definition, err := profile.DecodeDraftDefinition(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decoding profile draft: %w", err)
	}
	return definition, nil
}

func readSealedProfile(command *cobra.Command, inputPath string) (*profile.Definition, error) {
	data, err := readAuthoringInput(command, inputPath, "profile")
	if err != nil {
		return nil, err
	}
	definition, err := profile.DecodeDefinition(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decoding sealed profile: %w", err)
	}
	return definition, nil
}

func readAuthoringInput(command *cobra.Command, inputPath, kind string) (_ []byte, returnErr error) {
	reader := command.InOrStdin()
	var file *os.File
	path := strings.TrimSpace(inputPath)
	if path != "" && path != "-" {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("accessing %s input: %w", kind, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s input %q must be a regular file, not a symbolic link", kind, path)
		}
		file, err = os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("opening %s input: %w", kind, err)
		}
		reader = file
		defer func() {
			if closeErr := file.Close(); closeErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("closing %s input: %w", kind, closeErr))
			}
		}()
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxAuthoringInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s input: %w", kind, err)
	}
	if int64(len(data)) > maxAuthoringInputBytes {
		return nil, fmt.Errorf("%s input exceeds %d bytes", kind, maxAuthoringInputBytes)
	}
	return data, nil
}

func writeProfileDefinition(command *cobra.Command, definition *profile.Definition, outputPath string) error {
	data, err := yaml.Marshal(definition)
	if err != nil {
		return fmt.Errorf("encoding profile definition: %w", err)
	}
	write := func(writer io.Writer) error {
		if _, err := writer.Write(data); err != nil {
			return fmt.Errorf("writing profile definition: %w", err)
		}
		return nil
	}
	if path := strings.TrimSpace(outputPath); path != "" && path != "-" {
		return writeOutputFile(path, write)
	}
	return write(command.OutOrStdout())
}

func newProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List canonical profiles",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			names, err := profile.ListDefinitions()
			if err != nil {
				return err
			}
			writer := command.OutOrStdout()
			if len(names) == 0 {
				if _, err := fmt.Fprintln(writer, "No profiles found."); err != nil {
					return fmt.Errorf("writing profile list: %w", err)
				}
				return nil
			}
			table := tabwriter.NewWriter(writer, 0, 0, 2, ' ', 0)
			if _, err := fmt.Fprintln(table, "NAME\tSYSTEM\tENTITY\tMODEL SHA-256\tPROFILE SHA-256"); err != nil {
				return fmt.Errorf("writing profile list: %w", err)
			}
			for _, name := range names {
				stored, err := profile.LoadStored(name)
				if err != nil {
					return err
				}
				entity := "-"
				if stored.Definition.Identity != nil {
					entity = stored.Definition.Identity.Repository.EntityType
					if stored.Definition.Identity.Repository.Bundle != "" {
						entity += "/" + stored.Definition.Identity.Repository.Bundle
					}
				}
				if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", name, stored.Definition.System, entity, stored.Definition.ModelFingerprint, stored.Definition.Fingerprint.Value); err != nil {
					return fmt.Errorf("writing profile list: %w", err)
				}
			}
			if err := table.Flush(); err != nil {
				return fmt.Errorf("flushing profile list: %w", err)
			}
			return nil
		},
	}
}

func newProfileShowCmd() *cobra.Command {
	var showModel bool
	command := &cobra.Command{
		Use:          "show <name>",
		Short:        "Show a canonical profile or its exact model",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			stored, err := profile.LoadStored(args[0])
			if err != nil {
				return err
			}
			encoder := yaml.NewEncoder(command.OutOrStdout())
			encoder.SetIndent(2)
			value := any(stored.Definition)
			if showModel {
				value = stored.Model
			}
			if err := encoder.Encode(value); err != nil {
				closeErr := encoder.Close()
				return errors.Join(fmt.Errorf("encoding stored profile: %w", err), closeErr)
			}
			if err := encoder.Close(); err != nil {
				return fmt.Errorf("closing profile encoder: %w", err)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&showModel, "model", false, "show the immutable model snapshot instead of the profile definition")
	return command
}

func newProfileDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "delete <name>",
		Short:        "Delete a profile while retaining shared immutable models",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			if err := profile.DeleteDefinition(args[0]); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "Deleted profile: %s\n", args[0]); err != nil {
				return fmt.Errorf("writing profile deletion result: %w", err)
			}
			return nil
		},
	}
}

func compileDrupalModelPath(configPath string) (_ *model.Snapshot, returnErr error) {
	info, err := os.Lstat(configPath)
	if err != nil {
		return nil, fmt.Errorf("accessing Drupal config %q: %w", configPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("drupal config %q must not be a symbolic link", configPath)
	}
	if info.IsDir() {
		return modeldrupal.CompileDirectory(configPath, modeldrupal.CompileOptions{})
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("drupal config %q is not a directory or regular archive", configPath)
	}
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("opening Drupal config archive: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("closing Drupal config archive: %w", closeErr))
		}
	}()
	return modeldrupal.CompileArchive(file, modeldrupal.CompileOptions{})
}

func compileOmekaModelPath(snapshotPath string) (_ *model.Snapshot, returnErr error) {
	info, err := os.Lstat(snapshotPath)
	if err != nil {
		return nil, fmt.Errorf("accessing Omeka S snapshot %q: %w", snapshotPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("omeka S snapshot %q must be a regular file, not a symbolic link", snapshotPath)
	}
	file, err := os.Open(snapshotPath)
	if err != nil {
		return nil, fmt.Errorf("opening Omeka S snapshot: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("closing Omeka S snapshot: %w", closeErr))
		}
	}()
	return omekaformat.CompileModelJSON(file)
}

func institutionalIdentifierOptions(attribute, scheme, namespace, pattern, level string) (*profile.InstitutionalIdentifierOptions, error) {
	if scheme == "" && namespace == "" && pattern == "" {
		return nil, nil
	}
	if scheme == "" || namespace == "" || pattern == "" {
		return nil, fmt.Errorf("--institution-scheme, --institution-namespace, and --institution-pattern must be supplied together")
	}
	identityLevel := profile.IdentifierIdentityLevel(level)
	switch identityLevel {
	case profile.IdentityWork, profile.IdentityVersion, profile.IdentityManifestation, profile.IdentityConcept, profile.IdentitySourceRecord:
	default:
		return nil, fmt.Errorf("unsupported institutional identity level %q", level)
	}
	return &profile.InstitutionalIdentifierOptions{
		Attribute: attribute, Scheme: scheme, NamespaceURI: namespace,
		Pattern: pattern, IdentityLevel: identityLevel,
	}, nil
}

func omekaInstitutionalIdentifierOptions(field, scheme, namespace, pattern, level string) (*profile.OmekaSInstitutionalIdentifierOptions, error) {
	if field == "" && scheme == "" && namespace == "" && pattern == "" {
		return nil, nil
	}
	if strings.TrimSpace(field) == "" || strings.TrimSpace(scheme) == "" || strings.TrimSpace(namespace) == "" || strings.TrimSpace(pattern) == "" {
		return nil, fmt.Errorf("--institution-field, --institution-scheme, --institution-namespace, and --institution-pattern must be supplied together")
	}
	identityLevel := profile.IdentifierIdentityLevel(level)
	switch identityLevel {
	case profile.IdentityWork, profile.IdentityVersion, profile.IdentityManifestation, profile.IdentityConcept, profile.IdentitySourceRecord:
	default:
		return nil, fmt.Errorf("unsupported institutional identity level %q", level)
	}
	return &profile.OmekaSInstitutionalIdentifierOptions{
		FieldPath: field, Scheme: scheme, NamespaceURI: namespace,
		Pattern: pattern, IdentityLevel: identityLevel,
	}, nil
}
