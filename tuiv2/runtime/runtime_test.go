package runtime

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-proto/wire"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-shared/terminalmeta"
	"github.com/lozzow/termx/termx-testkit"
	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/sessionstore"
	"github.com/lozzow/termx/tuiv2/shared"
)

func newTestRuntime(t *testing.T) (*Runtime, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	daemon := testkit.StartDaemon(t, ctx, "termx.sock")
	client := daemon.NewClient(t, ctx)

	return New(bridge.NewProtocolClient(client)), ctx
}

func TestRuntimeListTerminalsDoesNotPopulateRegistry(t *testing.T) {
	rt, ctx := newTestRuntime(t)

	created, err := rt.client.Create(ctx, protocol.CreateParams{
		Command: []string{"sh"},
		Name:    "demo",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	terminals, err := rt.ListTerminals(ctx)
	if err != nil {
		t.Fatalf("list terminals: %v", err)
	}
	if len(terminals) != 1 {
		t.Fatalf("expected 1 terminal, got %d", len(terminals))
	}
	stored := rt.Registry().Get(created.TerminalID)
	if stored != nil {
		t.Fatalf("expected list to avoid populating registry, got %#v", stored)
	}
}

func TestRuntimeApplyTerminalListPatchesRegistryMetadata(t *testing.T) {
	rt := New(nil)
	exitCode := 7

	rt.ApplyTerminalList([]protocol.TerminalInfo{{
		ID:       "term-1",
		Name:     "renamed-shell",
		Command:  []string{"zsh", "-l"},
		Tags:     map[string]string{"termx.environment": "prod"},
		State:    "exited",
		ExitCode: &exitCode,
	}})

	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal to be created in registry")
	}
	if terminal.Name != "renamed-shell" || terminal.State != "exited" {
		t.Fatalf("unexpected terminal metadata: %#v", terminal)
	}
	if !reflect.DeepEqual(terminal.Command, []string{"zsh", "-l"}) {
		t.Fatalf("unexpected command: %#v", terminal.Command)
	}
	if terminal.Tags["termx.environment"] != "prod" {
		t.Fatalf("unexpected tags: %#v", terminal.Tags)
	}
	if terminal.ExitCode == nil || *terminal.ExitCode != exitCode {
		t.Fatalf("unexpected exit code: %#v", terminal.ExitCode)
	}
}

func TestRuntimeApplyTerminalListDemotesLocalOwnerForExternalResizeOwner(t *testing.T) {
	rt := New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.BoundPaneIDs = []string{"pane-1"}
	terminal.OwnerPaneID = "pane-1"
	terminal.ControlPaneID = "pane-1"
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true
	binding.Role = BindingRoleOwner

	rt.ApplyTerminalList([]protocol.TerminalInfo{{
		ID:                         "term-1",
		State:                      "running",
		ResizeOwnerAttachmentCount: 2,
	}})

	if terminal.OwnerPaneID != externalResizeOwnerPaneID || terminal.ControlPaneID != "" || !terminal.RequiresExplicitOwner {
		t.Fatalf("expected external owner to demote local control, got owner=%q control=%q explicit=%v", terminal.OwnerPaneID, terminal.ControlPaneID, terminal.RequiresExplicitOwner)
	}
	if binding.Role != BindingRoleFollower {
		t.Fatalf("expected local binding to become follower, got %q", binding.Role)
	}

	rt.ApplyTerminalList([]protocol.TerminalInfo{{
		ID:                         "term-1",
		State:                      "running",
		ResizeOwnerAttachmentCount: 1,
	}})

	if terminal.OwnerPaneID != "pane-1" || terminal.ControlPaneID != "pane-1" || terminal.RequiresExplicitOwner {
		t.Fatalf("expected local owner to restore after external owner leaves, got owner=%q control=%q explicit=%v", terminal.OwnerPaneID, terminal.ControlPaneID, terminal.RequiresExplicitOwner)
	}
	if binding.Role != BindingRoleOwner {
		t.Fatalf("expected local binding to become owner again, got %q", binding.Role)
	}
}

func TestRuntimeAttachAndLoadSnapshotInitializesVTermCache(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 7, Mode: "collaborator"}
	client.listResult = &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
		ID:    "term-1",
		Name:  "shell",
		State: "running",
	}}}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 6, 3, []string{
		"hello",
		"world",
	})

	rt := New(client)

	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal.VTerm == nil {
		t.Fatal("expected attach to initialize a vterm")
	}

	snapshot, err := rt.LoadSnapshot(ctx, "term-1", 0, 10)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}

	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.Snapshot == nil {
		t.Fatal("expected snapshot cached on terminal runtime")
	}
	if stored.Name != "shell" {
		t.Fatalf("expected attach to hydrate terminal metadata name, got %q", stored.Name)
	}
	screen := stored.VTerm.ScreenContent()
	if len(screen.Cells) < 2 || len(screen.Cells[0]) < 5 || len(screen.Cells[1]) < 5 {
		t.Fatalf("unexpected vterm screen dimensions: %#v", screen.Cells)
	}
	if got := screen.Cells[0][0].Content + screen.Cells[0][1].Content + screen.Cells[0][2].Content + screen.Cells[0][3].Content + screen.Cells[0][4].Content; got != "hello" {
		t.Fatalf("expected first row to contain hello, got %q", got)
	}
	if got := screen.Cells[1][0].Content + screen.Cells[1][1].Content + screen.Cells[1][2].Content + screen.Cells[1][3].Content + screen.Cells[1][4].Content; got != "world" {
		t.Fatalf("expected second row to contain world, got %q", got)
	}
}

func TestRuntimeLoadGridViewportDoesNotReplaceLiveSnapshot(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 6, 3, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromString("new0"),
		protocolRowFromString("new1"),
		protocolRowFromString("new2"),
	})
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	loaded, err := rt.LoadSnapshot(ctx, "term-1", 0, 2)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if loaded == nil || len(loaded.Scrollback) != 3 {
		t.Fatalf("expected initial snapshot page, got %#v", loaded)
	}
	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.Snapshot == nil {
		t.Fatal("expected runtime snapshot")
	}
	before := stored.Snapshot

	page, err := rt.LoadGridViewport(ctx, "term-1", 2, 1, 6)
	if err != nil {
		t.Fatalf("load grid viewport: %v", err)
	}
	if page == nil || len(page.Scrollback) != 1 || compactRowText(page.Scrollback[0]) != "new0" {
		t.Fatalf("unexpected grid viewport page: %#v", page)
	}
	if stored.Snapshot != before {
		t.Fatal("expected history viewport load to leave live runtime snapshot untouched")
	}
}

func TestRuntimeApplyGridViewportPagePrependsHistory(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 6, 3, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromString("new0"),
		protocolRowFromString("new1"),
	})
	live.ScrollbackLoadedRows = 2
	live.HistoryGeneration = 7
	live.ScrollbackFirstRowID = 1
	live.ScrollbackLastRowID = 2
	markSnapshotScrollbackPersisted(live)
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 2); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.Snapshot == nil {
		t.Fatal("expected runtime snapshot")
	}
	page := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 6, Rows: 3},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("old0")}),
		ScrollbackOffset:       2,
		ScrollbackTotal:        3,
		ScrollbackLogicalTotal: 2,
		ScrollbackHasMore:      false,
		ScrollbackLoadedRows:   3,
		HistoryGeneration:      7,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackTimestamps:   []time.Time{time.Now()},
		ScrollbackOwnership:    []string{protocol.RowOwnershipPersisted},
	}
	if !rt.ApplyGridViewportPage("term-1", page, 2) {
		t.Fatal("expected viewport page to apply")
	}
	got := rt.Registry().Get("term-1").Snapshot
	if len(got.Scrollback) != 3 || compactRowText(got.Scrollback[0]) != "old0" || compactRowText(got.Scrollback[1]) != "new0" {
		t.Fatalf("expected older page prepended, got %#v", got.Scrollback)
	}
	if got.ScrollbackLogicalTotal != 2 {
		t.Fatalf("expected logical total from prepended page, got %d", got.ScrollbackLogicalTotal)
	}
	if !stored.CommittedHistoryExhausted {
		t.Fatal("expected exhausted flag from page metadata")
	}
	if !stored.PreferSnapshot {
		t.Fatal("expected history page to render from snapshot instead of reloading local vterm")
	}
	if stored.VTerm == nil || len(stored.VTerm.ScrollbackContent()) != 2 {
		t.Fatalf("expected local vterm scrollback to stay on the live snapshot, got %#v", stored.VTerm)
	}
}

func TestSnapshotFromGridViewportPreservesLogicalTotals(t *testing.T) {
	viewport := &protocol.GridViewport{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 80, Rows: 24},
		Rows:                   protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("old0")}),
		ScrollbackOffset:       10,
		ScrollbackTotal:        100,
		ScrollbackLogicalTotal: 40,
		ScrollbackHasMore:      true,
		LoadedRows:             12,
	}

	snapshot := snapshotFromGridViewport("term-1", viewport)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.ScrollbackTotal != 100 || snapshot.ScrollbackLogicalTotal != 40 {
		t.Fatalf("expected logical totals preserved, got total=%d logical=%d", snapshot.ScrollbackTotal, snapshot.ScrollbackLogicalTotal)
	}
}

func TestRuntimeApplyGridViewportPageReplacesLatestWindow(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 6, 3, []string{"live"})
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	page := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 6, Rows: 3},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("new0"), protocolRowFromString("new1")}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 1,
		ScrollbackHasMore:      false,
		ScrollbackRowKinds:     []string{"", ""},
		ScrollbackWrapped:      []bool{false, false},
		ScrollbackTimestamps: []time.Time{
			time.Now(),
			time.Now(),
		},
	}
	if !rt.ApplyGridViewportPage("term-1", page, 0) {
		t.Fatal("expected latest viewport page to apply")
	}
	got := rt.Registry().Get("term-1").Snapshot
	if len(got.Scrollback) != 2 || compactRowText(got.Scrollback[0]) != "new0" || compactRowText(got.Scrollback[1]) != "new1" {
		t.Fatalf("expected latest page to replace loaded scrollback, got %#v", got.Scrollback)
	}
	if got.ScrollbackLogicalTotal != 1 {
		t.Fatalf("expected latest page logical total preserved, got %d", got.ScrollbackLogicalTotal)
	}
}

func TestRuntimeApplyGridViewportPageTrimsLatestScreenOverlap(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 8, 4, []string{"line-078", "line-079", "line-080", "prompt"})
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	page := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 8, Rows: 4},
		Scrollback: protocol.CompactRowsFromCells([][]protocol.Cell{
			protocolRowFromString("line-076"),
			protocolRowFromString("line-077"),
			protocolRowFromString("line-078"),
			protocolRowFromString("line-079"),
			protocolRowFromString("line-080"),
		}),
		ScrollbackOwnership:    []string{protocol.RowOwnershipPersisted, protocol.RowOwnershipPersisted, protocol.RowOwnershipPersisted, protocol.RowOwnershipPersisted, protocol.RowOwnershipPersisted},
		ScrollbackOffset:       0,
		ScrollbackTotal:        81,
		ScrollbackLogicalTotal: 81,
		ScrollbackLoadedRows:   81,
		HistoryGeneration:      7,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    80,
	}

	if !rt.ApplyGridViewportPage("term-1", page, 0) {
		t.Fatal("expected latest viewport page to apply")
	}
	got := rt.Registry().Get("term-1").Snapshot
	if got == nil {
		t.Fatal("expected merged snapshot")
	}
	if gotRows := len(got.Scrollback); gotRows != 2 {
		t.Fatalf("expected duplicated screen prefix trimmed from latest scrollback, got %d rows", gotRows)
	}
	if got := compactRowText(got.Scrollback[0]); got != "line-076" {
		t.Fatalf("expected first retained row line-076, got %q", got)
	}
	if got := compactRowText(got.Scrollback[1]); got != "line-077" {
		t.Fatalf("expected second retained row line-077, got %q", got)
	}
	if got, want := got.ScrollbackLoadedRows, 81; got != want {
		t.Fatalf("expected committed loaded depth to stay %d, got %d", want, got)
	}
	if got, want := got.ScrollbackLastRowID, uint64(80); got != want {
		t.Fatalf("expected canonical latest window metadata preserved, got last row id %d", got)
	}
}

func TestRuntimeApplyGridViewportPageReplacesCanonicalLatestWithShorterLiveTailOwnershipPage(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	scrollback := make([][]protocol.Cell, 0, materializedScrollbackRowLimit)
	for i := 0; i < materializedScrollbackRowLimit; i++ {
		scrollback = append(scrollback, protocolRowFromString(fmt.Sprintf("canon-%05d", i)))
	}
	live := snapshotWithLines("term-1", 12, 3, []string{"live0", "live1"})
	live.Scrollback = protocol.CompactRowsFromCells(scrollback)
	live.ScrollbackOffset = 0
	live.ScrollbackTotal = 12000
	live.ScrollbackLogicalTotal = 12000
	live.ScrollbackLoadedRows = 12000
	live.HistoryGeneration = 10
	live.ScrollbackFirstRowID = 0
	live.ScrollbackLastRowID = 11999
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 12000); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	page := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 12, Rows: 3},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("tail0"), protocolRowFromString("tail1")}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        4,
		ScrollbackLogicalTotal: 4,
		ScrollbackHasMore:      true,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackRowKinds:     []string{"", ""},
		ScrollbackWrapped:      []bool{false, false},
		ScrollbackOwnership:    []string{protocol.RowOwnershipLiveTailLive, protocol.RowOwnershipLiveTailLive},
		ScrollbackTimestamps:   []time.Time{time.Now(), time.Now()},
	}

	if !rt.ApplyGridViewportPage("term-1", page, 0) {
		t.Fatal("expected shorter live-tail ownership latest page to replace canonical latest window")
	}

	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected runtime snapshot, got %#v", terminal)
	}
	got := terminal.Snapshot
	if loaded := snapshotScrollbackLoadedDepth(got); loaded != 0 {
		t.Fatalf("expected replaced latest snapshot committed depth 0, got %d", loaded)
	}
	if got.ScrollbackLoadedRows != 0 || got.HistoryGeneration != 0 {
		t.Fatalf("expected replace semantics to drop canonical metadata, got loaded=%d generation=%d", got.ScrollbackLoadedRows, got.HistoryGeneration)
	}
	if got.ScrollbackFirstRowID != 0 || got.ScrollbackLastRowID != 0 {
		t.Fatalf("expected replace semantics to clear canonical row window, got %d..%d", got.ScrollbackFirstRowID, got.ScrollbackLastRowID)
	}
	if got.ScrollbackOffset != 0 {
		t.Fatalf("expected row window offset 0 after replace, got %d", got.ScrollbackOffset)
	}
	if got.ScrollbackTotal != 4 || got.ScrollbackLogicalTotal != 4 || !got.ScrollbackHasMore {
		t.Fatalf("expected incoming live-tail ownership metadata preserved, got total=%d logical=%d hasMore=%v", got.ScrollbackTotal, got.ScrollbackLogicalTotal, got.ScrollbackHasMore)
	}
	if len(got.Scrollback) != 2 || compactRowText(got.Scrollback[0]) != "tail0" || compactRowText(got.Scrollback[1]) != "tail1" {
		t.Fatalf("expected incoming live-tail rows to replace canonical materialization, got %#v", got.Scrollback)
	}
	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected live-tail ownership latest replace to reset known committed depth to 0, got %d", got)
	}
	if !protocol.HasOnlyLiveTailLiveOwnership(got.ScrollbackOwnership, len(got.Scrollback)) {
		t.Fatalf("expected live-tail ownership on replaced latest rows, got %#v", got.ScrollbackOwnership)
	}
}

func TestRuntimeApplyGridViewportPageLiveTailOnlyLatestReplaceKeepsZeroCommittedDepthAfterRefreshFromVTerm(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	scrollback := make([][]protocol.Cell, 0, materializedScrollbackRowLimit)
	for i := 0; i < materializedScrollbackRowLimit; i++ {
		scrollback = append(scrollback, protocolRowFromString(fmt.Sprintf("canon-%05d", i)))
	}
	live := snapshotWithLines("term-1", 12, 3, []string{"live0", "live1"})
	live.Scrollback = protocol.CompactRowsFromCells(scrollback)
	live.ScrollbackOffset = 0
	live.ScrollbackTotal = 12000
	live.ScrollbackLogicalTotal = 12000
	live.ScrollbackLoadedRows = 12000
	live.HistoryGeneration = 10
	live.ScrollbackFirstRowID = 0
	live.ScrollbackLastRowID = 11999
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 12000); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	page := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 12, Rows: 3},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("tail0"), protocolRowFromString("tail1")}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        4,
		ScrollbackLogicalTotal: 4,
		ScrollbackHasMore:      true,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackRowKinds:     []string{"", ""},
		ScrollbackWrapped:      []bool{false, false},
		ScrollbackOwnership:    []string{protocol.RowOwnershipLiveTailLive, protocol.RowOwnershipLiveTailLive},
		ScrollbackTimestamps:   []time.Time{time.Now(), time.Now()},
	}

	if !rt.ApplyGridViewportPage("term-1", page, 0) {
		t.Fatal("expected live-tail-only latest page to apply")
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected terminal with vterm after latest replace, got %#v", terminal)
	}
	if got := len(terminal.VTerm.ScrollbackContent()); got != 2 {
		t.Fatalf("expected latest replace to sync vterm live-tail rows, got %d", got)
	}
	terminal.PreferSnapshot = false
	if !rt.RefreshSnapshotFromVTerm("term-1") {
		t.Fatal("expected refresh from vterm")
	}

	terminal = rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected refreshed runtime snapshot, got %#v", terminal)
	}
	if got := snapshotScrollbackLoadedDepth(terminal.Snapshot); got != 0 {
		t.Fatalf("expected refresh to keep committed loaded depth 0, got %d", got)
	}
	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected refresh to keep known committed depth at 0, got %d", got)
	}
	if !protocol.HasOnlyLiveTailLiveOwnership(terminal.Snapshot.ScrollbackOwnership, len(terminal.Snapshot.Scrollback)) {
		t.Fatalf("expected refresh to preserve live-tail ownership, got %#v", terminal.Snapshot.ScrollbackOwnership)
	}
	if got, want := len(terminal.Snapshot.Scrollback), 2; got != want {
		t.Fatalf("expected refresh to keep %d live-tail rows, got %d", want, got)
	}
	if got := compactRowText(terminal.Snapshot.Scrollback[0]); got != "tail0" {
		t.Fatalf("expected refresh to preserve latest live-tail rows, got first row %q", got)
	}
	if got := compactRowText(terminal.Snapshot.Scrollback[1]); got != "tail1" {
		t.Fatalf("expected refresh to preserve latest live-tail rows, got second row %q", got)
	}
	if got, want := terminal.Snapshot.ScrollbackTotal, 4; got != want {
		t.Fatalf("expected refresh to preserve authoritative total rows %d, got %d", want, got)
	}
	if got, want := terminal.Snapshot.ScrollbackLogicalTotal, 4; got != want {
		t.Fatalf("expected refresh to preserve authoritative logical rows %d, got %d", want, got)
	}
	if !terminal.Snapshot.ScrollbackHasMore {
		t.Fatal("expected refresh to preserve authoritative has-more flag")
	}
}

func TestRuntimeApplyGridViewportPageCanonicalLatestReplaceUsesIncomingMetadata(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 12, 3, []string{"live0", "live1"})
	live.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromString("canon100"),
		protocolRowFromString("canon101"),
	})
	live.ScrollbackOffset = 0
	live.ScrollbackTotal = 12000
	live.ScrollbackLogicalTotal = 12000
	live.ScrollbackLoadedRows = 12000
	live.HistoryGeneration = 10
	live.ScrollbackFirstRowID = 100
	live.ScrollbackLastRowID = 11999
	markSnapshotScrollbackPersisted(live)
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 12000); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	page := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 12, Rows: 3},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("canon900"), protocolRowFromString("canon901"), protocolRowFromString("canon902")}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        13003,
		ScrollbackLogicalTotal: 13003,
		ScrollbackHasMore:      true,
		ScrollbackLoadedRows:   3,
		HistoryGeneration:      77,
		ScrollbackFirstRowID:   900,
		ScrollbackLastRowID:    902,
		ScrollbackRowKinds:     []string{"", "", ""},
		ScrollbackWrapped:      []bool{false, false, false},
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipPersisted, 3),
		ScrollbackTimestamps:   []time.Time{time.Now(), time.Now(), time.Now()},
	}

	if !rt.ApplyGridViewportPage("term-1", page, 0) {
		t.Fatal("expected canonical latest page to replace current latest window")
	}

	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected runtime snapshot, got %#v", terminal)
	}
	got := terminal.Snapshot
	if got.HistoryGeneration != 77 || got.ScrollbackLoadedRows != 3 {
		t.Fatalf("expected replace semantics to use incoming canonical metadata, got generation=%d loaded=%d", got.HistoryGeneration, got.ScrollbackLoadedRows)
	}
	if got.ScrollbackFirstRowID != 900 || got.ScrollbackLastRowID != 902 {
		t.Fatalf("expected replace semantics to use incoming canonical row window, got %d..%d", got.ScrollbackFirstRowID, got.ScrollbackLastRowID)
	}
	if got.ScrollbackTotal != 13003 || got.ScrollbackLogicalTotal != 13003 || !got.ScrollbackHasMore {
		t.Fatalf("expected replace semantics to use incoming totals/has-more, got total=%d logical=%d hasMore=%v", got.ScrollbackTotal, got.ScrollbackLogicalTotal, got.ScrollbackHasMore)
	}
	if got.ScrollbackOffset != 0 {
		t.Fatalf("expected replace semantics to use incoming row window offset, got %d", got.ScrollbackOffset)
	}
	if got, want := terminal.CommittedLoadedDepth, 3; got != want {
		t.Fatalf("expected canonical latest replace to adopt incoming committed depth %d, got %d", want, got)
	}
	if len(got.Scrollback) != 3 || compactRowText(got.Scrollback[0]) != "canon900" || compactRowText(got.Scrollback[2]) != "canon902" {
		t.Fatalf("expected replace semantics to use incoming materialized rows, got %#v", got.Scrollback)
	}
}

func TestRuntimeLoadSnapshotLiveTailOnlyLatestDoesNotPromoteCommittedLoadedLimit(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	liveTailOnly := snapshotWithLines("term-1", 6, 3, []string{"live"})
	liveTailOnly.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromString("tail0"),
		protocolRowFromString("tail1"),
	})
	liveTailOnly.ScrollbackTotal = 2
	liveTailOnly.ScrollbackLogicalTotal = 2
	markSnapshotScrollbackOwnership(liveTailOnly, protocol.RowOwnershipLiveTailLive)
	client.snapshotByTerminal["term-1"] = liveTailOnly

	rt := New(client)
	snapshot, err := rt.LoadSnapshot(ctx, "term-1", 0, 0)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal")
	}
	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected live-tail-only latest snapshot not to promote committed loaded limit, got %d", got)
	}
	if terminal.VTerm == nil {
		t.Fatal("expected vterm")
	}
	if got := len(terminal.VTerm.ScrollbackContent()); got != 2 {
		t.Fatalf("expected live-tail rows to still materialize into vterm scrollback, got %d", got)
	}
}

func TestRuntimeLoadSnapshotLiveTailOwnershipLatestReplacesKnownCommittedDepth(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	canonical := snapshotWithLines("term-1", 12, 3, []string{"live0", "live1"})
	canonicalRows := make([][]protocol.Cell, 0, materializedScrollbackRowLimit)
	for i := 0; i < materializedScrollbackRowLimit; i++ {
		canonicalRows = append(canonicalRows, protocolRowFromString(fmt.Sprintf("canon-%05d", i)))
	}
	canonical.Scrollback = protocol.CompactRowsFromCells(canonicalRows)
	canonical.ScrollbackOffset = 0
	canonical.ScrollbackTotal = 12000
	canonical.ScrollbackLogicalTotal = 12000
	canonical.ScrollbackLoadedRows = 12000
	canonical.HistoryGeneration = 10
	canonical.ScrollbackFirstRowID = 0
	canonical.ScrollbackLastRowID = 11999
	markSnapshotScrollbackPersisted(canonical)
	client.snapshotByTerminal["term-1"] = canonical

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 12000); err != nil {
		t.Fatalf("load canonical snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal")
	}
	if got := terminal.CommittedLoadedDepth; got != 12000 {
		t.Fatalf("expected canonical latest to seed committed depth 12000, got %d", got)
	}

	liveTailOnly := snapshotWithLines("term-1", 12, 3, []string{"live0", "live1"})
	liveTailOnly.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromString("tail0"),
		protocolRowFromString("tail1"),
	})
	liveTailOnly.ScrollbackOffset = 0
	liveTailOnly.ScrollbackTotal = 4
	liveTailOnly.ScrollbackLogicalTotal = 4
	liveTailOnly.ScrollbackHasMore = true
	markSnapshotScrollbackOwnership(liveTailOnly, protocol.RowOwnershipLiveTailLive)
	client.snapshotByTerminal["term-1"] = liveTailOnly

	snapshot, err := rt.LoadSnapshot(ctx, "term-1", 0, 0)
	if err != nil {
		t.Fatalf("load live-tail ownership latest snapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	terminal = rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected cached runtime snapshot, got %#v", terminal)
	}
	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected live-tail ownership latest to replace known committed depth with 0, got %d", got)
	}
	if got := snapshotScrollbackLoadedDepth(terminal.Snapshot); got != 0 {
		t.Fatalf("expected live-tail ownership latest snapshot committed depth 0, got %d", got)
	}
	if !protocol.HasOnlyLiveTailLiveOwnership(terminal.Snapshot.ScrollbackOwnership, len(terminal.Snapshot.Scrollback)) {
		t.Fatalf("expected live-tail ownership on cached snapshot, got %#v", terminal.Snapshot.ScrollbackOwnership)
	}
	if got := len(terminal.Snapshot.Scrollback); got != 2 {
		t.Fatalf("expected live-tail-only latest materialization to keep 2 rows, got %d", got)
	}
}

func TestRuntimeRefreshSnapshotFromVTermKeepsLiveTailOwnershipCommittedDepth(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	liveTailOnly := snapshotWithLines("term-1", 6, 3, []string{"live"})
	liveTailOnly.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromString("tail0"),
		protocolRowFromString("tail1"),
	})
	liveTailOnly.ScrollbackTotal = 2
	liveTailOnly.ScrollbackLogicalTotal = 2
	markSnapshotScrollbackOwnership(liveTailOnly, protocol.RowOwnershipLiveTailLive)
	client.snapshotByTerminal["term-1"] = liveTailOnly

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected terminal with vterm, got %#v", terminal)
	}
	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected live-tail ownership latest load to keep committed depth 0, got %d", got)
	}

	if !rt.RefreshSnapshotFromVTerm("term-1") {
		t.Fatal("expected refresh from vterm")
	}

	terminal = rt.Registry().Get("term-1")
	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected live-tail ownership latest refresh to keep committed depth 0, got %d", got)
	}
	if terminal.Snapshot == nil {
		t.Fatal("expected refreshed snapshot")
	}
	if !protocol.HasOnlyLiveTailLiveOwnership(terminal.Snapshot.ScrollbackOwnership, len(terminal.Snapshot.Scrollback)) {
		t.Fatalf("expected refreshed snapshot to preserve live-tail ownership, got %#v", terminal.Snapshot.ScrollbackOwnership)
	}
	if got, want := len(terminal.Snapshot.Scrollback), 2; got != want {
		t.Fatalf("expected refreshed snapshot to keep %d live-tail rows, got %d", want, got)
	}
}

func TestRuntimeBumpSurfaceVersionKeepsLiveTailOwnershipCommittedDepth(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	liveTailOnly := snapshotWithLines("term-1", 6, 3, []string{"live"})
	liveTailOnly.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromString("tail0"),
		protocolRowFromString("tail1"),
	})
	liveTailOnly.ScrollbackTotal = 2
	liveTailOnly.ScrollbackLogicalTotal = 2
	markSnapshotScrollbackOwnership(liveTailOnly, protocol.RowOwnershipLiveTailLive)
	client.snapshotByTerminal["term-1"] = liveTailOnly

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected terminal with vterm, got %#v", terminal)
	}
	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected live-tail ownership latest load to keep committed depth 0, got %d", got)
	}

	rt.bumpSurfaceVersion(terminal)

	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected surface sync to keep live-tail ownership committed depth 0, got %d", got)
	}
}

func TestRuntimeAttachTerminalPreservesLiveTailOwnershipAcrossReattach(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 7, Mode: "collaborator"}
	liveTailOnly := snapshotWithLines("term-1", 6, 3, []string{"live"})
	liveTailOnly.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromString("tail0"),
		protocolRowFromString("tail1"),
	})
	liveTailOnly.ScrollbackTotal = 4
	liveTailOnly.ScrollbackLogicalTotal = 4
	liveTailOnly.ScrollbackHasMore = true
	markSnapshotScrollbackOwnership(liveTailOnly, protocol.RowOwnershipLiveTailLive)
	client.snapshotByTerminal["term-1"] = liveTailOnly

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	checkLiveTailOwnership := func(stage string) {
		t.Helper()
		terminal := rt.Registry().Get("term-1")
		if terminal == nil {
			t.Fatalf("%s: expected terminal", stage)
		}
		if got := terminal.CommittedLoadedDepth; got != 0 {
			t.Fatalf("%s: expected committed loaded depth 0, got %d", stage, got)
		}
		if terminal.Snapshot == nil || !protocol.HasOnlyLiveTailLiveOwnership(terminal.Snapshot.ScrollbackOwnership, len(terminal.Snapshot.Scrollback)) {
			t.Fatalf("%s: expected live-tail ownership to survive, got %#v", stage, terminal.Snapshot)
		}
		if !rt.RefreshSnapshotFromVTerm("term-1") {
			t.Fatalf("%s: expected vterm refresh", stage)
		}
		terminal = rt.Registry().Get("term-1")
		if terminal.Snapshot == nil {
			t.Fatalf("%s: expected refreshed snapshot", stage)
		}
		if !protocol.HasOnlyLiveTailLiveOwnership(terminal.Snapshot.ScrollbackOwnership, len(terminal.Snapshot.Scrollback)) {
			t.Fatalf("%s: expected refreshed snapshot to keep live-tail ownership, got %#v", stage, terminal.Snapshot)
		}
		if got, want := terminal.Snapshot.ScrollbackLoadedRows, 0; got != want {
			t.Fatalf("%s: expected refreshed committed loaded rows %d, got %d", stage, want, got)
		}
		if got, want := terminal.Snapshot.ScrollbackTotal, 4; got != want {
			t.Fatalf("%s: expected refreshed total rows %d, got %d", stage, want, got)
		}
		if got, want := terminal.Snapshot.ScrollbackLogicalTotal, 4; got != want {
			t.Fatalf("%s: expected refreshed logical rows %d, got %d", stage, want, got)
		}
		if !terminal.Snapshot.ScrollbackHasMore {
			t.Fatalf("%s: expected refreshed snapshot has-more flag", stage)
		}
	}

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	checkLiveTailOwnership("after first attach")

	client.attachResult = &protocol.AttachResult{Channel: 8, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("reattach: %v", err)
	}
	checkLiveTailOwnership("after reattach")
}

func TestRuntimeBumpSurfaceVersionDoesNotPromoteLocalVTermOnlyScrollbackToCommittedDepth(t *testing.T) {
	rt := New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.VTerm = localvterm.New(8, 3, 16, nil)
	if terminal.VTerm == nil {
		t.Fatal("expected vterm")
	}
	if _, err := terminal.VTerm.Write([]byte("line0\r\nline1\r\nline2\r\nline3")); err != nil {
		t.Fatalf("write local fallback content: %v", err)
	}

	rt.bumpSurfaceVersion(terminal)

	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected local VTerm-only scrollback not to promote committed depth, got %d", got)
	}
}

func TestRuntimeApplyGridViewportPageRejectsStaleGeometry(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 80, 24, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromString("new0"),
	})
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	page := &protocol.Snapshot{
		TerminalID:         "term-1",
		Size:               protocol.Size{Cols: 40, Rows: 24},
		Scrollback:         protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("old0")}),
		ScrollbackOffset:   1,
		ScrollbackTotal:    2,
		ScrollbackHasMore:  false,
		ScrollbackRowKinds: []string{""},
		ScrollbackWrapped:  []bool{false},
	}
	if rt.ApplyGridViewportPage("term-1", page, 1) {
		t.Fatal("expected stale-geometry history page to be rejected")
	}
	got := rt.Registry().Get("term-1").Snapshot
	if len(got.Scrollback) != 1 || compactRowText(got.Scrollback[0]) != "new0" {
		t.Fatalf("expected live snapshot history to remain unchanged, got %#v", got.Scrollback)
	}
}

func TestRuntimeApplyGridViewportPageRejectsStaleHistoryGeneration(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 80, 24, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("new0")})
	live.ScrollbackLoadedRows = 1
	live.HistoryGeneration = 10
	live.ScrollbackFirstRowID = 100
	live.ScrollbackLastRowID = 100
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	page := &protocol.Snapshot{
		TerminalID:           "term-1",
		Size:                 protocol.Size{Cols: 80, Rows: 24},
		Scrollback:           protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("old0")}),
		ScrollbackOffset:     1,
		ScrollbackTotal:      2,
		ScrollbackHasMore:    false,
		ScrollbackLoadedRows: 2,
		HistoryGeneration:    11,
		ScrollbackFirstRowID: 99,
		ScrollbackLastRowID:  99,
	}
	if rt.ApplyGridViewportPage("term-1", page, 1) {
		t.Fatal("expected stale-generation history page to be rejected")
	}
	got := rt.Registry().Get("term-1").Snapshot
	if len(got.Scrollback) != 1 || compactRowText(got.Scrollback[0]) != "new0" {
		t.Fatalf("expected live snapshot history to remain unchanged, got %#v", got.Scrollback)
	}
}

func TestRuntimeApplyGridViewportPageRejectsNonAdjacentRowIDs(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 80, 24, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("new0")})
	live.ScrollbackLoadedRows = 1
	live.HistoryGeneration = 10
	live.ScrollbackFirstRowID = 100
	live.ScrollbackLastRowID = 100
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	page := &protocol.Snapshot{
		TerminalID:           "term-1",
		Size:                 protocol.Size{Cols: 80, Rows: 24},
		Scrollback:           protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("old0")}),
		ScrollbackOffset:     1,
		ScrollbackTotal:      2,
		ScrollbackHasMore:    false,
		ScrollbackLoadedRows: 2,
		HistoryGeneration:    10,
		ScrollbackFirstRowID: 98,
		ScrollbackLastRowID:  98,
	}
	if rt.ApplyGridViewportPage("term-1", page, 1) {
		t.Fatal("expected non-adjacent history page to be rejected")
	}
	got := rt.Registry().Get("term-1").Snapshot
	if len(got.Scrollback) != 1 || compactRowText(got.Scrollback[0]) != "new0" {
		t.Fatalf("expected live snapshot history to remain unchanged, got %#v", got.Scrollback)
	}
}

func TestRuntimeApplyGridViewportPageRejectsOlderPageWhenCurrentHasNoCanonicalHistoryWindow(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 80, 24, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("canon100")})
	live.ScrollbackLoadedRows = 1
	live.HistoryGeneration = 10
	live.ScrollbackFirstRowID = 100
	live.ScrollbackLastRowID = 100
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 1); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	latestNoCanonical := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 80, Rows: 24},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("tail0"), protocolRowFromString("tail1")}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 2,
		ScrollbackHasMore:      false,
		ScrollbackLoadedRows:   0,
		HistoryGeneration:      0,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackRowKinds:     []string{"", ""},
		ScrollbackWrapped:      []bool{false, false},
	}
	if !rt.ApplyGridViewportPage("term-1", latestNoCanonical, 0) {
		t.Fatal("expected no-canonical latest page to apply")
	}

	older := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 80, Rows: 24},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("canon099")}),
		ScrollbackOffset:       1,
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 2,
		ScrollbackHasMore:      false,
		ScrollbackLoadedRows:   2,
		HistoryGeneration:      10,
		ScrollbackFirstRowID:   99,
		ScrollbackLastRowID:    99,
		ScrollbackRowKinds:     []string{""},
		ScrollbackWrapped:      []bool{false},
	}
	if rt.ApplyGridViewportPage("term-1", older, 1) {
		t.Fatal("expected older page to be rejected once current snapshot has no canonical history window")
	}

	got := rt.Registry().Get("term-1").Snapshot
	if got == nil {
		t.Fatal("expected runtime snapshot")
	}
	if got.HistoryGeneration != 0 || got.ScrollbackFirstRowID != 0 || got.ScrollbackLastRowID != 0 {
		t.Fatalf("expected no-canonical latest snapshot to remain unchanged, got generation=%d window=%d..%d", got.HistoryGeneration, got.ScrollbackFirstRowID, got.ScrollbackLastRowID)
	}
	if len(got.Scrollback) != 2 || compactRowText(got.Scrollback[0]) != "tail0" || compactRowText(got.Scrollback[1]) != "tail1" {
		t.Fatalf("expected live-tail-only latest page to remain intact, got %#v", got.Scrollback)
	}
}

func TestRuntimeApplyGridViewportPageRejectsOlderPageWithoutCanonicalHistoryWindow(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 80, 24, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("canon001")})
	live.ScrollbackLoadedRows = 1
	live.HistoryGeneration = 10
	live.ScrollbackFirstRowID = 1
	live.ScrollbackLastRowID = 1
	markSnapshotScrollbackPersisted(live)
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 1); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	olderNoCanonical := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 80, Rows: 24},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("ghost0")}),
		ScrollbackOffset:       1,
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 2,
		ScrollbackHasMore:      false,
		ScrollbackLoadedRows:   0,
		HistoryGeneration:      0,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackRowKinds:     []string{""},
		ScrollbackWrapped:      []bool{false},
		ScrollbackOwnership:    []string{protocol.RowOwnershipPersisted},
	}
	if rt.ApplyGridViewportPage("term-1", olderNoCanonical, 1) {
		t.Fatal("expected older page without canonical history window to be rejected")
	}

	got := rt.Registry().Get("term-1").Snapshot
	if got == nil {
		t.Fatal("expected runtime snapshot")
	}
	if got.HistoryGeneration != 10 || got.ScrollbackFirstRowID != 1 || got.ScrollbackLastRowID != 1 {
		t.Fatalf("expected canonical current snapshot to remain unchanged, got generation=%d window=%d..%d", got.HistoryGeneration, got.ScrollbackFirstRowID, got.ScrollbackLastRowID)
	}
	if len(got.Scrollback) != 1 || compactRowText(got.Scrollback[0]) != "canon001" {
		t.Fatalf("expected canonical current page to remain intact, got %#v", got.Scrollback)
	}
}

func TestRuntimeApplyGridViewportPageRejectsOffsetFromStoredLoadedLimitWhenSnapshotDepthDiffers(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 80, 24, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("canon010")})
	live.ScrollbackOffset = 10
	live.ScrollbackLoadedRows = 11
	live.HistoryGeneration = 10
	live.ScrollbackFirstRowID = 10
	live.ScrollbackLastRowID = 10
	live.ScrollbackOwnership = []string{protocol.RowOwnershipPersisted}
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 1); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatal("expected runtime snapshot")
	}
	terminal.CommittedLoadedDepth = 100

	older := &protocol.Snapshot{
		TerminalID:           "term-1",
		Size:                 protocol.Size{Cols: 80, Rows: 24},
		Scrollback:           protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("canon009")}),
		ScrollbackOffset:     100,
		ScrollbackTotal:      101,
		ScrollbackHasMore:    false,
		ScrollbackLoadedRows: 101,
		HistoryGeneration:    10,
		ScrollbackFirstRowID: 9,
		ScrollbackLastRowID:  9,
		ScrollbackOwnership:  []string{protocol.RowOwnershipPersisted},
	}
	if rt.ApplyGridViewportPage("term-1", older, 100) {
		t.Fatal("expected older page keyed only by stored loaded limit to be rejected")
	}
	if got := terminal.CommittedLoadedDepth; got != 100 {
		t.Fatalf("expected rejected stale page not to rewrite stored loaded limit, got %d", got)
	}
	if got := compactRowText(terminal.Snapshot.Scrollback[0]); got != "canon010" {
		t.Fatalf("expected current ownership window to remain unchanged, got %q", got)
	}
}

func TestRuntimeApplyGridViewportPageAcceptsCanonicalOlderPageWithRowIDZero(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	live := snapshotWithLines("term-1", 80, 24, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("canon001")})
	live.ScrollbackLoadedRows = 1
	live.HistoryGeneration = 10
	live.ScrollbackFirstRowID = 1
	live.ScrollbackLastRowID = 1
	markSnapshotScrollbackPersisted(live)
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 1); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	older := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 80, Rows: 24},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("canon000")}),
		ScrollbackOffset:       1,
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 2,
		ScrollbackHasMore:      false,
		ScrollbackLoadedRows:   2,
		HistoryGeneration:      10,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackRowKinds:     []string{""},
		ScrollbackWrapped:      []bool{false},
		ScrollbackOwnership:    []string{protocol.RowOwnershipPersisted},
	}
	if !rt.ApplyGridViewportPage("term-1", older, 1) {
		t.Fatal("expected canonical older page ending at row id 0 to apply")
	}

	got := rt.Registry().Get("term-1").Snapshot
	if got == nil {
		t.Fatal("expected runtime snapshot")
	}
	if got.HistoryGeneration != 10 || got.ScrollbackFirstRowID != 0 || got.ScrollbackLastRowID != 1 {
		t.Fatalf("expected merged canonical window 0..1, got generation=%d window=%d..%d", got.HistoryGeneration, got.ScrollbackFirstRowID, got.ScrollbackLastRowID)
	}
	if len(got.Scrollback) != 2 || compactRowText(got.Scrollback[0]) != "canon000" || compactRowText(got.Scrollback[1]) != "canon001" {
		t.Fatalf("expected canonical older page to prepend cleanly, got %#v", got.Scrollback)
	}
}

func TestRuntimeApplyGridViewportPageKeepsBoundedWindow(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	allRows := make([][]protocol.Cell, 12500)
	for i := range allRows {
		allRows[i] = protocolRowFromString(fmt.Sprintf("hist-%05d", i))
	}
	live := snapshotWithLines("term-1", 12, 3, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells(allRows[500:])
	live.ScrollbackTotal = len(allRows)
	live.ScrollbackLogicalTotal = len(allRows)
	live.ScrollbackLoadedRows = len(allRows) - 500
	live.HistoryGeneration = 1
	live.ScrollbackFirstRowID = 500
	live.ScrollbackLastRowID = 12499
	markSnapshotScrollbackPersisted(live)
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 12000); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	page := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 12, Rows: 3},
		Scrollback:             protocol.CompactRowsFromCells(allRows[:500]),
		ScrollbackOffset:       12000,
		ScrollbackTotal:        12500,
		ScrollbackLogicalTotal: 12500,
		ScrollbackHasMore:      false,
		ScrollbackLoadedRows:   12500,
		HistoryGeneration:      1,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    499,
		ScrollbackRowKinds:     make([]string, 500),
		ScrollbackWrapped:      make([]bool, 500),
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipPersisted, 500),
	}
	if !rt.ApplyGridViewportPage("term-1", page, 12000) {
		t.Fatal("expected older page to apply")
	}
	terminal := rt.Registry().Get("term-1")
	got := terminal.Snapshot
	if got == nil {
		t.Fatal("expected runtime snapshot")
	}
	if gotRows := len(got.Scrollback); gotRows != materializedScrollbackRowLimit {
		t.Fatalf("expected bounded materialized rows, got %d want %d", gotRows, materializedScrollbackRowLimit)
	}
	if got.ScrollbackOffset != 500 {
		t.Fatalf("expected newest rows to be trimmed from the materialized window, offset=%d", got.ScrollbackOffset)
	}
	if got := compactRowText(got.Scrollback[0]); got != "hist-00000" {
		t.Fatalf("expected oldest loaded row at window start, got %q", got)
	}
	if terminal.CommittedLoadedDepth != 12500 {
		t.Fatalf("expected loaded depth to keep full logical progress, got %d", terminal.CommittedLoadedDepth)
	}
	if firstRowID, want := got.ScrollbackFirstRowID, uint64(0); firstRowID != want {
		t.Fatalf("expected bounded materialization to keep loaded committed window first row id, got %d want %d", firstRowID, want)
	}
	if lastRowID, want := got.ScrollbackLastRowID, uint64(12499); lastRowID != want {
		t.Fatalf("expected bounded materialization to keep loaded committed window last row id, got %d want %d", lastRowID, want)
	}
}

func TestRuntimeApplyGridViewportPageAcceptsOffsetBeyondMaterializedWindow(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	allRows := make([][]protocol.Cell, 13000)
	for i := range allRows {
		allRows[i] = protocolRowFromString(fmt.Sprintf("hist-%05d", i))
	}
	live := snapshotWithLines("term-1", 12, 3, []string{"live"})
	live.Scrollback = protocol.CompactRowsFromCells(allRows[1000:])
	live.ScrollbackTotal = len(allRows)
	live.ScrollbackLogicalTotal = len(allRows)
	live.ScrollbackLoadedRows = 12000
	live.HistoryGeneration = 1
	live.ScrollbackFirstRowID = 1000
	live.ScrollbackLastRowID = 12999
	markSnapshotScrollbackPersisted(live)
	client.snapshotByTerminal["term-1"] = live

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 12000); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	page1 := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 12, Rows: 3},
		Scrollback:             protocol.CompactRowsFromCells(allRows[500:1000]),
		ScrollbackOffset:       12000,
		ScrollbackTotal:        13000,
		ScrollbackLogicalTotal: 13000,
		ScrollbackHasMore:      true,
		ScrollbackLoadedRows:   12500,
		HistoryGeneration:      1,
		ScrollbackFirstRowID:   500,
		ScrollbackLastRowID:    999,
		ScrollbackRowKinds:     make([]string, 500),
		ScrollbackWrapped:      make([]bool, 500),
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipPersisted, 500),
	}
	if !rt.ApplyGridViewportPage("term-1", page1, 12000) {
		t.Fatal("expected first older page to apply")
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatal("expected runtime snapshot")
	}
	if got := len(terminal.Snapshot.Scrollback); got != materializedScrollbackRowLimit {
		t.Fatalf("expected bounded materialized window after first page, got %d", got)
	}

	page2 := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 12, Rows: 3},
		Scrollback:             protocol.CompactRowsFromCells(allRows[:500]),
		ScrollbackOffset:       12500,
		ScrollbackTotal:        13000,
		ScrollbackLogicalTotal: 13000,
		ScrollbackHasMore:      false,
		ScrollbackLoadedRows:   13000,
		HistoryGeneration:      terminal.Snapshot.HistoryGeneration,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    499,
		ScrollbackRowKinds:     make([]string, 500),
		ScrollbackWrapped:      make([]bool, 500),
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipPersisted, 500),
	}
	if !rt.ApplyGridViewportPage("term-1", page2, 12500) {
		t.Fatal("expected second older page beyond materialized window to apply")
	}
	if got, want := terminal.CommittedLoadedDepth, 13000; got != want {
		t.Fatalf("expected loaded depth 13000 after second page, got %d", got)
	}
	if got := compactRowText(terminal.Snapshot.Scrollback[0]); got != "hist-00000" {
		t.Fatalf("expected oldest page to reach window start, got %q", got)
	}
}

func TestRuntimeSnapshotWindowHelperKeepsCanonicalRowIDZeroSingleRowWindow(t *testing.T) {
	snapshot := &protocol.Snapshot{
		TerminalID:             "term-1",
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("row-00000")}),
		ScrollbackTotal:        1,
		ScrollbackLogicalTotal: 1,
		ScrollbackLoadedRows:   1,
		ScrollbackHasMore:      false,
		HistoryGeneration:      7,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackWrapped:      []bool{false},
		ScrollbackRowKinds:     []string{""},
	}

	window := testSnapshotWindow(snapshot, 0, 1)
	if window == nil {
		t.Fatal("expected window")
	}
	if got, want := window.ScrollbackLoadedRows, 1; got != want {
		t.Fatalf("expected loaded rows %d, got %d", want, got)
	}
	if got, want := window.HistoryGeneration, uint64(7); got != want {
		t.Fatalf("expected generation %d, got %d", want, got)
	}
	if got, want := window.ScrollbackFirstRowID, uint64(0); got != want {
		t.Fatalf("expected first row id %d, got %d", want, got)
	}
	if got, want := window.ScrollbackLastRowID, uint64(0); got != want {
		t.Fatalf("expected last row id %d, got %d", want, got)
	}
	if got := compactRowText(window.Scrollback[0]); got != "row-00000" {
		t.Fatalf("expected single canonical row to survive helper windowing, got %q", got)
	}
}

func TestRuntimeAttachTerminalDoesNotStartStreamBeforeSnapshotLoad(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 7, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}

	if got := client.subscriptionCount(7); got != 0 {
		t.Fatalf("expected attach to avoid starting stream before snapshot load, got %d subscriptions", got)
	}
}

func TestRuntimeLoadSnapshotDoesNotRaceWithAlternateScreenExit(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{
			Cells:             [][]protocol.Cell{{{Content: "v", Width: 1}, {Content: "i", Width: 1}}},
			IsAlternateScreen: true,
		},
		Cursor:    protocol.CursorState{Visible: false},
		Modes:     protocol.TerminalModes{AutoWrap: true, AlternateScreen: true, MouseTracking: true},
		Timestamp: time.Now(),
	}
	client.snapshotHook = func() {
		if client.subscriptionCount(9) != 0 {
			t.Fatalf("snapshot load should run before stream subscription")
		}
	}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, screenUpdateFrameForLines(t, 80, 24, "$ "))
	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "$ ")
	})

	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.VTerm == nil {
		t.Fatalf("expected terminal vterm after stream, got %#v", stored)
	}
	if !stored.VTerm.CursorState().Visible {
		t.Fatalf("expected streamed cursor show to win over older state, got %#v", stored.VTerm.CursorState())
	}
	if stored.VTerm.Modes().MouseTracking {
		t.Fatalf("expected streamed mouse disable to win over older state, got %#v", stored.VTerm.Modes())
	}
	if stored.VTerm.Modes().AlternateScreen || stored.VTerm.ScreenContent().IsAlternateScreen {
		t.Fatalf("expected alternate screen exit to win over older state, got modes=%#v alt=%v", stored.VTerm.Modes(), stored.VTerm.ScreenContent().IsAlternateScreen)
	}
}

func TestRuntimeStartStreamUpdatesSurfaceAndInvalidates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	var invalidateCount atomic.Int32
	rt := New(client, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal.VTerm == nil {
		t.Fatal("expected attach to initialize a vterm")
	}

	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}
	if got := client.streamReadyCount(9); got != 1 {
		t.Fatalf("expected initial stream ready, got %d", got)
	}

	client.sendFrame(9, screenUpdateFrameForLines(t, 80, 24, "hi"))

	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "hi")
	})
	if got := client.streamReadyCount(9); got != 1 {
		t.Fatalf("expected screen update ready to wait for render, got %d ready calls", got)
	}

	if invalidateCount.Load() == 0 {
		t.Fatal("expected stream refresh to invalidate rendering")
	}
	rt.MarkRenderedStreamUpdates(ctx)
	if got := client.streamReadyCount(9); got != 2 {
		t.Fatalf("expected rendered screen update ack, got %d ready calls", got)
	}
	if got := client.lastStreamReadySequence(9); got != 0 {
		t.Fatalf("expected direct fake stream frame to ack sequence 0, got %d", got)
	}
	if !vtermContains(rt.Registry().Get("term-1").VTerm, "hi") {
		t.Fatal("expected live surface to contain streamed output")
	}
	if rt.Registry().Get("term-1").SurfaceVersion == 0 {
		t.Fatal("expected surface version to advance after stream output")
	}
}

func TestRuntimeStreamReadyAcksRenderedScreenSequence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	rt := New(client)

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{
		Type:           wire.TypeScreenUpdate,
		Payload:        screenUpdatePayloadForLines(t, 80, 24, "seq-one"),
		ScreenSequence: 7,
	})

	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "seq-one")
	})
	rt.MarkRenderedStreamUpdates(ctx)
	if got := client.lastStreamReadySequence(9); got != 7 {
		t.Fatalf("expected rendered ack sequence 7, got %d", got)
	}
}

func TestRuntimeStreamReadyCanAckDuringSynchronousInvalidate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	var rt *Runtime
	rt = New(client, WithInvalidate(func() {
		if rt != nil {
			rt.MarkRenderedStreamUpdates(ctx)
		}
	}))

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{
		Type:           wire.TypeScreenUpdate,
		Payload:        screenUpdatePayloadForLines(t, 80, 24, "sync-render"),
		ScreenSequence: 13,
	})

	waitFor(t, func() bool {
		return client.lastStreamReadySequence(9) == 13
	})
	if !vtermContains(rt.Registry().Get("term-1").VTerm, "sync-render") {
		t.Fatal("expected streamed output to be applied")
	}
}

func TestRuntimeSyncLostAcksRecoveredScreenSequenceAfterRender(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"truth"})
	rt := New(client)

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{
		Type:           wire.TypeSyncLost,
		Payload:        wire.EncodeSyncLostPayload(128),
		ScreenSequence: 11,
	})

	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "truth")
	})
	if got := client.lastStreamReadySequence(9); got != 0 {
		t.Fatalf("expected sync-lost recovery ack to wait for render, got %d", got)
	}
	rt.MarkRenderedStreamUpdates(ctx)
	if got := client.lastStreamReadySequence(9); got != 11 {
		t.Fatalf("expected recovered sync-lost ack sequence 11, got %d", got)
	}
}

func TestSnapshotFromVTermPrefersRowViewsOverWholeContentCopies(t *testing.T) {
	vt := &countingSnapshotVTerm{VTerm: localvterm.New(8, 2, 16, nil)}
	if _, err := vt.Write([]byte("hello\r\nworld")); err != nil {
		t.Fatalf("seed vterm: %v", err)
	}

	snapshot := snapshotFromVTerm("term-1", vt)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := vt.screenContentCalls.Load(); got != 0 {
		t.Fatalf("expected snapshotFromVTerm to avoid ScreenContent copies, got %d calls", got)
	}
	if got := vt.scrollbackContentCalls.Load(); got != 0 {
		t.Fatalf("expected snapshotFromVTerm to avoid ScrollbackContent copies, got %d calls", got)
	}
	if !snapshotContains(snapshot, "hello") || !snapshotContains(snapshot, "world") {
		t.Fatalf("expected row-view snapshot content, got %#v", snapshot)
	}
}

func TestSnapshotFromVTermPreservesCanonicalTrailingBlankCells(t *testing.T) {
	vt := localvterm.New(12, 2, 16, nil)
	if _, err := vt.Write([]byte("████")); err != nil {
		t.Fatalf("seed QR-like row: %v", err)
	}

	snapshot := snapshotFromVTerm("term-1", vt)
	if snapshot == nil || len(snapshot.Screen.Cells) == 0 {
		t.Fatalf("expected snapshot screen rows, got %#v", snapshot)
	}
	if got := len(snapshot.Screen.Cells[0]); got != 12 {
		t.Fatalf("expected canonical-width screen row, got len=%d row=%#v", got, snapshot.Screen.Cells[0])
	}
	if got := rowTextRaw(snapshot.Screen.Cells[0]); got != "████        " {
		t.Fatalf("expected QR-like quiet-zone blanks to survive snapshot, got %q row=%#v", got, snapshot.Screen.Cells[0])
	}
}

func TestRuntimeScreenUpdateAlsoRefreshesLocalVTermSurface(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil {
		t.Fatal("expected terminal")
	}

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		FullReplace: true,
		Size:        protocol.Size{Cols: 6, Rows: 2},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}},
		},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})

	if terminal.PreferSnapshot {
		t.Fatalf("expected structured screen update to refresh local vterm surface, got %#v", terminal)
	}
	if terminal.SurfaceVersion == 0 {
		t.Fatalf("expected structured screen update to bump surface version, got %#v", terminal)
	}
	if terminal.VTerm == nil || !vtermContains(terminal.VTerm, "ok") {
		t.Fatalf("expected local vterm to receive structured screen update, got %#v", terminal.VTerm)
	}
	if terminal.Snapshot == nil || !snapshotContains(terminal.Snapshot, "ok") {
		t.Fatalf("expected snapshot to stay synchronized with structured update, got %#v", terminal.Snapshot)
	}
}

func TestRuntimeScrollbackChangeInvalidatesExhaustedSnapshotState(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 6, 2, []string{"live"})

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 500); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal")
	}
	if terminal.CommittedHistoryExhausted {
		t.Fatalf("expected initial snapshot without ownership not to infer exhausted history, got %#v", terminal)
	}

	payload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 6, Rows: 2},
		ScrollbackAppend: []protocol.ScrollbackRowAppend{{
			Cells: []protocol.Cell{
				{Content: "h", Width: 1},
				{Content: "i", Width: 1},
			},
			Timestamp: time.Now(),
		}},
		Ops: []protocol.ScreenOp{{
			Code:  protocol.ScreenOpWriteSpan,
			Row:   1,
			Col:   0,
			Cells: []protocol.Cell{{Content: "x", Width: 1}},
		}},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: payload})

	if terminal.CommittedHistoryExhausted {
		t.Fatalf("expected live scrollback change to clear exhausted state, got %#v", terminal)
	}
	if terminal.CommittedLoadedDepth != 0 {
		t.Fatalf("expected live-tail append not to advance committed loaded depth, got %#v", terminal)
	}
	if terminal.Snapshot == nil || !protocol.HasOnlyLiveTailLiveOwnership(terminal.Snapshot.ScrollbackOwnership, len(terminal.Snapshot.Scrollback)) {
		t.Fatalf("expected stream scrollback append to be live-tail owned in snapshot, got %#v", terminal.Snapshot)
	}
	if !rt.RefreshSnapshotFromVTerm("term-1") {
		t.Fatal("expected refresh from vterm after stream append")
	}
	if !protocol.HasOnlyLiveTailLiveOwnership(terminal.Snapshot.ScrollbackOwnership, len(terminal.Snapshot.Scrollback)) {
		t.Fatalf("expected stream append ownership to round trip through vterm projection, got %#v", terminal.Snapshot.ScrollbackOwnership)
	}
}

func TestRuntimeScrollOpcodeInvalidatesExhaustedSnapshotState(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 6, 2, []string{"line0", "line1"})

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 500); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal")
	}
	if terminal.CommittedHistoryExhausted {
		t.Fatalf("expected initial snapshot without ownership not to infer exhausted history, got %#v", terminal)
	}

	payload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 6, Rows: 2},
		Ops: []protocol.ScreenOp{
			{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 0, Width: 6, Height: 2}, Dy: -1},
			{Code: protocol.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: []protocol.Cell{{Content: "n", Width: 1}, {Content: "e", Width: 1}, {Content: "w", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 1, Col: 3, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: payload})

	if terminal.CommittedHistoryExhausted {
		t.Fatalf("expected screen scroll to clear exhausted state, got %#v", terminal)
	}
}

func TestRuntimeScreenUpdateUsesIncrementalVTermApplyWhenSupported(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	var counted *incrementalCountingVTerm
	rt := New(client, WithVTermFactory(func(channel uint16) VTermLike {
		counted = &incrementalCountingVTerm{VTerm: localvterm.New(80, 24, 10000, nil)}
		return counted
	}))
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil {
		t.Fatal("expected terminal")
	}
	if counted == nil {
		t.Fatal("expected incremental counting vterm")
	}

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 80, Rows: 24},
		Ops: []protocol.ScreenOp{{
			Code: protocol.ScreenOpWriteSpan,
			Row:  0,
			Col:  0,
			Cells: []protocol.Cell{
				{Content: "o", Width: 1},
				{Content: "k", Width: 1},
			},
			Timestamp: time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC),
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})

	if got := counted.partialCalls.Load(); got == 0 {
		t.Fatalf("expected incremental apply path, got partialCalls=%d", got)
	}
	if got := counted.fullLoadCalls.Load(); got != 0 {
		t.Fatalf("expected incremental apply to avoid full snapshot reload, got fullLoadCalls=%d", got)
	}
	if terminal.VTerm == nil || !vtermContains(terminal.VTerm, "ok") {
		t.Fatalf("expected local vterm to receive incremental structured update, got %#v", terminal.VTerm)
	}
}

func TestRuntimeScreenUpdatePreservesStyledBlankSpan(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil {
		t.Fatal("expected terminal")
	}

	const bg = "#222222"
	cells := make([]protocol.Cell, 12)
	for i := range cells {
		cells[i] = protocol.Cell{Content: " ", Width: 1, Style: protocol.CellStyle{BG: bg}}
	}
	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 12, Rows: 2},
		Ops: []protocol.ScreenOp{{
			Code:  protocol.ScreenOpWriteSpan,
			Row:   0,
			Col:   0,
			Cells: cells,
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})

	if got := terminal.Snapshot.Screen.Cells[0][11].Style.BG; got != bg {
		t.Fatalf("expected snapshot styled blank bg %q, got %#v", bg, terminal.Snapshot.Screen.Cells[0][11])
	}
	screen := terminal.VTerm.ScreenContent()
	if got := screen.Cells[0][11].Style.BG; got != bg {
		t.Fatalf("expected vterm styled blank bg %q, got %#v", bg, screen.Cells[0][11])
	}
}

func TestRuntimeScreenUpdateAppliesScreenScrollShiftToLocalVTerm(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil {
		t.Fatal("expected terminal")
	}

	terminal.Snapshot = snapshotWithLines("term-1", 4, 3, []string{"row1", "row2", "row3"})
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size:         protocol.Size{Cols: 4, Rows: 3},
		ScreenScroll: 1,
		Ops: []protocol.ScreenOp{
			{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 0, Width: 4, Height: 3}, Dy: -1},
			{Code: protocol.ScreenOpWriteSpan, Row: 2, Col: 0, Cells: []protocol.Cell{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "4", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 2, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})

	screen := terminal.VTerm.ScreenContent()
	got := []string{
		screen.Cells[0][0].Content + screen.Cells[0][1].Content + screen.Cells[0][2].Content + screen.Cells[0][3].Content,
		screen.Cells[1][0].Content + screen.Cells[1][1].Content + screen.Cells[1][2].Content + screen.Cells[1][3].Content,
		screen.Cells[2][0].Content + screen.Cells[2][1].Content + screen.Cells[2][2].Content + screen.Cells[2][3].Content,
	}
	if !reflect.DeepEqual(got, []string{"row2", "row3", "row4"}) {
		t.Fatalf("expected local vterm screen scroll shift applied, got %#v", got)
	}
}

func TestRuntimeScreenUpdateAppliesOpcodeScrollRectToLocalVTerm(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil {
		t.Fatal("expected terminal")
	}

	terminal.Snapshot = snapshotWithLines("term-1", 4, 4, []string{"row1", "row2", "row3", "row4"})
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size:         protocol.Size{Cols: 4, Rows: 4},
		ScreenScroll: 1,
		Ops: []protocol.ScreenOp{
			{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 0, Width: 4, Height: 4}, Dy: -1},
			{Code: protocol.ScreenOpWriteSpan, Row: 3, Col: 0, Cells: []protocol.Cell{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "5", Width: 1}}},
			{Code: protocol.ScreenOpCursor, Cursor: protocol.CursorState{Row: 3, Col: 0, Visible: true}},
			{Code: protocol.ScreenOpModes, Modes: protocol.TerminalModes{AutoWrap: true}},
		},
		Cursor: protocol.CursorState{Row: 3, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})

	screen := terminal.VTerm.ScreenContent()
	got := []string{
		screen.Cells[0][0].Content + screen.Cells[0][1].Content + screen.Cells[0][2].Content + screen.Cells[0][3].Content,
		screen.Cells[1][0].Content + screen.Cells[1][1].Content + screen.Cells[1][2].Content + screen.Cells[1][3].Content,
		screen.Cells[2][0].Content + screen.Cells[2][1].Content + screen.Cells[2][2].Content + screen.Cells[2][3].Content,
		screen.Cells[3][0].Content + screen.Cells[3][1].Content + screen.Cells[3][2].Content + screen.Cells[3][3].Content,
	}
	if !reflect.DeepEqual(got, []string{"row2", "row3", "row4", "row5"}) {
		t.Fatalf("expected local vterm opcode scrollrect applied, got %#v", got)
	}
}

func TestRuntimeCapturesAlternateScreenVisualHistoryFromScrollRect(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	terminal.Snapshot = snapshotWithLines("term-1", 4, 3, []string{"old1", "old2", "old3"})
	terminal.Snapshot.Modes.AlternateScreen = true
	terminal.Snapshot.Screen.IsAlternateScreen = true
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size:         protocol.Size{Cols: 4, Rows: 3},
		ScreenScroll: 1,
		Ops: []protocol.ScreenOp{
			{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 0, Width: 4, Height: 3}, Dy: -1},
			{Code: protocol.ScreenOpWriteSpan, Row: 2, Col: 0, Cells: []protocol.Cell{{Content: "n", Width: 1}, {Content: "e", Width: 1}, {Content: "w", Width: 1}, {Content: "4", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 2, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true, AlternateScreen: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})

	history := rt.AlternateScrollbackSnapshot("term-1", terminal.Snapshot)
	if history == nil || len(history.Scrollback) != 1 {
		t.Fatalf("expected one alternate history row, got %#v", history)
	}
	if got := compactRowText(history.Scrollback[0]); got != "old1" {
		t.Fatalf("expected scrolled-out row in alternate history, got %q", got)
	}
	if !history.Modes.AlternateScreen || !history.Screen.IsAlternateScreen {
		t.Fatalf("expected alternate flags preserved, got modes=%#v screen=%v", history.Modes, history.Screen.IsAlternateScreen)
	}
}

func TestRuntimeScreenUpdateTitleOnlyKeepsBootstrapPending(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}
	terminal.Snapshot = snapshotWithLines("term-1", 6, 2, []string{"seed"})
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)
	terminal.BootstrapPending = true

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Title:  "renamed",
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})

	if !terminal.BootstrapPending {
		t.Fatalf("expected title-only screen update to keep bootstrap pending, got %#v", terminal)
	}
	if terminal.Title != "renamed" {
		t.Fatalf("expected title-only screen update to update title, got %#v", terminal)
	}
	if terminal.Snapshot == nil || !snapshotContains(terminal.Snapshot, "seed") {
		t.Fatalf("expected title-only screen update to preserve existing snapshot content, got %#v", terminal.Snapshot)
	}
}

func TestRuntimeScreenUpdateTitleOnlyKeepsRecoveryState(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}
	terminal.Snapshot = snapshotWithLines("term-1", 6, 2, []string{"seed"})
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)
	terminal.Recovery = RecoveryState{SyncLost: true, DroppedBytes: 9}

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Title:  "renamed",
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})

	if terminal.Recovery != (RecoveryState{SyncLost: true, DroppedBytes: 9}) {
		t.Fatalf("expected title-only screen update to preserve recovery state, got %#v", terminal.Recovery)
	}
	if terminal.Title != "renamed" {
		t.Fatalf("expected title-only screen update to update title, got %#v", terminal)
	}
}

func TestRuntimeScreenUpdateFullReplaceClearsRecoveryState(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}

	rt := New(client)
	terminal, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}
	terminal.Snapshot = snapshotWithLines("term-1", 6, 2, []string{"seed"})
	loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)
	terminal.Recovery = RecoveryState{SyncLost: true, DroppedBytes: 9}

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		FullReplace: true,
		Size:        protocol.Size{Cols: 6, Rows: 2},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			{{Content: "o", Width: 1}, {Content: "k", Width: 1}},
		}},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})

	if terminal.Recovery != (RecoveryState{}) {
		t.Fatalf("expected full-replace screen update to clear recovery, got %#v", terminal.Recovery)
	}
	if terminal.Snapshot == nil || !snapshotContains(terminal.Snapshot, "ok") {
		t.Fatalf("expected full-replace screen update to refresh snapshot content, got %#v", terminal.Snapshot)
	}
	if !terminal.ScreenUpdate.FullReplace {
		t.Fatalf("expected full-replace screen update summary, got %#v", terminal.ScreenUpdate)
	}
}

func TestRuntimeScreenUpdateFullReplaceResetsLatestBoundaryContract(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name              string
		snapshot          *protocol.Snapshot
		wantLoadedLimit   int
		wantAuthoritative bool
	}{
		{
			name: "live_tail_only_latest",
			snapshot: func() *protocol.Snapshot {
				snapshot := snapshotWithLines("term-1", 6, 2, []string{"live"})
				snapshot.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
					protocolRowFromString("tail0"),
					protocolRowFromString("tail1"),
				})
				snapshot.ScrollbackTotal = 4
				snapshot.ScrollbackLogicalTotal = 4
				snapshot.ScrollbackHasMore = true
				markSnapshotScrollbackOwnership(snapshot, protocol.RowOwnershipLiveTailLive)
				return snapshot
			}(),
			wantLoadedLimit: 0,
		},
		{
			name: "canonical_plus_live_tail_latest",
			snapshot: func() *protocol.Snapshot {
				snapshot := snapshotWithLines("term-1", 6, 2, []string{"live"})
				snapshot.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
					protocolRowFromString("committed0"),
					protocolRowFromString("tail0"),
				})
				snapshot.ScrollbackTotal = 2
				snapshot.ScrollbackLogicalTotal = 1
				snapshot.ScrollbackHasMore = true
				snapshot.ScrollbackLoadedRows = 1
				snapshot.HistoryGeneration = 7
				snapshot.ScrollbackFirstRowID = 0
				snapshot.ScrollbackLastRowID = 0
				snapshot.ScrollbackOwnership = []string{protocol.RowOwnershipPersisted, protocol.RowOwnershipLiveTailLive}
				return snapshot
			}(),
			wantLoadedLimit: 1,
		},
		{
			name: "canonical_only_latest",
			snapshot: func() *protocol.Snapshot {
				snapshot := snapshotWithLines("term-1", 6, 2, []string{"live"})
				snapshot.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
					protocolRowFromString("committed0"),
				})
				snapshot.ScrollbackTotal = 1
				snapshot.ScrollbackLogicalTotal = 1
				snapshot.ScrollbackLoadedRows = 1
				snapshot.HistoryGeneration = 7
				snapshot.ScrollbackFirstRowID = 0
				snapshot.ScrollbackLastRowID = 0
				markSnapshotScrollbackPersisted(snapshot)
				return snapshot
			}(),
			wantLoadedLimit: 1,
		},
	}

	assertBoundaryReset := func(t *testing.T, stage string, terminal *TerminalRuntime) {
		t.Helper()
		if terminal == nil || terminal.Snapshot == nil {
			t.Fatalf("%s: expected terminal snapshot, got %#v", stage, terminal)
		}
		if got := terminal.CommittedLoadedDepth; got != 0 {
			t.Fatalf("%s: expected latest full-replace boundary to clear committed depth, got %d", stage, got)
		}
		if terminal.CommittedHistoryExhausted {
			t.Fatalf("%s: expected latest full-replace boundary to clear exhausted paging state", stage)
		}
		if got := snapshotScrollbackLoadedDepth(terminal.Snapshot); got != 0 {
			t.Fatalf("%s: expected full-replace snapshot committed depth 0, got %d", stage, got)
		}
		if got := terminal.Snapshot.HistoryGeneration; got != 0 {
			t.Fatalf("%s: expected full-replace snapshot generation 0, got %d", stage, got)
		}
		if got := terminal.Snapshot.ScrollbackLoadedRows; got != 0 {
			t.Fatalf("%s: expected full-replace snapshot loaded rows 0, got %d", stage, got)
		}
		if terminal.Snapshot.ScrollbackFirstRowID != 0 || terminal.Snapshot.ScrollbackLastRowID != 0 {
			t.Fatalf("%s: expected full-replace snapshot canonical row window cleared, got %d..%d", stage, terminal.Snapshot.ScrollbackFirstRowID, terminal.Snapshot.ScrollbackLastRowID)
		}
		if terminal.Snapshot.ScrollbackTotal != 0 || terminal.Snapshot.ScrollbackLogicalTotal != 0 || terminal.Snapshot.ScrollbackHasMore {
			t.Fatalf("%s: expected full-replace snapshot to drop old latest totals/has-more, got total=%d logical=%d hasMore=%v", stage, terminal.Snapshot.ScrollbackTotal, terminal.Snapshot.ScrollbackLogicalTotal, terminal.Snapshot.ScrollbackHasMore)
		}
		if got := len(terminal.Snapshot.Scrollback); got != 0 {
			t.Fatalf("%s: expected full-replace snapshot to avoid inherited scrollback rows, got %d", stage, got)
		}
		if !snapshotContains(terminal.Snapshot, "fresh") {
			t.Fatalf("%s: expected fresh full-replace screen content, got %#v", stage, terminal.Snapshot)
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeBridgeClient()
			client.snapshotByTerminal["term-1"] = cloneSnapshot(tt.snapshot)
			rt := New(client)
			if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 0); err != nil {
				t.Fatalf("load snapshot: %v", err)
			}

			terminal := rt.Registry().Get("term-1")
			if terminal == nil || terminal.Snapshot == nil || terminal.VTerm == nil {
				t.Fatalf("expected hydrated terminal, got %#v", terminal)
			}
			if got := terminal.CommittedLoadedDepth; got < tt.wantLoadedLimit {
				t.Fatalf("expected precondition loaded depth at least %d, got %d", tt.wantLoadedLimit, got)
			}
			terminal.CommittedHistoryExhausted = true

			updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
				FullReplace: true,
				Size:        protocol.Size{Cols: 6, Rows: 2},
				Screen:      snapshotWithLines("term-1", 6, 2, []string{"fresh"}).Screen,
				Cursor:      protocol.CursorState{Visible: true},
				Modes:       protocol.TerminalModes{AutoWrap: true},
			})
			if err != nil {
				t.Fatalf("encode update: %v", err)
			}

			rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})
			assertBoundaryReset(t, "after_full_replace", rt.Registry().Get("term-1"))

			if !rt.RefreshSnapshotFromVTerm("term-1") {
				t.Fatal("expected refresh from vterm after full replace")
			}
			assertBoundaryReset(t, "after_refresh_from_vterm", rt.Registry().Get("term-1"))
		})
	}
}

func TestRuntimeScreenUpdateFullReplaceWithScrollbackAppendKeepsBoundaryReset(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	seed := snapshotWithLines("term-1", 6, 2, []string{"live"})
	seed.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromString("committed0"),
		protocolRowFromString("tail0"),
	})
	seed.ScrollbackTotal = 2
	seed.ScrollbackLogicalTotal = 1
	seed.ScrollbackHasMore = true
	seed.ScrollbackLoadedRows = 1
	seed.HistoryGeneration = 7
	seed.ScrollbackFirstRowID = 0
	seed.ScrollbackLastRowID = 0
	seed.ScrollbackOwnership = []string{protocol.RowOwnershipPersisted, protocol.RowOwnershipLiveTailLive}
	client.snapshotByTerminal["term-1"] = seed

	rt := New(client)
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal, got %#v", terminal)
	}
	if got := terminal.CommittedLoadedDepth; got < 1 {
		t.Fatalf("expected precondition committed depth at least 1, got %d", got)
	}
	terminal.CommittedHistoryExhausted = true

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		FullReplace: true,
		Size:        protocol.Size{Cols: 6, Rows: 2},
		ScrollbackAppend: []protocol.ScrollbackRowAppend{{
			Cells:     protocolRowFromString("tail0"),
			Timestamp: time.Now(),
		}},
		Screen: snapshotWithLines("term-1", 6, 2, []string{"fresh"}).Screen,
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}

	assertBoundaryResetWithAppend := func(t *testing.T, stage string, terminal *TerminalRuntime) {
		t.Helper()
		if terminal == nil || terminal.Snapshot == nil {
			t.Fatalf("%s: expected terminal snapshot, got %#v", stage, terminal)
		}
		if got := terminal.CommittedLoadedDepth; got != 0 {
			t.Fatalf("%s: expected full-replace append to keep committed depth reset, got %d", stage, got)
		}
		if terminal.CommittedHistoryExhausted {
			t.Fatalf("%s: expected full-replace append to clear exhausted paging state", stage)
		}
		if got := terminal.Snapshot.ScrollbackLoadedRows; got != 0 {
			t.Fatalf("%s: expected full-replace append snapshot loaded rows 0, got %d", stage, got)
		}
		if got := terminal.Snapshot.HistoryGeneration; got != 0 {
			t.Fatalf("%s: expected full-replace append snapshot generation 0, got %d", stage, got)
		}
		if terminal.Snapshot.ScrollbackFirstRowID != 0 || terminal.Snapshot.ScrollbackLastRowID != 0 {
			t.Fatalf("%s: expected full-replace append snapshot canonical row window cleared, got %d..%d", stage, terminal.Snapshot.ScrollbackFirstRowID, terminal.Snapshot.ScrollbackLastRowID)
		}
		if got := terminal.Snapshot.ScrollbackOffset; got != 0 {
			t.Fatalf("%s: expected full-replace append snapshot offset 0, got %d", stage, got)
		}
		if terminal.Snapshot.ScrollbackTotal != 0 || terminal.Snapshot.ScrollbackLogicalTotal != 0 || terminal.Snapshot.ScrollbackHasMore {
			t.Fatalf("%s: expected full-replace append snapshot totals/has-more reset, got total=%d logical=%d hasMore=%v", stage, terminal.Snapshot.ScrollbackTotal, terminal.Snapshot.ScrollbackLogicalTotal, terminal.Snapshot.ScrollbackHasMore)
		}
		if got := len(terminal.Snapshot.Scrollback); got != 1 {
			t.Fatalf("%s: expected full-replace append snapshot to keep 1 materialized tail row, got %d", stage, got)
		}
		if got := compactRowText(terminal.Snapshot.Scrollback[0]); got != "tail0" {
			t.Fatalf("%s: expected full-replace append snapshot tail row, got %q", stage, got)
		}
		if !protocol.HasOnlyLiveTailLiveOwnership(terminal.Snapshot.ScrollbackOwnership, len(terminal.Snapshot.Scrollback)) {
			t.Fatalf("%s: expected full-replace append row to remain live-tail owned, got %#v", stage, terminal.Snapshot.ScrollbackOwnership)
		}
		if !snapshotContains(terminal.Snapshot, "fresh") {
			t.Fatalf("%s: expected fresh full-replace screen content, got %#v", stage, terminal.Snapshot)
		}
	}

	rt.handleStreamFrame("term-1", protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})
	assertBoundaryResetWithAppend(t, "after_full_replace", rt.Registry().Get("term-1"))

	if !rt.RefreshSnapshotFromVTerm("term-1") {
		t.Fatal("expected refresh from vterm after full replace append")
	}
	assertBoundaryResetWithAppend(t, "after_refresh_from_vterm", rt.Registry().Get("term-1"))

	page := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 6, Rows: 2},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("canon0")}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        1,
		ScrollbackLogicalTotal: 1,
		ScrollbackHasMore:      false,
		ScrollbackLoadedRows:   1,
		HistoryGeneration:      11,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackRowKinds:     []string{""},
		ScrollbackWrapped:      []bool{false},
		ScrollbackOwnership:    []string{protocol.RowOwnershipPersisted},
		ScrollbackTimestamps:   []time.Time{time.Now()},
	}
	if !rt.ApplyGridViewportPage("term-1", page, 0) {
		t.Fatal("expected latest boundary reset to restart viewport paging from offset 0")
	}
	terminal = rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected terminal snapshot after offset-0 replace, got %#v", terminal)
	}
	if got := terminal.CommittedLoadedDepth; got != 1 {
		t.Fatalf("expected authoritative offset-0 page to own committed depth 1 after reset, got %d", got)
	}
	if got := terminal.Snapshot.HistoryGeneration; got != 11 {
		t.Fatalf("expected offset-0 page generation 11 after reset, got %d", got)
	}
	if got := compactRowText(terminal.Snapshot.Scrollback[0]); got != "canon0" {
		t.Fatalf("expected offset-0 page to replace full-replace append tail, got %q", got)
	}
}

func TestNewScreenUpdateContractDeduplicatesChangedRows(t *testing.T) {
	contract := NewScreenUpdateContract(protocol.ScreenUpdate{
		Ops: []protocol.ScreenOp{
			{Code: protocol.ScreenOpWriteSpan, Row: 4},
			{Code: protocol.ScreenOpClearToEOL, Row: 4},
			{Code: protocol.ScreenOpWriteSpan, Row: 1},
		},
	})

	if !reflect.DeepEqual(contract.Summary.ChangedRows, []int{4, 1}) {
		t.Fatalf("expected changed row summary to deduplicate in wire order, got %#v", contract.Summary.ChangedRows)
	}
}

func TestEffectiveInteractiveLatencyWindowUsesRemoteProfile(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	t.Setenv("TERMX_INTERACTIVE_LATENCY_WINDOW", "")
	if got := effectiveInteractiveLatencyWindow(); got != remoteInteractiveLatencyWindow {
		t.Fatalf("expected remote interactive latency window %v, got %v", remoteInteractiveLatencyWindow, got)
	}
}

func TestRuntimeScreenUpdatePreservesAuthoritativeSnapshotSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 118, 36, []string{"ready"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.Snapshot == nil {
		t.Fatalf("expected cached snapshot, got %#v", stored)
	}
	if stored.Snapshot.Size.Cols != 118 || stored.Snapshot.Size.Rows != 36 {
		t.Fatalf("expected loaded snapshot size 118x36, got %#v", stored.Snapshot.Size)
	}

	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}
	client.sendFrame(9, screenUpdateFrameForLines(t, 118, 36, "x"))

	waitFor(t, func() bool {
		current := rt.Registry().Get("term-1")
		return current != nil && vtermContains(current.VTerm, "x")
	})

	current := rt.Registry().Get("term-1")
	if current == nil || current.VTerm == nil {
		t.Fatalf("expected refreshed live surface, got %#v", current)
	}
	cols, rows := current.VTerm.Size()
	if cols != 118 || rows != 36 {
		t.Fatalf("expected streamed output to preserve surface size 118x36, got %dx%d", cols, rows)
	}
}

func TestRuntimeScreenUpdatePreservesWideSnapshotCellsAfterReattach(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 8, Rows: 2},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			{
				{Content: "你", Width: 2},
				{Content: "", Width: 0},
				{Content: "好", Width: 2},
				{Content: "", Width: 0},
				{Content: "A", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
			},
			{
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
			},
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 5, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	updatePayload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 8, Rows: 2},
		Ops: []protocol.ScreenOp{{
			Code:  protocol.ScreenOpWriteSpan,
			Row:   0,
			Col:   5,
			Cells: []protocol.Cell{{Content: "!", Width: 1}},
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 6, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode screen update: %v", err)
	}
	client.sendFrame(9, protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: updatePayload})

	waitFor(t, func() bool {
		current := rt.Registry().Get("term-1")
		if current == nil || current.VTerm == nil {
			return false
		}
		row := current.VTerm.ScreenContent().Cells[0]
		return len(row) > 5 && row[5].Content == "!"
	})

	current := rt.Registry().Get("term-1")
	if current == nil || current.VTerm == nil {
		t.Fatalf("expected refreshed live surface, got %#v", current)
	}
	row := current.VTerm.ScreenContent().Cells[0]
	if got := row[0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected first wide snapshot cell preserved after stream output, got %#v", got)
	}
	if got := row[1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected first continuation preserved after stream output, got %#v", got)
	}
	if got := row[2]; got.Content != "好" || got.Width != 2 {
		t.Fatalf("expected second wide snapshot cell preserved after stream output, got %#v", got)
	}
	if got := row[3]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected second continuation preserved after stream output, got %#v", got)
	}
	if got := row[4]; got.Content != "A" || got.Width != 1 {
		t.Fatalf("expected ASCII cell before stream output preserved, got %#v", got)
	}
	if got := row[5]; got.Content != "!" || got.Width != 1 {
		t.Fatalf("expected streamed output appended after preserved wide cells, got %#v", got)
	}
}

func TestRuntimeSetHostDefaultColorsRefreshesVisibleState(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	initial := rt.Visible()
	if initial == nil {
		t.Fatal("expected visible runtime")
	}
	if initial.HostDefaultFG != "" || initial.HostDefaultBG != "" {
		t.Fatalf("expected empty initial host colors, got fg=%q bg=%q", initial.HostDefaultFG, initial.HostDefaultBG)
	}

	rt.SetHostDefaultColors(color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff})

	visible := rt.Visible()
	if visible.HostDefaultFG != "#aabbcc" || visible.HostDefaultBG != "#112233" {
		t.Fatalf("expected visible host colors to refresh, got fg=%q bg=%q", visible.HostDefaultFG, visible.HostDefaultBG)
	}
	if invalidateCount.Load() == 0 {
		t.Fatal("expected host default color update to invalidate rendering")
	}
}

func TestRuntimeSetHostPaletteColorRefreshesVisibleState(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	if visible := rt.Visible(); visible == nil {
		t.Fatal("expected visible runtime")
	}

	rt.SetHostPaletteColor(5, color.RGBA{R: 0x44, G: 0x88, B: 0xcc, A: 0xff})

	visible := rt.Visible()
	if got := visible.HostPalette[5]; got != "#4488cc" {
		t.Fatalf("expected visible host palette to refresh, got %q", got)
	}
	if invalidateCount.Load() == 0 {
		t.Fatal("expected host palette update to invalidate rendering")
	}
}

func TestRuntimeApplyHostThemeInvalidatesOnceForBatchedTheme(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	rt.ApplyHostTheme(
		color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff},
		color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff},
		map[int]color.Color{
			1: color.RGBA{R: 0x44, G: 0x88, B: 0xcc, A: 0xff},
			2: color.RGBA{R: 0x55, G: 0x99, B: 0xdd, A: 0xff},
		},
	)

	if got := invalidateCount.Load(); got != 1 {
		t.Fatalf("expected batched host theme apply to invalidate once, got %d", got)
	}
	visible := rt.Visible()
	if visible == nil {
		t.Fatal("expected visible runtime")
	}
	if visible.HostDefaultFG != "#aabbcc" || visible.HostDefaultBG != "#112233" {
		t.Fatalf("unexpected host default colors %#v", visible)
	}
	if visible.HostPalette[1] != "#4488cc" || visible.HostPalette[2] != "#5599dd" {
		t.Fatalf("unexpected host palette %#v", visible.HostPalette)
	}
}

func TestRuntimeApplyHostThemeSilentlyRefreshesVisibleStateWithoutInvalidation(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	rt.ApplyHostThemeSilently(
		color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff},
		color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff},
		map[int]color.Color{5: color.RGBA{R: 0x44, G: 0x88, B: 0xcc, A: 0xff}},
	)

	if got := invalidateCount.Load(); got != 0 {
		t.Fatalf("expected silent host theme apply not to invalidate, got %d", got)
	}
	visible := rt.Visible()
	if visible == nil {
		t.Fatal("expected visible runtime")
	}
	if visible.HostDefaultFG != "#aabbcc" || visible.HostDefaultBG != "#112233" {
		t.Fatalf("unexpected host default colors %#v", visible)
	}
	if visible.HostPalette[5] != "#4488cc" {
		t.Fatalf("unexpected host palette %#v", visible.HostPalette)
	}
}

func TestRuntimeSetHostAmbiguousEmojiVariationSelectorModeRefreshesVisibleState(t *testing.T) {
	var invalidateCount atomic.Int32
	rt := New(nil, WithInvalidate(func() {
		invalidateCount.Add(1)
	}))

	if visible := rt.Visible(); visible == nil {
		t.Fatal("expected visible runtime")
	} else if visible.HostEmojiVS16Mode != shared.AmbiguousEmojiVariationSelectorRaw {
		t.Fatalf("expected raw host emoji mode by default, got %q", visible.HostEmojiVS16Mode)
	}

	rt.SetHostAmbiguousEmojiVariationSelectorMode(shared.AmbiguousEmojiVariationSelectorAdvance)

	visible := rt.Visible()
	if visible.HostEmojiVS16Mode != shared.AmbiguousEmojiVariationSelectorAdvance {
		t.Fatalf("expected visible host emoji mode to refresh, got %q", visible.HostEmojiVS16Mode)
	}
	if invalidateCount.Load() == 0 {
		t.Fatal("expected host emoji mode update to invalidate rendering")
	}
}

func TestRuntimeResizePaneUsesBindingChannelAndRefreshesSnapshot(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 6, 3, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 100, 40); err != nil {
		t.Fatalf("resize pane: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected 1 resize call, got %d", len(client.resizeCalls))
	}
	call := client.resizeCalls[0]
	if call.channel != 11 || call.cols != 100 || call.rows != 40 {
		t.Fatalf("unexpected resize call: %+v", call)
	}
	stored := rt.Registry().Get("term-1")
	if stored == nil || stored.Snapshot == nil {
		t.Fatal("expected terminal snapshot after resize")
	}
	if stored.Snapshot.Size.Cols != 100 || stored.Snapshot.Size.Rows != 40 {
		t.Fatalf("expected resized snapshot size 100x40, got %dx%d", stored.Snapshot.Size.Cols, stored.Snapshot.Size.Rows)
	}
}

func TestRuntimeResizePaneSkipsFollowerBindings(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 100, 40, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}
	client.resizeCalls = nil

	if err := rt.ResizePane(ctx, "pane-2", "term-1", 50, 16); err != nil {
		t.Fatalf("resize follower: %v", err)
	}

	if len(client.resizeCalls) != 0 {
		t.Fatalf("expected follower resize to be ignored, got %#v", client.resizeCalls)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-1 to remain owner, got %#v", binding)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-2 to remain follower, got %#v", binding)
	}
}

func TestRuntimeResizePaneSkipsSizeLockedTerminal(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	terminal.Tags = map[string]string{terminalmeta.SizeLockTag: terminalmeta.SizeLockLock}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 100, 40); err != nil {
		t.Fatalf("resize pane: %v", err)
	}
	if len(client.resizeCalls) != 0 {
		t.Fatalf("expected locked terminal to skip resize call, got %#v", client.resizeCalls)
	}

	visible := rt.Visible()
	if len(visible.Terminals) != 1 || !visible.Terminals[0].SizeLocked {
		t.Fatalf("expected visible runtime to expose locked terminal, got %#v", visible.Terminals)
	}
}

func TestRuntimeResizePaneRefreshesSnapshotWhileBootstrapPending(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}
	terminal.BootstrapPending = true
	client.resizeCalls = nil

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 57, 36); err != nil {
		t.Fatalf("resize pane: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected bootstrap-pending resize to reach bridge client, got %#v", client.resizeCalls)
	}
	if terminal.Snapshot == nil || terminal.Snapshot.Size.Cols != 57 || terminal.Snapshot.Size.Rows != 36 {
		t.Fatalf("expected snapshot size refreshed during bootstrap pending resize, got %#v", terminal.Snapshot)
	}
	if cols, rows := terminal.VTerm.Size(); cols != 57 || rows != 36 {
		t.Fatalf("expected live vterm resized during bootstrap pending resize, got %dx%d", cols, rows)
	}
	if terminal.PendingOwnerResize {
		t.Fatalf("expected pending owner resize cleared after resize, got %#v", terminal)
	}
}

func TestRuntimeResizePaneShrinkKeepsRenderOnSnapshotUntilOutput(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"top", "middle", "bottom"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 57, 20); err != nil {
		t.Fatalf("resize pane shrink: %v", err)
	}

	if !terminal.PreferSnapshot {
		t.Fatalf("expected shrink preview to prefer snapshot, got %#v", terminal)
	}
	if terminal.Snapshot == nil || terminal.Snapshot.Size.Cols != 57 || terminal.Snapshot.Size.Rows != 20 {
		t.Fatalf("expected provisional shrink snapshot size 57x20, got %#v", terminal.Snapshot)
	}
	if cols, rows := terminal.VTerm.Size(); cols != 57 || rows != 20 {
		t.Fatalf("expected live vterm resized to 57x20, got %dx%d", cols, rows)
	}
	visible := rt.Visible()
	if len(visible.Terminals) != 1 || visible.Terminals[0].Surface != nil {
		t.Fatalf("expected visible runtime to hide live surface during shrink preview, got %#v", visible.Terminals)
	}

	rt.handleStreamFrame("term-1", screenUpdateFrameForLines(t, 57, 20, "x"))

	if terminal.PreferSnapshot {
		t.Fatalf("expected first post-resize output to clear shrink preview flag, got %#v", terminal)
	}
	visible = rt.Visible()
	if len(visible.Terminals) != 1 || visible.Terminals[0].Surface == nil {
		t.Fatalf("expected visible runtime to restore live surface after output, got %#v", visible.Terminals)
	}
	if terminal.Snapshot == nil || terminal.Snapshot.Size.Cols != 57 || terminal.Snapshot.Size.Rows != 20 {
		t.Fatalf("expected refreshed snapshot to keep resized geometry, got %#v", terminal.Snapshot)
	}
}

func TestRuntimeResizePaneShrinkPreviewKeepsBottomRowsVisible(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 12, 6, []string{
		"qr-row-0",
		"qr-row-1",
		"qr-row-2",
		"uri: termx",
		"expires",
		"prompt>",
	})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 8, 3); err != nil {
		t.Fatalf("resize pane shrink: %v", err)
	}

	if !terminal.PreferSnapshot {
		t.Fatalf("expected shrink preview to prefer snapshot, got %#v", terminal)
	}
	if terminal.Snapshot == nil || terminal.Snapshot.Size.Cols != 8 || terminal.Snapshot.Size.Rows != 3 {
		t.Fatalf("expected provisional shrink snapshot size 8x3, got %#v", terminal.Snapshot)
	}
	got := []string{
		rowText(terminal.Snapshot.Screen.Cells[0]),
		rowText(terminal.Snapshot.Screen.Cells[1]),
		rowText(terminal.Snapshot.Screen.Cells[2]),
	}
	want := []string{"uri: termx", "expires", "prompt>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected shrink preview to retain bottom rows, got %#v want %#v", got, want)
	}
}

func TestRuntimeResizePaneHeightGrowDoesNotExtendNonBlankBottomRowBackground(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	const statusBG = "#0055aa"
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 4, Rows: 2},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells: [][]protocol.Cell{
				{
					{Content: " ", Width: 1},
					{Content: " ", Width: 1},
					{Content: " ", Width: 1},
					{Content: " ", Width: 1},
				},
				{
					{Content: "S", Width: 1, Style: protocol.CellStyle{BG: statusBG}},
					{Content: "T", Width: 1, Style: protocol.CellStyle{BG: statusBG}},
					{Content: "A", Width: 1, Style: protocol.CellStyle{BG: statusBG}},
					{Content: "T", Width: 1, Style: protocol.CellStyle{BG: statusBG}},
				},
			},
		},
		Cursor:    protocol.CursorState{Row: 1, Col: 4, Visible: true},
		Modes:     protocol.TerminalModes{AutoWrap: true, AlternateScreen: true, MouseTracking: true},
		Timestamp: time.Now(),
	}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected hydrated terminal runtime, got %#v", terminal)
	}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 4, 4); err != nil {
		t.Fatalf("resize pane grow: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected one resize call, got %#v", client.resizeCalls)
	}
	screen := terminal.VTerm.ScreenContent()
	if len(screen.Cells) < 4 {
		t.Fatalf("expected resized vterm height, got %#v", screen.Cells)
	}
	if got := screen.Cells[1][0].Style.BG; got != statusBG {
		t.Fatalf("expected status row to keep background %q, got %#v", statusBG, screen.Cells[1][0])
	}
	for _, point := range []struct {
		row int
		col int
	}{
		{row: 2, col: 0},
		{row: 2, col: 3},
		{row: 3, col: 0},
		{row: 3, col: 3},
	} {
		if got := screen.Cells[point.row][point.col].Style.BG; got != "" {
			t.Fatalf("expected grown row %d col %d to stay unfilled, got %#v", point.row, point.col, screen.Cells[point.row][point.col])
		}
	}
	if terminal.Snapshot == nil || terminal.Snapshot.Size.Rows != 4 {
		t.Fatalf("expected snapshot size refreshed to height 4, got %#v", terminal.Snapshot)
	}
	if got := terminal.Snapshot.Screen.Cells[2][0].Style.BG; got != "" {
		t.Fatalf("expected refreshed snapshot not to extend status background, got %#v", terminal.Snapshot.Screen.Cells[2][0])
	}
}

func TestRuntimeResizeFrameDoesNotExposeLocalShrinkMidStateBeforeOutput(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"top", "middle", "bottom"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}

	if err := rt.ResizePane(ctx, "pane-1", "term-1", 57, 20); err != nil {
		t.Fatalf("resize pane shrink: %v", err)
	}
	rt.handleStreamFrame("term-1", protocol.StreamFrame{
		Type:    wire.TypeResize,
		Payload: wire.EncodeResizePayload(57, 20),
	})

	if !terminal.PreferSnapshot {
		t.Fatalf("expected resize frame alone to keep shrink preview on snapshot, got %#v", terminal)
	}
	visible := rt.Visible()
	if len(visible.Terminals) != 1 || visible.Terminals[0].Surface != nil {
		t.Fatalf("expected resize echo not to expose provisional shrink surface, got %#v", visible.Terminals)
	}
}

func TestRuntimeResizeFramePreservesExactWidthHardBreakRows(t *testing.T) {
	rt := New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.VTerm = localvterm.New(4, 4, 100, nil)
	if _, err := terminal.VTerm.Write([]byte("ABCD\r\nWXYZ")); err != nil {
		t.Fatalf("seed terminal: %v", err)
	}

	rt.handleResizeFrame(terminal, "term-1", protocol.StreamFrame{
		Type:    wire.TypeResize,
		Payload: wire.EncodeResizePayload(8, 4),
	})

	if terminal.Snapshot == nil {
		t.Fatal("expected resize frame to refresh snapshot")
	}
	if got := rowText(terminal.Snapshot.Screen.Cells[0]); got != "ABCD" {
		t.Fatalf("expected row 0 hard break preserved after resize frame, got %q", got)
	}
	if got := rowText(terminal.Snapshot.Screen.Cells[1]); got != "WXYZ" {
		t.Fatalf("expected row 1 hard break preserved after resize frame, got %q", got)
	}
	if len(terminal.Snapshot.ScreenWrapped) > 0 && terminal.Snapshot.ScreenWrapped[0] {
		t.Fatalf("expected exact-width hard break row to remain unwrapped, got %#v", terminal.Snapshot.ScreenWrapped)
	}
}

func TestRuntimeResizeFrameRejoinsSplitHardBreakRows(t *testing.T) {
	rt := New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.VTerm = localvterm.New(8, 6, 100, nil)
	if _, err := terminal.VTerm.Write([]byte("AA  BB  \r\nCCDDCCDD\r\nuri: ok")); err != nil {
		t.Fatalf("seed terminal: %v", err)
	}

	rt.handleResizeFrame(terminal, "term-1", protocol.StreamFrame{
		Type:    wire.TypeResize,
		Payload: wire.EncodeResizePayload(4, 6),
	})
	if terminal.Snapshot == nil {
		t.Fatal("expected shrink resize frame to refresh snapshot")
	}
	gotShrink := []string{
		strings.TrimSpace(rowText(terminal.Snapshot.Screen.Cells[0])),
		strings.TrimSpace(rowText(terminal.Snapshot.Screen.Cells[1])),
		rowText(terminal.Snapshot.Screen.Cells[2]),
		rowText(terminal.Snapshot.Screen.Cells[3]),
		strings.TrimSpace(rowText(terminal.Snapshot.Screen.Cells[4])),
		strings.TrimSpace(rowText(terminal.Snapshot.Screen.Cells[5])),
	}
	wantShrink := []string{"AA", "BB", "CCDD", "CCDD", "uri:", "ok"}
	if !reflect.DeepEqual(gotShrink, wantShrink) {
		t.Fatalf("expected shrink resize to split hard-break rows, got %#v want %#v", gotShrink, wantShrink)
	}
	if wrapped := terminal.Snapshot.ScreenWrapped; len(wrapped) < 6 || !wrapped[0] || wrapped[1] || !wrapped[2] || wrapped[3] || !wrapped[4] || wrapped[5] {
		t.Fatalf("unexpected shrink wrapped markers: %#v", wrapped)
	}

	rt.handleResizeFrame(terminal, "term-1", protocol.StreamFrame{
		Type:    wire.TypeResize,
		Payload: wire.EncodeResizePayload(8, 6),
	})
	gotGrow := []string{
		strings.TrimSpace(rowText(terminal.Snapshot.Screen.Cells[0])),
		rowText(terminal.Snapshot.Screen.Cells[1]),
		strings.TrimSpace(rowText(terminal.Snapshot.Screen.Cells[2])),
	}
	wantGrow := []string{"AA  BB", "CCDDCCDD", "uri: ok"}
	if !reflect.DeepEqual(gotGrow, wantGrow) {
		t.Fatalf("expected grow resize to rejoin hard-break rows, got %#v want %#v", gotGrow, wantGrow)
	}
	if wrapped := terminal.Snapshot.ScreenWrapped; len(wrapped) >= 3 && (wrapped[0] || wrapped[1] || wrapped[2]) {
		t.Fatalf("expected visible wrapped markers clear after grow, got %#v", wrapped)
	}
}

func TestRuntimeSnapshotLoadPreservesTrailingSpaceColumnsAfterResize(t *testing.T) {
	client := newFakeBridgeClient()
	rt := New(client)
	ctx := context.Background()
	client.attachResult = &protocol.AttachResult{Mode: "stream", Channel: 1}

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	source := localvterm.New(4, 6, 32, nil)
	if _, err := source.Write([]byte("AA  \r\nBB")); err != nil {
		t.Fatalf("seed source vterm: %v", err)
	}
	source.Resize(2, 6)
	client.snapshotByTerminal["term-1"] = snapshotFromVTerm("term-1", source)

	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil || terminal.VTerm == nil {
		t.Fatal("expected runtime snapshot and vterm")
	}

	got := []string{
		rowTextRaw(terminal.Snapshot.Screen.Cells[0]),
		rowTextRaw(terminal.Snapshot.Screen.Cells[1]),
		rowTextRaw(terminal.Snapshot.Screen.Cells[2]),
		vtermScreenRowTextRaw(terminal.VTerm, 0),
		vtermScreenRowTextRaw(terminal.VTerm, 1),
		vtermScreenRowTextRaw(terminal.VTerm, 2),
	}
	want := []string{"AA", "  ", "BB", "AA", "  ", "BB"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected snapshot and TUI vterm rows %#v, got %#v", want, got)
	}
}

func TestRuntimeUnbindOwnerLeavesTerminalWithoutOwner(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.UnbindPane("pane-1", "term-1")

	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	if terminal.OwnerPaneID != "" {
		t.Fatalf("expected terminal owner cleared, got %q", terminal.OwnerPaneID)
	}
	if !reflect.DeepEqual(terminal.BoundPaneIDs, []string{"pane-2"}) {
		t.Fatalf("expected only pane-2 to remain bound, got %#v", terminal.BoundPaneIDs)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-2 binding to remain follower, got %#v", binding)
	}
	if binding := rt.Binding("pane-1"); binding != nil {
		t.Fatalf("expected pane-1 binding removed, got %#v", binding)
	}
}

func TestRuntimeAcquireTerminalOwnershipPromotesRequestedPane(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	if err := rt.AcquireTerminalOwnership("pane-2", "term-1"); err != nil {
		t.Fatalf("acquire ownership: %v", err)
	}

	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 as owner, got %q", terminal.OwnerPaneID)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-1 demoted to follower, got %#v", binding)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-2 promoted to owner, got %#v", binding)
	}
}

func TestRuntimeAcquireTerminalOwnershipForcesNextOwnerResize(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 50, 16, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}
	if err := rt.AcquireTerminalOwnership("pane-2", "term-1"); err != nil {
		t.Fatalf("acquire ownership: %v", err)
	}

	client.resizeCalls = nil
	if err := rt.ResizePane(ctx, "pane-2", "term-1", 50, 16); err != nil {
		t.Fatalf("resize pane: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected forced resize after owner handoff, got %#v", client.resizeCalls)
	}
	if got := rt.Registry().Get("term-1"); got == nil || got.PendingOwnerResize {
		t.Fatalf("expected pending owner resize cleared after resize, got %#v", got)
	}
}

func TestRuntimeResizeDoesNothingWithoutExplicitOwner(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.UnbindPane("pane-1", "term-1")

	if err := rt.ResizePane(ctx, "pane-2", "term-1", 100, 30); err != nil {
		t.Fatalf("resize pane: %v", err)
	}
	if len(client.resizeCalls) != 0 {
		t.Fatalf("expected no resize calls without explicit owner, got %#v", client.resizeCalls)
	}
}

func TestRuntimeApplySessionLeasesDemotesForeignLeaseAndPromotesLocalLease(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.ApplySessionLeases("view-local", []sessionstore.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-remote",
		PaneID:     "pane-9",
	}})

	if terminal := rt.Registry().Get("term-1"); terminal == nil || terminal.OwnerPaneID != "pane-9" || terminal.ControlPaneID != "" || !terminal.RequiresExplicitOwner {
		t.Fatalf("expected foreign lease to demote local panes, got %#v", terminal)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-1 follower under foreign lease, got %#v", binding)
	}

	rt.ApplySessionLeases("view-local", []sessionstore.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-local",
		PaneID:     "pane-2",
	}})

	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.OwnerPaneID != "pane-2" || terminal.RequiresExplicitOwner {
		t.Fatalf("expected local lease to promote pane-2 owner, got %#v", terminal)
	}
	if !terminal.PendingOwnerResize {
		t.Fatalf("expected local lease promotion to force next resize, got %#v", terminal)
	}
	if binding := rt.Binding("pane-2"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-2 owner under local lease, got %#v", binding)
	}
}

func TestRuntimeApplySessionLeasesDemotesLocalPaneWhenForeignLeaseReusesSamePaneID(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}

	rt.ApplySessionLeases("view-local", []sessionstore.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-remote",
		PaneID:     "pane-1",
	}})

	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.OwnerPaneID != "pane-1" || terminal.ControlPaneID != "" || !terminal.RequiresExplicitOwner {
		t.Fatalf("expected foreign lease on same pane id to demote local control, got %#v", terminal)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleFollower {
		t.Fatalf("expected pane-1 demoted to follower under foreign lease, got %#v", binding)
	}
}

func TestRuntimeApplySessionLeasesRefreshesVisibleOwnerWhenForeignLeaseOwnerChanges(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}

	rt.ApplySessionLeases("view-local", []sessionstore.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-remote",
		PaneID:     "pane-9",
	}})
	visible := rt.Visible()
	if len(visible.Terminals) == 0 || visible.Terminals[0].OwnerPaneID != "pane-9" {
		t.Fatalf("expected visible runtime owner pane-9 after first foreign lease, got %#v", visible.Terminals)
	}

	rt.ApplySessionLeases("view-local", []sessionstore.LeaseInfo{{
		TerminalID: "term-1",
		ViewID:     "view-remote",
		PaneID:     "pane-10",
	}})
	visible = rt.Visible()
	if len(visible.Terminals) == 0 || visible.Terminals[0].OwnerPaneID != "pane-10" {
		t.Fatalf("expected visible runtime owner pane-10 after foreign lease owner change, got %#v", visible.Terminals)
	}
}

func TestRuntimeApplySessionLeasesPreservesFirstLocalOwnerWithoutLease(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}

	rt.ApplySessionLeases("view-local", nil)

	terminal := rt.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	if terminal.OwnerPaneID != "pane-1" || terminal.ControlPaneID != "pane-1" {
		t.Fatalf("expected first local attach to stay owner without lease, got %#v", terminal)
	}
	if terminal.RequiresExplicitOwner {
		t.Fatalf("expected first local attach not to require explicit owner, got %#v", terminal)
	}
	if binding := rt.Binding("pane-1"); binding == nil || binding.Role != BindingRoleOwner {
		t.Fatalf("expected pane-1 to remain owner, got %#v", binding)
	}
}

func TestRuntimeShouldAcquireTerminalOwnershipRequiresExplicitTakeoverAfterOwnerRelease(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.UnbindPane("pane-1", "term-1")

	if !rt.ShouldAcquireTerminalOwnership("term-1", TerminalOwnershipRequest{
		PaneID:           "pane-2",
		ExplicitTakeover: true,
	}) {
		t.Fatalf("expected explicit control reclaim to require ownership acquire, got %#v", rt.TerminalControlStatus("term-1"))
	}
}

func TestRuntimeResizeDecisionRequiresExplicitOwnerAfterOwnerRelease(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	rt.UnbindPane("pane-1", "term-1")

	decision := rt.ResizeDecision("pane-2", "term-1")
	if decision.Allowed {
		t.Fatalf("expected resize decision to deny pane-2 without explicit owner, got %#v", decision)
	}
	if !decision.Status.RequiresExplicitOwner {
		t.Fatalf("expected resize decision to surface explicit-owner requirement, got %#v", decision)
	}
}

func TestRuntimeAttachSnapshotInputAndResize(t *testing.T) {
	rt, ctx := newTestRuntime(t)

	created, err := rt.client.Create(ctx, protocol.CreateParams{
		Command: []string{"sh"},
		Name:    "demo",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	terminal, err := rt.AttachTerminal(ctx, "pane-1", created.TerminalID, "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal.Channel == 0 {
		t.Fatal("expected non-zero channel")
	}
	binding := rt.Binding("pane-1")
	if binding == nil || !binding.Connected {
		t.Fatal("expected connected pane binding")
	}

	snapshot, err := rt.LoadSnapshot(ctx, created.TerminalID, 0, 10)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if terminal.Snapshot == nil {
		t.Fatal("expected snapshot cached on terminal runtime")
	}

	if err := rt.SendInput(ctx, "pane-1", []byte("echo hi\n")); err != nil {
		t.Fatalf("send input: %v", err)
	}
	if err := rt.ResizePane(ctx, "pane-1", created.TerminalID, 100, 40); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}
}

func TestRuntimeAttachDoesNotRetainStructuralTerminalIDInBinding(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}

	if _, ok := reflect.TypeOf(PaneBinding{}).FieldByName("TerminalID"); ok {
		t.Fatal("expected PaneBinding to stop storing structural TerminalID")
	}
	binding := rt.Binding("pane-1")
	if binding == nil || !binding.Connected || binding.Channel != 11 {
		t.Fatalf("expected connected binding with channel only, got %#v", binding)
	}
}

func TestRuntimeReattachSamePaneCleansPreviousTerminalBindingState(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 11, Mode: "collaborator"}

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach term-1: %v", err)
	}

	client.attachResult = &protocol.AttachResult{Channel: 12, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-2", "collaborator"); err != nil {
		t.Fatalf("attach term-2: %v", err)
	}

	oldTerminal := rt.Registry().Get("term-1")
	if oldTerminal == nil {
		t.Fatal("expected old terminal runtime to remain present")
	}
	if oldTerminal.OwnerPaneID != "" {
		t.Fatalf("expected old terminal owner to be cleared, got %q", oldTerminal.OwnerPaneID)
	}
	if len(oldTerminal.BoundPaneIDs) != 0 {
		t.Fatalf("expected old terminal bound panes cleared, got %#v", oldTerminal.BoundPaneIDs)
	}

	newTerminal := rt.Registry().Get("term-2")
	if newTerminal == nil {
		t.Fatal("expected new terminal runtime")
	}
	if newTerminal.OwnerPaneID != "pane-1" {
		t.Fatalf("expected new terminal owner pane-1, got %q", newTerminal.OwnerPaneID)
	}
	if !reflect.DeepEqual(newTerminal.BoundPaneIDs, []string{"pane-1"}) {
		t.Fatalf("expected new terminal bound panes [pane-1], got %#v", newTerminal.BoundPaneIDs)
	}

	binding := rt.Binding("pane-1")
	if binding == nil || binding.Channel != 12 || binding.Role != BindingRoleOwner || !binding.Connected {
		t.Fatalf("expected binding reassigned to new channel/owner state, got %#v", binding)
	}
}

type fakeBridgeClient struct {
	mu                  sync.Mutex
	attachResult        *protocol.AttachResult
	listResult          *protocol.ListResult
	snapshotByTerminal  map[string]*protocol.Snapshot
	snapshotTerminalID  string
	snapshotHook        func()
	streams             map[uint16]chan protocol.StreamFrame
	streamSubscriptions map[uint16]int
	streamStops         map[uint16]int
	streamReadyCalls    map[uint16][]uint64
	inputCalls          []inputCall
	resizeCalls         []resizeCall
}

type incrementalCountingVTerm struct {
	*localvterm.VTerm
	partialCalls  atomic.Int32
	fullLoadCalls atomic.Int32
}

func (v *incrementalCountingVTerm) ApplyScreenUpdate(update localvterm.ScreenUpdate) bool {
	if v == nil || v.VTerm == nil {
		return false
	}
	v.partialCalls.Add(1)
	return v.VTerm.ApplyScreenUpdate(update)
}

func (v *incrementalCountingVTerm) LoadSnapshotWithMetadata(scrollback [][]localvterm.Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string, screen localvterm.ScreenData, screenTimestamps []time.Time, screenRowKinds []string, cursor localvterm.CursorState, modes localvterm.TerminalModes) {
	if v == nil || v.VTerm == nil {
		return
	}
	v.fullLoadCalls.Add(1)
	v.VTerm.LoadSnapshotWithMetadata(scrollback, scrollbackTimestamps, scrollbackRowKinds, screen, screenTimestamps, screenRowKinds, cursor, modes)
}

type countingSnapshotVTerm struct {
	*localvterm.VTerm
	screenContentCalls     atomic.Int32
	scrollbackContentCalls atomic.Int32
}

func (v *countingSnapshotVTerm) ScreenContent() localvterm.ScreenData {
	v.screenContentCalls.Add(1)
	return v.VTerm.ScreenContent()
}

func (v *countingSnapshotVTerm) ScrollbackContent() [][]localvterm.Cell {
	v.scrollbackContentCalls.Add(1)
	return v.VTerm.ScrollbackContent()
}

type inputCall struct {
	channel uint16
	data    []byte
}

type resizeCall struct {
	channel uint16
	cols    uint16
	rows    uint16
}

func newFakeBridgeClient() *fakeBridgeClient {
	return &fakeBridgeClient{
		listResult:          &protocol.ListResult{},
		snapshotByTerminal:  make(map[string]*protocol.Snapshot),
		streams:             make(map[uint16]chan protocol.StreamFrame),
		streamSubscriptions: make(map[uint16]int),
		streamStops:         make(map[uint16]int),
		streamReadyCalls:    make(map[uint16][]uint64),
	}
}

func (f *fakeBridgeClient) Close() error { return nil }

func (f *fakeBridgeClient) Create(context.Context, protocol.CreateParams) (*protocol.CreateResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) SetTags(context.Context, string, map[string]string) error { return nil }

func (f *fakeBridgeClient) SetMetadata(context.Context, string, string, map[string]string) error {
	return nil
}

func (f *fakeBridgeClient) List(context.Context) (*protocol.ListResult, error) {
	return f.listResult, nil
}

func (f *fakeBridgeClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeBridgeClient) Attach(context.Context, protocol.AttachParams) (*protocol.AttachResult, error) {
	if f.attachResult == nil {
		return nil, fmt.Errorf("attach result not configured")
	}
	return f.attachResult, nil
}

func (f *fakeBridgeClient) EnsureResize(_ context.Context, params protocol.EnsureResizeParams) (*protocol.EnsureResizeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizeCalls = append(f.resizeCalls, resizeCall{channel: params.Channel, cols: params.Cols, rows: params.Rows})
	return &protocol.EnsureResizeResult{
		ResizeControl: &protocol.ResizeControl{CanResize: true, Reason: protocol.ResizeControlReasonOwner},
		Size:          protocol.Size{Cols: params.Cols, Rows: params.Rows},
		Resized:       true,
	}, nil
}

func (f *fakeBridgeClient) Snapshot(_ context.Context, terminalID string, _ int, _ int) (*protocol.Snapshot, error) {
	f.mu.Lock()
	var snapshot *protocol.Snapshot
	f.snapshotTerminalID = terminalID
	hook := f.snapshotHook
	if candidate := f.snapshotByTerminal[terminalID]; candidate != nil {
		snapshot = cloneSnapshot(candidate)
	}
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if snapshot != nil {
		return snapshot, nil
	}
	return nil, fmt.Errorf("snapshot not configured")
}

func (f *fakeBridgeClient) GridViewport(_ context.Context, terminalID string, offset int, limit int, _ int) (*protocol.GridViewport, error) {
	f.mu.Lock()
	var snapshot *protocol.Snapshot
	if candidate := f.snapshotByTerminal[terminalID]; candidate != nil {
		snapshot = cloneSnapshot(candidate)
	}
	f.mu.Unlock()
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot not configured")
	}
	window := testSnapshotWindow(snapshot, offset, limit)
	if window == nil {
		return nil, nil
	}
	return &protocol.GridViewport{
		TerminalID:             terminalID,
		Size:                   window.Size,
		Rows:                   protocol.CloneCompactRows(window.Scrollback),
		ScrollbackOffset:       window.ScrollbackOffset,
		ScrollbackLimit:        limit,
		ScrollbackTotal:        window.ScrollbackTotal,
		ScrollbackLogicalTotal: window.ScrollbackLogicalTotal,
		ScrollbackHasMore:      window.ScrollbackHasMore,
		LoadedRows:             window.ScrollbackLoadedRows,
		HistoryGeneration:      window.HistoryGeneration,
		FirstRowID:             window.ScrollbackFirstRowID,
		LastRowID:              window.ScrollbackLastRowID,
		ScrollbackTimestamps:   append([]time.Time(nil), window.ScrollbackTimestamps...),
		ScrollbackRowKinds:     append([]string(nil), window.ScrollbackRowKinds...),
		ScrollbackWrapped:      append([]bool(nil), window.ScrollbackWrapped...),
		RowOwnership:           append([]string(nil), window.ScrollbackOwnership...),
		Timestamp:              window.Timestamp,
	}, nil
}

func (f *fakeBridgeClient) Input(_ context.Context, channel uint16, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputCalls = append(f.inputCalls, inputCall{channel: channel, data: append([]byte(nil), data...)})
	return nil
}

func (f *fakeBridgeClient) Resize(_ context.Context, channel uint16, cols, rows uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizeCalls = append(f.resizeCalls, resizeCall{channel: channel, cols: cols, rows: rows})
	return nil
}

func (f *fakeBridgeClient) StreamReady(_ context.Context, channel uint16, screenSequence uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streamReadyCalls[channel] = append(f.streamReadyCalls[channel], screenSequence)
	return nil
}

func (f *fakeBridgeClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stream := f.streams[channel]
	if stream == nil {
		stream = make(chan protocol.StreamFrame, 16)
		f.streams[channel] = stream
	}
	f.streamSubscriptions[channel]++
	return stream, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.streamStops[channel]++
	}
}

func (f *fakeBridgeClient) Kill(context.Context, string) error { return nil }

func (f *fakeBridgeClient) Remove(context.Context, string) error { return nil }

func (f *fakeBridgeClient) Restart(context.Context, string) error { return nil }

func (f *fakeBridgeClient) sendFrame(channel uint16, frame protocol.StreamFrame) {
	f.mu.Lock()
	stream := f.streams[channel]
	f.mu.Unlock()
	if stream == nil {
		panic("stream not initialized")
	}
	stream <- frame
}

func (f *fakeBridgeClient) closeStream(channel uint16) {
	f.mu.Lock()
	stream := f.streams[channel]
	delete(f.streams, channel)
	f.mu.Unlock()
	if stream != nil {
		close(stream)
	}
}

func (f *fakeBridgeClient) subscriptionCount(channel uint16) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamSubscriptions[channel]
}

func (f *fakeBridgeClient) stopCount(channel uint16) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamStops[channel]
}

func (f *fakeBridgeClient) streamReadyCount(channel uint16) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.streamReadyCalls[channel])
}

func (f *fakeBridgeClient) lastStreamReadySequence(channel uint16) uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := f.streamReadyCalls[channel]
	if len(calls) == 0 {
		return 0
	}
	return calls[len(calls)-1]
}

func snapshotWithLines(terminalID string, cols, rows uint16, lines []string) *protocol.Snapshot {
	grid := make([][]protocol.Cell, rows)
	for y := range rows {
		grid[y] = make([]protocol.Cell, cols)
		for x := range cols {
			grid[y][x] = protocol.Cell{Content: " ", Width: 1}
		}
	}
	for y, line := range lines {
		if y >= int(rows) {
			break
		}
		for x := 0; x < len(line) && x < int(cols); x++ {
			grid[y][x] = protocol.Cell{Content: string(line[x]), Width: 1}
		}
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: cols, Rows: rows},
		Screen:     protocol.ScreenData{Cells: grid},
		Cursor:     protocol.CursorState{Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
		Timestamp:  time.Now(),
	}
}

func markSnapshotScrollbackOwnership(snapshot *protocol.Snapshot, ownership string) {
	if snapshot == nil || len(snapshot.Scrollback) == 0 || ownership == "" {
		return
	}
	snapshot.ScrollbackOwnership = make([]string, len(snapshot.Scrollback))
	for i := range snapshot.ScrollbackOwnership {
		snapshot.ScrollbackOwnership[i] = ownership
	}
}

func markSnapshotScrollbackPersisted(snapshot *protocol.Snapshot) {
	markSnapshotScrollbackOwnership(snapshot, protocol.RowOwnershipPersisted)
}

func screenUpdateFrameForLines(t *testing.T, cols, rows uint16, lines ...string) protocol.StreamFrame {
	t.Helper()
	return protocol.StreamFrame{Type: wire.TypeScreenUpdate, Payload: screenUpdatePayloadForLines(t, cols, rows, lines...)}
}

func screenUpdatePayloadForLines(t *testing.T, cols, rows uint16, lines ...string) []byte {
	t.Helper()
	payload, err := protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		FullReplace: true,
		Size:        protocol.Size{Cols: cols, Rows: rows},
		Screen:      snapshotWithLines("term-1", cols, rows, lines).Screen,
		Cursor:      protocol.CursorState{Visible: true},
		Modes:       protocol.TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode screen update: %v", err)
	}
	return payload
}

func cloneSnapshot(snapshot *protocol.Snapshot) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Screen.Cells = make([][]protocol.Cell, len(snapshot.Screen.Cells))
	for y, row := range snapshot.Screen.Cells {
		cloned.Screen.Cells[y] = append([]protocol.Cell(nil), row...)
	}
	cloned.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback)
	cloned.ScreenOwnership = append([]string(nil), snapshot.ScreenOwnership...)
	cloned.ScrollbackOwnership = append([]string(nil), snapshot.ScrollbackOwnership...)
	return &cloned
}

func testSnapshotWindow(snapshot *protocol.Snapshot, offset int, limit int) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := cloneSnapshot(snapshot)
	if cloned == nil || limit <= 0 {
		return cloned
	}
	if offset < 0 {
		offset = 0
	}
	total := len(snapshot.Scrollback)
	if offset > total {
		offset = total
	}
	end := total - offset
	if end < 0 {
		end = 0
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	cloned.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback[start:end])
	if len(snapshot.ScrollbackTimestamps) >= end {
		cloned.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps[start:end]...)
	} else {
		cloned.ScrollbackTimestamps = nil
	}
	if len(snapshot.ScrollbackRowKinds) >= end {
		cloned.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds[start:end]...)
	} else {
		cloned.ScrollbackRowKinds = nil
	}
	if len(snapshot.ScrollbackWrapped) >= end {
		cloned.ScrollbackWrapped = append([]bool(nil), snapshot.ScrollbackWrapped[start:end]...)
	} else {
		cloned.ScrollbackWrapped = nil
	}
	if len(snapshot.ScrollbackOwnership) >= end {
		cloned.ScrollbackOwnership = append([]string(nil), snapshot.ScrollbackOwnership[start:end]...)
	} else {
		cloned.ScrollbackOwnership = nil
	}
	cloned.ScrollbackOffset = offset
	cloned.ScrollbackTotal = total
	if snapshot.ScrollbackLogicalTotal > 0 {
		cloned.ScrollbackLogicalTotal = snapshot.ScrollbackLogicalTotal
	} else {
		cloned.ScrollbackLogicalTotal = total
	}
	cloned.ScrollbackHasMore = start > 0
	cloned.ScrollbackLoadedRows = offset + len(cloned.Scrollback)
	if total > 0 {
		cloned.HistoryGeneration = snapshot.HistoryGeneration
	}
	if baseRowID, ok := testSnapshotRowIDBase(snapshot, total); ok && len(cloned.Scrollback) > 0 {
		cloned.ScrollbackFirstRowID = baseRowID + uint64(start)
		cloned.ScrollbackLastRowID = baseRowID + uint64(end-1)
	} else {
		cloned.ScrollbackFirstRowID = 0
		cloned.ScrollbackLastRowID = 0
	}
	return cloned
}

func testSnapshotRowIDBase(snapshot *protocol.Snapshot, total int) (uint64, bool) {
	if snapshot == nil || total <= 0 {
		return 0, false
	}
	if snapshot.HistoryGeneration != 0 && snapshot.ScrollbackLastRowID >= snapshot.ScrollbackFirstRowID {
		return snapshot.ScrollbackFirstRowID, true
	}
	if snapshot.ScrollbackLastRowID == 0 {
		return 0, false
	}
	base := snapshot.ScrollbackLastRowID + 1 - uint64(total)
	return base, true
}

func testCloneProtocolRows(rows [][]protocol.Cell) [][]protocol.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]protocol.Cell, len(rows))
	for i, row := range rows {
		out[i] = append([]protocol.Cell(nil), row...)
	}
	return out
}

func protocolRowFromString(value string) []protocol.Cell {
	row := make([]protocol.Cell, 0, len(value))
	for _, r := range value {
		row = append(row, protocol.Cell{Content: string(r), Width: 1})
	}
	return row
}

func rowText(row []protocol.Cell) string {
	var buf bytes.Buffer
	for _, cell := range row {
		buf.WriteString(cell.Content)
	}
	return strings.TrimRight(buf.String(), " ")
}

func rowTextRaw(row []protocol.Cell) string {
	var buf bytes.Buffer
	for _, cell := range row {
		buf.WriteString(cell.Content)
	}
	return buf.String()
}

func vtermRowTextRaw(row []localvterm.Cell) string {
	var buf bytes.Buffer
	for _, cell := range row {
		buf.WriteString(cell.Content)
	}
	return buf.String()
}

func vtermScreenRowTextRaw(vt VTermLike, row int) string {
	if source, ok := vt.(interface {
		ScreenRowView(int) []localvterm.Cell
	}); ok {
		return vtermRowTextRaw(source.ScreenRowView(row))
	}
	screen := vt.ScreenContent()
	if row < 0 || row >= len(screen.Cells) {
		return ""
	}
	return vtermRowTextRaw(screen.Cells[row])
}

func compactRowText(row protocol.CompactRow) string {
	return rowText(row.DecodeCells())
}

func snapshotContains(snapshot *protocol.Snapshot, want string) bool {
	if snapshot == nil {
		return false
	}
	for _, row := range snapshot.Screen.Cells {
		var buf bytes.Buffer
		for _, cell := range row {
			buf.WriteString(cell.Content)
		}
		if bytes.Contains(buf.Bytes(), []byte(want)) {
			return true
		}
	}
	return false
}

func vtermContains(vt VTermLike, want string) bool {
	if vt == nil {
		return false
	}
	screen := vt.ScreenContent()
	for _, row := range screen.Cells {
		var buf bytes.Buffer
		for _, cell := range row {
			buf.WriteString(cell.Content)
		}
		if bytes.Contains(buf.Bytes(), []byte(want)) {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestRuntimeStartStreamReconnectsAfterChannelClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	rt := New(client)

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, screenUpdateFrameForLines(t, 80, 24, "one"))
	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "one")
	})

	client.closeStream(9)

	waitFor(t, func() bool {
		return client.subscriptionCount(9) >= 2
	})

	client.sendFrame(9, screenUpdateFrameForLines(t, 80, 24, "two"))
	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "two")
	})

	stored := rt.Registry().Get("term-1")
	if stored == nil {
		t.Fatal("expected terminal runtime after reconnect")
	}
	if stored.Stream.RetryCount != 0 {
		t.Fatalf("expected retry count reset after successful frame, got %d", stored.Stream.RetryCount)
	}
	if !stored.Stream.Active {
		t.Fatal("expected stream to be active after reconnect")
	}
}

func TestRuntimeReattachIgnoresLateFramesFromPreviousStreamGeneration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.listResult = &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
		ID:    "term-1",
		Name:  "shared",
		State: "running",
	}}}
	rt := New(client)

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start initial stream: %v", err)
	}

	client.sendFrame(9, screenUpdateFrameForLines(t, 80, 24, "seed"))
	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "seed")
	})

	client.attachResult = &protocol.AttachResult{Channel: 10, Mode: "collaborator"}
	if _, err := rt.AttachTerminal(ctx, "pane-2", "term-1", "collaborator"); err != nil {
		t.Fatalf("reattach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start replacement stream: %v", err)
	}

	waitFor(t, func() bool { return client.stopCount(9) > 0 })
	waitFor(t, func() bool { return client.subscriptionCount(10) > 0 })

	client.sendFrame(9, screenUpdateFrameForLines(t, 80, 24, "stale"))
	client.sendFrame(10, screenUpdateFrameForLines(t, 80, 24, "fresh"))

	waitFor(t, func() bool {
		stored := rt.Registry().Get("term-1")
		return stored != nil && vtermContains(stored.VTerm, "fresh")
	})

	stored := rt.Registry().Get("term-1")
	if stored == nil {
		t.Fatal("expected terminal runtime after reattach")
	}
	if vtermContains(stored.VTerm, "stale") {
		t.Fatalf("expected stale frame from previous stream generation to be ignored, got %#v", stored.VTerm.ScreenContent())
	}
}

func TestRuntimeStreamResizeFrameRefreshesSnapshotGeometryDuringBootstrap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	client.snapshotByTerminal["term-1"] = snapshotWithLines("term-1", 80, 24, []string{"initial"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatal("expected terminal with VTerm")
	}
	cols, rows := terminal.VTerm.Size()
	if cols != 80 || rows != 24 {
		t.Fatalf("expected initial VTerm size 80x24, got %dx%d", cols, rows)
	}

	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	// Simulate server sending a resize frame (as if the owner resized the PTY)
	client.sendFrame(9, protocol.StreamFrame{
		Type:    wire.TypeResize,
		Payload: wire.EncodeResizePayload(120, 40),
	})

	waitFor(t, func() bool {
		c, r := terminal.VTerm.Size()
		return c == 120 && r == 40
	})

	cols, rows = terminal.VTerm.Size()
	if cols != 120 || rows != 40 {
		t.Fatalf("expected VTerm resized to 120x40 after resize frame, got %dx%d", cols, rows)
	}

	waitFor(t, func() bool {
		return terminal.Snapshot != nil && terminal.Snapshot.Size.Cols == 120 && terminal.Snapshot.Size.Rows == 40
	})
	if !snapshotContains(terminal.Snapshot, "initial") {
		t.Fatalf("expected bootstrap resize to preserve provisional snapshot content, got %#v", terminal.Snapshot)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: wire.TypeBootstrapDone})
	waitFor(t, func() bool {
		return terminal.Snapshot != nil && terminal.Snapshot.Size.Cols == 120 && terminal.Snapshot.Size.Rows == 40
	})

	// Subsequent output should be processed correctly at the new size.
	client.sendFrame(9, screenUpdateFrameForLines(t, 120, 40, "after-resize"))
	waitFor(t, func() bool {
		return vtermContains(terminal.VTerm, "after-resize")
	})
}

func TestRuntimeClosedStreamStopsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 9, Mode: "collaborator"}
	rt := New(client)

	if _, err := rt.AttachTerminal(ctx, "pane-1", "term-1", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, "term-1"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	client.sendFrame(9, protocol.StreamFrame{Type: wire.TypeClosed, Payload: wire.EncodeClosedPayload(0)})

	waitFor(t, func() bool {
		terminal := rt.Registry().Get("term-1")
		return terminal != nil && terminal.State == "exited"
	})
	waitFor(t, func() bool {
		return client.stopCount(9) == 1
	})
}
