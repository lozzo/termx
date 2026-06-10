package app

import (
	"context"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type CopyModeDeps struct {
	Core      services.CoreClient
	Clipboard services.ClipboardService
	Rows      int
}

type CopyModeHistoryResultMsg struct {
	Result services.HistoryResult
	Err    error
}

func (CopyModeHistoryResultMsg) isMsg() {}

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
	Text string
	Err  error
}

func (CopyModeCopyResultMsg) isMsg() {}

type CopyModeSetQueryMsg struct {
	Query string
}

func (CopyModeSetQueryMsg) isMsg() {}

type CopyModeMoveMatchMsg struct {
	Delta int
}

func (CopyModeMoveMatchMsg) isMsg() {}

type CopyModeScrollMsg struct {
	Delta int
}

func (CopyModeScrollMsg) isMsg() {}

type CopyModeMouseSelectMsg struct {
	Position state.CopyPosition
}

func (CopyModeMouseSelectMsg) isMsg() {}

func NewCopyModeReducer(deps CopyModeDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case InputMsg:
			intent := input.Route(msg.Event, root.CopyMode.Active)
			return reduceCopyModeIntent(root, intent, deps)
		case CopyModeHistoryResultMsg:
			return reduceCopyModeHistoryResult(root, msg)
		case CopyModeMoveCursorMsg:
			root.CopyMode = root.CopyMode.MoveCursor(msg.Position)
			return root.Advance(), nil
		case CopyModeSetMarkMsg:
			root.CopyMode = root.CopyMode.SetMark(msg.Position)
			return root.Advance(), nil
		case CopyModeCopySelectionMsg:
			return reduceCopyModeCopySelection(root, deps)
		case CopyModeCopyResultMsg:
			if msg.Err != nil {
				root.Surface = root.Surface.SetError(msg.Err.Error())
				root.Session = root.Session.SetError(msg.Err.Error())
				return root.Advance(), nil
			}
			return root, nil
		case CopyModeSetQueryMsg:
			root.CopyMode = root.CopyMode.SetQuery(msg.Query, state.FindCopyMatches(root.History, msg.Query))
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return root.Advance(), nil
		case CopyModeMoveMatchMsg:
			root.CopyMode = root.CopyMode.MoveMatch(msg.Delta)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return root.Advance(), nil
		case CopyModeScrollMsg:
			root.CopyMode = root.CopyMode.Scroll(msg.Delta, len(root.History.Rows))
			return root.Advance(), nil
		case CopyModeMouseSelectMsg:
			root.CopyMode = root.CopyMode.MoveCursor(msg.Position)
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			root.CopyMode = root.CopyMode.SetMark(root.CopyMode.Cursor)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return root.Advance(), nil
		default:
			return root, nil
		}
	}
}

func reduceCopyModeIntent(root state.Root, intent input.Intent, deps CopyModeDeps) (state.Root, []Effect) {
	switch intent.Kind {
	case input.IntentEnterCopyMode:
		next, effects := beginCopyModeLatest(root, deps)
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.IntentRequestOlder:
		next, effects := beginCopyModeOlder(root, deps)
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.IntentExitCopyMode:
		root.CopyMode = state.CopyModeStore{}
		return root.Advance(), []Effect{handledEffect{}}
	case input.IntentMouseSelect:
		if !root.CopyMode.Active {
			return root, nil
		}
		root.CopyMode = root.CopyMode.SetMark(root.CopyMode.Cursor)
		return root.Advance(), []Effect{handledEffect{}}
	default:
		if root.CopyMode.Active {
			next, handled := reduceCopyModeMouseInput(root, intent.Event)
			if handled {
				return next, []Effect{handledEffect{}}
			}
			next, keyEffects, handled := reduceCopyModeKeyInput(root, intent.Event, deps)
			if handled {
				return next, append([]Effect{handledEffect{}}, keyEffects...)
			}
		}
		return root, nil
	}
}

func reduceCopyModeMouseInput(root state.Root, event input.InputEvent) (state.Root, bool) {
	if event.Kind != input.EventKindMouse {
		return root, false
	}
	switch event.Mouse {
	case input.MouseWheelDown:
		root.CopyMode = root.CopyMode.Scroll(copyModePageRows(root.CopyMode), len(root.History.Rows))
		return root.Advance(), true
	default:
		return root, false
	}
}

func reduceCopyModeKeyInput(root state.Root, event input.InputEvent, deps CopyModeDeps) (state.Root, []Effect, bool) {
	if event.Kind != input.EventKindKey {
		return root, nil, false
	}
	switch event.Key {
	case input.KeyPageDn:
		root.CopyMode = root.CopyMode.Scroll(copyModePageRows(root.CopyMode), len(root.History.Rows))
		return root.Advance(), nil, true
	case input.KeyHome:
		root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row, Col: 0})
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		return root.Advance(), nil, true
	case input.KeyEnd:
		root.CopyMode = root.CopyMode.MoveCursor(copyModeLineEndPosition(root.History, root.CopyMode.Cursor.Row))
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		return root.Advance(), nil, true
	case input.KeyLeft:
		root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row, Col: root.CopyMode.Cursor.Col - 1})
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		return root.Advance(), nil, true
	case input.KeyRight:
		root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row, Col: root.CopyMode.Cursor.Col + 1})
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		return root.Advance(), nil, true
	case input.KeyDown:
		if root.CopyMode.Query != "" {
			root.CopyMode = root.CopyMode.MoveMatch(1)
		} else {
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row + 1, Col: root.CopyMode.Cursor.Col})
		}
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
		return root.Advance(), nil, true
	case input.KeyUp:
		if root.CopyMode.Query != "" {
			root.CopyMode = root.CopyMode.MoveMatch(-1)
		} else {
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row - 1, Col: root.CopyMode.Cursor.Col})
		}
		root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
		root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
		return root.Advance(), nil, true
	case input.KeyEnter:
		if root.CopyMode.Query != "" {
			root.CopyMode = root.CopyMode.MoveMatch(1)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return root.Advance(), nil, true
		}
		if root.CopyMode.Selection != nil {
			next, effects := reduceCopyModeCopySelection(root, deps)
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
			return root.Advance(), nil, true
		case "l":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row, Col: root.CopyMode.Cursor.Col + 1})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			return root.Advance(), nil, true
		case "j":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row + 1, Col: root.CopyMode.Cursor.Col})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return root.Advance(), nil, true
		case "k":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: root.CopyMode.Cursor.Row - 1, Col: root.CopyMode.Cursor.Col})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return root.Advance(), nil, true
		case "g":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: 0, Col: root.CopyMode.Cursor.Col})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return root.Advance(), nil, true
		case "G":
			root.CopyMode = root.CopyMode.MoveCursor(state.CopyPosition{Row: len(root.History.Rows) - 1, Col: root.CopyMode.Cursor.Col})
			root.CopyMode = clampCopyCursor(root.CopyMode, root.History)
			root.CopyMode = ensureCopyCursorVisible(root.CopyMode, len(root.History.Rows))
			return root.Advance(), nil, true
		case " ":
			root.CopyMode = root.CopyMode.SetMark(root.CopyMode.Cursor)
			return root.Advance(), nil, true
		case "y":
			if root.CopyMode.Selection != nil {
				next, effects := reduceCopyModeCopySelection(root, deps)
				return next, effects, true
			}
			return root, nil, true
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
	terminalID := root.Session.TerminalID
	if terminalID == "" {
		terminalID = root.Surface.TerminalID
	}
	rect, ok := copyModeContentRect(root)
	if terminalID == "" || !ok || rect.W <= 0 {
		return setCopyModeError(root, "copy mode requires attached terminal and cols"), nil
	}
	return beginCopyModeLatestForCols(root, deps, terminalID, rect.W, rect.H)
}

func beginCopyModeLatestForCols(root state.Root, deps CopyModeDeps, terminalID string, cols int, rowsHint int) (state.Root, []Effect) {
	requestID := nextHistoryRequestID(root)
	nextHistory, err := root.History.BeginLatest(state.HistoryPendingRequest{
		ID:         requestID,
		TerminalID: terminalID,
		Cols:       cols,
	})
	if err != nil {
		return setCopyModeError(root, err.Error()), nil
	}
	root.History = nextHistory
	root.CopyMode = root.CopyMode.BindLatest(terminalID, requestID, cols, rowsHint)
	rows := requestRows(deps, rowsHint)
	return root.Advance(), []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			result, err := deps.Core.HistoryLatest(ctx, services.HistoryLatestRequest{
				RequestID:  services.RequestID(requestID),
				TerminalID: terminalID,
				Cols:       cols,
				Rows:       rows,
			})
			return CopyModeHistoryResultMsg{Result: result, Err: err}
		},
	}}
}

func beginCopyModeOlder(root state.Root, deps CopyModeDeps) (state.Root, []Effect) {
	if deps.Core == nil {
		return setCopyModeError(root, "core client missing"), nil
	}
	if root.History.Pending != nil {
		return root, nil
	}
	if root.History.Exhausted.Valid &&
		root.History.Exhausted.Token == root.History.Token &&
		root.History.Exhausted.Cols == root.History.Cols &&
		root.History.Exhausted.Cursor == root.History.Cursor &&
		root.History.Exhausted.Boundary == root.History.Boundary {
		return root, nil
	}
	if root.History.Token == "" || !root.History.Cursor.Valid {
		return root, nil
	}
	requestID := nextHistoryRequestID(root)
	req := state.HistoryPendingRequest{
		ID:         requestID,
		TerminalID: root.History.TerminalID,
		Cols:       root.History.Cols,
		Token:      root.History.Token,
		Generation: root.History.Generation,
		Cursor:     root.History.Cursor,
		Boundary:   root.History.Boundary,
	}
	nextHistory, err := root.History.BeginOlder(req)
	if err != nil {
		return setCopyModeError(root, err.Error()), nil
	}
	root.History = nextHistory
	rows := requestRows(deps, copyModeRowsHint(root))
	return root.Advance(), []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			result, err := deps.Core.HistoryOlder(ctx, services.HistoryOlderRequest{
				RequestID:  services.RequestID(requestID),
				TerminalID: req.TerminalID,
				Cols:       req.Cols,
				Rows:       rows,
				Token:      req.Token,
				Generation: req.Generation,
				Cursor:     req.Cursor,
				Boundary:   req.Boundary,
			})
			return CopyModeHistoryResultMsg{Result: result, Err: err}
		},
	}}
}

func reduceCopyModeHistoryResult(root state.Root, msg CopyModeHistoryResultMsg) (state.Root, []Effect) {
	if msg.Err != nil {
		return setCopyModeError(root, msg.Err.Error()), nil
	}
	pending := root.History.Pending
	nextHistory, inserted, err := root.History.ApplyWindow(state.RequestID(msg.Result.RequestID), msg.Result.Window)
	if err != nil {
		return setCopyModeError(root, err.Error()), nil
	}
	root.History = nextHistory
	if pending != nil && pending.Kind == state.HistoryRequestLatest {
		root.CopyMode = root.CopyMode.AcceptLatest(msg.Result.Window)
	} else {
		root.CopyMode = root.CopyMode.AcceptOlder(inserted, msg.Result.Window)
	}
	if root.CopyMode.Query != "" {
		root.CopyMode = root.CopyMode.SetQuery(root.CopyMode.Query, state.FindCopyMatches(root.History, root.CopyMode.Query))
	}
	root.CopyMode = root.CopyMode.Scroll(0, len(root.History.Rows))
	return root.Advance(), nil
}

func reduceCopyModeCopySelection(root state.Root, deps CopyModeDeps) (state.Root, []Effect) {
	text := SelectedText(root.History, root.CopyMode)
	if text == "" {
		return root, nil
	}
	if deps.Clipboard == nil {
		return setCopyModeError(root, "clipboard service missing"), nil
	}
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastSuccess, Title: "Copied to clipboard", DismissAfterTicks: 3})
	root = root.Advance()
	return root, []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			err := deps.Clipboard.Write(ctx, services.ClipboardWriteRequest{Text: text})
			return CopyModeCopyResultMsg{Text: text, Err: err}
		},
	}}
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
		lines = append(lines, state.HistoryRowSliceDisplay(history.Rows[row], from, to))
	}
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
	if totalRows <= 0 {
		copyMode.ViewportTop = 0
		return copyMode
	}
	height := copyModePageRows(copyMode)
	if copyMode.Cursor.Row < copyMode.ViewportTop {
		copyMode.ViewportTop = copyMode.Cursor.Row
	}
	if copyMode.Cursor.Row >= copyMode.ViewportTop+height {
		copyMode.ViewportTop = copyMode.Cursor.Row - height + 1
	}
	return copyMode.Scroll(0, totalRows)
}

func copyModePageRows(copyMode state.CopyModeStore) int {
	if copyMode.ViewRows > 2 {
		return copyMode.ViewRows - 2
	}
	return 8
}

func requestRows(deps CopyModeDeps, sessionRows int) int {
	if deps.Rows > 0 {
		return deps.Rows
	}
	if sessionRows > 0 {
		return sessionRows
	}
	return 24
}

func setCopyModeError(root state.Root, message string) state.Root {
	root.Surface = root.Surface.SetError(message)
	root.Session = root.Session.SetError(message)
	return root.Advance()
}
