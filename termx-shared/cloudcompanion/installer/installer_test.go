package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
)

func TestSignedArchiveInstallsAndStatusRevalidatesExecutable(t *testing.T) {
	fixture := newInstallerFixture(t)
	archive := buildArchive(t, []archiveEntry{{name: ExecutableName(), body: []byte("companion-v1")}})
	manifest := fixture.manifest(t, "v1.2.3", archive)
	installed, err := fixture.installer.InstallArchive(context.Background(), manifest, bytes.NewReader(archive), "stable")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Version != "v1.2.3" || installed.BinaryPath != filepath.Join(fixture.root, "versions", "v1.2.3", ExecutableName()) {
		t.Fatalf("installation = %#v", installed)
	}
	status, err := fixture.installer.Status()
	if err != nil || status.BinarySHA256 != installed.BinarySHA256 {
		t.Fatalf("Status = (%#v, %v)", status, err)
	}
	if fixture.smokeCount != 1 {
		t.Fatalf("smoke count = %d", fixture.smokeCount)
	}

	if err := os.WriteFile(installed.BinaryPath, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.installer.Status(); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("tampered Status error = %v", err)
	}
}

func TestFailedUpdateSmokePreservesOldActiveVersion(t *testing.T) {
	fixture := newInstallerFixture(t)
	firstArchive := buildArchive(t, []archiveEntry{{name: ExecutableName(), body: []byte("companion-v1")}})
	if _, err := fixture.installer.InstallArchive(context.Background(), fixture.manifest(t, "v1.0.0", firstArchive), bytes.NewReader(firstArchive), "stable"); err != nil {
		t.Fatal(err)
	}
	fixture.failSmoke = true
	secondArchive := buildArchive(t, []archiveEntry{{name: ExecutableName(), body: []byte("companion-v2")}})
	if _, err := fixture.installer.InstallArchive(context.Background(), fixture.manifest(t, "v2.0.0", secondArchive), bytes.NewReader(secondArchive), "stable"); err == nil {
		t.Fatal("update smoke failure must reject activation")
	}
	status, err := fixture.installer.Status()
	if err != nil || status.Version != "v1.0.0" {
		t.Fatalf("active version after failed update = (%#v, %v)", status, err)
	}
}

func TestInstallerRejectsSignatureHashPlatformDowngradeAndArchiveScripts(t *testing.T) {
	fixture := newInstallerFixture(t)
	archive := buildArchive(t, []archiveEntry{{name: ExecutableName(), body: []byte("companion-v2")}})
	valid := fixture.baseManifest("v2.0.0", archive)

	wrongPlatform := valid
	wrongPlatform.Arch = "wrong-arch"
	wrongPlatformPayload, err := MarshalSignedManifest(wrongPlatform, fixture.privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.installer.InstallArchive(context.Background(), wrongPlatformPayload, bytes.NewReader(archive), "stable"); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("wrong platform error = %v", err)
	}

	wrongSignaturePayload := fixture.manifest(t, "v2.0.0", archive)
	wrongSignaturePayload[len(wrongSignaturePayload)-3] ^= 1
	if _, err := fixture.installer.InstallArchive(context.Background(), wrongSignaturePayload, bytes.NewReader(archive), "stable"); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("wrong signature error = %v", err)
	}

	wrongHash := valid
	wrongHash.ArchiveSHA256 = hex.EncodeToString(bytes.Repeat([]byte{0xaa}, 32))
	wrongHashPayload, _ := MarshalSignedManifest(wrongHash, fixture.privateKey)
	if _, err := fixture.installer.InstallArchive(context.Background(), wrongHashPayload, bytes.NewReader(archive), "stable"); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("wrong hash error = %v", err)
	}

	if _, err := fixture.installer.InstallArchive(context.Background(), fixture.manifest(t, "v2.0.0", archive), bytes.NewReader(archive), "stable"); err != nil {
		t.Fatal(err)
	}
	oldArchive := buildArchive(t, []archiveEntry{{name: ExecutableName(), body: []byte("companion-v1")}})
	if _, err := fixture.installer.InstallArchive(context.Background(), fixture.manifest(t, "v1.0.0", oldArchive), bytes.NewReader(oldArchive), "stable"); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("downgrade error = %v", err)
	}

	scriptArchive := buildArchive(t, []archiveEntry{{name: ExecutableName(), body: []byte("companion-v3")}, {name: "postinstall.sh", body: []byte("exit 0")}})
	if _, err := fixture.installer.InstallArchive(context.Background(), fixture.manifest(t, "v3.0.0", scriptArchive), bytes.NewReader(scriptArchive), "stable"); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("archive script error = %v", err)
	}
}

func TestTruncatedArchiveAndUnknownSigningKeyFailClosed(t *testing.T) {
	fixture := newInstallerFixture(t)
	archive := buildArchive(t, []archiveEntry{{name: ExecutableName(), body: []byte("companion")}})
	manifest := fixture.manifest(t, "v1.0.0", archive)
	if _, err := fixture.installer.InstallArchive(context.Background(), manifest, bytes.NewReader(archive[:len(archive)-1]), "stable"); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("truncated archive error = %v", err)
	}

	unknown := fixture.baseManifest("v1.0.0", archive)
	unknown.SigningKeyID = "unknown"
	unknownPayload, _ := MarshalSignedManifest(unknown, fixture.privateKey)
	if _, err := fixture.installer.InstallArchive(context.Background(), unknownPayload, bytes.NewReader(archive), "stable"); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("unknown signing key error = %v", err)
	}
}

func TestUninstallOnlyRemovesInstallerRoot(t *testing.T) {
	fixture := newInstallerFixture(t)
	archive := buildArchive(t, []archiveEntry{{name: ExecutableName(), body: []byte("companion")}})
	if _, err := fixture.installer.InstallArchive(context.Background(), fixture.manifest(t, "v1.0.0", archive), bytes.NewReader(archive), "stable"); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(filepath.Dir(fixture.root), "daemon-grants-unchanged")
	if err := os.WriteFile(sentinel, []byte("grant-store"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.installer.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("installer root still exists: %v", err)
	}
	if payload, err := os.ReadFile(sentinel); err != nil || string(payload) != "grant-store" {
		t.Fatalf("outside sentinel = (%q, %v)", payload, err)
	}
}

type installerFixture struct {
	t          *testing.T
	root       string
	now        time.Time
	privateKey ed25519.PrivateKey
	installer  *Installer
	smokeCount int
	failSmoke  bool
}

func newInstallerFixture(t *testing.T) *installerFixture {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &installerFixture{t: t, root: filepath.Join(t.TempDir(), "cloud-companion"), now: time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC), privateKey: privateKey}
	fixture.installer, err = New(Config{
		RootDir: fixture.root, Origin: "https://releases.example.test/cloud-companion", OS: runtime.GOOS, Arch: runtime.GOARCH,
		TrustedKeys: map[string]ed25519.PublicKey{"test-root": publicKey}, Now: func() time.Time { return fixture.now },
		Smoke: func(_ context.Context, binaryPath string, _ Manifest) error {
			fixture.smokeCount++
			payload, readErr := os.ReadFile(binaryPath)
			if readErr != nil || len(payload) == 0 {
				return errors.New("staged binary is unreadable")
			}
			if fixture.failSmoke {
				return errors.New("injected handshake failure")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture *installerFixture) baseManifest(version string, archive []byte) Manifest {
	digest := sha256.Sum256(archive)
	return Manifest{
		SchemaVersion: manifestSchemaVersion, Channel: "stable", Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH,
		DownloadURL:   "https://releases.example.test/cloud-companion/artifacts/" + version + ".tar.gz",
		ArchiveSHA256: hex.EncodeToString(digest[:]), ArchiveSize: int64(len(archive)), SigningKeyID: "test-root",
		MinCompanionProtocol: cloudcompanion.ProtocolVersionMin, MaxCompanionProtocol: cloudcompanion.ProtocolVersionMax,
		PublishedAt: fixture.now.Format(time.RFC3339),
	}
}

func (fixture *installerFixture) manifest(t *testing.T, version string, archive []byte) []byte {
	t.Helper()
	payload, err := MarshalSignedManifest(fixture.baseManifest(version, archive), fixture.privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

type archiveEntry struct {
	name string
	body []byte
}

func buildArchive(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		if err := tarWriter.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o700, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
