package commands

import (
	"testing"
	"time"

	"echopoint-cli/internal/auth"
)

func TestPreferredStoredAPIKey(t *testing.T) {
	const key = "echopoint_org_apikey_test"

	cases := []struct {
		name        string
		creds       *auth.Credentials
		bearerToken string
		want        string
	}{
		{name: "nil creds", creds: nil, bearerToken: "tok", want: ""},
		{name: "no api key", creds: &auth.Credentials{AccessToken: "tok"}, bearerToken: "tok", want: ""},
		{
			name:        "both stored, session default",
			creds:       &auth.Credentials{AccessToken: "tok", APIKey: key},
			bearerToken: "tok",
			want:        "",
		},
		{
			name:        "both stored, api key preferred",
			creds:       &auth.Credentials{AccessToken: "tok", APIKey: key, PreferAPIKey: true},
			bearerToken: "tok",
			want:        key,
		},
		{
			name:        "api key only (no session)",
			creds:       &auth.Credentials{APIKey: key},
			bearerToken: "",
			want:        key,
		},
		{
			name:        "session expired/absent falls back to api key",
			creds:       &auth.Credentials{AccessToken: "tok", APIKey: key},
			bearerToken: "",
			want:        key,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := preferredStoredAPIKey(tc.creds, tc.bearerToken); got != tc.want {
				t.Errorf("preferredStoredAPIKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCredentialsRoundTripStoresAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "default"

	saved := auth.Credentials{
		AccessToken:  "session-token",
		APIKey:       "echopoint_org_apikey_roundtrip",
		PreferAPIKey: true,
	}
	if _, err := auth.SaveCredentials(profile, saved); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	loaded, _, err := auth.LoadCredentials(profile)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected credentials, got nil")
	}
	if loaded.APIKey != saved.APIKey || !loaded.PreferAPIKey || loaded.AccessToken != saved.AccessToken {
		t.Errorf("round-trip mismatch: got %+v", loaded)
	}
}

func TestLoadCredentialsReturnsAPIKeyOnlyCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const profile = "default"

	if _, err := auth.SaveCredentials(profile, auth.Credentials{APIKey: "key-only"}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	loaded, _, err := auth.LoadCredentials(profile)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if loaded == nil || loaded.APIKey != "key-only" {
		t.Fatalf("expected api-key-only credentials to load, got %+v", loaded)
	}
}

func TestResolveStoredCredentialsExpiredSessionUsesStoredAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ECHOPOINT_TOKEN", "")
	const profile = "default"
	expired := time.Now().Add(-time.Hour)

	if _, err := auth.SaveCredentials(profile, auth.Credentials{
		AccessToken: "expired-session", ExpiresAt: &expired, APIKey: "stored-key", PreferAPIKey: true,
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	token, apiKey, err := resolveStoredCredentials("", profile)
	if err != nil {
		t.Fatalf("resolveStoredCredentials: %v", err)
	}
	if token != "" || apiKey != "stored-key" {
		t.Errorf("got token %q, api key %q; want no token and the stored key", token, apiKey)
	}
}

func TestResolveStoredCredentialsExpiredSessionWithoutAPIKeyFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ECHOPOINT_TOKEN", "")
	const profile = "default"
	expired := time.Now().Add(-time.Hour)

	if _, err := auth.SaveCredentials(profile, auth.Credentials{
		AccessToken: "expired-session", ExpiresAt: &expired,
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	if _, _, err := resolveStoredCredentials("", profile); err == nil {
		t.Fatal("expected the expired-session error")
	}
}
