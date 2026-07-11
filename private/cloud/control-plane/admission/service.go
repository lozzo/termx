// Package admission 从 managed session 真值签发短期 Hub signaling ticket。
package admission

import (
	"fmt"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
)

// SessionSource 是 admission service 读取 managed session 和 HubAssignment 的最小接口。
// 实现必须按 AccountID 校验 ownership；ticket 字段不能由未认证 caller 绕过该真值直接填写。
type SessionSource interface {
	ManagedSession(accountID, sessionID string, now time.Time) (domain.ManagedSession, error)
}

// Command 是为 managed session 一方签发 Hub ticket 的服务命令。
// PrincipalKind 决定 device 和 operation matrix，AudienceHubID 始终从 session assignment 推导。
type Command struct {
	TicketID         string
	AccountID        string
	ManagedSessionID string
	PrincipalKind    servicecredential.PrincipalKind
	Operations       []servicecredential.HubOperation
	TTL              time.Duration
}

// Service 是 managed session ownership 到 Hub admission 的事务边界。
// 成功只允许固定 Hub 上的 signaling operation，不授予 terminal 或 protocol capability。
type Service struct {
	sessions SessionSource
	issuer   servicecredential.HubAdmissionIssuer
}

// NewService 创建 Hub admission service。
// session source 缺失时直接失败，不允许 fallback 成 caller 自填 ticket claims。
func NewService(sessions SessionSource, issuer servicecredential.HubAdmissionIssuer) (*Service, error) {
	if sessions == nil {
		return nil, fmt.Errorf("Hub admission session source is required")
	}
	return &Service{sessions: sessions, issuer: issuer}, nil
}

// Issue 从 managed session 推导 Hub、device 和 target binding 后签发短期 ticket。
// client 绑定 client device 与 target daemon；daemon 绑定 target daemon 且不能注册其他 device presence。
func (service *Service) Issue(command Command, now time.Time) (servicecredential.HubAdmissionTicket, error) {
	session, err := service.sessions.ManagedSession(command.AccountID, command.ManagedSessionID, now)
	if err != nil {
		return servicecredential.HubAdmissionTicket{}, fmt.Errorf("load managed session: %w", err)
	}
	if session.ID != command.ManagedSessionID || session.AccountID != command.AccountID {
		return servicecredential.HubAdmissionTicket{}, servicecredential.ErrCredentialBinding
	}
	request := servicecredential.HubAdmissionRequest{
		TicketID:          command.TicketID,
		AudienceHubID:     session.Hub.HubID,
		PrincipalKind:     command.PrincipalKind,
		AccountID:         command.AccountID,
		SessionKind:       servicecredential.HubSessionManaged,
		SessionID:         command.ManagedSessionID,
		AllowedOperations: append([]servicecredential.HubOperation(nil), command.Operations...),
		TTL:               command.TTL,
	}
	switch command.PrincipalKind {
	case servicecredential.PrincipalClient:
		request.DeviceID = session.ClientDeviceID
		request.TargetDeviceID = session.TargetDeviceID
	case servicecredential.PrincipalDaemon:
		request.DeviceID = session.TargetDeviceID
	default:
		return servicecredential.HubAdmissionTicket{}, servicecredential.ErrCredentialBinding
	}
	return service.issuer.Issue(request, now)
}
