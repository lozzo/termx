package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-remote/agent/runtime"
	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/grpcadapter"
	"github.com/lozzow/termx/termx-remote/hub/httpapi"
	"github.com/lozzow/termx/termx-remote/hub/registry"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
	"github.com/soheilhy/cmux"
)

type localRuntime struct {
	localWebAddr  string
	iceTCPAddr    string
	httpURL       string
	actualWebAddr string
	localPairURL  string
	iceTCPPort    int
	iceTCPMux     *LocalICETCPMux
	listener      net.Listener
	httpServer    *http.Server
	registry      *registry.Registry
	shutdown      func(context.Context) error
	cancel        context.CancelFunc
	updatedAt     time.Time
}

var (
	localHubAgentTTL        = 2 * time.Minute
	localHubCleanupInterval = time.Minute
)

func (s *Service) managerContext(fallback context.Context) context.Context {
	if s != nil && s.rtcCtx != nil {
		return s.rtcCtx
	}
	if fallback != nil {
		return fallback
	}
	return context.Background()
}

func (r *localRuntime) statusLocked() remoteprotocol.LocalStatus {
	if r == nil {
		return remoteprotocol.LocalStatus{UpdatedAt: time.Now().UTC()}
	}
	return remoteprotocol.LocalStatus{
		Enabled:       true,
		HTTPURL:       r.httpURL,
		LocalWebAddr:  nonEmpty(r.actualWebAddr, r.localWebAddr),
		LocalPairURL:  r.localPairURL,
		ICETCPEnabled: r.iceTCPMux != nil,
		ICETCPAddr:    r.iceTCPAddr,
		ICETCPPort:    r.iceTCPPort,
		UpdatedAt:     r.updatedAt,
	}
}

func (r *localRuntime) close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.cancel != nil {
		r.cancel()
	}
	var err error
	if r.shutdown != nil {
		err = r.shutdown(ctx)
	}
	if r.iceTCPMux != nil {
		if closeErr := r.iceTCPMux.Close(); err == nil && closeErr != nil && !isClosedNetworkError(closeErr) {
			err = closeErr
		}
	}
	return err
}

func newEmbeddedLocalHub(ctx context.Context, params remoteprotocol.LocalEnableParams, cfg remoteconfig.Config) (*localRuntime, error) {
	cfg = remoteconfig.Normalize(cfg)
	listenAddr := strings.TrimSpace(params.LocalWebAddr)
	if listenAddr == "" {
		listenAddr = "0.0.0.0:18888"
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}
	actualAddr := listener.Addr().String()
	advertisedHost := localHubAdvertisedHost(listener.Addr())
	port, err := portFromAddr(listener.Addr())
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	localHubURL := "http://" + net.JoinHostPort(advertisedHost, strconv.Itoa(port))

	reg := registry.New(registry.Config{AgentTTL: localHubAgentTTL})
	cloudSvc := cloud.NewService(cloud.Config{Registry: reg})
	hubHandler := httpapi.NewHandler(httpapi.Config{
		Cloud:          cloudSvc,
		Registry:       reg,
		AnswerTimeout:  250 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		LocalDiscovery: true,
	})
	allowedNets, err := httpapi.ParseLANIPs(cfg.LANIPs)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("parse lan_ips: %w", err)
	}
	lanFilter := httpapi.NewLANFilter(cfg.AllowLAN, allowedNets)
	httpServer := &http.Server{Handler: lanFilter(newLocalHubHTTPHandler(hubHandler))}
	grpcServer := grpcadapter.NewServer(reg, cloudSvc, nil)
	mux := cmux.New(listener)
	grpcListener := mux.Match(cmux.HTTP2())
	httpListener := mux.Match(cmux.HTTP1Fast())
	iceListener := mux.Match(cmux.Any())
	iceMux, err := remotertc.NewLocalICETCPMuxFromListener(iceListener)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	cleanupInterval := localHubCleanupInterval
	if cleanupInterval <= 0 {
		cleanupInterval = time.Minute
	}
	cleanupTicker := time.NewTicker(cleanupInterval)
	go func() {
		defer cleanupTicker.Stop()
		if cleaner, ok := hubHandler.(interface {
			StartCleanup(context.Context, <-chan time.Time)
		}); ok {
			cleaner.StartCleanup(runCtx, cleanupTicker.C)
		}
	}()
	done := make(chan error, 3)
	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, net.ErrClosed) && !isClosedNetworkError(err) {
			done <- err
			cancel()
			return
		}
		done <- nil
	}()
	go func() {
		if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			done <- err
			cancel()
			return
		}
		done <- nil
	}()
	go func() {
		if err := mux.Serve(); err != nil && !errors.Is(err, net.ErrClosed) && !strings.Contains(strings.ToLower(err.Error()), "use of closed network connection") {
			done <- err
			cancel()
			return
		}
		done <- nil
	}()

	rt := &localRuntime{
		localWebAddr:  listenAddr,
		iceTCPAddr:    net.JoinHostPort(advertisedHost, strconv.Itoa(port)),
		httpURL:       localHubURL,
		actualWebAddr: actualAddr,
		localPairURL:  localHubURL,
		iceTCPPort:    port,
		iceTCPMux:     iceMux,
		listener:      listener,
		httpServer:    httpServer,
		registry:      reg,
		cancel:        cancel,
		updatedAt:     time.Now().UTC(),
	}
	rt.shutdown = func(shutdownCtx context.Context) error {
		if shutdownCtx == nil {
			shutdownCtx = context.Background()
		}
		cancel()
		var err error
		if grpcServer != nil {
			grpcServer.GracefulStop()
		}
		if httpServer != nil {
			err = httpServer.Shutdown(shutdownCtx)
			if isClosedNetworkError(err) {
				err = nil
			}
		}
		if listener != nil {
			if closeErr := listener.Close(); err == nil && closeErr != nil && !isClosedNetworkError(closeErr) {
				err = closeErr
			}
		}
		select {
		case <-runCtx.Done():
		default:
		}
		return err
	}
	return rt, nil
}

func (s *Service) attachManagerToLocalHub(ctx context.Context, local *localRuntime) {
	if s == nil || s.manager == nil || local == nil {
		return
	}
	runtimeAdapter := daemonRuntimeAdapter{daemon: s.daemon}
	s.manager.AddHubURLWithAnswerOptions(local.httpURL, remotertc.AnswerOptions{
		SettingEngine:      local.iceTCPMux,
		TerminalManagement: runtimeAdapter,
		Storage:            runtimeAdapter,
		Events:             runtimeAdapter,
	})
	if status := s.manager.Status(); status.State == runtime.StateDisabled {
		_ = s.manager.Start(ctx)
	}
}

func (s *Service) attachManagerToCloud(params remoteprotocol.LocalEnableParams, localHubURL string) {
	if s == nil || s.manager == nil {
		return
	}
	localHubURL = strings.TrimSpace(localHubURL)
	for _, hubURL := range params.HubURLs {
		hubURL = strings.TrimSpace(hubURL)
		if hubURL == "" || hubURL == localHubURL {
			continue
		}
		s.manager.AddExplicitHubURL(hubURL)
	}
	s.manager.ConfigureCloud(params.ControlURL, params.AccessToken, params.Region)
}

func (s *Service) detachManagerFromLocalHub(local *localRuntime) {
	if s == nil || s.manager == nil || local == nil {
		return
	}
	s.manager.DetachHub(local.httpURL)
}

func localHubAdvertisedHost(addr net.Addr) string {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok || tcpAddr.IP == nil || tcpAddr.IP.IsUnspecified() {
		return localHubLANIP()
	}
	return tcpAddr.IP.String()
}

func localHubLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			return ip.String()
		}
	}
	return "127.0.0.1"
}

func portFromAddr(addr net.Addr) (int, error) {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok || tcpAddr.Port <= 0 {
		return 0, fmt.Errorf("local hub listener has invalid address %q", addr.String())
	}
	return tcpAddr.Port, nil
}

func isClosedNetworkError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, net.ErrClosed) || strings.Contains(strings.ToLower(err.Error()), "use of closed network connection")
}
