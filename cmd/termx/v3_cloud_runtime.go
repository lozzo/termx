package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/activation"
	"github.com/lozzow/termx/shared/cloudcompanion/installer"
)

var (
	termxBuildVersion             = "v0.0.0-dev"
	cloudReleaseOrigin            = "https://releases.termx.dev/cloud-companion"
	cloudReleaseRootKeyID         = ""
	cloudReleaseRootPublicKey     = ""
	cloudRuntimeOnce              sync.Once
	cloudRuntimeManager           *activation.Manager
	cloudRuntimeErr               error
	newV3CloudInstallerForCommand func() (v3CloudInstaller, error) = func() (v3CloudInstaller, error) { return newV3CloudInstaller() }
	openV3CloudLifecycleClient                                     = defaultOpenV3CloudLifecycleClient
	openV3CloudCompanion                                           = defaultOpenV3CloudCompanion
)

type v3CloudClient interface {
	cloudcompanion.FullClient
	io.Closer
}

type v3CloudInstaller interface {
	InstallRelease(context.Context, installer.Request) (installer.Installation, error)
	Status() (installer.Installation, error)
	Uninstall() error
}

func newV3CloudInstaller() (*installer.Installer, error) {
	trustedKeys, err := v3CloudReleaseRoots()
	if err != nil {
		return nil, err
	}
	return installer.New(installer.Config{
		Origin: cloudReleaseOrigin, TrustedKeys: trustedKeys,
		Smoke: activation.SmokeFunc(termxBuildVersion),
	})
}

func defaultV3CloudManager() (*activation.Manager, error) {
	cloudRuntimeOnce.Do(func() {
		cloudInstaller, err := newV3CloudInstaller()
		if err != nil {
			cloudRuntimeErr = err
			return
		}
		cloudRuntimeManager, cloudRuntimeErr = activation.New(activation.Config{Installations: cloudInstaller, TermxVersion: termxBuildVersion})
	})
	return cloudRuntimeManager, cloudRuntimeErr
}

func defaultOpenV3CloudLifecycleClient(ctx context.Context, role cloudpb.CallerRole, capabilities ...cloudpb.CompanionCapability) (v3CloudClient, error) {
	if strings.TrimSpace(cloudReleaseRootKeyID) == "" || strings.TrimSpace(cloudReleaseRootPublicKey) == "" {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING, "this termx build does not include the official Cloud Companion release root")
	}
	manager, err := defaultV3CloudManager()
	if err != nil {
		return nil, err
	}
	return manager.Open(ctx, role, capabilities...)
}

func defaultOpenV3CloudCompanion(ctx context.Context) (cloudcompanion.Client, error) {
	return openV3CloudLifecycleClient(ctx, cloudpb.CallerRole_CALLER_ROLE_TUI,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_PATH_QUALITY,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SMART_ROUTE,
	)
}

func v3CloudReleaseRoots() (map[string]ed25519.PublicKey, error) {
	keyID := strings.TrimSpace(cloudReleaseRootKeyID)
	encoded := strings.TrimSpace(cloudReleaseRootPublicKey)
	if keyID == "" || encoded == "" {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "official Cloud Companion release root is not embedded in this termx build")
	}
	publicKey, err := decodeCloudReleasePublicKey(encoded)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid embedded Cloud Companion release root")
	}
	return map[string]ed25519.PublicKey{keyID: ed25519.PublicKey(publicKey)}, nil
}

func decodeCloudReleasePublicKey(encoded string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.URLEncoding} {
		if decoded, err := encoding.DecodeString(encoded); err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 public key")
}
