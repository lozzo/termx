package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

func TestBundledDevelopmentCompanionSourceVerifiesSiblingDigest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission validation is platform-specific")
	}
	dir := t.TempDir()
	muxviaPath := filepath.Join(dir, "muxvia")
	companionPath := filepath.Join(dir, "muxvia-cloud")
	if err := os.WriteFile(muxviaPath, []byte("muxvia"), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("verified development companion")
	if err := os.WriteFile(companionPath, payload, 0o700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	restore := configureBundledDevelopmentSourceTest(t, muxviaPath, hex.EncodeToString(hash[:]))
	defer restore()

	source, configured, err := v3BundledDevelopmentCompanionSource()
	if err != nil || !configured {
		t.Fatalf("source = (%v, %v, %v)", source, configured, err)
	}
	installation, err := source.Status()
	if err != nil {
		t.Fatal(err)
	}
	realMuxviaPath, err := filepath.EvalSymlinks(muxviaPath)
	if err != nil {
		t.Fatal(err)
	}
	wantCompanionPath := filepath.Join(filepath.Dir(realMuxviaPath), "muxvia-cloud")
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
	restore := configureBundledDevelopmentSourceTest(t, filepath.Join(t.TempDir(), "muxvia"), string(make([]byte, 64)))
	defer restore()
	muxviaBuildVersion = "v1.0.0"
	if _, configured, err := v3BundledDevelopmentCompanionSource(); configured || !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("stable bundled source = configured %v, err %v", configured, err)
	}
}

func configureBundledDevelopmentSourceTest(t *testing.T, muxviaPath, digest string) func() {
	t.Helper()
	previousExecutable := v3ExecutablePath
	previousMuxviaVersion := muxviaBuildVersion
	previousName := cloudDevelopmentCompanionName
	previousDigest := cloudDevelopmentCompanionSHA256
	previousVersion := cloudDevelopmentCompanionVersion
	previousChannel := cloudDevelopmentCompanionChannel
	v3ExecutablePath = func() (string, error) { return muxviaPath, nil }
	muxviaBuildVersion = v3DevelopmentBuildVersion
	cloudDevelopmentCompanionName = "muxvia-cloud"
	cloudDevelopmentCompanionSHA256 = digest
	cloudDevelopmentCompanionVersion = "v0.0.0-dev"
	cloudDevelopmentCompanionChannel = "development"
	return func() {
		v3ExecutablePath = previousExecutable
		muxviaBuildVersion = previousMuxviaVersion
		cloudDevelopmentCompanionName = previousName
		cloudDevelopmentCompanionSHA256 = previousDigest
		cloudDevelopmentCompanionVersion = previousVersion
		cloudDevelopmentCompanionChannel = previousChannel
	}
}
