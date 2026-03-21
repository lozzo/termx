package termx

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/fanout"
	"github.com/lozzow/termx/protocol"
	ptymgr "github.com/lozzow/termx/pty"
	"github.com/lozzow/termx/vterm"
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
	RemoveFunc     func(string, string)
	UpdateFunc     func()
}

type Terminal struct {
	events *EventBus
	pty    *ptymgr.PTY
	vterm  *vterm.VTerm
	stream *fanout.Fanout

	mu            sync.RWMutex
	id            string
	name          string
	command       []string
	tags          map[string]string
	size          Size
	state         TerminalState
	createdAt     time.Time
	exitCode      *int
	keepAfterExit time.Duration
	removeFunc    func(string, string)
	updateFunc    func()
	removed       bool

	// These caches hold deep-copied metadata snapshots so hot read paths do not
	// have to rebuild command/tag payloads for every request.
	protocolInfoCache json.RawMessage
	listInfoCache     *TerminalInfo
	metadataVersion   uint64

	attachMu    sync.Mutex
	attachments map[string]AttachInfo

	done     chan struct{}
	readDone chan struct{}
}

func newTerminal(ctx context.Context, events *EventBus, cfg terminalConfig) (*Terminal, error) {
	p, err := ptymgr.Spawn(ptymgr.SpawnOptions{
		Command:    cfg.Command,
		Dir:        cfg.Dir,
		Env:        cfg.Env,
		TerminalID: cfg.ID,
		Size:       ptymgr.Size{Cols: cfg.Size.Cols, Rows: cfg.Size.Rows},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSpawnFailed, err)
	}

	vt := vterm.New(int(cfg.Size.Cols), int(cfg.Size.Rows), cfg.ScrollbackSize, func(data []byte) {
		// Forward emulator responses (e.g. DSR cursor position) to the PTY
		// so the child process receives them.
		_, _ = p.Write(data)
	})
	t := &Terminal{
		events:        events,
		pty:           p,
		vterm:         vt,
		stream:        fanout.New(),
		id:            cfg.ID,
		name:          cfg.Name,
		command:       append([]string(nil), cfg.Command...),
		tags:          copyTags(cfg.Tags),
		size:          cfg.Size,
		state:         StateRunning,
		createdAt:     time.Now().UTC(),
		keepAfterExit: cfg.KeepAfterExit,
		removeFunc:    cfg.RemoveFunc,
		updateFunc:    cfg.UpdateFunc,
		attachments:   make(map[string]AttachInfo),
		done:          make(chan struct{}),
		readDone:      make(chan struct{}),
	}

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

	go t.readLoop()
	go t.waitLoop()
	return t, nil
}

func (t *Terminal) ID() string {
	return t.id
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
	t.mu.RLock()
	state := t.state
	exitCode := copyIntPtr(t.exitCode)
	t.mu.RUnlock()
	if state == StateExited {
		snap := t.Snapshot(0, 500)
		replay := snapshotReplayPayload(snap)
		ch := make(chan StreamMessage, 2)
		go func() {
			defer close(ch)
			if len(replay) > 0 {
				select {
				case <-ctx.Done():
					return
				case ch <- StreamMessage{Type: StreamOutput, Output: replay}:
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
	dst := make(chan StreamMessage, 256)
	go func() {
		defer close(dst)
		for msg := range src {
			dst <- StreamMessage{
				Type:         StreamMessageType(msg.Type),
				Output:       append([]byte(nil), msg.Output...),
				DroppedBytes: msg.DroppedBytes,
				ExitCode:     copyIntPtr(msg.ExitCode),
			}
		}
	}()
	return dst
}

func (t *Terminal) WriteInput(data []byte) error {
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
	t.size = Size{Cols: cols, Rows: rows}
	t.invalidateProtocolInfoCacheLocked()
	t.mu.Unlock()

	if err := t.pty.Resize(cols, rows); err != nil {
		return err
	}
	t.vterm.Resize(int(cols), int(rows))
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

func (t *Terminal) Snapshot(offset, limit int) *Snapshot {
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
	end := offset + limit
	if end > len(scrollback) {
		end = len(scrollback)
	}

	t.mu.RLock()
	size := t.size
	id := t.id
	t.mu.RUnlock()

	return &Snapshot{
		TerminalID: id,
		Size:       size,
		Screen:     convertScreenData(t.vterm.ScreenContent()),
		Scrollback: convertRows(scrollback[offset:end]),
		Cursor:     convertCursorState(t.vterm.CursorState()),
		Modes:      convertModes(t.vterm.Modes()),
		Timestamp:  time.Now().UTC(),
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

func (t *Terminal) readLoop() {
	defer close(t.readDone)
	buf := make([]byte, 32*1024)
	for {
		n, err := t.pty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			t.stream.Broadcast(chunk)
			_, _ = t.vterm.Write(chunk)
		}
		if err != nil {
			if err != io.EOF {
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

func (t *Terminal) waitLoop() {
	<-t.pty.Wait()
	code := t.pty.ExitCode()

	select {
	case <-t.readDone:
	case <-time.After(500 * time.Millisecond):
	}

	t.mu.Lock()
	oldState := t.state
	t.state = StateExited
	t.exitCode = &code
	t.invalidateProtocolInfoCacheLocked()
	t.mu.Unlock()

	// Terminal exit happens asynchronously, so we explicitly invalidate any
	// cached list payloads that include state or exit-code fields.
	if t.updateFunc != nil {
		t.updateFunc()
	}

	t.stream.Close(&code)
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
	close(t.done)

	if t.keepAfterExit <= 0 {
		t.remove("expired")
		return
	}

	timer := time.NewTimer(t.keepAfterExit)
	defer timer.Stop()
	<-timer.C
	t.remove("expired")
}

func (t *Terminal) remove(reason string) {
	t.mu.Lock()
	if t.removed {
		t.mu.Unlock()
		return
	}
	t.removed = true
	t.mu.Unlock()

	if t.removeFunc != nil {
		t.removeFunc(t.id, reason)
	}
}

func GenerateID() (string, error) {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	const size = 8
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf), nil
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

func snapshotReplayPayload(s *Snapshot) []byte {
	if s == nil {
		return nil
	}
	lines := make([]string, 0, len(s.Scrollback)+len(s.Screen.Cells))
	for _, row := range s.Scrollback {
		if line := snapshotRowString(row); line != "" {
			lines = append(lines, line)
		}
	}
	for _, row := range s.Screen.Cells {
		if line := snapshotRowString(row); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
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
