package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var appDebugLogMu sync.Mutex
var mouseDebugSeq atomic.Uint64
var latestQueuedMouseMotionSeq atomic.Uint64
var latestQueuedMouseWheelSeq atomic.Uint64
var latestMouseBoundaryAt atomic.Int64

func (m *Model) debugLog(event string, kv ...any) {
	if m == nil {
		return
	}
	path := tuiDebugLogPath(m.cfg.LogFilePath)
	if path == "" {
		return
	}
	appendDebugLogLine(path, event, kv...)
}

func tuiDebugLogPath(defaultPath string) string {
	value := strings.TrimSpace(os.Getenv("TERMX_TUI_DEBUG_LOG"))
	if value == "" {
		return ""
	}
	if value == "1" || strings.EqualFold(value, "true") {
		return strings.TrimSpace(defaultPath)
	}
	return value
}

func appendDebugLogLine(path, event string, kv ...any) {
	appDebugLogMu.Lock()
	defer appDebugLogMu.Unlock()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()

	var b strings.Builder
	b.WriteString("time=")
	b.WriteString(time.Now().UTC().Format(time.RFC3339Nano))
	b.WriteString(` level="DEBUG" component="tuiv2-app" event=`)
	b.WriteString(strconv.Quote(event))
	for i := 0; i < len(kv); i += 2 {
		key := fmt.Sprint(kv[i])
		value := "<missing>"
		if i+1 < len(kv) {
			value = fmt.Sprint(kv[i+1])
		}
		b.WriteByte(' ')
		b.WriteString(sanitizeDebugKey(key))
		b.WriteByte('=')
		b.WriteString(strconv.Quote(value))
	}
	b.WriteByte('\n')
	_, _ = file.WriteString(b.String())
}

func mouseDebugLogPath() string {
	return strings.TrimSpace(os.Getenv("TERMX_DEBUG_MOUSE_LOG"))
}

func appendMouseDebugLog(event string, kv ...any) {
	path := mouseDebugLogPath()
	if path == "" {
		return
	}
	appendDebugLogLine(path, event, kv...)
}

func nextMouseDebugSeq() uint64 {
	return mouseDebugSeq.Add(1)
}

func noteQueuedMouseMotion(seq uint64) {
	latestQueuedMouseMotionSeq.Store(seq)
}

func latestQueuedMotionSeq() uint64 {
	return latestQueuedMouseMotionSeq.Load()
}

func noteQueuedMouseWheel(seq uint64) {
	latestQueuedMouseWheelSeq.Store(seq)
}

func latestQueuedWheelSeq() uint64 {
	return latestQueuedMouseWheelSeq.Load()
}

func noteMouseBoundaryQueued(at time.Time) {
	if at.IsZero() {
		return
	}
	latestMouseBoundaryAt.Store(at.UnixNano())
}

func latestMouseBoundaryQueuedAt() time.Time {
	at := latestMouseBoundaryAt.Load()
	if at == 0 {
		return time.Time{}
	}
	return time.Unix(0, at).UTC()
}

func sanitizeDebugKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "field"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_' || r == '-':
			return r
		default:
			return '_'
		}
	}, key)
}
