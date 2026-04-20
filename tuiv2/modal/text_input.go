package modal

import "github.com/lozzow/termx/tuiv2/uiinput"

func (p *PickerState) QueryEditor() *uiinput.State {
	if p == nil {
		return nil
	}
	if !p.QueryInput.Initialized() || p.QueryInput.Value() != p.Query || p.QueryInput.Position() != uiinput.LegacyCursor(p.Query, p.Cursor, p.CursorSet) {
		p.QueryInput.ResetFromLegacy(p.Query, p.Cursor, p.CursorSet, "")
	}
	return &p.QueryInput
}

func (p *PickerState) SyncQueryLegacy() {
	if p == nil || !p.QueryInput.Initialized() {
		return
	}
	p.Query = p.QueryInput.Value()
	p.Cursor = p.QueryInput.Position()
	p.CursorSet = true
}

func (p PickerState) QueryState() uiinput.State {
	if p.QueryInput.Initialized() && p.QueryInput.Value() == p.Query && p.QueryInput.Position() == uiinput.LegacyCursor(p.Query, p.Cursor, p.CursorSet) {
		return p.QueryInput
	}
	return uiinput.FromLegacy(p.Query, p.Cursor, p.CursorSet, "")
}

func (p PickerState) QueryValue() string {
	if p.QueryInput.Initialized() {
		return p.QueryInput.Value()
	}
	return p.Query
}

func (p PickerState) QueryCursorIndex() int {
	if p.QueryInput.Initialized() {
		return p.QueryInput.Position()
	}
	return uiinput.LegacyCursor(p.Query, p.Cursor, p.CursorSet)
}

func (p *WorkspacePickerState) QueryEditor() *uiinput.State {
	if p == nil {
		return nil
	}
	if !p.QueryInput.Initialized() || p.QueryInput.Value() != p.Query || p.QueryInput.Position() != uiinput.LegacyCursor(p.Query, p.Cursor, p.CursorSet) {
		p.QueryInput.ResetFromLegacy(p.Query, p.Cursor, p.CursorSet, "")
	}
	return &p.QueryInput
}

func (p *WorkspacePickerState) SyncQueryLegacy() {
	if p == nil || !p.QueryInput.Initialized() {
		return
	}
	p.Query = p.QueryInput.Value()
	p.Cursor = p.QueryInput.Position()
	p.CursorSet = true
}

func (p WorkspacePickerState) QueryState() uiinput.State {
	if p.QueryInput.Initialized() && p.QueryInput.Value() == p.Query && p.QueryInput.Position() == uiinput.LegacyCursor(p.Query, p.Cursor, p.CursorSet) {
		return p.QueryInput
	}
	return uiinput.FromLegacy(p.Query, p.Cursor, p.CursorSet, "")
}

func (p WorkspacePickerState) QueryValue() string {
	if p.QueryInput.Initialized() {
		return p.QueryInput.Value()
	}
	return p.Query
}

func (p WorkspacePickerState) QueryCursorIndex() int {
	if p.QueryInput.Initialized() {
		return p.QueryInput.Position()
	}
	return uiinput.LegacyCursor(p.Query, p.Cursor, p.CursorSet)
}

func (m *TerminalManagerState) QueryEditor() *uiinput.State {
	if m == nil {
		return nil
	}
	if !m.QueryInput.Initialized() || m.QueryInput.Value() != m.Query || m.QueryInput.Position() != uiinput.LegacyCursor(m.Query, m.Cursor, m.CursorSet) {
		m.QueryInput.ResetFromLegacy(m.Query, m.Cursor, m.CursorSet, "")
	}
	return &m.QueryInput
}

func (m *TerminalManagerState) SyncQueryLegacy() {
	if m == nil || !m.QueryInput.Initialized() {
		return
	}
	m.Query = m.QueryInput.Value()
	m.Cursor = m.QueryInput.Position()
	m.CursorSet = true
}

func (m TerminalManagerState) QueryState() uiinput.State {
	if m.QueryInput.Initialized() && m.QueryInput.Value() == m.Query && m.QueryInput.Position() == uiinput.LegacyCursor(m.Query, m.Cursor, m.CursorSet) {
		return m.QueryInput
	}
	return uiinput.FromLegacy(m.Query, m.Cursor, m.CursorSet, "")
}

func (m TerminalManagerState) QueryValue() string {
	if m.QueryInput.Initialized() {
		return m.QueryInput.Value()
	}
	return m.Query
}

func (m TerminalManagerState) QueryCursorIndex() int {
	if m.QueryInput.Initialized() {
		return m.QueryInput.Position()
	}
	return uiinput.LegacyCursor(m.Query, m.Cursor, m.CursorSet)
}

func (p *PromptState) ValueEditor() *uiinput.State {
	if p == nil {
		return nil
	}
	if !p.Input.Initialized() || p.Input.Value() != p.Value || p.Input.Position() != p.Cursor {
		p.Input.ResetFromLegacy(p.Value, p.Cursor, true, "")
	}
	return &p.Input
}

func (p *PromptState) SyncValueLegacy() {
	if p == nil || !p.Input.Initialized() {
		return
	}
	p.Value = p.Input.Value()
	p.Cursor = p.Input.Position()
}

func (p PromptState) ValueState() uiinput.State {
	if p.Input.Initialized() && p.Input.Value() == p.Value && p.Input.Position() == p.Cursor {
		return p.Input
	}
	return uiinput.FromLegacy(p.Value, p.Cursor, true, "")
}

func (f *PromptField) ValueEditor() *uiinput.State {
	if f == nil {
		return nil
	}
	if !f.Input.Initialized() || f.Input.Value() != f.Value || f.Input.Position() != f.Cursor || f.Input.Placeholder() != f.Placeholder {
		f.Input.ResetFromLegacy(f.Value, f.Cursor, true, f.Placeholder)
	}
	return &f.Input
}

func (f *PromptField) SyncValueLegacy() {
	if f == nil || !f.Input.Initialized() {
		return
	}
	f.Value = f.Input.Value()
	f.Cursor = f.Input.Position()
}

func (f PromptField) ValueState() uiinput.State {
	if f.Input.Initialized() && f.Input.Value() == f.Value && f.Input.Position() == f.Cursor && f.Input.Placeholder() == f.Placeholder {
		return f.Input
	}
	return uiinput.FromLegacy(f.Value, f.Cursor, true, f.Placeholder)
}
