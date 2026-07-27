package integration_test

import (
	"context"
	"time"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type testEntitlementReader struct{}

func (testEntitlementReader) EffectiveEntitlement(context.Context, string) (*cloudv1.EffectiveEntitlement, error) {
	now := time.Now().UTC()
	return &cloudv1.EffectiveEntitlement{State: cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE, Capability: &cloudv1.CloudCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 10, RelayEnabled: true, RelayMaxConcurrency: 16, RelayMaxBytesPerPeriod: 1 << 40, RelayMaxBytesPerLease: 1 << 30, RelayMaxRateBytesPerSecond: 10 << 20, CloudDaemonLimit: 100, AllowedRegions: []string{"*"}}, RelayRemainingBytes: 1 << 40, EffectiveFrom: timestamppb.New(now.Add(-time.Hour)), EffectiveUntil: timestamppb.New(now.Add(time.Hour)), ComputedAt: timestamppb.New(now)}, nil
}
