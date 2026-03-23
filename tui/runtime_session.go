package tui

import (
	"context"
	"sort"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/domain/types"
)

type RuntimeSessionBootstrapper interface {
	Bootstrap(ctx context.Context, client Client, state types.AppState) (RuntimeSessions, error)
}

type RuntimeSessions struct {
	Terminals   map[types.TerminalID]TerminalRuntimeSession
	EventStream <-chan protocol.Event
}

type TerminalRuntimeSession struct {
	TerminalID types.TerminalID
	Channel    uint16
	Snapshot   *protocol.Snapshot
	Stream     <-chan protocol.StreamFrame
	Stop       func()
}

type runtimeSessionBootstrapper struct{}

func NewRuntimeSessionBootstrapper() RuntimeSessionBootstrapper {
	return runtimeSessionBootstrapper{}
}

// Bootstrap 只做运行时会话接线，不改动 reducer/domain。
// 当前最小职责是：订阅全局事件、为已连接 terminal 建立 attach channel，并抓一份初始 snapshot。
func (runtimeSessionBootstrapper) Bootstrap(ctx context.Context, client Client, state types.AppState) (RuntimeSessions, error) {
	eventStream, err := client.Events(ctx, protocol.EventsParams{})
	if err != nil {
		return RuntimeSessions{}, err
	}
	terminalIDs := connectedTerminalIDs(state)
	sessions := RuntimeSessions{
		Terminals:   make(map[types.TerminalID]TerminalRuntimeSession, len(terminalIDs)),
		EventStream: eventStream,
	}
	var stops []func()
	for _, terminalID := range terminalIDs {
		attach, err := client.Attach(ctx, string(terminalID), "collaborator")
		if err != nil {
			stopSessions(stops)
			return RuntimeSessions{}, err
		}
		snapshot, err := client.Snapshot(ctx, string(terminalID), 0, 200)
		if err != nil {
			stopSessions(stops)
			return RuntimeSessions{}, err
		}
		stream, stop := client.Stream(attach.Channel)
		stops = append(stops, stop)
		sessions.Terminals[terminalID] = TerminalRuntimeSession{
			TerminalID: terminalID,
			Channel:    attach.Channel,
			Snapshot:   snapshot,
			Stream:     stream,
			Stop:       stop,
		}
	}
	return sessions, nil
}

func connectedTerminalIDs(state types.AppState) []types.TerminalID {
	terminalIDs := make([]types.TerminalID, 0, len(state.Domain.Connections))
	for terminalID, conn := range state.Domain.Connections {
		if len(conn.ConnectedPaneIDs) == 0 {
			continue
		}
		terminalIDs = append(terminalIDs, terminalID)
	}
	sort.Slice(terminalIDs, func(i, j int) bool {
		return terminalIDs[i] < terminalIDs[j]
	})
	return terminalIDs
}

func stopSessions(stops []func()) {
	for i := len(stops) - 1; i >= 0; i-- {
		if stops[i] != nil {
			stops[i]()
		}
	}
}
