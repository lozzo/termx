// Package main builds the private desktop/headless TermX Cloud Companion artifact.
//
// 该 executable 只服务固定 local IPC contract；WebRTC、DTLS、DeviceIdentity、CapabilityGrant、
// DataChannel 与 terminal protocol 始终留在公开 termx 进程。
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
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
	companionVersion                  = "v0.0.0-dev"
	buildChannel                      = "development"
	embeddedDevelopmentManifestBase64 = ""
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
	runtimeConfiguration, err := cloudRuntimeConfigurationFor(devManifest, smoke)
	if err != nil {
		return err
	}
	service, err := companion.NewService(companion.Config{
		CompanionVersion:        companionVersion,
		BuildChannel:            buildChannel,
		StreamCapacity:          64,
		AllowPublicHTTPLoginURL: runtimeConfiguration.allowPublicHTTPLoginURL,
		Capabilities: []cloudpb.CompanionCapability{
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_PATH_QUALITY,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_SMART_ROUTE,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_DIRECTORY,
		},
	}, sessions, runtimeConfiguration.controlPlane, runtimeConfiguration.hub)
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

type cloudRuntimeConfiguration struct {
	controlPlane            cloudservice.ControlPlaneAdapter
	hub                     cloudservice.HubAdapter
	allowPublicHTTPLoginURL bool
}

// cloudRuntimeConfigurationFor 是 Companion development runtime 的唯一装配点。
// Adapter 地址与 HTTP 登录策略必须来自同一份已验证 manifest，禁止各自重读配置形成第二真值。
func cloudRuntimeConfigurationFor(devManifest string, smoke bool) (cloudRuntimeConfiguration, error) {
	embedded := strings.TrimSpace(embeddedDevelopmentManifestBase64)
	if smoke {
		if devManifest != "" {
			return cloudRuntimeConfiguration{}, fmt.Errorf("installer smoke does not accept a dev cloud manifest")
		}
		adapter := cloudservice.NewUnconfiguredAdapter()
		return cloudRuntimeConfiguration{controlPlane: adapter, hub: adapter}, nil
	}
	if devManifest == "" && embedded == "" {
		adapter := cloudservice.NewUnconfiguredAdapter()
		return cloudRuntimeConfiguration{controlPlane: adapter, hub: adapter}, nil
	}
	if buildChannel != "development" {
		return cloudRuntimeConfiguration{}, fmt.Errorf("dev cloud manifest is disabled for build channel %q", buildChannel)
	}
	if devManifest != "" && embedded != "" {
		return cloudRuntimeConfiguration{}, fmt.Errorf("embedded development manifest cannot be overridden")
	}
	var manifest httpapi.Manifest
	var err error
	if embedded != "" {
		payload, decodeErr := decodeEmbeddedDevelopmentManifest(embedded)
		if decodeErr != nil || len(payload) == 0 || len(payload) > 64<<10 {
			return cloudRuntimeConfiguration{}, fmt.Errorf("invalid embedded development manifest")
		}
		manifest, err = httpapi.ParseManifest(payload)
	} else {
		manifest, err = httpapi.LoadManifest(devManifest)
	}
	if err != nil {
		return cloudRuntimeConfiguration{}, err
	}
	adapter, err := httpapi.New(httpapi.Config{
		ControlPlaneURL: manifest.ControlPlaneURL, HubURL: manifest.HubURL,
		AllowPublicHTTP: manifest.Profile == httpapi.ProfileStagingPublicHTTP,
	})
	if err != nil {
		return cloudRuntimeConfiguration{}, err
	}
	return cloudRuntimeConfiguration{
		controlPlane: adapter, hub: adapter,
		allowPublicHTTPLoginURL: manifest.Profile == httpapi.ProfileStagingPublicHTTP,
	}, nil
}

func decodeEmbeddedDevelopmentManifest(encoded string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding} {
		if payload, err := encoding.DecodeString(encoded); err == nil {
			return payload, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 development manifest")
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
