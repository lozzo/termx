package termx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
)

type RemoteLocalOptions struct {
	LocalWebAddr string
	ICETCPAddr   string
}

type RemoteLocalStatus struct {
	Enabled       bool      `json:"enabled"`
	HTTPURL       string    `json:"http_url,omitempty"`
	LocalWebAddr  string    `json:"local_web_addr,omitempty"`
	LocalPairURL  string    `json:"local_pair_url,omitempty"`
	ICETCPEnabled bool      `json:"ice_tcp_enabled"`
	ICETCPAddr    string    `json:"ice_tcp_addr,omitempty"`
	ICETCPPort    int       `json:"ice_tcp_port,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type remoteLocalRuntime struct {
	localWebAddr  string
	iceTCPAddr    string
	httpURL       string
	actualWebAddr string
	localPairURL  string
	iceTCPPort    int
	httpServer    *http.Server
	iceTCPMux     *LocalICETCPMux
	shutdown      func(context.Context) error
	cancel        context.CancelFunc
	updatedAt     time.Time
}

func (s *Server) RemoteLocalEnable(ctx context.Context, opts RemoteLocalOptions) (RemoteLocalStatus, error) {
	if s == nil {
		return RemoteLocalStatus{}, fmt.Errorf("server is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.closed.Load() {
		return RemoteLocalStatus{}, ErrServerClosed
	}
	localWebAddr := strings.TrimSpace(opts.LocalWebAddr)
	if localWebAddr == "" {
		localWebAddr = "127.0.0.1:18888"
	}
	iceTCPAddr := strings.TrimSpace(opts.ICETCPAddr)
	if iceTCPAddr == "" {
		iceTCPAddr = "127.0.0.1:18889"
	}

	s.remoteLocalMu.Lock()
	defer s.remoteLocalMu.Unlock()
	if s.remoteLocal != nil && s.remoteLocal.localWebAddr == localWebAddr && s.remoteLocal.iceTCPAddr == iceTCPAddr {
		return s.remoteLocal.statusLocked(), nil
	}

	runtime, err := s.newRemoteLocalRuntime(ctx, localWebAddr, iceTCPAddr)
	if err != nil {
		return RemoteLocalStatus{}, err
	}
	old := s.remoteLocal
	s.remoteLocal = runtime
	status := runtime.statusLocked()
	if old != nil {
		go func() {
			if err := old.close(context.Background()); err != nil && s.cfg.logger != nil {
				s.cfg.logger.Warn("remote local previous runtime shutdown failed", "error", err)
			}
		}()
	}
	return status, nil
}

func (s *Server) newRemoteLocalRuntime(ctx context.Context, localWebAddr string, iceTCPAddr string) (*remoteLocalRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	iceMux, err := StartLocalICETCPMux(runtimeCtx, iceTCPAddr)
	if err != nil {
		cancel()
		return nil, err
	}
	httpURL, actualWebAddr, shutdown, err := s.startRemoteLocalWeb(runtimeCtx, localWebAddr, iceMux)
	if err != nil {
		_ = iceMux.Close()
		cancel()
		return nil, err
	}
	endpoint := iceMux.Endpoint()
	return &remoteLocalRuntime{
		localWebAddr:  localWebAddr,
		iceTCPAddr:    iceTCPAddr,
		httpURL:       httpURL,
		actualWebAddr: actualWebAddr,
		localPairURL:  httpURL + "/api/local/pair",
		iceTCPPort:    endpoint.Port,
		iceTCPMux:     iceMux,
		shutdown:      shutdown,
		cancel:        cancel,
		updatedAt:     time.Now().UTC(),
	}, nil
}

func (s *Server) RemoteLocalStatus() RemoteLocalStatus {
	if s == nil {
		return RemoteLocalStatus{UpdatedAt: time.Now().UTC()}
	}
	s.remoteLocalMu.Lock()
	defer s.remoteLocalMu.Unlock()
	if s.remoteLocal == nil {
		return RemoteLocalStatus{UpdatedAt: time.Now().UTC()}
	}
	return s.remoteLocal.statusLocked()
}

func (s *Server) RemoteLocalDisable(ctx context.Context) (RemoteLocalStatus, error) {
	if s == nil {
		return RemoteLocalStatus{}, fmt.Errorf("server is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.remoteLocalMu.Lock()
	runtime := s.remoteLocal
	s.remoteLocal = nil
	s.remoteLocalMu.Unlock()
	if runtime != nil {
		if err := runtime.close(ctx); err != nil {
			return RemoteLocalStatus{}, err
		}
	}
	return RemoteLocalStatus{UpdatedAt: time.Now().UTC()}, nil
}

func (s *Server) startRemoteLocalWeb(ctx context.Context, addr string, iceTCPMux *LocalICETCPMux) (string, string, func(context.Context) error, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", "", nil, fmt.Errorf("remote local web address is required")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", "", nil, err
	}

	baseURL := localWebBaseURL(listener.Addr())
	actualAddr := listener.Addr().String()
	httpServer := &http.Server{
		Handler: s.LocalWebHandler(LocalWebOptions{
			HTTPURL:                baseURL,
			LocalPairURL:           baseURL + "/api/local/pair",
			ICETCPMux:              iceTCPMux,
			LocalRTCSessionContext: ctx,
		}),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed && s.cfg.logger != nil {
			s.cfg.logger.Error("remote local web serve failed", "addr", listener.Addr().String(), "error", err)
		}
	}()

	var once sync.Once
	var shutdownErr error
	shutdown := func(shutdownCtx context.Context) error {
		once.Do(func() {
			shutdownErr = httpServer.Shutdown(shutdownCtx)
			if shutdownErr != nil {
				_ = httpServer.Close()
			}
			<-done
		})
		return shutdownErr
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()

	return baseURL, actualAddr, shutdown, nil
}

func (r *remoteLocalRuntime) statusLocked() RemoteLocalStatus {
	if r == nil {
		return RemoteLocalStatus{UpdatedAt: time.Now().UTC()}
	}
	return RemoteLocalStatus{
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

func nonEmpty(first, fallback string) string {
	if strings.TrimSpace(first) != "" {
		return first
	}
	return fallback
}

func (r *remoteLocalRuntime) close(ctx context.Context) error {
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
		if closeErr := r.iceTCPMux.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func remoteLocalStatusToProtocol(status RemoteLocalStatus) protocol.RemoteLocalStatus {
	return protocol.RemoteLocalStatus{
		Enabled:       status.Enabled,
		HTTPURL:       status.HTTPURL,
		LocalWebAddr:  status.LocalWebAddr,
		LocalPairURL:  status.LocalPairURL,
		ICETCPEnabled: status.ICETCPEnabled,
		ICETCPAddr:    status.ICETCPAddr,
		ICETCPPort:    status.ICETCPPort,
		UpdatedAt:     status.UpdatedAt,
	}
}

func localWebBaseURL(addr net.Addr) string {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return "http://" + addr.String()
	}
	host := tcpAddr.IP.String()
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(tcpAddr.Port))
}
