package input

import tea "github.com/charmbracelet/bubbletea"

// Binding maps a key to an action in a specific mode.
type Binding struct {
	// Type matches tea.KeyMsg.Type for non-rune keys (e.g. KeyCtrlF, KeyEsc).
	// When Type == tea.KeyRunes, Rune must also match.
	Type tea.KeyType
	// Rune is only examined when Type == tea.KeyRunes.
	Rune rune
	// Action produced when this binding fires.
	Action ActionKind
}

// Keymap holds per-mode key bindings.
type Keymap struct {
	// Normal holds bindings active in ModeNormal (passthrough; Ctrl shortcuts intercepted).
	Normal []Binding
	// Pane holds bindings active in ModePane.
	Pane []Binding
	// Resize holds bindings active in ModeResize.
	Resize []Binding
	// Tab holds bindings active in ModeTab.
	Tab []Binding
	// Workspace holds bindings active in ModeWorkspace.
	Workspace []Binding
	// Floating holds bindings active in ModeFloating.
	Floating []Binding
	// Display holds bindings active in ModeDisplay.
	Display []Binding
	// Global holds bindings active in ModeGlobal.
	Global []Binding
	// TerminalManager holds bindings active in ModeTerminalManager.
	TerminalManager []Binding
	// Picker holds bindings active in ModePicker.
	Picker []Binding
	// WorkspacePicker holds bindings active in ModeWorkspacePicker.
	WorkspacePicker []Binding
	// Help holds bindings active in ModeHelp.
	Help []Binding
}

// DefaultKeymap returns the canonical key bindings for tuiv2.
func DefaultKeymap() Keymap {
	return Keymap{
		// Normal mode: Ctrl shortcuts enter sub-modes; everything else passes through.
		Normal: []Binding{
			{Type: tea.KeyCtrlP, Action: ActionEnterPaneMode},
			{Type: tea.KeyCtrlR, Action: ActionEnterResizeMode},
			{Type: tea.KeyCtrlT, Action: ActionEnterTabMode},
			{Type: tea.KeyCtrlW, Action: ActionEnterWorkspaceMode},
			{Type: tea.KeyCtrlO, Action: ActionEnterFloatingMode},
			{Type: tea.KeyCtrlV, Action: ActionEnterDisplayMode},
			{Type: tea.KeyCtrlF, Action: ActionOpenPicker},
			{Type: tea.KeyCtrlG, Action: ActionEnterGlobalMode},
			{Type: tea.KeyRunes, Rune: '?', Action: ActionOpenHelp},
		},
		Pane: []Binding{
			// Focus via plain rune keys
			{Type: tea.KeyRunes, Rune: 'h', Action: ActionFocusPaneLeft},
			{Type: tea.KeyRunes, Rune: 'j', Action: ActionFocusPaneDown},
			{Type: tea.KeyRunes, Rune: 'k', Action: ActionFocusPaneUp},
			{Type: tea.KeyRunes, Rune: 'l', Action: ActionFocusPaneRight},
			// Focus via arrow keys
			{Type: tea.KeyLeft, Action: ActionFocusPaneLeft},
			{Type: tea.KeyDown, Action: ActionFocusPaneDown},
			{Type: tea.KeyUp, Action: ActionFocusPaneUp},
			{Type: tea.KeyRight, Action: ActionFocusPaneRight},
			// Split
			{Type: tea.KeyRunes, Rune: '%', Action: ActionSplitPane},
			{Type: tea.KeyRunes, Rune: '"', Action: ActionSplitPaneHorizontal},
			{Type: tea.KeyCtrlD, Action: ActionSplitPane},
			{Type: tea.KeyCtrlE, Action: ActionSplitPaneHorizontal},
			// Other
			{Type: tea.KeyRunes, Rune: 'z', Action: ActionZoomPane},
			{Type: tea.KeyRunes, Rune: 'w', Action: ActionClosePane},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Resize: []Binding{
			// Small step
			{Type: tea.KeyRunes, Rune: 'h', Action: ActionResizePaneLeft},
			{Type: tea.KeyRunes, Rune: 'l', Action: ActionResizePaneRight},
			{Type: tea.KeyRunes, Rune: 'k', Action: ActionResizePaneUp},
			{Type: tea.KeyRunes, Rune: 'j', Action: ActionResizePaneDown},
			{Type: tea.KeyLeft, Action: ActionResizePaneLeft},
			{Type: tea.KeyRight, Action: ActionResizePaneRight},
			{Type: tea.KeyUp, Action: ActionResizePaneUp},
			{Type: tea.KeyDown, Action: ActionResizePaneDown},
			// Large step
			{Type: tea.KeyRunes, Rune: 'H', Action: ActionResizePaneLargeLeft},
			{Type: tea.KeyRunes, Rune: 'L', Action: ActionResizePaneLargeRight},
			{Type: tea.KeyRunes, Rune: 'K', Action: ActionResizePaneLargeUp},
			{Type: tea.KeyRunes, Rune: 'J', Action: ActionResizePaneLargeDown},
			// Balance / cycle
			{Type: tea.KeyRunes, Rune: '=', Action: ActionBalancePanes},
			{Type: tea.KeySpace, Action: ActionCycleLayout},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Tab: []Binding{
			{Type: tea.KeyRunes, Rune: 'c', Action: ActionCreateTab},
			{Type: tea.KeyRunes, Rune: 'n', Action: ActionNextTab},
			{Type: tea.KeyRunes, Rune: 'p', Action: ActionPrevTab},
			{Type: tea.KeyRunes, Rune: 'w', Action: ActionCloseTab},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Workspace: []Binding{
			{Type: tea.KeyRunes, Rune: 'n', Action: ActionCreateWorkspace},
			{Type: tea.KeyRunes, Rune: 'd', Action: ActionDeleteWorkspace},
			{Type: tea.KeyCtrlF, Action: ActionOpenWorkspacePicker},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Floating: []Binding{
			{Type: tea.KeyRunes, Rune: 'n', Action: ActionCreateFloatingPane},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Display: []Binding{
			{Type: tea.KeyRunes, Rune: 'u', Action: ActionScrollUp},
			{Type: tea.KeyRunes, Rune: 'd', Action: ActionScrollDown},
			{Type: tea.KeyRunes, Rune: 'z', Action: ActionZoomPane},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Global: []Binding{
			{Type: tea.KeyCtrlQ, Action: ActionQuit},
			{Type: tea.KeyCtrlT, Action: ActionOpenTerminalManager},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		TerminalManager: []Binding{
			{Type: tea.KeyUp, Action: ActionPickerUp},
			{Type: tea.KeyDown, Action: ActionPickerDown},
			{Type: tea.KeyEnter, Action: ActionSubmitPrompt},
			{Type: tea.KeyCtrlT, Action: ActionAttachTab},
			{Type: tea.KeyCtrlO, Action: ActionAttachFloating},
			{Type: tea.KeyCtrlE, Action: ActionEditTerminal},
			{Type: tea.KeyCtrlK, Action: ActionKillTerminal},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Picker: []Binding{
			{Type: tea.KeyUp, Action: ActionPickerUp},
			{Type: tea.KeyDown, Action: ActionPickerDown},
			{Type: tea.KeyEnter, Action: ActionSubmitPrompt},
			{Type: tea.KeyTab, Action: ActionPickerAttachSplit},
			{Type: tea.KeyCtrlE, Action: ActionEditTerminal},
			{Type: tea.KeyCtrlK, Action: ActionKillTerminal},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		WorkspacePicker: []Binding{
			{Type: tea.KeyUp, Action: ActionPickerUp},
			{Type: tea.KeyDown, Action: ActionPickerDown},
			{Type: tea.KeyEnter, Action: ActionSubmitPrompt},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Help: []Binding{{Type: tea.KeyEsc, Action: ActionCancelMode}},
	}
}

// LookupNormal returns the ActionKind bound to msg in ModeNormal, or "".
func (km *Keymap) LookupNormal(msg tea.KeyMsg) ActionKind {
	return lookupBindings(km.Normal, msg)
}

func (km *Keymap) LookupPane(msg tea.KeyMsg) ActionKind      { return lookupBindings(km.Pane, msg) }
func (km *Keymap) LookupResize(msg tea.KeyMsg) ActionKind    { return lookupBindings(km.Resize, msg) }
func (km *Keymap) LookupTab(msg tea.KeyMsg) ActionKind       { return lookupBindings(km.Tab, msg) }
func (km *Keymap) LookupWorkspace(msg tea.KeyMsg) ActionKind { return lookupBindings(km.Workspace, msg) }
func (km *Keymap) LookupFloating(msg tea.KeyMsg) ActionKind  { return lookupBindings(km.Floating, msg) }
func (km *Keymap) LookupDisplay(msg tea.KeyMsg) ActionKind   { return lookupBindings(km.Display, msg) }
func (km *Keymap) LookupGlobal(msg tea.KeyMsg) ActionKind    { return lookupBindings(km.Global, msg) }
func (km *Keymap) LookupTerminalManager(msg tea.KeyMsg) ActionKind {
	return lookupBindings(km.TerminalManager, msg)
}

// LookupPicker returns the ActionKind bound to msg in ModePicker, or "".
func (km *Keymap) LookupPicker(msg tea.KeyMsg) ActionKind {
	return lookupBindings(km.Picker, msg)
}

// LookupWorkspacePicker returns the ActionKind bound to msg in ModeWorkspacePicker, or "".
func (km *Keymap) LookupWorkspacePicker(msg tea.KeyMsg) ActionKind {
	return lookupBindings(km.WorkspacePicker, msg)
}

// LookupHelp returns the ActionKind bound to msg in ModeHelp, or "".
func (km *Keymap) LookupHelp(msg tea.KeyMsg) ActionKind {
	return lookupBindings(km.Help, msg)
}

func lookupBindings(bindings []Binding, msg tea.KeyMsg) ActionKind {
	for _, b := range bindings {
		if b.Type != tea.KeyRunes {
			if msg.Type == b.Type {
				return b.Action
			}
		} else if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == b.Rune {
			return b.Action
		}
	}
	return ""
}
