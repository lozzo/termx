package app

import (
	"context"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestTerminalPoolListResultSchedulesResourceRefreshAndLivePreview(t *testing.T) {
	terminal := &services.FakeTerminalService{
		SurfaceResult: services.TerminalSurfaceResult{
			Ready: true,
			Snapshot: state.LiveSurfaceSnapshot{
				TerminalID: "term-1",
				Revision:   2,
				Lines:      []string{"preview"},
			},
		},
	}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{Shell: state.DefaultShell().OpenTerminalPool()}
	root.TerminalPool = root.TerminalPool.RequestList()
	seq := root.TerminalPool.RequestSeq

	next, effects := reducer(root, TerminalPoolListResultMsg{
		Seq: seq,
		Result: services.TerminalListResult{Items: []services.TerminalPoolItem{{
			TerminalID: "term-1",
			Title:      "shell",
			State:      "running",
			Cols:       90,
			Rows:       20,
		}}},
	})

	if next.TerminalPool.Status != state.TerminalPoolReady || !terminalPoolRefreshLoopScheduled(effects) {
		t.Fatalf("list result should refresh inventory and arm background refresh, pool=%#v effects=%#v", next.TerminalPool, effects)
	}
	msg, ok := terminalPoolLiveSurfaceMsgFromEffects(t, effects)
	if !ok || msg.Snapshot.TerminalID != "term-1" || msg.RequestedCols != 90 || msg.RequestedRows != 20 {
		t.Fatalf("list result should refresh selected live preview, msg=%#v effects=%#v", msg, effects)
	}
	if len(terminal.Surfaces) != 1 || terminal.Surfaces[0].TerminalID != "term-1" || terminal.Surfaces[0].Cols != 90 || terminal.Surfaces[0].Rows != 20 {
		t.Fatalf("preview refresh should request selected terminal surface, got %#v", terminal.Surfaces)
	}
}

func TestTerminalPoolRefreshTickRequestsSilentList(t *testing.T) {
	terminal := &services.FakeTerminalService{
		ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{
			TerminalID: "term-1",
			Title:      "shell",
			State:      "running",
		}}},
	}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{
		Shell: state.DefaultShell().OpenTerminalPool(),
		TerminalPool: state.TerminalPoolStore{
			Status: state.TerminalPoolReady,
			Items:  []state.TerminalPoolItem{{TerminalID: "term-1", Title: "shell", State: "running"}},
		},
	}

	next, effects := reducer(root, TerminalPoolRefreshTickMsg{})
	if next.TerminalPool.Status != state.TerminalPoolReady || next.TerminalPool.RequestSeq == root.TerminalPool.RequestSeq {
		t.Fatalf("refresh tick should keep ready state and advance request seq, before=%#v after=%#v", root.TerminalPool, next.TerminalPool)
	}
	if len(effects) != 1 {
		t.Fatalf("refresh tick should only schedule list effect, got %#v", effects)
	}
	result, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolListResultMsg)
	if !ok || !result.Refresh || result.Seq != next.TerminalPool.RequestSeq {
		t.Fatalf("refresh tick should return silent list result, got %#v", result)
	}
	if len(terminal.Lists) != 1 {
		t.Fatalf("refresh tick should call terminal list once, got %#v", terminal.Lists)
	}
}

func TestTerminalPoolSelectionInputRefreshesPreview(t *testing.T) {
	reducer := NewUIInputReducer()
	root := state.Root{
		Shell: state.DefaultShell().OpenTerminalPool(),
		TerminalPool: state.TerminalPoolStore{
			Status: state.TerminalPoolReady,
			Items: []state.TerminalPoolItem{
				{TerminalID: "term-1", Title: "one"},
				{TerminalID: "term-2", Title: "two"},
			},
		},
	}

	next, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}})
	if next.Shell.EnsureDefaults().Overlay.SelectedIndex != 1 || !terminalPoolPreviewRefreshScheduled(t, effects) {
		t.Fatalf("keyboard selection should refresh preview, shell=%#v effects=%#v", next.Shell, effects)
	}
	next, effects = reducer(next, ShellOverlayMouseSelectMsg{Delta: -1})
	if next.Shell.EnsureDefaults().Overlay.SelectedIndex != 0 || !terminalPoolPreviewRefreshScheduled(t, effects) {
		t.Fatalf("mouse selection should refresh preview, shell=%#v effects=%#v", next.Shell, effects)
	}

	shellNext, shellEffects := NewShellReducer()(next, ShellContentActionMsg{ActionID: render.ActionPoolSelect.String(), Row: 1})
	if shellNext.Shell.EnsureDefaults().Overlay.SelectedIndex != 1 || !terminalPoolPreviewRefreshScheduled(t, shellEffects) {
		t.Fatalf("row select action should refresh preview, shell=%#v effects=%#v", shellNext.Shell, shellEffects)
	}
}

func terminalPoolRefreshLoopScheduled(effects []Effect) bool {
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if ok && fn.Token == terminalPoolRefreshToken && fn.Async {
			return true
		}
	}
	return false
}

func terminalPoolLiveSurfaceMsgFromEffects(t *testing.T, effects []Effect) (LiveSurfaceMsg, bool) {
	t.Helper()
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if !ok || fn.Run == nil || fn.Token == terminalPoolRefreshToken {
			continue
		}
		if msg, ok := fn.Run(context.Background()).(LiveSurfaceMsg); ok {
			return msg, true
		}
	}
	return LiveSurfaceMsg{}, false
}

func terminalPoolPreviewRefreshScheduled(t *testing.T, effects []Effect) bool {
	t.Helper()
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if !ok || fn.Run == nil {
			continue
		}
		if _, ok := fn.Run(context.Background()).(TerminalPoolPreviewRefreshMsg); ok {
			return true
		}
	}
	return false
}
