package commands

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"echopoint-cli/internal/auth"
	"echopoint-cli/internal/config"

	"github.com/spf13/cobra"
)

// listVerb is the cobra Use string for the profile list subcommand.
const listVerb = "list"

func newProfileCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage configuration profiles",
		Long: `Manage configuration profiles.

The CLI talks to ` + "`https://api.echopoint.dev`" + ` by default. Create a
profile to point the CLI at a different API base URL (for example a
self-hosted or alternate environment) and switch between them with
` + "`echopoint profile use`" + `. Each profile keeps its own stored credentials.`,
	}

	cmd.AddCommand(
		newProfileListCmd(state),
		newProfileCurrentCmd(state),
		newProfileUseCmd(state),
		newProfileAddCmd(state),
		newProfileDeleteCmd(state),
	)

	return cmd
}

func newProfileListCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   listVerb,
		Short: "List configured profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := config.LoadStore()
			if err != nil {
				return err
			}
			active := store.ActiveProfileName()

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "CURRENT\tPROFILE\tAPI BASE URL")

			// The implicit default profile is always available.
			def, _ := store.Resolve(config.DefaultProfile)
			fmt.Fprintf(
				w,
				"%s\t%s\t%s\n",
				marker(active == config.DefaultProfile),
				config.DefaultProfile,
				def.API.BaseURL,
			)

			for _, name := range store.ProfileNames() {
				resolved, err := store.Resolve(name)
				if err != nil {
					return err
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", marker(active == name), name, resolved.API.BaseURL)
			}
			return w.Flush()
		},
	}
}

func marker(current bool) string {
	if current {
		return "*"
	}
	return ""
}

func newProfileCurrentCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the active profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(os.Stdout, "Profile: %s\n", state.Config.Profile)
			fmt.Fprintf(os.Stdout, "API base URL: %s\n", state.Config.API.BaseURL)
			return nil
		},
	}
}

func newProfileUseCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Switch the active profile (use \"default\" to reset)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			store, _, err := config.LoadStore()
			if err != nil {
				return err
			}

			if name == config.DefaultProfile {
				store.CurrentProfile = ""
			} else {
				if _, ok := store.Profiles[name]; !ok {
					return fmt.Errorf(
						"unknown profile %q; create it with 'echopoint profile add %s --api-url <url>'",
						name,
						name,
					)
				}
				store.CurrentProfile = name
			}

			path, err := config.SaveStore(store)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "✓ Switched to profile %q (%s)\n", store.ActiveProfileName(), path)
			return nil
		},
	}
}

func newProfileAddCmd(state *AppState) *cobra.Command {
	var (
		apiURL      string
		frontendURL string
		timeout     time.Duration
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create or update a profile that overrides the API base URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if config.IsReservedProfileName(name) {
				return fmt.Errorf("%q is reserved; choose another profile name", name)
			}
			if apiURL == "" {
				return fmt.Errorf("--api-url is required")
			}

			store, _, err := config.LoadStore()
			if err != nil {
				return err
			}
			if store.Profiles == nil {
				store.Profiles = map[string]config.Profile{}
			}

			existing := store.Profiles[name]
			profile := config.Profile{
				APIBaseURL:  apiURL,
				FrontendURL: frontendURL,
				Timeout:     existing.Timeout,
			}
			if timeout > 0 {
				profile.Timeout = timeout
			}
			store.Profiles[name] = profile

			path, err := config.SaveStore(store)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "✓ Saved profile %q -> %s (%s)\n", name, apiURL, path)
			fmt.Fprintf(os.Stdout, "Switch to it with: echopoint profile use %s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&apiURL, "api-url", "", "API base URL for this profile (required)")
	cmd.Flags().StringVar(&frontendURL, "frontend-url", "",
		"Browser-login frontend URL for this profile (defaults to the production frontend)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Request timeout for this profile (e.g. 30s)")

	return cmd
}

func newProfileDeleteCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a profile and its stored credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if config.IsReservedProfileName(name) {
				return fmt.Errorf("cannot delete the %q profile", config.DefaultProfile)
			}

			store, _, err := config.LoadStore()
			if err != nil {
				return err
			}
			if _, ok := store.Profiles[name]; !ok {
				return fmt.Errorf("unknown profile %q", name)
			}

			delete(store.Profiles, name)
			if store.CurrentProfile == name {
				store.CurrentProfile = ""
			}

			path, err := config.SaveStore(store)
			if err != nil {
				return err
			}

			// Best-effort removal of the profile's stored credentials.
			if credPath, derr := auth.DeleteCredentials(name); derr == nil {
				_ = credPath
			}

			fmt.Fprintf(os.Stdout, "✓ Deleted profile %q (%s)\n", name, path)
			return nil
		},
	}
}
