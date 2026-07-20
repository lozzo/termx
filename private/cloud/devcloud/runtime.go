// Package devcloud 装配显式 dev-local 单区域 Control Plane、Hub 与 Relay。
//
// 本包只服务开发纵向 harness：控制服务使用独立 loopback listener，Relay 使用 lease-bound UDP TURN，
// 但可以由同一 supervisor 进程托管。它不提供生产 OAuth、TLS、数据库或隐式 fallback。
package devcloud

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/private/cloud/control-plane/directory"
	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/entitlement"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudhub "github.com/lozzow/termx/private/cloud/hub"
	cloudrelay "github.com/lozzow/termx/private/cloud/relay"
	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
	"github.com/lozzow/termx/proto/cloudpb"
)

const (
	devAccountID        = "account-dev-local"
	devAccountLabel     = "TermX Dev Account"
	devUserID           = "user-dev-local"
	devClientDeviceID   = "client-dev-local"
	loginCodeAlphabet   = "23456789ABCDEFGHJKMNPQRSTVWXYZ"
	devHubID            = "hub-dev-local"
	devRegion           = "local-1"
	devAdmissionIssuer  = "control-plane.dev-local"
	devPublicHubURL     = "https://hub.dev.invalid"
	devRelayID          = "relay-dev-local"
	devRelayPool        = "relay-pool-dev-local"
	devRelayRealm       = "termx-dev-relay"
	devRelayBindingID   = "relay-binding-dev-local"
	devRelayLeaseIssuer = "control-plane.dev-local.relay"

	loginTTL                  = 5 * time.Minute
	enrollmentTTL             = 10 * time.Minute
	presenceChallengeTTL      = 2 * time.Minute
	cloudSessionTTL           = 8 * time.Hour
	managedTTL                = 5 * time.Minute
	relayLeaseTTL             = 5 * time.Minute
	edgePolicyRefreshInterval = 5 * time.Minute
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
	// UsageOutboxPath 是 Relay signed usage durable queue；为空时 dev runtime 使用独立临时路径。
	UsageOutboxPath string
	// WebAccountDBPath 是 Control Plane 拥有的浏览器账号 SQLite 路径；必须与 WebCatalogPath 同时配置。
	WebAccountDBPath string
	// WebCatalogPath 是 Control Plane 读取的套餐配置路径；浏览器只消费其公开投影。
	WebCatalogPath string
	// WebStaging 显式启用固定账号和测试付款，默认生产路径保持关闭。
	WebStaging bool
	// WebSecureCookie 强制浏览器 Session 只经 HTTPS 发送；公网生产必须开启。
	WebSecureCookie bool
	// WebPublicURL 是用户浏览器打开的静态 Web Controller origin；设备码验证 URI 只从这里生成。
	WebPublicURL string
	// SecurityDirectoryPath 是 Control Plane 账号、用户和设备公钥安全目录；为空时仅用于进程内测试。
	SecurityDirectoryPath string
	// AuthorityKeyPath 是 Control Plane Ed25519 authority 的 0600 持久文件。
	AuthorityKeyPath string
	// EdgeSnapshotPath 是 Hub 已验签 policy 的 0600 持久文件；恢复时必须重新验签。
	EdgeSnapshotPath string
	// RefreshSessionPath 只持久化 refresh secret 的 SHA-256 和绑定 metadata。
	RefreshSessionPath string
}

// Runtime 是一组已启动的 dev-local Control Plane/Hub listener 与单个 UDP TURN Relay。
// Manifest 只公开连接 metadata；edge/refresh token、authority private key 和 DeviceIdentity 私钥不会导出。
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
	edgeStop  chan struct{}
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
	userCode       string
	clientDeviceID string
	clientMetadata *cloudpb.DeviceMetadata
	ownerAccountID string
	ownerUserID    string
	accountID      string
	accountLabel   string
	expiresAt      time.Time
	claimed        bool
}

type enrollmentClaim struct {
	accountID string
	userID    string
	expiresAt time.Time
	claimed   bool
}

type enrollmentFlow struct {
	challengeID string
	challenge   []byte
	publicKey   []byte
	metadata    *cloudpb.DeviceMetadata
	accountID   string
	userID      string
	expiresAt   time.Time
}

type serviceState struct {
	now    func() time.Time
	random io.Reader

	mu sync.Mutex

	loginFlows       map[string]loginFlow
	loginCodes       map[string]string
	enrollmentClaims map[string]enrollmentClaim
	enrollmentFlows  map[string]enrollmentFlow
	sessions         map[[sha256.Size]byte]cloudSession
	webPublicURL     string
	refreshSessions  *refreshStore

	directory         *directory.Store
	hub               *cloudhub.Service
	edgeIssuer        servicecredential.EdgeAccessIssuer
	edgePolicyIssuer  servicecredential.EdgePolicyIssuer
	edgeAuth          *cloudhub.EdgeAuthorizer
	edgeRevision      uint64
	edgeDevices       map[string]cloudhub.DeviceAuthorization
	directoryAccounts map[string]struct{}
	webEntitlements   map[string]entitlement.Entitlement
	webAccounts       map[string]struct{}
	webCatalog        *webcontroller.Catalog
	usageOutboxPath   string
	relayControl      *relayControlState
	webCenter         *webcontroller.UserCenterStore
	webCommerce       *webcontroller.CommerceService
	webHandler        http.Handler

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
		loginFlows: make(map[string]loginFlow), loginCodes: make(map[string]string), enrollmentClaims: make(map[string]enrollmentClaim), enrollmentFlows: make(map[string]enrollmentFlow), sessions: make(map[[sha256.Size]byte]cloudSession), webEntitlements: make(map[string]entitlement.Entitlement), webAccounts: make(map[string]struct{}), directoryAccounts: make(map[string]struct{}),
		presenceQueueSize: options.presenceQueueSize, clientQueueSize: options.clientQueueSize,
	}
	development, err := developmentEntitlement(now)
	if err != nil {
		return nil, fmt.Errorf("build development entitlement: %w", err)
	}
	state.webEntitlements[devAccountID] = development
	state.webPublicURL = strings.TrimRight(strings.TrimSpace(config.WebPublicURL), "/")
	if state.webPublicURL == "" {
		state.webPublicURL = "https://login.dev.invalid"
	}
	if config.UsageOutboxPath == "" {
		outboxID, randomErr := state.randomID("usage-outbox")
		if randomErr != nil {
			return nil, randomErr
		}
		config.UsageOutboxPath = filepath.Join(os.TempDir(), outboxID+".json")
	}
	state.usageOutboxPath = config.UsageOutboxPath
	state.refreshSessions, err = openRefreshStore(config.RefreshSessionPath, state.random, now)
	if err != nil {
		return nil, err
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
	state.enrollmentClaims[config.EnrollmentCode] = enrollmentClaim{accountID: devAccountID, userID: devUserID, expiresAt: now.Add(24 * time.Hour)}
	if err := state.initializeDomain(now, config); err != nil {
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
	if err := state.initializeWeb(config); err != nil {
		_ = relayServer.Close()
		return nil, err
	}

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
		errors:        make(chan error, 2), done: make(chan struct{}), usageStop: make(chan struct{}), edgeStop: make(chan struct{}),
	}
	runtime.serve("Control Plane", runtime.controlServer, controlListener)
	runtime.serve("Hub", runtime.hubServer, hubListener)
	runtime.reportRelayUsage()
	runtime.refreshEdgePolicy()
	if err := waitForHealth(runtime.manifest.ControlPlaneURL, runtime.manifest.HubURL); err != nil {
		shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.Close(shutdownContext)
		return nil, err
	}
	cleanupListeners = false
	return runtime, nil
}

func (state *serviceState) initializeDomain(now time.Time, config Config) error {
	signer, err := loadOrCreateAuthority(config.AuthorityKeyPath, state.random, now)
	if err != nil {
		return err
	}
	keyRing, err := servicecredential.NewKeyRing(signer.PublicKey())
	if err != nil {
		return err
	}
	edgeIssuer, err := servicecredential.NewEdgeAccessIssuer(devAdmissionIssuer, signer)
	if err != nil {
		return err
	}
	edgePolicyIssuer, err := servicecredential.NewEdgePolicyIssuer(devAdmissionIssuer, signer)
	if err != nil {
		return err
	}
	var snapshotStore cloudhub.EdgeSnapshotStore
	if config.EdgeSnapshotPath != "" {
		snapshotStore, err = cloudhub.NewFileEdgeSnapshotStore(config.EdgeSnapshotPath)
		if err != nil {
			return err
		}
	}
	edgeAuth, err := cloudhub.NewEdgeAuthorizer(cloudhub.EdgeAuthorizerConfig{HubID: devHubID, Issuer: devAdmissionIssuer, KeyRing: keyRing, Clock: runtimeClock{now: state.now}, MaxStaleness: 30 * time.Minute, SnapshotStore: snapshotStore})
	if err != nil {
		return err
	}
	store := directory.NewStore()
	if config.SecurityDirectoryPath != "" {
		store, err = directory.OpenStore(config.SecurityDirectoryPath)
		if err != nil {
			return err
		}
	}
	state.edgeDevices = make(map[string]cloudhub.DeviceAuthorization)
	if config.EdgeSnapshotPath != "" {
		if _, statErr := os.Stat(config.EdgeSnapshotPath); statErr == nil {
			if err := edgeAuth.RestoreSignedSnapshot(); err != nil {
				return err
			}
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
	}
	security := store.Snapshot()
	accountExists, userExists, clientExists := false, false, false
	for _, account := range security.Accounts {
		state.directoryAccounts[account.ID] = struct{}{}
		accountExists = accountExists || account.ID == devAccountID
	}
	for _, user := range security.Users {
		userExists = userExists || user.ID == devUserID
	}
	for _, device := range security.Devices {
		platform := "unknown"
		if device.ID == devClientDeviceID {
			platform = "development"
		}
		state.edgeDevices[device.ID] = cloudhub.DeviceAuthorization{DeviceID: device.ID, AccountID: device.AccountID, Kind: string(device.Kind), DisplayName: device.Label, Platform: platform, PublicKey: append([]byte(nil), device.PublicKey...), Revoked: device.RevokedAt != nil}
		clientExists = clientExists || device.ID == devClientDeviceID
	}
	if !accountExists {
		if err := store.PutAccount(domain.Account{ID: devAccountID, DisplayName: devAccountLabel, CreatedAt: now}); err != nil {
			return err
		}
		state.directoryAccounts[devAccountID] = struct{}{}
	}
	if !userExists {
		if err := store.PutUser(domain.User{ID: devUserID, AccountID: devAccountID, Email: "dev-local@termx.invalid", CreatedAt: now}); err != nil {
			return err
		}
	}
	if !clientExists {
		if err := store.RegisterDevice(domain.DeviceRegistration{ID: devClientDeviceID, AccountID: devAccountID, OwnerUserID: devUserID, Kind: domain.DeviceKindClient, Label: "Dev Client", RegisteredAt: now}); err != nil {
			return err
		}
		state.edgeDevices[devClientDeviceID] = cloudhub.DeviceAuthorization{DeviceID: devClientDeviceID, AccountID: devAccountID, Kind: "client", DisplayName: "Dev Client", Platform: "development"}
	}
	state.edgeIssuer = edgeIssuer
	state.edgePolicyIssuer = edgePolicyIssuer
	state.edgeAuth = edgeAuth
	state.edgeRevision = edgeAuth.Revision() + 1
	if err := state.publishEdgeSnapshot(now); err != nil {
		return err
	}
	hubService, err := cloudhub.New(cloudhub.Config{
		HubID: devHubID, Clock: runtimeClock{now: state.now},
		MaxPresenceTTL: 5 * time.Minute, MaxSignalingTTL: 5 * time.Minute,
		PresenceChallengeTTL: presenceChallengeTTL, MaxPresenceChallenges: 256, Random: state.random,
		PresenceQueueSize: state.presenceQueueSize, ClientQueueSize: state.clientQueueSize, MaxSDPBytes: 1 << 20, MaxCandidates: 256,
		MaxPresences: 128, MaxSessions: 1024, MaxSessionsPerClient: 64,
		EdgeAuthorizer: edgeAuth,
	})
	if err != nil {
		return err
	}
	state.directory = store
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
		close(runtime.edgeStop)
		relayErr := runtime.relayServer.Close()
		usageErr := runtime.flushRelayUsage(ctx, "runtime_closed")
		controlErr := runtime.controlServer.Shutdown(ctx)
		hubErr := runtime.hubServer.Shutdown(ctx)
		_ = runtime.controlListener.Close()
		_ = runtime.hubListener.Close()
		runtime.waitGroup.Wait()
		var webErr error
		if runtime.state.webCenter != nil {
			webErr = runtime.state.webCenter.Close()
		}
		runtime.closeErr = errors.Join(usageErr, relayErr, controlErr, hubErr, webErr)
		close(runtime.done)
	})
	return runtime.closeErr
}

// refreshEdgePolicy 定期模拟 Control Plane 向 Hub 推送完整签名授权投影。
// Hub 请求热路径仍只读本地内存；刷新失败会使 runtime 明确失败，不能用过期快照继续放行。
func (runtime *Runtime) refreshEdgePolicy() {
	runtime.waitGroup.Add(1)
	go func() {
		defer runtime.waitGroup.Done()
		ticker := time.NewTicker(edgePolicyRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runtime.edgeStop:
				return
			case <-ticker.C:
				if err := runtime.state.refreshEdgeSnapshot(runtime.state.now().UTC()); err != nil {
					select {
					case runtime.errors <- fmt.Errorf("dev Hub authorization refresh failed"):
					default:
					}
					return
				}
			}
		}
	}()
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

// newLoginCodeLocked 生成活动集合内唯一的人类可输入短码；调用方必须持有 state.mu。
// 短码只定位 Flow，最终 Session 领取仍要求独立的 128-bit flow ID。
func (state *serviceState) newLoginCodeLocked() (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		value, err := state.randomBytes(8)
		if err != nil {
			return "", err
		}
		number := binary.BigEndian.Uint64(value)
		clear(value)
		compactBytes := make([]byte, 10)
		for index := len(compactBytes) - 1; index >= 0; index-- {
			compactBytes[index] = loginCodeAlphabet[number%uint64(len(loginCodeAlphabet))]
			number /= uint64(len(loginCodeAlphabet))
		}
		compact := string(compactBytes)
		code := compact[:5] + "-" + compact[5:]
		if _, exists := state.loginCodes[code]; !exists {
			return code, nil
		}
	}
	return "", fmt.Errorf("generate unique login code: active code space is unavailable")
}

func fingerprint(publicKey []byte) string {
	digest := sha256.Sum256(publicKey)
	return "sha256:" + base64.RawURLEncoding.EncodeToString(digest[:])
}
