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
	for _, item := range result.Cases {
		if item.Name == "copy-history" {
			return item.Frame, nil
		}
	}
	return result.Cases[len(result.Cases)-1].Frame, nil
}

func SmokeRunDetailed(ctx context.Context) (SmokeResult, error) {
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	rootCases := []struct {
		name string
		root state.Root
	}{
		{name: "workbench-live", root: smokeLiveRoot()},
		{name: "split-hidden-toast", root: smokeSplitHiddenToastRoot()},
		{name: "terminal-picker", root: smokeTerminalPickerRoot()},
		{name: "terminal-pool-page", root: smokeTerminalPoolRoot()},
		{name: "workbench-tree-page", root: smokeWorkbenchTreeRoot()},
		{name: "copy-empty", root: smokeCopyEmptyRoot()},
		{name: "copy-history", root: smokeCopyHistoryRoot()},
		{name: "prompt-overlay", root: smokePromptRoot()},
		{name: "help-overlay", root: smokeHelpRoot()},
		{name: "tab-workspace", root: smokeTabWorkspaceRoot()},
	}
	result := SmokeResult{Cases: make([]SmokeCase, 0, len(rootCases)+2)}
	for _, item := range rootCases {
		host := app.NewFakeTerminalHost(1)
		if item.root.Viewport.Valid {
			host.SetSize(item.root.Viewport.Cols, item.root.Viewport.Rows)
		}
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
	paneCommandFrame, err := smokePaneCommandFrame(ctx, builder, renderer)
	if err != nil {
		return SmokeResult{}, err
	}
	result.Cases = append(result.Cases, SmokeCase{Name: "pane-command-flow", Frame: paneCommandFrame})
	visualFrame, err := smokeVisualAuditFrame(ctx, builder, renderer)
	if err != nil {
		return SmokeResult{}, err
	}
	result.Cases = append(result.Cases, SmokeCase{Name: "visual-audit-current", Frame: visualFrame})
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

func smokeTerminalPoolRoot() state.Root {
	shell := state.DefaultShell().OpenTerminalPool().SetTerminalPoolQuery("日志")
	return state.Root{
		Shell:    shell,
		Viewport: state.ViewportStore{Valid: true, Cols: 100, Rows: 30},
		TerminalPool: state.TerminalPoolStore{
			Status: state.TerminalPoolReady,
			Items: []state.TerminalPoolItem{{
				TerminalID: "term-logs",
				Title:      "日志🚀",
				State:      "running",
				CWD:        "/Users/termx/project/日志",
				Cols:       120,
				Rows:       36,
				Attached:   true,
				Tags:       map[string]string{"role": "logs", "owner": "local"},
			}, {
				TerminalID: "term-worker",
				Title:      "worker",
				State:      "exited",
				CWD:        "/tmp/worker",
			}},
		},
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-logs",
			Cols:       100,
			Rows:       30,
			Lines:      []string{"pool page base"},
		},
	}
}

func smokeWorkbenchTreeRoot() state.Root {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-logs", Title: "日志🚀", Kind: state.PaneTerminalLive, TerminalID: "term-logs"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
		OpenWorkbenchTree().
		SetWorkbenchTreeQuery("日志")
	return state.Root{
		Shell:    shell,
		Viewport: state.ViewportStore{Valid: true, Cols: 100, Rows: 30},
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-logs",
			Cols:       100,
			Rows:       30,
			Lines:      []string{"tree page base"},
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

func smokePromptRoot() state.Root {
	shell := state.DefaultShell().OpenPrompt(state.PromptState{
		Title:       "Command Prompt",
		Context:     "Rename tab before switching workspace",
		Value:       "重命名",
		Placeholder: "name",
	})
	return state.Root{Shell: shell}
}

func smokeHelpRoot() state.Root {
	return state.Root{Shell: state.DefaultShell().OpenHelp("most-used")}
}

func smokeTabWorkspaceRoot() state.Root {
	shell := state.DefaultShell()
	shell, _ = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"})
	shell, _ = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceCreate, Name: "remote"})
	shell = shell.SetInteractionMode(state.InteractionModeWorkspace)
	return state.Root{
		Shell: shell,
		Surface: state.TerminalSurfaceStore{
			TerminalID: "termx-workspace",
			Cols:       80,
			Rows:       24,
			Lines:      []string{"workspace live"},
		},
	}
}

func smokePaneCommandFrame(ctx context.Context, builder render.RenderVMBuilder, renderer render.Renderer) (render.Frame, error) {
	host := app.NewFakeTerminalHost(16)
	host.SetSize(64, 16)
	root := state.Root{
		Shell: state.DefaultShell().SetPanelPresentation(state.PanelPresentationSplitLine),
		Surface: state.TerminalSurfaceStore{
			TerminalID: "termx-pane-command",
			Cols:       64,
			Rows:       16,
			Lines:      []string{"pane command live"},
		},
	}
	runtime := app.NewAppRuntime(root, app.NewShellReducer(), func(root state.Root) render.Frame {
		return renderer.Render(builder.Build(root))
	}, host, app.NewSyncEffectRunner())
	commands := []state.PaneCommand{
		{
			Action:         state.PaneCommandSplit,
			SplitDirection: state.SplitDirectionVertical,
			NewPane:        state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive},
			Source:         state.PaneCommandSourceTest,
		},
		{Action: state.PaneCommandResize, Target: state.PaneCommandTarget{PaneID: "pane-2"}, ResizeDirection: state.PaneResizeLeft, Delta: 4, Source: state.PaneCommandSourceTest},
		{Action: state.PaneCommandZoom, Target: state.PaneCommandTarget{PaneID: "pane-2"}, Source: state.PaneCommandSourceTest},
		{Action: state.PaneCommandUnzoom, Target: state.PaneCommandTarget{PaneID: "pane-2"}, Source: state.PaneCommandSourceTest},
		{Action: state.PaneCommandClose, Target: state.PaneCommandTarget{PaneID: "pane-2"}, Source: state.PaneCommandSourceTest},
	}
	for _, command := range commands {
		if err := runtime.Post(app.ShellPaneCommandMsg{Command: command}); err != nil {
			return render.Frame{}, err
		}
	}
	if err := runtime.Drain(ctx); err != nil {
		return render.Frame{}, err
	}
	frames := host.Frames()
	if len(frames) == 0 {
		return render.Frame{}, fmt.Errorf("pane command smoke produced no frames")
	}
	return frames[len(frames)-1], nil
}

func smokeVisualAuditFrame(ctx context.Context, builder render.RenderVMBuilder, renderer render.Renderer) (render.Frame, error) {
	host := app.NewFakeTerminalHost(4)
	host.SetSize(120, 40)
	shell := state.DefaultShell()
	shell, _ = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"})
	shell, _ = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabPrevious})
	shell = shell.
		SetPanelPresentation(state.PanelPresentationSplitLine).
		SplitActivePane(state.PaneState{ID: "pane-logs", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-logs"}, state.SplitDirectionVertical)
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-shell"
	shell.Workspace.Tabs[0].RootSplit.Ratio = 0.70
	shell, _ = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "float-visual",
		Title:    "quick actions",
		Pane:     state.PaneState{ID: "float-visual-pane", Title: "actions", Kind: state.PaneEmpty},
		Rect:     state.FloatingRect{X: 84, Y: 7, W: 29, H: 8},
		BoundsW:  120,
		BoundsH:  40,
		Source:   state.PaneCommandSourceTest,
	})
	root := state.Root{
		Shell:    shell,
		Viewport: state.ViewportStore{Valid: true, Cols: 120, Rows: 40},
		Surface:  visualAuditSurfaceStore(),
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
		return render.Frame{}, fmt.Errorf("visual audit smoke produced no frames")
	}
	return frames[len(frames)-1], nil
}

func visualAuditSurfaceStore() state.TerminalSurfaceStore {
	surface := (state.TerminalSurfaceStore{}).ApplySnapshot(state.LiveSurfaceSnapshot{
		TerminalID: "term-shell",
		Cols:       82,
		Rows:       34,
		Lines: []string{
			"termx git:termx-core-v2-tui-v3-migration  go v1.26.0",
			"> make test",
			"ok   termx-tui-v3/render",
			">",
		},
	})
	return surface.ApplySnapshot(state.LiveSurfaceSnapshot{
		TerminalID: "term-logs",
		Cols:       30,
		Rows:       34,
		Lines: []string{
			" visual review baseline",
			" target visual mismatch",
			" emoji 🚀 and 中文",
		},
	})
}
