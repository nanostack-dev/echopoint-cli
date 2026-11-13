# Authentication

Echopoint uses session tokens for API requests. The CLI stores the session in `~/.echopoint/credentials/<profile>.json` and attaches the token as a Bearer token. The file is readable only by you and is replaced as a whole when it changes.

## Login

### Browser Login (Recommended)

```bash
echopoint auth login
```

This opens a browser window to authenticate via Google, GitHub, or email/password.

After sign-in, the browser returns to the CLI's loopback callback page. Check the terminal for the final connection status, then close the tab. If the connection fails, run the login command again with the same CLI profile.

The callback document is rendered by `internal/auth/browser_page.go` from `internal/auth/pages/callback.html`. It uses Echopoint's logo, fonts, and semantic colors from `@nanostackorg/design-system` 0.2.2, with light/dark colors following the browser's system preference. This standalone Go page uses native HTML/CSS; it does not load the React design-system package or a frontend bundle.

The sign-in uses this page. Its CSS, SVGs, fonts, and font license notices are embedded in the initial response. Keep it self-contained: the CLI shuts down the local server after receiving the login result, so later asset requests may fail. Error messages pass through `html/template` escaping.

### OAuth sign-in with PKCE

`echopoint auth login` signs in with the OAuth 2.0 authorization-code flow and PKCE. The production API (`https://api.echopoint.dev`, the `default` profile) and the dev API (`https://apidev.echopoint.dev`) have this built in, so no extra setup is needed. The sign-in and consent pages are Clerk's; the last page is the CLI's own callback page above.

What happens when you run `echopoint auth login`:

1. The CLI makes a random secret (the verifier) and sends only its hash (the challenge) to the sign-in page. It also makes a random `state` value.
2. It opens your browser at the authorization server and listens on `http://127.0.0.1:8765/callback`. The port is fixed because the server only accepts registered redirect addresses.
3. After you sign in, the browser returns a one-time code. The CLI checks that `state` matches and that the answer comes from the expected issuer. It accepts only the first valid answer and waits at most 5 minutes.
4. The CLI sends the code and the verifier straight to the token endpoint. Only the real CLI can finish the exchange, because only it knows the verifier.
5. The CLI stores the access token, a refresh token and the issuer. The code and the tokens are never printed, not even with `--debug`.

The access token lasts about 24 hours and renews itself. When a command starts and the token ends within 60 seconds, the CLI uses the refresh token to get a new pair and saves both. A refresh token works only once, so a lock file next to the credentials makes parallel `echopoint` processes take turns; the second one finds the new token and does not refresh again. `echopoint mcp` renews the token while it runs, so it can stay up for days.

If the server refuses the refresh token, the CLI removes the stored session (an API key stays) and asks you to run `echopoint auth login` again. If the network fails, the CLI reports the error and keeps your credentials, so you can retry.

`echopoint auth logout` asks the server to revoke the refresh token and then deletes the file. If the server cannot be reached, the logout still works and prints a warning. `echopoint auth status` shows which method the session uses and whether it renews itself.

A profile uses OAuth when it has both settings. Set them with `profile add`, or in `config.yaml`:

```bash
echopoint profile add staging --api-url https://staging.example.com \
  --oauth-issuer https://issuer.example.com --oauth-client-id <public-client-id>
```

```yaml
profiles:
  staging:
    api_base_url: https://staging.example.com
    oauth_issuer: https://issuer.example.com
    oauth_client_id: <public-client-id>
```

Set both values or neither. They override the built-in table, which is keyed by the API base URL (today the production, dev and local `http://localhost:8080` APIs). The client is public: there is no client secret. The issuer must use `https`.

A profile for any other API without these settings cannot sign in through the browser: `auth login` stops and prints the `profile add` command to run, or use an API key. The older flow through the web app's `/cli/auth` page was retired after v3.16.0; sessions it stored keep working until their 7-day token expires.

### Use an existing session token

If you already have a session token:

```bash
echopoint flow list --profile dev --token "<SESSION_JWT>" -o json
```

`--token` supplies a session for the requested API command; `auth login --token`
does not store it. `--token` and `ECHOPOINT_TOKEN` are never renewed or revoked. Avoid putting real credentials in shell history; the
environment variable below is suitable for a token supplied by your environment.

Product administration belongs to the browser administration panel. CLI login
supports ordinary product workflows and has no administrator mode. See
[product administration scope](technical/administration-scope.md).

### Environment Variable

```bash
ECHOPOINT_TOKEN="<SESSION_JWT>" echopoint flow list
```

## Development Authentication

Test against dev API:

```bash
ECHOPOINT_API_URL="https://apidev.echopoint.dev" \
echopoint auth login

ECHOPOINT_API_URL="https://apidev.echopoint.dev" \
echopoint flow list
```

## Logout

```bash
echopoint auth logout
```

## Status

```bash
echopoint auth status
```
