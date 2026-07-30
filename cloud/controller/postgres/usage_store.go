package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/relayquota"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const relayAuthorizationTTL = 2 * time.Minute

type relayPolicyRows struct {
	accountID, accountState                      string
	accountRevision                              int64
	subscriptionID, subscriptionState, planID    string
	subscriptionRevision, planVersion            int64
	periodStart, periodEnd                       time.Time
	planRevision                                 int64
	relayEnabled                                 bool
	quotaBytes, maxBytesPerSession, maxRateBytes int64
	maxConcurrency                               int32
	allowedRegions                               []string
	edgeID, edgeRegion                           string
	edgeRevision                                 int64
	edgeEnabled                                  bool
	daemonID                                     string
	daemonRevision                               int64
	daemonRevoked                                bool
}

type usagePeriodRow struct {
	quota, ingress, egress, recovery, held int64
	heldSessions                           int64
}

type relayReservationRow struct {
	reservationID, accountID, subscriptionID, edgeID string
	daemonID, clientID, sessionID, state             string
	periodStart, periodEnd                           time.Time
	requestDigest, policyDigest, policySnapshot      []byte
	reservedBytes, maxRate, renewSequence            int64
	authorizedUntil                                  time.Time
	settlementKind                                   *string
	settledIngress, settledEgress, recoveryBytes     *int64
	observedAt, settledAt                            *time.Time
}

// ReserveRelay commits the only commercial authorization used by an Edge.
func (database *Database) ReserveRelay(ctx context.Context, edgeID string, request *cloudv1.RelayReserveRequest) (*cloudv1.RelayReserveResponse, error) {
	return database.reserveRelayAt(ctx, strings.TrimSpace(edgeID), request, time.Now().UTC())
}

func (database *Database) reserveRelayAt(ctx context.Context, edgeID string, request *cloudv1.RelayReserveRequest, now time.Time) (*cloudv1.RelayReserveResponse, error) {
	digest, err := validateReserveRequest(edgeID, request)
	if err != nil {
		return nil, err
	}
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	existing, found, err := findReservation(ctx, tx, request.GetReservationId(), false)
	if err != nil {
		return nil, err
	}
	if found {
		if existing.edgeID != edgeID || !relayquota.EqualDigest(existing.requestDigest, digest) {
			return reserveFailure(request, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT, "reservation ID conflicts with another request"), nil
		}
		if _, err := lockPolicyForReservation(ctx, tx, existing); err != nil {
			return nil, err
		}
		if _, err := lockUsagePeriod(ctx, tx, existing.accountID, existing.subscriptionID, existing.periodStart, existing.periodEnd); err != nil {
			return nil, err
		}
		existing, _, err = findReservation(ctx, tx, existing.reservationID, true)
		if err != nil {
			return nil, err
		}
		if existing.state == "held" && !now.Before(existing.authorizedUntil) {
			if err := recoverLockedReservation(ctx, tx, existing, now); err != nil {
				return nil, err
			}
			existing, _, err = findReservation(ctx, tx, existing.reservationID, false)
			if err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return reserveReplay(request, existing), nil
	}

	policyRows, err := lockCurrentPolicy(ctx, tx, request.GetAccountId(), request.GetDaemonId(), edgeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reserveFailure(request, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, "Relay policy identity is unavailable"), nil
		}
		return nil, err
	}
	policy, policyDigest, err := effectiveRelayPolicy(policyRows, now)
	if err != nil {
		return reserveFailure(request, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, err.Error()), nil
	}
	policyPayload, err := proto.MarshalOptions{Deterministic: true}.Marshal(policy)
	if err != nil {
		return nil, err
	}
	if err := ensureUsagePeriod(ctx, tx, policyRows, policyDigest, now); err != nil {
		return nil, err
	}
	usage, err := lockUsagePeriod(ctx, tx, policyRows.accountID, policyRows.subscriptionID, policyRows.periodStart, policyRows.periodEnd)
	if err != nil {
		return nil, err
	}
	if err := updateUsagePolicy(ctx, tx, policyRows, policyDigest, now); err != nil {
		return nil, err
	}
	if err := recoverExpiredReservations(ctx, tx, policyRows, now); err != nil {
		return nil, err
	}
	usage, err = lockUsagePeriod(ctx, tx, policyRows.accountID, policyRows.subscriptionID, policyRows.periodStart, policyRows.periodEnd)
	if err != nil {
		return nil, err
	}
	remaining := usage.quota - usage.ingress - usage.egress - usage.recovery - usage.held
	hold := policyRows.maxBytesPerSession
	if remaining < hold {
		hold = remaining
	}
	if hold <= 0 {
		return reserveFailure(request, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, "Relay quota is exhausted"), nil
	}
	if hold < policyRows.maxBytesPerSession {
		return reserveFailure(request, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, "remaining Relay quota cannot fund a complete session reservation"), nil
	}
	authorizedUntil := now.Add(relayAuthorizationTTL)
	if authorizedUntil.After(policyRows.periodEnd) {
		authorizedUntil = policyRows.periodEnd
	}
	var inserted string
	err = tx.QueryRow(ctx, `INSERT INTO relay_reservations(
reservation_id,request_digest,account_id,subscription_id,period_start,period_end,edge_id,daemon_id,client_id,session_id,state,
reserved_bytes,max_rate_bytes_per_second,renew_sequence,authorized_until,policy_digest,policy_snapshot,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'held',$11,$12,0,$13,$14,$15,$16,$16)
ON CONFLICT DO NOTHING RETURNING reservation_id::text`, request.GetReservationId(), digest, policyRows.accountID, policyRows.subscriptionID,
		policyRows.periodStart, policyRows.periodEnd, edgeID, request.GetDaemonId(), request.GetClientId(), request.GetSessionId(), hold,
		policyRows.maxRateBytes, authorizedUntil, policyDigest, policyPayload, now).Scan(&inserted)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("insert Relay reservation: %w", err)
	}
	if inserted == "" {
		conflict, found, findErr := findReservation(ctx, tx, request.GetReservationId(), false)
		if findErr != nil {
			return nil, findErr
		}
		if found && conflict.edgeID == edgeID && relayquota.EqualDigest(conflict.requestDigest, digest) {
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return reserveReplay(request, conflict), nil
		}
		return reserveFailure(request, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT, "session already has another Relay reservation"), nil
	}
	result, err := tx.Exec(ctx, `UPDATE usage_periods SET held_bytes=held_bytes+$1,held_sessions=held_sessions+1,revision=revision+1,updated_at=$2
WHERE account_id=$3 AND subscription_id=$4 AND period_start=$5 AND period_end=$6
AND held_sessions<$7
AND committed_ingress_bytes::numeric+committed_egress_bytes::numeric+recovery_bytes::numeric+held_bytes::numeric+$1::numeric<=quota_bytes::numeric`,
		hold, now, policyRows.accountID, policyRows.subscriptionID, policyRows.periodStart, policyRows.periodEnd, policyRows.maxConcurrency)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return reserveFailure(request, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, "Relay concurrency or quota is exhausted"), nil
	}
	reservation, _, err := findReservation(ctx, tx, request.GetReservationId(), false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &cloudv1.RelayReserveResponse{ReservationId: reservation.reservationID, RequestDigest: digest, Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED, Grant: reservation.grant()}, nil
}

// RenewRelay advances exactly one durable sequence without changing held bytes or slots.
func (database *Database) RenewRelay(ctx context.Context, edgeID string, request *cloudv1.RelayRenewRequest) (*cloudv1.RelayRenewResponse, error) {
	return database.renewRelayAt(ctx, strings.TrimSpace(edgeID), request, time.Now().UTC())
}

func (database *Database) renewRelayAt(ctx context.Context, edgeID string, request *cloudv1.RelayRenewRequest, now time.Time) (*cloudv1.RelayRenewResponse, error) {
	if err := validateRenewRequest(edgeID, request); err != nil {
		return nil, err
	}
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	reservation, found, err := findReservation(ctx, tx, request.GetReservationId(), false)
	if err != nil {
		return nil, err
	}
	if !found || reservation.edgeID != edgeID {
		return renewFailure(request, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, "Relay reservation is unavailable"), nil
	}
	policyRows, err := lockPolicyForReservation(ctx, tx, reservation)
	if err != nil {
		return nil, err
	}
	if _, err := lockUsagePeriod(ctx, tx, reservation.accountID, reservation.subscriptionID, reservation.periodStart, reservation.periodEnd); err != nil {
		return nil, err
	}
	reservation, _, err = findReservation(ctx, tx, reservation.reservationID, true)
	if err != nil {
		return nil, err
	}
	if reservation.state == "settled" {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &cloudv1.RelayRenewResponse{ReservationId: reservation.reservationID, RenewSequence: request.GetRenewSequence(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL, Grant: reservation.grant(), Terminal: reservation.ack(cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL)}, nil
	}
	if !now.Before(reservation.authorizedUntil) {
		if err := recoverLockedReservation(ctx, tx, reservation, now); err != nil {
			return nil, err
		}
		reservation, _, err = findReservation(ctx, tx, reservation.reservationID, false)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &cloudv1.RelayRenewResponse{ReservationId: reservation.reservationID, RenewSequence: request.GetRenewSequence(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL, Grant: reservation.grant(), Terminal: reservation.ack(cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL)}, nil
	}
	if request.GetRenewSequence() < uint64(reservation.renewSequence) || request.GetRenewSequence() > uint64(reservation.renewSequence)+1 {
		return renewFailure(request, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, "Relay renewal sequence is stale or has a gap"), nil
	}
	if request.GetRenewSequence() == uint64(reservation.renewSequence) {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &cloudv1.RelayRenewResponse{ReservationId: reservation.reservationID, RenewSequence: request.GetRenewSequence(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY, Grant: reservation.grant()}, nil
	}
	currentPolicy, currentDigest, policyErr := effectiveRelayPolicy(policyRows, now)
	_ = currentPolicy
	if policyErr != nil || !relayquota.EqualDigest(currentDigest, reservation.policyDigest) || !relayquota.EqualDigest(request.GetPolicyDigest(), reservation.policyDigest) ||
		policyRows.subscriptionID != reservation.subscriptionID || !policyRows.periodStart.Equal(reservation.periodStart) || !policyRows.periodEnd.Equal(reservation.periodEnd) {
		return renewFailure(request, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, "Relay policy or subscription period changed"), nil
	}
	authorizedUntil := now.Add(relayAuthorizationTTL)
	if authorizedUntil.After(reservation.periodEnd) {
		authorizedUntil = reservation.periodEnd
	}
	if _, err := tx.Exec(ctx, `UPDATE relay_reservations SET renew_sequence=$1,authorized_until=$2,updated_at=$3 WHERE reservation_id=$4`, request.GetRenewSequence(), authorizedUntil, now, reservation.reservationID); err != nil {
		return nil, err
	}
	reservation.renewSequence = int64(request.GetRenewSequence())
	reservation.authorizedUntil = authorizedUntil
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &cloudv1.RelayRenewResponse{ReservationId: reservation.reservationID, RenewSequence: request.GetRenewSequence(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED, Grant: reservation.grant()}, nil
}

// SettleRelay moves one held reservation to its immutable terminal fact.
func (database *Database) SettleRelay(ctx context.Context, edgeID string, settlement *cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error) {
	return database.settleRelayAt(ctx, strings.TrimSpace(edgeID), settlement, time.Now().UTC())
}

func (database *Database) settleRelayAt(ctx context.Context, edgeID string, settlement *cloudv1.RelaySettlement, now time.Time) (*cloudv1.RelaySettlementAck, error) {
	if err := validateSettlement(edgeID, settlement); err != nil {
		return nil, err
	}
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	reservation, found, err := findReservation(ctx, tx, settlement.GetReservationId(), false)
	if err != nil {
		return nil, err
	}
	if !found || reservation.edgeID != edgeID {
		return settlementFailure(settlement, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, "Relay reservation is unavailable"), nil
	}
	if _, err := lockPolicyForReservation(ctx, tx, reservation); err != nil {
		return nil, err
	}
	if _, err := lockUsagePeriod(ctx, tx, reservation.accountID, reservation.subscriptionID, reservation.periodStart, reservation.periodEnd); err != nil {
		return nil, err
	}
	reservation, _, err = findReservation(ctx, tx, reservation.reservationID, true)
	if err != nil {
		return nil, err
	}
	if reservation.state == "settled" {
		ack := terminalSettlementResponse(reservation, settlement)
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return ack, nil
	}
	if !relayquota.EqualDigest(settlement.GetPolicyDigest(), reservation.policyDigest) {
		return settlementFailure(settlement, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT, "Relay settlement policy digest conflicts with the reservation"), nil
	}
	ingress, egress, recovery := int64(settlement.GetIngressBytes()), int64(settlement.GetEgressBytes()), int64(0)
	kind := "exact"
	if settlement.GetKind() == cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX {
		kind, ingress, egress, recovery = "recovery_max", 0, 0, reservation.reservedBytes
	}
	if ingress > reservation.reservedBytes || egress > reservation.reservedBytes || ingress > reservation.reservedBytes-egress {
		return settlementFailure(settlement, cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, "Relay settlement exceeds reserved bytes"), nil
	}
	result, err := tx.Exec(ctx, `UPDATE usage_periods SET
committed_ingress_bytes=committed_ingress_bytes+$1,committed_egress_bytes=committed_egress_bytes+$2,recovery_bytes=recovery_bytes+$3,
held_bytes=held_bytes-$4,held_sessions=held_sessions-1,revision=revision+1,updated_at=$5
WHERE account_id=$6 AND subscription_id=$7 AND period_start=$8 AND period_end=$9 AND held_bytes>=$4 AND held_sessions>0
AND committed_ingress_bytes::numeric+committed_egress_bytes::numeric+recovery_bytes::numeric+$1::numeric+$2::numeric+$3::numeric<=9223372036854775807`,
		ingress, egress, recovery, reservation.reservedBytes, now, reservation.accountID, reservation.subscriptionID, reservation.periodStart, reservation.periodEnd)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, errors.New("Relay usage period invariant rejected settlement")
	}
	if _, err := tx.Exec(ctx, `UPDATE relay_reservations SET state='settled',settlement_kind=$1,settled_ingress_bytes=$2,settled_egress_bytes=$3,recovery_bytes=$4,observed_at=$5,settled_at=$6,updated_at=$6 WHERE reservation_id=$7`,
		kind, ingress, egress, recovery, settlement.GetObservedAt().AsTime(), now, reservation.reservationID); err != nil {
		return nil, err
	}
	reservation.state, reservation.settlementKind = "settled", &kind
	reservation.settledIngress, reservation.settledEgress, reservation.recoveryBytes = &ingress, &egress, &recovery
	observedAt := settlement.GetObservedAt().AsTime()
	reservation.observedAt, reservation.settledAt = &observedAt, &now
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return reservation.ack(cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED), nil
}

// QueryRelay returns the durable grant or terminal fact and lazily recovers an expired hold.
func (database *Database) QueryRelay(ctx context.Context, edgeID string, request *cloudv1.RelayQueryRequest) (*cloudv1.RelayQueryResponse, error) {
	return database.queryRelayAt(ctx, strings.TrimSpace(edgeID), request, time.Now().UTC())
}

func (database *Database) queryRelayAt(ctx context.Context, edgeID string, request *cloudv1.RelayQueryRequest, now time.Time) (*cloudv1.RelayQueryResponse, error) {
	if _, err := uuid.Parse(edgeID); err != nil || request == nil {
		return nil, errors.New("valid Edge and Relay query are required")
	}
	if _, err := uuid.Parse(strings.TrimSpace(request.GetReservationId())); err != nil {
		return nil, errors.New("Relay query reservation ID is invalid")
	}
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	reservation, found, err := findReservation(ctx, tx, request.GetReservationId(), false)
	if err != nil {
		return nil, err
	}
	if !found || reservation.edgeID != edgeID {
		return &cloudv1.RelayQueryResponse{ReservationId: request.GetReservationId(), Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED, ErrorMessage: "Relay reservation is unavailable"}, nil
	}
	if _, err := lockPolicyForReservation(ctx, tx, reservation); err != nil {
		return nil, err
	}
	if _, err := lockUsagePeriod(ctx, tx, reservation.accountID, reservation.subscriptionID, reservation.periodStart, reservation.periodEnd); err != nil {
		return nil, err
	}
	reservation, _, err = findReservation(ctx, tx, reservation.reservationID, true)
	if err != nil {
		return nil, err
	}
	if reservation.state == "held" && !now.Before(reservation.authorizedUntil) {
		if err := recoverLockedReservation(ctx, tx, reservation, now); err != nil {
			return nil, err
		}
		reservation, _, err = findReservation(ctx, tx, reservation.reservationID, false)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if reservation.state == "settled" {
		return &cloudv1.RelayQueryResponse{ReservationId: reservation.reservationID, Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL, Grant: reservation.grant(), Terminal: reservation.ack(cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL)}, nil
	}
	return &cloudv1.RelayQueryResponse{ReservationId: reservation.reservationID, Code: cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY, Grant: reservation.grant()}, nil
}

func validateReserveRequest(edgeID string, request *cloudv1.RelayReserveRequest) ([]byte, error) {
	if _, err := uuid.Parse(edgeID); err != nil || request == nil || request.GetObservedAt() == nil || request.GetObservedAt().CheckValid() != nil || strings.TrimSpace(request.GetClientId()) == "" {
		return nil, errors.New("valid Edge and complete Relay reserve request are required")
	}
	for _, value := range []string{request.GetReservationId(), request.GetAccountId(), request.GetDaemonId(), request.GetSessionId()} {
		if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
			return nil, errors.New("Relay reserve request contains an invalid UUID")
		}
	}
	digest, err := relayquota.ReserveRequestDigest(request)
	if err != nil || !relayquota.EqualDigest(digest, request.GetRequestDigest()) {
		return nil, errors.New("Relay reserve request digest is invalid")
	}
	return digest, nil
}

func validateRenewRequest(edgeID string, request *cloudv1.RelayRenewRequest) error {
	if _, err := uuid.Parse(edgeID); err != nil || request == nil || request.GetObservedAt() == nil || request.GetObservedAt().CheckValid() != nil || request.GetRenewSequence() > math.MaxInt64 || len(request.GetPolicyDigest()) != 32 {
		return errors.New("valid Edge and complete Relay renewal are required")
	}
	_, err := uuid.Parse(strings.TrimSpace(request.GetReservationId()))
	return err
}

func validateSettlement(edgeID string, settlement *cloudv1.RelaySettlement) error {
	if _, err := uuid.Parse(edgeID); err != nil || settlement == nil || settlement.GetObservedAt() == nil || settlement.GetObservedAt().CheckValid() != nil || len(settlement.GetPolicyDigest()) != 32 ||
		settlement.GetKind() < cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT || settlement.GetKind() > cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX ||
		settlement.GetIngressBytes() > math.MaxInt64 || settlement.GetEgressBytes() > math.MaxInt64 {
		return errors.New("valid Edge and complete Relay settlement are required")
	}
	if _, err := uuid.Parse(strings.TrimSpace(settlement.GetReservationId())); err != nil {
		return err
	}
	if settlement.GetKind() == cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX && (settlement.GetIngressBytes() != 0 || settlement.GetEgressBytes() != 0) {
		return errors.New("RECOVERY_MAX cannot carry exact byte counters")
	}
	return nil
}

func lockCurrentPolicy(ctx context.Context, tx pgx.Tx, accountID, daemonID, edgeID string) (relayPolicyRows, error) {
	var rows relayPolicyRows
	rows.accountID, rows.daemonID, rows.edgeID = accountID, daemonID, edgeID
	if err := tx.QueryRow(ctx, `SELECT state,revision FROM accounts WHERE account_id=$1 FOR UPDATE`, accountID).Scan(&rows.accountState, &rows.accountRevision); err != nil {
		return rows, err
	}
	if err := tx.QueryRow(ctx, `SELECT subscription_id::text,state,revision,plan_id,plan_version,period_start,period_end FROM subscriptions WHERE account_id=$1 FOR UPDATE`, accountID).
		Scan(&rows.subscriptionID, &rows.subscriptionState, &rows.subscriptionRevision, &rows.planID, &rows.planVersion, &rows.periodStart, &rows.periodEnd); err != nil {
		return rows, err
	}
	if err := tx.QueryRow(ctx, `SELECT revision,relay_enabled,relay_max_bytes_per_period,relay_max_bytes_per_lease,relay_max_rate_bytes_per_second,relay_max_concurrency,allowed_regions FROM plans WHERE plan_id=$1 AND version=$2 FOR UPDATE`, rows.planID, rows.planVersion).
		Scan(&rows.planRevision, &rows.relayEnabled, &rows.quotaBytes, &rows.maxBytesPerSession, &rows.maxRateBytes, &rows.maxConcurrency, &rows.allowedRegions); err != nil {
		return rows, err
	}
	if err := tx.QueryRow(ctx, `SELECT region,revision,enabled FROM edge_deployments WHERE edge_id=$1 FOR UPDATE`, edgeID).Scan(&rows.edgeRegion, &rows.edgeRevision, &rows.edgeEnabled); err != nil {
		return rows, err
	}
	if err := tx.QueryRow(ctx, `SELECT revision,revoked FROM daemons WHERE account_id=$1 AND daemon_id=$2 FOR UPDATE`, accountID, daemonID).Scan(&rows.daemonRevision, &rows.daemonRevoked); err != nil {
		return rows, err
	}
	return rows, nil
}

func lockPolicyForReservation(ctx context.Context, tx pgx.Tx, reservation relayReservationRow) (relayPolicyRows, error) {
	return lockCurrentPolicy(ctx, tx, reservation.accountID, reservation.daemonID, reservation.edgeID)
}

func effectiveRelayPolicy(rows relayPolicyRows, now time.Time) (*cloudv1.RelayPolicySnapshot, []byte, error) {
	validSubscription := rows.subscriptionState == "active" || rows.subscriptionState == "cancel_at_period_end" || rows.subscriptionState == "past_due"
	regionAllowed := slices.Contains(rows.allowedRegions, "*") || slices.Contains(rows.allowedRegions, rows.edgeRegion)
	if rows.accountState != "active" || !validSubscription || now.Before(rows.periodStart) || !now.Before(rows.periodEnd) || !rows.relayEnabled ||
		rows.quotaBytes <= 0 || rows.maxBytesPerSession <= 0 || rows.maxRateBytes <= 0 || rows.maxConcurrency <= 0 || !rows.edgeEnabled || rows.daemonRevoked || !regionAllowed {
		return nil, nil, errors.New("Relay is not enabled by the current account, subscription, plan, daemon, and Edge region")
	}
	regions := append([]string(nil), rows.allowedRegions...)
	slices.Sort(regions)
	policy := &cloudv1.RelayPolicySnapshot{
		AccountId: rows.accountID, AccountRevision: uint64(rows.accountRevision), AccountState: rows.accountState,
		SubscriptionId: rows.subscriptionID, SubscriptionRevision: uint64(rows.subscriptionRevision), SubscriptionState: rows.subscriptionState,
		PeriodStart: timestamppb.New(rows.periodStart), PeriodEnd: timestamppb.New(rows.periodEnd), PlanId: rows.planID, PlanVersion: uint64(rows.planVersion), PlanRevision: uint64(rows.planRevision),
		RelayEnabled: rows.relayEnabled, RelayMaxBytesPerPeriod: uint64(rows.quotaBytes), RelayMaxBytesPerSession: uint64(rows.maxBytesPerSession),
		RelayMaxRateBytesPerSecond: uint64(rows.maxRateBytes), RelayMaxConcurrency: uint32(rows.maxConcurrency), AllowedRegions: regions,
		EdgeId: rows.edgeID, EdgeRevision: uint64(rows.edgeRevision), EdgeEnabled: rows.edgeEnabled, EdgeRegion: rows.edgeRegion,
		DaemonId: rows.daemonID, DaemonRevision: uint64(rows.daemonRevision), DaemonRevoked: rows.daemonRevoked,
	}
	digest, err := relayquota.PolicyDigest(policy)
	return policy, digest, err
}

func ensureUsagePeriod(ctx context.Context, tx pgx.Tx, rows relayPolicyRows, digest []byte, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO usage_periods(account_id,subscription_id,period_start,period_end,quota_bytes,policy_digest,revision,updated_at)
VALUES($1,$2,$3,$4,$5,$6,1,$7) ON CONFLICT DO NOTHING`, rows.accountID, rows.subscriptionID, rows.periodStart, rows.periodEnd, rows.quotaBytes, digest, now)
	return err
}

func updateUsagePolicy(ctx context.Context, tx pgx.Tx, rows relayPolicyRows, digest []byte, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE usage_periods SET quota_bytes=$1,policy_digest=$2,revision=revision+1,updated_at=$3 WHERE account_id=$4 AND subscription_id=$5 AND period_start=$6 AND period_end=$7 AND (quota_bytes<>$1 OR policy_digest<>$2)`,
		rows.quotaBytes, digest, now, rows.accountID, rows.subscriptionID, rows.periodStart, rows.periodEnd)
	return err
}

func lockUsagePeriod(ctx context.Context, tx pgx.Tx, accountID, subscriptionID string, start, end time.Time) (usagePeriodRow, error) {
	var row usagePeriodRow
	err := tx.QueryRow(ctx, `SELECT quota_bytes,committed_ingress_bytes,committed_egress_bytes,recovery_bytes,held_bytes,held_sessions FROM usage_periods WHERE account_id=$1 AND subscription_id=$2 AND period_start=$3 AND period_end=$4 FOR UPDATE`,
		accountID, subscriptionID, start, end).Scan(&row.quota, &row.ingress, &row.egress, &row.recovery, &row.held, &row.heldSessions)
	return row, err
}

func recoverExpiredReservations(ctx context.Context, tx pgx.Tx, rows relayPolicyRows, now time.Time) error {
	reservations, err := lockedExpiredReservations(ctx, tx, rows, now)
	if err != nil {
		return err
	}
	for _, reservation := range reservations {
		if err := recoverLockedReservation(ctx, tx, reservation, now); err != nil {
			return err
		}
	}
	return nil
}

func lockedExpiredReservations(ctx context.Context, tx pgx.Tx, policy relayPolicyRows, now time.Time) ([]relayReservationRow, error) {
	rows, err := tx.Query(ctx, reservationSelect+` WHERE account_id=$1 AND subscription_id=$2 AND period_start=$3 AND period_end=$4 AND state='held' AND authorized_until<=$5 ORDER BY reservation_id FOR UPDATE`,
		policy.accountID, policy.subscriptionID, policy.periodStart, policy.periodEnd, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]relayReservationRow, 0)
	for rows.Next() {
		value, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func recoverLockedReservation(ctx context.Context, tx pgx.Tx, reservation relayReservationRow, now time.Time) error {
	result, err := tx.Exec(ctx, `UPDATE usage_periods SET recovery_bytes=recovery_bytes+$1,held_bytes=held_bytes-$1,held_sessions=held_sessions-1,revision=revision+1,updated_at=$2
WHERE account_id=$3 AND subscription_id=$4 AND period_start=$5 AND period_end=$6 AND held_bytes>=$1 AND held_sessions>0
AND committed_ingress_bytes::numeric+committed_egress_bytes::numeric+recovery_bytes::numeric+$1::numeric<=9223372036854775807`,
		reservation.reservedBytes, now, reservation.accountID, reservation.subscriptionID, reservation.periodStart, reservation.periodEnd)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("Relay usage period invariant rejected recovery")
	}
	_, err = tx.Exec(ctx, `UPDATE relay_reservations SET state='settled',settlement_kind='recovery_max',settled_ingress_bytes=0,settled_egress_bytes=0,recovery_bytes=reserved_bytes,observed_at=authorized_until,settled_at=$1,updated_at=$1 WHERE reservation_id=$2 AND state='held'`, now, reservation.reservationID)
	return err
}

const reservationSelect = `SELECT reservation_id::text,account_id::text,subscription_id::text,period_start,period_end,edge_id::text,daemon_id::text,client_id,session_id::text,state,request_digest,reserved_bytes,max_rate_bytes_per_second,renew_sequence,authorized_until,policy_digest,policy_snapshot,settlement_kind,settled_ingress_bytes,settled_egress_bytes,recovery_bytes,observed_at,settled_at FROM relay_reservations`

func findReservation(ctx context.Context, tx pgx.Tx, reservationID string, lock bool) (relayReservationRow, bool, error) {
	suffix := ` WHERE reservation_id=$1`
	if lock {
		suffix += ` FOR UPDATE`
	}
	row, err := scanReservation(tx.QueryRow(ctx, reservationSelect+suffix, reservationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return relayReservationRow{}, false, nil
	}
	return row, err == nil, err
}

type reservationScanner interface{ Scan(...any) error }

func scanReservation(row reservationScanner) (relayReservationRow, error) {
	var value relayReservationRow
	err := row.Scan(&value.reservationID, &value.accountID, &value.subscriptionID, &value.periodStart, &value.periodEnd, &value.edgeID, &value.daemonID, &value.clientID, &value.sessionID,
		&value.state, &value.requestDigest, &value.reservedBytes, &value.maxRate, &value.renewSequence, &value.authorizedUntil, &value.policyDigest, &value.policySnapshot,
		&value.settlementKind, &value.settledIngress, &value.settledEgress, &value.recoveryBytes, &value.observedAt, &value.settledAt)
	return value, err
}

func (reservation relayReservationRow) grant() *cloudv1.RelayGrant {
	policy := &cloudv1.RelayPolicySnapshot{}
	if err := proto.Unmarshal(reservation.policySnapshot, policy); err != nil {
		return nil
	}
	return &cloudv1.RelayGrant{ReservationId: reservation.reservationID, SessionId: reservation.sessionID, ReservedBytes: uint64(reservation.reservedBytes), MaxRateBytesPerSecond: uint64(reservation.maxRate), RenewSequence: uint64(reservation.renewSequence), AuthorizedUntil: timestamppb.New(reservation.authorizedUntil), PolicyDigest: append([]byte(nil), reservation.policyDigest...), Policy: policy}
}

func (reservation relayReservationRow) ack(code cloudv1.RelayResponseCode) *cloudv1.RelaySettlementAck {
	kind := cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT
	if reservation.settlementKind != nil && *reservation.settlementKind == "recovery_max" {
		kind = cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX
	}
	return &cloudv1.RelaySettlementAck{ReservationId: reservation.reservationID, Kind: kind, IngressBytes: uint64(*reservation.settledIngress), EgressBytes: uint64(*reservation.settledEgress), RecoveryBytes: uint64(*reservation.recoveryBytes), PolicyDigest: append([]byte(nil), reservation.policyDigest...), ObservedAt: timestamppb.New(*reservation.observedAt), SettledAt: timestamppb.New(*reservation.settledAt), Code: code}
}

func reserveReplay(request *cloudv1.RelayReserveRequest, reservation relayReservationRow) *cloudv1.RelayReserveResponse {
	response := &cloudv1.RelayReserveResponse{ReservationId: reservation.reservationID, RequestDigest: append([]byte(nil), reservation.requestDigest...)}
	if reservation.state == "settled" {
		response.Code, response.Grant, response.Terminal = cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL, reservation.grant(), reservation.ack(cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL)
	} else {
		response.Code, response.Grant = cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY, reservation.grant()
	}
	return response
}

func terminalSettlementResponse(reservation relayReservationRow, settlement *cloudv1.RelaySettlement) *cloudv1.RelaySettlementAck {
	ack := reservation.ack(cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_TERMINAL)
	if reservation.settlementKind != nil && *reservation.settlementKind == "exact" && settlement.GetKind() == cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT {
		same := ack.GetIngressBytes() == settlement.GetIngressBytes() && ack.GetEgressBytes() == settlement.GetEgressBytes() && relayquota.EqualDigest(ack.GetPolicyDigest(), settlement.GetPolicyDigest()) && ack.GetObservedAt().AsTime().Equal(settlement.GetObservedAt().AsTime())
		if same {
			ack.Code = cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY
		} else {
			ack.Code, ack.ErrorMessage = cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT, "settlement counters conflict with the durable terminal fact"
		}
	}
	return ack
}

func reserveFailure(request *cloudv1.RelayReserveRequest, code cloudv1.RelayResponseCode, message string) *cloudv1.RelayReserveResponse {
	return &cloudv1.RelayReserveResponse{ReservationId: request.GetReservationId(), RequestDigest: append([]byte(nil), request.GetRequestDigest()...), Code: code, ErrorMessage: message}
}

func renewFailure(request *cloudv1.RelayRenewRequest, code cloudv1.RelayResponseCode, message string) *cloudv1.RelayRenewResponse {
	return &cloudv1.RelayRenewResponse{ReservationId: request.GetReservationId(), RenewSequence: request.GetRenewSequence(), Code: code, ErrorMessage: message}
}

func settlementFailure(settlement *cloudv1.RelaySettlement, code cloudv1.RelayResponseCode, message string) *cloudv1.RelaySettlementAck {
	return &cloudv1.RelaySettlementAck{ReservationId: settlement.GetReservationId(), Kind: settlement.GetKind(), IngressBytes: settlement.GetIngressBytes(), EgressBytes: settlement.GetEgressBytes(), PolicyDigest: append([]byte(nil), settlement.GetPolicyDigest()...), ObservedAt: settlement.GetObservedAt(), Code: code, ErrorMessage: message}
}
