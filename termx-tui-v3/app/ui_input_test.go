package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestUIInputReducerOpensTerminalPickerFromCtrlF(t *testing.T) {
	reducer := NewUIInputReducer()
	root, effects := reducer(state.Root{Shell: state.DefaultShell()}, InputMsg{Event: input.InputEvent{
		Kind: input.EventKindKey,
		Key:  input.KeyChar,
		Char: "\x06",
		Ctrl: true,
	}})

	if !root.Shell.Overlay.Open || root.Shell.Overlay.Kind != state.OverlayTerminalPicker {
		t.Fatalf("expected terminal picker overlay, got %#v", root.Shell.Overlay)
	}
	if len(effects) != 2 {
		t.Fatalf("expected handled and pool list effects, got %#v", effects)
	}
	if _, ok := effects[0].(handledEffect); !ok {
		t.Fatalf("expected handled effect, got %#v", effects)
	}
	if effect, ok := effects[1].(FuncEffect); !ok || effect.Run == nil {
		t.Fatalf("expected terminal pool list effect, got %#v", effects[1])
	}
}

func TestInteractiveRuntimeCtrlFDoesNotSendTerminalInput(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(8)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x06", Ctrl: true}); err != nil {
		t.Fatalf("send ctrl-f: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain ctrl-f: %v", err)
	}

	if !runtime.State().Shell.Overlay.Open || runtime.State().Shell.Overlay.Kind != state.OverlayTerminalPicker {
		t.Fatalf("expected terminal picker overlay, got %#v", runtime.State().Shell.Overlay)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("ctrl-f must not be sent to terminal, got %#v", terminal.Inputs)
	}
	last := lastFrame(t, host.Frames())
	if !frameContains(last, "terminal picker") ||
		!frameContains(last, "search:") ||
		!frameContains(last, "▸ + new terminal  Create a new terminal") ||
		!frameContains(last, "shell") ||
		!frameContains(last, "live @pane-main") ||
		frameContains(last, "Select terminal source state target") ||
		frameContains(last, "DETAIL") {
		t.Fatalf("expected terminal picker product content in frame, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeTerminalPickerKeyboardFlow(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-main", Channel: 4, Cols: 80, Rows: 24},
	}
	initialShell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "日志🚀", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: initialShell},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-main", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x06", Ctrl: true}); err != nil {
		t.Fatalf("send ctrl-f: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "日"}); err != nil {
		t.Fatalf("send query char: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "志"}); err != nil {
		t.Fatalf("send query char: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain query: %v", err)
	}
	if runtime.State().Shell.EnsureDefaults().Overlay.Query != "日志" {
		t.Fatalf("expected picker query retained in reducer state, got %#v", runtime.State().Shell.Overlay)
	}
	queryFrame := lastFrame(t, host.Frames())
	if !frameContains(queryFrame, "search: 日志") || !frameContains(queryFrame, "▸ + new terminal  Create a new terminal") || !frameContains(queryFrame, "term-2") || !frameContains(queryFrame, "日志🚀") || !frameContains(queryFrame, "live @pane-2") || frameContains(queryFrame, "DETAIL 日志🚀") {
		t.Fatalf("expected filtered picker frame, got %#v", queryFrame.Lines)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("picker query must not leak to terminal input, got %#v", terminal.Inputs)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x7f"}); err != nil {
		t.Fatalf("send backspace: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x7f"}); err != nil {
		t.Fatalf("send backspace: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}); err != nil {
		t.Fatalf("send down to first terminal row: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}); err != nil {
		t.Fatalf("send down to second terminal row: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain enter: %v", err)
	}
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != "pane-2" || runtime.State().Shell.Overlay.Open {
		t.Fatalf("enter should attach/focus selected picker row and close overlay, got %#v", runtime.State().Shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("picker navigation must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeTerminalPickerEnterCreatesFromCreateRow(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-main", Channel: 4, Cols: 80, Rows: 24},
		CreateResult: services.TerminalCreateResult{TerminalID: "term-created", State: "running"},
	}
	initialShell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: initialShell},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-main", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x06", Ctrl: true}); err != nil {
		t.Fatalf("send ctrl-f: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain enter: %v", err)
	}
	if len(terminal.Creates) != 1 {
		t.Fatalf("create row enter should call terminal create, got %#v", terminal.Creates)
	}
	toasts := runtime.State().Shell.Toasts
	if len(toasts) == 0 || toasts[len(toasts)-1].Title != "picker.new" || toasts[len(toasts)-1].Body != "term-created" {
		t.Fatalf("create row enter should show create feedback, got %#v", toasts)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("picker create navigation must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeTerminalPickerUsesTerminalPoolService(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-pool", Channel: 9, Cols: 80, Rows: 24},
		ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{
			TerminalID: "term-pool",
			Title:      "远程🚀",
			State:      "running",
		}}},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x06", Ctrl: true}); err != nil {
		t.Fatalf("send ctrl-f: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain picker list: %v", err)
	}
	if len(terminal.Lists) != 1 || runtime.State().TerminalPool.Status != state.TerminalPoolReady {
		t.Fatalf("expected picker open to load terminal pool, lists=%#v pool=%#v", terminal.Lists, runtime.State().TerminalPool)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "term-pool") || !frameContains(frame, "远程🚀") || !frameContains(frame, "running @pool") {
		t.Fatalf("expected pool row in picker frame, got %#v", frame.Lines)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}); err != nil {
		t.Fatalf("send down to workspace row: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}); err != nil {
		t.Fatalf("send down to pool row: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pool attach: %v", err)
	}
	if len(terminal.Attaches) != 1 || terminal.Attaches[0].TerminalID != "term-pool" {
		t.Fatalf("expected pool attach through service, got %#v", terminal.Attaches)
	}
	if !runtime.State().Session.Attached || runtime.State().Session.TerminalID != "term-pool" || runtime.State().Shell.Overlay.Open {
		t.Fatalf("expected attached pool terminal and closed overlay, got session=%#v shell=%#v", runtime.State().Session, runtime.State().Shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("picker pool navigation must not leak terminal input, got %#v", terminal.Inputs)
	}
}

func TestTerminalPoolReducerHandlesListErrorCreateAndStaleResult(t *testing.T) {
	terminal := &services.FakeTerminalService{ListErr: errors.New("list failed")}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root, effects := reducer(state.Root{Shell: state.DefaultShell()}, TerminalPoolListRequestMsg{})
	if root.TerminalPool.Status != state.TerminalPoolLoading || len(effects) != 1 {
		t.Fatalf("expected loading pool and list effect, got root=%#v effects=%#v", root, effects)
	}
	root, _ = reducer(root, TerminalPoolListResultMsg{Seq: root.TerminalPool.RequestSeq, Err: errors.New("list failed")})
	if root.TerminalPool.Status != state.TerminalPoolError || root.TerminalPool.LastError != "list failed" || len(root.Shell.Toasts) == 0 {
		t.Fatalf("expected list error state and toast, got %#v", root)
	}
	staleSeq := root.TerminalPool.RequestSeq
	root.TerminalPool = root.TerminalPool.RequestList()
	root, _ = reducer(root, TerminalPoolListResultMsg{Seq: staleSeq, Result: services.TerminalListResult{Items: []services.TerminalPoolItem{{TerminalID: "stale"}}}})
	if len(root.TerminalPool.Items) != 0 {
		t.Fatalf("stale result must not update pool, got %#v", root.TerminalPool)
	}

	terminal = &services.FakeTerminalService{CreateResult: services.TerminalCreateResult{TerminalID: "term-created", State: "running"}}
	reducer = NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root, effects = reducer(root, TerminalPoolCreateRequestMsg{})
	if len(effects) != 1 {
		t.Fatalf("expected create effect, got %#v", effects)
	}
	createEffect, ok := effects[0].(FuncEffect)
	if !ok {
		t.Fatalf("expected create FuncEffect, got %#v", effects[0])
	}
	createMsg, ok := createEffect.Run(context.Background()).(TerminalPoolCreateResultMsg)
	if !ok {
		t.Fatalf("expected create result message, got %#v", createMsg)
	}
	if len(terminal.Creates) != 1 || len(terminal.Creates[0].Command) == 0 {
		t.Fatalf("terminal pool create must send a default shell command, creates=%#v", terminal.Creates)
	}
	root, effects = reducer(root, TerminalPoolCreateResultMsg{Result: services.TerminalCreateResult{TerminalID: "term-created", State: "running"}})
	if root.TerminalPool.LastCreatedID != "term-created" || len(root.Shell.Toasts) == 0 || root.Shell.Toasts[len(root.Shell.Toasts)-1].Body != "term-created" || len(effects) != 1 {
		t.Fatalf("expected create feedback and refresh effect, got root=%#v effects=%#v", root, effects)
	}
}

func TestTerminalPoolReducerHandlesRestartAndReconnectResults(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 3, Cols: 80, Rows: 24},
	}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{Shell: state.DefaultShell()}

	root, effects := reducer(root, TerminalPoolRestartRequestMsg{TerminalID: "term-1"})
	if len(effects) != 1 {
		t.Fatalf("expected restart effect, got %#v", effects)
	}
	restartEffect, ok := effects[0].(FuncEffect)
	if !ok {
		t.Fatalf("expected restart FuncEffect, got %#v", effects[0])
	}
	restartMsg, ok := restartEffect.Run(context.Background()).(TerminalPoolRestartResultMsg)
	if !ok || restartMsg.TerminalID != "term-1" || len(terminal.Restarts) != 1 {
		t.Fatalf("expected restart service result, msg=%#v restarts=%#v", restartMsg, terminal.Restarts)
	}
	root, effects = reducer(root, restartMsg)
	if len(root.Shell.Toasts) == 0 || root.Shell.Toasts[len(root.Shell.Toasts)-1].Title != "picker.restart" || len(effects) != 1 {
		t.Fatalf("expected restart result feedback and refresh effect, terminal=%#v root=%#v effects=%#v", terminal, root, effects)
	}

	root, effects = reducer(root, TerminalPoolReconnectRequestMsg{TerminalID: "term-1"})
	if len(effects) != 1 {
		t.Fatalf("expected reconnect effect, got %#v", effects)
	}
	reconnectEffect, ok := effects[0].(FuncEffect)
	if !ok {
		t.Fatalf("expected reconnect FuncEffect, got %#v", effects[0])
	}
	reconnectMsg, ok := reconnectEffect.Run(context.Background()).(TerminalPoolReconnectResultMsg)
	if !ok || reconnectMsg.TerminalID != "term-1" || len(terminal.Reconnects) != 1 {
		t.Fatalf("expected reconnect service result, msg=%#v reconnects=%#v", reconnectMsg, terminal.Reconnects)
	}
	root, _ = reducer(root, reconnectMsg)
	if !root.Session.Attached || root.Session.TerminalID != "term-1" || root.TerminalPool.LastAttachedID != "term-1" {
		t.Fatalf("expected reconnect result to attach session, got session=%#v pool=%#v", root.Session, root.TerminalPool)
	}
}

func TestInteractiveRuntimeTerminalPoolPageFlow(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-logs", Channel: 9, Cols: 100, Rows: 30},
		ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{
			TerminalID: "term-shell",
			Title:      "shell",
			State:      "running",
			Cols:       80,
			Rows:       24,
		}, {
			TerminalID: "term-logs",
			Title:      "日志🚀",
			State:      "running",
			CWD:        "/tmp/logs",
			Cols:       100,
			Rows:       30,
			Tags:       map[string]string{"role": "logs"},
		}}},
	}
	host := NewFakeTerminalHost(64)
	host.SetSize(96, 28)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x07", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "p"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "日"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "志"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send pool input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain pool input %#v: %v", event, err)
		}
	}
	if len(terminal.Lists) != 1 || runtime.State().Shell.Overlay.Kind != state.OverlayTerminalPool || runtime.State().Shell.Overlay.Query != "日志" {
		t.Fatalf("expected pool page loaded and queried, lists=%#v shell=%#v", terminal.Lists, runtime.State().Shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("pool query must not leak terminal input, got %#v", terminal.Inputs)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "Terminal Pool") || !frameContains(frame, "▌ 日志🚀") || !frameContains(frame, "role=logs") {
		t.Fatalf("expected terminal pool page frame, got %#v", frame.Lines)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send pool enter attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pool attach: %v", err)
	}
	if len(terminal.Attaches) != 1 || terminal.Attaches[0].TerminalID != "term-logs" || !runtime.State().Session.Attached {
		t.Fatalf("expected pool attach service result, attaches=%#v session=%#v", terminal.Attaches, runtime.State().Session)
	}

	if err := runtime.Post(ShellOpenTerminalPoolMsg{}); err != nil {
		t.Fatalf("post pool open: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pool reopen: %v", err)
	}
	frame = lastFrame(t, host.Frames())
	selectRegion := frameActionHitRegion(t, frame, "pool.select", "")
	if err := host.SendInput(mouseEventAt(selectRegion.Rect)); err != nil {
		t.Fatalf("send pool select click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pool select: %v", err)
	}
	if runtime.State().Shell.Overlay.SelectedIndex != 0 {
		t.Fatalf("expected row click to select first row, got %#v", runtime.State().Shell.Overlay)
	}
	editRegion := frameActionHitRegion(t, lastFrame(t, host.Frames()), "pool.edit", "")
	if err := host.SendInput(mouseEventAt(editRegion.Rect)); err != nil {
		t.Fatalf("send pool edit click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pool edit: %v", err)
	}
	if len(terminal.Edits) != 1 || terminal.Edits[0].TerminalID != "term-shell" {
		t.Fatalf("expected edit metadata service result, edits=%#v", terminal.Edits)
	}
	if terminal.Edits[0].Tags["edited-by"] != "termx-tui-v3" {
		t.Fatalf("expected edit action to populate metadata tags safely, edits=%#v", terminal.Edits)
	}
	killRegion := frameActionHitRegion(t, lastFrame(t, host.Frames()), "pool.kill", "")
	if err := host.SendInput(mouseEventAt(killRegion.Rect)); err != nil {
		t.Fatalf("send pool kill click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pool kill: %v", err)
	}
	if len(terminal.Kills) != 1 || terminal.Kills[0].TerminalID != "term-shell" || runtime.State().TerminalPool.LastKilledID != "term-shell" {
		t.Fatalf("expected kill service result without local lifecycle spoofing, kills=%#v pool=%#v", terminal.Kills, runtime.State().TerminalPool)
	}
}

func TestInteractiveRuntimeWorkbenchTreeOverlayFlow(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-main", Channel: 4, Cols: 80, Rows: 24},
	}
	initialShell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "日志🚀", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	host := NewFakeTerminalHost(32)
	host.SetSize(96, 28)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: initialShell},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-main", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x07", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "w"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "日"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "志"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send tree input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain tree input %#v: %v", event, err)
		}
	}
	if runtime.State().Shell.Overlay.Kind != state.OverlayWorkbenchTree || runtime.State().Shell.Overlay.Query != "日志" {
		t.Fatalf("expected workbench tree queried, shell=%#v", runtime.State().Shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("tree query must not leak terminal input, got %#v", terminal.Inputs)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "Workbench Tree") || !frameContains(frame, "TUI storage projection") || !frameContains(frame, "▌      pane  日志🚀") || !frameContains(frame, "[open]  Open") {
		t.Fatalf("expected workbench tree frame, got %#v", frame.Lines)
	}
	if frame.Cursor.Shape != render.CursorShapeBar {
		t.Fatalf("tree overlay should own search cursor, got %#v", frame.Cursor)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send tree enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain tree enter: %v", err)
	}
	if runtime.State().Shell.ActivePaneID != "pane-2" || runtime.State().Shell.Overlay.Open {
		t.Fatalf("tree enter should focus pane and close overlay, got %#v", runtime.State().Shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("tree enter must not leak terminal input, got %#v", terminal.Inputs)
	}

	if err := runtime.Post(ShellOpenWorkbenchTreeMsg{}); err != nil {
		t.Fatalf("post tree open: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain tree open: %v", err)
	}
	frame = lastFrame(t, host.Frames())
	selectRegion := frameActionHitRegion(t, frame, "workbench.select", "")
	if err := host.SendInput(mouseEventAt(selectRegion.Rect)); err != nil {
		t.Fatalf("send tree select click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain tree select click: %v", err)
	}
	if runtime.State().Shell.Overlay.SelectedIndex != 0 {
		t.Fatalf("expected row click to select first workbench node, got %#v", runtime.State().Shell.Overlay)
	}
	openRegion := frameActionHitRegion(t, lastFrame(t, host.Frames()), "workbench.open", "")
	if err := host.SendInput(mouseEventAt(openRegion.Rect)); err != nil {
		t.Fatalf("send tree open click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain tree open click: %v", err)
	}
	if runtime.State().Shell.Overlay.Open {
		t.Fatalf("workspace open should close tree overlay, got %#v", runtime.State().Shell.Overlay)
	}
	if len(runtime.State().Shell.Toasts) == 0 || runtime.State().Shell.Toasts[len(runtime.State().Shell.Toasts)-1].Title != "workbench.open" {
		t.Fatalf("expected workbench open feedback toast, got %#v", runtime.State().Shell.Toasts)
	}
}

func TestInteractiveRuntimePromptAndHelpOverlayFlow(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-main", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(48)
	host.SetSize(90, 26)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-main", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x07", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: ":"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "重"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "命"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "名"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send prompt input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain prompt input %#v: %v", event, err)
		}
	}
	if runtime.State().Shell.Overlay.Kind != state.OverlayPrompt || runtime.State().Shell.Overlay.Prompt.Value != "重命名" {
		t.Fatalf("expected prompt input captured, shell=%#v", runtime.State().Shell)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "Command Prompt") || !frameContains(frame, "NAME 重命名") || frame.Cursor.Shape != render.CursorShapeBar {
		t.Fatalf("expected prompt frame and cursor, got %#v", frame)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("prompt input must not leak to terminal, got %#v", terminal.Inputs)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x7f"}); err != nil {
		t.Fatalf("send prompt backspace: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send prompt enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain prompt submit: %v", err)
	}
	if runtime.State().Shell.Overlay.Open || len(runtime.State().Shell.Toasts) == 0 || runtime.State().Shell.Toasts[len(runtime.State().Shell.Toasts)-1].Body != "重命" {
		t.Fatalf("expected prompt submit close overlay and toast, shell=%#v", runtime.State().Shell)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x07", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "?"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send help input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain help input %#v: %v", event, err)
		}
	}
	if runtime.State().Shell.Overlay.Kind != state.OverlayHelp {
		t.Fatalf("expected help overlay, shell=%#v", runtime.State().Shell)
	}
	frame = lastFrame(t, host.Frames())
	if !frameContains(frame, "Help") || !frameContains(frame, "available actions") || !frameContains(frame, "Terminal Pool") || !frameContains(frame, "Workbench Tree") {
		t.Fatalf("expected help content, got %#v", frame.Lines)
	}
	closeRegion := frameActionHitRegion(t, frame, "help.close", "")
	if err := host.SendInput(mouseEventAt(closeRegion.Rect)); err != nil {
		t.Fatalf("send help close click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain help close click: %v", err)
	}
	if runtime.State().Shell.Overlay.Open {
		t.Fatalf("help close action should close overlay, got %#v", runtime.State().Shell.Overlay)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("help input must not leak to terminal, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeCtrlVEntersCopyWithoutTerminalInput(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-1",
			78,
			1,
			nil,
		)}},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: core, Rows: 20},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x16", Ctrl: true}); err != nil {
		t.Fatalf("send ctrl-v: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain ctrl-v: %v", err)
	}

	if !runtime.State().CopyMode.Active {
		t.Fatalf("expected copy mode active, got %#v", runtime.State().CopyMode)
	}
	if len(core.LatestRequests) != 1 {
		t.Fatalf("expected authoritative latest request, got %#v", core.LatestRequests)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("ctrl-v must not be sent to terminal, got %#v", terminal.Inputs)
	}
	last := lastFrame(t, host.Frames())
	if !frameContains(last, "copy history empty") {
		t.Fatalf("expected copy empty content in frame, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeShellSemanticActionsReachRenderPath(t *testing.T) {
	host := NewFakeTerminalHost(8)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	for _, msg := range []Msg{
		ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine},
		ShellSetHeaderVisibleMsg{Visible: false},
		ShellSetFooterVisibleMsg{Visible: false},
		ShellAddToastMsg{Toast: state.ToastSpec{ID: "toast-1", Severity: state.ToastWarning, Title: "warn"}},
		ShellCloseCurrentToastMsg{},
		ShellAddToastMsg{Toast: state.ToastSpec{ID: "toast-2", Severity: state.ToastInfo, Title: "notice"}},
		ShellClearToastsMsg{},
	} {
		if err := runtime.Post(msg); err != nil {
			t.Fatalf("post %T: %v", msg, err)
		}
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if runtime.State().Shell.PanelPresentation != state.PanelPresentationSplitLine {
		t.Fatalf("expected split line presentation, got %#v", runtime.State().Shell)
	}
	if runtime.State().Shell.HeaderVisible || runtime.State().Shell.FooterVisible {
		t.Fatalf("expected hidden header/footer, got %#v", runtime.State().Shell)
	}
	if len(runtime.State().Shell.Toasts) != 0 {
		t.Fatalf("expected cleared toasts, got %#v", runtime.State().Shell.Toasts)
	}
	last := lastFrame(t, host.Frames())
	if frameContains(last, " main ") || frameContains(last, " live ") {
		t.Fatalf("hidden header/footer should not render shell bars, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimePaneAndResizeModeKeymapUsesPaneCommandPath(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "v"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "n"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x12", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyRight},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain input %#v: %v", event, err)
		}
	}

	if runtime.State().Shell.InteractionMode != state.InteractionModeResize {
		t.Fatalf("expected resize mode, got %#v", runtime.State().Shell.InteractionMode)
	}
	tab := runtime.State().Shell.Workspace.Tabs[0]
	if len(tab.Panes) != 2 || tab.RootSplit.Direction != state.SplitDirectionVertical {
		t.Fatalf("expected keyboard split through pane command path, got %#v", tab)
	}
	if tab.RootSplit.BiasCells == 0 {
		t.Fatalf("expected resize key to update split bias, got %#v", tab.RootSplit)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("pane/resize shortcuts must not leak to terminal input, got %#v", terminal.Inputs)
	}
	last := lastFrame(t, host.Frames())
	if !frameContains(last, "RESIZE") {
		t.Fatalf("footer should show resize mode, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeActivePaneVisualFeedbackFollowsKeyboardAndMouse(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(32)
	host.SetSize(96, 28)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "v"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send keyboard event %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain keyboard event %#v: %v", event, err)
		}
	}
	keyboardFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != "pane-2" {
		t.Fatalf("keyboard split should activate pane-2, got %#v", runtime.State().Shell)
	}
	assertPaneVisualState(t, keyboardFrame, "pane", render.StyleAccent)
	assertPaneVisualState(t, keyboardFrame, "shell", render.StyleMuted)
	if !frameContains(keyboardFrame, "PANE") || !frameContains(keyboardFrame, "[v] SPLIT") {
		t.Fatalf("footer should reflect keyboard split active pane, got %#v", keyboardFrame.Lines)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "n"}); err != nil {
		t.Fatalf("send keyboard focus: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain keyboard focus: %v", err)
	}
	focusFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != state.DefaultPaneID {
		t.Fatalf("keyboard focus-next should activate default pane, got %#v", runtime.State().Shell)
	}
	assertPaneVisualState(t, focusFrame, "shell", render.StyleAccent)
	assertPaneVisualState(t, focusFrame, "pane", render.StyleMuted)
	if frameContains(focusFrame, "pane.focus-next") || !frameContains(focusFrame, "PANE") {
		t.Fatalf("keyboard focus should update footer without low-value toast, got %#v", focusFrame.Lines)
	}

	paneContent := frameHitRegion(t, focusFrame, render.HitRegionPaneContent, "pane-2")
	if err := host.SendInput(mouseEventAt(paneContent.Rect)); err != nil {
		t.Fatalf("send mouse focus: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain mouse focus: %v", err)
	}
	mouseFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != "pane-2" {
		t.Fatalf("mouse focus should activate pane-2, got %#v", runtime.State().Shell)
	}
	assertPaneVisualState(t, mouseFrame, "pane", render.StyleAccent)
	assertPaneVisualState(t, mouseFrame, "shell", render.StyleMuted)
	if frameContains(mouseFrame, "pane.focus") || !frameContains(mouseFrame, "PANE") {
		t.Fatalf("mouse focus should update footer without low-value toast, got %#v", mouseFrame.Lines)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x12", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyRight},
		{Kind: input.EventKindKey, Key: input.KeyEsc},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "p"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "z"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send post-focus event %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain post-focus event %#v: %v", event, err)
		}
	}
	zoomFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.ZoomedPaneID != "pane-2" {
		t.Fatalf("zoom should keep active pane zoomed, got %#v", runtime.State().Shell)
	}
	if !frameContains(zoomFrame, "PANE") || !frameContains(zoomFrame, "pane.toggle-zoom") {
		t.Fatalf("resize/presentation/zoom should keep visible active feedback, got %#v", zoomFrame.Lines)
	}
	assertPaneVisualState(t, zoomFrame, "pane", render.StyleAccent)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "z"}); err != nil {
		t.Fatalf("send unzoom before close: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain unzoom before close: %v", err)
	}
	unzoomFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.ZoomedPaneID != "" {
		t.Fatalf("unzoom before close should restore split layout, got %#v", runtime.State().Shell)
	}
	assertPaneVisualState(t, unzoomFrame, "pane", render.StyleAccent)

	if err := runtime.Post(ShellClearToastsMsg{}); err != nil {
		t.Fatalf("post clear toasts before close: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain clear toasts before close: %v", err)
	}
	clearFrame := lastFrame(t, host.Frames())
	action := frameHitRegionByAction(t, clearFrame, render.HitRegionPaneAction, "pane.close", "pane-2")
	if err := host.SendInput(mouseEventAt(action.Rect)); err != nil {
		t.Fatalf("send close click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain close click: %v", err)
	}
	closeFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.HasPane(state.PaneCommandTarget{PaneID: "pane-2"}) {
		t.Fatalf("mouse close should remove active pane, got %#v", runtime.State().Shell)
	}
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != state.DefaultPaneID {
		t.Fatalf("close should choose stable next active pane, got %#v", runtime.State().Shell)
	}
	if !frameContains(closeFrame, "pane.close") || !frameContains(closeFrame, "PANE") {
		t.Fatalf("close should update active pane visuals/footer/toast, got %#v", closeFrame.Lines)
	}
	assertPaneVisualState(t, closeFrame, "shell", render.StyleAccent)
}

func TestInteractiveRuntimeUIFrameworkProductizationFlow(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 68, Rows: 18},
	}
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{
			{Window: historyWindowForApp(state.HistoryWindowReplace, "term-1", "tok-1", 35, 7, []state.HistoryRow{{Text: "copy-old", LineID: 20}})},
			{Window: historyWindowForApp(state.HistoryWindowReplace, "term-1", "tok-2", 32, 8, []state.HistoryRow{{Text: "copy-sized", LineID: 30}})},
		},
	}
	host := NewFakeTerminalHost(64)
	host.SetSize(70, 22)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: core, Rows: 20},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 70, Rows: 22}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "v"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "c"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "p"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "b"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x12", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyLeft},
		{Kind: input.EventKindKey, Key: input.KeyEsc},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x07", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "h"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "T"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "t"},
		{Kind: input.EventKindKey, Key: input.KeyEsc},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send product flow input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain product flow input %#v: %v", event, err)
		}
	}

	shell := runtime.State().Shell.EnsureDefaults()
	if shell.InteractionMode != state.InteractionModeNormal {
		t.Fatalf("global esc should return to normal mode, got %#v", shell.InteractionMode)
	}
	if shell.HeaderVisible || shell.FooterVisible {
		t.Fatalf("global mode should hide header/footer, got %#v", shell)
	}
	if len(shell.Toasts) != 0 {
		t.Fatalf("global close/clear toast actions should clear toasts, got %#v", shell.Toasts)
	}
	if shell.PanelPresentation != state.PanelPresentationSplitLine {
		t.Fatalf("pane mode presentation switch should use split-line, got %#v", shell.PanelPresentation)
	}
	tab := shell.Workspace.Tabs[0]
	if len(tab.Panes) != 2 || tab.RootSplit.BiasCells == 0 {
		t.Fatalf("split and resize should update pane tree geometry, got %#v", tab)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("framework shortcuts must not leak to terminal input, got %#v", terminal.Inputs)
	}
	if len(terminal.Resizes) == 0 {
		t.Fatalf("split/header/footer/resize changes should drive content rect terminal resize")
	}
	hiddenFrame := lastFrame(t, host.Frames())
	if len(hiddenFrame.Lines) != 22 {
		t.Fatalf("hidden chrome frame should still fill viewport rows, got %d", len(hiddenFrame.Lines))
	}
	for i, line := range hiddenFrame.Lines {
		if render.DisplayWidth(line) != 70 {
			t.Fatalf("hidden chrome frame row %d width must fill viewport, got %d line=%q", i, render.DisplayWidth(line), line)
		}
	}
	if frameContains(hiddenFrame, " ws:") || frameContains(hiddenFrame, " mode:") {
		t.Fatalf("hidden header/footer must reclaim shell bar rows, got %#v", hiddenFrame.Lines)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseLeft, Row: 999, Col: 999}); err != nil {
		t.Fatalf("send missed mouse: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "q"}); err != nil {
		t.Fatalf("send terminal input after missed mouse: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain missed mouse and terminal input: %v", err)
	}
	if len(terminal.Inputs) != 1 || string(terminal.Inputs[0].Bytes) != "q" {
		t.Fatalf("missed mouse must not steal following terminal input, got %#v", terminal.Inputs)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send copy entry: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain copy entry: %v", err)
	}
	if len(core.LatestRequests) != 1 || core.LatestRequests[0].Cols != 35 {
		t.Fatalf("copy mode should bind to hidden split content cols, got %#v", core.LatestRequests)
	}
	if runtime.State().CopyMode.BoundToken != "tok-1" || runtime.State().CopyMode.BoundCols != 35 {
		t.Fatalf("copy mode should accept first authoritative window, got %#v", runtime.State().CopyMode)
	}

	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:   state.PaneCommandSetSize,
		Target:   state.PaneCommandTarget{PaneID: "pane-2"},
		SizeMode: state.PaneSizeCells,
		Cols:     34,
	}}); err != nil {
		t.Fatalf("post pane size rebind: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane size rebind: %v", err)
	}
	if len(core.LatestRequests) != 2 || core.LatestRequests[1].Cols != 32 {
		t.Fatalf("pane size should rebind copy mode through content rect cols, got %#v", core.LatestRequests)
	}
	if runtime.State().CopyMode.BoundToken != "tok-2" || runtime.State().History.Token != "tok-2" {
		t.Fatalf("copy rebind should replace authoritative window, got copy=%#v history=%#v", runtime.State().CopyMode, runtime.State().History)
	}
	if err := runtime.Post(ShellClearToastsMsg{}); err != nil {
		t.Fatalf("post clear toasts after copy rebind: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain clear toasts after copy rebind: %v", err)
	}
	copyFrame := lastFrame(t, host.Frames())
	if !frameContains(copyFrame, "copy-sized") || frameContains(copyFrame, "copy-old") {
		t.Fatalf("copy rebind should render only the latest authoritative window, got %#v", copyFrame.Lines)
	}
}

func TestInteractiveRuntimeTUIProductShellAcceptanceFlow(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{Channel: 4, Cols: 100, Rows: 30},
		ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{
			TerminalID: "term-shell",
			Title:      "shell",
			State:      "running",
			Cols:       100,
			Rows:       30,
		}, {
			TerminalID: "term-logs",
			Title:      "日志🚀",
			State:      "running",
			CWD:        "/tmp/logs",
			Cols:       120,
			Rows:       40,
			Tags:       map[string]string{"role": "logs"},
		}}},
		CreateResult: services.TerminalCreateResult{TerminalID: "term-created", State: "running"},
	}
	host := NewFakeTerminalHost(160)
	host.SetSize(110, 32)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 100, Rows: 30}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	send := func(event input.InputEvent) {
		t.Helper()
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send acceptance input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain acceptance input %#v: %v", event, err)
		}
	}
	sendKey := func(key input.Key) {
		t.Helper()
		send(input.InputEvent{Kind: input.EventKindKey, Key: key})
	}
	sendChar := func(char string) {
		t.Helper()
		send(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: char})
	}
	sendCtrl := func(char string) {
		t.Helper()
		send(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: char, Ctrl: true})
	}

	sendCtrl("\x10")
	sendChar("v")
	sendChar("n")
	sendCtrl("\x12")
	sendKey(input.KeyRight)
	sendKey(input.KeyEsc)
	sendCtrl("\x10")
	sendChar("p")
	sendChar("b")

	shell := runtime.State().Shell.EnsureDefaults()
	if shell.PanelPresentation != state.PanelPresentationSplitLine {
		t.Fatalf("expected split-line presentation in product shell flow, got %#v", shell.PanelPresentation)
	}
	if tab := shell.Workspace.Tabs[0]; len(tab.Panes) != 2 || tab.RootSplit.Direction != state.SplitDirectionVertical {
		t.Fatalf("expected split/focus/balance to keep a vertical pane tree, got %#v", tab)
	}
	if len(terminal.Resizes) == 0 {
		t.Fatalf("resize mode should drive content rect terminal resize")
	}
	paneFrame := lastFrame(t, host.Frames())
	if !frameContains(paneFrame, "PANE") || !frameContains(paneFrame, "[v] SPLIT") {
		t.Fatalf("expected pane mode footer and active feedback, got %#v", paneFrame.Lines)
	}

	sendCtrl("\x0f")
	sendChar("n")
	sendKey(input.KeyRight)
	sendKey(input.KeyDown)
	sendChar("L")
	sendChar("J")
	sendCtrl("\x07")
	sendChar("t")
	sendKey(input.KeyEsc)
	floatingFrame := lastFrame(t, host.Frames())
	if len(runtime.State().Shell.Floatings) != 1 ||
		!frameContains(floatingFrame, "No terminal attached floating") ||
		!frameContains(floatingFrame, "["+render.DefaultPaneChromeGlyphs().Zoom+"]─["+render.DefaultPaneChromeGlyphs().Close+"]") ||
		frameContains(floatingFrame, render.DefaultPaneChromeGlyphs().Running+" float") {
		t.Fatalf("expected floating pane product shell content, shell=%#v frame=%#v", runtime.State().Shell, floatingFrame.Lines)
	}
	floatingClose := frameActionHitRegion(t, floatingFrame, "floating.close", "floating-1")
	send(mouseEventAt(floatingClose.Rect))
	if len(runtime.State().Shell.Floatings) != 0 {
		t.Fatalf("floating close action should remove floating pane, got %#v", runtime.State().Shell.Floatings)
	}

	sendCtrl("\x07")
	sendChar("p")
	sendChar("日")
	sendChar("志")
	poolFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.Overlay.Kind != state.OverlayTerminalPool || !frameContains(poolFrame, "Terminal Pool") || !frameContains(poolFrame, "日志🚀") {
		t.Fatalf("expected Terminal Pool page in product shell flow, shell=%#v frame=%#v", runtime.State().Shell, poolFrame.Lines)
	}
	sendKey(input.KeyEnter)
	if len(terminal.Attaches) < 2 || runtime.State().Session.TerminalID != "term-logs" {
		t.Fatalf("Terminal Pool attach should use terminal service, attaches=%#v session=%#v", terminal.Attaches, runtime.State().Session)
	}

	sendCtrl("\x07")
	sendChar("w")
	workbenchFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.Overlay.Kind != state.OverlayWorkbenchTree || !frameContains(workbenchFrame, "Workbench Tree") {
		t.Fatalf("expected Workbench Tree overlay, shell=%#v frame=%#v", runtime.State().Shell, workbenchFrame.Lines)
	}
	sendKey(input.KeyEnter)
	if runtime.State().Shell.Overlay.Open {
		t.Fatalf("Workbench Tree enter should close overlay, got %#v", runtime.State().Shell.Overlay)
	}

	sendCtrl("\x07")
	sendChar(":")
	sendChar("重")
	sendChar("命")
	sendKey(input.KeyEnter)
	if runtime.State().Shell.Overlay.Open {
		t.Fatalf("Prompt submit should close overlay, got %#v", runtime.State().Shell.Overlay)
	}
	sendCtrl("\x07")
	sendChar("?")
	helpFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.Overlay.Kind != state.OverlayHelp || !frameContains(helpFrame, "Most used") {
		t.Fatalf("expected Help overlay, shell=%#v frame=%#v", runtime.State().Shell, helpFrame.Lines)
	}
	sendKey(input.KeyEnter)

	sendCtrl("\x14")
	sendChar("n")
	sendChar("r")
	sendChar("构")
	sendKey(input.KeyEnter)
	sendCtrl("\x17")
	sendChar("n")
	sendChar("r")
	sendChar("云")
	sendKey(input.KeyEnter)
	sendKey(input.KeyEsc)
	shell = runtime.State().Shell.EnsureDefaults()
	if shell.Workspace.Name != "workspace 2云" || len(shell.Workspaces) != 2 {
		t.Fatalf("workspace mode should create and rename workspace, got %#v", shell)
	}
	mainWorkspace, ok := findWorkspaceForTest(shell.Workspaces, state.DefaultWorkspaceID)
	if !ok || len(mainWorkspace.Tabs) != 2 || mainWorkspace.Tabs[1].Title != "tab 2构" {
		t.Fatalf("tab mode should create and rename tab in original workspace, ok=%v workspace=%#v", ok, mainWorkspace)
	}
	tabWorkspaceFrame := lastFrame(t, host.Frames())
	if !frameContains(tabWorkspaceFrame, "workspace 2云") || !frameContains(tabWorkspaceFrame, "1 main ") || !frameContains(tabWorkspaceFrame, " ") || !frameContains(tabWorkspaceFrame, "ws:workspace") {
		t.Fatalf("expected live footer/header after tab/workspace flow, got %#v", tabWorkspaceFrame.Lines)
	}

	sendCtrl("\x07")
	sendChar("h")
	sendChar("f")
	sendChar("T")
	sendChar("t")
	sendKey(input.KeyEsc)
	shell = runtime.State().Shell.EnsureDefaults()
	if shell.HeaderVisible || shell.FooterVisible || len(shell.Toasts) != 0 || shell.InteractionMode != state.InteractionModeNormal {
		t.Fatalf("global mode should hide chrome, clear toasts and return normal, got %#v", shell)
	}
	finalFrame := lastFrame(t, host.Frames())
	if len(finalFrame.Lines) != 32 {
		t.Fatalf("final frame must fill viewport rows, got %d", len(finalFrame.Lines))
	}
	for row, line := range finalFrame.Lines {
		if width := render.DisplayWidth(line); width != 110 {
			t.Fatalf("final frame row %d width=%d want=110 line=%q", row, width, line)
		}
	}
	if frameContains(finalFrame, " ws:") || frameContains(finalFrame, " mode:") {
		t.Fatalf("hidden header/footer must reclaim shell rows, got %#v", finalFrame.Lines)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("product shell UI operations must not leak to terminal input, got %#v", terminal.Inputs)
	}
	if len(terminal.Resizes) == 0 {
		t.Fatalf("product shell layout operations should drive content rect resize")
	}
}

func assertPaneVisualState(t *testing.T, frame render.Frame, text string, style render.StyleToken) {
	t.Helper()
	for _, line := range frame.StyledLines {
		var span strings.Builder
		flush := func() bool {
			if strings.Contains(span.String(), text) {
				return true
			}
			span.Reset()
			return false
		}
		for _, cell := range line.Cells {
			if cell.Style == style {
				span.WriteString(cell.Text)
				continue
			}
			if flush() {
				return
			}
		}
		if flush() {
			return
		}
	}
	t.Fatalf("expected styled pane text %q with style %s, got %#v", text, style, frame.StyledLines)
}

func TestInteractiveRuntimeGlobalModeTogglesChromeAndEscExitsMode(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x07", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "h"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"},
		{Kind: input.EventKindKey, Key: input.KeyEsc},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain input %#v: %v", event, err)
		}
	}

	if runtime.State().Shell.InteractionMode != state.InteractionModeNormal {
		t.Fatalf("expected normal mode after esc, got %#v", runtime.State().Shell.InteractionMode)
	}
	if runtime.State().Shell.HeaderVisible || runtime.State().Shell.FooterVisible {
		t.Fatalf("expected global mode toggles to hide header/footer, got %#v", runtime.State().Shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("global shortcuts must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeFloatingPaneProductFlow(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(32)
	host.SetSize(90, 28)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x0f", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "n"},
		{Kind: input.EventKindKey, Key: input.KeyRight},
		{Kind: input.EventKindKey, Key: input.KeyDown},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "L"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "J"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "z"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "z"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send floating input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain floating input %#v: %v", event, err)
		}
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if len(shell.Floatings) != 1 || !shell.Floatings[0].Active || shell.Floatings[0].Collapsed {
		t.Fatalf("expected active restored floating, got %#v", shell.Floatings)
	}
	floatingRect := shell.Floatings[0].Rect
	frameAfterFloating := lastFrame(t, host.Frames())
	if frameAfterFloating.CursorRect.X < floatingRect.X+1 || frameAfterFloating.CursorRect.X >= floatingRect.X+floatingRect.W-1 ||
		frameAfterFloating.CursorRect.Y < floatingRect.Y+1 || frameAfterFloating.CursorRect.Y >= floatingRect.Y+floatingRect.H-1 {
		t.Fatalf("floating input should anchor hidden host cursor inside floating content for IME, floating=%#v cursor=%#v frame=%#v", floatingRect, frameAfterFloating.CursorRect, frameAfterFloating.Cursor)
	}
	vmAfterFloating := render.NewRenderVMBuilder().Build(runtime.State())
	if len(vmAfterFloating.Shell.Layout.Panels) == 0 || vmAfterFloating.Shell.Layout.Panels[0].Active {
		t.Fatalf("active floating should dim tiled pane visual active state, panels=%#v floating=%#v", vmAfterFloating.Shell.Layout.Panels, vmAfterFloating.Shell.Layout.Floating)
	}
	if shell.Floatings[0].Rect.W <= 44 || shell.Floatings[0].Rect.H <= 12 {
		t.Fatalf("expected keyboard resize to grow floating rect, got %#v", shell.Floatings[0].Rect)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("floating shortcuts must not leak terminal input, got %#v", terminal.Inputs)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x07", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "t"},
		{Kind: input.EventKindKey, Key: input.KeyEsc},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send clear toast input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain clear toast input %#v: %v", event, err)
		}
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "floating") || !frameContains(frame, "No terminal attached floating") || !frameContains(frame, "Attach existing") {
		t.Fatalf("expected rendered floating pane, got %#v", frame.Lines)
	}

	raiseRegion := frameActionHitRegion(t, frame, "floating.raise", "floating-1")
	if err := host.SendInput(mouseEventAt(raiseRegion.Rect)); err != nil {
		t.Fatalf("send floating raise click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating raise: %v", err)
	}
	moveRegion := frameActionHitRegion(t, lastFrame(t, host.Frames()), "floating.move-drag", "floating-1")
	beforeMove := runtime.State().Shell.Floatings[0].Rect
	moveStart := mouseEventAt(moveRegion.Rect)
	moveDrag := moveStart
	moveDrag.Mouse = input.MouseLeftDrag
	moveDrag.Col += 3
	moveDrag.Row += 2
	moveRelease := moveDrag
	moveRelease.Mouse = input.MouseLeftUp
	for _, event := range []input.InputEvent{moveStart, moveDrag, moveRelease} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send floating move event %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain floating move event %#v: %v", event, err)
		}
	}
	afterMove := runtime.State().Shell.Floatings[0].Rect
	if afterMove.X != beforeMove.X+3 || afterMove.Y != beforeMove.Y+2 {
		t.Fatalf("mouse move should move floating rect, before=%#v after=%#v", beforeMove, afterMove)
	}
	resizeRegion := frameActionHitRegion(t, lastFrame(t, host.Frames()), "floating.resize-drag", "floating-1")
	before := runtime.State().Shell.Floatings[0].Rect
	resizeStart := mouseEventAt(resizeRegion.Rect)
	resizeDrag := resizeStart
	resizeDrag.Mouse = input.MouseLeftDrag
	resizeDrag.Col += 4
	resizeDrag.Row += 2
	resizeRelease := resizeDrag
	resizeRelease.Mouse = input.MouseLeftUp
	for _, event := range []input.InputEvent{resizeStart, resizeDrag, resizeRelease} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send floating resize event %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain floating resize event %#v: %v", event, err)
		}
	}
	after := runtime.State().Shell.Floatings[0].Rect
	if after.W <= before.W || after.H <= before.H {
		t.Fatalf("mouse resize should grow floating rect, before=%#v after=%#v", before, after)
	}
	if err := runtime.Post(ShellClearToastsMsg{}); err != nil {
		t.Fatalf("post clear toasts before floating close: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain clear toasts before floating close: %v", err)
	}
	closeRegion := frameActionHitRegion(t, lastFrame(t, host.Frames()), "floating.close", "floating-1")
	if err := host.SendInput(mouseEventAt(closeRegion.Rect)); err != nil {
		t.Fatalf("send floating close click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating close: %v", err)
	}
	if len(runtime.State().Shell.Floatings) != 0 {
		t.Fatalf("mouse close should remove floating pane, got %#v", runtime.State().Shell.Floatings)
	}
	vmAfterClose := render.NewRenderVMBuilder().Build(runtime.State())
	if len(vmAfterClose.Shell.Layout.Panels) == 0 || !vmAfterClose.Shell.Layout.Panels[0].Active {
		t.Fatalf("tiled pane visual active state should restore after floating closes, panels=%#v", vmAfterClose.Shell.Layout.Panels)
	}
}

func TestInteractiveRuntimeTabAndWorkspaceProductFlow(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(40)
	host.SetSize(90, 26)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x14", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "n"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "r"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "构"},
		{Kind: input.EventKindKey, Key: input.KeyEnter},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x14", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "h"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x17", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "n"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "r"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "云"},
		{Kind: input.EventKindKey, Key: input.KeyEnter},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain input %#v: %v", event, err)
		}
	}
	workspaceModeFrame := lastFrame(t, host.Frames())
	if !frameContains(workspaceModeFrame, "workspace 2云") || !frameContains(workspaceModeFrame, "WORKSPACE") {
		t.Fatalf("expected frame to expose workspace mode and active workspace, got %#v", workspaceModeFrame.Lines)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEsc}); err != nil {
		t.Fatalf("send workspace esc: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain workspace esc: %v", err)
	}

	shell := runtime.State().Shell.EnsureDefaults()
	if shell.InteractionMode != state.InteractionModeNormal {
		t.Fatalf("expected normal mode after esc, got %#v", shell.InteractionMode)
	}
	if len(shell.Workspaces) != 2 || shell.Workspace.Name != "workspace 2云" {
		t.Fatalf("expected created and renamed workspace active, got %#v", shell)
	}
	if len(shell.Workspace.Tabs) != 1 {
		t.Fatalf("new workspace should start with one tab, got %#v", shell.Workspace.Tabs)
	}
	mainWorkspace, ok := findWorkspaceForTest(shell.Workspaces, state.DefaultWorkspaceID)
	if !ok || len(mainWorkspace.Tabs) != 2 || mainWorkspace.ActiveTabID != state.DefaultTabID {
		t.Fatalf("expected original workspace to retain two tabs and previous active tab, got ok=%v workspace=%#v all=%#v", ok, mainWorkspace, shell.Workspaces)
	}
	if mainWorkspace.Tabs[1].Title != "tab 2构" {
		t.Fatalf("expected renamed tab in original workspace, got %#v", mainWorkspace.Tabs)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("tab/workspace shortcuts must not leak to terminal input, got %#v", terminal.Inputs)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "workspace 2云") || !frameContains(frame, "1 main ") || !frameContains(frame, " ") || !frameContains(frame, "ws:workspace") {
		t.Fatalf("expected frame to return to live mode and keep active workspace, got %#v", frame.Lines)
	}
}

func TestShellReducerHandlesFloatingContentActions(t *testing.T) {
	shell := state.DefaultShell()
	shell, _ = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "floating", Kind: state.PaneEmpty},
		Rect:     state.FloatingRect{X: 2, Y: 2, W: 30, H: 8},
		BoundsW:  80,
		BoundsH:  24,
	})
	reducer := NewShellReducer()
	root, _ := reducer(state.Root{
		Shell:    shell,
		Viewport: state.ViewportStore{Valid: true, Cols: 80, Rows: 24},
	}, ShellContentActionMsg{ActionID: "floating.resize", PaneID: "floating-1"})
	if got := root.Shell.Floatings[0].Rect; got.W != 32 || got.H != 9 {
		t.Fatalf("floating resize action should update rect, got %#v", got)
	}
	root, _ = reducer(root, ShellContentActionMsg{ActionID: "floating.close", PaneID: "floating-1"})
	if len(root.Shell.Floatings) != 0 {
		t.Fatalf("floating close action should remove floating, got %#v", root.Shell.Floatings)
	}
}

func TestInteractiveRuntimeTabJumpUsesWorkbenchCommand(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(24)
	host.SetSize(90, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x14", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "n"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "n"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "1"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "3"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "9"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send tab jump input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain tab jump input %#v: %v", event, err)
		}
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if shell.Workspace.ActiveTabID != "tab-3" {
		t.Fatalf("tab jump should keep valid tab active after out-of-range jump, got %#v", shell.Workspace)
	}
	if len(shell.Toasts) == 0 || shell.Toasts[len(shell.Toasts)-1].Body != "tab not found" {
		t.Fatalf("out-of-range tab jump should show workbench invalid feedback, got %#v", shell.Toasts)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("tab jump shortcuts must not leak terminal input, got %#v", terminal.Inputs)
	}
}

func findWorkspaceForTest(workspaces []state.WorkspaceState, id string) (state.WorkspaceState, bool) {
	for _, workspace := range workspaces {
		if workspace.ID == id {
			return workspace, true
		}
	}
	return state.WorkspaceState{}, false
}
