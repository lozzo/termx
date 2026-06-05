package termxcorev2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
	"github.com/lozzow/termx/termx-shared/transport"
	unixtransport "github.com/lozzow/termx/termx-shared/transport/unix"
)

type ListenerFactory func(socketPath string) (transport.Listener, error)

type ServerOption func(*serverConfig)

type serverConfig struct {
	socketPath      string
	defaultSize     Size
	logger          *slog.Logger
	listenerFactory ListenerFactory
	processFactory  ProcessFactory
	eventBuffer     int
}

type Server struct {
	cfg         serverConfig
	registry    *terminalRegistry
	terminals   map[string]*Terminal
	events      *eventBroker
	closed      atomic.Bool
	lifecycleMu sync.Mutex
	mu          sync.Mutex
	listeners   []transport.Listener
	transports  map[transport.Transport]struct{}
	wgMu        sync.Mutex
	wg          sync.WaitGroup
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
		cfg:        cfg,
		registry:   newTerminalRegistry(),
		terminals:  make(map[string]*Terminal),
		events:     newEventBroker(cfg.eventBuffer),
		transports: make(map[transport.Transport]struct{}),
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

func WithEventBuffer(size int) ServerOption {
	return func(cfg *serverConfig) {
		cfg.eventBuffer = size
	}
}

func (server *Server) SocketPath() string {
	return server.cfg.socketPath
}

func (server *Server) DefaultSize() Size {
	return server.cfg.defaultSize
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
	process, err := server.cfg.processFactory.Spawn(context.Background(), ProcessSpec{
		TerminalID: info.ID,
		Command:    info.Command,
		Size:       info.Size,
	})
	if err != nil {
		_, _ = server.registry.remove(info.ID)
		return TerminalInfo{}, err
	}
	terminal := newTerminal(info, process, server.events, server.updateTerminalInfo)
	server.mu.Lock()
	server.terminals[info.ID] = terminal
	server.mu.Unlock()
	server.publishTerminalEvent(EventTerminalCreated, info)
	return info, nil
}

func (server *Server) GetTerminal(id string) (TerminalInfo, error) {
	return server.registry.get(id)
}

func (server *Server) ListTerminals() []TerminalInfo {
	return server.registry.list()
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
	server.publishTerminalEvent(EventTerminalChanged, info)
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
	return terminal.Restart(ctx, server.cfg.processFactory)
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

func (server *Server) LatestWindow(id string, cols, rows int) (history.HistoryWindow, error) {
	terminal, err := server.Terminal(id)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	return terminal.LatestWindow(cols, rows)
}

func (server *Server) OlderWindow(id string, cols, rows int, cursor history.HistoryCursor) (history.HistoryWindow, error) {
	terminal, err := server.Terminal(id)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	return terminal.OlderWindow(cols, rows, cursor)
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
		_ = terminal.Close()
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
	defer server.untrackTransport(conn)
	defer func() { _ = conn.Close() }()
	session := newProtocolSession(server, conn)
	if err := session.run(ctx); err != nil && !errors.Is(err, transport.ErrListenerClosed) {
		server.cfg.logger.Debug("core-v2 protocol session stopped", "error", err)
	}
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

func unixListenerFactory(socketPath string) (transport.Listener, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("core-v2 server: empty socket path")
	}
	return unixtransport.NewListener(socketPath)
}

func defaultSocketPath() string {
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return runtimeDir + "/termx-v2.sock"
	}
	return fmt.Sprintf("%s/termx-v2-%d.sock", os.TempDir(), os.Getuid())
}
