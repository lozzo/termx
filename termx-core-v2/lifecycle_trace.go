package termxcorev2

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"
)

func coreLifecycleTrace(logger *slog.Logger, event string, attrs ...any) {
	if logger == nil || !coreLifecycleTraceEnabled() {
		return
	}
	values := append([]any{"event", event}, attrs...)
	logger.Info("core-v2 lifecycle trace", values...)
}

func coreLifecycleTraceEnabled() bool {
	return coreTraceEnvEnabled("TERMX_TUI_INPUT_TRACE") || coreTraceEnvEnabled("TERMX_TUI_DIAG")
}

func coreHistoryTraceEnabled() bool {
	return coreTraceEnvEnabled("TERMX_HISTORY_TRACE")
}

func coreHistoryTrace(logger *slog.Logger, stage string, attrs ...any) {
	if logger == nil || !coreHistoryTraceEnabled() {
		return
	}
	values := append([]any{"stage", stage}, attrs...)
	logger.Info("core-v2 history trace", values...)
}

func coreTraceEnvEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "on", "yes", "debug":
		return true
	default:
		return false
	}
}

func coreTerminalInfoAttrs(info TerminalInfo) []any {
	return []any{
		"terminal_id", info.ID,
		"state", string(info.State),
		"exit_code", coreTraceExitCode(info.ExitCode),
		"exited_at", coreTraceTime(info.ExitedAt),
		"command", strings.Join(info.Command, " "),
		"cols", info.Size.Cols,
		"rows", info.Size.Rows,
	}
}

func coreTraceExitCode(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func coreTraceTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func coreTerminalListSummary(items []TerminalInfo) string {
	if len(items) == 0 {
		return ""
	}
	ordered := append([]TerminalInfo(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})
	parts := make([]string, 0, len(ordered))
	for _, item := range ordered {
		exitCode := ""
		if item.ExitCode != nil {
			exitCode = fmt.Sprintf(" code=%d", *item.ExitCode)
		}
		exitedAt := ""
		if !item.ExitedAt.IsZero() {
			exitedAt = " exited_at=" + item.ExitedAt.UTC().Format(time.RFC3339)
		}
		parts = append(parts, fmt.Sprintf("%s:%s%s%s", item.ID, item.State, exitCode, exitedAt))
	}
	return strings.Join(parts, ",")
}
