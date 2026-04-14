package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lozzow/termx/tuiv2/workbench"
)

var sharedTerminalTracePath = strings.TrimSpace(os.Getenv("TERMX_DEBUG_TRACE_SHARED"))

func appendSharedTerminalTrace(event string, format string, args ...any) {
	if sharedTerminalTracePath == "" {
		return
	}
	var b strings.Builder
	b.WriteString(time.Now().Format(time.RFC3339Nano))
	b.WriteByte(' ')
	b.WriteString(event)
	if format != "" {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprintf(format, args...))
	}
	b.WriteByte('\n')
	f, err := os.OpenFile(sharedTerminalTracePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(b.String())
}

func traceRect(rect workbench.Rect) string {
	return fmt.Sprintf("%d,%d %dx%d", rect.X, rect.Y, rect.W, rect.H)
}
