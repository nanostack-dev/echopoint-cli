package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const (
	formClientID = "client_id"

	// oauthScope is what the Echopoint public client asks for. offline_access
	// is what makes the server issue a refresh token.
	oauthScope = "email profile offline_access"

	oauthHTTPTimeout = 30 * time.Second
	maxOAuthBody     = 1 << 20
)

// OAuthLoginOptions configure OAuthLogin. Only Issuer and ClientID are required.
type OAuthLoginOptions struct {
	Issuer   string
	ClientID string
	Debug    bool

	// The fields below exist for tests; zero values select the real ones.
	Port        string                 // loopback port, default 8765 (registered with the issuer)
	OpenBrowser func(url string) error // default: the system browser
	HTTPClient  *http.Client           // default: a client with a 30 s timeout
	Timeout     time.Duration          // overall wait, default 5 minutes
	Out         io.Writer              // progress and debug text, default os.Stderr
	Now         func() time.Time       // default time.Now
}

func (o OAuthLoginOptions) withDefaults() OAuthLoginOptions {
	if o.Port == "" {
		o.Port = localServerPort
	}
	if o.OpenBrowser == nil {
		o.OpenBrowser = openBrowser
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: oauthHTTPTimeout}
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	if o.Out == nil {
		o.Out = stderrWriter()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// ValidIssuer accepts an https issuer URL. Plain http is allowed only for a
// loopback host, which is how tests and local authorization servers run.
func ValidIssuer(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		ip := net.ParseIP(host)
		return host == "localhost" || (ip != nil && ip.IsLoopback())
	}
	return false
}

func randomURLToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// pkceChallenge is base64url(SHA-256(verifier)) without padding (S256).
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// OAuthLogin signs in with the OAuth 2.0 authorization-code flow and PKCE. It
// never prints the code, the verifier or a token.
func OAuthLogin(ctx context.Context, opts OAuthLoginOptions) (Credentials, error) {
	opts = opts.withDefaults()
	if !ValidIssuer(opts.Issuer) || opts.ClientID == "" {
		return Credentials{}, errors.New("OAuth sign-in needs an https issuer and a client id")
	}
	issuer := strings.TrimRight(opts.Issuer, "/")

	verifier, err := randomURLToken(32)
	if err != nil {
		return Credentials{}, err
	}
	state, err := randomURLToken(32)
	if err != nil {
		return Credentials{}, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:"+opts.Port)
	if err != nil {
		return Credentials{}, fmt.Errorf("failed to start local server on port %s: %w", opts.Port, err)
	}
	defer listener.Close()

	redirectURI := fmt.Sprintf("http://127.0.0.1:%s%s", opts.Port, callbackPath)
	query := url.Values{
		"response_type":         {"code"},
		formClientID:            {opts.ClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {oauthScope},
		"state":                 {state},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	authURL := issuer + "/oauth/authorize?" + query.Encode()

	type result struct {
		creds Credentials
		err   error
	}
	resultCh := make(chan result, 1)
	var claimed atomic.Bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != callbackPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")

		params := r.URL.Query()
		// A request that does not carry our state is not part of this sign-in:
		// refuse it and keep waiting, so a stray request cannot end the login.
		if subtle.ConstantTimeCompare([]byte(params.Get("state")), []byte(state)) != 1 {
			fmt.Fprintln(opts.Out, "Ignored a sign-in response that did not match this request; still waiting...")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, errorPage("The sign-in response did not match this request. Run the login command again."))
			return
		}
		if iss := params.Get("iss"); iss != "" && strings.TrimRight(iss, "/") != issuer {
			fmt.Fprintln(opts.Out, "Ignored a sign-in response from an unexpected issuer; still waiting...")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, errorPage("The sign-in response came from an unexpected issuer."))
			return
		}
		if !claimed.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, errorPage("This sign-in was already handled."))
			return
		}

		if code := params.Get("error"); code != "" {
			msg := "Sign-in was refused: " + code
			if d := params.Get("error_description"); d != "" {
				msg += " (" + d + ")"
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, errorPage(msg))
			resultCh <- result{err: errors.New(msg)}
			return
		}
		authCode := params.Get("code")
		if authCode == "" {
			msg := "The sign-in response has no authorization code."
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, errorPage(msg))
			resultCh <- result{err: errors.New(msg)}
			return
		}

		// A failed exchange burns the code, so it is tried exactly once. It runs
		// on the login's context, not the request's: a browser that closes the
		// connection early must not cancel the exchange.
		creds, err := exchangeCode(ctx, opts, issuer, redirectURI, authCode, verifier)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, errorPage("Sign-in could not be completed. Check the terminal and try again."))
			resultCh <- result{err: err}
			return
		}
		fmt.Fprint(w, successPage())
		resultCh <- result{creds: creds}
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if opts.Debug {
		fmt.Fprintf(opts.Out, "Debug: Auth URL: %s\n", authURL)
		fmt.Fprintf(opts.Out, "Debug: Redirect URI: %s\n", redirectURI)
	}
	fmt.Fprintln(opts.Out, "Opening browser for authentication...")
	fmt.Fprintln(opts.Out, "")
	fmt.Fprintln(opts.Out, "If the browser doesn't open automatically, visit:")
	fmt.Fprintf(opts.Out, "  %s\n", authURL)
	fmt.Fprintln(opts.Out, "")
	if err := opts.OpenBrowser(authURL); err != nil && opts.Debug {
		fmt.Fprintf(opts.Out, "Debug: Failed to open browser: %v\n", err)
	}

	loginCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	select {
	case res := <-resultCh:
		return res.creds, res.err
	case <-loginCtx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			return Credentials{}, ctx.Err()
		}
		return Credentials{}, errors.New("authentication timed out")
	}
}

// tokenResponse is the token endpoint's success body.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// oauthError is an error body from the authorization server.
type oauthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
	Status      int    `json:"-"`
}

func (e *oauthError) Error() string {
	msg := fmt.Sprintf("authorization server refused the request (HTTP %d): %s", e.Status, e.Code)
	if e.Description != "" {
		msg += ": " + e.Description
	}
	return msg
}

// postToken sends a form to the token endpoint and decodes the answer. An
// error body becomes *oauthError; a transport failure is returned as is.
func postToken(ctx context.Context, hc *http.Client, endpoint string, form url.Values) (tokenResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, oauthHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		// The URL error can embed nothing secret: the form is in the body.
		return tokenResponse{}, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOAuthBody))
	if err != nil {
		return tokenResponse{}, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		oerr := &oauthError{Status: resp.StatusCode}
		_ = json.Unmarshal(body, oerr)
		if oerr.Code == "" {
			oerr.Code = "unknown_error"
		}
		return tokenResponse{}, oerr
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return tokenResponse{}, errors.New("token response is not valid JSON")
	}
	if tr.AccessToken == "" {
		return tokenResponse{}, errors.New("token response has no access token")
	}
	if tr.TokenType != "" && !strings.EqualFold(tr.TokenType, "bearer") {
		return tokenResponse{}, fmt.Errorf("unsupported token type %q", tr.TokenType)
	}
	return tr, nil
}

func exchangeCode(
	ctx context.Context, opts OAuthLoginOptions, issuer, redirectURI, code, verifier string,
) (Credentials, error) {
	tr, err := postToken(ctx, opts.HTTPClient, issuer+"/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		formClientID:    {opts.ClientID},
		"redirect_uri":  {redirectURI},
		"code":          {code},
		"code_verifier": {verifier},
	})
	if err != nil {
		return Credentials{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	return credentialsFromToken(tr, opts.Now(), issuer, opts.ClientID), nil
}

func credentialsFromToken(tr tokenResponse, now time.Time, issuer, clientID string) Credentials {
	ttl := fallbackTokenTTL
	if tr.ExpiresIn > 0 {
		ttl = time.Duration(tr.ExpiresIn) * time.Second
	}
	expiresAt := now.Add(ttl)
	return Credentials{
		AccessToken:   tr.AccessToken,
		RefreshToken:  tr.RefreshToken,
		ExpiresAt:     &expiresAt,
		OAuthIssuer:   issuer,
		OAuthClientID: clientID,
	}
}
