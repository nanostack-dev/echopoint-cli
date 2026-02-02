package commands

import (
	"fmt"
	"os"
	"time"

	"echopoint-cli/internal/config"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

func newConfigCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
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
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			cfg, _, err := config.Load()
			if err != nil {
				return err
			}

			switch key {
			case "api.base_url":
				cfg.API.BaseURL = value
			case "api.timeout":
				timeout, err := time.ParseDuration(value)
				if err != nil {
					return fmt.Errorf("invalid timeout value")
				}
				cfg.API.Timeout = timeout
			case "defaults.output_format":
				cfg.Defaults.OutputFormat = value
			default:
				return fmt.Errorf("unknown config key: %s", key)
			}

			path, err := config.Save(cfg)
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
		Short: "Reset configuration to defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.Save(config.Default())
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Reset config at %s\n", path)
			return nil
		},
	}
}
