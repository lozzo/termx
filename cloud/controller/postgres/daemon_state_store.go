package postgres

import (
	"context"
	"errors"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/jackc/pgx/v5"
)

// DaemonStateSnapshot returns durable lifecycle truth without runtime topology.
func (database *Database) DaemonStateSnapshot(ctx context.Context) (*cloudv1.DaemonStateSnapshot, error) {
	rows, err := database.pool.Query(ctx, `SELECT daemon_id::text,state,state_revision FROM daemons ORDER BY daemon_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	snapshot := &cloudv1.DaemonStateSnapshot{}
	for rows.Next() {
		var id, state string
		var revision uint64
		if err := rows.Scan(&id, &state, &revision); err != nil {
			return nil, err
		}
		record := &cloudv1.DaemonStateRecord{DaemonId: id, State: parseDaemonState(state), StateRevision: revision}
		if !validDaemonStateRecord(record) {
			return nil, errors.New("database contains invalid daemon state")
		}
		snapshot.Daemons = append(snapshot.Daemons, record)
	}
	return snapshot, rows.Err()
}

// ResolveDaemonState returns one lifecycle record for Edge AgentGateway admission.
func (database *Database) ResolveDaemonState(ctx context.Context, daemonID string) (*cloudv1.DaemonStateRecord, bool, error) {
	var state string
	var revision uint64
	err := database.pool.QueryRow(ctx, `SELECT state,state_revision FROM daemons WHERE daemon_id=$1`, daemonID).Scan(&state, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	record := &cloudv1.DaemonStateRecord{DaemonId: daemonID, State: parseDaemonState(state), StateRevision: revision}
	if !validDaemonStateRecord(record) {
		return nil, false, errors.New("database contains invalid daemon state")
	}
	return record, true, nil
}

func validDaemonStateRecord(record *cloudv1.DaemonStateRecord) bool {
	return record != nil && record.GetDaemonId() != "" && record.GetStateRevision() > 0 &&
		(record.GetState() == cloudv1.DaemonState_DAEMON_STATE_ACTIVE || record.GetState() == cloudv1.DaemonState_DAEMON_STATE_BLOCKED || record.GetState() == cloudv1.DaemonState_DAEMON_STATE_DELETED)
}
