// Package runtime 组装单个 Muxvia Cloud Edge 进程的公网 listener、健康状态和 ControllerLink。
package runtime

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/muxvia/muxvia/cloud/edge/controllerlink"
	"github.com/muxvia/muxvia/cloud/processhealth"
	"github.com/muxvia/muxvia/cloud/securetransport"
	"google.golang.org/grpc"
	grpc_health "google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

// Config 是 Edge 进程启动所需的本机 bootstrap 配置。
// 区域、容量、域名和策略不在该结构中，后续由签名 DesiredConfig 拥有。
type Config struct {
	ListenAddress           string
	PublicCertificateFile   string
	PublicPrivateKeyFile    string
	ControllerAddress       string
	ControllerServerName    string
	ControllerCAFile        string
	IdentityCertificateFile string
	IdentityPrivateKeyFile  string
	EdgeID                  string
	BootID                  string
	SoftwareVersion         string
}

// Runtime 拥有 Edge 公网 listener、gRPC health 和唯一 ControllerLink 重连循环。
// R1 不拥有 agent/session/allocation map；这些真值由 R2 Runtime actor 建立。
type Runtime struct {
	config        Config
	publicTLS     *tls.Config
	controllerTLS *tls.Config
	listener      net.Listener
	httpServer    *http.Server
	grpcServer    *grpc.Server
	grpcHealth    *grpc_health.Server
	health        *processhealth.State
	errors        chan error
	readyChanges  chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
	waitGroup     sync.WaitGroup
	shutdownOnce  sync.Once
}

// Start 先启动固定 HTTPS /healthz，再在后台建立 mTLS EdgeControl。
// Controller 暂时不可达时进程保持 alive 但 ready=false，不开启任何收费会话。
func Start(parent context.Context, config Config) (*Runtime, error) {
	config = normalizeConfig(config)
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	publicTLS, err := securetransport.NewServerTLSConfig(securetransport.ServerOptions{
		CertificateFile: config.PublicCertificateFile,
		PrivateKeyFile:  config.PublicPrivateKeyFile,
	})
	if err != nil {
		return nil, fmt.Errorf("load Edge public TLS: %w", err)
	}
	controllerTLS, err := securetransport.NewClientTLSConfig(securetransport.ClientOptions{
		CertificateFile: config.IdentityCertificateFile,
		PrivateKeyFile:  config.IdentityPrivateKeyFile,
		RootCAFile:      config.ControllerCAFile,
		ServerName:      config.ControllerServerName,
	})
	if err != nil {
		return nil, fmt.Errorf("load Edge identity TLS: %w", err)
	}
	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen Edge public address: %w", err)
	}
	healthState := &processhealth.State{}
	healthState.SetAlive(true)
	grpcServer := grpc.NewServer()
	grpcHealth := grpc_health.NewServer()
	grpcHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, grpcHealth)
	ctx, cancel := context.WithCancel(parent)
	runtime := &Runtime{
		config: config, publicTLS: publicTLS, controllerTLS: controllerTLS,
		listener: listener, grpcServer: grpcServer, grpcHealth: grpcHealth, health: healthState,
		errors: make(chan error, 1), readyChanges: make(chan struct{}, 1), ctx: ctx, cancel: cancel,
	}
	runtime.httpServer = &http.Server{Handler: runtime, ReadHeaderTimeout: 5 * time.Second, TLSConfig: publicTLS}
	runtime.waitGroup.Add(2)
	go runtime.servePublic()
	go runtime.runControllerLink()
	return runtime, nil
}

// PublicAddress 返回 Edge 公网 HTTPS/gRPC 共用 listener 的已绑定地址。
func (runtime *Runtime) PublicAddress() string {
	return runtime.listener.Addr().String()
}

// Ready 表示 Edge 已完成当前 generation 的 EdgeHello/EdgeWelcome。
func (runtime *Runtime) Ready() bool {
	return runtime.health.Ready()
}

// WaitReady 等待 ControllerLink 就绪，超时或进程关闭时返回 context error。
func (runtime *Runtime) WaitReady(ctx context.Context) error {
	for !runtime.Ready() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-runtime.ctx.Done():
			return runtime.ctx.Err()
		case <-runtime.readyChanges:
		}
	}
	return nil
}

// Errors 只输出 Edge 公网 listener 的致命错误；ControllerLink 断开会撤销 ready 并有界重连。
func (runtime *Runtime) Errors() <-chan error {
	return runtime.errors
}

// ServeHTTP 在同一 TLS listener 上路由 gRPC health 和固定 HTTP 健康路径。
func (runtime *Runtime) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.ProtoMajor == 2 && strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/grpc") {
		runtime.grpcServer.ServeHTTP(writer, request)
		return
	}
	runtime.health.ServeHTTP(writer, request)
}

// Shutdown 进入 not-ready，取消 ControllerLink，关闭公网 listener 并等待有界 goroutine 退出。
func (runtime *Runtime) Shutdown(ctx context.Context) error {
	var shutdownErr error
	runtime.shutdownOnce.Do(func() {
		runtime.setReady(false)
		runtime.cancel()
		runtime.grpcHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		runtime.grpcServer.GracefulStop()
		if err := runtime.httpServer.Shutdown(ctx); err != nil {
			shutdownErr = err
		}
		waitDone := make(chan struct{})
		go func() {
			runtime.waitGroup.Wait()
			close(waitDone)
		}()
		select {
		case <-waitDone:
		case <-ctx.Done():
			if shutdownErr == nil {
				shutdownErr = ctx.Err()
			}
		}
		runtime.health.SetAlive(false)
	})
	return shutdownErr
}

func (runtime *Runtime) servePublic() {
	defer runtime.waitGroup.Done()
	if err := runtime.httpServer.ServeTLS(runtime.listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		runtime.errors <- fmt.Errorf("serve Edge public listener: %w", err)
	}
}

func (runtime *Runtime) runControllerLink() {
	defer runtime.waitGroup.Done()
	delay := 100 * time.Millisecond
	for runtime.ctx.Err() == nil {
		session, err := controllerlink.Open(runtime.ctx, controllerlink.Config{
			ControllerAddress: runtime.config.ControllerAddress,
			TLSConfig:         runtime.controllerTLS,
			EdgeID:            runtime.config.EdgeID,
			BootID:            runtime.config.BootID,
			SoftwareVersion:   runtime.config.SoftwareVersion,
		})
		if err == nil {
			if runtime.ctx.Err() != nil {
				_ = session.Close()
				return
			}
			runtime.setReady(true)
			runtime.grpcHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
			delay = 100 * time.Millisecond
			err = session.Wait()
			_ = session.Close()
			runtime.setReady(false)
			runtime.grpcHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		}
		if runtime.ctx.Err() != nil || controllerlink.IsExpectedClosure(runtime.ctx, err) {
			return
		}
		timer := time.NewTimer(delay)
		select {
		case <-runtime.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 5*time.Second {
			delay *= 2
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}
}

func (runtime *Runtime) setReady(value bool) {
	if runtime.health.Ready() == value {
		return
	}
	runtime.health.SetReady(value)
	select {
	case runtime.readyChanges <- struct{}{}:
	default:
	}
}

func normalizeConfig(config Config) Config {
	config.ListenAddress = strings.TrimSpace(config.ListenAddress)
	config.ControllerAddress = strings.TrimSpace(config.ControllerAddress)
	config.ControllerServerName = strings.TrimSpace(config.ControllerServerName)
	config.EdgeID = strings.TrimSpace(config.EdgeID)
	config.BootID = strings.TrimSpace(config.BootID)
	config.SoftwareVersion = strings.TrimSpace(config.SoftwareVersion)
	return config
}

func validateConfig(config Config) error {
	if config.ListenAddress == "" || config.ControllerAddress == "" || config.ControllerServerName == "" || config.EdgeID == "" || config.BootID == "" || config.SoftwareVersion == "" {
		return errors.New("Edge listen, Controller, identity, boot ID, and software version are required")
	}
	if _, err := securetransport.EdgeIdentityURI(config.EdgeID); err != nil {
		return err
	}
	return nil
}
