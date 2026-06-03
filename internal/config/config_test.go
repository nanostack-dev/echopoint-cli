package config

import (
	"testing"
	"time"
)

func TestResolveDefaultProfileEnforcesProd(t *testing.T) {
	store := DefaultStore()
	cfg, err := store.Resolve("")
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	if cfg.Profile != DefaultProfile {
		t.Errorf("profile = %q, want %q", cfg.Profile, DefaultProfile)
	}
	if cfg.API.BaseURL != defaultAPIBaseURL {
		t.Errorf("base url = %q, want %q", cfg.API.BaseURL, defaultAPIBaseURL)
	}
	if cfg.FrontendURL != defaultFrontendURL {
		t.Errorf("frontend url = %q, want %q", cfg.FrontendURL, defaultFrontendURL)
	}
}

func TestResolveCustomProfileOverridesBaseURL(t *testing.T) {
	store := DefaultStore()
	store.Profiles["staging"] = Profile{APIBaseURL: "https://staging.example.com"}
	store.CurrentProfile = "staging"

	cfg, err := store.Resolve("")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cfg.API.BaseURL != "https://staging.example.com" {
		t.Errorf("base url = %q, want override", cfg.API.BaseURL)
	}
	// Frontend falls back to the default when the profile omits it.
	if cfg.FrontendURL != defaultFrontendURL {
		t.Errorf("frontend url = %q, want default fallback", cfg.FrontendURL)
	}
	if cfg.API.Timeout != defaultTimeout {
		t.Errorf("timeout = %v, want default", cfg.API.Timeout)
	}
}

func TestResolveOverrideArgumentWins(t *testing.T) {
	store := DefaultStore()
	store.Profiles["staging"] = Profile{APIBaseURL: "https://staging.example.com"}
	store.CurrentProfile = "staging"

	cfg, err := store.Resolve(DefaultProfile)
	if err != nil {
		t.Fatalf("resolve override: %v", err)
	}
	if cfg.API.BaseURL != defaultAPIBaseURL {
		t.Errorf("override to default should enforce prod, got %q", cfg.API.BaseURL)
	}
}

func TestResolveUnknownProfileErrors(t *testing.T) {
	store := DefaultStore()
	if _, err := store.Resolve("nope"); err == nil {
		t.Fatal("expected error for unknown profile")
	}
}

func TestNoBuiltinProfiles(t *testing.T) {
	store := DefaultStore()
	if len(store.Profiles) != 0 {
		t.Errorf("default store has %d profiles, want 0 (no built-ins)", len(store.Profiles))
	}
}

func TestNormalizeDropsLegacyAndResetsCurrent(t *testing.T) {
	store := Store{
		CurrentProfile: "ghost", // references a non-existent profile
		Profiles:       map[string]Profile{},
	}
	store.normalize()
	if store.CurrentProfile != "" {
		t.Errorf("current profile = %q, want reset to default", store.CurrentProfile)
	}
	if store.LegacyAPI != nil {
		t.Error("legacy api should be cleared")
	}
}

func TestNormalizeDoesNotMigrateLegacyIntoProfile(t *testing.T) {
	// Pre-profiles config should NOT silently become a profile; default stays prod.
	store := Store{}
	store.LegacyAPI = &struct {
		BaseURL string        `yaml:"base_url"`
		Timeout time.Duration `yaml:"timeout"`
	}{BaseURL: "https://apidev.echopoint.dev", Timeout: time.Second}
	store.normalize()
	if len(store.Profiles) != 0 {
		t.Errorf("legacy config created %d profiles, want 0", len(store.Profiles))
	}
}

func TestIsReservedProfileName(t *testing.T) {
	for _, name := range []string{"", DefaultProfile} {
		if !IsReservedProfileName(name) {
			t.Errorf("%q should be reserved", name)
		}
	}
	if IsReservedProfileName("staging") {
		t.Error("staging should not be reserved")
	}
}
