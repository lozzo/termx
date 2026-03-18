package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
)

func (m *Model) newViewport(terminalID string, channel uint16, snap *protocol.Snapshot) *Viewport {
	cols := int(max(20, uint16(80)))
	rows := int(max(5, uint16(24)))
	if snap != nil {
		cols = int(max(20, snap.Size.Cols))
		rows = int(max(5, snap.Size.Rows))
	}
	return &Viewport{
		TerminalID: terminalID,
		Channel:    channel,
		VTerm: localvterm.New(cols, rows, 10000, func(data []byte) {
			_ = m.client.Input(context.Background(), channel, data)
		}),
		Snapshot:    snap,
		Mode:        ViewportModeFit,
		renderDirty: true,
	}
}

func (m *Model) openTerminalPickerCmd() tea.Cmd {
	return func() tea.Msg {
		result, err := m.client.List(context.Background())
		if err != nil {
			return terminalPickerLoadedMsg{err: err}
		}
		return terminalPickerLoadedMsg{items: m.buildTerminalPickerItems(result.Terminals)}
	}
}

func (m *Model) buildTerminalPickerItems(terminals []protocol.TerminalInfo) []terminalPickerItem {
	locations := m.terminalLocations()
	items := make([]terminalPickerItem, 0, len(terminals))
	for _, info := range terminals {
		locs := append([]string(nil), locations[info.ID]...)
		slices.Sort(locs)
		item := terminalPickerItem{
			Info:     info,
			Observed: len(locs) > 0,
			Orphan:   len(locs) == 0,
			Location: strings.Join(locs, ", "),
		}
		items = append(items, item)
	}
	slices.SortStableFunc(items, func(a, b terminalPickerItem) int {
		if a.Observed != b.Observed {
			if a.Observed {
				return -1
			}
			return 1
		}
		aRunning := a.Info.State == "running"
		bRunning := b.Info.State == "running"
		if aRunning != bRunning {
			if aRunning {
				return -1
			}
			return 1
		}
		if a.Info.CreatedAt.Before(b.Info.CreatedAt) {
			return -1
		}
		if b.Info.CreatedAt.Before(a.Info.CreatedAt) {
			return 1
		}
		return strings.Compare(a.Info.ID, b.Info.ID)
	})
	return items
}

func (m *Model) terminalLocations() map[string][]string {
	locations := make(map[string][]string)
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		seen := make(map[string]struct{})
		location := fmt.Sprintf("ws:%s / tab:%s", m.workspace.Name, tab.Name)
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID == "" {
				continue
			}
			key := pane.TerminalID + "\x00" + location
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			locations[pane.TerminalID] = append(locations[pane.TerminalID], location)
		}
	}
	return locations
}

func (p *terminalPicker) applyFilter() {
	query := strings.TrimSpace(strings.ToLower(p.Query))
	p.Filtered = p.Filtered[:0]
	for _, item := range p.Items {
		if query == "" || strings.Contains(strings.ToLower(item.searchText()), query) {
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

func (i terminalPickerItem) searchText() string {
	parts := []string{
		i.Info.ID,
		i.Info.Name,
		strings.Join(i.Info.Command, " "),
		i.Location,
		i.Info.State,
	}
	for key, value := range i.Info.Tags {
		parts = append(parts, key, value, key+"="+value)
	}
	return strings.Join(parts, " ")
}

func (m *Model) handleTerminalPickerKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.terminalPicker = nil
		m.invalidateRender()
	case tea.KeyEnter:
		return m.attachPickerSelection(false)
	case tea.KeyTab:
		return m.attachPickerSelection(true)
	case tea.KeyUp:
		m.movePickerSelection(-1)
	case tea.KeyDown:
		m.movePickerSelection(1)
	case tea.KeyBackspace:
		m.deletePickerRune()
	case tea.KeyCtrlK:
		return m.killPickerSelection()
	case tea.KeyRunes:
		if len(msg.Runes) > 0 {
			m.appendPickerQuery(string(msg.Runes))
		}
	}
	return nil
}

func (m *Model) consumeTerminalPickerInput() (int, tea.Cmd, bool) {
	if len(m.rawPending) == 0 {
		return 0, nil, false
	}
	if n, dir, ok, incomplete := parseArrowPrefix(m.rawPending); incomplete {
		return 0, nil, false
	} else if ok {
		switch dir {
		case DirectionUp:
			m.movePickerSelection(-1)
		case DirectionDown:
			m.movePickerSelection(1)
		}
		return n, nil, true
	}
	switch m.rawPending[0] {
	case '\r', '\n':
		return 1, m.attachPickerSelection(false), true
	case '\t':
		return 1, m.attachPickerSelection(true), true
	case 0x7f, 0x08:
		m.deletePickerRune()
		return 1, nil, true
	case 0x0b:
		return 1, m.killPickerSelection(), true
	case 0x1b:
		if len(m.rawPending) == 1 {
			m.terminalPicker = nil
			return 1, nil, true
		}
		return 0, nil, false
	}

	r, size := decodeInputRune(m.rawPending)
	if size == 0 {
		return 0, nil, false
	}
	if r >= 0x20 {
		m.appendPickerQuery(string(r))
	}
	return size, nil, true
}

func (m *Model) handleTerminalPickerEvent(event uv.Event) tea.Cmd {
	switch event := event.(type) {
	case uv.KeyPressEvent:
		switch {
		case event.MatchString("esc"):
			m.terminalPicker = nil
			m.invalidateRender()
		case event.MatchString("enter"):
			return m.attachPickerSelection(false)
		case event.MatchString("tab"):
			return m.attachPickerSelection(true)
		case event.MatchString("up"):
			m.movePickerSelection(-1)
		case event.MatchString("down"):
			m.movePickerSelection(1)
		case event.MatchString("ctrl+k"):
			return m.killPickerSelection()
		case event.MatchString("backspace"):
			m.deletePickerRune()
		case event.Text != "":
			m.appendPickerQuery(event.Text)
		}
	case uv.PasteEvent:
		m.appendPickerQuery(event.Content)
	}
	return nil
}

func (m *Model) movePickerSelection(delta int) {
	if m.terminalPicker == nil || len(m.terminalPicker.Filtered) == 0 {
		return
	}
	next := m.terminalPicker.Selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.terminalPicker.Filtered) {
		next = len(m.terminalPicker.Filtered) - 1
	}
	m.terminalPicker.Selected = next
	m.invalidateRender()
}

func (m *Model) appendPickerQuery(value string) {
	if m.terminalPicker == nil || value == "" {
		return
	}
	m.terminalPicker.Query += value
	m.terminalPicker.applyFilter()
	m.invalidateRender()
}

func (m *Model) deletePickerRune() {
	if m.terminalPicker == nil || m.terminalPicker.Query == "" {
		return
	}
	runes := []rune(m.terminalPicker.Query)
	m.terminalPicker.Query = string(runes[:len(runes)-1])
	m.terminalPicker.applyFilter()
	m.invalidateRender()
}

func (m *Model) attachPickerSelection(split bool) tea.Cmd {
	if m.terminalPicker == nil || len(m.terminalPicker.Filtered) == 0 {
		return nil
	}
	item := m.terminalPicker.Filtered[m.terminalPicker.Selected]
	m.terminalPicker = nil
	m.invalidateRender()
	if split {
		tab := m.currentTab()
		if tab == nil {
			return nil
		}
		return m.attachTerminalToNewPaneCmd(m.workspace.ActiveTab, tab.ActivePaneID, SplitVertical, item)
	}
	return m.attachTerminalToActivePaneCmd(item)
}

func (m *Model) killPickerSelection() tea.Cmd {
	if m.terminalPicker == nil || len(m.terminalPicker.Filtered) == 0 {
		return nil
	}
	item := m.terminalPicker.Filtered[m.terminalPicker.Selected]
	m.terminalPicker = nil
	m.invalidateRender()
	return func() tea.Msg {
		if err := m.client.Kill(context.Background(), item.Info.ID); err != nil {
			return errMsg{err}
		}
		return terminalClosedMsg{terminalID: item.Info.ID}
	}
}

func (m *Model) attachTerminalToActivePaneCmd(item terminalPickerItem) tea.Cmd {
	tab := m.currentTab()
	if tab == nil {
		return nil
	}
	pane := activePane(tab)
	if pane == nil {
		return nil
	}
	paneID := pane.ID
	return func() tea.Msg {
		attached, err := m.client.Attach(context.Background(), item.Info.ID, "collaborator")
		if err != nil {
			return errMsg{err}
		}
		snap, err := m.client.Snapshot(context.Background(), item.Info.ID, 0, 200)
		if err != nil {
			return errMsg{err}
		}
		return paneReplacedMsg{
			paneID: paneID,
			pane: &Pane{
				ID:       paneID,
				Title:    paneTitleForTerminal(item.Info),
				Viewport: m.newViewport(item.Info.ID, attached.Channel, snap),
			},
		}
	}
}

func (m *Model) attachTerminalToNewPaneCmd(tabIndex int, targetID string, split SplitDirection, item terminalPickerItem) tea.Cmd {
	return func() tea.Msg {
		attached, err := m.client.Attach(context.Background(), item.Info.ID, "collaborator")
		if err != nil {
			return errMsg{err}
		}
		snap, err := m.client.Snapshot(context.Background(), item.Info.ID, 0, 200)
		if err != nil {
			return errMsg{err}
		}
		paneID := m.nextPaneID()
		return paneCreatedMsg{
			tabIndex: tabIndex,
			targetID: targetID,
			split:    split,
			pane: &Pane{
				ID:       paneID,
				Title:    paneTitleForTerminal(item.Info),
				Viewport: m.newViewport(item.Info.ID, attached.Channel, snap),
			},
		}
	}
}

func paneTitleForTerminal(info protocol.TerminalInfo) string {
	command := ""
	if len(info.Command) > 0 {
		command = filepath.Base(info.Command[0])
	}
	return paneTitleForCommand(info.Name, command, info.ID)
}

func (m *Model) renderTerminalPicker() string {
	tabBar := m.renderTabBar()
	status := m.renderStatus()
	contentHeight := max(1, m.height-2)
	canvas := newCanvas(m.width, contentHeight)

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f8fafc")).Background(lipgloss.Color("#0f172a")).Bold(true)
	queryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#dbeafe")).Background(lipgloss.Color("#1d4ed8")).Bold(true)
	canvas.drawText(Rect{X: 2, Y: 1, W: max(1, m.width-4), H: 1}, []string{titleStyle.Render("Terminal Picker")})
	query := ""
	if m.terminalPicker != nil {
		query = m.terminalPicker.Query
	}
	canvas.drawText(Rect{X: 2, Y: 3, W: max(1, m.width-4), H: 1}, []string{queryStyle.Render(" query: " + query + "_" + " ")})

	lines := make([]string, 0, max(0, contentHeight-7))
	if m.terminalPicker != nil {
		for i, item := range m.terminalPicker.Filtered {
			prefix := "  "
			if i == m.terminalPicker.Selected {
				prefix = "> "
			}
			marker := "○"
			if item.Observed {
				marker = "●"
			}
			location := item.Location
			if location == "" {
				location = "(orphan)"
			}
			command := strings.Join(item.Info.Command, " ")
			if command == "" {
				command = item.Info.Name
			}
			line := fmt.Sprintf("%s%s %s %s [%s]", prefix, marker, item.Info.ID, strings.TrimSpace(command), location)
			lines = append(lines, trimToWidth(line, max(1, m.width-4)))
		}
	}
	if len(lines) == 0 {
		lines = []string{"  no terminals match"}
	}
	canvas.drawText(Rect{X: 2, Y: 5, W: max(1, m.width-4), H: max(1, contentHeight-7)}, lines)
	canvas.drawText(Rect{X: 2, Y: max(6, contentHeight-2), W: max(1, m.width-4), H: 1}, []string{
		trimToWidth("[Enter] attach  [Tab] split+attach  [Ctrl-k] kill  [Esc] close", max(1, m.width-4)),
	})

	body := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e2e8f0")).
		Background(lipgloss.Color("#0f172a")).
		Render(forceHeight(canvas.String(), contentHeight))
	return strings.Join([]string{tabBar, body, status}, "\n")
}

func decodeInputRune(data []byte) (rune, int) {
	r, size := utf8.DecodeRune(data)
	if r == utf8.RuneError && size == 1 && !utf8.FullRune(data) {
		return 0, 0
	}
	return r, size
}
