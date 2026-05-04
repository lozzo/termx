package discovery

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
)

func RegisterHub(ctx context.Context, baseURL string, payload hubv1.HubRegisterRequest) (hubv1.HubRegisterResponse, error) {
	var out hubv1.HubRegisterResponse
	if err := doJSON(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/agents/register", payload, &out, nil); err != nil {
		return hubv1.HubRegisterResponse{}, fmt.Errorf("register hub agent: %w", err)
	}
	return out, nil
}

func HeartbeatHub(ctx context.Context, baseURL string, payload hubv1.HubHeartbeatRequest) (hubv1.HubHeartbeatResponse, error) {
	var out hubv1.HubHeartbeatResponse
	if err := doJSON(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/agents/heartbeat", payload, &out, nil); err != nil {
		return hubv1.HubHeartbeatResponse{}, fmt.Errorf("hub heartbeat: %w", err)
	}
	return out, nil
}

func PollHubOffer(ctx context.Context, baseURL string, payload hubv1.SignalingPollRequest) (hubv1.SignalingPollResponse, bool, error) {
	var out hubv1.SignalingPollResponse
	err := doJSON(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/agents/signaling/poll", payload, &out, nil)
	if err != nil {
		if IsHTTPStatus(err, http.StatusNoContent) {
			return hubv1.SignalingPollResponse{}, false, nil
		}
		return hubv1.SignalingPollResponse{}, false, fmt.Errorf("poll hub offer: %w", err)
	}
	return out, out.Offer != nil, nil
}

func SubmitHubAnswer(ctx context.Context, baseURL string, payload hubv1.SubmitSignalingAnswerRequest) error {
	if err := doJSON(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/agents/signaling/answer", payload, nil, nil); err != nil {
		if IsHTTPStatus(err, http.StatusNoContent) {
			return nil
		}
		return fmt.Errorf("submit hub answer: %w", err)
	}
	return nil
}

func PollHubPairingClaim(ctx context.Context, baseURL string, payload hubv1.PairingPollRequest) (hubv1.PairingPollResponse, bool, error) {
	var out hubv1.PairingPollResponse
	err := doJSON(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/agents/pairing/poll", payload, &out, nil)
	if err != nil {
		if IsHTTPStatus(err, http.StatusNoContent) {
			return hubv1.PairingPollResponse{}, false, nil
		}
		return hubv1.PairingPollResponse{}, false, fmt.Errorf("poll hub pairing claim: %w", err)
	}
	return out, out.Claim != nil, nil
}

func SubmitHubPairingResult(ctx context.Context, baseURL string, payload hubv1.SubmitPairingResultRequest) error {
	if err := doJSON(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/agents/pairing/result", payload, nil, nil); err != nil {
		if IsHTTPStatus(err, http.StatusNoContent) {
			return nil
		}
		return fmt.Errorf("submit hub pairing result: %w", err)
	}
	return nil
}
