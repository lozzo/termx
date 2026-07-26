// Package main 是 muxvia-cloud-controller 的进程组装入口。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/controller/control"
	"github.com/muxvia/muxvia/cloud/controller/directory"
	"github.com/muxvia/muxvia/cloud/controller/postgres"
	controllerruntime "github.com/muxvia/muxvia/cloud/controller/runtime"
)

const softwareVersion = "development"

type options struct {
	controllerID      string
	grpcListen        string
	healthListen      string
	tlsCertificate    string
	tlsPrivateKey     string
	edgeCA            string
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration
	startupTimeout    time.Duration
	shutdownTimeout   time.Duration
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
	if strings.TrimSpace(config.controllerID) == "" {
		return errors.New("--controller-id is required")
	}
	if config.startupTimeout <= 0 || config.shutdownTimeout <= 0 {
		return errors.New("startup and shutdown timeout must be positive")
	}

	startupContext, cancelStartup := context.WithTimeout(ctx, config.startupTimeout)
	database, err := postgres.Open(startupContext, getenv("MUXVIA_CLOUD_DATABASE_URL"))
	cancelStartup()
	if err != nil {
		return err
	}
	defer database.Close()

	directoryState, err := directory.New(directory.Config{MailboxSize: 4096, GracePeriod: 10 * time.Second})
	if err != nil {
		return err
	}
	defer directoryState.Close()
	service, err := control.NewService(control.Config{
		ControllerID:      config.controllerID,
		ControllerBootID:  uuid.NewString(),
		HeartbeatInterval: config.heartbeatInterval,
		HeartbeatTimeout:  config.heartbeatTimeout,
		Directory:         directoryState,
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
	logger.Info("Controller 已启动", "version", softwareVersion, "grpc_address", runtime.GRPCAddress(), "health_address", runtime.HealthAddress())

	var runtimeErr error
	select {
	case <-ctx.Done():
	case runtimeErr = <-runtime.Errors():
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), config.shutdownTimeout)
	defer cancelShutdown()
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
	flags.StringVar(&config.controllerID, "controller-id", "", "stable Controller identity")
	flags.StringVar(&config.grpcListen, "grpc-listen", "0.0.0.0:8443", "mTLS gRPC listen address")
	flags.StringVar(&config.healthListen, "health-listen", "127.0.0.1:8081", "loopback health listen address")
	flags.StringVar(&config.tlsCertificate, "tls-cert", "", "Controller TLS certificate file")
	flags.StringVar(&config.tlsPrivateKey, "tls-key", "", "Controller TLS private key file")
	flags.StringVar(&config.edgeCA, "edge-ca", "", "EdgeIdentity CA certificate file")
	flags.DurationVar(&config.heartbeatInterval, "heartbeat-interval", 10*time.Second, "Edge heartbeat interval")
	flags.DurationVar(&config.heartbeatTimeout, "heartbeat-timeout", 30*time.Second, "Edge heartbeat timeout")
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
