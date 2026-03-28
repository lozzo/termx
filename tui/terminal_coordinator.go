package tui

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/protocol"
)

type TerminalCoordinator struct {
	client Client
	store  *TerminalStore
}

func NewTerminalCoordinator(client Client, store *TerminalStore) *TerminalCoordinator {
	return &TerminalCoordinator{client: client, store: store}
}

func (c *TerminalCoordinator) Client() Client {
	if c == nil {
		return nil
	}
	return c.client
}

func (c *TerminalCoordinator) Store() *TerminalStore {
	if c == nil {
		return nil
	}
	return c.store
}

func (c *TerminalCoordinator) AttachTerminal(ctx context.Context, info protocol.TerminalInfo) (*Viewport, error) {
	if c == nil || c.client == nil || c.store == nil {
		return nil, fmt.Errorf("terminal coordinator unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	attached, err := c.client.Attach(ctx, info.ID, "collaborator")
	if err != nil {
		return nil, err
	}
	snap, err := c.client.Snapshot(ctx, info.ID, 0, 200)
	if err != nil {
		return nil, err
	}
	terminal := c.store.GetOrCreate(info.ID)
	terminal.SetMetadata(info.Name, info.Command, info.Tags)
	terminal.State = info.State
	terminal.Snapshot = snap
	terminal.Channel = attached.Channel
	terminal.AttachMode = attached.Mode

	view := &Viewport{
		TerminalID:    info.ID,
		Channel:       attached.Channel,
		AttachMode:    attached.Mode,
		Snapshot:      snap,
		Name:          info.Name,
		Command:       append([]string(nil), info.Command...),
		Tags:          cloneStringMap(info.Tags),
		TerminalState: info.State,
		ExitCode:      info.ExitCode,
		Mode:          ViewportModeFit,
		renderDirty:   true,
	}
	return view, nil
}

func (c *TerminalCoordinator) MarkExited(terminalID string, exitCode int) {
	if c == nil || c.store == nil || terminalID == "" {
		return
	}
	terminal := c.store.GetOrCreate(terminalID)
	terminal.State = "exited"
	code := exitCode
	terminal.ExitCode = &code
}

func (c *TerminalCoordinator) MarkKilled(terminalID string) {
	if c == nil || c.store == nil || terminalID == "" {
		return
	}
	terminal := c.store.GetOrCreate(terminalID)
	terminal.State = "killed"
	terminal.ExitCode = nil
}
