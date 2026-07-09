package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
)

func TestVerifyGRPCOfferReplayAcceptsFreshNonce(t *testing.T) {
	manager := &Manager{}
	err := manager.verifyGRPCOfferReplay("grpc://hub.example", "nonce-1", time.Now().UTC().Unix())
	if err != nil {
		t.Fatalf("verifyGRPCOfferReplay: %v", err)
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	state := manager.hubStates["grpc://hub.example"]
	if state == nil || len(state.seenNonces) != 1 {
		t.Fatalf("seen nonces = %+v", state)
	}
}

func TestVerifyGRPCOfferReplayRejectsMissingNonce(t *testing.T) {
	manager := &Manager{}
	err := manager.verifyGRPCOfferReplay("grpc://hub.example", "  ", time.Now().UTC().Unix())
	if err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("verifyGRPCOfferReplay error = %v", err)
	}
}

func TestVerifyGRPCOfferReplayRejectsIssuedAtOutsideWindow(t *testing.T) {
	manager := &Manager{}
	issuedAt := time.Now().UTC().Add(-2 * offerReplayWindow).Unix()
	err := manager.verifyGRPCOfferReplay("grpc://hub.example", "nonce-1", issuedAt)
	if err == nil || !strings.Contains(err.Error(), "replay window") {
		t.Fatalf("verifyGRPCOfferReplay error = %v", err)
	}
}

func TestVerifyGRPCOfferReplayRejectsDuplicateNonce(t *testing.T) {
	manager := &Manager{}
	issuedAt := time.Now().UTC().Unix()
	if err := manager.verifyGRPCOfferReplay("grpc://hub.example", "nonce-1", issuedAt); err != nil {
		t.Fatalf("first verifyGRPCOfferReplay: %v", err)
	}
	err := manager.verifyGRPCOfferReplay("grpc://hub.example", "nonce-1", issuedAt)
	if err == nil || !strings.Contains(err.Error(), "replay") {
		t.Fatalf("second verifyGRPCOfferReplay error = %v", err)
	}
}

func TestCleanupHubOfferNoncesPrunesExpiredEntries(t *testing.T) {
	manager := &Manager{
		hubStates: map[string]*hubRuntimeState{
			"grpc://hub.example": {
				URL: "grpc://hub.example",
				seenNonces: map[string]time.Time{
					"old": time.Now().UTC().Add(-2 * offerReplayWindow),
				},
			},
		},
	}
	manager.cleanupHubOfferNonces("grpc://hub.example")
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if state := manager.hubStates["grpc://hub.example"]; state == nil || len(state.seenNonces) != 0 {
		t.Fatalf("seen nonces after cleanup = %+v", state)
	}
}

func TestHandleGRPCOfferRejectsMissingReplayNonce(t *testing.T) {
	manager := &Manager{}
	sender := &agentToHubCapture{}
	manager.handleGRPCOffer(context.Background(), "grpc://hub.example", &pb.SignalingOffer{
		SessionId: "offer-1",
		IssuedAt:  time.Now().UTC().Unix(),
	}, sender)
	answer := sender.msg.GetSignalingAnswer()
	if answer == nil {
		t.Fatalf("sent message = %+v", sender.msg)
	}
	if answer.GetSessionId() != "offer-1" || !strings.Contains(answer.GetError(), "nonce") {
		t.Fatalf("answer = %+v", answer)
	}
}

type agentToHubCapture struct {
	msg *pb.AgentToHub
}

func (s *agentToHubCapture) Send(msg *pb.AgentToHub) error {
	s.msg = msg
	return nil
}
