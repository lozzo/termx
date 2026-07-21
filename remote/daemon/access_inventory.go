package daemon

import (
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/remoteauth"
)

// BuildTerminalAccessInventory 从 daemon-local AccessStore 构造完整脱敏 replacement。
// Cloud 只能看到 opaque reference、label、fingerprint summary、状态和时间，不能恢复 grant 或 scope。
func BuildTerminalAccessInventory(store *remoteauth.AccessStore, reportID, daemonDeviceID, hubID string, assignmentEpoch uint64, presenceSessionID, runtimeGeneration string, registryRevision uint64, observedAt time.Time) *cloudpb.TerminalAccessInventorySnapshot {
	if store == nil || reportID == "" || daemonDeviceID == "" || hubID == "" || assignmentEpoch == 0 || presenceSessionID == "" || runtimeGeneration == "" || observedAt.IsZero() {
		return nil
	}
	revision := store.AccessProjectionRevision()
	result := &cloudpb.TerminalAccessInventorySnapshot{ReportId: reportID, DaemonDeviceId: daemonDeviceID, ControlOwnerHubId: hubID, AssignmentEpoch: assignmentEpoch, ControlPresenceSessionId: presenceSessionID, DaemonRuntimeGeneration: runtimeGeneration, RegistryRevision: registryRevision, AccessProjectionRevision: revision, ObservedAtUnixMillis: observedAt.UnixMilli()}
	for _, record := range store.ListClientAccess() {
		state := cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_ACTIVE
		if !record.RevokedAt.IsZero() {
			state = cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_REVOKED
		} else if !observedAt.Before(record.ExpiresAt) {
			state = cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_EXPIRED
		}
		result.Accesses = append(result.Accesses, &cloudpb.TerminalAccessProjection{DaemonDeviceId: daemonDeviceID, OpaqueAccessReference: OpaqueAccessReference(daemonDeviceID, record.GrantID), ClientLabel: record.ClientLabel, SubjectFingerprintSummary: fingerprintSummary(record.SubjectKeyFingerprint), State: state, IssuedAtUnixMillis: record.IssuedAt.UnixMilli(), ExpiresAtUnixMillis: record.ExpiresAt.UnixMilli(), AccessProjectionRevision: revision})
	}
	return result
}

func fingerprintSummary(fingerprint string) string {
	digest := sha256.Sum256([]byte(fingerprint))
	return base64.RawURLEncoding.EncodeToString(digest[:8])
}
