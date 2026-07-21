package audit_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/audit"
	"github.com/muxvia/muxvia/private/cloud/control-plane/domain"
)

func TestLogAcceptsOnlyHashedPairingReference(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	log := audit.NewLog()
	approval := domain.PairingApproval{
		ID: "pairing-1", AccountID: "account", ClientDeviceID: "client", TargetDeviceID: "daemon",
		ApproverUserID: "user", Decision: domain.PairingDecisionApproved, GrantReferenceHash: strings.Repeat("ab", 32), DecidedAt: now,
	}
	if err := log.RecordPairing(approval); err != nil {
		t.Fatal(err)
	}
	approval.ID = "pairing-raw"
	approval.GrantReferenceHash = "raw-bearer-grant"
	if err := log.RecordPairing(approval); !errors.Is(err, audit.ErrInvalidPairingMetadata) {
		t.Fatalf("raw grant error = %v", err)
	}
}
