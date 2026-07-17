package runtime

import (
	"context"
	"time"

	"github.com/lozzow/termx/client/endpoint"
)

// EndpointProbeRequest 描述 CLI/App 对单 endpoint 的显式可达性验证。
// RouteOverride 为空时使用 runtime planner；非空时只能探测指定 route，不得 fallback。
type EndpointProbeRequest struct {
	EndpointID    endpoint.EndpointID
	RouteOverride endpoint.RouteID
}

// EndpointProbeResult 返回一次 probe 实际形成的 session stamp 和公开诊断。
// 它不暴露 transport、protocol client、credential 或私有 route score。
type EndpointProbeResult struct {
	Stamp                EndpointSessionStamp
	ObservedPath         string
	RouteSelectionReason string
}

// EndpointApplication 是 CLI/App 使用的 endpoint 诊断 application interface。
// ProbeEndpoint 由共享 runtime 执行完整 identity/authorization/Hello，再立即释放 probe session lease。
type EndpointApplication interface {
	ProbeEndpoint(context.Context, EndpointProbeRequest) (EndpointProbeResult, error)
}

// AccessIdentityRequest 指定要查询 DeviceIdentity 的 owning endpoint。
type AccessIdentityRequest struct {
	EndpointID endpoint.EndpointID
}

// AccessIdentityResult 是 owning daemon DeviceIdentity 的公开部分。
// PublicKey 是副本；private key 永远不能进入 client runtime application event 或 CLI result。
type AccessIdentityResult struct {
	DeviceID          string
	DeviceFingerprint string
	PublicKey         []byte
}

// AccessScope 是 daemon-local client grant 的脱敏能力投影。
// 它只用于展示和审计，不参与本地授权判断；最终验证仍由 owning daemon AccessStore 完成。
type AccessScope struct {
	AllowDaemon        bool
	TerminalID         string
	MachineEventsOnly  bool
	FileReadMetadata   bool
	FileReadContent    bool
	FileWriteContent   bool
	FileMutate         bool
	ManageClientAccess bool
}

// AccessRecord 是 owning daemon AccessStore 返回的脱敏授权记录。
// 记录不包含 CapabilityGrant body、subject public key、ticket secret 或 credential material。
type AccessRecord struct {
	GrantID               string
	RevocationID          string
	SubjectKeyFingerprint string
	ClientLabel           string
	Scope                 AccessScope
	IssuedAt              time.Time
	ExpiresAt             time.Time
	RevokedAt             time.Time
}

// AccessListRequest 指定要列出授权记录的 owning endpoint。
type AccessListRequest struct {
	EndpointID endpoint.EndpointID
}

// AccessListResult 返回稳定有序的 daemon-local access record 副本。
type AccessListResult struct {
	Records []AccessRecord
}

// AccessRevokeRequest 按 owning endpoint 和 GrantID 执行显式撤销。
type AccessRevokeRequest struct {
	EndpointID endpoint.EndpointID
	GrantID    string
}

// AccessApplication 是 CLI/App 管理 daemon-local client access 的 application interface。
// 所有方法都必须通过已认证 endpoint session 到达 owning daemon，不能直接读取客户端 credential store。
type AccessApplication interface {
	AccessIdentity(context.Context, AccessIdentityRequest) (AccessIdentityResult, error)
	ListAccess(context.Context, AccessListRequest) (AccessListResult, error)
	RevokeAccess(context.Context, AccessRevokeRequest) (AccessRecord, error)
}
