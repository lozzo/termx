package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
)

func TestProtocolFileMetadataAndMutations(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("hello world"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("alpha.txt", filepath.Join(root, "alpha-link")); err != nil {
		t.Fatal(err)
	}
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	var list protocol.FileListResult
	if err := client.Call(context.Background(), "file.list", protocol.FileListParams{Path: root, Limit: 1}, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 1 || list.NextCursor == "" {
		t.Fatalf("unexpected first page %#v", list)
	}
	var next protocol.FileListResult
	if err := client.Call(context.Background(), "file.list", protocol.FileListParams{Path: root, Cursor: list.NextCursor, Limit: 10}, &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Entries) != 1 {
		t.Fatalf("unexpected second page %#v", next)
	}
	changedTime := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(root, changedTime, changedTime); err != nil {
		t.Fatal(err)
	}
	if _, err := client.FileList(context.Background(), protocol.FileListParams{Path: root, Cursor: list.NextCursor, Limit: 10}); err == nil || !strings.Contains(err.Error(), "stale file list cursor") {
		t.Fatalf("expected stale cursor error, got %v", err)
	}

	var link protocol.FileEntry
	if err := client.Call(context.Background(), "file.stat", protocol.FilePathParams{Path: filepath.Join(root, "alpha-link")}, &link); err != nil {
		t.Fatal(err)
	}
	if link.Type != "symlink" || link.LinkTarget != "alpha.txt" {
		t.Fatalf("symlink was followed: %#v", link)
	}

	var preview protocol.FilePreviewResult
	if err := client.Call(context.Background(), "file.preview", protocol.FilePreviewParams{Path: filepath.Join(root, "alpha.txt"), MaxBytes: 5}, &preview); err != nil {
		t.Fatal(err)
	}
	if string(preview.Content) != "hello" || !preview.Truncated {
		t.Fatalf("unexpected preview %#v", preview)
	}

	dir := filepath.Join(root, "target")
	var operation protocol.FileOperationResult
	if err := client.Call(context.Background(), "file.mkdir", protocol.FilePathParams{Path: dir}, &operation); err != nil || !operation.Success {
		t.Fatalf("mkdir: %#v %v", operation, err)
	}
	var copied protocol.FileBatchResult
	if err := client.Call(context.Background(), "file.copy", protocol.FileCopyMoveParams{Paths: []string{filepath.Join(root, "alpha.txt")}, TargetDir: dir}, &copied); err != nil || len(copied.Results) != 1 || !copied.Results[0].Success {
		t.Fatalf("copy: %#v %v", copied, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "alpha.txt")); err != nil || string(data) != "hello world" {
		t.Fatalf("copied content %q %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("replacement"), 0o640); err != nil {
		t.Fatal(err)
	}
	overwritten, err := client.FileCopy(context.Background(), protocol.FileCopyMoveParams{Paths: []string{filepath.Join(root, "alpha.txt")}, TargetDir: dir, Overwrite: true})
	if err != nil || !overwritten.Results[0].Success {
		t.Fatalf("overwrite copy: %#v %v", overwritten, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "alpha.txt")); err != nil || string(data) != "replacement" {
		t.Fatalf("overwrite content %q %v", data, err)
	}
	if err := client.Call(context.Background(), "file.rename", protocol.FileRenameParams{Path: filepath.Join(dir, "alpha.txt"), NewPath: filepath.Join(dir, "renamed.txt")}, &operation); err != nil || !operation.Success {
		t.Fatalf("rename: %#v %v", operation, err)
	}
	if err := client.Call(context.Background(), "file.delete", protocol.FilePathParams{Path: dir, Recursive: true}, &operation); err != nil || !operation.Success {
		t.Fatalf("delete: %#v %v", operation, err)
	}
}

func TestProtocolFileMethodsRequireExplicitDaemonPermission(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	client, closeClient := newClientForServedTransport(t, server, TransportScope{TerminalID: "term-1"}, true)
	defer closeClient()
	var out protocol.FileListResult
	err := client.Call(context.Background(), "file.list", protocol.FileListParams{Path: t.TempDir()}, &out)
	if err == nil || !strings.Contains(err.Error(), "denies daemon file method") {
		t.Fatalf("expected file scope denial, got %v", err)
	}
}

func TestProtocolFileCopyRequiresReadAndMutatePermissions(t *testing.T) {
	scope := TransportScope{AllowDaemon: true, FileMutate: true}
	params := protocol.FileCopyMoveParams{Paths: []string{filepath.Join(t.TempDir(), "source")}, TargetDir: t.TempDir()}
	if _, err := scope.constrainMethod("file.copy", params); err == nil || !strings.Contains(err.Error(), "denies file permission") {
		t.Fatalf("expected copy read denial, got %v", err)
	}
}
