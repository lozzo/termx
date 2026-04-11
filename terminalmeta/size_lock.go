package terminalmeta

import "strings"

const SizeLockTag = "termx.size_lock"

const (
	SizeLockLockedIcon   = "󰌾"
	SizeLockUnlockedIcon = "󰍀"
)

const (
	SizeLockOff  = "off"
	SizeLockWarn = "warn"
	SizeLockLock = "lock"
)

func SizeLockMode(tags map[string]string) string {
	if len(tags) == 0 {
		return SizeLockOff
	}
	switch strings.ToLower(strings.TrimSpace(tags[SizeLockTag])) {
	case SizeLockLock, "locked", "hard", "true", "on", "1":
		return SizeLockLock
	case SizeLockWarn:
		return SizeLockWarn
	default:
		return SizeLockOff
	}
}

func SizeLocked(tags map[string]string) bool {
	return SizeLockMode(tags) == SizeLockLock
}

func SizeLockButtonLabel(locked bool) string {
	if locked {
		return "[" + SizeLockLockedIcon + "]"
	}
	return "[" + SizeLockUnlockedIcon + "]"
}
