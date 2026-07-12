// Package httpapi 实现显式 dev-local Companion 到 Control Plane/Hub 的私有 HTTP contract。
//
// 该包只允许 loopback staging 地址，不是生产 TLS client。Payload 只包含 cloud protobuf、
// account/device cloud authorization 或 Hub admission；CapabilityGrant、DeviceIdentity private key、
// DataChannel 和 terminal payload 不属于任何 wire type。
package httpapi

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"google.golang.org/protobuf/proto"
)

const (
	// ManifestVersion 是 dev-local runtime manifest 的当前 schema 版本。
	// 读取方必须精确匹配该版本，未知版本不得按旧字段集合继续连接。
	ManifestVersion = 2
	// ProfileDevLocal 是允许 loopback 明文 HTTP 与固定开发账号的唯一 profile 名称。
	// production channel 必须拒绝该 profile，不能把它当作默认或 fallback 配置。
	ProfileDevLocal = "dev-local"
	// ProfileStagingSSH 只允许 development Companion 通过 SSH 转发访问 loopback Control Plane/Hub，
	// 同时使用公网 UDP TURN。它不放宽 HTTP listener，也不是 production profile。
	ProfileStagingSSH = "staging-ssh"
	// ProtobufMediaType 是 dev-local unary protobuf 与 CloudError response 的固定媒体类型。
	ProtobufMediaType = "application/x-protobuf"
	// JSONMediaType 是只承载 private session/admission envelope 的固定媒体类型。
	JSONMediaType = "application/json"
	// StreamMediaType 是 Hub length-prefixed protobuf response stream 的固定媒体类型。
	StreamMediaType = "application/x-termx-cloud-stream"

	maxBodyBytes = 4 << 20

	// ControlHealthPath 是 dev Control Plane readiness endpoint。
	ControlHealthPath = "/healthz"
	// ControlBeginLoginPath 是 dev account login flow 创建 endpoint。
	ControlBeginLoginPath = "/v1/login/begin"
	// ControlCompleteLoginPath 是 dev account flow 兑换 endpoint。
	ControlCompleteLoginPath = "/v1/login/complete"
	// ControlBeginEnrollmentPath 是 daemon enrollment challenge endpoint。
	ControlBeginEnrollmentPath = "/v1/enrollment/begin"
	// ControlCompleteEnrollmentPath 是 daemon enrollment proof endpoint。
	ControlCompleteEnrollmentPath = "/v1/enrollment/complete"
	// ControlBeginPresencePath 是 fresh daemon presence challenge endpoint。
	ControlBeginPresencePath = "/v1/presence/begin"
	// ControlResolveEndpointPath 是 account-scoped managed session resolve endpoint。
	ControlResolveEndpointPath = "/v1/endpoints/resolve"
	// ControlPresenceAdmissionPath 是 device-scoped presence ticket endpoint。
	ControlPresenceAdmissionPath = "/v1/admissions/presence"
	// ControlClientAdmissionPath 是 client managed signaling ticket endpoint。
	ControlClientAdmissionPath = "/v1/admissions/client"
	// ControlAnswerAdmissionPath 是 daemon managed answer ticket endpoint。
	ControlAnswerAdmissionPath = "/v1/admissions/answer"
	// ControlAcquireRelayLeasePath 是 account/device caller 获取同一 ManagedSession principal-specific TURN material 的 endpoint。
	ControlAcquireRelayLeasePath = "/v1/relay/leases/acquire"

	// HubHealthPath 是 dev Hub readiness endpoint。
	HubHealthPath = "/healthz"
	// HubOpenPresencePath 打开 admission-bound daemon presence stream。
	HubOpenPresencePath = "/v1/presence/open"
	// HubCreateSignalingPath 打开 admission-bound client answer stream。
	HubCreateSignalingPath = "/v1/signaling/create"
	// HubCompleteSignalingPath 返回 daemon answer 或稳定错误。
	HubCompleteSignalingPath = "/v1/signaling/complete"
)

// Manifest 是 `make cloud-dev` 写入 `.artifacts` 的非生产运行描述。
// 它只包含 loopback service 地址和公开 profile metadata，不包含 cloud session、Hub ticket 或 daemon secret。
type Manifest struct {
	Version          uint32 `json:"version"`
	Profile          string `json:"profile"`
	ControlPlaneURL  string `json:"control_plane_url"`
	HubURL           string `json:"hub_url"`
	RelayURL         string `json:"relay_url"`
	HubID            string `json:"hub_id"`
	Region           string `json:"region"`
	AccountLabel     string `json:"account_label"`
	EnrollmentCode   string `json:"enrollment_code"`
	StartedAtRFC3339 string `json:"started_at"`
}

// LoadManifest 严格读取 dev cloud manifest，并拒绝非 loopback、未知字段和尾随 JSON。
// production channel 不应调用本函数；Companion main 还必须显式检查 build channel。
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return Manifest{}, fmt.Errorf("read dev cloud manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode dev cloud manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Manifest{}, fmt.Errorf("dev cloud manifest has trailing data")
	}
	if manifest.Version != ManifestVersion || manifest.Profile != ProfileDevLocal && manifest.Profile != ProfileStagingSSH || manifest.HubID == "" || manifest.Region == "" || manifest.AccountLabel == "" || manifest.EnrollmentCode == "" {
		return Manifest{}, fmt.Errorf("invalid dev cloud manifest metadata")
	}
	if _, err := validateLoopbackURL(manifest.ControlPlaneURL); err != nil {
		return Manifest{}, fmt.Errorf("invalid dev Control Plane URL: %w", err)
	}
	if _, err := validateLoopbackURL(manifest.HubURL); err != nil {
		return Manifest{}, fmt.Errorf("invalid dev Hub URL: %w", err)
	}
	if err := validateTURNURL(manifest.RelayURL, manifest.Profile == ProfileStagingSSH); err != nil {
		return Manifest{}, fmt.Errorf("invalid dev Relay URL: %w", err)
	}
	if _, err := time.Parse(time.RFC3339, manifest.StartedAtRFC3339); err != nil {
		return Manifest{}, fmt.Errorf("invalid dev cloud start time")
	}
	return manifest, nil
}

func validateTURNURL(raw string, allowPublicIP bool) error {
	if raw == "" || raw != strings.TrimSpace(raw) || !strings.HasPrefix(strings.ToLower(raw), "turn:") {
		return fmt.Errorf("URL must be a canonical TURN UDP URL")
	}
	address, query, ok := strings.Cut(raw[len("turn:"):], "?")
	if !ok || query != "transport=udp" {
		return fmt.Errorf("URL must require UDP transport")
	}
	host, port, err := net.SplitHostPort(address)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("URL must include a Relay host and port")
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) && !(allowPublicIP && ip != nil && !ip.IsUnspecified()) {
		return fmt.Errorf("URL host must match the staging profile")
	}
	return nil
}

// SessionWire 是 Control Plane 返回给 Companion 的 private cloud session。
// AccessToken 只能传给 session.New 并写入 OS credential store，不得进入 public IPC response 或日志。
type SessionWire struct {
	Kind         session.Kind `json:"kind"`
	AccountID    string       `json:"account_id"`
	AccountLabel string       `json:"account_label"`
	DeviceID     string       `json:"device_id"`
	ExpiresAt    int64        `json:"expires_at_unix"`
	AccessToken  []byte       `json:"access_token"`
}

// AdmissionWire 是 Control Plane 返回给 Companion 的 private Hub admission envelope。
// Ticket 为短期 secret；其余字段用于 Hub request binding，不能替代 Hub 离线验签。
type AdmissionWire struct {
	Reference      string                      `json:"reference"`
	HubID          string                      `json:"hub_id"`
	AccountID      string                      `json:"account_id"`
	DeviceID       string                      `json:"device_id"`
	TargetDeviceID string                      `json:"target_device_id,omitempty"`
	SessionKind    cloudservice.HubSessionKind `json:"session_kind"`
	SessionID      string                      `json:"session_id"`
	ExpiresAt      int64                       `json:"expires_at_unix"`
	Ticket         []byte                      `json:"ticket"`
}

// AnswerAdmissionRequest 把 daemon 已消费 offer 的 ManagedSession 与 public completion DTO 绑定。
// SignalingSessionID 仍在 protobuf request 中；该 envelope 不允许出现 capability 或 terminal 字段。
type AnswerAdmissionRequest struct {
	ManagedSessionID string `json:"managed_session_id"`
	Payload          []byte `json:"payload"`
}

// HubRequest 把 private Hub admission 与一个 public cloud protobuf payload 绑定。
// Hub 不接收 account/device cloud authorization，唯一服务准入凭据是 Admission.Ticket。
type HubRequest struct {
	Admission AdmissionWire `json:"admission"`
	Payload   []byte        `json:"payload"`
}

func validateLoopbackURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return nil, fmt.Errorf("URL must be a plain loopback http origin")
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return parsed, nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("URL host must be loopback")
	}
	return parsed, nil
}

// WriteFrame 把一个 cloud protobuf 写成四字节大端长度前缀帧。
// 它是 dev-local Hub HTTP stream 的唯一 framing 真值；空帧和超过上限的帧会被拒绝。
func WriteFrame(writer io.Writer, message proto.Message) error {
	payload, err := proto.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal cloud stream frame: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxBodyBytes {
		return fmt.Errorf("cloud stream frame size is invalid")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}

// ReadFrame 从 dev-local Hub HTTP stream 读取一个长度前缀 protobuf 帧。
// target 必须是调用方预期的具体消息类型，非法长度或 protobuf 会稳定失败而不尝试其他格式。
func ReadFrame(reader io.Reader, target proto.Message) error {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > maxBodyBytes {
		return fmt.Errorf("cloud stream frame size is invalid")
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return err
	}
	if err := proto.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode cloud stream frame: %w", err)
	}
	if len(target.ProtoReflect().GetUnknown()) != 0 {
		return fmt.Errorf("cloud stream frame contains unknown fields")
	}
	return nil
}
