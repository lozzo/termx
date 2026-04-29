package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
	localvterm "github.com/lozzow/termx/termx-core/vterm"
)

type cursorWriterPatchPlannerFrame struct {
	lines  []string
	meta   *presentMeta
	cursor string
	mode   verticalScrollMode
}

type cursorWriterPatchPlannerScenario struct {
	name     string
	width    int
	height   int
	previous cursorWriterPatchPlannerFrame
	next     cursorWriterPatchPlannerFrame
}

type cursorWriterPatchPlannerReplayResult struct {
	screen localvterm.ScreenData
	cursor localvterm.CursorState
}

type cursorWriterPatchPlannerVariant struct {
	name          string
	remoteLatency string
	lrScrollEnv   string
	configure     func(*outputCursorWriter, cursorWriterPatchPlannerFrame)
}

var cursorWriterPatchPlannerRequiredScenarioNames = []string{
	"single_pane_full_width_scroll",
	"single_pane_scroll_with_cursor_motion",
	"stacked_panes",
	"side_by_side_panes",
	"side_by_side_panes_with_cursor_motion",
	"base_pane_with_floating_overlay",
	"base_pane_underlay_scroll_with_stable_floating_overlay",
	"floating_move",
	"floating_resize",
	"multiple_floating_panes_overlap",
	"overlay_modal_show_hide",
	"alt_screen_app_in_multi_pane_layout",
}

var cursorWriterPatchPlannerBenchmarkVariants = []cursorWriterPatchPlannerVariant{
	{
		name:          "diff_only",
		remoteLatency: "0",
		lrScrollEnv:   "0",
		configure: func(writer *outputCursorWriter, _ cursorWriterPatchPlannerFrame) {
			writer.SetVerticalScrollMode(verticalScrollModeNone)
			writer.SetOwnerAwareDeltaEnabled(false)
		},
	},
	{
		name:          "policy_local",
		remoteLatency: "0",
		lrScrollEnv:   "",
		configure: func(writer *outputCursorWriter, frame cursorWriterPatchPlannerFrame) {
			writer.SetVerticalScrollMode(frame.mode)
			writer.SetOwnerAwareDeltaEnabled(true)
		},
	},
	{
		name:          "policy_remote",
		remoteLatency: "1",
		lrScrollEnv:   "",
		configure: func(writer *outputCursorWriter, frame cursorWriterPatchPlannerFrame) {
			writer.SetVerticalScrollMode(frame.mode)
			writer.SetOwnerAwareDeltaEnabled(true)
		},
	},
}

func TestOutputCursorWriterPatchPlannerScenarioMatrixPreservesFinalFrames(t *testing.T) {
	scenarios := cursorWriterPatchPlannerScenarioMatrix(t)
	if got := cursorWriterPatchPlannerScenarioNames(scenarios); !slices.Equal(got, cursorWriterPatchPlannerRequiredScenarioNames) {
		t.Fatalf("unexpected patch-planner scenario matrix names: got=%v want=%v", got, cursorWriterPatchPlannerRequiredScenarioNames)
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			for _, variant := range cursorWriterPatchPlannerBenchmarkVariants[1:] {
				t.Run(variant.name, func(t *testing.T) {
					want := cursorWriterPatchPlannerReplay(t, scenario.width, scenario.height, []cursorWriterPatchPlannerFrame{scenario.next}, variant)
					got := cursorWriterPatchPlannerReplay(t, scenario.width, scenario.height, []cursorWriterPatchPlannerFrame{scenario.previous, scenario.next}, variant)
					if err := screenDiffError(got.screen, want.screen); err != nil {
						t.Fatalf("%s replay diverged: %v", variant.name, err)
					}
					if got.cursor != want.cursor {
						t.Fatalf("%s cursor diverged: got=%#v want=%#v", variant.name, got.cursor, want.cursor)
					}
				})
			}
		})
	}
}

func TestOutputCursorWriterPatchPlannerPolicyBeatsDiffOnlyForSinglePaneScrollScenarios(t *testing.T) {
	scenarios := cursorWriterPatchPlannerScenarioMatrix(t)
	diffOnly := cursorWriterPatchPlannerVariantByName(t, "diff_only")
	policyLocal := cursorWriterPatchPlannerVariantByName(t, "policy_local")

	for _, name := range []string{
		"single_pane_full_width_scroll",
		"single_pane_scroll_with_cursor_motion",
	} {
		scenario := cursorWriterPatchPlannerScenarioByName(t, scenarios, name)
		t.Run(name, func(t *testing.T) {
			diffBytes := cursorWriterPatchPlannerDeltaBytes(t, scenario, diffOnly)
			policyBytes := cursorWriterPatchPlannerDeltaBytes(t, scenario, policyLocal)
			if policyBytes >= diffBytes {
				t.Fatalf("expected %s to beat diff-only baseline, got policy=%d diff_only=%d", name, policyBytes, diffBytes)
			}
		})
	}
}

func TestOutputCursorWriterPatchPlannerPolicyMatchesDiffOnlyFloorForComplexLayoutHotspots(t *testing.T) {
	scenarios := cursorWriterPatchPlannerScenarioMatrix(t)
	diffOnly := cursorWriterPatchPlannerVariantByName(t, "diff_only")

	cases := []struct {
		name    string
		variant string
	}{
		{name: "side_by_side_panes", variant: "policy_remote"},
		{name: "base_pane_with_floating_overlay", variant: "policy_remote"},
		{name: "base_pane_underlay_scroll_with_stable_floating_overlay", variant: "policy_remote"},
		{name: "floating_move", variant: "policy_local"},
		{name: "floating_resize", variant: "policy_local"},
	}

	for _, tc := range cases {
		tc := tc
		scenario := cursorWriterPatchPlannerScenarioByName(t, scenarios, tc.name)
		variant := cursorWriterPatchPlannerVariantByName(t, tc.variant)
		t.Run(tc.name+"/"+tc.variant, func(t *testing.T) {
			diffBytes := cursorWriterPatchPlannerDeltaBytes(t, scenario, diffOnly)
			policyBytes := cursorWriterPatchPlannerDeltaBytes(t, scenario, variant)
			if policyBytes != diffBytes {
				t.Fatalf("expected %s to match the diff-only floor, got policy=%d diff_only=%d", tc.name, policyBytes, diffBytes)
			}
		})
	}
}

func BenchmarkOutputCursorWriterWriteFrameLinesPatchPlannerScenarioMatrix(b *testing.B) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	scenarios := cursorWriterPatchPlannerScenarioMatrix(b)
	if len(scenarios) == 0 {
		b.Fatal("expected non-empty patch-planner scenario matrix")
	}

	for _, scenario := range scenarios {
		scenario := scenario
		b.Run(scenario.name, func(b *testing.B) {
			for _, variant := range cursorWriterPatchPlannerBenchmarkVariants {
				variant := variant
				b.Run(variant.name, func(b *testing.B) {
					b.Setenv("TERMX_REMOTE_LATENCY", variant.remoteLatency)
					b.Setenv("TERMX_EXPERIMENTAL_LR_SCROLL", variant.lrScrollEnv)

					sink := &cursorWriterBenchmarkSink{}
					writer := newOutputCursorWriter(sink)

					variant.configure(writer, scenario.previous)
					if err := writer.WriteFrameLinesWithMeta(scenario.previous.lines, scenario.previous.cursor, scenario.previous.meta); err != nil {
						b.Fatalf("prime patch-planner frame: %v", err)
					}
					sink.Reset()

					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						frame := scenario.next
						if i%2 != 0 {
							frame = scenario.previous
						}
						variant.configure(writer, frame)
						if err := writer.WriteFrameLinesWithMeta(frame.lines, frame.cursor, frame.meta); err != nil {
							b.Fatalf("write patch-planner frame: %v", err)
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(sink.bytes)/float64(maxInt(1, b.N)), "bytes/op")
				})
			}
		})
	}
}

func TestBetterFramePatchCandidateUsesWireCost(t *testing.T) {
	current := framePatchCandidate{mode: framePatchCandidateDiff, payload: "abcdef"}
	candidate := framePatchCandidate{mode: framePatchCandidateVerticalScrollRows, payload: "a\nb\nc"}
	if betterFramePatchCandidate(candidate, current) {
		t.Fatalf("expected wire-cost comparison to reject newline-heavier candidate: candidate=%d current=%d", candidate.byteCost(), current.byteCost())
	}
}

func TestFramePatchCandidateByteCostCountsCRLFExpansion(t *testing.T) {
	candidate := framePatchCandidate{mode: framePatchCandidateDiff, payload: "a\nb\nc"}
	if got, want := candidate.byteCost(), len("a\r\nb\r\nc"); got != want {
		t.Fatalf("unexpected wire byte cost: got=%d want=%d", got, want)
	}
}

func cursorWriterPatchPlannerScenarioMatrix(tb testing.TB) []cursorWriterPatchPlannerScenario {
	tb.Helper()
	return []cursorWriterPatchPlannerScenario{
		cursorWriterPatchPlannerSinglePaneFullWidthScrollScenario(tb),
		cursorWriterPatchPlannerSinglePaneScrollWithCursorMotionScenario(tb),
		cursorWriterPatchPlannerStackedPanesScenario(tb),
		cursorWriterPatchPlannerSideBySidePanesScenario(tb),
		cursorWriterPatchPlannerSideBySidePanesWithCursorMotionScenario(tb),
		cursorWriterPatchPlannerBasePaneWithFloatingOverlayScenario(tb),
		cursorWriterPatchPlannerBasePaneUnderlayScrollWithStableFloatingOverlayScenario(tb),
		cursorWriterPatchPlannerFloatingMoveScenario(tb),
		cursorWriterPatchPlannerFloatingResizeScenario(tb),
		cursorWriterPatchPlannerMultipleFloatingPanesOverlapScenario(tb),
		cursorWriterPatchPlannerOverlayModalShowHideScenario(tb),
		cursorWriterPatchPlannerAltScreenAppInMultiPaneLayoutScenario(tb),
	}
}

func cursorWriterPatchPlannerScenarioNames(scenarios []cursorWriterPatchPlannerScenario) []string {
	names := make([]string, 0, len(scenarios))
	for _, scenario := range scenarios {
		names = append(names, scenario.name)
	}
	return names
}

func cursorWriterPatchPlannerScenarioByName(tb testing.TB, scenarios []cursorWriterPatchPlannerScenario, name string) cursorWriterPatchPlannerScenario {
	tb.Helper()
	for _, scenario := range scenarios {
		if scenario.name == name {
			return scenario
		}
	}
	tb.Fatalf("missing patch-planner scenario %q", name)
	return cursorWriterPatchPlannerScenario{}
}

func cursorWriterPatchPlannerVariantByName(tb testing.TB, name string) cursorWriterPatchPlannerVariant {
	tb.Helper()
	for _, variant := range cursorWriterPatchPlannerBenchmarkVariants {
		if variant.name == name {
			return variant
		}
	}
	tb.Fatalf("missing patch-planner variant %q", name)
	return cursorWriterPatchPlannerVariant{}
}

func cursorWriterPatchPlannerDeltaBytes(tb testing.TB, scenario cursorWriterPatchPlannerScenario, variant cursorWriterPatchPlannerVariant) int {
	tb.Helper()
	cursorWriterPatchPlannerSetenv(tb, "TERMX_REMOTE_LATENCY", variant.remoteLatency)
	cursorWriterPatchPlannerSetenv(tb, "TERMX_EXPERIMENTAL_LR_SCROLL", variant.lrScrollEnv)
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterBenchmarkSink{}
	writer := newOutputCursorWriter(sink)
	variant.configure(writer, scenario.previous)
	if err := writer.WriteFrameLinesWithMeta(scenario.previous.lines, scenario.previous.cursor, scenario.previous.meta); err != nil {
		tb.Fatalf("prime patch-planner scenario: %v", err)
	}
	sink.Reset()
	variant.configure(writer, scenario.next)
	if err := writer.WriteFrameLinesWithMeta(scenario.next.lines, scenario.next.cursor, scenario.next.meta); err != nil {
		tb.Fatalf("write patch-planner scenario delta: %v", err)
	}
	return sink.bytes
}

func cursorWriterPatchPlannerReplay(tb testing.TB, width, height int, frames []cursorWriterPatchPlannerFrame, variant cursorWriterPatchPlannerVariant) cursorWriterPatchPlannerReplayResult {
	tb.Helper()
	cursorWriterPatchPlannerSetenv(tb, "TERMX_REMOTE_LATENCY", variant.remoteLatency)
	cursorWriterPatchPlannerSetenv(tb, "TERMX_EXPERIMENTAL_LR_SCROLL", variant.lrScrollEnv)
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	for _, frame := range frames {
		variant.configure(writer, frame)
		if err := writer.WriteFrameLinesWithMeta(frame.lines, frame.cursor, frame.meta); err != nil {
			tb.Fatalf("write patch-planner replay frame: %v", err)
		}
	}

	sink.mu.Lock()
	stream := strings.Join(sink.writes, "")
	sink.mu.Unlock()

	vt := localvterm.New(width, height, 0, nil)
	if _, err := vt.Write([]byte(stream)); err != nil {
		tb.Fatalf("replay patch-planner stream: %v", err)
	}
	return cursorWriterPatchPlannerReplayResult{
		screen: vt.ScreenContent(),
		cursor: vt.CursorState(),
	}
}

func cursorWriterPatchPlannerSetenv(tb testing.TB, key, value string) {
	tb.Helper()
	setenv, ok := any(tb).(interface {
		Setenv(string, string)
	})
	if !ok {
		tb.Fatalf("%T does not support Setenv", tb)
	}
	setenv.Setenv(key, value)
}

func cursorWriterPatchPlannerCaptureFrame(tb testing.TB, model *Model) cursorWriterPatchPlannerFrame {
	tb.Helper()
	model.render.Invalidate()
	result := model.render.Render()
	mode, _ := model.verticalScrollOptimizationMode()
	return cursorWriterPatchPlannerFrame{
		lines:  append([]string(nil), result.Lines...),
		meta:   presentMetaFromRender(result.Meta),
		cursor: result.CursorSequence(),
		mode:   mode,
	}
}

func cursorWriterPatchPlannerSetupModel(tb testing.TB, opts modelOpts) *Model {
	tb.Helper()
	if opts.width == 0 {
		opts.width = 120
	}
	if opts.height == 0 {
		opts.height = 40
	}
	if opts.client == nil {
		opts.client = &recordingBridgeClient{
			attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
			snapshotByTerminal: map[string]*protocol.Snapshot{},
		}
	}

	rt := runtime.New(opts.client)
	wb := workbench.NewWorkbench()
	if opts.workspaces != nil {
		for name, ws := range opts.workspaces {
			wb.AddWorkspace(name, ws)
		}
	} else {
		wb.AddWorkspace("main", &workbench.WorkspaceState{
			Name:      "main",
			ActiveTab: 0,
			Tabs: []*workbench.TabState{{
				ID:           "tab-1",
				Name:         "tab 1",
				ActivePaneID: "pane-1",
				Panes: map[string]*workbench.PaneState{
					"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
				},
				Root: workbench.NewLeaf("pane-1"),
			}},
		})
		rt.Registry().GetOrCreate("term-1").Name = "shell"
		rt.Registry().Get("term-1").State = "running"
		rt.Registry().Get("term-1").Channel = 1
		binding := rt.BindPane("pane-1")
		binding.Channel = 1
		binding.Connected = true
	}

	model := New(shared.Config{WorkspaceStatePath: opts.statePath}, wb, rt)
	model.width = opts.width
	model.height = opts.height
	return model
}

func cursorWriterPatchPlannerSetupTwoPaneModel(tb testing.TB, direction workbench.SplitDirection) *Model {
	tb.Helper()
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "pane 1", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "pane 2", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: direction,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})
	model := cursorWriterPatchPlannerSetupModel(tb, modelOpts{
		width:      120,
		height:     36,
		workspaces: map[string]*workbench.WorkspaceState{"main": wb.CurrentWorkspace()},
	})
	for i, paneID := range []string{"pane-1", "pane-2"} {
		binding := model.runtime.BindPane(paneID)
		binding.Channel = uint16(i + 1)
		binding.Connected = true
		terminalID := "term-" + string('1'+rune(i))
		terminal := model.runtime.Registry().GetOrCreate(terminalID)
		terminal.Name = terminalID
		terminal.State = "running"
		terminal.Channel = uint16(i + 1)
	}
	return model
}

func cursorWriterPatchPlannerRequirePane(tb testing.TB, model *Model, paneID string) *workbench.PaneState {
	tb.Helper()
	tab := model.workbench.CurrentTab()
	if tab == nil {
		tb.Fatal("expected current tab")
	}
	pane := tab.Panes[paneID]
	if pane == nil {
		tb.Fatalf("expected pane %q", paneID)
	}
	return pane
}

func cursorWriterPatchPlannerSetPaneSnapshot(tb testing.TB, model *Model, paneID string, factory func(string, int, int) *protocol.Snapshot) {
	tb.Helper()
	pane := cursorWriterPatchPlannerRequirePane(tb, model, paneID)
	visiblePane, ok := model.visiblePaneProjection(paneID)
	if !ok {
		tb.Fatalf("expected visible pane projection for %q", paneID)
	}
	contentRect, ok := paneContentRectForVisible(visiblePane)
	if !ok {
		tb.Fatalf("expected content rect for %q", paneID)
	}
	terminal := model.runtime.Registry().GetOrCreate(pane.TerminalID)
	terminal.Name = pane.TerminalID
	terminal.State = "running"
	terminal.Snapshot = factory(pane.TerminalID, contentRect.W, contentRect.H)
	binding := model.runtime.BindPane(paneID)
	if binding.Channel == 0 {
		binding.Channel = 1
	}
	binding.Connected = true
}

func cursorWriterPatchPlannerCreateFloatingPane(tb testing.TB, model *Model, paneID, terminalID string, rect workbench.Rect) {
	tb.Helper()
	tab := model.workbench.CurrentTab()
	if tab == nil {
		tb.Fatal("expected current tab")
	}
	if err := model.workbench.CreateFloatingPane(tab.ID, paneID, rect); err != nil {
		tb.Fatalf("create floating pane %q: %v", paneID, err)
	}
	if err := model.workbench.BindPaneTerminal(tab.ID, paneID, terminalID); err != nil {
		tb.Fatalf("bind floating pane %q: %v", paneID, err)
	}
	terminal := model.runtime.Registry().GetOrCreate(terminalID)
	terminal.Name = terminalID
	terminal.State = "running"
	binding := model.runtime.BindPane(paneID)
	binding.Channel = uint16(len(tab.Panes) + 1)
	binding.Connected = true
}

func cursorWriterPatchPlannerWithCursor(snapshot *protocol.Snapshot, row, col int) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	out := *snapshot
	out.Cursor = snapshot.Cursor
	out.Cursor.Row = maxInt(0, minInt(int(snapshot.Size.Rows)-1, row))
	out.Cursor.Col = maxInt(0, minInt(int(snapshot.Size.Cols)-1, col))
	return &out
}

func cursorWriterPatchPlannerSinglePaneFullWidthScrollScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupModel(tb, modelOpts{width: 120, height: 36})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 1)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 2)
	})
	touchRuntimeVisibleStateForTest(model.runtime, 1)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "single_pane_full_width_scroll",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerSinglePaneScrollWithCursorMotionScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupModel(tb, modelOpts{width: 120, height: 36})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterPatchPlannerWithCursor(cursorWriterDenseTextSnapshot(terminalID, cols, rows, 1), rows-2, minInt(cols-1, 12))
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterPatchPlannerWithCursor(cursorWriterDenseTextSnapshot(terminalID, cols, rows, 2), rows-3, minInt(cols-1, 28))
	})
	touchRuntimeVisibleStateForTest(model.runtime, 2)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "single_pane_scroll_with_cursor_motion",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerStackedPanesScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupTwoPaneModel(tb, workbench.SplitHorizontal)
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 1)
	})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-2", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 200)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 2)
	})
	touchRuntimeVisibleStateForTest(model.runtime, 3)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "stacked_panes",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerSideBySidePanesScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupTwoPaneModel(tb, workbench.SplitVertical)
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 1)
	})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-2", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 200)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 2)
	})
	touchRuntimeVisibleStateForTest(model.runtime, 4)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "side_by_side_panes",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerSideBySidePanesWithCursorMotionScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupTwoPaneModel(tb, workbench.SplitVertical)
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterPatchPlannerWithCursor(cursorWriterDenseTextSnapshot(terminalID, cols, rows, 1), rows-2, minInt(cols-1, 10))
	})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-2", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 200)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterPatchPlannerWithCursor(cursorWriterDenseTextSnapshot(terminalID, cols, rows, 2), rows-4, minInt(cols-1, 20))
	})
	touchRuntimeVisibleStateForTest(model.runtime, 5)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "side_by_side_panes_with_cursor_motion",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerBasePaneWithFloatingOverlayScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupModel(tb, modelOpts{width: 120, height: 36})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterNvimLikeSnapshot(terminalID, cols, rows, "#444444")
	})
	cursorWriterPatchPlannerCreateFloatingPane(tb, model, "float-1", "term-float", workbench.Rect{X: 18, Y: 7, W: 54, H: 16})
	if err := model.workbench.FocusPane(model.workbench.CurrentTab().ID, "float-1"); err != nil {
		tb.Fatalf("focus floating pane: %v", err)
	}
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "float-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 1)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "float-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 2)
	})
	touchRuntimeVisibleStateForTest(model.runtime, 6)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "base_pane_with_floating_overlay",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerBasePaneUnderlayScrollWithStableFloatingOverlayScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupModel(tb, modelOpts{width: 120, height: 36})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 1)
	})
	cursorWriterPatchPlannerCreateFloatingPane(tb, model, "float-1", "term-float", workbench.Rect{X: 18, Y: 7, W: 54, H: 16})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "float-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterStyledSnapshot(terminalID, cols, rows)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 2)
	})
	touchRuntimeVisibleStateForTest(model.runtime, 7)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "base_pane_underlay_scroll_with_stable_floating_overlay",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerFloatingMoveScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupModel(tb, modelOpts{width: 120, height: 36})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterStyledSnapshot(terminalID, cols, rows)
	})
	cursorWriterPatchPlannerCreateFloatingPane(tb, model, "float-1", "term-float", workbench.Rect{X: 18, Y: 7, W: 54, H: 16})
	if err := model.workbench.FocusPane(model.workbench.CurrentTab().ID, "float-1"); err != nil {
		tb.Fatalf("focus floating pane: %v", err)
	}
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "float-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterStyledSnapshot(terminalID, cols, rows)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	if !model.workbench.MoveFloatingPane(model.workbench.CurrentTab().ID, "float-1", 28, 9) {
		tb.Fatal("expected floating move to change geometry")
	}
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "float-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterStyledSnapshot(terminalID, cols, rows)
	})
	touchRuntimeVisibleStateForTest(model.runtime, 8)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "floating_move",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerFloatingResizeScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupModel(tb, modelOpts{width: 120, height: 36})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterStyledSnapshot(terminalID, cols, rows)
	})
	cursorWriterPatchPlannerCreateFloatingPane(tb, model, "float-1", "term-float", workbench.Rect{X: 18, Y: 7, W: 40, H: 12})
	if err := model.workbench.FocusPane(model.workbench.CurrentTab().ID, "float-1"); err != nil {
		tb.Fatalf("focus floating pane: %v", err)
	}
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "float-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 1)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	if !model.workbench.ResizeFloatingPane(model.workbench.CurrentTab().ID, "float-1", 54, 16) {
		tb.Fatal("expected floating resize to change geometry")
	}
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "float-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 1)
	})
	touchRuntimeVisibleStateForTest(model.runtime, 9)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "floating_resize",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerMultipleFloatingPanesOverlapScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupModel(tb, modelOpts{width: 120, height: 36})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterNvimLikeSnapshot(terminalID, cols, rows, "#444444")
	})
	cursorWriterPatchPlannerCreateFloatingPane(tb, model, "float-1", "term-float-1", workbench.Rect{X: 18, Y: 7, W: 54, H: 16})
	cursorWriterPatchPlannerCreateFloatingPane(tb, model, "float-2", "term-float-2", workbench.Rect{X: 56, Y: 9, W: 44, H: 14})
	if err := model.workbench.FocusPane(model.workbench.CurrentTab().ID, "float-1"); err != nil {
		tb.Fatalf("focus floating pane: %v", err)
	}
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "float-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterStyledSnapshot(terminalID, cols, rows)
	})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "float-2", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 300)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	if !model.workbench.MoveFloatingPane(model.workbench.CurrentTab().ID, "float-1", 30, 8) {
		tb.Fatal("expected overlapping floating move to change geometry")
	}
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "float-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterStyledSnapshot(terminalID, cols, rows)
	})
	touchRuntimeVisibleStateForTest(model.runtime, 10)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "multiple_floating_panes_overlap",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerOverlayModalShowHideScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupModel(tb, modelOpts{width: 120, height: 36})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterScrollingSnapshot(terminalID, cols, rows, 1)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:  "rename-tab",
		Title: "Rename Tab",
		Value: "planner-matrix",
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})
	touchRuntimeVisibleStateForTest(model.runtime, 11)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "overlay_modal_show_hide",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}

func cursorWriterPatchPlannerAltScreenAppInMultiPaneLayoutScenario(tb testing.TB) cursorWriterPatchPlannerScenario {
	tb.Helper()
	model := cursorWriterPatchPlannerSetupTwoPaneModel(tb, workbench.SplitVertical)
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterNvimScrollingSnapshot(terminalID, cols, rows, 1, "#444444")
	})
	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-2", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterDenseTextSnapshot(terminalID, cols, rows, 200)
	})
	previous := cursorWriterPatchPlannerCaptureFrame(tb, model)

	cursorWriterPatchPlannerSetPaneSnapshot(tb, model, "pane-1", func(terminalID string, cols, rows int) *protocol.Snapshot {
		return cursorWriterNvimScrollingSnapshot(terminalID, cols, rows, 2, "#444444")
	})
	touchRuntimeVisibleStateForTest(model.runtime, 12)
	next := cursorWriterPatchPlannerCaptureFrame(tb, model)

	return cursorWriterPatchPlannerScenario{
		name:     "alt_screen_app_in_multi_pane_layout",
		width:    model.width,
		height:   model.height,
		previous: previous,
		next:     next,
	}
}
