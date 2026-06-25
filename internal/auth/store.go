package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"echopoint-cli/internal/config"
)

// credentialsSubdir holds one <profile>.json credentials file per profile.
const credentialsSubdir = "credentials"

type Credentials struct {
	AccessToken    string     `json:"access_token,omitempty"`
	APIKey         string     `json:"api_key,omitempty"`
	PreferAPIKey   bool       `json:"prefer_api_key,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	OrganizationID string     `json:"organization_id,omitempty"`
}

// sanitizeProfile keeps credentials filenames safe (no path traversal).
func sanitizeProfile(profile string) string {
	if profile == "" {
		return config.DefaultProfile
	}
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '-'
		}
	}, profile)
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return config.DefaultProfile
	}
	return cleaned
}

func credentialsDir() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsSubdir), nil
}

// CredentialsPath returns the credentials file for a profile.
//
// Note: the pre-profiles ~/.echopoint/credentials.json is intentionally NOT
// migrated. The old single-environment default pointed at a different endpoint
// than the new enforced default, so reusing that token here would be wrong —
// users re-authenticate per profile with `echopoint auth login`.
func CredentialsPath(profile string) (string, error) {
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sanitizeProfile(profile)+".json"), nil
}

func LoadCredentials(profile string) (*Credentials, string, error) {
	path, err := CredentialsPath(profile)
	if err != nil {
		return nil, "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, path, nil
		}
		return nil, "", err
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, "", err
	}

	if creds.AccessToken == "" && creds.APIKey == "" {
		return nil, path, nil
	}

	return &creds, path, nil
}

func SaveCredentials(profile string, creds Credentials) (string, error) {
	path, err := CredentialsPath(profile)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}

func DeleteCredentials(profile string) (string, error) {
	path, err := CredentialsPath(profile)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		return "", err
	}
	return path, nil
}
