# Command conventions


Every command follows these rules; `internal/commands` tests enforce most of them.

- Nouns are singular with a permanent plural alias: `flow` (`flows`), `collection` (`collections`), `status-page` (`status-pages`). `spec`, `org`, `profile`, `config` and `auth` have none.
- One read verb: `view`, with `get` and `show` kept as aliases. `list` is for many.
- A file is always `-f/--file` (`addFileFlag` registers the extensions for completion), and the command's `Args` is `fileFlagArgs(n)`, so a file typed as an argument fails with `pass the file with -f`. Never add a positional file.
- A flow argument is `<flow-id>` and goes through `resolveFlowID` (`resolve_flow.go`), the one place that turns it into an id. It reads an id and makes no request; anything else fails with `"x" is not a flow id; list the flows with: echopoint flow list`. Do not call `uuid.Parse` on a flow argument. `flow run` keeps its own words for a bad id (`invalid flow id "x": ...`) and the argument as typed in its result, because the Action reads them. `--flow-id` on `node add` and `status-page binding-options` goes through the resolver too.
- Vocabulary: a spec has a unique slug and a free-text title; flows and collections have an id and a free-text name, and no slug. Say "slug" for specs only.
- YAML is printed only through `output.PrintYAML`, which uses the key names of the JSON form; never marshal API types with a YAML encoder. A flag beats an environment variable, which beats the config (`configure` in `root.go`).
- **Deleting a whole resource asks; editing part of one does not.** `flow delete`, `collection delete`, `flow folder delete`, `flow env delete`, `org env delete`, `org env environments delete`, `profile delete`, `config reset` and `status-page unpublish` confirm. `env unset`, removing a node, edge, assertion or output, and the `spec` edit `remove` commands do not: they edit part of a resource, and a mistake is one command to put back. Do not add confirmations to those, and do not skip one on a command that deletes a whole resource.
- A command that deletes a whole resource calls `confirmDestructive` before it acts and `quietOnError`: `-y/--yes` skips the question, a terminal (stdin and stdout) is asked `Delete flow <id>? [y/N]` on stderr and anything but `y`/`yes` exits 2, and without a terminal it refuses with `pass --yes to delete <thing> without a prompt`. `AppState.IsTerminal` makes the terminal check injectable.
- Every leaf command has an `Example:`; `TestEveryLeafCommandHasAnExample` fails otherwise.
- Root commands sit in the help groups of `rootCommandGroups` in `command_groups.go`; a new root command needs an entry.
- Product administration belongs to the browser administration panel. Do not add administration commands, administrator login modes or MCP tools for `/administrations` routes; the complete generated API client remains an internal contract mirror.
- Completion goes through `completeFromAPI` in `complete.go` (2s timeout, silent on error, `name\tdescription`, no file completion). A command whose `Use` starts with `<flow-id>` gets flow completion from `completeFlowPlaceholders`.
- `--org` is the visible organization flag and `--organization-id` a hidden alias (`addOrganizationFlag`). `flow run` has no `-o` of its own: it reads the global one, and an `-o` typed on it wins over `ECHOPOINT_OUTPUT_FORMAT`.
- Vocabulary in help text: Cloud (EchoPoint runs the flow), Self-hosted (a long-lived runner the customer operates), Ephemeral (a short-lived runner the caller operates; `flow run` makes the CLI one), environment (a named overlay of variables on an organization), profile (the CLI's API target, never an environment). `flow run` and `flow launch` are different commands; do not merge them.
- Tests that run the CLI as a process use `runCLI` (`testmain_test.go`), which builds `cmd/echopoint` once.
