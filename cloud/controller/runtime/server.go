// Package runtime 组装 Controller 的 listener、EdgeControl 和健康状态，不持有业务领域真值。
package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anytty/anytty/cloud/processhealth"
	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	grpc_health "google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

// Config 是 Controller listener 与 TLS 的进程级配置。
// HealthListenAddress 必须是 loopback，不允许对公网暴露管理健康面。
type Config struct {
	GRPCListenAddress   string
	HealthListenAddress string
	TLSCertificateFile  string
	TLSPrivateKeyFile   string
	EdgeCAFile          string
}

// Runtime 拥有 Controller 的 gRPC/health listener 和优雅关闭顺序。
type Runtime struct {
	grpcServer     *grpc.Server
	grpcHealth     *grpc_health.Server
	healthServer   *http.Server
	grpcListener   net.Listener
	healthListener net.Listener
	health         *processhealth.State
	drainer        interface{ BeginShutdown() }
	errors         chan error
	shutdownOnce   sync.Once
}

// Start 绑定 listener，注册 EdgeControl 并在两个独立 goroutine 中启动 gRPC 与 loopback health。
// 调用方必须先完成 PostgreSQL ping 和领域依赖组装。
func Start(config Config, service cloudv1.EdgeControlServer) (*Runtime, error) {
	if service == nil {
		return nil, errors.New("EdgeControl service is required")
	}
	config.GRPCListenAddress = strings.TrimSpace(config.GRPCListenAddress)
	config.HealthListenAddress = strings.TrimSpace(config.HealthListenAddress)
	if config.GRPCListenAddress == "" {
		return nil, errors.New("Controller gRPC listen address is required")
	}
	if !processhealth.IsLoopbackAddress(config.HealthListenAddress) {
		return nil, errors.New("Controller health listen address must be loopback")
	}
	tlsConfig, err := securetransport.NewServerTLSConfig(securetransport.ServerOptions{
		CertificateFile: config.TLSCertificateFile,
		PrivateKeyFile:  config.TLSPrivateKeyFile,
		ClientCAFile:    config.EdgeCAFile,
	})
	if err != nil {
		return nil, err
	}
	grpcListener, err := net.Listen("tcp", config.GRPCListenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen Controller gRPC: %w", err)
	}
	healthListener, err := net.Listen("tcp", config.HealthListenAddress)
	if err != nil {
		_ = grpcListener.Close()
		return nil, fmt.Errorf("listen Controller health: %w", err)
	}
	healthState := &processhealth.State{}
	healthState.SetAlive(true)
	healthState.SetReady(true)
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))
	cloudv1.RegisterEdgeControlServer(grpcServer, service)
	grpcHealth := grpc_health.NewServer()
	grpcHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, grpcHealth)
	healthServer := &http.Server{
		Handler:           healthState,
		ReadHeaderTimeout: 5 * time.Second,
	}
	runtime := &Runtime{
		grpcServer: grpcServer, grpcHealth: grpcHealth, healthServer: healthServer,
		grpcListener: grpcListener, healthListener: healthListener, health: healthState,
		errors: make(chan error, 2),
	}
	if drainer, ok := service.(interface{ BeginShutdown() }); ok {
		runtime.drainer = drainer
	}
	go runtime.serveGRPC()
	go runtime.serveHealth()
	return runtime, nil
}

// GRPCAddress 返回已绑定的 Controller gRPC 地址，主要用于进程 harness 和日志。
func (runtime *Runtime) GRPCAddress() string {
	return runtime.grpcListener.Addr().String()
}

// HealthAddress 返回已绑定的 loopback 健康地址。
func (runtime *Runtime) HealthAddress() string {
	return runtime.healthListener.Addr().String()
}

// Errors 只输出 listener 的非预期致命错误，正常 Shutdown 不会写入。
func (runtime *Runtime) Errors() <-chan error {
	return runtime.errors
}

// Shutdown 先撤销 ready，再停止 gRPC 接入和 HTTP health，超时时强制终止 gRPC。
func (runtime *Runtime) Shutdown(ctx context.Context) error {
	var shutdownErr error
	runtime.shutdownOnce.Do(func() {
		runtime.health.SetReady(false)
		runtime.grpcHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		if runtime.drainer != nil {
			runtime.drainer.BeginShutdown()
		}
		grpcDone := make(chan struct{})
		go func() {
			runtime.grpcServer.GracefulStop()
			close(grpcDone)
		}()
		select {
		case <-grpcDone:
		case <-ctx.Done():
			runtime.grpcServer.Stop()
			shutdownErr = ctx.Err()
		}
		if err := runtime.healthServer.Shutdown(ctx); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
		runtime.health.SetAlive(false)
	})
	return shutdownErr
}

func (runtime *Runtime) serveGRPC() {
	if err := runtime.grpcServer.Serve(runtime.grpcListener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		runtime.errors <- fmt.Errorf("serve Controller gRPC: %w", err)
	}
}

func (runtime *Runtime) serveHealth() {
	if err := runtime.healthServer.Serve(runtime.healthListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		runtime.errors <- fmt.Errorf("serve Controller health: %w", err)
	}
}
