//go:build !windows

package installer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
)

func TestInstallRejectsSymlinkedVersionsDirectory(t *testing.T) {
	fixture := newInstallerFixture(t)
	if err := os.MkdirAll(fixture.root, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(fixture.root, "versions")); err != nil {
		t.Fatal(err)
	}
	archive := buildArchive(t, []archiveEntry{{name: ExecutableName(), body: []byte("companion")}})
	_, err := fixture.installer.InstallArchive(context.Background(), fixture.manifest(t, "v1.0.0", archive), bytes.NewReader(archive), "stable")
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("symlinked versions directory error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "v1.0.0")); !os.IsNotExist(err) {
		t.Fatalf("installer wrote through versions symlink: %v", err)
	}
}
