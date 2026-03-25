package tui

import (
	"context"
	"errors"
	"fmt"

	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
)

type runtimeTerminalService struct {
	client Client
}

type runtimeTerminalTopologyClient interface {
	ConnectTerminalInNewTab(workspaceID types.WorkspaceID, terminalID types.TerminalID) error
	ConnectTerminalInFloatingPane(workspaceID types.WorkspaceID, tabID types.TabID, terminalID types.TerminalID) error
}

var errRuntimeTopologyUnsupported = errors.New("runtime topology action unsupported by client")

func newRuntimeTerminalService(client Client) btui.TerminalService {
	if client == nil {
		return nil
	}
	return runtimeTerminalService{client: client}
}

func (s runtimeTerminalService) ConnectTerminal(types.PaneID, types.TerminalID) error {
	// connect-here 已经由 reducer 在本地 state 内完成，这里不再重复发远端动作。
	return nil
}

func (s runtimeTerminalService) CreateTerminal(_ types.PaneID, command []string, name string) (btui.CreateTerminalResult, error) {
	created, err := s.client.Create(context.Background(), command, name, protocol.Size{})
	if err != nil {
		return btui.CreateTerminalResult{}, err
	}
	state := types.TerminalRunStateRunning
	if created != nil && created.State != "" {
		state = types.TerminalRunState(created.State)
	}
	if created == nil {
		return btui.CreateTerminalResult{State: state}, nil
	}
	return btui.CreateTerminalResult{
		TerminalID: types.TerminalID(created.TerminalID),
		State:      state,
	}, nil
}

func (s runtimeTerminalService) StopTerminal(terminalID types.TerminalID) error {
	return s.client.Kill(context.Background(), string(terminalID))
}

func (s runtimeTerminalService) UpdateTerminalMetadata(terminalID types.TerminalID, name string, tags map[string]string) error {
	return s.client.SetMetadata(context.Background(), string(terminalID), name, tags)
}

func (s runtimeTerminalService) ConnectTerminalInNewTab(workspaceID types.WorkspaceID, terminalID types.TerminalID) error {
	client, ok := s.client.(runtimeTerminalTopologyClient)
	if !ok {
		return fmt.Errorf("%w: connect terminal %s in new tab for workspace %s", errRuntimeTopologyUnsupported, terminalID, workspaceID)
	}
	return client.ConnectTerminalInNewTab(workspaceID, terminalID)
}

func (s runtimeTerminalService) ConnectTerminalInFloatingPane(workspaceID types.WorkspaceID, tabID types.TabID, terminalID types.TerminalID) error {
	client, ok := s.client.(runtimeTerminalTopologyClient)
	if !ok {
		return fmt.Errorf("%w: connect terminal %s in floating pane for %s/%s", errRuntimeTopologyUnsupported, terminalID, workspaceID, tabID)
	}
	return client.ConnectTerminalInFloatingPane(workspaceID, tabID, terminalID)
}
