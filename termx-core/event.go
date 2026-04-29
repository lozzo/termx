package termx

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type EventType int

const (
	EventTerminalCreated EventType = iota + 1
	EventTerminalStateChanged
	EventTerminalResized
	EventTerminalRemoved
	EventCollaboratorsRevoked
	EventTerminalReadError
	EventSessionCreated
	EventSessionUpdated
	EventSessionDeleted
)

type TerminalCreatedData struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
	Size    Size     `json:"size"`
}

type TerminalStateChangedData struct {
	OldState TerminalState `json:"old_state"`
	NewState TerminalState `json:"new_state"`
	ExitCode *int          `json:"exit_code,omitempty"`
}

type TerminalResizedData struct {
	OldSize Size `json:"old_size"`
	NewSize Size `json:"new_size"`
}

type TerminalRemovedData struct {
	Reason string `json:"reason"`
}

type CollaboratorsRevokedData struct{}

type TerminalReadErrorData struct {
	Error string `json:"error"`
}

type SessionEventData struct {
	Revision uint64 `json:"revision,omitempty"`
	ViewID   string `json:"view_id,omitempty"`
}

type Event struct {
	Type                 EventType                 `json:"type"`
	TerminalID           string                    `json:"terminal_id"`
	SessionID            string                    `json:"session_id,omitempty"`
	Timestamp            time.Time                 `json:"timestamp"`
	Created              *TerminalCreatedData      `json:"created,omitempty"`
	StateChanged         *TerminalStateChangedData `json:"state_changed,omitempty"`
	Resized              *TerminalResizedData      `json:"resized,omitempty"`
	Removed              *TerminalRemovedData      `json:"removed,omitempty"`
	CollaboratorsRevoked *CollaboratorsRevokedData `json:"collaborators_revoked,omitempty"`
	ReadError            *TerminalReadErrorData    `json:"read_error,omitempty"`
	Session              *SessionEventData         `json:"session,omitempty"`
}

type EventsOption func(*eventsConfig)

type eventsConfig struct {
	terminalID string
	sessionID  string
	types      map[EventType]struct{}
}

func WithTerminalFilter(id string) EventsOption {
	return func(cfg *eventsConfig) {
		cfg.terminalID = id
	}
}

func WithTypeFilter(types ...EventType) EventsOption {
	return func(cfg *eventsConfig) {
		if cfg.types == nil {
			cfg.types = make(map[EventType]struct{}, len(types))
		}
		for _, typ := range types {
			cfg.types[typ] = struct{}{}
		}
	}
}

func WithSessionFilter(id string) EventsOption {
	return func(cfg *eventsConfig) {
		cfg.sessionID = id
	}
}

type EventBus struct {
	logger      *slog.Logger
	mu          sync.RWMutex
	subscribers map[*eventSubscriber]struct{}
}

type eventSubscriber struct {
	ch      chan Event
	cfg     eventsConfig
	dropped atomic.Int32
	closeMu sync.Once
}

func NewEventBus(logger *slog.Logger) *EventBus {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, nil))
	}
	return &EventBus{
		logger:      logger,
		subscribers: make(map[*eventSubscriber]struct{}),
	}
}

func (b *EventBus) Subscribe(ctx context.Context, opts ...EventsOption) <-chan Event {
	ch, cancel := b.subscribe(opts...)
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return ch
}

func (b *EventBus) subscribe(opts ...EventsOption) (<-chan Event, func()) {
	cfg := eventsConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	sub := &eventSubscriber{ch: make(chan Event, 64), cfg: cfg}

	b.mu.Lock()
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()

	return sub.ch, func() {
		b.removeSubscriber(sub)
	}
}

func (b *EventBus) removeSubscriber(sub *eventSubscriber) {
	if b == nil || sub == nil {
		return
	}
	b.mu.Lock()
	delete(b.subscribers, sub)
	b.mu.Unlock()
	sub.close()
}

func (b *EventBus) Publish(evt Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for sub := range b.subscribers {
		if !sub.matches(evt) {
			continue
		}
		select {
		case sub.ch <- evt:
			sub.dropped.Store(0)
		default:
			if sub.dropped.Add(1) == 10 {
				b.logger.Warn("dropping event for slow subscriber", "terminal_id", evt.TerminalID, "type", evt.Type)
			}
		}
	}
}

func (b *EventBus) Close() {
	b.mu.Lock()
	subs := make([]*eventSubscriber, 0, len(b.subscribers))
	for sub := range b.subscribers {
		subs = append(subs, sub)
		delete(b.subscribers, sub)
	}
	b.mu.Unlock()
	for _, sub := range subs {
		sub.close()
	}
}

func (s *eventSubscriber) matches(evt Event) bool {
	if s.cfg.terminalID != "" && s.cfg.terminalID != evt.TerminalID {
		return false
	}
	if s.cfg.sessionID != "" && s.cfg.sessionID != evt.SessionID {
		return false
	}
	if len(s.cfg.types) == 0 {
		return true
	}
	_, ok := s.cfg.types[evt.Type]
	return ok
}

func (s *eventSubscriber) close() {
	if s == nil {
		return
	}
	s.closeMu.Do(func() {
		close(s.ch)
	})
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
