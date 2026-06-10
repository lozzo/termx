package render

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestRenderVMBuilderUsesCopyModeOnlyWhenBoundToHistory(t *testing.T) {
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       80,
			Rows: []state.HistoryRow{
				{Text: "old", LineID: 10},
				{Text: "new", LineID: 11},
			},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  80,
			Cursor:     state.CopyPosition{Row: 1, Col: 2},
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory {
		t.Fatalf("expected copy-history content, got %#v", content)
	}
	if len(content.Lines) < 4 || content.Lines[1].PlainString() != "● old" || content.Lines[2].PlainString() != "● new" {
		t.Fatalf("unexpected copy-history content lines %#v", content.Lines)
	}
	if len(content.HitRegions) != 2 || content.HitRegions[0].LineID != 10 || content.HitRegions[0].Rect != (Rect{X: 2, Y: 1, W: 3, H: 1}) || content.HitRegions[1].Rect != (Rect{X: 2, Y: 2, W: 3, H: 1}) {
		t.Fatalf("unexpected hit regions %#v", content.HitRegions)
	}
	if !vm.Shell.Cursor.Visible || vm.Shell.Cursor.Row != 2 || vm.Shell.Cursor.Col != 4 {
		t.Fatalf("expected copy cursor VM, got %#v", vm.Shell.Cursor)
	}
}

func TestRenderVMBuilderBuildsStructuredChromeSlots(t *testing.T) {
	root := state.Root{
		Shell: state.ShellStore{
			HeaderVisible: true,
			FooterVisible: true,
			Workspace: state.WorkspaceState{
				ID:          "ws-main",
				Name:        "main",
				ActiveTabID: "tab-build",
				Tabs: []state.TabState{
					{ID: "tab-shell", Title: "shell", Panes: []state.PaneState{{ID: "pane-shell", Title: "shell", Kind: state.PaneTerminalLive, TerminalID: "term-1"}}},
					{ID: "tab-build", Title: "build", Panes: []state.PaneState{{ID: "pane-build", Title: "build", Kind: state.PaneTerminalLive, TerminalID: "term-2"}}},
				},
			},
			ActivePaneID: "pane-build",
		},
		Session: state.TerminalSessionStore{TerminalID: "term-2", Attached: true},
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-2",
			Ready:      true,
			Lines:      []string{"build live"},
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	if len(vm.Shell.Header.Tabs) != 2 || vm.Shell.Header.Tabs[0].Title != "shell" || vm.Shell.Header.Tabs[1].Title != "build" || !vm.Shell.Header.Tabs[1].Active {
		t.Fatalf("header should expose structured tab slots, got %#v", vm.Shell.Header.Tabs)
	}
	if vm.Shell.Header.Tabs[0].CloseTargetID != "tab-shell" || vm.Shell.Header.Tabs[1].CloseTargetID != "tab-build" {
		t.Fatalf("header tab close slots should carry target tab ids, got %#v", vm.Shell.Header.Tabs)
	}
	if len(vm.Shell.Footer.ActionTokens) == 0 || vm.Shell.Footer.ActionTokens[0].Key != "^P" || vm.Shell.Footer.ActionTokens[0].Label != "pane" {
		t.Fatalf("footer should expose structured action tokens, got %#v", vm.Shell.Footer.ActionTokens)
	}
	lastAction := vm.Shell.Footer.ActionTokens[len(vm.Shell.Footer.ActionTokens)-1]
	if lastAction.Key != "^G" || lastAction.Label != "global" {
		t.Fatalf("footer structured tokens should keep compacted labels, got %#v", vm.Shell.Footer.ActionTokens)
	}
	if vm.Shell.Footer.ActionTokens[0].ActionID != ActionFooterPaneMode.String() || lastAction.ActionID != ActionFooterGlobalMode.String() {
		t.Fatalf("footer structured tokens should expose semantic action ids, got %#v", vm.Shell.Footer.ActionTokens)
	}
	if len(vm.Shell.Layout.Panels) != 1 || len(vm.Shell.Layout.Panels[0].Chrome.Actions) != 4 {
		t.Fatalf("pane should expose structured chrome actions, got %#v", vm.Shell.Layout.Panels)
	}
	if vm.Shell.Layout.Panels[0].Chrome.Title.Text != "build" || vm.Shell.Layout.Panels[0].Chrome.Title.Style != StyleAccent {
		t.Fatalf("pane should expose structured title slot, got %#v", vm.Shell.Layout.Panels[0].Chrome.Title)
	}
	if vm.Shell.Layout.Panels[0].Chrome.State.Text != "active" || vm.Shell.Layout.Panels[0].Chrome.State.Style != StyleSuccess {
		t.Fatalf("pane should expose structured active state slot, got %#v", vm.Shell.Layout.Panels[0].Chrome.State)
	}
	if vm.Shell.Layout.Panels[0].Chrome.Actions[0].ActionID != ActionPaneZoom.String() || vm.Shell.Layout.Panels[0].Chrome.Actions[1].ActionID != ActionPaneSplitRight.String() ||
		vm.Shell.Layout.Panels[0].Chrome.Actions[2].ActionID != ActionPaneSplitDown.String() || vm.Shell.Layout.Panels[0].Chrome.Actions[3].ActionID != ActionPaneClose.String() {
		t.Fatalf("pane chrome actions should keep semantic action ids, got %#v", vm.Shell.Layout.Panels[0].Chrome.Actions)
	}
}

func TestRenderVMBuilderProjectsTerminalResizeOwnerChrome(t *testing.T) {
	shell := state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneTerminalLive, TerminalID: "term-1"}, state.SplitDirectionVertical)
	root := state.Root{Shell: shell.FocusPane(state.PaneCommandTarget{PaneID: "pane-2"})}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, state.TerminalResizeRoleFollower, "surface", "view-2", false))

	vm := NewRenderVMBuilder().Build(root)
	var owner PanelVM
	var follower PanelVM
	for _, panel := range vm.Shell.Layout.Panels {
		switch panel.ID {
		case state.DefaultPaneID:
			owner = panel
		case "pane-2":
			follower = panel
		}
	}
	if len(owner.Chrome.Meta) != 1 || owner.Chrome.Meta[0].Text != "size:owner" || owner.Chrome.Meta[0].Style != StyleSuccess {
		t.Fatalf("owner pane should show size owner metadata, got %#v", owner.Chrome.Meta)
	}
	if owner.Chrome.Terminal.Title.Text != "shell" || owner.Chrome.Terminal.State.Text != paneChromeRunningGlyph() || owner.Chrome.Terminal.AttachCount != 2 || owner.Chrome.Terminal.Owner.Text != "◆ owner" || !owner.Chrome.Terminal.Locked {
		t.Fatalf("owner pane should expose tuiv2 terminal chrome facts, got %#v", owner.Chrome.Terminal)
	}
	if len(follower.Chrome.Meta) != 1 || follower.Chrome.Meta[0].Text != "size:follower" {
		t.Fatalf("follower pane should show follower metadata, got %#v", follower.Chrome.Meta)
	}
	if follower.Chrome.Terminal.AttachCount != 2 || follower.Chrome.Terminal.Owner.Text != "◇ follow" || follower.Chrome.Terminal.Locked {
		t.Fatalf("follower pane should expose tuiv2 follower chrome facts, got %#v", follower.Chrome.Terminal)
	}
	if len(owner.Chrome.Actions) != 4 || owner.Chrome.Actions[0].ActionID == ActionTerminalTakeResizeOwner.String() {
		t.Fatalf("owner pane should not offer take-owner action, got %#v", owner.Chrome.Actions)
	}
	if len(follower.Chrome.Actions) != 4 || follower.Chrome.Actions[0].ActionID == ActionTerminalTakeResizeOwner.String() {
		t.Fatalf("follower pane should keep structural actions separate from owner token, got %#v", follower.Chrome.Actions)
	}
}

func TestRenderVMBuilderKeepsChromeActionsForEmptyPane(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell()}
	root.Shell.Workspace.Tabs[0].Panes[0].Kind = state.PaneEmpty
	root.Shell.Workspace.Tabs[0].Panes[0].TerminalID = ""
	root.Shell.Workspace.Tabs[0].Panes[0].Title = "unconnected"

	panel := NewRenderVMBuilder().Build(root).Shell.Layout.Panels[0]
	if panel.Content.Kind != ContentEmptyPane || !panel.Content.Empty {
		t.Fatalf("expected empty pane content, got %#v", panel.Content)
	}
	if len(panel.Chrome.Actions) != 4 || panel.Chrome.Actions[0].ActionID != ActionPaneZoom.String() || panel.Chrome.Actions[3].ActionID != ActionPaneClose.String() {
		t.Fatalf("empty pane should keep still-available pane chrome actions, got %#v", panel.Chrome.Actions)
	}
}

func TestRenderVMBuilderBuildsGlobalFooterActionIDs(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell().SetInteractionMode(state.InteractionModeGlobal)}
	actions := NewRenderVMBuilder().Build(root).Shell.Footer.ActionTokens
	want := map[string]string{
		"h": ActionFooterToggleHeader.String(),
		"f": ActionFooterToggleFooter.String(),
		"p": ActionFooterOpenPool.String(),
		"w": ActionFooterOpenTree.String(),
		"T": ActionFooterCloseToast.String(),
		"t": ActionFooterClearToasts.String(),
	}
	for _, action := range actions {
		if id, ok := want[action.Key]; ok && action.ActionID != id {
			t.Fatalf("global footer action %s should use %s, got %#v", action.Key, id, actions)
		}
	}
}

func TestRenderVMBuilderUsesStructuredFooterActionCatalog(t *testing.T) {
	builder := NewRenderVMBuilder()
	tests := []struct {
		name string
		root state.Root
		want []FooterActionVM
	}{
		{
			name: "live",
			root: state.Root{Shell: state.DefaultShell()},
			want: []FooterActionVM{
				{Key: "^P", Label: "pane", ActionID: ActionFooterPaneMode.String()},
				{Key: "^R", Label: "resize", ActionID: ActionFooterResizeMode.String()},
				{Key: "^T", Label: "tab", ActionID: ActionFooterTabMode.String()},
				{Key: "^W", Label: "workspace", ActionID: ActionFooterWorkspaceMode.String()},
				{Key: "^O", Label: "float", ActionID: ActionFooterFloatingMode.String()},
				{Key: "^V", Label: "copy", ActionID: ActionFooterCopyMode.String()},
				{Key: "^F", Label: "picker", ActionID: ActionFooterPicker.String()},
				{Key: "^G", Label: "global", ActionID: ActionFooterGlobalMode.String()},
			},
		},
		{
			name: "terminal pool overlay",
			root: state.Root{Shell: state.DefaultShell().OpenTerminalPool()},
			want: []FooterActionVM{
				{Key: "attach", ActionID: ActionPoolAttach.String()},
				{Key: "edit", ActionID: ActionPoolEdit.String()},
				{Key: "kill", ActionID: ActionPoolKill.String()},
			},
		},
		{
			name: "prompt overlay",
			root: state.Root{Shell: state.DefaultShell().OpenPrompt(state.PromptState{})},
			want: []FooterActionVM{
				{Key: "enter", Label: "submit", ActionID: ActionPromptSubmit.String()},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			footer := builder.Build(tc.root).Shell.Footer
			if len(footer.Actions) != 0 {
				t.Fatalf("default footer VM should not require legacy action strings, got %#v", footer.Actions)
			}
			for _, want := range tc.want {
				if !containsFooterAction(footer.ActionTokens, want.Key, want.Label, want.ActionID) {
					t.Fatalf("missing structured footer action %#v in %#v", want, footer.ActionTokens)
				}
			}
		})
	}
}

func TestRenderVMBuilderBuildsPaneStateSlotsFromContent(t *testing.T) {
	tests := []struct {
		name    string
		root    state.Root
		paneID  string
		want    string
		wantSty StyleToken
	}{
		{
			name: "active live",
			root: state.Root{
				Shell:   state.DefaultShell(),
				Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Ready: true, Lines: []string{"live"}},
			},
			paneID:  state.DefaultPaneID,
			want:    "active",
			wantSty: StyleSuccess,
		},
		{
			name: "pending live",
			root: state.Root{
				Shell: state.DefaultShell(),
			},
			paneID:  state.DefaultPaneID,
			want:    "pending",
			wantSty: StyleWarning,
		},
		{
			name: "error live",
			root: state.Root{
				Shell:   state.DefaultShell(),
				Session: state.TerminalSessionStore{LastError: "boom"},
			},
			paneID:  state.DefaultPaneID,
			want:    "error",
			wantSty: StyleDanger,
		},
		{
			name: "empty live",
			root: state.Root{
				Shell:   state.DefaultShell(),
				Surface: state.TerminalSurfaceStore{TerminalID: "term-empty", Ready: true},
			},
			paneID:  state.DefaultPaneID,
			want:    "empty",
			wantSty: StyleMuted,
		},
		{
			name: "inactive ready pane",
			root: func() state.Root {
				shell := state.DefaultShell().
					SplitActivePane(state.PaneState{ID: "pane-logs", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-logs"}, state.SplitDirectionVertical).
					FocusPane(state.PaneCommandTarget{PaneID: "pane-logs"})
				shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-main"
				surface := (state.TerminalSurfaceStore{}).ApplySnapshot(state.LiveSurfaceSnapshot{TerminalID: "term-main", Lines: []string{"main live"}})
				return state.Root{Shell: shell, Surface: surface}
			}(),
			paneID:  state.DefaultPaneID,
			want:    "idle",
			wantSty: StyleMuted,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := NewRenderVMBuilder().Build(tt.root)
			panel := panelByID(vm.Shell.Layout.Panels, tt.paneID)
			if panel == nil {
				t.Fatalf("missing panel %s in %#v", tt.paneID, vm.Shell.Layout.Panels)
			}
			if panel.Chrome.State.Text != tt.want || panel.Chrome.State.Style != tt.wantSty {
				t.Fatalf("unexpected pane state slot: got %#v want text=%q style=%s", panel.Chrome.State, tt.want, tt.wantSty)
			}
		})
	}
}

func TestPaneChromeStateSlotUsesIdleForInactiveReadyContent(t *testing.T) {
	slot := paneChromeStateSlot(false, ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("ready")}})
	if slot.Text != "idle" || slot.Style != StyleMuted {
		t.Fatalf("inactive ready content should use idle state slot, got %#v", slot)
	}
}

func panelByID(panels []PanelVM, id string) *PanelVM {
	for index := range panels {
		if panels[index].ID == id {
			return &panels[index]
		}
	}
	return nil
}

func TestRenderVMBuilderProjectsCopyHistoryContentRendererState(t *testing.T) {
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       12,
			Rows: []state.HistoryRow{
				{Text: "alpha", LineID: 10, RowInLine: 0, ClippedStart: true},
				{Text: "beta好", LineID: 10, RowInLine: 1, ClippedEnd: true},
				{Text: "gamma", LineID: 11, RowInLine: 0},
			},
			Lines: []state.HistoryLineSpan{
				{LineID: 10, StartRow: 0, EndRow: 1, ClippedBefore: true, ClippedAfter: true},
				{LineID: 11, StartRow: 2, EndRow: 2},
			},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  12,
			Cursor:     state.CopyPosition{Row: 1, Col: 4},
			Selection: &state.CopySelection{
				Anchor: state.CopyPosition{Row: 0, Col: 2},
				Focus:  state.CopyPosition{Row: 1, Col: 4},
			},
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if got := content.Lines[0].PlainString(); !strings.Contains(got, "search") {
		t.Fatalf("expected copy search row, got %#v", content.Lines)
	}
	if got := content.Lines[1].PlainString(); !strings.Contains(got, "⇡ alpha") {
		t.Fatalf("expected clipped-before marker, got %#v", content.Lines)
	}
	if got := content.Lines[2].PlainString(); !strings.Contains(got, "╎ beta好 ⇣") {
		t.Fatalf("expected continuation and clipped-end markers, got %#v", content.Lines)
	}
	if !lineHasStyledCell(content.Lines[1], "pha", StyleAccent) || !lineHasStyledCell(content.Lines[2], "beta", StyleAccent) {
		t.Fatalf("expected selection rendered as styled cells, got %#v", content.Lines)
	}
	if !content.Cursor.Visible || content.Cursor.Row != 2 || content.Cursor.Col != 6 {
		t.Fatalf("expected cursor offset by copy marker, got %#v", content.Cursor)
	}
	if !strings.Contains(content.Status, "row 2/3 line:10 part:2 cols:12") || !strings.Contains(content.Status, "span:1-2") {
		t.Fatalf("expected position status, got %q", content.Status)
	}
	if len(content.HitRegions) != 3 || content.HitRegions[1].LineID != 10 ||
		content.HitRegions[1].Rect != (Rect{X: 2, Y: 2, W: 6, H: 1}) {
		t.Fatalf("expected history hit regions to cover only row text display cells, got %#v", content.HitRegions)
	}
}

func TestRenderVMBuilderCopyHistoryHitRegionsUseAuthoritativeDisplayCells(t *testing.T) {
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       12,
			Rows: []state.HistoryRow{{
				Text:   "a好bc",
				LineID: 10,
				Cells: []state.HistoryCell{
					{Text: "a", Width: 1},
					{Text: "好", Width: 2},
					{Text: "bc", Width: 2},
				},
			}},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  12,
		},
	}

	content := activeContent(NewRenderVMBuilder().Build(root).Shell)
	if len(content.HitRegions) != 1 {
		t.Fatalf("expected one history hit region, got %#v", content.HitRegions)
	}
	if got, want := content.HitRegions[0].Rect, (Rect{X: copyHistoryMarkerCell(root.History.Rows[0]).Width, Y: 1, W: 5, H: 1}); got != want {
		t.Fatalf("history hit region should start after marker and use row display width got=%#v want=%#v", got, want)
	}
}

func TestRenderVMBuilderCopyHistoryEmptyRowKeepsOneCellHitRegion(t *testing.T) {
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       12,
			Rows:       []state.HistoryRow{{Text: "", LineID: 10}},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  12,
		},
	}

	content := activeContent(NewRenderVMBuilder().Build(root).Shell)
	if len(content.HitRegions) != 1 {
		t.Fatalf("expected one history hit region, got %#v", content.HitRegions)
	}
	if got, want := content.HitRegions[0].Rect, (Rect{X: 2, Y: 1, W: 1, H: 1}); got != want {
		t.Fatalf("empty authoritative row should keep a one-cell selectable region got=%#v want=%#v", got, want)
	}
}

func TestRenderVMBuilderCopyHistoryShowsAuthoritativeBoundaryTokens(t *testing.T) {
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       12,
			Rows: []state.HistoryRow{
				{Text: "one", LineID: 100},
				{Text: "two", LineID: 101},
				{Text: "three", LineID: 102},
				{Text: "four", LineID: 103},
			},
			Cursor:   state.HistoryCursor{Valid: true, BeforeLineID: 100},
			Boundary: state.HistoryBoundary{FirstLineID: 100, LastLineID: 103},
			HasMore:  true,
		},
		CopyMode: state.CopyModeStore{
			Active:      true,
			TerminalID:  "term-1",
			BoundToken:  "tok-1",
			BoundCols:   12,
			ViewRows:    4,
			ViewportTop: 1,
		},
	}

	content := activeContent(NewRenderVMBuilder().Build(root).Shell)
	if got := content.Lines[0].PlainString(); !strings.Contains(got, "↑ more") {
		t.Fatalf("search row should expose older-more token from authoritative cursor, got %q", got)
	}
	scroll := content.Lines[len(content.Lines)-1].PlainString()
	if !strings.Contains(scroll, "↓ loaded") || !strings.Contains(scroll, "lines:100-103 view:2-3") {
		t.Fatalf("scrollbar should expose bottom loaded and logical boundary summary, got %q", scroll)
	}
	if !strings.Contains(content.Status, "lines:100-103 view:2-3") {
		t.Fatalf("status should include boundary summary, got %q", content.Status)
	}

	root.CopyMode.ViewportTop = 2
	content = activeContent(NewRenderVMBuilder().Build(root).Shell)
	scroll = content.Lines[len(content.Lines)-1].PlainString()
	if !strings.Contains(scroll, "↓ latest") || !strings.Contains(scroll, "view:3-4") {
		t.Fatalf("bottom token should switch to latest at loaded tail, got %q", scroll)
	}

	root.History.Pending = &state.HistoryPendingRequest{ID: 7, Kind: state.HistoryRequestOlder, Token: "tok-1"}
	content = activeContent(NewRenderVMBuilder().Build(root).Shell)
	if got := content.Lines[0].PlainString(); !strings.Contains(got, "↑ loading") {
		t.Fatalf("search row should expose pending older request, got %q", got)
	}

	root.History.Pending = nil
	root.History.HasMore = false
	root.History.Exhausted = state.ExhaustedMarker{}
	content = activeContent(NewRenderVMBuilder().Build(root).Shell)
	if got := content.Lines[0].PlainString(); !strings.Contains(got, "↑ older") || strings.Contains(got, "↑ more") {
		t.Fatalf("cursor-ready window without HasMore should use neutral older token, got %q", got)
	}

	root.History.Exhausted = state.ExhaustedMarker{
		Valid:    true,
		Token:    "tok-1",
		Cols:     12,
		Cursor:   root.History.Cursor,
		Boundary: root.History.Boundary,
	}
	content = activeContent(NewRenderVMBuilder().Build(root).Shell)
	if got := content.Lines[0].PlainString(); !strings.Contains(got, "↑ top") {
		t.Fatalf("search row should expose exhausted top token, got %q", got)
	}
}

func TestFrameworkRendersCopyHistoryOverflowMarkersOnPaneChrome(t *testing.T) {
	root := state.Root{
		Viewport: state.ViewportStore{Valid: true, Cols: 18, Rows: 8},
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       78,
			Rows: []state.HistoryRow{
				{Text: "0123456789abcdef", LineID: 10},
				{Text: "row-2", LineID: 11},
				{Text: "row-3", LineID: 12},
				{Text: "row-4", LineID: 13},
				{Text: "row-5", LineID: 14},
				{Text: "row-6", LineID: 15},
				{Text: "row-7", LineID: 16},
			},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  78,
		},
	}

	result := NewRenderer(DefaultTheme()).RenderResult(NewRenderVMBuilder().Build(root))
	layer := firstLayer(t, result, LayerPanel)
	if !layer.ContentOverflow.Right || !layer.ContentOverflow.Bottom {
		t.Fatalf("copy history overflow should be exposed through panel layer, got %#v", layer.ContentOverflow)
	}
	lines := result.Lines()
	if got := SliceCells(lines[4], 17, 18); got != ">" {
		t.Fatalf("right overflow marker should be drawn on pane chrome, got %q frame=%#v", got, lines)
	}
	if got := SliceCells(lines[6], 9, 10); got != "v" {
		t.Fatalf("bottom overflow marker should be drawn on pane chrome, got %q frame=%#v", got, lines)
	}
	if strings.Contains(strings.Join(plainContentViewportLines(layer.Lines), "\n"), ">") ||
		strings.Contains(strings.Join(plainContentViewportLines(layer.Lines), "\n"), "v") {
		t.Fatalf("overflow markers must stay out of copy history content layer, got %#v", layer.Lines)
	}
}

func TestRenderVMBuilderProjectsCopyHistoryStyledCells(t *testing.T) {
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       12,
			Rows: []state.HistoryRow{{
				Text:   "ERR 好",
				LineID: 10,
				Cells: []state.HistoryCell{
					{Text: "ERR", Width: 3, Style: state.HistoryCellStyle{FG: "ansi:1", Bold: true}},
					{Text: " ", Width: 1},
					{Text: "好", Width: 2, Style: state.HistoryCellStyle{FG: "#ffcc00", Underline: true}, LinkURL: "file://build.log", LinkParams: "line=7"},
				},
			}},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  12,
		},
	}

	content := activeContent(NewRenderVMBuilder().Build(root).Shell)
	if got := content.Lines[1].PlainString(); !strings.Contains(got, "● ERR 好") {
		t.Fatalf("expected copy history plain text from styled cells, got %#v", content.Lines)
	}
	if !lineHasANSICell(content.Lines[1], "ERR", ANSICellStyle{FG: "ansi:1", Bold: true}) ||
		!lineHasANSICell(content.Lines[1], "好", ANSICellStyle{FG: "#ffcc00", Underline: true}) {
		t.Fatalf("copy history render should preserve history ANSI cells, got %#v", content.Lines[1])
	}
	if !lineHasLinkCell(content.Lines[1], "好", "file://build.log", "line=7") {
		t.Fatalf("copy history render should preserve history link metadata, got %#v", content.Lines[1])
	}

	root.CopyMode.Selection = &state.CopySelection{
		Anchor: state.CopyPosition{Row: 0, Col: 0},
		Focus:  state.CopyPosition{Row: 0, Col: 3},
	}
	content = activeContent(NewRenderVMBuilder().Build(root).Shell)
	if !lineHasStyledCell(content.Lines[1], "ERR", StyleAccent) {
		t.Fatalf("selection should override history ANSI style for selected cells, got %#v", content.Lines[1])
	}
	if !lineHasLinkCell(content.Lines[1], "好", "file://build.log", "line=7") {
		t.Fatalf("selection should not drop unselected history link metadata, got %#v", content.Lines[1])
	}
}

func TestRenderVMBuilderCopyHistorySelectionAndSearchUseDisplayColumns(t *testing.T) {
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       12,
			Rows: []state.HistoryRow{{
				Text:   "a好bc",
				LineID: 10,
				Cells: []state.HistoryCell{
					{Text: "a", Width: 1, Style: state.HistoryCellStyle{FG: "ansi:2"}},
					{Text: "好", Width: 2, Style: state.HistoryCellStyle{FG: "ansi:3"}, LinkURL: "file://wide.txt", LinkParams: "row=1"},
					{Text: "bc", Width: 2, Style: state.HistoryCellStyle{FG: "ansi:4"}, LinkURL: "file://split.txt", LinkParams: "row=1"},
				},
			}},
		},
		CopyMode: state.CopyModeStore{
			Active:      true,
			TerminalID:  "term-1",
			BoundToken:  "tok-1",
			BoundCols:   12,
			Cursor:      state.CopyPosition{Row: 0, Col: 4},
			Query:       "好b",
			Matches:     []state.CopyMatch{{Row: 0, StartCol: 1, EndCol: 4}},
			ActiveMatch: 0,
			Selection: &state.CopySelection{
				Anchor: state.CopyPosition{Row: 0, Col: 1},
				Focus:  state.CopyPosition{Row: 0, Col: 4},
			},
		},
	}

	content := activeContent(NewRenderVMBuilder().Build(root).Shell)
	if !content.Cursor.Visible || content.Cursor.Col != copyHistoryMarkerCell(root.History.Rows[0]).Width+4 {
		t.Fatalf("cursor should use display columns, got %#v", content.Cursor)
	}
	if !lineHasStyledCell(content.Lines[1], "好", StyleAccent) || !lineHasStyledCell(content.Lines[1], "b", StyleAccent) {
		t.Fatalf("selection should split styled cells by display columns, got %#v", content.Lines[1])
	}
	if !lineHasLinkCell(content.Lines[1], "好", "file://wide.txt", "row=1") || !lineHasLinkCell(content.Lines[1], "b", "file://split.txt", "row=1") {
		t.Fatalf("selection split should preserve link metadata on highlighted cells, got %#v", content.Lines[1])
	}
	if lineHasStyledCell(content.Lines[1], "a", StyleAccent) || lineHasStyledCell(content.Lines[1], "c", StyleAccent) {
		t.Fatalf("selection should not leak outside display-column range, got %#v", content.Lines[1])
	}

	root.CopyMode.Selection = nil
	content = activeContent(NewRenderVMBuilder().Build(root).Shell)
	if !lineHasStyledCell(content.Lines[1], "好", StyleWarning) || !lineHasStyledCell(content.Lines[1], "b", StyleWarning) {
		t.Fatalf("search should split styled cells by display columns, got %#v", content.Lines[1])
	}
	if !lineHasLinkCell(content.Lines[1], "好", "file://wide.txt", "row=1") || !lineHasLinkCell(content.Lines[1], "b", "file://split.txt", "row=1") {
		t.Fatalf("search split should preserve link metadata on highlighted cells, got %#v", content.Lines[1])
	}
	if !lineHasANSICell(content.Lines[1], "a", ANSICellStyle{FG: "ansi:2"}) ||
		!lineHasANSICell(content.Lines[1], "c", ANSICellStyle{FG: "ansi:4"}) {
		t.Fatalf("unmatched history cells should preserve ANSI style, got %#v", content.Lines[1])
	}
}

func TestRenderVMBuilderCopyHistorySearchUsesGraphemeDisplayColumns(t *testing.T) {
	family := "👨‍👩‍👧‍👦"
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       12,
			Rows: []state.HistoryRow{{
				Text:   family + "x",
				LineID: 10,
			}},
		},
		CopyMode: state.CopyModeStore{
			Active:      true,
			TerminalID:  "term-1",
			BoundToken:  "tok-1",
			BoundCols:   12,
			Query:       family,
			Matches:     []state.CopyMatch{{Row: 0, StartCol: 0, EndCol: 2}},
			ActiveMatch: 0,
		},
	}

	content := activeContent(NewRenderVMBuilder().Build(root).Shell)
	if !lineHasStyledCell(content.Lines[1], family, StyleWarning) {
		t.Fatalf("search should highlight emoji grapheme as one display range, got %#v", content.Lines[1])
	}
	if lineHasStyledCell(content.Lines[1], "x", StyleWarning) {
		t.Fatalf("search highlight should not leak into trailing x, got %#v", content.Lines[1])
	}
}

func TestRenderVMBuilderShowsPendingWithoutAuthoritativeHistory(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Lines:      []string{"live-row"},
		},
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-history",
			Cols:       80,
			Rows:       []state.HistoryRow{{Text: "old", LineID: 10}},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-copy",
			BoundCols:  80,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory || !content.Pending {
		t.Fatalf("expected pending copy-history content, got %#v", content)
	}
	if len(content.Lines) != 1 || !strings.Contains(content.Lines[0].PlainString(), "stale history token") {
		t.Fatalf("copy mode must not fallback to live rows, got %#v", content.Lines)
	}
}

func TestRenderVMBuilderShowsPendingAfterCopyResizeInvalidation(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Lines:      []string{"live-row"},
		},
		History: state.HistoryStore{
			TerminalID: "term-1",
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundCols:  98,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory || !content.Pending {
		t.Fatalf("expected pending copy-history after resize invalidation, got %#v", content)
	}
	if len(content.Lines) != 1 || !strings.Contains(content.Lines[0].PlainString(), "authoritative history window pending") {
		t.Fatalf("copy mode resize pending must not fallback to live rows, got %#v", content.Lines)
	}
}

func TestRenderVMBuilderShowsCopyHistoryEmptyWithoutLiveFallback(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Lines:      []string{"live-row"},
		},
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       80,
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  80,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory || !content.Empty || content.Pending {
		t.Fatalf("expected empty copy-history content, got %#v", content)
	}
	if len(content.Lines) != 1 || content.Lines[0].PlainString() != "copy history empty" {
		t.Fatalf("copy mode empty must not fallback to live rows, got %#v", content.Lines)
	}
}

func TestRenderVMBuilderBuildsProductHeaderFooterSummaries(t *testing.T) {
	shell := state.DefaultShell().
		SetInteractionMode(state.InteractionModePane).
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "日志 🚀", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical)
	shell, _ = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "float-1",
		Pane:     state.PaneState{ID: "float-pane", Title: "浮窗", Kind: state.PaneEmpty},
		Rect:     state.FloatingRect{X: 4, Y: 3, W: 30, H: 8},
	})
	root := state.Root{
		Shell: shell,
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Title:      "termx",
			Lines:      []string{"live"},
		},
		Session: state.TerminalSessionStore{
			TerminalID: "term-live",
			Attached:   true,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	header := vm.Shell.Header
	if !header.Visible || header.Workspace != "main" || header.Tab != "[main]" || header.ActivePane != "pane-2" || header.TerminalSummary != "term:1" || header.FloatingSummary != "float:1" {
		t.Fatalf("unexpected product header %#v", header)
	}
	footer := vm.Shell.Footer
	if !footer.Visible || footer.Mode != "pane" || footer.ActiveTarget != "pane:日志 🚀 attached" ||
		!containsFooterAction(footer.ActionTokens, "v", "split", ActionPaneFooterSplit.String()) ||
		!strings.Contains(footer.GlobalSummary, "ws:main") || !strings.Contains(footer.GlobalSummary, "float:1") || !strings.Contains(footer.GlobalSummary, "terminals:1") {
		t.Fatalf("unexpected product footer %#v", footer)
	}
	if len(footer.Actions) != 0 {
		t.Fatalf("default builder should use structured footer tokens, got legacy actions %#v", footer.Actions)
	}
	if len(vm.Shell.Layout.Floating) != 1 || vm.Shell.Layout.Floating[0].Title != "浮窗" || !vm.Shell.Layout.Floating[0].Active {
		t.Fatalf("expected floating VM projection, got %#v", vm.Shell.Layout.Floating)
	}
}

func TestRenderVMBuilderAnchorsEmptyPaneCursorForIME(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell()}
	root.Shell.Workspace.Tabs[0].Panes[0] = state.PaneState{ID: state.DefaultPaneID, Title: "浮窗", Kind: state.PaneEmpty, Active: true}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentEmptyPane || !content.Cursor.Visible {
		t.Fatalf("empty pane should expose a cursor anchor for IME, got %#v", content)
	}
	if content.Cursor.Col != DisplayWidth("No terminal attached") {
		t.Fatalf("empty pane cursor should follow headline text, got %#v", content.Cursor)
	}
}

func TestRenderVMBuilderDimsTiledPaneWhenFloatingOwnsFocus(t *testing.T) {
	shell := state.DefaultShell()
	shell, result := shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "float-1",
		Pane:     state.PaneState{ID: "float-pane", Title: "float", Kind: state.PaneEmpty},
		Rect:     state.FloatingRect{X: 4, Y: 3, W: 30, H: 8},
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if len(vm.Shell.Layout.Panels) != 1 || vm.Shell.Layout.Panels[0].Active {
		t.Fatalf("active floating should dim tiled pane chrome, panels=%#v floating=%#v", vm.Shell.Layout.Panels, vm.Shell.Layout.Floating)
	}
	if len(vm.Shell.Layout.Floating) != 1 || !vm.Shell.Layout.Floating[0].Active {
		t.Fatalf("expected active floating VM, got %#v", vm.Shell.Layout.Floating)
	}

	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandDeactivate})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("deactivate floating: %#v", result)
	}
	vm = NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if len(vm.Shell.Layout.Panels) != 1 || !vm.Shell.Layout.Panels[0].Active {
		t.Fatalf("tiled pane should regain active visual while inactive floating remains open, got %#v", vm.Shell.Layout.Panels)
	}
	if len(vm.Shell.Layout.Floating) != 1 || vm.Shell.Layout.Floating[0].Active {
		t.Fatalf("floating should remain visible but inactive, got %#v", vm.Shell.Layout.Floating)
	}

	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandFocusRaise, TargetID: "float-1"})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("raise floating: %#v", result)
	}
	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandClose, TargetID: "float-1"})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("close floating: %#v", result)
	}
	vm = NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if len(vm.Shell.Layout.Panels) != 1 || !vm.Shell.Layout.Panels[0].Active {
		t.Fatalf("tiled pane active visual should restore after floating closes, got %#v", vm.Shell.Layout.Panels)
	}
}

func TestRenderVMBuilderProjectsTabStripAndWorkspaceSummary(t *testing.T) {
	shell := state.DefaultShell()
	shell = shell.SetInteractionMode(state.InteractionModePane)
	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if vm.Shell.Footer.Mode != "pane" ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "v", "split", ActionPaneFooterSplit.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "x", "close", ActionPaneFooterClose.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "n", "focus", ActionPaneFooterFocus.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "z", "zoom", ActionPaneFooterZoom.String()) {
		t.Fatalf("expected pane footer structural actions, got %#v", vm.Shell.Footer)
	}
	shell = shell.SetInteractionMode(state.InteractionModeResize)
	vm = NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if vm.Shell.Footer.Mode != "resize" ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "←/h", "", ActionResizeLeft.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "→/l", "", ActionResizeRight.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "↑/k", "", ActionResizeUp.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "↓/j", "", ActionResizeDown.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "b", "balance", ActionResizeBalance.String()) {
		t.Fatalf("expected resize footer structural actions, got %#v", vm.Shell.Footer)
	}
	shell = shell.SetInteractionMode(state.InteractionModeFloating)
	vm = NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if vm.Shell.Footer.Mode != "floating" ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "n", "new", ActionFloatingNew.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "x", "close", ActionFloatingClose.String()) {
		t.Fatalf("expected floating footer structural actions, got %#v", vm.Shell.Footer)
	}

	shell, _ = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"})
	shell, _ = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceCreate, Name: "remote"})
	shell = shell.SetInteractionMode(state.InteractionModeWorkspace)

	vm = NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if vm.Shell.Header.Workspace != "remote" || vm.Shell.Header.Tab != "[main]" {
		t.Fatalf("expected active workspace header, got %#v", vm.Shell.Header)
	}
	if vm.Shell.Footer.Mode != "workspace" ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "n", "new", ActionFooterNewWorkspace.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "h", "prev", ActionFooterPreviousWorkspace.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "l", "next", ActionFooterNextWorkspace.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "r", "rename", ActionFooterRenameWorkspace.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "t", "tree", ActionFooterOpenTree.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "x", "delete", ActionFooterDeleteWorkspace.String()) ||
		!strings.Contains(vm.Shell.Footer.GlobalSummary, "ws:remote") {
		t.Fatalf("expected workspace footer summary, got %#v", vm.Shell.Footer)
	}

	shell, _ = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspacePrevious})
	vm = NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if vm.Shell.Header.Workspace != "main" || vm.Shell.Header.Tab != "main [logs]" {
		t.Fatalf("expected original workspace tab strip, got %#v", vm.Shell.Header)
	}
	shell = shell.SetInteractionMode(state.InteractionModeTab)
	vm = NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if vm.Shell.Footer.Mode != "tab" ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "h", "prev", ActionTabPrevious.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "l", "next", ActionTabNext.String()) ||
		!containsFooterAction(vm.Shell.Footer.ActionTokens, "r", "rename", ActionTabRename.String()) {
		t.Fatalf("expected tab footer rename action, got %#v", vm.Shell.Footer)
	}
}

func TestRenderVMBuilderDerivesFooterModePrecedence(t *testing.T) {
	builder := NewRenderVMBuilder()
	if got := builder.Build(state.Root{
		CopyMode: state.CopyModeStore{Active: true, TerminalID: "term-copy", BoundCols: 80},
	}).Shell.Footer.Mode; got != "copy" {
		t.Fatalf("expected copy footer mode, got %q", got)
	}
	copyFooter := builder.Build(state.Root{
		CopyMode: state.CopyModeStore{Active: true, TerminalID: "term-copy", BoundCols: 80},
	}).Shell.Footer
	if !containsFooterAction(copyFooter.ActionTokens, "pgup", "older", ActionCopyOlder.String()) {
		t.Fatalf("expected copy footer older action id, got %#v", copyFooter.ActionTokens)
	}
	if got := builder.Build(state.Root{
		Shell:    state.DefaultShell().OpenTerminalPicker().SetInteractionMode(state.InteractionModeGlobal),
		CopyMode: state.CopyModeStore{Active: true, TerminalID: "term-copy", BoundCols: 80},
	}).Shell.Footer.Mode; got != "terminal-picker" {
		t.Fatalf("overlay should win footer mode, got %q", got)
	}
	if got := builder.Build(state.Root{
		Shell: state.DefaultShell().SetInteractionMode(state.InteractionModeResize),
	}).Shell.Footer.Mode; got != "resize" {
		t.Fatalf("expected interaction footer mode, got %q", got)
	}
}

func TestRenderVMBuilderShowsCopyHistoryBindingErrorWithoutLiveFallback(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Lines:      []string{"live-row"},
		},
		History: state.HistoryStore{
			TerminalID: "term-history",
			Token:      "tok-1",
			Cols:       80,
			Rows:       []state.HistoryRow{{Text: "old", LineID: 10}},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-copy",
			BoundToken: "tok-1",
			BoundCols:  80,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory || content.Error == "" {
		t.Fatalf("expected copy-history binding error, got %#v", content)
	}
	if len(content.Lines) != 1 || !strings.Contains(content.Lines[0].PlainString(), "terminal mismatch") {
		t.Fatalf("copy mode error must not fallback to live rows, got %#v", content.Lines)
	}
}

func TestRenderVMBuilderBuildsShellPanelToastAndOverlayVM(t *testing.T) {
	shell := state.DefaultShell().
		SetHeaderVisible(false).
		SetFooterVisible(false).
		SetPanelPresentation(state.PanelPresentationSplitLine).
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionHorizontal).
		AddToast(state.ToastSpec{ID: "toast-1", Severity: state.ToastWarning, Title: "warn", Body: "body", Pending: true}).
		OpenTerminalPicker()
	root := state.Root{
		Shell: shell,
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Lines:      []string{"prompt"},
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	if vm.Shell.Header.Visible || vm.Shell.Footer.Visible {
		t.Fatalf("expected hidden header/footer, got header=%#v footer=%#v", vm.Shell.Header, vm.Shell.Footer)
	}
	if len(vm.Shell.Layout.Panels) != 2 {
		t.Fatalf("expected two panel VMs, got %#v", vm.Shell.Layout.Panels)
	}
	if vm.Shell.Layout.Panels[0].Presentation != PanelPresentationSplitLine || vm.Shell.Layout.Panels[1].Presentation != PanelPresentationSplitLine {
		t.Fatalf("expected split line presentation, got %#v", vm.Shell.Layout.Panels)
	}
	if !vm.Shell.Layout.Panels[1].Active || activeContent(vm.Shell).Kind != ContentTerminalLive {
		t.Fatalf("expected active live content in active panel, got %#v", vm.Shell.Layout.Panels)
	}
	if vm.Shell.Overlay.Kind != OverlayTerminalPicker || vm.Shell.Overlay.Content.Kind != ContentTerminalPicker {
		t.Fatalf("expected terminal picker overlay VM, got %#v", vm.Shell.Overlay)
	}
	if len(vm.Shell.Toasts) != 1 || vm.Shell.Toasts[0].Severity != ToastWarning || !vm.Shell.Toasts[0].Pending {
		t.Fatalf("expected warning toast VM, got %#v", vm.Shell.Toasts)
	}
}

func TestRenderVMBuilderProjectsZoomedPaneOnly(t *testing.T) {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionHorizontal).
		ZoomPane(state.PaneCommandTarget{PaneID: "pane-2"})

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell, Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Lines: []string{"prompt"}}})
	if len(vm.Shell.Layout.Panels) != 1 {
		t.Fatalf("expected one zoomed panel, got %#v", vm.Shell.Layout.Panels)
	}
	if panel := vm.Shell.Layout.Panels[0]; panel.ID != "pane-2" || !panel.Active || panel.Content.Kind != ContentTerminalLive {
		t.Fatalf("unexpected zoomed panel projection %#v", panel)
	}
	if vm.Shell.Layout.Split.PaneID != "pane-2" || len(vm.Shell.Layout.Split.Children) != 0 {
		t.Fatalf("expected zoom split root to target pane, got %#v", vm.Shell.Layout.Split)
	}
}

func TestRenderVMBuilderDoesNotProjectClosedPane(t *testing.T) {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionHorizontal).
		ClosePane(state.PaneCommandTarget{PaneID: "pane-2"})

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if len(vm.Shell.Layout.Panels) != 1 || vm.Shell.Layout.Panels[0].ID != state.DefaultPaneID {
		t.Fatalf("expected closed pane removed from render VM, got %#v", vm.Shell.Layout.Panels)
	}
	if vm.Shell.Layout.Split.PaneID != state.DefaultPaneID {
		t.Fatalf("expected split tree pruned to default pane, got %#v", vm.Shell.Layout.Split)
	}
}

func TestRenderVMBuilderProjectsPaneGeometryHints(t *testing.T) {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical).
		SetPaneSize(state.PaneCommand{Target: state.PaneCommandTarget{PaneID: "pane-2"}, SizeMode: state.PaneSizeCells, Cols: 12})

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if vm.Shell.Layout.Split.FixedPaneID != "pane-2" || vm.Shell.Layout.Split.FixedCols != 12 {
		t.Fatalf("expected fixed pane size hint in render VM, got %#v", vm.Shell.Layout.Split)
	}
}

func TestRenderVMBuilderUsesRootViewportAsLayoutTruth(t *testing.T) {
	root := state.Root{
		Viewport: state.ViewportStore{Valid: true, Cols: 37, Rows: 13},
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Cols:       80,
			Rows:       24,
			Lines:      []string{"prompt"},
		},
		Session: state.TerminalSessionStore{
			TerminalID: "term-live",
			Attached:   true,
			Cols:       120,
			Rows:       40,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	if got, want := vm.Shell.Layout.Viewport, (Rect{W: 37, H: 13}); got != want {
		t.Fatalf("expected external viewport as layout truth got=%#v want=%#v", got, want)
	}
}

func TestRenderVMBuilderUsesLiveSurface(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Cols:       80,
			Rows:       24,
			Lines:      []string{"prompt", "output"},
		},
		Session: state.TerminalSessionStore{
			TerminalID: "term-live",
			Attached:   true,
			Cols:       80,
			Rows:       24,
			State:      state.TerminalLiveAttached,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentTerminalLive || len(content.Lines) != 2 || content.Lines[0].PlainString() != "prompt" || content.Status != "live: term-live attached 80x24" {
		t.Fatalf("unexpected live content %#v", content)
	}
	if vm.Shell.Footer.ActiveTarget != "pane:shell attached" {
		t.Fatalf("expected attached footer active target, got %#v", vm.Shell.Footer)
	}
}

func TestRenderVMBuilderProjectsTerminalLiveANSIStyleCursorAndState(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Cols:       20,
			Rows:       5,
			Ready:      true,
			Screen: [][]state.LiveCell{{
				{Text: "ERR", Width: 3, FG: "ansi:1"},
				{Text: " ok ", Width: 4},
				{Text: "好", Width: 2, FG: "ansi:2"},
				{Text: "🚀", Width: 2, FG: "ansi:2"},
			}},
			Cursor: state.LiveCursor{Visible: true, Row: 0, Col: 10, Shape: "bar"},
		},
		Session: state.TerminalSessionStore{TerminalID: "term-live", Attached: true, Cols: 20, Rows: 5},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentTerminalLive || len(content.Lines) != 1 {
		t.Fatalf("expected terminal-live content, got %#v", content)
	}
	if got := content.Lines[0].PlainString(); got != "ERR ok 好🚀" {
		t.Fatalf("expected sanitized live text, got %q", got)
	}
	if !lineHasANSICell(content.Lines[0], "ERR", ANSICellStyle{FG: "ansi:1"}) ||
		!lineHasANSICell(content.Lines[0], "好", ANSICellStyle{FG: "ansi:2"}) ||
		!lineHasANSICell(content.Lines[0], "🚀", ANSICellStyle{FG: "ansi:2"}) {
		t.Fatalf("expected live terminal SGR preserved as ANSI cell style, got %#v", content.Lines[0])
	}
	if !content.Cursor.Visible || content.Cursor.Row != 0 || content.Cursor.Col != 10 || content.Cursor.Shape != CursorShapeBar {
		t.Fatalf("expected live cursor projection, got %#v", content.Cursor)
	}
	if content.Extent != (ContentExtent{Known: true, Cols: 20, Rows: 5}) {
		t.Fatalf("live content must expose terminal surface extent, got %#v", content.Extent)
	}

	legacy := NewRenderVMBuilder().Build(state.Root{Surface: state.TerminalSurfaceStore{
		TerminalID: "term-live",
		Ready:      true,
		Lines:      []string{"\x1b[31mERR\x1b[0m"},
	}})
	if content := activeContent(legacy.Shell); !lineHasStyledCell(content.Lines[0], "ERR", StyleDanger) {
		t.Fatalf("expected legacy ANSI line fallback to keep working, got %#v", content.Lines[0])
	}

	pending := NewRenderVMBuilder().Build(state.Root{})
	if content := activeContent(pending.Shell); !content.Pending || content.Empty || content.Lines[0].PlainString() != "live surface pending" {
		t.Fatalf("expected pending live surface content, got %#v", content)
	}

	empty := NewRenderVMBuilder().Build(state.Root{Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Ready: true}})
	if content := activeContent(empty.Shell); !content.Empty || content.Pending || content.Lines[0].PlainString() != "live surface empty" {
		t.Fatalf("expected empty live surface content, got %#v", content)
	}
}

func TestRenderVMBuilderDoesNotApplyLiveExtentToStatusFallbacks(t *testing.T) {
	cases := []struct {
		name    string
		root    state.Root
		want    string
		pending bool
		empty   bool
	}{
		{
			name: "pending",
			root: state.Root{
				Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Cols: 6, Rows: 2},
				Session: state.TerminalSessionStore{TerminalID: "term-live", Attached: true, Cols: 6, Rows: 2},
			},
			want:    "live surface pending",
			pending: true,
		},
		{
			name: "empty",
			root: state.Root{
				Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Cols: 6, Rows: 2, Ready: true},
				Session: state.TerminalSessionStore{TerminalID: "term-live", Attached: true, Cols: 6, Rows: 2},
			},
			want:  "live surface empty",
			empty: true,
		},
		{
			name: "exited",
			root: state.Root{
				Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Cols: 6, Rows: 2, State: state.TerminalLiveExited, ExitCode: 0},
				Session: state.TerminalSessionStore{TerminalID: "term-live", Attached: true, Cols: 6, Rows: 2, State: state.TerminalLiveExited, ExitCode: 0},
			},
			want:  "terminal exited: term-live code:0",
			empty: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := activeContent(NewRenderVMBuilder().Build(tc.root).Shell)
			if content.Pending != tc.pending || content.Empty != tc.empty || content.Lines[0].PlainString() != tc.want {
				t.Fatalf("unexpected fallback content %#v", content)
			}
			if content.Extent.Known {
				t.Fatalf("status fallback must not enable live extent dots, got %#v", content.Extent)
			}
		})
	}
}

func TestRenderVMBuilderDoesNotApplyResizeBoundaryExtentToStatusFallbacks(t *testing.T) {
	cases := []struct {
		name string
		root state.Root
		want string
	}{
		{
			name: "pending after resize",
			root: state.Root{
				Surface: state.TerminalSurfaceStore{
					TerminalID:     "term-live",
					Cols:           98,
					Rows:           36,
					ResizeBoundary: state.LiveResizeBoundary{Active: true, PreviousCols: 78, PreviousRows: 20, Cols: 98, Rows: 36},
				},
				Session: state.TerminalSessionStore{TerminalID: "term-live", Attached: true, Cols: 98, Rows: 36},
			},
			want: "live surface pending",
		},
		{
			name: "exited after resize",
			root: state.Root{
				Surface: state.TerminalSurfaceStore{
					TerminalID: "term-live",
					Cols:       98,
					Rows:       36,
					State:      state.TerminalLiveExited,
					ExitCode:   0,
				},
				Session: state.TerminalSessionStore{TerminalID: "term-live", Attached: true, Cols: 98, Rows: 36, State: state.TerminalLiveExited},
			},
			want: "terminal exited: term-live code:0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := activeContent(NewRenderVMBuilder().Build(tc.root).Shell)
			if content.Extent.Known {
				t.Fatalf("resize fallback must not enable live extent dots, got %#v", content.Extent)
			}
			if got := content.Lines[0].PlainString(); got != tc.want {
				t.Fatalf("unexpected fallback content got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestRenderVMBuilderUsesSessionSizeAsLiveExtentWhenSurfaceSizeMissing(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Ready:      true,
			Lines:      []string{"prompt"},
		},
		Session: state.TerminalSessionStore{
			TerminalID: "term-live",
			Attached:   true,
			Cols:       14,
			Rows:       3,
		},
	}

	content := activeContent(NewRenderVMBuilder().Build(root).Shell)
	if content.Extent != (ContentExtent{Known: true, Cols: 14, Rows: 3}) {
		t.Fatalf("live content should fall back to session size for extent, got %#v", content.Extent)
	}
}

func TestRenderVMBuilderProjectsLiveExitLifecycle(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			State:      state.TerminalLiveExited,
			ExitCode:   0,
			ExitReason: "done",
		},
		Session: state.TerminalSessionStore{
			TerminalID: "term-live",
			State:      state.TerminalLiveExited,
			ExitCode:   0,
			ExitReason: "done",
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentTerminalLive || !content.Empty || content.Pending || content.Cursor.Visible {
		t.Fatalf("expected exited live content lifecycle, got %#v", content)
	}
	if content.Lines[0].PlainString() != "terminal exited: term-live code:0 done" || content.Status != "exited: term-live code:0 done" {
		t.Fatalf("unexpected exited live projection content=%#v status=%q", content.Lines, content.Status)
	}
	if vm.Shell.Footer.ActiveTarget != "pane:shell exited" {
		t.Fatalf("expected exited footer active target, got %#v", vm.Shell.Footer)
	}
}

func TestRenderVMBuilderRespectsActiveEmptyAndExitedPaneContent(t *testing.T) {
	emptyShell := state.DefaultShell()
	emptyShell.Workspace.Tabs[0].Panes[0] = state.PaneState{ID: state.DefaultPaneID, Title: "slot", Kind: state.PaneEmpty, Active: true}
	emptyVM := NewRenderVMBuilder().Build(state.Root{
		Shell:   emptyShell,
		Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Ready: true, Lines: []string{"live must not replace empty"}},
	})
	if content := activeContent(emptyVM.Shell); content.Kind != ContentEmptyPane || !content.Empty || content.Lines[0].PlainString() != "No terminal attached" || !strings.Contains(content.Lines[1].PlainString(), "► Attach existing terminal ◄") {
		t.Fatalf("expected active empty pane placeholder, got %#v", content)
	} else if !contentHasAction(content, "empty.attach") || !contentHasAction(content, "empty.create") || !contentHasAction(content, "empty.manager") || !contentHasAction(content, "empty.close") {
		t.Fatalf("expected empty pane CTA action regions, got %#v", content.HitRegions)
	}

	exitedShell := state.DefaultShell()
	exitedShell.Workspace.Tabs[0].Panes[0] = state.PaneState{ID: state.DefaultPaneID, Title: "old shell", Kind: state.PaneExited, TerminalID: "term-old", Active: true}
	exitedVM := NewRenderVMBuilder().Build(state.Root{
		Shell:   exitedShell,
		Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Ready: true, Lines: []string{"live must not replace exited"}},
	})
	if content := activeContent(exitedVM.Shell); content.Kind != ContentExitedPane || content.Status != "exited: Restart / Reconnect / Close" || !strings.Contains(content.Lines[0].PlainString(), "Terminal exited old shell") || !strings.Contains(content.Lines[1].PlainString(), "term-old") {
		t.Fatalf("expected active exited pane placeholder, got %#v", content)
	} else if !contentHasAction(content, "exited.restart") || !contentHasAction(content, "exited.reconnect") || !contentHasAction(content, "exited.close") {
		t.Fatalf("expected exited pane CTA action regions, got %#v", content.HitRegions)
	}
}

func TestRenderVMBuilderProjectsEmptyTabContentWithoutSyntheticPanel(t *testing.T) {
	shell, result := state.DefaultShell().ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"})
	if result.Status != state.WorkbenchCommandOK {
		t.Fatalf("create tab: %#v", result)
	}

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	content := vm.Shell.Layout.BodyContent
	if len(vm.Shell.Layout.Panels) != 0 {
		t.Fatalf("empty tab must not create synthetic panel VMs, got %#v", vm.Shell.Layout.Panels)
	}
	if content.Kind != ContentEmptyPane || !content.Empty || !strings.Contains(content.Lines[0].PlainString(), "No panel in tab logs") || !strings.Contains(content.Lines[2].PlainString(), "Choose terminal") {
		t.Fatalf("expected empty tab content, got %#v", content)
	}
	if !contentHasAction(content, "empty.attach") || !contentHasAction(content, "empty.create") || !contentHasAction(content, "empty.manager") {
		t.Fatalf("expected empty tab CTA action regions, got %#v", content.HitRegions)
	}
}

func TestRenderVMBuilderProjectsEmptyWorkspaceWithoutSyntheticTab(t *testing.T) {
	shell, result := state.DefaultShell().ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabClose})
	if result.Status != state.WorkbenchCommandOK {
		t.Fatalf("close last tab: %#v", result)
	}

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	content := vm.Shell.Layout.BodyContent
	if len(vm.Shell.Header.Tabs) != 0 || vm.Shell.Header.Tab != "" {
		t.Fatalf("empty workspace must not synthesize header tabs, got %#v", vm.Shell.Header)
	}
	if len(vm.Shell.Layout.Panels) != 0 {
		t.Fatalf("empty workspace must not create synthetic panel VMs, got %#v", vm.Shell.Layout.Panels)
	}
	if content.Kind != ContentEmptyPane || !content.Empty || !strings.Contains(content.Lines[0].PlainString(), "No tabs in workspace main") || !strings.Contains(content.Lines[2].PlainString(), "Create tab") {
		t.Fatalf("expected empty workspace content, got %#v", content)
	}
	if !contentHasAction(content, "tab.create") || !contentHasAction(content, "empty.create") || !contentHasAction(content, "empty.manager") {
		t.Fatalf("expected empty workspace CTA action regions, got %#v", content.HitRegions)
	}
}

func TestRenderVMBuilderProjectsTerminalPickerContentRenderer(t *testing.T) {
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-main"
	shell = shell.SplitActivePane(state.PaneState{ID: "pane-2", Title: "日志🚀", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
		OpenTerminalPicker()
	shell.Overlay.Query = "term"

	root := state.Root{Shell: shell, TerminalPool: state.TerminalPoolStore{Status: state.TerminalPoolReady, Items: []state.TerminalPoolItem{{TerminalID: "term-main", Title: "shell", State: "running", Cols: 80, Rows: 24}, {TerminalID: "term-2", Title: "日志🚀", State: "running", Cols: 100, Rows: 30}}}}
	vm := NewRenderVMBuilder().Build(root)
	content := vm.Shell.Overlay.Content
	if vm.Shell.Overlay.Kind != OverlayTerminalPicker || content.Kind != ContentTerminalPicker {
		t.Fatalf("expected terminal picker content, got %#v", vm.Shell.Overlay)
	}
	plain := plainLines(content.Lines)
	for _, want := range []string{"search: term", "▸ + new terminal", "● shell", "running", "80x24", "● 日志🚀", "100x30", "Create terminal"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected compact picker marker %q, got %#v", want, content.Lines)
		}
	}
	for _, forbidden := range []string{"Select terminal source state target", "DETAIL", "TARGET", "TERMINAL", "[attach]", "[new]", "PREVIEW pane:", "pane:", "selected "} {
		if strings.Contains(plain, forbidden) {
			t.Fatalf("picker must not render management table/detail content %q, got %#v", forbidden, content.Lines)
		}
	}
	if content.Status != "terminal picker: 2 items query:term" {
		t.Fatalf("expected picker item count status, got %q", content.Status)
	}
	if !content.Cursor.Visible || content.Cursor.Shape != CursorShapeBar {
		t.Fatalf("expected picker search cursor, got %#v", content.Cursor)
	}
	if !contentHasAction(content, "picker.attach") || !contentHasAction(content, "picker.new") {
		t.Fatalf("expected terminal attach and create hit regions, got %#v", content.HitRegions)
	}
}

func TestRenderVMBuilderFiltersTerminalPickerAndHighlightsSelectedRow(t *testing.T) {
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-main"
	shell = shell.SplitActivePane(state.PaneState{ID: "pane-2", Title: "日志🚀", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
		OpenTerminalPicker().
		SetTerminalPickerQuery("日志")

	root := state.Root{Shell: shell, TerminalPool: state.TerminalPoolStore{Status: state.TerminalPoolReady, Items: []state.TerminalPoolItem{{TerminalID: "term-main", Title: "shell", State: "running", Cols: 80, Rows: 24}, {TerminalID: "term-2", Title: "日志🚀", State: "running", Cols: 100, Rows: 30}}}}
	content := NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !strings.Contains(content.Lines[0].PlainString(), "search: 日志") ||
		!strings.Contains(content.Lines[1].PlainString(), "▸ ● 日志🚀") ||
		!strings.Contains(content.Lines[1].PlainString(), "running") ||
		!strings.Contains(content.Lines[1].PlainString(), "100x30") ||
		strings.Contains(plainLines(content.Lines), "shell") ||
		strings.Contains(plainLines(content.Lines), "DETAIL") {
		t.Fatalf("expected filtered selected picker row, got %#v", content.Lines)
	}
	if !lineHasStyledCell(content.Lines[1], "日", StylePickerMatch) || !lineHasStyledCell(content.Lines[1], "志", StylePickerMatch) || !lineHasStyledCell(content.Lines[1], "running", StyleSuccess) {
		t.Fatalf("expected picker row text to use picker style and match highlight, got %#v", content.Lines)
	}
	if content.Cursor.Col != DisplayWidth("search: 日志") {
		t.Fatalf("expected cursor after query text, got %#v", content.Cursor)
	}
	if content.Status != "terminal picker: 1 items query:日志" {
		t.Fatalf("expected query status, got %q", content.Status)
	}
}

func TestRenderVMBuilderProjectsTerminalPickerPoolStateAndRows(t *testing.T) {
	shell := state.DefaultShell().OpenTerminalPicker()
	root := state.Root{
		Shell: shell,
		TerminalPool: state.TerminalPoolStore{
			Status: state.TerminalPoolReady,
			Items: []state.TerminalPoolItem{{
				TerminalID: "term-pool",
				Title:      "远程🚀",
				State:      "running",
				Cols:       120,
				Rows:       40,
			}},
		},
	}

	content := NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !strings.Contains(content.Lines[1].PlainString(), "▸ + new terminal") ||
		!strings.Contains(content.Lines[2].PlainString(), "● 远程🚀") ||
		!strings.Contains(content.Lines[2].PlainString(), "running") ||
		!strings.Contains(content.Lines[2].PlainString(), "120x40") ||
		strings.Contains(plainLines(content.Lines), "DETAIL") {
		t.Fatalf("expected terminal-only pool row, got %#v", content.Lines)
	}
	if len(content.HitRegions) != 2 || content.HitRegions[0].ActionID != ActionPickerNew.String() || content.HitRegions[1].PaneID != "" || content.HitRegions[1].ActionID != ActionPickerAttach.String() {
		t.Fatalf("expected create and terminal attach action regions, got %#v", content.HitRegions)
	}

	root.Shell = root.Shell.SetTerminalPickerQuery("trpl")
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if len(content.HitRegions) != 1 || content.HitRegions[0].ActionID != ActionPickerAttach.String() ||
		strings.Contains(plainLines(content.Lines), "term-pool") ||
		strings.Contains(plainLines(content.Lines), "DETAIL") {
		t.Fatalf("expected fuzzy terminal id match without id column highlight, got %#v", content.Lines)
	}

	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolLoading}
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !strings.Contains(plainLines(content.Lines), "loading terminals") {
		t.Fatalf("expected loading row, got %#v", content.Lines)
	}
	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolReady}
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if terminalPickerSelectableCount(state.TerminalPickerItems(root)) != 0 || strings.Contains(plainLines(content.Lines), "new terminal") {
		t.Fatalf("expected empty row, got %#v", content.Lines)
	}
	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolError, LastError: "boom"}
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !strings.Contains(plainLines(content.Lines), "pool error boom") {
		t.Fatalf("expected error row, got %#v", content.Lines)
	}
}

func TestRenderVMBuilderProjectsTerminalPoolPage(t *testing.T) {
	shell := state.DefaultShell().OpenTerminalPool().SetTerminalPoolQuery("日志")
	root := state.Root{
		Shell: shell,
		TerminalPool: state.TerminalPoolStore{
			Status: state.TerminalPoolReady,
			Items: []state.TerminalPoolItem{{
				TerminalID: "term-pool",
				Title:      "日志🚀",
				State:      "running",
				CWD:        "/tmp/日志",
				Cols:       120,
				Rows:       36,
				Tags:       map[string]string{"role": "shell"},
			}, {
				TerminalID: "term-other",
				Title:      "worker",
				State:      "exited",
			}},
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := vm.Shell.Overlay.Content
	if vm.Shell.Overlay.Kind != OverlayTerminalPool || content.Kind != ContentTerminalPool {
		t.Fatalf("expected terminal pool content, got %#v", vm.Shell.Overlay)
	}
	if !strings.Contains(content.Lines[0].PlainString(), "Terminal Pool") ||
		!strings.Contains(content.Lines[1].PlainString(), "⌕ search 日志") ||
		!strings.Contains(content.Lines[2].PlainString(), "▌ 日志🚀") ||
		!strings.Contains(content.Lines[3].PlainString(), "DETAIL 日志🚀") ||
		!strings.Contains(content.Lines[4].PlainString(), "120x36") ||
		!strings.Contains(content.Lines[6].PlainString(), "role=shell") ||
		!strings.Contains(content.Lines[len(content.Lines)-1].PlainString(), "[kill]  Kill") {
		t.Fatalf("expected pool list/detail/preview/actions, got %#v", content.Lines)
	}
	if strings.Contains(content.Lines[2].PlainString(), "worker") {
		t.Fatalf("query should filter non-matching row, got %#v", content.Lines)
	}
	if !contentHasAction(content, "pool.select") || !contentHasAction(content, "pool.attach") || !contentHasAction(content, "pool.edit") || !contentHasAction(content, "pool.kill") {
		t.Fatalf("expected pool action hit regions, got %#v", content.HitRegions)
	}
	if !content.Cursor.Visible || content.Cursor.Col != DisplayWidth("⌕ search 日志") {
		t.Fatalf("expected pool search cursor, got %#v", content.Cursor)
	}

	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolLoading}
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !content.Pending || !strings.Contains(content.Lines[2].PlainString(), "loading terminals") {
		t.Fatalf("expected pool loading page, got %#v", content)
	}
	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolError, LastError: "boom"}
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if content.Error != "boom" || !strings.Contains(content.Lines[2].PlainString(), "list error boom") {
		t.Fatalf("expected pool error page, got %#v", content)
	}
	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolReady}
	root.Shell = state.DefaultShell().OpenTerminalPool()
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !content.Empty || !strings.Contains(content.Lines[2].PlainString(), "list empty") {
		t.Fatalf("expected pool empty page, got %#v", content)
	}
}

func TestRenderVMBuilderProjectsWorkbenchTreeOverlay(t *testing.T) {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "日志🚀", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
		OpenWorkbenchTree().
		SetWorkbenchTreeQuery("日志")
	root := state.Root{Shell: shell}

	vm := NewRenderVMBuilder().Build(root)
	content := vm.Shell.Overlay.Content
	if vm.Shell.Overlay.Kind != OverlayWorkbenchTree || !vm.Shell.Overlay.Opaque || content.Kind != ContentWorkbenchTree {
		t.Fatalf("expected workbench tree opaque overlay, got %#v", vm.Shell.Overlay)
	}
	if !strings.Contains(content.Lines[0].PlainString(), "Workbench Tree") ||
		!strings.Contains(content.Lines[1].PlainString(), "⌕ search 日志") ||
		!strings.Contains(content.Lines[2].PlainString(), "▌      pane  日志🚀") ||
		!strings.Contains(content.Lines[0].PlainString(), "TUI storage projection") ||
		!strings.Contains(content.Lines[3].PlainString(), "DETAIL 日志🚀") ||
		!strings.Contains(content.Lines[len(content.Lines)-4].PlainString(), "[open]  Open") ||
		!strings.Contains(content.Lines[len(content.Lines)-3].PlainString(), "[rename]  Rename") ||
		!strings.Contains(content.Lines[len(content.Lines)-2].PlainString(), "[new]  New") ||
		!strings.Contains(content.Lines[len(content.Lines)-1].PlainString(), "[delete]  Delete") {
		t.Fatalf("expected tree header/search/row/detail/action, got %#v", content.Lines)
	}
	if !contentHasAction(content, "workbench.select") || !contentHasAction(content, "workbench.open") || !contentHasAction(content, "workbench.rename") || !contentHasAction(content, "workbench.new") || !contentHasAction(content, "workbench.delete") {
		t.Fatalf("expected workbench hit regions, got %#v", content.HitRegions)
	}
	if !content.Cursor.Visible || content.Cursor.Col != DisplayWidth("⌕ search 日志") {
		t.Fatalf("expected workbench search cursor, got %#v", content.Cursor)
	}

	root.Shell = state.DefaultShell().OpenWorkbenchTree().SetWorkbenchTreeQuery("missing")
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !content.Empty || !strings.Contains(content.Lines[2].PlainString(), "no workbench node selected") {
		t.Fatalf("expected empty tree page, got %#v", content)
	}
}

func TestRenderVMBuilderProjectsPromptAndHelpOverlay(t *testing.T) {
	shell := state.DefaultShell().OpenPrompt(state.PromptState{
		Title:       "Rename Pane",
		Context:     "输入新名称",
		Value:       "日志🚀",
		Placeholder: "name",
	})
	content := NewRenderVMBuilder().Build(state.Root{Shell: shell}).Shell.Overlay.Content
	if content.Kind != ContentPrompt ||
		!strings.Contains(content.Lines[0].PlainString(), "Rename Pane") ||
		!strings.Contains(content.Lines[1].PlainString(), "NAME 日志🚀") ||
		contentHasAction(content, "prompt.submit") ||
		contentHasAction(content, "prompt.cancel") {
		t.Fatalf("expected prompt content, got %#v", content)
	}
	if !content.Cursor.Visible || content.Cursor.Row != 1 || content.Cursor.Col != DisplayWidth("Name 日志🚀") {
		t.Fatalf("expected prompt cursor after input, got %#v", content.Cursor)
	}

	content = NewRenderVMBuilder().Build(state.Root{Shell: state.DefaultShell().OpenPrompt(state.PromptState{
		Title:       "Create Terminal",
		ActiveField: 1,
		Fields: []state.PromptFieldState{
			{Key: "name", Label: "name", Value: "shell", Required: true},
			{Key: "command", Label: "command", Value: "/bin/sh"},
			{Key: "workdir", Label: "workdir", Value: "/tmp"},
		},
	})}).Shell.Overlay.Content
	if !strings.Contains(content.Lines[0].PlainString(), "Create Terminal") ||
		!strings.Contains(content.Lines[1].PlainString(), "name*: shell") ||
		!strings.Contains(content.Lines[2].PlainString(), "command: /bin/sh") ||
		strings.Contains(plainLines(content.Lines), "name is required") ||
		contentHasAction(content, "prompt.submit") ||
		contentHasAction(content, "prompt.cancel") {
		t.Fatalf("expected create terminal form content, got %#v", content.Lines)
	}
	if !lineHasStyledCell(content.Lines[1], "name*: ", StyleStrongForeground) {
		t.Fatalf("expected prompt labels to use strong foreground, got %#v", content.Lines[1])
	}
	if content.Cursor.Row != 2 || content.Cursor.Col != DisplayWidth("command: /bin/sh") {
		t.Fatalf("expected form cursor on active command field, got %#v", content.Cursor)
	}

	promptVM := NewRenderVMBuilder().Build(state.Root{Shell: state.DefaultShell().OpenPrompt(state.PromptState{
		Title:              "Create Terminal",
		ActiveField:        2,
		SuggestionFocused:  true,
		SuggestionSelected: 6,
		SuggestionOffset:   1,
		Fields: []state.PromptFieldState{
			{Key: "name", Label: "name", Value: "shell", Required: true},
			{Key: "command", Label: "command", Value: "/bin/sh"},
			{
				Key:             "workdir",
				Label:           "workdir",
				Value:           "/tmp/de",
				Cursor:          len([]rune("/tmp/de")),
				SuggestionTitle: "path: /tmp",
				SuggestionItems: []string{
					"/tmp/d0/",
					"/tmp/d1/",
					"/tmp/d2/",
					"/tmp/d3/",
					"/tmp/d4/",
					"/tmp/d5/",
					"/tmp/dev/",
					"/tmp/d7/",
				},
			},
		},
	})})
	content = promptVM.Shell.Overlay.Content
	popup := promptVM.Shell.Overlay.Popup
	if strings.Contains(plainLines(content.Lines), "path: /tmp") ||
		strings.Contains(plainLines(content.Lines), "/tmp/dev/") {
		t.Fatalf("prompt content should not own suggestion popup rows, got %#v", content.Lines)
	}
	if popup.Kind != OverlayPopupPromptSuggestion ||
		!strings.Contains(plainLines(popup.Lines), "path: /tmp") ||
		!strings.Contains(plainLines(popup.Lines), "▸   dev/") ||
		!strings.Contains(plainLines(popup.Lines), "2-7/8") ||
		strings.Contains(plainLines(popup.Lines), "/tmp/d0/") ||
		strings.Contains(plainLines(popup.Lines), "│") ||
		!lineHasStyledCell(popup.Lines[6], "dev/", StylePromptSuggestionHit) {
		t.Fatalf("expected workdir suggestion popup rows, got %#v", popup)
	}
	if content.Cursor.Row != 3 || content.Cursor.Col != DisplayWidth("workdir: /tmp/de") {
		t.Fatalf("expected cursor on workdir field before suggestion rows, got %#v", content.Cursor)
	}

	content = NewRenderVMBuilder().Build(state.Root{Shell: state.DefaultShell().OpenHelp("most-used")}).Shell.Overlay.Content
	if content.Kind != ContentHelp ||
		!strings.Contains(content.Lines[0].PlainString(), "Help") ||
		!strings.Contains(content.Lines[1].PlainString(), "Most used") ||
		!strings.Contains(content.Lines[4].PlainString(), "Footer") ||
		!strings.Contains(content.Lines[6].PlainString(), "Terminal Pool") ||
		!contentHasAction(content, "help.close") {
		t.Fatalf("expected help content, got %#v", content)
	}
	helpPlain := plainLines(content.Lines)
	for _, forbidden := range []string{"edit metadata", "center", "collapse", "workspace delete"} {
		if strings.Contains(helpPlain, forbidden) {
			t.Fatalf("help must only show wired actions, found %q in %q", forbidden, helpPlain)
		}
	}
	if content.Cursor.Visible {
		t.Fatalf("help overlay should not show input cursor, got %#v", content.Cursor)
	}
}

func TestRenderVMBuilderShowsLiveError(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{Err: "boom"},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Status != "error: boom" || content.Error != "boom" {
		t.Fatalf("expected error content, got %#v", content)
	}
}

func TestRendererConsumesShellVMAndSanitizesContent(t *testing.T) {
	renderer := NewRenderer(DefaultTheme())
	result := renderer.RenderResult(RenderVM{
		Shell: ShellVM{
			Cursor: Cursor{Visible: true, Row: 2, Col: 3, Shape: CursorShapeBlock},
			Footer: FooterVM{Visible: true, Mode: "copy", Hint: "copy"},
			Layout: LayoutVM{Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "copy",
				Presentation: PanelPresentationCard,
				Active:       true,
				Content: ContentVM{
					Kind:  ContentCopyHistory,
					Lines: []Line{NewLine("hello\nworld")},
				},
			}}},
		},
	})
	frame := result.Frame()

	if len(frame.Lines) != defaultHeight {
		t.Fatalf("expected default frame height %d, got %d", defaultHeight, len(frame.Lines))
	}
	if !frameContains(frame, "hello world") {
		t.Fatalf("expected sanitized content inside panel, got %#v", frame.Lines)
	}
	if !frameContains(frame, "copy") {
		t.Fatalf("expected styled status line to contain text, got %q", frame.Lines[1])
	}
	if result.Metadata.Height != defaultHeight || result.Metadata.Width != defaultWidth {
		t.Fatalf("expected render metadata, got %#v", result.Metadata)
	}
	if !result.Cursor.Visible || result.Cursor.Row != 2 || result.Cursor.Col != 3 {
		t.Fatalf("expected cursor passthrough, got %#v", result.Cursor)
	}
}

func frameContains(frame Frame, value string) bool {
	for _, line := range frame.Lines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}

func containsFooterAction(actions []FooterActionVM, key string, label string, actionID string) bool {
	for _, action := range actions {
		if action.Key == key && action.Label == label && action.ActionID == actionID {
			return true
		}
	}
	return false
}

func lineHasStyledCell(line Line, text string, style StyleToken) bool {
	for _, cell := range line.Cells {
		if cell.Text == text && cell.Style == style {
			return true
		}
	}
	return false
}

func lineHasANSICell(line Line, text string, style ANSICellStyle) bool {
	for _, cell := range line.Cells {
		if cell.Text == text && cell.ANSIStyle == style && cell.Style == "" {
			return true
		}
	}
	return false
}

func lineHasLinkCell(line Line, text string, linkURL string, linkParams string) bool {
	for _, cell := range line.Cells {
		if cell.Text == text && cell.LinkURL == linkURL && cell.LinkParams == linkParams {
			return true
		}
	}
	return false
}

func contentHasAction(content ContentVM, actionID string) bool {
	for _, region := range content.HitRegions {
		if region.Kind == HitRegionContentAction && region.ActionID == actionID {
			return true
		}
	}
	return false
}

func TestStyleHelpersWidthAndTruncateANSI(t *testing.T) {
	styled := StatusStyle(DefaultTheme()).Render("abcdef")
	if Width(styled) != 6 {
		t.Fatalf("expected ANSI-safe width 6, got %d", Width(styled))
	}
	if got := Truncate(styled, 3); Width(got) != 3 {
		t.Fatalf("expected truncated width 3, got width=%d value=%q", Width(got), got)
	}
	if got := SafeLine("a\nb"); got != "a b" {
		t.Fatalf("unexpected safe line %q", got)
	}
}

func TestWidthSafeHelpersKeepRowsAtTargetDisplayWidth(t *testing.T) {
	cases := []struct {
		name  string
		value string
		width int
	}{
		{name: "cjk", value: "世界", width: 6},
		{name: "emoji", value: "ok 🚀", width: 8},
		{name: "combining", value: "e\u0301cho", width: 7},
		{name: "ansi", value: StatusStyle(DefaultTheme()).Render("warn 🚀"), width: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fitted := FitText(tc.value, tc.width)
			if got := DisplayWidth(fitted); got != tc.width {
				t.Fatalf("expected fitted width %d, got %d for %q", tc.width, got, fitted)
			}
			line := LineFromText(tc.value, tc.width)
			if got := line.Width(); got != tc.width {
				t.Fatalf("expected line width %d, got %d for %#v", tc.width, got, line)
			}
		})
	}
}

func TestWidthSafeTruncateDoesNotExceedTargetWidth(t *testing.T) {
	value := StatusStyle(DefaultTheme()).Render("title 🚀 世界")
	for width := 1; width <= 8; width++ {
		truncated := TruncateCells(value, width)
		if got := DisplayWidth(truncated); got > width {
			t.Fatalf("truncated width exceeds target width=%d got=%d value=%q", width, got, truncated)
		}
	}
}

func TestRenderVMBuilderCarriesHostAwareTheme(t *testing.T) {
	root := state.Root{}
	root.HostTheme = root.HostTheme.ApplyUpdate(state.HostThemeUpdate{DefaultFG: "#eeeeee"})
	root.HostTheme = root.HostTheme.ApplyUpdate(state.HostThemeUpdate{DefaultBG: "#101010"})
	root.HostTheme = root.HostTheme.ApplyUpdate(state.HostThemeUpdate{PaletteIndex: 5, PaletteColor: "#bb66ff"})
	vm := NewRenderVMBuilder().Build(root)
	if vm.Theme.HostFG != "#eeeeee" || vm.Theme.HostBG != "#101010" || vm.Theme.Accent != "#bb66ff" {
		t.Fatalf("expected host-aware theme in render VM, got %#v", vm.Theme)
	}
	frame := NewRenderer(DefaultTheme()).Render(RenderVM{
		Shell: ShellVM{
			Layout: LayoutVM{
				Viewport: Rect{W: 20, H: 3},
				Panels: []PanelVM{{
					ID:           "pane-1",
					Title:        "pane",
					Active:       true,
					Presentation: PanelPresentationCard,
					Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("ok")}},
				}},
			},
		},
		Theme: vm.Theme,
	})
	if frame.Theme.Accent != "#bb66ff" {
		t.Fatalf("renderer should prefer VM host-aware theme, got %#v", frame.Theme)
	}
	if !strings.Contains(strings.Join(frame.ANSILines, "\n"), "38;2;187;102;255") {
		t.Fatalf("styled frame should use host-aware accent, got %#v", frame.ANSILines)
	}
}

func plainLines(lines []Line) string {
	values := make([]string, len(lines))
	for i, line := range lines {
		values[i] = line.PlainString()
	}
	return strings.Join(values, "\n")
}
