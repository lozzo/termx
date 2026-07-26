package enrollment

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/muxvia/muxvia/cloud/controller/account"
	"github.com/muxvia/muxvia/cloud/controller/control"
	"github.com/muxvia/muxvia/cloud/controller/directory"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
)

// ManagementStore 是用户自助 daemon 查询和撤销的持久边界；实时 Presence 不得写入该接口。
type ManagementStore interface {
	ListDaemonsByAccount(context.Context, string) ([]Daemon, error)
	RevokeDaemon(context.Context, string, string, uint64, string, time.Time) (Daemon, error)
}

// ManagementConfig 固定用户 daemon API 所需的持久 owner、Directory、控制流和命令前缀。
type ManagementConfig struct {
	Enrollment    *Service
	Store         ManagementStore
	Directory     *directory.Directory
	Control       *control.Service
	CommandPrefix string
	Now           func() time.Time
}

// ManagementService 是登录用户管理自己 daemon 的应用边界；账号只来自认证 context。
type ManagementService struct {
	cloudv1.UnimplementedDaemonManagementServiceServer
	config ManagementConfig
}

// NewManagementService 创建用户 daemon 服务；缺少任一权威 owner 时 fail closed。
func NewManagementService(config ManagementConfig) (*ManagementService, error) {
	config.CommandPrefix = strings.TrimSpace(config.CommandPrefix)
	if config.Enrollment == nil || config.Store == nil || config.Directory == nil || config.Control == nil || config.CommandPrefix == "" {
		return nil, errors.New("daemon management owners and command prefix are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &ManagementService{config: config}, nil
}

// CreateMyEnrollment 为当前账号创建一次性 code，浏览器不能提交账号或 Controller 参数。
func (service *ManagementService) CreateMyEnrollment(ctx context.Context, request *cloudv1.CreateMyDaemonEnrollmentRequest) (*cloudv1.CreateDaemonEnrollmentResponse, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok || request == nil || strings.TrimSpace(request.GetDaemonName()) == "" {
		return nil, account.ErrUnauthenticated
	}
	return service.config.Enrollment.CreateEnrollment(ctx, &cloudv1.CreateDaemonEnrollmentRequest{
		AccountId: identity.Account.GetAccountId(), AccountName: identity.Account.GetDisplayName(), DaemonName: strings.TrimSpace(request.GetDaemonName()),
	}, service.config.CommandPrefix)
}

// ListMyDaemons 合并当前账号的持久 identity 与 Directory 内存 Presence。
func (service *ManagementService) ListMyDaemons(ctx context.Context, _ *cloudv1.ListMyDaemonsRequest) (*cloudv1.ListMyDaemonsResponse, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok {
		return nil, account.ErrUnauthenticated
	}
	daemons, err := service.config.Store.ListDaemonsByAccount(ctx, identity.Account.GetAccountId())
	if err != nil {
		return nil, err
	}
	managed, err := service.config.Enrollment.projectManagedDaemons(ctx, daemons)
	if err != nil {
		return nil, err
	}
	return &cloudv1.ListMyDaemonsResponse{Daemons: managed}, nil
}

// RevokeMyDaemon 先提交持久撤销，再对撤销前捕获的当前 generation 发一次 best-effort 断开。
func (service *ManagementService) RevokeMyDaemon(ctx context.Context, request *cloudv1.RevokeMyDaemonRequest) (*cloudv1.RevokeMyDaemonResponse, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok || request == nil || strings.TrimSpace(request.GetDaemonId()) == "" || request.GetExpectedRevision() == 0 || strings.TrimSpace(request.GetReason()) == "" {
		return nil, account.ErrUnauthenticated
	}
	location, online, err := service.config.Directory.LocateDaemon(ctx, request.GetDaemonId())
	if err != nil {
		return nil, err
	}
	daemon, err := service.config.Store.RevokeDaemon(ctx, identity.Account.GetAccountId(), request.GetDaemonId(), request.GetExpectedRevision(), strings.TrimSpace(request.GetReason()), service.config.Now().UTC())
	if err != nil {
		return nil, err
	}
	applied := false
	if online {
		result := service.config.Control.DisconnectDaemon(ctx, daemon.ID, location.Generation, "daemon identity revoked by account owner")
		applied = result == cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_APPLIED
	}
	return &cloudv1.RevokeMyDaemonResponse{Daemon: projectDaemon(daemon), RuntimeWasOnline: online, RuntimeDisconnectApplied: applied}, nil
}
