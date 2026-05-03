package hubregistry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrMachineNotOwned = errors.New("machine is not owned by user")
	ErrAgentNotFound   = errors.New("agent not found")
)

const (
	defaultHubTTL       = time.Minute
	maxHubTTL           = 5 * time.Minute
	maxReportedAgents   = 1024
	forcedOfflineStatus = AgentOffline
)

type Config struct {
	DB    *sql.DB
	Clock Clock
}

type Service struct {
	db    *sql.DB
	clock Clock
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Service{db: cfg.DB, clock: clock}
}

func (s *Service) ReportHub(ctx context.Context, in ReportHubInput) (ReportHubResult, error) {
	if s == nil || s.db == nil {
		return ReportHubResult{}, errors.New("hub registry service is not configured")
	}
	hubID := strings.TrimSpace(in.HubID)
	region := strings.TrimSpace(in.Region)
	httpURL := strings.TrimSpace(in.HTTPURL)
	status := normalizeHubStatus(in.Status)
	if hubID == "" || region == "" || httpURL == "" {
		return ReportHubResult{}, errors.New("hub id, region, and http url are required")
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = defaultHubTTL
	}
	if ttl > maxHubTTL {
		ttl = maxHubTTL
	}
	if len(in.Agents) > maxReportedAgents {
		return ReportHubResult{}, fmt.Errorf("hub report agents exceeds max %d", maxReportedAgents)
	}
	now := s.clock.Now().UTC()
	expiresAt := now.Add(ttl)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReportHubResult{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hubs(id, region, http_url, status, capacity, health_json, last_heartbeat_at, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			region = excluded.region,
			http_url = excluded.http_url,
			status = excluded.status,
			capacity = excluded.capacity,
			health_json = excluded.health_json,
			last_heartbeat_at = excluded.last_heartbeat_at,
			expires_at = excluded.expires_at,
			updated_at = excluded.updated_at
	`, hubID, region, httpURL, status, in.Capacity, strings.TrimSpace(in.Health), formatTime(now), formatTime(expiresAt), formatTime(now), formatTime(now)); err != nil {
		return ReportHubResult{}, fmt.Errorf("upsert hub report: %w", err)
	}
	policies := make([]AgentPolicy, 0, len(in.Agents))
	for _, agent := range in.Agents {
		machineID := strings.TrimSpace(agent.MachineID)
		agentID := strings.TrimSpace(agent.AgentID)
		if machineID == "" || agentID == "" {
			continue
		}
		policy, err := loadAgentPolicyTx(ctx, tx, machineID, agentID)
		if err != nil {
			return ReportHubResult{}, err
		}
		agentStatus := normalizeAgentStatus(agent.Status)
		if policy.ForceOffline {
			agentStatus = forcedOfflineStatus
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO hub_agents(machine_id, agent_id, hub_id, status, terminal_count, last_seen_at, expires_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(machine_id, agent_id) DO UPDATE SET
				hub_id = excluded.hub_id,
				status = excluded.status,
				terminal_count = excluded.terminal_count,
				last_seen_at = excluded.last_seen_at,
				expires_at = excluded.expires_at,
				updated_at = excluded.updated_at
		`, machineID, agentID, hubID, agentStatus, agent.TerminalCount, formatTime(now), formatTime(expiresAt), formatTime(now)); err != nil {
			return ReportHubResult{}, fmt.Errorf("upsert hub agent report: %w", err)
		}
		policies = append(policies, policy)
	}
	if err := tx.Commit(); err != nil {
		return ReportHubResult{}, err
	}
	tx = nil
	return ReportHubResult{
		Hub: Hub{
			ID:              hubID,
			Region:          region,
			HTTPURL:         httpURL,
			Status:          status,
			Capacity:        in.Capacity,
			Health:          strings.TrimSpace(in.Health),
			LastHeartbeatAt: now,
			ExpiresAt:       expiresAt,
		},
		AgentPolicies: policies,
	}, nil
}

func (s *Service) DiscoverHubs(ctx context.Context, in DiscoverHubsInput) ([]Hub, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("hub registry service is not configured")
	}
	now := in.Now.UTC()
	if now.IsZero() {
		now = s.clock.Now().UTC()
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, region, http_url, status, capacity, health_json, last_heartbeat_at, expires_at
		FROM hubs
		WHERE status = ? AND expires_at > ?
		ORDER BY region, id
	`, HubOnline, formatTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hubs []Hub
	for rows.Next() {
		hub, err := scanHub(rows)
		if err != nil {
			return nil, err
		}
		hubs = append(hubs, hub)
	}
	return hubs, rows.Err()
}

func (s *Service) ForceOfflineAgent(ctx context.Context, in ForceOfflineInput) error {
	if s == nil || s.db == nil {
		return errors.New("hub registry service is not configured")
	}
	userID := strings.TrimSpace(in.UserID)
	machineID := strings.TrimSpace(in.MachineID)
	agentID := strings.TrimSpace(in.AgentID)
	if userID == "" || machineID == "" || agentID == "" {
		return errors.New("user id, machine id, and agent id are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if err := requireOwnedMachine(ctx, tx, userID, machineID); err != nil {
		return err
	}
	now := s.clock.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hub_agent_policies(machine_id, agent_id, force_offline, reason, updated_at)
		VALUES (?, ?, 1, ?, ?)
		ON CONFLICT(machine_id, agent_id) DO UPDATE SET
			force_offline = 1,
			reason = excluded.reason,
			updated_at = excluded.updated_at
	`, machineID, agentID, strings.TrimSpace(in.Reason), formatTime(now)); err != nil {
		return fmt.Errorf("force offline agent: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE hub_agents
		SET status = ?, updated_at = ?
		WHERE machine_id = ? AND agent_id = ?
	`, AgentOffline, formatTime(now), machineID, agentID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}

func (s *Service) GetAgentPolicy(ctx context.Context, in AgentPolicyInput) (AgentPolicy, error) {
	if s == nil || s.db == nil {
		return AgentPolicy{}, errors.New("hub registry service is not configured")
	}
	return loadAgentPolicy(ctx, s.db, strings.TrimSpace(in.MachineID), strings.TrimSpace(in.AgentID))
}

func (s *Service) CleanupExpired(ctx context.Context, in CleanupInput) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("hub registry service is not configured")
	}
	now := in.Now.UTC()
	if now.IsZero() {
		now = s.clock.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	agents, err := tx.ExecContext(ctx, `DELETE FROM hub_agents WHERE expires_at <= ?`, formatTime(now))
	if err != nil {
		return 0, err
	}
	hubs, err := tx.ExecContext(ctx, `UPDATE hubs SET status = ?, updated_at = ? WHERE expires_at <= ? AND status != ?`, HubOffline, formatTime(now), formatTime(now), HubOffline)
	if err != nil {
		return 0, err
	}
	agentRows, _ := agents.RowsAffected()
	hubRows, _ := hubs.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	tx = nil
	return agentRows + hubRows, nil
}

type hubScanner interface {
	Scan(dest ...any) error
}

func scanHub(row hubScanner) (Hub, error) {
	var hub Hub
	var lastHeartbeat, expiresAt string
	if err := row.Scan(&hub.ID, &hub.Region, &hub.HTTPURL, &hub.Status, &hub.Capacity, &hub.Health, &lastHeartbeat, &expiresAt); err != nil {
		return Hub{}, err
	}
	var err error
	hub.LastHeartbeatAt, err = time.Parse(time.RFC3339Nano, lastHeartbeat)
	if err != nil {
		return Hub{}, err
	}
	hub.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return Hub{}, err
	}
	return hub, nil
}

func loadAgentPolicy(ctx context.Context, db *sql.DB, machineID string, agentID string) (AgentPolicy, error) {
	return loadAgentPolicyRow(ctx, db.QueryRowContext(ctx, `
		SELECT force_offline, reason
		FROM hub_agent_policies
		WHERE machine_id = ? AND agent_id = ?
	`, machineID, agentID), machineID, agentID)
}

func loadAgentPolicyTx(ctx context.Context, tx *sql.Tx, machineID string, agentID string) (AgentPolicy, error) {
	return loadAgentPolicyRow(ctx, tx.QueryRowContext(ctx, `
		SELECT force_offline, reason
		FROM hub_agent_policies
		WHERE machine_id = ? AND agent_id = ?
	`, machineID, agentID), machineID, agentID)
}

func loadAgentPolicyRow(ctx context.Context, row *sql.Row, machineID string, agentID string) (AgentPolicy, error) {
	_ = ctx
	policy := AgentPolicy{MachineID: machineID, AgentID: agentID}
	var force int
	var reason sql.NullString
	err := row.Scan(&force, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, nil
	}
	if err != nil {
		return AgentPolicy{}, err
	}
	policy.ForceOffline = force != 0
	if reason.Valid {
		policy.Reason = reason.String
	}
	return policy, nil
}

func requireOwnedMachine(ctx context.Context, tx *sql.Tx, userID string, machineID string) error {
	var owner sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT owner_user_id FROM machines WHERE id = ?`, machineID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMachineNotOwned
	}
	if err != nil {
		return err
	}
	if !owner.Valid || owner.String != userID {
		return ErrMachineNotOwned
	}
	return nil
}

func normalizeHubStatus(value string) string {
	if strings.TrimSpace(value) == HubOffline {
		return HubOffline
	}
	return HubOnline
}

func normalizeAgentStatus(value string) string {
	if strings.TrimSpace(value) == AgentOffline {
		return AgentOffline
	}
	return AgentOnline
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
