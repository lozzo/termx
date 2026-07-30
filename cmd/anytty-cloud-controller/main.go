// Package main 是 anytty-cloud-controller 的进程组装入口。
package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	"github.com/anytty/anytty/cloud/controller/apihttp"
	controllerbindingkeys "github.com/anytty/anytty/cloud/controller/bindingkeys"
	"github.com/anytty/anytty/cloud/controller/certificate"
	"github.com/anytty/anytty/cloud/controller/commerce"
	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/directoryapi"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/controller/enrollment"
	"github.com/anytty/anytty/cloud/controller/install"
	operatorservice "github.com/anytty/anytty/cloud/controller/operator"
	"github.com/anytty/anytty/cloud/controller/postgres"
	controllerruntime "github.com/anytty/anytty/cloud/controller/runtime"
	"github.com/anytty/anytty/cloud/keymaterial"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
)

const (
	softwareVersion = "development"
)

type options struct {
	migrate              bool
	controllerID         string
	grpcListen           string
	httpListen           string
	healthListen         string
	tlsCertificate       string
	tlsPrivateKey        string
	edgeCA               string
	edgeCAKey            string
	controllerCA         string
	publicOrigin         string
	controllerAddress    string
	controllerServerName string
	certificateSecretDir string
	operatorUsername     string
	operatorPasswordFile string
	configSigningKey     string
	configSigningKeyID   string
	artifactFile         string
	artifactVersion      string
	artifactSigningKey   string
	bindingSigningKey    string
	bindingSigningKeyID  string
	heartbeatInterval    time.Duration
	heartbeatTimeout     time.Duration
	developmentPayments  bool
	startupTimeout       time.Duration
	shutdownTimeout      time.Duration
	trustedProxyCIDRs    []netip.Prefix
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, logger); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		logger.Error("Controller 退出", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, getenv func(string) string, logger *slog.Logger) error {
	config, err := parseOptions(arguments, os.Stderr)
	if err != nil {
		return err
	}
	if config.startupTimeout <= 0 || config.shutdownTimeout <= 0 {
		return errors.New("startup and shutdown timeouts must be positive")
	}
	startupContext, cancelStartup := context.WithTimeout(ctx, config.startupTimeout)
	database, err := postgres.Open(startupContext, getenv("ANYTTY_CLOUD_DATABASE_URL"))
	cancelStartup()
	if err != nil {
		return err
	}
	defer database.Close()
	if config.migrate {
		migrationContext, cancelMigration := context.WithTimeout(ctx, config.startupTimeout)
		defer cancelMigration()
		return database.Migrate(migrationContext)
	}
	if strings.TrimSpace(config.controllerID) == "" {
		return errors.New("--controller-id is required")
	}
	if err := database.VerifySchema(ctx); err != nil {
		return err
	}
	configKey, err := keymaterial.LoadEd25519PrivateKey(config.configSigningKey)
	if err != nil {
		return err
	}
	artifactKey, err := keymaterial.LoadEd25519PrivateKey(config.artifactSigningKey)
	if err != nil {
		return err
	}
	bindingKey, err := keymaterial.LoadEd25519PrivateKey(config.bindingSigningKey)
	if err != nil {
		return err
	}
	bindingKeyOwner, err := controllerbindingkeys.New(ctx, controllerbindingkeys.Config{
		Store: database, TTL: 24 * time.Hour,
		Keys: []*cloudv1.VerificationKey{{KeyId: strings.TrimSpace(config.bindingSigningKeyID), Algorithm: "Ed25519", PublicKey: append([]byte(nil), bindingKey.Public().(ed25519.PublicKey)...)}},
	})
	if err != nil {
		return fmt.Errorf("initialize binding key bundle: %w", err)
	}
	edgeCAPayload, err := os.ReadFile(filepath.Clean(config.edgeCA))
	if err != nil {
		return fmt.Errorf("read Edge CA: %w", err)
	}
	var passwordPayload []byte
	if strings.TrimSpace(config.operatorPasswordFile) != "" {
		passwordPayload, err = os.ReadFile(filepath.Clean(config.operatorPasswordFile))
		if err != nil {
			return fmt.Errorf("read operator password: %w", err)
		}
	}
	accountService, err := account.New(account.Config{Store: database, AccessTTL: 15 * time.Minute, RefreshTTL: 30 * 24 * time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: 24 * time.Hour})
	if err != nil {
		return err
	}
	if _, err := accountService.EnsureBootstrapOperator(ctx, config.operatorUsername, strings.TrimSpace(string(passwordPayload))); err != nil {
		return fmt.Errorf("ensure bootstrap operator: %w", err)
	}
	commerceService, err := commerce.New(commerce.Config{Store: database, DevelopmentPayments: config.developmentPayments})
	if err != nil {
		return err
	}
	edgeService, err := edgeconfig.NewService(edgeconfig.Config{Store: database, SigningKey: configKey, SigningKeyID: config.configSigningKeyID, ClaimTTL: 10 * time.Minute})
	if err != nil {
		return err
	}
	installService, err := install.NewService(install.Config{
		Edges: edgeService, PublicOrigin: config.publicOrigin, ControllerAddress: config.controllerAddress, ControllerServerName: config.controllerServerName,
		EdgeCACertificateFile: config.edgeCA, EdgeCAPrivateKeyFile: config.edgeCAKey, ControllerCAFile: config.controllerCA,
		ArtifactFile: config.artifactFile, ArtifactVersion: config.artifactVersion, ArtifactSigningKey: artifactKey, CertificateValidity: 30 * 24 * time.Hour,
	})
	if err != nil {
		return err
	}

	directoryState, err := directory.New(directory.Config{MailboxSize: 4096, GracePeriod: 10 * time.Second, WatcherMailboxSize: 256})
	if err != nil {
		return err
	}
	defer directoryState.Close()
	secretStore, err := certificate.NewFileSecretStore(config.certificateSecretDir)
	if err != nil {
		return fmt.Errorf("open certificate secret store: %w", err)
	}
	var service *control.Service
	certificateService, err := certificate.New(certificate.Config{
		Store: database, Secrets: secretStore, Edges: edgeService,
		Dispatcher: certificate.DispatcherFunc(func(ctx context.Context, edgeID string) error {
			if service == nil {
				return errors.New("EdgeControl is not ready")
			}
			return service.RefreshCertificate(ctx, edgeID)
		}),
		Online: func(ctx context.Context, edgeID string) (bool, error) {
			_, found, err := directoryState.Edge(ctx, edgeID)
			return found, err
		},
	})
	if err != nil {
		return err
	}
	enrollmentService, err := enrollment.NewService(enrollment.Config{
		Store: database, Edges: edgeService, Directory: directoryState, BindingSigningKey: bindingKey, BindingSigningKeyID: config.bindingSigningKeyID,
		Entitlement:       commerceService,
		EdgeCACertificate: edgeCAPayload, EnrollmentTTL: 10 * time.Minute, ChallengeTTL: time.Minute, BindingTTL: 365 * 24 * time.Hour,
	})
	if err != nil {
		return err
	}
	clientDirectoryService, err := directoryapi.NewService(directoryapi.Config{
		Store: database, Directory: directoryState, Edges: edgeService, EdgeCACertificate: edgeCAPayload,
		ChallengeTTL: time.Minute, Entitlement: commerceService,
	})
	if err != nil {
		return err
	}
	service, err = control.NewService(control.Config{
		ControllerID:      config.controllerID,
		ControllerBootID:  uuid.NewString(),
		HeartbeatInterval: config.heartbeatInterval,
		HeartbeatTimeout:  config.heartbeatTimeout,
		Directory:         directoryState,
		BindingKeyBundle:  bindingKeyOwner.Bundle,
		RelayStore:        database,
		DesiredConfig: func(ctx context.Context, edgeID string) (*cloudv1.SignedEdgeDesiredConfig, error) {
			edge, err := edgeService.GetEdge(ctx, edgeID)
			return edge.SignedConfig, err
		},
		DesiredCertificate: certificateService.BundleForEdge,
		CertificateApplied: certificateService.RecordApplied,
	})
	if err != nil {
		return err
	}
	daemonManagementService, err := enrollment.NewManagementService(enrollment.ManagementConfig{
		Enrollment: enrollmentService, Store: database, Directory: directoryState, Control: service,
		CommandPrefix: "anytty cloud enroll --controller " + strings.TrimRight(config.publicOrigin, "/"),
	})
	if err != nil {
		return err
	}
	operatorService, err := operatorservice.New(operatorservice.Config{Store: database, Edges: edgeService, Enrollment: enrollmentService, Directory: directoryState, Control: service, Certificates: certificateService, Accounts: accountService})
	if err != nil {
		return err
	}
	runtime, err := controllerruntime.Start(controllerruntime.Config{
		GRPCListenAddress:   config.grpcListen,
		HealthListenAddress: config.healthListen,
		TLSCertificateFile:  config.tlsCertificate,
		TLSPrivateKeyFile:   config.tlsPrivateKey,
		EdgeCAFile:          config.edgeCA,
		BindingKeyOwnership: bindingKeyOwner,
	}, service)
	if err != nil {
		return err
	}
	httpServer, err := apihttp.Start(apihttp.Config{ListenAddress: config.httpListen, TLSCertificateFile: config.tlsCertificate, TLSPrivateKeyFile: config.tlsPrivateKey, PublicOrigin: config.publicOrigin, Edges: edgeService, Directory: directoryState, Install: installService, Enrollment: enrollmentService, DaemonManagement: daemonManagementService, ClientDirectory: clientDirectoryService, Accounts: accountService, Commerce: commerceService, Operator: operatorService, Certificates: certificateService, TrustedProxyCIDRs: config.trustedProxyCIDRs, Logger: logger})
	if err != nil {
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), config.shutdownTimeout)
		defer cancelShutdown()
		_ = runtime.Shutdown(shutdownContext)
		return err
	}
	logger.Info("Controller 已启动", "version", softwareVersion, "grpc_address", runtime.GRPCAddress(), "http_address", httpServer.Address(), "health_address", runtime.HealthAddress())

	var runtimeErr error
	select {
	case <-ctx.Done():
	case runtimeErr = <-runtime.Errors():
	case runtimeErr = <-httpServer.Errors():
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), config.shutdownTimeout)
	defer cancelShutdown()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown Controller HTTPS: %w", err)
	}
	if err := runtime.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown Controller: %w", err)
	}
	if runtimeErr != nil {
		return runtimeErr
	}
	logger.Info("Controller 已停止")
	return nil
}

func parseOptions(arguments []string, output io.Writer) (options, error) {
	var config options
	flags := flag.NewFlagSet("anytty-cloud-controller", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.BoolVar(&config.migrate, "migrate", false, "apply pending PostgreSQL migrations and exit")
	flags.StringVar(&config.controllerID, "controller-id", "", "stable Controller identity")
	flags.StringVar(&config.grpcListen, "grpc-listen", "0.0.0.0:8443", "mTLS gRPC listen address")
	flags.StringVar(&config.httpListen, "http-listen", "0.0.0.0:8444", "native HTTPS operator/install listen address")
	flags.StringVar(&config.healthListen, "health-listen", "127.0.0.1:8081", "loopback health listen address")
	flags.StringVar(&config.tlsCertificate, "tls-cert", "", "Controller TLS certificate file")
	flags.StringVar(&config.tlsPrivateKey, "tls-key", "", "Controller TLS private key file")
	flags.StringVar(&config.edgeCA, "edge-ca", "", "EdgeIdentity CA certificate file")
	flags.StringVar(&config.edgeCAKey, "edge-ca-key", "", "EdgeIdentity CA private key file")
	flags.StringVar(&config.controllerCA, "controller-ca", "", "Controller certificate chain returned to Edge")
	flags.StringVar(&config.publicOrigin, "public-origin", "", "public HTTPS origin for operator and installer")
	flags.StringVar(&config.controllerAddress, "controller-address", "", "public EdgeControl address returned to Edge")
	flags.StringVar(&config.controllerServerName, "controller-server-name", "", "EdgeControl TLS server name returned to Edge")
	flags.StringVar(&config.certificateSecretDir, "certificate-secret-dir", "/var/lib/anytty-cloud-controller/certificates", "restricted Controller service directory for managed Edge certificate files")
	flags.StringVar(&config.operatorUsername, "operator-username", "", "bootstrap administrator email")
	flags.StringVar(&config.operatorPasswordFile, "operator-password-file", "", "bootstrap administrator password file")
	flags.StringVar(&config.configSigningKey, "config-signing-key", "", "Edge desired config Ed25519 private key")
	flags.StringVar(&config.configSigningKeyID, "config-signing-key-id", "", "Edge desired config key ID")
	flags.StringVar(&config.artifactFile, "edge-artifact", "", "signed linux/amd64 Edge artifact")
	flags.StringVar(&config.artifactVersion, "edge-artifact-version", softwareVersion, "Edge artifact immutable version")
	flags.StringVar(&config.artifactSigningKey, "artifact-signing-key", "", "Edge artifact Ed25519 private key")
	flags.StringVar(&config.bindingSigningKey, "binding-signing-key", "", "daemon binding Ed25519 private key")
	flags.StringVar(&config.bindingSigningKeyID, "binding-signing-key-id", "", "daemon binding signing key ID")
	flags.DurationVar(&config.heartbeatInterval, "heartbeat-interval", 10*time.Second, "Edge heartbeat interval")
	flags.DurationVar(&config.heartbeatTimeout, "heartbeat-timeout", 30*time.Second, "Edge heartbeat timeout")
	flags.BoolVar(&config.developmentPayments, "development-payments", false, "enable the Development-only self-service payment adapter")
	flags.DurationVar(&config.startupTimeout, "startup-timeout", 15*time.Second, "PostgreSQL startup deadline")
	flags.DurationVar(&config.shutdownTimeout, "shutdown-timeout", 15*time.Second, "graceful shutdown deadline")
	flags.Var((*trustedProxyCIDRFlag)(&config.trustedProxyCIDRs), "trusted-proxy-cidr", "trusted direct HTTP proxy CIDR; repeat for each proxy network")
	if err := flags.Parse(arguments); err != nil {
		return options{}, fmt.Errorf("parse Controller flags: %w", err)
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected Controller arguments: %s", strings.Join(flags.Args(), " "))
	}
	return config, nil
}

type trustedProxyCIDRFlag []netip.Prefix

func (values *trustedProxyCIDRFlag) String() string {
	parts := make([]string, 0, len(*values))
	for _, prefix := range *values {
		parts = append(parts, prefix.String())
	}
	return strings.Join(parts, ",")
}

func (values *trustedProxyCIDRFlag) Set(value string) error {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("invalid trusted proxy CIDR %q: %w", value, err)
	}
	*values = append(*values, prefix.Masked())
	return nil
}
