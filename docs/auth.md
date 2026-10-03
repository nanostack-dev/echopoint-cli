# Authentication

Echopoint uses session JWTs for API requests. The CLI stores the session token in `~/.echopoint/credentials.json` and attaches it as a Bearer token.

## Login

### Browser Login (Recommended)

```bash
echopoint auth login
```

This opens a browser window to authenticate via Google, GitHub, or email/password.

After sign-in, the browser returns to the CLI's loopback callback page. Check the terminal for the final connection status, then close the tab. If the connection fails, run the login command again with the same CLI profile.

The callback document is rendered by `internal/auth/browser_page.go` from `internal/auth/pages/callback.html`. It uses Echopoint's logo, fonts, and semantic colors from `@nanostackorg/design-system` 0.2.2, with light/dark colors following the browser's system preference. This standalone Go page uses native HTML/CSS; it does not load the React design-system package or a frontend bundle.

Its CSS, SVGs, fonts, and font license notices are embedded in the initial response. Keep it self-contained: the CLI shuts down the local server after receiving the login token, so later asset requests may fail. Error messages pass through `html/template` escaping.

### Token-based Login

If you already have a session token:

```bash
echopoint auth login --token "<SESSION_JWT>"
```

### Environment Variable

```bash
ECHOPOINT_TOKEN="<SESSION_JWT>" echopoint flows list
```

## Development Authentication

Test against dev API:

```bash
ECHOPOINT_API_URL="https://apidev.echopoint.dev" \
echopoint auth login

ECHOPOINT_API_URL="https://apidev.echopoint.dev" \
echopoint flows list
```

For local development environments, the repository includes `.test-credentials.json` (gitignored) with test login details.

## Logout

```bash
echopoint auth logout
```

## Status

```bash
echopoint auth status
```
