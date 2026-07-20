package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// PutDeviceOwnership 持久化 Controller 设备目录的账号归属。
func (store *Store) PutDeviceOwnership(ctx context.Context, ownership cloudtopology.DeviceOwnership) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO cloud_device_ownership(device_id,account_id,device_kind,updated_at) VALUES(?,?,?,?)
ON CONFLICT(device_id) DO UPDATE SET account_id=excluded.account_id,device_kind=excluded.device_kind,updated_at=excluded.updated_at`, ownership.DeviceID, ownership.AccountID, int32(ownership.Kind), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// DeviceOwnership 返回 topology 校验使用的持久账号归属。
func (store *Store) DeviceOwnership(ctx context.Context, deviceID string) (cloudtopology.DeviceOwnership, error) {
	value := cloudtopology.DeviceOwnership{DeviceID: deviceID}
	var kind int32
	err := store.db.QueryRowContext(ctx, `SELECT account_id,device_kind FROM cloud_device_ownership WHERE device_id=?`, deviceID).Scan(&value.AccountID, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return cloudtopology.DeviceOwnership{}, cloudtopology.ErrOwnershipNotFound
	}
	value.Kind = cloudpb.ManagedDeviceKind(kind)
	return value, err
}

// ApplyTopologySnapshot 原子完整替换一个 Hub 的最后可信 topology projection。
func (store *Store) ApplyTopologySnapshot(ctx context.Context, snapshot cloudtopology.ValidatedSnapshot) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var generation, revision uint64
	var digest []byte
	err = tx.QueryRowContext(ctx, `SELECT control_generation,topology_revision,topology_digest FROM hub_topology_heads WHERE hub_id=?`, snapshot.HubID).Scan(&generation, &revision, &digest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if snapshot.ControlGeneration < generation || snapshot.ControlGeneration == generation && snapshot.TopologyRevision < revision {
			return cloudtopology.ErrTopologyRejected
		}
		if snapshot.ControlGeneration == generation && snapshot.TopologyRevision == revision {
			if !bytesEqual(digest, snapshot.Digest) {
				return cloudtopology.ErrTopologyRejected
			}
			return nil
		}
	}
	if err := degradeHubTopologyTx(ctx, tx, snapshot.HubID, snapshot.ControlGeneration, snapshot.ObservedAt, cloudpb.ObservationSource_OBSERVATION_SOURCE_HUB_TOPOLOGY_SNAPSHOT); err != nil {
		return err
	}
	updatedAt := snapshot.ObservedAt.UTC().Format(time.RFC3339Nano)
	for _, value := range snapshot.Presences {
		payload, err := proto.Marshal(value.Value)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO presence_topology(daemon_device_id,account_id,hub_id,control_generation,projection,updated_at) VALUES(?,?,?,?,?,?)
ON CONFLICT(daemon_device_id) DO UPDATE SET account_id=excluded.account_id,hub_id=excluded.hub_id,control_generation=excluded.control_generation,projection=excluded.projection,updated_at=excluded.updated_at`, value.Value.GetDaemonDeviceId(), value.AccountID, snapshot.HubID, snapshot.ControlGeneration, payload, updatedAt); err != nil {
			return err
		}
	}
	for _, value := range snapshot.PeerSessions {
		payload, err := proto.Marshal(value.Value)
		if err != nil {
			return err
		}
		target := value.Value.GetTarget()
		if _, err := tx.ExecContext(ctx, `INSERT INTO managed_peer_topology(daemon_device_id,managed_session_id,session_incarnation,account_id,hub_id,control_generation,projection,updated_at) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(daemon_device_id,managed_session_id,session_incarnation) DO UPDATE SET account_id=excluded.account_id,hub_id=excluded.hub_id,control_generation=excluded.control_generation,projection=excluded.projection,updated_at=excluded.updated_at`, target.GetDaemonDeviceId(), target.GetManagedSessionId(), target.GetSessionIncarnation(), value.AccountID, snapshot.HubID, snapshot.ControlGeneration, payload, updatedAt); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO hub_topology_heads(hub_id,control_generation,topology_revision,topology_digest,observed_at) VALUES(?,?,?,?,?)
ON CONFLICT(hub_id) DO UPDATE SET control_generation=excluded.control_generation,topology_revision=excluded.topology_revision,topology_digest=excluded.topology_digest,observed_at=excluded.observed_at`, snapshot.HubID, snapshot.ControlGeneration, snapshot.TopologyRevision, snapshot.Digest, updatedAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// MarkHubTopologyUnknown 在精确 control generation 断开时保留投影并降级证据。
func (store *Store) MarkHubTopologyUnknown(ctx context.Context, hubID string, generation uint64, observedAt time.Time) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current uint64
	if err := tx.QueryRowContext(ctx, `SELECT control_generation FROM hub_topology_heads WHERE hub_id=?`, hubID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if current != generation {
		return nil
	}
	if err := degradeHubTopologyTx(ctx, tx, hubID, generation, observedAt, cloudpb.ObservationSource_OBSERVATION_SOURCE_CONTROL_STREAM_LOST); err != nil {
		return err
	}
	return tx.Commit()
}

// PresenceProjection 返回账号隔离的最后可信 Presence projection。
func (store *Store) PresenceProjection(ctx context.Context, daemonDeviceID string) (string, *cloudpb.PresenceProjection, error) {
	var accountID string
	var payload []byte
	err := store.db.QueryRowContext(ctx, `SELECT account_id,projection FROM presence_topology WHERE daemon_device_id=?`, daemonDeviceID).Scan(&accountID, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, cloudtopology.ErrTopologyRejected
	}
	projection := &cloudpb.PresenceProjection{}
	if err != nil || proto.Unmarshal(payload, projection) != nil {
		return "", nil, cloudtopology.ErrTopologyRejected
	}
	return accountID, projection, nil
}

func degradeHubTopologyTx(ctx context.Context, tx *sql.Tx, hubID string, generation uint64, observedAt time.Time, source cloudpb.ObservationSource) error {
	presenceRows, err := tx.QueryContext(ctx, `SELECT daemon_device_id,projection FROM presence_topology WHERE hub_id=?`, hubID)
	if err != nil {
		return err
	}
	type update struct {
		key     string
		payload []byte
	}
	var presenceUpdates []update
	for presenceRows.Next() {
		var key string
		var payload []byte
		if err := presenceRows.Scan(&key, &payload); err != nil {
			presenceRows.Close()
			return err
		}
		value := &cloudpb.PresenceProjection{}
		if proto.Unmarshal(payload, value) != nil {
			presenceRows.Close()
			return cloudtopology.ErrTopologyRejected
		}
		value.Availability = cloudpb.Availability_AVAILABILITY_UNKNOWN
		value.Freshness = cloudpb.Freshness_FRESHNESS_STALE
		value.ObservationSource = source
		value.ObservedAtUnixMillis = observedAt.UnixMilli()
		encoded, _ := proto.Marshal(value)
		presenceUpdates = append(presenceUpdates, update{key: key, payload: encoded})
	}
	presenceRows.Close()
	for _, value := range presenceUpdates {
		if _, err := tx.ExecContext(ctx, `UPDATE presence_topology SET control_generation=?,projection=?,updated_at=? WHERE daemon_device_id=?`, generation, value.payload, observedAt.UTC().Format(time.RFC3339Nano), value.key); err != nil {
			return err
		}
	}
	sessionRows, err := tx.QueryContext(ctx, `SELECT daemon_device_id,managed_session_id,session_incarnation,projection FROM managed_peer_topology WHERE hub_id=?`, hubID)
	if err != nil {
		return err
	}
	type sessionUpdate struct {
		daemon, session string
		incarnation     uint64
		payload         []byte
	}
	var sessionUpdates []sessionUpdate
	for sessionRows.Next() {
		var value sessionUpdate
		if err := sessionRows.Scan(&value.daemon, &value.session, &value.incarnation, &value.payload); err != nil {
			sessionRows.Close()
			return err
		}
		projection := &cloudpb.ManagedPeerSessionProjection{}
		if proto.Unmarshal(value.payload, projection) != nil {
			sessionRows.Close()
			return cloudtopology.ErrTopologyRejected
		}
		projection.State = cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_UNKNOWN
		projection.Freshness = cloudpb.Freshness_FRESHNESS_STALE
		projection.ObservedAtUnixMillis = observedAt.UnixMilli()
		value.payload, _ = proto.Marshal(projection)
		sessionUpdates = append(sessionUpdates, value)
	}
	sessionRows.Close()
	for _, value := range sessionUpdates {
		if _, err := tx.ExecContext(ctx, `UPDATE managed_peer_topology SET control_generation=?,projection=?,updated_at=? WHERE daemon_device_id=? AND managed_session_id=? AND session_incarnation=?`, generation, value.payload, observedAt.UTC().Format(time.RFC3339Nano), value.daemon, value.session, value.incarnation); err != nil {
			return err
		}
	}
	return nil
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
