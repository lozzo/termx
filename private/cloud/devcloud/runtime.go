// Package devcloud 装配显式 dev-local 单区域 Control Plane、Hub 与 Relay。
//
// 本包只服务开发纵向 harness：控制服务使用独立 loopback listener，Relay 使用 lease-bound UDP TURN，
// 但可以由同一 supervisor 进程托管。它不提供生产 OAuth、TLS、数据库或隐式 fallback。
package devcloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/private/cloud/control-plane/admission"
	"github.com/lozzow/termx/private/cloud/control-plane/directory"
	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/presence"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudhub "github.com/lozzow/termx/private/cloud/hub"
	cloudrelay "github.com/lozzow/termx/private/cloud/relay"
	"github.com/lozzow/termx/proto/cloudpb"
)

const (
	devAccountID        = "account-dev-local"
	devAccountLabel     = "TermX Dev Account"
	devUserID           = "user-dev-local"
	devClientDeviceID   = "client-dev-local"
	devHubID            = "hub-dev-local"
	devRegion           = "local-1"
	devAdmissionIssuer  = "control-plane.dev-local"
	devPublicHubURL     = "https://hub.dev.invalid"
	devRelayID          = "relay-dev-local"
	devRelayPool        = "relay-pool-dev-local"
	devRelayRealm       = "termx-dev-relay"
	devRelayBindingID   = "relay-binding-dev-local"
	devRelayLeaseIssuer = "control-plane.dev-local.relay"

	loginTTL        = 5 * time.Minute
	enrollmentTTL   = 2 * time.Minute
	cloudSessionTTL = 8 * time.Hour
	managedTTL      = 5 * time.Minute
	admissionTTL    = 2 * time.Minute
	relayLeaseTTL   = 5 * time.Minute
)

// Config 控制开发 runtime 的 listener、时间、随机源和一次性 enrollment code。
// HTTP Listener 始终必须是 loopback；只有显式 staging-ssh profile 可将 UDP Relay 绑定公网。
type Config struct {
	ControlPlaneListener net.Listener
	HubListener          net.Listener
	RelayListenAddr      string
	RelayPublicIP        string
	Profile              string
	Now                  func() time.Time
	Random               io.Reader
	EnrollmentCode       string
}

// Runtime 是一组已启动的 dev-local Control Plane/Hub listener 与单个 UDP TURN Relay。
// Manifest 只公开连接 metadata；账号 token、admission signing key 和 DeviceIdentity 私钥不会导出。
type Runtime struct {
	manifest httpapi.Manifest

	controlListener net.Listener
	hubListener     net.Listener
	controlServer   *http.Server
	hubServer       *http.Server
	relayServer     *cloudrelay.Server
	state           *serviceState

	waitGroup sync.WaitGroup
	errors    chan error
	done      chan struct{}
	usageStop chan struct{}
	closeOnce sync.Once
	closeErr  error
}

type runtimeClock struct{ now func() time.Time }

func (clock runtimeClock) Now() time.Time { return clock.now().UTC() }

type synchronizedReader struct {
	mu     sync.Mutex
	source io.Reader
}

func (reader *synchronizedReader) Read(buffer []byte) (int, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.source.Read(buffer)
}

type runtimeOptions struct {
	presenceQueueSize int
	clientQueueSize   int
}

type cloudSession struct {
	kind         session.Kind
	accountID    string
	accountLabel string
	deviceID     string
	expiresAt    time.Time
}

type loginFlow struct {
	expiresAt time.Time
}

type enrollmentFlow struct {
	challengeID string
	challenge   []byte
	publicKey   []byte
	metadata    *cloudpb.DeviceMetadata
	expiresAt   time.Time
}

type serviceState struct {
	now    func() time.Time
	random io.Reader

	mu sync.Mutex

	enrollmentCode    string
	enrollmentClaimed bool
	loginFlows        map[string]loginFlow
	enrollmentFlows   map[string]enrollmentFlow
	sessions          map[[sha256.Size]byte]cloudSession

	directory    *directory.Store
	presence     *presence.Service
	admission    *admission.Service
	hub          *cloudhub.Service
	relayControl *relayControlState

	presenceQueueSize int
	clientQueueSize   int
}

// Start 创建短期签名 key、固定开发账号和两个真实 loopback HTTP listener。
// 初始化失败会关闭本函数创建或接管的 listener；成功后调用方必须 Close runtime。
func Start(config Config) (*Runtime, error) {
	return start(config, runtimeOptions{presenceQueueSize: 64, clientQueueSize: 64})
}

func start(config Config, options runtimeOptions) (*Runtime, error) {
	if config.Profile == "" {
		config.Profile = httpapi.ProfileDevLocal
	}
	if config.Profile != httpapi.ProfileDevLocal && config.Profile != httpapi.ProfileStagingSSH {
		return nil, fmt.Errorf("unsupported dev cloud profile %q", config.Profile)
	}
	if config.Profile == httpapi.ProfileDevLocal && config.RelayPublicIP != "" {
		return nil, fmt.Errorf("dev-local profile cannot advertise a public Relay")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	now := config.Now().UTC()
	controlListener, err := prepareListener(config.ControlPlaneListener)
	if err != nil {
		return nil, fmt.Errorf("prepare dev Control Plane listener: %w", err)
	}
	hubListener, err := prepareListener(config.HubListener)
	if err != nil {
		_ = controlListener.Close()
		return nil, fmt.Errorf("prepare dev Hub listener: %w", err)
	}
	cleanupListeners := true
	defer func() {
		if cleanupListeners {
			_ = controlListener.Close()
			_ = hubListener.Close()
		}
	}()
	state := &serviceState{
		now: config.Now, random: &synchronizedReader{source: config.Random},
		loginFlows: make(map[string]loginFlow), enrollmentFlows: make(map[string]enrollmentFlow), sessions: make(map[[sha256.Size]byte]cloudSession),
		presenceQueueSize: options.presenceQueueSize, clientQueueSize: options.clientQueueSize,
	}
	if state.presenceQueueSize < 1 || state.clientQueueSize < 1 {
		return nil, fmt.Errorf("dev Hub queue capacity must be positive")
	}
	if config.EnrollmentCode == "" {
		config.EnrollmentCode, err = state.randomID("enroll")
		if err != nil {
			return nil, err
		}
	}
	state.enrollmentCode = config.EnrollmentCode
	if err := state.initializeDomain(now); err != nil {
		return nil, err
	}
	relayListenAddr, err := prepareRelayListenAddr(config.RelayListenAddr, config.RelayPublicIP, config.Profile)
	if err != nil {
		return nil, err
	}
	relayServer, err := cloudrelay.NewServer(cloudrelay.ServerConfig{Authority: state.relayControl.authority, ListenAddr: relayListenAddr, PublicIP: config.RelayPublicIP})
	if err != nil {
		return nil, err
	}
	state.relayControl.url = relayServer.URL()

	startedAt := now.Truncate(time.Second)
	runtime := &Runtime{
		manifest: httpapi.Manifest{
			Version: httpapi.ManifestVersion, Profile: config.Profile,
			ControlPlaneURL: listenerOrigin(controlListener), HubURL: listenerOrigin(hubListener), RelayURL: state.relayControl.url,
			HubID: devHubID, Region: devRegion, AccountLabel: devAccountLabel,
			EnrollmentCode: config.EnrollmentCode, StartedAtRFC3339: startedAt.Format(time.RFC3339),
		},
		controlListener: controlListener, hubListener: hubListener,
		controlServer: &http.Server{Handler: state.controlHandler(), ReadHeaderTimeout: 5 * time.Second},
		hubServer:     &http.Server{Handler: state.hubHandler(), ReadHeaderTimeout: 5 * time.Second},
		relayServer:   relayServer,
		state:         state,
		errors:        make(chan error, 2), done: make(chan struct{}), usageStop: make(chan struct{}),
	}
	runtime.serve("Control Plane", runtime.controlServer, controlListener)
	runtime.serve("Hub", runtime.hubServer, hubListener)
	runtime.reportRelayUsage()
	if err := waitForHealth(runtime.manifest.ControlPlaneURL, runtime.manifest.HubURL); err != nil {
		shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.Close(shutdownContext)
		return nil, err
	}
	cleanupListeners = false
	return runtime, nil
}

func (state *serviceState) initializeDomain(now time.Time) error {
	_, signingPrivateKey, err := ed25519.GenerateKey(state.random)
	if err != nil {
		return fmt.Errorf("generate dev admission signing key: %w", err)
	}
	signer, err := servicecredential.NewSigner("dev-admission-key", signingPrivateKey, now.Add(-time.Minute), now.Add(24*time.Hour))
	clear(signingPrivateKey)
	if err != nil {
		return err
	}
	issuer, err := servicecredential.NewHubAdmissionIssuer(devAdmissionIssuer, signer)
	if err != nil {
		return err
	}
	keyRing, err := servicecredential.NewKeyRing(signer.PublicKey())
	if err != nil {
		return err
	}
	store := directory.NewStore()
	if err := store.PutAccount(domain.Account{ID: devAccountID, DisplayName: devAccountLabel, CreatedAt: now}); err != nil {
		return err
	}
	if err := store.PutUser(domain.User{ID: devUserID, AccountID: devAccountID, Email: "dev-local@termx.invalid", CreatedAt: now}); err != nil {
		return err
	}
	clientPublicKey, clientPrivateKey, err := ed25519.GenerateKey(state.random)
	if err != nil {
		return fmt.Errorf("generate dev client directory key: %w", err)
	}
	clear(clientPrivateKey)
	if err := store.RegisterDevice(domain.DeviceRegistration{
		ID: devClientDeviceID, AccountID: devAccountID, OwnerUserID: devUserID, Kind: domain.DeviceKindClient,
		Label: "Dev Client", PublicKey: clientPublicKey, Fingerprint: fingerprint(clientPublicKey), RegisteredAt: now,
	}); err != nil {
		return err
	}
	presenceService, err := presence.NewService(presence.Config{
		Devices: store, Issuer: issuer, HubID: devHubID, ChallengeTTL: enrollmentTTL,
		AdmissionTTL: admissionTTL, MaxChallenges: 256, Now: state.now, Random: state.random,
	})
	if err != nil {
		return err
	}
	admissionService, err := admission.NewService(store, issuer)
	if err != nil {
		return err
	}
	hubService, err := cloudhub.New(cloudhub.Config{
		HubID: devHubID, AdmissionIssuer: devAdmissionIssuer, KeyRing: keyRing, Clock: runtimeClock{now: state.now},
		MaxPresenceTTL: 5 * time.Minute, MaxSignalingTTL: 5 * time.Minute,
		PresenceQueueSize: state.presenceQueueSize, ClientQueueSize: state.clientQueueSize, MaxSDPBytes: 1 << 20, MaxCandidates: 256,
		MaxPresences: 128, MaxSessions: 1024, MaxSessionsPerClient: 64, MaxReplayEntries: 2048,
	})
	if err != nil {
		return err
	}
	state.directory = store
	state.presence = presenceService
	state.admission = admissionService
	state.hub = hubService
	return state.initializeRelay(now)
}

// Manifest 返回当前 runtime 的非秘密连接 metadata 副本。
// EnrollmentCode 仅用于本次 dev-local 启动，不能用于生产设备注册。
func (runtime *Runtime) Manifest() httpapi.Manifest {
	if runtime == nil {
		return httpapi.Manifest{}
	}
	return runtime.manifest
}

// WriteManifest 原子写入仅当前用户可读的 dev-local runtime manifest。
// 文件不包含 session token 或签名私钥；0600 权限仍避免一次性 enrollment code 被其他用户读取。
func (runtime *Runtime) WriteManifest(path string) error {
	if runtime == nil || path == "" {
		return fmt.Errorf("dev cloud manifest path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create dev cloud manifest directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".runtime-*.json")
	if err != nil {
		return fmt.Errorf("create dev cloud manifest: %w", err)
	}
	temporaryPath := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(runtime.manifest); err != nil {
		return fmt.Errorf("encode dev cloud manifest: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync dev cloud manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish dev cloud manifest: %w", err)
	}
	committed = true
	return nil
}

// Wait 阻塞到任一 HTTP listener 意外退出，或 runtime 已由 Close 完整停止。
// 正常 Close 返回 nil；listener 失败返回脱敏的服务级错误，不包含请求或 credential 内容。
func (runtime *Runtime) Wait() error {
	if runtime == nil {
		return nil
	}
	select {
	case err := <-runtime.errors:
		return err
	case <-runtime.done:
		return nil
	}
}

// Close 幂等关闭 Relay、Control Plane 与 Hub，并等待两个 HTTP Serve goroutine 退出。
// 关闭一个 dev runtime 不修改公开 daemon、terminal lifecycle 或本地/SSH endpoint。
func (runtime *Runtime) Close(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	runtime.closeOnce.Do(func() {
		close(runtime.usageStop)
		relayErr := runtime.relayServer.Close()
		usageErr := runtime.flushRelayUsage(ctx, "runtime_closed")
		controlErr := runtime.controlServer.Shutdown(ctx)
		hubErr := runtime.hubServer.Shutdown(ctx)
		_ = runtime.controlListener.Close()
		_ = runtime.hubListener.Close()
		runtime.waitGroup.Wait()
		runtime.closeErr = errors.Join(usageErr, relayErr, controlErr, hubErr)
		close(runtime.done)
	})
	return runtime.closeErr
}

func prepareRelayListenAddr(address, publicIP, profile string) (string, error) {
	if address == "" {
		return "127.0.0.1:0", nil
	}
	host, _, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if err != nil || ip == nil {
		return "", fmt.Errorf("Relay listen address must be an IP UDP address")
	}
	if ip.IsLoopback() {
		return address, nil
	}
	if profile != httpapi.ProfileStagingSSH || !ip.IsUnspecified() || net.ParseIP(publicIP) == nil || net.ParseIP(publicIP).IsUnspecified() {
		return "", fmt.Errorf("public Relay requires staging-ssh profile, unspecified bind, and public IP")
	}
	return address, nil
}

func (runtime *Runtime) serve(name string, server *http.Server, listener net.Listener) {
	runtime.waitGroup.Add(1)
	go func() {
		defer runtime.waitGroup.Done()
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			select {
			case runtime.errors <- fmt.Errorf("dev %s listener stopped", name):
			default:
			}
		}
	}()
}

func prepareListener(listener net.Listener) (net.Listener, error) {
	if listener == nil {
		return net.Listen("tcp", "127.0.0.1:0")
	}
	tcpAddress, ok := listener.Addr().(*net.TCPAddr)
	if !ok || tcpAddress.IP == nil || !tcpAddress.IP.IsLoopback() {
		return nil, fmt.Errorf("listener must be loopback TCP")
	}
	return listener, nil
}

func listenerOrigin(listener net.Listener) string {
	return "http://" + listener.Addr().String()
}

func waitForHealth(origins ...string) error {
	client := &http.Client{Timeout: time.Second}
	for _, origin := range origins {
		deadline := time.Now().Add(time.Second)
		for {
			response, err := client.Get(origin + "/healthz")
			if err == nil {
				_ = response.Body.Close()
				if response.StatusCode == http.StatusNoContent {
					break
				}
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("dev cloud readiness failed")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return nil
}

func (state *serviceState) randomBytes(size int) ([]byte, error) {
	buffer := make([]byte, size)
	_, err := io.ReadFull(state.random, buffer)
	if err != nil {
		clear(buffer)
		return nil, fmt.Errorf("generate dev cloud random value: %w", err)
	}
	return buffer, nil
}

func (state *serviceState) randomID(prefix string) (string, error) {
	value, err := state.randomBytes(16)
	if err != nil {
		return "", err
	}
	defer clear(value)
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(value), nil
}

func fingerprint(publicKey []byte) string {
	digest := sha256.Sum256(publicKey)
	return "sha256:" + base64.RawURLEncoding.EncodeToString(digest[:])
}
