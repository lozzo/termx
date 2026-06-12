package render

import "github.com/lozzow/termx/termx-tui-v3/state"

type ContentProjector interface {
	ProjectContent(ContentProjectorContext) ContentVM
}

type ContentProjectorFunc func(ContentProjectorContext) ContentVM

func (fn ContentProjectorFunc) ProjectContent(ctx ContentProjectorContext) ContentVM {
	return fn(ctx)
}

type ContentProjectorContext struct {
	Root    state.Root
	Shell   state.ShellStore
	Pane    state.PaneState
	Kind    ContentKind
	Active  bool
	Surface state.TerminalSurfaceStore
	Session state.TerminalSessionStore
}

type ContentProjectorRegistry struct {
	projectors map[ContentKind]ContentProjector
}

func DefaultContentProjectorRegistry() ContentProjectorRegistry {
	registry := ContentProjectorRegistry{projectors: map[ContentKind]ContentProjector{}}
	registry.Register(ContentTerminalLive, ContentProjectorFunc(projectTerminalLiveContent))
	registry.Register(ContentCopyHistory, ContentProjectorFunc(projectCopyHistoryContent))
	registry.Register(ContentEmptyPane, ContentProjectorFunc(projectEmptyPaneContent))
	registry.Register(ContentExitedPane, ContentProjectorFunc(projectExitedPaneContent))
	registry.Register(ContentTerminalPicker, ContentProjectorFunc(projectTerminalPickerContent))
	registry.Register(ContentTerminalPool, ContentProjectorFunc(projectTerminalPoolContent))
	registry.Register(ContentWorkbenchTree, ContentProjectorFunc(projectWorkbenchTreeContent))
	registry.Register(ContentClipboardHistory, ContentProjectorFunc(projectClipboardHistoryContent))
	registry.Register(ContentFloatingOverview, ContentProjectorFunc(projectFloatingOverviewContent))
	registry.Register(ContentPrompt, ContentProjectorFunc(projectPromptContent))
	registry.Register(ContentHelp, ContentProjectorFunc(projectHelpContent))
	registry.Register(ContentPlaceholder, ContentProjectorFunc(projectPlaceholderContent))
	return registry
}

func (registry ContentProjectorRegistry) Register(kind ContentKind, projector ContentProjector) {
	if registry.projectors == nil || kind == "" || projector == nil {
		return
	}
	registry.projectors[kind] = projector
}

func (registry ContentProjectorRegistry) Projector(kind ContentKind) (ContentProjector, bool) {
	projector, ok := registry.projectors[kind]
	return projector, ok
}

func (registry ContentProjectorRegistry) Project(ctx ContentProjectorContext) ContentVM {
	ctx.Shell = contentProjectorShell(ctx)
	if projector, ok := registry.Projector(ctx.Kind); ok {
		return projector.ProjectContent(ctx)
	}
	return ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine(activePaneTitle(ctx.Pane) + " inactive")}, Pending: true}
}

func contentProjectorShell(ctx ContentProjectorContext) state.ShellStore {
	if ctx.Shell.Workspace.ID != "" || len(ctx.Shell.Workspace.Tabs) > 0 || len(ctx.Shell.Floatings) > 0 || ctx.Shell.Overlay.Open {
		return ctx.Shell.EnsureDefaults()
	}
	return ctx.Root.Shell.EnsureDefaults()
}

func projectTerminalLiveContent(ctx ContentProjectorContext) ContentVM {
	surface := ctx.Surface
	if surface.TerminalID == "" && ctx.Pane.ID != "" {
		surface = surfaceForPane(ctx.Root, ctx.Pane)
	}
	session := ctx.Session
	if session.TerminalID == "" {
		session = ctx.Root.Session
	}
	if !ctx.Active && liveSurfaceIsPending(surface, session) {
		return placeholderContentForPane(ctx.Pane)
	}
	return buildLiveContentVM(surface, session)
}

func liveSurfaceIsPending(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) bool {
	return !surface.Ready &&
		surface.Err == "" &&
		session.LastError == "" &&
		surface.State != state.TerminalLiveExited &&
		session.State != state.TerminalLiveExited &&
		len(surface.Lines) == 0 &&
		len(surface.Screen) == 0
}

func projectCopyHistoryContent(ctx ContentProjectorContext) ContentVM {
	if !ctx.Active && !ctx.Root.CopyMode.Active {
		return placeholderContentForPane(ctx.Pane)
	}
	return buildCopyHistoryContentVM(ctx.Root, ctx.Root.History, ctx.Root.CopyMode)
}

func projectEmptyPaneContent(ctx ContentProjectorContext) ContentVM {
	return buildEmptyPaneContent(ctx.Pane)
}

func projectExitedPaneContent(ctx ContentProjectorContext) ContentVM {
	return buildExitedPaneContent(ctx.Pane)
}

func projectTerminalPickerContent(ctx ContentProjectorContext) ContentVM {
	return buildTerminalPickerContent(ctx.Root, ctx.Shell)
}

func projectTerminalPoolContent(ctx ContentProjectorContext) ContentVM {
	return buildTerminalPoolContent(ctx.Root, ctx.Shell)
}

func projectWorkbenchTreeContent(ctx ContentProjectorContext) ContentVM {
	return buildWorkbenchTreeContent(ctx.Root, ctx.Shell)
}

func projectClipboardHistoryContent(ctx ContentProjectorContext) ContentVM {
	return buildClipboardHistoryContent(ctx.Root, ctx.Shell)
}

func projectFloatingOverviewContent(ctx ContentProjectorContext) ContentVM {
	return buildFloatingOverviewContent(ctx.Root, ctx.Shell)
}

func projectPromptContent(ctx ContentProjectorContext) ContentVM {
	return buildPromptContent(ctx.Shell)
}

func projectHelpContent(ctx ContentProjectorContext) ContentVM {
	return buildHelpContent(ctx.Shell)
}

func projectPlaceholderContent(ctx ContentProjectorContext) ContentVM {
	return placeholderContentForPane(ctx.Pane)
}
