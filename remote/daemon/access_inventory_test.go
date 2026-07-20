package daemon

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/remoteauth"
)

func TestTerminalAccessInventoryIsOpaqueAndRevisioned(t *testing.T) {
	identity, credential, store, now := sessionFixture(t, remoteauth.Scope{TerminalID: "terminal-secret"})
	records := store.ListClientAccess()
	if len(records) != 1 || store.AccessProjectionRevision() == 0 {
		t.Fatalf("access store = %#v revision=%d", records, store.AccessProjectionRevision())
	}
	inventory := BuildTerminalAccessInventory(store, "runtime:1:1", identity.DeviceID, "hub-1", 7, "presence-1", "runtime-1", 1, now)
	if inventory == nil || inventory.GetAccessProjectionRevision() != store.AccessProjectionRevision() || len(inventory.GetAccesses()) != 1 {
		t.Fatalf("inventory = %v", inventory)
	}
	projection := inventory.GetAccesses()[0]
	if projection.GetState() != cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_ACTIVE || projection.GetOpaqueAccessReference() != OpaqueAccessReference(identity.DeviceID, records[0].GrantID) || projection.GetSubjectFingerprintSummary() == "" {
		t.Fatalf("projection = %v", projection)
	}
	encoded := projection.String()
	for _, forbidden := range []string{records[0].GrantID, credential.CapabilityGrant, "terminal-secret", records[0].SubjectKeyFingerprint} {
		if forbidden != "" && strings.Contains(encoded, forbidden) {
			t.Fatalf("projection leaked %q: %s", forbidden, encoded)
		}
	}
}
