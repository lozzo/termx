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
type OpenTerminalPoolIntent struct{}
type CloseTerminalPoolIntent struct{}
type SearchTerminalPoolIntent struct {
	Query string
}
type SetTerminalPoolSearchInputIntent struct {
	Active bool
}
type MoveTerminalPoolSelectionIntent struct {
	Delta int
}
type SelectTerminalPoolIntent struct {
	TerminalID types.TerminalID
}
type OpenTerminalMetadataEditorIntent struct{}
type UpdateTerminalMetadataDraftIntent struct {
	Name     string
	TagsText string
}
type SaveTerminalMetadataIntent struct{}
type OpenSelectedTerminalHereIntent struct{}
type OpenSelectedTerminalInNewTabIntent struct{}
type OpenSelectedTerminalInFloatingIntent struct{}
type PreviewStreamTickIntent struct {
	TerminalID types.TerminalID
	Revision   int
}
type SessionStreamTickIntent struct {
	TerminalID types.TerminalID
}
type KillSelectedTerminalIntent struct{}
type RemoveSelectedTerminalIntent struct{}
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
	case OpenTerminalPoolIntent:
		return next.openTerminalPool()
	case CloseTerminalPoolIntent:
		return next.SwitchScreen(ScreenWorkbench)
	case SearchTerminalPoolIntent:
		return next.applySearchTerminalPool(in.Query)
	case SetTerminalPoolSearchInputIntent:
		next.Pool.SearchInputActive = in.Active
		return next
	case MoveTerminalPoolSelectionIntent:
		return next.applyMoveTerminalPoolSelection(in.Delta)
	case SelectTerminalPoolIntent:
		return next.applySelectTerminalPool(in.TerminalID)
	case OpenTerminalMetadataEditorIntent:
		return next.applyOpenTerminalMetadataEditor()
	case UpdateTerminalMetadataDraftIntent:
		return next.applyUpdateTerminalMetadataDraft(in)
	case SaveTerminalMetadataIntent:
		return next.applySaveTerminalMetadata()
	case OpenSelectedTerminalHereIntent:
		return next.applyOpenSelectedTerminalHere()
	case OpenSelectedTerminalInNewTabIntent:
		return next.applyOpenSelectedTerminalInNewTab()
	case OpenSelectedTerminalInFloatingIntent:
		return next.applyOpenSelectedTerminalInFloating()
	case PreviewStreamTickIntent:
		return next.applyPreviewStreamTick(in)
	case SessionStreamTickIntent:
		return next.applySessionStreamTick(in)
	case KillSelectedTerminalIntent:
		return next.applyKillSelectedTerminal()
	case RemoveSelectedTerminalIntent:
		return next.applyRemoveSelectedTerminal()
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
	case PreviewTerminalSucceededIntent:
		return next.applyPreviewTerminalSucceeded(in)
	case PreviewSnapshotRefreshedIntent:
		return next.applyPreviewSnapshotRefreshed(in)
	case SessionSnapshotRefreshedIntent:
		return next.applySessionSnapshotRefreshed(in)
	case UpdateTerminalMetadataSucceededIntent:
		return next.applyUpdateTerminalMetadataSucceeded(in)
	case RemoveTerminalSucceededIntent:
		return next.applyRemoveTerminal(RemoveTerminalIntent{
			TerminalID: in.TerminalID,
			Visible:    in.Visible,
			Name:       in.Name,
		})
	case AttachTerminalSucceededIntent:
		return next.applyAttachTerminalSucceeded(in)
	case RemoteTerminalStateChangedIntent:
		return next.applyRemoteTerminalStateChanged(in)
	case RemoteCollaboratorsRevokedIntent:
		return next.applyRemoteCollaboratorsRevoked(in)
	case RemoteTerminalReadErrorIntent:
		return next.applyRemoteTerminalReadError(in)
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

func (m Model) openTerminalPool() Model {
	m = m.SwitchScreen(ScreenTerminalPool)
	m.Pool.SearchInputActive = false
	selected := m.firstTerminalPoolSelection(m.Pool.Query)
	m.Pool.SelectedTerminalID = selected
	m.Pool.PreviewTerminalID = selected
	m.Pool.PreviewReadonly = true
	if selected != "" {
		m.Pool.PreviewSubscriptionRevision++
		m.PendingEffects = append(m.PendingEffects, RefreshPreviewEffect{TerminalID: selected})
	}
	return m
}

func (m Model) applySearchTerminalPool(query string) Model {
	m.Pool.Query = strings.TrimSpace(query)
	selected := m.firstTerminalPoolSelection(m.Pool.Query)
	if selected == "" {
		m.Pool.SelectedTerminalID = ""
		m.Pool.PreviewTerminalID = ""
		m.Pool.PreviewReadonly = true
		return m
	}
	m.Pool.SelectedTerminalID = selected
	m.Pool.PreviewTerminalID = selected
	m.Pool.PreviewReadonly = true
	m.Pool.PreviewSubscriptionRevision++
	m.PendingEffects = append(m.PendingEffects, RefreshPreviewEffect{TerminalID: selected})
	return m
}

func (m Model) applyMoveTerminalPoolSelection(delta int) Model {
	items := m.orderedTerminalPoolIDs(m.Pool.Query)
	if len(items) == 0 || delta == 0 {
		return m
	}
	index := 0
	for i, item := range items {
		if item == m.selectedTerminalID() {
			index = i
			break
		}
	}
	index += delta
	if index < 0 {
		index = 0
	}
	if index >= len(items) {
		index = len(items) - 1
	}
	return m.applySelectTerminalPool(items[index])
}

func (m Model) applySelectTerminalPool(terminalID types.TerminalID) Model {
	if terminalID == "" {
		return m
	}
	if _, ok := m.Terminals[terminalID]; !ok {
		return m
	}
	m.Pool.SelectedTerminalID = terminalID
	m.Pool.PreviewTerminalID = terminalID
	m.Pool.PreviewReadonly = true
	m.Pool.PreviewSubscriptionRevision++
	m.PendingEffects = append(m.PendingEffects, RefreshPreviewEffect{TerminalID: terminalID})
	return m
}

func (m Model) applyOpenTerminalMetadataEditor() Model {
	terminalID := m.selectedTerminalID()
	meta, ok := m.Terminals[terminalID]
	if !ok {
		return m
	}
	m.Overlay = m.Overlay.Replace(OverlayState{
		Kind: OverlayTerminalMetadataEditor,
		MetadataEditor: &TerminalMetadataEditorState{
			TerminalID: terminalID,
			Name:       meta.Name,
			TagsText:   formatTagsText(meta.Tags),
		},
	})
	return m
}

func (m Model) applyUpdateTerminalMetadataDraft(in UpdateTerminalMetadataDraftIntent) Model {
	active := m.Overlay.Active()
	if active.Kind != OverlayTerminalMetadataEditor || active.MetadataEditor == nil {
		return m
	}
	editor := *active.MetadataEditor
	editor.Name = in.Name
	editor.TagsText = in.TagsText
	m.Overlay = m.Overlay.Replace(OverlayState{
		Kind:           OverlayTerminalMetadataEditor,
		MetadataEditor: &editor,
	})
	return m
}

func (m Model) applySaveTerminalMetadata() Model {
	active := m.Overlay.Active()
	if active.Kind != OverlayTerminalMetadataEditor || active.MetadataEditor == nil {
		return m
	}
	editor := active.MetadataEditor
	m.Overlay = m.Overlay.Clear()
	m.PendingEffects = append(m.PendingEffects, UpdateTerminalMetadataEffect{
		TerminalID: editor.TerminalID,
		Name:       strings.TrimSpace(editor.Name),
		Tags:       parseTagsText(editor.TagsText),
	})
	return m
}

func (m Model) applyOpenSelectedTerminalHere() Model {
	terminalID := m.selectedTerminalID()
	if terminalID == "" {
		return m
	}
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	pane, ok := tab.ActivePane()
	if ok {
		m.PendingEffects = append(m.PendingEffects, AttachTerminalEffect{
			PaneID:     pane.ID,
			TerminalID: terminalID,
		})
	}
	return m.SwitchScreen(ScreenWorkbench)
}

func (m Model) applyOpenSelectedTerminalInNewTab() Model {
	terminalID := m.selectedTerminalID()
	if terminalID == "" {
		return m
	}
	m = m.applyNewTab()
	m.Overlay = m.Overlay.Clear()
	if tab := m.Workspace.ActiveTab(); tab != nil {
		if pane, ok := tab.ActivePane(); ok {
			m.PendingEffects = append(m.PendingEffects, AttachTerminalEffect{
				PaneID:     pane.ID,
				TerminalID: terminalID,
			})
		}
	}
	return m.SwitchScreen(ScreenWorkbench)
}

func (m Model) applyOpenSelectedTerminalInFloating() Model {
	terminalID := m.selectedTerminalID()
	if terminalID == "" {
		return m
	}
	m = m.applyNewFloat()
	m.Overlay = m.Overlay.Clear()
	if tab := m.Workspace.ActiveTab(); tab != nil {
		if pane, ok := tab.ActivePane(); ok {
			m.PendingEffects = append(m.PendingEffects, AttachTerminalEffect{
				PaneID:     pane.ID,
				TerminalID: terminalID,
			})
		}
	}
	return m.SwitchScreen(ScreenWorkbench)
}

func (m Model) applyKillSelectedTerminal() Model {
	terminalID := m.selectedTerminalID()
	if terminalID == "" {
		return m
	}
	m.PendingEffects = append(m.PendingEffects, KillTerminalEffect{TerminalID: terminalID})
	return m
}

func (m Model) applyPreviewStreamTick(in PreviewStreamTickIntent) Model {
	if m.Pool.PreviewTerminalID != in.TerminalID || m.Pool.PreviewSubscriptionRevision != in.Revision {
		return m
	}
	m.PendingEffects = append(m.PendingEffects, RefreshPreviewSnapshotEffect{
		TerminalID: in.TerminalID,
		Revision:   in.Revision,
	})
	return m
}

func (m Model) applySessionStreamTick(in SessionStreamTickIntent) Model {
	session, ok := m.Sessions[in.TerminalID]
	if !ok || session.Preview || !session.Attached {
		return m
	}
	m.PendingEffects = append(m.PendingEffects, RefreshSessionSnapshotEffect{TerminalID: in.TerminalID})
	return m
}

func (m Model) applyRemoveSelectedTerminal() Model {
	terminalID := m.selectedTerminalID()
	if terminalID == "" {
		return m
	}
	meta := m.Terminals[terminalID]
	_, visibleAffected := m.visibleTerminal(terminalID)
	m.PendingEffects = append(m.PendingEffects, RemoveTerminalEffect{
		TerminalID: terminalID,
		Visible:    visibleAffected,
		Name:       meta.Name,
	})
	return m
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
	if m.Pool.SelectedTerminalID == in.TerminalID {
		m.Pool.SelectedTerminalID = m.firstTerminalPoolSelection(m.Pool.Query)
	}
	if m.Pool.PreviewTerminalID == in.TerminalID {
		m.Pool.PreviewTerminalID = m.Pool.SelectedTerminalID
		m.Pool.PreviewReadonly = true
		if m.Pool.PreviewTerminalID != "" {
			m.Pool.PreviewSubscriptionRevision++
			m.PendingEffects = append(m.PendingEffects, RefreshPreviewEffect{TerminalID: m.Pool.PreviewTerminalID})
		}
	}
	return m
}

func (m Model) applyRestartTerminal(terminalID types.TerminalID) Model {
	// 当前 daemon 协议还没有独立 restart 接口，这里先明确维持 state-only 语义，
	// 避免伪造 runtime 已执行的假象。
	if _, ok := m.Terminals[terminalID]; !ok {
		return m
	}
	m.Notice = &NoticeState{Message: fmt.Sprintf("restart for %q is not wired to runtime yet", terminalID)}
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

type PreviewTerminalSucceededIntent struct {
	TerminalID           types.TerminalID
	Channel              uint16
	Snapshot             *protocol.Snapshot
	SubscriptionRevision int
}

type PreviewSnapshotRefreshedIntent struct {
	TerminalID types.TerminalID
	Snapshot   *protocol.Snapshot
	Revision   int
}

type SessionSnapshotRefreshedIntent struct {
	TerminalID types.TerminalID
	Snapshot   *protocol.Snapshot
}

type UpdateTerminalMetadataSucceededIntent struct {
	TerminalID types.TerminalID
	Name       string
	Tags       map[string]string
}

type RemoveTerminalSucceededIntent struct {
	TerminalID types.TerminalID
	Visible    bool
	Name       string
}

type AttachTerminalSucceededIntent struct {
	PaneID     types.PaneID
	TerminalID types.TerminalID
	Channel    uint16
	Snapshot   *protocol.Snapshot
	ReadOnly   bool
	ForPreview bool
}

type RemoteTerminalStateChangedIntent struct {
	TerminalID types.TerminalID
	State      stateterminal.State
}

type RemoteCollaboratorsRevokedIntent struct {
	TerminalID types.TerminalID
}

type RemoteTerminalReadErrorIntent struct {
	TerminalID types.TerminalID
	Message    string
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

func (m Model) applyPreviewTerminalSucceeded(in PreviewTerminalSucceededIntent) Model {
	m.Pool.PreviewTerminalID = in.TerminalID
	m.Pool.PreviewReadonly = true
	if in.SubscriptionRevision > m.Pool.PreviewSubscriptionRevision {
		m.Pool.PreviewSubscriptionRevision = in.SubscriptionRevision
	}
	m.Sessions[in.TerminalID] = TerminalSession{
		TerminalID: in.TerminalID,
		Channel:    in.Channel,
		Attached:   true,
		ReadOnly:   true,
		Preview:    true,
		Snapshot:   in.Snapshot,
	}
	return m
}

func (m Model) applyPreviewSnapshotRefreshed(in PreviewSnapshotRefreshedIntent) Model {
	if m.Pool.PreviewTerminalID != in.TerminalID || m.Pool.PreviewSubscriptionRevision != in.Revision {
		return m
	}
	session, ok := m.Sessions[in.TerminalID]
	if !ok {
		return m
	}
	session.Snapshot = in.Snapshot
	m.Sessions[in.TerminalID] = session
	return m
}

func (m Model) applySessionSnapshotRefreshed(in SessionSnapshotRefreshedIntent) Model {
	session, ok := m.Sessions[in.TerminalID]
	if !ok {
		return m
	}
	session.Snapshot = in.Snapshot
	m.Sessions[in.TerminalID] = session
	return m
}

func (m Model) applyAttachTerminalSucceeded(in AttachTerminalSucceededIntent) Model {
	if in.ForPreview {
		return m
	}
	m.Sessions[in.TerminalID] = TerminalSession{
		TerminalID: in.TerminalID,
		Channel:    in.Channel,
		Attached:   true,
		ReadOnly:   in.ReadOnly,
		Preview:    false,
		Snapshot:   in.Snapshot,
	}
	tab := m.Workspace.ActiveTab()
	if tab == nil {
		return m
	}
	if _, ok := tab.Pane(in.PaneID); !ok {
		return m
	}
	m.bindPane(in.PaneID, in.TerminalID)
	tab.ActivePaneID = in.PaneID
	return m
}

func (m Model) applyRemoteTerminalStateChanged(in RemoteTerminalStateChangedIntent) Model {
	meta, ok := m.Terminals[in.TerminalID]
	if !ok {
		return m
	}
	meta.State = in.State
	m.Terminals[in.TerminalID] = meta
	m.updateAllPanesForTerminal(in.TerminalID, func(next workspace.PaneState) workspace.PaneState {
		if in.State == stateterminal.StateExited {
			next.SlotState = types.PaneSlotExited
			return next
		}
		next.SlotState = types.PaneSlotLive
		return next
	})
	return m
}

func (m Model) applyRemoteCollaboratorsRevoked(in RemoteCollaboratorsRevokedIntent) Model {
	session, ok := m.Sessions[in.TerminalID]
	if !ok {
		return m
	}
	session.ReadOnly = true
	m.Sessions[in.TerminalID] = session
	m.Notice = &NoticeState{Message: fmt.Sprintf("terminal %q became read-only", in.TerminalID)}
	return m
}

func (m Model) applyRemoteTerminalReadError(in RemoteTerminalReadErrorIntent) Model {
	session, ok := m.Sessions[in.TerminalID]
	if !ok {
		return m
	}
	session.Attached = false
	session.Channel = 0
	m.Sessions[in.TerminalID] = session
	if strings.TrimSpace(in.Message) != "" {
		m.Notice = &NoticeState{Message: in.Message}
	}
	return m
}

func daemonEventIntent(model Model, event protocol.Event) (Intent, bool) {
	switch event.Type {
	case protocol.EventTerminalRemoved:
		name := event.TerminalID
		if meta, ok := model.Terminals[types.TerminalID(event.TerminalID)]; ok && strings.TrimSpace(meta.Name) != "" {
			name = meta.Name
		}
		return RemoveTerminalIntent{
			TerminalID: types.TerminalID(event.TerminalID),
			Name:       name,
		}, true
	case protocol.EventTerminalStateChanged:
		if event.StateChanged == nil {
			return nil, false
		}
		state := stateterminal.State(event.StateChanged.NewState)
		if state == "" {
			state = stateterminal.StateRunning
		}
		return RemoteTerminalStateChangedIntent{
			TerminalID: types.TerminalID(event.TerminalID),
			State:      state,
		}, true
	case protocol.EventCollaboratorsRevoked:
		return RemoteCollaboratorsRevokedIntent{TerminalID: types.TerminalID(event.TerminalID)}, true
	case protocol.EventTerminalReadError:
		message := ""
		if event.ReadError != nil {
			message = event.ReadError.Error
		}
		return RemoteTerminalReadErrorIntent{
			TerminalID: types.TerminalID(event.TerminalID),
			Message:    message,
		}, true
	default:
		return nil, false
	}
}

func (m Model) applyUpdateTerminalMetadataSucceeded(in UpdateTerminalMetadataSucceededIntent) Model {
	meta, ok := m.Terminals[in.TerminalID]
	if !ok {
		return m
	}
	if strings.TrimSpace(in.Name) != "" {
		meta.Name = strings.TrimSpace(in.Name)
	}
	meta.Tags = cloneStringMap(in.Tags)
	m.Terminals[in.TerminalID] = meta
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
	if pane.TerminalID != "" && pane.TerminalID != terminalID {
		m.unbindPane(pane)
		pane.TerminalID = ""
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

func (m Model) selectedTerminalID() types.TerminalID {
	if m.Pool.SelectedTerminalID != "" {
		return m.Pool.SelectedTerminalID
	}
	return m.firstTerminalPoolSelection(m.Pool.Query)
}

func (m Model) firstTerminalPoolSelection(query string) types.TerminalID {
	items := m.orderedTerminalPoolIDs(query)
	for _, item := range items {
		if item != "" {
			return item
		}
	}
	return ""
}

func (m Model) orderedTerminalPoolIDs(query string) []types.TerminalID {
	items := pool.BuildConnectItems(filterTerminalPoolMetas(m.Terminals, query))
	visible := make([]types.TerminalID, 0, len(items))
	parked := make([]types.TerminalID, 0, len(items))
	exited := make([]types.TerminalID, 0, len(items))
	for _, item := range items {
		if item.TerminalID == "" {
			continue
		}
		meta, ok := m.Terminals[item.TerminalID]
		if !ok {
			continue
		}
		switch {
		case meta.State == stateterminal.StateExited:
			exited = append(exited, item.TerminalID)
		case m.isTerminalVisibleInPool(item.TerminalID):
			visible = append(visible, item.TerminalID)
		default:
			parked = append(parked, item.TerminalID)
		}
	}
	ids := make([]types.TerminalID, 0, len(visible)+len(parked)+len(exited))
	ids = append(ids, visible...)
	ids = append(ids, parked...)
	ids = append(ids, exited...)
	return ids
}

func (m Model) isTerminalVisibleInPool(terminalID types.TerminalID) bool {
	_, ok := m.visibleTerminal(terminalID)
	return ok
}

func filterTerminalPoolMetas(terminals map[types.TerminalID]stateterminal.Metadata, query string) map[types.TerminalID]stateterminal.Metadata {
	filtered := make(map[types.TerminalID]stateterminal.Metadata)
	needle := strings.ToLower(strings.TrimSpace(query))
	for id, meta := range terminals {
		if needle == "" || terminalPoolMatches(meta, needle) {
			filtered[id] = meta
		}
	}
	return filtered
}

func terminalPoolMatches(meta stateterminal.Metadata, needle string) bool {
	if strings.Contains(strings.ToLower(meta.Name), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(strings.Join(meta.Command, " ")), needle) {
		return true
	}
	for key, value := range meta.Tags {
		if strings.Contains(strings.ToLower(key), needle) || strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func formatTagsText(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tags))
	for _, value := range tags {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, ",")
}

func parseTagsText(text string) map[string]string {
	parts := strings.Split(text, ",")
	tags := make(map[string]string)
	index := 0
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		tags[fmt.Sprintf("tag:%d", index)] = trimmed
		index++
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
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
