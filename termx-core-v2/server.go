package termxcorev2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

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
	eventBuffer     int
}

type Server struct {
	cfg        serverConfig
	registry   *terminalRegistry
	events     *eventBroker
	closed     atomic.Bool
	mu         sync.Mutex
	listeners  []transport.Listener
	transports map[transport.Transport]struct{}
	wg         sync.WaitGroup
}

func NewServer(opts ...ServerOption) *Server {
	cfg := serverConfig{
		socketPath:      defaultSocketPath(),
		defaultSize:     Size{Cols: 80, Rows: 24},
		logger:          slog.Default(),
		listenerFactory: unixListenerFactory,
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
	return &Server{
		cfg:        cfg,
		registry:   newTerminalRegistry(),
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
	if server.closed.Load() {
		return TerminalInfo{}, ErrServerClosed
	}
	info, err := server.registry.register(record, server.cfg.defaultSize)
	if err != nil {
		return TerminalInfo{}, err
	}
	server.publishTerminalEvent(EventTerminalCreated, info)
	return info, nil
}

func (server *Server) GetTerminal(id string) (TerminalInfo, error) {
	return server.registry.get(id)
}

func (server *Server) ListTerminals() []TerminalInfo {
	return server.registry.list()
}

func (server *Server) RemoveTerminal(id string) error {
	if server.closed.Load() {
		return ErrServerClosed
	}
	info, err := server.registry.remove(id)
	if err != nil {
		return err
	}
	server.publishTerminalEvent(EventTerminalRemoved, info)
	return nil
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
	defer server.wg.Wait()
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
		server.trackTransport(conn)
		server.wg.Add(1)
		go server.handleTransport(ctx, conn)
	}
}

func (server *Server) Shutdown(ctx context.Context) error {
	_ = ctx
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
	server.registry.clear()
	server.events.publish(Event{Type: EventServerStopped, SocketPath: server.cfg.socketPath})
	server.events.close()
	server.wg.Wait()
	server.cfg.logger.Info("core-v2 server stopped", "socket_path", server.cfg.socketPath)
	return nil
}

func (server *Server) handleTransport(ctx context.Context, conn transport.Transport) {
	defer server.wg.Done()
	defer server.untrackTransport(conn)
	defer func() { _ = conn.Close() }()
	select {
	case <-ctx.Done():
	case <-conn.Done():
	}
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
