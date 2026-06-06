package state

// Root 是 reducer-owned TUI-v3 state root。
type Root struct {
	Generation    uint64
	History       HistoryStore
	CopyMode      CopyModeStore
	Surface       TerminalSurfaceStore
	Session       TerminalSessionStore
	TerminalPool  TerminalPoolStore
	Viewport      ViewportStore
	Shell         ShellStore
	HostTheme     HostThemeStore
	WorkbenchSync WorkbenchSyncStore
}

// Advance 返回 generation 递增后的副本。
func (r Root) Advance() Root {
	r.Generation++
	return r
}

type WorkbenchSyncStore struct {
	Ref                WorkbenchStorageRef
	LastSavedVersion   uint64
	LastAppliedVersion uint64
	LastEventVersion   uint64
}

func (store WorkbenchSyncStore) MarkSaved(ref WorkbenchStorageRef, version uint64) WorkbenchSyncStore {
	store.Ref = ref.WithVersion(version)
	store.LastSavedVersion = version
	return store
}

func (store WorkbenchSyncStore) MarkEvent(version uint64) WorkbenchSyncStore {
	store.LastEventVersion = version
	return store
}

func (store WorkbenchSyncStore) MarkApplied(version uint64) WorkbenchSyncStore {
	store.LastAppliedVersion = version
	return store
}

func (store WorkbenchSyncStore) ShouldIgnoreEvent(eventVersion uint64) bool {
	return eventVersion != 0 && eventVersion == store.LastSavedVersion
}
