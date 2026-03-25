package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	stateterminal "github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

const (
	persistedWorkbenchStateVersion = 1
	DefaultWorkspaceSaveDebounce   = 250 * time.Millisecond
)

type WorkspaceStore interface {
	Save(context.Context, app.Model) error
	Load(context.Context) (app.Model, error)
}

type fileWorkspaceStore struct {
	path string
}

type DebouncedWorkspaceSaver struct {
	store WorkspaceStore
	delay time.Duration

	mu       sync.Mutex
	revision int
	pending  app.Model
	timer    *time.Timer
}

type persistedWorkbenchState struct {
	Version   int                                           `json:"version"`
	Screen    app.Screen                                    `json:"screen"`
	Focus     app.FocusTarget                               `json:"focus_target"`
	Pool      app.TerminalPoolState                         `json:"pool"`
	Workspace *workspace.WorkspaceState                     `json:"workspace"`
	Terminals map[types.TerminalID]stateterminal.Metadata   `json:"terminals"`
	Sessions  map[types.TerminalID]persistedTerminalSession `json:"sessions"`
}

type persistedTerminalSession struct {
	TerminalID types.TerminalID   `json:"terminal_id"`
	Channel    uint16             `json:"channel"`
	Attached   bool               `json:"attached"`
	ReadOnly   bool               `json:"read_only"`
	Preview    bool               `json:"preview"`
	Snapshot   *persistedSnapshot `json:"snapshot,omitempty"`
}

type persistedSnapshot struct {
	TerminalID string                 `json:"terminal_id"`
	Size       protocol.Size          `json:"size"`
	Screen     persistedScreenData    `json:"screen"`
	Scrollback [][]protocol.Cell      `json:"scrollback,omitempty"`
	Cursor     protocol.CursorState   `json:"cursor"`
	Modes      protocol.TerminalModes `json:"modes"`
	Timestamp  time.Time              `json:"timestamp"`
}

type persistedScreenData struct {
	Cells             [][]protocol.Cell `json:"cells"`
	IsAlternateScreen bool              `json:"is_alternate_screen"`
}

func NewWorkspaceStore(path string) WorkspaceStore {
	return &fileWorkspaceStore{path: path}
}

func NewDebouncedWorkspaceSaver(store WorkspaceStore, delay time.Duration) *DebouncedWorkspaceSaver {
	if delay <= 0 {
		delay = DefaultWorkspaceSaveDebounce
	}
	return &DebouncedWorkspaceSaver{store: store, delay: delay}
}

func (s *DebouncedWorkspaceSaver) Schedule(model app.Model) tea.Cmd {
	if s == nil || s.store == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.revision++
	s.pending = cloneModelForPersistence(model)
	revision := s.revision

	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.delay, func() {
		_ = s.flushRevision(context.Background(), revision, false)
	})
	return nil
}

func (s *DebouncedWorkspaceSaver) Flush(ctx context.Context, model app.Model) error {
	if s == nil || s.store == nil {
		return nil
	}

	s.mu.Lock()
	s.revision++
	revision := s.revision
	s.pending = cloneModelForPersistence(model)
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()

	return s.flushRevision(ctx, revision, true)
}

func (s *DebouncedWorkspaceSaver) flushRevision(ctx context.Context, revision int, clearTimer bool) error {
	s.mu.Lock()
	if revision != s.revision {
		s.mu.Unlock()
		return nil
	}
	model := cloneModelForPersistence(s.pending)
	if clearTimer {
		s.timer = nil
	}
	err := s.store.Save(ctx, model)
	s.mu.Unlock()
	return err
}

func (s *fileWorkspaceStore) Save(_ context.Context, model app.Model) error {
	if s == nil || s.path == "" {
		return nil
	}

	state := persistedWorkbenchStateFromModel(model)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	file, err := os.CreateTemp(filepath.Dir(s.path), "workspace-state-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(file.Name())
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), s.path)
}

func (s *fileWorkspaceStore) Load(_ context.Context) (app.Model, error) {
	if s == nil || s.path == "" {
		return app.Model{}, os.ErrNotExist
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return app.Model{}, err
	}

	var state persistedWorkbenchState
	if err := json.Unmarshal(data, &state); err != nil {
		return app.Model{}, err
	}
	if state.Version != persistedWorkbenchStateVersion {
		return app.Model{}, fmt.Errorf("unsupported workspace state version %d", state.Version)
	}
	if state.Workspace == nil {
		return app.Model{}, fmt.Errorf("workspace state is missing workspace")
	}
	return state.toModel(), nil
}

// RebindRestoredModel 把磁盘恢复出来的 workbench 状态重新接回 runtime。
// 这里尽量重连现存 terminal；单个 terminal 恢复失败时只降级该 session，不让整次启动炸掉。
func RebindRestoredModel(ctx context.Context, client Client, model app.Model) app.Model {
	service := NewTerminalService(client)
	store := NewSessionStore()
	model.IntentExecutor = modelIntentExecutor{service: service, store: store}
	if model.Terminals == nil {
		model.Terminals = make(map[types.TerminalID]stateterminal.Metadata)
	}
	if model.Sessions == nil {
		model.Sessions = make(map[types.TerminalID]app.TerminalSession)
	}

	for terminalID, meta := range model.Terminals {
		session := model.Sessions[terminalID]
		session.TerminalID = terminalID
		if meta.State == stateterminal.StateExited {
			session.Attached = false
			session.Channel = 0
			model.Sessions[terminalID] = session
			continue
		}

		mode := "rw"
		if session.ReadOnly || session.Preview {
			mode = "observer"
		}
		attach, err := service.Attach(ctx, string(terminalID), mode)
		if err != nil {
			downgradeFailedRestoreTerminal(&model, terminalID)
			continue
		}
		snapshot, err := service.Snapshot(ctx, string(terminalID), 0, 0)
		if err != nil {
			downgradeFailedRestoreTerminal(&model, terminalID)
			continue
		}

		// 恢复后的 Terminal Pool preview 不能退回静态 snapshot。
		// 这里需要像正常 preview 订阅一样重建 stream binding，并把 Next cmd 接回模型。
		if session.Preview && model.Pool.PreviewTerminalID == terminalID {
			stream, cancel := service.Stream(attach.Channel)
			binding := store.BindPreviewAtRevision(
				terminalID,
				attach.Channel,
				snapshot,
				stream,
				cancel,
				model.Pool.PreviewSubscriptionRevision,
			)
			session.Channel = binding.Channel
			session.Attached = true
			session.ReadOnly = true
			session.Preview = true
			session.Snapshot = snapshot
			model.Sessions[terminalID] = session
			model.PreviewStreamNext = store.NextPreviewMessageCmd
			continue
		}

		store.Bind(terminalID, attach.Channel, snapshot)
		session.Channel = attach.Channel
		session.Attached = true
		session.Snapshot = snapshot
		model.Sessions[terminalID] = session
	}

	return model
}

// downgradeFailedRestoreTerminal 把单 terminal 恢复失败后的所有相关视图状态一起降级。
// 这样 workbench/pool 渲染不会继续把失联对象误判成 live pane 或 live preview。
func downgradeFailedRestoreTerminal(model *app.Model, terminalID types.TerminalID) {
	if model == nil {
		return
	}
	if model.Workspace != nil {
		for _, tab := range model.Workspace.Tabs {
			for paneID, pane := range tab.Panes {
				if pane.TerminalID != terminalID {
					continue
				}
				pane.TerminalID = ""
				pane.SlotState = types.PaneSlotUnconnected
				tab.Panes[paneID] = pane
			}
		}
	}
	delete(model.Sessions, terminalID)
	delete(model.Terminals, terminalID)
	if model.Pool.SelectedTerminalID == terminalID {
		model.Pool.SelectedTerminalID = ""
	}
	if model.Pool.PreviewTerminalID == terminalID {
		model.Pool.PreviewTerminalID = ""
		model.PreviewStreamNext = nil
	}
}

func persistedWorkbenchStateFromModel(model app.Model) persistedWorkbenchState {
	sessions := make(map[types.TerminalID]persistedTerminalSession, len(model.Sessions))
	for terminalID, session := range model.Sessions {
		sessions[terminalID] = persistedTerminalSession{
			TerminalID: session.TerminalID,
			Channel:    session.Channel,
			Attached:   session.Attached,
			ReadOnly:   session.ReadOnly,
			Preview:    session.Preview,
			Snapshot:   persistedSnapshotFromRuntime(session.Snapshot),
		}
	}

	return persistedWorkbenchState{
		Version:   persistedWorkbenchStateVersion,
		Screen:    model.Screen,
		Focus:     model.FocusTarget,
		Pool:      model.Pool,
		Workspace: model.Workspace.Clone(),
		Terminals: clonePersistedTerminalMap(model.Terminals),
		Sessions:  sessions,
	}
}

func (state persistedWorkbenchState) toModel() app.Model {
	model := app.NewModel()
	model.Screen = state.Screen
	model.FocusTarget = state.Focus
	model.Pool = state.Pool
	model.Workspace = state.Workspace.Clone()
	model.Terminals = clonePersistedTerminalMap(state.Terminals)
	model.Sessions = make(map[types.TerminalID]app.TerminalSession, len(state.Sessions))
	for terminalID, session := range state.Sessions {
		model.Sessions[terminalID] = app.TerminalSession{
			TerminalID: session.TerminalID,
			Channel:    session.Channel,
			Attached:   session.Attached,
			ReadOnly:   session.ReadOnly,
			Preview:    session.Preview,
			Snapshot:   session.Snapshot.toRuntime(),
		}
	}
	return model
}

func persistedSnapshotFromRuntime(snapshot *protocol.Snapshot) *persistedSnapshot {
	if snapshot == nil {
		return nil
	}
	return &persistedSnapshot{
		TerminalID: snapshot.TerminalID,
		Size:       snapshot.Size,
		Screen: persistedScreenData{
			Cells:             cloneCells(snapshot.Screen.Cells),
			IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
		},
		Scrollback: cloneCells(snapshot.Scrollback),
		Cursor:     snapshot.Cursor,
		Modes:      snapshot.Modes,
		Timestamp:  snapshot.Timestamp,
	}
}

func (snapshot *persistedSnapshot) toRuntime() *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	return &protocol.Snapshot{
		TerminalID: snapshot.TerminalID,
		Size:       snapshot.Size,
		Screen: protocol.ScreenData{
			Cells:             cloneCells(snapshot.Screen.Cells),
			IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
		},
		Scrollback: cloneCells(snapshot.Scrollback),
		Cursor:     snapshot.Cursor,
		Modes:      snapshot.Modes,
		Timestamp:  snapshot.Timestamp,
	}
}

func clonePersistedTerminalMap(input map[types.TerminalID]stateterminal.Metadata) map[types.TerminalID]stateterminal.Metadata {
	if len(input) == 0 {
		return make(map[types.TerminalID]stateterminal.Metadata)
	}
	out := make(map[types.TerminalID]stateterminal.Metadata, len(input))
	for key, meta := range input {
		out[key] = meta.Clone()
	}
	return out
}

func cloneCells(input [][]protocol.Cell) [][]protocol.Cell {
	if len(input) == 0 {
		return nil
	}
	out := make([][]protocol.Cell, len(input))
	for i, row := range input {
		out[i] = append([]protocol.Cell(nil), row...)
	}
	return out
}

func cloneModelForPersistence(model app.Model) app.Model {
	return persistedWorkbenchStateFromModel(model).toModel()
}
