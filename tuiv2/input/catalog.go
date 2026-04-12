package input

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// BindingDoc is the canonical keybinding definition for tuiv2.
// Stable IDs/config keys reserve space for future user overrides while keeping
// router/help/status/footer on one source of truth.
type BindingDoc struct {
	ID         string
	ConfigKey  string
	Mode       ModeKind
	Group      string
	Binding    Binding
	KeyLabel   string
	HelpText   string
	StatusText string
	FooterText string
}

type HelpBindingDoc struct {
	Key    string
	Action string
}

type HelpSectionDoc struct {
	Title    string
	Bindings []HelpBindingDoc
}

var helpSectionOrder = []string{
	"Most Used",
	"Pane",
	"Tab",
	"Workspace",
	"Floating",
	"Shared Terminal",
	"Resize & Display",
	"Global",
	"Exit / Close",
}

func DefaultBindingCatalog() []BindingDoc {
	return []BindingDoc{
		{ID: "root-pane", ConfigKey: "root.pane", Mode: ModeNormal, Group: "Most Used", Binding: Binding{Type: tea.KeyCtrlP, Action: ActionEnterPaneMode}, KeyLabel: "Ctrl-P", HelpText: "pane mode — focus, split, zoom, close", StatusText: "P PANE"},
		{ID: "root-resize", ConfigKey: "root.resize", Mode: ModeNormal, Group: "Most Used", Binding: Binding{Type: tea.KeyCtrlR, Action: ActionEnterResizeMode}, KeyLabel: "Ctrl-R", HelpText: "resize mode — resize panes and layout", StatusText: "R RESIZE"},
		{ID: "root-tab", ConfigKey: "root.tab", Mode: ModeNormal, Group: "Most Used", Binding: Binding{Type: tea.KeyCtrlT, Action: ActionEnterTabMode}, KeyLabel: "Ctrl-T", HelpText: "tab mode — create, jump, rename, kill", StatusText: "T TAB"},
		{ID: "root-workspace", ConfigKey: "root.workspace", Mode: ModeNormal, Group: "Most Used", Binding: Binding{Type: tea.KeyCtrlW, Action: ActionEnterWorkspaceMode}, KeyLabel: "Ctrl-W", HelpText: "workspace mode — create, switch, rename, delete", StatusText: "W WORKSPACE"},
		{ID: "root-floating", ConfigKey: "root.floating", Mode: ModeNormal, Group: "Most Used", Binding: Binding{Type: tea.KeyCtrlO, Action: ActionEnterFloatingMode}, KeyLabel: "Ctrl-O", HelpText: "floating mode — move, resize, toggle, close", StatusText: "O FLOAT"},
		{ID: "root-display", ConfigKey: "root.display", Mode: ModeNormal, Group: "Most Used", Binding: Binding{Type: tea.KeyCtrlV, Action: ActionEnterDisplayMode}, KeyLabel: "Ctrl-V", HelpText: "copy mode — scrollback, select, copy", StatusText: "V COPY"},
		{ID: "root-picker", ConfigKey: "root.picker", Mode: ModeNormal, Group: "Most Used", Binding: Binding{Type: tea.KeyCtrlF, Action: ActionOpenPicker}, KeyLabel: "Ctrl-F", HelpText: "terminal picker — attach or create terminal", StatusText: "F PICKER"},
		{ID: "root-global", ConfigKey: "root.global", Mode: ModeNormal, Group: "Most Used", Binding: Binding{Type: tea.KeyCtrlG, Action: ActionEnterGlobalMode}, KeyLabel: "Ctrl-G", HelpText: "global mode — help, terminal pool, quit", StatusText: "G GLOBAL"},

		{ID: "pane-enter", ConfigKey: "pane.enter", Mode: ModePane, Group: "Pane", Binding: Binding{Type: tea.KeyCtrlP, Action: ActionEnterPaneMode}, KeyLabel: "Ctrl-P", HelpText: "enter pane mode"},
		{ID: "pane-focus-left", ConfigKey: "pane.focus.left", Mode: ModePane, Group: "Pane", Binding: Binding{Type: tea.KeyRunes, Rune: 'h', Action: ActionFocusPaneLeft}, KeyLabel: "h/j/k/l or arrows", HelpText: "focus pane (left/down/up/right)", StatusText: "h/j/k/l FOCUS"},
		{ID: "pane-focus-down", ConfigKey: "pane.focus.down", Mode: ModePane, Binding: Binding{Type: tea.KeyRunes, Rune: 'j', Action: ActionFocusPaneDown}},
		{ID: "pane-focus-up", ConfigKey: "pane.focus.up", Mode: ModePane, Binding: Binding{Type: tea.KeyRunes, Rune: 'k', Action: ActionFocusPaneUp}},
		{ID: "pane-focus-right", ConfigKey: "pane.focus.right", Mode: ModePane, Binding: Binding{Type: tea.KeyRunes, Rune: 'l', Action: ActionFocusPaneRight}},
		{ID: "pane-focus-arrow-left", ConfigKey: "pane.focus.arrow_left", Mode: ModePane, Binding: Binding{Type: tea.KeyLeft, Action: ActionFocusPaneLeft}},
		{ID: "pane-focus-arrow-down", ConfigKey: "pane.focus.arrow_down", Mode: ModePane, Binding: Binding{Type: tea.KeyDown, Action: ActionFocusPaneDown}},
		{ID: "pane-focus-arrow-up", ConfigKey: "pane.focus.arrow_up", Mode: ModePane, Binding: Binding{Type: tea.KeyUp, Action: ActionFocusPaneUp}},
		{ID: "pane-focus-arrow-right", ConfigKey: "pane.focus.arrow_right", Mode: ModePane, Binding: Binding{Type: tea.KeyRight, Action: ActionFocusPaneRight}},
		{ID: "pane-split-vertical", ConfigKey: "pane.split.vertical", Mode: ModePane, Group: "Pane", Binding: Binding{Type: tea.KeyRunes, Rune: '%', Action: ActionSplitPane}, KeyLabel: "% or Ctrl-D", HelpText: "split pane vertically", StatusText: "% VSPLIT"},
		{ID: "pane-split-vertical-alias", ConfigKey: "pane.split.vertical_alias", Mode: ModePane, Binding: Binding{Type: tea.KeyCtrlD, Action: ActionSplitPane}},
		{ID: "pane-split-horizontal", ConfigKey: "pane.split.horizontal", Mode: ModePane, Group: "Pane", Binding: Binding{Type: tea.KeyRunes, Rune: '"', Action: ActionSplitPaneHorizontal}, KeyLabel: "\" or Ctrl-E", HelpText: "split pane horizontally", StatusText: "\" HSPLIT"},
		{ID: "pane-split-horizontal-alias", ConfigKey: "pane.split.horizontal_alias", Mode: ModePane, Binding: Binding{Type: tea.KeyCtrlE, Action: ActionSplitPaneHorizontal}},
		{ID: "pane-detach", ConfigKey: "pane.detach", Mode: ModePane, Group: "Pane", Binding: Binding{Type: tea.KeyRunes, Rune: 'd', Action: ActionDetachPane}, KeyLabel: "d", HelpText: "detach pane from current terminal", StatusText: "d DETACH"},
		{ID: "pane-reconnect", ConfigKey: "pane.reconnect", Mode: ModePane, Group: "Pane", Binding: Binding{Type: tea.KeyRunes, Rune: 'r', Action: ActionReconnectPane}, KeyLabel: "r", HelpText: "reconnect pane via terminal picker", StatusText: "r RECONNECT"},
		{ID: "pane-restart", ConfigKey: "pane.restart", Mode: ModePane, Group: "Pane", Binding: Binding{Type: tea.KeyRunes, Rune: 'R', Action: ActionRestartTerminal}, KeyLabel: "R", HelpText: "restart exited terminal in this pane", StatusText: "R RESTART"},
		{ID: "pane-owner", ConfigKey: "pane.owner", Mode: ModePane, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyRunes, Rune: 'a', Action: ActionBecomeOwner}, KeyLabel: "a", HelpText: "take terminal ownership for active pane", StatusText: "a OWNER"},
		{ID: "pane-size-lock", ConfigKey: "pane.size_lock", Mode: ModePane, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyRunes, Rune: 's', Action: ActionToggleTerminalSizeLock}, KeyLabel: "s", HelpText: "toggle terminal size lock for active pane", StatusText: "s LOCK"},
		{ID: "pane-kill", ConfigKey: "pane.kill", Mode: ModePane, Group: "Pane", Binding: Binding{Type: tea.KeyRunes, Rune: 'X', Action: ActionClosePaneKill}, KeyLabel: "X", HelpText: "close pane and kill terminal", StatusText: "X CLOSE+KILL"},
		{ID: "pane-zoom", ConfigKey: "pane.zoom", Mode: ModePane, Group: "Pane", Binding: Binding{Type: tea.KeyRunes, Rune: 'z', Action: ActionZoomPane}, KeyLabel: "z", HelpText: "zoom pane", StatusText: "z ZOOM"},
		{ID: "pane-close", ConfigKey: "pane.close", Mode: ModePane, Group: "Pane", Binding: Binding{Type: tea.KeyRunes, Rune: 'w', Action: ActionClosePane}, KeyLabel: "w", HelpText: "close pane", StatusText: "w CLOSE"},
		{ID: "pane-exit", ConfigKey: "pane.exit", Mode: ModePane, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", HelpText: "exit current mode or close modal", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "resize-enter", ConfigKey: "resize.enter", Mode: ModeResize, Group: "Resize & Display", Binding: Binding{Type: tea.KeyCtrlR, Action: ActionEnterResizeMode}, KeyLabel: "Ctrl-R", HelpText: "enter resize mode"},
		{ID: "resize-small-left", ConfigKey: "resize.small.left", Mode: ModeResize, Group: "Resize & Display", Binding: Binding{Type: tea.KeyRunes, Rune: 'h', Action: ActionResizePaneLeft}, KeyLabel: "h/j/k/l or arrows", HelpText: "resize pane (small step)", StatusText: "h/j/k/l RESIZE"},
		{ID: "resize-small-down", ConfigKey: "resize.small.down", Mode: ModeResize, Binding: Binding{Type: tea.KeyRunes, Rune: 'j', Action: ActionResizePaneDown}},
		{ID: "resize-small-up", ConfigKey: "resize.small.up", Mode: ModeResize, Binding: Binding{Type: tea.KeyRunes, Rune: 'k', Action: ActionResizePaneUp}},
		{ID: "resize-small-right", ConfigKey: "resize.small.right", Mode: ModeResize, Binding: Binding{Type: tea.KeyRunes, Rune: 'l', Action: ActionResizePaneRight}},
		{ID: "resize-arrow-left", ConfigKey: "resize.arrow.left", Mode: ModeResize, Binding: Binding{Type: tea.KeyLeft, Action: ActionResizePaneLeft}},
		{ID: "resize-arrow-down", ConfigKey: "resize.arrow.down", Mode: ModeResize, Binding: Binding{Type: tea.KeyDown, Action: ActionResizePaneDown}},
		{ID: "resize-arrow-up", ConfigKey: "resize.arrow.up", Mode: ModeResize, Binding: Binding{Type: tea.KeyUp, Action: ActionResizePaneUp}},
		{ID: "resize-arrow-right", ConfigKey: "resize.arrow.right", Mode: ModeResize, Binding: Binding{Type: tea.KeyRight, Action: ActionResizePaneRight}},
		{ID: "resize-large-left", ConfigKey: "resize.large.left", Mode: ModeResize, Group: "Resize & Display", Binding: Binding{Type: tea.KeyRunes, Rune: 'H', Action: ActionResizePaneLargeLeft}, KeyLabel: "H/J/K/L", HelpText: "resize pane (large step)", StatusText: "H/J/K/L RESIZEx2"},
		{ID: "resize-large-down", ConfigKey: "resize.large.down", Mode: ModeResize, Binding: Binding{Type: tea.KeyRunes, Rune: 'J', Action: ActionResizePaneLargeDown}},
		{ID: "resize-large-up", ConfigKey: "resize.large.up", Mode: ModeResize, Binding: Binding{Type: tea.KeyRunes, Rune: 'K', Action: ActionResizePaneLargeUp}},
		{ID: "resize-large-right", ConfigKey: "resize.large.right", Mode: ModeResize, Binding: Binding{Type: tea.KeyRunes, Rune: 'L', Action: ActionResizePaneLargeRight}},
		{ID: "resize-owner", ConfigKey: "resize.owner", Mode: ModeResize, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyRunes, Rune: 'a', Action: ActionBecomeOwner}, KeyLabel: "a", HelpText: "take terminal ownership for active pane", StatusText: "a OWNER"},
		{ID: "resize-size-lock", ConfigKey: "resize.size_lock", Mode: ModeResize, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyRunes, Rune: 's', Action: ActionToggleTerminalSizeLock}, KeyLabel: "s", HelpText: "toggle terminal size lock for active pane", StatusText: "s LOCK"},
		{ID: "resize-balance", ConfigKey: "resize.balance", Mode: ModeResize, Group: "Resize & Display", Binding: Binding{Type: tea.KeyRunes, Rune: '=', Action: ActionBalancePanes}, KeyLabel: "=", HelpText: "balance panes", StatusText: "= BALANCE"},
		{ID: "resize-layout", ConfigKey: "resize.layout", Mode: ModeResize, Group: "Resize & Display", Binding: Binding{Type: tea.KeySpace, Action: ActionCycleLayout}, KeyLabel: "Space", HelpText: "cycle layout", StatusText: "Space LAYOUT"},
		{ID: "resize-exit", ConfigKey: "resize.exit", Mode: ModeResize, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "tab-enter", ConfigKey: "tab.enter", Mode: ModeTab, Group: "Tab", Binding: Binding{Type: tea.KeyCtrlT, Action: ActionEnterTabMode}, KeyLabel: "Ctrl-T", HelpText: "enter tab mode"},
		{ID: "tab-create", ConfigKey: "tab.create", Mode: ModeTab, Group: "Tab", Binding: Binding{Type: tea.KeyRunes, Rune: 'c', Action: ActionCreateTab}, KeyLabel: "c", HelpText: "create new tab", StatusText: "C NEW"},
		{ID: "tab-rename", ConfigKey: "tab.rename", Mode: ModeTab, Group: "Tab", Binding: Binding{Type: tea.KeyRunes, Rune: 'r', Action: ActionRenameTab}, KeyLabel: "r", HelpText: "rename current tab", StatusText: "R RENAME"},
		{ID: "tab-next", ConfigKey: "tab.next", Mode: ModeTab, Group: "Tab", Binding: Binding{Type: tea.KeyRunes, Rune: 'n', Action: ActionNextTab}, KeyLabel: "n/p", HelpText: "next/previous tab", StatusText: "N/P NEXT/PREV"},
		{ID: "tab-prev", ConfigKey: "tab.prev", Mode: ModeTab, Binding: Binding{Type: tea.KeyRunes, Rune: 'p', Action: ActionPrevTab}},
		{ID: "tab-jump", ConfigKey: "tab.jump", Mode: ModeTab, Group: "Tab", Binding: Binding{Type: tea.KeyRunes, RuneMin: '1', RuneMax: '9', Action: ActionJumpTab, TextFromRune: true}, KeyLabel: "1-9", HelpText: "jump to tab number", StatusText: "1-9 JUMP"},
		{ID: "tab-kill", ConfigKey: "tab.kill", Mode: ModeTab, Group: "Tab", Binding: Binding{Type: tea.KeyRunes, Rune: 'x', Action: ActionKillTab}, KeyLabel: "x", HelpText: "kill terminals in current tab and close it", StatusText: "X KILL"},
		{ID: "tab-exit", ConfigKey: "tab.exit", Mode: ModeTab, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "workspace-enter", ConfigKey: "workspace.enter", Mode: ModeWorkspace, Group: "Workspace", Binding: Binding{Type: tea.KeyCtrlW, Action: ActionEnterWorkspaceMode}, KeyLabel: "Ctrl-W", HelpText: "enter workspace mode"},
		{ID: "workspace-picker", ConfigKey: "workspace.picker", Mode: ModeWorkspace, Group: "Workspace", Binding: Binding{Type: tea.KeyRunes, Rune: 'f', Action: ActionOpenWorkspacePicker}, KeyLabel: "f or s", HelpText: "workspace picker", StatusText: "F PICK"},
		{ID: "workspace-picker-alias", ConfigKey: "workspace.picker_alias", Mode: ModeWorkspace, Binding: Binding{Type: tea.KeyRunes, Rune: 's', Action: ActionOpenWorkspacePicker}},
		{ID: "workspace-create", ConfigKey: "workspace.create", Mode: ModeWorkspace, Group: "Workspace", Binding: Binding{Type: tea.KeyRunes, Rune: 'c', Action: ActionCreateWorkspace}, KeyLabel: "c", HelpText: "create new workspace", StatusText: "C NEW"},
		{ID: "workspace-rename", ConfigKey: "workspace.rename", Mode: ModeWorkspace, Group: "Workspace", Binding: Binding{Type: tea.KeyRunes, Rune: 'r', Action: ActionRenameWorkspace}, KeyLabel: "r", HelpText: "rename current workspace", StatusText: "R RENAME"},
		{ID: "workspace-delete", ConfigKey: "workspace.delete", Mode: ModeWorkspace, Group: "Workspace", Binding: Binding{Type: tea.KeyRunes, Rune: 'x', Action: ActionDeleteWorkspace}, KeyLabel: "x", HelpText: "delete current workspace", StatusText: "X DELETE"},
		{ID: "workspace-next", ConfigKey: "workspace.next", Mode: ModeWorkspace, Group: "Workspace", Binding: Binding{Type: tea.KeyRunes, Rune: 'n', Action: ActionNextWorkspace}, KeyLabel: "n/p", HelpText: "next/previous workspace", StatusText: "N/P NEXT/PREV"},
		{ID: "workspace-prev", ConfigKey: "workspace.prev", Mode: ModeWorkspace, Binding: Binding{Type: tea.KeyRunes, Rune: 'p', Action: ActionPrevWorkspace}},
		{ID: "workspace-exit", ConfigKey: "workspace.exit", Mode: ModeWorkspace, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "floating-enter", ConfigKey: "floating.enter", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyCtrlO, Action: ActionEnterFloatingMode}, KeyLabel: "Ctrl-O", HelpText: "enter floating mode"},
		{ID: "floating-create", ConfigKey: "floating.create", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'n', Action: ActionCreateFloatingPane}, KeyLabel: "n", HelpText: "create new floating pane", StatusText: "N NEW FLOAT"},
		{ID: "floating-cycle-next", ConfigKey: "floating.cycle_next", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyTab, Action: ActionFocusNextFloatingPane}, KeyLabel: "Tab / Shift-Tab", HelpText: "cycle floating pane focus"},
		{ID: "floating-cycle-prev", ConfigKey: "floating.cycle_prev", Mode: ModeFloating, Binding: Binding{Type: tea.KeyShiftTab, Action: ActionFocusPrevFloatingPane}},
		{ID: "floating-move-left", ConfigKey: "floating.move.left", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'h', Action: ActionMoveFloatingLeft}, KeyLabel: "h/j/k/l", HelpText: "move floating pane", StatusText: "h/j/k/l MOVE"},
		{ID: "floating-move-down", ConfigKey: "floating.move.down", Mode: ModeFloating, Binding: Binding{Type: tea.KeyRunes, Rune: 'j', Action: ActionMoveFloatingDown}},
		{ID: "floating-move-up", ConfigKey: "floating.move.up", Mode: ModeFloating, Binding: Binding{Type: tea.KeyRunes, Rune: 'k', Action: ActionMoveFloatingUp}},
		{ID: "floating-move-right", ConfigKey: "floating.move.right", Mode: ModeFloating, Binding: Binding{Type: tea.KeyRunes, Rune: 'l', Action: ActionMoveFloatingRight}},
		{ID: "floating-resize-left", ConfigKey: "floating.resize.left", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'H', Action: ActionResizeFloatingLeft}, KeyLabel: "H/J/K/L", HelpText: "resize floating pane", StatusText: "H/J/K/L RESIZE"},
		{ID: "floating-resize-down", ConfigKey: "floating.resize.down", Mode: ModeFloating, Binding: Binding{Type: tea.KeyRunes, Rune: 'J', Action: ActionResizeFloatingDown}},
		{ID: "floating-resize-up", ConfigKey: "floating.resize.up", Mode: ModeFloating, Binding: Binding{Type: tea.KeyRunes, Rune: 'K', Action: ActionResizeFloatingUp}},
		{ID: "floating-resize-right", ConfigKey: "floating.resize.right", Mode: ModeFloating, Binding: Binding{Type: tea.KeyRunes, Rune: 'L', Action: ActionResizeFloatingRight}},
		{ID: "floating-center", ConfigKey: "floating.center", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'c', Action: ActionCenterFloatingPane}, KeyLabel: "c", HelpText: "center selected floating pane", StatusText: "c CENTER"},
		{ID: "floating-collapse", ConfigKey: "floating.collapse", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'm', Action: ActionCollapseFloatingPane}, KeyLabel: "m", HelpText: "collapse selected floating pane into overview", StatusText: "m COLLAPSE"},
		{ID: "floating-overview", ConfigKey: "floating.overview", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'o', Action: ActionOpenFloatingOverview}, KeyLabel: "o", HelpText: "open floating overview", StatusText: "o OVERVIEW"},
		{ID: "floating-summon", ConfigKey: "floating.summon", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, RuneMin: '1', RuneMax: '9', Action: ActionSummonFloatingPane, TextFromRune: true}, KeyLabel: "1-9", HelpText: "summon recent floating pane by slot", StatusText: "1-9 SUMMON"},
		{ID: "floating-owner", ConfigKey: "floating.owner", Mode: ModeFloating, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyRunes, Rune: 'a', Action: ActionBecomeOwner}, KeyLabel: "a", HelpText: "take terminal ownership for active pane", StatusText: "a OWNER"},
		{ID: "floating-toggle", ConfigKey: "floating.toggle", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'v', Action: ActionToggleFloatingVisibility}, KeyLabel: "v", HelpText: "toggle collapse or restore all floating panes", StatusText: "v ALL"},
		{ID: "floating-fit-once", ConfigKey: "floating.fit_once", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: '=', Action: ActionAutoFitFloatingPane}, KeyLabel: "=", HelpText: "fit floating pane to current content", StatusText: "= FIT"},
		{ID: "floating-fit-auto", ConfigKey: "floating.fit_auto", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 's', Action: ActionToggleFloatingAutoFit}, KeyLabel: "s", HelpText: "toggle floating auto-fit", StatusText: "s AUTO-FIT"},
		{ID: "floating-close", ConfigKey: "floating.close", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'x', Action: ActionCloseFloatingPane}, KeyLabel: "x", HelpText: "close active floating pane", StatusText: "x CLOSE"},
		{ID: "floating-picker", ConfigKey: "floating.picker", Mode: ModeFloating, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'f', Action: ActionOpenPicker}, KeyLabel: "f", HelpText: "open terminal picker for active floating pane", StatusText: "f PICK"},
		{ID: "floating-exit", ConfigKey: "floating.exit", Mode: ModeFloating, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "floating-overview-up", ConfigKey: "floating_overview.up", Mode: ModeFloatingOverview, Group: "Floating", Binding: Binding{Type: tea.KeyUp, Action: ActionPickerUp}, KeyLabel: "Up/Down", HelpText: "move floating selection", StatusText: "UP/DOWN MOVE"},
		{ID: "floating-overview-down", ConfigKey: "floating_overview.down", Mode: ModeFloatingOverview, Binding: Binding{Type: tea.KeyDown, Action: ActionPickerDown}},
		{ID: "floating-overview-up-alt", ConfigKey: "floating_overview.up_alt", Mode: ModeFloatingOverview, Binding: Binding{Type: tea.KeyRunes, Rune: 'k', Action: ActionPickerUp}},
		{ID: "floating-overview-down-alt", ConfigKey: "floating_overview.down_alt", Mode: ModeFloatingOverview, Binding: Binding{Type: tea.KeyRunes, Rune: 'j', Action: ActionPickerDown}},
		{ID: "floating-overview-open", ConfigKey: "floating_overview.open", Mode: ModeFloatingOverview, Group: "Floating", Binding: Binding{Type: tea.KeyEnter, Action: ActionSubmitPrompt}, KeyLabel: "Enter", HelpText: "restore and focus selected floating pane", StatusText: "Enter OPEN", FooterText: "open"},
		{ID: "floating-overview-show-all", ConfigKey: "floating_overview.show_all", Mode: ModeFloatingOverview, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 's', Action: ActionExpandAllFloatingPanes}, KeyLabel: "s", HelpText: "expand all floating panes", StatusText: "s SHOW ALL", FooterText: "show-all"},
		{ID: "floating-overview-collapse-all", ConfigKey: "floating_overview.collapse_all", Mode: ModeFloatingOverview, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'c', Action: ActionCollapseAllFloatingPanes}, KeyLabel: "c", HelpText: "collapse all floating panes", StatusText: "c COLLAPSE ALL", FooterText: "collapse-all"},
		{ID: "floating-overview-close", ConfigKey: "floating_overview.close", Mode: ModeFloatingOverview, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, Rune: 'x', Action: ActionCloseFloatingPane}, KeyLabel: "x", HelpText: "close selected floating pane", StatusText: "x CLOSE", FooterText: "close-pane"},
		{ID: "floating-overview-summon", ConfigKey: "floating_overview.summon", Mode: ModeFloatingOverview, Group: "Floating", Binding: Binding{Type: tea.KeyRunes, RuneMin: '1', RuneMax: '9', Action: ActionSummonFloatingPane, TextFromRune: true}, KeyLabel: "1-9", HelpText: "open a floating pane by slot", StatusText: "1-9 SUMMON"},
		{ID: "floating-overview-exit", ConfigKey: "floating_overview.exit", Mode: ModeFloatingOverview, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "display-enter", ConfigKey: "display.enter", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeyCtrlV, Action: ActionEnterDisplayMode}, KeyLabel: "Ctrl-V", HelpText: "enter copy mode"},
		{ID: "display-up", ConfigKey: "display.up", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeyUp, Action: ActionCopyModeCursorUp}, KeyLabel: "arrows or j/k/l", HelpText: "move copy cursor", StatusText: "MOVE CURSOR"},
		{ID: "display-down", ConfigKey: "display.down", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyDown, Action: ActionCopyModeCursorDown}},
		{ID: "display-left", ConfigKey: "display.left", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyLeft, Action: ActionCopyModeCursorLeft}},
		{ID: "display-right", ConfigKey: "display.right", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyRight, Action: ActionCopyModeCursorRight}},
		{ID: "display-up-alt", ConfigKey: "display.up_alt", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyRunes, Rune: 'k', Action: ActionCopyModeCursorUp}},
		{ID: "display-down-alt", ConfigKey: "display.down_alt", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyRunes, Rune: 'j', Action: ActionCopyModeCursorDown}},
		{ID: "display-right-alt", ConfigKey: "display.right_alt", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyRunes, Rune: 'l', Action: ActionCopyModeCursorRight}},
		{ID: "display-page-up", ConfigKey: "display.page_up", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeyPgUp, Action: ActionCopyModePageUp}, KeyLabel: "PgUp/PgDn", HelpText: "page up/down", StatusText: "PG SCROLL"},
		{ID: "display-page-down", ConfigKey: "display.page_down", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyPgDown, Action: ActionCopyModePageDown}},
		{ID: "display-half-up", ConfigKey: "display.half_up", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeyRunes, Rune: 'u', Action: ActionCopyModeHalfPageUp}, KeyLabel: "u/d", HelpText: "half-page up/down", StatusText: "u/d HALF"},
		{ID: "display-half-down", ConfigKey: "display.half_down", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyRunes, Rune: 'd', Action: ActionCopyModeHalfPageDown}},
		{ID: "display-line-start", ConfigKey: "display.line_start", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeyHome, Action: ActionCopyModeStartOfLine}, KeyLabel: "Home/End", HelpText: "line start/end", StatusText: "HOME/END LINE"},
		{ID: "display-line-end", ConfigKey: "display.line_end", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyEnd, Action: ActionCopyModeEndOfLine}},
		{ID: "display-top", ConfigKey: "display.top", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeyRunes, Rune: 'g', Action: ActionCopyModeTop}, KeyLabel: "g/G", HelpText: "top/bottom", StatusText: "g/G EDGE"},
		{ID: "display-bottom", ConfigKey: "display.bottom", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyRunes, Rune: 'G', Action: ActionCopyModeBottom}},
		{ID: "display-select", ConfigKey: "display.select", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeySpace, Action: ActionCopyModeBeginSelection}, KeyLabel: "Space", HelpText: "mark or copy selection", StatusText: "Space MARK/COPY"},
		{ID: "display-copy", ConfigKey: "display.copy", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeyRunes, Rune: 'y', Action: ActionCopyModeCopySelection}, KeyLabel: "y or Enter", HelpText: "copy selection", StatusText: "y COPY"},
		{ID: "display-copy-enter", ConfigKey: "display.copy_enter", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyEnter, Action: ActionCopyModeCopySelectionExit}},
		{ID: "display-paste-buffer", ConfigKey: "display.paste_buffer", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeyRunes, Rune: 'p', Action: ActionPasteBuffer}, KeyLabel: "p/P", HelpText: "paste last copy or system clipboard", StatusText: "p/P PASTE"},
		{ID: "display-paste-clipboard", ConfigKey: "display.paste_clipboard", Mode: ModeDisplay, Binding: Binding{Type: tea.KeyRunes, Rune: 'P', Action: ActionPasteClipboard}},
		{ID: "display-history", ConfigKey: "display.history", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeyRunes, Rune: 'h', Action: ActionOpenClipboardHistory}, KeyLabel: "h", HelpText: "open clipboard history", StatusText: "h HISTORY"},
		{ID: "display-zoom", ConfigKey: "display.zoom", Mode: ModeDisplay, Group: "Resize & Display", Binding: Binding{Type: tea.KeyRunes, Rune: 'z', Action: ActionZoomPane}, KeyLabel: "z", HelpText: "zoom pane", StatusText: "z ZOOM"},
		{ID: "display-exit", ConfigKey: "display.exit", Mode: ModeDisplay, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "global-enter", ConfigKey: "global.enter", Mode: ModeGlobal, Group: "Global", Binding: Binding{Type: tea.KeyCtrlG, Action: ActionEnterGlobalMode}, KeyLabel: "Ctrl-G", HelpText: "enter global mode"},
		{ID: "global-help", ConfigKey: "global.help", Mode: ModeGlobal, Group: "Global", Binding: Binding{Type: tea.KeyRunes, Rune: '?', Action: ActionOpenHelp}, KeyLabel: "?", HelpText: "show help", StatusText: "? HELP"},
		{ID: "global-manager", ConfigKey: "global.manager", Mode: ModeGlobal, Group: "Global", Binding: Binding{Type: tea.KeyRunes, Rune: 't', Action: ActionOpenTerminalManager}, KeyLabel: "t", HelpText: "open terminal pool", StatusText: "t TERMINALS"},
		{ID: "global-quit", ConfigKey: "global.quit", Mode: ModeGlobal, Group: "Global", Binding: Binding{Type: tea.KeyRunes, Rune: 'q', Action: ActionQuit}, KeyLabel: "q", HelpText: "quit termx", StatusText: "q QUIT"},
		{ID: "global-exit", ConfigKey: "global.exit", Mode: ModeGlobal, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "picker-up", ConfigKey: "picker.up", Mode: ModePicker, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyUp, Action: ActionPickerUp}, StatusText: "UP/DOWN MOVE"},
		{ID: "picker-down", ConfigKey: "picker.down", Mode: ModePicker, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyDown, Action: ActionPickerDown}},
		{ID: "picker-filter", ConfigKey: "picker.filter", Mode: ModePicker, Group: "Shared Terminal", KeyLabel: "type", HelpText: "filter picker and terminal-pool results", StatusText: "TYPE FILTER"},
		{ID: "picker-enter", ConfigKey: "picker.enter", Mode: ModePicker, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyEnter, Action: ActionSubmitPrompt}, KeyLabel: "Enter", HelpText: "attach terminal to current pane", StatusText: "Enter HERE", FooterText: "attach"},
		{ID: "picker-split", ConfigKey: "picker.split", Mode: ModePicker, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyTab, Action: ActionPickerAttachSplit}, KeyLabel: "Tab", HelpText: "split current pane and attach selected terminal", StatusText: "Tab SPLIT", FooterText: "split+attach"},
		{ID: "picker-edit", ConfigKey: "picker.edit", Mode: ModePicker, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyCtrlE, Action: ActionEditTerminal}, KeyLabel: "Ctrl-E", HelpText: "edit terminal metadata", StatusText: "Ctrl-E EDIT", FooterText: "edit"},
		{ID: "picker-kill", ConfigKey: "picker.kill", Mode: ModePicker, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyCtrlK, Action: ActionKillTerminal}, KeyLabel: "Ctrl-K", HelpText: "kill terminal", StatusText: "Ctrl-K KILL", FooterText: "kill"},
		{ID: "picker-exit", ConfigKey: "picker.exit", Mode: ModePicker, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "terminal-manager-up", ConfigKey: "terminal_manager.up", Mode: ModeTerminalManager, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyUp, Action: ActionPickerUp}, StatusText: "UP/DOWN MOVE"},
		{ID: "terminal-manager-down", ConfigKey: "terminal_manager.down", Mode: ModeTerminalManager, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyDown, Action: ActionPickerDown}},
		{ID: "terminal-manager-filter", ConfigKey: "terminal_manager.filter", Mode: ModeTerminalManager, Group: "Shared Terminal", KeyLabel: "type", HelpText: "filter picker and terminal-pool results", StatusText: "TYPE FILTER"},
		{ID: "terminal-manager-enter", ConfigKey: "terminal_manager.enter", Mode: ModeTerminalManager, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyEnter, Action: ActionSubmitPrompt}, KeyLabel: "Enter", HelpText: "attach terminal to current pane", StatusText: "Enter HERE", FooterText: "here"},
		{ID: "terminal-manager-tab", ConfigKey: "terminal_manager.tab", Mode: ModeTerminalManager, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyCtrlT, Action: ActionAttachTab}, KeyLabel: "Ctrl-T", StatusText: "Ctrl-T TAB", FooterText: "tab"},
		{ID: "terminal-manager-floating", ConfigKey: "terminal_manager.floating", Mode: ModeTerminalManager, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyCtrlO, Action: ActionAttachFloating}, KeyLabel: "Ctrl-O", StatusText: "Ctrl-O FLOAT", FooterText: "float"},
		{ID: "terminal-manager-edit", ConfigKey: "terminal_manager.edit", Mode: ModeTerminalManager, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyCtrlE, Action: ActionEditTerminal}, KeyLabel: "Ctrl-E", StatusText: "Ctrl-E EDIT", FooterText: "edit"},
		{ID: "terminal-manager-kill", ConfigKey: "terminal_manager.kill", Mode: ModeTerminalManager, Group: "Shared Terminal", Binding: Binding{Type: tea.KeyCtrlK, Action: ActionKillTerminal}, KeyLabel: "Ctrl-K", StatusText: "Ctrl-K KILL", FooterText: "kill"},
		{ID: "terminal-manager-exit", ConfigKey: "terminal_manager.exit", Mode: ModeTerminalManager, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "workspace-picker-up", ConfigKey: "workspace_picker.up", Mode: ModeWorkspacePicker, Group: "Workspace", Binding: Binding{Type: tea.KeyUp, Action: ActionPickerUp}, StatusText: "UP/DOWN MOVE"},
		{ID: "workspace-picker-down", ConfigKey: "workspace_picker.down", Mode: ModeWorkspacePicker, Group: "Workspace", Binding: Binding{Type: tea.KeyDown, Action: ActionPickerDown}},
		{ID: "workspace-picker-filter", ConfigKey: "workspace_picker.filter", Mode: ModeWorkspacePicker, Group: "Workspace", KeyLabel: "type", HelpText: "filter workspace results", StatusText: "TYPE FILTER"},
		{ID: "workspace-picker-enter", ConfigKey: "workspace_picker.enter", Mode: ModeWorkspacePicker, Group: "Workspace", Binding: Binding{Type: tea.KeyEnter, Action: ActionSubmitPrompt}, KeyLabel: "Enter", HelpText: "open selected item", StatusText: "Enter OPEN"},
		{ID: "workspace-picker-create", ConfigKey: "workspace_picker.create", Mode: ModeWorkspacePicker, Group: "Workspace", Binding: Binding{Type: tea.KeyCtrlN, Action: ActionCreateWorkspace}, KeyLabel: "Ctrl-N", HelpText: "create workspace", StatusText: "Ctrl-N NEW", FooterText: "new"},
		{ID: "workspace-picker-rename", ConfigKey: "workspace_picker.rename", Mode: ModeWorkspacePicker, Group: "Workspace", Binding: Binding{Type: tea.KeyCtrlR, Action: ActionRenameWorkspace}, KeyLabel: "Ctrl-R", HelpText: "rename selected item", StatusText: "Ctrl-R RENAME", FooterText: "rename"},
		{ID: "workspace-picker-remove", ConfigKey: "workspace_picker.remove", Mode: ModeWorkspacePicker, Group: "Workspace", Binding: Binding{Type: tea.KeyCtrlX, Action: ActionDeleteWorkspace}, KeyLabel: "Ctrl-X", HelpText: "remove selected item", StatusText: "Ctrl-X REMOVE", FooterText: "remove"},
		{ID: "workspace-picker-detach", ConfigKey: "workspace_picker.detach", Mode: ModeWorkspacePicker, Group: "Workspace", Binding: Binding{Type: tea.KeyCtrlD, Action: ActionDetachPane}, KeyLabel: "Ctrl-D", HelpText: "detach selected pane", StatusText: "Ctrl-D DETACH", FooterText: "detach"},
		{ID: "workspace-picker-zoom", ConfigKey: "workspace_picker.zoom", Mode: ModeWorkspacePicker, Group: "Workspace", Binding: Binding{Type: tea.KeyCtrlZ, Action: ActionZoomPane}, KeyLabel: "Ctrl-Z", HelpText: "zoom selected pane", StatusText: "Ctrl-Z ZOOM", FooterText: "zoom"},
		{ID: "workspace-picker-exit", ConfigKey: "workspace_picker.exit", Mode: ModeWorkspacePicker, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", StatusText: "Esc BACK", FooterText: "close"},

		{ID: "prompt-type", ConfigKey: "prompt.type", Mode: ModePrompt, Group: "Most Used", StatusText: "TYPE INPUT"},
		{ID: "prompt-backspace", ConfigKey: "prompt.backspace", Mode: ModePrompt, Group: "Most Used", StatusText: "Backspace DELETE"},
		{ID: "prompt-enter", ConfigKey: "prompt.enter", Mode: ModePrompt, Group: "Most Used", StatusText: "Enter CONTINUE"},
		{ID: "prompt-exit", ConfigKey: "prompt.exit", Mode: ModePrompt, Group: "Exit / Close", StatusText: "Esc BACK"},

		{ID: "help-exit", ConfigKey: "help.exit", Mode: ModeHelp, Group: "Exit / Close", Binding: Binding{Type: tea.KeyEsc, Action: ActionCancelMode}, KeyLabel: "Esc", HelpText: "exit current mode or close modal", StatusText: "Esc BACK", FooterText: "close"},
	}
}

func HelpSections() []HelpSectionDoc {
	sections := make([]HelpSectionDoc, 0, len(helpSectionOrder)+1)
	for _, title := range helpSectionOrder {
		bindings := helpBindingsForGroup(title)
		if len(bindings) == 0 {
			continue
		}
		sections = append(sections, HelpSectionDoc{Title: title, Bindings: bindings})
	}
	sections = append(sections, HelpSectionDoc{
		Title: "Concepts",
		Bindings: []HelpBindingDoc{
			{Key: "pane", Action: "visible area; it shows a terminal"},
			{Key: "terminal", Action: "runtime object managed by the server"},
			{Key: "manager", Action: "terminal pool page for attach/edit/stop"},
		},
	})
	return sections
}

func StatusTextsForMode(mode ModeKind) []string {
	out := make([]string, 0, 8)
	seen := make(map[string]struct{})
	for _, doc := range DefaultBindingCatalog() {
		if doc.Mode != mode || strings.TrimSpace(doc.StatusText) == "" {
			continue
		}
		if _, ok := seen[doc.StatusText]; ok {
			continue
		}
		seen[doc.StatusText] = struct{}{}
		out = append(out, doc.StatusText)
	}
	return out
}

func FooterForMode(mode ModeKind, includeFilter bool) string {
	parts := make([]string, 0, 8)
	if includeFilter {
		parts = append(parts, "[type] filter")
	}
	for _, doc := range DefaultBindingCatalog() {
		if doc.Mode != mode || strings.TrimSpace(doc.FooterText) == "" || strings.TrimSpace(doc.KeyLabel) == "" {
			continue
		}
		parts = append(parts, "["+doc.KeyLabel+"] "+doc.FooterText)
	}
	return strings.Join(parts, "  ")
}

func helpBindingsForGroup(group string) []HelpBindingDoc {
	out := make([]HelpBindingDoc, 0, 8)
	seen := make(map[string]struct{})
	for _, doc := range DefaultBindingCatalog() {
		if doc.Group != group || strings.TrimSpace(doc.KeyLabel) == "" || strings.TrimSpace(doc.HelpText) == "" {
			continue
		}
		key := doc.KeyLabel + "\x00" + doc.HelpText
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, HelpBindingDoc{Key: doc.KeyLabel, Action: doc.HelpText})
	}
	return out
}
