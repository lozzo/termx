package sessionstore

import (
	"time"

	"github.com/lozzow/termx/tuiv2/sessiondoc"
	"github.com/lozzow/termx/tuiv2/sessionstore/sessionpb"
	"google.golang.org/protobuf/proto"
)

func encodeSessionRecord(info SessionInfo, doc *sessiondoc.Doc) ([]byte, error) {
	return proto.Marshal(&sessionpb.SessionRecord{
		Session:   sessionInfoToPB(info),
		Workbench: docToPB(doc),
	})
}

func decodeSessionRecord(data []byte) (SessionInfo, *sessiondoc.Doc, error) {
	var msg sessionpb.SessionRecord
	if err := proto.Unmarshal(data, &msg); err != nil {
		return SessionInfo{}, nil, err
	}
	return sessionInfoFromPB(msg.GetSession()), docFromPB(msg.GetWorkbench()), nil
}

func encodeView(info ViewInfo) ([]byte, error) {
	return proto.Marshal(viewInfoToPB(info))
}

func decodeView(data []byte) (ViewInfo, error) {
	var msg sessionpb.ViewInfo
	if err := proto.Unmarshal(data, &msg); err != nil {
		return ViewInfo{}, err
	}
	return viewInfoFromPB(&msg), nil
}

func encodeLease(info LeaseInfo) ([]byte, error) {
	return proto.Marshal(leaseInfoToPB(info))
}

func decodeLease(data []byte) (LeaseInfo, error) {
	var msg sessionpb.LeaseInfo
	if err := proto.Unmarshal(data, &msg); err != nil {
		return LeaseInfo{}, err
	}
	return leaseInfoFromPB(&msg), nil
}

func sessionInfoToPB(info SessionInfo) *sessionpb.SessionInfo {
	return &sessionpb.SessionInfo{
		Id:                info.ID,
		Name:              info.Name,
		CreatedAtUnixNano: timeToUnixNano(info.CreatedAt),
		UpdatedAtUnixNano: timeToUnixNano(info.UpdatedAt),
		Revision:          info.Revision,
	}
}

func sessionInfoFromPB(msg *sessionpb.SessionInfo) SessionInfo {
	if msg == nil {
		return SessionInfo{}
	}
	return SessionInfo{
		ID:        msg.GetId(),
		Name:      msg.GetName(),
		CreatedAt: unixNanoToTime(msg.GetCreatedAtUnixNano()),
		UpdatedAt: unixNanoToTime(msg.GetUpdatedAtUnixNano()),
		Revision:  msg.GetRevision(),
	}
}

func viewInfoToPB(info ViewInfo) *sessionpb.ViewInfo {
	return &sessionpb.ViewInfo{
		ViewId:              info.ViewID,
		SessionId:           info.SessionID,
		ClientId:            info.ClientID,
		ActiveWorkspaceName: info.ActiveWorkspaceName,
		ActiveTabId:         info.ActiveTabID,
		FocusedPaneId:       info.FocusedPaneID,
		WindowCols:          uint32(info.WindowCols),
		WindowRows:          uint32(info.WindowRows),
		AttachedAtUnixNano:  timeToUnixNano(info.AttachedAt),
		UpdatedAtUnixNano:   timeToUnixNano(info.UpdatedAt),
	}
}

func viewInfoFromPB(msg *sessionpb.ViewInfo) ViewInfo {
	if msg == nil {
		return ViewInfo{}
	}
	return ViewInfo{
		ViewID:              msg.GetViewId(),
		SessionID:           msg.GetSessionId(),
		ClientID:            msg.GetClientId(),
		ActiveWorkspaceName: msg.GetActiveWorkspaceName(),
		ActiveTabID:         msg.GetActiveTabId(),
		FocusedPaneID:       msg.GetFocusedPaneId(),
		WindowCols:          uint16(msg.GetWindowCols()),
		WindowRows:          uint16(msg.GetWindowRows()),
		AttachedAt:          unixNanoToTime(msg.GetAttachedAtUnixNano()),
		UpdatedAt:           unixNanoToTime(msg.GetUpdatedAtUnixNano()),
	}
}

func leaseInfoToPB(info LeaseInfo) *sessionpb.LeaseInfo {
	return &sessionpb.LeaseInfo{
		TerminalId:         info.TerminalID,
		SessionId:          info.SessionID,
		ViewId:             info.ViewID,
		PaneId:             info.PaneID,
		AcquiredAtUnixNano: timeToUnixNano(info.AcquiredAt),
	}
}

func leaseInfoFromPB(msg *sessionpb.LeaseInfo) LeaseInfo {
	if msg == nil {
		return LeaseInfo{}
	}
	return LeaseInfo{
		TerminalID: msg.GetTerminalId(),
		SessionID:  msg.GetSessionId(),
		ViewID:     msg.GetViewId(),
		PaneID:     msg.GetPaneId(),
		AcquiredAt: unixNanoToTime(msg.GetAcquiredAtUnixNano()),
	}
}

func docToPB(doc *sessiondoc.Doc) *sessionpb.Doc {
	if doc == nil {
		return nil
	}
	out := &sessionpb.Doc{
		CurrentWorkspace: doc.CurrentWorkspace,
		WorkspaceOrder:   append([]string(nil), doc.WorkspaceOrder...),
		Workspaces:       make(map[string]*sessionpb.Workspace, len(doc.Workspaces)),
	}
	for name, ws := range doc.Workspaces {
		if ws == nil {
			continue
		}
		out.Workspaces[name] = workspaceToPB(ws)
	}
	return out
}

func docFromPB(msg *sessionpb.Doc) *sessiondoc.Doc {
	if msg == nil {
		return nil
	}
	out := &sessiondoc.Doc{
		CurrentWorkspace: msg.GetCurrentWorkspace(),
		WorkspaceOrder:   append([]string(nil), msg.GetWorkspaceOrder()...),
		Workspaces:       make(map[string]*sessiondoc.Workspace, len(msg.GetWorkspaces())),
	}
	for name, ws := range msg.GetWorkspaces() {
		if ws == nil {
			continue
		}
		out.Workspaces[name] = workspaceFromPB(ws)
	}
	return out
}

func workspaceToPB(ws *sessiondoc.Workspace) *sessionpb.Workspace {
	if ws == nil {
		return nil
	}
	out := &sessionpb.Workspace{Name: ws.Name, ActiveTab: int32(ws.ActiveTab), Tabs: make([]*sessionpb.Tab, 0, len(ws.Tabs))}
	for _, tab := range ws.Tabs {
		if tab != nil {
			out.Tabs = append(out.Tabs, tabToPB(tab))
		}
	}
	return out
}

func workspaceFromPB(msg *sessionpb.Workspace) *sessiondoc.Workspace {
	if msg == nil {
		return nil
	}
	out := &sessiondoc.Workspace{Name: msg.GetName(), ActiveTab: int(msg.GetActiveTab()), Tabs: make([]*sessiondoc.Tab, 0, len(msg.GetTabs()))}
	for _, tab := range msg.GetTabs() {
		if tab != nil {
			out.Tabs = append(out.Tabs, tabFromPB(tab))
		}
	}
	return out
}

func tabToPB(tab *sessiondoc.Tab) *sessionpb.Tab {
	if tab == nil {
		return nil
	}
	out := &sessionpb.Tab{
		Id:              tab.ID,
		Name:            tab.Name,
		Root:            layoutNodeToPB(tab.Root),
		Panes:           make(map[string]*sessionpb.Pane, len(tab.Panes)),
		Floating:        make([]*sessionpb.FloatingPane, 0, len(tab.Floating)),
		FloatingVisible: tab.FloatingVisible,
		ActivePaneId:    tab.ActivePaneID,
		ZoomedPaneId:    tab.ZoomedPaneID,
		ScrollOffset:    int32(tab.ScrollOffset),
		LayoutPreset:    int32(tab.LayoutPreset),
	}
	for paneID, pane := range tab.Panes {
		if pane != nil {
			out.Panes[paneID] = paneToPB(pane)
		}
	}
	for _, pane := range tab.Floating {
		if pane != nil {
			out.Floating = append(out.Floating, floatingPaneToPB(pane))
		}
	}
	return out
}

func tabFromPB(msg *sessionpb.Tab) *sessiondoc.Tab {
	if msg == nil {
		return nil
	}
	out := &sessiondoc.Tab{
		ID:              msg.GetId(),
		Name:            msg.GetName(),
		Root:            layoutNodeFromPB(msg.GetRoot()),
		Panes:           make(map[string]*sessiondoc.Pane, len(msg.GetPanes())),
		Floating:        make([]*sessiondoc.FloatingPane, 0, len(msg.GetFloating())),
		FloatingVisible: msg.GetFloatingVisible(),
		ActivePaneID:    msg.GetActivePaneId(),
		ZoomedPaneID:    msg.GetZoomedPaneId(),
		ScrollOffset:    int(msg.GetScrollOffset()),
		LayoutPreset:    int(msg.GetLayoutPreset()),
	}
	for paneID, pane := range msg.GetPanes() {
		if pane != nil {
			out.Panes[paneID] = paneFromPB(pane)
		}
	}
	for _, pane := range msg.GetFloating() {
		if pane != nil {
			out.Floating = append(out.Floating, floatingPaneFromPB(pane))
		}
	}
	return out
}

func layoutNodeToPB(node *sessiondoc.LayoutNode) *sessionpb.LayoutNode {
	if node == nil {
		return nil
	}
	return &sessionpb.LayoutNode{
		PaneId:    node.PaneID,
		Direction: string(node.Direction),
		Ratio:     node.Ratio,
		First:     layoutNodeToPB(node.First),
		Second:    layoutNodeToPB(node.Second),
	}
}

func layoutNodeFromPB(msg *sessionpb.LayoutNode) *sessiondoc.LayoutNode {
	if msg == nil {
		return nil
	}
	return &sessiondoc.LayoutNode{
		PaneID:    msg.GetPaneId(),
		Direction: sessiondoc.SplitDirection(msg.GetDirection()),
		Ratio:     msg.GetRatio(),
		First:     layoutNodeFromPB(msg.GetFirst()),
		Second:    layoutNodeFromPB(msg.GetSecond()),
	}
}

func paneToPB(pane *sessiondoc.Pane) *sessionpb.Pane {
	if pane == nil {
		return nil
	}
	return &sessionpb.Pane{Id: pane.ID, Title: pane.Title, TerminalId: pane.TerminalID}
}

func paneFromPB(msg *sessionpb.Pane) *sessiondoc.Pane {
	if msg == nil {
		return nil
	}
	return &sessiondoc.Pane{ID: msg.GetId(), Title: msg.GetTitle(), TerminalID: msg.GetTerminalId()}
}

func floatingPaneToPB(pane *sessiondoc.FloatingPane) *sessionpb.FloatingPane {
	if pane == nil {
		return nil
	}
	return &sessionpb.FloatingPane{
		PaneId:      pane.PaneID,
		Rect:        rectToPB(pane.Rect),
		Z:           int32(pane.Z),
		Display:     pane.Display,
		FitMode:     pane.FitMode,
		RestoreRect: rectToPB(pane.RestoreRect),
		AutoFitCols: int32(pane.AutoFitCols),
		AutoFitRows: int32(pane.AutoFitRows),
	}
}

func floatingPaneFromPB(msg *sessionpb.FloatingPane) *sessiondoc.FloatingPane {
	if msg == nil {
		return nil
	}
	return &sessiondoc.FloatingPane{
		PaneID:      msg.GetPaneId(),
		Rect:        rectFromPB(msg.GetRect()),
		Z:           int(msg.GetZ()),
		Display:     msg.GetDisplay(),
		FitMode:     msg.GetFitMode(),
		RestoreRect: rectFromPB(msg.GetRestoreRect()),
		AutoFitCols: int(msg.GetAutoFitCols()),
		AutoFitRows: int(msg.GetAutoFitRows()),
	}
}

func rectToPB(rect sessiondoc.Rect) *sessionpb.Rect {
	return &sessionpb.Rect{X: int32(rect.X), Y: int32(rect.Y), W: int32(rect.W), H: int32(rect.H)}
}

func rectFromPB(msg *sessionpb.Rect) sessiondoc.Rect {
	if msg == nil {
		return sessiondoc.Rect{}
	}
	return sessiondoc.Rect{X: int(msg.GetX()), Y: int(msg.GetY()), W: int(msg.GetW()), H: int(msg.GetH())}
}

func timeToUnixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func unixNanoToTime(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}
