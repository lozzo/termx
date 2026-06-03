package termx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-core/fanout"
	ptymgr "github.com/lozzow/termx/termx-core/pty"
	"github.com/lozzow/termx/termx-shared/gridtrace"
	"github.com/lozzow/termx/termx-shared/perftrace"
	"github.com/lozzow/termx/termx-shared/terminalmeta"
	"github.com/lozzow/termx/termx-vterm/vterm"
)

var terminalIDCounter atomic.Uint64

const (
	terminalPTYReadBufferBytes      = 512 * 1024
	terminalPTYParserQueueBytes     = 2 * 1024 * 1024
	terminalPTYParserChunkMaxBytes  = 1024 * 1024
	terminalPTYParserFlushDelay     = 2 * time.Millisecond
	terminalPTYParserCloseWait      = 10 * time.Second
	terminalInlineDamageMaxBytes    = 32 * 1024
	terminalAlternateScrollbackRows = 10000
)

type terminalConfig struct {
	ID                 string
	Name               string
	Command            []string
	Tags               map[string]string
	Size               Size
	Dir                string
	Env                []string
	ScrollbackSize     int
	ScrollbackMaxBytes int64
	ScrollbackMaxAge   time.Duration
	KeepAfterExit      time.Duration
	GridRoot           string
	Logger             *slog.Logger
	RemoveFunc         func(string, string)
	UpdateFunc         func()
}

func (cfg terminalConfig) gridRetentionPolicy() terminalGridRetentionPolicy {
	policy := terminalGridRetentionPolicy{}
	if cfg.ScrollbackSize > 0 {
		policy.maxLogicalLines = cfg.ScrollbackSize
		// Keep an internal byte guard roughly proportional to the logical-line
		// budget so pathological huge rows cannot retain unbounded page data.
		policy.maxRetainedBytes = int64(cfg.ScrollbackSize) * 16 * 1024
	}
	if cfg.ScrollbackMaxBytes > 0 {
		policy.maxRetainedBytes = cfg.ScrollbackMaxBytes
	}
	if cfg.ScrollbackMaxAge > 0 {
		policy.maxAge = cfg.ScrollbackMaxAge
	}
	return policy
}

type Terminal struct {
	events       *EventBus
	pty          *ptymgr.PTY
	vterm        *vterm.VTerm
	stream       *fanout.Fanout
	grid         *terminalGridStore
	gridAppender *terminalGridAppender
	logger       *slog.Logger

	mu             sync.RWMutex
	id             string
	name           string
	command        []string
	tags           map[string]string
	size           Size
	dir            string
	cwd            string
	env            []string
	scrollbackSize int
	state          TerminalState
	createdAt      time.Time
	exitCode       *int
	title          string
	keepAfterExit  time.Duration
	removeFunc     func(string, string)
	updateFunc     func()
	removed        bool
	processEpoch   uint64

	// streamMu serializes VTerm updates, bootstrap capture, broadcasts and
	// resize/close notifications so subscribers can replay a consistent screen
	// state before switching to live frames.
	streamMu           sync.Mutex
	screenRevision     uint64
	alternateGrid      terminalAlternateGrid
	primaryLiveTail    terminalPrimaryLiveTail
	liveLineMigrations map[uint64]uint64

	// This cache holds deep-copied metadata snapshots so frequent read paths do not
	// have to rebuild command/tag payloads for every request.
	listInfoCache   *TerminalInfo
	metadataVersion uint64

	attachMu         sync.Mutex
	attachments      map[string]AttachInfo
	resizeOwnerEpoch uint64

	done     chan struct{}
	readDone chan struct{}
}

func newTerminal(ctx context.Context, events *EventBus, cfg terminalConfig) (*Terminal, error) {
	p, vt, err := spawnTerminalProcess(cfg)
	if err != nil {
		return nil, err
	}
	grid, err := newTerminalGridStore(cfg.GridRoot, cfg.ID)
	if err != nil {
		_ = p.Close()
		return nil, err
	}
	t := &Terminal{
		events:         events,
		pty:            p,
		vterm:          vt,
		stream:         fanout.New(),
		grid:           grid,
		gridAppender:   newTerminalGridAppender(grid, cfg.ID, cfg.gridRetentionPolicy(), cfg.Logger),
		logger:         cfg.Logger,
		id:             cfg.ID,
		name:           cfg.Name,
		command:        append([]string(nil), cfg.Command...),
		tags:           copyTags(cfg.Tags),
		size:           cfg.Size,
		dir:            cfg.Dir,
		cwd:            cfg.Dir,
		env:            append([]string(nil), cfg.Env...),
		scrollbackSize: cfg.ScrollbackSize,
		state:          StateRunning,
		createdAt:      time.Now().UTC(),
		keepAfterExit:  cfg.KeepAfterExit,
		removeFunc:     cfg.RemoveFunc,
		updateFunc:     cfg.UpdateFunc,
		attachments:    make(map[string]AttachInfo),
		done:           make(chan struct{}),
		readDone:       make(chan struct{}),
		processEpoch:   1,
	}
	t.installVTermHandlers()

	t.startProcessLoops()
	return t, nil
}

func spawnTerminalProcess(cfg terminalConfig) (*ptymgr.PTY, *vterm.VTerm, error) {
	p, err := ptymgr.Spawn(ptymgr.SpawnOptions{
		Command:    cfg.Command,
		Dir:        cfg.Dir,
		Env:        cfg.Env,
		TerminalID: cfg.ID,
		Size:       ptymgr.Size{Cols: cfg.Size.Cols, Rows: cfg.Size.Rows},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrSpawnFailed, err)
	}
	vt := vterm.New(int(cfg.Size.Cols), int(cfg.Size.Rows), liveScrollbackRows(cfg.ScrollbackSize), func(data []byte) {
		// Forward emulator responses (e.g. DSR cursor position) to the PTY
		// so the child process receives them.
		_, _ = p.Write(data)
	})
	vt.DisableEmulatorScrollback()
	return p, vt, nil
}

func (t *Terminal) installVTermHandlers() {
	if t == nil || t.vterm == nil {
		return
	}
	t.vterm.SetTitleHandler(func(title string) {
		t.mu.Lock()
		t.title = title
		t.mu.Unlock()
		if t.updateFunc != nil {
			t.updateFunc()
		}
	})
	t.vterm.SetWorkingDirectoryHandler(func(path string) {
		cwd := normalizeReportedWorkingDirectory(path)
		if cwd == "" {
			return
		}
		t.mu.Lock()
		if t.cwd == cwd {
			t.mu.Unlock()
			return
		}
		t.cwd = cwd
		t.invalidateProtocolInfoCacheLocked()
		t.mu.Unlock()
		if t.updateFunc != nil {
			t.updateFunc()
		}
	})
}

func (t *Terminal) ID() string {
	return t.id
}

func (t *Terminal) Name() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.name
}

func (t *Terminal) Info() *TerminalInfo {
	info, _ := t.listInfoSnapshot(ListOptions{})
	// Return a distinct top-level struct so callers cannot mutate cached scalar
	// fields, while nested metadata continues to reuse the immutable snapshot.
	snapshot := *info
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if cwd := t.CurrentWorkingDirectory(ctx); cwd != "" {
		snapshot.LiveCWD = cwd
		snapshot.CWD = cwd
	}
	return &snapshot
}

func (t *Terminal) Size() Size {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.size
}

func (t *Terminal) RunningSize() (Size, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.size, t.state != StateExited
}

func (t *Terminal) CurrentWorkingDirectory(ctx context.Context) string {
	if t == nil {
		return ""
	}
	t.mu.RLock()
	state := t.state
	p := t.pty
	t.mu.RUnlock()
	if state != StateRunning || p == nil {
		return ""
	}
	cwd, err := p.CurrentWorkingDirectory(ctx)
	if err != nil {
		return ""
	}
	return normalizeReportedWorkingDirectory(cwd)
}

func (t *Terminal) SizeLocked() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return terminalmeta.SizeLocked(t.tags)
}

func (t *Terminal) Done() <-chan struct{} {
	return t.done
}

func (t *Terminal) Subscribe(ctx context.Context) <-chan StreamMessage {
	return t.subscribe(ctx, t.screenSnapshotFallbackMessage)
}

func (t *Terminal) SubscribeLatest(ctx context.Context) <-chan StreamMessage {
	return t.subscribeLatest(ctx)
}

func (t *Terminal) subscribe(ctx context.Context, fallback func() StreamMessage) <-chan StreamMessage {
	t.streamMu.Lock()
	bootstrap := t.bootstrapMessagesLocked()
	t.mu.RLock()
	state := t.state
	exitCode := copyIntPtr(t.exitCode)
	t.mu.RUnlock()
	if state == StateExited {
		t.streamMu.Unlock()
		ch := make(chan StreamMessage, len(bootstrap)+1)
		go func() {
			defer close(ch)
			for _, msg := range bootstrap {
				select {
				case <-ctx.Done():
					return
				case ch <- msg:
				}
			}
			select {
			case <-ctx.Done():
				return
			case ch <- StreamMessage{Type: StreamClosed, ExitCode: exitCode}:
			}
		}()
		return ch
	}

	src := t.stream.Subscribe(ctx)
	t.streamMu.Unlock()
	dst := make(chan StreamMessage, 1)
	go func() {
		defer close(dst)
		if fallback == nil {
			for _, msg := range bootstrap {
				if !sendTerminalStreamMessage(ctx, dst, cloneTerminalStreamMessage(msg)) {
					return
				}
			}
			forwardTerminalStreamMessagesImmediate(ctx, src, dst, nil)
			return
		}
		for _, msg := range bootstrap {
			select {
			case <-ctx.Done():
				return
			case dst <- cloneTerminalStreamMessage(msg):
			}
		}
		forwardTerminalStreamMessagesImmediate(ctx, src, dst, fallback)
	}()
	return dst
}

func (t *Terminal) subscribeLatest(ctx context.Context) <-chan StreamMessage {
	t.streamMu.Lock()
	bootstrap := t.bootstrapMessagesLocked()
	t.mu.RLock()
	state := t.state
	exitCode := copyIntPtr(t.exitCode)
	t.mu.RUnlock()
	if state == StateExited {
		t.streamMu.Unlock()
		ch := make(chan StreamMessage, len(bootstrap)+1)
		go func() {
			defer close(ch)
			for _, msg := range bootstrap {
				if !sendTerminalStreamMessage(ctx, ch, cloneTerminalStreamMessage(msg)) {
					return
				}
			}
			_ = sendTerminalStreamMessage(ctx, ch, StreamMessage{Type: StreamClosed, ExitCode: exitCode})
		}()
		return ch
	}

	src := t.stream.Subscribe(ctx)
	t.streamMu.Unlock()
	dst := make(chan StreamMessage, 1)
	go func() {
		defer close(dst)
		for _, msg := range bootstrap {
			if !sendTerminalStreamMessage(ctx, dst, cloneTerminalStreamMessage(msg)) {
				return
			}
		}
		forwardTerminalStreamMessagesImmediate(ctx, src, dst, t.screenInvalidationMessage)
	}()
	return dst
}

func forwardTerminalStreamMessagesImmediate(ctx context.Context, src <-chan fanout.StreamMessage, dst chan<- StreamMessage, fallback func() StreamMessage) {
	var pending *fanout.StreamMessage
	for {
		var msg fanout.StreamMessage
		if pending != nil {
			msg = *pending
			pending = nil
		} else {
			var ok bool
			msg, ok = <-src
			if !ok {
				return
			}
		}
		if msg.Type == fanout.StreamScreenUpdate {
			var collapsed int
			msg, pending, collapsed = collapseTerminalStreamScreenUpdates(src, msg)
			if collapsed > 0 && fallback != nil {
				msg = fanout.StreamMessage{Type: fanout.StreamScreenUpdate, Revision: msg.Revision}
			}
		}
		if !sendFanoutStreamMessage(ctx, dst, msg, fallback) {
			return
		}
	}
}

func collapseTerminalStreamScreenUpdates(src <-chan fanout.StreamMessage, first fanout.StreamMessage) (fanout.StreamMessage, *fanout.StreamMessage, int) {
	msg := first
	collapsed := 0
	for {
		select {
		case next, ok := <-src:
			if !ok {
				if collapsed > 0 {
					perftrace.Count("terminal.stream.screen_update.coalesced", collapsed)
				}
				return msg, nil, collapsed
			}
			if next.Type == fanout.StreamScreenUpdate {
				msg = next
				collapsed++
				continue
			}
			if collapsed > 0 {
				perftrace.Count("terminal.stream.screen_update.coalesced", collapsed)
			}
			pending := next
			return msg, &pending, collapsed
		default:
			if collapsed > 0 {
				perftrace.Count("terminal.stream.screen_update.coalesced", collapsed)
			}
			return msg, nil, collapsed
		}
	}
}

func sendFanoutStreamMessage(ctx context.Context, dst chan<- StreamMessage, msg fanout.StreamMessage, fallback func() StreamMessage) bool {
	if msg.Type == fanout.StreamSyncLost && fallback != nil {
		return sendTerminalStreamMessage(ctx, dst, fallback())
	}
	if msg.Type == fanout.StreamScreenUpdate && len(msg.Payload) == 0 && fallback != nil {
		return sendTerminalStreamMessage(ctx, dst, fallback())
	}
	return sendTerminalStreamMessage(ctx, dst, cloneFanoutStreamMessage(msg))
}

func sendTerminalStreamMessage(ctx context.Context, dst chan<- StreamMessage, msg StreamMessage) bool {
	select {
	case <-ctx.Done():
		return false
	case dst <- msg:
		return true
	}
}

func (t *Terminal) WriteInput(data []byte) error {
	finish := perftrace.Measure("terminal.input.write")
	defer func() {
		finish(len(data))
	}()
	t.mu.RLock()
	if t.state == StateExited {
		t.mu.RUnlock()
		return ErrTerminalExited
	}
	t.mu.RUnlock()
	_, err := t.pty.Write(data)
	return err
}

func (t *Terminal) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return fmt.Errorf("termx: invalid terminal size %dx%d", cols, rows)
	}
	t.mu.Lock()
	if t.state == StateExited {
		t.mu.Unlock()
		return ErrTerminalExited
	}
	old := t.size
	if old.Cols == cols && old.Rows == rows {
		t.mu.Unlock()
		return nil
	}
	if terminalmeta.SizeLocked(t.tags) {
		t.mu.Unlock()
		return fmt.Errorf("%w: terminal %q size is locked", ErrPermissionDenied, t.id)
	}
	newSize := Size{Cols: cols, Rows: rows}
	t.mu.Unlock()

	t.streamMu.Lock()
	gridRowsBefore := 0
	if t.grid != nil {
		gridRowsBefore = t.grid.RowCount()
	}
	if err := t.pty.Resize(cols, rows); err != nil {
		t.streamMu.Unlock()
		return err
	}
	damage := t.vterm.ResizeWithDamage(int(cols), int(rows))
	t.captureAlternateDamageLocked(damage)
	t.appendGridFromDamageLocked(damage)
	if int(cols) > int(old.Cols) || int(rows) > int(old.Rows) {
		if err := t.reclaimPrimaryLiveTailForGrowResizeLocked(int(cols)); err != nil && t.logger != nil {
			t.logger.Warn("termx terminal grow resize live-tail reclaim failed", "terminal_id", t.id, "error", err)
		}
	}
	gridRowsAfterAppend := gridRowsBefore
	if t.grid != nil {
		gridRowsAfterAppend = t.grid.RowCount()
	}
	revision := t.bumpScreenRevisionLocked()
	payload, ok := t.screenUpdatePayloadFromDamageLocked(damage)
	t.mu.Lock()
	t.size = newSize
	t.invalidateProtocolInfoCacheLocked()
	t.mu.Unlock()
	if gridtrace.Enabled() {
		gridtrace.Log(
			"core.resize.summary",
			"terminal_id", t.id,
			"old_cols", old.Cols,
			"old_rows", old.Rows,
			"new_cols", cols,
			"new_rows", rows,
			"scrollback_append_rows", len(damage.ScrollbackAppend),
			"grid_rows_before", gridRowsBefore,
			"grid_rows_after_append", gridRowsAfterAppend,
			"full_replace_reason", damage.FullReplaceReason,
		)
	}
	t.broadcastResizeLocked(t.stream, cols, rows)
	if ok {
		t.stream.BroadcastMessage(fanout.StreamMessage{Type: fanout.StreamScreenUpdate, Payload: payload, Revision: revision})
	} else {
		t.stream.BroadcastMessage(fanout.StreamMessage{Type: fanout.StreamScreenUpdate, Revision: revision})
	}
	t.streamMu.Unlock()
	t.events.Publish(Event{
		Type:       EventTerminalResized,
		TerminalID: t.id,
		Timestamp:  time.Now().UTC(),
		Resized: &TerminalResizedData{
			OldSize: old,
			NewSize: Size{Cols: cols, Rows: rows},
		},
	})
	return nil
}

func (t *Terminal) Kill() error {
	return t.pty.Kill()
}

func (t *Terminal) Close() error {
	if t == nil {
		return nil
	}
	var err error
	if t.pty != nil {
		err = t.pty.Close()
	}
	if t.gridAppender != nil {
		t.gridAppender.close()
	}
	if gridErr := closeTerminalGridStore(t.grid); gridErr != nil && err == nil {
		err = gridErr
	}
	return err
}

func (t *Terminal) Restart() error {
	t.mu.Lock()
	if t.state != StateExited {
		t.mu.Unlock()
		return ErrTerminalNotExited
	}
	liveTail := t.primaryLiveTail.clone()
	preservedScrollback, preservedScrollbackTimestamps, preservedScrollbackRowKinds, preservedScrollbackWrapped := restartPreservedRows(t, liveTail)
	grid := t.grid
	gridAppender := t.gridAppender
	cfg := terminalConfig{
		ID:             t.id,
		Command:        append([]string(nil), t.command...),
		Dir:            t.dir,
		Env:            append([]string(nil), t.env...),
		Size:           t.size,
		ScrollbackSize: t.scrollbackSize,
	}
	currentEpoch := t.processEpoch
	t.mu.Unlock()

	if gridAppender != nil {
		gridAppender.flush()
	}
	if grid != nil {
		if err := grid.appendRows(terminalGridRowsFromPreserved(preservedScrollback, preservedScrollbackTimestamps, preservedScrollbackRowKinds, preservedScrollbackWrapped)); err != nil {
			return err
		}
	}

	p, vt, err := spawnTerminalProcess(cfg)
	if err != nil {
		return err
	}
	seedRestartScrollback(vt, preservedScrollback, preservedScrollbackTimestamps, preservedScrollbackRowKinds, preservedScrollbackWrapped)

	t.mu.Lock()
	if t.removed {
		t.mu.Unlock()
		_ = p.Close()
		return ErrNotFound
	}
	if t.state != StateExited || t.processEpoch != currentEpoch {
		t.mu.Unlock()
		_ = p.Close()
		return ErrTerminalNotExited
	}
	oldState := t.state
	t.pty = p
	t.vterm = vt
	t.stream = fanout.New()
	t.state = StateRunning
	t.exitCode = nil
	t.done = make(chan struct{})
	t.readDone = make(chan struct{})
	t.processEpoch++
	t.invalidateProtocolInfoCacheLocked()
	t.mu.Unlock()
	t.installVTermHandlers()

	if t.updateFunc != nil {
		t.updateFunc()
	}
	t.events.Publish(Event{
		Type:       EventTerminalStateChanged,
		TerminalID: t.id,
		Timestamp:  time.Now().UTC(),
		StateChanged: &TerminalStateChangedData{
			OldState: oldState,
			NewState: StateRunning,
		},
	})
	t.startProcessLoops()
	return nil
}

func restartPreservedRows(t *Terminal, liveTail terminalPrimaryLiveTail) ([][]vterm.Cell, []time.Time, []string, []bool) {
	if t == nil || t.vterm == nil {
		return nil, nil, nil, nil
	}
	restartAt := time.Now().UTC()
	rows := t.primaryLiveTailRowsForExit(liveTail)
	if len(rows) == 0 {
		return appendRestartMarker(nil, nil, nil, nil, restartAt)
	}
	out := terminalGridRowsToCellsRows(rows)
	timestamps := terminalGridRowsTimestamps(rows)
	rowKinds := terminalGridRowsKinds(rows)
	wrapped := terminalGridRowsWrapped(rows)
	return appendRestartMarker(out, timestamps, rowKinds, wrapped, restartAt)
}

func appendRestartMarker(rows [][]vterm.Cell, timestamps []time.Time, rowKinds []string, wrapped []bool, restartAt time.Time) ([][]vterm.Cell, []time.Time, []string, []bool) {
	rows = append(rows, nil)
	timestamps = append(timestamps, restartAt)
	rowKinds = append(rowKinds, SnapshotRowKindRestart)
	wrapped = append(wrapped, false)
	return rows, timestamps, rowKinds, wrapped
}

func terminalGridRowsFromPreserved(rows [][]vterm.Cell, timestamps []time.Time, rowKinds []string, wrapped []bool) []terminalGridRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]terminalGridRow, 0, len(rows))
	for i, row := range rows {
		out = append(out, terminalGridRow{
			cells:     row,
			timestamp: timeAt(timestamps, i),
			rowKind:   stringAt(rowKinds, i),
			wrapped:   boolAt(wrapped, i),
		})
	}
	return out
}

func seedRestartScrollback(vt *vterm.VTerm, scrollback [][]vterm.Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string, scrollbackWrapped []bool) {
	if vt == nil || len(scrollback) == 0 {
		return
	}
	screen := vt.ScreenContent()
	cols, rows := vt.Size()
	vt.LoadSizedSnapshotWithExtendedMetadata(cols, rows, scrollback, scrollbackTimestamps, scrollbackRowKinds, scrollbackWrapped, screen, nil, nil, nil, vt.CursorState(), vt.Modes())
}

func trimBoolSlice(values []bool, size int) []bool {
	if size <= 0 {
		return nil
	}
	if size > len(values) {
		size = len(values)
	}
	return append([]bool(nil), values[:size]...)
}

func trimTrailingBlankVTermRows(rows [][]vterm.Cell) [][]vterm.Cell {
	last := len(rows)
	for last > 0 && isBlankVTermRow(rows[last-1]) {
		last--
	}
	return rows[:last]
}

func isBlankVTermRow(row []vterm.Cell) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell.Content) != "" {
			return false
		}
		if cell.Style != (vterm.CellStyle{}) {
			return false
		}
		if cell.LinkURL != "" || cell.LinkParams != "" {
			return false
		}
	}
	return true
}

func (t *Terminal) MarkRemoved() {
	t.mu.Lock()
	t.removed = true
	t.mu.Unlock()
}

func (t *Terminal) Snapshot(offset, limit int) *Snapshot {
	t.flushGridAppender()
	if offset < 0 {
		offset = 0
	}

	t.mu.RLock()
	size := t.size
	id := t.id
	liveTail := t.primaryLiveTail.clone()
	liveTailRows := liveTail.rows()
	t.mu.RUnlock()
	screenTimestamps := t.vterm.ScreenTimestamps()
	screenRowKinds := t.vterm.ScreenRowKinds()
	screenWrapped := t.vterm.ScreenWrapped()
	screenContent := t.vterm.ScreenContent()
	screenData := convertScreenData(screenContent)
	modes := t.vterm.Modes()
	if modes.AlternateScreen || screenContent.IsAlternateScreen {
		outScrollback, outScrollbackTimestamps, outScrollbackRowKinds, outScrollbackWrapped, scrollbackTotal, scrollbackHasMore := t.alternateGrid.viewport(offset, limit)
		return &Snapshot{
			TerminalID:           id,
			Size:                 size,
			Screen:               screenData,
			Scrollback:           outScrollback,
			ScrollbackOffset:     offset,
			ScrollbackTotal:      scrollbackTotal,
			ScrollbackHasMore:    scrollbackHasMore,
			ScreenTimestamps:     cloneTimeSlice(screenTimestamps),
			ScrollbackTimestamps: outScrollbackTimestamps,
			ScreenRowKinds:       cloneStringSlice(screenRowKinds),
			ScrollbackRowKinds:   outScrollbackRowKinds,
			ScreenWrapped:        cloneBoolSlice(screenWrapped),
			ScrollbackWrapped:    outScrollbackWrapped,
			ScreenOwnership:      repeatedString(RowOwnershipScreen, len(screenData.Cells)),
			Cursor:               convertCursorState(t.vterm.CursorState()),
			Modes:                convertModes(modes),
			Timestamp:            time.Now().UTC(),
		}
	}

	var (
		outScrollback           [][]Cell
		outScrollbackTimestamps []time.Time
		outScrollbackRowKinds   []string
		outScrollbackWrapped    []bool
		outScrollbackOwnership  []string
		scrollbackTotal         int
		scrollbackLogicalTotal  int
		scrollbackHasMore       bool
		scrollbackLoadedRows    int
		historyGeneration       uint64
		scrollbackFirstRowID    uint64
		scrollbackLastRowID     uint64
		usedGrid                bool
	)
	if t.grid != nil && limit > 0 {
		gridViewport, err := t.combinedGridViewport(offset, limit, int(size.Cols), liveTail)
		if err != nil {
			if t.logger != nil {
				t.logger.Warn("termx terminal grid snapshot failed", "terminal_id", id, "error", err)
			}
		} else if gridViewport.TotalRows > 0 {
			outScrollback = convertRows(gridViewport.Rows)
			outScrollbackTimestamps = cloneTimeSlice(gridViewport.Timestamps)
			outScrollbackRowKinds = cloneStringSlice(gridViewport.RowKinds)
			outScrollbackWrapped = cloneBoolSlice(gridViewport.Wrapped)
			outScrollbackOwnership = cloneStringSlice(gridViewport.Ownership)
			scrollbackTotal = gridViewport.TotalRows
			scrollbackLogicalTotal = gridViewport.LogicalTotal
			scrollbackHasMore = gridViewport.HasMore
			scrollbackLoadedRows = gridViewport.LoadedRows
			historyGeneration = gridViewport.Generation
			scrollbackFirstRowID = gridViewport.FirstRowID
			scrollbackLastRowID = gridViewport.LastRowID
			usedGrid = true
			traceGridVTermRows("core.snapshot.grid_rows", id, gridViewport.Rows, "offset", offset, "limit", limit, "cols", int(size.Cols), "total", scrollbackTotal, "has_more", scrollbackHasMore)
		}
	} else if t.grid != nil {
		var persistedRows int
		_, historyGeneration, persistedRows = t.grid.coordinates()
		scrollbackTotal = persistedRows
		if offset == 0 {
			scrollbackTotal += len(liveTailRows)
		}
		scrollbackLogicalTotal = t.grid.LogicalLineCount()
		scrollbackHasMore = offset < scrollbackTotal
		scrollbackLoadedRows = minInt(offset, persistedRows)
		canonicalLoadedRows := persistedRows - minInt(offset, persistedRows)
		if canonicalLoadedRows > 0 {
			historyGeneration, scrollbackFirstRowID, scrollbackLastRowID, _ = t.grid.rowWindowCoordinates(offset, canonicalLoadedRows)
		}
		if persistedRows <= 0 {
			historyGeneration = 0
		}
		usedGrid = true
	}
	if !usedGrid {
		scrollback := t.vterm.ScrollbackContent()
		liveOffset := offset
		if liveOffset > len(scrollback) {
			liveOffset = len(scrollback)
		}
		end := len(scrollback) - liveOffset
		if end < 0 {
			end = 0
		}
		start := end
		if limit > 0 {
			start = end - limit
			if start < 0 {
				start = 0
			}
		}
		scrollbackTimestamps := t.vterm.ScrollbackTimestamps()
		scrollbackRowKinds := t.vterm.ScrollbackRowKinds()
		scrollbackWrapped := t.vterm.ScrollbackWrapped()
		outScrollback = convertRows(scrollback[start:end])
		outScrollbackTimestamps = sliceTimeRange(scrollbackTimestamps, start, end)
		outScrollbackRowKinds = sliceStringRange(scrollbackRowKinds, start, end)
		outScrollbackWrapped = sliceBoolRange(scrollbackWrapped, start, end)
		outScrollbackOwnership = repeatedString(RowOwnershipPersisted, len(outScrollback))
		scrollbackTotal = len(scrollback)
		scrollbackLogicalTotal = len(scrollback)
		scrollbackHasMore = start > 0
		scrollbackLoadedRows = len(scrollback[start:end])
		traceGridVTermRows("core.snapshot.vterm_scrollback", id, scrollback[start:end], "offset", offset, "limit", limit, "total", scrollbackTotal, "has_more", scrollbackHasMore)
	}
	if offset == 0 && !(usedGrid && len(liveTailRows) > 0) {
		beforeTrim := len(outScrollback)
		outScrollback, outScrollbackTimestamps, outScrollbackRowKinds, outScrollbackWrapped, outScrollbackOwnership = trimScrollbackScreenOverlap(
			outScrollback,
			outScrollbackTimestamps,
			outScrollbackRowKinds,
			outScrollbackWrapped,
			outScrollbackOwnership,
			screenData.Cells,
			screenTimestamps,
			screenRowKinds,
			screenWrapped,
		)
		if beforeTrim != len(outScrollback) {
			traceGridTrimScreenOverlap(id, beforeTrim, len(outScrollback), scrollbackTotal)
		}
	}
	traceGridCoreRows("core.snapshot.scrollback_out", id, outScrollback, "offset", offset, "limit", limit, "total", scrollbackTotal, "has_more", scrollbackHasMore, "used_grid", usedGrid)
	traceGridCoreRows("core.snapshot.screen_out", id, screenData.Cells, "screen_rows", len(screenData.Cells), "screen_cols", int(size.Cols))

	return &Snapshot{
		TerminalID:             id,
		Size:                   size,
		Screen:                 screenData,
		Scrollback:             outScrollback,
		ScrollbackOffset:       offset,
		ScrollbackTotal:        scrollbackTotal,
		ScrollbackLogicalTotal: scrollbackLogicalTotal,
		ScrollbackHasMore:      scrollbackHasMore,
		ScrollbackLoadedRows:   scrollbackLoadedRows,
		HistoryGeneration:      historyGeneration,
		ScrollbackFirstRowID:   scrollbackFirstRowID,
		ScrollbackLastRowID:    scrollbackLastRowID,
		ScreenTimestamps:       cloneTimeSlice(screenTimestamps),
		ScrollbackTimestamps:   outScrollbackTimestamps,
		ScreenRowKinds:         cloneStringSlice(screenRowKinds),
		ScrollbackRowKinds:     outScrollbackRowKinds,
		ScreenWrapped:          cloneBoolSlice(screenWrapped),
		ScrollbackWrapped:      outScrollbackWrapped,
		ScreenOwnership:        repeatedString(RowOwnershipScreen, len(screenData.Cells)),
		ScrollbackOwnership:    outScrollbackOwnership,
		Cursor:                 convertCursorState(t.vterm.CursorState()),
		Modes:                  convertModes(modes),
		Timestamp:              time.Now().UTC(),
	}
}

func (t *Terminal) GridViewport(offset, limit, cols int) *GridViewport {
	return t.GridViewportWithOptions(GridViewportOptions{
		ScrollbackOffset: offset,
		ScrollbackLimit:  limit,
		Cols:             cols,
	})
}

func (t *Terminal) GridViewportWithOptions(opt GridViewportOptions) *GridViewport {
	if t == nil || t.vterm == nil {
		return nil
	}
	t.flushGridAppender()
	offset := opt.ScrollbackOffset
	limit := opt.ScrollbackLimit
	if limit <= 0 {
		limit = defaultGridHistoryPageRows
	}
	if offset < 0 {
		offset = 0
	}
	t.mu.RLock()
	size := t.size
	id := t.id
	liveTail := t.primaryLiveTail.clone()
	t.mu.RUnlock()
	cols := int(size.Cols)
	if cols <= 0 {
		cols = 80
	}
	screenContent := t.vterm.ScreenContent()
	modes := t.vterm.Modes()
	if opt.Alternate || modes.AlternateScreen || screenContent.IsAlternateScreen {
		rows, timestamps, rowKinds, wrapped, total, hasMore := t.alternateGrid.viewport(offset, limit)
		return &GridViewport{
			TerminalID:             id,
			Size:                   Size{Cols: uint16(cols), Rows: size.Rows},
			Rows:                   rows,
			ScrollbackOffset:       offset,
			ScrollbackLimit:        limit,
			ScrollbackTotal:        total,
			ScrollbackLogicalTotal: total,
			ScrollbackHasMore:      hasMore,
			LoadedRows:             offset + len(rows),
			ScrollbackTimestamps:   timestamps,
			ScrollbackRowKinds:     rowKinds,
			ScrollbackWrapped:      wrapped,
			Timestamp:              time.Now().UTC(),
		}
	}
	if t.grid != nil {
		gridViewport, err := t.combinedGridViewport(offset, limit, cols, liveTail)
		if err != nil {
			if t.logger != nil {
				t.logger.Warn("termx terminal grid viewport failed", "terminal_id", id, "error", err)
			}
		} else if gridViewport.TotalRows > 0 {
			rows := convertRows(gridViewport.Rows)
			traceGridVTermRows("core.grid_viewport.grid_rows", id, gridViewport.Rows, "offset", offset, "limit", limit, "cols", cols, "total", gridViewport.TotalRows, "has_more", gridViewport.HasMore, "loaded_rows", gridViewport.LoadedRows)
			traceGridCoreRows("core.grid_viewport.out_rows", id, rows, "offset", offset, "limit", limit, "cols", cols, "total", gridViewport.TotalRows, "has_more", gridViewport.HasMore, "loaded_rows", gridViewport.LoadedRows)
			return &GridViewport{
				TerminalID:             id,
				Size:                   Size{Cols: uint16(cols), Rows: size.Rows},
				Rows:                   rows,
				ScrollbackOffset:       offset,
				ScrollbackLimit:        limit,
				ScrollbackTotal:        gridViewport.TotalRows,
				ScrollbackLogicalTotal: gridViewport.LogicalTotal,
				ScrollbackHasMore:      gridViewport.HasMore,
				LoadedRows:             gridViewport.LoadedRows,
				HistoryGeneration:      gridViewport.Generation,
				FirstRowID:             gridViewport.FirstRowID,
				LastRowID:              gridViewport.LastRowID,
				ScrollbackTimestamps:   cloneTimeSlice(gridViewport.Timestamps),
				ScrollbackRowKinds:     cloneStringSlice(gridViewport.RowKinds),
				ScrollbackWrapped:      cloneBoolSlice(gridViewport.Wrapped),
				RowOwnership:           cloneStringSlice(gridViewport.Ownership),
				Timestamp:              time.Now().UTC(),
			}
		}
	}
	scrollback := t.vterm.ScrollbackContent()
	if offset > len(scrollback) {
		offset = len(scrollback)
	}
	end := len(scrollback) - offset
	if end < 0 {
		end = 0
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	scrollbackWrapped := t.vterm.ScrollbackWrapped()
	for start > 0 && boolAt(scrollbackWrapped, start-1) {
		start--
	}
	rows := convertRows(scrollback[start:end])
	traceGridVTermRows("core.grid_viewport.vterm_rows", id, scrollback[start:end], "offset", offset, "limit", limit, "cols", cols, "total", len(scrollback), "has_more", start > 0)
	traceGridCoreRows("core.grid_viewport.vterm_out_rows", id, rows, "offset", offset, "limit", limit, "cols", cols, "total", len(scrollback), "has_more", start > 0)
	return &GridViewport{
		TerminalID:             id,
		Size:                   Size{Cols: uint16(cols), Rows: size.Rows},
		Rows:                   rows,
		ScrollbackOffset:       offset,
		ScrollbackLimit:        limit,
		ScrollbackTotal:        len(scrollback),
		ScrollbackLogicalTotal: len(scrollback),
		ScrollbackHasMore:      start > 0,
		LoadedRows:             offset + (end - start),
		ScrollbackTimestamps:   sliceTimeRange(t.vterm.ScrollbackTimestamps(), start, end),
		ScrollbackRowKinds:     sliceStringRange(t.vterm.ScrollbackRowKinds(), start, end),
		ScrollbackWrapped:      sliceBoolRange(scrollbackWrapped, start, end),
		RowOwnership:           repeatedString(RowOwnershipPersisted, len(rows)),
		Timestamp:              time.Now().UTC(),
	}
}

func (t *Terminal) combinedGridViewport(offset, limit, cols int, liveTail terminalPrimaryLiveTail) (terminalGridViewport, error) {
	if t == nil || t.grid == nil {
		return terminalGridViewport{}, nil
	}
	return combinedGridViewportFromStore(t.grid, offset, limit, cols, liveTail)
}

func combinedRowWindowCoordinates(store *terminalGridStore, totalRows int, start int, end int) (firstRowID uint64, lastRowID uint64) {
	if store == nil || totalRows <= 0 || end <= start {
		return 0, 0
	}
	baseRowID, _, _ := store.coordinates()
	return baseRowID + uint64(start), baseRowID + uint64(end-1)
}

func (t *Terminal) reclaimPrimaryLiveTailForGrowResizeLocked(cols int) error {
	if t == nil || t.grid == nil || t.vterm == nil || cols <= 0 {
		return nil
	}
	if t.vterm.Modes().AlternateScreen || t.vterm.ScreenContent().IsAlternateScreen {
		return nil
	}
	screenRows := trimTrailingBlankVTermRows(t.vterm.ScreenContent().Cells)
	needed := maxInt(0, int(t.size.Rows)-len(screenRows)-t.primaryLiveTail.nonReclaimedRowCount())
	if needed <= 0 {
		t.primaryLiveTail.replaceReclaimedPrefix(nil, 0, 0, 0)
		return nil
	}
	_, generation, persistedRows := t.grid.coordinates()
	if persistedRows <= 0 {
		t.primaryLiveTail.replaceReclaimedPrefix(nil, 0, 0, 0)
		return nil
	}
	viewport, err := t.grid.reclaimViewport(needed, cols)
	if err != nil {
		return err
	}
	if len(viewport.Rows) == 0 {
		t.primaryLiveTail.replaceReclaimedPrefix(nil, 0, 0, 0)
		return nil
	}
	reclaimed := make([]vterm.DamageOp, 0, len(viewport.Rows))
	for i, row := range viewport.Rows {
		reclaimed = append(reclaimed, vterm.DamageOp{
			Cells:      cloneVTermCells(row),
			Timestamp:  timeAt(viewport.Timestamps, i),
			RowKind:    stringAt(viewport.RowKinds, i),
			WrappedSet: true,
			Wrapped:    boolAt(viewport.Wrapped, i),
		})
	}
	firstRowID := viewport.FirstRowID
	lastRowID := viewport.LastRowID
	if firstRowID == 0 && lastRowID == 0 {
		firstRowID, lastRowID = combinedRowWindowCoordinates(t.grid, persistedRows, persistedRows-len(reclaimed), persistedRows)
	}
	if generation == 0 {
		generation = viewport.Generation
	}
	t.primaryLiveTail.replaceReclaimedPrefixWithLogicalLineIDs(reclaimed, viewport.LogicalLineIDs, generation, firstRowID, lastRowID)
	return nil
}

func (t *Terminal) sealLiveTailForProcessExitLocked() error {
	if t == nil || t.vterm == nil || t.grid == nil {
		return nil
	}
	modes := t.vterm.Modes()
	if modes.AlternateScreen || t.vterm.ScreenContent().IsAlternateScreen {
		if _, err := t.vterm.Write([]byte("\x1b[?1049l")); err != nil {
			return err
		}
		t.alternateGrid.reset()
	}
	rows := t.primaryLiveTailRowsForExit(t.primaryLiveTail.clone())
	if len(rows) == 0 {
		t.primaryLiveTail.reset()
		t.recordLiveTailMetadataLocked()
		return nil
	}
	if t.gridAppender != nil {
		t.gridAppender.flush()
	}
	if err := t.grid.appendRows(rows); err != nil {
		return err
	}
	t.primaryLiveTail.reset()
	t.recordLiveTailMetadataLocked()
	return nil
}

func (t *Terminal) primaryLiveTailRowsForExit(liveTail terminalPrimaryLiveTail) []terminalGridRow {
	if t == nil || t.vterm == nil {
		return nil
	}
	liveTailRows := liveTail.rows()
	vt := t.vterm
	screen := trimTrailingBlankVTermRows(vt.ScreenContent().Cells)
	screenTimestamps := trimTrailingZeroTimes(vt.ScreenTimestamps(), len(screen))
	screenRowKinds := trimTrailingStrings(vt.ScreenRowKinds(), len(screen))
	screenWrapped := trimBoolSlice(vt.ScreenWrapped(), len(screen))
	if len(screen) == 0 && len(liveTailRows) == 0 && !liveTail.wrapPending {
		return nil
	}

	out := make([]terminalGridRow, 0, len(liveTailRows)+len(screen))
	for _, row := range liveTailRows {
		out = append(out, terminalGridRow{
			cells:     damageOpCells(row),
			timestamp: row.Timestamp,
			rowKind:   row.RowKind,
			wrapped:   row.WrappedSet && row.Wrapped,
		})
	}

	if len(screen) > 0 {
		screenRows := terminalGridRowsFromPreserved(screen, screenTimestamps, screenRowKinds, screenWrapped)
		if t.grid != nil {
			cols, _ := vt.Size()
			if cols <= 0 {
				cols = len(screen[0])
			}
			if cols > 0 {
				latestPersisted, err := t.grid.Viewport(0, len(screenRows), cols)
				if err == nil && len(latestPersisted.Rows) > 0 {
					overlap := scrollbackScreenOverlap(
						convertRows(latestPersisted.Rows),
						latestPersisted.Timestamps,
						latestPersisted.RowKinds,
						latestPersisted.Wrapped,
						convertRows(terminalGridRowsToCellsRows(screenRows)),
						screenTimestamps,
						screenRowKinds,
						screenWrapped,
					)
					if overlap > 0 && overlap <= len(screenRows) {
						screenRows = screenRows[overlap:]
						screenTimestamps = trimTimeMetadataHead(screenTimestamps, overlap)
						screenRowKinds = trimStringMetadataHead(screenRowKinds, overlap)
						screenWrapped = trimBoolMetadataHead(screenWrapped, overlap)
					}
				}
			}
		}
		if len(liveTailRows) > 0 {
			screenCore := convertRows(terminalGridRowsToCellsRows(screenRows))
			liveTailCore := convertRows(terminalGridRowsToCellsRows(out))
			overlap := scrollbackScreenOverlap(
				liveTailCore,
				terminalGridRowsTimestamps(out),
				terminalGridRowsKinds(out),
				terminalGridRowsWrapped(out),
				screenCore,
				screenTimestamps,
				screenRowKinds,
				screenWrapped,
			)
			if overlap > 0 && overlap <= len(screenRows) {
				screenRows = screenRows[overlap:]
			}
		}
		out = append(out, screenRows...)
	}

	if len(out) == 0 {
		return nil
	}
	out[len(out)-1].wrapped = false
	return out
}

func terminalGridRowsToCellsRows(rows []terminalGridRow) [][]vterm.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]vterm.Cell, 0, len(rows))
	for _, row := range rows {
		out = append(out, cloneVTermCells(row.cells))
	}
	return out
}

func terminalGridRowsTimestamps(rows []terminalGridRow) []time.Time {
	if len(rows) == 0 {
		return nil
	}
	out := make([]time.Time, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.timestamp)
	}
	return out
}

func terminalGridRowsKinds(rows []terminalGridRow) []string {
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.rowKind)
	}
	return out
}

func terminalGridRowsWrapped(rows []terminalGridRow) []bool {
	if len(rows) == 0 {
		return nil
	}
	out := make([]bool, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.wrapped)
	}
	return out
}

func terminalGridRowsFromVTermRows(rows [][]vterm.Cell, timestamps []time.Time, rowKinds []string, wrapped []bool) []terminalGridRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]terminalGridRow, 0, len(rows))
	for i, row := range rows {
		out = append(out, terminalGridRow{
			cells:     cloneVTermCells(row),
			timestamp: timeAt(timestamps, i),
			rowKind:   stringAt(rowKinds, i),
			wrapped:   boolAt(wrapped, i),
		})
	}
	return out
}

func expandDamageWindowStartToLogicalLine(rows []vterm.DamageOp, logicalLineIDs []uint64, start int) int {
	for start > 0 {
		currentID := uint64At(logicalLineIDs, start)
		prevID := uint64At(logicalLineIDs, start-1)
		if currentID != 0 && prevID != 0 {
			if currentID != prevID {
				break
			}
			start--
			continue
		}
		prev := rows[start-1]
		if !(prev.WrappedSet && prev.Wrapped) {
			break
		}
		start--
	}
	return start
}

func damageOpCells(row vterm.DamageOp) []vterm.Cell {
	if len(row.Cells) > 0 {
		return cloneVTermCells(row.Cells)
	}
	if len(row.Runs) == 0 {
		return nil
	}
	cells := make([]vterm.Cell, 0)
	for _, run := range row.Runs {
		cells = append(cells, vtermCellsFromRun(run)...)
	}
	return cells
}

func trimScrollbackScreenOverlap(scrollback [][]Cell, timestamps []time.Time, rowKinds []string, wrapped []bool, ownership []string, screen [][]Cell, screenTimestamps []time.Time, screenRowKinds []string, screenWrapped []bool) ([][]Cell, []time.Time, []string, []bool, []string) {
	overlap := scrollbackScreenOverlap(scrollback, timestamps, rowKinds, wrapped, screen, screenTimestamps, screenRowKinds, screenWrapped)
	if overlap <= 0 {
		return scrollback, timestamps, rowKinds, wrapped, ownership
	}
	keep := len(scrollback) - overlap
	if keep <= 0 {
		return nil, nil, nil, nil, nil
	}
	scrollback = scrollback[:keep]
	if len(timestamps) >= keep {
		timestamps = timestamps[:keep]
	} else {
		timestamps = nil
	}
	if len(rowKinds) >= keep {
		rowKinds = rowKinds[:keep]
	} else {
		rowKinds = nil
	}
	if len(wrapped) >= keep {
		wrapped = wrapped[:keep]
	} else {
		wrapped = nil
	}
	if len(ownership) >= keep {
		ownership = ownership[:keep]
	} else {
		ownership = nil
	}
	return scrollback, timestamps, rowKinds, wrapped, ownership
}

func scrollbackScreenOverlap(scrollback [][]Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string, scrollbackWrapped []bool, screen [][]Cell, screenTimestamps []time.Time, screenRowKinds []string, screenWrapped []bool) int {
	maxOverlap := minInt(len(scrollback), len(screen))
	for n := maxOverlap; n > 0; n-- {
		scrollbackStart := len(scrollback) - n
		candidate := scrollback[scrollbackStart:]
		if rowsContainNonDefaultCell(candidate) && rowsOverlap(
			candidate,
			scrollbackTimestamps,
			scrollbackRowKinds,
			scrollbackWrapped,
			scrollbackStart,
			screen[:n],
			screenTimestamps,
			screenRowKinds,
			screenWrapped,
			0,
		) {
			return n
		}
	}
	return 0
}

func rowsOverlap(left [][]Cell, leftTimestamps []time.Time, leftRowKinds []string, leftWrapped []bool, leftStart int, right [][]Cell, rightTimestamps []time.Time, rightRowKinds []string, rightWrapped []bool, rightStart int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !cellRowsEqual(left[i], right[i]) {
			return false
		}
		if !rowMetadataEqual(leftTimestamps, leftRowKinds, leftStart+i, rightTimestamps, rightRowKinds, rightStart+i) {
			return false
		}
		if boolAt(leftWrapped, leftStart+i) != boolAt(rightWrapped, rightStart+i) {
			return false
		}
	}
	return true
}

func rowMetadataEqual(leftTimestamps []time.Time, leftRowKinds []string, leftIndex int, rightTimestamps []time.Time, rightRowKinds []string, rightIndex int) bool {
	leftTime := timeAt(leftTimestamps, leftIndex)
	rightTime := timeAt(rightTimestamps, rightIndex)
	if (!leftTime.IsZero() || !rightTime.IsZero()) && !leftTime.Equal(rightTime) {
		return false
	}
	leftKind := stringAt(leftRowKinds, leftIndex)
	rightKind := stringAt(rightRowKinds, rightIndex)
	if (leftKind != "" || rightKind != "") && leftKind != rightKind {
		return false
	}
	return !leftTime.IsZero() || !rightTime.IsZero() || leftKind != "" || rightKind != ""
}

func cellRowsEqual(a []Cell, b []Cell) bool {
	a = trimTrailingDefaultBlankCells(a)
	b = trimTrailingDefaultBlankCells(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func trimTrailingDefaultBlankCells(row []Cell) []Cell {
	for len(row) > 0 && isDefaultBlankCell(row[len(row)-1]) {
		row = row[:len(row)-1]
	}
	return row
}

func rowsContainNonDefaultCell(rows [][]Cell) bool {
	for _, row := range rows {
		for _, cell := range row {
			if !isDefaultBlankCell(cell) {
				return true
			}
		}
	}
	return false
}

func isDefaultBlankCell(cell Cell) bool {
	return cell.Content == " " && cell.Width == 1 && cell.Style == (CellStyle{}) && cell.LinkURL == "" && cell.LinkParams == ""
}

func (t *Terminal) HistoryReplay(opts HistoryReplayOptions) HistoryReplayResult {
	if t == nil || t.vterm == nil {
		return HistoryReplayResult{}
	}
	t.flushGridAppender()
	beforeOffset, limit := sanitizeGridReplayWindow(opts.BeforeOffset, opts.Limit)
	t.mu.RLock()
	id := t.id
	t.mu.RUnlock()
	if opts.Alternate {
		replay, rows, hasMore := t.alternateGrid.replay(beforeOffset, limit)
		return HistoryReplayResult{
			TerminalID:   id,
			BeforeOffset: beforeOffset,
			Limit:        limit,
			Rows:         rows,
			HasMore:      hasMore,
			Replay:       string(replay),
		}
	}
	var (
		replay  []byte
		rows    int
		hasMore bool
	)
	if t.grid != nil {
		var err error
		replay, rows, hasMore, err = t.grid.Replay(beforeOffset, limit)
		if err != nil && t.logger != nil {
			t.logger.Warn("termx terminal grid replay failed", "terminal_id", t.id, "error", err)
		}
	}
	if t.grid == nil || (rows == 0 && beforeOffset == 0) {
		replay, rows, hasMore = t.vterm.EncodeHistoryReplay(beforeOffset, limit)
	}
	return HistoryReplayResult{
		TerminalID:   id,
		BeforeOffset: beforeOffset,
		Limit:        limit,
		Rows:         rows,
		HasMore:      hasMore,
		Replay:       string(replay),
	}
}

func (t *Terminal) SetTags(tags map[string]string) {
	t.mu.Lock()
	if t.tags == nil {
		t.tags = make(map[string]string)
	}
	for k, v := range tags {
		if v == "" {
			delete(t.tags, k)
			continue
		}
		t.tags[k] = v
	}
	t.invalidateProtocolInfoCacheLocked()
	t.mu.Unlock()
	t.events.Publish(Event{
		Type:       EventTerminalMetadataChanged,
		TerminalID: t.id,
		Timestamp:  time.Now().UTC(),
	})
}

func (t *Terminal) SetMetadata(name string, tags map[string]string) {
	t.mu.Lock()
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		t.name = trimmed
	}
	t.tags = copyTags(tags)
	t.invalidateProtocolInfoCacheLocked()
	t.mu.Unlock()
	t.events.Publish(Event{
		Type:       EventTerminalMetadataChanged,
		TerminalID: t.id,
		Timestamp:  time.Now().UTC(),
	})
}

func (t *Terminal) AddAttachment(id, remote string, mode AttachMode, surfaceID, viewID string, resizeOwner bool) {
	t.attachMu.Lock()
	if resizeOwner {
		t.clearResizeOwnersLocked("")
		t.resizeOwnerEpoch++
	}
	t.attachments[id] = AttachInfo{
		RemoteAddr:  remote,
		Mode:        string(mode),
		SurfaceID:   strings.TrimSpace(surfaceID),
		ViewID:      strings.TrimSpace(viewID),
		ResizeOwner: resizeOwner,
		AttachedAt:  time.Now().UTC(),
	}
	t.attachMu.Unlock()
	t.invalidateAttachmentInfo()
}

func (t *Terminal) AttachmentMode(id string) (AttachMode, bool) {
	t.attachMu.Lock()
	defer t.attachMu.Unlock()
	info, ok := t.attachments[id]
	if !ok {
		return "", false
	}
	return AttachMode(info.Mode), true
}

func (t *Terminal) AttachmentResizeOwner(id string) bool {
	t.attachMu.Lock()
	defer t.attachMu.Unlock()
	info, ok := t.attachments[id]
	return ok && info.ResizeOwner
}

func (t *Terminal) SetAttachmentResizeOwner(id string, resizeOwner bool) {
	t.attachMu.Lock()
	info, ok := t.attachments[id]
	if !ok || info.ResizeOwner == resizeOwner {
		t.attachMu.Unlock()
		return
	}
	if resizeOwner {
		t.clearResizeOwnersLocked(id)
	}
	info.ResizeOwner = resizeOwner
	t.attachments[id] = info
	t.resizeOwnerEpoch++
	t.attachMu.Unlock()
	t.invalidateAttachmentInfo()
}

func (t *Terminal) SetAttachmentResizeSurface(id string, surfaceID, viewID string) {
	t.attachMu.Lock()
	info, ok := t.attachments[id]
	if !ok {
		t.attachMu.Unlock()
		return
	}
	nextSurfaceID := strings.TrimSpace(surfaceID)
	nextViewID := strings.TrimSpace(viewID)
	if info.SurfaceID == nextSurfaceID && info.ViewID == nextViewID {
		t.attachMu.Unlock()
		return
	}
	info.SurfaceID = nextSurfaceID
	info.ViewID = nextViewID
	t.attachments[id] = info
	if info.ResizeOwner {
		t.resizeOwnerEpoch++
	}
	t.attachMu.Unlock()
	t.invalidateAttachmentInfo()
}

func (t *Terminal) clearResizeOwnersLocked(exceptID string) {
	for id, info := range t.attachments {
		if id == exceptID || !info.ResizeOwner {
			continue
		}
		info.ResizeOwner = false
		t.attachments[id] = info
	}
}

func (t *Terminal) RemoveAttachment(id string) {
	t.attachMu.Lock()
	info, had := t.attachments[id]
	delete(t.attachments, id)
	if had && info.ResizeOwner {
		t.resizeOwnerEpoch++
	}
	t.attachMu.Unlock()
	if had {
		t.invalidateAttachmentInfo()
	}
}

func (t *Terminal) Attached() []AttachInfo {
	t.attachMu.Lock()
	defer t.attachMu.Unlock()
	out := make([]AttachInfo, 0, len(t.attachments))
	for _, info := range t.attachments {
		out = append(out, info)
	}
	return out
}

func (t *Terminal) RevokeCollaborators() int {
	t.attachMu.Lock()
	revoked := 0
	revokedResizeOwner := false
	for id, info := range t.attachments {
		if info.Mode != string(ModeCollaborator) {
			continue
		}
		revokedResizeOwner = revokedResizeOwner || info.ResizeOwner
		info.Mode = string(ModeObserver)
		info.ResizeOwner = false
		t.attachments[id] = info
		revoked++
	}
	if revokedResizeOwner {
		t.resizeOwnerEpoch++
	}
	t.attachMu.Unlock()
	if revoked > 0 {
		t.invalidateAttachmentInfo()
	}
	return revoked
}

func (t *Terminal) invalidateAttachmentInfo() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.invalidateProtocolInfoCacheLocked()
	t.mu.Unlock()
	if t.updateFunc != nil {
		t.updateFunc()
	}
	if t.events != nil {
		t.events.Publish(Event{
			Type:       EventTerminalMetadataChanged,
			TerminalID: t.id,
			Timestamp:  time.Now().UTC(),
		})
	}
}

func (t *Terminal) resizeOwnerAttachmentCountLocked() int {
	count := 0
	for _, info := range t.attachments {
		if info.ResizeOwner {
			count++
		}
	}
	return count
}

func (t *Terminal) resizeOwnershipSnapshotLocked() ResizeOwnership {
	out := ResizeOwnership{Epoch: t.resizeOwnerEpoch}
	for id, info := range t.attachments {
		if !info.ResizeOwner {
			continue
		}
		out.OwnerAttachmentID = id
		out.OwnerSurfaceID = info.SurfaceID
		out.OwnerViewID = info.ViewID
		out.OwnerRemoteAddr = info.RemoteAddr
		break
	}
	return out
}

func (t *Terminal) startProcessLoops() {
	t.mu.RLock()
	epoch := t.processEpoch
	p := t.pty
	stream := t.stream
	readDone := t.readDone
	done := t.done
	t.mu.RUnlock()
	parserDone := make(chan struct{})
	parserInput := make(chan []byte, terminalPTYParserQueueBytes/terminalPTYReadBufferBytes)
	go t.parseLoop(epoch, stream, parserInput, parserDone)
	go t.readLoop(epoch, p, parserInput, readDone)
	go t.waitLoop(epoch, p, stream, readDone, parserDone, done)
}

func (t *Terminal) broadcastResizeLocked(stream *fanout.Fanout, cols, rows uint16) {
	if t == nil || stream == nil {
		return
	}
	stream.BroadcastResize(cols, rows)
}

func (t *Terminal) closeStreamLocked(stream *fanout.Fanout, exitCode *int) {
	if t == nil || stream == nil {
		return
	}
	stream.Close(exitCode)
}

func (t *Terminal) writeAuthoritativeScreenUpdateLocked(stream *fanout.Fanout, chunk []byte) {
	if t == nil || stream == nil || t.vterm == nil || len(chunk) == 0 {
		return
	}
	if len(chunk) > terminalInlineDamageMaxBytes {
		writeFinish := perftrace.Measure("terminal.screen_update.write_vterm_latest")
		n, err, damage := t.vterm.WriteForLatestFrame(chunk)
		writeFinish(len(chunk))
		if err != nil || n != len(chunk) {
			dropped := len(chunk) - maxInt(0, n)
			stream.BroadcastMessage(fanout.StreamMessage{
				Type:         fanout.StreamSyncLost,
				DroppedBytes: uint64(dropped),
			})
			return
		}
		perftrace.Count("terminal.screen_update.latest_frame_fast_path", len(chunk))
		traceGridDamageOps("core.vterm.damage.large_latest_scrollback", t.id, damage.ScrollbackAppend, "chunk_bytes", len(chunk), "ops", len(damage.Ops), "full_replace", damage.RequiresFullReplace)
		traceGridVTermRows("core.vterm.screen.large_latest", t.id, t.vterm.ScreenContent().Cells, "chunk_bytes", len(chunk))
		t.captureAlternateDamageLocked(damage)
		t.appendGridFromDamageLocked(damage)
		revision := t.bumpScreenRevisionLocked()
		t.debugLogScreenBroadcastLocked("large_latest_placeholder", revision, len(chunk), 0, true, damage)
		// Large PTY bursts are consumed into the daemon vterm with the latest-frame
		// path, but the live payload still has to be derived from each attachment's
		// last acknowledged screen state. A scroll-only delta guessed from the burst
		// damage can leave old rows on the client when wrapped output scrolls more
		// than the captured direct damage describes.
		stream.BroadcastMessage(fanout.StreamMessage{Type: fanout.StreamScreenUpdate, Revision: revision})
		return
	}
	writeFinish := perftrace.Measure("terminal.screen_update.write_vterm")
	n, err, damage := t.vterm.WriteWithDamage(chunk)
	writeFinish(len(chunk))
	if err != nil || n != len(chunk) {
		dropped := len(chunk) - maxInt(0, n)
		stream.BroadcastMessage(fanout.StreamMessage{
			Type:         fanout.StreamSyncLost,
			DroppedBytes: uint64(dropped),
		})
		return
	}
	traceGridDamageOps("core.vterm.damage.inline_scrollback", t.id, damage.ScrollbackAppend, "chunk_bytes", len(chunk), "ops", len(damage.Ops), "full_replace", damage.RequiresFullReplace)
	traceGridVTermRows("core.vterm.screen.inline", t.id, t.vterm.ScreenContent().Cells, "chunk_bytes", len(chunk))
	t.captureAlternateDamageLocked(damage)
	t.appendGridFromDamageLocked(damage)
	revision := t.bumpScreenRevisionLocked()
	payload, ok := t.screenUpdatePayloadFromDamageLocked(damage)
	if !ok {
		t.debugLogScreenBroadcastLocked("encode_failed_placeholder", revision, len(chunk), 0, true, damage)
		stream.BroadcastMessage(fanout.StreamMessage{Type: fanout.StreamScreenUpdate, Revision: revision})
		return
	}
	t.debugLogScreenBroadcastLocked("payload", revision, len(chunk), len(payload), false, damage)
	stream.BroadcastMessage(fanout.StreamMessage{Type: fanout.StreamScreenUpdate, Payload: payload, Revision: revision})
}

func (t *Terminal) bumpScreenRevisionLocked() uint64 {
	if t == nil {
		return 0
	}
	t.screenRevision++
	return t.screenRevision
}

func (t *Terminal) currentScreenRevision() uint64 {
	if t == nil {
		return 0
	}
	t.streamMu.Lock()
	defer t.streamMu.Unlock()
	return t.screenRevision
}

func (t *Terminal) appendGridFromDamageLocked(damage vterm.WriteDamage) {
	if t == nil {
		return
	}
	wrapPending := t.currentWrapPendingLocked()
	persistedRows, persistedLineIDs, liveTailRows, liveTailChanged := t.reconcileLiveTailRowsLocked(damage, wrapPending)
	if damage.RequiresFullReplace && damage.FullReplaceReason == "resize" && len(persistedRows) > 0 && t.grid != nil {
		beforeTrim := len(persistedRows)
		persistedRows = t.trimResizePersistedRowsAlreadyAtGridTailLocked(persistedRows)
		if trimmed := beforeTrim - len(persistedRows); trimmed > 0 {
			persistedLineIDs = trimUint64Prefix(persistedLineIDs, trimmed)
		}
	}
	if liveTailChanged {
		if damage.RequiresFullReplace && damage.FullReplaceReason == "resize" {
			t.primaryLiveTail.replaceResizeRowsWithLogicalLineIDs(liveTailRows.rows, liveTailRows.logicalLineIDs, wrapPending)
		} else {
			t.primaryLiveTail.replaceLiveRowsWithLogicalLineIDs(liveTailRows.rows, liveTailRows.logicalLineIDs, wrapPending)
		}
		t.recordLiveTailMetadataLocked()
	} else {
		t.primaryLiveTail.setWrapPending(wrapPending)
	}
	if t.grid == nil || len(persistedRows) == 0 {
		return
	}
	t.recordLiveTailLineMigrationsLocked(persistedRows, persistedLineIDs)
	traceGridDamageOps("core.append_grid.damage", t.id, persistedRows, "ops", len(damage.Ops), "alternate_rows", len(damage.AlternateAppend), "live_tail_rows", len(liveTailRows.rows))
	if t.gridAppender != nil {
		t.gridAppender.appendWithLogicalLineIDs(persistedRows, persistedLineIDs)
		return
	}
	if err := t.grid.AppendDamageRowsWithLogicalLineIDs(persistedRows, persistedLineIDs); err != nil && t.logger != nil {
		t.logger.Warn("termx terminal grid append failed", "terminal_id", t.id, "error", err)
	}
}

func (t *Terminal) trimResizePersistedRowsAlreadyAtGridTailLocked(rows []vterm.DamageOp) []vterm.DamageOp {
	if t == nil || t.grid == nil || len(rows) == 0 {
		return rows
	}
	_, _, persistedRows := t.grid.coordinates()
	if persistedRows <= 0 {
		return rows
	}
	limit := minInt(len(rows), persistedRows)
	viewport, err := t.grid.Viewport(0, limit, 0)
	if err != nil || len(viewport.Rows) == 0 {
		return rows
	}
	tailRows := terminalGridRowsFromVTermRows(viewport.Rows, viewport.Timestamps, viewport.RowKinds, viewport.Wrapped)
	maxOverlap := minInt(len(rows), len(tailRows))
	for overlap := maxOverlap; overlap > 0; overlap-- {
		if terminalGridDamageRowsEqual(tailRows[len(tailRows)-overlap:], rows[:overlap]) {
			return cloneGridDamageOps(rows[overlap:])
		}
	}
	return rows
}

func terminalGridDamageRowsEqual(gridRows []terminalGridRow, damageRows []vterm.DamageOp) bool {
	if len(gridRows) != len(damageRows) {
		return false
	}
	for i, gridRow := range gridRows {
		damageRow := damageRows[i]
		if gridRow.wrapped != (damageRow.WrappedSet && damageRow.Wrapped) {
			return false
		}
		if !reflect.DeepEqual(gridRow.cells, damageOpCells(damageRow)) {
			return false
		}
	}
	return true
}

func terminalDamageRowsEqual(left []vterm.DamageOp, right []vterm.DamageOp) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if (left[i].WrappedSet && left[i].Wrapped) != (right[i].WrappedSet && right[i].Wrapped) {
			return false
		}
		if !reflect.DeepEqual(damageOpCells(left[i]), damageOpCells(right[i])) {
			return false
		}
	}
	return true
}

func splitDamageRowsByLiveTailHint(rows []vterm.DamageOp, liveTailRows int) ([]vterm.DamageOp, []vterm.DamageOp) {
	if len(rows) == 0 {
		return nil, nil
	}
	if liveTailRows <= 0 {
		return cloneGridDamageOps(rows), nil
	}
	if liveTailRows > len(rows) {
		liveTailRows = len(rows)
	}
	persistedCount := len(rows) - liveTailRows
	for persistedCount > 0 && rows[persistedCount-1].WrappedSet && rows[persistedCount-1].Wrapped {
		persistedCount--
	}
	return cloneGridDamageOps(rows[:persistedCount]), cloneGridDamageOps(rows[persistedCount:])
}

func (t *Terminal) reconcileLiveTailRowsLocked(damage vterm.WriteDamage, nextWrapPending bool) ([]vterm.DamageOp, []uint64, terminalLiveTailRowsWithLogicalLineIDs, bool) {
	if t == nil || len(damage.ScrollbackAppend) == 0 {
		return nil, nil, terminalLiveTailRowsWithLogicalLineIDs{}, false
	}
	if damage.RequiresFullReplace && damage.FullReplaceReason == "resize" {
		liveTail := t.primaryLiveTail.nonReclaimedRowsForResizePrefixWithLogicalLineIDs()
		resizeRows := cloneGridDamageOps(damage.ScrollbackAppend)
		resizeLineIDs := make([]uint64, len(resizeRows))
		for i := range resizeRows {
			if damage.ResizeLiveTailRows > 0 && i >= len(resizeRows)-damage.ResizeLiveTailRows {
				resizeRows[i].WrappedSet = true
				resizeRows[i].Wrapped = true
			}
		}
		liveTail.rows = append(liveTail.rows, resizeRows...)
		liveTail.logicalLineIDs = append(liveTail.logicalLineIDs, resizeLineIDs...)
		return nil, nil, liveTail, true
	}

	persistedPrefix, _ := splitDamageRowsByLiveTailHint(damage.ScrollbackAppend, damage.LiveTailAppendRows)
	liveTailStart := len(persistedPrefix)
	existingLiveTail := t.primaryLiveTail.liveRowsWithLogicalLineIDs()
	persistedRows := make([]vterm.DamageOp, 0, len(existingLiveTail.rows)+len(damage.ScrollbackAppend))
	persistedLineIDs := make([]uint64, 0, len(existingLiveTail.rows)+len(damage.ScrollbackAppend))
	liveTailRows := cloneGridDamageOps(existingLiveTail.rows)
	liveTailLineIDs := cloneUint64Slice(existingLiveTail.logicalLineIDs)
	pendingWrap := t.primaryLiveTail.wrapPending
	canContinuePendingWrap := nextWrapPending || damage.LiveTailAppendRows > 0

	for i, row := range damage.ScrollbackAppend {
		cloned := cloneGridDamageOp(row)
		belongsToLiveTail := i >= liveTailStart || (canContinuePendingWrap && pendingWrap && i == 0) || (len(liveTailRows) > 0 && cloned.WrappedSet && cloned.Wrapped)
		if pendingWrap {
			if !belongsToLiveTail {
				if len(liveTailRows) > 0 {
					persistedRows = append(persistedRows, liveTailRows...)
					persistedLineIDs = append(persistedLineIDs, liveTailLineIDs...)
					liveTailRows = nil
					liveTailLineIDs = nil
				}
				pendingWrap = false
			} else {
				cloned.WrappedSet = true
				cloned.Wrapped = true
				liveTailRows = append(liveTailRows, cloned)
				liveTailLineIDs = append(liveTailLineIDs, 0)
				pendingWrap = false
				continue
			}
		}
		if belongsToLiveTail {
			cloned.WrappedSet = true
			cloned.Wrapped = true
			liveTailRows = append(liveTailRows, cloned)
			liveTailLineIDs = append(liveTailLineIDs, 0)
			continue
		}
		if len(liveTailRows) > 0 {
			persistedRows = append(persistedRows, liveTailRows...)
			persistedLineIDs = append(persistedLineIDs, liveTailLineIDs...)
			liveTailRows = nil
			liveTailLineIDs = nil
		}
		persistedRows = append(persistedRows, cloned)
		persistedLineIDs = append(persistedLineIDs, 0)
	}
	return persistedRows, persistedLineIDs, terminalLiveTailRowsWithLogicalLineIDs{rows: liveTailRows, logicalLineIDs: liveTailLineIDs}, true
}

func (t *Terminal) recordLiveTailLineMigrationsLocked(rows []vterm.DamageOp, runtimeLineIDs []uint64) {
	if t == nil || t.grid == nil || len(rows) == 0 || len(runtimeLineIDs) == 0 {
		return
	}
	baseRowID, _, rowCount := t.grid.coordinates()
	startRowID := baseRowID + uint64(rowCount)
	start := 0
	for i, row := range rows {
		currentID := uint64At(runtimeLineIDs, i)
		nextID := uint64At(runtimeLineIDs, i+1)
		if i < len(rows)-1 && currentID != 0 && nextID != 0 && currentID == nextID {
			continue
		}
		if currentID == 0 && row.WrappedSet && row.Wrapped && i < len(rows)-1 {
			continue
		}
		runtimeID := uint64At(runtimeLineIDs, start)
		if terminalRuntimeLogicalLineID(runtimeID) {
			if t.liveLineMigrations == nil {
				t.liveLineMigrations = make(map[uint64]uint64)
			}
			t.liveLineMigrations[runtimeID] = persistedLogicalLineIDFromRowID(startRowID + uint64(start))
		}
		start = i + 1
	}
	if len(t.liveLineMigrations) > 0 {
		if err := t.grid.recordLineMigrations(t.liveLineMigrations); err != nil && t.logger != nil {
			t.logger.Warn("termx terminal grid line metadata write failed", "terminal_id", t.id, "error", err)
		}
	}
}

func (t *Terminal) recordLiveTailMetadataLocked() {
	if t == nil || t.grid == nil {
		return
	}
	if err := t.grid.recordLiveTailLineState(t.primaryLiveTail.logicalLineRecords(), t.primaryLiveTail.rows()); err != nil && t.logger != nil {
		t.logger.Warn("termx terminal live tail metadata write failed", "terminal_id", t.id, "error", err)
	}
}

func (t *Terminal) currentWrapPendingLocked() bool {
	if t == nil || t.vterm == nil {
		return false
	}
	modes := t.vterm.Modes()
	if !modes.AutoWrap || modes.AlternateScreen {
		return false
	}
	cols, _ := t.vterm.Size()
	if cols <= 0 {
		return false
	}
	cursor := t.vterm.CursorState()
	if cursor.Col >= cols {
		return true
	}
	if cursor.Col != cols-1 {
		return false
	}
	if cursor.Row < 0 {
		return false
	}
	row := t.vterm.UsedScreenRow(cursor.Row)
	return len(row) >= cols
}

func (t *Terminal) captureAlternateDamageLocked(damage vterm.WriteDamage) {
	if t == nil {
		return
	}
	if !damage.Modes.AlternateScreen {
		t.alternateGrid.reset()
		return
	}
	t.alternateGrid.appendDamageRows(damage.AlternateAppend)
}

func (t *Terminal) flushGridAppender() {
	if t == nil || t.gridAppender == nil {
		return
	}
	t.gridAppender.flush()
}

func (t *Terminal) screenInvalidationMessage() StreamMessage {
	return StreamMessage{
		Type:     StreamScreenUpdate,
		Revision: t.currentScreenRevision(),
		Latest:   t.screenSnapshotFallbackMessage,
	}
}

func (t *Terminal) readLoop(epoch uint64, p *ptymgr.PTY, parserInput chan<- []byte, readDone chan struct{}) {
	defer close(readDone)
	defer close(parserInput)
	buf := make([]byte, terminalPTYReadBufferBytes)
	var (
		lastStatsLog = time.Now()
		readCount    int
		readBytes    int
		maxReadBytes int
	)
	flushStats := func(reason string) {
		if t == nil || t.logger == nil || readCount == 0 {
			lastStatsLog = time.Now()
			return
		}
		t.logger.Info(
			"termx terminal pty read stats",
			"terminal_id", t.id,
			"epoch", epoch,
			"reason", reason,
			"reads", readCount,
			"bytes", readBytes,
			"max_read_bytes", maxReadBytes,
		)
		lastStatsLog = time.Now()
		readCount = 0
		readBytes = 0
		maxReadBytes = 0
	}
	if t.logger != nil {
		t.logger.Info("termx terminal read loop started", "terminal_id", t.id, "epoch", epoch)
	}
	defer flushStats("closed")
	for {
		n, err := p.ReadBatch(buf)
		if n > 0 {
			readCount++
			readBytes += n
			if n > maxReadBytes {
				maxReadBytes = n
			}
			if n >= coreDiagnosticsLargePayloadBytes && t.logger != nil {
				t.logger.Warn("termx terminal pty large read", "terminal_id", t.id, "epoch", epoch, "bytes", n)
			}
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			parserInput <- chunk
			if time.Since(lastStatsLog) >= coreDiagnosticsInterval {
				flushStats("interval")
			}
		}
		if err != nil {
			t.mu.RLock()
			removed := t.removed
			currentEpoch := t.processEpoch
			t.mu.RUnlock()
			if currentEpoch != epoch {
				return
			}
			flushStats("read_error")
			if err != io.EOF {
				if removed {
					return
				}
				if t.logger != nil {
					t.logger.Warn("termx terminal pty read failed", "terminal_id", t.id, "epoch", epoch, "error", err)
				}
				t.events.Publish(Event{
					Type:       EventTerminalReadError,
					TerminalID: t.id,
					Timestamp:  time.Now().UTC(),
					ReadError:  &TerminalReadErrorData{Error: err.Error()},
				})
			}
			return
		}
	}
}

func (t *Terminal) parseLoop(epoch uint64, stream *fanout.Fanout, parserInput <-chan []byte, parserDone chan struct{}) {
	defer close(parserDone)
	var pending []byte
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	timerActive := false
	flush := func() {
		if len(pending) == 0 {
			return
		}
		pendingBytes := len(pending)
		perftrace.Count("terminal.parser.flush", pendingBytes)
		lockWaitFinish := perftrace.Measure("terminal.parser.stream_lock_wait")
		t.streamMu.Lock()
		lockWaitFinish(pendingBytes)
		t.writeAuthoritativeScreenUpdateLocked(stream, pending)
		t.streamMu.Unlock()
		pending = nil
	}
	stopTimer := func() {
		if !timerActive {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerActive = false
	}
	startTimer := func() {
		if terminalPTYParserFlushDelay <= 0 || timerActive {
			return
		}
		timer.Reset(terminalPTYParserFlushDelay)
		timerActive = true
	}
	for {
		select {
		case chunk, ok := <-parserInput:
			if !ok {
				stopTimer()
				flush()
				return
			}
			if len(pending) == 0 {
				pending = chunk
			} else {
				pending = append(pending, chunk...)
			}
			if len(pending) >= terminalPTYParserChunkMaxBytes {
				stopTimer()
				flush()
				continue
			}
			startTimer()
		case <-timer.C:
			timerActive = false
			flush()
		}
		if t == nil {
			return
		}
		t.mu.RLock()
		currentEpoch := t.processEpoch
		t.mu.RUnlock()
		if currentEpoch != epoch {
			stopTimer()
			return
		}
	}
}

func (t *Terminal) waitLoop(epoch uint64, p *ptymgr.PTY, stream *fanout.Fanout, readDone <-chan struct{}, parserDone <-chan struct{}, done chan struct{}) {
	<-p.Wait()
	code := p.ExitCode()

	select {
	case <-readDone:
	case <-time.After(terminalPTYParserCloseWait):
		if t.logger != nil {
			t.logger.Warn("termx terminal reader did not drain before close", "terminal_id", t.id, "epoch", epoch)
		}
	}
	select {
	case <-parserDone:
	case <-time.After(terminalPTYParserCloseWait):
		if t.logger != nil {
			t.logger.Warn("termx terminal parser did not drain before close", "terminal_id", t.id, "epoch", epoch)
		}
	}

	t.streamMu.Lock()
	if err := t.sealLiveTailForProcessExitLocked(); err != nil && t.logger != nil {
		t.logger.Warn("termx terminal process-exit force seal failed", "terminal_id", t.id, "epoch", epoch, "error", err)
	}
	t.streamMu.Unlock()

	t.mu.Lock()
	if t.processEpoch != epoch || t.pty != p {
		t.mu.Unlock()
		return
	}
	oldState := t.state
	t.state = StateExited
	t.exitCode = &code
	removed := t.removed
	keepAfterExit := t.keepAfterExit
	t.invalidateProtocolInfoCacheLocked()
	t.mu.Unlock()

	// Terminal exit happens asynchronously, so we explicitly invalidate any
	// cached list payloads that include state or exit-code fields.
	if !removed && t.updateFunc != nil {
		t.updateFunc()
	}

	t.streamMu.Lock()
	t.closeStreamLocked(stream, &code)
	t.streamMu.Unlock()
	if !removed {
		t.events.Publish(Event{
			Type:       EventTerminalStateChanged,
			TerminalID: t.id,
			Timestamp:  time.Now().UTC(),
			StateChanged: &TerminalStateChangedData{
				OldState: oldState,
				NewState: StateExited,
				ExitCode: &code,
			},
		})
	}
	close(done)
	if removed {
		return
	}

	if keepAfterExit <= 0 {
		t.removeIfEpoch(epoch, "expired")
		return
	}

	timer := time.NewTimer(keepAfterExit)
	defer timer.Stop()
	<-timer.C
	t.removeIfEpoch(epoch, "expired")
}

func (t *Terminal) removeIfEpoch(epoch uint64, reason string) {
	t.mu.Lock()
	if t.removed || t.processEpoch != epoch || t.state != StateExited {
		t.mu.Unlock()
		return
	}
	t.removed = true
	id := t.id
	removeFunc := t.removeFunc
	t.mu.Unlock()

	if removeFunc != nil {
		removeFunc(id, reason)
	}
	if t.gridAppender != nil {
		t.gridAppender.close()
	}
	_ = closeTerminalGridStore(t.grid)
}

func liveScrollbackRows(configured int) int {
	if configured <= 0 {
		return defaultTerminalLiveScrollbackRows
	}
	return minInt(configured, defaultTerminalLiveScrollbackRows)
}

func GenerateID() (string, error) {
	return strconv.FormatUint(terminalIDCounter.Add(1), 10), nil
}

func ObserveGeneratedID(raw string) {
	value, ok := parseObservedTerminalID(raw)
	if !ok {
		return
	}
	for {
		current := terminalIDCounter.Load()
		if current >= value {
			return
		}
		if terminalIDCounter.CompareAndSwap(current, value) {
			return
		}
	}
}

func parseObservedTerminalID(raw string) (uint64, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return n, true
}

func copyTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for k, v := range tags {
		out[k] = v
	}
	return out
}

func copyIntPtr(v *int) *int {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}

func cloneResizeOwnership(in ResizeOwnership) *ResizeOwnership {
	out := in
	return &out
}

func protocolResizeOwnership(in ResizeOwnership) *protocol.ResizeOwnership {
	return &protocol.ResizeOwnership{
		OwnerAttachmentID: in.OwnerAttachmentID,
		OwnerSurfaceID:    in.OwnerSurfaceID,
		OwnerViewID:       in.OwnerViewID,
		OwnerRemoteAddr:   in.OwnerRemoteAddr,
		Size:              protocol.Size{Cols: in.Size.Cols, Rows: in.Size.Rows},
		SizeLocked:        in.SizeLocked,
		Epoch:             in.Epoch,
	}
}

func (t *Terminal) bootstrapMessagesLocked() []StreamMessage {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	size := t.size
	t.mu.RUnlock()
	msgs := make([]StreamMessage, 0, 3)
	if size.Cols > 0 && size.Rows > 0 {
		msgs = append(msgs, StreamMessage{Type: StreamResize, Cols: size.Cols, Rows: size.Rows})
	}
	if payload, ok := t.screenSnapshotPayloadLocked(); ok {
		msgs = append(msgs, StreamMessage{Type: StreamScreenUpdate, Payload: payload, Revision: t.screenRevision})
	}
	msgs = append(msgs, StreamMessage{Type: StreamBootstrapDone})
	return msgs
}

func (t *Terminal) screenSnapshotFallbackMessage() StreamMessage {
	if t == nil {
		return StreamMessage{Type: StreamSyncLost}
	}
	t.streamMu.Lock()
	defer t.streamMu.Unlock()
	return t.screenSnapshotFallbackMessageLocked()
}

func (t *Terminal) screenSnapshotFallbackMessageLocked() StreamMessage {
	if t == nil {
		return StreamMessage{Type: StreamSyncLost}
	}
	payload, ok := t.screenSnapshotPayloadLocked()
	if !ok {
		return StreamMessage{Type: StreamSyncLost}
	}
	return StreamMessage{Type: StreamScreenUpdate, Payload: payload, Revision: t.screenRevision}
}

func (t *Terminal) screenSnapshotPayloadLocked() ([]byte, bool) {
	finish := perftrace.Measure("terminal.screen_update.snapshot_payload")
	defer finish(0)
	if t == nil || t.vterm == nil {
		return nil, false
	}
	update := screenFullReplaceUpdateFromVTerm(t.vterm, t.currentTitleLocked())
	encodeFinish := perftrace.Measure("terminal.screen_update.encode")
	payload, err := protocol.EncodeScreenUpdatePayload(update)
	encodeFinish(len(payload))
	if err != nil {
		return nil, false
	}
	recordEncodedScreenUpdatePayload(screenUpdateEncodeModeFullReplace, payload)
	return payload, true
}

func (t *Terminal) screenUpdatePayloadFromDamageLocked(damage vterm.WriteDamage) ([]byte, bool) {
	finish := perftrace.Measure("terminal.screen_update.from_damage")
	defer finish(0)
	if t == nil || t.vterm == nil {
		return nil, false
	}
	deltaUpdate := screenUpdateFromDamageState(damage, t.currentTitleLocked())
	if damage.RequiresFullReplace {
		perftrace.Count("terminal.screen_update.requires_full_replace_damage", 0)
		if damage.FullReplaceReason != "" {
			perftrace.Count("terminal.screen_update.requires_full_replace."+damage.FullReplaceReason, 0)
		}
		fullUpdate := screenFullReplaceUpdateFromVTerm(t.vterm, t.currentTitleLocked())
		encodeFinish := perftrace.Measure("terminal.screen_update.encode")
		payload, err := protocol.EncodeScreenUpdatePayload(fullUpdate)
		encodeFinish(len(payload))
		if err != nil {
			return nil, false
		}
		recordEncodedScreenUpdatePayload(screenUpdateEncodeModeFullReplace, payload)
		t.logScreenUpdatePayloadDiagnosticsLocked(damage, screenUpdateEncodeResult{
			Payload:          payload,
			Mode:             screenUpdateEncodeModeFullReplace,
			FullPayloadBytes: len(payload),
			Reason:           fullReplaceDamageReason(damage),
		})
		return payload, true
	}
	if screenUpdateShouldEncodeDeltaOnly(deltaUpdate, damage.Modes.AlternateScreen) {
		perftrace.Count("terminal.screen_update.delta_only_shortcut", 0)
		encodeFinish := perftrace.Measure("terminal.screen_update.encode")
		payload, err := protocol.EncodeScreenUpdatePayload(deltaUpdate)
		encodeFinish(len(payload))
		if err == nil && len(payload) <= screenUpdateDeltaOnlyShortcutMaxPayloadBytes {
			recordEncodedScreenUpdatePayload(screenUpdateEncodeModeDelta, payload)
			t.logScreenUpdatePayloadDiagnosticsLocked(damage, screenUpdateEncodeResult{
				Payload:           payload,
				Mode:              screenUpdateEncodeModeDelta,
				DeltaPayloadBytes: len(payload),
				Reason:            "delta_only_shortcut",
			})
			return payload, true
		}
		if err != nil {
			perftrace.Count("terminal.screen_update.delta_only_encode_error", 0)
		} else {
			perftrace.Count("terminal.screen_update.delta_only_shortcut_large_compare", len(payload))
		}
	}
	perftrace.Count("terminal.screen_update.requires_full_snapshot", 0)
	fullUpdate := screenFullReplaceUpdateFromVTerm(t.vterm, t.currentTitleLocked())
	result, ok := encodeScreenUpdatePayloadByStrategyWithDiagnostics(deltaUpdate, fullUpdate, fullUpdate.Modes.AlternateScreen)
	if !ok {
		return nil, false
	}
	t.logScreenUpdatePayloadDiagnosticsLocked(damage, result)
	return result.Payload, true
}

func fullReplaceDamageReason(damage vterm.WriteDamage) string {
	if damage.FullReplaceReason == "" {
		return "requires_full_replace_damage"
	}
	return "requires_full_replace_" + damage.FullReplaceReason
}

type screenUpdateDamageDiagnostics struct {
	Ops                  int
	WriteSpanOps         int
	WriteSpanCells       int
	ScrollRectOps        int
	CopyRectOps          int
	ClearRectOps         int
	ClearToEOLOps        int
	ControlOps           int
	ScrollbackAppendRows int
	ScrollbackAppendCell int
	ChangedRows          int
	ChangedCells         int
}

func (t *Terminal) logScreenUpdatePayloadDiagnosticsLocked(damage vterm.WriteDamage, result screenUpdateEncodeResult) {
	if t == nil || t.logger == nil || len(result.Payload) == 0 {
		return
	}
	debug := coreScreenUpdateDebugEnabled()
	stats := screenUpdateDamageStats(damage)
	if len(result.Payload) < coreDiagnosticsScreenUpdateBytes && !result.Compared && !debug {
		return
	}
	fields := []any{
		"termx screen update payload detail",
		"terminal_id", t.id,
		"mode", string(result.Mode),
		"reason", result.Reason,
		"payload_bytes", len(result.Payload),
		"delta_payload_bytes", result.DeltaPayloadBytes,
		"full_payload_bytes", result.FullPayloadBytes,
		"compared", result.Compared,
		"requires_full_replace", damage.RequiresFullReplace,
		"alternate_screen", damage.Modes.AlternateScreen,
		"cols", damage.SizeCols,
		"rows", damage.SizeRows,
		"screen_scroll", damage.ScreenScroll,
		"ops", stats.Ops,
		"full_replace_reason", damage.FullReplaceReason,
		"direct_damage_items", damage.DirectDamageItems,
		"direct_damage_rows", damage.DirectDamageRows,
		"direct_damage_cells", damage.DirectDamageCells,
		"write_span_ops", stats.WriteSpanOps,
		"write_span_cells", stats.WriteSpanCells,
		"scroll_rect_ops", stats.ScrollRectOps,
		"copy_rect_ops", stats.CopyRectOps,
		"clear_rect_ops", stats.ClearRectOps,
		"clear_to_eol_ops", stats.ClearToEOLOps,
		"control_ops", stats.ControlOps,
		"scrollback_trim", damage.ScrollbackTrim,
		"scrollback_append_rows", stats.ScrollbackAppendRows,
		"scrollback_append_cells", stats.ScrollbackAppendCell,
		"changed_rows", stats.ChangedRows,
		"changed_cells", stats.ChangedCells,
		"diff_cpu_ms", float64(damage.DiffCPUNanos) / 1_000_000.0,
	}
	if debug {
		fields = append(fields, "screen_tail", t.debugScreenTailLocked(6))
	}
	t.logger.Info(fields[0].(string), fields[1:]...)
}

func (t *Terminal) debugLogScreenBroadcastLocked(reason string, revision uint64, chunkBytes int, payloadBytes int, placeholder bool, damage vterm.WriteDamage) {
	if t == nil || t.logger == nil || !coreScreenUpdateDebugEnabled() {
		return
	}
	stats := screenUpdateDamageStats(damage)
	cursor := vterm.CursorState{}
	if t.vterm != nil {
		cursor = t.vterm.CursorState()
	}
	t.logger.Debug(
		"termx screen update broadcast",
		"terminal_id", t.id,
		"revision", revision,
		"reason", reason,
		"chunk_bytes", chunkBytes,
		"payload_bytes", payloadBytes,
		"placeholder", placeholder,
		"alternate_screen", damage.Modes.AlternateScreen,
		"cols", damage.SizeCols,
		"rows", damage.SizeRows,
		"screen_scroll", damage.ScreenScroll,
		"ops", stats.Ops,
		"write_span_ops", stats.WriteSpanOps,
		"scroll_rect_ops", stats.ScrollRectOps,
		"copy_rect_ops", stats.CopyRectOps,
		"clear_rect_ops", stats.ClearRectOps,
		"clear_to_eol_ops", stats.ClearToEOLOps,
		"scrollback_append_rows", stats.ScrollbackAppendRows,
		"changed_rows", stats.ChangedRows,
		"changed_cells", stats.ChangedCells,
		"cursor_row", cursor.Row,
		"cursor_col", cursor.Col,
		"screen_tail", t.debugScreenTailLocked(6),
	)
}

func (t *Terminal) debugScreenTailLocked(limit int) string {
	if t == nil || t.vterm == nil || limit <= 0 {
		return ""
	}
	screen := t.vterm.ScreenContent()
	rows := screen.Cells
	if len(rows) == 0 {
		return ""
	}
	start := len(rows) - limit
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, len(rows)-start)
	for _, row := range rows[start:] {
		out = append(out, debugVTermRowString(row))
	}
	return strings.Join(out, "\\n")
}

func debugVTermRowString(row []vterm.Cell) string {
	if len(row) == 0 {
		return ""
	}
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func screenUpdateDamageStats(damage vterm.WriteDamage) screenUpdateDamageDiagnostics {
	stats := screenUpdateDamageDiagnostics{Ops: len(damage.Ops)}
	changedRows := make(map[int]struct{}, len(damage.Ops))
	markRectRows := func(y, h int) {
		for row := y; row < y+h; row++ {
			changedRows[row] = struct{}{}
		}
	}
	for _, op := range damage.Ops {
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			stats.WriteSpanOps++
			stats.WriteSpanCells += len(op.Cells)
			changedRows[op.Row] = struct{}{}
		case vterm.ScreenOpScrollRect:
			stats.ScrollRectOps++
			stats.ChangedCells += maxInt(0, op.Rect.Width*op.Rect.Height)
			markRectRows(op.Rect.Y, op.Rect.Height)
		case vterm.ScreenOpCopyRect:
			stats.CopyRectOps++
			stats.ChangedCells += maxInt(0, op.Src.Width*op.Src.Height)
			markRectRows(op.DstY, op.Src.Height)
		case vterm.ScreenOpClearRect:
			stats.ClearRectOps++
			stats.ChangedCells += maxInt(0, op.Rect.Width*op.Rect.Height)
			markRectRows(op.Rect.Y, op.Rect.Height)
		case vterm.ScreenOpClearToEOL:
			stats.ClearToEOLOps++
			stats.ChangedCells++
			changedRows[op.Row] = struct{}{}
		case vterm.ScreenOpCursor, vterm.ScreenOpModes, vterm.ScreenOpResize, vterm.ScreenOpTitle:
			stats.ControlOps++
		}
	}
	stats.ScrollbackAppendRows = len(damage.ScrollbackAppend)
	for _, row := range damage.ScrollbackAppend {
		stats.ScrollbackAppendCell += len(row.Cells)
	}
	stats.ChangedRows = len(changedRows) + stats.ScrollbackAppendRows
	stats.ChangedCells += stats.WriteSpanCells + stats.ScrollbackAppendCell
	return stats
}

func (t *Terminal) currentStreamScreenStateLocked() *streamScreenState {
	finish := perftrace.Measure("terminal.screen_update.full_snapshot")
	defer finish(0)
	if t == nil || t.vterm == nil {
		return nil
	}
	return &streamScreenState{
		snapshot: snapshotFromVTerm(t.vterm),
		title:    t.currentTitleLocked(),
	}
}

func (t *Terminal) currentTitleLocked() string {
	if t == nil {
		return ""
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.title
}

func cloneTerminalStreamMessage(msg StreamMessage) StreamMessage {
	return StreamMessage{
		Type:         msg.Type,
		Payload:      append([]byte(nil), msg.Payload...),
		Revision:     msg.Revision,
		DroppedBytes: msg.DroppedBytes,
		ExitCode:     copyIntPtr(msg.ExitCode),
		Cols:         msg.Cols,
		Rows:         msg.Rows,
		Latest:       msg.Latest,
	}
}

func cloneFanoutStreamMessage(msg fanout.StreamMessage) StreamMessage {
	return StreamMessage{
		Type:         StreamMessageType(msg.Type),
		Payload:      append([]byte(nil), msg.Payload...),
		Revision:     msg.Revision,
		DroppedBytes: msg.DroppedBytes,
		ExitCode:     copyIntPtr(msg.ExitCode),
		Cols:         msg.Cols,
		Rows:         msg.Rows,
	}
}

func snapshotRowString(row []Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func (t *Terminal) invalidateProtocolInfoCacheLocked() {
	t.listInfoCache = nil
	t.metadataVersion++
}

func (t *Terminal) protocolInfo() *protocol.TerminalInfo {
	t.attachMu.Lock()
	resizeOwnerAttachmentCount := t.resizeOwnerAttachmentCountLocked()
	resizeOwnership := t.resizeOwnershipSnapshotLocked()
	t.mu.RLock()
	t.attachMu.Unlock()
	resizeOwnership.Size = Size{Cols: t.size.Cols, Rows: t.size.Rows}
	resizeOwnership.SizeLocked = terminalmeta.SizeLocked(t.tags)
	if terminalmeta.SizeLocked(t.tags) {
		resizeOwnerAttachmentCount = 0
		resizeOwnership.OwnerAttachmentID = ""
		resizeOwnership.OwnerSurfaceID = ""
		resizeOwnership.OwnerViewID = ""
		resizeOwnership.OwnerRemoteAddr = ""
	}

	info := &protocol.TerminalInfo{
		ID:                         t.id,
		Name:                       t.name,
		Command:                    append([]string(nil), t.command...),
		Tags:                       copyTags(t.tags),
		Size:                       protocol.Size{Cols: t.size.Cols, Rows: t.size.Rows},
		State:                      string(t.state),
		CWD:                        t.cwd,
		LiveCWD:                    t.cwd,
		CreatedAt:                  t.createdAt,
		ExitCode:                   copyIntPtr(t.exitCode),
		ResizeOwnership:            protocolResizeOwnership(resizeOwnership),
		ResizeOwnerAttachmentCount: resizeOwnerAttachmentCount,
	}
	t.mu.RUnlock()
	return info
}

func (t *Terminal) listInfoSnapshot(filter ListOptions) (*TerminalInfo, bool) {
	t.attachMu.Lock()
	resizeOwnerAttachmentCount := t.resizeOwnerAttachmentCountLocked()
	resizeOwnership := t.resizeOwnershipSnapshotLocked()
	t.mu.RLock()
	t.attachMu.Unlock()
	if filter.State != nil && t.state != *filter.State {
		t.mu.RUnlock()
		return nil, false
	}
	if !matchTags(t.tags, filter.Tags) {
		t.mu.RUnlock()
		return nil, false
	}
	if cached := t.listInfoCache; cached != nil {
		t.mu.RUnlock()
		return cached, true
	}
	version := t.metadataVersion
	resizeOwnership.Size = t.size
	resizeOwnership.SizeLocked = terminalmeta.SizeLocked(t.tags)
	if terminalmeta.SizeLocked(t.tags) {
		resizeOwnerAttachmentCount = 0
		resizeOwnership.OwnerAttachmentID = ""
		resizeOwnership.OwnerSurfaceID = ""
		resizeOwnership.OwnerViewID = ""
		resizeOwnership.OwnerRemoteAddr = ""
	}
	info := &TerminalInfo{
		ID:                         t.id,
		Name:                       t.name,
		Command:                    append([]string(nil), t.command...),
		Tags:                       copyTags(t.tags),
		Size:                       t.size,
		State:                      t.state,
		CWD:                        t.cwd,
		LiveCWD:                    t.cwd,
		CreatedAt:                  t.createdAt,
		ExitCode:                   copyIntPtr(t.exitCode),
		ResizeOwnership:            cloneResizeOwnership(resizeOwnership),
		ResizeOwnerAttachmentCount: resizeOwnerAttachmentCount,
	}
	t.mu.RUnlock()

	t.mu.Lock()
	// Reuse the deep-copied metadata only if nothing changed while we were
	// building it; otherwise return the fresh snapshot without caching it.
	if t.metadataVersion == version && t.listInfoCache == nil {
		t.listInfoCache = info
	}
	if cached := t.listInfoCache; cached != nil {
		info = cached
	}
	t.mu.Unlock()
	return info, true
}

func normalizeReportedWorkingDirectory(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "file://") {
		if u, err := url.Parse(path); err == nil {
			if decoded, err := url.PathUnescape(u.Path); err == nil && decoded != "" {
				path = decoded
			} else {
				path = u.Path
			}
		}
	}
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return "/"
	}
	return trimmed
}

func cloneRows(rows [][]Cell) [][]Cell {
	out := make([][]Cell, len(rows))
	for i, row := range rows {
		out[i] = append([]Cell(nil), row...)
	}
	return out
}

func cloneTimeSlice(values []time.Time) []time.Time {
	if len(values) == 0 {
		return nil
	}
	return append([]time.Time(nil), values...)
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneBoolSlice(values []bool) []bool {
	if len(values) == 0 {
		return nil
	}
	return append([]bool(nil), values...)
}

func repeatedString(value string, count int) []string {
	if count <= 0 || value == "" {
		return nil
	}
	out := make([]string, count)
	for i := range out {
		out[i] = value
	}
	return out
}

func appendRepeatedString(values []string, value string, count int) []string {
	if count <= 0 || value == "" {
		return values
	}
	for i := 0; i < count; i++ {
		values = append(values, value)
	}
	return values
}

func sliceTimeRange(values []time.Time, start, end int) []time.Time {
	if len(values) == 0 {
		return nil
	}
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(values) {
		start = len(values)
	}
	if end > len(values) {
		end = len(values)
	}
	return cloneTimeSlice(values[start:end])
}

func sliceStringRange(values []string, start, end int) []string {
	if len(values) == 0 {
		return nil
	}
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(values) {
		start = len(values)
	}
	if end > len(values) {
		end = len(values)
	}
	return cloneStringSlice(values[start:end])
}

func sliceBoolRange(values []bool, start, end int) []bool {
	if len(values) == 0 {
		return nil
	}
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(values) {
		start = len(values)
	}
	if end > len(values) {
		end = len(values)
	}
	return cloneBoolSlice(values[start:end])
}

func trimTrailingZeroTimes(values []time.Time, count int) []time.Time {
	if count <= 0 {
		return nil
	}
	if count > len(values) {
		count = len(values)
	}
	return cloneTimeSlice(values[:count])
}

func trimTrailingStrings(values []string, count int) []string {
	if count <= 0 {
		return nil
	}
	if count > len(values) {
		count = len(values)
	}
	return cloneStringSlice(values[:count])
}

func convertScreenData(in vterm.ScreenData) ScreenData {
	return ScreenData{
		Cells:             convertRows(in.Cells),
		IsAlternateScreen: in.IsAlternateScreen,
	}
}

func convertRows(rows [][]vterm.Cell) [][]Cell {
	out := make([][]Cell, len(rows))
	for i, row := range rows {
		out[i] = make([]Cell, len(row))
		for j, cell := range row {
			out[i][j] = Cell{
				Content:    cell.Content,
				Width:      cell.Width,
				LinkURL:    cell.LinkURL,
				LinkParams: cell.LinkParams,
				Style: CellStyle{
					FG:            cell.Style.FG,
					BG:            cell.Style.BG,
					Bold:          cell.Style.Bold,
					Italic:        cell.Style.Italic,
					Underline:     cell.Style.Underline,
					Blink:         cell.Style.Blink,
					Reverse:       cell.Style.Reverse,
					Strikethrough: cell.Style.Strikethrough,
				},
			}
		}
	}
	return out
}

func convertCursorState(in vterm.CursorState) CursorState {
	return CursorState{
		Row:     in.Row,
		Col:     in.Col,
		Visible: in.Visible,
		Shape:   CursorShape(in.Shape),
		Blink:   in.Blink,
	}
}

func convertModes(in vterm.TerminalModes) TerminalModes {
	return TerminalModes{
		AlternateScreen:   in.AlternateScreen,
		MouseTracking:     in.MouseTracking,
		BracketedPaste:    in.BracketedPaste,
		ApplicationCursor: in.ApplicationCursor,
		AutoWrap:          in.AutoWrap,
	}
}

func protocolScreenDataFromVTerm(in vterm.ScreenData) protocol.ScreenData {
	return protocol.ScreenData{
		Cells:             protocolRowsFromVTerm(in.Cells),
		IsAlternateScreen: in.IsAlternateScreen,
	}
}

func protocolRowsFromVTerm(rows [][]vterm.Cell) [][]protocol.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]protocol.Cell, len(rows))
	for i, row := range rows {
		out[i] = protocolCellsFromVTermRow(row)
	}
	return out
}

func protocolRowsFromCore(rows [][]Cell) [][]protocol.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]protocol.Cell, len(rows))
	for i, row := range rows {
		out[i] = protocolCellsFromCoreRow(row)
	}
	return out
}

func protocolCompactRowsFromCorePreserveTrailingBlankCells(rows [][]Cell) []protocol.CompactRow {
	return protocolCompactRowsFromCoreWithOptions(rows, true)
}

func protocolCompactRowsFromCoreWithOptions(rows [][]Cell, preserveTrailingBlankCells bool) []protocol.CompactRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]protocol.CompactRow, len(rows))
	for i, row := range rows {
		out[i] = protocolCompactRowFromCoreWithOptions(row, preserveTrailingBlankCells)
	}
	return out
}

func protocolCompactRowFromCoreWithOptions(row []Cell, preserveTrailingBlankCells bool) protocol.CompactRow {
	last := len(row)
	if !preserveTrailingBlankCells {
		for last > 0 {
			cell := row[last-1]
			if cell.Content != "" && strings.TrimSpace(cell.Content) != "" {
				break
			}
			if cell.Style != (CellStyle{}) {
				break
			}
			if cell.LinkURL != "" || cell.LinkParams != "" {
				break
			}
			last--
		}
	}
	row = row[:last]
	if len(row) == 0 {
		return protocol.CompactRow{}
	}
	var text strings.Builder
	allSimple := true
	allPlain := true
	for _, cell := range row {
		cellText, ok := protocolCompactCoreCellText(cell)
		if !ok {
			allSimple = false
			break
		}
		if cell.Style != (CellStyle{}) {
			allPlain = false
		}
		text.WriteString(cellText)
	}
	if allSimple && allPlain {
		return protocol.CompactRow{Text: text.String()}
	}
	if allSimple {
		runs := make([]protocol.CompactRowRun, 0, 4)
		var runText strings.Builder
		runStyle := row[0].Style
		flushRun := func() {
			if runText.Len() == 0 {
				return
			}
			runs = append(runs, protocol.CompactRowRun{
				Text:  runText.String(),
				Style: protocolCompactRowStyleFromCore(runStyle),
			})
			runText.Reset()
		}
		for _, cell := range row {
			if cell.Style != runStyle {
				flushRun()
				runStyle = cell.Style
			}
			cellText, _ := protocolCompactCoreCellText(cell)
			runText.WriteString(cellText)
		}
		flushRun()
		return protocol.CompactRow{Runs: runs}
	}
	cells := make([]protocol.CompactRowCell, 0, len(row))
	for _, cell := range row {
		cells = append(cells, protocol.CompactRowCell{
			Content: cell.Content,
			Width:   protocolCompactCoreCellWidth(cell),
			Style:   protocolCompactRowStyleFromCore(cell.Style),
		})
	}
	return protocol.CompactRow{Cells: cells}
}

func protocolCompactCoreCellText(cell Cell) (string, bool) {
	if cell.Width > 1 {
		return "", false
	}
	if cell.Content == "" {
		return " ", true
	}
	if utf8.RuneCountInString(cell.Content) != 1 {
		return "", false
	}
	return cell.Content, true
}

func protocolCompactCoreCellWidth(cell Cell) int {
	if cell.Width > 1 {
		return cell.Width
	}
	return 0
}

func protocolCompactRowStyleFromCore(style CellStyle) *protocol.CompactRowStyle {
	if style == (CellStyle{}) {
		return nil
	}
	return &protocol.CompactRowStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

func protocolCellsFromCoreRow(row []Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	out := make([]protocol.Cell, len(row))
	for i, cell := range row {
		out[i] = protocol.Cell{
			Content:    cell.Content,
			Width:      cell.Width,
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
			Style: protocol.CellStyle{
				FG:            cell.Style.FG,
				BG:            cell.Style.BG,
				Bold:          cell.Style.Bold,
				Italic:        cell.Style.Italic,
				Underline:     cell.Style.Underline,
				Blink:         cell.Style.Blink,
				Reverse:       cell.Style.Reverse,
				Strikethrough: cell.Style.Strikethrough,
			},
		}
	}
	return out
}

func protocolGridViewportFromCore(viewport *GridViewport) *protocol.GridViewport {
	if viewport == nil {
		return nil
	}
	rows := protocolCompactRowsFromCorePreserveTrailingBlankCells(viewport.Rows)
	traceGridCoreRows("core.protocol.grid_viewport.input_rows", viewport.TerminalID, viewport.Rows, "offset", viewport.ScrollbackOffset, "limit", viewport.ScrollbackLimit, "total", viewport.ScrollbackTotal, "has_more", viewport.ScrollbackHasMore)
	traceGridProtocolRows("core.protocol.grid_viewport.compact_rows", viewport.TerminalID, rows, "offset", viewport.ScrollbackOffset, "limit", viewport.ScrollbackLimit, "total", viewport.ScrollbackTotal, "has_more", viewport.ScrollbackHasMore)
	return &protocol.GridViewport{
		TerminalID:             viewport.TerminalID,
		Size:                   protocol.Size{Cols: viewport.Size.Cols, Rows: viewport.Size.Rows},
		Rows:                   rows,
		ScrollbackOffset:       viewport.ScrollbackOffset,
		ScrollbackLimit:        viewport.ScrollbackLimit,
		ScrollbackTotal:        viewport.ScrollbackTotal,
		ScrollbackLogicalTotal: viewport.ScrollbackLogicalTotal,
		ScrollbackHasMore:      viewport.ScrollbackHasMore,
		LoadedRows:             viewport.LoadedRows,
		HistoryGeneration:      viewport.HistoryGeneration,
		FirstRowID:             viewport.FirstRowID,
		LastRowID:              viewport.LastRowID,
		ScrollbackTimestamps:   cloneTimeSlice(viewport.ScrollbackTimestamps),
		ScrollbackRowKinds:     cloneStringSlice(viewport.ScrollbackRowKinds),
		ScrollbackWrapped:      cloneBoolSlice(viewport.ScrollbackWrapped),
		RowOwnership:           cloneStringSlice(viewport.RowOwnership),
		Timestamp:              viewport.Timestamp,
	}
}

func protocolSnapshotFromCore(snapshot *Snapshot) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	scrollback := protocolCompactRowsFromCorePreserveTrailingBlankCells(snapshot.Scrollback)
	traceGridCoreRows("core.protocol.snapshot.scrollback_input", snapshot.TerminalID, snapshot.Scrollback, "offset", snapshot.ScrollbackOffset, "total", snapshot.ScrollbackTotal, "has_more", snapshot.ScrollbackHasMore)
	traceGridProtocolRows("core.protocol.snapshot.scrollback_compact", snapshot.TerminalID, scrollback, "offset", snapshot.ScrollbackOffset, "total", snapshot.ScrollbackTotal, "has_more", snapshot.ScrollbackHasMore)
	traceGridCoreRows("core.protocol.snapshot.screen_input", snapshot.TerminalID, snapshot.Screen.Cells, "screen_rows", len(snapshot.Screen.Cells), "screen_cols", int(snapshot.Size.Cols))
	return &protocol.Snapshot{
		TerminalID:             snapshot.TerminalID,
		Size:                   protocol.Size{Cols: snapshot.Size.Cols, Rows: snapshot.Size.Rows},
		Screen:                 protocol.ScreenData{Cells: protocolRowsFromCore(snapshot.Screen.Cells), IsAlternateScreen: snapshot.Screen.IsAlternateScreen},
		Scrollback:             scrollback,
		ScrollbackOffset:       snapshot.ScrollbackOffset,
		ScrollbackTotal:        snapshot.ScrollbackTotal,
		ScrollbackLogicalTotal: snapshot.ScrollbackLogicalTotal,
		ScrollbackHasMore:      snapshot.ScrollbackHasMore,
		ScrollbackLoadedRows:   snapshot.ScrollbackLoadedRows,
		HistoryGeneration:      snapshot.HistoryGeneration,
		ScrollbackFirstRowID:   snapshot.ScrollbackFirstRowID,
		ScrollbackLastRowID:    snapshot.ScrollbackLastRowID,
		ScreenTimestamps:       cloneTimeSlice(snapshot.ScreenTimestamps),
		ScrollbackTimestamps:   cloneTimeSlice(snapshot.ScrollbackTimestamps),
		ScreenRowKinds:         cloneStringSlice(snapshot.ScreenRowKinds),
		ScrollbackRowKinds:     cloneStringSlice(snapshot.ScrollbackRowKinds),
		ScreenWrapped:          cloneBoolSlice(snapshot.ScreenWrapped),
		ScrollbackWrapped:      cloneBoolSlice(snapshot.ScrollbackWrapped),
		ScreenOwnership:        cloneStringSlice(snapshot.ScreenOwnership),
		ScrollbackOwnership:    cloneStringSlice(snapshot.ScrollbackOwnership),
		Cursor: protocol.CursorState{
			Row:     snapshot.Cursor.Row,
			Col:     snapshot.Cursor.Col,
			Visible: snapshot.Cursor.Visible,
			Shape:   string(snapshot.Cursor.Shape),
			Blink:   snapshot.Cursor.Blink,
		},
		Modes: protocol.TerminalModes{
			AlternateScreen:   snapshot.Modes.AlternateScreen,
			AlternateScroll:   snapshot.Modes.AlternateScroll,
			MouseTracking:     snapshot.Modes.MouseTracking,
			BracketedPaste:    snapshot.Modes.BracketedPaste,
			ApplicationCursor: snapshot.Modes.ApplicationCursor,
			AutoWrap:          snapshot.Modes.AutoWrap,
		},
		Timestamp: snapshot.Timestamp,
	}
}

func protocolCellsFromVTermRow(row []vterm.Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	out := make([]protocol.Cell, len(row))
	for i, cell := range row {
		out[i] = protocol.Cell{
			Content:    cell.Content,
			Width:      cell.Width,
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
			Style: protocol.CellStyle{
				FG:            cell.Style.FG,
				BG:            cell.Style.BG,
				Bold:          cell.Style.Bold,
				Italic:        cell.Style.Italic,
				Underline:     cell.Style.Underline,
				Blink:         cell.Style.Blink,
				Reverse:       cell.Style.Reverse,
				Strikethrough: cell.Style.Strikethrough,
			},
		}
	}
	return out
}

func protocolCursorStateFromVTerm(in vterm.CursorState) protocol.CursorState {
	return protocol.CursorState{
		Row:     in.Row,
		Col:     in.Col,
		Visible: in.Visible,
		Shape:   string(in.Shape),
		Blink:   in.Blink,
	}
}

func protocolModesFromVTerm(in vterm.TerminalModes) protocol.TerminalModes {
	return protocol.TerminalModes{
		AlternateScreen:   in.AlternateScreen,
		AlternateScroll:   in.AlternateScroll,
		MouseTracking:     in.MouseTracking,
		BracketedPaste:    in.BracketedPaste,
		ApplicationCursor: in.ApplicationCursor,
		AutoWrap:          in.AutoWrap,
	}
}
