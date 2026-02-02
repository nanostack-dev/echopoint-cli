package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"echopoint-cli/internal/config"
)

const credentialsFileName = "credentials.json"

type Credentials struct {
	AccessToken string     `json:"access_token"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

func CredentialsPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsFileName), nil
}

func LoadCredentials() (*Credentials, string, error) {
	path, err := CredentialsPath()
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

	if creds.AccessToken == "" {
		return nil, path, nil
	}

	return &creds, path, nil
}

func SaveCredentials(creds Credentials) (string, error) {
	path, err := CredentialsPath()
	if err != nil {
		return "", err
	}
	if err := config.EnsureConfigDir(); err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}

func DeleteCredentials() (string, error) {
	path, err := CredentialsPath()
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
