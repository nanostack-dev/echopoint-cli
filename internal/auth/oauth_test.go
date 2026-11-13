package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testClientID = "test-client"
	testCode     = "code-SECRET-123"
	testAccess   = "oat_access_SECRET"
	testRefresh  = "refresh_SECRET"
)

// fakeIssuer is an authorization server that records what it receives.
type fakeIssuer struct {
	*httptest.Server
	mu        sync.Mutex
	forms     []url.Values
	revokes   []url.Values
	tokenCall int
	// respond decides the token endpoint answer; nil returns a success body.
	respond func(form url.Values, call int) (int, string)
	revoke  int
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	f := &fakeIssuer{revoke: http.StatusOK}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.tokenCall++
		call := f.tokenCall
		f.forms = append(f.forms, r.PostForm)
		respond := f.respond
		f.mu.Unlock()
		status, body := http.StatusOK, fmt.Sprintf(
			`{"access_token":"%s","refresh_token":"%s","token_type":"bearer","expires_in":86400}`,
			testAccess, testRefresh)
		if respond != nil {
			status, body = respond(r.PostForm, call)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	mux.HandleFunc("/oauth/token/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.revokes = append(f.revokes, r.PostForm)
		status := f.revoke
		f.mu.Unlock()
		w.WriteHeader(status)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeIssuer) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenCall
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// callbackOpener plays the browser: it reads the authorize URL and calls the
// loopback redirect with the query that mutate builds from the real state.
func callbackOpener(
	t *testing.T,
	got *url.Values,
	status *atomic.Int32,
	mutate func(q url.Values, authQuery url.Values),
) func(string) error {
	return func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		aq := u.Query()
		if got != nil {
			*got = aq
		}
		q := url.Values{"state": {aq.Get("state")}, "code": {testCode}}
		mutate(q, aq)
		go func() {
			resp, err := http.Get(aq.Get("redirect_uri") + "?" + q.Encode())
			if err != nil {
				t.Errorf("callback: %v", err)
				return
			}
			defer resp.Body.Close()
			if status != nil {
				status.Store(int32(resp.StatusCode))
			}
		}()
		return nil
	}
}

func loginOpts(f *fakeIssuer, port string, out io.Writer, open func(string) error) OAuthLoginOptions {
	return OAuthLoginOptions{
		Issuer: f.URL, ClientID: testClientID, Port: port, OpenBrowser: open,
		Out: out, Timeout: 5 * time.Second, Debug: true,
	}
}

func TestOAuthLoginHappyPath(t *testing.T) {
	f := newFakeIssuer(t)
	var out bytes.Buffer
	var authQuery url.Values
	now := time.Unix(1_800_000_000, 0)
	opts := loginOpts(f, freePort(t), &out, callbackOpener(t, &authQuery, nil, func(url.Values, url.Values) {}))
	opts.Now = func() time.Time { return now }

	creds, err := OAuthLogin(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != testAccess || creds.RefreshToken != testRefresh {
		t.Fatalf("unexpected tokens: %+v", creds)
	}
	if creds.ExpiresAt == nil || !creds.ExpiresAt.Equal(now.Add(86400*time.Second)) {
		t.Fatalf("ExpiresAt = %v", creds.ExpiresAt)
	}
	if creds.OAuthIssuer != f.URL || creds.OAuthClientID != testClientID {
		t.Fatalf("issuer/client not stored: %+v", creds)
	}

	for key, want := range map[string]string{
		"response_type": "code", "client_id": testClientID, "scope": "email profile offline_access",
		"code_challenge_method": "S256",
	} {
		if authQuery.Get(key) != want {
			t.Errorf("authorize %s = %q, want %q", key, authQuery.Get(key), want)
		}
	}
	state, err := base64.RawURLEncoding.DecodeString(authQuery.Get("state"))
	if err != nil || len(state) < 16 {
		t.Errorf("state must be >=16 random bytes, got %d (%v)", len(state), err)
	}

	form := f.forms[0]
	verifier := form.Get("code_verifier")
	raw, err := base64.RawURLEncoding.DecodeString(verifier)
	if err != nil || len(raw) != 32 {
		t.Fatalf("verifier must be 32 bytes base64url, got %d (%v)", len(raw), err)
	}
	sum := sha256.Sum256([]byte(verifier))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != authQuery.Get("code_challenge") {
		t.Errorf("challenge %q != base64url(SHA-256(verifier)) %q", authQuery.Get("code_challenge"), got)
	}
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != testCode ||
		form.Get("redirect_uri") != authQuery.Get("redirect_uri") || form.Get("client_id") != testClientID {
		t.Errorf("unexpected token request: %v", form)
	}

	// Nothing secret reaches the output, even with debug on.
	for _, secret := range []string{testCode, testAccess, testRefresh, verifier} {
		if strings.Contains(out.String(), secret) {
			t.Errorf("output leaks a secret value %q", secret)
		}
	}
	if !strings.Contains(out.String(), "/oauth/authorize?") {
		t.Error("authorize URL should be printed")
	}
}

func TestOAuthLoginRefusesStateMismatch(t *testing.T) {
	f := newFakeIssuer(t)
	var status atomic.Int32
	open := callbackOpener(t, nil, &status, func(q, _ url.Values) { q.Set("state", "forged") })
	opts := loginOpts(f, freePort(t), io.Discard, open)
	opts.Timeout = 500 * time.Millisecond

	_, err := OAuthLogin(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout (forged callback ignored), got %v", err)
	}
	if status.Load() != http.StatusBadRequest {
		t.Errorf("forged callback status = %d, want 400", status.Load())
	}
	if f.calls() != 0 {
		t.Error("token endpoint must not be called on a state mismatch")
	}
}

func TestOAuthLoginErrorCallback(t *testing.T) {
	f := newFakeIssuer(t)
	open := callbackOpener(t, nil, nil, func(q, _ url.Values) {
		q.Del("code")
		q.Set("error", "access_denied")
		q.Set("error_description", "The user said no")
		q.Set("iss", f.URL)
	})
	_, err := OAuthLogin(context.Background(), loginOpts(f, freePort(t), io.Discard, open))
	if err == nil || !strings.Contains(err.Error(), "access_denied") ||
		!strings.Contains(err.Error(), "The user said no") {
		t.Fatalf("want a clear error with code and description, got %v", err)
	}
	if f.calls() != 0 {
		t.Error("no token call after an error callback")
	}
}

func TestOAuthLoginRefusesWrongIssuer(t *testing.T) {
	f := newFakeIssuer(t)
	open := callbackOpener(t, nil, nil, func(q, _ url.Values) { q.Set("iss", "https://evil.example") })
	opts := loginOpts(f, freePort(t), io.Discard, open)
	opts.Timeout = 500 * time.Millisecond
	if _, err := OAuthLogin(context.Background(), opts); err == nil {
		t.Fatal("expected failure")
	}
	if f.calls() != 0 {
		t.Error("token endpoint must not be called for a wrong issuer")
	}
}

func TestOAuthLoginAcceptsOnlyFirstCallback(t *testing.T) {
	f := newFakeIssuer(t)
	f.respond = func(url.Values, int) (int, string) {
		time.Sleep(150 * time.Millisecond) // the second callback arrives mid-exchange
		return 200, fmt.Sprintf(`{"access_token":"%s","refresh_token":"%s","token_type":"bearer","expires_in":60}`,
			testAccess, testRefresh)
	}
	var statuses [2]atomic.Int32
	open := func(authURL string) error {
		u, _ := url.Parse(authURL)
		aq := u.Query()
		cb := aq.Get("redirect_uri") + "?" + url.Values{"state": {aq.Get("state")}, "code": {testCode}}.Encode()
		for i := range statuses {
			go func() {
				time.Sleep(time.Duration(i) * 50 * time.Millisecond)
				if resp, err := http.Get(cb); err == nil {
					statuses[i].Store(int32(resp.StatusCode))
					resp.Body.Close()
				}
			}()
		}
		return nil
	}
	if _, err := OAuthLogin(context.Background(), loginOpts(f, freePort(t), io.Discard, open)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if f.calls() != 1 {
		t.Fatalf("token calls = %d, want 1", f.calls())
	}
	if statuses[1].Load() == http.StatusOK {
		t.Error("the second callback must be refused")
	}
}

func TestOAuthLoginTokenEndpointError(t *testing.T) {
	f := newFakeIssuer(t)
	f.respond = func(url.Values, int) (int, string) {
		return 400, `{"error":"invalid_grant","error_description":"code already used"}`
	}
	var out bytes.Buffer
	open := callbackOpener(t, nil, nil, func(url.Values, url.Values) {})
	_, err := OAuthLogin(context.Background(), loginOpts(f, freePort(t), &out, open))
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("want invalid_grant error, got %v", err)
	}
	if strings.Contains(err.Error(), testCode) || strings.Contains(out.String(), testCode) {
		t.Error("code leaked")
	}
	if f.calls() != 1 {
		t.Errorf("a burned code must not be retried; calls = %d", f.calls())
	}
}

func TestValidIssuer(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://x.example": true, "http://127.0.0.1:1": true, "http://localhost:3": true,
		"http://x.example": false, "ftp://x": false, "": false,
	} {
		if ValidIssuer(raw) != want {
			t.Errorf("ValidIssuer(%q) != %v", raw, want)
		}
	}
}

// ---- session ----

func storeSession(t *testing.T, f *fakeIssuer, expiresIn time.Duration, extra func(*Credentials)) {
	t.Helper()
	exp := time.Now().Add(expiresIn)
	c := Credentials{
		AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: &exp,
		OAuthIssuer: f.URL, OAuthClientID: testClientID, OrganizationID: "org-1",
	}
	if extra != nil {
		extra(&c)
	}
	if _, err := SaveCredentials("p", c); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureSessionSavesRotatedTokens(t *testing.T) {
	isolateHome(t)
	f := newFakeIssuer(t)
	storeSession(t, f, 30*time.Second, nil) // inside the 60 s window

	creds, err := EnsureSession(context.Background(), "p", RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != testAccess || creds.RefreshToken != testRefresh {
		t.Fatalf("not rotated: %+v", creds)
	}
	form := f.forms[0]
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "old-refresh" ||
		form.Get("client_id") != testClientID {
		t.Errorf("bad refresh request: %v", form)
	}
	saved, path, _ := LoadCredentials("p")
	if saved.AccessToken != testAccess || saved.RefreshToken != testRefresh || saved.OrganizationID != "org-1" {
		t.Errorf("saved = %+v", saved)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 && !isWindows() {
		t.Errorf("mode = %v (%v)", info.Mode(), err)
	}
}

func TestEnsureSessionSkipsFreshToken(t *testing.T) {
	isolateHome(t)
	f := newFakeIssuer(t)
	storeSession(t, f, time.Hour, nil)
	creds, err := EnsureSession(context.Background(), "p", RefreshOptions{})
	if err != nil || creds.AccessToken != "old-access" || f.calls() != 0 {
		t.Fatalf("unexpected refresh: %v %v calls=%d", creds, err, f.calls())
	}
}

func TestEnsureSessionConcurrentRefreshMakesOneCall(t *testing.T) {
	isolateHome(t)
	f := newFakeIssuer(t)
	f.respond = func(url.Values, int) (int, string) {
		time.Sleep(100 * time.Millisecond) // let the others queue on the lock
		return 200, fmt.Sprintf(`{"access_token":"%s","refresh_token":"%s","token_type":"bearer","expires_in":86400}`,
			testAccess, testRefresh)
	}
	storeSession(t, f, 10*time.Second, nil)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			c, err := EnsureSession(context.Background(), "p", RefreshOptions{})
			if err == nil && c.AccessToken != testAccess {
				err = fmt.Errorf("stale token %q", c.AccessToken)
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if f.calls() != 1 {
		t.Fatalf("token calls = %d, want exactly 1", f.calls())
	}
}

func TestEnsureSessionInvalidGrantClearsSessionKeepsAPIKey(t *testing.T) {
	isolateHome(t)
	f := newFakeIssuer(t)
	f.respond = func(url.Values, int) (int, string) { return 400, `{"error":"invalid_grant"}` }
	storeSession(t, f, -time.Minute, func(c *Credentials) { c.APIKey = "key-1"; c.PreferAPIKey = true })

	_, err := EnsureSession(context.Background(), "p", RefreshOptions{})
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	saved, _, _ := LoadCredentials("p")
	if saved == nil || saved.APIKey != "key-1" || !saved.PreferAPIKey {
		t.Fatalf("API key must survive: %+v", saved)
	}
	if saved.AccessToken != "" || saved.RefreshToken != "" || saved.ExpiresAt != nil || saved.OAuthIssuer != "" {
		t.Errorf("session not cleared: %+v", saved)
	}
}

func TestEnsureSessionNetworkErrorKeepsCredentials(t *testing.T) {
	isolateHome(t)
	f := newFakeIssuer(t)
	storeSession(t, f, -time.Minute, nil)
	f.Close() // the server is gone

	_, err := EnsureSession(context.Background(), "p", RefreshOptions{})
	if err == nil || errors.Is(err, ErrSessionExpired) {
		t.Fatalf("want a network error, got %v", err)
	}
	saved, _, _ := LoadCredentials("p")
	if saved == nil || saved.RefreshToken != "old-refresh" || saved.AccessToken != "old-access" {
		t.Fatalf("credentials must be kept: %+v", saved)
	}
}

func TestLoadOldCredentialsFile(t *testing.T) {
	isolateHome(t)
	path, _ := CredentialsPath("p")
	_ = os.MkdirAll(strings.TrimSuffix(path, "/p.json"), 0o700)
	old := `{"access_token":"jwt","api_key":"k","prefer_api_key":true,"expires_at":"2030-01-01T00:00:00Z","organization_id":"o"}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadCredentials("p")
	if err != nil || c == nil || c.AccessToken != "jwt" || c.APIKey != "k" || c.IsOAuth() {
		t.Fatalf("old file: %+v %v", c, err)
	}
	// An old session never attempts a refresh.
	got, err := EnsureSession(context.Background(), "p", RefreshOptions{})
	if err != nil || got.AccessToken != "jwt" {
		t.Fatalf("%v %v", got, err)
	}
	data, _ := json.Marshal(Credentials{AccessToken: "a"})
	if strings.Contains(string(data), "refresh_token") {
		t.Error("new fields must be omitempty")
	}
}

func TestRevokeRefreshToken(t *testing.T) {
	f := newFakeIssuer(t)
	creds := Credentials{AccessToken: "a", RefreshToken: "r1", OAuthIssuer: f.URL, OAuthClientID: testClientID}
	if err := RevokeRefreshToken(context.Background(), creds, nil); err != nil {
		t.Fatal(err)
	}
	got := f.revokes[0]
	if got.Get("token") != "r1" || got.Get("client_id") != testClientID ||
		got.Get("token_type_hint") != "refresh_token" {
		t.Errorf("revoke form = %v", got)
	}
	f.revoke = 500
	if err := RevokeRefreshToken(context.Background(), creds, nil); err == nil {
		t.Error("want an error for HTTP 500")
	}
}

func TestSessionSourceRefreshesOnceUnderConcurrency(t *testing.T) {
	isolateHome(t)
	f := newFakeIssuer(t)
	f.respond = func(url.Values, int) (int, string) {
		time.Sleep(50 * time.Millisecond)
		return 200, fmt.Sprintf(`{"access_token":"%s","refresh_token":"%s","token_type":"bearer","expires_in":86400}`,
			testAccess, testRefresh)
	}
	storeSession(t, f, 5*time.Second, nil)
	initial, _, _ := LoadCredentials("p")
	src := NewSessionSource("p", *initial, RefreshOptions{})

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			tok, err := src.Token(context.Background())
			if err != nil || tok != testAccess {
				t.Errorf("token = %q, err = %v", tok, err)
			}
		})
	}
	wg.Wait()
	if f.calls() != 1 {
		t.Fatalf("token calls = %d, want 1", f.calls())
	}
}

func TestLockTakesOverStaleLock(t *testing.T) {
	isolateHome(t)
	path, _ := CredentialsPath("p")
	_ = os.MkdirAll(strings.TrimSuffix(path, "p.json"), 0o700)
	lock := strings.TrimSuffix(path, ".json") + ".lock"
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(lock, old, old)
	unlock, err := lockProfile(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

func TestLockWaiterTakesOverALockThatGoesStale(t *testing.T) {
	isolateHome(t)
	oldStale := lockStale
	lockStale = 200 * time.Millisecond
	t.Cleanup(func() { lockStale = oldStale })

	unlockDead, err := lockProfile(context.Background(), "p") // never released: a crashed holder
	if err != nil {
		t.Fatal(err)
	}
	_ = unlockDead
	start := time.Now()
	unlock, err := lockProfile(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if time.Since(start) < 150*time.Millisecond {
		t.Error("the waiter should have waited for the lock to go stale")
	}
}
