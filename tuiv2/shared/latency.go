package shared

import (
	"os"
	"strings"
	"time"
)

// RemoteLatencyProfileEnabled reports whether termx should prefer lower local
// batching delays over the more conservative "merge a little more" defaults.
//
// TERMX_REMOTE_LATENCY accepts:
// - "1", "true", "yes", "on", "remote": force remote profile
// - "0", "false", "no", "off", "local": force local profile
// - "", "auto": auto-detect based on common SSH environment variables
func RemoteLatencyProfileEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TERMX_REMOTE_LATENCY"))) {
	case "", "auto":
		return likelySSHSession()
	case "1", "true", "yes", "on", "remote":
		return true
	case "0", "false", "no", "off", "local":
		return false
	default:
		return likelySSHSession()
	}
}

func likelySSHSession() bool {
	return strings.TrimSpace(os.Getenv("SSH_CONNECTION")) != "" ||
		strings.TrimSpace(os.Getenv("SSH_CLIENT")) != "" ||
		strings.TrimSpace(os.Getenv("SSH_TTY")) != ""
}

// HostPaletteProbeEnabled reports whether termx should query the host terminal
// for its 16-color palette on startup.
//
// TERMX_HOST_PALETTE_PROBE accepts:
// - "1", "true", "yes", "on", "always": force probe
// - "0", "false", "no", "off", "never": disable probe
// - "", "auto": disable in remote-latency mode, enable otherwise
func HostPaletteProbeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TERMX_HOST_PALETTE_PROBE"))) {
	case "", "auto":
		return !RemoteLatencyProfileEnabled()
	case "1", "true", "yes", "on", "always":
		return true
	case "0", "false", "no", "off", "never":
		return false
	default:
		return !RemoteLatencyProfileEnabled()
	}
}

// BubbleTeaRendererEnabled reports whether termx should keep Bubble Tea's
// standard renderer enabled on TTYs instead of using the direct frame writer
// path. This is intended for apples-to-apples experiments, not the default.
func BubbleTeaRendererEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TERMX_USE_BUBBLETEA_RENDERER"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// DurationOverride returns the duration from env when present and valid.
// Invalid or negative values fall back to the provided default.
func DurationOverride(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}
