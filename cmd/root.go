// Package cmd provides CLI commands for crosswalk.
package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func setupLogger() {
	logLevel := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	if logLevel == "" {
		logLevel = "INFO"
	}

	var level slog.Level
	switch logLevel {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	handler := slog.NewTextHandler(os.Stderr, opts)
	logger := slog.New(handler)

	slog.SetDefault(logger)
}

var rootCmd = &cobra.Command{
	Use:   "crosswalk",
	Short: "Convert scholarly metadata between formats",
	Long: `Crosswalk is a CLI tool for converting scholarly metadata between formats.

It uses a smart intermediate representation (IR) to enable conversions between
various formats like Drupal/Islandora JSON, CSV, BibTeX, DataCite XML, and more.

Examples:
  crosswalk convert drupal csv -i data.json -o output.csv
  crosswalk convert drupal csv < export.json
  cat export.json | crosswalk convert drupal csv
  crosswalk validate drupal -i data.json`,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	setupLogger()
	rootCmd.AddCommand(convertCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(profilesCmd)
}
