// Package controller 装配单个 muxvia-cloud-controller 进程。
//
// composition root 只拥有 listener、配置和依赖生命周期；Hub registry、projection publisher、
// Web catalog 与 persistent store 各自保持独立 owner，不在本包复制业务状态机。
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
	"sort"
	"sync"
	"time"

	cloudcatalog "github.com/muxvia/muxvia/private/cloud/control-plane/catalog"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	cloudcommerce "github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	cloudcreem "github.com/muxvia/muxvia/private/cloud/control-plane/creem"
	cloudentitlement "github.com/muxvia/muxvia/private/cloud/control-plane/entitlement"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubcontrol"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/persistence"
	cloudpolicy "github.com/muxvia/muxvia/private/cloud/control-plane/policy"
	cloudpostgres "github.com/muxvia/muxvia/private/cloud/control-plane/postgres"
	cloudpromotion "github.com/muxvia/muxvia/private/cloud/control-plane/promotion"
	"github.com/muxvia/muxvia/private/cloud/control-plane/relaycontrol"
	"github.com/muxvia/muxvia/private/cloud/control-plane/relaylease"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	"github.com/muxvia/muxvia/private/cloud/control-plane/usage"
	webcontroller "github.com/muxvia/muxvia/private/cloud/web-controller"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	projectionTTL             = 30 * time.Minute
	projectionRefreshInterval = 10 * time.Minute
)

// Config 是 Controller development composition 的显式配置。
type Config struct {
	PostgresDSN                string `json:"postgres_dsn"`
	PublicListen               string `json:"public_listen"`
	InternalControlListen      string `json:"internal_control_listen"`
	OperatorListen             string `json:"operator_listen"`
	CatalogPath                string `json:"catalog_path"`
	ProjectionKeyID            string `json:"projection_key_id"`
	ProjectionPrivateKeyBase64 string `json:"projection_private_key_base64"`
	// CredentialNotBeforeUnixMillis 和 CredentialNotAfterUnixMillis 是部署密钥的绝对验证窗口，
	// Controller 与全部 Edge 必须使用相同值，避免各进程按启动时间建立第二份 key validity 真值。
	CredentialNotBeforeUnixMillis int64                        `json:"credential_not_before_unix_millis"`
	CredentialNotAfterUnixMillis  int64                        `json:"credential_not_after_unix_millis"`
	DaemonControlKeyID            string                       `json:"daemon_control_key_id"`
	DaemonControlPrivateKeyBase64 string                       `json:"daemon_control_private_key_base64"`
	Devices                       []*cloudpb.CloudDevicePolicy `json:"devices"`
	Assignments                   []*cloudpb.HubAssignment     `json:"assignments"`
	EnableTestPaymentProvider     bool                         `json:"enable_test_payment_provider"`
	CreemEnvironment              string                       `json:"creem_environment,omitempty"`
	CreemSuccessURL               string                       `json:"creem_success_url,omitempty"`
	CreemAPIKey                   string                       `json:"-"`
	CreemWebhookSecret            string                       `json:"-"`
	DevelopmentMobileHubID        string                       `json:"development_mobile_hub_id"`
	OperatorID                    string                       `json:"operator_id"`
	OperatorRole                  string                       `json:"operator_role"`
	OperatorAccessTokenBase64     string                       `json:"operator_access_token_base64"`
	SecureCookie                  bool                         `json:"secure_cookie"`
	WebStaticDir                  string                       `json:"web_static_dir"`
}

// Manifest 是 supervisor 和 E2E harness 使用的非秘密 Controller 进程描述。
type Manifest struct {
	PID                int    `json:"pid"`
	PublicURL          string `json:"public_url"`
	InternalControlURL string `json:"internal_control_url"`
	OperatorURL        string `json:"operator_url"`
	DatabaseEngine     string `json:"database_engine"`
}

// Runtime 持有 Controller listener、persistent store 与 control publisher 生命周期。
type Runtime struct {
	store          persistence.Store
	publisher      *hubcontrol.Publisher
	relayPublisher *relaycontrol.Publisher
	registry       *hubregistry.Registry
	topology       *cloudtopology.Service
	outbox         *commandoutbox.Service
	planner        *commandoutbox.Planner
	dispatcher     *commandoutbox.Dispatcher
	enrollment     *enrollmentService
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
	if config.PostgresDSN == "" || config.CatalogPath == "" || config.ProjectionKeyID == "" || config.DaemonControlKeyID == "" {
		return nil, fmt.Errorf("Controller database, catalog, projection key and daemon control key are required")
	}
	if refreshInterval <= 0 || refreshInterval >= projectionTTL {
		return nil, fmt.Errorf("Controller projection refresh interval is invalid")
	}
	now := time.Now().UTC()
	credentialNotBefore, credentialNotAfter, err := credentialWindow(now, config.CredentialNotBeforeUnixMillis, config.CredentialNotAfterUnixMillis)
	if err != nil {
		return nil, err
	}
	privateKeyBytes, err := base64.RawStdEncoding.DecodeString(config.ProjectionPrivateKeyBase64)
	if err != nil || len(privateKeyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Controller projection private key")
	}
	privateKey := ed25519.PrivateKey(privateKeyBytes)
	credentialSigner, err := servicecredential.NewSigner(config.ProjectionKeyID, privateKey, credentialNotBefore, credentialNotAfter)
	if err != nil {
		return nil, err
	}
	edgeIssuer, err := servicecredential.NewEdgeAccessIssuer("muxvia-cloud-controller", credentialSigner)
	if err != nil {
		return nil, err
	}
	daemonControlKeyBytes, err := base64.RawStdEncoding.DecodeString(config.DaemonControlPrivateKeyBase64)
	if err != nil || len(daemonControlKeyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Controller daemon control private key")
	}
	daemonControlKey := ed25519.PrivateKey(daemonControlKeyBytes)
	store, err := cloudpostgres.Open(context.Background(), config.PostgresDSN)
	if err != nil {
		return nil, err
	}
	catalog, err := webcontroller.LoadCatalog(config.CatalogPath)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	catalogService, err := cloudcatalog.New(store, time.Now)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := catalogService.Bootstrap(context.Background(), catalog.Contract()); err != nil {
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
	var creemClient *cloudcreem.Client
	if config.CreemEnvironment != "" || config.CreemAPIKey != "" || config.CreemWebhookSecret != "" || config.CreemSuccessURL != "" {
		creemClient, err = cloudcreem.NewClient(cloudcreem.ClientConfig{Environment: cloudcreem.Environment(config.CreemEnvironment), APIKey: config.CreemAPIKey})
		if err != nil || config.CreemSuccessURL == "" {
			_ = store.Close()
			return nil, fmt.Errorf("invalid Creem provider configuration")
		}
	}
	var promotionValidator cloudpromotion.ExternalValidator
	if creemClient != nil {
		promotionValidator, err = cloudcreem.NewPromotionValidator(creemClient, catalogService)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	promotionService, err := cloudpromotion.New(store, time.Now, nil, promotionValidator)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	commerceService, err := cloudcommerce.New(cloudcommerce.Config{Store: store, Catalog: catalogService, Promotions: promotionService, Now: time.Now, NotifyPolicyChange: notifyPolicyChange})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	var creemService *cloudcreem.Service
	if creemClient != nil {
		creemService, err = cloudcreem.NewService(cloudcreem.ServiceConfig{API: creemClient, Commerce: commerceService, SuccessURL: config.CreemSuccessURL, WebhookSecret: config.CreemWebhookSecret, Now: time.Now})
		if err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	overrideService, err := cloudentitlement.NewOverrideService(cloudentitlement.OverrideServiceConfig{Store: store, Plans: catalogService, Now: time.Now, NotifyPolicyChange: notifyPolicyChange})
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
	dispatcher, err := commandoutbox.NewDispatcher(outboxService, publisher, relayPublisher, topologyService, config.DaemonControlKeyID, daemonControlKey)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	registeredDeployments, err := registry.Deployments(context.Background())
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	for _, deployment := range registeredDeployments {
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
	enrollment, err := newEnrollmentService(enrollmentServiceConfig{
		Commerce: commerceService, Topology: topologyService, Registry: registry, EnrollmentStore: store, EdgeIssuer: edgeIssuer,
		CandidateProvider: enrollmentCandidateProvider(registry, controlServer),
		ControlKeyID:      config.DaemonControlKeyID, ControlPublicKey: daemonControlKey.Public().(ed25519.PublicKey),
		ControlNotBefore: credentialNotBefore, ControlNotAfter: credentialNotAfter, Now: time.Now, NotifyPolicyChange: notifyPolicyChange,
	})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	relayControlServer, err := relaycontrol.NewServer(relaycontrol.ServerConfig{Registry: registry, CursorStore: store, Publisher: relayPublisher, Results: outboxService, Clock: time.Now, ChallengeTTL: 30 * time.Second, EnvelopeTTL: 5 * time.Minute})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	relayLeaseHandler, err := newRelayLeaseHTTPHandler(store, topologyService, registry, credentialSigner)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	relayUsageHandler, err := newRelayUsageHTTPHandler(store, credentialSigner, registry, credentialNotBefore, credentialNotAfter)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	var checkoutProvider webcontroller.CheckoutProvider
	if creemService != nil {
		checkoutProvider = creemService
	}
	productHandler, err := webcontroller.ProductAPIHandler(webcontroller.ProductAPIConfig{Commerce: commerceService, EnableTestPaymentProvider: config.EnableTestPaymentProvider, CheckoutProvider: checkoutProvider, SecureCookie: config.SecureCookie})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	managementHandler, err := webcontroller.ManagementAPIHandler(webcontroller.ManagementAPIConfig{Commerce: commerceService, Planner: planner, Outbox: outboxService, Topology: topologyService, Quota: store, Now: time.Now, SecureCookie: config.SecureCookie})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	mobileHubID := config.DevelopmentMobileHubID
	mobileActivation, err := newMobileActivationService(commerceService, store, topologyService, registry, edgeIssuer, mobileHubID, credentialNotAfter, time.Now, notifyPolicyChange)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	mobileActivationHandler, err := webcontroller.MobileActivationAPIHandler(webcontroller.MobileActivationAPIConfig{Commerce: commerceService, Service: mobileActivation})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	daemonEnrollmentHandler, err := webcontroller.DaemonEnrollmentAPIHandler(webcontroller.DaemonEnrollmentAPIConfig{Commerce: commerceService, Service: enrollment})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/healthz", healthHandler)
	if creemService != nil {
		publicMux.Handle("/pay/creem", creemService.WebhookHandler())
	}
	mobileActivation.registerHTTP(publicMux)
	enrollment.registerHTTP(publicMux)
	publicMux.HandleFunc("/api/v1/catalog", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		activeCatalog, catalogErr := catalogService.Active(request.Context())
		if catalogErr != nil {
			http.Error(writer, "catalog unavailable", http.StatusServiceUnavailable)
			return
		}
		body, _ := protojson.Marshal(&cloudpb.GetPlanCatalogResponse{Catalog: activeCatalog})
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(body)
	})
	publicMux.Handle("/api/v1/management/", managementHandler)
	publicMux.Handle("/api/v1/mobile-activations/", mobileActivationHandler)
	publicMux.Handle("/api/v1/daemon-enrollments/", daemonEnrollmentHandler)
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
		fleet := &fleetQuery{registry: registry, publisher: publisher, hubControl: controlServer, relayControl: relayControlServer, onEnabled: func(hubID string, changedAt time.Time) {
			if runtime == nil {
				return
			}
			if publishErr := runtime.publishHubPolicy(config, policyService, privateKey, hubID, changedAt); publishErr != nil {
				runtime.reportPolicyError(publishErr)
			}
		}}
		operatorHandler, handlerErr := webcontroller.OperatorAPIHandler(webcontroller.OperatorAPIConfig{AccessToken: operatorToken, OperatorID: config.OperatorID, ActorKind: actorKind, Commerce: commerceService, Catalog: catalogService, Overrides: overrideService, Promotions: promotionService, Topology: topologyService, Quota: store, Outbox: outboxService, Planner: planner, Fleet: fleet, PaymentReconciler: creemService, Now: time.Now, SecureCookie: config.SecureCookie})
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
	runtime = &Runtime{store: store, publisher: publisher, relayPublisher: relayPublisher, registry: registry, topology: topologyService, outbox: outboxService, planner: planner, dispatcher: dispatcher, enrollment: enrollment, listeners: []net.Listener{publicListener, internalListener, operatorListener}, errors: make(chan error, 3), policyChanges: policyChanges, policyDone: policyDone}
	runtime.servers = []*http.Server{{Handler: publicMux, ReadHeaderTimeout: 5 * time.Second}, {Handler: internalMux, ReadHeaderTimeout: 5 * time.Second}, {Handler: operatorMux, ReadHeaderTimeout: 5 * time.Second}}
	for index := range runtime.servers {
		server, listener := runtime.servers[index], runtime.listeners[index]
		go func() {
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				runtime.errors <- err
			}
		}()
	}
	runtime.policyWG.Add(3)
	go runtime.runPolicyPublisher(config, policyService, privateKey, refreshInterval)
	go runtime.runEntitlementOverrideReconciler(overrideService)
	go runtime.runPromotionReservationReconciler(promotionService)
	if creemService != nil {
		runtime.policyWG.Add(1)
		go runtime.runCreemReconciler(creemService)
	}
	runtime.dispatcherWG.Add(1)
	go runtime.runCommandDispatcher()
	manifest := Manifest{PID: os.Getpid(), PublicURL: origin(publicListener), InternalControlURL: origin(internalListener), OperatorURL: origin(operatorListener), DatabaseEngine: "postgresql"}
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

// Close 关闭全部 listener 与 persistent store。
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

func (runtime *Runtime) runCreemReconciler(service *cloudcreem.Service) {
	defer runtime.policyWG.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := service.ReconcileOnce(context.Background(), 100); err != nil {
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

type enrollmentRegistryView interface {
	Deployments(context.Context) ([]hubregistry.Deployment, error)
	AssignmentsForHub(context.Context, string, time.Time) ([]hubregistry.Assignment, error)
}

type enrollmentAttachmentView interface {
	AttachmentStatus(string) (uint64, time.Time, bool)
}

func enrollmentCandidateProvider(registry enrollmentRegistryView, control enrollmentAttachmentView) func(context.Context, time.Time, string) ([]enrollmentHubCandidate, error) {
	return func(ctx context.Context, now time.Time, existingHubID string) ([]enrollmentHubCandidate, error) {
		registered, err := registry.Deployments(ctx)
		if err != nil {
			return nil, err
		}
		candidates := make([]enrollmentHubCandidate, 0, len(registered))
		for _, deployment := range registered {
			if !deployment.IdentityApproved || !deployment.Enabled || deployment.Archived || deployment.Draining && deployment.Metadata.GetHubId() != existingHubID || deployment.PublicHubURL == "" || deployment.HealthURL == "" || deployment.MaxAssignments == 0 {
				continue
			}
			if _, _, attached := control.AttachmentStatus(deployment.Metadata.GetHubId()); !attached {
				continue
			}
			assignments, err := registry.AssignmentsForHub(ctx, deployment.Metadata.GetHubId(), now)
			if err != nil {
				return nil, err
			}
			count := uint64(len(assignments))
			// 满载只阻止新增 assignment；当前 daemon 复用自己的 owning Hub 不增加占用。
			if count >= deployment.MaxAssignments && deployment.Metadata.GetHubId() != existingHubID {
				continue
			}
			candidates = append(candidates, enrollmentHubCandidate{
				value: &cloudpb.HubEnrollmentCandidate{
					HubId: deployment.Metadata.GetHubId(), HubUrl: deployment.PublicHubURL, HealthUrl: deployment.HealthURL,
					Region: deployment.Metadata.GetRegion(), DirectoryRevision: deployment.DirectoryRevision,
				},
				assignmentCount: count,
				maxAssignments:  deployment.MaxAssignments,
			})
		}
		sort.Slice(candidates, func(left, right int) bool {
			return candidates[left].value.GetHubId() < candidates[right].value.GetHubId()
		})
		if len(candidates) > 100 {
			candidates = candidates[:100]
		}
		return candidates, nil
	}
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

func (runtime *Runtime) runEntitlementOverrideReconciler(overrides *cloudentitlement.OverrideService) {
	defer runtime.policyWG.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if _, err := overrides.ReconcileDue(context.Background(), 100); err != nil {
			runtime.reportPolicyError(err)
		}
		select {
		case <-ticker.C:
		case <-runtime.policyDone:
			return
		}
	}
}

func (runtime *Runtime) runPromotionReservationReconciler(promotions *cloudpromotion.Service) {
	defer runtime.policyWG.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if _, err := promotions.ReconcileExpired(context.Background(), 100); err != nil {
			runtime.reportPolicyError(err)
		}
		select {
		case <-ticker.C:
		case <-runtime.policyDone:
			return
		}
	}
}

func (runtime *Runtime) publishAccountPolicy(config Config, policies *cloudpolicy.Service, signingKey ed25519.PrivateKey, accountID string, now time.Time) error {
	deployments, err := runtime.registry.Deployments(context.Background())
	if err != nil {
		return err
	}
	for _, deployment := range deployments {
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
	deployments, err := runtime.registry.Deployments(context.Background())
	if err != nil {
		return err
	}
	for _, deployment := range deployments {
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

// credentialWindow 返回 Controller projection、edge access 与 daemon control 共用的部署密钥窗口。
// 零值只用于保持 development harness 的原有 24 小时行为；公网装配必须显式给出窗口，
// 避免进程运行一天后重启时因为重新推导窗口而接受已经越界的部署密钥。
func credentialWindow(now time.Time, notBeforeMillis, notAfterMillis int64) (time.Time, time.Time, error) {
	if notBeforeMillis == 0 && notAfterMillis == 0 {
		return now.Add(-time.Hour), now.Add(24 * time.Hour), nil
	}
	if notBeforeMillis == 0 || notAfterMillis == 0 {
		return time.Time{}, time.Time{}, fmt.Errorf("Controller credential window must be configured together")
	}
	notBefore := time.UnixMilli(notBeforeMillis).UTC()
	notAfter := time.UnixMilli(notAfterMillis).UTC()
	if !notAfter.After(notBefore) || now.Before(notBefore) || !now.Before(notAfter) {
		return time.Time{}, time.Time{}, fmt.Errorf("Controller credential window is invalid or inactive")
	}
	return notBefore, notAfter, nil
}

func origin(listener net.Listener) string { return "http://" + listener.Addr().String() }

func healthHandler(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
