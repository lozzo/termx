// Package activation 负责从已验证的 active installation 启动并协商本机 Cloud Companion。
//
// 该包不下载 artifact、不选择任意 executable，也不实现 cloud 业务；它只接受 installer
// 复验后的固定 binary path，使用固定 serve 参数，并在 Hello 成功后返回 public IPC client。
package activation

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"github.com/muxvia/muxvia/shared/cloudcompanion/installer"
	"github.com/muxvia/muxvia/shared/cloudcompanion/ipc"
)

// InstallationSource 只返回 installer 已复验的 active binary。
// 实现必须重新检查 active record、owner/mode 和 hash，不能只读取未验证配置路径。
type InstallationSource interface {
	// Status 返回当前可信 active installation，缺失时返回 COMPANION_MISSING。
	Status() (installer.Installation, error)
}

// DialFunc 建立平台本地 IPC；生产使用 ipc.Dial，注入点只用于 deterministic harness。
type DialFunc func(context.Context, string) (*ipc.Client, error)

// StartFunc 启动 installer 已验证的 binary 与固定 endpoint。
// smoke 为 true 时进程只能服务兼容性握手；实现不得添加 shell、hook 或 caller-provided argument。
type StartFunc func(binaryPath, endpoint string, smoke bool) error

// Config 固定 activation 的 active source、IPC endpoint、公开 muxvia 版本和受限重启策略。
// RetryWindow 内最多启动一次；并发 endpoint open 共享同一个 Manager 锁和启动时间。
type Config struct {
	Installations InstallationSource
	Endpoint      string
	MuxviaVersion string
	Dial          DialFunc
	Start         StartFunc
	Now           func() time.Time
	ReadyTimeout  time.Duration
	RetryInterval time.Duration
	RetryWindow   time.Duration
}

// Manager 是 TUI、CLI 与 daemon 共用的固定 Companion 激活器。
// Companion 缺失、不可信、崩溃或不兼容只返回 managed cloud 错误，不触发 local/SSH 或旧 remote fallback。
type Manager struct {
	installations InstallationSource
	endpoint      string
	muxviaVersion string
	dial          DialFunc
	start         StartFunc
	now           func() time.Time
	readyTimeout  time.Duration
	retryInterval time.Duration
	retryWindow   time.Duration

	mu        sync.Mutex
	lastStart time.Time
}

// New 创建固定路径 Companion activation manager。
// Installations、MuxviaVersion 必须存在；空 endpoint 使用平台默认 user-scoped socket/Named Pipe。
func New(config Config) (*Manager, error) {
	if config.Installations == nil || config.MuxviaVersion == "" {
		return nil, fmt.Errorf("Cloud Companion installation source and muxvia version are required")
	}
	if config.Endpoint == "" {
		config.Endpoint = ipc.DefaultEndpoint()
	}
	if config.Dial == nil {
		config.Dial = ipc.Dial
	}
	if config.Start == nil {
		config.Start = startVerifiedProcess
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.ReadyTimeout <= 0 {
		config.ReadyTimeout = 5 * time.Second
	}
	if config.RetryInterval <= 0 {
		config.RetryInterval = 50 * time.Millisecond
	}
	if config.RetryWindow <= 0 {
		config.RetryWindow = 10 * time.Second
	}
	return &Manager{
		installations: config.Installations, endpoint: config.Endpoint, muxviaVersion: config.MuxviaVersion,
		dial: config.Dial, start: config.Start, now: config.Now,
		readyTimeout: config.ReadyTimeout, retryInterval: config.RetryInterval, retryWindow: config.RetryWindow,
	}, nil
}

// Open 复验 active installation，按需受限启动 Companion，并完成 caller-scoped Hello。
// requested capabilities 只能被 companion 减少；未知、重复或未请求 capability 会被视为协议违规。
func (manager *Manager) Open(ctx context.Context, role cloudpb.CallerRole, requested ...cloudpb.CompanionCapability) (*ipc.Client, error) {
	installation, err := manager.installations.Status()
	if err != nil {
		return nil, err
	}
	if installation.Version == "" || installation.Channel == "" || installation.BinaryPath == "" || !validSHA256(installation.BinarySHA256) {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "active Cloud Companion installation metadata is incomplete")
	}
	client, stale, err := manager.dialAndHello(ctx, installation, role, requested)
	if err == nil {
		return client, nil
	}
	if !isStartableDialError(err) {
		return nil, err
	}
	staleObserved := stale

	manager.mu.Lock()
	defer manager.mu.Unlock()
	client, stale, err = manager.dialAndHello(ctx, installation, role, requested)
	if err == nil {
		return client, nil
	}
	if !isStartableDialError(err) {
		return nil, err
	}
	staleObserved = staleObserved || stale
	now := manager.now()
	if !manager.lastStart.IsZero() && now.Sub(manager.lastStart) < manager.retryWindow {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "Cloud Companion restart is rate limited")
	}
	readyContext, cancel := context.WithTimeout(ctx, manager.readyTimeout)
	defer cancel()
	if staleObserved {
		if err := manager.waitForEndpointExit(readyContext); err != nil {
			return nil, err
		}
	}
	manager.lastStart = now
	if err := manager.start(installation.BinaryPath, manager.endpoint, false); err != nil {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "Cloud Companion could not be started")
	}
	for {
		client, _, err := manager.dialAndHello(readyContext, installation, role, requested)
		if err == nil {
			return client, nil
		}
		if !isStartableDialError(err) {
			return nil, err
		}
		select {
		case <-readyContext.Done():
			return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "Cloud Companion did not become ready")
		case <-time.After(manager.retryInterval):
		}
	}
}

// SmokeFunc 返回 installer 使用的 staging binary handshake smoke。
// 它启动固定 `serve --smoke --socket` 模式、协商 CLI role 后请求有序 Shutdown，不激活该版本。
func SmokeFunc(muxviaVersion string) installer.SmokeFunc {
	return func(ctx context.Context, binaryPath string, manifest installer.Manifest) error {
		executableSHA256, err := executableDigest(binaryPath)
		if err != nil {
			return err
		}
		endpoint, err := smokeEndpoint()
		if err != nil {
			return err
		}
		if err := startVerifiedProcess(binaryPath, endpoint, true); err != nil {
			return err
		}
		deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var client *ipc.Client
		for {
			client, err = ipc.Dial(deadline, endpoint)
			if err == nil {
				break
			}
			if !isStartableDialError(err) {
				return err
			}
			select {
			case <-deadline.Done():
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "staged Cloud Companion did not start")
			case <-time.After(25 * time.Millisecond):
			}
		}
		defer client.Close()
		response, err := negotiate(deadline, client, muxviaVersion, cloudpb.CallerRole_CALLER_ROLE_CLI, nil)
		if err != nil {
			return err
		}
		validationErr := validateReleaseHello(response, manifest, executableSHA256)
		_, shutdownErr := client.Shutdown(deadline, &cloudpb.ShutdownRequest{Reason: "installer_smoke_complete"})
		if validationErr != nil {
			return validationErr
		}
		return shutdownErr
	}
}

func (manager *Manager) dialAndHello(ctx context.Context, installation installer.Installation, role cloudpb.CallerRole, requested []cloudpb.CompanionCapability) (*ipc.Client, bool, error) {
	client, err := manager.dial(ctx, manager.endpoint)
	if err != nil {
		return nil, false, err
	}
	response, err := negotiate(ctx, client, manager.muxviaVersion, role, requested)
	if err != nil {
		_ = client.Close()
		return nil, false, err
	}
	if response.GetCompanionVersion() != installation.Version || response.GetBuildChannel() != installation.Channel || !matchesInstallationDigest(response.GetExecutableSha256(), installation.BinarySHA256) {
		_ = client.Close()
		// 业务 caller 可能是 daemon/mobile，不能使用它的 role 调用 CLI-only Shutdown。
		// 版本接管必须重新建立最小 CLI lifecycle 连接，且不继承任何业务 capability。
		shutdownContext, cancel := context.WithTimeout(ctx, time.Second)
		_ = manager.shutdownStale(shutdownContext)
		cancel()
		return nil, true, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "running Cloud Companion does not match the active signed installation")
	}
	return client, false, nil
}

func (manager *Manager) shutdownStale(ctx context.Context) error {
	client, err := manager.dial(ctx, manager.endpoint)
	if err != nil {
		return err
	}
	defer client.Close()
	if _, err := negotiate(ctx, client, manager.muxviaVersion, cloudpb.CallerRole_CALLER_ROLE_CLI, nil); err != nil {
		return err
	}
	_, err = client.Shutdown(ctx, &cloudpb.ShutdownRequest{Reason: "active_version_changed"})
	return err
}

func negotiate(ctx context.Context, client *ipc.Client, muxviaVersion string, role cloudpb.CallerRole, requested []cloudpb.CompanionCapability) (*cloudpb.CompanionHelloResponse, error) {
	if client == nil || muxviaVersion == "" || role == cloudpb.CallerRole_CALLER_ROLE_UNSPECIFIED {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "invalid Companion Hello configuration")
	}
	requestedSet := make(map[cloudpb.CompanionCapability]struct{}, len(requested))
	for _, capability := range requested {
		if capability == cloudpb.CompanionCapability_COMPANION_CAPABILITY_UNSPECIFIED {
			return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Companion Hello contains an unspecified capability")
		}
		if _, duplicate := requestedSet[capability]; duplicate {
			return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Companion Hello contains a duplicate capability")
		}
		requestedSet[capability] = struct{}{}
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Companion Hello nonce generation failed")
	}
	response, err := client.Hello(ctx, &cloudpb.CompanionHelloRequest{
		ProtocolMin: cloudcompanion.ProtocolVersionMin, ProtocolMax: cloudcompanion.ProtocolVersionMax,
		MuxviaVersion: muxviaVersion, CallerRole: role, RequestedCapabilities: append([]cloudpb.CompanionCapability(nil), requested...), RequestNonce: nonce,
	})
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetSelectedProtocol() < cloudcompanion.ProtocolVersionMin || response.GetSelectedProtocol() > cloudcompanion.ProtocolVersionMax || response.GetCompanionVersion() == "" || response.GetBuildChannel() == "" || len(response.GetResponseNonce()) < 16 || len(response.GetResponseNonce()) > 64 || bytes.Equal(response.GetResponseNonce(), nonce) {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE, "Cloud Companion returned an invalid Hello response")
	}
	seen := make(map[cloudpb.CompanionCapability]struct{}, len(response.GetSupportedCapabilities()))
	for _, capability := range response.GetSupportedCapabilities() {
		if _, requested := requestedSet[capability]; !requested {
			return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion expanded the requested capability set")
		}
		if _, duplicate := seen[capability]; duplicate {
			return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned a duplicate capability")
		}
		seen[capability] = struct{}{}
	}
	return response, nil
}

func (manager *Manager) waitForEndpointExit(ctx context.Context) error {
	for {
		client, err := manager.dial(ctx, manager.endpoint)
		if err != nil {
			if isStartableDialError(err) {
				return nil
			}
			return err
		}
		_ = client.Close()
		select {
		case <-ctx.Done():
			return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "stale Cloud Companion did not release its local endpoint")
		case <-time.After(manager.retryInterval):
		}
	}
}

func validateReleaseHello(response *cloudpb.CompanionHelloResponse, manifest installer.Manifest, executableSHA256 string) error {
	if response == nil || response.GetCompanionVersion() != manifest.Version || response.GetBuildChannel() != manifest.Channel || response.GetSelectedProtocol() < manifest.MinCompanionProtocol || response.GetSelectedProtocol() > manifest.MaxCompanionProtocol || !matchesInstallationDigest(response.GetExecutableSha256(), executableSHA256) {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "staged Cloud Companion identity does not match its signed release manifest")
	}
	return nil
}

func executableDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validSHA256(encoded string) bool {
	decoded, err := hex.DecodeString(encoded)
	return err == nil && len(decoded) == 32
}

func matchesInstallationDigest(actual []byte, expected string) bool {
	decoded, err := hex.DecodeString(expected)
	return err == nil && len(decoded) == 32 && bytes.Equal(actual, decoded)
}

func isStartableDialError(err error) bool {
	return cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING) || cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING)
}

func startVerifiedProcess(binaryPath, endpoint string, smoke bool) error {
	arguments := []string{"serve", "--socket", endpoint}
	if smoke {
		arguments = append(arguments, "--smoke")
	}
	command := exec.Command(binaryPath, arguments...)
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	configureDetachedProcess(command)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
