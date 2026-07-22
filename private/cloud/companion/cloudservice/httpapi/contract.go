// Package httpapi 实现显式 development Companion 到 Control Plane/Hub 的私有 HTTP contract。
//
// 该包默认只允许 loopback staging 地址；公网 HTTP/HTTPS 必须由 manifest profile 显式授权。
// 它仍是 development 装配，不是 production release channel。Payload 只包含 cloud protobuf、
// account/device cloud authorization；CapabilityGrant、DeviceIdentity private key、
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

	"github.com/muxvia/muxvia/private/cloud/companion/session"
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
	// ProfileStagingPublicHTTP 是用户明确授权的无隧道公网明文 development staging。
	// 它只能承载固定测试账号和短期内存 session；stable/production build 仍必须拒绝该 manifest。
	ProfileStagingPublicHTTP = "staging-public-http"
	// ProfileStagingPublicHTTPS 是用户明确选择的公网 TLS development staging。
	// 它允许 Companion 连接受系统信任链校验的 HTTPS Controller/Hub，但不改变 build channel 门禁。
	ProfileStagingPublicHTTPS = "staging-public-https"
	// ProtobufMediaType 是 dev-local unary protobuf 与 CloudError response 的固定媒体类型。
	ProtobufMediaType = "application/x-protobuf"
	// JSONMediaType 是只承载 private session/edge envelope 的固定媒体类型。
	JSONMediaType = "application/json"
	// StreamMediaType 是 Hub length-prefixed protobuf response stream 的固定媒体类型。
	StreamMediaType = "application/x-muxvia-cloud-stream"

	maxBodyBytes = 4 << 20

	// ControlHealthPath 是 dev Control Plane readiness endpoint。
	ControlHealthPath = "/healthz"
	// ControlBeginLoginPath 是 dev account login flow 创建 endpoint。
	ControlBeginLoginPath = "/v1/login/begin"
	// ControlCompleteLoginPath 是 dev account flow 兑换 endpoint。
	ControlCompleteLoginPath = "/v1/login/complete"
	// ControlClaimMobileActivationPath 让 Official App 认领 Web 创建的短期设备激活 Flow。
	ControlClaimMobileActivationPath = "/v1/login/mobile/claim"
	// ControlBeginEnrollmentPath 是 daemon enrollment challenge endpoint。
	ControlBeginEnrollmentPath = "/v1/enrollment/begin"
	// ControlCompleteEnrollmentPath 是 daemon enrollment proof endpoint。
	ControlCompleteEnrollmentPath = "/v1/enrollment/complete"
	// ControlRefreshSessionPath 使用单次 refresh secret 轮换 account/device edge session。
	ControlRefreshSessionPath = "/v1/sessions/refresh"
	// HubHealthPath 是 dev Hub readiness endpoint。
	HubHealthPath = "/healthz"
	// HubBeginPresencePath 使用 daemon edge credential 创建 fresh DeviceProof challenge。
	HubBeginPresencePath = "/v1/presence/begin"
	// HubOpenPresencePath 使用 daemon edge credential 和 fresh proof 打开 presence stream。
	HubOpenPresencePath = "/v1/presence/open"
	// HubCreateSignalingPath 打开 edge-authorization-bound client answer stream。
	HubCreateSignalingPath = "/v1/signaling/create"
	// HubCompleteSignalingPath 返回 daemon answer 或稳定错误。
	HubCompleteSignalingPath = "/v1/signaling/complete"
	// HubReportDaemonRuntimePath 接收当前 Presence 的完整 managed runtime replacement。
	HubReportDaemonRuntimePath = "/v1/daemon/runtime"
	// HubReportDaemonCommandResultPath 接收 daemon 对精确 deny-only command 的独立执行 receipt。
	HubReportDaemonCommandResultPath = "/v1/daemon/command-result"
	// HubAcquireRelayLeasePath 使用区域委派预算签发 caller-specific TURN material。
	HubAcquireRelayLeasePath = "/v1/relay/leases/acquire"
	// HubResolveEndpointPath 使用本地 policy/presence 解析 managed target。
	HubResolveEndpointPath = "/v1/endpoints/resolve"
	// HubListManagedDevicesPath 从签名内存投影列出当前账号设备，不查询 Control Plane。
	HubListManagedDevicesPath = "/v1/devices/list"
)

// Manifest 是 development Cloud 的非生产运行描述。
// 它可以来自 `make cloud-dev` 的 runtime 文件，也可以在显式测试构建时固化进 Companion；
// 只允许包含 service 地址和公开 profile metadata，不得包含 cloud session、refresh secret 或 daemon secret。
type Manifest struct {
	Version          uint32 `json:"version"`
	Profile          string `json:"profile"`
	ControlPlaneURL  string `json:"control_plane_url"`
	HubURL           string `json:"hub_url"`
	RelayURL         string `json:"relay_url"`
	HubID            string `json:"hub_id"`
	Region           string `json:"region"`
	AccountLabel     string `json:"account_label"`
	StartedAtRFC3339 string `json:"started_at"`
}

// LoadManifest 严格读取 dev cloud manifest，并拒绝非 loopback、未知字段和尾随 JSON。
// production channel 不应调用本函数；Companion main 还必须显式检查 build channel。
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return Manifest{}, fmt.Errorf("read dev cloud manifest: %w", err)
	}
	return ParseManifest(data)
}

// ParseManifest 严格解析 development Cloud manifest bytes，并验证 profile 对应的网络边界。
// 调用方负责限定数据来源；未知字段、尾随 JSON、非法公网地址或生产 profile 都会失败，且不得 fallback。
func ParseManifest(data []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode dev cloud manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Manifest{}, fmt.Errorf("dev cloud manifest has trailing data")
	}
	if manifest.Version != ManifestVersion || manifest.Profile != ProfileDevLocal && manifest.Profile != ProfileStagingSSH && manifest.Profile != ProfileStagingPublicHTTP && manifest.Profile != ProfileStagingPublicHTTPS || manifest.HubID == "" || manifest.Region == "" || manifest.AccountLabel == "" {
		return Manifest{}, fmt.Errorf("invalid dev cloud manifest metadata")
	}
	allowPublicHTTP := manifest.Profile == ProfileStagingPublicHTTP
	allowPublicHTTPS := manifest.Profile == ProfileStagingPublicHTTPS
	if _, err := validateServiceURL(manifest.ControlPlaneURL, allowPublicHTTP, allowPublicHTTPS); err != nil {
		return Manifest{}, fmt.Errorf("invalid dev Control Plane URL: %w", err)
	}
	if _, err := validateServiceURL(manifest.HubURL, allowPublicHTTP, allowPublicHTTPS); err != nil {
		return Manifest{}, fmt.Errorf("invalid dev Hub URL: %w", err)
	}
	if err := validateTURNURL(manifest.RelayURL, manifest.Profile == ProfileStagingSSH || allowPublicHTTP || allowPublicHTTPS); err != nil {
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
	Kind                session.Kind `json:"kind"`
	AccountID           string       `json:"account_id"`
	AccountLabel        string       `json:"account_label"`
	DeviceID            string       `json:"device_id"`
	ExpiresAt           int64        `json:"expires_at_unix"`
	AccessToken         []byte       `json:"access_token"`
	RefreshToken        []byte       `json:"refresh_token"`
	RefreshExpiresAt    int64        `json:"refresh_expires_at_unix"`
	HubID               string       `json:"hub_id"`
	HubURL              string       `json:"hub_url"`
	HubRegion           string       `json:"hub_region"`
	HubDirectoryVersion uint64       `json:"hub_directory_version"`
}

// RefreshSessionWire 是只发往 Control Plane 的 refresh 请求。
// RefreshToken 不得进入 Hub、WebView、日志或配置；服务端只持有其 SHA-256。
type RefreshSessionWire struct {
	Kind         session.Kind `json:"kind"`
	RefreshToken []byte       `json:"refresh_token"`
}

// EdgeHubRequest 只包装 managed signaling protobuf payload。
// client/daemon edge credential 必须位于 Authorization header；请求体不复制 credential、grant 或 terminal 数据。
type EdgeHubRequest struct {
	AccountID string `json:"account_id"`
	DeviceID  string `json:"device_id"`
	Payload   []byte `json:"payload"`
}

func validateServiceURL(raw string, allowPublicHTTP, allowPublicHTTPS bool) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || raw != trimmed || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return nil, fmt.Errorf("URL must be a canonical service origin")
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("URL host is required")
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || ip != nil && ip.IsLoopback()
	if port := parsed.Port(); port != "" {
		portNumber, portErr := strconv.Atoi(port)
		if portErr != nil || portNumber < 1 || portNumber > 65535 {
			return nil, fmt.Errorf("URL port is invalid")
		}
	}
	if parsed.Scheme == "http" && (loopback || allowPublicHTTP) || parsed.Scheme == "https" && allowPublicHTTPS {
		if ip == nil || !ip.IsUnspecified() && !ip.IsMulticast() {
			return parsed, nil
		}
	}
	if ip != nil && (ip.IsUnspecified() || ip.IsMulticast()) {
		return nil, fmt.Errorf("URL host is not allowed by the staging profile")
	}
	return nil, fmt.Errorf("URL scheme or host is not allowed by the staging profile")
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
