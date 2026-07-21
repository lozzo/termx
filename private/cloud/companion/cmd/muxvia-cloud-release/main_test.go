package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"github.com/muxvia/muxvia/shared/cloudcompanion/installer"
)

func TestReleaseToolProducesInstallerVerifiableArtifactWithoutKeyLeak(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "release-key.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}), 0o600); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(t.TempDir(), "muxvia-cloud")
	if err := os.WriteFile(binaryPath, []byte("private companion binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	outputDir := t.TempDir()
	now := time.Date(2026, 7, 11, 14, 0, 0, 0, time.UTC)
	var output bytes.Buffer
	err = run([]string{
		"--binary", binaryPath, "--signing-key", keyPath, "--key-id", "release-1", "--channel", "stable",
		"--version", "v1.2.3", "--os", runtime.GOOS, "--arch", runtime.GOARCH,
		"--download-url", "https://releases.muxvia.dev/cloud-companion/artifacts/v1.2.3.tar.gz", "--out", outputDir,
	}, &output, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(outputDir, "muxvia-cloud_1.2.3_"+runtime.GOOS+"_"+runtime.GOARCH+".json")
	archivePath := filepath.Join(outputDir, "muxvia-cloud_1.2.3_"+runtime.GOOS+"_"+runtime.GOARCH+".tar.gz")
	manifestPayload, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := installer.ParseAndVerifyManifest(manifestPayload, installer.Verification{
		Channel: "stable", OS: runtime.GOOS, Arch: runtime.GOARCH,
		ProtocolMin: cloudcompanion.ProtocolVersionMin, ProtocolMax: cloudcompanion.ProtocolVersionMax,
		Now: now, TrustedKeys: map[string]ed25519.PublicKey{"release-1": publicKey},
	})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(archivePath)
	if err != nil || int64(len(archive)) != manifest.ArchiveSize {
		t.Fatalf("archive = (%d bytes, %v)", len(archive), err)
	}
	if bytes.Contains(manifestPayload, privateKey) || bytes.Contains(archive, privateKey) || bytes.Contains(output.Bytes(), privateKey) {
		t.Fatal("release artifact leaked signing private key")
	}
}
