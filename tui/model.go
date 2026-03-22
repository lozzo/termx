package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
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
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
	"go.yaml.in/yaml/v3"
)

type Config struct {
	DefaultShell       string
	Workspace          string
	AttachID           string
	IconSet            string
	StartupLayout      string
	WorkspaceStatePath string
	StartupAutoLayout  bool
	StartupPicker      bool
	Logger             *slog.Logger
	RequestTimeout     time.Duration
	PrefixTimeout      time.Duration
}

const DefaultPrefixTimeout = 3 * time.Second

type Workspace struct {
	Name      string
	Tabs      []*Tab
	ActiveTab int
}

type Tab struct {
	Name              string
	Root              *LayoutNode
	Panes             map[string]*Pane
	Floating          []*FloatingPane
	FloatingVisible   bool
	ActivePaneID      string
	ZoomedPaneID      string
	LayoutPreset      int
	AutoAcquireResize bool

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
	TerminalID     string
	Channel        uint16
	AttachMode     string
	VTerm          *localvterm.VTerm
	DefaultFG      string
	DefaultBG      string
	Snapshot       *protocol.Snapshot
	Name           string
	Command        []string
	Tags           map[string]string
	TerminalState  string
	ExitCode       *int
	Mode           ViewportMode
	Offset         Point
	Pin            bool
	Readonly       bool
	ResizeAcquired bool
	stopStream     func()

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
	Kind     string
	Title    string
	Value    string
	Original string
	Hint     string
}

type terminalCreateDraft struct {
	Action      terminalPickerAction
	Command     []string
	DefaultName string
	Name        string
	Tags        map[string]string
}

type terminalMetadataDraft struct {
	TerminalID   string
	DefaultName  string
	Name         string
	Command      []string
	OriginalTags map[string]string
	Tags         map[string]string
}

type resizeAcquireDraft struct {
	PaneID      string
	TerminalID  string
	WarningMode string
}

type terminalStopDraft struct {
	TerminalID   string
	DisplayName  string
	PaneCount    int
	LocationHint string
}

type prefixMode int

const (
	prefixModeRoot prefixMode = iota
	prefixModePane
	prefixModeResize
	prefixModeTab
	prefixModeWorkspace
	prefixModeViewport
	prefixModeFloating
	prefixModeOffsetPan
	prefixModeGlobal
)

type prefixFallback int

const (
	prefixFallbackNone prefixFallback = iota
	prefixFallbackFloatingCreate
)

type prefixDispatchResult struct {
	cmd   tea.Cmd
	keep  bool
	rearm bool
}

type mouseDragMode int

const (
	mouseDragNone mouseDragMode = iota
	mouseDragMove
	mouseDragResize
)

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

type terminalManager struct {
	Query       string
	Items       []terminalPickerItem
	Filtered    []terminalPickerItem
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
	PaneIDs  []string
}

type Model struct {
	client Client
	cfg    Config
	logger *slog.Logger
	icons  iconSet

	program    *tea.Program
	paneWriter func(*Pane, []byte) (int, error)

	renderInterval          time.Duration
	renderFastInterval      time.Duration
	renderInteractiveWindow time.Duration
	renderStatsInterval     time.Duration
	renderCache             string
	renderDirty             bool
	renderBatching          bool
	renderTickerStop        chan struct{}
	renderTickerRunning     bool
	renderPending           atomic.Bool
	renderInteractiveUntil  time.Time
	renderLastFlush         time.Time
	timeNow                 func() time.Time
	renderViewCalls         atomic.Uint64
	renderFrames            atomic.Uint64
	renderCacheHits         atomic.Uint64
	hostDefaultFG           string
	hostDefaultBG           string
	hostPalette             map[int]string

	workspace       Workspace
	width           int
	height          int
	prefixActive    bool
	directMode      bool
	prefixSeq       int
	prefixTimeout   time.Duration
	prefixMode      prefixMode
	prefixFallback  prefixFallback
	rawPending      []byte
	showHelp        bool
	prompt          *textPrompt
	terminalManager *terminalManager
	terminalPicker  *terminalPicker
	workspacePicker *workspacePicker
	inputBlocked    bool
	nextPane        int
	nextTab         int
	nextTerminal    int
	quitting        bool
	notice          string
	err             error
	eventsStarted   bool
	eventsCancel    context.CancelFunc

	workspaceStore        map[string]Workspace
	workspaceOrder        []string
	activeWorkspace       int
	layoutPromptQueue     []LayoutCreatePlan
	layoutPromptCurrent   *LayoutCreatePlan
	mouseDragPaneID       string
	mouseDragOffset       Point
	mouseDragMode         mouseDragMode
	pendingTerminalCreate *terminalCreateDraft
	pendingTerminalEdit   *terminalMetadataDraft
	pendingResizeAcquire  *resizeAcquireDraft
	pendingTerminalStop   *terminalStopDraft
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

type paneGroupReplacedMsg struct {
	promptPaneID string
	panes        []paneReplacedMsg
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
	paneID      string
	hadTerminal bool
}

type tabClosedMsg struct {
	tabIndex int
}

type errMsg struct{ err error }

type terminalMetadataUpdatedMsg struct {
	TerminalID string
	Name       string
	Tags       map[string]string
}

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

type terminalManagerLoadedMsg struct {
	manager *terminalManager
	err     error
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
	bootstrap bool
}

type workspaceStateLoadedMsg struct {
	workspace Workspace
	store     map[string]Workspace
	order     []string
	active    int
	notice    string
	bootstrap bool
}

type terminalClosedMsg struct {
	terminalID string
}

type terminalEventMsg struct {
	event protocol.Event
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
	if cfg.PrefixTimeout <= 0 {
		cfg.PrefixTimeout = DefaultPrefixTimeout
	}
	cfg.IconSet = normalizeIconSetName(cfg.IconSet)
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Model{
		client: client,
		cfg:    cfg,
		logger: logger,
		icons:  resolveIconSet(cfg.IconSet),
		paneWriter: func(pane *Pane, data []byte) (int, error) {
			if pane == nil || pane.VTerm == nil {
				return 0, fmt.Errorf("pane runtime unavailable")
			}
			return pane.VTerm.Write(data)
		},
		workspace: Workspace{
			Name: cfg.Workspace,
			Tabs: []*Tab{newTab("1")},
		},
		renderInterval:          16 * time.Millisecond,
		renderFastInterval:      8 * time.Millisecond,
		renderInteractiveWindow: 200 * time.Millisecond,
		renderStatsInterval:     10 * time.Second,
		renderDirty:             true,
		width:                   80,
		height:                  24,
		prefixTimeout:           cfg.PrefixTimeout,
		timeNow:                 time.Now,
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

func (m *Model) applyHostTerminalColors(fg, bg color.Color) {
	nextFG := m.hostDefaultFG
	nextBG := m.hostDefaultBG
	if fg != nil {
		nextFG = colorToHex(fg)
	}
	if bg != nil {
		nextBG = colorToHex(bg)
	}
	if nextFG == m.hostDefaultFG && nextBG == m.hostDefaultBG {
		return
	}
	m.hostDefaultFG = nextFG
	m.hostDefaultBG = nextBG
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane == nil {
				continue
			}
			pane.DefaultFG = nextFG
			pane.DefaultBG = nextBG
			if pane.VTerm != nil {
				pane.VTerm.SetDefaultColors(nextFG, nextBG)
			}
			pane.renderDirty = true
			pane.cellVersion++
			pane.clearDirtyRegion()
		}
	}
	m.invalidateRender()
}

func (m *Model) applyHostTerminalPaletteColor(index int, c color.Color) {
	if index < 0 || index > 255 || c == nil {
		return
	}
	if m.hostPalette == nil {
		m.hostPalette = make(map[int]string)
	}
	value := colorToHex(c)
	if m.hostPalette[index] == value {
		return
	}
	m.hostPalette[index] = value
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane == nil || pane.VTerm == nil {
				continue
			}
			pane.VTerm.SetIndexedColor(index, value)
			pane.renderDirty = true
			pane.cellVersion++
			pane.clearDirtyRegion()
		}
	}
	m.invalidateRender()
}

func (m *Model) startTerminalEventForwarder() {
	if m == nil || m.client == nil || m.program == nil || m.eventsStarted {
		return
	}
	m.eventsStarted = true
	ctx, cancel := context.WithCancel(context.Background())
	m.eventsCancel = cancel
	go func() {
		events, err := m.client.Events(ctx, protocol.EventsParams{
			Types: []protocol.EventType{protocol.EventTerminalRemoved},
		})
		if err != nil {
			m.logger.Error("failed to subscribe terminal events", "error", m.wrapClientError("subscribe terminal events", err))
			return
		}
		for evt := range events {
			m.program.Send(terminalEventMsg{event: evt})
		}
	}()
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
	m.startTerminalEventForwarder()
	m.renderBatching = true
	m.startRenderTicker()
}

func (m *Model) StopRenderTicker() {
	if m.renderTickerStop != nil {
		close(m.renderTickerStop)
		m.renderTickerStop = nil
	}
	if m.eventsCancel != nil {
		m.eventsCancel()
		m.eventsCancel = nil
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
	interval := minPositiveDuration(m.renderFastInterval, m.renderInterval)
	if interval <= 0 {
		interval = m.renderInterval
	}
	statsInterval := m.renderStatsInterval
	program := m.program
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var statsTicker *time.Ticker
		if statsInterval > 0 {
			statsTicker = time.NewTicker(statsInterval)
			defer statsTicker.Stop()
		}
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if !m.renderPending.Load() {
					continue
				}
				program.Send(renderTickMsg{})
			case <-tickerChan(statsTicker):
				m.logRenderStats()
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
	now := m.now()
	interval := m.currentRenderInterval(now)
	if interval > 0 && !m.renderLastFlush.IsZero() && now.Sub(m.renderLastFlush) < interval {
		return
	}
	if !m.updateBackpressureState() {
		return
	}
	if !m.renderPending.Load() && !m.anyPaneDirty() {
		return
	}
	m.renderPending.Store(false)
	m.renderLastFlush = now
	m.invalidateRender()
}

func (m *Model) now() time.Time {
	if m != nil && m.timeNow != nil {
		return m.timeNow()
	}
	return time.Now()
}

func tickerChan(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}

func (m *Model) noteInteraction() {
	if m == nil || m.renderInteractiveWindow <= 0 {
		return
	}
	until := m.now().Add(m.renderInteractiveWindow)
	if until.After(m.renderInteractiveUntil) {
		m.renderInteractiveUntil = until
	}
}

func (m *Model) requestInteractiveRender() {
	if !m.renderBatching || m.program == nil {
		m.invalidateRender()
		return
	}
	now := m.now()
	interval := m.currentRenderInterval(now)
	if interval <= 0 || m.renderLastFlush.IsZero() || now.Sub(m.renderLastFlush) >= interval {
		m.invalidateRender()
		m.renderLastFlush = now
		return
	}
	m.renderPending.Store(true)
}

func (m *Model) logRenderStats() {
	if m == nil || m.logger == nil || m.renderStatsInterval <= 0 {
		return
	}
	viewCalls := m.renderViewCalls.Swap(0)
	frames := m.renderFrames.Swap(0)
	cacheHits := m.renderCacheHits.Swap(0)
	fps := 0.0
	if seconds := m.renderStatsInterval.Seconds(); seconds > 0 {
		fps = float64(frames) / seconds
	}
	m.logger.Info("tui render stats",
		"window", m.renderStatsInterval.String(),
		"view_calls", viewCalls,
		"frames", frames,
		"cache_hits", cacheHits,
		"fps", fmt.Sprintf("%.2f", fps),
	)
}

func (m *Model) currentRenderInterval(now time.Time) time.Duration {
	interval := m.renderInterval
	if interval <= 0 {
		interval = m.renderFastInterval
	}
	if !m.renderInteractiveUntil.IsZero() && now.Before(m.renderInteractiveUntil) && m.renderFastInterval > 0 && (interval <= 0 || m.renderFastInterval < interval) {
		return m.renderFastInterval
	}
	return interval
}

func minPositiveDuration(a, b time.Duration) time.Duration {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
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
	case tea.MouseMsg:
		switch {
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
			return m, m.handleInputEvent(uv.MouseClickEvent{X: msg.X, Y: msg.Y, Button: uv.MouseLeft})
		case msg.Action == tea.MouseActionMotion:
			return m, m.handleInputEvent(uv.MouseMotionEvent{X: msg.X, Y: msg.Y, Button: uv.MouseLeft})
		case msg.Action == tea.MouseActionRelease:
			return m, m.handleInputEvent(uv.MouseReleaseEvent{X: msg.X, Y: msg.Y, Button: uv.MouseLeft})
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case paneCreatedMsg:
		m.inputBlocked = false
		m.attachPane(msg)
		return m, tea.Batch(m.resizeVisiblePanesCmd(), m.advanceLayoutPromptAfterPaneMsg("", msg.pane))
	case paneReplacedMsg:
		m.inputBlocked = false
		m.replacePane(msg)
		return m, tea.Batch(m.resizeVisiblePanesCmd(), m.advanceLayoutPromptAfterPaneMsg(msg.paneID, msg.pane))
	case paneGroupReplacedMsg:
		m.inputBlocked = false
		for _, paneMsg := range msg.panes {
			m.replacePane(paneMsg)
		}
		return m, tea.Batch(m.resizeVisiblePanesCmd(), m.advanceLayoutPromptAfterPaneMsg(msg.promptPaneID, nil))
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
		if msg.hadTerminal {
			m.notice = "pane closed; terminal keeps running"
			m.err = nil
			m.invalidateRender()
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
	case terminalManagerLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.logger.Error("terminal manager load failed", "error", msg.err)
			m.invalidateRender()
			return m, nil
		}
		m.terminalManager = msg.manager
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
		if msg.bootstrap {
			return m, m.startEmptyWorkspaceBootstrapCmd()
		}
		return m, tea.Batch(m.resizeVisiblePanesCmd(), m.autoAcquireCurrentTabResizeCmd())
	case workspaceStateLoadedMsg:
		m.notice = msg.notice
		m.err = nil
		m.workspaceStore = msg.store
		m.workspaceOrder = msg.order
		m.activeWorkspace = msg.active
		m.replaceWorkspace(msg.workspace)
		m.invalidateRender()
		if msg.bootstrap {
			return m, m.startEmptyWorkspaceBootstrapCmd()
		}
		return m, tea.Batch(m.resizeVisiblePanesCmd(), m.autoAcquireCurrentTabResizeCmd())
	case terminalClosedMsg:
		saved := m.removeTerminal(msg.terminalID)
		if saved > 0 {
			suffix := "panes"
			if saved == 1 {
				suffix = "pane"
			}
			m.notice = fmt.Sprintf("stopped terminal %q; left %d saved %s", msg.terminalID, saved, suffix)
			m.err = nil
			m.invalidateRender()
		} else {
			m.markTerminalKilled(msg.terminalID)
		}
		if m.terminalManager != nil {
			return m, m.refreshTerminalManagerCmd()
		}
		return m, nil
	case terminalEventMsg:
		return m, m.handleTerminalEvent(msg.event)
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
			fallback := m.prefixFallback
			m.clearPrefixState()
			m.invalidateRender()
			return m, m.prefixFallbackCmd(fallback)
		}
		return m, nil
	case errMsg:
		m.inputBlocked = false
		m.notice = ""
		m.err = msg.err
		m.invalidateRender()
		return m, nil
	case terminalMetadataUpdatedMsg:
		m.inputBlocked = false
		m.err = nil
		m.applyTerminalMetadataUpdate(msg.TerminalID, msg.Name, msg.Tags)
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
		m.renderViewCalls.Add(1)
		m.renderFrames.Add(1)
		return "loading..."
	}
	m.renderViewCalls.Add(1)

	if !m.renderDirty && m.renderCache != "" && (m.workspacePicker != nil || m.terminalManager != nil || m.terminalPicker != nil || m.showHelp || m.prompt != nil) {
		m.renderCacheHits.Add(1)
		return m.renderCache
	}

	if m.renderBatching && !m.renderDirty && m.renderCache != "" {
		if m.program != nil || !m.anyPaneDirty() {
			m.renderCacheHits.Add(1)
			return m.renderCache
		}
	}

	var out string
	if m.workspacePicker != nil {
		out = m.renderWorkspacePicker()
		m.renderCache = out
		m.renderDirty = false
		m.renderLastFlush = m.now()
		m.renderFrames.Add(1)
		return out
	}

	if m.terminalManager != nil {
		out = m.renderTerminalManager()
		m.renderCache = out
		m.renderDirty = false
		m.renderLastFlush = m.now()
		m.renderFrames.Add(1)
		return out
	}

	if m.terminalPicker != nil {
		out = m.renderTerminalPicker()
		m.renderCache = out
		m.renderDirty = false
		m.renderLastFlush = m.now()
		m.renderFrames.Add(1)
		return out
	}

	if m.showHelp {
		out = m.renderHelpScreen()
		m.renderCache = out
		m.renderDirty = false
		m.renderLastFlush = m.now()
		m.renderFrames.Add(1)
		return out
	}

	if m.prompt != nil && m.prompt.Kind != "command" {
		out = m.renderPromptScreen()
		m.renderCache = out
		m.renderDirty = false
		m.renderLastFlush = m.now()
		m.renderFrames.Add(1)
		return out
	}

	out = strings.Join([]string{m.renderTabBar(), m.renderContentBody(), m.renderStatus()}, "\n")
	m.renderCache = out
	m.renderDirty = false
	m.renderLastFlush = m.now()
	m.renderFrames.Add(1)
	return out
}

func (m *Model) renderContentBody() string {
	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	tab := m.currentTab()
	if tab == nil || tab.Root == nil {
		return m.renderEmptyStateBody(contentHeight)
	}
	return m.renderTabComposite(tab, m.width, contentHeight)
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.workspacePicker != nil {
		return m, m.handleWorkspacePickerKey(msg)
	}

	if m.terminalManager != nil {
		return m, m.handleTerminalManagerKey(msg)
	}

	if m.terminalPicker != nil {
		return m, m.handleTerminalPickerKey(msg)
	}

	if m.prompt != nil {
		return m, m.handlePromptKey(msg)
	}

	if m.inputBlocked {
		return m, nil
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

	if m.prefixActive && m.directMode {
		if cmd := m.directModeCmdForKey(msg); cmd != nil {
			return m, cmd
		}
	}

	if m.prefixActive {
		return m, m.handleActivePrefixKey(msg)
	}

	if cmd := m.directModeCmdForKey(msg); cmd != nil {
		return m, cmd
	}

	if msg.Type == tea.KeyCtrlA {
		return m, m.sendToActive([]byte{0x01})
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

func (m *Model) directModeCmdForKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyCtrlP:
		return m.enterDirectMode(prefixModePane)
	case tea.KeyCtrlR:
		return m.enterDirectMode(prefixModeResize)
	case tea.KeyCtrlT:
		return m.enterDirectMode(prefixModeTab)
	case tea.KeyCtrlW:
		return m.enterDirectMode(prefixModeWorkspace)
	case tea.KeyCtrlO:
		return m.enterDirectMode(prefixModeFloating)
	case tea.KeyCtrlV:
		return m.enterDirectMode(prefixModeViewport)
	case tea.KeyCtrlG:
		return m.enterDirectMode(prefixModeGlobal)
	case tea.KeyCtrlF:
		return m.openTerminalPickerCmd()
	default:
		return nil
	}
}

func (m *Model) clearPrefixState() {
	m.prefixActive = false
	m.directMode = false
	m.prefixMode = prefixModeRoot
	m.prefixFallback = prefixFallbackNone
}

func (m *Model) prefixFallbackCmd(fallback prefixFallback) tea.Cmd {
	switch fallback {
	case prefixFallbackFloatingCreate:
		return m.openFloatingTerminalPickerCmd(m.workspace.ActiveTab)
	default:
		return nil
	}
}

func (m *Model) enterPrefixMode(mode prefixMode, fallback prefixFallback) tea.Cmd {
	m.prefixActive = true
	m.directMode = false
	m.prefixMode = mode
	m.prefixFallback = fallback
	if mode == prefixModeFloating {
		if tab := m.currentTab(); tab != nil {
			floating := m.visibleFloatingPanes(tab)
			if len(floating) > 0 && !isFloatingPane(tab, tab.ActivePaneID) {
				tab.ActivePaneID = floating[len(floating)-1].PaneID
			}
		}
	}
	m.invalidateRender()
	return m.armPrefixTimeout()
}

func isStickyPrefixMode(mode prefixMode) bool {
	return mode == prefixModeFloating || mode == prefixModeOffsetPan
}

func (m *Model) enterDirectMode(mode prefixMode) tea.Cmd {
	m.prefixActive = true
	m.directMode = true
	m.prefixMode = mode
	m.prefixFallback = prefixFallbackNone
	m.prefixSeq++
	m.invalidateRender()
	return m.armPrefixTimeout()
}

func (m *Model) armPrefixTimeout() tea.Cmd {
	if !m.prefixActive || m.prefixTimeout <= 0 {
		return nil
	}
	m.prefixSeq++
	seq := m.prefixSeq
	timeout := m.prefixTimeout
	return tea.Tick(timeout, func(time.Time) tea.Msg {
		return prefixTimeoutMsg{seq: seq}
	})
}

func (m *Model) handleActivePrefixKey(msg tea.KeyMsg) tea.Cmd {
	result := m.dispatchPrefixKey(msg)
	if !result.keep {
		m.clearPrefixState()
		m.invalidateRender()
		return result.cmd
	}
	m.invalidateRender()
	if result.rearm {
		return tea.Batch(result.cmd, m.armPrefixTimeout())
	}
	return result.cmd
}

func (m *Model) dispatchPrefixKey(msg tea.KeyMsg) prefixDispatchResult {
	switch m.prefixMode {
	case prefixModePane:
		return m.dispatchPaneModeKey(msg)
	case prefixModeResize:
		return m.dispatchResizeModeKey(msg)
	case prefixModeTab:
		return m.dispatchTabSubPrefixKey(msg)
	case prefixModeWorkspace:
		return m.dispatchWorkspaceSubPrefixKey(msg)
	case prefixModeViewport:
		return m.dispatchViewportSubPrefixKey(msg)
	case prefixModeFloating:
		return m.dispatchFloatingModeKey(msg)
	case prefixModeOffsetPan:
		return m.dispatchOffsetPanModeKey(msg)
	case prefixModeGlobal:
		return m.dispatchGlobalModeKey(msg)
	default:
		return m.dispatchRootPrefixKey(msg)
	}
}

func (m *Model) dispatchRootPrefixKey(msg tea.KeyMsg) prefixDispatchResult {
	switch msg.String() {
	case "t":
		return prefixDispatchResult{cmd: m.enterPrefixMode(prefixModeTab, prefixFallbackNone), keep: true}
	case "v":
		return prefixDispatchResult{cmd: m.enterPrefixMode(prefixModeViewport, prefixFallbackNone), keep: true}
	case "o":
		return prefixDispatchResult{cmd: m.enterPrefixMode(prefixModeFloating, prefixFallbackNone), keep: true}
	case "w":
		return prefixDispatchResult{cmd: m.enterPrefixMode(prefixModeWorkspace, prefixFallbackFloatingCreate), keep: true}
	}
	cmd := m.handlePrefixKey(msg)
	keep := shouldKeepPrefixKey(msg)
	return prefixDispatchResult{cmd: cmd, keep: keep, rearm: keep}
}

func shouldKeepPrefixKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyLeft, tea.KeyRight, tea.KeyUp, tea.KeyDown,
		tea.KeyCtrlLeft, tea.KeyCtrlRight, tea.KeyCtrlUp, tea.KeyCtrlDown,
		tea.KeyCtrlH, tea.KeyCtrlJ, tea.KeyCtrlK, tea.KeyCtrlL:
		return true
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return false
		}
		switch msg.Runes[0] {
		case 'h', 'j', 'k', 'l', 'H', 'J', 'K', 'L':
			return true
		}
	}
	return false
}

func (m *Model) modeResult(cmd tea.Cmd, keep bool) prefixDispatchResult {
	return prefixDispatchResult{cmd: cmd, keep: keep, rearm: keep}
}

func (m *Model) dispatchPaneModeKey(msg tea.KeyMsg) prefixDispatchResult {
	if msg.Type == tea.KeyEsc {
		return prefixDispatchResult{}
	}
	cmd := m.handlePrefixKey(msg)
	if cmd == nil && !shouldKeepPrefixKey(msg) {
		if m.directMode {
			return prefixDispatchResult{keep: true}
		}
		if msg.Type != tea.KeyRunes && msg.Type != tea.KeyTab {
			return m.modeResult(nil, false)
		}
		return m.modeResult(nil, false)
	}
	return m.modeResult(cmd, shouldKeepPrefixKey(msg))
}

func (m *Model) dispatchResizeModeKey(msg tea.KeyMsg) prefixDispatchResult {
	if msg.Type == tea.KeyEsc {
		return prefixDispatchResult{}
	}
	switch msg.Type {
	case tea.KeyLeft:
		m.resizeActivePane(DirectionLeft, 2)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case tea.KeyDown:
		m.resizeActivePane(DirectionDown, 2)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case tea.KeyUp:
		m.resizeActivePane(DirectionUp, 2)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case tea.KeyRight:
		m.resizeActivePane(DirectionRight, 2)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	}
	switch msg.String() {
	case "h":
		m.resizeActivePane(DirectionLeft, 2)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case "j":
		m.resizeActivePane(DirectionDown, 2)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case "k":
		m.resizeActivePane(DirectionUp, 2)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case "l":
		m.resizeActivePane(DirectionRight, 2)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case "H":
		m.resizeActivePane(DirectionLeft, 4)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case "J":
		m.resizeActivePane(DirectionDown, 4)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case "K":
		m.resizeActivePane(DirectionUp, 4)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case "L":
		m.resizeActivePane(DirectionRight, 4)
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case "a":
		return prefixDispatchResult{cmd: m.acquireActivePaneResizeCmd(), keep: true, rearm: true}
	case "=":
		if tab := m.currentTab(); tab != nil && tab.Root != nil {
			resetLayoutRatios(tab.Root)
		}
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	case " ":
		m.cycleActiveLayout()
		return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true, rearm: true}
	default:
		return prefixDispatchResult{keep: true}
	}
}

func (m *Model) dispatchTabSubPrefixKey(msg tea.KeyMsg) prefixDispatchResult {
	if m.directMode && msg.Type == tea.KeyEsc {
		return prefixDispatchResult{}
	}
	switch msg.String() {
	case "c":
		return m.modeResult(m.openNewTabTerminalPickerCmd(), false)
	case ",":
		if m.directMode {
			m.beginRenameTab()
			return prefixDispatchResult{}
		}
		m.beginRenameTab()
		return prefixDispatchResult{}
	case "r":
		m.beginRenameTab()
		return m.modeResult(nil, false)
	case "n":
		if len(m.workspace.Tabs) > 0 {
			next := (m.workspace.ActiveTab + 1) % len(m.workspace.Tabs)
			return m.modeResult(m.activateTab(next), false)
		}
		return m.modeResult(nil, false)
	case "p":
		if len(m.workspace.Tabs) > 0 {
			next := (m.workspace.ActiveTab - 1 + len(m.workspace.Tabs)) % len(m.workspace.Tabs)
			return m.modeResult(m.activateTab(next), false)
		}
		return m.modeResult(nil, false)
	case "f":
		return m.modeResult(m.openTerminalPickerCmd(), false)
	case "x":
		return m.modeResult(m.killActiveTabCmd(), false)
	default:
		if m.directMode && msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			r := msg.Runes[0]
			if r >= '1' && r <= '9' {
				if int(r-'1') < len(m.workspace.Tabs) {
					return m.modeResult(m.activateTab(int(r-'1')), false)
				}
				return m.modeResult(nil, false)
			}
		}
		if m.directMode {
			return prefixDispatchResult{keep: true}
		}
		return prefixDispatchResult{}
	}
}

func (m *Model) dispatchWorkspaceSubPrefixKey(msg tea.KeyMsg) prefixDispatchResult {
	if m.directMode && msg.Type == tea.KeyEsc {
		return prefixDispatchResult{}
	}
	switch msg.String() {
	case "s":
		return m.modeResult(m.openWorkspacePickerCmd(), false)
	case "c":
		return m.modeResult(m.createWorkspaceCmd(nextWorkspaceName(m.workspaceOrder)), false)
	case "r":
		m.beginRenameWorkspace()
		return m.modeResult(nil, false)
	case "x":
		return m.modeResult(m.deleteCurrentWorkspaceCmd(), false)
	case "n":
		return m.modeResult(m.activateWorkspaceByOffset(1), false)
	case "p":
		return m.modeResult(m.activateWorkspaceByOffset(-1), false)
	case "f":
		return m.modeResult(m.openWorkspacePickerCmd(), false)
	default:
		if m.directMode {
			return prefixDispatchResult{keep: true}
		}
		return prefixDispatchResult{}
	}
}

func (m *Model) dispatchViewportSubPrefixKey(msg tea.KeyMsg) prefixDispatchResult {
	if m.directMode && msg.Type == tea.KeyEsc {
		return prefixDispatchResult{}
	}
	switch msg.Type {
	case tea.KeyLeft:
		m.panActiveViewport(-4, 0)
		return m.modeResult(nil, true)
	case tea.KeyRight:
		m.panActiveViewport(4, 0)
		return m.modeResult(nil, true)
	case tea.KeyUp:
		m.panActiveViewport(0, -2)
		return m.modeResult(nil, true)
	case tea.KeyDown:
		m.panActiveViewport(0, 2)
		return m.modeResult(nil, true)
	}
	switch msg.String() {
	case "m":
		m.toggleActiveViewportMode()
		return m.modeResult(m.resizeVisiblePanesCmd(), false)
	case "r":
		m.toggleActiveViewportReadonly()
		return m.modeResult(nil, false)
	case "p":
		m.toggleActiveViewportPin()
		return m.modeResult(nil, false)
	case "h":
		m.panActiveViewport(-4, 0)
		return m.modeResult(nil, true)
	case "j":
		m.panActiveViewport(0, 2)
		return m.modeResult(nil, true)
	case "k":
		m.panActiveViewport(0, -2)
		return m.modeResult(nil, true)
	case "l":
		m.panActiveViewport(4, 0)
		return m.modeResult(nil, true)
	case "0":
		m.setActiveViewportOffset(0, 0)
		return m.modeResult(nil, false)
	case "$":
		m.setActiveViewportOffset(int(^uint(0)>>1), 0)
		return m.modeResult(nil, false)
	case "g":
		m.setActiveViewportOffset(0, 0)
		return m.modeResult(nil, false)
	case "G":
		m.setActiveViewportOffset(0, int(^uint(0)>>1))
		return m.modeResult(nil, false)
	case "z":
		pane := activePane(m.currentTab())
		if pane != nil {
			pane.Offset = Point{}
			pane.Pin = false
			if pane.Mode != ViewportModeFit {
				pane.Mode = ViewportModeFit
			}
		}
		return m.modeResult(m.resizeVisiblePanesCmd(), false)
	case "o":
		if m.directMode {
			return prefixDispatchResult{keep: true}
		}
		return prefixDispatchResult{cmd: m.enterPrefixMode(prefixModeOffsetPan, prefixFallbackNone), keep: true}
	default:
		if m.directMode {
			return prefixDispatchResult{keep: true}
		}
		return prefixDispatchResult{}
	}
}

func (m *Model) dispatchFloatingModeKey(msg tea.KeyMsg) prefixDispatchResult {
	switch msg.Type {
	case tea.KeyEsc:
		_ = m.focusTiledPane()
		return prefixDispatchResult{}
	case tea.KeyTab:
		m.cycleFloatingFocus()
		return m.modeResult(nil, true)
	}
	switch msg.String() {
	case "n":
		return m.modeResult(m.openFloatingTerminalPickerCmd(m.workspace.ActiveTab), false)
	case "x":
		return m.modeResult(m.closeActivePaneCmd(), true)
	case "v":
		m.toggleFloatingLayerVisibility()
		return m.modeResult(nil, true)
	case "]":
		m.raiseActiveFloatingPane()
		return m.modeResult(nil, true)
	case "[":
		m.lowerActiveFloatingPane()
		return m.modeResult(nil, true)
	case "c":
		m.centerActiveFloatingPane()
		return m.modeResult(nil, true)
	case "h":
		m.moveActiveFloatingPane(-4, 0)
		return m.modeResult(nil, true)
	case "j":
		m.moveActiveFloatingPane(0, 2)
		return m.modeResult(nil, true)
	case "k":
		m.moveActiveFloatingPane(0, -2)
		return m.modeResult(nil, true)
	case "l":
		m.moveActiveFloatingPane(4, 0)
		return m.modeResult(nil, true)
	case "H":
		if m.resizeActiveFloatingPane(-4, 0) {
			return m.modeResult(m.resizeVisiblePanesCmd(), true)
		}
		return m.modeResult(nil, true)
	case "J":
		if m.resizeActiveFloatingPane(0, 2) {
			return m.modeResult(m.resizeVisiblePanesCmd(), true)
		}
		return m.modeResult(nil, true)
	case "K":
		if m.resizeActiveFloatingPane(0, -2) {
			return m.modeResult(m.resizeVisiblePanesCmd(), true)
		}
		return m.modeResult(nil, true)
	case "L":
		if m.resizeActiveFloatingPane(4, 0) {
			return m.modeResult(m.resizeVisiblePanesCmd(), true)
		}
		return m.modeResult(nil, true)
	case "f":
		return m.modeResult(m.openTerminalPickerCmd(), false)
	default:
		if m.directMode {
			return prefixDispatchResult{keep: true}
		}
		return prefixDispatchResult{}
	}
}

func (m *Model) dispatchOffsetPanModeKey(msg tea.KeyMsg) prefixDispatchResult {
	switch msg.Type {
	case tea.KeyEsc:
		return prefixDispatchResult{}
	case tea.KeyLeft:
		m.panActiveViewport(-4, 0)
		return prefixDispatchResult{keep: true, rearm: true}
	case tea.KeyRight:
		m.panActiveViewport(4, 0)
		return prefixDispatchResult{keep: true, rearm: true}
	case tea.KeyUp:
		m.panActiveViewport(0, -2)
		return prefixDispatchResult{keep: true, rearm: true}
	case tea.KeyDown:
		m.panActiveViewport(0, 2)
		return prefixDispatchResult{keep: true, rearm: true}
	}
	switch msg.String() {
	case "h":
		m.panActiveViewport(-4, 0)
		return prefixDispatchResult{keep: true, rearm: true}
	case "j":
		m.panActiveViewport(0, 2)
		return prefixDispatchResult{keep: true, rearm: true}
	case "k":
		m.panActiveViewport(0, -2)
		return prefixDispatchResult{keep: true, rearm: true}
	case "l":
		m.panActiveViewport(4, 0)
		return prefixDispatchResult{keep: true, rearm: true}
	case "0":
		m.setActiveViewportOffset(0, 0)
		return prefixDispatchResult{keep: true, rearm: true}
	case "$":
		m.setActiveViewportOffset(int(^uint(0)>>1), 0)
		return prefixDispatchResult{keep: true, rearm: true}
	case "g":
		m.setActiveViewportOffset(0, 0)
		return prefixDispatchResult{keep: true, rearm: true}
	case "G":
		m.setActiveViewportOffset(0, int(^uint(0)>>1))
		return prefixDispatchResult{keep: true, rearm: true}
	default:
		return prefixDispatchResult{}
	}
}

func (m *Model) dispatchGlobalModeKey(msg tea.KeyMsg) prefixDispatchResult {
	if msg.Type == tea.KeyEsc {
		return prefixDispatchResult{}
	}
	switch msg.String() {
	case "?":
		m.showHelp = true
		m.invalidateRender()
		return prefixDispatchResult{}
	case "t":
		return prefixDispatchResult{cmd: m.openTerminalManagerCmd()}
	case ":":
		m.beginCommandPrompt()
		return prefixDispatchResult{}
	case "d":
		m.quitting = true
		m.invalidateRender()
		return prefixDispatchResult{cmd: tea.Quit}
	case "q":
		m.quitting = true
		m.invalidateRender()
		return prefixDispatchResult{cmd: tea.Quit}
	default:
		return prefixDispatchResult{keep: true}
	}
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
			next := (m.workspace.ActiveTab + 1) % len(m.workspace.Tabs)
			return m.activateTab(next)
		}
		return nil
	case "p":
		if len(m.workspace.Tabs) > 0 {
			next := (m.workspace.ActiveTab - 1 + len(m.workspace.Tabs)) % len(m.workspace.Tabs)
			return m.activateTab(next)
		}
		return nil
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
				return m.activateTab(idx)
			}
			return nil
		}
	}
	return nil
}

func (m *Model) handleRawInput(data []byte) tea.Cmd {
	if len(data) == 0 {
		return nil
	}
	if m.inputBlocked {
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

		if m.terminalManager != nil {
			consumed, cmd, ok := m.consumeTerminalManagerInput()
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

		payload, keep := rewriteInputForActivePane(m.currentTab(), m.rawPending)
		if cmd := m.sendToActive(payload); cmd != nil {
			ordered = append(ordered, cmd)
		}
		m.rawPending = keep
		break
	}

	return combineCmdsOrdered(ordered, background)
}

func (m *Model) consumeExitedPaneInput() (int, tea.Cmd, bool) {
	if len(m.rawPending) == 0 {
		return 0, nil, false
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
		m.cancelPrompt()
	case tea.KeyEnter:
		return m.commitPrompt()
	case tea.KeyBackspace:
		m.deletePromptRune()
	case tea.KeySpace:
		if m.promptAcceptsText() {
			m.appendPrompt(" ")
		}
	case tea.KeyRunes:
		if len(msg.Runes) > 0 && m.promptAcceptsText() {
			m.appendPrompt(string(msg.Runes))
		}
	}
	return nil
}

func (m *Model) consumePromptInput() (int, tea.Cmd, bool) {
	if len(m.rawPending) == 0 {
		return 0, nil, false
	}
	if !m.promptAcceptsText() {
		switch m.rawPending[0] {
		case '\r', '\n':
			return 1, m.commitPrompt(), true
		case 0x1b:
			if n, ok := consumeArrowSequence(m.rawPending); ok {
				return n, nil, true
			}
			if len(m.rawPending) == 1 {
				m.cancelPrompt()
				return 1, nil, true
			}
			return 0, nil, false
		default:
			return 1, nil, true
		}
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
			m.cancelPrompt()
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

func (m *Model) cancelPrompt() {
	if m.prompt == nil {
		return
	}
	if strings.HasPrefix(m.prompt.Kind, "create-terminal") {
		m.pendingTerminalCreate = nil
	}
	if strings.HasPrefix(m.prompt.Kind, "edit-terminal") {
		m.pendingTerminalEdit = nil
	}
	if strings.HasPrefix(m.prompt.Kind, "confirm-acquire-resize") {
		m.pendingResizeAcquire = nil
	}
	if strings.HasPrefix(m.prompt.Kind, "confirm-stop-terminal") {
		m.pendingTerminalStop = nil
	}
	m.prompt = nil
	m.invalidateRender()
}

func (m *Model) consumePrefixInput() (int, tea.Cmd, bool) {
	if len(m.rawPending) == 0 {
		return 0, nil, false
	}

	if n, key, ok, incomplete := parseCtrlArrowPrefix(m.rawPending); incomplete {
		return 0, nil, false
	} else if ok {
		return n, m.handleActivePrefixKey(key), true
	}

	if n, dir, ok, incomplete := parseArrowPrefix(m.rawPending); incomplete {
		return 0, nil, false
	} else if ok {
		return n, m.handleActivePrefixKey(prefixDirectionKey(dir)), true
	}

	b := m.rawPending[0]
	switch b {
	case 0x01:
		return 1, m.handleActivePrefixKey(tea.KeyMsg{Type: tea.KeyCtrlA}), true
	case 0x08, 0x0a, 0x0b, 0x0c:
		return 1, m.handleActivePrefixKey(prefixCtrlKey(b)), true
	case '\t':
		return 1, m.handleActivePrefixKey(prefixTabKey()), true
	case 0x1b:
		if len(m.rawPending) == 1 {
			return 1, m.handleActivePrefixKey(tea.KeyMsg{Type: tea.KeyEsc}), true
		}
		return 0, nil, false
	case '"', '%', ',', ':', 'W', ']', '_', '[', 'f', 'h', 'j', 'k', 'l', 'w', 'v', 't', 'o', 'r', 'p', 'm', 'H', 'J', 'K', 'L', 'M', 'P', 'R', 'X', 'c', 'n', 'N', 's', 'x', 'g', 'G', '0', '$', 'a', 'd', '?', '&', ' ', 'z', '{', '}':
		return 1, m.handleActivePrefixKey(prefixRuneKey(rune(b))), true
	default:
		if b >= '1' && b <= '9' {
			return 1, m.handleActivePrefixKey(prefixRuneKey(rune(b))), true
		}
		m.clearPrefixState()
		m.invalidateRender()
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

type terminalCreateSpec struct {
	Command []string
	Name    string
	Tags    map[string]string
}

func (m *Model) createPaneCmd(tabIndex int, targetID string, split SplitDirection) tea.Cmd {
	return m.createPaneCmdWithSpec(tabIndex, targetID, split, terminalCreateSpec{})
}

func (m *Model) createPaneCmdWithSpec(tabIndex int, targetID string, split SplitDirection, spec terminalCreateSpec) tea.Cmd {
	command, name, tags := m.resolveTerminalCreateSpec(spec)
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		m.logger.Debug("creating pane terminal", "tab_index", tabIndex, "target_id", targetID, "split", split)
		size := protocol.Size{Cols: 80, Rows: 24}
		created, err := m.client.Create(ctx, command, name, size)
		if err != nil {
			return errMsg{m.wrapClientError("create terminal", err)}
		}
		if len(tags) > 0 {
			if err := m.client.SetTags(ctx, created.TerminalID, tags); err != nil {
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
		paneID := m.nextPaneID()
		m.logger.Info("created pane terminal", "pane_id", paneID, "terminal_id", created.TerminalID, "tab_index", tabIndex)
		return paneCreatedMsg{
			tabIndex: tabIndex,
			targetID: targetID,
			split:    split,
			pane: &Pane{
				ID:    paneID,
				Title: paneTitleForCommand(name, firstCommandWord(command), created.TerminalID),
				Viewport: func() *Viewport {
					view := m.newViewport(created.TerminalID, attached.Channel, snap)
					view.AttachMode = attached.Mode
					view.Name = name
					view.Command = append([]string(nil), command...)
					view.Tags = cloneStringMap(tags)
					return view
				}(),
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
		view.AttachMode = attached.Mode
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
	return m.createFloatingPaneCmdWithSpec(tabIndex, terminalCreateSpec{})
}

func (m *Model) createFloatingPaneCmdWithSpec(tabIndex int, spec terminalCreateSpec) tea.Cmd {
	command, name, tags := m.resolveTerminalCreateSpec(spec)
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		m.logger.Debug("creating floating terminal", "tab_index", tabIndex)
		size := protocol.Size{Cols: 80, Rows: 24}
		created, err := m.client.Create(ctx, command, name, size)
		if err != nil {
			return errMsg{m.wrapClientError("create terminal", err)}
		}
		if len(tags) > 0 {
			if err := m.client.SetTags(ctx, created.TerminalID, tags); err != nil {
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
		paneID := m.nextPaneID()
		view := m.newViewport(created.TerminalID, attached.Channel, snap)
		view.AttachMode = attached.Mode
		view.Name = name
		view.Command = append([]string(nil), command...)
		view.Tags = cloneStringMap(tags)
		view.Mode = ViewportModeFixed
		m.logger.Info("created floating terminal", "pane_id", paneID, "terminal_id", created.TerminalID, "tab_index", tabIndex)
		return paneCreatedMsg{
			tabIndex: tabIndex,
			floating: true,
			pane: &Pane{
				ID:       paneID,
				Title:    paneTitleForCommand(name, firstCommandWord(command), created.TerminalID),
				Viewport: view,
			},
		}
	}
}

func (m *Model) resolveTerminalCreateSpec(spec terminalCreateSpec) ([]string, string, map[string]string) {
	command := append([]string(nil), spec.Command...)
	if len(command) == 0 {
		command = []string{m.cfg.DefaultShell}
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		name = m.nextTerminalName(command)
	}
	return command, name, cloneStringMap(spec.Tags)
}

func (m *Model) attachPane(msg paneCreatedMsg) {
	if msg.tabIndex >= len(m.workspace.Tabs) {
		return
	}
	tab := m.workspace.Tabs[msg.tabIndex]
	if msg.floating {
		rect := m.defaultFloatingRectForPane(msg.pane)
		rect = m.staggerFloatingRect(rect, len(tab.Floating))
		tab.Panes[msg.pane.ID] = msg.pane
		tab.Floating = append(tab.Floating, &FloatingPane{
			PaneID: msg.pane.ID,
			Rect:   rect,
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
		if !m.ensurePaneRuntime(pane) {
			return nil
		}
		beforeCursor := pane.VTerm.CursorState()
		beforeAlt := pane.VTerm.IsAltScreen()
		termCols, termRows := pane.VTerm.Size()
		n, err := m.paneWriter(pane, msg.frame.Payload)
		if err != nil {
			m.logger.Warn("pane write failed; recovering from snapshot",
				"pane_id", pane.ID,
				"terminal_id", pane.TerminalID,
				"channel", pane.Channel,
				"bytes", len(msg.frame.Payload),
				"written", n,
				"error", err,
			)
			pane.syncLost = true
			if dropped := len(msg.frame.Payload) - max(0, n); dropped > 0 {
				pane.droppedBytes += uint64(dropped)
			}
			pane.renderDirty = true
			m.scheduleRender()
			if pane.recovering {
				return nil
			}
			pane.recovering = true
			return m.recoverPaneSnapshotCmd(pane.ID, pane.TerminalID, pane.droppedBytes)
		}
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

func (m *Model) handleTerminalEvent(evt protocol.Event) tea.Cmd {
	if evt.Type != protocol.EventTerminalRemoved || evt.TerminalID == "" {
		return nil
	}
	saved := m.removeTerminal(evt.TerminalID)
	if saved > 0 {
		suffix := "panes"
		if saved == 1 {
			suffix = "pane"
		}
		m.notice = fmt.Sprintf("terminal %q was removed by another client; left %d saved %s", evt.TerminalID, saved, suffix)
		m.err = nil
		m.invalidateRender()
	}
	if m.terminalManager != nil {
		return m.refreshTerminalManagerCmd()
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
	if !m.ensurePaneRuntime(pane) {
		return
	}
	pane.Snapshot = msg.snapshot
	loadSnapshotIntoVTerm(pane.VTerm, msg.snapshot)
	pane.live = true
	pane.TerminalState = "running"
	pane.syncLost = false
	pane.recovering = false
	pane.droppedBytes = msg.droppedBytes
	pane.cellVersion++
	pane.renderDirty = true
	pane.clearDirtyRegion()
	pane.cellCache = nil
	if msg.snapshot.Size.Cols > 0 && msg.snapshot.Size.Rows > 0 {
		m.resizeTerminalPanes(pane.TerminalID, pane.ID, msg.snapshot.Size.Cols, msg.snapshot.Size.Rows)
	}
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
				view.AttachMode = attached.Mode
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
			createdByHint := make(map[string]string)
			for _, plan := range plans {
				pane := findPane(workspace.Tabs, plan.PaneID)
				if pane == nil {
					return errMsg{fmt.Errorf("missing pane for create plan %q", plan.PaneID)}
				}
				command := commandStringToSlice(plan.Terminal.Command)
				if len(command) == 0 {
					command = []string{m.cfg.DefaultShell}
				}
				terminalID := ""
				hintID := strings.TrimSpace(plan.Terminal.HintID)
				if hintID != "" {
					terminalID = createdByHint[hintID]
				}
				if terminalID == "" {
					created, err := m.client.Create(ctx, command, "", protocol.Size{Cols: 80, Rows: 24})
					if err != nil {
						return errMsg{m.wrapClientError("create terminal", err)}
					}
					terminalID = created.TerminalID
					if hintID != "" {
						createdByHint[hintID] = terminalID
					}
					if len(pane.Tags) > 0 {
						if err := m.client.SetTags(ctx, terminalID, pane.Tags); err != nil {
							return errMsg{m.wrapClientError("set terminal tags", err, "terminal_id", terminalID)}
						}
					}
				}
				attached, err := m.client.Attach(ctx, terminalID, "collaborator")
				if err != nil {
					return errMsg{m.wrapClientError("attach terminal", err, "terminal_id", terminalID)}
				}
				snap, err := m.client.Snapshot(ctx, terminalID, 0, 200)
				if err != nil {
					return errMsg{m.wrapClientError("snapshot terminal", err, "terminal_id", terminalID)}
				}
				view := m.newViewport(terminalID, attached.Channel, snap)
				view.AttachMode = attached.Mode
				view.Name = pane.Name
				view.Command = command
				view.Tags = cloneStringMap(pane.Tags)
				view.TerminalState = "running"
				view.Mode = pane.Mode
				view.Offset = pane.Offset
				view.Pin = pane.Pin
				view.Readonly = pane.Readonly
				pane.TerminalID = terminalID
				pane.Title = paneTitleForCommand("", firstCommandWord(command), terminalID)
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
	paneIDs := []string{plan.PaneID}
	if hintID := strings.TrimSpace(plan.Terminal.HintID); hintID != "" {
		remaining := m.layoutPromptQueue[:0]
		for _, queued := range m.layoutPromptQueue {
			if strings.TrimSpace(queued.Terminal.HintID) == hintID {
				paneIDs = append(paneIDs, queued.PaneID)
				continue
			}
			remaining = append(remaining, queued)
		}
		m.layoutPromptQueue = remaining
	}
	m.layoutPromptCurrent = &plan
	m.focusPaneByID(plan.PaneID)
	return m.openLayoutResolvePickerCmd(plan, paneIDs)
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
	case "edit-terminal":
		pane := activePane(m.currentTab())
		if pane == nil || strings.TrimSpace(pane.TerminalID) == "" {
			m.notice = ""
			m.err = fmt.Errorf("edit-terminal requires an active terminal")
			m.invalidateRender()
			return nil
		}
		m.beginTerminalEditPrompt(protocol.TerminalInfo{
			ID:      pane.TerminalID,
			Name:    pane.Name,
			Command: append([]string(nil), pane.Command...),
			Tags:    cloneStringMap(pane.Tags),
		})
		return nil
	case "terminals":
		return m.openTerminalManagerCmd()
	case "acquire-resize":
		return m.acquireActivePaneResizeCmd()
	case "tab-auto-acquire":
		if len(fields) < 2 {
			m.notice = ""
			m.err = fmt.Errorf("tab-auto-acquire requires on or off")
			m.invalidateRender()
			return nil
		}
		return m.setCurrentTabAutoAcquireCmd(fields[1])
	case "set-size-lock":
		if len(fields) < 2 {
			m.notice = ""
			m.err = fmt.Errorf("set-size-lock requires off or warn")
			m.invalidateRender()
			return nil
		}
		return m.setActiveTerminalSizeLockCmd(fields[1])
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
					msg := cmd()
					if failed, ok := msg.(errMsg); ok {
						m.logger.Warn("tui startup ignoring workspace state restore failure", "path", path, "error", failed.err)
					} else if loaded, ok := msg.(workspaceStateLoadedMsg); ok {
						if !workspaceHasPanes(&loaded.workspace) {
							m.logger.Info("tui startup restored empty workspace state; bootstrapping chooser", "path", path, "workspace", loaded.workspace.Name)
							loaded.bootstrap = true
							loaded.notice = "restored empty workspace state"
							return loaded
						}
						return msg
					}
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

func (m *Model) startEmptyWorkspaceBootstrapCmd() tea.Cmd {
	m.logger.Info("tui empty workspace bootstrapping chooser", "workspace", m.workspace.Name)
	return m.openBootstrapTerminalPickerCmd(m.workspace.ActiveTab)
}

func (m *Model) saveLayoutCmd(name string) tea.Cmd {
	return func() tea.Msg {
		spec, err := ExportLayoutSpec(name, &m.workspace)
		if err != nil {
			return errMsg{err}
		}
		m.applyFloatingPositionsToLayoutSpec(spec)
		data, err := yaml.Marshal(spec)
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

func (m *Model) applyFloatingPositionsToLayoutSpec(spec *LayoutSpec) {
	if spec == nil {
		return
	}
	limit := min(len(spec.Tabs), len(m.workspace.Tabs))
	for i := 0; i < limit; i++ {
		tabSpec := &spec.Tabs[i]
		tab := m.workspace.Tabs[i]
		if tab == nil {
			continue
		}
		for j := range tabSpec.Floating {
			if j >= len(tab.Floating) || tab.Floating[j] == nil {
				continue
			}
			tabSpec.Floating[j].Position = m.floatingRectPositionAnchor(tab.Floating[j].Rect)
		}
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
	m.resizeTerminalPanes(pane.TerminalID, "", msg.cols, msg.rows)
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
	if tab == nil {
		return nil
	}
	rootRect := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	if paneID, rect, ok := m.zoomedPaneRect(tab, rootRect); ok {
		return map[string]Rect{paneID: rect}
	}
	if tab.Root == nil {
		rects := make(map[string]Rect)
		for _, floating := range m.visibleFloatingPanes(tab) {
			rects[floating.PaneID] = floating.Rect
		}
		if len(rects) == 0 {
			return nil
		}
		return rects
	}
	rects := tab.Root.Rects(rootRect)
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
	rect.W = clampFloatingDimension(rect.W, bounds.W, 8)
	rect.H = clampFloatingDimension(rect.H, bounds.H, 4)
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

func clampFloatingRectLoose(rect, bounds Rect) Rect {
	if bounds.W <= 0 || bounds.H <= 0 {
		return Rect{W: max(1, rect.W), H: max(1, rect.H)}
	}
	rect.W = clampFloatingDimension(rect.W, bounds.W, 8)
	rect.H = clampFloatingDimension(rect.H, bounds.H, 4)

	minVisibleX := min(4, rect.W)
	minVisibleY := min(2, rect.H)
	minX := bounds.X - rect.W + minVisibleX
	maxX := bounds.X + bounds.W - minVisibleX
	minY := bounds.Y - rect.H + minVisibleY
	maxY := bounds.Y + bounds.H - minVisibleY

	if rect.X < minX {
		rect.X = minX
	}
	if rect.X > maxX {
		rect.X = maxX
	}
	if rect.Y < minY {
		rect.Y = minY
	}
	if rect.Y > maxY {
		rect.Y = maxY
	}
	return rect
}

func clampFloatingDimension(value, bound, minSize int) int {
	if bound <= 0 {
		return max(1, value)
	}
	minAllowed := min(minSize, bound)
	maxAllowed := bound
	if bound > minAllowed {
		maxAllowed = max(minAllowed, bound-2)
	}
	return max(minAllowed, min(value, maxAllowed))
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

func floatingPaneByID(tab *Tab, paneID string) *FloatingPane {
	if tab == nil || paneID == "" {
		return nil
	}
	for _, entry := range tab.Floating {
		if entry != nil && entry.PaneID == paneID {
			return entry
		}
	}
	return nil
}

func (m *Model) paneViewportSizeInTab(tab *Tab, paneID string) (int, int, bool) {
	rects := m.visiblePaneRects(tab)
	rect, ok := rects[paneID]
	if !ok {
		return 0, 0, false
	}
	return max(1, rect.W-2), max(1, rect.H-2), true
}

func (m *Model) zoomedPaneRect(tab *Tab, rootRect Rect) (string, Rect, bool) {
	if tab == nil {
		return "", Rect{}, false
	}
	paneID := strings.TrimSpace(tab.ZoomedPaneID)
	if paneID == "" {
		return "", Rect{}, false
	}
	if tab.Panes[paneID] == nil {
		return "", Rect{}, false
	}
	return paneID, rootRect, true
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

func (m *Model) setActiveViewportOffset(x, y int) {
	tab := m.currentTab()
	pane := activePane(tab)
	if pane == nil || pane.Mode != ViewportModeFixed || !pane.Pin {
		return
	}
	viewW, viewH, ok := m.paneViewportSizeInTab(tab, pane.ID)
	if !ok {
		return
	}
	pane.Offset = Point{X: x, Y: y}
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
		m.invalidateRender()
		return
	}
	for i, entry := range floating {
		if entry.PaneID != tab.ActivePaneID {
			continue
		}
		tab.ActivePaneID = floating[(i+1)%len(floating)].PaneID
		m.invalidateRender()
		return
	}
	tab.ActivePaneID = floating[0].PaneID
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
		next := clampFloatingRectLoose(rect, bounds)
		if next == entry.Rect {
			return
		}
		entry.Rect = next
		m.invalidateRender()
		return
	}
}

func (m *Model) dragFloatingPaneTo(tab *Tab, paneID string, x, y int) {
	if tab == nil || paneID == "" {
		return
	}
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	for _, entry := range tab.Floating {
		if entry == nil || entry.PaneID != paneID {
			continue
		}
		rect := entry.Rect
		rect.X = x
		rect.Y = y
		next := clampFloatingRectLoose(rect, bounds)
		if next == entry.Rect {
			return
		}
		entry.Rect = next
		m.invalidateRender()
		return
	}
}

func (m *Model) resizeFloatingPaneTo(tab *Tab, paneID string, width, height int) bool {
	if tab == nil || paneID == "" {
		return false
	}
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	for _, entry := range tab.Floating {
		if entry == nil || entry.PaneID != paneID {
			continue
		}
		rect := entry.Rect
		rect.W = width
		rect.H = height
		next := clampFloatingRectLoose(rect, bounds)
		if next == entry.Rect {
			return false
		}
		entry.Rect = next
		m.invalidateRender()
		return true
	}
	return false
}

func (m *Model) resizeActiveFloatingPane(dw, dh int) bool {
	tab := m.currentTab()
	if tab == nil || !isFloatingPane(tab, tab.ActivePaneID) {
		return false
	}
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	for _, entry := range tab.Floating {
		if entry == nil || entry.PaneID != tab.ActivePaneID {
			continue
		}
		rect := entry.Rect
		rect.W += dw
		rect.H += dh
		next := clampFloatingRectLoose(rect, bounds)
		if next == entry.Rect {
			return false
		}
		entry.Rect = next
		m.invalidateRender()
		return true
	}
	return false
}

func (m *Model) centerActiveFloatingPane() {
	tab := m.currentTab()
	if tab == nil || !isFloatingPane(tab, tab.ActivePaneID) {
		return
	}
	entry := floatingPaneByID(tab, tab.ActivePaneID)
	if entry == nil {
		return
	}
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	next := centeredFloatingRect(bounds, entry.Rect.W, entry.Rect.H)
	if next == entry.Rect {
		return
	}
	entry.Rect = next
	m.invalidateRender()
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

func (m *Model) staggerFloatingRect(rect Rect, index int) Rect {
	if index <= 0 {
		return rect
	}
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	stepX := 4
	stepY := 2
	maxStepsX := max(1, max(0, bounds.W-rect.W)/stepX+1)
	offsetIndex := index % maxStepsX
	rect.X += offsetIndex * stepX
	rect.Y += index * stepY
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

func (m *Model) floatingRectPositionAnchor(rect Rect) string {
	bounds := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	rect = clampFloatingRect(rect, bounds)
	type candidate struct {
		name string
		rect Rect
	}
	candidates := []candidate{
		{name: "center", rect: centeredFloatingRect(bounds, rect.W, rect.H)},
		{name: "top-left", rect: Rect{X: bounds.X, Y: bounds.Y, W: rect.W, H: rect.H}},
		{name: "top-right", rect: Rect{X: bounds.X + max(0, bounds.W-rect.W), Y: bounds.Y, W: rect.W, H: rect.H}},
		{name: "bottom-left", rect: Rect{X: bounds.X, Y: bounds.Y + max(0, bounds.H-rect.H), W: rect.W, H: rect.H}},
		{name: "bottom-right", rect: Rect{X: bounds.X + max(0, bounds.W-rect.W), Y: bounds.Y + max(0, bounds.H-rect.H), W: rect.W, H: rect.H}},
	}
	best := candidates[0]
	bestScore := floatingAnchorDistance(rect, best.rect)
	for _, candidate := range candidates[1:] {
		score := floatingAnchorDistance(rect, candidate.rect)
		if score < bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best.name
}

func (m *Model) mouseContentPoint(screenX, screenY int) (int, int, bool) {
	if screenY < 1 || screenY >= m.height-1 || screenX < 0 || screenX >= m.width {
		return 0, 0, false
	}
	return screenX, screenY - 1, true
}

func (m *Model) paneAtPoint(tab *Tab, x, y int) (string, Rect, bool) {
	if tab == nil {
		return "", Rect{}, false
	}
	rootRect := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	if paneID, rect, ok := m.zoomedPaneRect(tab, rootRect); ok {
		if pointInRect(x, y, rect) {
			return paneID, rect, isFloatingPane(tab, paneID)
		}
		return "", Rect{}, false
	}
	floating := m.visibleFloatingPanes(tab)
	for i := len(floating) - 1; i >= 0; i-- {
		entry := floating[i]
		if entry == nil {
			continue
		}
		if pointInRect(x, y, entry.Rect) {
			return entry.PaneID, entry.Rect, true
		}
	}
	if tab.Root == nil {
		return "", Rect{}, false
	}
	rects := tab.Root.Rects(rootRect)
	for _, paneID := range tab.Root.LeafIDs() {
		rect, ok := rects[paneID]
		if ok && pointInRect(x, y, rect) {
			return paneID, rect, false
		}
	}
	return "", Rect{}, false
}

func floatingResizeHandleContains(rect Rect, x, y int) bool {
	if rect.W < 2 || rect.H < 2 {
		return false
	}
	handleX := max(rect.X+rect.W-2, rect.X)
	handleY := max(rect.Y+rect.H-2, rect.Y)
	return x >= handleX && x < rect.X+rect.W && y >= handleY && y < rect.Y+rect.H
}

func pointInRect(x, y int, rect Rect) bool {
	return x >= rect.X && x < rect.X+rect.W && y >= rect.Y && y < rect.Y+rect.H
}

func resetLayoutRatios(node *LayoutNode) {
	if node == nil || node.IsLeaf() {
		return
	}
	node.Ratio = 0.5
	resetLayoutRatios(node.First)
	resetLayoutRatios(node.Second)
}

func centeredFloatingRect(bounds Rect, width, height int) Rect {
	rect := Rect{W: width, H: height}
	rect = clampFloatingRect(rect, bounds)
	rect.X = bounds.X + max(0, (bounds.W-rect.W)/2)
	rect.Y = bounds.Y + max(0, (bounds.H-rect.H)/2)
	return clampFloatingRect(rect, bounds)
}

func floatingAnchorDistance(a, b Rect) int {
	return abs(a.X-b.X) + abs(a.Y-b.Y)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
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
	resizedTerminals := make(map[string]struct{}, len(rects))
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
		if !paneShouldSubmitResize(m.workspace.Tabs, pane) {
			tab.renderCache = nil
			m.invalidateRender()
			continue
		}
		if _, ok := resizedTerminals[pane.TerminalID]; ok {
			continue
		}
		resizedTerminals[pane.TerminalID] = struct{}{}
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
	hadTerminal := strings.TrimSpace(pane.TerminalID) != ""
	return func() tea.Msg {
		return paneDetachedMsg{paneID: paneID, hadTerminal: hadTerminal}
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
	if strings.TrimSpace(pane.TerminalID) == "" {
		m.notice = ""
		m.err = fmt.Errorf("active pane has no bound terminal")
		m.invalidateRender()
		return nil
	}
	if pane.Readonly {
		m.notice = ""
		m.err = fmt.Errorf("readonly pane cannot kill/remove terminal")
		m.invalidateRender()
		return nil
	}
	return m.beginTerminalStopPrompt(pane.TerminalID, terminalDisplayNameForNotice(pane), terminalBindingCount(m.workspace.Tabs, pane.TerminalID), "current pane")
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
			if strings.TrimSpace(pane.TerminalID) == "" {
				continue
			}
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

func (m *Model) removeTerminal(terminalID string) (saved int) {
	if terminalID == "" {
		return 0
	}
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != terminalID {
				continue
			}
			m.unbindPaneTerminal(pane)
			saved++
		}
	}
	if saved > 0 {
		m.invalidateRender()
	}
	return saved
}

func (m *Model) unbindPaneTerminal(pane *Pane) {
	if pane == nil || pane.Viewport == nil {
		return
	}
	if pane.stopStream != nil {
		pane.stopStream()
		pane.stopStream = nil
	}
	if pane.Snapshot == nil && pane.VTerm != nil {
		cols, rows := pane.VTerm.Size()
		pane.Snapshot = snapshotFromVTerm(pane.TerminalID, protocol.Size{Cols: uint16(cols), Rows: uint16(rows)}, pane.VTerm)
	}
	pane.TerminalID = ""
	pane.Channel = 0
	pane.AttachMode = ""
	pane.TerminalState = "unbound"
	pane.ExitCode = nil
	pane.live = false
	pane.ResizeAcquired = false
	pane.syncLost = false
	pane.recovering = false
	pane.catchingUp = false
	pane.droppedBytes = 0
	pane.renderDirty = true
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

func (m *Model) applyTerminalMetadataUpdate(terminalID string, name string, tags map[string]string) {
	if strings.TrimSpace(terminalID) == "" {
		return
	}
	changed := updateWorkspaceTerminalMetadata(&m.workspace, terminalID, name, tags)
	for workspaceName, workspace := range m.workspaceStore {
		if updateWorkspaceTerminalMetadata(&workspace, terminalID, name, tags) {
			m.workspaceStore[workspaceName] = workspace
			changed = true
		}
	}
	m.notice = "updated terminal metadata"
	if changed {
		m.invalidateRender()
		return
	}
	m.invalidateRender()
}

func (m *Model) acquireActivePaneResizeCmd() tea.Cmd {
	tab := m.currentTab()
	pane := activePane(tab)
	if pane == nil || strings.TrimSpace(pane.TerminalID) == "" {
		m.notice = ""
		m.err = fmt.Errorf("acquire-resize requires an active terminal")
		m.invalidateRender()
		return nil
	}
	if !paneAllowsResize(pane) {
		m.notice = ""
		m.err = fmt.Errorf("acquire-resize requires a running terminal")
		m.invalidateRender()
		return nil
	}
	if mode := terminalSizeLockMode(pane); mode == "warn" {
		m.pendingResizeAcquire = &resizeAcquireDraft{
			PaneID:      pane.ID,
			TerminalID:  pane.TerminalID,
			WarningMode: mode,
		}
		m.prompt = &textPrompt{
			Kind:  "confirm-acquire-resize",
			Title: "size change warning",
		}
		m.notice = ""
		m.err = nil
		m.invalidateRender()
		return nil
	}
	return m.forceAcquirePaneResize(pane)
}

func (m *Model) forceAcquirePaneResize(pane *Pane) tea.Cmd {
	if pane == nil || strings.TrimSpace(pane.TerminalID) == "" {
		return nil
	}
	clearTerminalResizeAcquire(m.workspace.Tabs, pane.TerminalID)
	pane.ResizeAcquired = true
	m.notice = fmt.Sprintf("acquired resize control for terminal %q", terminalDisplayNameForNotice(pane))
	m.err = nil
	m.invalidateRender()
	return m.resizeVisiblePanesCmd()
}

func terminalSizeLockMode(pane *Pane) string {
	if pane == nil {
		return "off"
	}
	switch strings.TrimSpace(pane.Tags["termx.size_lock"]) {
	case "warn":
		return "warn"
	default:
		return "off"
	}
}

func clearTerminalResizeAcquire(tabs []*Tab, terminalID string) {
	if strings.TrimSpace(terminalID) == "" {
		return
	}
	for _, tab := range tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane != nil && pane.TerminalID == terminalID {
				pane.ResizeAcquired = false
			}
		}
	}
}

func terminalDisplayNameForNotice(pane *Pane) string {
	if pane == nil {
		return ""
	}
	if name := strings.TrimSpace(pane.Name); name != "" {
		return name
	}
	if title := strings.TrimSpace(pane.Title); title != "" {
		return title
	}
	return pane.TerminalID
}

func (m *Model) setCurrentTabAutoAcquireCmd(value string) tea.Cmd {
	tab := m.currentTab()
	if tab == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1":
		tab.AutoAcquireResize = true
		m.notice = "tab auto-acquire resize enabled"
		m.err = nil
		m.invalidateRender()
		return nil
	case "off", "false", "0":
		tab.AutoAcquireResize = false
		m.notice = "tab auto-acquire resize disabled"
		m.err = nil
		m.invalidateRender()
		return nil
	default:
		m.notice = ""
		m.err = fmt.Errorf("tab-auto-acquire requires on or off")
		m.invalidateRender()
		return nil
	}
}

func (m *Model) setActiveTerminalSizeLockCmd(value string) tea.Cmd {
	tab := m.currentTab()
	pane := activePane(tab)
	if pane == nil || strings.TrimSpace(pane.TerminalID) == "" {
		m.notice = ""
		m.err = fmt.Errorf("set-size-lock requires an active terminal")
		m.invalidateRender()
		return nil
	}

	lockValue := strings.ToLower(strings.TrimSpace(value))
	switch lockValue {
	case "off", "warn":
	default:
		m.notice = ""
		m.err = fmt.Errorf("set-size-lock requires off or warn")
		m.invalidateRender()
		return nil
	}

	tags := cloneStringMap(pane.Tags)
	if tags == nil {
		tags = make(map[string]string)
	}
	tags["termx.size_lock"] = lockValue
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		if err := m.client.SetMetadata(ctx, pane.TerminalID, pane.Name, tags); err != nil {
			return errMsg{m.wrapClientError("set terminal metadata", err, "terminal_id", pane.TerminalID)}
		}
		return terminalMetadataUpdatedMsg{
			TerminalID: pane.TerminalID,
			Name:       pane.Name,
			Tags:       tags,
		}
	}
}

func updateWorkspaceTerminalMetadata(workspace *Workspace, terminalID string, name string, tags map[string]string) bool {
	if workspace == nil {
		return false
	}
	changed := false
	for _, tab := range workspace.Tabs {
		if tab == nil {
			continue
		}
		tabChanged := false
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != terminalID {
				continue
			}
			pane.Name = name
			pane.Tags = cloneStringMap(tags)
			pane.Title = paneTitleForCommand(name, firstCommandWord(pane.Command), pane.TerminalID)
			pane.renderDirty = true
			tabChanged = true
			changed = true
		}
		if tabChanged {
			tab.renderCache = nil
		}
	}
	return changed
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
	m.noteInteraction()
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

func paneShouldSubmitResize(tabs []*Tab, pane *Pane) bool {
	if pane == nil {
		return false
	}
	if terminalBindingCount(tabs, pane.TerminalID) <= 1 {
		return true
	}
	return pane.ResizeAcquired
}

func terminalBindingCount(tabs []*Tab, terminalID string) int {
	if strings.TrimSpace(terminalID) == "" {
		return 0
	}
	count := 0
	for _, tab := range tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane != nil && pane.TerminalID == terminalID {
				count++
			}
		}
	}
	return count
}

func paneTerminalState(pane *Pane) string {
	if pane == nil || pane.Viewport == nil || pane.TerminalState == "" {
		return "running"
	}
	return pane.TerminalState
}

func (m *Model) paneByID(paneID string) *Pane {
	if strings.TrimSpace(paneID) == "" {
		return nil
	}
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		if pane := tab.Panes[paneID]; pane != nil {
			return pane
		}
	}
	return nil
}

func (m *Model) activateTab(index int) tea.Cmd {
	if index < 0 || index >= len(m.workspace.Tabs) {
		return nil
	}
	m.workspace.ActiveTab = index
	m.invalidateRender()
	return tea.Batch(m.resizeVisiblePanesCmd(), m.autoAcquireCurrentTabResizeCmd())
}

func (m *Model) autoAcquireCurrentTabResizeCmd() tea.Cmd {
	tab := m.currentTab()
	if tab == nil || !tab.AutoAcquireResize {
		return nil
	}
	pane := activePane(tab)
	if pane == nil || strings.TrimSpace(pane.TerminalID) == "" || !paneAllowsResize(pane) {
		return nil
	}
	return m.forceAcquirePaneResize(pane)
}

func (m *Model) currentTab() *Tab {
	if m.workspace.ActiveTab < 0 || m.workspace.ActiveTab >= len(m.workspace.Tabs) {
		return nil
	}
	return m.workspace.Tabs[m.workspace.ActiveTab]
}

func (m *Model) CurrentTabForTest() *Tab {
	return m.currentTab()
}

func (m *Model) ActiveModeForTest() string {
	if m == nil || !m.prefixActive {
		return ""
	}
	switch m.prefixMode {
	case prefixModePane:
		return "pane"
	case prefixModeResize:
		return "resize"
	case prefixModeTab:
		return "tab"
	case prefixModeWorkspace:
		return "workspace"
	case prefixModeViewport:
		return "view"
	case prefixModeFloating:
		return "floating"
	case prefixModeOffsetPan:
		return "offset-pan"
	case prefixModeGlobal:
		return "global"
	default:
		return "root"
	}
}

func (m *Model) InputBlockedForTest() bool {
	if m == nil {
		return false
	}
	return m.inputBlocked
}

func (m *Model) PromptKindForTest() string {
	if m == nil || m.prompt == nil {
		return ""
	}
	return m.prompt.Kind
}

func (m *Model) SetCurrentTabAutoAcquireForTest(enabled bool) {
	if m == nil {
		return
	}
	tab := m.currentTab()
	if tab == nil {
		return
	}
	tab.AutoAcquireResize = enabled
}

func (m *Model) SetTerminalTagForTest(terminalID, key, value string) {
	if m == nil || strings.TrimSpace(terminalID) == "" || strings.TrimSpace(key) == "" {
		return
	}
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != terminalID {
				continue
			}
			if pane.Tags == nil {
				pane.Tags = make(map[string]string)
			}
			pane.Tags[key] = value
		}
	}
}

func (m *Model) ActivateTabForTest(index int) {
	if m == nil {
		return
	}
	m.runCmdForTest(m.activateTab(index))
}

func PaneTerminalStateForTest(pane *Pane) string {
	return paneTerminalState(pane)
}

func (m *Model) runCmdForTest(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	switch batch := msg.(type) {
	case tea.BatchMsg:
		for _, sub := range batch {
			m.runCmdForTest(sub)
		}
	default:
		_, next := m.Update(msg)
		if next != nil {
			m.runCmdForTest(next)
		}
	}
}

func (m *Model) renderTabBar() string {
	workspaceText := m.workspace.Name
	if m.icons.Name != "ascii" {
		workspaceText = m.icons.Workspace + " " + workspaceText
	}
	workspaceLabel := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#e2e8f0")).
		Background(lipgloss.Color("#0f172a")).
		Padding(0, 1).
		Render("[" + workspaceText + "]")

	items := make([]string, 0, len(m.workspace.Tabs)+1)
	items = append(items, workspaceLabel)
	for i, tab := range m.workspace.Tabs {
		name := strings.TrimSpace(tab.Name)
		if name == "" || name == itoa(i+1) {
			name = "tab " + itoa(i+1)
		}
		labelText := fmt.Sprintf("%d:%s", i+1, name)
		label := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94a3b8")).
			Background(lipgloss.Color("#020617")).
			Render(labelText)
		if i == m.workspace.ActiveTab {
			label = lipgloss.NewStyle().
				Bold(true).
				Underline(true).
				Foreground(lipgloss.Color("#e2e8f0")).
				Background(lipgloss.Color("#020617")).
				Render("[" + labelText + "]")
		}
		items = append(items, label)
	}
	left := strings.Join(items, " ")
	right := renderTopBarSummary(m.topBarStateParts(), m.icons)
	return fillHorizontal(left, right, m.width, lipgloss.NewStyle().Background(lipgloss.Color("#020617")))
}

func (m *Model) renderStatus() string {
	if m.prompt != nil && m.prompt.Kind == "command" {
		left := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f8fafc")).
			Background(lipgloss.Color("#1d4ed8")).
			Bold(true).
			Padding(0, 1).
			Render(m.prompt.Title + ": " + m.prompt.Value + "_")
		hint := "enter save  esc cancel"
		if m.prompt.Hint != "" {
			hint = m.prompt.Hint
		}
		right := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#dbeafe")).
			Background(lipgloss.Color("#1d4ed8")).
			Padding(0, 1).
			Render(hint)
		return fillHorizontal(left, right, m.width, lipgloss.NewStyle().Background(lipgloss.Color("#1d4ed8")))
	}

	leftParts := m.statusShortcutParts()
	rightParts := m.statusStateParts()
	left := renderStatusHints(leftParts)
	right := renderStatusSummary(rightParts, m.icons)
	return fillStatusSections(left, right, m.width, lipgloss.NewStyle().Background(lipgloss.Color("#020617")))
}

func (m *Model) statusShortcutParts() []string {
	if m.terminalManager != nil {
		item := m.selectedTerminalManagerItem()
		if item != nil && item.CreateNew {
			return []string{"[TERMINALS]", "Enter:start here", "Ctrl-t:new tab", "Ctrl-o:floating", "Esc:close"}
		}
		return []string{"[TERMINALS]", "Enter:bring here", "Ctrl-t:new tab", "Ctrl-o:floating", "Ctrl-e:edit", "Ctrl-k:stop terminal", "Esc:close"}
	}
	parts := make([]string, 0, 12)
	if m.prefixActive && m.directMode {
		switch m.prefixMode {
		case prefixModePane:
			parts = append(parts, "[PANE]", "\":split", "%:split", "hjkl:focus", "{}:swap", "z:zoom", "x:close", "X:kill", "c:new-tab", "f:pick", "Esc:exit")
		case prefixModeResize:
			parts = append(parts, "[RESIZE]", "hjkl:resize", "HJKL:coarse", "=:balance", "Space:layout", "Esc:exit")
		case prefixModeTab:
			parts = append(parts, "[TAB]", "1-9:jump", "n/p:next-prev", "c:new", "r:rename", "x:close", "f:pick", "Esc:exit")
		case prefixModeWorkspace:
			parts = append(parts, "[WORKSPACE]", "s:switch", "c:new", "r:rename", "x:delete", "n/p:next-prev", "f:pick", "Esc:exit")
		case prefixModeViewport:
			parts = append(parts, "[DISPLAY]", "m:fit/fixed", "r:readonly", "p:pin", "hjkl:pan", "0/$/g/G:jump", "z:reset", "Esc:exit")
		case prefixModeFloating:
			parts = append(parts, "[FLOAT]", "n:new", "Tab:focus", "[]:z-order", "hjkl:move", "HJKL:size", "c:center", "v:toggle", "x:close", "f:pick", "Esc:exit")
		case prefixModeGlobal:
			parts = append(parts, "[GLOBAL]", "?:help", "t:terminals", "::command", "d:detach", "q:quit", "Esc:exit")
		}
	} else if m.prefixActive {
		switch m.prefixMode {
		case prefixModeTab:
			parts = append(parts, "prefix:t", "tab:c", "rename:,", "close:x")
		case prefixModeWorkspace:
			parts = append(parts, "prefix:w", "ws:s", "create:c", "rename:r", "delete:x")
		case prefixModeViewport:
			parts = append(parts, "prefix:v", "display:m", "readonly:r", "pin:p", "pan:o")
		case prefixModeFloating:
			parts = append(parts, "mode:floating", "new:n", "move:hjkl", "size:HJKL", "center:c", "Esc")
		case prefixModeOffsetPan:
			parts = append(parts, "mode:offset-pan", "pan:hjkl", "jump:0/$/g/G", "Esc")
		default:
			parts = append(parts, "prefix", "move:hjkl", "resize:HJKL")
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "[NORMAL]", "Ctrl-p pane", "Ctrl-r resize", "Ctrl-t tab", "Ctrl-w ws", "Ctrl-o float", "Ctrl-v display", "Ctrl-f picker", "Ctrl-g global")
	} else if !m.prefixActive {
		parts = append(parts, "[NORMAL]", "Ctrl-p pane", "Ctrl-r resize", "Ctrl-t tab", "Ctrl-w ws", "Ctrl-o float", "Ctrl-v display", "Ctrl-f picker", "Ctrl-g global")
	}
	if m.showHelp {
		parts = append(parts, "[help]")
	}
	return parts
}

func (m *Model) statusStateParts() []string {
	if m.terminalManager != nil {
		item := m.selectedTerminalManagerItem()
		if item == nil {
			return []string{"term:none"}
		}
		if item.CreateNew {
			return []string{"term:new terminal", "visibility:create"}
		}
		locations := m.terminalDisplayLocations()[item.Info.ID]
		parts := []string{
			"term:" + terminalDisplayLabel(item.Info.Name, item.Info.Command),
			"visibility:" + terminalVisibility(item.Info, locations),
		}
		if count := len(locations); count > 0 {
			parts = append(parts, "shown:"+itoa(count))
		}
		return parts
	}
	parts := make([]string, 0, 8)
	if tab := m.currentTab(); tab != nil {
		if pane := tab.Panes[tab.ActivePaneID]; pane != nil {
			parts = append(parts, "pane:"+paneDisplayLabel(pane))
			if isFloatingPane(tab, tab.ActivePaneID) {
				parts = append(parts, "layer:floating")
			} else {
				parts = append(parts, "layer:tiled")
			}
			parts = append(parts, "state:"+paneTerminalState(pane))
			if pane.syncLost || pane.recovering || pane.catchingUp {
				parts = append(parts, fmt.Sprintf("catching-up:%dB", pane.droppedBytes))
			}
		}
	}
	return parts
}

func renderStatusHints(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	mode := strings.TrimSpace(parts[0])
	rendered := make([]string, 0, len(parts))
	if lead := renderStatusLead(mode); lead != "" {
		rendered = append(rendered, lead)
	}
	if badge := renderModeBadge(mode); badge != "" {
		rendered = append(rendered, badge)
	}
	for _, part := range parts[1:] {
		if hint := renderShortcutHint(part); hint != "" {
			rendered = append(rendered, hint)
		}
	}
	return strings.Join(rendered, "")
}

func renderStatusSummary(parts []string, icons iconSet) string {
	if len(parts) == 0 {
		return ""
	}
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cbd5e1")).
			Background(lipgloss.Color("#020617"))
		text := formatSummaryPart(part, icons)
		switch {
		case strings.HasPrefix(part, "err:"), strings.HasPrefix(part, "error:"):
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#fee2e2")).Background(lipgloss.Color("#7f1d1d")).Bold(true)
		case strings.HasPrefix(part, "notice:"), strings.Contains(part, "saved "), strings.Contains(part, "loaded "), strings.Contains(part, "deleted "), strings.HasPrefix(part, "updated "), strings.HasPrefix(part, "workspace:"):
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#dcfce7")).Background(lipgloss.Color("#166534")).Bold(true)
		case strings.HasPrefix(part, "pane:"):
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#f8fafc")).Background(lipgloss.Color("#020617")).Bold(true)
		case strings.HasPrefix(part, "term:"):
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#f8fafc")).Background(lipgloss.Color("#020617")).Bold(true)
		case strings.HasPrefix(part, "state:"), strings.HasPrefix(part, "display:"), strings.HasPrefix(part, "layer:"), strings.HasPrefix(part, "shared:"), strings.HasPrefix(part, "access:"), strings.HasPrefix(part, "size-lock:"), part == "readonly", part == "pinned":
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0")).Background(lipgloss.Color("#020617"))
		case strings.HasPrefix(part, "visibility:"), strings.HasPrefix(part, "shown:"), strings.HasPrefix(part, "tags:"):
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0")).Background(lipgloss.Color("#020617"))
		case strings.HasPrefix(part, "restart:"), strings.HasPrefix(part, "attach:"), strings.HasPrefix(part, "close:"), strings.HasPrefix(part, "catching-up:"):
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#fde68a")).Background(lipgloss.Color("#020617"))
		}
		items = append(items, style.Render(text))
	}
	return strings.Join(items, "  ")
}

func (m *Model) topBarStateParts() []string {
	parts := make([]string, 0, 6)
	parts = append(parts, "workspace:"+m.workspace.Name)
	if m.err != nil {
		parts = append(parts, "err:"+m.err.Error())
	} else if strings.TrimSpace(m.notice) != "" {
		parts = append(parts, "notice:"+m.notice)
	}
	if tab := m.currentTab(); tab != nil {
		parts = append(parts, "pane-count:"+itoa(len(tab.Panes)))
		if count := tabTerminalCount(tab); count > 0 {
			parts = append(parts, "term-count:"+itoa(count))
		}
		if count := len(m.visibleFloatingPanes(tab)); count > 0 {
			parts = append(parts, "float-count:"+itoa(count))
		}
		if tab.AutoAcquireResize {
			parts = append(parts, "auto-fit")
		}
	}
	return parts
}

func renderTopBarSummary(parts []string, icons iconSet) string {
	if len(parts) == 0 {
		return ""
	}
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94a3b8")).
			Background(lipgloss.Color("#020617"))
		text := formatTopBarPart(part, icons)
		switch {
		case strings.HasPrefix(part, "err:"):
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#fee2e2")).Background(lipgloss.Color("#7f1d1d")).Bold(true).Padding(0, 1)
		case strings.HasPrefix(part, "notice:"):
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0f2fe")).Background(lipgloss.Color("#0f766e")).Bold(true).Padding(0, 1)
		case strings.HasPrefix(part, "float-count:"), part == "auto-fit":
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#fef3c7")).Background(lipgloss.Color("#020617")).Bold(true)
		default:
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#cbd5e1")).Background(lipgloss.Color("#020617"))
		}
		items = append(items, style.Render(text))
	}
	return strings.Join(items, "  ")
}

func formatTopBarPart(part string, icons iconSet) string {
	switch {
	case strings.HasPrefix(part, "notice:"):
		return icons.token(strings.TrimPrefix(part, "notice:"), icons.Notice)
	case strings.HasPrefix(part, "err:"):
		return icons.token(strings.TrimPrefix(part, "err:"), icons.Error)
	case strings.HasPrefix(part, "workspace:"):
		return "ws:" + strings.TrimPrefix(part, "workspace:")
	case strings.HasPrefix(part, "pane-count:"):
		return icons.countToken("pane", icons.Pane, parsePositiveInt(strings.TrimPrefix(part, "pane-count:")))
	case strings.HasPrefix(part, "term-count:"):
		return icons.countToken("term", icons.Terminal, parsePositiveInt(strings.TrimPrefix(part, "term-count:")))
	case strings.HasPrefix(part, "float-count:"):
		return icons.countToken("float", icons.Floating, parsePositiveInt(strings.TrimPrefix(part, "float-count:")))
	case part == "auto-fit":
		return icons.token("auto-fit", icons.AutoFit)
	default:
		return part
	}
}

func formatSummaryPart(part string, icons iconSet) string {
	switch {
	case strings.HasPrefix(part, "pane:"):
		return strings.TrimPrefix(part, "pane:")
	case strings.HasPrefix(part, "term:"):
		return strings.TrimPrefix(part, "term:")
	case strings.HasPrefix(part, "state:"):
		state := strings.TrimPrefix(part, "state:")
		switch state {
		case "waiting":
			return icons.token("wait", icons.Waiting)
		case "unbound":
			return icons.token("saved", icons.Unbound)
		case "killed":
			return icons.token("killed", icons.Killed)
		case "exited":
			return icons.token("exit", icons.Exited)
		default:
			return icons.token("live", icons.Running)
		}
	case strings.HasPrefix(part, "display:"):
		mode := strings.TrimPrefix(part, "display:")
		if mode == string(ViewportModeFixed) {
			return icons.token("fixed", icons.Fixed)
		}
		return icons.token("fit", icons.Fit)
	case strings.HasPrefix(part, "layer:"):
		layer := strings.TrimPrefix(part, "layer:")
		if layer == "floating" {
			return icons.token("float", icons.Floating)
		}
		return icons.token("tiled", icons.Pane)
	case strings.HasPrefix(part, "shared:"):
		return icons.countToken("share", icons.Shared, parsePositiveInt(strings.TrimPrefix(part, "shared:")))
	case strings.HasPrefix(part, "visibility:"):
		visibility := strings.TrimPrefix(part, "visibility:")
		switch visibility {
		case "visible":
			return icons.token("visible", icons.Running)
		case "parked":
			return icons.token("parked", icons.Unbound)
		case "exited":
			return icons.token("exited", icons.Exited)
		default:
			return visibility
		}
	case strings.HasPrefix(part, "shown:"):
		return "shown:" + strings.TrimPrefix(part, "shown:")
	case strings.HasPrefix(part, "tags:"):
		return strings.TrimPrefix(part, "tags:")
	case strings.HasPrefix(part, "access:observer"):
		return icons.token("obs", icons.Observer)
	case part == "readonly":
		return icons.token("ro", icons.Readonly)
	case part == "pinned":
		return icons.token("pin", icons.Pinned)
	case strings.HasPrefix(part, "size-lock:"):
		return icons.token("lock", icons.LockWarn)
	case strings.HasPrefix(part, "catching-up:"):
		return icons.pairToken("sync", icons.CatchUp, strings.TrimPrefix(part, "catching-up:"))
	default:
		return part
	}
}

func tabTerminalCount(tab *Tab) int {
	if tab == nil {
		return 0
	}
	seen := make(map[string]struct{}, len(tab.Panes))
	for _, pane := range tab.Panes {
		if pane == nil || strings.TrimSpace(pane.TerminalID) == "" {
			continue
		}
		seen[pane.TerminalID] = struct{}{}
	}
	return len(seen)
}

func parsePositiveInt(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func renderModeBadge(mode string) string {
	label := strings.Trim(strings.TrimSpace(mode), "[]")
	if label == "" || strings.EqualFold(label, "NORMAL") {
		return ""
	}
	bg := "#d1d5db"
	switch strings.ToUpper(label) {
	case "PANE":
		bg = "#86efac"
	case "RESIZE":
		bg = "#fca5a5"
	case "TAB":
		bg = "#93c5fd"
	case "WORKSPACE":
		bg = "#fcd34d"
	case "FLOAT":
		bg = "#fde047"
	case "DISPLAY":
		bg = "#c4b5fd"
	case "GLOBAL":
		bg = "#67e8f9"
	case "TERMINALS":
		bg = "#a7f3d0"
	}
	return renderDirectionalSegment(label, bg, "#020617")
}

func renderShortcutHint(part string) string {
	key, action := splitShortcutHint(part)
	if key == "" && action == "" {
		return ""
	}
	actionLabel := key
	if strings.TrimSpace(action) != "" {
		actionLabel = action
	}
	label := strings.ToUpper(actionLabel)
	if strings.TrimSpace(key) != "" && strings.TrimSpace(action) != "" {
		label = "<" + displayShortcutKey(key) + "> " + strings.ToUpper(action)
	}
	return renderDirectionalSegment(label, "#b8b8b8", "#020617")
}

func splitShortcutHint(part string) (string, string) {
	part = strings.TrimSpace(part)
	if part == "" {
		return "", ""
	}
	if idx := strings.Index(part, ":"); idx >= 0 {
		key := strings.TrimSpace(part[:idx])
		action := strings.TrimSpace(part[idx+1:])
		if key != "" || action != "" {
			return key, action
		}
	}
	if idx := strings.Index(part, " "); idx >= 0 {
		key := strings.TrimSpace(part[:idx])
		action := strings.TrimSpace(part[idx+1:])
		if key != "" && action != "" {
			return key, action
		}
	}
	return part, ""
}

func renderStatusLead(mode string) string {
	if !strings.EqualFold(strings.Trim(strings.TrimSpace(mode), "[]"), "NORMAL") {
		return ""
	}
	lead := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f8fafc")).
		Background(lipgloss.Color("#020617")).
		Bold(true).
		Render("Ctrl + ")
	arrow := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#b8b8b8")).
		Background(lipgloss.Color("#020617")).
		Bold(true).
		Render("")
	return lead + arrow
}

func renderDirectionalSegment(label, bg, fg string) string {
	block := lipgloss.NewStyle().
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg)).
		Bold(true).
		Padding(0, 1).
		Render(label)
	arrow := lipgloss.NewStyle().
		Foreground(lipgloss.Color(bg)).
		Background(lipgloss.Color("#020617")).
		Bold(true).
		Render("")
	return block + arrow
}

func displayShortcutKey(key string) string {
	key = strings.TrimSpace(key)
	lower := strings.ToLower(key)
	switch {
	case strings.HasPrefix(lower, "ctrl-"):
		return strings.TrimSpace(key[5:])
	case strings.HasPrefix(lower, "ctrl+"):
		return strings.TrimSpace(key[5:])
	default:
		return key
	}
}

func fillStatusSections(left, right string, width int, filler lipgloss.Style) string {
	leftW := xansi.StringWidth(left)
	rightW := xansi.StringWidth(right)
	if width <= 0 {
		return ""
	}
	if left == "" {
		return forceWidthANSI(right, width)
	}
	if right == "" {
		return forceWidthANSI(left, width)
	}
	if leftW+rightW < width {
		return left + filler.Render(strings.Repeat(" ", width-leftW-rightW)) + right
	}
	gap := 1
	maxLeft := max(0, width-rightW-gap)
	if maxLeft > 0 {
		return forceWidthANSI(left, maxLeft) + filler.Render(" ") + forceWidthANSI(right, width-maxLeft-gap)
	}
	return forceWidthANSI(right, width)
}

func viewportStatusParts(pane *Pane) []string {
	if pane == nil {
		return nil
	}
	parts := []string{"state:" + paneTerminalState(pane), "display:" + string(pane.Mode)}
	if access := paneAccessMode(pane); access != "" {
		switch access {
		case "collaborator":
			parts = append(parts, "access:collab")
		default:
			parts = append(parts, "access:"+access)
		}
	}
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

func paneAccessMode(pane *Pane) string {
	if pane == nil || pane.Viewport == nil || strings.TrimSpace(pane.TerminalID) == "" {
		return ""
	}
	mode := strings.TrimSpace(strings.ToLower(pane.AttachMode))
	if mode == "" {
		return "collaborator"
	}
	return mode
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
		Kind:     "rename-tab",
		Title:    "rename tab",
		Original: tab.Name,
	}
	m.invalidateRender()
}

func (m *Model) beginRenameWorkspace() {
	m.ensureWorkspaceStore()
	name := strings.TrimSpace(m.workspace.Name)
	if name == "" {
		name = nextWorkspaceName(m.workspaceOrder)
	}
	m.prompt = &textPrompt{
		Kind:     "rename-workspace",
		Title:    "rename workspace",
		Original: name,
	}
	m.invalidateRender()
}

func (m *Model) beginCommandPrompt() {
	m.prompt = &textPrompt{Kind: "command", Title: "command"}
	m.invalidateRender()
}

func (m *Model) beginTerminalStopPrompt(terminalID, displayName string, paneCount int, locationHint string) tea.Cmd {
	if strings.TrimSpace(terminalID) == "" {
		return nil
	}
	if paneCount <= 0 {
		paneCount = terminalBindingCount(m.workspace.Tabs, terminalID)
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = terminalID
	}
	m.pendingTerminalStop = &terminalStopDraft{
		TerminalID:   terminalID,
		DisplayName:  displayName,
		PaneCount:    paneCount,
		LocationHint: locationHint,
	}
	m.prompt = &textPrompt{
		Kind:  "confirm-stop-terminal",
		Title: "stop terminal",
		Hint:  "enter stop  esc cancel",
	}
	m.notice = ""
	m.err = nil
	m.invalidateRender()
	return nil
}

func (m *Model) beginTerminalEditPrompt(info protocol.TerminalInfo) {
	if strings.TrimSpace(info.ID) == "" {
		return
	}
	name := terminalDisplayLabel(info.Name, info.Command)
	m.pendingTerminalEdit = &terminalMetadataDraft{
		TerminalID:   info.ID,
		DefaultName:  name,
		Name:         name,
		Command:      append([]string(nil), info.Command...),
		OriginalTags: cloneStringMap(info.Tags),
		Tags:         cloneStringMap(info.Tags),
	}
	m.prompt = &textPrompt{
		Kind:     "edit-terminal-name",
		Title:    "edit terminal name",
		Value:    name,
		Original: name,
		Hint:     "enter next  esc cancel",
	}
	m.invalidateRender()
}

func (m *Model) beginTerminalCreatePrompt(action terminalPickerAction, command []string) {
	if len(command) == 0 {
		command = []string{m.cfg.DefaultShell}
	}
	defaultName := m.nextTerminalName(command)
	m.pendingTerminalCreate = &terminalCreateDraft{
		Action:      action,
		Command:     append([]string(nil), command...),
		DefaultName: defaultName,
	}
	m.prompt = &textPrompt{
		Kind:     "create-terminal-name",
		Title:    "new terminal name",
		Value:    defaultName,
		Original: defaultName,
		Hint:     "enter next  esc cancel",
	}
	m.invalidateRender()
}

func (m *Model) promptAcceptsText() bool {
	if m.prompt == nil {
		return false
	}
	return !strings.HasPrefix(m.prompt.Kind, "confirm-")
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
	if m.prompt.Kind == "command" {
		value := strings.TrimSpace(m.prompt.Value)
		m.prompt = nil
		m.invalidateRender()
		return m.executeCommandPrompt(value)
	}
	value := strings.TrimSpace(m.prompt.Value)
	if value == "" && m.prompt.Kind != "create-terminal-tags" && m.prompt.Kind != "edit-terminal-tags" {
		value = m.prompt.Original
	}
	switch m.prompt.Kind {
	case "create-terminal-name":
		if m.pendingTerminalCreate == nil {
			m.prompt = nil
			m.invalidateRender()
			return nil
		}
		m.pendingTerminalCreate.Name = value
		m.prompt = &textPrompt{
			Kind:  "create-terminal-tags",
			Title: "new terminal tags",
			Hint:  "key=value key2=value2  enter create  esc cancel",
		}
		m.invalidateRender()
		return nil
	case "create-terminal-tags":
		if m.pendingTerminalCreate == nil {
			m.prompt = nil
			m.invalidateRender()
			return nil
		}
		tags, err := parseTerminalTagsInput(value)
		if err != nil {
			m.err = err
			m.invalidateRender()
			return nil
		}
		draft := *m.pendingTerminalCreate
		draft.Tags = tags
		m.pendingTerminalCreate = nil
		m.prompt = nil
		m.invalidateRender()
		cmd := m.finishPendingTerminalCreate(draft)
		if cmd != nil {
			m.inputBlocked = true
			m.notice = "opening pane"
			m.invalidateRender()
		}
		return cmd
	case "edit-terminal-name":
		if m.pendingTerminalEdit == nil {
			m.prompt = nil
			m.invalidateRender()
			return nil
		}
		m.pendingTerminalEdit.Name = value
		tagsValue := formatTerminalTagsInput(m.pendingTerminalEdit.Tags)
		m.prompt = &textPrompt{
			Kind:     "edit-terminal-tags",
			Title:    "edit terminal tags",
			Value:    tagsValue,
			Original: tagsValue,
			Hint:     "key=value key2=value2  enter save  esc cancel",
		}
		m.invalidateRender()
		return nil
	case "edit-terminal-tags":
		if m.pendingTerminalEdit == nil {
			m.prompt = nil
			m.invalidateRender()
			return nil
		}
		tags, err := parseTerminalTagsInput(value)
		if err != nil {
			m.err = err
			m.invalidateRender()
			return nil
		}
		draft := *m.pendingTerminalEdit
		draft.Tags = tags
		m.pendingTerminalEdit = nil
		m.prompt = nil
		m.invalidateRender()
		cmd := m.finishPendingTerminalEdit(draft)
		if cmd != nil {
			m.inputBlocked = true
			m.notice = "updating terminal metadata"
			m.invalidateRender()
		}
		return cmd
	case "confirm-acquire-resize":
		if m.pendingResizeAcquire == nil {
			m.prompt = nil
			m.invalidateRender()
			return nil
		}
		draft := *m.pendingResizeAcquire
		m.pendingResizeAcquire = nil
		m.prompt = nil
		m.invalidateRender()
		pane := m.paneByID(draft.PaneID)
		if pane == nil || pane.TerminalID != draft.TerminalID {
			m.notice = ""
			m.err = fmt.Errorf("resize acquire target is no longer available")
			m.invalidateRender()
			return nil
		}
		return m.forceAcquirePaneResize(pane)
	case "confirm-stop-terminal":
		if m.pendingTerminalStop == nil {
			m.prompt = nil
			m.invalidateRender()
			return nil
		}
		draft := *m.pendingTerminalStop
		m.pendingTerminalStop = nil
		m.prompt = nil
		m.invalidateRender()
		return func() tea.Msg {
			ctx, cancel := m.requestContext()
			defer cancel()
			if err := m.client.Kill(ctx, draft.TerminalID); err != nil {
				return errMsg{m.wrapClientError("stop terminal", err, "terminal_id", draft.TerminalID)}
			}
			return terminalClosedMsg{terminalID: draft.TerminalID}
		}
	case "rename-workspace":
		if value != "" {
			m.renameCurrentWorkspace(value)
		}
	default:
		if tab := m.currentTab(); tab != nil && value != "" {
			tab.Name = value
		}
	}
	m.prompt = nil
	m.invalidateRender()
	return nil
}

func (m *Model) finishPendingTerminalCreate(draft terminalCreateDraft) tea.Cmd {
	spec := terminalCreateSpec{
		Command: append([]string(nil), draft.Command...),
		Name:    strings.TrimSpace(coalesce(draft.Name, draft.DefaultName)),
		Tags:    cloneStringMap(draft.Tags),
	}
	switch draft.Action.Kind {
	case terminalPickerActionReplace:
		tab := m.currentTab()
		if tab == nil || activePane(tab) == nil {
			return m.createPaneCmdWithSpec(draft.Action.TabIndex, "", "", spec)
		}
		return m.createTerminalForPaneCmdWithSpec(tab.ActivePaneID, spec)
	case terminalPickerActionBootstrap:
		return m.createPaneCmdWithSpec(draft.Action.TabIndex, "", "", spec)
	case terminalPickerActionNewTab:
		m.workspace.Tabs = append(m.workspace.Tabs, newTab(nextTabName(m.workspace.Tabs)))
		m.workspace.ActiveTab = len(m.workspace.Tabs) - 1
		m.invalidateRender()
		return m.createPaneCmdWithSpec(m.workspace.ActiveTab, "", "", spec)
	case terminalPickerActionSplit:
		return m.createPaneCmdWithSpec(draft.Action.TabIndex, draft.Action.TargetID, draft.Action.Split, spec)
	case terminalPickerActionFloating:
		return m.createFloatingPaneCmdWithSpec(draft.Action.TabIndex, spec)
	default:
		return nil
	}
}

func (m *Model) finishPendingTerminalEdit(draft terminalMetadataDraft) tea.Cmd {
	name := strings.TrimSpace(coalesce(draft.Name, draft.DefaultName))
	tags := cloneStringMap(draft.Tags)
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		if err := m.client.SetMetadata(ctx, draft.TerminalID, name, tags); err != nil {
			return errMsg{m.wrapClientError("set terminal metadata", err, "terminal_id", draft.TerminalID)}
		}
		return terminalMetadataUpdatedMsg{
			TerminalID: draft.TerminalID,
			Name:       name,
			Tags:       tags,
		}
	}
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
	for _, pane := range findPanesByTerminalID(tabs, terminalID) {
		return pane
	}
	return nil
}

func findPanesByTerminalID(tabs []*Tab, terminalID string) []*Pane {
	if terminalID == "" {
		return nil
	}
	var panes []*Pane
	for _, tab := range tabs {
		for _, pane := range tab.Panes {
			if pane != nil && pane.TerminalID == terminalID {
				panes = append(panes, pane)
			}
		}
	}
	return panes
}

func firstPaneID(panes map[string]*Pane) string {
	for id := range panes {
		return id
	}
	return ""
}

func workspaceHasPanes(workspace *Workspace) bool {
	if workspace == nil {
		return false
	}
	for _, tab := range workspace.Tabs {
		if tab != nil && len(tab.Panes) > 0 {
			return true
		}
	}
	return false
}

func (m *Model) ensurePaneRuntime(pane *Pane) bool {
	if pane == nil {
		return false
	}
	if pane.Viewport == nil {
		pane.Viewport = &Viewport{
			TerminalID:    pane.TerminalID,
			Channel:       pane.Channel,
			Snapshot:      pane.Snapshot,
			Name:          pane.Name,
			Command:       append([]string(nil), pane.Command...),
			Tags:          cloneStringMap(pane.Tags),
			TerminalState: defaultTerminalState(pane.TerminalState),
			ExitCode:      pane.ExitCode,
			Mode:          defaultViewportMode(pane.Mode, ViewportModeFit),
			Offset:        pane.Offset,
			Pin:           pane.Pin,
			Readonly:      pane.Readonly,
			renderDirty:   true,
		}
	}
	if pane.VTerm != nil {
		return true
	}
	cols, rows := 80, 24
	if pane.Snapshot != nil {
		if pane.Snapshot.Size.Cols > 0 {
			cols = int(max(20, pane.Snapshot.Size.Cols))
		}
		if pane.Snapshot.Size.Rows > 0 {
			rows = int(max(5, pane.Snapshot.Size.Rows))
		}
	}
	channel := pane.Channel
	pane.VTerm = localvterm.New(cols, rows, 10000, func(data []byte) {
		ctx, cancel := m.requestContext()
		defer cancel()
		_ = m.client.Input(ctx, channel, data)
	})
	if pane.Snapshot != nil {
		loadSnapshotIntoVTerm(pane.VTerm, pane.Snapshot)
	}
	pane.renderDirty = true
	pane.cellCache = nil
	pane.viewportCache = nil
	m.logger.Warn("recreated missing pane runtime", "pane_id", pane.ID, "terminal_id", pane.TerminalID, "channel", pane.Channel)
	return true
}

func loadSnapshotIntoVTerm(vt *localvterm.VTerm, snap *protocol.Snapshot) {
	if vt == nil || snap == nil {
		return
	}
	cols, rows := vt.Size()
	vt.LoadSnapshot(protocolScreenToVTerm(snap.Screen), protocolCursorToVTerm(snap.Cursor), protocolModesToVTerm(snap.Modes))
	if cols > 0 && rows > 0 {
		vt.Resize(cols, rows)
	}
}

func snapshotFromVTerm(terminalID string, size protocol.Size, vt *localvterm.VTerm) *protocol.Snapshot {
	if vt == nil {
		return nil
	}
	screen := vt.ScreenContent()
	rows := make([][]protocol.Cell, 0, len(screen.Cells))
	for _, row := range screen.Cells {
		out := make([]protocol.Cell, 0, len(row))
		for _, cell := range row {
			out = append(out, protocolCellFromVTermCell(cell))
		}
		rows = append(rows, out)
	}
	scrollback := vt.ScrollbackContent()
	backlog := make([][]protocol.Cell, 0, len(scrollback))
	for _, row := range scrollback {
		out := make([]protocol.Cell, 0, len(row))
		for _, cell := range row {
			out = append(out, protocolCellFromVTermCell(cell))
		}
		backlog = append(backlog, out)
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       size,
		Screen: protocol.ScreenData{
			Cells:             rows,
			IsAlternateScreen: screen.IsAlternateScreen,
		},
		Scrollback: backlog,
		Cursor:     protocolCursorFromVTerm(vt.CursorState()),
		Modes:      protocolModesFromVTerm(vt.Modes()),
		Timestamp:  time.Now(),
	}
}

func protocolCellFromVTermCell(cell localvterm.Cell) protocol.Cell {
	return protocol.Cell{
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

func protocolCursorFromVTerm(cursor localvterm.CursorState) protocol.CursorState {
	return protocol.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   string(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func protocolModesFromVTerm(modes localvterm.TerminalModes) protocol.TerminalModes {
	return protocol.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		MouseTracking:     modes.MouseTracking,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}

func (m *Model) resizeTerminalPanes(terminalID, skipPaneID string, cols, rows uint16) {
	if terminalID == "" || cols == 0 || rows == 0 {
		return
	}
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane == nil || pane.ID == skipPaneID || pane.TerminalID != terminalID {
				continue
			}
			if !m.ensurePaneRuntime(pane) {
				continue
			}
			pane.VTerm.Resize(int(cols), int(rows))
			if pane.Snapshot != nil {
				pane.Snapshot.Size = protocol.Size{Cols: cols, Rows: rows}
			}
			pane.cellVersion++
			pane.renderDirty = true
			pane.clearDirtyRegion()
			pane.cellCache = nil
			if viewW, viewH, ok := m.paneViewportSizeInTab(tab, pane.ID); ok && m.syncViewport(pane, viewW, viewH) {
				tab.renderCache = nil
				continue
			}
			tab.renderCache = nil
		}
	}
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

func (m *Model) nextTerminalName(command []string) string {
	m.nextTerminal++
	return fmt.Sprintf("%s-%d", terminalDisplayLabel("", command), m.nextTerminal)
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
	m.prefixMode = prefixModeRoot
	m.prefixFallback = prefixFallbackNone
	m.invalidateRender()
	return m.armPrefixTimeout()
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

func (c *canvas) StringFixedWidth() string {
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
		lines[i] = line.String()
	}
	return strings.Join(lines, "\n")
}

func (m *Model) emptyStateLines() []string {
	return []string{
		"Start New Terminal",
		"Attach Existing Terminal",
		"Open Workspace Picker",
		"Help Manual",
	}
}

func (m *Model) renderEmptyStateBody(contentHeight int) string {
	lines := []string{
		"termx workspace",
		"",
		"  Enter      Start New Terminal",
		"  Ctrl-f     Attach Existing Terminal",
		"  Ctrl-w     Open Workspace Picker",
		"  Ctrl-g ?   Help Manual",
	}
	cardWidth := min(max(42, m.width/2), max(34, m.width-12))
	cardWidth = max(34, cardWidth)
	contentWidth := max(24, cardWidth-2)
	cardLines := make([]string, 0, len(lines)+2)
	cardLines = append(cardLines, centeredPromptBorderLine("top", contentWidth, "termx workspace"))
	cardLines = append(cardLines, centeredPromptContentLine("", contentWidth))
	for _, line := range lines[2:] {
		cardLines = append(cardLines, centeredPromptContentLine(line, contentWidth))
	}
	cardLines = append(cardLines, centeredPromptContentLine("", contentWidth))
	cardLines = append(cardLines, centeredPromptBorderLine("bottom", contentWidth, "v1"))
	card := strings.Join(cardLines, "\n")
	body := lipgloss.Place(
		m.width,
		contentHeight,
		lipgloss.Center,
		lipgloss.Center,
		card,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#020617")),
	)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e2e8f0")).
		Background(lipgloss.Color("#020617")).
		Render(forceHeight(body, contentHeight))
}

func (m *Model) renderPromptScreen() string {
	tabBar := m.renderTabBar()
	status := m.renderStatus()
	contentHeight := max(1, m.height-2)
	title, lines, footer := m.promptModalContent()
	if title == "" {
		return strings.Join([]string{tabBar, m.renderContentBody(), status}, "\n")
	}
	contentWidth := min(max(38, m.width/2), max(30, m.width-12))
	contentWidth = max(28, contentWidth-2)
	cardLines := make([]string, 0, len(lines)+2)
	cardLines = append(cardLines, centeredPromptBorderLine("top", contentWidth, title))
	for _, line := range lines {
		cardLines = append(cardLines, centeredPromptContentLine(line, contentWidth))
	}
	cardLines = append(cardLines, centeredPromptContentLine("", contentWidth))
	cardLines = append(cardLines, centeredPromptContentLine(footer, contentWidth))
	cardLines = append(cardLines, centeredPromptBorderLine("bottom", contentWidth, ""))
	card := strings.Join(cardLines, "\n")
	body := lipgloss.Place(
		m.width,
		contentHeight,
		lipgloss.Center,
		lipgloss.Center,
		card,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#020617")),
	)
	body = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e2e8f0")).
		Background(lipgloss.Color("#020617")).
		Render(forceHeight(body, contentHeight))
	return strings.Join([]string{tabBar, body, status}, "\n")
}

func (m *Model) promptModalContent() (string, []string, string) {
	if m.prompt == nil {
		return "", nil, ""
	}
	fieldLabel := "value"
	title := strings.Title(m.prompt.Title)
	footer := "Enter save  Esc cancel"
	lines := make([]string, 0, 4)
	switch m.prompt.Kind {
	case "create-terminal-name":
		title = "New Terminal"
		fieldLabel = "name"
		footer = "Enter next  Esc cancel"
		lines = append(lines, "step 1/2")
		if draft := m.pendingTerminalCreate; draft != nil {
			lines = append(lines,
				"opens in: "+terminalCreateTargetLabel(draft.Action),
				"command: "+terminalCommandLabel(draft.Command),
			)
		}
	case "create-terminal-tags":
		title = "New Terminal"
		fieldLabel = "tags"
		footer = "Enter create  Esc cancel"
		lines = append(lines, "step 2/2")
		if draft := m.pendingTerminalCreate; draft != nil {
			lines = append(lines,
				"name: "+strings.TrimSpace(coalesce(draft.Name, draft.DefaultName)),
				"opens in: "+terminalCreateTargetLabel(draft.Action),
				"tip: use key=value pairs, e.g. role=api team=infra",
			)
		}
	case "edit-terminal-name":
		title = "Edit Terminal"
		fieldLabel = "name"
		footer = "Enter next  Esc cancel"
		lines = append(lines, "step 1/2")
		if draft := m.pendingTerminalEdit; draft != nil {
			lines = append(lines,
				fmt.Sprintf("terminal id: %s", draft.TerminalID),
				"command: "+terminalCommandLabel(draft.Command),
			)
		}
		lines = append(lines, "updates terminal metadata for every attached pane")
	case "edit-terminal-tags":
		title = "Edit Terminal"
		fieldLabel = "tags"
		footer = "Enter save  Esc cancel"
		lines = append(lines, "step 2/2")
		if draft := m.pendingTerminalEdit; draft != nil {
			lines = append(lines,
				fmt.Sprintf("terminal id: %s", draft.TerminalID),
				"name: "+strings.TrimSpace(coalesce(draft.Name, draft.DefaultName)),
				"command: "+terminalCommandLabel(draft.Command),
			)
		}
		lines = append(lines, "updates terminal metadata for every attached pane")
	case "rename-tab":
		title = "Rename Tab"
		fieldLabel = "name"
	case "rename-workspace":
		title = "Rename Workspace"
		fieldLabel = "name"
	case "confirm-acquire-resize":
		title = "Size Change Warning"
		footer = "Enter continue  Esc cancel"
		lines = []string{
			"this terminal may be running an interactive tui program",
			"changing terminal size can affect internal rendering",
		}
		if draft := m.pendingResizeAcquire; draft != nil {
			lines = append(lines,
				"",
				fmt.Sprintf("terminal: %s", draft.TerminalID),
				fmt.Sprintf("lock mode: %s", draft.WarningMode),
			)
		}
		return title, lines, footer
	case "confirm-stop-terminal":
		title = "Stop Terminal"
		footer = "Enter stop  Esc cancel"
		lines = []string{
			"Stopping this terminal will affect every pane viewing it.",
		}
		if draft := m.pendingTerminalStop; draft != nil {
			lines = append(lines,
				"",
				fmt.Sprintf("terminal: %s", draft.DisplayName),
				fmt.Sprintf("terminal id: %s", draft.TerminalID),
			)
			if draft.PaneCount > 0 {
				lines = append(lines, fmt.Sprintf("visible in panes: %d", draft.PaneCount))
			}
			if strings.TrimSpace(draft.LocationHint) != "" {
				lines = append(lines, fmt.Sprintf("requested from: %s", draft.LocationHint))
			}
		}
		return title, lines, footer
	default:
		fieldLabel = "value"
	}
	inputLine := fmt.Sprintf("%s:  [ %s_ ]", fieldLabel, m.prompt.Value)
	if len(lines) > 0 {
		lines = append([]string{inputLine, ""}, lines...)
	} else {
		lines = append(lines, inputLine)
	}
	return title, lines, footer
}

func centeredPromptBorderLine(edge string, innerWidth int, title string) string {
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff"))
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f8fafc")).
		Background(lipgloss.Color("#0f172a")).
		Bold(true)
	switch edge {
	case "top":
		title = xansi.Truncate(" "+title+" ", innerWidth, "")
		return borderStyle.Render("┌") +
			titleStyle.Render(title) +
			borderStyle.Render(strings.Repeat("─", max(0, innerWidth-xansi.StringWidth(title)))) +
			borderStyle.Render("┐")
	case "bottom":
		if strings.TrimSpace(title) == "" {
			return borderStyle.Render("└" + strings.Repeat("─", innerWidth) + "┘")
		}
		title = xansi.Truncate(" "+title+" ", innerWidth, "")
		return borderStyle.Render("└") +
			borderStyle.Render(strings.Repeat("─", max(0, innerWidth-xansi.StringWidth(title)))) +
			titleStyle.Render(title) +
			borderStyle.Render("┘")
	default:
		return borderStyle.Render("└" + strings.Repeat("─", innerWidth) + "┘")
	}
}

func terminalCreateTargetLabel(action terminalPickerAction) string {
	switch action.Kind {
	case terminalPickerActionNewTab:
		return "new tab"
	case terminalPickerActionSplit:
		return "split pane"
	case terminalPickerActionFloating:
		return "floating pane"
	case terminalPickerActionBootstrap:
		return "new pane"
	default:
		return "current pane"
	}
}

func terminalCommandLabel(command []string) string {
	raw := strings.TrimSpace(strings.Join(command, " "))
	if raw == "" {
		return "(default shell)"
	}
	return raw
}

func centeredPromptContentLine(content string, innerWidth int) string {
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff"))
	panelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0")).Background(lipgloss.Color("#0b1220"))
	return borderStyle.Render("│") +
		panelStyle.Render(forceWidthANSI(content, innerWidth)) +
		borderStyle.Render("│")
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
		title = " " + paneTitle(pane) + " "
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
	lines := []string{
		"Most used",
		"  Ctrl-p   pane actions",
		"  Ctrl-r   resize actions",
		"  Ctrl-t   tab actions",
		"  Ctrl-w   workspace actions",
		"  Ctrl-o   floating actions",
		"  Ctrl-v   display actions",
		"  Ctrl-f   terminal picker",
		"  Ctrl-g   global actions",
		"",
		"Concepts",
		"  pane     visible area; it shows a terminal",
		"  terminal runtime object managed by the server",
		"  manager  terminal pool page for attach/edit/stop",
		"",
		"Shared terminal",
		"  acquire resize control before changing PTY size",
		"  size lock warn warns before risky size changes",
		"  one terminal can appear in multiple panes",
		"",
		"Floating",
		"  Tab focus next floating pane",
		"  h/j/k/l move, H/J/K/L resize, [/] z-order",
		"  c center current floating pane",
		"  drag body to move",
		"  drag bottom-right corner to resize",
		"",
		"Exit",
		"  Esc      close current mode/modal",
		"  Ctrl-g d detach, Ctrl-g q quit",
	}
	contentWidth := min(max(70, m.width-18), max(42, m.width-8))
	cardLines := make([]string, 0, len(lines)+4)
	cardLines = append(cardLines, centeredPromptBorderLine("top", contentWidth, "Help / Shortcut Map"))
	maxBodyLines := max(1, contentHeight-4)
	if len(lines) > maxBodyLines {
		lines = lines[:maxBodyLines]
	}
	for _, line := range lines {
		cardLines = append(cardLines, centeredPromptContentLine(line, contentWidth))
	}
	cardLines = append(cardLines, centeredPromptContentLine("", contentWidth))
	cardLines = append(cardLines, centeredPromptContentLine("Esc close", contentWidth))
	cardLines = append(cardLines, centeredPromptBorderLine("bottom", contentWidth, ""))
	card := strings.Join(cardLines, "\n")
	body := lipgloss.Place(
		m.width,
		contentHeight,
		lipgloss.Center,
		lipgloss.Center,
		card,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#020617")),
	)
	body = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e2e8f0")).
		Background(lipgloss.Color("#020617")).
		Render(forceHeight(body, contentHeight))
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
	if strings.TrimSpace(fallback) != "" {
		return "terminal"
	}
	return "terminal"
}

func terminalDisplayLabel(name string, command []string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	base := filepath.Base(strings.TrimSpace(firstCommandWord(command)))
	switch base {
	case "", ".", string(filepath.Separator):
		return "terminal"
	case "sh", "bash", "zsh":
		return "shell"
	default:
		return base
	}
}

func paneDisplayLabel(pane *Pane) string {
	if pane == nil {
		return "terminal"
	}
	if strings.TrimSpace(pane.Title) != "" && pane.Title != "terminal" {
		return pane.Title
	}
	return terminalDisplayLabel(pane.Name, pane.Command)
}

func formatTerminalTagsInput(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+tags[key])
	}
	return strings.Join(parts, " ")
}

func parseTerminalTagsInput(input string) (map[string]string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}
	fields := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
	tags := make(map[string]string, len(fields))
	for _, field := range fields {
		if field == "" {
			continue
		}
		key, value, ok := strings.Cut(field, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("invalid tag %q; use key=value", field)
		}
		tags[key] = value
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
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
	if output != nil {
		_ = uv.EncodeMouseMode(output, uv.MouseModeDrag)
		defer func() {
			_ = uv.EncodeMouseMode(output, uv.MouseModeNone)
		}()
	}
	stopInput, restoreInput, err := startInputForwarder(program, input)
	if err != nil {
		model.logger.Error("failed to start input forwarder", "error", err)
		return err
	}
	if output != nil {
		_, _ = io.WriteString(output, xansi.RequestForegroundColor+xansi.RequestBackgroundColor+requestTerminalPaletteQueries())
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

func colorToHex(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

func requestTerminalPaletteQueries() string {
	var b strings.Builder
	for i := 0; i < 16; i++ {
		b.WriteString("\x1b]4;")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(";?\x07")
	}
	return b.String()
}
