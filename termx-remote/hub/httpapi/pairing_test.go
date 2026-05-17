package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/httpapi"
	"github.com/lozzow/termx/termx-remote/hub/registry"
)

func TestHubPairingRequiresWebControlProxy(t *testing.T) {
	t.Parallel()

	router := httpapi.NewHandler(httpapi.Config{
		Registry:       registry.New(registry.Config{}),
		InternalSecret: "hub-secret",
	})

	resp := postJSON(t, router, "/api/v1/pairing/claims", validPairingClaimRequest("mach_1"))

	if resp.Code != http.StatusForbidden {
		t.Fatalf("public Hub pairing status = %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "web_control_required") {
		t.Fatalf("public Hub pairing error = %s", resp.Body.String())
	}
}

func TestInternalPairingClaimIsHubScoped(t *testing.T) {
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
		resp := postJSONWithSecret(t, router, "/api/internal/pairing/claims", "hub-secret", validPairingClaimRequest("mach_1"))
		responseCh <- responseRecord{code: resp.Code, body: resp.Body.String()}
	}()

	claim, err := reg.PollPairingClaim(ctx, registry.PairingPollInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll internal pairing claim: %v", err)
	}
	if strings.Join(claim.AllowedPaths, ",") != registry.PathHub {
		t.Fatalf("internal pairing paths = %#v", claim.AllowedPaths)
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
			t.Fatalf("internal pairing status = %d body=%s", resp.code, resp.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("internal pairing response did not return")
	}
}

func TestLocalPairingClaimIsLocalScoped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg := registry.New(registry.Config{AgentTTL: time.Minute})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_local", AgentID: "agent_local"}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Registry:       reg,
		LocalDiscovery: true,
		AnswerTimeout:  500 * time.Millisecond,
		PollInterval:   time.Millisecond,
	})

	responseCh := make(chan responseRecord, 1)
	go func() {
		resp := postJSON(t, router, "/api/v1/pairing/claims", validPairingClaimRequest("mach_local"))
		responseCh <- responseRecord{code: resp.Code, body: resp.Body.String()}
	}()

	claim, err := reg.PollPairingClaim(ctx, registry.PairingPollInput{
		AgentID:   "agent_local",
		MachineID: "mach_local",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll local pairing claim: %v", err)
	}
	if strings.Join(claim.AllowedPaths, ",") != registry.PathLocal {
		t.Fatalf("local pairing paths = %#v", claim.AllowedPaths)
	}
	if _, err := reg.SubmitPairingResult(ctx, registry.PairingResultInput{
		AgentID:      "agent_local",
		MachineID:    "mach_local",
		ClaimID:      claim.ID,
		SessionToken: "session-token-local",
		ExpiresAt:    "2099-05-06T00:00:00Z",
	}); err != nil {
		t.Fatalf("submit pairing result: %v", err)
	}

	select {
	case resp := <-responseCh:
		if resp.code != http.StatusOK {
			t.Fatalf("local pairing status = %d body=%s", resp.code, resp.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("local pairing response did not return")
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
