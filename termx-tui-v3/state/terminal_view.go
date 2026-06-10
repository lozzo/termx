package state

const (
	TerminalResizeRoleOwner    = "owner"
	TerminalResizeRoleFollower = "follower"
	TerminalResizeRoleObserver = "observer"
)

// TerminalViewStore 是 pane/floating 到 core-v2 attachment 的 reducer-owned 连接视图状态。
// Terminal 本身仍是共享 process/lifecycle/history truth，view 只保存 UI 连接身份和请求状态。
type TerminalViewStore struct {
	Views         map[string]TerminalViewBinding
	PaneViews     map[string]string
	FloatingViews map[string]string
}

type TerminalViewBinding struct {
	ViewID      string
	SurfaceID   string
	TerminalID  string
	Channel     uint16
	ResizeRole  string
	DesiredCols int
	DesiredRows int
	RequestSeq  uint64
	LastError   string
	PaneID      string
	FloatingID  string
	Attached    bool
	CanResize   bool
}

func NewPaneTerminalView(paneID string, terminalID string, channel uint16, cols int, rows int, resizeRole string, surfaceID string, viewID string, canResize bool) TerminalViewBinding {
	if viewID == "" {
		viewID = TerminalPaneViewID(paneID)
	}
	if surfaceID == "" {
		surfaceID = "termx-tui-v3"
	}
	return TerminalViewBinding{ViewID: viewID, SurfaceID: surfaceID, TerminalID: terminalID, Channel: channel, ResizeRole: normalizeTerminalResizeRole(resizeRole), DesiredCols: cols, DesiredRows: rows, PaneID: paneID, Attached: terminalID != "", CanResize: canResize}
}

func NewFloatingTerminalView(floatingID string, paneID string, terminalID string, channel uint16, cols int, rows int, resizeRole string, surfaceID string, viewID string, canResize bool) TerminalViewBinding {
	if viewID == "" {
		viewID = TerminalFloatingViewID(floatingID)
	}
	if surfaceID == "" {
		surfaceID = "termx-tui-v3"
	}
	return TerminalViewBinding{ViewID: viewID, SurfaceID: surfaceID, TerminalID: terminalID, Channel: channel, ResizeRole: normalizeTerminalResizeRole(resizeRole), DesiredCols: cols, DesiredRows: rows, FloatingID: floatingID, PaneID: paneID, Attached: terminalID != "", CanResize: canResize}
}

func TerminalPaneViewID(paneID string) string {
	if paneID == "" {
		paneID = DefaultPaneID
	}
	return "pane:" + paneID
}

func TerminalFloatingViewID(floatingID string) string {
	if floatingID == "" {
		floatingID = "floating"
	}
	return "floating:" + floatingID
}

func (store TerminalViewStore) BindPane(binding TerminalViewBinding) TerminalViewStore {
	if binding.PaneID == "" || binding.TerminalID == "" {
		return store
	}
	if binding.ViewID == "" {
		binding.ViewID = TerminalPaneViewID(binding.PaneID)
	}
	return store.bind(binding)
}

func (store TerminalViewStore) BindFloating(binding TerminalViewBinding) TerminalViewStore {
	if binding.FloatingID == "" || binding.TerminalID == "" {
		return store
	}
	if binding.ViewID == "" {
		binding.ViewID = TerminalFloatingViewID(binding.FloatingID)
	}
	return store.bind(binding)
}

func (store TerminalViewStore) bind(binding TerminalViewBinding) TerminalViewStore {
	binding.ResizeRole = normalizeTerminalResizeRole(binding.ResizeRole)
	binding.Attached = true
	store.Views = cloneTerminalViewBindings(store.Views)
	store.PaneViews = cloneTerminalViewIDs(store.PaneViews)
	store.FloatingViews = cloneTerminalViewIDs(store.FloatingViews)
	store.Views[binding.ViewID] = binding
	if binding.PaneID != "" {
		store.PaneViews[binding.PaneID] = binding.ViewID
	}
	if binding.FloatingID != "" {
		store.FloatingViews[binding.FloatingID] = binding.ViewID
	}
	return store
}

func (store TerminalViewStore) DetachPane(paneID string) TerminalViewStore {
	viewID := store.PaneViews[paneID]
	if viewID == "" {
		return store
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	store.PaneViews = cloneTerminalViewIDs(store.PaneViews)
	delete(store.Views, viewID)
	delete(store.PaneViews, paneID)
	return store
}

func (store TerminalViewStore) DetachFloating(floatingID string) TerminalViewStore {
	viewID := store.FloatingViews[floatingID]
	if viewID == "" {
		return store
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	store.FloatingViews = cloneTerminalViewIDs(store.FloatingViews)
	delete(store.Views, viewID)
	delete(store.FloatingViews, floatingID)
	return store
}

func (store TerminalViewStore) RemoveTerminal(terminalID string) TerminalViewStore {
	if terminalID == "" {
		return store
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	store.PaneViews = cloneTerminalViewIDs(store.PaneViews)
	store.FloatingViews = cloneTerminalViewIDs(store.FloatingViews)
	for viewID, binding := range store.Views {
		if binding.TerminalID != terminalID {
			continue
		}
		delete(store.Views, viewID)
		if binding.PaneID != "" {
			delete(store.PaneViews, binding.PaneID)
		}
		if binding.FloatingID != "" {
			delete(store.FloatingViews, binding.FloatingID)
		}
	}
	return store
}

func (store TerminalViewStore) PaneBinding(paneID string) (TerminalViewBinding, bool) {
	viewID := store.PaneViews[paneID]
	if viewID == "" {
		return TerminalViewBinding{}, false
	}
	binding, ok := store.Views[viewID]
	return binding, ok
}

func (store TerminalViewStore) FloatingBinding(floatingID string) (TerminalViewBinding, bool) {
	viewID := store.FloatingViews[floatingID]
	if viewID == "" {
		return TerminalViewBinding{}, false
	}
	binding, ok := store.Views[viewID]
	return binding, ok
}

func (store TerminalViewStore) BindingsForTerminal(terminalID string) []TerminalViewBinding {
	if terminalID == "" {
		return nil
	}
	bindings := make([]TerminalViewBinding, 0)
	for _, binding := range store.Views {
		if binding.TerminalID == terminalID {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

func normalizeTerminalResizeRole(role string) string {
	switch role {
	case TerminalResizeRoleFollower, TerminalResizeRoleObserver:
		return role
	default:
		return TerminalResizeRoleOwner
	}
}

func cloneTerminalViewBindings(values map[string]TerminalViewBinding) map[string]TerminalViewBinding {
	cloned := make(map[string]TerminalViewBinding, len(values)+1)
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneTerminalViewIDs(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values)+1)
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
