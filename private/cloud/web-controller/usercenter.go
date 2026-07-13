package webcontroller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var (
	ErrUserCenterNotFound = errors.New("user center resource not found")
	ErrIdentityConflict   = errors.New("identity already exists")
	ErrPasswordInvalid    = errors.New("email or password is invalid")
)

// UserProfile 是账号中心公开的用户投影；PasswordConfigured 只表示是否存在密码凭据，不泄露凭据内容。
type UserProfile struct {
	UserID             string `json:"user_id"`
	AccountID          string `json:"account_id"`
	DisplayName        string `json:"display_name"`
	Email              string `json:"email"`
	AvatarURL          string `json:"avatar_url,omitempty"`
	PasswordConfigured bool   `json:"password_configured"`
}

// ManagedNode 是账号拥有的云节点目录投影，不包含 terminal inventory、grant 或 daemon private key。
type ManagedNode struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Online    bool      `json:"online"`
	Revoked   bool      `json:"revoked"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReferralReward 是首次有效付款产生的不可重复奖励账目；Days 是订阅延期量。
type ReferralReward struct {
	ID        string    `json:"id"`
	OrderID   string    `json:"order_id"`
	Kind      string    `json:"kind"`
	Days      int       `json:"days"`
	CreatedAt time.Time `json:"created_at"`
}

// ReferralProgram 是用户自己的 AFF 码、归因数量和奖励账目投影。
type ReferralProgram struct {
	Code          string           `json:"code"`
	ReferredCount int              `json:"referred_count"`
	RewardDays    int              `json:"reward_days"`
	Rewards       []ReferralReward `json:"rewards"`
}

// AccountEntitlement 是 Control Plane 启动时从 paid order 与奖励账本重建的订阅投影。
// 它只包含托管服务能力期限，不包含 terminal capability。
type AccountEntitlement struct {
	AccountID  string
	PlanID     string
	OrderID    string
	ValidUntil time.Time
}

// UserAuditEvent 是账号中心可展示的持久审计投影。
type UserAuditEvent struct {
	ID         string    `json:"id"`
	Action     string    `json:"action"`
	ResourceID string    `json:"resource_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

// UserCenterStore 以 SQLite 作为账号、密码身份、AFF 归因、奖励、节点和审计的持久真值。
// Hub 不读取该数据库；连接热路径仍只消费 Control Plane 发布的授权投影。
type UserCenterStore struct {
	db  *sql.DB
	now func() time.Time
}

// OpenUserCenterStore 打开持久账号库并执行幂等 schema bootstrap。
func OpenUserCenterStore(path string, now func() time.Time) (*UserCenterStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("account database path is required")
	}
	if now == nil {
		now = time.Now
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &UserCenterStore{db: db, now: now}
	if err := store.bootstrap(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// NewUserCenterStore 创建隔离的内存数据库，仅用于测试 harness。
func NewUserCenterStore(now func() time.Time) *UserCenterStore {
	store, err := OpenUserCenterStore("file:termx-usercenter-"+fmt.Sprint(time.Now().UnixNano())+"?mode=memory&cache=shared", now)
	if err != nil {
		panic(err)
	}
	return store
}

// Close 关闭数据库连接；调用方必须在服务退出时释放它。
func (store *UserCenterStore) Close() error { return store.db.Close() }

func (store *UserCenterStore) bootstrap() error {
	schema := `
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
CREATE TABLE IF NOT EXISTS accounts(account_id TEXT PRIMARY KEY, user_id TEXT UNIQUE NOT NULL, email TEXT UNIQUE NOT NULL, display_name TEXT NOT NULL, referral_code TEXT UNIQUE NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS password_identities(user_id TEXT PRIMARY KEY, password_hash BLOB NOT NULL, updated_at TEXT NOT NULL, FOREIGN KEY(user_id) REFERENCES accounts(user_id));
CREATE TABLE IF NOT EXISTS nodes(id TEXT PRIMARY KEY, account_id TEXT NOT NULL, name TEXT NOT NULL, kind TEXT NOT NULL, online INTEGER NOT NULL, revoked INTEGER NOT NULL, updated_at TEXT NOT NULL, FOREIGN KEY(account_id) REFERENCES accounts(account_id));
CREATE TABLE IF NOT EXISTS referral_attributions(referred_account_id TEXT PRIMARY KEY, referrer_account_id TEXT NOT NULL, code TEXT NOT NULL, created_at TEXT NOT NULL, FOREIGN KEY(referred_account_id) REFERENCES accounts(account_id), FOREIGN KEY(referrer_account_id) REFERENCES accounts(account_id));
CREATE TABLE IF NOT EXISTS referral_rewards(id TEXT PRIMARY KEY, order_id TEXT NOT NULL, beneficiary_account_id TEXT NOT NULL, kind TEXT NOT NULL, days INTEGER NOT NULL, created_at TEXT NOT NULL, UNIQUE(order_id, beneficiary_account_id), FOREIGN KEY(beneficiary_account_id) REFERENCES accounts(account_id));
CREATE TABLE IF NOT EXISTS commerce_sessions(token_hash BLOB PRIMARY KEY, account_id TEXT NOT NULL, user_id TEXT NOT NULL, email TEXT NOT NULL, expires_at TEXT NOT NULL, FOREIGN KEY(account_id) REFERENCES accounts(account_id));
CREATE TABLE IF NOT EXISTS orders(id TEXT PRIMARY KEY, account_id TEXT NOT NULL, plan_id TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, paid_at TEXT, FOREIGN KEY(account_id) REFERENCES accounts(account_id));
CREATE TABLE IF NOT EXISTS payment_events(event_id TEXT PRIMARY KEY, order_id TEXT NOT NULL, created_at TEXT NOT NULL, FOREIGN KEY(order_id) REFERENCES orders(id));
CREATE TABLE IF NOT EXISTS audit_events(id TEXT PRIMARY KEY, account_id TEXT NOT NULL, action TEXT NOT NULL, resource_id TEXT NOT NULL, occurred_at TEXT NOT NULL, FOREIGN KEY(account_id) REFERENCES accounts(account_id));`
	if _, err := store.db.Exec(schema); err != nil {
		return fmt.Errorf("bootstrap account database: %w", err)
	}
	now := store.now().UTC().Format(time.RFC3339Nano)
	_, err := store.db.Exec(`INSERT OR IGNORE INTO accounts(account_id,user_id,email,display_name,referral_code,created_at) VALUES(?,?,?,?,?,?)`, "account-dev-local", "user-dev-local", "dev-local@termx.invalid", "TermX Developer", "TERMXDEV", now)
	if err != nil {
		return err
	}
	_, _ = store.db.Exec(`INSERT OR IGNORE INTO nodes(id,account_id,name,kind,online,revoked,updated_at) VALUES(?,?,?,?,1,0,?)`, "device-staging", "account-dev-local", "Public staging", "daemon", now)
	_, _ = store.db.Exec(`INSERT OR IGNORE INTO audit_events(id,account_id,action,resource_id,occurred_at) VALUES(?,?,?,?,?)`, "audit-bootstrap", "account-dev-local", "account.created", "account-dev-local", now)
	return nil
}

// RegisterPasswordAccount 创建邮箱密码账号，并只在有效 AFF 码存在时写入一次不可变归因。
func (store *UserCenterStore) RegisterPasswordAccount(email, password, aff string) (UserProfile, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || len(password) < 10 {
		return UserProfile{}, ErrPasswordInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return UserProfile{}, err
	}
	userToken, err := randomToken(10)
	if err != nil {
		return UserProfile{}, err
	}
	accountID, userID, code := "account-"+userToken, "user-"+userToken, strings.ToUpper(userToken[:8])
	now := store.now().UTC().Format(time.RFC3339Nano)
	tx, err := store.db.BeginTx(context.Background(), nil)
	if err != nil {
		return UserProfile{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO accounts(account_id,user_id,email,display_name,referral_code,created_at) VALUES(?,?,?,?,?,?)`, accountID, userID, email, strings.Split(email, "@")[0], code, now); err != nil {
		return UserProfile{}, ErrIdentityConflict
	}
	if _, err = tx.Exec(`INSERT INTO password_identities(user_id,password_hash,updated_at) VALUES(?,?,?)`, userID, hash, now); err != nil {
		return UserProfile{}, err
	}
	if aff = strings.ToUpper(strings.TrimSpace(aff)); aff != "" {
		var referrer string
		if err = tx.QueryRow(`SELECT account_id FROM accounts WHERE referral_code=?`, aff).Scan(&referrer); err != nil {
			return UserProfile{}, fmt.Errorf("invalid referral code")
		}
		if _, err = tx.Exec(`INSERT INTO referral_attributions(referred_account_id,referrer_account_id,code,created_at) VALUES(?,?,?,?)`, accountID, referrer, aff, now); err != nil {
			return UserProfile{}, err
		}
	}
	auditID, _ := randomToken(8)
	_, err = tx.Exec(`INSERT INTO audit_events(id,account_id,action,resource_id,occurred_at) VALUES(?,?,?,?,?)`, "audit-"+auditID, accountID, "account.created", accountID, now)
	if err != nil {
		return UserProfile{}, err
	}
	if err = tx.Commit(); err != nil {
		return UserProfile{}, err
	}
	return store.profile(accountID)
}

// AuthenticatePassword 验证邮箱密码身份并返回所属账号；错误不区分邮箱不存在与密码错误。
func (store *UserCenterStore) AuthenticatePassword(email, password string) (UserProfile, error) {
	var accountID string
	var hash []byte
	err := store.db.QueryRow(`SELECT a.account_id,p.password_hash FROM accounts a JOIN password_identities p ON p.user_id=a.user_id WHERE a.email=?`, strings.ToLower(strings.TrimSpace(email))).Scan(&accountID, &hash)
	if err != nil || bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil {
		return UserProfile{}, ErrPasswordInvalid
	}
	return store.profile(accountID)
}

// ChangePassword 设置或修改当前用户的密码凭据；已有密码时必须验证旧密码。
func (store *UserCenterStore) ChangePassword(accountID, current, next string) error {
	if len(next) < 10 {
		return ErrPasswordInvalid
	}
	var userID string
	if err := store.db.QueryRow(`SELECT user_id FROM accounts WHERE account_id=?`, accountID).Scan(&userID); err != nil {
		return ErrUserCenterNotFound
	}
	var hash []byte
	err := store.db.QueryRow(`SELECT password_hash FROM password_identities WHERE user_id=?`, userID).Scan(&hash)
	if err == nil && bcrypt.CompareHashAndPassword(hash, []byte(current)) != nil {
		return ErrPasswordInvalid
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	nextHash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = store.db.Exec(`INSERT INTO password_identities(user_id,password_hash,updated_at) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET password_hash=excluded.password_hash,updated_at=excluded.updated_at`, userID, nextHash, store.now().UTC().Format(time.RFC3339Nano))
	if err == nil {
		store.appendAudit(accountID, "identity.password.updated", userID)
	}
	return err
}

// ApplyReferralPayment 在首次有效订单上幂等生成邀请人 +15 天、被邀请人 +7 天两条奖励。
func (store *UserCenterStore) ApplyReferralPayment(orderID, paidAccountID string) (string, error) {
	var referrer string
	if err := store.db.QueryRow(`SELECT referrer_account_id FROM referral_attributions WHERE referred_account_id=?`, paidAccountID).Scan(&referrer); errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	tx, err := store.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := store.now().UTC().Format(time.RFC3339Nano)
	for _, reward := range []struct {
		account, kind string
		days          int
	}{{referrer, "referrer", 15}, {paidAccountID, "referred", 7}} {
		id, _ := randomToken(8)
		if _, err = tx.Exec(`INSERT OR IGNORE INTO referral_rewards(id,order_id,beneficiary_account_id,kind,days,created_at) VALUES(?,?,?,?,?,?)`, "reward-"+id, orderID, reward.account, reward.kind, reward.days, now); err != nil {
			return "", err
		}
	}
	return referrer, tx.Commit()
}

// ReferralRewardDays 返回账号已记账的总延期天数，供订阅有效期和 entitlement 投影统一计算。
func (store *UserCenterStore) ReferralRewardDays(accountID string) int {
	var days int
	_ = store.db.QueryRow(`SELECT COALESCE(SUM(days),0) FROM referral_rewards WHERE beneficiary_account_id=?`, accountID).Scan(&days)
	return days
}

// ActiveEntitlements 从持久订单和奖励账本重建当前有效订阅，供 Control Plane 启动时恢复 Hub 投影。
func (store *UserCenterStore) ActiveEntitlements(now time.Time) []AccountEntitlement {
	rows, err := store.db.Query(`SELECT id,account_id,plan_id,paid_at FROM orders WHERE status='paid' AND paid_at IS NOT NULL ORDER BY paid_at`)
	if err != nil {
		return nil
	}
	type paidOrder struct {
		orderID, accountID, planID string
		paidAt                     time.Time
	}
	orders := []paidOrder{}
	for rows.Next() {
		var value paidOrder
		var paidAtText string
		if rows.Scan(&value.orderID, &value.accountID, &value.planID, &paidAtText) != nil {
			continue
		}
		value.paidAt, _ = time.Parse(time.RFC3339Nano, paidAtText)
		orders = append(orders, value)
	}
	rows.Close()
	latest := map[string]AccountEntitlement{}
	for _, order := range orders {
		validUntil := order.paidAt.Add(time.Duration(30+store.ReferralRewardDays(order.accountID)) * 24 * time.Hour)
		if now.Before(validUntil) {
			latest[order.accountID] = AccountEntitlement{AccountID: order.accountID, PlanID: order.planID, OrderID: order.orderID, ValidUntil: validUntil}
		}
	}
	result := make([]AccountEntitlement, 0, len(latest))
	for _, value := range latest {
		result = append(result, value)
	}
	return result
}

// AccountIDs 返回 Control Plane 持久账号全集，供启动时重建 Hub 授权投影。
// 返回值不包含密码摘要、浏览器 Session 或 terminal capability。
func (store *UserCenterStore) AccountIDs() []string {
	rows, err := store.db.Query(`SELECT account_id FROM accounts ORDER BY account_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var accountID string
		if rows.Scan(&accountID) == nil {
			result = append(result, accountID)
		}
	}
	return result
}

// Profile 返回已认证账号的公开身份投影，供 Control Plane 签发客户端账号 Session。
// 调用方必须已经完成浏览器 Session 校验，不能用本方法验证密码或 Cookie。
func (store *UserCenterStore) Profile(accountID string) (UserProfile, error) {
	return store.profile(accountID)
}

// UpsertManagedNode 写入账号名下 daemon 的目录投影；节点状态不包含 terminal inventory 或 capability。
func (store *UserCenterStore) UpsertManagedNode(accountID, nodeID, name string, online bool) error {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(nodeID) == "" || strings.TrimSpace(name) == "" {
		return ErrUserCenterNotFound
	}
	now := store.now().UTC().Format(time.RFC3339Nano)
	onlineValue := 0
	if online {
		onlineValue = 1
	}
	_, err := store.db.Exec(`INSERT INTO nodes(id,account_id,name,kind,online,revoked,updated_at) VALUES(?,?,?,?,?,0,?) ON CONFLICT(id) DO UPDATE SET account_id=excluded.account_id,name=excluded.name,kind=excluded.kind,online=excluded.online,revoked=0,updated_at=excluded.updated_at`, nodeID, accountID, name, "daemon", onlineValue, now)
	if err == nil {
		store.appendAudit(accountID, "node.enrolled", nodeID)
	}
	return err
}

func (store *UserCenterStore) referralReferrer(accountID string) string {
	var referrer string
	_ = store.db.QueryRow(`SELECT referrer_account_id FROM referral_attributions WHERE referred_account_id=?`, accountID).Scan(&referrer)
	return referrer
}

// Snapshot 返回账号自己的 profile、节点、AFF 奖励和审计；所有查询都按 AccountID 隔离。
func (store *UserCenterStore) Snapshot(accountID string) (UserProfile, []ManagedNode, ReferralProgram, []UserAuditEvent, error) {
	profile, err := store.profile(accountID)
	if err != nil {
		return UserProfile{}, nil, ReferralProgram{}, nil, err
	}
	nodes := []ManagedNode{}
	rows, err := store.db.Query(`SELECT id,name,kind,online,revoked,updated_at FROM nodes WHERE account_id=? ORDER BY name`, accountID)
	if err != nil {
		return UserProfile{}, nil, ReferralProgram{}, nil, err
	}
	for rows.Next() {
		var n ManagedNode
		var online, revoked int
		var at string
		if err = rows.Scan(&n.ID, &n.Name, &n.Kind, &online, &revoked, &at); err != nil {
			rows.Close()
			return UserProfile{}, nil, ReferralProgram{}, nil, err
		}
		n.Online, n.Revoked = online != 0, revoked != 0
		n.UpdatedAt, _ = time.Parse(time.RFC3339Nano, at)
		nodes = append(nodes, n)
	}
	rows.Close()
	program := ReferralProgram{}
	_ = store.db.QueryRow(`SELECT referral_code FROM accounts WHERE account_id=?`, accountID).Scan(&program.Code)
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM referral_attributions WHERE referrer_account_id=?`, accountID).Scan(&program.ReferredCount)
	rewardRows, _ := store.db.Query(`SELECT id,order_id,kind,days,created_at FROM referral_rewards WHERE beneficiary_account_id=? ORDER BY created_at DESC`, accountID)
	if rewardRows != nil {
		for rewardRows.Next() {
			var r ReferralReward
			var at string
			_ = rewardRows.Scan(&r.ID, &r.OrderID, &r.Kind, &r.Days, &at)
			r.CreatedAt, _ = time.Parse(time.RFC3339Nano, at)
			program.RewardDays += r.Days
			program.Rewards = append(program.Rewards, r)
		}
		rewardRows.Close()
	}
	audit := []UserAuditEvent{}
	auditRows, _ := store.db.Query(`SELECT id,action,resource_id,occurred_at FROM audit_events WHERE account_id=? ORDER BY occurred_at DESC`, accountID)
	if auditRows != nil {
		for auditRows.Next() {
			var a UserAuditEvent
			var at string
			_ = auditRows.Scan(&a.ID, &a.Action, &a.ResourceID, &at)
			a.OccurredAt, _ = time.Parse(time.RFC3339Nano, at)
			audit = append(audit, a)
		}
		auditRows.Close()
	}
	return profile, nodes, program, audit, nil
}

// UpdateProfile 更新显示名称并追加审计事件。
func (store *UserCenterStore) UpdateProfile(accountID, displayName string) (UserProfile, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 80 {
		return UserProfile{}, fmt.Errorf("invalid profile update")
	}
	result, err := store.db.Exec(`UPDATE accounts SET display_name=? WHERE account_id=?`, displayName, accountID)
	if err != nil {
		return UserProfile{}, err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return UserProfile{}, ErrUserCenterNotFound
	}
	store.appendAudit(accountID, "profile.updated", accountID)
	return store.profile(accountID)
}

// RevokeNode 撤销账号云目录节点；该操作不修改 daemon capability grant。
func (store *UserCenterStore) RevokeNode(accountID, nodeID string) (ManagedNode, error) {
	now := store.now().UTC().Format(time.RFC3339Nano)
	result, err := store.db.Exec(`UPDATE nodes SET revoked=1,online=0,updated_at=? WHERE id=? AND account_id=?`, now, nodeID, accountID)
	if err != nil {
		return ManagedNode{}, err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ManagedNode{}, ErrUserCenterNotFound
	}
	store.appendAudit(accountID, "node.revoked", nodeID)
	var n ManagedNode
	var online, revoked int
	var at string
	err = store.db.QueryRow(`SELECT id,name,kind,online,revoked,updated_at FROM nodes WHERE id=?`, nodeID).Scan(&n.ID, &n.Name, &n.Kind, &online, &revoked, &at)
	n.Online, n.Revoked = online != 0, revoked != 0
	n.UpdatedAt, _ = time.Parse(time.RFC3339Nano, at)
	return n, err
}

func (store *UserCenterStore) profile(accountID string) (UserProfile, error) {
	var p UserProfile
	var configured int
	err := store.db.QueryRow(`SELECT a.user_id,a.account_id,a.email,a.display_name,EXISTS(SELECT 1 FROM password_identities i WHERE i.user_id=a.user_id) FROM accounts a WHERE a.account_id=?`, accountID).Scan(&p.UserID, &p.AccountID, &p.Email, &p.DisplayName, &configured)
	if errors.Is(err, sql.ErrNoRows) {
		return UserProfile{}, ErrUserCenterNotFound
	}
	p.PasswordConfigured = configured != 0
	return p, err
}
func (store *UserCenterStore) appendAudit(accountID, action, resourceID string) {
	id, _ := randomToken(8)
	_, _ = store.db.Exec(`INSERT INTO audit_events(id,account_id,action,resource_id,occurred_at) VALUES(?,?,?,?,?)`, "audit-"+id, accountID, action, resourceID, store.now().UTC().Format(time.RFC3339Nano))
}

func (store *UserCenterStore) saveSession(hash [32]byte, session CommerceSession) error {
	_, err := store.db.Exec(`INSERT OR REPLACE INTO commerce_sessions(token_hash,account_id,user_id,email,expires_at) VALUES(?,?,?,?,?)`, hash[:], session.AccountID, session.UserID, session.Email, session.ExpiresAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (store *UserCenterStore) loadSession(hash [32]byte) (CommerceSession, error) {
	var session CommerceSession
	var expires string
	err := store.db.QueryRow(`SELECT account_id,user_id,email,expires_at FROM commerce_sessions WHERE token_hash=?`, hash[:]).Scan(&session.AccountID, &session.UserID, &session.Email, &expires)
	if err != nil {
		return CommerceSession{}, ErrCommerceUnauthorized
	}
	session.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
	return session, nil
}

func (store *UserCenterStore) deleteSession(hash [32]byte) {
	_, _ = store.db.Exec(`DELETE FROM commerce_sessions WHERE token_hash=?`, hash[:])
}

func (store *UserCenterStore) saveOrder(order Order) error {
	var paid any
	if !order.PaidAt.IsZero() {
		paid = order.PaidAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := store.db.Exec(`INSERT INTO orders(id,account_id,plan_id,status,created_at,paid_at) VALUES(?,?,?,?,?,?)`, order.ID, order.AccountID, order.PlanID, order.Status, order.CreatedAt.UTC().Format(time.RFC3339Nano), paid)
	return err
}

func (store *UserCenterStore) order(id string) (Order, error) {
	var order Order
	var created string
	var paid sql.NullString
	err := store.db.QueryRow(`SELECT id,account_id,plan_id,status,created_at,paid_at FROM orders WHERE id=?`, id).Scan(&order.ID, &order.AccountID, &order.PlanID, &order.Status, &created, &paid)
	if err != nil {
		return Order{}, ErrCommerceConflict
	}
	order.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if paid.Valid {
		order.PaidAt, _ = time.Parse(time.RFC3339Nano, paid.String)
	}
	return order, nil
}

func (store *UserCenterStore) accountOrders(accountID string) []Order {
	rows, err := store.db.Query(`SELECT id,account_id,plan_id,status,created_at,paid_at FROM orders WHERE account_id=? ORDER BY created_at`, accountID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []Order
	for rows.Next() {
		var order Order
		var created string
		var paid sql.NullString
		if rows.Scan(&order.ID, &order.AccountID, &order.PlanID, &order.Status, &created, &paid) == nil {
			order.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
			if paid.Valid {
				order.PaidAt, _ = time.Parse(time.RFC3339Nano, paid.String)
			}
			result = append(result, order)
		}
	}
	return result
}

func (store *UserCenterStore) paymentApplied(eventID string) bool {
	var exists int
	_ = store.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM payment_events WHERE event_id=?)`, eventID).Scan(&exists)
	return exists != 0
}
func (store *UserCenterStore) commitPayment(eventID string, order Order, referrer string) error {
	tx, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE orders SET status=?,paid_at=? WHERE id=? AND status='pending'`, order.Status, order.PaidAt.UTC().Format(time.RFC3339Nano), order.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO payment_events(event_id,order_id,created_at) VALUES(?,?,?)`, eventID, order.ID, store.now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if referrer != "" {
		now := store.now().UTC().Format(time.RFC3339Nano)
		for _, reward := range []struct {
			account, kind string
			days          int
		}{{referrer, "referrer", 15}, {order.AccountID, "referred", 7}} {
			id, _ := randomToken(8)
			if _, err = tx.Exec(`INSERT OR IGNORE INTO referral_rewards(id,order_id,beneficiary_account_id,kind,days,created_at) VALUES(?,?,?,?,?,?)`, "reward-"+id, order.ID, reward.account, reward.kind, reward.days, now); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
