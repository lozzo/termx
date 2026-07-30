package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EnsureBootstrapOperator 原子创建部署管理员；并发发现既有账号时只返回该精确 email 记录。
func (database *Database) EnsureBootstrapOperator(ctx context.Context, desired account.Record) (account.Record, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return account.Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, desired.Profile.GetEmail()); err != nil {
		return account.Record{}, err
	}
	record, err := scanAccountRecord(tx.QueryRow(ctx, accountSelect+` WHERE a.email=$1`, desired.Profile.GetEmail()))
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

// ProvisionAccount 原子创建 pending 账号、setup digest、初始订阅和审计。
func (database *Database) ProvisionAccount(ctx context.Context, record account.Record, actorID, reason string, now time.Time) error {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertAccount(ctx, tx, record); err != nil {
		return err
	}
	if err := insertStarterSubscription(ctx, tx, record.Profile.GetAccountId(), now); err != nil {
		return err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "account.provision", "account", record.Profile.GetAccountId(), reason, "applied", now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (database *Database) AccountByLogin(ctx context.Context, login string) (account.Record, error) {
	return accountRecordResult(scanAccountRecord(database.pool.QueryRow(ctx, accountSelect+` WHERE lower(a.email)=lower($1)`, login)))
}

func (database *Database) AccountByExactEmail(ctx context.Context, email string) (account.Record, error) {
	return accountRecordResult(scanAccountRecord(database.pool.QueryRow(ctx, accountSelect+` WHERE a.email=$1`, email)))
}

func (database *Database) AccountByID(ctx context.Context, accountID string) (account.Record, error) {
	return accountRecordResult(scanAccountRecord(database.pool.QueryRow(ctx, accountSelect+` WHERE a.account_id=$1`, accountID)))
}

func accountRecordResult(record account.Record, err error) (account.Record, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return account.Record{}, account.ErrAccountNotFound
	}
	return record, err
}

// CreateSession 在密码完成事务外校验后锁定并重检 credential，再创建 session。
func (database *Database) CreateSession(ctx context.Context, expected account.Record, session account.Session, now time.Time) (account.Record, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return account.Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := lockAccountCredential(ctx, tx, session.AccountID)
	if err != nil {
		return account.Record{}, err
	}
	if !activeCredentialMatches(locked, expected) {
		return account.Record{}, account.ErrAccountConflict
	}
	if !now.Before(session.AccessExpiresAt) || !session.AccessExpiresAt.Before(session.RefreshExpiresAt) {
		return account.Record{}, account.ErrAccountConflict
	}
	if err := insertAccountSession(ctx, tx, session); err != nil {
		return account.Record{}, err
	}
	roles, err := loadAccountRoles(ctx, tx, session.AccountID)
	if err != nil {
		return account.Record{}, err
	}
	locked.Roles = roles
	if err := tx.Commit(ctx); err != nil {
		return account.Record{}, err
	}
	return locked, nil
}

func (database *Database) SessionByAccessDigest(ctx context.Context, digest [sha256.Size]byte) (account.Session, error) {
	return scanAccountSession(database.pool.QueryRow(ctx, accountSessionSelect+` WHERE access_token_digest=$1`, digest[:]))
}

func (database *Database) SessionByRefreshDigest(ctx context.Context, digest [sha256.Size]byte) (account.Session, error) {
	return scanAccountSession(database.pool.QueryRow(ctx, accountSessionSelect+` WHERE refresh_token_digest=$1`, digest[:]))
}

// RotateSession 按 account -> credential -> session 顺序锁定并 fail closed 地轮换 refresh session。
func (database *Database) RotateSession(ctx context.Context, previous, next account.Session, now time.Time) (account.Record, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return account.Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	lockedAccount, err := lockAccountCredential(ctx, tx, previous.AccountID)
	if err != nil {
		return account.Record{}, err
	}
	if lockedAccount.Profile.GetState() != cloudv1.AccountState_ACCOUNT_STATE_ACTIVE || !validBcryptHash(lockedAccount.PasswordHash) {
		return account.Record{}, account.ErrUnauthenticated
	}
	lockedSession, err := scanAccountSession(tx.QueryRow(ctx, accountSessionSelect+` WHERE session_id=$1 AND account_id=$2 FOR UPDATE`, previous.ID, previous.AccountID))
	if err != nil || lockedSession.Revoked || !now.Before(lockedSession.RefreshExpiresAt) || lockedSession.Revision != previous.Revision || !bytes.Equal(lockedSession.RefreshDigest[:], previous.RefreshDigest[:]) {
		return account.Record{}, account.ErrUnauthenticated
	}
	result, err := tx.Exec(ctx, `UPDATE account_sessions SET revoked_at=$1,revision=revision+1 WHERE session_id=$2 AND revoked_at IS NULL`, now, lockedSession.ID)
	if err != nil {
		return account.Record{}, err
	}
	if result.RowsAffected() != 1 {
		return account.Record{}, account.ErrAccountConflict
	}
	next.AccountID = lockedSession.AccountID
	next.RecentAuthExpiresAt = lockedSession.RecentAuthExpiresAt
	next.Revision = lockedSession.Revision + 1
	if err := insertAccountSession(ctx, tx, next); err != nil {
		return account.Record{}, err
	}
	roles, err := loadAccountRoles(ctx, tx, lockedSession.AccountID)
	if err != nil {
		return account.Record{}, err
	}
	lockedAccount.Roles = roles
	if err := tx.Commit(ctx); err != nil {
		return account.Record{}, err
	}
	return lockedAccount, nil
}

func (database *Database) RevokeSession(ctx context.Context, accountID, sessionID string, all bool) error {
	query := `UPDATE account_sessions SET revoked_at=now(),revision=revision+1 WHERE account_id=$1 AND session_id=$2 AND revoked_at IS NULL`
	arguments := []any{accountID, sessionID}
	if all {
		query = `UPDATE account_sessions SET revoked_at=now(),revision=revision+1 WHERE account_id=$1 AND revoked_at IS NULL`
		arguments = []any{accountID}
	}
	result, err := database.pool.Exec(ctx, query, arguments...)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return account.ErrAccountConflict
	}
	return nil
}

// SetRecentAuthentication 在同一事务中重检账号、credential 和精确 session。
func (database *Database) SetRecentAuthentication(ctx context.Context, accountID, sessionID string, expected account.Record, expiresAt, now time.Time) error {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := lockAccountCredential(ctx, tx, accountID)
	if err != nil || !activeCredentialMatches(locked, expected) {
		return account.ErrUnauthenticated
	}
	session, err := scanAccountSession(tx.QueryRow(ctx, accountSessionSelect+` WHERE session_id=$1 AND account_id=$2 FOR UPDATE`, sessionID, accountID))
	if err != nil || session.Revoked || !now.Before(session.RefreshExpiresAt) {
		return account.ErrUnauthenticated
	}
	result, err := tx.Exec(ctx, `UPDATE account_sessions SET recent_auth_expires_at=$1,revision=revision+1 WHERE session_id=$2 AND revoked_at IS NULL`, expiresAt, sessionID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return account.ErrUnauthenticated
	}
	return tx.Commit(ctx)
}

func (database *Database) ListAccountSessions(ctx context.Context, accountID string, now time.Time) ([]account.Session, error) {
	rows, err := database.pool.Query(ctx, accountSessionSelect+` WHERE account_id=$1 AND revoked_at IS NULL AND refresh_expires_at>$2 ORDER BY created_at DESC,session_id`, accountID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]account.Session, 0)
	for rows.Next() {
		session, err := scanAccountSession(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, session)
	}
	return result, rows.Err()
}

// UpdatePassword 锁定并重检旧 credential，替换密码后撤销全部旧 session。
func (database *Database) UpdatePassword(ctx context.Context, accountID string, expected account.Record, passwordHash []byte, now time.Time) (*cloudv1.AccountProfile, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := lockAccountCredential(ctx, tx, accountID)
	if err != nil {
		return nil, err
	}
	if !activeCredentialMatches(locked, expected) || !validBcryptHash(passwordHash) {
		return nil, account.ErrAccountConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE account_credentials SET password_hash=$1,setup_digest=NULL,setup_expires_at=NULL,revision=revision+1,updated_at=$2 WHERE account_id=$3`, passwordHash, now, accountID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET revision=revision+1,updated_at=$1 WHERE account_id=$2`, now, accountID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_sessions SET revoked_at=$1,revision=revision+1 WHERE account_id=$2 AND revoked_at IS NULL`, now, accountID); err != nil {
		return nil, err
	}
	if err := insertOperatorAudit(ctx, tx, accountID, "account.password.change", "account", accountID, "user security action", "applied", now); err != nil {
		return nil, err
	}
	profile, err := scanAccountProfile(tx.QueryRow(ctx, accountProfileSelect+` WHERE account_id=$1`, accountID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return profile, nil
}

// RedeemAccountSetup 原子消费 setup digest；过期、重放和并发失败使用同一错误。
func (database *Database) RedeemAccountSetup(ctx context.Context, digest [sha256.Size]byte, passwordHash []byte, session account.Session, now time.Time) (account.Record, error) {
	var accountID string
	if err := database.pool.QueryRow(ctx, `SELECT account_id::text FROM account_credentials WHERE setup_digest=$1`, digest[:]).Scan(&accountID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return account.Record{}, account.ErrSetupCredentialInvalid
		}
		return account.Record{}, err
	}
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return account.Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := lockAccountCredential(ctx, tx, accountID)
	if err != nil || locked.Profile.GetState() != cloudv1.AccountState_ACCOUNT_STATE_PENDING || len(locked.PasswordHash) != 0 || len(locked.SetupDigest) != sha256.Size || !bytes.Equal(locked.SetupDigest, digest[:]) || locked.SetupExpiresAt.IsZero() || !now.Before(locked.SetupExpiresAt) || !validBcryptHash(passwordHash) {
		return account.Record{}, account.ErrSetupCredentialInvalid
	}
	if _, err := tx.Exec(ctx, `UPDATE account_credentials SET password_hash=$1,setup_digest=NULL,setup_expires_at=NULL,revision=revision+1,updated_at=$2 WHERE account_id=$3`, passwordHash, now, accountID); err != nil {
		return account.Record{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET state='active',revision=revision+1,updated_at=$1 WHERE account_id=$2`, now, accountID); err != nil {
		return account.Record{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_sessions SET revoked_at=$1,revision=revision+1 WHERE account_id=$2 AND revoked_at IS NULL`, now, accountID); err != nil {
		return account.Record{}, err
	}
	session.AccountID = accountID
	if !now.Before(session.AccessExpiresAt) || !session.AccessExpiresAt.Before(session.RefreshExpiresAt) {
		return account.Record{}, account.ErrSetupCredentialInvalid
	}
	if err := insertAccountSession(ctx, tx, session); err != nil {
		return account.Record{}, err
	}
	roles, err := loadAccountRoles(ctx, tx, accountID)
	if err != nil {
		return account.Record{}, err
	}
	if err := insertOperatorAudit(ctx, tx, accountID, "account.setup.redeem", "account", accountID, "setup credential redemption", "applied", now); err != nil {
		return account.Record{}, err
	}
	profile, err := scanAccountProfile(tx.QueryRow(ctx, accountProfileSelect+` WHERE account_id=$1`, accountID))
	if err != nil {
		return account.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return account.Record{}, err
	}
	return account.Record{Profile: profile, PasswordHash: append([]byte(nil), passwordHash...), CredentialRevision: locked.CredentialRevision + 1, CredentialUpdatedAt: now, Roles: roles}, nil
}

// ResetAccountSetup 把目标账号置为 pending、轮换 setup digest、清除密码并撤销全部 session。
func (database *Database) ResetAccountSetup(ctx context.Context, accountID, actorID, reason string, digest [sha256.Size]byte, expiresAt, now time.Time) (*cloudv1.AccountProfile, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAccountRows(ctx, tx, actorID, accountID); err != nil {
		return nil, err
	}
	if _, err := lockAccountCredentialRowsLocked(ctx, tx, accountID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_credentials SET password_hash=NULL,setup_digest=$1,setup_expires_at=$2,revision=revision+1,updated_at=$3 WHERE account_id=$4`, digest[:], expiresAt, now, accountID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET state='pending',revision=revision+1,updated_at=$1 WHERE account_id=$2`, now, accountID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_sessions SET revoked_at=$1,revision=revision+1 WHERE account_id=$2 AND revoked_at IS NULL`, now, accountID); err != nil {
		return nil, err
	}
	if err := insertOperatorAudit(ctx, tx, actorID, "account.setup.reset", "account", accountID, reason, "applied", now); err != nil {
		return nil, err
	}
	profile, err := scanAccountProfile(tx.QueryRow(ctx, accountProfileSelect+` WHERE account_id=$1`, accountID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return profile, nil
}

const accountProfileSelect = `SELECT account_id::text,coalesce(email,''),display_name,state,revision,created_at,updated_at FROM accounts`

const accountSelect = `SELECT a.account_id::text,coalesce(a.email,''),a.display_name,a.state,a.revision,a.created_at,a.updated_at,c.password_hash,c.setup_digest,c.setup_expires_at,c.revision,c.updated_at,ARRAY(SELECT r.role FROM account_roles r WHERE r.account_id=a.account_id ORDER BY r.role) FROM accounts a JOIN account_credentials c ON c.account_id=a.account_id`

func insertAccount(ctx context.Context, tx pgx.Tx, record account.Record) error {
	profile := record.Profile
	if profile == nil {
		return errors.New("account profile is required")
	}
	state, err := accountStateName(profile.GetState())
	if err != nil {
		return err
	}
	if err := validateCredentialForState(record, profile.GetState()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO accounts(account_id,email,display_name,state,revision,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, profile.GetAccountId(), profile.GetEmail(), profile.GetDisplayName(), state, profile.GetRevision(), profile.GetCreatedAt().AsTime(), profile.GetUpdatedAt().AsTime()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO account_credentials(account_id,password_hash,setup_digest,setup_expires_at,revision,updated_at) VALUES($1,$2,$3,$4,$5,$6)`, profile.GetAccountId(), nullBytes(record.PasswordHash), nullBytes(record.SetupDigest), nullTime(record.SetupExpiresAt), record.CredentialRevision, record.CredentialUpdatedAt); err != nil {
		return err
	}
	for _, value := range record.Roles {
		role, err := accountRoleName(value)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO account_roles(account_id,role,created_at) VALUES($1,$2,$3)`, profile.GetAccountId(), role, profile.GetCreatedAt().AsTime()); err != nil {
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

func lockAccountCredential(ctx context.Context, tx pgx.Tx, accountID string) (account.Record, error) {
	if err := lockAccountRows(ctx, tx, accountID); err != nil {
		return account.Record{}, err
	}
	return lockAccountCredentialRowsLocked(ctx, tx, accountID)
}

func lockAccountCredentialRowsLocked(ctx context.Context, tx pgx.Tx, accountID string) (account.Record, error) {
	profile, err := scanAccountProfile(tx.QueryRow(ctx, accountProfileSelect+` WHERE account_id=$1`, accountID))
	if err != nil {
		return account.Record{}, err
	}
	record := account.Record{Profile: profile}
	var setupExpiresAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT password_hash,setup_digest,setup_expires_at,revision,updated_at FROM account_credentials WHERE account_id=$1 FOR UPDATE`, accountID).Scan(&record.PasswordHash, &record.SetupDigest, &setupExpiresAt, &record.CredentialRevision, &record.CredentialUpdatedAt); err != nil {
		return account.Record{}, err
	}
	if setupExpiresAt != nil {
		record.SetupExpiresAt = setupExpiresAt.UTC()
	}
	if err := validatePersistedCredential(record); err != nil {
		return account.Record{}, err
	}
	return record, nil
}

func lockAccountRows(ctx context.Context, tx pgx.Tx, accountIDs ...string) error {
	unique := make(map[string]struct{}, len(accountIDs))
	ordered := make([]string, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(accountID))
		if err != nil {
			return err
		}
		accountID = parsed.String()
		if _, found := unique[accountID]; found {
			continue
		}
		unique[accountID] = struct{}{}
		ordered = append(ordered, accountID)
	}
	slices.Sort(ordered)
	for _, accountID := range ordered {
		var locked string
		if err := tx.QueryRow(ctx, `SELECT account_id::text FROM accounts WHERE account_id=$1 FOR UPDATE`, accountID).Scan(&locked); err != nil {
			return err
		}
	}
	return nil
}

func scanAccountRecord(row rowScanner) (account.Record, error) {
	var id, email, displayName, state string
	var revision uint64
	var createdAt, updatedAt, credentialUpdatedAt time.Time
	var passwordHash, setupDigest []byte
	var setupExpiresAt *time.Time
	var credentialRevision uint64
	var roleNames []string
	if err := row.Scan(&id, &email, &displayName, &state, &revision, &createdAt, &updatedAt, &passwordHash, &setupDigest, &setupExpiresAt, &credentialRevision, &credentialUpdatedAt, &roleNames); err != nil {
		return account.Record{}, err
	}
	stateValue, err := parseAccountState(state)
	if err != nil {
		return account.Record{}, err
	}
	roles, err := parseAccountRoles(roleNames)
	if err != nil {
		return account.Record{}, err
	}
	record := account.Record{Profile: &cloudv1.AccountProfile{AccountId: id, Email: email, DisplayName: displayName, State: stateValue, Revision: revision, CreatedAt: timestamppb.New(createdAt), UpdatedAt: timestamppb.New(updatedAt)}, PasswordHash: append([]byte(nil), passwordHash...), SetupDigest: append([]byte(nil), setupDigest...), CredentialRevision: credentialRevision, CredentialUpdatedAt: credentialUpdatedAt.UTC(), Roles: roles}
	if setupExpiresAt != nil {
		record.SetupExpiresAt = setupExpiresAt.UTC()
	}
	if err := validatePersistedCredential(record); err != nil {
		return account.Record{}, err
	}
	return record, nil
}

func scanAccountProfile(row rowScanner) (*cloudv1.AccountProfile, error) {
	var id, email, displayName, state string
	var revision uint64
	var createdAt, updatedAt time.Time
	if err := row.Scan(&id, &email, &displayName, &state, &revision, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	stateValue, err := parseAccountState(state)
	if err != nil {
		return nil, err
	}
	return &cloudv1.AccountProfile{AccountId: id, Email: email, DisplayName: displayName, State: stateValue, Revision: revision, CreatedAt: timestamppb.New(createdAt), UpdatedAt: timestamppb.New(updatedAt)}, nil
}

func loadAccountRoles(ctx context.Context, tx pgx.Tx, accountID string) ([]cloudv1.AccountRole, error) {
	var names []string
	if err := tx.QueryRow(ctx, `SELECT ARRAY(SELECT role FROM account_roles WHERE account_id=$1 ORDER BY role)`, accountID).Scan(&names); err != nil {
		return nil, err
	}
	return parseAccountRoles(names)
}

func parseAccountRoles(names []string) ([]cloudv1.AccountRole, error) {
	roles := make([]cloudv1.AccountRole, 0, len(names))
	for _, name := range names {
		role, err := parseAccountRole(name)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
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

func accountStateName(value cloudv1.AccountState) (string, error) {
	switch value {
	case cloudv1.AccountState_ACCOUNT_STATE_PENDING:
		return "pending", nil
	case cloudv1.AccountState_ACCOUNT_STATE_ACTIVE:
		return "active", nil
	case cloudv1.AccountState_ACCOUNT_STATE_DISABLED:
		return "disabled", nil
	default:
		return "", errors.New("unknown account state")
	}
}

func parseAccountState(value string) (cloudv1.AccountState, error) {
	switch value {
	case "pending":
		return cloudv1.AccountState_ACCOUNT_STATE_PENDING, nil
	case "active":
		return cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, nil
	case "disabled":
		return cloudv1.AccountState_ACCOUNT_STATE_DISABLED, nil
	default:
		return cloudv1.AccountState_ACCOUNT_STATE_UNSPECIFIED, errors.New("unknown persisted account state")
	}
}

func accountRoleName(value cloudv1.AccountRole) (string, error) {
	switch value {
	case cloudv1.AccountRole_ACCOUNT_ROLE_USER:
		return "user", nil
	case cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR:
		return "operator", nil
	case cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN:
		return "admin", nil
	default:
		return "", errors.New("unknown account role")
	}
}

func parseAccountRole(value string) (cloudv1.AccountRole, error) {
	switch value {
	case "user":
		return cloudv1.AccountRole_ACCOUNT_ROLE_USER, nil
	case "operator":
		return cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR, nil
	case "admin":
		return cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN, nil
	default:
		return cloudv1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED, errors.New("unknown persisted account role")
	}
}

func validateCredentialForState(record account.Record, state cloudv1.AccountState) error {
	if record.CredentialRevision == 0 || record.CredentialUpdatedAt.IsZero() {
		return errors.New("account credential revision and update time are required")
	}
	switch state {
	case cloudv1.AccountState_ACCOUNT_STATE_PENDING:
		if len(record.PasswordHash) != 0 || len(record.SetupDigest) != sha256.Size || record.SetupExpiresAt.IsZero() {
			return errors.New("pending account requires only setup credential state")
		}
	case cloudv1.AccountState_ACCOUNT_STATE_ACTIVE:
		if !validBcryptHash(record.PasswordHash) || len(record.SetupDigest) != 0 || !record.SetupExpiresAt.IsZero() {
			return errors.New("active account requires only a valid password")
		}
	case cloudv1.AccountState_ACCOUNT_STATE_DISABLED:
		if !validBcryptHash(record.PasswordHash) || len(record.SetupDigest) != 0 || !record.SetupExpiresAt.IsZero() {
			return errors.New("disabled account requires only a valid password")
		}
	default:
		return errors.New("unknown account credential state")
	}
	return nil
}

func validatePersistedCredential(record account.Record) error {
	if record.CredentialRevision == 0 || record.CredentialUpdatedAt.IsZero() || (len(record.SetupDigest) == 0) != record.SetupExpiresAt.IsZero() || len(record.SetupDigest) != 0 && len(record.SetupDigest) != sha256.Size {
		return errors.New("invalid persisted account credential")
	}
	return validateCredentialForState(record, record.Profile.GetState())
}

func activeCredentialMatches(locked, expected account.Record) bool {
	return locked.Profile != nil && expected.Profile != nil && locked.Profile.GetState() == cloudv1.AccountState_ACCOUNT_STATE_ACTIVE && locked.Profile.GetRevision() == expected.Profile.GetRevision() && validBcryptHash(locked.PasswordHash) && locked.CredentialRevision == expected.CredentialRevision && bytes.Equal(locked.PasswordHash, expected.PasswordHash)
}

func validBcryptHash(value []byte) bool {
	_, err := bcrypt.Cost(value)
	return err == nil
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
