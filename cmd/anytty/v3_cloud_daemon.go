package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"

	clouddaemon "github.com/anytty/anytty/cloud/daemon"
)

// startV3CloudDaemon 复用同一 DeviceIdentity/AccessStore/Core，并让 Cloud runtime 只拥有发现和信令。
func startV3CloudDaemon(ctx context.Context, core v3RemoteDaemonCore, clientAccess v3ClientAccessRuntime, logger *slog.Logger) (func(), error) {
	record, err := clouddaemon.LoadRecord(v3CloudEnrollmentRecordPath())
	if errors.Is(err, os.ErrNotExist) {
		return func() {}, nil
	}
	if err != nil {
		return nil, err
	}
	runtime, err := clouddaemon.NewAuthorizedRuntime(
		record, clientAccess.Identity, clientAccess.Store, core, "development",
		func() {
			logger.Info("AnyTTY Cloud DataChannel 已进入端到端授权", "daemon_id", record.DaemonID)
		},
		func(sessionErr error) {
			if errors.Is(sessionErr, io.EOF) || errors.Is(sessionErr, context.Canceled) {
				return
			}
			logger.Warn("AnyTTY Cloud DataChannel 会话异常结束", "daemon_id", record.DaemonID, "error", sessionErr)
		},
	)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if runErr := runtime.Run(runCtx); runErr != nil && runCtx.Err() == nil {
			logger.Error("AnyTTY Cloud daemon runtime stopped", "error", runErr)
		}
	}()
	logger.Info("AnyTTY Cloud daemon runtime started", "daemon_id", record.DaemonID, "controller", record.ControllerAddress)
	return func() { cancel(); <-done }, nil
}
