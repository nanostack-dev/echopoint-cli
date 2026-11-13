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
	var apiKey string
	var organizationID string
	var setDefault bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in via browser, or store an organization API key",
		Long: `Sign in to Echopoint.

By default this opens your browser and signs in with OAuth 2.0 and PKCE. The
session renews itself. The production, dev and local (localhost:8080) APIs have
their sign-in settings built in; a profile for another API sets them with
'profile add --oauth-issuer --oauth-client-id'.

Pass --api-key to store an organization API key instead. A session and an API
key can both be stored; the session is preferred when both are present. Use
--default with --api-key to prefer the API key instead.`,
		Example: `  echopoint auth login
  echopoint auth login --api-key <key>
  echopoint auth login --api-key <key> --org <organization-id> --default`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if apiKey != "" {
				return storeAPIKeyCredential(state, apiKey, organizationID, setDefault)
			}
			return browserLogin(cmd, state, debug)
		},
	}

	cmd.Flags().BoolVar(&debug, "debug", false, "Print debug information")
	cmd.Flags().StringVar(&apiKey, "api-key", "",
		"Store an organization API key instead of signing in via browser")
	addOrganizationFlag(cmd.Flags(), &organizationID)
	cmd.Flags().Lookup("org").Usage = "Organization id to store with the API key (optional; resolved from the key when omitted)"
	cmd.Flags().BoolVar(&setDefault, "default", false,
		"Prefer this API key over a stored session when both are present")

	return cmd
}

// storeAPIKeyCredential persists an organization API key into the profile's
// credentials, preserving any existing session token. The API key becomes the
// preferred method only when --default is set.
func storeAPIKeyCredential(state *AppState, apiKey, organizationID string, setDefault bool) error {
	// Resolve the organization from the key itself when not given explicitly, so
	// subsequent requests can send X-Organization-Id without the user supplying it.
	if organizationID == "" {
		resolved, resolveErr := resolveAPIKeyOrganizationID(
			state.Config.API.BaseURL, apiKey, state.Config.API.Timeout,
		)
		if resolveErr != nil {
			fmt.Fprintf(os.Stderr,
				"Warning: could not resolve organization from API key "+
					"(pass --org to set it): %v\n", resolveErr)
		} else {
			organizationID = resolved
		}
	}

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
	if creds.OrganizationID != "" {
		fmt.Fprintf(os.Stdout, "Organization: %s\n", creds.OrganizationID)
	}
	if creds.AccessToken != "" {
		preferred := "session (Bearer)"
		if creds.PreferAPIKey {
			preferred = "API key"
		}
		fmt.Fprintf(os.Stdout, "A session and an API key are both stored; preferred: %s\n", preferred)
	}
	return nil
}

// oauthBrowserLogin is the sign-in flow; tests replace it.
var oauthBrowserLogin = auth.OAuthLogin

// signIn runs the OAuth 2.0 PKCE sign-in for the active profile, saves the
// session and returns it with the credentials path. A previously stored API
// key is kept, and the fresh session becomes preferred. Warnings go to stderr:
// the MCP server owns stdout.
func signIn(ctx context.Context, state *AppState, debug bool) (auth.Credentials, string, error) {
	if !state.Config.OAuth.Enabled() {
		return auth.Credentials{}, "", fmt.Errorf(
			"profile %q: no browser sign-in is known for %s; set it with "+
				"'echopoint profile add %s --api-url %s --oauth-issuer <issuer> --oauth-client-id <client-id>', "+
				"or store an API key with 'echopoint auth login --api-key <key>'",
			state.Profile, state.Config.API.BaseURL, state.Profile, state.Config.API.BaseURL)
	}
	creds, err := oauthBrowserLogin(ctx, auth.OAuthLoginOptions{
		Issuer:   state.Config.OAuth.Issuer,
		ClientID: state.Config.OAuth.ClientID,
		Debug:    debug,
	})
	if err != nil {
		return auth.Credentials{}, "", err
	}

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
		return auth.Credentials{}, "", err
	}
	return creds, path, nil
}

func browserLogin(cmd *cobra.Command, state *AppState, debug bool) error {
	creds, path, err := signIn(cmd.Context(), state, debug)
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
		Use:     "help",
		Short:   "Show authentication instructions",
		Example: `  echopoint auth help`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprint(os.Stdout, `
┌─────────────────────────────────────────────────────────────────┐
│ Echopoint CLI Authentication                                   │
└─────────────────────────────────────────────────────────────────┘

The CLI uses the same authentication system as the frontend.

How it works:

1. Run: echopoint auth login

2. Your browser opens; sign in to Echopoint there

3. The CLI stores the session for the active profile

Sign-in uses OAuth 2.0 and PKCE and the session renews itself. The production,
dev and local (localhost:8080) APIs have it built in; a profile for another API
sets it with 'profile add --oauth-issuer --oauth-client-id'.

CI and scripts: store an organization API key instead with
echopoint auth login --api-key <key>

Commands:

  echopoint auth login                  Sign in via browser
  echopoint auth login --api-key <key>  Store an organization API key
  echopoint auth status                 Check authentication status
  echopoint auth logout                 Sign out (revokes an OAuth session) and clear credentials
`)
		},
	}
}

func newAuthStatusCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		Example: `  echopoint auth status
  echopoint --profile staging auth status`,
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
			printSessionStatus(creds)
			return nil
		},
	}
}

// printSessionStatus says which sign-in method the session uses, whether it
// renews itself, and when its token expires.
func printSessionStatus(creds *auth.Credentials) {
	if creds.AccessToken == "" {
		return
	}
	label := "Session expires"
	if creds.IsOAuth() {
		label = "Access token expires"
		fmt.Fprintln(os.Stdout, "Session method: OAuth 2.0 (PKCE); renews itself")
	} else {
		fmt.Fprintln(os.Stdout, "Session method: browser token; does not renew, sign in again when it expires")
	}
	if creds.ExpiresAt != nil {
		fmt.Fprintf(os.Stdout, "%s: %s\n", label, creds.ExpiresAt.Format(time.RFC3339))
	} else {
		fmt.Fprintf(os.Stdout, "%s: unknown\n", label)
	}
}

func resolveDefaultOrganizationID(baseURL string, token string, timeout time.Duration) (string, error) {
	organizations, err := sessionOrganizations(context.Background(), baseURL, token, timeout)
	if err != nil {
		return "", err
	}
	if len(organizations) == 0 {
		return "", nil
	}

	return organizations[0].ID, nil
}

type sessionOrganization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// sessionOrganizations lists the organizations the session's user belongs to, from /me.
func sessionOrganizations(
	ctx context.Context,
	baseURL, token string,
	timeout time.Duration,
) ([]sessionOrganization, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resolve default organization failed with status %d", resp.StatusCode)
	}

	var payload struct {
		User struct {
			Organizations []sessionOrganization `json:"organizations"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.User.Organizations, nil
}

// resolveAPIKeyOrganizationID asks /api-keys/self for the organization the API
// key is scoped to. The endpoint resolves the organization from the key, so no
// X-Organization-Id header is required.
func resolveAPIKeyOrganizationID(baseURL, apiKey string, timeout time.Duration) (string, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/api-keys/self", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Api-Key", apiKey)

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve organization failed with status %d", resp.StatusCode)
	}

	var payload struct {
		OrganizationID string `json:"organization_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	return payload.OrganizationID, nil
}

func newAuthLogoutCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		Example: `  echopoint auth logout
  echopoint --profile staging auth logout`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Revoke the refresh token first; a failure never blocks the logout.
			if creds := loadOrEmptyCredentials(state.Profile); creds.RefreshToken != "" {
				if revokeErr := auth.RevokeRefreshToken(cmd.Context(), creds, nil); revokeErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not revoke the session on the server: %v\n", revokeErr)
				}
			}
			path, err := auth.DeleteCredentials(state.Profile)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "✓ Removed credentials for profile %s at %s\n", state.Profile, path)
			return nil
		},
	}
}
