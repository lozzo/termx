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
	// Normal holds bindings active in ModeNormal.
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
			{Type: tea.KeyCtrlD, Action: ActionSplitPane},
			{Type: tea.KeyCtrlE, Action: ActionSplitPaneHorizontal},
			{Type: tea.KeyCtrlH, Action: ActionFocusPaneLeft},
			{Type: tea.KeyCtrlL, Action: ActionFocusPaneRight},
			{Type: tea.KeyCtrlJ, Action: ActionFocusPaneDown},
			{Type: tea.KeyCtrlK, Action: ActionFocusPaneUp},
			{Type: tea.KeyCtrlW, Action: ActionClosePane},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Resize: []Binding{
			{Type: tea.KeyRunes, Rune: 'h', Action: ActionResizePaneLeft},
			{Type: tea.KeyRunes, Rune: 'l', Action: ActionResizePaneRight},
			{Type: tea.KeyRunes, Rune: 'k', Action: ActionResizePaneUp},
			{Type: tea.KeyRunes, Rune: 'j', Action: ActionResizePaneDown},
			{Type: tea.KeyLeft, Action: ActionResizePaneLeft},
			{Type: tea.KeyRight, Action: ActionResizePaneRight},
			{Type: tea.KeyUp, Action: ActionResizePaneUp},
			{Type: tea.KeyDown, Action: ActionResizePaneDown},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Tab: []Binding{
			{Type: tea.KeyCtrlT, Action: ActionCreateTab},
			{Type: tea.KeyCtrlN, Action: ActionNextTab},
			{Type: tea.KeyCtrlP, Action: ActionPrevTab},
			{Type: tea.KeyCtrlW, Action: ActionCloseTab},
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
			{Type: tea.KeyCtrlU, Action: ActionScrollUp},
			{Type: tea.KeyCtrlY, Action: ActionScrollDown},
			{Type: tea.KeyCtrlV, Action: ActionZoomPane},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Global: []Binding{
			{Type: tea.KeyCtrlQ, Action: ActionQuit},
			{Type: tea.KeyCtrlT, Action: ActionOpenTerminalManager},
			{Type: tea.KeyEsc, Action: ActionCancelMode},
		},
		Picker: []Binding{
			{Type: tea.KeyUp, Action: ActionPickerUp},
			{Type: tea.KeyDown, Action: ActionPickerDown},
			{Type: tea.KeyEnter, Action: ActionSubmitPrompt},
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
