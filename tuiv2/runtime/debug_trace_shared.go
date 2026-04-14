package runtime

import (
	"fmt"
	"os"
	"strings"
	"time"
)

var sharedTerminalTracePath = strings.TrimSpace(os.Getenv("TERMX_DEBUG_TRACE_SHARED"))

func appendSharedTerminalTrace(event string, terminal *TerminalRuntime, format string, args ...any) {
	if sharedTerminalTracePath == "" {
		return
	}
	var b strings.Builder
	b.WriteString(time.Now().Format(time.RFC3339Nano))
	b.WriteByte(' ')
	b.WriteString(event)
	if terminal != nil {
		b.WriteString(" term=")
		b.WriteString(terminal.TerminalID)
		b.WriteString(" owner=")
		b.WriteString(traceValueOrDash(terminal.OwnerPaneID))
		b.WriteString(" control=")
		b.WriteString(traceValueOrDash(terminal.ControlPaneID))
		b.WriteString(" explicit=")
		b.WriteString(fmt.Sprintf("%t", terminal.RequiresExplicitOwner))
		b.WriteString(" pending=")
		b.WriteString(fmt.Sprintf("%t", terminal.PendingOwnerResize))
		b.WriteString(" bound=")
		b.WriteString(tracePaneList(terminal.BoundPaneIDs))
		b.WriteString(" live=")
		b.WriteString(traceTerminalLiveSize(terminal))
		b.WriteString(" snap=")
		b.WriteString(traceTerminalSnapshotSize(terminal))
	}
	if format != "" {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprintf(format, args...))
	}
	b.WriteByte('\n')
	appendSharedTerminalTraceLine(b.String())
}

func appendSharedTerminalTraceLine(line string) {
	if sharedTerminalTracePath == "" || line == "" {
		return
	}
	f, err := os.OpenFile(sharedTerminalTracePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func traceValueOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func tracePaneList(ids []string) string {
	if len(ids) == 0 {
		return "[]"
	}
	return "[" + strings.Join(ids, ",") + "]"
}

func traceTerminalLiveSize(terminal *TerminalRuntime) string {
	if terminal == nil || terminal.VTerm == nil {
		return "-"
	}
	cols, rows := terminal.VTerm.Size()
	if cols <= 0 || rows <= 0 {
		return "-"
	}
	return fmt.Sprintf("%dx%d", cols, rows)
}

func traceTerminalSnapshotSize(terminal *TerminalRuntime) string {
	if terminal == nil || terminal.Snapshot == nil || terminal.Snapshot.Size.Cols == 0 || terminal.Snapshot.Size.Rows == 0 {
		return "-"
	}
	return fmt.Sprintf("%dx%d", terminal.Snapshot.Size.Cols, terminal.Snapshot.Size.Rows)
}
