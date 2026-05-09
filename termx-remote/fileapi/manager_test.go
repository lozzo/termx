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

func TestPreviewReportsVideoCategoryAndLimit(t *testing.T) {
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
		t.Fatalf("expected oversized preview to omit content, got %#v", preview["content_base64"])
	}
	if preview["preview_limit"] != float64(4) {
		t.Fatalf("expected preview limit 4, got %#v", preview["preview_limit"])
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
