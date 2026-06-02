# Echopoint CLI

Terminal-first tooling for the Echopoint webhook testing platform. Manage webhooks, flows, collections, and analytics from a fast, interactive CLI built on Bubble Tea.

## Installation

### Quick Install (macOS/Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/nanostack-dev/echopoint-cli/main/install.sh | bash
```

### Homebrew (macOS)

```bash
brew install nanostack-dev/tap/echopoint
```

### Manual Download

Download the latest release from [GitHub Releases](https://github.com/nanostack-dev/echopoint-cli/releases).

### From Source

```bash
go install github.com/nanostack-dev/echopoint-cli/cmd/echopoint@latest
```

Or build locally:

```bash
git clone https://github.com/nanostack-dev/echopoint-cli.git
cd echopoint-cli
go build -o echopoint ./cmd/echopoint
```

## Features

- Browser-based OAuth authentication via Clerk
- Manage flows with granular node, edge, and assertion control
- Manage collections with OpenAPI import support
- Environment variable management for flows
- Interactive TUI mode
- JSON/YAML/Table output formats

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

## Authentication

Echopoint uses Clerk session JWTs. The CLI stores the token in `~/.echopoint/credentials.json`.

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

# Remove node
echopoint flows node remove <flow-id> <node-id>

# Update node
echopoint flows node update <flow-id> <node-id> --name "New Name"
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

### Interactive TUI

```bash
echopoint tui
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
