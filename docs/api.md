# Raw API calls

`echopoint api <path>` calls any EchoPoint API endpoint with the profile's credentials, organization and workspace, like `gh api`. Use it for an endpoint that has no command yet. Then add a command for that endpoint: `api` is a fallback, not the way to cover a capability.

```bash
echopoint api /license/usage
echopoint api /me -o yaml
echopoint api /flows/search -F 'pagination[limit]=5' --workspace ws_123
echopoint api /flows/search -f search.json
echopoint api -X PATCH /organization -F name="Acme Payments"
echopoint api /license/usage --include
```

## Request

- The path is relative to the profile's API URL. A missing leading `/` is added. A full URL is refused, so the credentials never reach another host.
- Auth and scope are the same as for every other command: the stored session or API key, `--org`, and `--workspace`.
- `-X/--method` sets the method. The default is `GET`, or `POST` when fields or a body are given.
- `-F/--field key=value` adds a field that keeps its JSON type: `true`, `false`, `null` and numbers stay typed, and `@<file>` reads the value from a file. `--raw-field key=value` adds a string field. `name[key]=value` nests: `pagination[limit]=5` sends `{"pagination":{"limit":5}}`.
- Fields go to the query string for `GET`, `HEAD` and `DELETE`, and to a JSON body otherwise.
- `-f/--file <file>` sends a file as the body, and `-f -` reads stdin. You cannot give fields and `-f` together.
- `-H/--header key:value` adds a request header.

## Response

- The body goes to stdout: indented when it is JSON, as YAML with `-o yaml`, and unchanged otherwise.
- `-i/--include` prints the status line and the response headers first.
- A status outside 2xx prints the body too, writes `HTTP <status> (<error code>)` on stderr, and exits 1.

## Limits

- `/administrations` routes belong to the browser Admin Panel ([administration scope](technical/administration-scope.md)). The command refuses them before it sends anything.
- The command does not page. Pass the `next_cursor` of a search response back as `pagination[cursor]`.
