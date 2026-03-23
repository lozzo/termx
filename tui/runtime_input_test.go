package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestRuntimeTerminalInputHandlerForwardsActivePaneRuneInput(t *testing.T) {
	client := &stubRuntimeInputClient{}
	handler := NewRuntimeTerminalInputHandler(client, NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    17,
			},
		},
	}))

	cmd := handler.HandleKey(connectedRunAppState(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("expected rune key to produce input command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected successful input forwarding to return nil msg, got %#v", msg)
	}
	if len(client.inputs) != 1 {
		t.Fatalf("expected one input call, got %d", len(client.inputs))
	}
	if client.inputs[0].channel != 17 || string(client.inputs[0].data) != "a" {
		t.Fatalf("unexpected input payload: %+v", client.inputs[0])
	}
}

func TestRuntimeTerminalInputHandlerReturnsNoticeOnInputError(t *testing.T) {
	client := &stubRuntimeInputClient{err: errRuntimeRunBoom}
	handler := NewRuntimeTerminalInputHandler(client, NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    17,
			},
		},
	}))

	cmd := handler.HandleKey(connectedRunAppState(), tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected enter key to produce input command")
	}
	msg := cmd()
	feedback, ok := msg.(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msg)
	}
	if len(feedback.Notices) != 1 || feedback.Notices[0].Text != errRuntimeRunBoom.Error() {
		t.Fatalf("unexpected feedback notices: %+v", feedback.Notices)
	}
}

func TestRuntimeTerminalInputHandlerSkipsWhenModeActive(t *testing.T) {
	client := &stubRuntimeInputClient{}
	state := connectedRunAppState()
	state.UI.Mode.Active = types.ModeGlobal
	handler := NewRuntimeTerminalInputHandler(client, NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    17,
			},
		},
	}))

	cmd := handler.HandleKey(state, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd != nil {
		t.Fatalf("expected active mode to block passthrough, got %v", cmd)
	}
	if len(client.inputs) != 0 {
		t.Fatalf("expected no input call while mode active, got %+v", client.inputs)
	}
}

func TestRuntimeTerminalInputHandlerBlocksObserverOnlyTerminalInput(t *testing.T) {
	client := &stubRuntimeInputClient{}
	store := NewRuntimeTerminalStore(RuntimeSessions{
		Terminals: map[types.TerminalID]TerminalRuntimeSession{
			types.TerminalID("term-1"): {
				TerminalID: types.TerminalID("term-1"),
				Channel:    17,
			},
		},
	})
	store.ApplyEvent(protocol.Event{
		Type:                 protocol.EventCollaboratorsRevoked,
		TerminalID:           "term-1",
		CollaboratorsRevoked: &protocol.CollaboratorsRevokedData{},
	})
	handler := NewRuntimeTerminalInputHandler(client, store)

	cmd := handler.HandleKey(connectedRunAppState(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("expected observer-only terminal to surface notice command")
	}
	msg := cmd()
	feedback, ok := msg.(btui.FeedbackMsg)
	if !ok {
		t.Fatalf("expected feedback msg, got %#v", msg)
	}
	if len(feedback.Notices) != 1 || !strings.Contains(feedback.Notices[0].Text, "observer") {
		t.Fatalf("expected observer-only notice, got %+v", feedback.Notices)
	}
	if len(client.inputs) != 0 {
		t.Fatalf("expected input to be blocked after revoke, got %+v", client.inputs)
	}
}

type stubRuntimeInputClient struct {
	inputs []runtimeInputCall
	err    error
}

type runtimeInputCall struct {
	channel uint16
	data    []byte
}

func (c *stubRuntimeInputClient) Input(_ context.Context, channel uint16, data []byte) error {
	c.inputs = append(c.inputs, runtimeInputCall{
		channel: channel,
		data:    append([]byte(nil), data...),
	})
	return c.err
}
