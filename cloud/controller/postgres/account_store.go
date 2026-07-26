package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/muxvia/muxvia/cloud/controller/account"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CreateAccount 在同一事务创建账号、角色、基础订阅和首个 session。
func (database *Database) CreateAccount(ctx context.Context, record account.Record, session account.Session) error {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertAccount(ctx, tx, record); err != nil {
		return err
	}
	if err := insertStarterSubscription(ctx, tx, record.Profile.GetAccountId(), record.Profile.GetCreatedAt().AsTime()); err != nil {
		return err
	}
	if err := insertAccountSession(ctx, tx, session); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// EnsureBootstrapOperator 首次创建部署管理员；存在时只读取，不在重启时修改 verifier。
func (database *Database) EnsureBootstrapOperator(ctx context.Context, desired account.Record) (account.Record, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return account.Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := scanAccountRecord(tx.QueryRow(ctx, accountSelect+` WHERE lower(a.email)=lower($1)`, desired.Profile.GetEmail()))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return account.Record{}, err
		}
		return record, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return account.Record{}, err
	}
	if err := insertAccount(ctx, tx, desired); err != nil {
		return account.Record{}, err
	}
	if err := insertStarterSubscription(ctx, tx, desired.Profile.GetAccountId(), desired.Profile.GetCreatedAt().AsTime()); err != nil {
		return account.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return account.Record{}, err
	}
	return desired, nil
}

// AccountByLogin 按规范化 email/部署 login 查询账号和完整角色集合。
func (database *Database) AccountByLogin(ctx context.Context, login string) (account.Record, error) {
	return scanAccountRecord(database.pool.QueryRow(ctx, accountSelect+` WHERE lower(a.email)=lower($1)`, login))
}

// AccountByID 返回账号 credential 记录；不读取实时 daemon 或 Edge 状态。
func (database *Database) AccountByID(ctx context.Context, accountID string) (account.Record, error) {
	return scanAccountRecord(database.pool.QueryRow(ctx, accountSelect+` WHERE a.account_id=$1`, accountID))
}

// PutSession 持久化 token 摘要；原始 token 不进入数据库。
func (database *Database) PutSession(ctx context.Context, session account.Session) error {
	_, err := database.pool.Exec(ctx, accountSessionInsert, session.ID, session.AccountID, session.AccessDigest[:], session.RefreshDigest[:], session.CSRFDigest[:], session.AccessExpiresAt, session.RefreshExpiresAt, nullTime(session.RecentAuthExpiresAt), session.Revision, session.CreatedAt)
	return err
}

// SessionByAccessDigest 读取 Web/bearer access session。
func (database *Database) SessionByAccessDigest(ctx context.Context, digest [sha256.Size]byte) (account.Session, error) {
	return scanAccountSession(database.pool.QueryRow(ctx, accountSessionSelect+` WHERE access_token_digest=$1`, digest[:]))
}

// SessionByRefreshDigest 读取可单次轮换的 refresh session。
func (database *Database) SessionByRefreshDigest(ctx context.Context, digest [sha256.Size]byte) (account.Session, error) {
	return scanAccountSession(database.pool.QueryRow(ctx, accountSessionSelect+` WHERE refresh_token_digest=$1`, digest[:]))
}

// RotateSession 在同一事务 CAS 撤销旧 session 并插入新 session。
func (database *Database) RotateSession(ctx context.Context, previous, next account.Session) error {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `UPDATE account_sessions SET revoked_at=now(),revision=revision+1 WHERE session_id=$1 AND account_id=$2 AND revoked_at IS NULL AND revision=$3`, previous.ID, previous.AccountID, previous.Revision)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return account.ErrAccountConflict
	}
	if err := insertAccountSession(ctx, tx, next); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RevokeSession 撤销当前账号的精确 session 或所有活动 session。
func (database *Database) RevokeSession(ctx context.Context, accountID, sessionID string, all bool) error {
	if all {
		result, err := database.pool.Exec(ctx, `UPDATE account_sessions SET revoked_at=now(),revision=revision+1 WHERE account_id=$1 AND revoked_at IS NULL`, accountID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return account.ErrAccountConflict
		}
		return nil
	}
	result, err := database.pool.Exec(ctx, `UPDATE account_sessions SET revoked_at=now(),revision=revision+1 WHERE account_id=$1 AND session_id=$2 AND revoked_at IS NULL`, accountID, sessionID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return account.ErrAccountConflict
	}
	return nil
}

// SetRecentAuthentication 更新精确 session 的高风险操作窗口。
func (database *Database) SetRecentAuthentication(ctx context.Context, sessionID string, expiresAt time.Time) error {
	result, err := database.pool.Exec(ctx, `UPDATE account_sessions SET recent_auth_expires_at=$1,revision=revision+1 WHERE session_id=$2 AND revoked_at IS NULL`, expiresAt, sessionID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return account.ErrUnauthenticated
	}
	return nil
}

const accountSelect = `SELECT a.account_id::text,coalesce(a.email,''),a.display_name,a.state,a.revision,a.created_at,a.updated_at,a.password_hash,ARRAY(SELECT r.role FROM account_roles r WHERE r.account_id=a.account_id ORDER BY r.role) FROM accounts a`

func insertAccount(ctx context.Context, tx pgx.Tx, record account.Record) error {
	profile := record.Profile
	if profile == nil {
		return errors.New("account profile is required")
	}
	_, err := tx.Exec(ctx, `INSERT INTO accounts(account_id,email,password_hash,display_name,state,revision,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, profile.GetAccountId(), profile.GetEmail(), record.PasswordHash, profile.GetDisplayName(), accountStateName(profile.GetState()), profile.GetRevision(), profile.GetCreatedAt().AsTime(), profile.GetUpdatedAt().AsTime())
	if err != nil {
		return err
	}
	for _, role := range record.Roles {
		if _, err := tx.Exec(ctx, `INSERT INTO account_roles(account_id,role,created_at) VALUES($1,$2,$3)`, profile.GetAccountId(), accountRoleName(role), profile.GetCreatedAt().AsTime()); err != nil {
			return err
		}
	}
	return nil
}

func insertStarterSubscription(ctx context.Context, tx pgx.Tx, accountID string, now time.Time) error {
	periodStart := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	_, err := tx.Exec(ctx, `INSERT INTO subscriptions(subscription_id,account_id,plan_id,plan_version,state,cancel_at_period_end,period_start,period_end,revision,updated_at) VALUES(md5($1::text || ':starter')::uuid,$1::uuid,'starter',1,'active',false,$2,$3,1,$4)`, accountID, periodStart, periodStart.AddDate(0, 1, 0), now)
	return err
}

const accountSessionInsert = `INSERT INTO account_sessions(session_id,account_id,access_token_digest,refresh_token_digest,csrf_token_digest,access_expires_at,refresh_expires_at,recent_auth_expires_at,revision,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`

func insertAccountSession(ctx context.Context, tx pgx.Tx, session account.Session) error {
	_, err := tx.Exec(ctx, accountSessionInsert, session.ID, session.AccountID, session.AccessDigest[:], session.RefreshDigest[:], session.CSRFDigest[:], session.AccessExpiresAt, session.RefreshExpiresAt, nullTime(session.RecentAuthExpiresAt), session.Revision, session.CreatedAt)
	return err
}

const accountSessionSelect = `SELECT session_id::text,account_id::text,access_token_digest,refresh_token_digest,csrf_token_digest,created_at,access_expires_at,refresh_expires_at,recent_auth_expires_at,revision,revoked_at IS NOT NULL FROM account_sessions`

func scanAccountRecord(row rowScanner) (account.Record, error) {
	var id, email, displayName, state string
	var revision uint64
	var createdAt, updatedAt time.Time
	var passwordHash []byte
	var roleNames []string
	if err := row.Scan(&id, &email, &displayName, &state, &revision, &createdAt, &updatedAt, &passwordHash, &roleNames); err != nil {
		return account.Record{}, err
	}
	roles := make([]cloudv1.AccountRole, 0, len(roleNames))
	for _, role := range roleNames {
		roles = append(roles, parseAccountRole(role))
	}
	return account.Record{Profile: &cloudv1.AccountProfile{AccountId: id, Email: email, DisplayName: displayName, State: parseAccountState(state), Revision: revision, CreatedAt: timestamppb.New(createdAt), UpdatedAt: timestamppb.New(updatedAt)}, PasswordHash: append([]byte(nil), passwordHash...), Roles: roles}, nil
}

func scanAccountSession(row rowScanner) (account.Session, error) {
	var result account.Session
	var access, refresh, csrf []byte
	var recentAuth *time.Time
	if err := row.Scan(&result.ID, &result.AccountID, &access, &refresh, &csrf, &result.CreatedAt, &result.AccessExpiresAt, &result.RefreshExpiresAt, &recentAuth, &result.Revision, &result.Revoked); err != nil {
		return account.Session{}, err
	}
	if len(access) != sha256.Size || len(refresh) != sha256.Size || len(csrf) != sha256.Size {
		return account.Session{}, errors.New("invalid persisted account session digest")
	}
	copy(result.AccessDigest[:], access)
	copy(result.RefreshDigest[:], refresh)
	copy(result.CSRFDigest[:], csrf)
	if recentAuth != nil {
		result.RecentAuthExpiresAt = recentAuth.UTC()
	}
	return result, nil
}

func accountStateName(value cloudv1.AccountState) string {
	if value == cloudv1.AccountState_ACCOUNT_STATE_DISABLED {
		return "disabled"
	}
	return "active"
}
func parseAccountState(value string) cloudv1.AccountState {
	if value == "disabled" {
		return cloudv1.AccountState_ACCOUNT_STATE_DISABLED
	}
	return cloudv1.AccountState_ACCOUNT_STATE_ACTIVE
}
func accountRoleName(value cloudv1.AccountRole) string {
	switch value {
	case cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN:
		return "admin"
	case cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR:
		return "operator"
	default:
		return "user"
	}
}
func parseAccountRole(value string) cloudv1.AccountRole {
	switch value {
	case "admin":
		return cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN
	case "operator":
		return cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR
	default:
		return cloudv1.AccountRole_ACCOUNT_ROLE_USER
	}
}
func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
