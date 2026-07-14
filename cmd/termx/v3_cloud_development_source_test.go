package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
)

func TestBundledDevelopmentCompanionSourceVerifiesSiblingDigest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission validation is platform-specific")
	}
	dir := t.TempDir()
	termxPath := filepath.Join(dir, "termx")
	companionPath := filepath.Join(dir, "termx-cloud")
	if err := os.WriteFile(termxPath, []byte("termx"), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("verified development companion")
	if err := os.WriteFile(companionPath, payload, 0o700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	restore := configureBundledDevelopmentSourceTest(t, termxPath, hex.EncodeToString(hash[:]))
	defer restore()

	source, configured, err := v3BundledDevelopmentCompanionSource()
	if err != nil || !configured {
		t.Fatalf("source = (%v, %v, %v)", source, configured, err)
	}
	installation, err := source.Status()
	if err != nil {
		t.Fatal(err)
	}
	realTermxPath, err := filepath.EvalSymlinks(termxPath)
	if err != nil {
		t.Fatal(err)
	}
	wantCompanionPath := filepath.Join(filepath.Dir(realTermxPath), "termx-cloud")
	if installation.BinaryPath != wantCompanionPath || installation.Version != "v0.0.0-dev" || installation.Channel != "development" {
		t.Fatalf("installation = %#v", installation)
	}

	if err := os.WriteFile(companionPath, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Status(); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("tampered status error = %v", err)
	}
}

func TestBundledDevelopmentCompanionSourceRejectsStableBuild(t *testing.T) {
	restore := configureBundledDevelopmentSourceTest(t, filepath.Join(t.TempDir(), "termx"), string(make([]byte, 64)))
	defer restore()
	termxBuildVersion = "v1.0.0"
	if _, configured, err := v3BundledDevelopmentCompanionSource(); configured || !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("stable bundled source = configured %v, err %v", configured, err)
	}
}

func configureBundledDevelopmentSourceTest(t *testing.T, termxPath, digest string) func() {
	t.Helper()
	previousExecutable := v3ExecutablePath
	previousTermxVersion := termxBuildVersion
	previousName := cloudDevelopmentCompanionName
	previousDigest := cloudDevelopmentCompanionSHA256
	previousVersion := cloudDevelopmentCompanionVersion
	previousChannel := cloudDevelopmentCompanionChannel
	v3ExecutablePath = func() (string, error) { return termxPath, nil }
	termxBuildVersion = v3DevelopmentBuildVersion
	cloudDevelopmentCompanionName = "termx-cloud"
	cloudDevelopmentCompanionSHA256 = digest
	cloudDevelopmentCompanionVersion = "v0.0.0-dev"
	cloudDevelopmentCompanionChannel = "development"
	return func() {
		v3ExecutablePath = previousExecutable
		termxBuildVersion = previousTermxVersion
		cloudDevelopmentCompanionName = previousName
		cloudDevelopmentCompanionSHA256 = previousDigest
		cloudDevelopmentCompanionVersion = previousVersion
		cloudDevelopmentCompanionChannel = previousChannel
	}
}
