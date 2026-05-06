package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lozzow/termx/termx-core"
	"github.com/lozzow/termx/termx-core/clientapi"
	"github.com/lozzow/termx/termx-core/protocol"
	unixtransport "github.com/lozzow/termx/termx-core/transport/unix"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	tuiv2app "github.com/lozzow/termx/tuiv2/app"
	"github.com/lozzow/termx/tuiv2/shared" //nolint:typecheck
	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	isInteractiveTerminal = func() bool {
		return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	}
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		socketPath := resolveSocket(cfg.SocketPath)
		cfg.SocketPath = socketPath
		client, err := dialOrStartClient(socketPath, cfg.LogFilePath, nil)
		if err != nil {
			return err
		}
		defer client.Close()
		return tuiv2app.RunWithClient(cfg, clientapi.NewProtocolClient(client), stdin, stdout)
	}
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		client, err := dialOrStartClient(resolveSocket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var out remoteprotocol.PairStartResult
		if err := client.Call(ctx, "remote.pair.start", params, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		client, err := dialOrStartClient(resolveSocket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var out remoteprotocol.Status
		if err := client.Call(ctx, "remote.status", map[string]any{}, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		client, err := dialOrStartClient(resolveSocket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var out remoteprotocol.LocalStatus
		if err := client.Call(ctx, "remote.local.enable", params, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		client, err := dialOrStartClient(resolveSocket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var out remoteprotocol.LocalStatus
		if err := client.Call(ctx, "remote.local.status", map[string]any{}, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	remoteLocalDisableClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		client, err := dialOrStartClient(resolveSocket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var out remoteprotocol.LocalStatus
		if err := client.Call(ctx, "remote.local.disable", map[string]any{}, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	openBrowser = func(rawURL string) error {
		switch runtime.GOOS {
		case "darwin":
			return exec.Command("open", rawURL).Start()
		case "windows":
			return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
		default:
			return exec.Command("xdg-open", rawURL).Start()
		}
	}
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

func nestedTUIBlocked() bool {
	return os.Getenv("TERMX") == "1" && os.Getenv("TERMX_ALLOW_NESTED") != "1"
}

func rejectNestedTUI() error {
	if !nestedTUIBlocked() {
		return nil
	}
	return fmt.Errorf("refusing to start termx TUI inside a termx remote terminal; use a normal shell, or set TERMX_ALLOW_NESTED=1 if you really want nesting")
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var socket string
	var logFile string
	var configPath string
	cmd := &cobra.Command{
		Use: "termx",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			logger.Info("starting tuiv2 root command", "log_file", logPath)
			if !isInteractiveTerminal() {
				return fmt.Errorf("termx TUI requires an interactive terminal; use `termx --help` or subcommands like `new`, `ls`, `attach`, `kill`, `daemon`")
			}
			if err := rejectNestedTUI(); err != nil {
				logger.Warn("blocked nested tui launch")
				return err
			}
			cfg, err := tuiSharedConfig("main", "main", "", socket, logPath, resolveWorkspaceStatePath(), configPath)
			if err != nil {
				return err
			}
			return runTUIv2(cfg, os.Stdin, os.Stdout)
		},
	}
	cmd.PersistentFlags().StringVar(&socket, "socket", "", "socket path")
	cmd.PersistentFlags().StringVar(&logFile, "log-file", "", "log file path (default: $TERMX_LOG_FILE or XDG state dir)")
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "termx config path (default: XDG config dir termx.yaml)")
	cmd.AddCommand(daemonCommand(&socket, &configPath))
	cmd.AddCommand(newCommand(&socket, &logFile))
	cmd.AddCommand(lsCommand(&socket, &logFile))
	cmd.AddCommand(killCommand(&socket, &logFile))
	cmd.AddCommand(attachCommand(&socket, &logFile, &configPath))
	cmd.AddCommand(pairCommand(&socket, &logFile))
	cmd.AddCommand(remoteCommand(&socket, &logFile, &configPath))
	return cmd
}

func tuiSharedConfig(workspace, sessionID, attachID, socket, logPath, workspaceStatePath, configPath string) (shared.Config, error) {
	if configPath == "" {
		configPath = shared.DefaultConfigPath()
	}
	if err := shared.EnsureDefaultConfigFile(configPath); err != nil {
		return shared.Config{}, err
	}
	fileCfg, err := shared.LoadConfig(configPath)
	if err != nil {
		return shared.Config{}, err
	}
	fileCfg.Workspace = workspace
	fileCfg.SessionID = sessionID
	fileCfg.AttachID = attachID
	fileCfg.SocketPath = socket
	fileCfg.LogFilePath = logPath
	fileCfg.WorkspaceStatePath = workspaceStatePath
	fileCfg.ConfigPath = configPath
	return fileCfg, nil
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
			logger.Info("starting daemon", "socket", resolveSocket(*socket), "log_file", logPath)
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			remoteCfg, err := remoteConfigLoader(*configPath)
			if err != nil {
				return err
			}
			opts := []termx.ServerOption{termx.WithLogger(logger)}
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
			if localWebAddr := remoteLocalWebAddrFromEnv(); localWebAddr != "" {
				if remoteHost == nil || remoteHost.service == nil || remoteHost.core == nil {
					return fmt.Errorf("remote local runtime requires a core daemon")
				}
				localStatus, err := remoteHost.service.LocalEnable(ctx, remoteprotocol.LocalEnableParams{
					LocalWebAddr: localWebAddr,
					ICETCPAddr:   remoteLocalICETCPAddrFromEnv(),
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

func newCommand(socket *string, logFile *string) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:  "new -- CMD [ARGS...]",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			if len(args) == 0 {
				shell := os.Getenv("SHELL")
				if shell == "" {
					shell = "/bin/sh"
				}
				args = []string{shell}
			}
			logger.Info("creating terminal", "socket", resolveSocket(*socket), "command", strings.Join(args, " "), "log_file", logPath)
			client, err := dialOrStartClient(resolveSocket(*socket), logPath, logger)
			if err != nil {
				return err
			}
			defer client.Close()
			created, err := client.Create(context.Background(), protocol.CreateParams{
				Command: args,
				Name:    name,
				Size:    currentSize(),
			})
			if err != nil {
				logger.Error("create terminal failed", "error", err)
				return err
			}
			logger.Info("created terminal", "terminal_id", created.TerminalID)
			fmt.Fprintln(cmd.OutOrStdout(), created.TerminalID)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "terminal name")
	return cmd
}

func lsCommand(socket *string, logFile *string) *cobra.Command {
	return &cobra.Command{
		Use: "ls",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, _, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			client, err := dialOrStartClient(resolveSocket(*socket), resolveLogFilePath(*logFile), logger)
			if err != nil {
				return err
			}
			defer client.Close()
			list, err := client.List(context.Background())
			if err != nil {
				logger.Error("list terminals failed", "error", err)
				return err
			}
			logger.Info("listed terminals", "count", len(list.Terminals))
			for _, item := range list.Terminals {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%dx%d\n",
					item.ID, item.Name, strings.Join(item.Command, " "), item.State, item.Size.Cols, item.Size.Rows)
			}
			return nil
		},
	}
}

func killCommand(socket *string, logFile *string) *cobra.Command {
	return &cobra.Command{
		Use:  "kill <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			logger.Info("killing terminal", "terminal_id", args[0], "socket", resolveSocket(*socket), "log_file", logPath)
			client, err := dialOrStartClient(resolveSocket(*socket), logPath, logger)
			if err != nil {
				return err
			}
			defer client.Close()
			err = client.Kill(context.Background(), args[0])
			if err != nil {
				logger.Error("kill terminal failed", "terminal_id", args[0], "error", err)
			}
			return err
		},
	}
}

func attachCommand(socket *string, logFile *string, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:  "attach <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			logger.Info("starting attach tui", "terminal_id", args[0], "socket", resolveSocket(*socket), "log_file", logPath)
			if err := rejectNestedTUI(); err != nil {
				logger.Warn("blocked nested attach tui", "terminal_id", args[0])
				return err
			}
			cfg, err := tuiSharedConfig("main", "main", args[0], *socket, logPath, "", *configPath)
			if err != nil {
				return err
			}
			return runTUIv2(cfg, os.Stdin, os.Stdout)
		},
	}
}

func remoteCommand(socket *string, logFile *string, configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage TermX remote access",
	}
	cmd.AddCommand(remoteLoginCommand(configPath))
	cmd.AddCommand(remoteStatusCommand(socket, logFile))
	cmd.AddCommand(remoteInfoCommand(socket, logFile))
	cmd.AddCommand(remoteEnableCommand(socket, logFile, configPath))
	cmd.AddCommand(remoteLocalOnlyCommand(socket, logFile))
	cmd.AddCommand(remoteDisableCommand(socket, logFile))
	cmd.AddCommand(remotePairCommand(socket, logFile))
	cmd.AddCommand(remoteQRCodeCommand(socket, logFile))
	cmd.AddCommand(remoteOpenCommand(socket, logFile))
	return cmd
}

func pairCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	var localURL string
	var ttl time.Duration
	cmd := &cobra.Command{
		Use: "pair",
		RunE: func(cmd *cobra.Command, args []string) error {
			if ttl <= 0 {
				ttl = 5 * time.Minute
			}
			localURL = strings.TrimSpace(localURL)
			result, err := pairStartClient(context.Background(), *socket, *logFile, remoteprotocol.PairStartParams{
				LocalPairURL: localURL,
				TTLSeconds:   int(ttl.Seconds()),
			})
			if err != nil {
				return err
			}
			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			printPairStartResult(cmd.OutOrStdout(), result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	cmd.Flags().StringVar(&localURL, "local-url", "", "local pair URL; optional when pairing through hub")
	cmd.Flags().DurationVar(&ttl, "ttl", 5*time.Minute, "pair session TTL")
	return cmd
}

func remoteStatusCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use: "status",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := remoteStatusClient(context.Background(), *socket, *logFile)
			if err != nil {
				return err
			}
			localStatus, err := remoteLocalStatusClient(context.Background(), *socket, *logFile)
			if err != nil {
				return err
			}
			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Remote *remoteprotocol.Status      `json:"remote"`
					Local  *remoteprotocol.LocalStatus `json:"local"`
				}{Remote: status, Local: localStatus})
			}

			printRemoteStatus(cmd.OutOrStdout(), status)
			printRemoteLocalStatus(cmd.OutOrStdout(), localStatus)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return cmd
}

func remoteInfoCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:     "info",
		Aliases: []string{"show"},
		Short:   "Show remote runtime and local web details",
		RunE: func(cmd *cobra.Command, args []string) error {
			remoteStatus, err := remoteStatusClient(context.Background(), *socket, *logFile)
			if err != nil {
				return err
			}
			localStatus, err := remoteLocalStatusClient(context.Background(), *socket, *logFile)
			if err != nil {
				return err
			}
			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Remote *remoteprotocol.Status      `json:"remote"`
					Local  *remoteprotocol.LocalStatus `json:"local"`
				}{Remote: remoteStatus, Local: localStatus})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "[remote]")
			printRemoteStatus(cmd.OutOrStdout(), remoteStatus)
			fmt.Fprintln(cmd.OutOrStdout(), "[local]")
			printRemoteLocalStatus(cmd.OutOrStdout(), localStatus)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return cmd
}

func remoteEnableCommand(socket *string, logFile *string, configPath *string) *cobra.Command {
	var enableMode string
	var outputJSON bool
	var addr string
	var iceTCPAddr string
	var serverURL string
	var controlURL string
	var hubURL string
	var token string
	var tokenEnv string
	var tokenFile string
	var browser bool
	var browserTimeout time.Duration
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable remote access features",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := strings.ToLower(strings.TrimSpace(enableMode))
			if mode != "local" && mode != "online" && mode != "both" {
				return fmt.Errorf("--mode must be local, online, or both")
			}
			if mode == "local" && browser {
				return fmt.Errorf("--browser is only valid for online or both mode")
			}
			hasTokenSource := strings.TrimSpace(token) != "" || strings.TrimSpace(tokenEnv) != "" || strings.TrimSpace(tokenFile) != ""
			resolvedToken, err := resolveSecretFlag(token, tokenEnv, tokenFile)
			if err != nil {
				return err
			}
			token = strings.TrimSpace(resolvedToken)
			if browser && hasTokenSource {
				return fmt.Errorf("--browser cannot be used with --token, --token-env, or --token-file")
			}
			controlURL = firstNonEmpty(serverURL, controlURL)
			switch mode {
			case "both":
				onlineRuntime, err := runRemoteOnlineEnable(cmd, configPath, controlURL, hubURL, token, browser, noBrowser, browserTimeout, outputJSON, mode)
				if err != nil {
					return err
				}
				return runRemoteLocalEnable(cmd, socket, logFile, addr, iceTCPAddr, onlineRuntime, outputJSON)
			case "local":
				if err := ensureRemoteConfigBootstrap(*configPath, "", "", "", mode); err != nil {
					return err
				}
				return runRemoteLocalEnable(cmd, socket, logFile, addr, iceTCPAddr, remoteprotocol.Config{Enabled: true, Mode: mode}, outputJSON)
			case "online":
				_, err := runRemoteOnlineEnable(cmd, configPath, controlURL, hubURL, token, browser, noBrowser, browserTimeout, outputJSON, mode)
				return err
			default:
				return fmt.Errorf("--mode must be local, online, or both")
			}
		},
	}
	cmd.Flags().StringVar(&enableMode, "mode", "both", "connection mode: local, online, or both")
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:18888", "local web listen address")
	cmd.Flags().StringVar(&iceTCPAddr, "ice-tcp-addr", "127.0.0.1:18889", "local ICE TCP listen address")
	cmd.Flags().StringVar(&serverURL, "server", "", "Web Control URL")
	cmd.Flags().StringVar(&controlURL, "control-url", "", "Web Control URL for cloud remote")
	cmd.Flags().StringVar(&hubURL, "hub-url", "", "optional explicit Hub URL; normally discovered from Web Control")
	cmd.Flags().StringVar(&token, "token", "", "Web Control access token for automation")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "environment variable containing the Web Control connection key for automation")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "file containing the Web Control connection key for automation")
	cmd.Flags().BoolVar(&browser, "browser", false, "force browser login instead of using a saved token")
	cmd.Flags().DurationVar(&browserTimeout, "timeout", 5*time.Minute, "browser login timeout")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the browser login URL without opening it")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	_ = cmd.Flags().MarkHidden("control-url")
	_ = cmd.Flags().MarkHidden("hub-url")
	_ = cmd.Flags().MarkHidden("token-env")
	_ = cmd.Flags().MarkHidden("token-file")
	return cmd
}

func remoteLocalOnlyCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	var addr string
	var iceTCPAddr string
	cmd := &cobra.Command{
		Use:     "local",
		Aliases: []string{"local-only", "local_only"},
		Short:   "Enable local browser pairing",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemoteLocalEnable(cmd, socket, logFile, addr, iceTCPAddr, remoteprotocol.Config{}, outputJSON)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:18888", "local web listen address")
	cmd.Flags().StringVar(&iceTCPAddr, "ice-tcp-addr", "127.0.0.1:18889", "local ICE TCP listen address")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return cmd
}

func remoteDisableCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable the local remote web/ICE TCP runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := remoteLocalDisableClient(context.Background(), *socket, *logFile)
			if err != nil {
				return err
			}
			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(status)
			}
			printRemoteLocalStatus(cmd.OutOrStdout(), status)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return cmd
}

func remotePairCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	var localURL string
	var ttl time.Duration
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Create a local pairing session for the running local remote web",
		RunE: func(cmd *cobra.Command, args []string) error {
			if ttl <= 0 {
				ttl = 5 * time.Minute
			}
			localURL = strings.TrimSpace(localURL)
			if localURL == "" {
				localStatus, err := remoteLocalStatusClient(context.Background(), *socket, *logFile)
				if err != nil {
					return err
				}
				if localStatus == nil || !localStatus.Enabled || strings.TrimSpace(localStatus.LocalPairURL) == "" {
					localURL = ""
				} else {
					localURL = localStatus.LocalPairURL
				}
			}
			result, err := pairStartClient(context.Background(), *socket, *logFile, remoteprotocol.PairStartParams{
				LocalPairURL: localURL,
				TTLSeconds:   int(ttl.Seconds()),
			})
			if err != nil {
				return err
			}
			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			printPairStartResult(cmd.OutOrStdout(), result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	cmd.Flags().StringVar(&localURL, "local-url", "", "local pair URL; defaults to running local remote")
	cmd.Flags().DurationVar(&ttl, "ttl", 5*time.Minute, "pair session TTL")
	return cmd
}

func remoteQRCodeCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	var payloadOnly bool
	var localURL string
	var ttl time.Duration
	var controlURL string
	var hubURL string
	cmd := &cobra.Command{
		Use:   "qrcode",
		Short: "Show a TermX remote pairing QR",
		RunE: func(cmd *cobra.Command, args []string) error {
			if ttl <= 0 {
				ttl = 5 * time.Minute
			}
			localURL = strings.TrimSpace(localURL)
			if localURL == "" {
				localStatus, err := remoteLocalStatusClient(context.Background(), *socket, *logFile)
				if err != nil {
					return err
				}
				if localStatus != nil {
					localURL = strings.TrimSpace(localStatus.LocalPairURL)
				}
			}
			remoteStatus, err := remoteStatusClient(context.Background(), *socket, *logFile)
			if err != nil {
				return err
			}
			result, err := pairStartClient(context.Background(), *socket, *logFile, remoteprotocol.PairStartParams{
				LocalPairURL: localURL,
				TTLSeconds:   int(ttl.Seconds()),
			})
			if err != nil {
				return err
			}
			hubURLs := hubURLsForPairPayload(remoteStatus, hubURL)
			payload := buildRemotePairPayload(result, remoteStatus, hubURLs)
			uri, err := termxPairURI(payload)
			if err != nil {
				return err
			}
			if payloadOnly {
				fmt.Fprintln(cmd.OutOrStdout(), uri)
				return nil
			}
			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					URI     string         `json:"uri"`
					Payload map[string]any `json:"payload"`
				}{
					URI:     uri,
					Payload: payload,
				})
			}
			qr, err := qrcode.New(uri, qrcode.Medium)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), qr.ToSmallString(false))
			fmt.Fprintf(cmd.OutOrStdout(), "uri:\t%s\n", uri)
			fmt.Fprintf(cmd.OutOrStdout(), "expires_at:\t%s\n", result.ExpiresAt.Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	cmd.Flags().BoolVar(&payloadOnly, "payload", false, "print only the termx://pair URI")
	cmd.Flags().StringVar(&localURL, "local-url", "", "local pair URL; defaults to running local remote")
	cmd.Flags().DurationVar(&ttl, "ttl", 5*time.Minute, "pair session TTL")
	cmd.Flags().StringVar(&controlURL, "server", "", "Web Control URL override")
	cmd.Flags().StringVar(&hubURL, "hub-url", "", "Hub URL override")
	_ = cmd.Flags().MarkHidden("hub-url")
	return cmd
}

func remoteOpenCommand(socket *string, logFile *string) *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the local remote web UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			localStatus, err := remoteLocalStatusClient(context.Background(), *socket, *logFile)
			if err != nil {
				return err
			}
			if localStatus == nil || !localStatus.Enabled || strings.TrimSpace(localStatus.HTTPURL) == "" {
				return fmt.Errorf("local remote is not enabled; run `termx remote enable --mode local` first")
			}
			if printOnly {
				fmt.Fprintln(cmd.OutOrStdout(), localStatus.HTTPURL)
				return nil
			}
			if err := openBrowser(localStatus.HTTPURL); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), localStatus.HTTPURL)
			return nil
		},
	}
	cmd.Flags().BoolVar(&printOnly, "print", false, "print URL without opening a browser")
	return cmd
}

func runRemoteLocalEnable(cmd *cobra.Command, socket *string, logFile *string, addr string, iceTCPAddr string, cloud remoteprotocol.Config, outputJSON bool) error {
	status, err := remoteLocalEnableClient(context.Background(), *socket, *logFile, remoteprotocol.LocalEnableParams{
		LocalWebAddr: addr,
		ICETCPAddr:   iceTCPAddr,
		HubURLs:      compactStringList(cloud.HubURLs),
		ControlURL:   cloud.ControlURL,
		AccessToken:  cloud.AccessToken,
		Region:       cloud.Region,
	})
	if err != nil {
		return err
	}
	if outputJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}
	printRemoteLocalStatus(cmd.OutOrStdout(), status)
	return nil
}

func runRemoteOnlineEnable(cmd *cobra.Command, configPath *string, controlURL string, hubURL string, token string, forceBrowser bool, noBrowser bool, browserTimeout time.Duration, outputJSON bool, mode string) (remoteprotocol.Config, error) {
	return runRemoteCloudEnableWithToken(cmd, configPath, controlURL, hubURL, token, forceBrowser, noBrowser, browserTimeout, outputJSON, mode)
}

func runRemoteCloudEnable(cmd *cobra.Command, configPath *string, controlURL string, hubURL string, token string, tokenEnv string, tokenFile string, noBrowser bool, outputJSON bool) (remoteprotocol.Config, error) {
	resolvedToken, err := resolveSecretFlag(token, tokenEnv, tokenFile)
	if err != nil {
		return remoteprotocol.Config{}, err
	}
	return runRemoteCloudEnableWithToken(cmd, configPath, controlURL, hubURL, resolvedToken, false, noBrowser, 5*time.Minute, outputJSON, "online")
}

func runRemoteCloudEnableWithToken(cmd *cobra.Command, configPath *string, controlURL string, hubURL string, token string, forceBrowser bool, noBrowser bool, browserTimeout time.Duration, outputJSON bool, mode string) (remoteprotocol.Config, error) {
	controlURL = strings.TrimSpace(controlURL)
	hubURL = strings.TrimSpace(hubURL)
	token = strings.TrimSpace(token)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "online"
	}
	if controlURL == "" || hubURL == "" || (!forceBrowser && token == "") {
		cfg, cfgErr := remoteConfigFromFileAndEnv(*configPath)
		if cfgErr != nil {
			return remoteprotocol.Config{}, cfgErr
		}
		if controlURL == "" {
			controlURL = cfg.ControlURL
		}
		if hubURL == "" {
			hubURL = cfg.HubURL
		}
		if !forceBrowser && token == "" {
			token = cfg.AccessToken
		}
	}
	if controlURL == "" {
		return remoteprotocol.Config{}, fmt.Errorf("--server is required for online remote enable")
	}
	var refreshToken string
	if forceBrowser || token == "" {
		created, err := remoteLoginHTTPClient.CreateBrowserLogin(cmd.Context(), controlURL, "termx local service")
		if err != nil {
			return remoteprotocol.Config{}, err
		}
		var progress io.Writer = cmd.OutOrStdout()
		if outputJSON {
			progress = cmd.ErrOrStderr()
		}
		auth, err := completeRemoteBrowserLogin(cmd, controlURL, created, browserTimeout, !noBrowser, progress)
		if err != nil {
			return remoteprotocol.Config{}, err
		}
		token = strings.TrimSpace(auth.AccessToken)
		refreshToken = strings.TrimSpace(auth.RefreshToken)
		if token == "" {
			return remoteprotocol.Config{}, fmt.Errorf("control did not return a connection key")
		}
	}
	if _, err := remoteLoginHTTPClient.Me(cmd.Context(), controlURL, token); err != nil {
		return remoteprotocol.Config{}, err
	}
	authStorePath, err := remoteAuthStorePath(*configPath)
	if err != nil {
		return remoteprotocol.Config{}, err
	}
	if err := saveRemoteAuthRecord(authStorePath, remoteAuthRecord{
		ControlURL:   controlURL,
		HubURL:       hubURL,
		AccessToken:  token,
		RefreshToken: refreshToken,
	}); err != nil {
		return remoteprotocol.Config{}, err
	}
	if err := ensureRemoteConfigBootstrap(*configPath, controlURL, hubURL, authStorePath, mode); err != nil {
		return remoteprotocol.Config{}, err
	}
	hubURLs := compactStringList([]string{hubURL})
	if hubURL == "" {
		hubURLs = nil
	}
	cloud := remoteprotocol.Config{
		Enabled:     true,
		ControlURL:  controlURL,
		HubURL:      hubURL,
		HubURLs:     hubURLs,
		AccessToken: token,
		Mode:        mode,
	}
	if outputJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return cloud, enc.Encode(struct {
			Enabled    bool   `json:"enabled"`
			ControlURL string `json:"control_url"`
			HubURL     string `json:"hub_url,omitempty"`
			Mode       string `json:"mode"`
			AuthStore  string `json:"auth_store"`
		}{
			Enabled:    true,
			ControlURL: controlURL,
			HubURL:     hubURL,
			Mode:       mode,
			AuthStore:  authStorePath,
		})
	}
	fmt.Fprintln(cmd.OutOrStdout(), "online_enabled:\ttrue")
	fmt.Fprintf(cmd.OutOrStdout(), "control_url:\t%s\n", controlURL)
	if hubURL != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "hub_url:\t%s\n", hubURL)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "mode:\t%s\n", mode)
	fmt.Fprintf(cmd.OutOrStdout(), "auth_store:\t%s\n", authStorePath)
	fmt.Fprintln(cmd.OutOrStdout(), "next:\trun `termx daemon` to keep online remote connected")
	return cloud, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func csvList(raw string) []string {
	return compactStringList(strings.Split(raw, ","))
}

func compactStringList(values []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func compactStringListOrEmpty(values []string) []string {
	out := compactStringList(values)
	if out == nil {
		return []string{}
	}
	return out
}

func printRemoteStatus(w io.Writer, status *remoteprotocol.Status) {
	if status == nil {
		return
	}
	fmt.Fprintf(w, "state:\t%s\n", status.State)
	fmt.Fprintf(w, "device_id:\t%s\n", status.DeviceID)
	fmt.Fprintf(w, "device_name:\t%s\n", status.DeviceName)
	fmt.Fprintf(w, "control_url:\t%s\n", status.ControlURL)
	fmt.Fprintf(w, "hub_url:\t%s\n", status.HubURL)
	fmt.Fprintf(w, "mode:\t%s\n", statusValue(status, "mode"))
	fmt.Fprintf(w, "allow_lan:\t%v\n", status.AllowLAN)
	fmt.Fprintf(w, "data_dir:\t%s\n", status.DataDir)
	fmt.Fprintf(w, "terminal_count:\t%d\n", status.TerminalCount)
	fmt.Fprintf(w, "updated_at:\t%s\n", status.UpdatedAt.Format(time.RFC3339))
	if status.Detail != "" {
		fmt.Fprintf(w, "detail:\t%s\n", status.Detail)
	}
}

func printRemoteLocalStatus(w io.Writer, status *remoteprotocol.LocalStatus) {
	if status == nil {
		return
	}
	fmt.Fprintf(w, "local_enabled:\t%t\n", status.Enabled)
	fmt.Fprintf(w, "local_web_url:\t%s\n", status.HTTPURL)
	fmt.Fprintf(w, "local_web_addr:\t%s\n", status.LocalWebAddr)
	fmt.Fprintf(w, "local_pair_url:\t%s\n", status.LocalPairURL)
	fmt.Fprintf(w, "ice_tcp_enabled:\t%t\n", status.ICETCPEnabled)
	fmt.Fprintf(w, "ice_tcp_addr:\t%s\n", status.ICETCPAddr)
	fmt.Fprintf(w, "ice_tcp_port:\t%d\n", status.ICETCPPort)
	fmt.Fprintf(w, "updated_at:\t%s\n", status.UpdatedAt.Format(time.RFC3339))
}

func printPairStartResult(w io.Writer, result *remoteprotocol.PairStartResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(w, "type:\t%s\n", result.Type)
	fmt.Fprintf(w, "machine_id:\t%s\n", result.MachineID)
	fmt.Fprintf(w, "machine_name:\t%s\n", result.MachineName)
	fmt.Fprintf(w, "local_pair_url:\t%s\n", result.LocalPairURL)
	fmt.Fprintf(w, "pair_session_id:\t%s\n", result.PairSessionID)
	fmt.Fprintf(w, "pair_secret:\t%s\n", result.PairSecret)
	fmt.Fprintf(w, "answer_proof_secret:\t%s\n", result.AnswerProofSecret)
	fmt.Fprintf(w, "expires_at:\t%s\n", result.ExpiresAt.Format(time.RFC3339))
}

func buildRemotePairPayload(result *remoteprotocol.PairStartResult, _ *remoteprotocol.Status, hubURLs []string) map[string]any {
	payload := map[string]any{
		"type":           "termx_pair_v2",
		"schema_version": 3,
		"machine": map[string]any{
			"id":   result.MachineID,
			"name": firstNonEmpty(result.MachineName, result.MachineID),
		},
		"addresses": map[string]any{
			"local":  []string{},
			"lan":    compactStringSlice(result.LocalPairURL),
			"public": compactStringListOrEmpty(hubURLs),
		},
		"pairing": map[string]any{
			"session_id":          result.PairSessionID,
			"secret":              result.PairSecret,
			"answer_proof_secret": result.AnswerProofSecret,
			"expires_at":          result.ExpiresAt.Format(time.RFC3339),
		},
	}
	cleanEmptyStrings(payload)
	return payload
}

func hubURLsForPairPayload(status *remoteprotocol.Status, override string) []string {
	if strings.TrimSpace(override) != "" {
		return compactStringList([]string{override})
	}
	if status == nil {
		return nil
	}
	hubURLs := compactStringList(status.HubURLs)
	if len(hubURLs) == 0 && strings.TrimSpace(status.HubURL) != "" {
		hubURLs = []string{strings.TrimSpace(status.HubURL)}
	}
	return hubURLs
}

func termxPairURI(payload map[string]any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "termx://pair?payload=" + base64.RawURLEncoding.EncodeToString(data), nil
}

func statusValue(status *remoteprotocol.Status, field string) string {
	if status == nil {
		return ""
	}
	switch field {
	case "control":
		return status.ControlURL
	case "hub":
		return status.HubURL
	case "mode":
		return status.Mode
	default:
		return ""
	}
}

func compactStringSlice(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func cleanEmptyStrings(value any) {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			switch typed := child.(type) {
			case string:
				if strings.TrimSpace(typed) == "" {
					delete(item, key)
				}
			case map[string]any:
				cleanEmptyStrings(typed)
			}
		}
	}
}

func dialClient(path string) (*protocol.Client, error) {
	conn, err := unixtransport.Dial(path)
	if err != nil {
		return nil, err
	}
	client := protocol.NewClient(conn)
	if err := client.Hello(context.Background(), protocol.Hello{
		Version: protocol.Version,
		Client:  "termx-cli",
	}); err != nil {
		return nil, err
	}
	return client, nil
}

func dialOrStartClient(path string, logFile string, logger *slog.Logger) (*protocol.Client, error) {
	client, err := dialClient(path)
	if err == nil {
		if logger != nil {
			logger.Debug("connected to existing daemon", "socket", path)
		}
		return client, nil
	}
	if logger != nil {
		logger.Warn("initial daemon dial failed, attempting auto-start", "socket", path, "error", err)
	}
	if startErr := startDaemon(path, logFile); startErr != nil {
		return nil, err
	}
	if waitErr := waitForSocket(path, 5*time.Second, func() error {
		c, dialErr := dialClient(path)
		if dialErr != nil {
			return dialErr
		}
		_ = c.Close()
		return nil
	}); waitErr != nil {
		return nil, waitErr
	}
	if logger != nil {
		logger.Info("auto-started daemon became ready", "socket", path)
	}
	return dialClient(path)
}

func waitForSocket(path string, timeout time.Duration, try func() error) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := try(); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for daemon at %s", path)
}

func startDaemon(path string, logFile string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"--socket", path}
	if logFile != "" {
		args = append(args, "--log-file", logFile)
	}
	args = append(args, "daemon")
	cmd := exec.Command(exe, args...)
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func resolveSocket(path string) string {
	if path != "" {
		return path
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return runtimeDir + "/termx.sock"
	}
	return fmt.Sprintf("%s/termx-%d.sock", os.TempDir(), os.Getuid())
}

func currentSize() protocol.Size {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return protocol.Size{}
	}
	return protocol.Size{Cols: uint16(cols), Rows: uint16(rows)}
}

func remoteConfigFromEnv() remoteprotocol.Config {
	enabled, enabledSet, _ := envBoolValue("TERMX_REMOTE_ENABLE")
	allowLAN, _, _ := envBoolValue("TERMX_REMOTE_ALLOW_LAN")
	hubURLs := csvList(os.Getenv("TERMX_REMOTE_HUB_URLS"))
	hubURL := strings.TrimSpace(os.Getenv("TERMX_REMOTE_HUB_URL"))
	if hubURL == "" && len(hubURLs) > 0 {
		hubURL = hubURLs[0]
	}
	if hubURL != "" && len(hubURLs) == 0 {
		hubURLs = []string{hubURL}
	}
	cfg := remoteprotocol.Config{
		Enabled:    enabled,
		ControlURL: strings.TrimSpace(os.Getenv("TERMX_REMOTE_CONTROL_URL")),
		HubURL:     hubURL,
		HubURLs:    hubURLs,
		AccessToken: firstNonEmpty(
			strings.TrimSpace(os.Getenv("TERMX_REMOTE_CONNECTION_KEY")),
			strings.TrimSpace(os.Getenv("TERMX_REMOTE_TOKEN")),
			strings.TrimSpace(os.Getenv("TERMX_REMOTE_ACCESS_TOKEN")),
		),
		DataDir:    strings.TrimSpace(os.Getenv("TERMX_REMOTE_DATA_DIR")),
		DeviceName: strings.TrimSpace(os.Getenv("TERMX_REMOTE_DEVICE_NAME")),
		Region:     strings.TrimSpace(os.Getenv("TERMX_REMOTE_REGION")),
		Mode:       strings.TrimSpace(os.Getenv("TERMX_REMOTE_MODE")),
		AllowLAN:   allowLAN,
		LANIPs:     splitTrimmed(os.Getenv("TERMX_REMOTE_LAN_IPS")),
	}
	if tokenTTL := durationSecondsFromString(os.Getenv("TERMX_REMOTE_TOKEN_TTL")); tokenTTL > 0 {
		cfg.TokenTTLSeconds = tokenTTL
	}
	if !enabledSet && !cfg.Enabled && remoteConfigHasFields(cfg) {
		cfg.Enabled = true
	}
	return cfg
}

func remoteConfigFromFileAndEnv(path string) (remoteprotocol.Config, error) {
	cfg, fileEnabledSet, err := loadRemoteConfigFromFile(path)
	if err != nil {
		return remoteprotocol.Config{}, err
	}
	envCfg := remoteConfigFromEnv()
	envEnabled, envEnabledSet, _ := envBoolValue("TERMX_REMOTE_ENABLE")
	envAllowLAN, envAllowLANSet, _ := envBoolValue("TERMX_REMOTE_ALLOW_LAN")
	if envEnabledSet {
		cfg.Enabled = envEnabled
	}
	if envCfg.ControlURL != "" {
		cfg.ControlURL = envCfg.ControlURL
	}
	if envCfg.HubURL != "" {
		cfg.HubURL = envCfg.HubURL
		if len(envCfg.HubURLs) > 0 {
			cfg.HubURLs = append([]string(nil), envCfg.HubURLs...)
		} else {
			cfg.HubURLs = []string{envCfg.HubURL}
		}
	}
	if envCfg.AccessToken != "" {
		cfg.AccessToken = envCfg.AccessToken
	}
	if envCfg.DataDir != "" {
		cfg.DataDir = envCfg.DataDir
	}
	if envCfg.DeviceName != "" {
		cfg.DeviceName = envCfg.DeviceName
	}
	if envCfg.Region != "" {
		cfg.Region = envCfg.Region
	}
	if envCfg.Mode != "" {
		cfg.Mode = envCfg.Mode
	}
	if envCfg.TokenTTLSeconds > 0 {
		cfg.TokenTTLSeconds = envCfg.TokenTTLSeconds
	}
	if envAllowLANSet {
		cfg.AllowLAN = envAllowLAN
	}
	if len(envCfg.LANIPs) > 0 {
		cfg.LANIPs = append([]string(nil), envCfg.LANIPs...)
	}
	if !envEnabledSet && !fileEnabledSet && !cfg.Enabled && remoteConfigHasFields(cfg) {
		cfg.Enabled = true
	}
	return cfg, nil
}

func remoteConfigFromFile(path string) (remoteprotocol.Config, error) {
	cfg, _, err := loadRemoteConfigFromFile(path)
	return cfg, err
}

func loadRemoteConfigFromFile(path string) (remoteprotocol.Config, bool, error) {
	if strings.TrimSpace(path) == "" {
		path = shared.DefaultConfigPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return remoteprotocol.Config{}, false, nil
		}
		return remoteprotocol.Config{}, false, err
	}
	values, err := parseRemoteConfigSection(string(data))
	if err != nil {
		return remoteprotocol.Config{}, false, fmt.Errorf("remote config %q: %w", path, err)
	}
	enabled, enabledSet, validEnabled := configBoolValue(values["enabled"])
	if enabledSet && !validEnabled {
		return remoteprotocol.Config{}, false, fmt.Errorf("remote enabled has invalid boolean value %q", values["enabled"])
	}
	cfg := remoteprotocol.Config{
		Enabled:    enabled,
		ControlURL: remoteConfigValue(values, "controlURL", "control_url"),
		HubURL:     remoteConfigValue(values, "hubURL", "hub_url"),
		HubURLs:    splitTrimmed(remoteConfigValue(values, "hubURLs", "hub_urls")),
		DataDir:    remoteConfigValue(values, "dataDir", "data_dir"),
		DeviceName: remoteConfigValue(values, "deviceName", "device_name"),
		Region:     remoteConfigValue(values, "region"),
		Mode:       remoteConfigValue(values, "mode"),
	}
	if tokenTTL := durationSecondsFromString(remoteConfigValue(values, "token_ttl", "tokenTTL")); tokenTTL > 0 {
		cfg.TokenTTLSeconds = tokenTTL
	}
	if rawAllowLAN := remoteConfigValue(values, "allow_lan", "allowLAN"); rawAllowLAN != "" {
		cfg.AllowLAN, _ = strconv.ParseBool(rawAllowLAN)
	}
	cfg.LANIPs = splitTrimmed(remoteConfigValue(values, "lan_ips", "lanIPs"))
	if cfg.HubURL == "" && len(cfg.HubURLs) > 0 {
		cfg.HubURL = cfg.HubURLs[0]
	}
	if cfg.HubURL != "" && len(cfg.HubURLs) == 0 {
		cfg.HubURLs = []string{cfg.HubURL}
	}
	if accessToken := remoteConfigValue(values, "access_token", "token"); accessToken != "" {
		cfg.AccessToken = accessToken
	}
	if authStore := remoteConfigValue(values, "authStore", "auth_store"); authStore != "" {
		record, err := loadRemoteAuthRecord(authStore)
		if err != nil {
			return remoteprotocol.Config{}, false, fmt.Errorf("load remote auth store: %w", err)
		}
		if cfg.ControlURL == "" {
			cfg.ControlURL = record.ControlURL
		}
		if cfg.HubURL == "" && cfg.ControlURL == "" {
			cfg.HubURL = record.HubURL
			if cfg.HubURL != "" {
				cfg.HubURLs = []string{cfg.HubURL}
			}
		}
		cfg.AccessToken = record.AccessToken
	}
	if keyEnv := firstNonEmpty(
		remoteConfigValue(values, "connectionKeyEnv", "connection_key_env"),
		remoteConfigValue(values, "accessTokenEnv", "access_token_env"),
	); keyEnv != "" {
		cfg.AccessToken = strings.TrimSpace(os.Getenv(keyEnv))
	}
	if !enabledSet && !cfg.Enabled && remoteConfigHasFields(cfg) {
		cfg.Enabled = true
	}
	return cfg, enabledSet, nil
}

func remoteConfigValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func remoteConfigHasFields(cfg remoteprotocol.Config) bool {
	return cfg.ControlURL != "" ||
		cfg.HubURL != "" ||
		len(cfg.HubURLs) > 0 ||
		cfg.AccessToken != "" ||
		cfg.DataDir != "" ||
		cfg.DeviceName != "" ||
		cfg.Region != "" ||
		cfg.Mode != "" ||
		cfg.TokenTTLSeconds > 0 ||
		cfg.AllowLAN ||
		len(cfg.LANIPs) > 0
}

func durationSecondsFromString(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds > 0 {
			return seconds
		}
		return 0
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		return 0
	}
	return int(duration.Seconds())
}

func parseRemoteConfigSection(content string) (map[string]string, error) {
	values := make(map[string]string)
	var section string
	for lineNo, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "-") {
			section = strings.TrimSuffix(line, ":")
			continue
		}
		if section != "remote" || strings.HasPrefix(line, "-") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: invalid remote mapping", lineNo+1)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		if key == "accessToken" {
			continue
		}
		values[key] = value
	}
	return values, nil
}

func splitTrimmed(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "[]")
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		field = strings.Trim(field, `"'`)
		if field == "" {
			continue
		}
		out = append(out, field)
	}
	return out
}

func parseConfigBool(value string) bool {
	parsed, _, _ := configBoolValue(value)
	return parsed
}

func configBoolValue(value string) (bool, bool, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return false, false, true
	}
	switch strings.ToLower(raw) {
	case "1", "t", "true", "yes", "y", "on":
		return true, true, true
	case "0", "f", "false", "no", "n", "off":
		return false, true, true
	default:
		return false, true, false
	}
}

func remoteLocalWebAddrFromEnv() string {
	if addr := strings.TrimSpace(os.Getenv("TERMX_REMOTE_LOCAL_WEB_ADDR")); addr != "" {
		return addr
	}
	if envBool("TERMX_REMOTE_LOCAL_WEB_ENABLE") {
		return "127.0.0.1:18888"
	}
	return ""
}

func remoteLocalICETCPAddrFromEnv() string {
	if addr := strings.TrimSpace(os.Getenv("TERMX_REMOTE_LOCAL_ICE_TCP_ADDR")); addr != "" {
		return addr
	}
	if envBool("TERMX_REMOTE_LOCAL_ICE_TCP_ENABLE") {
		return "127.0.0.1:18889"
	}
	return ""
}

func envBool(key string) bool {
	value, _, _ := envBoolValue(key)
	return value
}

func envBoolValue(key string) (bool, bool, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, false, true
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, true, true
	case "0", "false", "no", "off":
		return false, true, true
	default:
		return false, true, false
	}
}
