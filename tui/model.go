package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	DefaultShell string
	Workspace    string
}

type Workspace struct {
	Name      string
	Tabs      []*Tab
	ActiveTab int
}

type Tab struct {
	Name         string
	Root         *LayoutNode
	Panes        map[string]*Pane
	ActivePaneID string
	ZoomedPaneID string
	LayoutPreset int

	renderCache *tabRenderCache
}

type tabRenderCache struct {
	canvas       *composedCanvas
	rects        map[string]Rect
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
	TerminalID string
	Channel    uint16
	VTerm      *localvterm.VTerm
	Snapshot   *protocol.Snapshot
	Mode       ViewportMode
	Offset     Point
	Pin        bool
	Readonly   bool
	stopStream func()

	cellCache    [][]drawCell
	renderDirty  bool
	live         bool
	syncLost     bool
	droppedBytes uint64
	recovering   bool
	catchingUp   bool
	dirtyTicks   int
	cleanTicks   int
	skipTick     bool
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
	Query    string
	Items    []terminalPickerItem
	Filtered []terminalPickerItem
	Selected int
}

type terminalPickerItem struct {
	Info     protocol.TerminalInfo
	Observed bool
	Orphan   bool
	Location string
}

type Model struct {
	client Client
	cfg    Config

	program *tea.Program

	renderInterval      time.Duration
	renderCache         string
	renderDirty         bool
	renderBatching      bool
	renderTickerStop    chan struct{}
	renderTickerRunning bool
	renderPending       atomic.Bool

	workspace      Workspace
	width          int
	height         int
	prefixActive   bool
	prefixSeq      int
	prefixTimeout  time.Duration
	rawPending     []byte
	showHelp       bool
	prompt         *textPrompt
	terminalPicker *terminalPicker
	nextPane       int
	nextTab        int
	quitting       bool
	err            error
}

type paneCreatedMsg struct {
	tabIndex int
	targetID string
	split    SplitDirection
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
	items []terminalPickerItem
	err   error
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
	return &Model{
		client: client,
		cfg:    cfg,
		workspace: Workspace{
			Name: cfg.Workspace,
			Tabs: []*Tab{newTab("1")},
		},
		renderInterval: 16 * time.Millisecond,
		renderDirty:    true,
		width:          80,
		height:         24,
		prefixTimeout:  800 * time.Millisecond,
	}
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
	return m.createPaneCmd(0, "", "")
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.invalidateRender()
		return m, m.resizeVisiblePanesCmd()
	case tea.KeyMsg:
		return m.handleKey(msg)
	case paneCreatedMsg:
		m.attachPane(msg)
		return m, m.resizeVisiblePanesCmd()
	case paneReplacedMsg:
		m.replacePane(msg)
		return m, m.resizeVisiblePanesCmd()
	case paneOutputMsg:
		cmd := m.handlePaneOutput(msg)
		if m.quitting {
			return m, tea.Quit
		}
		return m, cmd
	case paneResizeMsg:
		m.handlePaneResize(msg)
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
			m.invalidateRender()
			return m, nil
		}
		m.terminalPicker = &terminalPicker{Items: msg.items}
		m.terminalPicker.applyFilter()
		m.invalidateRender()
		return m, nil
	case terminalClosedMsg:
		if m.removeTerminal(msg.terminalID) {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
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
		m.err = msg.err
		m.invalidateRender()
		return m, nil
	}
	return m, nil
}

func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "loading..."
	}

	if m.renderBatching && !m.renderDirty && m.renderCache != "" {
		if m.program != nil || !m.anyPaneDirty() {
			return m.renderCache
		}
	}

	var out string
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
		m.workspace.Tabs = append(m.workspace.Tabs, newTab(fmt.Sprintf("%d", len(m.workspace.Tabs)+1)))
		m.workspace.ActiveTab = len(m.workspace.Tabs) - 1
		m.invalidateRender()
		return m.createPaneCmd(m.workspace.ActiveTab, "", "")
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
		m.commitPrompt()
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
		m.commitPrompt()
		return 1, nil, true
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
	case '"', '%', ',', 'f', 'h', 'j', 'k', 'l', 'H', 'J', 'K', 'L', 'M', 'P', 'R', 'X', 'c', 'n', 'p', 'z', '{', '}', ' ', 'x', '&', 'd', '?':
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
	return m.createPaneCmd(m.workspace.ActiveTab, tab.ActivePaneID, dir)
}

func (m *Model) createPaneCmd(tabIndex int, targetID string, split SplitDirection) tea.Cmd {
	return func() tea.Msg {
		size := protocol.Size{Cols: 80, Rows: 24}
		created, err := m.client.Create(context.Background(), []string{m.cfg.DefaultShell}, "", size)
		if err != nil {
			return errMsg{err}
		}
		attached, err := m.client.Attach(context.Background(), created.TerminalID, "collaborator")
		if err != nil {
			return errMsg{err}
		}
		snap, err := m.client.Snapshot(context.Background(), created.TerminalID, 0, 200)
		if err != nil {
			return errMsg{err}
		}
		paneID := m.nextPaneID()
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

func (m *Model) attachPane(msg paneCreatedMsg) {
	if msg.tabIndex >= len(m.workspace.Tabs) {
		return
	}
	tab := m.workspace.Tabs[msg.tabIndex]
	if tab.Root == nil {
		tab.Root = NewLeaf(msg.pane.ID)
	} else if msg.targetID != "" {
		_ = tab.Root.Split(msg.targetID, msg.split, msg.pane.ID)
		tab.LayoutPreset = layoutPresetCustom
	}
	tab.Panes[msg.pane.ID] = msg.pane
	tab.ActivePaneID = msg.pane.ID

	stream, stop := m.client.Stream(msg.pane.Channel)
	msg.pane.stopStream = stop
	if m.program != nil {
		go func(paneID string) {
			for frame := range stream {
				m.program.Send(paneOutputMsg{paneID: paneID, frame: frame})
			}
		}(msg.pane.ID)
	}
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
	stream, stop := m.client.Stream(pane.Channel)
	pane.stopStream = stop
	if m.program != nil {
		go func(paneID string) {
			for frame := range stream {
				m.program.Send(paneOutputMsg{paneID: paneID, frame: frame})
			}
		}(pane.ID)
	}
	m.invalidateRender()
}

func (m *Model) handlePaneOutput(msg paneOutputMsg) tea.Cmd {
	pane := findPane(m.workspace.Tabs, msg.paneID)
	if pane == nil {
		return nil
	}
	switch msg.frame.Type {
	case protocol.TypeOutput:
		_, _ = pane.VTerm.Write(msg.frame.Payload)
		pane.live = true
		pane.renderDirty = true
		pane.syncLost = false
		pane.recovering = false
		if tab := m.tabForPane(pane.ID); tab != nil {
			if viewW, viewH, ok := m.paneViewportSizeInTab(tab, pane.ID); ok && m.syncViewport(pane, viewW, viewH) {
				tab.renderCache = nil
			}
		}
		m.scheduleRender()
	case protocol.TypeClosed:
		if m.removeTerminal(pane.TerminalID) {
			m.quitting = true
		}
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

func (m *Model) handlePaneRecovered(msg paneRecoveredMsg) {
	pane := findPane(m.workspace.Tabs, msg.paneID)
	if pane == nil || msg.snapshot == nil {
		return
	}
	pane.Snapshot = msg.snapshot
	pane.VTerm.LoadSnapshot(protocolScreenToVTerm(msg.snapshot.Screen), protocolCursorToVTerm(msg.snapshot.Cursor), protocolModesToVTerm(msg.snapshot.Modes))
	pane.live = true
	pane.syncLost = false
	pane.recovering = false
	pane.droppedBytes = msg.droppedBytes
	pane.renderDirty = true
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
		snap, err := m.client.Snapshot(context.Background(), terminalID, 0, 200)
		if err != nil {
			return paneRecoveryFailedMsg{paneID: paneID, err: err}
		}
		return paneRecoveredMsg{
			paneID:       paneID,
			snapshot:     snap,
			droppedBytes: dropped,
		}
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
	pane.renderDirty = true
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
		return nil
	}
	rootRect := Rect{X: 0, Y: 0, W: max(1, m.width), H: max(1, m.height-2)}
	rects := tab.Root.Rects(rootRect)
	if tab.ZoomedPaneID != "" {
		if _, ok := rects[tab.ZoomedPaneID]; ok {
			rects = map[string]Rect{tab.ZoomedPaneID: rootRect}
		}
	}
	return rects
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
	if tab == nil || tab.Root == nil {
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
		if pane.Mode == ViewportModeFixed {
			_ = m.syncViewport(pane, int(cols), int(rows))
			tab.renderCache = nil
			m.invalidateRender()
			continue
		}
		cmds = append(cmds, func(channel uint16, cols, rows uint16) tea.Cmd {
			return func() tea.Msg {
				if err := m.client.Resize(context.Background(), channel, cols, rows); err != nil {
					return errMsg{err}
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
		return paneOutputMsg{paneID: paneID, frame: protocol.StreamFrame{Type: protocol.TypeClosed}}
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
		if err := m.client.Kill(context.Background(), pane.TerminalID); err != nil {
			return errMsg{err}
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
			if err := m.client.Kill(context.Background(), terminalID); err != nil {
				return errMsg{err}
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
		tab.Root = tab.Root.Remove(paneID)
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
	if pane.Readonly {
		data = filterReadonlyInput(data)
		if len(data) == 0 {
			return nil
		}
	}
	return func() tea.Msg {
		if err := m.client.Input(context.Background(), pane.Channel, data); err != nil {
			return errMsg{err}
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
	if m.prefixActive {
		parts = append(parts, "[prefix]")
	}
	if m.showHelp {
		parts = append(parts, "[help]")
	}
	if tab := m.currentTab(); tab != nil {
		if pane := tab.Panes[tab.ActivePaneID]; pane != nil {
			parts = append(parts, "pane:"+pane.TerminalID)
			parts = append(parts, viewportStatusParts(pane)...)
			if pane.syncLost || pane.recovering || pane.catchingUp {
				parts = append(parts, fmt.Sprintf("catching-up:%dB", pane.droppedBytes))
			}
		}
	}
	if m.err != nil {
		parts = append(parts, "err:"+m.err.Error())
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
	parts := []string{"mode:" + string(pane.Mode)}
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
		Name:         name,
		Panes:        make(map[string]*Pane),
		LayoutPreset: layoutPresetCustom,
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

func (m *Model) commitPrompt() {
	if m.prompt == nil {
		return
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
		"Ctrl-a M       toggle fit/fixed viewport mode",
		"Ctrl-a P       toggle viewport pin in fixed mode",
		"Ctrl-a R       toggle readonly viewport mode",
		"Ctrl-a Ctrl-h/j/k/l pan fixed viewport offset",
		"Ctrl-a x       close current viewport",
		"Ctrl-a X       kill current terminal",
		"Ctrl-a d       detach from TUI",
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
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInput(nil), tea.WithOutput(output))
	model.SetProgram(program)
	stopInput, restoreInput, err := startInputForwarder(program, input)
	if err != nil {
		return err
	}
	defer func() {
		_ = restoreInput()
	}()
	defer stopInput()
	defer model.StopRenderTicker()
	_, err = program.Run()
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
