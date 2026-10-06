# Echopoint CLI

Terminal-first tooling for EchoPoint. Manage flows and their executions, OpenAPI specs, collections, status pages, and the variables of an organization from a fast CLI, and run flows from your terminal or CI.

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
- Run a flow from the CLI as an Ephemeral runner (`flow run`), or ask EchoPoint to run it on Cloud or a Self-hosted runner (`flow launch`)
- Organize flows into folders, and bulk-move them by id, search, or tag
- Keep [OpenAPI specs](docs/specs.md) in EchoPoint: create, push, pull, check, lint, diff, and edit them by slug
- Configure and publish [public status pages](docs/status-pages.md), including anonymous verification
- Manage collections with OpenAPI import support
- Variables for an organization, its environments, and each flow
- Flow reuse via module nodes (run a flow inside another flow)
- JSON/YAML/Table output formats
- Tab completion of flows, folders, executions, collections, environments and profiles
- Configuration profiles for switching between API targets
- Built-in self-update from GitHub releases
- MCP server (`echopoint mcp`) exposing echopoint operations as tools for AI clients

## Quick Start

```bash
# Authenticate (opens browser)
echopoint auth login

# List your flows
echopoint flow list

# Create an empty flow
echopoint flow create --name "My API Test"

# Add nodes to a flow
echopoint flow node add <flow-id> --type request --name "Login" --method POST --url "https://api.example.com/login"

# Set variables
echopoint flow env set <flow-id> --var API_KEY=secret --var BASE_URL=https://api.example.com

# Run it from here and wait for the result
echopoint flow run <flow-id>
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
ECHOPOINT_TOKEN="<SESSION_JWT>" echopoint flow list
```

### Organization API Key

Store an organization API key (no browser) and use it for subsequent commands:

```bash
echopoint auth login --api-key "<ORG_API_KEY>"
```

The organization is resolved from the key, so no organization id is required.
A session and an API key can both be stored on the same profile — the **session
(Bearer) is preferred** when both are present. Pass `--default` to prefer the
API key instead:

```bash
echopoint auth login --api-key "<ORG_API_KEY>" --default
```

Resolution order for each command: an explicit `--api-key` / `ECHOPOINT_API_KEY`
wins, then the stored credential per the profile's preference (session by
default, API key when preferred or when no session is stored). `echopoint auth
status` shows what's stored and which is preferred.

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

A **profile** is the CLI's API target; it is not an environment (`dev`, `prd`), which is a
named overlay of variables on an organization. Select a profile for a single command
without switching the active one:

```bash
echopoint --profile staging flow list
# or
ECHOPOINT_PROFILE=staging echopoint flow list
```

The `default` profile always targets `https://api.echopoint.dev` and cannot be
modified or removed.

## Commands

Nouns are singular and the plural stays as an alias: `flow` (`flows`), `collection`
(`collections`), `status-page` (`status-pages`). `echopoint --help` groups the commands:

| Group | Commands |
|-------|----------|
| Resources | `flow`, `spec`, `collection`, `status-page`, `org` |
| Account and setup | `auth`, `profile`, `config` |
| Tools | `mcp`, `completion`, `update`, `version` |

Every command has an `Example:` in its `--help`. An unknown subcommand fails with suggestions.

### Conventions

- **One read verb: `view`.** `flow view`, `collection view`, `status-page view`,
  `flow execution view`, `flow env view`, `org env view`, `spec view` and `config view` show one thing; `get` and
  `show` still work as aliases of `view`. `list` shows many.
- **A file is always `-f/--file`.** `flow create`, `flow update`, `collection import`,
  `org env import`, `flow env set`, `status-page save`, `status-page validate` and the
  `spec` commands take it that way; a file typed as an argument fails with
  `pass the file with -f`. `-f -` reads stdin where the command says so
  (`status-page save|validate`).
- **Specs are named by their slug; flows by their id.** A spec's `<slug>` is unique and
  its title is free text. A flow is `<flow-id>`, the id `flow list` shows; anything else
  fails before any request.
- **Deleting a whole resource asks; editing part of one does not.** `flow delete`,
  `collection delete`, `flow folder delete`, `flow env delete`, `org env delete`,
  `org env environments delete`, `profile delete`, `config reset` and
  `status-page unpublish` ask `Delete flow <id>? [y/N]` on stderr when stdin and
  stdout are terminals; anything but `y` or `yes` cancels with exit code 2. Without a
  terminal they refuse (`pass --yes to delete flow <id> without a prompt`) unless
  `-y/--yes` is given. `env unset`, removing a node, edge, assertion or output, and the
  `spec` edit `remove` commands never ask.
- **Output.** `-o table|json|yaml` is global. YAML uses the key names of the JSON.
- **Precedence.** A flag beats an environment variable, which beats the config:
  `--api-url` over `ECHOPOINT_API_URL`, `-o` over `ECHOPOINT_OUTPUT_FORMAT`, `--profile`
  over `ECHOPOINT_PROFILE`, `--org` over `ECHOPOINT_ORGANIZATION_ID`.

### Global flags

| Flag | |
|------|-|
| `-o, --output` | `table` (default), `json`, `yaml` |
| `--org` | Organization ID (`ECHOPOINT_ORGANIZATION_ID`); `--organization-id` is a hidden alias |
| `--profile` | API target, for one command |
| `--api-url`, `--api-key`, `--token`, `--config`, `--debug` | |
| `-y, --yes` | Skip the confirmation of a command that deletes a whole resource |

### Flows

```bash
# List flows: NAME, ID, UPDATED
echopoint flow list
echopoint flow list -o json
echopoint flow view <flow-id>
echopoint flow view <flow-id> -o json

# Create: an empty flow, or one from a JSON file (exactly one of --name and -f)
echopoint flow create --name "Checkout smoke"
echopoint flow create -f flow.json

# Update, from a file or by flags (exactly one of the two)
echopoint flow update <flow-id> -f flow.json
echopoint flow update <flow-id> --name "Checkout smoke test" --description "Runs after deploy"

# Delete
echopoint flow delete <flow-id>
echopoint flow delete <flow-id> --yes
```

### Running Flows

`flow run` and `flow launch` are different commands. A flow runs on **Cloud**
(EchoPoint runs it), on a **Self-hosted** runner (a long-lived runner you operate), or on
an **Ephemeral** runner (a short-lived runner the caller operates).

```bash
# Run it here: this CLI is the Ephemeral runner. It waits, shows live progress, and exits
# 0 (passed), 1 (failed), 2 (cancelled), 3 (error) or 4 (timeout).
echopoint flow run <flow-id>
echopoint flow run <flow-id> <flow-id> --parallel 2 -e dev -o json
echopoint flow run --tag smoke

# Ask EchoPoint to run it on Cloud or a Self-hosted runner: prints the execution id and returns.
echopoint flow launch <flow-id>
echopoint flow launch <flow-id> --runner self_hosted -e prd

# Look at the executions
echopoint flow execution list <flow-id>
echopoint flow execution view <flow-id> <execution-id>
```

`-e/--environment` overlays a named environment on both. `echopoint flows run ... -o json` is
what the [GitHub Action](docs/github-action.md) runs; it is the same command as `flow run`.

### Flow Folders and Tags

```bash
echopoint flow folder list
echopoint flow folder create "Anchor/Identity"
echopoint flow folder rename "Anchor/Identity" "Identity and access"
echopoint flow folder move "Identity" --to "Anchor"
echopoint flow folder delete "Anchor/Identity"
echopoint flow move <flow-id> <flow-id> --to "Anchor/Identity"
echopoint flow tag <flow-id> --add smoke
```

### Flow Nodes

```bash
# Add request node
echopoint flow node add <flow-id> \
  --type request \
  --name "API Call" \
  --method POST \
  --url "https://api.example.com/endpoint" \
  --headers '{"Content-Type": "application/json"}' \
  --body '{"key": "value"}'

# Add delay node
echopoint flow node add <flow-id> \
  --type delay \
  --name "Wait" \
  --duration 5000

# Add module node (run another flow inside this one — flow reuse)
echopoint flow node add <flow-id> \
  --type module \
  --name "Login" \
  --flow-id <child-flow-id> \
  --input email={{userEmail}} \
  --input password={{userPassword}} \
  --output token=authToken

# Remove node
echopoint flow node remove <flow-id> <node-id>

# Update node
echopoint flow node update <flow-id> <node-id> --name "New Name"
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
echopoint flow node add <parent-flow> \
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
echopoint flow node output add <flow-id> <node-id> \
  --name "token" \
  --extractor json_path \
  --path "$.accessToken"

# Add body output
echopoint flow node output add <flow-id> <node-id> \
  --name "response" \
  --extractor body

# Remove output
echopoint flow node output remove <flow-id> <node-id> <output-name>
```

### Node Assertions

```bash
# Add status code assertion
echopoint flow node assertion add <flow-id> <node-id> \
  --extractor status_code \
  --operator equals \
  --value "200"

# Add JSONPath assertion
echopoint flow node assertion add <flow-id> <node-id> \
  --extractor json_path \
  --path "$.status" \
  --operator equals \
  --value "success"

# Remove assertion
echopoint flow node assertion remove <flow-id> <node-id> <index>
```

### Flow Edges

```bash
# Connect nodes
echopoint flow edge add <flow-id> \
  --from <source-node-id> \
  --to <target-node-id> \
  --type success

# Remove edge
echopoint flow edge remove <flow-id> <edge-id>
```

### Flow Variables

```bash
# Get variables
echopoint flow env view <flow-id>

# Set variables, inline or from a JSON or dotenv file
echopoint flow env set <flow-id> --var KEY=value --var KEY2=value2
echopoint flow env set <flow-id> -f vars.env

# Delete all variables of a flow (asks first)
echopoint flow env delete <flow-id>
```

### Organization Variables and Environments

An **environment** is a named overlay of variables on an organization (`dev`, `prd`).

```bash
echopoint org env view -e prd
echopoint org env set -e prd --var BASE_URL=https://api.example.com
echopoint org env import -f vars.env -e prd --secret
echopoint org env environments list
echopoint org env environments create prd
echopoint org env environments delete prd
echopoint org env delete
```

### OpenAPI Specs

```bash
echopoint spec list
echopoint spec view pets-api
echopoint spec push pets-api -f openapi.yaml
echopoint spec pull pets-api -f openapi.yaml
```

See [docs/specs.md](docs/specs.md) for the rest.

### Collections

```bash
echopoint collection list
echopoint collection view <id>
echopoint collection create --name "My collection"
echopoint collection update <id> --name "New name"
echopoint collection delete <id>
echopoint collection import -f ./openapi.json --name "My API"
```

### Status Pages

```bash
echopoint status-page view
echopoint status-page validate -f page.json
echopoint status-page save -f page.json
echopoint status-page publish --expected-draft-version 1 --expected-intent-version 0
echopoint status-page unpublish --expected-intent-version 1
```

See [docs/status-pages.md](docs/status-pages.md).

### Configuration

```bash
echopoint config view
echopoint config set defaults.output_format json
echopoint config reset            # asks first; removes every profile
```

### Completion

Flows (as `<id>`, with their name and folder), folders, executions of the flow already typed, collections, environments (`-e`
and `org env environments delete`) and profiles (`profile use|delete`, `--profile`)
complete with Tab, next to spec slugs and versions:

```bash
echopoint completion zsh > "${fpath[1]}/_echopoint"   # or bash, fish, powershell
```

## Configuration

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
| `ECHOPOINT_API_URL` | API base URL (`--api-url` wins) |
| `ECHOPOINT_OUTPUT_FORMAT` | Default output format (table/json/yaml) (`-o` wins) |
| `ECHOPOINT_TOKEN` | Session token |
| `ECHOPOINT_API_KEY` | Organization API key |
| `ECHOPOINT_ORGANIZATION_ID` | Organization ID (same as `--org`) |
| `ECHOPOINT_PROFILE` | Profile to use |
| `ECHOPOINT_CONFIG` | Config file path |

### Using with Local Development

```bash
# Point to local backend
echopoint --api-url http://localhost:8080 flow list
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

- [Flow Management](./docs/flows.md) - Comprehensive guide to managing and running flows
- [Running flows in CI](./docs/github-action.md) - `flow run` and the GitHub Action
- [OpenAPI specs](./docs/specs.md) - Keep OpenAPI specs in EchoPoint: create, push, pull, check, lint, diff, and edit them by slug

## License

MIT
