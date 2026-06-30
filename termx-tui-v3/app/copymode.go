package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type CopyModeDeps struct {
	Core      services.CoreClient
	Clipboard services.ClipboardService
	Terminal  services.TerminalService
	// Logger 是 copy/history 诊断链路的日志出口；truth source 仍是 core 返回的 HistoryWindow，
	// reducer 不能从日志反推分页、合并或渲染状态。
	Logger *slog.Logger
	// Rows 只作为没有 panel content rect 时的 fallback；真实 copy history 请求量必须跟随当前 panel 尺寸。
	Rows int
}

const (
	copyModeHistoryRequestScreens  = 6
	copyModeHistoryPrefetchScreens = 2
	copyModeHistoryMinRequestRows  = 64
	copyModeHistoryMaxRequestRows  = 512
	copyModeHistoryWindowScreens   = 8
)

type CopyModeHistoryResultMsg struct {
	Result     services.HistoryResult
	Err        error
	PaneID     string
	ViewID     string
	TerminalID string
}

func (CopyModeHistoryResultMsg) isMsg() {}

type CopyModeEnterViewMsg struct {
	Binding            state.TerminalViewBinding
	Cols               int
	Rows               int
	InitialScrollDelta int
}

func (CopyModeEnterViewMsg) isMsg() {}

type CopyModeMoveCursorMsg struct {
	Position state.CopyPosition
}

func (CopyModeMoveCursorMsg) isMsg() {}

type CopyModeSetMarkMsg struct {
	Position state.CopyPosition
}

func (CopyModeSetMarkMsg) isMsg() {}

type CopyModeCopySelectionMsg struct{}

func (CopyModeCopySelectionMsg) isMsg() {}

type CopyModeCopyResultMsg struct {
	Text   string
	Err    error
	Commit bool
}

func (CopyModeCopyResultMsg) isMsg() {}

type CopyModePasteResultMsg struct {
	Text string
	Err  error
}

func (CopyModePasteResultMsg) isMsg() {}

type CopyModePasteTextMsg struct {
	Text string
}

func (CopyModePasteTextMsg) isMsg() {}

type CopyModeSetQueryMsg struct {
	Query string
}

func (CopyModeSetQueryMsg) isMsg() {}

type CopyModeMoveMatchMsg struct {
	Delta int
}

func (CopyModeMoveMatchMsg) isMsg() {}

type CopyModeScrollMsg struct {
	Delta  int
	ViewID string
}

func (CopyModeScrollMsg) isMsg() {}

type CopyModeMouseSelectMsg struct {
	Position state.CopyPosition
	PaneID   string
	ViewID   string
}

func (CopyModeMouseSelectMsg) isMsg() {}

type CopyModeWheelMsg struct {
	Event  input.InputEvent
	ViewID string
}

func (CopyModeWheelMsg) isMsg() {}

func staleCopyModeHistoryResult(root state.Root, msg CopyModeHistoryResultMsg) bool {
	return root.History.Pending == nil || state.RequestID(msg.Result.RequestID) != root.History.Pending.ID
}

func rootWithActiveCopyHistorySession(root state.Root) (state.Root, string) {
	viewID := ""
	if binding, ok := activeTerminalViewBinding(root); ok {
		viewID = binding.ViewID
	}
	if viewID == "" {
		viewID = copyHistoryWorkingViewID(root)
	}
	if workingViewID := copyHistoryWorkingViewID(root); workingViewID != "" {
		if viewID == "" || workingViewID == viewID || root.CopyModeByView[viewID].ViewID == "" && root.HistoryByView[viewID].ViewID == "" {
			return root, workingViewID
		}
	}
	if copyModeInputContext(root.CopyMode) && root.CopyMode.ViewID == "" && root.CopyMode.PaneID == "" {
		return root, viewID
	}
	return rootWithCopyHistorySessionForView(root, viewID), viewID
}

func rootWithCopyHistorySessionForResult(root state.Root, msg CopyModeHistoryResultMsg) (state.Root, string) {
	viewID := copyHistoryResultViewID(root, msg)
	if viewID == "" {
		return root, copyHistoryWorkingViewID(root)
	}
	return rootWithCopyHistorySessionForView(root, viewID), viewID
}

func rootWithCopyHistorySessionForView(root state.Root, viewID string) state.Root {
	if viewID == "" {
		return root
	}
	history, copyMode := root.CopyHistorySessionForView(viewID)
	root.History = history
	root.CopyMode = copyMode
	return root
}

func saveCopyHistorySessionForView(root state.Root, viewID string) state.Root {
	if viewID == "" {
		viewID = copyHistoryWorkingViewID(root)
	}
	if viewID == "" {
		return root
	}
	// 中文说明：reducer 仍复用单会话算法，提交时按 TerminalView 写回，
	// 避免 floating/pane 的 history 交互态互相覆盖。
	return root.WithCopyHistorySession(viewID, root.History, root.CopyMode)
}

func copyHistoryWorkingViewID(root state.Root) string {
	if root.CopyMode.ViewID != "" {
		return root.CopyMode.ViewID
	}
	if root.History.ViewID != "" {
		return root.History.ViewID
	}
	if root.CopyMode.PaneID != "" {
		return state.TerminalPaneViewID(root.CopyMode.PaneID)
	}
	if root.History.PaneID != "" {
		return state.TerminalPaneViewID(root.History.PaneID)
	}
	return ""
}

func copyHistoryResultViewID(root state.Root, msg CopyModeHistoryResultMsg) string {
	requestID := state.RequestID(msg.Result.RequestID)
	if requestID != 0 {
		if viewID := copyHistoryPendingViewID(root.History, root.CopyMode, requestID); viewID != "" {
			return viewID
		}
		for viewID, history := range root.HistoryByView {
			copyMode := root.CopyModeByView[viewID]
			if pendingViewID := copyHistoryPendingViewID(history, copyMode, requestID); pendingViewID != "" {
				return pendingViewID
			}
		}
		return ""
	}
	if msg.ViewID != "" {
		return msg.ViewID
	}
	if msg.Result.Window.ViewID != "" {
		return msg.Result.Window.ViewID
	}
	if msg.PaneID != "" {
		return state.TerminalPaneViewID(msg.PaneID)
	}
	return ""
}

func copyHistoryPendingViewID(history state.HistoryStore, copyMode state.CopyModeStore, requestID state.RequestID) string {
	if history.Pending == nil || history.Pending.ID != requestID {
		return ""
	}
	if history.Pending.ViewID != "" {
		return history.Pending.ViewID
	}
	if copyMode.ViewID != "" {
		return copyMode.ViewID
	}
	if history.ViewID != "" {
		return history.ViewID
	}
	if history.Pending.PaneID != "" {
		return state.TerminalPaneViewID(history.Pending.PaneID)
	}
	return ""
}

func NewCopyModeReducer(deps CopyModeDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case InputMsg:
			if msg.TerminalMousePassthrough {
				return root, nil
			}
			root, activeViewID := rootWithActiveCopyHistorySession(root)
			copyOwnsInput := copyModeOwnsActiveInput(root)
			intent := input.Route(msg.Event, copyOwnsInput)
			if !copyOwnsInput && !copyModeEnterIntent(intent) {
				return root, nil
			}
			next, effects := reduceCopyModeIntent(root, intent, deps)
			if copyModeEnterIntent(intent) {
				activeViewID = copyHistoryWorkingViewID(next)
			}
			return saveCopyHistorySessionForView(next, activeViewID), effects
		case CopyModeEnterViewMsg:
			next, effects := beginCopyModeLatestForView(root, deps, msg.Binding, msg.Cols, msg.Rows)
			next = applyCopyModeEnteringScrollDelta(next, msg.InitialScrollDelta)
			next = saveCopyHistorySessionForView(next, msg.Binding.ViewID)
			return next, append([]Effect{handledEffect{}}, effects...)
		case CopyModeHistoryResultMsg:
			root, viewID := rootWithCopyHistorySessionForResult(root, msg)
			next, effects := reduceCopyModeHistoryResult(root, msg, deps)
			return saveCopyHistorySessionForView(next, viewID), effects
		case CopyModeMoveCursorMsg:
			root, activeViewID := rootWithActiveCopyHistorySession(root)
			root.CopyMode = root.CopyMode.MoveCursor(msg.Position).RefreshLogicalSelectionFocus(root.History)
			root = root.Advance()
			return saveCopyHistorySessionForView(root, activeViewID), nil
		case CopyModeSetMarkMsg:
			root, activeViewID := rootWithActiveCopyHistorySession(root)
			root.CopyMode = root.CopyMode.SetMark(msg.Position).RefreshLogicalSelection(root.History)
			root = root.Advance()
			return saveCopyHistorySessionForView(root, activeViewID), nil
		case CopyModeCopySelectionMsg:
			root, activeViewID := rootWithActiveCopyHistorySession(root)
			next, effects := reduceCopyModeCopySelection(root, deps)
			return saveCopyHistorySessionForView(next, activeViewID), effects
		case CopyModeCopyResultMsg:
			if msg.Err != nil {
				if errors.Is(msg.Err, services.ErrStaleHistoryWindow) {
					root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "History window expired", Body: "retry copy mode", DismissAfterTicks: 3})
					return root.Advance(), nil
				}
				root.Surface = root.Surface.SetError(msg.Err.Error())
				root.Session = root.Session.SetError(msg.Err.Error())
				return root.Advance(), nil
			}
			if !msg.Commit || msg.Text == "" {
				return root, nil
			}
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastSuccess, Title: "Copied to clipboard", DismissAfterTicks: 3})
			root.Clipboard = root.Clipboard.WithCopiedText(msg.Text)
			root = root.Advance()
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg { return ClipboardStoragePersistRequestMsg{Reason: "copy"} }}}
		case CopyModePasteResultMsg:
			if msg.Err != nil {
				root.Surface = root.Surface.SetError(msg.Err.Error())
				root.Session = root.Session.SetError(msg.Err.Error())
				return root.Advance(), nil
			}
			return root, nil
		case CopyModePasteTextMsg:
			root, activeViewID := rootWithActiveCopyHistorySession(root)
			next, effects := reduceCopyModePasteText(root, deps, msg.Text)
			return saveCopyHistorySessionForView(next, activeViewID), effects
		case CopyModeSetQueryMsg:
			root, activeViewID := rootWithActiveCopyHistorySession(root)
			root.CopyMode = root.CopyMode.SetQuery(msg.Query, state.FindCopyMatches(root.History, msg.Query))
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			root = root.Advance()
			return saveCopyHistorySessionForView(root, activeViewID), nil
		case CopyModeMoveMatchMsg:
			root, activeViewID := rootWithActiveCopyHistorySession(root)
			root.CopyMode = root.CopyMode.MoveMatch(msg.Delta)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			root = root.Advance()
			return saveCopyHistorySessionForView(root, activeViewID), nil
		case CopyModeScrollMsg:
			activeViewID := msg.ViewID
			if activeViewID != "" {
				root = rootWithCopyHistorySessionForView(root, activeViewID)
			} else {
				root, activeViewID = rootWithActiveCopyHistorySession(root)
			}
			root.CopyMode = root.CopyMode.ScrollCursor(msg.Delta, len(root.History.Rows))
			root = refreshCopyModeLogicalSelectionFocus(root).Advance()
			return saveCopyHistorySessionForView(root, activeViewID), nil
		case CopyModeMouseSelectMsg:
			root = rootWithCopyHistorySessionForView(root, msg.ViewID)
			if !copyModeMouseSelectTargetMatches(root, msg.PaneID) {
				return root, nil
			}
			root.CopyMode = root.CopyMode.MoveCursor(msg.Position)
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			root.CopyMode = root.CopyMode.SetMark(root.CopyMode.Cursor)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			root.CopyMode = root.CopyMode.RefreshLogicalSelection(root.History)
			root = root.Advance()
			return saveCopyHistorySessionForView(root, msg.ViewID), nil
		case CopyModeWheelMsg:
			root = rootWithCopyHistorySessionForView(root, msg.ViewID)
			if !copyModeInputContext(root.CopyMode) {
				return root, nil
			}
			intent := input.Route(msg.Event, true)
			next, effects := reduceCopyModeIntent(root, intent, deps)
			return saveCopyHistorySessionForView(next, msg.ViewID), effects
		default:
			return root, nil
		}
	}
}

func copyModeMouseSelectTargetMatches(root state.Root, paneID string) bool {
	if paneID == "" {
		return true
	}
	copyMode := root.CopyMode
	if copyMode.PaneID == paneID || copyMode.ViewID == root.TerminalViews.FloatingViewID(paneID) {
		return true
	}
	for _, binding := range root.TerminalViews.Bindings() {
		if binding.PaneID == paneID && binding.ViewID == copyMode.ViewID {
			return true
		}
	}
	shell := root.Shell.ReadonlyDefaults()
	return copyMode.PaneID == "" && copyMode.ViewID == "" && paneID == shell.ActivePaneID && shell.ActiveFloatingID() == ""
}

func refreshCopyModeLogicalSelectionFocus(root state.Root) state.Root {
	root.CopyMode = root.CopyMode.RefreshLogicalSelectionFocus(root.History)
	return root
}

func copyModeInputContext(copyMode state.CopyModeStore) bool {
	return copyMode.Active || copyMode.Entering
}

func copyModeOwnsActiveInput(root state.Root) bool {
	if !copyModeInputContext(root.CopyMode) {
		return false
	}
	if root.CopyMode.ViewID == "" && root.CopyMode.PaneID == "" {
		return true
	}
	binding, ok := activeTerminalViewBinding(root)
	if !ok {
		return false
	}
	if root.CopyMode.ViewID != "" {
		return binding.ViewID == root.CopyMode.ViewID
	}
	if root.CopyMode.PaneID != "" {
		return binding.PaneID == root.CopyMode.PaneID
	}
	return true
}

func reduceCopyModeIntent(root state.Root, intent input.Intent, deps CopyModeDeps) (state.Root, []Effect) {
	if next, effects, handled := reduceCopyModeEnteringIntent(root, intent); handled {
		return next, effects
	}
	switch intent.Kind {
	case input.IntentEnterCopyMode:
		next, effects := beginCopyModeLatest(root, deps)
		if delta, ok := copyModeEnteringScrollDelta(next.CopyMode, intent); ok {
			next = applyCopyModeEnteringScrollDelta(next, delta)
		}
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.IntentRequestOlder:
		next, effects := reduceCopyModeScrollOlder(root, deps, intent.Event)
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.IntentRequestNewer:
		rows := copyModeNewerScrollRows(root.CopyMode, intent.Event)
		next, effects := reduceCopyModeScrollNewer(root, deps, rows)
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.IntentExitCopyMode:
		next, effects := exitCopyModeWithRelease(root, deps)
		return next.Advance(), append([]Effect{handledEffect{}}, effects...)
	case input.IntentOpenClipboardHistory:
		root.Shell = root.Shell.OpenClipboardHistory()
		return root.Advance(), []Effect{
			handledEffect{},
			FuncEffect{Run: func(context.Context) Msg { return ClipboardStorageLoadRequestMsg{Reason: "open"} }},
		}
	case input.IntentShellAction:
		if intent.Action == input.ShellActionOpenClipboardHistory {
			root.Shell = root.Shell.OpenClipboardHistory()
			return root.Advance(), []Effect{
				handledEffect{},
				FuncEffect{Run: func(context.Context) Msg { return ClipboardStorageLoadRequestMsg{Reason: "open"} }},
			}
		}
		return root, nil
	case input.IntentPasteLastCopy:
		next, effects := reduceCopyModePaste(root, deps, false)
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.IntentPasteClipboard:
		next, effects := reduceCopyModePaste(root, deps, true)
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.IntentMouseSelect:
		if !root.CopyMode.Active {
			return root, []Effect{handledEffect{}}
		}
		root.CopyMode = root.CopyMode.SetMark(root.CopyMode.Cursor)
		root.CopyMode = root.CopyMode.RefreshLogicalSelection(root.History)
		return root.Advance(), []Effect{handledEffect{}}
	default:
		if root.CopyMode.Active {
			next, mouseEffects, handled := reduceCopyModeMouseInput(root, intent.Event, deps)
			if handled {
				return next, append([]Effect{handledEffect{}}, mouseEffects...)
			}
			next, keyEffects, handled := reduceCopyModeKeyInput(root, intent.Event, deps)
			if handled {
				return next, append([]Effect{handledEffect{}}, keyEffects...)
			}
		}
		if root.CopyMode.Entering {
			return root, []Effect{handledEffect{}}
		}
		return root, nil
	}
}

func copyModeEnterIntent(intent input.Intent) bool {
	return intent.Kind == input.IntentEnterCopyMode
}

func reduceCopyModeEnteringIntent(root state.Root, intent input.Intent) (state.Root, []Effect, bool) {
	if !root.CopyMode.Entering {
		return root, nil, false
	}
	if intent.Kind == input.IntentExitCopyMode {
		next, effects := exitCopyModeWithRelease(root, CopyModeDeps{})
		return next.Advance(), append([]Effect{handledEffect{}}, effects...), true
	}
	if intent.Kind == input.IntentOpenClipboardHistory {
		root.Shell = root.Shell.OpenClipboardHistory()
		return root.Advance(), []Effect{
			handledEffect{},
			FuncEffect{Run: func(context.Context) Msg { return ClipboardStorageLoadRequestMsg{Reason: "open"} }},
		}, true
	}
	if delta, ok := copyModeEnteringScrollDelta(root.CopyMode, intent); ok {
		root.CopyMode.EnteringScrollDelta += delta
		return root.Advance(), []Effect{handledEffect{}}, true
	}
	// 中文说明：latest 还没回来时 copy/history 尚未真正激活；这段时间只拦截输入，
	// 防止 p/P 或普通按键落到 terminal，也不提前打开依赖 frozen history 的功能。
	return root, []Effect{handledEffect{}}, true
}

func copyModeEnteringScrollDelta(copyMode state.CopyModeStore, intent input.Intent) (int, bool) {
	switch intent.Kind {
	case input.IntentEnterCopyMode:
		if intent.Event.Kind == input.EventKindMouse && intent.Event.Mouse == input.MouseWheelUp {
			return -copyModeLineScrollRows(), true
		}
		return 0, false
	case input.IntentRequestOlder:
		rows := copyModeOlderScrollRows(copyMode, intent.Event)
		if rows <= 0 {
			rows = 1
		}
		return -rows, true
	case input.IntentRequestNewer:
		rows := copyModeNewerScrollRows(copyMode, intent.Event)
		if rows <= 0 {
			rows = 1
		}
		return rows, true
	default:
		if intent.Event.Kind == input.EventKindMouse && intent.Event.Mouse == input.MouseWheelDown {
			return copyModeLineScrollRows(), true
		}
		if intent.Event.Kind == input.EventKindKey && intent.Event.Key == input.KeyPageDn {
			return copyModePageRows(copyMode), true
		}
		return 0, false
	}
}

func applyCopyModeEnteringScrollDelta(root state.Root, delta int) state.Root {
	if delta == 0 || !root.CopyMode.Entering {
		return root
	}
	root.CopyMode.EnteringScrollDelta += delta
	return root.Advance()
}

func reduceCopyModeMouseInput(root state.Root, event input.InputEvent, deps CopyModeDeps) (state.Root, []Effect, bool) {
	if event.Kind != input.EventKindMouse {
		return root, nil, false
	}
	switch event.Mouse {
	case input.MouseWheelUp:
		root.CopyMode = root.CopyMode.ScrollCursor(-copyModeLineScrollRows(), len(root.History.Rows))
		return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
	case input.MouseWheelDown:
		next, effects := reduceCopyModeScrollNewer(root, deps, copyModeLineScrollRows())
		return next, effects, true
	default:
		return root, nil, false
	}
}

func reduceCopyModeScrollNewer(root state.Root, deps CopyModeDeps, rows int) (state.Root, []Effect) {
	if rows <= 0 {
		rows = 1
	}
	if !root.CopyMode.Active {
		return root, nil
	}
	previousCursorRow := root.CopyMode.Cursor.Row
	previousCopyMode := root.CopyMode
	root.CopyMode = root.CopyMode.ScrollCursor(rows, len(root.History.Rows))
	consumedRows := root.CopyMode.Cursor.Row - previousCursorRow
	if consumedRows < 0 {
		consumedRows = 0
	}
	unconsumedRows := rows - consumedRows
	if unconsumedRows < 0 {
		unconsumedRows = 0
	}
	if unconsumedRows == 0 || root.History.NewerRequestState() != state.NewerRequestReady {
		if root.CopyMode.Cursor != previousCopyMode.Cursor || root.CopyMode.ViewportTop != previousCopyMode.ViewportTop {
			return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil
		}
		return root, nil
	}
	next, effects := beginCopyModeNewer(root, deps, unconsumedRows)
	if len(effects) > 0 {
		return next, effects
	}
	if root.CopyMode.Cursor != previousCopyMode.Cursor || root.CopyMode.ViewportTop != previousCopyMode.ViewportTop {
		return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil
	}
	return root, nil
}

func reduceCopyModeScrollOlder(root state.Root, deps CopyModeDeps, event input.InputEvent) (state.Root, []Effect) {
	rows := copyModeOlderScrollRows(root.CopyMode, event)
	if rows <= 0 {
		rows = 1
	}
	if root.CopyMode.Active {
		if root.CopyMode.Cursor.Row > 0 {
			previousCursorRow := root.CopyMode.Cursor.Row
			previousCopyMode := root.CopyMode
			root.CopyMode = root.CopyMode.ScrollCursor(-rows, len(root.History.Rows))
			consumedRows := previousCursorRow - root.CopyMode.Cursor.Row
			unconsumedRows := rows - consumedRows
			if unconsumedRows < 0 {
				unconsumedRows = 0
			}
			next, effects := maybePrefetchCopyModeOlder(root, deps, unconsumedRows)
			if len(effects) > 0 {
				return next, effects
			}
			if unconsumedRows > 0 && root.History.Pending != nil && root.History.Pending.Kind == state.HistoryRequestOlder {
				// 本地已加载区域先消费滚动；跨过顶部但 older 仍在飞时，只把没消费的行数挂到 pending。
				root.History.Pending.ScrollDeltaAfterPrepend += unconsumedRows
			}
			if root.CopyMode.Cursor != previousCopyMode.Cursor || root.CopyMode.ViewportTop != previousCopyMode.ViewportTop {
				return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil
			}
		}
		if root.History.Pending != nil && root.History.Pending.Kind == state.HistoryRequestOlder {
			next := root
			next.History.Pending.ScrollDeltaAfterPrepend += rows
			return refreshCopyModeLogicalSelectionFocus(next).Advance(), nil
		}
	}
	return beginCopyModeOlder(root, deps, rows)
}

func reduceCopyModeKeyInput(root state.Root, event input.InputEvent, deps CopyModeDeps) (state.Root, []Effect, bool) {
	if event.Kind != input.EventKindKey {
		return root, nil, false
	}
	switch event.Key {
	case input.KeyPageDn:
		next, effects := reduceCopyModeScrollNewer(root, deps, copyModePageRows(root.CopyMode))
		return next, effects, true
	case input.KeyHome:
		root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row, Col: 0})
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
	case input.KeyEnd:
		root.CopyMode = root.CopyMode.MoveCursor(copyModeLineEndPosition(root.History, root.CopyMode.Cursor.Row))
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
	case input.KeyLeft:
		root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row, Col: root.CopyMode.Cursor.Col - 1})
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
	case input.KeyRight:
		root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row, Col: root.CopyMode.Cursor.Col + 1})
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
	case input.KeyDown:
		if root.CopyMode.Query != "" {
			root.CopyMode = root.CopyMode.MoveMatch(1)
		} else {
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row + 1, Col: root.CopyMode.Cursor.Col})
		}
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
		return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
	case input.KeyUp:
		if root.CopyMode.Query != "" {
			root.CopyMode = root.CopyMode.MoveMatch(-1)
		} else {
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row - 1, Col: root.CopyMode.Cursor.Col})
		}
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
		return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
	case input.KeyEnter:
		if root.CopyMode.Query != "" {
			root.CopyMode = root.CopyMode.MoveMatch(1)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return root.Advance(), nil, true
		}
		if root.CopyMode.Selection != nil {
			next, effects := reduceCopyModeCopySelection(root, deps)
			next, releaseEffects := exitCopyModeWithRelease(next, deps)
			effects = append(effects, releaseEffects...)
			return next, effects, true
		}
	case input.KeyChar:
		if isBackspaceEvent(event) {
			query := trimLastRune(root.CopyMode.Query)
			root.CopyMode = root.CopyMode.SetQuery(query, state.FindCopyMatches(root.History, query))
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return root.Advance(), nil, true
		}
		if event.Ctrl || event.Char == "" {
			return root, nil, false
		}
		switch event.Char {
		case "h":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row, Col: root.CopyMode.Cursor.Col - 1})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
		case "l":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row, Col: root.CopyMode.Cursor.Col + 1})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
		case "j":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row + 1, Col: root.CopyMode.Cursor.Col})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
		case "k":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row - 1, Col: root.CopyMode.Cursor.Col})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
		case "g":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: 0, Col: root.CopyMode.Cursor.Col})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			// `g` 在 copy mode 里表达“去最老处”。这里直接请求 frozen snapshot
			// 的 oldest page，不能靠重复 older 把中间所有页都拉进 TUI。
			if root.CopyMode.ViewportTop == 0 && root.History.OlderRequestState() == state.OlderRequestReady {
				next, effects := beginCopyModeOldest(root, deps)
				return next, effects, true
			}
			return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
		case "G":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: len(root.History.Rows) - 1, Col: root.CopyMode.Cursor.Col})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			if root.History.NewerRequestState() == state.NewerRequestReady {
				next, effects := beginCopyModeNewer(root, deps, 0)
				return next, effects, true
			}
			return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
		case "u":
			root.CopyMode = root.CopyMode.ScrollCursor(-(copyModePageRows(root.CopyMode) / 2), len(root.History.Rows))
			return refreshCopyModeLogicalSelectionFocus(root).Advance(), nil, true
		case "d":
			next, effects := reduceCopyModeScrollNewer(root, deps, copyModePageRows(root.CopyMode)/2)
			return next, effects, true
		case " ":
			root.CopyMode = root.CopyMode.SetMark(root.CopyMode.Cursor)
			root.CopyMode = root.CopyMode.RefreshLogicalSelection(root.History)
			return root.Advance(), nil, true
		case "y":
			if root.CopyMode.Selection != nil {
				next, effects := reduceCopyModeCopySelection(root, deps)
				return next, effects, true
			}
			return root, nil, true
		case "p":
			next, effects := reduceCopyModePaste(root, deps, false)
			return next, effects, true
		case "P":
			next, effects := reduceCopyModePaste(root, deps, true)
			return next, effects, true
		}
		if event.Char == "/" && root.CopyMode.Query == "" {
			root.CopyMode = root.CopyMode.SetQuery("", nil)
			return root.Advance(), nil, true
		}
		query := root.CopyMode.Query + event.Char
		root.CopyMode = root.CopyMode.SetQuery(query, state.FindCopyMatches(root.History, query))
		root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
		return root.Advance(), nil, true
	}
	return root, nil, false
}

func copyModeLineEndPosition(history state.HistoryStore, row int) state.CopyPosition {
	if len(history.Rows) == 0 {
		return state.CopyPosition{}
	}
	row = clampColumn(row, 0, len(history.Rows)-1)
	return state.CopyPosition{Row: row, Col: state.HistoryRowDisplayWidth(history.Rows[row])}
}

func beginCopyModeLatest(root state.Root, deps CopyModeDeps) (state.Root, []Effect) {
	if deps.Core == nil {
		return setCopyModeError(root, "core client missing"), nil
	}
	binding, hasBinding := activeTerminalViewBinding(root)
	rect, ok := copyModeContentRect(root)
	if !hasBinding || binding.TerminalID == "" || !ok || rect.W <= 0 {
		return setCopyModeError(root, "copy mode requires attached terminal and cols"), nil
	}
	return beginCopyModeLatestForView(root, deps, binding, rect.W, rect.H)
}

func beginCopyModeLatestForView(root state.Root, deps CopyModeDeps, binding state.TerminalViewBinding, cols int, rowsHint int) (state.Root, []Effect) {
	root = rootWithCopyHistorySessionForView(root, binding.ViewID)
	if deps.Core == nil {
		return setCopyModeError(root, "core client missing"), nil
	}
	if binding.TerminalID == "" || cols <= 0 {
		return setCopyModeError(root, "copy mode requires attached terminal and cols"), nil
	}
	requestID := nextHistoryRequestID(root)
	nextHistory, err := root.History.BeginLatest(state.HistoryPendingRequest{
		ID:         requestID,
		PaneID:     binding.PaneID,
		ViewID:     binding.ViewID,
		TerminalID: binding.TerminalID,
		Cols:       cols,
	})
	if err != nil {
		// 连续 latest/rebind 期间如果上一个请求还在飞，只保留 pending，不把内部背压抬成用户可见错误。
		if errors.Is(err, state.ErrHistoryRequestPending) {
			return root, nil
		}
		return setCopyModeError(root, err.Error()), nil
	}
	root.History = nextHistory
	enteringLive := state.CloneLiveSurfaceSnapshot(root.Surface.SurfaceForTerminal(binding.TerminalID).Snapshot())
	root.CopyMode = root.CopyMode.BindLatest(binding.PaneID, binding.ViewID, binding.TerminalID, requestID, cols, rowsHint, enteringLive)
	rows := requestRows(rowsHint, deps.Rows)
	logHistoryTrace(deps.Logger, "tui.request.latest",
		"request_id", uint64(requestID),
		"pane_id", binding.PaneID,
		"view_id", binding.ViewID,
		"terminal_id", binding.TerminalID,
		"cols", cols,
		"limit", rows,
		"rows_hint", rowsHint,
		"live_rows", len(enteringLive.Screen),
	)
	root = root.Advance()
	root = saveCopyHistorySessionForView(root, binding.ViewID)
	return root, []Effect{FuncEffect{
		// history.window 真实走 protocol/client 时可能明显慢于一帧；
		// 这里必须异步请求，不能把 copy mode 入口卡在 runtime 主循环里。
		Async:            true,
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			result, err := deps.Core.HistoryLatest(ctx, services.HistoryLatestRequest{
				RequestID:  services.RequestID(requestID),
				PaneID:     binding.PaneID,
				ViewID:     binding.ViewID,
				TerminalID: binding.TerminalID,
				Cols:       cols,
				Rows:       rows,
				// 中文说明：EnteringLive 只冻结等待态显示；history.window 的 frozen
				// logical-line 边界由 core 请求时建立，不能用可能滞后的 live revision 截断。
			})
			result.Window.PaneID = binding.PaneID
			result.Window.ViewID = binding.ViewID
			return CopyModeHistoryResultMsg{Result: result, Err: err, PaneID: binding.PaneID, ViewID: binding.ViewID, TerminalID: binding.TerminalID}
		},
	}}
}

func beginCopyModeOlder(root state.Root, deps CopyModeDeps, scrollDeltaAfterPrepend int) (state.Root, []Effect) {
	if deps.Core == nil {
		return setCopyModeError(root, "core client missing"), nil
	}
	switch root.History.OlderRequestState() {
	case state.OlderRequestPending, state.OlderRequestExhausted, state.OlderRequestMissing:
		return root, nil
	}
	requestID := nextHistoryRequestID(root)
	req := state.HistoryPendingRequest{
		ID:                      requestID,
		PaneID:                  root.History.PaneID,
		ViewID:                  root.History.ViewID,
		TerminalID:              root.History.TerminalID,
		Cols:                    root.History.Cols,
		Token:                   root.History.Token,
		Generation:              root.History.Generation,
		Cursor:                  root.History.Cursor,
		Boundary:                root.History.Boundary,
		ScrollDeltaAfterPrepend: scrollDeltaAfterPrepend,
	}
	nextHistory, err := root.History.BeginOlder(req)
	if err != nil {
		// older 重复触发时直接忽略，保持当前 pending/history 视图，不弹假错误。
		if errors.Is(err, state.ErrHistoryRequestPending) {
			return root, nil
		}
		return setCopyModeError(root, err.Error()), nil
	}
	root.History = nextHistory
	rows := copyModeHistoryRequestRows(root, deps)
	attrs := []any{
		"request_id", uint64(requestID),
		"pane_id", req.PaneID,
		"view_id", req.ViewID,
		"terminal_id", req.TerminalID,
		"cols", req.Cols,
		"limit", rows,
		"token", req.Token,
		"generation", req.Generation,
		"scroll_delta_after_prepend", scrollDeltaAfterPrepend,
		"local_rows", len(root.History.Rows),
		"local_summary", state.HistoryTraceWindowSummary(root.History.Rows),
	}
	attrs = append(attrs, historyTraceCursorAttrs("request", req.Cursor)...)
	attrs = append(attrs, historyTraceBoundaryAttrs("request", req.Boundary)...)
	logHistoryTrace(deps.Logger, "tui.request.older", attrs...)
	return root.Advance(), []Effect{FuncEffect{
		// older 分页也必须异步，否则连续 PageUp / wheel up 会把整个 UI 主循环卡住。
		Async:            true,
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			result, err := deps.Core.HistoryOlder(ctx, services.HistoryOlderRequest{
				RequestID:  services.RequestID(requestID),
				PaneID:     req.PaneID,
				ViewID:     req.ViewID,
				TerminalID: req.TerminalID,
				Cols:       req.Cols,
				Rows:       rows,
				Token:      req.Token,
				Generation: req.Generation,
				Cursor:     req.Cursor,
				Boundary:   req.Boundary,
			})
			result.Window.PaneID = req.PaneID
			result.Window.ViewID = req.ViewID
			return CopyModeHistoryResultMsg{Result: result, Err: err, PaneID: req.PaneID, ViewID: req.ViewID, TerminalID: req.TerminalID}
		},
	}}
}

func beginCopyModeNewer(root state.Root, deps CopyModeDeps, scrollDeltaAfterAppend int) (state.Root, []Effect) {
	if deps.Core == nil {
		return setCopyModeError(root, "core client missing"), nil
	}
	if root.History.NewerRequestState() != state.NewerRequestReady {
		return root, nil
	}
	tail := root.History.Rows[len(root.History.Rows)-1]
	requestID := nextHistoryRequestID(root)
	req := state.HistoryPendingRequest{
		ID:         requestID,
		PaneID:     root.History.PaneID,
		ViewID:     root.History.ViewID,
		TerminalID: root.History.TerminalID,
		Cols:       root.History.Cols,
		Token:      root.History.Token,
		Generation: root.History.Generation,
		Cursor: state.HistoryCursor{
			Valid:           tail.LineID != 0,
			BeforeLineID:    tail.LineID,
			BeforeRowInLine: tail.RowInLine,
			Segment:         tail.Segment,
		},
		Boundary:                root.History.Boundary,
		ScrollDeltaAfterPrepend: scrollDeltaAfterAppend,
	}
	nextHistory, err := root.History.BeginNewer(req)
	if err != nil {
		if errors.Is(err, state.ErrHistoryRequestPending) {
			return root, nil
		}
		return setCopyModeError(root, err.Error()), nil
	}
	root.History = nextHistory
	rows := copyModeHistoryRequestRows(root, deps)
	newerAttrs := []any{
		"request_id", uint64(requestID),
		"pane_id", req.PaneID,
		"view_id", req.ViewID,
		"terminal_id", req.TerminalID,
		"cols", req.Cols,
		"limit", rows,
		"token", req.Token,
		"generation", req.Generation,
		"scroll_delta_after_append", scrollDeltaAfterAppend,
		"local_rows", len(root.History.Rows),
		"local_summary", state.HistoryTraceWindowSummary(root.History.Rows),
	}
	newerAttrs = append(newerAttrs, historyTraceCursorAttrs("request", req.Cursor)...)
	newerAttrs = append(newerAttrs, historyTraceBoundaryAttrs("request", req.Boundary)...)
	logHistoryTrace(deps.Logger, "tui.request.newer", newerAttrs...)
	return root.Advance(), []Effect{FuncEffect{
		// newer 只在本地窗口已回收较新尾部时触发，仍然按事件循环异步拉取。
		Async:            true,
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			result, err := deps.Core.HistoryNewer(ctx, services.HistoryNewerRequest{
				RequestID:  services.RequestID(requestID),
				PaneID:     req.PaneID,
				ViewID:     req.ViewID,
				TerminalID: req.TerminalID,
				Cols:       req.Cols,
				Rows:       rows,
				Token:      req.Token,
				Generation: req.Generation,
				Cursor:     req.Cursor,
				Boundary:   req.Boundary,
			})
			result.Window.PaneID = req.PaneID
			result.Window.ViewID = req.ViewID
			return CopyModeHistoryResultMsg{Result: result, Err: err, PaneID: req.PaneID, ViewID: req.ViewID, TerminalID: req.TerminalID}
		},
	}}
}

func beginCopyModeOldest(root state.Root, deps CopyModeDeps) (state.Root, []Effect) {
	if deps.Core == nil {
		return setCopyModeError(root, "core client missing"), nil
	}
	switch root.History.OlderRequestState() {
	case state.OlderRequestPending, state.OlderRequestExhausted, state.OlderRequestMissing:
		return root, nil
	}
	requestID := nextHistoryRequestID(root)
	req := state.HistoryPendingRequest{
		ID:         requestID,
		Kind:       state.HistoryRequestOldest,
		PaneID:     root.History.PaneID,
		ViewID:     root.History.ViewID,
		TerminalID: root.History.TerminalID,
		Cols:       root.History.Cols,
		Token:      root.History.Token,
		Generation: root.History.Generation,
		Boundary:   root.History.Boundary,
	}
	nextHistory, err := root.History.BeginOldest(req)
	if err != nil {
		if errors.Is(err, state.ErrHistoryRequestPending) {
			return root, nil
		}
		return setCopyModeError(root, err.Error()), nil
	}
	root.History = nextHistory
	rows := copyModeHistoryRequestRows(root, deps)
	oldestAttrs := []any{
		"request_id", uint64(requestID),
		"pane_id", req.PaneID,
		"view_id", req.ViewID,
		"terminal_id", req.TerminalID,
		"cols", req.Cols,
		"limit", rows,
		"token", req.Token,
		"generation", req.Generation,
	}
	oldestAttrs = append(oldestAttrs, historyTraceBoundaryAttrs("request", req.Boundary)...)
	logHistoryTrace(deps.Logger, "tui.request.oldest", oldestAttrs...)
	return root.Advance(), []Effect{FuncEffect{
		Async:            true,
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			result, err := deps.Core.HistoryOldest(ctx, services.HistoryOldestRequest{
				RequestID:  services.RequestID(requestID),
				PaneID:     req.PaneID,
				ViewID:     req.ViewID,
				TerminalID: req.TerminalID,
				Cols:       req.Cols,
				Rows:       rows,
				Token:      req.Token,
				Generation: req.Generation,
				Boundary:   req.Boundary,
			})
			result.Window.PaneID = req.PaneID
			result.Window.ViewID = req.ViewID
			return CopyModeHistoryResultMsg{Result: result, Err: err, PaneID: req.PaneID, ViewID: req.ViewID, TerminalID: req.TerminalID}
		},
	}}
}

func reduceCopyModeHistoryResult(root state.Root, msg CopyModeHistoryResultMsg, deps CopyModeDeps) (state.Root, []Effect) {
	if msg.Err != nil {
		if errors.Is(msg.Err, services.ErrStaleHistoryWindow) {
			return rejectStaleHistoryWindowError(root, msg), nil
		}
		if staleCopyModeHistoryResult(root, msg) {
			return root, nil
		}
		if root.CopyMode.Entering {
			return setCopyModeEnterError(root, msg.Err.Error()), nil
		}
		return setCopyModeError(root, msg.Err.Error()), nil
	}
	pending := root.History.Pending
	responseAttrs := []any{
		"request_id", uint64(msg.Result.RequestID),
		"pending_kind", historyTracePendingKind(pending),
		"pane_id", msg.PaneID,
		"view_id", msg.ViewID,
		"terminal_id", msg.TerminalID,
		"window_terminal_id", msg.Result.Window.TerminalID,
		"op", string(msg.Result.Window.Op),
		"cols", msg.Result.Window.Cols,
		"token", msg.Result.Window.Token,
		"generation", msg.Result.Window.Generation,
		"rows", len(msg.Result.Window.Rows),
		"source_lines", len(msg.Result.Window.SourceLines),
		"spans", len(msg.Result.Window.Lines),
		"has_more", msg.Result.Window.HasMore,
		"summary", state.HistoryTraceWindowSummary(msg.Result.Window.Rows),
	}
	responseAttrs = append(responseAttrs, historyTraceCursorAttrs("response", msg.Result.Window.Cursor)...)
	responseAttrs = append(responseAttrs, historyTraceBoundaryAttrs("response", msg.Result.Window.Boundary)...)
	logHistoryTrace(deps.Logger, "tui.response.window", responseAttrs...)
	enteringScrollDelta := 0
	if pending != nil && pending.Kind == state.HistoryRequestLatest {
		enteringScrollDelta = root.CopyMode.EnteringScrollDelta
	}
	beforeHistory := root.History
	nextHistory, inserted, err := root.History.ApplyWindow(state.RequestID(msg.Result.RequestID), msg.Result.Window)
	if err != nil {
		if errors.Is(err, state.ErrStaleHistoryResponse) {
			return rejectMatchingHistoryResponse(root, msg, err), nil
		}
		if root.CopyMode.Entering {
			return setCopyModeEnterError(root, err.Error()), nil
		}
		return setCopyModeError(root, err.Error()), nil
	}
	applyAttrs := []any{
		"request_id", uint64(msg.Result.RequestID),
		"pending_kind", historyTracePendingKind(pending),
		"inserted_rows", inserted,
		"before_rows", len(beforeHistory.Rows),
		"after_rows", len(nextHistory.Rows),
		"before_summary", state.HistoryTraceWindowSummary(beforeHistory.Rows),
		"after_summary", state.HistoryTraceWindowSummary(nextHistory.Rows),
		"token", nextHistory.Token,
		"generation", nextHistory.Generation,
		"has_more", nextHistory.HasMore,
	}
	applyAttrs = append(applyAttrs, historyTraceCursorAttrs("after", nextHistory.Cursor)...)
	applyAttrs = append(applyAttrs, historyTraceBoundaryAttrs("after", nextHistory.Boundary)...)
	logHistoryTrace(deps.Logger, "tui.apply.window", applyAttrs...)
	root.History = nextHistory
	remainingEnteringOlderRows := 0
	if pending != nil && pending.Kind == state.HistoryRequestLatest {
		root.CopyMode = root.CopyMode.AcceptLatest(historyWindowForCopyModeAnchor(msg.Result.Window, nextHistory), nextHistory.Cols, len(nextHistory.Rows))
		remainingEnteringOlderRows = copyModeEnteringOlderRemainder(enteringScrollDelta, len(nextHistory.Rows))
	} else if pending != nil && pending.Kind == state.HistoryRequestOldest {
		root.CopyMode = root.CopyMode.AcceptOldest(msg.Result.Window, nextHistory.Cols, len(nextHistory.Rows))
	} else if pending != nil && pending.Kind == state.HistoryRequestNewer {
		root.CopyMode.BoundToken = msg.Result.Window.Token
		root.CopyMode.BoundCols = nextHistory.Cols
		root.CopyMode = root.CopyMode.FollowCursor(len(nextHistory.Rows))
		root.CopyMode = root.CopyMode.ScrollCursor(pending.ScrollDeltaAfterPrepend, len(nextHistory.Rows))
	} else {
		root.CopyMode = root.CopyMode.AcceptOlder(inserted, beforeHistory, nextHistory, msg.Result.Window, nextHistory.Cols)
		deferredRows := 0
		if pending != nil {
			deferredRows = pending.ScrollDeltaAfterPrepend
		}
		root.CopyMode = root.CopyMode.ApplyDeferredOlderScroll(deferredRows, len(nextHistory.Rows))
	}
	if root.CopyMode.Query != "" {
		root.CopyMode = root.CopyMode.RefreshQueryMatches(state.FindCopyMatches(root.History, root.CopyMode.Query))
	}
	root.CopyMode = root.CopyMode.Scroll(0, len(root.History.Rows))
	root = refreshCopyModeLogicalSelectionFocus(root)
	if pending != nil && (pending.Kind == state.HistoryRequestOlder || pending.Kind == state.HistoryRequestNewer) {
		root = trimCopyModeHistoryWindow(root, deps)
	}
	if remainingEnteringOlderRows > 0 && root.History.OlderRequestState() == state.OlderRequestReady {
		return beginCopyModeOlder(root, deps, remainingEnteringOlderRows)
	}
	return root.Advance(), nil
}

func historyTracePendingKind(pending *state.HistoryPendingRequest) string {
	if pending == nil {
		return ""
	}
	return string(pending.Kind)
}

func trimCopyModeHistoryWindow(root state.Root, deps CopyModeDeps) state.Root {
	if !root.CopyMode.Active || len(root.History.Rows) == 0 {
		return root
	}
	visible := copyModePanelRows(root, deps)
	if visible <= 0 {
		visible = copyModePageRows(root.CopyMode)
	}
	keepRows := clampColumn(visible*copyModeHistoryWindowScreens, copyModeHistoryMinRequestRows, copyModeHistoryMaxRequestRows)
	if len(root.History.Rows) <= keepRows {
		return root
	}
	root.CopyMode = root.CopyMode.EnsureLogicalSelection(root.History)
	start, end := copyModeHistoryTrimRange(root.CopyMode, len(root.History.Rows), keepRows)
	beforeRows := root.History.Rows
	beforeCursor := root.History.Cursor
	nextHistory, trim := root.History.TrimRows(start, end)
	if trim.DroppedRowsBefore == 0 && trim.DroppedRowsAfter == 0 {
		return root
	}
	root.History = nextHistory
	root.CopyMode = root.CopyMode.ApplyHistoryTrim(trim, len(root.History.Rows))
	if root.CopyMode.Query != "" {
		root.CopyMode = root.CopyMode.RefreshQueryMatches(state.FindCopyMatches(root.History, root.CopyMode.Query))
	}
	trimAttrs := []any{
		"view_id", root.History.ViewID,
		"terminal_id", root.History.TerminalID,
		"start", start,
		"end", end,
		"keep_rows", keepRows,
		"dropped_rows_before", trim.DroppedRowsBefore,
		"dropped_rows_after", trim.DroppedRowsAfter,
		"dropped_lines_before", trim.DroppedLinesBefore,
		"dropped_lines_after", trim.DroppedLinesAfter,
		"before_rows", len(beforeRows),
		"after_rows", len(root.History.Rows),
		"before_summary", state.HistoryTraceWindowSummary(beforeRows),
		"after_summary", state.HistoryTraceWindowSummary(root.History.Rows),
	}
	trimAttrs = append(trimAttrs, historyTraceCursorAttrs("before", beforeCursor)...)
	trimAttrs = append(trimAttrs, historyTraceCursorAttrs("after", root.History.Cursor)...)
	logHistoryTrace(deps.Logger, "tui.trim.window", trimAttrs...)
	return root
}

func copyModeHistoryTrimRange(copyMode state.CopyModeStore, totalRows int, keepRows int) (int, int) {
	if totalRows <= 0 {
		return 0, -1
	}
	if keepRows <= 0 || keepRows >= totalRows {
		return 0, totalRows - 1
	}
	visible := copyMode.ViewRows
	if visible <= 0 {
		visible = 8
	}
	// 中文说明：history.window 已有 older/newer cursor，TUI 只保留围绕当前
	// viewport 的本地投影；窗口外内容后续按 frozen token 从 core/backend 拉回。
	center := copyMode.ViewportTop + visible/2
	start := center - keepRows/2
	if start < 0 {
		start = 0
	}
	end := start + keepRows - 1
	if end >= totalRows {
		end = totalRows - 1
		start = end - keepRows + 1
		if start < 0 {
			start = 0
		}
	}
	viewportEnd := copyMode.ViewportTop + visible - 1
	marginStart := copyMode.ViewportTop - visible*copyModeHistoryPrefetchScreens
	marginEnd := viewportEnd + visible*copyModeHistoryPrefetchScreens
	if marginStart < 0 {
		marginStart = 0
	}
	if marginEnd >= totalRows {
		marginEnd = totalRows - 1
	}
	if marginStart < start {
		start = marginStart
	}
	if marginEnd > end {
		end = marginEnd
	}
	start, end = expandTrimRangeForSelection(copyMode, start, end, totalRows)
	start = clampColumn(start, 0, totalRows-1)
	end = clampColumn(end, start, totalRows-1)
	return start, end
}

func expandTrimRangeForSelection(copyMode state.CopyModeStore, start int, end int, totalRows int) (int, int) {
	positions := []state.CopyPosition{copyMode.Cursor}
	selectionHasLogicalRange := copyMode.Selection != nil &&
		copyMode.Selection.LogicalAnchor.Valid &&
		copyMode.Selection.LogicalFocus.Valid
	if !selectionHasLogicalRange {
		if copyMode.Mark != nil {
			positions = append(positions, *copyMode.Mark)
		}
		if copyMode.Selection != nil {
			positions = append(positions, copyMode.Selection.Anchor, copyMode.Selection.Focus)
		}
	}
	for _, pos := range positions {
		if pos.Row < start {
			start = pos.Row
		}
		if pos.Row > end {
			end = pos.Row
		}
	}
	start = clampColumn(start, 0, totalRows-1)
	end = clampColumn(end, start, totalRows-1)
	return start, end
}

func copyModeEnteringOlderRemainder(scrollDelta int, loadedRows int) int {
	if scrollDelta >= 0 {
		return 0
	}
	localRowsAboveTail := loadedRows - 1
	if localRowsAboveTail < 0 {
		localRowsAboveTail = 0
	}
	olderRows := -scrollDelta - localRowsAboveTail
	if olderRows < 0 {
		return 0
	}
	return olderRows
}

func rejectMatchingHistoryResponse(root state.Root, msg CopyModeHistoryResultMsg, err error) state.Root {
	if root.History.Pending == nil || state.RequestID(msg.Result.RequestID) != root.History.Pending.ID || !copyModeOwnsPendingHistory(root) {
		return root
	}
	// 中文说明：同一个 request 已经返回但被 stale guard 拒绝时，不能继续保留 pending；
	// 否则 copy history footer 会永久显示 `↑ loading`，并阻止下一次 older 请求重试。
	root.History.Pending = nil
	if root.CopyMode.Entering {
		return setCopyModeEnterError(root, err.Error())
	}
	return setCopyModeError(root, err.Error())
}

func rejectStaleHistoryWindowError(root state.Root, msg CopyModeHistoryResultMsg) state.Root {
	if root.History.Pending == nil || state.RequestID(msg.Result.RequestID) != root.History.Pending.ID || !copyModeOwnsPendingHistory(root) {
		return root
	}
	pending := *root.History.Pending
	// 中文说明：protocol stale history window 表达 frozen token/cursor 已过期；
	// 这是请求生命周期控制信号，不能把 protocol 400 暴露成用户可见错误。
	root.History.Pending = nil
	if pending.Kind == state.HistoryRequestLatest && root.CopyMode.Entering {
		return exitCopyMode(root).Advance()
	}
	return root.Advance()
}

func copyModeOwnsPendingHistory(root state.Root) bool {
	pending := root.History.Pending
	if pending == nil || !copyModeInputContext(root.CopyMode) {
		return false
	}
	if pending.TerminalID != "" && root.CopyMode.TerminalID != pending.TerminalID {
		return false
	}
	if pending.PaneID != "" && root.CopyMode.PaneID != pending.PaneID {
		return false
	}
	if pending.ViewID != "" && root.CopyMode.ViewID != pending.ViewID {
		return false
	}
	if (pending.Kind == state.HistoryRequestOlder || pending.Kind == state.HistoryRequestOldest) && pending.Token != "" && root.CopyMode.BoundToken != pending.Token {
		return false
	}
	return true
}

func reduceCopyModeCopySelection(root state.Root, deps CopyModeDeps) (state.Root, []Effect) {
	if deps.Clipboard == nil {
		return setCopyModeError(root, "clipboard service missing"), nil
	}
	text := SelectedText(root.History, root.CopyMode)
	start, end, hasRange := root.CopyMode.SelectionLogicalRange(root.History)
	needsBackend := root.CopyMode.SelectionNeedsBackend(root.History)
	if text == "" && (!hasRange || !needsBackend) {
		return root, nil
	}
	req := services.HistoryCopyRangeRequest{
		TerminalID: root.History.TerminalID,
		Cols:       root.History.Cols,
		Token:      root.History.Token,
		Generation: root.History.Generation,
		Boundary:   root.History.Boundary,
		Start:      start,
		End:        end,
	}
	shouldFetchBackend := needsBackend && hasRange && deps.Core != nil && req.TerminalID != "" && req.Token != ""
	if needsBackend && !shouldFetchBackend {
		return setCopyModeError(root, "history copy backend missing"), nil
	}
	root = root.Advance()
	return root, []Effect{
		FuncEffect{
			Async:            true,
			ForceSyncInTests: true,
			Run: func(ctx context.Context) Msg {
				copied := text
				if shouldFetchBackend {
					// 中文说明：本地窗口被虚拟滚动裁掉后，只保留 logical range，
					// 复制正文必须从 frozen core/backend 拉回，不能要求 TUI 常驻旧 rows。
					result, err := deps.Core.HistoryCopyRange(ctx, req)
					if err != nil {
						return CopyModeCopyResultMsg{Err: err}
					}
					copied = result.Text
				}
				if copied == "" {
					return CopyModeCopyResultMsg{}
				}
				err := deps.Clipboard.Write(ctx, services.ClipboardWriteRequest{Text: copied})
				return CopyModeCopyResultMsg{Text: copied, Err: err, Commit: true}
			},
		},
	}
}

func reduceCopyModePaste(root state.Root, deps CopyModeDeps, readSystemClipboard bool) (state.Root, []Effect) {
	if deps.Clipboard == nil {
		return setCopyModeError(root, "clipboard service missing"), nil
	}
	if deps.Terminal == nil {
		return setCopyModeError(root, "terminal service missing"), nil
	}
	target, ok := liveInputTarget(root)
	if !ok || target.TerminalID == "" || target.Channel == 0 {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.input", Body: "no terminal bound"})
		return root.Advance(), nil
	}
	root, releaseEffects := exitCopyModeWithRelease(root, deps)
	root = root.Advance()
	effects := append([]Effect{}, releaseEffects...)
	effects = append(effects, FuncEffect{
		Async:            true,
		SerialKey:        terminalInputSerialKey(target),
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			text := deps.Clipboard.LastCopy()
			if readSystemClipboard {
				result, err := deps.Clipboard.Read(ctx)
				if err != nil {
					return CopyModePasteResultMsg{Err: err}
				}
				text = result.Text
			}
			if text == "" && !readSystemClipboard {
				text = latestClipboardHistoryText(root.Clipboard)
			}
			if text == "" {
				if readSystemClipboard {
					return CopyModePasteResultMsg{Err: errors.New("system clipboard is empty")}
				}
				return CopyModePasteResultMsg{Err: errors.New("copy buffer is empty")}
			}
			// 中文说明：paste 属于发往 active terminal 的语义化输入；
			// 如果 live surface 开着 bracketed paste，就在 reducer-owned live modes 基础上包裹 200~/201~。
			err := deps.Terminal.SendInput(ctx, services.TerminalInputRequest{
				TerminalID: target.TerminalID,
				Channel:    target.Channel,
				SurfaceID:  target.SurfaceID,
				ViewID:     target.ViewID,
				Bytes:      encodeTerminalPaste(text, root.Surface.SurfaceForTerminal(target.TerminalID).Modes),
			})
			return CopyModePasteResultMsg{Text: text, Err: err}
		},
	})
	return root, effects
}

func historyWindowForCopyModeAnchor(window state.HistoryWindow, history state.HistoryStore) state.HistoryWindow {
	window.Rows = history.Rows
	window.Cols = history.Cols
	return window
}

func latestClipboardHistoryText(store state.ClipboardStore) string {
	for _, entry := range store.Entries {
		text := strings.TrimSpace(entry.Text)
		if text != "" {
			return entry.Text
		}
	}
	return ""
}

func reduceCopyModePasteText(root state.Root, deps CopyModeDeps, text string) (state.Root, []Effect) {
	if deps.Terminal == nil {
		return setCopyModeError(root, "terminal service missing"), nil
	}
	target, ok := liveInputTarget(root)
	if !ok || target.TerminalID == "" || target.Channel == 0 {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.input", Body: "no terminal bound"})
		return root.Advance(), nil
	}
	if strings.TrimSpace(text) == "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "clipboard history", Body: "no clipboard entry"})
		return root.Advance(), nil
	}
	root, releaseEffects := exitCopyModeWithRelease(root, deps)
	root = root.Advance()
	effects := append([]Effect{}, releaseEffects...)
	effects = append(effects, FuncEffect{
		Async:            true,
		SerialKey:        terminalInputSerialKey(target),
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			err := deps.Terminal.SendInput(ctx, services.TerminalInputRequest{
				TerminalID: target.TerminalID,
				Channel:    target.Channel,
				SurfaceID:  target.SurfaceID,
				ViewID:     target.ViewID,
				Bytes:      encodeTerminalPaste(text, root.Surface.SurfaceForTerminal(target.TerminalID).Modes),
			})
			return CopyModePasteResultMsg{Text: text, Err: err}
		},
	})
	return root, effects
}

func SelectedText(history state.HistoryStore, copyMode state.CopyModeStore) string {
	if copyMode.Selection == nil || !copyMode.Active {
		return ""
	}
	start := copyMode.Selection.Anchor
	end := copyMode.Selection.Focus
	if positionAfter(start, end) {
		start, end = end, start
	}
	if start.Row < 0 {
		start.Row = 0
	}
	if end.Row >= len(history.Rows) {
		end.Row = len(history.Rows) - 1
	}
	if start.Row > end.Row || start.Row >= len(history.Rows) || end.Row < 0 {
		return ""
	}
	lines := make([]string, 0, end.Row-start.Row+1)
	var currentLineID uint64
	var currentLine strings.Builder
	flushCurrentLine := func() {
		if currentLine.Len() == 0 && currentLineID == 0 {
			return
		}
		lines = append(lines, currentLine.String())
		currentLine.Reset()
		currentLineID = 0
	}
	for row := start.Row; row <= end.Row; row++ {
		from := 0
		to := state.HistoryRowDisplayWidth(history.Rows[row])
		if row == start.Row {
			from = clampColumn(start.Col, 0, to)
		}
		if row == end.Row {
			to = clampColumn(end.Col, 0, to)
		}
		if from > to {
			from, to = to, from
		}
		segment := state.HistoryRowSliceDisplay(history.Rows[row], from, to)
		lineID := history.Rows[row].LineID
		if currentLineID != 0 && lineID != 0 && currentLineID != lineID {
			flushCurrentLine()
		}
		if currentLineID == 0 {
			currentLineID = lineID
		}
		currentLine.WriteString(segment)
	}
	flushCurrentLine()
	return strings.Join(lines, "\n")
}

func positionAfter(left state.CopyPosition, right state.CopyPosition) bool {
	if left.Row != right.Row {
		return left.Row > right.Row
	}
	return left.Col > right.Col
}

func clampColumn(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func encodeTerminalPaste(text string, modes state.LiveTerminalModes) []byte {
	if text == "" {
		return nil
	}
	if !modes.BracketedPaste {
		return []byte(text)
	}
	return []byte("\x1b[200~" + text + "\x1b[201~")
}

func nextHistoryRequestID(root state.Root) state.RequestID {
	next := state.RequestID(root.Generation + 1)
	if root.History.Pending != nil && root.History.Pending.ID >= next {
		next = root.History.Pending.ID + 1
	}
	return next
}

func clampCopyCursor(copyMode state.CopyModeStore, history state.HistoryStore) state.CopyModeStore {
	if len(history.Rows) == 0 {
		copyMode.Cursor = state.CopyPosition{}
		return copyMode
	}
	row := clampColumn(copyMode.Cursor.Row, 0, len(history.Rows)-1)
	col := clampColumn(copyMode.Cursor.Col, 0, state.HistoryRowDisplayWidth(history.Rows[row]))
	copyMode.Cursor = state.CopyPosition{Row: row, Col: col}
	return copyMode
}

func ensureCopyCursorVisible(copyMode state.CopyModeStore, totalRows int) state.CopyModeStore {
	return copyMode.FollowCursor(totalRows)
}

func copyModePageRows(copyMode state.CopyModeStore) int {
	if copyMode.ViewRows > 2 {
		return copyMode.ViewRows - 2
	}
	return 8
}

func copyModeVisibleRows(copyMode state.CopyModeStore) int {
	if copyMode.ViewRows > 0 {
		return copyMode.ViewRows
	}
	return 8
}

func copyModeLineScrollRows() int {
	return 1
}

func copyModeOlderScrollRows(copyMode state.CopyModeStore, event input.InputEvent) int {
	if event.Kind == input.EventKindKey && event.Key == input.KeyPageUp {
		return copyModePageRows(copyMode)
	}
	return copyModeLineScrollRows()
}

func copyModeNewerScrollRows(copyMode state.CopyModeStore, event input.InputEvent) int {
	if event.Kind == input.EventKindKey && event.Key == input.KeyPageDn {
		return copyModePageRows(copyMode)
	}
	return copyModeLineScrollRows()
}

func maybePrefetchCopyModeOlder(root state.Root, deps CopyModeDeps, scrollDeltaAfterPrepend int) (state.Root, []Effect) {
	if root.CopyMode.ViewportTop > copyModeOlderPrefetchRows(root, deps) {
		return root, nil
	}
	if root.History.OlderRequestState() != state.OlderRequestReady {
		return root, nil
	}
	return beginCopyModeOlder(root, deps, scrollDeltaAfterPrepend)
}

func copyModeHistoryRequestRows(root state.Root, deps CopyModeDeps) int {
	panelRows := copyModePanelRows(root, deps)
	return clampColumn(panelRows*copyModeHistoryRequestScreens, copyModeHistoryMinRequestRows, copyModeHistoryMaxRequestRows)
}

func copyModeOlderPrefetchRows(root state.Root, deps CopyModeDeps) int {
	panelRows := copyModePanelRows(root, deps)
	return clampColumn(panelRows*copyModeHistoryPrefetchScreens, panelRows, copyModeHistoryMaxRequestRows/2)
}

func copyModePanelRows(root state.Root, deps CopyModeDeps) int {
	if root.CopyMode.ViewRows > 0 {
		return root.CopyMode.ViewRows
	}
	if rect, ok := copyModeContentRect(root); ok && rect.H > 0 {
		return rect.H
	}
	if deps.Rows > 0 {
		return deps.Rows
	}
	return 24
}

func requestRows(panelRows int, fallbackRows int) int {
	if panelRows <= 0 {
		panelRows = fallbackRows
	}
	if panelRows <= 0 {
		panelRows = 24
	}
	return clampColumn(panelRows*copyModeHistoryRequestScreens, copyModeHistoryMinRequestRows, copyModeHistoryMaxRequestRows)
}

func setCopyModeError(root state.Root, message string) state.Root {
	root.Surface = root.Surface.SetError(message)
	root.Session = root.Session.SetError(message)
	return root.Advance()
}

func setCopyModeEnterError(root state.Root, message string) state.Root {
	root = exitCopyMode(root)
	return setCopyModeError(root, message)
}

func exitCopyModeWithRelease(root state.Root, deps CopyModeDeps) (state.Root, []Effect) {
	token := root.CopyMode.BoundToken
	terminalID := root.CopyMode.TerminalID
	viewID := copyHistoryWorkingViewID(root)
	root = exitCopyMode(root)
	root = root.WithoutCopyHistorySession(viewID)
	if deps.Core == nil || token == "" {
		return root, nil
	}
	return root, []Effect{FuncEffect{
		Async:            true,
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			_ = deps.Core.ReleaseHistory(ctx, services.HistoryReleaseRequest{
				TerminalID: terminalID,
				Token:      token,
			})
			return NoopMsg{}
		},
	}}
}

func exitCopyMode(root state.Root) state.Root {
	// 中文说明：copy/history window 只是当前交互投影；退出或取消后必须释放，
	// 否则 TUI 会继续持有 rows/source lines 的 backing array。
	viewID := copyHistoryWorkingViewID(root)
	root.History = state.HistoryStore{}
	root.CopyMode = state.CopyModeStore{}
	if viewID != "" {
		root = root.WithoutCopyHistorySession(viewID)
	}
	return root
}
