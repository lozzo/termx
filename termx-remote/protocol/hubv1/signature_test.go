package hubv1

import (
	"strings"
	"testing"
	"time"
)

func TestCanonicalAgentRegistrationSignatureMessageHashesMachineAndAgent(t *testing.T) {
	message := string(CanonicalAgentRegistrationSignatureMessage(AgentRegistrationSignatureFields{
		MachineID: "machine-1",
		AgentID:   "agent-1",
		Nonce:     "nonce-1",
		Timestamp: time.Date(2026, 5, 3, 14, 20, 0, 0, time.UTC),
	}))
	for _, want := range []string{
		"termx-agent-registration-v1:",
		"sha256(machine_id):",
		"sha256(agent_id):",
		"nonce:nonce-1",
		"timestamp:1777818000",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("message missing %q: %q", want, message)
		}
	}
	if strings.Contains(message, "machine-1") || strings.Contains(message, "agent-1") {
		t.Fatalf("signature message should hash identifiers: %q", message)
	}
}
