package termx

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLiveOutputFPS            = 60
	liveOutputInteractiveWindow     = 24 * time.Millisecond
	liveOutputInteractiveFrameFloor = 4 * time.Millisecond
)

type liveOutputThrottleConfig struct {
	FPS int
}

func defaultLiveOutputThrottleConfig() liveOutputThrottleConfig {
	return liveOutputThrottleConfig{
		FPS: parseLiveOutputFPSEnv("TERMX_LIVE_OUTPUT_FPS", defaultLiveOutputFPS),
	}
}

func sanitizeLiveOutputThrottleConfig(cfg liveOutputThrottleConfig) liveOutputThrottleConfig {
	if cfg.FPS < 0 {
		cfg.FPS = 0
	}
	return cfg
}

func parseLiveOutputFPSEnv(name string, fallback int) int {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch raw {
	case "":
		return fallback
	case "0", "off", "false", "no", "disable", "disabled":
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func (cfg liveOutputThrottleConfig) enabled() bool {
	return cfg.FPS > 0
}

func (cfg liveOutputThrottleConfig) frameInterval() time.Duration {
	if cfg.FPS <= 0 {
		return 0
	}
	return time.Second / time.Duration(cfg.FPS)
}

func (cfg liveOutputThrottleConfig) interactiveFrameInterval() time.Duration {
	base := cfg.frameInterval()
	if base <= 0 {
		return 0
	}
	delay := base / 4
	if delay <= 0 {
		delay = time.Millisecond
	}
	if delay > liveOutputInteractiveFrameFloor {
		delay = liveOutputInteractiveFrameFloor
	}
	if delay > base {
		return base
	}
	return delay
}
