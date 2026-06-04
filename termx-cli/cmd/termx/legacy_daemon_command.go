package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	termx "github.com/lozzow/termx/termx-core"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	"github.com/spf13/cobra"
)

var (
	newServer = func(opts ...termx.ServerOption) termxServer {
		return termx.NewServer(opts...)
	}
	remoteConfigLoader     = remoteConfigFromFileAndEnv
	newRemoteRuntimeHostFn = newRemoteRuntimeHost
)

type termxServer interface {
	ListenAndServe(context.Context) error
	Shutdown(context.Context) error
}

func daemonCommand(socket *string, configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use: "daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			logFile, _ := cmd.Flags().GetString("log-file")
			logger, closeLogger, logPath, err := openLogFileLogger(logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			gridRoot := resolveGridStatePath()
			logger.Info("starting daemon", "socket", resolveSocket(*socket), "log_file", logPath, "grid_root", gridRoot)
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			remoteCfg, err := remoteConfigLoader(*configPath)
			if err != nil {
				return err
			}
			opts := []termx.ServerOption{termx.WithLogger(logger), termx.WithGridRoot(gridRoot)}
			if *socket != "" {
				opts = append(opts, termx.WithSocketPath(*socket))
			}
			if remoteCfg.Enabled {
				logger.Info(
					"remote runtime configured",
					"control_url", remoteCfg.ControlURL,
					"hub_url", remoteCfg.HubURL,
					"data_dir", remoteCfg.DataDir,
					"device_name", remoteCfg.DeviceName,
				)
			}
			remoteHost := newRemoteRuntimeHostFn(nil, remoteCfg)
			if remoteHost != nil {
				opts = append(opts, termx.WithProtocolMethodHandler(remoteHost), termx.WithTerminalInventoryObserver(remoteHost))
			}
			srv := newServer(opts...)
			if remoteHost != nil {
				if core, ok := srv.(remoteRuntimeCore); ok {
					remoteHost.core = core
				}
				if err := remoteHost.Start(ctx); err != nil {
					return err
				}
			}
			defer func() {
				if remoteHost != nil {
					_ = remoteHost.Close(context.Background())
				}
				_ = srv.Shutdown(context.Background())
			}()
			if localWebAddr := daemonLocalWebAddr(remoteCfg); localWebAddr != "" {
				if remoteHost == nil || remoteHost.service == nil || remoteHost.core == nil {
					return fmt.Errorf("remote local runtime requires a core daemon")
				}
				localStatus, err := remoteHost.service.LocalEnable(ctx, remoteprotocol.LocalEnableParams{
					LocalWebAddr: localWebAddr,
					ICETCPAddr:   daemonLocalICETCPAddr(remoteCfg),
					HubURLs:      compactStringList(remoteCfg.HubURLs),
					ControlURL:   remoteCfg.ControlURL,
					AccessToken:  remoteCfg.AccessToken,
					Region:       remoteCfg.Region,
				})
				if err != nil {
					return err
				}
				logger.Info("remote local web configured", "url", localStatus.HTTPURL, "ice_tcp_port", localStatus.ICETCPPort)
			} else if iceTCPAddr := remoteLocalICETCPAddrFromEnv(); iceTCPAddr != "" {
				logger.Warn("remote local ICE TCP requested without local web; ignoring standalone ICE TCP listener", "addr", iceTCPAddr)
			}
			err = srv.ListenAndServe(ctx)
			if err != nil {
				logger.Error("daemon exited with error", "error", err)
			} else {
				logger.Info("daemon exited")
			}
			return err
		},
	}
	return cmd
}
