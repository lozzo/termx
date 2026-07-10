package daemon

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	hubclient "github.com/lozzow/termx/termx-hub/client"
)

func TestAgentRegistersAnswersOfferAndStopsOnKick(t *testing.T) {
	stream := &fakeHubStream{messages: []hubclient.Message{
		{Offer: &hubclient.Offer{SessionID: "session-1", CapabilityGrant: "grant-1", SDP: "offer-sdp"}},
		{Kick: "device revoked"},
	}}
	answerer := &fakeOfferAnswerer{answer: hubclient.Answer{SessionID: "session-1", SDP: "answer-sdp"}}
	agent := Agent{
		Registration: hubclient.Registration{AgentID: "agent-1", DeviceID: "device-1", MachineID: "device-1"},
		Connect: func(context.Context, string, hubclient.Registration) (HubStream, hubclient.RegistrationAck, error) {
			return stream, hubclient.RegistrationAck{SessionID: "agent-session-1", HeartbeatSeconds: 60}, nil
		},
		Answerer: answerer,
	}
	err := agent.Run(context.Background())
	if err == nil || !errors.Is(err, ErrHubKick) {
		t.Fatalf("expected hub kick, got %v", err)
	}
	if answerer.offers != 1 || len(stream.answers) != 1 || stream.answers[0].SDP != "answer-sdp" {
		t.Fatalf("offer was not answered: answerer=%d answers=%#v", answerer.offers, stream.answers)
	}
}

func TestAgentSendsOfferErrorWithoutStoppingOtherEndpointState(t *testing.T) {
	stream := &fakeHubStream{messages: []hubclient.Message{
		{Offer: &hubclient.Offer{SessionID: "session-1", CapabilityGrant: "bad-grant"}},
	}}
	agent := Agent{
		Registration: hubclient.Registration{AgentID: "agent-1", DeviceID: "device-1", MachineID: "device-1"},
		Connect: func(context.Context, string, hubclient.Registration) (HubStream, hubclient.RegistrationAck, error) {
			return stream, hubclient.RegistrationAck{SessionID: "agent-session-1", HeartbeatSeconds: 60}, nil
		},
		Answerer: &fakeOfferAnswerer{err: errors.New("grant revoked")},
	}
	if err := agent.Run(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("agent should continue until stream ends, got %v", err)
	}
	if len(stream.answers) != 1 || stream.answers[0].Error != "grant revoked" {
		t.Fatalf("expected endpoint-scoped answer error, got %#v", stream.answers)
	}
}

func TestAgentHeartbeatUsesCurrentInventory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &fakeHubStream{blockReceive: make(chan struct{})}
	agent := Agent{
		Registration: hubclient.Registration{AgentID: "agent-1", DeviceID: "device-1", MachineID: "device-1"},
		Connect: func(context.Context, string, hubclient.Registration) (HubStream, hubclient.RegistrationAck, error) {
			return stream, hubclient.RegistrationAck{SessionID: "agent-session-1", HeartbeatSeconds: 1}, nil
		},
		Answerer: &fakeOfferAnswerer{},
		Inventory: func(context.Context) []hubclient.Terminal {
			return []hubclient.Terminal{{ID: "term-1", Name: "shell", RemoteEnabled: true}}
		},
		HeartbeatInterval: 10 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	deadline := time.After(time.Second)
	for len(stream.heartbeats) == 0 {
		select {
		case <-deadline:
			t.Fatal("heartbeat was not sent")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	close(stream.blockReceive)
	<-done
	if len(stream.heartbeats[0]) != 1 || stream.heartbeats[0][0].ID != "term-1" {
		t.Fatalf("heartbeat lost current inventory: %#v", stream.heartbeats)
	}
}

type fakeHubStream struct {
	messages     []hubclient.Message
	answers      []hubclient.Answer
	heartbeats   [][]hubclient.Terminal
	blockReceive chan struct{}
}

func (stream *fakeHubStream) Receive() (hubclient.Message, error) {
	if len(stream.messages) > 0 {
		message := stream.messages[0]
		stream.messages = stream.messages[1:]
		return message, nil
	}
	if stream.blockReceive != nil {
		<-stream.blockReceive
	}
	return hubclient.Message{}, io.EOF
}

func (stream *fakeHubStream) Heartbeat(_ string, terminals []hubclient.Terminal) error {
	stream.heartbeats = append(stream.heartbeats, append([]hubclient.Terminal(nil), terminals...))
	return nil
}

func (stream *fakeHubStream) SendAnswer(answer hubclient.Answer) error {
	stream.answers = append(stream.answers, answer)
	return nil
}

func (stream *fakeHubStream) Close() error { return nil }

type fakeOfferAnswerer struct {
	offers int
	answer hubclient.Answer
	err    error
}

func (answerer *fakeOfferAnswerer) Answer(context.Context, hubclient.Offer, hubclient.RegistrationAck) (hubclient.Answer, error) {
	answerer.offers++
	return answerer.answer, answerer.err
}
