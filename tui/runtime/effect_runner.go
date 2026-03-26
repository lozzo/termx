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
