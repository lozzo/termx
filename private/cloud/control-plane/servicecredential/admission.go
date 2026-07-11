package servicecredential

import (
	"fmt"
	"time"
)

const (
	admissionVersion = 1
	maxAdmissionTTL  = 5 * time.Minute
)

// PrincipalKind 表示使用 Hub signaling 服务的设备角色。
// daemon 与 client 使用不同 operation 集，不能通过同一 ticket 互换身份。
type PrincipalKind string

const (
	// PrincipalDaemon 表示注册自身短期 presence 并回答连接的 daemon。
	PrincipalDaemon PrincipalKind = "daemon"
	// PrincipalClient 表示向固定 target device 发起连接的客户端。
	PrincipalClient PrincipalKind = "client"
)

// HubSessionKind 区分 device-scoped presence 与 client-scoped managed signaling 身份。
// 两类 session 的 ID、生命周期和 admission operation 不得互换。
type HubSessionKind string

const (
	// HubSessionPresence 表示 daemon 在 Hub 注册的短期 device presence。
	HubSessionPresence HubSessionKind = "presence"
	// HubSessionManaged 表示 client 到 target daemon 的一次 managed signaling session。
	HubSessionManaged HubSessionKind = "managed"
)

// HubOperation 是 Hub admission 允许的最小 signaling 操作。
// 该枚举不包含 terminal、protocol 或 capability 操作。
type HubOperation string

const (
	// HubOperationPresence 允许 daemon 注册短 TTL presence。
	HubOperationPresence HubOperation = "presence"
	// HubOperationOffer 允许 client 向 ticket 绑定的 target 发送 offer。
	HubOperationOffer HubOperation = "offer"
	// HubOperationAnswer 允许 daemon 为绑定 session 返回 answer。
	HubOperationAnswer HubOperation = "answer"
	// HubOperationCandidate 允许双方交换绑定 session 的 ICE candidate。
	HubOperationCandidate HubOperation = "candidate"
)

// HubAdmissionClaims 是 Control Plane 签名的 Hub 服务准入声明。
// Claims 只绑定账号、设备、Hub、显式 session kind/id 和 signaling operation，不包含 terminal ID、scope 或 grant。
type HubAdmissionClaims struct {
	Version           uint32         `json:"version"`
	KeyID             string         `json:"key_id"`
	TicketID          string         `json:"ticket_id"`
	Issuer            string         `json:"issuer"`
	AudienceHubID     string         `json:"audience_hub_id"`
	PrincipalKind     PrincipalKind  `json:"principal_kind"`
	AccountID         string         `json:"account_id"`
	DeviceID          string         `json:"device_id"`
	SessionKind       HubSessionKind `json:"session_kind"`
	SessionID         string         `json:"session_id"`
	TargetDeviceID    string         `json:"target_device_id,omitempty"`
	AllowedOperations []HubOperation `json:"allowed_operations"`
	IssuedAtUnix      int64          `json:"issued_at_unix"`
	ExpiresAtUnix     int64          `json:"expires_at_unix"`
}

// HubAdmissionRequest 是签发短期 Hub ticket 的输入。
// TTL 由调用方请求但受五分钟硬上限约束；生产 adapter 应使用不可预测且全局唯一的 TicketID。
type HubAdmissionRequest struct {
	TicketID          string
	AudienceHubID     string
	PrincipalKind     PrincipalKind
	AccountID         string
	DeviceID          string
	SessionKind       HubSessionKind
	SessionID         string
	TargetDeviceID    string
	AllowedOperations []HubOperation
	TTL               time.Duration
}

// HubAdmissionTicket 包装不可记录的签名 ticket bytes。
// String 始终脱敏；跨服务传输必须显式调用 Bytes。
type HubAdmissionTicket struct {
	raw string
}

// Bytes 返回传给指定 Hub 的签名 ticket 副本。
// 该值属于短期 secret，调用方不得写入日志、URL 或长期数据库。
func (ticket HubAdmissionTicket) Bytes() []byte {
	return []byte(ticket.raw)
}

// String 返回脱敏文本，防止 fmt 或结构化日志泄漏 ticket body。
func (ticket HubAdmissionTicket) String() string {
	return "HubAdmissionTicket{[REDACTED]}"
}

// HubAdmissionIssuer 使用 Control Plane active key 签发 Hub admission。
// Issuer 只负责云服务准入，不查询或解释 terminal capability。
type HubAdmissionIssuer struct {
	issuer string
	signer Signer
}

// NewHubAdmissionIssuer 创建 Hub admission issuer。
// issuer 是稳定的 Control Plane 服务身份，不是账号或设备展示名。
func NewHubAdmissionIssuer(issuer string, signer Signer) (HubAdmissionIssuer, error) {
	if issuer == "" || signer.KeyID() == "" {
		return HubAdmissionIssuer{}, fmt.Errorf("invalid Hub admission issuer")
	}
	return HubAdmissionIssuer{issuer: issuer, signer: signer}, nil
}

// Issue 验证 principal operation matrix 后签发最多五分钟有效的 ticket。
// client 必须绑定 target device 且不能获得 presence；daemon 不能获得 offer。
func (issuer HubAdmissionIssuer) Issue(request HubAdmissionRequest, now time.Time) (HubAdmissionTicket, error) {
	operations, err := canonicalHubOperations(request.PrincipalKind, request.AllowedOperations)
	if err != nil {
		return HubAdmissionTicket{}, err
	}
	if request.TicketID == "" || request.AudienceHubID == "" || request.AccountID == "" || request.DeviceID == "" || request.SessionID == "" {
		return HubAdmissionTicket{}, ErrMalformedCredential
	}
	if err := validateHubSessionBinding(request.SessionKind, request.PrincipalKind, operations); err != nil {
		return HubAdmissionTicket{}, err
	}
	if request.PrincipalKind == PrincipalClient && request.TargetDeviceID == "" {
		return HubAdmissionTicket{}, ErrMalformedCredential
	}
	if request.PrincipalKind == PrincipalDaemon && request.TargetDeviceID != "" {
		return HubAdmissionTicket{}, ErrMalformedCredential
	}
	if request.TTL <= 0 || request.TTL > maxAdmissionTTL {
		return HubAdmissionTicket{}, ErrCredentialExpired
	}
	now = now.UTC().Truncate(time.Second)
	claims := HubAdmissionClaims{
		Version:           admissionVersion,
		KeyID:             issuer.signer.KeyID(),
		TicketID:          request.TicketID,
		Issuer:            issuer.issuer,
		AudienceHubID:     request.AudienceHubID,
		PrincipalKind:     request.PrincipalKind,
		AccountID:         request.AccountID,
		DeviceID:          request.DeviceID,
		SessionKind:       request.SessionKind,
		SessionID:         request.SessionID,
		TargetDeviceID:    request.TargetDeviceID,
		AllowedOperations: operations,
		IssuedAtUnix:      now.Unix(),
		ExpiresAtUnix:     now.Add(request.TTL).Unix(),
	}
	raw, err := signToken(hubAdmissionPrefix, claims, issuer.signer, now)
	if err != nil {
		return HubAdmissionTicket{}, err
	}
	return HubAdmissionTicket{raw: raw}, nil
}

// HubAdmissionExpectation 是 Hub 在接纳单个 signaling 操作前必须匹配的上下文。
// Hub 必须从认证连接和路由状态构造这些值，不能信任 ticket 外的 caller 声明覆盖它们。
type HubAdmissionExpectation struct {
	Issuer         string
	AudienceHubID  string
	PrincipalKind  PrincipalKind
	AccountID      string
	DeviceID       string
	SessionKind    HubSessionKind
	SessionID      string
	TargetDeviceID string
	Operation      HubOperation
}

// VerifyHubAdmission 离线验签并验证 Hub、principal、session、target 和 operation 绑定。
// 成功只表示可以使用该 Hub 操作，不表示 daemon 已授权任何 terminal request。
func VerifyHubAdmission(ring *KeyRing, encoded []byte, expected HubAdmissionExpectation, now time.Time) (HubAdmissionClaims, error) {
	var claims HubAdmissionClaims
	if err := verifyToken(hubAdmissionPrefix, string(encoded), tokenKeyID, &claims, ring, now); err != nil {
		return HubAdmissionClaims{}, err
	}
	if err := validateHubAdmissionClaims(claims, expected, now); err != nil {
		return HubAdmissionClaims{}, err
	}
	return claims, nil
}

func validateHubAdmissionClaims(claims HubAdmissionClaims, expected HubAdmissionExpectation, now time.Time) error {
	if claims.Version != admissionVersion || claims.KeyID == "" || claims.TicketID == "" || claims.Issuer == "" || claims.AudienceHubID == "" || claims.AccountID == "" || claims.DeviceID == "" || claims.SessionID == "" {
		return ErrMalformedCredential
	}
	issuedAt := time.Unix(claims.IssuedAtUnix, 0).UTC()
	expiresAt := time.Unix(claims.ExpiresAtUnix, 0).UTC()
	if !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > maxAdmissionTTL || now.Before(issuedAt) || !now.Before(expiresAt) {
		return ErrCredentialExpired
	}
	canonical, err := canonicalHubOperations(claims.PrincipalKind, claims.AllowedOperations)
	if err != nil || len(canonical) != len(claims.AllowedOperations) {
		return ErrMalformedCredential
	}
	for index := range canonical {
		if canonical[index] != claims.AllowedOperations[index] {
			return ErrMalformedCredential
		}
	}
	if claims.PrincipalKind == PrincipalClient && claims.TargetDeviceID == "" || claims.PrincipalKind == PrincipalDaemon && claims.TargetDeviceID != "" {
		return ErrMalformedCredential
	}
	if err := validateHubSessionBinding(claims.SessionKind, claims.PrincipalKind, claims.AllowedOperations); err != nil {
		return err
	}
	if claims.Issuer != expected.Issuer || claims.AudienceHubID != expected.AudienceHubID || claims.PrincipalKind != expected.PrincipalKind || claims.AccountID != expected.AccountID || claims.DeviceID != expected.DeviceID || claims.SessionKind != expected.SessionKind || claims.SessionID != expected.SessionID || claims.TargetDeviceID != expected.TargetDeviceID || !containsString(claims.AllowedOperations, expected.Operation) {
		return ErrCredentialBinding
	}
	return nil
}

func validateHubSessionBinding(kind HubSessionKind, principal PrincipalKind, operations []HubOperation) error {
	switch kind {
	case HubSessionPresence:
		if principal != PrincipalDaemon || len(operations) != 1 || operations[0] != HubOperationPresence {
			return ErrCredentialBinding
		}
	case HubSessionManaged:
		if containsString(operations, HubOperationPresence) {
			return ErrCredentialBinding
		}
	default:
		return ErrMalformedCredential
	}
	return nil
}

func canonicalHubOperations(principal PrincipalKind, requested []HubOperation) ([]HubOperation, error) {
	if len(requested) == 0 {
		return nil, ErrMalformedCredential
	}
	allowed := []HubOperation{HubOperationPresence, HubOperationOffer, HubOperationAnswer, HubOperationCandidate}
	result := make([]HubOperation, 0, len(requested))
	for _, operation := range allowed {
		if !containsString(requested, operation) {
			continue
		}
		if principal == PrincipalClient && (operation == HubOperationPresence || operation == HubOperationAnswer) || principal == PrincipalDaemon && operation == HubOperationOffer {
			return nil, ErrCredentialBinding
		}
		result = append(result, operation)
	}
	if len(result) != len(requested) || principal != PrincipalClient && principal != PrincipalDaemon {
		return nil, ErrMalformedCredential
	}
	return result, nil
}
