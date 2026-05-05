package rtc

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lozzow/termx/termx-remote/fileapi"
	"github.com/pion/webrtc/v4"
)

func TestIsRelayConnectionReturnsTrueWhenRelayCandidate(t *testing.T) {
	report := webrtc.StatsReport{
		"transport": webrtc.TransportStats{
			ID:                      "transport",
			SelectedCandidatePairID: "pair",
		},
		"pair": webrtc.ICECandidatePairStats{
			ID:                "pair",
			State:             webrtc.StatsICECandidatePairStateSucceeded,
			Nominated:         true,
			LocalCandidateID:  "local",
			RemoteCandidateID: "remote",
		},
		"local": webrtc.ICECandidateStats{
			ID:            "local",
			CandidateType: webrtc.ICECandidateTypeHost,
		},
		"remote": webrtc.ICECandidateStats{
			ID:            "remote",
			CandidateType: webrtc.ICECandidateTypeRelay,
		},
	}
	if !isRelayConnectionStats(report) {
		t.Fatal("expected relay candidate in succeeded pair to be detected")
	}
}

func TestIsRelayConnectionReturnsFalseWhenP2PCandidate(t *testing.T) {
	report := webrtc.StatsReport{
		"transport": webrtc.TransportStats{
			ID:                      "transport",
			SelectedCandidatePairID: "pair",
		},
		"pair": webrtc.ICECandidatePairStats{
			ID:                "pair",
			State:             webrtc.StatsICECandidatePairStateSucceeded,
			Nominated:         true,
			LocalCandidateID:  "local",
			RemoteCandidateID: "remote",
		},
		"local": webrtc.ICECandidateStats{
			ID:            "local",
			CandidateType: webrtc.ICECandidateTypeHost,
		},
		"remote": webrtc.ICECandidateStats{
			ID:            "remote",
			CandidateType: webrtc.ICECandidateTypeSrflx,
		},
	}
	if isRelayConnectionStats(report) {
		t.Fatal("host/srflx succeeded pair should not be treated as relay")
	}
}

func TestIsRelayConnectionIgnoresNonSelectedSucceededRelayPair(t *testing.T) {
	report := webrtc.StatsReport{
		"transport": webrtc.TransportStats{
			ID:                      "transport",
			SelectedCandidatePairID: "p2p-pair",
		},
		"p2p-pair": webrtc.ICECandidatePairStats{
			ID:                "p2p-pair",
			State:             webrtc.StatsICECandidatePairStateSucceeded,
			Nominated:         true,
			LocalCandidateID:  "local-host",
			RemoteCandidateID: "remote-srflx",
		},
		"relay-pair": webrtc.ICECandidatePairStats{
			ID:                "relay-pair",
			State:             webrtc.StatsICECandidatePairStateSucceeded,
			LocalCandidateID:  "local-relay",
			RemoteCandidateID: "remote-relay",
		},
		"local-host": webrtc.ICECandidateStats{
			ID:            "local-host",
			CandidateType: webrtc.ICECandidateTypeHost,
		},
		"remote-srflx": webrtc.ICECandidateStats{
			ID:            "remote-srflx",
			CandidateType: webrtc.ICECandidateTypeSrflx,
		},
		"local-relay": webrtc.ICECandidateStats{
			ID:            "local-relay",
			CandidateType: webrtc.ICECandidateTypeRelay,
		},
		"remote-relay": webrtc.ICECandidateStats{
			ID:            "remote-relay",
			CandidateType: webrtc.ICECandidateTypeRelay,
		},
	}
	if isRelayConnectionStats(report) {
		t.Fatal("non-selected relay pair should not mark the active connection as relay")
	}
}

func TestFileTransferForbiddenOnRelayWithoutPermission(t *testing.T) {
	ctx := withRelayConnection(context.Background(), true)
	status, _, errMsg := routeRuntimeAPIRequestWithContext(ctx, fileapi.NewManager(), nil, apiRequest{
		ID:     "req_file",
		Method: http.MethodPost,
		Path:   "/files/stat",
		Body:   json.RawMessage(`{"path":"/tmp"}`),
	})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, err=%q", status, errMsg)
	}
	if errMsg != "file transfer is not allowed over relay connection" {
		t.Fatalf("error = %q", errMsg)
	}
}

func TestFileTransferAllowedOnRelayWithPermission(t *testing.T) {
	ctx := withRelayTransferAllowed(withRelayConnection(context.Background(), true), true)
	status, _, errMsg := routeRuntimeAPIRequestWithContext(ctx, fileapi.NewManager(), nil, apiRequest{
		ID:     "req_file",
		Method: http.MethodPost,
		Path:   "/files/stat",
		Body:   json.RawMessage(`{"path":"/definitely-missing-termx-file"}`),
	})
	if status == http.StatusForbidden {
		t.Fatalf("file API should be routed when relay transfer is allowed, status=%d err=%q", status, errMsg)
	}
}
