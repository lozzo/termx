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
	ViewID         string             `json:"viewId"`
	SurfaceID      string             `json:"surfaceId,omitempty"`
	TerminalID     string             `json:"terminalId"`
	Channel        uint16             `json:"channel,omitempty"`
	Layout         TerminalViewLayout `json:"layout,omitempty"`
	ResizeRole     string             `json:"resizeRole,omitempty"`
	DesiredCols    int                `json:"desiredCols,omitempty"`
	DesiredRows    int                `json:"desiredRows,omitempty"`
	RequestSeq     uint64             `json:"requestSeq,omitempty"`
	LastError      string             `json:"lastError,omitempty"`
	PaneID         string             `json:"paneId,omitempty"`
	FloatingID     string             `json:"floatingId,omitempty"`
	Attached       bool               `json:"attached"`
	CanResize      bool               `json:"canResize,omitempty"`
	SizeLocked     bool               `json:"sizeLocked,omitempty"`
	ControlReason  string             `json:"controlReason,omitempty"`
	OwnerSurfaceID string             `json:"ownerSurfaceId,omitempty"`
	OwnerViewID    string             `json:"ownerViewId,omitempty"`
	ResizeEpoch    uint64             `json:"resizeEpoch,omitempty"`
	ResizePending  bool               `json:"resizePending,omitempty"`
	AttachPending  bool               `json:"attachPending,omitempty"`
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
	return TerminalViewBinding{ViewID: viewID, SurfaceID: surfaceID, TerminalID: terminalID, Channel: channel, ResizeRole: resizeRole, DesiredCols: cols, DesiredRows: rows, PaneID: paneID, Attached: terminalID != "" && channel != 0, CanResize: canResize}
}

func NewFloatingTerminalView(floatingID string, paneID string, terminalID string, channel uint16, cols int, rows int, resizeRole string, surfaceID string, viewID string, canResize bool) TerminalViewBinding {
	if viewID == "" {
		viewID = TerminalFloatingViewID(floatingID)
	}
	if surfaceID == "" {
		surfaceID = "termx-tui-v3"
	}
	resizeRole = normalizeTerminalResizeRole(resizeRole)
	return TerminalViewBinding{ViewID: viewID, SurfaceID: surfaceID, TerminalID: terminalID, Channel: channel, ResizeRole: resizeRole, DesiredCols: cols, DesiredRows: rows, FloatingID: floatingID, PaneID: paneID, Attached: terminalID != "" && channel != 0, CanResize: canResize}
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
	binding.Attached = binding.TerminalID != "" && binding.Channel != 0
	binding = binding.applyTerminalSizeLockProjection(binding.SizeLocked || store.terminalSizeLocked(binding.TerminalID))
	store.Views = cloneTerminalViewBindings(store.Views)
	store.PaneViews = cloneTerminalViewIDs(store.PaneViews)
	store.FloatingViews = cloneTerminalViewIDs(store.FloatingViews)
	if binding.ResizeRole == TerminalResizeRoleOwner {
		_, hadDifferentOwner := store.ownerIdentityBinding(binding.TerminalID)
		if owner, ok := store.ownerIdentityBinding(binding.TerminalID); ok && owner.ViewID == binding.ViewID {
			hadDifferentOwner = false
		}
		if existing, ok := store.Views[binding.ViewID]; ok && existing.TerminalID == binding.TerminalID && !existing.HasResizeOwner() {
			// 中文说明：attach result 可能把 follower 投影成 owner；这同样需要下一帧主动校验 PTY size。
			binding.ResizePending = true
		}
		if hadDifferentOwner {
			// 中文说明：新建 view 直接以 owner 写入时也属于 owner transfer，不能等后续输入输出才同步尺寸。
			binding.ResizePending = true
		}
	}
	if binding.ResizeRole == TerminalResizeRoleOwner {
		store.demoteResizeOwnersLocked(binding.TerminalID, binding.ViewID)
	}
	store.Views[binding.ViewID] = binding
	if binding.PaneID != "" {
		store.PaneViews[binding.PaneID] = binding.ViewID
	}
	if binding.FloatingID != "" {
		store.FloatingViews[binding.FloatingID] = binding.ViewID
	}
	return store
}

func (store TerminalViewStore) demoteResizeOwnersLocked(terminalID string, exceptViewID string) {
	if terminalID == "" {
		return
	}
	for candidateID, candidate := range store.Views {
		if candidateID == exceptViewID || candidate.TerminalID != terminalID || candidate.ResizeRole != TerminalResizeRoleOwner {
			continue
		}
		candidate.ResizeRole = TerminalResizeRoleFollower
		candidate.CanResize = false
		candidate.ResizePending = false
		store.Views[candidateID] = candidate
	}
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
	binding, ok := store.Views[viewID]
	if !ok {
		return store
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	store.PaneViews = cloneTerminalViewIDs(store.PaneViews)
	delete(store.Views, viewID)
	delete(store.PaneViews, paneID)
	store.promoteReplacementOwnerLocked(binding.TerminalID)
	return store
}

func (store TerminalViewStore) DetachFloating(floatingID string) TerminalViewStore {
	viewID := store.FloatingViews[floatingID]
	if viewID == "" {
		return store
	}
	binding, ok := store.Views[viewID]
	if !ok {
		return store
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	store.FloatingViews = cloneTerminalViewIDs(store.FloatingViews)
	delete(store.Views, viewID)
	delete(store.FloatingViews, floatingID)
	store.promoteReplacementOwnerLocked(binding.TerminalID)
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

func (store TerminalViewStore) PaneViewID(paneID string) string {
	if paneID == "" {
		return ""
	}
	if viewID := store.PaneViews[paneID]; viewID != "" {
		return viewID
	}
	return TerminalPaneViewID(paneID)
}

func (store TerminalViewStore) FloatingBinding(floatingID string) (TerminalViewBinding, bool) {
	viewID := store.FloatingViews[floatingID]
	if viewID == "" {
		return TerminalViewBinding{}, false
	}
	binding, ok := store.Views[viewID]
	return binding, ok
}

func (store TerminalViewStore) FloatingViewID(floatingID string) string {
	if floatingID == "" {
		return ""
	}
	if viewID := store.FloatingViews[floatingID]; viewID != "" {
		return viewID
	}
	return TerminalFloatingViewID(floatingID)
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

func (store TerminalViewStore) MarkAttachPending(binding TerminalViewBinding) TerminalViewStore {
	if binding.TerminalID == "" || binding.ViewID == "" {
		return store
	}
	existing, hasExisting := store.Views[binding.ViewID]
	if hasExisting {
		binding.Layout = existing.Layout
		if binding.PaneID == "" {
			binding.PaneID = existing.PaneID
		}
		if binding.FloatingID == "" {
			binding.FloatingID = existing.FloatingID
		}
		if binding.DesiredCols <= 0 {
			binding.DesiredCols = existing.DesiredCols
		}
		if binding.DesiredRows <= 0 {
			binding.DesiredRows = existing.DesiredRows
		}
		if binding.ResizeRole == "" {
			binding.ResizeRole = existing.ResizeRole
		}
		if binding.SurfaceID == "" {
			binding.SurfaceID = existing.SurfaceID
		}
		binding.SizeLocked = existing.SizeLocked
	}
	binding.Channel = 0
	binding.Attached = false
	binding.AttachPending = true
	binding.CanResize = false
	binding.LastError = ""
	if binding.PaneID != "" {
		return store.BindPane(binding)
	}
	if binding.FloatingID != "" {
		return store.BindFloating(binding)
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	store.Views[binding.ViewID] = binding
	return store
}

func (store TerminalViewStore) ClearAttachPending(viewID string, err string) TerminalViewStore {
	if viewID == "" {
		return store
	}
	binding, ok := store.Views[viewID]
	if !ok || !binding.AttachPending {
		return store
	}
	binding.AttachPending = false
	binding.LastError = err
	store.Views = cloneTerminalViewBindings(store.Views)
	store.Views[viewID] = binding
	return store
}

func (store TerminalViewStore) MarkTerminalReattaching(terminalID string) TerminalViewStore {
	if terminalID == "" {
		return store
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	for viewID, binding := range store.Views {
		if binding.TerminalID != terminalID {
			continue
		}
		// restart 后旧 channel 已经属于退出前的 attachment；保留 view 绑定意图，
		// 但必须让输入路径重新 attach 当前 view 后再发送。
		binding.Channel = 0
		binding.Attached = false
		binding.AttachPending = false
		binding.LastError = ""
		store.Views[viewID] = binding
	}
	return store
}

func (store TerminalViewStore) OwnerBinding(terminalID string) (TerminalViewBinding, bool) {
	for _, binding := range store.Views {
		if binding.TerminalID == terminalID && binding.HasAuthoritativeResizeOwner() {
			return binding, true
		}
	}
	return TerminalViewBinding{}, false
}

func (store TerminalViewStore) ownerIdentityBinding(terminalID string) (TerminalViewBinding, bool) {
	for _, binding := range store.Views {
		if binding.TerminalID == terminalID && binding.HasResizeOwner() {
			return binding, true
		}
	}
	return TerminalViewBinding{}, false
}

func (store TerminalViewStore) terminalSizeLocked(terminalID string) bool {
	if terminalID == "" {
		return false
	}
	for _, binding := range store.Views {
		if binding.TerminalID == terminalID && binding.SizeLocked {
			return true
		}
	}
	return false
}

func (store TerminalViewStore) promoteReplacementOwnerLocked(terminalID string) {
	if terminalID == "" {
		return
	}
	if _, ok := store.OwnerBinding(terminalID); ok {
		return
	}
	locked := store.terminalSizeLocked(terminalID)
	for viewID, binding := range store.Views {
		if binding.TerminalID != terminalID {
			continue
		}
		binding.ResizeRole = TerminalResizeRoleOwner
		// 中文说明：关闭旧 owner 后本地接任不能保留已关闭 view 的 core owner identity；
		// 接任 view 会通过 pending ensure_resize 把 daemon 全局 owner 转到自己。
		binding.OwnerSurfaceID = ""
		binding.OwnerViewID = ""
		binding.ControlReason = ""
		binding = binding.applyTerminalSizeLockProjection(locked)
		if !locked {
			binding.CanResize = true
		}
		// owner 删除后的接任 view 不能沿用旧的 desired size；
		// 否则 close/unzoom 这类布局回弹会被误判成“尺寸未变”，漏发真实 PTY resize。
		binding.DesiredCols = 0
		binding.DesiredRows = 0
		// 中文说明：被动接任 owner 后必须至少走一次 ensure_resize，让 core 同步 owner 尺寸语义。
		binding.ResizePending = true
		store.Views[viewID] = binding
		store.demoteResizeOwnersLocked(terminalID, viewID)
		return
	}
}

func (store TerminalViewStore) TransferResizeOwner(viewID string) TerminalViewStore {
	target, ok := store.Views[viewID]
	if !ok || target.TerminalID == "" {
		return store
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	locked := store.terminalSizeLocked(target.TerminalID)
	for candidateID, binding := range store.Views {
		if binding.TerminalID != target.TerminalID {
			continue
		}
		if candidateID == viewID {
			becameOwner := !binding.HasResizeOwner()
			binding.ResizeRole = TerminalResizeRoleOwner
			// 中文说明：本地抢 owner 后不能继续保留旧 core owner identity；
			// 否则后续 layout pass 仍会把当前 view 判成 follower，再把 resize 打回旧 owner。
			binding.OwnerSurfaceID = ""
			binding.OwnerViewID = ""
			binding.ControlReason = ""
			binding = binding.applyTerminalSizeLockProjection(locked)
			if !locked {
				binding.CanResize = true
			}
			if becameOwner {
				// 中文说明：主动抢 owner 是 attachment ownership 变化，尺寸相同也要校验一次。
				binding.ResizePending = true
			}
		} else if binding.ResizeRole == TerminalResizeRoleOwner {
			binding.ResizeRole = TerminalResizeRoleFollower
			binding.CanResize = false
			binding.ResizePending = false
			binding = binding.applyTerminalSizeLockProjection(locked)
		} else {
			binding = binding.applyTerminalSizeLockProjection(locked)
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
	if binding.SizeLocked {
		decision.Reason = "size-locked"
		return store, decision
	}
	if binding.ResizeRole != TerminalResizeRoleOwner || !binding.CanResize {
		decision.Reason = "not-owner"
		return store, decision
	}
	decision.Allowed = true
	if binding.DesiredCols == cols && binding.DesiredRows == rows && !binding.ResizePending {
		decision.Reason = "unchanged"
		return store, decision
	}
	binding.DesiredCols = cols
	binding.DesiredRows = rows
	binding.ResizePending = false
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
	binding.ResizePending = false
	binding.LastError = lastError
	store.Views = cloneTerminalViewBindings(store.Views)
	store.Views[viewID] = binding
	return store, true
}

type TerminalResizeControlProjection struct {
	CanResize      bool
	SizeLocked     bool
	ControlReason  string
	OwnerSurfaceID string
	OwnerViewID    string
	ResizeEpoch    uint64
	ResizeRole     string
	SurfaceID      string
	ViewID         string
}

func (store TerminalViewStore) ApplyResizeControl(viewID string, projection TerminalResizeControlProjection) (TerminalViewStore, bool) {
	binding, ok := store.Views[viewID]
	if !ok {
		return store, false
	}
	if binding.ResizeRole == TerminalResizeRoleOwner &&
		binding.ResizePending &&
		projection.ResizeRole == TerminalResizeRoleFollower &&
		projection.OwnerViewID != "" &&
		projection.OwnerViewID != binding.ViewID {
		// 中文说明：本地刚显式抢 owner 后，旧 attach/resize projection 可能仍带着前任 owner；
		// 这类 follower 回包不能把 pending owner 立刻降回 follower。
		return store, false
	}
	binding.CanResize = projection.CanResize
	binding.SizeLocked = projection.SizeLocked
	binding.ControlReason = projection.ControlReason
	binding.OwnerSurfaceID = projection.OwnerSurfaceID
	binding.OwnerViewID = projection.OwnerViewID
	binding.ResizeEpoch = projection.ResizeEpoch
	if projection.ResizeRole != "" {
		wasOwner := binding.HasResizeOwner()
		binding.ResizeRole = normalizeTerminalResizeRole(projection.ResizeRole)
		if !wasOwner && binding.HasResizeOwner() {
			// 中文说明：core 投影把当前 view 提升为 owner 后，下一次 layout pass 必须主动校验尺寸。
			binding.ResizePending = true
		}
	}
	if projection.SurfaceID != "" {
		binding.SurfaceID = projection.SurfaceID
	}
	if projection.ViewID != "" {
		binding.ViewID = projection.ViewID
	}
	binding = binding.applyTerminalSizeLockProjection(binding.SizeLocked)
	store.Views = cloneTerminalViewBindings(store.Views)
	if binding.HasResizeOwner() {
		store.demoteResizeOwnersLocked(binding.TerminalID, viewID)
	}
	store.Views[viewID] = binding
	return store, true
}

func (store TerminalViewStore) ApplyTerminalResizeControl(terminalID string, projection TerminalResizeControlProjection) TerminalViewStore {
	if terminalID == "" {
		return store
	}
	if store.terminalResizeControlProjectionStale(terminalID, projection) {
		return store
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	for viewID, binding := range store.Views {
		if binding.TerminalID != terminalID {
			continue
		}
		viewProjection := projection
		viewProjection.SurfaceID = binding.SurfaceID
		viewProjection.ViewID = binding.ViewID
		if projection.OwnerViewID == binding.ViewID && projection.OwnerSurfaceID == binding.SurfaceID {
			viewProjection.ResizeRole = TerminalResizeRoleOwner
			viewProjection.CanResize = !projection.SizeLocked
		} else {
			viewProjection.ResizeRole = TerminalResizeRoleFollower
			viewProjection.CanResize = false
		}
		binding.CanResize = viewProjection.CanResize
		binding.SizeLocked = viewProjection.SizeLocked
		binding.ControlReason = viewProjection.ControlReason
		binding.OwnerSurfaceID = viewProjection.OwnerSurfaceID
		binding.OwnerViewID = viewProjection.OwnerViewID
		binding.ResizeEpoch = viewProjection.ResizeEpoch
		wasOwner := binding.HasResizeOwner()
		binding.ResizeRole = normalizeTerminalResizeRole(viewProjection.ResizeRole)
		if !binding.HasResizeOwner() {
			binding.ResizePending = false
		} else if !wasOwner {
			// 中文说明：外部投影把当前 view 提升为 owner 后，下一轮布局要校验一次 PTY 尺寸。
			binding.ResizePending = true
		}
		binding = binding.applyTerminalSizeLockProjection(binding.SizeLocked)
		store.Views[viewID] = binding
	}
	return store
}

func (store TerminalViewStore) terminalResizeControlProjectionStale(terminalID string, projection TerminalResizeControlProjection) bool {
	var maxEpoch uint64
	var localOwner TerminalViewBinding
	var hasLocalOwner bool
	var projectedLocalOwner TerminalViewBinding
	var hasProjectedLocalOwner bool
	for _, binding := range store.Views {
		if binding.TerminalID != terminalID {
			continue
		}
		if binding.HasResizeOwner() {
			localOwner = binding
			hasLocalOwner = true
		}
		if projection.OwnerViewID != "" && projection.OwnerSurfaceID != "" &&
			binding.ViewID == projection.OwnerViewID && binding.SurfaceID == projection.OwnerSurfaceID {
			projectedLocalOwner = binding
			hasProjectedLocalOwner = true
		}
		if binding.ResizeEpoch > maxEpoch {
			maxEpoch = binding.ResizeEpoch
		}
		if binding.ResizeRole == TerminalResizeRoleOwner && binding.ResizePending && projection.OwnerViewID != "" {
			if projection.OwnerViewID != binding.ViewID || projection.OwnerSurfaceID != binding.SurfaceID {
				return true
			}
		}
	}
	if hasLocalOwner && hasProjectedLocalOwner && !projectedLocalOwner.HasResizeOwner() &&
		(localOwner.ViewID != projectedLocalOwner.ViewID || localOwner.SurfaceID != projectedLocalOwner.SurfaceID) {
		// 中文说明：本 TUI 刚把 owner 转到另一个 view 后，旧 owner 的异步 resize/metadata
		// 可能带着更晚的 epoch 回来；它不能把本地 follower 再提升回 owner。
		return true
	}
	return projection.ResizeEpoch != 0 && maxEpoch > projection.ResizeEpoch
}

func (store TerminalViewStore) ApplyTerminalSizeLock(terminalID string, locked bool) TerminalViewStore {
	if terminalID == "" {
		return store
	}
	store.Views = cloneTerminalViewBindings(store.Views)
	for viewID, binding := range store.Views {
		if binding.TerminalID != terminalID {
			continue
		}
		binding = binding.applyTerminalSizeLockProjection(locked)
		store.Views[viewID] = binding
	}
	return store
}

func (binding TerminalViewBinding) applyTerminalSizeLockProjection(locked bool) TerminalViewBinding {
	binding.SizeLocked = locked
	if locked {
		// 中文说明：Size lock 是 terminal 级最高优先级；owner 身份可以存在，但不能恢复 PTY resize 权限。
		if binding.HasResizeOwner() {
			binding.ResizeRole = TerminalResizeRoleOwner
		}
		binding.CanResize = false
		binding.ControlReason = "size_locked"
		return binding
	}
	if binding.ControlReason == "size_locked" {
		binding.ControlReason = ""
		if binding.HasResizeOwner() {
			binding.CanResize = true
		}
	}
	return binding
}

func (binding TerminalViewBinding) hasAuthoritativeResizeOwner() bool {
	return binding.HasAuthoritativeResizeOwner()
}

func (binding TerminalViewBinding) HasAuthoritativeResizeOwner() bool {
	if !binding.CanResize {
		return false
	}
	return binding.HasResizeOwner()
}

func (binding TerminalViewBinding) HasProjectedResizeOwner() bool {
	if binding.ResizeRole != TerminalResizeRoleOwner {
		return false
	}
	if binding.OwnerViewID == "" || binding.OwnerSurfaceID == "" {
		return false
	}
	return binding.OwnerViewID == binding.ViewID && binding.OwnerSurfaceID == binding.SurfaceID
}

func (binding TerminalViewBinding) HasResizeOwner() bool {
	if binding.ResizeRole != TerminalResizeRoleOwner {
		return false
	}
	if binding.OwnerViewID != "" {
		if binding.OwnerViewID != binding.ViewID {
			return false
		}
		// 中文说明：不同 TUI 实例可以有相同 logical ViewID；core owner 必须同时匹配 surface。
		return binding.OwnerSurfaceID != "" && binding.OwnerSurfaceID == binding.SurfaceID
	}
	return true
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
