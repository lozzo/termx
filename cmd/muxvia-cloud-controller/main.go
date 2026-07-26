// Package main 是 muxvia-cloud-controller 的进程组装入口。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/controller/apihttp"
	"github.com/muxvia/muxvia/cloud/controller/control"
	"github.com/muxvia/muxvia/cloud/controller/directory"
	"github.com/muxvia/muxvia/cloud/controller/directoryapi"
	"github.com/muxvia/muxvia/cloud/controller/edgeconfig"
	"github.com/muxvia/muxvia/cloud/controller/enrollment"
	"github.com/muxvia/muxvia/cloud/controller/install"
	"github.com/muxvia/muxvia/cloud/controller/postgres"
	controllerruntime "github.com/muxvia/muxvia/cloud/controller/runtime"
	"github.com/muxvia/muxvia/cloud/keymaterial"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
)

const softwareVersion = "development"

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
	operatorUsername     string
	operatorPasswordFile string
	configSigningKey     string
	configSigningKeyID   string
	artifactFile         string
	artifactVersion      string
	artifactSigningKey   string
	ticketSigningKey     string
	ticketSigningKeyID   string
	heartbeatInterval    time.Duration
	heartbeatTimeout     time.Duration
	relayLeaseTTL        time.Duration
	relayMaxBytes        uint64
	relayMaxRate         uint64
	relayMaxAllocations  uint
	startupTimeout       time.Duration
	shutdownTimeout      time.Duration
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
	if config.startupTimeout <= 0 || config.shutdownTimeout <= 0 || config.relayLeaseTTL <= 0 || config.relayLeaseTTL > 5*time.Minute || config.relayMaxBytes == 0 || config.relayMaxRate == 0 || config.relayMaxAllocations == 0 || config.relayMaxAllocations > math.MaxUint32 {
		return errors.New("startup, shutdown, and bounded Relay policy values must be positive")
	}
	startupContext, cancelStartup := context.WithTimeout(ctx, config.startupTimeout)
	database, err := postgres.Open(startupContext, getenv("MUXVIA_CLOUD_DATABASE_URL"))
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
	ticketKey, err := keymaterial.LoadEd25519PrivateKey(config.ticketSigningKey)
	if err != nil {
		return err
	}
	edgeCAPayload, err := os.ReadFile(filepath.Clean(config.edgeCA))
	if err != nil {
		return fmt.Errorf("read Edge CA: %w", err)
	}
	passwordPayload, err := os.ReadFile(filepath.Clean(config.operatorPasswordFile))
	if err != nil {
		return fmt.Errorf("read operator password: %w", err)
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

	directoryState, err := directory.New(directory.Config{MailboxSize: 4096, GracePeriod: 10 * time.Second})
	if err != nil {
		return err
	}
	defer directoryState.Close()
	enrollmentService, err := enrollment.NewService(enrollment.Config{
		Store: database, Edges: edgeService, Directory: directoryState, TicketSigningKey: ticketKey, TicketSigningKeyID: config.ticketSigningKeyID,
		EdgeCACertificate: edgeCAPayload, EnrollmentTTL: 10 * time.Minute, ChallengeTTL: time.Minute, AgentTicketTTL: 10 * time.Minute,
	})
	if err != nil {
		return err
	}
	clientDirectoryService, err := directoryapi.NewService(directoryapi.Config{
		Store: database, Directory: directoryState, Edges: edgeService, EdgeCACertificate: edgeCAPayload,
		TicketSigningKey: ticketKey, TicketSigningKeyID: config.ticketSigningKeyID, ChallengeTTL: time.Minute, ClientTicketTTL: 2 * time.Minute,
	})
	if err != nil {
		return err
	}
	service, err := control.NewService(control.Config{
		ControllerID:           config.controllerID,
		ControllerBootID:       uuid.NewString(),
		HeartbeatInterval:      config.heartbeatInterval,
		HeartbeatTimeout:       config.heartbeatTimeout,
		Directory:              directoryState,
		TicketVerificationKeys: []*cloudv1.VerificationKey{enrollmentService.TicketVerificationKey()},
		TicketSigningKey:       ticketKey, TicketSigningKeyID: config.ticketSigningKeyID, RelayLeaseTTL: config.relayLeaseTTL,
		RelayPolicy: control.ConfiguredRelayPolicy{Value: control.RelayLimits{MaxBytes: config.relayMaxBytes, MaxRateBytesPerSecond: config.relayMaxRate, MaxConcurrentAllocations: uint32(config.relayMaxAllocations)}},
		UsageStore:  database,
		DesiredConfig: func(ctx context.Context, edgeID string) (*cloudv1.SignedEdgeDesiredConfig, error) {
			edge, err := edgeService.GetEdge(ctx, edgeID)
			return edge.SignedConfig, err
		},
	})
	if err != nil {
		return err
	}
	runtime, err := controllerruntime.Start(controllerruntime.Config{
		GRPCListenAddress:   config.grpcListen,
		HealthListenAddress: config.healthListen,
		TLSCertificateFile:  config.tlsCertificate,
		TLSPrivateKeyFile:   config.tlsPrivateKey,
		EdgeCAFile:          config.edgeCA,
	}, service)
	if err != nil {
		return err
	}
	httpServer, err := apihttp.Start(apihttp.Config{ListenAddress: config.httpListen, TLSCertificateFile: config.tlsCertificate, TLSPrivateKeyFile: config.tlsPrivateKey, PublicOrigin: config.publicOrigin, OperatorUsername: config.operatorUsername, OperatorPassword: strings.TrimSpace(string(passwordPayload)), Edges: edgeService, Directory: directoryState, Install: installService, Enrollment: enrollmentService, ClientDirectory: clientDirectoryService})
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
	flags := flag.NewFlagSet("muxvia-cloud-controller", flag.ContinueOnError)
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
	flags.StringVar(&config.operatorUsername, "operator-username", "operator", "R3 operator Basic Auth username")
	flags.StringVar(&config.operatorPasswordFile, "operator-password-file", "", "R3 operator Basic Auth password file")
	flags.StringVar(&config.configSigningKey, "config-signing-key", "", "Edge desired config Ed25519 private key")
	flags.StringVar(&config.configSigningKeyID, "config-signing-key-id", "", "Edge desired config key ID")
	flags.StringVar(&config.artifactFile, "edge-artifact", "", "signed linux/amd64 Edge artifact")
	flags.StringVar(&config.artifactVersion, "edge-artifact-version", softwareVersion, "Edge artifact immutable version")
	flags.StringVar(&config.artifactSigningKey, "artifact-signing-key", "", "Edge artifact Ed25519 private key")
	flags.StringVar(&config.ticketSigningKey, "ticket-signing-key", "", "Cloud ticket Ed25519 private key")
	flags.StringVar(&config.ticketSigningKeyID, "ticket-signing-key-id", "", "Cloud ticket signing key ID")
	flags.DurationVar(&config.heartbeatInterval, "heartbeat-interval", 10*time.Second, "Edge heartbeat interval")
	flags.DurationVar(&config.heartbeatTimeout, "heartbeat-timeout", 30*time.Second, "Edge heartbeat timeout")
	flags.DurationVar(&config.relayLeaseTTL, "relay-lease-ttl", 5*time.Minute, "maximum lifetime of a signed RelayLease")
	flags.Uint64Var(&config.relayMaxBytes, "relay-max-bytes", 1<<30, "maximum bytes allowed by one development RelayLease")
	flags.Uint64Var(&config.relayMaxRate, "relay-max-rate", 10<<20, "maximum bytes per second allowed by one development RelayLease")
	flags.UintVar(&config.relayMaxAllocations, "relay-max-allocations", 2, "maximum concurrent allocations allowed by one development RelayLease")
	flags.DurationVar(&config.startupTimeout, "startup-timeout", 15*time.Second, "PostgreSQL startup deadline")
	flags.DurationVar(&config.shutdownTimeout, "shutdown-timeout", 15*time.Second, "graceful shutdown deadline")
	if err := flags.Parse(arguments); err != nil {
		return options{}, fmt.Errorf("parse Controller flags: %w", err)
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected Controller arguments: %s", strings.Join(flags.Args(), " "))
	}
	return config, nil
}
