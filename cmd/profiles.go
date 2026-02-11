package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/lehigh-university-libraries/crosswalk/mapping"
)

var profilesCmd = &cobra.Command{
	Use:   "profiles",
	Short: "Manage mapping profiles",
	Long:  `List and inspect mapping profiles used for format conversions.`,
}

var profilesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		registry, err := mapping.NewProfileRegistry()
		if err != nil {
			return err
		}

		profiles := registry.List()
		if len(profiles) == 0 {
			fmt.Println("No profiles found")
			return nil
		}

		fmt.Println("Available profiles:")
		for _, name := range profiles {
			profile, _ := registry.Get(name)
			version := ""
			if profile.Version != "" {
				version = "@" + profile.Version
			}
			desc := ""
			if profile.Description != "" {
				desc = " - " + profile.Description
			}
			fmt.Printf("  %s%s%s\n", name, version, desc)
		}

		return nil
	},
}

var profilesShowCmd = &cobra.Command{
	Use:   "show [profile]",
	Short: "Show profile details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName := args[0]

		registry, err := mapping.NewProfileRegistry()
		if err != nil {
			return err
		}

		profile, ok := registry.Get(profileName)
		if !ok {
			return fmt.Errorf("unknown profile: %s", profileName)
		}

		// Print as YAML
		out, err := yaml.Marshal(profile)
		if err != nil {
			return err
		}

		fmt.Println(string(out))
		return nil
	},
}

var profilesFieldsCmd = &cobra.Command{
	Use:   "fields [profile]",
	Short: "List fields in a profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName := args[0]

		registry, err := mapping.NewProfileRegistry()
		if err != nil {
			return err
		}

		profile, ok := registry.Get(profileName)
		if !ok {
			return fmt.Errorf("unknown profile: %s", profileName)
		}

		fmt.Printf("Fields in %s profile:\n\n", profileName)
		fmt.Printf("%-30s -> %-25s %s\n", "Source Field", "IR Field", "Type")
		fmt.Printf("%-30s    %-25s %s\n", "------------", "--------", "----")

		for source, m := range profile.Fields {
			typeStr := ""
			if m.Type != "" {
				typeStr = m.Type
			}
			if m.Resolve != "" {
				if typeStr != "" {
					typeStr += ", "
				}
				typeStr += "resolve:" + m.Resolve
			}
			fmt.Printf("%-30s -> %-25s %s\n", source, m.IR, typeStr)
		}

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesListCmd)
	profilesCmd.AddCommand(profilesShowCmd)
	profilesCmd.AddCommand(profilesFieldsCmd)
}
