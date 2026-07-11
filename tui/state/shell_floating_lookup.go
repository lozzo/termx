package state

func (store ShellStore) FloatingByPaneID(paneID string) (FloatingPaneState, bool) {
	if paneID == "" {
		return FloatingPaneState{}, false
	}
	store = store.ReadonlyDefaults()
	for _, tab := range store.Workspace.Tabs {
		for _, floating := range tab.Floatings {
			if floating.Pane.ID == paneID {
				return floating, true
			}
		}
	}
	return FloatingPaneState{}, false
}

func (store ShellStore) FloatingIDForPaneID(paneID string) (string, bool) {
	floating, ok := store.FloatingByPaneID(paneID)
	if !ok || floating.ID == "" {
		return "", false
	}
	return floating.ID, true
}
