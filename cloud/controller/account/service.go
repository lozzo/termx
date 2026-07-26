// Package account 拥有 Cloud 账号 credential、session、角色和 recent-auth 生命周期。
package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// ErrUnauthenticated 表示 credential 不存在、失效或密码校验失败。
	ErrUnauthenticated = errors.New("account credential is invalid")
	// ErrAccountConflict 表示账号标识已存在或 session CAS 失败。
	ErrAccountConflict = errors.New("account state conflicts")
	// ErrAccountDisabled 表示账号已被运营人员禁用。
	ErrAccountDisabled = errors.New("account is disabled")
)

// Record 是账号领域读取模型；密码 hash 只在 account package 与 Store 之间流动。
type Record struct {
	Profile      *cloudv1.AccountProfile
	PasswordHash []byte
	Roles        []cloudv1.AccountRole
}

// Session 是持久 session 记录；数据库只保存三个原始 token 的 SHA-256。
type Session struct {
	ID, AccountID                           string
	AccessDigest, RefreshDigest, CSRFDigest [sha256.Size]byte
	CreatedAt                               time.Time
	AccessExpiresAt, RefreshExpiresAt       time.Time
	RecentAuthExpiresAt                     time.Time
	Revision                                uint64
	Revoked                                 bool
}

// Store 是账号写事务和 session CAS 的持久边界。
type Store interface {
	CreateAccount(context.Context, Record, Session) error
	EnsureBootstrapOperator(context.Context, Record) (Record, error)
	AccountByLogin(context.Context, string) (Record, error)
	AccountByID(context.Context, string) (Record, error)
	PutSession(context.Context, Session) error
	SessionByAccessDigest(context.Context, [sha256.Size]byte) (Session, error)
	SessionByRefreshDigest(context.Context, [sha256.Size]byte) (Session, error)
	RotateSession(context.Context, Session, Session) error
	RevokeSession(context.Context, string, string, bool) error
	SetRecentAuthentication(context.Context, string, time.Time) error
}

// Config 固定账号 Store、token 时限和可替换的时间/随机来源。
type Config struct {
	Store                   Store
	AccessTTL               time.Duration
	RefreshTTL              time.Duration
	RecentAuthenticationTTL time.Duration
	BcryptCost              int
	Now                     func() time.Time
}

// Service 是 AccountService 的应用实现，不拥有 HTTP cookie 或 transport 状态。
type Service struct {
	cloudv1.UnimplementedAccountServiceServer
	store                   Store
	accessTTL               time.Duration
	refreshTTL              time.Duration
	recentAuthenticationTTL time.Duration
	bcryptCost              int
	now                     func() time.Time
}

// New 创建账号服务；credential 策略缺失时直接失败，不能回退到固定 token。
func New(config Config) (*Service, error) {
	if config.Store == nil || config.AccessTTL <= 0 || config.RefreshTTL <= config.AccessTTL || config.RecentAuthenticationTTL <= 0 {
		return nil, errors.New("account store and bounded session TTLs are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.BcryptCost == 0 {
		config.BcryptCost = bcrypt.DefaultCost
	}
	return &Service{store: config.Store, accessTTL: config.AccessTTL, refreshTTL: config.RefreshTTL, recentAuthenticationTTL: config.RecentAuthenticationTTL, bcryptCost: config.BcryptCost, now: config.Now}, nil
}

// EnsureBootstrapOperator 首次创建部署管理员；已存在账号不会因 Controller 重启而轮换密码或撤销 session。
func (service *Service) EnsureBootstrapOperator(ctx context.Context, login, password string) (*cloudv1.AccountProfile, error) {
	login = strings.TrimSpace(strings.ToLower(login))
	if login == "" || len(password) < 8 {
		return nil, errors.New("bootstrap operator login and password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), service.bcryptCost)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	record, err := service.store.EnsureBootstrapOperator(ctx, Record{Profile: &cloudv1.AccountProfile{AccountId: uuid.NewString(), Email: login, DisplayName: "系统管理员", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}, PasswordHash: hash, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER, cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN}})
	if err != nil {
		return nil, err
	}
	return record.Profile, nil
}

// Register 创建普通账号、基础角色、首个 session；初始 Subscription 由 Store 同事务创建。
func (service *Service) Register(ctx context.Context, request *cloudv1.RegisterAccountRequest) (*cloudv1.RegisterAccountResponse, error) {
	if request == nil {
		return nil, ErrAccountConflict
	}
	email := strings.TrimSpace(strings.ToLower(request.GetEmail()))
	displayName := strings.TrimSpace(request.GetDisplayName())
	if !strings.Contains(email, "@") || len(request.GetPassword()) < 8 || displayName == "" {
		return nil, ErrAccountConflict
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.GetPassword()), service.bcryptCost)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	record := Record{Profile: &cloudv1.AccountProfile{AccountId: uuid.NewString(), Email: email, DisplayName: displayName, State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}, PasswordHash: hash, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}}
	credential, session, err := service.newSession(record.Profile.GetAccountId(), 1, now)
	if err != nil {
		return nil, err
	}
	if err := service.store.CreateAccount(ctx, record, session); err != nil {
		return nil, err
	}
	return &cloudv1.RegisterAccountResponse{Account: record.Profile, Session: credential}, nil
}

// Login 验证密码并创建持久 session；禁用账号不能创建新 session。
func (service *Service) Login(ctx context.Context, request *cloudv1.LoginAccountRequest) (*cloudv1.LoginAccountResponse, error) {
	if request == nil {
		return nil, ErrUnauthenticated
	}
	record, err := service.store.AccountByLogin(ctx, strings.TrimSpace(strings.ToLower(request.GetLogin())))
	if err != nil || bcrypt.CompareHashAndPassword(record.PasswordHash, []byte(request.GetPassword())) != nil {
		return nil, ErrUnauthenticated
	}
	if record.Profile.GetState() != cloudv1.AccountState_ACCOUNT_STATE_ACTIVE {
		return nil, ErrAccountDisabled
	}
	now := service.now().UTC()
	credential, session, err := service.newSession(record.Profile.GetAccountId(), 1, now)
	if err != nil {
		return nil, err
	}
	if err := service.store.PutSession(ctx, session); err != nil {
		return nil, err
	}
	return &cloudv1.LoginAccountResponse{Account: record.Profile, Roles: record.Roles, Session: credential}, nil
}

// Refresh 单次轮换 refresh token；旧 session 在同一事务内撤销。
func (service *Service) Refresh(ctx context.Context, request *cloudv1.RefreshAccountSessionRequest) (*cloudv1.RefreshAccountSessionResponse, error) {
	if request == nil || len(request.GetRefreshToken()) == 0 {
		return nil, ErrUnauthenticated
	}
	old, err := service.store.SessionByRefreshDigest(ctx, sha256.Sum256(request.GetRefreshToken()))
	if err != nil || old.Revoked || !service.now().UTC().Before(old.RefreshExpiresAt) {
		return nil, ErrUnauthenticated
	}
	record, err := service.store.AccountByID(ctx, old.AccountID)
	if err != nil || record.Profile.GetState() != cloudv1.AccountState_ACCOUNT_STATE_ACTIVE {
		return nil, ErrUnauthenticated
	}
	credential, next, err := service.newSession(old.AccountID, old.Revision+1, service.now().UTC())
	if err != nil {
		return nil, err
	}
	// Refresh credential 只轮换 session，不构成再次验证密码；recent-auth 窗口沿用旧 session 的绝对截止时间。
	next.RecentAuthExpiresAt = old.RecentAuthExpiresAt
	if err := service.store.RotateSession(ctx, old, next); err != nil {
		return nil, err
	}
	return &cloudv1.RefreshAccountSessionResponse{Account: record.Profile, Roles: record.Roles, Session: credential}, nil
}

// Logout 撤销当前账号的精确 session 或全部 session。
func (service *Service) Logout(ctx context.Context, request *cloudv1.LogoutAccountSessionRequest) (*cloudv1.LogoutAccountSessionResponse, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok || request == nil {
		return nil, ErrUnauthenticated
	}
	if err := service.store.RevokeSession(ctx, identity.Account.GetAccountId(), request.GetSessionId(), request.GetAllSessions()); err != nil {
		return nil, err
	}
	return &cloudv1.LogoutAccountSessionResponse{}, nil
}

// GetCurrent 返回认证上下文中的账号和角色，不重新解释浏览器输入。
func (service *Service) GetCurrent(ctx context.Context, _ *cloudv1.GetCurrentAccountRequest) (*cloudv1.GetCurrentAccountResponse, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, ErrUnauthenticated
	}
	return &cloudv1.GetCurrentAccountResponse{Account: identity.Account, Roles: identity.Roles, RecentAuthExpiresAt: timestamppb.New(identity.RecentAuthExpiresAt)}, nil
}

// VerifyRecentAuthentication 验证当前密码并延长精确 session 的高风险操作窗口。
func (service *Service) VerifyRecentAuthentication(ctx context.Context, request *cloudv1.VerifyRecentAuthenticationRequest) (*cloudv1.VerifyRecentAuthenticationResponse, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok || request == nil {
		return nil, ErrUnauthenticated
	}
	record, err := service.store.AccountByID(ctx, identity.Account.GetAccountId())
	if err != nil || bcrypt.CompareHashAndPassword(record.PasswordHash, []byte(request.GetPassword())) != nil {
		return nil, ErrUnauthenticated
	}
	expiresAt := service.now().UTC().Add(service.recentAuthenticationTTL)
	if err := service.store.SetRecentAuthentication(ctx, identity.SessionID, expiresAt); err != nil {
		return nil, err
	}
	return &cloudv1.VerifyRecentAuthenticationResponse{ExpiresAt: timestamppb.New(expiresAt)}, nil
}

// AuthenticateAccess 验证 bearer/cookie token 并构造 transport 可注入的认证身份。
func (service *Service) AuthenticateAccess(ctx context.Context, token []byte) (Identity, error) {
	if len(token) == 0 {
		return Identity{}, ErrUnauthenticated
	}
	session, err := service.store.SessionByAccessDigest(ctx, sha256.Sum256(token))
	if err != nil || session.Revoked || !service.now().UTC().Before(session.AccessExpiresAt) {
		return Identity{}, ErrUnauthenticated
	}
	record, err := service.store.AccountByID(ctx, session.AccountID)
	if err != nil || record.Profile.GetState() != cloudv1.AccountState_ACCOUNT_STATE_ACTIVE {
		return Identity{}, ErrUnauthenticated
	}
	return Identity{Account: record.Profile, Roles: record.Roles, SessionID: session.ID, RecentAuthExpiresAt: session.RecentAuthExpiresAt, CSRFDigest: session.CSRFDigest}, nil
}

// ValidateCSRF 比较当前 session 的持久摘要；只用于 cookie mutation 的双提交校验。
func ValidateCSRF(identity Identity, token []byte) bool {
	if len(token) == 0 {
		return false
	}
	digest := sha256.Sum256(token)
	var mismatch byte
	for index := range digest {
		mismatch |= digest[index] ^ identity.CSRFDigest[index]
	}
	return mismatch == 0
}

func (service *Service) newSession(accountID string, revision uint64, now time.Time) (*cloudv1.AccountSessionCredential, Session, error) {
	access, err := randomToken(32)
	if err != nil {
		return nil, Session{}, err
	}
	refresh, err := randomToken(32)
	if err != nil {
		return nil, Session{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return nil, Session{}, err
	}
	id := uuid.NewString()
	accessExpiresAt, refreshExpiresAt := now.Add(service.accessTTL), now.Add(service.refreshTTL)
	session := Session{ID: id, AccountID: accountID, AccessDigest: sha256.Sum256(access), RefreshDigest: sha256.Sum256(refresh), CSRFDigest: sha256.Sum256(csrf), CreatedAt: now, AccessExpiresAt: accessExpiresAt, RefreshExpiresAt: refreshExpiresAt, RecentAuthExpiresAt: now.Add(service.recentAuthenticationTTL), Revision: revision}
	return &cloudv1.AccountSessionCredential{SessionId: id, AccessToken: access, RefreshToken: refresh, AccessExpiresAt: timestamppb.New(accessExpiresAt), RefreshExpiresAt: timestamppb.New(refreshExpiresAt), CsrfToken: csrf}, session, nil
}

func randomToken(size int) ([]byte, error) {
	value := make([]byte, size)
	_, err := rand.Read(value)
	return value, err
}
