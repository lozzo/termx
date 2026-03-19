package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
)

type Config struct {
	DefaultShell       string
	Workspace          string
	AttachID           string
	StartupLayout      string
	WorkspaceStatePath string
	StartupAutoLayout  bool
	StartupPicker      bool
	Logger             *slog.Logger
	RequestTimeout     time.Duration
}

type Workspace struct {
	Name      string
	Tabs      []*Tab
	ActiveTab int
}

type Tab struct {
	Name            string
	Root            *LayoutNode
	Panes           map[string]*Pane
	Floating        []*FloatingPane
	FloatingVisible bool
	ActivePaneID    string
	ZoomedPaneID    string
	LayoutPreset    int

	renderCache *tabRenderCache
}

type FloatingPane struct {
	PaneID string
	Rect   Rect
	Z      int
}

type tabRenderCache struct {
	canvas       *composedCanvas
	rects        map[string]Rect
	order        []string
	frameKeys    map[string]string
	width        int
	height       int
	activePaneID string
	zoomedPaneID string
}

type ViewportMode string

const (
	ViewportModeFit   ViewportMode = "fit"
	ViewportModeFixed ViewportMode = "fixed"
)

type Point struct {
	X int
	Y int
}

type Viewport struct {
	TerminalID    string
	Channel       uint16
	VTerm         *localvterm.VTerm
	Snapshot      *protocol.Snapshot
	Name          string
	Command       []string
	Tags          map[string]string
	TerminalState string
	ExitCode      *int
	Mode          ViewportMode
	Offset        Point
	Pin           bool
	Readonly      bool
	stopStream    func()

	cellCache       [][]drawCell
	cellVersion     uint64
	viewportCache   [][]drawCell
	viewportOffset  Point
	viewportWidth   int
	viewportHeight  int
	viewportVersion uint64
	renderDirty     bool
	live            bool
	syncLost        bool
	droppedBytes    uint64
	recovering      bool
	catchingUp      bool
	dirtyTicks      int
	cleanTicks      int
	skipTick        bool
	dirtyRowsKnown  bool
	dirtyRowStart   int
	dirtyRowEnd     int
	dirtyColsKnown  bool
	dirtyColStart   int
	dirtyColEnd     int
}

type Pane struct {
	ID    string
	Title string
	*Viewport
}

type textPrompt struct {
	Title    string
	Value    string
	Original string
}

type terminalPicker struct {
	Title       string
	Footer      string
	Query       string
	Items       []terminalPickerItem
	Filtered    []terminalPickerItem
	Selected    int
	Action      terminalPickerAction
	OpenedAt    time.Time
	RenderWidth int
}

type workspacePicker struct {
	Title       string
	Footer      string
	Query       string
	Items       []workspacePickerItem
	Filtered    []workspacePickerItem
	Selected    int
	RenderWidth int
}

type terminalPickerItem struct {
	Info            protocol.TerminalInfo
	Observed        bool
	Orphan          bool
	Location        string
	CreateNew       bool
	Label           string
	Description     string
	searchTextLower string
	lineBody        string
	lineWidth       int
	lineNormal      string
	lineActive      string
}

type workspacePickerItem struct {
	Name            string
	Description     string
	CreateNew       bool
	searchTextLower string
	lineBody        string
	lineWidth       int
	lineNormal      string
	lineActive      string
}

type terminalPickerActionKind int

const (
	terminalPickerActionReplace terminalPickerActionKind = iota
	terminalPickerActionBootstrap
	terminalPickerActionNewTab
	terminalPickerActionSplit
	terminalPickerActionFloating
	terminalPickerActionLayoutResolve
)

type terminalPickerAction struct {
	Kind     terminalPickerActionKind
	TabIndex int
	TargetID string
	Split    SplitDirection
	PaneID   string
}

type Model struct {
	client Client
	cfg    Config
	logger *slog.Logger

	program *tea.Program

	renderInterval      time.Duration
	renderCache         string
	renderDirty         bool
	renderBatching      bool
	renderTickerStop    chan struct{}
	renderTickerRunning bool
	renderPending       atomic.Bool

	workspace       Workspace
	width           int
	height          int
	prefixActive    bool
	prefixSeq       int
	prefixTimeout   time.Duration
	rawPending      []byte
	showHelp        bool
	prompt          *textPrompt
	terminalPicker  *terminalPicker
	workspacePicker *workspacePicker
	nextPane        int
	nextTab         int
	quitting        bool
	notice          string
	err             error

	workspaceStore  map[string]Workspace
	workspaceOrder  []string
	activeWorkspace int
	layoutPromptQueue   []LayoutCreatePlan
	layoutPromptCurrent *LayoutCreatePlan
}

type paneCreatedMsg struct {
	tabIndex int
	targetID string
	split    SplitDirection
	floating bool
	pane     *Pane
}

type paneReplacedMsg struct {
	paneID string
	pane   *Pane
}

type paneOutputMsg struct {
	paneID string
	frame  protocol.StreamFrame
}

type paneResizeMsg struct {
	channel uint16
	cols    uint16
	rows    uint16
}

type paneDetachedMsg struct {
	paneID string
}

type tabClosedMsg struct {
	tabIndex int
}

type errMsg struct{ err error }

type prefixTimeoutMsg struct {
	seq int
}

type renderTickMsg struct{}

type rawInputMsg struct {
	data []byte
}

type terminalPickerLoadedMsg struct {
	picker *terminalPicker
	err    error
}

type terminalPickerSelectionMsg struct {
	Action terminalPickerAction
	Item   terminalPickerItem
}

type workspacePickerLoadedMsg struct {
	picker *workspacePicker
}

type workspaceActivatedMsg struct {
	workspace Workspace
	index     int
	notice    string
}

type workspaceStateLoadedMsg struct {
	workspace Workspace
	store     map[string]Workspace
	order     []string
	active    int
	notice    string
}

type terminalClosedMsg struct {
	terminalID string
}

type paneRecoveredMsg struct {
	paneID       string
	snapshot     *protocol.Snapshot
	droppedBytes uint64
}

type paneRecoveryFailedMsg struct {
	paneID string
	err    error
}

type layoutLoadedMsg struct {
	workspace Workspace
	notice    string
	prompt    []LayoutCreatePlan
}

type noticeMsg struct {
	text string
}

const (
	layoutPresetCustom = -1
)

const (
	layoutPresetEvenHorizontal = iota
	layoutPresetEvenVertical
	layoutPresetMainHorizontal
	layoutPresetMainVertical
	layoutPresetTiled
	layoutPresetCount
)

func NewModel(client Client, cfg Config) *Model {
	if cfg.DefaultShell == "" {
		cfg.DefaultShell = os.Getenv("SHELL")
		if cfg.DefaultShell == "" {
			cfg.DefaultShell = "/bin/sh"
		}
	}
	if cfg.Workspace == "" {
		cfg.Workspace = "main"
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 3 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Model{
		client: client,
		cfg:    cfg,
		logger: logger,
		workspace: Workspace{
			Name: cfg.Workspace,
			Tabs: []*Tab{newTab("1")},
		},
		renderInterval: 16 * time.Millisecond,
		renderDirty:    true,
		width:          80,
		height:         24,
		prefixTimeout:  800 * time.Millisecond,
		workspaceStore: map[string]Workspace{
			cfg.Workspace: {
				Name: cfg.Workspace,
				Tabs: []*Tab{newTab("1")},
			},
		},
		workspaceOrder:  []string{cfg.Workspace},
		activeWorkspace: 0,
	}
}

func (m *Model) requestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), m.cfg.RequestTimeout)
}

func (m *Model) wrapClientError(op string, err error, attrs ...any) error {
	if err == nil {
		return nil
	}
	logAttrs := append([]any{"operation", op, "error", err}, attrs...)
	if errors.Is(err, context.DeadlineExceeded) {
		m.logger.Error("tui client operation timed out", logAttrs...)
		return fmt.Errorf("%s timed out after %s", op, m.cfg.RequestTimeout)
	}
	if errors.Is(err, context.Canceled) {
		m.logger.Warn("tui client operation canceled", logAttrs...)
		return fmt.Errorf("%s canceled", op)
	}
	m.logger.Error("tui client operation failed", logAttrs...)
	return err
}

func (m *Model) SetProgram(program *tea.Program) {
	m.program = program
	m.renderBatching = true
	m.startRenderTicker()
}

func (m *Model) StopRenderTicker() {
	if m.renderTickerStop != nil {
		close(m.renderTickerStop)
		m.renderTickerStop = nil
	}
	m.renderTickerRunning = false
}

func (m *Model) startRenderTicker() {
	if m.program == nil || m.renderTickerRunning || m.renderInterval <= 0 {
		return
	}
	stop := make(chan struct{})
	m.renderTickerStop = stop
	m.renderTickerRunning = true
	interval := m.renderInterval
	program := m.program
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if !m.renderPending.Load() {
					continue
				}
				program.Send(renderTickMsg{})
			}
		}
	}()
}

func (m *Model) invalidateRender() {
	m.renderDirty = true
}

func (m *Model) scheduleRender() {
	if !m.renderBatching {
		m.invalidateRender()
		return
	}
	m.renderPending.Store(true)
}

func (m *Model) flushPendingRender() {
	if !m.renderBatching {
		m.invalidateRender()
		return
	}
	if !m.updateBackpressureState() {
		return
	}
	if !m.renderPending.Load() && !m.anyPaneDirty() {
		return
	}
	m.renderPending.Store(false)
	m.invalidateRender()
}

func (m *Model) anyPaneDirty() bool {
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane != nil && pane.renderDirty {
				return true
			}
		}
	}
	return false
}

func (m *Model) updateBackpressureState() bool {
	shouldRender := false
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane == nil {
				continue
			}
			if pane.renderDirty {
				pane.dirtyTicks++
				pane.cleanTicks = 0
				if pane.dirtyTicks >= 30 {
					pane.catchingUp = true
				}
				if pane.catchingUp {
					pane.skipTick = !pane.skipTick
					if pane.skipTick {
						continue
					}
				}
				shouldRender = true
				continue
			}

			pane.dirtyTicks = 0
			if pane.catchingUp {
				pane.cleanTicks++
				if pane.cleanTicks >= 5 {
					pane.catchingUp = false
					pane.cleanTicks = 0
					pane.skipTick = false
				}
			}
		}
	}
	return shouldRender || !m.anyPaneDirty()
}

func (m *Model) Init() tea.Cmd {
	if m.cfg.AttachID != "" {
		m.logger.Info("tui init attach bootstrap", "terminal_id", m.cfg.AttachID)
		return m.attachInitialTerminalCmd(0, m.cfg.AttachID)
	}
	if strings.TrimSpace(m.cfg.StartupLayout) != "" {
		m.logger.Info("tui init startup layout", "layout", m.cfg.StartupLayout)
		return m.loadLayoutCmd(m.cfg.StartupLayout, LayoutResolveCreate)
	}
	if strings.TrimSpace(m.cfg.WorkspaceStatePath) != "" || m.cfg.StartupAutoLayout {
		return m.startStartupBootstrapCmd()
	}
	if m.cfg.StartupPicker {
		m.logger.Info("tui init startup chooser")
		return m.openBootstrapTerminalPickerCmd(0)
	}
	m.logger.Info("tui init create first pane")
	return m.createPaneCmd(0, "", "")
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampFloatingPanes()
		m.invalidateRender()
		return m, m.resizeVisiblePanesCmd()
	case tea.KeyMsg:
		return m.handleKey(msg)
	case paneCreatedMsg:
		m.attachPane(msg)
		return m, tea.Batch(m.resizeVisiblePanesCmd(), m.advanceLayoutPromptAfterPaneMsg("", msg.pane))
	case paneReplacedMsg:
		m.replacePane(msg)
		return m, tea.Batch(m.resizeVisiblePanesCmd(), m.advanceLayoutPromptAfterPaneMsg(msg.paneID, msg.pane))
	case paneOutputMsg:
		cmd := m.handlePaneOutput(msg)
		if m.quitting {
			return m, tea.Quit
		}
		return m, cmd
	case paneResizeMsg:
		m.handlePaneResize(msg)
		return m, nil
	case paneDetachedMsg:
		if m.removePane(msg.paneID) {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	case tabClosedMsg:
		if m.removeTab(msg.tabIndex) {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	case rawInputMsg:
		return m, m.handleRawInput(msg.data)
	case inputEventMsg:
		return m, m.handleInputEvent(msg.event)
	case terminalPickerLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.logger.Error("terminal picker load failed", "error", msg.err)
			m.invalidateRender()
			return m, nil
		}
		if msg.picker != nil {
			m.logger.Info("terminal picker opened", "title", msg.picker.Title, "items", len(msg.picker.Items))
		}
		m.terminalPicker = msg.picker
		m.invalidateRender()
		return m, nil
	case workspacePickerLoadedMsg:
		m.workspacePicker = msg.picker
		m.invalidateRender()
		return m, nil
	case terminalPickerSelectionMsg:
		return m, m.resolveTerminalPickerSelection(msg.Action, msg.Item, false)
	case workspaceActivatedMsg:
		m.notice = msg.notice
		m.err = nil
		m.activeWorkspace = msg.index
		m.replaceWorkspace(msg.workspace)
		m.invalidateRender()
		return m, m.resizeVisiblePanesCmd()
	case workspaceStateLoadedMsg:
		m.notice = msg.notice
		m.err = nil
		m.workspaceStore = msg.store
		m.workspaceOrder = msg.order
		m.activeWorkspace = msg.active
		m.replaceWorkspace(msg.workspace)
		m.invalidateRender()
		return m, m.resizeVisiblePanesCmd()
	case terminalClosedMsg:
		m.markTerminalKilled(msg.terminalID)
		return m, nil
	case layoutLoadedMsg:
		m.notice = msg.notice
		m.err = nil
		m.replaceWorkspace(msg.workspace)
		m.layoutPromptQueue = append([]LayoutCreatePlan(nil), msg.prompt...)
		m.layoutPromptCurrent = nil
		m.invalidateRender()
		return m, tea.Batch(m.resizeVisiblePanesCmd(), m.advanceLayoutPromptCmd())
	case paneRecoveredMsg:
		m.handlePaneRecovered(msg)
		return m, nil
	case paneRecoveryFailedMsg:
		m.handlePaneRecoveryFailed(msg)
		return m, nil
	case renderTickMsg:
		m.flushPendingRender()
		return m, nil
	case prefixTimeoutMsg:
		if m.prefixActive && msg.seq == m.prefixSeq {
			m.prefixActive = false
			m.invalidateRender()
		}
		return m, nil
	case errMsg:
		m.notice = ""
		m.err = msg.err
		m.invalidateRender()
		return m, nil
	case noticeMsg:
		m.notice = msg.text
		m.err = nil
		m.invalidateRender()
		return m, nil
	}
	return m, nil
}

func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "loading..."
	}

	if !m.renderDirty && m.renderCache != "" && (m.workspacePicker != nil || m.terminalPicker != nil || m.showHelp || m.prompt != nil) {
		return m.renderCache
	}

	if m.renderBatching && !m.renderDirty && m.renderCache != "" {
		if m.program != nil || !m.anyPaneDirty() {
			return m.renderCache
		}
	}

	var out string
	if m.workspacePicker != nil {
		out = m.renderWorkspacePicker()
		m.renderCache = out
		m.renderDirty = false
		return out
	}

	if m.terminalPicker != nil {
		out = m.renderTerminalPicker()
		m.renderCache = out
		m.renderDirty = false
		return out
	}

	if m.showHelp {
		out = m.renderHelpScreen()
		m.renderCache = out
		m.renderDirty = false
		return out
	}

	tabBar := m.renderTabBar()
	status := m.renderStatus()
	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	tab := m.currentTab()
	var body string
	if tab == nil || tab.Root == nil {
		canvas := newCanvas(m.width, contentHeight)
		canvas.drawText(Rect{X: 2, Y: 2, W: max(1, m.width-4), H: max(1, contentHeight-4)}, m.emptyStateLines())
		body = canvas.String()
	} else {
		body = m.renderTabComposite(tab, m.width, contentHeight)
	}

	out = strings.Join([]string{tabBar, body, status}, "\n")
	m.renderCache = out
	m.renderDirty = false
	return out
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.workspacePicker != nil {
		return m, m.handleWorkspacePickerKey(msg)
	}

	if m.terminalPicker != nil {
		return m, m.handleTerminalPickerKey(msg)
	}

	if m.prompt != nil {
		return m, m.handlePromptKey(msg)
	}

	if m.showHelp {
		switch msg.Type {
		case tea.KeyEsc:
			m.showHelp = false
			m.invalidateRender()
			return m, nil
		case tea.KeyRunes:
			if len(msg.Runes) == 1 && (msg.Runes[0] == 'q' || msg.Runes[0] == '?') {
				m.showHelp = false
				m.invalidateRender()
			}
			return m, nil
		default:
			return m, nil
		}
	}

	if m.prefixActive {
		m.prefixActive = false
		m.invalidateRender()
		return m, m.handlePrefixKey(msg)
	}

	if msg.Type == tea.KeyCtrlA {
		cmd := m.activatePrefix()
		m.invalidateRender()
		return m, cmd
	}

	if msg.Type == tea.KeyEsc {
		if m.focusTiledPane() {
			return m, nil
		}
	}

	if cmd := m.handleExitedPaneKey(msg); cmd != nil {
		return m, cmd
	}

	if tab := m.currentTab(); tab != nil {
		pane := tab.Panes[tab.ActivePaneID]
		if pane != nil {
			data := encodeKey(msg)
			if len(data) > 0 {
				return m, m.sendToActive(data)
			}
		}
	}

	return m, nil
}

func (m *Model) handleExitedPaneKey(msg tea.KeyMsg) tea.Cmd {
	pane := activePane(m.currentTab())
	if paneTerminalState(pane) != "exited" {
		return nil
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'r' {
		return m.restartActivePaneCmd()
	}
	return nil
}

func (m *Model) handlePrefixKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyLeft:
		m.moveFocus(DirectionLeft)
		m.invalidateRender()
		return nil
	case tea.KeyCtrlLeft:
		m.panActiveViewport(-4, 0)
		return nil
	case tea.KeyDown:
		m.moveFocus(DirectionDown)
		m.invalidateRender()
		return nil
	case tea.KeyCtrlDown:
		m.panActiveViewport(0, 2)
		return nil
	case tea.KeyUp:
		m.moveFocus(DirectionUp)
		m.invalidateRender()
		return nil
	case tea.KeyCtrlUp:
		m.panActiveViewport(0, -2)
		return nil
	case tea.KeyRight:
		m.moveFocus(DirectionRight)
		m.invalidateRender()
		return nil
	case tea.KeyCtrlRight:
		m.panActiveViewport(4, 0)
		return nil
	case tea.KeyCtrlH:
		m.panActiveViewport(-4, 0)
		return nil
	case tea.KeyCtrlJ:
		m.panActiveViewport(0, 2)
		return nil
	case tea.KeyCtrlK:
		m.panActiveViewport(0, -2)
		return nil
	case tea.KeyCtrlL:
		m.panActiveViewport(4, 0)
		return nil
	}

	key := msg.String()
	switch key {
	case "ctrl+a":
		return m.sendToActive([]byte{0x01})
	case "\"":
		return m.splitActivePane(SplitHorizontal)
	case "%":
		return m.splitActivePane(SplitVertical)
	case "h":
		m.moveFocus(DirectionLeft)
	case "j":
		m.moveFocus(DirectionDown)
	case "k":
		m.moveFocus(DirectionUp)
	case "l":
		m.moveFocus(DirectionRight)
	case "c":
		return m.openNewTabTerminalPickerCmd()
	case "n":
		if len(m.workspace.Tabs) > 0 {
			m.workspace.ActiveTab = (m.workspace.ActiveTab + 1) % len(m.workspace.Tabs)
		}
		return m.resizeVisiblePanesCmd()
	case "p":
		if len(m.workspace.Tabs) > 0 {
			m.workspace.ActiveTab = (m.workspace.ActiveTab - 1 + len(m.workspace.Tabs)) % len(m.workspace.Tabs)
		}
		return m.resizeVisiblePanesCmd()
	case "z":
		tab := m.currentTab()
		if tab != nil {
			if tab.ZoomedPaneID == tab.ActivePaneID {
				tab.ZoomedPaneID = ""
			} else {
				tab.ZoomedPaneID = tab.ActivePaneID
			}
		}
		return m.resizeVisiblePanesCmd()
	case "{":
		m.swapActivePane(-1)
		return m.resizeVisiblePanesCmd()
	case "}":
		m.swapActivePane(1)
		return m.resizeVisiblePanesCmd()
	case "H":
		m.resizeActivePane(DirectionLeft, 2)
		return m.resizeVisiblePanesCmd()
	case "J":
		m.resizeActivePane(DirectionDown, 2)
		return m.resizeVisiblePanesCmd()
	case "K":
		m.resizeActivePane(DirectionUp, 2)
		return m.resizeVisiblePanesCmd()
	case "L":
		m.resizeActivePane(DirectionRight, 2)
		return m.resizeVisiblePanesCmd()
	case " ":
		m.cycleActiveLayout()
		return m.resizeVisiblePanesCmd()
	case ",":
		m.beginRenameTab()
		return nil
	case "f":
		return m.openTerminalPickerCmd()
	case "s":
		return m.openWorkspacePickerCmd()
	case "w":
		return m.openFloatingTerminalPickerCmd(m.workspace.ActiveTab)
	case "W":
		m.toggleFloatingLayerVisibility()
		return nil
	case "tab":
		m.cycleFloatingFocus()
		return nil
	case "]":
		m.raiseActiveFloatingPane()
		return nil
	case "_":
		m.lowerActiveFloatingPane()
		return nil
	case ":":
		m.beginCommandPrompt()
		return nil
	case "x":
		return m.closeActivePaneCmd()
	case "X":
		return m.killActiveTerminalCmd()
	case "M":
		m.toggleActiveViewportMode()
		return m.resizeVisiblePanesCmd()
	case "P":
		m.toggleActiveViewportPin()
		return nil
	case "R":
		m.toggleActiveViewportReadonly()
		return nil
	case "&":
		return m.killActiveTabCmd()
	case "d":
		m.quitting = true
		m.invalidateRender()
		return tea.Quit
	case "?":
		m.showHelp = true
		m.invalidateRender()
	default:
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			idx := int(key[0] - '1')
			if idx < len(m.workspace.Tabs) {
				m.workspace.ActiveTab = idx
			}
			m.invalidateRender()
			return m.resizeVisiblePanesCmd()
		}
	}
	return nil
}

func (m *Model) handleRawInput(data []byte) tea.Cmd {
	if len(data) == 0 {
		return nil
	}

	m.rawPending = append(m.rawPending, data...)
	var ordered []tea.Cmd
	var background []tea.Cmd

	for len(m.rawPending) > 0 {
		if m.workspacePicker != nil {
			consumed, cmd, ok := m.consumeWorkspacePickerInput()
			if !ok {
				break
			}
			m.rawPending = m.rawPending[consumed:]
			if cmd != nil {
				ordered = append(ordered, cmd)
			}
			continue
		}

		if m.terminalPicker != nil {
			consumed, cmd, ok := m.consumeTerminalPickerInput()
			if !ok {
				break
			}
			m.rawPending = m.rawPending[consumed:]
			if cmd != nil {
				ordered = append(ordered, cmd)
			}
			continue
		}

		if m.prompt != nil {
			consumed, cmd, ok := m.consumePromptInput()
			if !ok {
				break
			}
			m.rawPending = m.rawPending[consumed:]
			if cmd != nil {
				ordered = append(ordered, cmd)
			}
			continue
		}

		if m.showHelp {
			consumed, ok := m.consumeHelpInput()
			if !ok {
				break
			}
			m.rawPending = m.rawPending[consumed:]
			continue
		}

		if m.prefixActive {
			consumed, cmd, ok := m.consumePrefixInput()
			if !ok {
				break
			}
			m.rawPending = m.rawPending[consumed:]
			if cmd != nil {
				ordered = append(ordered, cmd)
			}
			continue
		}

		if active := activePane(m.currentTab()); paneTerminalState(active) == "exited" {
			consumed, cmd, ok := m.consumeExitedPaneInput()
			if !ok {
				break
			}
			m.rawPending = m.rawPending[consumed:]
			if cmd != nil {
				ordered = append(ordered, cmd)
			}
			continue
		}

		idx := bytes.IndexByte(m.rawPending, 0x01)
		if idx < 0 {
			payload, keep := rewriteInputForActivePane(m.currentTab(), m.rawPending)
			if cmd := m.sendToActive(payload); cmd != nil {
				ordered = append(ordered, cmd)
			}
			m.rawPending = keep
			break
		}

		if idx > 0 {
			payload, keep := rewriteInputForActivePane(m.currentTab(), m.rawPending[:idx])
			if len(keep) > 0 {
				payload = append(payload, keep...)
				keep = nil
			}
			if cmd := m.sendToActive(payload); cmd != nil {
				ordered = append(ordered, cmd)
			}
			m.rawPending = m.rawPending[idx:]
			continue
		}

		m.rawPending = m.rawPending[1:]
		if cmd := m.activatePrefix(); cmd != nil {
			background = append(background, cmd)
		}
	}

	return combineCmdsOrdered(ordered, background)
}

func (m *Model) consumeExitedPaneInput() (int, tea.Cmd, bool) {
	if len(m.rawPending) == 0 {
		return 0, nil, false
	}
	if m.rawPending[0] == 0x01 {
		return 1, m.activatePrefix(), true
	}

	r, size := utf8.DecodeRune(m.rawPending)
	if r == utf8.RuneError && size == 1 {
		if !utf8.FullRune(m.rawPending) {
			return 0, nil, false
		}
	}
	switch r {
	case 'r':
		return size, m.restartActivePaneCmd(), true
	default:
		return size, nil, true
	}
}

func (m *Model) consumeHelpInput() (int, bool) {
	if len(m.rawPending) == 0 {
		return 0, false
	}

	switch m.rawPending[0] {
	case 'q', '?':
		m.showHelp = false
		return 1, true
	case 0x1b:
		if n, ok := consumeArrowSequence(m.rawPending); ok {
			return n, true
		}
		m.showHelp = false
		return 1, true
	default:
		return 1, true
	}
}

func (m *Model) handlePromptKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.prompt = nil
	case tea.KeyEnter:
		return m.commitPrompt()
	case tea.KeyBackspace:
		m.deletePromptRune()
	case tea.KeySpace:
		m.appendPrompt(" ")
	case tea.KeyRunes:
		if len(msg.Runes) > 0 {
			m.appendPrompt(string(msg.Runes))
		}
	}
	return nil
}

func (m *Model) consumePromptInput() (int, tea.Cmd, bool) {
	if len(m.rawPending) == 0 {
		return 0, nil, false
	}
	switch m.rawPending[0] {
	case '\r', '\n':
		return 1, m.commitPrompt(), true
	case 0x7f, 0x08:
		m.deletePromptRune()
		return 1, nil, true
	case 0x1b:
		if n, ok := consumeArrowSequence(m.rawPending); ok {
			return n, nil, true
		}
		if len(m.rawPending) == 1 {
			m.prompt = nil
			return 1, nil, true
		}
		return 0, nil, false
	}

	r, size := utf8.DecodeRune(m.rawPending)
	if r == utf8.RuneError && size == 1 {
		if !utf8.FullRune(m.rawPending) {
			return 0, nil, false
		}
	}
	if r < 0x20 {
		return size, nil, true
	}
	m.appendPrompt(string(r))
	return size, nil, true
}

func (m *Model) consumePrefixInput() (int, tea.Cmd, bool) {
	if len(m.rawPending) == 0 {
		return 0, nil, false
	}

	if n, key, ok, incomplete := parseCtrlArrowPrefix(m.rawPending); incomplete {
		return 0, nil, false
	} else if ok {
		m.prefixActive = false
		m.prefixSeq++
		return n, m.handlePrefixKey(key), true
	}

	if n, dir, ok, incomplete := parseArrowPrefix(m.rawPending); incomplete {
		return 0, nil, false
	} else if ok {
		m.prefixActive = false
		m.prefixSeq++
		return n, m.handlePrefixKey(prefixDirectionKey(dir)), true
	}

	b := m.rawPending[0]
	m.prefixActive = false
	m.prefixSeq++

	switch b {
	case 0x01:
		return 1, m.sendToActive([]byte{0x01}), true
	case 0x08, 0x0a, 0x0b, 0x0c:
		return 1, m.handlePrefixKey(prefixCtrlKey(b)), true
	case '\t':
		return 1, m.handlePrefixKey(prefixTabKey()), true
	case '"', '%', ',', ':', 'W', ']', '_', 'f', 'h', 'j', 'k', 'l', 'w', 'H', 'J', 'K', 'L', 'M', 'P', 'R', 'X', 'c', 'n', 'p', 'z', '{', '}', ' ', 'x', '&', 'd', '?':
		return 1, m.handlePrefixKey(prefixRuneKey(rune(b))), true
	default:
		if b >= '1' && b <= '9' {
			return 1, m.handlePrefixKey(prefixRuneKey(rune(b))), true
		}
		if b == 0x1b {
			return 0, nil, false
		}
		return 1, nil, true
	}
}

func (m *Model) splitActivePane(dir SplitDirection) tea.Cmd {
	tab := m.currentTab()
	if tab == nil || tab.ActivePaneID == "" {
		return nil
	}
	return m.openSplitTerminalPickerCmd(m.workspace.ActiveTab, tab.ActivePaneID, dir)
}

func (m *Model) createPaneCmd(tabIndex int, targetID string, split SplitDirection) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		m.logger.Debug("creating pane terminal", "tab_index", tabIndex, "target_id", targetID, "split", split)
		size := protocol.Size{Cols: 80, Rows: 24}
		created, err := m.client.Create(ctx, []string{m.cfg.DefaultShell}, "", size)
		if err != nil {
			return errMsg{m.wrapClientError("create terminal", err)}
		}
		attached, err := m.client.Attach(ctx, created.TerminalID, "collaborator")
		if err != nil {
			return errMsg{m.wrapClientError("attach terminal", err, "terminal_id", created.TerminalID)}
		}
		snap, err := m.client.Snapshot(ctx, created.TerminalID, 0, 200)
		if err != nil {
			return errMsg{m.wrapClientError("snapshot terminal", err, "terminal_id", created.TerminalID)}
		}
		paneID := m.nextPaneID()
		m.logger.Info("created pane terminal", "pane_id", paneID, "terminal_id", created.TerminalID, "tab_index", tabIndex)
		return paneCreatedMsg{
			tabIndex: tabIndex,
			targetID: targetID,
			split:    split,
			pane: &Pane{
				ID:       paneID,
				Title:    paneTitleForCommand("", m.cfg.DefaultShell, created.TerminalID),
				Viewport: m.newViewport(created.TerminalID, attached.Channel, snap),
			},
		}
	}
}

func (m *Model) restartActivePaneCmd() tea.Cmd {
	tab := m.currentTab()
	pane := activePane(tab)
	if pane == nil || paneTerminalState(pane) != "exited" {
		return nil
	}

	paneID := pane.ID
	command := append([]string(nil), pane.Command...)
	if len(command) == 0 {
		command = []string{m.cfg.DefaultShell}
	}
	name := pane.Name
	tags := cloneStringMap(pane.Tags)
	mode := pane.Mode
	offset := pane.Offset
	pin := pane.Pin
	readonly := pane.Readonly
	size := paneCreateSize(pane)
	if viewW, viewH, ok := m.paneViewportSizeInTab(tab, pane.ID); ok {
		size = paneRestartSize(pane, size, viewW, viewH)
	}

	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()

		m.logger.Info("restarting exited pane", "pane_id", paneID, "terminal_id", pane.TerminalID)
		created, err := m.client.Create(ctx, command, name, size)
		if err != nil {
			return errMsg{m.wrapClientError("restart terminal", err, "pane_id", paneID, "terminal_id", pane.TerminalID)}
		}
		if len(tags) > 0 {
			if err := m.client.SetTags(ctx, created.TerminalID, tags); err != nil {
				return errMsg{m.wrapClientError("set terminal tags", err, "pane_id", paneID, "terminal_id", created.TerminalID)}
			}
		}
		attached, err := m.client.Attach(ctx, created.TerminalID, "collaborator")
		if err != nil {
			return errMsg{m.wrapClientError("attach terminal", err, "pane_id", paneID, "terminal_id", created.TerminalID)}
		}
		snap, err := m.client.Snapshot(ctx, created.TerminalID, 0, 200)
		if err != nil {
			return errMsg{m.wrapClientError("snapshot terminal", err, "pane_id", paneID, "terminal_id", created.TerminalID)}
		}
		if snap != nil {
			if snap.Size.Cols < size.Cols {
				snap.Size.Cols = size.Cols
			}
			if snap.Size.Rows < size.Rows {
				snap.Size.Rows = size.Rows
			}
		}

		view := m.newViewport(created.TerminalID, attached.Channel, snap)
		if view.VTerm != nil {
			view.VTerm.Resize(int(size.Cols), int(size.Rows))
		}
		view.Name = name
		view.Command = command
		view.Tags = cloneStringMap(tags)
		view.TerminalState = "running"
		view.Mode = mode
		view.Offset = offset
		view.Pin = pin
		view.Readonly = readonly

		return paneReplacedMsg{
			paneID: paneID,
			pane: &Pane{
				ID:       paneID,
				Title:    paneTitleForCommand(name, firstCommandWord(command), created.TerminalID),
				Viewport: view,
			},
		}
	}
}

func paneCreateSize(pane *Pane) protocol.Size {
	if pane != nil {
		if pane.VTerm != nil {
			cols, rows := pane.VTerm.Size()
			if cols > 0 && rows > 0 {
				return protocol.Size{Cols: uint16(cols), Rows: uint16(rows)}
			}
		}
		if pane.Snapshot != nil && pane.Snapshot.Size.Cols > 0 && pane.Snapshot.Size.Rows > 0 {
			return pane.Snapshot.Size
		}
	}
	return protocol.Size{Cols: 80, Rows: 24}
}

func paneRestartSize(pane *Pane, base protocol.Size, viewW, viewH int) protocol.Size {
	size := base
	if pane == nil {
		return size
	}
	if pane.Mode == ViewportModeFixed && pane.Pin {
		minCols := uint16(max(80, viewW+pane.Offset.X))
		minRows := uint16(max(24, viewH+pane.Offset.Y))
		if size.Cols < minCols {
			size.Cols = minCols
		}
		if size.Rows < minRows {
			size.Rows = minRows
		}
	}
	return size
}

func (m *Model) createFloatingPaneCmd(tabIndex int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		m.logger.Debug("creating floating terminal", "tab_index", tabIndex)
		size := protocol.Size{Cols: 80, Rows: 24}
		created, err := m.client.Create(ctx, []string{m.cfg.DefaultShell}, "", size)
		if err != nil {
			return errMsg{m.wrapClientError("create terminal", err)}
		}
		attached, err := m.client.Attach(ctx, created.TerminalID, "collaborator")
		if err != nil {
			return errMsg{m.wrapClientError("attach terminal", err, "terminal_id", created.TerminalID)}
		}
		snap, err := m.client.Snapshot(ctx, created.TerminalID, 0, 200)
		if err != nil {
			return errMsg{m.wrapClientError("snapshot terminal", err, "terminal_id", created.TerminalID)}
		}
		paneID := m.nextPaneID()
		view := m.newViewport(created.TerminalID, attached.Channel, snap)
		view.Mode = ViewportModeFixed
		m.logger.Info("created floating terminal", "pane_id", paneID, "terminal_id", created.TerminalID, "tab_index", tabIndex)
		return paneCreatedMsg{
			tabIndex: tabIndex,
			floating: true,
			pane: &Pane{
				ID:       paneID,
				Title:    paneTitleForCommand("", m.cfg.DefaultShell, created.TerminalID),
				Viewport: view,
			},
		}
	}
}

func (m *Model) attachPane(msg paneCreatedMsg) {
	if msg.tabIndex >= len(m.workspace.Tabs) {
		return
	}
	tab := m.workspace.Tabs[msg.tabIndex]
	if msg.floating {
		tab.Panes[msg.pane.ID] = msg.pane
		tab.Floating = append(tab.Floating, &FloatingPane{
			PaneID: msg.pane.ID,
			Rect:   m.defaultFloatingRectForPane(msg.pane),
			Z:      len(tab.Floating),
		})
		tab.FloatingVisible = true
		tab.ActivePaneID = msg.pane.ID
		m.startPaneStream(msg.pane)
		m.logger.Info("attached floating pane", "pane_id", msg.pane.ID, "terminal_id", msg.pane.TerminalID, "tab_index", msg.tabIndex)
		m.invalidateRender()
		return
	}
	if tab.Root == nil {
		tab.Root = NewLeaf(msg.pane.ID)
	} else if msg.targetID != "" {
		_ = tab.Root.Split(msg.targetID, msg.split, msg.pane.ID)
		tab.LayoutPreset = layoutPresetCustom
	}
	tab.Panes[msg.pane.ID] = msg.pane
	tab.ActivePaneID = msg.pane.ID

	m.startPaneStream(msg.pane)
	m.logger.Info("attached pane", "pane_id", msg.pane.ID, "terminal_id", msg.pane.TerminalID, "tab_index", msg.tabIndex, "target_id", msg.targetID, "split", msg.split)
	m.invalidateRender()
}

func (m *Model) replacePane(msg paneReplacedMsg) {
	pane := findPane(m.workspace.Tabs, msg.paneID)
	if pane == nil {
		return
	}
	if pane.stopStream != nil {
		pane.stopStream()
	}
	pane.Title = msg.pane.Title
	pane.Viewport = msg.pane.Viewport
	m.startPaneStream(pane)
	m.logger.Info("replaced pane terminal", "pane_id", pane.ID, "terminal_id", pane.TerminalID)
	m.invalidateRender()
}

func (m *Model) startPaneStream(pane *Pane) {
	if pane == nil {
		return
	}
	m.logger.Debug("starting pane stream", "pane_id", pane.ID, "terminal_id", pane.TerminalID, "channel", pane.Channel)
	stream, stop := m.client.Stream(pane.Channel)
	pane.stopStream = stop
	if m.program != nil {
		go func(paneID string) {
			for frame := range stream {
				m.program.Send(paneOutputMsg{paneID: paneID, frame: frame})
			}
			m.logger.Debug("pane stream closed", "pane_id", paneID)
		}(pane.ID)
	}
}

func (m *Model) handlePaneOutput(msg paneOutputMsg) tea.Cmd {
	pane := findPane(m.workspace.Tabs, msg.paneID)
	if pane == nil {
		return nil
	}
	switch msg.frame.Type {
	case protocol.TypeOutput:
		beforeCursor := pane.VTerm.CursorState()
		beforeAlt := pane.VTerm.IsAltScreen()
		termCols, termRows := pane.VTerm.Size()
		_, _ = pane.VTerm.Write(msg.frame.Payload)
		afterCursor := pane.VTerm.CursorState()
		afterAlt := pane.VTerm.IsAltScreen()
		pane.live = true
		pane.TerminalState = "running"
		pane.ExitCode = nil
		pane.cellVersion++
		pane.renderDirty = true
		applyViewportDirtyRegionForOutput(pane.Viewport, msg.frame.Payload, beforeCursor, afterCursor, beforeAlt, afterAlt, termCols, termRows)
		pane.syncLost = false
		pane.recovering = false
		if tab := m.tabForPane(pane.ID); tab != nil {
			if viewW, viewH, ok := m.paneViewportSizeInTab(tab, pane.ID); ok && m.syncViewport(pane, viewW, viewH) {
				tab.renderCache = nil
			}
		}
		m.scheduleRender()
	case protocol.TypeClosed:
		code, _ := protocol.DecodeClosedPayload(msg.frame.Payload)
		m.markTerminalExited(pane.TerminalID, code)
	case protocol.TypeSyncLost:
		dropped, _ := protocol.DecodeSyncLostPayload(msg.frame.Payload)
		pane.syncLost = true
		pane.droppedBytes += dropped
		pane.renderDirty = true
		m.scheduleRender()
		if pane.recovering {
			return nil
		}
		pane.recovering = true
		return m.recoverPaneSnapshotCmd(pane.ID, pane.TerminalID, pane.droppedBytes)
	}
	return nil
}

func applyViewportDirtyRegionForOutput(view *Viewport, payload []byte, beforeCursor, afterCursor localvterm.CursorState, beforeAlt, afterAlt bool, cols, rows int) {
	if view == nil {
		return
	}
	if rowStart, rowEnd, colStart, colEnd, ok := dirtyRegionForOutput(payload, beforeCursor, afterCursor, beforeAlt, afterAlt, cols, rows); ok {
		view.markDirtyRegion(rowStart, rowEnd, colStart, colEnd)
		return
	}
	view.clearDirtyRegion()
}

func dirtyRegionForOutput(payload []byte, beforeCursor, afterCursor localvterm.CursorState, beforeAlt, afterAlt bool, cols, rows int) (int, int, int, int, bool) {
	if rows <= 0 || beforeAlt != afterAlt || len(payload) == 0 {
		return 0, 0, 0, 0, false
	}
	lineBreaks := 0
	carriageReturn := false
	for _, b := range payload {
		switch {
		case b == 0x1b:
			return 0, 0, 0, 0, false
		case b == '\n':
			lineBreaks++
		case b == '\r':
			carriageReturn = true
		case b == '\t' || b == '\b':
		case b < 0x20:
			return 0, 0, 0, 0, false
		}
	}
	if lineBreaks > 1 {
		return 0, 0, 0, 0, false
	}
	if lineBreaks == 1 && beforeCursor.Row >= rows-1 {
		return 0, 0, 0, 0, false
	}
	start := clampDirtyRow(min(beforeCursor.Row, afterCursor.Row), rows)
	end := clampDirtyRow(max(beforeCursor.Row, afterCursor.Row), rows)
	if carriageReturn {
		start = clampDirtyRow(min(start, beforeCursor.Row), rows)
		end = clampDirtyRow(max(end, beforeCursor.Row), rows)
	}
	if start > end {
		return 0, 0, 0, 0, false
	}
	colStart, colEnd, colsKnown := dirtyColsForOutput(payload, beforeCursor, afterCursor, carriageReturn, cols)
	if !colsKnown {
		colStart = 0
		colEnd = 0
	}
	return start, end, colStart, colEnd, true
}

func dirtyRowsForOutput(payload []byte, beforeCursor, afterCursor localvterm.CursorState, beforeAlt, afterAlt bool, rows int) (int, int, bool) {
	start, end, _, _, ok := dirtyRegionForOutput(payload, beforeCursor, afterCursor, beforeAlt, afterAlt, 0, rows)
	return start, end, ok
}

func dirtyColsForOutput(payload []byte, beforeCursor, afterCursor localvterm.CursorState, carriageReturn bool, cols int) (int, int, bool) {
	if carriageReturn || beforeCursor.Row != afterCursor.Row {
		return 0, 0, false
	}
	if beforeCursor.Col < 0 || afterCursor.Col < 0 {
		return 0, 0, false
	}
	if beforeCursor.Col == afterCursor.Col {
		return clampDirtyCol(beforeCursor.Col, cols), clampDirtyCol(beforeCursor.Col, cols), true
	}
	start := clampDirtyCol(min(beforeCursor.Col, afterCursor.Col), cols)
	end := clampDirtyCol(max(beforeCursor.Col, afterCursor.Col), cols)
	if end < start {
		end = start
	}
	return start, end, true
}

func clampDirtyRow(row, rows int) int {
	if rows <= 0 {
		return 0
	}
	if row < 0 {
		return 0
	}
	if row >= rows {
		return rows - 1
	}
	return row
}

func (v *Viewport) markDirtyRows(start, end int) {
	if v == nil {
		return
	}
	if start > end {
		start, end = end, start
	}
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if !v.dirtyRowsKnown {
		v.dirtyRowsKnown = true
		v.dirtyRowStart = start
		v.dirtyRowEnd = end
		return
	}
	v.dirtyRowStart = min(v.dirtyRowStart, start)
	v.dirtyRowEnd = max(v.dirtyRowEnd, end)
}

func (v *Viewport) markDirtyRegion(rowStart, rowEnd, colStart, colEnd int) {
	if v == nil {
		return
	}
	v.markDirtyRows(rowStart, rowEnd)
	if rowStart != rowEnd || colStart > colEnd {
		v.dirtyColsKnown = false
		v.dirtyColStart = 0
		v.dirtyColEnd = 0
		return
	}
	if !v.dirtyColsKnown {
		v.dirtyColsKnown = true
		v.dirtyColStart = colStart
		v.dirtyColEnd = colEnd
		return
	}
	v.dirtyColStart = min(v.dirtyColStart, colStart)
	v.dirtyColEnd = max(v.dirtyColEnd, colEnd)
}

func (v *Viewport) clearDirtyRows() {
	if v == nil {
		return
	}
	v.dirtyRowsKnown = false
	v.dirtyRowStart = 0
	v.dirtyRowEnd = 0
}

func (v *Viewport) clearDirtyRegion() {
	if v == nil {
		return
	}
	v.clearDirtyRows()
	v.dirtyColsKnown = false
	v.dirtyColStart = 0
	v.dirtyColEnd = 0
}

func clampDirtyCol(col, cols int) int {
	if cols <= 0 {
		if col < 0 {
			return 0
		}
		return col
	}
	if col < 0 {
		return 0
	}
	if col >= cols {
		return cols - 1
	}
	return col
}

func (m *Model) handlePaneRecovered(msg paneRecoveredMsg) {
	pane := findPane(m.workspace.Tabs, msg.paneID)
	if pane == nil || msg.snapshot == nil {
		return
	}
	pane.Snapshot = msg.snapshot
	pane.VTerm.LoadSnapshot(protocolScreenToVTerm(msg.snapshot.Screen), protocolCursorToVTerm(msg.snapshot.Cursor), protocolModesToVTerm(msg.snapshot.Modes))
	pane.live = true
	pane.TerminalState = "running"
	pane.syncLost = false
	pane.recovering = false
	pane.droppedBytes = msg.droppedBytes
	pane.cellVersion++
	pane.renderDirty = true
	pane.clearDirtyRegion()
	pane.cellCache = nil
	if tab := m.tabForPane(pane.ID); tab != nil {
		if viewW, viewH, ok := m.paneViewportSizeInTab(tab, pane.ID); ok && m.syncViewport(pane, viewW, viewH) {
			tab.renderCache = nil
		}
	}
	m.invalidateRender()
}

func (m *Model) handlePaneRecoveryFailed(msg paneRecoveryFailedMsg) {
	pane := findPane(m.workspace.Tabs, msg.paneID)
	if pane == nil {
		return
	}
	pane.recovering = false
	m.err = msg.err
	m.invalidateRender()
}

func (m *Model) recoverPaneSnapshotCmd(paneID, terminalID string, dropped uint64) tea.Cmd {
	if terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		snap, err := m.client.Snapshot(ctx, terminalID, 0, 200)
		if err != nil {
			return paneRecoveryFailedMsg{paneID: paneID, err: m.wrapClientError("recover snapshot", err, "terminal_id", terminalID)}
		}
		return paneRecoveredMsg{
			paneID:       paneID,
			snapshot:     snap,
			droppedBytes: dropped,
		}
	}
}

func (m *Model) loadLayoutSpecCmd(layout *LayoutSpec, workspaceName string, policy LayoutResolvePolicy) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		result, err := m.client.List(ctx)
		if err != nil {
			return errMsg{m.wrapClientError("list terminals", err)}
		}
		workspace, plans, err := BuildWorkspaceFromLayoutSpec(layout, workspaceName, result.Terminals, policy)
		if err != nil {
			return errMsg{err}
		}
		m.applyLayoutFloatingRects(workspace, layout)
		for _, tab := range workspace.Tabs {
			if tab == nil {
				continue
			}
			for paneID, pane := range tab.Panes {
				if pane == nil || pane.TerminalID == "" || paneTerminalState(pane) == "waiting" {
					continue
				}
				info := findTerminalInfo(result.Terminals, pane.TerminalID)
				attached, err := m.client.Attach(ctx, pane.TerminalID, "collaborator")
				if err != nil {
					return errMsg{m.wrapClientError("attach terminal", err, "terminal_id", pane.TerminalID)}
				}
				snap, err := m.client.Snapshot(ctx, pane.TerminalID, 0, 200)
				if err != nil {
					return errMsg{m.wrapClientError("snapshot terminal", err, "terminal_id", pane.TerminalID)}
				}
				view := m.newViewport(pane.TerminalID, attached.Channel, snap)
				if info != nil {
					view = viewportWithTerminalInfo(view, *info)
				} else {
					view.TerminalState = pane.TerminalState
				}
				view.Mode = pane.Mode
				view.Offset = pane.Offset
				view.Pin = pane.Pin
				view.Readonly = pane.Readonly
				pane.Viewport = view
				m.startPaneStream(pane)
				tab.Panes[paneID] = pane
			}
		}
		if policy == LayoutResolveCreate {
			for _, plan := range plans {
				pane := findPane(workspace.Tabs, plan.PaneID)
				if pane == nil {
					return errMsg{fmt.Errorf("missing pane for create plan %q", plan.PaneID)}
				}
				command := commandStringToSlice(plan.Terminal.Command)
				if len(command) == 0 {
					command = []string{m.cfg.DefaultShell}
				}
				created, err := m.client.Create(ctx, command, "", protocol.Size{Cols: 80, Rows: 24})
				if err != nil {
					return errMsg{m.wrapClientError("create terminal", err)}
				}
				if len(pane.Tags) > 0 {
					if err := m.client.SetTags(ctx, created.TerminalID, pane.Tags); err != nil {
						return errMsg{m.wrapClientError("set terminal tags", err, "terminal_id", created.TerminalID)}
					}
				}
				attached, err := m.client.Attach(ctx, created.TerminalID, "collaborator")
				if err != nil {
					return errMsg{m.wrapClientError("attach terminal", err, "terminal_id", created.TerminalID)}
				}
				snap, err := m.client.Snapshot(ctx, created.TerminalID, 0, 200)
				if err != nil {
					return errMsg{m.wrapClientError("snapshot terminal", err, "terminal_id", created.TerminalID)}
				}
				view := m.newViewport(created.TerminalID, attached.Channel, snap)
				view.Name = pane.Name
				view.Command = command
				view.Tags = cloneStringMap(pane.Tags)
				view.TerminalState = "running"
				view.Mode = pane.Mode
				view.Offset = pane.Offset
				view.Pin = pane.Pin
				view.Readonly = pane.Readonly
				pane.TerminalID = created.TerminalID
				pane.Title = paneTitleForCommand("", firstCommandWord(command), created.TerminalID)
				pane.Viewport = view
				m.startPaneStream(pane)
			}
		}
		m.applyLayoutFloatingRects(workspace, layout)
		msg := layoutLoadedMsg{workspace: *workspace}
		if policy == LayoutResolvePrompt && len(plans) > 0 {
			msg.prompt = append([]LayoutCreatePlan(nil), plans...)
		}
		return msg
	}
}

func (m *Model) advanceLayoutPromptAfterPaneMsg(paneID string, pane *Pane) tea.Cmd {
	if m.layoutPromptCurrent == nil {
		return nil
	}
	if paneID == "" && pane != nil {
		paneID = pane.ID
	}
	if paneID == "" || paneID != m.layoutPromptCurrent.PaneID {
		return nil
	}
	return m.advanceLayoutPromptCmd()
}

func (m *Model) advanceLayoutPromptCmd() tea.Cmd {
	if m.layoutPromptCurrent != nil {
		m.layoutPromptCurrent = nil
	}
	if len(m.layoutPromptQueue) == 0 {
		return nil
	}
	plan := m.layoutPromptQueue[0]
	m.layoutPromptQueue = m.layoutPromptQueue[1:]
	m.layoutPromptCurrent = &plan
	m.focusPaneByID(plan.PaneID)
	return m.openLayoutResolvePickerCmd(plan)
}

func (m *Model) focusPaneByID(paneID string) {
	if paneID == "" {
		return
	}
	for tabIndex, tab := range m.workspace.Tabs {
		if tab == nil || tab.Panes[paneID] == nil {
			continue
		}
		m.workspace.ActiveTab = tabIndex
		tab.ActivePaneID = paneID
		return
	}
}

func (m *Model) executeCommandPrompt(value string) tea.Cmd {
	value = strings.TrimSpace(strings.TrimPrefix(value, ":"))
	if value == "" {
		return nil
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return nil
	}
	switch fields[0] {
	case "save-layout":
		if len(fields) < 2 {
			m.notice = ""
			m.err = fmt.Errorf("save-layout requires a name")
			m.invalidateRender()
			return nil
		}
		return m.saveLayoutCmd(fields[1])
	case "load-layout":
		if len(fields) < 2 {
			m.notice = ""
			m.err = fmt.Errorf("load-layout requires a name")
			m.invalidateRender()
			return nil
		}
		policy, err := parseLayoutResolvePolicy(fields[2:])
		if err != nil {
			m.notice = ""
			m.err = err
			m.invalidateRender()
			return nil
		}
		return m.loadLayoutCmd(fields[1], policy)
	case "list-layouts":
		return m.listLayoutsCmd()
	case "delete-layout":
		if len(fields) < 2 {
			m.notice = ""
			m.err = fmt.Errorf("delete-layout requires a name")
			m.invalidateRender()
			return nil
		}
		return m.deleteLayoutCmd(fields[1])
	default:
		m.notice = ""
		m.err = fmt.Errorf("unknown command: %s", fields[0])
		m.invalidateRender()
		return nil
	}
}

func parseLayoutResolvePolicy(args []string) (LayoutResolvePolicy, error) {
	policy := LayoutResolveCreate
	if len(args) == 0 {
		return policy, nil
	}
	if len(args) > 1 {
		return "", fmt.Errorf("load-layout accepts at most one policy")
	}
	value := strings.TrimSpace(args[0])
	switch value {
	case "create", "--create":
		return LayoutResolveCreate, nil
	case "prompt", "--prompt":
		return LayoutResolvePrompt, nil
	case "skip", "--skip":
		return LayoutResolveSkip, nil
	default:
		return "", fmt.Errorf("unsupported load-layout policy: %s", value)
	}
}

func (m *Model) startStartupBootstrapCmd() tea.Cmd {
	return func() tea.Msg {
		if path := strings.TrimSpace(m.cfg.WorkspaceStatePath); path != "" {
			if exists, err := fileExists(path); err != nil {
				return errMsg{err}
			} else if exists {
				m.logger.Info("tui startup restoring workspace state", "path", path)
				if cmd := m.loadWorkspaceStateCmd(path); cmd != nil {
					return cmd()
				}
			}
		}
		if m.cfg.StartupAutoLayout {
			path, err := m.resolveAutoStartupLayoutPath()
			if err != nil {
				return errMsg{err}
			}
			if path != "" {
				m.logger.Info("tui startup auto layout discovered", "path", path)
				if cmd := m.loadLayoutCmd(path, LayoutResolveCreate); cmd != nil {
					return cmd()
				}
			}
		}
		if m.cfg.StartupPicker {
			m.logger.Info("tui startup falling back to chooser")
			if cmd := m.openBootstrapTerminalPickerCmd(0); cmd != nil {
				return cmd()
			}
			return nil
		}
		m.logger.Info("tui startup falling back to first pane creation")
		if cmd := m.createPaneCmd(0, "", ""); cmd != nil {
			return cmd()
		}
		return nil
	}
}

func (m *Model) saveLayoutCmd(name string) tea.Cmd {
	return func() tea.Msg {
		data, err := ExportLayoutYAML(name, &m.workspace)
		if err != nil {
			return errMsg{err}
		}
		path, err := m.layoutFilePath(name)
		if err != nil {
			return errMsg{err}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return errMsg{err}
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return errMsg{err}
		}
		return noticeMsg{text: "saved layout: " + name}
	}
}

func (m *Model) loadLayoutCmd(name string, policy LayoutResolvePolicy) tea.Cmd {
	return func() tea.Msg {
		path, err := m.resolveLayoutFilePath(name)
		if err != nil {
			return errMsg{err}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return errMsg{err}
		}
		layout, err := ParseLayoutYAML(data)
		if err != nil {
			return errMsg{err}
		}
		if cmd := m.loadLayoutSpecCmd(layout, layout.Name, policy); cmd != nil {
			msg := cmd()
			if loaded, ok := msg.(layoutLoadedMsg); ok {
				loaded.notice = "loaded layout: " + layout.Name
				return loaded
			}
			return msg
		}
		return noticeMsg{text: "loaded layout: " + layout.Name}
	}
}

func (m *Model) layoutFilePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("layout name is required")
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		if filepath.Ext(name) == "" {
			name += ".yaml"
		}
		return name, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if filepath.Ext(name) == "" {
		name += ".yaml"
	}
	return filepath.Join(home, ".config", "termx", "layouts", name), nil
}

func (m *Model) resolveLayoutFilePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("layout name is required")
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		if filepath.Ext(name) == "" {
			name += ".yaml"
		}
		return name, nil
	}

	fileName := name
	if filepath.Ext(fileName) == "" {
		fileName += ".yaml"
	}

	projectDirs, err := projectLayoutDirs()
	if err != nil {
		return "", err
	}
	for _, dir := range projectDirs {
		path := filepath.Join(dir, fileName)
		if exists, err := fileExists(path); err != nil {
			return "", err
		} else if exists {
			return path, nil
		}
	}

	userPath, err := m.layoutFilePath(name)
	if err != nil {
		return "", err
	}
	if exists, err := fileExists(userPath); err != nil {
		return "", err
	} else if exists {
		return userPath, nil
	}
	return "", fmt.Errorf("layout %q not found", name)
}

func (m *Model) listLayoutsCmd() tea.Cmd {
	return func() tea.Msg {
		names, err := m.listLayoutNames()
		if err != nil {
			return errMsg{err}
		}
		if len(names) == 0 {
			return noticeMsg{text: "layouts: none"}
		}
		return noticeMsg{text: "layouts: " + strings.Join(names, ", ")}
	}
}

func (m *Model) deleteLayoutCmd(name string) tea.Cmd {
	return func() tea.Msg {
		path, err := m.resolveLayoutFilePath(name)
		if err != nil {
			return errMsg{err}
		}
		if err := os.Remove(path); err != nil {
			return errMsg{err}
		}
		return noticeMsg{text: "deleted layout: " + strings.TrimSpace(name)}
	}
}

func (m *Model) listLayoutNames() ([]string, error) {
	seen := map[string]struct{}{}
	names := make([]string, 0)
	addDir := func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name, ok := layoutNameForFile(entry.Name())
			if !ok {
				continue
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
		return nil
	}

	projectDirs, err := projectLayoutDirs()
	if err != nil {
		return nil, err
	}
	for _, dir := range projectDirs {
		if err := addDir(dir); err != nil {
			return nil, err
		}
	}

	userPath, err := m.layoutFilePath("placeholder")
	if err != nil {
		return nil, err
	}
	if err := addDir(filepath.Dir(userPath)); err != nil {
		return nil, err
	}

	slices.Sort(names)
	return names, nil
}

func projectLayoutDirs() ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, 8)
	for {
		dirs = append(dirs, filepath.Join(cwd, ".termx", "layouts"))
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}
	return dirs, nil
}

func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

func layoutNameForFile(name string) (string, bool) {
	switch ext := filepath.Ext(name); ext {
	case ".yaml", ".yml":
		return strings.TrimSuffix(name, ext), true
	default:
		return "", false
	}
}

func (m *Model) handlePaneResize(msg paneResizeMsg) {
	pane := findPaneByChannel(m.workspace.Tabs, msg.channel)
	if pane == nil {
		return
	}
	pane.VTerm.Resize(int(msg.cols), int(msg.rows))
	if pane.Snapshot != nil {
		pane.Snapshot.Size = protocol.Size{Cols: msg.cols, Rows: msg.rows}
	}
	pane.cellVersion++
	pane.renderDirty = true
	pane.clearDirtyRegion()
	pane.cellCache = nil
	if tab := m.tabForPane(pane.ID); tab != nil {
		if viewW, viewH, ok := m.paneViewportSizeInTab(tab, pane.ID); ok && m.syncViewport(pane, viewW, viewH) {
			tab.renderCache = nil
		}
	}
	m.invalidateRender()
}

func (m *Model) moveFocus(dir Direction) {
	tab := m.currentTab()
	if tab == nil || tab.Root == nil {
		return
	}
	rects := tab.Root.Rects(Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)})
	if next := tab.Root.Adjacent(tab.ActivePaneID, dir, rects); next != "" {
		tab.ActivePaneID = next
		m.invalidateRender()
	}
}

func (m *Model) swapActivePane(delta int) {
	tab := m.currentTab()
	if tab == nil || tab.Root == nil || tab.ActivePaneID == "" {
		return
	}
	if tab.Root.SwapWithNeighbor(tab.ActivePaneID, delta) {
		tab.LayoutPreset = layoutPresetCustom
		m.invalidateRender()
	}
}

func (m *Model) resizeActivePane(dir Direction, step int) {
	tab := m.currentTab()
	if tab == nil || tab.Root == nil || tab.ActivePaneID == "" || step <= 0 {
		return
	}
	if tab.ZoomedPaneID != "" {
		return
	}
	rootRect := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	if tab.Root.AdjustPaneBoundary(tab.ActivePaneID, dir, step, 4, rootRect) {
		tab.LayoutPreset = layoutPresetCustom
		m.invalidateRender()
	}
}

func (m *Model) visiblePaneRects(tab *Tab) map[string]Rect {
	if tab == nil || tab.Root == nil {
		rects := make(map[string]Rect)
		for _, floating := range m.visibleFloatingPanes(tab) {
			rects[floating.PaneID] = floating.Rect
		}
		if len(rects) == 0 {
			return nil
		}
		return rects
	}
	rootRect := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	rects := tab.Root.Rects(rootRect)
	if tab.ZoomedPaneID != "" {
		if _, ok := rects[tab.ZoomedPaneID]; ok {
			rects = map[string]Rect{tab.ZoomedPaneID: rootRect}
		}
	}
	for _, floating := range m.visibleFloatingPanes(tab) {
		rects[floating.PaneID] = floating.Rect
	}
	return rects
}

func (m *Model) visibleFloatingPanes(tab *Tab) []*FloatingPane {
	if tab == nil || !tab.FloatingVisible || len(tab.Floating) == 0 {
		return nil
	}
	rootRect := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	out := make([]*FloatingPane, 0, len(tab.Floating))
	for _, floating := range tab.Floating {
		if floating == nil {
			continue
		}
		entry := *floating
		entry.Rect = clampFloatingRect(entry.Rect, rootRect)
		out = append(out, &entry)
	}
	slices.SortStableFunc(out, func(a, b *FloatingPane) int {
		if a.Z != b.Z {
			return a.Z - b.Z
		}
		return strings.Compare(a.PaneID, b.PaneID)
	})
	return out
}

func (m *Model) clampFloatingPanes() {
	tab := m.currentTab()
	if tab == nil || len(tab.Floating) == 0 {
		return
	}
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	changed := false
	for _, floating := range tab.Floating {
		if floating == nil {
			continue
		}
		next := clampFloatingRect(floating.Rect, bounds)
		if next != floating.Rect {
			floating.Rect = next
			changed = true
		}
	}
	if changed {
		tab.renderCache = nil
	}
}

func clampFloatingRect(rect, bounds Rect) Rect {
	if bounds.W <= 0 || bounds.H <= 0 {
		return Rect{W: max(1, rect.W), H: max(1, rect.H)}
	}
	rect.W = max(8, min(rect.W, bounds.W))
	rect.H = max(4, min(rect.H, bounds.H))
	if rect.X < bounds.X {
		rect.X = bounds.X
	}
	if rect.Y < bounds.Y {
		rect.Y = bounds.Y
	}
	maxX := bounds.X + bounds.W - rect.W
	maxY := bounds.Y + bounds.H - rect.H
	if rect.X > maxX {
		rect.X = maxX
	}
	if rect.Y > maxY {
		rect.Y = maxY
	}
	return rect
}

func removeFloatingPane(entries []*FloatingPane, paneID string) []*FloatingPane {
	if len(entries) == 0 {
		return nil
	}
	out := entries[:0]
	for _, entry := range entries {
		if entry == nil || entry.PaneID == paneID {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func (m *Model) paneViewportSizeInTab(tab *Tab, paneID string) (int, int, bool) {
	rects := m.visiblePaneRects(tab)
	rect, ok := rects[paneID]
	if !ok {
		return 0, 0, false
	}
	return max(1, rect.W-2), max(1, rect.H-2), true
}

func paneContentSize(pane *Pane) (int, int) {
	if pane == nil {
		return 1, 1
	}

	width := 1
	height := 1
	switch {
	case pane.live && pane.VTerm != nil:
		width, height = pane.VTerm.Size()
	case pane.Snapshot != nil:
		width = max(1, int(pane.Snapshot.Size.Cols))
		height = max(1, int(pane.Snapshot.Size.Rows))
		for _, row := range pane.Snapshot.Screen.Cells {
			if len(row) > width {
				width = len(row)
			}
		}
	}

	cursor := paneCursor(pane)
	if cursor.Col+1 > width {
		width = cursor.Col + 1
	}
	if cursor.Row+1 > height {
		height = cursor.Row + 1
	}
	return max(1, width), max(1, height)
}

func normalizeViewportOffset(pane *Pane, viewW, viewH int) bool {
	if pane == nil {
		return false
	}
	viewW = max(1, viewW)
	viewH = max(1, viewH)
	contentW, contentH := paneContentSize(pane)
	maxX := max(0, contentW-viewW)
	maxY := max(0, contentH-viewH)
	next := pane.Offset
	if next.X < 0 {
		next.X = 0
	}
	if next.Y < 0 {
		next.Y = 0
	}
	if next.X > maxX {
		next.X = maxX
	}
	if next.Y > maxY {
		next.Y = maxY
	}
	if next == pane.Offset {
		return false
	}
	pane.Offset = next
	return true
}

func followViewportCursor(pane *Pane, viewW, viewH int) bool {
	if pane == nil {
		return false
	}
	viewW = max(1, viewW)
	viewH = max(1, viewH)
	next := pane.Offset
	cursor := paneCursor(pane)
	if cursor.Col >= 0 {
		if cursor.Col < next.X {
			next.X = cursor.Col
		} else if cursor.Col >= next.X+viewW {
			next.X = cursor.Col - viewW + 1
		}
	}
	if cursor.Row >= 0 {
		if cursor.Row < next.Y {
			next.Y = cursor.Row
		} else if cursor.Row >= next.Y+viewH {
			next.Y = cursor.Row - viewH + 1
		}
	}
	changed := next != pane.Offset
	pane.Offset = next
	if normalizeViewportOffset(pane, viewW, viewH) {
		return true
	}
	return changed
}

func (m *Model) syncViewport(pane *Pane, viewW, viewH int) bool {
	if pane == nil || pane.Mode != ViewportModeFixed {
		return false
	}
	if pane.Pin {
		return normalizeViewportOffset(pane, viewW, viewH)
	}
	return followViewportCursor(pane, viewW, viewH)
}

func (m *Model) toggleActiveViewportMode() {
	tab := m.currentTab()
	pane := activePane(tab)
	if pane == nil {
		return
	}

	if pane.Mode == ViewportModeFixed {
		pane.Mode = ViewportModeFit
		pane.Pin = false
		pane.Offset = Point{}
		tab.renderCache = nil
		m.invalidateRender()
		return
	}

	pane.Mode = ViewportModeFixed
	pane.Pin = false
	if viewW, viewH, ok := m.paneViewportSizeInTab(tab, pane.ID); ok {
		_ = m.syncViewport(pane, viewW, viewH)
	} else {
		pane.Offset = Point{}
	}
	tab.renderCache = nil
	m.invalidateRender()
}

func (m *Model) toggleActiveViewportPin() {
	tab := m.currentTab()
	pane := activePane(tab)
	if pane == nil || pane.Mode != ViewportModeFixed {
		return
	}
	pane.Pin = !pane.Pin
	if !pane.Pin {
		if viewW, viewH, ok := m.paneViewportSizeInTab(tab, pane.ID); ok {
			_ = m.syncViewport(pane, viewW, viewH)
		}
	}
	tab.renderCache = nil
	m.invalidateRender()
}

func (m *Model) toggleActiveViewportReadonly() {
	tab := m.currentTab()
	pane := activePane(tab)
	if pane == nil {
		return
	}
	pane.Readonly = !pane.Readonly
	tab.renderCache = nil
	m.invalidateRender()
}

func (m *Model) panActiveViewport(dx, dy int) {
	tab := m.currentTab()
	pane := activePane(tab)
	if pane == nil || pane.Mode != ViewportModeFixed || !pane.Pin {
		return
	}
	viewW, viewH, ok := m.paneViewportSizeInTab(tab, pane.ID)
	if !ok {
		return
	}
	next := pane.Offset
	next.X += dx
	next.Y += dy
	if next == pane.Offset && dx == 0 && dy == 0 {
		return
	}
	pane.Offset = next
	_ = normalizeViewportOffset(pane, viewW, viewH)
	tab.renderCache = nil
	m.invalidateRender()
}

func (m *Model) toggleFloatingLayerVisibility() {
	tab := m.currentTab()
	if tab == nil || len(tab.Floating) == 0 {
		return
	}
	tab.FloatingVisible = !tab.FloatingVisible
	if !tab.FloatingVisible {
		_ = m.focusTiledPane()
	}
	tab.renderCache = nil
	m.invalidateRender()
}

func (m *Model) cycleFloatingFocus() {
	tab := m.currentTab()
	if tab == nil {
		return
	}
	floating := m.visibleFloatingPanes(tab)
	if len(floating) == 0 {
		return
	}
	if !isFloatingPane(tab, tab.ActivePaneID) {
		tab.ActivePaneID = floating[0].PaneID
		tab.renderCache = nil
		m.invalidateRender()
		return
	}
	for i, entry := range floating {
		if entry.PaneID != tab.ActivePaneID {
			continue
		}
		tab.ActivePaneID = floating[(i+1)%len(floating)].PaneID
		tab.renderCache = nil
		m.invalidateRender()
		return
	}
	tab.ActivePaneID = floating[0].PaneID
	tab.renderCache = nil
	m.invalidateRender()
}

func (m *Model) raiseActiveFloatingPane() {
	tab := m.currentTab()
	if tab == nil || !isFloatingPane(tab, tab.ActivePaneID) {
		return
	}
	reorderFloatingPanes(tab, tab.ActivePaneID, true)
	tab.renderCache = nil
	m.invalidateRender()
}

func (m *Model) lowerActiveFloatingPane() {
	tab := m.currentTab()
	if tab == nil || !isFloatingPane(tab, tab.ActivePaneID) {
		return
	}
	reorderFloatingPanes(tab, tab.ActivePaneID, false)
	tab.renderCache = nil
	m.invalidateRender()
}

func (m *Model) focusTiledPane() bool {
	tab := m.currentTab()
	if tab == nil || !isFloatingPane(tab, tab.ActivePaneID) {
		return false
	}
	if paneID := firstTiledPaneID(tab); paneID != "" {
		tab.ActivePaneID = paneID
		tab.renderCache = nil
		m.invalidateRender()
		return true
	}
	return false
}

func (m *Model) moveActiveFloatingPane(dx, dy int) {
	tab := m.currentTab()
	if tab == nil || !isFloatingPane(tab, tab.ActivePaneID) {
		return
	}
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	for _, entry := range tab.Floating {
		if entry == nil || entry.PaneID != tab.ActivePaneID {
			continue
		}
		rect := entry.Rect
		rect.X += dx
		rect.Y += dy
		entry.Rect = clampFloatingRect(rect, bounds)
		tab.renderCache = nil
		m.invalidateRender()
		return
	}
}

func (m *Model) resizeActiveFloatingPane(dw, dh int) {
	tab := m.currentTab()
	if tab == nil || !isFloatingPane(tab, tab.ActivePaneID) {
		return
	}
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	for _, entry := range tab.Floating {
		if entry == nil || entry.PaneID != tab.ActivePaneID {
			continue
		}
		rect := entry.Rect
		rect.W += dw
		rect.H += dh
		entry.Rect = clampFloatingRect(rect, bounds)
		tab.renderCache = nil
		m.invalidateRender()
		return
	}
}

func (m *Model) defaultFloatingRect() Rect {
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	rect := Rect{
		W: max(24, bounds.W/2),
		H: max(8, bounds.H/2),
	}
	rect = clampFloatingRect(rect, bounds)
	rect.X = bounds.X + max(0, (bounds.W-rect.W)/2)
	rect.Y = bounds.Y + max(0, (bounds.H-rect.H)/2)
	return clampFloatingRect(rect, bounds)
}

func (m *Model) defaultFloatingRectForPane(pane *Pane) Rect {
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	contentW, contentH := paneContentSize(pane)
	rect := Rect{
		W: max(24, contentW+2),
		H: max(8, contentH+2),
	}
	rect = clampFloatingRect(rect, bounds)
	rect.X = bounds.X + max(0, (bounds.W-rect.W)/2)
	rect.Y = bounds.Y + max(0, (bounds.H-rect.H)/2)
	return clampFloatingRect(rect, bounds)
}

func (m *Model) applyLayoutFloatingRects(workspace *Workspace, layout *LayoutSpec) {
	if workspace == nil || layout == nil {
		return
	}
	limit := min(len(workspace.Tabs), len(layout.Tabs))
	for i := 0; i < limit; i++ {
		tab := workspace.Tabs[i]
		tabSpec := layout.Tabs[i]
		if tab == nil || len(tab.Floating) == 0 || len(tabSpec.Floating) == 0 {
			continue
		}
		for j, entry := range tab.Floating {
			if entry == nil || j >= len(tabSpec.Floating) {
				continue
			}
			pane := tab.Panes[entry.PaneID]
			entry.Rect = m.layoutFloatingRect(tabSpec.Floating[j], pane)
			entry.Z = j
		}
	}
}

func (m *Model) layoutFloatingRect(spec FloatingEntrySpec, pane *Pane) Rect {
	rect := m.defaultFloatingRectForPane(pane)
	if spec.Width > 0 {
		rect.W = spec.Width
	}
	if spec.Height > 0 {
		rect.H = spec.Height
	}
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	rect = clampFloatingRect(rect, bounds)
	switch strings.TrimSpace(spec.Position) {
	case "top-left":
		rect.X = bounds.X
		rect.Y = bounds.Y
	case "top-right":
		rect.X = bounds.X + max(0, bounds.W-rect.W)
		rect.Y = bounds.Y
	case "bottom-left":
		rect.X = bounds.X
		rect.Y = bounds.Y + max(0, bounds.H-rect.H)
	case "bottom-right":
		rect.X = bounds.X + max(0, bounds.W-rect.W)
		rect.Y = bounds.Y + max(0, bounds.H-rect.H)
	default:
		rect.X = bounds.X + max(0, (bounds.W-rect.W)/2)
		rect.Y = bounds.Y + max(0, (bounds.H-rect.H)/2)
	}
	return clampFloatingRect(rect, bounds)
}

func (m *Model) cycleActiveLayout() {
	tab := m.currentTab()
	if tab == nil || tab.Root == nil {
		return
	}
	ids := tab.Root.LeafIDs()
	if len(ids) < 2 {
		return
	}
	next := tab.LayoutPreset + 1
	if next < layoutPresetEvenHorizontal || next >= layoutPresetCount {
		next = layoutPresetEvenHorizontal
	}
	if root := buildPresetLayout(ids, next); root != nil {
		tab.Root = root
		tab.LayoutPreset = next
		m.invalidateRender()
	}
}

func (m *Model) resizeVisiblePanesCmd() tea.Cmd {
	tab := m.currentTab()
	if tab == nil {
		return nil
	}
	rects := m.visiblePaneRects(tab)
	cmds := make([]tea.Cmd, 0, len(rects))
	for paneID, rect := range rects {
		pane := tab.Panes[paneID]
		if pane == nil {
			continue
		}
		cols := uint16(max(2, rect.W-2))
		rows := uint16(max(2, rect.H-2))
		if !paneAllowsResize(pane) {
			tab.renderCache = nil
			m.invalidateRender()
			continue
		}
		if pane.Mode == ViewportModeFixed {
			_ = m.syncViewport(pane, int(cols), int(rows))
			tab.renderCache = nil
			m.invalidateRender()
			continue
		}
		cmds = append(cmds, func(channel uint16, cols, rows uint16) tea.Cmd {
			return func() tea.Msg {
				ctx, cancel := m.requestContext()
				defer cancel()
				if err := m.client.Resize(ctx, channel, cols, rows); err != nil {
					return errMsg{m.wrapClientError("resize terminal", err, "channel", channel)}
				}
				return paneResizeMsg{channel: channel, cols: cols, rows: rows}
			}
		}(pane.Channel, cols, rows))
	}
	return tea.Batch(cmds...)
}

func (m *Model) closeActivePaneCmd() tea.Cmd {
	tab := m.currentTab()
	if tab == nil {
		return nil
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		return nil
	}
	paneID := pane.ID
	return func() tea.Msg {
		return paneDetachedMsg{paneID: paneID}
	}
}

func (m *Model) killActiveTerminalCmd() tea.Cmd {
	tab := m.currentTab()
	if tab == nil {
		return nil
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		if err := m.client.Kill(ctx, pane.TerminalID); err != nil {
			return errMsg{m.wrapClientError("kill terminal", err, "terminal_id", pane.TerminalID)}
		}
		return terminalClosedMsg{terminalID: pane.TerminalID}
	}
}

func (m *Model) killActiveTabCmd() tea.Cmd {
	tabIndex := m.workspace.ActiveTab
	tab := m.currentTab()
	if tab == nil || len(tab.Panes) == 0 {
		return nil
	}

	terminalIDs := make([]string, 0, len(tab.Panes))
	seen := make(map[string]struct{}, len(tab.Panes))
	for _, pane := range tab.Panes {
		if pane != nil {
			if _, ok := seen[pane.TerminalID]; ok {
				continue
			}
			seen[pane.TerminalID] = struct{}{}
			terminalIDs = append(terminalIDs, pane.TerminalID)
		}
	}

	return func() tea.Msg {
		for _, terminalID := range terminalIDs {
			ctx, cancel := m.requestContext()
			err := m.client.Kill(ctx, terminalID)
			cancel()
			if err != nil {
				return errMsg{m.wrapClientError("kill terminal", err, "terminal_id", terminalID)}
			}
		}
		return tabClosedMsg{tabIndex: tabIndex}
	}
}

func (m *Model) removePane(paneID string) bool {
	for i, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		if _, ok := tab.Panes[paneID]; !ok {
			continue
		}
		if pane := tab.Panes[paneID]; pane != nil && pane.stopStream != nil {
			pane.stopStream()
		}
		delete(tab.Panes, paneID)
		tab.Floating = removeFloatingPane(tab.Floating, paneID)
		if tab.Root != nil {
			tab.Root = tab.Root.Remove(paneID)
		}
		tab.LayoutPreset = layoutPresetCustom
		if tab.ZoomedPaneID == paneID {
			tab.ZoomedPaneID = ""
		}
		if len(tab.Panes) == 0 {
			m.workspace.Tabs = append(m.workspace.Tabs[:i], m.workspace.Tabs[i+1:]...)
			switch {
			case len(m.workspace.Tabs) == 0:
				m.workspace.ActiveTab = 0
				return true
			case m.workspace.ActiveTab > i:
				m.workspace.ActiveTab--
			case m.workspace.ActiveTab >= len(m.workspace.Tabs):
				m.workspace.ActiveTab = len(m.workspace.Tabs) - 1
			}
			if current := m.currentTab(); current != nil && current.ActivePaneID == "" {
				current.ActivePaneID = firstPaneID(current.Panes)
			}
			return false
		}
		if tab.ActivePaneID == paneID || tab.ActivePaneID == "" {
			tab.ActivePaneID = firstPaneID(tab.Panes)
		}
		m.invalidateRender()
		return false
	}
	return false
}

func (m *Model) removeTerminal(terminalID string) bool {
	if terminalID == "" {
		return false
	}
	for {
		pane := findPaneByTerminalID(m.workspace.Tabs, terminalID)
		if pane == nil {
			return false
		}
		if m.removePane(pane.ID) {
			return true
		}
	}
}

func (m *Model) markTerminalKilled(terminalID string) {
	if terminalID == "" {
		return
	}
	changed := false
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != terminalID {
				continue
			}
			if pane.stopStream != nil {
				pane.stopStream()
				pane.stopStream = nil
			}
			pane.live = false
			pane.Snapshot = nil
			pane.TerminalState = "killed"
			pane.ExitCode = nil
			pane.cellVersion++
			pane.renderDirty = true
			pane.clearDirtyRegion()
			pane.cellCache = nil
			tab.renderCache = nil
			changed = true
		}
	}
	if changed {
		m.invalidateRender()
	}
}

func (m *Model) markTerminalExited(terminalID string, exitCode int) {
	if terminalID == "" {
		return
	}
	changed := false
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != terminalID {
				continue
			}
			if pane.stopStream != nil {
				pane.stopStream()
				pane.stopStream = nil
			}
			pane.live = pane.VTerm != nil
			pane.TerminalState = "exited"
			code := exitCode
			pane.ExitCode = &code
			pane.renderDirty = true
			pane.cellVersion++
			pane.clearDirtyRegion()
			changed = true
		}
	}
	if changed {
		m.invalidateRender()
	}
}

func (m *Model) replaceWorkspace(workspace Workspace) {
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane != nil && pane.stopStream != nil {
				pane.stopStream()
				pane.stopStream = nil
			}
		}
	}
	m.workspace = workspace
	if len(m.workspace.Tabs) == 0 {
		m.workspace.Tabs = []*Tab{newTab("1")}
		m.workspace.ActiveTab = 0
	}
	if m.workspace.ActiveTab < 0 || m.workspace.ActiveTab >= len(m.workspace.Tabs) {
		m.workspace.ActiveTab = 0
	}
	if tab := m.currentTab(); tab != nil && tab.ActivePaneID == "" {
		tab.ActivePaneID = firstPaneID(tab.Panes)
	}
	m.renderCache = ""
	m.invalidateRender()
}

func (m *Model) removeTab(index int) bool {
	if index < 0 || index >= len(m.workspace.Tabs) {
		return false
	}
	tab := m.workspace.Tabs[index]
	if tab != nil {
		for _, pane := range tab.Panes {
			if pane != nil && pane.stopStream != nil {
				pane.stopStream()
			}
		}
	}

	m.workspace.Tabs = append(m.workspace.Tabs[:index], m.workspace.Tabs[index+1:]...)
	switch {
	case len(m.workspace.Tabs) == 0:
		m.workspace.ActiveTab = 0
		return true
	case m.workspace.ActiveTab > index:
		m.workspace.ActiveTab--
	case m.workspace.ActiveTab >= len(m.workspace.Tabs):
		m.workspace.ActiveTab = len(m.workspace.Tabs) - 1
	}

	if current := m.currentTab(); current != nil && current.ActivePaneID == "" {
		current.ActivePaneID = firstPaneID(current.Panes)
	}
	m.invalidateRender()
	return false
}

func (m *Model) sendToActive(data []byte) tea.Cmd {
	if len(data) == 0 {
		return nil
	}
	tab := m.currentTab()
	if tab == nil {
		return nil
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		return nil
	}
	if !paneAcceptsInput(pane) {
		return nil
	}
	if pane.Readonly {
		data = filterReadonlyInput(data)
		if len(data) == 0 {
			return nil
		}
	}
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		if err := m.client.Input(ctx, pane.Channel, data); err != nil {
			return errMsg{m.wrapClientError("send input", err, "channel", pane.Channel)}
		}
		return nil
	}
}

func filterReadonlyInput(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	out := make([]byte, 0, len(data))
	for _, b := range data {
		if b == 0x03 {
			out = append(out, b)
		}
	}
	return out
}

func paneAcceptsInput(pane *Pane) bool {
	return pane != nil && paneTerminalState(pane) == "running"
}

func paneAllowsResize(pane *Pane) bool {
	return pane != nil && paneTerminalState(pane) == "running"
}

func paneTerminalState(pane *Pane) string {
	if pane == nil || pane.Viewport == nil || pane.TerminalState == "" {
		return "running"
	}
	return pane.TerminalState
}

func (m *Model) currentTab() *Tab {
	if m.workspace.ActiveTab < 0 || m.workspace.ActiveTab >= len(m.workspace.Tabs) {
		return nil
	}
	return m.workspace.Tabs[m.workspace.ActiveTab]
}

func (m *Model) renderTabBar() string {
	items := make([]string, 0, len(m.workspace.Tabs))
	for i, tab := range m.workspace.Tabs {
		label := fmt.Sprintf(" %d:%s ", i+1, tab.Name)
		if i == m.workspace.ActiveTab {
			label = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#0f172a")).
				Background(lipgloss.Color("#fbbf24")).
				Render(label)
		} else {
			label = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#cbd5e1")).
				Background(lipgloss.Color("#334155")).
				Render(label)
		}
		items = append(items, label)
	}
	left := strings.Join(items, " ")
	right := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#93c5fd")).
		Background(lipgloss.Color("#0f172a")).
		Bold(true).
		Render(" ws:" + m.workspace.Name + " ")
	return fillHorizontal(left, right, m.width, lipgloss.NewStyle().Background(lipgloss.Color("#0f172a")))
}

func (m *Model) renderStatus() string {
	if m.prompt != nil {
		left := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f8fafc")).
			Background(lipgloss.Color("#1d4ed8")).
			Bold(true).
			Render(" " + m.prompt.Title + ": " + m.prompt.Value + "_" + " ")
		right := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#dbeafe")).
			Background(lipgloss.Color("#1d4ed8")).
			Render(" enter save | esc cancel ")
		return fillHorizontal(left, right, m.width, lipgloss.NewStyle().Background(lipgloss.Color("#1d4ed8")))
	}

	parts := []string{"C-a ? help", "split:\"/%", "nav:hjkl", "resize:HJKL", "swap:{/}", "layout:Space", "tab:c , n/p &", "zoom:z", "detach:d"}
	prefixParts := make([]string, 0, 3)
	if m.prefixActive {
		parts = append(parts, "[prefix]")
	}
	if m.showHelp {
		parts = append(parts, "[help]")
	}
	if tab := m.currentTab(); tab != nil {
		if pane := tab.Panes[tab.ActivePaneID]; pane != nil {
			parts = append(parts, paneLayerStatusParts(tab, tab.ActivePaneID)...)
			parts = append(parts, "pane:"+pane.TerminalID)
			parts = append(parts, viewportStatusParts(pane)...)
			switch paneTerminalState(pane) {
			case "exited":
				prefixParts = append(prefixParts, "restart:r", "attach:C-a f", "close:C-a x")
			case "killed":
				prefixParts = append(prefixParts, "attach:C-a f", "close:C-a x")
			}
			if pane.syncLost || pane.recovering || pane.catchingUp {
				parts = append(parts, fmt.Sprintf("catching-up:%dB", pane.droppedBytes))
			}
		}
	}
	if len(prefixParts) > 0 {
		parts = append(prefixParts, parts...)
	}
	if m.notice != "" {
		parts = append([]string{m.notice}, parts...)
	}
	if m.err != nil {
		parts = append([]string{"err:" + m.err.Error()}, parts...)
	}
	left := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e2e8f0")).
		Background(lipgloss.Color("#1e293b")).
		Render(" " + strings.Join(parts, " | ") + " ")
	right := ""
	if tab := m.currentTab(); tab != nil {
		if pane := tab.Panes[tab.ActivePaneID]; pane != nil {
			right = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#fde68a")).
				Background(lipgloss.Color("#1e293b")).
				Bold(true).
				Render(" " + pane.TerminalID + " ")
		}
	}
	return fillHorizontal(left, right, m.width, lipgloss.NewStyle().Background(lipgloss.Color("#1e293b")))
}

func viewportStatusParts(pane *Pane) []string {
	if pane == nil {
		return nil
	}
	parts := []string{"state:" + paneTerminalState(pane), "mode:" + string(pane.Mode)}
	if pane.Mode == ViewportModeFixed {
		parts = append(parts, fmt.Sprintf("offset:%d,%d", pane.Offset.X, pane.Offset.Y))
		if pane.Pin {
			parts = append(parts, "pinned")
		}
	}
	if pane.Readonly {
		parts = append(parts, "readonly")
	}
	return parts
}

func paneLayerStatusParts(tab *Tab, paneID string) []string {
	z, total, ok := floatingPaneOrder(tab, paneID)
	if !ok {
		return nil
	}
	parts := []string{"layer:floating"}
	if total > 1 {
		parts = append(parts, "z:"+itoa(z))
	}
	return parts
}

func (m *Model) paneLines(pane *Pane) []string {
	if pane == nil {
		return nil
	}
	grid := paneCells(pane)
	lines := make([]string, 0, len(grid))
	for _, row := range grid {
		lines = append(lines, rowToANSI(row))
	}
	return lines
}

func newTab(name string) *Tab {
	return &Tab{
		Name:            name,
		Panes:           make(map[string]*Pane),
		FloatingVisible: true,
		LayoutPreset:    layoutPresetCustom,
	}
}

func buildPresetLayout(ids []string, preset int) *LayoutNode {
	switch preset {
	case layoutPresetEvenHorizontal:
		return buildEvenLayout(ids, SplitHorizontal)
	case layoutPresetEvenVertical:
		return buildEvenLayout(ids, SplitVertical)
	case layoutPresetMainHorizontal:
		return buildMainLayout(ids, SplitVertical)
	case layoutPresetMainVertical:
		return buildMainLayout(ids, SplitHorizontal)
	case layoutPresetTiled:
		return buildTiledLayout(ids, SplitVertical)
	default:
		return nil
	}
}

func buildEvenLayout(ids []string, dir SplitDirection) *LayoutNode {
	switch len(ids) {
	case 0:
		return nil
	case 1:
		return NewLeaf(ids[0])
	}
	leftCount := len(ids) / 2
	if leftCount < 1 {
		leftCount = 1
	}
	return &LayoutNode{
		Direction: dir,
		Ratio:     float64(leftCount) / float64(len(ids)),
		First:     buildEvenLayout(ids[:leftCount], dir),
		Second:    buildEvenLayout(ids[leftCount:], dir),
	}
}

func buildMainLayout(ids []string, dir SplitDirection) *LayoutNode {
	switch len(ids) {
	case 0:
		return nil
	case 1:
		return NewLeaf(ids[0])
	}
	restDir := SplitHorizontal
	if dir == SplitHorizontal {
		restDir = SplitVertical
	}
	return &LayoutNode{
		Direction: dir,
		Ratio:     0.6,
		First:     NewLeaf(ids[0]),
		Second:    buildEvenLayout(ids[1:], restDir),
	}
}

func buildTiledLayout(ids []string, dir SplitDirection) *LayoutNode {
	switch len(ids) {
	case 0:
		return nil
	case 1:
		return NewLeaf(ids[0])
	}
	leftCount := len(ids) / 2
	if leftCount < 1 {
		leftCount = 1
	}
	nextDir := SplitHorizontal
	if dir == SplitHorizontal {
		nextDir = SplitVertical
	}
	return &LayoutNode{
		Direction: dir,
		Ratio:     float64(leftCount) / float64(len(ids)),
		First:     buildTiledLayout(ids[:leftCount], nextDir),
		Second:    buildTiledLayout(ids[leftCount:], nextDir),
	}
}

func findPane(tabs []*Tab, paneID string) *Pane {
	for _, tab := range tabs {
		if pane := tab.Panes[paneID]; pane != nil {
			return pane
		}
	}
	return nil
}

func (m *Model) tabForPane(paneID string) *Tab {
	for _, tab := range m.workspace.Tabs {
		if tab != nil && tab.Panes[paneID] != nil {
			return tab
		}
	}
	return nil
}

func (m *Model) beginRenameTab() {
	tab := m.currentTab()
	if tab == nil {
		return
	}
	m.prompt = &textPrompt{
		Title:    "rename tab",
		Original: tab.Name,
	}
	m.invalidateRender()
}

func (m *Model) beginCommandPrompt() {
	m.prompt = &textPrompt{Title: "command"}
	m.invalidateRender()
}

func (m *Model) appendPrompt(value string) {
	if m.prompt == nil || value == "" {
		return
	}
	m.prompt.Value += value
	m.invalidateRender()
}

func (m *Model) deletePromptRune() {
	if m.prompt == nil || m.prompt.Value == "" {
		return
	}
	runes := []rune(m.prompt.Value)
	m.prompt.Value = string(runes[:len(runes)-1])
	m.invalidateRender()
}

func (m *Model) commitPrompt() tea.Cmd {
	if m.prompt == nil {
		return nil
	}
	if m.prompt.Title == "command" {
		value := strings.TrimSpace(m.prompt.Value)
		m.prompt = nil
		m.invalidateRender()
		return m.executeCommandPrompt(value)
	}
	value := strings.TrimSpace(m.prompt.Value)
	if value == "" {
		value = m.prompt.Original
	}
	if tab := m.currentTab(); tab != nil && value != "" {
		tab.Name = value
	}
	m.prompt = nil
	m.invalidateRender()
	return nil
}

func findPaneByChannel(tabs []*Tab, channel uint16) *Pane {
	for _, tab := range tabs {
		for _, pane := range tab.Panes {
			if pane != nil && pane.Channel == channel {
				return pane
			}
		}
	}
	return nil
}

func findPaneByTerminalID(tabs []*Tab, terminalID string) *Pane {
	for _, tab := range tabs {
		for _, pane := range tab.Panes {
			if pane != nil && pane.TerminalID == terminalID {
				return pane
			}
		}
	}
	return nil
}

func firstPaneID(panes map[string]*Pane) string {
	for id := range panes {
		return id
	}
	return ""
}

func firstTiledPaneID(tab *Tab) string {
	if tab == nil || tab.Root == nil {
		return ""
	}
	ids := tab.Root.LeafIDs()
	for _, paneID := range ids {
		if paneID != "" {
			return paneID
		}
	}
	return ""
}

func isFloatingPane(tab *Tab, paneID string) bool {
	if tab == nil || paneID == "" {
		return false
	}
	for _, entry := range tab.Floating {
		if entry != nil && entry.PaneID == paneID {
			return true
		}
	}
	return false
}

func floatingPaneOrder(tab *Tab, paneID string) (int, int, bool) {
	if tab == nil || paneID == "" {
		return 0, 0, false
	}
	total := 0
	z := 0
	ok := false
	for _, entry := range tab.Floating {
		if entry == nil {
			continue
		}
		total++
		if entry.PaneID == paneID {
			z = entry.Z + 1
			ok = true
		}
	}
	return z, total, ok
}

func reorderFloatingPanes(tab *Tab, paneID string, bringToFront bool) {
	if tab == nil || len(tab.Floating) < 2 {
		return
	}
	ordered := slices.Clone(tab.Floating)
	slices.SortStableFunc(ordered, func(a, b *FloatingPane) int {
		if a == nil || b == nil {
			return 0
		}
		if a.Z != b.Z {
			return a.Z - b.Z
		}
		return strings.Compare(a.PaneID, b.PaneID)
	})
	target := -1
	for i, entry := range ordered {
		if entry != nil && entry.PaneID == paneID {
			target = i
			break
		}
	}
	if target < 0 {
		return
	}
	entry := ordered[target]
	ordered = append(ordered[:target], ordered[target+1:]...)
	if bringToFront {
		ordered = append(ordered, entry)
	} else {
		ordered = append([]*FloatingPane{entry}, ordered...)
	}
	for i, floating := range ordered {
		if floating != nil {
			floating.Z = i
		}
	}
}

func findTerminalInfo(terminals []protocol.TerminalInfo, terminalID string) *protocol.TerminalInfo {
	for i := range terminals {
		if terminals[i].ID == terminalID {
			return &terminals[i]
		}
	}
	return nil
}

func (m *Model) nextPaneID() string {
	m.nextPane++
	return fmt.Sprintf("pane-%03d", m.nextPane)
}

func encodeKey(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyCtrlC:
		return []byte{0x03}
	case tea.KeyCtrlD:
		return []byte{0x04}
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyEsc:
		return []byte{0x1b}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyRight:
		return []byte("\x1b[C")
	}
	if len(msg.Runes) > 0 {
		return []byte(string(msg.Runes))
	}
	return nil
}

func (m *Model) activatePrefix() tea.Cmd {
	m.prefixActive = true
	m.prefixSeq++
	if m.prefixTimeout <= 0 {
		return nil
	}
	seq := m.prefixSeq
	timeout := m.prefixTimeout
	return tea.Tick(timeout, func(time.Time) tea.Msg {
		return prefixTimeoutMsg{seq: seq}
	})
}

func prefixRuneKey(r rune) tea.KeyMsg {
	if r == ' ' {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func prefixDirectionKey(dir Direction) tea.KeyMsg {
	switch dir {
	case DirectionLeft:
		return tea.KeyMsg{Type: tea.KeyLeft}
	case DirectionDown:
		return tea.KeyMsg{Type: tea.KeyDown}
	case DirectionUp:
		return tea.KeyMsg{Type: tea.KeyUp}
	case DirectionRight:
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return tea.KeyMsg{}
	}
}

func prefixTabKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyTab}
}

func prefixCtrlKey(b byte) tea.KeyMsg {
	switch b {
	case 0x08:
		return tea.KeyMsg{Type: tea.KeyCtrlH}
	case 0x0a:
		return tea.KeyMsg{Type: tea.KeyCtrlJ}
	case 0x0b:
		return tea.KeyMsg{Type: tea.KeyCtrlK}
	case 0x0c:
		return tea.KeyMsg{Type: tea.KeyCtrlL}
	default:
		return tea.KeyMsg{}
	}
}

func consumeArrowSequence(data []byte) (int, bool) {
	n, _, ok, incomplete := parseArrowPrefix(data)
	if incomplete || !ok {
		return 0, false
	}
	return n, true
}

func parseArrowPrefix(data []byte) (int, Direction, bool, bool) {
	if len(data) == 0 || data[0] != 0x1b {
		return 0, "", false, false
	}
	if len(data) == 1 {
		return 0, "", false, true
	}
	if data[1] != '[' && data[1] != 'O' {
		return 0, "", false, false
	}
	if len(data) < 3 {
		return 0, "", false, true
	}
	switch data[2] {
	case 'A':
		return 3, DirectionUp, true, false
	case 'B':
		return 3, DirectionDown, true, false
	case 'C':
		return 3, DirectionRight, true, false
	case 'D':
		return 3, DirectionLeft, true, false
	default:
		return 0, "", false, false
	}
}

func parseCtrlArrowPrefix(data []byte) (int, tea.KeyMsg, bool, bool) {
	if len(data) == 0 || data[0] != 0x1b {
		return 0, tea.KeyMsg{}, false, false
	}
	seqs := []struct {
		seq []byte
		key tea.KeyMsg
	}{
		{[]byte("\x1b[1;5D"), tea.KeyMsg{Type: tea.KeyCtrlLeft}},
		{[]byte("\x1b[1;5B"), tea.KeyMsg{Type: tea.KeyCtrlDown}},
		{[]byte("\x1b[1;5A"), tea.KeyMsg{Type: tea.KeyCtrlUp}},
		{[]byte("\x1b[1;5C"), tea.KeyMsg{Type: tea.KeyCtrlRight}},
	}
	for _, candidate := range seqs {
		if len(data) < len(candidate.seq) {
			if bytes.Equal(data, candidate.seq[:len(data)]) {
				return 0, tea.KeyMsg{}, false, true
			}
			continue
		}
		if bytes.Equal(data[:len(candidate.seq)], candidate.seq) {
			return len(candidate.seq), candidate.key, true, false
		}
	}
	return 0, tea.KeyMsg{}, false, false
}

func copyBytes(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	return append([]byte(nil), data...)
}

func combineCmdsOrdered(ordered, background []tea.Cmd) tea.Cmd {
	ordered = compactCmds(ordered)
	background = compactCmds(background)
	switch {
	case len(ordered) == 0 && len(background) == 0:
		return nil
	case len(background) == 0:
		if len(ordered) == 1 {
			return ordered[0]
		}
		return tea.Sequence(ordered...)
	case len(ordered) == 0:
		if len(background) == 1 {
			return background[0]
		}
		return tea.Batch(background...)
	default:
		combined := make([]tea.Cmd, 0, len(background)+1)
		if len(ordered) == 1 {
			combined = append(combined, ordered[0])
		} else {
			combined = append(combined, tea.Sequence(ordered...))
		}
		combined = append(combined, background...)
		return tea.Batch(combined...)
	}
}

func compactCmds(cmds []tea.Cmd) []tea.Cmd {
	out := cmds[:0]
	for _, cmd := range cmds {
		if cmd != nil {
			out = append(out, cmd)
		}
	}
	return out
}

func rewriteInputForActivePane(tab *Tab, data []byte) ([]byte, []byte) {
	if len(data) == 0 {
		return nil, nil
	}
	pane := activePane(tab)
	if pane == nil || !paneWantsApplicationCursor(pane) {
		return copyBytes(data), nil
	}
	return rewriteApplicationCursorKeys(data)
}

func activePane(tab *Tab) *Pane {
	if tab == nil {
		return nil
	}
	return tab.Panes[tab.ActivePaneID]
}

func paneWantsApplicationCursor(pane *Pane) bool {
	if pane == nil {
		return false
	}
	if pane.live && pane.VTerm != nil {
		return pane.VTerm.Modes().ApplicationCursor
	}
	if pane.Snapshot != nil {
		return pane.Snapshot.Modes.ApplicationCursor
	}
	return false
}

func protocolScreenToVTerm(screen protocol.ScreenData) localvterm.ScreenData {
	rows := make([][]localvterm.Cell, len(screen.Cells))
	for y, row := range screen.Cells {
		rows[y] = make([]localvterm.Cell, len(row))
		for x, cell := range row {
			rows[y][x] = localvterm.Cell{
				Content: cell.Content,
				Width:   cell.Width,
				Style: localvterm.CellStyle{
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
	return localvterm.ScreenData{
		Cells:             rows,
		IsAlternateScreen: screen.IsAlternateScreen,
	}
}

func protocolCursorToVTerm(cursor protocol.CursorState) localvterm.CursorState {
	return localvterm.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   localvterm.CursorShape(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func protocolModesToVTerm(modes protocol.TerminalModes) localvterm.TerminalModes {
	return localvterm.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		MouseTracking:     modes.MouseTracking,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}

func rewriteApplicationCursorKeys(data []byte) ([]byte, []byte) {
	var out []byte
	i := 0
	for i < len(data) {
		if data[i] != 0x1b {
			out = append(out, data[i])
			i++
			continue
		}
		if i+1 >= len(data) {
			return out, copyBytes(data[i:])
		}
		if data[i+1] != '[' {
			out = append(out, data[i])
			i++
			continue
		}
		if i+2 >= len(data) {
			return out, copyBytes(data[i:])
		}
		switch data[i+2] {
		case 'A', 'B', 'C', 'D', 'H', 'F':
			out = append(out, 0x1b, 'O', data[i+2])
			i += 3
		default:
			out = append(out, data[i])
			i++
		}
	}
	return out, nil
}

func trimToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = truncateTextToWidth(s, width)
	if displayWidth := xansi.StringWidth(s); displayWidth < width {
		return s + strings.Repeat(" ", width-displayWidth)
	}
	return s
}

func hasVisibleLines(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(xansi.Strip(line)) != "" {
			return true
		}
	}
	return false
}

func snapshotRow(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(renderCell(cell.Content, cell.Width, cell.Style.FG, cell.Style.BG, cell.Style.Bold, cell.Style.Italic, cell.Style.Underline, cell.Style.Blink, cell.Style.Reverse, cell.Style.Strikethrough))
	}
	return b.String()
}

func vtermRow(row []localvterm.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(renderCell(cell.Content, cell.Width, cell.Style.FG, cell.Style.BG, cell.Style.Bold, cell.Style.Italic, cell.Style.Underline, cell.Style.Blink, cell.Style.Reverse, cell.Style.Strikethrough))
	}
	return b.String()
}

func renderCell(content string, width int, fg, bg string, bold, italic, underline, blink, reverse, strike bool) string {
	if content == "" {
		if width <= 0 {
			width = 1
		}
		content = strings.Repeat(" ", width)
	}
	codes := make([]string, 0, 8)
	if fg != "" {
		if rgb, ok := hexToRGB(fg); ok {
			codes = append(codes, "38", "2", strconv.Itoa(rgb[0]), strconv.Itoa(rgb[1]), strconv.Itoa(rgb[2]))
		}
	}
	if bg != "" {
		if rgb, ok := hexToRGB(bg); ok {
			codes = append(codes, "48", "2", strconv.Itoa(rgb[0]), strconv.Itoa(rgb[1]), strconv.Itoa(rgb[2]))
		}
	}
	if bold {
		codes = append(codes, "1")
	}
	if italic {
		codes = append(codes, "3")
	}
	if underline {
		codes = append(codes, "4")
	}
	if blink {
		codes = append(codes, "5")
	}
	if reverse {
		codes = append(codes, "7")
	}
	if strike {
		codes = append(codes, "9")
	}
	if len(codes) == 0 {
		return content
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + content + "\x1b[0m"
}

func hexToRGB(value string) ([3]int, bool) {
	var out [3]int
	if len(value) != 7 || value[0] != '#' {
		return out, false
	}
	for i := 0; i < 3; i++ {
		n, err := strconv.ParseUint(value[1+i*2:3+i*2], 16, 8)
		if err != nil {
			return out, false
		}
		out[i] = int(n)
	}
	return out, true
}

func max[T ~int | ~uint16](a, b T) T {
	if a > b {
		return a
	}
	return b
}

type canvas struct {
	width  int
	height int
	cells  [][]canvasCell
}

type canvasCell struct {
	Content      string
	Continuation bool
}

func newCanvas(width, height int) *canvas {
	c := &canvas{width: width, height: height, cells: make([][]canvasCell, height)}
	for y := 0; y < height; y++ {
		c.cells[y] = make([]canvasCell, width)
		for x := 0; x < width; x++ {
			c.cells[y][x] = canvasCell{Content: " "}
		}
	}
	return c
}

func (c *canvas) set(x, y int, content string, width int) {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}
	if width <= 0 {
		width = max(1, xansi.StringWidth(content))
	}
	if content == "" {
		content = " "
		width = 1
	}
	c.cells[y][x] = canvasCell{Content: content}
	for i := 1; i < width && x+i < c.width; i++ {
		c.cells[y][x+i] = canvasCell{Continuation: true}
	}
}

func (c *canvas) drawBox(rect Rect, title string, active bool) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	h := '-'
	v := '|'
	if active {
		h = '='
		v = '#'
	}
	for x := rect.X; x < rect.X+rect.W; x++ {
		c.set(x, rect.Y, string(h), 1)
		c.set(x, rect.Y+rect.H-1, string(h), 1)
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		c.set(rect.X, y, string(v), 1)
		c.set(rect.X+rect.W-1, y, string(v), 1)
	}
	c.set(rect.X, rect.Y, "+", 1)
	c.set(rect.X+rect.W-1, rect.Y, "+", 1)
	c.set(rect.X, rect.Y+rect.H-1, "+", 1)
	c.set(rect.X+rect.W-1, rect.Y+rect.H-1, "+", 1)
	title = trimToWidth(" "+title+" ", max(0, rect.W-2))
	for i, cell := range stringToDrawCells(title, drawStyle{}) {
		if i >= rect.W-2 {
			break
		}
		if cell.Continuation {
			continue
		}
		c.set(rect.X+1+i, rect.Y, cell.Content, cell.Width)
	}
}

func (c *canvas) drawText(rect Rect, lines []string) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	start := 0
	if len(lines) > rect.H {
		start = len(lines) - rect.H
	}
	lines = lines[start:]
	for y, line := range lines {
		if y >= rect.H {
			break
		}
		line = trimToWidth(line, rect.W)
		for x, cell := range stringToDrawCells(line, drawStyle{}) {
			if x >= rect.W {
				break
			}
			if cell.Continuation {
				continue
			}
			c.set(rect.X+x, rect.Y+y, cell.Content, cell.Width)
		}
	}
}

func (c *canvas) String() string {
	lines := make([]string, len(c.cells))
	for i, row := range c.cells {
		var line strings.Builder
		for _, cell := range row {
			if cell.Continuation {
				continue
			}
			content := cell.Content
			if content == "" {
				content = " "
			}
			line.WriteString(content)
		}
		lines[i] = strings.TrimRight(line.String(), " ")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) emptyStateLines() []string {
	return []string{
		"termx",
		"",
		"Flat terminal pool with a local TUI workspace.",
		"",
		"Quick start",
		"  - Ctrl-a %  split vertically",
		"  - Ctrl-a \"  split horizontally",
		"  - Ctrl-a c  new tab",
		"  - Ctrl-a ,  rename current tab",
		"  - Ctrl-a f  terminal picker",
		"  - Ctrl-a s  workspace picker",
		"  - Ctrl-a Space  cycle layout presets",
		"  - Ctrl-a &  close current tab",
		"  - Ctrl-a h/j/k/l  move focus",
		"  - Ctrl-a H/J/K/L  resize current pane",
		"  - Ctrl-a { / }  swap pane position",
		"  - Ctrl-a z  zoom current pane",
		"  - Ctrl-a x  close current viewport",
		"  - Ctrl-a X  kill current terminal",
		"  - Ctrl-a d  detach",
		"  - Ctrl-a ?  help",
	}
}

func (c *canvas) fill(rect Rect, ch rune) {
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x++ {
			c.set(x, y, string(ch), 1)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m *Model) renderTabBody(tab *Tab, width, height int) []string {
	root := tab.Root
	if tab.ZoomedPaneID != "" {
		if pane := tab.Panes[tab.ZoomedPaneID]; pane != nil {
			return m.renderPaneBlock(pane, width, height, true)
		}
	}
	return m.renderLayoutNode(tab, root, width, height)
}

func (m *Model) renderLayoutNode(tab *Tab, node *LayoutNode, width, height int) []string {
	if node == nil {
		return blankLines(width, height)
	}
	if node.IsLeaf() {
		return m.renderPaneBlock(tab.Panes[node.PaneID], width, height, node.PaneID == tab.ActivePaneID)
	}
	if node.Direction == SplitHorizontal {
		topH := int(float64(height) * ratio(node.Ratio))
		if topH < 1 {
			topH = 1
		}
		if topH >= height {
			topH = height - 1
		}
		top := m.renderLayoutNode(tab, node.First, width, topH)
		bottom := m.renderLayoutNode(tab, node.Second, width, height-topH)
		return append(top, bottom...)
	}
	leftW := int(float64(width) * ratio(node.Ratio))
	if leftW < 1 {
		leftW = 1
	}
	if leftW >= width {
		leftW = width - 1
	}
	left := m.renderLayoutNode(tab, node.First, leftW, height)
	right := m.renderLayoutNode(tab, node.Second, width-leftW, height)
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out = append(out, forceWidthANSI(l, leftW)+forceWidthANSI(r, width-leftW))
	}
	return out
}

func (m *Model) renderPaneBlock(pane *Pane, width, height int, active bool) []string {
	if width < 2 || height < 2 {
		return blankLines(max(1, width), max(1, height))
	}
	innerW := width - 2
	innerH := height - 2
	lines := m.paneLines(pane)

	borderColor := lipgloss.Color("#475569")
	titleFG := lipgloss.Color("#cbd5e1")
	if active {
		borderColor = lipgloss.Color("#22c55e")
		titleFG = lipgloss.Color("#ecfccb")
	}
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	titleStyle := lipgloss.NewStyle().Foreground(titleFG).Background(lipgloss.Color("#111827")).Bold(true)

	title := " pane "
	if pane != nil {
		title = " " + coalesce(pane.Title, pane.TerminalID, pane.ID) + " "
	}
	title = xansi.Truncate(title, innerW, "")
	top := borderStyle.Render("┌") +
		titleStyle.Render(title) +
		borderStyle.Render(strings.Repeat("─", max(0, innerW-xansi.StringWidth(title)))) +
		borderStyle.Render("┐")

	out := []string{top}
	for i := 0; i < innerH; i++ {
		content := ""
		if i < len(lines) {
			content = forceWidthANSI(lines[i], innerW)
		} else {
			content = strings.Repeat(" ", innerW)
		}
		out = append(out, borderStyle.Render("│")+content+borderStyle.Render("│"))
	}
	out = append(out, borderStyle.Render("└"+strings.Repeat("─", innerW)+"┘"))
	return out
}

func (m *Model) renderHelpScreen() string {
	tabBar := m.renderTabBar()
	status := m.renderStatus()
	contentHeight := max(1, m.height-2)
	canvas := newCanvas(m.width, contentHeight)
	rect := Rect{X: 2, Y: 1, W: max(1, m.width-4), H: max(1, contentHeight-2)}
	lines := []string{
		"Help",
		"",
		"Ctrl-a ?       close this help",
		"Ctrl-a %       split vertically",
		"Ctrl-a \"       split horizontally",
		"Ctrl-a f       terminal picker",
		"Ctrl-a s       workspace picker",
		"Ctrl-a h/j/k/l move focus",
		"Ctrl-a c       new tab",
		"Ctrl-a ,       rename current tab",
		"Ctrl-a n / p   next or previous tab",
		"Ctrl-a 1-9     jump to tab",
		"Ctrl-a Space   cycle layout presets",
		"Ctrl-a &       close current tab",
		"Ctrl-a H/J/K/L resize current pane",
		"Ctrl-a { / }   swap pane position",
		"Ctrl-a z       zoom current pane",
		"Ctrl-a M / Ctrl-a P / Ctrl-a R viewport mode, pin, readonly",
		"Ctrl-a Ctrl-h/j/k/l pan fixed viewport offset",
		"Ctrl-a x       close current viewport",
		"Ctrl-a X       kill current terminal",
		"Ctrl-a d       detach from TUI",
		"Ctrl-a w / Ctrl-a W / Ctrl-a Tab floating create, toggle, focus",
		"Ctrl-a ] / _   raise or lower floating z-order",
		"Ctrl-a Alt-h/j/k/l move floating viewport",
		"Ctrl-a Alt-H/J/K/L resize floating viewport",
		"Ctrl-a Ctrl-a  send raw Ctrl-a",
		"",
		"Colors come from your shell ANSI output and your outer terminal theme/font.",
	}
	if len(lines) > rect.H {
		lines = lines[:rect.H]
	}
	canvas.drawText(rect, lines)
	body := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e2e8f0")).
		Background(lipgloss.Color("#0f172a")).
		Render(forceHeight(canvas.String(), contentHeight))
	return strings.Join([]string{tabBar, body, status}, "\n")
}

func fillHorizontal(left, right string, width int, filler lipgloss.Style) string {
	leftW := xansi.StringWidth(left)
	rightW := xansi.StringWidth(right)
	if leftW+rightW >= width {
		return forceWidthANSI(left+right, width)
	}
	return left + filler.Render(strings.Repeat(" ", width-leftW-rightW)) + right
}

func ratio(v float64) float64 {
	if v <= 0 || v >= 1 {
		return 0.5
	}
	return v
}

func blankLines(width, height int) []string {
	out := make([]string, height)
	for i := range out {
		out[i] = strings.Repeat(" ", width)
	}
	return out
}

func coalesce(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func paneTitleForCommand(name string, command string, fallback string) string {
	if name != "" {
		return name
	}
	if command != "" {
		base := filepath.Base(command)
		if base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
		return command
	}
	return fallback
}

func forceWidthANSI(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if xansi.StringWidth(s) >= width {
		return xansi.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-xansi.StringWidth(s))
}

func forceHeight(s string, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) >= height {
		return strings.Join(lines[:height], "\n")
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func Run(client Client, cfg Config, input io.Reader, output io.Writer) error {
	model := NewModel(client, cfg)
	model.logger.Info("tui run starting", "workspace", cfg.Workspace, "startup_picker", cfg.StartupPicker, "attach_id", cfg.AttachID)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInput(nil), tea.WithOutput(output))
	model.SetProgram(program)
	stopInput, restoreInput, err := startInputForwarder(program, input)
	if err != nil {
		model.logger.Error("failed to start input forwarder", "error", err)
		return err
	}
	defer func() {
		_ = restoreInput()
	}()
	defer stopInput()
	defer model.StopRenderTicker()
	defer func() {
		if r := recover(); r != nil {
			model.logger.Error("tui panic", "panic", r, "stack", string(debug.Stack()))
			panic(r)
		}
	}()
	finalModel, err := program.Run()
	if err != nil {
		model.logger.Error("tui run failed", "error", err)
	} else {
		model.logger.Info("tui run stopped cleanly")
	}
	if persistErr := persistWorkspaceState(finalModel, cfg.WorkspaceStatePath, model.logger); persistErr != nil {
		model.logger.Error("failed to persist workspace state", "path", cfg.WorkspaceStatePath, "error", persistErr)
	}
	return err
}

func WaitForSocket(path string, timeout time.Duration, try func() error) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := try(); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for daemon at %s", path)
}
