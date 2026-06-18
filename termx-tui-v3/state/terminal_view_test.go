package state

import "testing"

func TestTerminalViewStoreKeepsMultipleViewsForSameTerminal(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))

	bindings := store.BindingsForTerminal("term-1")
	if len(bindings) != 2 {
		t.Fatalf("expected two view bindings for one terminal, got %#v", bindings)
	}
	first, ok := store.PaneBinding("pane-1")
	if !ok || first.Channel != 7 || first.ResizeRole != TerminalResizeRoleOwner {
		t.Fatalf("unexpected owner binding %#v ok=%v", first, ok)
	}
	second, ok := store.PaneBinding("pane-2")
	if !ok || second.Channel != 8 || second.ResizeRole != TerminalResizeRoleFollower {
		t.Fatalf("unexpected follower binding %#v ok=%v", second, ok)
	}
}

func TestTerminalViewStoreDetachAndKillHaveDifferentScope(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))

	store = store.DetachPane("pane-1")
	if _, ok := store.PaneBinding("pane-1"); ok {
		t.Fatal("detached pane should lose its view binding")
	}
	second, ok := store.PaneBinding("pane-2")
	if !ok {
		t.Fatal("sibling pane binding should survive detach")
	}
	if second.ResizeRole != TerminalResizeRoleOwner || !second.CanResize {
		t.Fatalf("sibling pane should inherit resize owner after owner detach, got %#v", second)
	}
	if !second.ResizePending {
		t.Fatalf("sibling pane promoted to resize owner must require one resize check, got %#v", second)
	}

	store = store.RemoveTerminal("term-1")
	if bindings := store.BindingsForTerminal("term-1"); len(bindings) != 0 {
		t.Fatalf("kill terminal should remove all view bindings, got %#v", bindings)
	}
}

func TestTerminalViewStoreMarkTerminalReattachingClearsOnlyRestartedChannels(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))
	store = store.BindPane(NewPaneTerminalView("pane-3", "term-2", 9, 100, 30, TerminalResizeRoleOwner, "surface", "view-3", true))

	store = store.MarkTerminalReattaching("term-1")
	first, _ := store.PaneBinding("pane-1")
	second, _ := store.PaneBinding("pane-2")
	third, _ := store.PaneBinding("pane-3")
	if first.Channel != 0 || first.Attached || first.TerminalID != "term-1" || first.ResizeRole != TerminalResizeRoleOwner {
		t.Fatalf("restart should keep owner binding intent but clear old channel, got %#v", first)
	}
	if second.Channel != 0 || second.Attached || second.TerminalID != "term-1" || second.ResizeRole != TerminalResizeRoleFollower {
		t.Fatalf("restart should keep follower binding intent but clear old channel, got %#v", second)
	}
	if third.Channel != 9 || !third.Attached || third.TerminalID != "term-2" {
		t.Fatalf("unrelated terminal binding must be preserved, got %#v", third)
	}
}

func TestTerminalViewStoreTransfersResizeOwnerWithinTerminal(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))
	store = store.BindPane(NewPaneTerminalView("pane-3", "term-2", 9, 100, 30, TerminalResizeRoleOwner, "surface", "view-3", true))

	store = store.TransferPaneResizeOwner("pane-2")
	first, _ := store.PaneBinding("pane-1")
	second, _ := store.PaneBinding("pane-2")
	third, _ := store.PaneBinding("pane-3")
	if first.ResizeRole != TerminalResizeRoleFollower || first.CanResize {
		t.Fatalf("previous owner should become follower, got %#v", first)
	}
	if second.ResizeRole != TerminalResizeRoleOwner || !second.CanResize {
		t.Fatalf("target pane should become owner, got %#v", second)
	}
	if !second.ResizePending {
		t.Fatalf("target pane should force one owner resize check after transfer, got %#v", second)
	}
	if third.ResizeRole != TerminalResizeRoleOwner || !third.CanResize {
		t.Fatalf("unrelated terminal owner should be preserved, got %#v", third)
	}
}

func TestTerminalViewStoreDemotesPreviousOwnerOnBind(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleOwner, "surface", "view-2", true))
	store = store.BindPane(NewPaneTerminalView("pane-3", "term-1", 9, 30, 10, TerminalResizeRoleFollower, "surface", "view-3", false))

	first, _ := store.PaneBinding("pane-1")
	second, _ := store.PaneBinding("pane-2")
	third, _ := store.PaneBinding("pane-3")
	if first.ResizeRole != TerminalResizeRoleFollower || first.CanResize {
		t.Fatalf("previous owner should be demoted by new owner bind, got %#v", first)
	}
	if second.ResizeRole != TerminalResizeRoleOwner || !second.CanResize {
		t.Fatalf("new owner should stay owner, got %#v", second)
	}
	if third.ResizeRole != TerminalResizeRoleFollower || third.CanResize {
		t.Fatalf("follower bind should not become owner, got %#v", third)
	}
}

func TestTerminalViewStoreDoesNotPromoteOwnerRoleToResizePermission(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", false))

	binding, _ := store.PaneBinding("pane-1")
	if binding.ResizeRole != TerminalResizeRoleOwner || binding.CanResize || !binding.HasResizeOwner() || binding.HasAuthoritativeResizeOwner() {
		t.Fatalf("locked owner should remain owner without resize permission, got %#v", binding)
	}
}

func TestTerminalViewStoreResizeControlDemotesPreviousOwner(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))

	store, applied := store.ApplyResizeControl("view-2", TerminalResizeControlProjection{ResizeRole: TerminalResizeRoleOwner, CanResize: true})
	if !applied {
		t.Fatal("expected resize control projection to apply")
	}
	first, _ := store.PaneBinding("pane-1")
	second, _ := store.PaneBinding("pane-2")
	if first.ResizeRole != TerminalResizeRoleFollower || first.CanResize {
		t.Fatalf("previous owner should be demoted by control projection, got %#v", first)
	}
	if second.ResizeRole != TerminalResizeRoleOwner || !second.CanResize {
		t.Fatalf("projected owner should become owner, got %#v", second)
	}
}

func TestTerminalViewStoreResizeControlDoesNotDemoteForStaleOwnerProjection(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))

	store, applied := store.ApplyResizeControl("view-2", TerminalResizeControlProjection{ResizeRole: TerminalResizeRoleOwner, CanResize: true, OwnerViewID: "view-1"})
	if !applied {
		t.Fatal("expected resize control projection to apply")
	}
	first, _ := store.PaneBinding("pane-1")
	second, _ := store.PaneBinding("pane-2")
	if first.ResizeRole != TerminalResizeRoleOwner || !first.CanResize {
		t.Fatalf("stale owner projection must not demote current owner, got %#v", first)
	}
	if second.ResizeRole != TerminalResizeRoleOwner || !second.CanResize || second.OwnerViewID != "view-1" {
		t.Fatalf("projected binding should retain control metadata without becoming authoritative, got %#v", second)
	}
}

func TestTerminalViewStoreResizeRequestRequiresOwnerAndGuardsStaleResults(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))

	unchanged, follower := store.RequestPaneResize("pane-2", 50, 14)
	if follower.Allowed || follower.Changed || follower.Reason != "not-owner" {
		t.Fatalf("follower must not request PTY resize, decision=%#v", follower)
	}
	if got, _ := unchanged.PaneBinding("pane-2"); got.DesiredCols != 40 || got.DesiredRows != 12 || got.RequestSeq != 0 {
		t.Fatalf("follower resize must not mutate binding, got %#v", got)
	}

	next, owner := store.RequestPaneResize("pane-1", 100, 30)
	if !owner.Allowed || !owner.Changed || owner.Seq != 1 {
		t.Fatalf("owner resize should advance request seq, decision=%#v", owner)
	}
	stale, applied := next.ApplyResizeResult("view-1", 0, 90, 20, "")
	if !applied {
		t.Fatalf("zero seq bootstrap result should apply")
	}
	current, _ := stale.PaneBinding("pane-1")
	if current.DesiredCols != 90 || current.DesiredRows != 20 {
		t.Fatalf("bootstrap result should update desired size, got %#v", current)
	}
	stale, newer := stale.RequestPaneResize("pane-1", 110, 32)
	if !newer.Changed || newer.Seq != 2 {
		t.Fatalf("newer owner resize should advance pending seq, decision=%#v", newer)
	}
	stale, applied = stale.ApplyResizeResult("view-1", 1, 80, 24, "")
	if applied {
		t.Fatalf("old result should be stale after newer request")
	}
	current, _ = stale.PaneBinding("pane-1")
	if current.DesiredCols != 110 || current.DesiredRows != 32 {
		t.Fatalf("stale result must not overwrite desired size, got %#v", current)
	}
}

func TestTerminalViewStoreOwnerTransferForcesSameSizeResizeOnce(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 80, 24, TerminalResizeRoleFollower, "surface", "view-2", false))
	store = store.TransferPaneResizeOwner("pane-2")

	next, decision := store.RequestPaneResize("pane-2", 80, 24)
	if !decision.Allowed || !decision.Changed || decision.Seq != 1 {
		t.Fatalf("new owner must issue one same-size resize check, decision=%#v", decision)
	}
	binding, _ := next.PaneBinding("pane-2")
	if binding.ResizePending || binding.RequestSeq != 1 || binding.DesiredCols != 80 || binding.DesiredRows != 24 {
		t.Fatalf("resize check should clear pending and keep desired size, got %#v", binding)
	}
	_, duplicate := next.RequestPaneResize("pane-2", 80, 24)
	if !duplicate.Allowed || duplicate.Changed || duplicate.Reason != "unchanged" {
		t.Fatalf("same-size resize after pending check must dedupe, decision=%#v", duplicate)
	}
}

func TestTerminalViewLayoutStateIsViewLocal(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", "view-2", false))

	next, changed, ok := store.ApplyPaneLayoutCommand("pane-1", TerminalViewLayoutCommand{Action: "pan", DeltaX: 4, DeltaY: -2})
	if !ok {
		t.Fatal("expected pane layout command to apply")
	}
	if changed.Layout.PanX != 4 || changed.Layout.PanY != -2 {
		t.Fatalf("expected pane-1 pan to change, got %#v", changed.Layout)
	}
	sibling, _ := next.PaneBinding("pane-2")
	if sibling.Layout.Normalize().PanX != 0 || sibling.Layout.Normalize().PanY != 0 {
		t.Fatalf("sibling binding for same terminal must keep independent layout, got %#v", sibling.Layout)
	}
}

func TestTerminalViewLayoutCommandsNormalizeAndReset(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))

	store, binding, ok := store.ApplyPaneLayoutCommand("pane-1", TerminalViewLayoutCommand{Action: "toggle-lock"})
	if !ok || !binding.Layout.SizeLocked || binding.Layout.Mode != TerminalViewLayoutAuto {
		t.Fatalf("lock should toggle on default normalized layout, binding=%#v ok=%v", binding, ok)
	}
	store, binding, _ = store.ApplyPaneLayoutCommand("pane-1", TerminalViewLayoutCommand{Action: "toggle-layout"})
	if binding.Layout.Mode != TerminalViewLayoutFit {
		t.Fatalf("layout toggle should move auto to fit, got %#v", binding.Layout)
	}
	store, binding, _ = store.ApplyPaneLayoutCommand("pane-1", TerminalViewLayoutCommand{Action: "center"})
	if binding.Layout.Mode != TerminalViewLayoutCenter || binding.Layout.AlignX != TerminalViewAlignCenter || binding.Layout.AlignY != TerminalViewAlignCenter {
		t.Fatalf("center should set centered layout, got %#v", binding.Layout)
	}
	_, binding, _ = store.ApplyPaneLayoutCommand("pane-1", TerminalViewLayoutCommand{Action: "reset"})
	if binding.Layout.SizeLocked || binding.Layout.Mode != TerminalViewLayoutAuto || binding.Layout.AlignX != TerminalViewAlignStart || binding.Layout.AlignY != TerminalViewAlignStart {
		t.Fatalf("reset should restore normalized default layout, got %#v", binding.Layout)
	}
}

func TestTerminalViewApplyTerminalSizeLockKeepsOwnerIdentityAndBlocksResize(t *testing.T) {
	store := TerminalViewStore{}
	store = store.BindPane(NewPaneTerminalView("pane-1", "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", "view-1", true))
	store = store.BindPane(NewPaneTerminalView("pane-2", "term-1", 8, 80, 24, TerminalResizeRoleFollower, "surface", "view-2", false))

	store = store.ApplyTerminalSizeLock("term-1", true)
	owner, _ := store.PaneBinding("pane-1")
	follower, _ := store.PaneBinding("pane-2")
	if owner.ResizeRole != TerminalResizeRoleOwner || !owner.HasResizeOwner() || owner.HasAuthoritativeResizeOwner() || !owner.SizeLocked || owner.ControlReason != "size_locked" {
		t.Fatalf("locked owner should keep owner identity without resize authority, got %#v", owner)
	}
	if follower.HasResizeOwner() || follower.CanResize || !follower.SizeLocked || follower.ControlReason != "size_locked" {
		t.Fatalf("follower should inherit terminal lock without owner identity, got %#v", follower)
	}
	_, decision := store.RequestPaneResize("pane-1", 100, 30)
	if decision.Allowed || decision.Reason != "size-locked" {
		t.Fatalf("locked owner must not allow resize requests, got %#v", decision)
	}

	store = store.BindPane(NewPaneTerminalView("pane-3", "term-1", 9, 80, 24, TerminalResizeRoleOwner, "surface", "view-3", true))
	store = store.TransferPaneResizeOwner("pane-3")
	newOwner, _ := store.PaneBinding("pane-3")
	if newOwner.ResizeRole != TerminalResizeRoleOwner || newOwner.CanResize || !newOwner.SizeLocked || newOwner.ControlReason != "size_locked" {
		t.Fatalf("owner transfer on locked terminal must preserve lock and block resize, got %#v", newOwner)
	}
	oldOwner, _ := store.PaneBinding("pane-1")
	if oldOwner.SizeLocked != true || oldOwner.CanResize {
		t.Fatalf("previous view should stay locked after owner transfer, got %#v", oldOwner)
	}

	store = store.ApplyTerminalSizeLock("term-1", false)
	owner, _ = store.PaneBinding("pane-3")
	follower, _ = store.PaneBinding("pane-2")
	if owner.SizeLocked || !owner.CanResize || owner.ControlReason != "" {
		t.Fatalf("unlock should restore owner resize authority, got %#v", owner)
	}
	if follower.SizeLocked || follower.CanResize || follower.ControlReason != "" {
		t.Fatalf("unlock should clear follower lock projection without granting authority, got %#v", follower)
	}
}
