package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	clouddaemon "github.com/muxvia/muxvia/cloud/daemon"
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
	runtime, err := clouddaemon.NewAuthorizedRuntime(record, clientAccess.Identity, clientAccess.Store, core, "development")
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if runErr := runtime.Run(runCtx); runErr != nil && runCtx.Err() == nil {
			logger.Error("Muxvia Cloud daemon runtime stopped", "error", runErr)
		}
	}()
	logger.Info("Muxvia Cloud daemon runtime started", "daemon_id", record.DaemonID, "controller", record.ControllerAddress)
	return func() { cancel(); <-done }, nil
}
