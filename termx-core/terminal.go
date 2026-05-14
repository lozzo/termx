package termx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/termx-core/fanout"
	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-core/protocol"
	ptymgr "github.com/lozzow/termx/termx-core/pty"
	"github.com/lozzow/termx/termx-core/terminalmeta"
	"github.com/lozzow/termx/termx-core/vterm"
)

var terminalIDCounter atomic.Uint64

const (
	terminalPTYReadBufferBytes     = 512 * 1024
	terminalPTYParserQueueBytes    = 2 * 1024 * 1024
	terminalPTYParserChunkMaxBytes = 1024 * 1024
	terminalPTYParserFlushDelay    = 2 * time.Millisecond
	terminalPTYParserCloseWait     = 10 * time.Second
)

type terminalConfig struct {
	ID             string
	Name           string
	Command        []string
	Tags           map[string]string
	Size           Size
	Dir            string
	Env            []string
	ScrollbackSize int
	KeepAfterExit  time.Duration
	GridRoot       string
	Logger         *slog.Logger
	RemoveFunc     func(string, string)
	UpdateFunc     func()
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
	streamMu sync.Mutex

	// These caches hold deep-copied metadata snapshots so hot read paths do not
	// have to rebuild command/tag payloads for every request.
	protocolInfoCache json.RawMessage
	listInfoCache     *TerminalInfo
	metadataVersion   uint64

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
		gridAppender:   newTerminalGridAppender(grid, cfg.ID, cfg.Logger),
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

func forwardLiveStreamMessages(ctx context.Context, src <-chan fanout.StreamMessage, dst chan<- StreamMessage) {
	for {
		msg, ok := <-src
		if !ok {
			return
		}
		if !sendFanoutStreamMessage(ctx, dst, msg, nil) {
			return
		}
	}
}

func (t *Terminal) forwardTerminalStreamMessages(ctx context.Context, src <-chan fanout.StreamMessage, dst chan<- StreamMessage) {
	forwardTerminalStreamMessagesImmediate(ctx, src, dst, t.screenSnapshotFallbackMessage)
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
			msg, pending = collapseTerminalStreamScreenUpdates(src, msg)
		}
		if !sendFanoutStreamMessage(ctx, dst, msg, fallback) {
			return
		}
	}
}

func collapseTerminalStreamScreenUpdates(src <-chan fanout.StreamMessage, first fanout.StreamMessage) (fanout.StreamMessage, *fanout.StreamMessage) {
	msg := first
	collapsed := 0
	for {
		select {
		case next, ok := <-src:
			if !ok {
				if collapsed > 0 {
					perftrace.Count("terminal.stream.screen_update.coalesced", collapsed)
				}
				return msg, nil
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
			return msg, &pending
		default:
			if collapsed > 0 {
				perftrace.Count("terminal.stream.screen_update.coalesced", collapsed)
			}
			return msg, nil
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
	t.size = Size{Cols: cols, Rows: rows}
	t.invalidateProtocolInfoCacheLocked()
	t.mu.Unlock()

	t.streamMu.Lock()
	if err := t.pty.Resize(cols, rows); err != nil {
		t.streamMu.Unlock()
		return err
	}
	t.vterm.Resize(int(cols), int(rows))
	t.broadcastResizeLocked(t.stream, cols, rows)
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
	preservedScrollback, preservedScrollbackTimestamps, preservedScrollbackRowKinds, preservedScrollbackWrapped := restartPreservedRows(t.vterm)
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

func restartPreservedRows(vt *vterm.VTerm) ([][]vterm.Cell, []time.Time, []string, []bool) {
	if vt == nil {
		return nil, nil, nil, nil
	}
	scrollback := vt.ScrollbackContent()
	scrollbackTimestamps := vt.ScrollbackTimestamps()
	scrollbackRowKinds := vt.ScrollbackRowKinds()
	scrollbackWrapped := vt.ScrollbackWrapped()
	screen := trimTrailingBlankVTermRows(vt.ScreenContent().Cells)
	screenTimestamps := trimTrailingZeroTimes(vt.ScreenTimestamps(), len(screen))
	screenRowKinds := trimTrailingStrings(vt.ScreenRowKinds(), len(screen))
	screenWrapped := trimBoolSlice(vt.ScreenWrapped(), len(screen))
	restartAt := time.Now().UTC()
	if len(screen) == 0 {
		out := append([][]vterm.Cell(nil), scrollback...)
		timestamps := append([]time.Time(nil), scrollbackTimestamps...)
		rowKinds := append([]string(nil), scrollbackRowKinds...)
		wrapped := append([]bool(nil), scrollbackWrapped...)
		return appendRestartMarker(out, timestamps, rowKinds, wrapped, restartAt)
	}
	out := make([][]vterm.Cell, 0, len(scrollback)+len(screen))
	out = append(out, scrollback...)
	out = append(out, screen...)
	timestamps := make([]time.Time, 0, len(scrollbackTimestamps)+len(screenTimestamps))
	timestamps = append(timestamps, scrollbackTimestamps...)
	timestamps = append(timestamps, screenTimestamps...)
	rowKinds := make([]string, 0, len(scrollbackRowKinds)+len(screenRowKinds))
	rowKinds = append(rowKinds, scrollbackRowKinds...)
	rowKinds = append(rowKinds, screenRowKinds...)
	wrapped := make([]bool, 0, len(scrollbackWrapped)+len(screenWrapped))
	wrapped = append(wrapped, scrollbackWrapped...)
	wrapped = append(wrapped, screenWrapped...)
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
	vt.LoadSnapshotWithExtendedMetadata(scrollback, scrollbackTimestamps, scrollbackRowKinds, scrollbackWrapped, screen, nil, nil, nil, vt.CursorState(), vt.Modes())
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
	t.mu.RUnlock()
	screenTimestamps := t.vterm.ScreenTimestamps()
	screenRowKinds := t.vterm.ScreenRowKinds()
	screenWrapped := t.vterm.ScreenWrapped()

	var (
		outScrollback           [][]Cell
		outScrollbackTimestamps []time.Time
		outScrollbackRowKinds   []string
		outScrollbackWrapped    []bool
		scrollbackTotal         int
		scrollbackHasMore       bool
		usedGrid                bool
	)
	if t.grid != nil && limit > 0 {
		gridViewport, err := t.grid.Viewport(offset, limit, int(size.Cols))
		if err != nil {
			if t.logger != nil {
				t.logger.Warn("termx terminal grid snapshot failed", "terminal_id", id, "error", err)
			}
		} else if gridViewport.TotalRows > 0 {
			outScrollback = convertRows(gridViewport.Rows)
			outScrollbackTimestamps = cloneTimeSlice(gridViewport.Timestamps)
			outScrollbackRowKinds = cloneStringSlice(gridViewport.RowKinds)
			outScrollbackWrapped = cloneBoolSlice(gridViewport.Wrapped)
			scrollbackTotal = gridViewport.TotalRows
			scrollbackHasMore = gridViewport.HasMore
			usedGrid = true
		}
	} else if t.grid != nil {
		scrollbackTotal = t.grid.RowCount()
		scrollbackHasMore = offset < scrollbackTotal
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
		scrollbackTotal = len(scrollback)
		scrollbackHasMore = start > 0
	}

	return &Snapshot{
		TerminalID:           id,
		Size:                 size,
		Screen:               convertScreenData(t.vterm.ScreenContent()),
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
		Cursor:               convertCursorState(t.vterm.CursorState()),
		Modes:                convertModes(t.vterm.Modes()),
		Timestamp:            time.Now().UTC(),
	}
}

func (t *Terminal) GridViewport(offset, limit, cols int) *GridViewport {
	if t == nil || t.vterm == nil {
		return nil
	}
	t.flushGridAppender()
	if limit <= 0 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	t.mu.RLock()
	size := t.size
	id := t.id
	t.mu.RUnlock()
	if cols <= 0 {
		cols = int(size.Cols)
	}
	if cols <= 0 {
		cols = 80
	}
	if t.grid != nil {
		gridViewport, err := t.grid.Viewport(offset, limit, cols)
		if err != nil {
			if t.logger != nil {
				t.logger.Warn("termx terminal grid viewport failed", "terminal_id", id, "error", err)
			}
		} else if gridViewport.TotalRows > 0 {
			return &GridViewport{
				TerminalID:           id,
				Size:                 Size{Cols: uint16(cols), Rows: size.Rows},
				Rows:                 convertRows(gridViewport.Rows),
				ScrollbackOffset:     offset,
				ScrollbackLimit:      limit,
				ScrollbackTotal:      gridViewport.TotalRows,
				ScrollbackHasMore:    gridViewport.HasMore,
				ScrollbackTimestamps: cloneTimeSlice(gridViewport.Timestamps),
				ScrollbackRowKinds:   cloneStringSlice(gridViewport.RowKinds),
				ScrollbackWrapped:    cloneBoolSlice(gridViewport.Wrapped),
				Timestamp:            time.Now().UTC(),
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
	return &GridViewport{
		TerminalID:           id,
		Size:                 Size{Cols: uint16(cols), Rows: size.Rows},
		Rows:                 convertRows(scrollback[start:end]),
		ScrollbackOffset:     offset,
		ScrollbackLimit:      limit,
		ScrollbackTotal:      len(scrollback),
		ScrollbackHasMore:    start > 0,
		ScrollbackTimestamps: sliceTimeRange(t.vterm.ScrollbackTimestamps(), start, end),
		ScrollbackRowKinds:   sliceStringRange(t.vterm.ScrollbackRowKinds(), start, end),
		ScrollbackWrapped:    sliceBoolRange(t.vterm.ScrollbackWrapped(), start, end),
		Timestamp:            time.Now().UTC(),
	}
}

func (t *Terminal) HistoryReplay(opts HistoryReplayOptions) HistoryReplayResult {
	if t == nil || t.vterm == nil {
		return HistoryReplayResult{}
	}
	t.flushGridAppender()
	beforeOffset, limit := sanitizeGridReplayWindow(opts.BeforeOffset, opts.Limit)
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
	t.mu.RLock()
	id := t.id
	t.mu.RUnlock()
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

func (t *Terminal) hasCollaboratorAttachmentLocked() bool {
	for _, info := range t.attachments {
		if info.Mode == string(ModeCollaborator) {
			return true
		}
	}
	return false
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
	t.appendGridFromDamageLocked(damage)
	stream.BroadcastMessage(fanout.StreamMessage{Type: fanout.StreamScreenUpdate})
}

func (t *Terminal) appendGridFromDamageLocked(damage vterm.WriteDamage) {
	if t == nil || t.grid == nil || len(damage.ScrollbackAppend) == 0 {
		return
	}
	if t.gridAppender != nil {
		t.gridAppender.append(damage.ScrollbackAppend)
		return
	}
	if err := t.grid.AppendDamageRows(damage.ScrollbackAppend); err != nil && t.logger != nil {
		t.logger.Warn("termx terminal grid append failed", "terminal_id", t.id, "error", err)
	}
}

func (t *Terminal) flushGridAppender() {
	if t == nil || t.gridAppender == nil {
		return
	}
	t.gridAppender.flush()
}

func (t *Terminal) screenInvalidationMessage() StreamMessage {
	return StreamMessage{
		Type:   StreamScreenUpdate,
		Latest: t.screenSnapshotFallbackMessage,
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
		t.streamMu.Lock()
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
		msgs = append(msgs, StreamMessage{Type: StreamScreenUpdate, Payload: payload})
	}
	msgs = append(msgs, StreamMessage{Type: StreamBootstrapDone})
	return msgs
}

func (t *Terminal) screenSnapshotFallbackMessage() StreamMessage {
	if t == nil {
		return StreamMessage{Type: StreamSyncLost}
	}
	payload, ok := t.screenSnapshotPayloadLocked()
	if !ok {
		return StreamMessage{Type: StreamSyncLost}
	}
	return StreamMessage{Type: StreamScreenUpdate, Payload: payload}
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
		fullUpdate := screenFullReplaceUpdateFromVTerm(t.vterm, t.currentTitleLocked())
		encodeFinish := perftrace.Measure("terminal.screen_update.encode")
		payload, err := protocol.EncodeScreenUpdatePayload(fullUpdate)
		encodeFinish(len(payload))
		if err != nil {
			return nil, false
		}
		recordEncodedScreenUpdatePayload(screenUpdateEncodeModeFullReplace, payload)
		return payload, true
	}
	if screenUpdateShouldEncodeDeltaOnly(deltaUpdate, damage.Modes.AlternateScreen) {
		perftrace.Count("terminal.screen_update.delta_only_shortcut", 0)
		encodeFinish := perftrace.Measure("terminal.screen_update.encode")
		payload, err := protocol.EncodeScreenUpdatePayload(deltaUpdate)
		encodeFinish(len(payload))
		if err == nil {
			recordEncodedScreenUpdatePayload(screenUpdateEncodeModeDelta, payload)
			return payload, true
		}
		perftrace.Count("terminal.screen_update.delta_only_encode_error", 0)
	}
	perftrace.Count("terminal.screen_update.requires_full_snapshot", 0)
	fullUpdate := screenFullReplaceUpdateFromVTerm(t.vterm, t.currentTitleLocked())
	payload, _, ok := encodeScreenUpdatePayloadByStrategy(deltaUpdate, fullUpdate, fullUpdate.Modes.AlternateScreen)
	if !ok {
		return nil, false
	}
	return payload, true
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

func (t *Terminal) currentScreenStreamStateLocked() *streamScreenState {
	finish := perftrace.Measure("terminal.screen_update.screen_snapshot")
	defer finish(0)
	if t == nil || t.vterm == nil {
		return nil
	}
	return &streamScreenState{
		snapshot: screenSnapshotFromVTerm(t.vterm),
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
	t.protocolInfoCache = nil
	t.listInfoCache = nil
	t.metadataVersion++
}

func (t *Terminal) protocolInfoJSON() (json.RawMessage, error) {
	t.attachMu.Lock()
	resizeOwnerAttachmentCount := t.resizeOwnerAttachmentCountLocked()
	resizeOwnership := t.resizeOwnershipSnapshotLocked()
	t.mu.RLock()
	t.attachMu.Unlock()
	if cached := t.protocolInfoCache; cached != nil {
		t.mu.RUnlock()
		return cached, nil
	}
	version := t.metadataVersion
	resizeOwnership.Size = Size{Cols: t.size.Cols, Rows: t.size.Rows}
	resizeOwnership.SizeLocked = terminalmeta.SizeLocked(t.tags)
	if terminalmeta.SizeLocked(t.tags) {
		resizeOwnerAttachmentCount = 0
		resizeOwnership.OwnerAttachmentID = ""
		resizeOwnership.OwnerSurfaceID = ""
		resizeOwnership.OwnerViewID = ""
		resizeOwnership.OwnerRemoteAddr = ""
	}

	// Marshal under the read lock so command/tags/state stay consistent and we
	// can safely reuse internal metadata without allocating fresh copies.
	data, err := json.Marshal(protocol.TerminalInfo{
		ID:                         t.id,
		Name:                       t.name,
		Command:                    t.command,
		Tags:                       t.tags,
		Size:                       protocol.Size{Cols: t.size.Cols, Rows: t.size.Rows},
		State:                      string(t.state),
		CWD:                        t.cwd,
		LiveCWD:                    t.cwd,
		CreatedAt:                  t.createdAt,
		ExitCode:                   t.exitCode,
		ResizeOwnership:            protocolResizeOwnership(resizeOwnership),
		ResizeOwnerAttachmentCount: resizeOwnerAttachmentCount,
	})
	t.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	if t.metadataVersion == version && t.protocolInfoCache == nil {
		t.protocolInfoCache = data
	}
	cached := t.protocolInfoCache
	t.mu.Unlock()
	if cached == nil {
		return data, nil
	}
	return cached, nil
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
				Content: cell.Content,
				Width:   cell.Width,
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

func protocolCompactRowsFromCore(rows [][]Cell) []protocol.CompactRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]protocol.CompactRow, len(rows))
	for i, row := range rows {
		out[i] = protocol.CompactRowFromCells(protocolCellsFromCoreRow(row))
	}
	return out
}

func protocolCellsFromCoreRow(row []Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	out := make([]protocol.Cell, len(row))
	for i, cell := range row {
		out[i] = protocol.Cell{
			Content: cell.Content,
			Width:   cell.Width,
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
	return &protocol.GridViewport{
		TerminalID:           viewport.TerminalID,
		Size:                 protocol.Size{Cols: viewport.Size.Cols, Rows: viewport.Size.Rows},
		Rows:                 protocolCompactRowsFromCore(viewport.Rows),
		ScrollbackOffset:     viewport.ScrollbackOffset,
		ScrollbackLimit:      viewport.ScrollbackLimit,
		ScrollbackTotal:      viewport.ScrollbackTotal,
		ScrollbackHasMore:    viewport.ScrollbackHasMore,
		ScrollbackTimestamps: cloneTimeSlice(viewport.ScrollbackTimestamps),
		ScrollbackRowKinds:   cloneStringSlice(viewport.ScrollbackRowKinds),
		ScrollbackWrapped:    cloneBoolSlice(viewport.ScrollbackWrapped),
		Timestamp:            viewport.Timestamp,
	}
}

func protocolCellsFromVTermRow(row []vterm.Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	out := make([]protocol.Cell, len(row))
	for i, cell := range row {
		out[i] = protocol.Cell{
			Content: cell.Content,
			Width:   cell.Width,
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
