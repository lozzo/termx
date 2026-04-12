package render

import (
	"strconv"
	"strings"
	"sync"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// Coordinator 负责 render invalidation / schedule / flush / ticker。
// 它通过 VisibleStateFn 拉取 workbench + runtime 的当前可见状态。
type Coordinator struct {
	visibleFn   VisibleStateFn
	mu          sync.Mutex
	dirty       bool
	lastFrame   string
	lastLines   []string
	lastCursor  string
	lastState   renderStateKey
	bodyCache   *bodyRenderCache
	tabBarValue string
	statusValue string
	tabBarKey   tabBarCacheKey
	statusKey   statusBarCacheKey

	cursorBlinkVisible bool
}

const CursorBlinkInterval = 600 * time.Millisecond

type VisibleStateFn func() VisibleRenderState

type renderedBody struct {
	content string
	cursor  string
	blink   bool
}

type tabBarCacheKey struct {
	Theme         uiTheme
	Width         int
	WorkspaceName string
	ActiveTab     int
	Error         string
	Notice        string
	Tabs          []tabBarCacheTab
}

type tabBarCacheTab struct {
	ID   string
	Name string
}

type statusBarCacheKey struct {
	Theme             uiTheme
	Width             int
	InputMode         string
	StatusHintsSig    string
	WorkspaceName     string
	WorkspaceCount    int
	TabCount          int
	ActiveTabID       string
	ActivePaneID      string
	ActiveTerminalID  string
	ActivePaneRole    string
	ActivePaneExited  bool
	ActiveIsFloating  bool
	FloatingTotal     int
	FloatingCollapsed int
	FloatingHidden    int
	TerminalCount     int
	SelectedTreeSig   string
}

type renderStateKey struct {
	Workbench                  *workbench.VisibleWorkbench
	Runtime                    *VisibleRuntimeStateProxy
	SurfaceKind                VisibleSurfaceKind
	SurfaceTerminalPool        *modal.TerminalManagerState
	OverlayKind                VisibleOverlayKind
	OverlayPrompt              *modal.PromptState
	OverlayPicker              *modal.PickerState
	OverlayWorkspacePicker     *modal.WorkspacePickerState
	OverlayTerminalManager     *modal.TerminalManagerState
	OverlayHelp                *modal.HelpState
	OverlayFloatingOverview    *modal.FloatingOverviewState
	TermSize                   TermSize
	Notice                     string
	Error                      string
	InputMode                  string
	OwnerConfirmPaneID         string
	EmptyPaneSelectionPaneID   string
	EmptyPaneSelectionIndex    int
	ExitedPaneSelectionPaneID  string
	ExitedPaneSelectionIndex   int
	PaneSnapshotOverridePaneID string
	PaneSnapshotOverride       *protocol.Snapshot
	CopyModePaneID             string
	CopyModeCursorRow          int
	CopyModeCursorCol          int
	CopyModeViewTopRow         int
	CopyModeMarkSet            bool
	CopyModeMarkRow            int
	CopyModeMarkCol            int
	CopyModeSnapshot           *protocol.Snapshot
}

type paneContentKey struct {
	TerminalID           string
	Snapshot             *protocol.Snapshot
	SurfaceVersion       uint64
	Name                 string
	State                string
	ThemeBG              string
	TerminalKnown        bool
	SharedLeft           bool
	SharedTop            bool
	ScrollOffset         int
	EmptyActionSelected  int
	ExitedActionSelected int
	ExitedActionPulse    bool
	CopyModeActive       bool
	CopyModeCursorRow    int
	CopyModeCursorCol    int
	CopyModeViewTopRow   int
	CopyModeMarkSet      bool
	CopyModeMarkRow      int
	CopyModeMarkCol      int
}

func NewCoordinator(fn VisibleStateFn) *Coordinator {
	return &Coordinator{
		visibleFn:          fn,
		dirty:              true,
		cursorBlinkVisible: true,
	}
}

func (c *Coordinator) Invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
}

func (c *Coordinator) RevealCursorBlink() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.cursorBlinkVisible = true
	c.dirty = true
	c.mu.Unlock()
}

func (c *Coordinator) Schedule()     {}
func (c *Coordinator) FlushPending() {}
func (c *Coordinator) StartTicker()  {}

func (c *Coordinator) RenderFrame() string {
	finish := perftrace.Measure("render.frame")
	frame := ""
	cacheMetric := "render.frame.cache_miss"
	defer func() {
		perftrace.Count(cacheMetric, len(frame))
		finish(len(frame))
	}()
	if c == nil || c.visibleFn == nil {
		return ""
	}
	state := c.visibleFn()
	key := stateKey(state)
	c.mu.Lock()
	if !c.dirty && c.lastFrame != "" && c.lastState == key {
		frame = c.lastFrame
		cacheMetric = "render.frame.cache_hit"
		c.mu.Unlock()
		return frame
	}
	c.mu.Unlock()
	if c != nil {
		c.mu.Lock()
		if !c.dirty && c.lastFrame == "" && len(c.lastLines) > 0 && c.lastState == key {
			frame = strings.Join(c.lastLines, "\n")
			c.lastFrame = frame
			cacheMetric = "render.frame.cache_hit"
			c.mu.Unlock()
			return frame
		}
		c.mu.Unlock()
	}
	if state.Workbench == nil {
		c.mu.Lock()
		c.lastFrame = "tuiv2"
		c.lastLines = []string{"tuiv2"}
		c.lastCursor = hideCursorANSI()
		c.lastState = key
		c.dirty = false
		frame = c.lastFrame
		c.mu.Unlock()
		return frame
	}

	immersiveZoom := immersiveZoomActive(state)
	bodyCursorOffsetY := TopChromeRows
	if immersiveZoom {
		bodyCursorOffsetY = 0
	}
	tabBar := ""
	statusBar := ""
	bodyHeight := FrameBodyHeight(state.TermSize.Height)
	if immersiveZoom {
		bodyHeight = maxInt(1, state.TermSize.Height)
	} else {
		tabBar = c.renderTabBarCached(state)
		statusBar = c.renderStatusBarCached(state)
	}
	rendered := renderBodyFrameWithCoordinator(c, state, state.TermSize.Width, bodyHeight)
	body := rendered.content
	cursor := rendered.cursor

	overlaySize := TermSize{Width: state.TermSize.Width, Height: bodyHeight}
	overlayCursorVisible := true
	c.mu.Lock()
	overlayCursorVisible = c.cursorBlinkVisible
	c.mu.Unlock()
	if overlay := renderActiveOverlayWithCursor(state, overlaySize, bodyCursorOffsetY, overlayCursorVisible); overlay.content != "" {
		body = compositeOverlay(body, overlay.content, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
		cursor = overlay.cursor
		rendered.blink = overlay.blink
	}
	frameParts := []string{body}
	if !immersiveZoom {
		frameParts = []string{tabBar, body, statusBar}
	}
	frame = strings.Join(frameParts, "\n")
	c.mu.Lock()
	if !rendered.blink {
		c.cursorBlinkVisible = true
	}
	c.lastFrame = frame
	c.lastLines = splitRenderedLines(frame, c.lastLines[:0])
	c.lastCursor = cursor
	c.lastState = key
	c.dirty = false
	frame = c.lastFrame
	c.mu.Unlock()
	return frame
}

func (c *Coordinator) RenderFrameLines() ([]string, string) {
	if c == nil || c.visibleFn == nil {
		return nil, hideCursorANSI()
	}
	state := c.visibleFn()
	key := stateKey(state)
	c.mu.Lock()
	if !c.dirty && len(c.lastLines) > 0 && c.lastState == key {
		lines := append([]string(nil), c.lastLines...)
		cursor := c.lastCursor
		if cursor == "" {
			cursor = hideCursorANSI()
		}
		c.mu.Unlock()
		return lines, cursor
	}
	c.mu.Unlock()
	lines, cursor := renderFrameLinesWithCoordinator(c, state)
	c.mu.Lock()
	c.lastLines = append(c.lastLines[:0], lines...)
	c.lastFrame = ""
	c.lastCursor = cursor
	c.lastState = key
	c.dirty = false
	c.mu.Unlock()
	return append([]string(nil), lines...), cursor
}

func stateKey(state VisibleRenderState) renderStateKey {
	return renderStateKey{
		Workbench:                  state.Workbench,
		Runtime:                    state.Runtime,
		SurfaceKind:                state.Surface.Kind,
		SurfaceTerminalPool:        state.Surface.TerminalPool,
		OverlayKind:                state.Overlay.Kind,
		OverlayPrompt:              state.Overlay.Prompt,
		OverlayPicker:              state.Overlay.Picker,
		OverlayWorkspacePicker:     state.Overlay.WorkspacePicker,
		OverlayTerminalManager:     state.Overlay.TerminalManager,
		OverlayHelp:                state.Overlay.Help,
		OverlayFloatingOverview:    state.Overlay.FloatingOverview,
		TermSize:                   state.TermSize,
		Notice:                     state.Notice,
		Error:                      state.Error,
		InputMode:                  state.InputMode,
		OwnerConfirmPaneID:         state.OwnerConfirmPaneID,
		EmptyPaneSelectionPaneID:   state.EmptyPaneSelectionPaneID,
		EmptyPaneSelectionIndex:    state.EmptyPaneSelectionIndex,
		ExitedPaneSelectionPaneID:  state.ExitedPaneSelectionPaneID,
		ExitedPaneSelectionIndex:   state.ExitedPaneSelectionIndex,
		PaneSnapshotOverridePaneID: state.PaneSnapshotOverridePaneID,
		PaneSnapshotOverride:       state.PaneSnapshotOverride,
		CopyModePaneID:             state.CopyModePaneID,
		CopyModeCursorRow:          state.CopyModeCursorRow,
		CopyModeCursorCol:          state.CopyModeCursorCol,
		CopyModeViewTopRow:         state.CopyModeViewTopRow,
		CopyModeMarkSet:            state.CopyModeMarkSet,
		CopyModeMarkRow:            state.CopyModeMarkRow,
		CopyModeMarkCol:            state.CopyModeMarkCol,
		CopyModeSnapshot:           state.CopyModeSnapshot,
	}
}

func (c *Coordinator) CursorSequence() string {
	if c == nil {
		return hideCursorANSI()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastCursor == "" {
		return hideCursorANSI()
	}
	return c.lastCursor
}

func (c *Coordinator) CachedFrameAndCursor() (string, string, bool) {
	if c == nil || c.visibleFn == nil {
		return "", hideCursorANSI(), false
	}
	state := c.visibleFn()
	key := stateKey(state)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty || c.lastFrame == "" || c.lastState != key {
		return "", "", false
	}
	cursor := c.lastCursor
	if cursor == "" {
		cursor = hideCursorANSI()
	}
	return c.lastFrame, cursor, true
}

func (c *Coordinator) CachedFrameLinesAndCursor() ([]string, string, bool) {
	if c == nil || c.visibleFn == nil {
		return nil, hideCursorANSI(), false
	}
	state := c.visibleFn()
	key := stateKey(state)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty || len(c.lastLines) == 0 || c.lastState != key {
		return nil, "", false
	}
	cursor := c.lastCursor
	if cursor == "" {
		cursor = hideCursorANSI()
	}
	return append([]string(nil), c.lastLines...), cursor, true
}

func (c *Coordinator) NeedsCursorTicks() bool {
	if c == nil || c.visibleFn == nil {
		return false
	}
	return visibleStateNeedsCursorBlink(c.visibleFn())
}

func (c *Coordinator) AdvanceCursorBlink() bool {
	if c == nil {
		return false
	}
	if !c.NeedsCursorTicks() {
		c.mu.Lock()
		c.cursorBlinkVisible = true
		c.mu.Unlock()
		return false
	}
	c.mu.Lock()
	c.cursorBlinkVisible = !c.cursorBlinkVisible
	c.dirty = true
	c.mu.Unlock()
	return true
}

func (c *Coordinator) syntheticCursorVisible(_ protocol.CursorState) bool {
	_ = c
	// Pane-local synthetic cursors stay steady. Reusing overlay blink state here
	// leaves them stranded in the hidden phase after overlays close.
	return true
}

func (c *Coordinator) renderTabBarCached(state VisibleRenderState) string {
	theme := uiThemeForState(state)
	c.mu.Lock()
	if c.tabBarKey.matches(state, theme) {
		value := c.tabBarValue
		c.mu.Unlock()
		return value
	}
	c.mu.Unlock()
	value := renderTabBar(state)
	c.mu.Lock()
	if c.tabBarValue == value {
		value = c.tabBarValue
	}
	c.tabBarValue = value
	c.tabBarKey.capture(state, theme)
	c.mu.Unlock()
	return value
}

func (c *Coordinator) renderStatusBarCached(state VisibleRenderState) string {
	theme := uiThemeForState(state)
	key := statusBarCacheKeyForState(state, theme)
	c.mu.Lock()
	if c.statusKey == key {
		value := c.statusValue
		c.mu.Unlock()
		return value
	}
	c.mu.Unlock()
	value := renderStatusBar(state)
	c.mu.Lock()
	if c.statusValue == value {
		value = c.statusValue
	}
	c.statusValue = value
	c.statusKey = key
	c.mu.Unlock()
	return value
}

func renderFrameLinesWithCoordinator(c *Coordinator, state VisibleRenderState) ([]string, string) {
	if state.Workbench == nil {
		return []string{"tuiv2"}, hideCursorANSI()
	}
	immersiveZoom := immersiveZoomActive(state)
	bodyCursorOffsetY := TopChromeRows
	if immersiveZoom {
		bodyCursorOffsetY = 0
	}
	bodyHeight := FrameBodyHeight(state.TermSize.Height)
	if immersiveZoom {
		bodyHeight = maxInt(1, state.TermSize.Height)
	}
	bodyLines, cursor, blink := renderBodyLinesWithCoordinator(c, state, state.TermSize.Width, bodyHeight)
	overlaySize := TermSize{Width: state.TermSize.Width, Height: bodyHeight}
	overlayCursorVisible := true
	c.mu.Lock()
	overlayCursorVisible = c.cursorBlinkVisible
	c.mu.Unlock()
	if overlay := renderActiveOverlayWithCursor(state, overlaySize, bodyCursorOffsetY, overlayCursorVisible); overlay.content != "" {
		body := compositeOverlay(strings.Join(bodyLines, "\n"), overlay.content, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
		bodyLines = strings.Split(body, "\n")
		cursor = overlay.cursor
		blink = overlay.blink
	}
	lines := make([]string, 0, len(bodyLines)+2)
	if !immersiveZoom {
		lines = append(lines, c.renderTabBarCached(state))
	}
	lines = append(lines, bodyLines...)
	if !immersiveZoom {
		lines = append(lines, c.renderStatusBarCached(state))
	}
	c.mu.Lock()
	if !blink {
		c.cursorBlinkVisible = true
	}
	c.mu.Unlock()
	return lines, cursor
}

func renderBodyLinesWithCoordinator(coordinator *Coordinator, state VisibleRenderState, width, height int) ([]string, string, bool) {
	if width <= 0 || height <= 0 {
		return nil, hideCursorANSI(), false
	}
	cursorOffsetY := TopChromeRows
	if immersiveZoomActive(state) {
		cursorOffsetY = 0
	}
	if state.Surface.Kind == VisibleSurfaceTerminalPool && state.Surface.TerminalPool != nil {
		cursorVisible := true
		if coordinator != nil {
			coordinator.mu.Lock()
			cursorVisible = coordinator.cursorBlinkVisible
			coordinator.mu.Unlock()
		}
		rendered := renderTerminalPoolPageWithCursor(state.Surface.TerminalPool, state.Runtime, TermSize{Width: width, Height: height}, cursorOffsetY, cursorVisible)
		return strings.Split(rendered.content, "\n"), rendered.cursor, rendered.blink
	}
	if state.Workbench == nil {
		rendered := renderedBody{content: strings.Repeat("\n", maxInt(0, height-1))}
		return strings.Split(rendered.content, "\n"), rendered.cursor, rendered.blink
	}
	activeTabIdx := state.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(state.Workbench.Tabs) {
		rendered := renderEmptyWorkbenchBody(state, width, height, emptyWorkbenchNoTabs)
		return strings.Split(rendered.content, "\n"), rendered.cursor, rendered.blink
	}
	tab := state.Workbench.Tabs[activeTabIdx]
	if len(tab.Panes) == 0 {
		rendered := renderEmptyWorkbenchBody(state, width, height, emptyWorkbenchNoPanes)
		return strings.Split(rendered.content, "\n"), rendered.cursor, rendered.blink
	}
	lookup := newRuntimeLookup(state.Runtime)
	exitedSelectionPulse := true
	if coordinator != nil {
		coordinator.mu.Lock()
		exitedSelectionPulse = coordinator.cursorBlinkVisible
		coordinator.mu.Unlock()
	}
	entries := paneEntriesForTab(tab, state.Workbench.FloatingPanes, width, height, lookup, state.OwnerConfirmPaneID, state.EmptyPaneSelectionPaneID, state.EmptyPaneSelectionIndex, state.ExitedPaneSelectionPaneID, state.ExitedPaneSelectionIndex, exitedSelectionPulse, state, uiThemeForRuntime(state.Runtime))
	canvas := renderBodyCanvas(coordinator, state, entries, width, height)
	return canvas.contentLines(), canvas.cursorANSI(), canvas.syntheticCursorBlink
}

func (k tabBarCacheKey) matches(state VisibleRenderState, theme uiTheme) bool {
	if k.Theme != theme || k.Width != state.TermSize.Width || k.Error != state.Error || k.Notice != state.Notice {
		return false
	}
	if state.Workbench == nil {
		return k.WorkspaceName == "" && k.ActiveTab == -1 && len(k.Tabs) == 0
	}
	if k.WorkspaceName != state.Workbench.WorkspaceName || k.ActiveTab != state.Workbench.ActiveTab || len(k.Tabs) != len(state.Workbench.Tabs) {
		return false
	}
	for i := range state.Workbench.Tabs {
		tab := state.Workbench.Tabs[i]
		if k.Tabs[i].ID != tab.ID || k.Tabs[i].Name != tab.Name {
			return false
		}
	}
	return true
}

func (k *tabBarCacheKey) capture(state VisibleRenderState, theme uiTheme) {
	if k == nil {
		return
	}
	k.Theme = theme
	k.Width = state.TermSize.Width
	k.Error = state.Error
	k.Notice = state.Notice
	k.WorkspaceName = ""
	k.ActiveTab = -1
	if state.Workbench == nil {
		k.Tabs = k.Tabs[:0]
		return
	}
	k.WorkspaceName = state.Workbench.WorkspaceName
	k.ActiveTab = state.Workbench.ActiveTab
	if cap(k.Tabs) < len(state.Workbench.Tabs) {
		k.Tabs = make([]tabBarCacheTab, len(state.Workbench.Tabs))
	} else {
		k.Tabs = k.Tabs[:len(state.Workbench.Tabs)]
	}
	for i, tab := range state.Workbench.Tabs {
		k.Tabs[i] = tabBarCacheTab{ID: tab.ID, Name: tab.Name}
	}
}

func statusBarCacheKeyForState(state VisibleRenderState, theme uiTheme) statusBarCacheKey {
	key := statusBarCacheKey{
		Theme:          theme,
		Width:          state.TermSize.Width,
		InputMode:      strings.TrimSpace(state.InputMode),
		StatusHintsSig: strings.Join(state.StatusHints, "\x1f"),
		WorkspaceName:  "",
	}
	if state.Workbench != nil {
		key.WorkspaceName = state.Workbench.WorkspaceName
		key.WorkspaceCount = state.Workbench.WorkspaceCount
		key.TabCount = len(state.Workbench.Tabs)
		key.FloatingTotal = state.Workbench.FloatingTotal
		key.FloatingCollapsed = state.Workbench.FloatingCollapsed
		key.FloatingHidden = state.Workbench.FloatingHidden
		if state.Workbench.ActiveTab >= 0 && state.Workbench.ActiveTab < len(state.Workbench.Tabs) {
			tab := state.Workbench.Tabs[state.Workbench.ActiveTab]
			key.ActiveTabID = tab.ID
			key.ActivePaneID = tab.ActivePaneID
			for i := range state.Workbench.FloatingPanes {
				if state.Workbench.FloatingPanes[i].ID == key.ActivePaneID {
					key.ActiveIsFloating = true
					key.ActiveTerminalID = state.Workbench.FloatingPanes[i].TerminalID
					break
				}
			}
			if key.ActiveTerminalID == "" {
				for i := range tab.Panes {
					if tab.Panes[i].ID == key.ActivePaneID {
						key.ActiveTerminalID = tab.Panes[i].TerminalID
						break
					}
				}
			}
		}
	}
	if state.Runtime != nil {
		key.TerminalCount = len(state.Runtime.Terminals)
	}
	if key.ActivePaneID != "" {
		role, exited, floating := statusBarActivePaneState(state)
		key.ActivePaneRole = role
		key.ActivePaneExited = exited
		key.ActiveIsFloating = floating
	}
	if state.Overlay.Kind == VisibleOverlayWorkspacePicker && state.Overlay.WorkspacePicker != nil {
		key.SelectedTreeSig = statusBarSelectedTreeSignature(state.Overlay.WorkspacePicker.SelectedItem())
	}
	return key
}

func statusBarActivePaneState(state VisibleRenderState) (role string, exited bool, floating bool) {
	if state.Workbench == nil || state.Workbench.ActiveTab < 0 || state.Workbench.ActiveTab >= len(state.Workbench.Tabs) {
		return "", false, false
	}
	tab := state.Workbench.Tabs[state.Workbench.ActiveTab]
	activePaneID := strings.TrimSpace(tab.ActivePaneID)
	if activePaneID == "" {
		return "", false, false
	}
	var terminalID string
	for i := range state.Workbench.FloatingPanes {
		if state.Workbench.FloatingPanes[i].ID == activePaneID {
			floating = true
			terminalID = state.Workbench.FloatingPanes[i].TerminalID
			break
		}
	}
	if terminalID == "" {
		for i := range tab.Panes {
			if tab.Panes[i].ID == activePaneID {
				terminalID = tab.Panes[i].TerminalID
				break
			}
		}
	}
	if state.Runtime != nil {
		for _, binding := range state.Runtime.Bindings {
			if binding.PaneID == activePaneID {
				role = binding.Role
				break
			}
		}
		if terminalID != "" {
			for _, terminal := range state.Runtime.Terminals {
				if terminal.TerminalID == terminalID {
					exited = terminal.State == "exited"
					break
				}
			}
		}
	}
	return role, exited, floating
}

func statusBarSelectedTreeSignature(item *modal.WorkspacePickerItem) string {
	if item == nil {
		return ""
	}
	parts := []string{
		string(item.Kind),
		item.Name,
		item.WorkspaceName,
		item.TabID,
		strconv.Itoa(item.TabIndex),
		item.PaneID,
		item.State,
		item.Role,
		strconv.FormatBool(item.CreateNew),
		item.CreateName,
		strconv.FormatBool(item.Current),
		strconv.FormatBool(item.Active),
		strconv.FormatBool(item.Floating),
		strconv.Itoa(item.TabCount),
		strconv.Itoa(item.PaneCount),
		strconv.Itoa(item.FloatingCount),
	}
	return strings.Join(parts, "|")
}

func renderBody(state VisibleRenderState, width, height int) string {
	return renderBodyFrameWithCoordinator(nil, state, width, height).content
}

func splitRenderedLines(frame string, dst []string) []string {
	dst = dst[:0]
	start := 0
	for i := 0; i < len(frame); i++ {
		if frame[i] != '\n' {
			continue
		}
		dst = append(dst, frame[start:i])
		start = i + 1
	}
	return append(dst, frame[start:])
}

func renderBodyFrame(state VisibleRenderState, width, height int) renderedBody {
	return renderBodyFrameWithCoordinator(nil, state, width, height)
}

func renderBodyFrameWithCoordinator(coordinator *Coordinator, state VisibleRenderState, width, height int) renderedBody {
	finish := perftrace.Measure("render.body")
	defer finish(0)
	if width <= 0 || height <= 0 {
		return renderedBody{}
	}
	cursorOffsetY := TopChromeRows
	if immersiveZoomActive(state) {
		cursorOffsetY = 0
	}
	if state.Surface.Kind == VisibleSurfaceTerminalPool && state.Surface.TerminalPool != nil {
		cursorVisible := true
		if coordinator != nil {
			coordinator.mu.Lock()
			cursorVisible = coordinator.cursorBlinkVisible
			coordinator.mu.Unlock()
		}
		return renderTerminalPoolPageWithCursor(state.Surface.TerminalPool, state.Runtime, TermSize{Width: width, Height: height}, cursorOffsetY, cursorVisible)
	}
	if state.Workbench == nil {
		return renderedBody{content: strings.Repeat("\n", maxInt(0, height-1))}
	}

	activeTabIdx := state.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(state.Workbench.Tabs) {
		return renderEmptyWorkbenchBody(state, width, height, emptyWorkbenchNoTabs)
	}
	tab := state.Workbench.Tabs[activeTabIdx]
	if len(tab.Panes) == 0 {
		return renderEmptyWorkbenchBody(state, width, height, emptyWorkbenchNoPanes)
	}
	lookup := newRuntimeLookup(state.Runtime)
	exitedSelectionPulse := true
	if coordinator != nil {
		coordinator.mu.Lock()
		exitedSelectionPulse = coordinator.cursorBlinkVisible
		coordinator.mu.Unlock()
	}
	entries := paneEntriesForTab(tab, state.Workbench.FloatingPanes, width, height, lookup, state.OwnerConfirmPaneID, state.EmptyPaneSelectionPaneID, state.EmptyPaneSelectionIndex, state.ExitedPaneSelectionPaneID, state.ExitedPaneSelectionIndex, exitedSelectionPulse, state, uiThemeForRuntime(state.Runtime))

	canvas := renderBodyCanvas(coordinator, state, entries, width, height)
	return renderedBody{
		content: canvas.contentString(),
		cursor:  canvas.cursorANSI(),
		blink:   canvas.syntheticCursorBlink,
	}
}

func visibleStateNeedsCursorBlink(state VisibleRenderState) bool {
	if overlayNeedsCursorBlink(state.Overlay) || terminalPoolNeedsCursorBlink(state.Surface) {
		return true
	}
	if state.Overlay.Kind != VisibleOverlayNone || state.Surface.Kind != VisibleSurfaceWorkbench {
		return false
	}
	if state.Workbench == nil || state.Runtime == nil {
		return false
	}
	activeTabIdx := state.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(state.Workbench.Tabs) {
		return false
	}
	tab := state.Workbench.Tabs[activeTabIdx]
	if len(tab.Panes) == 0 {
		return false
	}
	width := state.TermSize.Width
	height := FrameBodyHeight(state.TermSize.Height)
	if immersiveZoomActive(state) {
		height = maxInt(1, state.TermSize.Height)
	}
	if width <= 0 || height <= 0 {
		return false
	}
	if activeExitedPaneHasRecoverySelection(state) {
		return true
	}
	// Keep the in-pane synthetic cursor steady. Blinking it forces full-frame
	// redraws every tick, which causes visible footer/status-bar shimmer in
	// some host terminals even though only the pane cursor changes.
	return false
}

func overlayNeedsCursorBlink(overlay VisibleOverlay) bool {
	switch overlay.Kind {
	case VisibleOverlayPrompt, VisibleOverlayPicker, VisibleOverlayWorkspacePicker, VisibleOverlayTerminalManager:
		return true
	default:
		return false
	}
}

func terminalPoolNeedsCursorBlink(surface VisibleSurface) bool {
	return surface.Kind == VisibleSurfaceTerminalPool && surface.TerminalPool != nil
}

func activeExitedPaneHasRecoverySelection(state VisibleRenderState) bool {
	if state.Workbench == nil || state.Runtime == nil || state.ExitedPaneSelectionPaneID == "" {
		return false
	}
	activeTabIdx := state.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(state.Workbench.Tabs) {
		return false
	}
	tab := state.Workbench.Tabs[activeTabIdx]
	if tab.ActivePaneID == "" || tab.ActivePaneID != state.ExitedPaneSelectionPaneID {
		return false
	}
	for i := range tab.Panes {
		pane := &tab.Panes[i]
		if pane.ID != tab.ActivePaneID || pane.TerminalID == "" {
			continue
		}
		terminal := findVisibleTerminal(state.Runtime, pane.TerminalID)
		return terminal != nil && terminal.State == "exited"
	}
	return false
}

type emptyWorkbenchKind uint8

const (
	emptyWorkbenchNoTabs emptyWorkbenchKind = iota
	emptyWorkbenchNoPanes
)

func renderEmptyWorkbenchBody(state VisibleRenderState, width, height int, kind emptyWorkbenchKind) renderedBody {
	canvas := newComposedCanvas(width, height)
	canvas.hostEmojiVS16Mode = emojiVariationSelectorModeForRuntime(state.Runtime)
	theme := uiThemeForState(state)

	headline := "No tabs in this workspace"
	details := []string{
		"Ctrl-F open terminal picker",
		"Ctrl-T then c create a new tab",
	}
	if kind == emptyWorkbenchNoPanes {
		headline = "No panes in this tab"
		details = []string{
			"Ctrl-F create the first pane via terminal picker",
			"Ctrl-T then c create a fresh tab",
		}
	}

	lines := append([]string{headline}, details...)
	startY := maxInt(0, (height-len(lines))/2)
	for i, line := range lines {
		y := startY + i
		if y >= height {
			break
		}
		text := centerText(xansi.Truncate(line, width, ""), width)
		style := drawStyle{FG: theme.panelMuted}
		if i == 0 {
			style = drawStyle{FG: theme.panelText, Bold: true}
		}
		canvas.drawText(0, y, text, style)
	}

	return renderedBody{
		content: canvas.contentString(),
		cursor:  hideCursorANSI(),
	}
}

func emojiVariationSelectorModeForRuntime(runtimeState *VisibleRuntimeStateProxy) shared.AmbiguousEmojiVariationSelectorMode {
	if runtimeState == nil {
		return shared.AmbiguousEmojiVariationSelectorStrip
	}
	switch runtimeState.HostEmojiVS16Mode {
	case shared.AmbiguousEmojiVariationSelectorRaw:
		return shared.AmbiguousEmojiVariationSelectorRaw
	case shared.AmbiguousEmojiVariationSelectorAdvance, shared.AmbiguousEmojiVariationSelectorStrip:
		// 中文说明：即便探测分类里有 advance，真正进入 Bubble Tea 行渲染时
		// 也要合并到 strip，因为这里不能安全依赖行内光标移动来补齐宽度。
		return shared.AmbiguousEmojiVariationSelectorStrip
	default:
		return shared.AmbiguousEmojiVariationSelectorStrip
	}
}

func renderPageWithPinnedFooter(headerLines, contentLines []string, footerLine string, width, height int) string {
	if height <= 0 {
		return ""
	}
	if height == 1 {
		return forceWidthANSIOverlay(footerLine, width)
	}

	lines := make([]string, 0, height)
	lines = append(lines, headerLines...)
	lines = append(lines, contentLines...)
	if len(lines) > height-1 {
		lines = lines[:height-1]
	}
	for len(lines) < height-1 {
		lines = append(lines, forceWidthANSIOverlay("", width))
	}
	lines = append(lines, forceWidthANSIOverlay(footerLine, width))
	return strings.Join(lines, "\n")
}

// resolvePaneTitle returns the stable title for a pane, preferring terminal
// metadata name, then the persisted pane title. OSC/program titles are not used
// for pane chrome because the chrome should stay anchored to the terminal name.

func immersiveZoomActive(state VisibleRenderState) bool {
	if state.Surface.Kind != VisibleSurfaceWorkbench || state.Workbench == nil {
		return false
	}
	activeTab := state.Workbench.ActiveTab
	if activeTab < 0 || activeTab >= len(state.Workbench.Tabs) {
		return false
	}
	return strings.TrimSpace(state.Workbench.Tabs[activeTab].ZoomedPaneID) != ""
}

func copyModeTimestampLabel(snapshot *protocol.Snapshot, row int) string {
	ts := snapshotRowTimestamp(snapshot, row)
	if ts.IsZero() {
		return ""
	}
	return formatSnapshotRowTimestamp(ts)
}

func copyModeRowPositionLabel(snapshot *protocol.Snapshot, row int) string {
	totalRows := snapshotTotalRows(snapshot)
	if totalRows <= 0 || row < 0 || row >= totalRows {
		return ""
	}
	return strconv.Itoa(row+1) + "/" + strconv.Itoa(totalRows)
}

func formatSnapshotRowTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Local().Format("2006-01-02 15:04:05")
}

func snapshotRowTimestamp(snapshot *protocol.Snapshot, row int) time.Time {
	if snapshot == nil || row < 0 {
		return time.Time{}
	}
	if row < len(snapshot.Scrollback) {
		if row < len(snapshot.ScrollbackTimestamps) {
			return snapshot.ScrollbackTimestamps[row]
		}
		return time.Time{}
	}
	row -= len(snapshot.Scrollback)
	if row < 0 || row >= len(snapshot.Screen.Cells) {
		return time.Time{}
	}
	if row < len(snapshot.ScreenTimestamps) {
		return snapshot.ScreenTimestamps[row]
	}
	return time.Time{}
}

func snapshotRowKind(snapshot *protocol.Snapshot, row int) string {
	if snapshot == nil || row < 0 {
		return ""
	}
	if row < len(snapshot.Scrollback) {
		if row < len(snapshot.ScrollbackRowKinds) {
			return snapshot.ScrollbackRowKinds[row]
		}
		return ""
	}
	row -= len(snapshot.Scrollback)
	if row < 0 || row >= len(snapshot.Screen.Cells) {
		return ""
	}
	if row < len(snapshot.ScreenRowKinds) {
		return snapshot.ScreenRowKinds[row]
	}
	return ""
}

func snapshotTotalRows(snapshot *protocol.Snapshot) int {
	if snapshot == nil {
		return 0
	}
	return len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
}

func applyScrollbackOffset(snapshot *protocol.Snapshot, offset int, height int) *protocol.Snapshot {
	if snapshot == nil || offset <= 0 || height <= 0 {
		return snapshot
	}
	rows := make([][]protocol.Cell, 0, len(snapshot.Scrollback)+len(snapshot.Screen.Cells))
	rows = append(rows, snapshot.Scrollback...)
	rows = append(rows, snapshot.Screen.Cells...)
	if len(rows) == 0 {
		return snapshot
	}
	end := len(rows) - offset
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	window := rows[start:end]
	cells := make([][]protocol.Cell, 0, len(window))
	for _, row := range window {
		cells = append(cells, append([]protocol.Cell(nil), row...))
	}
	cloned := *snapshot
	cloned.Screen = protocol.ScreenData{
		Cells:             cells,
		IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
	}
	return &cloned
}

func drawSnapshotWithOffset(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, offset int, theme uiTheme) {
	drawTerminalSourceWithOffset(canvas, rect, renderSource(snapshot, nil), offset, theme)
}

func drawTerminalSourceWithOffset(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, offset int, theme uiTheme) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if offset <= 0 {
		drawTerminalSourceInRect(canvas, rect, source)
		drawTerminalExtentHints(canvas, rect, source, theme)
		return
	}
	totalRows := source.TotalRows()
	if totalRows == 0 {
		drawTerminalExtentHints(canvas, rect, source, theme)
		return
	}
	end := totalRows - offset
	if end < 0 {
		end = 0
	}
	start := end - rect.H
	if start < 0 {
		start = 0
	}
	targetY := rect.Y
	for rowIndex := start; rowIndex < end && targetY < rect.Y+rect.H; rowIndex++ {
		drawTerminalSourceRowInRect(canvas, rect, source, rowIndex, targetY, theme)
		targetY++
	}
	drawTerminalExtentHints(canvas, rect, terminalExtentHintsView(source, totalRows), theme)
}

func drawSnapshotRowInRect(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, rowIndex int, targetY int, theme uiTheme) {
	drawTerminalSourceRowInRect(canvas, rect, renderSource(snapshot, nil), rowIndex, targetY, theme)
}

func drawTerminalSourceInRect(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	base := source.ScrollbackRows()
	for y := 0; y < rect.H && y < source.ScreenRows(); y++ {
		canvas.drawProtocolRowInRect(rect, rect.Y+y, source.Row(base+y))
	}
}

func drawTerminalSourceRowInRect(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, rowIndex int, targetY int, theme uiTheme) {
	if source == nil {
		return
	}
	if kind := source.RowKind(rowIndex); kind != "" {
		if drawSnapshotMarkerRow(canvas, rect, targetY, kind, source.RowTimestamp(rowIndex), theme) {
			return
		}
	}
	canvas.drawProtocolRowInRect(rect, targetY, source.Row(rowIndex))
}

func drawSnapshotMarkerRow(canvas *composedCanvas, rect workbench.Rect, targetY int, kind string, ts time.Time, theme uiTheme) bool {
	if canvas == nil || rect.W <= 0 {
		return false
	}
	label := snapshotMarkerLabel(kind, ts)
	if strings.TrimSpace(label) == "" {
		return false
	}
	canvas.drawText(rect.X, targetY, centerText(label, rect.W), drawStyle{FG: theme.panelMuted})
	return true
}

func snapshotMarkerLabel(kind string, ts time.Time) string {
	switch kind {
	case protocol.SnapshotRowKindRestart:
		label := "[ restarted ]"
		if formatted := formatSnapshotRowTimestamp(ts); formatted != "" {
			label = "[ restarted " + formatted + " ]"
		}
		return label
	default:
		return ""
	}
}

func snapshotExtentHintsView(snapshot *protocol.Snapshot, rows int) *protocol.Snapshot {
	if snapshot == nil || rows <= 0 {
		return snapshot
	}
	if int(snapshot.Size.Rows) >= rows {
		return snapshot
	}
	cloned := *snapshot
	if rows > int(^uint16(0)) {
		rows = int(^uint16(0))
	}
	cloned.Size.Rows = uint16(rows)
	return &cloned
}

func terminalExtentHintsView(source terminalRenderSource, rows int) terminalRenderSource {
	if source == nil || rows <= 0 {
		return source
	}
	if size := source.Size(); int(size.Rows) >= rows {
		return source
	}
	switch typed := source.(type) {
	case snapshotRenderSource:
		return renderSource(snapshotExtentHintsView(typed.snapshot, rows), nil)
	default:
		return source
	}
}

func drawSnapshotExtentHints(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, theme uiTheme) {
	drawTerminalExtentHints(canvas, rect, renderSource(snapshot, nil), theme)
}

func drawTerminalExtentHints(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, theme uiTheme) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	metrics := terminalMetricsForSource(source)
	if metrics.Cols <= 0 || metrics.Rows <= 0 {
		return
	}

	dotStyle := drawStyle{FG: theme.panelBorder}

	visibleCols := minInt(rect.W, metrics.Cols)
	visibleRows := minInt(rect.H, metrics.Rows)

	if metrics.Cols < rect.W {
		startX := rect.X + visibleCols
		endX := rect.X + rect.W
		for y := rect.Y; y < rect.Y+visibleRows; y++ {
			for x := startX; x < endX; x++ {
				canvas.set(x, y, drawCell{Content: "·", Width: 1, Style: dotStyle})
			}
		}
	}
	if metrics.Rows < rect.H {
		startY := rect.Y + visibleRows
		endY := rect.Y + rect.H
		for y := startY; y < endY; y++ {
			for x := rect.X; x < rect.X+rect.W; x++ {
				canvas.set(x, y, drawCell{Content: "·", Width: 1, Style: dotStyle})
			}
		}
	}
}

func renderTerminalMetricsForSnapshot(snapshot *protocol.Snapshot) renderTerminalMetrics {
	return terminalMetricsForSource(renderSource(snapshot, nil))
}

func drawCopyModeOverlay(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, theme uiTheme, cursorRow, cursorCol, viewTopRow int, markSet bool, markRow, markCol int) {
	if canvas == nil || snapshot == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows <= 0 {
		return
	}
	cursorRow, cursorCol = clampCopyPoint(snapshot, cursorRow, cursorCol)
	selectionStartRow, selectionStartCol := markRow, markCol
	selectionEndRow, selectionEndCol := cursorRow, cursorCol
	if markSet {
		selectionStartRow, selectionStartCol = clampCopyPoint(snapshot, selectionStartRow, selectionStartCol)
		selectionEndRow, selectionEndCol = clampCopyPoint(snapshot, selectionEndRow, selectionEndCol)
		if selectionStartRow > selectionEndRow || (selectionStartRow == selectionEndRow && selectionStartCol > selectionEndCol) {
			selectionStartRow, selectionEndRow = selectionEndRow, selectionStartRow
			selectionStartCol, selectionEndCol = selectionEndCol, selectionStartCol
		}
	}
	start := clampCopyViewportTop(snapshot, rect.H, viewTopRow)
	selectionBG := ensureContrast(mixHex(theme.info, theme.chromeAccent, 0.35), theme.hostBG, 1.2)
	cursorBG := ensureContrast(theme.warning, theme.hostBG, 1.2)
	for visibleRow := 0; visibleRow < rect.H; visibleRow++ {
		rowIndex := start + visibleRow
		if rowIndex < 0 || rowIndex >= totalRows {
			continue
		}
		if markSet && rowIndex >= selectionStartRow && rowIndex <= selectionEndRow {
			firstCol := 0
			lastCol := rowMaxCol(snapshot, rowIndex)
			if rowIndex == selectionStartRow {
				firstCol = selectionStartCol
			}
			if rowIndex == selectionEndRow {
				lastCol = selectionEndCol
			}
			for col := firstCol; col <= lastCol; col++ {
				drawCopyModeCellHighlight(canvas, rect.X+col, rect.Y+visibleRow, selectionBG)
			}
		}
	}
	screenRow := cursorRow - start
	if screenRow >= 0 && screenRow < rect.H {
		drawCopyModeCellHighlight(canvas, rect.X+cursorCol, rect.Y+screenRow, cursorBG)
	}
}

func drawCopyModeCellHighlight(canvas *composedCanvas, x, y int, bg string) {
	if canvas == nil || x < 0 || y < 0 || x >= canvas.width || y >= canvas.height {
		return
	}
	leadX := x
	for leadX > 0 && canvas.cells[y][leadX].Continuation {
		leadX--
	}
	cell := canvas.cells[y][leadX]
	if cell.Continuation {
		cell = blankDrawCell()
	}
	if cell.Content == "" {
		cell = blankDrawCell()
	}
	style := cell.Style
	style.BG = bg
	style.FG = contrastTextColor(bg)
	cell.Style = style
	canvas.set(leadX, y, cell)
}

func clampCopyViewportTop(snapshot *protocol.Snapshot, height, viewTopRow int) int {
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows <= 0 {
		return 0
	}
	maxTop := maxInt(0, totalRows-maxInt(1, height))
	if viewTopRow < 0 {
		viewTopRow = 0
	}
	if viewTopRow > maxTop {
		viewTopRow = maxTop
	}
	return viewTopRow
}

func scrollOffsetForViewportTop(snapshot *protocol.Snapshot, height, viewTopRow int) int {
	if snapshot == nil {
		return 0
	}
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	viewTopRow = clampCopyViewportTop(snapshot, height, viewTopRow)
	offset := totalRows - (viewTopRow + maxInt(1, height))
	if offset < 0 {
		offset = 0
	}
	if viewTopRow < len(snapshot.Scrollback) && offset == 0 && len(snapshot.Scrollback) > 0 {
		offset = 1
	}
	return offset
}

func snapshotRow(snapshot *protocol.Snapshot, rowIndex int) []protocol.Cell {
	if snapshot == nil || rowIndex < 0 {
		return nil
	}
	if rowIndex < len(snapshot.Scrollback) {
		return snapshot.Scrollback[rowIndex]
	}
	rowIndex -= len(snapshot.Scrollback)
	if rowIndex < 0 || rowIndex >= len(snapshot.Screen.Cells) {
		return nil
	}
	return snapshot.Screen.Cells[rowIndex]
}

func rowMaxCol(snapshot *protocol.Snapshot, rowIndex int) int {
	row := snapshotRow(snapshot, rowIndex)
	if len(row) > 0 {
		return len(row) - 1
	}
	if snapshot == nil || snapshot.Size.Cols == 0 {
		return 0
	}
	return int(snapshot.Size.Cols) - 1
}

func clampCopyPoint(snapshot *protocol.Snapshot, row, col int) (int, int) {
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows <= 0 {
		return 0, 0
	}
	if row < 0 {
		row = 0
	}
	if row >= totalRows {
		row = totalRows - 1
	}
	maxCol := rowMaxCol(snapshot, row)
	if col < 0 {
		col = 0
	}
	if col > maxCol {
		col = maxCol
	}
	rowCells := snapshotRow(snapshot, row)
	for col > 0 && col < len(rowCells) && rowCells[col].Content == "" && rowCells[col].Width == 0 {
		col--
	}
	return row, col
}

func entriesOverlap(entries []paneRenderEntry) bool {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if rectsOverlap(entries[i].Rect, entries[j].Rect) {
				return true
			}
		}
	}
	return false
}

func overlapsAnyRect(rect workbench.Rect, others []workbench.Rect) bool {
	for _, other := range others {
		if rectsOverlap(rect, other) {
			return true
		}
	}
	return false
}

func rectsOverlap(a, b workbench.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}
