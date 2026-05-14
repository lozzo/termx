package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/termx-core/protocol"
	unixtransport "github.com/lozzow/termx/termx-shared/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/sessionstore"
	"github.com/lozzow/termx/tuiv2/shared"
)

var sessionEventsReconnectDelay = 500 * time.Millisecond

type sessionEventsClient interface {
	Close() error
	Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error)
	StorageGet(ctx context.Context, params protocol.StorageGetParams) (*protocol.StorageEntry, error)
	StoragePut(ctx context.Context, params protocol.StoragePutParams) (*protocol.StorageEntry, error)
	StorageDelete(ctx context.Context, params protocol.StorageDeleteParams) (*protocol.StorageDeleteResult, error)
	StorageList(ctx context.Context, params protocol.StorageListParams) (*protocol.StorageListResult, error)
}

var newSessionEventsClient = func(ctx context.Context, socketPath string) (sessionEventsClient, error) {
	transport, err := unixtransport.Dial(socketPath)
	if err != nil {
		return nil, err
	}
	client := protocol.NewClient(transport)
	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func startSessionEventsForwarder(send func(tea.Msg), cfg shared.Config, fallback bridge.Client) func() {
	if send == nil || cfg.SessionID == "" {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSessionEventsForwarder(ctx, send, cfg, fallback)
	}()
	return func() {
		cancel()
		<-done
	}
}

func runSessionEventsForwarder(ctx context.Context, send func(tea.Msg), cfg shared.Config, fallback bridge.Client) {
	if ctx == nil || send == nil || cfg.SessionID == "" {
		return
	}
	if cfg.SocketPath == "" {
		runFallbackSessionEventsForwarder(ctx, send, cfg.SessionID, fallback)
		return
	}

	connectedOnce := false
	for ctx.Err() == nil {
		client, err := newSessionEventsClient(ctx, cfg.SocketPath)
		if err != nil {
			if !sleepWithContext(ctx, sessionEventsReconnectDelay) {
				return
			}
			continue
		}
		eventsCtx, eventsCancel := context.WithCancel(ctx)
		store := sessionstore.New(client)
		events, err := store.Watch(eventsCtx, cfg.SessionID)
		if err != nil {
			eventsCancel()
			_ = client.Close()
			if !sleepWithContext(ctx, sessionEventsReconnectDelay) {
				return
			}
			continue
		}
		if connectedOnce {
			snapshot, err := store.Get(eventsCtx, cfg.SessionID)
			send(sessionSnapshotMsg{Snapshot: snapshot, Err: err})
		}
		connectedOnce = true
		for {
			select {
			case <-ctx.Done():
				eventsCancel()
				_ = client.Close()
				return
			case evt, ok := <-events:
				if !ok {
					eventsCancel()
					_ = client.Close()
					if !sleepWithContext(ctx, sessionEventsReconnectDelay) {
						return
					}
					goto reconnect
				}
				send(sessionEventMsg{Event: evt})
			}
		}
	reconnect:
	}
}

func runFallbackSessionEventsForwarder(ctx context.Context, send func(tea.Msg), sessionID string, client bridge.Client) {
	if ctx == nil || send == nil || sessionID == "" || client == nil {
		return
	}
	storageClient, ok := client.(sessionstore.Client)
	if !ok {
		return
	}
	store := sessionstore.New(storageClient)
	events, err := store.Watch(ctx, sessionID)
	if err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			send(sessionEventMsg{Event: evt})
		}
	}
}

func sleepWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
