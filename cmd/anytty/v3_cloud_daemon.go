package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"time"

	clouddaemon "github.com/anytty/anytty/cloud/daemon"
)

// startV3CloudDaemon 复用同一 DeviceIdentity/AccessStore/Core，并让 Cloud runtime 只拥有发现和信令。
func startV3CloudDaemon(ctx context.Context, core v3RemoteDaemonCore, clientAccess v3ClientAccessRuntime, logger *slog.Logger) (func(), error) {
	recordPath := v3CloudEnrollmentRecordPath()
	record, err := clouddaemon.LoadRecord(recordPath)
	if errors.Is(err, os.ErrNotExist) {
		record = clouddaemon.EnrollmentRecord{}
		err = nil
	}
	if err != nil {
		return nil, err
	}
	var initial *clouddaemon.Runtime
	if record.DaemonID != "" {
		initial, err = newV3CloudRuntime(record, recordPath, core, clientAccess, logger)
		if err != nil {
			return nil, err
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime := initial
		for runCtx.Err() == nil {
			if runtime == nil {
				next, loadErr := clouddaemon.LoadRecord(recordPath)
				if errors.Is(loadErr, os.ErrNotExist) {
					if !waitForCloudEnrollment(runCtx) {
						return
					}
					continue
				}
				if loadErr != nil {
					logger.Error("AnyTTY Cloud enrollment could not be loaded", "error", loadErr)
					return
				}
				runtime, loadErr = newV3CloudRuntime(next, recordPath, core, clientAccess, logger)
				if loadErr != nil {
					logger.Error("AnyTTY Cloud daemon runtime could not start", "error", loadErr)
					return
				}
				record = next
			}
			logger.Info("AnyTTY Cloud daemon runtime started", "daemon_id", record.DaemonID)
			runErr := runtime.Run(runCtx)
			if runErr != nil && runCtx.Err() == nil {
				logger.Error("AnyTTY Cloud daemon runtime stopped", "error", runErr)
			}
			runtime = nil
		}
	}()
	return func() { cancel(); <-done }, nil
}

func newV3CloudRuntime(record clouddaemon.EnrollmentRecord, recordPath string, core v3RemoteDaemonCore, clientAccess v3ClientAccessRuntime, logger *slog.Logger) (*clouddaemon.Runtime, error) {
	return clouddaemon.NewAuthorizedRuntime(
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
		clouddaemon.WithPionLogger(logger),
		clouddaemon.WithEnrollmentRecordPath(recordPath),
	)
}

func waitForCloudEnrollment(ctx context.Context) bool {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
