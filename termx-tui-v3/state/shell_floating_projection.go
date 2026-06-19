package state

func FloatingOverviewItems(root Root) []FloatingOverviewItem {
	shell := root.Shell.EnsureDefaults()
	floatings := shell.ActiveFloatings()
	activeFloatingID := shell.ActiveFloatingID()
	items := make([]FloatingOverviewItem, 0, len(floatings))
	for _, floating := range floatings {
		item := FloatingOverviewItem{
			FloatingID: floating.ID,
			Title:      floating.Title,
			PaneID:     floating.Pane.ID,
			PaneKind:   floating.Pane.Kind,
			TerminalID: pickerTerminalID(root, floating.Pane),
			Rect:       floating.Rect,
			Z:          floating.Z,
			Active:     floating.ID == activeFloatingID,
			Collapsed:  floating.Collapsed,
			FitMode:    floating.FitMode,
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
