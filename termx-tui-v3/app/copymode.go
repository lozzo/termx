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
	default:
		return root, nil
	}
}

func beginCopyModeLatest(root state.Root, deps CopyModeDeps) (state.Root, []Effect) {
	if deps.Core == nil {
		return setCopyModeError(root, "core client missing"), nil
	}
	terminalID := root.Session.TerminalID
	if terminalID == "" {
		terminalID = root.Surface.TerminalID
	}
	cols := root.Session.Cols
	if cols == 0 {
		cols = root.Surface.Cols
	}
	if terminalID == "" || cols == 0 {
		return setCopyModeError(root, "copy mode requires attached terminal and cols"), nil
	}
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
	root.CopyMode = root.CopyMode.BindLatest(terminalID, requestID, cols)
	rows := requestRows(deps, root.Session.Rows)
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
	rows := requestRows(deps, root.Session.Rows)
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
		runes := []rune(history.Rows[row].Text)
		from := 0
		to := len(runes)
		if row == start.Row {
			from = clampColumn(start.Col, 0, len(runes))
		}
		if row == end.Row {
			to = clampColumn(end.Col, 0, len(runes))
		}
		if from > to {
			from, to = to, from
		}
		lines = append(lines, string(runes[from:to]))
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
