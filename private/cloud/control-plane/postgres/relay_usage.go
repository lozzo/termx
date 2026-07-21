package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	"github.com/muxvia/muxvia/private/cloud/control-plane/usage"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// ApplyRelayUsage 在单个事务内提交 signed event journal、lease reservation、period used 和 session aggregate。
// 调用前必须完成 lease 与 Relay event 两层签名验证；本方法重新检查所有持久 binding 和数值上限。
func (store *Store) ApplyRelayUsage(ctx context.Context, record *cloudpb.RelayUsageRecord, claims servicecredential.RelayLeaseClaims, event usage.Event, digest [sha256.Size]byte, now time.Time) (*cloudpb.RelayUsageAck, *cloudpb.RelayQuotaPeriod, *cloudpb.RelayUsageAggregate, error) {
	if record == nil || record.GetEvent() == nil || len(record.GetSignedLease()) == 0 || event.EventID == "" || event.LeaseID == "" || event.RelayID == "" || event.Sequence == 0 || claims.LeaseID != event.LeaseID || claims.ManagedSessionID != event.ManagedSessionID {
		return nil, nil, nil, usage.ErrJournalConflict
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	defer tx.Rollback()
	if duplicate, err := relayUsageReplay(ctx, tx, event, digest); err != nil || duplicate {
		if err != nil {
			return nil, nil, nil, err
		}
		reservation, loadErr := relayReservation(ctx, tx, event.LeaseID)
		if loadErr != nil {
			return nil, nil, nil, loadErr
		}
		period, aggregate, loadErr := relayUsageProjection(ctx, tx, reservation, event.ManagedSessionID, event.RouteID)
		if loadErr != nil {
			return nil, nil, nil, loadErr
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, nil, err
		}
		return &cloudpb.RelayUsageAck{EventId: event.EventID, Sequence: event.Sequence, Duplicate: true}, period, aggregate, nil
	}
	reservation, err := relayReservation(ctx, tx, event.LeaseID)
	if err != nil || reservation.GetState() != cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE || reservation.GetAccountId() != claims.AccountID || reservation.GetManagedSessionId() != claims.ManagedSessionID || reservation.GetClientDeviceId() != claims.ClientDeviceID || reservation.GetTargetDeviceId() != claims.TargetDeviceID || reservation.GetRegion() != claims.Region || reservation.GetReservedBytes() != claims.MaxBytes {
		return nil, nil, nil, usage.ErrJournalConflict
	}
	var lastSequence uint64
	err = queryRowContext(ctx, tx, `SELECT COALESCE(MAX(sequence),0) FROM relay_usage_events WHERE relay_id=? AND lease_id=?`, event.RelayID, event.LeaseID).Scan(&lastSequence)
	if err != nil || event.Sequence != lastSequence+1 {
		return nil, nil, nil, usage.ErrSequenceRollback
	}
	if event.BytesUp > math.MaxUint64-event.BytesDown {
		return nil, nil, nil, usage.ErrUsageOutOfRange
	}
	delta := event.BytesUp + event.BytesDown
	if reservation.GetUsedBytes() > reservation.GetReservedBytes() || delta > reservation.GetReservedBytes()-reservation.GetUsedBytes() {
		return nil, nil, nil, usage.ErrUsageOutOfRange
	}
	period, aggregate, err := relayUsageProjection(ctx, tx, reservation, event.ManagedSessionID, event.RouteID)
	if err != nil || period.GetUsedBytes() > period.GetLimitBytes() || delta > period.GetLimitBytes()-period.GetUsedBytes() || aggregate.GetBytesUp() > math.MaxUint64-event.BytesUp || aggregate.GetBytesDown() > math.MaxUint64-event.BytesDown || aggregate.GetActiveSeconds() > math.MaxUint64-event.ActiveSeconds {
		return nil, nil, nil, usage.ErrUsageOutOfRange
	}
	recordBody, err := proto.MarshalOptions{Deterministic: true}.Marshal(record)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err := execContext(ctx, tx, `INSERT INTO relay_usage_events(relay_id,lease_id,sequence,event_id,digest,record,created_at_unix_millis) VALUES(?,?,?,?,?,?,?)`, event.RelayID, event.LeaseID, event.Sequence, event.EventID, digest[:], recordBody, now.UTC().UnixMilli()); err != nil {
		return nil, nil, nil, usage.ErrJournalConflict
	}
	reservation.UsedBytes += delta
	reservation.UpdatedAtUnixMillis = now.UTC().UnixMilli()
	reservation.Revision++
	if event.TerminationReason != "" {
		reservation.State = cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_RELEASED
	}
	reservationBody, err := marshal(reservation)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err := execContext(ctx, tx, `UPDATE relay_lease_reservations SET used_bytes=?,state=?,revision=?,projection=? WHERE lease_id=?`, reservation.GetUsedBytes(), reservation.GetState(), reservation.GetRevision(), reservationBody, reservation.GetLeaseId()); err != nil {
		return nil, nil, nil, err
	}
	period.UsedBytes += delta
	period.Revision++
	if _, err := execContext(ctx, tx, `UPDATE relay_quota_periods SET used_bytes=?,revision=? WHERE account_id=? AND period_start_unix_millis=?`, period.GetUsedBytes(), period.GetRevision(), period.GetAccountId(), period.GetPeriodStartUnixMillis()); err != nil {
		return nil, nil, nil, err
	}
	aggregate.BytesUp += event.BytesUp
	aggregate.BytesDown += event.BytesDown
	aggregate.ActiveSeconds += event.ActiveSeconds
	aggregate.Revision++
	aggregateBody, err := marshal(aggregate)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err := execContext(ctx, tx, `INSERT INTO relay_usage_aggregates(account_id,managed_session_id,route_id,period_start_unix_millis,projection) VALUES(?,?,?,?,?) ON CONFLICT(account_id,managed_session_id,route_id,period_start_unix_millis) DO UPDATE SET projection=excluded.projection`, aggregate.GetAccountId(), aggregate.GetManagedSessionId(), aggregate.GetRouteId(), aggregate.GetPeriodStartUnixMillis(), aggregateBody); err != nil {
		return nil, nil, nil, err
	}
	updatedPeriod, err := relayQuotaPeriod(ctx, tx, reservation.GetAccountId(), time.UnixMilli(reservation.GetPeriodStartUnixMillis()), time.UnixMilli(reservation.GetPeriodEndUnixMillis()), period.GetLimitBytes())
	if err != nil {
		return nil, nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, nil, err
	}
	return &cloudpb.RelayUsageAck{EventId: event.EventID, Sequence: event.Sequence}, updatedPeriod, aggregate, nil
}

func relayUsageReplay(ctx context.Context, tx *sql.Tx, event usage.Event, digest [sha256.Size]byte) (bool, error) {
	var stored []byte
	err := queryRowContext(ctx, tx, `SELECT digest FROM relay_usage_events WHERE relay_id=? AND lease_id=? AND sequence=?`, event.RelayID, event.LeaseID, event.Sequence).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(stored) != len(digest) || !bytesEqual(stored, digest[:]) {
		return false, usage.ErrDuplicateConflict
	}
	return true, nil
}

func relayUsageProjection(ctx context.Context, tx *sql.Tx, reservation *cloudpb.RelayLeaseReservation, managedSessionID, routeID string) (*cloudpb.RelayQuotaPeriod, *cloudpb.RelayUsageAggregate, error) {
	period, err := relayQuotaPeriod(ctx, tx, reservation.GetAccountId(), time.UnixMilli(reservation.GetPeriodStartUnixMillis()), time.UnixMilli(reservation.GetPeriodEndUnixMillis()), 0)
	if err != nil {
		return nil, nil, err
	}
	aggregate := &cloudpb.RelayUsageAggregate{AccountId: reservation.GetAccountId(), ManagedSessionId: managedSessionID, RouteId: routeID, PeriodStartUnixMillis: reservation.GetPeriodStartUnixMillis(), PeriodEndUnixMillis: reservation.GetPeriodEndUnixMillis()}
	var body []byte
	if err := queryRowContext(ctx, tx, `SELECT projection FROM relay_usage_aggregates WHERE account_id=? AND managed_session_id=? AND route_id=? AND period_start_unix_millis=?`, reservation.GetAccountId(), managedSessionID, routeID, reservation.GetPeriodStartUnixMillis()).Scan(&body); err == nil {
		if err := proto.Unmarshal(body, aggregate); err != nil {
			return nil, nil, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	return period, aggregate, nil
}
