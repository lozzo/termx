package termxcorev2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport"
	unixtransport "github.com/lozzow/termx/termx-shared/transport/unix"
)

type ListenerFactory func(socketPath string) (transport.Listener, error)

type ServerOption func(*serverConfig)

type serverConfig struct {
	socketPath          string
	defaultSize         Size
	logger              *slog.Logger
	listenerFactory     ListenerFactory
	processFactory      ProcessFactory
	remoteService       RemoteService
	eventBuffer         int
	historyStoreFactory HistoryStoreFactory
	historyStorageDir   string
	historyDisabled     bool
}

// HistoryStoreFactory 为每个 terminal 创建 core-v2 authoritative history store。
// domain owner 是 Server/Terminal lifecycle：工厂只决定 payload residency backend，
// 不能解释 terminal semantic transaction，也不能替代 HistoryStore 的 cursor truth。
type HistoryStoreFactory func(terminalID string) (history.HistoryStore, error)

type Server struct {
	cfg                   serverConfig
	registry              *terminalRegistry
	storage               *storageStore
	workbench             *workbenchStore
	terminals             map[string]*Terminal
	events                *eventBroker
	closed                atomic.Bool
	nextProtocolSessionID atomic.Uint64
	lifecycleMu           sync.Mutex
	protocolAttachmentMu  sync.Mutex
	protocolAttachments   map[string]protocolAttachment
	protocolChannelIndex  map[protocolAttachmentKey]string
	protocolResizeOwners  map[string]string
	protocolSizeLocks     map[string]bool
	protocolOwnerEpoch    uint64
	remoteServiceMu       sync.RWMutex
	remoteService         RemoteService
	mu                    sync.Mutex
	listeners             []transport.Listener
	transports            map[transport.Transport]struct{}
	wgMu                  sync.Mutex
	wg                    sync.WaitGroup
}

func NewServer(opts ...ServerOption) *Server {
	cfg := serverConfig{
		socketPath:      defaultSocketPath(),
		defaultSize:     Size{Cols: 80, Rows: 24},
		logger:          slog.Default(),
		listenerFactory: unixListenerFactory,
		processFactory:  newPTYProcessFactory(),
		eventBuffer:     64,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.logger == nil {
		cfg.logger = slog.Default()
	}
	if cfg.listenerFactory == nil {
		cfg.listenerFactory = unixListenerFactory
	}
	if cfg.processFactory == nil {
		cfg.processFactory = newPTYProcessFactory()
	}
	return &Server{
		cfg:                  cfg,
		registry:             newTerminalRegistry(),
		storage:              newStorageStore(),
		workbench:            newWorkbenchStore(),
		terminals:            make(map[string]*Terminal),
		events:               newEventBroker(cfg.eventBuffer),
		protocolAttachments:  make(map[string]protocolAttachment),
		protocolChannelIndex: make(map[protocolAttachmentKey]string),
		protocolResizeOwners: make(map[string]string),
		protocolSizeLocks:    make(map[string]bool),
		remoteService:        cfg.remoteService,
		transports:           make(map[transport.Transport]struct{}),
	}
}

func WithSocketPath(path string) ServerOption {
	return func(cfg *serverConfig) {
		if path != "" {
			cfg.socketPath = path
		}
	}
}

func WithDefaultSize(cols, rows uint16) ServerOption {
	return func(cfg *serverConfig) {
		cfg.defaultSize = Size{Cols: cols, Rows: rows}
	}
}

func WithLogger(logger *slog.Logger) ServerOption {
	return func(cfg *serverConfig) {
		cfg.logger = logger
	}
}

func WithListenerFactory(factory ListenerFactory) ServerOption {
	return func(cfg *serverConfig) {
		cfg.listenerFactory = factory
	}
}

func WithProcessFactory(factory ProcessFactory) ServerOption {
	return func(cfg *serverConfig) {
		cfg.processFactory = factory
	}
}

func WithRemoteService(service RemoteService) ServerOption {
	return func(cfg *serverConfig) {
		cfg.remoteService = service
	}
}

func WithEventBuffer(size int) ServerOption {
	return func(cfg *serverConfig) {
		cfg.eventBuffer = size
	}
}

// WithHistoryStoreFactory 注入 terminal history store 工厂。调用边界是
// RegisterTerminal；工厂返回错误时 terminal 创建必须失败，不能静默回退到内存
// store 后继续占用大量 RSS。
func WithHistoryStoreFactory(factory HistoryStoreFactory) ServerOption {
	return func(cfg *serverConfig) {
		cfg.historyStoreFactory = factory
	}
}

// WithHistoryStorageDir 为生产 daemon 配置文件-backed sealed payload store。
// 文件 backend 只承载 logical-line payload 驻留；timeline/window/cursor truth 仍由
// core-v2 HistoryStore 持有。
func WithHistoryStorageDir(dir string) ServerOption {
	return func(cfg *serverConfig) {
		cfg.historyStorageDir = dir
		cfg.historyStoreFactory = fileBackedHistoryStoreFactory(dir)
		cfg.historyDisabled = false
	}
}

// WithHistoryDisabled 关闭 core-v2 authoritative history owner。
// domain owner 是 Server/Terminal lifecycle：关闭后 terminal 只维护 native live
// screen 与 live revision，PTY 写入热路径不会创建 renderer/store mutation，
// history.window/copy/freeze/release 必须返回 ErrHistoryDisabled。
func WithHistoryDisabled() ServerOption {
	return func(cfg *serverConfig) {
		cfg.historyDisabled = true
		cfg.historyStoreFactory = nil
		cfg.historyStorageDir = ""
	}
}

// SetRemoteService 允许 daemon 完成 core-v2 server 构造后再注入 remote runtime hook。
func (server *Server) SetRemoteService(service RemoteService) {
	server.remoteServiceMu.Lock()
	defer server.remoteServiceMu.Unlock()
	server.remoteService = service
}

func (server *Server) RemoteService() RemoteService {
	server.remoteServiceMu.RLock()
	defer server.remoteServiceMu.RUnlock()
	return server.remoteService
}

func (server *Server) SocketPath() string {
	return server.cfg.socketPath
}

func (server *Server) DefaultSize() Size {
	return server.cfg.defaultSize
}

// HistoryStorageDir 返回 daemon 配置的 file-backed history payload 目录。
// 它只用于诊断和测试确认 residency 配置；history truth 仍由 Terminal 的
// HistoryStore/window/cursor contract 决定。
func (server *Server) HistoryStorageDir() string {
	return server.cfg.historyStorageDir
}

func (server *Server) RegisterTerminal(record TerminalRecord) (TerminalInfo, error) {
	server.lifecycleMu.Lock()
	defer server.lifecycleMu.Unlock()
	if server.closed.Load() {
		return TerminalInfo{}, ErrServerClosed
	}
	info, err := server.registry.register(record, server.cfg.defaultSize)
	if err != nil {
		return TerminalInfo{}, err
	}
	var historyStore history.HistoryStore
	historyEnabled := !server.cfg.historyDisabled
	if historyEnabled {
		var err error
		historyStore, err = server.newHistoryStore(info.ID)
		if err != nil {
			_, _ = server.registry.remove(info.ID)
			return TerminalInfo{}, err
		}
	}
	process, err := server.cfg.processFactory.Spawn(context.Background(), processSpecFromTerminal(info, record.Options))
	if err != nil {
		_, _ = server.registry.remove(info.ID)
		return TerminalInfo{}, err
	}
	terminal := newTerminal(info, record.Options, process, server.events, server.updateTerminalInfo, historyStore, historyEnabled)
	server.mu.Lock()
	server.terminals[info.ID] = terminal
	server.mu.Unlock()
	server.publishTerminalEvent(EventTerminalCreated, info)
	return info, nil
}

func (server *Server) newHistoryStore(terminalID string) (history.HistoryStore, error) {
	if server.cfg.historyStoreFactory == nil {
		return history.NewInMemoryHistoryStore(terminalID), nil
	}
	return server.cfg.historyStoreFactory(terminalID)
}

func fileBackedHistoryStoreFactory(dir string) HistoryStoreFactory {
	return func(terminalID string) (history.HistoryStore, error) {
		if strings.TrimSpace(dir) == "" {
			return history.NewInMemoryHistoryStore(terminalID), nil
		}
		backend, err := history.NewFileStorageBackend(dir, terminalID)
		if err != nil {
			return nil, err
		}
		return history.NewBackendHistoryStore(terminalID, backend), nil
	}
}

func processSpecFromTerminal(info TerminalInfo, options TerminalCreateOptions) ProcessSpec {
	return ProcessSpec{
		TerminalID:         info.ID,
		Command:            append([]string(nil), info.Command...),
		Size:               info.Size,
		Dir:                options.Dir,
		Env:                append([]string(nil), options.Env...),
		ScrollbackSize:     options.ScrollbackSize,
		ScrollbackMaxBytes: options.ScrollbackMaxBytes,
		ScrollbackMaxAge:   options.ScrollbackMaxAge,
	}
}

func (server *Server) GetTerminal(id string) (TerminalInfo, error) {
	return server.registry.get(id)
}

func (server *Server) ListTerminals() []TerminalInfo {
	return server.registry.list()
}

func (server *Server) StorageGet(ctx context.Context, appID string, scope StorageScope, ownerID string, key string) (StorageEntry, error) {
	_ = ctx
	return server.storage.get(appID, scope, ownerID, key)
}

func (server *Server) StoragePut(ctx context.Context, request StoragePutRequest) (StorageEntry, error) {
	_ = ctx
	entry, err := server.storage.put(request)
	if err != nil {
		return StorageEntry{}, err
	}
	server.publishStorageEvent(StorageChanged{
		AppID:   entry.AppID,
		Scope:   entry.Scope,
		OwnerID: entry.OwnerID,
		Key:     entry.Key,
		Version: entry.Version,
		Op:      StorageOpPut,
	})
	return entry, nil
}

func (server *Server) StorageDelete(ctx context.Context, request StorageDeleteRequest) (StorageDeleteResult, error) {
	_ = ctx
	result, err := server.storage.delete(request)
	if err != nil {
		return StorageDeleteResult{}, err
	}
	server.publishStorageEvent(StorageChanged{
		AppID:   result.AppID,
		Scope:   result.Scope,
		OwnerID: result.OwnerID,
		Key:     result.Key,
		Version: result.Version,
		Op:      StorageOpDelete,
	})
	return result, nil
}

func (server *Server) StorageList(ctx context.Context, appID string, scope StorageScope, ownerID string, prefix string) []StorageEntry {
	_ = ctx
	return server.storage.list(appID, scope, ownerID, prefix)
}

func (server *Server) WorkbenchSnapshot(ctx context.Context, workspaceID string) (WorkbenchSnapshot, error) {
	_ = ctx
	return server.workbench.get(workspaceID)
}

func (server *Server) ApplyWorkbenchMutation(ctx context.Context, params WorkbenchMutateParams) (WorkbenchMutateResult, error) {
	_ = ctx
	result, change, err := server.workbench.apply(params)
	if err != nil {
		return WorkbenchMutateResult{}, err
	}
	server.publishWorkbenchEvent(change)
	return result, nil
}

func (server *Server) SetMetadata(ctx context.Context, id string, name string, tags map[string]string) (TerminalInfo, error) {
	_ = ctx
	terminal, err := server.Terminal(id)
	if err == nil {
		return terminal.SetMetadata(name, tags), nil
	}
	if !errors.Is(err, ErrTerminalNotFound) {
		return TerminalInfo{}, err
	}
	info, err := server.registry.setMetadata(id, name, tags)
	if err != nil {
		return TerminalInfo{}, err
	}
	server.publishTerminalEvent(EventTerminalMetadataChanged, info)
	return info, nil
}

func (server *Server) RemoveTerminal(id string) error {
	server.lifecycleMu.Lock()
	defer server.lifecycleMu.Unlock()
	if server.closed.Load() {
		return ErrServerClosed
	}
	info, err := server.registry.remove(id)
	if err != nil {
		return err
	}
	terminal := server.removeTerminalHandle(id)
	if terminal != nil {
		_ = terminal.Close()
	}
	server.publishTerminalEvent(EventTerminalRemoved, info)
	return nil
}

func (server *Server) updateTerminalInfo(info TerminalInfo) {
	_ = server.registry.replace(info)
}

func (server *Server) Terminal(id string) (*Terminal, error) {
	server.mu.Lock()
	defer server.mu.Unlock()
	terminal, ok := server.terminals[id]
	if !ok {
		return nil, ErrTerminalNotFound
	}
	return terminal, nil
}

func (server *Server) WriteInput(ctx context.Context, id string, data []byte) error {
	_ = ctx
	terminal, err := server.Terminal(id)
	if err != nil {
		return err
	}
	return terminal.Input(data)
}

func (server *Server) IngestOutput(ctx context.Context, id string, output string) error {
	_ = ctx
	terminal, err := server.Terminal(id)
	if err != nil {
		return err
	}
	return terminal.IngestOutput(output)
}

func (server *Server) ResizeTerminal(ctx context.Context, id string, cols, rows uint16) error {
	_ = ctx
	terminal, err := server.Terminal(id)
	if err != nil {
		return err
	}
	return terminal.Resize(Size{Cols: cols, Rows: rows})
}

func (server *Server) KillTerminal(ctx context.Context, id string) error {
	_ = ctx
	terminal, err := server.Terminal(id)
	if err != nil {
		return err
	}
	return terminal.Kill()
}

func (server *Server) RestartTerminal(ctx context.Context, id string) error {
	terminal, err := server.Terminal(id)
	if err != nil {
		return err
	}
	before := terminal.Info()
	coreLifecycleTrace(server.cfg.logger, "server.restart.request", coreTerminalInfoAttrs(before)...)
	if err := terminal.Restart(ctx, server.cfg.processFactory); err != nil {
		coreLifecycleTrace(server.cfg.logger, "server.restart.result",
			"terminal_id", id,
			"error", err.Error(),
			"state_before", string(before.State),
			"exit_code_before", coreTraceExitCode(before.ExitCode),
			"exited_at_before", coreTraceTime(before.ExitedAt),
		)
		return err
	}
	after := terminal.Info()
	attrs := coreTerminalInfoAttrs(after)
	attrs = append(attrs,
		"state_before", string(before.State),
		"exited_at_before", coreTraceTime(before.ExitedAt),
	)
	coreLifecycleTrace(server.cfg.logger, "server.restart.result", attrs...)
	return nil
}

func (server *Server) LiveRows(id string) ([]string, error) {
	terminal, err := server.Terminal(id)
	if err != nil {
		return nil, err
	}
	return terminal.LiveRows(), nil
}

func (server *Server) LiveSnapshot(id string) (live.SurfaceSnapshot, error) {
	terminal, err := server.Terminal(id)
	if err != nil {
		return live.SurfaceSnapshot{}, err
	}
	return terminal.LiveSnapshot(), nil
}

// TerminalHistoryBacklogStatus 返回 terminal history consumer 的 applied/target seq。
// 它是只读诊断入口，不触发 FlushHistory，也不读取 live snapshot 或组装 window；
// 后续 copy materialized projection 可以用它判断 catchup_pending。
func (server *Server) TerminalHistoryBacklogStatus(id string) (HistoryBacklogStatus, error) {
	terminal, err := server.Terminal(id)
	if err != nil {
		return HistoryBacklogStatus{}, err
	}
	return terminal.HistoryBacklogStatus(), nil
}

// NextLiveInvalidation 等待指定 terminal 的下一次 live invalidation。
// observedRevision 是客户端已从 core 看到的 native screen revision，不是 TUI 已渲染
// revision；core 只用它补 one-shot arm 间隙丢失的 wake，仍不维护客户端渲染进度。
func (server *Server) NextLiveInvalidation(ctx context.Context, id string, observedRevision LiveRevision) (Event, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	terminal, err := server.Terminal(id)
	if err != nil {
		return Event{}, err
	}
	if revision := terminal.LiveRevision(); revision > observedRevision {
		// 中文说明：这是 latest native screen 的边沿补偿，不是事件回放队列。
		// 客户端只得到一个 wake，随后仍自行拉 core 当前 latest screen。
		return Event{
			Type:       EventTerminalLiveInvalidated,
			TerminalID: id,
			Live: &LiveScreenInvalidated{
				TerminalID: id,
				Revision:   revision,
			},
		}, nil
	}
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := server.Events(waitCtx, EventFilter{
		TerminalID: id,
		Types:      []EventType{EventTerminalLiveInvalidated},
	})
	for {
		select {
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case event, ok := <-events:
			if !ok {
				return Event{}, context.Canceled
			}
			if event.Live == nil {
				continue
			}
			if event.Live.Revision <= observedRevision {
				continue
			}
			return event, nil
		}
	}
}

// TerminalHistoryWindow 返回 core-v2 terminal 内部 authoritative history domain
// projection。调用边界包括 protocol history.window 和 domain harness；实现只能读取
// core history store，不能 fallback 到 live rows 或 snapshot。
func (server *Server) TerminalHistoryWindow(ctx context.Context, id string, req history.HistoryWindowRequest) (history.HistoryWindow, error) {
	return server.terminalHistoryWindow(ctx, id, req, true)
}

// TerminalHistoryCopyEntryProjection 返回 copy/history 入口的 materialized projection。
// 它不 flush history backlog、不创建 FrozenHistorySnapshot，只读取 HistoryStore 已应用
// frontier；native cols 和 backlog seq 只是 projection 元数据，不能把 live screen rows
// 提升为可复制 history truth。
func (server *Server) TerminalHistoryCopyEntryProjection(ctx context.Context, id string, req history.CopyEntryProjectionRequest) (history.CopyEntryProjection, error) {
	_ = ctx
	terminal, err := server.Terminal(id)
	if err != nil {
		return history.CopyEntryProjection{}, err
	}
	if req.TerminalID == "" {
		req.TerminalID = id
	}
	status := terminal.HistoryBacklogStatus()
	req.AppliedHistorySeq = status.AppliedSeq
	req.TargetHistorySeq = status.TargetSeq
	req.CatchupPending = status.CatchupPending
	if req.NativeCols <= 0 {
		native := terminal.NativeScreenSnapshot(id)
		req.NativeCols = int(native.Size.Cols)
	}
	projection, err := terminal.HistoryCopyEntryProjection(req)
	if err != nil {
		return history.CopyEntryProjection{}, err
	}
	attrs := []any{
		"terminal_id", projection.TerminalID,
		"cols", req.Cols,
		"native_cols", projection.NativeCols,
		"rows", len(projection.Window.Rows),
		"generation", uint64(projection.Generation),
		"catchup_pending", projection.CatchupPending,
		"selectable", projection.Capabilities.Selectable,
		"copyable", projection.Capabilities.Copyable,
		"searchable", projection.Capabilities.Searchable,
		"pageable", projection.Capabilities.Pageable,
		"summary", coreHistoryWindowSummary(projection.Window.Rows),
	}
	attrs = append(attrs, coreHistoryBacklogAttrs("projection", status)...)
	attrs = append(attrs, coreHistoryCursorAttrs("projection", projection.Boundary.Cursor)...)
	coreHistoryTrace(server.cfg.logger, "core.store.copy_entry_projection", attrs...)
	return projection, nil
}

func (server *Server) terminalHistoryWindow(ctx context.Context, id string, req history.HistoryWindowRequest, flush bool) (history.HistoryWindow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	terminal, err := server.Terminal(id)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	if flush {
		// 中文说明：普通 window/copy 调用必须先等 history worker 追平；
		// protocol latest 已先通过 Freeze 建立 token fence，读取同一 token window 时
		// 不再重复 flush，避免压力输出后 copy 入口多等一轮 history queue。
		if err := terminal.FlushHistory(ctx); err != nil {
			return history.HistoryWindow{}, err
		}
	} else if req.Token == "" {
		return history.HistoryWindow{}, history.ErrHistoryInvalidMutation
	}
	if req.TerminalID == "" {
		req.TerminalID = id
	}
	requestAttrs := []any{
		"terminal_id", req.TerminalID,
		"mode", string(req.Mode),
		"cols", req.Cols,
		"limit", req.Limit,
		"token", string(req.Token),
	}
	if status, err := server.TerminalHistoryBacklogStatus(id); err == nil {
		requestAttrs = append(requestAttrs, coreHistoryBacklogAttrs("request", status)...)
	}
	requestAttrs = append(requestAttrs, coreHistoryCursorAttrs("request", req.Cursor)...)
	requestAttrs = append(requestAttrs, coreHistoryBoundaryAttrs("request", req.Boundary)...)
	coreHistoryTrace(server.cfg.logger, "core.store.request_window", requestAttrs...)
	window, err := terminal.HistoryWindow(req)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	responseAttrs := []any{
		"terminal_id", window.TerminalID,
		"mode", string(req.Mode),
		"op", string(window.Op),
		"cols", window.Cols,
		"rows", len(window.Rows),
		"lines", len(window.Lines),
		"token", string(window.Token),
		"generation", uint64(window.Generation),
		"has_more", window.HasMore,
		"logical_total", window.LogicalTotal,
		"summary", coreHistoryWindowSummary(window.Rows),
	}
	if status, err := server.TerminalHistoryBacklogStatus(id); err == nil {
		responseAttrs = append(responseAttrs, coreHistoryBacklogAttrs("response", status)...)
	}
	responseAttrs = append(responseAttrs, coreHistoryCursorAttrs("response", window.Boundary.Cursor)...)
	responseAttrs = append(responseAttrs, coreHistoryBoundaryAttrs("response", window.Boundary)...)
	coreHistoryTrace(server.cfg.logger, "core.store.response_window", responseAttrs...)
	return window, nil
}

// TerminalHistoryCopy 从 core-v2 terminal 内部 authoritative history domain 复制文本。
// 调用边界包括 protocol history.copy；复制文本必须来自 tokenized history payload，
// 不能由 TUI rows 或 live surface cache 组装。
func (server *Server) TerminalHistoryCopy(ctx context.Context, id string, req history.HistoryCopyRequest) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	terminal, err := server.Terminal(id)
	if err != nil {
		return "", err
	}
	if err := terminal.FlushHistory(ctx); err != nil {
		return "", err
	}
	if req.TerminalID == "" {
		req.TerminalID = id
	}
	return terminal.HistoryCopy(req)
}

// TerminalHistoryFreeze 创建 core-v2 terminal 内部 frozen history boundary。它为
// protocol latest/copy 建立 core-owned token，后续 repaint 不得改写该 token 的复制边界。
func (server *Server) TerminalHistoryFreeze(ctx context.Context, id string, req history.FreezeHistoryRequest) (history.FrozenHistorySnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	terminal, err := server.Terminal(id)
	if err != nil {
		return history.FrozenHistorySnapshot{}, err
	}
	if err := terminal.FlushHistory(ctx); err != nil {
		return history.FrozenHistorySnapshot{}, err
	}
	if req.TerminalID == "" {
		req.TerminalID = id
	}
	return terminal.HistoryFreeze(req)
}

// TerminalHistoryRelease 释放 core-v2 authoritative history token。调用边界是
// protocol history.release；它只回收 frozen/window 资源，不删除 committed history truth。
func (server *Server) TerminalHistoryRelease(ctx context.Context, id string, token history.HistoryToken) error {
	_ = ctx
	terminal, err := server.Terminal(id)
	if err != nil {
		return err
	}
	return terminal.HistoryRelease(token)
}

func (server *Server) Events(ctx context.Context, filter EventFilter) <-chan Event {
	if ctx == nil {
		ctx = context.Background()
	}
	return server.events.subscribe(ctx, filter)
}

func (server *Server) ListenAndServe(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if server.closed.Load() {
		return ErrServerClosed
	}
	listener, err := server.cfg.listenerFactory(server.cfg.socketPath)
	if err != nil {
		return err
	}
	server.mu.Lock()
	server.listeners = append(server.listeners, listener)
	server.mu.Unlock()
	server.events.publish(Event{Type: EventServerListening, SocketPath: listener.Addr()})
	server.cfg.logger.Info("core-v2 server listening", "socket_path", listener.Addr())
	defer server.waitTransports()
	defer func() {
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return server.Shutdown(context.Background())
			}
			if errors.Is(err, transport.ErrListenerClosed) || server.closed.Load() {
				return nil
			}
			server.cfg.logger.Warn("core-v2 server accept failed", "socket_path", listener.Addr(), "error", err)
			continue
		}
		if !server.startTransport(ctx, conn) {
			return nil
		}
	}
}

func (server *Server) ServeTransport(ctx context.Context, conn transport.Transport) error {
	return server.ServeScopedTransport(ctx, conn, TransportScope{})
}

func (server *Server) ServeScopedTransport(ctx context.Context, conn transport.Transport, scope TransportScope) error {
	if ctx == nil {
		ctx = context.Background()
	}
	scope = scope.normalized()
	if err := scope.validate(); err != nil {
		_ = conn.Close()
		return err
	}
	server.wgMu.Lock()
	if server.closed.Load() {
		server.wgMu.Unlock()
		_ = conn.Close()
		return ErrServerClosed
	}
	server.trackTransport(conn)
	server.wgMu.Unlock()
	return server.serveTrackedTransport(ctx, conn, scope)
}

func (server *Server) Shutdown(ctx context.Context) error {
	_ = ctx
	server.lifecycleMu.Lock()
	defer server.lifecycleMu.Unlock()
	if !server.closed.CompareAndSwap(false, true) {
		return nil
	}
	server.mu.Lock()
	listeners := append([]transport.Listener(nil), server.listeners...)
	transports := make([]transport.Transport, 0, len(server.transports))
	for conn := range server.transports {
		transports = append(transports, conn)
	}
	server.mu.Unlock()
	for _, listener := range listeners {
		_ = listener.Close()
	}
	for _, conn := range transports {
		_ = conn.Close()
	}
	server.mu.Lock()
	terminals := make([]*Terminal, 0, len(server.terminals))
	for _, terminal := range server.terminals {
		terminals = append(terminals, terminal)
	}
	server.terminals = make(map[string]*Terminal)
	server.mu.Unlock()
	for _, terminal := range terminals {
		_ = terminal.closeWithReason(history.CloseReasonDaemonShutdown)
	}
	server.registry.clear()
	server.events.publish(Event{Type: EventServerStopped, SocketPath: server.cfg.socketPath})
	server.events.close()
	server.waitTransports()
	server.cfg.logger.Info("core-v2 server stopped", "socket_path", server.cfg.socketPath)
	return nil
}

func (server *Server) removeTerminalHandle(id string) *Terminal {
	server.mu.Lock()
	defer server.mu.Unlock()
	terminal := server.terminals[id]
	delete(server.terminals, id)
	return terminal
}

func (server *Server) handleTransport(ctx context.Context, conn transport.Transport) {
	defer server.wg.Done()
	if err := server.serveTrackedTransport(ctx, conn, TransportScope{}); err != nil && !errors.Is(err, transport.ErrListenerClosed) {
		server.cfg.logger.Debug("core-v2 protocol session stopped", "error", err)
	}
}

func (server *Server) serveTrackedTransport(ctx context.Context, conn transport.Transport, scope TransportScope) error {
	defer server.untrackTransport(conn)
	defer func() { _ = conn.Close() }()
	session := newProtocolSession(server, conn, scope)
	return session.run(ctx)
}

func (server *Server) startTransport(ctx context.Context, conn transport.Transport) bool {
	server.wgMu.Lock()
	defer server.wgMu.Unlock()
	if server.closed.Load() {
		_ = conn.Close()
		return false
	}
	server.trackTransport(conn)
	server.wg.Add(1)
	go server.handleTransport(ctx, conn)
	return true
}

func (server *Server) waitTransports() {
	server.wgMu.Lock()
	defer server.wgMu.Unlock()
	server.wg.Wait()
}

func (server *Server) trackTransport(conn transport.Transport) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.transports[conn] = struct{}{}
}

func (server *Server) untrackTransport(conn transport.Transport) {
	server.mu.Lock()
	defer server.mu.Unlock()
	delete(server.transports, conn)
}

func (server *Server) publishTerminalEvent(typ EventType, info TerminalInfo) {
	terminal := info.Clone()
	server.events.publish(Event{
		Type:       typ,
		TerminalID: info.ID,
		Terminal:   &terminal,
	})
}

func (server *Server) publishStorageEvent(change StorageChanged) {
	server.events.publish(Event{Type: EventStorageChanged, Storage: &change})
}

func (server *Server) publishWorkbenchEvent(change WorkbenchChanged) {
	server.events.publish(Event{Type: EventWorkbenchChanged, Workbench: &change})
}

func unixListenerFactory(socketPath string) (transport.Listener, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("core-v2 server: empty socket path")
	}
	return unixtransport.NewListener(socketPath)
}

func defaultSocketPath() string {
	name := fmt.Sprintf("termx-v2-wire%d.sock", wire.Version)
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return runtimeDir + "/" + name
	}
	return fmt.Sprintf("%s/termx-v2-wire%d-%d.sock", os.TempDir(), wire.Version, os.Getuid())
}
