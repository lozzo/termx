package termx

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLiveOutputFPS              = 60
	liveOutputInteractiveWindow       = 24 * time.Millisecond
	liveOutputInteractiveFrameFloor   = 4 * time.Millisecond
	liveOutputInteractiveBypassDelay  = 500 * time.Microsecond
	liveOutputSynchronizedOutputBegin = "\x1b[?2026h"
	liveOutputSynchronizedOutputEnd   = "\x1b[?2026l"
)

var serverLiveOutputSyncWaitStep = 500 * time.Microsecond
var serverLiveOutputSyncWaitBudget = 5 * time.Millisecond

type liveOutputThrottleConfig struct {
	FPS int
}

type liveOutputSyncState struct {
	synchronizedOutputActive bool
	synchronizedOutputTail   string
	waited                   time.Duration
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

func updateLiveOutputSyncState(state *liveOutputSyncState, payload []byte) bool {
	if state == nil {
		return false
	}
	if len(payload) == 0 {
		return state.synchronizedOutputActive
	}

	tail := state.synchronizedOutputTail
	combined := tail + string(payload)
	tailLen := len(tail)

	for i := 0; i < len(combined); {
		switch {
		case strings.HasPrefix(combined[i:], liveOutputSynchronizedOutputBegin):
			if i+len(liveOutputSynchronizedOutputBegin) <= tailLen {
				i++
				continue
			}
			state.synchronizedOutputActive = true
			i += len(liveOutputSynchronizedOutputBegin)
		case strings.HasPrefix(combined[i:], liveOutputSynchronizedOutputEnd):
			if i+len(liveOutputSynchronizedOutputEnd) <= tailLen {
				i++
				continue
			}
			state.synchronizedOutputActive = false
			i += len(liveOutputSynchronizedOutputEnd)
		default:
			i++
		}
	}

	maxTail := len(liveOutputSynchronizedOutputBegin) - 1
	if len(combined) > maxTail {
		state.synchronizedOutputTail = combined[len(combined)-maxTail:]
	} else {
		state.synchronizedOutputTail = combined
	}
	return state.synchronizedOutputActive
}

func resetLiveOutputSyncState(state *liveOutputSyncState) {
	if state == nil {
		return
	}
	state.synchronizedOutputActive = false
	state.synchronizedOutputTail = ""
	state.waited = 0
}
