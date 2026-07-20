package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/relayquota"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// Reserve 原子清理到期 reservation、校验账期额度和账号并发，再写入单 lease reservation。
// 相同 lease ID 与相同输入是精确重放；不同输入复用同一 ID 会 fail closed。
func (store *Store) Reserve(ctx context.Context, request relayquota.ReserveRequest, now time.Time) (*cloudpb.RelayLeaseReservation, *cloudpb.RelayQuotaPeriod, bool, error) {
	now = now.UTC()
	if err := request.Validate(now); err != nil {
		return nil, nil, false, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, err
	}
	defer tx.Rollback()
	if err := expireRelayReservations(ctx, tx, now); err != nil {
		return nil, nil, false, err
	}
	if existing, loadErr := relayReservation(ctx, tx, request.LeaseID); loadErr == nil {
		if !sameReservation(existing, request) || existing.GetState() != cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE {
			return nil, nil, false, relayquota.ErrReservationConflict
		}
		period, err := relayQuotaPeriod(ctx, tx, request.AccountID, request.PeriodStart, request.PeriodEnd, request.PeriodLimitBytes)
		if err != nil {
			return nil, nil, false, err
		}
		return existing, period, false, tx.Commit()
	} else if !errors.Is(loadErr, relayquota.ErrReservationNotFound) {
		return nil, nil, false, loadErr
	}
	if err := ensureRelayPeriod(ctx, tx, request); err != nil {
		return nil, nil, false, err
	}
	var activeCount uint32
	var reservedBytes uint64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(reserved_bytes-used_bytes),0) FROM relay_lease_reservations WHERE account_id=? AND period_start_unix_millis=? AND state=?`, request.AccountID, request.PeriodStart.UnixMilli(), cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE).Scan(&activeCount, &reservedBytes); err != nil {
		return nil, nil, false, err
	}
	var usedBytes uint64
	if err := tx.QueryRowContext(ctx, `SELECT used_bytes FROM relay_quota_periods WHERE account_id=? AND period_start_unix_millis=?`, request.AccountID, request.PeriodStart.UnixMilli()).Scan(&usedBytes); err != nil {
		return nil, nil, false, err
	}
	if activeCount >= request.MaxConcurrency || usedBytes >= request.PeriodLimitBytes || reservedBytes >= request.PeriodLimitBytes-usedBytes {
		return nil, nil, false, relayquota.ErrQuotaExhausted
	}
	remaining := request.PeriodLimitBytes - usedBytes - reservedBytes
	reserved := request.MaxBytesPerLease
	if reserved > remaining {
		reserved = remaining
	}
	if reserved == 0 {
		return nil, nil, false, relayquota.ErrQuotaExhausted
	}
	reservation := &cloudpb.RelayLeaseReservation{
		LeaseId: request.LeaseID, AccountId: request.AccountID, ManagedSessionId: request.ManagedSessionID,
		ClientDeviceId: request.ClientDeviceID, TargetDeviceId: request.TargetDeviceID, Region: request.Region,
		HubId: request.HubID, RelayId: request.RelayID, RouteId: request.RouteID,
		PeriodStartUnixMillis: request.PeriodStart.UnixMilli(), PeriodEndUnixMillis: request.PeriodEnd.UnixMilli(),
		ReservedBytes: reserved, State: cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE,
		ExpiresAtUnixMillis: request.ExpiresAt.UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli(), Revision: 1, IssuedAtUnixMillis: now.UnixMilli(),
	}
	body, err := marshal(reservation)
	if err != nil {
		return nil, nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO relay_lease_reservations(lease_id,account_id,managed_session_id,client_device_id,target_device_id,region,period_start_unix_millis,period_end_unix_millis,reserved_bytes,used_bytes,state,expires_at_unix_millis,release_after_unix_millis,revision,projection) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		request.LeaseID, request.AccountID, request.ManagedSessionID, request.ClientDeviceID, request.TargetDeviceID, request.Region,
		request.PeriodStart.UnixMilli(), request.PeriodEnd.UnixMilli(), reserved, 0, reservation.GetState(), request.ExpiresAt.UnixMilli(), request.ReleaseAfter.UnixMilli(), 1, body); err != nil {
		return nil, nil, false, conflict(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE relay_quota_periods SET revision=revision+1 WHERE account_id=? AND period_start_unix_millis=?`, request.AccountID, request.PeriodStart.UnixMilli()); err != nil {
		return nil, nil, false, err
	}
	period, err := relayQuotaPeriod(ctx, tx, request.AccountID, request.PeriodStart, request.PeriodEnd, request.PeriodLimitBytes)
	if err != nil {
		return nil, nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, err
	}
	return reservation, period, true, nil
}

// Release 释放仍未结算 usage 的 ACTIVE reservation。
// 调用方必须先确认对应 Relay allocation 已取消且没有待 drain usage；CLOUDP005 会把 settlement 接入同一事务 owner。
func (store *Store) Release(ctx context.Context, accountID, leaseID string, now time.Time) (*cloudpb.RelayLeaseReservation, *cloudpb.RelayQuotaPeriod, error) {
	if accountID == "" || leaseID == "" {
		return nil, nil, relayquota.ErrReservationNotFound
	}
	now = now.UTC()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	if err := expireRelayReservations(ctx, tx, now); err != nil {
		return nil, nil, err
	}
	reservation, err := relayReservation(ctx, tx, leaseID)
	if err != nil || reservation.GetAccountId() != accountID {
		return nil, nil, relayquota.ErrReservationNotFound
	}
	if reservation.GetState() == cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE {
		reservation.State = cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_RELEASED
		reservation.UpdatedAtUnixMillis = now.UnixMilli()
		reservation.Revision++
		body, marshalErr := marshal(reservation)
		if marshalErr != nil {
			return nil, nil, marshalErr
		}
		result, updateErr := tx.ExecContext(ctx, `UPDATE relay_lease_reservations SET state=?,revision=?,projection=? WHERE lease_id=? AND state=?`, reservation.GetState(), reservation.GetRevision(), body, leaseID, cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE)
		if updateErr != nil {
			return nil, nil, updateErr
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return nil, nil, relayquota.ErrReservationConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE relay_quota_periods SET revision=revision+1 WHERE account_id=? AND period_start_unix_millis=?`, accountID, reservation.GetPeriodStartUnixMillis()); err != nil {
			return nil, nil, err
		}
	}
	period, err := relayQuotaPeriod(ctx, tx, accountID, time.UnixMilli(reservation.GetPeriodStartUnixMillis()), time.UnixMilli(reservation.GetPeriodEndUnixMillis()), 0)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return reservation, period, nil
}

// Snapshot 返回指定账期 aggregate 和仍占用额度的 ACTIVE reservation。
func (store *Store) Snapshot(ctx context.Context, accountID string, periodStart, periodEnd, now time.Time) (*cloudpb.GetAccountRelayQuotaResponse, error) {
	if accountID == "" || periodStart.IsZero() || periodEnd.IsZero() || !periodEnd.After(periodStart) {
		return nil, relayquota.ErrReservationNotFound
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := expireRelayReservations(ctx, tx, now.UTC()); err != nil {
		return nil, err
	}
	period, err := relayQuotaPeriod(ctx, tx, accountID, periodStart, periodEnd, 0)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT projection FROM relay_lease_reservations WHERE account_id=? AND period_start_unix_millis=? AND state=? ORDER BY lease_id`, accountID, periodStart.UnixMilli(), cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	response := &cloudpb.GetAccountRelayQuotaResponse{Period: period}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.RelayLeaseReservation{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		response.ActiveReservations = append(response.ActiveReservations, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}

// SnapshotForPeriod 确保尚无 reservation 的新账期也返回真实 limit/remaining。
func (store *Store) SnapshotForPeriod(ctx context.Context, accountID string, periodStart, periodEnd time.Time, limit uint64, now time.Time) (*cloudpb.GetAccountRelayQuotaResponse, error) {
	if accountID == "" || periodStart.IsZero() || periodEnd.IsZero() || !periodEnd.After(periodStart) {
		return nil, relayquota.ErrReservationNotFound
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO relay_quota_periods(account_id,period_start_unix_millis,period_end_unix_millis,limit_bytes,used_bytes,revision) VALUES(?,?,?,?,0,1)`, accountID, periodStart.UnixMilli(), periodEnd.UnixMilli(), limit); err != nil {
		return nil, err
	}
	if err := expireRelayReservations(ctx, tx, now.UTC()); err != nil {
		return nil, err
	}
	period, err := relayQuotaPeriod(ctx, tx, accountID, periodStart, periodEnd, limit)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT projection FROM relay_lease_reservations WHERE account_id=? AND period_start_unix_millis=? AND state=? ORDER BY lease_id`, accountID, periodStart.UnixMilli(), cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE)
	if err != nil {
		return nil, err
	}
	response := &cloudpb.GetAccountRelayQuotaResponse{Period: period}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			rows.Close()
			return nil, err
		}
		value := &cloudpb.RelayLeaseReservation{}
		if err := proto.Unmarshal(body, value); err != nil {
			rows.Close()
			return nil, err
		}
		response.ActiveReservations = append(response.ActiveReservations, value)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}

func ensureRelayPeriod(ctx context.Context, tx *sql.Tx, request relayquota.ReserveRequest) error {
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO relay_quota_periods(account_id,period_start_unix_millis,period_end_unix_millis,limit_bytes,used_bytes,revision) VALUES(?,?,?,?,0,1)`, request.AccountID, request.PeriodStart.UnixMilli(), request.PeriodEnd.UnixMilli(), request.PeriodLimitBytes)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 1 {
		return nil
	}
	var periodEnd int64
	var limit uint64
	if err := tx.QueryRowContext(ctx, `SELECT period_end_unix_millis,limit_bytes FROM relay_quota_periods WHERE account_id=? AND period_start_unix_millis=?`, request.AccountID, request.PeriodStart.UnixMilli()).Scan(&periodEnd, &limit); err != nil {
		return err
	}
	if periodEnd != request.PeriodEnd.UnixMilli() || limit != request.PeriodLimitBytes {
		return relayquota.ErrReservationConflict
	}
	return nil
}

func expireRelayReservations(ctx context.Context, tx *sql.Tx, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT lease_id,account_id,period_start_unix_millis,projection FROM relay_lease_reservations WHERE state=? AND release_after_unix_millis<=?`, cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE, now.UnixMilli())
	if err != nil {
		return err
	}
	type expired struct {
		leaseID, accountID string
		periodStart        int64
		projection         []byte
	}
	var values []expired
	for rows.Next() {
		var value expired
		if err := rows.Scan(&value.leaseID, &value.accountID, &value.periodStart, &value.projection); err != nil {
			rows.Close()
			return err
		}
		values = append(values, value)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, value := range values {
		reservation := &cloudpb.RelayLeaseReservation{}
		if err := proto.Unmarshal(value.projection, reservation); err != nil {
			return err
		}
		reservation.State = cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_EXPIRED
		reservation.UpdatedAtUnixMillis = now.UnixMilli()
		reservation.Revision++
		body, err := marshal(reservation)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE relay_lease_reservations SET state=?,revision=?,projection=? WHERE lease_id=? AND state=?`, reservation.GetState(), reservation.GetRevision(), body, value.leaseID, cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE relay_quota_periods SET revision=revision+1 WHERE account_id=? AND period_start_unix_millis=?`, value.accountID, value.periodStart); err != nil {
			return err
		}
	}
	return nil
}

func relayReservation(ctx context.Context, tx *sql.Tx, leaseID string) (*cloudpb.RelayLeaseReservation, error) {
	var body []byte
	if err := tx.QueryRowContext(ctx, `SELECT projection FROM relay_lease_reservations WHERE lease_id=?`, leaseID).Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, relayquota.ErrReservationNotFound
		}
		return nil, err
	}
	value := &cloudpb.RelayLeaseReservation{}
	return value, proto.Unmarshal(body, value)
}

func relayQuotaPeriod(ctx context.Context, tx *sql.Tx, accountID string, periodStart, periodEnd time.Time, expectedLimit uint64) (*cloudpb.RelayQuotaPeriod, error) {
	var storedEnd int64
	var limit, used, revision uint64
	if err := tx.QueryRowContext(ctx, `SELECT period_end_unix_millis,limit_bytes,used_bytes,revision FROM relay_quota_periods WHERE account_id=? AND period_start_unix_millis=?`, accountID, periodStart.UnixMilli()).Scan(&storedEnd, &limit, &used, &revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, relayquota.ErrReservationNotFound
		}
		return nil, err
	}
	if storedEnd != periodEnd.UnixMilli() || expectedLimit != 0 && limit != expectedLimit {
		return nil, relayquota.ErrReservationConflict
	}
	var reserved uint64
	var active uint32
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(reserved_bytes-used_bytes),0),COUNT(*) FROM relay_lease_reservations WHERE account_id=? AND period_start_unix_millis=? AND state=?`, accountID, periodStart.UnixMilli(), cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE).Scan(&reserved, &active); err != nil {
		return nil, err
	}
	if used > limit || reserved > limit-used {
		return nil, fmt.Errorf("Relay quota invariant violated")
	}
	return &cloudpb.RelayQuotaPeriod{AccountId: accountID, PeriodStartUnixMillis: periodStart.UnixMilli(), PeriodEndUnixMillis: storedEnd, LimitBytes: limit, UsedBytes: used, ReservedBytes: reserved, RemainingBytes: limit - used - reserved, ActiveLeaseCount: active, Revision: revision}, nil
}

func sameReservation(value *cloudpb.RelayLeaseReservation, request relayquota.ReserveRequest) bool {
	return value.GetLeaseId() == request.LeaseID && value.GetAccountId() == request.AccountID && value.GetManagedSessionId() == request.ManagedSessionID && value.GetClientDeviceId() == request.ClientDeviceID && value.GetTargetDeviceId() == request.TargetDeviceID && value.GetRegion() == request.Region && value.GetHubId() == request.HubID && value.GetRelayId() == request.RelayID && value.GetRouteId() == request.RouteID && value.GetPeriodStartUnixMillis() == request.PeriodStart.UnixMilli() && value.GetPeriodEndUnixMillis() == request.PeriodEnd.UnixMilli()
}

// RelayReservation 返回 CommandOutbox target planner 使用的持久 reservation 深拷贝。
func (store *Store) RelayReservation(ctx context.Context, leaseID string) (*cloudpb.RelayLeaseReservation, error) {
	var body []byte
	if err := store.db.QueryRowContext(ctx, `SELECT projection FROM relay_lease_reservations WHERE lease_id=?`, leaseID).Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, relayquota.ErrReservationNotFound
		}
		return nil, err
	}
	value := &cloudpb.RelayLeaseReservation{}
	if err := proto.Unmarshal(body, value); err != nil {
		return nil, err
	}
	return value, nil
}

// RelayReservationsForSession 返回 managed session 当前全部 reservation。
func (store *Store) RelayReservationsForSession(ctx context.Context, managedSessionID string) ([]*cloudpb.RelayLeaseReservation, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT projection FROM relay_lease_reservations WHERE managed_session_id=? ORDER BY lease_id`, managedSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.RelayLeaseReservation
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.RelayLeaseReservation{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, relayquota.ErrReservationNotFound
	}
	return result, nil
}
