package state

const (
	TerminalResizeRoleOwner    = "owner"
	TerminalResizeRoleFollower = "follower"
	TerminalResizeRoleObserver = "observer"

	TerminalViewAlignStart  = "start"
	TerminalViewAlignCenter = "center"
	TerminalViewAlignEnd    = "end"
	TerminalViewAlignBase   = "base"

	TerminalViewLayoutAuto   = "auto"
	TerminalViewLayoutFit    = "fit"
	TerminalViewLayoutCenter = "center"
)

// TerminalViewStore 是 pane/floating 到 core-v2 attachment 的 reducer-owned 连接视图状态。
// Terminal 本身仍是共享 process/lifecycle/history truth，view 只保存 UI 连接身份和请求状态。
type TerminalViewStore struct {
	Views         map[string]TerminalViewBinding
	PaneViews     map[string]string
	FloatingViews map[string]string
}

type TerminalViewBinding struct {
	ViewID      string             `json:"viewId"`
	SurfaceID   string             `json:"surfaceId,omitempty"`
	TerminalID  string             `json:"terminalId"`
	Channel     uint16             `json:"channel,omitempty"`
	Layout      TerminalViewLayout `json:"layout,omitempty"`
	ResizeRole  string             `json:"resizeRole,omitempty"`
	DesiredCols int                `json:"desiredCols,omitempty"`
	DesiredRows int                `json:"desiredRows,omitempty"`
	RequestSeq  uint64             `json:"requestSeq,omitempty"`
	LastError   string             `json:"lastError,omitempty"`
	PaneID      string             `json:"paneId,omitempty"`
	FloatingID  string             `json:"floatingId,omitempty"`
	Attached    bool               `json:"attached"`
	CanResize   bool               `json:"canResize,omitempty"`
}

// TerminalViewLayout 是 pane/floating 的 view-local 内容布局状态。
// 它不改变共享 terminal process、history truth 或 PTY size ownership。
type TerminalViewLayout struct {
	SizeLocked bool   `json:"sizeLocked,omitempty"`
	Mode       string `json:"mode,omitempty"`
	PanX       int    `json:"panX,omitempty"`
	PanY       int    `json:"panY,omitempty"`
	AlignX     string `json:"alignX,omitempty"`
	AlignY     string `json:"alignY,omitempty"`
}

type TerminalViewLayoutCommand struct {
	Action string
	Mode   string
	AlignX string
	AlignY string
	DeltaX int
	DeltaY int
}

type TerminalViewResizeDecision struct {
	Binding TerminalViewBinding
	Allowed bool
	Changed bool
	Seq     uint64
	Reason  string
}

func NewPaneTerminalView(paneID string, terminalID string, channel uint16, cols int, rows int, resizeRole string, surfaceID string, viewID string, canResize bool) TerminalViewBinding {
	if viewID == "" {
		viewID = TerminalPaneViewID(paneID)
	}
	if surfaceID == "" {
		surfaceID = "termx-tui-v3"
	}
	resizeRole = normalizeTerminalResizeRole(resizeRole)
	return TerminalViewBinding{ViewID: viewID, SurfaceID: surfaceID, TerminalID: terminalID, Channel: channel, ResizeRole: resizeRole, DesiredCols: cols, DesiredRows: rows, PaneID: paneID, Attached: terminalID != "", CanResize: canResize || resizeRole == TerminalResizeRoleOwner}
}

func NewFloatingTerminalView(floatingID string, paneID string, terminalID string, channel uint16, cols int, rows int, resizeRole string, surfaceID string, viewID string, canResize bool) TerminalViewBinding {
	if viewID == "" {
		viewID = TerminalFloatingViewID(floatingID)
	}
	if surfaceID == "" {
		surfaceID = "termx-tui-v3"
	}
	resizeRole = normalizeTerminalResizeRole(resizeRole)
	return TerminalViewBinding{ViewID: viewID, SurfaceID: surfaceID, TerminalID: terminalID, Channel: channel, ResizeRole: resizeRole, DesiredCols: cols, DesiredRows: rows, FloatingID: floatingID, PaneID: paneID, Attached: terminalID != "", CanResize: canResize || resizeRole == TerminalResizeRoleOwner}
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
	binding.Layout = binding.Layout.Normalize()
	if binding.ResizeRole == TerminalResizeRoleOwner {
		binding.CanResize = true
	}
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

func (store TerminalViewStore) ApplyPaneLayoutCommand(paneID string, command TerminalViewLayoutCommand) (TerminalViewStore, TerminalViewBinding, bool) {
	return store.ApplyViewLayoutCommand(store.PaneViews[paneID], command)
}

func (store TerminalViewStore) ApplyFloatingLayoutCommand(floatingID string, command TerminalViewLayoutCommand) (TerminalViewStore, TerminalViewBinding, bool) {
	return store.ApplyViewLayoutCommand(store.FloatingViews[floatingID], command)
}

func (store TerminalViewStore) ApplyViewLayoutCommand(viewID string, command TerminalViewLayoutCommand) (TerminalViewStore, TerminalViewBinding, bool) {
	binding, ok := store.Views[viewID]
	if !ok || binding.TerminalID == "" {
		return store, TerminalViewBinding{}, false
	}
	next := binding
	next.Layout = next.Layout.Apply(command)
	store.Views = cloneTerminalViewBindings(store.Views)
	store.Views[viewID] = next
	return store, next, true
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

func (store TerminalViewStore) Bindings() []TerminalViewBinding {
	bindings := make([]TerminalViewBinding, 0, len(store.Views))
	for _, binding := range store.Views {
		bindings = append(bindings, binding)
	}
	return bindings
}

func (store TerminalViewStore) OwnerBinding(terminalID string) (TerminalViewBinding, bool) {
	for _, binding := range store.Views {
		if binding.TerminalID == terminalID && binding.ResizeRole == TerminalResizeRoleOwner && binding.CanResize {
			return binding, true
		}
	}
	return TerminalViewBinding{}, false
}

func (store TerminalViewStore) TransferResizeOwner(viewID string) TerminalViewStore {
	target, ok := store.Views[viewID]
	if !ok || target.TerminalID == "" {
		return store
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	for candidateID, binding := range store.Views {
		if binding.TerminalID != target.TerminalID {
			continue
		}
		if candidateID == viewID {
			binding.ResizeRole = TerminalResizeRoleOwner
			binding.CanResize = true
		} else if binding.ResizeRole == TerminalResizeRoleOwner {
			binding.ResizeRole = TerminalResizeRoleFollower
			binding.CanResize = false
		}
		store.Views[candidateID] = binding
	}
	return store
}

func (store TerminalViewStore) TransferPaneResizeOwner(paneID string) TerminalViewStore {
	return store.TransferResizeOwner(store.PaneViews[paneID])
}

func (store TerminalViewStore) RequestPaneResize(paneID string, cols int, rows int) (TerminalViewStore, TerminalViewResizeDecision) {
	viewID := store.PaneViews[paneID]
	return store.RequestViewResize(viewID, cols, rows)
}

func (store TerminalViewStore) RequestViewResize(viewID string, cols int, rows int) (TerminalViewStore, TerminalViewResizeDecision) {
	binding, ok := store.Views[viewID]
	if !ok || binding.TerminalID == "" {
		return store, TerminalViewResizeDecision{Reason: "missing-view"}
	}
	decision := TerminalViewResizeDecision{Binding: binding}
	if binding.ResizeRole != TerminalResizeRoleOwner || !binding.CanResize {
		decision.Reason = "not-owner"
		return store, decision
	}
	decision.Allowed = true
	if binding.DesiredCols == cols && binding.DesiredRows == rows {
		decision.Reason = "unchanged"
		return store, decision
	}
	binding.DesiredCols = cols
	binding.DesiredRows = rows
	binding.RequestSeq++
	store.Views = cloneTerminalViewBindings(store.Views)
	store.Views[viewID] = binding
	decision.Binding = binding
	decision.Changed = true
	decision.Seq = binding.RequestSeq
	return store, decision
}

func (store TerminalViewStore) ApplyResizeResult(viewID string, seq uint64, cols int, rows int, lastError string) (TerminalViewStore, bool) {
	binding, ok := store.Views[viewID]
	if !ok || binding.IsStaleResizeResult(seq) {
		return store, false
	}
	binding.DesiredCols = cols
	binding.DesiredRows = rows
	binding.LastError = lastError
	store.Views = cloneTerminalViewBindings(store.Views)
	store.Views[viewID] = binding
	return store, true
}

func (binding TerminalViewBinding) IsStaleResizeResult(seq uint64) bool {
	return seq != 0 && seq < binding.RequestSeq
}

func (layout TerminalViewLayout) Normalize() TerminalViewLayout {
	if layout.Mode == "" {
		layout.Mode = TerminalViewLayoutAuto
	}
	if layout.AlignX == "" {
		layout.AlignX = TerminalViewAlignStart
	}
	if layout.AlignY == "" {
		layout.AlignY = TerminalViewAlignStart
	}
	return layout
}

func (layout TerminalViewLayout) Apply(command TerminalViewLayoutCommand) TerminalViewLayout {
	layout = layout.Normalize()
	switch command.Action {
	case "toggle-lock":
		layout.SizeLocked = !layout.SizeLocked
	case "toggle-layout":
		layout.Mode = nextTerminalViewLayoutMode(layout.Mode)
	case "pan":
		layout.PanX += command.DeltaX
		layout.PanY += command.DeltaY
	case "align":
		if command.AlignX != "" {
			layout.AlignX = normalizeTerminalViewAlign(command.AlignX)
		}
		if command.AlignY != "" {
			layout.AlignY = normalizeTerminalViewAlign(command.AlignY)
		}
	case "center":
		layout.Mode = TerminalViewLayoutCenter
		layout.PanX = 0
		layout.PanY = 0
		layout.AlignX = TerminalViewAlignCenter
		layout.AlignY = TerminalViewAlignCenter
	case "reset":
		layout = TerminalViewLayout{}.Normalize()
	}
	return layout.Normalize()
}

func nextTerminalViewLayoutMode(mode string) string {
	switch mode {
	case TerminalViewLayoutAuto:
		return TerminalViewLayoutFit
	case TerminalViewLayoutFit:
		return TerminalViewLayoutCenter
	default:
		return TerminalViewLayoutAuto
	}
}

func normalizeTerminalViewAlign(align string) string {
	switch align {
	case TerminalViewAlignCenter, TerminalViewAlignEnd, TerminalViewAlignBase:
		return align
	default:
		return TerminalViewAlignStart
	}
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
