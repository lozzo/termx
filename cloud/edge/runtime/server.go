// Package runtime 组装单个 AnyTTY Cloud Edge 进程的公网 listener、健康状态和 ControllerLink。
package runtime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
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
	edgebindingkeys "github.com/anytty/anytty/cloud/edge/bindingkeys"
	edgecertificate "github.com/anytty/anytty/cloud/edge/certificate"
	"github.com/anytty/anytty/cloud/edge/clientgateway"
	"github.com/anytty/anytty/cloud/edge/controllerlink"
	"github.com/anytty/anytty/cloud/edge/policy"
	"github.com/anytty/anytty/cloud/edge/relay"
	"github.com/anytty/anytty/cloud/edge/reservation"
	"github.com/anytty/anytty/cloud/processhealth"
	"github.com/anytty/anytty/cloud/relayquota"
	"github.com/anytty/anytty/cloud/securetransport"
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
	RelayJournalFile            string
	BindingKeyBundleCacheFile   string
}

type relayLifecycle interface {
	Address() string
	Degraded() bool
	Close(context.Context) error
	CloseSessionAllocations(context.Context, string) error
	StateCloseSafe() bool
}

type relayControlSession interface {
	Done() <-chan struct{}
	ReserveRelay(context.Context, *cloudv1.RelayReserveRequest) (*cloudv1.RelayReserveResponse, error)
	RenewRelay(context.Context, *cloudv1.RelayRenewRequest) (*cloudv1.RelayRenewResponse, error)
	SettleRelay(context.Context, *cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error)
	QueryRelay(context.Context, *cloudv1.RelayQueryRequest) (*cloudv1.RelayQueryResponse, error)
}

type daemonStateControlSession interface {
	ResolveDaemonState(context.Context, string) (*cloudv1.DaemonStateRecord, bool, error)
}

var errRelayReplayConsumed = errors.New("Relay replay record was consumed")

type relayReplayConsumedError struct{ message string }

func (err relayReplayConsumedError) Error() string {
	if err.message == "" {
		return "Controller rejected Relay reservation"
	}
	return err.message
}
func (relayReplayConsumedError) Unwrap() error { return errRelayReplayConsumed }

// Runtime 拥有 Edge 公网 listener、唯一 ControllerLink 和唯一内存 State actor。
type Runtime struct {
	config              Config
	publicTLS           *tls.Config
	controllerTLS       *tls.Config
	listener            net.Listener
	httpServer          *http.Server
	grpcServer          *grpc.Server
	grpcHealth          *grpc_health.Server
	health              *processhealth.State
	errors              chan error
	readyChanges        chan struct{}
	ctx                 context.Context
	cancel              context.CancelFunc
	waitGroup           sync.WaitGroup
	shutdownGateInit    sync.Mutex
	shutdownGate        chan struct{}
	teardownStarted     bool
	stateDone           chan struct{}
	grpcStopped         bool
	httpDone            bool
	workersDone         chan struct{}
	shutdownComplete    bool
	shutdownErr         error
	state               *State
	configPublicKey     ed25519.PublicKey
	certificateManager  *edgecertificate.Manager
	bindingKeys         *edgebindingkeys.Store
	bindingKeyChanges   chan struct{}
	controllerConnected atomic.Bool
	credentialDeriver   *policy.CredentialDeriver
	relayJournal        *reservation.Journal
	relayServer         relayLifecycle
	controlSessionMu    sync.RWMutex
	controlSession      relayControlSession
	replayMu            sync.Mutex
	replayRunMu         sync.Mutex
	relayOperationLocks [256]sync.Mutex
	replayGate          sync.Mutex
	replayClosing       bool
	replayWait          sync.WaitGroup
	replayDone          chan struct{}
	beforeReplayLock    func(string)
	relayDegraded       atomic.Bool
	shuttingDown        atomic.Bool
}

// Start 先启动固定 HTTPS /healthz，再在后台建立 mTLS EdgeControl。
// Controller 暂时不可达时进程保持 alive 但 ready=false，不开启任何收费会话。
func Start(parent context.Context, config Config) (*Runtime, error) {
	config = normalizeConfig(config)
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	bindingKeyStore, err := edgebindingkeys.Open(config.BindingKeyBundleCacheFile)
	if err != nil {
		return nil, fmt.Errorf("load binding key bundle cache: %w", err)
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
	state, err := NewState(StateConfig{MailboxSize: 1024, DeltaBuffer: 4096, MaxAgents: 4096, MaxSessions: 4096, MaxPendingSignals: 4096})
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
	grpcServer := grpc.NewServer(grpc.MaxRecvMsgSize(1024*1024), grpc.MaxSendMsgSize(1024*1024), grpc.MaxConcurrentStreams(256))
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
		bindingKeys:        bindingKeyStore,
		bindingKeyChanges:  make(chan struct{}, 1),
	}
	if config.TURNListenAddress != "" {
		journal, openErr := reservation.Open(config.RelayJournalFile, 2*time.Second)
		if openErr != nil {
			// Journal 损坏或不可写只关闭收费 Relay；P2P 信令仍可运行。
			runtime.relayDegraded.Store(true)
		} else {
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				_ = journal.Close()
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
				_ = journal.Close()
				_ = listener.Close()
				state.Close()
				cancel()
				return nil, err
			}
			runtime.relayJournal, runtime.credentialDeriver = journal, deriver
		}
	}
	agentService, err := agentgateway.NewService(agentgateway.Config{
		EdgeID: config.EdgeID, EdgeBootID: config.BootID, Runtime: runtime,
		VerificationKeys: runtime.bindingKeys.VerificationKeys, Heartbeat: 10 * time.Second, HeartbeatTimeout: 30 * time.Second,
	})
	if err != nil {
		_ = listener.Close()
		if runtime.relayJournal != nil {
			_ = runtime.relayJournal.Close()
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
		if runtime.relayJournal != nil {
			_ = runtime.relayJournal.Close()
		}
		state.Close()
		cancel()
		return nil, err
	}
	cloudv1.RegisterClientGatewayServer(grpcServer, clientService)
	if runtime.relayJournal != nil {
		relayServer, relayErr := relay.Start(relay.Config{ListenAddress: config.TURNListenAddress, PublicEndpoint: config.TURNPublicEndpoint, Realm: config.TURNRealm, Runtime: state})
		if relayErr != nil {
			_ = listener.Close()
			_ = runtime.relayJournal.Close()
			state.Close()
			cancel()
			return nil, relayErr
		}
		runtime.relayServer = relayServer
	}
	runtime.httpServer = &http.Server{Handler: runtime, ReadHeaderTimeout: 5 * time.Second, TLSConfig: publicTLS}
	runtime.waitGroup.Add(3)
	go runtime.servePublic()
	go runtime.runControllerLink()
	go runtime.monitorBindingKeyValidity()
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

// RelayDegraded 表示 reservation journal 不可用或 Relay 数据面已发生不可继续授权的错误。
// 该状态只关闭新 Relay 分配，不影响同一 Edge 上的 P2P 信令和健康存活。
func (runtime *Runtime) RelayDegraded() bool {
	if runtime == nil {
		return false
	}
	return runtime.relayDegraded.Load() || (runtime.relayServer != nil && runtime.relayServer.Degraded())
}

// RelayJournalDepth returns the bounded count of unsettled durable reservations.
func (runtime *Runtime) RelayJournalDepth() (int, error) {
	if runtime == nil {
		return 0, nil
	}
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	if runtime.relayJournal == nil {
		return 0, nil
	}
	return runtime.relayJournal.Len()
}

// Ready requires both an accepted Controller generation and a currently usable binding key bundle.
func (runtime *Runtime) Ready() bool {
	return !runtime.shuttingDown.Load() && runtime.ControllerConnected() && runtime.BindingKeysUsable()
}

// ControllerConnected reports whether the current EdgeControl generation completed snapshot synchronization.
func (runtime *Runtime) ControllerConnected() bool { return runtime.controllerConnected.Load() }

// BindingKeysUsable reports whether AgentGateway admission can use the current persisted bundle.
func (runtime *Runtime) BindingKeysUsable() bool { return runtime.bindingKeys.Usable(time.Now().UTC()) }

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

// AttachSession atomically submits admission, Presence and its cancellation owner.
func (runtime *Runtime) AttachSession(ctx context.Context, session *cloudv1.ClientSessionSummary, closeSession func()) error {
	return runtime.state.AttachSession(ctx, session, closeSession)
}

func (runtime *Runtime) AttachAuthenticatedAgent(ctx context.Context, agent *cloudv1.AgentPresence, claims *cloudv1.DaemonBindingClaims, send func(*cloudv1.EdgeCommand) bool, closeWriter func()) (uint64, *cloudv1.DaemonStateRecord, error) {
	return runtime.state.AttachAuthenticatedAgent(ctx, agent, claims, send, closeWriter)
}

func (runtime *Runtime) DetachAgent(ctx context.Context, daemonID string, generation uint64) error {
	return runtime.state.DetachAgent(ctx, daemonID, generation)
}

func (runtime *Runtime) ResolveAgentSignal(ctx context.Context, daemonID string, generation uint64, event *cloudv1.AgentEvent) error {
	return runtime.state.ResolveAgentSignal(ctx, daemonID, generation, event)
}

func (runtime *Runtime) ApplyDaemonLifecycleResult(ctx context.Context, daemonID string, generation uint64, result *cloudv1.DaemonLifecycleResult) error {
	return runtime.state.ApplyDaemonLifecycleResult(ctx, daemonID, generation, result)
}

func (runtime *Runtime) ResolveDaemonState(ctx context.Context, daemonID string) (*cloudv1.DaemonStateRecord, error) {
	record, err := runtime.state.DaemonState(ctx, daemonID)
	if err == nil || !errors.Is(err, ErrDaemonStateUnavailable) {
		return record, err
	}
	session := runtime.currentControlSession()
	if session == nil {
		return nil, ErrDaemonStateUnavailable
	}
	stateSession, ok := session.(daemonStateControlSession)
	if !ok {
		return nil, ErrDaemonStateUnavailable
	}
	record, found, err := stateSession.ResolveDaemonState(ctx, daemonID)
	if err != nil || !found || record == nil {
		return nil, ErrDaemonStateUnavailable
	}
	if err := runtime.state.ApplyDaemonStateDelta(ctx, &cloudv1.DaemonStateDelta{Daemon: record}); err != nil {
		return nil, err
	}
	return runtime.state.DaemonState(ctx, daemonID)
}

// ServeHTTP 在同一 TLS listener 上路由 gRPC health 和固定 HTTP 健康路径。
func (runtime *Runtime) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.ProtoMajor == 2 && strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/grpc") {
		runtime.grpcServer.ServeHTTP(writer, request)
		return
	}
	if request.URL.Path == "/readyz" {
		controllerConnected := runtime.ControllerConnected()
		bindingKeysUsable := runtime.BindingKeysUsable()
		ready := controllerConnected && bindingKeysUsable
		statusText := "ready"
		if !controllerConnected {
			statusText = "controller_disconnected"
		} else if !bindingKeysUsable {
			statusText = "binding_keys_unusable"
		}
		writer.Header().Set("Content-Type", "application/json")
		code := http.StatusOK
		if !ready {
			code = http.StatusServiceUnavailable
		}
		writer.WriteHeader(code)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"ok": ready, "status": statusText, "controller_connected": controllerConnected, "binding_keys_usable": bindingKeysUsable,
		})
		return
	}
	runtime.health.ServeHTTP(writer, request)
}

// Shutdown 先停止新 Relay authority，把现存 allocation 冻结为 durable aggregate，
// 再取消 ControllerLink、关闭公网 listener 并等待有界 goroutine 退出。
func (runtime *Runtime) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("Runtime shutdown requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	shutdownGate := runtime.shutdownGateChannel()
	select {
	case shutdownGate <- struct{}{}:
		defer func() { <-shutdownGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if runtime.shutdownComplete {
		return runtime.shutdownErr
	}

	if !runtime.teardownStarted {
		runtime.shuttingDown.Store(true)
		runtime.setControllerConnected(false)
		runtime.replayGate.Lock()
		runtime.replayClosing = true
		runtime.replayGate.Unlock()
		if err := runtime.waitRelayReplay(ctx); err != nil {
			return runtime.recordShutdownFailure(ctx, err)
		}
		closingRecords, err := runtime.prepareRelayShutdown(ctx)
		if err != nil {
			return runtime.recordShutdownFailure(ctx, err)
		}
		if runtime.relayServer != nil {
			relayErr := runtime.relayServer.Close(ctx)
			runtime.rememberShutdownError(relayErr)
			closeSafe := runtime.relayServer.StateCloseSafe()
			contextErr := shutdownContextError(ctx, relayErr)
			if contextErr != nil || !closeSafe {
				var currentErr error
				if !closeSafe {
					currentErr = errors.New("Relay shutdown did not reach a close-safe state")
				}
				return errors.Join(runtime.shutdownErr, contextErr, currentErr)
			}
		}
		if err := runtime.finishRelayShutdown(ctx, closingRecords); err != nil {
			return runtime.recordShutdownFailure(ctx, err)
		}

		runtime.teardownStarted = true
		runtime.cancel()
		runtime.stateDone = make(chan struct{})
		go func() {
			if runtime.state != nil {
				runtime.state.Close()
			}
			close(runtime.stateDone)
		}()
		runtime.workersDone = make(chan struct{})
		go func() {
			runtime.waitGroup.Wait()
			close(runtime.workersDone)
		}()
	}

	if err := ctx.Err(); err != nil {
		return errors.Join(runtime.shutdownErr, err)
	}
	select {
	case <-runtime.stateDone:
	case <-ctx.Done():
		return errors.Join(runtime.shutdownErr, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(runtime.shutdownErr, err)
	}

	if !runtime.grpcStopped {
		err := stopGRPC(ctx, runtime.grpcServer)
		runtime.grpcStopped = true
		if err != nil {
			return runtime.recordShutdownFailure(ctx, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(runtime.shutdownErr, err)
	}

	if !runtime.httpDone {
		if runtime.httpServer != nil {
			if err := runtime.httpServer.Shutdown(ctx); err != nil {
				return runtime.recordShutdownFailure(ctx, err)
			}
		}
		runtime.httpDone = true
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(runtime.shutdownErr, err)
	}
	select {
	case <-runtime.workersDone:
	case <-ctx.Done():
		return errors.Join(runtime.shutdownErr, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(runtime.shutdownErr, err)
	}

	runtime.replayMu.Lock()
	if runtime.relayJournal != nil {
		runtime.rememberShutdownError(runtime.relayJournal.Close())
		runtime.relayJournal = nil
	}
	runtime.replayMu.Unlock()
	runtime.health.SetAlive(false)
	runtime.shutdownComplete = true
	return runtime.shutdownErr
}

func (runtime *Runtime) shutdownGateChannel() chan struct{} {
	runtime.shutdownGateInit.Lock()
	defer runtime.shutdownGateInit.Unlock()
	if runtime.shutdownGate == nil {
		runtime.shutdownGate = make(chan struct{}, 1)
	}
	return runtime.shutdownGate
}

func (runtime *Runtime) recordShutdownFailure(ctx context.Context, err error) error {
	runtime.rememberShutdownError(err)
	return errors.Join(runtime.shutdownErr, shutdownContextError(ctx, err))
}

func (runtime *Runtime) rememberShutdownError(err error) {
	if persistent := nonContextShutdownError(err); persistent != nil {
		runtime.shutdownErr = errors.Join(runtime.shutdownErr, persistent)
	}
}

func shutdownContextError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return nil
}

func nonContextShutdownError(err error) error {
	if err == nil || err == context.Canceled || err == context.DeadlineExceeded {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := make([]error, 0, len(joined.Unwrap()))
		for _, part := range joined.Unwrap() {
			if persistent := nonContextShutdownError(part); persistent != nil {
				parts = append(parts, persistent)
			}
		}
		return errors.Join(parts...)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		child := wrapped.Unwrap()
		if errors.Is(child, context.Canceled) || errors.Is(child, context.DeadlineExceeded) {
			return nonContextShutdownError(child)
		}
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

func (runtime *Runtime) waitRelayReplay(ctx context.Context) error {
	runtime.replayGate.Lock()
	if runtime.replayDone == nil {
		// Shutdown sets replayClosing under replayGate before reaching this point,
		// so the WaitGroup cannot receive another Add after this waiter starts.
		runtime.replayDone = make(chan struct{})
		done := runtime.replayDone
		go func() {
			runtime.replayWait.Wait()
			close(done)
		}()
	}
	done := runtime.replayDone
	runtime.replayGate.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (runtime *Runtime) prepareRelayShutdown(ctx context.Context) ([]*cloudv1.RelayJournalRecord, error) {
	if runtime.relayJournal == nil {
		return nil, nil
	}
	records, err := runtime.journalRecords(reservation.MaxDurableRecords)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		switch record.GetStage() {
		case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED,
			cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING,
			cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_CLOSING:
			grant := record.GetGrant()
			if grant == nil {
				return nil, errors.New("Relay journal grant is missing during shutdown")
			}
			if err := runtime.state.BeginRelaySessionClose(ctx, grant.GetSessionId()); err != nil {
				return nil, err
			}
			if err := runtime.journalMarkClosing(grant.GetReservationId()); err != nil {
				return nil, err
			}
		}
	}
	return records, nil
}

func (runtime *Runtime) finishRelayShutdown(ctx context.Context, records []*cloudv1.RelayJournalRecord) error {
	session := runtime.currentControlSession()
	for _, record := range records {
		var settlement *cloudv1.RelaySettlement
		grant := record.GetGrant()
		switch record.GetStage() {
		case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_HELD_UNEXPOSED:
			settlement = exactZeroSettlement(grant, time.Now().UTC())
		case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED,
			cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING,
			cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_CLOSING:
			var err error
			settlement, err = runtime.state.RelaySessionSettlement(ctx, grant.GetSessionId(), time.Now().UTC())
			if err != nil || settlement == nil {
				return errors.Join(err, errors.New("Relay group did not become static during shutdown"))
			}
		case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_SETTLEMENT_DURABLE:
			settlement = record.GetSettlement()
		default:
			// REQUESTED remains uncertain until the next ready Controller generation.
			continue
		}
		if settlement == nil {
			return errors.New("Relay shutdown settlement is missing")
		}
		if record.GetStage() != cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_SETTLEMENT_DURABLE {
			if err := runtime.journalPutSettlement(settlement); err != nil {
				return err
			}
		}
		if grant != nil {
			if live, err := runtime.state.RelayReservationLive(ctx, grant.GetReservationId()); err != nil {
				return err
			} else if live {
				if err := runtime.state.ForgetRelayGroup(ctx, grant.GetReservationId()); err != nil {
					return err
				}
			}
		}
		if session != nil {
			// The durable fact is sufficient for shutdown safety; a lost ACK is replayed.
			_ = runtime.deliverSettlement(ctx, session, settlement)
		}
	}
	return nil
}

func stopGRPC(ctx context.Context, server *grpc.Server) error {
	if server == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		server.Stop()
		<-done
		return ctx.Err()
	}
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
		capabilities := []cloudv1.EdgeCapability{cloudv1.EdgeCapability_EDGE_CAPABILITY_CONTROL_STREAM, cloudv1.EdgeCapability_EDGE_CAPABILITY_DAEMON_LIFECYCLE_POLICY, cloudv1.EdgeCapability_EDGE_CAPABILITY_DAEMON_EDGE_RESELECTION}
		capabilities = append(capabilities, cloudv1.EdgeCapability_EDGE_CAPABILITY_CERTIFICATE_HOT_RELOAD)
		if runtime.relayServer != nil && runtime.relayJournal != nil && !runtime.RelayDegraded() {
			capabilities = append(capabilities, cloudv1.EdgeCapability_EDGE_CAPABILITY_RELAY, cloudv1.EdgeCapability_EDGE_CAPABILITY_RESERVATION_JOURNAL)
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
			ApplyDesiredConfig:       applyDesiredConfig,
			ApplyBindingKeyBundle:    runtime.applyBindingKeyBundle,
			ApplyDaemonStateSnapshot: runtime.state.ApplyDaemonStateSnapshot,
			ApplyDaemonStateDelta:    runtime.state.ApplyDaemonStateDelta,
			ApplyCertificate:         runtime.certificateManager.Apply,
			CloseDaemon:              runtime.state.CloseAgentConnection,
			CloseSession:             runtime.state.CloseSession,
			ReselectDaemonEdge: func(ctx context.Context, daemonID string, generation, preferenceRevision uint64) error {
				return runtime.state.SendAgentCommand(ctx, daemonID, generation, &cloudv1.EdgeCommand{Payload: &cloudv1.EdgeCommand_EdgeReselect{EdgeReselect: &cloudv1.DaemonEdgeReselectCommand{AgentGeneration: generation, PreferenceRevision: preferenceRevision}}})
			},
		})
		if err == nil {
			if runtime.ctx.Err() != nil {
				_ = session.Close()
				return
			}
			if err = session.WaitReady(runtime.ctx); err != nil {
				_ = session.Close()
				_ = runtime.state.InvalidateDaemonStates(context.Background())
			} else {
				runtime.setControlSession(session)
				runtime.setControllerConnected(true)
				delay = 100 * time.Millisecond
				err = session.Wait()
				runtime.setControlSession(nil)
				_ = session.Close()
				_ = runtime.state.InvalidateDaemonStates(context.Background())
				runtime.setControllerConnected(false)
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

func (runtime *Runtime) applyBindingKeyBundle(bundle *cloudv1.KeyBundle) error {
	if err := runtime.bindingKeys.Update(bundle); err != nil {
		return err
	}
	select {
	case runtime.bindingKeyChanges <- struct{}{}:
	default:
	}
	runtime.refreshReadiness()
	return nil
}

func (runtime *Runtime) setControllerConnected(value bool) {
	runtime.controllerConnected.Store(value)
	runtime.refreshReadiness()
}

func (runtime *Runtime) refreshReadiness() {
	value := runtime.Ready()
	changed := runtime.health.Ready() != value
	runtime.health.SetReady(value)
	status := grpc_health_v1.HealthCheckResponse_NOT_SERVING
	if value {
		status = grpc_health_v1.HealthCheckResponse_SERVING
	}
	runtime.grpcHealth.SetServingStatus("", status)
	if !changed {
		return
	}
	select {
	case runtime.readyChanges <- struct{}{}:
	default:
	}
}

func (runtime *Runtime) monitorBindingKeyValidity() {
	defer runtime.waitGroup.Done()
	for {
		var deadline <-chan time.Time
		var timer *time.Timer
		if bundle := runtime.bindingKeys.Bundle(); bundle != nil {
			now := time.Now().UTC()
			wakeAt := bundle.GetExpiresAt().AsTime()
			if now.Before(bundle.GetIssuedAt().AsTime()) {
				wakeAt = bundle.GetIssuedAt().AsTime()
			}
			if wakeAt.After(now) {
				timer = time.NewTimer(wakeAt.Sub(now))
				deadline = timer.C
			}
		}
		select {
		case <-runtime.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-runtime.bindingKeyChanges:
			if timer != nil {
				timer.Stop()
			}
		case <-deadline:
			runtime.refreshReadiness()
		}
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
	config.RelayJournalFile = strings.TrimSpace(config.RelayJournalFile)
	config.BindingKeyBundleCacheFile = strings.TrimSpace(config.BindingKeyBundleCacheFile)
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
	if config.ListenAddress == "" || config.ControllerAddress == "" || config.ControllerServerName == "" || config.EdgeID == "" || config.BootID == "" || config.SoftwareVersion == "" || config.BindingKeyBundleCacheFile == "" {
		return errors.New("Edge listen, Controller, identity, boot ID, software version, and binding key bundle cache are required")
	}
	if _, err := securetransport.EdgeIdentityURI(config.EdgeID); err != nil {
		return err
	}
	r6Values := []string{config.TURNListenAddress, config.TURNPublicEndpoint, config.TURNRealm, config.RelayJournalFile}
	configured := 0
	for _, value := range r6Values {
		if value != "" {
			configured++
		}
	}
	if configured != 0 && configured != len(r6Values) {
		return errors.New("TURN listener, public endpoint, realm, and reservation journal must be configured together")
	}
	return nil
}

func (runtime *Runtime) relayBroker() clientgateway.RelayBroker {
	if runtime.credentialDeriver == nil || runtime.relayJournal == nil || runtime.RelayDegraded() {
		return nil
	}
	return runtime
}

func (runtime *Runtime) RequestRelay(ctx context.Context, identity *clientgateway.RelayRequest) (*cloudv1.RelayICEConfig, error) {
	if runtime == nil || runtime.credentialDeriver == nil || runtime.relayJournal == nil || runtime.RelayDegraded() || identity == nil ||
		strings.TrimSpace(identity.SessionID) == "" || strings.TrimSpace(identity.AccountID) == "" || strings.TrimSpace(identity.DaemonID) == "" || strings.TrimSpace(identity.ClientID) == "" {
		return nil, errors.New("Edge Relay control is unavailable or request identity is incomplete")
	}
	if runtime.shuttingDown.Load() || !runtime.ControllerConnected() {
		return nil, errors.New("ready Controller generation is unavailable for Relay reservation")
	}
	session := runtime.currentControlSession()
	if session == nil {
		return nil, errors.New("ready Controller generation is unavailable for Relay reservation")
	}
	now := time.Now().UTC()
	request := &cloudv1.RelayReserveRequest{
		ReservationId: uuid.NewString(), AccountId: strings.TrimSpace(identity.AccountID), DaemonId: strings.TrimSpace(identity.DaemonID),
		ClientId: strings.TrimSpace(identity.ClientID), SessionId: strings.TrimSpace(identity.SessionID), ObservedAt: timestamppb.New(now),
	}
	digest, err := relayquota.ReserveRequestDigest(request)
	if err != nil {
		return nil, err
	}
	request.RequestDigest = digest
	operationLock := runtime.relayOperationLock(request.GetReservationId())
	operationLock.Lock()
	defer operationLock.Unlock()
	if err := runtime.journalCreateRequested(request); err != nil {
		return nil, err
	}
	response, err := session.ReserveRelay(ctx, request)
	if err != nil {
		runtime.startRelayReplay(session)
		return nil, err
	}
	grant, err := runtime.acceptReserveResponse(request, response)
	if err != nil {
		return nil, err
	}
	material, err := runtime.credentialDeriver.Material(grant)
	if err != nil {
		return nil, runtime.abandonUnexposed(session, grant, err)
	}
	if err := runtime.state.RegisterRelayGrant(ctx, grant, material); err != nil {
		return nil, runtime.abandonUnexposed(session, grant, err)
	}
	// This is the last durable transition before credentials may leave the Edge.
	if err := runtime.journalMarkExposed(grant.GetReservationId()); err != nil {
		_ = runtime.state.BeginRelaySessionClose(context.Background(), grant.GetSessionId())
		_ = runtime.state.ForgetRelayGroup(context.Background(), grant.GetReservationId())
		return nil, runtime.abandonUnexposed(session, grant, err)
	}
	return material, nil
}

func (runtime *Runtime) RenewRelay(ctx context.Context, current *cloudv1.RelayICEConfig) (*cloudv1.RelayICEConfig, error) {
	if runtime == nil || current == nil || strings.TrimSpace(current.GetReservationId()) == "" || strings.TrimSpace(current.GetUsername()) == "" {
		return nil, errors.New("current Relay reservation credential is required")
	}
	if runtime.shuttingDown.Load() || !runtime.ControllerConnected() {
		return nil, errors.New("ready Controller generation is unavailable for Relay renewal")
	}
	operationLock := runtime.relayOperationLock(current.GetReservationId())
	operationLock.Lock()
	defer operationLock.Unlock()
	session := runtime.currentControlSession()
	if session == nil {
		return nil, errors.New("ready Controller generation is unavailable for Relay renewal")
	}
	record, exists, err := runtime.journalRecord(current.GetReservationId())
	if err != nil || !exists || record.GetGrant() == nil || record.GetGrant().GetReservationId() != current.GetReservationId() {
		return nil, errors.New("durable Relay reservation is unavailable for renewal")
	}
	sequence := record.GetGrant().GetRenewSequence() + 1
	if record.GetStage() == cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING {
		sequence = record.GetPendingRenewSequence()
	} else if err := runtime.journalMarkRenewPending(current.GetReservationId(), sequence); err != nil {
		return nil, err
	}
	request := &cloudv1.RelayRenewRequest{ReservationId: current.GetReservationId(), RenewSequence: sequence, PolicyDigest: append([]byte(nil), record.GetGrant().GetPolicyDigest()...), ObservedAt: timestamppb.Now()}
	response, err := session.RenewRelay(ctx, request)
	if err != nil {
		runtime.startRelayReplay(session)
		return nil, err
	}
	return runtime.acceptRenewResponse(ctx, current.GetUsername(), request, response)
}

// CloseRelaySession 把 ClientGateway session 生命周期绑定到同进程 TURN allocations。
func (runtime *Runtime) CloseRelaySession(ctx context.Context, sessionID string) (resultErr error) {
	if runtime == nil || strings.TrimSpace(sessionID) == "" || runtime.relayJournal == nil {
		return nil
	}
	defer func() {
		if resultErr != nil {
			runtime.startRelayReplay(runtime.currentControlSession())
		}
	}()
	record, err := runtime.journalRecordForSession(sessionID)
	if err != nil || record == nil {
		return err
	}
	reservationID := record.GetReserveRequest().GetReservationId()
	operationLock := runtime.relayOperationLock(reservationID)
	operationLock.Lock()
	defer operationLock.Unlock()
	record, exists, err := runtime.journalRecord(reservationID)
	if err != nil || !exists || record.GetReserveRequest().GetSessionId() != sessionID {
		return err
	}
	if record.GetStage() == cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_REQUESTED {
		if session := runtime.currentControlSession(); session != nil {
			runtime.startRelayReplay(session)
		}
		return nil
	}
	if record.GetStage() == cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_SETTLEMENT_DURABLE {
		if err := runtime.state.ForgetRelayGroup(ctx, record.GetGrant().GetReservationId()); err != nil {
			return err
		}
		return runtime.deliverSettlement(ctx, runtime.currentControlSession(), record.GetSettlement())
	}
	grant := record.GetGrant()
	if grant == nil {
		return errors.New("Relay journal grant is missing during close")
	}
	if record.GetStage() == cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_HELD_UNEXPOSED {
		settlement := exactZeroSettlement(grant, time.Now().UTC())
		if err := runtime.journalPutSettlement(settlement); err != nil {
			return err
		}
		return runtime.deliverSettlement(ctx, runtime.currentControlSession(), settlement)
	}
	if err := runtime.journalMarkClosing(grant.GetReservationId()); err != nil {
		return err
	}
	return runtime.finishClosingRelaySession(ctx, runtime.currentControlSession(), grant)
}

// finishClosingRelaySession runs under the reservation operation lock. CLOSING is
// already durable, so any failure can be retried by the existing journal replay.
func (runtime *Runtime) finishClosingRelaySession(ctx context.Context, session relayControlSession, grant *cloudv1.RelayGrant) error {
	if grant == nil {
		return errors.New("Relay journal grant is missing during close")
	}
	live, err := runtime.state.RelayReservationLive(ctx, grant.GetReservationId())
	if err != nil {
		return err
	}
	if !live {
		settlement := recoverySettlement(grant, time.Now().UTC())
		if err := runtime.journalPutSettlement(settlement); err != nil {
			return err
		}
		return runtime.deliverSettlement(ctx, session, settlement)
	}
	if err := runtime.state.BeginRelaySessionClose(ctx, grant.GetSessionId()); err != nil {
		return err
	}
	if runtime.relayServer != nil {
		if err := runtime.relayServer.CloseSessionAllocations(ctx, grant.GetSessionId()); err != nil {
			return err
		}
	}
	settlement, err := runtime.state.RelaySessionSettlement(ctx, grant.GetSessionId(), time.Now().UTC())
	if err != nil || settlement == nil {
		return errors.Join(err, errors.New("Relay group did not produce a static aggregate settlement"))
	}
	if err := runtime.journalPutSettlement(settlement); err != nil {
		return err
	}
	if err := runtime.state.ForgetRelayGroup(ctx, grant.GetReservationId()); err != nil {
		return err
	}
	return runtime.deliverSettlement(ctx, session, settlement)
}

func (runtime *Runtime) setControlSession(session relayControlSession) {
	runtime.controlSessionMu.Lock()
	runtime.controlSession = session
	runtime.controlSessionMu.Unlock()
	if session != nil {
		runtime.startRelayReplay(session)
	}
}

func (runtime *Runtime) currentControlSession() relayControlSession {
	runtime.controlSessionMu.RLock()
	defer runtime.controlSessionMu.RUnlock()
	return runtime.controlSession
}

func (runtime *Runtime) startRelayReplay(session relayControlSession) bool {
	if session == nil || runtime.relayJournal == nil {
		return false
	}
	runtime.replayGate.Lock()
	if runtime.replayClosing {
		runtime.replayGate.Unlock()
		return false
	}
	runtime.replayWait.Add(1)
	runtime.replayGate.Unlock()
	go func() {
		defer runtime.replayWait.Done()
		runtime.replayRelayJournal(session)
	}()
	return true
}

func (runtime *Runtime) replayRelayJournal(session relayControlSession) {
	runtime.replayRunMu.Lock()
	defer runtime.replayRunMu.Unlock()
	records, err := runtime.journalRecords(reservation.MaxDurableRecords)
	if err != nil {
		runtime.relayDegraded.Store(true)
		return
	}
	for _, record := range records {
		select {
		case <-runtime.ctx.Done():
			return
		case <-session.Done():
			return
		default:
		}
		ctx, cancel := context.WithTimeout(runtime.ctx, 5*time.Second)
		err = runtime.replayRelayRecord(ctx, session, record)
		cancel()
		if err != nil {
			return
		}
	}
}

func (runtime *Runtime) replayRelayRecord(ctx context.Context, session relayControlSession, record *cloudv1.RelayJournalRecord) error {
	if record == nil {
		return errors.New("Relay journal record is required for replay")
	}
	reservationID := record.GetReserveRequest().GetReservationId()
	if reservationID == "" {
		reservationID = record.GetGrant().GetReservationId()
	}
	if reservationID == "" {
		return errors.New("Relay journal record has no reservation identity")
	}
	if runtime.beforeReplayLock != nil {
		runtime.beforeReplayLock(reservationID)
	}
	operationLock := runtime.relayOperationLock(reservationID)
	operationLock.Lock()
	defer operationLock.Unlock()
	record, exists, err := runtime.journalRecord(reservationID)
	if err != nil || !exists {
		return err
	}
	grant := record.GetGrant()
	switch record.GetStage() {
	case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_REQUESTED:
		query, err := session.QueryRelay(ctx, &cloudv1.RelayQueryRequest{ReservationId: record.GetReserveRequest().GetReservationId()})
		if err != nil {
			return err
		}
		if query == nil || query.GetReservationId() != reservationID {
			return errors.New("Controller returned a Relay query replay with invalid identity")
		}
		if query.GetCode() == cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED {
			response, reserveErr := session.ReserveRelay(ctx, record.GetReserveRequest())
			if reserveErr != nil {
				return reserveErr
			}
			grant, err = runtime.acceptReserveResponse(record.GetReserveRequest(), response)
			if err != nil {
				if errors.Is(err, errRelayReplayConsumed) {
					return nil
				}
				return err
			}
		} else if query.GetTerminal() != nil && query.GetGrant() != nil {
			if query.GetGrant().GetReservationId() != reservationID || query.GetTerminal().GetReservationId() != reservationID {
				return errors.New("Controller returned a terminal Relay query replay with invalid identity")
			}
			if err := runtime.journalApplyGrant(query.GetGrant()); err != nil {
				return err
			}
			return runtime.journalAck(query.GetTerminal())
		} else if query.GetGrant() != nil {
			grant = query.GetGrant()
			if err := runtime.journalApplyGrant(grant); err != nil {
				return err
			}
		} else {
			return errors.New("Controller returned an invalid Relay query replay")
		}
		settlement := exactZeroSettlement(grant, time.Now().UTC())
		if err := runtime.journalPutSettlement(settlement); err != nil {
			return err
		}
		return runtime.deliverSettlement(ctx, session, settlement)
	case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_HELD_UNEXPOSED:
		settlement := exactZeroSettlement(grant, time.Now().UTC())
		if err := runtime.journalPutSettlement(settlement); err != nil {
			return err
		}
		return runtime.deliverSettlement(ctx, session, settlement)
	case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_EXPOSED:
		live, err := runtime.state.RelayReservationLive(ctx, grant.GetReservationId())
		if err != nil || live {
			return err
		}
	case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_RENEW_PENDING:
		live, err := runtime.state.RelayReservationLive(ctx, grant.GetReservationId())
		if err != nil {
			return err
		}
		if live {
			request := &cloudv1.RelayRenewRequest{ReservationId: grant.GetReservationId(), RenewSequence: record.GetPendingRenewSequence(), PolicyDigest: append([]byte(nil), grant.GetPolicyDigest()...), ObservedAt: timestamppb.Now()}
			response, err := session.RenewRelay(ctx, request)
			if err != nil {
				return err
			}
			_, err = runtime.acceptRenewResponse(ctx, "v2:"+grant.GetReservationId()+":"+grant.GetSessionId(), request, response)
			return err
		}
	case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_CLOSING:
		return runtime.finishClosingRelaySession(ctx, session, grant)
	case cloudv1.RelayJournalStage_RELAY_JOURNAL_STAGE_SETTLEMENT_DURABLE:
		if err := runtime.state.ForgetRelayGroup(ctx, grant.GetReservationId()); err != nil {
			return err
		}
		return runtime.deliverSettlement(ctx, session, record.GetSettlement())
	default:
		return errors.New("Relay journal contains an invalid stage")
	}
	settlement := recoverySettlement(grant, time.Now().UTC())
	if err := runtime.journalPutSettlement(settlement); err != nil {
		return err
	}
	return runtime.deliverSettlement(ctx, session, settlement)
}

func (runtime *Runtime) relayOperationLock(reservationID string) *sync.Mutex {
	const offset32 = uint32(2166136261)
	const prime32 = uint32(16777619)
	hash := offset32
	for index := 0; index < len(reservationID); index++ {
		hash ^= uint32(reservationID[index])
		hash *= prime32
	}
	return &runtime.relayOperationLocks[hash%uint32(len(runtime.relayOperationLocks))]
}

func (runtime *Runtime) acceptReserveResponse(request *cloudv1.RelayReserveRequest, response *cloudv1.RelayReserveResponse) (*cloudv1.RelayGrant, error) {
	if response == nil || response.GetReservationId() != request.GetReservationId() || !relayquota.EqualDigest(response.GetRequestDigest(), request.GetRequestDigest()) {
		return nil, errors.New("Controller Relay reserve response identity is invalid")
	}
	if response.GetCode() == cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED || response.GetCode() == cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT || response.GetCode() == cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_UNAVAILABLE {
		if err := runtime.journalDropRequested(request.GetReservationId(), request.GetRequestDigest()); err != nil {
			return nil, err
		}
		return nil, relayReplayConsumedError{message: response.GetErrorMessage()}
	}
	if response.GetTerminal() != nil {
		if response.GetGrant() == nil {
			return nil, errors.New("terminal Relay reserve replay omitted its grant")
		}
		if err := runtime.journalApplyGrant(response.GetGrant()); err != nil {
			return nil, err
		}
		if err := runtime.journalAck(response.GetTerminal()); err != nil {
			return nil, err
		}
		return nil, relayReplayConsumedError{message: "Relay reservation is already terminal"}
	}
	if response.GetGrant() == nil || (response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED && response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY) {
		return nil, errors.New("Controller Relay reserve response omitted a committed grant")
	}
	if err := runtime.journalApplyGrant(response.GetGrant()); err != nil {
		return nil, err
	}
	return response.GetGrant(), nil
}

func (runtime *Runtime) acceptRenewResponse(ctx context.Context, username string, request *cloudv1.RelayRenewRequest, response *cloudv1.RelayRenewResponse) (*cloudv1.RelayICEConfig, error) {
	if response == nil || response.GetReservationId() != request.GetReservationId() || response.GetRenewSequence() != request.GetRenewSequence() {
		return nil, errors.New("Controller Relay renewal response identity is invalid")
	}
	if response.GetTerminal() != nil {
		record, _, err := runtime.journalRecord(request.GetReservationId())
		if err != nil || record.GetGrant() == nil {
			return nil, errors.Join(err, errors.New("terminal Relay renewal lost its durable grant"))
		}
		if err := runtime.state.BeginRelaySessionClose(ctx, record.GetGrant().GetSessionId()); err != nil {
			return nil, err
		}
		if runtime.relayServer != nil {
			if err := runtime.relayServer.CloseSessionAllocations(ctx, record.GetGrant().GetSessionId()); err != nil {
				return nil, err
			}
		}
		if err := runtime.state.ForgetRelayGroup(ctx, request.GetReservationId()); err != nil {
			return nil, err
		}
		if err := runtime.journalAck(response.GetTerminal()); err != nil {
			return nil, err
		}
		return nil, errors.New("Relay reservation became terminal during renewal")
	}
	if response.GetGrant() == nil || (response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_APPLIED && response.GetCode() != cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REPLAY) {
		return nil, errors.New(response.GetErrorMessage())
	}
	if err := runtime.journalApplyRenewedGrant(response.GetGrant()); err != nil {
		return nil, err
	}
	return runtime.state.RenewRelayGrant(ctx, username, response.GetGrant())
}

func (runtime *Runtime) abandonUnexposed(session relayControlSession, grant *cloudv1.RelayGrant, cause error) error {
	settlement := exactZeroSettlement(grant, time.Now().UTC())
	if err := runtime.journalPutSettlement(settlement); err != nil {
		return errors.Join(cause, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := runtime.deliverSettlement(ctx, session, settlement)
	cancel()
	return errors.Join(cause, err)
}

func (runtime *Runtime) deliverSettlement(ctx context.Context, session relayControlSession, settlement *cloudv1.RelaySettlement) error {
	if session == nil {
		return errors.New("ready Controller generation is unavailable for Relay settlement")
	}
	ack, err := session.SettleRelay(ctx, settlement)
	if err != nil {
		return err
	}
	if ack.GetReservationId() != settlement.GetReservationId() || ack.GetCode() == cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_REJECTED || ack.GetCode() == cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_CONFLICT || ack.GetCode() == cloudv1.RelayResponseCode_RELAY_RESPONSE_CODE_UNAVAILABLE {
		return errors.New("Controller rejected Relay settlement: " + ack.GetErrorMessage())
	}
	return runtime.journalAck(ack)
}

func exactZeroSettlement(grant *cloudv1.RelayGrant, observedAt time.Time) *cloudv1.RelaySettlement {
	return &cloudv1.RelaySettlement{ReservationId: grant.GetReservationId(), Kind: cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT, PolicyDigest: append([]byte(nil), grant.GetPolicyDigest()...), ObservedAt: timestamppb.New(observedAt.UTC())}
}

func recoverySettlement(grant *cloudv1.RelayGrant, observedAt time.Time) *cloudv1.RelaySettlement {
	return &cloudv1.RelaySettlement{ReservationId: grant.GetReservationId(), Kind: cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_RECOVERY_MAX, PolicyDigest: append([]byte(nil), grant.GetPolicyDigest()...), ObservedAt: timestamppb.New(observedAt.UTC())}
}

func (runtime *Runtime) journalCreateRequested(request *cloudv1.RelayReserveRequest) error {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.CreateRequested(request)
}
func (runtime *Runtime) journalApplyGrant(grant *cloudv1.RelayGrant) error {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.ApplyGrant(grant)
}
func (runtime *Runtime) journalMarkExposed(id string) error {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.MarkExposed(id)
}
func (runtime *Runtime) journalMarkRenewPending(id string, sequence uint64) error {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.MarkRenewPending(id, sequence)
}
func (runtime *Runtime) journalApplyRenewedGrant(grant *cloudv1.RelayGrant) error {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.ApplyRenewedGrant(grant)
}
func (runtime *Runtime) journalMarkClosing(id string) error {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.MarkClosing(id)
}
func (runtime *Runtime) journalPutSettlement(settlement *cloudv1.RelaySettlement) error {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.PutSettlement(settlement)
}
func (runtime *Runtime) journalAck(ack *cloudv1.RelaySettlementAck) error {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.Ack(ack)
}
func (runtime *Runtime) journalDropRequested(id string, digest []byte) error {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.DropRequested(id, digest)
}
func (runtime *Runtime) journalRecord(id string) (*cloudv1.RelayJournalRecord, bool, error) {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.Record(id)
}
func (runtime *Runtime) journalRecords(limit int) ([]*cloudv1.RelayJournalRecord, error) {
	runtime.replayMu.Lock()
	defer runtime.replayMu.Unlock()
	return runtime.relayJournal.Records(limit)
}
func (runtime *Runtime) journalRecordForSession(sessionID string) (*cloudv1.RelayJournalRecord, error) {
	records, err := runtime.journalRecords(reservation.MaxDurableRecords)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.GetReserveRequest().GetSessionId() == sessionID {
			return record, nil
		}
	}
	return nil, nil
}
