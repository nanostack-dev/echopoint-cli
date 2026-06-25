package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
)

const serverName = "echopoint"

// NewServer builds an MCP server exposing every x-ai-tool annotated operation,
// each dispatched through the authenticated API client. The returned server is
// run by the caller over a transport (stdio for the CLI).
func NewServer(cli *client.Client, version string) (*mcpsdk.Server, error) {
	tools, err := buildCatalog(api.OpenAPISpec)
	if err != nil {
		return nil, err
	}

	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: serverName, Version: version}, nil)
	for _, td := range tools {
		schema := &jsonschema.Schema{}
		if err := json.Unmarshal(td.InputSchema, schema); err != nil {
			return nil, fmt.Errorf("tool %s: invalid input schema: %w", td.Name, err)
		}
		srv.AddTool(
			&mcpsdk.Tool{Name: td.Name, Description: td.Description, InputSchema: schema},
			makeHandler(cli, td),
		)
	}
	return srv, nil
}

// makeHandler builds the CallTool handler for one tool: it un-merges the flat
// argument object back into path, query, and body by recorded location, calls
// the API, and returns the response (or error) as text content.
func makeHandler(cli *client.Client, td toolDef) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := map[string]any{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return errResult(fmt.Sprintf("invalid arguments: %v", err)), nil
			}
		}

		path := td.PathTemplate
		query := url.Values{}
		body := map[string]any{}
		for k, v := range args {
			switch td.Locations[k] {
			case locPath:
				path = strings.ReplaceAll(path, "{"+k+"}", url.PathEscape(fmt.Sprint(v)))
			case locQuery:
				query.Set(k, fmt.Sprint(v))
			default: // locBody or any unmapped argument
				body[k] = v
			}
		}

		var bodyBytes []byte
		if len(body) > 0 {
			b, err := json.Marshal(body)
			if err != nil {
				return errResult(fmt.Sprintf("encode body: %v", err)), nil
			}
			bodyBytes = b
		}

		status, respBody, err := cli.Do(ctx, td.Method, path, query, bodyBytes)
		if err != nil {
			return errResult(fmt.Sprintf("request failed: %v", err)), nil
		}

		text := formatJSON(respBody)
		if status >= 400 {
			return errResult(fmt.Sprintf("HTTP %d\n%s", status, text)), nil
		}
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
		}, nil
	}
}

func errResult(msg string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		IsError: true,
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: msg}},
	}
}

// formatJSON pretty-prints a JSON response body, falling back to the raw bytes.
func formatJSON(b []byte) string {
	if len(b) == 0 {
		return "(empty response)"
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, b, "", "  "); err == nil {
		return pretty.String()
	}
	return string(b)
}
