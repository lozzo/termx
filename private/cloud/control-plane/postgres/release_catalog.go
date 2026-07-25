package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/muxvia/muxvia/private/cloud/control-plane/releasecatalog"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// PublishRelease 原子保存不可变制品 metadata 与运营审计。
func (store *Store) PublishRelease(ctx context.Context, value *cloudpb.ReleaseArtifactProjection, audit *cloudpb.OperatorMutationAuditProjection) error {
	body, err := marshal(value)
	if err != nil || audit == nil {
		return releasecatalog.ErrInvalid
	}
	auditBody, err := marshal(audit)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := execContext(ctx, tx, `INSERT INTO release_artifacts(release_id,product,channel,target_os,target_arch,version_code,published_at,projection) VALUES(?,?,?,?,?,?,?,?)`, value.GetReleaseId(), value.GetProduct(), value.GetChannel(), value.GetOs(), value.GetArch(), value.GetVersionCode(), value.GetPublishedAtUnixMillis(), body); err != nil {
		return releasecatalog.ErrConflict
	}
	if _, err := execContext(ctx, tx, `INSERT INTO operator_mutation_audit(audit_id,account_id,occurred_at,projection) VALUES(?,?,?,?)`, audit.GetAuditId(), "", audit.GetOccurredAtUnixMillis(), auditBody); err != nil {
		return releasecatalog.ErrConflict
	}
	return tx.Commit()
}

// ReleaseArtifact 读取精确 immutable artifact。
func (store *Store) ReleaseArtifact(ctx context.Context, releaseID string) (*cloudpb.ReleaseArtifactProjection, error) {
	var body []byte
	if err := queryRowContext(ctx, store.db, `SELECT projection FROM release_artifacts WHERE release_id=?`, releaseID).Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, releasecatalog.ErrNotFound
		}
		return nil, err
	}
	value := &cloudpb.ReleaseArtifactProjection{}
	if err := proto.Unmarshal(body, value); err != nil {
		return nil, err
	}
	return value, nil
}

// Releases 按发布时间倒序返回过滤后的 immutable artifact。
func (store *Store) Releases(ctx context.Context, product cloudpb.ReleaseProduct, channel cloudpb.ReleaseChannel, targetOS, arch string, limit int) ([]*cloudpb.ReleaseArtifactProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM release_artifacts WHERE (?=0 OR product=?) AND (?=0 OR channel=?) AND (?='' OR target_os=?) AND (?='' OR target_arch=?) ORDER BY published_at DESC,release_id DESC LIMIT ?`, product, product, channel, channel, targetOS, targetOS, arch, arch, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.ReleaseArtifactProjection
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.ReleaseArtifactProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// ReleaseChannel 读取 target channel head。
func (store *Store) ReleaseChannel(ctx context.Context, product cloudpb.ReleaseProduct, channel cloudpb.ReleaseChannel, targetOS, arch string) (*cloudpb.ReleaseChannelProjection, error) {
	value := &cloudpb.ReleaseChannelProjection{Product: product, Channel: channel, Os: targetOS, Arch: arch}
	if err := queryRowContext(ctx, store.db, `SELECT active_release_id,revision,paused,updated_at FROM release_channel_heads WHERE product=? AND channel=? AND target_os=? AND target_arch=?`, product, channel, targetOS, arch).Scan(&value.ActiveReleaseId, &value.Revision, &value.Paused, &value.UpdatedAtUnixMillis); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, releasecatalog.ErrNotFound
		}
		return nil, err
	}
	return value, nil
}

// ReleaseChannels 返回有界 channel heads。
func (store *Store) ReleaseChannels(ctx context.Context, limit int) ([]*cloudpb.ReleaseChannelProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT product,channel,target_os,target_arch,active_release_id,revision,paused,updated_at FROM release_channel_heads ORDER BY product,channel,target_os,target_arch LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.ReleaseChannelProjection
	for rows.Next() {
		value := &cloudpb.ReleaseChannelProjection{}
		if err := rows.Scan(&value.Product, &value.Channel, &value.Os, &value.Arch, &value.ActiveReleaseId, &value.Revision, &value.Paused, &value.UpdatedAtUnixMillis); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// ReleaseAudits 从全局 Operator audit 表读取版本 mutation，不建立第二份 release audit 表。
func (store *Store) ReleaseAudits(ctx context.Context, limit int) ([]*cloudpb.OperatorMutationAuditProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT projection FROM operator_mutation_audit WHERE account_id='' AND audit_id LIKE 'audit_release_%' ORDER BY occurred_at DESC,audit_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.OperatorMutationAuditProjection
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		value := &cloudpb.OperatorMutationAuditProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		if value.GetResourceKind() == "release_artifact" || value.GetResourceKind() == "release_channel" {
			result = append(result, value)
		}
	}
	return result, rows.Err()
}

// SetReleaseChannel 以 revision CAS 切换或暂停 head，并在同一事务写审计。
func (store *Store) SetReleaseChannel(ctx context.Context, value *cloudpb.ReleaseChannelProjection, expected uint64, audit *cloudpb.OperatorMutationAuditProjection) error {
	if value == nil || audit == nil || value.GetRevision() != expected+1 {
		return releasecatalog.ErrInvalid
	}
	auditBody, err := marshal(audit)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var result sql.Result
	if expected == 0 {
		result, err = execContext(ctx, tx, `INSERT INTO release_channel_heads(product,channel,target_os,target_arch,active_release_id,revision,paused,updated_at) VALUES(?,?,?,?,?,?,?,?)`, value.GetProduct(), value.GetChannel(), value.GetOs(), value.GetArch(), value.GetActiveReleaseId(), value.GetRevision(), value.GetPaused(), value.GetUpdatedAtUnixMillis())
	} else {
		result, err = execContext(ctx, tx, `UPDATE release_channel_heads SET active_release_id=?,revision=?,paused=?,updated_at=? WHERE product=? AND channel=? AND target_os=? AND target_arch=? AND revision=?`, value.GetActiveReleaseId(), value.GetRevision(), value.GetPaused(), value.GetUpdatedAtUnixMillis(), value.GetProduct(), value.GetChannel(), value.GetOs(), value.GetArch(), expected)
	}
	if err != nil {
		return releasecatalog.ErrConflict
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return releasecatalog.ErrConflict
	}
	if _, err := execContext(ctx, tx, `INSERT INTO operator_mutation_audit(audit_id,account_id,occurred_at,projection) VALUES(?,?,?,?)`, audit.GetAuditId(), "", audit.GetOccurredAtUnixMillis(), auditBody); err != nil {
		return releasecatalog.ErrConflict
	}
	return tx.Commit()
}
