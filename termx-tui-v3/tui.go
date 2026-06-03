package termxtuiv3

import (
	"context"

	"github.com/lozzow/termx/termx-tui-v3/app"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// ModuleName 是 v3 TUI module 的稳定标识。
const ModuleName = "termx-tui-v3"

// SmokeRun 跑通 state -> RenderVM -> Renderer -> FrameSink 的最小新路径。
func SmokeRun(ctx context.Context) (render.Frame, error) {
	host := app.NewFakeTerminalHost(1)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "termx-smoke",
			Token:      "smoke-token",
			Cols:       80,
			Rows:       []state.HistoryRow{{Text: "termx-tui-v3", LineID: 1}},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "termx-smoke",
			BoundToken: "smoke-token",
			BoundCols:  80,
		},
	}
	runtime := app.NewAppRuntime(root, nil, func(root state.Root) render.Frame {
		return renderer.Render(builder.Build(root))
	}, host, nil)
	if err := runtime.Post(app.NoopMsg{}); err != nil {
		return render.Frame{}, err
	}
	if err := runtime.Drain(ctx); err != nil {
		return render.Frame{}, err
	}
	frames := host.Frames()
	if len(frames) == 0 {
		return render.Frame{}, nil
	}
	return frames[len(frames)-1], nil
}
