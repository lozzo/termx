package enrollment

import (
	"testing"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestDaemonRelayDelegationNeverExceedsRemainingQuota(t *testing.T) {
	entitlement := &cloudv1.EffectiveEntitlement{
		State: cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE,
		Capability: &cloudv1.CloudCapability{
			RelayEnabled: true, RelayMaxBytesPerLease: 4096, RelayMaxRateBytesPerSecond: 1024, RelayMaxConcurrency: 2,
		},
		RelayRemainingBytes: 2048,
	}
	delegation := daemonRelayDelegation(entitlement)
	if delegation.GetMaxBytesPerLease() != 2048 || delegation.GetMaxRateBytesPerSecond() != 1024 || delegation.GetMaxConcurrentAllocations() != 2 {
		t.Fatalf("daemon Relay delegation = %v", delegation)
	}
}
