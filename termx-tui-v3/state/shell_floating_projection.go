package state

func FloatingOverviewItems(root Root) []FloatingOverviewItem {
	shell := root.Shell.EnsureDefaults()
	items := make([]FloatingOverviewItem, 0, len(shell.Floatings))
	for _, floating := range shell.Floatings {
		item := FloatingOverviewItem{
			FloatingID: floating.ID,
			Title:      floating.Title,
			PaneID:     floating.Pane.ID,
			PaneKind:   floating.Pane.Kind,
			TerminalID: pickerTerminalID(root, floating.Pane),
			Rect:       floating.Rect,
			Z:          floating.Z,
			Active:     floating.ID == shell.ActiveFloatingID,
			Collapsed:  floating.Collapsed,
		}
		if item.Title == "" {
			item.Title = floating.ID
		}
		items = append(items, item)
	}
	if len(items) > 0 {
		selected := shell.Overlay.SelectedIndex
		if selected < 0 {
			selected = 0
		}
		if selected >= len(items) {
			selected = len(items) - 1
		}
		items[selected].Selected = true
	}
	return items
}
