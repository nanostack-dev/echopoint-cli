package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/mcp"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newMcpCmd runs a Model Context Protocol server over stdio, exposing echopoint
// operations annotated with `x-ai-tool` as tools. The user points an
// MCP-compatible AI client (Claude Desktop, Cursor, ...) at this command; the
// client owns the agent loop, and every tool call is dispatched through the
// CLI's existing authenticated API client.
func newMcpCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run a Model Context Protocol server over stdio",
		Long: "Expose echopoint operations as MCP tools for an MCP-compatible AI client " +
			"(Claude Desktop, Cursor, etc.). Communicates over stdin/stdout and " +
			"authenticates with your stored session or ECHOPOINT_API_KEY.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if state.Token == "" && state.APIKey == "" {
				return fmt.Errorf(
					"authentication required: sign in with 'echopoint auth login', " +
						"store an organization API key with 'echopoint auth login --api-key <key>', " +
						"or set ECHOPOINT_API_KEY")
			}
			srv, err := mcp.NewServer(state.Client, Version())
			if err != nil {
				return err
			}
			return srv.Run(cmd.Context(), &mcpsdk.StdioTransport{})
		},
	}
}
