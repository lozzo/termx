package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Enabled     bool
	ControlURL  string
	HubURL      string
	HubURLs     []string
	AccessToken string
	DataDir     string
	DeviceName  string
	Region      string
	Mode        string
	AllowLAN    bool
	LANIPs      []string
	TokenTTL    time.Duration
	modeValid   bool
}

func Normalize(cfg Config) Config {
	cfg.ControlURL = strings.TrimSpace(cfg.ControlURL)
	cfg.HubURL = strings.TrimSpace(cfg.HubURL)
	cfg.HubURLs = normalizeHubURLs(cfg.HubURL, cfg.HubURLs)
	if cfg.HubURL == "" && len(cfg.HubURLs) > 0 {
		cfg.HubURL = cfg.HubURLs[0]
	}
	cfg.AccessToken = strings.TrimSpace(cfg.AccessToken)
	cfg.DataDir = strings.TrimSpace(cfg.DataDir)
	cfg.DeviceName = strings.TrimSpace(cfg.DeviceName)
	cfg.Region = strings.TrimSpace(cfg.Region)
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "local", "hub", "both":
		cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
		cfg.modeValid = true
	case "":
		cfg.Mode = "both"
		cfg.modeValid = true
	default:
		cfg.Mode = "both"
		cfg.modeValid = false
	}
	cfg.LANIPs = normalizeStringSlice(cfg.LANIPs)

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

func normalizeHubURLs(primary string, values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values)+1)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(primary)
	for _, value := range values {
		add(value)
	}
	return out
}

func normalizeStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
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
	if !c.modeValid {
		return errors.New("remote mode must be one of local, hub, or both")
	}
	if c.TokenTTL < 0 {
		return errors.New("remote token ttl must not be negative")
	}
	return nil
}

func ModeIncludesLocal(mode string) bool {
	return mode == "local" || mode == "both"
}

func ModeIncludesHub(mode string) bool {
	return mode == "hub" || mode == "both"
}
