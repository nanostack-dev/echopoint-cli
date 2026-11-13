package commands

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/auth"
	"echopoint-cli/internal/client"
	"echopoint-cli/internal/mcp"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newMcpCmd runs a Model Context Protocol server over stdio, exposing echopoint
// eligible operations annotated with `x-ai-tool` as tools, excluding product
// administration. The user points an
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
		Example:      `  echopoint mcp`,
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
// finally an interactive browser sign-in. An OAuth session is renewed before
// use and again whenever it nears expiry while the server runs.
func resolveMcpClient(cmd *cobra.Command, state *AppState) (*client.Client, error) {
	cfg := state.Config

	if state.APIKey != "" {
		return client.NewWithAPIKey(
			cfg.API.BaseURL,
			state.APIKey,
			state.OrganizationID,
			cfg.API.Timeout,
			client.WithWorkspaceHeader(state.WorkspaceID),
		)
	}

	creds := loadOrEmptyCredentials(state.Profile)
	if creds.IsOAuth() {
		renewed, err := auth.EnsureSession(contextOf(cmd), state.Profile, auth.RefreshOptions{})
		switch {
		case errors.Is(err, auth.ErrSessionExpired):
			creds = loadOrEmptyCredentials(state.Profile) // the dead session was cleared
		case err != nil:
			return nil, err
		case renewed != nil:
			creds = *renewed
		}
	}
	sessionValid := creds.AccessToken != "" &&
		(creds.ExpiresAt == nil || creds.ExpiresAt.After(time.Now()))

	if creds.APIKey != "" && (creds.PreferAPIKey || !sessionValid) {
		orgID := firstNonEmpty(state.OrganizationID, creds.OrganizationID)
		return client.NewWithAPIKey(
			cfg.API.BaseURL,
			creds.APIKey,
			orgID,
			cfg.API.Timeout,
			client.WithWorkspaceHeader(state.WorkspaceID),
		)
	}
	if sessionValid {
		orgID := firstNonEmpty(state.OrganizationID, creds.OrganizationID)
		if orgID == "" {
			orgID = recoverSessionOrganizationID(contextOf(cmd), cfg, creds.AccessToken)
		}
		return newSessionClient(state, creds, orgID)
	}

	fmt.Fprintln(os.Stderr, "No valid echopoint credentials found; starting browser sign-in...")
	signedIn, _, err := signIn(cmd.Context(), state, false, state.Debug)
	if err != nil {
		return nil, fmt.Errorf("sign-in failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "✓ Signed in (profile: %s)\n", state.Profile)
	return newSessionClient(state, signedIn, firstNonEmpty(state.OrganizationID, signedIn.OrganizationID))
}

// newSessionClient builds a Bearer client. An OAuth session gets a token source,
// so a server that outlives the access token keeps working.
func newSessionClient(state *AppState, creds auth.Credentials, orgID string) (*client.Client, error) {
	editors := []api.RequestEditorFn{}
	if creds.IsOAuth() {
		source := auth.NewSessionSource(state.Profile, creds, auth.RefreshOptions{})
		editors = append(editors, client.WithTokenSource(source.Token))
	}
	editors = append(editors, client.WithWorkspaceHeader(state.WorkspaceID))
	return client.New(
		state.Config.API.BaseURL,
		creds.AccessToken,
		orgID,
		state.Config.API.Timeout,
		editors...,
	)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
