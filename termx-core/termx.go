package termx

import (
	"context"
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

	"github.com/lozzow/termx/termx-proto/wire"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-shared/perftrace"
	"github.com/lozzow/termx/termx-shared/terminalmeta"
	"github.com/lozzow/termx/termx-shared/transport"
	unixtransport "github.com/lozzow/termx/termx-shared/transport/unix"
	"github.com/lozzow/termx/termx-vterm/vterm"
)

const (
	snapshotResponseFrameBudget      = wire.MaxFrameSize - 64*1024
	defaultTerminalHistoryRetainRows = 12000
)

type ServerOption func(*serverConfig)

type serverConfig struct {
	socketPath                string
	defaultSize               Size
	defaultScrollback         int
	defaultScrollbackMaxBytes int64
	defaultScrollbackMaxAge   time.Duration
	defaultKeepAfterExit      time.Duration
	gridRoot                  string
	logger                    *slog.Logger
	methodHandler             ProtocolMethodHandler
	terminalObserver          TerminalInventoryObserver
}

type Server struct {
	cfg       serverConfig
	events    *EventBus
	mu        sync.RWMutex
	terminals map[string]*Terminal
	storage   *Storage
	closed    atomic.Bool
	listeners []transport.Listener

	// protocolListCache stores the encoded response for the wire-level
	// unfiltered "list" request. The Go API still returns fresh copies.
	protocolListCache        []byte
	protocolListCacheVersion uint64
}

func NewServer(opts ...ServerOption) *Server {
	cfg := serverConfig{
		socketPath:           defaultSocketPath(),
		defaultSize:          Size{Cols: 80, Rows: 24},
		defaultScrollback:    defaultTerminalHistoryRetainRows,
		defaultKeepAfterExit: 5 * time.Minute,
		logger:               slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	observeTerminalGridIDs(cfg.gridRoot)
	srv := &Server{
		cfg:       cfg,
		terminals: make(map[string]*Terminal),
		storage:   NewStorage(),
	}
	srv.events = NewEventBus(cfg.logger)
	return srv
}

func (s *Server) Storage() *Storage {
	if s == nil {
		return nil
	}
	return s.storage
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

func WithDefaultScrollbackMaxBytes(bytes int64) ServerOption {
	return func(cfg *serverConfig) {
		cfg.defaultScrollbackMaxBytes = bytes
	}
}

func WithDefaultScrollbackMaxAge(age time.Duration) ServerOption {
	return func(cfg *serverConfig) {
		cfg.defaultScrollbackMaxAge = age
	}
}

func WithDefaultKeepAfterExit(d time.Duration) ServerOption {
	return func(cfg *serverConfig) {
		cfg.defaultKeepAfterExit = d
	}
}

func WithGridRoot(path string) ServerOption {
	return func(cfg *serverConfig) {
		cfg.gridRoot = strings.TrimSpace(path)
	}
}

func WithLogger(logger *slog.Logger) ServerOption {
	return func(cfg *serverConfig) {
		if logger != nil {
			cfg.logger = logger
		}
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
	scrollbackMaxBytes := opts.ScrollbackMaxBytes
	if scrollbackMaxBytes <= 0 {
		scrollbackMaxBytes = s.cfg.defaultScrollbackMaxBytes
	}
	scrollbackMaxAge := opts.ScrollbackMaxAge
	if scrollbackMaxAge <= 0 {
		scrollbackMaxAge = s.cfg.defaultScrollbackMaxAge
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
		ScrollbackMaxBytes: scrollbackMaxBytes,
		ScrollbackMaxAge:   scrollbackMaxAge,
		KeepAfterExit:      keepAfterExit,
		GridRoot:           s.cfg.gridRoot,
		Logger:             s.cfg.logger,
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
	if err := removeTerminalGridStore(s.cfg.gridRoot, id); err != nil {
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
	if size, running := term.RunningSize(); running && size.Cols == cols && size.Rows == rows {
		return nil
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
	var opt SnapshotOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	term, err := s.getTerminal(id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		return s.gridSnapshotFromStore(id, opt)
	}
	return term.Snapshot(opt.ScrollbackOffset, opt.ScrollbackLimit), nil
}

func (s *Server) GridViewport(ctx context.Context, id string, opts ...GridViewportOptions) (*protocol.GridViewport, error) {
	_ = ctx
	var opt GridViewportOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	term, err := s.getTerminal(id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		return s.gridViewportFromStore(id, opt)
	}
	return protocolGridViewportFromCore(term.GridViewportWithOptions(opt)), nil
}

func (s *Server) HistoryReplay(ctx context.Context, id string, opts ...HistoryReplayOptions) (*HistoryReplayResult, error) {
	_ = ctx
	var opt HistoryReplayOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	term, err := s.getTerminal(id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		result, replayErr := s.gridReplayFromStore(id, opt)
		if replayErr != nil {
			return nil, replayErr
		}
		return result, nil
	}
	result := term.HistoryReplay(opt)
	return &result, nil
}

func (s *Server) gridReplayFromStore(id string, opt HistoryReplayOptions) (*HistoryReplayResult, error) {
	store, err := openTerminalGridStoreForReplay(s.cfg.gridRoot, id)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	beforeOffset, limit := sanitizeGridReplayWindow(opt.BeforeOffset, opt.Limit)
	replay, rows, hasMore, err := store.Replay(beforeOffset, limit)
	if err != nil {
		return nil, err
	}
	return &HistoryReplayResult{
		TerminalID:   id,
		BeforeOffset: beforeOffset,
		Limit:        limit,
		Rows:         rows,
		HasMore:      hasMore,
		Replay:       string(replay),
	}, nil
}

func (s *Server) gridViewportFromStore(id string, opt GridViewportOptions) (*protocol.GridViewport, error) {
	viewport, err := s.gridViewportCoreFromStore(id, opt)
	if err != nil {
		return nil, err
	}
	return protocolGridViewportFromCore(viewport), nil
}

func (s *Server) gridViewportCoreFromStore(id string, opt GridViewportOptions) (*GridViewport, error) {
	store, err := openTerminalGridStoreForReplay(s.cfg.gridRoot, id)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	beforeOffset, limit := sanitizeGridViewportWindow(opt.ScrollbackOffset, opt.ScrollbackLimit)
	cols := int(s.cfg.defaultSize.Cols)
	if cols <= 0 {
		cols = 80
	}
	result, err := store.Viewport(beforeOffset, limit, cols)
	if err != nil {
		return nil, err
	}
	return &GridViewport{
		TerminalID:             id,
		Size:                   Size{Cols: uint16(cols), Rows: s.cfg.defaultSize.Rows},
		Rows:                   convertRows(result.Rows),
		ScrollbackOffset:       beforeOffset,
		ScrollbackLimit:        limit,
		ScrollbackTotal:        result.TotalRows,
		ScrollbackLogicalTotal: result.LogicalTotal,
		ScrollbackHasMore:      result.HasMore,
		LoadedRows:             result.LoadedRows,
		HistoryGeneration:      result.Generation,
		FirstRowID:             result.FirstRowID,
		LastRowID:              result.LastRowID,
		ScrollbackTimestamps:   cloneTimeSlice(result.Timestamps),
		ScrollbackRowKinds:     cloneStringSlice(result.RowKinds),
		ScrollbackWrapped:      cloneBoolSlice(result.Wrapped),
		RowOwnership:           cloneStringSlice(result.Ownership),
		Timestamp:              time.Now().UTC(),
	}, nil
}

func (s *Server) historyGridViewport(ctx context.Context, id string, opt GridViewportOptions) (*GridViewport, error) {
	_ = ctx
	term, err := s.getTerminal(id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		return s.gridViewportCoreFromStore(id, opt)
	}
	return term.GridViewportWithOptions(opt), nil
}

func (s *Server) gridSnapshotFromStore(id string, opt SnapshotOptions) (*Snapshot, error) {
	store, err := openTerminalGridStoreForReplay(s.cfg.gridRoot, id)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	beforeOffset := opt.ScrollbackOffset
	if beforeOffset < 0 {
		beforeOffset = 0
	}
	limit := opt.ScrollbackLimit
	if limit < 0 {
		limit = 0
	}
	screenRowsWanted := int(s.cfg.defaultSize.Rows)
	if screenRowsWanted <= 0 {
		screenRowsWanted = 1
	}
	readLimit := screenRowsWanted + limit
	result, err := store.Viewport(beforeOffset, readLimit, int(s.cfg.defaultSize.Cols))
	if err != nil {
		return nil, err
	}
	scrollbackRows, screenRows := splitGridSnapshotRows(result.Rows, screenRowsWanted, int(s.cfg.defaultSize.Cols))
	metaRows := append([][]vterm.Cell(nil), scrollbackRows...)
	metaRows = append(metaRows, screenRows...)
	if limit == 0 {
		scrollbackRows = nil
	} else if len(scrollbackRows) > limit {
		scrollbackRows = scrollbackRows[len(scrollbackRows)-limit:]
	}
	scrollbackMetaRows := len(scrollbackRows)
	scrollbackTimestamps, screenTimestamps := splitGridSnapshotTimes(result.Timestamps, len(metaRows)-len(screenRows), len(screenRows))
	scrollbackRowKinds, screenRowKinds := splitGridSnapshotStrings(result.RowKinds, len(metaRows)-len(screenRows), len(screenRows))
	scrollbackWrapped, screenWrapped := splitGridSnapshotBools(result.Wrapped, len(metaRows)-len(screenRows), len(screenRows))
	scrollbackTimestamps = tailTimeSlice(scrollbackTimestamps, scrollbackMetaRows)
	scrollbackRowKinds = tailStringSlice(scrollbackRowKinds, scrollbackMetaRows)
	scrollbackWrapped = tailBoolSlice(scrollbackWrapped, scrollbackMetaRows)
	return &Snapshot{
		TerminalID: id,
		Size:       s.cfg.defaultSize,
		Screen: ScreenData{
			Cells:             convertRows(screenRows),
			IsAlternateScreen: false,
		},
		Scrollback:             convertRows(scrollbackRows),
		ScrollbackOffset:       beforeOffset,
		ScrollbackTotal:        result.TotalRows,
		ScrollbackLogicalTotal: result.LogicalTotal,
		ScrollbackHasMore:      result.HasMore,
		ScrollbackLoadedRows:   result.LoadedRows,
		HistoryGeneration:      result.Generation,
		ScrollbackFirstRowID:   result.FirstRowID,
		ScrollbackLastRowID:    result.LastRowID,
		ScreenTimestamps:       screenTimestamps,
		ScrollbackTimestamps:   scrollbackTimestamps,
		ScreenRowKinds:         screenRowKinds,
		ScrollbackRowKinds:     scrollbackRowKinds,
		ScreenWrapped:          screenWrapped,
		ScrollbackWrapped:      scrollbackWrapped,
		ScreenOwnership:        repeatedString(RowOwnershipScreen, len(screenRows)),
		ScrollbackOwnership:    repeatedString(RowOwnershipPersisted, len(scrollbackRows)),
		Cursor:                 CursorState{Visible: true},
		Modes:                  TerminalModes{AutoWrap: true},
		Timestamp:              time.Now().UTC(),
	}, nil
}

func splitGridSnapshotRows(rows [][]vterm.Cell, screenHeight int, cols int) ([][]vterm.Cell, [][]vterm.Cell) {
	if screenHeight <= 0 {
		screenHeight = 1
	}
	if cols <= 0 {
		cols = 80
	}
	screen := make([][]vterm.Cell, screenHeight)
	for i := range screen {
		screen[i] = make([]vterm.Cell, cols)
	}
	if len(rows) == 0 {
		return nil, screen
	}
	visibleStart := len(rows) - screenHeight
	if visibleStart < 0 {
		visibleStart = 0
	}
	visible := rows[visibleStart:]
	copy(screen[screenHeight-len(visible):], visible)
	return rows[:visibleStart], screen
}

func splitGridSnapshotTimes(values []time.Time, scrollbackRows int, screenRows int) ([]time.Time, []time.Time) {
	if len(values) == 0 {
		return nil, nil
	}
	values = alignTimeSliceTail(values, scrollbackRows+screenRows)
	return cloneTimeSlice(values[:scrollbackRows]), cloneTimeSlice(values[scrollbackRows:])
}

func splitGridSnapshotStrings(values []string, scrollbackRows int, screenRows int) ([]string, []string) {
	if len(values) == 0 {
		return nil, nil
	}
	values = alignStringSliceTail(values, scrollbackRows+screenRows)
	return cloneStringSlice(values[:scrollbackRows]), cloneStringSlice(values[scrollbackRows:])
}

func splitGridSnapshotBools(values []bool, scrollbackRows int, screenRows int) ([]bool, []bool) {
	if len(values) == 0 {
		return nil, nil
	}
	values = alignBoolSliceTail(values, scrollbackRows+screenRows)
	return cloneBoolSlice(values[:scrollbackRows]), cloneBoolSlice(values[scrollbackRows:])
}

func tailTimeSlice(values []time.Time, count int) []time.Time {
	if count <= 0 || len(values) == 0 {
		return nil
	}
	if count >= len(values) {
		return cloneTimeSlice(values)
	}
	return cloneTimeSlice(values[len(values)-count:])
}

func tailStringSlice(values []string, count int) []string {
	if count <= 0 || len(values) == 0 {
		return nil
	}
	if count >= len(values) {
		return cloneStringSlice(values)
	}
	return cloneStringSlice(values[len(values)-count:])
}

func tailBoolSlice(values []bool, count int) []bool {
	if count <= 0 || len(values) == 0 {
		return nil
	}
	if count >= len(values) {
		return cloneBoolSlice(values)
	}
	return cloneBoolSlice(values[len(values)-count:])
}

func alignTimeSliceTail(values []time.Time, size int) []time.Time {
	if size <= 0 {
		return nil
	}
	out := make([]time.Time, size)
	if len(values) > size {
		values = values[len(values)-size:]
	}
	copy(out[size-len(values):], values)
	return out
}

func alignStringSliceTail(values []string, size int) []string {
	if size <= 0 {
		return nil
	}
	out := make([]string, size)
	if len(values) > size {
		values = values[len(values)-size:]
	}
	copy(out[size-len(values):], values)
	return out
}

func alignBoolSliceTail(values []bool, size int) []bool {
	if size <= 0 {
		return nil
	}
	out := make([]bool, size)
	if len(values) > size {
		values = values[len(values)-size:]
	}
	copy(out[size-len(values):], values)
	return out
}

func (s *Server) Events(ctx context.Context, opts ...EventsOption) <-chan Event {
	return s.events.Subscribe(ctx, opts...)
}

func (s *Server) StorageGet(ctx context.Context, req StorageGetRequest) (StorageEntry, error) {
	_ = ctx
	if s == nil || s.storage == nil {
		return StorageEntry{}, ErrServerClosed
	}
	return s.storage.Get(req)
}

func (s *Server) StoragePut(ctx context.Context, req StoragePutRequest) (StorageEntry, error) {
	_ = ctx
	if s == nil || s.storage == nil {
		return StorageEntry{}, ErrServerClosed
	}
	entry, err := s.storage.Put(req)
	if err != nil {
		return StorageEntry{}, err
	}
	s.publishStorageEvent(entry, StorageOpPut)
	return entry, nil
}

func (s *Server) StorageDelete(ctx context.Context, req StorageDeleteRequest) (StorageDeleteResult, error) {
	_ = ctx
	if s == nil || s.storage == nil {
		return StorageDeleteResult{}, ErrServerClosed
	}
	result, err := s.storage.Delete(req)
	if err != nil {
		return StorageDeleteResult{}, err
	}
	if result.Deleted {
		s.publishStorageEvent(StorageEntry{
			AppID:   result.AppID,
			Scope:   result.Scope,
			OwnerID: result.OwnerID,
			Key:     result.Key,
			Version: result.Version,
		}, StorageOpDelete)
	}
	return result, nil
}

func (s *Server) StorageList(ctx context.Context, req StorageListRequest) ([]StorageEntry, error) {
	_ = ctx
	if s == nil || s.storage == nil {
		return nil, ErrServerClosed
	}
	return s.storage.List(req)
}

func (s *Server) publishStorageEvent(entry StorageEntry, op string) {
	if s == nil || s.events == nil {
		return
	}
	s.events.Publish(Event{
		Type:      EventStorageChanged,
		Timestamp: time.Now().UTC(),
		Storage: &StorageChangedData{
			AppID:   entry.AppID,
			Scope:   entry.Scope,
			OwnerID: entry.OwnerID,
			Key:     entry.Key,
			Version: entry.Version,
			Op:      op,
		},
	})
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

func (s *Server) protocolListResponse() ([]byte, error) {
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

	result := protocol.ListResult{Terminals: make([]protocol.TerminalInfo, 0, len(terms))}
	for _, term := range terms {
		item := term.protocolInfo()
		if item != nil {
			result.Terminals = append(result.Terminals, *item)
		}
	}
	payload, err := protocol.EncodeMethodResult("list", result)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	// Only publish the freshly encoded payload if the terminal set stayed
	// stable while we were building it; otherwise another request will rebuild.
	if s.protocolListCacheVersion == version && s.protocolListCache == nil {
		s.protocolListCache = payload
	}
	if cached := s.protocolListCache; cached != nil {
		payload = cached
	}
	s.mu.Unlock()
	return payload, nil
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
	surfaceID     string
	viewID        string
	mu            sync.RWMutex
	resizeControl protocol.ResizeControl
	streamPump    *attachmentStreamPump
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
	rxStats := newTransportFrameStats(s.cfg.logger, remote, "rx")
	txStats := newTransportFrameStats(s.cfg.logger, remote, "tx")
	defer func() {
		eventsCancelMu.Lock()
		if eventsCancel != nil {
			eventsCancel()
		}
		eventsCancelMu.Unlock()
		rxStats.flush("termx transport frame stats final")
		txStats.flush("termx transport frame stats final")
	}()

	var sendMu sync.Mutex
	sendFrame := func(channel uint16, typ uint8, payload []byte) error {
		frame, err := wire.EncodeFrame(channel, typ, payload)
		if err != nil {
			return err
		}
		sendMu.Lock()
		defer sendMu.Unlock()
		perftrace.Count("transport.bytes_over_wire", len(frame))
		if channel != 0 {
			perftrace.Count("transport.stream.bytes_over_wire", len(frame))
		}
		txStats.record(channel, typ, len(payload), len(frame))
		started := time.Now()
		err = t.Send(frame)
		if elapsed := time.Since(started); elapsed >= coreDiagnosticsSlowOperation && s.cfg.logger != nil {
			s.cfg.logger.Warn(
				"termx transport send slow",
				"remote", remote,
				"channel", channel,
				"type", protocolFrameTypeName(typ),
				"payload_bytes", len(payload),
				"frame_bytes", len(frame),
				"elapsed_ms", diagnosticDurationMillis(elapsed),
				"error", err,
			)
		}
		return err
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
		channel, typ, payload, err := wire.DecodeFrame(raw)
		if err != nil {
			s.cfg.logger.Warn("transport decode failed", "remote", remote, "error", err)
			return err
		}
		rxStats.record(channel, typ, len(payload), len(raw))

		if channel == 0 {
			switch typ {
			case wire.TypeHello:
				hello, err := protocol.DecodeHelloPayload(payload)
				if err != nil {
					return sendProtocolError(sendFrame, 0, 0, 400, err.Error())
				}
				s.cfg.logger.Debug("transport hello", "remote", remote, "client", hello.Client, "version", hello.Version)
				resp, err := protocol.EncodeHelloPayload(protocol.Hello{
					Version: wire.Version,
					Server:  "termx",
				})
				if err != nil {
					return err
				}
				if err := sendFrame(0, wire.TypeHello, resp); err != nil {
					return err
				}
			case wire.TypeRequest:
				req, err := protocol.DecodeRequestPayload(payload)
				if err != nil {
					if err := sendProtocolError(sendFrame, 0, 0, 400, err.Error()); err != nil {
						return err
					}
					continue
				}
				s.cfg.logger.Debug("transport request", "remote", remote, "method", req.Method, "id", req.ID)
				if err := scope.authorizeRequest(req); err != nil {
					s.cfg.logger.Warn("termx protocol request rejected", "remote", remote, "id", req.ID, "method", req.Method, "params_bytes", len(req.Params), "error", err)
					if err := sendProtocolError(sendFrame, req.ID, 0, protocolErrorCode(err), err.Error()); err != nil {
						return err
					}
					continue
				}
				var (
					result []byte
					code   int
				)
				requestStarted := time.Now()
				if s.cfg.logger != nil {
					s.cfg.logger.Info("termx protocol request started", "remote", remote, "id", req.ID, "method", req.Method, "params_bytes", len(req.Params))
				}
				if req.Method == "events" {
					result, code, err = s.handleEventsRequest(sessionCtx, remote, req, cancel, &eventsCancelMu, &eventsCancel, sendFrame)
				} else {
					result, code, err = s.handleRequest(sessionCtx, remote, nil, allocator, attachments, &attachmentsMu, scope, req, sendFrame)
				}
				perftrace.Count("protocol.request."+req.Method+".result_bytes", len(result))
				if err != nil {
					if s.cfg.logger != nil {
						s.cfg.logger.Warn(
							"termx protocol request failed",
							"remote", remote,
							"id", req.ID,
							"method", req.Method,
							"code", code,
							"params_bytes", len(req.Params),
							"elapsed_ms", diagnosticDurationMillis(time.Since(requestStarted)),
							"error", err,
						)
					}
					if err := sendProtocolError(sendFrame, req.ID, 0, code, err.Error()); err != nil {
						return err
					}
					continue
				}
				responseType := wire.TypeResponse
				var respPayload []byte
				if protocolResponseResultIsBinary(req.Method) {
					binaryEncodeFinish := perftrace.Measure("protocol.response.binary.encode." + req.Method)
					binaryPayload, err := protocol.EncodeBinaryResponsePayload(req.ID, result)
					binaryEncodeFinish(len(binaryPayload))
					if err != nil {
						return sendProtocolError(sendFrame, req.ID, 0, 500, err.Error())
					}
					respPayload = binaryPayload
					responseType = wire.TypeResponseBinary
					perftrace.Count("protocol.response.binary.bytes", len(respPayload))
				} else {
					var err error
					respPayload, err = protocol.EncodeResponsePayload(protocol.Response{ID: req.ID, Result: result})
					if err != nil {
						return sendProtocolError(sendFrame, req.ID, 0, 500, err.Error())
					}
				}
				if s.cfg.logger != nil {
					elapsed := time.Since(requestStarted)
					level := slog.LevelInfo
					if elapsed >= coreDiagnosticsSlowOperation || len(result) >= coreDiagnosticsLargePayloadBytes || len(respPayload) >= coreDiagnosticsLargePayloadBytes {
						level = slog.LevelWarn
					}
					s.cfg.logger.Log(
						sessionCtx,
						level,
						"termx protocol request completed",
						"remote", remote,
						"id", req.ID,
						"method", req.Method,
						"params_bytes", len(req.Params),
						"result_bytes", len(result),
						"response_bytes", len(respPayload),
						"elapsed_ms", diagnosticDurationMillis(elapsed),
					)
				}
				if len(respPayload) > wire.MaxFrameSize {
					if s.cfg.logger != nil {
						s.cfg.logger.Warn(
							"termx protocol response too large",
							"remote", remote,
							"id", req.ID,
							"method", req.Method,
							"response_bytes", len(respPayload),
							"max_frame_bytes", wire.MaxFrameSize,
						)
					}
					if err := sendProtocolError(sendFrame, req.ID, 0, 413, "protocol response too large"); err != nil {
						return err
					}
					continue
				}
				if err := sendFrame(0, responseType, respPayload); err != nil {
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
			if typ == wire.TypeInput || typ == wire.TypeResize {
				if err := sendProtocolError(sendFrame, 0, channel, 404, fmt.Sprintf("terminal attachment channel %d is not attached", channel)); err != nil {
					return err
				}
			}
			continue
		}
		if typ == wire.TypeStreamReady {
			screenSequence, err := wire.DecodeStreamReadyPayload(payload)
			if err != nil {
				s.cfg.logger.Warn("transport stream ready rejected for malformed payload", "remote", remote, "terminal_id", attachment.terminalID, "channel", channel, "error", err)
				continue
			}
			attachment.streamReady(screenSequence)
			continue
		}
		if attachment.mode() != ModeCollaborator {
			s.cfg.logger.Warn("transport stream frame rejected for readonly attachment", "remote", remote, "terminal_id", attachment.terminalID, "channel", channel, "type", typ, "mode", attachment.mode())
			if typ == wire.TypeInput || typ == wire.TypeResize {
				if err := sendProtocolError(sendFrame, 0, channel, 403, fmt.Sprintf("terminal attachment channel %d is readonly", channel)); err != nil {
					return err
				}
			}
			continue
		}
		switch typ {
		case wire.TypeInput:
			if len(payload) >= coreDiagnosticsLargePayloadBytes && s.cfg.logger != nil {
				s.cfg.logger.Warn("termx transport large input frame", "remote", remote, "terminal_id", attachment.terminalID, "channel", channel, "bytes", len(payload))
			}
			if err := s.WriteInput(sessionCtx, attachment.terminalID, payload); err != nil && !errors.Is(err, ErrTerminalExited) {
				s.cfg.logger.Warn("transport input failed", "remote", remote, "terminal_id", attachment.terminalID, "error", err)
				return err
			}
		case wire.TypeResize:
			if !attachment.canResize() {
				continue
			}
			if len(payload) != 4 {
				continue
			}
			cols, rows, err := wire.DecodeResizePayload(payload)
			if err != nil {
				continue
			}
			if s.cfg.logger != nil {
				s.cfg.logger.Info("termx transport resize frame", "remote", remote, "terminal_id", attachment.terminalID, "channel", channel, "cols", cols, "rows", rows)
			}
			if err := s.Resize(sessionCtx, attachment.terminalID, cols, rows); err != nil &&
				!errors.Is(err, ErrTerminalExited) &&
				!errors.Is(err, ErrPermissionDenied) {
				s.cfg.logger.Warn("transport resize failed", "remote", remote, "terminal_id", attachment.terminalID, "error", err)
				return err
			}
		case wire.TypeHistoryRequest:
			beforeOffset, limit, err := wire.DecodeHistoryRequestPayload(payload)
			if err != nil {
				if err := sendProtocolError(sendFrame, 0, channel, 400, err.Error()); err != nil {
					return err
				}
				continue
			}
			viewport, err := s.historyGridViewport(sessionCtx, attachment.terminalID, GridViewportOptions{
				ScrollbackOffset: beforeOffset,
				ScrollbackLimit:  limit,
				Alternate:        len(payload) >= 9 && payload[8] == 1,
			})
			if err != nil {
				if err := sendProtocolError(sendFrame, 0, channel, protocolErrorCode(err), err.Error()); err != nil {
					return err
				}
				continue
			}
			viewportPayload, err := protocol.EncodeGridViewportPayload(protocolGridViewportFromCore(viewport))
			if err != nil {
				return err
			}
			loadedRows := 0
			hasMore := false
			if viewport != nil {
				loadedRows = viewport.LoadedRows
				if loadedRows <= 0 {
					loadedRows = len(viewport.Rows)
				}
				hasMore = viewport.ScrollbackHasMore
			}
			replayPayload, err := wire.EncodeHistoryReplayPayload(loadedRows, hasMore, viewportPayload)
			if err != nil {
				return err
			}
			if s.cfg.logger != nil {
				s.cfg.logger.Info(
					"termx grid history viewport served",
					"remote", remote,
					"terminal_id", attachment.terminalID,
					"channel", channel,
					"before_offset", beforeOffset,
					"limit", limit,
					"rows", loadedRows,
					"has_more", hasMore,
					"viewport_bytes", len(viewportPayload),
				)
			}
			if err := sendFrame(channel, wire.TypeHistoryReplay, replayPayload); err != nil {
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
		decoded, err := protocol.DecodeMethodParams(req.Method, req.Params)
		if err != nil {
			return err
		}
		params := decoded.(protocol.EventsParams)
		if strings.TrimSpace(params.TerminalID) != "" {
			return fmt.Errorf("%w: machine inventory events must not set terminal_id", ErrPermissionDenied)
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
	if strings.HasPrefix(req.Method, "storage.") {
		return scopedTransportDenied(req.Method, terminalID)
	}
	switch req.Method {
	case "get":
		params, err := decodeProtocolParams[protocol.GetParams](req)
		if err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "resize":
		params, err := decodeProtocolParams[protocol.ResizeParams](req)
		if err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "ensure_resize":
		params, err := decodeProtocolParams[protocol.EnsureResizeParams](req)
		if err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "snapshot":
		params, err := decodeProtocolParams[protocol.SnapshotParams](req)
		if err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "attach":
		params, err := decodeProtocolParams[protocol.AttachParams](req)
		if err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "detach":
		params, err := decodeProtocolParams[protocol.DetachParams](req)
		if err != nil {
			return err
		}
		return authorizeScopedTerminal(req.Method, params.TerminalID, terminalID)
	case "events":
		params, err := decodeProtocolParams[protocol.EventsParams](req)
		if err != nil {
			return err
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

func decodeProtocolParams[T any](req protocol.Request) (T, error) {
	var zero T
	decoded, err := protocol.DecodeMethodParams(req.Method, req.Params)
	if err != nil {
		return zero, err
	}
	params, ok := decoded.(T)
	if !ok {
		return zero, fmt.Errorf("protocol params for method %q decoded as %T", req.Method, decoded)
	}
	return params, nil
}

func emptyProtocolResult() []byte {
	payload, _ := protocol.EncodeMethodResult("", nil)
	return payload
}

func protocolEventFromEvent(evt Event) protocol.Event {
	out := protocol.Event{
		Type:       protocol.EventType(evt.Type),
		TerminalID: evt.TerminalID,
		Timestamp:  evt.Timestamp,
	}
	if evt.Created != nil {
		out.Created = &protocol.TerminalCreatedData{
			Name:    evt.Created.Name,
			Command: append([]string(nil), evt.Created.Command...),
			Size:    protocol.Size{Cols: evt.Created.Size.Cols, Rows: evt.Created.Size.Rows},
		}
	}
	if evt.StateChanged != nil {
		out.StateChanged = &protocol.TerminalStateChangedData{
			OldState: string(evt.StateChanged.OldState),
			NewState: string(evt.StateChanged.NewState),
			ExitCode: copyIntPtr(evt.StateChanged.ExitCode),
		}
	}
	if evt.Resized != nil {
		out.Resized = &protocol.TerminalResizedData{
			OldSize: protocol.Size{Cols: evt.Resized.OldSize.Cols, Rows: evt.Resized.OldSize.Rows},
			NewSize: protocol.Size{Cols: evt.Resized.NewSize.Cols, Rows: evt.Resized.NewSize.Rows},
		}
	}
	if evt.Removed != nil {
		out.Removed = &protocol.TerminalRemovedData{Reason: evt.Removed.Reason}
	}
	if evt.CollaboratorsRevoked != nil {
		out.CollaboratorsRevoked = &protocol.CollaboratorsRevokedData{}
	}
	if evt.ReadError != nil {
		out.ReadError = &protocol.TerminalReadErrorData{Error: evt.ReadError.Error}
	}
	if evt.Storage != nil {
		out.Storage = &protocol.StorageChangedData{
			AppID:   evt.Storage.AppID,
			Scope:   protocol.StorageScope(evt.Storage.Scope),
			OwnerID: evt.Storage.OwnerID,
			Key:     evt.Storage.Key,
			Version: evt.Storage.Version,
			Op:      evt.Storage.Op,
		}
	}
	return out
}

func storageOwnerID(scope protocol.StorageScope, ownerID string, remote string) string {
	return storageOwnerIDForScope(StorageScope(scope), ownerID, remote)
}

func storageOwnerIDForScope(scope StorageScope, ownerID string, remote string) string {
	if scope != StorageScopePrivate {
		return ""
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID != "" {
		return ownerID
	}
	return strings.TrimSpace(remote)
}

func storageGetRequestFromProtocol(params protocol.StorageGetParams, remote string) StorageGetRequest {
	return StorageGetRequest{
		AppID:   params.AppID,
		Scope:   StorageScope(params.Scope),
		OwnerID: storageOwnerID(params.Scope, params.OwnerID, remote),
		Key:     params.Key,
	}
}

func storagePutRequestFromProtocol(params protocol.StoragePutParams, remote string) StoragePutRequest {
	return StoragePutRequest{
		AppID:           params.AppID,
		Scope:           StorageScope(params.Scope),
		OwnerID:         storageOwnerID(params.Scope, params.OwnerID, remote),
		Key:             params.Key,
		Value:           append([]byte(nil), params.Value...),
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	}
}

func storageDeleteRequestFromProtocol(params protocol.StorageDeleteParams, remote string) StorageDeleteRequest {
	return StorageDeleteRequest{
		AppID:           params.AppID,
		Scope:           StorageScope(params.Scope),
		OwnerID:         storageOwnerID(params.Scope, params.OwnerID, remote),
		Key:             params.Key,
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	}
}

func storageListRequestFromProtocol(params protocol.StorageListParams, remote string) StorageListRequest {
	return StorageListRequest{
		AppID:   params.AppID,
		Scope:   StorageScope(params.Scope),
		OwnerID: storageOwnerID(params.Scope, params.OwnerID, remote),
		Prefix:  params.Prefix,
	}
}

func protocolStorageEntryFromCore(entry StorageEntry) protocol.StorageEntry {
	return protocol.StorageEntry{
		AppID:     entry.AppID,
		Scope:     protocol.StorageScope(entry.Scope),
		OwnerID:   entry.OwnerID,
		Key:       entry.Key,
		Value:     append([]byte(nil), entry.Value...),
		Version:   entry.Version,
		UpdatedAt: entry.UpdatedAt,
	}
}

func protocolStorageEntriesFromCore(entries []StorageEntry) []protocol.StorageEntry {
	out := make([]protocol.StorageEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, protocolStorageEntryFromCore(entry))
	}
	return out
}

func protocolStorageDeleteResultFromCore(result StorageDeleteResult) protocol.StorageDeleteResult {
	return protocol.StorageDeleteResult{
		AppID:   result.AppID,
		Scope:   protocol.StorageScope(result.Scope),
		OwnerID: result.OwnerID,
		Key:     result.Key,
		Deleted: result.Deleted,
		Version: result.Version,
	}
}

func protocolResponseResultIsBinary(method string) bool {
	switch method {
	case "snapshot", "grid.viewport":
		return true
	default:
		return false
	}
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
) ([]byte, int, error) {
	started := time.Now()
	if s.cfg.logger != nil {
		defer func() {
			elapsed := time.Since(started)
			if elapsed >= coreDiagnosticsSlowOperation {
				s.cfg.logger.Warn("termx protocol handler slow", "remote", remote, "id", req.ID, "method", req.Method, "elapsed_ms", diagnosticDurationMillis(elapsed), "params_bytes", len(req.Params))
			}
		}()
	}
	if s.cfg.methodHandler != nil {
		if result, code, handled, err := s.cfg.methodHandler.HandleProtocolMethod(ctx, req.Method, req.Params); handled {
			return result, code, err
		}
	}
	switch req.Method {
	case "storage.get":
		params, err := decodeProtocolParams[protocol.StorageGetParams](req)
		if err != nil {
			return nil, 400, err
		}
		entry, err := s.StorageGet(ctx, storageGetRequestFromProtocol(params, remote))
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		result, err := protocol.EncodeMethodResult(req.Method, protocolStorageEntryFromCore(entry))
		if err != nil {
			return nil, 500, err
		}
		s.logProtocolMethodResult(ctx, remote, req, result, started, "app_id", entry.AppID, "scope", entry.Scope, "owner_id", entry.OwnerID, "key", entry.Key, "version", entry.Version)
		return result, 0, nil
	case "storage.put":
		params, err := decodeProtocolParams[protocol.StoragePutParams](req)
		if err != nil {
			return nil, 400, err
		}
		entry, err := s.StoragePut(ctx, storagePutRequestFromProtocol(params, remote))
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		result, err := protocol.EncodeMethodResult(req.Method, protocolStorageEntryFromCore(entry))
		if err != nil {
			return nil, 500, err
		}
		s.logProtocolMethodResult(ctx, remote, req, result, started, "app_id", entry.AppID, "scope", entry.Scope, "owner_id", entry.OwnerID, "key", entry.Key, "version", entry.Version, "value_bytes", len(entry.Value))
		return result, 0, nil
	case "storage.delete":
		params, err := decodeProtocolParams[protocol.StorageDeleteParams](req)
		if err != nil {
			return nil, 400, err
		}
		deleted, err := s.StorageDelete(ctx, storageDeleteRequestFromProtocol(params, remote))
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		result, err := protocol.EncodeMethodResult(req.Method, protocolStorageDeleteResultFromCore(deleted))
		if err != nil {
			return nil, 500, err
		}
		s.logProtocolMethodResult(ctx, remote, req, result, started, "app_id", deleted.AppID, "scope", deleted.Scope, "owner_id", deleted.OwnerID, "key", deleted.Key, "deleted", deleted.Deleted, "version", deleted.Version)
		return result, 0, nil
	case "storage.list":
		params, err := decodeProtocolParams[protocol.StorageListParams](req)
		if err != nil {
			return nil, 400, err
		}
		entries, err := s.StorageList(ctx, storageListRequestFromProtocol(params, remote))
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		result, err := protocol.EncodeMethodResult(req.Method, protocol.StorageListResult{Entries: protocolStorageEntriesFromCore(entries)})
		if err != nil {
			return nil, 500, err
		}
		s.logProtocolMethodResult(ctx, remote, req, result, started, "app_id", params.AppID, "scope", params.Scope, "owner_id", storageOwnerID(params.Scope, params.OwnerID, remote), "prefix", params.Prefix, "entries", len(entries))
		return result, 0, nil
	case "create":
		params, err := decodeProtocolParams[protocol.CreateParams](req)
		if err != nil {
			return nil, 400, err
		}
		info, err := s.Create(ctx, CreateOptions{
			Command:            params.Command,
			ID:                 params.ID,
			Name:               params.Name,
			Tags:               params.Tags,
			Size:               Size{Cols: params.Size.Cols, Rows: params.Size.Rows},
			Dir:                params.Dir,
			Env:                params.Env,
			ScrollbackSize:     params.ScrollbackSize,
			ScrollbackMaxBytes: params.ScrollbackMaxBytes,
			ScrollbackMaxAge:   params.ScrollbackMaxAge,
		})
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		result, err := protocol.EncodeMethodResult(req.Method, protocol.CreateResult{TerminalID: info.ID, State: string(info.State)})
		if err != nil {
			return nil, 500, err
		}
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", info.ID)
		return result, 0, nil
	case "list":
		result, err := s.protocolListResponse()
		if err != nil {
			return nil, 500, err
		}
		s.logProtocolMethodResult(ctx, remote, req, result, started)
		return result, 0, nil
	case "get":
		params, err := decodeProtocolParams[protocol.GetParams](req)
		if err != nil {
			return nil, 400, err
		}
		term, err := s.getTerminal(params.TerminalID)
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		result, err := protocol.EncodeMethodResult(req.Method, term.protocolInfo())
		if err != nil {
			return nil, 500, err
		}
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", params.TerminalID)
		return result, 0, nil
	case "kill":
		params, err := decodeProtocolParams[protocol.GetParams](req)
		if err != nil {
			return nil, 400, err
		}
		if err := requireControlPermission(attachments, attachmentsMu, params.TerminalID); err != nil {
			return nil, protocolErrorCode(err), err
		}
		if err := s.Kill(ctx, params.TerminalID); err != nil {
			return nil, protocolErrorCode(err), err
		}
		result := emptyProtocolResult()
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", params.TerminalID)
		return result, 0, nil
	case "restart":
		params, err := decodeProtocolParams[protocol.GetParams](req)
		if err != nil {
			return nil, 400, err
		}
		if err := requireControlPermission(attachments, attachmentsMu, params.TerminalID); err != nil {
			return nil, protocolErrorCode(err), err
		}
		if err := s.Restart(ctx, params.TerminalID); err != nil {
			return nil, protocolErrorCode(err), err
		}
		result := emptyProtocolResult()
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", params.TerminalID)
		return result, 0, nil
	case "remove":
		params, err := decodeProtocolParams[protocol.GetParams](req)
		if err != nil {
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
		if err := removeTerminalGridStore(s.cfg.gridRoot, params.TerminalID); err != nil {
			return nil, 500, err
		}
		s.removeTerminal(params.TerminalID, "removed")
		result := emptyProtocolResult()
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", params.TerminalID, "cleaned_attachments", len(toCleanup))
		return result, 0, nil
	case "resize":
		params, err := decodeProtocolParams[protocol.ResizeParams](req)
		if err != nil {
			return nil, 400, err
		}
		if err := requireResizePermission(attachments, attachmentsMu, params.TerminalID, scope); err != nil {
			return nil, protocolErrorCode(err), err
		}
		if err := s.Resize(ctx, params.TerminalID, params.Cols, params.Rows); err != nil {
			return nil, protocolErrorCode(err), err
		}
		result := emptyProtocolResult()
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", params.TerminalID, "cols", params.Cols, "rows", params.Rows)
		return result, 0, nil
	case "ensure_resize":
		params, err := decodeProtocolParams[protocol.EnsureResizeParams](req)
		if err != nil {
			return nil, 400, err
		}
		resizeResult, err := s.ensureResize(ctx, attachments, attachmentsMu, scope, params)
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		result, err := protocol.EncodeMethodResult(req.Method, resizeResult)
		if err != nil {
			return nil, 500, err
		}
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", params.TerminalID, "channel", params.Channel, "cols", params.Cols, "rows", params.Rows)
		return result, 0, nil
	case "set_tags":
		params, err := decodeProtocolParams[protocol.SetTagsParams](req)
		if err != nil {
			return nil, 400, err
		}
		if err := s.SetTags(ctx, params.TerminalID, params.Tags); err != nil {
			return nil, protocolErrorCode(err), err
		}
		result := emptyProtocolResult()
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", params.TerminalID)
		return result, 0, nil
	case "set_metadata":
		params, err := decodeProtocolParams[protocol.SetMetadataParams](req)
		if err != nil {
			return nil, 400, err
		}
		if err := s.SetMetadata(ctx, params.TerminalID, params.Name, params.Tags); err != nil {
			return nil, protocolErrorCode(err), err
		}
		result := emptyProtocolResult()
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", params.TerminalID)
		return result, 0, nil
	case "snapshot":
		params, err := decodeProtocolParams[protocol.SnapshotParams](req)
		if err != nil {
			return nil, 400, err
		}
		snap, err := s.Snapshot(ctx, params.TerminalID, SnapshotOptions{
			ScrollbackOffset: params.ScrollbackOffset,
			ScrollbackLimit:  params.ScrollbackLimit,
		})
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		encodeFinish := perftrace.Measure("protocol.snapshot.encode_binary")
		result, resultErr := protocol.EncodeSnapshotPayload(protocolSnapshotFromCore(snap))
		encodeFinish(len(result))
		snap, result = trimSnapshotBinaryResultToFrameBudget(snap, result, snapshotResponseFrameBudget)
		if resultErr != nil {
			return nil, 500, resultErr
		}
		s.logProtocolMethodResult(
			ctx,
			remote,
			req,
			result,
			started,
			"terminal_id", params.TerminalID,
			"scrollback_offset", params.ScrollbackOffset,
			"scrollback_limit", params.ScrollbackLimit,
			"snapshot_scrollback_rows", len(snap.Scrollback),
			"snapshot_screen_rows", len(snap.Screen.Cells),
		)
		return result, 0, nil
	case "grid.viewport":
		params, err := decodeProtocolParams[protocol.GridViewportParams](req)
		if err != nil {
			return nil, 400, err
		}
		viewport, err := s.GridViewport(ctx, params.TerminalID, GridViewportOptions{
			ScrollbackOffset: params.ScrollbackOffset,
			ScrollbackLimit:  params.ScrollbackLimit,
			Cols:             params.Cols,
		})
		if err != nil {
			return nil, protocolErrorCode(err), err
		}
		encodeFinish := perftrace.Measure("protocol.grid_viewport.encode_binary")
		result, resultErr := protocol.EncodeGridViewportPayload(viewport)
		encodeFinish(len(result))
		viewport, result = trimGridViewportBinaryResultToFrameBudget(viewport, result, snapshotResponseFrameBudget)
		if resultErr != nil {
			return nil, 500, resultErr
		}
		s.logProtocolMethodResult(
			ctx,
			remote,
			req,
			result,
			started,
			"terminal_id", params.TerminalID,
			"scrollback_offset", params.ScrollbackOffset,
			"scrollback_limit", params.ScrollbackLimit,
			"cols", params.Cols,
			"viewport_rows", len(viewport.Rows),
		)
		return result, 0, nil
	case "attach":
		params, err := decodeProtocolParams[protocol.AttachParams](req)
		if err != nil {
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
		attachmentID := fmt.Sprintf("%s:%d", remote, ch)
		surfaceID := normalizeResizeSurfaceID(params.SurfaceID, attachmentID)
		viewID := strings.TrimSpace(params.ViewID)
		resizeControl, err := terminalResizeControl(term, attachmentID, surfaceID, viewID, AttachMode(params.Mode), params.ResizePolicy)
		if err != nil {
			allocator.Free(ch)
			return nil, 400, err
		}
		subCtx, cancel := context.WithCancel(ctx)
		attachment := &sessionAttachment{
			terminal:      term,
			terminalID:    params.TerminalID,
			attachmentID:  attachmentID,
			surfaceID:     surfaceID,
			viewID:        viewID,
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
		term.AddAttachment(attachmentID, remote, AttachMode(params.Mode), surfaceID, viewID, resizeControl.CanResize)
		resizeControl = withResizeOwnership(resizeControl, term, surfaceID)
		attachment.setResizeControl(resizeControl)
		s.cfg.logger.Info("server attached terminal", "terminal_id", params.TerminalID, "remote", remote, "channel", ch, "mode", params.Mode, "surface_id", surfaceID, "resize_owner", resizeControl.CanResize)
		stream := term.SubscribeLatest(subCtx)
		pump := newAttachmentStreamPump(subCtx, cancel, params.TerminalID, ch, remote, stream, term.screenSnapshotFallbackMessage, term.currentScreenRevision, sendFrame, s.cfg.logger)
		attachment.setStreamPump(pump)
		go func() {
			defer attachment.cleanup()
			defer attachment.setStreamPump(nil)
			if err := pump.run(); err != nil {
				s.cfg.logger.Warn("transport attachment stream send failed", "terminal_id", params.TerminalID, "remote", remote, "channel", ch, "error", err)
			}
		}()
		result, err := protocol.EncodeMethodResult(req.Method, protocol.AttachResult{Mode: params.Mode, Channel: ch, ResizeControl: &resizeControl})
		if err != nil {
			return nil, 500, err
		}
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", params.TerminalID, "channel", ch, "resize_owner", resizeControl.CanResize)
		return result, 0, nil
	case "detach":
		params, err := decodeProtocolParams[protocol.DetachParams](req)
		if err != nil {
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
		result := emptyProtocolResult()
		s.logProtocolMethodResult(ctx, remote, req, result, started, "terminal_id", params.TerminalID, "detached_attachments", len(toCleanup))
		return result, 0, nil
	default:
		return nil, 400, fmt.Errorf("unsupported method: %s", req.Method)
	}
}

func (s *Server) logProtocolMethodResult(ctx context.Context, remote string, req protocol.Request, result []byte, started time.Time, attrs ...any) {
	if s == nil || s.cfg.logger == nil {
		return
	}
	elapsed := time.Since(started)
	level := slog.LevelDebug
	if elapsed >= coreDiagnosticsSlowOperation || len(result) >= coreDiagnosticsLargePayloadBytes {
		level = slog.LevelWarn
	}
	base := []any{
		"remote", remote,
		"id", req.ID,
		"method", req.Method,
		"params_bytes", len(req.Params),
		"result_bytes", len(result),
		"elapsed_ms", diagnosticDurationMillis(elapsed),
	}
	base = append(base, attrs...)
	s.cfg.logger.Log(ctx, level, "termx protocol method result", base...)
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
	if strings.TrimSpace(params.SurfaceID) != "" || strings.TrimSpace(params.ViewID) != "" {
		attachment.setResizeSurface(
			normalizeResizeSurfaceID(params.SurfaceID, attachment.attachmentID),
			strings.TrimSpace(params.ViewID),
		)
		term.SetAttachmentResizeSurface(attachment.attachmentID, attachment.surfaceIDValue(), attachment.viewIDValue())
	}
	control, err := terminalResizeControl(term, attachment.attachmentID, attachment.surfaceIDValue(), attachment.viewIDValue(), attachment.mode(), params.ResizePolicy)
	if err != nil {
		return protocol.EnsureResizeResult{}, err
	}
	attachment.setResizeControl(control)
	term.SetAttachmentResizeOwner(attachment.attachmentID, control.CanResize)
	control = withResizeOwnership(control, term, attachment.surfaceIDValue())
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
	remote string,
	req protocol.Request,
	cancelSession context.CancelFunc,
	eventsCancelMu *sync.Mutex,
	eventsCancel *context.CancelFunc,
	sendFrame func(uint16, uint8, []byte) error,
) ([]byte, int, error) {
	params, err := decodeProtocolParams[protocol.EventsParams](req)
	if err != nil {
		return nil, 400, err
	}

	opts := []EventsOption{WithStorageVisibility(remote)}
	if params.TerminalID != "" {
		opts = append(opts, WithTerminalFilter(params.TerminalID))
	}
	if len(params.Types) > 0 {
		types := make([]EventType, len(params.Types))
		for i, typ := range params.Types {
			types[i] = EventType(typ)
		}
		opts = append(opts, WithTypeFilter(types...))
	}
	if params.StorageAppID != "" || params.StorageScope != "" || params.StorageOwnerID != "" || params.StorageKeyPrefix != "" {
		opts = append(opts, WithStorageFilter(
			params.StorageAppID,
			StorageScope(params.StorageScope),
			storageOwnerIDForScope(StorageScope(params.StorageScope), params.StorageOwnerID, remote),
			params.StorageKeyPrefix,
		))
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
			payload, err := protocol.EncodeEventPayload(protocolEventFromEvent(evt))
			if err != nil {
				cancelSession()
				return
			}
			if err := sendFrame(0, wire.TypeEvent, payload); err != nil {
				cancelSession()
				return
			}
		}
	}()

	return emptyProtocolResult(), 0, nil
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
	return a.mode() == ModeCollaborator && a.terminal.AttachmentResizeOwner(a.attachmentID)
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

func (a *sessionAttachment) setStreamPump(pump *attachmentStreamPump) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.streamPump = pump
}

func (a *sessionAttachment) streamReady(screenSequence uint64) {
	if a == nil {
		return
	}
	a.mu.RLock()
	pump := a.streamPump
	a.mu.RUnlock()
	if pump != nil {
		pump.screenReady(screenSequence)
	}
}

func (a *sessionAttachment) setResizeSurface(surfaceID, viewID string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.surfaceID = strings.TrimSpace(surfaceID)
	a.viewID = strings.TrimSpace(viewID)
}

func (a *sessionAttachment) surfaceIDValue() string {
	if a == nil {
		return ""
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.surfaceID
}

func (a *sessionAttachment) viewIDValue() string {
	if a == nil {
		return ""
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.viewID
}

func terminalResizeControl(term *Terminal, attachmentID, surfaceID, viewID string, mode AttachMode, policy string) (protocol.ResizeControl, error) {
	trimmedPolicy := strings.TrimSpace(policy)
	if trimmedPolicy == "" {
		trimmedPolicy = protocol.ResizePolicyOwner
	}
	switch trimmedPolicy {
	case protocol.ResizePolicyOwner, protocol.ResizePolicyFollower:
	default:
		return protocol.ResizeControl{}, fmt.Errorf("%w: unsupported resize_policy %q", ErrInvalidCommand, policy)
	}
	surfaceID = normalizeResizeSurfaceID(surfaceID, attachmentID)
	if term != nil && term.SizeLocked() {
		return withResizeOwnership(protocol.ResizeControl{
			CanResize:  false,
			Reason:     protocol.ResizeControlReasonSizeLocked,
			SizeLocked: true,
			SurfaceID:  surfaceID,
		}, term, surfaceID), nil
	}
	if mode != ModeCollaborator {
		return withResizeOwnership(protocol.ResizeControl{
			CanResize: false,
			Reason:    protocol.ResizeControlReasonObserver,
			SurfaceID: surfaceID,
		}, term, surfaceID), nil
	}
	if trimmedPolicy == protocol.ResizePolicyFollower {
		return withResizeOwnership(protocol.ResizeControl{
			CanResize: false,
			Reason:    protocol.ResizeControlReasonFollower,
			SurfaceID: surfaceID,
		}, term, surfaceID), nil
	}
	return withResizeOwnership(protocol.ResizeControl{
		CanResize: true,
		Reason:    protocol.ResizeControlReasonOwner,
		SurfaceID: surfaceID,
	}, term, surfaceID), nil
}

func normalizeResizeSurfaceID(surfaceID, fallback string) string {
	if trimmed := strings.TrimSpace(surfaceID); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fallback)
}

func withResizeOwnership(control protocol.ResizeControl, term *Terminal, surfaceID string) protocol.ResizeControl {
	ownership := terminalProtocolResizeOwnership(term)
	control.SurfaceID = normalizeResizeSurfaceID(control.SurfaceID, surfaceID)
	control.ResizeOwnership = ownership
	if ownership != nil {
		control.OwnerSurfaceID = ownership.OwnerSurfaceID
		control.OwnerViewID = ownership.OwnerViewID
		control.SizeLocked = control.SizeLocked || ownership.SizeLocked
	}
	return control
}

func terminalProtocolResizeOwnership(term *Terminal) *protocol.ResizeOwnership {
	if term == nil {
		return nil
	}
	term.attachMu.Lock()
	ownership := term.resizeOwnershipSnapshotLocked()
	term.mu.RLock()
	term.attachMu.Unlock()
	ownership.Size = Size{Cols: term.size.Cols, Rows: term.size.Rows}
	ownership.SizeLocked = terminalmeta.SizeLocked(term.tags)
	if ownership.SizeLocked {
		ownership.OwnerAttachmentID = ""
		ownership.OwnerSurfaceID = ""
		ownership.OwnerViewID = ""
		ownership.OwnerRemoteAddr = ""
	}
	term.mu.RUnlock()
	return protocolResizeOwnership(ownership)
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
	ch, typ, payload, err := wire.DecodeFrame(frame)
	if err != nil {
		return err
	}
	return sendFrame(ch, typ, payload)
}

func sendProtocolError(sendFrame func(uint16, uint8, []byte) error, id uint64, channel uint16, code int, msg string) error {
	payload, _ := protocol.EncodeErrorPayload(protocol.ErrorMessage{
		ID: id,
		Error: protocol.ProtocolError{
			Code:    code,
			Message: msg,
		},
	})
	return sendFrame(channel, wire.TypeError, payload)
}

func protocolErrorCode(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return 404
	case errors.Is(err, ErrDuplicateID), errors.Is(err, ErrDuplicateName), isStorageConflict(err):
		return 409
	case errors.Is(err, ErrPermissionDenied):
		return 403
	case errors.Is(err, ErrInvalidCommand), errors.Is(err, ErrTerminalExited), errors.Is(err, ErrTerminalNotExited):
		return 400
	default:
		return 500
	}
}
