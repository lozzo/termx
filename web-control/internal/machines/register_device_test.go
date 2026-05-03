package machines_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/machines"
)

func TestRegisterRemoteDeviceClaimsOwnedMachineAndStoresInventory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, ctx, "termx-machines-register-device-test")
	ownerID := registerUser(t, ctx, db, "owner-register-device@example.com")
	otherID := registerUser(t, ctx, db, "other-register-device@example.com")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 11, 48, 0, 0, time.UTC)}
	svc := machines.NewService(machines.Config{DB: db, Clock: clock})

	if _, err := svc.RegisterRemoteDevice(ctx, machines.RegisterRemoteDeviceInput{
		UserID:            ownerID,
		MachineID:         "device-smoke-1",
		MachinePublicKey:  "machine-public-key",
		MachinePrivateKey: "must-not-upload",
		DisplayName:       "Smoke Agent",
	}); err == nil {
		t.Fatal("registered device accepted uploaded machine private key")
	}

	machine, err := svc.RegisterRemoteDevice(ctx, machines.RegisterRemoteDeviceInput{
		UserID:           ownerID,
		MachineID:        "device-smoke-1",
		MachinePublicKey: "machine-public-key",
		DisplayName:      "Smoke Agent",
		Hostname:         "agent-host",
		Platform:         "linux/amd64",
		Terminals: []machines.RemoteTerminalInput{{
			ID:      "term-1",
			Name:    "Shell",
			Command: []string{"bash"},
			Cols:    80,
			Rows:    24,
			State:   "running",
		}},
	})
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	if machine.ID != "device-smoke-1" || machine.OwnerUserID != ownerID || machine.MachinePublicKey != "machine-public-key" {
		t.Fatalf("registered machine = %+v", machine)
	}
	if machine.LastSeenAt == nil || !machine.LastSeenAt.Equal(clock.value) {
		t.Fatalf("registered last_seen_at = %+v, want %v", machine.LastSeenAt, clock.value)
	}
	terminals, err := svc.ListRemoteTerminals(ctx, ownerID, machine.ID)
	if err != nil {
		t.Fatalf("list terminals: %v", err)
	}
	if len(terminals) != 1 || terminals[0].ID != "term-1" || terminals[0].MachineID != machine.ID ||
		terminals[0].Name != "Shell" || terminals[0].State != "running" {
		t.Fatalf("terminals = %+v", terminals)
	}

	clock.value = clock.value.Add(time.Minute)
	updated, err := svc.RegisterRemoteDevice(ctx, machines.RegisterRemoteDeviceInput{
		UserID:           ownerID,
		MachineID:        "device-smoke-1",
		MachinePublicKey: "machine-public-key-rotated",
		DisplayName:      "Smoke Agent Updated",
		Terminals: []machines.RemoteTerminalInput{{
			ID:    "term-2",
			Name:  "New Shell",
			State: "running",
		}},
	})
	if err != nil {
		t.Fatalf("update device: %v", err)
	}
	if updated.OwnerUserID != ownerID || updated.MachinePublicKey != "machine-public-key-rotated" ||
		updated.DisplayName != "Smoke Agent Updated" {
		t.Fatalf("updated machine = %+v", updated)
	}
	terminals, err = svc.ListRemoteTerminals(ctx, ownerID, machine.ID)
	if err != nil {
		t.Fatalf("list terminals after update: %v", err)
	}
	if len(terminals) != 1 || terminals[0].ID != "term-2" {
		t.Fatalf("terminal inventory was not replaced: %+v", terminals)
	}

	if _, err := svc.RegisterRemoteDevice(ctx, machines.RegisterRemoteDeviceInput{
		UserID:           otherID,
		MachineID:        "device-smoke-1",
		MachinePublicKey: "attacker-public-key",
		DisplayName:      "Takeover",
	}); !errors.Is(err, machines.ErrMachineNotOwned) {
		t.Fatalf("other user takeover err = %v", err)
	}
}

func TestRegisterRemoteDeviceToleratesDuplicateTerminalIDsInSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, ctx, "termx-machines-register-duplicate-terminals-test")
	ownerID := registerUser(t, ctx, db, "owner-register-duplicate-terminals@example.com")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 14, 52, 0, 0, time.UTC)}
	svc := machines.NewService(machines.Config{DB: db, Clock: clock})

	machine, err := svc.RegisterRemoteDevice(ctx, machines.RegisterRemoteDeviceInput{
		UserID:           ownerID,
		MachineID:        "device-duplicate-terminals",
		MachinePublicKey: "machine-public-key",
		DisplayName:      "Duplicate Terminal Agent",
		Terminals: []machines.RemoteTerminalInput{{
			ID:      "term-1",
			Name:    "Old Shell",
			Command: []string{"bash", "-lc", "old"},
			Cols:    80,
			Rows:    24,
			State:   "exited",
		}, {
			ID:      "term-1",
			Name:    "Latest Shell",
			Command: []string{"bash", "-lc", "latest"},
			Cols:    120,
			Rows:    40,
			State:   "running",
		}},
	})
	if err != nil {
		t.Fatalf("register device with duplicate terminal ids: %v", err)
	}

	terminals, err := svc.ListRemoteTerminals(ctx, ownerID, machine.ID)
	if err != nil {
		t.Fatalf("list terminals: %v", err)
	}
	if len(terminals) != 1 {
		t.Fatalf("terminals len = %d, want 1: %+v", len(terminals), terminals)
	}
	terminal := terminals[0]
	if terminal.ID != "term-1" || terminal.Name != "Latest Shell" || terminal.State != "running" ||
		terminal.Cols != 120 || terminal.Rows != 40 || strings.Join(terminal.Command, " ") != "bash -lc latest" {
		t.Fatalf("terminal was not replaced by latest snapshot entry: %+v", terminal)
	}
}

type testAgentRegistrationFields struct {
	MachineID string
	AgentID   string
	Nonce     string
	Timestamp time.Time
}

func testAgentRegistrationMessage(fields testAgentRegistrationFields) []byte {
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

func TestVerifyAgentRegistrationRequiresMachineSignatureAndRejectsReplay(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, ctx, "termx-machines-agent-proof-test")
	ownerID := registerUser(t, ctx, db, "agent-proof@example.com")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 14, 24, 0, 0, time.UTC)}
	svc := machines.NewService(machines.Config{DB: db, Clock: clock})
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate machine key: %v", err)
	}
	if _, err := svc.RegisterRemoteDevice(ctx, machines.RegisterRemoteDeviceInput{
		UserID:           ownerID,
		MachineID:        "device-proof-1",
		MachinePublicKey: base64.RawURLEncoding.EncodeToString(publicKey),
		DisplayName:      "Proof Agent",
	}); err != nil {
		t.Fatalf("register remote device: %v", err)
	}

	input := machines.VerifyAgentRegistrationInput{
		MachineID: "device-proof-1",
		AgentID:   "agent-proof-1",
		Signature: machines.AgentRegistrationSignature{
			Algorithm: "ed25519",
			Nonce:     "nonce-proof-1",
			Timestamp: clock.value.Unix(),
		},
	}
	input.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, testAgentRegistrationMessage(testAgentRegistrationFields{
		MachineID: input.MachineID,
		AgentID:   input.AgentID,
		Nonce:     input.Signature.Nonce,
		Timestamp: clock.value,
	})))
	if err := svc.VerifyAgentRegistration(ctx, input); err != nil {
		t.Fatalf("verify registration: %v", err)
	}
	if err := svc.VerifyAgentRegistration(ctx, input); err == nil {
		t.Fatal("replayed registration nonce was accepted")
	}

	input.Signature.Nonce = "nonce-proof-2"
	input.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, testAgentRegistrationMessage(testAgentRegistrationFields{
		MachineID: input.MachineID,
		AgentID:   "other-agent",
		Nonce:     input.Signature.Nonce,
		Timestamp: clock.value,
	})))
	if err := svc.VerifyAgentRegistration(ctx, input); err == nil {
		t.Fatal("registration signed for a different agent was accepted")
	}
}
