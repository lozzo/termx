package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/state/layout"
	"github.com/lozzow/termx/tui/state/pool"
	stateterminal "github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

var (
	IntentSplitVertical            = SplitPaneIntent{Direction: types.SplitDirectionVertical}
	IntentNewTab                   = NewTabIntent{}
	IntentNewFloat                 = NewFloatIntent{}
	IntentCancelOverlay            = CancelOverlayIntent{}
	IntentClosePane                = ClosePaneIntent{}
	IntentDisconnectPane           = DisconnectPaneIntent{}
	IntentClosePaneAndKillTerminal = ClosePaneAndKillTerminalIntent{}
	IntentOpenHelp                 = OpenHelpIntent{}
)

type Intent interface{}

type SplitPaneIntent struct {
	Direction types.SplitDirection
}

type NewTabIntent struct{}
type NewFloatIntent struct{}
type CancelOverlayIntent struct{}
type OpenReconnectIntent struct{}
type ConfirmReconnectIntent struct {
	TerminalID types.TerminalID
}
type ConfirmConnectExistingIntent struct {
	TerminalID types.TerminalID
}
type ConfirmCreateTerminalIntent struct {
	Command []string
	Name    string
}
type ClosePaneIntent struct{}
type DisconnectPaneIntent struct{}
type ClosePaneAndKillTerminalIntent struct{}
type RemoveTerminalIntent struct {
	TerminalID types.TerminalID
	Visible    bool
	Name       string
}
type RestartTerminalIntent struct {
	TerminalID types.TerminalID
}
type BecomeOwnerIntent struct {
	TerminalID types.TerminalID
}
type OpenHelpIntent struct{}
type MoveFloatingPaneIntent struct {
	PaneID types.PaneID
	DeltaX int
	DeltaY int
}
type CenterFloatingPaneIntent struct {
	PaneID types.PaneID
}

// Apply 是本阶段 workbench reducer 的统一入口。
// 这里保持纯状态变换，便于把 create/kill 等副作用继续下放给 runtime。
func (m Model) Apply(intent Intent) Model {
	next := m.clone()
	next.ensureWorkspace()
	next.PendingEffects = nil

	switch in := intent.(type) {
	case SplitPaneIntent:
		return next.applySplitPane(in.Direction)
	case NewTabIntent:
		return next.applyNewTab()
	case NewFloatIntent:
		return next.applyNewFloat()
	case CancelOverlayIntent:
		next.Overlay = next.Overlay.Clear()
		return next
	case ConfirmCreateTerminalIntent:
		return next.applyCreateTerminal(in)
	case ConfirmConnectExistingIntent:
		return next.applyConnectExisting(in.TerminalID)
	case ClosePaneIntent:
		return next.applyClosePane()
	case DisconnectPaneIntent:
		return next.applyDisconnectPane()
	case OpenReconnectIntent:
		return next.openConnectOverlay(ConnectTargetReconnect)
	case ConfirmReconnectIntent:
		return next.applyReconnect(in.TerminalID)
	case ClosePaneAndKillTerminalIntent:
		return next.applyKillActiveTerminal()
	case RemoveTerminalIntent:
		return next.applyRemoveTerminal(in)
	case RestartTerminalIntent:
		return next.applyRestartTerminal(in.TerminalID)
	case BecomeOwnerIntent:
		return next.applyBecomeOwner(in.TerminalID)
	case CreateTerminalSucceededIntent:
		return next.applyCreateTerminalSucceeded(in)
	case KillTerminalSucceededIntent:
		return next.applyKillTerminalSucceeded(in.TerminalID)
	case OpenHelpIntent:
		next.Overlay = next.Overlay.Replace(OverlayState{
			Kind: OverlayHelp,
			Help: DefaultHelpOverlay(),
		})
		return next
	case MoveFloatingPaneIntent:
		return next.applyMoveFloatingPane(in)
	case CenterFloatingPaneIntent:
		return next.applyCenterFloatingPane(in.PaneID)
	default:
		return next
	}
}

func (m Model) applySplitPane(direction types.SplitDirection) Model {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	current, ok := tab.ActivePane()
	if !ok {
		return m
	}
	newPaneID := nextPaneID(m.Workspace)
	if tab.Layout == nil {
		tab.Layout = layout.NewLeaf(current.ID)
	}
	_ = tab.Layout.Split(current.ID, direction, newPaneID)
	tab.TrackPane(workspace.PaneState{
		ID:        newPaneID,
		Kind:      types.PaneKindTiled,
		SlotState: types.PaneSlotUnconnected,
	})
	tab.ActivePaneID = newPaneID
	return m.openConnectOverlay(ConnectTargetSplitRight)
}

func (m Model) applyNewTab() Model {
	newPaneID := nextPaneID(m.Workspace)
	newTabID := nextTabID(m.Workspace)
	m.Workspace.Tabs[newTabID] = &workspace.TabState{
		ID:           newTabID,
		Title:        "shell",
		Layout:       layout.NewLeaf(newPaneID),
		ActivePaneID: newPaneID,
		Panes: map[types.PaneID]workspace.PaneState{
			newPaneID: {
				ID:        newPaneID,
				Kind:      types.PaneKindTiled,
				SlotState: types.PaneSlotUnconnected,
			},
		},
	}
	m.Workspace.ActiveTabID = newTabID
	return m.openConnectOverlay(ConnectTargetNewTab)
}

func (m Model) applyNewFloat() Model {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	newPaneID := nextFloatingPaneID(tab)
	tab.TrackPane(workspace.PaneState{
		ID:        newPaneID,
		Kind:      types.PaneKindFloating,
		SlotState: types.PaneSlotUnconnected,
		Rect:      centeredFloatingRect(DefaultFloatingViewport(), 32, 12),
	})
	tab.ActivePaneID = newPaneID
	tab.RaiseFloatingPane(newPaneID)
	return m.openConnectOverlay(ConnectTargetNewFloat)
}

func (m Model) applyCreateTerminal(in ConfirmCreateTerminalIntent) Model {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	pane, ok := tab.ActivePane()
	if !ok {
		return m
	}
	if len(in.Command) == 0 {
		in.Command = []string{"/bin/sh"}
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = "shell"
	}
	m.Overlay = m.Overlay.Clear()
	m.PendingEffects = append(m.PendingEffects, CreateTerminalEffect{
		PaneID:  pane.ID,
		Command: append([]string(nil), in.Command...),
		Name:    in.Name,
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	return m
}

func (m Model) applyConnectExisting(terminalID types.TerminalID) Model {
	m.bindActivePaneToTerminal(terminalID)
	m.Overlay = m.Overlay.Clear()
	return m
}

func (m Model) applyClosePane() Model {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	pane, ok := tab.ActivePane()
	if !ok {
		return m
	}
	m.unbindPane(pane)
	if len(tab.Panes) <= 1 {
		pane.TerminalID = ""
		pane.SlotState = types.PaneSlotUnconnected
		tab.TrackPane(pane)
		return m
	}
	delete(tab.Panes, pane.ID)
	if pane.Kind == types.PaneKindFloating {
		tab.RemoveFloatingPane(pane.ID)
	} else if tab.Layout != nil {
		tab.Layout = tab.Layout.Remove(pane.ID)
	}
	tab.ActivePaneID = tab.FirstPaneID()
	return m
}

func (m Model) applyDisconnectPane() Model {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	pane, ok := tab.ActivePane()
	if !ok {
		return m
	}
	m.unbindPane(pane)
	pane.TerminalID = ""
	pane.SlotState = types.PaneSlotUnconnected
	tab.TrackPane(pane)
	m.Overlay = m.Overlay.Clear()
	return m
}

func (m Model) applyReconnect(terminalID types.TerminalID) Model {
	m.bindActivePaneToTerminal(terminalID)
	m.Overlay = m.Overlay.Clear()
	return m
}

func (m Model) applyKillActiveTerminal() Model {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	pane, ok := tab.ActivePane()
	if !ok || pane.TerminalID == "" {
		return m
	}
	m.PendingEffects = append(m.PendingEffects, KillTerminalEffect{
		TerminalID: pane.TerminalID,
	})
	return m
}

func (m Model) applyRemoveTerminal(in RemoveTerminalIntent) Model {
	_, visibleAffected := m.visibleTerminal(in.TerminalID)
	delete(m.Terminals, in.TerminalID)
	delete(m.Sessions, in.TerminalID)
	m.updateAllPanesForTerminal(in.TerminalID, func(next workspace.PaneState) workspace.PaneState {
		next.TerminalID = ""
		next.SlotState = types.PaneSlotUnconnected
		return next
	})
	if in.Visible || visibleAffected {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = string(in.TerminalID)
		}
		m.Notice = &NoticeState{
			Message: fmt.Sprintf("terminal %q was removed from the pool", name),
		}
	}
	return m
}

func (m Model) applyRestartTerminal(terminalID types.TerminalID) Model {
	// 当前 daemon 协议还没有独立 restart 接口，这里先明确维持 state-only 语义，
	// 避免伪造 runtime 已执行的假象。
	meta, ok := m.Terminals[terminalID]
	if !ok {
		return m
	}
	meta.State = stateterminal.StateRunning
	meta.LastInteraction = time.Now().UTC()
	m.Terminals[terminalID] = meta
	m.updateAllPanesForTerminal(terminalID, func(next workspace.PaneState) workspace.PaneState {
		next.SlotState = types.PaneSlotLive
		return next
	})
	return m
}

func (m Model) applyBecomeOwner(terminalID types.TerminalID) Model {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	pane, ok := tab.ActivePane()
	if !ok || pane.TerminalID != terminalID {
		return m
	}
	meta, ok := m.Terminals[terminalID]
	if !ok {
		return m
	}
	meta.OwnerPaneID = pane.ID
	meta.LastInteraction = time.Now().UTC()
	m.Terminals[terminalID] = meta
	return m
}

type CreateTerminalSucceededIntent struct {
	PaneID     types.PaneID
	TerminalID types.TerminalID
	Command    []string
	Name       string
	Channel    uint16
	Snapshot   *protocol.Snapshot
}

type KillTerminalSucceededIntent struct {
	TerminalID types.TerminalID
}

func (m Model) applyCreateTerminalSucceeded(in CreateTerminalSucceededIntent) Model {
	meta := stateterminal.Metadata{
		ID:              in.TerminalID,
		Name:            in.Name,
		Command:         append([]string(nil), in.Command...),
		State:           stateterminal.StateRunning,
		OwnerPaneID:     in.PaneID,
		AttachedPaneIDs: []types.PaneID{in.PaneID},
		LastInteraction: time.Now().UTC(),
	}
	m.Terminals[in.TerminalID] = meta
	m.Sessions[in.TerminalID] = TerminalSession{
		TerminalID: in.TerminalID,
		Channel:    in.Channel,
		Attached:   true,
		Snapshot:   in.Snapshot,
	}
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	pane, ok := tab.Pane(in.PaneID)
	if !ok {
		return m
	}
	pane.TerminalID = in.TerminalID
	pane.SlotState = types.PaneSlotLive
	tab.TrackPane(pane)
	tab.ActivePaneID = in.PaneID
	return m
}

func (m Model) applyKillTerminalSucceeded(terminalID types.TerminalID) Model {
	meta, ok := m.Terminals[terminalID]
	if !ok {
		return m
	}
	meta.State = stateterminal.StateExited
	m.Terminals[terminalID] = meta
	m.updateAllPanesForTerminal(terminalID, func(next workspace.PaneState) workspace.PaneState {
		next.SlotState = types.PaneSlotExited
		return next
	})
	return m
}

func (m Model) applyMoveFloatingPane(in MoveFloatingPaneIntent) Model {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	pane, ok := tab.Pane(in.PaneID)
	if !ok || pane.Kind != types.PaneKindFloating {
		return m
	}
	pane.Rect.X += in.DeltaX
	pane.Rect.Y += in.DeltaY
	pane.Rect = clampFloatingRect(pane.Rect, DefaultFloatingViewport())
	tab.TrackPane(pane)
	tab.RaiseFloatingPane(in.PaneID)
	return m
}

func (m Model) applyCenterFloatingPane(paneID types.PaneID) Model {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	pane, ok := tab.Pane(paneID)
	if !ok || pane.Kind != types.PaneKindFloating {
		return m
	}
	pane.Rect = centeredFloatingRect(DefaultFloatingViewport(), pane.Rect.W, pane.Rect.H)
	tab.TrackPane(pane)
	tab.RaiseFloatingPane(paneID)
	return m
}

func (m Model) openConnectOverlay(target ConnectTarget) Model {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	pane, _ := tab.ActivePane()
	m.Overlay = m.Overlay.Replace(OverlayState{
		Kind: OverlayConnectDialog,
		Connect: &ConnectDialogState{
			Target:      target,
			Destination: connectDestination(m.Workspace, tab, pane),
			Items:       prependCreateItem(pool.BuildConnectItems(m.Terminals)),
		},
	})
	return m
}

func (m Model) bindPane(paneID types.PaneID, terminalID types.TerminalID) {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return
	}
	pane, ok := tab.Pane(paneID)
	if !ok {
		return
	}
	meta, ok := m.Terminals[terminalID]
	if !ok {
		return
	}
	meta.AttachedPaneIDs = appendUniquePane(meta.AttachedPaneIDs, paneID)
	if meta.OwnerPaneID == "" {
		meta.OwnerPaneID = paneID
	}
	meta.LastInteraction = time.Now().UTC()
	m.Terminals[terminalID] = meta
	pane.TerminalID = terminalID
	if meta.State == stateterminal.StateExited {
		pane.SlotState = types.PaneSlotExited
	} else {
		pane.SlotState = types.PaneSlotLive
	}
	tab.TrackPane(pane)
}

func (m Model) bindActivePaneToTerminal(terminalID types.TerminalID) {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return
	}
	pane, ok := tab.ActivePane()
	if !ok {
		return
	}
	m.bindPane(pane.ID, terminalID)
}

func (m Model) unbindPane(pane workspace.PaneState) {
	if pane.TerminalID == "" {
		return
	}
	meta, ok := m.Terminals[pane.TerminalID]
	if !ok {
		return
	}
	meta.AttachedPaneIDs = removePane(meta.AttachedPaneIDs, pane.ID)
	if meta.OwnerPaneID == pane.ID {
		meta.OwnerPaneID = ""
	}
	m.Terminals[pane.TerminalID] = meta
}

func (m Model) updateAllPanesForTerminal(terminalID types.TerminalID, apply func(workspace.PaneState) workspace.PaneState) {
	for _, tab := range m.Workspace.Tabs {
		for paneID, pane := range tab.Panes {
			if pane.TerminalID != terminalID {
				continue
			}
			tab.Panes[paneID] = apply(pane)
		}
	}
}

func (m Model) visibleTerminal(terminalID types.TerminalID) (workspace.PaneState, bool) {
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return workspace.PaneState{}, false
	}
	for _, pane := range tab.Panes {
		if pane.TerminalID == terminalID {
			return pane, true
		}
	}
	return workspace.PaneState{}, false
}

func prependCreateItem(items []pool.ConnectItem) []pool.ConnectItem {
	return append([]pool.ConnectItem{{
		Name: "+ new terminal",
	}}, items...)
}

func nextPaneID(ws *workspace.WorkspaceState) types.PaneID {
	index := 1
	for {
		id := types.PaneID(fmt.Sprintf("pane-%d", index))
		if !ws.HasPane(id) {
			return id
		}
		index++
	}
}

func nextFloatingPaneID(tab *workspace.TabState) types.PaneID {
	index := 1
	for {
		id := types.PaneID(fmt.Sprintf("float-%d", index))
		if _, ok := tab.Pane(id); !ok {
			return id
		}
		index++
	}
}

func nextTabID(ws *workspace.WorkspaceState) types.TabID {
	index := 1
	for {
		id := types.TabID(fmt.Sprintf("tab-%d", index))
		if _, ok := ws.Tabs[id]; !ok {
			return id
		}
		index++
	}
}

func nextTerminalID(terminals map[types.TerminalID]stateterminal.Metadata) types.TerminalID {
	index := 1
	for {
		id := types.TerminalID(fmt.Sprintf("term-%d", index))
		if _, ok := terminals[id]; !ok {
			return id
		}
		index++
	}
}

func connectDestination(ws *workspace.WorkspaceState, tab *workspace.TabState, pane workspace.PaneState) string {
	if pane.Kind == types.PaneKindFloating {
		return fmt.Sprintf("%s / %s / %s", ws.ID, tab.ID, pane.ID)
	}
	if pane.ID == "" {
		return fmt.Sprintf("%s / %s", ws.ID, tab.ID)
	}
	return fmt.Sprintf("%s / %s / %s", ws.ID, tab.ID, pane.ID)
}

func appendUniquePane(items []types.PaneID, paneID types.PaneID) []types.PaneID {
	for _, existing := range items {
		if existing == paneID {
			return items
		}
	}
	return append(items, paneID)
}

func removePane(items []types.PaneID, paneID types.PaneID) []types.PaneID {
	out := make([]types.PaneID, 0, len(items))
	for _, existing := range items {
		if existing != paneID {
			out = append(out, existing)
		}
	}
	return out
}

func DefaultFloatingViewport() types.Rect {
	return types.Rect{X: 0, Y: 0, W: 120, H: 40}
}

func clampFloatingRect(rect, bounds types.Rect) types.Rect {
	maxX := bounds.X + bounds.W - 1
	maxY := bounds.Y + bounds.H - 1
	if rect.X < bounds.X {
		rect.X = bounds.X
	}
	if rect.Y < bounds.Y {
		rect.Y = bounds.Y
	}
	if rect.X > maxX {
		rect.X = maxX
	}
	if rect.Y > maxY {
		rect.Y = maxY
	}
	return rect
}

func centeredFloatingRect(bounds types.Rect, width, height int) types.Rect {
	if width <= 0 {
		width = 32
	}
	if height <= 0 {
		height = 12
	}
	return types.Rect{
		X: bounds.X + maxInt(0, (bounds.W-width)/2),
		Y: bounds.Y + maxInt(0, (bounds.H-height)/2),
		W: width,
		H: height,
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
