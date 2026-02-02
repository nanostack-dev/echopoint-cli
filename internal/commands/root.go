package commands

import (
	"errors"
	"fmt"
	"os"
	"time"

	"echopoint-cli/internal/auth"
	"echopoint-cli/internal/client"
	"echopoint-cli/internal/config"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

type AppState struct {
	Config       config.Config
	ConfigPath   string
	OutputFormat output.Format
	Token        string
	Client       *client.Client
	Debug        bool
}

func NewRootCmd() *cobra.Command {
	state := &AppState{}

	var (
		flagConfig string
		flagAPIURL string
		flagOutput string
		flagToken  string
		flagDebug  bool
	)

	cmd := &cobra.Command{
		Use:   "echopoint",
		Short: "Echopoint CLI",
		Long:  "Echopoint CLI for managing webhooks, flows, collections, and analytics.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, cfgPath, err := loadConfig(flagConfig)
			if err != nil {
				return err
			}

			if flagAPIURL != "" {
				cfg.API.BaseURL = flagAPIURL
			}
			if envAPI := os.Getenv("ECHOPOINT_API_URL"); envAPI != "" {
				cfg.API.BaseURL = envAPI
			}

			outputValue := cfg.Defaults.OutputFormat
			if flagOutput != "" {
				outputValue = flagOutput
			}
			if envOutput := os.Getenv("ECHOPOINT_OUTPUT_FORMAT"); envOutput != "" {
				outputValue = envOutput
			}

			// Skip token validation for auth commands
			var token string
			if cmd.Parent() == nil || cmd.Parent().Name() != "auth" {
				token, err = resolveToken(flagToken)
				if err != nil {
					return err
				}
			}

			state.Config = cfg
			state.ConfigPath = cfgPath
			state.OutputFormat = output.ParseFormat(outputValue)
			state.Token = token
			state.Debug = flagDebug

			// Set debug environment variable if --debug flag is used
			if flagDebug {
				os.Setenv("ECHOPOINT_DEBUG", "DEBUG")
			}

			cli, err := client.New(cfg.API.BaseURL, token, cfg.API.Timeout)
			if err != nil {
				return err
			}
			state.Client = cli

			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&flagConfig, "config", "", "Path to config file")
	cmd.PersistentFlags().StringVar(&flagAPIURL, "api-url", "", "Override API base URL")
	cmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "", "Output format: table, json, yaml")
	cmd.PersistentFlags().StringVar(&flagToken, "token", "", "Session token (overrides stored credentials)")
	cmd.PersistentFlags().BoolVar(&flagDebug, "debug", false, "Enable debug logging")

	cmd.AddCommand(
		newAuthCmd(state),
		newFlowsCmd(state),
		newCollectionsCmd(state),
		newConfigCmd(state),
		newTUICmd(state),
	)

	return cmd
}

func loadConfig(flagConfig string) (config.Config, string, error) {
	if flagConfig != "" {
		return config.LoadFrom(flagConfig)
	}

	if envConfig := os.Getenv("ECHOPOINT_CONFIG"); envConfig != "" {
		return config.LoadFrom(envConfig)
	}

	return config.Load()
}

func resolveToken(flagToken string) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if envToken := os.Getenv("ECHOPOINT_TOKEN"); envToken != "" {
		return envToken, nil
	}

	creds, _, err := auth.LoadCredentials()
	if err != nil {
		return "", err
	}
	if creds != nil {
		if creds.ExpiresAt != nil && creds.ExpiresAt.Before(time.Now()) {
			return "", errors.New("stored credentials have expired; run 'echopoint auth login' again")
		}
		return creds.AccessToken, nil
	}
	return "", nil
}

func requireToken(state *AppState) error {
	if state.Token == "" {
		return fmt.Errorf("authentication required: run 'echopoint auth login' or set ECHOPOINT_TOKEN")
	}
	return nil
}
