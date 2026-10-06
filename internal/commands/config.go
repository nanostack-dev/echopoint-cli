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
		newConfigViewCmd(state),
		newConfigSetCmd(state),
		newConfigResetCmd(state),
	)

	return cmd
}

// configView is the configuration as -o json and -o yaml print it.
type configView struct {
	Profile     string             `json:"profile"`
	API         configAPIView      `json:"api"`
	FrontendURL string             `json:"frontend_url"`
	Defaults    configDefaultsView `json:"defaults"`
}

type configAPIView struct {
	BaseURL string `json:"base_url"`
	Timeout string `json:"timeout"`
}

type configDefaultsView struct {
	OutputFormat string `json:"output_format"`
}

func newConfigView(cfg config.Config) configView {
	return configView{
		Profile:     cfg.Profile,
		API:         configAPIView{BaseURL: cfg.API.BaseURL, Timeout: cfg.API.Timeout.String()},
		FrontendURL: cfg.FrontendURL,
		Defaults:    configDefaultsView{OutputFormat: cfg.Defaults.OutputFormat},
	}
}

func newConfigViewCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:     viewVerb,
		Aliases: []string{showVerb},
		Short:   "Show the current configuration",
		Example: `  echopoint config view
  echopoint config view -o yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, newConfigView(state.Config))
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, newConfigView(state.Config))
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
		Example: `  echopoint config set defaults.output_format json`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			store, err := state.loadStore()
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

			path, err := state.saveStore(store)
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
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset configuration to defaults (removes all profiles)",
		Example: `  echopoint config reset
  echopoint config reset --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirmDestructive(cmd, state, "reset", "the configuration and every profile"); err != nil {
				return err
			}
			path, err := state.saveStore(config.DefaultStore())
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Reset config at %s\n", path)
			return nil
		},
	}
	return quietOnError(cmd)
}
