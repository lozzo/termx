package app

import (
	"context"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type CopyModeDeps struct {
	Core      services.CoreClient
	Clipboard services.ClipboardService
}

type CopyModeHistoryResultMsg struct {
	RequestID state.RequestID
	Result    services.HistoryResult
	Err       error
}

func (CopyModeHistoryResultMsg) isMsg() {}

type CopyModeCopyResultMsg struct {
	Text string
	Err  error
}

func (CopyModeCopyResultMsg) isMsg() {}

type CopyModeSetMarkMsg struct {
	Position state.CopyPosition
}

func (CopyModeSetMarkMsg) isMsg() {}

func NewCopyModeReducer(deps CopyModeDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case InputMsg:
			return reduceCopyModeInput(root, msg, deps)
		case CopyModeHistoryResultMsg:
			return reduceCopyModeHistoryResult(root, msg)
		case CopyModeCopyResultMsg:
			return reduceCopyModeCopyResult(root, msg)
		case CopyModeSetMarkMsg:
			history, copyMode, viewID, ok := activeCopyHistorySession(root)
			if !ok {
				return root, nil
			}
			copyMode = copyMode.SetMark(msg.Position).RefreshLogicalSelection(history)
			root = root.WithCopyHistorySession(viewID, history, clampCopyCursor(copyMode, history))
			return root.Advance(), []Effect{handledEffect{}}
		default:
			return root, nil
		}
	}
}

func reduceCopyModeInput(root state.Root, msg InputMsg, deps CopyModeDeps) (state.Root, []Effect) {
	shell := root.Shell.ReadonlyDefaults()
	if shell.Overlay.Open {
		return root, nil
	}
	// 中文说明：copy/history 激活后拥有当前 terminal view 的键鼠输入；
	// 未识别的输入也必须消费，不能漏到子进程。
	active := copyModeOwnsActiveInput(root)
	intent := input.RouteWithOptions(msg.Event, input.RouteOptions{
		Mode:                     inputMode(shell.InteractionMode),
		CopyModeActive:           active,
		TerminalMousePassthrough: msg.TerminalMousePassthrough,
	})
	if intent.Kind == input.IntentEnterCopyMode && !active {
		next, effects := beginCopyModeLatest(root, deps)
		return next, append([]Effect{handledEffect{}}, effects...)
	}
	if !active {
		return root, nil
	}
	switch intent.Kind {
	case input.IntentEnterCopyMode:
		return root, []Effect{handledEffect{}}
	case input.IntentExitCopyMode:
		next, effects := exitCopyModeWithRelease(root, deps)
		return next.Advance(), append([]Effect{handledEffect{}}, effects...)
	case input.IntentRequestOlder:
		next, effects := reduceCopyModeScrollOlder(root, deps, copyModeScrollRows(root.CopyMode, intent.Event))
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.IntentRequestNewer:
		next, effects := reduceCopyModeScrollNewer(root, deps, copyModeScrollRows(root.CopyMode, intent.Event))
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.IntentSetCopyMark:
		root = setCopyModeMarkFromIntent(root, intent)
		return root.Advance(), []Effect{handledEffect{}}
	case input.IntentCopySelection:
		next, effects := reduceCopyModeCopySelection(root, deps)
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.IntentOpenClipboardHistory:
		root.Shell = root.Shell.OpenClipboardHistory()
		return root.Advance(), []Effect{
			handledEffect{},
			FuncEffect{Run: func(context.Context) Msg { return ClipboardStorageLoadRequestMsg{Reason: "open"} }},
		}
	case input.IntentPasteLastCopy, input.IntentPasteClipboard:
		return root, []Effect{handledEffect{}}
	default:
		return root, []Effect{handledEffect{}}
	}
}

func beginCopyModeLatest(root state.Root, deps CopyModeDeps) (state.Root, []Effect) {
	if deps.Core == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "copy", Body: "history service missing"})
		return root.Advance(), nil
	}
	binding, ok := activeTerminalViewBinding(root)
	if !ok || binding.TerminalID == "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "copy", Body: "no active terminal"})
		return root.Advance(), nil
	}
	// 中文说明：TUI 只保存 request guard 和 view-local window；历史 truth 仍由 core-v2
	// HistoryWindow 返回，进入 copy mode 不从 live surface 合成 committed history。
	cols, rows := copyModeRequestSize(root, binding)
	requestID := nextCopyModeRequestID(root)
	history, err := (state.HistoryStore{}).BeginLatest(state.HistoryPendingRequest{
		ID:         requestID,
		Kind:       state.HistoryRequestLatest,
		PaneID:     binding.PaneID,
		ViewID:     binding.ViewID,
		TerminalID: binding.TerminalID,
		Cols:       cols,
	})
	if err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "copy", Body: err.Error()})
		return root.Advance(), nil
	}
	copyMode := state.CopyModeStore{}.BindLatest(binding.PaneID, binding.ViewID, binding.TerminalID, requestID, cols, rows, root.Surface.SurfaceForTerminal(binding.TerminalID).Snapshot())
	root = root.WithCopyHistorySession(binding.ViewID, history, copyMode)
	root = root.Advance()
	return root, []Effect{copyModeLatestEffect(deps.Core, services.HistoryLatestRequest{
		RequestID:  requestID,
		PaneID:     binding.PaneID,
		ViewID:     binding.ViewID,
		TerminalID: binding.TerminalID,
		Cols:       cols,
		Rows:       copyModeWindowRows(rows),
	})}
}

func reduceCopyModeHistoryResult(root state.Root, msg CopyModeHistoryResultMsg) (state.Root, []Effect) {
	viewID := root.CopyMode.ViewID
	history, copyMode := root.CopyHistorySessionForView(viewID)
	if history.Pending == nil || history.Pending.ID != msg.RequestID {
		return root, nil
	}
	if msg.Err != nil {
		history = history.InvalidateWindow()
		copyMode = state.CopyModeStore{}
		root = root.WithoutCopyHistorySession(viewID)
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "copy", Body: msg.Err.Error()})
		return root.Advance(), nil
	}
	pending := *history.Pending
	before := history
	// 中文说明：stale/mismatch 判定集中在 HistoryStore.ApplyWindow，reducer 不凭响应内容
	// 手工修正窗口，避免产生第二份历史 truth。
	nextHistory, insertedRows, err := history.ApplyWindow(msg.RequestID, msg.Result.Window)
	if err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "copy", Body: err.Error()})
		return root.Advance(), nil
	}
	switch pending.Kind {
	case state.HistoryRequestLatest:
		copyMode = copyMode.AcceptLatest(msg.Result.Window, nextHistory.Cols, len(nextHistory.Rows))
	case state.HistoryRequestOlder:
		copyMode = copyMode.AcceptOlder(insertedRows, before, nextHistory, msg.Result.Window, nextHistory.Cols)
	case state.HistoryRequestNewer:
		copyMode.BoundToken = msg.Result.Window.Token
		copyMode.BoundCols = firstPositiveInt(nextHistory.Cols, msg.Result.Window.Cols, copyMode.BoundCols)
		copyMode.Empty = len(nextHistory.Rows) == 0
		copyMode = ensureCopyCursorVisible(copyMode, len(nextHistory.Rows))
	}
	copyMode = clampCopyCursor(copyMode, nextHistory)
	root = root.WithCopyHistorySession(firstNonEmptyString(copyMode.ViewID, pending.ViewID, viewID), nextHistory, copyMode)
	return root.Advance(), nil
}

func reduceCopyModeScrollOlder(root state.Root, deps CopyModeDeps, rows int) (state.Root, []Effect) {
	if rows <= 0 {
		rows = 1
	}
	history, copyMode, viewID, ok := activeCopyHistorySession(root)
	if !ok {
		return root, nil
	}
	copyMode = copyMode.MoveCursor(state.CopyPosition{Row: copyMode.Cursor.Row - rows, Col: copyMode.Cursor.Col})
	copyMode = clampCopyCursor(copyMode, history)
	copyMode = ensureCopyCursorVisible(copyMode, len(history.Rows))
	root = root.WithCopyHistorySession(viewID, history, copyMode)
	if copyMode.Cursor.Row > 0 || history.OlderRequestState() != state.OlderRequestReady || deps.Core == nil {
		return root.Advance(), nil
	}
	requestID := nextCopyModeRequestID(root)
	pending := state.HistoryPendingRequest{
		ID:         requestID,
		Kind:       state.HistoryRequestOlder,
		PaneID:     copyMode.PaneID,
		ViewID:     copyMode.ViewID,
		TerminalID: copyMode.TerminalID,
		Cols:       firstPositiveInt(history.Cols, copyMode.BoundCols),
		Token:      history.Token,
		Generation: history.Generation,
		Cursor:     history.Cursor,
		Boundary:   history.Boundary,
	}
	nextHistory, err := history.BeginOlder(pending)
	if err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "copy", Body: err.Error()})
		return root.Advance(), nil
	}
	root = root.WithCopyHistorySession(viewID, nextHistory, copyMode)
	return root.Advance(), []Effect{copyModeOlderEffect(deps.Core, services.HistoryOlderRequest{
		RequestID:  requestID,
		PaneID:     pending.PaneID,
		ViewID:     pending.ViewID,
		TerminalID: pending.TerminalID,
		Cols:       pending.Cols,
		Rows:       copyModeWindowRows(copyMode.ViewRows),
		Token:      pending.Token,
		Generation: pending.Generation,
		Cursor:     pending.Cursor,
		Boundary:   pending.Boundary,
	})}
}

func reduceCopyModeScrollNewer(root state.Root, deps CopyModeDeps, rows int) (state.Root, []Effect) {
	if rows <= 0 {
		rows = 1
	}
	history, copyMode, viewID, ok := activeCopyHistorySession(root)
	if !ok {
		return root, nil
	}
	copyMode = copyMode.MoveCursor(state.CopyPosition{Row: copyMode.Cursor.Row + rows, Col: copyMode.Cursor.Col})
	copyMode = clampCopyCursor(copyMode, history)
	copyMode = ensureCopyCursorVisible(copyMode, len(history.Rows))
	root = root.WithCopyHistorySession(viewID, history, copyMode)
	if len(history.Rows) == 0 || copyMode.Cursor.Row < len(history.Rows)-1 || history.NewerRequestState() != state.NewerRequestReady || deps.Core == nil {
		return root.Advance(), nil
	}
	requestID := nextCopyModeRequestID(root)
	tail := history.Rows[len(history.Rows)-1]
	cursor := state.HistoryCursor{
		Valid:           true,
		BeforeLineID:    tail.LineID,
		BeforeRowInLine: tail.RowInLine,
		BeforeRowIndex:  tail.ProjectionRowIndex,
		Segment:         tail.Segment,
	}
	pending := state.HistoryPendingRequest{
		ID:         requestID,
		Kind:       state.HistoryRequestNewer,
		PaneID:     copyMode.PaneID,
		ViewID:     copyMode.ViewID,
		TerminalID: copyMode.TerminalID,
		Cols:       firstPositiveInt(history.Cols, copyMode.BoundCols),
		Token:      history.Token,
		Generation: history.Generation,
		Cursor:     cursor,
		Boundary:   history.Boundary,
	}
	nextHistory, err := history.BeginNewer(pending)
	if err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "copy", Body: err.Error()})
		return root.Advance(), nil
	}
	root = root.WithCopyHistorySession(viewID, nextHistory, copyMode)
	return root.Advance(), []Effect{copyModeNewerEffect(deps.Core, services.HistoryNewerRequest{
		RequestID:  requestID,
		PaneID:     pending.PaneID,
		ViewID:     pending.ViewID,
		TerminalID: pending.TerminalID,
		Cols:       pending.Cols,
		Rows:       copyModeWindowRows(copyMode.ViewRows),
		Token:      pending.Token,
		Generation: pending.Generation,
		Cursor:     pending.Cursor,
		Boundary:   pending.Boundary,
	})}
}

func setCopyModeMarkFromIntent(root state.Root, intent input.Intent) state.Root {
	history, copyMode, viewID, ok := activeCopyHistorySession(root)
	if !ok || len(history.Rows) == 0 {
		return root
	}
	pos := copyMode.Cursor
	if intent.Event.Kind == input.EventKindMouse {
		pos = state.CopyPosition{Row: copyMode.ViewportTop, Col: 0}
	}
	copyMode = copyMode.SetMark(pos).RefreshLogicalSelection(history)
	root = root.WithCopyHistorySession(viewID, history, copyMode)
	return root
}

func reduceCopyModeCopySelection(root state.Root, deps CopyModeDeps) (state.Root, []Effect) {
	history, copyMode, viewID, ok := activeCopyHistorySession(root)
	if !ok || len(history.Rows) == 0 {
		return root, nil
	}
	if copyMode.Selection == nil {
		copyMode = copyMode.SetMark(copyMode.Cursor).RefreshLogicalSelection(history)
	}
	copyMode = copyMode.EnsureLogicalSelection(history)
	root = root.WithCopyHistorySession(viewID, history, copyMode)
	if copyMode.SelectionNeedsBackend(history) && deps.Core != nil {
		start, end, ok := copyMode.SelectionLogicalRange(history)
		if ok {
			// 中文说明：跨本地窗口裁剪的选择必须回到 core-v2 logical range 复制；
			// 本地 rows 只能处理当前已加载窗口内的简单复制。
			return root.Advance(), []Effect{copyModeBackendCopyEffect(deps.Core, deps.Clipboard, services.HistoryCopyRangeRequest{
				TerminalID: history.TerminalID,
				Cols:       firstPositiveInt(history.Cols, copyMode.BoundCols),
				Token:      history.Token,
				Generation: history.Generation,
				Boundary:   history.Boundary,
				Start:      start,
				End:        end,
			})}
		}
	}
	text := copyModeLocalCopyText(history, copyMode)
	return root.Advance(), []Effect{copyModeClipboardWriteEffect(deps.Clipboard, text)}
}

func reduceCopyModeCopyResult(root state.Root, msg CopyModeCopyResultMsg) (state.Root, []Effect) {
	if msg.Err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "copy", Body: msg.Err.Error()})
		return root.Advance(), nil
	}
	if strings.TrimSpace(msg.Text) == "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "copy", Body: "empty selection"})
		return root.Advance(), nil
	}
	root.Clipboard = root.Clipboard.WithCopiedText(msg.Text)
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "copy", Body: "copied"})
	return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
		return ClipboardStoragePersistRequestMsg{Reason: "copy"}
	}}}
}

func exitCopyModeWithRelease(root state.Root, deps CopyModeDeps) (state.Root, []Effect) {
	_, copyMode, viewID, ok := activeCopyHistorySession(root)
	if !ok {
		return root, nil
	}
	token := copyMode.BoundToken
	terminalID := copyMode.TerminalID
	root = root.WithoutCopyHistorySession(viewID)
	if deps.Core == nil || token == "" {
		return root, nil
	}
	return root, []Effect{FuncEffect{Async: true, Run: func(ctx context.Context) Msg {
		_ = deps.Core.ReleaseHistory(ctx, services.HistoryReleaseRequest{TerminalID: terminalID, Token: token})
		return NoopMsg{}
	}}}
}

func copyModeOwnsActiveInput(root state.Root) bool {
	_, copyMode, _, ok := activeCopyHistorySession(root)
	if !ok || (!copyMode.Active && !copyMode.Entering) {
		return false
	}
	binding, hasBinding := activeTerminalViewBinding(root)
	if !hasBinding {
		return false
	}
	if copyMode.ViewID != "" {
		return binding.ViewID == copyMode.ViewID
	}
	return copyMode.PaneID == "" || binding.PaneID == copyMode.PaneID
}

func activeCopyHistorySession(root state.Root) (state.HistoryStore, state.CopyModeStore, string, bool) {
	binding, ok := activeTerminalViewBinding(root)
	if ok {
		history, copyMode := root.CopyHistorySessionForView(binding.ViewID)
		if copyMode.Active || copyMode.Entering || history.Pending != nil {
			return history, copyMode, binding.ViewID, true
		}
	}
	if root.CopyMode.Active || root.CopyMode.Entering || root.History.Pending != nil {
		viewID := firstNonEmptyString(root.CopyMode.ViewID, root.History.ViewID)
		return root.History, root.CopyMode, viewID, true
	}
	return state.HistoryStore{}, state.CopyModeStore{}, "", false
}

func copyModeRequestSize(root state.Root, binding state.TerminalViewBinding) (int, int) {
	cols, rows := binding.DesiredCols, binding.DesiredRows
	if cols <= 0 {
		cols = root.Surface.SurfaceForTerminal(binding.TerminalID).Cols
	}
	if rows <= 0 {
		rows = root.Surface.SurfaceForTerminal(binding.TerminalID).Rows
	}
	if rect, ok := activeTerminalContentRect(root, render.Rect{W: cols, H: rows}); ok {
		cols = firstPositiveInt(rect.W, cols)
		rows = firstPositiveInt(rect.H, rows)
	}
	return firstPositiveInt(cols, 80), firstPositiveInt(rows, 24)
}

func copyModeScrollRows(copyMode state.CopyModeStore, event input.InputEvent) int {
	if event.Kind == input.EventKindKey && (event.Key == input.KeyPageUp || event.Key == input.KeyPageDn) {
		return copyModePageRows(copyMode)
	}
	return 1
}

func copyModePageRows(copyMode state.CopyModeStore) int {
	rows := copyMode.ViewRows - 1
	if rows <= 0 {
		rows = 1
	}
	return rows
}

func copyModeWindowRows(viewRows int) int {
	if viewRows <= 0 {
		viewRows = 24
	}
	if viewRows < 64 {
		return 64
	}
	return viewRows * 2
}

func ensureCopyCursorVisible(copyMode state.CopyModeStore, totalRows int) state.CopyModeStore {
	if totalRows <= 0 {
		copyMode.ViewportTop = 0
		copyMode.Cursor = state.CopyPosition{}
		return copyMode
	}
	visible := copyMode.ViewRows
	if visible <= 0 {
		visible = 1
	}
	if copyMode.Cursor.Row < copyMode.ViewportTop {
		copyMode.ViewportTop = copyMode.Cursor.Row
	}
	if copyMode.Cursor.Row >= copyMode.ViewportTop+visible {
		copyMode.ViewportTop = copyMode.Cursor.Row - visible + 1
	}
	maxTop := totalRows - visible
	if maxTop < 0 {
		maxTop = 0
	}
	copyMode.ViewportTop = clampAppInt(copyMode.ViewportTop, 0, maxTop)
	return copyMode
}

func clampCopyCursor(copyMode state.CopyModeStore, history state.HistoryStore) state.CopyModeStore {
	if len(history.Rows) == 0 {
		copyMode.Cursor = state.CopyPosition{}
		copyMode.ViewportTop = 0
		return copyMode
	}
	row := clampAppInt(copyMode.Cursor.Row, 0, len(history.Rows)-1)
	col := clampAppInt(copyMode.Cursor.Col, 0, state.HistoryRowDisplayWidth(history.Rows[row]))
	copyMode.Cursor = state.CopyPosition{Row: row, Col: col}
	return copyMode
}

func copyModeLocalCopyText(history state.HistoryStore, copyMode state.CopyModeStore) string {
	if len(history.Rows) == 0 {
		return ""
	}
	if copyMode.Selection == nil {
		row := clampAppInt(copyMode.Cursor.Row, 0, len(history.Rows)-1)
		return history.Rows[row].Text
	}
	start := copyMode.Selection.Anchor.Row
	end := copyMode.Selection.Focus.Row
	if start > end {
		start, end = end, start
	}
	start = clampAppInt(start, 0, len(history.Rows)-1)
	end = clampAppInt(end, 0, len(history.Rows)-1)
	var out strings.Builder
	for row := start; row <= end; row++ {
		if row > start {
			out.WriteByte('\n')
		}
		out.WriteString(history.Rows[row].Text)
	}
	return out.String()
}

func copyModeLatestEffect(core services.CoreClient, req services.HistoryLatestRequest) Effect {
	return FuncEffect{Async: true, ForceSyncInTests: true, Run: func(ctx context.Context) Msg {
		result, err := core.HistoryLatest(ctx, req)
		return CopyModeHistoryResultMsg{RequestID: req.RequestID, Result: result, Err: err}
	}}
}

func copyModeOlderEffect(core services.CoreClient, req services.HistoryOlderRequest) Effect {
	return FuncEffect{Async: true, ForceSyncInTests: true, Run: func(ctx context.Context) Msg {
		result, err := core.HistoryOlder(ctx, req)
		return CopyModeHistoryResultMsg{RequestID: req.RequestID, Result: result, Err: err}
	}}
}

func copyModeNewerEffect(core services.CoreClient, req services.HistoryNewerRequest) Effect {
	return FuncEffect{Async: true, ForceSyncInTests: true, Run: func(ctx context.Context) Msg {
		result, err := core.HistoryNewer(ctx, req)
		return CopyModeHistoryResultMsg{RequestID: req.RequestID, Result: result, Err: err}
	}}
}

func copyModeBackendCopyEffect(core services.CoreClient, clipboard services.ClipboardService, req services.HistoryCopyRangeRequest) Effect {
	return FuncEffect{Async: true, ForceSyncInTests: true, Run: func(ctx context.Context) Msg {
		result, err := core.HistoryCopyRange(ctx, req)
		if err != nil {
			return CopyModeCopyResultMsg{Err: err}
		}
		if clipboard != nil {
			err = clipboard.Write(ctx, services.ClipboardWriteRequest{Text: result.Text})
		}
		return CopyModeCopyResultMsg{Text: result.Text, Err: err}
	}}
}

func copyModeClipboardWriteEffect(clipboard services.ClipboardService, text string) Effect {
	return FuncEffect{Async: true, ForceSyncInTests: true, Run: func(ctx context.Context) Msg {
		if clipboard != nil {
			if err := clipboard.Write(ctx, services.ClipboardWriteRequest{Text: text}); err != nil {
				return CopyModeCopyResultMsg{Err: err}
			}
		}
		return CopyModeCopyResultMsg{Text: text}
	}}
}

func nextCopyModeRequestID(root state.Root) state.RequestID {
	next := state.RequestID(root.Generation + 1)
	history, _, _, ok := activeCopyHistorySession(root)
	if ok && history.Pending != nil && history.Pending.ID >= next {
		next = history.Pending.ID + 1
	} else if root.History.Pending != nil && root.History.Pending.ID >= next {
		next = root.History.Pending.ID + 1
	}
	return next
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func clampAppInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxAppInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
