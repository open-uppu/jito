package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ProviderConfig struct {
	Name      string `yaml:"name"`
	BaseURL   string `yaml:"base_url"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type FallbackProvider struct {
	Name    string `yaml:"name"`
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
}

type HeartbeatConfig struct {
	Enabled         bool   `yaml:"enabled"`
	IntervalSeconds int    `yaml:"interval_seconds"`
	LogDir          string `yaml:"log_dir"`
}

type Config struct {
	Provider           ProviderConfig     `yaml:"provider"`
	FallbackProviders  []FallbackProvider `yaml:"fallback_providers"`
	ModeDefault        string             `yaml:"mode_default"`
	Heartbeat          HeartbeatConfig    `yaml:"heartbeat"`
}

func defaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".jito", "config.yaml"), nil
}

// Load reads config from default path or returns error if missing.
func Load() (*Config, error) {
	path, err := defaultPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads config from explicit path.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.ModeDefault == "" {
		cfg.ModeDefault = "universal"
	}
	return &cfg, nil
}