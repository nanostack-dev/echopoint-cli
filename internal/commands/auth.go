package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"echopoint-cli/internal/auth"

	"github.com/spf13/cobra"
)

func newAuthCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   authCommandName,
		Short: "Manage authentication",
	}

	cmd.AddCommand(
		newAuthLoginCmd(state),
		newAuthStatusCmd(state),
		newAuthLogoutCmd(state),
		newAuthHelpCmd(state),
	)

	return cmd
}

func newAuthLoginCmd(state *AppState) *cobra.Command {
	var debug bool
	var local bool
	var apiKey string
	var organizationID string
	var setDefault bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in via browser, or store an organization API key",
		Long: `Sign in to Echopoint.

By default this opens your browser and stores a session (Bearer) token.

Pass --api-key to store an organization API key instead. A session and an API
key can both be stored; the session is preferred when both are present. Use
--default with --api-key to prefer the API key instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if apiKey != "" {
				return storeAPIKeyCredential(state, apiKey, organizationID, setDefault)
			}
			return browserLogin(cmd, state, local, debug)
		},
	}

	cmd.Flags().BoolVar(&debug, "debug", false, "Print debug information")
	cmd.Flags().BoolVar(&local, "local", false, "Use localhost:3001 for authentication")
	cmd.Flags().StringVar(&apiKey, "api-key", "",
		"Store an organization API key instead of signing in via browser")
	cmd.Flags().StringVar(&organizationID, "organization-id", "",
		"Organization id to store with the API key (optional; resolved from the key when omitted)")
	cmd.Flags().BoolVar(&setDefault, "default", false,
		"Prefer this API key over a stored session when both are present")

	return cmd
}

// storeAPIKeyCredential persists an organization API key into the profile's
// credentials, preserving any existing session token. The API key becomes the
// preferred method only when --default is set.
func storeAPIKeyCredential(state *AppState, apiKey, organizationID string, setDefault bool) error {
	creds := loadOrEmptyCredentials(state.Profile)
	creds.APIKey = apiKey
	if organizationID != "" {
		creds.OrganizationID = organizationID
	}
	if setDefault {
		creds.PreferAPIKey = true
	}

	path, err := auth.SaveCredentials(state.Profile, creds)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\n✓ Stored API key (profile: %s)\n", state.Profile)
	fmt.Fprintf(os.Stdout, "Credentials saved to %s\n", path)
	if creds.AccessToken != "" {
		preferred := "session (Bearer)"
		if creds.PreferAPIKey {
			preferred = "API key"
		}
		fmt.Fprintf(os.Stdout, "A session and an API key are both stored; preferred: %s\n", preferred)
	}
	return nil
}

func browserLogin(cmd *cobra.Command, state *AppState, local, debug bool) error {
	// Frontend URL comes from the active profile; --local and the legacy
	// localhost API both fall back to the local frontend.
	frontendURL := state.Config.FrontendURL
	if frontendURL == "" {
		frontendURL = "https://dev.echopoint.dev"
	}
	if local || state.Config.API.BaseURL == "http://localhost:8080" {
		frontendURL = "http://localhost:3001"
	}

	creds, err := auth.BrowserLogin(cmd.Context(), frontendURL, debug)
	if err != nil {
		return err
	}

	// Preserve a previously stored API key, but make the fresh session the
	// preferred method.
	existing := loadOrEmptyCredentials(state.Profile)
	creds.APIKey = existing.APIKey
	creds.PreferAPIKey = false

	orgID, orgErr := resolveDefaultOrganizationID(
		state.Config.API.BaseURL,
		creds.AccessToken,
		state.Config.API.Timeout,
	)
	if orgErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to resolve default organization: %v\n", orgErr)
	} else {
		creds.OrganizationID = orgID
	}

	path, err := auth.SaveCredentials(state.Profile, creds)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\n✓ Successfully authenticated! (profile: %s)\n", state.Profile)
	fmt.Fprintf(os.Stdout, "Credentials saved to %s\n", path)
	if creds.OrganizationID != "" {
		fmt.Fprintf(os.Stdout, "Default organization: %s\n", creds.OrganizationID)
	}
	return nil
}

// loadOrEmptyCredentials returns the stored credentials for a profile, or an
// empty value when none exist or they cannot be read.
func loadOrEmptyCredentials(profile string) auth.Credentials {
	existing, _, err := auth.LoadCredentials(profile)
	if err != nil || existing == nil {
		return auth.Credentials{}
	}
	return *existing
}

func newAuthHelpCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "help",
		Short: "Show authentication instructions",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprint(os.Stdout, `
┌─────────────────────────────────────────────────────────────────┐
│ Echopoint CLI Authentication                                   │
└─────────────────────────────────────────────────────────────────┘

The CLI uses the same authentication system as the frontend.

How it works:

1. Run: echopoint auth login

2. Enter your email address (the one registered with Echopoint)

3. The CLI creates a secure session for you

4. You're authenticated! The session is valid for ~1 hour

Commands:

  echopoint auth login         Sign in with your email
  echopoint auth login -e X    Sign in with email X
  echopoint auth status        Check authentication status
  echopoint auth logout        Sign out and clear credentials

Note: You must have an existing Echopoint account. Sign up at
https://dev.echopoint.dev if you don't have one.
`)
		},
	}
}

func newAuthStatusCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, path, err := auth.LoadCredentials(state.Profile)
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "Profile: %s\n", state.Profile)
			if creds == nil {
				fmt.Fprintln(os.Stdout, "No credentials found.")
				fmt.Fprintf(os.Stdout, "Expected path: %s\n", path)
				return nil
			}

			fmt.Fprintf(os.Stdout, "Credentials: %s\n", path)

			methods := []string{}
			if creds.AccessToken != "" {
				methods = append(methods, "session (Bearer)")
			}
			if creds.APIKey != "" {
				methods = append(methods, "API key")
			}
			fmt.Fprintf(os.Stdout, "Stored: %s\n", strings.Join(methods, ", "))
			if creds.AccessToken != "" && creds.APIKey != "" {
				preferred := "session (Bearer)"
				if creds.PreferAPIKey {
					preferred = "API key"
				}
				fmt.Fprintf(os.Stdout, "Preferred: %s\n", preferred)
			}

			if creds.OrganizationID != "" {
				fmt.Fprintf(os.Stdout, "Organization: %s\n", creds.OrganizationID)
			}
			if creds.AccessToken != "" {
				if creds.ExpiresAt != nil {
					fmt.Fprintf(os.Stdout, "Session expires: %s\n", creds.ExpiresAt.Format(time.RFC3339))
				} else {
					fmt.Fprintln(os.Stdout, "Session expires: unknown")
				}
			}
			return nil
		},
	}
}

func resolveDefaultOrganizationID(baseURL string, token string, timeout time.Duration) (string, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/me", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve default organization failed with status %d", resp.StatusCode)
	}

	var payload struct {
		User struct {
			Organizations []struct {
				Id string `json:"id"`
			} `json:"organizations"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if len(payload.User.Organizations) == 0 {
		return "", nil
	}

	return payload.User.Organizations[0].Id, nil
}

func newAuthLogoutCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := auth.DeleteCredentials(state.Profile)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "✓ Removed credentials for profile %s at %s\n", state.Profile, path)
			return nil
		},
	}
}
