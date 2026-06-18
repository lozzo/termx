package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("terminal picker is an overlay and must not arm sticky timeout, got %#v", effects)
	}
}

func TestUIInputReducerStickyInteractionModeTimeoutAndRearm(t *testing.T) {
	reducer := NewUIInputReducer()
	root := state.Root{Shell: state.DefaultShell()}

	root, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true}})
	if root.Shell.InteractionMode != state.InteractionModePane || !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("ctrl-p should enter pane mode and arm timeout, shell=%#v effects=%#v", root.Shell, effects)
	}
	paneSeq := root.Shell.InteractionModeSeq

	root, _ = reducer(root, ShellInteractionModeTimeoutMsg{Mode: state.InteractionModePane, Seq: paneSeq})
	if root.Shell.InteractionMode != state.InteractionModeNormal {
		t.Fatalf("matching timeout should exit sticky mode, shell=%#v", root.Shell)
	}

	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x12", Ctrl: true}})
	if root.Shell.InteractionMode != state.InteractionModeResize || !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("ctrl-r should enter resize mode and arm timeout, shell=%#v effects=%#v", root.Shell, effects)
	}
	oldSeq := root.Shell.InteractionModeSeq
	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyRight}})
	if root.Shell.InteractionMode != state.InteractionModeResize || root.Shell.InteractionModeSeq <= oldSeq || !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("resize action should rearm sticky timeout, shell=%#v effects=%#v oldSeq=%d", root.Shell, effects, oldSeq)
	}

	root, _ = reducer(root, ShellInteractionModeTimeoutMsg{Mode: state.InteractionModeResize, Seq: oldSeq})
	if root.Shell.InteractionMode != state.InteractionModeResize {
		t.Fatalf("stale timeout must not exit renewed mode, shell=%#v", root.Shell)
	}
	root, _ = reducer(root, ShellInteractionModeTimeoutMsg{Mode: state.InteractionModeResize, Seq: root.Shell.InteractionModeSeq})
	if root.Shell.InteractionMode != state.InteractionModeNormal {
		t.Fatalf("latest timeout should exit renewed mode, shell=%#v", root.Shell)
	}
}

func TestUIInputReducerStickyTimeoutDoesNotCloseOverlayOrCopyMode(t *testing.T) {
	reducer := NewUIInputReducer()
	shell := state.DefaultShell().SetInteractionMode(state.InteractionModeGlobal).OpenTerminalPicker()
	root := state.Root{
		Shell:    shell,
		CopyMode: state.CopyModeStore{Active: true, TerminalID: "term-1"},
	}
	seq := root.Shell.InteractionModeSeq

	root, _ = reducer(root, ShellInteractionModeTimeoutMsg{Mode: state.InteractionModeGlobal, Seq: seq})
	if root.Shell.InteractionMode != state.InteractionModeNormal {
		t.Fatalf("timeout should clear only sticky interaction mode, shell=%#v", root.Shell)
	}
	if !root.Shell.Overlay.Open || root.Shell.Overlay.Kind != state.OverlayTerminalPicker {
		t.Fatalf("timeout must not close overlay, overlay=%#v", root.Shell.Overlay)
	}
	if !root.CopyMode.Active || root.CopyMode.TerminalID != "term-1" {
		t.Fatalf("timeout must not mutate copy mode, copy=%#v", root.CopyMode)
	}
}

func hasStickyInteractionModeTimeoutEffect(effects []Effect) bool {
	for _, effect := range effects {
		if fn, ok := effect.(FuncEffect); ok && fn.Token == stickyInteractionModeTimeoutToken {
			return true
		}
	}
	return false
}

func runFirstNonStickyTimeoutEffect(t *testing.T, effects []Effect) Msg {
	t.Helper()
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if !ok || fn.Run == nil || fn.Token == stickyInteractionModeTimeoutToken {
			continue
		}
		return fn.Run(context.Background())
	}
	t.Fatalf("expected non-timeout function effect, got %#v", effects)
	return nil
}

func TestUIInputReducerPaneModeTuiv2OwnerPickerAndRestart(t *testing.T) {
	reducer := NewUIInputReducer()
	shell := state.DefaultShell()
	shell.ActivePaneID = state.DefaultPaneID
	root := state.Root{Shell: shell}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 3, 80, 24, state.TerminalResizeRoleFollower, "surface-1", "", false))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 4, 80, 24, state.TerminalResizeRoleOwner, "surface-1", "", true))

	root, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true}})
	if root.Shell.InteractionMode != state.InteractionModePane || !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("ctrl-p should enter pane mode, mode=%#v effects=%#v", root.Shell.InteractionMode, effects)
	}
	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "a"}})
	if owner, ok := root.TerminalViews.OwnerBinding("term-1"); !ok || owner.PaneID != "pane-2" {
		t.Fatalf("pane owner shortcut must keep current owner until service result, owner=%#v ok=%v", owner, ok)
	}
	ownerMsg, ok := runFirstNonStickyTimeoutEffect(t, effects).(LiveAttachMsg)
	if !ok || ownerMsg.Config.TerminalID != "term-1" || ownerMsg.Config.ViewID != state.TerminalPaneViewID(state.DefaultPaneID) || ownerMsg.Config.ResizePolicy != state.TerminalResizeRoleOwner {
		t.Fatalf("pane owner shortcut should request authoritative owner attach, got %#v", ownerMsg)
	}

	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "r"}})
	if !root.Shell.Overlay.Open || root.Shell.Overlay.Kind != state.OverlayTerminalPicker || !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("pane reconnect should open picker and refresh pool, overlay=%#v effects=%#v", root.Shell.Overlay, effects)
	}
	if _, ok := runFirstNonStickyTimeoutEffect(t, effects).(TerminalPoolListRequestMsg); !ok {
		t.Fatalf("pane reconnect should emit pool list effect, got %#v", effects)
	}

	root.Shell = root.Shell.CloseOverlay().SetInteractionMode(state.InteractionModePane)
	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "R"}})
	if !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("pane restart should emit handled and restart effects, got %#v", effects)
	}
	restartMsg, ok := runFirstNonStickyTimeoutEffect(t, effects).(TerminalPoolRestartIfExitedRequestMsg)
	if !ok || restartMsg.TerminalID != "term-1" {
		t.Fatalf("pane restart should query active pane terminal lifecycle, got %#v", restartMsg)
	}
}

func TestUIInputReducerFloatingModeTuiv2PickerAndOwner(t *testing.T) {
	reducer := NewUIInputReducer()
	shell := state.DefaultShell()
	shell, _ = shell.ApplyFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandCreate, TargetID: "float-1", Title: "float", Pane: state.PaneState{ID: "float-pane", Title: "float", Kind: state.PaneTerminalLive, TerminalID: "term-1"}})
	root := state.Root{Shell: shell}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 3, 80, 24, state.TerminalResizeRoleOwner, "surface-1", "", true))
	root.TerminalViews = root.TerminalViews.BindFloating(state.NewFloatingTerminalView("float-1", "", "term-1", 5, 80, 24, state.TerminalResizeRoleFollower, "surface-1", "", false))

	root, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x0f", Ctrl: true}})
	if root.Shell.InteractionMode != state.InteractionModeFloating || !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("ctrl-o should enter floating mode, mode=%#v effects=%#v", root.Shell.InteractionMode, effects)
	}
	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"}})
	if !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("floating picker shortcut should emit handled and picker effects, got %#v", effects)
	}
	if msg, ok := runFirstNonStickyTimeoutEffect(t, effects).(ShellOpenTerminalPickerMsg); !ok {
		t.Fatalf("floating picker shortcut should request picker open, got %#v", msg)
	}

	root.Shell = root.Shell.CloseOverlay().SetInteractionMode(state.InteractionModeFloating)
	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "a"}})
	if owner, ok := root.TerminalViews.OwnerBinding("term-1"); !ok || owner.PaneID != state.DefaultPaneID {
		t.Fatalf("floating owner shortcut must keep current owner until service result, owner=%#v ok=%v", owner, ok)
	}
	msg, ok := runFirstNonStickyTimeoutEffect(t, effects).(LiveAttachMsg)
	if !ok || msg.Config.TerminalID != "term-1" || msg.Config.ViewID != state.TerminalFloatingViewID("float-1") || msg.Config.ResizePolicy != state.TerminalResizeRoleOwner {
		t.Fatalf("floating owner shortcut should request authoritative owner attach, got %#v", msg)
	}
}

func TestUIInputReducerEmptyPaneCTAKeyboardSelectionAndEnter(t *testing.T) {
	reducer := NewUIInputReducer()
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0] = state.PaneState{ID: state.DefaultPaneID, Title: "slot", Kind: state.PaneEmpty, Active: true}
	root := state.Root{Shell: shell}

	root, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}})
	if root.Shell.EmptyPaneCTA.SelectedIndex != 1 {
		t.Fatalf("down should select create CTA, got %#v effects=%#v", root.Shell.EmptyPaneCTA, effects)
	}
	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}})
	if root.Shell.EmptyPaneCTA.SelectedIndex != 2 {
		t.Fatalf("second down should select manager CTA, got %#v effects=%#v", root.Shell.EmptyPaneCTA, effects)
	}
	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}})
	if root.Shell.EmptyPaneCTA.SelectedIndex != 2 || len(effects) != 2 {
		t.Fatalf("enter should keep selection and emit handled action effects, root=%#v effects=%#v", root.Shell.EmptyPaneCTA, effects)
	}
	effect, ok := effects[1].(FuncEffect)
	if !ok || effect.Run == nil {
		t.Fatalf("enter should emit content action effect, got %#v", effects)
	}
	msg, ok := effect.Run(context.Background()).(ShellContentActionMsg)
	if !ok || msg.ActionID != render.ActionEmptyManager.String() || msg.PaneID != state.DefaultPaneID {
		t.Fatalf("enter should execute selected manager CTA, got %#v", msg)
	}
}

func TestUIInputReducerFloatingEmptyPaneCTAKeyboardTargetsFloatingPanel(t *testing.T) {
	reducer := NewUIInputReducer()
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0] = state.PaneState{ID: state.DefaultPaneID, Title: "tiled", Kind: state.PaneEmpty, Active: true}
	shell, _ = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "float", Kind: state.PaneEmpty},
	})
	root := state.Root{Shell: shell}

	root, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}})
	if root.Shell.EmptyPaneCTA.SelectedIndex != 1 || len(effects) != 1 {
		t.Fatalf("down should select floating create CTA, shell=%#v effects=%#v", root.Shell, effects)
	}
	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}})
	if len(effects) != 2 {
		t.Fatalf("enter should emit handled floating action effects, effects=%#v", effects)
	}
	effect, ok := effects[1].(FuncEffect)
	if !ok || effect.Run == nil {
		t.Fatalf("enter should emit content action effect, got %#v", effects)
	}
	msg, ok := effect.Run(context.Background()).(ShellContentActionMsg)
	if !ok || msg.ActionID != render.ActionEmptyCreate.String() || msg.PaneID != "floating-pane-1" || !msg.Floating {
		t.Fatalf("enter should execute selected floating CTA, got %#v", msg)
	}
}

func TestUIInputReducerExitedPaneCTAKeyboardSelectionAndEnter(t *testing.T) {
	reducer := NewUIInputReducer()
	root := state.Root{
		Shell:   state.DefaultShell(),
		Surface: state.TerminalSurfaceStore{TerminalID: "term-exited", State: state.TerminalLiveExited, ExitCode: 23},
		TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
			state.DefaultPaneID, "term-exited", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true,
		)),
	}

	root, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}})
	if root.Shell.ExitedPaneCTA.SelectedIndex != 1 || len(effects) != 1 {
		t.Fatalf("down should select picker CTA, shell=%#v effects=%#v", root.Shell.ExitedPaneCTA, effects)
	}
	root, effects = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}})
	if root.Shell.ExitedPaneCTA.SelectedIndex != 1 || len(effects) != 2 {
		t.Fatalf("enter should keep exited selection and emit action effects, root=%#v effects=%#v", root.Shell.ExitedPaneCTA, effects)
	}
	effect, ok := effects[1].(FuncEffect)
	if !ok || effect.Run == nil {
		t.Fatalf("enter should emit content action effect, got %#v", effects)
	}
	msg, ok := effect.Run(context.Background()).(ShellContentActionMsg)
	if !ok || msg.ActionID != render.ActionExitedReconnect.String() || msg.PaneID != state.DefaultPaneID {
		t.Fatalf("enter should execute selected exited picker CTA, got %#v", msg)
	}
}

func TestUIInputReducerExitedPaneCTAKeyboardRestartDefault(t *testing.T) {
	reducer := NewUIInputReducer()
	root := state.Root{
		Shell:   state.DefaultShell(),
		Surface: state.TerminalSurfaceStore{TerminalID: "term-exited", State: state.TerminalLiveExited, ExitCode: 23},
		TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
			state.DefaultPaneID, "term-exited", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true,
		)),
	}

	_, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}})
	if len(effects) != 2 {
		t.Fatalf("enter should emit handled restart action effects, got %#v", effects)
	}
	msg, ok := effects[1].(FuncEffect).Run(context.Background()).(ShellContentActionMsg)
	if !ok || msg.ActionID != render.ActionExitedRestart.String() || msg.PaneID != state.DefaultPaneID {
		t.Fatalf("enter should execute default restart CTA, got %#v", msg)
	}
}

func TestUIInputReducerExitedCacheRestartQueriesCoreLifecycle(t *testing.T) {
	reducer := NewUIInputReducer()
	root := state.Root{
		Shell:   state.DefaultShell(),
		Surface: state.TerminalSurfaceStore{TerminalID: "term-live", State: state.TerminalLiveExited, ExitCode: 23},
		TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
			state.DefaultPaneID, "term-live", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true,
		)),
	}

	_, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}})
	if len(effects) != 2 {
		t.Fatalf("exited CTA enter should be handled and schedule core lifecycle query, got %#v", effects)
	}
	msg, ok := effects[1].(FuncEffect).Run(context.Background()).(ShellContentActionMsg)
	if !ok || msg.ActionID != render.ActionExitedRestart.String() || msg.PaneID != state.DefaultPaneID {
		t.Fatalf("enter should still route through exited restart action, got %#v", msg)
	}
	next, effects := NewShellReducer()(root, msg)
	if len(effects) != 1 {
		t.Fatalf("restart action should schedule a restart-if-exited core query, root=%#v effects=%#v", next, effects)
	}
	query, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolRestartIfExitedRequestMsg)
	if !ok || query.TerminalID != "term-live" {
		t.Fatalf("restart action must query core lifecycle before restart, got %#v", query)
	}
}

func TestUIInputReducerTerminalPickerDeleteKeysTrimQuery(t *testing.T) {
	reducer := NewUIInputReducer()
	root := state.Root{Shell: state.DefaultShell().OpenTerminalPicker().SetTerminalPickerQuery("日志")}

	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyBackspace}})
	if got := root.Shell.EnsureDefaults().Overlay.Query; got != "日" {
		t.Fatalf("backspace should trim picker query, got %q", got)
	}
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDelete}})
	if got := root.Shell.EnsureDefaults().Overlay.Query; got != "" {
		t.Fatalf("delete should trim picker query, got %q", got)
	}
	root = state.Root{Shell: root.Shell.SetTerminalPickerQuery("x")}
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x7f"}})
	if got := root.Shell.EnsureDefaults().Overlay.Query; got != "" {
		t.Fatalf("DEL char should trim picker query, got %q", got)
	}
}

func TestUIInputReducerOpensClipboardHistoryFromCopyModeH(t *testing.T) {
	reducer := NewCopyModeReducer(CopyModeDeps{Core: &services.FakeCoreClient{}, Clipboard: &services.FakeClipboardService{}, Terminal: &services.FakeTerminalService{}, Rows: 20})
	root := state.Root{
		CopyMode: state.CopyModeStore{Active: true},
		Clipboard: state.ClipboardStore{
			Entries: []state.ClipboardEntry{{ID: "clip:1", Title: "alpha", Text: "alpha", Preview: "alpha"}},
		},
	}

	next, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "H"}})
	if !next.Shell.Overlay.Open || next.Shell.Overlay.Kind != state.OverlayClipboardHistory {
		t.Fatalf("expected clipboard history overlay, got %#v", next.Shell.Overlay)
	}
	if len(effects) != 2 {
		t.Fatalf("expected handled and storage load effects, got %#v", effects)
	}
	if _, ok := effects[1].(FuncEffect); !ok {
		t.Fatalf("expected storage load effect, got %#v", effects)
	}
}

func TestOverlayKeyboardCommandsRouteClipboardHistoryContentActions(t *testing.T) {
	inputReducer := NewUIInputReducer()
	root := state.Root{
		Shell: state.DefaultShell().OpenClipboardHistory(),
		Clipboard: state.ClipboardStore{
			Entries: []state.ClipboardEntry{{ID: "clip:1", Title: "alpha", Text: "alpha", Preview: "alpha"}},
		},
	}

	_, effects := inputReducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x05", Ctrl: true}})
	assertContentActionEffect(t, effects, render.ActionClipboardHistoryEdit)

	_, effects = inputReducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x0e", Ctrl: true}})
	assertContentActionEffect(t, effects, render.ActionClipboardHistoryNew)

	_, effects = inputReducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x18", Ctrl: true}})
	assertContentActionEffect(t, effects, render.ActionClipboardHistoryDelete)
}

func TestOverlayMouseWheelMovesCurrentSelection(t *testing.T) {
	inputReducer := NewUIInputReducer()
	tests := []struct {
		name     string
		root     state.Root
		wantKind state.OverlayKind
	}{
		{
			name: "terminal picker",
			root: state.Root{
				Shell:        state.DefaultShell().OpenTerminalPicker(),
				TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{{TerminalID: "term-a", Title: "a"}, {TerminalID: "term-b", Title: "b"}}},
			},
			wantKind: state.OverlayTerminalPicker,
		},
		{
			name: "terminal pool",
			root: state.Root{
				Shell:        state.DefaultShell().OpenTerminalPool(),
				TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{{TerminalID: "term-a", Title: "a"}, {TerminalID: "term-b", Title: "b"}}},
			},
			wantKind: state.OverlayTerminalPool,
		},
		{
			name: "workbench tree",
			root: state.Root{
				Shell: state.DefaultShell().
					SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneEmpty}, state.SplitDirectionVertical).
					OpenWorkbenchTree(),
			},
			wantKind: state.OverlayWorkbenchTree,
		},
		{
			name: "clipboard history",
			root: state.Root{
				Shell: state.DefaultShell().OpenClipboardHistory(),
				Clipboard: state.ClipboardStore{
					Entries: []state.ClipboardEntry{
						{ID: "clip:1", Title: "one", Text: "one", Preview: "one"},
						{ID: "clip:2", Title: "two", Text: "two", Preview: "two"},
					},
				},
			},
			wantKind: state.OverlayClipboardHistory,
		},
		{
			name: "floating overview",
			root: state.Root{
				Shell: shellWithFloatingOverviewForMouseWheelTest(),
			},
			wantKind: state.OverlayFloatingOverview,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			next, effects := inputReducer(tc.root, ShellOverlayMouseSelectMsg{Delta: 1})
			if next.Shell.Overlay.Kind != tc.wantKind || next.Shell.Overlay.SelectedIndex != 1 {
				t.Fatalf("wheel down should move overlay selection, overlay=%#v", next.Shell.Overlay)
			}
			if len(effects) != 1 {
				t.Fatalf("overlay wheel should be handled locally, effects=%#v", effects)
			}
			if _, ok := effects[0].(handledEffect); !ok {
				t.Fatalf("overlay wheel should emit handled effect, got %#v", effects[0])
			}
			next, effects = inputReducer(next, ShellOverlayMouseSelectMsg{Delta: -1})
			if next.Shell.Overlay.SelectedIndex != 0 {
				t.Fatalf("wheel up should move overlay selection back, overlay=%#v effects=%#v", next.Shell.Overlay, effects)
			}
		})
	}
}

func shellWithFloatingOverviewForMouseWheelTest() state.ShellStore {
	shell := state.DefaultShell()
	shell, _ = shell.ApplyFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandCreate, TargetID: "floating-1", Title: "one", Pane: state.PaneState{ID: "floating-pane-1", Title: "one", Kind: state.PaneEmpty}})
	shell, _ = shell.ApplyFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandCreate, TargetID: "floating-2", Title: "two", Pane: state.PaneState{ID: "floating-pane-2", Title: "two", Kind: state.PaneEmpty}})
	return shell.OpenFloatingOverview()
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
	terminal.AttachResult = services.TerminalAttachResult{Channel: 9, Cols: 80, Rows: 24}
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
		!frameContains(last, "▸ + new terminal") ||
		!frameContains(last, "● term-1") ||
		!frameContains(last, "attached") ||
		!frameContains(last, "80x24") ||
		!frameContains(last, "+ new terminal") ||
		!frameContains(last, "Create terminal") ||
		frameContains(last, "@pane-main") ||
		frameContains(last, "Select terminal source state target") ||
		frameContains(last, "DETAIL") {
		t.Fatalf("expected terminal picker product content in frame, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeOverlayMouseWheelMovesSelectionWithoutTerminalLeak(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-main", Channel: 4, Cols: 80, Rows: 24},
		ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{
			{TerminalID: "term-a", Title: "alpha", State: "running"},
			{TerminalID: "term-b", Title: "beta", State: "running"},
		}},
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
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-main", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := runtime.Post(ShellOpenTerminalPoolMsg{}); err != nil {
		t.Fatalf("post terminal pool: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain terminal pool: %v", err)
	}
	overlay := frameHitRegion(t, lastFrame(t, host.Frames()), render.HitRegionOverlay, "")
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseWheelDown, Row: overlay.Rect.Y + 1, Col: overlay.Rect.X + 1}); err != nil {
		t.Fatalf("send overlay wheel: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain overlay wheel: %v", err)
	}
	if runtime.State().Shell.Overlay.SelectedIndex != 1 {
		t.Fatalf("overlay wheel should move selection, overlay=%#v", runtime.State().Shell.Overlay)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("overlay wheel must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeTerminalPickerKeyboardFlow(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-main", Channel: 4, Cols: 80, Rows: 24},
		ListResult:   services.TerminalListResult{Items: []services.TerminalPoolItem{{TerminalID: "term-2", Title: "日志🚀", State: "running", Cols: 100, Rows: 30}}},
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
	terminal.AttachResult = services.TerminalAttachResult{Channel: 9, Cols: 80, Rows: 24}
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
	if !frameContains(queryFrame, "search: 日志") || !frameContains(queryFrame, "▸ ● 日志🚀") || !frameContains(queryFrame, "running") || !frameContains(queryFrame, "100x30") || frameContains(queryFrame, "@pane-2") || frameContains(queryFrame, "DETAIL 日志🚀") {
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
		t.Fatalf("send down: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain enter: %v", err)
	}
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != state.DefaultPaneID || runtime.State().Shell.Overlay.Open || runtime.State().Session.TerminalID != "term-2" {
		t.Fatalf("enter should attach selected terminal into current pane and close overlay, state=%#v shell=%#v", runtime.State().Session, runtime.State().Shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("picker navigation must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeTerminalPickerShowsExitedTerminalImmediatelyAfterAttach(t *testing.T) {
	exitedAt := time.Date(2026, 6, 17, 12, 45, 0, 0, time.UTC)
	exitCode := 23
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-main", Channel: 4, Cols: 80, Rows: 24},
		ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{
			TerminalID: "term-dead",
			Title:      "dead shell",
			State:      string(state.TerminalLiveExited),
			ExitCode:   &exitCode,
			ExitedAt:   exitedAt,
			Command:    []string{"bash", "-lc", "exit 23"},
			Cols:       80,
			Rows:       24,
		}}},
		SurfaceResult: services.TerminalSurfaceResult{
			Ready: true,
			Snapshot: state.LiveSurfaceSnapshot{
				TerminalID: "term-dead",
				Cols:       80,
				Rows:       24,
				State:      state.TerminalLiveExited,
				ExitCode:   23,
				ExitReason: "exited",
				ExitedAt:   exitedAt,
				Command:    []string{"bash", "-lc", "exit 23"},
			},
		},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: state.DefaultShell()},
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
	terminal.AttachResult = services.TerminalAttachResult{Channel: 9, Cols: 80, Rows: 24}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x06", Ctrl: true}); err != nil {
		t.Fatalf("send ctrl-f: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "d"}); err != nil {
		t.Fatalf("send query d: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "e"}); err != nil {
		t.Fatalf("send query e: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain picker query: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}); err != nil {
		t.Fatalf("send down: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain exited attach: %v", err)
	}

	if runtime.State().Shell.Overlay.Open {
		t.Fatalf("picker should close after attach, overlay=%#v", runtime.State().Shell.Overlay)
	}
	surface := runtime.State().Surface.SurfaceForTerminal("term-dead")
	if surface.State != state.TerminalLiveExited || surface.ExitCode != 23 {
		t.Fatalf("attached exited terminal should be visible immediately, surface=%#v", surface)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "terminal exited: term-dead code:23 exited") ||
		!frameContains(frame, "exited at: 2026-06-17T12:45:00Z") ||
		!frameContains(frame, "command: bash -lc exit 23") ||
		!frameContains(frame, "R restart current terminal") ||
		!frameContains(frame, "Ctrl-F choose another terminal") {
		t.Fatalf("exited terminal selected from picker should render lifecycle CTA without extra input, frame=%#v", frame.Lines)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("picker attach to exited terminal must not require/leak input, got %#v", terminal.Inputs)
	}
}

func TestUIInputReducerGlobalQuitShortcutEmitsQuitMsg(t *testing.T) {
	reducer := NewUIInputReducer()
	root := state.Root{Shell: state.DefaultShell().SetInteractionMode(state.InteractionModeGlobal)}

	_, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "q"}})
	if !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("global q should be handled and emit quit effect, got %#v", effects)
	}
	if _, ok := effects[0].(handledEffect); !ok {
		t.Fatalf("global q should mark input handled, got %#v", effects[0])
	}
	msg := runFirstNonStickyTimeoutEffect(t, effects)
	if _, ok := msg.(QuitMsg); !ok {
		t.Fatalf("global q should emit QuitMsg, got %#v", msg)
	}
}

func TestInteractiveRuntimeTerminalPickerEnterDefaultsToCreateTerminal(t *testing.T) {
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
	if len(terminal.Creates) != 0 || len(terminal.Attaches) != 1 {
		t.Fatalf("picker default enter should only open create form, creates=%#v attaches=%#v", terminal.Creates, terminal.Attaches)
	}
	if !runtime.State().Shell.Overlay.Open || runtime.State().Shell.Overlay.Kind != state.OverlayPrompt || runtime.State().Shell.Overlay.Prompt.Purpose != "terminal.create" {
		t.Fatalf("picker default enter should open create terminal prompt, overlay=%#v", runtime.State().Shell.Overlay)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("picker create navigation must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeCreateTerminalFormSubmitsTerminalCreate(t *testing.T) {
	terminal := &services.FakeTerminalService{
		CreateResult: services.TerminalCreateResult{TerminalID: "term-created", State: "running"},
	}
	host := NewFakeTerminalHost(64)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: state.DefaultShell().OpenPrompt(createTerminalPrompt(state.DefaultPaneID))},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	sendChars := func(value string) {
		t.Helper()
		for _, r := range value {
			if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: string(r)}); err != nil {
				t.Fatalf("send char %q: %v", r, err)
			}
		}
	}
	sendChars("my-term")
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}); err != nil {
		t.Fatalf("send tab: %v", err)
	}
	sendChars("bash -lc 'echo hi'")
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain create form: %v", err)
	}
	if len(terminal.Creates) != 1 {
		t.Fatalf("create form should call terminal create once, got %#v", terminal.Creates)
	}
	if len(terminal.Attaches) != 1 {
		t.Fatalf("successful create should attach new terminal once, got %#v", terminal.Attaches)
	}
	create := terminal.Creates[0]
	if create.Title != "my-term" || len(create.Command) != 3 || create.Command[2] != "echo hi" || len(create.Tags) != 0 {
		t.Fatalf("unexpected create request %#v", create)
	}
	attach := terminal.Attaches[0]
	if attach.TerminalID != "term-created" || attach.ViewID != state.TerminalPaneViewID(state.DefaultPaneID) || attach.ResizePolicy != "owner" {
		t.Fatalf("create should attach created terminal to current pane, got %#v", attach)
	}
	if runtime.State().Session.TerminalID != "term-created" || runtime.State().Shell.EnsureDefaults().Workspace.Tabs[0].Panes[0].TerminalID != "term-created" {
		t.Fatalf("create should switch current pane to created terminal, state=%#v shell=%#v", runtime.State().Session, runtime.State().Shell)
	}
	if runtime.State().Shell.Overlay.Open {
		t.Fatalf("successful create should close overlay, got %#v", runtime.State().Shell.Overlay)
	}
}

func TestInteractiveRuntimeCreateTerminalFormRequiresName(t *testing.T) {
	terminal := &services.FakeTerminalService{
		CreateResult: services.TerminalCreateResult{TerminalID: "term-created", State: "running"},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: state.DefaultShell().OpenPrompt(createTerminalPrompt(state.DefaultPaneID))},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain create form: %v", err)
	}
	if len(terminal.Creates) != 0 {
		t.Fatalf("missing name must not create terminal, got %#v", terminal.Creates)
	}
	prompt := runtime.State().Shell.EnsureDefaults().Overlay.Prompt
	if !runtime.State().Shell.Overlay.Open || prompt.LastResult != "name is required" {
		t.Fatalf("missing name should keep form open with validation text, overlay=%#v", runtime.State().Shell.Overlay)
	}
}

func TestUIInputReducerCreateTerminalFormEditsAndCancels(t *testing.T) {
	reducer := NewUIInputReducer()
	root := state.Root{Shell: state.DefaultShell().OpenPrompt(createTerminalPrompt(state.DefaultPaneID))}
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "a"}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "b"}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyLeft}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyBackspace}})
	prompt := root.Shell.EnsureDefaults().Overlay.Prompt
	if prompt.FieldRawValue("name") != "ab" || prompt.ActiveField != 0 || prompt.Fields[0].Cursor != 1 {
		t.Fatalf("name field should edit at cursor and delete in-place, prompt=%#v", prompt)
	}
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "s"}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "h"}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyHome}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "z"}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyShiftTab}})
	prompt = root.Shell.EnsureDefaults().Overlay.Prompt
	if prompt.ActiveField != 0 || prompt.FieldRawValue("command") != "zsh" || prompt.Fields[1].Cursor != 1 {
		t.Fatalf("tab should move between editable form fields without losing cursor values, prompt=%#v", prompt)
	}
	root, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEsc}})
	if root.Shell.EnsureDefaults().Overlay.Open || len(effects) != 1 {
		t.Fatalf("esc should close create form and consume input, root=%#v effects=%#v", root, effects)
	}
}

func TestUIInputReducerCreateTerminalWorkdirSuggestions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"demo", "dev", "delta"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	nested := filepath.Join(dir, "dev", "src")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	prefix := filepath.Join(dir, "d")
	reducer := NewUIInputReducer()
	root := state.Root{Shell: state.DefaultShell().OpenPrompt(state.PromptState{
		Title:       "Create Terminal",
		Purpose:     "terminal.create",
		ActiveField: 2,
		Fields: []state.PromptFieldState{
			{Key: "name", Label: "name", Value: "shell", Required: true},
			{Key: "command", Label: "command", Value: "/bin/sh"},
			{Key: "workdir", Label: "workdir", Value: prefix, Cursor: len([]rune(prefix))},
		},
	})}

	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}})
	prompt := root.Shell.EnsureDefaults().Overlay.Prompt
	if !prompt.SuggestionFocused || len(prompt.ActiveSuggestionItems()) != 3 || prompt.ActiveField != 2 {
		t.Fatalf("tab on workdir should enter suggestion focus, prompt=%#v", prompt)
	}
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}})
	prompt = root.Shell.EnsureDefaults().Overlay.Prompt
	want := filepath.Join(dir, "dev") + string(os.PathSeparator)
	if prompt.Submitted || prompt.SuggestionFocused || prompt.FieldRawValue("workdir") != want || prompt.Fields[2].Cursor != len([]rune(want)) {
		t.Fatalf("enter should accept selected workdir suggestion without submitting, want=%q prompt=%#v", want, prompt)
	}

	root = state.Root{Shell: state.DefaultShell().OpenPrompt(state.PromptState{
		Title:       "Create Terminal",
		Purpose:     "terminal.create",
		ActiveField: 2,
		Fields: []state.PromptFieldState{
			{Key: "name", Label: "name", Value: "shell", Required: true},
			{Key: "command", Label: "command", Value: "/bin/sh"},
			{Key: "workdir", Label: "workdir", Value: prefix, Cursor: len([]rune(prefix))},
		},
	})}
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyShiftTab}})
	prompt = root.Shell.EnsureDefaults().Overlay.Prompt
	if !prompt.SuggestionFocused || prompt.SuggestionSelected != 2 {
		t.Fatalf("shift-tab in suggestion focus should wrap to previous candidate, prompt=%#v", prompt)
	}

	root = state.Root{Shell: state.DefaultShell().OpenPrompt(state.PromptState{
		Title:       "Create Terminal",
		Purpose:     "terminal.create",
		ActiveField: 2,
		Fields: []state.PromptFieldState{
			{Key: "name", Label: "name", Value: "shell", Required: true},
			{Key: "command", Label: "command", Value: "/bin/sh"},
			{Key: "workdir", Label: "workdir", Value: prefix, Cursor: len([]rune(prefix))},
		},
	})}
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}})
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyRight}})
	prompt = root.Shell.EnsureDefaults().Overlay.Prompt
	if !prompt.SuggestionFocused || prompt.FieldRawValue("workdir") != want || len(prompt.ActiveSuggestionItems()) != 1 || !strings.HasSuffix(prompt.ActiveSuggestionItems()[0], "src"+string(os.PathSeparator)) {
		t.Fatalf("right should enter selected directory and keep suggestion focus, want=%q prompt=%#v", want, prompt)
	}
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyLeft}})
	prompt = root.Shell.EnsureDefaults().Overlay.Prompt
	if !prompt.SuggestionFocused || prompt.FieldRawValue("workdir") != dir+string(os.PathSeparator) || len(prompt.ActiveSuggestionItems()) != 3 {
		t.Fatalf("left should move to parent directory and refresh suggestions, prompt=%#v", prompt)
	}

	root = state.Root{Shell: state.DefaultShell().OpenPrompt(state.PromptState{
		Title:       "Create Terminal",
		Purpose:     "terminal.create",
		ActiveField: 2,
		Fields: []state.PromptFieldState{
			{Key: "name", Label: "name", Value: "shell", Required: true},
			{Key: "command", Label: "command", Value: "/bin/sh"},
			{Key: "workdir", Label: "workdir", Value: prefix, Cursor: len([]rune(prefix))},
		},
	})}
	root, _ = reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}})
	root, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEsc}})
	prompt = root.Shell.EnsureDefaults().Overlay.Prompt
	if !root.Shell.EnsureDefaults().Overlay.Open || prompt.SuggestionFocused || len(effects) != 1 {
		t.Fatalf("esc in suggestion focus should only exit suggestions, overlay=%#v effects=%#v", root.Shell.Overlay, effects)
	}
}

func TestCreateTerminalPromptSubmitUsesEditedFields(t *testing.T) {
	dir := t.TempDir()
	workdir := filepath.Join(dir, "work")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	reducer := NewUIInputReducer()
	root := state.Root{Shell: state.DefaultShell().OpenPrompt(createTerminalPrompt(state.DefaultPaneID))}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "d"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "e"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "v"},
		{Kind: input.EventKindKey, Key: input.KeyTab},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "b"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "a"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "s"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "h"},
		{Kind: input.EventKindKey, Key: input.KeyTab},
		{Kind: input.EventKindKey, Key: input.KeyHome},
		{Kind: input.EventKindKey, Key: input.KeyEnd},
	} {
		root, _ = reducer(root, InputMsg{Event: event})
	}
	root.Shell = root.Shell.SetPromptValue(workdir)

	next, effects := reducePromptSubmit(root)
	if next.Shell.Overlay.Open || len(effects) != 1 {
		t.Fatalf("submit should close prompt and emit create effect, root=%#v effects=%#v", next, effects)
	}
	effect, ok := effects[0].(FuncEffect)
	if !ok || effect.Run == nil {
		t.Fatalf("expected create FuncEffect, got %#v", effects)
	}
	msg := effect.Run(context.Background())
	request, ok := msg.(TerminalPoolCreateRequestMsg)
	if !ok {
		t.Fatalf("expected terminal create request, got %#v", msg)
	}
	if request.Title != "dev" || strings.Join(request.Command, " ") != "bash" || request.CWD != workdir {
		t.Fatalf("edited prompt fields should drive create request, got %#v", request)
	}

	root = state.Root{Shell: state.DefaultShell().OpenPrompt(createTerminalPrompt(state.DefaultPaneID))}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "c"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"},
		{Kind: input.EventKindKey, Key: input.KeyTab},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "c"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "o"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "d"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "e"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"},
	} {
		root, _ = reducer(root, InputMsg{Event: event})
	}
	next, effects = reducePromptSubmit(root)
	if next.Shell.Overlay.Open || len(effects) != 1 {
		t.Fatalf("submit custom command should close prompt and emit effect, root=%#v effects=%#v", next, effects)
	}
	effect = effects[0].(FuncEffect)
	request = effect.Run(context.Background()).(TerminalPoolCreateRequestMsg)
	if request.Title != "cx" || strings.Join(request.Command, " ") != "codex" {
		t.Fatalf("custom command should override default shell fallback, got %#v", request)
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
	if !frameContains(frame, "远程🚀") || !frameContains(frame, "running") || frameContains(frame, "@pool") || frameContains(frame, "term-pool") {
		t.Fatalf("expected pool row in picker frame, got %#v", frame.Lines)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}); err != nil {
		t.Fatalf("send down: %v", err)
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
	if root.TerminalPool.LastCreatedID != "term-created" || len(root.Shell.Toasts) == 0 || root.Shell.Toasts[len(root.Shell.Toasts)-1].Body != "term-created" || len(effects) != 2 {
		t.Fatalf("expected create feedback plus refresh and attach effects, got root=%#v effects=%#v", root, effects)
	}
	attachEffect, ok := effects[1].(FuncEffect)
	if !ok {
		t.Fatalf("expected create attach FuncEffect, got %#v", effects[1])
	}
	attachMsg, ok := attachEffect.Run(context.Background()).(TerminalPoolAttachRequestMsg)
	if !ok || attachMsg.TerminalID != "term-created" {
		t.Fatalf("expected create to request attach for created terminal, got %#v", attachMsg)
	}
}

func TestTerminalSizeLockToggleWritesTerminalTagsAndProjectsViews(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{
		Shell:        state.DefaultShell(),
		TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{{TerminalID: "term-1", Title: "main", Tags: map[string]string{"role": "shell"}}}},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 80, 24, state.TerminalResizeRoleFollower, "surface", "view-2", false))

	next, effects := reducer(root, TerminalSizeLockToggleRequestMsg{})
	if len(effects) != 1 {
		t.Fatalf("size lock should issue one tag edit, got %#v", effects)
	}
	result, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalSizeLockToggleResultMsg)
	if !ok || result.TerminalID != "term-1" || !result.Locked {
		t.Fatalf("expected size lock result, got %#v", result)
	}
	if len(terminal.TagEdits) != 1 || terminal.TagEdits[0].Tags["role"] != "shell" || terminal.TagEdits[0].Tags["termx.size_lock"] != "lock" {
		t.Fatalf("size lock must preserve existing tags and set lock tag, edits=%#v", terminal.TagEdits)
	}
	next, _ = reducer(next, result)
	owner, _ := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	follower, _ := next.TerminalViews.PaneBinding("pane-2")
	if !owner.SizeLocked || owner.CanResize || owner.ControlReason != "size_locked" || owner.ResizeRole != state.TerminalResizeRoleOwner {
		t.Fatalf("owner view should project locked owner without resize rights, got %#v", owner)
	}
	if !follower.SizeLocked || follower.CanResize || follower.ControlReason != "size_locked" {
		t.Fatalf("follower view should project terminal lock, got %#v", follower)
	}
	if next.TerminalPool.Items[0].Tags["termx.size_lock"] != "lock" {
		t.Fatalf("terminal pool tags should update after lock result, pool=%#v", next.TerminalPool)
	}

	next, effects = reducer(next, TerminalSizeLockToggleRequestMsg{})
	result = effects[0].(FuncEffect).Run(context.Background()).(TerminalSizeLockToggleResultMsg)
	if result.Locked {
		t.Fatalf("unlock should report unlocked result, got %#v", result)
	}
	if _, ok := result.Tags["termx.size_lock"]; ok {
		t.Fatalf("unlock should remove size lock tag, got %#v", result)
	}
	next, _ = reducer(next, result)
	owner, _ = next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if owner.SizeLocked || !owner.CanResize || owner.ControlReason != "" {
		t.Fatalf("unlock should restore owner resize rights, got %#v", owner)
	}
}

func TestTerminalSizeLockToggleRequiresTerminalPoolTagsBeforeWriting(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{Shell: state.DefaultShell()}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))

	next, effects := reducer(root, TerminalSizeLockToggleRequestMsg{})
	if len(terminal.TagEdits) != 0 || len(effects) != 1 {
		t.Fatalf("missing pool tags should only request metadata refresh, edits=%#v effects=%#v", terminal.TagEdits, effects)
	}
	if _, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolListRequestMsg); !ok {
		t.Fatalf("missing pool tags should refresh terminal pool, got %#v", effects[0])
	}
	if len(next.Shell.Toasts) == 0 || next.Shell.Toasts[len(next.Shell.Toasts)-1].Body != "terminal metadata pending" {
		t.Fatalf("missing pool tags should warn instead of overwriting tags, root=%#v", next)
	}
}

func TestTerminalSizeLockToggleRequiresOwnerIdentity(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{
		Shell:        state.DefaultShell(),
		TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{{TerminalID: "term-1", Title: "main", Tags: map[string]string{"role": "shell"}}}},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleFollower, "surface", "view-1", false))

	next, effects := reducer(root, TerminalSizeLockToggleRequestMsg{})
	if len(terminal.TagEdits) != 0 || len(effects) != 0 {
		t.Fatalf("follower must not write size lock tags, edits=%#v effects=%#v", terminal.TagEdits, effects)
	}
	if len(next.Shell.Toasts) == 0 || next.Shell.Toasts[len(next.Shell.Toasts)-1].Body != "no active terminal" {
		t.Fatalf("follower size lock should warn, root=%#v", next)
	}
}

func TestTerminalPoolReducerHandlesRestartAndReconnectResults(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 3, Cols: 80, Rows: 24},
	}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{Shell: state.DefaultShell()}
	root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1")
	root.Session = root.Session.AttachWithResizeOwner("term-1", 9, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 9, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	root.Surface = root.Surface.ApplySnapshot(state.LiveSurfaceSnapshot{TerminalID: "term-1", Cols: 80, Rows: 24, Lines: []string{"old live tail"}})
	root.Surface = root.Surface.MarkExitedWithMetadata("term-1", 23, "exited", time.Date(2026, 6, 17, 12, 30, 0, 0, time.UTC), []string{"bash", "-lc", "exit 23"})
	code := 23
	root.TerminalPool = state.TerminalPoolStore{Items: []state.TerminalPoolItem{{TerminalID: "term-1", State: string(state.TerminalLiveExited), ExitCode: &code, ExitedAt: time.Date(2026, 6, 17, 12, 30, 0, 0, time.UTC), Command: []string{"bash", "-lc", "exit 23"}}}}

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
	if len(root.Shell.Toasts) == 0 || root.Shell.Toasts[len(root.Shell.Toasts)-1].Title != "picker.restart" || len(effects) != 2 {
		t.Fatalf("expected restart result feedback and refresh effect, terminal=%#v root=%#v effects=%#v", terminal, root, effects)
	}
	if root.Surface.State != state.TerminalLiveAttached || root.Surface.ExitCode != 0 || !root.Surface.ExitedAt.IsZero() || len(root.Surface.Command) != 0 {
		t.Fatalf("restart should clear exited surface metadata, got %#v", root.Surface)
	}
	if root.TerminalPool.Items[0].State != string(state.TerminalLiveExited) || root.TerminalPool.Items[0].ExitCode == nil {
		t.Fatalf("restart ack must not forge pool running state before next core list, got %#v", root.TerminalPool.Items[0])
	}
	if len(root.Surface.Lines) != 1 || root.Surface.Lines[0] != "old live tail" || !root.Surface.Ready {
		t.Fatalf("restart should preserve live tail while reattaching, got %#v", root.Surface)
	}
	frame := render.NewRenderer(render.DefaultTheme()).Render(render.NewRenderVMBuilder().Build(root))
	if frame.Cursor.Visible {
		t.Fatalf("restart preserved tail must not synthesize a cursor at the old line end, cursor=%#v rect=%#v", frame.Cursor, frame.CursorRect)
	}
	if binding, ok := root.TerminalViews.PaneBinding(state.DefaultPaneID); !ok || binding.Channel != 0 || binding.Attached {
		t.Fatalf("restart should preserve binding intent but clear stale input channel, got %#v ok=%v", binding, ok)
	}
	if root.Session.Channel != 0 || root.Session.Attached {
		t.Fatalf("restart should clear session input channel before reattach, got %#v", root.Session)
	}
	if _, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolListRequestMsg); !ok {
		t.Fatalf("first restart result effect should refresh pool, got %#v", effects[0])
	}
	if attachMsg, ok := effects[1].(FuncEffect).Run(context.Background()).(LiveAttachMsg); !ok || attachMsg.Config.TerminalID != "term-1" || attachMsg.Config.ViewID != state.TerminalPaneViewID(state.DefaultPaneID) {
		t.Fatalf("restart should reattach current terminal view, got %#v ok=%v", attachMsg, ok)
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

func TestTerminalPoolRestartResultPreventsStaleExitedPoolFromPoisoningReattach(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
		ListResult:   services.TerminalListResult{Items: []services.TerminalPoolItem{{TerminalID: "term-1", State: "running", Command: []string{"/bin/zsh"}, Cols: 80, Rows: 24}}},
	}
	host := NewFakeTerminalHost(16)
	runtime := NewInteractiveRuntime(state.Root{}, host, NewSyncEffectRunner(), LiveDeps{Terminal: terminal}, CopyModeDeps{})
	root := runtime.State()
	root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1")
	root.Session = root.Session.AttachWithResizeOwner("term-1", 9, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 9, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	root.Surface = root.Surface.ApplySnapshot(state.LiveSurfaceSnapshot{TerminalID: "term-1", Revision: 2, Cols: 80, Rows: 24, Lines: []string{"terminal exited: term-1 code:0 exited"}})
	root.Surface = root.Surface.MarkExitedWithMetadata("term-1", 0, "exited", time.Date(2026, 6, 17, 12, 45, 0, 0, time.UTC), []string{"/bin/zsh"})
	code := 0
	root.TerminalPool = state.TerminalPoolStore{Items: []state.TerminalPoolItem{{TerminalID: "term-1", State: string(state.TerminalLiveExited), ExitCode: &code, ExitedAt: time.Date(2026, 6, 17, 12, 45, 0, 0, time.UTC), Command: []string{"/bin/zsh"}}}}
	runtime.state = root

	if err := runtime.Post(TerminalPoolRestartResultMsg{TerminalID: "term-1"}); err != nil {
		t.Fatalf("post restart result: %v", err)
	}
	if err := runtime.Post(TerminalPoolAttachResultMsg{
		TerminalID:   "term-1",
		TargetPaneID: state.DefaultPaneID,
		ResizePolicy: state.TerminalResizeRoleOwner,
		Result:       services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24, ResizePolicy: state.TerminalResizeRoleOwner, SurfaceID: "surface", ViewID: state.TerminalPaneViewID(state.DefaultPaneID), CanResize: true},
	}); err != nil {
		t.Fatalf("post reattach result: %v", err)
	}
	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   3,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"terminal exited: term-1 code:0 exited", "% "},
		Cursor:     state.LiveCursor{Visible: true, Row: 1, Col: 2, Shape: "bar"},
	}}); err != nil {
		t.Fatalf("post live surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain restart reattach: %v", err)
	}

	final := runtime.State()
	if final.Surface.State != state.TerminalLiveAttached || final.Surface.ExitReason != "" || final.Surface.Cursor != (state.LiveCursor{Visible: true, Row: 1, Col: 2, Shape: "bar"}) {
		t.Fatalf("restart reattach should accept running live surface and cursor, surface=%#v", final.Surface)
	}
	if final.TerminalPool.Items[0].State != "running" || final.TerminalPool.Items[0].ExitCode != nil {
		t.Fatalf("restart should keep pool item running until refreshed list arrives, pool=%#v", final.TerminalPool)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "% ") || frameContains(frame, "R restart current terminal") || !frame.Cursor.Visible || frame.Cursor.Shape != render.CursorShapeBar {
		t.Fatalf("frame should show restarted live prompt with visible cursor, lines=%#v cursor=%#v", frame.Lines, frame.Cursor)
	}
}

func TestRestartIfExitedSkipsRunningCoreTerminal(t *testing.T) {
	terminal := &services.FakeTerminalService{
		ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{
			TerminalID: "term-1",
			Title:      "main",
			State:      "running",
			Command:    []string{"/bin/zsh"},
			Cols:       80,
			Rows:       24,
		}}},
	}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{Shell: state.DefaultShell()}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	root.Session = root.Session.AttachWithResizeOwner("term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID))
	root.Surface = root.Surface.ApplySnapshot(state.LiveSurfaceSnapshot{TerminalID: "term-1", State: state.TerminalLiveAttached, Cols: 80, Rows: 24})

	root, effects := reducer(root, TerminalPoolRestartIfExitedRequestMsg{TerminalID: "term-1"})
	if len(effects) != 1 {
		t.Fatalf("restart-if-exited request should list core lifecycle, effects=%#v", effects)
	}
	resultMsg, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolRestartIfExitedResultMsg)
	if !ok {
		t.Fatalf("expected restart-if-exited result msg, got %#v", effects[0])
	}
	root, effects = reducer(root, resultMsg)
	if len(effects) != 0 || len(terminal.Restarts) != 0 {
		t.Fatalf("running core terminal must not be restarted, effects=%#v restarts=%#v", effects, terminal.Restarts)
	}
	if root.Surface.State != state.TerminalLiveAttached {
		t.Fatalf("running core lifecycle should keep surface attached, surface=%#v", root.Surface)
	}
}

func TestTerminalPoolRunningListDoesNotCacheLifecycleBeforeSurface(t *testing.T) {
	host := NewFakeTerminalHost(16)
	runtime := NewInteractiveRuntime(state.Root{}, host, NewSyncEffectRunner(), LiveDeps{Terminal: &services.FakeTerminalService{}}, CopyModeDeps{})
	root := runtime.State()
	root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1")
	root.Session = root.Session.AttachWithResizeOwner("term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID))
	root.Session = root.Session.MarkExitedWithMetadata("term-1", 0, "exited", time.Date(2026, 6, 17, 12, 50, 0, 0, time.UTC), []string{"/bin/zsh"})
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	root.Surface = root.Surface.ApplySnapshot(state.LiveSurfaceSnapshot{TerminalID: "term-1", Revision: 2, Cols: 80, Rows: 24, Lines: []string{"terminal exited: term-1 code:0 exited"}})
	root.Surface = root.Surface.MarkExitedWithMetadata("term-1", 0, "exited", time.Date(2026, 6, 17, 12, 50, 0, 0, time.UTC), []string{"/bin/zsh"})
	root.TerminalPool = root.TerminalPool.RequestList()
	seq := root.TerminalPool.RequestSeq
	runtime.state = root

	if err := runtime.Post(TerminalPoolListResultMsg{Seq: seq, Result: services.TerminalListResult{Items: []services.TerminalPoolItem{{TerminalID: "term-1", State: "running", Command: []string{"/bin/zsh"}, Cols: 80, Rows: 24}}}}); err != nil {
		t.Fatalf("post running list: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain running list: %v", err)
	}
	afterList := runtime.State()
	if afterList.Surface.State != state.TerminalLiveExited || afterList.Session.State != state.TerminalLiveExited {
		t.Fatalf("running list must not mutate live surface/session lifecycle, surface=%#v session=%#v", afterList.Surface, afterList.Session)
	}
	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   3,
		Cols:       80,
		Rows:       24,
		Lines:      []string{"terminal exited: term-1 code:0 exited", "% "},
		Cursor:     state.LiveCursor{Visible: true, Row: 1, Col: 2, Shape: "bar"},
		State:      state.TerminalLiveAttached,
	}, LifecycleKnown: true}); err != nil {
		t.Fatalf("post live surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain running surface: %v", err)
	}

	final := runtime.State()
	if final.Surface.State != state.TerminalLiveAttached || final.Surface.ExitReason != "" || final.Session.State == state.TerminalLiveExited {
		t.Fatalf("running live surface should clear exited lifecycle after list-only cache, surface=%#v session=%#v", final.Surface, final.Session)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "% ") || frameContains(frame, "R restart current terminal") || !frame.Cursor.Visible {
		t.Fatalf("running live surface should unblock fresh live prompt and cursor, lines=%#v cursor=%#v", frame.Lines, frame.Cursor)
	}
}

func TestTerminalPoolRestartReattachesEachViewWithoutReusingExitedChannels(t *testing.T) {
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: &services.FakeTerminalService{}})
	root := state.Root{Shell: state.DefaultShell()}
	root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1")
	var result state.FloatingCommandResult
	root.Shell, result = root.Shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane", Title: "floating", Kind: state.PaneEmpty},
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating failed: %#v", result)
	}
	root.Shell = root.Shell.BindFloatingTerminal("floating-1", "term-1")
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	root.TerminalViews = root.TerminalViews.BindFloating(state.NewFloatingTerminalView("floating-1", "floating-pane", "term-1", 8, 40, 12, state.TerminalResizeRoleFollower, "surface", state.TerminalFloatingViewID("floating-1"), false))
	root.Session = root.Session.AttachWithResizeOwner("term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID))
	root.Session = root.Session.MarkExitedWithMetadata("term-1", 23, "exited", time.Date(2026, 6, 17, 12, 35, 0, 0, time.UTC), []string{"bash", "-lc", "exit 23"})

	next, effects := reducer(root, TerminalPoolRestartResultMsg{TerminalID: "term-1"})
	if len(effects) != 3 {
		t.Fatalf("restart should refresh pool and reattach both views, got %#v", effects)
	}
	pane, _ := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	floating, _ := next.TerminalViews.FloatingBinding("floating-1")
	if pane.Channel != 0 || pane.Attached || floating.Channel != 0 || floating.Attached {
		t.Fatalf("restart must clear stale channels for all views, pane=%#v floating=%#v", pane, floating)
	}
	if next.Session.State != state.TerminalLivePending || next.Session.Channel != 0 || next.Session.Attached || !next.Session.ExitedAt.IsZero() || len(next.Session.Command) != 0 {
		t.Fatalf("restart should clear session exit metadata while waiting for attach, got %#v", next.Session)
	}
	seen := map[string]bool{}
	for _, effect := range effects[1:] {
		attachMsg, ok := effect.(FuncEffect).Run(context.Background()).(LiveAttachMsg)
		if !ok {
			t.Fatalf("restart view effect should produce LiveAttachMsg, got %#v", effect)
		}
		seen[attachMsg.Config.ViewID] = true
	}
	if !seen[state.TerminalPaneViewID(state.DefaultPaneID)] || !seen[state.TerminalFloatingViewID("floating-1")] {
		t.Fatalf("restart should reattach original pane and floating views, seen=%#v", seen)
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
	if !frameContains(frame, "Help") || !frameContains(frame, "core workflows") || !frameContains(frame, "Shell") || !frameContains(frame, "Terminal Pool") || !frameContains(frame, "Workbench Tree") || frameContains(frame, "clear toasts") {
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

func TestInteractiveRuntimeCtrlVRendersLatestStressTail(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 120, Rows: 30},
	}
	rows := make([]state.HistoryRow, 0, 100)
	for i := 1; i <= 100; i++ {
		text := stressHistoryLineForTUI(i)
		rows = append(rows, state.HistoryRow{
			Text:   text,
			LineID: uint64(i),
			Cells:  []state.HistoryCell{{Text: text, Width: len(text)}},
		})
	}
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-stress",
			118,
			1,
			rows,
		)}},
	}
	host := NewFakeTerminalHost(64)
	host.SetSize(120, 30)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: core, Rows: 20},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 120, Rows: 30}}); err != nil {
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
	last := lastFrame(t, host.Frames())
	if !frameContains(last, "000100") || frameContains(last, "ALT_SCREEN_MARK") {
		t.Fatalf("copy latest should render newest primary stress tail only, got %#v", last.Lines)
	}
}

func stressHistoryLineForTUI(n int) string {
	return fmt.Sprintf("%06d [DEBUG ] stream pending path=/var/tmp/alpha/beta/gamma wrap======================== tail-marker", n)
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
	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:         state.PaneCommandSplit,
		Target:         state.PaneCommandTarget{PaneID: state.DefaultPaneID},
		SplitDirection: state.SplitDirectionVertical,
		NewPane:        state.PaneState{ID: "pane-2", Title: "pane", Kind: state.PaneEmpty},
		Source:         state.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post semantic pane split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain semantic pane split: %v", err)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true},
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
		t.Fatalf("expected semantic split through pane command path, got %#v", tab)
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

	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:         state.PaneCommandSplit,
		Target:         state.PaneCommandTarget{PaneID: state.DefaultPaneID},
		SplitDirection: state.SplitDirectionVertical,
		NewPane:        state.PaneState{ID: "pane-2", Title: "pane", Kind: state.PaneEmpty},
		Source:         state.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post semantic pane split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain semantic pane split: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true}); err != nil {
		t.Fatalf("send pane mode after semantic split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane mode after semantic split: %v", err)
	}
	keyboardFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != "pane-2" {
		t.Fatalf("semantic split should activate pane-2, got %#v", runtime.State().Shell)
	}
	assertPaneVisualState(t, keyboardFrame, "pane", render.StyleAccent)
	assertPaneVisualState(t, keyboardFrame, "shell", render.StyleMuted)
	if !frameContains(keyboardFrame, "PANE") || !frameContains(keyboardFrame, "VSPLIT") || !frameContains(keyboardFrame, "HSPLIT") {
		t.Fatalf("footer should reflect tuiv2 pane split hints, got %#v", keyboardFrame.Lines)
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
	if !frameContains(zoomFrame, "PANE") || len(runtime.State().Shell.Toasts) == 0 {
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
	if !frameContains(closeFrame, "PANE") || len(runtime.State().Shell.Toasts) == 0 {
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
			{Window: historyWindowForApp(state.HistoryWindowReplace, "term-1", "tok-1", 33, 7, []state.HistoryRow{{Text: "copy-old", LineID: 20}})},
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
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "c"},
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
	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:         state.PaneCommandSplit,
		Target:         state.PaneCommandTarget{PaneID: state.DefaultPaneID},
		SplitDirection: state.SplitDirectionVertical,
		NewPane:        state.PaneState{ID: "pane-2", Title: "pane", Kind: state.PaneEmpty},
		Source:         state.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post product flow semantic split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain product flow semantic split: %v", err)
	}
	shell = runtime.State().Shell.EnsureDefaults()
	runtime.state.Shell = runtime.state.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: "pane-2"}, "term-1")
	if binding, ok := runtime.state.TerminalViews.PaneBinding(state.DefaultPaneID); ok {
		runtime.state.TerminalViews = runtime.state.TerminalViews.BindPane(state.NewPaneTerminalView(
			"pane-2", binding.TerminalID, binding.Channel, binding.DesiredCols, binding.DesiredRows, state.TerminalResizeRoleOwner, binding.SurfaceID, state.TerminalPaneViewID("pane-2"), true,
		)).TransferPaneResizeOwner("pane-2")
	}
	tab := shell.Workspace.Tabs[0]
	if len(tab.Panes) != 2 || tab.RootSplit.Direction != state.SplitDirectionVertical {
		t.Fatalf("semantic split should update pane tree geometry, got %#v", tab)
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

	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action: state.PaneCommandFocus,
		Target: state.PaneCommandTarget{PaneID: state.DefaultPaneID},
		Source: state.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("focus bound pane before missed mouse: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain bound pane focus: %v", err)
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
	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action: state.PaneCommandFocus,
		Target: state.PaneCommandTarget{PaneID: "pane-2"},
		Source: state.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("restore split pane focus before copy entry: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain split pane focus: %v", err)
	}
	runtime.state.CopyMode = state.CopyModeStore{}
	runtime.state.History = state.HistoryStore{}
	runtime.state.Shell.InteractionMode = state.InteractionModeNormal
	runtime.state.Shell = runtime.state.Shell.CloseOverlay()
	runtime.state.Shell.ActiveFloatingID = ""

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send copy entry: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain copy entry: %v", err)
	}
	if len(core.LatestRequests) != 1 || core.LatestRequests[0].Cols <= 0 || core.LatestRequests[0].PaneID != "pane-2" {
		t.Fatalf("copy mode should bind to hidden split content cols, got %#v", core.LatestRequests)
	}
	if runtime.State().CopyMode.BoundToken != "tok-1" || runtime.State().CopyMode.BoundCols != core.LatestRequests[0].Cols {
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
	if len(core.LatestRequests) != 1 || core.LatestRequests[0].Cols != 33 {
		t.Fatalf("pane size local reflow must not request a second latest window, got %#v", core.LatestRequests)
	}
	if runtime.State().CopyMode.BoundToken != "tok-1" || runtime.State().History.Token != "tok-1" {
		t.Fatalf("pane size local reflow should keep current frozen token, got copy=%#v history=%#v", runtime.State().CopyMode, runtime.State().History)
	}
	if runtime.State().CopyMode.BoundCols != 32 || runtime.State().History.Cols != 32 {
		t.Fatalf("pane size local reflow should update local history cols binding, got copy=%#v history=%#v", runtime.State().CopyMode, runtime.State().History)
	}
	if err := runtime.Post(ShellClearToastsMsg{}); err != nil {
		t.Fatalf("post clear toasts after copy rebind: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain clear toasts after copy rebind: %v", err)
	}
	copyFrame := lastFrame(t, host.Frames())
	if !frameContains(copyFrame, "copy-old") || frameContains(copyFrame, "copy-sized") {
		t.Fatalf("pane size local reflow should keep rendering current frozen history, got %#v", copyFrame.Lines)
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

	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:         state.PaneCommandSplit,
		Target:         state.PaneCommandTarget{PaneID: state.DefaultPaneID},
		SplitDirection: state.SplitDirectionVertical,
		NewPane:        state.PaneState{ID: "pane-2", Title: "pane", Kind: state.PaneEmpty},
		Source:         state.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post product shell semantic split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain product shell semantic split: %v", err)
	}
	sendCtrl("\x10")
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
	if !frameContains(paneFrame, "PANE") || !frameContains(paneFrame, "VSPLIT") || !frameContains(paneFrame, "HSPLIT") {
		t.Fatalf("expected pane mode footer and active feedback, got %#v", paneFrame.Lines)
	}

	sendCtrl("\x0f")
	sendChar("n")
	sendKey(input.KeyRight)
	sendKey(input.KeyDown)
	sendChar("L")
	sendChar("J")
	sendCtrl("\x07")
	sendChar("c")
	sendKey(input.KeyEsc)
	floatingFrame := lastFrame(t, host.Frames())
	if len(runtime.State().Shell.Floatings) != 1 ||
		!frameContains(floatingFrame, "unconnected") ||
		!frameContains(floatingFrame, "Attach existing terminal") ||
		!frameContains(floatingFrame, "["+render.DefaultPaneChromeGlyphs().Zoom+"]─["+render.DefaultPaneChromeGlyphs().Close+"]") ||
		frameContains(floatingFrame, render.DefaultPaneChromeGlyphs().Running+" float") {
		t.Fatalf("expected floating pane product shell content, shell=%#v frame=%#v", runtime.State().Shell, floatingFrame.Lines)
	}
	floatingClose := frameActionHitRegion(t, floatingFrame, "floating.close", "floating-pane-1")
	if !floatingClose.Floating {
		t.Fatalf("floating close hit region should carry floating flag, got %#v", floatingClose)
	}
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
	if frameContains(helpFrame, "clear toasts") || frameContains(helpFrame, "close toast") {
		t.Fatalf("help overlay should not promote toast controls, got %#v", helpFrame.Lines)
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
	sendChar("c")
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

func assertPaneANSIState(t *testing.T, frame render.Frame, text string, style render.ANSICellStyle) {
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
			if cell.Style == "" && cell.ANSIStyle == style {
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
	t.Fatalf("expected ANSI pane text %q with style %#v, got %#v", text, style, frame.StyledLines)
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
		t.Fatalf("floating input should anchor cursor inside floating content for IME, floating=%#v cursor=%#v frame=%#v", floatingRect, frameAfterFloating.CursorRect, frameAfterFloating.Cursor)
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
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "c"},
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
	if !frameContains(frame, "unconnected") || !frameContains(frame, "Attach existing terminal") {
		t.Fatalf("expected rendered floating pane, got %#v", frame.Lines)
	}

	raiseRegion := frameActionHitRegion(t, frame, "floating.raise", "floating-pane-1")
	if !raiseRegion.Floating {
		t.Fatalf("floating raise hit region should carry floating flag, got %#v", raiseRegion)
	}
	if err := host.SendInput(mouseEventAt(raiseRegion.Rect)); err != nil {
		t.Fatalf("send floating raise click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating raise: %v", err)
	}
	moveRegion := frameActionHitRegion(t, lastFrame(t, host.Frames()), "floating.move-drag", "floating-pane-1")
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
	resizeRegion := frameActionHitRegion(t, lastFrame(t, host.Frames()), "floating.resize-drag", "floating-pane-1")
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
	closeRegion := frameActionHitRegion(t, lastFrame(t, host.Frames()), "floating.close", "floating-pane-1")
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
	root, _ = reducer(root, ShellContentActionMsg{ActionID: render.ActionFloatingCenter.String(), PaneID: "floating-1"})
	if got := root.Shell.Floatings[0].Rect; got.X != 24 || got.Y != 7 {
		t.Fatalf("floating center action should center rect, got %#v", got)
	}
	root, _ = reducer(root, ShellContentActionMsg{ActionID: render.ActionFloatingCollapse.String(), PaneID: "floating-1"})
	if !root.Shell.Floatings[0].Collapsed {
		t.Fatalf("floating collapse action should toggle collapsed state, got %#v", root.Shell.Floatings[0])
	}
	root, _ = reducer(root, ShellContentActionMsg{ActionID: render.ActionFloatingShowAll.String()})
	if root.Shell.Floatings[0].Collapsed {
		t.Fatalf("floating show-all action should expand collapsed panes, got %#v", root.Shell.Floatings[0])
	}
	root, _ = reducer(root, ShellContentActionMsg{ActionID: render.ActionFloatingCollapseAll.String()})
	if !root.Shell.Floatings[0].Collapsed {
		t.Fatalf("floating collapse-all action should collapse all panes, got %#v", root.Shell.Floatings[0])
	}
	root, _ = reducer(root, ShellContentActionMsg{ActionID: "floating.close", PaneID: "floating-1"})
	if len(root.Shell.Floatings) != 0 {
		t.Fatalf("floating close action should remove floating, got %#v", root.Shell.Floatings)
	}
}

func TestFloatingGroupKeyboardIntentsMapToCommands(t *testing.T) {
	root := state.Root{
		Shell:    state.DefaultShell().SetInteractionMode(state.InteractionModeFloating),
		Viewport: state.ViewportStore{Valid: true, Cols: 80, Rows: 24},
	}
	reducer := NewUIInputReducer()
	for _, tc := range []struct {
		char   string
		action state.FloatingCommandAction
	}{
		{char: "v", action: state.FloatingCommandToggleAll},
		{char: "=", action: state.FloatingCommandFit},
		{char: "s", action: state.FloatingCommandToggleAutoFit},
	} {
		next, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: tc.char}})
		if next.Generation != root.Generation {
			t.Fatalf("keyboard intent %q should defer to effect message, got root=%#v", tc.char, next)
		}
		found := false
		for _, effect := range effects {
			fn, ok := effect.(FuncEffect)
			if !ok {
				continue
			}
			msg, ok := fn.Run(context.Background()).(ShellFloatingCommandMsg)
			if ok && msg.Command.Action == tc.action {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("keyboard intent %q should emit floating command %q, effects=%#v", tc.char, tc.action, effects)
		}
	}
}

func TestShellReducerHandlesResizeOwnerContentAction(t *testing.T) {
	root := state.Root{}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, state.TerminalResizeRoleFollower, "surface", "view-2", false))

	next, effects := NewShellReducer()(root, ShellContentActionMsg{ActionID: render.ActionTerminalTakeResizeOwner.String(), PaneID: "pane-2"})
	if len(effects) != 1 {
		t.Fatalf("owner transfer should request authoritative attach, got %#v", effects)
	}
	first, _ := next.TerminalViews.PaneBinding("pane-1")
	second, _ := next.TerminalViews.PaneBinding("pane-2")
	if first.ResizeRole != state.TerminalResizeRoleOwner || !first.CanResize {
		t.Fatalf("previous owner must stay stable until service result, got %#v", first)
	}
	if second.ResizeRole != state.TerminalResizeRoleFollower || second.CanResize {
		t.Fatalf("clicked pane must stay follower until service result, got %#v", second)
	}
	msg, ok := effects[0].(FuncEffect).Run(context.Background()).(LiveAttachMsg)
	if !ok || msg.Config.TerminalID != "term-1" || msg.Config.ViewID != "view-2" || msg.Config.ResizePolicy != state.TerminalResizeRoleOwner {
		t.Fatalf("owner request should target clicked view, got %#v", msg)
	}
}

func TestResizeModeTerminalSizeLockKeyAndFooterEmitTerminalLockRequest(t *testing.T) {
	root := state.Root{}
	root.Shell = state.DefaultShell().SetInteractionMode(state.InteractionModeResize)
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, state.TerminalResizeRoleFollower, "surface", "view-2", false))

	inputReducer := NewUIInputReducer()
	next, effects := inputReducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "s"}})
	if !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("size lock key should emit request effect, got %#v", effects)
	}
	msg, ok := runFirstNonStickyTimeoutEffect(t, effects).(TerminalSizeLockToggleRequestMsg)
	if !ok || msg != (TerminalSizeLockToggleRequestMsg{}) {
		t.Fatalf("resize s should request terminal size lock toggle, got %#v", msg)
	}
	binding, _ := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if binding.Layout.SizeLocked {
		t.Fatalf("resize s must not mutate view-local layout lock, got %#v", binding.Layout)
	}

	shellReducer := NewShellReducer()
	_, effects = shellReducer(next, ShellContentActionMsg{ActionID: render.ActionResizeLayoutLock.String()})
	if !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("footer lock should emit request effect, got %#v", effects)
	}
	if _, ok := runFirstNonStickyTimeoutEffect(t, effects).(TerminalSizeLockToggleRequestMsg); !ok {
		t.Fatalf("footer lock should request terminal size lock toggle, got %#v", effects)
	}
}

func TestResizeModeTerminalLayoutKeysAndActionsShareViewLocalState(t *testing.T) {
	root := state.Root{}
	root.Shell = state.DefaultShell().SetInteractionMode(state.InteractionModeResize)
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, state.TerminalResizeRoleFollower, "surface", "view-2", false))

	inputReducer := NewUIInputReducer()
	next, _ := inputReducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyRight, Shift: true}})
	binding, _ := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	sibling, _ := next.TerminalViews.PaneBinding("pane-2")
	if sibling.Layout.PanX != 0 || sibling.Layout.PanY != 0 {
		t.Fatalf("sibling view on same terminal must not inherit layout pan, got %#v", sibling.Layout)
	}
	binding, _ = next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if binding.Layout.PanX != 2 {
		t.Fatalf("shift-right should pan active terminal view, got %#v", binding.Layout)
	}

	shellReducer := NewShellReducer()
	next, _ = shellReducer(next, ShellContentActionMsg{ActionID: render.ActionResizeLayoutCenter.String()})
	binding, _ = next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if binding.Layout.Mode != state.TerminalViewLayoutCenter || binding.Layout.AlignX != state.TerminalViewAlignCenter || binding.Layout.AlignY != state.TerminalViewAlignCenter {
		t.Fatalf("footer center should use same layout command path, got %#v", binding.Layout)
	}
	vm := render.NewRenderVMBuilder().Build(next)
	if len(vm.Shell.Layout.Panels) == 0 || vm.Shell.Layout.Panels[0].Chrome.Terminal.LayoutMode != state.TerminalViewLayoutCenter {
		t.Fatalf("render should project terminal view layout, got %#v", vm.Shell.Layout.Panels)
	}
	if vm.Shell.Layout.Panels[0].Content.Layout.Mode != state.TerminalViewLayoutCenter || vm.Shell.Layout.Panels[0].Content.Layout.AlignX != state.TerminalViewAlignCenter {
		t.Fatalf("render content should consume terminal view layout, got %#v", vm.Shell.Layout.Panels[0].Content.Layout)
	}
}

func TestPaneModeLockUsesTerminalSizeLockPath(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell().SetInteractionMode(state.InteractionModePane)}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))

	next, effects := NewUIInputReducer()(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "s"}})
	if !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("pane s should emit terminal size lock request, got %#v", effects)
	}
	if _, ok := runFirstNonStickyTimeoutEffect(t, effects).(TerminalSizeLockToggleRequestMsg); !ok {
		t.Fatalf("pane s should request terminal size lock toggle, got %#v", effects)
	}
	binding, _ := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if binding.Layout.SizeLocked {
		t.Fatalf("pane s must not toggle view-local layout lock, got %#v", binding.Layout)
	}

	next, effects = NewUIInputReducer()(state.Root{Shell: state.DefaultShell().SetInteractionMode(state.InteractionModePane)}, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "s"}})
	if !hasStickyInteractionModeTimeoutEffect(effects) {
		t.Fatalf("pane s without active terminal still routes to size lock reducer, got %#v toasts=%#v", effects, next.Shell.Toasts)
	}
}

func TestOverlayKeyboardCommandsRouteThroughContentActions(t *testing.T) {
	inputReducer := NewUIInputReducer()
	root := state.Root{Shell: state.DefaultShell().OpenTerminalPicker()}

	_, effects := inputReducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyTab}})
	assertContentActionEffect(t, effects, render.ActionPickerSplit)

	_, effects = inputReducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x05", Ctrl: true}})
	assertContentActionEffect(t, effects, render.ActionPickerEdit)

	root.Shell = state.DefaultShell().OpenTerminalPool()
	_, effects = inputReducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x0f", Ctrl: true}})
	assertContentActionEffect(t, effects, render.ActionPoolAttachFloat)

	root.Shell = state.DefaultShell().OpenWorkbenchTree()
	_, effects = inputReducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x04", Ctrl: true}})
	assertContentActionEffect(t, effects, render.ActionWorkbenchDetach)
}

func TestOverlayContentActionsUseSelectedItemsAndReducers(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-logs", Channel: 12, Cols: 100, Rows: 30},
	}
	reducer := ComposeReducers(NewShellReducer(), NewTerminalPoolReducer(LiveDeps{Terminal: terminal}))
	root := state.Root{Shell: state.DefaultShell().OpenTerminalPool(), Viewport: state.ViewportStore{Valid: true, Cols: 100, Rows: 30}}
	root.TerminalPool, _ = root.TerminalPool.ApplyList(0, []state.TerminalPoolItem{{TerminalID: "term-logs", Title: "logs", State: "running", Cols: 100, Rows: 30}}, "")

	next, effects := reducer(root, ShellContentActionMsg{ActionID: render.ActionPoolAttachFloat.String(), Row: -1})
	if len(next.Shell.Floatings) != 1 || next.Shell.ActiveFloatingID != "floating-1" {
		t.Fatalf("pool ctrl-o should create floating before attach, shell=%#v", next.Shell)
	}
	var requestMsg TerminalPoolAttachRequestMsg
	foundAttach := false
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if !ok {
			continue
		}
		msg, ok := fn.Run(context.Background()).(TerminalPoolAttachRequestMsg)
		if ok {
			requestMsg = msg
			foundAttach = true
			break
		}
	}
	if !foundAttach || requestMsg.TerminalID != "term-logs" || requestMsg.TargetFloatingID != "floating-1" {
		t.Fatalf("pool ctrl-o should request selected terminal attach, msg=%#v", requestMsg)
	}
	_, effects = reducer(next, requestMsg)
	if len(effects) != 1 {
		t.Fatalf("pool attach request should emit service effect, got %#v", effects)
	}
	msg, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolAttachResultMsg)
	if !ok || msg.TerminalID != "term-logs" || msg.TargetFloatingID != "floating-1" || len(terminal.Attaches) != 1 {
		t.Fatalf("pool ctrl-o should attach selected terminal to floating, msg=%#v attaches=%#v", msg, terminal.Attaches)
	}

	root = state.Root{Shell: state.DefaultShell()}
	root.Shell, _ = root.Shell.ApplyPaneCommand(state.PaneCommand{Action: state.PaneCommandSplit, Target: state.PaneCommandTarget{PaneID: state.DefaultPaneID}, SplitDirection: state.SplitDirectionVertical, NewPane: state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-logs"}})
	root.Shell = root.Shell.OpenWorkbenchTree()
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-logs", 12, 100, 30, state.TerminalResizeRoleFollower, "surface", state.TerminalPaneViewID("pane-2"), false))
	items := state.WorkbenchTreeItems(root)
	selectedPane := false
	for index, item := range items {
		if item.Kind == state.WorkbenchTreeKindPane && item.PaneID == "pane-2" {
			root.Shell = root.Shell.OpenWorkbenchTree().SetWorkbenchTreeSelectedIndex(index, len(items))
			selectedPane = true
			break
		}
	}
	if !selectedPane {
		t.Fatalf("expected workbench tree to contain pane-2, items=%#v", items)
	}
	next, _ = NewShellReducer()(root, ShellContentActionMsg{ActionID: render.ActionWorkbenchDetach.String(), Row: -1})
	if pane, ok := next.Shell.Pane(state.PaneCommandTarget{PaneID: "pane-2"}); !ok || pane.Kind != state.PaneEmpty || pane.TerminalID != "" {
		t.Fatalf("workbench ctrl-d should detach selected pane, pane=%#v ok=%v", pane, ok)
	}
	if _, ok := next.TerminalViews.PaneBinding("pane-2"); ok {
		t.Fatal("workbench ctrl-d should detach selected pane terminal view binding")
	}
}

func TestOverlayDeleteContentActionsDispatchTerminalRemove(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell().OpenTerminalPool()}
	root.TerminalPool, _ = root.TerminalPool.ApplyList(0, []state.TerminalPoolItem{{TerminalID: "term-logs", Title: "logs"}}, "")

	next, effects := reducer(root, ShellContentActionMsg{ActionID: render.ActionPoolDelete.String(), Row: -1})
	if len(effects) != 1 || len(next.Shell.Toasts) != 0 {
		t.Fatalf("pool ctrl-x should dispatch terminal remove without local toast, effects=%#v toasts=%#v", effects, next.Shell.Toasts)
	}
	msg, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolRemoveRequestMsg)
	if !ok || msg.TerminalID != "term-logs" {
		t.Fatalf("pool ctrl-x should request terminal inventory remove, got %#v", msg)
	}
}

func assertContentActionEffect(t *testing.T, effects []Effect, actionID render.ActionID) {
	t.Helper()
	if len(effects) != 2 {
		t.Fatalf("expected handled and content action effects, got %#v", effects)
	}
	if _, ok := effects[0].(handledEffect); !ok {
		t.Fatalf("expected handled effect, got %#v", effects[0])
	}
	effect, ok := effects[1].(FuncEffect)
	if !ok || effect.Run == nil {
		t.Fatalf("expected content action func effect, got %#v", effects[1])
	}
	msg, ok := effect.Run(context.Background()).(ShellContentActionMsg)
	if !ok || msg.ActionID != actionID.String() || msg.Row != -1 {
		t.Fatalf("expected content action %s row -1, got %#v", actionID, msg)
	}
}

func assertShellMsgEffect[T Msg](t *testing.T, effects []Effect) T {
	t.Helper()
	if len(effects) != 2 {
		t.Fatalf("expected handled and shell msg effects, got %#v", effects)
	}
	if _, ok := effects[0].(handledEffect); !ok {
		t.Fatalf("expected handled effect, got %#v", effects[0])
	}
	effect, ok := effects[1].(FuncEffect)
	if !ok || effect.Run == nil {
		t.Fatalf("expected shell msg func effect, got %#v", effects[1])
	}
	msg, ok := effect.Run(context.Background()).(T)
	if !ok {
		t.Fatalf("unexpected shell msg %#v", msg)
	}
	return msg
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
