package fileapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRouteRequestListAndPreview(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	manager := NewManager()

	status, body, errMsg := manager.RouteRequest("POST", "/files/list", []byte(`{"path":"`+dir+`"}`))
	if status != 200 || errMsg != "" {
		t.Fatalf("list returned status=%d err=%q", status, errMsg)
	}
	var listResp DirListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if len(listResp.Entries) != 1 || listResp.Entries[0].Name != "hello.txt" {
		t.Fatalf("unexpected list response: %#v", listResp)
	}
	if listResp.Entries[0].ModTime == "" {
		t.Fatalf("expected mod time in list response: %#v", listResp.Entries[0])
	}

	status, body, errMsg = manager.RouteRequest("POST", "/files/preview", []byte(`{"path":"`+filePath+`"}`))
	if status != 200 || errMsg != "" {
		t.Fatalf("preview returned status=%d err=%q", status, errMsg)
	}
	var preview map[string]any
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatalf("unmarshal preview response: %v", err)
	}
	if preview["content"] != "hello world\n" {
		t.Fatalf("unexpected preview content: %#v", preview["content"])
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

	status, body, errMsg := manager.RouteRequest("POST", "/files/list", []byte(`{"path":"`+dir+`"}`))
	if status != 200 || errMsg != "" {
		t.Fatalf("list returned status=%d err=%q", status, errMsg)
	}
	var listResp DirListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	var docsEntry *FileEntry
	var linkEntry *FileEntry
	for i := range listResp.Entries {
		switch listResp.Entries[i].Name {
		case "docs":
			docsEntry = &listResp.Entries[i]
		case "docs-link":
			linkEntry = &listResp.Entries[i]
		}
	}
	if docsEntry == nil || docsEntry.ChildCount == nil || *docsEntry.ChildCount != 1 {
		t.Fatalf("expected docs child count, got %#v", docsEntry)
	}
	if linkEntry == nil || linkEntry.LinkTarget == "" || linkEntry.Type != "symlink-dir" {
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

	status, body, errMsg := manager.RouteRequest("POST", "/files/preview", []byte(`{"path":"`+filePath+`","max_size":4}`))
	if status != 200 || errMsg != "" {
		t.Fatalf("preview returned status=%d err=%q", status, errMsg)
	}
	var preview map[string]any
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatalf("unmarshal preview response: %v", err)
	}
	if preview["category"] != "video" {
		t.Fatalf("expected video category, got %#v", preview["category"])
	}
	if preview["mime_type"] != "video/mp4" {
		t.Fatalf("expected video/mp4 mime, got %#v", preview["mime_type"])
	}
	if preview["content_base64"] != nil {
		t.Fatalf("expected video preview to omit inline content, got %#v", preview["content_base64"])
	}
	if preview["preview_limit"] != nil {
		t.Fatalf("expected video preview to avoid size limit, got %#v", preview["preview_limit"])
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

			status, body, errMsg := manager.RouteRequest("POST", "/files/preview", []byte(`{"path":"`+filePath+`","max_size":4}`))
			if status != 200 || errMsg != "" {
				t.Fatalf("preview returned status=%d err=%q", status, errMsg)
			}
			var preview map[string]any
			if err := json.Unmarshal(body, &preview); err != nil {
				t.Fatalf("unmarshal preview response: %v", err)
			}
			if preview["category"] != "model" {
				t.Fatalf("expected model category, got %#v", preview["category"])
			}
			if preview["mime_type"] != tc.mimeType {
				t.Fatalf("expected %s mime, got %#v", tc.mimeType, preview["mime_type"])
			}
			if preview["content_base64"] != nil {
				t.Fatalf("expected model preview to omit inline content, got %#v", preview["content_base64"])
			}
			if preview["preview_limit"] != nil {
				t.Fatalf("expected model preview to avoid size limit, got %#v", preview["preview_limit"])
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

	status, body, errMsg := manager.RouteRequest("POST", "/files/preview", []byte(`{"path":"`+filePath+`","max_size":4}`))
	if status != 200 || errMsg != "" {
		t.Fatalf("preview returned status=%d err=%q", status, errMsg)
	}
	var preview map[string]any
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatalf("unmarshal preview response: %v", err)
	}
	if preview["category"] != "image" {
		t.Fatalf("expected image category, got %#v", preview["category"])
	}
	if preview["content_base64"] == nil {
		t.Fatalf("expected image preview content, got %#v", preview)
	}
	if preview["preview_limit"] != nil {
		t.Fatalf("expected image preview to avoid size limit, got %#v", preview["preview_limit"])
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

	status, body, errMsg := manager.RouteRequest("POST", "/files/download/init", []byte(`{"path":"`+filePath+`","offset":4,"transfer_id":"dl-resume-1"}`))
	if status != 200 || errMsg != "" {
		t.Fatalf("download init returned status=%d err=%q", status, errMsg)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal download init response: %v", err)
	}
	if resp["transfer_id"] != "dl-resume-1" {
		t.Fatalf("expected stable transfer id, got %#v", resp["transfer_id"])
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

	status, body, errMsg := manager.RouteRequest("POST", "/files/download/init", []byte(`{"path":"`+filePath+`","offset":4,"length":3,"transfer_id":"preview-range-1"}`))
	if status != 200 || errMsg != "" {
		t.Fatalf("download init returned status=%d err=%q", status, errMsg)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal download init response: %v", err)
	}
	if resp["offset"] != float64(4) {
		t.Fatalf("expected response offset 4, got %#v", resp["offset"])
	}
	if resp["length"] != float64(3) {
		t.Fatalf("expected response length 3, got %#v", resp["length"])
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

	status, body, errMsg := manager.RouteRequest("POST", "/files/download/init", []byte(`{"path":"`+filePath+`","offset":7,"length":99,"transfer_id":"preview-range-2"}`))
	if status != 200 || errMsg != "" {
		t.Fatalf("download init returned status=%d err=%q", status, errMsg)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal download init response: %v", err)
	}
	if resp["length"] != float64(3) {
		t.Fatalf("expected clamped response length 3, got %#v", resp["length"])
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

	status, _, errMsg := manager.RouteRequest("POST", "/files/download/init", []byte(`{"path":"`+filePath+`","transfer_id":"../bad"}`))
	if status != 400 || errMsg != "invalid transfer_id" {
		t.Fatalf("download init returned status=%d err=%q", status, errMsg)
	}
}
