package cmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/lehigh-university-libraries/crosswalk/server/httpapi"
	transformationspec "github.com/lehigh-university-libraries/crosswalk/spec"
)

const (
	maxWorkbenchCLIInputBytes = int64(64 << 20)
	maxWorkbenchCLIRows       = 100_002
	maxWorkbenchCLICells      = int64(1_000_000)
)

type workbenchCommandOptions struct {
	inputPath     string
	specPath      string
	drupalProfile string
	artifactDir   string
}

type workbenchValidationError struct {
	count int
}

func (e *workbenchValidationError) Error() string {
	return fmt.Sprintf("Workbench metadata validation failed in %d cell(s)", e.count)
}

func newWorkbenchCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "workbench",
		Short: "Validate a human-readable CSV and build Workbench task artifacts",
		Long: `Validate Google Sheet CSV exports and transform them into a complete,
deterministic Islandora Workbench artifact bundle. CSV is accepted directly;
callers do not need to convert it to JSON. A sealed transformation maps human
headers to canonical fields and declares workflow validation policy.`,
	}
	command.AddCommand(newWorkbenchValidateCmd())
	command.AddCommand(newWorkbenchTransformCmd())
	return command
}

func newWorkbenchValidateCmd() *cobra.Command {
	options := &workbenchCommandOptions{}
	command := &cobra.Command{
		Use:   "validate",
		Short: "Run mapping- and Drupal-model-aware Check My Work validation",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			_, rows, err := readWorkbenchCLIInput(command, options.inputPath)
			if err != nil {
				return err
			}
			engine, err := options.engine()
			if err != nil {
				return err
			}
			findings, err := engine.Check(command.Context(), rows)
			if err != nil {
				return fmt.Errorf("validating Workbench metadata: %w", err)
			}
			if len(findings) != 0 {
				if err := writeWorkbenchFindings(command.OutOrStdout(), findings); err != nil {
					return err
				}
				return &workbenchValidationError{count: len(findings)}
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), "Valid: no Workbench metadata findings")
			return err
		},
	}
	options.addInputFlags(command)
	return command
}

func newWorkbenchTransformCmd() *cobra.Command {
	options := &workbenchCommandOptions{}
	command := &cobra.Command{
		Use:   "transform",
		Short: "Validate a CSV and atomically write all Workbench task artifacts",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			data, rows, err := readWorkbenchCLIInput(command, options.inputPath)
			if err != nil {
				return err
			}
			engine, err := options.engine()
			if err != nil {
				return err
			}
			findings, err := engine.Check(command.Context(), rows)
			if err != nil {
				return fmt.Errorf("validating Workbench metadata: %w", err)
			}
			if len(findings) != 0 {
				if err := writeWorkbenchFindings(command.ErrOrStderr(), findings); err != nil {
					return err
				}
				return &workbenchValidationError{count: len(findings)}
			}
			artifacts, err := engine.Transform(command.Context(), bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("transforming Workbench metadata: %w", err)
			}
			outputs := make([]namedOutput, 0, len(artifacts))
			for _, artifact := range artifacts {
				outputs = append(outputs, namedOutput{name: artifact.Name, data: artifact.Data})
			}
			if err := writeOutputDirectory(options.artifactDir, outputs); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Wrote %d Workbench artifacts to %s\n", len(outputs), options.artifactDir)
			return err
		},
	}
	options.addInputFlags(command)
	command.Flags().StringVar(&options.artifactDir, "artifact-dir", "", "new directory for target CSVs and their manifest")
	_ = command.MarkFlagRequired("artifact-dir")
	return command
}

func (o *workbenchCommandOptions) addInputFlags(command *cobra.Command) {
	command.Flags().StringVarP(&o.inputPath, "input", "i", "", "Google Sheet CSV export (default: stdin)")
	command.Flags().StringVar(&o.specPath, "spec", "", "reviewed, sealed Workbench transformation specification")
	command.Flags().StringVar(&o.drupalProfile, "drupal-profile", "", "stored Drupal profile whose frozen model supplies field validation")
}

func (o *workbenchCommandOptions) engine() (*httpapi.CrosswalkEngine, error) {
	var systemProfile *drupalReconciliationProfile
	var err error
	if strings.TrimSpace(o.drupalProfile) != "" {
		systemProfile, err = loadDrupalReconciliationProfile(o.drupalProfile, false)
		if err != nil {
			return nil, err
		}
	}

	transformation := (*transformationspec.Transformation)(nil)
	if systemProfile != nil {
		transformation = systemProfile.transformation
	}
	if strings.TrimSpace(o.specPath) != "" {
		transformation, err = loadTransformationSpec(o.specPath, "csv", "islandora-workbench")
		if err != nil {
			return nil, err
		}
		if transformation.Fingerprint.Profile != "" && systemProfile == nil {
			return nil, errors.New("profile-bound --spec requires the exact --drupal-profile")
		}
		if transformation.Fingerprint.Profile == "" && systemProfile != nil {
			return nil, errors.New("unbound --spec cannot be combined with --drupal-profile")
		}
		if systemProfile != nil && (transformation.Fingerprint.Model != systemProfile.compiled.ModelFingerprint() || transformation.Fingerprint.Profile != systemProfile.compiled.Fingerprint()) {
			return nil, errors.New("transformation fingerprints do not match the stored Drupal profile")
		}
	}

	engine := httpapi.NewCrosswalkEngine()
	if transformation != nil {
		engine, err = httpapi.NewCrosswalkEngineWithSpec(transformation)
		if err != nil {
			return nil, err
		}
	}
	if systemProfile != nil {
		if err := engine.ConfigureReconciliation(httpapi.ReconciliationConfig{
			Policy: systemProfile.policy, Provenance: systemProfile.provenance, TargetProfile: systemProfile.compiled,
		}); err != nil {
			return nil, fmt.Errorf("configuring Drupal-model-aware validation: %w", err)
		}
	}
	return engine, nil
}

func readWorkbenchCLIInput(command *cobra.Command, path string) (_ []byte, _ [][]string, returnErr error) {
	reader := command.InOrStdin()
	var file *os.File
	if strings.TrimSpace(path) != "" {
		var err error
		file, err = os.Open(path)
		if err != nil {
			return nil, nil, fmt.Errorf("opening Workbench CSV: %w", err)
		}
		defer func() {
			if err := file.Close(); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("closing Workbench CSV: %w", err))
			}
		}()
		reader = file
	}

	limited := &io.LimitedReader{R: reader, N: maxWorkbenchCLIInputBytes + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, nil, fmt.Errorf("reading Workbench CSV: %w", err)
	}
	if int64(len(data)) > maxWorkbenchCLIInputBytes {
		return nil, nil, fmt.Errorf("workbench CSV exceeds %d bytes", maxWorkbenchCLIInputBytes)
	}
	if !utf8.Valid(data) {
		return nil, nil, errors.New("workbench CSV must be valid UTF-8")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	readerCSV := csv.NewReader(bytes.NewReader(data))
	readerCSV.FieldsPerRecord = -1
	rows := make([][]string, 0)
	var cells int64
	for {
		row, err := readerCSV.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("parsing Workbench CSV: %w", err)
		}
		if len(rows) >= maxWorkbenchCLIRows {
			return nil, nil, fmt.Errorf("workbench CSV row count exceeds %d", maxWorkbenchCLIRows)
		}
		if int64(len(row)) > maxWorkbenchCLICells-cells {
			return nil, nil, fmt.Errorf("workbench CSV cell count exceeds %d", maxWorkbenchCLICells)
		}
		cells += int64(len(row))
		rows = append(rows, row)
	}
	return data, rows, nil
}

func writeWorkbenchFindings(writer io.Writer, findings httpapi.CheckResult) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(findings); err != nil {
		return fmt.Errorf("writing Workbench validation findings: %w", err)
	}
	return nil
}
