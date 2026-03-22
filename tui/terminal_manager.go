package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
)

var (
	terminalManagerSectionStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#fef3c7")).
		Background(lipgloss.Color("#172033")).
		Bold(true).
		Padding(0, 1)
)

func (m *Model) openTerminalManagerCmd() tea.Cmd {
	return m.loadTerminalManagerCmd("", "")
}

func (m *Model) refreshTerminalManagerCmd() tea.Cmd {
	if m == nil || m.terminalManager == nil {
		return nil
	}
	selectedID := ""
	if item := m.selectedTerminalManagerItem(); item != nil && !item.CreateNew {
		selectedID = item.Info.ID
	}
	return m.loadTerminalManagerCmd(m.terminalManager.Query, selectedID)
}

func (m *Model) loadTerminalManagerCmd(query, selectedID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		result, err := m.client.List(ctx)
		if err != nil {
			return terminalManagerLoadedMsg{err: m.wrapClientError("list terminals", err, "view", "terminal-manager")}
		}
		manager := &terminalManager{
			Query: query,
			Items: m.buildTerminalPickerItems(result.Terminals, true, time.Now()),
		}
		manager.applyFilter()
		if strings.TrimSpace(selectedID) == "" {
			selectedID = m.defaultTerminalManagerSelectionID(manager)
		}
		manager.selectID(selectedID)
		return terminalManagerLoadedMsg{manager: manager}
	}
}

func (m *Model) defaultTerminalManagerSelectionID(manager *terminalManager) string {
	if manager == nil {
		return ""
	}
	activeTerminalID := ""
	if pane := activePane(m.currentTab()); pane != nil {
		activeTerminalID = strings.TrimSpace(pane.TerminalID)
	}
	fallback := ""
	for i := range manager.Filtered {
		item := manager.Filtered[i]
		if item.CreateNew {
			continue
		}
		if activeTerminalID != "" && item.Info.ID == activeTerminalID {
			return item.Info.ID
		}
		if fallback == "" {
			fallback = item.Info.ID
		}
		if activeTerminalID == "" {
			return item.Info.ID
		}
	}
	return fallback
}

func (tm *terminalManager) applyFilter() {
	query := strings.TrimSpace(strings.ToLower(tm.Query))
	tm.Filtered = tm.Filtered[:0]
	for i := range tm.Items {
		item := tm.Items[i]
		if query == "" || strings.Contains(item.searchTextLower, query) {
			tm.Filtered = append(tm.Filtered, item)
		}
	}
	if len(tm.Filtered) == 0 {
		tm.Selected = 0
		return
	}
	if tm.Selected < 0 {
		tm.Selected = 0
	}
	if tm.Selected >= len(tm.Filtered) {
		tm.Selected = len(tm.Filtered) - 1
	}
	tm.normalizeSelection()
}

func (tm *terminalManager) selectID(terminalID string) {
	if tm == nil || strings.TrimSpace(terminalID) == "" {
		tm.normalizeSelection()
		return
	}
	for i := range tm.Filtered {
		if tm.Filtered[i].CreateNew {
			continue
		}
		if tm.Filtered[i].Info.ID == terminalID {
			tm.Selected = i
			return
		}
	}
	tm.normalizeSelection()
}

func (tm *terminalManager) normalizeSelection() {
	if tm == nil || len(tm.Filtered) == 0 {
		return
	}
	if tm.Selected < 0 {
		tm.Selected = 0
	}
	if tm.Selected >= len(tm.Filtered) {
		tm.Selected = len(tm.Filtered) - 1
	}
	if !tm.Filtered[tm.Selected].CreateNew {
		return
	}
	for i := range tm.Filtered {
		if !tm.Filtered[i].CreateNew {
			tm.Selected = i
			return
		}
	}
}

func (m *Model) closeTerminalManager() tea.Cmd {
	if m.terminalManager == nil {
		return nil
	}
	m.terminalManager = nil
	m.invalidateRender()
	return nil
}

func (m *Model) moveTerminalManagerSelection(delta int) {
	if m.terminalManager == nil || len(m.terminalManager.Filtered) == 0 {
		return
	}
	next := m.terminalManager.Selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.terminalManager.Filtered) {
		next = len(m.terminalManager.Filtered) - 1
	}
	m.terminalManager.Selected = next
	m.invalidateRender()
}

func (m *Model) appendTerminalManagerQuery(value string) {
	if m.terminalManager == nil || value == "" {
		return
	}
	m.terminalManager.Query += value
	m.terminalManager.applyFilter()
	m.invalidateRender()
}

func (m *Model) deleteTerminalManagerRune() {
	if m.terminalManager == nil || m.terminalManager.Query == "" {
		return
	}
	_, size := utf8.DecodeLastRuneInString(m.terminalManager.Query)
	if size <= 0 {
		m.terminalManager.Query = ""
	} else {
		m.terminalManager.Query = m.terminalManager.Query[:len(m.terminalManager.Query)-size]
	}
	m.terminalManager.applyFilter()
	m.invalidateRender()
}

func (m *Model) selectedTerminalManagerItem() *terminalPickerItem {
	if m.terminalManager == nil || len(m.terminalManager.Filtered) == 0 {
		return nil
	}
	m.terminalManager.normalizeSelection()
	item := m.terminalManager.Filtered[m.terminalManager.Selected]
	return &item
}

func (m *Model) resolveTerminalManagerSelection(action terminalPickerAction) tea.Cmd {
	item := m.selectedTerminalManagerItem()
	if item == nil {
		return nil
	}
	m.terminalManager = nil
	m.invalidateRender()
	return m.resolveTerminalPickerSelection(action, *item, false)
}

func (m *Model) editTerminalManagerSelection() tea.Cmd {
	item := m.selectedTerminalManagerItem()
	if item == nil || item.CreateNew {
		return nil
	}
	m.terminalManager = nil
	m.beginTerminalEditPrompt(item.Info)
	m.invalidateRender()
	return nil
}

func (m *Model) killTerminalManagerSelection() tea.Cmd {
	item := m.selectedTerminalManagerItem()
	if item == nil || item.CreateNew {
		return nil
	}
	return m.beginTerminalStopPrompt(item.Info.ID, terminalDisplayLabel(item.Info.Name, item.Info.Command), terminalBindingCount(m.workspace.Tabs, item.Info.ID), "terminal manager")
}

func (m *Model) handleTerminalManagerKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		return m.closeTerminalManager()
	case tea.KeyEnter:
		return m.resolveTerminalManagerSelection(terminalPickerAction{Kind: terminalPickerActionReplace, TabIndex: m.workspace.ActiveTab})
	case tea.KeyUp:
		m.moveTerminalManagerSelection(-1)
	case tea.KeyDown:
		m.moveTerminalManagerSelection(1)
	case tea.KeyBackspace:
		m.deleteTerminalManagerRune()
	case tea.KeyCtrlT:
		return m.resolveTerminalManagerSelection(terminalPickerAction{Kind: terminalPickerActionNewTab})
	case tea.KeyCtrlO:
		return m.resolveTerminalManagerSelection(terminalPickerAction{Kind: terminalPickerActionFloating, TabIndex: m.workspace.ActiveTab})
	case tea.KeyCtrlK:
		return m.killTerminalManagerSelection()
	case tea.KeyCtrlE:
		return m.editTerminalManagerSelection()
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return nil
		}
		m.appendTerminalManagerQuery(string(msg.Runes))
	}
	return nil
}

func (m *Model) consumeTerminalManagerInput() (int, tea.Cmd, bool) {
	if len(m.rawPending) == 0 {
		return 0, nil, false
	}
	if n, dir, ok, incomplete := parseArrowPrefix(m.rawPending); incomplete {
		return 0, nil, false
	} else if ok {
		switch dir {
		case DirectionUp:
			m.moveTerminalManagerSelection(-1)
		case DirectionDown:
			m.moveTerminalManagerSelection(1)
		}
		return n, nil, true
	}
	switch m.rawPending[0] {
	case '\r', '\n':
		return 1, m.resolveTerminalManagerSelection(terminalPickerAction{Kind: terminalPickerActionReplace, TabIndex: m.workspace.ActiveTab}), true
	case 0x14:
		return 1, m.resolveTerminalManagerSelection(terminalPickerAction{Kind: terminalPickerActionNewTab}), true
	case 0x0f:
		return 1, m.resolveTerminalManagerSelection(terminalPickerAction{Kind: terminalPickerActionFloating, TabIndex: m.workspace.ActiveTab}), true
	case 0x05:
		return 1, m.editTerminalManagerSelection(), true
	case 0x0b:
		return 1, m.killTerminalManagerSelection(), true
	case 0x7f, 0x08:
		m.deleteTerminalManagerRune()
		return 1, nil, true
	case 0x1b:
		if len(m.rawPending) == 1 {
			return 1, m.closeTerminalManager(), true
		}
		return 0, nil, false
	}
	r, size := decodeInputRune(m.rawPending)
	if size == 0 {
		return 0, nil, false
	}
	switch r {
	default:
		if r >= 0x20 {
			m.appendTerminalManagerQuery(string(r))
		}
		return size, nil, true
	}
}

func (m *Model) handleTerminalManagerEvent(event uv.Event) tea.Cmd {
	switch event := event.(type) {
	case uv.KeyPressEvent:
		switch {
		case event.MatchString("esc"):
			return m.closeTerminalManager()
		case event.MatchString("enter"):
			return m.resolveTerminalManagerSelection(terminalPickerAction{Kind: terminalPickerActionReplace, TabIndex: m.workspace.ActiveTab})
		case event.MatchString("up"):
			m.moveTerminalManagerSelection(-1)
		case event.MatchString("down"):
			m.moveTerminalManagerSelection(1)
		case event.MatchString("ctrl+t"):
			return m.resolveTerminalManagerSelection(terminalPickerAction{Kind: terminalPickerActionNewTab})
		case event.MatchString("ctrl+o"):
			return m.resolveTerminalManagerSelection(terminalPickerAction{Kind: terminalPickerActionFloating, TabIndex: m.workspace.ActiveTab})
		case event.MatchString("ctrl+e"):
			return m.editTerminalManagerSelection()
		case event.MatchString("ctrl+k"):
			return m.killTerminalManagerSelection()
		case event.MatchString("backspace"):
			m.deleteTerminalManagerRune()
		case event.Text != "":
			m.appendTerminalManagerQuery(event.Text)
		}
	case uv.PasteEvent:
		m.appendTerminalManagerQuery(event.Content)
	}
	return nil
}

func (m *Model) renderTerminalManager() string {
	tabBar := m.renderTabBar()
	status := m.renderStatus()
	contentHeight := max(1, m.height-2)
	listWidth := max(34, min(56, m.width/2))
	detailWidth := max(24, m.width-listWidth-1)

	listInnerWidth := max(10, listWidth-2)
	if m.terminalManager != nil && m.terminalManager.RenderWidth != listInnerWidth {
		m.terminalManager.RenderWidth = listInnerWidth
		for i := range m.terminalManager.Items {
			m.terminalManager.Items[i].lineWidth = 0
		}
	}

	listBody := m.renderTerminalManagerListBody(listInnerWidth)

	detailBody := m.renderTerminalManagerDetail(detailWidth - 2)
	leftBox := terminalManagerBox("Running Terminals", listWidth, contentHeight, append([]string{
		terminalPickerQueryStyle.Render(forceWidthANSI("search: "+m.terminalManager.Query+"_", listInnerWidth)),
		"",
	}, listBody...))
	rightBox := terminalManagerBox("Terminal Details", detailWidth, contentHeight, detailBody)

	bodyLines := make([]string, 0, contentHeight)
	for i := 0; i < contentHeight; i++ {
		left := ""
		if i < len(leftBox) {
			left = leftBox[i]
		}
		right := ""
		if i < len(rightBox) {
			right = rightBox[i]
		}
		bodyLines = append(bodyLines, forceWidthANSI(left, listWidth)+" "+forceWidthANSI(right, detailWidth))
	}
	return strings.Join([]string{tabBar, strings.Join(bodyLines, "\n"), status}, "\n")
}

func (m *Model) renderTerminalManagerListBody(width int) []string {
	width = max(10, width)
	if m.terminalManager == nil {
		return []string{pickerLineStyle.Render(forceWidthANSI(" no terminals", width))}
	}
	if len(m.terminalManager.Filtered) == 0 {
		return []string{pickerLineStyle.Render(forceWidthANSI(" no terminals", width))}
	}
	sections := m.terminalManagerSections()
	body := make([]string, 0, len(m.terminalManager.Filtered)+8)
	selected := m.selectedTerminalManagerItem()
	for sectionIndex, section := range sections {
		if len(section.Items) == 0 {
			continue
		}
		if len(body) > 0 && sectionIndex > 0 {
			body = append(body, "")
		}
		header := terminalManagerSectionStyle.Render(forceWidthANSI(" "+section.Title+" ", width))
		body = append(body, header)
		for _, item := range section.Items {
			isSelected := false
			if selected != nil {
				if item.CreateNew && selected.CreateNew {
					isSelected = true
				} else if !item.CreateNew && !selected.CreateNew && item.Info.ID == selected.Info.ID {
					isSelected = true
				}
			}
			body = append(body, item.line(width, isSelected))
		}
	}
	if len(body) == 0 {
		return []string{pickerLineStyle.Render(forceWidthANSI(" no terminals", width))}
	}
	return body
}

type terminalManagerSection struct {
	Title string
	Items []*terminalPickerItem
}

func (m *Model) terminalManagerSections() []terminalManagerSection {
	if m == nil || m.terminalManager == nil {
		return nil
	}
	sections := []terminalManagerSection{
		{Title: "NEW"},
		{Title: "VISIBLE"},
		{Title: "PARKED"},
		{Title: "EXITED"},
	}
	locations := m.terminalDisplayLocations()
	for i := range m.terminalManager.Filtered {
		item := &m.terminalManager.Filtered[i]
		switch {
		case item.CreateNew:
			sections[0].Items = append(sections[0].Items, item)
		default:
			switch terminalVisibility(item.Info, locations[item.Info.ID]) {
			case "visible":
				sections[1].Items = append(sections[1].Items, item)
			case "parked":
				sections[2].Items = append(sections[2].Items, item)
			default:
				sections[3].Items = append(sections[3].Items, item)
			}
		}
	}
	return sections
}

func (m *Model) renderTerminalManagerDetail(width int) []string {
	width = max(10, width)
	item := m.selectedTerminalManagerItem()
	if item == nil {
		return []string{"no terminal selected"}
	}
	if item.CreateNew {
		return []string{
			"Create New Terminal",
			"",
			"Enter: start in current pane",
			"Ctrl-t: create in new tab",
			"Ctrl-o: create as floating pane",
			"",
			"default command: " + m.cfg.DefaultShell,
		}
	}
	info := item.Info
	locations := m.terminalDisplayLocations()[info.ID]
	visibility := terminalVisibility(info, locations)
	lines := []string{
		terminalDisplayLabel(info.Name, info.Command),
		"",
		"state: " + terminalInfoStateLabel(info),
		"visibility: " + visibility,
		"command: " + strings.TrimSpace(strings.Join(info.Command, " ")),
		"id: " + info.ID,
		fmt.Sprintf("open panes: %d", terminalBindingCount(m.workspace.Tabs, info.ID)),
	}
	if info.ExitCode != nil {
		lines = append(lines, fmt.Sprintf("exit code: %d", *info.ExitCode))
	}
	if formatted := formatTerminalTags(info.Tags); formatted != "tags:-" {
		lines = append(lines, "tags: "+formatted)
	}
	lines = append(lines, "")
	if len(locations) > 0 {
		lines = append(lines, "shown in:")
		for _, location := range locations {
			lines = append(lines, "- "+location)
		}
	} else if visibility == "parked" {
		lines = append(lines, "shown in:", "- parked terminal (not visible in any pane)")
	}
	lines = append(lines,
		"",
		"Enter brings this terminal into the active pane.",
		"Use Ctrl-t / Ctrl-o to open in a new tab or floating pane.",
	)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, forceWidthANSI(line, width))
	}
	return out
}

func terminalVisibility(info protocol.TerminalInfo, locations []string) string {
	if defaultTerminalState(info.State) != "running" {
		return "exited"
	}
	if len(locations) > 0 {
		return "visible"
	}
	return "parked"
}

func terminalManagerBox(title string, width, height int, body []string) []string {
	width = max(4, width)
	height = max(3, height)
	innerW := width - 2
	contentLines := make([]string, 0, height)
	topTitle := xansi.Truncate(" "+title+" ", innerW, "")
	contentLines = append(contentLines,
		pickerBorderStyle.Render("┌")+
			terminalPickerTitleStyle.Render(topTitle)+
			pickerBorderStyle.Render(strings.Repeat("─", max(0, innerW-xansi.StringWidth(topTitle))))+
			pickerBorderStyle.Render("┐"),
	)
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		contentLines = append(contentLines, pickerBorderStyle.Render("│")+forceWidthANSI(line, innerW)+pickerBorderStyle.Render("│"))
	}
	contentLines = append(contentLines, pickerBorderStyle.Render("└"+strings.Repeat("─", innerW)+"┘"))
	return contentLines
}
