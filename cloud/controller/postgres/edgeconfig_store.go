package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anytty/anytty/cloud/controller/certificate"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

// ListEdges 实现 edgeconfig.Store，返回每个 Edge 当前持久配置及签名版本。
func (database *Database) ListEdges(ctx context.Context) ([]edgeconfig.Edge, error) {
	rows, err := database.pool.Query(ctx, edgeSelect+` ORDER BY deployment.created_at, deployment.edge_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]edgeconfig.Edge, 0)
	for rows.Next() {
		edge, err := scanEdge(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, edge)
	}
	return result, rows.Err()
}

// GetEdge 实现 edgeconfig.Store，不从实时 Directory 回填持久字段。
func (database *Database) GetEdge(ctx context.Context, edgeID string) (edgeconfig.Edge, error) {
	return scanEdge(database.pool.QueryRow(ctx, edgeSelect+` WHERE deployment.edge_id=$1`, edgeID))
}

// CreateEdge 在一个事务中写入 deployment、不可变 config version 和 install claim 摘要。
func (database *Database) CreateEdge(ctx context.Context, edge edgeconfig.Edge, claimDigest []byte, claimExpiresAt time.Time) error {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO edge_deployments(edge_id,name,region,capacity,public_endpoint,enabled,desired_config_version,revision,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, edge.ID, edge.Name, edge.Region, edge.Capacity, edge.PublicEndpoint, edge.Enabled, edge.ConfigVersion, edge.Revision, edge.CreatedAt, edge.UpdatedAt); err != nil {
		return err
	}
	if err := insertConfigVersion(ctx, tx, edge); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO edge_claim_tokens(token_digest,edge_id,purpose,expires_at,created_at) VALUES($1,$2,'install',$3,$4)`, claimDigest, edge.ID, claimExpiresAt, edge.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateEdge 锁定 Edge 后使用 revision CAS，并在同一事务校验当前证书绑定和插入新 config version。
func (database *Database) UpdateEdge(ctx context.Context, input edgeconfig.UpdateInput, updated edgeconfig.Edge) error {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var currentRevision uint64
	err = tx.QueryRow(ctx, `SELECT revision FROM edge_deployments WHERE edge_id=$1 FOR UPDATE`, updated.ID).Scan(&currentRevision)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && currentRevision != input.ExpectedRevision) {
		return edgeconfig.ErrRevisionConflict
	}
	if err != nil {
		return err
	}
	var boundProfile certificate.Profile
	err = tx.QueryRow(ctx, `SELECT certificate_profile_id::text FROM edge_certificate_bindings WHERE edge_id=$1`, updated.ID).Scan(&boundProfile.ID)
	if err == nil {
		if err := tx.QueryRow(ctx, `SELECT dns_names FROM certificate_profiles WHERE certificate_profile_id=$1 FOR SHARE`, boundProfile.ID).Scan(&boundProfile.DNSNames); err != nil {
			return err
		}
		if err := certificate.VerifyEndpoint(boundProfile, updated.PublicEndpoint); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE edge_deployments SET name=$1,region=$2,capacity=$3,public_endpoint=$4,enabled=$5,desired_config_version=$6,revision=$7,updated_at=$8 WHERE edge_id=$9 AND revision=$10`, updated.Name, updated.Region, updated.Capacity, updated.PublicEndpoint, updated.Enabled, updated.ConfigVersion, updated.Revision, updated.UpdatedAt, updated.ID, input.ExpectedRevision)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return edgeconfig.ErrRevisionConflict
	}
	if err := insertConfigVersion(ctx, tx, updated); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteEdge 原子校验 revision/停用状态、删除 deployment，并保留独立审计记录。
func (database *Database) DeleteEdge(ctx context.Context, input edgeconfig.DeleteInput) error {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var enabled bool
	var revision uint64
	err = tx.QueryRow(ctx, `SELECT enabled,revision FROM edge_deployments WHERE edge_id=$1 FOR UPDATE`, input.EdgeID).Scan(&enabled, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return edgeconfig.ErrEdgeNotFound
	}
	if err != nil {
		return err
	}
	if revision != input.ExpectedRevision {
		return edgeconfig.ErrRevisionConflict
	}
	if enabled {
		return edgeconfig.ErrEdgeEnabled
	}
	if _, err := tx.Exec(ctx, `DELETE FROM edge_deployments WHERE edge_id=$1`, input.EdgeID); err != nil {
		return err
	}
	if err := insertOperatorAudit(ctx, tx, input.ActorID, "edge.delete", "edge", input.EdgeID, input.Reason, "applied", input.DeletedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ConsumeInstallClaim 原子消费 URL claim 并创建同 Edge 的 bootstrap claim 摘要。
func (database *Database) ConsumeInstallClaim(ctx context.Context, installDigest, bootstrapDigest []byte, bootstrapExpiresAt time.Time) (edgeconfig.Edge, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return edgeconfig.Edge{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var edgeID string
	if err := tx.QueryRow(ctx, `UPDATE edge_claim_tokens SET consumed_at=now() WHERE token_digest=$1 AND purpose='install' AND consumed_at IS NULL AND expires_at>now() RETURNING edge_id`, installDigest).Scan(&edgeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return edgeconfig.Edge{}, edgeconfig.ErrClaimInvalid
		}
		return edgeconfig.Edge{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO edge_claim_tokens(token_digest,edge_id,purpose,expires_at,created_at) VALUES($1,$2,'bootstrap',$3,now())`, bootstrapDigest, edgeID, bootstrapExpiresAt); err != nil {
		return edgeconfig.Edge{}, err
	}
	edge, err := scanEdge(tx.QueryRow(ctx, edgeSelect+` WHERE deployment.edge_id=$1`, edgeID))
	if err != nil {
		return edgeconfig.Edge{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return edgeconfig.Edge{}, err
	}
	return edge, nil
}

// ConsumeBootstrapClaim 在同一事务消费 bootstrap 并绑定已校验 CSR 摘要。
func (database *Database) ConsumeBootstrapClaim(ctx context.Context, bootstrapDigest []byte, edgeID string, csrDigest []byte) (edgeconfig.Edge, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return edgeconfig.Edge{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var claimedEdgeID string
	if err := tx.QueryRow(ctx, `UPDATE edge_claim_tokens SET consumed_at=now() WHERE token_digest=$1 AND edge_id=$2 AND purpose='bootstrap' AND consumed_at IS NULL AND expires_at>now() RETURNING edge_id`, bootstrapDigest, edgeID).Scan(&claimedEdgeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return edgeconfig.Edge{}, edgeconfig.ErrClaimInvalid
		}
		return edgeconfig.Edge{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE edge_deployments SET identity_csr_sha256=$1,updated_at=now() WHERE edge_id=$2`, csrDigest, claimedEdgeID); err != nil {
		return edgeconfig.Edge{}, err
	}
	edge, err := scanEdge(tx.QueryRow(ctx, edgeSelect+` WHERE deployment.edge_id=$1`, claimedEdgeID))
	if err != nil {
		return edgeconfig.Edge{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return edgeconfig.Edge{}, err
	}
	return edge, nil
}

const edgeSelect = `SELECT deployment.edge_id::text,deployment.name,deployment.region,deployment.capacity,deployment.public_endpoint,deployment.enabled,deployment.desired_config_version,deployment.revision,deployment.created_at,deployment.updated_at,config.key_id,config.payload,config.signature FROM edge_deployments deployment JOIN edge_config_versions config ON config.edge_id=deployment.edge_id AND config.version=deployment.desired_config_version`

type rowScanner interface{ Scan(...any) error }

func scanEdge(row rowScanner) (edgeconfig.Edge, error) {
	var edge edgeconfig.Edge
	var keyID string
	var payload, signature []byte
	if err := row.Scan(&edge.ID, &edge.Name, &edge.Region, &edge.Capacity, &edge.PublicEndpoint, &edge.Enabled, &edge.ConfigVersion, &edge.Revision, &edge.CreatedAt, &edge.UpdatedAt, &keyID, &payload, &signature); err != nil {
		return edgeconfig.Edge{}, err
	}
	edge.SignedConfig = &cloudv1.SignedEdgeDesiredConfig{KeyId: keyID, Payload: payload, Signature: signature}
	return edge, nil
}

func insertConfigVersion(ctx context.Context, tx pgx.Tx, edge edgeconfig.Edge) error {
	if edge.SignedConfig == nil {
		return errors.New("signed Edge config is required")
	}
	if _, err := proto.Marshal(edge.SignedConfig); err != nil {
		return fmt.Errorf("validate signed Edge config: %w", err)
	}
	_, err := tx.Exec(ctx, `INSERT INTO edge_config_versions(edge_id,version,key_id,payload,signature,created_at) VALUES($1,$2,$3,$4,$5,$6)`, edge.ID, edge.ConfigVersion, edge.SignedConfig.GetKeyId(), edge.SignedConfig.GetPayload(), edge.SignedConfig.GetSignature(), edge.UpdatedAt)
	return err
}
