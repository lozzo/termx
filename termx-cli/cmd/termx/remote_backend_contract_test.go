package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreprotocol "github.com/lozzow/termx/internal/protocol"
	corev2 "github.com/lozzow/termx/termx-core-v2"
	remote "github.com/lozzow/termx/termx-remote"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	"github.com/lozzow/termx/termx-remote/protocol/runtimepb"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
)

func TestRemoteBackendContractRoutesEverythingThroughCoreV2Truth(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	factory := &remoteAdapterProcessFactory{}
	server := corev2.NewServer(corev2.WithSocketPath(socketPath), corev2.WithProcessFactory(factory))
	service := remote.NewService(
		remoteprotocol.Config{Enabled: true, DataDir: t.TempDir(), DeviceName: "backend-contract"},
		newCoreV2RemoteDaemonAdapter(server),
	)
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("start remote service: %v", err)
	}
	defer func() {
		if err := service.Close(context.Background()); err != nil {
			t.Fatalf("close remote service: %v", err)
		}
	}()
	server.SetRemoteService(coreV2RemoteServiceHook{service: service})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.ListenAndServe(ctx)
	}()
	defer func() {
		cancel()
		_ = server.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("core-v2 server did not stop")
		}
	}()
	if err := waitForSocket(socketPath, 2*time.Second, func() error {
		client, err := dialV3Client(socketPath)
		if err != nil {
			return err
		}
		return client.Close()
	}); err != nil {
		t.Fatalf("core-v2 daemon did not become ready: %v", err)
	}

	daemonClient, err := dialV3Client(socketPath)
	if err != nil {
		t.Fatalf("dial core-v2 daemon: %v", err)
	}
	defer daemonClient.Close()
	var remoteStatus coreprotocol.RemoteStatus
	if err := daemonClient.Call(context.Background(), "remote.status", map[string]any{}, &remoteStatus); err != nil {
		t.Fatalf("remote.status did not route through core-v2 daemon hook: %v", err)
	}
	if remoteStatus.DeviceName != "backend-contract" {
		t.Fatalf("remote.status did not come from termx-remote service: %#v", remoteStatus)
	}
	var localStatus coreprotocol.RemoteLocalStatus
	if err := daemonClient.Call(context.Background(), "remote.local.status", map[string]any{}, &localStatus); err != nil {
		t.Fatalf("remote.local.status did not route through core-v2 daemon hook: %v", err)
	}
	if localStatus.Enabled {
		t.Fatalf("remote.local.status should start disabled before enable, got %#v", localStatus)
	}
	var enabled coreprotocol.RemoteLocalStatus
	if err := daemonClient.Call(context.Background(), "remote.local.enable", coreprotocol.RemoteLocalEnableParams{
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
	}, &enabled); err != nil {
		t.Fatalf("remote.local.enable did not route through core-v2 daemon hook: %v", err)
	}
	if !enabled.Enabled || strings.TrimSpace(enabled.HTTPURL) == "" || !enabled.ICETCPEnabled {
		t.Fatalf("remote.local.enable returned incomplete remote service status: %#v", enabled)
	}
	var disabled coreprotocol.RemoteLocalStatus
	if err := daemonClient.Call(context.Background(), "remote.local.disable", map[string]any{}, &disabled); err != nil {
		t.Fatalf("remote.local.disable did not route through core-v2 daemon hook: %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("remote.local.disable should disable local runtime, got %#v", disabled)
	}
	var pair coreprotocol.RemotePairStartResult
	if err := daemonClient.Call(context.Background(), "remote.pair.start", coreprotocol.RemotePairStartParams{TTLSeconds: 30, AuthTTLSeconds: 60}, &pair); err != nil {
		t.Fatalf("remote.pair.start did not route through core-v2 daemon hook: %v", err)
	}
	if pair.MachineID == "" || pair.PairSessionID == "" || pair.PairSecret == "" {
		t.Fatalf("remote.pair.start returned incomplete remote service payload: %#v", pair)
	}

	events, unsubscribe, err := service.SubscribeRemoteEvents(context.Background(), remotedEventFilters(coreprotocol.EventTerminalCreated, coreprotocol.EventTerminalMetadataChanged))
	if err != nil {
		t.Fatalf("subscribe runtime events: %v", err)
	}
	defer unsubscribe()

	createBody := routeRemoteTerminalAPI(t, service, "create", &runtimepb.TerminalCreateRequest{
		Name:    "contract shell",
		Command: []string{"shell"},
		Dir:     "/contract",
		Env:     []string{"CONTRACT=1"},
	})
	var created runtimepb.TerminalInventoryItem
	unmarshalRuntimeAPI(t, createBody, &created)
	if created.GetTerminalId() == "" {
		t.Fatalf("remote runtime create returned empty terminal id: %#v", &created)
	}
	spec := factory.spawnedSpec(created.GetTerminalId())
	if spec.TerminalID != created.GetTerminalId() || spec.Dir != "/contract" {
		t.Fatalf("remote runtime create did not reach core-v2 process truth: %#v", spec)
	}
	requireRuntimeEvent(t, events, "terminal_created", created.GetTerminalId())

	routeRemoteStorageAPI(t, service, "/storage/put", &runtimepb.StoragePutRequest{
		AppId:   "remote-ui",
		Scope:   string(coreprotocol.StorageScopePublic),
		OwnerId: "contract-app",
		Key:     "backend/proof",
		Value:   []byte("core-v2"),
	})
	stored, err := server.StorageGet(context.Background(), "remote-ui", corev2.StorageScopePublic, "contract-app", "backend/proof")
	if err != nil || string(stored.Value) != "core-v2" {
		t.Fatalf("remote storage did not write core-v2 storage truth entry=%#v err=%v", stored, err)
	}

	transportClient, closeTransportClient := newRemoteServiceProtocolClient(t, service, "webrtc:terminal:machine-1:"+created.GetTerminalId())
	defer closeTransportClient()
	var terminalInfo coreprotocol.TerminalInfo
	if err := transportClient.Call(context.Background(), "get", coreprotocol.GetParams{TerminalID: created.GetTerminalId()}, &terminalInfo); err != nil {
		t.Fatalf("remote transport did not reach core-v2 scoped protocol session: %v", err)
	}
	if terminalInfo.ID != created.GetTerminalId() {
		t.Fatalf("remote transport returned wrong terminal info: %#v", terminalInfo)
	}
	if err := transportClient.Call(context.Background(), "get", coreprotocol.GetParams{TerminalID: "other-terminal"}, &coreprotocol.TerminalInfo{}); err == nil || !strings.Contains(err.Error(), "transport scope") {
		t.Fatalf("remote transport did not enforce terminal scope, got %v", err)
	}

	if _, err := server.SetMetadata(context.Background(), created.GetTerminalId(), "contract renamed", nil); err != nil {
		t.Fatalf("set metadata through core-v2 truth: %v", err)
	}
	requireRuntimeEvent(t, events, "terminal_metadata_changed", created.GetTerminalId())
}

func TestRemoteBackendLegacyFallbackBoundaryIsGone(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "..", "..", "termx-core"),
		filepath.Join("..", "..", "..", "tuiv2"),
		"remote_runtime.go",
		"remote_protocol_codec.go",
		"legacy_commands.go",
	} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("legacy fallback path must not be restored: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %s: %v", path, err)
		}
	}
}

func remotedEventFilters(types ...coreprotocol.EventType) remotertc.EventFilters {
	out := remotertc.EventFilters{Types: make([]int, 0, len(types))}
	for _, typ := range types {
		out.Types = append(out.Types, int(typ))
	}
	return out
}
