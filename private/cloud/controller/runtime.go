// Package controller 装配单个 termx-cloud-controller 进程。
//
// composition root 只拥有 listener、配置和依赖生命周期；Hub registry、projection publisher、
// Web catalog 与 SQLite 各自保持独立 owner，不在本包复制业务状态机。
package controller

import (
	"bytes"
	"context"
	"crypto/ed25519"
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

	"github.com/lozzow/termx/private/cloud/control-plane/commandoutbox"
	cloudcommerce "github.com/lozzow/termx/private/cloud/control-plane/commerce"
	"github.com/lozzow/termx/private/cloud/control-plane/hubcontrol"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudpolicy "github.com/lozzow/termx/private/cloud/control-plane/policy"
	"github.com/lozzow/termx/private/cloud/control-plane/relaycontrol"
	"github.com/lozzow/termx/private/cloud/control-plane/relaylease"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/private/cloud/control-plane/usage"
	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	projectionTTL             = 30 * time.Minute
	projectionRefreshInterval = 10 * time.Minute
)

// DeploymentConfig 绑定一个 Hub deployment metadata 与其独立 control public key。
type DeploymentConfig struct {
	Metadata                    *cloudpb.EdgeDeploymentMetadata `json:"metadata"`
	HubControlPublicKeyBase64   string                          `json:"hub_control_public_key_base64"`
	RelayControlPublicKeyBase64 string                          `json:"relay_control_public_key_base64"`
}

// Config 是 Controller development composition 的显式配置。
type Config struct {
	DatabasePath                   string                       `json:"database_path"`
	PublicListen                   string                       `json:"public_listen"`
	InternalControlListen          string                       `json:"internal_control_listen"`
	OperatorListen                 string                       `json:"operator_listen"`
	CatalogPath                    string                       `json:"catalog_path"`
	ProjectionKeyID                string                       `json:"projection_key_id"`
	ProjectionPrivateKeyBase64     string                       `json:"projection_private_key_base64"`
	DaemonControlKeyID             string                       `json:"daemon_control_key_id"`
	DaemonControlPrivateKeyBase64  string                       `json:"daemon_control_private_key_base64"`
	Deployments                    []DeploymentConfig           `json:"deployments"`
	Devices                        []*cloudpb.CloudDevicePolicy `json:"devices"`
	Assignments                    []*cloudpb.HubAssignment     `json:"assignments"`
	EnableTestPaymentProvider      bool                         `json:"enable_test_payment_provider"`
	DevelopmentEnrollmentCode      string                       `json:"development_enrollment_code"`
	DevelopmentEnrollmentAccountID string                       `json:"development_enrollment_account_id"`
	DevelopmentEnrollmentHubID     string                       `json:"development_enrollment_hub_id"`
	OperatorID                     string                       `json:"operator_id"`
	OperatorRole                   string                       `json:"operator_role"`
	OperatorAccessTokenBase64      string                       `json:"operator_access_token_base64"`
	SecureCookie                   bool                         `json:"secure_cookie"`
	WebStaticDir                   string                       `json:"web_static_dir"`
}

// Manifest 是 supervisor 和 E2E harness 使用的非秘密 Controller 进程描述。
type Manifest struct {
	PID                int      `json:"pid"`
	PublicURL          string   `json:"public_url"`
	InternalControlURL string   `json:"internal_control_url"`
	OperatorURL        string   `json:"operator_url"`
	DatabasePath       string   `json:"database_path"`
	HubIDs             []string `json:"hub_ids"`
}

// Runtime 持有 Controller listener、SQLite 与 control publisher 生命周期。
type Runtime struct {
	store          *cloudsqlite.Store
	publisher      *hubcontrol.Publisher
	relayPublisher *relaycontrol.Publisher
	registry       *hubregistry.Registry
	topology       *cloudtopology.Service
	outbox         *commandoutbox.Service
	planner        *commandoutbox.Planner
	dispatcher     *commandoutbox.Dispatcher
	manifest       Manifest
	listeners      []net.Listener
	servers        []*http.Server
	errors         chan error
	policyChanges  chan string
	policyDone     chan struct{}
	policyWG       sync.WaitGroup
	dispatcherWG   sync.WaitGroup
	closeOnce      sync.Once
}

// LoadConfig 读取 0600 development config；未知字段 fail closed。
func LoadConfig(path string) (Config, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode Controller config: %w", err)
	}
	return config, nil
}

// Start 建立三个独立 listener，并在 serving 前发布每个 Hub 的持久递增 full projection。
func Start(config Config) (*Runtime, error) {
	return start(config, projectionRefreshInterval)
}

func start(config Config, refreshInterval time.Duration) (*Runtime, error) {
	if config.DatabasePath == "" || config.CatalogPath == "" || config.ProjectionKeyID == "" || config.DaemonControlKeyID == "" || len(config.Deployments) < 1 {
		return nil, fmt.Errorf("Controller database, catalog, projection key, daemon control key and deployments are required")
	}
	if refreshInterval <= 0 || refreshInterval >= projectionTTL {
		return nil, fmt.Errorf("Controller projection refresh interval is invalid")
	}
	if (config.DevelopmentEnrollmentCode == "") != (config.DevelopmentEnrollmentAccountID == "") {
		return nil, fmt.Errorf("development enrollment code and account must be configured together")
	}
	now := time.Now().UTC()
	privateKeyBytes, err := base64.RawStdEncoding.DecodeString(config.ProjectionPrivateKeyBase64)
	if err != nil || len(privateKeyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Controller projection private key")
	}
	privateKey := ed25519.PrivateKey(privateKeyBytes)
	credentialSigner, err := servicecredential.NewSigner(config.ProjectionKeyID, privateKey, now.Add(-time.Hour), now.Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	edgeIssuer, err := servicecredential.NewEdgeAccessIssuer("termx-cloud-controller", credentialSigner)
	if err != nil {
		return nil, err
	}
	daemonControlKeyBytes, err := base64.RawStdEncoding.DecodeString(config.DaemonControlPrivateKeyBase64)
	if err != nil || len(daemonControlKeyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Controller daemon control private key")
	}
	daemonControlKey := ed25519.PrivateKey(daemonControlKeyBytes)
	store, err := cloudsqlite.Open(config.DatabasePath)
	if err != nil {
		return nil, err
	}
	catalog, err := webcontroller.LoadCatalog(config.CatalogPath)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	policyService, err := cloudpolicy.New(store)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	policyChanges := make(chan string, 64)
	policyDone := make(chan struct{})
	notifyPolicyChange := func(accountID string) {
		select {
		case policyChanges <- accountID:
		case <-policyDone:
		}
	}
	commerceService, err := cloudcommerce.New(cloudcommerce.Config{Store: store, Catalog: catalog.Contract(), Now: time.Now, NotifyPolicyChange: notifyPolicyChange})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	registry, _ := hubregistry.New(store)
	topologyService, err := cloudtopology.New(registry, store)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	for _, device := range config.Devices {
		stored, loadErr := topologyService.Device(context.Background(), device.GetDeviceId())
		if loadErr == nil {
			if stored.AccountID != device.GetAccountId() || stored.Kind != device.GetDeviceKind() || !bytes.Equal(stored.PublicKey, device.GetPublicKey()) {
				_ = store.Close()
				return nil, fmt.Errorf("persisted topology device ownership %q conflicts with config", device.GetDeviceId())
			}
			continue
		}
		if !errors.Is(loadErr, cloudtopology.ErrOwnershipNotFound) {
			_ = store.Close()
			return nil, loadErr
		}
		if err := topologyService.PutDeviceOwnership(context.Background(), device); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("persist topology device ownership %q: %w", device.GetDeviceId(), err)
		}
	}
	for _, deployment := range config.Deployments {
		publicKey, decodeErr := base64.RawStdEncoding.DecodeString(deployment.HubControlPublicKeyBase64)
		if decodeErr != nil {
			_ = store.Close()
			return nil, fmt.Errorf("decode Hub control public key: %w", decodeErr)
		}
		relayPublicKey, decodeErr := base64.RawStdEncoding.DecodeString(deployment.RelayControlPublicKeyBase64)
		if decodeErr != nil || len(relayPublicKey) != ed25519.PublicKeySize {
			_ = store.Close()
			return nil, fmt.Errorf("decode Relay control public key")
		}
		if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: deployment.Metadata, ControlPublicKey: ed25519.PublicKey(publicKey), RelayControlPublicKey: ed25519.PublicKey(relayPublicKey), Enabled: true, UpdatedAt: now}); err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	for _, assignment := range config.Assignments {
		_, currentErr := registry.Assignment(context.Background(), assignment.GetDaemonDeviceId())
		if currentErr == nil {
			continue
		}
		if !errors.Is(currentErr, hubregistry.ErrAssignmentConflict) {
			_ = store.Close()
			return nil, fmt.Errorf("load initial assignment %q: %w", assignment.GetDaemonDeviceId(), currentErr)
		}
		if _, err := registry.Assign(context.Background(), assignment, now); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("publish initial assignment %q: %w", assignment.GetDaemonDeviceId(), err)
		}
	}
	publisher := hubcontrol.NewPublisher()
	relayPublisher := relaycontrol.NewPublisher()
	outboxService, err := commandoutbox.New(store)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	planner, err := commandoutbox.NewPlanner(outboxService, topologyService, store, nil, notifyPolicyChange, registry)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	var enrollment *enrollmentService
	if config.DevelopmentEnrollmentCode != "" {
		hubID := config.DevelopmentEnrollmentHubID
		if hubID == "" {
			hubID = config.Deployments[0].Metadata.GetHubId()
		}
		if _, err := registry.Deployment(context.Background(), hubID); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("development enrollment Hub is invalid: %w", err)
		}
		enrollment, err = newEnrollmentService(enrollmentServiceConfig{
			Code: config.DevelopmentEnrollmentCode, AccountID: config.DevelopmentEnrollmentAccountID, HubID: hubID,
			Commerce: commerceService, Topology: topologyService, Registry: registry, EdgeIssuer: edgeIssuer,
			ControlKeyID: config.DaemonControlKeyID, ControlPublicKey: daemonControlKey.Public().(ed25519.PublicKey),
			ControlNotBefore: now.Add(-time.Hour), ControlNotAfter: now.Add(24 * time.Hour), Now: time.Now, NotifyPolicyChange: notifyPolicyChange,
		})
		if err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	dispatcher, err := commandoutbox.NewDispatcher(outboxService, publisher, relayPublisher, topologyService, config.DaemonControlKeyID, daemonControlKey)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	for _, deployment := range config.Deployments {
		hubID := deployment.Metadata.GetHubId()
		revision, err := store.AllocateProjectionRevision(context.Background(), hubID, now)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		accounts, devices, assignments, err := projectionForHub(registry, topologyService, policyService, hubID, now)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		full, err := hubcontrol.BuildSignedFullProjection(hubcontrol.FullProjectionInput{HubID: hubID, Revision: revision, GeneratedAt: now, TTL: projectionTTL, Accounts: accounts, Devices: devices, Assignments: assignments, SigningKeyID: config.ProjectionKeyID, SigningKey: privateKey})
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		if err := store.SetProjectionDigest(context.Background(), hubID, revision, full.GetSnapshotDigest()); err != nil {
			_ = store.Close()
			return nil, err
		}
		if err := publisher.PublishFull(full); err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	var runtime *Runtime
	resultSink := &migrationResultSink{outbox: outboxService, refresh: func(projection *cloudpb.ManagementCommandProjection, completedAt time.Time) error {
		migration := projection.GetTarget().GetAssignmentMigration()
		for _, hubID := range []string{migration.GetSourceHubId(), migration.GetTargetHubId()} {
			if err := runtime.publishHubPolicy(config, policyService, privateKey, hubID, completedAt); err != nil {
				return err
			}
		}
		return nil
	}}
	controlServer, err := hubcontrol.NewServer(hubcontrol.ServerConfig{Registry: registry, CursorStore: store, Publisher: publisher, Topology: topologyService, Results: resultSink, Clock: time.Now, ChallengeTTL: 30 * time.Second, EnvelopeTTL: 5 * time.Minute})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	relayControlServer, err := relaycontrol.NewServer(relaycontrol.ServerConfig{Registry: registry, CursorStore: store, Publisher: relayPublisher, Results: outboxService, Clock: time.Now, ChallengeTTL: 30 * time.Second, EnvelopeTTL: 5 * time.Minute})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	relayLeaseHandler, err := newRelayLeaseHTTPHandler(store, topologyService, registry, credentialSigner, config.Deployments)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	relayUsageHandler, err := newRelayUsageHTTPHandler(store, credentialSigner, config.Deployments, now)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	productHandler, err := webcontroller.ProductAPIHandler(webcontroller.ProductAPIConfig{Commerce: commerceService, EnableTestPaymentProvider: config.EnableTestPaymentProvider, SecureCookie: config.SecureCookie})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	managementHandler, err := webcontroller.ManagementAPIHandler(webcontroller.ManagementAPIConfig{Commerce: commerceService, Planner: planner, Outbox: outboxService, Topology: topologyService, Quota: store, Now: time.Now, SecureCookie: config.SecureCookie})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/healthz", healthHandler)
	if enrollment != nil {
		enrollment.registerHTTP(publicMux)
	}
	publicMux.HandleFunc("/api/v1/catalog", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := protojson.Marshal(&cloudpb.GetPlanCatalogResponse{Catalog: catalog.Contract()})
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(body)
	})
	publicMux.Handle("/api/v1/management/", managementHandler)
	publicMux.Handle("/api/v1/", productHandler)
	operatorMux := http.NewServeMux()
	operatorMux.HandleFunc("/healthz", healthHandler)
	if config.OperatorAccessTokenBase64 != "" {
		operatorToken, decodeErr := base64.RawStdEncoding.DecodeString(config.OperatorAccessTokenBase64)
		if decodeErr != nil || len(operatorToken) < 32 || config.OperatorID == "" {
			_ = store.Close()
			return nil, fmt.Errorf("invalid operator identity or access token")
		}
		actorKind := cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_ADMIN
		if config.OperatorRole == "readonly" {
			actorKind = cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_READONLY
		} else if config.OperatorRole != "admin" {
			clear(operatorToken)
			_ = store.Close()
			return nil, fmt.Errorf("invalid operator role")
		}
		fleet := &fleetQuery{registry: registry, publisher: publisher, hubControl: controlServer, relayControl: relayControlServer}
		operatorHandler, handlerErr := webcontroller.OperatorAPIHandler(webcontroller.OperatorAPIConfig{AccessToken: operatorToken, OperatorID: config.OperatorID, ActorKind: actorKind, Commerce: commerceService, Topology: topologyService, Quota: store, Outbox: outboxService, Planner: planner, Fleet: fleet, Now: time.Now, SecureCookie: config.SecureCookie})
		clear(operatorToken)
		if handlerErr != nil {
			_ = store.Close()
			return nil, handlerErr
		}
		operatorMux.Handle("/api/v1/operator/", operatorHandler)
	}
	if config.WebStaticDir != "" {
		staticHandler, staticErr := webStaticHandler(config.WebStaticDir)
		if staticErr != nil {
			_ = store.Close()
			return nil, staticErr
		}
		publicMux.Handle("/", staticHandler)
		operatorMux.Handle("/", staticHandler)
	}
	internalMux := http.NewServeMux()
	internalMux.Handle(relaylease.InternalReservePath, relayLeaseHandler)
	internalMux.Handle(usage.InternalReportPath, relayUsageHandler)
	internalMux.Handle("/v1/relay/control/", relayControlServer.Handler())
	internalMux.Handle("/", controlServer.Handler())
	publicListener, err := listen(config.PublicListen)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	internalListener, err := listen(config.InternalControlListen)
	if err != nil {
		_ = publicListener.Close()
		_ = store.Close()
		return nil, err
	}
	operatorListener, err := listen(config.OperatorListen)
	if err != nil {
		_ = publicListener.Close()
		_ = internalListener.Close()
		_ = store.Close()
		return nil, err
	}
	runtime = &Runtime{store: store, publisher: publisher, relayPublisher: relayPublisher, registry: registry, topology: topologyService, outbox: outboxService, planner: planner, dispatcher: dispatcher, listeners: []net.Listener{publicListener, internalListener, operatorListener}, errors: make(chan error, 3), policyChanges: policyChanges, policyDone: policyDone}
	runtime.servers = []*http.Server{{Handler: publicMux, ReadHeaderTimeout: 5 * time.Second}, {Handler: internalMux, ReadHeaderTimeout: 5 * time.Second}, {Handler: operatorMux, ReadHeaderTimeout: 5 * time.Second}}
	for index := range runtime.servers {
		server, listener := runtime.servers[index], runtime.listeners[index]
		go func() {
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				runtime.errors <- err
			}
		}()
	}
	runtime.policyWG.Add(1)
	go runtime.runPolicyPublisher(config, policyService, privateKey, refreshInterval)
	runtime.dispatcherWG.Add(1)
	go runtime.runCommandDispatcher()
	manifest := Manifest{PID: os.Getpid(), PublicURL: origin(publicListener), InternalControlURL: origin(internalListener), OperatorURL: origin(operatorListener), DatabasePath: config.DatabasePath}
	for _, deployment := range config.Deployments {
		manifest.HubIDs = append(manifest.HubIDs, deployment.Metadata.GetHubId())
	}
	runtime.manifest = manifest
	return runtime, nil
}

// Manifest 返回非秘密 runtime metadata。
func (runtime *Runtime) Manifest() Manifest { return runtime.manifest }

// WriteManifest 原子写入 supervisor 可读取的 Controller manifest。
func (runtime *Runtime) WriteManifest(path string) error {
	body, _ := json.MarshalIndent(runtime.manifest, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// Wait 等待任一 listener 异常退出。
func (runtime *Runtime) Wait() error { return <-runtime.errors }

// Close 关闭全部 listener 与 SQLite。
func (runtime *Runtime) Close(ctx context.Context) error {
	var result error
	runtime.closeOnce.Do(func() {
		close(runtime.policyDone)
		for _, server := range runtime.servers {
			if err := server.Shutdown(ctx); err != nil && result == nil {
				result = err
			}
		}
		runtime.policyWG.Wait()
		runtime.dispatcherWG.Wait()
		if err := runtime.store.Close(); err != nil && result == nil {
			result = err
		}
	})
	return result
}

func (runtime *Runtime) runCommandDispatcher() {
	defer runtime.dispatcherWG.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := runtime.dispatcher.DispatchOnce(context.Background(), time.Now().UTC(), 128)
		if err != nil && !errors.Is(err, hubcontrol.ErrPublisherBackpressure) && !errors.Is(err, relaycontrol.ErrPublisherBackpressure) && !errors.Is(err, commandoutbox.ErrCommandConflict) && !errors.Is(err, commandoutbox.ErrCommandNotFound) {
			runtime.reportPolicyError(err)
		}
		select {
		case <-ticker.C:
		case <-runtime.policyDone:
			return
		}
	}
}

func projectionForHub(registry *hubregistry.Registry, topology *cloudtopology.Service, policies *cloudpolicy.Service, hubID string, now time.Time) ([]*cloudpb.HubAccountPolicy, []*cloudpb.CloudDevicePolicy, []*cloudpb.HubAssignment, error) {
	stored, err := registry.AssignmentsForHub(context.Background(), hubID, now)
	if err != nil {
		return nil, nil, nil, err
	}
	accountsNeeded := map[string]bool{}
	var assignments []*cloudpb.HubAssignment
	for _, value := range stored {
		assignments = append(assignments, value.Value)
		accountsNeeded[value.Value.GetAccountId()] = true
	}
	var accounts []*cloudpb.HubAccountPolicy
	for accountID := range accountsNeeded {
		value, err := policies.HubAccountPolicy(context.Background(), accountID)
		if err != nil {
			return nil, nil, nil, err
		}
		accounts = append(accounts, value)
	}
	storedDevices, err := topology.DevicePolicies(context.Background())
	if err != nil {
		return nil, nil, nil, err
	}
	var devices []*cloudpb.CloudDevicePolicy
	for _, value := range storedDevices {
		if accountsNeeded[value.GetAccountId()] {
			devices = append(devices, value)
		}
	}
	return accounts, devices, assignments, nil
}

func (runtime *Runtime) runPolicyPublisher(config Config, policies *cloudpolicy.Service, signingKey ed25519.PrivateKey, refreshInterval time.Duration) {
	defer runtime.policyWG.Done()
	refresh := time.NewTicker(refreshInterval)
	defer refresh.Stop()
	for {
		select {
		case accountID := <-runtime.policyChanges:
			if err := runtime.publishAccountPolicy(config, policies, signingKey, accountID, time.Now().UTC()); err != nil {
				runtime.reportPolicyError(err)
			}
		case now := <-refresh.C:
			if err := runtime.publishAllPolicies(config, policies, signingKey, now.UTC()); err != nil {
				runtime.reportPolicyError(err)
			}
		case <-runtime.policyDone:
			return
		}
	}
}

func (runtime *Runtime) publishAccountPolicy(config Config, policies *cloudpolicy.Service, signingKey ed25519.PrivateKey, accountID string, now time.Time) error {
	for _, deployment := range config.Deployments {
		hubID := deployment.Metadata.GetHubId()
		assignments, err := runtime.registry.AssignmentsForHub(context.Background(), hubID, now)
		if err != nil {
			return err
		}
		assigned := false
		for _, assignment := range assignments {
			if assignment.Value.GetAccountId() == accountID {
				assigned = true
				break
			}
		}
		if !assigned {
			continue
		}
		if err := runtime.publishHubPolicy(config, policies, signingKey, hubID, now); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *Runtime) publishAllPolicies(config Config, policies *cloudpolicy.Service, signingKey ed25519.PrivateKey, now time.Time) error {
	for _, deployment := range config.Deployments {
		if err := runtime.publishHubPolicy(config, policies, signingKey, deployment.Metadata.GetHubId(), now); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *Runtime) publishHubPolicy(config Config, policies *cloudpolicy.Service, signingKey ed25519.PrivateKey, hubID string, now time.Time) error {
	revision, err := runtime.store.AllocateProjectionRevision(context.Background(), hubID, now)
	if err != nil {
		return err
	}
	accounts, devices, assignments, err := projectionForHub(runtime.registry, runtime.topology, policies, hubID, now)
	if err != nil {
		return err
	}
	full, err := hubcontrol.BuildSignedFullProjection(hubcontrol.FullProjectionInput{HubID: hubID, Revision: revision, GeneratedAt: now, TTL: projectionTTL, Accounts: accounts, Devices: devices, Assignments: assignments, SigningKeyID: config.ProjectionKeyID, SigningKey: signingKey})
	if err != nil {
		return err
	}
	if err := runtime.store.SetProjectionDigest(context.Background(), hubID, revision, full.GetSnapshotDigest()); err != nil {
		return err
	}
	return runtime.publishFullWithRetry(full)
}

func (runtime *Runtime) reportPolicyError(err error) {
	select {
	case runtime.errors <- err:
	default:
	}
}

func (runtime *Runtime) publishFullWithRetry(full *cloudpb.FullProjectionSnapshot) error {
	for {
		err := runtime.publisher.PublishFull(full)
		if !errors.Is(err, hubcontrol.ErrPublisherBackpressure) {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-timer.C:
		case <-runtime.policyDone:
			timer.Stop()
			return nil
		}
	}
}

func listen(address string) (net.Listener, error) {
	if address == "" {
		address = "127.0.0.1:0"
	}
	return net.Listen("tcp", address)
}

func origin(listener net.Listener) string { return "http://" + listener.Addr().String() }

func healthHandler(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
