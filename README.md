# Echopoint CLI

Terminal-first tooling for the Echopoint webhook testing platform. Manage webhooks, flows, collections, and analytics from a fast CLI.

## Installation

### Quick Install (macOS/Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/nanostack-dev/echopoint-cli/main/install.sh | sh
```

Installs to `~/.local/bin` (honoring `$XDG_BIN_HOME`) — **no sudo required** —
verifies the release checksum, and adds the directory to your shell `PATH`.
Open a new terminal afterwards, or run `. ~/.echopoint/env`.

Options (pass after `-s --`):

```bash
# Custom install directory
curl -fsSL .../install.sh | sh -s -- --dir /usr/local/bin

# Pin a version, or don't touch shell PATH
curl -fsSL .../install.sh | sh -s -- --version v0.3.0 --no-modify-path
```

### Quick Install (Windows / PowerShell)

```powershell
irm https://raw.githubusercontent.com/nanostack-dev/echopoint-cli/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\Programs\echopoint`, verifies the checksum, and adds
it to your user `PATH`. No administrator rights required.

### Manual Download

Download the latest release for your platform from
[GitHub Releases](https://github.com/nanostack-dev/echopoint-cli/releases),
extract it, and move the `echopoint` binary onto your `PATH`.

### From Source

```bash
git clone https://github.com/nanostack-dev/echopoint-cli.git
cd echopoint-cli
go build -o echopoint ./cmd/echopoint
```

## Updating

The CLI can update itself in place from the latest GitHub release:

```bash
# Check whether a newer version is available
echopoint update --check

# Download and install the latest release (verifies the checksum)
echopoint update

# Show the installed version
echopoint version
```

Alternatively, re-run the quick-install script above — it always fetches the
latest release.

## Features

- Browser-based OAuth authentication via Clerk
- Manage flows with granular node, edge, and assertion control
- Manage collections with OpenAPI import support
- Environment variable management for flows
- Flow reuse via module nodes (run a flow inside another flow)
- JSON/YAML/Table output formats
- Configuration profiles for switching between environments
- Built-in self-update from GitHub releases
- MCP server (`echopoint mcp`) exposing echopoint operations as tools for AI clients

## Quick Start

```bash
# Authenticate (opens browser)
echopoint auth login

# List your flows
echopoint flows list

# Create a flow interactively
echopoint flows create-interactive --name "My API Test"

# Add nodes to a flow
echopoint flows node add <flow-id> --type request --name "Login" --method POST --url "https://api.example.com/login"

# Set environment variables
echopoint flows env set <flow-id> --var API_KEY=secret --var BASE_URL=https://api.example.com
```

## MCP Server

`echopoint mcp` runs a Model Context Protocol server over stdio so an
MCP-compatible AI client (Claude Desktop, Cursor, ...) can drive echopoint with
its own model. Tools are derived from the OpenAPI contract — operations annotated
`x-ai-tool: true` become tools — and every call runs as you, through your stored
credentials. See [docs/mcp.md](docs/mcp.md) for setup and the Claude Desktop
config snippet.

## Authentication

Echopoint uses Clerk session JWTs. Credentials are stored per profile in
`~/.echopoint/credentials/<profile>.json`.

### Browser Login (Recommended)

```bash
echopoint auth login
```

This opens a browser window to authenticate via Google, GitHub, or email/password.

### Token-based Login

```bash
echopoint auth login --token "<SESSION_JWT>"
```

### Environment Variable

```bash
ECHOPOINT_TOKEN="<SESSION_JWT>" echopoint flows list
```

## Profiles

By default the CLI talks to `https://api.echopoint.dev`. Profiles let you point
the CLI at a different API base URL (for example a self-hosted or alternate
environment) and switch between them. Each profile keeps its own stored
credentials, so you stay logged in to every environment independently.

```bash
# Create a profile that overrides the API base URL
echopoint profile add staging \
  --api-url https://staging.example.com \
  --frontend-url https://app.staging.example.com

# List profiles (the active one is marked with *)
echopoint profile list

# Switch the active profile
echopoint profile use staging

# Show the active profile
echopoint profile current

# Reset back to the default (api.echopoint.dev)
echopoint profile use default

# Delete a profile (also removes its stored credentials)
echopoint profile delete staging
```

Select a profile for a single command without switching the active one:

```bash
echopoint --profile staging flows list
# or
ECHOPOINT_PROFILE=staging echopoint flows list
```

The `default` profile always targets `https://api.echopoint.dev` and cannot be
modified or removed.

## Commands

### Flows

```bash
# List flows
echopoint flows list
echopoint flows list -o json

# Get flow details
echopoint flows get <flow-id>
echopoint flows get <flow-id> -o json

# Create flow from JSON
echopoint flows create --file flow.json

# Create flow interactively
echopoint flows create-interactive --name "My Flow"

# Update flow
echopoint flows update <flow-id> --file flow.json

# Delete flow
echopoint flows delete <flow-id>
```

### Flow Nodes

```bash
# Add request node
echopoint flows node add <flow-id> \
  --type request \
  --name "API Call" \
  --method POST \
  --url "https://api.example.com/endpoint" \
  --headers '{"Content-Type": "application/json"}' \
  --body '{"key": "value"}'

# Add delay node
echopoint flows node add <flow-id> \
  --type delay \
  --name "Wait" \
  --duration 5000

# Add module node (run another flow inside this one — flow reuse)
echopoint flows node add <flow-id> \
  --type module \
  --name "Login" \
  --flow-id <child-flow-id> \
  --input email={{userEmail}} \
  --input password={{userPassword}} \
  --output token=authToken

# Remove node
echopoint flows node remove <flow-id> <node-id>

# Update node
echopoint flows node update <flow-id> <node-id> --name "New Name"
```

### Module Nodes (Flow Reuse)

A **module node** runs another flow as a step inside the current flow, so a flow
can be composed from smaller reusable flows.

- `--flow-id` — ID of the child flow to run (required).
- `--input key=value` — bind a child input from a parent variable or upstream
  output. Repeatable. Values support `{{template}}` substitution.
- `--output parentName=childKey` — expose a child final-output key under
  `parentName` for downstream nodes in the parent flow. Repeatable.

```bash
echopoint flows node add <parent-flow-id> \
  --type module \
  --name "Authenticate" \
  --flow-id <auth-flow-id> \
  --input baseUrl={{apiUrl}} \
  --output token=sessionToken

# Downstream nodes then reference {{<module-node-id>.sessionToken}}
```

### Node Outputs

```bash
# Add JSONPath output
echopoint flows node output add <flow-id> <node-id> \
  --name "token" \
  --extractor jsonPath \
  --path "$.accessToken"

# Add body output
echopoint flows node output add <flow-id> <node-id> \
  --name "response" \
  --extractor body

# Remove output
echopoint flows node output remove <flow-id> <node-id> <output-name>
```

### Node Assertions

```bash
# Add status code assertion
echopoint flows node assertion add <flow-id> <node-id> \
  --extractor statusCode \
  --operator equals \
  --value "200"

# Add JSONPath assertion
echopoint flows node assertion add <flow-id> <node-id> \
  --extractor jsonPath \
  --path "$.status" \
  --operator equals \
  --value "success"

# Remove assertion
echopoint flows node assertion remove <flow-id> <node-id> <index>
```

### Flow Edges

```bash
# Connect nodes
echopoint flows edge add <flow-id> \
  --from <source-node-id> \
  --to <target-node-id> \
  --type success

# Remove edge
echopoint flows edge remove <flow-id> <edge-id>
```

### Flow Environment Variables

```bash
# Get environment variables
echopoint flows env get <flow-id>

# Set environment variables
echopoint flows env set <flow-id> --var KEY=value --var KEY2=value2

# Delete environment
echopoint flows env delete <flow-id>
```

### Collections

```bash
echopoint collections list
echopoint collections get <id>
echopoint collections create --name "My collection"
echopoint collections update <id> --name "New name"
echopoint collections delete <id>
echopoint collections import --file ./openapi.json --name "My API"
```

### Configuration

```bash
echopoint config show
echopoint config set api.base_url https://api.echopoint.dev
```

## Configuration

Default config file: `~/.echopoint/config.yaml`

```yaml
api:
  base_url: "https://apidev.echopoint.dev"
  timeout: 30s

defaults:
  output_format: "table"
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `ECHOPOINT_API_URL` | API base URL |
| `ECHOPOINT_OUTPUT_FORMAT` | Default output format (table/json/yaml) |
| `ECHOPOINT_TOKEN` | Session token |
| `ECHOPOINT_CONFIG` | Config file path |

### Using with Local Development

```bash
# Point to local backend
echopoint --api-url http://localhost:8080 flows list
```

## Development

### Generate API Client

```bash
go generate ./internal/api
```

### Run Tests

```bash
go test ./...
```

### Build

```bash
go build -o echopoint ./cmd/echopoint
```

### Lint

```bash
golangci-lint run
```

## Documentation

See the [docs/](./docs/) directory for detailed documentation:

- [Flow Management](./docs/flows.md) - Comprehensive guide to managing flows

## License

MIT
