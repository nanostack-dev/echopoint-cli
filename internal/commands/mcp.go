package commands

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/auth"
	"echopoint-cli/internal/client"
	"echopoint-cli/internal/mcp"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newMcpCmd runs a Model Context Protocol server over stdio, exposing echopoint
// operations annotated with `x-ai-tool` as tools. The user points an
// MCP-compatible AI client (Claude Desktop, Cursor, ...) at this command; the
// client owns the agent loop, and every tool call is dispatched through the
// CLI's existing authenticated API client.
//
// When no valid credentials are available the command starts the browser
// sign-in flow before serving, so an expired session does not hard-fail the
// server on launch. All sign-in output goes to stderr: stdout is the MCP
// protocol channel.
func newMcpCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run a Model Context Protocol server over stdio",
		Long: "Expose echopoint operations as MCP tools for an MCP-compatible AI client " +
			"(Claude Desktop, Cursor, etc.). Communicates over stdin/stdout and " +
			"authenticates with your stored session, an organization API key, or " +
			"ECHOPOINT_API_KEY. With no valid credentials it opens the browser to sign in.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := resolveMcpClient(cmd, state)
			if err != nil {
				return err
			}
			srv, err := mcp.NewServer(cli, Version())
			if err != nil {
				return err
			}
			return srv.Run(cmd.Context(), &mcpsdk.StdioTransport{})
		},
	}
}

// resolveMcpClient returns an authenticated client for the MCP server, choosing
// in priority order: an explicit API key (flag/env), a stored API key when
// preferred or when no valid session exists, a non-expired stored session, and
// finally an interactive browser sign-in.
func resolveMcpClient(cmd *cobra.Command, state *AppState) (*client.Client, error) {
	cfg := state.Config

	if state.APIKey != "" {
		return client.NewWithAPIKey(cfg.API.BaseURL, state.APIKey, state.OrganizationID, cfg.API.Timeout)
	}

	creds := loadOrEmptyCredentials(state.Profile)
	sessionValid := creds.AccessToken != "" &&
		(creds.ExpiresAt == nil || creds.ExpiresAt.After(time.Now()))

	if creds.APIKey != "" && (creds.PreferAPIKey || !sessionValid) {
		orgID := firstNonEmpty(state.OrganizationID, creds.OrganizationID)
		return client.NewWithAPIKey(cfg.API.BaseURL, creds.APIKey, orgID, cfg.API.Timeout)
	}
	if sessionValid {
		orgID := firstNonEmpty(state.OrganizationID, creds.OrganizationID)
		return client.New(cfg.API.BaseURL, creds.AccessToken, orgID, cfg.API.Timeout)
	}

	fmt.Fprintln(os.Stderr, "No valid echopoint credentials found; starting browser sign-in...")
	signedIn, err := loginForMcp(cmd, state)
	if err != nil {
		return nil, fmt.Errorf("sign-in failed: %w", err)
	}
	orgID := firstNonEmpty(state.OrganizationID, signedIn.OrganizationID)
	return client.New(cfg.API.BaseURL, signedIn.AccessToken, orgID, cfg.API.Timeout)
}

// loginForMcp runs the browser sign-in, persists the resulting session
// (preserving any stored API key), and resolves the default organization. It
// mirrors `auth login` but writes every message to stderr, since stdout is
// reserved for the MCP protocol.
func loginForMcp(cmd *cobra.Command, state *AppState) (auth.Credentials, error) {
	frontendURL := state.Config.FrontendURL
	if frontendURL == "" {
		frontendURL = "https://dev.echopoint.dev"
	}
	if state.Config.API.BaseURL == "http://localhost:8080" {
		frontendURL = "http://localhost:3001"
	}

	creds, err := auth.BrowserLogin(cmd.Context(), frontendURL, state.Debug)
	if err != nil {
		return auth.Credentials{}, err
	}

	// Preserve a previously stored API key but make the fresh session preferred.
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

	if _, err := auth.SaveCredentials(state.Profile, creds); err != nil {
		return auth.Credentials{}, err
	}
	fmt.Fprintf(os.Stderr, "✓ Signed in (profile: %s)\n", state.Profile)
	return creds, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
