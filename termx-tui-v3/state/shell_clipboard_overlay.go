package state

func (store ShellStore) SetClipboardHistoryQuery(query string) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayClipboardHistory || !store.Overlay.Open {
		return store
	}
	store.Overlay.Query = query
	store.Overlay.SelectedIndex = 0
	return store
}

func (store ShellStore) MoveClipboardHistorySelection(delta int, itemCount int) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayClipboardHistory || !store.Overlay.Open || itemCount <= 0 || delta == 0 {
		return store
	}
	next := store.Overlay.SelectedIndex + delta
	next %= itemCount
	if next < 0 {
		next += itemCount
	}
	store.Overlay.SelectedIndex = next
	return store
}

func (store ShellStore) SetClipboardHistorySelectedIndex(index int, itemCount int) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayClipboardHistory || !store.Overlay.Open || itemCount <= 0 {
		return store
	}
	if index < 0 {
		index = 0
	}
	if index >= itemCount {
		index = itemCount - 1
	}
	store.Overlay.SelectedIndex = index
	return store
}
