package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lozzow/termx/protocol"
)

func (m *Model) openWorkspacePickerCmd() tea.Cmd {
	return func() tea.Msg {
		m.snapshotCurrentWorkspace()
		picker := &workspacePicker{
			Title:  "Choose Workspace",
			Footer: "[Enter] switch or create  [Esc] close",
			Items:  m.buildWorkspacePickerItems(),
		}
		picker.applyFilter()
		return workspacePickerLoadedMsg{picker: picker}
	}
}

func (m *Model) buildWorkspacePickerItems() []workspacePickerItem {
	m.snapshotCurrentWorkspace()
	items := make([]workspacePickerItem, 0, len(m.workspaceOrder)+1)
	create := workspacePickerItem{
		CreateNew:   true,
		Name:        nextWorkspaceName(m.workspaceOrder),
		Description: "Create a new workspace",
	}
	create.primeCaches()
	items = append(items, create)
	for idx, name := range m.workspaceOrder {
		workspace, ok := m.workspaceStore[name]
		if !ok {
			continue
		}
		paneCount := 0
		for _, tab := range workspace.Tabs {
			if tab != nil {
				paneCount += len(tab.Panes)
			}
		}
		item := workspacePickerItem{
			Name:        name,
			Description: fmt.Sprintf("%d tab(s), %d pane(s)", len(workspace.Tabs), paneCount),
		}
		if idx == m.activeWorkspace {
			item.Description += " [active]"
		}
		item.primeCaches()
		items = append(items, item)
	}
	return items
}

func (p *workspacePicker) applyFilter() {
	query := strings.TrimSpace(strings.ToLower(p.Query))
	p.Filtered = p.Filtered[:0]
	for i := range p.Items {
		item := p.Items[i]
		if query == "" || strings.Contains(item.searchTextLower, query) {
			p.Filtered = append(p.Filtered, item)
		}
	}
	if len(p.Filtered) == 0 {
		p.Selected = 0
		return
	}
	if p.Selected < 0 {
		p.Selected = 0
	}
	if p.Selected >= len(p.Filtered) {
		p.Selected = len(p.Filtered) - 1
	}
}

func (i *workspacePickerItem) primeCaches() {
	if i == nil {
		return
	}
	i.searchTextLower = strings.ToLower(strings.TrimSpace(i.Name + " " + i.Description))
	i.lineBody = strings.TrimSpace(i.Name + "  " + i.Description)
	i.lineWidth = 0
	i.lineNormal = ""
	i.lineActive = ""
}

func (i *workspacePickerItem) line(width int, selected bool) string {
	if i == nil {
		return ""
	}
	if i.lineWidth != width {
		i.lineWidth = width
		plain := forceWidthANSI(" "+i.lineBody+" ", width)
		if i.CreateNew {
			i.lineNormal = pickerCreateRowStyle.Render(plain)
		} else {
			i.lineNormal = pickerLineStyle.Render(plain)
		}
		i.lineActive = pickerSelectedWorkspaceStyle.Render(plain)
	}
	if selected {
		return i.lineActive
	}
	return i.lineNormal
}

func (m *Model) handleWorkspacePickerKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.workspacePicker = nil
		m.invalidateRender()
	case tea.KeyEnter:
		return m.activateWorkspacePickerSelection()
	case tea.KeyUp:
		m.moveWorkspacePickerSelection(-1)
	case tea.KeyDown:
		m.moveWorkspacePickerSelection(1)
	case tea.KeyBackspace:
		m.deleteWorkspacePickerRune()
	case tea.KeyRunes:
		if len(msg.Runes) > 0 {
			m.appendWorkspacePickerQuery(string(msg.Runes))
		}
	}
	return nil
}

func (m *Model) consumeWorkspacePickerInput() (int, tea.Cmd, bool) {
	if len(m.rawPending) == 0 {
		return 0, nil, false
	}
	if n, dir, ok, incomplete := parseArrowPrefix(m.rawPending); incomplete {
		return 0, nil, false
	} else if ok {
		switch dir {
		case DirectionUp:
			m.moveWorkspacePickerSelection(-1)
		case DirectionDown:
			m.moveWorkspacePickerSelection(1)
		}
		return n, nil, true
	}
	switch m.rawPending[0] {
	case '\r', '\n':
		return 1, m.activateWorkspacePickerSelection(), true
	case 0x7f, 0x08:
		m.deleteWorkspacePickerRune()
		return 1, nil, true
	case 0x1b:
		if len(m.rawPending) == 1 {
			m.workspacePicker = nil
			m.invalidateRender()
			return 1, nil, true
		}
		return 0, nil, false
	}
	r, size := decodeInputRune(m.rawPending)
	if size == 0 {
		return 0, nil, false
	}
	if r >= 0x20 {
		m.appendWorkspacePickerQuery(string(r))
	}
	return size, nil, true
}

func (m *Model) handleWorkspacePickerEvent(event uv.Event) tea.Cmd {
	switch event := event.(type) {
	case uv.KeyPressEvent:
		switch {
		case event.MatchString("esc"):
			m.workspacePicker = nil
			m.invalidateRender()
		case event.MatchString("enter"):
			return m.activateWorkspacePickerSelection()
		case event.MatchString("up"):
			m.moveWorkspacePickerSelection(-1)
		case event.MatchString("down"):
			m.moveWorkspacePickerSelection(1)
		case event.MatchString("backspace"):
			m.deleteWorkspacePickerRune()
		case event.Text != "":
			m.appendWorkspacePickerQuery(event.Text)
		}
	case uv.PasteEvent:
		m.appendWorkspacePickerQuery(event.Content)
	}
	return nil
}

func (m *Model) moveWorkspacePickerSelection(delta int) {
	if m.workspacePicker == nil || len(m.workspacePicker.Filtered) == 0 {
		return
	}
	next := m.workspacePicker.Selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.workspacePicker.Filtered) {
		next = len(m.workspacePicker.Filtered) - 1
	}
	m.workspacePicker.Selected = next
	m.invalidateRender()
}

func (m *Model) appendWorkspacePickerQuery(value string) {
	if m.workspacePicker == nil || value == "" {
		return
	}
	m.workspacePicker.Query += value
	m.workspacePicker.applyFilter()
	m.invalidateRender()
}

func (m *Model) deleteWorkspacePickerRune() {
	if m.workspacePicker == nil || m.workspacePicker.Query == "" {
		return
	}
	_, size := utf8.DecodeLastRuneInString(m.workspacePicker.Query)
	if size <= 0 {
		m.workspacePicker.Query = ""
	} else {
		m.workspacePicker.Query = m.workspacePicker.Query[:len(m.workspacePicker.Query)-size]
	}
	m.workspacePicker.applyFilter()
	m.invalidateRender()
}

func (m *Model) activateWorkspacePickerSelection() tea.Cmd {
	if m.workspacePicker == nil || len(m.workspacePicker.Filtered) == 0 {
		return nil
	}
	item := m.workspacePicker.Filtered[m.workspacePicker.Selected]
	m.workspacePicker = nil
	m.invalidateRender()
	if item.CreateNew {
		return m.createWorkspaceCmd(item.Name)
	}
	return m.switchWorkspaceCmd(item.Name)
}

func (m *Model) renderWorkspacePicker() string {
	title := "Workspace Picker"
	if m.workspacePicker != nil && m.workspacePicker.Title != "" {
		title = m.workspacePicker.Title
	}
	query := ""
	if m.workspacePicker != nil {
		query = m.workspacePicker.Query
	}
	lines := make([]string, 0, 16)
	if m.workspacePicker != nil {
		contentWidth := m.centeredPickerInnerWidth()
		if m.workspacePicker.RenderWidth != contentWidth {
			m.workspacePicker.RenderWidth = contentWidth
			for i := range m.workspacePicker.Items {
				m.workspacePicker.Items[i].lineWidth = 0
			}
		}
		for i := range m.workspacePicker.Filtered {
			item := &m.workspacePicker.Filtered[i]
			lines = append(lines, item.line(contentWidth, i == m.workspacePicker.Selected))
		}
	}
	if len(lines) == 0 {
		lines = []string{"  no workspaces match"}
	}
	footer := "[Enter] switch or create  [Esc] close"
	if m.workspacePicker != nil && m.workspacePicker.Footer != "" {
		footer = m.workspacePicker.Footer
	}
	return m.renderCenteredPickerModal(title, query, lines, footer)
}

func (m *Model) snapshotCurrentWorkspace() {
	m.ensureWorkspaceStore()
	name := strings.TrimSpace(m.workspace.Name)
	if name == "" {
		name = nextWorkspaceName(m.workspaceOrder)
		m.workspace.Name = name
	}
	if m.activeWorkspace < 0 || m.activeWorkspace >= len(m.workspaceOrder) {
		m.activeWorkspace = 0
	}
	if len(m.workspaceOrder) == 0 {
		m.workspaceOrder = []string{name}
		m.activeWorkspace = 0
	}
	if m.activeWorkspace >= len(m.workspaceOrder) {
		m.workspaceOrder = append(m.workspaceOrder, name)
		m.activeWorkspace = len(m.workspaceOrder) - 1
	}
	oldName := m.workspaceOrder[m.activeWorkspace]
	if oldName != name {
		delete(m.workspaceStore, oldName)
		m.workspaceOrder[m.activeWorkspace] = name
	}
	m.workspaceStore[name] = m.workspace
}

func (m *Model) ensureWorkspaceStore() {
	if m.workspaceStore == nil {
		m.workspaceStore = make(map[string]Workspace)
	}
	if len(m.workspaceOrder) == 0 {
		name := m.workspace.Name
		if strings.TrimSpace(name) == "" {
			name = "main"
			m.workspace.Name = name
		}
		m.workspaceOrder = []string{name}
		m.activeWorkspace = 0
		m.workspaceStore[name] = m.workspace
	}
}

func (m *Model) createWorkspaceCmd(name string) tea.Cmd {
	m.snapshotCurrentWorkspace()
	name = strings.TrimSpace(name)
	if name == "" {
		name = nextWorkspaceName(m.workspaceOrder)
	}
	if _, exists := m.workspaceStore[name]; !exists {
		m.workspaceStore[name] = Workspace{Name: name, Tabs: []*Tab{newTab("1")}, ActiveTab: 0}
		m.workspaceOrder = append(m.workspaceOrder, name)
	}
	index := slices.Index(m.workspaceOrder, name)
	if index < 0 {
		index = len(m.workspaceOrder) - 1
	}
	return func() tea.Msg {
		workspace := m.workspaceStore[name]
		return workspaceActivatedMsg{
			workspace: workspace,
			index:     index,
			notice:    "workspace: " + name,
			bootstrap: workspaceNeedsBootstrap(workspace),
		}
	}
}

func (m *Model) switchWorkspaceCmd(name string) tea.Cmd {
	m.snapshotCurrentWorkspace()
	index := slices.Index(m.workspaceOrder, name)
	if index < 0 {
		return nil
	}
	workspace := m.workspaceStore[name]
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		result, err := m.client.List(ctx)
		if err != nil {
			return errMsg{m.wrapClientError("list terminals", err)}
		}
		if err := m.hydrateWorkspaceRuntime(&workspace, result.Terminals); err != nil {
			return errMsg{err}
		}
		return workspaceActivatedMsg{
			workspace: workspace,
			index:     index,
			notice:    "workspace: " + name,
			bootstrap: workspaceNeedsBootstrap(workspace),
		}
	}
}

func (m *Model) activateWorkspaceByOffset(delta int) tea.Cmd {
	m.ensureWorkspaceStore()
	if len(m.workspaceOrder) == 0 || delta == 0 {
		return nil
	}
	next := (m.activeWorkspace + delta) % len(m.workspaceOrder)
	if next < 0 {
		next += len(m.workspaceOrder)
	}
	return m.switchWorkspaceCmd(m.workspaceOrder[next])
}

func (m *Model) renameCurrentWorkspace(name string) {
	m.ensureWorkspaceStore()
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	oldName := strings.TrimSpace(m.workspace.Name)
	if oldName == "" {
		oldName = nextWorkspaceName(m.workspaceOrder)
	}
	if name == oldName {
		return
	}
	if _, exists := m.workspaceStore[name]; exists {
		return
	}
	delete(m.workspaceStore, oldName)
	m.workspace.Name = name
	if m.activeWorkspace >= 0 && m.activeWorkspace < len(m.workspaceOrder) {
		m.workspaceOrder[m.activeWorkspace] = name
	}
	m.workspaceStore[name] = m.workspace
}

func (m *Model) deleteCurrentWorkspaceCmd() tea.Cmd {
	m.ensureWorkspaceStore()
	if len(m.workspaceOrder) <= 1 || m.activeWorkspace < 0 || m.activeWorkspace >= len(m.workspaceOrder) {
		return nil
	}
	index := m.activeWorkspace
	name := m.workspaceOrder[index]
	delete(m.workspaceStore, name)
	m.workspaceOrder = append(m.workspaceOrder[:index], m.workspaceOrder[index+1:]...)
	if index >= len(m.workspaceOrder) {
		index = len(m.workspaceOrder) - 1
	}
	nextName := m.workspaceOrder[index]
	workspace := m.workspaceStore[nextName]
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		result, err := m.client.List(ctx)
		if err != nil {
			return errMsg{m.wrapClientError("list terminals", err)}
		}
		if err := m.hydrateWorkspaceRuntime(&workspace, result.Terminals); err != nil {
			return errMsg{err}
		}
		return workspaceActivatedMsg{
			workspace: workspace,
			index:     index,
			notice:    "workspace: " + nextName,
			bootstrap: workspaceNeedsBootstrap(workspace),
		}
	}
}

func workspaceNeedsBootstrap(workspace Workspace) bool {
	if len(workspace.Tabs) == 0 {
		return true
	}
	if workspace.ActiveTab < 0 || workspace.ActiveTab >= len(workspace.Tabs) {
		return true
	}
	tab := workspace.Tabs[workspace.ActiveTab]
	if tab == nil {
		return true
	}
	return len(tab.Panes) == 0
}

func (m *Model) hydrateWorkspaceRuntime(workspace *Workspace, terminals []protocol.TerminalInfo) error {
	if workspace == nil {
		return nil
	}
	for _, tab := range workspace.Tabs {
		if tab == nil {
			continue
		}
		for paneID, pane := range tab.Panes {
			if pane == nil || pane.TerminalID == "" || paneTerminalState(pane) != "running" {
				continue
			}
			info := findTerminalInfo(terminals, pane.TerminalID)
			if info == nil || defaultTerminalState(info.State) != "running" {
				pane.TerminalState = "exited"
				pane.live = false
				pane.stopStream = nil
				tab.Panes[paneID] = pane
				continue
			}
			ctx, cancel := m.requestContext()
			attached, err := m.client.Attach(ctx, pane.TerminalID, "collaborator")
			if err != nil {
				cancel()
				return m.wrapClientError("attach terminal", err, "terminal_id", pane.TerminalID)
			}
			snap, err := m.client.Snapshot(ctx, pane.TerminalID, 0, 200)
			cancel()
			if err != nil {
				return m.wrapClientError("snapshot terminal", err, "terminal_id", pane.TerminalID)
			}
			view := m.newViewport(pane.TerminalID, attached.Channel, snap)
			view.AttachMode = attached.Mode
			view = viewportWithTerminalInfo(view, *info)
			view.Mode = pane.Mode
			view.Offset = pane.Offset
			view.Pin = pane.Pin
			view.Readonly = pane.Readonly
			pane.Viewport = view
			m.startPaneStream(pane)
			tab.Panes[paneID] = pane
		}
	}
	return nil
}

func nextWorkspaceName(existing []string) string {
	used := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		used[name] = struct{}{}
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("workspace-%d", i)
		if _, ok := used[name]; !ok {
			return name
		}
	}
}
