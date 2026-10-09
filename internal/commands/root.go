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
	"github.com/spf13/pflag"
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
	AssumeYes      bool

	// IsTerminal tells whether stdin and stdout are terminals; nil asks the real ones.
	IsTerminal func() bool

	// Configure resolves the state from the flags typed so far; completion calls it.
	Configure func(cmd *cobra.Command) error
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
		flagYes            bool
	)

	configure := func(cmd *cobra.Command) error {
		profile := resolveProfile(flagProfile)
		cfg, cfgPath, err := loadConfig(flagConfig, profile)
		if err != nil {
			return err
		}

		// a flag beats an environment variable, which beats the config
		if envAPI := os.Getenv("ECHOPOINT_API_URL"); envAPI != "" {
			cfg.API.BaseURL = envAPI
		}
		if flagAPIURL != "" {
			cfg.API.BaseURL = flagAPIURL
		}

		outputValue := cfg.Defaults.OutputFormat
		if envOutput := os.Getenv("ECHOPOINT_OUTPUT_FORMAT"); envOutput != "" {
			outputValue = envOutput
		}
		if flagOutput != "" {
			outputValue = flagOutput
		}

		// Resolve API key (flag > env var). An explicit API key takes precedence
		// over a Bearer token.
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
			// Fall back to a stored API key per the profile's preference: a
			// stored session (Bearer) is the default when both are present,
			// unless the API key is marked preferred or no session is available.
			apiKey = resolveStoredAPIKey(cfg.Profile, token)
		}

		state.Config = cfg
		state.ConfigPath = cfgPath
		state.Profile = cfg.Profile
		state.OutputFormat = output.ParseFormat(outputValue)
		state.Token = token
		state.APIKey = apiKey
		state.OrganizationID = organizationID
		state.Debug = flagDebug
		state.AssumeYes = flagYes

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
	}
	state.Configure = configure

	cmd := &cobra.Command{
		Use:   "echopoint",
		Short: "Manage flows, specs, collections and status pages in EchoPoint",
		Long: `Manage what lives in EchoPoint: flows and their executions, OpenAPI specs,
collections, status pages, and the variables of an organization.

A flow runs on Cloud (EchoPoint runs it), on a Self-hosted runner (a long-lived
runner you operate), or on an Ephemeral runner (a short-lived runner the caller
operates: 'echopoint flow run' makes this CLI one).`,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if skipsConfiguration(cmd) {
				return nil
			}
			return configure(cmd)
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
	addOrganizationFlag(cmd.PersistentFlags(), &flagOrganizationID)
	cmd.PersistentFlags().BoolVar(&flagDebug, "debug", false, "Enable debug logging")
	cmd.PersistentFlags().BoolVarP(&flagYes, "yes", "y", false,
		"Skip the confirmation of a command that deletes a whole resource")
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles(true, false))

	cmd.AddCommand(
		newFlowCmd(state),
		newSpecCmd(state),
		newCollectionCmd(state),
		newStatusPageCmd(state),
		newOrgCmd(state),
		newAuthCmd(state),
		newProfileCmd(state),
		newConfigCmd(state),
		newMcpCmd(state),
		newVersionCmd(),
		newUpdateCmd(),
	)
	groupRootCommands(cmd)
	cmd.InitDefaultCompletionCmd()
	exampleCompletionCommands(cmd)
	rejectUnknownSubcommands(cmd)

	return cmd
}

// requiresToken reports whether a command needs a resolved session token in
// PersistentPreRunE. Auth, profile, config, version, update, completion, and mcp commands
// manage their own state and must run without valid credentials — mcp in
// particular resolves auth itself (and may trigger browser sign-in) at launch.
// A command annotated offline works on local files only.
//
// The auth/profile/config groups match anywhere in the parent chain so their
// subcommands (e.g. "auth login") also skip token resolution. The top-level
// self-management commands "version", "update", "completion" and "mcp" match ONLY when they are a
// direct child of root — otherwise a subcommand named "update" (notably
// "flow update") would wrongly skip token resolution and then fail its own
// requireToken check, making it impossible to authenticate.
func requiresToken(cmd *cobra.Command) bool {
	if cmd.Annotations[offlineAnnotation] == annotationEnabled ||
		cmd.Annotations[anonymousAnnotation] == annotationEnabled {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case authCommandName, profileCommandName, configCommandName:
			return false
		case versionCommandName, updateCommandName, mcpCommandName, completionCommandName:
			if isRootChild(c) {
				return false
			}
		}
	}
	return true
}

// isRootChild reports whether c is a direct child of the root command.
func isRootChild(c *cobra.Command) bool {
	parent := c.Parent()
	return parent != nil && parent.Parent() == nil
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
	if state.Token == "" && state.APIKey == "" {
		return fmt.Errorf(
			"authentication required: sign in with 'echopoint auth login', " +
				"store an organization API key with 'echopoint auth login --api-key <key>', " +
				"or set ECHOPOINT_API_KEY")
	}
	return nil
}

func resolveAPIKey(flagValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return strings.TrimSpace(flagValue)
	}
	return strings.TrimSpace(os.Getenv("ECHOPOINT_API_KEY"))
}

// resolveStoredAPIKey returns a stored organization API key when it should be
// used over a stored session: either the profile prefers the API key, or no
// Bearer token is available. Returns "" to keep using the Bearer session.
func resolveStoredAPIKey(profile, bearerToken string) string {
	creds, _, err := auth.LoadCredentials(profile)
	if err != nil {
		return ""
	}
	return preferredStoredAPIKey(creds, bearerToken)
}

// preferredStoredAPIKey decides, for a loaded credential set, whether the stored
// API key should be used instead of the Bearer session. A session is the
// default when both are present; the API key wins only when it is marked
// preferred or no session token is available.
func preferredStoredAPIKey(creds *auth.Credentials, bearerToken string) string {
	if creds == nil || creds.APIKey == "" {
		return ""
	}
	if creds.PreferAPIKey || bearerToken == "" {
		return creds.APIKey
	}
	return ""
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

// addOrganizationFlag registers --org, with --organization-id kept as a hidden alias.
func addOrganizationFlag(flags *pflag.FlagSet, target *string) {
	flags.StringVar(target, "org", "", "Organization ID (overrides ECHOPOINT_ORGANIZATION_ID)")
	flags.StringVar(target, "organization-id", "", "Alias of --org")
	_ = flags.MarkHidden("organization-id")
}

var completionExamples = map[string]string{
	"bash":       `  echopoint completion bash > /usr/local/etc/bash_completion.d/echopoint`,
	"zsh":        `  echopoint completion zsh > "${fpath[1]}/_echopoint"`,
	"fish":       `  echopoint completion fish > ~/.config/fish/completions/echopoint.fish`,
	"powershell": `  echopoint completion powershell | Out-String | Invoke-Expression`,
}

// exampleCompletionCommands gives the shell commands cobra generates the Example
// every other leaf has.
func exampleCompletionCommands(root *cobra.Command) {
	completion, _, err := root.Find([]string{"completion"})
	if err != nil {
		return
	}
	for _, shell := range completion.Commands() {
		shell.Example = completionExamples[shell.Name()]
	}
}
