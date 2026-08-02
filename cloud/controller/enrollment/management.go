package enrollment

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

// ManagementStore 是用户自助 daemon 查询和生命周期变更的持久边界。
type ManagementStore interface {
	ListDaemonsByAccount(context.Context, string) ([]Daemon, error)
	ChangeDaemonState(context.Context, string, string, cloudv1.DaemonState, uint64, string, time.Time) (Daemon, error)
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

// ChangeMyDaemonState 先提交持久状态，再广播给所有在线 Edge。
func (service *ManagementService) ChangeMyDaemonState(ctx context.Context, request *cloudv1.ChangeMyDaemonStateRequest) (*cloudv1.ChangeMyDaemonStateResponse, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok {
		return nil, account.ErrUnauthenticated
	}
	if request == nil || strings.TrimSpace(request.GetDaemonId()) == "" || request.GetExpectedStateRevision() == 0 || strings.TrimSpace(request.GetReason()) == "" ||
		request.GetTargetState() != cloudv1.DaemonState_DAEMON_STATE_ACTIVE && request.GetTargetState() != cloudv1.DaemonState_DAEMON_STATE_BLOCKED && request.GetTargetState() != cloudv1.DaemonState_DAEMON_STATE_DELETED {
		return nil, ErrDaemonUnavailable
	}
	daemon, err := service.config.Store.ChangeDaemonState(ctx, identity.Account.GetAccountId(), request.GetDaemonId(), request.GetTargetState(), request.GetExpectedStateRevision(), strings.TrimSpace(request.GetReason()), service.config.Now().UTC())
	if err != nil {
		return nil, err
	}
	record := &cloudv1.DaemonStateRecord{DaemonId: daemon.ID, State: daemon.State, StateRevision: daemon.StateRevision}
	service.config.Control.BroadcastDaemonState(record)
	return &cloudv1.ChangeMyDaemonStateResponse{Daemon: projectDaemon(daemon)}, nil
}

// ListMyDaemonEdges 返回当前账号 daemon 的候选 Edge、软偏好和最近一次 daemon 侧测速。
func (service *ManagementService) ListMyDaemonEdges(ctx context.Context, request *cloudv1.ListMyDaemonEdgesRequest) (*cloudv1.ListMyDaemonEdgesResponse, error) {
	daemon, err := service.ownedDaemon(ctx, request.GetDaemonId())
	if err != nil {
		return nil, err
	}
	selection, err := service.edgeSelection(ctx, daemon)
	if err != nil && selection == nil {
		return nil, err
	}
	return &cloudv1.ListMyDaemonEdgesResponse{Selection: selection}, nil
}

// ChangeMyDaemonEdgePreference 保存软偏好，并可向当前在线 generation 发送立即重选命令。
func (service *ManagementService) ChangeMyDaemonEdgePreference(ctx context.Context, request *cloudv1.ChangeMyDaemonEdgePreferenceRequest) (*cloudv1.ChangeMyDaemonEdgePreferenceResponse, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok {
		return nil, account.ErrUnauthenticated
	}
	if request == nil || strings.TrimSpace(request.GetDaemonId()) == "" || request.GetExpectedPreferenceRevision() == 0 {
		return nil, ErrDaemonUnavailable
	}
	preferredEdgeID := strings.TrimSpace(request.GetPreferredEdgeId())
	current, err := service.ownedDaemon(ctx, request.GetDaemonId())
	if err != nil {
		return nil, err
	}
	selection, selectionErr := service.edgeSelection(ctx, current)
	if selectionErr != nil && selection == nil {
		return nil, selectionErr
	}
	if preferredEdgeID != "" {
		found := false
		for _, candidate := range selection.GetCandidates() {
			found = found || candidate.GetLocator().GetEdgeId() == preferredEdgeID
		}
		if !found {
			return nil, ErrDaemonUnavailable
		}
	}
	store, ok := service.config.Store.(EdgeSelectionStore)
	if !ok {
		return nil, errors.New("daemon Edge preference store is unavailable")
	}
	updated, err := store.ChangeDaemonEdgePreference(ctx, identity.Account.GetAccountId(), current.ID, preferredEdgeID, request.GetExpectedPreferenceRevision(), service.config.Now().UTC())
	if err != nil {
		return nil, err
	}
	selection, _ = service.edgeSelection(ctx, updated)
	response := &cloudv1.ChangeMyDaemonEdgePreferenceResponse{Selection: selection, Message: "偏好已保存；daemon 下次连接时会使用新选择"}
	if request.GetReselectNow() {
		response.ReselectAccepted, response.Message = service.requestReselect(ctx, updated)
	}
	return response, nil
}

// ReselectMyDaemonEdge 不改偏好，只让在线 daemon 立即测速并刷新 binding。
func (service *ManagementService) ReselectMyDaemonEdge(ctx context.Context, request *cloudv1.ReselectMyDaemonEdgeRequest) (*cloudv1.ReselectMyDaemonEdgeResponse, error) {
	daemon, err := service.ownedDaemon(ctx, request.GetDaemonId())
	if err != nil {
		return nil, err
	}
	selection, selectionErr := service.edgeSelection(ctx, daemon)
	if selectionErr != nil && selection == nil {
		return nil, selectionErr
	}
	accepted, message := service.requestReselect(ctx, daemon)
	return &cloudv1.ReselectMyDaemonEdgeResponse{Selection: selection, ReselectAccepted: accepted, Message: message}, nil
}

func (service *ManagementService) ownedDaemon(ctx context.Context, daemonID string) (Daemon, error) {
	identity, ok := account.IdentityFromContext(ctx)
	if !ok {
		return Daemon{}, account.ErrUnauthenticated
	}
	daemonID = strings.TrimSpace(daemonID)
	if daemonID == "" {
		return Daemon{}, ErrDaemonUnavailable
	}
	daemons, err := service.config.Store.ListDaemonsByAccount(ctx, identity.Account.GetAccountId())
	if err != nil {
		return Daemon{}, err
	}
	for _, daemon := range daemons {
		if daemon.ID == daemonID {
			return daemon, nil
		}
	}
	return Daemon{}, ErrDaemonUnavailable
}

func (service *ManagementService) edgeSelection(ctx context.Context, daemon Daemon) (*cloudv1.DaemonEdgeSelection, error) {
	currentEdgeID := ""
	if location, found, err := service.config.Directory.LocateDaemon(ctx, daemon.ID); err != nil {
		return nil, err
	} else if found {
		currentEdgeID = location.EdgeID
	}
	_, selection, err := service.config.Enrollment.selectEdge(ctx, daemon, currentEdgeID)
	return selection, err
}

func (service *ManagementService) requestReselect(ctx context.Context, daemon Daemon) (bool, string) {
	if daemon.State != cloudv1.DaemonState_DAEMON_STATE_ACTIVE {
		return false, "偏好已保存；daemon 当前未启用，将在恢复后生效"
	}
	location, found, err := service.config.Directory.LocateDaemon(ctx, daemon.ID)
	if err != nil || !found {
		return false, "偏好已保存；daemon 当前离线，将在下次连接时生效"
	}
	commandContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result := service.config.Control.ReselectDaemonEdge(commandContext, daemon.ID, location.Generation, daemon.EdgePreferenceRevision)
	if result == cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_APPLIED {
		return true, "重选命令已送达；daemon 正在测速并切换"
	}
	return false, "偏好已保存，但当前连接未接受重选命令；daemon 会在下次连接时生效"
}
