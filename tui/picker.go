package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
)

var (
	terminalPickerTitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f8fafc")).Background(lipgloss.Color("#0f172a")).Bold(true)
	terminalPickerQueryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#dbeafe")).Background(lipgloss.Color("#1d4ed8")).Bold(true)
	terminalPickerBodyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0")).Background(lipgloss.Color("#0f172a"))
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
			ctx, cancel := m.requestContext()
			defer cancel()
			_ = m.client.Input(ctx, channel, data)
		}),
		Snapshot:      snap,
		Command:       []string{m.cfg.DefaultShell},
		TerminalState: "running",
		Mode:          ViewportModeFit,
		renderDirty:   true,
	}
}

func (m *Model) openTerminalPickerCmd() tea.Cmd {
	action := terminalPickerAction{Kind: terminalPickerActionReplace, TabIndex: m.workspace.ActiveTab}
	allowCreate := false
	if activePane(m.currentTab()) == nil {
		action.Kind = terminalPickerActionBootstrap
		allowCreate = true
	}
	return m.openPickerCmd(
		action,
		"Terminal Picker",
		"[Enter] attach  [Tab] split+attach  [Ctrl-k] kill  [Esc] close",
		allowCreate,
	)
}

func (m *Model) openBootstrapTerminalPickerCmd(tabIndex int) tea.Cmd {
	return m.openPickerCmd(
		terminalPickerAction{Kind: terminalPickerActionBootstrap, TabIndex: tabIndex},
		"Choose Terminal",
		"[Enter] open selected terminal or create new  [Esc] close",
		true,
	)
}

func (m *Model) openNewTabTerminalPickerCmd() tea.Cmd {
	return m.openPickerCmd(
		terminalPickerAction{Kind: terminalPickerActionNewTab},
		"Open Tab",
		"[Enter] open selected terminal or create new  [Esc] close",
		true,
	)
}

func (m *Model) openSplitTerminalPickerCmd(tabIndex int, targetID string, split SplitDirection) tea.Cmd {
	return m.openPickerCmd(
		terminalPickerAction{Kind: terminalPickerActionSplit, TabIndex: tabIndex, TargetID: targetID, Split: split},
		"Open Viewport",
		"[Enter] open selected terminal or create new  [Esc] close",
		true,
	)
}

func (m *Model) openFloatingTerminalPickerCmd(tabIndex int) tea.Cmd {
	return m.openPickerCmd(
		terminalPickerAction{Kind: terminalPickerActionFloating, TabIndex: tabIndex},
		"Open Floating Viewport",
		"[Enter] open selected terminal or create new  [Esc] close",
		true,
	)
}

func (m *Model) openLayoutResolvePickerCmd(plan LayoutCreatePlan) tea.Cmd {
	title := "Resolve Layout Viewport"
	if plan.Terminal.Command != "" {
		title = "Resolve Layout Viewport: " + plan.Terminal.Command
	}
	return m.openPickerCmd(
		terminalPickerAction{Kind: terminalPickerActionLayoutResolve, PaneID: plan.PaneID},
		title,
		"[Enter] attach  [Create New] first row  [Esc] skip",
		true,
	)
}

func (m *Model) openPickerCmd(action terminalPickerAction, title, footer string, allowCreate bool) tea.Cmd {
	return func() tea.Msg {
		m.logger.Debug("loading terminal picker", "title", title, "allow_create", allowCreate, "action", action.Kind)
		ctx, cancel := m.requestContext()
		defer cancel()
		result, err := m.client.List(ctx)
		if err != nil {
			return terminalPickerLoadedMsg{err: m.wrapClientError("list terminals", err, "title", title)}
		}
		if allowCreate && len(result.Terminals) == 0 && action.Kind != terminalPickerActionLayoutResolve {
			m.logger.Info("picker empty, auto-creating terminal", "title", title)
			cmd := m.resolveTerminalPickerSelection(action, terminalPickerItem{
				CreateNew:   true,
				Label:       "new terminal",
				Description: "Create a new terminal in this viewport",
			}, false)
			if cmd == nil {
				return nil
			}
			return cmd()
		}
		picker := &terminalPicker{
			Title:    title,
			Footer:   footer,
			Action:   action,
			OpenedAt: time.Now(),
			Items:    m.buildTerminalPickerItems(result.Terminals, allowCreate, time.Now()),
		}
		picker.applyFilter()
		return terminalPickerLoadedMsg{picker: picker}
	}
}

func (m *Model) buildTerminalPickerItems(terminals []protocol.TerminalInfo, allowCreate bool, now time.Time) []terminalPickerItem {
	locations := m.terminalLocations()
	items := make([]terminalPickerItem, 0, len(terminals)+1)
	if allowCreate {
		item := terminalPickerItem{
			CreateNew:   true,
			Label:       "new terminal",
			Description: "Create a new terminal in this viewport",
		}
		item.primeCaches(now)
		items = append(items, item)
	}
	for _, info := range terminals {
		locs := append([]string(nil), locations[info.ID]...)
		slices.Sort(locs)
		item := terminalPickerItem{
			Info:     info,
			Observed: len(locs) > 0,
			Orphan:   len(locs) == 0,
			Location: strings.Join(locs, ", "),
		}
		item.primeCaches(now)
		items = append(items, item)
	}
	slices.SortStableFunc(items, func(a, b terminalPickerItem) int {
		if a.CreateNew != b.CreateNew {
			if a.CreateNew {
				return -1
			}
			return 1
		}
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
	m.snapshotCurrentWorkspace()
	locations := make(map[string][]string)
	for _, workspaceName := range m.workspaceOrder {
		workspace, ok := m.workspaceStore[workspaceName]
		if !ok {
			continue
		}
		for _, tab := range workspace.Tabs {
			if tab == nil {
				continue
			}
			seen := make(map[string]struct{})
			location := fmt.Sprintf("ws:%s / tab:%s", workspace.Name, tab.Name)
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
	}
	return locations
}

func (p *terminalPicker) applyFilter() {
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

func (i terminalPickerItem) searchText() string {
	if i.CreateNew {
		return strings.Join([]string{i.Label, i.Description, "create new terminal"}, " ")
	}
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

func (i *terminalPickerItem) primeCaches(now time.Time) {
	if i == nil {
		return
	}
	i.searchTextLower = strings.ToLower(i.searchTextValue())
	i.lineBody = i.lineBodyValue(now)
	i.lineWidth = 0
	i.lineNormal = ""
	i.lineActive = ""
}

func (i terminalPickerItem) searchTextValue() string {
	return i.searchText()
}

func (i terminalPickerItem) lineBodyValue(now time.Time) string {
	if i.CreateNew {
		return fmt.Sprintf("+ %s | %s", i.Label, i.Description)
	}
	marker := "○"
	if i.Observed {
		marker = "●"
	}
	location := i.Location
	if location == "" {
		location = "(orphan)"
	}
	command := strings.Join(i.Info.Command, " ")
	if command == "" {
		command = i.Info.Name
	}
	return fmt.Sprintf(
		"%s %s %s | %s | %s | %s [%s]",
		marker,
		i.Info.ID,
		strings.TrimSpace(command),
		terminalInfoStateLabel(i.Info),
		formatTerminalAge(i.Info.CreatedAt, now),
		formatTerminalTags(i.Info.Tags),
		location,
	)
}

func (i *terminalPickerItem) line(width int, selected bool) string {
	if i == nil {
		return ""
	}
	if width <= 0 {
		return ""
	}
	if i.lineBody == "" {
		i.primeCaches(time.Now())
	}
	if i.lineWidth != width {
		i.lineWidth = width
		i.lineNormal = trimToWidth("  "+i.lineBody, width)
		i.lineActive = trimToWidth("> "+i.lineBody, width)
	}
	if selected {
		return i.lineActive
	}
	return i.lineNormal
}

func (m *Model) handleTerminalPickerKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		return m.closeTerminalPicker()
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
			return 1, m.closeTerminalPicker(), true
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
			return m.closeTerminalPicker()
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
	_, size := utf8.DecodeLastRuneInString(m.terminalPicker.Query)
	if size <= 0 {
		m.terminalPicker.Query = ""
	} else {
		m.terminalPicker.Query = m.terminalPicker.Query[:len(m.terminalPicker.Query)-size]
	}
	m.terminalPicker.applyFilter()
	m.invalidateRender()
}

func (m *Model) attachPickerSelection(split bool) tea.Cmd {
	if m.terminalPicker == nil || len(m.terminalPicker.Filtered) == 0 {
		return nil
	}
	item := m.terminalPicker.Filtered[m.terminalPicker.Selected]
	action := m.terminalPicker.Action
	if item.CreateNew {
		m.logger.Info("terminal picker selected create new", "title", m.terminalPicker.Title, "action", action.Kind, "split", split)
	} else {
		m.logger.Info("terminal picker selected terminal", "title", m.terminalPicker.Title, "action", action.Kind, "split", split, "terminal_id", item.Info.ID)
	}
	m.terminalPicker = nil
	m.invalidateRender()
	return m.resolveTerminalPickerSelection(action, item, split)
}

func (m *Model) closeTerminalPicker() tea.Cmd {
	if m.terminalPicker == nil {
		return nil
	}
	action := m.terminalPicker.Action
	m.terminalPicker = nil
	m.invalidateRender()
	if action.Kind == terminalPickerActionLayoutResolve {
		return m.advanceLayoutPromptCmd()
	}
	return nil
}

func (m *Model) resolveTerminalPickerSelection(action terminalPickerAction, item terminalPickerItem, split bool) tea.Cmd {
	switch action.Kind {
	case terminalPickerActionBootstrap:
		if item.CreateNew {
			return m.createPaneCmd(action.TabIndex, "", "")
		}
		return m.attachTerminalToBootstrapCmd(action.TabIndex, item)
	case terminalPickerActionNewTab:
		m.workspace.Tabs = append(m.workspace.Tabs, newTab(nextTabName(m.workspace.Tabs)))
		m.workspace.ActiveTab = len(m.workspace.Tabs) - 1
		m.invalidateRender()
		if item.CreateNew {
			return m.createPaneCmd(m.workspace.ActiveTab, "", "")
		}
		return m.attachTerminalToBootstrapCmd(m.workspace.ActiveTab, item)
	case terminalPickerActionSplit:
		if item.CreateNew {
			return m.createPaneCmd(action.TabIndex, action.TargetID, action.Split)
		}
		return m.attachTerminalToNewPaneCmd(action.TabIndex, action.TargetID, action.Split, item)
	case terminalPickerActionFloating:
		if item.CreateNew {
			return m.createFloatingPaneCmd(action.TabIndex)
		}
		return m.attachTerminalToFloatingPaneCmd(action.TabIndex, item)
	case terminalPickerActionLayoutResolve:
		if item.CreateNew {
			return m.createTerminalForPaneCmd(action.PaneID)
		}
		return m.attachTerminalToPaneCmd(action.PaneID, item)
	default:
		if split {
			tab := m.currentTab()
			if tab == nil {
				return nil
			}
			return m.attachTerminalToNewPaneCmd(m.workspace.ActiveTab, tab.ActivePaneID, SplitVertical, item)
		}
		return m.attachTerminalToActivePaneCmd(item)
	}
}

func (m *Model) killPickerSelection() tea.Cmd {
	if m.terminalPicker == nil || len(m.terminalPicker.Filtered) == 0 {
		return nil
	}
	item := m.terminalPicker.Filtered[m.terminalPicker.Selected]
	if item.CreateNew {
		return nil
	}
	m.logger.Warn("terminal picker killing terminal", "terminal_id", item.Info.ID)
	m.terminalPicker = nil
	m.invalidateRender()
	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()
		if err := m.client.Kill(ctx, item.Info.ID); err != nil {
			return errMsg{m.wrapClientError("kill terminal", err, "terminal_id", item.Info.ID)}
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
	return m.attachTerminalToPaneCmd(paneID, item)
}

func (m *Model) attachTerminalToPaneCmd(paneID string, item terminalPickerItem) tea.Cmd {
	existing := findPane(m.workspace.Tabs, paneID)
	mode := ViewportModeFit
	offset := Point{}
	pin := false
	readonly := false
	if existing != nil {
		mode = existing.Mode
		offset = existing.Offset
		pin = existing.Pin
		readonly = existing.Readonly
	}
	return func() tea.Msg {
		m.logger.Debug("attaching terminal to active pane", "pane_id", paneID, "terminal_id", item.Info.ID)
		ctx, cancel := m.requestContext()
		defer cancel()
		attached, err := m.client.Attach(ctx, item.Info.ID, "collaborator")
		if err != nil {
			return errMsg{m.wrapClientError("attach terminal", err, "pane_id", paneID, "terminal_id", item.Info.ID)}
		}
		snap, err := m.client.Snapshot(ctx, item.Info.ID, 0, 200)
		if err != nil {
			return errMsg{m.wrapClientError("snapshot terminal", err, "pane_id", paneID, "terminal_id", item.Info.ID)}
		}
		return paneReplacedMsg{
			paneID: paneID,
			pane: &Pane{
				ID:    paneID,
				Title: paneTitleForTerminal(item.Info),
				Viewport: func() *Viewport {
					view := viewportWithTerminalInfo(m.newViewport(item.Info.ID, attached.Channel, snap), item.Info)
					view.Mode = mode
					view.Offset = offset
					view.Pin = pin
					view.Readonly = readonly
					return view
				}(),
			},
		}
	}
}

func (m *Model) createTerminalForPaneCmd(paneID string) tea.Cmd {
	pane := findPane(m.workspace.Tabs, paneID)
	if pane == nil {
		return nil
	}

	command := append([]string(nil), pane.Command...)
	if len(command) == 0 {
		command = []string{m.cfg.DefaultShell}
	}
	name := pane.Name
	tags := cloneStringMap(pane.Tags)
	mode := pane.Mode
	offset := pane.Offset
	pin := pane.Pin
	readonly := pane.Readonly
	size := paneCreateSize(pane)
	tab := m.tabForPane(paneID)
	if viewW, viewH, ok := m.paneViewportSizeInTab(tab, paneID); ok {
		size = paneRestartSize(pane, size, viewW, viewH)
	}

	return func() tea.Msg {
		ctx, cancel := m.requestContext()
		defer cancel()

		created, err := m.client.Create(ctx, command, name, size)
		if err != nil {
			return errMsg{m.wrapClientError("create terminal", err, "pane_id", paneID)}
		}
		if len(tags) > 0 {
			if err := m.client.SetTags(ctx, created.TerminalID, tags); err != nil {
				return errMsg{m.wrapClientError("set terminal tags", err, "pane_id", paneID, "terminal_id", created.TerminalID)}
			}
		}
		attached, err := m.client.Attach(ctx, created.TerminalID, "collaborator")
		if err != nil {
			return errMsg{m.wrapClientError("attach terminal", err, "pane_id", paneID, "terminal_id", created.TerminalID)}
		}
		snap, err := m.client.Snapshot(ctx, created.TerminalID, 0, 200)
		if err != nil {
			return errMsg{m.wrapClientError("snapshot terminal", err, "pane_id", paneID, "terminal_id", created.TerminalID)}
		}
		if snap != nil {
			if snap.Size.Cols < size.Cols {
				snap.Size.Cols = size.Cols
			}
			if snap.Size.Rows < size.Rows {
				snap.Size.Rows = size.Rows
			}
		}
		view := m.newViewport(created.TerminalID, attached.Channel, snap)
		if view.VTerm != nil {
			view.VTerm.Resize(int(size.Cols), int(size.Rows))
		}
		view.Name = name
		view.Command = command
		view.Tags = cloneStringMap(tags)
		view.TerminalState = "running"
		view.Mode = mode
		view.Offset = offset
		view.Pin = pin
		view.Readonly = readonly
		return paneReplacedMsg{
			paneID: paneID,
			pane: &Pane{
				ID:       paneID,
				Title:    paneTitleForCommand(name, firstCommandWord(command), created.TerminalID),
				Viewport: view,
			},
		}
	}
}

func (m *Model) attachInitialTerminalCmd(tabIndex int, terminalID string) tea.Cmd {
	return func() tea.Msg {
		m.logger.Debug("bootstrapping attach terminal", "tab_index", tabIndex, "terminal_id", terminalID)
		info := protocol.TerminalInfo{ID: terminalID, State: "running"}
		ctx, cancel := m.requestContext()
		defer cancel()
		if list, err := m.client.List(ctx); err == nil {
			for _, item := range list.Terminals {
				if item.ID == terminalID {
					info = item
					break
				}
			}
		}
		cmd := m.attachTerminalToBootstrapCmd(tabIndex, terminalPickerItem{Info: info})
		if cmd == nil {
			return nil
		}
		return cmd()
	}
}

func (m *Model) attachTerminalToBootstrapCmd(tabIndex int, item terminalPickerItem) tea.Cmd {
	return func() tea.Msg {
		m.logger.Debug("attaching terminal to bootstrap pane", "tab_index", tabIndex, "terminal_id", item.Info.ID)
		ctx, cancel := m.requestContext()
		defer cancel()
		attached, err := m.client.Attach(ctx, item.Info.ID, "collaborator")
		if err != nil {
			return errMsg{m.wrapClientError("attach terminal", err, "tab_index", tabIndex, "terminal_id", item.Info.ID)}
		}
		snap, err := m.client.Snapshot(ctx, item.Info.ID, 0, 200)
		if err != nil {
			return errMsg{m.wrapClientError("snapshot terminal", err, "tab_index", tabIndex, "terminal_id", item.Info.ID)}
		}
		paneID := m.nextPaneID()
		title := paneTitleForTerminal(item.Info)
		if title == "" {
			title = paneTitleForCommand("", m.cfg.DefaultShell, item.Info.ID)
		}
		return paneCreatedMsg{
			tabIndex: tabIndex,
			pane: &Pane{
				ID:       paneID,
				Title:    title,
				Viewport: viewportWithTerminalInfo(m.newViewport(item.Info.ID, attached.Channel, snap), item.Info),
			},
		}
	}
}

func (m *Model) attachTerminalToNewPaneCmd(tabIndex int, targetID string, split SplitDirection, item terminalPickerItem) tea.Cmd {
	return func() tea.Msg {
		m.logger.Debug("attaching terminal to new pane", "tab_index", tabIndex, "target_id", targetID, "split", split, "terminal_id", item.Info.ID)
		ctx, cancel := m.requestContext()
		defer cancel()
		attached, err := m.client.Attach(ctx, item.Info.ID, "collaborator")
		if err != nil {
			return errMsg{m.wrapClientError("attach terminal", err, "tab_index", tabIndex, "target_id", targetID, "terminal_id", item.Info.ID)}
		}
		snap, err := m.client.Snapshot(ctx, item.Info.ID, 0, 200)
		if err != nil {
			return errMsg{m.wrapClientError("snapshot terminal", err, "tab_index", tabIndex, "target_id", targetID, "terminal_id", item.Info.ID)}
		}
		paneID := m.nextPaneID()
		return paneCreatedMsg{
			tabIndex: tabIndex,
			targetID: targetID,
			split:    split,
			pane: &Pane{
				ID:       paneID,
				Title:    paneTitleForTerminal(item.Info),
				Viewport: viewportWithTerminalInfo(m.newViewport(item.Info.ID, attached.Channel, snap), item.Info),
			},
		}
	}
}

func (m *Model) attachTerminalToFloatingPaneCmd(tabIndex int, item terminalPickerItem) tea.Cmd {
	return func() tea.Msg {
		m.logger.Debug("attaching terminal to floating pane", "tab_index", tabIndex, "terminal_id", item.Info.ID)
		ctx, cancel := m.requestContext()
		defer cancel()
		attached, err := m.client.Attach(ctx, item.Info.ID, "collaborator")
		if err != nil {
			return errMsg{m.wrapClientError("attach terminal", err, "tab_index", tabIndex, "terminal_id", item.Info.ID)}
		}
		snap, err := m.client.Snapshot(ctx, item.Info.ID, 0, 200)
		if err != nil {
			return errMsg{m.wrapClientError("snapshot terminal", err, "tab_index", tabIndex, "terminal_id", item.Info.ID)}
		}
		paneID := m.nextPaneID()
		view := viewportWithTerminalInfo(m.newViewport(item.Info.ID, attached.Channel, snap), item.Info)
		view.Mode = ViewportModeFixed
		return paneCreatedMsg{
			tabIndex: tabIndex,
			floating: true,
			pane: &Pane{
				ID:       paneID,
				Title:    paneTitleForTerminal(item.Info),
				Viewport: view,
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

func viewportWithTerminalInfo(viewport *Viewport, info protocol.TerminalInfo) *Viewport {
	if viewport == nil {
		return nil
	}
	viewport.Name = info.Name
	viewport.Command = append([]string(nil), info.Command...)
	if len(info.Tags) > 0 {
		viewport.Tags = make(map[string]string, len(info.Tags))
		for key, value := range info.Tags {
			viewport.Tags[key] = value
		}
	} else {
		viewport.Tags = nil
	}
	viewport.TerminalState = info.State
	viewport.ExitCode = info.ExitCode
	return viewport
}

func (m *Model) renderTerminalPicker() string {
	tabBar := m.renderTabBar()
	status := m.renderStatus()
	contentHeight := max(1, m.height-2)
	canvas := newCanvas(m.width, contentHeight)
	title := "Terminal Picker"
	if m.terminalPicker != nil && m.terminalPicker.Title != "" {
		title = m.terminalPicker.Title
	}
	contentWidth := max(1, m.width-4)
	canvas.drawText(Rect{X: 2, Y: 1, W: contentWidth, H: 1}, []string{terminalPickerTitleStyle.Render(title)})
	query := ""
	if m.terminalPicker != nil {
		query = m.terminalPicker.Query
	}
	canvas.drawText(Rect{X: 2, Y: 3, W: contentWidth, H: 1}, []string{terminalPickerQueryStyle.Render(" query: " + query + "_" + " ")})

	lines := make([]string, 0, max(0, contentHeight-7))
	if m.terminalPicker != nil {
		if m.terminalPicker.RenderWidth != contentWidth {
			m.terminalPicker.RenderWidth = contentWidth
			for i := range m.terminalPicker.Items {
				m.terminalPicker.Items[i].lineWidth = 0
			}
		}
		for i := range m.terminalPicker.Filtered {
			item := &m.terminalPicker.Filtered[i]
			lines = append(lines, item.line(contentWidth, i == m.terminalPicker.Selected))
		}
	}
	if len(lines) == 0 {
		lines = []string{"  no terminals match"}
	}
	canvas.drawText(Rect{X: 2, Y: 5, W: contentWidth, H: max(1, contentHeight-7)}, lines)
	canvas.drawText(Rect{X: 2, Y: max(6, contentHeight-2), W: contentWidth, H: 1}, []string{
		trimToWidth(coalesce(m.terminalPicker.Footer, "[Enter] attach  [Tab] split+attach  [Ctrl-k] kill  [Esc] close"), contentWidth),
	})

	body := terminalPickerBodyStyle.Render(forceHeight(canvas.String(), contentHeight))
	return strings.Join([]string{tabBar, body, status}, "\n")
}

func terminalInfoStateLabel(info protocol.TerminalInfo) string {
	state := info.State
	if state == "" {
		state = "running"
	}
	if state == "exited" && info.ExitCode != nil {
		return fmt.Sprintf("exited code=%d", *info.ExitCode)
	}
	return state
}

func formatTerminalAge(createdAt, now time.Time) string {
	if createdAt.IsZero() {
		return "-"
	}
	if now.Before(createdAt) {
		now = createdAt
	}
	age := now.Sub(createdAt)
	switch {
	case age >= time.Hour:
		return fmt.Sprintf("%dh", int(age/time.Hour))
	case age >= time.Minute:
		return fmt.Sprintf("%dm", int(age/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(age/time.Second))
	}
}

func formatTerminalTags(tags map[string]string) string {
	if len(tags) == 0 {
		return "tags:-"
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+tags[key])
	}
	return strings.Join(parts, " ")
}

func decodeInputRune(data []byte) (rune, int) {
	r, size := utf8.DecodeRune(data)
	if r == utf8.RuneError && size == 1 && !utf8.FullRune(data) {
		return 0, 0
	}
	return r, size
}
