package commands

import (
	"testing"

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
