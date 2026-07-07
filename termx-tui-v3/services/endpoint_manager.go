package services

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/lozzow/termx/termx-shared/connection"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// EndpointServiceBundle 是单个 daemon endpoint 对应的一组 service adapter。
// EndpointManager 按 EndpointID 找到 bundle，进入 bundle 前会剥离 EndpointID；
// bundle 内部仍是单 daemon 边界，不能持有跨 endpoint 路由真值。
type EndpointServiceBundle struct {
	EndpointID state.EndpointID
	Terminal   TerminalService
	Core       CoreClient
	Surface    TerminalSurfaceService
	LiveEvents TerminalLiveEventService
	Path       PathService
	Lifecycle  EndpointLifecycle
}

// EndpointDialer 是 endpoint manager 的 lazy transport 连接入口。
// dialer 只根据单个 connection.Config 建立 per-endpoint protocol bundle；它不能修改 reducer state，也不能 fallback 到其它 endpoint。
type EndpointDialer func(context.Context, connection.Config) (EndpointServiceBundle, error)

// EndpointManager 是 TUI/client 侧 endpoint service router。
// connections.yaml 是 registry truth；每个 bundle 是已经建立的 per-endpoint protocol session。
// manager 只负责路由和 endpoint-scoped 结果回填，不拥有 terminal lifecycle 或 history truth。
type EndpointManager struct {
	mu          sync.Mutex
	registry    connection.Registry
	registryErr error
	bundles     map[state.EndpointID]EndpointServiceBundle
	dialers     map[connection.TransportKind]EndpointDialer
	watchers    map[state.EndpointID]<-chan struct{}
	subscribers map[uint64]chan EndpointRuntimeEvent
	nextSubID   uint64
}

// NewEndpointManager 构造一个按 registry 路由的 endpoint manager。
// 当前 workflow 切片只允许 local unix socket bundle；非 local transport 会留在展示层，
// 但真实 service 请求会返回明确错误，避免静默 fallback 成本地 daemon。
func NewEndpointManager(registry connection.Registry, bundles ...EndpointServiceBundle) *EndpointManager {
	return NewEndpointManagerWithDialers(registry, nil, bundles...)
}

// NewEndpointManagerWithDialers 构造支持 lazy transport 连接的 endpoint manager。
// 已连接 bundle 优先复用；缺失 bundle 时按 connection transport 查找 dialer，失败只回到该 endpoint 的 service request。
func NewEndpointManagerWithDialers(registry connection.Registry, dialers map[connection.TransportKind]EndpointDialer, bundles ...EndpointServiceBundle) *EndpointManager {
	normalized, err := registry.Normalize()
	if err != nil {
		normalized = connection.DefaultRegistry()
	}
	manager := &EndpointManager{
		registry:    normalized,
		registryErr: err,
		bundles:     map[state.EndpointID]EndpointServiceBundle{},
		dialers:     cloneEndpointDialers(dialers),
		watchers:    map[state.EndpointID]<-chan struct{}{},
		subscribers: map[uint64]chan EndpointRuntimeEvent{},
	}
	for _, bundle := range bundles {
		endpointID := state.NormalizeEndpointID(bundle.EndpointID)
		if endpointID == "" {
			continue
		}
		bundle.EndpointID = endpointID
		manager.bundles[endpointID] = bundle
	}
	return manager
}

// EndpointStore 返回 renderer/reducer 可消费的 endpoint 展示投影。
// 投影只来自 connection registry；连接成功、失败和 terminal 数量仍由 reducer 消息后续更新。
func (manager *EndpointManager) EndpointStore() state.EndpointStore {
	if manager == nil || manager.registryErr != nil {
		return state.EndpointStore{}
	}
	return state.EndpointStore{}.ApplyConnectionRegistry(manager.registry)
}

// WatchEndpointEvents 订阅 endpoint manager 主动侦测到的 transport/protocol 生命周期事件。
// manager 只发布 endpoint-scoped 状态，不直接修改 reducer，也不会把断线升级成全局 UI 提示。
func (manager *EndpointManager) WatchEndpointEvents(ctx context.Context) (<-chan EndpointRuntimeEvent, error) {
	if manager == nil {
		return nil, fmt.Errorf("endpoint manager is nil")
	}
	out := make(chan EndpointRuntimeEvent, 32)
	manager.mu.Lock()
	manager.nextSubID++
	id := manager.nextSubID
	manager.subscribers[id] = out
	for endpointID, bundle := range manager.bundles {
		manager.startBundleWatcherLocked(endpointID, bundle)
	}
	manager.mu.Unlock()
	go func() {
		<-ctx.Done()
		manager.removeEndpointSubscriber(id)
	}()
	return out, nil
}

// HistoryLatest 把 latest history 请求路由到 owning endpoint 的 core adapter。
// 返回窗口会重新补上 EndpointID，保证 reducer 后续 stale guard 和 copy/history token 都带 endpoint 作用域。
func (manager *EndpointManager) HistoryLatest(ctx context.Context, req HistoryLatestRequest) (HistoryResult, error) {
	endpointID, core, err := manager.core(ctx, req.EndpointID)
	if err != nil {
		return HistoryResult{RequestID: req.RequestID}, err
	}
	req.EndpointID = ""
	result, err := core.HistoryLatest(ctx, req)
	result.Window.EndpointID = endpointID
	return result, err
}

// HistoryOlder 把 older history 请求路由到 owning endpoint 的 core adapter。
// EndpointID 不进入单 daemon adapter，回包时由 manager 恢复为跨 endpoint truth。
func (manager *EndpointManager) HistoryOlder(ctx context.Context, req HistoryOlderRequest) (HistoryResult, error) {
	endpointID, core, err := manager.core(ctx, req.EndpointID)
	if err != nil {
		return HistoryResult{RequestID: req.RequestID}, err
	}
	req.EndpointID = ""
	result, err := core.HistoryOlder(ctx, req)
	result.Window.EndpointID = endpointID
	return result, err
}

// HistoryNewer 把 newer history 请求路由到 owning endpoint 的 core adapter。
// 失败只返回当前 endpoint 的错误，不会触碰其它 endpoint 的 history session。
func (manager *EndpointManager) HistoryNewer(ctx context.Context, req HistoryNewerRequest) (HistoryResult, error) {
	endpointID, core, err := manager.core(ctx, req.EndpointID)
	if err != nil {
		return HistoryResult{RequestID: req.RequestID}, err
	}
	req.EndpointID = ""
	result, err := core.HistoryNewer(ctx, req)
	result.Window.EndpointID = endpointID
	return result, err
}

// HistoryOldest 把 oldest history 请求路由到 owning endpoint 的 core adapter。
// 该方法不推断 history truth，只维护跨 endpoint 请求边界。
func (manager *EndpointManager) HistoryOldest(ctx context.Context, req HistoryOldestRequest) (HistoryResult, error) {
	endpointID, core, err := manager.core(ctx, req.EndpointID)
	if err != nil {
		return HistoryResult{RequestID: req.RequestID}, err
	}
	req.EndpointID = ""
	result, err := core.HistoryOldest(ctx, req)
	result.Window.EndpointID = endpointID
	return result, err
}

// HistoryCopyRange 把 copy range 请求路由到 owning endpoint 的 core adapter。
// 返回文本没有 endpoint 字段；路由身份只用于选择正确 daemon。
func (manager *EndpointManager) HistoryCopyRange(ctx context.Context, req HistoryCopyRangeRequest) (HistoryCopyRangeResult, error) {
	_, core, err := manager.core(ctx, req.EndpointID)
	if err != nil {
		return HistoryCopyRangeResult{}, err
	}
	req.EndpointID = ""
	return core.HistoryCopyRange(ctx, req)
}

// ReleaseHistory 把 history token release 路由到 token 所属 endpoint。
// release 失败只影响该 endpoint 的 core session，不会释放其它 endpoint 的 token。
func (manager *EndpointManager) ReleaseHistory(ctx context.Context, req HistoryReleaseRequest) error {
	_, core, err := manager.core(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	req.EndpointID = ""
	return core.ReleaseHistory(ctx, req)
}

// Attach 把 terminal attach 请求路由到 owning endpoint 的 terminal adapter。
// per-endpoint adapter 只看到 daemon-local TerminalID；manager 负责把结果补回 TerminalRef 的 EndpointID。
func (manager *EndpointManager) Attach(ctx context.Context, req TerminalAttachRequest) (TerminalAttachResult, error) {
	endpointID, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return TerminalAttachResult{EndpointID: endpointID, TerminalID: req.TerminalID}, err
	}
	req.EndpointID = ""
	result, err := terminal.Attach(ctx, req)
	result.EndpointID = endpointID
	return result, err
}

// Detach 把 terminal detach 请求路由到 owning endpoint。
// channel 仍是 daemon-local attachment channel，不能跨 endpoint 复用。
func (manager *EndpointManager) Detach(ctx context.Context, req TerminalDetachRequest) error {
	_, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	req.EndpointID = ""
	return terminal.Detach(ctx, req)
}

// List 把 terminal inventory 请求路由到指定 endpoint。
// 回包的每个 terminal row 都会补上 EndpointID，避免同名 TerminalID 在 pool 中冲突。
func (manager *EndpointManager) List(ctx context.Context, req TerminalListRequest) (TerminalListResult, error) {
	endpointID, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return TerminalListResult{}, err
	}
	req.EndpointID = ""
	result, err := terminal.List(ctx, req)
	for index := range result.Items {
		result.Items[index].EndpointID = endpointID
	}
	return result, err
}

// ListDirectories 把 path completion 请求路由到目标 endpoint 的 daemon 文件系统。
// Prefix 进入单 daemon adapter 后不再带 endpoint；结果由 manager 回填 EndpointID，
// 防止异步补全结果覆盖其它机器的 prompt 状态。
func (manager *EndpointManager) ListDirectories(ctx context.Context, req PathListDirectoriesRequest) (PathListDirectoriesResult, error) {
	endpointID, pathService, err := manager.path(ctx, req.EndpointID)
	if err != nil {
		return PathListDirectoriesResult{EndpointID: endpointID}, err
	}
	req.EndpointID = ""
	result, err := pathService.ListDirectories(ctx, req)
	result.EndpointID = endpointID
	return result, err
}

// Defaults 把创建默认 shell/cwd 查询路由到目标 endpoint 的 daemon。
// 结果由 manager 回填 EndpointID，保证 reducer 缓存的是 endpoint-owned 默认值，
// 而不是 TUI/client 本地进程环境。
func (manager *EndpointManager) Defaults(ctx context.Context, req PathDefaultsRequest) (PathDefaultsResult, error) {
	endpointID, pathService, err := manager.path(ctx, req.EndpointID)
	if err != nil {
		return PathDefaultsResult{EndpointID: endpointID}, err
	}
	req.EndpointID = ""
	result, err := pathService.Defaults(ctx, req)
	result.EndpointID = endpointID
	result.DefaultCommand = append([]string(nil), result.DefaultCommand...)
	return result, err
}

// Create 把 terminal create 请求路由到目标 endpoint。
// create 的 TerminalID 仍由 owning daemon 校验；manager 只补回 endpoint-scoped result。
func (manager *EndpointManager) Create(ctx context.Context, req TerminalCreateRequest) (TerminalCreateResult, error) {
	endpointID, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return TerminalCreateResult{EndpointID: endpointID, TerminalID: req.TerminalID}, err
	}
	req.EndpointID = ""
	result, err := terminal.Create(ctx, req)
	result.EndpointID = endpointID
	return result, err
}

// Restart 把 terminal restart 请求路由到 owning endpoint。
// restart 判断和 lifecycle truth 仍由 core-v2 daemon 负责。
func (manager *EndpointManager) Restart(ctx context.Context, req TerminalRestartRequest) error {
	_, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	req.EndpointID = ""
	return terminal.Restart(ctx, req)
}

// Reconnect 把 reconnect/reattach 请求路由到 owning endpoint。
// 当前切片只支持 local bundle；非 local transport 不会 fallback 成本地 attach。
func (manager *EndpointManager) Reconnect(ctx context.Context, req TerminalReconnectRequest) (TerminalAttachResult, error) {
	endpointID, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return TerminalAttachResult{EndpointID: endpointID, TerminalID: req.TerminalID}, err
	}
	req.EndpointID = ""
	result, err := terminal.Reconnect(ctx, req)
	result.EndpointID = endpointID
	return result, err
}

// Kill 把 terminal kill 请求路由到 owning endpoint。
// kill 只作用于 daemon-local TerminalID，不影响其它 endpoint 的同名 terminal。
func (manager *EndpointManager) Kill(ctx context.Context, req TerminalKillRequest) error {
	_, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	req.EndpointID = ""
	return terminal.Kill(ctx, req)
}

// Remove 把 terminal remove 请求路由到 owning endpoint。
// remove 后 reducer 会按 TerminalRef 清理本地投影，manager 不直接改 state。
func (manager *EndpointManager) Remove(ctx context.Context, req TerminalRemoveRequest) error {
	_, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	req.EndpointID = ""
	return terminal.Remove(ctx, req)
}

// EditMetadata 把 title/tags metadata 请求路由到 owning endpoint。
// metadata 展示值来自 daemon 回包或 list 刷新，manager 不缓存。
func (manager *EndpointManager) EditMetadata(ctx context.Context, req TerminalEditMetadataRequest) error {
	_, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	req.EndpointID = ""
	return terminal.EditMetadata(ctx, req)
}

// EditTags 把 tags 请求路由到 owning endpoint。
// tags 是 terminal metadata，不属于 workbench storage truth。
func (manager *EndpointManager) EditTags(ctx context.Context, req TerminalEditTagsRequest) error {
	_, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	req.EndpointID = ""
	return terminal.EditTags(ctx, req)
}

// SendInput 把 terminal input bytes 路由到 owning endpoint。
// input serial key 在 app 层按 TerminalRef 隔离；manager 只保证字节不会发到错误 daemon。
func (manager *EndpointManager) SendInput(ctx context.Context, req TerminalInputRequest) error {
	_, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	req.EndpointID = ""
	return terminal.SendInput(ctx, req)
}

// Resize 把 resize owner/ensure 请求路由到 owning endpoint。
// resize channel 是 daemon-local attachment channel，返回结果会补回 EndpointID。
func (manager *EndpointManager) Resize(ctx context.Context, req TerminalResizeRequest) (TerminalResizeResult, error) {
	endpointID, terminal, err := manager.terminal(ctx, req.EndpointID)
	if err != nil {
		return TerminalResizeResult{EndpointID: endpointID, TerminalID: req.TerminalID}, err
	}
	req.EndpointID = ""
	result, err := terminal.Resize(ctx, req)
	result.EndpointID = endpointID
	return result, err
}

// LiveSurface 把 latest native screen 请求路由到 owning endpoint。
// native screen 只用于实时显示；manager 不把它提升为 history truth。
func (manager *EndpointManager) LiveSurface(ctx context.Context, req TerminalSurfaceRequest) (TerminalSurfaceResult, error) {
	endpointID, surface, err := manager.surface(ctx, req.EndpointID)
	if err != nil {
		return TerminalSurfaceResult{}, err
	}
	req.EndpointID = ""
	result, err := surface.LiveSurface(ctx, req)
	result.Snapshot.EndpointID = endpointID
	return result, err
}

// ArmLiveInvalidation 把 one-shot live wake 请求路由到 owning endpoint。
// event 只唤醒当前 endpoint 的 live refresh，不跨 endpoint 广播。
func (manager *EndpointManager) ArmLiveInvalidation(ctx context.Context, req TerminalLiveEventRequest) (TerminalLiveEvent, error) {
	endpointID, liveEvents, err := manager.liveEvents(ctx, req.EndpointID)
	if err != nil {
		return TerminalLiveEvent{EndpointID: endpointID, TerminalID: req.TerminalID, Err: err}, err
	}
	req.EndpointID = ""
	event, err := liveEvents.ArmLiveInvalidation(ctx, req)
	event.EndpointID = endpointID
	return event, err
}

func (manager *EndpointManager) core(ctx context.Context, endpointID state.EndpointID) (state.EndpointID, CoreClient, error) {
	endpointID, bundle, err := manager.bundle(ctx, endpointID)
	if err != nil {
		return endpointID, nil, err
	}
	if bundle.Core == nil {
		return endpointID, nil, fmt.Errorf("endpoint %q has no core service", endpointID)
	}
	return endpointID, bundle.Core, nil
}

func (manager *EndpointManager) terminal(ctx context.Context, endpointID state.EndpointID) (state.EndpointID, TerminalService, error) {
	endpointID, bundle, err := manager.bundle(ctx, endpointID)
	if err != nil {
		return endpointID, nil, err
	}
	if bundle.Terminal == nil {
		return endpointID, nil, fmt.Errorf("endpoint %q has no terminal service", endpointID)
	}
	return endpointID, bundle.Terminal, nil
}

func (manager *EndpointManager) path(ctx context.Context, endpointID state.EndpointID) (state.EndpointID, PathService, error) {
	endpointID, bundle, err := manager.bundle(ctx, endpointID)
	if err != nil {
		return endpointID, nil, err
	}
	if bundle.Path == nil {
		return endpointID, nil, fmt.Errorf("endpoint %q has no path service", endpointID)
	}
	return endpointID, bundle.Path, nil
}

func (manager *EndpointManager) surface(ctx context.Context, endpointID state.EndpointID) (state.EndpointID, TerminalSurfaceService, error) {
	endpointID, bundle, err := manager.bundle(ctx, endpointID)
	if err != nil {
		return endpointID, nil, err
	}
	if bundle.Surface == nil {
		return endpointID, nil, fmt.Errorf("endpoint %q has no live surface service", endpointID)
	}
	return endpointID, bundle.Surface, nil
}

func (manager *EndpointManager) liveEvents(ctx context.Context, endpointID state.EndpointID) (state.EndpointID, TerminalLiveEventService, error) {
	endpointID, bundle, err := manager.bundle(ctx, endpointID)
	if err != nil {
		return endpointID, nil, err
	}
	if bundle.LiveEvents == nil {
		return endpointID, nil, fmt.Errorf("endpoint %q has no live event service", endpointID)
	}
	return endpointID, bundle.LiveEvents, nil
}

func (manager *EndpointManager) bundle(ctx context.Context, endpointID state.EndpointID) (state.EndpointID, EndpointServiceBundle, error) {
	endpointID = state.NormalizeEndpointID(endpointID)
	if manager == nil {
		return endpointID, EndpointServiceBundle{}, fmt.Errorf("endpoint manager is nil")
	}
	if manager.registryErr != nil {
		return endpointID, EndpointServiceBundle{}, fmt.Errorf("endpoint registry invalid: %w", manager.registryErr)
	}
	cfg, ok := manager.registry.Connections[connection.EndpointID(endpointID)]
	if !ok {
		return endpointID, EndpointServiceBundle{}, fmt.Errorf("endpoint %q is not registered", endpointID)
	}
	if !cfg.Enabled {
		return endpointID, EndpointServiceBundle{}, fmt.Errorf("endpoint %q is disabled", endpointID)
	}
	manager.mu.Lock()
	bundle, ok := manager.bundles[endpointID]
	dialer := manager.dialers[cfg.Transport]
	manager.mu.Unlock()
	if ok {
		return endpointID, bundle, nil
	}
	if dialer == nil {
		return endpointID, EndpointServiceBundle{}, fmt.Errorf("endpoint %q transport %q is not connected", endpointID, cfg.Transport)
	}
	bundle, err := dialer(ctx, cfg)
	if err != nil {
		return endpointID, EndpointServiceBundle{}, err
	}
	bundle.EndpointID = endpointID
	manager.mu.Lock()
	if existing, ok := manager.bundles[endpointID]; ok {
		manager.mu.Unlock()
		return endpointID, existing, nil
	}
	manager.bundles[endpointID] = bundle
	manager.startBundleWatcherLocked(endpointID, bundle)
	manager.mu.Unlock()
	return endpointID, bundle, nil
}

func (manager *EndpointManager) startBundleWatcherLocked(endpointID state.EndpointID, bundle EndpointServiceBundle) {
	done := bundle.Lifecycle.Done
	if done == nil {
		return
	}
	if existing := manager.watchers[endpointID]; existing == done {
		return
	}
	manager.watchers[endpointID] = done
	go manager.watchBundleLifecycle(endpointID, bundle.Lifecycle)
}

func (manager *EndpointManager) watchBundleLifecycle(endpointID state.EndpointID, lifecycle EndpointLifecycle) {
	<-lifecycle.Done
	err := io.EOF
	if lifecycle.Err != nil {
		if lifecycleErr := lifecycle.Err(); lifecycleErr != nil {
			err = lifecycleErr
		}
	}
	event := EndpointRuntimeEvent{
		EndpointID: endpointID,
		Status:     state.EndpointStatusOffline,
		ErrorKind:  ClassifyEndpointError(err),
		Message:    err.Error(),
		Err:        err,
	}
	manager.mu.Lock()
	if current := manager.watchers[endpointID]; current != lifecycle.Done {
		manager.mu.Unlock()
		return
	}
	if current, ok := manager.bundles[endpointID]; ok && current.Lifecycle.Done == lifecycle.Done {
		delete(manager.bundles, endpointID)
	}
	delete(manager.watchers, endpointID)
	subscribers := cloneEndpointEventSubscribers(manager.subscribers)
	manager.mu.Unlock()
	publishEndpointRuntimeEvent(subscribers, event)
}

func (manager *EndpointManager) removeEndpointSubscriber(id uint64) {
	manager.mu.Lock()
	delete(manager.subscribers, id)
	manager.mu.Unlock()
}

func cloneEndpointEventSubscribers(values map[uint64]chan EndpointRuntimeEvent) []chan EndpointRuntimeEvent {
	if len(values) == 0 {
		return nil
	}
	out := make([]chan EndpointRuntimeEvent, 0, len(values))
	for _, ch := range values {
		out = append(out, ch)
	}
	return out
}

func publishEndpointRuntimeEvent(subscribers []chan EndpointRuntimeEvent, event EndpointRuntimeEvent) {
	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func cloneEndpointDialers(dialers map[connection.TransportKind]EndpointDialer) map[connection.TransportKind]EndpointDialer {
	if len(dialers) == 0 {
		return nil
	}
	cloned := make(map[connection.TransportKind]EndpointDialer, len(dialers))
	for transportKind, dialer := range dialers {
		if dialer != nil {
			cloned[transportKind] = dialer
		}
	}
	return cloned
}
