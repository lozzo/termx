package clientgateway

import (
	"context"
	"strconv"
	"testing"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestCleanupSessionClosesRelayBeforeRemovingRuntimeSession(t *testing.T) {
	order := make([]string, 0, 2)
	runtime := &cleanupRuntime{order: &order}
	relay := &cleanupRelay{order: &order}
	service := &Service{config: Config{Runtime: runtime, Relay: relay}}

	service.cleanupSession("session-1", 7)

	if len(order) != 2 || order[0] != "relay:session-1" || order[1] != "runtime:session-1:7" {
		t.Fatalf("cleanup order = %#v", order)
	}
}

type cleanupRuntime struct{ order *[]string }

func (*cleanupRuntime) UpsertSession(context.Context, *cloudv1.ClientSessionSummary) error {
	return nil
}
func (runtime *cleanupRuntime) RemoveSession(_ context.Context, sessionID string, generation uint64) error {
	*runtime.order = append(*runtime.order, "runtime:"+sessionID+":"+strconv.FormatUint(generation, 10))
	return nil
}
func (*cleanupRuntime) BeginAgentSignal(context.Context, string, string, string) (uint64, <-chan *cloudv1.AgentEvent, error) {
	return 0, nil, nil
}
func (*cleanupRuntime) CancelAgentSignal(context.Context, string) error { return nil }
func (*cleanupRuntime) SendAgentCommand(context.Context, string, uint64, *cloudv1.EdgeCommand) error {
	return nil
}

type cleanupRelay struct{ order *[]string }

func (*cleanupRelay) RequestRelayLease(context.Context, *cloudv1.RelayLeaseRequest) (*cloudv1.RelayICEConfig, error) {
	return nil, nil
}
func (relay *cleanupRelay) CloseRelaySession(_ context.Context, sessionID string) error {
	*relay.order = append(*relay.order, "relay:"+sessionID)
	return nil
}
