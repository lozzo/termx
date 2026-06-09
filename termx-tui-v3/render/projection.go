package render

import "github.com/lozzow/termx/termx-tui-v3/state"

func (RenderVMBuilder) Build(root state.Root) RenderVM {
	shell := NewShellProjector().Project(root)
	return RenderVM{Shell: shell, Theme: ThemeFromHostTheme(root.HostTheme)}
}

type ShellProjector struct {
	Content ContentProjectorRegistry
}

func NewShellProjector() ShellProjector {
	return ShellProjector{Content: DefaultContentProjectorRegistry()}
}

func (projector ShellProjector) Project(root state.Root) ShellVM {
	if projector.Content.projectors == nil {
		projector.Content = DefaultContentProjectorRegistry()
	}
	shellState := root.Shell.EnsureDefaults()
	activeContent := projector.buildActiveContentVM(root)
	return ShellVM{
		Header:  buildHeaderVM(shellState, root),
		Footer:  buildFooterVM(root, activeContent),
		Layout:  projector.buildLayoutVM(shellState, activeContent, root),
		Overlay: projector.buildOverlayVM(root, shellState),
		Toasts:  buildToastVMs(shellState),
		Cursor:  activeContent.Cursor,
	}
}
