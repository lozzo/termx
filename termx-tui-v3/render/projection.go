package render

import "github.com/lozzow/termx/termx-tui-v3/state"

func (RenderVMBuilder) Build(root state.Root) RenderVM {
	shell := NewShellProjector().Project(root)
	return RenderVM{Shell: shell, Theme: ThemeFromHostTheme(root.HostTheme)}
}

type ShellProjector struct{}

func NewShellProjector() ShellProjector {
	return ShellProjector{}
}

func (ShellProjector) Project(root state.Root) ShellVM {
	shellState := root.Shell.EnsureDefaults()
	activeContent := buildActiveContentVM(root)
	return ShellVM{
		Header:  buildHeaderVM(shellState, root),
		Footer:  buildFooterVM(root, activeContent),
		Layout:  buildLayoutVM(shellState, activeContent, root),
		Overlay: buildOverlayVM(root, shellState),
		Toasts:  buildToastVMs(shellState),
		Cursor:  activeContent.Cursor,
	}
}
