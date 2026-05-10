package termx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/termx-core/transport"
	unixtransport "github.com/lozzow/termx/termx-core/transport/unix"
	"github.com/lozzow/termx/termx-core/workbenchsvc"
)

type ServerOption func(*serverConfig)

type serverConfig struct {
	socketPath           string
	defaultSize          Size
	defaultScrollback    int
	defaultKeepAfterExit time.Duration
	liveOutputThrottle   liveOutputThrottleConfig
	logger               *slog.Logger
	methodHandler        ProtocolMethodHandler
	terminalObserver     TerminalInventoryObserver
}

type Server struct {
	cfg            serverConfig
	events         *EventBus
	mu             sync.RWMutex
	terminals      map[string]*Terminal
	workbench      *workbenchsvc.Service
	sessionHandler sessionRequestHandler
	closed         atomic.Bool
	listeners      []transport.Listener

	// protocolListCache stores the marshaled response for the wire-level
	// unfiltered "list" request. The Go API still returns fresh copies.
	protocolListCache        json.RawMessage
	protocolListCacheVersion uint64
}

func NewServer(opts ...ServerOption) *Server {
	cfg := serverConfig{
		socketPath:           defaultSocketPath(),
		defaultSize:          Size{Cols: 80, Rows: 24},
		defaultScrollback:    10000,
		defaultKeepAfterExit: 5 * time.Minute,
		liveOutputThrottle:   defaultLiveOutputThrottleConfig(),
		logger:               slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	srv := &Server{
		cfg:       cfg,
		terminals: make(map[string]*Terminal),
		workbench: workbenchsvc.New(),
	}
	srv.events = NewEventBus(cfg.logger)
	srv.sessionHandler = newSessionHandler(srv)
	return srv
}

func (s *Server) Workbench() *workbenchsvc.Service {
	if s == nil {
		return nil
	}
	return s.workbench
}

func WithSocketPath(path string) ServerOption {
	return func(cfg *serverConfig) {
		cfg.socketPath = path
	}
}

func WithDefaultSize(cols, rows uint16) ServerOption {
	return func(cfg *serverConfig) {
		cfg.defaultSize = Size{Cols: cols, Rows: rows}
	}
}

func WithDefaultScrollback(lines int) ServerOption {
	return func(cfg *serverConfig) {
		cfg.defaultScrollback = lines
	}
}

func WithDefaultKeepAfterExit(d time.Duration) ServerOption {
	return func(cfg *serverConfig) {
		cfg.defaultKeepAfterExit = d
	}
}

func WithLogger(logger *slog.Logger) ServerOption {
	return func(cfg *serverConfig) {
		if logger != nil {
			cfg.logger = logger
		}
	}
}

func WithLiveOutputFPS(fps int) ServerOption {
	return func(cfg *serverConfig) {
		cfg.liveOutputThrottle = sanitizeLiveOutputThrottleConfig(liveOutputThrottleConfig{FPS: fps})
	}
}

func (s *Server) Create(ctx context.Context, opts CreateOptions) (*TerminalInfo, error) {
	if s.closed.Load() {
		return nil, ErrServerClosed
	}
	if len(opts.Command) == 0 {
		return nil, ErrInvalidCommand
	}
	_ = ctx

	id := opts.ID
	if id == "" {
		var err error
		id, err = s.nextGeneratedTerminalID()
		if err != nil {
			return nil, err
		}
	} else {
		ObserveGeneratedID(id)
	}

	size := opts.Size
	if size.Cols == 0 || size.Rows == 0 {
		size = s.cfg.defaultSize
	}
	scrollback := opts.ScrollbackSize
	if scrollback <= 0 {
		scrollback = s.cfg.defaultScrollback
	}
	keepAfterExit := opts.KeepAfterExit
	if keepAfterExit <= 0 {
		keepAfterExit = s.cfg.defaultKeepAfterExit
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = id
	}
	s.cfg.logger.Info("server create terminal requested", "terminal_id", id, "name", name)

	s.mu.Lock()
	if _, exists := s.terminals[id]; exists {
		s.mu.Unlock()
		return nil, ErrDuplicateID
	}
	if s.terminalNameExistsLocked(name, "") {
		s.mu.Unlock()
		return nil, ErrDuplicateName
	}
	s.mu.Unlock()

	term, err := newTerminal(context.Background(), s.events, terminalConfig{
		ID:                 id,
		Name:               name,
		Command:            append([]string(nil), opts.Command...),
		Tags:               copyTags(opts.Tags),
		Size:               size,
		Dir:                opts.Dir,
		Env:                opts.Env,
		ScrollbackSize:     scrollback,
		KeepAfterExit:      keepAfterExit,
		LiveOutputThrottle: s.cfg.liveOutputThrottle,
		RemoveFunc:         s.removeTerminal,
		UpdateFunc:         s.invalidateProtocolListCache,
	})
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if _, exists := s.terminals[id]; exists {
		s.mu.Unlock()
		_ = term.Close()
		return nil, ErrDuplicateID
	}
	if s.terminalNameExistsLocked(name, "") {
		s.mu.Unlock()
		_ = term.Close()
		return nil, ErrDuplicateName
	}
	s.terminals[id] = term
	s.invalidateProtocolListCacheLocked()
	s.mu.Unlock()
	info := term.Info()
	s.notifyTerminalInventoryChanged()
	s.events.Publish(Event{
		Type:       EventTerminalCreated,
		TerminalID: info.ID,
		Timestamp:  time.Now().UTC(),
		Created: &TerminalCreatedData{
			Name:    info.Name,
			Command: append([]string(nil), info.Command...),
			Size:    info.Size,
		},
	})
	s.cfg.logger.Info("server created terminal", "terminal_id", id, "name", name)
	return info, nil
}

func (s *Server) Get(ctx context.Context, id string) (*TerminalInfo, error) {
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		return nil, err
	}
	return term.Info(), nil
}

func (s *Server) List(ctx context.Context, opts ...ListOptions) ([]*TerminalInfo, error) {
	_ = ctx
	var filter ListOptions
	if len(opts) > 0 {
		filter = opts[0]
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*TerminalInfo, 0, len(s.terminals))
	for _, term := range s.terminals {
		info, ok := term.listInfo(filter)
		if !ok {
			continue
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		return lessNumericString(out[i].ID, out[j].ID)
	})
	return out, nil
}

func (s *Server) Kill(ctx context.Context, id string) error {
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		return err
	}
	s.cfg.logger.Info("server kill terminal requested", "terminal_id", id)
	info := term.Info()
	if info.State == StateExited {
		s.removeTerminal(id, "killed")
		return nil
	}
	return term.Kill()
}

func (s *Server) Restart(ctx context.Context, id string) error {
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		return err
	}
	s.cfg.logger.Info("server restart terminal requested", "terminal_id", id)
	return term.Restart()
}

func (s *Server) Remove(ctx context.Context, id string) error {
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		return err
	}
	term.MarkRemoved()
	if err := term.Close(); err != nil {
		return err
	}
	s.removeTerminal(id, "removed")
	return nil
}

func (s *Server) SetTags(ctx context.Context, id string, tags map[string]string) error {
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		return err
	}
	term.SetTags(tags)
	s.invalidateProtocolListCache()
	return nil
}

func (s *Server) SetMetadata(ctx context.Context, id string, name string, tags map[string]string) error {
	_ = ctx
	s.mu.Lock()
	term, ok := s.terminals[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	currentName := strings.TrimSpace(term.Name())
	nextName := strings.TrimSpace(name)
	if nextName == "" {
		nextName = currentName
	}
	if nextName != currentName && s.terminalNameExistsLocked(nextName, id) {
		s.mu.Unlock()
		return ErrDuplicateName
	}
	term.SetMetadata(name, tags)
	s.invalidateProtocolListCacheLocked()
	s.mu.Unlock()
	s.notifyTerminalInventoryChanged()
	return nil
}

func (s *Server) WriteInput(ctx context.Context, id string, data []byte) error {
	finish := perftrace.Measure("server.input.write")
	defer func() {
		finish(len(data))
	}()
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		return err
	}
	return term.WriteInput(data)
}

func (s *Server) SendKeys(ctx context.Context, id string, keys ...string) error {
	for _, key := range keys {
		var data []byte
		switch key {
		case "Enter":
			data = []byte{'\n'}
		case "Tab":
			data = []byte{'\t'}
		case "Escape":
			data = []byte{0x1b}
		case "Ctrl-C":
			data = []byte{0x03}
		case "Ctrl-D":
			data = []byte{0x04}
		default:
			data = []byte(key)
		}
		if err := s.WriteInput(ctx, id, data); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Resize(ctx context.Context, id string, cols, rows uint16) error {
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		return err
	}
	if err := term.Resize(cols, rows); err != nil {
		return err
	}
	s.invalidateProtocolListCache()
	return nil
}

func (s *Server) Subscribe(ctx context.Context, id string) (<-chan StreamMessage, error) {
	term, err := s.getTerminal(id)
	if err != nil {
		return nil, err
	}
	return term.Subscribe(ctx), nil
}

func (s *Server) Snapshot(ctx context.Context, id string, opts ...SnapshotOptions) (*Snapshot, error) {
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		return nil, err
	}
	var opt SnapshotOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	return term.Snapshot(opt.ScrollbackOffset, opt.ScrollbackLimit), nil
}

func (s *Server) Events(ctx context.Context, opts ...EventsOption) <-chan Event {
	return s.events.Subscribe(ctx, opts...)
}

func (s *Server) RevokeCollaborators(ctx context.Context, id string) error {
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		return err
	}
	term.RevokeCollaborators()
	s.events.Publish(Event{
		Type:                 EventCollaboratorsRevoked,
		TerminalID:           id,
		Timestamp:            time.Now().UTC(),
		CollaboratorsRevoked: &CollaboratorsRevokedData{},
	})
	return nil
}

func (s *Server) Attached(ctx context.Context, id string) ([]AttachInfo, error) {
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		return nil, err
	}
	return term.Attached(), nil
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.closed.Load() {
		return ErrServerClosed
	}
	s.cfg.logger.Info("server listen starting", "socket_path", s.cfg.socketPath)
	listener, err := unixtransport.NewListener(s.cfg.socketPath)
	if err != nil {
		s.cfg.logger.Error("server listen failed", "socket_path", s.cfg.socketPath, "error", err)
		return err
	}
	s.listeners = append(s.listeners, listener)

	var wg sync.WaitGroup
	defer wg.Wait()
	defer listener.Close()

	for {
		select {
		case <-ctx.Done():
			return s.Shutdown(context.Background())
		default:
		}

		conn, err := listener.Accept(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, transport.ErrListenerClosed) {
				s.cfg.logger.Info("server listen stopping", "socket_path", s.cfg.socketPath)
				return nil
			}
			s.cfg.logger.Warn("server accept failed", "socket_path", s.cfg.socketPath, "error", err)
			continue
		}
		wg.Add(1)
		go func(c transport.Transport) {
			defer wg.Done()
			s.cfg.logger.Info("server accepted transport", "remote", listener.Addr())
			_ = s.handleTransport(ctx, c, listener.Addr())
		}(conn)
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	_ = ctx
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	for _, listener := range s.listeners {
		_ = listener.Close()
	}

	s.mu.RLock()
	terms := make([]*Terminal, 0, len(s.terminals))
	for _, term := range s.terminals {
		terms = append(terms, term)
	}
	s.mu.RUnlock()

	for _, term := range terms {
		_ = term.Close()
	}
	s.events.Close()
	return nil
}

func (s *Server) removeTerminal(id, reason string) {
	s.mu.Lock()
	if _, ok := s.terminals[id]; ok {
		delete(s.terminals, id)
	}
	s.invalidateProtocolListCacheLocked()
	s.mu.Unlock()
	s.notifyTerminalInventoryChanged()

	s.events.Publish(Event{
		Type:       EventTerminalRemoved,
		TerminalID: id,
		Timestamp:  time.Now().UTC(),
		Removed:    &TerminalRemovedData{Reason: reason},
	})
}

func (s *Server) getTerminal(id string) (*Terminal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	term, ok := s.terminals[id]
	if !ok {
		return nil, ErrNotFound
	}
	return term, nil
}

func (s *Server) invalidateProtocolListCache() {
	s.mu.Lock()
	s.invalidateProtocolListCacheLocked()
	s.mu.Unlock()
	s.notifyTerminalInventoryChanged()
}

func (s *Server) invalidateProtocolListCacheLocked() {
	s.protocolListCache = nil
	s.protocolListCacheVersion++
}

func (s *Server) terminalNameExistsLocked(name, exceptID string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for id, term := range s.terminals {
		if id == exceptID || term == nil {
			continue
		}
		if strings.TrimSpace(term.Name()) == name {
			return true
		}
	}
	return false
}

func (t *Terminal) listInfo(filter ListOptions) (*TerminalInfo, bool) {
	info, ok := t.listInfoSnapshot(filter)
	if !ok {
		return nil, false
	}
	// Copy the top-level struct so callers get distinct *TerminalInfo values
	// while the immutable nested metadata stays cached per terminal.
	snapshot := *info
	return &snapshot, true
}

func (s *Server) protocolListResponse() (json.RawMessage, error) {
	s.mu.RLock()
	if cached := s.protocolListCache; cached != nil {
		s.mu.RUnlock()
		return cached, nil
	}
	version := s.protocolListCacheVersion
	terms := make([]*Terminal, 0, len(s.terminals))
	for _, term := range s.terminals {
		terms = append(terms, term)
	}
	s.mu.RUnlock()
	sort.Slice(terms, func(i, j int) bool {
		return lessNumericString(terms[i].ID(), terms[j].ID())
	})

	var buf bytes.Buffer
	buf.WriteString(`{"terminals":[`)
	for i, term := range terms {
		if i > 0 {
			buf.WriteByte(',')
		}
		item, err := term.protocolInfoJSON()
		if err != nil {
			return nil, err
		}
		buf.Write(item)
	}
	buf.WriteString(`]}`)
	result := json.RawMessage(append([]byte(nil), buf.Bytes()...))

	s.mu.Lock()
	// Only publish the freshly marshaled payload if the terminal set stayed
	// stable while we were building it; otherwise another request will rebuild.
	if s.protocolListCacheVersion == version && s.protocolListCache == nil {
		s.protocolListCache = result
	}
	if cached := s.protocolListCache; cached != nil {
		result = cached
	}
	s.mu.Unlock()
	return result, nil
}

func (s *Server) nextGeneratedTerminalID() (string, error) {
	s.mu.RLock()
	existing := make([]string, 0, len(s.terminals))
	for id := range s.terminals {
		existing = append(existing, id)
	}
	s.mu.RUnlock()
	for _, id := range existing {
		ObserveGeneratedID(id)
	}
	for {
		id, err := GenerateID()
		if err != nil {
			return "", err
		}
		s.mu.RLock()
		_, exists := s.terminals[id]
		s.mu.RUnlock()
		if !exists {
			return id, nil
		}
	}
}

func lessNumericString(a, b string) bool {
	an, aok := parseNumericString(a)
	bn, bok := parseNumericString(b)
	if aok && bok {
		if an != bn {
			return an < bn
		}
	}
	if aok != bok {
		return aok
	}
	return a < b
}

func parseNumericString(raw string) (uint64, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}
	const maxUint64 = ^uint64(0)
	const maxBeforeMul10 = maxUint64 / 10
	const maxLastDigit = maxUint64 % 10

	var n uint64
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch < '0' || ch > '9' {
			return 0, false
		}
		digit := uint64(ch - '0')
		if n > maxBeforeMul10 || (n == maxBeforeMul10 && digit > maxLastDigit) {
			return 0, false
		}
		n = n*10 + digit
	}
	if n == 0 {
		return 0, false
	}
	return n, true
}

func matchTags(have, want map[string]string) bool {
	if len(want) == 0 {
		return true
	}
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

func defaultSocketPath() string {
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "termx.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("termx-%d.sock", os.Getuid()))
}

type sessionAttachment struct {
	terminal      *Terminal
	terminalID    string
	attachmentID  string
	mu            sync.RWMutex
	resizeControl protocol.ResizeControl
	cleanup       func()
}

type transportScope struct {
	TerminalID        string
	MachineEventsOnly bool
}

func (s *Server) handleTransport(ctx context.Context, t transport.Transport, remote string) error {
	return s.handleTransportScoped(ctx, t, remote, transportScope{})
}

func (s *Server) handleTransportScoped(ctx context.Context, t transport.Transport, remote string, scope transportScope) error {
	defer t.Close()
	s.cfg.logger.Info("transport session opened", "remote", remote)
	defer s.cfg.logger.Info("transport session closed", "remote", remote)

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sessionClosed := make(chan struct{})
	go func() {
		select {
		case <-sessionCtx.Done():
			_ = t.Close()
		case <-sessionClosed:
		}
	}()
	defer close(sessionClosed)

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	var eventsCancelMu sync.Mutex
	var eventsCancel context.CancelFunc
	defer func() {
		eventsCancelMu.Lock()
		if eventsCancel != nil {
			eventsCancel()
		}
		eventsCancelMu.Unlock()
	}()

	var sendMu sync.Mutex
	sendFrame := func(channel uint16, typ uint8, payload []byte) error {
		frame, err := protocol.EncodeFrame(channel, typ, payload)
		if err != nil {
			return err
		}
		sendMu.Lock()
		defer sendMu.Unlock()
		perftrace.Count("transport.bytes_over_wire", len(frame))
		if channel != 0 {
			perftrace.Count("transport.stream.bytes_over_wire", len(frame))
		}
		return t.Send(frame)
	}

	for {
		select {
		case <-sessionCtx.Done():
			return nil
		default:
		}

		raw, err := t.Recv()
		if err != nil {
			if sessionCtx.Err() != nil || errors.Is(err, io.EOF) {
				return nil
			}
			s.cfg.logger.Warn("transport recv failed", "remote", remote, "error", err)
			return err
		}
		channel, typ, payload, err := protocol.DecodeFrame(raw)
		if err != nil {
			s.cfg.logger.Warn("transport decode failed", "remote", remote, "error", err)
			return err
		}

		if channel == 0 {
			switch typ {
			case protocol.TypeHello:
				var hello protocol.Hello
				if err := json.Unmarshal(payload, &hello); err != nil {
					return sendProtocolError(sendFrame, 0, 0, 400, err.Error())
				}
				s.cfg.logger.Debug("transport hello", "remote", remote, "client", hello.Client, "version", hello.Version)
				resp, _ := json.Marshal(protocol.Hello{
					Version:      protocol.Version,
					Server:       "termx",
					Capabilities: []string{},
				})
				if err := sendFrame(0, protocol.TypeHello, resp); err != nil {
					return err
				}
			case protocol.TypeRequest:
				var req protocol.Request
				if err := json.Unmarshal(payload, &req); err != nil {
					if err := sendProtocolError(sendFrame, 0, 0, 400, err.Error()); err != nil {
						return err
					}
					continue
				}
				s.cfg.logger.Debug("transport request", "remote", remote, "method", req.Method, "id", req.ID)
				if err := scope.authorizeRequest(req); err != nil {
					if err := sendProtocolError(sendFrame, req.ID, 0, protocolErrorCode(err), err.Error()); err != nil {
						return err
					}
					continue
				}
				var (
					result json.RawMessage
					code   int
				)
				if req.Method == "events" {
					result, code, err = s.handleEventsRequest(sessionCtx, req, cancel, &eventsCancelMu, &eventsCancel, sendFrame)
				} else {
					result, code, err = s.handleRequest(sessionCtx, remote, nil, allocator, attachments, &attachmentsMu, scope, req, sendFrame)
				}
				if err != nil {
					if err := sendProtocolError(sendFrame, req.ID, 0, code, err.Error()); err != nil {
						return err
					}
					continue
				}
				respPayload, _ := json.Marshal(protocol.Response{ID: req.ID, Result: result})
				if err := sendFrame(0, protocol.TypeResponse, respPayload); err != nil {
					return err
				}
			}
			continue
		}

		attachmentsMu.RLock()
		attachment, ok := attachments[channel]
		attachmentsMu.RUnlock()
		if !ok {
			s.cfg.logger.Warn("transport stream frame for unknown attachment", "remote", remote, "channel", channel, "type", typ)
			if typ == protocol.TypeInput || typ == protocol.TypeResize {
				if err := sendProtocolError(sendFrame, 0, channel, 404, fmt.Sprintf("terminal attachment channel %d is not attached", channel)); err != nil {
					return err
				}
			}
			continue
		}
		if attachment.mode() != ModeCollaborator {
			s.cfg.logger.Warn("transport stream frame rejected for readonly attachment", "remote", remote, "terminal_id", attachment.terminalID, "channel", channel, "type", typ, "mode", attachment.mode())
			if typ == protocol.TypeInput || typ == protocol.TypeResize {
				if err := sendProtocolError(sendFrame, 0, channel, 403, fmt.Sprintf("terminal attachment channel %d is readonly", channel)); err != nil {
					return err
				}
			}
			continue
		}
		switch typ {
		case protocol.TypeInput:
			if err := s.WriteInput(sessionCtx, attachment.terminalID, payload); err != nil && !errors.Is(err, ErrTerminalExited) {
				s.cfg.logger.Warn("transport input failed", "remote", remote, "terminal_id", attachment.terminalID, "error", err)
				return err
			}
		case protocol.TypeResize:
			if !attachment.canResize() {
				continue
			}
			if len(payload) != 4 {
				continue
			}
			cols, rows, err := protocol.DecodeResizePayload(payload)
			if err != nil {
				continue
			}
			if err := s.Resize(sessionCtx, attachment.terminalID, cols, rows); err != nil &&
				!errors.Is(err, ErrTerminalExited) &&
				!errors.Is(err, ErrPermissionDenied) {
				s.cfg.logger.Warn("transport resize failed", "remote", remote, "terminal_id", attachment.terminalID, "error", err)
				return err
			}
		}
	}
}

func (s transportScope) authorizeRequest(req protocol.Request) error {
	terminalID := strings.TrimSpace(s.TerminalID)
	if s.MachineEventsOnly {
		if req.Method != "events" {
			return fmt.Errorf("%w: method %q is not authorized for machine inventory events", ErrPermissionDenied, req.Method)
		}
		var params protocol.EventsParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return err
			}
		}
		if strings.TrimSpace(params.TerminalID) != "" || strings.TrimSpace(params.SessionID) != "" {
			return fmt.Errorf("%w: machine inventory events must not set terminal_id or session_id", ErrPermissionDenied)
		}
		if len(params.Types) == 0 {
			return fmt.Errorf("%w: machine inventory events require an explicit terminal inventory type filter", ErrPermissionDenied)
		}
		for _, typ := range params.Types {
			switch EventType(typ) {
			case EventTerminalCreated, EventTerminalStateChanged, EventTerminalResized, EventTerminalRemoved, EventTerminalMetadataChanged:
			default:
				return fmt.Errorf("%w: event type %d is not authorized for machine inventory events", ErrPermissionDenied, typ)
			}
		}
		return nil
	}
	if terminalID == "" {
		return nil
	}
	if strings.HasPrefix(req.Method, "session.") {
		return scopedTransportDenied(req.Method, terminalID)
	}
	switch req.Method {
	case "get":
		var params protocol.GetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "resize":
		var params protocol.ResizeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "ensure_resize":
		var params protocol.EnsureResizeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "snapshot":
		var params protocol.SnapshotParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "attach":
		var params protocol.AttachParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "detach":
		var params protocol.DetachParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "events":
		var params protocol.EventsParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return err
			}
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	default:
		return scopedTransportDenied(req.Method, terminalID)
	}
}

func authorizeScopedTerminal(method string, requested string, allowed string) error {
	requested = strings.TrimSpace(requested)
	allowed = strings.TrimSpace(allowed)
	if requested == "" || requested != allowed {
		return scopedTransportDenied(method, allowed)
	}
	return nil
}

func scopedTransportDenied(method string, terminalID string) error {
	return fmt.Errorf("%w: method %q is not authorized for terminal %q", ErrPermissionDenied, method, terminalID)
}

func (s *Server) handleRequest(
	ctx context.Context,
	remote string,
	sessionCapabilities []string,
	allocator *protocol.ChannelAllocator,
	attachments map[uint16]*sessionAttachment,
	attachmentsMu *sync.RWMutex,
	scope transportScope,
	req protocol.Request,
	sendFrame func(uint16, uint8, []byte) error,
) (json.RawMessage, int, error) {
	if strings.HasPrefix(req.Method, "session.") {
		return s.handleSessionRequest(ctx, remote, req)
	}
	if s.cfg.methodHandler != nil {
		if result, code, handled, err := s.cfg.methodHandler.HandleProtocolMethod(ctx, req.Method, req.Params); handled {
			return result, code, err
		}
	}
	switch req.Method {
	case "create":
		var params protocol.CreateParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		info, err := s.Create(ctx, CreateOptions{
			Command:        params.Command,
			ID:             params.ID,
			Name:           params.Name,
			Tags:           params.Tags,
			Size:           Size{Cols: params.Size.Cols, Rows: params.Size.Rows},
			Dir:            params.Dir,
			Env:            params.Env,
			ScrollbackSize: params.ScrollbackSize,
		})
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		result, _ := json.Marshal(protocol.CreateResult{TerminalID: info.ID, State: string(info.State)})
		return result, 0, nil
	case "list":
		result, err := s.protocolListResponse()
		if err != nil {
			return nil, 500, err
		}
		return result, 0, nil
	case "get":
		var params protocol.GetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		term, err := s.getTerminal(params.TerminalID)
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		result, err := term.protocolInfoJSON()
		if err != nil {
			return nil, 500, err
		}
		return result, 0, nil
	case "kill":
		var params protocol.GetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		if err := requireControlPermission(attachments, attachmentsMu, params.TerminalID); err != nil {
			return nil, protocolErrorCode(err), err
		}
		if err := s.Kill(ctx, params.TerminalID); err != nil {
			return nil, protocolErrorCode(err), err
		}
		return json.RawMessage(`{}`), 0, nil
	case "restart":
		var params protocol.GetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		if err := requireControlPermission(attachments, attachmentsMu, params.TerminalID); err != nil {
			return nil, protocolErrorCode(err), err
		}
		if err := s.Restart(ctx, params.TerminalID); err != nil {
			return nil, protocolErrorCode(err), err
		}
		return json.RawMessage(`{}`), 0, nil
	case "remove":
		var params protocol.GetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		if err := requireControlPermission(attachments, attachmentsMu, params.TerminalID); err != nil {
			return nil, protocolErrorCode(err), err
		}
		term, err := s.getTerminal(params.TerminalID)
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		attachmentsMu.RLock()
		toCleanup := make([]*sessionAttachment, 0, len(attachments))
		for _, attachment := range attachments {
			if attachment == nil || attachment.terminalID != params.TerminalID {
				continue
			}
			toCleanup = append(toCleanup, attachment)
		}
		attachmentsMu.RUnlock()
		for _, attachment := range toCleanup {
			attachment.cleanup()
		}
		term.MarkRemoved()
		if err := term.Close(); err != nil {
			return nil, 500, err
		}
		s.removeTerminal(params.TerminalID, "removed")
		return json.RawMessage(`{}`), 0, nil
	case "resize":
		var params protocol.ResizeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		if err := requireResizePermission(attachments, attachmentsMu, params.TerminalID, scope); err != nil {
			return nil, protocolErrorCode(err), err
		}
		if err := s.Resize(ctx, params.TerminalID, params.Cols, params.Rows); err != nil {
			return nil, protocolErrorCode(err), err
		}
		return json.RawMessage(`{}`), 0, nil
	case "ensure_resize":
		var params protocol.EnsureResizeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		result, err := s.ensureResize(ctx, attachments, attachmentsMu, scope, params)
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		data, _ := json.Marshal(result)
		return data, 0, nil
	case "set_tags":
		var params protocol.SetTagsParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		if err := s.SetTags(ctx, params.TerminalID, params.Tags); err != nil {
			return nil, protocolErrorCode(err), err
		}
		return json.RawMessage(`{}`), 0, nil
	case "set_metadata":
		var params protocol.SetMetadataParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		if err := s.SetMetadata(ctx, params.TerminalID, params.Name, params.Tags); err != nil {
			return nil, protocolErrorCode(err), err
		}
		return json.RawMessage(`{}`), 0, nil
	case "snapshot":
		var params protocol.SnapshotParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		snap, err := s.Snapshot(ctx, params.TerminalID, SnapshotOptions{
			ScrollbackOffset: params.ScrollbackOffset,
			ScrollbackLimit:  params.ScrollbackLimit,
		})
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		result, _ := json.Marshal(snap)
		return result, 0, nil
	case "attach":
		var params protocol.AttachParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		term, err := s.getTerminal(params.TerminalID)
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		ch, err := allocator.Alloc()
		if err != nil {
			return nil, 500, err
		}
		resizeControl, err := terminalResizeControl(term, AttachMode(params.Mode), params.ResizePolicy)
		if err != nil {
			allocator.Free(ch)
			return nil, 400, err
		}
		subCtx, cancel := context.WithCancel(ctx)
		attachmentID := fmt.Sprintf("%s:%d", remote, ch)
		attachment := &sessionAttachment{
			terminal:      term,
			terminalID:    params.TerminalID,
			attachmentID:  attachmentID,
			resizeControl: resizeControl,
		}
		var cleanupOnce sync.Once
		attachment.cleanup = func() {
			cleanupOnce.Do(func() {
				cancel()
				term.RemoveAttachment(attachmentID)
				attachmentsMu.Lock()
				delete(attachments, ch)
				attachmentsMu.Unlock()
				allocator.Free(ch)
			})
		}
		attachmentsMu.Lock()
		attachments[ch] = attachment
		attachmentsMu.Unlock()
		term.AddAttachment(attachmentID, remote, AttachMode(params.Mode))
		s.cfg.logger.Info("server attached terminal", "terminal_id", params.TerminalID, "remote", remote, "channel", ch, "mode", params.Mode)
		stream := term.Subscribe(subCtx)
		go func() {
			defer attachment.cleanup()
			pump := newAttachmentStreamPump(subCtx, cancel, params.TerminalID, ch, stream, sendFrame)
			pump.run()
		}()
		result, _ := json.Marshal(protocol.AttachResult{Mode: params.Mode, Channel: ch, ResizeControl: &resizeControl})
		return result, 0, nil
	case "detach":
		var params protocol.DetachParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		attachmentsMu.RLock()
		toCleanup := make([]*sessionAttachment, 0, len(attachments))
		for _, attachment := range attachments {
			if attachment.terminalID == params.TerminalID {
				toCleanup = append(toCleanup, attachment)
			}
		}
		attachmentsMu.RUnlock()
		for _, attachment := range toCleanup {
			attachment.cleanup()
		}
		return json.RawMessage(`{}`), 0, nil
	default:
		return nil, 400, fmt.Errorf("unsupported method: %s", req.Method)
	}
}

func (s *Server) ensureResize(
	ctx context.Context,
	attachments map[uint16]*sessionAttachment,
	attachmentsMu *sync.RWMutex,
	scope transportScope,
	params protocol.EnsureResizeParams,
) (protocol.EnsureResizeResult, error) {
	term, err := s.getTerminal(params.TerminalID)
	if err != nil {
		return protocol.EnsureResizeResult{}, err
	}
	attachment, err := resizeAttachment(attachments, attachmentsMu, params.TerminalID, params.Channel, scope)
	if err != nil {
		return protocol.EnsureResizeResult{}, err
	}
	control, err := terminalResizeControl(term, attachment.mode(), params.ResizePolicy)
	if err != nil {
		return protocol.EnsureResizeResult{}, err
	}
	attachment.setResizeControl(control)
	size := term.Size()
	result := protocol.EnsureResizeResult{
		ResizeControl: &control,
		Size:          protocol.Size{Cols: size.Cols, Rows: size.Rows},
	}
	if !control.CanResize || params.Cols == 0 || params.Rows == 0 {
		return result, nil
	}
	if result.Size.Cols == params.Cols && result.Size.Rows == params.Rows {
		return result, nil
	}
	if err := s.Resize(ctx, params.TerminalID, params.Cols, params.Rows); err != nil {
		return protocol.EnsureResizeResult{}, err
	}
	result.Size = protocol.Size{Cols: params.Cols, Rows: params.Rows}
	result.Resized = true
	return result, nil
}

func (s *Server) handleEventsRequest(
	ctx context.Context,
	req protocol.Request,
	cancelSession context.CancelFunc,
	eventsCancelMu *sync.Mutex,
	eventsCancel *context.CancelFunc,
	sendFrame func(uint16, uint8, []byte) error,
) (json.RawMessage, int, error) {
	var params protocol.EventsParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
	}

	opts := make([]EventsOption, 0, 2)
	if params.TerminalID != "" {
		opts = append(opts, WithTerminalFilter(params.TerminalID))
	}
	if params.SessionID != "" {
		opts = append(opts, WithSessionFilter(params.SessionID))
	}
	if len(params.Types) > 0 {
		types := make([]EventType, len(params.Types))
		for i, typ := range params.Types {
			types[i] = EventType(typ)
		}
		opts = append(opts, WithTypeFilter(types...))
	}

	subCtx, subCancel := context.WithCancel(ctx)
	events, unsubscribe := s.events.subscribe(opts...)
	cancelEvents := func() {
		subCancel()
		unsubscribe()
	}
	go func() {
		<-subCtx.Done()
		unsubscribe()
	}()

	eventsCancelMu.Lock()
	if *eventsCancel != nil {
		(*eventsCancel)()
	}
	*eventsCancel = cancelEvents
	eventsCancelMu.Unlock()

	go func() {
		for evt := range events {
			payload, err := json.Marshal(evt)
			if err != nil {
				cancelSession()
				return
			}
			if err := sendFrame(0, protocol.TypeEvent, payload); err != nil {
				cancelSession()
				return
			}
		}
	}()

	return json.RawMessage(`{}`), 0, nil
}

func (a *sessionAttachment) mode() AttachMode {
	if a == nil || a.terminal == nil {
		return ModeObserver
	}
	mode, ok := a.terminal.AttachmentMode(a.attachmentID)
	if !ok {
		return ModeObserver
	}
	return mode
}

func (a *sessionAttachment) canResize() bool {
	if a == nil || !a.currentResizeControl().CanResize {
		return false
	}
	return a.mode() == ModeCollaborator
}

func (a *sessionAttachment) currentResizeControl() protocol.ResizeControl {
	if a == nil {
		return protocol.ResizeControl{}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.resizeControl
}

func (a *sessionAttachment) setResizeControl(control protocol.ResizeControl) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resizeControl = control
}

func terminalResizeControl(term *Terminal, mode AttachMode, policy string) (protocol.ResizeControl, error) {
	trimmedPolicy := strings.TrimSpace(policy)
	if trimmedPolicy == "" {
		trimmedPolicy = protocol.ResizePolicyOwner
	}
	switch trimmedPolicy {
	case protocol.ResizePolicyOwner, protocol.ResizePolicyFollower:
	default:
		return protocol.ResizeControl{}, fmt.Errorf("%w: unsupported resize_policy %q", ErrInvalidCommand, policy)
	}
	if term != nil && term.SizeLocked() {
		return protocol.ResizeControl{
			CanResize:  false,
			Reason:     protocol.ResizeControlReasonSizeLocked,
			SizeLocked: true,
		}, nil
	}
	if mode != ModeCollaborator {
		return protocol.ResizeControl{
			CanResize: false,
			Reason:    protocol.ResizeControlReasonObserver,
		}, nil
	}
	if trimmedPolicy == protocol.ResizePolicyFollower {
		return protocol.ResizeControl{
			CanResize: false,
			Reason:    protocol.ResizeControlReasonFollower,
		}, nil
	}
	return protocol.ResizeControl{
		CanResize: true,
		Reason:    protocol.ResizeControlReasonOwner,
	}, nil
}

func resizeAttachment(attachments map[uint16]*sessionAttachment, attachmentsMu *sync.RWMutex, terminalID string, channel uint16, scope transportScope) (*sessionAttachment, error) {
	if strings.TrimSpace(terminalID) == "" || attachmentsMu == nil {
		return nil, fmt.Errorf("%w: ensure resize requires terminal attachment", ErrPermissionDenied)
	}
	attachmentsMu.RLock()
	defer attachmentsMu.RUnlock()
	if channel != 0 {
		attachment := attachments[channel]
		if attachment == nil || attachment.terminalID != terminalID {
			return nil, fmt.Errorf("%w: attachment channel %d does not match terminal %q", ErrPermissionDenied, channel, terminalID)
		}
		return attachment, nil
	}
	var matched *sessionAttachment
	for _, attachment := range attachments {
		if attachment == nil || attachment.terminalID != terminalID {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("%w: ensure resize requires attachment channel for terminal %q", ErrPermissionDenied, terminalID)
		}
		matched = attachment
	}
	if matched != nil {
		return matched, nil
	}
	if strings.TrimSpace(scope.TerminalID) != "" || attachments != nil {
		return nil, fmt.Errorf("%w: scoped resize requires resize owner attachment for terminal %q", ErrPermissionDenied, terminalID)
	}
	return nil, fmt.Errorf("%w: ensure resize requires attachment for terminal %q", ErrPermissionDenied, terminalID)
}

func requireControlPermission(attachments map[uint16]*sessionAttachment, attachmentsMu *sync.RWMutex, terminalID string) error {
	if strings.TrimSpace(terminalID) == "" || attachmentsMu == nil {
		return nil
	}
	attachmentsMu.RLock()
	defer attachmentsMu.RUnlock()
	seen := false
	for _, attachment := range attachments {
		if attachment == nil || attachment.terminalID != terminalID {
			continue
		}
		seen = true
		if attachment.mode() == ModeCollaborator {
			return nil
		}
	}
	if seen {
		return fmt.Errorf("%w: observer/readonly attachments cannot kill/remove terminal %q", ErrPermissionDenied, terminalID)
	}
	return nil
}

func requireResizePermission(attachments map[uint16]*sessionAttachment, attachmentsMu *sync.RWMutex, terminalID string, scope transportScope) error {
	if strings.TrimSpace(terminalID) == "" || attachmentsMu == nil {
		return nil
	}
	attachmentsMu.RLock()
	defer attachmentsMu.RUnlock()
	seen := false
	for _, attachment := range attachments {
		if attachment == nil || attachment.terminalID != terminalID {
			continue
		}
		seen = true
		if attachment.canResize() {
			return nil
		}
	}
	if seen {
		return fmt.Errorf("%w: attachment does not own resize for terminal %q", ErrPermissionDenied, terminalID)
	}
	if strings.TrimSpace(scope.TerminalID) != "" || attachments != nil {
		return fmt.Errorf("%w: scoped resize requires resize owner attachment for terminal %q", ErrPermissionDenied, terminalID)
	}
	return nil
}

func sendRawFrame(sendFrame func(uint16, uint8, []byte) error, frame []byte) error {
	ch, typ, payload, err := protocol.DecodeFrame(frame)
	if err != nil {
		return err
	}
	return sendFrame(ch, typ, payload)
}

func sendProtocolError(sendFrame func(uint16, uint8, []byte) error, id uint64, channel uint16, code int, msg string) error {
	payload, _ := json.Marshal(protocol.ErrorMessage{
		ID: id,
		Error: protocol.ProtocolError{
			Code:    code,
			Message: msg,
		},
	})
	return sendFrame(channel, protocol.TypeError, payload)
}

func protocolErrorCode(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return 404
	case errors.Is(err, ErrDuplicateID), errors.Is(err, ErrDuplicateName):
		return 409
	case errors.Is(err, ErrPermissionDenied):
		return 403
	case errors.Is(err, ErrInvalidCommand), errors.Is(err, ErrTerminalExited), errors.Is(err, ErrTerminalNotExited):
		return 400
	default:
		return 500
	}
}
