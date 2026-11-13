package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrSessionExpired means the stored session cannot be renewed any more.
var ErrSessionExpired = errors.New("stored credentials have expired; run 'echopoint auth login' again")

const (
	grantRefreshToken = "refresh_token"

	// refreshSkew is how long before expiry a token counts as due for renewal.
	refreshSkew = 60 * time.Second

	lockPollInterval = 25 * time.Millisecond
)

// These are variables so a test can shorten them. lockWait is longer than
// lockStale, so a waiter outlasts the lock of a process that died.
var (
	// A lock older than lockStale belongs to a process that died; the longest
	// holder works for one 30 s token request.
	lockStale = 45 * time.Second
	// lockWait is how long a caller waits for another process to finish.
	lockWait = 60 * time.Second
)

// RefreshOptions configure session renewal; the zero value is the real setup.
type RefreshOptions struct {
	HTTPClient *http.Client
	Now        func() time.Time
}

func (o RefreshOptions) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{Timeout: oauthHTTPTimeout}
}

func (o RefreshOptions) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (c *Credentials) needsRefresh(now time.Time) bool {
	return c.IsOAuth() && (c.ExpiresAt == nil || !c.ExpiresAt.After(now.Add(refreshSkew)))
}

// EnsureSession returns the stored credentials of a profile, renewing an OAuth
// session that expires within 60 seconds. It returns nil when nothing is
// stored. The renewal runs under a cross-process lock, and the file is read
// again once the lock is held, because another process may have renewed first.
//
// Refresh tokens rotate: the old one stops working at once, so the new pair is
// saved before the lock is released and a token is never sent twice.
func EnsureSession(ctx context.Context, profile string, opts RefreshOptions) (*Credentials, error) {
	creds, _, err := LoadCredentials(profile)
	if err != nil || creds == nil || !creds.needsRefresh(opts.now()) {
		return creds, err
	}

	unlock, err := lockProfile(ctx, profile)
	if err != nil {
		return nil, err
	}
	defer unlock()

	creds, _, err = LoadCredentials(profile)
	if err != nil || creds == nil || !creds.needsRefresh(opts.now()) {
		return creds, err
	}

	tr, err := postToken(ctx, opts.client(), strings.TrimRight(creds.OAuthIssuer, "/")+"/oauth/token", url.Values{
		"grant_type":    {grantRefreshToken},
		formClientID:    {creds.OAuthClientID},
		"refresh_token": {creds.RefreshToken},
	})
	if err != nil {
		var oerr *oauthError
		if errors.As(err, &oerr) && oerr.Code == "invalid_grant" {
			if clearErr := clearSession(profile, *creds); clearErr != nil {
				return nil, clearErr
			}
			return nil, ErrSessionExpired
		}
		return nil, fmt.Errorf("renew session: %w", err)
	}

	fresh := credentialsFromToken(tr, opts.now(), creds.OAuthIssuer, creds.OAuthClientID)
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = creds.RefreshToken
	}
	updated := *creds
	updated.AccessToken = fresh.AccessToken
	updated.RefreshToken = fresh.RefreshToken
	updated.ExpiresAt = fresh.ExpiresAt
	if _, err := SaveCredentials(profile, updated); err != nil {
		return nil, fmt.Errorf("save renewed session: %w", err)
	}
	return &updated, nil
}

// clearSession drops the session fields and keeps the API key settings.
func clearSession(profile string, creds Credentials) error {
	creds.AccessToken = ""
	creds.RefreshToken = ""
	creds.ExpiresAt = nil
	creds.OAuthIssuer = ""
	creds.OAuthClientID = ""
	if creds.APIKey == "" {
		_, err := DeleteCredentials(profile)
		return err
	}
	_, err := SaveCredentials(profile, creds)
	return err
}

// RevokeRefreshToken asks the issuer to revoke the stored refresh token. Callers
// treat a failure as non-fatal.
func RevokeRefreshToken(ctx context.Context, creds Credentials, hc *http.Client) error {
	if creds.RefreshToken == "" || !ValidIssuer(creds.OAuthIssuer) || creds.OAuthClientID == "" {
		return nil
	}
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	form := url.Values{
		"token":           {creds.RefreshToken},
		formClientID:      {creds.OAuthClientID},
		"token_type_hint": {grantRefreshToken},
	}
	endpoint := strings.TrimRight(creds.OAuthIssuer, "/") + "/oauth/token/revoke"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("revoke request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("revoke refused (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// lockProfile takes an exclusive lock on a profile's credentials across
// processes. It is a lock file created with O_EXCL, which behaves the same on
// macOS, Linux and Windows; a file older than lockStale is taken over.
func lockProfile(ctx context.Context, profile string) (func(), error) {
	path, err := CredentialsPath(profile)
	if err != nil {
		return nil, err
	}
	path = strings.TrimSuffix(path, ".json") + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("lock credentials: %w", err)
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for another echopoint process to renew the session")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockPollInterval):
		}
	}
}

// SessionSource hands out the access token of an OAuth session and renews it
// before it expires. It is safe for concurrent use: callers share one renewal.
type SessionSource struct {
	profile string
	opts    RefreshOptions

	mu    sync.Mutex
	token string
	exp   *time.Time
}

// NewSessionSource starts from credentials already loaded for the profile.
func NewSessionSource(profile string, initial Credentials, opts RefreshOptions) *SessionSource {
	return &SessionSource{profile: profile, opts: opts, token: initial.AccessToken, exp: initial.ExpiresAt}
}

// Token returns a token that is valid for at least 60 more seconds.
func (s *SessionSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.exp != nil && s.exp.After(s.opts.now().Add(refreshSkew)) {
		return s.token, nil
	}
	creds, err := EnsureSession(ctx, s.profile, s.opts)
	if err != nil {
		return "", err
	}
	if creds == nil || creds.AccessToken == "" {
		return "", ErrSessionExpired
	}
	s.token, s.exp = creds.AccessToken, creds.ExpiresAt
	return s.token, nil
}
