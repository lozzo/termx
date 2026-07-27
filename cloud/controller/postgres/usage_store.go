package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

// CommitRelayUsage 在一个 PostgreSQL 事务内幂等插入事件并增加账号周期与 Edge 聚合。
// 已存在的 (edge_id,event_id) 仍返回 ACK，但绝不再次增加 aggregate。
func (database *Database) CommitRelayUsage(ctx context.Context, edgeID string, events []*cloudv1.UsageEvent) ([]string, error) {
	edgeID = strings.TrimSpace(edgeID)
	if _, err := uuid.Parse(edgeID); err != nil || len(events) == 0 {
		return nil, errors.New("Relay usage edge identity and non-empty batch are required")
	}
	for _, event := range events {
		if err := validateRelayUsage(edgeID, event); err != nil {
			return nil, err
		}
	}
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	acknowledged := make([]string, 0, len(events))
	committedAt := time.Now().UTC()
	for _, event := range events {
		transport, err := relayTransportName(event.GetTransport())
		if err != nil {
			return nil, err
		}
		var inserted string
		err = tx.QueryRow(ctx, `INSERT INTO relay_usage_events(edge_id,event_id,account_id,lease_id,daemon_id,client_id,session_id,allocation_id,transport,ingress_bytes,egress_bytes,started_at,ended_at,committed_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT(edge_id,event_id) DO NOTHING RETURNING event_id::text`,
			edgeID, event.GetEventId(), event.GetAccountId(), event.GetLeaseId(), event.GetDaemonId(), event.GetClientId(), event.GetSessionId(), event.GetAllocationId(), transport,
			event.GetIngressBytes(), event.GetEgressBytes(), event.GetStartedAt().AsTime(), event.GetEndedAt().AsTime(), committedAt).Scan(&inserted)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("insert Relay usage event %s: %w", event.GetEventId(), err)
		}
		if inserted != "" {
			periodStart := monthStart(event.GetEndedAt().AsTime())
			periodEnd := periodStart.AddDate(0, 1, 0)
			if _, err := tx.Exec(ctx, `INSERT INTO usage_periods(account_id,period_start,period_end,relay_ingress_bytes,relay_egress_bytes,revision,updated_at)
VALUES($1,$2,$3,$4,$5,1,$6) ON CONFLICT(account_id,period_start) DO UPDATE SET relay_ingress_bytes=usage_periods.relay_ingress_bytes+EXCLUDED.relay_ingress_bytes,relay_egress_bytes=usage_periods.relay_egress_bytes+EXCLUDED.relay_egress_bytes,revision=usage_periods.revision+1,updated_at=EXCLUDED.updated_at`,
				event.GetAccountId(), periodStart, periodEnd, event.GetIngressBytes(), event.GetEgressBytes(), committedAt); err != nil {
				return nil, fmt.Errorf("aggregate account Relay usage: %w", err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO relay_usage_aggregates(account_id,edge_id,period_start,ingress_bytes,egress_bytes,event_count,updated_at)
VALUES($1,$2,$3,$4,$5,1,$6) ON CONFLICT(account_id,edge_id,period_start) DO UPDATE SET ingress_bytes=relay_usage_aggregates.ingress_bytes+EXCLUDED.ingress_bytes,egress_bytes=relay_usage_aggregates.egress_bytes+EXCLUDED.egress_bytes,event_count=relay_usage_aggregates.event_count+1,updated_at=EXCLUDED.updated_at`,
				event.GetAccountId(), edgeID, periodStart, event.GetIngressBytes(), event.GetEgressBytes(), committedAt); err != nil {
				return nil, fmt.Errorf("aggregate Edge Relay usage: %w", err)
			}
		}
		acknowledged = append(acknowledged, event.GetEventId())
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return acknowledged, nil
}

func validateRelayUsage(edgeID string, event *cloudv1.UsageEvent) error {
	if event == nil || event.GetSchemaVersion() != 1 || event.GetEdgeId() != edgeID || event.GetStartedAt() == nil || event.GetEndedAt() == nil ||
		event.GetStartedAt().CheckValid() != nil || event.GetEndedAt().CheckValid() != nil || event.GetEndedAt().AsTime().Before(event.GetStartedAt().AsTime()) || strings.TrimSpace(event.GetClientId()) == "" {
		return errors.New("Relay UsageEvent is incomplete or targets another Edge")
	}
	if event.GetIngressBytes() > math.MaxInt64 || event.GetEgressBytes() > math.MaxInt64 {
		return errors.New("Relay UsageEvent byte counters exceed PostgreSQL bigint")
	}
	for _, value := range []string{event.GetEventId(), event.GetAccountId(), event.GetLeaseId(), event.GetDaemonId(), event.GetSessionId(), event.GetAllocationId()} {
		if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
			return errors.New("Relay UsageEvent contains an invalid UUID")
		}
	}
	_, err := relayTransportName(event.GetTransport())
	return err
}

func relayTransportName(transport cloudv1.RelayTransport) (string, error) {
	switch transport {
	case cloudv1.RelayTransport_RELAY_TRANSPORT_UDP:
		return "udp", nil
	case cloudv1.RelayTransport_RELAY_TRANSPORT_TCP:
		return "tcp", nil
	case cloudv1.RelayTransport_RELAY_TRANSPORT_TLS:
		return "tls", nil
	default:
		return "", errors.New("Relay UsageEvent transport is invalid")
	}
}

func monthStart(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, time.UTC)
}
