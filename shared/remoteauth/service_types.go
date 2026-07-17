package remoteauth

import "time"

// ClientAccessIdentityResult 是 local owner 查询 daemon DeviceIdentity 的公开投影。
// 它不包含 daemon private key、Cloud enrollment 或任何 route 配置。
type ClientAccessIdentityResult struct {
	DeviceID          string
	DeviceFingerprint string
	DevicePublicKey   []byte
}

// ClientAccessTicketCreateParams 是 local owner 或 ManageClientAccess session 创建一次性 ticket 的领域输入。
// AccessStore 仍负责校验 scope 与有效期，protocol adapter 不能扩大权限。
type ClientAccessTicketCreateParams struct {
	Label                string
	Scope                Scope
	TicketTTLSeconds     int64
	GrantLifetimeSeconds int64
}

// ClientAccessTicketCreateResult 是成功登记 PairingTicket 后返回的公开结果。
// Bundle 包含短期 ticket，但不包含长期 grant 或任何 private key。
type ClientAccessTicketCreateResult struct {
	Bundle    []byte
	TicketID  string
	ExpiresAt time.Time
}

// ClientAccessRevokeParams 指定 daemon AccessStore 要撤销的 GrantID。
// 删除客户端本地 credential ref 不能替代该操作。
type ClientAccessRevokeParams struct {
	GrantID string
}

// ClientAccessListResult 是 daemon-local client access truth 的稳定有序脱敏投影。
type ClientAccessListResult struct {
	Records []ClientAccessRecord
}
