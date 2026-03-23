package reducer

import (
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/domain/connection"
	"github.com/lozzow/termx/tui/domain/types"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
)

type Effect interface {
	effectName() string
}

type ConnectTerminalEffect struct {
	PaneID     types.PaneID
	TerminalID types.TerminalID
}

func (ConnectTerminalEffect) effectName() string { return "connect_terminal" }

type StopTerminalEffect struct {
	TerminalID types.TerminalID
}

func (StopTerminalEffect) effectName() string { return "stop_terminal" }

type Result struct {
	State   types.AppState
	Effects []Effect
}

type StateReducer interface {
	Reduce(state types.AppState, in intent.Intent) Result
}

type DefaultReducer struct{}

func New() StateReducer {
	return DefaultReducer{}
}

// Reduce 保持纯状态迁移，不直接触碰 runtime service。
// 这里先把最容易反复返工的连接、退出和 workspace 跳转链路锁定下来。
func (DefaultReducer) Reduce(state types.AppState, in intent.Intent) Result {
	next := cloneAppState(state)
	result := Result{State: next}

	switch intentValue := in.(type) {
	case intent.ConnectTerminalIntent:
		applyConnectTerminal(&result.State, intentValue)
		result.Effects = append(result.Effects, ConnectTerminalEffect{
			PaneID:     intentValue.PaneID,
			TerminalID: intentValue.TerminalID,
		})
	case intent.StopTerminalIntent:
		applyStopTerminal(&result.State, intentValue)
		result.Effects = append(result.Effects, StopTerminalEffect{
			TerminalID: intentValue.TerminalID,
		})
	case intent.TerminalProgramExitedIntent:
		applyProgramExited(&result.State, intentValue)
	case intent.WorkspaceTreeJumpIntent:
		applyWorkspaceTreeJump(&result.State, intentValue)
	case intent.ClosePaneIntent:
		applyClosePane(&result.State, intentValue)
	}

	return result
}

func applyConnectTerminal(state *types.AppState, in intent.ConnectTerminalIntent) {
	setPaneState(state, in.PaneID, func(pane *types.PaneState) {
		pane.TerminalID = in.TerminalID
		pane.SlotState = types.PaneSlotConnected
		pane.LastExitCode = nil
	})
	terminal := state.Domain.Terminals[in.TerminalID]
	if terminal.ID == "" {
		terminal.ID = in.TerminalID
	}
	state.Domain.Terminals[in.TerminalID] = terminal
	snapshot := state.Domain.Connections[in.TerminalID]
	snapshot.TerminalID = in.TerminalID
	conn := connection.FromSnapshot(snapshot)
	conn.Connect(in.PaneID)
	state.Domain.Connections[in.TerminalID] = conn.Snapshot()
}

func applyStopTerminal(state *types.AppState, in intent.StopTerminalIntent) {
	forEachPane(state, func(pane *types.PaneState) {
		if pane.TerminalID != in.TerminalID {
			return
		}
		pane.TerminalID = ""
		pane.SlotState = types.PaneSlotEmpty
	})
	terminal := state.Domain.Terminals[in.TerminalID]
	terminal.State = types.TerminalRunStateStopped
	state.Domain.Terminals[in.TerminalID] = terminal
	delete(state.Domain.Connections, in.TerminalID)
}

func applyProgramExited(state *types.AppState, in intent.TerminalProgramExitedIntent) {
	exitCode := in.ExitCode
	forEachPane(state, func(pane *types.PaneState) {
		if pane.TerminalID != in.TerminalID {
			return
		}
		pane.SlotState = types.PaneSlotExited
		pane.LastExitCode = &exitCode
	})
	terminal := state.Domain.Terminals[in.TerminalID]
	terminal.State = types.TerminalRunStateExited
	terminal.ExitCode = &exitCode
	state.Domain.Terminals[in.TerminalID] = terminal
}

func applyWorkspaceTreeJump(state *types.AppState, in intent.WorkspaceTreeJumpIntent) {
	workspace, ok := state.Domain.Workspaces[in.WorkspaceID]
	if !ok {
		return
	}
	focus, ok := workspacedomain.ResolveTreeJumpFocus(workspace, in.TabID, in.PaneID)
	if !ok {
		return
	}
	workspace.ActiveTabID = in.TabID
	tab := workspace.Tabs[in.TabID]
	tab.ActivePaneID = in.PaneID
	tab.ActiveLayer = focus.Layer
	workspace.Tabs[in.TabID] = tab
	state.Domain.Workspaces[in.WorkspaceID] = workspace
	state.Domain.ActiveWorkspaceID = in.WorkspaceID
	state.UI.Focus = focus
}

func applyClosePane(state *types.AppState, in intent.ClosePaneIntent) {
	for workspaceID, workspace := range state.Domain.Workspaces {
		changedWorkspace := false
		for tabID, tab := range workspace.Tabs {
			pane, ok := tab.Panes[in.PaneID]
			if !ok {
				continue
			}
			delete(tab.Panes, in.PaneID)
			if pane.TerminalID != "" {
				snapshot := state.Domain.Connections[pane.TerminalID]
				conn := connection.FromSnapshot(snapshot)
				conn.Disconnect(in.PaneID)
				next := conn.Snapshot()
				if len(next.ConnectedPaneIDs) == 0 {
					delete(state.Domain.Connections, pane.TerminalID)
				} else {
					state.Domain.Connections[pane.TerminalID] = next
				}
			}
			if tab.ActivePaneID == in.PaneID {
				tab.ActivePaneID = firstRemainingPaneID(tab.Panes)
			}
			tab.RootSplit = removePaneFromSplit(tab.RootSplit, in.PaneID)
			workspace.Tabs[tabID] = tab
			changedWorkspace = true
		}
		if changedWorkspace {
			state.Domain.Workspaces[workspaceID] = workspace
		}
	}
}

func setPaneState(state *types.AppState, paneID types.PaneID, mutate func(*types.PaneState)) {
	for workspaceID, workspace := range state.Domain.Workspaces {
		changedWorkspace := false
		for tabID, tab := range workspace.Tabs {
			pane, ok := tab.Panes[paneID]
			if !ok {
				continue
			}
			mutate(&pane)
			tab.Panes[paneID] = pane
			workspace.Tabs[tabID] = tab
			changedWorkspace = true
		}
		if changedWorkspace {
			state.Domain.Workspaces[workspaceID] = workspace
		}
	}
}

func forEachPane(state *types.AppState, fn func(*types.PaneState)) {
	for workspaceID, workspace := range state.Domain.Workspaces {
		for tabID, tab := range workspace.Tabs {
			for paneID, pane := range tab.Panes {
				fn(&pane)
				tab.Panes[paneID] = pane
			}
			workspace.Tabs[tabID] = tab
		}
		state.Domain.Workspaces[workspaceID] = workspace
	}
}

func cloneAppState(state types.AppState) types.AppState {
	next := state
	next.Domain.WorkspaceOrder = append([]types.WorkspaceID(nil), state.Domain.WorkspaceOrder...)
	next.Domain.Workspaces = make(map[types.WorkspaceID]types.WorkspaceState, len(state.Domain.Workspaces))
	for workspaceID, workspace := range state.Domain.Workspaces {
		nextWorkspace := workspace
		nextWorkspace.TabOrder = append([]types.TabID(nil), workspace.TabOrder...)
		nextWorkspace.Tabs = make(map[types.TabID]types.TabState, len(workspace.Tabs))
		for tabID, tab := range workspace.Tabs {
			nextTab := tab
			nextTab.FloatingOrder = append([]types.PaneID(nil), tab.FloatingOrder...)
			nextTab.Panes = make(map[types.PaneID]types.PaneState, len(tab.Panes))
			for paneID, pane := range tab.Panes {
				nextTab.Panes[paneID] = pane
			}
			nextWorkspace.Tabs[tabID] = nextTab
		}
		next.Domain.Workspaces[workspaceID] = nextWorkspace
	}
	next.Domain.Terminals = make(map[types.TerminalID]types.TerminalRef, len(state.Domain.Terminals))
	for terminalID, terminal := range state.Domain.Terminals {
		clone := terminal
		clone.Command = append([]string(nil), terminal.Command...)
		if terminal.Tags != nil {
			clone.Tags = make(map[string]string, len(terminal.Tags))
			for k, v := range terminal.Tags {
				clone.Tags[k] = v
			}
		}
		next.Domain.Terminals[terminalID] = clone
	}
	next.Domain.Connections = make(map[types.TerminalID]types.ConnectionState, len(state.Domain.Connections))
	for terminalID, conn := range state.Domain.Connections {
		next.Domain.Connections[terminalID] = types.ConnectionState{
			TerminalID:        conn.TerminalID,
			ConnectedPaneIDs:  append([]types.PaneID(nil), conn.ConnectedPaneIDs...),
			OwnerPaneID:       conn.OwnerPaneID,
			AutoAcquirePolicy: conn.AutoAcquirePolicy,
		}
	}
	return next
}

func firstRemainingPaneID(panes map[types.PaneID]types.PaneState) types.PaneID {
	for paneID := range panes {
		return paneID
	}
	return ""
}

func removePaneFromSplit(node *types.SplitNode, paneID types.PaneID) *types.SplitNode {
	if node == nil {
		return nil
	}
	if node.First == nil && node.Second == nil {
		if node.PaneID == paneID {
			return nil
		}
		return node
	}
	node.First = removePaneFromSplit(node.First, paneID)
	node.Second = removePaneFromSplit(node.Second, paneID)
	switch {
	case node.First == nil:
		return node.Second
	case node.Second == nil:
		return node.First
	default:
		return node
	}
}
