package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultProfile is the implicit profile used when no profile is selected.
	// It is not stored in config.yaml and cannot be overridden — it always
	// targets the enforced production endpoint below.
	DefaultProfile = "default"

	// defaultAPIBaseURL is the enforced base URL for the default profile.
	defaultAPIBaseURL = "https://api.echopoint.dev"
	// defaultFrontendURL is the web app of the default profile, used for links
	// (flow URLs, spec URLs), and the fallback for custom profiles that omit it.
	defaultFrontendURL = "https://app.echopoint.dev"

	defaultOutputFormat = "table"
	defaultTimeout      = 30 * time.Second
)

// Profile is a user-created environment override. Only APIBaseURL is required;
// FrontendURL and Timeout fall back to the defaults when empty.
type Profile struct {
	APIBaseURL  string        `yaml:"api_base_url"`
	FrontendURL string        `yaml:"frontend_url,omitempty"`
	Timeout     time.Duration `yaml:"timeout,omitempty"`

	// OAuthIssuer and OAuthClientID turn on OAuth 2.0 PKCE sign-in for the
	// profile. Set both or neither; they override the built-in table.
	OAuthIssuer   string `yaml:"oauth_issuer,omitempty"`
	OAuthClientID string `yaml:"oauth_client_id,omitempty"`
}

// OAuthSettings locate the authorization server and public client of a
// profile. The zero value means the profile has no browser sign-in.
type OAuthSettings struct {
	Issuer   string
	ClientID string
}

// Enabled reports whether the profile signs in with OAuth.
func (o OAuthSettings) Enabled() bool { return o.Issuer != "" && o.ClientID != "" }

// builtinOAuth maps a normalized API base URL to its OAuth settings: the Clerk
// instance and the public client id of its "Echopoint CLI" OAuth application.
// A local API (`make dev`) uses the dev Clerk instance.
var builtinOAuth = map[string]OAuthSettings{
	"https://api.echopoint.dev": {
		Issuer:   "https://clerk.echopoint.dev",
		ClientID: "K4rWr6Ns5NCFHqat",
	},
	"https://apidev.echopoint.dev": devOAuth,
	"http://localhost:8080":        devOAuth,
	"http://127.0.0.1:8080":        devOAuth,
}

var devOAuth = OAuthSettings{
	Issuer:   "https://blessed-cobra-90.clerk.accounts.dev",
	ClientID: "l6FPYrmcg3X8lCVJ",
}

// NormalizeAPIBaseURL lowercases scheme and host and drops trailing slashes, so
// the built-in table matches however the URL was typed.
func NormalizeAPIBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(strings.ToLower(raw), "/")
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimRight(u.Path, "/")
}

// BuiltinOAuth returns the built-in OAuth settings for an API base URL.
func BuiltinOAuth(apiBaseURL string) OAuthSettings {
	return builtinOAuth[NormalizeAPIBaseURL(apiBaseURL)]
}

// Defaults holds settings shared across every profile.
type Defaults struct {
	OutputFormat string `yaml:"output_format"`
}

// Store is the on-disk shape of config.yaml. There are no built-in profiles;
// Profiles is empty until the user creates one. The legacy* fields capture the
// pre-profiles layout so old configs load without error.
type Store struct {
	CurrentProfile string             `yaml:"current_profile,omitempty"`
	Profiles       map[string]Profile `yaml:"profiles,omitempty"`
	Defaults       Defaults           `yaml:"defaults"`

	// Legacy single-environment fields (pre-profiles). Read on load so old
	// files don't error; never written back.
	LegacyAPI *struct {
		BaseURL string        `yaml:"base_url"`
		Timeout time.Duration `yaml:"timeout"`
	} `yaml:"api,omitempty"`
}

// Config is the resolved, active-profile view consumed by commands.
type Config struct {
	Profile string
	API     struct {
		BaseURL string
		Timeout time.Duration
	}
	FrontendURL string
	// OAuth is empty for an API outside the built-in table whose profile does
	// not set it; such a profile can sign in only with an API key.
	OAuth OAuthSettings
	// oauthFromProfile is true when the profile set the OAuth settings itself.
	oauthFromProfile bool
	Defaults         struct {
		OutputFormat string
	}
}

// OverrideAPIBaseURL points the config at another API (the --api-url flag or
// ECHOPOINT_API_URL). OAuth settings that the profile did not set itself follow
// the new URL through the built-in table.
func (c *Config) OverrideAPIBaseURL(baseURL string) {
	c.API.BaseURL = baseURL
	if !c.oauthFromProfile {
		c.OAuth = BuiltinOAuth(baseURL)
	}
}

// IsReservedProfileName reports whether a name cannot be used for a custom
// profile because it collides with the implicit default.
func IsReservedProfileName(name string) bool {
	return name == DefaultProfile || name == ""
}

// DefaultStore is a brand-new config: no profiles, default output format.
func DefaultStore() Store {
	return Store{
		Profiles: map[string]Profile{},
		Defaults: Defaults{OutputFormat: defaultOutputFormat},
	}
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".echopoint"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func EnsureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

// LoadStore reads config.yaml. A missing file yields DefaultStore. The returned
// store is normalized (valid CurrentProfile, no legacy fields).
func LoadStore() (Store, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return Store{}, "", err
	}
	return LoadStoreFrom(path)
}

func LoadStoreFrom(path string) (Store, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultStore(), path, nil
		}
		return Store{}, "", err
	}

	var store Store
	if err := yaml.Unmarshal(data, &store); err != nil {
		return Store{}, "", err
	}

	store.normalize()
	return store, path, nil
}

// normalize ensures Profiles is non-nil, backfills the output format, drops the
// legacy layout, and resets CurrentProfile when it points at a missing profile.
func (s *Store) normalize() {
	if s.Profiles == nil {
		s.Profiles = map[string]Profile{}
	}
	if s.Defaults.OutputFormat == "" {
		s.Defaults.OutputFormat = defaultOutputFormat
	}
	// Legacy single-environment configs are intentionally not migrated into a
	// profile: the default context is enforced to production.
	s.LegacyAPI = nil

	if s.CurrentProfile == DefaultProfile {
		s.CurrentProfile = ""
	}
	if s.CurrentProfile != "" {
		if _, ok := s.Profiles[s.CurrentProfile]; !ok {
			s.CurrentProfile = ""
		}
	}
}

// ProfileNames returns the user-created profile names, sorted.
func (s *Store) ProfileNames() []string {
	names := make([]string, 0, len(s.Profiles))
	for name := range s.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ActiveProfileName is the effective profile (CurrentProfile or the default).
func (s *Store) ActiveProfileName() string {
	if s.CurrentProfile == "" {
		return DefaultProfile
	}
	return s.CurrentProfile
}

// Resolve produces the active Config for the given profile override. An empty
// override selects CurrentProfile; if that is also empty, the enforced default
// (api.echopoint.dev) is returned. Unknown profile names error.
func (s *Store) Resolve(profileOverride string) (Config, error) {
	name := profileOverride
	if name == "" {
		name = s.CurrentProfile
	}

	// Default profile: enforced production endpoint, not user-overridable.
	if name == "" || name == DefaultProfile {
		cfg := Config{Profile: DefaultProfile, FrontendURL: defaultFrontendURL}
		cfg.API.BaseURL = defaultAPIBaseURL
		cfg.API.Timeout = defaultTimeout
		cfg.OAuth = BuiltinOAuth(cfg.API.BaseURL)
		cfg.Defaults.OutputFormat = s.outputFormat()
		return cfg, nil
	}

	profile, ok := s.Profiles[name]
	if !ok {
		return Config{}, fmt.Errorf("unknown profile %q (configured: %v)", name, s.ProfileNames())
	}

	cfg := Config{Profile: name}
	cfg.API.BaseURL = profile.APIBaseURL
	if cfg.API.BaseURL == "" {
		cfg.API.BaseURL = defaultAPIBaseURL
	}
	cfg.FrontendURL = profile.FrontendURL
	if cfg.FrontendURL == "" {
		cfg.FrontendURL = defaultFrontendURL
	}
	cfg.API.Timeout = profile.Timeout
	if cfg.API.Timeout <= 0 {
		cfg.API.Timeout = defaultTimeout
	}
	if (profile.OAuthIssuer == "") != (profile.OAuthClientID == "") {
		return Config{}, fmt.Errorf(
			"profile %q: set both oauth_issuer and oauth_client_id, or neither", name)
	}
	cfg.OAuth = BuiltinOAuth(cfg.API.BaseURL)
	if profile.OAuthIssuer != "" {
		cfg.OAuth = OAuthSettings{Issuer: profile.OAuthIssuer, ClientID: profile.OAuthClientID}
		cfg.oauthFromProfile = true
	}
	cfg.Defaults.OutputFormat = s.outputFormat()
	return cfg, nil
}

func (s *Store) outputFormat() string {
	if s.Defaults.OutputFormat == "" {
		return defaultOutputFormat
	}
	return s.Defaults.OutputFormat
}

func SaveStore(store Store) (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", err
	}
	if err := EnsureConfigDir(); err != nil {
		return "", err
	}
	return SaveStoreTo(path, store)
}

// SaveStoreTo writes the store to the given file, creating its directory.
func SaveStoreTo(path string, store Store) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}

	store.LegacyAPI = nil
	data, err := yaml.Marshal(store)
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}
