package termxtuiv3

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/termx-tui-v3/app"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// ModuleName 是 v3 TUI module 的稳定标识。
const ModuleName = "termx-tui-v3"

type SmokeCase struct {
	Name  string
	Frame render.Frame
}

type SmokeResult struct {
	Cases []SmokeCase
}

// SmokeRun 跑通 state -> ShellVM -> render framework -> FrameSink 的默认 smoke。
func SmokeRun(ctx context.Context) (render.Frame, error) {
	result, err := SmokeRunDetailed(ctx)
	if err != nil {
		return render.Frame{}, err
	}
	if len(result.Cases) == 0 {
		return render.Frame{}, nil
	}
	return result.Cases[len(result.Cases)-1].Frame, nil
}

func SmokeRunDetailed(ctx context.Context) (SmokeResult, error) {
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	roots := []struct {
		name string
		root state.Root
	}{
		{name: "workbench-live", root: smokeLiveRoot()},
		{name: "split-hidden-toast", root: smokeSplitHiddenToastRoot()},
		{name: "terminal-picker", root: smokeTerminalPickerRoot()},
		{name: "copy-empty", root: smokeCopyEmptyRoot()},
		{name: "copy-history", root: smokeCopyHistoryRoot()},
	}
	result := SmokeResult{Cases: make([]SmokeCase, 0, len(roots))}
	for _, item := range roots {
		host := app.NewFakeTerminalHost(1)
		runtime := app.NewAppRuntime(item.root, nil, func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		}, host, nil)
		if err := runtime.Post(app.NoopMsg{}); err != nil {
			return SmokeResult{}, err
		}
		if err := runtime.Drain(ctx); err != nil {
			return SmokeResult{}, err
		}
		frames := host.Frames()
		if len(frames) == 0 {
			return SmokeResult{}, fmt.Errorf("smoke case %s produced no frames", item.name)
		}
		result.Cases = append(result.Cases, SmokeCase{Name: item.name, Frame: frames[len(frames)-1]})
	}
	return result, nil
}

func smokeLiveRoot() state.Root {
	return state.Root{
		Shell: state.DefaultShell(),
		Surface: state.TerminalSurfaceStore{
			TerminalID: "termx-live",
			Cols:       80,
			Rows:       24,
			Lines:      []string{"termx live 🚀", "你好 output"},
		},
	}
}

func smokeSplitHiddenToastRoot() state.Root {
	shell := state.DefaultShell().
		SetPanelPresentation(state.PanelPresentationSplitLine).
		SetHeaderVisible(false).
		SetFooterVisible(false).
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical).
		AddToast(state.ToastSpec{ID: "toast-1", Severity: state.ToastWarning, Title: "warn 🚀", Body: "世界", Pending: true})
	return state.Root{
		Shell: shell,
		Surface: state.TerminalSurfaceStore{
			TerminalID: "termx-split",
			Cols:       80,
			Rows:       24,
			Lines:      []string{"split live"},
		},
	}
}

func smokeTerminalPickerRoot() state.Root {
	return state.Root{
		Shell: state.DefaultShell().OpenTerminalPicker(),
		Surface: state.TerminalSurfaceStore{
			TerminalID: "termx-picker",
			Cols:       80,
			Rows:       24,
			Lines:      []string{"picker base"},
		},
	}
}

func smokeCopyEmptyRoot() state.Root {
	return state.Root{
		History: state.HistoryStore{
			TerminalID: "termx-copy-empty",
			Token:      "empty-token",
			Cols:       80,
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "termx-copy-empty",
			BoundToken: "empty-token",
			BoundCols:  80,
		},
	}
}

func smokeCopyHistoryRoot() state.Root {
	return state.Root{
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
}
