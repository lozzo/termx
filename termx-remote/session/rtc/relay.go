package rtc

import (
	"context"
	"strings"

	"github.com/pion/webrtc/v4"
)

type relayConnectionContextKey struct{}
type relayTransferAllowedContextKey struct{}

func IsRelayConnection(pc *webrtc.PeerConnection) bool {
	if pc == nil {
		return false
	}
	return isRelayConnectionStats(pc.GetStats())
}

func isRelayConnectionStats(stats webrtc.StatsReport) bool {
	if selectedID := selectedCandidatePairID(stats); selectedID != "" {
		if pair, ok := stats[selectedID].(webrtc.ICECandidatePairStats); ok && pair.State == webrtc.StatsICECandidatePairStateSucceeded {
			return isRelayCandidate(stats, pair.LocalCandidateID) || isRelayCandidate(stats, pair.RemoteCandidateID)
		}
		return false
	}
	for _, stat := range stats {
		pair, ok := stat.(webrtc.ICECandidatePairStats)
		if !ok || pair.State != webrtc.StatsICECandidatePairStateSucceeded || !pair.Nominated {
			continue
		}
		if isRelayCandidate(stats, pair.LocalCandidateID) || isRelayCandidate(stats, pair.RemoteCandidateID) {
			return true
		}
	}
	return false
}

func selectedCandidatePairID(stats webrtc.StatsReport) string {
	for _, stat := range stats {
		transport, ok := stat.(webrtc.TransportStats)
		if !ok {
			continue
		}
		if id := strings.TrimSpace(transport.SelectedCandidatePairID); id != "" {
			return id
		}
	}
	return ""
}

func isRelayCandidate(stats webrtc.StatsReport, id string) bool {
	for _, stat := range stats {
		candidate, ok := stat.(webrtc.ICECandidateStats)
		if !ok || candidate.ID != id {
			continue
		}
		return candidate.CandidateType == webrtc.ICECandidateTypeRelay
	}
	return false
}

func withRelayConnection(ctx context.Context, relay bool) context.Context {
	return context.WithValue(ctx, relayConnectionContextKey{}, relay)
}

func relayConnectionFromContext(ctx context.Context) bool {
	relay, _ := ctx.Value(relayConnectionContextKey{}).(bool)
	return relay
}

func withRelayTransferAllowed(ctx context.Context, allowed bool) context.Context {
	return context.WithValue(ctx, relayTransferAllowedContextKey{}, allowed)
}

func relayTransferAllowedFromContext(ctx context.Context) bool {
	allowed, _ := ctx.Value(relayTransferAllowedContextKey{}).(bool)
	return allowed
}
