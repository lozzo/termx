package fileapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lozzow/termx/termx-remote/protocol/runtimepb"
	"google.golang.org/protobuf/proto"
)

func TestRouteRequestListAndPreview(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	manager := NewManager()

	status, body, errMsg := manager.RouteRequest("POST", "/files/list", mustMarshalProto(t, &runtimepb.FileListRequest{Path: dir}))
	if status != 200 || errMsg != "" {
		t.Fatalf("list returned status=%d err=%q", status, errMsg)
	}
	var listResp runtimepb.FileListResponse
	if err := proto.Unmarshal(body, &listResp); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if len(listResp.GetEntries()) != 1 || listResp.GetEntries()[0].GetName() != "hello.txt" {
		t.Fatalf("unexpected list response: %#v", listResp)
	}
	if listResp.GetEntries()[0].GetModTime() == "" {
		t.Fatalf("expected mod time in list response: %#v", listResp.GetEntries()[0])
	}

	status, body, errMsg = manager.RouteRequest("POST", "/files/preview", mustMarshalProto(t, &runtimepb.FilePreviewRequest{Path: filePath}))
	if status != 200 || errMsg != "" {
		t.Fatalf("preview returned status=%d err=%q", status, errMsg)
	}
	var preview runtimepb.FilePreviewResponse
	if err := proto.Unmarshal(body, &preview); err != nil {
		t.Fatalf("unmarshal preview response: %v", err)
	}
	if preview.GetContent() != "hello world\n" {
		t.Fatalf("unexpected preview content: %#v", preview.GetContent())
	}
}

func TestRouteRequestListIncludesDirectoryChildCountAndSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	nestedDir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "guide.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	linkPath := filepath.Join(dir, "docs-link")
	if err := os.Symlink(nestedDir, linkPath); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	manager := NewManager()
	t.Cleanup(manager.Close)

	status, body, errMsg := manager.RouteRequest("POST", "/files/list", mustMarshalProto(t, &runtimepb.FileListRequest{Path: dir}))
	if status != 200 || errMsg != "" {
		t.Fatalf("list returned status=%d err=%q", status, errMsg)
	}
	var listResp runtimepb.FileListResponse
	if err := proto.Unmarshal(body, &listResp); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	var docsEntry *runtimepb.FileEntry
	var linkEntry *runtimepb.FileEntry
	for _, entry := range listResp.GetEntries() {
		switch entry.GetName() {
		case "docs":
			docsEntry = entry
		case "docs-link":
			linkEntry = entry
		}
	}
	if docsEntry == nil || docsEntry.ChildCount == nil || docsEntry.GetChildCount() != 1 {
		t.Fatalf("expected docs child count, got %#v", docsEntry)
	}
	if linkEntry == nil || linkEntry.GetLinkTarget() == "" || linkEntry.GetType() != "symlink-dir" {
		t.Fatalf("expected symlink target, got %#v", linkEntry)
	}
}

func TestPreviewReportsVideoCategoryWithoutInlineContent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(filePath, []byte("fake video bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	manager := NewManager()
	t.Cleanup(manager.Close)

	status, body, errMsg := manager.RouteRequest("POST", "/files/preview", mustMarshalProto(t, &runtimepb.FilePreviewRequest{Path: filePath, MaxSize: 4}))
	if status != 200 || errMsg != "" {
		t.Fatalf("preview returned status=%d err=%q", status, errMsg)
	}
	var preview runtimepb.FilePreviewResponse
	if err := proto.Unmarshal(body, &preview); err != nil {
		t.Fatalf("unmarshal preview response: %v", err)
	}
	if preview.GetCategory() != "video" {
		t.Fatalf("expected video category, got %#v", preview.GetCategory())
	}
	if preview.GetMimeType() != "video/mp4" {
		t.Fatalf("expected video/mp4 mime, got %#v", preview.GetMimeType())
	}
	if preview.GetContentBase64() != "" {
		t.Fatalf("expected video preview to omit inline content, got %#v", preview.GetContentBase64())
	}
	if preview.GetPreviewLimit() != 0 {
		t.Fatalf("expected video preview to avoid size limit, got %#v", preview.GetPreviewLimit())
	}
}

func TestPreviewReportsModelCategoryWithoutInlineContent(t *testing.T) {
	cases := []struct {
		name     string
		content  []byte
		mimeType string
	}{
		{name: "part.stl", content: []byte("solid part\nendsolid part\n"), mimeType: "model/stl"},
		{name: "mesh.obj", content: []byte("v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n"), mimeType: "model/obj"},
		{name: "scene.glb", content: []byte("glb bytes"), mimeType: "model/gltf-binary"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, tc.name)
			if err := os.WriteFile(filePath, tc.content, 0o644); err != nil {
				t.Fatalf("WriteFile returned error: %v", err)
			}

			manager := NewManager()
			t.Cleanup(manager.Close)

			status, body, errMsg := manager.RouteRequest("POST", "/files/preview", mustMarshalProto(t, &runtimepb.FilePreviewRequest{Path: filePath, MaxSize: 4}))
			if status != 200 || errMsg != "" {
				t.Fatalf("preview returned status=%d err=%q", status, errMsg)
			}
			var preview runtimepb.FilePreviewResponse
			if err := proto.Unmarshal(body, &preview); err != nil {
				t.Fatalf("unmarshal preview response: %v", err)
			}
			if preview.GetCategory() != "model" {
				t.Fatalf("expected model category, got %#v", preview.GetCategory())
			}
			if preview.GetMimeType() != tc.mimeType {
				t.Fatalf("expected %s mime, got %#v", tc.mimeType, preview.GetMimeType())
			}
			if preview.GetContentBase64() != "" {
				t.Fatalf("expected model preview to omit inline content, got %#v", preview.GetContentBase64())
			}
			if preview.GetPreviewLimit() != 0 {
				t.Fatalf("expected model preview to avoid size limit, got %#v", preview.GetPreviewLimit())
			}
		})
	}
}

func TestPreviewAllowsImageBeyondRequestedMaxSize(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(filePath, []byte("fake image bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	manager := NewManager()
	t.Cleanup(manager.Close)

	status, body, errMsg := manager.RouteRequest("POST", "/files/preview", mustMarshalProto(t, &runtimepb.FilePreviewRequest{Path: filePath, MaxSize: 4}))
	if status != 200 || errMsg != "" {
		t.Fatalf("preview returned status=%d err=%q", status, errMsg)
	}
	var preview runtimepb.FilePreviewResponse
	if err := proto.Unmarshal(body, &preview); err != nil {
		t.Fatalf("unmarshal preview response: %v", err)
	}
	if preview.GetCategory() != "image" {
		t.Fatalf("expected image category, got %#v", preview.GetCategory())
	}
	if preview.GetContentBase64() == "" {
		t.Fatalf("expected image preview content, got %#v", preview)
	}
	if preview.GetPreviewLimit() != 0 {
		t.Fatalf("expected image preview to avoid size limit, got %#v", preview.GetPreviewLimit())
	}
}

func TestDownloadInitAcceptsStableTransferIDForResume(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "artifact.bin")
	if err := os.WriteFile(filePath, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	manager := NewManager()
	t.Cleanup(manager.Close)

	status, body, errMsg := manager.RouteRequest("POST", "/files/download/init", mustMarshalProto(t, &runtimepb.FileDownloadInitRequest{Path: filePath, Offset: 4, TransferId: "dl-resume-1"}))
	if status != 200 || errMsg != "" {
		t.Fatalf("download init returned status=%d err=%q", status, errMsg)
	}
	var resp runtimepb.FileDownloadInitResponse
	if err := proto.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal download init response: %v", err)
	}
	if resp.GetTransferId() != "dl-resume-1" {
		t.Fatalf("expected stable transfer id, got %#v", resp.GetTransferId())
	}

	manager.mu.Lock()
	transfer := manager.transfers["dl-resume-1"]
	manager.mu.Unlock()
	if transfer == nil {
		t.Fatal("expected transfer to be registered")
	}
	if transfer.Offset != 4 {
		t.Fatalf("expected offset 4, got %d", transfer.Offset)
	}
}

func TestDownloadInitAcceptsByteRangeLength(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(filePath, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	manager := NewManager()
	t.Cleanup(manager.Close)

	status, body, errMsg := manager.RouteRequest("POST", "/files/download/init", mustMarshalProto(t, &runtimepb.FileDownloadInitRequest{Path: filePath, Offset: 4, Length: 3, TransferId: "preview-range-1"}))
	if status != 200 || errMsg != "" {
		t.Fatalf("download init returned status=%d err=%q", status, errMsg)
	}
	var resp runtimepb.FileDownloadInitResponse
	if err := proto.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal download init response: %v", err)
	}
	if resp.GetOffset() != 4 {
		t.Fatalf("expected response offset 4, got %#v", resp.GetOffset())
	}
	if resp.GetLength() != 3 {
		t.Fatalf("expected response length 3, got %#v", resp.GetLength())
	}

	manager.mu.Lock()
	transfer := manager.transfers["preview-range-1"]
	manager.mu.Unlock()
	if transfer == nil {
		t.Fatal("expected transfer to be registered")
	}
	if transfer.Offset != 4 || transfer.Length != 3 {
		t.Fatalf("expected offset=4 length=3, got offset=%d length=%d", transfer.Offset, transfer.Length)
	}
}

func TestDownloadInitClampsRangeLengthToFileRemainder(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(filePath, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	manager := NewManager()
	t.Cleanup(manager.Close)

	status, body, errMsg := manager.RouteRequest("POST", "/files/download/init", mustMarshalProto(t, &runtimepb.FileDownloadInitRequest{Path: filePath, Offset: 7, Length: 99, TransferId: "preview-range-2"}))
	if status != 200 || errMsg != "" {
		t.Fatalf("download init returned status=%d err=%q", status, errMsg)
	}
	var resp runtimepb.FileDownloadInitResponse
	if err := proto.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal download init response: %v", err)
	}
	if resp.GetLength() != 3 {
		t.Fatalf("expected clamped response length 3, got %#v", resp.GetLength())
	}

	manager.mu.Lock()
	transfer := manager.transfers["preview-range-2"]
	manager.mu.Unlock()
	if transfer == nil {
		t.Fatal("expected transfer to be registered")
	}
	if transfer.Length != 3 {
		t.Fatalf("expected transfer length 3, got %d", transfer.Length)
	}
}

func TestDownloadInitRejectsInvalidStableTransferID(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "artifact.bin")
	if err := os.WriteFile(filePath, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	manager := NewManager()
	t.Cleanup(manager.Close)

	status, _, errMsg := manager.RouteRequest("POST", "/files/download/init", mustMarshalProto(t, &runtimepb.FileDownloadInitRequest{Path: filePath, TransferId: "../bad"}))
	if status != 400 || errMsg != "invalid transfer_id" {
		t.Fatalf("download init returned status=%d err=%q", status, errMsg)
	}
}

func mustMarshalProto(t *testing.T, msg proto.Message) []byte {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal proto: %v", err)
	}
	return data
}
