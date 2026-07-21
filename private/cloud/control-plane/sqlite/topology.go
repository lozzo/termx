package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// PutDeviceOwnership 持久化 Controller 设备目录的账号归属。
func (store *Store) PutDeviceOwnership(ctx context.Context, ownership cloudtopology.DeviceOwnership) error {
	publicKey := make([]byte, len(ownership.PublicKey))
	copy(publicKey, ownership.PublicKey)
	_, err := store.db.ExecContext(ctx, `INSERT INTO cloud_device_ownership(device_id,account_id,device_kind,auth_epoch,revoked,public_key,updated_at) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(device_id) DO UPDATE SET account_id=excluded.account_id,device_kind=excluded.device_kind,auth_epoch=excluded.auth_epoch,revoked=excluded.revoked,public_key=excluded.public_key,updated_at=excluded.updated_at`, ownership.DeviceID, ownership.AccountID, int32(ownership.Kind), ownership.AuthEpoch, boolInt(ownership.Revoked), publicKey, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// DeviceOwnership 返回 topology 校验使用的持久账号归属。
func (store *Store) DeviceOwnership(ctx context.Context, deviceID string) (cloudtopology.DeviceOwnership, error) {
	value := cloudtopology.DeviceOwnership{DeviceID: deviceID}
	var kind int32
	var revoked int
	err := store.db.QueryRowContext(ctx, `SELECT account_id,device_kind,auth_epoch,revoked,public_key FROM cloud_device_ownership WHERE device_id=?`, deviceID).Scan(&value.AccountID, &kind, &value.AuthEpoch, &revoked, &value.PublicKey)
	if errors.Is(err, sql.ErrNoRows) {
		return cloudtopology.DeviceOwnership{}, cloudtopology.ErrOwnershipNotFound
	}
	value.Kind = cloudpb.ManagedDeviceKind(kind)
	value.Revoked = revoked != 0
	return value, err
}

// DevicePolicies 返回 signed Hub projection 使用的完整持久设备策略。
func (store *Store) DevicePolicies(ctx context.Context) ([]*cloudpb.CloudDevicePolicy, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT device_id,account_id,device_kind,auth_epoch,revoked,public_key FROM cloud_device_ownership ORDER BY device_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.CloudDevicePolicy
	for rows.Next() {
		value := &cloudpb.CloudDevicePolicy{}
		var kind int32
		var revoked int
		if err := rows.Scan(&value.DeviceId, &value.AccountId, &kind, &value.AuthEpoch, &revoked, &value.PublicKey); err != nil {
			return nil, err
		}
		value.DeviceKind = cloudpb.ManagedDeviceKind(kind)
		value.Revoked = revoked != 0
		result = append(result, value)
	}
	return result, rows.Err()
}

// PeerSessionProjection 返回全部 fencing 字段精确匹配的 topology session。
func (store *Store) PeerSessionProjection(ctx context.Context, target *cloudpb.ManagedPeerSessionTarget) (cloudtopology.StoredPeerSession, error) {
	if target == nil {
		return cloudtopology.StoredPeerSession{}, cloudtopology.ErrTopologyRejected
	}
	var accountID, hubID string
	var payload []byte
	err := store.db.QueryRowContext(ctx, `SELECT account_id,hub_id,projection FROM managed_peer_topology WHERE daemon_device_id=? AND managed_session_id=? AND session_incarnation=?`, target.GetDaemonDeviceId(), target.GetManagedSessionId(), target.GetSessionIncarnation()).Scan(&accountID, &hubID, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return cloudtopology.StoredPeerSession{}, cloudtopology.ErrTopologyRejected
	}
	value := &cloudpb.ManagedPeerSessionProjection{}
	if err != nil || proto.Unmarshal(payload, value) != nil || !proto.Equal(value.GetTarget(), target) {
		return cloudtopology.StoredPeerSession{}, cloudtopology.ErrTopologyRejected
	}
	return cloudtopology.StoredPeerSession{AccountID: accountID, HubID: hubID, Value: value}, nil
}

// PeerSessionsByClient 返回该 client 当前非 CLOSED topology session，供 revoke fan-out 固定 child target。
func (store *Store) PeerSessionsByClient(ctx context.Context, clientDeviceID string) ([]cloudtopology.StoredPeerSession, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT account_id,hub_id,projection FROM managed_peer_topology ORDER BY hub_id,daemon_device_id,managed_session_id,session_incarnation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []cloudtopology.StoredPeerSession
	for rows.Next() {
		var accountID, hubID string
		var payload []byte
		if err := rows.Scan(&accountID, &hubID, &payload); err != nil {
			return nil, err
		}
		value := &cloudpb.ManagedPeerSessionProjection{}
		if proto.Unmarshal(payload, value) != nil {
			return nil, cloudtopology.ErrTopologyRejected
		}
		if value.GetClientDeviceId() == clientDeviceID && value.GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED {
			result = append(result, cloudtopology.StoredPeerSession{AccountID: accountID, HubID: hubID, Value: value})
		}
	}
	return result, rows.Err()
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM managed_peer_topology WHERE hub_id=?`, snapshot.HubID); err != nil {
		return err
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM terminal_access_topology WHERE hub_id=?`, snapshot.HubID); err != nil {
		return err
	}
	for _, value := range snapshot.TerminalAccesses {
		payload, err := proto.Marshal(value.Value)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO terminal_access_topology(daemon_device_id,account_id,hub_id,control_generation,access_projection_revision,freshness,inventory,updated_at) VALUES(?,?,?,?,?,?,?,?)`, value.Value.GetDaemonDeviceId(), value.AccountID, snapshot.HubID, snapshot.ControlGeneration, value.Value.GetAccessProjectionRevision(), cloudpb.Freshness_FRESHNESS_FRESH, payload, updatedAt); err != nil {
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

// ListAccountTopology 查询账号隔离后的最后可信 topology projection。
func (store *Store) ListAccountTopology(ctx context.Context, accountID, daemonDeviceID, clientDeviceID string, freshness cloudpb.Freshness, limit int) ([]*cloudpb.PresenceProjection, []*cloudpb.ManagedPeerSessionProjection, error) {
	presenceQuery := `SELECT projection FROM presence_topology WHERE account_id=?`
	presenceArgs := []any{accountID}
	if daemonDeviceID != "" {
		presenceQuery += ` AND daemon_device_id=?`
		presenceArgs = append(presenceArgs, daemonDeviceID)
	}
	presenceQuery += ` ORDER BY daemon_device_id LIMIT ?`
	presenceArgs = append(presenceArgs, limit)
	rows, err := store.db.QueryContext(ctx, presenceQuery, presenceArgs...)
	if err != nil {
		return nil, nil, err
	}
	var presences []*cloudpb.PresenceProjection
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			rows.Close()
			return nil, nil, err
		}
		value := &cloudpb.PresenceProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if freshness == cloudpb.Freshness_FRESHNESS_UNSPECIFIED || value.GetFreshness() == freshness {
			presences = append(presences, value)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	peerQuery := `SELECT projection FROM managed_peer_topology WHERE account_id=?`
	peerArgs := []any{accountID}
	if daemonDeviceID != "" {
		peerQuery += ` AND daemon_device_id=?`
		peerArgs = append(peerArgs, daemonDeviceID)
	}
	peerQuery += ` ORDER BY daemon_device_id,managed_session_id,session_incarnation LIMIT ?`
	peerArgs = append(peerArgs, limit)
	rows, err = store.db.QueryContext(ctx, peerQuery, peerArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var sessions []*cloudpb.ManagedPeerSessionProjection
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, nil, err
		}
		value := &cloudpb.ManagedPeerSessionProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, nil, err
		}
		if clientDeviceID != "" && value.GetClientDeviceId() != clientDeviceID || freshness != cloudpb.Freshness_FRESHNESS_UNSPECIFIED && value.GetFreshness() != freshness {
			continue
		}
		sessions = append(sessions, value)
	}
	return presences, sessions, rows.Err()
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
	if _, err := tx.ExecContext(ctx, `UPDATE terminal_access_topology SET control_generation=?,freshness=?,updated_at=? WHERE hub_id=?`, generation, cloudpb.Freshness_FRESHNESS_STALE, observedAt.UTC().Format(time.RFC3339Nano), hubID); err != nil {
		return err
	}
	return nil
}

// TerminalAccessProjection 返回精确 opaque reference 与其 inventory fencing。
func (store *Store) TerminalAccessProjection(ctx context.Context, daemonDeviceID, opaqueReference string) (cloudtopology.StoredTerminalAccess, error) {
	var accountID, hubID string
	var payload []byte
	err := store.db.QueryRowContext(ctx, `SELECT account_id,hub_id,inventory FROM terminal_access_topology WHERE daemon_device_id=?`, daemonDeviceID).Scan(&accountID, &hubID, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return cloudtopology.StoredTerminalAccess{}, cloudtopology.ErrTopologyRejected
	}
	inventory := &cloudpb.TerminalAccessInventorySnapshot{}
	if err != nil || proto.Unmarshal(payload, inventory) != nil {
		return cloudtopology.StoredTerminalAccess{}, cloudtopology.ErrTopologyRejected
	}
	for _, access := range inventory.GetAccesses() {
		if access.GetOpaqueAccessReference() == opaqueReference {
			return cloudtopology.StoredTerminalAccess{AccountID: accountID, HubID: hubID, Value: proto.Clone(access).(*cloudpb.TerminalAccessProjection), Inventory: inventory}, nil
		}
	}
	return cloudtopology.StoredTerminalAccess{}, cloudtopology.ErrTopologyRejected
}

// ListTerminalAccess 返回账号隔离的当前 inventory 条目和 freshness。
func (store *Store) ListTerminalAccess(ctx context.Context, accountID, daemonDeviceID string, state cloudpb.TerminalAccessState, limit int) ([]cloudtopology.StoredTerminalAccess, cloudpb.Freshness, time.Time, error) {
	if limit < 1 || limit > 256 {
		return nil, cloudpb.Freshness_FRESHNESS_UNSPECIFIED, time.Time{}, cloudtopology.ErrTopologyRejected
	}
	query := `SELECT account_id,hub_id,freshness,inventory,updated_at FROM terminal_access_topology WHERE account_id=?`
	args := []any{accountID}
	if daemonDeviceID != "" {
		query += ` AND daemon_device_id=?`
		args = append(args, daemonDeviceID)
	}
	query += ` ORDER BY daemon_device_id LIMIT ?`
	args = append(args, limit)
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, cloudpb.Freshness_FRESHNESS_UNSPECIFIED, time.Time{}, err
	}
	defer rows.Close()
	var result []cloudtopology.StoredTerminalAccess
	freshness := cloudpb.Freshness_FRESHNESS_FRESH
	var latest time.Time
	for rows.Next() {
		var storedAccount, hubID, updated string
		var storedFreshness int32
		var payload []byte
		if err := rows.Scan(&storedAccount, &hubID, &storedFreshness, &payload, &updated); err != nil {
			return nil, cloudpb.Freshness_FRESHNESS_UNSPECIFIED, time.Time{}, err
		}
		inventory := &cloudpb.TerminalAccessInventorySnapshot{}
		if proto.Unmarshal(payload, inventory) != nil {
			return nil, cloudpb.Freshness_FRESHNESS_UNSPECIFIED, time.Time{}, cloudtopology.ErrTopologyRejected
		}
		observed, _ := time.Parse(time.RFC3339Nano, updated)
		if observed.After(latest) {
			latest = observed
		}
		if cloudpb.Freshness(storedFreshness) == cloudpb.Freshness_FRESHNESS_STALE {
			freshness = cloudpb.Freshness_FRESHNESS_STALE
		}
		for _, access := range inventory.GetAccesses() {
			if state == cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_UNSPECIFIED || access.GetState() == state {
				result = append(result, cloudtopology.StoredTerminalAccess{AccountID: storedAccount, HubID: hubID, Value: proto.Clone(access).(*cloudpb.TerminalAccessProjection), Inventory: proto.Clone(inventory).(*cloudpb.TerminalAccessInventorySnapshot)})
				if len(result) == limit {
					return result, freshness, latest, nil
				}
			}
		}
	}
	return result, freshness, latest, rows.Err()
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
