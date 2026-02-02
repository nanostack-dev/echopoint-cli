# Authentication

Echopoint uses Clerk session JWTs for API requests. The CLI stores the session token in `~/.echopoint/credentials.json` and attaches it as a Bearer token.

## Login

1. Open the Echopoint web app and sign in with Google, GitHub, or password.
2. Copy the session JWT from your browser.
3. Save it with the CLI:

```bash
echopoint auth login --token "<SESSION_JWT>"
```

## Browser OAuth login

```bash
echopoint auth login
```

Test against dev API:

```bash
ECHOPOINT_API_URL="https://apidev.echopoint.dev" \
echopoint auth login

ECHOPOINT_API_URL="https://apidev.echopoint.dev" \
echopoint flows list
```

Redirect URL setup:

- Add `http://127.0.0.1:8765/callback` to the Clerk OAuth app redirect URLs.
- Ensure the CLI uses the exact Client ID shown in Clerk (copy/paste it).
- Run `echopoint auth login --debug` to see the authorize URL and confirm the redirect URL matches.
- If you need a different port, pass `--redirect-url` or set `CLERK_REDIRECT_URL`.

For development environments, the repository includes `.test-credentials.json` (gitignored) with test login details. Use those credentials to sign in via the browser.

## Logout

```bash
echopoint auth logout
```

## Status

```bash
echopoint auth status
```
