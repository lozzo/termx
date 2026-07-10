// Package companion 实现官方 desktop/headless Cloud Companion 的公开 domain contract。
//
// 本包只编排 OS credential session、Control Plane/Hub TLS adapters、presence/signaling、
// Relay lease 和质量摘要。WebRTC、DTLS、DeviceIdentity 私钥、CapabilityGrant、DataChannel
// 与 terminal protocol 始终属于公开 termx 进程。
package companion

import (
	"crypto/rand"
	"fmt"
	"io"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/companion/cloudservice"
	"github.com/lozzow/termx/private/termx-cloud/companion/session"
	"github.com/lozzow/termx/termx-proto/cloudpb"
)

// Config 固定 companion build identity、可协商能力和有界 stream 容量。
// Now 与 NonceReader 只用于确定性 harness；生产应使用默认 UTC clock 和 crypto/rand.Reader。
type Config struct {
	CompanionVersion string
	BuildChannel     string
	Capabilities     []cloudpb.CompanionCapability
	StreamCapacity   int
	Now              func() time.Time
	NonceReader      io.Reader
}

// Service 是 companion 进程内共享的依赖容器。
// 每个本地 IPC peer 必须通过 NewConnection 获得独立 Hello、caller role 和 stream ownership 状态。
type Service struct {
	version        string
	buildChannel   string
	capabilities   map[cloudpb.CompanionCapability]struct{}
	streamCapacity int
	now            func() time.Time
	nonceReader    io.Reader
	sessions       *session.Manager
	controlPlane   cloudservice.ControlPlaneAdapter
	hub            cloudservice.HubAdapter
}

// NewService 创建 desktop/headless companion service。
// OS credential manager、Control Plane 和 Hub adapter 均为必需依赖，不允许无云 fake 或旧 Hub fallback 混入生产 service。
func NewService(config Config, sessions *session.Manager, controlPlane cloudservice.ControlPlaneAdapter, hub cloudservice.HubAdapter) (*Service, error) {
	if sessions == nil || controlPlane == nil || hub == nil || config.CompanionVersion == "" || config.BuildChannel == "" || config.StreamCapacity < 1 {
		return nil, fmt.Errorf("invalid Cloud Companion service configuration")
	}
	capabilities := make(map[cloudpb.CompanionCapability]struct{}, len(config.Capabilities))
	for _, capability := range config.Capabilities {
		if !knownCapability(capability) {
			return nil, fmt.Errorf("unknown companion capability %s", capability)
		}
		if _, exists := capabilities[capability]; exists {
			return nil, fmt.Errorf("duplicate companion capability %s", capability)
		}
		capabilities[capability] = struct{}{}
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.NonceReader == nil {
		config.NonceReader = rand.Reader
	}
	return &Service{
		version:        config.CompanionVersion,
		buildChannel:   config.BuildChannel,
		capabilities:   capabilities,
		streamCapacity: config.StreamCapacity,
		now:            config.Now,
		nonceReader:    config.NonceReader,
		sessions:       sessions,
		controlPlane:   controlPlane,
		hub:            hub,
	}, nil
}

// NewConnection 创建一个 caller-scoped public contract implementation。
// 新连接在成功 Hello 前只能调用 Hello；关闭该连接只取消它拥有的 streams。
func (service *Service) NewConnection() *Connection {
	return &Connection{service: service, capabilities: make(map[cloudpb.CompanionCapability]struct{}), streams: make(map[uint64]ownedStream), offers: make(map[string]struct{})}
}

func knownCapability(capability cloudpb.CompanionCapability) bool {
	switch capability {
	case cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_PATH_QUALITY,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SMART_ROUTE:
		return true
	default:
		return false
	}
}

func capabilityAllowedForRole(capability cloudpb.CompanionCapability, role cloudpb.CallerRole) bool {
	switch role {
	case cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP:
		return capability != cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE
	case cloudpb.CallerRole_CALLER_ROLE_CLI:
		return capability == cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION
	case cloudpb.CallerRole_CALLER_ROLE_DAEMON:
		return capability == cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE ||
			capability == cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING ||
			capability == cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE ||
			capability == cloudpb.CompanionCapability_COMPANION_CAPABILITY_PATH_QUALITY
	default:
		return false
	}
}
