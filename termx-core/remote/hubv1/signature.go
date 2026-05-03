package hubv1

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type AgentRegistrationSignatureFields struct {
	MachineID string
	AgentID   string
	Nonce     string
	Timestamp time.Time
}

func CanonicalAgentRegistrationSignatureMessage(fields AgentRegistrationSignatureFields) []byte {
	machineHash := sha256.Sum256([]byte(strings.TrimSpace(fields.MachineID)))
	agentHash := sha256.Sum256([]byte(strings.TrimSpace(fields.AgentID)))
	return []byte(strings.Join([]string{
		"termx-agent-registration-v1:",
		"sha256(machine_id):" + hex.EncodeToString(machineHash[:]),
		"sha256(agent_id):" + hex.EncodeToString(agentHash[:]),
		"nonce:" + strings.TrimSpace(fields.Nonce),
		fmt.Sprintf("timestamp:%d", fields.Timestamp.UTC().Unix()),
	}, "\n"))
}
