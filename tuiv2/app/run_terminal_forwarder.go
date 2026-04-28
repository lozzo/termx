package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/internal/clientapi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
)

func startTerminalEventsForwarder(send func(tea.Msg), cfg shared.Config, fallback clientapi.Client) func() {
	if send == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runTerminalEventsForwarder(ctx, send, cfg, fallback)
	}()
	return func() {
		cancel()
		<-done
	}
}

func runTerminalEventsForwarder(ctx context.Context, send func(tea.Msg), cfg shared.Config, fallback clientapi.Client) {
	if ctx == nil || send == nil {
		return
	}
	if cfg.SocketPath == "" {
		runFallbackTerminalEventsForwarder(ctx, send, fallback)
		return
	}

	for ctx.Err() == nil {
		client, err := newSessionEventsClient(ctx, cfg.SocketPath)
		if err != nil {
			if !sleepWithContext(ctx, sessionEventsReconnectDelay) {
				return
			}
			continue
		}
		eventsCtx, eventsCancel := context.WithCancel(ctx)
		events, err := client.Events(eventsCtx, protocol.EventsParams{
			Types: []protocol.EventType{
				protocol.EventTerminalResized,
			},
		})
		if err != nil {
			eventsCancel()
			_ = client.Close()
			if !sleepWithContext(ctx, sessionEventsReconnectDelay) {
				return
			}
			continue
		}
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
				send(terminalEventMsg{Event: evt})
			}
		}
	reconnect:
	}
}

func runFallbackTerminalEventsForwarder(ctx context.Context, send func(tea.Msg), client clientapi.Client) {
	if ctx == nil || send == nil || client == nil {
		return
	}
	events, err := client.Events(ctx, protocol.EventsParams{
		Types: []protocol.EventType{
			protocol.EventTerminalResized,
		},
	})
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
			send(terminalEventMsg{Event: evt})
		}
	}
}
