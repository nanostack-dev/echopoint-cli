---
name: echopoint
description: Test an HTTP API with the Echopoint CLI. Use when asked to set up Echopoint, create or fix a flow, run flows against an environment, or run them in CI or on a schedule.
---

# Echopoint

Echopoint tests HTTP APIs with **flows**: chained HTTP requests where each step asserts on the response and passes values to the next step. A flow runs on demand from the terminal, on a schedule as a **monitor**, or in CI after a deploy.

Every command prints its flags and an example with `--help`. Read it before you use a command this page does not show.

## 1. Set up the CLI

1. Check the install with `echopoint version`. When the command is missing, install it and load the new `PATH`:
   ```bash
   curl -fsSL https://raw.githubusercontent.com/nanostack-dev/echopoint-cli/main/install.sh | sh
   . ~/.echopoint/env
   ```
   On Windows PowerShell: `irm https://raw.githubusercontent.com/nanostack-dev/echopoint-cli/main/install.ps1 | iex`
2. Create a dedicated profile from the API URL and the frontend URL the user gave. A profile keeps its own credentials.
   ```bash
   echopoint profile add <profile> --api-url <api-url> --frontend-url <frontend-url>
   ```
3. Check the credentials: `echopoint --profile <profile> auth status`. On `No credentials found`, run `echopoint --profile <profile> auth login` and ask the user to finish the sign-in in the browser it opens. Continue when `auth status` shows `Stored:`.
4. Find the organization and the workspace:
   ```bash
   echopoint --profile <profile> --org <organization-id> org view    # the organization's name and id
   echopoint --profile <profile> --org <organization-id> workspace list   # the workspaces you can open
   ```
   Flows and monitors live in a workspace and the CLI never chooses one. When `workspace list` shows several, ask the user which one to use.
5. Select the profile, organization and workspace on every command, with flags or with environment variables:
   ```bash
   echopoint --profile <profile> --org <organization-id> --workspace <workspace-id> flow list
   # or, for the commands of one shell session:
   export ECHOPOINT_PROFILE=<profile>
   export ECHOPOINT_ORGANIZATION_ID=<organization-id>
   export ECHOPOINT_WORKSPACE_ID=<workspace-id>
   ```
   The commands below assume the three variables. When your shell does not keep variables between commands, pass the three flags each time.

CI has no browser: there, set `ECHOPOINT_API_KEY` to an organization API key and `ECHOPOINT_ORGANIZATION_ID` to its organization. `workspace list` reads a browser session, so note the workspace id during setup.

## 2. Look before you create

1. Read the project's API: an OpenAPI file, the route definitions, the existing tests. Choose one small path that reads data or cleans up what it creates.
2. Read what Echopoint already holds:
   ```bash
   echopoint flow list --all                  # the ID column is the <flow-id>
   echopoint flow view <flow-id> -o json      # a flow's nodes, edges and assertions
   echopoint org env environments list        # named environments such as dev
   echopoint org env view -e <environment>    # the variable names in one; values stay hidden
   echopoint monitor list
   ```
3. When a flow already covers the path, edit that flow (section 3, "Edit a flow").

## 3. Build a flow

This example runs as written against a public test API. Run it once to learn the commands, then build the flow for the user's API the same way. It needs `jq`.

```bash
set -euo pipefail
FLOW_ID=$(echopoint flow create --name "Posts API smoke" -o json | jq -r .id)
echopoint flow env set "$FLOW_ID" --var BASE_URL=https://jsonplaceholder.typicode.com

# Step 1: a request node with an id you choose
echopoint flow node add "$FLOW_ID" --id get-post --type request --name "Get post" \
  --method GET --url "{{BASE_URL}}/posts/1"
echopoint flow node assertion add "$FLOW_ID" get-post \
  --extractor status_code --operator equals --value "200"
echopoint flow node assertion add "$FLOW_ID" get-post \
  --extractor json_path --path '$.id' --operator equals --value "1"
echopoint flow node assertion add "$FLOW_ID" get-post \
  --extractor header --header-name Content-Type --operator contains --value "application/json"
echopoint flow node output add "$FLOW_ID" get-post \
  --name userId --extractor json_path --path '$.userId'

# Step 2: runs after step 1 and reads its output
echopoint flow node add "$FLOW_ID" --id get-author --after get-post --type request --name "Get author" \
  --method GET --url "{{BASE_URL}}/users/{{get-post.userId}}"
echopoint flow node assertion add "$FLOW_ID" get-author \
  --extractor status_code --operator equals --value "200"
echopoint flow node assertion add "$FLOW_ID" get-author \
  --extractor json_path --path '$.email' --operator not_empty

echopoint flow validate "$FLOW_ID"
echopoint flow run "$FLOW_ID" -o json
```

The parts:

- **Order.** `--after <node-id>` adds the edge from an earlier node. A node reads only the outputs of nodes before it.
- **Templates.** `{{NAME}}` reads a variable. `{{node-id.output}}` reads an output that an earlier node declared with `flow node output add`. Both work in the URL, the headers, the body and an assertion's `--value`.
- **Headers and body.** `--headers '{"Authorization":"Bearer {{API_TOKEN}}"}'` and `--body '{"name":"smoke"}'` take JSON.
- **Assertions.** Extractors: `status_code`, `json_path` with `--path`, `header` with `--header-name`, `body`. Operators: `equals`, `not_equals`, `contains`, `not_contains`, `greater_than`, `less_than`, `greater_than_or_equal`, `less_than_or_equal`, `empty`, `not_empty`, `starts_with`, `ends_with`, `regex`.
- **A token the flow obtains.** Add `--secret` to `flow node output add` for a login token: results then show it as `***`.

### URLs as environment variables, credentials as secrets

A named environment holds the values of one deployment. A flow variable wins over an environment variable, which wins over an organization base variable.

```bash
echopoint org env environments list
echopoint org env environments create dev          # only when the list does not show it
echopoint org env view -e dev                      # `set` replaces a variable of the same name: check first
echopoint org env set -e dev --var BASE_URL=<the-api-url-the-user-gave>
echopoint org env set -e dev --secret --var "API_TOKEN=$API_TOKEN"
echopoint flow run <flow-id> -e dev -o json
```

A secret is encrypted, a read never returns it, and results show `***`. Take its value from a variable the user set in the shell, as `$API_TOKEN` above, so the value appears in no file and no message.

### Edit a flow

```bash
echopoint flow node update <flow-id> <node-id> --url "{{BASE_URL}}/v2/posts/1"
echopoint flow node assertion remove <flow-id> <node-id> <index>   # the index in `flow view -o json`
echopoint flow node remove <flow-id> <node-id>
```

To keep a flow in the repository, write it as a `*.flow.yaml` file: `echopoint flow compile -f <file> --check` lints it, `echopoint flow create -f <file>` creates it and `echopoint flow update <flow-id> -f <file>` replaces it. The grammar is in <https://github.com/nanostack-dev/echopoint-cli/blob/main/docs/flows.md#compile-a-flow-file>.

## 4. Validate and run

1. `echopoint flow validate <flow-id>` checks the graph without running it: an edge to a missing node, a `{{node.output}}` with no path from that node, a cycle. It exits non-zero on a problem.
2. `echopoint flow run <flow-id> -e <environment> -o json` runs the flow from this machine and waits, so it reaches an API on `localhost` or a private network. It prints one JSON object on stdout and exits with:

   | Exit code | Meaning |
   | --- | --- |
   | 0 | all flows succeeded |
   | 1 | one or more flows failed (node assertions or runner failure) |
   | 2 | cancelled |
   | 3 | API / runner / contract error |
   | 4 | timeout |

3. Read a failure in the JSON. The object has `status`, `success`, `exit_code`, `error_message`, `flow_url` and `nodes`. A failed node has `"status": "failed"`, and each entry of its `assertions` gives `extractor`, `operator`, `expected`, `actual` and `passed`:
   ```bash
   echopoint flow run <flow-id> -e <environment> -o json \
     | jq '.nodes[] | select(.status == "failed") | {node_id, error_message, assertions}'
   ```
   `--verbose` prints the same comparison for every node on stderr. A run of several flows puts one such object per flow under `results`.
4. Decide which side is wrong. Fix the flow when it asserts the wrong thing. When the API returns the wrong thing, keep the assertion and report the failure to the user.
5. `flow_url` is the link to the flow in the web app. It is built from the profile's `--frontend-url`.

## 5. Run after a deploy or on a schedule

**GitHub Actions.** Store an organization API key that has the `flows:execute` scope as the repository secret `ECHOPOINT_API_KEY`, and the organization id as the repository variable `ECHOPOINT_ORGANIZATION_ID`. Then add a job after the deploy job:

```yaml
jobs:
  echopoint:
    runs-on: ubuntu-latest
    steps:
      - uses: nanostack-dev/echopoint-cli@v1
        with:
          api-key: ${{ secrets.ECHOPOINT_API_KEY }}
          organization-id: ${{ vars.ECHOPOINT_ORGANIZATION_ID }}
          workspace-id: 7c9e6679-7425-40de-944b-e07fc1f90ae7
          flow-id: 550e8400-e29b-41d4-a716-446655440000
          environment: dev
          api-url: <api-url>   # omit for https://api.echopoint.dev
```

Replace the workspace id and the flow id with the user's own. The job fails when the flow fails. Inputs for several flows, tags and parallel runs: <https://github.com/nanostack-dev/echopoint-cli/blob/main/docs/github-action.md>.

**Monitor.** A monitor runs flows on a schedule. With `runner_type: cloud` Echopoint runs them, so the API under test must be reachable from the internet. A cron monitor runs at most every 15 minutes.

```yaml
# monitor.yaml
name: Posts API smoke
flow_ids: [550e8400-e29b-41d4-a716-446655440000]
runner_type: cloud
environment_key: dev
cron_expression: "*/30 * * * *"
```

```bash
echopoint monitor create -f monitor.yaml
```

Health policies, shorter intervals and run history: <https://github.com/nanostack-dev/echopoint-cli/blob/main/docs/monitors.md>.

## 6. Rules

- Ask the user for a target URL, a credential or a workspace you do not have. A guessed URL sends test requests to someone else's system.
- Keep credentials in Echopoint secrets and CI secrets. Leave them out of commits, flow files, logs and your messages, and show an API key only as its variable name.
- Use the user's own test data. An address that a flow sends mail to is on a domain the user owns.
- Edit the existing flow that covers a path; create a flow only for a path that none covers.
- Finish with a report: the exit code, the result of each node, and the `flow_url`.

## 7. MCP

`echopoint mcp` runs a Model Context Protocol server over stdio, so an MCP client can call Echopoint operations as tools with the credentials of the active profile. Setup and client configuration: <https://github.com/nanostack-dev/echopoint-cli/blob/main/docs/mcp.md>.
