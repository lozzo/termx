package state

import "testing"

func TestDefaultShellOwnsWorkbenchTreeAndChromeState(t *testing.T) {
	shell := DefaultShell()

	if shell.Workspace.ID != DefaultWorkspaceID || shell.Workspace.ActiveTabID != DefaultTabID {
		t.Fatalf("unexpected default workspace %#v", shell.Workspace)
	}
	if len(shell.Workspace.Tabs) != 1 || len(shell.Workspace.Tabs[0].Panes) != 1 {
		t.Fatalf("expected one tab and one pane, got %#v", shell.Workspace)
	}
	if shell.ActivePaneID != DefaultPaneID {
		t.Fatalf("unexpected active pane %q", shell.ActivePaneID)
	}
	if shell.PanelPresentation != PanelPresentationCard {
		t.Fatalf("expected card presentation, got %q", shell.PanelPresentation)
	}
	if !shell.HeaderVisible || !shell.FooterVisible {
		t.Fatalf("header/footer must default visible, got header=%v footer=%v", shell.HeaderVisible, shell.FooterVisible)
	}
	if !shell.Workspace.Tabs[0].Panes[0].Active {
		t.Fatalf("default pane must be active %#v", shell.Workspace.Tabs[0].Panes[0])
	}
}

func TestShellPanelPresentationCanToggleBetweenCardAndSplit(t *testing.T) {
	shell := DefaultShell()

	shell = shell.TogglePanelPresentation()
	if shell.PanelPresentation != PanelPresentationSplitLine {
		t.Fatalf("expected split line, got %q", shell.PanelPresentation)
	}
	shell = shell.TogglePanelPresentation()
	if shell.PanelPresentation != PanelPresentationCard {
		t.Fatalf("expected card, got %q", shell.PanelPresentation)
	}
	shell = shell.SetPanelPresentation("invalid")
	if shell.PanelPresentation != PanelPresentationCard {
		t.Fatalf("invalid presentation should be ignored, got %q", shell.PanelPresentation)
	}
}

func TestShellHeaderFooterVisibilityCanBeHiddenAndRestored(t *testing.T) {
	shell := DefaultShell()

	shell = shell.SetHeaderVisible(false).SetFooterVisible(false)
	if shell.HeaderVisible || shell.FooterVisible {
		t.Fatalf("expected hidden header/footer, got header=%v footer=%v", shell.HeaderVisible, shell.FooterVisible)
	}
	shell = shell.ToggleHeaderVisible().ToggleFooterVisible()
	if !shell.HeaderVisible || !shell.FooterVisible {
		t.Fatalf("expected restored header/footer, got header=%v footer=%v", shell.HeaderVisible, shell.FooterVisible)
	}
}

func TestShellWorkbenchTabCommandsManageActiveTab(t *testing.T) {
	shell := DefaultShell()

	var result WorkbenchCommandResult
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabCreate, Name: "build"})
	if result.Status != WorkbenchCommandOK || result.ID == "" {
		t.Fatalf("expected tab create ok, result=%#v shell=%#v", result, shell)
	}
	if len(shell.Workspace.Tabs) != 2 || shell.Workspace.ActiveTabID != result.ID || shell.ActivePaneID != "" {
		t.Fatalf("expected new empty tab active, result=%#v shell=%#v", result, shell)
	}
	if active := shell.activeTab(); len(active.Panes) != 0 || active.RootSplit.PaneID != "" || active.RootSplit.Direction != "" || len(active.RootSplit.Children) != 0 {
		t.Fatalf("new tab must not synthesize a pane, got %#v", active)
	}
	buildTabID := result.ID
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabRename, Name: "构建🚀"})
	if result.Status != WorkbenchCommandOK || shell.activeTab().Title != "构建🚀" {
		t.Fatalf("expected tab rename, result=%#v tab=%#v", result, shell.activeTab())
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabPrevious})
	if result.Status != WorkbenchCommandOK || shell.Workspace.ActiveTabID != DefaultTabID || shell.ActivePaneID != DefaultPaneID {
		t.Fatalf("expected previous tab active, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabSwitch, TargetID: buildTabID})
	if result.Status != WorkbenchCommandOK || shell.Workspace.ActiveTabID != buildTabID || shell.ActivePaneID != "" {
		t.Fatalf("expected switch tab active, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabSwitch, TargetID: "missing-tab"})
	if result.Status != WorkbenchCommandInvalid || shell.Workspace.ActiveTabID != buildTabID {
		t.Fatalf("missing tab switch must be rejected without changing active tab, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabSwitch, TargetID: DefaultTabID})
	if result.Status != WorkbenchCommandOK || shell.Workspace.ActiveTabID != DefaultTabID {
		t.Fatalf("expected switch back to main tab, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabClose})
	if result.Status != WorkbenchCommandOK || len(shell.Workspace.Tabs) != 1 || shell.Workspace.ActiveTabID != buildTabID {
		t.Fatalf("expected close current tab and keep remaining active, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabClose})
	if result.Status != WorkbenchCommandOK || len(shell.Workspace.Tabs) != 0 || shell.Workspace.ActiveTabID != "" || shell.ActivePaneID != "" {
		t.Fatalf("last tab close must leave an empty workspace, result=%#v shell=%#v", result, shell)
	}
	shell = shell.EnsureDefaults()
	if len(shell.Workspace.Tabs) != 0 || shell.Workspace.ActiveTabID != "" || shell.ActivePaneID != "" {
		t.Fatalf("empty workspace must survive defaults, got %#v", shell)
	}
}

func TestShellEmptyWorkspaceCreatesTabForTerminalAttach(t *testing.T) {
	shell := DefaultShell()
	var result WorkbenchCommandResult
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabClose})
	if result.Status != WorkbenchCommandOK || len(shell.Workspace.Tabs) != 0 {
		t.Fatalf("expected empty workspace, result=%#v shell=%#v", result, shell)
	}

	shell = shell.EnsureActiveTabForAttach().BindPaneTerminal(PaneCommandTarget{}, "term-main")
	if len(shell.Workspace.Tabs) != 1 || shell.Workspace.ActiveTabID == "" || shell.ActivePaneID == "" {
		t.Fatalf("attach must create a real tab and pane, got %#v", shell)
	}
	pane, ok := shell.Pane(PaneCommandTarget{PaneID: shell.ActivePaneID})
	if !ok || pane.TerminalID != "term-main" || pane.Kind != PaneTerminalLive {
		t.Fatalf("expected terminal-bound first pane, pane=%#v ok=%v shell=%#v", pane, ok, shell)
	}
}

func TestShellWorkbenchWorkspaceCommandsSwitchAndRename(t *testing.T) {
	shell := DefaultShell()

	var result WorkbenchCommandResult
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandWorkspaceCreate, Name: "work-2"})
	if result.Status != WorkbenchCommandOK || result.ID == "" {
		t.Fatalf("expected workspace create ok, result=%#v shell=%#v", result, shell)
	}
	if shell.Workspace.ID != result.ID || shell.Workspace.Name != "work-2" || len(shell.Workspaces) != 2 {
		t.Fatalf("expected new workspace active and stored, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandWorkspaceRename, Name: "云端🚀"})
	if result.Status != WorkbenchCommandOK || shell.Workspace.Name != "云端🚀" {
		t.Fatalf("expected active workspace rename, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandWorkspacePrevious})
	if result.Status != WorkbenchCommandOK || shell.Workspace.ID != DefaultWorkspaceID || shell.ActivePaneID != DefaultPaneID {
		t.Fatalf("expected previous workspace active, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandWorkspaceNext})
	if result.Status != WorkbenchCommandOK || shell.Workspace.Name != "云端🚀" {
		t.Fatalf("expected next workspace to restore renamed workspace, result=%#v shell=%#v", result, shell)
	}

	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandWorkspaceSwitch, TargetID: DefaultWorkspaceID})
	if result.Status != WorkbenchCommandOK || shell.Workspace.ID != DefaultWorkspaceID {
		t.Fatalf("expected explicit workspace switch, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandWorkspaceDelete, TargetID: "workspace-2"})
	if result.Status != WorkbenchCommandNeedsConfirmation || len(shell.Workspaces) != 2 {
		t.Fatalf("workspace delete must require confirmation, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandWorkspaceDelete, TargetID: "workspace-2", Confirm: PaneConfirmAccepted})
	if result.Status != WorkbenchCommandOK || len(shell.Workspaces) != 1 || shell.Workspaces[0].ID != DefaultWorkspaceID {
		t.Fatalf("expected confirmed workspace delete, result=%#v shell=%#v", result, shell)
	}

	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandWorkspaceCreate, Name: "ephemeral"})
	if result.Status != WorkbenchCommandOK || shell.Workspace.ID == DefaultWorkspaceID {
		t.Fatalf("expected new active workspace for active-delete regression, result=%#v shell=%#v", result, shell)
	}
	activeID := shell.Workspace.ID
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandWorkspaceDelete, TargetID: activeID, Confirm: PaneConfirmAccepted})
	if result.Status != WorkbenchCommandOK || result.ID != activeID {
		t.Fatalf("expected active workspace delete ok, result=%#v shell=%#v", result, shell)
	}
	if shell.Workspace.ID == activeID || workspaceIndexByID(shell.Workspaces, activeID) >= 0 || len(shell.Workspaces) != 1 {
		t.Fatalf("active workspace delete must not reinsert deleted workspace, active=%q shell=%#v", activeID, shell)
	}
}

func TestShellWorkbenchTabKillAndPaneCRUDDistinguishTerminalLifecycle(t *testing.T) {
	shell := DefaultShell().
		SplitActivePane(PaneState{ID: "pane-logs", Title: "logs", Kind: PaneTerminalLive, TerminalID: "term-logs"}, SplitDirectionVertical).
		FocusPane(PaneCommandTarget{PaneID: "pane-logs"})

	var result WorkbenchCommandResult
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandPaneDetach, Target: PaneCommandTarget{PaneID: "pane-logs"}})
	pane, _ := shell.Pane(PaneCommandTarget{PaneID: "pane-logs"})
	if result.Status != WorkbenchCommandOK || pane.Kind != PaneEmpty || pane.TerminalID != "" {
		t.Fatalf("pane detach should keep pane and drop terminal binding only, result=%#v pane=%#v", result, pane)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandPaneRename, Target: PaneCommandTarget{PaneID: "pane-logs"}, Name: "日志"})
	pane, _ = shell.Pane(PaneCommandTarget{PaneID: "pane-logs"})
	if result.Status != WorkbenchCommandOK || pane.Title != "日志" {
		t.Fatalf("pane rename should update workbench schema, result=%#v pane=%#v", result, pane)
	}

	shell = shell.BindPaneTerminal(PaneCommandTarget{PaneID: "pane-logs"}, "term-logs")
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandPaneKill, Target: PaneCommandTarget{PaneID: "pane-logs"}})
	if result.Status != WorkbenchCommandNeedsConfirmation || shell.ActivePaneID != "pane-logs" {
		t.Fatalf("pane kill must require confirmation without mutation, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandPaneKill, Target: PaneCommandTarget{PaneID: "pane-logs"}, Confirm: PaneConfirmAccepted})
	if result.Status != WorkbenchCommandOK || result.ID != "pane-logs" || len(result.Killed) != 1 || result.Killed[0] != "term-logs" || shell.HasPane(PaneCommandTarget{PaneID: "pane-logs"}) {
		t.Fatalf("pane kill should close pane and report terminal kill boundary, result=%#v shell=%#v", result, shell)
	}

	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabCreate, TargetID: "tab-logs", Name: "logs"})
	if result.Status != WorkbenchCommandOK {
		t.Fatalf("expected tab create, result=%#v", result)
	}
	shell = shell.BindPaneTerminal(PaneCommandTarget{TabID: "tab-logs"}, "term-tab")
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabKill, TargetID: "tab-logs", Confirm: PaneConfirmAccepted})
	if result.Status != WorkbenchCommandOK || result.ID != "tab-logs" || len(result.Killed) != 1 || result.Killed[0] != "term-tab" || shell.Workspace.ActiveTabID == "tab-logs" {
		t.Fatalf("tab kill should close tab and report terminal ids, result=%#v shell=%#v", result, shell)
	}
}

func TestShellToastLifecycle(t *testing.T) {
	shell := DefaultShell().
		AddToast(ToastSpec{ID: "keep", Severity: ToastInfo, Title: "sync"}).
		AddToast(ToastSpec{ID: "drop", Severity: ToastWarning, Title: "warn", DismissAfterTicks: 2})

	shell = shell.TickToasts(1)
	if len(shell.Toasts) != 2 || shell.Toasts[1].AgeTicks != 1 {
		t.Fatalf("expected both toasts after one tick, got %#v", shell.Toasts)
	}
	shell = shell.TickToasts(1)
	if len(shell.Toasts) != 1 || shell.Toasts[0].ID != "keep" {
		t.Fatalf("expected auto-dismissed toast, got %#v", shell.Toasts)
	}
	shell = shell.AddToast(ToastSpec{ID: "close", Severity: ToastSuccess, Title: "done"})
	shell = shell.CloseCurrentToast()
	if len(shell.Toasts) != 1 || shell.Toasts[0].ID != "keep" {
		t.Fatalf("expected close current to remove latest toast, got %#v", shell.Toasts)
	}
	shell = shell.ClearToasts()
	if len(shell.Toasts) != 0 {
		t.Fatalf("expected clear all toasts, got %#v", shell.Toasts)
	}
}

func TestShellToastDefaultDismissTicks(t *testing.T) {
	shell := DefaultShell().
		AddToast(ToastSpec{ID: "info", Severity: ToastInfo, Title: "info"}).
		AddToast(ToastSpec{ID: "error", Severity: ToastError, Title: "error"}).
		AddToast(ToastSpec{ID: "pending", Severity: ToastWarning, Title: "pending", Pending: true})

	if shell.Toasts[0].DismissAfterTicks != defaultToastDismissTicks {
		t.Fatalf("expected info default dismiss ticks, got %#v", shell.Toasts[0])
	}
	if shell.Toasts[1].DismissAfterTicks != attentionToastDismissTicks {
		t.Fatalf("expected error default dismiss ticks, got %#v", shell.Toasts[1])
	}
	if shell.Toasts[2].DismissAfterTicks != pendingToastDismissTicks {
		t.Fatalf("expected pending default dismiss ticks, got %#v", shell.Toasts[2])
	}

	shell = shell.TickToasts(defaultToastDismissTicks)
	if len(shell.Toasts) != 2 || shell.Toasts[0].ID != "error" || shell.Toasts[1].ID != "pending" {
		t.Fatalf("expected short info toast to disappear first, got %#v", shell.Toasts)
	}
	shell = shell.TickToasts(attentionToastDismissTicks - defaultToastDismissTicks)
	if len(shell.Toasts) != 1 || shell.Toasts[0].ID != "pending" {
		t.Fatalf("expected error toast to disappear before pending, got %#v", shell.Toasts)
	}
	shell = shell.TickToasts(pendingToastDismissTicks - attentionToastDismissTicks)
	if len(shell.Toasts) != 0 {
		t.Fatalf("expected pending toast to have explicit lifecycle, got %#v", shell.Toasts)
	}
}

func TestShellToastDeduplicatesByVisibleMessage(t *testing.T) {
	shell := DefaultShell().
		AddToast(ToastSpec{ID: "first", Severity: ToastInfo, Title: "floating.move", Body: "floating-1"}).
		TickToasts(2).
		AddToast(ToastSpec{Severity: ToastInfo, Title: "floating.move", Body: "floating-1"})

	if len(shell.Toasts) != 1 {
		t.Fatalf("expected duplicate toast to be refreshed, got %#v", shell.Toasts)
	}
	if shell.Toasts[0].ID != "first" || shell.Toasts[0].AgeTicks != 0 {
		t.Fatalf("expected refreshed duplicate to keep stable id and reset age, got %#v", shell.Toasts[0])
	}

	shell = shell.
		AddToast(ToastSpec{Severity: ToastWarning, Title: "floating.move", Body: "blocked"}).
		AddToast(ToastSpec{Severity: ToastWarning, Title: "floating.move", Body: "blocked"})
	if len(shell.Toasts) != 2 || shell.Toasts[1].AgeTicks != 0 {
		t.Fatalf("expected severity/body scoped dedupe without losing distinct messages, got %#v", shell.Toasts)
	}
}

func TestShellToastAutoIDAndDefaultSeverity(t *testing.T) {
	shell := DefaultShell().AddToast(ToastSpec{Title: "hello"})
	if len(shell.Toasts) != 1 {
		t.Fatalf("expected one toast, got %#v", shell.Toasts)
	}
	if shell.Toasts[0].ID != "toast-1" || shell.Toasts[0].Severity != ToastInfo {
		t.Fatalf("unexpected generated toast %#v", shell.Toasts[0])
	}
}

func TestShellTerminalPickerOverlayTargetsActivePane(t *testing.T) {
	shell := DefaultShell().OpenTerminalPicker()

	if !shell.Overlay.Open || shell.Overlay.Kind != OverlayTerminalPicker {
		t.Fatalf("expected terminal picker overlay, got %#v", shell.Overlay)
	}
	if shell.Overlay.TargetID != DefaultPaneID {
		t.Fatalf("expected overlay target active pane, got %#v", shell.Overlay)
	}
	shell = shell.CloseOverlay()
	if shell.Overlay.Open || shell.Overlay.Kind != OverlayNone {
		t.Fatalf("expected closed overlay, got %#v", shell.Overlay)
	}
}

func TestTerminalPickerStateFiltersAndMovesSelection(t *testing.T) {
	shell := DefaultShell().
		SplitActivePane(PaneState{ID: "pane-2", Title: "日志🚀", Kind: PaneTerminalLive, TerminalID: "term-2"}, SplitDirectionVertical).
		FocusPane(PaneCommandTarget{PaneID: DefaultPaneID}).
		OpenTerminalPicker()
	root := Root{Shell: shell, Session: TerminalSessionStore{TerminalID: "term-main"}}

	items := TerminalPickerItems(root)
	if len(items) != 2 || !items[0].Selected || !items[0].CreateNew || items[1].TerminalID != "term-main" || items[1].PaneID != "" {
		t.Fatalf("expected create row plus terminal row, got %#v", items)
	}
	root.Shell = root.Shell.SetTerminalPickerQuery("日志")
	items = TerminalPickerItems(root)
	if len(items) != 0 {
		t.Fatalf("query should filter terminal-only rows, got %#v", items)
	}
	root.Shell = root.Shell.SetTerminalPickerQuery("")
	root.Shell = root.Shell.MoveTerminalPickerSelection(1, len(TerminalPickerItems(root)))
	items = TerminalPickerItems(root)
	if len(items) != 2 || !items[1].Selected || items[1].TerminalID != "term-main" {
		t.Fatalf("selection should move to terminal row, got %#v", items)
	}
}

func TestTerminalPickerItemsShowOnlyTerminalPoolInfo(t *testing.T) {
	shell := DefaultShell().OpenTerminalPicker()
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-main"
	root := Root{
		Shell: shell,
		TerminalPool: TerminalPoolStore{
			Status: TerminalPoolReady,
			Items: []TerminalPoolItem{
				{TerminalID: "term-main", Title: "main", State: "running", Cols: 80, Rows: 24},
				{TerminalID: "term-pool", Title: "远程🚀", State: "running", Cols: 100, Rows: 30},
			},
		},
	}

	items := TerminalPickerItems(root)
	if len(items) != 3 || !items[0].CreateNew || items[1].PaneID != "" || items[1].Location != "" || items[1].CreateNew || !items[1].FromPool || items[2].TerminalID != "term-pool" || items[2].Cols != 100 || items[2].Rows != 30 {
		t.Fatalf("expected create row plus terminal-only pool rows, got %#v", items)
	}
	root.Shell = root.Shell.SetTerminalPickerQuery("远程")
	items = TerminalPickerItems(root)
	if len(items) != 1 || !items[0].Selected || items[0].TerminalID != "term-pool" {
		t.Fatalf("expected query to match pool terminal item, got %#v", items)
	}
	root.Shell = root.Shell.SetTerminalPickerQuery("trpl")
	items = TerminalPickerItems(root)
	if len(items) != 1 || !items[0].Selected || items[0].TerminalID != "term-pool" {
		t.Fatalf("expected fuzzy query to match term-pool, got %#v", items)
	}
	if got := TerminalPickerQueryMatchIndexes("term-pool", "trpl"); len(got) != 4 || got[0] != 0 || got[1] != 2 || got[2] != 5 || got[3] != 8 {
		t.Fatalf("unexpected fuzzy match indexes for term-pool/trpl: %#v", got)
	}
}

func TestTerminalPoolStoreAppliesListWithStaleGuardAndError(t *testing.T) {
	pool := TerminalPoolStore{}
	pool = pool.RequestList()
	firstSeq := pool.RequestSeq
	pool = pool.RequestList()
	secondSeq := pool.RequestSeq

	next, applied := pool.ApplyList(firstSeq, []TerminalPoolItem{{TerminalID: "old"}}, "")
	if applied || len(next.Items) != 0 {
		t.Fatalf("stale list result must be ignored, got applied=%v pool=%#v", applied, next)
	}
	next, applied = pool.ApplyList(secondSeq, []TerminalPoolItem{{TerminalID: "term-1", Title: "main"}}, "")
	if !applied || next.Status != TerminalPoolReady || len(next.Items) != 1 || next.Items[0].TerminalID != "term-1" {
		t.Fatalf("expected fresh list applied, got applied=%v pool=%#v", applied, next)
	}
	next, applied = next.ApplyList(secondSeq, nil, "boom")
	if !applied || next.Status != TerminalPoolError || next.LastError != "boom" {
		t.Fatalf("expected error state, got applied=%v pool=%#v", applied, next)
	}
}

func TestWorkbenchTreeItemsProjectStructureSearchAndSelection(t *testing.T) {
	shell := DefaultShell().
		SplitActivePane(PaneState{ID: "pane-2", Title: "日志🚀", Kind: PaneTerminalLive, TerminalID: "term-2"}, SplitDirectionVertical).
		FocusPane(PaneCommandTarget{PaneID: DefaultPaneID}).
		OpenWorkbenchTree()
	root := Root{Shell: shell, Session: TerminalSessionStore{TerminalID: "term-main"}}

	items := WorkbenchTreeItems(root)
	if len(items) != 5 {
		t.Fatalf("expected workspace/tab/two panes/floating rows, got %#v", items)
	}
	if items[0].Kind != WorkbenchTreeKindWorkspace || !items[0].Selected {
		t.Fatalf("expected workspace selected first, got %#v", items)
	}
	if items[3].Kind != WorkbenchTreeKindPane || items[3].PaneID != "pane-2" || items[3].TerminalID != "term-2" {
		t.Fatalf("expected pane-2 row with terminal binding, got %#v", items[3])
	}
	if items[4].Kind != WorkbenchTreeKindFloating || items[4].Summary != "float:0" {
		t.Fatalf("expected floating summary row, got %#v", items[4])
	}

	root.Shell = root.Shell.SetWorkbenchTreeQuery("日志")
	items = WorkbenchTreeItems(root)
	if len(items) != 1 || items[0].PaneID != "pane-2" || !items[0].Selected {
		t.Fatalf("query should filter to matching wide-char pane row, got %#v", items)
	}
	root.Shell = root.Shell.SetWorkbenchTreeQuery("")
	root.Shell = root.Shell.MoveWorkbenchTreeSelection(2, len(WorkbenchTreeItems(root)))
	items = WorkbenchTreeItems(root)
	if len(items) != 5 || !items[2].Selected || items[2].PaneID != DefaultPaneID {
		t.Fatalf("selection should move to default pane row, got %#v", items)
	}
}

func TestShellSplitActivePaneCreatesMinimalPaneTree(t *testing.T) {
	shell := DefaultShell().SplitActivePane(PaneState{
		ID:         "pane-2",
		Title:      "logs",
		Kind:       PaneTerminalLive,
		TerminalID: "term-2",
	}, SplitDirectionHorizontal)

	tab := shell.Workspace.Tabs[0]
	if len(tab.Panes) != 2 {
		t.Fatalf("expected two panes, got %#v", tab.Panes)
	}
	if shell.ActivePaneID != "pane-2" || tab.ActivePaneID != "pane-2" {
		t.Fatalf("expected new pane active, shell=%#v tab=%#v", shell, tab)
	}
	if tab.RootSplit.Direction != SplitDirectionHorizontal || len(tab.RootSplit.Children) != 2 {
		t.Fatalf("expected horizontal root split, got %#v", tab.RootSplit)
	}
	if tab.RootSplit.Children[0].PaneID != DefaultPaneID || tab.RootSplit.Children[1].PaneID != "pane-2" {
		t.Fatalf("unexpected split children %#v", tab.RootSplit.Children)
	}
	if !tab.Panes[1].Active || tab.Panes[0].Active {
		t.Fatalf("expected only new pane active, got %#v", tab.Panes)
	}
}

func TestShellSplitActivePaneSupportsVerticalDirection(t *testing.T) {
	shell := DefaultShell().SplitActivePane(PaneState{ID: "pane-2"}, SplitDirectionVertical)
	if shell.Workspace.Tabs[0].RootSplit.Direction != SplitDirectionVertical {
		t.Fatalf("expected vertical split, got %#v", shell.Workspace.Tabs[0].RootSplit)
	}
}

func TestShellFocusCloseAndZoomPaneState(t *testing.T) {
	shell := DefaultShell().
		SplitActivePane(PaneState{ID: "pane-2", Title: "logs", Kind: PaneTerminalLive, TerminalID: "term-2"}, SplitDirectionVertical).
		SplitActivePane(PaneState{ID: "pane-3", Title: "build", Kind: PaneTerminalLive, TerminalID: "term-3"}, SplitDirectionHorizontal)

	shell = shell.FocusPane(PaneCommandTarget{PaneID: DefaultPaneID})
	if shell.ActivePaneID != DefaultPaneID || !shell.Workspace.Tabs[0].Panes[0].Active {
		t.Fatalf("expected focused default pane, got %#v", shell)
	}

	shell = shell.ZoomPane(PaneCommandTarget{PaneID: "pane-2"})
	if shell.ZoomedPaneID != "pane-2" || shell.ActivePaneID != "pane-2" {
		t.Fatalf("expected zoomed pane-2, got %#v", shell)
	}
	shell = shell.UnzoomPane()
	if shell.ZoomedPaneID != "" {
		t.Fatalf("expected unzoomed shell, got %#v", shell)
	}

	shell = shell.ClosePane(PaneCommandTarget{PaneID: "pane-2"})
	tab := shell.Workspace.Tabs[0]
	if len(tab.Panes) != 2 || shell.HasPane(PaneCommandTarget{PaneID: "pane-2"}) {
		t.Fatalf("expected pane-2 closed, got %#v", shell)
	}
	if containsPaneID(tab.RootSplit, "pane-2") {
		t.Fatalf("closed pane must be removed from split tree, got %#v", tab.RootSplit)
	}
}

func TestShellCloseLastPaneIsIgnoredByStateMethod(t *testing.T) {
	shell := DefaultShell().ClosePane(PaneCommandTarget{PaneID: DefaultPaneID})
	if len(shell.Workspace.Tabs[0].Panes) != 1 || shell.ActivePaneID != DefaultPaneID {
		t.Fatalf("last pane close must be ignored, got %#v", shell)
	}
}

func TestShellResizeSetSizeAndBalancePaneGeometry(t *testing.T) {
	shell := DefaultShell().SplitActivePane(PaneState{ID: "pane-2"}, SplitDirectionVertical)

	shell = shell.ResizePane(PaneCommandTarget{PaneID: DefaultPaneID}, PaneResizeRight, 4)
	if shell.Workspace.Tabs[0].RootSplit.BiasCells != 4 {
		t.Fatalf("expected resize bias for first pane, got %#v", shell.Workspace.Tabs[0].RootSplit)
	}

	shell = shell.SetPaneSize(PaneCommand{
		Action:   PaneCommandSetSize,
		Target:   PaneCommandTarget{PaneID: "pane-2"},
		SizeMode: PaneSizeRatio,
		Ratio:    0.25,
	})
	if got := shell.Workspace.Tabs[0].RootSplit.Ratio; got != 0.75 {
		t.Fatalf("ratio is stored as first child ratio, got %#v", shell.Workspace.Tabs[0].RootSplit)
	}

	shell = shell.SetPaneSize(PaneCommand{
		Action:   PaneCommandSetSize,
		Target:   PaneCommandTarget{PaneID: "pane-2"},
		SizeMode: PaneSizeCells,
		Cols:     12,
	})
	root := shell.Workspace.Tabs[0].RootSplit
	if root.FixedPaneID != "pane-2" || root.FixedCols != 12 || root.Ratio != 0 {
		t.Fatalf("expected fixed cols for pane-2, got %#v", root)
	}

	shell = shell.BalancePanes(PaneCommandTarget{PaneID: "pane-2"})
	root = shell.Workspace.Tabs[0].RootSplit
	if root.BiasCells != 0 || root.Ratio != 0 || root.FixedPaneID != "" || root.FixedCols != 0 {
		t.Fatalf("expected balanced split hints, got %#v", root)
	}
}

func TestShellResizeSplitPathOnlyChangesExactNestedDivider(t *testing.T) {
	shell := DefaultShell().
		SplitActivePane(PaneState{ID: "pane-middle"}, SplitDirectionVertical).
		SplitActivePane(PaneState{ID: "pane-right"}, SplitDirectionVertical)

	shell = shell.ResizeSplitPath(PaneCommandTarget{PaneID: "pane-middle"}, "root/1", PaneResizeRight, -4)
	root := shell.Workspace.Tabs[0].RootSplit
	if root.BiasCells != 0 {
		t.Fatalf("exact nested divider resize must not change root split, got %#v", root)
	}
	if len(root.Children) < 2 || root.Children[1].BiasCells != -4 {
		t.Fatalf("expected right nested split bias only, got %#v", root)
	}

	unchanged := shell.ResizeSplitPath(PaneCommandTarget{PaneID: "pane-middle"}, "root/9", PaneResizeRight, 3)
	if unchanged.Workspace.Tabs[0].RootSplit.Children[1].BiasCells != -4 {
		t.Fatalf("invalid split path must not mutate layout, got %#v", unchanged.Workspace.Tabs[0].RootSplit)
	}
}

func TestShellResizePaneGroupKeepsNonAdjacentColumnsFixed(t *testing.T) {
	shell := DefaultShell().
		SplitActivePane(PaneState{ID: "pane-2"}, SplitDirectionVertical).
		SplitActivePane(PaneState{ID: "pane-3"}, SplitDirectionVertical).
		SplitActivePane(PaneState{ID: "pane-4"}, SplitDirectionVertical)

	shell = shell.ResizePaneGroup(PaneCommandTarget{PaneID: "pane-2"}, PaneResizeRight, []PaneResizeGroupItem{
		{PaneID: "pane-2", Cells: 25},
		{PaneID: "pane-3", Cells: 15},
		{PaneID: "pane-4", Cells: 20},
	})
	root := shell.Workspace.Tabs[0].RootSplit
	right := root.Children[1]
	if right.FixedPaneID != "pane-2" || right.FixedCols != 25 {
		t.Fatalf("right subtree should fix pane-2 width, got %#v", right)
	}
	if right.Children[1].FixedPaneID != "pane-3" || right.Children[1].FixedCols != 15 {
		t.Fatalf("right subtree should fix pane-3 width without resizing pane-4, got %#v", right.Children[1])
	}
	if right.Children[1].Children[1].PaneID != "pane-4" {
		t.Fatalf("pane-4 should remain an independent leaf, got %#v", right)
	}
}

func TestShellResizePaneGroupKeepsStackedRightColumnSharedWidth(t *testing.T) {
	shell := DefaultShell()
	tab := &shell.Workspace.Tabs[0]
	tab.Panes = []PaneState{
		{ID: "left", Kind: PaneTerminalLive},
		{ID: "top", Kind: PaneTerminalLive},
		{ID: "middle-left", Kind: PaneTerminalLive},
		{ID: "middle-right", Kind: PaneTerminalLive},
		{ID: "bottom", Kind: PaneTerminalLive},
	}
	tab.ActivePaneID = "middle-left"
	tab.RootSplit = SplitNode{
		Direction: SplitDirectionVertical,
		Children: []SplitNode{
			{PaneID: "left"},
			{
				Direction: SplitDirectionHorizontal,
				Children: []SplitNode{
					{PaneID: "top"},
					{
						Direction: SplitDirectionHorizontal,
						Children: []SplitNode{
							{
								Direction: SplitDirectionVertical,
								Children:  []SplitNode{{PaneID: "middle-left"}, {PaneID: "middle-right"}},
							},
							{PaneID: "bottom"},
						},
					},
				},
			},
		},
	}
	shell.ActivePaneID = "middle-left"
	shell = shell.EnsureDefaults()

	shell = shell.ResizePaneGroup(PaneCommandTarget{PaneID: "left"}, PaneResizeRight, []PaneResizeGroupItem{
		{PaneID: "left", Cells: 36, DeltaSign: 1},
		{PaneID: "top", Cells: 44, DeltaSign: -1},
		{PaneID: "middle-left", Cells: 24, DeltaSign: -1},
		{PaneID: "middle-right", Cells: 20},
		{PaneID: "bottom", Cells: 44, DeltaSign: -1},
	})

	root := shell.Workspace.Tabs[0].RootSplit
	if root.FixedPaneID != "left" || root.FixedCols != 36 {
		t.Fatalf("root divider should fix the left column width, got %#v", root)
	}
	rightColumn := root.Children[1]
	if rightColumn.FixedCols != 0 || rightColumn.FixedRows != 0 {
		t.Fatalf("orthogonal stacked column should keep height geometry independent, got %#v", rightColumn)
	}
	middleRow := rightColumn.Children[1].Children[0]
	if middleRow.FixedPaneID != "middle-left" || middleRow.FixedCols != 24 {
		t.Fatalf("nested middle row should resize its left child while preserving right child anchor, got %#v", middleRow)
	}
}

func TestShellFloatingCommandsManageRectZOrderAndCollapse(t *testing.T) {
	shell := DefaultShell()
	var result FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{
		Action:   FloatingCommandCreate,
		TargetID: "float-1",
		Pane:     PaneState{ID: "float-pane", Title: "日志🚀", Kind: PaneEmpty},
		BoundsW:  80,
		BoundsH:  24,
	})
	if result.Status != FloatingCommandOK || len(shell.Floatings) != 1 || shell.ActiveFloatingID != "float-1" {
		t.Fatalf("expected created active floating, result=%#v shell=%#v", result, shell)
	}
	created := shell.Floatings[0]
	if created.Rect.X <= 0 || created.Rect.Y <= 0 || created.Rect.W < 16 || created.Rect.H < 4 || !created.Active {
		t.Fatalf("expected centered clamped floating, got %#v", created)
	}

	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandMove, TargetID: "float-1", DeltaX: -999, DeltaY: -999, BoundsW: 80, BoundsH: 24})
	if result.Status != FloatingCommandOK || shell.Floatings[0].Rect.X != 0 || shell.Floatings[0].Rect.Y != 0 {
		t.Fatalf("move should clamp to viewport, result=%#v floating=%#v", result, shell.Floatings[0])
	}
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandResize, TargetID: "float-1", DeltaW: -999, DeltaH: -999, BoundsW: 80, BoundsH: 24})
	if result.Status != FloatingCommandOK || shell.Floatings[0].Rect.W != 16 || shell.Floatings[0].Rect.H != 4 {
		t.Fatalf("resize should keep minimum floating size, result=%#v floating=%#v", result, shell.Floatings[0])
	}
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandToggleCollapse, TargetID: "float-1"})
	if result.Status != FloatingCommandOK || !shell.Floatings[0].Collapsed {
		t.Fatalf("expected collapsed floating, result=%#v floating=%#v", result, shell.Floatings[0])
	}
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandFocusRaise, TargetID: "float-1"})
	if result.Status != FloatingCommandOK || shell.ActiveFloatingID != "float-1" || !shell.Floatings[0].Active {
		t.Fatalf("expected raised floating, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandDeactivate})
	if result.Status != FloatingCommandOK || shell.ActiveFloatingID != "" || shell.Floatings[0].Active {
		t.Fatalf("deactivate should keep floating but clear active state, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandClose, TargetID: "float-1"})
	if result.Status != FloatingCommandOK || len(shell.Floatings) != 0 || shell.ActiveFloatingID != "" {
		t.Fatalf("expected closed floating, result=%#v shell=%#v", result, shell)
	}
}

func TestShellFloatingGroupCommandsManageCollapseAndFitMode(t *testing.T) {
	shell := DefaultShell()
	var result FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{
		Action:   FloatingCommandCreate,
		TargetID: "float-1",
		Pane:     PaneState{ID: "float-pane-1", Title: "one", Kind: PaneEmpty},
		Rect:     FloatingRect{X: 1, Y: 1, W: 20, H: 8},
		BoundsW:  100,
		BoundsH:  40,
	})
	if result.Status != FloatingCommandOK {
		t.Fatalf("create first floating: %#v", result)
	}
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{
		Action:   FloatingCommandCreate,
		TargetID: "float-2",
		Pane:     PaneState{ID: "float-pane-2", Title: "two", Kind: PaneEmpty},
		Rect:     FloatingRect{X: 10, Y: 5, W: 22, H: 9},
		BoundsW:  100,
		BoundsH:  40,
	})
	if result.Status != FloatingCommandOK {
		t.Fatalf("create second floating: %#v", result)
	}

	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandCollapseAll})
	if result.Status != FloatingCommandOK || shell.ActiveFloatingID != "" || !shell.Floatings[0].Collapsed || !shell.Floatings[1].Collapsed {
		t.Fatalf("collapse all should collapse every floating, result=%#v shell=%#v", result, shell)
	}

	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandShowAll})
	if result.Status != FloatingCommandOK || shell.ActiveFloatingID == "" || shell.Floatings[0].Collapsed || shell.Floatings[1].Collapsed {
		t.Fatalf("show all should restore every floating, result=%#v shell=%#v", result, shell)
	}

	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandToggleAll})
	if result.Status != FloatingCommandOK || !shell.Floatings[0].Collapsed || !shell.Floatings[1].Collapsed {
		t.Fatalf("toggle all should collapse open floatings, result=%#v shell=%#v", result, shell)
	}
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandToggleAll})
	if result.Status != FloatingCommandOK || shell.Floatings[0].Collapsed || shell.Floatings[1].Collapsed {
		t.Fatalf("toggle all should restore collapsed floatings, result=%#v shell=%#v", result, shell)
	}

	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandFit, TargetID: "float-1", FitCols: 50, FitRows: 16, BoundsW: 100, BoundsH: 40})
	if result.Status != FloatingCommandOK {
		t.Fatalf("fit floating: %#v", result)
	}
	if got := shell.Floatings[0]; got.Rect.W != 52 || got.Rect.H != 18 || got.FitMode != FloatingFitManual || got.AutoFit != (FloatingAutoFitState{}) {
		t.Fatalf("fit should resize to content extent plus chrome, got %#v", got)
	}

	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandToggleAutoFit, TargetID: "float-1", FitCols: 60, FitRows: 20, BoundsW: 100, BoundsH: 40})
	if result.Status != FloatingCommandOK {
		t.Fatalf("enable auto-fit: %#v", result)
	}
	if got := shell.Floatings[0]; got.FitMode != FloatingFitAuto || got.AutoFit.Cols != 60 || got.AutoFit.Rows != 20 || got.Rect.W != 62 || got.Rect.H != 22 {
		t.Fatalf("auto-fit should save latest fit size and update rect, got %#v", got)
	}

	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandRefreshAutoFit, TargetID: "float-1", FitCols: 70, FitRows: 18, BoundsW: 100, BoundsH: 40})
	if result.Status != FloatingCommandOK {
		t.Fatalf("refresh auto-fit: %#v", result)
	}
	if got := shell.Floatings[0]; got.AutoFit.Cols != 70 || got.AutoFit.Rows != 18 || got.Rect.W != 72 || got.Rect.H != 20 {
		t.Fatalf("refresh auto-fit should update metadata and rect, got %#v", got)
	}

	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandResize, TargetID: "float-1", DeltaW: -2, DeltaH: -2, BoundsW: 100, BoundsH: 40})
	if result.Status != FloatingCommandOK {
		t.Fatalf("manual resize after auto-fit: %#v", result)
	}
	if got := shell.Floatings[0]; got.FitMode != FloatingFitManual || got.AutoFit != (FloatingAutoFitState{}) {
		t.Fatalf("manual resize should clear auto-fit state, got %#v", got)
	}
}

func TestShellBindFloatingTerminal(t *testing.T) {
	shell := DefaultShell().BindPaneTerminal(PaneCommandTarget{PaneID: DefaultPaneID}, "term-main")
	var result FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{
		Action:   FloatingCommandCreate,
		TargetID: "float-1",
		Pane:     PaneState{ID: "float-pane", Title: "float", Kind: PaneEmpty},
	})
	if result.Status != FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}

	shell = shell.BindFloatingTerminal("float-1", "term-float")
	if shell.ActiveFloatingID != "float-1" || len(shell.Floatings) != 1 || !shell.Floatings[0].Active {
		t.Fatalf("floating bind should focus target floating, got %#v", shell.Floatings)
	}
	if pane := shell.Floatings[0].Pane; pane.Kind != PaneTerminalLive || pane.TerminalID != "term-float" {
		t.Fatalf("floating pane should bind terminal-live target, got %#v", pane)
	}
	tiled, ok := shell.Pane(PaneCommandTarget{PaneID: DefaultPaneID})
	if !ok || tiled.TerminalID != "term-main" {
		t.Fatalf("floating bind must not rewrite tiled pane terminal, got %#v ok=%v", tiled, ok)
	}
}

func TestShellTerminalOverlaysTargetActiveFloating(t *testing.T) {
	shell := DefaultShell()
	var result FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(FloatingCommand{Action: FloatingCommandCreate, TargetID: "float-1", Pane: PaneState{ID: "float-pane", Kind: PaneEmpty}})
	if result.Status != FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}

	picker := shell.OpenTerminalPicker()
	if picker.Overlay.TargetID != "float-1" {
		t.Fatalf("terminal picker should target active floating, got %#v", picker.Overlay)
	}
	pool := shell.OpenTerminalPool()
	if pool.Overlay.TargetID != "float-1" {
		t.Fatalf("terminal pool should target active floating, got %#v", pool.Overlay)
	}
}

func TestShellPromptOverlaySubmitCancelAndConfirm(t *testing.T) {
	shell := DefaultShell().OpenPrompt(PromptState{Title: "Danger", Destructive: true, ConfirmText: "DELETE"})
	if !shell.Overlay.Open || shell.Overlay.Kind != OverlayPrompt || !shell.Overlay.Prompt.Destructive {
		t.Fatalf("expected prompt overlay, got %#v", shell.Overlay)
	}
	shell = shell.SetPromptValue("nope").SubmitPrompt()
	if shell.Overlay.Prompt.Submitted || shell.Overlay.Prompt.LastResult != "confirm required: DELETE" {
		t.Fatalf("expected destructive confirm guard, got %#v", shell.Overlay.Prompt)
	}
	shell = shell.SetPromptValue("DELETE").SubmitPrompt()
	if !shell.Overlay.Prompt.Submitted || shell.Overlay.Prompt.LastResult != "DELETE" {
		t.Fatalf("expected prompt submitted, got %#v", shell.Overlay.Prompt)
	}
	shell = shell.OpenPrompt(PromptState{Title: "Rename"}).CancelPrompt()
	if !shell.Overlay.Prompt.Canceled {
		t.Fatalf("expected prompt canceled, got %#v", shell.Overlay.Prompt)
	}
}

func TestShellUpdatesDoNotMutatePreviousSlices(t *testing.T) {
	original := DefaultShell().AddToast(ToastSpec{ID: "old"})
	next := original.AddToast(ToastSpec{ID: "new"})
	next.Workspace.Tabs[0].Panes[0].Title = "mutated"

	if len(original.Toasts) != 1 || original.Toasts[0].ID != "old" {
		t.Fatalf("original toasts mutated: %#v next=%#v", original.Toasts, next.Toasts)
	}
	if original.Workspace.Tabs[0].Panes[0].Title != "shell" {
		t.Fatalf("original pane mutated: %#v", original.Workspace.Tabs[0].Panes[0])
	}
}

func containsPaneID(node SplitNode, paneID string) bool {
	if node.PaneID == paneID {
		return true
	}
	for _, child := range node.Children {
		if containsPaneID(child, paneID) {
			return true
		}
	}
	return false
}
