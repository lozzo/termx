package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/muxvia/muxvia/cloud/controller/certificate"
	"github.com/muxvia/muxvia/cloud/controller/edgeconfig"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
)

const certificateProfileSelect = `SELECT certificate_profile_id::text,name,dns_names,sha256_fingerprint,not_before,not_after,revision,secret_ref,created_at,updated_at FROM certificate_profiles`

const certificateBindingSelect = `SELECT binding.edge_id::text,edge.name,edge.public_endpoint,binding.certificate_profile_id::text,profile.name,binding.binding_revision,binding.desired_revision,coalesce(binding.applied_profile_id::text,''),binding.applied_revision,binding.last_error_code,binding.last_error_message,binding.applied_at,binding.updated_at FROM edge_certificate_bindings binding JOIN edge_deployments edge ON edge.edge_id=binding.edge_id JOIN certificate_profiles profile ON profile.certificate_profile_id=binding.certificate_profile_id`

// ListCertificateProfiles 返回全部当前档案及绑定；查询结果不包含 secret 内容。
func (database *Database) ListCertificateProfiles(ctx context.Context) ([]certificate.Profile, error) {
	rows, err := database.pool.Query(ctx, certificateProfileSelect+` ORDER BY created_at,certificate_profile_id`)
	if err != nil {
		return nil, err
	}
	profiles := make([]certificate.Profile, 0)
	byID := make(map[string]int)
	for rows.Next() {
		profile, err := scanCertificateProfile(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		byID[profile.ID] = len(profiles)
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	bindingRows, err := database.pool.Query(ctx, certificateBindingSelect+` ORDER BY binding.edge_id`)
	if err != nil {
		return nil, err
	}
	defer bindingRows.Close()
	for bindingRows.Next() {
		binding, err := scanCertificateBinding(bindingRows)
		if err != nil {
			return nil, err
		}
		if index, ok := byID[binding.ProfileID]; ok {
			profiles[index].Bindings = append(profiles[index].Bindings, binding)
		}
	}
	return profiles, bindingRows.Err()
}

// GetCertificateProfile 返回当前档案元数据和内部 secret 引用。
func (database *Database) GetCertificateProfile(ctx context.Context, profileID string) (certificate.Profile, error) {
	profile, err := scanCertificateProfile(database.pool.QueryRow(ctx, certificateProfileSelect+` WHERE certificate_profile_id=$1`, profileID))
	if errors.Is(err, pgx.ErrNoRows) {
		return certificate.Profile{}, certificate.ErrNotFound
	}
	return profile, err
}

// CreateCertificateProfile 在一个事务中写入档案元数据和无 secret 的运营审计。
func (database *Database) CreateCertificateProfile(ctx context.Context, profile certificate.Profile, actorID string) error {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO certificate_profiles(certificate_profile_id,name,dns_names,sha256_fingerprint,not_before,not_after,revision,secret_ref,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, profile.ID, profile.Name, profile.DNSNames, profile.Fingerprint, profile.NotBefore, profile.NotAfter, profile.Revision, profile.SecretRef, profile.CreatedAt, profile.UpdatedAt); err != nil {
		return err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "certificate.create", "certificate_profile", profile.ID, "上传证书链与私钥", "applied", profile.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReplaceCertificateProfile 使用 revision CAS 替换当前 secret 引用，并原子推进全部绑定 desired revision。
func (database *Database) ReplaceCertificateProfile(ctx context.Context, expectedRevision uint64, profile certificate.Profile, actorID string) (string, []certificate.Binding, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var oldSecretRef string
	var currentRevision uint64
	err = tx.QueryRow(ctx, `SELECT secret_ref,revision FROM certificate_profiles WHERE certificate_profile_id=$1 FOR UPDATE`, profile.ID).Scan(&oldSecretRef, &currentRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, certificate.ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	if currentRevision != expectedRevision || profile.Revision != expectedRevision+1 {
		return "", nil, certificate.ErrRevisionConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE certificate_profiles SET name=$1,dns_names=$2,sha256_fingerprint=$3,not_before=$4,not_after=$5,revision=$6,secret_ref=$7,updated_at=$8 WHERE certificate_profile_id=$9`, profile.Name, profile.DNSNames, profile.Fingerprint, profile.NotBefore, profile.NotAfter, profile.Revision, profile.SecretRef, profile.UpdatedAt, profile.ID); err != nil {
		return "", nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE edge_certificate_bindings SET desired_revision=$1,last_error_code='',last_error_message='',updated_at=$2 WHERE certificate_profile_id=$3`, profile.Revision, profile.UpdatedAt, profile.ID); err != nil {
		return "", nil, err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "certificate.replace", "certificate_profile", profile.ID, "替换证书链与私钥", "applied", profile.UpdatedAt); err != nil {
		return "", nil, err
	}
	rows, err := tx.Query(ctx, certificateBindingSelect+` WHERE binding.certificate_profile_id=$1 ORDER BY binding.edge_id`, profile.ID)
	if err != nil {
		return "", nil, err
	}
	bindings := make([]certificate.Binding, 0)
	for rows.Next() {
		binding, err := scanCertificateBinding(rows)
		if err != nil {
			rows.Close()
			return "", nil, err
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", nil, err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return "", nil, err
	}
	return oldSecretRef, bindings, nil
}

// GetCertificateBinding 返回指定 Edge 的当前持久绑定。
func (database *Database) GetCertificateBinding(ctx context.Context, edgeID string) (certificate.Binding, bool, error) {
	binding, err := scanCertificateBinding(database.pool.QueryRow(ctx, certificateBindingSelect+` WHERE binding.edge_id=$1`, edgeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return certificate.Binding{}, false, nil
	}
	return binding, err == nil, err
}

// BindCertificateProfile 锁定当前 Edge 与档案后创建或 CAS 更新证书选择。
// 事务外读取的 revision 只作为 optimistic fence，不能覆盖事务内的域名或档案真值。
func (database *Database) BindCertificateProfile(ctx context.Context, edge edgeconfig.Edge, profile certificate.Profile, expectedRevision uint64, actorID string, now time.Time) (certificate.Binding, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return certificate.Binding{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var currentEndpoint string
	var currentEdgeRevision uint64
	err = tx.QueryRow(ctx, `SELECT public_endpoint,revision FROM edge_deployments WHERE edge_id=$1 FOR UPDATE`, edge.ID).Scan(&currentEndpoint, &currentEdgeRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return certificate.Binding{}, certificate.ErrNotFound
	}
	if err != nil {
		return certificate.Binding{}, err
	}
	if currentEdgeRevision != edge.Revision {
		return certificate.Binding{}, certificate.ErrRevisionConflict
	}
	currentProfile := certificate.Profile{ID: profile.ID}
	err = tx.QueryRow(ctx, `SELECT dns_names,revision FROM certificate_profiles WHERE certificate_profile_id=$1 FOR SHARE`, profile.ID).Scan(&currentProfile.DNSNames, &currentProfile.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return certificate.Binding{}, certificate.ErrNotFound
	}
	if err != nil {
		return certificate.Binding{}, err
	}
	if currentProfile.Revision != profile.Revision {
		return certificate.Binding{}, certificate.ErrRevisionConflict
	}
	if err := certificate.VerifyEndpoint(currentProfile, currentEndpoint); err != nil {
		return certificate.Binding{}, err
	}
	var current uint64
	err = tx.QueryRow(ctx, `SELECT binding_revision FROM edge_certificate_bindings WHERE edge_id=$1 FOR UPDATE`, edge.ID).Scan(&current)
	switch {
	case errors.Is(err, pgx.ErrNoRows) && expectedRevision == 0:
		_, err = tx.Exec(ctx, `INSERT INTO edge_certificate_bindings(edge_id,certificate_profile_id,binding_revision,desired_revision,updated_at) VALUES($1,$2,1,$3,$4)`, edge.ID, profile.ID, profile.Revision, now)
	case err == nil && current == expectedRevision && expectedRevision > 0:
		_, err = tx.Exec(ctx, `UPDATE edge_certificate_bindings SET certificate_profile_id=$1,binding_revision=binding_revision+1,desired_revision=$2,last_error_code='',last_error_message='',updated_at=$3 WHERE edge_id=$4`, profile.ID, profile.Revision, now, edge.ID)
	case err == nil || errors.Is(err, pgx.ErrNoRows):
		return certificate.Binding{}, certificate.ErrRevisionConflict
	}
	if err != nil {
		return certificate.Binding{}, err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "certificate.bind", "edge", edge.ID, "绑定证书档案 "+profile.ID, "applied", now); err != nil {
		return certificate.Binding{}, err
	}
	binding, err := scanCertificateBinding(tx.QueryRow(ctx, certificateBindingSelect+` WHERE binding.edge_id=$1`, edge.ID))
	if err != nil {
		return certificate.Binding{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return certificate.Binding{}, err
	}
	return binding, nil
}

// UnbindCertificateProfile 使用 revision CAS 删除绑定；Edge 当前已加载证书不被远程清除。
func (database *Database) UnbindCertificateProfile(ctx context.Context, edgeID string, expectedRevision uint64, actorID string, now time.Time) (certificate.Binding, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return certificate.Binding{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var edgeExists bool
	err = tx.QueryRow(ctx, `SELECT true FROM edge_deployments WHERE edge_id=$1 FOR UPDATE`, edgeID).Scan(&edgeExists)
	if errors.Is(err, pgx.ErrNoRows) {
		return certificate.Binding{}, certificate.ErrNotFound
	}
	if err != nil {
		return certificate.Binding{}, err
	}
	binding, err := scanCertificateBinding(tx.QueryRow(ctx, certificateBindingSelect+` WHERE binding.edge_id=$1 FOR UPDATE OF binding`, edgeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return certificate.Binding{}, certificate.ErrNotFound
	}
	if err != nil {
		return certificate.Binding{}, err
	}
	if expectedRevision == 0 || binding.BindingRevision != expectedRevision {
		return certificate.Binding{}, certificate.ErrRevisionConflict
	}
	if _, err := tx.Exec(ctx, `DELETE FROM edge_certificate_bindings WHERE edge_id=$1 AND binding_revision=$2`, edgeID, expectedRevision); err != nil {
		return certificate.Binding{}, err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "certificate.unbind", "edge", edgeID, "解除证书档案绑定", "applied", now); err != nil {
		return certificate.Binding{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return certificate.Binding{}, err
	}
	binding.ProfileID, binding.ProfileName = "", ""
	binding.BindingRevision++
	binding.DesiredRevision = 0
	binding.UpdatedAt = now
	return binding, nil
}

// RecordCertificateApplied 只接受当前 desired profile/revision 的回执，迟到回执不会覆盖新配置。
func (database *Database) RecordCertificateApplied(ctx context.Context, edgeID string, applied *cloudv1.CertificateApplied, now time.Time) error {
	if applied == nil || applied.GetCertificateProfileId() == "" || applied.GetRevision() == 0 {
		return errors.New("certificate applied result is invalid")
	}
	if applied.GetApplied() {
		_, err := database.pool.Exec(ctx, `UPDATE edge_certificate_bindings SET applied_profile_id=$1,applied_revision=$2,last_error_code='',last_error_message='',applied_at=$3,updated_at=$3 WHERE edge_id=$4 AND certificate_profile_id=$1 AND desired_revision=$2`, applied.GetCertificateProfileId(), applied.GetRevision(), now, edgeID)
		return err
	}
	_, err := database.pool.Exec(ctx, `UPDATE edge_certificate_bindings SET last_error_code=$1,last_error_message=$2,updated_at=$3 WHERE edge_id=$4 AND certificate_profile_id=$5 AND desired_revision=$6`, applied.GetErrorCode(), applied.GetErrorMessage(), now, edgeID, applied.GetCertificateProfileId(), applied.GetRevision())
	return err
}

func scanCertificateProfile(row rowScanner) (certificate.Profile, error) {
	var profile certificate.Profile
	err := row.Scan(&profile.ID, &profile.Name, &profile.DNSNames, &profile.Fingerprint, &profile.NotBefore, &profile.NotAfter, &profile.Revision, &profile.SecretRef, &profile.CreatedAt, &profile.UpdatedAt)
	return profile, err
}

func scanCertificateBinding(row rowScanner) (certificate.Binding, error) {
	var binding certificate.Binding
	var appliedAt sql.NullTime
	err := row.Scan(&binding.EdgeID, &binding.EdgeName, &binding.PublicEndpoint, &binding.ProfileID, &binding.ProfileName, &binding.BindingRevision, &binding.DesiredRevision, &binding.AppliedProfileID, &binding.AppliedRevision, &binding.LastErrorCode, &binding.LastErrorMessage, &appliedAt, &binding.UpdatedAt)
	if appliedAt.Valid {
		binding.AppliedAt = appliedAt.Time.UTC()
	}
	return binding, err
}
