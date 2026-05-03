package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/lozzow/termx/termx-core"
	"github.com/lozzow/termx/termx-core/clientapi"
	"github.com/lozzow/termx/termx-core/protocol"
	unixtransport "github.com/lozzow/termx/termx-core/transport/unix"
	tuiv2app "github.com/lozzow/termx/tuiv2/app"
	"github.com/lozzow/termx/tuiv2/shared" //nolint:typecheck
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
	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params protocol.PairStartParams) (*protocol.PairStartResult, error) {
		client, err := dialOrStartClient(resolveSocket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return client.RemotePairStart(ctx, params)
	}
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteStatus, error) {
		client, err := dialOrStartClient(resolveSocket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return client.RemoteStatus(ctx)
	}
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params protocol.RemoteLocalEnableParams) (*protocol.RemoteLocalStatus, error) {
		client, err := dialOrStartClient(resolveSocket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return client.RemoteLocalEnable(ctx, params)
	}
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteLocalStatus, error) {
		client, err := dialOrStartClient(resolveSocket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return client.RemoteLocalStatus(ctx)
	}
	remoteLocalDisableClient = func(ctx context.Context, socketPath string, logFile string) (*protocol.RemoteLocalStatus, error) {
		client, err := dialOrStartClient(resolveSocket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return client.RemoteLocalDisable(ctx)
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
	remoteConfigLoader = remoteConfigFromFileAndEnv
	withRemoteConfig   = termx.WithRemoteConfig
)

type termxServer interface {
	ListenAndServe(context.Context) error
	Shutdown(context.Context) error
	RemoteLocalEnable(context.Context, termx.RemoteLocalOptions) (termx.RemoteLocalStatus, error)
}

func nestedTUIBlocked() bool {
	return os.Getenv("TERMX") == "1" && os.Getenv("TERMX_ALLOW_NESTED") != "1"
}

func rejectNestedTUI() error {
	if !nestedTUIBlocked() {
		return nil
	}
	return fmt.Errorf("refusing to start termx TUI inside a termx-managed terminal; use a normal shell, or set TERMX_ALLOW_NESTED=1 if you really want nesting")
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
	cmd.AddCommand(webCommand(&socket, &logFile))
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
			opts := []termx.ServerOption{termx.WithLogger(logger), withRemoteConfig(remoteCfg)}
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
			srv := newServer(opts...)
			defer func() {
				_ = srv.Shutdown(context.Background())
			}()
			if localWebAddr := remoteLocalWebAddrFromEnv(); localWebAddr != "" {
				localStatus, err := srv.RemoteLocalEnable(ctx, termx.RemoteLocalOptions{
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
	cmd.AddCommand(remoteEnableCommand(socket, logFile))
	cmd.AddCommand(remoteLocalOnlyCommand(socket, logFile))
	cmd.AddCommand(remoteDisableCommand(socket, logFile))
	cmd.AddCommand(remotePairCommand(socket, logFile))
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
			if strings.TrimSpace(localURL) == "" {
				return fmt.Errorf("local pair URL is required")
			}
			if ttl <= 0 {
				ttl = 5 * time.Minute
			}
			result, err := pairStartClient(context.Background(), *socket, *logFile, protocol.PairStartParams{
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
	cmd.Flags().StringVar(&localURL, "local-url", "http://127.0.0.1:18888/api/local/pair", "local pair URL")
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
					Remote *protocol.RemoteStatus      `json:"remote"`
					Local  *protocol.RemoteLocalStatus `json:"local"`
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
					Remote *protocol.RemoteStatus      `json:"remote"`
					Local  *protocol.RemoteLocalStatus `json:"local"`
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

func remoteEnableCommand(socket *string, logFile *string) *cobra.Command {
	var localOnly bool
	var outputJSON bool
	var addr string
	var iceTCPAddr string
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable remote access features",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !localOnly {
				return fmt.Errorf("managed remote enable is not implemented yet; use `termx remote enable --local-only` for local browser/app pairing")
			}
			return runRemoteLocalEnable(cmd, socket, logFile, addr, iceTCPAddr, outputJSON)
		},
	}
	cmd.Flags().BoolVar(&localOnly, "local-only", false, "enable only local embedded web and local WebRTC")
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:18888", "local web listen address")
	cmd.Flags().StringVar(&iceTCPAddr, "ice-tcp-addr", "127.0.0.1:18889", "local ICE TCP listen address")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return cmd
}

func remoteLocalOnlyCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	var addr string
	var iceTCPAddr string
	cmd := &cobra.Command{
		Use:     "local-only",
		Aliases: []string{"local_only"},
		Short:   "Enable local-only remote access",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemoteLocalEnable(cmd, socket, logFile, addr, iceTCPAddr, outputJSON)
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
					return fmt.Errorf("local remote is not enabled; run `termx remote enable --local-only` first or pass --local-url")
				}
				localURL = localStatus.LocalPairURL
			}
			result, err := pairStartClient(context.Background(), *socket, *logFile, protocol.PairStartParams{
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
				return fmt.Errorf("local remote is not enabled; run `termx remote enable --local-only` first")
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

func runRemoteLocalEnable(cmd *cobra.Command, socket *string, logFile *string, addr string, iceTCPAddr string, outputJSON bool) error {
	status, err := remoteLocalEnableClient(context.Background(), *socket, *logFile, protocol.RemoteLocalEnableParams{
		LocalWebAddr: addr,
		ICETCPAddr:   iceTCPAddr,
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

func printRemoteStatus(w io.Writer, status *protocol.RemoteStatus) {
	if status == nil {
		return
	}
	fmt.Fprintf(w, "state:\t%s\n", status.State)
	fmt.Fprintf(w, "device_id:\t%s\n", status.DeviceID)
	fmt.Fprintf(w, "device_name:\t%s\n", status.DeviceName)
	fmt.Fprintf(w, "control_url:\t%s\n", status.ControlURL)
	fmt.Fprintf(w, "hub_url:\t%s\n", status.HubURL)
	fmt.Fprintf(w, "data_dir:\t%s\n", status.DataDir)
	fmt.Fprintf(w, "terminal_count:\t%d\n", status.TerminalCount)
	fmt.Fprintf(w, "updated_at:\t%s\n", status.UpdatedAt.Format(time.RFC3339))
	if status.Detail != "" {
		fmt.Fprintf(w, "detail:\t%s\n", status.Detail)
	}
}

func printRemoteLocalStatus(w io.Writer, status *protocol.RemoteLocalStatus) {
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

func printPairStartResult(w io.Writer, result *protocol.PairStartResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(w, "type:\t%s\n", result.Type)
	fmt.Fprintf(w, "machine_id:\t%s\n", result.MachineID)
	fmt.Fprintf(w, "machine_name:\t%s\n", result.MachineName)
	fmt.Fprintf(w, "machine_public_key_fingerprint:\t%s\n", result.MachinePublicKeyFingerprint)
	fmt.Fprintf(w, "local_pair_url:\t%s\n", result.LocalPairURL)
	fmt.Fprintf(w, "pair_session_id:\t%s\n", result.PairSessionID)
	fmt.Fprintf(w, "pair_secret:\t%s\n", result.PairSecret)
	fmt.Fprintf(w, "expires_at:\t%s\n", result.ExpiresAt.Format(time.RFC3339))
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

func remoteConfigFromEnv() termx.RemoteConfig {
	enabled, enabledSet, _ := envBoolValue("TERMX_REMOTE_ENABLE")
	cfg := termx.RemoteConfig{
		Enabled:     enabled,
		ControlURL:  strings.TrimSpace(os.Getenv("TERMX_REMOTE_CONTROL_URL")),
		HubURL:      strings.TrimSpace(os.Getenv("TERMX_REMOTE_HUB_URL")),
		AccessToken: strings.TrimSpace(os.Getenv("TERMX_REMOTE_ACCESS_TOKEN")),
		DataDir:     strings.TrimSpace(os.Getenv("TERMX_REMOTE_DATA_DIR")),
		DeviceName:  strings.TrimSpace(os.Getenv("TERMX_REMOTE_DEVICE_NAME")),
		Region:      strings.TrimSpace(os.Getenv("TERMX_REMOTE_REGION")),
	}
	if !enabledSet && !cfg.Enabled && (cfg.ControlURL != "" || cfg.HubURL != "" || cfg.AccessToken != "" || cfg.DataDir != "" || cfg.DeviceName != "" || cfg.Region != "") {
		cfg.Enabled = true
	}
	return cfg
}

func remoteConfigFromFileAndEnv(path string) (termx.RemoteConfig, error) {
	cfg, fileEnabledSet, err := loadRemoteConfigFromFile(path)
	if err != nil {
		return termx.RemoteConfig{}, err
	}
	envCfg := remoteConfigFromEnv()
	envEnabled, envEnabledSet, _ := envBoolValue("TERMX_REMOTE_ENABLE")
	if envEnabledSet {
		cfg.Enabled = envEnabled
	}
	if envCfg.ControlURL != "" {
		cfg.ControlURL = envCfg.ControlURL
	}
	if envCfg.HubURL != "" {
		cfg.HubURL = envCfg.HubURL
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
	if !envEnabledSet && !fileEnabledSet && !cfg.Enabled && (cfg.ControlURL != "" || cfg.HubURL != "" || cfg.AccessToken != "" || cfg.DataDir != "" || cfg.DeviceName != "" || cfg.Region != "") {
		cfg.Enabled = true
	}
	return cfg, nil
}

func remoteConfigFromFile(path string) (termx.RemoteConfig, error) {
	cfg, _, err := loadRemoteConfigFromFile(path)
	return cfg, err
}

func loadRemoteConfigFromFile(path string) (termx.RemoteConfig, bool, error) {
	if strings.TrimSpace(path) == "" {
		path = shared.DefaultConfigPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return termx.RemoteConfig{}, false, nil
		}
		return termx.RemoteConfig{}, false, err
	}
	values, err := parseRemoteConfigSection(string(data))
	if err != nil {
		return termx.RemoteConfig{}, false, fmt.Errorf("remote config %q: %w", path, err)
	}
	enabled, enabledSet, validEnabled := configBoolValue(values["enabled"])
	if enabledSet && !validEnabled {
		return termx.RemoteConfig{}, false, fmt.Errorf("remote enabled has invalid boolean value %q", values["enabled"])
	}
	cfg := termx.RemoteConfig{
		Enabled:    enabled,
		ControlURL: strings.TrimSpace(values["controlURL"]),
		HubURL:     strings.TrimSpace(values["hubURL"]),
		DataDir:    strings.TrimSpace(values["dataDir"]),
		DeviceName: strings.TrimSpace(values["deviceName"]),
		Region:     strings.TrimSpace(values["region"]),
	}
	if authStore := strings.TrimSpace(values["authStore"]); authStore != "" {
		record, err := loadRemoteAuthRecord(authStore)
		if err != nil {
			return termx.RemoteConfig{}, false, fmt.Errorf("load remote auth store: %w", err)
		}
		if cfg.ControlURL == "" {
			cfg.ControlURL = record.ControlURL
		}
		cfg.AccessToken = record.AccessToken
	}
	if tokenEnv := strings.TrimSpace(values["accessTokenEnv"]); tokenEnv != "" {
		cfg.AccessToken = strings.TrimSpace(os.Getenv(tokenEnv))
	}
	if !enabledSet && !cfg.Enabled && (cfg.ControlURL != "" || cfg.HubURL != "" || cfg.AccessToken != "" || cfg.DataDir != "" || cfg.DeviceName != "" || cfg.Region != "") {
		cfg.Enabled = true
	}
	return cfg, enabledSet, nil
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
