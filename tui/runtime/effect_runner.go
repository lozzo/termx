package runtime

import (
	"context"

	"github.com/lozzow/termx/tui/app"
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
	default:
		return nil
	}
}
