package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"

	"github.com/muxvia/muxvia/private/cloud/control-plane/catalog"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// PublishCatalog 原子插入不可变 release 并切换 active head；版本必须严格递增。
func (store *Store) PublishCatalog(ctx context.Context, release *cloudpb.PlanCatalogReleaseProjection) error {
	if release == nil || release.GetCatalog() == nil || release.GetCatalog().GetCatalogVersion() == 0 || release.GetRequestId() == "" {
		return catalog.ErrInvalid
	}
	body, err := marshal(release)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(5589538654210)`); err != nil {
		return err
	}
	var current uint64
	err = queryRowContext(ctx, tx, `SELECT catalog_version FROM plan_catalog_head WHERE singleton=1 FOR UPDATE`).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	version := release.GetCatalog().GetCatalogVersion()
	if current >= version {
		return catalog.ErrConflict
	}
	if _, err = execContext(ctx, tx, `INSERT INTO plan_catalog_releases(catalog_version,request_id,published_at,projection) VALUES(?,?,?,?)`, version, release.GetRequestId(), release.GetPublishedAtUnixMillis(), body); err != nil {
		return catalog.ErrConflict
	}
	for _, plan := range release.GetCatalog().GetPlans() {
		planBody, marshalErr := marshal(plan)
		if marshalErr != nil {
			return marshalErr
		}
		var existing []byte
		planErr := queryRowContext(ctx, tx, `SELECT projection FROM plan_definitions WHERE plan_id=? AND plan_version=?`, plan.GetPlanId(), plan.GetPlanVersion()).Scan(&existing)
		switch {
		case errors.Is(planErr, sql.ErrNoRows):
			if _, err := execContext(ctx, tx, `INSERT INTO plan_definitions(plan_id,plan_version,projection) VALUES(?,?,?)`, plan.GetPlanId(), plan.GetPlanVersion(), planBody); err != nil {
				return catalog.ErrConflict
			}
		case planErr != nil:
			return planErr
		case !bytes.Equal(existing, planBody):
			return catalog.ErrConflict
		}
		if _, err := execContext(ctx, tx, `INSERT INTO plan_catalog_release_plans(catalog_version,plan_id,plan_version) VALUES(?,?,?)`, version, plan.GetPlanId(), plan.GetPlanVersion()); err != nil {
			return catalog.ErrConflict
		}
	}
	if current == 0 {
		_, err = execContext(ctx, tx, `INSERT INTO plan_catalog_head(singleton,catalog_version,revision) VALUES(1,?,1)`, version)
	} else {
		_, err = execContext(ctx, tx, `UPDATE plan_catalog_head SET catalog_version=?,revision=revision+1 WHERE singleton=1 AND catalog_version=?`, version, current)
	}
	if err != nil {
		return catalog.ErrConflict
	}
	return tx.Commit()
}

// CatalogPlan 按 canonical plan_id/version 直接读取历史套餐，不受 release 数量限制。
func (store *Store) CatalogPlan(ctx context.Context, planID string, planVersion uint64) (*cloudpb.PlanDefinition, error) {
	var body []byte
	if err := queryRowContext(ctx, store.db, `SELECT projection FROM plan_definitions WHERE plan_id=? AND plan_version=?`, planID, planVersion).Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, catalog.ErrNotFound
		}
		return nil, err
	}
	value := &cloudpb.PlanDefinition{}
	if err := proto.Unmarshal(body, value); err != nil {
		return nil, err
	}
	return value, nil
}

// ActiveCatalog 返回 head 指向的发布快照，并在读取时投影 active=true。
func (store *Store) ActiveCatalog(ctx context.Context) (*cloudpb.PlanCatalogReleaseProjection, error) {
	var body []byte
	if err := queryRowContext(ctx, store.db, `SELECT releases.projection FROM plan_catalog_releases releases JOIN plan_catalog_head head ON head.catalog_version=releases.catalog_version WHERE head.singleton=1`).Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, catalog.ErrNotFound
		}
		return nil, err
	}
	value := &cloudpb.PlanCatalogReleaseProjection{}
	if err := proto.Unmarshal(body, value); err != nil {
		return nil, err
	}
	value.Active = true
	return value, nil
}

// CatalogRelease 返回精确版本并根据当前 head 计算 active 标记。
func (store *Store) CatalogRelease(ctx context.Context, version uint64) (*cloudpb.PlanCatalogReleaseProjection, error) {
	var body []byte
	var active bool
	if err := queryRowContext(ctx, store.db, `SELECT releases.projection,COALESCE(head.catalog_version=releases.catalog_version,FALSE) FROM plan_catalog_releases releases LEFT JOIN plan_catalog_head head ON head.singleton=1 WHERE releases.catalog_version=?`, version).Scan(&body, &active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, catalog.ErrNotFound
		}
		return nil, err
	}
	value := &cloudpb.PlanCatalogReleaseProjection{}
	if err := proto.Unmarshal(body, value); err != nil {
		return nil, err
	}
	value.Active = active
	return value, nil
}

// CatalogReleases 按版本倒序返回不可变历史，并标出当前 head。
func (store *Store) CatalogReleases(ctx context.Context, limit int) ([]*cloudpb.PlanCatalogReleaseProjection, error) {
	rows, err := queryContext(ctx, store.db, `SELECT releases.projection,COALESCE(head.catalog_version=releases.catalog_version,FALSE) FROM plan_catalog_releases releases LEFT JOIN plan_catalog_head head ON head.singleton=1 ORDER BY releases.catalog_version DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*cloudpb.PlanCatalogReleaseProjection
	for rows.Next() {
		var body []byte
		var active bool
		if err := rows.Scan(&body, &active); err != nil {
			return nil, err
		}
		value := &cloudpb.PlanCatalogReleaseProjection{}
		if err := proto.Unmarshal(body, value); err != nil {
			return nil, err
		}
		value.Active = active
		result = append(result, value)
	}
	return result, rows.Err()
}
