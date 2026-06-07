package commands

import (
	"fmt"
	"os"

	"echopoint-cli/internal/config"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

func newConfigCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   configCommandName,
		Short: "Manage CLI configuration",
	}

	cmd.AddCommand(
		newConfigShowCmd(state),
		newConfigSetCmd(state),
		newConfigResetCmd(state),
	)

	return cmd
}

func newConfigShowCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, state.Config)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, state.Config)
			default:
				fmt.Fprintf(os.Stdout, "Config path: %s\n", state.ConfigPath)
				fmt.Fprintf(os.Stdout, "Profile: %s\n", state.Config.Profile)
				fmt.Fprintf(os.Stdout, "API base URL: %s\n", state.Config.API.BaseURL)
				fmt.Fprintf(os.Stdout, "API timeout: %s\n", state.Config.API.Timeout)
				fmt.Fprintf(os.Stdout, "Output format: %s\n", state.Config.Defaults.OutputFormat)
				return nil
			}
		},
	}
}

func newConfigSetCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Update a configuration value",
		Long: `Update a configuration value.

Supported keys:
  defaults.output_format   table | json | yaml

Per-environment settings (API base URL, timeout) live on profiles — see
'echopoint profile --help'.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			store, _, err := config.LoadStore()
			if err != nil {
				return err
			}

			switch key {
			case "defaults.output_format":
				store.Defaults.OutputFormat = value
			default:
				return fmt.Errorf(
					"unknown or read-only config key: %s (set API base URL via 'echopoint profile add')",
					key,
				)
			}

			path, err := config.SaveStore(store)
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "Updated %s in %s\n", key, path)
			return nil
		},
	}

	return cmd
}

func newConfigResetCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset configuration to defaults (removes all profiles)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.SaveStore(config.DefaultStore())
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Reset config at %s\n", path)
			return nil
		},
	}
}
