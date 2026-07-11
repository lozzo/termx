// Package main builds the private desktop/headless TermX Cloud Companion artifact.
//
// 该 executable 只服务固定 local IPC contract；WebRTC、DTLS、DeviceIdentity、CapabilityGrant、
// DataChannel 与 terminal protocol 始终留在公开 termx 进程。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lozzow/termx/private/cloud/companion"
	"github.com/lozzow/termx/private/cloud/companion/cloudservice"
	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/ipc"
)

var (
	companionVersion = "v0.0.0-dev"
	buildChannel     = "development"
)

const keyringService = "io.termx.cloud-companion"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: termx-cloud serve|version|licenses")
	}
	switch args[0] {
	case "version":
		return json.NewEncoder(stdout).Encode(map[string]string{"version": companionVersion, "build_channel": buildChannel})
	case "licenses":
		if len(args) != 1 {
			return fmt.Errorf("unexpected termx-cloud licenses arguments")
		}
		_, err := io.WriteString(stdout, thirdPartyNotices)
		return err
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(stderr)
		var endpoint string
		var smoke bool
		var devManifest string
		var profile string
		flags.StringVar(&endpoint, "socket", "", "fixed user-scoped IPC endpoint")
		flags.BoolVar(&smoke, "smoke", false, "serve installer compatibility handshake only")
		flags.StringVar(&devManifest, "dev-manifest", "", "explicit dev-local cloud runtime manifest")
		flags.StringVar(&profile, "profile", "default", "credential-isolated Companion profile")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected termx-cloud serve arguments")
		}
		return serve(ctx, endpoint, smoke, devManifest, profile)
	default:
		return fmt.Errorf("unknown termx-cloud command %q", args[0])
	}
}

func serve(ctx context.Context, endpoint string, smoke bool, devManifest, profile string) error {
	if smoke {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	store, err := credentialStore(smoke)
	if err != nil {
		return err
	}
	sessions, err := session.NewManager(store, profile)
	if err != nil {
		return err
	}
	controlPlane, hub, err := cloudAdapters(devManifest, smoke)
	if err != nil {
		return err
	}
	service, err := companion.NewService(companion.Config{
		CompanionVersion: companionVersion,
		BuildChannel:     buildChannel,
		StreamCapacity:   64,
		Capabilities: []cloudpb.CompanionCapability{
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_PATH_QUALITY,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_SMART_ROUTE,
		},
	}, sessions, controlPlane, hub)
	if err != nil {
		return err
	}
	listener, err := ipc.Listen(endpoint)
	if err != nil {
		return err
	}
	serveContext, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &ipc.Server{
		NewClient:  func() (cloudcompanion.FullClient, error) { return service.NewConnection(), nil },
		OnShutdown: cancel,
	}
	return server.Serve(serveContext, listener)
}

// cloudAdapters 保持 Companion 默认 fail closed，并只在 development build 收到显式 manifest 时启用 dev-local 网络。
// production/smoke 路径拒绝 loopback adapter；缺少配置时绝不读取环境变量或尝试旧 Hub fallback。
func cloudAdapters(devManifest string, smoke bool) (cloudservice.ControlPlaneAdapter, cloudservice.HubAdapter, error) {
	if devManifest == "" {
		adapter := cloudservice.NewUnconfiguredAdapter()
		return adapter, adapter, nil
	}
	if smoke {
		return nil, nil, fmt.Errorf("installer smoke does not accept a dev cloud manifest")
	}
	if buildChannel != "development" {
		return nil, nil, fmt.Errorf("dev cloud manifest is disabled for build channel %q", buildChannel)
	}
	manifest, err := httpapi.LoadManifest(devManifest)
	if err != nil {
		return nil, nil, err
	}
	adapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: manifest.ControlPlaneURL, HubURL: manifest.HubURL})
	if err != nil {
		return nil, nil, err
	}
	return adapter, adapter, nil
}

func credentialStore(smoke bool) (session.OSCredentialStore, error) {
	if smoke {
		return unavailableCredentialStore{}, nil
	}
	return session.NewKeyringStore(keyringService)
}

type unavailableCredentialStore struct{}

func (unavailableCredentialStore) LoadSecret(context.Context, string) ([]byte, error) {
	return nil, session.ErrNotFound
}

func (unavailableCredentialStore) StoreSecret(context.Context, string, []byte) error {
	return errors.New("credential writes are disabled during installer smoke")
}

func (unavailableCredentialStore) DeleteSecret(context.Context, string) error {
	return session.ErrNotFound
}
