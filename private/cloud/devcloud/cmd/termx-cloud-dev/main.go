// Package main 启动一个 Controller 与两个独立 Edge 子进程的 development supervisor。
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	cloudcommerce "github.com/lozzow/termx/private/cloud/control-plane/commerce"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	"github.com/lozzow/termx/private/cloud/controller"
	"github.com/lozzow/termx/private/cloud/edge"
	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
	"github.com/lozzow/termx/proto/cloudpb"
)

type processRecord struct {
	Name         string `json:"name"`
	PID          int    `json:"pid"`
	BinaryPath   string `json:"binary_path"`
	ConfigPath   string `json:"config_path"`
	ManifestPath string `json:"manifest_path"`
	LogPath      string `json:"log_path"`
}

type supervisorManifest struct {
	Version               uint32              `json:"version"`
	StartedAt             string              `json:"started_at"`
	Controller            controller.Manifest `json:"controller"`
	Edges                 []edge.Manifest     `json:"edges"`
	Processes             []processRecord     `json:"processes"`
	CompanionManifestPath string              `json:"companion_manifest_path"`
	CredentialsPath       string              `json:"credentials_path"`
}

type developmentAccount struct {
	Projection *cloudpb.AccountProjection
	Email      string
	Password   string
}

type developmentCredentials struct {
	PublicURL           string `json:"public_url"`
	AccountEmail        string `json:"account_email"`
	AccountPassword     string `json:"account_password"`
	OperatorURL         string `json:"operator_url"`
	OperatorID          string `json:"operator_id"`
	OperatorAccessToken string `json:"operator_access_token"`
}

type childProcess struct {
	record processRecord
	cmd    *exec.Cmd
	log    *os.File
	done   chan error
}

func seedDevelopmentAccount(databasePath, catalogPath string, now time.Time) (*developmentAccount, error) {
	store, err := cloudsqlite.Open(databasePath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	catalog, err := webcontroller.LoadCatalog(catalogPath)
	if err != nil {
		return nil, err
	}
	passwordBytes := make([]byte, 24)
	if _, err := rand.Read(passwordBytes); err != nil {
		return nil, err
	}
	password := base64.RawURLEncoding.EncodeToString(passwordBytes)
	clear(passwordBytes)
	service, err := cloudcommerce.New(cloudcommerce.Config{Store: store, Catalog: catalog.Contract(), Now: func() time.Time { return now }})
	if err != nil {
		return nil, err
	}
	email := "devcloud-fixture@termx.invalid"
	registered, err := service.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: email, Password: password})
	if err != nil {
		return nil, err
	}
	return &developmentAccount{Projection: registered.GetSession().GetAccount(), Email: email, Password: password}, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("termx-cloud-dev", flag.ContinueOnError)
	manifestPath := flags.String("manifest", ".artifacts/cloud-dev/runtime.json", "supervisor runtime manifest path")
	repoRoot := flags.String("repo-root", "", "repository root; defaults to current directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected termx-cloud-dev arguments")
	}
	root, err := resolveRepoRoot(*repoRoot)
	if err != nil {
		return err
	}
	artifactDir, err := filepath.Abs(filepath.Dir(*manifestPath))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		return err
	}
	controllerBinary := filepath.Join(artifactDir, "termx-cloud-controller")
	edgeBinary := filepath.Join(artifactDir, "termx-cloud-edge")
	if err := buildBinary(ctx, root, controllerBinary, "./private/cloud/controller/cmd/termx-cloud-controller"); err != nil {
		return err
	}
	if err := buildBinary(ctx, root, edgeBinary, "./private/cloud/edge/cmd/termx-cloud-edge"); err != nil {
		return err
	}
	webStaticDir := filepath.Join(root, "private/cloud/web-controller/web/dist")
	if err := buildWeb(ctx, filepath.Join(root, "private/cloud/web-controller/web")); err != nil {
		return err
	}

	now := time.Now().UTC()
	projectionPublic, projectionPrivate, _ := ed25519.GenerateKey(rand.Reader)
	projectionKeyID := "development-controller-projection"
	_, daemonControlPrivate, _ := ed25519.GenerateKey(rand.Reader)
	daemonControlKeyID := "development-daemon-control"
	controllerDatabase := filepath.Join(artifactDir, "controller.db")
	_ = os.Remove(controllerDatabase)
	catalogPath := filepath.Join(root, "private/cloud/web-controller/config/plans.json")
	development, err := seedDevelopmentAccount(controllerDatabase, catalogPath, now)
	if err != nil {
		return err
	}
	account := development.Projection
	operatorTokenBytes := make([]byte, 32)
	if _, err := rand.Read(operatorTokenBytes); err != nil {
		return err
	}
	operatorToken := base64.RawURLEncoding.EncodeToString(operatorTokenBytes)
	enrollmentBytes := make([]byte, 24)
	if _, err := rand.Read(enrollmentBytes); err != nil {
		return err
	}
	enrollmentCode := base64.RawURLEncoding.EncodeToString(enrollmentBytes)
	clear(enrollmentBytes)
	devices := []*cloudpb.CloudDevicePolicy{
		{AccountId: account.GetAccountId(), DeviceId: "client-dev-local", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, AuthEpoch: account.GetAuthRevision()},
		{AccountId: account.GetAccountId(), DeviceId: "daemon-edge-a", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: account.GetAuthRevision()},
		{AccountId: account.GetAccountId(), DeviceId: "daemon-edge-b", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: account.GetAuthRevision()},
	}
	assignments := []*cloudpb.HubAssignment{
		{DaemonDeviceId: "daemon-edge-a", AccountId: account.GetAccountId(), HubId: "hub-edge-a", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()},
		{DaemonDeviceId: "daemon-edge-b", AccountId: account.GetAccountId(), HubId: "hub-edge-b", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()},
	}
	deploymentConfigs := make([]controller.DeploymentConfig, 0, 2)
	edgeKeys := map[string]ed25519.PrivateKey{}
	relayKeys := map[string]ed25519.PrivateKey{}
	for _, hubID := range []string{"hub-edge-a", "hub-edge-b"} {
		hubPublic, hubPrivate, _ := ed25519.GenerateKey(rand.Reader)
		relayPublic, relayPrivate, _ := ed25519.GenerateKey(rand.Reader)
		edgeID := "edge-" + hubID
		metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: edgeID, Region: "local-1", PublicLabel: hubID, HubId: hubID, HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublic), RelayId: "relay-" + hubID, RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayPublic)}
		deploymentConfigs = append(deploymentConfigs, controller.DeploymentConfig{Metadata: metadata, HubControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(hubPublic), RelayControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(relayPublic)})
		edgeKeys[hubID] = hubPrivate
		relayKeys[hubID] = relayPrivate
	}
	controllerConfig := controller.Config{DatabasePath: controllerDatabase, PublicListen: "127.0.0.1:0", InternalControlListen: "127.0.0.1:0", OperatorListen: "127.0.0.1:0", CatalogPath: catalogPath, ProjectionKeyID: projectionKeyID, ProjectionPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPrivate), DaemonControlKeyID: daemonControlKeyID, DaemonControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(daemonControlPrivate), EnableTestPaymentProvider: true, Deployments: deploymentConfigs, Devices: devices, Assignments: assignments, DevelopmentEnrollmentCode: enrollmentCode, DevelopmentEnrollmentAccountID: account.GetAccountId(), DevelopmentEnrollmentHubID: "hub-edge-a", OperatorID: "development-admin", OperatorRole: "admin", OperatorAccessTokenBase64: base64.RawStdEncoding.EncodeToString(operatorTokenBytes), WebStaticDir: webStaticDir}
	clear(operatorTokenBytes)
	controllerConfigPath := filepath.Join(artifactDir, "controller-config.json")
	controllerManifestPath := filepath.Join(artifactDir, "controller-runtime.json")
	if err := writeJSONFile(controllerConfigPath, controllerConfig); err != nil {
		return err
	}
	controllerChild, err := startChild(controllerBinary, controllerConfigPath, controllerManifestPath, filepath.Join(artifactDir, "controller.log"), "controller")
	if err != nil {
		return err
	}
	children := []*childProcess{controllerChild}
	defer func() { stopChildren(children) }()
	controllerRuntime := controller.Manifest{}
	if err := waitManifest(ctx, controllerManifestPath, &controllerRuntime, controllerChild.done); err != nil {
		return err
	}
	if err := waitHealth(ctx, controllerRuntime.OperatorURL+"/healthz", true); err != nil {
		return err
	}
	credentialsPath := filepath.Join(artifactDir, "development-credentials.json")
	if err := writeJSONFile(credentialsPath, developmentCredentials{PublicURL: controllerRuntime.PublicURL, AccountEmail: development.Email, AccountPassword: development.Password, OperatorURL: controllerRuntime.OperatorURL, OperatorID: "development-admin", OperatorAccessToken: operatorToken}); err != nil {
		return err
	}

	edgeManifests := make([]edge.Manifest, 0, len(deploymentConfigs))
	for _, deployment := range deploymentConfigs {
		hubID := deployment.Metadata.GetHubId()
		config := edge.Config{ControllerURL: controllerRuntime.InternalControlURL, HubListen: "127.0.0.1:0", HealthListen: "127.0.0.1:0", RelayListen: "127.0.0.1:0", UsageOutboxPath: filepath.Join(artifactDir, hubID+"-usage.outbox"), Metadata: deployment.Metadata, HubControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(edgeKeys[hubID]), RelayControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(relayKeys[hubID]), ControllerProjectionKeyID: projectionKeyID, ControllerProjectionPublicKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPublic)}
		configPath := filepath.Join(artifactDir, hubID+"-config.json")
		manifestPath := filepath.Join(artifactDir, hubID+"-runtime.json")
		if err := writeJSONFile(configPath, config); err != nil {
			return err
		}
		child, err := startChild(edgeBinary, configPath, manifestPath, filepath.Join(artifactDir, hubID+".log"), hubID)
		if err != nil {
			return err
		}
		children = append(children, child)
		var runtime edge.Manifest
		if err := waitManifest(ctx, manifestPath, &runtime, child.done); err != nil {
			return err
		}
		if err := waitHealth(ctx, runtime.HealthURL+"/healthz", true); err != nil {
			return err
		}
		edgeManifests = append(edgeManifests, runtime)
	}
	companionManifestPath := filepath.Join(artifactDir, "companion-manifest.json")
	var primaryEdge edge.Manifest
	for _, value := range edgeManifests {
		if value.HubID == "hub-edge-a" {
			primaryEdge = value
			break
		}
	}
	if primaryEdge.HubID == "" {
		return fmt.Errorf("development primary Edge did not start")
	}
	if err := writeJSONFile(companionManifestPath, httpapi.Manifest{Version: httpapi.ManifestVersion, Profile: httpapi.ProfileDevLocal, ControlPlaneURL: controllerRuntime.PublicURL, HubURL: primaryEdge.HubURL, RelayURL: primaryEdge.RelayURL, HubID: primaryEdge.HubID, Region: "local-1", AccountLabel: account.GetDisplayName(), EnrollmentCode: enrollmentCode, StartedAtRFC3339: now.Format(time.RFC3339)}); err != nil {
		return err
	}
	manifest := supervisorManifest{Version: 1, StartedAt: now.Format(time.RFC3339Nano), Controller: controllerRuntime, Edges: edgeManifests, CompanionManifestPath: companionManifestPath, CredentialsPath: credentialsPath}
	for _, child := range children {
		manifest.Processes = append(manifest.Processes, child.record)
	}
	if err := writeJSONFile(*manifestPath, manifest); err != nil {
		return err
	}
	_ = json.NewEncoder(os.Stdout).Encode(manifest)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		for _, child := range children {
			select {
			case err := <-child.done:
				return fmt.Errorf("%s exited: %w", child.record.Name, err)
			default:
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func resolveRepoRoot(value string) (string, error) {
	if value == "" {
		value, _ = os.Getwd()
	}
	root, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		return "", fmt.Errorf("repository root does not contain go.work")
	}
	return root, nil
}

func buildBinary(ctx context.Context, root, output, packagePath string) error {
	command := exec.CommandContext(ctx, "go", "build", "-o", output, packagePath)
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK="+filepath.Join(root, "go.work"))
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("build %s: %w", packagePath, err)
	}
	return nil
}

func buildWeb(ctx context.Context, directory string) error {
	command := exec.CommandContext(ctx, "npm", "run", "build")
	command.Dir = directory
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("build Web Controller: %w", err)
	}
	return nil
}

func startChild(binary, configPath, manifestPath, logPath, name string) (*childProcess, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	command := exec.Command(binary, "--config", configPath, "--manifest", manifestPath)
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	child := &childProcess{record: processRecord{Name: name, PID: command.Process.Pid, BinaryPath: binary, ConfigPath: configPath, ManifestPath: manifestPath, LogPath: logPath}, cmd: command, log: logFile, done: make(chan error, 1)}
	go func() {
		err := command.Wait()
		_ = logFile.Close()
		child.done <- err
	}()
	return child, nil
}

func stopChildren(children []*childProcess) {
	for index := len(children) - 1; index >= 0; index-- {
		child := children[index]
		if child.cmd.Process != nil {
			_ = child.cmd.Process.Signal(syscall.SIGTERM)
		}
	}
	for _, child := range children {
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-child.done:
			timer.Stop()
		case <-timer.C:
			if child.cmd.Process != nil {
				_ = child.cmd.Process.Kill()
			}
			select {
			case <-child.done:
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func waitManifest(ctx context.Context, path string, target any, done <-chan error) error {
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		body, err := os.ReadFile(path)
		if err == nil && json.Unmarshal(body, target) == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			return fmt.Errorf("child exited before manifest: %w", err)
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s", path)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func waitHealth(ctx context.Context, endpoint string, requireSuccess bool) error {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		response, err := http.Get(endpoint)
		if err == nil {
			_ = response.Body.Close()
			if requireSuccess && response.StatusCode == http.StatusNoContent || !requireSuccess && response.StatusCode != 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s", endpoint)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func writeJSONFile(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
