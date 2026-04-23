package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var appDebugLogMu sync.Mutex
var mouseDebugSeq atomic.Uint64
var latestQueuedMouseMotionSeq atomic.Uint64
var latestQueuedMouseWheelSeq atomic.Uint64
var latestMouseBoundaryAt atomic.Int64

func (m *Model) debugLog(event string, kv ...any) {
	if m == nil || strings.TrimSpace(m.cfg.LogFilePath) == "" {
		return
	}
	appendDebugLogLine(m.cfg.LogFilePath, event, kv...)
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

func debugMessageFields(msg tea.Msg) []any {
	switch typed := msg.(type) {
	case terminalTitleMsg:
		return []any{
			"msg", "terminal_title",
			"terminal_id", typed.TerminalID,
			"title", typed.Title,
		}
	case InvalidateMsg:
		return []any{
			"msg", "invalidate",
		}
	default:
		return []any{
			"msg", fmt.Sprintf("%T", msg),
		}
	}
}
