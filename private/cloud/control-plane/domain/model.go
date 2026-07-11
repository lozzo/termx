// Package domain 定义私有 Control Plane 的持久业务实体。
//
// 这些实体只描述账号、设备所有权、managed cloud session 和审计 metadata；
// terminal lifecycle、terminal scope 与 CapabilityGrant 均不属于该领域。
package domain

import "time"

// Account 表示计费与云服务 entitlement 的主体。
// Account 是 Control Plane 的持久真值，不代表任何 daemon terminal 权限。
type Account struct {
	ID             string
	OrganizationID string
	DisplayName    string
	CreatedAt      time.Time
}

// User 表示可以登录 Control Plane 的自然人身份。
// 用户与账号的关系仅用于云服务管理，不会被映射成 terminal scope。
type User struct {
	ID        string
	AccountID string
	Email     string
	CreatedAt time.Time
}

// Organization 表示团队治理和账单归属单元。
// Organization 不持有 daemon、terminal 或 capability 的运行时状态。
type Organization struct {
	ID          string
	DisplayName string
	CreatedAt   time.Time
}

// DeviceKind 区分注册到云目录的客户端与 daemon 设备。
// 该枚举只服务于云端发现和 admission，不推导设备拥有的 terminal 权限。
type DeviceKind string

const (
	// DeviceKindClient 表示发起 managed connection 的客户端设备。
	DeviceKindClient DeviceKind = "client"
	// DeviceKindDaemon 表示提供 endpoint 的 daemon 设备。
	DeviceKindDaemon DeviceKind = "daemon"
)

// DeviceRegistration 是 Control Plane 保存的最小设备目录记录。
// PublicKey 和 Fingerprint 用于设备目录与配对审核；daemon 私钥和原始 grant 永不进入该结构。
type DeviceRegistration struct {
	ID           string
	AccountID    string
	OwnerUserID  string
	Kind         DeviceKind
	Label        string
	PublicKey    []byte
	Fingerprint  string
	RegisteredAt time.Time
	RevokedAt    *time.Time
}

// HubAssignment 表示 Control Plane 为 managed session 选择的 Hub 和区域。
// 它只决定 signaling 服务目标，不承载 SDP、ICE、terminal 或 capability 数据。
type HubAssignment struct {
	HubID  string
	Region string
}

// ManagedSession 表示一次云托管连接意图的 metadata。
// 它不是 daemon terminal session；其 ID 只用于 Hub admission、Relay lease、usage 和审计关联。
type ManagedSession struct {
	ID             string
	AccountID      string
	ClientDeviceID string
	TargetDeviceID string
	Hub            HubAssignment
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

// PairingDecision 表示云端记录的配对审批结果。
// 审批只记录 metadata 和 grant reference hash，不保存原始 CapabilityGrant。
type PairingDecision string

const (
	// PairingDecisionApproved 表示用户已批准设备配对 metadata。
	PairingDecisionApproved PairingDecision = "approved"
	// PairingDecisionRejected 表示用户拒绝了设备配对请求。
	PairingDecisionRejected PairingDecision = "rejected"
)

// PairingApproval 保存配对审批的最小审计记录。
// GrantReferenceHash 只能是不可逆引用摘要，不能是 bearer grant 本体。
type PairingApproval struct {
	ID                 string
	AccountID          string
	ClientDeviceID     string
	TargetDeviceID     string
	ApproverUserID     string
	Decision           PairingDecision
	GrantReferenceHash string
	DecidedAt          time.Time
}

// AuditEvent 保存 Control Plane 管理操作的 metadata。
// ResourceID 应使用账号、设备、managed session 或 lease 引用，不允许记录 credential body 和 terminal 内容。
type AuditEvent struct {
	ID         string
	AccountID  string
	ActorID    string
	Action     string
	ResourceID string
	OccurredAt time.Time
}
