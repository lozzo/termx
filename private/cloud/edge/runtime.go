// Package edge 装配单个 termx-cloud-edge 进程中的纯内存 Hub 与 Relay runtime。
//
// Hub policy/assignment 不落盘；Relay 只保留未确认 usage outbox。进程重启后新的 Hub
// 必须重新从 Controller 获取 full projection，新的 Relay Authority 不恢复 allocation。
package edge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudhub "github.com/lozzow/termx/private/cloud/hub"
	cloudrelay "github.com/lozzow/termx/private/cloud/relay"
	"github.com/lozzow/termx/proto/cloudpb"
)

// Config 是 Edge development composition 的显式配置。
type Config struct {
	ControllerURL                       string                          `json:"controller_url"`
	HubListen                           string                          `json:"hub_listen"`
	HealthListen                        string                          `json:"health_listen"`
	RelayListen                         string                          `json:"relay_listen"`
	RelayPublicIP                       string                          `json:"relay_public_ip"`
	UsageOutboxPath                     string                          `json:"usage_outbox_path"`
	Metadata                            *cloudpb.EdgeDeploymentMetadata `json:"metadata"`
	HubControlPrivateKeyBase64          string                          `json:"hub_control_private_key_base64"`
	RelayControlPrivateKeyBase64        string                          `json:"relay_control_private_key_base64"`
	ControllerProjectionKeyID           string                          `json:"controller_projection_key_id"`
	ControllerProjectionPublicKeyBase64 string                          `json:"controller_projection_public_key_base64"`
}

// Manifest 是 supervisor 和 E2E harness 使用的非秘密 Edge 进程描述。
type Manifest struct {
	PID                int    `json:"pid"`
	EdgeDeploymentID   string `json:"edge_deployment_id"`
	HubID              string `json:"hub_id"`
	RelayID            string `json:"relay_id"`
	HubURL             string `json:"hub_url"`
	HealthURL          string `json:"health_url"`
	RelayURL           string `json:"relay_url"`
	UsageOutboxPath    string `json:"usage_outbox_path"`
	ControlGeneration  uint64 `json:"control_generation"`
	ProjectionRevision uint64 `json:"projection_revision"`
}

// Runtime 持有 Edge listener、Hub/Relay owner 与 control client 生命周期。
type Runtime struct {
	config      Config
	projection  *cloudhub.Projection
	control     *cloudhub.ControlClient
	authorizer  *cloudhub.EdgeAuthorizer
	hub         *cloudhub.Service
	relay       *cloudrelay.Server
	usageOutbox *cloudrelay.UsageOutbox
	listeners   []net.Listener
	servers     []*http.Server
	cancel      context.CancelFunc
	errors      chan error
	closeOnce   sync.Once
	usageWG     sync.WaitGroup
}

// LoadConfig 读取 Edge JSON config，未知字段 fail closed。
func LoadConfig(path string) (Config, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode Edge config: %w", err)
	}
	return config, nil
}

// Start 创建 Hub/Relay runtime 并启动唯一 Hub control client。
func Start(config Config) (*Runtime, error) {
	if config.Metadata == nil || config.ControllerURL == "" || config.UsageOutboxPath == "" || config.ControllerProjectionKeyID == "" {
		return nil, fmt.Errorf("Edge metadata, Controller URL, usage outbox and projection key are required")
	}
	hubPrivateBytes, err := base64.RawStdEncoding.DecodeString(config.HubControlPrivateKeyBase64)
	if err != nil || len(hubPrivateBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Hub control private key")
	}
	controllerPublicBytes, err := base64.RawStdEncoding.DecodeString(config.ControllerProjectionPublicKeyBase64)
	if err != nil || len(controllerPublicBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid Controller projection public key")
	}
	now := time.Now().UTC()
	verificationKey := servicecredential.VerificationKey{ID: config.ControllerProjectionKeyID, PublicKey: ed25519.PublicKey(controllerPublicBytes), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour)}
	keyRing, err := servicecredential.NewKeyRing(verificationKey)
	if err != nil {
		return nil, err
	}
	authorizer, err := cloudhub.NewEdgeAuthorizer(cloudhub.EdgeAuthorizerConfig{HubID: config.Metadata.GetHubId(), Issuer: "termx-cloud-controller", KeyRing: keyRing, MaxStaleness: 30 * time.Minute})
	if err != nil {
		return nil, err
	}
	fence := &assignmentFence{}
	projection, err := cloudhub.NewProjection(cloudhub.ProjectionConfig{HubID: config.Metadata.GetHubId(), ControllerKeyID: config.ControllerProjectionKeyID, ControllerPublicKey: ed25519.PublicKey(controllerPublicBytes), MaxStaleness: 30 * time.Minute, PolicySink: authorizer, AssignmentFence: fence})
	if err != nil {
		return nil, err
	}
	hubService, err := cloudhub.New(cloudhub.Config{HubID: config.Metadata.GetHubId(), MaxPresenceTTL: 5 * time.Minute, MaxSignalingTTL: 5 * time.Minute, PresenceQueueSize: 64, ClientQueueSize: 64, MaxSDPBytes: 1 << 20, MaxCandidates: 256, MaxPresences: 1024, MaxSessions: 4096, MaxSessionsPerClient: 64, PresenceChallengeTTL: 2 * time.Minute, MaxPresenceChallenges: 1024, EdgeAuthorizer: authorizer, AssignmentSource: projection})
	if err != nil {
		projection.Close()
		return nil, err
	}
	fence.hub = hubService
	usageOutbox, err := cloudrelay.NewUsageOutbox(config.UsageOutboxPath)
	if err != nil {
		projection.Close()
		return nil, err
	}
	relayServer, err := startRelay(config, verificationKey, now)
	if err != nil {
		projection.Close()
		return nil, err
	}
	hubListener, err := listenTCP(config.HubListen)
	if err != nil {
		_ = relayServer.Close()
		projection.Close()
		return nil, err
	}
	healthListener, err := listenTCP(config.HealthListen)
	if err != nil {
		_ = hubListener.Close()
		_ = relayServer.Close()
		projection.Close()
		return nil, err
	}
	controlClient, err := cloudhub.NewControlClient(cloudhub.ControlClientConfig{ControllerURL: config.ControllerURL, Metadata: config.Metadata, PrivateKey: ed25519.PrivateKey(hubPrivateBytes), SoftwareVersion: "development", Projection: projection, Topology: hubService})
	if err != nil {
		_ = hubListener.Close()
		_ = healthListener.Close()
		_ = relayServer.Close()
		projection.Close()
		return nil, err
	}
	runtime := &Runtime{config: config, projection: projection, control: controlClient, authorizer: authorizer, hub: hubService, relay: relayServer, usageOutbox: usageOutbox, listeners: []net.Listener{hubListener, healthListener}, errors: make(chan error, 3)}
	hubMux := newHubHTTPHandler(hubHTTPConfig{Hub: hubService, Authorizer: authorizer, Projection: projection, HubID: config.Metadata.GetHubId(), HubURL: origin(hubListener), ControllerURL: config.ControllerURL, Relay: relayServer, RelayID: config.Metadata.GetRelayId(), Region: config.Metadata.GetRegion()})
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/healthz", runtime.healthHandler)
	runtime.servers = []*http.Server{{Handler: hubMux, ReadHeaderTimeout: 5 * time.Second}, {Handler: healthMux, ReadHeaderTimeout: 5 * time.Second}}
	for index := range runtime.servers {
		server, listener := runtime.servers[index], runtime.listeners[index]
		go func() {
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				runtime.errors <- err
			}
		}()
	}
	controlContext, cancel := context.WithCancel(context.Background())
	runtime.cancel = cancel
	go func() {
		if err := controlClient.Run(controlContext); err != nil && !errors.Is(err, context.Canceled) {
			runtime.errors <- err
		}
	}()
	runtime.usageWG.Add(1)
	go runtime.runUsagePump(controlContext)
	return runtime, nil
}

// WaitReady 等待 control attachment 与 full projection，禁止 Hub 重启读取本地 snapshot 冒充 ready。
func (runtime *Runtime) WaitReady(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		state := runtime.control.State()
		if state.Attached && runtime.projection.Ready() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-runtime.errors:
			return err
		case <-ticker.C:
		}
	}
}

// Manifest 返回实时 control generation 与 projection revision。
func (runtime *Runtime) Manifest() Manifest {
	state := runtime.control.State()
	projection := runtime.projection.Snapshot()
	return Manifest{PID: os.Getpid(), EdgeDeploymentID: runtime.config.Metadata.GetEdgeDeploymentId(), HubID: runtime.config.Metadata.GetHubId(), RelayID: runtime.config.Metadata.GetRelayId(), HubURL: origin(runtime.listeners[0]), HealthURL: origin(runtime.listeners[1]), RelayURL: runtime.relay.URL(), UsageOutboxPath: runtime.config.UsageOutboxPath, ControlGeneration: state.ControlGeneration, ProjectionRevision: projection.Revision}
}

// WriteManifest 原子写入 Edge runtime manifest。
func (runtime *Runtime) WriteManifest(path string) error {
	body, _ := json.MarshalIndent(runtime.Manifest(), "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// Wait 等待 listener 或 control client 异常。
func (runtime *Runtime) Wait() error { return <-runtime.errors }

// Close 关闭 control client、HTTP listener、内存 Hub 和 Relay allocation。
func (runtime *Runtime) Close(ctx context.Context) error {
	var result error
	runtime.closeOnce.Do(func() {
		runtime.cancel()
		runtime.usageWG.Wait()
		for _, server := range runtime.servers {
			if err := server.Shutdown(ctx); err != nil && result == nil {
				result = err
			}
		}
		runtime.projection.Close()
		if err := runtime.relay.Close(); err != nil && result == nil {
			result = err
		}
		if err := runtime.relay.FlushUsageOutbox(runtime.usageOutbox, "edge_shutdown"); err != nil && result == nil {
			result = err
		}
	})
	return result
}

func (runtime *Runtime) healthHandler(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !runtime.projection.Ready() {
		http.Error(writer, "Edge control projection unavailable", http.StatusServiceUnavailable)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

type assignmentFence struct{ hub *cloudhub.Service }

func (fence *assignmentFence) FenceAssignment(daemonDeviceID string, assignmentEpoch uint64) {
	if fence.hub != nil {
		fence.hub.FenceAssignment(daemonDeviceID, assignmentEpoch)
	}
}

func startRelay(config Config, controllerKey servicecredential.VerificationKey, now time.Time) (*cloudrelay.Server, error) {
	keyRing, _ := servicecredential.NewKeyRing(controllerKey)
	relayPrivateBytes, err := base64.RawStdEncoding.DecodeString(config.RelayControlPrivateKeyBase64)
	if err != nil || len(relayPrivateBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Relay control private key")
	}
	usageSigner, err := servicecredential.NewSigner("relay-control-"+config.Metadata.GetRelayId(), ed25519.PrivateKey(relayPrivateBytes), now.Add(-time.Hour), now.Add(365*24*time.Hour))
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	bindingID := "binding-" + config.Metadata.GetRelayId()
	authority, err := cloudrelay.NewAuthority(cloudrelay.Config{RelayID: config.Metadata.GetRelayId(), RelayPool: "pool-" + config.Metadata.GetRegion(), Region: config.Metadata.GetRegion(), LeaseIssuer: "termx-cloud-controller-relay", Realm: "termx-edge-relay", KeyRing: keyRing, Bindings: cloudrelay.StaticBindings{bindingID: {config.Metadata.GetRelayId(): {}}}, CredentialSecret: secret, UsageSigner: usageSigner, CredentialTTL: 5 * time.Minute, PendingAuthTTL: 10 * time.Second})
	clear(secret)
	if err != nil {
		return nil, err
	}
	return cloudrelay.NewServer(cloudrelay.ServerConfig{Authority: authority, ListenAddr: config.RelayListen, PublicIP: config.RelayPublicIP})
}

func listenTCP(address string) (net.Listener, error) {
	if address == "" {
		address = "127.0.0.1:0"
	}
	return net.Listen("tcp", address)
}

func origin(listener net.Listener) string { return "http://" + listener.Addr().String() }
