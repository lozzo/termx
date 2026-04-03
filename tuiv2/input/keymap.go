package input

import tea "github.com/charmbracelet/bubbletea"

// Binding maps a key to an action in a specific mode.
type Binding struct {
	// Type matches tea.KeyMsg.Type for non-rune keys (e.g. KeyCtrlF, KeyEsc).
	// When Type == tea.KeyRunes, Rune must also match.
	Type tea.KeyType
	// Rune is only examined when Type == tea.KeyRunes.
	Rune rune
	// RuneMin/RuneMax allow a single binding to match a rune range.
	RuneMin rune
	RuneMax rune
	// Action produced when this binding fires.
	Action ActionKind
	// TextFromRune copies the matching rune into SemanticAction.Text.
	TextFromRune bool
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
	km := Keymap{}
	for _, doc := range DefaultBindingCatalog() {
		if doc.Binding.Action == "" {
			continue
		}
		switch doc.Mode {
		case ModeNormal:
			km.Normal = append(km.Normal, doc.Binding)
		case ModePane:
			km.Pane = append(km.Pane, doc.Binding)
		case ModeResize:
			km.Resize = append(km.Resize, doc.Binding)
		case ModeTab:
			km.Tab = append(km.Tab, doc.Binding)
		case ModeWorkspace:
			km.Workspace = append(km.Workspace, doc.Binding)
		case ModeFloating:
			km.Floating = append(km.Floating, doc.Binding)
		case ModeDisplay:
			km.Display = append(km.Display, doc.Binding)
		case ModeGlobal:
			km.Global = append(km.Global, doc.Binding)
		case ModeTerminalManager:
			km.TerminalManager = append(km.TerminalManager, doc.Binding)
		case ModePicker:
			km.Picker = append(km.Picker, doc.Binding)
		case ModeWorkspacePicker:
			km.WorkspacePicker = append(km.WorkspacePicker, doc.Binding)
		case ModeHelp:
			km.Help = append(km.Help, doc.Binding)
		}
	}
	return km
}

// LookupNormal returns the SemanticAction bound to msg in ModeNormal, or nil.
func (km *Keymap) LookupNormal(msg tea.KeyMsg) *SemanticAction {
	return lookupBindings(km.Normal, msg)
}

func (km *Keymap) LookupPane(msg tea.KeyMsg) *SemanticAction   { return lookupBindings(km.Pane, msg) }
func (km *Keymap) LookupResize(msg tea.KeyMsg) *SemanticAction { return lookupBindings(km.Resize, msg) }
func (km *Keymap) LookupTab(msg tea.KeyMsg) *SemanticAction    { return lookupBindings(km.Tab, msg) }
func (km *Keymap) LookupWorkspace(msg tea.KeyMsg) *SemanticAction {
	return lookupBindings(km.Workspace, msg)
}
func (km *Keymap) LookupFloating(msg tea.KeyMsg) *SemanticAction {
	return lookupBindings(km.Floating, msg)
}
func (km *Keymap) LookupDisplay(msg tea.KeyMsg) *SemanticAction {
	return lookupBindings(km.Display, msg)
}
func (km *Keymap) LookupGlobal(msg tea.KeyMsg) *SemanticAction { return lookupBindings(km.Global, msg) }
func (km *Keymap) LookupTerminalManager(msg tea.KeyMsg) *SemanticAction {
	return lookupBindings(km.TerminalManager, msg)
}

// LookupPicker returns the SemanticAction bound to msg in ModePicker, or nil.
func (km *Keymap) LookupPicker(msg tea.KeyMsg) *SemanticAction {
	return lookupBindings(km.Picker, msg)
}

// LookupWorkspacePicker returns the SemanticAction bound to msg in ModeWorkspacePicker, or nil.
func (km *Keymap) LookupWorkspacePicker(msg tea.KeyMsg) *SemanticAction {
	return lookupBindings(km.WorkspacePicker, msg)
}

// LookupHelp returns the SemanticAction bound to msg in ModeHelp, or nil.
func (km *Keymap) LookupHelp(msg tea.KeyMsg) *SemanticAction {
	return lookupBindings(km.Help, msg)
}

func lookupBindings(bindings []Binding, msg tea.KeyMsg) *SemanticAction {
	for _, b := range bindings {
		if b.Type != tea.KeyRunes {
			if msg.Type == b.Type {
				return &SemanticAction{Kind: b.Action}
			}
			continue
		}
		if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
			continue
		}
		r := msg.Runes[0]
		if b.Rune != 0 && r != b.Rune {
			continue
		}
		if b.RuneMin != 0 && r < b.RuneMin {
			continue
		}
		if b.RuneMax != 0 && r > b.RuneMax {
			continue
		}
		action := &SemanticAction{Kind: b.Action}
		if b.TextFromRune {
			action.Text = string(r)
		}
		return action
	}
	return nil
}
