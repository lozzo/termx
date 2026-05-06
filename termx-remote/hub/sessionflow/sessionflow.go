package sessionflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-remote/bridge"
	"github.com/lozzow/termx/termx-remote/fileapi"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

const (
	PathLocal     = "local"
	PathPublicP2P = "public_p2p"
	PathManaged   = "managed"
)

type RelayPolicy struct {
	AllowRelay         bool
	AllowRelayTransfer bool
	RelayInUse         bool
}

type Plan struct {
	Path       string
	ICEServers []hubv1.RTCIceServerConfig
	Relay      RelayPolicy
}

type Answerer interface {
	AnswerOffer(
		ctx context.Context,
		offer hubv1.SignalingOffer,
		iceServers []hubv1.RTCIceServerConfig,
		sink bridge.TransportSink,
		fileManager *fileapi.Manager,
		opts any,
	) (hubv1.SignalingAnswer, error)
}

type AnswerInput struct {
	Plan    Plan
	Offer   hubv1.SignalingOffer
	Sink    bridge.TransportSink
	Files   *fileapi.Manager
	Options any
}

func ManagedPlan(iceServers []hubv1.RTCIceServerConfig, relay RelayPolicy) Plan {
	return Plan{
		Path:       PathManaged,
		ICEServers: cloneICEServers(iceServers),
		Relay:      relay,
	}
}

func AnswerManaged(ctx context.Context, answerer Answerer, in AnswerInput) (hubv1.SignalingAnswer, error) {
	return answerWithPath(ctx, answerer, in, PathManaged)
}

func ValidateClientPath(path string) error {
	switch strings.TrimSpace(path) {
	case PathLocal, PathPublicP2P, PathManaged:
		return nil
	case "relay":
		return fmt.Errorf("relay is not a client path")
	default:
		return fmt.Errorf("invalid client path: %s", path)
	}
}

func cloneICEServers(in []hubv1.RTCIceServerConfig) []hubv1.RTCIceServerConfig {
	out := make([]hubv1.RTCIceServerConfig, 0, len(in))
	for _, server := range in {
		out = append(out, hubv1.RTCIceServerConfig{
			URLs:       append([]string(nil), server.URLs...),
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return out
}

func answerWithPath(ctx context.Context, answerer Answerer, in AnswerInput, expectedPath string) (hubv1.SignalingAnswer, error) {
	path := strings.TrimSpace(in.Plan.Path)
	if err := ValidateClientPath(path); err != nil {
		return hubv1.SignalingAnswer{}, err
	}
	if path != expectedPath {
		return hubv1.SignalingAnswer{}, fmt.Errorf("session flow path %q cannot answer %s offer", path, expectedPath)
	}
	if answerer == nil {
		return hubv1.SignalingAnswer{}, fmt.Errorf("session flow answerer is required")
	}
	return answerer.AnswerOffer(ctx, in.Offer, cloneICEServers(in.Plan.ICEServers), in.Sink, in.Files, in.Options)
}
