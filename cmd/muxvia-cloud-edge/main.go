// Package main 是 muxvia-cloud-edge 的进程组装入口。
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
	edgeruntime "github.com/muxvia/muxvia/cloud/edge/runtime"
)

const defaultSoftwareVersion = "development"

type options struct {
	listenAddress       string
	controllerAddress   string
	controllerServer    string
	edgeID              string
	publicCertificate   string
	publicPrivateKey    string
	identityCertificate string
	identityPrivateKey  string
	controllerCA        string
	softwareVersion     string
	shutdownTimeout     time.Duration
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], logger); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		logger.Error("Edge 退出", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, logger *slog.Logger) error {
	config, err := parseOptions(arguments, os.Stderr)
	if err != nil {
		return err
	}
	if strings.TrimSpace(config.edgeID) == "" {
		return errors.New("--edge-id is required")
	}
	if config.shutdownTimeout <= 0 {
		return errors.New("shutdown timeout must be positive")
	}
	runtime, err := edgeruntime.Start(ctx, edgeruntime.Config{
		ListenAddress:           config.listenAddress,
		PublicCertificateFile:   config.publicCertificate,
		PublicPrivateKeyFile:    config.publicPrivateKey,
		ControllerAddress:       config.controllerAddress,
		ControllerServerName:    config.controllerServer,
		ControllerCAFile:        config.controllerCA,
		IdentityCertificateFile: config.identityCertificate,
		IdentityPrivateKeyFile:  config.identityPrivateKey,
		EdgeID:                  config.edgeID,
		BootID:                  uuid.NewString(),
		SoftwareVersion:         config.softwareVersion,
	})
	if err != nil {
		return err
	}
	logger.Info("Edge 已启动", "edge_id", config.edgeID, "version", config.softwareVersion, "public_address", runtime.PublicAddress(), "controller_address", config.controllerAddress)

	var runtimeErr error
	select {
	case <-ctx.Done():
	case runtimeErr = <-runtime.Errors():
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), config.shutdownTimeout)
	defer cancelShutdown()
	if err := runtime.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown Edge: %w", err)
	}
	if runtimeErr != nil {
		return runtimeErr
	}
	logger.Info("Edge 已停止")
	return nil
}

func parseOptions(arguments []string, output io.Writer) (options, error) {
	var config options
	flags := flag.NewFlagSet("muxvia-cloud-edge", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&config.listenAddress, "listen", "0.0.0.0:8443", "public HTTPS/gRPC listen address")
	flags.StringVar(&config.controllerAddress, "controller", "", "Controller mTLS gRPC address")
	flags.StringVar(&config.controllerServer, "controller-server-name", "", "Controller TLS server name")
	flags.StringVar(&config.edgeID, "edge-id", "", "stable Edge identity")
	flags.StringVar(&config.publicCertificate, "public-tls-cert", "", "Edge public TLS certificate file")
	flags.StringVar(&config.publicPrivateKey, "public-tls-key", "", "Edge public TLS private key file")
	flags.StringVar(&config.identityCertificate, "identity-cert", "", "EdgeIdentity mTLS certificate file")
	flags.StringVar(&config.identityPrivateKey, "identity-key", "", "EdgeIdentity mTLS private key file")
	flags.StringVar(&config.controllerCA, "controller-ca", "", "Controller CA certificate file")
	flags.StringVar(&config.softwareVersion, "version", defaultSoftwareVersion, "Edge software version reported in EdgeHello")
	flags.DurationVar(&config.shutdownTimeout, "shutdown-timeout", 15*time.Second, "graceful shutdown deadline")
	if err := flags.Parse(arguments); err != nil {
		return options{}, fmt.Errorf("parse Edge flags: %w", err)
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected Edge arguments: %s", strings.Join(flags.Args(), " "))
	}
	return config, nil
}
