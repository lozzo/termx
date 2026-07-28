// Package runtime 组装单个 AnyTTY Cloud Edge 进程的公网 listener、健康状态和 ControllerLink。
package runtime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anytty/anytty/cloud/configsignature"
	"github.com/anytty/anytty/cloud/edge/agentgateway"
	edgecertificate "github.com/anytty/anytty/cloud/edge/certificate"
	"github.com/anytty/anytty/cloud/edge/clientgateway"
	"github.com/anytty/anytty/cloud/edge/controllerlink"
	"github.com/anytty/anytty/cloud/edge/policy"
	"github.com/anytty/anytty/cloud/edge/relay"
	"github.com/anytty/anytty/cloud/edge/usage"
	"github.com/anytty/anytty/cloud/processhealth"
	"github.com/anytty/anytty/cloud/securetransport"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	grpc_health "google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Config 是 Edge 进程启动所需的本机 bootstrap 配置。
// 区域、容量、域名和策略不在该结构中，后续由签名 DesiredConfig 拥有。
type Config struct {
	ListenAddress               string
	PublicCertificateFile       string
	PublicPrivateKeyFile        string
	ControllerAddress           string
	ControllerServerName        string
	ControllerCAFile            string
	IdentityCertificateFile     string
	IdentityPrivateKeyFile      string
	EdgeID                      string
	BootID                      string
	SoftwareVersion             string
	ConfigSigningKeyID          string
	ConfigSigningPublicKeyFile  string
	DesiredConfigCacheFile      string
	ManagedCertificateStateFile string
	TURNListenAddress           string
	TURNPublicEndpoint          string
	TURNRealm                   string
	UsageOutboxFile             string
}

// Runtime 拥有 Edge 公网 listener、唯一 ControllerLink 和唯一内存 State actor。
type Runtime struct {
	config             Config
	publicTLS          *tls.Config
	controllerTLS      *tls.Config
	listener           net.Listener
	httpServer         *http.Server
	grpcServer         *grpc.Server
	grpcHealth         *grpc_health.Server
	health             *processhealth.State
	errors             chan error
	readyChanges       chan struct{}
	ctx                context.Context
	cancel             context.CancelFunc
	waitGroup          sync.WaitGroup
	shutdownOnce       sync.Once
	state              *State
	configPublicKey    ed25519.PublicKey
	certificateManager *edgecertificate.Manager
	ticketKeysMu       sync.RWMutex
	ticketKeys         ticket.KeySet
	credentialDeriver  *policy.CredentialDeriver
	usageOutbox        *usage.Outbox
	relayServer        *relay.Server
	controlSessionMu   sync.RWMutex
	controlSession     *controllerlink.Session
	usagePumpMu        sync.Mutex
	usageDegraded      atomic.Bool
}

// Start 先启动固定 HTTPS /healthz，再在后台建立 mTLS EdgeControl。
// Controller 暂时不可达时进程保持 alive 但 ready=false，不开启任何收费会话。
func Start(parent context.Context, config Config) (*Runtime, error) {
	config = normalizeConfig(config)
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	publicTLS, publicCertificateLoader, err := securetransport.NewReloadableServerTLSConfig(securetransport.ServerOptions{
		CertificateFile: config.PublicCertificateFile,
		PrivateKeyFile:  config.PublicPrivateKeyFile,
	})
	if err != nil {
		return nil, fmt.Errorf("load Edge public TLS: %w", err)
	}
	certificateManager, err := edgecertificate.New(edgecertificate.Config{EdgeID: config.EdgeID, StateFile: config.ManagedCertificateStateFile}, publicCertificateLoader)
	if err != nil {
		return nil, fmt.Errorf("load managed Edge certificate: %w", err)
	}
	controllerTLS, err := securetransport.NewClientTLSConfig(securetransport.ClientOptions{
		CertificateFile: config.IdentityCertificateFile,
		PrivateKeyFile:  config.IdentityPrivateKeyFile,
		RootCAFile:      config.ControllerCAFile,
		ServerName:      config.ControllerServerName,
	})
	if err != nil {
		return nil, fmt.Errorf("load Edge identity TLS: %w", err)
	}
	var configPublicKey ed25519.PublicKey
	if config.ConfigSigningKeyID != "" || config.ConfigSigningPublicKeyFile != "" || config.DesiredConfigCacheFile != "" {
		payload, readErr := os.ReadFile(config.ConfigSigningPublicKeyFile)
		if readErr != nil {
			return nil, fmt.Errorf("read Edge config signing public key: %w", readErr)
		}
		if len(payload) != ed25519.PublicKeySize || config.ConfigSigningKeyID == "" || config.DesiredConfigCacheFile == "" {
			return nil, errors.New("Edge config signing key ID, raw Ed25519 public key, and cache file must be configured together")
		}
		configPublicKey = ed25519.PublicKey(append([]byte(nil), payload...))
	}
	state, err := NewState(StateConfig{MailboxSize: 1024, DeltaBuffer: 4096})
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		state.Close()
		return nil, fmt.Errorf("listen Edge public address: %w", err)
	}
	healthState := &processhealth.State{}
	healthState.SetAlive(true)
	grpcServer := grpc.NewServer()
	grpcHealth := grpc_health.NewServer()
	grpcHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, grpcHealth)
	ctx, cancel := context.WithCancel(parent)
	runtime := &Runtime{
		config: config, publicTLS: publicTLS, controllerTLS: controllerTLS,
		listener: listener, grpcServer: grpcServer, grpcHealth: grpcHealth, health: healthState,
		errors: make(chan error, 1), readyChanges: make(chan struct{}, 1), ctx: ctx, cancel: cancel,
		state:              state,
		configPublicKey:    configPublicKey,
		certificateManager: certificateManager,
	}
	if config.TURNListenAddress != "" {
		outbox, openErr := usage.Open(config.UsageOutboxFile, 2*time.Second)
		if openErr != nil {
			// usage outbox 是 Relay 的计费真值；损坏或不可写只关闭收费 Relay，P2P 信令仍可运行。
			runtime.usageDegraded.Store(true)
		} else {
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				_ = outbox.Close()
				_ = listener.Close()
				state.Close()
				cancel()
				return nil, fmt.Errorf("generate TURN credential secret: %w", err)
			}
			deriver, err := policy.NewCredentialDeriver(secret, []string{
				"turn:" + config.TURNPublicEndpoint + "?transport=udp",
				"turn:" + config.TURNPublicEndpoint + "?transport=tcp",
			})
			if err != nil {
				_ = outbox.Close()
				_ = listener.Close()
				state.Close()
				cancel()
				return nil, err
			}
			runtime.usageOutbox, runtime.credentialDeriver = outbox, deriver
		}
	}
	agentService, err := agentgateway.NewService(agentgateway.Config{
		EdgeID: config.EdgeID, EdgeBootID: config.BootID, Runtime: state,
		VerificationKeys: runtime.currentTicketKeys, Heartbeat: 10 * time.Second, HeartbeatTimeout: 30 * time.Second,
	})
	if err != nil {
		_ = listener.Close()
		if runtime.usageOutbox != nil {
			_ = runtime.usageOutbox.Close()
		}
		state.Close()
		cancel()
		return nil, err
	}
	cloudv1.RegisterAgentGatewayServer(grpcServer, agentService)
	clientService, err := clientgateway.NewService(clientgateway.Config{
		EdgeID: config.EdgeID, EdgeBootID: config.BootID, Runtime: state,
		SignalTimeout: 20 * time.Second, Relay: runtime.relayBroker(),
	})
	if err != nil {
		_ = listener.Close()
		if runtime.usageOutbox != nil {
			_ = runtime.usageOutbox.Close()
		}
		state.Close()
		cancel()
		return nil, err
	}
	cloudv1.RegisterClientGatewayServer(grpcServer, clientService)
	if runtime.usageOutbox != nil {
		relayServer, relayErr := relay.Start(relay.Config{ListenAddress: config.TURNListenAddress, PublicEndpoint: config.TURNPublicEndpoint, Realm: config.TURNRealm, Runtime: state, Outbox: runtime.usageOutbox})
		if relayErr != nil {
			_ = listener.Close()
			_ = runtime.usageOutbox.Close()
			state.Close()
			cancel()
			return nil, relayErr
		}
		runtime.relayServer = relayServer
	}
	runtime.httpServer = &http.Server{Handler: runtime, ReadHeaderTimeout: 5 * time.Second, TLSConfig: publicTLS}
	runtime.waitGroup.Add(2)
	go runtime.servePublic()
	go runtime.runControllerLink()
	return runtime, nil
}

// PublicAddress 返回 Edge 公网 HTTPS/gRPC 共用 listener 的已绑定地址。
func (runtime *Runtime) PublicAddress() string {
	return runtime.listener.Addr().String()
}

// TURNAddress 返回同一 Edge 进程内置的 TURN UDP/TCP listener；未配置或降级时为空。
func (runtime *Runtime) TURNAddress() string {
	if runtime == nil || runtime.relayServer == nil {
		return ""
	}
	return runtime.relayServer.Address()
}

// RelayDegraded 表示 usage outbox 不可用或 Relay 数据面已发生不可继续计费的错误。
// 该状态只关闭新 Relay 分配，不影响同一 Edge 上的 P2P 信令和健康存活。
func (runtime *Runtime) RelayDegraded() bool {
	if runtime == nil {
		return false
	}
	return runtime.usageDegraded.Load() || (runtime.relayServer != nil && runtime.relayServer.Degraded())
}

// UsageOutboxDepth 返回未确认 Relay usage 数量，供健康检查和 R6 故障恢复门禁使用。
func (runtime *Runtime) UsageOutboxDepth() (int, error) {
	if runtime == nil || runtime.usageOutbox == nil {
		return 0, nil
	}
	return runtime.usageOutbox.Len()
}

// Ready 表示 Controller 已校验并原子接受当前 generation 的完整 Runtime 快照。
func (runtime *Runtime) Ready() bool {
	return runtime.health.Ready()
}

// WaitReady 等待 ControllerLink 就绪，超时或进程关闭时返回 context error。
func (runtime *Runtime) WaitReady(ctx context.Context) error {
	for !runtime.Ready() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-runtime.ctx.Done():
			return runtime.ctx.Err()
		case <-runtime.readyChanges:
		}
	}
	return nil
}

// Errors 只输出 Edge 公网 listener 的致命错误；ControllerLink 断开会撤销 ready 并有界重连。
func (runtime *Runtime) Errors() <-chan error {
	return runtime.errors
}

// UpsertAgent 把认证后的 daemon Presence 提交给唯一 State actor。
// R2 由 integration harness 调用，R4 将由 AgentGateway 调用同一路径。
func (runtime *Runtime) UpsertAgent(ctx context.Context, agent *cloudv1.AgentPresence) error {
	return runtime.state.UpsertAgent(ctx, agent)
}

// UpsertSession 把认证后的客户端信令摘要提交给唯一 State actor。
// R2 由 integration harness 调用，R5 将由 ClientGateway 调用同一路径。
func (runtime *Runtime) UpsertSession(ctx context.Context, session *cloudv1.ClientSessionSummary) error {
	return runtime.state.UpsertSession(ctx, session)
}

// ServeHTTP 在同一 TLS listener 上路由 gRPC health 和固定 HTTP 健康路径。
func (runtime *Runtime) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.ProtoMajor == 2 && strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/grpc") {
		runtime.grpcServer.ServeHTTP(writer, request)
		return
	}
	runtime.health.ServeHTTP(writer, request)
}

// Shutdown 进入 not-ready，取消 ControllerLink，关闭公网 listener 并等待有界 goroutine 退出。
func (runtime *Runtime) Shutdown(ctx context.Context) error {
	var shutdownErr error
	runtime.shutdownOnce.Do(func() {
		runtime.setReady(false)
		runtime.grpcHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		if runtime.relayServer != nil {
			if err := runtime.relayServer.Close(); err != nil {
				shutdownErr = err
			}
			_ = runtime.flushUsage(ctx)
		}
		runtime.cancel()
		runtime.grpcServer.GracefulStop()
		if err := runtime.httpServer.Shutdown(ctx); err != nil {
			shutdownErr = err
		}
		waitDone := make(chan struct{})
		go func() {
			runtime.waitGroup.Wait()
			close(waitDone)
		}()
		select {
		case <-waitDone:
		case <-ctx.Done():
			if shutdownErr == nil {
				shutdownErr = ctx.Err()
			}
		}
		runtime.state.Close()
		if runtime.usageOutbox != nil {
			if err := runtime.usageOutbox.Close(); shutdownErr == nil {
				shutdownErr = err
			}
		}
		runtime.health.SetAlive(false)
	})
	return shutdownErr
}

func (runtime *Runtime) servePublic() {
	defer runtime.waitGroup.Done()
	if err := runtime.httpServer.ServeTLS(runtime.listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		runtime.errors <- fmt.Errorf("serve Edge public listener: %w", err)
	}
}

func (runtime *Runtime) runControllerLink() {
	defer runtime.waitGroup.Done()
	delay := 100 * time.Millisecond
	for runtime.ctx.Err() == nil {
		var applyDesiredConfig func(context.Context, *cloudv1.SignedEdgeDesiredConfig) (uint64, error)
		if len(runtime.configPublicKey) != 0 {
			applyDesiredConfig = runtime.applyDesiredConfig
		}
		capabilities := []cloudv1.EdgeCapability{cloudv1.EdgeCapability_EDGE_CAPABILITY_CONTROL_STREAM}
		capabilities = append(capabilities, cloudv1.EdgeCapability_EDGE_CAPABILITY_CERTIFICATE_HOT_RELOAD)
		if runtime.relayServer != nil && runtime.usageOutbox != nil && !runtime.RelayDegraded() {
			capabilities = append(capabilities, cloudv1.EdgeCapability_EDGE_CAPABILITY_RELAY, cloudv1.EdgeCapability_EDGE_CAPABILITY_USAGE_OUTBOX)
		}
		certificateProfileID, certificateRevision := runtime.certificateManager.Current()
		session, err := controllerlink.Open(runtime.ctx, controllerlink.Config{
			ControllerAddress:    runtime.config.ControllerAddress,
			TLSConfig:            runtime.controllerTLS,
			EdgeID:               runtime.config.EdgeID,
			BootID:               runtime.config.BootID,
			SoftwareVersion:      runtime.config.SoftwareVersion,
			CertificateProfileID: certificateProfileID,
			CertificateVersion:   certificateRevision,
			Capabilities:         capabilities,
			OpenRuntimeFeed: func(ctx context.Context) (*controllerlink.RuntimeFeed, error) {
				feed, err := runtime.state.OpenFeed(ctx)
				if err != nil {
					return nil, err
				}
				return &controllerlink.RuntimeFeed{Snapshot: feed.Snapshot, Deltas: feed.Deltas, Close: feed.Close}, nil
			},
			ApplyDesiredConfig: applyDesiredConfig,
			ApplyCertificate:   runtime.certificateManager.Apply,
			CloseDaemon:        runtime.state.CloseAgentConnection,
			CloseSession:       runtime.state.CloseSession,
		})
		if err == nil {
			if keys := session.Welcome().GetTicketVerificationKeys(); len(keys) != 0 {
				parsed, parseErr := ticket.FromVerificationKeys(keys)
				if parseErr != nil {
					_ = session.Close()
					err = parseErr
				} else {
					runtime.ticketKeysMu.Lock()
					runtime.ticketKeys = parsed
					runtime.ticketKeysMu.Unlock()
				}
			}
			if runtime.ctx.Err() != nil {
				_ = session.Close()
				return
			}
			if err = session.WaitReady(runtime.ctx); err != nil {
				_ = session.Close()
			} else {
				runtime.setControlSession(session)
				runtime.setReady(true)
				runtime.grpcHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
				delay = 100 * time.Millisecond
				err = session.Wait()
				runtime.setControlSession(nil)
				_ = session.Close()
				runtime.setReady(false)
				runtime.grpcHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
			}
		}
		if runtime.ctx.Err() != nil || controllerlink.IsExpectedClosure(runtime.ctx, err) {
			return
		}
		timer := time.NewTimer(delay)
		select {
		case <-runtime.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 5*time.Second {
			delay *= 2
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}
}

func (runtime *Runtime) currentTicketKeys() ticket.KeySet {
	runtime.ticketKeysMu.RLock()
	defer runtime.ticketKeysMu.RUnlock()
	result := make(ticket.KeySet, len(runtime.ticketKeys))
	for id, publicKey := range runtime.ticketKeys {
		result[id] = append(ed25519.PublicKey(nil), publicKey...)
	}
	return result
}

func (runtime *Runtime) setReady(value bool) {
	if runtime.health.Ready() == value {
		return
	}
	runtime.health.SetReady(value)
	select {
	case runtime.readyChanges <- struct{}{}:
	default:
	}
}

func normalizeConfig(config Config) Config {
	config.ListenAddress = strings.TrimSpace(config.ListenAddress)
	config.ControllerAddress = strings.TrimSpace(config.ControllerAddress)
	config.ControllerServerName = strings.TrimSpace(config.ControllerServerName)
	config.EdgeID = strings.TrimSpace(config.EdgeID)
	config.BootID = strings.TrimSpace(config.BootID)
	config.SoftwareVersion = strings.TrimSpace(config.SoftwareVersion)
	config.ConfigSigningKeyID = strings.TrimSpace(config.ConfigSigningKeyID)
	config.ConfigSigningPublicKeyFile = strings.TrimSpace(config.ConfigSigningPublicKeyFile)
	config.DesiredConfigCacheFile = strings.TrimSpace(config.DesiredConfigCacheFile)
	config.ManagedCertificateStateFile = strings.TrimSpace(config.ManagedCertificateStateFile)
	if config.ManagedCertificateStateFile == "" && config.PublicCertificateFile != "" {
		config.ManagedCertificateStateFile = filepath.Join(filepath.Dir(config.PublicCertificateFile), "managed-certificate.pb")
	}
	config.TURNListenAddress = strings.TrimSpace(config.TURNListenAddress)
	config.TURNPublicEndpoint = strings.TrimSpace(config.TURNPublicEndpoint)
	config.TURNRealm = strings.TrimSpace(config.TURNRealm)
	config.UsageOutboxFile = strings.TrimSpace(config.UsageOutboxFile)
	return config
}

func (runtime *Runtime) applyDesiredConfig(_ context.Context, signed *cloudv1.SignedEdgeDesiredConfig) (uint64, error) {
	if len(runtime.configPublicKey) == 0 {
		return 0, errors.New("Edge desired config verifier is not configured")
	}
	config, err := configsignature.Verify(signed, runtime.config.ConfigSigningKeyID, runtime.configPublicKey)
	if err != nil {
		return 0, err
	}
	if config.GetEdgeId() != runtime.config.EdgeID || config.GetVersion() == 0 || strings.TrimSpace(config.GetPublicEndpoint()) == "" || config.GetCapacity() == 0 || strings.TrimSpace(config.GetRegion()) == "" {
		return config.GetVersion(), errors.New("desired config identity, version, endpoint, region, or capacity is invalid")
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(signed)
	if err != nil {
		return config.GetVersion(), err
	}
	if err := os.MkdirAll(filepath.Dir(runtime.config.DesiredConfigCacheFile), 0o700); err != nil {
		return config.GetVersion(), err
	}
	temporary := runtime.config.DesiredConfigCacheFile + ".tmp"
	if err := os.WriteFile(temporary, payload, 0o600); err != nil {
		return config.GetVersion(), err
	}
	if err := os.Rename(temporary, runtime.config.DesiredConfigCacheFile); err != nil {
		_ = os.Remove(temporary)
		return config.GetVersion(), err
	}
	return config.GetVersion(), nil
}

func validateConfig(config Config) error {
	if config.ListenAddress == "" || config.ControllerAddress == "" || config.ControllerServerName == "" || config.EdgeID == "" || config.BootID == "" || config.SoftwareVersion == "" {
		return errors.New("Edge listen, Controller, identity, boot ID, and software version are required")
	}
	if _, err := securetransport.EdgeIdentityURI(config.EdgeID); err != nil {
		return err
	}
	r6Values := []string{config.TURNListenAddress, config.TURNPublicEndpoint, config.TURNRealm, config.UsageOutboxFile}
	configured := 0
	for _, value := range r6Values {
		if value != "" {
			configured++
		}
	}
	if configured != 0 && configured != len(r6Values) {
		return errors.New("TURN listener, public endpoint, realm, and usage outbox must be configured together")
	}
	return nil
}

func (runtime *Runtime) relayBroker() clientgateway.RelayBroker {
	if runtime.credentialDeriver == nil || runtime.usageOutbox == nil {
		return nil
	}
	return runtime
}

// RequestRelayLease 优先使用 Controller 最新决策；控制流离线时使用 AgentTicket 冻结的 Relay 委托。
func (runtime *Runtime) RequestRelayLease(ctx context.Context, request *cloudv1.RelayLeaseRequest) (*cloudv1.RelayICEConfig, error) {
	if runtime == nil || runtime.credentialDeriver == nil || runtime.RelayDegraded() {
		return nil, errors.New("Edge Relay control is unavailable")
	}
	if request == nil || strings.TrimSpace(request.GetSessionId()) == "" || strings.TrimSpace(request.GetAccountId()) == "" || strings.TrimSpace(request.GetDaemonId()) == "" || strings.TrimSpace(request.GetClientId()) == "" {
		return nil, errors.New("Relay lease request is incomplete")
	}
	if runtime.Ready() {
		material, err := runtime.requestControllerRelayLease(ctx, request)
		if err == nil {
			return material, nil
		}
		if runtime.Ready() {
			return nil, err
		}
	}
	return runtime.issueDelegatedRelayLease(ctx, request)
}

func (runtime *Runtime) requestControllerRelayLease(ctx context.Context, request *cloudv1.RelayLeaseRequest) (*cloudv1.RelayICEConfig, error) {
	runtime.controlSessionMu.RLock()
	session := runtime.controlSession
	runtime.controlSessionMu.RUnlock()
	if session == nil {
		return nil, errors.New("EdgeControl has no ready generation")
	}
	decision, err := session.RequestRelayLease(ctx, request)
	if err != nil {
		return nil, err
	}
	if denied := decision.GetDenied(); denied != nil {
		return nil, fmt.Errorf("Relay lease denied (%s): %s", denied.GetCode(), denied.GetMessage())
	}
	claims, err := ticket.VerifyRelayLease(decision.GetLease(), runtime.currentTicketKeys(), runtime.config.EdgeID, request.GetSessionId(), time.Now().UTC(), 30*time.Second)
	if err != nil {
		return nil, err
	}
	if claims.GetAccountId() != request.GetAccountId() || claims.GetDaemonId() != request.GetDaemonId() || claims.GetClientId() != request.GetClientId() {
		return nil, errors.New("RelayLease identity does not match the accepted client session")
	}
	material, err := runtime.credentialDeriver.Material(claims)
	if err != nil {
		return nil, err
	}
	if err := runtime.state.RegisterRelayLease(ctx, claims, material); err != nil {
		return nil, err
	}
	return material, nil
}

func (runtime *Runtime) issueDelegatedRelayLease(ctx context.Context, request *cloudv1.RelayLeaseRequest) (*cloudv1.RelayICEConfig, error) {
	agent, err := runtime.state.AuthenticatedAgentClaims(ctx, request.GetDaemonId())
	if err != nil || agent.GetAccountId() != request.GetAccountId() || agent.GetEdgeId() != runtime.config.EdgeID {
		return nil, errors.New("authenticated daemon Relay delegation is unavailable")
	}
	delegation := agent.GetRelayDelegation()
	if delegation == nil || delegation.GetMaxBytesPerLease() == 0 || delegation.GetMaxRateBytesPerSecond() == 0 || delegation.GetMaxConcurrentAllocations() == 0 {
		return nil, errors.New("authenticated daemon is not delegated Relay access")
	}
	now := time.Now().UTC()
	claims := &cloudv1.RelayLeaseClaims{
		LeaseId: uuid.NewString(), AccountId: request.GetAccountId(), EdgeId: runtime.config.EdgeID, DaemonId: request.GetDaemonId(), ClientId: request.GetClientId(), SessionId: request.GetSessionId(),
		MaxBytes: delegation.GetMaxBytesPerLease(), MaxRateBytesPerSecond: delegation.GetMaxRateBytesPerSecond(), MaxConcurrentAllocations: delegation.GetMaxConcurrentAllocations(),
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(5 * time.Minute)),
	}
	material, err := runtime.credentialDeriver.Material(claims)
	if err != nil {
		return nil, err
	}
	if err := runtime.state.RegisterRelayLease(ctx, claims, material); err != nil {
		return nil, err
	}
	return material, nil
}

// CloseRelaySession 把 ClientGateway session 生命周期绑定到同进程 TURN allocations。
func (runtime *Runtime) CloseRelaySession(ctx context.Context, sessionID string) error {
	if runtime == nil || runtime.relayServer == nil {
		return nil
	}
	return runtime.relayServer.CloseSessionAllocations(ctx, sessionID)
}

func (runtime *Runtime) setControlSession(session *controllerlink.Session) {
	runtime.controlSessionMu.Lock()
	runtime.controlSession = session
	runtime.controlSessionMu.Unlock()
	if session != nil && runtime.usageOutbox != nil {
		go runtime.runUsagePump(session)
	}
}

func (runtime *Runtime) runUsagePump(session *controllerlink.Session) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(runtime.ctx, 5*time.Second)
		err := runtime.flushUsageWithSession(ctx, session)
		cancel()
		if err != nil {
			select {
			case <-runtime.ctx.Done():
				return
			case <-session.Done():
				return
			case <-time.After(time.Second):
			}
		}
		select {
		case <-runtime.ctx.Done():
			return
		case <-session.Done():
			return
		case <-ticker.C:
		}
	}
}

func (runtime *Runtime) flushUsage(ctx context.Context) error {
	runtime.controlSessionMu.RLock()
	session := runtime.controlSession
	runtime.controlSessionMu.RUnlock()
	if session == nil {
		return errors.New("EdgeControl is unavailable for usage flush")
	}
	return runtime.flushUsageWithSession(ctx, session)
}

func (runtime *Runtime) flushUsageWithSession(ctx context.Context, session *controllerlink.Session) error {
	runtime.usagePumpMu.Lock()
	defer runtime.usagePumpMu.Unlock()
	if runtime.usageOutbox == nil {
		return nil
	}
	events, err := runtime.usageOutbox.Batch(128)
	if err != nil || len(events) == 0 {
		return err
	}
	ack, err := session.CommitUsageBatch(ctx, &cloudv1.UsageBatch{BatchId: uuid.NewString(), Events: events})
	if err != nil {
		return err
	}
	known := make(map[string]struct{}, len(events))
	for _, event := range events {
		known[event.GetEventId()] = struct{}{}
	}
	for _, eventID := range ack.GetEventIds() {
		if _, exists := known[eventID]; !exists {
			return errors.New("Controller UsageAck contains an event outside the sent batch")
		}
	}
	return runtime.usageOutbox.Ack(ack.GetEventIds())
}
