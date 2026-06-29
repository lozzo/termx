package input

// Binding 是 tui-v3 自有快捷键目录项；它只表达 semantic intent，不携带 app state。
type Binding struct {
	ID      string
	Mode    InteractionMode
	Key     Key
	Char    string
	Ctrl    bool
	Alt     bool
	Shift   bool
	Intent  IntentKind
	Command string
	Action  ShellAction
	Reason  string
	Target  InteractionMode
}

var bindingCatalog = []Binding{
	{ID: "root-pane", Mode: InteractionModeNormal, Key: KeyChar, Char: "\x10", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModePane},
	{ID: "root-pane-named", Mode: InteractionModeNormal, Key: KeyChar, Char: "p", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModePane},
	{ID: "root-resize", Mode: InteractionModeNormal, Key: KeyChar, Char: "\x12", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModeResize},
	{ID: "root-resize-named", Mode: InteractionModeNormal, Key: KeyChar, Char: "r", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModeResize},
	{ID: "root-global", Mode: InteractionModeNormal, Key: KeyChar, Char: "\x07", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModeGlobal},
	{ID: "root-global-named", Mode: InteractionModeNormal, Key: KeyChar, Char: "g", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModeGlobal},
	{ID: "root-floating", Mode: InteractionModeNormal, Key: KeyChar, Char: "\x0f", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModeFloating},
	{ID: "root-floating-named", Mode: InteractionModeNormal, Key: KeyChar, Char: "o", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModeFloating},
	{ID: "root-tab", Mode: InteractionModeNormal, Key: KeyChar, Char: "\x14", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModeTab},
	{ID: "root-tab-named", Mode: InteractionModeNormal, Key: KeyChar, Char: "t", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModeTab},
	{ID: "root-workspace", Mode: InteractionModeNormal, Key: KeyChar, Char: "\x17", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModeWorkspace},
	{ID: "root-workspace-named", Mode: InteractionModeNormal, Key: KeyChar, Char: "w", Ctrl: true, Intent: IntentSetInteractionMode, Target: InteractionModeWorkspace},
	{ID: "root-picker", Mode: InteractionModeNormal, Key: KeyChar, Char: "\x06", Ctrl: true, Intent: IntentOpenTerminalPicker},
	{ID: "root-picker-named", Mode: InteractionModeNormal, Key: KeyChar, Char: "f", Ctrl: true, Intent: IntentOpenTerminalPicker},

	{ID: "pane-close", Mode: InteractionModePane, Key: KeyChar, Char: "x", Intent: IntentWorkbenchCommand, Command: "pane close"},
	{ID: "pane-close-tuiv2", Mode: InteractionModePane, Key: KeyChar, Char: "w", Intent: IntentWorkbenchCommand, Command: "pane close"},
	{ID: "pane-detach-tuiv2", Mode: InteractionModePane, Key: KeyChar, Char: "d", Intent: IntentWorkbenchCommand, Command: "pane detach"},
	{ID: "pane-reconnect-tuiv2", Mode: InteractionModePane, Key: KeyChar, Char: "r", Intent: IntentWorkbenchCommand, Command: "pane reconnect"},
	{ID: "pane-restart-tuiv2", Mode: InteractionModePane, Key: KeyChar, Char: "R", Intent: IntentWorkbenchCommand, Command: "pane restart"},
	{ID: "pane-owner-tuiv2", Mode: InteractionModePane, Key: KeyChar, Char: "a", Intent: IntentWorkbenchCommand, Command: "pane take-owner"},
	{ID: "pane-lock-tuiv2", Mode: InteractionModePane, Key: KeyChar, Char: "s", Intent: IntentWorkbenchCommand, Command: "terminal size lock"},
	{ID: "pane-split-right-tuiv2", Mode: InteractionModePane, Key: KeyChar, Char: "%", Intent: IntentPaneCommand, Command: "pane split-right"},
	{ID: "pane-split-right-ctrl-d", Mode: InteractionModePane, Key: KeyChar, Char: "\x04", Ctrl: true, Intent: IntentPaneCommand, Command: "pane split-right"},
	{ID: "pane-split-right-ctrl-d-named", Mode: InteractionModePane, Key: KeyChar, Char: "d", Ctrl: true, Intent: IntentPaneCommand, Command: "pane split-right"},
	{ID: "pane-split-down-tuiv2", Mode: InteractionModePane, Key: KeyChar, Char: "\"", Intent: IntentPaneCommand, Command: "pane split-down"},
	{ID: "pane-split-down-ctrl-e", Mode: InteractionModePane, Key: KeyChar, Char: "\x05", Ctrl: true, Intent: IntentPaneCommand, Command: "pane split-down"},
	{ID: "pane-split-down-ctrl-e-named", Mode: InteractionModePane, Key: KeyChar, Char: "e", Ctrl: true, Intent: IntentPaneCommand, Command: "pane split-down"},
	{ID: "pane-close-kill", Mode: InteractionModePane, Key: KeyChar, Char: "X", Intent: IntentWorkbenchCommand, Command: "pane kill confirm=accepted"},
	{ID: "pane-zoom", Mode: InteractionModePane, Key: KeyChar, Char: "z", Intent: IntentPaneCommand, Command: "pane toggle-zoom"},
	{ID: "pane-balance", Mode: InteractionModePane, Key: KeyChar, Char: "b", Intent: IntentPaneCommand, Command: "pane balance"},
	{ID: "pane-card", Mode: InteractionModePane, Key: KeyChar, Char: "c", Intent: IntentPaneCommand, Command: "pane presentation card"},
	{ID: "pane-split-line", Mode: InteractionModePane, Key: KeyChar, Char: "p", Intent: IntentPaneCommand, Command: "pane presentation split-line"},
	{ID: "pane-focus-next", Mode: InteractionModePane, Key: KeyChar, Char: "n", Intent: IntentPaneCommand, Command: "pane focus-next"},
	{ID: "pane-focus-prev", Mode: InteractionModePane, Key: KeyChar, Char: "N", Intent: IntentPaneCommand, Command: "pane focus-prev"},
	{ID: "pane-focus-left", Mode: InteractionModePane, Key: KeyChar, Char: "h", Intent: IntentPaneCommand, Command: "pane focus-prev"},
	{ID: "pane-focus-up", Mode: InteractionModePane, Key: KeyChar, Char: "k", Intent: IntentPaneCommand, Command: "pane focus-prev"},
	{ID: "pane-focus-right", Mode: InteractionModePane, Key: KeyChar, Char: "l", Intent: IntentPaneCommand, Command: "pane focus-next"},
	{ID: "pane-focus-down", Mode: InteractionModePane, Key: KeyChar, Char: "j", Intent: IntentPaneCommand, Command: "pane focus-next"},
	{ID: "pane-focus-left-arrow", Mode: InteractionModePane, Key: KeyLeft, Intent: IntentPaneCommand, Command: "pane focus-prev"},
	{ID: "pane-focus-up-arrow", Mode: InteractionModePane, Key: KeyUp, Intent: IntentPaneCommand, Command: "pane focus-prev"},
	{ID: "pane-focus-right-arrow", Mode: InteractionModePane, Key: KeyRight, Intent: IntentPaneCommand, Command: "pane focus-next"},
	{ID: "pane-focus-down-arrow", Mode: InteractionModePane, Key: KeyDown, Intent: IntentPaneCommand, Command: "pane focus-next"},

	{ID: "resize-left", Mode: InteractionModeResize, Key: KeyLeft, Intent: IntentPaneCommand, Command: "pane resize left delta=2"},
	{ID: "resize-right", Mode: InteractionModeResize, Key: KeyRight, Intent: IntentPaneCommand, Command: "pane resize right delta=2"},
	{ID: "resize-up", Mode: InteractionModeResize, Key: KeyUp, Intent: IntentPaneCommand, Command: "pane resize up delta=2"},
	{ID: "resize-down", Mode: InteractionModeResize, Key: KeyDown, Intent: IntentPaneCommand, Command: "pane resize down delta=2"},
	{ID: "resize-left-h", Mode: InteractionModeResize, Key: KeyChar, Char: "h", Intent: IntentPaneCommand, Command: "pane resize left delta=2"},
	{ID: "resize-right-l", Mode: InteractionModeResize, Key: KeyChar, Char: "l", Intent: IntentPaneCommand, Command: "pane resize right delta=2"},
	{ID: "resize-up-k", Mode: InteractionModeResize, Key: KeyChar, Char: "k", Intent: IntentPaneCommand, Command: "pane resize up delta=2"},
	{ID: "resize-down-j", Mode: InteractionModeResize, Key: KeyChar, Char: "j", Intent: IntentPaneCommand, Command: "pane resize down delta=2"},
	{ID: "resize-owner-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "a", Intent: IntentWorkbenchCommand, Command: "pane take-owner"},
	{ID: "resize-lock-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "s", Intent: IntentWorkbenchCommand, Command: "terminal size lock"},
	{ID: "resize-layout-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: " ", Intent: IntentWorkbenchCommand, Command: "terminal layout toggle"},
	{ID: "resize-pan-left-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "A", Intent: IntentWorkbenchCommand, Command: "terminal layout pan-left"},
	{ID: "resize-pan-down-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "S", Intent: IntentWorkbenchCommand, Command: "terminal layout pan-down"},
	{ID: "resize-pan-up-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "W", Intent: IntentWorkbenchCommand, Command: "terminal layout pan-up"},
	{ID: "resize-pan-right-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "D", Intent: IntentWorkbenchCommand, Command: "terminal layout pan-right"},
	{ID: "resize-pan-left-arrow-tuiv2", Mode: InteractionModeResize, Key: KeyLeft, Shift: true, Intent: IntentWorkbenchCommand, Command: "terminal layout pan-left"},
	{ID: "resize-pan-down-arrow-tuiv2", Mode: InteractionModeResize, Key: KeyDown, Shift: true, Intent: IntentWorkbenchCommand, Command: "terminal layout pan-down"},
	{ID: "resize-pan-up-arrow-tuiv2", Mode: InteractionModeResize, Key: KeyUp, Shift: true, Intent: IntentWorkbenchCommand, Command: "terminal layout pan-up"},
	{ID: "resize-pan-right-arrow-tuiv2", Mode: InteractionModeResize, Key: KeyRight, Shift: true, Intent: IntentWorkbenchCommand, Command: "terminal layout pan-right"},
	{ID: "resize-align-left-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "0", Intent: IntentWorkbenchCommand, Command: "terminal layout align-left"},
	{ID: "resize-align-right-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "$", Intent: IntentWorkbenchCommand, Command: "terminal layout align-right"},
	{ID: "resize-align-top-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "^", Intent: IntentWorkbenchCommand, Command: "terminal layout align-top"},
	{ID: "resize-align-bottom-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "B", Intent: IntentWorkbenchCommand, Command: "terminal layout align-bottom"},
	{ID: "resize-center-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "m", Intent: IntentWorkbenchCommand, Command: "terminal layout center"},
	{ID: "resize-center-horizontal-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "|", Intent: IntentWorkbenchCommand, Command: "terminal layout center-x"},
	{ID: "resize-center-vertical-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "_", Intent: IntentWorkbenchCommand, Command: "terminal layout center-y"},
	{ID: "resize-layout-reset-tuiv2", Mode: InteractionModeResize, Key: KeyChar, Char: "r", Intent: IntentWorkbenchCommand, Command: "terminal layout reset"},
	{ID: "resize-left-large", Mode: InteractionModeResize, Key: KeyChar, Char: "H", Intent: IntentPaneCommand, Command: "pane resize left delta=6"},
	{ID: "resize-right-large", Mode: InteractionModeResize, Key: KeyChar, Char: "L", Intent: IntentPaneCommand, Command: "pane resize right delta=6"},
	{ID: "resize-up-large", Mode: InteractionModeResize, Key: KeyChar, Char: "K", Intent: IntentPaneCommand, Command: "pane resize up delta=6"},
	{ID: "resize-down-large", Mode: InteractionModeResize, Key: KeyChar, Char: "J", Intent: IntentPaneCommand, Command: "pane resize down delta=6"},
	{ID: "resize-balance", Mode: InteractionModeResize, Key: KeyChar, Char: "b", Intent: IntentPaneCommand, Command: "pane balance"},
	{ID: "resize-balance-equals", Mode: InteractionModeResize, Key: KeyChar, Char: "=", Intent: IntentPaneCommand, Command: "pane balance"},

	{ID: "global-header", Mode: InteractionModeGlobal, Key: KeyChar, Char: "h", Intent: IntentShellAction, Action: ShellActionToggleHeader},
	{ID: "global-footer", Mode: InteractionModeGlobal, Key: KeyChar, Char: "f", Intent: IntentShellAction, Action: ShellActionToggleFooter},
	{ID: "global-clear-toasts", Mode: InteractionModeGlobal, Key: KeyChar, Char: "c", Intent: IntentShellAction, Action: ShellActionClearToasts},
	{ID: "global-close-toast", Mode: InteractionModeGlobal, Key: KeyChar, Char: "T", Intent: IntentShellAction, Action: ShellActionCloseToast},
	{ID: "global-pool", Mode: InteractionModeGlobal, Key: KeyChar, Char: "p", Intent: IntentShellAction, Action: ShellActionOpenPool},
	{ID: "global-pool-tuiv2", Mode: InteractionModeGlobal, Key: KeyChar, Char: "m", Intent: IntentShellAction, Action: ShellActionOpenPool},
	{ID: "global-pool-tuiv2-status", Mode: InteractionModeGlobal, Key: KeyChar, Char: "t", Intent: IntentShellAction, Action: ShellActionOpenPool},
	{ID: "global-tree", Mode: InteractionModeGlobal, Key: KeyChar, Char: "w", Intent: IntentShellAction, Action: ShellActionOpenTree},
	{ID: "global-prompt", Mode: InteractionModeGlobal, Key: KeyChar, Char: ":", Intent: IntentShellAction, Action: ShellActionOpenPrompt},
	{ID: "global-help", Mode: InteractionModeGlobal, Key: KeyChar, Char: "?", Intent: IntentShellAction, Action: ShellActionOpenHelp},
	{ID: "global-quit", Mode: InteractionModeGlobal, Key: KeyChar, Char: "q", Intent: IntentShellAction, Action: ShellActionQuit},

	{ID: "floating-new", Mode: InteractionModeFloating, Key: KeyChar, Char: "n", Intent: IntentShellAction, Action: ShellActionFloatingNew},
	{ID: "floating-overview-tuiv2", Mode: InteractionModeFloating, Key: KeyChar, Char: "o", Intent: IntentShellAction, Action: ShellActionFloatingOverview},
	{ID: "floating-summon-1", Mode: InteractionModeFloating, Key: KeyChar, Char: "1", Intent: IntentShellAction, Action: ShellActionFloatingSummon, Reason: "1"},
	{ID: "floating-summon-2", Mode: InteractionModeFloating, Key: KeyChar, Char: "2", Intent: IntentShellAction, Action: ShellActionFloatingSummon, Reason: "2"},
	{ID: "floating-summon-3", Mode: InteractionModeFloating, Key: KeyChar, Char: "3", Intent: IntentShellAction, Action: ShellActionFloatingSummon, Reason: "3"},
	{ID: "floating-summon-4", Mode: InteractionModeFloating, Key: KeyChar, Char: "4", Intent: IntentShellAction, Action: ShellActionFloatingSummon, Reason: "4"},
	{ID: "floating-summon-5", Mode: InteractionModeFloating, Key: KeyChar, Char: "5", Intent: IntentShellAction, Action: ShellActionFloatingSummon, Reason: "5"},
	{ID: "floating-summon-6", Mode: InteractionModeFloating, Key: KeyChar, Char: "6", Intent: IntentShellAction, Action: ShellActionFloatingSummon, Reason: "6"},
	{ID: "floating-summon-7", Mode: InteractionModeFloating, Key: KeyChar, Char: "7", Intent: IntentShellAction, Action: ShellActionFloatingSummon, Reason: "7"},
	{ID: "floating-summon-8", Mode: InteractionModeFloating, Key: KeyChar, Char: "8", Intent: IntentShellAction, Action: ShellActionFloatingSummon, Reason: "8"},
	{ID: "floating-summon-9", Mode: InteractionModeFloating, Key: KeyChar, Char: "9", Intent: IntentShellAction, Action: ShellActionFloatingSummon, Reason: "9"},
	{ID: "floating-pick-tuiv2", Mode: InteractionModeFloating, Key: KeyChar, Char: "f", Intent: IntentShellAction, Action: ShellActionOpenPicker},
	{ID: "floating-owner-tuiv2", Mode: InteractionModeFloating, Key: KeyChar, Char: "a", Intent: IntentWorkbenchCommand, Command: "floating take-owner"},
	{ID: "floating-close", Mode: InteractionModeFloating, Key: KeyChar, Char: "x", Intent: IntentShellAction, Action: ShellActionFloatingCtrl, Reason: "close"},
	{ID: "floating-collapse", Mode: InteractionModeFloating, Key: KeyChar, Char: "z", Intent: IntentShellAction, Action: ShellActionFloatingCtrl, Reason: "collapse"},
	{ID: "floating-collapse-tuiv2", Mode: InteractionModeFloating, Key: KeyChar, Char: "m", Intent: IntentShellAction, Action: ShellActionFloatingCtrl, Reason: "collapse"},
	{ID: "floating-center", Mode: InteractionModeFloating, Key: KeyChar, Char: "c", Intent: IntentShellAction, Action: ShellActionFloatingCtrl, Reason: "center"},
	{ID: "floating-toggle-all", Mode: InteractionModeFloating, Key: KeyChar, Char: "v", Intent: IntentShellAction, Action: ShellActionFloatingGroup, Reason: "toggle-all"},
	{ID: "floating-fit", Mode: InteractionModeFloating, Key: KeyChar, Char: "=", Intent: IntentShellAction, Action: ShellActionFloatingGroup, Reason: "fit"},
	{ID: "floating-auto-fit", Mode: InteractionModeFloating, Key: KeyChar, Char: "s", Intent: IntentShellAction, Action: ShellActionFloatingGroup, Reason: "toggle-auto-fit"},
	{ID: "floating-left", Mode: InteractionModeFloating, Key: KeyChar, Char: "h", Intent: IntentShellAction, Action: ShellActionFloatingMove, Reason: "left"},
	{ID: "floating-right", Mode: InteractionModeFloating, Key: KeyChar, Char: "l", Intent: IntentShellAction, Action: ShellActionFloatingMove, Reason: "right"},
	{ID: "floating-up", Mode: InteractionModeFloating, Key: KeyChar, Char: "k", Intent: IntentShellAction, Action: ShellActionFloatingMove, Reason: "up"},
	{ID: "floating-down", Mode: InteractionModeFloating, Key: KeyChar, Char: "j", Intent: IntentShellAction, Action: ShellActionFloatingMove, Reason: "down"},
	{ID: "floating-left-arrow", Mode: InteractionModeFloating, Key: KeyLeft, Intent: IntentShellAction, Action: ShellActionFloatingMove, Reason: "left"},
	{ID: "floating-right-arrow", Mode: InteractionModeFloating, Key: KeyRight, Intent: IntentShellAction, Action: ShellActionFloatingMove, Reason: "right"},
	{ID: "floating-up-arrow", Mode: InteractionModeFloating, Key: KeyUp, Intent: IntentShellAction, Action: ShellActionFloatingMove, Reason: "up"},
	{ID: "floating-down-arrow", Mode: InteractionModeFloating, Key: KeyDown, Intent: IntentShellAction, Action: ShellActionFloatingMove, Reason: "down"},
	{ID: "floating-narrow", Mode: InteractionModeFloating, Key: KeyChar, Char: "H", Intent: IntentShellAction, Action: ShellActionFloatingSize, Reason: "narrow"},
	{ID: "floating-wide", Mode: InteractionModeFloating, Key: KeyChar, Char: "L", Intent: IntentShellAction, Action: ShellActionFloatingSize, Reason: "wide"},
	{ID: "floating-short", Mode: InteractionModeFloating, Key: KeyChar, Char: "K", Intent: IntentShellAction, Action: ShellActionFloatingSize, Reason: "short"},
	{ID: "floating-tall", Mode: InteractionModeFloating, Key: KeyChar, Char: "J", Intent: IntentShellAction, Action: ShellActionFloatingSize, Reason: "tall"},

	{ID: "tab-create", Mode: InteractionModeTab, Key: KeyChar, Char: "c", Intent: IntentWorkbenchCommand, Command: "tab create"},
	{ID: "tab-next", Mode: InteractionModeTab, Key: KeyChar, Char: "n", Intent: IntentWorkbenchCommand, Command: "tab next"},
	{ID: "tab-next-vim", Mode: InteractionModeTab, Key: KeyChar, Char: "l", Intent: IntentWorkbenchCommand, Command: "tab next"},
	{ID: "tab-next-bracket", Mode: InteractionModeTab, Key: KeyChar, Char: "]", Intent: IntentWorkbenchCommand, Command: "tab next"},
	{ID: "tab-prev", Mode: InteractionModeTab, Key: KeyChar, Char: "p", Intent: IntentWorkbenchCommand, Command: "tab previous"},
	{ID: "tab-prev-vim", Mode: InteractionModeTab, Key: KeyChar, Char: "h", Intent: IntentWorkbenchCommand, Command: "tab previous"},
	{ID: "tab-prev-bracket", Mode: InteractionModeTab, Key: KeyChar, Char: "[", Intent: IntentWorkbenchCommand, Command: "tab previous"},
	{ID: "tab-jump-1", Mode: InteractionModeTab, Key: KeyChar, Char: "1", Intent: IntentWorkbenchCommand, Command: "tab jump 1"},
	{ID: "tab-jump-2", Mode: InteractionModeTab, Key: KeyChar, Char: "2", Intent: IntentWorkbenchCommand, Command: "tab jump 2"},
	{ID: "tab-jump-3", Mode: InteractionModeTab, Key: KeyChar, Char: "3", Intent: IntentWorkbenchCommand, Command: "tab jump 3"},
	{ID: "tab-jump-4", Mode: InteractionModeTab, Key: KeyChar, Char: "4", Intent: IntentWorkbenchCommand, Command: "tab jump 4"},
	{ID: "tab-jump-5", Mode: InteractionModeTab, Key: KeyChar, Char: "5", Intent: IntentWorkbenchCommand, Command: "tab jump 5"},
	{ID: "tab-jump-6", Mode: InteractionModeTab, Key: KeyChar, Char: "6", Intent: IntentWorkbenchCommand, Command: "tab jump 6"},
	{ID: "tab-jump-7", Mode: InteractionModeTab, Key: KeyChar, Char: "7", Intent: IntentWorkbenchCommand, Command: "tab jump 7"},
	{ID: "tab-jump-8", Mode: InteractionModeTab, Key: KeyChar, Char: "8", Intent: IntentWorkbenchCommand, Command: "tab jump 8"},
	{ID: "tab-jump-9", Mode: InteractionModeTab, Key: KeyChar, Char: "9", Intent: IntentWorkbenchCommand, Command: "tab jump 9"},
	{ID: "tab-rename", Mode: InteractionModeTab, Key: KeyChar, Char: "r", Intent: IntentWorkbenchCommand, Command: "tab rename"},
	{ID: "tab-close", Mode: InteractionModeTab, Key: KeyChar, Char: "x", Intent: IntentWorkbenchCommand, Command: "tab close"},
	{ID: "tab-kill-tuiv2", Mode: InteractionModeTab, Key: KeyChar, Char: "X", Intent: IntentWorkbenchCommand, Command: "tab kill confirm=accepted"},

	{ID: "workspace-create", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "c", Intent: IntentWorkbenchCommand, Command: "workspace create"},
	{ID: "workspace-next", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "n", Intent: IntentWorkbenchCommand, Command: "workspace next"},
	{ID: "workspace-next-vim", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "l", Intent: IntentWorkbenchCommand, Command: "workspace next"},
	{ID: "workspace-next-bracket", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "]", Intent: IntentWorkbenchCommand, Command: "workspace next"},
	{ID: "workspace-prev", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "p", Intent: IntentWorkbenchCommand, Command: "workspace previous"},
	{ID: "workspace-prev-vim", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "h", Intent: IntentWorkbenchCommand, Command: "workspace previous"},
	{ID: "workspace-prev-bracket", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "[", Intent: IntentWorkbenchCommand, Command: "workspace previous"},
	{ID: "workspace-rename", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "r", Intent: IntentWorkbenchCommand, Command: "workspace rename"},
	{ID: "workspace-delete-tuiv2", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "x", Intent: IntentWorkbenchCommand, Command: "workspace delete confirm=accepted"},
	{ID: "workspace-tree", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "t", Intent: IntentShellAction, Action: ShellActionOpenTree},
	{ID: "workspace-tree-tuiv2", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "f", Intent: IntentShellAction, Action: ShellActionOpenTree},
	{ID: "workspace-tree-tuiv2-alias", Mode: InteractionModeWorkspace, Key: KeyChar, Char: "s", Intent: IntentShellAction, Action: ShellActionOpenTree},
}

func BindingCatalog() []Binding {
	out := make([]Binding, len(bindingCatalog))
	copy(out, bindingCatalog)
	return out
}

func routeKey(event InputEvent, options RouteOptions) Intent {
	if event.Key == KeyEsc {
		if options.Mode != InteractionModeNormal {
			return Intent{Kind: IntentExitInteraction, Event: event}
		}
		return Intent{Kind: IntentTerminalInput, Event: event, Bytes: []byte{'\x1b'}}
	}
	if binding, ok := lookupBinding(options.Mode, event); ok {
		return intentFromBinding(event, binding)
	}
	if options.Mode != InteractionModeNormal {
		if binding, ok := lookupBinding(InteractionModeNormal, event); ok {
			return intentFromBinding(event, binding)
		}
	}
	if options.Mode != InteractionModeNormal {
		return Intent{Kind: IntentNone, Event: event}
	}
	if data := terminalBytes(event); len(data) > 0 {
		return Intent{Kind: IntentTerminalInput, Event: event, Bytes: data}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func routeMouse(event InputEvent, options RouteOptions) Intent {
	if options.TerminalMousePassthrough && event.RawSeq != "" {
		return Intent{Kind: IntentTerminalInput, Event: event, Bytes: []byte(event.RawSeq), RawMouse: true}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func lookupBinding(mode InteractionMode, event InputEvent) (Binding, bool) {
	for _, binding := range bindingCatalog {
		if binding.Mode == mode && bindingMatches(binding, event) {
			return binding, true
		}
	}
	return Binding{}, false
}

func bindingMatches(binding Binding, event InputEvent) bool {
	if binding.Key != event.Key {
		return false
	}
	if binding.Char != event.Char {
		return false
	}
	return binding.Ctrl == event.Ctrl && binding.Alt == event.Alt && binding.Shift == event.Shift
}

func intentFromBinding(event InputEvent, binding Binding) Intent {
	return Intent{
		Kind:    binding.Intent,
		Event:   event,
		Command: binding.Command,
		Action:  binding.Action,
		Reason:  binding.Reason,
		Mode:    binding.Target,
	}
}

func terminalBytes(event InputEvent) []byte {
	if event.RawSeq != "" {
		return []byte(event.RawSeq)
	}
	switch event.Key {
	case KeyEnter:
		return []byte{'\r'}
	case KeyBackspace:
		return []byte{0x7f}
	case KeyTab:
		return []byte{'\t'}
	case KeyChar:
		if event.Char != "" {
			return []byte(event.Char)
		}
	}
	return nil
}
