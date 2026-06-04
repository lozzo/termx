package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	unixtransport "github.com/lozzow/termx/termx-shared/transport/unix"
)

var (
	v3DialClient  = dialV3Client
	startV3Daemon = startCoreV2Daemon
	osExecutable  = os.Executable
)

func dialV3Client(path string) (*protocol.Client, error) {
	conn, err := unixtransport.Dial(path)
	if err != nil {
		return nil, err
	}
	client := protocol.NewClient(conn)
	if err := client.Hello(context.Background(), protocol.Hello{
		Version: wire.Version,
		Client:  "termx-cli-v3",
	}); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func dialOrStartV3Client(path string, logFile string, logger *slog.Logger) (*protocol.Client, error) {
	client, err := v3DialClient(path)
	if err == nil {
		if logger != nil {
			logger.Debug("connected to existing core-v2 daemon", "socket", path)
		}
		return client, nil
	}
	if logger != nil {
		logger.Warn("initial core-v2 daemon dial failed, attempting v3 auto-start", "socket", path, "error", err)
	}
	if startErr := startV3Daemon(path, logFile); startErr != nil {
		return nil, fmt.Errorf("start core-v2 daemon: %w", startErr)
	}
	if waitErr := waitForSocket(path, 5*time.Second, func() error {
		c, dialErr := v3DialClient(path)
		if dialErr != nil {
			return dialErr
		}
		if c != nil {
			_ = c.Close()
		}
		return nil
	}); waitErr != nil {
		return nil, waitErr
	}
	if logger != nil {
		logger.Info("auto-started core-v2 daemon became ready", "socket", path)
	}
	return v3DialClient(path)
}

func startCoreV2Daemon(path string, logFile string) error {
	cmd, err := buildStartCoreV2DaemonCommand(path, logFile)
	if err != nil {
		return err
	}
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

func buildStartCoreV2DaemonCommand(path string, logFile string) (*exec.Cmd, error) {
	exe, err := osExecutable()
	if err != nil {
		return nil, err
	}
	args := []string{"--socket", path}
	if logFile != "" {
		args = append(args, "--log-file", logFile)
	}
	// 自动启动必须显式进入 v3 daemon，不能落回默认 legacy daemon。
	args = append(args, "v3", "daemon")
	return exec.Command(exe, args...), nil
}

func resolveV3Socket(path string) string {
	if path != "" {
		return path
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return runtimeDir + "/termx-v2.sock"
	}
	return fmt.Sprintf("%s/termx-v2-%d.sock", os.TempDir(), os.Getuid())
}
