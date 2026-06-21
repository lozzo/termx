package main

import (
	"context"
	"os/exec"
	"runtime"

	coreprotocol "github.com/lozzow/termx/internal/protocol"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
)

var (
	defaultRemoteControlURL = "http://114.66.58.243:12306"

	pairStartClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.PairStartParams) (*remoteprotocol.PairStartResult, error) {
		client, err := dialOrStartV3Client(resolveV3Socket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var out coreprotocol.RemotePairStartResult
		if err := client.Call(ctx, "remote.pair.start", corePairStartParamsFromRemote(params), &out); err != nil {
			return nil, err
		}
		result := remotePairStartResultFromCore(out)
		return &result, nil
	}
	remoteStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.Status, error) {
		client, err := dialOrStartV3Client(resolveV3Socket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var out coreprotocol.RemoteStatus
		if err := client.Call(ctx, "remote.status", map[string]any{}, &out); err != nil {
			return nil, err
		}
		status := remoteStatusFromCore(out)
		return &status, nil
	}
	remoteLocalEnableClient = func(ctx context.Context, socketPath string, logFile string, params remoteprotocol.LocalEnableParams) (*remoteprotocol.LocalStatus, error) {
		client, err := dialOrStartV3Client(resolveV3Socket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var out coreprotocol.RemoteLocalStatus
		if err := client.Call(ctx, "remote.local.enable", coreLocalEnableParamsFromRemote(params), &out); err != nil {
			return nil, err
		}
		status := remoteLocalStatusFromCore(out)
		return &status, nil
	}
	remoteLocalStatusClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		client, err := dialOrStartV3Client(resolveV3Socket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var out coreprotocol.RemoteLocalStatus
		if err := client.Call(ctx, "remote.local.status", map[string]any{}, &out); err != nil {
			return nil, err
		}
		status := remoteLocalStatusFromCore(out)
		return &status, nil
	}
	remoteLocalDisableClient = func(ctx context.Context, socketPath string, logFile string) (*remoteprotocol.LocalStatus, error) {
		client, err := dialOrStartV3Client(resolveV3Socket(socketPath), resolveLogFilePath(logFile), nil)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var out coreprotocol.RemoteLocalStatus
		if err := client.Call(ctx, "remote.local.disable", map[string]any{}, &out); err != nil {
			return nil, err
		}
		status := remoteLocalStatusFromCore(out)
		return &status, nil
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
)
