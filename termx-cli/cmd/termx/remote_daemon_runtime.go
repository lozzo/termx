package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	coreprotocol "github.com/lozzow/termx/internal/protocol"
	corev2 "github.com/lozzow/termx/termx-core-v2"
	remote "github.com/lozzow/termx/termx-remote"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
)

type coreV2RemoteLifecycleService interface {
	corev2.RemoteService
	Start(context.Context) error
	Close(context.Context) error
	LocalEnable(context.Context, coreprotocol.RemoteLocalEnableParams) (coreprotocol.RemoteLocalStatus, error)
}

type coreV2DaemonRemoteRuntime struct {
	service coreV2RemoteLifecycleService
	server  *corev2.Server
	logger  *slog.Logger
}

var newCoreV2DaemonRemoteLifecycleService = func(cfg remoteprotocol.Config, daemon remote.Daemon) coreV2RemoteLifecycleService {
	return coreV2RemoteLifecycleServiceAdapter{service: remote.NewService(cfg, daemon)}
}

func configureCoreV2DaemonRemoteRuntime(ctx context.Context, server *corev2.Server, cfg remoteprotocol.Config, logger *slog.Logger) (*coreV2DaemonRemoteRuntime, error) {
	if server == nil {
		return nil, nil
	}
	if !cfg.Enabled {
		return nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	adapter := newCoreV2RemoteDaemonAdapter(server)
	service := newCoreV2DaemonRemoteLifecycleService(cfg, adapter)
	if service == nil {
		return nil, fmt.Errorf("core-v2 daemon remote lifecycle service is nil")
	}
	server.SetRemoteService(service)
	runtime := &coreV2DaemonRemoteRuntime{service: service, server: server, logger: logger}
	if err := runtime.start(ctx, cfg); err != nil {
		server.SetRemoteService(nil)
		_ = service.Close(context.Background())
		return nil, err
	}
	return runtime, nil
}

func (runtime *coreV2DaemonRemoteRuntime) start(ctx context.Context, cfg remoteprotocol.Config) error {
	if runtime == nil || runtime.service == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := runtime.service.Start(ctx); err != nil {
		return err
	}
	if params, ok := daemonRemoteLocalEnableParams(cfg); ok {
		status, err := runtime.service.LocalEnable(ctx, coreLocalEnableParamsFromRemote(params))
		if err != nil {
			return err
		}
		if runtime.logger != nil {
			runtime.logger.Info("core-v2 daemon remote local runtime enabled",
				"http_url", status.HTTPURL,
				"local_web_addr", status.LocalWebAddr,
				"ice_tcp_addr", status.ICETCPAddr,
				"ice_tcp_port", status.ICETCPPort,
			)
		}
	}
	return nil
}

func (runtime *coreV2DaemonRemoteRuntime) Close(ctx context.Context) error {
	if runtime == nil || runtime.service == nil {
		return nil
	}
	if runtime.server != nil {
		runtime.server.SetRemoteService(nil)
	}
	return runtime.service.Close(ctx)
}

func daemonRemoteLocalEnableParams(cfg remoteprotocol.Config) (remoteprotocol.LocalEnableParams, bool) {
	webAddr := strings.TrimSpace(daemonLocalWebAddr(cfg))
	iceAddr := strings.TrimSpace(daemonLocalICETCPAddr(cfg))
	if webAddr == "" && iceAddr == "" {
		return remoteprotocol.LocalEnableParams{}, false
	}
	return remoteprotocol.LocalEnableParams{
		LocalWebAddr: webAddr,
		ICETCPAddr:   iceAddr,
		HubURLs:      append([]string(nil), cfg.HubURLs...),
		ControlURL:   cfg.ControlURL,
		AccessToken:  cfg.AccessToken,
		Region:       cfg.Region,
	}, true
}

type coreV2RemoteLifecycleServiceAdapter struct {
	service *remote.Service
}

func (adapter coreV2RemoteLifecycleServiceAdapter) Start(ctx context.Context) error {
	if adapter.service == nil {
		return fmt.Errorf("remote service is nil")
	}
	return adapter.service.Start(ctx)
}

func (adapter coreV2RemoteLifecycleServiceAdapter) Close(ctx context.Context) error {
	if adapter.service == nil {
		return nil
	}
	return adapter.service.Close(ctx)
}

func (adapter coreV2RemoteLifecycleServiceAdapter) Status(ctx context.Context) (coreprotocol.RemoteStatus, error) {
	return coreV2RemoteServiceHook{service: adapter.service}.Status(ctx)
}

func (adapter coreV2RemoteLifecycleServiceAdapter) PairStart(ctx context.Context, params coreprotocol.RemotePairStartParams) (coreprotocol.RemotePairStartResult, error) {
	return coreV2RemoteServiceHook{service: adapter.service}.PairStart(ctx, params)
}

func (adapter coreV2RemoteLifecycleServiceAdapter) LocalEnable(ctx context.Context, params coreprotocol.RemoteLocalEnableParams) (coreprotocol.RemoteLocalStatus, error) {
	return coreV2RemoteServiceHook{service: adapter.service}.LocalEnable(ctx, params)
}

func (adapter coreV2RemoteLifecycleServiceAdapter) LocalStatus(ctx context.Context) (coreprotocol.RemoteLocalStatus, error) {
	return coreV2RemoteServiceHook{service: adapter.service}.LocalStatus(ctx)
}

func (adapter coreV2RemoteLifecycleServiceAdapter) LocalDisable(ctx context.Context) (coreprotocol.RemoteLocalStatus, error) {
	return coreV2RemoteServiceHook{service: adapter.service}.LocalDisable(ctx)
}
