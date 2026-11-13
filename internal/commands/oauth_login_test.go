package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"echopoint-cli/internal/auth"
	"echopoint-cli/internal/config"
)

func isolateHomeOnly(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ECHOPOINT_TOKEN", "")
}

// stubLogins replaces both sign-in flows and reports which one ran.
func stubLogins(t *testing.T) (*atomic.Int32, *atomic.Int32) {
	t.Helper()
	legacy, oauth := new(atomic.Int32), new(atomic.Int32)
	origLegacy, origOAuth := legacyBrowserLogin, oauthBrowserLogin
	t.Cleanup(func() { legacyBrowserLogin, oauthBrowserLogin = origLegacy, origOAuth })
	exp := time.Now().Add(time.Hour)
	legacyBrowserLogin = func(context.Context, string, bool) (auth.Credentials, error) {
		legacy.Add(1)
		return auth.Credentials{AccessToken: "legacy", ExpiresAt: &exp}, nil
	}
	oauthBrowserLogin = func(_ context.Context, o auth.OAuthLoginOptions) (auth.Credentials, error) {
		oauth.Add(1)
		return auth.Credentials{
			AccessToken: "oauth", RefreshToken: "r", ExpiresAt: &exp,
			OAuthIssuer: o.Issuer, OAuthClientID: o.ClientID,
		}, nil
	}
	return legacy, oauth
}

func stateFor(t *testing.T, profile string, api string, store config.Store) *AppState {
	t.Helper()
	cfg, err := store.Resolve(profile)
	if err != nil {
		t.Fatal(err)
	}
	if api != "" {
		cfg.OverrideAPIBaseURL(api)
	}
	return &AppState{Config: cfg, Profile: cfg.Profile}
}

func TestSignInDefaultProfileUsesPKCE(t *testing.T) {
	isolateHomeOnly(t)
	legacy, oauth := stubLogins(t)
	state := stateFor(t, "", "", config.DefaultStore())
	state.Config.API.BaseURL = "http://127.0.0.1:1" // /me lookup fails fast; only a warning

	creds, _, err := signIn(context.Background(), state, false, false)
	if err != nil || creds.AccessToken != "oauth" || creds.OAuthIssuer != "https://clerk.echopoint.dev" ||
		legacy.Load() != 0 || oauth.Load() != 1 {
		t.Fatalf("legacy=%d oauth=%d creds=%+v err=%v", legacy.Load(), oauth.Load(), creds, err)
	}
}

func TestSignInUnknownAPIUsesLegacyFlow(t *testing.T) {
	isolateHomeOnly(t)
	legacy, oauth := stubLogins(t)
	state := stateFor(t, "", "https://api.example.com", config.DefaultStore())
	state.Config.API.BaseURL = "http://127.0.0.1:1" // /me lookup fails fast; only a warning

	creds, _, err := signIn(context.Background(), state, false, false)
	if err != nil || creds.AccessToken != "legacy" || legacy.Load() != 1 || oauth.Load() != 0 {
		t.Fatalf("legacy=%d oauth=%d creds=%+v err=%v", legacy.Load(), oauth.Load(), creds, err)
	}
}

func TestSignInOAuthProfileUsesPKCEAndKeepsAPIKey(t *testing.T) {
	isolateHomeOnly(t)
	legacy, oauth := stubLogins(t)
	if _, err := auth.SaveCredentials("dev", auth.Credentials{APIKey: "key-1"}); err != nil {
		t.Fatal(err)
	}
	store := config.DefaultStore()
	store.Profiles["dev"] = config.Profile{APIBaseURL: "http://127.0.0.1:1"}
	state := stateFor(t, "dev", "https://apidev.echopoint.dev", store)
	state.Config.API.BaseURL = "http://127.0.0.1:1"

	creds, _, err := signIn(context.Background(), state, false, false)
	if err != nil || oauth.Load() != 1 || legacy.Load() != 0 || creds.APIKey != "key-1" || !creds.IsOAuth() {
		t.Fatalf("legacy=%d oauth=%d creds=%+v err=%v", legacy.Load(), oauth.Load(), creds, err)
	}
	// --local forces the legacy frontend flow.
	if _, _, err := signIn(context.Background(), state, true, false); err != nil || legacy.Load() != 1 {
		t.Fatalf("--local should use the legacy flow: %v", err)
	}
}

// fakeIssuer answers token and revoke calls.
type fakeIssuer struct {
	*httptest.Server
	tokenCalls  atomic.Int32
	revokeCalls atomic.Int32
	revokeCode  atomic.Int32
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	f := &fakeIssuer{}
	f.revokeCode.Store(http.StatusOK)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		n := f.tokenCalls.Add(1)
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(
			w,
			`{"access_token":"access-%d","refresh_token":"refresh-%d","token_type":"bearer","expires_in":86400}`,
			n,
			n,
		)
	})
	mux.HandleFunc("/oauth/token/revoke", func(w http.ResponseWriter, r *http.Request) {
		f.revokeCalls.Add(1)
		w.WriteHeader(int(f.revokeCode.Load()))
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func saveOAuthSession(t *testing.T, f *fakeIssuer, profile string, expiresIn time.Duration) {
	t.Helper()
	exp := time.Now().Add(expiresIn)
	if _, err := auth.SaveCredentials(profile, auth.Credentials{
		AccessToken: "old", RefreshToken: "old-refresh", ExpiresAt: &exp,
		OAuthIssuer: f.URL, OAuthClientID: "cid", APIKey: "key-1",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTokenRenewsAnOAuthSession(t *testing.T) {
	isolateHomeOnly(t)
	f := newFakeIssuer(t)
	saveOAuthSession(t, f, "dev", 20*time.Second)

	token, err := resolveToken("", "dev")
	if err != nil || token != "access-1" {
		t.Fatalf("token=%q err=%v", token, err)
	}
	if token, _ := resolveToken("--flag", "dev"); token != "--flag" || f.tokenCalls.Load() != 1 {
		t.Errorf("--token must bypass the stored session (calls=%d)", f.tokenCalls.Load())
	}
}

func TestResolveTokenReportsExpiredSessionAfterInvalidGrant(t *testing.T) {
	isolateHomeOnly(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
	}))
	defer srv.Close()
	f := &fakeIssuer{Server: srv}
	saveOAuthSession(t, f, "dev", -time.Minute)

	_, err := resolveToken("", "dev")
	if !errors.Is(err, auth.ErrSessionExpired) || !strings.Contains(err.Error(), "run 'echopoint auth login' again") {
		t.Fatalf("err = %v", err)
	}
	// The API key is the fallback for the commands that run next.
	if _, key, err := resolveStoredCredentials("", "dev"); err != nil || key != "key-1" {
		t.Fatalf("key=%q err=%v", key, err)
	}
}

func TestLogoutRevokesAndSucceedsWhenRevokeFails(t *testing.T) {
	for _, revokeStatus := range []int{http.StatusOK, http.StatusInternalServerError} {
		isolateHomeOnly(t)
		f := newFakeIssuer(t)
		f.revokeCode.Store(int32(revokeStatus))
		saveOAuthSession(t, f, "dev", time.Hour)

		cmd := newAuthLogoutCmd(&AppState{Profile: "dev"})
		cmd.SetArgs(nil)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("logout must not fail (revoke %d): %v", revokeStatus, err)
		}
		if f.revokeCalls.Load() != 1 {
			t.Errorf("revoke calls = %d", f.revokeCalls.Load())
		}
		if c, _, _ := auth.LoadCredentials("dev"); c != nil {
			t.Errorf("credentials must be gone: %+v", c)
		}
	}
}

func TestRefreshingClientRenewsOnceUnderConcurrentRequests(t *testing.T) {
	isolateHomeOnly(t)
	f := newFakeIssuer(t)
	saveOAuthSession(t, f, "dev", 10*time.Second)

	var mu sync.Mutex
	seen := map[string]int{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get("Authorization")]++
		mu.Unlock()
		fmt.Fprint(w, `{}`)
	}))
	defer api.Close()

	creds, _, _ := auth.LoadCredentials("dev")
	state := &AppState{Profile: "dev"}
	state.Config.API.BaseURL = api.URL
	state.Config.API.Timeout = 5 * time.Second
	cli, err := newSessionClient(state, *creds, "")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if status, _, err := cli.Do(
				context.Background(),
				http.MethodGet,
				"/ping",
				nil,
				nil,
			); err != nil ||
				status != 200 {
				t.Errorf("status=%d err=%v", status, err)
			}
		})
	}
	wg.Wait()
	if f.tokenCalls.Load() != 1 {
		t.Fatalf("token calls = %d, want 1", f.tokenCalls.Load())
	}
	if len(seen) != 1 || seen["Bearer access-1"] != 16 {
		t.Fatalf("every request must carry the renewed token: %v", seen)
	}
}
