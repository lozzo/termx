package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Enabled     bool
	ControlURL  string
	HubURL      string
	AccessToken string
	DataDir     string
	DeviceName  string
}

func Normalize(cfg Config) Config {
	cfg.ControlURL = strings.TrimSpace(cfg.ControlURL)
	cfg.HubURL = strings.TrimSpace(cfg.HubURL)
	cfg.AccessToken = strings.TrimSpace(cfg.AccessToken)
	cfg.DataDir = strings.TrimSpace(cfg.DataDir)
	cfg.DeviceName = strings.TrimSpace(cfg.DeviceName)

	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	if cfg.DeviceName == "" {
		if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
			cfg.DeviceName = strings.TrimSpace(hostname)
		}
	}
	return cfg
}

func DefaultDataDir() string {
	if stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); stateHome != "" {
		return filepath.Join(stateHome, "termx", "remote")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".local", "state", "termx", "remote")
	}
	return filepath.Join(os.TempDir(), "termx-remote")
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return errors.New("remote data dir is required")
	}
	if c.ControlURL != "" && c.AccessToken == "" {
		return errors.New("remote access token is required when control URL is configured")
	}
	return nil
}
