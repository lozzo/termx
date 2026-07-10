package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	corev2 "github.com/lozzow/termx/termx-core-v2"
	hubclient "github.com/lozzow/termx/termx-hub/client"
	remotev2daemon "github.com/lozzow/termx/termx-remote-v2/daemon"
	remotev2webrtc "github.com/lozzow/termx/termx-remote-v2/webrtc"
	"github.com/lozzow/termx/termx-shared/remoteauth"
	"github.com/lozzow/termx/termx-shared/transport"
)

type v3RemoteCore interface {
	ListTerminals() []corev2.TerminalInfo
	ServeScopedTransport(context.Context, transport.Transport, corev2.TransportScope) error
}

func startV3RemoteAgent(ctx context.Context, server coreV2Server, logger *slog.Logger) (func(), error) {
	hubURL := strings.TrimSpace(os.Getenv("TERMX_HUB_URL"))
	if hubURL == "" {
		return func() {}, nil
	}
	core, ok := server.(v3RemoteCore)
	if !ok {
		return nil, fmt.Errorf("core-v2 server does not expose remote transport boundary")
	}
	deviceID := strings.TrimSpace(os.Getenv("TERMX_REMOTE_DEVICE_ID"))
	if deviceID == "" {
		return nil, fmt.Errorf("TERMX_REMOTE_DEVICE_ID is required when TERMX_HUB_URL is set")
	}
	bearerToken := strings.TrimSpace(os.Getenv("TERMX_HUB_AGENT_TOKEN"))
	if bearerToken == "" {
		return nil, fmt.Errorf("TERMX_HUB_AGENT_TOKEN is required when TERMX_HUB_URL is set")
	}
	dataDir := resolveStateFilePath("remote-v2")
	identity, err := remoteauth.LoadOrCreateIdentity(dataDir, deviceID)
	if err != nil {
		return nil, err
	}
	revocations, err := remoteauth.LoadRevocationStore(dataDir)
	if err != nil {
		return nil, err
	}
	hub, err := hubclient.Dial(ctx, hubclient.DialOptions{URL: hubURL})
	if err != nil {
		return nil, err
	}
	agentCtx, cancel := context.WithCancel(ctx)
	agent := remotev2daemon.Agent{
		BearerToken: bearerToken,
		Registration: hubclient.Registration{
			AgentID: deviceID, DeviceID: deviceID, MachineID: deviceID,
			DisplayName: remoteDisplayName(deviceID), Hostname: remoteHostname(), Platform: runtime.GOOS, Version: "termx-v3",
		},
		Connect: func(connectCtx context.Context, token string, registration hubclient.Registration) (remotev2daemon.HubStream, hubclient.RegistrationAck, error) {
			return hub.Connect(connectCtx, token, registration)
		},
		Answerer: remotev2webrtc.Answerer{Acceptor: remotev2daemon.SessionAcceptor{
			Core: core, DeviceFingerprint: identity.Fingerprint, Revocations: revocations,
		}},
		Inventory: func(context.Context) []hubclient.Terminal {
			items := core.ListTerminals()
			terminals := make([]hubclient.Terminal, 0, len(items))
			for _, item := range items {
				terminals = append(terminals, hubclient.Terminal{ID: item.ID, Name: item.Name, RemoteEnabled: true})
			}
			return terminals
		},
	}
	go func() {
		if err := agent.Run(agentCtx); err != nil && agentCtx.Err() == nil {
			logger.Error("hub/P2P daemon agent stopped", "hub_url", hubURL, "device_id", deviceID, "error", err)
		}
	}()
	logger.Info("hub/P2P daemon agent started", "hub_url", hubURL, "device_id", deviceID, "device_fingerprint", identity.Fingerprint)
	return func() {
		cancel()
		_ = hub.Close()
	}, nil
}

func remoteDisplayName(fallback string) string {
	if value := strings.TrimSpace(os.Getenv("TERMX_REMOTE_DISPLAY_NAME")); value != "" {
		return value
	}
	return fallback
}

func remoteHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}
	return hostname
}
