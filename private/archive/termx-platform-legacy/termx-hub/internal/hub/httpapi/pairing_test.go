package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-hub/internal/hub/httpapi"
	"github.com/lozzow/termx/termx-hub/internal/hub/registry"
)

func TestPublicPairingClaimIsMachineScoped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg := registry.New(registry.Config{AgentTTL: time.Minute})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Registry:       reg,
		InternalSecret: "hub-secret",
		AnswerTimeout:  500 * time.Millisecond,
		PollInterval:   time.Millisecond,
	})

	responseCh := make(chan responseRecord, 1)
	go func() {
		resp := postJSON(t, router, "/api/v1/pairing/claims", validPairingClaimRequest("mach_1"))
		responseCh <- responseRecord{code: resp.Code, body: resp.Body.String()}
	}()

	claim, err := reg.PollPairingClaim(ctx, registry.PairingPollInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll public pairing claim: %v", err)
	}
	if _, err := reg.SubmitPairingResult(ctx, registry.PairingResultInput{
		AgentID:      "agent_1",
		MachineID:    "mach_1",
		ClaimID:      claim.ID,
		SessionToken: "session-token-cloud",
		ExpiresAt:    "2099-05-06T00:00:00Z",
	}); err != nil {
		t.Fatalf("submit pairing result: %v", err)
	}

	select {
	case resp := <-responseCh:
		if resp.code != http.StatusOK {
			t.Fatalf("public pairing status = %d body=%s", resp.code, resp.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public pairing response did not return")
	}
}

func TestPairingClaimRateLimit(t *testing.T) {
	t.Parallel()

	router := httpapi.NewHandler(httpapi.Config{
		Registry: registry.New(registry.Config{}),
		PairingRateLimit: httpapi.PairingRateLimitConfig{
			Window:     time.Minute,
			PerIP:      1,
			PerMachine: 10,
		},
	})

	first := postJSON(t, router, "/api/v1/pairing/claims", validPairingClaimRequest("mach_1"))
	if first.Code == http.StatusTooManyRequests {
		t.Fatalf("first public pairing claim was rate limited: %s", first.Body.String())
	}
	second := postJSON(t, router, "/api/v1/pairing/claims", validPairingClaimRequest("mach_2"))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second public pairing status = %d body=%s", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), "pairing_rate_limited") {
		t.Fatalf("rate limit error = %s", second.Body.String())
	}
}

type responseRecord struct {
	code int
	body string
}

func validPairingClaimRequest(machineID string) map[string]any {
	return map[string]any{
		"machine_id":             machineID,
		"pair_session_id":        "pair-session-1",
		"pair_secret":            "pair-secret-1",
		"app_device_id":          "app-device-1",
		"app_name":               "TermX App",
		"requested_capabilities": []string{"terminal", "file_manager"},
	}
}
