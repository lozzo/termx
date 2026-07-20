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

	"github.com/lozzow/termx/private/cloud/control-plane/hubcontrol"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
)

// DeploymentConfig 绑定一个 Hub deployment metadata 与其独立 control public key。
type DeploymentConfig struct {
	Metadata                  *cloudpb.EdgeDeploymentMetadata `json:"metadata"`
	HubControlPublicKeyBase64 string                          `json:"hub_control_public_key_base64"`
}

// Config 是 Controller development composition 的显式配置。
type Config struct {
	DatabasePath               string                       `json:"database_path"`
	PublicListen               string                       `json:"public_listen"`
	InternalControlListen      string                       `json:"internal_control_listen"`
	OperatorListen             string                       `json:"operator_listen"`
	CatalogPath                string                       `json:"catalog_path"`
	ProjectionKeyID            string                       `json:"projection_key_id"`
	ProjectionPrivateKeyBase64 string                       `json:"projection_private_key_base64"`
	Deployments                []DeploymentConfig           `json:"deployments"`
	Accounts                   []*cloudpb.HubAccountPolicy  `json:"accounts"`
	Devices                    []*cloudpb.CloudDevicePolicy `json:"devices"`
	Assignments                []*cloudpb.HubAssignment     `json:"assignments"`
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
	store     *cloudsqlite.Store
	publisher *hubcontrol.Publisher
	registry  *hubregistry.Registry
	topology  *cloudtopology.Service
	manifest  Manifest
	listeners []net.Listener
	servers   []*http.Server
	errors    chan error
	closeOnce sync.Once
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
	if config.DatabasePath == "" || config.CatalogPath == "" || config.ProjectionKeyID == "" || len(config.Deployments) < 1 {
		return nil, fmt.Errorf("Controller database, catalog, projection key and deployments are required")
	}
	privateKeyBytes, err := base64.RawStdEncoding.DecodeString(config.ProjectionPrivateKeyBase64)
	if err != nil || len(privateKeyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Controller projection private key")
	}
	privateKey := ed25519.PrivateKey(privateKeyBytes)
	store, err := cloudsqlite.Open(config.DatabasePath)
	if err != nil {
		return nil, err
	}
	registry, _ := hubregistry.New(store)
	topologyService, err := cloudtopology.New(registry, store)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	now := time.Now().UTC()
	for _, device := range config.Devices {
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
		if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: deployment.Metadata, ControlPublicKey: ed25519.PublicKey(publicKey), Enabled: true, UpdatedAt: now}); err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	for _, assignment := range config.Assignments {
		current, currentErr := registry.Assignment(context.Background(), assignment.GetDaemonDeviceId())
		if currentErr == nil && equalAssignment(current.Value, assignment) {
			continue
		}
		if _, err := registry.Assign(context.Background(), assignment, now); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("publish initial assignment %q: %w", assignment.GetDaemonDeviceId(), err)
		}
	}
	publisher := hubcontrol.NewPublisher()
	for _, deployment := range config.Deployments {
		hubID := deployment.Metadata.GetHubId()
		revision, err := store.AllocateProjectionRevision(context.Background(), hubID, now)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		accounts, devices, assignments, err := projectionForHub(registry, config, hubID, now)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		full, err := hubcontrol.BuildSignedFullProjection(hubcontrol.FullProjectionInput{HubID: hubID, Revision: revision, GeneratedAt: now, TTL: 30 * time.Minute, Accounts: accounts, Devices: devices, Assignments: assignments, SigningKeyID: config.ProjectionKeyID, SigningKey: privateKey})
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
	controlServer, err := hubcontrol.NewServer(hubcontrol.ServerConfig{Registry: registry, CursorStore: store, Publisher: publisher, Topology: topologyService, Clock: time.Now, ChallengeTTL: 30 * time.Second, EnvelopeTTL: 5 * time.Minute})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	catalog, err := webcontroller.LoadCatalog(config.CatalogPath)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/healthz", healthHandler)
	publicMux.HandleFunc("/api/v1/catalog", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := protojson.Marshal(&cloudpb.GetPlanCatalogResponse{Catalog: catalog.Contract()})
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(body)
	})
	operatorMux := http.NewServeMux()
	operatorMux.HandleFunc("/healthz", healthHandler)
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
	runtime := &Runtime{store: store, publisher: publisher, registry: registry, topology: topologyService, listeners: []net.Listener{publicListener, internalListener, operatorListener}, errors: make(chan error, 3)}
	runtime.servers = []*http.Server{{Handler: publicMux, ReadHeaderTimeout: 5 * time.Second}, {Handler: controlServer.Handler(), ReadHeaderTimeout: 5 * time.Second}, {Handler: operatorMux, ReadHeaderTimeout: 5 * time.Second}}
	for index := range runtime.servers {
		server, listener := runtime.servers[index], runtime.listeners[index]
		go func() {
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				runtime.errors <- err
			}
		}()
	}
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
		for _, server := range runtime.servers {
			if err := server.Shutdown(ctx); err != nil && result == nil {
				result = err
			}
		}
		if err := runtime.store.Close(); err != nil && result == nil {
			result = err
		}
	})
	return result
}

func projectionForHub(registry *hubregistry.Registry, config Config, hubID string, now time.Time) ([]*cloudpb.HubAccountPolicy, []*cloudpb.CloudDevicePolicy, []*cloudpb.HubAssignment, error) {
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
	for _, value := range config.Accounts {
		if accountsNeeded[value.GetAccountId()] {
			accounts = append(accounts, value)
		}
	}
	var devices []*cloudpb.CloudDevicePolicy
	for _, value := range config.Devices {
		if accountsNeeded[value.GetAccountId()] {
			devices = append(devices, value)
		}
	}
	return accounts, devices, assignments, nil
}

func equalAssignment(left, right *cloudpb.HubAssignment) bool {
	if left == nil || right == nil {
		return false
	}
	return left.GetDaemonDeviceId() == right.GetDaemonDeviceId() && left.GetAccountId() == right.GetAccountId() && left.GetHubId() == right.GetHubId() && left.GetAssignmentEpoch() == right.GetAssignmentEpoch() && left.GetNotBeforeUnixMillis() == right.GetNotBeforeUnixMillis() && left.GetExpiresAtUnixMillis() == right.GetExpiresAtUnixMillis()
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
