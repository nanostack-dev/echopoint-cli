package commands

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"echopoint-cli/internal/auth"
	"echopoint-cli/internal/client"
	"echopoint-cli/internal/config"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

type AppState struct {
	Config         config.Config
	ConfigPath     string
	Profile        string
	OutputFormat   output.Format
	Token          string
	APIKey         string
	OrganizationID string
	Client         *client.Client
	Debug          bool
}

func NewRootCmd() *cobra.Command {
	state := &AppState{}

	var (
		flagConfig         string
		flagProfile        string
		flagAPIURL         string
		flagOutput         string
		flagToken          string
		flagAPIKey         string
		flagOrganizationID string
		flagDebug          bool
	)

	cmd := &cobra.Command{
		Use:   "echopoint",
		Short: "Echopoint CLI",
		Long:  "Echopoint CLI for managing webhooks, flows, collections, and analytics.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			profile := resolveProfile(flagProfile)
			cfg, cfgPath, err := loadConfig(flagConfig, profile)
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

			// Resolve API key (flag > env var). API key takes precedence over Bearer token.
			apiKey := resolveAPIKey(flagAPIKey)
			organizationID := resolveOrganizationIDFlag(flagOrganizationID, cfg.Profile)

			// Skip token validation for auth/profile/version/update commands or when
			// an API key is present.
			var token string
			if apiKey == "" && requiresToken(cmd) {
				token, err = resolveToken(flagToken, cfg.Profile)
				if err != nil {
					return err
				}
			}

			state.Config = cfg
			state.ConfigPath = cfgPath
			state.Profile = cfg.Profile
			state.OutputFormat = output.ParseFormat(outputValue)
			state.Token = token
			state.APIKey = apiKey
			state.OrganizationID = organizationID
			state.Debug = flagDebug

			// Set debug environment variable if --debug flag is used
			if flagDebug {
				os.Setenv("ECHOPOINT_DEBUG", "DEBUG")
			}

			if apiKey != "" {
				cli, err := client.NewWithAPIKey(cfg.API.BaseURL, apiKey, organizationID, cfg.API.Timeout)
				if err != nil {
					return err
				}
				state.Client = cli
			} else {
				cli, err := client.New(cfg.API.BaseURL, token, organizationID, cfg.API.Timeout)
				if err != nil {
					return err
				}
				state.Client = cli
			}

			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&flagConfig, "config", "", "Path to config file")
	cmd.PersistentFlags().StringVar(&flagProfile, "profile", "",
		"Profile to use (overrides ECHOPOINT_PROFILE and the current profile)")
	cmd.PersistentFlags().StringVar(&flagAPIURL, "api-url", "", "Override API base URL")
	cmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "", "Output format: table, json, yaml")
	cmd.PersistentFlags().StringVar(&flagToken, "token", "", "Session token (overrides stored credentials)")
	cmd.PersistentFlags().StringVar(&flagAPIKey, "api-key", "",
		"Organization API key (overrides ECHOPOINT_API_KEY env; takes precedence over Bearer token)")
	cmd.PersistentFlags().StringVar(&flagOrganizationID, "organization-id", "",
		"Organization ID for API key auth (overrides ECHOPOINT_ORGANIZATION_ID env)")
	cmd.PersistentFlags().BoolVar(&flagDebug, "debug", false, "Enable debug logging")

	cmd.AddCommand(
		newAuthCmd(state),
		newFlowsCmd(state),
		newOrgCmd(state),
		newCollectionsCmd(state),
		newConfigCmd(state),
		newProfileCmd(state),
		newVersionCmd(),
		newUpdateCmd(),
	)

	return cmd
}

// requiresToken reports whether a command needs a resolved session token in
// PersistentPreRunE. Auth, profile, config, version, and update commands manage
// their own state and must run without valid credentials.
func requiresToken(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "auth", "profile", "config", "version", "update":
			return false
		}
	}
	return true
}

func resolveProfile(flagProfile string) string {
	if strings.TrimSpace(flagProfile) != "" {
		return strings.TrimSpace(flagProfile)
	}
	return strings.TrimSpace(os.Getenv("ECHOPOINT_PROFILE"))
}

func loadConfig(flagConfig, profile string) (config.Config, string, error) {
	var (
		store config.Store
		path  string
		err   error
	)

	switch {
	case flagConfig != "":
		store, path, err = config.LoadStoreFrom(flagConfig)
	case os.Getenv("ECHOPOINT_CONFIG") != "":
		store, path, err = config.LoadStoreFrom(os.Getenv("ECHOPOINT_CONFIG"))
	default:
		store, path, err = config.LoadStore()
	}
	if err != nil {
		return config.Config{}, "", err
	}

	cfg, err := store.Resolve(profile)
	if err != nil {
		return config.Config{}, "", err
	}
	return cfg, path, nil
}

func resolveToken(flagToken, profile string) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if envToken := os.Getenv("ECHOPOINT_TOKEN"); envToken != "" {
		return envToken, nil
	}

	creds, _, err := auth.LoadCredentials(profile)
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

func resolveAPIKey(flagValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return strings.TrimSpace(flagValue)
	}
	return strings.TrimSpace(os.Getenv("ECHOPOINT_API_KEY"))
}

func resolveOrganizationIDFlag(flagValue, profile string) string {
	if strings.TrimSpace(flagValue) != "" {
		return strings.TrimSpace(flagValue)
	}
	if env := strings.TrimSpace(os.Getenv("ECHOPOINT_ORGANIZATION_ID")); env != "" {
		return env
	}
	if creds, _, err := auth.LoadCredentials(profile); err == nil && creds != nil {
		return strings.TrimSpace(creds.OrganizationID)
	}
	return ""
}
