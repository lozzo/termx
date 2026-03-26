package runtime

import (
	"context"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

type EffectRunner struct {
	client terminalClient
}

func NewEffectRunner(client terminalClient) EffectRunner {
	return EffectRunner{client: client}
}

// Run 统一消费 reducer 产出的 effect，并把结果折叠回 app message。
func (r EffectRunner) Run(ctx context.Context, effect app.Effect) app.Message {
	if r.client == nil {
		return nil
	}
	switch typed := effect.(type) {
	case app.EffectConnectTerminal:
		meta, snapshot, err := loadTerminalRuntime(ctx, r.client, typed.TerminalID)
		if err != nil {
			return nil
		}
		return app.MessageTerminalConnected{Terminal: meta, Snapshot: snapshot}
	case app.EffectDisconnectPane:
		return app.MessageTerminalDisconnected{PaneID: typed.PaneID}
	case app.EffectReconnectTerminal:
		meta, snapshot, err := loadTerminalRuntime(ctx, r.client, typed.TerminalID)
		if err != nil {
			return nil
		}
		return app.MessageTerminalConnected{Terminal: meta, Snapshot: snapshot}
	case app.EffectRemoveTerminal:
		if err := r.client.Remove(ctx, string(typed.TerminalID)); err != nil {
			return nil
		}
		return app.MessageTerminalRemoved{TerminalID: typed.TerminalID}
	case app.EffectKillTerminal:
		if err := r.client.Kill(ctx, string(typed.TerminalID)); err != nil {
			return nil
		}
		return app.MessageTerminalExited{TerminalID: typed.TerminalID}
	case app.EffectLoadTerminalPool:
		result, err := r.client.List(ctx)
		if err != nil {
			return nil
		}
		return app.MessageTerminalPoolLoaded{Terminals: protocolListToMetadata(result)}
	default:
		return nil
	}
}

func loadTerminalRuntime(ctx context.Context, client terminalClient, terminalID types.TerminalID) (coreterminal.Metadata, *protocol.Snapshot, error) {
	result, err := client.List(ctx)
	if err != nil {
		return coreterminal.Metadata{}, nil, err
	}
	for _, terminal := range result.Terminals {
		if terminal.ID != string(terminalID) {
			continue
		}
		snapshot, err := client.Snapshot(ctx, terminal.ID, 0, 0)
		if err != nil {
			return coreterminal.Metadata{}, nil, err
		}
		return coreterminal.Metadata{
			ID:      types.TerminalID(terminal.ID),
			Name:    terminal.Name,
			Command: append([]string(nil), terminal.Command...),
			State:   protocolStateToCoreState(terminal.State),
			Tags:    terminal.Tags,
		}, snapshot, nil
	}
	snapshot, err := client.Snapshot(ctx, string(terminalID), 0, 0)
	if err != nil {
		return coreterminal.Metadata{}, nil, err
	}
	return coreterminal.Metadata{
		ID:    terminalID,
		Name:  string(terminalID),
		State: coreterminal.StateRunning,
		}, snapshot, nil
}

func protocolListToMetadata(result interface { /* compile fence */
}) []coreterminal.Metadata {
	switch typed := result.(type) {
	case *protocol.ListResult:
		out := make([]coreterminal.Metadata, 0, len(typed.Terminals))
		for _, terminal := range typed.Terminals {
			out = append(out, coreterminal.Metadata{
				ID:      types.TerminalID(terminal.ID),
				Name:    terminal.Name,
				Command: append([]string(nil), terminal.Command...),
				State:   protocolStateToCoreState(terminal.State),
				Tags:    terminal.Tags,
			})
		}
		return out
	}
	return nil
}

func protocolStateToCoreState(raw string) coreterminal.State {
	if raw == string(coreterminal.StateExited) {
		return coreterminal.StateExited
	}
	return coreterminal.StateRunning
}
