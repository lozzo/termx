// Package operator 组装运营查询、持久 mutation、实时目录和控制命令。
package operator

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	"github.com/anytty/anytty/cloud/controller/certificate"
	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/controller/enrollment"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Store 是运营持久查询和审计 mutation 的事务边界。
type Store interface {
	ListOperatorAccounts(context.Context, *cloudv1.PageRequest, time.Time) ([]*cloudv1.AccountSummary, string, error)
	GetOperatorAccount(context.Context, string, time.Time) (*cloudv1.AccountSummary, error)
	ListOperatorOrders(context.Context, *cloudv1.PageRequest) ([]*cloudv1.OrderProjection, string, error)
	ListOperatorSubscriptions(context.Context, *cloudv1.PageRequest) ([]*cloudv1.SubscriptionProjection, string, error)
	ListOperatorUsage(context.Context, *cloudv1.PageRequest, time.Time) ([]*cloudv1.UsagePeriodProjection, []*cloudv1.EdgeUsageProjection, string, error)
	ListOperatorAudit(context.Context, *cloudv1.PageRequest) ([]*cloudv1.OperatorAuditEvent, string, error)
	SetAccountState(context.Context, *cloudv1.SetAccountStateRequest, string, time.Time) (*cloudv1.AccountProfile, error)
	SetAccountRole(context.Context, *cloudv1.SetAccountRoleRequest, string, time.Time) ([]cloudv1.AccountRole, error)
	AuditRuntimeCommand(context.Context, string, string, string, string, cloudv1.RuntimeCommandResult, time.Time) error
	RelayBytesCurrentPeriod(context.Context, time.Time) (uint64, error)
}

// Config 固定 OperatorService 的持久和实时 owner。
type Config struct {
	Store        Store
	Edges        *edgeconfig.Service
	Enrollment   *enrollment.Service
	Directory    *directory.Directory
	Control      *control.Service
	Certificates *certificate.Service
	Now          func() time.Time
}

// Service 是运营 API 应用边界；Web view state 不进入该对象。
type Service struct {
	cloudv1.UnimplementedOperatorServiceServer
	config Config
}

// New 创建 OperatorService，任一真值 owner 缺失时 fail closed。
func New(config Config) (*Service, error) {
	if config.Store == nil || config.Edges == nil || config.Enrollment == nil || config.Directory == nil || config.Control == nil || config.Certificates == nil {
		return nil, errors.New("operator stores and runtime owners are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{config: config}, nil
}

// ListCertificateProfiles 返回当前档案、绑定和真实在线投影，不包含 PEM。
func (service *Service) ListCertificateProfiles(ctx context.Context, _ *cloudv1.ListCertificateProfilesRequest) (*cloudv1.ListCertificateProfilesResponse, error) {
	if _, err := requireOperator(ctx, false); err != nil {
		return nil, err
	}
	return service.config.Certificates.ListProfiles(ctx)
}

// UploadCertificateProfile 要求最近认证，并把双文件 mutation 委托给证书领域。
func (service *Service) UploadCertificateProfile(ctx context.Context, request *cloudv1.UploadCertificateProfileRequest) (*cloudv1.UploadCertificateProfileResponse, error) {
	actor, err := requireOperator(ctx, true)
	if err != nil {
		return nil, err
	}
	return service.config.Certificates.UploadProfile(ctx, request, actor.Account.GetAccountId())
}

// BindCertificateProfile 要求最近认证，并使用 binding revision 避免覆盖并发操作。
func (service *Service) BindCertificateProfile(ctx context.Context, request *cloudv1.BindCertificateProfileRequest) (*cloudv1.BindCertificateProfileResponse, error) {
	actor, err := requireOperator(ctx, true)
	if err != nil {
		return nil, err
	}
	return service.config.Certificates.BindProfile(ctx, request, actor.Account.GetAccountId())
}

// GetOverview 聚合持久用量和当前 Directory，不用数据库构造在线数量。
func (service *Service) GetOverview(ctx context.Context, _ *cloudv1.GetOperatorOverviewRequest) (*cloudv1.GetOperatorOverviewResponse, error) {
	if _, err := requireOperator(ctx, false); err != nil {
		return nil, err
	}
	edges, err := service.config.Edges.ListEdges(ctx)
	if err != nil {
		return nil, err
	}
	runtimeEdges, err := service.config.Directory.ListEdges(ctx)
	if err != nil {
		return nil, err
	}
	daemons, err := service.config.Enrollment.ListManagedDaemons(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := service.config.Directory.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	bytes, err := service.config.Store.RelayBytesCurrentPeriod(ctx, service.config.Now().UTC())
	if err != nil {
		return nil, err
	}
	overview := &cloudv1.OperatorOverview{EdgeTotal: uint64(len(edges)), EdgeOnline: uint64(len(runtimeEdges)), DaemonTotal: uint64(len(daemons.GetDaemons())), ClientSessionOnline: uint64(len(sessions)), RelayBytesCurrentPeriod: bytes, ControllerInstanceId: service.config.Directory.InstanceID(), GeneratedAt: timestamppb.New(service.config.Now().UTC())}
	for _, daemon := range daemons.GetDaemons() {
		if daemon.GetRuntime().GetOnline() {
			overview.DaemonOnline++
		}
	}
	for _, session := range sessions {
		if session.Relay {
			overview.RelaySessionOnline++
		} else {
			overview.P2PSessionOnline++
		}
	}
	return &cloudv1.GetOperatorOverviewResponse{Overview: overview}, nil
}

// ListAccounts 返回持久账号、订阅、Entitlement、daemon 数和用量摘要。
func (service *Service) ListAccounts(ctx context.Context, request *cloudv1.ListOperatorAccountsRequest) (*cloudv1.ListOperatorAccountsResponse, error) {
	if _, err := requireOperator(ctx, false); err != nil {
		return nil, err
	}
	page := pageFrom(request)
	values, next, err := service.config.Store.ListOperatorAccounts(ctx, page, service.config.Now().UTC())
	return &cloudv1.ListOperatorAccountsResponse{Accounts: values, NextCursor: next}, err
}

// GetAccount 返回单账号运营详情。
func (service *Service) GetAccount(ctx context.Context, request *cloudv1.GetOperatorAccountRequest) (*cloudv1.GetOperatorAccountResponse, error) {
	if _, err := requireOperator(ctx, false); err != nil {
		return nil, err
	}
	if request == nil || strings.TrimSpace(request.GetAccountId()) == "" {
		return nil, errors.New("account ID is required")
	}
	value, err := service.config.Store.GetOperatorAccount(ctx, request.GetAccountId(), service.config.Now().UTC())
	return &cloudv1.GetOperatorAccountResponse{Account: value}, err
}

// ListRuntimeSessions 只读取 Directory 当前 generation，不提供数据库 fallback。
func (service *Service) ListRuntimeSessions(ctx context.Context, _ *cloudv1.ListRuntimeSessionsRequest) (*cloudv1.ListRuntimeSessionsResponse, error) {
	if _, err := requireOperator(ctx, false); err != nil {
		return nil, err
	}
	sessions, err := service.config.Directory.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*cloudv1.RuntimeSessionProjection, 0, len(sessions))
	for _, value := range sessions {
		result = append(result, &cloudv1.RuntimeSessionProjection{SessionId: value.Session.GetSessionId(), AccountId: value.Session.GetAccountId(), DaemonId: value.Session.GetDaemonId(), EdgeId: value.Location.EdgeID, ClientId: value.Session.GetClientId(), Product: value.Session.GetProduct(), Relay: value.Relay, Generation: value.Location.Generation, ConnectedAt: timestamppb.New(value.ConnectedAt)})
	}
	return &cloudv1.ListRuntimeSessionsResponse{Sessions: result}, nil
}

// ListOrders 返回持久订单分页。
func (service *Service) ListOrders(ctx context.Context, request *cloudv1.ListOperatorOrdersRequest) (*cloudv1.ListOperatorOrdersResponse, error) {
	if _, err := requireOperator(ctx, false); err != nil {
		return nil, err
	}
	values, next, err := service.config.Store.ListOperatorOrders(ctx, pageFrom(request))
	return &cloudv1.ListOperatorOrdersResponse{Orders: values, NextCursor: next}, err
}

// ListSubscriptions 返回持久订阅分页。
func (service *Service) ListSubscriptions(ctx context.Context, request *cloudv1.ListOperatorSubscriptionsRequest) (*cloudv1.ListOperatorSubscriptionsResponse, error) {
	if _, err := requireOperator(ctx, false); err != nil {
		return nil, err
	}
	values, next, err := service.config.Store.ListOperatorSubscriptions(ctx, pageFrom(request))
	return &cloudv1.ListOperatorSubscriptionsResponse{Subscriptions: values, NextCursor: next}, err
}

// ListUsage 返回账号账期和 Edge 聚合，不读取 Edge 内存计数冒充结算。
func (service *Service) ListUsage(ctx context.Context, request *cloudv1.ListOperatorUsageRequest) (*cloudv1.ListOperatorUsageResponse, error) {
	if _, err := requireOperator(ctx, false); err != nil {
		return nil, err
	}
	accounts, edges, next, err := service.config.Store.ListOperatorUsage(ctx, pageFrom(request), service.config.Now().UTC())
	return &cloudv1.ListOperatorUsageResponse{Accounts: accounts, Edges: edges, NextCursor: next}, err
}

// ListAudit 返回持久运营事实；读取本身不新增审计。
func (service *Service) ListAudit(ctx context.Context, request *cloudv1.ListOperatorAuditRequest) (*cloudv1.ListOperatorAuditResponse, error) {
	if _, err := requireOperator(ctx, false); err != nil {
		return nil, err
	}
	values, next, err := service.config.Store.ListOperatorAudit(ctx, pageFrom(request))
	return &cloudv1.ListOperatorAuditResponse{Events: values, NextCursor: next}, err
}

// SetAccountState 使用 revision CAS 禁用或恢复账号，并与审计同事务提交。
func (service *Service) SetAccountState(ctx context.Context, request *cloudv1.SetAccountStateRequest) (*cloudv1.SetAccountStateResponse, error) {
	actor, err := requireOperator(ctx, true)
	if err != nil {
		return nil, err
	}
	if request == nil || request.GetAccountId() == "" || request.GetExpectedRevision() == 0 || strings.TrimSpace(request.GetReason()) == "" || request.GetState() < cloudv1.AccountState_ACCOUNT_STATE_ACTIVE || request.GetState() > cloudv1.AccountState_ACCOUNT_STATE_DISABLED {
		return nil, errors.New("valid account state mutation is required")
	}
	value, err := service.config.Store.SetAccountState(ctx, request, actor.Account.GetAccountId(), service.config.Now().UTC())
	return &cloudv1.SetAccountStateResponse{Account: value}, err
}

// SetAccountRole 调整 operator/admin 角色；普通 user 基础角色不可移除。
func (service *Service) SetAccountRole(ctx context.Context, request *cloudv1.SetAccountRoleRequest) (*cloudv1.SetAccountRoleResponse, error) {
	actor, err := requireOperator(ctx, true)
	if err != nil {
		return nil, err
	}
	if request == nil || request.GetAccountId() == "" || strings.TrimSpace(request.GetReason()) == "" || (request.GetRole() != cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR && request.GetRole() != cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN) {
		return nil, errors.New("valid operator role mutation is required")
	}
	roles, err := service.config.Store.SetAccountRole(ctx, request, actor.Account.GetAccountId(), service.config.Now().UTC())
	return &cloudv1.SetAccountRoleResponse{Roles: roles}, err
}

// DisconnectDaemon 执行不持久重试的 generation-fenced 实时断开，并持久记录结果。
func (service *Service) DisconnectDaemon(ctx context.Context, request *cloudv1.DisconnectDaemonRequest) (*cloudv1.DisconnectDaemonResponse, error) {
	actor, err := requireOperator(ctx, true)
	if err != nil {
		return nil, err
	}
	if request == nil || request.GetDaemonId() == "" || request.GetGeneration() == 0 || strings.TrimSpace(request.GetReason()) == "" {
		return nil, errors.New("valid daemon disconnect is required")
	}
	result := service.config.Control.DisconnectDaemon(ctx, request.GetDaemonId(), request.GetGeneration(), request.GetReason())
	// HTTP 取消只终止本次等待；命令已经发出后，结果审计必须使用独立的短事务完成。
	auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.config.Store.AuditRuntimeCommand(auditCtx, actor.Account.GetAccountId(), "daemon.disconnect", request.GetDaemonId(), request.GetReason(), result, service.config.Now().UTC()); err != nil {
		return nil, err
	}
	return &cloudv1.DisconnectDaemonResponse{Result: result}, nil
}

// DisconnectSession 执行精确客户端 session 断开并持久记录结果。
func (service *Service) DisconnectSession(ctx context.Context, request *cloudv1.DisconnectSessionRequest) (*cloudv1.DisconnectSessionResponse, error) {
	actor, err := requireOperator(ctx, true)
	if err != nil {
		return nil, err
	}
	if request == nil || request.GetSessionId() == "" || request.GetGeneration() == 0 || strings.TrimSpace(request.GetReason()) == "" {
		return nil, errors.New("valid session disconnect is required")
	}
	result := service.config.Control.DisconnectSession(ctx, request.GetSessionId(), request.GetGeneration(), request.GetReason())
	// 与 daemon 命令相同，客户端断开不能撤销已经发生的控制动作或其审计事实。
	auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.config.Store.AuditRuntimeCommand(auditCtx, actor.Account.GetAccountId(), "session.disconnect", request.GetSessionId(), request.GetReason(), result, service.config.Now().UTC()); err != nil {
		return nil, err
	}
	return &cloudv1.DisconnectSessionResponse{Result: result}, nil
}

func requireOperator(ctx context.Context, recent bool) (account.Identity, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok || !identity.HasRole(cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR) {
		return account.Identity{}, account.ErrUnauthenticated
	}
	if recent && !time.Now().UTC().Before(identity.RecentAuthExpiresAt) {
		return account.Identity{}, errors.New("recent authentication is required")
	}
	return identity, nil
}
func pageFrom(value any) *cloudv1.PageRequest {
	switch request := value.(type) {
	case *cloudv1.ListOperatorAccountsRequest:
		if request != nil && request.GetPage() != nil {
			return request.GetPage()
		}
	case *cloudv1.ListOperatorOrdersRequest:
		if request != nil && request.GetPage() != nil {
			return request.GetPage()
		}
	case *cloudv1.ListOperatorSubscriptionsRequest:
		if request != nil && request.GetPage() != nil {
			return request.GetPage()
		}
	case *cloudv1.ListOperatorUsageRequest:
		if request != nil && request.GetPage() != nil {
			return request.GetPage()
		}
	case *cloudv1.ListOperatorAuditRequest:
		if request != nil && request.GetPage() != nil {
			return request.GetPage()
		}
	}
	return &cloudv1.PageRequest{PageSize: 50}
}
