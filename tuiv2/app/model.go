package app

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/persist"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/sessionstate"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
	"github.com/lozzow/termx/workbenchdoc"
)

type Model struct {
	cfg               shared.Config
	statePath         string
	width             int
	height            int
	quitting          bool
	err               error
	errorSeq          uint64
	ownerSeq          uint64
	notice            string
	noticeSeq         uint64
	terminalResyncSeq uint64
	yankBuffer        string
	clipboardHistory  []clipboardHistoryEntry
	clipboardSeq      uint64

	sessionID        string
	sessionViewID    string
	sessionRevision  uint64
	sessionSharedDoc *workbenchdoc.Doc
	sessionLeases    map[string]protocol.LeaseInfo

	startup bootstrap.StartupResult

	prefixSeq int // incremented each time a sticky mode is entered or a valid action fires

	send func(tea.Msg) // injected by run.go after tea.NewProgram

	ui           *UIState
	input        *input.Router // compatibility alias; UIState owns the router
	render       *render.Coordinator
	modalHost    *modal.ModalHost            // compatibility alias; UIState owns modal state
	terminalPage *modal.TerminalManagerState // compatibility alias; UIState owns surface state
	orchestrator *orchestrator.Orchestrator

	// 只读引用，仅用于将 visible state 注入 render 层。
	// 业务编排走 orchestrator，不直接通过这两个字段。
	workbench      *workbench.Workbench
	runtime        *runtime.Runtime
	cursorOut      cursorSequenceWriter
	frameOut       frameSequenceWriter
	lastViewFrame  string
	lastViewCursor string

	terminalInputs              terminalInputDispatchQueue
	terminalInputSending        bool
	interactionBatchActive      bool
	interactionBoundaryEpoch    uint64
	pendingPaneAttaches         map[string]string
	pendingPaneResizes          map[string]pendingPaneResize
	invalidatePending           atomic.Bool
	invalidateDeferred          atomic.Bool
	invalidateScheduled         atomic.Bool
	invalidateBlockedByFrameOut atomic.Bool
	hostEmojiProbePending       bool
	hostThemeFlushPending       bool
	hostThemeBootstrapPending   bool
	hostThemeBootstrapPaletteN  int
	hostThemeBootstrapSeenFG    bool
	hostThemeBootstrapSeenBG    bool
	pendingHostDefaultFG        color.Color
	pendingHostDefaultBG        color.Color
	pendingHostPalette          map[int]color.Color
	hostThemeBootstrapPalette   map[int]struct{}
	pendingMouseMotion          *queuedMouseMsg
	mouseMotionFlushPending     bool
	copyModeMouseActivitySeq    uint64

	// 鼠标拖动状态
	mouseDragPaneID  string
	mouseDragOffsetX int
	mouseDragOffsetY int
	mouseDragMode    mouseDragMode
	mouseDragSplit   *workbench.LayoutNode
	mouseDragBounds  workbench.Rect
	mouseDragDirty   bool

	ownerConfirmPaneID string

	emptyPaneSelectionPaneID string
	emptyPaneSelectionIndex  int

	exitedPaneSelectionPaneID string
	exitedPaneSelectionIndex  int

	copyMode       copyModeState
	copyModeResume copyModeResumeState
}

type mouseDragMode int

var invalidateBatchDelay = 4 * time.Millisecond

const (
	mouseDragNone mouseDragMode = iota
	mouseDragMove
	mouseDragResize
	mouseDragResizeSplit
)

func New(cfg shared.Config, wb *workbench.Workbench, rt *runtime.Runtime) *Model {
	if wb == nil {
		wb = workbench.NewWorkbench()
	}
	if rt == nil {
		rt = runtime.New(nil)
	}
	ui := NewUIState()
	model := &Model{
		cfg:                 cfg,
		statePath:           cfg.WorkspaceStatePath,
		ui:                  ui,
		input:               ui.Router(),
		modalHost:           ui.ModalHost(),
		workbench:           wb,
		runtime:             rt,
		pendingPaneAttaches: make(map[string]string),
		pendingPaneResizes:  make(map[string]pendingPaneResize),
	}
	model.orchestrator = orchestrator.New(model.workbench, model.runtime)
	model.render = render.NewCoordinatorWithVM(func() render.RenderVM { return model.renderVM() })
	// Default invalidate: no-op until SetSendFunc is called by run.go.
	if model.runtime != nil {
		model.runtime.SetInvalidate(func() { model.queueInvalidate() })
		model.runtime.SetTitleChange(func(terminalID, title string) {
			model.sendAsync(terminalTitleMsg{TerminalID: terminalID, Title: title})
		})
	}
	return model
}

func (m *Model) SetCursorWriter(writer cursorSequenceWriter) {
	if m == nil {
		return
	}
	m.cursorOut = writer
}

func (m *Model) SetFrameWriter(writer frameSequenceWriter) {
	if m == nil {
		return
	}
	if current, ok := m.frameOut.(frameBackpressureWriter); ok {
		current.SetDrainHook(nil)
	}
	m.frameOut = writer
	if aware, ok := writer.(frameBackpressureWriter); ok {
		aware.SetDrainHook(func() {
			m.onFrameWriterDrained()
		})
	}
}

// SetSendFunc wires p.Send into the model so that the runtime stream goroutine
// can trigger a bubbletea redraw via InvalidateMsg. Must be called before p.Run().
func (m *Model) SetSendFunc(send func(tea.Msg)) {
	if m == nil {
		return
	}
	m.send = send
	if m.runtime != nil {
		m.runtime.SetInvalidate(func() { m.queueInvalidate() })
	}
}

func (m *Model) queueInvalidate() {
	if m == nil {
		return
	}
	perftrace.Count("app.invalidate.request", 0)
	m.render.Invalidate()
	if m.send == nil {
		return
	}
	if m.invalidatePending.Load() {
		perftrace.Count("app.invalidate.pending_skip", 0)
		m.invalidateDeferred.Store(true)
		return
	}
	if m.invalidateScheduled.Load() {
		perftrace.Count("app.invalidate.scheduled_skip", 0)
		m.invalidateDeferred.Store(true)
		return
	}
	if m.frameWriterHasBacklog() {
		perftrace.Count("app.invalidate.backlog_blocked", 0)
		m.invalidateBlockedByFrameOut.Store(true)
		return
	}
	if m.runtime != nil && m.runtime.RecentLocalInput() {
		perftrace.Count("app.invalidate.interactive_bypass", 0)
		m.queueInvalidateImmediate()
		return
	}
	if invalidateBatchDelay <= 0 {
		m.queueInvalidateImmediate()
		return
	}
	if m.invalidateScheduled.Swap(true) {
		perftrace.Count("app.invalidate.timer_skip", 0)
		m.invalidateDeferred.Store(true)
		return
	}
	perftrace.Count("app.invalidate.timer_scheduled", 0)
	time.AfterFunc(invalidateBatchDelay, func() {
		if m == nil {
			return
		}
		m.invalidateScheduled.Store(false)
		m.queueInvalidateImmediate()
	})
}

func (m *Model) queueInvalidateImmediate() {
	if m == nil || m.send == nil {
		return
	}
	if !m.invalidatePending.Swap(true) {
		perftrace.Count("app.invalidate.sent", 0)
		m.sendAsync(InvalidateMsg{})
		return
	}
	perftrace.Count("app.invalidate.deferred", 0)
	m.invalidateDeferred.Store(true)
}

func (m *Model) frameWriterHasBacklog() bool {
	if m == nil {
		return false
	}
	writer, ok := m.frameOut.(frameBackpressureWriter)
	return ok && writer.HasPendingFrame()
}

func (m *Model) onFrameWriterDrained() {
	if m == nil || m.send == nil {
		return
	}
	if !m.invalidateBlockedByFrameOut.Swap(false) {
		return
	}
	perftrace.Count("app.invalidate.frame_writer_drained", 0)
	m.queueInvalidateImmediate()
}

func (m *Model) sendAsync(msg tea.Msg) {
	if m == nil || m.send == nil {
		return
	}
	fields := debugMessageFields(msg)
	m.debugLog("send_async_start", fields...)
	// Runtime callbacks can fire from PTY/stream goroutines. Program.Send uses
	// an unbuffered channel, so sending asynchronously avoids stalling terminal
	// output when Bubble Tea is busy rendering or handling another message.
	go func() {
		m.send(msg)
		m.debugLog("send_async_done", fields...)
	}()
}

func (m *Model) bootstrapStartup() error {
	if m == nil || m.workbench == nil {
		return nil
	}
	if m.cfg.SessionID != "" {
		return m.bootstrapSessionStartup(context.Background())
	}
	if m.workbench.CurrentWorkspace() != nil {
		return nil
	}
	var data []byte
	if m.cfg.WorkspaceStatePath != "" {
		data, _ = os.ReadFile(m.cfg.WorkspaceStatePath)
	}
	result, err := bootstrap.RestoreOrStartup(data, bootstrap.Config{}, m.workbench, m.runtime)
	if err != nil {
		return err
	}
	m.startup = result
	if result.ShouldOpenPicker {
		m.openModal(input.ModePicker, "startup-picker")
		m.resetPickerState()
	}
	return nil
}

func (m *Model) bootstrapSessionStartup(ctx context.Context) error {
	if m == nil || m.runtime == nil || m.workbench == nil || m.cfg.SessionID == "" {
		return nil
	}
	client := m.runtime.Client()
	if client == nil {
		return nil
	}
	sessionID := m.cfg.SessionID
	if _, err := client.GetSession(ctx, sessionID); err != nil {
		if _, createErr := client.CreateSession(ctx, protocol.CreateSessionParams{
			SessionID: sessionID,
			Name:      sessionID,
		}); createErr != nil {
			return createErr
		}
	}
	snapshot, err := client.AttachSession(ctx, protocol.AttachSessionParams{
		SessionID:  sessionID,
		WindowCols: uint16(maxInt(0, m.width)),
		WindowRows: uint16(maxInt(0, m.height)),
	})
	if err != nil {
		return err
	}
	m.applySessionSnapshot(snapshot)
	if pane := m.workbench.ActivePane(); pane != nil && pane.TerminalID == "" {
		m.openModal(input.ModePicker, "startup-picker")
		m.resetPickerState()
	}
	return nil
}

func (m *Model) saveStateCmd() tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	if m.sessionID != "" {
		return batchCmds(m.replaceSessionCmd(), m.updateSessionViewCmd())
	}
	if m.statePath == "" {
		return nil
	}
	wb := m.workbench
	rt := m.runtime
	path := m.statePath
	return func() tea.Msg {
		if err := saveState(path, wb, rt); err != nil {
			return nil
		}
		return nil
	}
}

func (m *Model) applySessionSnapshot(snapshot *protocol.SessionSnapshot) {
	if m == nil || snapshot == nil {
		return
	}
	projection := m.captureLocalViewProjection()
	currentViewID := m.sessionViewID
	if snapshot.Session.ID != "" {
		m.sessionID = snapshot.Session.ID
	}
	if snapshot.View != nil {
		m.sessionViewID = snapshot.View.ViewID
		if shouldAdoptSnapshotViewProjection(currentViewID, snapshot.View.ViewID, projection) {
			projection.WorkspaceName = snapshot.View.ActiveWorkspaceName
			projection.ActiveTabID = snapshot.View.ActiveTabID
			projection.FocusedPaneID = snapshot.View.FocusedPaneID
		}
	}
	m.sessionRevision = snapshot.Session.Revision
	if snapshot.Workbench != nil {
		m.sessionSharedDoc = snapshot.Workbench.Clone()
	}
	if len(snapshot.Leases) > 0 {
		m.sessionLeases = make(map[string]protocol.LeaseInfo, len(snapshot.Leases))
		for _, lease := range snapshot.Leases {
			if lease.TerminalID != "" {
				m.sessionLeases[lease.TerminalID] = lease
			}
		}
	} else {
		m.sessionLeases = nil
	}

	oldBindings := sessionstate.PaneTerminalBindings(sessionstate.ExportWorkbench(m.workbench))
	m.workbench = sessionstate.ImportDoc(snapshot.Workbench)
	m.applyLocalViewProjection(projection)
	m.orchestrator = orchestrator.New(m.workbench, m.runtime)

	if m.runtime != nil {
		nextBindings := sessionstate.PaneTerminalBindings(snapshot.Workbench)
		m.reconcileSessionRuntime(context.Background(), oldBindings, nextBindings)
		if service := m.sessionRuntimeService(); service != nil {
			service.applyCurrentLeases()
		}
	}
	m.render.Invalidate()
}

func shouldAdoptSnapshotViewProjection(currentViewID, snapshotViewID string, projection localViewProjection) bool {
	if snapshotViewID == "" {
		return false
	}
	if currentViewID == "" || currentViewID != snapshotViewID {
		return true
	}
	return projection.WorkspaceName == "" && projection.ActiveTabID == "" && projection.FocusedPaneID == ""
}

func saveState(path string, wb *workbench.Workbench, rt *runtime.Runtime) error {
	if path == "" || wb == nil {
		return nil
	}
	data, err := persist.Save(wb)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
