package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func isContextLifecycleError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// 正常退出、重连或 watcher 重订阅会触发 context 取消；只记日志，不能污染 pane/toast。
func logEffectError(logger *slog.Logger, component string, err error, attrs ...any) {
	if logger == nil || err == nil {
		return
	}
	values := append([]any{"component", component, "error", err}, attrs...)
	if isContextLifecycleError(err) {
		logger.Debug("tui-v3 effect stopped", values...)
		return
	}
	logger.Warn("tui-v3 effect failed", values...)
}

func logLifecycleTrace(logger *slog.Logger, event string, attrs ...any) {
	if logger == nil {
		return
	}
	if !terminalInputTraceEnabled() && !diagnosticsEnabledFromEnv(tuiDiagnosticsEnv) {
		return
	}
	values := append([]any{"event", event}, attrs...)
	logger.Info("tui-v3 lifecycle trace", values...)
}

func historyTraceEnabled() bool {
	return diagnosticsEnabledFromEnv(tuiHistoryTraceEnv)
}

func logHistoryTrace(logger *slog.Logger, stage string, attrs ...any) {
	if logger == nil || !historyTraceEnabled() {
		return
	}
	values := append([]any{"stage", stage}, attrs...)
	logger.Info("tui-v3 history trace", values...)
}

func historyTraceCursorAttrs(prefix string, cursor state.HistoryCursor) []any {
	return []any{
		prefix + "_cursor_valid", cursor.Valid,
		prefix + "_cursor_line", cursor.BeforeLineID,
		prefix + "_cursor_row", cursor.BeforeRowInLine,
		prefix + "_cursor_index", cursor.BeforeRowIndex,
		prefix + "_cursor_segment", cursor.Segment,
	}
}

func historyTraceBoundaryAttrs(prefix string, boundary state.HistoryBoundary) []any {
	return []any{
		prefix + "_boundary_first", boundary.FirstLineID,
		prefix + "_boundary_last", boundary.LastLineID,
	}
}
