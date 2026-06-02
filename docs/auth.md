# Authentication

Echopoint uses session JWTs for API requests. The CLI stores the session token in `~/.echopoint/credentials.json` and attaches it as a Bearer token.

## Login

### Browser Login (Recommended)

```bash
echopoint auth login
```

This opens a browser window to authenticate via Google, GitHub, or email/password.

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
