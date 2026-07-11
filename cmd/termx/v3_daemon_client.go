package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/wire"
	unixtransport "github.com/lozzow/termx/shared/transport/unix"
)

var (
	v3DialClient            = dialV3Client
	startV3Daemon           = startCoreV2Daemon
	startV3DaemonWithConfig = startCoreV2DaemonWithConfig
	osExecutable            = os.Executable
)

func dialV3Client(path string) (*protocol.Client, error) {
	conn, err := unixtransport.Dial(path)
	if err != nil {
		return nil, err
	}
	client := protocol.NewClient(conn)
	if err := client.Hello(context.Background(), protocol.Hello{
		Version: wire.Version,
		Client:  "cmd/termx-v3",
	}); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func dialOrStartV3Client(path string, logFile string, logger *slog.Logger) (*protocol.Client, error) {
	return dialOrStartV3ClientWithConfig(path, logFile, "", logger)
}

func dialOrStartV3ClientWithConfig(path string, logFile string, configPath string, logger *slog.Logger) (*protocol.Client, error) {
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
	if startErr := startCoreV2DaemonForConfig(path, logFile, configPath); startErr != nil {
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

func dialOrStartV3TransportWithConfig(path string, logFile string, configPath string, logger *slog.Logger) (*unixtransport.Transport, error) {
	transport, err := unixtransport.Dial(path)
	if err == nil {
		if logger != nil {
			logger.Debug("connected to existing core-v2 daemon transport", "socket", path)
		}
		return transport, nil
	}
	if logger != nil {
		logger.Warn("initial core-v2 daemon transport dial failed, attempting v3 auto-start", "socket", path, "error", err)
	}
	if startErr := startCoreV2DaemonForConfig(path, logFile, configPath); startErr != nil {
		return nil, fmt.Errorf("start core-v2 daemon: %w", startErr)
	}
	if waitErr := waitForSocket(path, 5*time.Second, func() error {
		c, dialErr := unixtransport.Dial(path)
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
		logger.Info("auto-started core-v2 daemon transport became ready", "socket", path)
	}
	return unixtransport.Dial(path)
}

func startCoreV2Daemon(path string, logFile string) error {
	return startCoreV2DaemonWithConfig(path, logFile, "")
}

func startCoreV2DaemonWithConfig(path string, logFile string, configPath string) error {
	cmd, err := buildStartCoreV2DaemonCommandWithConfig(path, logFile, configPath)
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

func startCoreV2DaemonForConfig(path string, logFile string, configPath string) error {
	if strings.TrimSpace(configPath) == "" {
		return startV3Daemon(path, logFile)
	}
	if startV3DaemonWithConfig == nil {
		return fmt.Errorf("core-v2 daemon config starter is nil")
	}
	// 中文说明：显式 --config 入口触发 auto-start 时，必须启动同一
	// config 的 daemon；普通 v3 auto-start 仍走可替换的 startV3Daemon。
	return startV3DaemonWithConfig(path, logFile, configPath)
}

func buildStartCoreV2DaemonCommand(path string, logFile string) (*exec.Cmd, error) {
	return buildStartCoreV2DaemonCommandWithConfig(path, logFile, "")
}

func buildStartCoreV2DaemonCommandWithConfig(path string, logFile string, configPath string) (*exec.Cmd, error) {
	exe, err := osExecutable()
	if err != nil {
		return nil, err
	}
	args := []string{"--socket", path}
	if logFile != "" {
		args = append(args, "--log-file", logFile)
	}
	if configPath = strings.TrimSpace(configPath); configPath != "" {
		args = append(args, "--config", configPath)
	}
	if envBool("TERMX_HISTORY_DISABLE") {
		cmd := exec.Command(exe, append(args, "daemon")...)
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "TERMX_HISTORY_DISABLE=1")
		return cmd, nil
	}
	// 默认 daemon 已切换为 core-v2；自动启动不得落回 legacy daemon。
	args = append(args, "daemon")
	return exec.Command(exe, args...), nil
}
