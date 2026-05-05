package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

func TestSubmitHubAnswerAcceptsNoContentSuccess(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents/signaling/answer" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hub.Close()

	err := SubmitHubAnswer(context.Background(), hub.URL, hubv1.SubmitSignalingAnswerRequest{
		AgentSessionID: "agent-session-1",
		DeviceID:       "machine-1",
		Answer: hubv1.SignalingAnswer{
			SessionID: "session-1",
			SDP:       "v=0\r\ns=answer\r\n",
		},
	})
	if err != nil {
		t.Fatalf("SubmitHubAnswer returned error for 204 success: %v", err)
	}
}
