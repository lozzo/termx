// Package account 拥有 Cloud 账号 credential、Access JWT、Refresh token、角色和 recent-auth 生命周期。
package account

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// ErrUnauthenticated 表示 credential 不存在、失效或密码校验失败。
	ErrUnauthenticated = errors.New("account credential is invalid")
	// ErrLoginUnavailable 表示登录所需的持久化或系统依赖暂时不可用。
	ErrLoginUnavailable = errors.New("account login is unavailable")
	// ErrAccountNotFound 是 Store 的内部缺失记录契约；transport 不得直接暴露。
	ErrAccountNotFound = errors.New("account record was not found")
	// ErrAccountConflict 表示账号标识已存在或 Refresh token CAS 失败。
	ErrAccountConflict = errors.New("account state conflicts")
	// ErrAccountDisabled 表示账号已被运营人员禁用。
	ErrAccountDisabled = errors.New("account is disabled")
	// ErrForbidden 表示身份有效但无权执行该账号操作。
	ErrForbidden = errors.New("account operation is forbidden")
	// ErrRecentAuthenticationRequired 表示高风险操作的最近认证窗口已失效。
	ErrRecentAuthenticationRequired = errors.New("recent authentication is required")
	// ErrInvalidArgument 表示外部请求不满足已评审契约。
	ErrInvalidArgument = errors.New("account request is invalid")
	// ErrSetupCredentialInvalid 对过期、已使用、重放和未知 setup credential 使用同一外部结果。
	ErrSetupCredentialInvalid = errors.New("account setup credential is invalid or expired")
)

// Record 是账号领域读取模型；密码 hash 只在 account package 与 Store 之间流动。
type Record struct {
	Profile             *cloudv1.AccountProfile
	PasswordHash        []byte
	SetupDigest         []byte
	SetupExpiresAt      time.Time
	CredentialRevision  uint64
	CredentialUpdatedAt time.Time
	Roles               []cloudv1.AccountRole
}

// RefreshToken 是持久登录记录；数据库只保存 Refresh token 与 CSRF token 的 SHA-256。
type RefreshToken struct {
	ID, AccountID           string
	TokenDigest, CSRFDigest [sha256.Size]byte
	CreatedAt, ExpiresAt    time.Time
	RecentAuthExpiresAt     time.Time
	Revision                uint64
	Revoked                 bool
}

// Store 是账号写事务和 Refresh token CAS 的持久边界。
type Store interface {
	EnsureBootstrapOperator(context.Context, Record) (Record, error)
	ProvisionAccount(context.Context, Record, string, string, time.Time) error
	AccountByLogin(context.Context, string) (Record, error)
	AccountByExactEmail(context.Context, string) (Record, error)
	AccountByID(context.Context, string) (Record, error)
	CreateRefreshToken(context.Context, Record, RefreshToken, time.Time) (Record, error)
	RefreshTokenByDigest(context.Context, [sha256.Size]byte) (RefreshToken, error)
	RotateRefreshToken(context.Context, RefreshToken, RefreshToken, time.Time) (Record, error)
	RevokeRefreshToken(context.Context, string, string, bool) error
	SetRecentAuthentication(context.Context, string, string, Record, time.Time, time.Time) error
	ListAccountRefreshTokens(context.Context, string, time.Time) ([]RefreshToken, error)
	UpdatePassword(context.Context, string, string, Record, []byte, time.Time) (*cloudv1.AccountProfile, error)
	RedeemAccountSetup(context.Context, [sha256.Size]byte, []byte, RefreshToken, time.Time) (Record, error)
	ResetAccountSetup(context.Context, string, string, string, [sha256.Size]byte, time.Time, time.Time) (*cloudv1.AccountProfile, error)
}

// Config 固定账号 Store、token 时限和可替换的时间/随机来源。
type Config struct {
	Store                   Store
	AccessTTL               time.Duration
	RefreshTTL              time.Duration
	RecentAuthenticationTTL time.Duration
	SetupTTL                time.Duration
	BcryptCost              int
	AccessSigningKey        ed25519.PrivateKey
	AccessSigningKeyID      string
	AccessIssuer            string
	AccessAudience          string
	Now                     func() time.Time
}

// Service 是账号应用边界，不拥有 HTTP cookie、公开 gRPC 注册或 transport 状态。
type Service struct {
	store                   Store
	accessTTL               time.Duration
	refreshTTL              time.Duration
	recentAuthenticationTTL time.Duration
	setupTTL                time.Duration
	bcryptCost              int
	dummyPasswordHash       []byte
	accessSigningKey        ed25519.PrivateKey
	accessVerificationKey   ed25519.PublicKey
	accessSigningKeyID      string
	accessIssuer            string
	accessAudience          string
	now                     func() time.Time
}

// New 创建账号服务；credential 策略缺失时直接失败，不能回退到固定 token。
func New(config Config) (*Service, error) {
	config.AccessSigningKeyID = strings.TrimSpace(config.AccessSigningKeyID)
	config.AccessIssuer = strings.TrimSpace(config.AccessIssuer)
	config.AccessAudience = strings.TrimSpace(config.AccessAudience)
	if config.Store == nil || config.AccessTTL <= 0 || config.RefreshTTL <= config.AccessTTL || config.RecentAuthenticationTTL <= 0 || config.SetupTTL <= 0 || len(config.AccessSigningKey) != ed25519.PrivateKeySize || config.AccessSigningKeyID == "" || config.AccessIssuer == "" || config.AccessAudience == "" {
		return nil, errors.New("account store, bounded token TTLs, and JWT signing configuration are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.BcryptCost == 0 {
		config.BcryptCost = bcrypt.DefaultCost
	}
	dummyPasswordHash, err := bcrypt.GenerateFromPassword([]byte("anytty-cloud-dummy-password"), config.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("create dummy password verifier: %w", err)
	}
	signingKey := append(ed25519.PrivateKey(nil), config.AccessSigningKey...)
	verificationKey := append(ed25519.PublicKey(nil), signingKey.Public().(ed25519.PublicKey)...)
	return &Service{store: config.Store, accessTTL: config.AccessTTL, refreshTTL: config.RefreshTTL, recentAuthenticationTTL: config.RecentAuthenticationTTL, setupTTL: config.SetupTTL, bcryptCost: config.BcryptCost, dummyPasswordHash: dummyPasswordHash, accessSigningKey: signingKey, accessVerificationKey: verificationKey, accessSigningKeyID: config.AccessSigningKeyID, accessIssuer: config.AccessIssuer, accessAudience: config.AccessAudience, now: config.Now}, nil
}

// EnsureBootstrapOperator 首次创建部署管理员；已存在账号不会因 Controller 重启而轮换密码或撤销 Refresh token。
func (service *Service) EnsureBootstrapOperator(ctx context.Context, login, password string) (*cloudv1.AccountProfile, error) {
	login = NormalizeLogin(login)
	if !strings.Contains(login, "@") {
		return nil, errors.New("bootstrap administrator email is required")
	}
	existing, err := service.store.AccountByExactEmail(ctx, login)
	if err == nil {
		if err := validateBootstrapRecord(existing, login); err != nil {
			return nil, err
		}
		return existing.Profile, nil
	}
	if !errors.Is(err, ErrAccountNotFound) {
		return nil, err
	}
	if !validPassword(password) {
		return nil, errors.New("bootstrap administrator password is required for first creation")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), service.bcryptCost)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	record, err := service.store.EnsureBootstrapOperator(ctx, Record{Profile: &cloudv1.AccountProfile{AccountId: uuid.NewString(), Email: login, DisplayName: "系统管理员", State: cloudv1.AccountState_ACCOUNT_STATE_ACTIVE, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}, PasswordHash: hash, CredentialRevision: 1, CredentialUpdatedAt: now, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER, cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN}})
	if err != nil {
		return nil, err
	}
	if err := validateBootstrapRecord(record, login); err != nil {
		return nil, err
	}
	return record.Profile, nil
}

// ProvisionAccount 只允许最近验证过的管理员创建待设置普通账号。
func (service *Service) ProvisionAccount(ctx context.Context, request *cloudv1.ProvisionAccountRequest) (*cloudv1.ProvisionAccountResponse, error) {
	identity, err := requireAdmin(ctx, true, service.now().UTC())
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, ErrInvalidArgument
	}
	email := NormalizeLogin(request.GetEmail())
	displayName := strings.TrimSpace(request.GetDisplayName())
	reason := strings.TrimSpace(request.GetReason())
	if !strings.Contains(email, "@") || displayName == "" || reason == "" {
		return nil, ErrInvalidArgument
	}
	setupCredential, setupDigest, err := newSetupCredential()
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	expiresAt := now.Add(service.setupTTL)
	record := Record{Profile: &cloudv1.AccountProfile{AccountId: uuid.NewString(), Email: email, DisplayName: displayName, State: cloudv1.AccountState_ACCOUNT_STATE_PENDING, Revision: 1, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}, SetupDigest: setupDigest[:], SetupExpiresAt: expiresAt, CredentialRevision: 1, CredentialUpdatedAt: now, Roles: []cloudv1.AccountRole{cloudv1.AccountRole_ACCOUNT_ROLE_USER}}
	if err := service.store.ProvisionAccount(ctx, record, identity.Account.GetAccountId(), reason, now); err != nil {
		return nil, err
	}
	return &cloudv1.ProvisionAccountResponse{Account: record.Profile, SetupCredential: setupCredential, ExpiresAt: timestamppb.New(expiresAt)}, nil
}

// ResetAccountSetup 轮换一次性 setup credential、清除旧密码并撤销目标账号全部 Refresh token。
func (service *Service) ResetAccountSetup(ctx context.Context, request *cloudv1.ResetAccountSetupRequest) (*cloudv1.ResetAccountSetupResponse, error) {
	identity, err := requireAdmin(ctx, true, service.now().UTC())
	if err != nil {
		return nil, err
	}
	if request == nil || strings.TrimSpace(request.GetAccountId()) == "" || strings.TrimSpace(request.GetReason()) == "" {
		return nil, ErrInvalidArgument
	}
	credential, digest, err := newSetupCredential()
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	expiresAt := now.Add(service.setupTTL)
	profile, err := service.store.ResetAccountSetup(ctx, request.GetAccountId(), identity.Account.GetAccountId(), strings.TrimSpace(request.GetReason()), digest, expiresAt, now)
	if err != nil {
		return nil, err
	}
	return &cloudv1.ResetAccountSetupResponse{Account: profile, SetupCredential: credential, ExpiresAt: timestamppb.New(expiresAt)}, nil
}

// Login 验证密码，签发 Access JWT，并创建持久 Refresh token。
func (service *Service) Login(ctx context.Context, request *cloudv1.LoginAccountRequest) (*cloudv1.LoginAccountResponse, error) {
	if request == nil {
		return nil, ErrUnauthenticated
	}
	record, lookupErr := service.store.AccountByLogin(ctx, NormalizeLogin(request.GetLogin()))
	passwordHash := record.PasswordHash
	recordValid := lookupErr == nil && record.Profile != nil && validPasswordHash(record.PasswordHash)
	if !recordValid {
		passwordHash = service.dummyPasswordHash
	}
	passwordErr := bcrypt.CompareHashAndPassword(passwordHash, []byte(request.GetPassword()))
	if lookupErr != nil && !errors.Is(lookupErr, ErrAccountNotFound) {
		return nil, ErrLoginUnavailable
	}
	if !recordValid || !validPassword(request.GetPassword()) || passwordErr != nil {
		return nil, ErrUnauthenticated
	}
	switch record.Profile.GetState() {
	case cloudv1.AccountState_ACCOUNT_STATE_ACTIVE:
	case cloudv1.AccountState_ACCOUNT_STATE_DISABLED:
		return nil, ErrAccountDisabled
	case cloudv1.AccountState_ACCOUNT_STATE_PENDING:
		return nil, ErrUnauthenticated
	default:
		return nil, ErrLoginUnavailable
	}
	now := service.now().UTC()
	credential, refresh, err := service.newRefreshToken(record.Profile.GetAccountId(), 1, now)
	if err != nil {
		return nil, ErrLoginUnavailable
	}
	locked, err := service.store.CreateRefreshToken(ctx, record, refresh, now)
	if err != nil {
		return nil, ErrLoginUnavailable
	}
	if err := service.issueAccess(credential, locked, refresh, now); err != nil {
		return nil, ErrLoginUnavailable
	}
	return &cloudv1.LoginAccountResponse{Account: locked.Profile, Roles: locked.Roles, Credential: credential}, nil
}

// NormalizeLogin 是密码登录与登录限流共用的唯一账号规范化规则。
func NormalizeLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

// Refresh 单次轮换 Refresh token，并签发新的 Access JWT。
func (service *Service) Refresh(ctx context.Context, request *cloudv1.RefreshAccountTokenRequest) (*cloudv1.RefreshAccountTokenResponse, error) {
	if request == nil || len(request.GetRefreshToken()) == 0 {
		return nil, ErrUnauthenticated
	}
	now := service.now().UTC()
	old, err := service.store.RefreshTokenByDigest(ctx, sha256.Sum256(request.GetRefreshToken()))
	if err != nil || old.Revoked || !now.Before(old.ExpiresAt) {
		return nil, ErrUnauthenticated
	}
	credential, next, err := service.newRefreshToken(old.AccountID, old.Revision+1, now)
	if err != nil {
		return nil, err
	}
	next.RecentAuthExpiresAt = old.RecentAuthExpiresAt
	record, err := service.store.RotateRefreshToken(ctx, old, next, now)
	if err != nil {
		return nil, err
	}
	if err := service.issueAccess(credential, record, next, now); err != nil {
		return nil, err
	}
	return &cloudv1.RefreshAccountTokenResponse{Account: record.Profile, Roles: record.Roles, Credential: credential}, nil
}

// Logout 撤销当前账号的精确 Refresh token 或全部 Refresh token。
func (service *Service) Logout(ctx context.Context, request *cloudv1.LogoutAccountRequest) (*cloudv1.LogoutAccountResponse, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok || request == nil {
		return nil, ErrUnauthenticated
	}
	if err := service.store.RevokeRefreshToken(ctx, identity.Account.GetAccountId(), identity.RefreshID, request.GetAllRefreshTokens()); err != nil {
		return nil, err
	}
	return &cloudv1.LogoutAccountResponse{}, nil
}

// GetCurrent 返回认证上下文中的账号和角色，不重新解释浏览器输入。
func (service *Service) GetCurrent(ctx context.Context, _ *cloudv1.GetCurrentAccountRequest) (*cloudv1.GetCurrentAccountResponse, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, ErrUnauthenticated
	}
	return &cloudv1.GetCurrentAccountResponse{Account: identity.Account, Roles: identity.Roles, RecentAuthExpiresAt: timestamppb.New(identity.RecentAuthExpiresAt)}, nil
}

// VerifyRecentAuthentication 验证当前密码，延长 Refresh token 的高风险窗口并重签 Access JWT。
func (service *Service) VerifyRecentAuthentication(ctx context.Context, request *cloudv1.VerifyRecentAuthenticationRequest) (*cloudv1.VerifyRecentAuthenticationResponse, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok || request == nil || !validPassword(request.GetPassword()) {
		return nil, ErrUnauthenticated
	}
	record, err := service.store.AccountByID(ctx, identity.Account.GetAccountId())
	if err != nil || bcrypt.CompareHashAndPassword(record.PasswordHash, []byte(request.GetPassword())) != nil {
		return nil, ErrUnauthenticated
	}
	now := service.now().UTC()
	expiresAt := now.Add(service.recentAuthenticationTTL)
	if record.Profile.GetState() != cloudv1.AccountState_ACCOUNT_STATE_ACTIVE || !validPasswordHash(record.PasswordHash) {
		return nil, ErrUnauthenticated
	}
	if err := service.store.SetRecentAuthentication(ctx, identity.Account.GetAccountId(), identity.RefreshID, record, expiresAt, now); err != nil {
		return nil, err
	}
	credential := &cloudv1.AccountTokenCredential{RefreshId: identity.RefreshID}
	refresh := RefreshToken{ID: identity.RefreshID, AccountID: identity.Account.GetAccountId(), CSRFDigest: identity.CSRFDigest, RecentAuthExpiresAt: expiresAt}
	if err := service.issueAccess(credential, record, refresh, now); err != nil {
		return nil, err
	}
	return &cloudv1.VerifyRecentAuthenticationResponse{ExpiresAt: timestamppb.New(expiresAt), Credential: credential}, nil
}

// ListRefreshTokens 返回当前账号仍可使用的持久登录凭据元数据。
func (service *Service) ListRefreshTokens(ctx context.Context, _ *cloudv1.ListAccountRefreshTokensRequest) (*cloudv1.ListAccountRefreshTokensResponse, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, ErrUnauthenticated
	}
	values, err := service.store.ListAccountRefreshTokens(ctx, identity.Account.GetAccountId(), service.now().UTC())
	if err != nil {
		return nil, err
	}
	response := &cloudv1.ListAccountRefreshTokensResponse{RefreshTokens: make([]*cloudv1.AccountRefreshTokenProjection, 0, len(values))}
	for _, value := range values {
		projection := &cloudv1.AccountRefreshTokenProjection{
			RefreshId: value.ID, Current: value.ID == identity.RefreshID, Revision: value.Revision,
			CreatedAt: timestamppb.New(value.CreatedAt), ExpiresAt: timestamppb.New(value.ExpiresAt),
		}
		if !value.RecentAuthExpiresAt.IsZero() {
			projection.RecentAuthExpiresAt = timestamppb.New(value.RecentAuthExpiresAt)
		}
		response.RefreshTokens = append(response.RefreshTokens, projection)
	}
	return response, nil
}

// ChangePassword 校验当前 verifier 后原子替换密码，并撤销其它 Refresh token。
func (service *Service) ChangePassword(ctx context.Context, request *cloudv1.ChangeAccountPasswordRequest) (*cloudv1.ChangeAccountPasswordResponse, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok || request == nil || !validPassword(request.GetCurrentPassword()) {
		return nil, ErrUnauthenticated
	}
	record, err := service.store.AccountByID(ctx, identity.Account.GetAccountId())
	if err != nil || bcrypt.CompareHashAndPassword(record.PasswordHash, []byte(request.GetCurrentPassword())) != nil {
		return nil, ErrUnauthenticated
	}
	if !validPassword(request.GetNewPassword()) || request.GetCurrentPassword() == request.GetNewPassword() {
		return nil, ErrInvalidArgument
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.GetNewPassword()), service.bcryptCost)
	if err != nil {
		return nil, err
	}
	profile, err := service.store.UpdatePassword(ctx, identity.Account.GetAccountId(), identity.RefreshID, record, hash, service.now().UTC())
	if err != nil {
		return nil, err
	}
	return &cloudv1.ChangeAccountPasswordResponse{Account: profile}, nil
}

// RedeemAccountSetup 原子消费一次性 setup credential，写入新密码并激活账号。
func (service *Service) RedeemAccountSetup(ctx context.Context, request *cloudv1.RedeemAccountSetupRequest) (*cloudv1.RedeemAccountSetupResponse, error) {
	if request == nil || len(request.GetSetupCredential()) != 43 || !validPassword(request.GetNewPassword()) {
		return nil, ErrSetupCredentialInvalid
	}
	if _, err := base64.RawURLEncoding.DecodeString(request.GetSetupCredential()); err != nil {
		return nil, ErrSetupCredentialInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.GetNewPassword()), service.bcryptCost)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	credential, refresh, err := service.newRefreshToken("", 1, now)
	if err != nil {
		return nil, err
	}
	record, err := service.store.RedeemAccountSetup(ctx, sha256.Sum256([]byte(request.GetSetupCredential())), hash, refresh, now)
	if err != nil {
		if errors.Is(err, ErrSetupCredentialInvalid) {
			return nil, ErrSetupCredentialInvalid
		}
		return nil, err
	}
	refresh.AccountID = record.Profile.GetAccountId()
	if err := service.issueAccess(credential, record, refresh, now); err != nil {
		return nil, err
	}
	return &cloudv1.RedeemAccountSetupResponse{Account: record.Profile, Roles: record.Roles, Credential: credential}, nil
}

// RevokeRefreshToken 撤销当前账号的另一个 Refresh token；当前 token 必须通过 Logout 退出。
func (service *Service) RevokeRefreshToken(ctx context.Context, request *cloudv1.RevokeAccountRefreshTokenRequest) (*cloudv1.RevokeAccountRefreshTokenResponse, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok || request == nil || strings.TrimSpace(request.GetRefreshId()) == "" || request.GetRefreshId() == identity.RefreshID {
		return nil, ErrUnauthenticated
	}
	if !service.now().UTC().Before(identity.RecentAuthExpiresAt) {
		return nil, ErrUnauthenticated
	}
	if err := service.store.RevokeRefreshToken(ctx, identity.Account.GetAccountId(), request.GetRefreshId(), false); err != nil {
		return nil, err
	}
	return &cloudv1.RevokeAccountRefreshTokenResponse{}, nil
}

// AuthenticateAccess 本地验签 Access JWT，不读取数据库。
func (service *Service) AuthenticateAccess(_ context.Context, token []byte) (Identity, error) {
	if len(token) == 0 {
		return Identity{}, ErrUnauthenticated
	}
	claims := &accessClaims{}
	parsed, err := jwt.ParseWithClaims(string(token), claims, func(value *jwt.Token) (any, error) {
		if value.Header["kid"] != service.accessSigningKeyID {
			return nil, ErrUnauthenticated
		}
		return service.accessVerificationKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}), jwt.WithIssuer(service.accessIssuer), jwt.WithAudience(service.accessAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(service.now))
	if err != nil || !parsed.Valid || claims.Subject == "" || claims.RefreshID == "" || claims.AccountState != int32(cloudv1.AccountState_ACCOUNT_STATE_ACTIVE) {
		return Identity{}, ErrUnauthenticated
	}
	csrf, err := base64.RawURLEncoding.DecodeString(claims.CSRFSHA256)
	if err != nil || len(csrf) != sha256.Size {
		return Identity{}, ErrUnauthenticated
	}
	roles := make([]cloudv1.AccountRole, 0, len(claims.Roles))
	for _, raw := range claims.Roles {
		role := cloudv1.AccountRole(raw)
		if role < cloudv1.AccountRole_ACCOUNT_ROLE_USER || role > cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN {
			return Identity{}, ErrUnauthenticated
		}
		roles = append(roles, role)
	}
	identity := Identity{Account: &cloudv1.AccountProfile{AccountId: claims.Subject, Email: claims.Email, DisplayName: claims.DisplayName, State: cloudv1.AccountState(claims.AccountState), Revision: claims.AccountRevision}, Roles: roles, RefreshID: claims.RefreshID, RecentAuthExpiresAt: time.Unix(claims.RecentAuthExpiresAt, 0).UTC()}
	copy(identity.CSRFDigest[:], csrf)
	return identity, nil
}

// ValidateCSRF 比较 Access JWT 内签名的摘要；只用于 cookie mutation 的双提交校验。
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

func (service *Service) newRefreshToken(accountID string, revision uint64, now time.Time) (*cloudv1.AccountTokenCredential, RefreshToken, error) {
	refresh, err := randomToken(32)
	if err != nil {
		return nil, RefreshToken{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return nil, RefreshToken{}, err
	}
	id := uuid.NewString()
	expiresAt := now.Add(service.refreshTTL)
	value := RefreshToken{ID: id, AccountID: accountID, TokenDigest: sha256.Sum256(refresh), CSRFDigest: sha256.Sum256(csrf), CreatedAt: now, ExpiresAt: expiresAt, RecentAuthExpiresAt: now.Add(service.recentAuthenticationTTL), Revision: revision}
	return &cloudv1.AccountTokenCredential{RefreshId: id, RefreshToken: refresh, RefreshExpiresAt: timestamppb.New(expiresAt), CsrfToken: csrf}, value, nil
}

type accessClaims struct {
	jwt.RegisteredClaims
	RefreshID           string  `json:"rid"`
	Email               string  `json:"email"`
	DisplayName         string  `json:"name"`
	AccountState        int32   `json:"state"`
	AccountRevision     uint64  `json:"account_revision"`
	Roles               []int32 `json:"roles"`
	RecentAuthExpiresAt int64   `json:"recent_auth_expires_at"`
	CSRFSHA256          string  `json:"csrf_sha256"`
}

func (service *Service) issueAccess(credential *cloudv1.AccountTokenCredential, record Record, refresh RefreshToken, now time.Time) error {
	if credential == nil || record.Profile == nil || refresh.ID == "" || refresh.AccountID == "" {
		return ErrUnauthenticated
	}
	expiresAt := now.Add(service.accessTTL)
	roles := make([]int32, len(record.Roles))
	for index, role := range record.Roles {
		roles[index] = int32(role)
	}
	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{Issuer: service.accessIssuer, Subject: refresh.AccountID, Audience: jwt.ClaimStrings{service.accessAudience}, ExpiresAt: jwt.NewNumericDate(expiresAt), IssuedAt: jwt.NewNumericDate(now), ID: uuid.NewString()},
		RefreshID:        refresh.ID, Email: record.Profile.GetEmail(), DisplayName: record.Profile.GetDisplayName(), AccountState: int32(record.Profile.GetState()), AccountRevision: record.Profile.GetRevision(), Roles: roles, RecentAuthExpiresAt: refresh.RecentAuthExpiresAt.Unix(), CSRFSHA256: base64.RawURLEncoding.EncodeToString(refresh.CSRFDigest[:]),
	}
	value := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	value.Header["kid"] = service.accessSigningKeyID
	signed, err := value.SignedString(service.accessSigningKey)
	if err != nil {
		return err
	}
	credential.AccessToken = signed
	credential.AccessExpiresAt = timestamppb.New(expiresAt)
	return nil
}

func randomToken(size int) ([]byte, error) {
	value := make([]byte, size)
	_, err := rand.Read(value)
	return value, err
}

func newSetupCredential() (string, [sha256.Size]byte, error) {
	raw, err := randomToken(32)
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	credential := base64.RawURLEncoding.EncodeToString(raw)
	return credential, sha256.Sum256([]byte(credential)), nil
}

func validPasswordHash(hash []byte) bool {
	_, err := bcrypt.Cost(hash)
	return err == nil
}

func validPassword(value string) bool {
	return utf8.ValidString(value) && len(value) >= 8 && len(value) <= 72
}

func validateBootstrapRecord(record Record, email string) error {
	if record.Profile == nil || record.Profile.GetEmail() != email || record.Profile.GetState() != cloudv1.AccountState_ACCOUNT_STATE_ACTIVE || !validPasswordHash(record.PasswordHash) {
		return errors.New("bootstrap administrator account is invalid")
	}
	for _, role := range record.Roles {
		if role == cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN {
			return nil
		}
	}
	return errors.New("bootstrap administrator role is missing")
}

func requireAdmin(ctx context.Context, recent bool, now time.Time) (Identity, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok {
		return Identity{}, ErrUnauthenticated
	}
	if !identity.HasRole(cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN) {
		return Identity{}, ErrForbidden
	}
	if recent && !now.Before(identity.RecentAuthExpiresAt) {
		return Identity{}, ErrRecentAuthenticationRequired
	}
	return identity, nil
}
