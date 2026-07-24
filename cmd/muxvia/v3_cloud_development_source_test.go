package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

func TestBundledDevelopmentCompanionSourceMaterializesEmbeddedArtifact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission validation is platform-specific")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("verified development companion")
	hash := sha256.Sum256(payload)
	digest := hex.EncodeToString(hash[:])
	restore := configureBundledDevelopmentSourceTest(t, root, payload, digest)
	defer restore()

	source, configured, err := v3BundledDevelopmentCompanionSource()
	if err != nil || !configured {
		t.Fatalf("source = (%v, %v, %v)", source, configured, err)
	}
	installation, err := source.Status()
	if err != nil {
		t.Fatal(err)
	}
	wantCompanionPath := filepath.Join(root, "bundled", digest, "muxvia-cloud")
	if installation.BinaryPath != wantCompanionPath || installation.Version != "v0.0.0-dev" || installation.Channel != "development" {
		t.Fatalf("installation = %#v", installation)
	}
	materialized, err := os.ReadFile(wantCompanionPath)
	if err != nil || string(materialized) != string(payload) {
		t.Fatalf("materialized Companion = %q, %v", materialized, err)
	}
	info, err := os.Stat(wantCompanionPath)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("materialized Companion mode = %v, %v", info, err)
	}

	if err := os.WriteFile(wantCompanionPath, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Status(); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("tampered status error = %v", err)
	}
}

func TestBundledDevelopmentCompanionSourceMaterializesConcurrently(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("one embedded companion for every caller")
	hash := sha256.Sum256(payload)
	restore := configureBundledDevelopmentSourceTest(t, root, payload, hex.EncodeToString(hash[:]))
	defer restore()
	source, configured, err := v3BundledDevelopmentCompanionSource()
	if err != nil || !configured {
		t.Fatalf("source = (%v, %v, %v)", source, configured, err)
	}
	var group sync.WaitGroup
	errorsByCaller := make(chan error, 8)
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, statusErr := source.Status()
			errorsByCaller <- statusErr
		}()
	}
	group.Wait()
	close(errorsByCaller)
	for statusErr := range errorsByCaller {
		if statusErr != nil {
			t.Fatal(statusErr)
		}
	}
}

func TestBundledDevelopmentCompanionSourceRejectsEmbeddedDigestMismatch(t *testing.T) {
	payload := []byte("embedded companion")
	restore := configureBundledDevelopmentSourceTest(t, t.TempDir(), payload, string(make([]byte, 64)))
	defer restore()
	if _, configured, err := v3BundledDevelopmentCompanionSource(); configured || !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("mismatched embedded source = configured %v, err %v", configured, err)
	}
}

func TestBundledDevelopmentCompanionSourceRejectsSymlinkRootWithoutChangingTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink ownership validation is platform-specific")
	}
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "root-link")
	if err := os.Symlink(target, root); err != nil {
		t.Fatal(err)
	}
	payload := []byte("embedded companion")
	hash := sha256.Sum256(payload)
	restore := configureBundledDevelopmentSourceTest(t, root, payload, hex.EncodeToString(hash[:]))
	defer restore()
	source, configured, err := v3BundledDevelopmentCompanionSource()
	if err != nil || !configured {
		t.Fatalf("source = (%v, %v, %v)", source, configured, err)
	}
	if _, err := source.Status(); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("symlink root status error = %v", err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("symlink target mode changed = %v, %v", info, err)
	}
}

func TestBundledDevelopmentCompanionSourceRejectsStableBuild(t *testing.T) {
	payload := []byte("embedded companion")
	hash := sha256.Sum256(payload)
	restore := configureBundledDevelopmentSourceTest(t, t.TempDir(), payload, hex.EncodeToString(hash[:]))
	defer restore()
	muxviaBuildVersion = "v1.0.0"
	if _, configured, err := v3BundledDevelopmentCompanionSource(); configured || !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("stable bundled source = configured %v, err %v", configured, err)
	}
}

func configureBundledDevelopmentSourceTest(t *testing.T, root string, payload []byte, digest string) func() {
	t.Helper()
	previousRoot := v3CloudCompanionRootDir
	previousMuxviaVersion := muxviaBuildVersion
	previousEmbedded := cloudDevelopmentCompanionEmbedded
	previousDigest := cloudDevelopmentCompanionSHA256
	previousVersion := cloudDevelopmentCompanionVersion
	previousChannel := cloudDevelopmentCompanionChannel
	v3CloudCompanionRootDir = func() string { return root }
	muxviaBuildVersion = v3DevelopmentBuildVersion
	cloudDevelopmentCompanionEmbedded = append([]byte(nil), payload...)
	cloudDevelopmentCompanionSHA256 = digest
	cloudDevelopmentCompanionVersion = "v0.0.0-dev"
	cloudDevelopmentCompanionChannel = "development"
	return func() {
		v3CloudCompanionRootDir = previousRoot
		muxviaBuildVersion = previousMuxviaVersion
		cloudDevelopmentCompanionEmbedded = previousEmbedded
		cloudDevelopmentCompanionSHA256 = previousDigest
		cloudDevelopmentCompanionVersion = previousVersion
		cloudDevelopmentCompanionChannel = previousChannel
	}
}
