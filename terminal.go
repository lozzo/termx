package termx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/fanout"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	ptymgr "github.com/lozzow/termx/pty"
	"github.com/lozzow/termx/terminalmeta"
	"github.com/lozzow/termx/vterm"
)

var terminalIDCounter atomic.Uint64

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
	RemoveFunc     func(string, string)
	UpdateFunc     func()
}

type Terminal struct {
	events *EventBus
	pty    *ptymgr.PTY
	vterm  *vterm.VTerm
	stream *fanout.Fanout

	mu             sync.RWMutex
	id             string
	name           string
	command        []string
	tags           map[string]string
	size           Size
	dir            string
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

	attachMu    sync.Mutex
	attachments map[string]AttachInfo

	pendingVTermEpoch      uint64
	pendingVTermOutput     []byte
	pendingVTermFlushTimer *time.Timer

	done     chan struct{}
	readDone chan struct{}
}

const attachReplayScrollbackLimit = 0
const serverVTermFlushThreshold = 256 * 1024

var serverVTermFlushIdleDelay = 2 * time.Millisecond

func newTerminal(ctx context.Context, events *EventBus, cfg terminalConfig) (*Terminal, error) {
	p, vt, err := spawnTerminalProcess(cfg)
	if err != nil {
		return nil, err
	}
	t := &Terminal{
		events:         events,
		pty:            p,
		vterm:          vt,
		stream:         fanout.New(),
		id:             cfg.ID,
		name:           cfg.Name,
		command:        append([]string(nil), cfg.Command...),
		tags:           copyTags(cfg.Tags),
		size:           cfg.Size,
		dir:            cfg.Dir,
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

	t.events.Publish(Event{
		Type:       EventTerminalCreated,
		TerminalID: t.id,
		Timestamp:  time.Now().UTC(),
		Created: &TerminalCreatedData{
			Name:    t.name,
			Command: append([]string(nil), t.command...),
			Size:    t.size,
		},
	})

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
	vt := vterm.New(int(cfg.Size.Cols), int(cfg.Size.Rows), cfg.ScrollbackSize, func(data []byte) {
		// Forward emulator responses (e.g. DSR cursor position) to the PTY
		// so the child process receives them.
		_, _ = p.Write(data)
	})
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
	return &snapshot
}

func (t *Terminal) Done() <-chan struct{} {
	return t.done
}

func (t *Terminal) Subscribe(ctx context.Context) <-chan StreamMessage {
	t.streamMu.Lock()
	t.flushPendingVTermOutputLocked()
	bootstrap := t.bootstrapMessagesLocked(attachReplayScrollbackLimit)
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
		for _, msg := range bootstrap {
			select {
			case <-ctx.Done():
				return
			case dst <- cloneTerminalStreamMessage(msg):
			}
		}
		t.forwardTerminalStreamMessages(ctx, src, dst)
	}()
	return dst
}

const maxMergedLiveOutputBytes = protocol.MaxFrameSize

func forwardLiveStreamMessages(ctx context.Context, src <-chan fanout.StreamMessage, dst chan<- StreamMessage) {
	var (
		pending    fanout.StreamMessage
		hasPending bool
	)
	for {
		msg, ok := nextLiveStreamMessage(src, &pending, &hasPending)
		if !ok {
			return
		}
		if msg.Type != fanout.StreamOutput && msg.Type != fanout.StreamSyncLost {
			select {
			case <-ctx.Done():
				return
			case dst <- cloneFanoutStreamMessage(msg):
			}
			continue
		}
		batch, next, nextOK := coalesceLiveStreamMessages(msg, src)
		for _, out := range batch {
			select {
			case <-ctx.Done():
				return
			case dst <- out:
			}
		}
		if !nextOK {
			return
		}
		pending = next
		hasPending = true
	}
}

func (t *Terminal) forwardTerminalStreamMessages(ctx context.Context, src <-chan fanout.StreamMessage, dst chan<- StreamMessage) {
	var (
		pending    fanout.StreamMessage
		hasPending bool
	)
	for {
		msg, ok := nextLiveStreamMessage(src, &pending, &hasPending)
		if !ok {
			return
		}
		if msg.Type != fanout.StreamOutput && msg.Type != fanout.StreamSyncLost {
			select {
			case <-ctx.Done():
				return
			case dst <- cloneFanoutStreamMessage(msg):
			}
			continue
		}
		batch, next, nextOK := coalesceLiveStreamMessages(msg, src)
		for _, out := range batch {
			if out.Type == StreamSyncLost {
				out = t.screenSnapshotFallbackMessage()
			}
			select {
			case <-ctx.Done():
				return
			case dst <- out:
			}
		}
		if !nextOK {
			return
		}
		pending = next
		hasPending = true
	}
}

func nextLiveStreamMessage(src <-chan fanout.StreamMessage, pending *fanout.StreamMessage, hasPending *bool) (fanout.StreamMessage, bool) {
	if hasPending != nil && *hasPending {
		*hasPending = false
		return *pending, true
	}
	msg, ok := <-src
	return msg, ok
}

func coalesceLiveStreamMessages(first fanout.StreamMessage, src <-chan fanout.StreamMessage) ([]StreamMessage, fanout.StreamMessage, bool) {
	batch := make([]StreamMessage, 0, 4)
	var (
		output       []byte
		outputShared bool
		dropped      uint64
	)
	flushOutput := func() {
		if len(output) == 0 {
			return
		}
		batch = append(batch, StreamMessage{Type: StreamOutput, Output: output})
		output = nil
		outputShared = false
	}
	flushDropped := func() {
		if dropped == 0 {
			return
		}
		batch = append(batch, StreamMessage{Type: StreamSyncLost, DroppedBytes: dropped})
		dropped = 0
	}
	handle := func(msg fanout.StreamMessage) (fanout.StreamMessage, bool, bool) {
		switch msg.Type {
		case fanout.StreamOutput:
			if dropped > 0 {
				flushDropped()
			}
			if len(output) > 0 && len(output)+len(msg.Output) > maxMergedLiveOutputBytes {
				flushOutput()
			}
			if len(output) == 0 {
				output = msg.Output
				outputShared = true
				return fanout.StreamMessage{}, false, true
			}
			if outputShared {
				merged := make([]byte, 0, len(output)+len(msg.Output))
				merged = append(merged, output...)
				merged = append(merged, msg.Output...)
				output = merged
				outputShared = false
				return fanout.StreamMessage{}, false, true
			}
			output = append(output, msg.Output...)
			return fanout.StreamMessage{}, false, true
		case fanout.StreamSyncLost:
			flushOutput()
			dropped += msg.DroppedBytes
			return fanout.StreamMessage{}, false, true
		default:
			flushOutput()
			flushDropped()
			return msg, true, true
		}
	}
	if pending, hasPending, ok := handle(first); hasPending || !ok {
		if !ok {
			return batch, fanout.StreamMessage{}, false
		}
		return batch, pending, true
	}
	for {
		select {
		case msg, ok := <-src:
			if !ok {
				flushOutput()
				flushDropped()
				return batch, fanout.StreamMessage{}, false
			}
			if pending, hasPending, ok := handle(msg); hasPending || !ok {
				if !ok {
					return batch, fanout.StreamMessage{}, false
				}
				return batch, pending, true
			}
		default:
			flushOutput()
			flushDropped()
			return batch, fanout.StreamMessage{}, true
		}
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
	if terminalmeta.SizeLocked(t.tags) {
		t.mu.Unlock()
		return fmt.Errorf("%w: terminal %q size is locked", ErrPermissionDenied, t.id)
	}
	old := t.size
	t.size = Size{Cols: cols, Rows: rows}
	t.invalidateProtocolInfoCacheLocked()
	t.mu.Unlock()

	t.streamMu.Lock()
	if err := t.pty.Resize(cols, rows); err != nil {
		t.streamMu.Unlock()
		return err
	}
	t.flushPendingVTermOutputLocked()
	t.vterm.Resize(int(cols), int(rows))
	t.stream.BroadcastResize(cols, rows)
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
	return t.pty.Close()
}

func (t *Terminal) Restart() error {
	t.flushPendingVTermOutput(0)
	t.mu.Lock()
	if t.state != StateExited {
		t.mu.Unlock()
		return ErrTerminalNotExited
	}
	preservedScrollback, preservedScrollbackTimestamps, preservedScrollbackRowKinds := restartPreservedScrollback(t.vterm)
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

	p, vt, err := spawnTerminalProcess(cfg)
	if err != nil {
		return err
	}
	seedRestartScrollback(vt, preservedScrollback, preservedScrollbackTimestamps, preservedScrollbackRowKinds)

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

func restartPreservedScrollback(vt *vterm.VTerm) ([][]vterm.Cell, []time.Time, []string) {
	if vt == nil {
		return nil, nil, nil
	}
	scrollback := vt.ScrollbackContent()
	scrollbackTimestamps := vt.ScrollbackTimestamps()
	scrollbackRowKinds := vt.ScrollbackRowKinds()
	screen := trimTrailingBlankVTermRows(vt.ScreenContent().Cells)
	screenTimestamps := trimTrailingZeroTimes(vt.ScreenTimestamps(), len(screen))
	screenRowKinds := trimTrailingStrings(vt.ScreenRowKinds(), len(screen))
	restartAt := time.Now().UTC()
	if len(screen) == 0 {
		out := append([][]vterm.Cell(nil), scrollback...)
		timestamps := append([]time.Time(nil), scrollbackTimestamps...)
		rowKinds := append([]string(nil), scrollbackRowKinds...)
		return appendRestartMarker(out, timestamps, rowKinds, restartAt)
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
	return appendRestartMarker(out, timestamps, rowKinds, restartAt)
}

func appendRestartMarker(rows [][]vterm.Cell, timestamps []time.Time, rowKinds []string, restartAt time.Time) ([][]vterm.Cell, []time.Time, []string) {
	rows = append(rows, nil)
	timestamps = append(timestamps, restartAt)
	rowKinds = append(rowKinds, SnapshotRowKindRestart)
	return rows, timestamps, rowKinds
}

func seedRestartScrollback(vt *vterm.VTerm, scrollback [][]vterm.Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string) {
	if vt == nil || len(scrollback) == 0 {
		return
	}
	screen := vt.ScreenContent()
	vt.LoadSnapshotWithMetadata(scrollback, scrollbackTimestamps, scrollbackRowKinds, screen, nil, nil, vt.CursorState(), vt.Modes())
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
	t.flushPendingVTermOutput(0)
	if limit <= 0 {
		limit = 500
	}
	scrollback := t.vterm.ScrollbackContent()
	if offset < 0 {
		offset = 0
	}
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

	t.mu.RLock()
	size := t.size
	id := t.id
	t.mu.RUnlock()
	scrollbackTimestamps := t.vterm.ScrollbackTimestamps()
	screenTimestamps := t.vterm.ScreenTimestamps()
	scrollbackRowKinds := t.vterm.ScrollbackRowKinds()
	screenRowKinds := t.vterm.ScreenRowKinds()

	return &Snapshot{
		TerminalID:           id,
		Size:                 size,
		Screen:               convertScreenData(t.vterm.ScreenContent()),
		Scrollback:           convertRows(scrollback[start:end]),
		ScreenTimestamps:     cloneTimeSlice(screenTimestamps),
		ScrollbackTimestamps: sliceTimeRange(scrollbackTimestamps, start, end),
		ScreenRowKinds:       cloneStringSlice(screenRowKinds),
		ScrollbackRowKinds:   sliceStringRange(scrollbackRowKinds, start, end),
		Cursor:               convertCursorState(t.vterm.CursorState()),
		Modes:                convertModes(t.vterm.Modes()),
		Timestamp:            time.Now().UTC(),
	}
}

func (t *Terminal) SetTags(tags map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()
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
}

func (t *Terminal) SetMetadata(name string, tags map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		t.name = trimmed
	}
	t.tags = copyTags(tags)
	t.invalidateProtocolInfoCacheLocked()
}

func (t *Terminal) AddAttachment(id, remote string, mode AttachMode) {
	t.attachMu.Lock()
	defer t.attachMu.Unlock()
	t.attachments[id] = AttachInfo{
		RemoteAddr: remote,
		Mode:       string(mode),
		AttachedAt: time.Now().UTC(),
	}
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

func (t *Terminal) RemoveAttachment(id string) {
	t.attachMu.Lock()
	defer t.attachMu.Unlock()
	delete(t.attachments, id)
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
	defer t.attachMu.Unlock()

	revoked := 0
	for id, info := range t.attachments {
		if info.Mode != string(ModeCollaborator) {
			continue
		}
		info.Mode = string(ModeObserver)
		t.attachments[id] = info
		revoked++
	}
	return revoked
}

func (t *Terminal) startProcessLoops() {
	t.mu.RLock()
	epoch := t.processEpoch
	p := t.pty
	stream := t.stream
	readDone := t.readDone
	done := t.done
	t.mu.RUnlock()
	go t.readLoop(epoch, p, stream, readDone)
	go t.waitLoop(epoch, p, stream, readDone, done)
}

func (t *Terminal) queuePendingVTermOutputLocked(epoch uint64, chunk []byte) {
	if t == nil || len(chunk) == 0 {
		return
	}
	if t.pendingVTermEpoch != epoch {
		t.clearPendingVTermOutputLocked()
		t.pendingVTermEpoch = epoch
	}
	t.pendingVTermOutput = append(t.pendingVTermOutput, chunk...)
	if len(t.pendingVTermOutput) >= serverVTermFlushThreshold || len(t.pendingVTermOutput) >= protocol.MaxFrameSize {
		t.flushPendingVTermOutputLocked()
	}
}

func (t *Terminal) armPendingVTermFlushLocked(epoch uint64) {
	if t == nil || len(t.pendingVTermOutput) == 0 || serverVTermFlushIdleDelay <= 0 {
		return
	}
	t.stopPendingVTermFlushTimerLocked()
	t.pendingVTermFlushTimer = time.AfterFunc(serverVTermFlushIdleDelay, func() {
		t.flushPendingVTermOutput(epoch)
	})
}

func (t *Terminal) flushPendingVTermOutput(epoch uint64) {
	if t == nil {
		return
	}
	t.streamMu.Lock()
	defer t.streamMu.Unlock()
	if epoch != 0 && t.pendingVTermEpoch != 0 && t.pendingVTermEpoch != epoch {
		return
	}
	t.flushPendingVTermOutputLocked()
}

func (t *Terminal) flushPendingVTermOutputLocked() {
	if t == nil {
		return
	}
	t.stopPendingVTermFlushTimerLocked()
	if len(t.pendingVTermOutput) == 0 {
		t.pendingVTermEpoch = 0
		return
	}
	output := append([]byte(nil), t.pendingVTermOutput...)
	t.pendingVTermOutput = t.pendingVTermOutput[:0]
	t.pendingVTermEpoch = 0
	if t.vterm != nil {
		_, _, damage := t.vterm.WriteWithDamage(output)
		if t.stream != nil {
			if payload, ok := t.screenUpdatePayloadFromDamageLocked(damage); ok {
				t.stream.BroadcastMessage(fanout.StreamMessage{Type: fanout.StreamScreenUpdate, Payload: payload})
			}
		}
	}
}

func (t *Terminal) clearPendingVTermOutputLocked() {
	if t == nil {
		return
	}
	t.stopPendingVTermFlushTimerLocked()
	t.pendingVTermOutput = nil
	t.pendingVTermEpoch = 0
}

func (t *Terminal) stopPendingVTermFlushTimerLocked() {
	if t == nil || t.pendingVTermFlushTimer == nil {
		return
	}
	t.pendingVTermFlushTimer.Stop()
	t.pendingVTermFlushTimer = nil
}

func (t *Terminal) readLoop(epoch uint64, p *ptymgr.PTY, stream *fanout.Fanout, readDone chan struct{}) {
	defer close(readDone)
	buf := make([]byte, 32*1024)
	for {
		n, err := p.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			t.streamMu.Lock()
			t.queuePendingVTermOutputLocked(epoch, chunk)
			t.armPendingVTermFlushLocked(epoch)
			t.streamMu.Unlock()
		}
		if err != nil {
			t.mu.RLock()
			removed := t.removed
			currentEpoch := t.processEpoch
			t.mu.RUnlock()
			if currentEpoch != epoch {
				return
			}
			if err != io.EOF {
				if removed {
					return
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

func (t *Terminal) waitLoop(epoch uint64, p *ptymgr.PTY, stream *fanout.Fanout, readDone <-chan struct{}, done chan struct{}) {
	<-p.Wait()
	code := p.ExitCode()

	select {
	case <-readDone:
	case <-time.After(500 * time.Millisecond):
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
	t.flushPendingVTermOutputLocked()
	stream.Close(&code)
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

func (t *Terminal) bootstrapMessagesLocked(scrollbackLimit int) []StreamMessage {
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
	if payload, ok := t.screenSnapshotPayloadLocked(scrollbackLimit == 0); ok {
		msgs = append(msgs, StreamMessage{Type: StreamScreenUpdate, Payload: payload})
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
	payload, ok := t.screenSnapshotPayloadLocked(true)
	if !ok {
		return StreamMessage{Type: StreamSyncLost}
	}
	return StreamMessage{Type: StreamScreenUpdate, Payload: payload}
}

func (t *Terminal) screenSnapshotPayloadLocked(resetScrollback bool) ([]byte, bool) {
	if t == nil || t.vterm == nil {
		return nil, false
	}
	state := t.currentStreamScreenStateLocked()
	if state == nil || state.snapshot == nil {
		return nil, false
	}
	update := fullReplaceUpdateForStateDelta(nil, state, resetScrollback)
	payload, err := protocol.EncodeScreenUpdatePayload(update)
	if err != nil {
		return nil, false
	}
	perftrace.Count("terminal.screen_update.encoded_bytes", len(payload))
	return payload, true
}

func (t *Terminal) screenUpdatePayloadFromDamageLocked(damage vterm.WriteDamage) ([]byte, bool) {
	if t == nil || t.vterm == nil {
		return nil, false
	}
	state := t.currentStreamScreenStateLocked()
	if state == nil || state.snapshot == nil {
		return nil, false
	}
	deltaUpdate := screenUpdateFromDamageState(damage, state)
	fullUpdate := fullReplaceUpdateForStateDelta(nil, state, !state.snapshot.Modes.AlternateScreen)
	if state.snapshot.Modes.AlternateScreen {
		fullUpdate.ResetScrollback = false
		fullUpdate.ScrollbackTrim = deltaUpdate.ScrollbackTrim
		fullUpdate.ScrollbackAppend = append([]protocol.ScrollbackRowAppend(nil), deltaUpdate.ScrollbackAppend...)
	}
	payload, _, ok := encodeScreenUpdatePayloadByStrategy(deltaUpdate, fullUpdate, state.snapshot.Modes.AlternateScreen)
	if !ok {
		return nil, false
	}
	return payload, true
}

func (t *Terminal) currentStreamScreenStateLocked() *streamScreenState {
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
		Output:       append([]byte(nil), msg.Output...),
		Payload:      append([]byte(nil), msg.Payload...),
		DroppedBytes: msg.DroppedBytes,
		ExitCode:     copyIntPtr(msg.ExitCode),
		Cols:         msg.Cols,
		Rows:         msg.Rows,
	}
}

func cloneFanoutStreamMessage(msg fanout.StreamMessage) StreamMessage {
	return StreamMessage{
		Type:         StreamMessageType(msg.Type),
		Output:       append([]byte(nil), msg.Output...),
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
	t.mu.RLock()
	if cached := t.protocolInfoCache; cached != nil {
		t.mu.RUnlock()
		return cached, nil
	}

	// Marshal under the read lock so command/tags/state stay consistent and we
	// can safely reuse internal metadata without allocating fresh copies.
	data, err := json.Marshal(protocol.TerminalInfo{
		ID:        t.id,
		Name:      t.name,
		Command:   t.command,
		Tags:      t.tags,
		Size:      protocol.Size{Cols: t.size.Cols, Rows: t.size.Rows},
		State:     string(t.state),
		CreatedAt: t.createdAt,
		ExitCode:  t.exitCode,
	})
	t.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	if t.protocolInfoCache == nil {
		t.protocolInfoCache = data
	}
	cached := t.protocolInfoCache
	t.mu.Unlock()
	return cached, nil
}

func (t *Terminal) listInfoSnapshot(filter ListOptions) (*TerminalInfo, bool) {
	t.mu.RLock()
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
	info := &TerminalInfo{
		ID:        t.id,
		Name:      t.name,
		Command:   append([]string(nil), t.command...),
		Tags:      copyTags(t.tags),
		Size:      t.size,
		State:     t.state,
		CreatedAt: t.createdAt,
		ExitCode:  copyIntPtr(t.exitCode),
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
