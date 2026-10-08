package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"
	"time"
)

func TestBrowserAuthURLKeepsAdministratorLoginExplicit(t *testing.T) {
	const callback = "http://127.0.0.1:8765/callback"
	for _, admin := range []bool{false, true} {
		authURL, err := browserAuthURL("https://dev.echopoint.dev/", callback, admin)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(authURL)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Host != "dev.echopoint.dev" || parsed.Path != "/cli/auth" ||
			parsed.Query().Get("callback") != callback {
			t.Errorf("invalid browser authentication target: %s", authURL)
		}
		if admin && parsed.Query().Get("admin") != "true" {
			t.Error("administrator login did not request the explicit browser exchange")
		}
		if !admin && parsed.Query().Has("admin") {
			t.Error("ordinary login broadened its requested token mode")
		}
	}
}

func TestBrowserAuthURLDoesNotInheritAdminModeFromFrontendURL(t *testing.T) {
	value, err := browserAuthURL("https://dev.echopoint.dev?admin=true", "http://127.0.0.1:8765/callback", false)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Has("admin") {
		t.Error("configured frontend query selected administrator mode without --admin")
	}
}

func makeJWT(t *testing.T, exp int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payloadBytes, err := json.Marshal(map[string]int64{"exp": exp})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return header + "." + payload + ".sig"
}

func TestTokenExpiry_UsesExpClaim(t *testing.T) {
	now := time.Unix(1_780_000_000, 0)
	exp := now.Add(7 * 24 * time.Hour).Unix()
	got := tokenExpiry(makeJWT(t, exp), now)
	if got.Unix() != exp {
		t.Fatalf("expected exp %d, got %d", exp, got.Unix())
	}
}

func TestTokenExpiry_FallbackOnNonJWT(t *testing.T) {
	now := time.Unix(1_780_000_000, 0)
	for _, tok := range []string{"not-a-jwt", "a.b", "a.!!!.c"} {
		got := tokenExpiry(tok, now)
		if got != now.Add(fallbackTokenTTL) {
			t.Fatalf("token %q: expected fallback %v, got %v", tok, now.Add(fallbackTokenTTL), got)
		}
	}
}

func TestTokenExpiry_FallbackOnMissingExp(t *testing.T) {
	now := time.Unix(1_780_000_000, 0)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u_1"}`))
	got := tokenExpiry(header+"."+payload+".sig", now)
	if got != now.Add(fallbackTokenTTL) {
		t.Fatalf("expected fallback for missing exp, got %v", got)
	}
}
