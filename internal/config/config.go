package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultBaseURL        = "https://apidev.echopoint.dev"
	defaultOutputFormat   = "table"
	defaultClerkSecretKey = "sk_test_fWOXWmvTE8XYyW96FX7rcg724kdrP0wR3ivd9qN5uT"
)

type Config struct {
	API struct {
		BaseURL string        `yaml:"base_url"`
		Timeout time.Duration `yaml:"timeout"`
	} `yaml:"api"`
	Auth struct {
		ClerkSecretKey string `yaml:"clerk_secret_key"`
	} `yaml:"auth"`
	Defaults struct {
		OutputFormat string `yaml:"output_format"`
	} `yaml:"defaults"`
}

func Default() Config {
	cfg := Config{}
	cfg.API.BaseURL = defaultBaseURL
	cfg.API.Timeout = 30 * time.Second
	cfg.Auth.ClerkSecretKey = defaultClerkSecretKey
	cfg.Defaults.OutputFormat = defaultOutputFormat
	return cfg
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

func Load() (Config, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, "", err
	}
	return LoadFrom(path)
}

func LoadFrom(path string) (Config, string, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, path, nil
		}
		return Config{}, "", err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, "", err
	}

	if cfg.API.BaseURL == "" {
		cfg.API.BaseURL = defaultBaseURL
	}
	if cfg.Auth.ClerkSecretKey == "" {
		cfg.Auth.ClerkSecretKey = defaultClerkSecretKey
	}
	if cfg.Defaults.OutputFormat == "" {
		cfg.Defaults.OutputFormat = defaultOutputFormat
	}

	return cfg, path, nil
}

func Save(cfg Config) (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", err
	}
	if err := EnsureConfigDir(); err != nil {
		return "", err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}
